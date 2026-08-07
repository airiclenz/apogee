# Context-size formatter — unit-capped display everywhere

- **Goal:** one global formatter for context/token sizes that never shows a value ≥ 1000 in its unit (e.g. `1M`, never `1048k`), used at every display site; kill the duplicate helpers.
- **Date:** 2026-08-07
- **Status:** not started
- **Authoritative sources:** `ISSUES.md` line 5 (the request); the Ratified design calls below; the site inventory enumerated in the items (swept 2026-08-07).
- **Ratified design calls** (owner, via AskUserQuestion, 2026-08-07):
  1. **Binary units with plain suffixes:** divide by 1024 per step, display `k` / `M` / `G`. Rationale: real context windows are powers of two (`32768`, `131072`) and models are named that way ("128k model"). Accepted consequences: `128000 → "125k"`, `1000000 → "977k"`.
  2. **Precision:** coarse form shows integer `k` (normal half-up rounding, not truncation — `1999 → "2k"`, a behavior change from today), and one decimal for `M`/`G` when the displayed value is < 10 (`"1.2M"`, `"15M"`). The fine variant keeps one decimal in every unit.
  3. **`apogee probe host` stays raw** (`"context window 131072"`): probe is a diagnostic and the exact server-reported number is the point. Out of scope.
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item text must land as a dated NOTES line under the item.
- **Out of scope:**
  - `internal/probe/host.go:181,188` raw ints (design call 3).
  - `/settings` value cell (`cmd/apogee/settingsrows.go:119`) — documented contract at `internal/tui/tui.go:316,894`: the cell doubles as the edit-field seed and must spell the value as the config file would.
  - `formatBytes` (`internal/tui/model.go:4064`) and the context-files `KiB` notices — file bytes, not tokens; different domain, stays binary `KiB/MiB`.
  - `throughputSuffix` `tok/s` rate (`internal/tui/model.go:3816`).
  - Model-facing `%d bytes` strings in `internal/tools/*` (machine-facing, not display).
  - Sessions browser: `Meta.CtxUsed` is persisted but never displayed — adding a display is not this plan.
  - The narrow startup layout dropping the context row (`internal/tui/render.go:884`) — pre-existing, unrelated.

## 1. Shared `internal/format` package with the unit-capped token formatter

**What:** Create a new stdlib-only package `internal/format` with two exported functions, the single global formatter for token/context-size display (this supersedes the deliberate `formatTokens` twin in `cmd/apogee/headless.go`, removed in item 3):

- `Tokens(n int) string` — coarse form.
  - `n <= 0` → `""` (the empty string is a load-bearing "unknown" sentinel: callers drop rows/cells on it).
  - `n < 1000` → bare integer (`"999"`).
  - Otherwise walk the ladder `k=1024`, `M=1024²`, `G=1024³`: pick the smallest unit whose half-up-rounded display value is < 1000. `k` always renders as a rounded integer (`"2k"`, `"32k"`, `"977k"`); `M`/`G` render with one decimal when the displayed value is < 10 (`"1.0M"`, `"1.2M"`) and as a rounded integer otherwise (`"15M"`).
- `TokensFine(n int) string` — fine form (replaces `formatTokensFine`): same sentinel (`n <= 0` → `""` — note: the old helper returned `"0"`; its only call site passes positive values, so this unification is behavior-neutral), same ladder and unit-cap, but always one decimal once a unit applies (`"4.1k"`, `"32.0k"`, `"1.2M"`).

Package doc comment states the ratified rule and the binary-÷1024 rationale (design calls 1–2) so future sites know which helper to reach for.

**Tests:** table test in `internal/format` pinning at least: `Tokens`: `0→""`, `-1→""`, `999→"999"`, `1000→"1k"`, `1010→"1k"`, `1999→"2k"`, `18432→"18k"`, `32768→"32k"`, `65536→"64k"`, `131072→"128k"`, `128000→"125k"`, `1000000→"977k"`, `1048576→"1M"`, `1153434→"1.1M"`, `10485760→"10M"`, `2147483648→"2G"`; `TokensFine`: `0→""`, `4242→"4.1k"`, `32768→"32.0k"`, `1258291→"1.2M"`.

