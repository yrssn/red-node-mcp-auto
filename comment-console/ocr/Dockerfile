FROM python:3.11-slim-bookworm
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    BIND=0.0.0.0 \
    PORT=18063
WORKDIR /app/ocr
RUN apt-get update && apt-get install -y --no-install-recommends libglib2.0-0 libgomp1 \
    && rm -rf /var/lib/apt/lists/*
COPY ocr/requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY ocr/server.py ./
# 构建时验证 wheel 已带齐权重，运行时不依赖模型下载。
RUN python -c "from server import load_engine; load_engine()"
EXPOSE 18063
HEALTHCHECK --interval=15s --timeout=3s --start-period=90s --retries=5 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:18063/health', timeout=2)" || exit 1
CMD ["python", "server.py"]
