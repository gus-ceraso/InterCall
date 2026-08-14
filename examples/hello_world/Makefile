EXAMPLE_DIR := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
REPOSITORY_DIR := $(abspath $(EXAMPLE_DIR)/../..)
GO_DIR := $(REPOSITORY_DIR)/go
TYPESCRIPT_DIR := $(REPOSITORY_DIR)/typescript
BACKEND_DIR := $(EXAMPLE_DIR)/backend
FRONTEND_DIR := $(EXAMPLE_DIR)/frontend
TOOLS_DIR := $(EXAMPLE_DIR)/.tools

INTERCALL_GO := $(TOOLS_DIR)/intercall-go
HELLO_SERVER := $(TOOLS_DIR)/hello-server
TYPESCRIPT_STAMP := $(TOOLS_DIR)/typescript.stamp
FRONTEND_STAMP := $(FRONTEND_DIR)/node_modules/.install-stamp

GO_TOOL_SOURCES := $(shell find $(GO_DIR) -type f -name '*.go') $(GO_DIR)/go.mod $(GO_DIR)/go.sum
TYPESCRIPT_SOURCES := $(shell find $(TYPESCRIPT_DIR)/src -type f -name '*.ts') \
	$(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json \
	$(TYPESCRIPT_DIR)/tsconfig.base.json $(TYPESCRIPT_DIR)/tsconfig.browser.json \
	$(TYPESCRIPT_DIR)/tsconfig.cli.json
BACKEND_SOURCES := $(shell find $(BACKEND_DIR) -type f -name '*.go') \
	$(BACKEND_DIR)/go.mod $(BACKEND_DIR)/go.sum

.PHONY: all install generate build run dev check gofmt clean distclean

all: build

install: $(TYPESCRIPT_STAMP) $(FRONTEND_STAMP) $(INTERCALL_GO)

$(TOOLS_DIR):
	mkdir -p "$@"

$(TYPESCRIPT_STAMP): $(TYPESCRIPT_SOURCES) | $(TOOLS_DIR)
	npm --prefix "$(TYPESCRIPT_DIR)" ci
	npm --prefix "$(TYPESCRIPT_DIR)" run build
	touch "$@"

$(FRONTEND_STAMP): $(FRONTEND_DIR)/package.json $(FRONTEND_DIR)/package-lock.json $(TYPESCRIPT_STAMP)
	npm --prefix "$(FRONTEND_DIR)" ci
	touch "$@"

$(INTERCALL_GO): $(GO_TOOL_SOURCES) | $(TOOLS_DIR)
	cd "$(GO_DIR)" && go build -o "$@" ./cmd/intercall-go

generate: install
	cd "$(EXAMPLE_DIR)" && node "$(TYPESCRIPT_DIR)/dist/cli/main.js" export \
		--project frontend/tsconfig.json \
		--out frontend/src/generated/browser \
		--interface api/browser.intercall \
		frontend/src/providers.ts
	cd "$(BACKEND_DIR)" && ../.tools/intercall-go import \
		--out gen/browserimport \
		../api/browser.intercall
	cd "$(BACKEND_DIR)" && ../.tools/intercall-go export \
		--out gen/backendexport \
		--interface ../api/backend.intercall \
		./hello
	cd "$(EXAMPLE_DIR)" && node "$(TYPESCRIPT_DIR)/dist/cli/main.js" import \
		--out frontend/src/generated/backend \
		api/backend.intercall

$(HELLO_SERVER): $(BACKEND_SOURCES) | $(TOOLS_DIR)
	cd "$(BACKEND_DIR)" && go build -o "$@" ./cmd/server

build: install $(HELLO_SERVER)
	npm --prefix "$(FRONTEND_DIR)" run build

run: build
	cd "$(EXAMPLE_DIR)" && ./.tools/hello-server -static frontend/dist

dev: install $(HELLO_SERVER)
	@set -eu; \
		cd "$(EXAMPLE_DIR)"; \
		./.tools/hello-server -static frontend/dist & backend_pid=$$!; \
		trap 'kill "$$backend_pid" 2>/dev/null || true; wait "$$backend_pid" 2>/dev/null || true' EXIT INT TERM; \
		cd frontend; \
		npm run dev

check: install
	npm --prefix "$(FRONTEND_DIR)" run typecheck
	cd "$(BACKEND_DIR)" && go test ./...
	npm --prefix "$(FRONTEND_DIR)" run build

gofmt:
	cd "$(BACKEND_DIR)" && gofmt -w $$(find . -type f -name '*.go')

clean:
	rm -rf "$(TOOLS_DIR)" "$(FRONTEND_DIR)/dist"

distclean: clean
	rm -rf "$(FRONTEND_DIR)/node_modules"
