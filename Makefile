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

# Where `dist` stages the release archives, and the version their names carry — the
# VERSION file's value with the leading "v" stripped, because an archive name reads
# better as apogee_0.11.0_darwin_arm64 than apogee_v0.11.0_darwin_arm64. VERSION stays
# the single source of truth: nothing here re-states the number.
DIST_DIR     := dist
DIST_VERSION  = $(shell tr -d ' \t\r\n' < VERSION | sed 's/^v//')

# sha256 is spelled differently on the two hosts a release is ever cut from.
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo 'sha256sum' || echo 'shasum -a 256')

# Run user-supplied args through `make run ARGS="--help"`.
ARGS ?=

# actionlint checks the workflow files `make check` and CI both run it over. It is pinned by
# MODULE VERSION and fetched by `go run` rather than added as a third-party GitHub Action:
# one fewer publisher trusted with the workflow's context, one fewer SHA to keep current.
# CI runs `make actionlint` rather than its own copy of the command, so this version literal
# is the only one and the two cannot drift. `go run` always runs the pinned version; after the
# first run the module cache serves it, so an offline build still lints the workflows.
ACTIONLINT_VERSION := v1.7.12
ACTIONLINT = go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

# The default endpoint for `make live-eval` (override: make live-eval LIVE_ENDPOINT=...).
# Set APOGEE_LIVE_MODEL in the environment to pin the model (and bust the result cache on a swap).
# The default is a local server, matching the addresses the live tests document themselves. A
# server on another host is an override, never a default: an address baked in here is stale the
# moment that host's gateway moves, and a live test that cannot reach its endpoint fails where an
# unset APOGEE_LIVE_ENDPOINT would have skipped.
LIVE_ENDPOINT ?= http://127.0.0.1:1111

# The endpoint `make live-eval` points the LLM judge at (internal/judge), defaulting to the same
# server the live tests use — override to judge with a bigger model than the one under test:
# `make live-eval JUDGE_ENDPOINT=http://host.internal:1111`. Set APOGEE_JUDGE_MODEL in the
# environment to pin the judging model. Judge verdicts are BINDING: a fail fails the Go test.
JUDGE_ENDPOINT ?= $(LIVE_ENDPOINT)

# Where `install` drops the binary. Leave empty to let `install` auto-pick the
# first candidate dir that is already on $PATH *and* writable without sudo,
# trying, in order: /usr/local/bin (most Linux, and macOS if you own it), the Go
# bin dir (`go env GOBIN`, else `$(go env GOPATH)/bin` — on PATH for most Go
# developers), ~/.local/bin, /opt/homebrew/bin (Apple Silicon), ~/bin. Nothing is
# ever installed off-PATH by auto-detection: if no candidate qualifies, `install`
# stops and prints the one-line ways to finish, because a binary copied somewhere
# the shell cannot see is not an install. Override with
# `make install PREFIX=/some/dir` (use sudo if the dir needs root); an explicit
# PREFIX is honoured even when it is off-PATH, with a warning.
#
# `=` (not `:=`) keeps the `go env` calls out of every other target.
PREFIX ?=
GO_BIN_DIR = $(shell go env GOBIN 2>/dev/null)
GOPATH_BIN = $(shell p="$$(go env GOPATH 2>/dev/null)"; [ -n "$$p" ] && printf '%s/bin' "$$p")
INSTALL_CANDIDATES = /usr/local/bin $(or $(GO_BIN_DIR),$(GOPATH_BIN)) $$HOME/.local/bin /opt/homebrew/bin $$HOME/bin

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

