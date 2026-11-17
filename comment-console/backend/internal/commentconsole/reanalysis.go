package commentconsole

import "fmt"

// ReanalysisStatus 记录当前或最近一批候选的处理进度。
type ReanalysisStatus struct {
	Active    bool   `json:"active"`
	Current   string `json:"current,omitempty"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	LastError string `json:"last_error,omitempty"`
}

func (e *Engine) StartReanalysis(ids []string) error {
	if len(ids) == 0 || len(ids) > 50 {
		return fmt.Errorf("每批请选择 1 到 50 条候选帖子")
	}
	keys := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			keys = append(keys, id)
			seen[id] = true
		}
	}
	e.mu.Lock()
	if e.stopping || e.ctx.Err() != nil || e.reanalysis.Active || e.running != "" || len(e.taskRuns) > 0 {
		e.mu.Unlock()
		return fmt.Errorf("有任务正在运行，请等待本轮完成后重试")
	}
	e.reanalysis = ReanalysisStatus{Active: true, Total: len(keys)}
	e.workers.Add(1)
	e.mu.Unlock()
	go e.runReanalysis(keys)
	return nil
}

func (e *Engine) runReanalysis(keys []string) {
	defer func() {
		e.mu.Lock()
		e.reanalysis.Active = false
		e.reanalysis.Current = ""
		e.mu.Unlock()
		e.workers.Done()
		e.notifyScheduler()
	}()
	for _, key := range keys {
		if e.ctx.Err() != nil {
			return
		}
		e.mu.Lock()
		e.reanalysis.Current = key
		e.mu.Unlock()
		err := e.reanalyzeQueuedPost(key)
		e.mu.Lock()
		e.reanalysis.Completed++
		if err != nil {
			e.reanalysis.Failed++
			e.reanalysis.LastError = err.Error()
		}
		e.mu.Unlock()
	}
}

func (e *Engine) reanalyzeQueuedPost(key string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = safeTaskFailure("", value)
		}
	}()
	return e.ReanalyzePost(e.ctx, key)
}
