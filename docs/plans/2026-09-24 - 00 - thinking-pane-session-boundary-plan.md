# Thinking and advice boards reset at every session boundary — plan

**Goal:** A new or switched session (`/clear`, `/new`, `/sessions` resume, `/fork`) opens with an empty `/thinking` board and an empty `/advice` board, like a launch does. The Inspector ring and the attempt ring keep surviving `/clear`, as documented.
**Date:** 2026-09-24
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** cf01dee3
**Closes:** apogee-thinking-pane-survives-clear

**Regression check (2026-09-24, cf01dee3):**
- 1: guard folded (writer-list comments name `resetSessionBoards`; `reset` assigns nil, never a `[:0]` reslice)
- 3: guard folded (Inspector clause dropped from the Goal; backtick-tolerant Acceptance grep; lifetime-sentence site grep in the Approach, naming `layout.md`'s `/thinking` and `/advice` popup sections)

**Sources:**
- `CONTEXT.md` — "Thinking pane" entry ("never persisted — a resumed session opens with an empty board"), "Inspector" entry
- `docs/manual/commands.md` — the `/thinking` and `/advice` rows
- `internal/tui/advicepane.go` header comment ("The scope is the WHOLE SESSION")
- `internal/tui/inspector.go` header comment ("The ring survives /clear …")
- bead `apogee-thinking-pane-survives-clear`

**Ratified design calls** (owner, 2026-09-24):
- **Advice board:** resets at every session boundary too, with the thinking board.
- **Open pane:** a `/thinking` or `/advice` pane open at the boundary stays open and shows its empty row; pane state (`reportPane`) is not touched.
- **Scope:** every session boundary: `/clear`, `/new` (both `startNewSession` branches), and `resumeLoaded` (`/sessions` resume and `/fork`).
- **Inspector and attempts:** `m.wire` and `m.attempts` are not reset (documented in `inspector.go`: a view of the run, not of the session).

**Standing requirements:**
- skills: coding-standards

**Out of scope:**
- Launch-time `--resume` / `--continue`: `newModel` builds a fresh Model, so its boards already start empty
- Turn-number collisions across sessions (`ClearContext` keeps the turn index)
- Unifying `resetSessionView` with `resumeLoaded`'s hand-copied reset beyond the one shared call below
- Closing open report panes at `/clear`

## 1. The thinking and advice boards reset at every session boundary

**What:**
**Goal:** After `/clear`, `/new` (bound and pre-bound) and a `resumeLoaded` restore, `m.thinking` holds no records and `m.advice` is empty. The thinking board keeps its wrap memo, and `m.wire`, `m.attempts` and the report-pane state are unchanged.
**Approach (assumed at the header base):** Fixes `apogee-thinking-pane-survives-clear`: `Model.resetSessionView` (`internal/tui/commandrun.go`) and the mirrored reset in `Model.resumeLoaded` (`internal/tui/sessions.go`) never clear `m.thinking` or `m.advice`. Add a `reset()` method to `thinkingBoard` (`internal/tui/thinking.go`) that empties `done` and `live` and keeps the `rows` memo pointer, because `newModel` installs it and a nil memo renders uncached. Add one Model method, `resetSessionBoards()`, that calls `m.thinking.reset()` and sets `m.advice = nil`. Both reset sites call it, so there is one owner of what a session boundary empties and one reason for it to change. Do not touch `m.wire`, `m.attempts`, `m.inspector`, `m.thinkingPane` or `m.advicePane`.

**Regression guard.**
- Every comment enumerating either board's writers names `resetSessionBoards`: `thinking.go:85-86`, `doc.go:506-509`, `advicepane.go:14` — find them with `grep -n -E 'only writer|written by|read by nothing else|worker boundaries' internal/tui/{thinking,advicepane,doc,model}.go`.
- `reset` assigns `nil` to `done` and `live`, never a `[:0]` reslice: a reslice lets the next `push` append into an array an earlier Model copy still shares (`thinking.go:151-153`, ADR 0011).

**Files:** internal/tui/thinking.go; internal/tui/commandrun.go; internal/tui/sessions.go; internal/tui/doc.go; internal/tui/advicepane.go; internal/tui/thinking_test.go; internal/tui/thinkingpane_test.go
**Read first:** internal/tui/commandrun.go — Model.startNewSession, Model.resetSessionView; internal/tui/sessions.go — Model.resumeLoaded; internal/tui/thinking.go — thinkingBoard, thinkingBoard.push;
internal/tui/contextfiles_test.go — clearSession, restoreSession; internal/tui/minilang_test.go — TestPreboundClearResetsTheViewWithoutTheEngine

**Tests:**
- `thinking_test.go`: `TestThinkingBoardResetKeepsTheRowMemo`. Fill `done` and `live`, call `reset`, then assert `b.done == nil && b.live == nil` and `rows` is the same pointer.
- `thinkingpane_test.go`: `TestSessionBoundaryEmptiesThinkingAndAdvice`. Table over `clearSession`, `restoreSession` (from `contextfiles_test.go`) and the pre-bound `/clear` branch (driven as `TestPreboundClearResetsTheViewWithoutTheEngine` does). Each case:
  - Fold a committed main-run reasoning record (`reasoningAt` + `MessageEvent`) and an `advise` `ReactionFiredEvent` (built as `TestAdviceBoardFoldsAdviseFiringsInOrder` does).
  - Cross the boundary, then submit `/thinking`.
  - Assert the painted `paneThinking` block holds `thinkingEmptyRow` and not the old thought. Do the same for `/advice` with `adviceEmptyRow`.
- In the same test, open `/thinking` before `clearSession`. Assert it stays open and paints `thinkingEmptyRow` after the boundary.
- In the same test, fold a `WireEvent` before `clearSession`. Assert `len(m.wire)` survives it.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'TestThinkingBoard|TestSessionBoundary|TestThinkingCommand|TestAdvice|TestClear|TestRestore|TestContextFilesNotice' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/` passes. Bite check: `TestSessionBoundaryEmptiesThinkingAndAdvice` fails on the base tree.

**Closes:** apogee-thinking-pane-survives-clear

**Commit:** `fix(tui): /clear, /new, resume and fork empty the thinking and advice boards`

## 2. An e2e run proves /clear empties the /thinking pane

**What:**
Test-only. Depends on item 1. In `cmd/apogee/e2e_thinking_test.go`, add `TestE2EClearEmptiesTheThinkingPane`, modelled on `TestE2EThinkingPaneShowsEitherWireSpelling`. It reuses the `thinking` stubllm script and the constants `thinkingPrompt`, `thinkingThought`, `thinkingReply`, `thinkingEmpty` and `thinkingPaneMarker`. The sequence:
1. `submit(thinkingPrompt)`, then wait for `thinkingReply`.
2. `submit("/clear")`, then `WaitGone(thinkingReply)`.
3. `submit("/thinking")`, then wait for `thinkingPaneMarker`.
4. Assert `thinkingEmpty` is on the frame and `thinkingThought` is not.
5. `closePane`.

The test calls `t.Parallel()` and waits on content, never on time (`docs/design/test-drivers.md`, "Writing a new e2e test").
**Files:** cmd/apogee/e2e_thinking_test.go
**Read first:** cmd/apogee/e2e_thinking_test.go — TestE2EThinkingPaneShowsEitherWireSpelling, thinkingPaneMarker, thinkingEmpty; cmd/apogee/e2e_usage_test.go — TestE2EClearResetsUsage;
cmd/apogee/e2e_smoke_test.go — closePane, submit; cmd/apogee/testdata/stubllm/thinking.yaml; internal/tui/commandrun.go — Model.queueCommand

**Tests:** `TestE2EClearEmptiesTheThinkingPane`

**Acceptance:**
- `go test -race -count=1 -run 'TestE2EClearEmptiesTheThinkingPane|TestE2EThinkingPane' ./cmd/apogee/`

**Commit:** `test(cmd/apogee): an e2e /clear leaves the /thinking pane empty`

## 3. Docs state the boards' session lifetime

**What:**
**Goal:** `CONTEXT.md` and `docs/manual/commands.md` say that the `/thinking` and `/advice` boards empty at every session boundary (`/clear`, `/new`, `/sessions` resume, `/fork`).
**Approach (assumed at the header base):** Extend the "Thinking pane" entry in `CONTEXT.md`: its "a resumed session opens with an empty board" becomes every session boundary. Add the same fact to the `/advice` wording in `CONTEXT.md`, and to the `/thinking` and `/advice` rows in `docs/manual/commands.md` ("nothing saved with the session" becomes "emptied at /clear, /new, /sessions and /fork"). Rule for finding every site: every doc sentence that states either board's lifetime or scope. `grep -n -i -E 'resumed session opens with|nothing saved with the session|never persisted' CONTEXT.md docs/manual/*.md layout.md`; the `layout.md` sites are its `/thinking` popup and `/advice` popup sections. Leave the Inspector wording as it is.

**Regression guard.**
- The site-finding grep is `grep -n -i -E 'resumed session opens with|nothing saved with the session|never persisted' CONTEXT.md docs/manual/*.md layout.md`; the `layout.md` sites are lines 1988 and 2053-2054, and `CONTEXT.md:451` and `commands.md:35` are sites too.
- The Acceptance grep tolerates the house backticks (`` `/clear`, `/new` ``): `` grep -n -E '/clear`?, `?/new' CONTEXT.md docs/manual/commands.md ``.

**Files:** CONTEXT.md; docs/manual/commands.md; layout.md
**Read first:** CONTEXT.md — Thinking pane entry, Inspector entry, Advice span entry; docs/manual/commands.md — /thinking row, /advice row, /inspect row;
layout.md — The `/thinking` popup section, The `/advice` popup section

**Tests:** none (docs).

**Acceptance:**
- `` grep -n -E '/clear`?, `?/new' CONTEXT.md docs/manual/commands.md `` shows the boundary wording at both the /thinking and /advice sites

**Commit:** `docs: the thinking and advice boards empty at every session boundary`
