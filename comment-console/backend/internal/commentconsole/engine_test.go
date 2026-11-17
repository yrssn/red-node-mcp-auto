package commentconsole

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fakeBackend struct {
	DemoBackend
	accountFn  func(context.Context) (Account, error)
	discoverFn func(context.Context, Source, Task) ([]Post, error)
	detailFn   func(context.Context, Post) (Post, error)
	sendFn     func(context.Context, Post, string) error
}

type memoryStore struct {
	mu     sync.Mutex
	state  State
	closed bool
}

func (s *memoryStore) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("测试存储已关闭")
	}
	s.state = clone(state)
	return nil
}
func (s *memoryStore) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }

func (f fakeBackend) Account(ctx context.Context) (Account, error) {
	if f.accountFn != nil {
		return f.accountFn(ctx)
	}
	return f.DemoBackend.Account(ctx)
}
func (f fakeBackend) Discover(ctx context.Context, s Source, t Task) ([]Post, error) {
	if f.discoverFn != nil {
		return f.discoverFn(ctx, s, t)
	}
	return f.DemoBackend.Discover(ctx, s, t)
}
func (f fakeBackend) Send(ctx context.Context, p Post, text string) error {
	if f.sendFn != nil {
		return f.sendFn(ctx, p, text)
	}
	return nil
}
func (f fakeBackend) Detail(ctx context.Context, p Post) (Post, error) {
	if f.detailFn != nil {
		return f.detailFn(ctx, p)
	}
	return f.DemoBackend.Detail(ctx, p)
}

func testEngine(t *testing.T, backend Backend) *Engine {
	t.Helper()
	state := EmptyState()
	store := &memoryStore{state: state}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{store: store, state: state, backend: backend, demo: true, ctx: ctx, cancel: cancel, pending: map[string]bool{}}
	t.Cleanup(func() { cancel(); _ = store.Close() })
	if err := e.SeedDemo(); err != nil {
		t.Fatal(err)
	}
	if err := e.update(func(s *State) error { s.Tasks[0].Paused = false; return nil }); err != nil {
		t.Fatal(err)
	}
	return e
}

