.PHONY: setup test test_setup dev format sync build fe gen

# 新克隆项目后安装依赖（Go 模块 + 前端依赖）
setup: setup_frontend
	go mod download
	bun install --cwd frontend

test: test_setup
	go test ./...

test_setup:
	test -d frontend/build || echo hello > frontend/build
	test -d cmd/uiautodev-server/frontend_build || cp -r frontend/build cmd/uiautodev-server/frontend_build

dev:
	./scripts/dev.sh

format:
	bun --cwd frontend prettier --write "src/**/*.svelte"

sync:
	go run ./cmd/sync-binaries

build:
	bun --cwd frontend build

fe:
	bun --cwd frontend dev

gen: setup_frontend
	go generate ./version ./webserver/...
	wails generate module

setup_frontend:
	test -d frontend/build || (mkdir -p frontend/build && touch frontend/build/hello.txt)

