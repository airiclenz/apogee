# Apogee — developer Makefile.
#
# Thin convenience wrapper over the Go toolchain. The source of truth for the
# build is `go build`; these targets just give the common invocations one-word
# names and bundle the pre-commit gate into a single `make check` (it began as the Phase-2
# acceptance gate, docs/plans/archived/phase-2-detail-plan.md §7, and has grown since).

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

# The 6 release targets the cross-build invariant must stay green on.
CROSS_TARGETS := \
	linux/amd64   linux/arm64 \
	darwin/amd64  darwin/arm64 \
	windows/amd64 windows/arm64

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

# golangci-lint runs the standard linter set over the module, configured by `.golangci.yml`.
# Pinned by MODULE VERSION and fetched by `go run` for the same reason actionlint is: no
# third-party GitHub Action in the workflow's context, and no second version literal to drift.
# CI runs `make lint` rather than its own copy of the command. After the first run the module
# cache serves the tool, so an offline build still lints.
GOLANGCI_LINT_VERSION := v2.13.2
GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# govulncheck reports known vulnerabilities in the module's dependencies, filtered to the ones
# actually reachable from this code. Pinned by MODULE VERSION and fetched by `go run` like the
# two tools above; CI runs `make vulncheck` so the version literal here is the only one. Unlike
# them this target NEEDS THE NETWORK every run: it queries the Go vulnerability database, which
# is not cached. Offline, the target fails with the tool's own error — that is by design, since
# a vulnerability gate that silently passes when it cannot reach the database is not a gate.
GOVULNCHECK_VERSION := v1.7.0
GOVULNCHECK = go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

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

## sessionexpect: build the session-entry expect judge to ./sessionexpect (a dev tool, never a release asset)
.PHONY: sessionexpect
sessionexpect:
	go build -o sessionexpect ./cmd/sessionexpect

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

## test: run the full test suite with the race detector, as a queue of short-lived processes over a few lanes
#
# The suite's wall time sits almost entirely in two packages, and both are `t.Parallel` inside
# (serial only where a test itself `t.Setenv`s or swaps a package-level seam — directly or
# through a helper — which a guard in each package pins). The script builds each heavy package's
# test binary once, packs its tests into chunks of about two minutes, packs the other packages
# into batches, and runs the queue over a fixed number of lanes, a fresh process per item — a
# split by PROCESS, which changes no test, and which bounds how much memory any one process
# holds. Each process gets a `-parallel` bound (its share of the process budget, 1 at the
# default sizing), so the lanes spend the box and a process's own fan-out does not multiply the
# load. scripts/test-shards.sh holds the reasoning and the plan; `go test -race -count=1 ./...`
# remains the equivalent single-process run for a bisect or a one-off.
# APOGEE_TEST_LANES=n sets the lane count; APOGEE_TEST_SLOW=1 is the slow-box plan (Raspberry-Pi
# class: four lanes, three under 7 GiB RAM, every process -parallel 1, test budgets doubled);
# APOGEE_TEST_TIME_SCALE=x scales the test budgets; APOGEE_TEST_RACE=0 drops -race for a kernel that cannot run it — all
# explained in the script's header.
.PHONY: test
test:
	@./scripts/test-shards.sh $(ARGS)

## test-timings-seed: copy the last run's .test-timings into the committed scripts/test-timings.seed
#
# The seed is what scripts/test-shards.sh packs by on a checkout that has never run the suite —
# CI's runner every time — so its shards balance like a warm local run instead of the equal-cost
# fallback (185 s against 117 s for CI's two cmd/apogee shards before the seed). Refresh it from
# a dev-box `make test` when the suite's shape moves; a stale seed only costs balance, never a
# test. The header line names the box the timings came from, since a slow host's absolute
# durations still rank the tests correctly but read oddly beside a fast box's.
.PHONY: test-timings-seed
test-timings-seed:
	@test -f .test-timings || (echo 'run make test first' && exit 1)
	@{ \
		printf '# scripts/test-timings.seed — shard-timings fallback for scripts/test-shards.sh (importpath<TAB>TestName<TAB>seconds).\n'; \
		printf '# Harvested by `make test` on %s (%s cores); regenerate with `make test-timings-seed`.\n' \
			"$$(uname -m)" "$$( (command -v nproc >/dev/null 2>&1 && nproc) || sysctl -n hw.ncpu 2>/dev/null || echo '?')"; \
		cat .test-timings; \
	} >scripts/test-timings.seed

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

