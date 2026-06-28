# Handoff 02 (2026-06-24) — TUI layout redesign (presentation pass): plan ready, implementation next session

**Date:** 2026-06-24 · **Branch:** `main` (commit directly — pre-production owner directive) ·
**Type:** planning only — **no code changed**, docs only (the verify gate is untouched; nothing under
`internal/`/`cmd/` moved). **Status:** 📋 PLANNED, owner-approved. Next session **implements**.

## What this session was

The owner pointed at [`layout.md`](../../layout.md) (a target look-and-feel sketch) and asked to update
the TUI to **look and behave like it**, doing a **grouping / normalization sweep first** so the
rendering is modular and reusable, referencing the VS Code extension (`/workspace/repos/apogee-code/`)
for conventions already in place there. This session explored the current TUI + the reference extension
+ the domain data, made the load-bearing scope decisions with the owner, and produced the
implementation spec below. **No TUI code was written** — implementation is the next session.

Full approved plan (identical spec, kept as the scratch plan):
`/root/.claude/plans/workspace-repos-apogee-layout-md-please-vivid-kite.md`. This handoff is the
repo-of-record copy.

## The target (from `layout.md`)

- Last **user prompt**: white text on **dark-gray** background block, word-wrapped, full width, and it
  **sticks to the top** of the visible chat area while the assistant responds (apogee-code does this).
- Assistant response prefixed `✦`, with **one empty line** between the user prompt and the response.
- **Tool calls** as a header + tree-branch detail block:
  `✦ [Read File] <file>` → `  ┕ 0 - 100`; `✦ [Update File] <file>` → `  ┕ +2 -2` + diff lines;
  `✦ [Sub Agent] 3 Sub Agents` → `  ┝ Sub Agent 1: Name (summary)` … `  ┕ …`.
- **Status line** above the input: `⣻ turn 5    16k 50% ██████` (spinner, turn, context gauge).
- Exactly **one empty line** between chat content and the bottom chrome.
- **Input box**: rounded border (`╭─╮ … ╰─╯`), **black** bg, **dark-gray** border, multiline,
  **auto-grows** height, placeholder text.
- **Footer bar** below the input: left `host-alias ✦ <model.gguf> ✦ 32k`, right `<mode>`, drawn with a
  decorative top/bottom border mixing `━`/`─`.

## Decisions made with the owner (do not re-grill)

1. **Token gauge → static ctx now, live gauge later.** Wire the real **context-window size** (`32k`)
   from `provider.ModelInfo.ContextWindow` now; build the gauge component but **suppress the live
   `used% ██████`** until token usage is routed (Phase 4 — see fit analysis). No faked numbers.
2. **host-alias → a `host-alias` config key.** Add it to `~/.apogee/config.yaml` + `Options`; when unset,
   fall back to the endpoint URL's host.
3. **Diff display → render what we have.** `[Write File] <path>` + a byte/line summary (`write_file`
   replaces whole files, returns only a byte count — no diff data, no edit tool). Build the red/green
   `+/-` diff renderer but leave it **ready-and-unused**. The `[Update File]` diff in `layout.md` is
   **aspirational** until an edit/diff tool exists (would arrive with the P3.7 file-editing family).

## Grounding facts verified this session (so the next session needn't re-derive)

- **Current TUI** (`internal/tui/`): `model.go` (value-type `Model`, copied each `Update` per ADR 0011 —
  **no `strings.Builder` by value**, guarded by `TestModelNoBuilderByValue`; `textarea` fixed 3 rows,
  `viewport` SoftWrap, `spinner`, `statusLine()`/`hintLine()`, `layout()` vpH = `height − 3 − 1 − 1`,
  `refreshViewport()` = `SetContent` + `GotoBottom`). `transcript.go`: `[]entry{kind,text,depth}`,
  `apply(domain.Event)` fold, labels `you`/`apogee`/`tool`/`result`/`error`/`·`; **tool call and result
  are SEPARATE entries**. `tui.go`: `Options{Model, Endpoint, Mode, Bypass, Workspace, Save}`.
