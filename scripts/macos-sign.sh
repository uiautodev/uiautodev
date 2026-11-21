#!/usr/bin/env bash
#
# macOS 代码签名 / 公证脚本
#
# 用法:
#   scripts/macos-sign.sh sign     "build/bin/Uiautodev Desktop.app"
#   scripts/macos-sign.sh notarize "dist/UiautodevDesktop-mac-x.y.z.dmg"
#
# 默认使用仓库维护者的 Developer ID 证书与公证 profile，均可通过环境变量覆盖：
#
#   CODESIGN_IDENTITY   签名证书，默认 "Developer ID Application: Shengxiang Sun (74Z4C6R8NT)"
#   NOTARY_PROFILE      公证 profile，默认 uiautodev-notary
#
# 公证凭据按以下顺序选择：
#   APPLE_API_KEY + APPLE_API_KEY_ID + APPLE_API_ISSUER   App Store Connect API Key（CI 使用）
#   APPLE_ID + APPLE_APP_PASSWORD + APPLE_TEAM_ID          Apple ID + App 专用密码
#   NOTARY_PROFILE                                       钥匙串里的 notarytool profile
#
# 证书不存在时退化为 ad-hoc 签名；没有可用凭据时只签名不公证
# （REQUIRE_NOTARIZATION=1 可强制要求公证）。
set -euo pipefail

# 默认使用本机已安装的 Developer ID 证书和钥匙串里的公证 profile
DEFAULT_IDENTITY="Developer ID Application: Shengxiang Sun (74Z4C6R8NT)"
export NOTARY_PROFILE="${NOTARY_PROFILE-uiautodev-notary}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[sign]${NC} $*"; }
warn() { echo -e "${YELLOW}[sign]${NC} $*"; }
err()  { echo -e "${RED}[sign]${NC} $*" >&2; }

usage() {
    echo "usage: $0 {sign <app>|notarize <dmg>}"
    exit 1
}

detect_identity() {
    if [ -n "${CODESIGN_IDENTITY:-}" ]; then
        echo "$CODESIGN_IDENTITY"
        return
    fi
    local ids
    ids="$(security find-identity -v -p codesigning 2>/dev/null \
        | sed -n 's/.*"\(Developer ID Application: .*\)"/\1/p')"
    if printf '%s\n' "$ids" | grep -qxF "$DEFAULT_IDENTITY"; then
        echo "$DEFAULT_IDENTITY"
        return
    fi
    printf '%s\n' "$ids" | head -1
}

sign_app() {
    local app="$1"
    [ -d "$app" ] || { err "app not found: $app"; exit 1; }

    local identity
    identity="$(detect_identity)"
    if [ -z "$identity" ] || [ "$identity" = "-" ]; then
        identity="-"
        warn "未找到 Developer ID Application 证书，使用 ad-hoc 签名"
        warn "ad-hoc 签名可以修复“包已损坏”，但下载后仍会被 Gatekeeper 拦截，正式分发需要公证"
    fi

    local args=(--force --sign "$identity" --options runtime)
    if [ "$identity" != "-" ]; then
        args+=(--timestamp)
    fi

    # 先签包内的可执行文件（adb、ios 等），否则会被外层的 seal 判为无效。
    # 主可执行文件由最后签整个 bundle 时处理，单独签会因包内其它未签名文件而失败。
    local main_exe
    main_exe="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app/Contents/Info.plist")"

    while IFS= read -r -d '' file; do
        [ "$(basename "$file")" = "$main_exe" ] && continue
        if file -b "$file" 2>/dev/null | grep -q "Mach-O"; then
            info "signing $(basename "$file")"
            codesign "${args[@]}" "$file"
        fi
    done < <(find "$app/Contents" -type f -print0)

    info "signing $(basename "$app")"
    codesign "${args[@]}" "$app"

    codesign --verify --deep --strict --verbose=2 "$app"
    info "签名完成: $identity"
}

notarize() {
    local file="$1"
    [ -f "$file" ] || { err "file not found: $file"; exit 1; }

    local identity
    identity="$(detect_identity)"
    if [ -z "$identity" ] || [ "$identity" = "-" ]; then
        if [ "${REQUIRE_NOTARIZATION:-0}" = "1" ]; then
            err "没有 Developer ID Application 证书，无法公证"
            exit 1
        fi
        warn "没有 Developer ID Application 证书，跳过公证"
        return
    fi

    local creds=()
    if [ -n "${APPLE_API_KEY:-}" ] && [ -n "${APPLE_API_KEY_ID:-}" ] && [ -n "${APPLE_API_ISSUER:-}" ]; then
        # App Store Connect API Key（CI 推荐）：p8 文件路径 + Key ID + Issuer ID
        creds=(--key "$APPLE_API_KEY" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER")
    elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_APP_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
        creds=(--apple-id "$APPLE_ID" --password "$APPLE_APP_PASSWORD" --team-id "$APPLE_TEAM_ID")
    elif [ -n "${NOTARY_PROFILE:-}" ]; then
        creds=(--keychain-profile "$NOTARY_PROFILE")
        local login_keychain="$HOME/Library/Keychains/login.keychain-db"
        [ -f "$login_keychain" ] && creds+=(--keychain "$login_keychain")
    else
        if [ "${REQUIRE_NOTARIZATION:-0}" = "1" ]; then
            err "缺少公证凭据，请设置 APPLE_API_KEY/APPLE_API_KEY_ID/APPLE_API_ISSUER、APPLE_ID/APPLE_APP_PASSWORD/APPLE_TEAM_ID 或 NOTARY_PROFILE"
            exit 1
        fi
        warn "缺少公证凭据，跳过公证"
        return
    fi

    case "$file" in
        *.dmg)
            info "signing $(basename "$file")"
            codesign --force --sign "$identity" --timestamp "$file"
            ;;
    esac

    info "提交公证: ${file}（可能需要几分钟）"
    xcrun notarytool submit "$file" "${creds[@]}" --wait
    xcrun stapler staple "$file"
    xcrun stapler validate "$file"
    info "公证完成并已 staple"
}

case "${1:-}" in
    sign)
        [ -n "${2:-}" ] || usage
        sign_app "$2"
        ;;
    notarize)
        [ -n "${2:-}" ] || usage
        notarize "$2"
        ;;
    *)
        usage
        ;;
esac
