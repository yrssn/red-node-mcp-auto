package commentconsole

import (
	"fmt"
	"slices"
)

// DeletePosts 只清理本地候选记录，保留有关联评论的帖子。
func (e *Engine) DeletePosts(ids []string) (deleted, protected int, err error) {
	if len(ids) == 0 {
		return 0, 0, fmt.Errorf("请选择要删除的候选帖子")
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	err = e.update(func(s *State) error {
		if e.running != "" || len(e.taskRuns) > 0 || e.reanalysis.Active {
			return fmt.Errorf("候选帖子正在处理，请等待本轮结束再删除")
		}
		linked := make(map[string]bool)
		for _, comment := range s.Comments {
			if selected[comment.PostID] {
				linked[comment.PostID] = true
			}
		}
		s.Posts = slices.DeleteFunc(s.Posts, func(p Post) bool {
			if !selected[p.ID] {
				return false
			}
			if linked[p.ID] {
				protected++
				return false
			}
			deleted++
			return true
		})
		if deleted > 0 {
			logState(s, "", "info", fmt.Sprintf("删除了 %d 条候选帖子", deleted))
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return deleted, protected, nil
}

// DeleteComments 保留发送中的评论及用于核对和防重的发送记录。
func (e *Engine) DeleteComments(ids []string) (deleted, protected int, err error) {
	if len(ids) == 0 {
		return 0, 0, fmt.Errorf("请选择要删除的评论")
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	err = e.update(func(s *State) error {
		s.Comments = slices.DeleteFunc(s.Comments, func(c Comment) bool {
			if !selected[c.ID] {
				return false
			}
			switch c.Status {
			case "draft", "review", "queued", "failed", "skipped", "cancelled":
				if c.PostID != e.reanalysisPost || e.reanalysisPost == "" {
					deleted++
					return true
				}
			}
			protected++
			return false
		})
		if deleted > 0 {
			logState(s, "", "info", fmt.Sprintf("删除了 %d 条评论记录", deleted))
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return deleted, protected, nil
}
