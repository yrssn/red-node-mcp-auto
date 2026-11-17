package commentconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type accountWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *accountWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func newAccountStatusEngine(t *testing.T, accountFn func(context.Context) (Account, error)) *Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{ctx: ctx, cancel: cancel, backend: fakeBackend{accountFn: accountFn}}
	t.Cleanup(cancel)
	return e
}

func awaitAccountProbe(t *testing.T, c *accountStatusCache) *accountStatusProbe {
	t.Helper()
	c.mu.Lock()
	p := c.probe
	c.mu.Unlock()
	if p == nil {
		t.Fatal("后台检查未登记")
	}
	return p
}

func awaitAccountDone(t *testing.T, p *accountStatusProbe) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("登录状态检查没有结束")
	}
}

func beginAccountVerification(t *testing.T, e *Engine) *accountStatusProbe {
	t.Helper()
	ctx := &accountWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() { _, _ = e.VerifyAccountStatus(ctx) }()
	<-ctx.waiting
	return awaitAccountProbe(t, e.accountStatusCache())
}

func TestAccountStatusExplicitVerificationCoalescesAndCaches(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		calls.Add(1)
		close(started)
		<-release
		return Account{ID: "account-1", Name: "原账号", LoggedIn: true}, nil
	})
	initial := e.AccountStatusSnapshot()
	if initial.CheckedAt != 0 || initial.LoggedIn || initial.Checking || calls.Load() != 0 {
		t.Fatalf("初始状态错误: %+v", initial)
	}
	p := beginAccountVerification(t, e)
	<-started
	for range 20 {
		if !e.AccountStatusSnapshot().Checking {
			t.Fatal("并发检查状态丢失")
		}
	}
	type result struct {
		status AccountStatus
		err    error
	}
	fresh := make(chan result, 1)
	waitCtx := &accountWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() { s, err := e.VerifyAccountStatus(waitCtx); fresh <- result{s, err} }()
	<-waitCtx.waiting
	if calls.Load() != 1 {
		t.Fatalf("重复启动浏览器检查: %d", calls.Load())
	}
	close(release)
	awaitAccountDone(t, p)
	got := <-fresh
	if got.err != nil || !got.status.LoggedIn || got.status.ID != "account-1" || got.status.Checking {
		t.Fatalf("手动检查未复用后台结果: %+v / %v", got.status, got.err)
	}
	for range 20 {
		s := e.AccountStatusSnapshot()
		if !s.LoggedIn || s.CheckedAt == 0 || s.Checking || s.Error != "" {
			t.Fatalf("缓存结果错误: %+v", s)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("读取缓存触发了站点检查")
	}
	data, err := json.Marshal(got.status)
	if err != nil || !strings.Contains(string(data), `"logged_in":true`) || strings.Contains(string(data), `"Account"`) {
		t.Fatalf("账号字段没有平铺: %s / %v", data, err)
	}
}

func TestAccountStatusFreshAlwaysChecksAndErrorsPreserveIdentity(t *testing.T) {
	var calls atomic.Int32
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		if calls.Add(1) > 1 {
			return Account{}, errors.New("Cookie=private-cookie; key=private-key")
		}
		return Account{ID: "known-id", Name: "已确认账号", LoggedIn: true}, nil
	})
	first, err := e.VerifyAccountStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	failed, err := e.VerifyAccountStatus(context.Background())
	if err == nil || failed.ID != first.ID || failed.Name != first.Name || !failed.LoggedIn || failed.CheckedAt != first.CheckedAt || failed.Error == "" {
		t.Fatalf("检查失败覆盖了已有账号: %+v / %v", failed, err)
	}
	if strings.Contains(failed.Error, "private") || strings.Contains(err.Error(), "private") {
		t.Fatal("检查错误泄露原始凭证")
	}
	for range 5 {
		e.AccountStatusSnapshot()
	}
	if calls.Load() != 2 {
		t.Fatal("读取错误缓存触发了站点检查")
	}
}

func TestAccountStatusCachedGETNeverStartsSiteChecks(t *testing.T) {
	for _, cached := range []AccountStatus{
		{},
		{Account: Account{ID: "old-id", Name: "已确认账号", LoggedIn: true}, CheckedAt: time.Now().Add(-24 * time.Hour).Unix()},
	} {
		name := "未知"
		if cached.CheckedAt > 0 {
			name = "旧缓存"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			e := newAccountStatusEngine(t, func(context.Context) (Account, error) { calls.Add(1); return Account{}, nil })
			c := e.accountStatusCache()
			c.status = cached
			router := gin.New()
			e.Register(router.Group("/api/console"))
			// 缓存读取不需要浏览器，已有站点动作占用锁时也须立即返回。
			e.browserMu.Lock()
			defer e.browserMu.Unlock()
			for range 30 {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/console/account/status", nil))
				var got AccountStatus
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || got != cached {
					t.Fatalf("读取缓存改变了状态: %+v / %d", got, response.Code)
				}
			}
			c.mu.Lock()
			pending := c.probe != nil
			c.mu.Unlock()
			if pending || calls.Load() != 0 {
				t.Fatalf("自动读取触发了站点检查: pending=%t calls=%d", pending, calls.Load())
			}
		})
	}
}

