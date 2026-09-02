# Human-readable /inspect wire traffic

**Goal:** /inspect reads as what happened on the wire — thinking and reply text as wrapped prose, tool calls named, the request envelope summarised — with today's pretty-JSON rendering one key away, and the pane scoped to the sub-agent whose run view is open. Origin: `IDEAS.md` line 12 ("/inspect raw wire trafic should be human readable").

**Date:** 2026-09-01 · **Status:** unexecuted · **Sized for:** ~200k-context host · **Base:** 2fc5dda9

**Sources:**
- `internal/tui/inspector.go` — ring, `foldWire`, `wirePayloadLines`, `inspectorRows`, `hasUnrecordedReply` (authoritative for today's rendering)
- `internal/tui/reportpane.go` — the report module: `reportPane`, `reportKey`, `dismissReport`, the non-modal doctrine (header comment)
- `internal/provider/wire.go:184-215` — capture contract: response = raw `data:` payloads newline-joined, `[DONE]` included; non-streaming success body NOT recorded
- `internal/provider/stream.go:383-398` — `sseChunk`: the delta shape the readable classifier reads (`content`, `reasoning_content`, `tool_calls`)
- `internal/tui/runview.go:36,92` — `viewedRun()` / `runLabel()`; `internal/tui/transcript.go:164-172` — `runRef{depth, spawn}` = `(EventBase.Depth, EventBase.CallID)`
- `internal/agent/subagent.go:463` — only a ROUTED spawn binds its own wire tap; an unrouted child's records carry the parent's identity
- ADRs 0011 (value Model), 0031 (WireEvents are data), 0035 (`ui.inspector` read at start-up), 0039 (Depth+CallID identity), 0045 (routed spawn owns its tap), 0063 (run view)
- `docs/design/test-drivers.md` — the TUI-driven test idiom items 2-3 use

**Ratified design calls** (owner, 2026-09-01):
- **Rendering default:** readable by default; one pane key flips to today's pretty-JSON rendering. Per-pane, in-memory — nothing persisted.
- **Toggle key:** `ctrl+r` — non-printable, so the report doctrine (a printable key reaches the prompt) stays intact. Never a plain letter.
- **Storage:** both renderings are computed ONCE in `foldWire` from the raw payload and stored on the record (two line slices, two hidden counts); the pane's mode picks one per frame. No parsing on the paint path.
- **Delta rows:** consecutive deltas of one kind are merged into one passage and hard-wrapped at column 96 at fold time; the first row carries the kind prefix, continuation rows are indented two spaces. Never one row per delta.
- **Line cap:** `maxWireRecordLines` (100) applies to EACH rendering separately with its own hidden count; the ring bound (20 records) is untouched.
- **Sub-agent scoping:** with a run view open, /inspect shows only that run's records (depth + call id), labelled in the header; no run view → global. No manual filter key.
- **Scoped empty:** with capture ON, a viewed run owning no records shows one row worded for every cause it covers (`inspectorScopedEmptyRow`, item 3: "no records for this run — not called yet, rotated out of the ring, or an unrouted delegation speaking over its parent's connection; close the view for the whole ring") — never the global "armed" row and never a fallback to the whole ring; with capture OFF the scoped-empty slot keeps `inspectorDisarmedRow`, because capture off is the cause (refined at the regression check, 2026-09-01).
- **Thinking display:** from captured wire payloads inside /inspect only; the retained reasoning tail stays render-nowhere (`TestReasoningTailIsRenderedNowhere` stands).
- **Tool-call identity:** `tool_calls[i].function.name` + enclosing `id` truncated to 12 chars; arguments elided in readable, never expanded beyond raw's pretty form.

**Regression check (2026-09-01, 2fc5dda9):**
- 1: guard folded — the flat-rows header comment (inspector.go:42-45) and the wireRecord doc (:47-49) are amended in-item; the sseChunk mirror keeps its (corrected) reason and the none-of-`messages`/`tools`/`model` request body falls back to `wirePayloadLines` (writer's decision).
- 2: recast — `inspectContent` becomes the Model method `m.inspectContent()` routing both callers; the two existing tests the readable default turns red are amended in-item; the "whole keyboard"/"five keys" code comments are item 2's to amend.
- 3: recast — yields to inspector.go:24-27 and layout.md:1715-1719 for the capture-off case (the scoped-empty slot keeps `inspectorDisarmedRow`); the armed scoped-empty row is reworded for every cause it covers and the title is composed inside `m.inspectContent()` (no signature change).
- 4: guard folded — CONTEXT.md gains an Inspector term (it has no Inspector section to update); the "stays as written" claim is narrowed to the printable-key rule and the sweep extends to the code comments.
- 2 (re-check round, 2fc5dda9): Tests amended — `internal/tuitest` has no CtrlR constant, so the in-package test sends `tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}` (its String() is "ctrl+r") (writer's decision settling the reviewer's note); no other re-checked item changed.

**Standing requirements:**
- skills: coding-standards
- No VERSION or CHANGELOG-heading changes by any item; the changelog entry travels the plan's sidecar at closeout.

**Out of scope:** the ring bound (20 records); capturing the non-streaming success body; rendering thinking outside /inspect; any manual scope-filter key; wrapping at the live pane width (rows stay flat, elided at the border as today); doc rewrites beyond item 4's four files.

## 1. A readable rendering of the wire, stored beside the pretty-JSON one — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's `wireReadableLines` splits into three named helpers rather than one
function — `wireRequestSummary`, `wireResponsePassages` and `toolCallLabel` beside `wrapReadable`
and its `cutReadable` — with `wireReadableLines` the sole entry point the fold calls; behaviour and
signature are exactly as the item specifies.
NOTES (2026-09-02): "carries none of the three" is decided by key PRESENCE (pointer fields), so
`{"messages":[]}` summarises as "0 messages" while `{"a":1}` falls back, and a response line counts
as a delta chunk when it decodes with at least one `choices` entry — which is what lets an empty
delta contribute nothing without being re-read as an unclassifiable line.
NOTES (2026-09-02): inspector.go is now ~450 lines, past the ~400-line guidance in the
coding-standards skill; the item pins the file, and items 2-4 add to it, so no split was made.

**What:** In `internal/tui/inspector.go`, `wireRecord` gains `readable []string` and `readableHidden int` beside `lines`/`hidden`. `foldWire` fills both from the escape-stripped payload in one pass: `wirePayloadLines` (unchanged) and a new `wireReadableLines(direction, payload string) (lines []string, hidden int)`, each capped at `maxWireRecordLines` with its own hidden count. Nothing parses JSON after the fold.

`wireReadableLines` for a REQUEST decodes the body's top-level `messages` (len), `tools` (len) and `model` and emits ONE line: `<n> messages · <m> tools · model <model>`, omitting a missing field; an undecodable body falls back to `wirePayloadLines`. For a RESPONSE it walks the newline-split payload, decoding each line into the `sseChunk` shape (a local unexported struct in `inspector.go` mirroring `internal/provider/stream.go:383` — `choices[0].delta.{content, reasoning_content, tool_calls[].{id, function.name}}`; `sseChunk` is unexported in `internal/provider/stream.go:383`, so it is mirrored rather than imported), and produces passages:

- `reasoning_content` deltas → kind `thinking`; `content` deltas → kind `text`; consecutive deltas of one kind are concatenated into one passage; an empty delta string contributes nothing and does not break the run;
- a delta carrying `tool_calls[i].function.name` → its own passage `tool call <name> <id[:12]>` (arguments elided; a fragment without a name is skipped);
- `[DONE]`, blank and non-JSON lines → verbatim, each its own passage, closing any open run;
- a JSON line carrying none of the above (a usage-only chunk, an `error` member) → `prettyWireLine` output verbatim.

Each passage becomes rows: `· thinking <text>` / `· <text>` / `· tool call …`, hard-wrapped at column 96 by `wrapReadable(prefix, text string) []string` (rune-aware split on spaces where possible, hard-cut otherwise; continuation rows indented two spaces; newlines inside the text start a new row). Prefixes are package constants (`readableThinkingPrefix = "· thinking "`, `readableTextPrefix = "· "`, `readableToolCallPrefix = "· tool call "`). Raw mode's rows stay `lines` exactly as today.

**Regression guard.** Amend the header comment (`inspector.go:42-45`, "Its rows are FLAT … rather than wrapped") and the `wireRecord` doc (`inspector.go:47-49`, "pretty-printed ONCE") to say rows are flat at the PANE but the readable rendering is pre-wrapped at 96 at fold time; rule: every comment in `inspector.go` naming "wrapped"/"pretty-printed" (`grep -n 'wrap\|pretty' internal/tui/inspector.go`). The sseChunk mirror is kept because `sseChunk` is unexported in internal/provider/stream.go:383 — replace the false premise "the TUI does not import provider" with that reason. A request body that decodes but carries none of `messages`/`tools`/`model` falls back to `wirePayloadLines` exactly like an undecodable body (so today's fixtures such as `{"a":1}` render as before, and row counts stay deterministic); add a test for it.

**Files:** internal/tui/inspector.go, internal/tui/inspector_test.go

**Tests:** in `inspector_test.go` with the existing `wireEvent` helpers: request summary with all three fields and with `tools` absent; a request body that decodes but carries none of the three (`{"a":1}`) — `readable`/`readableHidden` equal `lines`/`hidden`; thinking merge across three deltas with an empty delta in the middle; text merge; a tool-call chunk (name + long id → 12 chars, arguments absent from every row); a usage-only chunk kept pretty; malformed JSON line verbatim; `[DONE]` verbatim; a 400-char passage wrapped into ≥5 rows with the prefix on the first only; a response whose pretty form exceeds 100 lines but whose readable form does not — `hidden > 0`, `readableHidden == 0`; `TestWireRingKeepsTheLatestRecordsInOrder` and every existing test pass unchanged.

**Acceptance:** `go build ./...`; `go test ./internal/tui -run 'TestWire|TestInspector|TestReadable' -count=1`

**Commit:** feat(inspector): readable wire rendering stored beside the pretty-JSON lines

**Regression guard.** Every existing inspector test passes byte-for-byte: `lines`, `hidden`, `wirePayloadLines` and `prettyWireLine` are untouched, and `inspectorRows` still renders `lines` until item 2 lands. `TestReasoningTailIsRenderedNowhere` passes — nothing here reads `m.reasoning`. A decoder failure never drops a line: the fallback for every unclassifiable line is its pretty form.

## 2. The `ctrl+r` toggle between readable and raw — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item offered "assert the readable line `model red`" for
`TestWirePayloadReachesThePaneStrippedAndPretty`; that line is not producible — `stripEscapes`
removes the ESC byte and leaves the CSI remainder, so the summary reads `model [31mred[0m`.
Asserted the readable SHAPE instead (the pane shows `model ` and NOT the JSON key `"model"`),
keeping the raw-mode `"model"` assertion the item requires.
NOTES (2026-09-02): `inspectorHint` kept as the readable-mode hint and `inspectorRawHint` added
beside it, since the item asks for both hints and the pane must spell one per rendering.

**What:** Recast at the regression check (2026-09-01). `reportPane` (`internal/tui/reportpane.go`) gains `raw bool` (false = readable). `dismissReport` already zeroes the struct, so a reopen starts readable with no new code. `reportKey` claims `"ctrl+r"` ONLY for `inspectReport` while it is open — a kind check beside the `esc` case, the module's existing pattern (`reportContent`, `pane`); no hook type, no function field. `/usage` is key-identical. `inspectorRows` (item 1's struct) renders `rec.readable`/`rec.readableHidden` when `!m.inspector.raw`, else `rec.lines`/`rec.hidden`; the elision marker uses the shown mode's count. `inspectorHint` becomes `↑/↓ scroll · ctrl+r raw · esc close`; `inspectContent` becomes a Model method `m.inspectContent() reportContent` that reads the mode off the Model, so the hint reads `ctrl+r readable` while raw; both callers — `inspector.go`'s `inspectorSpec` and `reportpane.go`'s `reportContent` — route through it. The `/inspect` commandSpec summary (`internal/tui/command.go`, the `/inspect` entry) names the readable default and the toggle in one sentence. Depends on item 1.

**Regression guard.** `inspectContent` becomes a Model method `m.inspectContent() reportContent` that reads the mode (and, from item 3, the title) off the Model; both callers — inspector.go's `inspectorSpec` and reportpane.go's `reportContent` — route through it. internal/tui/reportpane.go stays in Files. Amend `TestWirePayloadReachesThePaneStrippedAndPretty` (inspector_test.go:98): set `m.inspector.raw = true` before `renderInspector` for the `"model"` assertion (or assert the readable line `model red`), keeping a raw-mode assertion of `"model"`. Amend `TestWireRecordCapsItsLinesAndSaysSo` (inspector_test.go:130-135): flip `m.inspector.raw = true` before `inspectorRows()` for the raw marker, and add the readable-mode expectation — its `{"a":[1,…]}` body carries none of `messages`/`tools`/`model`, so under item 1's fallback the readable rows end on the same marker (`readableHidden == hidden`). Amend the keyboard comments this key falsifies — `reportpane.go:19` ("four scroll keys are its whole keyboard"), `inspector.go:37`, `model.go:1300` ("the same five keys") and the `inspector_test.go:348,424` comments — so `grep -n 'five keys\|four scroll keys\|whole keyboard' internal/tui/*.go` names `ctrl+r` wherever it hits.

**Files:** internal/tui/reportpane.go, internal/tui/inspector.go, internal/tui/model.go, internal/tui/command.go, internal/tui/inspector_test.go, internal/tui/reportpane_test.go

**Tests:** `TestWirePayloadReachesThePaneStrippedAndPretty` asserts `"model"` in raw mode and `model red` in readable; `TestWireRecordCapsItsLinesAndSaysSo` asserts the elision marker in raw mode and the same marker in readable (item 1's fallback); drive the pane per `docs/design/test-drivers.md` with `tea.KeyPressMsg` strings — `internal/tuitest` has no CtrlR constant, so the in-package test sends `tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}` (its String() is "ctrl+r"): `"ctrl+r"` while open flips readable→raw→readable and the rendered frame changes from `· thinking` rows to indented JSON; `"ctrl+r"` while closed is unhandled and reaches the prompt path; `"r"` while open is unhandled (non-modal doctrine); both hints asserted verbatim; `/usage` ignores `"ctrl+r"`; dismiss-then-reopen shows readable rows again; `TestInspectorLeavesEveryOtherKeyAlone` extended with `"ctrl+r"` on the closed pane.

**Acceptance:** `go build ./...`; `go test ./internal/tui -run 'TestWire|TestInspector|TestReport|TestUsage' -count=1`

**Commit:** feat(inspector): ctrl+r toggles pretty-JSON in the /inspect pane

**Regression guard.** esc, up, down, pgup, pgdown behave identically for BOTH reports; `reportKey` stays non-modal (every printable key still falls through — the `"r"` test pins it). The `reportPane` zero value stays "closed at the top, readable". The scroll-clamp in `reportSpec` is re-evaluated against the shown mode's row count, so flipping modes never paints past the end.

## 3. /inspect scoped to the viewed sub-agent

**What:** Recast at the regression check (2026-09-01). With a run view open, /inspect shows only that run's records; otherwise the whole ring as today. Scope key: `m.viewedRun()` (`runview.go:36`) equals `runRef{rec.depth, rec.callID}` — build the ref with `runOf`'s field mapping. Filter at row composition: a new `m.scopedWire() []wireRecord` returns `m.wire` unscoped, or the filtered slice (a fresh slice, ADR 0011) when `m.inRunView()`; `inspectorRows` iterates that slice and passes it to `hasUnrecordedReply`, so headers, elision counts and no-reply notes are computed over the scoped list only. Title: `raw wire traffic · <runLabel(spawn)>` when scoped, byte-identical `raw wire traffic` otherwise (composed inside `m.inspectContent()`, item 2's Model method — no signature change lands here). Empty scoped list with capture ON (`m.opts.Inspector`) → ONE row, `inspectorScopedEmptyRow = "no records for this run — not called yet, rotated out of the ring, or an unrouted delegation speaking over its parent's connection; close the view for the whole ring"`; with capture OFF the scoped-empty slot keeps `inspectorDisarmedRow` (capture off is the cause); an empty UNSCOPED ring keeps today's armed/disarmed rows. `runInspectCommand` sets `top` past the SCOPED row count. A viewed run whose records rotated out of the ring shows the scoped-empty row too. `wireRecordHeader` unchanged. Depends on item 2.

**Regression guard.** (a) With capture OFF (`!m.opts.Inspector`), the scoped-empty slot keeps `inspectorDisarmedRow` — capture off is the cause and the ratified call bars only the ARMED row; only with capture ON does the empty scoped list show `inspectorScopedEmptyRow`. (b) `inspectorScopedEmptyRow` is worded for every cause it covers: "no records for this run — not called yet, rotated out of the ring, or an unrouted delegation speaking over its parent's connection; close the view for the whole ring"; update the header's "Scoped empty" ratified line to that wording (owner, 2026-09-01, refined at regression check). (c) The scoped title is composed inside `m.inspectContent()` (item 2's Model method), so no signature change lands in this item; add internal/tui/reportpane.go to Files. Tests: the disarmed-scoped case asserts `inspectorDisarmedRow`; the armed-scoped-empty case asserts the new wording verbatim. This item yields to the documented decision at `inspector.go:24-27` and `layout.md:1715-1719` (the disarmed row is the one actionable answer to an empty pane): that row is never displaced by the scoped-empty row.

**Files:** internal/tui/inspector.go, internal/tui/reportpane.go, internal/tui/inspector_test.go

**Tests:** per `docs/design/test-drivers.md`: open a run view for a routed child and fold wire events for the child's `(1, callID)`, a sibling's `(1, otherID)` and the parent's `(0, "")`: the pane lists only the child's records under the scoped title; close the view — the same ring lists all three under the plain title in ring order; a viewed run with zero records and capture ON shows `inspectorScopedEmptyRow` verbatim (the reworded text); the same with capture OFF shows `inspectorDisarmedRow`; a sibling's response landing between the child's request and its reply does not put `inspectorNoReplyRow` under the child's request; the pane opens on the child's newest record.

**Acceptance:** `go build ./...`; `go test ./internal/tui -run 'TestWire|TestInspector|TestRunView' -count=1`

**Commit:** feat(inspector): scope /inspect to the viewed sub-agent's wire stream

**Regression guard.** Unscoped title, row order, armed/disarmed rows and `hasUnrecordedReply` pairing stay byte-identical (existing tests unchanged). The run-view esc contract (`runViewOwnsEsc`) is untouched: with /inspect open, esc still closes the pane first, as today. `hasUnrecordedReply` is asked only over the scoped slice.

## 4. Documentation for the readable, scoping /inspect

**What:** Update the prose that pins /inspect: `layout.md` /inspect section (the block starting at "It opens on the newest record", :1730-1740) — readable by default, `ctrl+r`, merged-and-wrapped passages, the scoping rule, scoped title and scoped-empty row; the keyboard sentence there ("`esc`, `↑`/`↓` … and nothing else") gains `ctrl+r`; `CONTEXT.md` gains an Inspector term beside the TUI/Driver concepts (it has no Inspector section today) stating the default rendering, `ctrl+r` and the run-view scoping, in the concept map's voice; `docs/manual/commands.md` /inspect row — the toggle and the scoping in one sentence each; `IDEAS.md` line 12 removed (delivered). The report doctrine's printable-key rule in `layout.md:1698` and `reportpane.go`'s header stays as written — `ctrl+r` is not a printable key; the "four scroll keys are its whole keyboard" / "same five keys" sentences in the code comments are item 2's to amend. Depends on item 3.

**Regression guard.** `CONTEXT.md` has no Inspector section (`grep -in 'inspector\|wire traffic\|WireEvent' CONTEXT.md` is empty), so this item ADDS an Inspector term beside the TUI/Driver concepts stating default rendering, `ctrl+r` and the run-view scoping; the acceptance grep on CONTEXT.md is kept. The "stays as written" claim covers only the printable-key doctrine: `reportpane.go:18-19`, `inspector.go:37` and `model.go:1300` are false once `ctrl+r` lands and item 2 amends them (named in its guard); the sweep here extends to code comments — `grep -n 'five keys\|four scroll keys\|whole keyboard' internal/tui/*.go` must show no sentence still claiming esc and the four scroll keys are the whole keyboard.

**Files:** layout.md, CONTEXT.md, docs/manual/commands.md, IDEAS.md

**Tests:** none (prose).

**Acceptance:** `grep -n "ctrl+r" layout.md CONTEXT.md docs/manual/commands.md` hits all three; `grep -n "inspect raw wire" IDEAS.md` finds nothing; no remaining claim in the three doc files says the pane is pretty-JSON-only, always global, or that esc and the four scroll keys are its whole keyboard.

**Commit:** docs: /inspect readable rendering and run-view scoping

**Regression guard.** The three doc files agree on the default mode, the key and the scoping rule — each states all three, none contradicts another. Rule for the sweep: every sentence naming /inspect's keyboard or calling it "raw-protocol"/"pretty-printed" as its ONLY description — `grep -n "inspect" layout.md CONTEXT.md docs/manual/commands.md`.

---

**Suggested version bump:** micro (`VERSION` third component) — user-facing feature work in one subsystem; the user owns the bump and it lands as its own commit at release time.
