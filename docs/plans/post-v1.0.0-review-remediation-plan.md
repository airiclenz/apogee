# Plan — Post-v1.0.0 review remediation (fix before the next stage)

**Date:** 2026-07-02
**Status:** IN PROGRESS — items 1–6 done (item 1 commit `8dc295e`; item 2 code + item 3 tests
+ item 4 docs + item 5 mouse/paste + item 6 compact-budget 2026-07-02); items 7–8 open. Ordered
action list from the 2026-07-02 code review of
`v1.0.0..HEAD` (the apogee-code feature-parity track: mini-language, skills, quick-wins
bundle, `/props` discovery, gauge restyle, mouse support, un-wedge fix, `/compact` reducer).
**Source:** six-specialist review (mission/security/bugs/concurrency/health/tests) + a docs
pass over all ADRs and design docs. Items marked *(×N)* were found independently by N
reviewers. Suite status at review time: `go test ./...` and `-race` green **except**
`TestSeatbeltProbe` (= item 1, a real bug, not flakiness) — now green after item 1.
**Track:** post-`v1.0.0` hardening. Everything here is a fix or test on shipped behaviour —
no freeze break. Items 6 and 7 each need one small design call (flagged inline).

Work the items in order; 1–4 are the "before moving on" core, 5–8 can trail.

---

## 1. Canonicalize ConfinementBox roots — Auto mode is broken on macOS (P0) — ✅ DONE (`8dc295e`, 2026-07-02)

**Done:** fixed in the seatbelt backend, not `confinementBox()`. Two deliberate departures from
the suggested fix below, decided after reading the code:
- **Backend, not `confinementBox()`.** The "preferred" central spot cannot satisfy the
  acceptance test — `TestSeatbeltProbe` builds its own box and drives the backend directly, so
  `confinementBox()` canonicalization would never turn it green, and the backend would stay
  fragile for the bench / third-party embedders that construct boxes directly. The need to
  canonicalize is a property of *how `sandbox-exec` matches* (kernel-canonical), so its correct
  home is `seatbeltProfile` (`internal/platform/seatbelt.go`, new `seatbeltCanonicalRoot`).
- **Stdlib `filepath.EvalSymlinks`, not `security.EvalRealPath`.** `internal/platform` is
  pristine (imports only `internal/domain`); pulling in `internal/security` would invert the
  layer direction. For existing dirs (box roots always exist at confine time) the two resolve
  identically, so agreement with path-safety holds.
- **Landlock confirmed unaffected** — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel
  resolves symlinks to the inode the rule keys on. Left untouched.
- **Coverage:** live `TestSeatbeltProbe` in-box rows now pass on macOS hardware, plus a new
  hermetic `TestSeatbeltProfileCanonicalizesSymlinkedRoot` (constructs a real symlink) that runs
  on every non-Windows host — including Linux CI, where the live probe self-skips.
- Closes the `v1.0.0` "Box-root canonicalization" residual and the macOS arm of the seatbelt
  live-enforcement proof (CHANGELOG updated). The **Linux landlock** live run and the **live
  Auto-confined deliverable** run remain open (out of scope — need a landlock host/CI).

---

<details>
<summary>Original plan text (superseded by the Done note above)</summary>

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

</details>

---

## 2. `/compact` + gauge truthfulness cluster (one pass over worker/model/compact) — ✅ CODE DONE (2026-07-02)

**Done:** all four fixes landed as described below. `Agent.Compact` now returns
`(skipped bool, err error)` — the reducer's `Result.Skipped` threaded through the `Engine`
seam (a `bool`, keeping the TUI's "public types via `internal/domain`" contract; no new
`internal/context` import) into a `compactDoneMsg{Skipped, Err}`. 2a reclassifies from the
returned error, not `ctx.Err()`; 2c zeroes `ctxUsed`/`tokPerSec` on a successful
`ClearContext`; 2d clears `genStart` in `finishWorker` (every terminal path). Existing gauge/
throughput and compact tests updated to the new signature and stay green (`go test ./...`,
`-race` clean). The dedicated fault/cancel spine tests are **item 3** (still open). CHANGELOG
`[Unreleased] → Fixes` records the cluster.

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

## 3. Tests for the `/compact` failure/cancel spine (write alongside item 2) — ✅ DONE (2026-07-02)

