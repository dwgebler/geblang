.PHONY: all test test-go test-lang doc-test test-pgvector check-lang build build-with-path install bench bench-web bench-db bench-docker run repl check doctor cache-stats clean fmt docs docker-build compose-build vscode-build vscode-install vscode-install-wsl vscode-install-native

BINARY ?= geblang
GO ?= go
GOCACHE ?= /tmp/geblang-go-cache
DOCS_SRC ?= docs/user
DOCS_OUT ?= docs/site
DOCS_API_SRC ?= stdlib
DOCS_EXAMPLES_SRC ?= docs/examples
DOCS_BASE_URL ?=
DOCKER_IMAGE ?= geblang-build
DOCKER_CONTAINER ?= geblang-build-artifacts
VSCODE_IMAGE ?= geblang-vscode-artifacts
VSCODE_CONTAINER ?= geblang-vscode-build-artifacts
CODE ?= code

# Geblang needs no cgo (pure-Go SQLite, purego FFI), so default CGO off for portable, glibc-independent binaries. Override with `make CGO_ENABLED=1`.
CGO_ENABLED ?= 0
export CGO_ENABLED

# Full local build: run all tests, static-check the .gb corpus, install
# the binary, regenerate the docs site, build + install the VS Code
# extension, then run the benchmark suite. Each step is a separate
# sub-make invocation so a failure aborts the chain. check-lang runs
# right after the tests as a build gate: a stale catalog, a drifted
# native surface, or a regressed `geblang check` fails the build before
# the expensive install/docs/vscode/bench steps.
all:
	$(MAKE) test
	$(MAKE) doc-test
	$(MAKE) install
	$(MAKE) docs
	$(MAKE) vscode-build
	$(MAKE) vscode-install
	$(MAKE) bench

test: test-go test-lang

test-go: build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) test ./...

# doc-test parses every full-program ```gb block in docs/user so a broken
# example fails the build; snippet/fragment blocks are skipped.
doc-test:
	GEBLANG_DOCTEST=1 GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) test ./internal/doctest -run TestDocExamplesParse -count=1

# test-lang runs the Geblang-level regression suite under tests/.
# It depends on the geblang binary being built (target: build).
# FFI tests need explicit permission to load libm/libc/libsqlite3
# under tests/stdlib/; without these flags they would skip cleanly
# but leave the FFI surface unexercised.
test-lang: build
	./$(BINARY) test \
	  --allow-ffi 'libm.so.*' --allow-ffi 'libc.so.*' --allow-ffi 'libsqlite3*' \
	  --allow-ffi 'libzstd*' --allow-ffi 'libsystemd*' --allow-ffi 'libmagic*' \
	  --allow-ffi 'libncursesw*' --allow-ffi 'libncurses*' --allow-ffi 'libtinfo*' \
	  --allow-ffi 'libc.dylib' --allow-ffi 'libm.dylib' --allow-ffi 'libSystem*' \
	  tests/

# test-pgvector spins up a pgvector Postgres container (pgvector/pgvector:pg16),
# points GEBLANG_PG_DSN at it, and runs the suite so the pgvector integration
# tests - skipped by default when no database is configured - actually run.
# FFI allow-flags are passed too, so nothing is skipped by default. Override the
# image/port with PGVECTOR_IMAGE / PGVECTOR_PORT; pass a narrower target as
# `make test-pgvector PG_TARGET=tests/stdlib/pgvector_integration_test.gb`.
PG_TARGET ?= tests/
test-pgvector: build
	./scripts/test-pgvector.sh $(PG_TARGET)

# check-lang statically checks every .gb file under tests/. The tests/check
# subdirectory contains intentionally invalid files that must each emit at
# least one diagnostic; the script verifies that and lets `geblang check`
# pass clean over the rest.
check-lang: build
	@./scripts/check-lang.sh

build:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) build -o $(BINARY) ./cmd/geblang

# Build and install the binary into INSTALL_DIR (defaults to /usr/local/bin).
# Override with `make build-with-path INSTALL_DIR=~/.local/bin`. Uses sudo
# when INSTALL_DIR is not writable by the current user.
INSTALL_DIR ?= /usr/local/bin

