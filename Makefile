# nerdui Makefile. Run `make help` for available targets.

BINARY      := nerdui
PKG         := github.com/pessom/nerdui
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOARCH      ?= $(shell go env GOARCH)
BIN_DIR     := bin
IMAGE       ?= ghcr.io/pessom/nerdui
LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)
GO_BUILD    := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

# Dependency licenses compatible with AGPL-3.0-only that may be linked into nerdui.
GO_LICENSES      := go run github.com/google/go-licenses/v2@v2.0.1
ALLOWED_GO_LIC   := Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT,MPL-2.0
ALLOWED_NPM_LIC  := MIT;ISC;Apache-2.0;BSD-2-Clause;BSD-3-Clause;0BSD;Unlicense;MIT OR CC0-1.0
THIRD_PARTY_DIR  := third_party

.DEFAULT_GOAL := help

##@ Development

.PHONY: run
RUN_ARGS    ?= --data-dir $(CURDIR)/.data --listen 127.0.0.1:9443

run: ## Run nerdui locally with a project-local data dir (extra flags via ARGS="...")
	go run ./cmd/nerdui $(RUN_ARGS) $(ARGS)

.PHONY: web-dev
web-dev: ## Run Vite dev server (proxies API to NERDUI_API_URL, default http://localhost:9443)
	cd web && npm run dev

.PHONY: deps
deps: ## Install Go and npm dependencies
	go mod download
	cd web && npm ci

SQLC        := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

.PHONY: sqlc
sqlc: ## Regenerate typed Go code from internal/store/queries/*.sql (sqlc)
	cd internal/store && $(SQLC) generate

.PHONY: sqlc-check
sqlc-check: ## Fail if generated sqlc code is out of date
	cd internal/store && $(SQLC) diff

.PHONY: api-gen
api-gen: ## Regenerate TypeScript types from api/openapi.yaml
	cd web && npm run api:gen

##@ Build

.PHONY: web
web: ## Build the frontend into web/dist
	cd web && npm run build
	@touch web/dist/.gitkeep

.PHONY: build
build: web build-go ## Build frontend and nerdui binary for the host platform

.PHONY: build-go
build-go: ## Build only the Go binary (reuses existing web/dist)
	$(GO_BUILD) -o $(BIN_DIR)/$(BINARY) ./cmd/nerdui

.PHONY: build-linux
build-linux: web ## Build Linux binary (GOARCH=amd64|arm64, default: host arch)
	GOOS=linux GOARCH=$(GOARCH) $(GO_BUILD) -o $(BIN_DIR)/$(BINARY)-linux-$(GOARCH) ./cmd/nerdui

.PHONY: image
image: ## Build Docker image for the host platform
	docker build -t $(IMAGE):$(VERSION) \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) .

##@ Quality

.PHONY: test
test: test-go test-web ## Run all unit tests

.PHONY: test-go
test-go: ## Run Go unit tests with race detector and coverage
	go test -race -coverprofile=coverage.out ./...

.PHONY: test-web
test-web: ## Run frontend unit tests
	cd web && npm test

.PHONY: test-integration
test-integration: ## Run integration tests against real containerd (Linux, root)
	go test -p 1 -race -tags integration -count=1 ./...

.PHONY: lint
lint: lint-go lint-web spdx-check ## Run all linters

.PHONY: spdx-check
spdx-check: ## Verify SPDX license headers in source files
	scripts/check-spdx.sh

.PHONY: licenses-check
licenses-check: ## Fail if a dependency has a license incompatible with AGPL-3.0-only
	$(GO_LICENSES) check ./... --ignore $(PKG) --allowed_licenses=$(ALLOWED_GO_LIC)
	cd web && npx license-checker-rseidelsohn --production --excludePrivatePackages \
		--onlyAllow '$(ALLOWED_NPM_LIC)' --summary

.PHONY: licenses
licenses: ## Collect third-party licenses and notices into third_party/
	rm -rf $(THIRD_PARTY_DIR)
	$(GO_LICENSES) save ./... --ignore $(PKG) --save_path=$(THIRD_PARTY_DIR)/go
	cd web && npx license-checker-rseidelsohn --production --excludePrivatePackages \
		--plainVertical --out ../$(THIRD_PARTY_DIR)/web-licenses.txt

.PHONY: lint-go
lint-go: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: lint-web
lint-web: ## Run eslint, prettier and tsc
	cd web && npm run lint && npm run format:check && npm run typecheck

.PHONY: vuln
vuln: ## Scan Go dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: fmt
fmt: ## Format Go and frontend code
	gofmt -w -s cmd internal web/embed.go
	cd web && npm run format

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

##@ Release

.PHONY: release-snapshot
release-snapshot: ## Build a local snapshot release with GoReleaser (no publish)
	goreleaser release --snapshot --clean

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	goreleaser check

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) dist coverage.out $(THIRD_PARTY_DIR)
	find web/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
