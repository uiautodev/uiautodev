#!/bin/bash
set -eo pipefail

# Color codes
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Log file
LOG_FILE="build.log"
VERSION=

# Remove old log file if exists
if [ -f "$LOG_FILE" ]; then
    rm "$LOG_FILE"
fi

# Logging function
log() {
    echo -e "$@" | tee -a "$LOG_FILE"
}

# Generate version
generate_version() {
    log "${BLUE}[1/7]${NC} Generating version..."
    go generate ./version ./webserver/... >> "$LOG_FILE" 2>&1
    VERSION=$(jq -r .version version/version.json)
    log "Building version: ${GREEN}${VERSION}${NC}"
}

# Clean build directories
clean_build() {
    log "${BLUE}[2/7]${NC} Cleaning build directories..."
    rm -fr ./dist/* >> "$LOG_FILE" 2>&1
    rm -fr ./cmd/uiautodev-server/frontend_build >> "$LOG_FILE" 2>&1
    rm -fr ./frontend/build >> "$LOG_FILE" 2>&1
    log "Clean complete"
}

# Build frontend
build_frontend() {
    log "${BLUE}[3/7]${NC} Building frontend..."
    cd frontend
    bun install >> "$LOG_FILE" 2>&1
    bun run build >> "$LOG_FILE" 2>&1
    cd ..
    rm -fr ./cmd/uiautodev-server/frontend_build >> "$LOG_FILE" 2>&1
    cp -r ./frontend/build ./cmd/uiautodev-server/frontend_build >> "$LOG_FILE" 2>&1
    log "Frontend build complete"
}

# Build server binaries
build_server() {
    log "${BLUE}[4/7]${NC} Building server binaries..."
    # The server is pure Go; disable CGO so every target cross-compiles
    # without a toolchain for the target platform.
    # GOOS=darwin GOARCH=arm64 go build -o ./dist/uiautodev-server-darwin-arm64-$VERSION ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o ./dist/uiautodev-server-darwin-x86_64-$VERSION ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o ./dist/uiautodev-server-darwin-arm64-$VERSION ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ./dist/uiautodev-server-windows-amd64-$VERSION.exe ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./dist/uiautodev-server-linux-x86_64-$VERSION ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ./dist/uiautodev-server-linux-arm64-$VERSION ./cmd/uiautodev-server >> "$LOG_FILE" 2>&1
    log "Server binaries build complete"
}

# Build Windows desktop app
build_windows() {
    log "${BLUE}[5/7]${NC} Building Windows desktop app..."
    rm -rf ./build/bin
    GOOS=windows GOARCH=amd64 wails3 build -tags devtools APP_NAME=uiautodev-desktop >> "$LOG_FILE" 2>&1
    mkdir -p ./build/bin/tools
    cp binaries/windows/adb.exe binaries/windows/AdbWinApi.dll binaries/windows/AdbWinUsbApi.dll ./build/bin/tools/ >> "$LOG_FILE" 2>&1
    cd build/bin
    rm -f ../../dist/UiautodevDesktop-windows-$VERSION.zip
    zip -r ../../dist/UiautodevDesktop-windows-$VERSION.zip tools *.exe -x "*.DS_Store" >> "$LOG_FILE" 2>&1
    cd -
    log "Windows build complete"
}

# Build macOS desktop app for the given arch
build_mac() {
    local arch="$1"
    local app="./build/bin/Uiautodev Desktop.app"
    local dmg
    if [ "$arch" = "amd64" ] || [ "$arch" = "universal" ]; then
        dmg="./dist/UiautodevDesktop-mac-$VERSION.dmg"
    else
        dmg="./dist/UiautodevDesktop-mac-$arch-$VERSION.dmg"
    fi
    log "${BLUE}[6/7]${NC} Building macOS $arch desktop app..."
    rm -rf ./build/bin
    if [ "$arch" = "universal" ]; then
        wails3 task darwin:package:universal EXTRA_TAGS=devtools >> "$LOG_FILE" 2>&1
    else
        wails3 task darwin:package ARCH="$arch" EXTRA_TAGS=devtools >> "$LOG_FILE" 2>&1
    fi
    /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier dev.uiauto.desktop" "$app/Contents/Info.plist" >> "$LOG_FILE" 2>&1
    cp binaries/darwin/adb "$app/Contents/MacOS/" >> "$LOG_FILE" 2>&1
    ./scripts/macos-sign.sh sign "$app" 2>&1 | tee -a "$LOG_FILE"
    dmgbuild -s dmg_settings.py -D app="$app" "Uiautodev Desktop $VERSION" "$dmg" >> "$LOG_FILE" 2>&1
    ./scripts/macos-sign.sh notarize "$dmg" 2>&1 | tee -a "$LOG_FILE"
    log "macOS $arch build complete"
}

# Generate the updater manifest for the published release assets
generate_release_manifest() {
    log "${BLUE}[7/7]${NC} Generating release manifest..."
    go run ./cmd/release manifest -dir ./dist -version "$VERSION" -tag "$VERSION" -out ./dist/release.json >> "$LOG_FILE" 2>&1
    log "Release manifest generated"
}

# Main build process
main() {
    log "========================================="
    log "Build started at $(date)"
    log "========================================="

    generate_version
    ./scripts/download-binaries.sh all
    make test
    clean_build

    touch dist/${VERSION:?}
    build_frontend
    build_server
    build_mac universal
    build_windows
    generate_release_manifest

    log "========================================="
    log -e "${GREEN}Build complete!${NC} Version: ${YELLOW}${VERSION}${NC}"
    log "Build finished at $(date)"
    log "Log file: $LOG_FILE"
    log "========================================="
}

# Run main function
main