- **Tools** (`internal/tools/`): `read_file` (args `path`/`start_line`/`end_line`/`max_lines`; result
  header `[File: <p>, <N> lines total, showing lines <A>-<B>]` — `read_file.go:120`), `write_file` (args
  `path`/`content`; result `wrote <N> bytes to <p>` — `write_file.go:82`), `list_dir`, `grep`. Tools are
  an **open extension point** (ADR 0002); `ToolCall.ID` / `ToolResult.CallID` both exist (grouping key).
- **Events** carry `Turn` + `Depth`; `Depth>0` = sub-agent (infra only, not emitted until P3.13).
- **No token/ctx data routed to TUI yet**: `provider.Usage` exists post-response; `ModelInfo.ContextWindow`
  discovered but not in `Options`; `domain.Budget.Used` always 0 this phase.
- **Modes**: `plan` / `ask-before` / `auto` today; P3.4 adds `allow-edits` (4-rung ladder).
- **apogee-code** renders raw tool names (`Tool call: read_file`); the friendly labels (`Read File`,
  `Write File`, …) are a **new convention** defined here. It confirms the sticky-prompt + grouped
  tool-call conventions (webview `chat.ts` `updateStickyState`, `createToolCallEl`).

## Implementation spec (modular decomposition, all in `internal/tui`)

Keep one package (preserves the value-type Model + ADR-0010 import invariant). **3 new files, 3 edits.**

- **NEW `theme.go`** — central palette (`colWhite`/`colDarkGray`/`colBlack`/`colFaint`/`colDiffAdd`/
  `colDiffDel`), glyphs (`glyphAssistant="✦"`, `glyphBranch="┝"`, `glyphBranchLast="┕"`, `glyphUser="❯"`),
  braille spinner frames, and a `theme` struct of reusable `lipgloss.Style`s (`userBlock`, `assistant`,
  `toolHeader`, `toolDetail`, `diffAdded`, `diffRemoved`, `inputBorder`, `statusFaint`). Built once in
  `newModel`, stored as a Model value field (no Builder).
- **NEW `toolpresent.go`** — pure (no lipgloss), trivially testable.
  `toolView{Label,Target string; Details []detailLine}`, `detailLine{Kind detailKind; Text string}`
  (`detailPlain|detailDiffAdded|detailDiffRemoved`). `presentToolCall(call)` builds the header from args;
  `(*toolView).enrichWithResult(result)` parses the **fixed** result headers → detail lines (read→`A - B`,
  write→`+N bytes`, list→`N entries`, grep→`N matches`). **Make the name→label mapping an OPEN, name-keyed
  registry** (not a closed switch) so P3.7–P3.11 tools each add one entry. Unknown tool → fall back to
  today's pretty-printed JSON args (keep `prettyJSON`). Malformed args shown verbatim (approval is a
  security surface). The diff `detailKind`s are defined + rendered but **unused** for now.
- **NEW `render.go`** — line-oriented renderer (returns `[]string` so the caller can feed
  `viewport.SetContentLines` and compute wrapped offsets without re-splitting a joined string;
  tool results contain embedded newlines). `renderLines(th, width) []string` (✦ prefix, one blank line
  user→assistant, dark-gray user block); `lastUserLineIndex() int`; `renderToolBlock(th, tv, depth)
  []string` (header + `┝`/`┕` branch detail); `wrappedOffset(linesAbove, vpWidth) int` =
  `Σ max(1, ceil(displayWidth(line)/vpWidth))` — **mirrors the viewport's `calculateLine` exactly**;
  valid only while the viewport has no border/gutter (`maxWidth == Width`, currently true) — guard with
  a unit test.
- **EDIT `transcript.go`** — group call+result by `CallID`. Extend `entry` with `callID string`,
  `tool toolView`, `done bool`. `addToolCall` → store `presentToolCall(call)` + `call.ID`, append one
  entry. `addToolResult` → scan from the tail for the last un-`done` `entryToolCall` with matching
  `CallID`, `enrichWithResult`, set `done`; **orphan fallback** = standalone result entry. Keep `apply`,
  the `pending string` stream buffer, `commitAssistant`, `finalizeNarration` unchanged.
