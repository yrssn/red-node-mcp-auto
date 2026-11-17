package commentconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod"
)

type runtimeTestStore struct {
	mu          sync.Mutex
	state       State
	fail        bool
	failStart   bool
	closed      bool
	closeOnce   sync.Once
	failureOnce sync.Once
	closedCh    chan struct{}
	startFailed chan struct{}
}

func (s *runtimeTestStore) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail || s.closed {
		return errors.New("模拟存储不可用")
	}
	if s.failStart {
		for _, task := range state.Tasks {
			if task.LastRun > 0 {
				s.failureOnce.Do(func() { close(s.startFailed) })
				return errors.New("模拟启动记录保存失败")
			}
		}
	}
	s.state = clone(state)
	return nil
}
func (s *runtimeTestStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeOnce.Do(func() { close(s.closedCh) })
	return nil
}

type runtimeTestBackend struct {
	DemoBackend
	active      atomic.Int32
	peak        atomic.Int32
	accounts    atomic.Int32
	sends       atomic.Int32
	mu          sync.Mutex
	discoveries map[string]int
	sendEntered chan struct{}
	sendRelease chan struct{}
}

func (b *runtimeTestBackend) enterBrowser() func() {
	active := b.active.Add(1)
	for old := b.peak.Load(); active > old; old = b.peak.Load() {
		if b.peak.CompareAndSwap(old, active) {
			break
		}
	}
	return func() { b.active.Add(-1) }
}
func runtimeBrowserDelay(ctx context.Context) error {
	timer := time.NewTimer(3 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (b *runtimeTestBackend) Account(ctx context.Context) (Account, error) {
	done := b.enterBrowser()
	defer done()
	b.accounts.Add(1)
	if err := runtimeBrowserDelay(ctx); err != nil {
		return Account{}, err
	}
	return Account{ID: "runtime-account", LoggedIn: true}, nil
}
func (b *runtimeTestBackend) Discover(ctx context.Context, _ Source, task Task) ([]Post, error) {
	done := b.enterBrowser()
	defer done()
	if err := runtimeBrowserDelay(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.discoveries[task.ID]++
	b.mu.Unlock()
	return []Post{{FeedID: "feed-" + task.ID, Title: "已发现JavaWeb需求 " + task.ID}}, nil
}
func (b *runtimeTestBackend) Detail(ctx context.Context, p Post) (Post, error) {
	done := b.enterBrowser()
	defer done()
	if err := runtimeBrowserDelay(ctx); err != nil {
		return p, err
	}
	p.Content = "JavaWeb 急求帮忙"
	p.Images = []string{"https://ocr-test.xhscdn.com/" + p.TaskID}
	return p, nil
}
func (b *runtimeTestBackend) Send(ctx context.Context, _ Post, _ string) error {
	done := b.enterBrowser()
	defer done()
	b.sends.Add(1)
	if b.sendEntered != nil {
		select {
		case b.sendEntered <- struct{}{}:
		default:
		}
	}
	if b.sendRelease != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.sendRelease:
		}
	}
	return nil
}
func (b *runtimeTestBackend) discoverCount(key string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.discoveries[key]
}

type runtimeTestOCR struct {
	mu           sync.Mutex
	releases     map[string]chan struct{}
	cancelDelays map[string]chan struct{}
	calls        map[string]int
	cancels      map[string]int
	active       atomic.Int32
	peak         atomic.Int32
}

func (*runtimeTestOCR) Health(context.Context) error { return nil }
func (o *runtimeTestOCR) RecognizePost(ctx context.Context, images []string) OCRResult {
	key := strings.TrimPrefix(images[0], "https://ocr-test.xhscdn.com/")
	o.mu.Lock()
	o.calls[key]++
	release := o.releases[key]
	delay := o.cancelDelays[key]
	o.mu.Unlock()
	active := o.active.Add(1)
	for old := o.peak.Load(); active > old; old = o.peak.Load() {
		if o.peak.CompareAndSwap(old, active) {
			break
		}
	}
	defer o.active.Add(-1)
	select {
	case <-ctx.Done():
		o.mu.Lock()
		o.cancels[key]++
		o.mu.Unlock()
		if delay != nil {
			<-delay
		}
		return OCRResult{Status: "failed", Error: "测试任务已取消"}
	case <-release:
		return OCRResult{Status: "success", Text: "JavaWeb 急求帮忙"}
	}
}
func (o *runtimeTestOCR) count(key string) int { o.mu.Lock(); defer o.mu.Unlock(); return o.calls[key] }
func (o *runtimeTestOCR) canceled(key string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cancels[key]
}
func closeRuntimeChannel(ch chan struct{}) {
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}
func (o *runtimeTestOCR) release(key string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	closeRuntimeChannel(o.releases[key])
}
func (o *runtimeTestOCR) releaseAll() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ch := range o.releases {
		closeRuntimeChannel(ch)
	}
	for _, ch := range o.cancelDelays {
		closeRuntimeChannel(ch)
	}
}