**Done:** the fault side of compaction is now covered at both seams; `go test ./... -race` on
the touched packages is green.
- **Agent reducer faults** (`internal/agent/compact_test.go`, new): `DeltaContextOverflow` ⇒
  error + `skipped=false` + conv untouched (the deterministic high-fill failure item 6 will make
  survivable); ctx-cancel mid-summary ⇒ `ctx.Err()` (`context.Canceled`) wins over the
  masqueraded terminal `DeltaError`, conv untouched (via `blockingResponder`); the silence
  contract — after a real exchange's events are dropped, `Compact` emits no `TokenEvent`/
  `UsageEvent`; and a fold over a real tool-call conversation (assistant `ToolCalls` + `RoleTool`
  results) leaves **no dangling tool message** (clean prefix→summary), the summarizer still saw
  the inlined tool work, and the folded Agent `Snapshot`→`Resume`→`Submit`→`Step`s to completion.
- **`startCompact` outcomes** (`internal/tui/worker_test.go`): Esc-cancel ⇒ `cancelledMsg` (not
  `compactDoneMsg`); the 2a inverse — a late cancel after a nil-error return still reports
  `compactDoneMsg` (classified from the returned error, not a fresh `ctx.Err()` read).
- **TUI folding** (`internal/tui/minilang_test.go`): a `Skipped` compact ⇒ "nothing to compact"
  with the gauge left lit (2b); a cancelled `/compact` leaves `ctxUsed` unchanged, calls
  `AbortExchange`, and records the "cancelled" note.