- **EDIT `model.go`** —
  - **Sticky-to-top**: `refreshViewport` = `SetContentLines(renderLines(...))` then, unless
    `userScrolled`, `SetYOffset(wrappedOffset(lines[:lastUserLineIndex()], width))`. Add `userScrolled
    bool` (set when a scroll key reaches the viewport; reset on `submit`). Replaces `GotoBottom()`.
  - **Auto-grow bordered input**: replace `inputHeight=3` with dynamic `inputRows()` clamped `[1,~10]`
    (measure `lipgloss.Height(m.input.View())` for wrap; textarea scrolls internally past the cap).
    `layout()` → `vpH = max(1, height − statusHeight − gapRows(1) − (inputRows()+borderFrame) −
    footerHeight)`. Re-run `layout()` after each idle keystroke so the box grows live. In `View`, give the
    textarea the inner width (black bg via theme) wrapped in `theme.inputBorder` (rounded, dark-gray).
    Keep the **single empty line** between transcript and chrome.
  - **Status line**: `⣻ turn N` left, `contextGauge()` right (`justify(width,…)`); braille spinner.
  - **Context gauge**: self-hiding `contextUsage{Used, Limit int}` — renders nothing in the status line
    when `Used==0`; footer shows the static window (`32k`). Live `16k 50% ██████` appears automatically
    when `Used` is wired (Phase 4). No UI rework.
  - **Footer bar**: left `host-alias ✦ <model> ✦ <ctx>`, right `<mode>`, between two manually composed
    rule strings mixing `━`/`─` + literal corner runes (a single `lipgloss.Border` rune can't vary per
    column). Guard `width<3`. **Renders `string(m.opts.Mode)`** → picks up `allow-edits` (P3.4) for free.
  - **Drop `hintLine`** (layout has no hint row); fold the approval legend into the approval prompt block;
    rely on the input placeholder for the idle hint.
  - **Approval prompt**: keep the **raw** tool name + reason + args (NOT the friendly label) — security
    surface; document why.
- **EDIT `tui.go` + `cmd/apogee/`** — `Options` += `ContextWindow int`, `HostAlias string`.
  `cmd/apogee/config.go`: add `HostAlias string \`yaml:"host-alias"\`` to `fileConfig`, `hostAlias *string`
  to `layer`, resolve in `applyConfig` (file value, else endpoint host). `cmd/apogee/wire.go`: populate
  both (`ContextWindow` from `provider.ModelInfo.ContextWindow`). `cmd/apogee/defaults/config.yaml`: add a
  commented `# host-alias: my-box`.

**Build order:** theme → toolpresent(+tests) → transcript grouping → render → sticky scroll →
auto-grow input → status/footer → Options/config wiring.

## Test impact

- **Rewrite** `TestTranscriptToolTurnGolden` to the new grouped block; split into (a) structured assert on
  `entry.tool` and (b) a small render snapshot.
- **Update** old-label substring tests (`transcript_test.go`) to `✦ [Read File] …` / `✦ <text>`.
- **Replace** `TestFormatToolCall` → `TestPresentToolCall` (4 tools + raw fallback + malformed args).
- **Update** `TestModelStatusLine` (model/mode/turn/ctx now in status+footer; **drop the full endpoint-URL
  assertion** — footer shows host-alias). **Update** `e2e_test.go`/`live_test.go`: `write_file` →
  `Write File`.
- **Keep green**: `TestModelApprovalPromptRender` (raw tool name), `TestModelNoBuilderByValue`,
  `TestModelResizeDoesNotPanic` (keep `max(1,…)` floors on tiny windows).
- **Add**: `TestWrappedOffsetMatchesViewport` (Σ offset == `viewport.TotalLineCount()` — drift guard),
  `TestToolResultGroupsByCallID`, `TestStickyPinsLastUserPrompt`, `TestInputAutoGrowReflowsViewport`.