func newTaskRuntimeEngine(t *testing.T, count int) (*Engine, *runtimeTestStore, *runtimeTestBackend, *runtimeTestOCR) {
	t.Helper()
	state := EmptyState()
	state.Models = []Model{{ID: "test-model"}}
	state.Strategies = []Strategy{{ID: "runtime-strategy", Name: "只筛选候选", Mode: "ai", Enabled: true, Version: 1, MaxLength: 120}}
	backend := &runtimeTestBackend{discoveries: map[string]int{}}
	ocr := &runtimeTestOCR{releases: map[string]chan struct{}{}, cancelDelays: map[string]chan struct{}{}, calls: map[string]int{}, cancels: map[string]int{}}
	for i := 1; i <= count; i++ {
		key := fmt.Sprintf("runtime-%d", i)
		state.Tasks = append(state.Tasks, Task{ID: key, Name: key, Sources: []Source{{Kind: "keyword", Value: "JavaWeb"}}, StrategyID: "runtime-strategy", AnalysisMode: "model", ModelID: "test-model", MatchKeywords: "JavaWeb", DemandKeywords: "急求", Interval: 60, Limit: 1, SendLimit: 3, Paused: true})
		ocr.releases[key] = make(chan struct{})
	}
	store := &runtimeTestStore{closedCh: make(chan struct{}), startFailed: make(chan struct{})}
	e, err := NewWithStore(store, state, backend, true)
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.ocr = ocr
	e.mu.Unlock()
	t.Cleanup(func() {
		ocr.releaseAll()
		if backend.sendRelease != nil {
			closeRuntimeChannel(backend.sendRelease)
		}
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	return e, store, backend, ocr
}

func waitTaskRuntime(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("等待超时：" + description)
		}
	}
}
func taskRuntimeSnapshot(e *Engine, key string) Task {
	s := e.Snapshot()
	if task := findTask(&s, key); task != nil {
		return *task
	}
	return Task{}
}
func waitTaskRuntimeIdle(t *testing.T, e *Engine, key string) {
	t.Helper()
	waitTaskRuntime(t, key+" 完成本轮", func() bool {
		task := taskRuntimeSnapshot(e, key)
		return !task.Running && !task.Queued && task.Phase != "" && task.Phase != "idle"
	})
}

