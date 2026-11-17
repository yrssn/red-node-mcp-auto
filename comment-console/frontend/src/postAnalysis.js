export function decisionLabel(post) {
  const labels = {
    matched: "匹配",
    unmatched: "不匹配",
    review: "待人工确认",
  };
  return (
    labels[post?.decision_status] ||
    (post?.match == null ? "待分析" : post.match ? "匹配" : "不匹配")
  );
}

export function decisionType(post) {
  if (post?.decision_status === "review") return "warning";
  if (post?.match == null) return "info";
  return post.match ? "success" : "info";
}

export function analysisMethodLabel(method) {
  return { rules: "历史规则结果", model: "文本模型" }[method] || "未记录";
}

export function analysisModeLabel(mode) {
  return (
    {
      rules: "本地规则",
      hybrid: "规则优先 + 模型",
      model: "AI 模型",
    }[mode] || "AI 模型"
  );
}

export function ocrStatusLabel(status) {
  return (
    {
      pending: "待识别",
      done: "识别完成",
      success: "已识别",
      partial: "部分成功",
      empty: "未识别文字",
      failed: "识别失败",
      disabled: "未启用",
      none: "无图片",
    }[status] || "未识别"
  );
}
