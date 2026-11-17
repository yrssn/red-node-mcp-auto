package commentconsole

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func manualDraftAllowed(p Post) error {
	if strings.TrimSpace(p.FeedID) == "" {
		return fmt.Errorf("帖子标识不可用，请先重新获取详情")
	}
	if p.Match == nil {
		return fmt.Errorf("请先分析帖子，再保存人工草稿")
	}
	return nil
}

// CreateManualDraft 只保存本人写入的正文，不生成内容、不入队发送。
func (e *Engine) CreateManualDraft(ctx context.Context, postID, text string) error {
	text = strings.TrimSpace(text)
	e.mu.Lock()
	if e.stopping || e.ctx.Err() != nil || e.running != "" || len(e.taskRuns) > 0 || e.reanalysisPost == postID {
		e.mu.Unlock()
		return fmt.Errorf("有任务正在运行，请等待本轮完成后保存草稿")
	}
	s := clone(e.state)
	p := findPost(&s, postID)
	if p == nil {
		e.mu.Unlock()
		return fmt.Errorf("帖子不存在")
	}
	t := findTask(&s, p.TaskID)
	if t == nil {
		e.mu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	if err := manualDraftAllowed(*p); err != nil {
		e.mu.Unlock()
		return err
	}
	strategy := findStrategy(&s, t.StrategyID)
	if strategy == nil || !strategy.Enabled {
		e.mu.Unlock()
		return fmt.Errorf("评论策略不可用")
	}
	if err := validateText(text, *strategy); err != nil {
		e.mu.Unlock()
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	e.running, e.runCancel = t.ID, cancel
	e.setTaskStatusLocked(t.ID, "manual", "核实账号并保存人工草稿")
	e.workers.Add(1)
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		e.running = ""
		e.runCancel = nil
		e.setTaskStatusLocked(t.ID, "idle", "人工草稿操作已结束")
		e.mu.Unlock()
		e.workers.Done()
		e.notifyScheduler()
	}()
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	account, err := e.accountLiveLocked(runCtx)
	if err != nil {
		return err
	}
	if !account.LoggedIn || account.ID == "" {
		return fmt.Errorf("请先登录并确认账号身份")
	}
	return e.update(func(s *State) error {
		if e.reanalysisPost == postID {
			return fmt.Errorf("该帖子正在重新分析，请等待完成")
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		post := findPost(s, postID)
		task := findTask(s, p.TaskID)
		if post == nil || task == nil {
			return fmt.Errorf("帖子或任务已变更，请刷新后重试")
		}
		if err := manualDraftAllowed(*post); err != nil {
			return err
		}
		if task.AccountID != "" && task.AccountID != account.ID {
			return fmt.Errorf("当前账号与任务绑定账号不一致")
		}
		currentStrategy := findStrategy(s, task.StrategyID)
		if currentStrategy == nil || !currentStrategy.Enabled {
			return fmt.Errorf("评论策略不可用")
		}
		if err := validateText(text, *currentStrategy); err != nil {
			return err
		}
		for _, comment := range s.Comments {
			oldPost := findPost(s, comment.PostID)
			if comment.AccountID == account.ID && oldPost != nil && oldPost.FeedID == post.FeedID {
				return fmt.Errorf("该账号已为此帖建立评论记录，请查看或编辑已有记录")
			}
		}
		task.AccountID = account.ID
		c := Comment{ID: id(), TaskID: task.ID, PostID: post.ID, AccountID: account.ID, Original: text, Text: text, Status: "review", Strategy: *currentStrategy, StrategyVersions: []Strategy{*currentStrategy}, CreatedAt: now(), UpdatedAt: now(), History: []Revision{{At: now(), Action: "保存人工草稿", Text: text}}}
		s.Comments = append(s.Comments, c)
		logState(s, task.ID, "info", "保存人工评论草稿，等待本人审核")
		return nil
	})
}
