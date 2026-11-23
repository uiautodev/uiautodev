#!/bin/bash
set -e

# Color codes
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 版本号来源优先级：环境变量 VERSION > dist/release.json > version/version.json（go generate 产物）
if [ -z "${VERSION:-}" ] && [ -f ./dist/release.json ]; then
    VERSION=$(jq -r '.version // empty' ./dist/release.json)
fi
if [ -z "${VERSION:-}" ] && [ -f ./version/version.json ]; then
    VERSION=$(jq -r '.version // empty' ./version/version.json)
fi

if [ -z "${VERSION:-}" ]; then
    echo "无法确定版本号：请设置 VERSION，或先下载 release 产物 / 执行 go generate ./version" >&2
    exit 1
fi

if [ ! -f ./dist/release.json ]; then
    echo "dist/release.json is missing; run build.sh or download the release assets first" >&2
    exit 1
fi

echo -e "Uploading version: ${GREEN}${VERSION}${NC}"

# 凭据：S3_* / R2_* 环境变量优先，否则读 ~/.config/rclone/rclone.conf 的 r2 remote
go run ./cmd/release upload \
    -dir ./dist \
    -version "$VERSION" \
    -bucket "${S3_BUCKET:-uiautodev}" \
    -latest-key updates/latest.json \
    -latest-guard-url "${LATEST_GUARD_URL:-https://dl.uiauto.dev/updates/latest.json}"

echo -e "${GREEN}Upload complete!${NC} Version: ${YELLOW}${VERSION}${NC}"
