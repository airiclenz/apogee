# Plan — Post-v1.0.0 review remediation (fix before the next stage)

**Date:** 2026-07-02
**Status:** READY — not started. Ordered action list from the 2026-07-02 code review of
`v1.0.0..HEAD` (the apogee-code feature-parity track: mini-language, skills, quick-wins
bundle, `/props` discovery, gauge restyle, mouse support, un-wedge fix, `/compact` reducer).
**Source:** six-specialist review (mission/security/bugs/concurrency/health/tests) + a docs
pass over all ADRs and design docs. Items marked *(×N)* were found independently by N
reviewers. Suite status at review time: `go test ./...` and `-race` green **except**
`TestSeatbeltProbe` (= item 1, a real bug, not flakiness).
**Track:** post-`v1.0.0` hardening. Everything here is a fix or test on shipped behaviour —
no freeze break. Items 6 and 7 each need one small design call (flagged inline).

Work the items in order; 1–4 are the "before moving on" core, 5–8 can trail.

---

## 1. Canonicalize ConfinementBox roots — Auto mode is broken on macOS (P0)

**What:** seatbelt denies writes *inside* the workspace whenever the workspace path
contains a symlink component. On macOS `/tmp` and `/var` are symlinks into `/private`, and
`sandbox-exec` matches kernel-canonical paths, so a box rooted at `/var/folders/...` never
matches. Verified by live repro: the identical profile fails with the symlinked root and
succeeds with the canonicalized one. This is the `v1.0.0` "Known post-release verification"
residual #4 (CHANGELOG), now confirmed as a real bug on real hardware.

**Where:** `internal/agent/dispatch.go:340` (`confinementBox()` passes `cfg.WorkspaceDir` /
`ConfineWritablePaths` verbatim) → `internal/platform/seatbelt.go:146` (`seatbeltProfile`
embeds them uncanonicalized). Path-safety already canonicalizes
(`security.EvalRealPath`, `internal/agent/disposition.go:180`); the box does not.

**Fix:** run `WorkspaceRoot` and every `WritablePaths` entry through
`security.EvalRealPath` — either once in `confinementBox()` (preferred: fixes every
backend) or per-backend in the profile/ruleset builders. Landlock is fd-based (open
follows symlinks) so Linux is likely unaffected, but the single canonicalization point
covers it regardless.

**Verify:** `go test ./internal/platform/...` on this Mac — `TestSeatbeltProbe`
`write_in_workspace_succeeds` / `write_in_writable_path_succeeds` go green (the deny rows
already pass). This also closes the macOS half of the owner-run enforcement proofs in the
`2026-06-25 - 00` handoff.

---

## 2. `/compact` + gauge truthfulness cluster (one pass over worker/model/compact)

Four small fixes; land together, they touch the same seam.

- **2a. Committed compaction reported as "cancelled"** *(×3)*. `startCompact`
  (`internal/tui/worker.go:38-44`) decides cancelled by re-reading `ctx.Err()` *after*
  `eng.Compact` returns; an Esc landing after `conv.Replace` committed yields
  `cancelledMsg` — transcript says cancelled, gauge stays stale, history was folded.
  **Fix:** classify from the returned error (`errors.Is(err, context.Canceled)`); nil error
  = committed ⇒ `compactDoneMsg{nil}` even if Esc landed late.
- **2b. `Result.Skipped` is discarded — false "context compacted"** *(×2)*.
  `Agent.Compact` drops the reducer's `Result` (`internal/agent/compact.go:31`), so a
  `minCompactTail` no-op still prints "context compacted" and resets the gauge to hidden
  (`internal/tui/model.go:249-254`). **Fix:** return the `Result` (or a skipped sentinel)
  through the Engine seam into `compactDoneMsg`; report "nothing to compact" and leave the
  gauge alone on skip.
- **2c. `/clear` leaves the gauge lit** *(×2)*. The `"clear"` case
  (`internal/tui/model.go:524-534`) never resets `m.ctxUsed`/`tokPerSec`, while
  `compactDoneMsg` (model.go:252) resets for exactly this reason. **Fix:** zero them on a
  successful `ClearContext`.
- **2d. Stale `genStart` corrupts the next turn's tok/s.** A cancelled stream emits no
  terminal `UsageEvent`, so `genStart` survives into the next exchange and the readout is
  timed from the dead turn (`internal/tui/model.go:85-87, 979-1008`). **Fix:** reset
  `m.genStart = time.Time{}` in `finishWorker` (model.go:582-587 — called on every
  terminal msg).

**Verify:** with item 3's tests; plus existing `model_test.go` gauge/throughput tests stay
green.

---

## 3. Tests for the `/compact` failure/cancel spine (write alongside item 2)

The whole fault side of compaction shipped untested — and it is precisely where 2a/2b live,
and `/compact` runs exactly when the upstream is likeliest to fault.

- `startCompact` Esc-cancel (`internal/tui/worker.go:36`): fake-engine `compactFn` blocks
  on ctx → cancel → assert `cancelledMsg` (not `compactDoneMsg`), fold it, assert state
  back to `stateIdle`, `ctxUsed` unchanged, "cancelled" note. And the 2a inverse: cancel
  landing after a nil-error return still reports compacted.
