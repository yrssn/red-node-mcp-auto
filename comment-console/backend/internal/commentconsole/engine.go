package commentconsole

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	mu             sync.Mutex
	browserMu      sync.Mutex
	sendMu         sync.Mutex
	store          StateStore
	state          State
	backend        Backend
	demo           bool
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	pending        map[string]bool
	pendingOrder   []string
	taskRuns       map[string]*taskRun
	taskStatuses   map[string]taskRuntimeStatus
	workers        sync.WaitGroup
	wake           chan struct{}
	stopping       bool
	running        string
	runCancel      context.CancelFunc
	lastSend       time.Time
	ocr            OCRClient
	reanalysisPost string
	accountCache   *accountStatusCache
	queueNotes     map[string]string
	checkingSend   string
}

func id() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func now() int64 { return time.Now().Unix() }

func New(dsn string, backend Backend, demo bool) (*Engine, error) {
	s, state, err := OpenStore(dsn)
	if err != nil {
		return nil, err
	}
	return NewWithStore(s, state, backend, demo)
}

// NewWithStore 接管存储生命周期，供集成测试注入无外部依赖的实现。
func NewWithStore(s StateStore, initial State, backend Backend, demo bool) (*Engine, error) {
	if s == nil || backend == nil {
		return nil, fmt.Errorf("存储和站点后端不能为空")
	}
	state := clone(initial)
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{store: s, state: state, backend: backend, demo: demo, ctx: ctx, cancel: cancel, done: make(chan struct{}), pending: map[string]bool{}, wake: make(chan struct{}, 1)}
	// 重启后不重发中断的请求，需人工核实。
	for i := range state.Comments {
		if state.Comments[i].Status == "sending" {
			state.Comments[i].Status = "uncertain"
			state.Comments[i].Error = "服务在发送期间重启，请先在原帖核实"
		}
	}
	for i := range state.Tasks {
		if state.Tasks[i].AnalysisMode == "rules" || state.Tasks[i].AnalysisMode == "hybrid" {
			state.Tasks[i].AutoSend = false
		}
		state.Tasks[i].AnalysisMode = "model"
		if findModel(&state, state.Tasks[i].ModelID) == nil {
			state.Tasks[i].Paused = true
		}
		state.Tasks[i].Running = false
		state.Tasks[i].Queued, state.Tasks[i].Phase, state.Tasks[i].Progress = false, "", ""
	}
	for i := range state.Posts {
		p := &state.Posts[i]
		// 清理旧规则产生的结果，保留独立的模型判断和 OCR 状态。
		if p.DecisionStatus == "closed" || (p.DemandClosed && p.DecisionStatus == "review" && p.Match != nil && *p.Match) {
			p.DecisionStatus, p.Reason, p.AnalysisMethod, p.Match = "", "", "", nil
		}
		p.DemandClosed = false
	}
	state.Settings.SendPaused = true
	if err := s.Save(state); err != nil {
		s.Close()
		cancel()
		return nil, err
	}
	e.state = state
	go e.loop()
	return e, nil
}

// Stop 停止接收新一轮任务，已提交的评论仍需等待结果。
func (e *Engine) Stop() {
	e.cancel()
	e.mu.Lock()
	e.stopping = true
	e.cancelTasksLocked()
	e.mu.Unlock()
}
func (e *Engine) Close() error { e.Stop(); <-e.done; return e.store.Close() }

func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := clone(e.state)
	s.Demo = e.demo
	s.Version = Version
	for i := range s.Models {
		s.Models[i].HasKey = s.Models[i].APIKey != ""
		s.Models[i].APIKey = ""
	}
	for i := range s.Tasks {
		e.taskSnapshotLocked(&s.Tasks[i])
	}
	for i := range s.Comments {
		s.Comments[i].QueueReason, s.Comments[i].QueueBlocked = "", false
		if s.Comments[i].Status == "queued" {
			s.Comments[i].QueueReason, s.Comments[i].QueueBlocked = e.queueStatusLocked(&s, s.Comments[i])
		}
	}
	return s
}

func (e *Engine) update(fn func(*State) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.updateLocked(fn)
}

