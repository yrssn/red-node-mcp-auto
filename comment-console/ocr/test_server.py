import http.client
import io
import json
from pathlib import Path
import struct
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zlib

from PIL import Image

import server


def image_bytes(image_format="PNG"):
    buffer = io.BytesIO()
    Image.new("RGB", (12, 8), "white").save(buffer, format=image_format)
    return buffer.getvalue()


def large_png(width, height):
    data = bytearray(image_bytes())
    data[16:24] = struct.pack(">II", width, height)
    data[29:33] = struct.pack(">I", zlib.crc32(data[12:29]))
    return bytes(data)


class FakeEngine:
    def __init__(self):
        self.calls = 0

    def __call__(self, picture):
        self.calls += 1
        return SimpleNamespace(
            txts=("计算机 Java", "Spring Boot 求助"),
            scores=(0.98, 0.95),
            boxes=(
                ((0, 0), (10, 0), (10, 3), (0, 3)),
                ((0, 4), (10, 4), (10, 7), (0, 7)),
            ),
        )


class ImageValidationTests(unittest.TestCase):
    def test_supported_formats_and_octet_stream(self):
        for mime, image_format in server.FORMATS.items():
            with self.subTest(mime=mime):
                data = image_bytes(image_format)
                result = server.decode_image(data, mime)
                self.assertEqual(result.size, (12, 8))
                self.assertEqual(result.mode, "RGB")
                self.assertEqual(server.decode_image(data, "application/octet-stream").size, (12, 8))

    def test_rejects_urls_svg_empty_and_bad_bytes(self):
        for data in (b"", b"https://example.com/image.png", b"/tmp/private.png", b"<svg/>", b"broken image"):
            with self.subTest(data=data):
                with self.assertRaises(server.OCRRequestError):
                    server.decode_image(data, "application/octet-stream")

    def test_rejects_wrong_mime_and_content_type(self):
        for mime in ("image/jpeg", "text/plain", "application/json"):
            with self.subTest(mime=mime):
                with self.assertRaises(server.OCRRequestError):
                    server.decode_image(image_bytes(), mime)

    def test_rejects_large_byte_payload(self):
        with self.assertRaises(server.OCRRequestError) as error:
            server.decode_image(b"x" * (server.MAX_IMAGE_BYTES + 1), "image/png")
        self.assertEqual(error.exception.status, 413)

    def test_rejects_dimensions_before_pixel_allocation(self):
        for width, height in ((16385, 1), (4001, 4000)):
            with self.subTest(dimensions=(width, height)):
                with self.assertRaises(server.OCRRequestError) as error:
                    server.decode_image(large_png(width, height), "image/png")
                self.assertEqual(error.exception.status, 413)

    def test_rejects_corrupt_png_data(self):
        data = bytearray(image_bytes())
        data[41] ^= 0xFF
        with self.assertRaises(server.OCRRequestError):
            server.decode_image(bytes(data), "image/png")

    def test_transparency_has_white_background(self):
        buffer = io.BytesIO()
        Image.new("RGBA", (2, 2), (0, 0, 0, 0)).save(buffer, format="PNG")
        picture = server.decode_image(buffer.getvalue(), "image/png")
        self.assertEqual(picture.getpixel((0, 0)), (255, 255, 255))