- **The un-wedge, error flavour** — resolved by the *fix*, not just a pin. `errMsg`
  (`internal/tui/model.go`) now calls `AbortExchange` before going to `stateErrored`, mirroring
  `cancelledMsg`, so a (latent) mid-Exchange `Step` fault can no longer wedge the engine at
  `ErrInputPending`. Pinned by a new `TestModelSeamMessageTransitions` sub-test; CHANGELOG
  `[Unreleased] → Fixes` records it. (This is the fourth deliberate Update-goroutine engine call
  — feeds item 4's ADR-0011 realisation note.)

<details>
<summary>Original plan text (superseded by the Done note above)</summary>

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

</details>

---

## 4. Docs/CHANGELOG reconciliation (must precede the next tag) — ✅ DONE (2026-07-02)

**Done:** all three reconciliations landed (docs/markdown only — `go build ./...`, `go vet
./internal/tui/`, and `gofmt` all clean).
- **CHANGELOG.** Rewrote the two self-contradicting `[Unreleased]` entries to the as-shipped
  state: the mini-language `/compact` line no longer calls it "**stubbed**" (it now points at the
  "Context compaction (`/compact`)" section describing the shipped reducer), and the Public-API
  entry now reads `Agent.Compact(context.Context) (skipped bool, err error)` with the skip
  semantics, dropping the removed `ErrCompactionNotImplemented` sentinel. The two remaining
  sentinel mentions (lines ~19/33) are the *correct* ones — they document the stub's replacement
  and the sentinel's removal.
- **Stale gauge comments.** The three "gauge is reserved until Phase 4 routes usage" comments
  (`internal/tui/doc.go`, `tui.go` `Options.ContextWindow`, `model.go` `statusRight`) now describe
  the live `UsageEvent → ctxUsed → contextGauge → statusRight` wiring against the discovered
  window. Same pass: `doc.go` now narrates `markdown.go`, `filecache.go`, and `mouse.go` (it
  already narrated every other file).
- **ADR 0011 realisation note.** Added a "Post-v1 realisation (apogee-code track)" section
  recording the four deliberate `Update`-goroutine engine calls as the two documented exceptions
  to C1's "the `Update` goroutine never touches the Agent": idle-only synchronous seams
  (`ClearContext`/`Compact`/`AbortExchange`/`Snapshot`, each state-machine-guarded to a quiescent
  boundary) and the one mid-`Step`-safe `modeMu`-guarded `SetMode` — the exact contract the next
  interactive consumer copies (ADR-0007 realisation pattern).

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

## 5. Mouse + paste input correctness — ✅ DONE (2026-07-02)

**Done:** both fixes landed as described below (`go test ./internal/tui/ -race` green; `go vet`
and `gofmt` clean).
- **5a.** `caretTo` (`internal/tui/mouse.go`) no longer feeds the display-cell `visCol` into
  the rune-indexed `SetCursorColumn`. It reconstructs the landed visual sub-line (new
  `visualSubline` — the `[StartColumn, StartColumn+Width)` rune slice of the logical line, so a
  click near a wrap never reads into the next row) and maps the cell column to a rune offset via
  new `cellToRuneOffset`, which walks the sub-line accumulating `runewidth.RuneWidth` — the same
  width source the textarea's own cursor math uses (`textarea.go:705`), so the mapping inverts the
  widget's rendering. A column inside a wide rune resolves to its left edge; a column past the end
  clamps to the **rune** count. (Note: `LineInfo.Width` is the sub-line's rune count and
  `CharWidth` its display width — the field doc comments read inverted vs. the code, which set the
  old clamp up to fail.) go-runewidth was promoted from an indirect to a direct dependency.
- **5b.** A new `case tea.PasteMsg` in `Update` (`internal/tui/model.go`) mirrors handleKey's
  idle/ask edit path: clear the selection, `input.Update`, recompute autocomplete (idle), and
  `layout()`. A paste outside the editable states is dropped, as keystrokes are.
- **Tests** (`internal/tui/mouse_test.go`): `cellToRuneOffset` table + a width-inversion invariant
  (every rune boundary round-trips, any script); `visualSubline` bounds/clamps; end-to-end
  `日本語 text` click and drag (the clipboard-vs-highlight regression, `copied 3 chars`); a
  self-calibrating soft-wrap drag on the second visual row; and three paste cases (multi-line
  insert grows the box, `/comp` re-opens the command overlay, paste-while-running dropped).
- CHANGELOG `[Unreleased] → Fixes` records the cluster.

<details>
<summary>Original plan text (superseded by the Done note above)</summary>

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

</details>

---

## 6. `/compact` must survive high context fill — ✅ DONE (2026-07-02)

**Design call made: the tail-budget option** (the plan's recommended small change), implemented
proactively — no chunked summarization, no reactive retry.

**Done:**
- **`internal/context.Compact` gained a `maxTranscriptChars` budget.** The rendered transcript is
  bounded by `renderBudgetedTranscript`: the protected prefix stays verbatim, the most recent
  messages are kept backwards until the next would exceed the budget (the latest is *always* kept —
  the next turn depends on it), and the dropped middle is marked with a
  `[... N earlier message(s) omitted to fit the compaction budget ...]` notice so the summarizer
  treats prefix and tail as non-contiguous. A non-positive budget renders the whole conversation
  (renderTranscript), so the window-unknown path is byte-for-byte the old behaviour. `renderMessage`
  was factored out so the full and budgeted paths share one rendering and one length measure.
- **`Agent.compactTranscriptChars` computes the budget** from the discovered window:
  `(MaxContextTokens − compactMaxTokens − compactPromptOverheadTokens) × CharsPerToken`, floored at
  `compactMinTranscriptTokens`, and **0 (unbounded) when the window is unknown** — there is no safe
  basis to bound against an unknown window, so the pre-item-6 full render stands (documented, not a
  guess). The response reserve stays `compactMaxTokens`; overhead (512 tok) covers the system
  prompt, trailer, role headers, and chars→token slack.
- **The window is now threaded into the Agent.** `cmd/apogee/wire.go` sets
  `cfg.Context.MaxContextTokens = opts.contextWindow` (the runtime `/props` window) — previously the
  discovered window reached only the TUI footer/gauge, never the agent, so *every* compaction ran
  unbudgeted. Safe: no live Mechanism reads `Budget().ContextLimit`, and the budget is a hook-view
  only (never on the wire), so the loop is unaffected.
- **Verify — done.** Item 3's overflow test flipped: `TestCompactSurvivesHighFillViaTranscriptBudget`
  (new `windowResponder` that overflows iff the prompt exceeds the window) proves a large
  conversation now folds because the request is budgeted under the window and is smaller than the
  raw transcript. The unbudgetable case is retained as
  `TestCompactUnbudgetableOverflowErrorsAndLeavesConvUntouched` (unconditional overflow ⇒ clean error,
  conv untouched). Plus reducer-level unit tests: unbounded == full render, a fitting transcript is
  untouched (no notice), the middle elides keeping prefix+tail, the most recent message survives a
  tiny budget, and the budget threads through `Compact`. `go test ./... -race` green; `go vet` and
  `gofmt` clean. CHANGELOG `[Unreleased] → Fixes` and `context/doc.go` updated.

Left for the automatic-compaction trigger (parked in `TODO.md`): the trigger fires *at* high fill by
definition, so it will lean on this budget; a reactive retry-on-overflow backstop (shrink and retry
if the chars→token estimate still overflows) can be added there if the proactive estimate proves too
loose in practice.

<details>
<summary>Original plan text (superseded by the Done note above)</summary>

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

</details>

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
