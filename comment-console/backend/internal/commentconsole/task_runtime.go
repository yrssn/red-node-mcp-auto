package commentconsole

import (
	"context"
	"errors"
	"time"
)

const maxConcurrentTasks = 2

type taskRuntimeStatus struct {
	phase, progress string
}

type taskRun struct {
	cancel  context.CancelFunc
	failure *taskDiagnosticError
	taskRuntimeStatus
}

func (e *Engine) taskRunningLocked(key string) bool {
	return key != "" && (e.running == key || e.taskRuns[key] != nil)
}

func (e *Engine) taskSnapshotLocked(t *Task) {
	t.Running = e.taskRunningLocked(t.ID)
	t.Queued = e.pending[t.ID] && !t.Running
	status := e.taskStatuses[t.ID]
	if run := e.taskRuns[t.ID]; run != nil {
		status = run.taskRuntimeStatus
	} else if t.Queued {
		if status.phase != "queued" {
			status = taskRuntimeStatus{"queued", "等待空闲任务名额，最多同时处理 2 个任务"}
		}
	} else if !t.Running && t.Paused {
		status = taskRuntimeStatus{"paused", "任务已暂停"}
	} else if status.phase == "" {
		if t.LastError != "" {
			status = taskRuntimeStatus{"failed", t.LastError}
		} else if t.LastRun > 0 {
			status = taskRuntimeStatus{"completed", "上轮已结束，等待手动运行或下次定时检查"}
		} else {
			status = taskRuntimeStatus{"idle", "尚未运行"}
		}
	}
	t.Phase, t.Progress = status.phase, status.progress
}

func (e *Engine) setTaskProgress(key, phase, progress string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if run := e.taskRuns[key]; run != nil {
		run.taskRuntimeStatus = taskRuntimeStatus{phase, progress}
		run.failure = nil
	} else if e.running == key {
		e.setTaskStatusLocked(key, phase, progress)
	}
}

func (e *Engine) setTaskStatusLocked(key, phase, progress string) {
	if e.taskStatuses == nil {
		e.taskStatuses = map[string]taskRuntimeStatus{}
	}
	e.taskStatuses[key] = taskRuntimeStatus{phase, progress}
}

func (e *Engine) notifyScheduler() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) enqueueTaskLocked(key string) {
	if e.pending == nil {
		e.pending = map[string]bool{}
	}
	if e.pending[key] || e.taskRunningLocked(key) {
		return
	}
	e.pending[key] = true
	e.pendingOrder = append(e.pendingOrder, key)
	e.setTaskStatusLocked(key, "queued", "等待空闲任务名额，最多同时处理 2 个任务")
}

func (e *Engine) removePendingLocked(key string) {
	delete(e.pending, key)
	if !e.taskRunningLocked(key) && e.taskStatuses[key].phase == "queued" {
		delete(e.taskStatuses, key)
	}
	remaining := e.pendingOrder[:0]
	for _, queued := range e.pendingOrder {
		if queued != key {
			remaining = append(remaining, queued)
		}
	}
	e.pendingOrder = remaining
}

func (e *Engine) cancelTasksLocked() {
	for _, run := range e.taskRuns {
		run.cancel()
	}
	if e.runCancel != nil {
		e.runCancel()
	}
	e.pending = map[string]bool{}
	e.pendingOrder = nil
}

// taskBrowser 串行执行同一账号的站点动作，OCR 和模型请求不占浏览器锁。
func (e *Engine) taskBrowser(ctx context.Context, key, phase, progress string, action func() error) error {
	e.setTaskProgress(key, "waiting_browser", "等待浏览器："+progress)
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.setTaskProgress(key, phase, progress)
	err := action()
	if err != nil {
		diagnostic := safeTaskFailure(phase, err)
		e.mu.Lock()
		if run := e.taskRuns[key]; run != nil {
			run.failure = diagnostic
		}
		e.mu.Unlock()
		return diagnostic
	}
	return nil
}

func (e *Engine) loop() {
	senderDone := make(chan struct{})
	go func() { defer close(senderDone); e.sendLoop() }()
	defer func() {
		e.mu.Lock()
		e.stopping = true
		e.cancelTasksLocked()
		e.mu.Unlock()
		e.workers.Wait()
		<-senderDone
		close(e.done)
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-tick.C:
			e.runNext()
		case <-e.wake:
			e.runNext()
		}
	}
}

