.PHONY: help all check test coverage coverage-check coverage-clean fmt fmt-check vet lint lint-ci vuln mod-check \
	frontend frontend-install frontend-check frontend-urls build build-gui bundle-macos appicon winres spike dev clean

help: ## This help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

# Ignore a parent go.work so this module builds standalone (a go.work for
# local library development is allowed but never committed).
export GOWORK := off

# CI formats and lints with the Go version from go.mod, not with the toolchain installed here.
GO_MOD_VERSION := $(shell sed -nE 's/^go ([0-9]+\.[0-9]+).*/\1/p' go.mod | head -n1)
GOFMT_CI = $(shell GOTOOLCHAIN=go$(GO_MOD_VERSION).0 go env GOROOT)/bin/gofmt

# macOS: compile the cgo parts (the webview bindings) and link for the same
# minimum. Without the flags, clang builds the C objects for the SDK's own
# version (27.0 with Xcode 27) while Go links for 11.0, and the linker warns
# about every object file. 13.0 is Wails v3's minimum and the bundle's
# LSMinimumSystemVersion.
ifeq ($(shell uname -s),Darwin)
export MACOSX_DEPLOYMENT_TARGET ?= 13.0
export CGO_CFLAGS ?= -O2 -g -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)
export CGO_LDFLAGS ?= -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)
endif

GUI_NAME = modbus-trace-explorer
GUI_SRC  = ./cmd/modbus-trace-explorer
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS  = -ldflags "-s -w -X github.com/otfabric/modbus-trace-explorer/internal/buildinfo.Version=$(VERSION)"

# Packages measured by the coverage gate. The Wails shell (cmd/, the window
# parts of internal/gui) needs a window and is covered by the frontend tests.
TEST_PKGS    := ./internal/engine/... ./internal/service/... ./internal/store/... ./internal/buildinfo/...
COVERAGE_MIN := 85

all: check build ## Run all checks and build

check: fmt fmt-check mod-check vet lint lint-ci vuln test coverage-check frontend-check ## Run all checks (Go and frontend)

test: ## Run the Go tests with the race detector
	@echo "Running tests (race detector)"
	@go test -count=1 -race ./...

coverage: ## Run tests with coverage profile and text summary
	@go test -count=1 -race -coverprofile=coverage.out -covermode=atomic $(TEST_PKGS)
	@go tool cover -func=coverage.out | tee coverage.txt

coverage-check: coverage ## Fail if total coverage is below $(COVERAGE_MIN)%
	@go tool cover -func=coverage.out | grep 'total:' | awk -v min=$(COVERAGE_MIN) '{gsub(/%/,""); p=$$NF+0; if (p < min) { printf "Coverage %.1f%% is below %d%%\n", p, min; exit 1 } else { printf "Coverage %.1f%% (>= %d%%)\n", p, min } }'

coverage-clean: ## Remove coverage artifacts
	@rm -f coverage.out coverage.txt coverage.html

fmt: ## Format Go code with the gofmt of the Go version in go.mod (what CI uses)
	@echo "Running gofmt (go$(GO_MOD_VERSION))"
	@$(GOFMT_CI) -w cmd internal frontend/*.go

fmt-check: ## Fail if the go.mod-version gofmt or the local gofmt would change any file
	@ci="$$($(GOFMT_CI) -l cmd internal frontend/*.go)"; loc="$$(gofmt -l cmd internal frontend/*.go)"; \
	if [ -n "$$ci$$loc" ]; then echo "Not formatted: $$ci $$loc. Run 'make fmt'."; exit 1; fi

mod-check: ## Fail on a replace directive (the library comes from a tag or pseudo-version)
	@if grep -q '^replace\|^	*replace ' go.mod; then echo "go.mod must not contain replace directives"; exit 1; fi

vet: ## Run go vet on all packages
	@go vet ./...

lint: ## Run staticcheck
	@echo "Running staticcheck"
	@staticcheck ./...

lint-ci: ## Run golangci-lint (uses .golangci.yml; includes the network and Modbus bans)
	@echo "Running golangci-lint"
	@golangci-lint run ./...

vuln: ## Run govulncheck
	@echo "Running govulncheck"
	@govulncheck ./...

frontend-install: ## Install the frontend dependencies (npm ci)
	@cd frontend && npm ci

frontend: ## Build the frontend into dist/wails
	@echo "Building frontend"
	@cd frontend && npm run build

frontend-check: ## Typecheck, lint the tokens and test the frontend
	@echo "Checking frontend"
	@cd frontend && npm run typecheck && npm test

build: build-gui ## Build everything into ./bin

build-gui: frontend ## Build the desktop application (needs cgo and the platform's webview)
	@mkdir -p bin
	@echo "Building $(GUI_NAME) $(VERSION)"
	@go build -tags production $(LDFLAGS) -o bin/$(GUI_NAME) $(GUI_SRC)

bundle-macos: build-gui ## Wrap the desktop binary in bin/Modbus Trace Explorer.app (macOS, ad-hoc signed)
	@packaging/macos/bundle.sh bin/$(GUI_NAME) $(VERSION) bin

appicon: ## Render cmd/modbus-trace-explorer/appicon.png from the Modbus icon geometry
	@python3 packaging/icons/render_appicon.py

winres: ## Regenerate the Windows resources (icon, manifest) linked into the desktop executable
	@cd cmd/modbus-trace-explorer && go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 --manifest gui \
		--icon appicon.png --product-name "Modbus Trace Explorer" --file-description "OT Fabric Modbus Trace Explorer" \
		--copyright "Copyright (c) 2026 OT Fabric. MIT License." --original-filename modbus-trace-explorer.exe --out rsrc

spike: ## Run the M0 store spike (SPIKE_ROWS rows, default 10,000,000); see docs/ARCHITECTURE.md section 5
	@MTE_SPIKE_ROWS=$${SPIKE_ROWS:-10000000} go test -count=1 -run '^TestSpike$$' -v -timeout 60m ./internal/store

dev: frontend ## Run the desktop application from source
	@go run $(GUI_SRC)

clean: coverage-clean ## Clean build artifacts
	@rm -rf bin release

# A12: the built user interface may name URLs only as XML namespaces or in
# library messages that are never fetched; the CSP (default-src 'self')
# blocks any request anyway.
URL_ALLOW = http://www.w3.org/|https://react.dev/errors/|https://v3.wails.io/learn/build/|https://tailwindcss.com
frontend-urls: ## Fail when the built frontend references an external URL (A12)
	@found=$$(grep -rhoI 'https\?://[a-zA-Z0-9./_#?=%-]*' frontend/dist/wails | sort -u | grep -Ev '^($(URL_ALLOW))' || true); \
	if [ -n "$$found" ]; then echo "external URLs in frontend/dist/wails:"; echo "$$found"; exit 1; fi; echo "no external URLs"