func TestTaskRuntimeTwoWorkersAPIProgressAndDuplicateRun(t *testing.T) {
	e, store, backend, ocr := newTaskRuntimeEngine(t, 3)
	for i := 1; i <= 3; i++ {
		if err := e.TaskAction(fmt.Sprintf("runtime-%d", i), "run"); err != nil {
			t.Fatal(err)
		}
	}
	e.runNext()
	waitTaskRuntime(t, "两个任务进入OCR", func() bool { return ocr.count("runtime-1") == 1 && ocr.count("runtime-2") == 1 })
	for range 3 {
		if err := e.TaskAction("runtime-1", "run"); err != nil {
			t.Fatal(err)
		}
		if err := e.TaskAction("runtime-3", "run"); err != nil {
			t.Fatal(err)
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	e.Register(router.Group("/api/console"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/console/state", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态API失败：%d %s", recorder.Code, recorder.Body.String())
	}
	var apiState State
	if err := json.Unmarshal(recorder.Body.Bytes(), &apiState); err != nil {
		t.Fatal(err)
	}
	running := 0
	for _, task := range apiState.Tasks {
		if task.Running {
			running++
			if task.Phase == "" || task.Progress == "" {
				t.Fatalf("运行任务缺少进度：%+v", task)
			}
		}
	}
	third := findTask(&apiState, "runtime-3")
	if running != 2 || third == nil || third.Running || !third.Queued || third.Phase != "queued" || third.Progress == "" {
		t.Fatalf("API并发或排队状态错误：%+v", apiState.Tasks)
	}
	if len(apiState.Posts) != 2 {
		t.Fatalf("任务尚在OCR时，已发现帖子应立即可见：%d", len(apiState.Posts))
	}
	for _, p := range apiState.Posts {
		if p.ID == "" || p.TaskID == "" || p.Title == "" || p.Match != nil {
			t.Fatalf("API未暴露已保存但尚在分析的候选：%+v", p)
		}
	}
	e.mu.Lock()
	beforeMemory, _ := json.Marshal(e.state)
	e.mu.Unlock()
	store.mu.Lock()
	beforeStore, _ := json.Marshal(store.state)
	store.mu.Unlock()
	_ = e.Snapshot()
	_ = e.Snapshot()
	e.mu.Lock()
	afterMemory, _ := json.Marshal(e.state)
	raw := clone(e.state)
	e.mu.Unlock()
	store.mu.Lock()
	afterStore, _ := json.Marshal(store.state)
	saved := clone(store.state)
	store.mu.Unlock()
	if !bytes.Equal(beforeMemory, afterMemory) || !bytes.Equal(beforeStore, afterStore) {
		t.Fatal("Snapshot污染了内存或持久化状态")
	}
	for _, snapshot := range []State{raw, saved} {
		for _, task := range snapshot.Tasks {
			if task.Running || task.Queued || task.Phase != "" || task.Progress != "" {
				t.Fatalf("运行态被持久化：%+v", task)
			}
		}
	}
	if backend.peak.Load() != 1 || ocr.peak.Load() != 2 {
		t.Fatalf("应允许2个非浏览器worker、浏览器仍互斥：browser=%d ocr=%d", backend.peak.Load(), ocr.peak.Load())
	}
	ocr.release("runtime-1")
	waitTaskRuntimeIdle(t, e, "runtime-1")
	e.runNext()
	waitTaskRuntime(t, "第三任务获得空闲名额", func() bool { return ocr.count("runtime-3") == 1 })
	ocr.releaseAll()
	for i := 1; i <= 3; i++ {
		waitTaskRuntimeIdle(t, e, fmt.Sprintf("runtime-%d", i))
	}
	for range 3 {
		e.runNext()
	}
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("runtime-%d", i)
		if backend.discoverCount(key) != 1 || ocr.count(key) != 1 {
			t.Fatalf("重复run导致隐式重跑：task=%s discover=%d ocr=%d", key, backend.discoverCount(key), ocr.count(key))
		}
	}
}

func TestTaskRuntimePauseCancelsOnlySelectedWorker(t *testing.T) {
	e, _, _, ocr := newTaskRuntimeEngine(t, 2)
	for _, key := range []string{"runtime-1", "runtime-2"} {
		if err := e.TaskAction(key, "run"); err != nil {
			t.Fatal(err)
		}
	}
	e.runNext()
	waitTaskRuntime(t, "两任务阻塞在OCR", func() bool { return ocr.count("runtime-1") == 1 && ocr.count("runtime-2") == 1 })
	if err := e.TaskAction("runtime-1", "pause"); err != nil {
		t.Fatal(err)
	}
	waitTaskRuntime(t, "指定任务取消", func() bool { return ocr.canceled("runtime-1") == 1 && !taskRuntimeSnapshot(e, "runtime-1").Running })
	if ocr.canceled("runtime-2") != 0 || !taskRuntimeSnapshot(e, "runtime-2").Running {
		t.Fatal("暂停一个任务取消或停止了另一个任务")
	}
	if !taskRuntimeSnapshot(e, "runtime-1").Paused {
		t.Fatal("暂停状态未保存")
	}
	ocr.release("runtime-2")
	waitTaskRuntimeIdle(t, e, "runtime-2")
}

func TestTaskRuntimeCloseWaitsForWorkerBeforeClosingStore(t *testing.T) {
	e, store, _, ocr := newTaskRuntimeEngine(t, 1)
	ocr.mu.Lock()
	delay := make(chan struct{})
	ocr.cancelDelays["runtime-1"] = delay
	ocr.mu.Unlock()
	if err := e.TaskAction("runtime-1", "run"); err != nil {
		t.Fatal(err)
	}
	e.runNext()
	waitTaskRuntime(t, "worker进入OCR", func() bool { return ocr.count("runtime-1") == 1 })
	finished := make(chan error, 1)
	go func() { finished <- e.Close() }()
	waitTaskRuntime(t, "Close取消worker", func() bool { return ocr.canceled("runtime-1") == 1 })
	select {
	case <-finished:
		t.Fatal("worker尚未退出时Close提前返回")
	default:
	}
	select {
	case <-store.closedCh:
		t.Fatal("worker尚未退出时存储被关闭")
	default:
	}
	ocr.mu.Lock()
	closeRuntimeChannel(delay)
	ocr.mu.Unlock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker退出后Close未完成")
	}
	select {
	case <-store.closedCh:
	default:
		t.Fatal("Close完成后未关闭存储")
	}
}