- `compactCompleter` fault branches (`internal/agent/compact.go:52`): (a)
  `DeltaContextOverflow` ⇒ error, `conv.Len()` unchanged; (b) ctx-cancel mid-summary ⇒
  `ctx.Err()` wins over the masqueraded `DeltaError`, conv untouched
  (`blockingResponder` in `harness_test.go:67` already fits); (c) the silence contract —
  no `TokenEvent`/`UsageEvent` in the sink during compaction.
- Compaction over a conversation with real tool-call turns (assistant `ToolCalls` +
  `RoleTool` results — the shape `/compact` exists to fold): no dangling tool messages
  after `Replace`; then `Snapshot` → `Resume` → Submit+Step to completion.
- The un-wedge regression, error flavour: `errMsg` mid-Exchange currently does **not**
  `AbortExchange` the way `cancelledMsg` does (`internal/tui/model.go:238`) — pin the
  recovery (or make `errMsg` abort too; that's the likely fix). Today `Step` never returns
  a mid-Exchange error, so this is a latent re-wedge, not a live one.

---

## 4. Docs/CHANGELOG reconciliation (must precede the next tag)

- **CHANGELOG [Unreleased] contradicts itself:** lines ~97 and ~110 still call `/compact`
  "**stubbed**" and advertise `Agent.Compact` "returns the new
  `ErrCompactionNotImplemented` sentinel" — a symbol that no longer exists anywhere —
  while lines ~19-33 of the same block describe the shipped reducer and the sentinel's
  removal. Rewrite the mini-language and Public-API entries to the as-shipped state.
- **Three stale "gauge is reserved until Phase 4" comments** describe the gauge this track
  shipped: `internal/tui/doc.go:29`, `internal/tui/tui.go:80` (`Options.ContextWindow`),
  `internal/tui/model.go:1051` (`statusRight`). Rewrite to the UsageEvent→gauge wiring;
  same pass: `tui/doc.go` doesn't mention `mouse.go`/`filecache.go`/`markdown.go` though it
  narrates every other file.
- **ADR 0011 realisation note:** the ADR says "the Update goroutine never touches the
  Agent", but the track added three deliberate, safe Update-goroutine calls
  (`AbortExchange` on `cancelledMsg`, `ClearContext` in `/clear`, mutex-guarded `SetMode`
  on Shift+Tab). Record the "idle-only synchronous calls + mutex-guarded mode" exceptions
  as a realisation section (the ADR-0007 pattern), so the next interactive consumer copies
  the real contract.

---

## 5. Mouse + paste input correctness

- **5a. Cell-vs-rune confusion in click/drag** *(bug + missing-test, ×2)*.
  `caretTo` (`internal/tui/mouse.go:94`, with `pointInputRow` :79) feeds a display-cell
  `visCol` into bubbles' rune-indexed `SetCursorColumn`, clamped by `CharWidth` (cells)
  instead of rune count. With CJK/emoji the caret lands wrong and a drag-copy puts
  **different text on the clipboard (OSC52) than was highlighted**. **Fix:** convert cell
  column → rune offset by scanning the row's runes accumulating `runewidth`; clamp with
  the rune count. **Tests:** click/drag over `"日本語 text"` and over a soft-wrapped line,
  asserting caret rune offset and copied string (all current mouse tests are single-width
  ASCII on unwrapped lines).
- **5b. `tea.PasteMsg` bypasses the edit path.** Bracketed paste (default-on in
  bubbletea v2) falls into `Update`'s `default:` (`internal/tui/model.go:292-297`): the
  textarea inserts, but no `layout()`, no autocomplete recompute, no `m.sel` clear — a
  multi-line paste renders wrong until the next keypress, and a live selection's cached
  offsets go stale (wrong copy). **Fix:** add a `PasteMsg` case mirroring the KeyPress
  edit path.

---

## 6. `/compact` must survive high context fill *(needs one design call)*

The reducer sends the **entire** rendered transcript as one request with
`compactMaxTokens=4096` and no fallback (`internal/context/compact.go:56`,
`internal/agent/compact.go:44`). Near `n_ctx − 4096` the summary call itself overflows
(`DeltaContextOverflow`) — compaction deterministically fails at exactly the fill level it
exists to relieve, leaving `/clear` as the only recovery.

**Design call:** bound the rendered transcript to a budget derived from the discovered
context window (keep the protected prefix + a budgeted tail of the rendering, dropping the
middle with an elision marker), **or** summarize in chunks. The tail-budget option is the
small change and is probably enough for v1's on-demand `/compact`; decide before Phase 4
makes compaction automatic (the trigger will fire *at* high fill by definition).
**Verify:** item 3's overflow test flips from "errors cleanly" to "succeeds via fallback".

---

## 7. Sub-agent must see a live mode tightening *(needs one design call)*

`newChildAgent` freezes the parent's mode at spawn (`internal/agent/subagent.go:108`) and
the child runs its whole Exchange on it (`sub.Run`, subagent.go:80) — many Turns on a small
local model. A mid-delegation Shift+Tab from Auto down to Plan flips the footer, and the
TUI claims it "takes effect on the next tool call" (`internal/tui/model.go:363-365`), but
the child keeps auto-approving writes until its Exchange ends. This fails ADR 0005 in the
**tighten** direction (child running Auto while the parent is now Plan).

**Design call:** thread a live-mode view into the child tighten-only — share the
`modeMu`-guarded field or inject `liveMode func() domain.Mode`, and have the child's
disposition use `min(parentLive, spawnMode)` (loosening mid-flight stays impossible).
**Verify:** ADR-0013-style test — spawn a child in Auto, `SetMode(Plan)` mid-run, assert
the child's next write gates/refuses. Amend ADR 0013 with a realisation note.

---

## 8. Cleanup batch (each small; batch in one or two commits)

- **Lost cancels:** `finishWorker` nils `m.cancel` without calling it — one leaked child
  context per completed exchange for the session (`internal/tui/model.go:582`). Call
  before clearing.
- **Bounded reads of untrusted files:** skills discovery reads `SKILL.md` unbounded at
  startup (`internal/skills/load.go:99` — hostile-repo OOM: `.apogee/skills` is always
  scanned, `skills/` by default); the `@file` 10 MB cap is checked *after* the full read
  (`internal/agent/loop.go:462`). Both should stat-or-LimitReader **before**
  materializing, mirroring `internal/tools/read_file.go:80`; cap skill count/size.
- **Escape-strip hardening:** model text/skill names reach the terminal unsanitized.
  Not exploitable today (the bubbletea cellbuf drops non-SGR escapes when printable cells
  follow, and the app's footer always renders after transcript content — verified
  empirically), but trailing-position escapes DO survive the cellbuf, so this is one
  layout refactor from OSC52 clipboard injection. Strip ESC from untrusted text at the
  transcript boundary (`internal/tui/transcript.go` apply) and pin with a test feeding
  `\x1b]52;...`/CSI payloads through TokenEvent/MessageEvent and a skill DisplayName.
- **Quit-while-busy teardown race (latent):** `quit()` returns `tea.Quit` without joining
  the in-flight worker; `runRoot`'s deferred `mcpClient.Close()`/`agent.Close()` then run
  concurrently with a worker still inside `Step` (`internal/tui/model.go:595`,
  `cmd/apogee/wire.go:124,133`). Benign while `Close` is a no-op; a use-after-close the
  moment it gains its promised teardown. Defer `tea.Quit` until the worker's terminal msg
  arrives (the state machine already delivers exactly one).
- **Dead code / drift:** delete `fitLeftRight` (`internal/tui/render.go:339`, zero
  callers) — or better, resurrect it as the one style-aware justify helper that
  `statusLine` / `footerContent` / `renderUserChipRow` (three *diverged* hand-rolled
  copies) all call; delete `workspaceFiles` + the unreachable `m.files == nil` fallback
  (`internal/tui/filecache.go:56`, `autocomplete.go:201` — `newModel` always sets the
  cache); drop `Engine.Mode()` (zero callers) or make it the single source of truth the
  footer reads instead of the Shift+Tab-mutated `m.opts.Mode` shadow copy
  (`internal/tui/tui.go:53`, `model.go:367`); merge the duplicated skill-chip
  render/ID-resolution (`autocomplete.go:361-377` vs `render.go:151-163` vs
  `model.go:482-497`); move the ~500 lines of chrome rendering out of `model.go`
  (1,199 lines) into `render.go`/`chrome.go` so model.go is the state machine again.
- **Test gaps (non-compact):** the loop's `UsageEvent` emission hop
  (`internal/agent/loop.go:308` — Delta.Usage → event fields/Depth, and *no* event when
  Usage is nil; only provider parsing and TUI folding are covered today); the combined
  skills→files→text injection order in one Submit + the `@file` oversize refusal
  (`loop.go:466,170`).

---

## Explicitly NOT in this plan

- Everything already parked in `TODO.md` (tool×mode matrix, url-safety config key,
  `/server` + session UI + inspector, transcript drag-select, auto-sizing prompt box,
  automatic compaction trigger — item 6 only makes on-demand `/compact` survivable).
- The remaining owner-run enforcement proofs (Linux landlock live run, live Auto-confined
  deliverable run) — item 1 closes the macOS arm; the Linux arms still need a
  landlock-enabled host/CI runner (`2026-06-25 - 00` handoff §1).
- The uncertain single-source finding on same-layer skill-id collisions
  (`internal/skills/catalog.go:28` — two nested dirs deriving the same `path.Base` id
  silently overwrite with no soft error): unverified; check it when touching the skills
  loader, not before.

## Review artifacts

The full review report (executive summary, per-specialist findings, what looked good) was
delivered in-session on 2026-07-02; this plan is its actionable half. The one accepted-risk
adjudication worth restating: terminal-escape injection was tested empirically and is not
currently exploitable — the hardening entry in item 8 is belt-and-braces, not a live hole.
