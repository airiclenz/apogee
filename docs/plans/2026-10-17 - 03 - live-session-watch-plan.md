# Plan: live session watch mode

**Goal:** Let a person watch a running apogee session (TUI, headless or daemon Driver) live and read-only in the TUI: streamed text, tool cards, read-only run views, and thinking in `/thinking`. Every Driver writes a per-session Event journal while a session runs. `/sessions` and `--resume <id>` open a live session in watch mode. Homes of other users, once configured, are watchable the same way.
**Date:** 2026-10-17
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f5b4e1fd
**Closes:** live-session-watch

**Sources:**
- `docs/handoffs/2026-10-17 - 00 - live session watch mode.md` (local only; findings re-verified at base)
- ADR 0011, 0022, 0031, 0033, 0034, 0052, 0063, 0075, 0083, 0090, 0096, 0100 (`capcom know show "ADR NNNN"`)
- Glossary: Driver, Session, Event, Event lines, Firing, Run view, Stage view, Thinking pane

**Ratified design calls (owner, 2026-10-17):**
- **Scope:** a general feature for every Driver, not Seqcom-specific.
- **Thinking:** shown only in the `/thinking` pop-up, never inline.
- **Entry:** `/sessions` and `--resume <id>`. No `apogee watch` subcommand.
- **Retention:** the owner deletes the journal at release; the startup sweep removes orphans whose lock is free. The record stays the only history.
- **Coalescing:** the journal merges token/reasoning deltas over a ~30 ms window, losslessly. This is a named variant of Event lines with its own golden.
- **Owner ends:** a banner, and the view stays read-only. In your own home, ⏎ resumes through the normal door. A watched home is never resumable.
- **Leaving:** the prompt box reads `watching <title> · read-only · esc back`. esc returns to the previous session, or to a fresh one after `--resume`.
- **Cross-user:** in this plan, last. `sessions.watch-homes` and `sessions.share-group: <group>` are both global-only keys.
- **Browser:** one list. Live rows get a `● live` cell. Rows from watched homes are mixed in by time and tagged `· <label>`, and they are read-only.

