# syntax=docker/dockerfile:1.6

FROM node:22-bookworm-slim AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24 AS builder
WORKDIR /app/backend
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG VERSION=v1.0.0
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X xhs-comment-console/internal/server.version=${VERSION}" -o /out/comment-console ./cmd/server

FROM ubuntu:22.04
ARG DEBIAN_FRONTEND=noninteractive
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
WORKDIR /app/backend
RUN apt-get update && apt-get install -y ca-certificates wget gnupg && \
    sed -i 's|http://archive.ubuntu.com|https://mirrors.aliyun.com|g' /etc/apt/sources.list && \
    sed -i 's|http://security.ubuntu.com|https://mirrors.aliyun.com|g' /etc/apt/sources.list
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl fonts-liberation fonts-noto-color-emoji fonts-unifont \
    fonts-freefont-ttf fonts-wqy-zenhei libasound2 libatk-bridge2.0-0 libatk1.0-0 \
    libc6 libcairo2 libcairo-gobject2 libcups2 libdbus-1-3 libdrm2 libexpat1 \
    libfontconfig1 libgdk-pixbuf-2.0-0 libgbm1 libgcc1 libglib2.0-0 libgtk-3-0 \
    libnspr4 libnss3 libpango-1.0-0 libpangocairo-1.0-0 libstdc++6 libx11-6 \
    libx11-xcb1 libxcb1 libxcomposite1 libxcursor1 libxdamage1 libxext6 libxfixes3 \
    libxi6 libxkbcommon0 libxrandr2 libxrender1 libxshmfence1 libxss1 libxtst6 \
    lsb-release tini tzdata wget xdg-utils xz-utils && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /app/backend/data/home /app/backend/data/config /app/backend/config /app/backend/images /app/bin && \
    chmod -R 777 /app/backend/data /app/backend/images
# 浏览器缓存与运行数据卷分离，挂载账号数据时不会覆盖预置浏览器。
ENV XDG_CACHE_HOME=/app/cache
COPY backend/internal/browser/browser_version.txt /tmp/browser_version.txt
RUN VER="$(cat /tmp/browser_version.txt | tr -d '[:space:]')" && \
    BASE="https://cdn.one-world.ai/browsers/${VER}" && \
    BROWSER_DIR="${XDG_CACHE_HOME}/xiaohongshu-mcp/browser/${VER}" && \
    mkdir -p "${BROWSER_DIR}" && \
    curl --retry 3 --connect-timeout 20 -fsSL -o /tmp/browser.tar.xz "${BASE}/linux-x64.tar.xz" && \
    curl --retry 3 --connect-timeout 20 -fsSL "${BASE}/SHA256SUMS" | grep " linux-x64.tar.xz$" | awk '{print $1"  /tmp/browser.tar.xz"}' | sha256sum -c - && \
    tar -xJf /tmp/browser.tar.xz -C "${BROWSER_DIR}" --strip-components=1 && \
    rm /tmp/browser.tar.xz /tmp/browser_version.txt && \
    test -x "${BROWSER_DIR}/chrome" && chmod -R 755 /app/cache
COPY --from=builder /out/comment-console /app/bin/comment-console
COPY --from=frontend /app/frontend/dist /app/frontend/dist
COPY LICENSE /app/LICENSE
COPY docs/third-party.md /app/THIRD_PARTY_NOTICES.md
COPY docs/licenses/ /app/licenses/
COPY deploy/console-entrypoint.sh /usr/local/bin/comment-console-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/comment-console-entrypoint && \
    chmod +x /usr/local/bin/comment-console-entrypoint
ENV HOME=/app/backend/data/home
ENV XDG_CONFIG_HOME=/app/backend/data/config
ENV COOKIES_PATH=/app/backend/data/cookies.json
EXPOSE 18060
HEALTHCHECK --interval=15s --timeout=5s --start-period=90s --retries=5 \
    CMD curl -fsS http://127.0.0.1:18060/health >/dev/null || exit 1
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/usr/local/bin/comment-console-entrypoint"]
CMD []