class ServiceTests(unittest.TestCase):
    def test_result_shape_and_chinese_text(self):
        engine = FakeEngine()
        service = server.OCRService(engine)
        result = service.recognize(image_bytes(), "image/png")
        self.assertEqual(result["text"], "计算机 Java\nSpring Boot 求助")
        self.assertEqual(result["engine"], server.ENGINE_NAME)
        self.assertEqual(result["lines"][0]["score"], 0.98)
        self.assertEqual(len(result["lines"][0]["box"]), 4)
        self.assertEqual(engine.calls, 1)

    def test_empty_result_is_success(self):
        service = server.OCRService(lambda _picture: SimpleNamespace(txts=None, boxes=None, scores=None))
        result = service.recognize(image_bytes(), "image/png")
        self.assertEqual(result["text"], "")
        self.assertEqual(result["lines"], [])

    def test_only_one_ocr_request_runs_at_once(self):
        service = server.OCRService(FakeEngine())
        service.lock.acquire()
        try:
            with self.assertRaises(server.OCRRequestError) as error:
                service.recognize(image_bytes(), "image/png")
            self.assertEqual(error.exception.status, 429)
        finally:
            service.lock.release()
        self.assertTrue(service.recognize(image_bytes(), "image/png")["text"])

    def test_lock_is_released_after_invalid_image(self):
        service = server.OCRService(FakeEngine())
        with self.assertRaises(server.OCRRequestError):
            service.recognize(b"bad", "image/png")
        self.assertTrue(service.recognize(image_bytes(), "image/png")["text"])

    def test_model_paths_are_local_and_missing_models_fail_early(self):
        with tempfile.TemporaryDirectory() as directory:
            package = SimpleNamespace(__file__=str(Path(directory) / "__init__.py"))
            with patch.dict(sys.modules, {"rapidocr": package}):
                package.RapidOCR = lambda **_kwargs: self.fail("缺少权重时仍创建推理引擎")
                with self.assertRaisesRegex(RuntimeError, "缺少"):
                    server.load_engine()

    def test_bundled_model_paths_and_warmup_use_single_engine(self):
        with tempfile.TemporaryDirectory() as directory:
            model_directory = Path(directory) / "models"
            model_directory.mkdir()
            for name in server.MODEL_FILES.values():
                (model_directory / name).write_bytes(b"test model placeholder")
            created = []

            class ProbeEngine:
                def __init__(self, params):
                    self.params = params
                    self.warmup_calls = []
                    created.append(self)

                def __call__(self, _picture, **options):
                    self.warmup_calls.append(options)

            package = SimpleNamespace(__file__=str(Path(directory) / "__init__.py"), RapidOCR=ProbeEngine)
            with patch.dict(sys.modules, {"rapidocr": package}):
                engine = server.load_engine()
            self.assertEqual(len(created), 1)
            self.assertEqual(len(engine.warmup_calls), 2)
            self.assertFalse(engine.params["EngineConfig.onnxruntime.use_cuda"])
            for key, name in server.MODEL_FILES.items():
                self.assertEqual(engine.params[key], str(model_directory / name))


class HTTPTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.engine = FakeEngine()
        cls.service = server.OCRService(cls.engine)
        cls.http = server.OCRHTTPServer(("127.0.0.1", 0), cls.service)
        cls.thread = threading.Thread(target=cls.http.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.http.shutdown()
        cls.http.server_close()
        cls.thread.join(timeout=3)

    def request(self, method, path, body=None, headers=None):
        connection = http.client.HTTPConnection(*self.http.server_address, timeout=3)
        try:
            connection.request(method, path, body=body, headers=headers or {})
            response = connection.getresponse()
            return response.status, dict(response.getheaders()), json.loads(response.read())
        finally:
            connection.close()

    def test_health_is_available_during_busy_ocr(self):
        self.service.lock.acquire()
        try:
            status, headers, value = self.request("GET", "/health")
            self.assertEqual(status, 200)
            self.assertTrue(value["ok"])
            self.assertEqual(headers["Cache-Control"], "no-store")
        finally:
            self.service.lock.release()

    def test_raw_image_request(self):
        status, _headers, result = self.request("POST", "/ocr", image_bytes(), {"Content-Type": "application/octet-stream"})
        self.assertEqual(status, 200)
        self.assertEqual(len(result["lines"]), 2)

    def test_rejects_json_url_input_without_calling_engine(self):
        before = self.engine.calls
        status, _headers, _result = self.request("POST", "/ocr", b'{"url":"https://example.com/private"}', {"Content-Type": "application/json"})
        self.assertEqual(status, 415)
        self.assertEqual(self.engine.calls, before)

    def test_oversized_length_is_rejected_without_reading_body(self):
        status, _headers, _result = self.request("POST", "/ocr", b"", {"Content-Type": "image/png", "Content-Length": str(server.MAX_IMAGE_BYTES + 1)})
        self.assertEqual(status, 413)

    def test_query_or_file_path_is_not_an_endpoint(self):
        for path in ("/ocr?url=https://example.com/x.png", "/etc/passwd"):
            with self.subTest(path=path):
                self.assertEqual(self.request("GET", path)[0], 404)

    def test_busy_returns_retryable_status(self):
        self.service.lock.acquire()
        try:
            status, headers, _result = self.request("POST", "/ocr", image_bytes(), {"Content-Type": "image/png"})
            self.assertEqual(status, 429)
            self.assertEqual(headers["Retry-After"], "1")
        finally:
            self.service.lock.release()


if __name__ == "__main__":
    unittest.main()