func TestTaskRuntimeStorageFailureNeverStartsWorker(t *testing.T) {
	t.Run("run请求保存失败", func(t *testing.T) {
		e, store, backend, _ := newTaskRuntimeEngine(t, 1)
		store.mu.Lock()
		store.fail = true
		store.mu.Unlock()
		if err := e.TaskAction("runtime-1", "run"); err == nil {
			t.Fatal("存储失败时仍接受运行请求")
		}
		e.runNext()
		task := taskRuntimeSnapshot(e, "runtime-1")
		if task.Running || task.Queued || !task.Paused || backend.accounts.Load() != 0 {
			t.Fatalf("保存失败仍排队或访问账号：%+v calls=%d", task, backend.accounts.Load())
		}
	})
	t.Run("启动记录保存失败", func(t *testing.T) {
		e, store, backend, _ := newTaskRuntimeEngine(t, 1)
		store.mu.Lock()
		store.failStart = true
		store.mu.Unlock()
		if err := e.TaskAction("runtime-1", "run"); err != nil {
			t.Fatal(err)
		}
		e.runNext()
		select {
		case <-store.startFailed:
		case <-time.After(2 * time.Second):
			t.Fatal("未尝试保存启动记录")
		}
		task := taskRuntimeSnapshot(e, "runtime-1")
		if task.Running || backend.accounts.Load() != 0 || backend.discoverCount("runtime-1") != 0 {
			t.Fatalf("启动未落盘仍访问站点：%+v", task)
		}
		if !strings.Contains(task.Progress, "保存失败") {
			t.Fatalf("未暴露启动保存错误：%+v", task)
		}
	})
}