## release-status: report the release gate, the baseline and each publish step (make release-status [VERSION=vX.Y.Z])
#
# A wrapper over `cmd/release status` (ADR 0098): read-only apart from fetching origin, it names
# each condition blocking the release gate and probes every publish step for the version.
# VERSION= is passed through verbatim; left empty, the verb defaults to the VERSION file. It exits
# 1 while the gate is blocked, so it is never part of `make check`.
.PHONY: release-status
release-status:
	go run ./cmd/release status $(VERSION)

## release-smoke: verify a PUBLISHED release from the outside (make release-smoke [VERSION=vX.Y.Z])
#
# A wrapper over `cmd/release smoke`, which `cmd/release publish` also runs as its last step: the
# tag is on origin and annotated, the six archives download and match SHA256SUMS, the host's
# archive reports the version, `brew upgrade apogee` moves a Homebrew install onto it and the
# Scoop bucket names it. VERSION= is passed through verbatim; left empty, the verb defaults to the
# VERSION file. Never part of `make check`: it reaches the network on purpose.
.PHONY: release-smoke
release-smoke:
	go run ./cmd/release smoke $(VERSION)

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

## lint: run golangci-lint (standard set, .golangci.yml) over the module
.PHONY: lint
lint:
	$(GOLANGCI_LINT) run ./...

## vulncheck: scan the dependency graph for known vulnerabilities (needs the network)
.PHONY: vulncheck
vulncheck:
	$(GOVULNCHECK) ./...

## cross: build every release target (CGO off); reports every broken one
#
# The six builds share nothing but the module cache, which is concurrency-safe by design, so
# they run at once — six sequential full-module builds is the single largest non-test cost in
# `make check`. Every target is built even when one fails, because "which platforms are broken"
# is more useful than "the first one alphabetically".
.PHONY: cross
cross:
	@pids=""; \
	for t in $(CROSS_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		( GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(GO_LDFLAGS)" -o /dev/null ./... \
			&& printf '  -> %s/%s ok\n' "$$os" "$$arch" \
			|| { printf '  -> %s/%s FAILED\n' "$$os" "$$arch"; exit 1; } ) & \
		pids="$$pids $$!"; \
	done; \
	rc=0; for p in $$pids; do wait $$p || rc=1; done; \
	[ $$rc -eq 0 ] || { echo "cross-build FAILED" >&2; exit 1; }; \
	echo "cross-build OK ($(words $(CROSS_TARGETS)) targets)"

## dist: build the six release archives and SHA256SUMS into dist/ (a wrapper over cmd/release dist)
.PHONY: dist
dist:
	go run ./cmd/release dist

## check: the pre-commit gate (gofmt, Windows vet, lint, build, vulncheck, race tests, action pins, actionlint, ADR-0010, cross, --help)
.PHONY: check
check:
# The gate is raced by definition: an unraced pass (APOGEE_TEST_RACE=0, a slow-box convenience
# scripts/test-shards.sh offers) never stands in for it, so the `==> go test -race` echo below stays true.
	@if [ "$${APOGEE_TEST_RACE:-}" = 0 ]; then echo "make check: APOGEE_TEST_RACE=0 is set; the gate runs raced — unset it, or run make test for an unraced pass" >&2; exit 1; fi
	@echo "==> gofmt (must be empty)"
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi
# Plain `go vet ./...` has no step of its own: golangci-lint's standard set runs govet.
# The Windows vet covers the whole module, not only the trees CI's test-windows job runs: any
# package's Windows build must still compile, and its Windows-tagged files must vet clean.
	@echo "==> go vet (windows build tag: the whole module's Windows build must still compile)"
	@GOOS=windows go vet ./...
	@echo "==> golangci-lint"
	@$(MAKE) --no-print-directory lint
	@echo "==> go build ./..."
	@go build ./...
	@echo "==> govulncheck"
	@$(MAKE) --no-print-directory vulncheck
	@echo "==> go test -race (chunks over lanes — scripts/test-shards.sh)"
	@$(MAKE) --no-print-directory test
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
	@echo "all make check gates passed"

## clean: remove the built binary
.PHONY: clean
clean:
	rm -f $(BINARY)
