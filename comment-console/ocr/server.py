"""本地 OCR 辅助服务：只接收图片字节，不接受网址或文件路径。"""

from __future__ import annotations

import io
import json
import logging
import os
from pathlib import Path
import signal
import socket
import threading
import warnings
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from PIL import Image, ImageOps, UnidentifiedImageError

ENGINE_NAME = "RapidOCR 3.10.0 / ONNX Runtime CPU"
MAX_IMAGE_BYTES = 10 * 1024 * 1024
MAX_IMAGE_PIXELS = 16_000_000
MAX_IMAGE_SIDE = 16_384
Image.MAX_IMAGE_PIXELS = MAX_IMAGE_PIXELS
FORMATS = {"image/png": "PNG", "image/jpeg": "JPEG", "image/webp": "WEBP"}
MODEL_FILES = {
    "Det.model_path": "PP-OCRv6_det_small.onnx",
    "Cls.model_path": "ch_ppocr_mobile_v2.0_cls_mobile.onnx",
    "Rec.model_path": "PP-OCRv6_rec_small.onnx",
}


class OCRRequestError(Exception):
    def __init__(self, message: str, status: int = 400):
        super().__init__(message)
        self.status = status


def decode_image(data: bytes, content_type: str) -> Image.Image:
    if not data:
        raise OCRRequestError("图片不能为空")
    if len(data) > MAX_IMAGE_BYTES:
        raise OCRRequestError("图片不能超过 10 MiB", 413)
    if content_type not in (*FORMATS, "application/octet-stream"):
        raise OCRRequestError("仅接收 PNG、JPEG、WebP 图片字节", 415)
    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            with Image.open(io.BytesIO(data)) as source:
                actual_format = source.format
                if actual_format not in FORMATS.values():
                    raise OCRRequestError("实际图片格式须为 PNG、JPEG 或 WebP", 415)
                if content_type in FORMATS and FORMATS[content_type] != actual_format:
                    raise OCRRequestError("图片格式与 Content-Type 不一致", 415)
                width, height = source.size
                if (
                    width <= 0
                    or height <= 0
                    or max(width, height) > MAX_IMAGE_SIDE
                    or width * height > MAX_IMAGE_PIXELS
                ):
                    raise OCRRequestError("图片不能超过 1600 万像素或 16384 像素最长边", 413)
                if getattr(source, "n_frames", 1) != 1:
                    raise OCRRequestError("请使用静态图片，暂不识别动画", 415)
                source.verify()
            with Image.open(io.BytesIO(data)) as source:
                source.load()
                picture = ImageOps.exif_transpose(source)
                if "A" in picture.getbands() or "transparency" in picture.info:
                    rgba = picture.convert("RGBA")
                    background = Image.new("RGBA", rgba.size, "white")
                    picture = Image.alpha_composite(background, rgba)
                return picture.convert("RGB")
    except OCRRequestError:
        raise
    except (Image.DecompressionBombError, Image.DecompressionBombWarning):
        raise OCRRequestError("图片像素过大", 413) from None
    except (UnidentifiedImageError, OSError, SyntaxError, ValueError):
        raise OCRRequestError("图片损坏或无法解码") from None


def load_engine():
    # 使用 wheel 自带权重的明确路径；缺失时拒绝启动，不触发自动下载。
    import rapidocr
    from rapidocr import RapidOCR

    model_directory = Path(rapidocr.__file__).resolve().parent / "models"
    params = {
        "Global.log_level": "error",
        "EngineConfig.onnxruntime.use_cuda": False,
        "EngineConfig.onnxruntime.use_dml": False,
        "EngineConfig.onnxruntime.intra_op_num_threads": 2,
        "EngineConfig.onnxruntime.inter_op_num_threads": 1,
    }
    for key, name in MODEL_FILES.items():
        path = model_directory / name
        if not path.is_file():
            raise RuntimeError(f"缺少随包附带的 OCR 权重：{name}，请重新安装固定版本 rapidocr")
        params[key] = str(path)
    engine = RapidOCR(params=params)
    # 通过公开调用预热三种模型，之后复用同一实例。
    probe = Image.new("RGB", (320, 48), "white")
    engine(probe, use_det=False, use_cls=True, use_rec=True)
    engine(probe, use_det=True, use_cls=True, use_rec=True)
    return engine


