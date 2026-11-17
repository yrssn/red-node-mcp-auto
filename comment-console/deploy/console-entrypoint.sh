#!/bin/sh
set -eu

# 默认 Compose 注入独立变量；已有 MySQL 使用只读配置文件。
if [ -n "${DB_HOST:-}" ]; then
    : "${DB_USER:?DB_USER is required}"
    : "${DB_PASSWORD:?DB_PASSWORD is required}"
    : "${DB_NAME:?DB_NAME is required}"
    : "${DB_DEMO_NAME:?DB_DEMO_NAME is required}"
    if [ "$DB_NAME" = "$DB_DEMO_NAME" ]; then
        echo "Production and demo databases must be different." >&2
        exit 1
    fi
    export MYSQL_DSN="${DB_USER}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT:-3306})/${DB_NAME}?charset=utf8mb4"
    export MYSQL_DEMO_DSN="${DB_USER}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT:-3306})/${DB_DEMO_NAME}?charset=utf8mb4"
fi
case "${CONSOLE_DEMO:-false}" in
    true|false) ;;
    *) echo "CONSOLE_DEMO must be true or false." >&2; exit 1 ;;
esac
exec /app/bin/comment-console -headless=true -port=:18060 "-demo=${CONSOLE_DEMO:-false}" \
    -web-dir=/app/frontend/dist -config=/app/backend/config/mysql.json "$@"
