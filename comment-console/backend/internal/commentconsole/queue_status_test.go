package commentconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func queueStatusEngine(t *testing.T, backend Backend) *Engine {
	t.Helper()
	e := testEngine(t, backend)
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = false
		s.Settings.SendInterval = 60
		s.Tasks[0].AccountID = "demo-account"
		s.Posts = []Post{{ID: "queue-post", TaskID: s.Tasks[0].ID, FeedID: "queue-feed"}}
		s.Comments = []Comment{{
			ID: "queue-comment", TaskID: s.Tasks[0].ID, PostID: "queue-post",
			AccountID: "demo-account", Status: "queued", Text: "已审核的评论",
			Strategy: s.Strategies[0],
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func queueReasonContains(reason string, alternatives ...string) bool {
	for _, word := range alternatives {
		if strings.Contains(reason, word) {
			return true
		}
	}
	return false
}

func TestQueueSnapshotExplainsBlockersInPriorityOrder(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Engine, *State)
		words  []string
	}{
		{"任务不存在优先全局暂停", func(_ *Engine, s *State) {
			s.Comments[0].TaskID = "missing-task"
			s.Settings.SendPaused = true
		}, []string{"任务不存在", "任务已删除", "缺少任务"}},
		{"帖子不存在优先全局暂停", func(_ *Engine, s *State) {
			s.Comments[0].PostID = "missing-post"
			s.Settings.SendPaused = true
		}, []string{"帖子不存在", "帖子已删除", "缺少帖子", "原帖记录不存在"}},
		{"全局优先任务开关", func(_ *Engine, s *State) {
			s.Settings.SendPaused = true
			s.Tasks[0].Paused, s.Tasks[0].SendPaused = true, true
		}, []string{"全局"}},
		{"任务暂停优先发送阶段", func(_ *Engine, s *State) {
			s.Tasks[0].Paused, s.Tasks[0].SendPaused = true, true
		}, []string{"任务已暂停", "任务暂停"}},
		{"发送阶段优先额度", func(_ *Engine, s *State) {
			s.Tasks[0].SendPaused = true
			s.Tasks[0].SendCount = s.Tasks[0].SendLimit
		}, []string{"发送阶段", "任务发送", "暂停发送"}},
		{"额度优先策略", func(_ *Engine, s *State) {
			s.Tasks[0].SendCount = s.Tasks[0].SendLimit
			s.Strategies[0].Enabled = false
		}, []string{"额度", "上限"}},
		{"策略已停用", func(_ *Engine, s *State) {
			s.Strategies[0].Enabled = false
		}, []string{"策略"}},
		{"策略不存在", func(_ *Engine, s *State) {
			s.Strategies = nil
		}, []string{"策略"}},
		{"同帖重新分析", func(e *Engine, _ *State) {
			e.reanalysisPost = "queue-post"
		}, []string{"重新分析"}},
		{"当前任务运行中", func(e *Engine, s *State) {
			e.running = s.Tasks[0].ID
		}, []string{"任务", "操作"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := queueStatusEngine(t, DemoBackend{})
			if err := e.update(func(s *State) error { tt.change(e, s); return nil }); err != nil {
				t.Fatal(err)
			}
			comment := e.Snapshot().Comments[0]
			if !comment.QueueBlocked || !queueReasonContains(comment.QueueReason, tt.words...) {
				t.Fatalf("未解释当前阻塞原因：blocked=%v reason=%q", comment.QueueBlocked, comment.QueueReason)
			}
		})
	}
}

func TestQueueSnapshotImmediatelyReflectsPauseSwitches(t *testing.T) {
	e := queueStatusEngine(t, DemoBackend{})
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = true
		s.Tasks[0].SendPaused = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Comments[0]; !got.QueueBlocked || !strings.Contains(got.QueueReason, "全局") {
		t.Fatalf("未显示全局暂停：%+v", got)
	}
	if err := e.update(func(s *State) error { s.Settings.SendPaused = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Comments[0]; !got.QueueBlocked || !queueReasonContains(got.QueueReason, "发送阶段", "任务发送", "暂停发送") {
		t.Fatalf("恢复全局后未立即显示阶段暂停：%+v", got)
	}
	if err := e.update(func(s *State) error { s.Tasks[0].SendPaused = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Comments[0]; got.QueueBlocked || got.QueueReason == "" || strings.Contains(got.QueueReason, "未登录") {
		t.Fatalf("所有开关恢复后应待发送前核实账号，不能误报未登录：%+v", got)
	}
}

func TestQueueSnapshotDoesNotPersistDerivedFields(t *testing.T) {
	e := queueStatusEngine(t, DemoBackend{})
	if err := e.update(func(s *State) error { s.Tasks[0].SendPaused = true; return nil }); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	beforeState, _ := json.Marshal(e.state)
	e.mu.Unlock()
	store := e.store.(*memoryStore)
	store.mu.Lock()
	beforeStore, _ := json.Marshal(store.state)
	store.mu.Unlock()
	for i := 0; i < 3; i++ {
		if comment := e.Snapshot().Comments[0]; comment.QueueReason == "" || !comment.QueueBlocked {
			t.Fatal("快照未生成队列提示")
		}
	}
	e.mu.Lock()
	afterState, _ := json.Marshal(e.state)
	e.mu.Unlock()
	store.mu.Lock()
	afterStore, _ := json.Marshal(store.state)
	store.mu.Unlock()
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeStore, afterStore) {
		t.Fatal("读取快照修改了内存原状态或持久化存储")
	}
}

func TestQueueSnapshotClearsReasonForNonqueuedRecords(t *testing.T) {
	for _, status := range []string{"draft", "review", "sending", "sent", "failed", "uncertain", "skipped", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			e := queueStatusEngine(t, DemoBackend{})
			if err := e.update(func(s *State) error {
				s.Comments[0].Status = status
				s.Comments[0].QueueReason, s.Comments[0].QueueBlocked = "陈旧的队列原因", true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if got := e.Snapshot().Comments[0]; got.QueueReason != "" || got.QueueBlocked {
				t.Fatalf("非queued记录仍显示队列阻塞：%+v", got)
			}
		})
	}
}

func TestQueuePauseNeverChecksAccountOrSends(t *testing.T) {
	for _, gate := range []string{"global", "task", "send-stage"} {
		t.Run(gate, func(t *testing.T) {
			var accounts, sends atomic.Int32
			e := queueStatusEngine(t, fakeBackend{
				accountFn: func(context.Context) (Account, error) {
					accounts.Add(1)
					return Account{ID: "demo-account", LoggedIn: true}, nil
				},
				sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil },
			})
			if err := e.update(func(s *State) error {
				switch gate {
				case "global":
					s.Settings.SendPaused = true
				case "task":
					s.Tasks[0].Paused = true
				case "send-stage":
					s.Tasks[0].SendPaused = true
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			e.sendNext()
			if accounts.Load() != 0 || sends.Load() != 0 || e.Snapshot().Comments[0].Status != "queued" {
				t.Fatal("暂停时仍核验账号或开始发送")
			}
		})
	}
}

func TestQueueShowsLoginCheckAndIntervalAsWaiting(t *testing.T) {
	e := queueStatusEngine(t, DemoBackend{})
	e.mu.Lock()
	e.checkingSend = "queue-comment"
	e.mu.Unlock()
	if got := e.Snapshot().Comments[0]; got.QueueBlocked || !queueReasonContains(got.QueueReason, "核验", "核实", "检查", "确认") {
		t.Fatalf("正在实时核验账号应显示非阻塞等待：%+v", got)
	}
	e.mu.Lock()
	e.checkingSend = ""
	e.lastSend = time.Now()
	e.mu.Unlock()
	if got := e.Snapshot().Comments[0]; !strings.Contains(got.QueueReason, "间隔") {
		t.Fatalf("发送间隔未显示：%+v", got)
	}
}

func TestQueueLiveAccountFailureCannotUseCachedLogin(t *testing.T) {
	for _, failure := range []string{"logged-out", "wrong-account", "error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			var sends atomic.Int32
			e := queueStatusEngine(t, fakeBackend{
				accountFn: func(context.Context) (Account, error) {
					return Account{ID: "demo-account", Name: "上次已登录账号", LoggedIn: true}, nil
				},
			})
			if _, err := e.account(context.Background()); err != nil {
				t.Fatal(err)
			}
			e.backend = fakeBackend{
				accountFn: func(context.Context) (Account, error) {
					switch failure {
					case "logged-out":
						return Account{LoggedIn: false}, nil
					case "wrong-account":
						return Account{ID: "other-account", LoggedIn: true}, nil
					case "error":
						return Account{}, errors.New("private-browser-url?token=must-not-leak")
					default:
						panic("private-browser-url?token=must-not-leak")
					}
				},
				sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil },
			}
			e.sendNext()
			got := e.Snapshot().Comments[0]
			if sends.Load() != 0 || got.Status != "queued" || !got.QueueBlocked || got.QueueReason == "" {
				t.Fatalf("live账号失败后仍发送或缺少阻塞提示：sends=%d comment=%+v", sends.Load(), got)
			}
			if strings.Contains(got.QueueReason, "must-not-leak") || strings.Contains(got.QueueReason, "private-browser-url") {
				t.Fatal("队列提示泄漏原始账号检查错误")
			}
			if failure == "error" && strings.Contains(got.QueueReason, "未登录") {
				t.Fatal("检查错误不应将上次已知登录身份直接说成未登录")
			}
			e.mu.Lock()
			checking := e.checkingSend
			e.mu.Unlock()
			if checking != "" {
				t.Fatal("账号检查结束后仍残留checkingSend标记")
			}
		})
	}
}

func TestQueueClaimPersistenceFailureHasTransientReason(t *testing.T) {
	var sends atomic.Int32
	e := queueStatusEngine(t, fakeBackend{
		sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil },
	})
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	e.sendNext()
	got := e.Snapshot().Comments[0]
	if sends.Load() != 0 || got.Status != "queued" || !got.QueueBlocked || !queueReasonContains(got.QueueReason, "保存", "数据库", "落盘") {
		t.Fatalf("claim保存失败后没有展示临时原因：sends=%d comment=%+v", sends.Load(), got)
	}
}
