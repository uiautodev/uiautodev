#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
OUT="build/dmg"
PAGE="file://$(pwd)/build/dmg/background.html"

render() {
    local scale="$1"
    local file="$2"
    "$CHROME" \
        --headless=new \
        --disable-gpu \
        --hide-scrollbars \
        --window-size=720,440 \
        --force-device-scale-factor="$scale" \
        --screenshot="$file" \
        "$PAGE" >/dev/null 2>&1
}

render 1 "$OUT/background.png"
render 2 "$OUT/background@2x.png"
echo "generated $OUT/background.png and $OUT/background@2x.png"