build-with-path: build
	@if [ -w "$(INSTALL_DIR)" ]; then \
		install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY); \
		echo "installed $(INSTALL_DIR)/$(BINARY)"; \
	else \
		echo "installing into $(INSTALL_DIR) (requires sudo)"; \
		sudo install -m 0755 $(BINARY) $(INSTALL_DIR)/$(BINARY); \
		echo "installed $(INSTALL_DIR)/$(BINARY)"; \
	fi

# Alias of build-with-path for muscle memory.
install: build-with-path

bench: build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto GEBLANG_BIN=$(PWD)/$(BINARY) ./benchmarks/run.sh

bench-web: build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto GEBLANG_BIN=$(PWD)/$(BINARY) ./benchmarks/web/run.sh

bench-db: build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto GEBLANG_BIN=$(PWD)/$(BINARY) ./benchmarks/db/run.sh

bench-docker: build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto GEBLANG_BIN=$(PWD)/$(BINARY) ./benchmarks/run.sh --docker

docs:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/docsite $(DOCS_SRC) $(DOCS_OUT) $(DOCS_API_SRC) --examples $(DOCS_EXAMPLES_SRC) --base-url '$(DOCS_BASE_URL)'

docker-build:
	rm -rf build
	mkdir -p build
	docker build -t $(DOCKER_IMAGE) .
	-docker rm -f $(DOCKER_CONTAINER) 2>/dev/null
	docker create --name $(DOCKER_CONTAINER) $(DOCKER_IMAGE)
	docker cp $(DOCKER_CONTAINER):/out/. build/
	docker rm -f $(DOCKER_CONTAINER)

compose-build:
	rm -rf build
	mkdir -p build build/vscode/out build/vscode/vsix
	docker compose build
	-docker rm -f $(DOCKER_CONTAINER) $(VSCODE_CONTAINER) 2>/dev/null
	docker create --name $(DOCKER_CONTAINER) geblang-artifacts
	docker cp $(DOCKER_CONTAINER):/out/. build/
	docker rm -f $(DOCKER_CONTAINER)
	docker create --name $(VSCODE_CONTAINER) geblang-vscode-artifacts
	docker cp $(VSCODE_CONTAINER):/out/. build/vscode/out/
	docker cp $(VSCODE_CONTAINER):/vsix/. build/vscode/vsix/
	docker rm -f $(VSCODE_CONTAINER)

vscode-build:
	mkdir -p build/vscode/out build/vscode/vsix
	docker compose build vscode-ext
	-docker rm -f $(VSCODE_CONTAINER) 2>/dev/null
	docker create --name $(VSCODE_CONTAINER) geblang-vscode-artifacts
	docker cp $(VSCODE_CONTAINER):/out/. build/vscode/out/
	docker cp $(VSCODE_CONTAINER):/vsix/. build/vscode/vsix/
	docker rm -f $(VSCODE_CONTAINER)

vscode-install-wsl:
	@VSIX_SRC=build/vscode/vsix/geblang.vsix; \
	VSIX_TMP=/mnt/c/Windows/Temp/geblang.vsix; \
	cp "$$VSIX_SRC" "$$VSIX_TMP" && \
	NODE_OPTIONS="$$NODE_OPTIONS --no-deprecation" $(CODE) --install-extension "$$(wslpath -w $$VSIX_TMP)"

vscode-install-native:
	@bin="$(CODE)"; \
	command -v "$$bin" >/dev/null 2>&1 || bin="/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"; \
	command -v "$$bin" >/dev/null 2>&1 || { echo "error: VS Code 'code' CLI not found; install it (VS Code: Command Palette > 'Shell Command: Install code command in PATH') or run: make vscode-install CODE=/path/to/code" >&2; exit 1; }; \
	NODE_OPTIONS="$$NODE_OPTIONS --no-deprecation" "$$bin" --install-extension build/vscode/vsix/geblang.vsix

vscode-install:
	@if [ -n "$$WSL_DISTRO_NAME" ]; then \
		$(MAKE) vscode-install-wsl; \
	else \
		$(MAKE) vscode-install-native; \
	fi

run repl:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/geblang repl

check:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/geblang check .

doctor:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/geblang doctor

cache-stats:
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/geblang cache stats

clean:
	rm -f $(BINARY)
	rm -rf build
	GOCACHE=$(GOCACHE) GOTOOLCHAIN=auto $(GO) run ./cmd/geblang cache clean

fmt:
	gofmt -w cmd internal
