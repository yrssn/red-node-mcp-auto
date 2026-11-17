# 小红书评论工作台

本仓库的应用位于 [comment-console](comment-console/README.md)，前端、Go 后端、OCR、MySQL 和 Docker 部署均在该目录内。

在 Windows PowerShell 中，从仓库根目录执行：

```powershell
Set-Location .\comment-console
.\scripts\build.ps1
.\scripts\start.ps1
```

首次启动前，按[本地配置说明](comment-console/README.md#本地使用)准备 MySQL。正式工作台默认打开 [本地控制台](http://127.0.0.1:18061/console/)。需要容器运行时，请看 [Docker 部署说明](comment-console/docs/docker.md)。
