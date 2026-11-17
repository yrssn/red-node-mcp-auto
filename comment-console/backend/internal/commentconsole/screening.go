package commentconsole

import (
	"context"
	"fmt"
	"time"
)

func (e *Engine) ocrClient() OCRClient {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ocr == nil {
		e.ocr = NewOCRClientFromEnv()
	}
	return e.ocr
}

func (e *Engine) readPostDecision(ctx context.Context, task Task, old Post, strategy Strategy, model *Model, materials []Material, progress ...string) (Post, Decision, error) {
	label := old.Title
	if len(progress) > 0 {
		label = progress[0]
	}
	var p Post
	err := e.taskBrowser(ctx, task.ID, "detail", "读取详情，"+label, func() error {
		var detailErr error
		p, detailErr = e.backend.Detail(ctx, old)
		return detailErr
	})
	if err != nil {
		return p, Decision{}, err
	}
	if err = ctx.Err(); err != nil {
		return p, Decision{}, err
	}
	p.ID, p.TaskID, p.Source, p.CreatedAt = old.ID, old.TaskID, old.Source, old.CreatedAt
	if len(p.Images) > 0 {
		e.setTaskProgress(task.ID, "ocr", "识别图片文字，"+label)
		ocr := e.ocrClient().RecognizePost(ctx, p.Images)
		p.OCRText, p.OCRStatus, p.OCRError = ocr.Text, ocr.Status, ocr.Error
	} else {
		p.OCRText, p.OCRStatus, p.OCRError = "", "none", ""
	}
	if err = ctx.Err(); err != nil {
		return p, Decision{}, err
	}
	e.setTaskProgress(task.ID, "analyze", "分析帖子内容，"+label)
	var d Decision
	p.AnalysisMethod = ""
	switch {
	case len(p.Images) > 0 && p.OCRStatus != "success" && p.OCRStatus != "empty":
		d = Decision{Status: "review", Reason: "图片文字尚未完整识别；请检查本地 OCR 后重新分析，或人工核对。"}
	default:
		if model == nil {
			return p, d, fmt.Errorf("请先为任务配置有效模型")
		}
		d, err = e.decide(ctx, *model, task, p, strategy, materials, "")
		if err != nil {
			return p, d, err
		}
		p.AnalysisMethod = "model"
		d.Status = "unmatched"
		if d.Match {
			d.Status = "matched"
		}
	}
	p.DecisionStatus, p.Reason = d.Status, d.Reason
	if err = ctx.Err(); err != nil {
		return p, Decision{}, err
	}
	p.Match = &d.Match
	return p, d, nil
}

// ReanalyzePost 只刷新指定候选，不采集新帖、不生成或发送评论。
func (e *Engine) ReanalyzePost(ctx context.Context, key string) error {
	e.mu.Lock()
	if e.stopping || e.ctx.Err() != nil || e.running != "" || len(e.taskRuns) > 0 {
		e.mu.Unlock()
		return fmt.Errorf("有任务正在运行，请等待本轮完成后重试")
	}
	s := clone(e.state)
	p := findPost(&s, key)
	if p == nil {
		e.mu.Unlock()
		return fmt.Errorf("帖子不存在")
	}
	t := findTask(&s, p.TaskID)
	if t == nil {
		e.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	if err := taskResourcesReady(&s, *t); err != nil {
		e.mu.Unlock()
		return err
	}
	for _, c := range s.Comments {
		if c.PostID == key && contains([]string{"draft", "review", "queued", "sending", "uncertain", "sent"}, c.Status) {
			e.mu.Unlock()
			return fmt.Errorf("该帖已有发送记录或待处理队列，请先核实评论状态")
		}
	}
	st := findStrategy(&s, t.StrategyID)
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	e.running, e.runCancel = t.ID, cancel
	e.setTaskStatusLocked(t.ID, "reanalyze", "重新分析指定候选")
	e.workers.Add(1)
	e.reanalysisPost = key
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		e.running = ""
		e.runCancel = nil
		e.reanalysisPost = ""
		e.setTaskStatusLocked(t.ID, "idle", "指定候选分析已结束")
		e.mu.Unlock()
		e.workers.Done()
		e.notifyScheduler()
	}()
	updated, _, err := e.readPostDecision(runCtx, *t, *p, *st, findModel(&s, t.ModelID), s.Materials)
	if err != nil {
		return err
	}
	return e.update(func(s *State) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if current := findPost(s, key); current != nil {
			*current = updated
		}
		logState(s, t.ID, "info", "重新分析指定候选："+updated.AnalysisMethod+" / "+updated.DecisionStatus)
		return nil
	})
}