func (e *Engine) updateLocked(fn func(*State) error) error {
	s := clone(e.state)
	if err := fn(&s); err != nil {
		return err
	}
	if len(s.Logs) > 1000 {
		s.Logs = s.Logs[len(s.Logs)-1000:]
	}
	// 运行进度只在内存展示，重启后不恢复执行中的任务。
	for i := range s.Tasks {
		s.Tasks[i].Running = false
		s.Tasks[i].Queued, s.Tasks[i].Phase, s.Tasks[i].Progress = false, "", ""
	}
	if err := e.store.Save(s); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	e.state = s
	return nil
}

func logState(s *State, task, level, message string) {
	s.Logs = append(s.Logs, Log{At: now(), TaskID: task, Level: level, Message: message})
}
func (e *Engine) log(task, level, message string) {
	_ = e.update(func(s *State) error { logState(s, task, level, message); return nil })
}
func findTask(s *State, key string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].ID == key {
			return &s.Tasks[i]
		}
	}
	return nil
}
func findPost(s *State, key string) *Post {
	for i := range s.Posts {
		if s.Posts[i].ID == key {
			return &s.Posts[i]
		}
	}
	return nil
}
func findComment(s *State, key string) *Comment {
	for i := range s.Comments {
		if s.Comments[i].ID == key {
			return &s.Comments[i]
		}
	}
	return nil
}
func findStrategy(s *State, key string) *Strategy {
	for i := range s.Strategies {
		if s.Strategies[i].ID == key {
			return &s.Strategies[i]
		}
	}
	return nil
}
func findModel(s *State, key string) *Model {
	for i := range s.Models {
		if s.Models[i].ID == key {
			return &s.Models[i]
		}
	}
	return nil
}

func (e *Engine) SaveModel(m Model) error {
	if err := validateModel(m); err != nil {
		return err
	}
	return e.update(func(s *State) error {
		if old := findModel(s, m.ID); old != nil {
			if m.APIKey == "" {
				m.APIKey = old.APIKey
			}
			*old = m
			return nil
		}
		if m.ID != "" {
			return fmt.Errorf("模型不存在，请刷新后重试")
		}
		m.ID = id()
		s.Models = append(s.Models, m)
		return nil
	})
}

func (e *Engine) SaveStrategy(v Strategy) error {
	if strings.TrimSpace(v.Name) == "" || v.MaxLength < 1 || v.MaxLength > 500 || (v.Mode != "ai" && v.Mode != "fixed") {
		return fmt.Errorf("请填写策略名称、生成方式和 1–500 字的长度限制")
	}
	if v.Mode == "fixed" {
		if err := validateText(v.FixedText, v); err != nil {
			return err
		}
	}
	return e.update(func(s *State) error {
		if old := findStrategy(s, v.ID); old != nil {
			v.Version = old.Version + 1
			*old = v
			return nil
		}
		if v.ID != "" {
			return fmt.Errorf("策略不存在，请刷新后重试")
		}
		v.ID = id()
		v.Version = 1
		s.Strategies = append(s.Strategies, v)
		return nil
	})
}

func (e *Engine) SaveMaterial(v Material) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Content) == "" {
		return fmt.Errorf("素材名称和内容不能为空")
	}
	if err := validateMaterialImage(v.Image); err != nil {
		return err
	}
	return e.update(func(s *State) error {
		for i := range s.Materials {
			if s.Materials[i].ID == v.ID {
				s.Materials[i] = v
				return nil
			}
		}
		if v.ID != "" {
			return fmt.Errorf("素材不存在，请刷新后重试")
		}
		v.ID = id()
		s.Materials = append(s.Materials, v)
		return nil
	})
}

