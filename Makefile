# RemoraSFTP build & development tasks.
#
#   make web      build the browser UI into internal/server/webassets
#   make build    build the single executable with embedded UI
#   make dev      run the Go engine (use `make web-dev` for the Vite dev server)
#   make web-dev  run the Vite dev server with /api proxied to the engine
#   make test     build the embedded UI, then run all Go tests
#   make lint     go vet + gofmt check + frontend typecheck
#   make fmt      format Go and TypeScript sources
#   make clean    remove build artifacts

GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X remorasftp/internal/version.Version=$(VERSION) \
           -X remorasftp/internal/version.Commit=$(COMMIT) \
           -X remorasftp/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: all web build test lint fmt dev web-dev clean tidy

all: web build

# npm ci (not npm install): deterministic install from package-lock.json,
# which also repairs a missing/partial node_modules (e.g. a fresh checkout
# on another machine). The build output lands in internal/server/webassets,
# where //go:embed captures it at Go compile time.
web:
	cd web && npm ci --no-audit --no-fund && npm run build

# build depends on web: the binary embeds internal/server/webassets at
# compile time, so a stale or missing frontend would ship in the binary.
build: web
	$(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o dist/remorasftp ./cmd/remorasftp

# Cross-compilation targets.
build-windows: web
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o dist/remorasftp-windows-amd64.exe ./cmd/remorasftp
build-macos: web
	GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o dist/remorasftp-macos-arm64 ./cmd/remorasftp
build-linux: web
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o dist/remorasftp-linux-amd64 ./cmd/remorasftp

# test depends on web: internal/server embeds webassets/ at compile time,
# so the SPA tests need a real index.html in the embed tree. `npm ci`
# inside the web target repairs a missing node_modules first.
test: web
	$(GO) test ./... -count=1

test-race: web
	$(GO) test -race ./... -count=1

dev:
	$(GO) run ./cmd/remorasftp start --headless --no-browser --port 7970

web-dev:
	cd web && npm run dev

lint:
	$(GO) vet ./...
	@test -z "$$(gofmt -l .)" || (echo "Go files need formatting:"; gofmt -l .; exit 1)
	cd web && npx tsc --noEmit

fmt:
	$(GO) fmt ./...
	cd web && npx tsc --noEmit

tidy:
	cd web && npm install --no-audit --no-fund
	$(GO) mod tidy

clean:
	rm -rf dist
	$(GO) clean -testcache
