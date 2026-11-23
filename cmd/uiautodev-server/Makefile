# Get version from git tag, fallback to "dev"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

all: generate frontend
	GOOS=darwin GOARCH=arm64 go build -o uiautodev-server-darwin-arm64
	GOOS=linux GOARCH=amd64 go build -o uiautodev-server-linux-amd64
	GOOS=windows GOARCH=amd64 go build -o uiautodev-server-windows-amd64.exe

linux: generate frontend
	GOOS=linux GOARCH=amd64 go build -o uiautodev-server-linux-amd64

generate:
	cd ../../version && go generate

frontend: build-frontend
	rm -rf ./frontend_build
	mkdir -p ./frontend_build
	cp -r ../../frontend/build/* ./frontend_build/

build-frontend:
	cd ../../frontend && \
	bun install && \
	bun run build

release:
	rclone copy --progress uiautodev-server-darwin-arm64 r2:/uiautodev/
	rclone copy --progress uiautodev-server-linux-amd64 r2:/uiautodev/
	rclone copy --progress uiautodev-server-windows-amd64.exe r2:/uiautodev/