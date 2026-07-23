# Plan — A build number in the version string

**Date:** 2026-07-23
**Status:** **READY TO IMPLEMENT** — not started.
**Track:** post-`v1.7.1` version-string affordance. Touches `version.go`, the `Makefile`,
the two version tests, and `CHANGELOG.md`. No public Go API change.

**Owner-ratified decisions (2026-07-23):**

- The version string gains a **build number** rendered as `vX.Y.Z+<count>.g<rev>[.dirty]`,
  e.g. `v1.7.1+436.g28b6f838e6e1` (the owner asked for exactly `v1.7.1+842.g…` — number,
  then `.g<hash>`, no `build.` word).
- The number is the repository's **commit count**, `git rev-list --count HEAD` (per-commit,
  reproducible). It is **not** a per-compile counter — a literal "+1 every `go build`" was
  rejected (it needs persisted mutable state, a build wrapper, and is non-reproducible).

**Authoritative sources (precedence):** the `version.go` file-level doc comment and the
`Makefile` header comment are the current contract. Where any item text below disagrees with
the owner-ratified decisions above, flag **BUILD-NUMBER QUESTION** rather than deviate.

## Why

`apogee.Version()` today returns `<VERSION-file>` + an optional `+g<rev>[.dirty]` provenance
suffix (`version.go:35`). There is **no build number** — `g28b6f838e6e1` is the git commit
hash, not a counter. The owner wants a number that ticks up per commit, printed as
`v1.7.1+436.g28b6f838e6e1`.

The one hard constraint that shapes the whole change: **the commit count is the single fact
the runtime cannot observe.** `debug.ReadBuildInfo()` stamps `vcs.revision`, `vcs.time`, and
`vcs.modified` for free — but **no count**. So the count must be injected at build time.

