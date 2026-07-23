# Plan — Start-up box shows the release version only (no build provenance)

**Date:** 2026-07-23
**Status:** **READY TO IMPLEMENT** — not started.
**Track:** post-`v1.8.0` follow-up to the build-number work
(`docs/plans/2026-07-23 - 04 - version-build-number-plan.md`). Touches `version.go`, the TUI
`Options` seam + start-up seed, the two `cmd/apogee` wiring sites, and their tests. No public
behaviour change to `--version` or `/version`.

**Owner-ratified decisions (2026-07-23):**

- The **start-up box** shows the **simple release version only** — the `VERSION`-file value,
  e.g. `v1.8.0`, with **no** `+<count>.g<rev>[.dirty]` provenance suffix.
- The **`--version` flag** and the in-TUI **`/version` command** keep showing the **full**
  version string (`v1.8.0+437.g250aff44891f`), unchanged.

## Why

Today all three surfaces read one value — `apogee.Version()`, the full string with build
provenance — threaded through `Options.Version` (`tui.go:101`). The owner wants the start-up
box (the always-visible launch card) to read clean, while the two *explicit* version queries
(`--version`, `/version`) keep the full provenance for support/debugging. So the box needs the
**base** version and the two queries need the **full** version — two values, not one.

`baseVersion()` (`version.go:63`) already computes exactly the base (trimmed `VERSION`, or
`"dev"`), but it is unexported, and **ADR-0010 forbids `internal/*` from importing the root
facade** — the TUI cannot call it. The value must be threaded in through `Options`, exactly as
`Version` already is (set in `cmd/apogee`, which *is* allowed to import the facade). That makes
this a "expose the base + add one more seam field" change, not a renderer change.

## Where things stand (grounded, verified 2026-07-23)

- **`Version()`** (`version.go:49`) = `baseVersion()` + `"+" + buildMetadata(...)`;
  **`baseVersion()`** (`version.go:63`, unexported) returns the trimmed `VERSION` file (or
  `"dev"`) — the exact "simple version" wanted, and its only caller is `Version()` (`:50`).
- **`Options.Version`** (`tui.go:101-104`) is the single version seam; its doc says the
  `/version` command **and** the start-up box both read it. Grep confirms exactly two readers:
  the start-up seed and `/version`.
- **Start-up box seed** (`model.go:140-146`): `addStartup(startupView{… Version: opts.Version})`
  at `:145`. `startupView.Version` (a render struct) flows to `renderStartupBox` and is printed
  at `render.go:256` (wide) / `:309` (stacked). **There is no footer version surface** — the
  footer renders host/model/context, not the version.
- **`/version` command** (`model.go:604-609`): `addNote("apogee " + m.opts.Version)` at `:607`.
- **`--version`** is Cobra's flag, fed by `cmd.Version` from `apogee.Version()` (`root.go:118`).
- **Both `cmd/apogee` wiring sites set the full string:** `root.go:118` (`Version: apogee.Version()`)
  and `wire.go:304` (`Version: apogee.Version()`), each with a nearby comment claiming the
  start-up box reads this value — those comments go stale under this change.
- **Tests:** `TestNewModelSeedsStartupBox` (`model_test.go:131`) pins
  `e.startup.Version == opts.Version` (`:157-158`) and sets `opts.Version` at `:137` — this
  assertion must move to the base seam. `TestRenderStartupBox` (`render_test.go:667`) is a pure
  render test over a literal `startupView` — unaffected (it only checks the string appears). A
  `/version` path test sets `opts.Version = "v1.2.3"` (`model_test.go:96`) — must keep expecting
  the **full** `Options.Version`, unchanged.

## Decisions this plan implements

- **Approach: expose the base in the facade and thread it as its own named seam.** Add
  exported `apogee.BaseVersion()` (the format authority stays in `version.go`), carry it in a
  new `Options.BaseVersion` field set by `cmd/apogee`, and seed the start-up box from it. The
  renderer and `startupView` are untouched — the box just receives a different string.
- **Rejected: cut `Options.Version` on `"+"` inside the TUI.** It is fewer edits but leaks the
  version-string format into the renderer/model layer, duplicating a contract that `version.go`
  already owns. The ADR-0010 seam pattern (values enter the TUI pre-resolved by `cmd/apogee`)
  is the house style; keep the TUI format-agnostic. (Consistent with the memory: prefer the
  best long-term architecture over lowest churn.)

