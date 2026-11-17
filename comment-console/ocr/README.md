# 本地免费 OCR 服务

采用 RapidOCR 3.10.0、ONNX Runtime CPU 1.23.2。识别在自己的电脑或容器中执行，不需要账号、API Key 或按次付费的 OCR 服务；仍会使用本机 CPU 和内存。支持中英文印刷文字，结果需人工校对。

## 安装与启动

Python 3.11–3.13 x64，推荐使用独立虚拟环境，不改系统 Python。

从独立项目根执行：

    python -m venv .runtime/ocr
    ./.runtime/ocr/Scripts/python.exe -m pip install -r ocr/requirements.txt
    ./.runtime/ocr/Scripts/python.exe ocr/server.py

默认只监听 127.0.0.1:18063。可通过 BIND、PORT 环境变量修改；容器设置 BIND=0.0.0.0，不要向公网直接发布此内部服务。服务启动前加载并预热同一组模型，推理串行执行；并发时返回 HTTP 429，不无限排队。

首次安装需要连接 PyPI 或可信镜像下载依赖。RapidOCR wheel 已打包下列模型，安装后不需要再下载模型：

- PP-OCRv6_det_small.onnx
- ch_ppocr_mobile_v2.0_cls_mobile.onnx
- PP-OCRv6_rec_small.onnx

启动时会检查上述文件，并显式传入本地路径；缺少权重时直接失败。服务不调用在线图片地址、自动下载模型或可视化字体，也不保存图片和识别文本。首次安装和 Docker 构建仍需下载 Python 依赖及基础镜像；完整离线安装可先用 pip wheel -r ocr/requirements.txt -w wheelhouse 准备所有 wheel，再在目标机使用 pip install --no-index --find-links wheelhouse -r ocr/requirements.txt。

## HTTP 接口

- GET /health：返回 ok 和 engine。
- POST /ocr：请求体为原始 PNG、JPEG 或静态 WebP 字节，Content-Type 使用对应图片 MIME，或 application/octet-stream。必须提供 Content-Length，不接收 URL、文件路径、JSON、multipart 或 chunked 上传。

图片上限 10 MiB、1600 万像素、最长边 16384 像素；超过限制会在完整解码前拒绝。动画图片不支持。透明区域按白底处理，返回坐标基于处理 EXIF 朝向后的图片。

返回格式：

    {
      "text": "计算机 Java\nSpring Boot 求助",
      "lines": [
        {"text": "计算机 Java", "score": 0.98, "box": [[0,0],[100,0],[100,20],[0,20]]}
      ],
      "engine": "RapidOCR 3.10.0 / ONNX Runtime CPU"
    }

图片无文字时成功返回空 text 和空 lines。调用方设置 OCR 请求总超时；超时或断开不会强制中止已进入本地推理的计算。

测试不加载真实模型、不访问外网：

    ./.runtime/ocr/Scripts/python.exe -m unittest discover -s ocr -p "test_*.py" -v

Dockerfile 的构建上下文为独立项目根，例如 docker build -f ocr/Dockerfile -t comment-console-ocr:1.0.0 .。

## 来源与许可证

- [RapidOCR 官方安装文档](https://rapidai.github.io/RapidOCRDocs/main/en/install_usage/rapidocr/install/)：默认 ONNX CPU 路径、随 wheel 附带的模型与 headless OpenCV 说明。
- [RapidOCR 3.10.0 发布页](https://pypi.org/project/rapidocr/3.10.0/)：固定版本及 Apache-2.0 标识。
- [RapidOCR Apache-2.0 LICENSE](https://github.com/RapidAI/RapidOCR/blob/v3.10.0/LICENSE)。
- [ONNX Runtime 1.23.2](https://pypi.org/project/onnxruntime/1.23.2/) 使用 MIT 许可证。
- [Pillow 12.2.0](https://pypi.org/project/pillow/12.2.0/) 使用 MIT-CMU 许可证。

模型随 RapidOCR 官方发行物分发；本项目使用其随包模型，不加入第三方来源权重。分发依赖时应同时保留其发行物中的许可证和版权声明。
