# Apogee — developer Makefile.
#
# Thin convenience wrapper over the Go toolchain. The source of truth for the
# build is `go build`; these targets just give the common invocations one-word
# names and bundle the Phase-2 acceptance gate (docs/plans/phase-2-detail-plan.md §7)
# into a single `make check`.

BINARY  := apogee
PKG     := ./cmd/apogee
MODULE  := github.com/airiclenz/apogee

# The release version is the single source of truth in the top-level VERSION file,
# embedded into the binary at build time (see version.go / apogee.Version), so the version
# NUMBER is identical on every build path and cannot drift. Build provenance is appended to it:
# the short commit + dirty flag come from Go's own VCS stamp at runtime (debug.ReadBuildInfo),
# and the build number — the repository's commit count — is the one field the runtime cannot
# derive, so it is injected here via -ldflags. A bare `go build` omits the number and reports
# just `+g<rev>`; the make targets below stamp it. To release, edit VERSION.
BUILD_COUNT := $(shell git rev-list --count HEAD 2>/dev/null)
GO_LDFLAGS  := $(if $(BUILD_COUNT),-X $(MODULE).buildCount=$(BUILD_COUNT))

# The 6 release targets the Phase-2 cross-build invariant must stay green on.
CROSS_TARGETS := \
	linux/amd64   linux/arm64 \
	darwin/amd64  darwin/arm64 \
	windows/amd64 windows/arm64

# Run user-supplied args through `make run ARGS="--help"`.
ARGS ?=

# The default endpoint for `make live-eval` (override: make live-eval LIVE_ENDPOINT=...).
# Set APOGEE_LIVE_MODEL in the environment to pin the model (and bust the result cache on a swap).
LIVE_ENDPOINT ?= http://192.168.64.1:1111

# Where `install` drops the binary. Leave empty to let `install` auto-pick the
# first candidate dir that is already on $PATH *and* writable without sudo,
# trying, in order: /usr/local/bin (Intel macOS + most Linux), /opt/homebrew/bin
# (Apple Silicon), ~/.local/bin, ~/bin. If none qualifies it creates and uses
# ~/.local/bin and warns if that is not on PATH. Override with
# `make install PREFIX=/some/dir` (use sudo if the dir needs root).
PREFIX ?=
INSTALL_CANDIDATES := /usr/local/bin /opt/homebrew/bin $$HOME/.local/bin $$HOME/bin

.DEFAULT_GOAL := help

## help: list the available targets
.PHONY: help
help:
	@echo "Apogee — make targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //' | awk -F': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

## build: compile the binary to ./apogee
.PHONY: build
build:
	go build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) $(PKG)

## run: build-and-run the binary (pass flags via ARGS="...")
.PHONY: run
run:
	go run -ldflags "$(GO_LDFLAGS)" $(PKG) $(ARGS)

## install: build and copy the binary to a writable dir on PATH (auto-detected; override with PREFIX=...)
.PHONY: install
install: build
	@dir='$(PREFIX)'; \
	if [ -n "$$dir" ]; then \
		mkdir -p "$$dir" || { echo "error: cannot create $$dir" >&2; exit 1; }; \
	else \
		for d in $(INSTALL_CANDIDATES); do \
			case ":$$PATH:" in *":$$d:"*) ;; *) continue ;; esac; \
			if [ -d "$$d" ] && [ -w "$$d" ]; then dir="$$d"; break; fi; \
		done; \
		if [ -z "$$dir" ]; then dir="$$HOME/.local/bin"; mkdir -p "$$dir"; fi; \
	fi; \
	if [ ! -w "$$dir" ]; then \
		echo "error: $$dir is not writable — re-run with sudo, or 'make install PREFIX=<writable dir on PATH>'." >&2; \
		exit 1; \
	fi; \
	rm -f "$$dir/$(BINARY)"; \
	cp "$(BINARY)" "$$dir/$(BINARY)" || exit 1; \
	echo "installed $(BINARY) -> $$dir/$(BINARY)"; \
	case ":$$PATH:" in \
		*":$$dir:"*) ;; \
		*) echo "warning: $$dir is not on your PATH — add it (e.g. 'export PATH=\"$$dir:\$$PATH\"') to run '$(BINARY)' by name." ;; \
	esac

## test: run the full test suite with the race detector
.PHONY: test
test:
	go test -race -count=1 ./...

## live-eval: run the opt-in live-model eval against a real local model (always -count=1, never cached)
.PHONY: live-eval
live-eval:
	APOGEE_LIVE_ENDPOINT=$(LIVE_ENDPOINT) go test -race -count=1 -run TestE2ELiveModel -v ./internal/tui/

## fmt: format all Go source in place
.PHONY: fmt
fmt:
	gofmt -w .

## vet: run go vet over the module
.PHONY: vet
vet:
	go vet ./...

## cross: build every release target (CGO off); fails on the first broken one
.PHONY: cross
cross:
	@for t in $(CROSS_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		printf '  -> %s/%s\n' "$$os" "$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o /dev/null ./... || exit 1; \
	done
	@echo "cross-build OK ($(words $(CROSS_TARGETS)) targets)"

## check: the Phase-2 acceptance gate (fmt-check, vet, build, race tests, ADR-0010, cross, --help)
.PHONY: check
check:
	@echo "==> gofmt (must be empty)"
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi
	@echo "==> go vet"
	@go vet ./...
	@echo "==> go build ./..."
	@go build ./...
	@echo "==> go test -race ./..."
	@go test -race -count=1 ./...
	@echo "==> ADR-0010 invariant (internal/ must not import the root module path)"
	@if grep -rl '"$(MODULE)"' internal/; then echo "ADR-0010 violation: internal/ imports the root module path"; exit 1; fi
	@echo "==> cross-build"
	@$(MAKE) --no-print-directory cross
	@echo "==> apogee --help (exit 0)"
	@go run $(PKG) --help >/dev/null
	@echo "all Phase-2 gates passed"

## clean: remove the built binary
.PHONY: clean
clean:
	rm -f $(BINARY)
