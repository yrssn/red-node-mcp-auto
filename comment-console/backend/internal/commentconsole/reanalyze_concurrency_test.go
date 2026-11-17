package commentconsole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSubmittedReanalysisSurvivesRequestCancellation(t *testing.T) {
	e, ocr, _ := reanalyzeConcurrencyEngine(t, false)
	backend := e.backend.(fakeBackend)
	detail := backend.detailFn
	backend.detailFn = func(ctx context.Context, p Post) (Post, error) {
		if p.ID == "second-post" {
			p.Title = "已读取第二篇帖子"
			return p, nil
		}
		return detail(ctx, p)
	}
	e.backend = backend
	if err := e.update(func(s *State) error {
		s.Posts = append(s.Posts, Post{ID: "second-post", TaskID: s.Tasks[0].ID})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	e.Register(router.Group("/api/console"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/console/posts/reanalyze",
		strings.NewReader(`{"ids":["concurrency-post","second-post"]}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("提交返回 %d：%s", recorder.Code, recorder.Body.String())
	}
	cancel()
	select {
	case <-ocr.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("提交后没有开始分析")
	}
	ocr.unblock()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		state := e.Snapshot()
		if !state.Reanalysis.Active {
			if state.Reanalysis.Completed != 2 || state.Reanalysis.Failed != 0 || state.Posts[1].Title != "已读取第二篇帖子" {
				t.Fatalf("请求取消后批次没有完成：%+v", state.Reanalysis)
			}
			return
		}
	}
	t.Fatal("批次没有结束")
}

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
	e.demo = true
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
	if snapshot.Posts[0].DecisionStatus != "matched" || sends.Load() != 0 || snapshot.Comments[0].Status != "failed" {
		t.Fatal("作者补充含“找到了”时的重新分析结果或旧评论状态不正确")
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