func TestTaskRuntimeSenderRunsIndependentlyAndClaimsOnce(t *testing.T) {
	e, _, backend, ocr := newTaskRuntimeEngine(t, 2)
	backend.sendEntered = make(chan struct{}, 8)
	backend.sendRelease = make(chan struct{})
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = false
		s.Tasks[1].Paused = false
		s.Tasks[1].AccountID = "runtime-account"
		s.Posts = append(s.Posts, Post{ID: "send-post", TaskID: "runtime-2", FeedID: "send-feed"})
		s.Comments = []Comment{{ID: "send-comment", TaskID: "runtime-2", PostID: "send-post", AccountID: "runtime-account", Status: "queued", Text: "这是一条fake发送", Strategy: s.Strategies[0]}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.TaskAction("runtime-1", "run"); err != nil {
		t.Fatal(err)
	}
	e.runNext()
	waitTaskRuntime(t, "处理任务进入OCR", func() bool { return ocr.count("runtime-1") == 1 })
	select {
	case <-backend.sendEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("OCR阻塞期间独立sender未处理另一任务的队列")
	}
	if !taskRuntimeSnapshot(e, "runtime-1").Running {
		t.Fatal("sender未与任务处理独立运行")
	}
	var callers sync.WaitGroup
	for range 5 {
		callers.Add(1)
		go func() { defer callers.Done(); e.sendNext() }()
	}
	closeRuntimeChannel(backend.sendRelease)
	done := make(chan struct{})
	go func() { callers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("并发sendNext未结束")
	}
	waitTaskRuntime(t, "fake发送完成", func() bool { return e.Snapshot().Comments[0].Status == "sent" })
	if backend.sends.Load() != 1 {
		t.Fatalf("同一queued评论被重复提交：%d", backend.sends.Load())
	}
	ocr.releaseAll()
	waitTaskRuntimeIdle(t, e, "runtime-1")
	if backend.peak.Load() != 1 {
		t.Fatalf("sender与任务的浏览器动作未互斥：%d", backend.peak.Load())
	}
}

func TestTaskRuntimeResumeManualTaskDoesNotStartAnotherRound(t *testing.T) {
	e, _, backend, _ := newTaskRuntimeEngine(t, 1)
	if err := e.TaskAction("runtime-1", "resume"); err != nil {
		t.Fatal(err)
	}
	e.runNext()
	task := taskRuntimeSnapshot(e, "runtime-1")
	if task.Paused || task.Running || task.Queued || task.LastRun != 0 || backend.accounts.Load() != 0 || backend.discoverCount(task.ID) != 0 {
		t.Fatalf("手动任务的启用操作意外启动了新一轮：%+v", task)
	}
}

func TestTaskRuntimePausedPendingTaskRejoinsFIFOAtTail(t *testing.T) {
	e, _, _, ocr := newTaskRuntimeEngine(t, 4)
	for i := 1; i <= 4; i++ {
		if err := e.TaskAction(fmt.Sprintf("runtime-%d", i), "run"); err != nil {
			t.Fatal(err)
		}
	}
	e.runNext()
	waitTaskRuntime(t, "两个任务占满名额", func() bool { return ocr.count("runtime-1") == 1 && ocr.count("runtime-2") == 1 })
	if err := e.TaskAction("runtime-3", "pause"); err != nil {
		t.Fatal(err)
	}
	if err := e.TaskAction("runtime-3", "resume"); err != nil {
		t.Fatal(err)
	}
	resumed := taskRuntimeSnapshot(e, "runtime-3")
	if resumed.Queued || resumed.Running || resumed.Phase == "queued" || ocr.count("runtime-3") != 0 {
		t.Fatalf("暂停排队任务后仅启用仍残留排队或触发请求：%+v", resumed)
	}
	if err := e.TaskAction("runtime-3", "run"); err != nil {
		t.Fatal(err)
	}
	ocr.release("runtime-1")
	waitTaskRuntimeIdle(t, e, "runtime-1")
	e.runNext()
	waitTaskRuntime(t, "较早排队的第四任务先启动", func() bool { return ocr.count("runtime-4") > 0 || ocr.count("runtime-3") > 0 })
	if ocr.count("runtime-4") != 1 || ocr.count("runtime-3") != 0 || !taskRuntimeSnapshot(e, "runtime-3").Queued {
		t.Fatal("暂停后重新运行的任务借用旧FIFO位置插队")
	}
	ocr.releaseAll()
	for i := 1; i <= 4; i++ {
		waitTaskRuntimeIdle(t, e, fmt.Sprintf("runtime-%d", i))
	}
}

func TestTaskRuntimeRestartShowsPreviousOutcome(t *testing.T) {
	for _, previousError := range []string{"", "上轮来源获取失败"} {
		t.Run(previousError, func(t *testing.T) {
			state := EmptyState()
			state.Models = []Model{{ID: "test-model"}}
			state.Tasks = []Task{{ID: "previous-task", ModelID: "test-model", LastRun: 123, LastError: previousError, Paused: false, Scheduled: false}}
			store := &runtimeTestStore{closedCh: make(chan struct{}), startFailed: make(chan struct{})}
			e, err := NewWithStore(store, state, DemoBackend{}, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := e.Close(); err != nil {
					t.Error(err)
				}
			})
			task := taskRuntimeSnapshot(e, "previous-task")
			want := "completed"
			if previousError != "" {
				want = "failed"
			}
			if task.Phase != want || task.Progress == "" || strings.Contains(task.Progress, "尚未运行") || task.Running || task.Queued {
				t.Fatalf("重启后丢失上轮执行结果：%+v", task)
			}
		})
	}
}