**Acceptance:** `go test ./internal/format/...`

**Commit:** `feat(format): add shared unit-capped token-count formatter`

## 2. TUI adopts the shared formatter

Depends on item 1.

**What:** In `internal/tui`, delete `formatTokens` (`model.go:4040`) and `formatTokensFine` (`model.go:4054`) and switch every call site to `format.Tokens` / `format.TokensFine`. Sites (complete inventory): the context gauge `model.go:3999`; startup box `model.go:3697`; rebind/heartbeat notes `model.go:2602,2608,2612`; `windowWord` (`model.go:2632` — keep the wrapper, delegate to `format.Tokens`); Budget warning `model.go:430-431` (fine form); `subAgentFill` `render.go:478`; pickers `picker.go:515` and `picker.go:932`. `formatBytes` stays untouched.

Update the pinned tests — only pins whose spelling actually changes under the new rule (most power-of-two pins like `"32k"`/`"16k"`/`"8k"` are unchanged; known changes: `45000` now `"44k"` in `heartbeat_test.go:563-564`, `4242/…` now `"~4.1k"` in `contextfiles_test.go:127`). Check each of: `model_test.go`, `heartbeat_test.go`, `transcript_test.go`, `picker_test.go`, `paint_test.go`, `render_test.go`, `paintcache_test.go`, `actuation_test.go`, `contextfiles_test.go`.

**Tests:** the updated existing suite is the test surface; formatter behavior itself is owned by item 1's table test — add no duplicate tables here.

**Acceptance:** `go test ./internal/tui/...`

**Commit:** `refactor(tui): use shared format.Tokens for context-size display`

## 3. Remove the headless `formatTokens` twin

Depends on item 1.

**What:** In `cmd/apogee/headless.go`, delete the duplicate `formatTokens` (`:475`) and its doc comment (`:467-473`) that justified byte-identical duplication — the shared `internal/format` package dissolves that rationale. Switch `headlessSubAgentLines` (`:442`) to `format.Tokens`. Update `headless_test.go`: repurpose the twin-sync table (`:655-667`) to pin the shared helper's spellings at the CLI seam under the new rule (`{1999,"2k"}` etc. — keep the table, it guards the user-visible CLI strings), and check the sub-agent line pins (`:476,522,550,573` — expected unchanged: `"12k/32k"`, `"4k/32k"`).

**Acceptance:** `go test ./cmd/apogee/...`

**Commit:** `refactor(headless): drop formatTokens twin for shared format package`

## 4. Docs sweep and issue closeout

Depends on items 2 and 3.

**What:** Single owning item for all cross-cutting doc edits:

- `layout.md`: add the display-unit rule (binary ÷1024; `k`/`M`/`G`; integer `k`; one decimal for `M`/`G` under 10; `""` = unknown; unit switches so the displayed value never reaches 1000) in the section that describes the status line / context gauge, and verify the pinned example spellings at `layout.md:53,682,867,1137,1139,1218` still match the rule (update any that do not).
- `CONTEXT.md:110` and `README.md:871`: same verification, update only if the example spellings changed.
- `ISSUES.md`: remove the line-5 context-size entry (this plan resolves it).

**Tests:** none (docs only).

**Acceptance:** `grep -n "1048k" layout.md CONTEXT.md README.md` returns nothing; `grep -n "context sizes should be displayed" ISSUES.md` returns nothing; `make check` passes.

**Commit:** `docs: record context-size display-unit rule; close ISSUES entry`

---

**Suggested version bump:** patch (`v0.12.3`) — user-visible display fix plus internal refactor, no new feature surface. Owner decides; no item in this plan touches VERSION or CHANGELOG release headings.
