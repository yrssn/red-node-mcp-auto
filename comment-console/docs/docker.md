# Docker 部署

需要 Docker Compose 2.20+。配置包含独立的本地 OCR 容器，首次构建需下载 Python 依赖；OCR 无宿主端口映射，应用通过内部网络访问。OCR 不可用时有图片的候选会转人工确认，详见 [OCR 说明](ocr.md)。

所有部署文件集中在 deploy，构建上下文为独立项目根。前端和后端分别在镜像构建阶段编译，运行阶段由 Go 服务提供 API 和 frontend/dist 静态资源。

当前电脑未安装 Docker 引擎，镜像构建与容器运行仍需在 Docker 主机验证。应用仅支持 Linux amd64；Windows Docker Desktop 使用 Linux 容器模式。

## 应用和独立 MySQL

在项目根复制 deploy/.env.example 为 deploy/.env，填写 MYSQL_ROOT_PASSWORD 和 MYSQL_PASSWORD。应用账号固定 comment_console，两个库分别为 xhs_comment_console 和 xhs_comment_console_demo。

从项目根启动：

    docker compose --env-file deploy/.env -f deploy/compose.yaml up -d --build
    docker compose --env-file deploy/.env -f deploy/compose.yaml ps
    docker compose --env-file deploy/.env -f deploy/compose.yaml logs -f app

访问 http://127.0.0.1:18062/console/。MySQL 没有映射宿主机端口，不占用已有 3666 服务。

CONSOLE_DEMO=false 使用正式模式，true 模拟外部操作；两种模式仍写各自的 MySQL 库。APP_BIND 默认 127.0.0.1，APP_PORT 默认 18062；AUTH_TOKEN 本机使用可留空，填写后在页面「访问凭证」中输入同一值。XHS_PROXY 可选。

MySQL 初始化只在空数据卷上执行。修改 .env 密码不会修改已有数据库用户，需要在数据库中完成密码变更后同步配置。初始化密码建议使用足够长的随机字母数字，避免单引号、反斜杠或换行。

## 使用现有 MySQL

复制 backend/config/mysql.docker.example.json 为 backend/config/mysql.docker.json，填写实际凭证，并提前创建两个 utf8mb4 数据库。

容器连接宿主 MySQL 使用 host.docker.internal:3666，不使用容器的 localhost。Compose 已设置 host-gateway 映射；MySQL 监听地址、账号来源权限和防火墙必须允许来自容器的连接。

配置会只读挂载到 /app/backend/config/mysql.json。默认 MYSQL_CONFIG_FILE=../backend/config/mysql.docker.json，路径相对于 deploy。

从项目根启动：

    docker compose --env-file deploy/.env -f deploy/compose.external.yaml up -d --build

该方案只启动应用，项目名为 xhs-comment-console-external；默认独立 MySQL 方案的项目名为 xhs-comment-console。两套方案同时运行时，需要使用不同 APP_PORT。

## 持久化与停止

默认方案的命名数据卷：

| 卷 | 容器路径 | 内容 |
| --- | --- | --- |
| mysql_data | /var/lib/mysql | 数据库 |
| session_data | /app/backend/data | Cookie、seed、浏览器运行目录 |
| image_data | /app/backend/images | 图片文件 |

浏览器缓存固定在 /app/cache，不要用数据卷遮住该路径或整个 /app。

停止：

    docker compose --env-file deploy/.env -f deploy/compose.yaml down

此命令保留命名卷；不要使用 down -v，除非明确要删除数据库和登录会话。外部 MySQL 方案的操作需要始终替换为对应 compose.external.yaml。

升级应用：

    docker compose --env-file deploy/.env -f deploy/compose.yaml up -d --build app

升级前暂停任务和发送队列，备份数据库与会话。升级不更改数据库大版本。

## 备份

在项目根先创建 backups 目录，然后：

    docker compose --env-file deploy/.env -f deploy/compose.yaml exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --databases xhs_comment_console xhs_comment_console_demo > /tmp/comment-console.sql'
    docker compose --env-file deploy/.env -f deploy/compose.yaml cp mysql:/tmp/comment-console.sql ./backups/comment-console.sql

同时备份 session_data、image_data 数据卷，以及受控保存的私有配置。外部数据库使用现有 MySQL 的备份工具。恢复前停止应用；还原记录后先检查发送待确认状态，再恢复任务。

## 运行约定

- MySQL 健康后启动应用，应用 /health 提供存活检查。
- 应用由 tini 管理，入口脚本用 exec 转发信号，停止宽限期 90 秒。
- Chromium 共享内存为 1GB，运行时 Cookie 路径明确指向会话卷。
- 镜像不复制私有配置，源码交付包只放示例。
- Cookie、数据库内容和现有单账号逻辑保持不变。