func (e *Engine) SaveTask(t Task) error {
	if t.AnalysisMode != "" && !contains([]string{"rules", "hybrid", "model"}, t.AnalysisMode) {
		return fmt.Errorf("分析方式无效")
	}
	if t.AnalysisMode == "rules" || t.AnalysisMode == "hybrid" {
		t.AutoSend = false
	}
	t.AnalysisMode = "model"
	if strings.TrimSpace(t.Name) == "" || len(t.Sources) == 0 || len(t.Sources) > 20 || t.Limit < 1 || t.Limit > 50 || t.SendLimit < 1 || t.SendLimit > 50 || t.Interval < 60 || t.Interval > 86400 {
		return fmt.Errorf("任务名称和来源必填；每轮获取/发送 1–50 条，刷新间隔至少 60 秒")
	}
	if t.PublishTime != "" && !contains([]string{"不限", "一天内", "一周内", "半年内"}, t.PublishTime) {
		return fmt.Errorf("发布时间筛选无效")
	}
	if t.NoteType != "" && !contains([]string{"不限", "视频", "图文"}, t.NoteType) {
		return fmt.Errorf("帖子类型筛选无效")
	}
	for _, src := range t.Sources {
		if !contains([]string{"keyword", "tag", "profile", "link"}, src.Kind) || strings.TrimSpace(src.Value) == "" {
			return fmt.Errorf("来源须为关键词、话题词、主页或帖子链接")
		}
	}
	return e.update(func(s *State) error {
		if (t.ModelID != "" && findModel(s, t.ModelID) == nil) || findStrategy(s, t.StrategyID) == nil {
			return fmt.Errorf("请选择有效模型和评论策略；暂停的任务可暂不配置模型")
		}
		if old := findTask(s, t.ID); old != nil {
			if e.taskRunningLocked(t.ID) || e.pending[t.ID] {
				return fmt.Errorf("任务运行中，请先暂停并等待本轮结束后编辑")
			}
			if !old.Paused && t.ModelID == "" {
				return fmt.Errorf("请先暂停任务，再移除模型配置")
			}
			t.AccountID = old.AccountID
			t.SendCount = old.SendCount
			t.LastRun = old.LastRun
			t.NextRun = old.NextRun
			t.LastError = old.LastError
			t.Paused = old.Paused
			*old = t
			return nil
		}
		if t.ID != "" {
			return fmt.Errorf("任务不存在，请刷新后重试")
		}
		t.ID = id()
		t.Paused = true
		t.AccountID = ""
		t.SendCount = 0
		t.LastRun = 0
		t.NextRun = 0
		t.LastError = ""
		t.Running = false
		s.Tasks = append(s.Tasks, t)
		return nil
	})
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func taskResourcesReady(s *State, t Task) error {
	if findModel(s, t.ModelID) == nil {
		return fmt.Errorf("请先为任务配置有效模型，再运行")
	}
	strategy := findStrategy(s, t.StrategyID)
	if strategy == nil || !strategy.Enabled {
		return fmt.Errorf("请先为任务选择已启用的评论策略，再运行")
	}
	return nil
}

func (e *Engine) TaskAction(key, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if action == "run" || action == "resume" {
		if e.stopping || e.ctx.Err() != nil {
			return fmt.Errorf("服务正在停止，无法启动任务")
		}
		if e.taskRunningLocked(key) || e.pending[key] {
			return nil
		}
	}
	err := e.updateLocked(func(s *State) error {
		t := findTask(s, key)
		if t == nil {
			return fmt.Errorf("任务不存在")
		}
		switch action {
		case "run", "resume":
			if err := taskResourcesReady(s, *t); err != nil {
				return err
			}
			t.Paused = false
			t.NextRun = now()
		case "pause":
			t.Paused = true
		case "collect":
			t.CollectPaused = !t.CollectPaused
		case "analyze":
			t.AnalyzePaused = !t.AnalyzePaused
		case "send":
			t.SendPaused = !t.SendPaused
		default:
			return fmt.Errorf("未知任务操作")
		}
		logState(s, key, "info", "任务操作："+action)
		return nil
	})
	if err == nil {
		if action == "run" {
			e.enqueueTaskLocked(key)
		}
		if action == "run" || action == "resume" {
			e.notifyScheduler()
		}
		if action == "pause" {
			e.removePendingLocked(key)
			if run := e.taskRuns[key]; run != nil {
				run.cancel()
			}
			if e.running == key && e.runCancel != nil {
				e.runCancel()
			}
		}
	}
	return err
}

func (e *Engine) Delete(kind, key string) error {
	return e.update(func(s *State) error {
		for _, t := range s.Tasks {
			if (kind == "models" && t.ModelID == key) || (kind == "strategies" && t.StrategyID == key) {
				return fmt.Errorf("仍被任务引用，请先修改任务")
			}
		}
		switch kind {
		case "models":
			for i, v := range s.Models {
				if v.ID == key {
					s.Models = append(s.Models[:i], s.Models[i+1:]...)
					return nil
				}
			}
		case "strategies":
			for i, v := range s.Strategies {
				if v.ID == key {
					s.Strategies = append(s.Strategies[:i], s.Strategies[i+1:]...)
					return nil
				}
			}
		case "materials":
			for i, v := range s.Materials {
				if v.ID == key {
					s.Materials = append(s.Materials[:i], s.Materials[i+1:]...)
					return nil
				}
			}
		case "tasks":
			for i, v := range s.Tasks {
				if v.ID == key {
					if !v.Paused || e.taskRunningLocked(key) || e.pending[key] {
						return fmt.Errorf("请先暂停任务并等待本轮结束")
					}
					for j := range s.Comments {
						c := &s.Comments[j]
						if c.TaskID == key && contains([]string{"draft", "review", "queued", "failed"}, c.Status) {
							c.Status = "cancelled"
							c.History = append(c.History, Revision{now(), "任务删除", c.Text})
						}
					}
					s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
					return nil
				}
			}
		default:
			return fmt.Errorf("未知资源")
		}
		return fmt.Errorf("记录不存在")
	})
}

func (e *Engine) CommentAction(key, action, text string) error {
	return e.update(func(s *State) error {
		c := findComment(s, key)
		if c == nil {
			return fmt.Errorf("评论不存在")
		}
		if e.reanalysisPost != "" && c.PostID == e.reanalysisPost {
			return fmt.Errorf("该帖子正在重新分析，请等待完成后再操作评论")
		}
		if c.Status == "sending" {
			return fmt.Errorf("评论正在发送，暂不能修改")
		}
		if c.Status == "sent" {
			return fmt.Errorf("已发送评论仅保留记录，不能修改或重发")
		}
		switch action {
		case "edit":
			if !contains([]string{"draft", "review", "queued", "failed", "skipped", "cancelled"}, c.Status) {
				return fmt.Errorf("当前状态不能编辑")
			}
			if err := validateText(text, c.Strategy); err != nil {
				return err
			}
			c.Text = strings.TrimSpace(text)
			c.Status = "review"
		case "approve":
			if !contains([]string{"draft", "review", "failed"}, c.Status) {
				return fmt.Errorf("当前状态不能审核")
			}
			if err := validateText(c.Text, c.Strategy); err != nil {
				return err
			}
			if findTask(s, c.TaskID) == nil {
				return fmt.Errorf("任务已删除")
			}
			c.Status = "queued"
			c.Error = ""
		case "skip", "cancel":
			if c.Status == "uncertain" {
				return fmt.Errorf("请先核实发送结果")
			}
			if action == "skip" {
				c.Status = "skipped"
			} else {
				c.Status = "cancelled"
			}
		case "confirm_sent":
			if c.Status != "uncertain" {
				return fmt.Errorf("仅待确认记录可核实")
			}
			c.Status = "sent"
			c.SentText = c.Text
			c.SentAt = now()
			c.Error = ""
		case "confirm_failed":
			if c.Status != "uncertain" {
				return fmt.Errorf("仅待确认记录可核实")
			}
			c.Status = "failed"
			c.Error = "人工核实未发送，可重新审核入队"
		default:
			return fmt.Errorf("未知评论操作")
		}
		c.UpdatedAt = now()
		c.History = append(c.History, Revision{now(), action, c.Text})
		logState(s, c.TaskID, "info", "评论操作："+action)
		return nil
	})
}

func (e *Engine) ModelTest(ctx context.Context, key string) error {
	e.mu.Lock()
	m := findModel(&e.state, key)
	if m == nil {
		e.mu.Unlock()
		return fmt.Errorf("模型不存在")
	}
	copy := *m
	e.mu.Unlock()
	if e.demo {
		return nil
	}
	_, err := chat(ctx, copy, "请简短回复连接成功", map[string]string{"message": "连接测试"})
	return err
}

func (e *Engine) Regenerate(ctx context.Context, key, instruction string) error {
	e.mu.Lock()
	s := clone(e.state)
	busyPost := e.reanalysisPost
	e.mu.Unlock()
	c := findComment(&s, key)
	if c == nil {
		return fmt.Errorf("评论不存在")
	}
	if busyPost != "" && c.PostID == busyPost {
		return fmt.Errorf("该帖子正在重新分析，请等待完成")
	}
	if !contains([]string{"draft", "review", "failed", "skipped", "cancelled"}, c.Status) {
		return fmt.Errorf("请先取消排队；仅草稿、待审核或失败评论可重新生成")
	}
	t := findTask(&s, c.TaskID)
	p := findPost(&s, c.PostID)
	if t == nil || p == nil {
		return fmt.Errorf("任务或帖子不存在")
	}
	st := findStrategy(&s, t.StrategyID)
	m := findModel(&s, t.ModelID)
	if st == nil || !st.Enabled || m == nil {
		return fmt.Errorf("模型或策略不可用")
	}
	d, err := e.decide(ctx, *m, *t, *p, *st, s.Materials, instruction)
	if err != nil {
		return err
	}
	if !d.Match {
		return fmt.Errorf("重新分析后不匹配：%s", d.Reason)
	}
	return e.update(func(s *State) error {
		cur := findComment(s, key)
		latestPost := findPost(s, c.PostID)
		if e.reanalysisPost == c.PostID || latestPost == nil || latestPost.Reason != p.Reason || latestPost.OCRText != p.OCRText || latestPost.Content != p.Content || latestPost.AuthorUpdates != p.AuthorUpdates {
			return fmt.Errorf("帖子已变更或正在重新分析，请刷新后重试")
		}
		if cur == nil || cur.Text != c.Text || cur.Status != c.Status || len(cur.History) != len(c.History) {
			return fmt.Errorf("评论已变更，请刷新后重试")
		}
		cur.Text = d.Comment
		if len(cur.StrategyVersions) == 0 {
			cur.StrategyVersions = append(cur.StrategyVersions, cur.Strategy)
		}
		cur.StrategyVersions = append(cur.StrategyVersions, *st)
		cur.Strategy = *st
		cur.Status = "review"
		cur.Error = ""
		cur.UpdatedAt = now()
		cur.History = append(cur.History, Revision{now(), "重新生成：" + instruction, d.Comment})
		if p := findPost(s, cur.PostID); p != nil {
			p.Reason = d.Reason
		}
		return nil
	})
}

func (e *Engine) decide(ctx context.Context, m Model, t Task, p Post, st Strategy, materials []Material, instruction string) (Decision, error) {
	if e.demo {
		text := "你提到的这个问题很具体，可以先把预算和使用需求列出来，再逐项比较。"
		if st.Mode == "fixed" {
			text = st.FixedText
		}
		if err := validateText(text, st); err != nil {
			return Decision{}, err
		}
		return Decision{Match: true, Reason: "演示分析：该帖子包含明确的问题和交流需求。", Comment: text}, nil
	}
	return analyze(ctx, m, t, p, st, materials, instruction)
}

func (e *Engine) currentTask(key string) (Task, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := findTask(&e.state, key)
	if t == nil {
		return Task{}, false
	}
	return *t, true
}
func (e *Engine) account(ctx context.Context) (Account, error) {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	return e.accountLiveLocked(ctx)
}

// WithBrowser 将旧接口与控制台任务串行化，防止同账号同时操作。
func (e *Engine) WithBrowser(fn func()) {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	fn()
}

func (e *Engine) process(ctx context.Context, task Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	err := taskResourcesReady(&e.state, task)
	e.mu.Unlock()
	if err != nil {
		return err
	}
	var account Account
	err = e.taskBrowser(ctx, task.ID, "account", "核实登录账号", func() error {
		var checkErr error
		account, checkErr = e.accountLiveLocked(ctx)
		return checkErr
	})
	if err != nil {
		return err
	}
	if !account.LoggedIn || account.ID == "" {
		return fmt.Errorf("请先登录并确认账号身份")
	}
	if task.AccountID != "" && task.AccountID != account.ID {
		return fmt.Errorf("当前账号与任务绑定账号不一致，请使用原账号或新建任务")
	}
	if err = e.update(func(s *State) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t := findTask(s, task.ID); t != nil {
			t.AccountID = account.ID
		}
		return nil
	}); err != nil {
		return err
	}
	if !task.CollectPaused {
		count := 0
		sourceOK := 0
		var sourceError error
		for sourceIndex, src := range task.Sources {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			cur, ok := e.currentTask(task.ID)
			if !ok || cur.Paused || cur.CollectPaused {
				break
			}
			var posts []Post
			progress := fmt.Sprintf("来源 %d/%d：%s，已新增 %d/%d 帖", sourceIndex+1, len(task.Sources), src.Value, count, task.Limit)
			discoverErr := e.taskBrowser(ctx, task.ID, "discover", progress, func() error {
				var discoverErr error
				posts, discoverErr = e.backend.Discover(ctx, src, task)
				return discoverErr
			})
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if discoverErr != nil {
				e.log(task.ID, "error", "来源获取失败："+discoverErr.Error())
				sourceError = discoverErr
				continue
			}
			sourceOK++
			for _, p := range posts {
				if count >= task.Limit {
					break
				}
				if p.FeedID == "" {
					continue
				}
				p.ID = id()
				p.TaskID = task.ID
				p.CreatedAt = now()
				p.Source = src.Kind + ":" + src.Value
				added := false
				err = e.update(func(s *State) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					for _, old := range s.Posts {
						if old.TaskID == task.ID && old.FeedID == p.FeedID {
							return nil
						}
					}
					s.Posts = append(s.Posts, p)
					added = true
					return nil
				})
				if err != nil {
					return err
				}
				if added {
					count++
					e.setTaskProgress(task.ID, "discover", fmt.Sprintf("来源 %d/%d：%s，已新增 %d/%d 帖", sourceIndex+1, len(task.Sources), src.Value, count, task.Limit))
				}
			}
			if count >= task.Limit {
				break
			}
		}
		if sourceOK == 0 && sourceError != nil {
			return fmt.Errorf("所有找帖来源均失败，请检查链接、登录状态和运行日志")
		}
	}
	e.mu.Lock()
	snapshot := clone(e.state)
	e.mu.Unlock()
	strategy := findStrategy(&snapshot, task.StrategyID)
	model := findModel(&snapshot, task.ModelID)
	if strategy == nil || model == nil || !strategy.Enabled {
		return fmt.Errorf("评论策略已停用或模型不存在")
	}
	total := 0
	for _, post := range snapshot.Posts {
		if post.TaskID == task.ID && post.Match == nil && total < task.Limit {
			total++
		}
	}
	processed := 0
	queued := 0
	analyzed := 0
	for _, post := range snapshot.Posts {
		if post.TaskID != task.ID || post.Match != nil {
			continue
		}
		if processed >= task.Limit {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cur, ok := e.currentTask(task.ID)
		if !ok || cur.Paused || cur.AnalyzePaused {
			break
		}
		processed++
		progress := fmt.Sprintf("第 %d/%d 帖：%s", processed, total, post.Title)
		p, d, detailErr := e.readPostDecision(ctx, task, post, *strategy, model, snapshot.Materials, progress)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if detailErr != nil {
			e.log(task.ID, "error", "读取帖子详情失败："+detailErr.Error())
			continue
		}
		p.Match = &d.Match
		p.Reason = d.Reason
		analyzed++
		err = e.update(func(s *State) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			cur := findTask(s, task.ID)
			if cur == nil {
				return fmt.Errorf("任务不存在")
			}
			if old := findPost(s, p.ID); old != nil {
				*old = p
			}
			if !d.Match || d.Status != "matched" || strings.TrimSpace(d.Comment) == "" {
				return nil
			}
			for _, c := range s.Comments {
				old := findPost(s, c.PostID)
				if old != nil && old.FeedID == p.FeedID && c.AccountID == account.ID {
					return nil
				}
			}
			status := "review"
			if task.AutoSend && p.AnalysisMethod == "model" && !cur.Paused && !cur.SendPaused && queued < task.SendLimit {
				status = "queued"
				queued++
			}
			c := Comment{ID: id(), TaskID: task.ID, PostID: p.ID, AccountID: account.ID, Original: d.Comment, Text: d.Comment, Status: status, Strategy: *strategy, StrategyVersions: []Strategy{*strategy}, CreatedAt: now(), UpdatedAt: now(), History: []Revision{{now(), "生成草稿", d.Comment}}}
			s.Comments = append(s.Comments, c)
			return nil
		})
		if err != nil {
			return err
		}
	}
	if processed > 0 && analyzed == 0 && ctx.Err() == nil {
		return fmt.Errorf("本轮帖子详情或 AI 分析均未成功，请查看运行日志")
	}
	return ctx.Err()
}

