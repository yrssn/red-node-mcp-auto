package commentconsole

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type reanalyzeBlockingOCR struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*reanalyzeBlockingOCR) Health(context.Context) error { return nil }
func (o *reanalyzeBlockingOCR) unblock()                   { o.once.Do(func() { close(o.release) }) }
func (o *reanalyzeBlockingOCR) RecognizePost(ctx context.Context, _ []string) OCRResult {
	close(o.entered)
	select {
	case <-ctx.Done():
		return OCRResult{Status: "failed", Error: "图片文字识别已取消"}
	case <-o.release:
		return OCRResult{Status: "success", Text: "JavaWeb 急求帮忙"}
	}
}

func reanalyzeConcurrencyEngine(t *testing.T, withComment bool) (*Engine, *reanalyzeBlockingOCR, *atomic.Int32) {
	t.Helper()
	sends := &atomic.Int32{}
	e, _ := screeningEngine(t, fakeBackend{
		detailFn: func(_ context.Context, p Post) (Post, error) {
			p.Images = []string{"https://a.xhscdn.com/a"}
			p.AuthorUpdates = "找到了，谢谢"
			return p, nil
		},
		sendFn: func(context.Context, Post, string) error { sends.Add(1); return nil },
	}, OCRResult{})
	ocr := &reanalyzeBlockingOCR{entered: make(chan struct{}), release: make(chan struct{})}
	e.ocr = ocr
	t.Cleanup(ocr.unblock)
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = false
		s.Strategies[0].Mode = "fixed"
		s.Strategies[0].FixedText = "旧评论"
		matched := true
		s.Posts = []Post{{ID: "concurrency-post", TaskID: s.Tasks[0].ID, FeedID: "concurrency-feed", Match: &matched, DecisionStatus: "matched", Reason: "原判断"}}
		if withComment {
			s.Comments = []Comment{{ID: "failed-comment", TaskID: s.Tasks[0].ID, PostID: "concurrency-post", AccountID: "demo-account", Status: "failed", Text: "旧评论", Strategy: s.Strategies[0]}}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e, ocr, sends
}

func TestReanalyzeBlocksApprovingFailedComment(t *testing.T) {
	e, ocr, sends := reanalyzeConcurrencyEngine(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- e.ReanalyzePost(ctx, "concurrency-post") }()
	select {
	case <-ocr.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("重新分析未进入阻塞 OCR")
	}
	if err := e.CommentAction("failed-comment", "approve", ""); err == nil {
		t.Fatal("重新分析期间不应允许将失败旧稿重新审核入队")
	}
	e.sendNext()
	if sends.Load() != 0 || e.Snapshot().Comments[0].Status != "failed" {
		t.Fatal("重新分析期间发送了旧稿或改变了其状态")
	}
	ocr.unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("释放 OCR 后重新分析未完成")
	}
	snapshot := e.Snapshot()
	if snapshot.Posts[0].DecisionStatus != "closed" || sends.Load() != 0 || snapshot.Comments[0].Status != "failed" {
		t.Fatal("已结束需求的重新分析结果或旧评论状态不正确")
	}
}

func TestCancelledReanalyzeKeepsOriginalCandidate(t *testing.T) {
	e, ocr, sends := reanalyzeConcurrencyEngine(t, false)
	before := e.Snapshot().Posts[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- e.ReanalyzePost(ctx, before.ID) }()
	select {
	case <-ocr.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("重新分析未进入阻塞 OCR")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("应返回取消错误：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("重新分析未及时响应取消")
	}
	after := e.Snapshot().Posts[0]
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("取消后原候选被部分结果覆盖：before=%+v after=%+v", before, after)
	}
	if sends.Load() != 0 || len(e.Snapshot().Comments) != 0 {
		t.Fatal("取消重新分析后不应生成或发送评论")
	}
}
