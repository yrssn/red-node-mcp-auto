# 评论工作台前端

沿用 Vue 2.7.16、Element UI 2.15.14 和 Vite 7。界面与业务功能保持不变，API 前缀为 /api/console。

## 安装与开发

需要 Node.js 20.19+ 或 22.12+，以及 npm。在本目录执行：

    npm ci
    npm run dev

打开 Vite 显示的地址并加上 /console/。开发服务器默认将 /api 代理到 http://127.0.0.1:18061，后端请从 ../backend 单独启动。

可通过 VITE_API_TARGET 指定其他后端地址，也可在本目录 .env.local 中配置：

    VITE_API_TARGET=http://127.0.0.1:18061

不要将数据库密码或模型密钥放入 VITE_ 开头的变量，它们属于浏览器可见配置。

## 构建

    npm run build

构建产物写入 dist/，页面基路径保持 /console/。新后端从本目录的 dist/ 读取静态资源，不再嵌入 Go 二进制；部署时保留前端构建目录，并按后端启动说明指定静态资源路径。

vite.config.js 将应用 import 与 Element UI require 统一解析到同一个 Vue runtime。请保留 alias 与 dedupe，避免表格和消息组件出现重复 Vue 构造器问题。