class OCRService:
    def __init__(self, engine):
        self.engine = engine
        self.lock = threading.Lock()

    def recognize(self, data: bytes, content_type: str) -> dict:
        # 不堆积大图和推理任务；并发调用方收到 429 后可稍后重试。
        if not self.lock.acquire(blocking=False):
            raise OCRRequestError("OCR 正忙，请稍后重试", 429)
        try:
            picture = decode_image(data, content_type)
            result = self.engine(picture)
            lines = []
            if result.txts is not None and result.boxes is not None:
                for text, score, box in zip(result.txts, result.scores, result.boxes):
                    if not str(text).strip():
                        continue
                    lines.append(
                        {
                            "text": str(text),
                            "score": float(score),
                            "box": [[float(x), float(y)] for x, y in box],
                        }
                    )
            return {"text": "\n".join(line["text"] for line in lines), "lines": lines, "engine": ENGINE_NAME}
        finally:
            self.lock.release()


class OCRHTTPServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address, service: OCRService):
        self.service = service
        super().__init__(address, OCRHandler)


class OCRHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"
    server_version = "LocalOCR/1.0"
    sys_version = ""

    def setup(self):
        super().setup()
        self.connection.settimeout(15)

    def json_response(self, status: int, value: dict):
        body = json.dumps(value, ensure_ascii=False, allow_nan=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Connection", "close")
        if status == 429:
            self.send_header("Retry-After", "1")
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError, socket.timeout):
            pass
        self.close_connection = True

    def do_GET(self):
        if self.path == "/health":
            self.json_response(200, {"ok": True, "engine": ENGINE_NAME})
        else:
            self.json_response(404, {"error": "接口不存在"})

    def do_POST(self):
        if self.path != "/ocr":
            self.json_response(404, {"error": "接口不存在"})
            return
        try:
            lengths = self.headers.get_all("Content-Length", [])
            if self.headers.get("Transfer-Encoding") or len(lengths) != 1:
                raise OCRRequestError("须使用单一 Content-Length 发送原始图片字节")
            try:
                length = int(lengths[0])
            except ValueError:
                raise OCRRequestError("Content-Length 无效") from None
            if length <= 0 or length > MAX_IMAGE_BYTES:
                raise OCRRequestError("图片大小须为 1 字节至 10 MiB", 413)
            data = self.rfile.read(length)
            if len(data) != length:
                raise OCRRequestError("图片数据不完整")
            content_type = self.headers.get_content_type()
            result = self.server.service.recognize(data, content_type)
            self.json_response(200, result)
        except OCRRequestError as error:
            self.json_response(error.status, {"error": str(error)})
        except (socket.timeout, TimeoutError):
            self.json_response(408, {"error": "读取图片超时"})
        except Exception as error:
            # 不记录图片、识别文字或调用方传入的内容。
            logging.error("OCR 执行异常：%s", type(error).__name__)
            self.json_response(500, {"error": "OCR 识别失败，请检查图片或稍后重试"})

    def log_message(self, _format, *_args):
        pass


def main():
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    try:
        port = int(os.environ.get("PORT", "18063"))
        if not 1 <= port <= 65535:
            raise ValueError("PORT 范围须为 1–65535")
        service = OCRService(load_engine())
        server = OCRHTTPServer((os.environ.get("BIND", "127.0.0.1"), port), service)
    except Exception as error:
        logging.error("OCR 启动失败：%s", str(error))
        raise SystemExit(1) from None

    def stop(_signum, _frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    logging.info("本地 OCR 已就绪：%s:%d", server.server_address[0], port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
