package commentconsole

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func manualClosedEngine(t *testing.T) (*Engine, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	accounts, siteWrites := &atomic.Int32{}, &atomic.Int32{}
	e, _ := screeningEngine(t, fakeBackend{
		accountFn: func(context.Context) (Account, error) {
			accounts.Add(1)
			return Account{ID: "demo-account", Name: "测试账号", LoggedIn: true}, nil
		},
		discoverFn: func(context.Context, Source, Task) ([]Post, error) {
			siteWrites.Add(1)
			return nil, nil
		},
		detailFn: func(context.Context, Post) (Post, error) {
			siteWrites.Add(1)
			return Post{}, nil
		},
		sendFn: func(context.Context, Post, string) error { siteWrites.Add(1); return nil },
	}, OCRResult{})
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = false
		task := &s.Tasks[0]
		task.AccountID, task.AutoSend = "demo-account", true
		s.Strategies[0].MaxLength = 20
		s.Strategies[0].Forbidden = "禁用词"
		matched := true
		s.Posts = []Post{{ID: "manual-closed", TaskID: task.ID, FeedID: "same-feed", Match: &matched, DecisionStatus: "review", DemandClosed: true, AnalysisMethod: "rules", Reason: "相关需求已结束，供人工复核"}}
		s.Comments = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e, accounts, siteWrites
}

func TestCreateManualDraftForClosedCandidateRequiresExplicitApproval(t *testing.T) {
	e, accounts, siteWrites := manualClosedEngine(t)
	const text = "以后还有需求可以再交流🙂"
	if err := e.CreateManualDraft(context.Background(), "manual-closed", text); err != nil {
		t.Fatalf("人工写入结束需求草稿失败：%v", err)
	}
	s := e.Snapshot()
	if len(s.Comments) != 1 {
		t.Fatalf("草稿数量错误：%d", len(s.Comments))
	}
	c := s.Comments[0]
	if c.Text != text || c.Status != "review" || c.PostID != "manual-closed" || c.TaskID != s.Tasks[0].ID || c.AccountID != "demo-account" || c.SentText != "" {
		t.Fatalf("人工草稿关联或状态错误：%+v", c)
	}
	e.sendNext()
	if accounts.Load() != 1 || siteWrites.Load() != 0 || e.Snapshot().Comments[0].Status != "review" {
		t.Fatal("人工草稿执行了账号读取之外的站点操作或自动入队")
	}
	if err := e.CommentAction(c.ID, "approve", ""); err != nil {
		t.Fatalf("本人单条审核应允许显式入队：%v", err)
	}
	if e.Snapshot().Comments[0].Status != "queued" || siteWrites.Load() != 0 {
		t.Fatal("显式审核未入队或同步执行了发送")
	}
}

func TestCreateManualDraftAcceptsAnalyzedCandidates(t *testing.T) {
	for _, status := range []string{"matched", "review", "unmatched"} {
		t.Run(status, func(t *testing.T) {
			e, _, siteWrites := manualClosedEngine(t)
			if err := e.update(func(s *State) error {
				s.Posts[0].DemandClosed = false
				s.Posts[0].DecisionStatus = status
				matched := status == "matched"
				s.Posts[0].Match = &matched
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := e.CreateManualDraft(context.Background(), "manual-closed", "人工确认后的回复"); err != nil {
				t.Fatal(err)
			}
			if got := e.Snapshot().Comments; len(got) != 1 || got[0].Status != "review" || siteWrites.Load() != 0 {
				t.Fatal("已分析候选未保持人工审核流程")
			}
		})
	}
}

func TestCreateManualDraftRejectsInvalidCandidateOrText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		change func(*Engine, *State)
	}{
		{"尚未分析", "人工回复", func(_ *Engine, s *State) { s.Posts[0].Match = nil; s.Posts[0].DecisionStatus = "" }},
		{"空内容", "  ", nil},
		{"超长内容", strings.Repeat("字", 21), nil},
		{"禁用词", "包含禁用词", nil},
		{"策略停用", "人工回复", func(_ *Engine, s *State) { s.Strategies[0].Enabled = false }},
		{"策略删除", "人工回复", func(_ *Engine, s *State) { s.Strategies = nil }},
		{"任务删除", "人工回复", func(_ *Engine, s *State) { s.Tasks = nil }},
		{"任务运行中", "人工回复", func(e *Engine, s *State) { e.running = s.Tasks[0].ID }},
		{"帖子重新分析中", "人工回复", func(e *Engine, s *State) {
			e.reanalysisPost = "manual-closed"
			e.running = s.Tasks[0].ID
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, siteWrites := manualClosedEngine(t)
			if tt.change != nil {
				if err := e.update(func(s *State) error { tt.change(e, s); return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.CreateManualDraft(context.Background(), "manual-closed", tt.text); err == nil {
				t.Fatal("应拒绝无效候选或评论")
			}
			if len(e.Snapshot().Comments) != 0 || siteWrites.Load() != 0 {
				t.Fatal("校验失败仍产生草稿或站点操作")
			}
		})
	}
}

func TestCreateManualDraftDeduplicatesEveryExistingStateAcrossTasks(t *testing.T) {
	for _, status := range []string{"draft", "review", "queued", "sending", "uncertain", "sent", "failed", "skipped", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			e, _, siteWrites := manualClosedEngine(t)
			if err := e.update(func(s *State) error {
				other := s.Tasks[0]
				other.ID = "other-task"
				s.Tasks = append(s.Tasks, other)
				s.Posts = append(s.Posts, Post{ID: "other-post", TaskID: other.ID, FeedID: s.Posts[0].FeedID})
				s.Comments = []Comment{{ID: "old-comment", TaskID: other.ID, PostID: "other-post", AccountID: "demo-account", Status: status, Text: "之前的评论"}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := e.CreateManualDraft(context.Background(), "manual-closed", "新的人工回复"); err == nil {
				t.Fatal("同一账号与帖子已有记录，不应再新建草稿")
			}
			if len(e.Snapshot().Comments) != 1 || siteWrites.Load() != 0 {
				t.Fatal("跨任务去重失败")
			}
		})
	}
}

func TestCreateManualDraftDoesNotDeduplicateOtherAccount(t *testing.T) {
	e, _, _ := manualClosedEngine(t)
	if err := e.update(func(s *State) error {
		s.Comments = []Comment{{ID: "foreign-account-comment", TaskID: s.Tasks[0].ID, PostID: s.Posts[0].ID, AccountID: "another-account", Status: "sent", Text: "其他账号的历史评论"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.CreateManualDraft(context.Background(), "manual-closed", "当前账号的人工回复"); err != nil {
		t.Fatalf("不应按其他账号的历史记录去重：%v", err)
	}
	if len(e.Snapshot().Comments) != 2 {
		t.Fatal("当前账号草稿未保存")
	}
}

func TestCreateManualDraftRejectsInvalidAccount(t *testing.T) {
	for _, account := range []Account{{}, {ID: "another-account", LoggedIn: true}} {
		t.Run(account.ID, func(t *testing.T) {
			e, _, siteWrites := manualClosedEngine(t)
			e.backend = fakeBackend{accountFn: func(context.Context) (Account, error) { return account, nil }}
			if err := e.CreateManualDraft(context.Background(), "manual-closed", "人工回复"); err == nil {
				t.Fatal("未登录或与任务绑定不一致的账号不应创建草稿")
			}
			if len(e.Snapshot().Comments) != 0 || siteWrites.Load() != 0 {
				t.Fatal("账号校验失败仍创建了草稿")
			}
		})
	}
}
