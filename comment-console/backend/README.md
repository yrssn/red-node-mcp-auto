# 后端

独立 Go 模块，应用入口为 cmd/server，Go 源码与测试均在本目录。

在本目录执行检查：

    go test ./...
    go vet ./...

Windows PowerShell 中从源码启动：

```powershell
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go run ./cmd/server -port 127.0.0.1:18061
```

想观察浏览器操作时，在源码启动命令后追加 `-headless=false`，或使用下面的一键脚本并追加 `-ShowBrowser`。GoLand 的 **Comment Console (amd64)** 配置已启用可见浏览器。切换显示模式需要先停止旧服务。

浏览器组件需要 64 位程序。部分 Windows Go SDK 默认使用 `386`，必须先设置上面的 `GOARCH`，否则会报“windows/386 无预编译浏览器”。环境设置仅作用于当前终端。

使用 GoLand 打开本目录后，选择运行配置 **Comment Console (amd64)**。该配置已设置 64 位目标和工作目录；使用自己创建的配置时，在 Run → Edit Configurations → Environment 中填写 `GOARCH=amd64;CGO_ENABLED=0`，Program arguments 填写 `-port 127.0.0.1:18061 -headless=false`，Working directory 选择本 backend 目录。用 Run 启动；Debug 建议安装 64 位 Go SDK。

如果只需要使用工作台，在本目录执行一键启动脚本，会使用已构建的 64 位程序并尝试启动 OCR：

```powershell
powershell -ExecutionPolicy Bypass -File ..\scripts\start.ps1
```

两种启动方式选一种，避免重复占用端口。启动后访问 http://127.0.0.1:18061/console/。

运行前填写 config/mysql.json；页面静态资源默认读取 ../frontend/dist。

如果从其他工作目录启动，请显式设置 -config 和 -web-dir。默认本地 Cookie 写入 data/cookies.json，也可用 COOKIES_PATH 覆盖。详细启动步骤见上层 README。
