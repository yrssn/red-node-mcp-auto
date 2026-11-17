package commentconsole

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const accountProbeTimeout = 40 * time.Second

var (
	errAccountIdentityUnknown = errors.New("已检测到登录，但账号身份无法确认，请重新检查")
	errAccountCheckFailed     = errors.New("登录状态检查失败，请重试")
	errAccountCheckPanic      = errors.New("登录状态检查异常，请重试")
	errAccountStatusChanged   = errors.New("登录状态已变化，请重新检查")
	errAccountServiceStopped  = errors.New("服务已停止，无法检查登录状态")
)

// AccountStatus 仅用于界面展示，发送前仍须实时核实账号身份。
type AccountStatus struct {
	Account
	CheckedAt int64  `json:"checked_at"`
	Checking  bool   `json:"checking"`
	Error     string `json:"error"`
}

type accountStatusCache struct {
	mu     sync.Mutex
	status AccountStatus
	epoch  uint64
	probe  *accountStatusProbe
}

type accountStatusProbe struct {
	epoch    uint64
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	finished bool
	result   AccountStatus
	err      error
}

func (e *Engine) accountStatusCache() *accountStatusCache {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.accountCache == nil {
		e.accountCache = &accountStatusCache{}
	}
	return e.accountCache
}

func (c *accountStatusCache) snapshotLocked() AccountStatus {
	status := c.status
	status.Checking = c.probe != nil
	return status
}

func (e *Engine) startAccountProbeLocked(c *accountStatusCache) *accountStatusProbe {
	base := e.ctx
	if base == nil {
		base = context.Background()
	}
	if base.Err() != nil {
		c.status.Error = errAccountServiceStopped.Error()
		return nil
	}
	ctx, cancel := context.WithTimeout(base, accountProbeTimeout)
	p := &accountStatusProbe{epoch: c.epoch, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.probe = p
	// 等待浏览器锁也计入超时；已超时的检查取得锁后不会再访问站点。
	context.AfterFunc(ctx, func() { e.finishAccountProbe(c, p, ctx.Err()) })
	go e.runAccountProbe(c, p)
	return p
}

// AccountStatusSnapshot 只读缓存，未知或较旧的状态也不会自动启动浏览器。
func (e *Engine) AccountStatusSnapshot() AccountStatus {
	c := e.accountStatusCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// VerifyAccountStatus 强制核实，多个调用共用同一次检查。
func (e *Engine) VerifyAccountStatus(ctx context.Context) (AccountStatus, error) {
	c := e.accountStatusCache()
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		status := c.snapshotLocked()
		c.mu.Unlock()
		return status, safeAccountError(err)
	}
	p := c.probe
	if p == nil {
		p = e.startAccountProbeLocked(c)
	}
	status := c.snapshotLocked()
	c.mu.Unlock()
	if p == nil {
		return status, errAccountServiceStopped
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		status = c.snapshotLocked()
		c.mu.Unlock()
		return status, safeAccountError(ctx.Err())
	case <-p.done:
		return p.result, p.err
	}
}

func (e *Engine) runAccountProbe(c *accountStatusCache, p *accountStatusProbe) {
	defer p.cancel()
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	c.mu.Lock()
	current := !p.finished && c.probe == p && c.epoch == p.epoch
	c.mu.Unlock()
	if !current || p.ctx.Err() != nil {
		e.finishAccountProbe(c, p, p.ctx.Err())
		return
	}
	_, err := e.readAccountLiveLocked(p.ctx, c, p.epoch)
	e.finishAccountProbe(c, p, err)
}

func (e *Engine) finishAccountProbe(c *accountStatusCache, p *accountStatusProbe, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.finished {
		return
	}
	p.finished = true
	if c.epoch != p.epoch || c.probe != p {
		p.err = errAccountStatusChanged
	} else {
		p.err = safeAccountError(err)
		if p.err != nil {
			c.status.Error = p.err.Error()
		}
		c.probe = nil
	}
	p.result = c.snapshotLocked()
	close(p.done)
}

// accountLiveLocked 调用方须持有 browserMu，缓存绝不作为发送身份依据。
func (e *Engine) accountLiveLocked(ctx context.Context) (Account, error) {
	c := e.accountStatusCache()
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	return e.readAccountLiveLocked(ctx, c, epoch)
}

func (e *Engine) readAccountLiveLocked(ctx context.Context, c *accountStatusCache, epoch uint64) (account Account, err error) {
	defer func() {
		if recover() != nil {
			account, err = Account{}, errAccountCheckPanic
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil && account.LoggedIn && strings.TrimSpace(account.ID) == "" {
			err = errAccountIdentityUnknown
		}
		err = safeAccountError(err)
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.epoch != epoch {
			return
		}
		if err == nil {
			if !account.LoggedIn {
				account = Account{}
			}
			c.status.Account, c.status.CheckedAt, c.status.Error = account, time.Now().Unix(), ""
		} else {
			// 站点错误不能把上次已确认的账号变成退出登录。
			c.status.Error = err.Error()
		}
	}()
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	return e.backend.Account(ctx)
}

// InvalidateAccountStatus 使旧检查失效；注销只影响展示状态，不恢复任何队列。
func (e *Engine) InvalidateAccountStatus(loggedOut bool) {
	c := e.accountStatusCache()
	c.mu.Lock()
	c.epoch++
	p := c.probe
	c.probe = nil
	c.status.Error = ""
	if loggedOut {
		c.status = AccountStatus{CheckedAt: time.Now().Unix()}
	}
	c.mu.Unlock()
	if p != nil {
		p.cancel()
	}
}

// 不向接口透传浏览器原始异常，避免意外暴露会话或代理凭证。
func safeAccountError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("登录状态检查超时，请重试: %w", context.DeadlineExceeded)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("登录状态检查已取消: %w", context.Canceled)
	case errors.Is(err, errAccountIdentityUnknown):
		return errAccountIdentityUnknown
	case errors.Is(err, errAccountCheckPanic):
		return errAccountCheckPanic
	default:
		return errAccountCheckFailed
	}
}