func TestTaskRuntimeCancelledBrowserPanicIsNotFailure(t *testing.T) {
	e, _, _, _ := newTaskRuntimeEngine(t, 1)
	started := make(chan struct{})
	var discoveryCalls atomic.Int32
	e.backend = fakeBackend{discoverFn: func(ctx context.Context, _ Source, _ Task) ([]Post, error) {
		discoveryCalls.Add(1)
		close(started)
		<-ctx.Done()
		panic(ctx.Err())
	}}
	if err := e.TaskAction("runtime-1", "run"); err != nil {
		t.Fatal(err)
	}
	e.runNext()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("任务未进入浏览器操作")
	}
	if err := e.TaskAction("runtime-1", "pause"); err != nil {
		t.Fatal(err)
	}
	waitTaskRuntimeIdle(t, e, "runtime-1")
	if task := taskRuntimeSnapshot(e, "runtime-1"); task.LastError != "" {
		t.Fatalf("主动取消被当成浏览器失败：%+v", task)
	}
	if err := e.TaskAction("runtime-1", "resume"); err != nil {
		t.Fatal(err)
	}
	task := taskRuntimeSnapshot(e, "runtime-1")
	if task.Phase != "cancelled" || task.Queued || task.Running || discoveryCalls.Load() != 1 {
		t.Fatalf("取消结果丢失或恢复操作导致隐式重跑：%+v", task)
	}
}

type taskOpaqueTimeout struct{ formatted *atomic.Int32 }

func (e taskOpaqueTimeout) Error() string {
	e.formatted.Add(1)
	return "https://example.invalid/?api_key=hidden-api&cookie=hidden-cookie"
}
func (taskOpaqueTimeout) Timeout() bool   { return true }
func (taskOpaqueTimeout) Temporary() bool { return false }

type taskNoFormatError struct{}

func (taskNoFormatError) Error() string { panic("不应格式化原始错误") }

func TestTaskFailureClassificationNeverFormatsRawPanic(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		category string
		cause    error
	}{
		{"wrapped-timeout", fmt.Errorf("private-url hidden-api: %w", context.DeadlineExceeded), "timeout", context.DeadlineExceeded},
		{"rod-timeout", &rod.TryError{Value: context.DeadlineExceeded, Stack: "hidden-cookie hidden-api"}, "timeout", context.DeadlineExceeded},
		{"url-timeout", &url.Error{Op: "Get", URL: "https://example.invalid/?cookie=hidden-cookie", Err: context.DeadlineExceeded}, "timeout", context.DeadlineExceeded},
		{"cancel", fmt.Errorf("hidden-api: %w", context.Canceled), "canceled", context.Canceled},
		{"closed-connection", fmt.Errorf("hidden-cookie: %w", net.ErrClosed), "closed", net.ErrClosed},
		{"closed-eof", fmt.Errorf("hidden-api: %w", io.EOF), "closed", net.ErrClosed},
		{"closed-page", &rod.PageNotFoundError{}, "closed", net.ErrClosed},
		{"opaque-panic", "raw-panic https://example.invalid/?api_key=hidden-api", "unknown", nil},
		{"unformatted-error", taskNoFormatError{}, "unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostic := safeTaskFailure("discover", tt.value)
			if diagnostic.category != tt.category {
				t.Fatalf("错误类别不准确：%s", diagnostic.category)
			}
			if tt.cause != nil && !errors.Is(diagnostic, tt.cause) {
				t.Fatal("安全诊断未保留可判断的标准错误类别")
			}
			message := diagnostic.Error()
			if !strings.Contains(message, "搜索来源") {
				t.Fatalf("错误丢失失败阶段：%s", message)
			}
			for _, secret := range []string{"example.invalid", "hidden-api", "hidden-cookie", "raw-panic"} {
				if strings.Contains(message, secret) {
					t.Fatal("安全诊断泄漏原始错误内容")
				}
			}
		})
	}
	var formatted atomic.Int32
	if d := safeTaskFailure("discover", taskOpaqueTimeout{formatted: &formatted}); d.category != "timeout" || formatted.Load() != 0 {
		t.Fatal("net.Error 超时分类不应格式化其原始错误")
	}
}

