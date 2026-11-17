// 只预览素材内嵌图片，避免旧数据中的外链触发网络请求。
export function referenceImage(value) {
  return typeof value === "string" &&
    /^data:image\/(?:png|jpeg);base64,/i.test(value)
    ? value
    : "";
}