**Regression check (2026-10-17, f5b4e1fd):**
- 1 — guard folded (decision); yields to ADR 0052 amend-0824 and ADR 0025's decision as written (no amendment to either).
- 2 — guard folded (decision); yields to the encoder's no-omitempty rule (`internal/eventjson/encode.go:242-244`).
- 3 — guard folded (decision); supersedes ADR 0025's "SubAgentGroupEvent writes no NDJSON line" (`adr-0025-decision.md:64`).
- 4 — guard folded (decision).
- 5 — guard folded (decision).
- 6 — guard folded (decision); the orphan sweep yields to the free unconfigured default (`cmd/apogee/wire.go:676-681`, `internal/session/store.go:536-539`).
- 7 — guard folded (decision); yields to ADR 0075 decision 9 (the Event-line encoder stays the outermost sink).
- 8 — guard folded (decision).
- 9 — recast (decision): split into 9 (watch mode core) and a new 9a (watch mode isolates the owner's session).
- 9a — new, split from 9 (decision).
- 10 — guard folded; depends on 9a (decision 9).
- 11 — guard folded (decision).
- 12 — guard folded (decision).
- 13 — guard folded.
- 14 — guard folded.
- 15 — guard folded.
- 16 — guard folded; supersedes `SECURITY.md:51` (hostile local user out of scope) for watched homes, and `docs/manual/sessions.md:46-50` (held session refused at `--resume` and ⏎).
- Round 2 (regression-1, -2, -4, -5; same base). Where a line below disagrees with a round-1 line above, the round-2 line wins:
- 1 — recast (decision): supersedes ADR 0052 amend-0824 (`adr-0052-amend-0824.md:17`) and ADR 0025's decision (`adr-0025-decision.md:64`) by dated amendment Notes, replacing the round-1 yield; decision 3 corrected (`run_started` already carries `workspace`).
- 2 — recast (decision): a nil summary writes `"summary":null`; yields to the encoder's no-omitempty rule (`internal/eventjson/encode.go:242-244`) and ADR 0075 D3.
- 3 — recast (decision): the `cancelled` bullet is dropped (stays the constant false documented in `docs/manual/headless.md`); supersedes ADR 0025's decision (`adr-0025-decision.md:64`).
- 4 — guard folded (decision): documented lossy points and comparison rules.
- 5 — recast (decision): window override, unmerged forwarding to inner, a journal-only frame struct.
- 6 — recast (decision): supersedes the "unconfigured default is free — Prune walks no directory" notes (`cmd/apogee/wire.go:676-681`, `internal/session/store.go:536-539`), replacing the round-1 yield; contention is absorbed in `Store.hold`, not `platform.AcquireLock`.
- 8 — recast (decision): `sessionHost` drives the journal; supersedes `internal/session/live.go:30-31` ("hold at BIRTH, never at the mint").
- 9 — recast (decision): one allow-list input gate at the send/command chokepoints; regression-5's item-9 guards folded.
- 9a — recast (decision): parks exactly `resetSessionView`'s field set; regression-5's item-9a guards folded.
- 11 — guard folded (decision): depends on 9a.
- 12 — guard folded (decision): depends on 9a.

**Standing requirements:**
- skills: coding-standards
- Each `go test` command names one package and, for `internal/tui` and `cmd/apogee`, a `-run` pattern (AGENTS.md: never a multi-package race run).
- Everything a watcher reads is untrusted: text reaches the view only through the existing escape stripping (`internal/tui/sanitize.go`).
- A watcher never calls `Store.Load`/`Hold`/`Save`/`Delete`/`Prune`, and never takes a lock.

**Out of scope:** inline thinking; replaying a finished run from its journal; an `apogee watch` subcommand; watch over a network or socket; interjecting into or steering a watched session; `--format` for the daemon; `WireEvent` on any line (ADR 0075 D2).

## 1. ADR 0103 and glossary: event journal and watch mode

**What:** Recast at the regression check (2026-10-17).
**Goal:** ADR 0103 records the design. The ADRs it amends and the glossary terms it touches carry dated notes pointing at it, and `capcom check` reports no problems.
**Approach (assumed at the header base):** `capcom know add --parent decisions` for `adr-0103` ("Live sessions are watchable through a Driver-written event journal"), with its sections as child Notes. Decisions, binding on every later item:
1. Each Driver composes the journal as a sink. The engine stays wire-silent (ADR 0031 rule 1).
2. The journal is `sessions/<id>.events.jsonl` in the Event-lines envelope `v:2`, deltas coalesced ~30 ms. It is synchronous, and on a write error it reports once and stops (ADR 0075 D9).
3. One `run_started`/`run_finished` pair per journal, which spans one hold of one record id. `run_started` already carries `workspace`; the journal's frame gains only `title` and `entries` (the record's transcript entry count at the journal's birth).
4. Birth at the record's hold, after `hasPrompt`, never under `--no-save`. Removal comes after the final save and before the lock is released.
5. Watch mode is not a Driver: it drives no loop and has an inert Engine. It never saves, holds or writes. It paints the record's first `entries` entries, then follows the journal.
6. A tailed file is not IPC (ADR 0034 D2).
7. A watched home is a read-only source and never a second home (ADR 0100). Nothing in it is swept, written, held or configured from.
8. `share-group` is the one opt-in exception to the owner-private modes. Both keys are global-only (ADR 0096 D6). It is OS-group read access, not RBAC (ADR 0031 non-goal).
9. ToolSummary gains an eventjson-owned wire mirror. It is decoded only for live watching, never on a record replay path (ADR 0052 D5, ADR 0083 part 1).

Add dated amendment Notes to:
- ADR 0022 D7: `--resume` and the browser's ⏎ on a held record now watch it; rename, delete and `--continue` still refuse.
- ADR 0022 consequences: the share-group exception.
- ADR 0075 D13/D5: the journal reaches every Driver, and the frames are per journal.
- ADR 0034 D2, and ADR 0100.
- ADR 0052 amend-0824: ToolSummary had no wire form; ADR 0103 gives it an eventjson mirror, decoded only for live watch.
- ADR 0025 decision: SubAgentGroupEvent wrote no line; it now writes `sub_agent_group`.

Glossary:
- New Notes: **Event journal**, **Watch mode**, **Live session**, **Watched home**.
- Amend **Session** (doors), **Event lines** (the "no log files" line), **Thinking pane** (a watcher fills it live), **Driver** (a watcher is not one), **Tool summary** and **Edit regions** (the summary now has a wire form).
**Regression guard.** ADR 0103 also records (a) the interactive TUI takes its record's hold at the first prompt submission (hasPrompt), before Turn 1 — "hold at the first prompt, never at the mint" — amending ADR 0022 D7's 2026-09-19 timing and `internal/session/live.go`'s documented "hold at BIRTH, never at the mint" (item 8); (b) the journal carries a Driver-written `prompt` line kind for every top-level prompt and interjection, because ADR 0025 keeps prompts off the Event stream; (c) `Store.hold` absorbs a probe-length lock contention (item 6), so a watcher's shared probe never makes an owner refuse. Dated amendment Notes explicitly supersede ADR 0052 amend-0824 (`adr-0052-amend-0824.md:17`, "never reaches disk and has no wire form": ToolSummary had no wire form; ADR 0103 gives it an eventjson mirror decoded only for live watch) and ADR 0025's decision (`adr-0025-decision.md:64`: SubAgentGroupEvent writes no line; it now does). Rule: amend every glossary or decision Note that `grep -rln -i 'no wire form\|never reaches disk' .capcom/knowledge` finds (at base: Tool summary, Edit regions, `adr-0052-decision.md`, `adr-0052-amend-0824.md`). Decision 3 is corrected: `run_started` already carries `workspace` (`eventjson.RunStarted.Workspace`); the journal frame gains only `title` and `entries`.
**Files:** .capcom/knowledge/decisions/adr-0103.md, .capcom/knowledge/decisions/adr-0103/, .capcom/knowledge/decisions/adr-0022/, .capcom/knowledge/decisions/adr-0025/, .capcom/knowledge/decisions/adr-0052/, .capcom/knowledge/decisions/adr-0075/, .capcom/knowledge/decisions/adr-0034/, .capcom/knowledge/decisions/adr-0100/, .capcom/knowledge/glossary/glossary-identity/, .capcom/knowledge/glossary/glossary-context/
**Read first:** .capcom/knowledge/decisions/adr-0075/adr-0075-decision.md — D3, D5, D13; .capcom/knowledge/decisions/adr-0052/adr-0052-amend-0824.md; .capcom/knowledge/decisions/adr-0025/adr-0025-decision.md; .capcom/knowledge/glossary/glossary-identity/event-lines.md; .capcom/knowledge/glossary/glossary-context/tool-summary.md; .capcom/knowledge/decisions/adr-0034/adr-0034-decision.md — D2; .capcom/knowledge/decisions/adr-0022/adr-0022-decision.md — D7
**Tests:** none (knowledge only).
**Acceptance:** `capcom check`; `capcom know show "ADR 0103"`; `capcom know show "Event journal"`; `capcom know show "Watch mode"`; `capcom know show "Tool summary"` names ADR 0103.
**Commit:** `docs(adr): record ADR 0103 — live sessions watchable through an event journal`

## 2. eventjson: ToolSummary wire mirror

**What:** Recast at the regression check (2026-10-17).
**Goal:** `tool_result` and `sub_agent_phase` lines carry the result's ToolSummary as an additive `summary` object, `{"kind": <variant>, …facts}`, for all eight variants in `internal/domain/toolsummary.go`. A nil summary writes `"summary":null`. `v` stays 2.
**Approach (assumed at the header base):** extend `toolResult`/`toolResultOf` in `internal/eventjson/encode.go` with a private tagged mirror type per variant: `read_span`, `listed_entries`, `matched_lines`, `diff_stat`, `changed_files`, `edit_regions`, `search_hits`, `delegation_outcome`. eventjson owns the mirror, as `session.EditRegion` mirrors regions for the transcript codec. Amend the "NO WIRE FORM" paragraph in `toolsummary.go` to name this mirror and ADR 0103. Add the `summary` field to the `tool_result` and `sub_agent_phase` rows of `docs/manual/headless.md`'s event reference.
**Regression guard.** A nil summary writes `"summary":null`, never an absent key: no omitempty, so the item yields to the encoder's rule (`internal/eventjson/encode.go:242-244`) and ADR 0075 D3 (a consumer never tests for a missing key). Rule: amend every comment that says the summary has no wire form, is never persisted or is dropped from the line — `grep -rn -i 'no wire form\|never persisted\|dropping Summary\|drops the summary' internal/domain internal/eventjson` (at base: `toolsummary.go`'s NO WIRE FORM and EditRegions comments, `tools.go:572`, `encode.go:531` and `:576`, the `encode_test.go:93` case name). Files add cmd/apogee/testdata/eventlines/run.jsonl (the ADR 0075 D14 golden, re-recorded with `-update`) and internal/domain/tools.go; internal/eventjson/testdata/ does not exist and is not used by this item. Acceptance runs one `go test` command per package.
**Files:** internal/eventjson/encode.go, internal/eventjson/encode_test.go, cmd/apogee/testdata/eventlines/run.jsonl, internal/domain/toolsummary.go, internal/domain/tools.go, docs/manual/headless.md
**Read first:** internal/eventjson/encode.go — toolResult, toolResultOf, subAgentPhaseData, encode; internal/domain/toolsummary.go — ToolSummary, EditRegions, DelegationOutcome; internal/eventjson/encode_test.go — TestEncodeJSONGolden; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden, eventLinesGolden, eventLinesRedactions; cmd/apogee/testdata/eventlines/run.jsonl; internal/agent/dispatch.go — ToolResultEvent emission; docs/manual/headless.md — event kinds table
**Tests:** one encode test per variant, plus nil → `"summary":null`. The golden `cmd/apogee/testdata/eventlines/run.jsonl` is re-recorded (ADR 0075 D14). The grep above finds no stale comment.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 ./internal/domain/ && go test -race -count=1 -run 'TestE2EEventLinesGolden|TestManualListsEveryEventLineKind' ./cmd/apogee/`
**Commit:** `feat(eventjson): encode ToolResult.Summary as a tagged summary mirror`

## 3. eventjson: close the remaining encoder gaps

**What:** Recast at the regression check (2026-10-17).
**Goal:** every Event variant except `WireEvent` encodes the fields the TUI's fold reads. These fields and events are added under `v` 2:
- `SubAgentPhaseEvent.StepCap` and `CapRequested`.
- `UsageEvent.DelegateFold` and `Cumulative.PricedCalls`.
- `WorkflowPhaseEvent` ItemName, Background, Origin, Stages, StageKinds, Items, Round, Rounds, Outcome, Run, Attempt, Resume, Tally.
- The `WorkflowItemStarted` and `WorkflowStageFinished` phases.
- `SubAgentGroupEvent`, as a new line kind `sub_agent_group`.
- `ToolCall.Malformed`.
- `UserInput.Recipe` and `RecipeInputs`.

`SeamClosedEvent.Value` and `Image.Data` stay dropped, as documented.
**Approach (assumed at the header base):** extend the per-kind builders in `internal/eventjson/encode.go` and `Kinds()`, and correct the encoder's own gap comment. `cancelled` stays the documented constant `false`. Add the new rows and fields to `docs/manual/headless.md`'s event reference.
**Regression guard.** The `cancelled` bullet is dropped: it stays the constant `false` documented in `docs/manual/headless.md`'s `sub_agent_phase` row (SubAgentPhaseEvent has no cancelled field, `internal/domain/events.go:242-244`); item 2's `delegation_outcome` summary carries the stop. Writing `sub_agent_group` supersedes ADR 0025's decision that SubAgentGroupEvent "is in-process only and writes no NDJSON line" (`.capcom/knowledge/decisions/adr-0025/adr-0025-decision.md:64`), by item 1's amendment Note. `cmd/apogee/headless_test.go`'s stub emits a SubAgentGroupEvent at its `Kinds()` position, so `TestHeadlessFormatJSONStreamsEveryEvent` stays green. `TestKindsAreTwentyThree` asserts a kind count of 24; `TestEncodeSkipsTheSubAgentGroupEvent` and `TestEncodeSkipsTheDriverOnlyWorkflowPhases` are inverted to assert the new lines. Rule: fix every comment that says a field, phase or variant is not on the line — `grep -n -i 'ndjson\|not on the\|no line kind\|twenty' internal/domain/events.go internal/eventjson/*.go`. The golden `cmd/apogee/testdata/eventlines/run.jsonl` is re-recorded (usage gains `delegate_fold`/`priced_calls`, tool_call gains `malformed`); internal/eventjson/testdata/ is not used by this item; `sub_agent_group` is listed in `docs/manual/headless.md` so `TestManualListsEveryEventLineKind` passes.
**Files:** internal/eventjson/encode.go, internal/eventjson/encode_test.go, internal/eventjson/doc.go, internal/domain/events.go, cmd/apogee/headless_test.go, cmd/apogee/testdata/eventlines/run.jsonl, docs/manual/headless.md
**Read first:** internal/eventjson/encode.go — encode, Kinds, subAgentPhaseData, workflowPhaseData, isLineWorkflowPhase, toolCall, userInput; internal/domain/events.go — SubAgentPhaseEvent, SubAgentGroupEvent, WorkflowPhaseEvent, UsageEvent; internal/eventjson/encode_test.go — TestKindsAreTwentyThree, TestEncodeSkipsTheSubAgentGroupEvent, TestEncodeSkipsTheDriverOnlyWorkflowPhases; cmd/apogee/headless_test.go — TestHeadlessFormatJSONStreamsEveryEvent; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/domain/tools.go — ToolCall, MalformedArguments; internal/domain/config.go — UserInput, RecipeLaunch
**Tests:** one encode test per added field or kind; the golden `cmd/apogee/testdata/eventlines/run.jsonl` is re-recorded; a test asserts every `domain` Event variant except `WireEvent` maps to a kind; `TestKindsAreTwentyThree` pins 24 kinds; `TestEncodeSkipsTheSubAgentGroupEvent` and `TestEncodeSkipsTheDriverOnlyWorkflowPhases` inverted to assert the `sub_agent_group` and the two phase lines; `TestHeadlessFormatJSONStreamsEveryEvent` green with the stub's SubAgentGroupEvent; `sub_agent_group` is listed in `docs/manual/headless.md`; the grep above finds no stale comment.
**Acceptance:** `go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 -run 'TestE2EEventLinesGolden|TestHeadlessFormatJSONStreamsEveryEvent|TestManualListsEveryEventLineKind' ./cmd/apogee/`
**Commit:** `feat(eventjson): encode the sub-agent, usage, workflow and input fields the TUI folds`

Depends on item 2.

## 4. eventjson: decoder with round-trip tests

**What:**
**Goal:** `eventjson.Decode(line []byte) (Line, error)` returns the envelope fields (`seq`, `time`, `session`, `turn`, `depth`, `call_id`, `run_id`, `v`) and the `domain.Event`, or a frame for `run_started`/`run_finished`. An unknown kind or field is skipped without error (ADR 0075 D10). `v` > 2 is an error. Encoding then decoding every kind returns the original Event, apart from the documented drops and the documented lossy points: UpstreamAttempt durations truncate to whole ms; a nil `Fired` decodes as `[]` and nil receipt `Fields` as `{}`; RawMessage arguments come back compacted and HTML-escaped by `json.Marshal`; `Malformed.Err` comes back as a text-only error (its processing sentinel is not rebuilt).
**Approach (assumed at the header base):** a new `internal/eventjson/decode.go` beside `encode.go`. It reuses the encoder's per-kind wire structs for unmarshalling; do not declare a second set. Decoded text is not sanitized here; the TUI sanitizes at the view.
**Regression guard.** Decode also returns the journal-only `prompt` line (item 5) as its own Line variant {Text, Interjection}. The round-trip compares under the documented lossy points (Goal): fixtures use whole-ms durations, nil and empty compare equal, arguments compare after `json.Compact`, `Malformed.Err` compares by text.
**Files:** internal/eventjson/decode.go, internal/eventjson/decode_test.go, internal/eventjson/encode.go
**Read first:** internal/eventjson/writer.go — envelope, writeLine, lineVersion, RunStarted, RunFinished; internal/eventjson/encode.go — encode, upstreamAttemptData, firedOrEmpty, workflowReceiptOf, rawOrNull, inputImagesOf, CostAmount; internal/domain/reaction.go — Moment.Closing, Seams; internal/eventjson/writer_test.go — TestWriterMalformedDataFallsBackToNullData
**Tests:** a table round-trip over every kind and every ToolSummary variant, run through `Writer`, compared with the Goal's rules (whole-ms durations, nil↔empty equal, arguments after `json.Compact`, `Malformed.Err` by text); tests for an unknown kind, an unknown field, a malformed line, a `v:3` line, and a line with null `data` (writer.go's unmarshalable-argument path); a `prompt` line (top-level and interjection) decodes to its own Line variant {Text, Interjection}.
**Acceptance:** `go test -race -count=1 ./internal/eventjson/`
**Commit:** `feat(eventjson): decode Event lines back to domain events`

Depends on item 3.

## 5. eventjson: coalescing journal writer

**What:** Recast at the regression check (2026-10-17).
**Goal:** `eventjson.NewJournal(w io.Writer, JournalOptions{Session, Title, Workspace, Entries, Report, Now, Window})` is a sink. It writes `run_started` (with the item 1 fields) on open and `run_finished` on `Close`. It merges consecutive `token`/`reasoning` deltas that share depth, run id and turn for up to 30 ms. It flushes a pending delta before any other event, on `Close`, and from a timer once the window passes, so a quiet stream still lands within ~30 ms. It never drops content. On the first write error it reports once through `Report` and goes silent.
**Approach (assumed at the header base):** in `internal/eventjson/journal.go`, on top of `Writer`. A mutex guards the pending delta and the timer; reuse `Writer`'s stamping and error-silencing, and do not copy them. The window constant mirrors `tokenCoalesceWindow` (`internal/tui/sink.go`) by value, with a comment naming it.
**Regression guard.** `JournalOptions` carries `Session`, `Title`, `Workspace`, `Entries`, `Report` and `Now`, plus a window override / timer hook (`Window`, as `teaSink.window` does in `internal/tui/sink.go`) — `internal/testtime` exports no fake clock. The journal golden is built only from event-driven flushes; the timer path is tested with `testtime.Eventually`. Every event is forwarded to the inner sink at once and unmerged; coalescing applies only to the journal's written lines, through `writeLine`, never through `Writer.Emit` (a timer-goroutine forward would bypass `agent.serialEventSink`). The frames use a journal-only struct that adds `title` and `entries` (`run_started` already carries `workspace`), so headless `--format json` frames and cmd/apogee/testdata/eventlines/run.jsonl stay byte-identical. Add `Journal.Prompt(text string, interjection bool)` that flushes any pending delta then writes a `prompt` line; the journal golden lives in a new internal/eventjson/testdata/ dir.
**Files:** internal/eventjson/journal.go, internal/eventjson/journal_test.go, internal/eventjson/testdata/
**Read first:** internal/eventjson/writer.go — Writer, Emit, writeLine, fail, Options, RunStarted; internal/tui/sink.go — teaSink.emitDelta, closeWindow, tokenCoalesceWindow; internal/testtime/testtime.go — Budget, Eventually; internal/testtime/guard_test.go — TestNoFixedTimingInTests; internal/agent/construct.go — serialEventSink; cmd/apogee/headless.go — RunStarted frame
**Tests:**
- Coalescing merges, and the merged text equals the concatenation.
- A non-delta event flushes the pending delta first.
- A timer flush through the `Window` override, awaited with `testtime.Eventually`; the journal golden holds only event-driven flushes.
- Every event reaches the inner sink at once and unmerged, including a delta still pending in the journal.
- Write-error silencing; frame contents; a journal golden.
- Decoding a journal (item 4) yields the same text as the uncoalesced stream.
- `Journal.Prompt` flushes a pending delta, then writes a `prompt` line.
- Headless `--format json` frames stay byte-identical (`TestE2EEventLinesGolden` green, `run.jsonl` untouched).
**Acceptance:** `go test -race -count=1 ./internal/eventjson/ && go test -race -count=1 -run 'TestE2EEventLinesGolden' ./cmd/apogee/`
**Commit:** `feat(eventjson): coalescing event journal writer`

Depends on item 4.

## 6. Session store: journal lifecycle and a read-only hold probe

**What:** Recast at the regression check (2026-10-17).
**Goal:**
- `Store.JournalPath(id)` names `<dir>/<id>.events.jsonl`. `Store.OpenJournal(id)` creates it with the record's file mode, truncating an old one, and `Store.RemoveJournal(id)` removes it.
- `Delete` and `Prune` remove a record's journal with it, best-effort.
- A startup journal sweep, its own step beside the retention sweep, removes any journal whose lock probes free.
- `Store.ProbeHold(id) (held bool, pid int, err error)` opens the lock file read-only and never creates, writes or truncates it. A missing lock reads as not held.
- `Store.Live() ([]LiveEntry, error)` lists ids with a journal present and a held lock. Each entry gives `{ID, PID, JournalPath, HasRecord}`.
**Approach (assumed at the header base):**
- `internal/platform`: add `ProbeLock(path)`, which opens `O_RDONLY` and takes `LOCK_SH|LOCK_NB` on Unix, then unlocks at once. Add the Windows equivalent: `LockFileEx` shared, fail-immediately, on a read-only handle.
- `internal/session/store.go`: the journal API sits beside `lockPath`. The sweep is its own step beside `gcSessions`.
- `.lock` files outlive their runs, so liveness is the probe, never the file's presence.
**Regression guard.** The journal sweep runs as its own step beside `gcSessions` (`cmd/apogee/wire.go`), not inside `Prune` past its zero-Retention early return, and creates nothing (`TestGCSessionsAppliesTheConfiguredPolicy`'s "no store directory" case still finds the root absent). It explicitly supersedes the note "the unconfigured default is free — Prune walks no directory" (`cmd/apogee/wire.go:676-681`, `internal/session/store.go:536-539`): the sweep reads the sessions dir when it exists, and both comments are amended. `Store.hold` absorbs a probe-length contention (`platform.AcquireLockWait` with a few-ms timeout, or a short retry); `platform.AcquireLock` is unchanged, and a pinned test shows Hold succeeds while a ProbeLock is in flight. Delete and Prune remove the journal best-effort and ErrNotExist-tolerant, never changing Delete's return (as `sessionHost.Delete` removes the snapshot, `cmd/apogee/wire_session.go`). `OpenJournal`'s doc states it is only called under the record's hold (its truncate is safe only there).
**Files:** internal/platform/lock.go, internal/platform/lock_unix.go, internal/platform/lock_windows.go, internal/platform/lock_test.go, internal/session/store.go, internal/session/store_test.go, cmd/apogee/wire.go, cmd/apogee/wire_live.go, cmd/apogee/wire_session_test.go
**Read first:** internal/session/store.go — Hold, hold, lockPath, Delete, Prune, scan; internal/platform/lock.go — acquireLock, readLockPID, LockHeldError; internal/platform/lock_unix.go — lockFile; internal/platform/lock_windows.go — lockFile, lockRegion; cmd/apogee/wire.go — gcSessions; cmd/apogee/wire_session.go — probeHold; internal/session/store_test.go — TestStoreDeleteAndPruneRemoveTheLockAfterReleasing; cmd/apogee/wire_session_test.go — TestGCSessionsAppliesTheConfiguredPolicy
**Tests:**
- The probe sees a lock held by `AcquireLock` and leaves the file's bytes and mtime unchanged.
- The probe on a read-only file.
- Delete and Prune remove the journal; a Delete with no journal returns exactly as today.
- The sweep keeps a held journal and removes a free one, and with no store directory creates nothing (`TestGCSessionsAppliesTheConfiguredPolicy` green).
- `Live` lists only held-with-journal ids.
- `Store.Hold` succeeds while a `ProbeLock` is in flight.
**Acceptance:** `go test -race -count=1 ./internal/platform/ && go test -race -count=1 ./internal/session/ && go test -race -count=1 -run 'TestGCSessions' ./cmd/apogee/ && GOOS=windows go vet ./internal/platform/ ./internal/session/`
**Commit:** `feat(session): event journal files and a read-only hold probe`

## 7. Firings write a journal (headless, daemon, TUI-scheduled)

**What:**
**Goal:**
- While a headless run, a daemon Firing or a TUI-scheduled Firing runs with a store, `sessions/<id>.events.jsonl` exists and grows with its Events.
- The journal does not exist after the run returns, nor ever under `--no-save`.
- `--format json` on stdout is unchanged, and the journal sits beside it.
- A journal write failure never fails the run.
**Approach (assumed at the header base):**
- In `raise` (`cmd/apogee/wire_firing.go`), once the record is held (`run.Once` holds it at start), wrap the sink outermost with `eventjson.NewJournal` on `store.OpenJournal(recordID)`.
- `Entries` is the resumed record's transcript length, 0 for a fresh run. `Title` and `Workspace` are the ones the record will carry.
- Close and remove the journal after `run.Once`'s final save and before the hold is released. If the existing order makes that impossible, add a release hook in `internal/run`.
- Headless, daemon (`daemonfire.go`) and `schedule.go` all reach it through `raise`; do not wire each one separately.
**Regression guard.** The journal opens inside run.Once immediately after holdRecord, via an opener carried on run's options (not in raise's narrate slot), and closes/removes after the final save and before the release; Entries is always 0 (Firings never resume a record); the Firing's prompt is written with Journal.Prompt right after the journal opens. The item yields to ADR 0075 decision 9 (`adr-0075-decision.md:135-137`; `cmd/apogee/headless_test.go:3243-3247`): raise's sink composition is untouched and the Event-line encoder stays the outermost sink.
**Files:** cmd/apogee/wire_firing.go, internal/run/run.go, internal/run/run_test.go, cmd/apogee/headless_test.go, internal/run/doc.go (if its lifecycle doc names the hold order)
**Read first:** internal/run/run.go — Once, holdRecord, eventTap, Spec.Store/RecordID; cmd/apogee/wire_firing.go — raise; cmd/apogee/headless.go — the sessions/store/--no-save block; cmd/apogee/headless_test.go — stubRunner.once, headlessRun, TestHeadlessComposesTheRunnerSpec, TestHeadlessFormatJSONEncoderIsOutermost; cmd/apogee/wire_firing_test.go — TestRaiseDecoratesTheSinkAfterTheGate; docs/design/test-drivers.md — `await:` / server.Release
**Tests:**
- A driven headless run against a scripted upstream (`docs/design/test-drivers.md`) whose tool step blocks: assert the journal exists mid-run, decodes, and has `run_started`; assert it is gone after the run.
- `--no-save` leaves no journal.
- An unwritable journal still completes the run.
- The journal's `run_started` carries `entries` 0, and its next line is the Firing's `prompt`.
- `TestHeadlessComposesTheRunnerSpec` and `TestHeadlessFormatJSONEncoderIsOutermost` stay green unchanged.
**Acceptance:** `go test -race -count=1 ./internal/run/ && go test -race -count=1 -run 'TestHeadless.*Journal' ./cmd/apogee/`
**Commit:** `feat(run): Firings write a live event journal while they run`

Depends on items 5, 6.

## 8. The interactive TUI session writes a journal

**What:** Recast at the regression check (2026-10-17).
**Goal:**
- While a TUI session holds its record, `sessions/<id>.events.jsonl` receives that session's Events.
- The record's hold is taken at the first prompt submission (`hasPrompt`), and the journal is born just after that hold, never before.
- On `/clear`, a `/sessions` switch, `/fork` and exit, it closes and is removed after the final save and before the hold is released. A new id gets a new journal.
- `Entries` is the record's transcript length at birth.
**Approach (assumed at the header base):** `sessionHost` in `cmd/apogee/wire_session.go` drives the journal itself. The journal sink sits outermost in the interactive chain (`cmd/apogee/wire_boot.go`, over `statsSink`) behind one swappable holder, so the chain is built once and `sessionHost` swaps the target journal.
**Regression guard.** Within the ratified early hold, `sessionHost` drives the journal, not a `Live` follower (Rotate/Activate release the hold before calling followers; Close and Begin call none): the hold (`Live.Begin`) moves to the first prompt submission, the journal opens just after that hold, and `sessionHost.Rotate`/`Activate`/`Close` close and remove it before delegating to `Live`. Live's documented "hold at BIRTH, never at the mint" (`internal/session/live.go:30-31`) is amended to "hold at the first prompt, never at the mint" (ADR 0103, item 1). The journal holder wraps outermost over `statsSink` (`w.stats.wrap(w.hooks)`), never inside `reactions.Runner` (ADR 0075 D9); `TestRootWiringEmitsThroughTheHookRunner` (`cmd/apogee/wire_settings_test.go`) is updated to unwrap the holder. Every top-level prompt and interjection is written with Journal.Prompt from the cmd/apogee engine wrapper's submit/interject path (cmd/apogee/wire_engine.go), not from internal/tui.
**Files:** cmd/apogee/wire_session.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_session_test.go, cmd/apogee/wire_settings_test.go, internal/session/live.go, internal/session/live_test.go, cmd/apogee/wire_engine.go
**Read first:** internal/session/live.go — Live.Begin, Rotate, Activate, Close, move; cmd/apogee/wire_session.go — sessionHost.Save, Rotate, Activate, Close, newSessionHost; cmd/apogee/wire_boot.go — w.cfg.Events = w.stats.wrap(w.hooks); internal/tui/model.go — turnSnapshotMsg case; internal/tui/sessionsave.go — worthSaving, persist; cmd/apogee/wire_settings_test.go — TestRootWiringEmitsThroughTheHookRunner; cmd/apogee/wire.go — w.host.Close
**Tests:**
- A driven TUI session (test drivers): the journal appears after the first prompt and not before.
- `/clear` removes it and starts a new one under the new id.
- Exit removes it.
- On `/clear`, a switch and exit the journal is removed before the hold is released.
- `TestRootWiringEmitsThroughTheHookRunner` passes with the holder unwrapped.
- Its decoded events match the turns run.
- The record's hold is taken at the first prompt submission, before Turn 1 (`internal/session/live_test.go`).
- Every top-level prompt and interjection appears as a `prompt` line, in order.
**Acceptance:** `go test -race -count=1 ./internal/session/ && go test -race -count=1 -run 'TestSession.*Journal|TestRootWiringEmitsThroughTheHookRunner' ./cmd/apogee/`
**Commit:** `feat(tui): the interactive session writes a live event journal`

Depends on items 5, 6.

## 9. TUI watch mode core

**What:** Recast at the regression check (2026-10-17).
**Goal:** given a watch source `{recordPath, journalPath, title, foreign bool}`, the TUI enters watch mode:
- It paints the record's first `entries` entries via the existing replay, then folds every journal Event through the live eventMsg routing (`foldBackgroundEvent` or `foldEvent`), exactly as live: tool cards, run views, the workflow showing as running, thinking in `/thinking`.
- The prompt box reads `watching <title> · read-only · esc back`. One allow-list input gate at the send/command chokepoints flashes ⏎ instead of sending and refuses every command outside a named view-only set.
- Run views inside it are read-only: no `^x`, no interject.
- It never calls a real Engine method, never saves (no `progressSave`) and never holds.
- On truncation or a new `run_started`, it clears and repaints.
- esc returns to the previous session, or to a fresh one.
**Approach (assumed at the header base):**
- One deep seam. Watch mode swaps `m.eng` for an `inertEngine` whose 37 methods do nothing and return zero values; the owner's engine is parked in a value-typed `watchState` field (ADR 0011: plain fields, no no-copy types).
- A journal follower: a `tea.Cmd` that reads the newly appended complete lines (polling ~100 ms with `internal/testtime`), decodes them with item 4, and returns a batch message tagged with a watch generation, so batches after exit are dropped.
- The running state goes through `markRunning`, which stays the only writer of `stateRunning`.
**Regression guard.** Every item-9 `G:` of the regression check is binding; because they make the item oversized it is SPLIT: this item is the watch mode core (follower, paint without resume notices, legend, ⏎ flash, read-only run views incl. blockcursor ^x, prompt lines, production poll const), and item 9a, right after it, isolates the owner's session (`m.sessions` parked, quit/ctrl+c, engine setters, wake, paint cache, the `fakeSessionHost` assertions). Watch paints `decodeTranscript`→`entries[:min(N,len)]`→`closeInterruptedCalls`→`transcript.replay` directly, with none of `replayScrollback`'s notices ("resumed: <title>", the no-scrollback note, `progressSavedNote`, `interruptedNote`). The poll interval is a plain production const; `internal/testtime` is used only in `watch_test.go`. The run view's ^x is refused in watch, including blockcursor.go's ctrl+x→stopRun. The watch also paints each journal `prompt` line as the user/interjection entry the owner's TUI showed. Item 9 owns one allow-list input gate at the send/command chokepoints (`submit`, `stageWhileRunning`/`stageCommand`, `runCommand`, `queueCommand`), not on the ⏎ case, so ctrl+g (`sendAtIdle`) and a dropdown-accepted verb (`acceptAutocomplete`) meet it too: only a named view-only verb set runs (`/thinking`, `/help`, `/usage`, `/inspect`, `/version`), everything else flashes and is never queued; item 9a extends the gate with the engine-setter refusals. Journal `prompt` text and the legend's `run_started` title pass through `stripEscapes` before they reach the entry and the box. Each decoded Event takes the eventMsg case's routing (`m.workflows.owns` → `foldBackgroundEvent`, else `foldEvent`), minus `progressSave` and `noteEngineHeard`. The inert engine's getters return values captured from the owner's engine at entry (`ConfineToWorkspace`, `ThinkingEffort`, `InExchange`), so the footer is unchanged across enter. `internal/tui/doc.go` names `watch.go` (`TestDocMapNamesEveryFile`); `keyclaim_test.go` changes only if watch joins `keyClaimOrder`.
**Files:** internal/tui/watch.go, internal/tui/watch_test.go, internal/tui/model.go, internal/tui/runview.go, internal/tui/interject.go, internal/tui/autocomplete.go, internal/tui/commandrun.go, internal/tui/tui.go, internal/tui/blockcursor.go, internal/tui/doc.go, internal/tui/keyclaim_test.go (only if watch joins `keyClaimOrder`)
**Read first:** internal/tui/model.go — Update eventMsg case, handleKey (esc/enter/ctrl+g), markRunning, footerRow, footerLeftText; internal/tui/interject.go — sendAtIdle, stageWhileRunning, stageInterjection; internal/tui/autocomplete.go — acceptAutocomplete; internal/tui/workflow.go — backgroundWorkflows.owns, foldBackgroundEvent; internal/tui/transcript.go — addUser, addInterjected, replay, reset; internal/tui/runview.go — runViewKey, stopRun, legend; internal/tui/blockcursor.go — blockCursorKey ctrl+x; internal/tui/tui.go — Engine
**Tests:** a driven TUI over a fixture journal appended during the test asserts:
- streamed text, a tool card with its summary, a sub-agent run view, `/thinking` content, and the legend;
- ⏎ flashes; ctrl+g at idle flashes and adds no entry; a dropdown-accepted verb outside the allow-list, and a /verb typed while the watched run shows running, flash and are never queued; `/thinking` still runs;
- an escape sequence in a token, in a `prompt` line and in the `run_started` title is stripped;
- a background-workflow event in the fixture journal is not painted inline in the conversation;
- the footer (confinement word, effort segment) is unchanged across enter;
- truncation repaints;
- no "resumed:" note (nor any other `replayScrollback` notice) appears;
- a journal `prompt` line paints as the user entry, an interjection one as the interjection entry;
- ^x in a run view and in the block cursor is refused;
- the inert engine records zero calls (the session-host assertions are item 9a's).
**Acceptance:** `go test -race -count=1 -run 'TestWatch|TestDocMapNamesEveryFile|TestKeyClaimOrderMatchesTheDocumentedPrecedence' ./internal/tui/`
**Commit:** `feat(tui): read-only watch mode that follows an event journal`

Depends on item 4.

## 9a. Watch mode isolates the owner's session

**What:** Recast at the regression check (2026-10-17).
**Goal:** while watching, nothing the owner's session holds is saved, changed or lost, and every way out of watch restores it:
- `m.sessions` and exactly the field set `resetSessionView` (`internal/tui/commandrun.go`) resets are parked while watching. Every Update Msg not from the watch follower or input — owner `eventMsg`s and notices included — folds into the parked state, or is held until exit — never into the watch view.
- quit, ctrl+c and every other terminal path first leave watch, then run today's quit.
- Item 9's allow-list gate extends to every engine setter: every verb or key whose run reaches `m.eng` or an `m.opts` host callback (shift+tab, the mode/effort/confine pickers, /model, /clear, /compact, …) is refused with the flash.
- `canWake`/`engineHolds` answers false while watching, so a pending wake survives until exit.
- The paint cache is cleared on both the enter and the exit swap.
**Approach (assumed at the header base):** park `m.sessions` (nil while watching, so `persist`/`saveAtIdle` gate out) and exactly `resetSessionView`'s field set (not a hand-listed set) in `watchState`, beside the parked engine (ADR 0011: plain fields, no no-copy types). Leaving watch restores the parked engine, host and state and the owner's real uiState. Clear the cache with `transcript.paints.clear`, the `transcript.reset` rule.
**Regression guard.** Split from item 9 at the regression check (2026-10-17); its Goal is that item's isolation guards, binding. They close: a `/bg` workflow's depth≥1 ToolResultEvent during watch reaching `progressSave`/`persist` and writing the watched transcript into the owner's record; ctrl+c in watch saving the watched scrollback into the owner's id (idle) or never exiting (running); shift+tab, `/confine` or effort leaving `m.opts` out of step with the real engine; a background-workflow finish wake lost to the inert `Wake`; restored transcript indices hitting the watch session's paints. Park and restore exactly the `resetSessionView` field set (`internal/tui/commandrun.go`), not a hand-listed set. Watch is entered only once `holdRecordWrite` is free (`writeBusy` false, `pendingWrites` empty), or `writeCmd` uses the parked host — a queued write never calls a nil `SessionHost`. Rule: every `Model.Update` Msg case not from the watch follower or input (grep `case .*Msg:`; at base autoTitleMsg, manualTitleMsg, saveDoneMsg, recordWriteDoneMsg, beatMsg, configChangedMsg, scheduleEventMsg, hookNoticeMsg, routingNoticeMsg) is held until exit or folded with the parked state swapped in. Item 9's gate refuses by rule, not by list: every verb or key whose run reaches `m.eng` or an `m.opts` host callback flashes (grep `m\.eng\.` and `m\.opts\.[A-Z][A-Za-z.]*(`), including the mode picker's `acceptMode`. An idle quit may still save the owner's own session (`saveAtIdle`/`worthSaving`): the tests assert what a save carries, not that none happens.
**Files:** internal/tui/watch.go, internal/tui/watch_test.go, internal/tui/model.go, internal/tui/sessionsave.go, internal/tui/commandrun.go, internal/tui/workflow.go, internal/tui/engineholds.go, internal/tui/transcript.go, internal/tui/paintcache.go, internal/tui/confine.go, internal/tui/effort.go, internal/tui/picker.go, internal/tui/autotitle.go, internal/tui/heartbeat.go, internal/tui/settingswatcher.go, internal/tui/settingsapply.go
**Read first:** internal/tui/sessionsave.go — persist, saveAtIdle, saveComplete, pumpWrites, writeCmd, foldRecordWrite; internal/tui/model.go — Update Msg cases, quit, frameChord, shift+tab case; internal/tui/commandrun.go — resetSessionView, startNewSession; internal/tui/engineholds.go — engineHoldSources, canWake, canRebind; internal/tui/autotitle.go — foldAutoTitle, applyTitle; internal/tui/heartbeat.go — foldBeat, applyRebind; internal/tui/picker.go — acceptMode; internal/tui/seam_test.go — fakeSessionHost
**Tests:** driven over a fixture journal, in tests named `TestWatchIsolates*`:
- an owner event injected during watch lands in the parked state, not the watch view, and shows after esc;
- ctrl+c in watch, both idle and while the watched run is running, exits; every save after it carries only the owner's own entries and none of the watched record's, and a fresh owner session saves nothing;
- shift+tab, the mode/effort/confine pickers, /model and /compact flash and leave `m.opts` and the engine unchanged;
- a record write in flight at entry completes without a nil-host call;
- an autoTitleMsg, a beat rebind and a config reload during watch land on the owner's parked state, and show after esc;
- a wake pending when watch starts is still pending at exit;
- after esc the restored transcript paints its own rows;
- `fakeSessionHost` renames, forks and rotates are empty after the watch, and its saves carry none of the watched record's entries, including after an injected owner event and a ctrl+c.
**Acceptance:** `go test -race -count=1 -run 'TestWatchIsolates' ./internal/tui/`
**Commit:** `feat(tui): watch mode isolates the owner's session`

Depends on item 9.

## 10. Watch mode: the owner ends

**What:**
**Goal:** while watching, the TUI probes the hold about once a second.
- When `run_finished` has arrived and the lock is free, a banner reads `finished`. In your own home, ⏎ then leaves watch and opens the record through the normal resume door. In a watched home, ⏎ stays refused.
- When the lock is free with no `run_finished`, the banner reads `owner gone without a final save`.
- The view stays read-only until esc or ⏎-resume.
**Approach (assumed at the header base):** the probe is `Store.ProbeHold` (item 6) through a function the watch source carries, so the TUI keeps no store dependency of its own. The resume reuses `loadSession`.
**Regression guard.** The follower keeps the journal fd open and drains it to EOF after the probe reads free, before it picks a banner; ENOENT reads as "owner releasing", never as truncation. On Windows a watcher's open fd blocks removal (Go opens without FILE_SHARE_DELETE), so `RemoveJournal` tolerates the sharing violation (the sweep collects it) or retries like `renameWithRetry`. ⏎-resume first leaves watch (restores the parked engine, host and state, and sets idle), then takes `acceptBrowser`'s path — `workflowsRunning`→`confirmBoundary(boundarySwitch)`, else `switchSession` — never a bare `loadSession`; before the load it stops the probe chain (bumps the watch generation, no probe in flight), or retries the Park once on a `*HeldError`.
**Files:** internal/tui/watch.go, internal/tui/watch_test.go, internal/tui/sessions.go, internal/session/store.go
**Read first:** internal/tui/sessions.go — loadSession, resumeLoaded, acceptBrowser, switchSession; internal/tui/commandrun.go — confirmBoundary; cmd/apogee/wire_session.go — sessionHost.Load, probeHold; internal/session/store.go — Store.hold, HeldError; internal/session/rename.go — renameWithRetry; internal/tui/model.go — sessionLoadedMsg case (holdSessionLoad release)
**Tests:** driven cases with a fake probe:
- finished in your own home, then ⏎ resumes;
- finished in a foreign home, then ⏎ is refused;
- a crash, which shows the "owner gone" banner;
- a real journal file: `run_finished`, then the file removed, then the probe free — the banner reads `finished`;
- ⏎-resume with a workflow running asks the switch confirm;
- ⏎-resume while a probe is in flight is not refused as held by this process.
**Acceptance:** `go test -race -count=1 -run 'TestWatch.*Owner' ./internal/tui/`
**Commit:** `feat(tui): watch mode reports the owner finishing or vanishing`

Depends on item 9a.

## 11. /sessions lists live sessions and opens them in watch mode

**What:**
**Goal:**
- `/sessions` shows a `● live` cell on every row whose session another instance holds with a journal, including record-less headless and daemon runs.
- A record-less row takes its title, time and workspace from the journal's `run_started`.
- ⏎ on a live row enters watch mode (item 9) instead of the "open in another apogee" refusal.
- Delete and rename of a live row still refuse.
**Approach (assumed at the header base):** the host's list (`cmd/apogee/wire_session.go`) merges `Store.Live()` into `Store.List` results. It decodes only the journal's first line, and a row the decode fails on is skipped. The row model in `internal/tui/sessions.go` (`sessionRows`/`sessionRowCells`) gains a live flag. `acceptBrowser`/`switchSession` route a live row to watch. The exact strings: `● live`, and the refusals from `store.go`'s `HeldError`.
**Regression guard.** The host's merge drops the ids this host holds itself (`h.live.ActiveID()` and the parked id) from the live set, so your own current row is never `● live` and ⏎ on it still reloads it. The live flag and watch source reach the TUI through a new `SessionHost` method (live rows: id, title, time, workspace, watch source), not through `session.Meta`, which stays the on-disk struct; `fakeSessionHost` gains it. `acceptBrowser` branches a live row to watch before `confirmBoundary` and `switchSession`, so no `holdSessionLoad` is taken and no boundary confirm is shown. The Acceptance uses the house test names `TestSessionBrowser*` and `TestSessionHost*`.
**Files:** cmd/apogee/wire_session.go, cmd/apogee/wire_session_test.go, internal/tui/sessions.go, internal/tui/sessions_test.go, internal/tui/tui.go, internal/tui/seam_test.go
**Read first:** cmd/apogee/wire_session.go — sessionHost.List, Load, Delete, Rename, ActiveID; internal/tui/sessions.go — acceptBrowser, switchSession, sessionRowCells, sessionRows, sessionBrowser.record; internal/tui/tui.go — SessionHost; internal/tui/seam_test.go — fakeSessionHost; internal/tui/sessions_test.go — TestSessionBrowserResumeOfTheCurrentSessionReloads, TestSessionBrowserResumeOfAHeldSessionNotesAndStays; internal/session/store.go — Meta, Store.Delete, Store.Rename
**Tests:**
- A host list with a held journal-only id, and with a held record plus journal.
- The host's own active session, with its journal, is not live.
- A driven browser shows `● live`, and ⏎ enters watch, with the legend visible; no load hold is taken and no "stop running workflows?" confirm is shown.
- Delete on a live row shows the held refusal.
**Acceptance:** `go test -race -count=1 -run 'TestSessionBrowser.*Live' ./internal/tui/ && go test -race -count=1 -run 'TestSessionHost.*Live' ./cmd/apogee/`
**Commit:** `feat(sessions): mark live sessions in /sessions and open them in watch mode`

Depends on items 6, 9a.

## 12. --resume <id> on a live session enters watch mode

**What:**
**Goal:**
- `apogee --resume <id>` on a session another instance holds with a journal launches the TUI in watch mode. This covers a record-less headless run, which fails today with "not a known session id".
- A held session without a journal keeps today's refusal.
- `--continue` keeps refusing a held session.
**Approach (assumed at the header base):** in `resolveResumeArg` (`cmd/apogee/wire_session.go`), before `store.Load` and the `probeHold` refusal, check `ProbeHold` together with `JournalPath`. A live id resolves to a watch source that the options carry into the Model (`wire_options.go`) instead of `Resumed`. Escape after `--resume` lands in a fresh session.
**Regression guard.** The clause about `apogee headless --resume` is dropped: no such flag exists, and Firings never resume. A `ProbeHold`/`JournalPath` error or an invalid id reads as not live and falls through to `store.Load` then `LoadPath` unchanged, so a path argument (`--resume ./x.json`) resolves exactly as today. The watch source reaches the Model through `cmd/apogee/wire_live.go` (the `resolveResume` call) and `cmd/apogee/wire.go`; `w.resumed` stays nil on a watch start, so `keepSessions`/`newSessionHost`/`buildAgent` see a fresh start. New tests are named to match `TestResolveResume.*Watch|TestResume.*Watch`.
**Files:** cmd/apogee/wire_session.go, cmd/apogee/wire_options.go, cmd/apogee/wire_live.go, cmd/apogee/wire.go, cmd/apogee/wire_session_test.go
**Read first:** cmd/apogee/wire_session.go — resolveResume, resolveResumeArg, resolveContinue, probeHold; cmd/apogee/wire_live.go — resolveResume call, gcSessions keepSessions, newSessionHost call; cmd/apogee/wire_options.go — Resumed: resumedSession; internal/session/store.go — Hold, validateID, lockPath, HeldError; cmd/apogee/wire_session_test.go — TestResolveResumeRefusesAHeldSessionWithTheFriendlyLine, TestResolveResumeLegacyPath, TestResolveResumeByPathRemintsID
**Tests:**
- `--resume` on a held journal-only id yields a watch source.
- On a held record plus journal, the same.
- On a held id without a journal, the exact refusal text from `HeldError`.
- `--continue` still refuses.
- A path argument still resolves through `LoadPath` (`TestResolveResumeLegacyPath`, `TestResolveResumeByPathRemintsID` green).
- A watch start leaves `w.resumed` nil.
**Acceptance:** `go test -race -count=1 -run 'TestResolveResume.*Watch|TestResume.*Watch|TestResolveResumeLegacyPath|TestResolveResumeByPathRemintsID' ./cmd/apogee/`
**Commit:** `feat(sessions): --resume <id> watches a live session`

Depends on items 6, 9a.

## 13. Config: sessions.watch-homes and sessions.share-group

**What:**
**Goal:** `sessions.watch-homes` (a map of label → absolute home path) and `sessions.share-group` (a group name) parse in the global config.
- Both are global-only: a Project config that sets either is ignored, with a notice (ADR 0096 D6).
- Validation:
  - each home path is absolute and is not this process's own home;
  - each label is non-empty and unique;
  - `share-group` names an existing group the user is a member of;
  - on Windows, `share-group` is refused.
- Every refusal is a startup error that names the key and the value.
**Approach (assumed at the header base):** extend the `sessions:` block in `internal/config/config.go` beside `max-age`/`max-count`, and register both keys in the key-class table that ADR 0096 D6 uses. Document both keys in `docs/manual/configuration.md`.
**Regression guard.** Both keys get `KeyRegistry` rows in `internal/config/registry.go` beside `sessions.max-age`/`max-count` (Class `ClassGlobalOnly`; `keyfield.go` if a field descriptor is used) and commented examples under `sessions:` in `internal/config/defaults/config.yaml`, in registry order. A map on `SessionSettings` breaks `opts.Sessions != tt.want` (config_test.go:5549) and `o.Sessions != want` (keyfield_test.go:279): keep watch-homes out of its comparable shape or move both to `reflect.DeepEqual`. Membership is judged with `os.Getgroups()` against the resolved gid, a numeric gid is accepted, and the refusal says the name lookup is `/etc/group`-only (release builds are `CGO_ENABLED=0`).
**Files:** internal/config/config.go, internal/config/config_test.go, internal/config/registry.go, internal/config/keyfield.go, internal/config/keyfield_test.go, internal/config/defaults/config.yaml, cmd/apogee/settingsrows_test.go, docs/manual/configuration.md
**Read first:** internal/config/registry.go — KeyRegistry (sessions.max-age, sessions.max-count rows), KeyClass, ClassGlobalOnly; internal/config/config.go — SessionSettings, SessionSettings.Validate, sessionsConfig, toSessionSettings, ApplyConfig; internal/config/layer.go — projectGlobalOnlyNotice, applyProjectRows; internal/config/registry_test.go — TestRegistryIsBijectionWithFileConfig, TestRegistryKeyClassesArePinned; internal/config/defaults_test.go — TestTemplateMentionsEveryRegistryKey, TestRegistryFollowsTheTemplateOrder; internal/config/config_test.go — TestEveryConfigKeyReachesTheOptions, everyKeyFileConfig, TestApplyConfigSessionsRetention; cmd/apogee/settingsrows_test.go — TestSettingsRowsFormatEffectiveValues
**Tests:**
- Parsing.
- Each validation error, by its exact text.
- A project-layer setting is ignored, with its notice text.
- A numeric gid is accepted; membership is judged by `os.Getgroups()`.
- `TestRegistryIsBijectionWithFileConfig`, `TestTemplateMentionsEveryRegistryKey` and `TestRegistryFollowsTheTemplateOrder` stay green; `TestSettingsRowsFormatEffectiveValues` pins a value for each new key.
- Any config-docs drift test, kept green.
**Acceptance:** `go test -race -count=1 ./internal/config/ && go test -race -count=1 -run 'TestSettingsRows|TestManualDocumentsEverySettingsKey|TestSettingSections' ./cmd/apogee/`
**Commit:** `feat(config): sessions.watch-homes and sessions.share-group keys`

## 14. share-group: group-readable session files

**What:**
**Goal:** with `sessions.share-group` set, every write to the home's sessions store goes out group-owned by that group:
- the home dir: group-traversable `0710`;
- the `sessions/` dir: `0750`;
- records (including their temp files before rename), journals and lock files: `0640`.
Unset, every mode stays owner-private (`0700`/`0600`). Scratch, snapshots and every other dir stay owner-private either way.
**Approach (assumed at the header base):** the `session.Store` constructor takes an optional sharing option `{GID}`. `Store`'s mkdir, atomic write (`renameRecord` path), `OpenJournal` and the lock path (`platform.AcquireLock` gains a mode/gid option) apply it. `cmd/apogee`'s `resolveRoots` passes it from config. Existing files are re-moded on their next write.
**Regression guard.** `platform.AcquireLock`'s signature stays (`var acquireDaemonLock = platform.AcquireLock`, daemon.go:80, and its daemon_test.go stubs): add a separate `AcquireLockShared(path, mode, gid)` or an unexported option. The option is passed from `opts.Sessions` at all four `session.NewStore` sites (wire_live.go, headless.go, daemonfire.go, undo.go), not via `resolveRoots`. Every create and open applies an explicit Chmod/Chown (fchmod on the lock fd), never create-mode alone. State here and in item 16 that a `0710` home exposes any known-name file whose own mode allows it (a hand-made `0644` config.yaml with a plaintext `api-key:`), and that group read on `.lock` files lets a member hold LOCK_SH against the owner; warn at startup when share-group is set and config.yaml is group-readable.
**Files:** internal/session/store.go, internal/session/store_test.go, internal/platform/lock.go, internal/platform/lock_unix.go, cmd/apogee/wire_live.go, cmd/apogee/headless.go, cmd/apogee/daemonfire.go, cmd/apogee/undo.go
**Read first:** internal/session/store.go — NewStore, save, hold, atomicWrite, dirPerm/filePerm; internal/platform/lock.go — AcquireLock, acquireLock, lockFilePerm, writeLockPID; cmd/apogee/daemon.go — acquireDaemonLock; cmd/apogee/daemon_test.go — acquireDaemonLock stubs; cmd/apogee/wire_live.go, cmd/apogee/headless.go, cmd/apogee/daemonfire.go, cmd/apogee/undo.go — session.NewStore; cmd/apogee/wire.go — resolveRoots
**Tests:**
- Store tests on Unix assert the modes and gid (a supplementary gid from `os.Getgroups`, skipped if there is none) on the record, the temp file, the journal, the lock and the dirs.
- An existing owner-private lock file and record are re-moded on their next open or write.
- Unset modes stay `0600`/`0700`.
- Scratch is untouched.
- Startup warns when share-group is set and the home's config.yaml is group-readable.
**Acceptance:** `go test -race -count=1 ./internal/session/ && go test -race -count=1 ./internal/platform/ && go build ./... && GOOS=windows go vet ./internal/platform/ ./internal/session/`
**Commit:** `feat(session): share-group makes a home's session files group-readable`

Depends on items 6, 13.

## 15. Watched homes in /sessions and --resume

**What:**
**Goal:** sessions under each `sessions.watch-homes` entry appear in `/sessions` mixed in by time, tagged `· <label>`.
- ⏎ on a live one watches it.
- ⏎ on a finished one opens it read-only: watch mode with no journal, painting the record, with the "finished" banner and resume refused.
- Delete and rename refuse with `session <id> is in watched home <label> — read-only`.
- `--resume <id>` also resolves ids found in watched homes, into watch mode.
- Nothing in a watched home is ever created, written, swept, truncated or locked.
- An unreadable watched home shows one notice, not an error.
**Approach (assumed at the header base):** the host opens a read-only view of each watched home's `sessions/` dir: List, Live and ProbeHold only. No retention sweep and no `Hold` path exist on it, which is enforced by type, not by convention. It merges those rows with a home label.
**Regression guard.** The home label rides on a row type or a `json:"-"` field, never a serialized `Meta` member (store.go:143-181); the `SessionHost` change lands in `internal/tui/tui.go` with `fakeSessionHost` (`internal/tui/seam_test.go`) updated. The watched homes reach `resolveResumeArg` and `newSessionHost` through their call sites in `cmd/apogee/wire_live.go`. Watched homes merge only into the browser's open/refresh list (`listSessions`), or the foreign scan is bounded; the relist on the record-write goroutine after a rename or delete (`internal/tui/sessionsave.go`) stays own-home.
**Files:** cmd/apogee/wire_session.go, cmd/apogee/wire_session_test.go, cmd/apogee/wire_live.go, internal/session/store.go, internal/tui/sessions.go, internal/tui/sessions_test.go, internal/tui/tui.go, internal/tui/seam_test.go
**Read first:** cmd/apogee/wire_session.go — sessionHost.List, resolveResumeArg; cmd/apogee/wire_live.go — resolveResume call, newSessionHost call; internal/session/store.go — Store.List, scan, loadPath, Meta; internal/tui/tui.go — SessionHost; internal/tui/sessions.go — listSessions, foldSessionList, acceptBrowser, sessionBrowserVerb, deleteSession, sessionRowCells; internal/tui/sessionsave.go — relist after writeRename/writeDelete; internal/tui/seam_test.go — fakeSessionHost
**Tests:**
- A watched-home fixture listed and tagged.
- A live one watched; a finished one painted read-only.
- Delete and rename show the exact refusal text.
- `--resume` resolves a watched-home id.
- A tree snapshot of the watched home (names, sizes, mtimes, modes) is unchanged after listing, watching and finishing.
- The relist after a rename or delete lists the own home only.
**Acceptance:** `go test -race -count=1 -run 'TestWatchedHome' ./cmd/apogee/ && go test -race -count=1 -run 'TestSessions.*Watched' ./internal/tui/`
**Commit:** `feat(sessions): list and watch sessions from configured watched homes`

Depends on items 11, 12, 13.

## 16. Manual: watch mode, the journal, and the security note

**What:**
**Goal:** the manual documents what ships:
- `docs/manual/sessions.md`: a "Watching a live session" section covering `● live`, ⏎ and `--resume`, the legend, esc, the banners, and watched homes with the Seqcom-style setup (a `share-group` on the owner, `watch-homes` on the watcher, group membership).
- `docs/manual/headless.md` and `docs/manual/daemon.md`: the journal and its lifetime.
- `SECURITY.md`: the journal is unscrubbed, like the record. `share-group` deliberately admits group members as readers. A watcher treats a watched home's bytes as untrusted and never writes there.
- Every count the public docs state stays in agreement with its drift test.
**Approach (assumed at the header base):** prose only. Link ADR 0103.
**Regression guard.** Every manual sentence that says `--resume` or ⏎ refuses a held session is rewritten (`grep -rn 'open in another apogee\|held session' docs/manual README.md`), superseding `docs/manual/sessions.md:46-50` (ADR 0022 D7, amended by item 1); undo's refusal at `headless.md:179` stays. `SECURITY.md:51`'s out-of-scope "hostile local user" sentence is reconciled explicitly — superseded for a watched home's untrusted bytes. "Event journal" is spelled in full on every new mention, because "journal" already names the undo/confinement journal (`SECURITY.md:54-64`, glossary Undo journal). The security note also carries item 14's share-group exposures (the `0710` home, group-readable `.lock` files).
**Files:** docs/manual/sessions.md, docs/manual/headless.md, docs/manual/daemon.md, SECURITY.md, README.md
**Read first:** docs/manual/sessions.md — held-session paragraph, --resume/--continue bullet; docs/manual/headless.md — Event lines sections, undo held refusal; SECURITY.md — out-of-scope line, Windows confinement journal; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/config/reactions_test.go — TestManualStatesTheFloorGuardCount; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** the existing docs drift tests, including `TestManualListsEveryEventLineKind` (headless.md) and `TestManualStatesTheFloorGuardCount` (daemon.md).
**Acceptance:** `go test -race -count=1 -run 'Drift|Doc' ./cmd/apogee/ && go test -race -count=1 -run 'TestManualListsEveryEventLineKind' ./cmd/apogee/ && go test -race -count=1 -run 'TestManualStatesTheFloorGuardCount' ./internal/config/`
**Commit:** `docs(manual): watch mode, the event journal and watched homes`

Depends on items 1–15.