func TestTaskFailureMessageKeepsSafeStage(t *testing.T) {
	for stage, label := range map[string]string{
		"account": "核实登录账号", "discover": "搜索来源", "detail": "读取帖子详情",
		"ocr": "识别图片文字", "analyze": "分析帖子内容",
	} {
		t.Run(stage, func(t *testing.T) {
			message := safeTaskFailure(stage, context.DeadlineExceeded).Error()
			if !strings.Contains(message, label) || !strings.Contains(message, "超时") {
				t.Fatalf("阶段诊断不可操作：%s", message)
			}
			if stage != "account" && strings.Contains(message, "登录") {
				t.Fatalf("其他阶段失败被误归因为登录：%s", message)
			}
		})
	}
}

type taskFailureOCR struct{ value any }

func (*taskFailureOCR) Health(context.Context) error { return nil }
func (o *taskFailureOCR) RecognizePost(context.Context, []string) OCRResult {
	panic(o.value)
}

func TestTaskRuntimeFailureStageAndPrivacySurviveWorkerRecovery(t *testing.T) {
	for _, failure := range []string{"account-timeout", "discover-panic", "discover-returned-timeout", "detail-closed", "ocr-panic"} {
		t.Run(failure, func(t *testing.T) {
			e, _, _, _ := newTaskRuntimeEngine(t, 1)
			secretTimeout := fmt.Errorf("https://example.invalid/?api_key=hidden-api cookie=hidden-cookie: %w", context.DeadlineExceeded)
			backend := fakeBackend{
				discoverFn: func(context.Context, Source, Task) ([]Post, error) { return []Post{{FeedID: "diagnostic-feed"}}, nil },
				detailFn:   func(_ context.Context, p Post) (Post, error) { p.Content = "JavaWeb 急求"; return p, nil },
			}
			wantStage, wantCategory := "", ""
			switch failure {
			case "account-timeout":
				wantStage, wantCategory = "核实登录账号", "超时"
				backend.accountFn = func(context.Context) (Account, error) { return Account{}, secretTimeout }
			case "discover-panic":
				wantStage, wantCategory = "搜索来源", "超时"
				backend.discoverFn = func(context.Context, Source, Task) ([]Post, error) {
					panic(&rod.TryError{Value: secretTimeout, Stack: "hidden-cookie raw-panic"})
				}
			case "discover-returned-timeout":
				wantStage, wantCategory = "搜索来源", "超时"
				backend.discoverFn = func(context.Context, Source, Task) ([]Post, error) { return nil, secretTimeout }
			case "detail-closed":
				wantStage, wantCategory = "读取帖子详情", "已关闭"
				backend.detailFn = func(context.Context, Post) (Post, error) {
					panic(fmt.Errorf("hidden-api: %w", io.EOF))
				}
			case "ocr-panic":
				wantStage, wantCategory = "识别图片文字", "超时"
				backend.detailFn = func(_ context.Context, p Post) (Post, error) {
					p.Images = []string{"https://safe.xhscdn.com/test-image"}
					return p, nil
				}
				e.ocr = &taskFailureOCR{value: secretTimeout}
			}
			e.backend = backend
			if err := e.TaskAction("runtime-1", "run"); err != nil {
				t.Fatal(err)
			}
			e.runNext()
			waitTaskRuntimeIdle(t, e, "runtime-1")
			snapshot := e.Snapshot()
			task := snapshot.Tasks[0]
			if task.Phase != "failed" || !strings.Contains(task.Progress, wantStage) || !strings.Contains(task.Progress, wantCategory) ||
				!strings.Contains(task.LastError, wantStage) || !strings.Contains(task.LastError, wantCategory) {
				t.Fatalf("失败阶段或类别被泛化：%+v", task)
			}
			messages := task.Progress + "\n" + task.LastError
			for _, log := range snapshot.Logs {
				messages += "\n" + log.Message
			}
			for _, secret := range []string{"example.invalid", "hidden-api", "hidden-cookie", "raw-panic"} {
				if strings.Contains(messages, secret) {
					t.Fatalf("任务错误或日志泄漏原始异常：%s", secret)
				}
			}
		})
	}
}
