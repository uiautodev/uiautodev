#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log()  { echo -e "$@"; }
info() { log "${BLUE}[INFO]${NC} $@"; }
ok()   { log "${GREEN}[OK]${NC} $@"; }
warn() { log "${YELLOW}[WARN]${NC} $@"; }
err()  { log "${RED}[ERROR]${NC} $@"; }

# 发布流程：打 tag -> push -> 触发 .github/workflows/release.yml
# 构建与 GitHub Release 由 Actions 完成；R2 / 更新清单按需在本地同步。
WORKFLOW_FILE="release.yml"

cleanup_tag() {
    if [ -n "${NEW_VERSION:-}" ] && git rev-parse --verify "refs/tags/$NEW_VERSION" >/dev/null 2>&1; then
        warn "推送失败，删除本地 tag: $NEW_VERSION"
        git tag -d "$NEW_VERSION"
    fi
}

# Step 1: 确认 master 分支并拉取最新代码
info "Step 1: 确认分支并拉取最新代码..."
BRANCH=$(git branch --show-current)
if [ "$BRANCH" != "master" ]; then
    info "当前在 $BRANCH，切换到 master..."
    git checkout master
fi
git pull
ok "代码已更新"

# Step 2: 获取最新 tag 并推荐版本号
info "Step 2: 获取最新 tag..."
LATEST_TAG=$(git tag --sort=-v:refname | head -1)
if [ -z "$LATEST_TAG" ]; then
    LATEST_TAG="0.0.0"
fi
info "最新 tag: ${GREEN}$LATEST_TAG${NC}"

COMMITS=$(git log "$LATEST_TAG..HEAD" --oneline 2>/dev/null || true)
if [ -z "$COMMITS" ]; then
    warn "没有新 commit，已是最新版本，退出"
    exit 0
fi
echo "$COMMITS"
echo ""

HAS_FEAT=$(echo "$COMMITS" | grep -iE "^[a-f0-9]+ feat[:\(]" || true)
IFS='.' read -r MAJOR MINOR PATCH <<< "${LATEST_TAG#v}"

if [ -n "$HAS_FEAT" ]; then
    MINOR=$((MINOR + 1))
    PATCH=0
else
    PATCH=$((PATCH + 1))
fi
RECOMMENDED="${MAJOR}.${MINOR}.${PATCH}"
info "推荐版本号: ${GREEN}$RECOMMENDED${NC}"

# Step 3: 确认版本号
NEW_VERSION="${VERSION:-}"
if [ -z "$NEW_VERSION" ]; then
    read -p "输入版本号 [回车确认: $RECOMMENDED]: " INPUT_VERSION
    NEW_VERSION="${INPUT_VERSION:-$RECOMMENDED}"
fi

if ! echo "$NEW_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    err "版本号格式无效: $NEW_VERSION (需要 x.y.z)"
    exit 1
fi
if git rev-parse --verify "refs/tags/$NEW_VERSION" >/dev/null 2>&1; then
    err "tag $NEW_VERSION 已存在"
    exit 1
fi

# Step 4: 打 tag 并推送，触发 GitHub Actions
info "Step 4: 创建并推送 tag $NEW_VERSION..."
trap cleanup_tag EXIT
git tag -a "$NEW_VERSION" -m "release $NEW_VERSION"
git push origin "$NEW_VERSION"
trap - EXIT
ok "tag 已推送，Actions 开始构建"

# Step 5: 等待 workflow 完成
info "Step 5: 等待 release workflow..."
RUN_ID=""
for _ in $(seq 1 30); do
    RUN_ID=$(gh run list --workflow="$WORKFLOW_FILE" --limit 20 \
        --json databaseId,headBranch \
        -q "[.[] | select(.headBranch == \"$NEW_VERSION\")][0].databaseId // empty")
    if [ -n "$RUN_ID" ]; then
        break
    fi
    sleep 5
done

if [ -z "$RUN_ID" ]; then
    warn "未找到工作流运行记录，请到 Actions 页面查看"
else
    gh run watch "$RUN_ID" --interval 20 --exit-status || {
        err "workflow 失败，日志: gh run view $RUN_ID --log-failed"
        exit 1
    }
    ok "构建完成: $(gh release view "$NEW_VERSION" --json url -q .url)"
fi

# Step 6: R2 已由 workflow 自动同步；SYNC_R2=1 时可再手动重推一次
if [ "${SYNC_R2:-}" = "1" ]; then
    info "Step 6: 手动重推 R2..."
    if [ -d ./dist ]; then
        rm -rf ./dist
    fi
    export HTTPS_PROXY="${HTTPS_PROXY:-http://127.0.0.1:10808}"
    gh release download "$NEW_VERSION" -D ./dist --clobber
    ./upload.sh
    curl -fsSL -X POST https://get.uiauto.dev/api/cache/refresh >/dev/null
    ok "R2 与更新清单已重新同步"
else
    ok "R2 与更新清单已由 workflow 同步"
fi

echo ""
log "========================================="
log " ${GREEN}Release $NEW_VERSION 完成!${NC}"
log " Release URL: $(gh release view "$NEW_VERSION" --json url -q .url 2>/dev/null || echo "https://github.com/$(gh repo view --json nameWithOwner -q .nameWithOwner)/releases/tag/$NEW_VERSION")"
log "========================================="
