export function queueBlockReason(comment, state) {
  if (comment.status !== "queued") return "";
  if (typeof comment.queue_blocked === "boolean") {
    return comment.queue_blocked
      ? comment.queue_reason || "等待发送条件恢复"
      : "";
  }
  if (state.settings.send_paused) return "全局发送已暂停";
  const task = state.tasks.find((item) => item.id === comment.task_id);
  if (!task) return "任务已删除，无法继续发送";
  if (task.paused) return "任务已暂停";
  if (task.send_paused) return "任务发送阶段已暂停";
  if (task.send_limit > 0 && task.send_count >= task.send_limit) {
    return (
      "本轮发送额度已用完（" +
      task.send_count +
      "/" +
      task.send_limit +
      "），等待下一轮"
    );
  }
  const strategy = state.strategies.find(
    (item) => item.id === task.strategy_id
  );
  if (strategy && !strategy.enabled) return "评论策略已停用";
  return "";
}

export function queueReason(comment, state) {
  if (comment.status !== "queued") return "";
  return (
    comment.queue_reason ||
    queueBlockReason(comment, state) ||
    "等待前序评论或发送间隔（" + state.settings.send_interval + " 秒）"
  );
}