That directly re-opens a property commit `37bcd42` ("source the version from the top-level
VERSION file") deliberately established: *no `-ldflags`, every build path reports the same*
(`Makefile:12-16`). We accept the smallest possible re-introduction:

- The **version number** stays sourced solely from the embedded `VERSION` file on **every**
  build path — `go build`, `go run`, `go install`, `make build` — with **no** `-ldflags`
  override and therefore **no drift**. This invariant is preserved intact.
- Only the **build-count field of the provenance suffix** is `-ldflags`-injected. A build
  that does not pass it (a bare `go build ./cmd/apogee`) simply omits the number and reports
  `+g<rev>` as it does today — a graceful, honest fallback (absent, never stale).

**Consequence to accept explicitly:** `make build` / `make run` / `make install` /
`make cross` carry the count; a raw `go build`/`go run` of `./cmd/apogee` does not. The
release artifacts (all built through `make`) always carry it. This asymmetry is the price of
a real counter and is the intended behaviour, not a bug.

## Where things stand (grounded, verified 2026-07-23)

- **`Version()`** (`version.go:35`) = `baseVersion()` + `"+" + buildMetadata(info.Settings)`
  when the suffix is non-empty. `baseVersion()` (`:49`) returns the trimmed `VERSION` file
  verbatim — the file currently holds **`v1.7.1`** (with the leading `v`), so the base already
  includes the `v`; the plan does not touch base handling.
- **`buildMetadata(settings)`** (`version.go:60`) is the pure composer: it reads
  `vcs.revision` + `vcs.modified`, truncates the revision to `commitShortLen = 12`
  (`:24`), prepends `"g"`, and appends `".dirty"` when modified. Returns `""` when there is
  no revision.
- **`commitShortLen = 12`** (`version.go:24`); module path is
  **`github.com/airiclenz/apogee`** (`go.mod`); current `git rev-list --count HEAD` = **436**.
- **`TestBuildMetadata`** (`version_internal_test.go:10`, `package apogee`) drives the pure
  composer with synthetic settings — the natural home for the new count cases.
- **`TestVersionMatchesVERSIONFile`** (`version_test.go:16`, `package apogee_test`) cuts
  `Version()` on the first `"+"` and asserts the base equals the `VERSION` file. The cut still
  works with a `<count>.` prefix in the suffix; only its explanatory comment (`:27-28`)
  mentions `"+g<commit>"` and should be widened.
- **`Makefile`**: `MODULE` at `:10`; `build` at `:50-51` (`go build -o $(BINARY) $(PKG)`);
  `run` at `:55-56` (`go run $(PKG) $(ARGS)`); `install: build` at `:60`; `cross` inner build
  at `:109`; header comment claiming "no `-ldflags` stamp here" at `:12-16`. `check` (`:115`)
  is a validation gate, not a release artifact.
- **`Version()` is surfaced** via `cmd/apogee/root.go:118` and `cmd/apogee/wire.go:304`
  (Cobra `--version` + the `Options.Version` seam the TUI `/version` command and start-up box
  read). No consumer parses the suffix, so the format change is display-only and safe.
- **CHANGELOG `[Unreleased]` is stale.** Its "Added" bullet (`CHANGELOG.md:~19`) still
  describes the pre-`37bcd42` design — a `internal/version` package resolving
  `git describe --tags --always --dirty` via `-ldflags -X`. The code now uses
  `go:embed VERSION` + runtime `BuildInfo` and there is no `internal/version` package. This
  bullet is the exact text item 3 edits.

## 1. `version.go` — the `buildCount` seam and a count-aware composer

**What:**

Add a package-level injection seam beside `commitShortLen` (`version.go:24`):

```go
// buildCount is the build number — the repository's commit count
// (`git rev-list --count HEAD`) at build time. It is the ONE value the version
// string cannot derive at runtime: Go's VCS stamp (debug.ReadBuildInfo) carries the
// revision and dirty flag but no count, so a release build injects it via
// `-ldflags -X github.com/airiclenz/apogee.buildCount=<n>` (see the Makefile). A build
// that does not pass it (a bare `go build`/`go run`) leaves it empty and the suffix
// simply omits the number — the version NUMBER itself is unaffected, always the
// embedded VERSION file. This is provenance, never a second source for the version.
var buildCount string
```

Change the composer's signature to take the count and prepend it to the `g<rev>` anchor —
**only when there is a revision** (the count decorates the git provenance; with no revision
the whole suffix is empty as today):

```go
func buildMetadata(count string, settings []debug.BuildSetting) string {
	var revision string
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > commitShortLen {
		revision = revision[:commitShortLen]
	}
	meta := "g" + revision
	if count = strings.TrimSpace(count); count != "" {
		meta = count + "." + meta
	}
	if modified {
		meta += ".dirty"
	}
	return meta
}
```

Update the call site in `Version()` (`version.go:41`) to pass the seam:

```go
if meta := buildMetadata(buildCount, info.Settings); meta != "" {
	return base + "+" + meta
}
```

Refresh the two doc comments so the file stays self-describing and honest:

- The `Version()` examples (`version.go:32`) become `"1.7.1+436.g28b6f838e6e1"`,
  `"1.7.1+436.g28b6f838e6e1.dirty"`, `"1.7.1+g28b6f838e6e1"` (bare `go build`, no count),
  and bare `"1.7.1"` (no VCS). Note the count is `-ldflags`-injected build provenance.
- The file-level comment (`version.go:9-20`) must keep its core claim — **the version
  *number* has no `-ldflags` override and cannot drift from the `VERSION` file** — while
  adding one sentence: the build *count* is the sole `-ldflags`-injected field, is
  provenance-only, and is absent (not stale) on any build that does not pass it. Do **not**
  let the edit read as "we now stamp the version via ldflags" — that is false and is the
  invariant we are protecting.

`strings` is already imported (`version.go:6`).

**Tests:** update `TestBuildMetadata` (`version_internal_test.go`) — add a `count string`
field to the table, thread it through `buildMetadata(tt.count, tt.settings)`, keep the five
existing rows with `count: ""`, and add:

- count + clean revision → `"436.g28b6f838e6e1"`.
- count + dirty revision → `"436.g28b6f838e6e1.dirty"`.
- count set but **no** revision → `""` (count is dropped with the empty suffix).
- count with surrounding whitespace (`" 436 "`) → `"436.g28b6f838e6e1"` (trim proof).

No new `Version()`-level test — reading the real `buildCount`/build stamp of the test binary
would be brittle; the pure composer is the coverage boundary, as it is today.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean; diff confined to
`version.go` + `version_internal_test.go`. Commit:
`feat(version): prepend an optional build number to the provenance suffix`.

## 2. `Makefile` — compute the count and inject it via `-ldflags`

**What:** after `MODULE` (`Makefile:10`), add the count and a guarded ldflags fragment:

```make
# The build number is the repository's commit count, injected at build time (see
# version.go / apogee.buildCount). It is the one field the runtime cannot derive —
# Go's VCS stamp carries the revision but no count — so it travels through -ldflags -X.
# A bare `go build` omits it and reports just `+g<rev>`; the version NUMBER is the
# embedded VERSION file on every path and never drifts.
BUILD_COUNT := $(shell git rev-list --count HEAD 2>/dev/null)
GO_LDFLAGS  := $(if $(BUILD_COUNT),-X $(MODULE).buildCount=$(BUILD_COUNT))
```

Rewrite the header comment (`Makefile:12-16`) so it no longer claims "there is no `-ldflags`
stamp here": state that the *version number* is embed-sourced and identical on every path,
while the *build number* is the one `-ldflags`-injected provenance field (present on `make`
builds, absent on a bare `go build`).

Thread `-ldflags "$(GO_LDFLAGS)"` into the artifact-producing targets:

- `build` (`:51`): `go build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) $(PKG)`
- `run` (`:56`): `go run -ldflags "$(GO_LDFLAGS)" $(PKG) $(ARGS)`
- `cross` inner build (`:109`): add `-ldflags "$(GO_LDFLAGS)"` before `-o /dev/null`.

`install` (`:60`) depends on `build`, so it inherits the count — no change. Leave `check`
(`:115`) **as-is**: it is a compile/gate check, not a shipped artifact, and an empty
`-ldflags ""` (when `BUILD_COUNT` is empty, e.g. outside a git tree) is a harmless no-op if a
future edit ever adds it.

**Tests:** manual, since this is build wiring (note the exact commands in the commit body):

- `make build && ./apogee --version` → `v1.7.1+436.g<rev>` (count present; `436` will differ
  as commits land).
- `go build -o /tmp/apogee-nold ./cmd/apogee && /tmp/apogee-nold --version` →
  `v1.7.1+g<rev>` (**no** count — proves the graceful fallback).
- `make check` stays green (`cross` still builds all six targets with the injected flag).

**Acceptance:** the three manual checks above pass; `make check` green. Commit:
`build(make): stamp the commit-count build number via -ldflags`.

## 3. `CHANGELOG.md` — correct the stale `--version` bullet and record the build number

**What:** in `[Unreleased] › Added`:

- **DOC-TRUTH fix (folded in because this change edits the same bullet):** the existing
  `apogee --version` bullet (`CHANGELOG.md:~19`) describes the superseded design — an
  `internal/version` package resolving `git describe --tags --always --dirty` via
  `-ldflags`. Rewrite it to the shipped design: the version is the embedded top-level
  `VERSION` file (single source of truth, no `-ldflags`, identical on every build path), with
  an optional runtime provenance suffix from `debug.ReadBuildInfo` (`+g<rev>`, `.dirty` on a
  modified tree). Keep the Cobra `--version` / `Options.Version` seam / `/version` / start-up
  box wording, which is still accurate.
- **The build number (this change):** the version now also carries a build number on release
  builds — the commit count (`git rev-list --count HEAD`) injected via `-ldflags -X`, rendered
  as `vX.Y.Z+<count>.g<rev>[.dirty]` (e.g. `v1.7.1+436.g28b6f838e6e1`). State the honest
  boundary: the *number* is always the embedded `VERSION` file; the *count* is provenance that
  a bare `go build` omits.

**VERSION QUESTION (owner call, do not presume):** the public Go API is unchanged (the count
is build metadata, not an API surface — the CHANGELOG's SemVer clause governs the Go API), so
the recommendation is **not** to bump `VERSION`; this entry rides the next release cut under
`[Unreleased]`. If the owner would rather cut a version now, that is a one-line `VERSION` edit
outside this plan.

**Tests:** none (docs). Confirm no other CHANGELOG bullet references the removed
`internal/version` package or `git describe`.

**Acceptance:** `[Unreleased]` reads true against the shipped code; no dangling reference to
the superseded design. Commit: `docs(changelog): record the build number and fix the stale --version note`.
