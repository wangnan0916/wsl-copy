GO ?= go
VERSION ?= dev
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build test test-windows release

build:
	mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/wsl-copy ./cmd/wsl-copy

test:
	$(GO) test ./...

test-windows:
	WSL_COPY_WINDOWS_TESTS=1 $(GO) test -count=1 -run '^TestWindows$$' -v ./wslcopy

release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/wsl-copy-linux-amd64 ./cmd/wsl-copy
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/wsl-copy-linux-arm64 ./cmd/wsl-copy
	set -e; for arch in amd64 arm64; do \
		mkdir -p dist/package-$$arch; \
		cp dist/wsl-copy-linux-$$arch dist/package-$$arch/wsl-copy; \
		cp LICENSE THIRD_PARTY_NOTICES dist/package-$$arch/; \
		tar -czf dist/wsl-copy-linux-$$arch.tar.gz -C dist/package-$$arch wsl-copy LICENSE THIRD_PARTY_NOTICES; \
	done
	cd dist && sha256sum wsl-copy-linux-amd64.tar.gz wsl-copy-linux-arm64.tar.gz > SHA256SUMS