func (e *Engine) sendLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-tick.C:
			e.sendNext()
		}
	}
}

// runNext 按 FIFO 分配任务名额；持久化启动记录成功后才创建 worker。
func (e *Engine) runNext() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping || e.ctx.Err() != nil || e.running != "" {
		return
	}
	// 同时兼容已经在 pending 中、尚未加入 FIFO 的任务。
	ordered := map[string]bool{}
	for _, key := range e.pendingOrder {
		ordered[key] = true
	}
	for _, task := range e.state.Tasks {
		if task.Paused || e.taskRunningLocked(task.ID) {
			continue
		}
		if e.pending[task.ID] {
			if !ordered[task.ID] {
				e.pendingOrder = append(e.pendingOrder, task.ID)
			}
		} else if task.Scheduled && task.NextRun <= now() {
			e.enqueueTaskLocked(task.ID)
		}
	}
	for len(e.taskRuns) < maxConcurrentTasks && len(e.pendingOrder) > 0 {
		key := e.pendingOrder[0]
		task := findTask(&e.state, key)
		if !e.pending[key] || task == nil || task.Paused || e.taskRunningLocked(key) {
			e.pendingOrder = e.pendingOrder[1:]
			delete(e.pending, key)
			continue
		}
		err := e.updateLocked(func(s *State) error {
			t := findTask(s, key)
			t.LastRun, t.SendCount, t.LastError = now(), 0, ""
			t.NextRun = now() + int64(t.Interval)
			logState(s, key, "info", "开始获取与分析")
			return nil
		})
		if err != nil {
			e.setTaskStatusLocked(key, "queued", "启动状态保存失败，尚未访问站点；请检查数据库连接")
			return
		}
		task = findTask(&e.state, key)
		ctx, cancel := context.WithTimeout(e.ctx, 15*time.Minute)
		run := &taskRun{cancel: cancel, taskRuntimeStatus: taskRuntimeStatus{"account", "准备核实登录账号"}}
		if e.taskRuns == nil {
			e.taskRuns = map[string]*taskRun{}
		}
		e.taskRuns[key] = run
		e.pendingOrder = e.pendingOrder[1:]
		delete(e.pending, key)
		e.workers.Add(1)
		go e.executeTask(ctx, *task, run)
	}
}

func (e *Engine) executeTask(ctx context.Context, task Task, run *taskRun) {
	var err error
	defer e.workers.Done()
	defer func() {
		recovered := recover()
		e.mu.Lock()
		failurePhase, previousFailure := run.phase, run.failure
		e.mu.Unlock()
		if ctx.Err() != nil {
			err = safeTaskFailure(failurePhase, ctx.Err())
		} else if recovered != nil {
			err = safeTaskFailure(failurePhase, recovered)
		} else if err != nil && previousFailure != nil {
			err = previousFailure
		}
		run.cancel()
		e.mu.Lock()
		phase, progress := "completed", "本轮处理完成"
		if errors.Is(err, context.Canceled) {
			phase, progress = "cancelled", "本轮已停止"
			if diagnostic, ok := err.(*taskDiagnosticError); ok {
				progress = diagnostic.Error()
			}
		} else if err != nil {
			phase, progress = "failed", err.Error()
		}
		if saveErr := e.updateLocked(func(s *State) error {
			if t := findTask(s, task.ID); t != nil {
				t.NextRun = now() + int64(t.Interval)
				if err != nil && !errors.Is(err, context.Canceled) {
					t.LastError = err.Error()
				}
				level := "info"
				if phase == "failed" {
					level = "error"
				}
				logState(s, task.ID, level, progress)
			}
			return nil
		}); saveErr != nil {
			phase, progress = "failed", "结束状态保存失败，请检查数据库连接"
		}
		delete(e.taskRuns, task.ID)
		e.setTaskStatusLocked(task.ID, phase, progress)
		e.mu.Unlock()
		e.notifyScheduler()
	}()
	err = e.process(ctx, task)
}
