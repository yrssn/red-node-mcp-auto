package commentconsole

import (
	"fmt"
	"strings"
	"time"
)

func (e *Engine) setQueueNote(commentID, note string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.queueNotes == nil {
		e.queueNotes = map[string]string{}
	}
	if note == "" {
		delete(e.queueNotes, commentID)
	} else {
		e.queueNotes[commentID] = note
	}
}

// 只计算展示状态，不改队列或发送开关；调用时已持有 mu。
func (e *Engine) queueStatusLocked(s *State, c Comment) (string, bool) {
	t := findTask(s, c.TaskID)
	if t == nil {
		return "关联任务不存在，无法发送", true
	}
	if findPost(s, c.PostID) == nil {
		return "原帖记录不存在，无法发送", true
	}
	reasons := []string{}
	if s.Settings.SendPaused {
		reasons = append(reasons, "全局发送队列已暂停")
	}
	if t.Paused {
		reasons = append(reasons, "关联任务已暂停")
	}
	if t.SendPaused {
		reasons = append(reasons, "任务的发送阶段已暂停，请在任务管理中查看发送开关")
	}
	if t.SendCount >= t.SendLimit {
		reasons = append(reasons, "本轮发送额度已用完，下一轮运行后重置")
	}
	strategy := findStrategy(s, t.StrategyID)
	if strategy == nil || !strategy.Enabled {
		reasons = append(reasons, "评论策略不存在或已停用")
	}
	if len(reasons) > 0 {
		if note := e.queueNotes[c.ID]; note != "" {
			reasons = append(reasons, note)
		}
		return strings.Join(reasons, "；"), true
	}
	if note := e.queueNotes[c.ID]; note != "" {
		return note, true
	}
	if e.reanalysisPost == c.PostID {
		return "帖子正在重新分析，完成后再检查队列", true
	}
	if e.running != "" {
		return "正在处理人工操作，完成后再检查发送队列", true
	}
	if e.taskRuns[c.TaskID] != nil {
		return "关联任务正在获取或分析，待本轮结束后检查发送队列", true
	}
	if e.checkingSend == c.ID {
		return "正在核实登录账号，尚未提交评论", false
	}
	remaining := time.Until(e.lastSend.Add(time.Duration(s.Settings.SendInterval) * time.Second))
	if remaining > 0 {
		return fmt.Sprintf("等待发送间隔，约 %d 秒", int(remaining/time.Second)+1), false
	}
	return "等待执行发送前账号核实", false
}
