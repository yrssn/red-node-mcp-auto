package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cc "xhs-comment-console/internal/commentconsole"
)

type testMemoryStore struct {
	data []byte
}

func (s *testMemoryStore) Save(state cc.State) error {
	data, err := json.Marshal(state)
	if err == nil {
		s.data = data
	}
	return err
}

func (*testMemoryStore) Close() error { return nil }

// 通过真实路由验证鉴权与演示隔离，不连接浏览器或外部模型。
func newConsoleIntegrationRouter(t *testing.T) http.Handler {
	t.Helper()
	engine, err := cc.NewWithStore(&testMemoryStore{}, cc.EmptyState(), cc.DemoBackend{}, true)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	app := NewAppServer(NewXiaohongshuService(), "console-test-token")
	app.console = engine
	app.webDir = newConsoleWebFixture(t)
	return setupRoutes(app)
}

func newConsoleWebFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>评论工作台测试</title>"), 0600))
	return dir
}

func consoleRequest(router http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestConsoleRouteAuthAndDemoIsolation(t *testing.T) {
	router := newConsoleIntegrationRouter(t)
	tests := []struct {
		method string
		path   string
		token  string
		status int
	}{
		{http.MethodGet, "/health", "", http.StatusOK},
		{http.MethodGet, "/", "", http.StatusTemporaryRedirect},
		{http.MethodGet, "/console", "", http.StatusTemporaryRedirect},
		{http.MethodGet, "/console/", "", http.StatusOK},
		{http.MethodGet, "/api/console/state", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/console/state", "wrong-token", http.StatusUnauthorized},
		{http.MethodGet, "/api/console/state", "console-test-token", http.StatusOK},
		{http.MethodGet, "/api/console/account", "console-test-token", http.StatusOK},
		{http.MethodGet, "/api/v1/login/status", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/login/status", "console-test-token", http.StatusForbidden},
		{http.MethodPost, "/api/v1/feeds/comment", "console-test-token", http.StatusForbidden},
		{http.MethodPost, "/mcp", "console-test-token", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.token, func(t *testing.T) {
			got := consoleRequest(router, tt.method, tt.path, tt.token, "{}")
			assert.Equal(t, tt.status, got.Code, got.Body.String())
		})
	}
	got := consoleRequest(router, http.MethodGet, "/", "", "")
	assert.Equal(t, "/console/", got.Header().Get("Location"))
	state := consoleRequest(router, http.MethodGet, "/api/console/state", "console-test-token", "")
	assert.Equal(t, "no-store", state.Header().Get("Cache-Control"))
	assert.Contains(t, state.Body.String(), "\"demo\":true")
}

func TestConsoleModelSecretNeverReturned(t *testing.T) {
	router := newConsoleIntegrationRouter(t)
	got := consoleRequest(router, http.MethodPost, "/api/console/models", "console-test-token",
		"{\"name\":\"测试模型\",\"base_url\":\"https://example.com/v1\",\"model\":\"test-model\",\"api_key\":\"private-test-key\"}")
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	got = consoleRequest(router, http.MethodGet, "/api/console/state", "console-test-token", "")
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	assert.NotContains(t, got.Body.String(), "private-test-key")
	var state cc.State
	require.NoError(t, json.Unmarshal(got.Body.Bytes(), &state))
	require.Len(t, state.Models, 1)
	assert.True(t, state.Models[0].HasKey)
	assert.Empty(t, state.Models[0].APIKey)
	got = consoleRequest(router, http.MethodPost, "/api/console/models/"+state.Models[0].ID+"/test", "console-test-token", "{}")
	assert.Equal(t, http.StatusOK, got.Code, got.Body.String())
}

func TestConsoleLinkDiscoveryDoesNotOpenBrowser(t *testing.T) {
	backend := consoleBackend{}
	posts, err := backend.Discover(t.Context(), cc.Source{
		Kind: "link", Value: "https://www.xiaohongshu.com/explore/note-id?xsec_token=a%2Bb",
	}, cc.Task{})
	require.NoError(t, err)
	require.Len(t, posts, 1)
	assert.Equal(t, "note-id", posts[0].FeedID)
	assert.Equal(t, "a+b", posts[0].Token)
}

func TestParseConsoleURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  string
		id    string
		token string
		valid bool
	}{
		{"主页", "https://www.xiaohongshu.com/user/profile/user-id?xsec_token=abc%2Bdef", "profile", "user-id", "abc+def", true},
		{"帖子", "https://xiaohongshu.com/explore/note-id?xsec_token=test", "link", "note-id", "test", true},
		{"分享帖子路径", "https://www.xiaohongshu.com/discovery/item/note-id?xsec_token=test", "link", "note-id", "test", true},
		{"错误协议", "http://www.xiaohongshu.com/explore/note-id", "link", "", "", false},
		{"伪造域名", "https://www.xiaohongshu.com.example.com/explore/note-id", "link", "", "", false},
		{"外部链接", "https://example.com/explore/note-id", "link", "", "", false},
		{"短链接", "https://xhslink.com/test", "link", "", "", false},
		{"错误来源类型", "https://www.xiaohongshu.com/explore/note-id", "profile", "", "", false},
		{"缺少ID", "https://www.xiaohongshu.com/explore/", "link", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, token, err := parseConsoleURL(tt.input, tt.kind)
			if !tt.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.id, id)
			assert.Equal(t, tt.token, token)
		})
	}
}

func TestLegacyRequestsShareConsoleBrowserGate(t *testing.T) {
	engine, err := cc.NewWithStore(&testMemoryStore{}, cc.EmptyState(), cc.DemoBackend{}, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	app := NewAppServer(NewXiaohongshuService(), "console-test-token")
	app.console = engine
	router := setupRoutes(app)
	locked, release, unlocked := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		engine.WithBrowser(func() {
			close(locked)
			<-release
		})
		close(unlocked)
	}()
	<-locked
	released := false
	defer func() {
		if !released {
			close(release)
		}
		<-unlocked
	}()

	// 状态页无需浏览器，应能在账号操作期间正常响应。
	state := consoleRequest(router, http.MethodGet, "/api/console/state", "console-test-token", "")
	require.Equal(t, http.StatusOK, state.Code)

	// 空正文只会触发参数校验，不会操作真实浏览器。
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- consoleRequest(router, http.MethodPost, "/api/v1/feeds/comment", "console-test-token", "{}")
	}()
	select {
	case got := <-response:
		t.Fatalf("旧接口绕过浏览器锁提前响应，HTTP %d", got.Code)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	released = true
	select {
	case got := <-response:
		assert.Equal(t, http.StatusBadRequest, got.Code, got.Body.String())
	case <-time.After(2 * time.Second):
		t.Fatal("浏览器锁释放后旧接口未继续执行")
	}
}