func TestAccountStatusInvalidationDiscardsOldLogoutProbe(t *testing.T) {
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		close(started)
		<-release // 模拟未及时响应取消的站点操作。
		close(returned)
		return Account{ID: "old-id", Name: "旧账号", LoggedIn: true}, nil
	})
	p := beginAccountVerification(t, e)
	<-started
	e.InvalidateAccountStatus(true)
	awaitAccountDone(t, p)
	if !errors.Is(p.err, errAccountStatusChanged) {
		t.Fatalf("旧检查没有失效: %v", p.err)
	}
	close(release)
	<-returned
	// 等待实际浏览器操作退出，确保它已经尝试写回结果。
	e.browserMu.Lock()
	e.browserMu.Unlock()
	s := e.AccountStatusSnapshot()
	if s.LoggedIn || s.ID != "" || s.Name != "" || s.Checking || s.Error != "" || s.CheckedAt == 0 {
		t.Fatalf("旧结果覆盖了注销: %+v", s)
	}
}

func TestAccountStatusInvalidationPreservesKnownUntilExplicitVerification(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		if calls.Add(1) == 1 {
			return Account{ID: "first", Name: "原账号", LoggedIn: true}, nil
		}
		<-release
		return Account{ID: "second", Name: "新账号", LoggedIn: true}, nil
	})
	if _, err := e.VerifyAccountStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.InvalidateAccountStatus(false)
	s := e.AccountStatusSnapshot()
	if s.ID != "first" || s.Checking || calls.Load() != 1 {
		t.Fatalf("失效时丢失旧账号: %+v", s)
	}
	p := beginAccountVerification(t, e)
	close(release)
	awaitAccountDone(t, p)
	fresh := e.AccountStatusSnapshot()
	if fresh.ID != "second" || fresh.Error != "" || calls.Load() != 2 {
		t.Fatalf("未核实新账号: %+v", fresh)
	}
}

func TestAccountStatusPanicAndMissingIdentityRemainUnconfirmed(t *testing.T) {
	var calls atomic.Int32
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		switch calls.Add(1) {
		case 1:
			panic("private-cookie")
		case 2:
			return Account{Name: "缺少 ID", LoggedIn: true}, nil
		default:
			return Account{ID: "recovered", LoggedIn: true}, nil
		}
	})
	for range 2 {
		s, err := e.VerifyAccountStatus(context.Background())
		if err == nil || s.CheckedAt != 0 || s.LoggedIn || s.Checking || s.Error == "" || strings.Contains(s.Error, "private") {
			t.Fatalf("无法确认的状态被视为登录成功: %+v / %v", s, err)
		}
	}
	s, err := e.VerifyAccountStatus(context.Background())
	if err != nil || !s.LoggedIn || s.ID != "recovered" || s.Error != "" {
		t.Fatalf("异常后缓存或浏览器锁不可用: %+v / %v", s, err)
	}
}

func TestAccountStatusCallerCancellationDoesNotCancelSharedProbe(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	e := newAccountStatusEngine(t, func(ctx context.Context) (Account, error) {
		close(started)
		select {
		case <-ctx.Done():
			return Account{}, ctx.Err()
		case <-release:
			return Account{ID: "shared", LoggedIn: true}, nil
		}
	})
	p := beginAccountVerification(t, e)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.VerifyAccountStatus(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("调用方取消未生效: %v", err)
	}
	if !e.AccountStatusSnapshot().Checking {
		t.Fatal("取消一个请求终止了共享检查")
	}
	close(release)
	awaitAccountDone(t, p)
	if s := e.AccountStatusSnapshot(); s.ID != "shared" || !s.LoggedIn {
		t.Fatalf("共享检查未完成: %+v", s)
	}
}

func TestAccountStatusShutdownWhileWaitingBrowserNeverCallsBackend(t *testing.T) {
	var calls atomic.Int32
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) { calls.Add(1); return Account{}, nil })
	e.browserMu.Lock()
	p := beginAccountVerification(t, e)
	e.cancel()
	awaitAccountDone(t, p)
	if !errors.Is(p.err, context.Canceled) || p.result.Checking || p.result.Error == "" {
		t.Fatalf("等待浏览器期间未取消: %+v / %v", p.result, p.err)
	}
	e.browserMu.Unlock()
	e.browserMu.Lock()
	e.browserMu.Unlock()
	if calls.Load() != 0 {
		t.Fatal("服务停止后仍访问了站点")
	}
}

func TestAccountLiveAlwaysVerifiesEvenWhenStatusCached(t *testing.T) {
	var calls atomic.Int32
	e := newAccountStatusEngine(t, func(context.Context) (Account, error) {
		if calls.Add(1) == 1 {
			return Account{ID: "first", LoggedIn: true}, nil
		}
		return Account{ID: "second", LoggedIn: true}, nil
	})
	if _, err := e.VerifyAccountStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.browserMu.Lock()
	account, err := e.accountLiveLocked(context.Background())
	e.browserMu.Unlock()
	if err != nil || account.ID != "second" || calls.Load() != 2 {
		t.Fatalf("实时校验复用了过期身份: %+v / %v", account, err)
	}
	if s := e.AccountStatusSnapshot(); s.ID != "second" || s.Error != "" {
		t.Fatalf("实时校验未更新展示状态: %+v", s)
	}
}
