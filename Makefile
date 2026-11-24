.PHONY: setup test test_setup dev format build fe gen

# 新克隆项目后安装依赖（Go 模块 + 前端依赖）
setup: setup_frontend
	go mod download
	bun install --cwd frontend

test: test_setup
	go test ./...

test_setup:
	test -d frontend/build || { rm -rf frontend/build; mkdir -p frontend/build && echo hello > frontend/build/index.html; }
	@# A leftover file stub cannot be replaced by cp -r, so clear it before copying.
	test -d cmd/uiautodev-server/frontend_build || { rm -f cmd/uiautodev-server/frontend_build; cp -r frontend/build cmd/uiautodev-server/frontend_build; }

dev:
	./scripts/dev.sh

format:
	bun --cwd frontend prettier --write "src/**/*.svelte"

build:
	bun --cwd frontend build

fe:
	bun --cwd frontend dev

gen: setup_frontend
	go generate ./version ./webserver/...
	wails generate module

setup_frontend:
	test -d frontend/build || (mkdir -p frontend/build && touch frontend/build/hello.txt)

