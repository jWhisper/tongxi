.PHONY: setup dev build release test probe

WAILS := $(CURDIR)/.tools/wails

setup:
	go mod download
	GOBIN="$(CURDIR)/.tools" go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
	cd frontend && npm ci --prefer-offline --no-audit --no-fund

dev:
	TONGXI_DATA_DIR="$${TONGXI_DATA_DIR:-$(CURDIR)/.local/dev}" $(WAILS) dev

build:
	$(WAILS) build

release:
	bash scripts/package-macos.sh

test:
	go test ./...
	cd frontend && npm run typecheck
	cd frontend && npm test

probe:
	go run ./cmd/probe
