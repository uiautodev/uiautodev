#!/usr/bin/env bash
#
# 下载构建所需的 adb / ios 等二进制到 ./binaries。
#
# 用法:
#   scripts/download-binaries.sh [darwin|windows|all]
#
# 默认按当前系统选择目标；CI 里直接传目标平台。默认从公开的
# https://dl.uiauto.dev/binaries 拉取，避免依赖 rclone 与 R2 凭据。
# 需要更新二进制本身时，用 rclone 直接上传到 r2:uiautodev/binaries/，
# 新增文件后记得同步更新下面的文件清单。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${BINARIES_BASE_URL:-https://dl.uiauto.dev/binaries}"
DEST="${BINARIES_DIR:-$ROOT/binaries}"

usage() {
    echo "usage: $0 {darwin|windows|all}" >&2
    exit 1
}

target="${1:-}"
if [ -z "$target" ]; then
    case "$(uname -s)" in
        Darwin) target=darwin ;;
        MINGW*|MSYS*|CYGWIN*) target=windows ;;
        *) usage ;;
    esac
fi

fetch() {
    local path="$1"
    local out="$DEST/$path"
    mkdir -p "$(dirname "$out")"
    echo "downloading $BASE_URL/$path"
    curl -fL --retry 3 --retry-delay 2 --progress-bar "$BASE_URL/$path" -o "$out"
    chmod +x "$out" 2>/dev/null || true
}

download_darwin() {
    fetch darwin/adb
    fetch darwin/ios
}

download_windows() {
    fetch windows/adb.exe
    fetch windows/AdbWinApi.dll
    fetch windows/AdbWinUsbApi.dll
    fetch windows/ios.exe
}

case "$target" in
    darwin) download_darwin ;;
    windows) download_windows ;;
    all)
        download_darwin
        download_windows
        ;;
    *) usage ;;
esac

echo "binaries ready in $DEST"