## Verification (next session)

- `gofmt -l .` empty · `go vet ./...` · `go build ./...` · `go test -race ./internal/tui/... ./cmd/apogee/...`.
- **Manual:** `go run ./cmd/apogee` against the local endpoint (`http://192.168.64.1:1111`). Confirm:
  user prompt white-on-dark-gray, sticks to top while the reply streams; `✦`-prefixed assistant with one
  blank line after the prompt; a read/write/grep turn renders `✦ [Read File] <file>` + `┕ <range>`;
  rounded black input grows as you type multi-line; one blank line before the chrome; footer shows
  `host-alias ✦ <model> ✦ 32k` left, mode right.
- **Live E2E:** `APOGEE_LIVE_ENDPOINT=… go test ./internal/tui/ -run TestE2ELiveModel` (gated).
- Set/unset `host-alias:` in `~/.apogee/config.yaml`; confirm footer picks it up / falls back to host.

## Fit with the overall plan (Phase-3 roadmap)

**Not a scheduled Phase-3 task.** [`docs/plans/phase-3-detail-plan.md`](../plans/phase-3-detail-plan.md)
covers confinement, the tool fan-out, sub-agents, and MCP; only **P3.14** touches TUI visuals. This is a
**TUI presentation pass** polishing the Phase-2 shell. **Recommended framing: a standalone `P2.7`
(pre-P3 TUI presentation pass)** that lands **before the tool fan-out (P3.7) and before P3.14**, so those
tasks extend its seams rather than reworking them. Independent of the confinement critical path
(P3.1–P3.4); does not block / is not blocked by P3.0.

**It sets up four later Phase-3 tasks — design the seams now:**
- **P3.7–P3.11 (tool suite, ~30 tools, ADR 0002):** `toolpresent.go` must be an **open, name-keyed
  registry** of `label + detail-extractor`. Each later tool adds one entry (`terminal`→"Run",
  `git`→"Git", `find_replace`→"Edit File", …). Don't hard-code a closed switch.
- **P3.14 (sub-agent `Depth>0` rendering):** the `[Sub Agent]` + `┝`/`┕` block in `layout.md` **is the
  P3.14 deliverable**. No `Depth>0` events exist until P3.13, so build the **tree-branch + depth-indent
  primitives now** (used by tool detail) and treat the sub-agent block as a **reserved renderer** (same
  posture as the diff renderer). **Ordering:** this pass rewrites the `Depth==0` goldens to the new look;
  P3.14's "flat goldens unchanged" acceptance then builds on the *new* baseline — **doing this pass first
  avoids P3.14 rework.**
- **P3.11 (`ask-user` `Asker` delegate):** keep the bordered-prompt / input-box styling in `theme.go`
  **reusable** so the ask-user prompt (analogous to Approval) reuses it for free.
- **P3.4 (`ModeAllowEdits`):** the footer renders `string(mode)` → picks up the 4th rung automatically;
  just verify all four labels fit at narrow widths.

**Phase-4-gated data:** `phase-3-detail-plan.md` §6 defers **token counting to Phase 4**, so the live
`16k 50% ██████` gauge's data won't exist until then — *this is why* the owner chose "static ctx now,
live gauge later." Wire the ctx window (`32k`) now; the live bar lights up when Phase-4 routes `Used`.

**Invariants preserved:** ADR-0010 (`internal/tui` imports only down to `internal/domain`; new `Options`
fields are display-only — no root import) · ADR 0011 (Model stays value-type, no `strings.Builder` by
value). Only user-facing surface addition is the `host-alias` **config key** (config schema is not the
frozen Go API; the `v1.0.0` freeze at P3.16 covers the public Go surface — safe, no freeze review).

## Open question for next session

- **Where to record `P2.7`** — add it to `phase-2-detail-plan.md` (as a post-completion follow-up) or
  note it at the top of `phase-3-detail-plan.md` as a pre-P3 pass? (Recommend the latter: it's the doc
  the next builder opens.) Owner to confirm framing before/at implementation.