func (e *Engine) sendNext() {
	if !e.sendMu.TryLock() {
		return
	}
	defer e.sendMu.Unlock()
	if e.ctx.Err() != nil {
		return
	}
	e.mu.Lock()
	if e.running != "" || e.state.Settings.SendPaused || time.Since(e.lastSend) < time.Duration(e.state.Settings.SendInterval)*time.Second {
		e.mu.Unlock()
		return
	}
	s := clone(e.state)
	busyTasks := map[string]bool{}
	for key := range e.taskRuns {
		busyTasks[key] = true
	}
	e.mu.Unlock()
	for _, c := range s.Comments {
		if c.Status != "queued" {
			continue
		}
		t := findTask(&s, c.TaskID)
		p := findPost(&s, c.PostID)
		if t == nil || p == nil || t.Paused || t.SendPaused || t.SendCount >= t.SendLimit || busyTasks[t.ID] {
			continue
		}
		e.send(c, *p)
		return
	}
}

func (e *Engine) send(c Comment, p Post) {
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)
	defer cancel()
	e.mu.Lock()
	e.checkingSend = c.ID
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.checkingSend == c.ID {
			e.checkingSend = ""
		}
		e.mu.Unlock()
	}()
	defer func() {
		if r := recover(); r != nil {
			_ = e.update(func(s *State) error {
				if cur := findComment(s, c.ID); cur != nil && cur.Status == "sending" {
					cur.Status = "uncertain"
					cur.Error = "发送期间异常，请在原帖核实"
				}
				return nil
			})
			e.setQueueNote(c.ID, "发送流程发生异常，请先检查状态；尚未确认提交的留言不会自动重发")
		}
	}()
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	a, err := e.accountLiveLocked(ctx)
	if err != nil || !a.LoggedIn || a.ID != c.AccountID {
		message := "登录状态未通过实时核验，留言尚未提交，请检查账号"
		if err != nil {
			message = "账号检查暂时失败，留言尚未提交；请重新检查登录状态"
		} else if a.LoggedIn && a.ID != c.AccountID {
			message = "当前账号与评论绑定账号不同，留言尚未提交"
		}
		e.setQueueNote(c.ID, message)
		_ = e.update(func(s *State) error {
			s.Settings.SendPaused = true
			logState(s, c.TaskID, "error", "发送已暂停："+message)
			return nil
		})
		return
	}
	err = e.update(func(s *State) error {
		cur := findComment(s, c.ID)
		t := findTask(s, c.TaskID)
		if cur == nil || cur.Status != "queued" || cur.Text != c.Text || t == nil || t.Paused || t.SendPaused || s.Settings.SendPaused || t.SendCount >= t.SendLimit || ctx.Err() != nil || e.reanalysisPost == c.PostID || e.running != "" || e.taskRuns[c.TaskID] != nil {
			return fmt.Errorf("队列状态已变化")
		}
		st := findStrategy(s, t.StrategyID)
		if st == nil || !st.Enabled {
			cur.Status = "failed"
			cur.Error = "策略已停用"
			return nil
		}
		if err := validateText(cur.Text, cur.Strategy); err != nil {
			cur.Status = "failed"
			cur.Error = err.Error()
			return nil
		}
		for _, other := range s.Comments {
			op := findPost(s, other.PostID)
			if other.ID != cur.ID && other.AccountID == cur.AccountID && op != nil && op.FeedID == p.FeedID && contains([]string{"sent", "sending", "uncertain"}, other.Status) {
				cur.Status = "failed"
				cur.Error = "该账号已处理过同一帖子"
				return nil
			}
		}
		cur.Status = "sending"
		t.SendCount++
		cur.UpdatedAt = now()
		cur.History = append(cur.History, Revision{now(), "开始发送", cur.Text})
		return nil
	})
	if err != nil {
		e.setQueueNote(c.ID, "尚未提交：队列状态变化或保存失败，请检查任务状态与数据库连接")
		return
	}
	e.mu.Lock()
	cur := findComment(&e.state, c.ID)
	claimed := cur != nil && cur.Status == "sending"
	e.mu.Unlock()
	if !claimed {
		return
	}
	e.setQueueNote(c.ID, "")
	e.mu.Lock()
	e.lastSend = time.Now()
	e.mu.Unlock()
	err = e.backend.Send(ctx, p, c.Text)
	_ = e.update(func(s *State) error {
		cur := findComment(s, c.ID)
		if cur == nil {
			return fmt.Errorf("评论不存在")
		}
		cur.UpdatedAt = now()
		if err != nil {
			cur.Status = "uncertain"
			cur.Error = "未确认发送结果，请在原帖核实后处理"
			logState(s, c.TaskID, "error", cur.Error)
		} else {
			cur.Status = "sent"
			cur.SentAt = now()
			cur.SentText = c.Text
			cur.Error = ""
			logState(s, c.TaskID, "info", "评论发送成功")
		}
		cur.History = append(cur.History, Revision{now(), cur.Status, c.Text})
		return nil
	})
}