func runProcess(t *testing.T, e *Engine) {
	t.Helper()
	snapshot := e.Snapshot()
	if err := e.process(context.Background(), snapshot.Tasks[0]); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryDeduplicatesAcrossSourcesAndTasks(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	runProcess(t, e)
	runProcess(t, e)
	s := e.Snapshot()
	if len(s.Posts) != 3 || len(s.Comments) != 3 {
		t.Fatalf("duplicates: %d posts, %d comments", len(s.Posts), len(s.Comments))
	}
	if err := e.update(func(s *State) error {
		other := s.Tasks[0]
		other.ID = id()
		other.Name = "另一个来源"
		s.Tasks = append(s.Tasks, other)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s = e.Snapshot()
	if err := e.process(context.Background(), s.Tasks[1]); err != nil {
		t.Fatal(err)
	}
	s = e.Snapshot()
	if len(s.Posts) != 6 || len(s.Comments) != 3 {
		t.Fatalf("跨任务应保留发现记录且不重复生成评论: %d/%d", len(s.Posts), len(s.Comments))
	}
}

func TestExistingPostsDoNotConsumeDiscoveryBudget(t *testing.T) {
	e := testEngine(t, fakeBackend{discoverFn: func(_ context.Context, src Source, _ Task) ([]Post, error) {
		if src.Value == "旧渠道" {
			return []Post{{FeedID: "old", Title: "旧帖"}}, nil
		}
		return []Post{{FeedID: "new", Title: "新帖"}}, nil
	}})
	if err := e.update(func(s *State) error {
		s.Tasks[0].Limit = 1
		s.Tasks[0].AnalyzePaused = true
		s.Tasks[0].Sources = []Source{{"keyword", "旧渠道"}, {"keyword", "新渠道"}}
		s.Posts = append(s.Posts, Post{ID: id(), TaskID: s.Tasks[0].ID, FeedID: "old"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runProcess(t, e)
	if got := len(e.Snapshot().Posts); got != 2 {
		t.Fatalf("新渠道被旧帖挤占额度，帖子数=%d", got)
	}
}

func TestStrategyVersionsAndCommentHistory(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	runProcess(t, e)
	s := e.Snapshot()
	comment := s.Comments[0]
	strategy := s.Strategies[0]
	strategy.Mode = "fixed"
	strategy.FixedText = "新的固定评论"
	if err := e.SaveStrategy(strategy); err != nil {
		t.Fatal(err)
	}
	s = e.Snapshot()
	if s.Strategies[0].Version != 2 || s.Comments[0].Strategy.Version != 1 {
		t.Fatal("修改策略覆盖了评论旧版本")
	}
	if err := e.CommentAction(comment.ID, "edit", "人工修改"); err != nil {
		t.Fatal(err)
	}
	if err := e.CommentAction(comment.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CommentAction(comment.ID, "cancel", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Regenerate(context.Background(), comment.ID, "应用新策略"); err != nil {
		t.Fatal(err)
	}
	s = e.Snapshot()
	c := s.Comments[0]
	if c.Strategy.Version != 2 || len(c.StrategyVersions) != 2 || c.StrategyVersions[0].Version != 1 || c.StrategyVersions[1].Version != 2 || c.Text != "新的固定评论" || c.Original != comment.Original || len(c.History) != 5 || c.Status != "review" {
		t.Fatalf("版本/修改记录错误: %+v", c)
	}
}

func queueAll(t *testing.T, e *Engine) {
	t.Helper()
	s := e.Snapshot()
	for _, c := range s.Comments {
		if err := e.CommentAction(c.ID, "approve", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.update(func(s *State) error { s.Settings.SendPaused = false; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestSendLimitIsDurableAndResetByNextRun(t *testing.T) {
	var sends atomic.Int32
	e := testEngine(t, fakeBackend{sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil }})
	runProcess(t, e)
	queueAll(t, e)
	if err := e.update(func(s *State) error { s.Tasks[0].SendLimit = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	e.sendNext()
	e.mu.Lock()
	e.lastSend = time.Time{}
	e.mu.Unlock()
	e.sendNext()
	s := e.Snapshot()
	if sends.Load() != 1 || s.Tasks[0].SendCount != 1 || s.Comments[0].Status != "sent" {
		t.Fatalf("发送额度未生效: %d/%+v", sends.Load(), s.Tasks[0])
	}
	e.pending[s.Tasks[0].ID] = true
	e.runNext()
	waitTaskRuntimeIdle(t, e, s.Tasks[0].ID)
	e.sendNext()
	if sends.Load() != 2 {
		t.Fatalf("新一轮未重置额度，实际发送=%d", sends.Load())
	}
}

func TestUncertainSendRequiresExplicitResolution(t *testing.T) {
	e := testEngine(t, fakeBackend{sendFn: func(context.Context, Post, string) error { return errors.New("timeout after submit") }})
	runProcess(t, e)
	queueAll(t, e)
	e.sendNext()
	s := e.Snapshot()
	c := s.Comments[0]
	if c.Status != "uncertain" {
		t.Fatalf("发送结果应待确认: %s", c.Status)
	}
	if err := e.CommentAction(c.ID, "approve", ""); err == nil {
		t.Fatal("待确认评论被直接重发")
	}
	if err := e.CommentAction(c.ID, "confirm_failed", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CommentAction(c.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().Comments[0].Status != "queued" {
		t.Fatal("人工核实后无法重新审核")
	}
}

func TestAccountMismatchPausesQueue(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	runProcess(t, e)
	queueAll(t, e)
	e.backend = fakeBackend{accountFn: func(context.Context) (Account, error) { return Account{ID: "other-account", LoggedIn: true}, nil }}
	e.sendNext()
	s := e.Snapshot()
	if !s.Settings.SendPaused || s.Comments[0].Status != "queued" {
		t.Fatal("切换账号后队列未暂停")
	}
}

func TestPauseDuringAccountCheckPreventsSend(t *testing.T) {
	var sends atomic.Int32
	e := testEngine(t, DemoBackend{})
	runProcess(t, e)
	queueAll(t, e)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	e.backend = fakeBackend{accountFn: func(context.Context) (Account, error) {
		close(started)
		<-release
		return Account{ID: "demo-account", LoggedIn: true}, nil
	}, sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil }}
	go func() { e.sendNext(); close(finished) }()
	<-started
	if err := e.TaskAction(e.Snapshot().Tasks[0].ID, "pause"); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-finished
	if sends.Load() != 0 || e.Snapshot().Comments[0].Status != "queued" {
		t.Fatal("暂停之后仍然开始发送")
	}
}

func TestRestartRecoversSendingAndPausesQueue(t *testing.T) {
	s := EmptyState()
	store := &memoryStore{}
	s.Comments = append(s.Comments, Comment{ID: "c", Status: "sending"})
	s.Settings.SendPaused = false
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	e, err := NewWithStore(store, clone(store.state), DemoBackend{}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	recovered := e.Snapshot()
	if recovered.Comments[0].Status != "uncertain" || !recovered.Settings.SendPaused {
		t.Fatal("重启恢复不安全")
	}
}

func TestPersistenceFailureDoesNotChangeMemory(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	before := e.Snapshot()
	e.store.Close()
	if err := e.SaveMaterial(Material{Name: "不应保存", Content: "内容"}); err == nil {
		t.Fatal("数据库关闭时应报错")
	}
	if len(e.Snapshot().Materials) != len(before.Materials) {
		t.Fatal("落盘失败仍更新了内存")
	}
}

func TestModelKeysStayPrivateAndBlankEditPreservesKey(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	m := Model{Name: "private", BaseURL: "https://example.com/v1", Model: "m", APIKey: "test-secret"}
	if err := e.SaveModel(m); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	m = s.Models[len(s.Models)-1]
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), "test-secret") || !m.HasKey {
		t.Fatal("快照泄露密钥或标识错误")
	}
	m.Name = "renamed"
	if err := e.SaveModel(m); err != nil {
		t.Fatal(err)
	}
	if findModel(&e.state, m.ID).APIKey != "test-secret" {
		t.Fatal("留空编辑清除了密钥")
	}
}

func TestAIValidatesStructuredOutputAndOmitsAccessToken(t *testing.T) {
	var received map[string]any
	response := "" + `{"match":true,"reason":"符合需求","comment":"建议先确认预算"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer model-key" {
			t.Errorf("模型请求地址或鉴权错误")
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": response}}}})
	}))
	defer server.Close()
	m := Model{Name: "test", Model: "m", BaseURL: server.URL + "/v1", APIKey: "model-key"}
	st := Strategy{Mode: "ai", MaxLength: 30}
	d, err := analyze(context.Background(), m, Task{Goal: "预算咨询"}, Post{Title: "预算怎么分配", Token: "private-xsec-token"}, st, nil, "")
	if err != nil || !d.Match {
		t.Fatalf("有效模型结果未通过: %+v/%v", d, err)
	}
	raw, _ := json.Marshal(received)
	if strings.Contains(string(raw), "private-xsec-token") {
		t.Fatal("把站点访问 token 发送给模型")
	}
	response = `{"reason":"没有match字段","comment":"不能使用"}`
	if _, err = analyze(context.Background(), m, Task{}, Post{}, st, nil, ""); err == nil {
		t.Fatal("接受了缺失匹配结果的模型响应")
	}
}

func TestExportStatusGroupsAndFormulaEscaping(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	runProcess(t, e)
	c := e.Snapshot().Comments[0]
	if err := e.CommentAction(c.ID, "edit", "=危险公式"); err != nil {
		t.Fatal(err)
	}
	if err := e.CommentAction(c.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	e.Register(router.Group("/api/console"))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest("GET", "/api/console/export?status=queued", nil))
	if res.Code != 200 {
		t.Fatalf("导出失败: %s", res.Body.String())
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(res.Body.String(), "\ufeff")))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][7] != "'=危险公式" {
		t.Fatalf("导出未按队列筛选或公式未转义: %v", rows)
	}
	for _, status := range []string{"queued", "sending"} {
		if !matchesCommentStatus(status, "queued") {
			t.Fatal(status)
		}
	}
}

func TestAIHTTPErrorDoesNotExposeResponseSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, "private-model-key")
	}))
	defer server.Close()
	_, err := chat(context.Background(), Model{Name: "x", Model: "x", BaseURL: server.URL}, "", nil)
	if err == nil || strings.Contains(err.Error(), "private-model-key") {
		t.Fatalf("模型错误泄露响应: %v", err)
	}
}

func TestEntireDiscoveryFailureIsVisibleOnTask(t *testing.T) {
	e := testEngine(t, fakeBackend{discoverFn: func(context.Context, Source, Task) ([]Post, error) { return nil, errors.New("site unavailable") }})
	task := e.Snapshot().Tasks[0]
	e.pending[task.ID] = true
	e.runNext()
	waitTaskRuntimeIdle(t, e, task.ID)
	if e.Snapshot().Tasks[0].LastError == "" {
		t.Fatal("所有来源失败仍显示任务完成")
	}
}

func TestEntireAnalysisFailureIsReported(t *testing.T) {
	e := testEngine(t, fakeBackend{detailFn: func(_ context.Context, p Post) (Post, error) { return p, errors.New("detail inaccessible") }})
	task := e.Snapshot().Tasks[0]
	if err := e.process(context.Background(), task); err == nil {
		t.Fatal("全部详情失败仍报告成功")
	}
}
