# 代码结构

## 前端

frontend/src/App.vue 组织单页面导航、列表、筛选和 API 请求。

- ResourceForm.vue：任务、模型、策略、素材编辑表单。
- CommentDrawer.vue：原帖、AI 判断、评论编辑和修改记录。
- Pagination.vue：列表分页。
- style.css：页面样式。
- vite.config.js：构建与 API 代理配置，统一 Vue runtime，避免 Element UI 使用不同 Vue 构造器。

前端不保存数据库连接密码；浏览器通过 /api/console 调用后端。

## 后端

backend 是独立 Go 模块 xhs-comment-console。cmd/server 仅调用 internal/server.Run，组装入口与业务包分开。

| 目录 | 作用 |
| --- | --- |
| internal/server | 启动配置、路由、鉴权、API/MCP 适配、站点服务组装、静态资源服务 |
| internal/commentconsole | 任务执行、模型调用、评论状态流转、策略快照、MySQL 持久化 |
| internal/xiaohongshu | 搜索、主页、帖子详情、评论和登录等页面操作 |
| internal/browser | 内置 Chromium、代理与浏览器指纹配置 |
| internal/cookies | 登录 Cookie 和 seed 会话文件 |
| internal/configs | 浏览器运行参数、seed 选择 |
| internal/humanize | 鼠标、键盘、操作间隔 |
| internal/pkg | 下载、图片与标题工具 |

backend 的测试和编译不依赖 frontend 是否安装 npm 依赖，也不依赖外层参考仓库。前端是否构建只影响页面静态资源，不影响 API 服务的独立启动。

## 数据与登录状态

MySQL 保持已有工作空间数据格式，不修改数据库名、表结构或现有任务记录。v1 使用单行 JSON 快照与 revision CAS；每个工作空间使用一个服务实例。

本地数据库私有配置在 backend/config/mysql.json。正式和演示模式分别使用 dsn 与 demo_dsn，也支持 MYSQL_DSN / MYSQL_DEMO_DSN。

本地 Cookie 在 backend/data/cookies.json，包含 cookies 与固定 seed。Docker 的会话目录由持久化卷保存。原浏览器缓存目录沿用现有位置，迁移不重复下载浏览器。

## 调用链

前端页面 → Gin API → commentconsole 任务与评论逻辑 → server 的站点适配器 → xiaohongshu 页面动作 → go-rod / Chromium。

任务调度最多保留两个处理中的任务，待运行请求按顺序排队；同任务的重复运行请求合并。浏览器动作由 browserMu 互斥，OCR 与模型分析在锁外执行；发送使用独立单 worker。任务的 queued、phase、progress 由内存执行状态生成，仅用于 API 展示，不写入 MySQL。服务停止时取消任务并等待 worker 退出后再关闭存储。

当前账号隔离维持单进程一份会话；没有通过 seed 自动支持多账号。
