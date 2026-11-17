# 小红书评论工作台

Gin + Vue + Element UI + MySQL 评论工作台。开发、构建和部署都以此目录为项目根。

## 目录

~~~text
comment-console/
├── frontend/              Vue 页面、组件、样式、前端依赖与 dist
├── backend/
│   ├── cmd/server/        Go 服务入口
│   ├── cmd/login/         有头浏览器登录工具
│   ├── internal/server/   Gin 路由、HTTP/MCP 接口、服务组装
│   ├── internal/commentconsole/  任务、AI、评论状态、MySQL 存储
│   ├── internal/browser/  浏览器启动、下载与指纹配置
│   ├── internal/xiaohongshu/     小红书页面自动化
│   ├── internal/…         Cookie、配置、输入操作与公共工具
│   ├── config/            数据库配置示例与本地私有配置
│   ├── data/              本地 Cookie、指纹 seed、日志
│   └── go.mod             独立 Go 模块
├── deploy/                Dockerfile、Compose、初始化 SQL、环境示例
├── scripts/               本地构建、启动与打包
├── docs/                  功能、架构与部署说明
├── bin/                   本地构建的程序，不提交
└── release/               交付压缩包，不提交
~~~

前端源码只放在 frontend，Go 源码只放在 backend。前端构建产物保留在 frontend/dist，后端通过 -web-dir 读取，不再把前端复制或嵌入到后端源码目录。

## 本地使用

需要 Go 1.24+、Node.js 22.12+、npm 和 MySQL 5.7+。

1. 将 backend/config/mysql.example.json 复制为 backend/config/mysql.json，填写正式和演示数据库连接。两个数据库须提前创建，字符集使用 utf8mb4；应用启动后自动建表。
2. 在项目根执行构建：

    ./scripts/build.ps1

3. 启动正式模式：

    ./scripts/start.ps1

默认地址：http://127.0.0.1:18061/console/

要看到浏览器的实际操作，在服务未运行时执行 `./scripts/start.ps1 -ShowBrowser`。浏览器会在登录检查、找帖等站点操作发生时打开；显示模式需要重启服务才生效。GoLand 的 **Comment Console (amd64)** 配置默认显示浏览器。

演示模式：

    ./scripts/start.ps1 -Demo

演示地址：http://127.0.0.1:18060/console/

本地配置和登录 Cookie 不包含在交付包中。当前工作区迁移了已有私有配置与会话，解压包需要填写自己的连接信息。

技术需求预置：服务启动后运行 `./scripts/seed-programming.ps1`，可添加 Java、Spring Boot、PHP、Vue、Python、MySQL 等 7 个暂停任务及评论素材。默认暂停，选择文本模型后手动运行和审核。文字表情可以直接在策略、素材和评论编辑中点击插入；素材库可保存图片参考附件，图片表情包评论发送暂未接入。详见 [预置说明与规则边界](docs/programming-presets.md)。

帖子图片由本地 OCR 提取文字，标题、正文和 OCR 文字由文本模型分析。首次执行 `./scripts/install-ocr.ps1`，正常启动会尝试启动 OCR。已有候选可单条重新分析，详情保留 OCR 正文与判断来源，见 [OCR 与省费筛选](docs/ocr.md)。

## 开发

后端开发，在 backend 内执行：

Windows PowerShell 先指定 64 位目标：

    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    go run ./cmd/server -port 127.0.0.1:18061

部分 Windows Go SDK 默认编译成 `386`，浏览器组件只支持 64 位程序。GoLand 直接打开 backend 目录后，选择 **Comment Console (amd64)** 运行配置；已有配置则在环境变量中设置 `GOARCH=amd64;CGO_ENABLED=0`。完整步骤见 [后端启动说明](backend/README.md)。`scripts/start.ps1` 使用已构建的 64 位程序，无需修改全局 Go 环境。

后端独立检查：

    go test ./...
    go vet ./...

前端开发，在 frontend 内执行：

    npm ci
    npm run dev

Vite 默认代理 /api 到 http://127.0.0.1:18061，可用 VITE_API_TARGET 指定其他地址。前端单独构建使用 npm run build。

## Docker

在 deploy 内将 .env.example 复制为 .env，填写新 MySQL 容器的密码，然后从项目根执行：

    docker compose --env-file deploy/.env -f deploy/compose.yaml up -d --build

默认地址：http://127.0.0.1:18062/console/

连接已有 MySQL 使用 deploy/compose.external.yaml，私有连接文件放在 backend/config/mysql.docker.json。容器访问宿主 MySQL 使用 host.docker.internal，具体见 [部署说明](docs/docker.md)。

## 账号、Cookie 和种子

当前保留单账号模式。seed 用于固定浏览器指纹，Cookie 用于保存登录状态，两者保存在同一会话文件；seed 不代表账号列表。

登录：页面右上角「账号登录」→「获取登录二维码」→小红书 App 扫码确认→「检查登录状态」。系统自动保存会话，无需手动抓 Cookie。

真正的多账号管理需要每个账号独立的 Cookie、seed、登录会话和任务绑定；本次按要求只整理目录，不增加该功能。

## 功能与交付

保留原有找帖、AI 分析、评论审核、策略/素材、发送队列、记录导出、登录、日志、MCP 和 HTTP API。

使用 scripts/package.ps1 生成 release/comment-console-v1.0.0.zip 和 SHA256。压缩包仅包含当前独立项目，排除真实密码、Cookie、运行数据和 node_modules。

- [架构与代码入口](docs/architecture.md)
- [功能使用说明](docs/usage.md)
- [Docker 部署](docs/docker.md)

基于原 xiaohongshu-mcp 的浏览器自动化实现，保留 Apache-2.0 LICENSE。代码未推送远程。
