package commentconsole

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestPresetTaskWithoutModelIsSavedAsPausedDraft(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	task := e.Snapshot().Tasks[0]
	task.ID = ""
	task.Name = "待配置模型的预设"
	task.ModelID = ""
	task.Paused = false
	if err := e.SaveTask(task); err != nil {
		t.Fatalf("应允许保存未选择模型的草稿：%v", err)
	}
	snapshot := e.Snapshot()
	draft := snapshot.Tasks[len(snapshot.Tasks)-1]
	if draft.ModelID != "" || !draft.Paused {
		t.Fatalf("未配置模型的任务必须保存为暂停草稿：%+v", draft)
	}

	// 运行中的任务不能直接移除模型，需先明确暂停。
	active := snapshot.Tasks[0]
	if active.Paused {
		t.Fatal("测试前置条件：原任务应已启用")
	}
	active.ModelID = ""
	if err := e.SaveTask(active); err == nil {
		t.Fatal("未暂停任务不应允许直接移除模型")
	}
	unchanged := e.Snapshot().Tasks[0]
	if unchanged.Paused || unchanged.ModelID != snapshot.Tasks[0].ModelID {
		t.Fatal("校验失败后改变了原任务状态")
	}
	if err := e.TaskAction(active.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveTask(active); err != nil {
		t.Fatalf("暂停后清空模型保存草稿失败：%v", err)
	}
	got := e.Snapshot().Tasks[0]
	if !got.Paused || got.ModelID != "" {
		t.Fatalf("清空模型后未暂停任务：%+v", got)
	}
}

func TestPresetTaskRejectsUnknownNonemptyModel(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	before := e.Snapshot()
	task := before.Tasks[0]
	task.ID = ""
	task.ModelID = "unknown-model"
	if err := e.SaveTask(task); err == nil {
		t.Fatal("非空且不存在的模型 ID 不应当作为草稿保存")
	}
	after := e.Snapshot()
	if len(after.Tasks) != len(before.Tasks) {
		t.Fatal("无效模型任务已写入")
	}
	task = before.Tasks[0]
	task.ModelID = "unknown-model"
	if err := e.SaveTask(task); err == nil {
		t.Fatal("编辑任务时也应拒绝不存在的模型")
	}
	if got := e.Snapshot().Tasks[0]; got.ModelID != before.Tasks[0].ModelID {
		t.Fatalf("校验失败后覆盖了原模型：%s", got.ModelID)
	}
}

func TestPresetTaskActivationValidatesBeforeChangingState(t *testing.T) {
	for _, action := range []string{"run", "resume"} {
		for _, invalid := range []string{"empty-model", "missing-model", "disabled-strategy"} {
			t.Run(action+"/"+invalid, func(t *testing.T) {
				var accountCalls, discoveryCalls atomic.Int32
				backend := fakeBackend{
					accountFn: func(context.Context) (Account, error) {
						accountCalls.Add(1)
						return Account{ID: "test-account", LoggedIn: true}, nil
					},
					discoverFn: func(context.Context, Source, Task) ([]Post, error) {
						discoveryCalls.Add(1)
						return nil, nil
					},
				}
				e := testEngine(t, backend)
				if err := e.update(func(s *State) error {
					task := &s.Tasks[0]
					task.Paused = true
					task.Scheduled = true
					task.NextRun = 123
					switch invalid {
					case "empty-model":
						task.ModelID = ""
					case "missing-model":
						task.ModelID = "unknown-model"
					case "disabled-strategy":
						s.Strategies[0].Enabled = false
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				before := e.Snapshot().Tasks[0]
				if err := e.TaskAction(before.ID, action); err == nil {
					t.Fatal("配置不完整的任务不应允许启动或恢复")
				}
				after := e.Snapshot().Tasks[0]
				if !after.Paused || after.NextRun != before.NextRun {
					t.Fatalf("校验失败仍改变了调度状态：%+v", after)
				}
				e.mu.Lock()
				pending := e.pending[before.ID]
				e.mu.Unlock()
				if pending {
					t.Fatal("校验失败仍加入了运行队列")
				}
				e.runNext()
				if accountCalls.Load() != 0 || discoveryCalls.Load() != 0 {
					t.Fatalf("未配置任务触发了站点操作：account=%d discover=%d", accountCalls.Load(), discoveryCalls.Load())
				}
			})
		}
	}
}

func TestPresetProcessWithoutModelNeverTouchesBackend(t *testing.T) {
	var accountCalls, discoveryCalls atomic.Int32
	e := testEngine(t, fakeBackend{
		accountFn: func(context.Context) (Account, error) {
			accountCalls.Add(1)
			return Account{ID: "test-account", LoggedIn: true}, nil
		},
		discoverFn: func(context.Context, Source, Task) ([]Post, error) {
			discoveryCalls.Add(1)
			return nil, nil
		},
	})
	task := e.Snapshot().Tasks[0]
	if err := e.update(func(s *State) error {
		s.Tasks[0].ModelID = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	task.ModelID = ""
	if err := e.process(context.Background(), task); err == nil {
		t.Fatal("绕过界面启动的未配置任务也应被拒绝")
	}
	if accountCalls.Load() != 0 || discoveryCalls.Load() != 0 {
		t.Fatalf("应先校验模型再操作站点：account=%d discover=%d", accountCalls.Load(), discoveryCalls.Load())
	}
}

func TestPresetEmojiFixedCommentSurvivesUTF8RoundTrip(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	const emojiText = "🙂👍💻"
	strategy := e.Snapshot().Strategies[0]
	strategy.Mode = "fixed"
	strategy.FixedText = emojiText
	strategy.MaxLength = utf8.RuneCountInString(emojiText)
	if err := validateText(emojiText, strategy); err != nil {
		t.Fatalf("三个 emoji 应当满足三 rune 上限：%v", err)
	}
	shorter := strategy
	shorter.MaxLength--
	if err := validateText(emojiText, shorter); err == nil {
		t.Fatal("两个 rune 的上限不应接受三个 emoji")
	}
	if err := e.SaveStrategy(strategy); err != nil {
		t.Fatalf("保存 emoji 固定话术失败：%v", err)
	}
	runProcess(t, e)
	snapshot := e.Snapshot()
	if len(snapshot.Comments) == 0 {
		t.Fatal("未生成固定话术草稿")
	}
	for _, comment := range snapshot.Comments {
		if comment.Text != emojiText || comment.Original != emojiText ||
			comment.Strategy.FixedText != emojiText || !utf8.ValidString(comment.Text) {
			t.Fatalf("emoji 草稿或策略内容发生损坏：%+v", comment)
		}
	}
	store := e.store.(*memoryStore)
	store.mu.Lock()
	persisted := clone(store.state)
	store.mu.Unlock()
	data, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Strategies[0].FixedText != emojiText || decoded.Comments[0].Text != emojiText {
		t.Fatal("emoji 内容在状态序列化往返后发生变化")
	}
	if decoded.Comments[0].History[0].Text != emojiText {
		t.Fatal("操作历史未保留完整 emoji")
	}

	// 长度沿用 rune 语义，带肤色或连接符的组合不按一个字形计算。
	strategy.MaxLength = 2
	if err := validateText("👍🏽", strategy); err != nil {
		t.Fatal(err)
	}
	strategy.MaxLength = 1
	if err := validateText("👍🏽", strategy); err == nil {
		t.Fatal("组合 emoji 应遵循两个 rune 的现有长度语义")
	}
}