## stubllm: build the scripted test upstream to ./stubllm (a dev tool, never a release asset)
.PHONY: stubllm
stubllm:
	go build -o stubllm ./cmd/stubllm

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
	fi; \
	if [ -z "$$dir" ]; then \
		echo "error: no candidate directory is both on your PATH and writable without sudo:" >&2; \
		for d in $(INSTALL_CANDIDATES); do \
			why=""; \
			[ -d "$$d" ] || why="does not exist"; \
			if [ -z "$$why" ]; then case ":$$PATH:" in *":$$d:"*) ;; *) why="not on your PATH" ;; esac; fi; \
			[ -n "$$why" ] || why="not writable (owned by another user)"; \
			printf '  %-28s %s\n' "$$d" "$$why" >&2; \
		done; \
		echo "" >&2; \
		echo "./$(BINARY) is built — finish the install with either:" >&2; \
		echo "" >&2; \
		echo "  sudo install -m 0755 ./$(BINARY) /usr/local/bin/$(BINARY)" >&2; \
		echo "      system-wide; asks for your password" >&2; \
		echo "" >&2; \
		echo "  make install PREFIX=\"\$$HOME/.local/bin\"" >&2; \
		echo "      then put that dir on your PATH, e.g. (zsh — bash: ~/.bashrc):" >&2; \
		echo "      echo 'export PATH=\"\$$HOME/.local/bin:\$$PATH\"' >> ~/.zshrc" >&2; \
		exit 1; \
	fi; \
	if [ ! -w "$$dir" ]; then \
		echo "error: $$dir is not writable — re-run with sudo, or 'make install PREFIX=<writable dir on PATH>'." >&2; \
		exit 1; \
	fi; \
	rm -f "$$dir/$(BINARY)"; \
	cp "$(BINARY)" "$$dir/$(BINARY)" || exit 1; \
	chmod 0755 "$$dir/$(BINARY)" || exit 1; \
	echo "installed $(BINARY) -> $$dir/$(BINARY)"; \
	case ":$$PATH:" in \
		*":$$dir:"*) \
			found="$$(command -v $(BINARY) 2>/dev/null || true)"; \
			if [ -n "$$found" ] && [ "$$found" != "$$dir/$(BINARY)" ]; then \
				echo "warning: '$(BINARY)' still resolves to $$found, which comes earlier on your PATH — remove that copy or reorder PATH." >&2; \
			fi ;; \
		*) echo "warning: $$dir is not on your PATH — add it (e.g. 'export PATH=\"$$dir:\$$PATH\"') to run '$(BINARY)' by name." >&2 ;; \
	esac

## test: run the full test suite with the race detector
.PHONY: test
test:
	go test -race -count=1 ./...

## live-eval: run the opt-in live-model eval and the gated judge tests against a real local model (always -count=1, never cached)
#
# The recipe counts the REAL apogee home before and after the run and fails on growth
# (checklist T-22 step 9). No test may write there — every one of them takes a temp home —
# and a live run is exactly where a missing --config goes unnoticed, because the run that
# leaks is also the run that passes. The count is entries, not bytes: a new session record
# or a new scratch dir is what a leak looks like.
.PHONY: live-eval
live-eval:
	@before="$$($(MAKE) --no-print-directory home-census)"; 	APOGEE_LIVE_ENDPOINT=$(LIVE_ENDPOINT) APOGEE_JUDGE_ENDPOINT=$(JUDGE_ENDPOINT) go test -race -count=1 -run 'TestE2ELiveModel|TestLiveDelegateCapAndWorkingWindow|TestJudge' -v ./internal/judge/ ./internal/tui/ ./internal/agent/ ./cmd/apogee/; 	rc=$$?; 	after="$$($(MAKE) --no-print-directory home-census)"; 	if [ "$$before" != "$$after" ]; then 		echo "" >&2; 		echo "live-eval: the real apogee home GREW during this run — a test reached ~/.apogee" >&2; 		echo "  before: $$before" >&2; 		echo "  after:  $$after" >&2; 		echo "Keep the new files as evidence and find the run that omitted --config/APOGEE_CONFIG." >&2; 		exit 1; 	fi; 	exit $$rc

