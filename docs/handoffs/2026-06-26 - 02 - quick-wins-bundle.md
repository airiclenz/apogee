# Handoff — Quick-wins bundle (skill chips after send · @-file cache · live usage gauge + tok/s)

**Date:** 2026-06-26
**Status:** **SHIPPED** (implemented + tested on `main`, pre-production — committed direct once the
owner OKs the commit). Build/vet/gofmt clean; `go test ./...` and `go test -race ./...` green.
**Track:** post-`v1.0.0` **feature-parity / polish** — the *third* slice after the chat
mini-language core (`… - 00`) and the Skills system (`… - 01`). Belongs to **no** phase plan;
purely **additive** (one new domain Event variant, additively versioned). No freeze break.

## Why

Answering "is there anything left in the TODO we can implement?" the owner picked the **quick-wins
bundle** from the implementable backlog — three small, self-contained slices that each close a real
gap and together touch the TUI status surface, the @-file autocomplete, and the skills UX.

## What shipped (three slices)

1. **Used skills visible after a send** (ISSUES #5, resolved). `transcript.entry` gained a
   `skills []string` (display names); `addUser(text, skills)` carries them; `renderUserBlock` now
   appends a `renderUserChipRow` — a full-width row inside the user block composing a dark-gray
   lead marker + the violet skill chips (each keeping its own bg) + dark-gray pad (the
   `footerContent` three-segment idiom). Submit + `/continue` resolve the attached IDs to display
   names via the new `skillDisplayNames` (which **replaced** `userPromptLine`/`attachedSkillNames`
   — the old empty-text "(skill: …)" note is gone; an empty-text-with-skills send is now just the
   chip row, marker and all). Files: `internal/tui/{transcript,render,model}.go`.

2. **@-file walk cache** (TODO [P0] mini-language polish). New `internal/tui/filecache.go`: a
   `*fileCache` field on the Model memoises the workspace listing (`walkWorkspaceFiles`, the same
   `os.OpenRoot` fence as before, hidden/.git excluded, capped at `maxCachedFiles=4096`) with a
   `fileCacheTTL=3s` and filters it in memory (`filterFiles`) per keystroke — so a typing burst
   reuses one walk instead of re-scanning the disk on every character. The cache invalidates on
   root change or TTL lapse. `fileSuggestions` routes through it (nil-safe fallback to the uncached
   `workspaceFiles`, which moved here and is now a thin walk+filter wrapper the existing
   `TestWorkspaceFiles` still pins). The pointer is shared across the value-copied Model (ADR 0011),
   `now` is injected into `suggest` for deterministic expiry tests.

3. **Live usage gauge + tokens/sec** (TODO [P2] throughput). `stream_options.include_usage` already
   rode every request, so the server's token accounting arrived on the terminal `DeltaDone` and was
   simply dropped. Added **`domain.UsageEvent`** (`{PromptTokens, CompletionTokens, TotalTokens}`,
   re-exported in `apogee.go` for parity with the other variants), emitted from
   `agent/loop.go streamResponse` when `delta.Usage != nil`. The TUI folds the latest **Depth-0**
   usage (`model.go foldStats`, wired into the `eventMsg` case): `contextGauge` now reads
   `m.ctxUsed` (was a hard-coded 0 — the gauge was always dark), and the status line shows a rolling
   `· N tok/s` (`throughputSuffix`) while running, the completion timed against the Update clock from
   the Turn's first token (clock resets on `StreamResetEvent`; a sub-agent's Depth>0 usage is
   ignored so it never moves the top-level gauge).

## Tests

- `internal/tui/skill_test.go::TestSentUserBlockShowsSkillChipsWithText` (text + skill both visible
  after send); the existing empty-text send test already asserted the name shows and still passes.
- `internal/tui/filecache_test.go` — serves-then-expires (TTL), root-change invalidation, empty-root
  nil. `TestWorkspaceFiles` unchanged and green.
- `internal/tui/model_test.go::TestUsageEventDrivesGaugeAndThroughput` (gauge lights, ctxUsed,
  tok/s > 0, Depth>0 ignored) + `TestUsageThroughputClockResetsOnReStream`.
- ~12 existing `addUser(...)` test call sites updated to the two-arg signature.

## Out of scope / still open (unchanged)

- **Context *window* mis-read** (ISSUES) — the gauge surfaces usage against the window; the separate
  bug where the window itself reads wrong from the server (`provider/discovery.go`) is untouched.
- **Mid-string token completion** — kept deferred on purpose (cursor-position-free design).
- Remaining TODO feature-parity dominoes: **session-management UI**, **`/server` + model switching**,
  **`/compact` real reducer**, inspector/raw-protocol view, undo-all-changes. See `TODO.md`.
