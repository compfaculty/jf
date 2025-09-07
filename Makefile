# -----------------------------
# Project settings (override on CLI: make run LOG_LEVEL=info)
# -----------------------------
APP_NAME       ?= jf
BINARY         ?= bin/$(APP_NAME)
PKG            ?= .
PKGS           ?= ./...
GO             ?= go

# Runtime config
ADDR           ?= :8080
DB_DIR         ?= data
DB_PATH        ?= $(DB_DIR)/jobs.db
CONFIG_PATH    ?= config/config.yaml
LOG_LEVEL      ?= debug
DB_DEBUG       ?= 1
HTTP_RPS       ?= 0          # 0 = disabled (no rate limit)
HTTP_BURST     ?= 10
SQLITE_JOURNAL ?= WAL        # set to DELETE for Docker Desktop on Windows

# Build config
CGO_ENABLED    ?= 0          # modernc.org/sqlite is pure Go; keep CGO off
GO_BUILD_FLAGS ?= -trimpath
LD_EXTRA       ?=
# You can inject version info here if you add variables in code:
# LD_EXTRA     += -X 'main.version=$(shell git describe --tags --dirty --always 2>/dev/null || echo dev)'
# LD_EXTRA     += -X 'main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'

LDFLAGS        ?= -s -w $(LD_EXTRA)

# Docker
IMAGE          ?= $(APP_NAME):latest
DOCKER_BUILD_ARGS ?=

# Default goal
.DEFAULT_GOAL := help

# -----------------------------
# Utility
# -----------------------------
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make \033[36m<TARGET>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_\-\/]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0,5) } ' $(MAKEFILE_LIST)

# -----------------------------
# Go hygiene
# -----------------------------
.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: fmt
fmt: ## go fmt
	$(GO) fmt $(PKGS)

.PHONY: vet
vet: ## go vet
	$(GO) vet $(PKGS)

.PHONY: test
test: ## run unit tests
	$(GO) test -v $(PKGS)

# -----------------------------
# Build & run locally
# -----------------------------
$(BINARY): ## build binary
	@mkdir -p $(dir $(BINARY))
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

.PHONY: build
build: tidy fmt vet $(BINARY) ## build all (tidy, fmt, vet, build)

.PHONY: run
run: build ## run the server locally with env vars
	@mkdir -p $(DB_DIR)
	JF_ADDR="$(ADDR)" \
	JFV2_DB_PATH="$(DB_PATH)" \
	JF_CONFIG_PATH="$(CONFIG_PATH)" \
	JF_LOG_LEVEL="$(LOG_LEVEL)" \
	JF_DB_DEBUG="$(DB_DEBUG)" \
	JF_HTTP_RPS="$(HTTP_RPS)" \
	JF_HTTP_BURST="$(HTTP_BURST)" \
	JF_SQLITE_JOURNAL="$(SQLITE_JOURNAL)" \
	./$(BINARY)

.PHONY: clean
clean: ## remove build artifacts
	rm -rf bin

# -----------------------------
# Docker
# -----------------------------
.PHONY: docker-build
docker-build: ## docker build image (IMAGE=$(IMAGE))
	docker build $(DOCKER_BUILD_ARGS) -t $(IMAGE) .

# Linux/macOS friendly run (bind-mount ./data -> /data)
.PHONY: docker-run
docker-run: ## run container with host ./data bind-mounted as /data
	@mkdir -p $(DB_DIR)
	docker run --rm -it \
		-p 8080:8080 \
		-v "$$PWD/$(DB_DIR):/data:rw" \
		-v "$$PWD:/app:ro" \
		-w /app \
		-e JF_ADDR="$(ADDR)" \
		-e JFV2_DB_PATH="/data/jobs.db" \
		-e JF_CONFIG_PATH="$(CONFIG_PATH)" \
		-e JF_LOG_LEVEL="$(LOG_LEVEL)" \
		-e JF_DB_DEBUG="$(DB_DEBUG)" \
		-e JF_HTTP_RPS="$(HTTP_RPS)" \
		-e JF_HTTP_BURST="$(HTTP_BURST)" \
		-e JF_SQLITE_JOURNAL="$(SQLITE_JOURNAL)" \
		$(IMAGE)

# Windows PowerShell friendly run (uses PowerShell to expand absolute path)
# Invoke with: make docker-run-win
.PHONY: docker-run-win
docker-run-win: ## run container on Windows PowerShell (bind-mount ./data)
	@powershell -NoProfile -Command ^
	  "New-Item -ItemType Directory -Force -Path '$(DB_DIR)' | Out-Null; ^
	   $$p = (Get-Location).Path; ^
	   docker run --rm -it ^
	     -p 8080:8080 ^
	     -v $$p\$(DB_DIR):/data:rw ^
	     -v $$p:/app:ro ^
	     -w /app ^
	     -e JF_ADDR='$(ADDR)' ^
	     -e JFV2_DB_PATH='/data/jobs.db' ^
	     -e JF_CONFIG_PATH='$(CONFIG_PATH)' ^
	     -e JF_LOG_LEVEL='$(LOG_LEVEL)' ^
	     -e JF_DB_DEBUG='$(DB_DEBUG)' ^
	     -e JF_HTTP_RPS='$(HTTP_RPS)' ^
	     -e JF_HTTP_BURST='$(HTTP_BURST)' ^
	     -e JF_SQLITE_JOURNAL='$(SQLITE_JOURNAL)' ^
	     $(IMAGE)"

# -----------------------------
# Diagnostics
# -----------------------------
.PHONY: print-env
print-env: ## print resolved environment used by 'run' target
	@echo "ADDR              = $(ADDR)"
	@echo "DB_DIR            = $(DB_DIR)"
	@echo "DB_PATH           = $(DB_PATH)"
	@echo "CONFIG_PATH       = $(CONFIG_PATH)"
	@echo "LOG_LEVEL         = $(LOG_LEVEL)"
	@echo "DB_DEBUG          = $(DB_DEBUG)"
	@echo "HTTP_RPS          = $(HTTP_RPS)"
	@echo "HTTP_BURST        = $(HTTP_BURST)"
	@echo "SQLITE_JOURNAL    = $(SQLITE_JOURNAL)"
	@echo "CGO_ENABLED       = $(CGO_ENABLED)"
	@echo "IMAGE             = $(IMAGE)"
