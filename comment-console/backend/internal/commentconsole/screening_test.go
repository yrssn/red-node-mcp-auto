package commentconsole

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type screeningOCR struct {
	result OCRResult
	calls  int
	images []string
}

func (o *screeningOCR) RecognizePost(_ context.Context, images []string) OCRResult {
	o.calls++
	o.images = append([]string(nil), images...)
	return o.result
}

func (*screeningOCR) Health(context.Context) error { return nil }

func screeningEngine(t *testing.T, backend Backend, result OCRResult) (*Engine, *screeningOCR) {
	t.Helper()
	e := testEngine(t, backend)
	e.demo = false
	ocr := &screeningOCR{result: result}
	e.ocr = ocr
	if err := e.update(func(s *State) error {
		s.Settings.SendPaused = true
		task := &s.Tasks[0]
		task.AnalysisMode = "model"
		task.MatchKeywords = "JavaWeb,Spring Boot"
		task.DemandKeywords = "求助,急求,代做"
		task.Sources = []Source{{Kind: "keyword", Value: "JavaWeb"}}
		task.AutoSend = false
		task.Scheduled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e, ocr
}

func screeningBackend(authorUpdates string) fakeBackend {
	return fakeBackend{
		discoverFn: func(context.Context, Source, Task) ([]Post, error) {
			return []Post{{FeedID: "image-demand", Token: "private-xsec-token"}}, nil
		},
		detailFn: func(_ context.Context, p Post) (Post, error) {
			p.Images = []string{"https://sns-webpic.xhscdn.com/image-demand.png"}
			p.AuthorUpdates = authorUpdates
			return p, nil
		},
	}
}

func TestScreeningModelReceivesOCRAndAuthorUpdatesWithoutImageURLs(t *testing.T) {
	requests := make(chan []byte, 1)
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{"choices": []any{map[string]any{"message": map[string]string{
			"content": "{\"match\":true,\"reason\":\"图片文字说明仍有技术需求\",\"comment\":\"可以补充具体报错信息。\"}",
		}}}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer model.Close()
	const update = "现在仍有需求，预算已经确认。"
	e, ocr := screeningEngine(t, screeningBackend(update), OCRResult{Status: "success", Text: "javaweb作业\n代做，急求"})
	if err := e.update(func(s *State) error {
		s.Models = []Model{{ID: "model", Name: "测试模型", Model: "test", BaseURL: model.URL + "/v1"}}
		s.Tasks[0].AnalysisMode = "model"
		s.Tasks[0].ModelID = "model"
		s.Materials[0].Image = "data:image/png;base64,private-reference-image"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runProcess(t, e)
	if modelCalls.Load() != 1 {
		t.Fatalf("模型模式调用次数错误: %d", modelCalls.Load())
	}
	body := <-requests
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("模型消息格式错误: %s", body)
	}
	message, ok := messages[1].(map[string]any)
	if !ok {
		t.Fatal("模型缺少用户消息")
	}
	var input map[string]any
	content, _ := message["content"].(string)
	if err := json.Unmarshal([]byte(content), &input); err != nil {
		t.Fatal(err)
	}
	post, ok := input["post"].(map[string]any)
	if !ok || post["image_ocr_text"] != ocr.result.Text || post["author_updates"] != update {
		t.Fatalf("模型未收到 OCR 或原作者补充: %+v", input["post"])
	}
	for _, secret := range []string{"https://sns-webpic.xhscdn.com/image-demand.png", "private-xsec-token", "private-reference-image"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("向模型泄露了图片地址或访问数据: %s", secret)
		}
	}
	s := e.Snapshot()
	if s.Posts[0].AnalysisMethod != "model" || len(s.Comments) != 1 || s.Comments[0].Status != "review" {
		t.Fatalf("模型分析结果未进入审核流程: %+v", s)
	}
}

func TestScreeningReanalyzeTouchesOnlySelectedPost(t *testing.T) {
	var discoveryCalls, sendCalls, detailCalls atomic.Int32
	e, ocr := screeningEngine(t, fakeBackend{
		discoverFn: func(context.Context, Source, Task) ([]Post, error) {
			discoveryCalls.Add(1)
			return nil, nil
		},
		sendFn: func(context.Context, Post, string) error { sendCalls.Add(1); return nil },
		detailFn: func(_ context.Context, p Post) (Post, error) {
			detailCalls.Add(1)
			if p.ID != "selected-post" {
				t.Errorf("重新分析读取了其他帖子: %s", p.ID)
			}
			p.Title = "更新后的标题"
			p.Images = []string{"https://sns-webpic.xhscdn.com/selected-post.png"}
			p.AuthorUpdates = "仍有需求，请先说明技术方案。"
			return p, nil
		},
	}, OCRResult{Status: "success", Text: "javaweb作业\n代做，急求"})
	e.demo = true
	if err := e.update(func(s *State) error {
		task := &s.Tasks[0]
		task.Paused = true
		s.Strategies[0].Mode = "fixed"
		s.Strategies[0].FixedText = "固定草稿"
		unmatched := false
		s.Posts = []Post{
			{ID: "selected-post", TaskID: task.ID, FeedID: "selected", Title: "旧标题", Match: &unmatched, Reason: "旧判断", DecisionStatus: "unmatched", CreatedAt: 123},
			{ID: "other-post", TaskID: task.ID, FeedID: "other", Title: "其他帖子", Content: "不能变更", OCRText: "旧图片文字", DecisionStatus: "review", CreatedAt: 456},
		}
		s.Comments = []Comment{{ID: "existing-comment", TaskID: task.ID, PostID: "other-post", Text: "已有草稿", Status: "review"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot()
	if err := e.ReanalyzePost(context.Background(), "selected-post"); err != nil {
		t.Fatal(err)
	}
	after := e.Snapshot()
	if discoveryCalls.Load() != 0 || sendCalls.Load() != 0 || detailCalls.Load() != 1 || ocr.calls != 1 {
		t.Fatalf("重新分析越界操作: discover=%d send=%d detail=%d ocr=%d", discoveryCalls.Load(), sendCalls.Load(), detailCalls.Load(), ocr.calls)
	}
	if !reflect.DeepEqual(before.Posts[1], after.Posts[1]) || !reflect.DeepEqual(before.Comments, after.Comments) {
		t.Fatal("重新分析修改了其他帖子或评论")
	}
	p := after.Posts[0]
	if p.Title != "更新后的标题" || p.DecisionStatus != "matched" || p.OCRText != ocr.result.Text || p.CreatedAt != before.Posts[0].CreatedAt {
		t.Fatalf("指定帖子详情或判断未更新: %+v", p)
	}
	e.mu.Lock()
	running, cancel := e.running, e.runCancel
	e.mu.Unlock()
	if running != "" || cancel != nil {
		t.Fatal("重新分析结束后未释放任务占用")
	}
}

func TestScreeningRunNextDoesNotOverlapActiveWork(t *testing.T) {
	var accountCalls, discoverCalls atomic.Int32
	e, _ := screeningEngine(t, fakeBackend{
		accountFn: func(context.Context) (Account, error) {
			accountCalls.Add(1)
			return Account{ID: "demo-account", LoggedIn: true}, nil
		},
		discoverFn: func(context.Context, Source, Task) ([]Post, error) {
			discoverCalls.Add(1)
			return nil, nil
		},
	}, OCRResult{Status: "success"})
	taskID := e.Snapshot().Tasks[0].ID
	e.mu.Lock()
	e.running = "reanalyzing-another-post"
	e.pending[taskID] = true
	e.mu.Unlock()
	e.runNext()
	e.mu.Lock()
	pending, running := e.pending[taskID], e.running
	e.mu.Unlock()
	if accountCalls.Load() != 0 || discoverCalls.Load() != 0 || !pending || running != "reanalyzing-another-post" {
		t.Fatalf("已有操作时仍运行新任务: account=%d discover=%d pending=%v running=%s", accountCalls.Load(), discoverCalls.Load(), pending, running)
	}
}
