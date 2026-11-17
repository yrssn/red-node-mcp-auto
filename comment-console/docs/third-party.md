# 第三方组件说明

本地评论工作台基于 xiaohongshu-mcp，保留上游项目的 Apache-2.0 LICENSE。浏览器自动化继续使用 go-rod 与 headless_browser。

新增组件：

| 组件 | 版本 | 许可证 |
| --- | --- | --- |
| Vue | 2.7.16 | MIT |
| Element UI | 2.15.14 | MIT |
| Vite | 7.3.7 | MIT |
| @vitejs/plugin-vue2 | 2.3.4 | MIT |
| github.com/go-sql-driver/mysql | 1.9.3 | MPL-2.0 |
| golang.org/x/image | 0.30.0 | BSD-3-Clause |
| RapidOCR | 3.10.0 | Apache-2.0 |
| ONNX Runtime CPU | 1.23.2 | MIT |
| Pillow | 12.2.0 | MIT-CMU |

Vue 和 Element UI 的源码许可证、Go 依赖的许可证随分发保留，docs/licenses/ 提供依赖包许可证副本。版本的完整锁定信息见 backend/go.mod、backend/go.sum、frontend/package-lock.json。

Chromium 未包含在 Windows 交付包内，正式模式首次启动由原项目机制下载。浏览器分发与组件声明沿用上游实现。

OCR 使用独立 Python 环境或内部容器；代码和模型来自 RapidOCR 官方发行物。版本与依赖见 ocr/requirements.txt，来源和安装边界见 [OCR 说明](../ocr/README.md)。Windows ZIP 不打包本机虚拟环境、用户图片和数据库内容。分发 Python 依赖及模型时须同时保留其包内许可证。
