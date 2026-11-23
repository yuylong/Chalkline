#!/bin/sh
# 首次启动若无证书则生成自签名证书，保证 `docker compose up -d` 开箱即用；
# 生产环境请将正式证书放置于 deploy/certs/（chalkline.crt / chalkline.key）。
set -e

CERT_DIR=/etc/nginx/certs
CN="${CERT_CN:-localhost}"

# 预留 HTTP-01 webroot 目录（nginx 放行 /.well-known/acme-challenge/）。
# 目录只读时仅告警，不阻断启动（自签证书场景用不到该目录）。
if ! mkdir -p "$CERT_DIR/acme/.well-known/acme-challenge" 2>/dev/null; then
    echo "警告：无法创建 ACME webroot 目录（$CERT_DIR/acme），HTTP-01 校验将不可用。" >&2
fi

if [ ! -f "$CERT_DIR/chalkline.crt" ] || [ ! -f "$CERT_DIR/chalkline.key" ]; then
    echo "未发现 TLS 证书，生成自签名证书 (CN=$CN) ..."
    mkdir -p "$CERT_DIR"
    if ! command -v openssl >/dev/null 2>&1; then
        apk add --no-cache openssl >/dev/null
    fi
    openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
        -keyout "$CERT_DIR/chalkline.key" \
        -out "$CERT_DIR/chalkline.crt" \
        -subj "/CN=$CN" \
        -addext "subjectAltName=DNS:$CN,DNS:localhost,IP:127.0.0.1"
    echo "自签名证书已生成。生产环境请替换为正式证书。"
fi

exec "$@"