## home-census: print the entry counts of the real ~/.apogee sessions and scratch dirs (used by live-eval)
.PHONY: home-census
home-census:
	@h="$${APOGEE_CONFIG:-$$HOME/.apogee}"; 	printf 'sessions=%s scratch=%s\n' 		"$$(ls -1 "$$h/sessions" 2>/dev/null | wc -l | tr -d ' ')" 		"$$(ls -1 "$$h/scratch" 2>/dev/null | wc -l | tr -d ' ')"

## release-smoke: verify a PUBLISHED release end to end (make release-smoke VERSION=v0.18.0)
#
# The post-publish half of checklist T-21: the six archives exist and their SHA256SUMS
# verify, the host's own archive unpacks to a binary reporting that version, and — when
# Homebrew is installed — `brew upgrade apogee` moves this machine onto it. It is never
# part of `make check`: there is nothing to smoke until a release is cut, and the target
# reaches the network on purpose. VERSION= names the released tag, defaulting to the tag
# the VERSION file currently claims.
.PHONY: release-smoke
release-smoke:
	@VERSION="$(VERSION)" ./scripts/release-smoke.sh

## fmt: format all Go source in place
.PHONY: fmt
fmt:
	gofmt -w .

## vet: run go vet over the module
.PHONY: vet
vet:
	go vet ./...

## actionlint: lint the GitHub workflow files with the pinned actionlint
.PHONY: actionlint
actionlint:
	$(ACTIONLINT) .github/workflows/*.yml

## cross: build every release target (CGO off); fails on the first broken one
.PHONY: cross
cross:
	@for t in $(CROSS_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		printf '  -> %s/%s\n' "$$os" "$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o /dev/null ./... || exit 1; \
	done
	@echo "cross-build OK ($(words $(CROSS_TARGETS)) targets)"

## dist: build the publishable release archives for every target into dist/ (+ SHA256SUMS)
.PHONY: dist
dist:
	@command -v zip >/dev/null 2>&1 || { echo "error: 'zip' is required for the Windows archives — install it (brew install zip / apt-get install zip)." >&2; exit 1; }
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)
	@v='$(DIST_VERSION)'; \
	[ -n "$$v" ] || { echo "error: could not read a version from VERSION" >&2; exit 1; }; \
	for t in $(CROSS_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		name="$(BINARY)_$${v}_$${os}_$${arch}"; \
		exe="$(BINARY)"; [ "$$os" != windows ] || exe="$(BINARY).exe"; \
		printf '  -> %s\n' "$$name"; \
		mkdir -p "$(DIST_DIR)/$$name" || exit 1; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -trimpath -ldflags "$(GO_LDFLAGS)" -o "$(DIST_DIR)/$$name/$$exe" $(PKG) || exit 1; \
		cp LICENSE README.md "$(DIST_DIR)/$$name/" || exit 1; \
		if [ "$$os" = windows ]; then \
			(cd $(DIST_DIR) && zip -qr "$$name.zip" "$$name") || exit 1; \
		else \
			tar -czf "$(DIST_DIR)/$$name.tar.gz" -C "$(DIST_DIR)" "$$name" || exit 1; \
		fi; \
		rm -rf "$(DIST_DIR)/$$name"; \
	done
	@cd $(DIST_DIR) && $(SHA256) *.tar.gz *.zip > SHA256SUMS
	@echo "dist OK -> $(DIST_DIR)/ ($(words $(CROSS_TARGETS)) archives + SHA256SUMS)"

## check: the Phase-2 acceptance gate (fmt-check, vet, build, race tests, ADR-0010, cross, --help)
.PHONY: check
check:
	@echo "==> gofmt (must be empty)"
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi
	@echo "==> go vet"
	@go vet ./...
	@echo "==> go vet (windows build tag: the Windows-tagged tests must still compile)"
	@GOOS=windows go vet ./internal/platform/... ./internal/probe/...
	@echo "==> go build ./..."
	@go build ./...
	@echo "==> go test -race ./..."
	@go test -race -count=1 ./...
	@echo "==> workflow action pins (SHA + version comment)"
	@./scripts/check-pins.sh
	@echo "==> actionlint"
	@$(MAKE) --no-print-directory actionlint
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