## 1. `version.go` — export `BaseVersion()`, the release version without provenance — ✅ DONE (2026-07-23)

**What:** promote the existing unexported `baseVersion()` (`version.go:61-66`) to an exported
`BaseVersion()` and update its one internal caller in `Version()` (`:50`). Give it a doc
comment stating it is the release version alone — the trimmed `VERSION` file (or `"dev"`) with
**no** build-provenance suffix — the value the start-up box shows, versus `Version()` which
appends provenance. Keep the trim/`"dev"` fallback behaviour identical.

```go
// BaseVersion is the release version alone: the trimmed VERSION file (or "dev" if it is empty
// or whitespace-only), with NONE of the "+<count>.g<rev>[.dirty]" build provenance Version()
// appends. It is what the start-up box displays; --version and /version show Version().
func BaseVersion() string {
	if v := strings.TrimSpace(versionFile); v != "" {
		return v
	}
	return "dev"
}
```

and in `Version()`: `base := BaseVersion()`.

**Tests:** in `version_test.go` (external `apogee_test`), add `TestBaseVersion`: it equals the
trimmed `VERSION` file contents and contains no `"+"`. Confirm `TestVersionMatchesVERSIONFile`
(which cuts `Version()` on `"+"`) still holds — its `want` is the same file, so `BaseVersion()`
and the cut base must agree; optionally tighten it to assert `apogee.BaseVersion() == want`
directly instead of the `strings.Cut`.

**Acceptance:** `go build ./... && go vet ./... && go test ./... ` (root package) clean; diff
confined to `version.go` + `version_test.go`. Commit:
`refactor(version): export BaseVersion — the release version without build provenance`.

## 2. Thread the base to the start-up box; leave `--version` / `/version` on the full string — ✅ DONE (2026-07-23)

NOTES (2026-07-23): `root.go:118`'s `Version: apogee.Version()` is the **Cobra command's**
`Version` field (the `--version` flag), not a `tui.Options` field — `cobra.Command` has no
`BaseVersion` field, and `--version` must stay full — so no `BaseVersion` was (or could be) added
there; only its stale comment was corrected. The `BaseVersion: apogee.BaseVersion()` seam is set in
the single `tui.Options` literal, which lives only in `wire.go` (as the plan's own line 44 notes).

**What:**

- **`internal/tui/tui.go`** — add a `BaseVersion string` field to `Options` beside `Version`
  (`:101-104`). Document it: the release version without provenance, shown by the start-up box;
  empty ⇒ unwired. Update the `Version` field's doc so it no longer claims the start-up box
  reads it — `Version` is now read only by `/version` (and mirrors `--version`).
- **`internal/tui/model.go`** — change the start-up seed (`:145`) from `Version: opts.Version`
  to `Version: opts.BaseVersion`. Leave the `/version` case (`:607`) reading `m.opts.Version`
  (full) untouched.
- **`cmd/apogee/root.go`** and **`cmd/apogee/wire.go`** — set `BaseVersion: apogee.BaseVersion()`
  alongside the existing `Version: apogee.Version()` (`root.go:118`, `wire.go:304`). Correct the
  two nearby comments that say the start-up box reads `Options.Version` — it now reads
  `Options.BaseVersion`; the full `Version` feeds `--version` and `/version`.

`startupView` and `renderStartupBox` need **no** change — the box renders whatever string it is
seeded with.

**Tests:** update `TestNewModelSeedsStartupBox` (`model_test.go:131`) — set both
`opts.Version` (a full-looking string, e.g. `"v1.2.3+45.gdeadbee"`) and `opts.BaseVersion`
(`"v1.2.3"`), and assert `e.startup.Version == opts.BaseVersion` (proving the box drops the
provenance). Verify the `/version` path test (`model_test.go:96`) still expects the full
`Options.Version`. `TestRenderStartupBox` is unaffected.

**Acceptance:** `make check` green. Manual proof: `make build && ./apogee` — the start-up box
shows `v1.8.0`; inside the TUI, `/version` prints `apogee v1.8.0+<count>.g<rev>`; and
`./apogee --version` prints the full string. Commit:
`feat(tui): show the release version (no build provenance) in the start-up box`.
