.PHONY: setup test test_setup dev prepare format build fe gen bindings setup_frontend

# 优先使用已安装的 wails3，没有就用 go.mod 里锁定的版本临时运行
WAILS3_VERSION := $(shell go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3 2>/dev/null)
WAILS3 ?= $(shell command -v wails3 2>/dev/null || echo "go run github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS3_VERSION)")
BINDINGS_DIR := frontend/bindings/github.com/uiautodev

# 新克隆项目后需要的全部准备：Go 模块、前端依赖、生成的代码与 wails bindings
setup: prepare

# 幂等：已准备好的部分会跳过，可以随时重复执行
prepare: setup_frontend
	@command -v bun >/dev/null || { echo "bun not found, install it from https://bun.sh"; exit 1; }
	go mod download
	@test -d frontend/node_modules || bun install --cwd frontend
	go generate ./version ./webserver/...
	@test -d $(BINDINGS_DIR) || $(MAKE) bindings

test: test_setup
	go test ./...

test_setup:
	test -d frontend/build || { rm -rf frontend/build; mkdir -p frontend/build && echo hello > frontend/build/index.html; }
	@# A leftover file stub cannot be replaced by cp -r, so clear it before copying.
	test -d cmd/uiautodev-server/frontend_build || { rm -f cmd/uiautodev-server/frontend_build; cp -r frontend/build cmd/uiautodev-server/frontend_build; }

dev: prepare
	./scripts/dev.sh

format:
	bun --cwd frontend prettier --write "src/**/*.svelte"

build:
	bun --cwd frontend build

fe:
	bun --cwd frontend dev

gen: setup_frontend
	go generate ./version ./webserver/...
	$(MAKE) bindings

bindings:
	$(WAILS3) generate bindings -clean=true -ts -i

setup_frontend:
	test -d frontend/build || (mkdir -p frontend/build && touch frontend/build/hello.txt)
