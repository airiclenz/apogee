# Task list — a model-owned checklist the harness keeps in front of the model

**Goal:** a `task_list` tool holding the model's COMPLETE checklist as engine session state,
re-rendered as a standing block, so a long run knows what is left after compaction.

**Date:** 2026-09-03 · **Status:** unexecuted · **Base:** `5bb33e92` · **sized for:** ~200k-context host

**Sources**
- `docs/handoffs/2026-09-02 - 00 - harness-over-mechanisms-parked-items.md` (parked item 2)
- ADRs `0008` (stateless tools), `0012` (blast radius), `0022` §8 (session vs live state), `0023` §6 + its dated records (standing blocks), `0057` (roster axis), `0059` (engine-holds-it, ctx-carries-it)
- `docs/design/tool-surface-findings.md` — the "Task/todo persistence" denial this plan reverses
- `internal/undo/context.go` (with `internal/console/context.go`) — the ctx-carrier idiom; `docs/design/test-drivers.md` — its "Writing a new e2e test" checklist

**Ratified design calls** (owner, 2026-09-03)
- **Render site:** a standing head block, the fifth part of `standingSystem()`; the prefix KV cache is invalidated whenever the list changes, and that cost is accepted and recorded.
- **Tool shape:** whole-list replace — one call carries the complete list, no item ids.
- **Delegations:** a child gets the tool and its OWN fresh empty list; nothing is inherited.
- **Roster:** default-on for every model, no new config key (`tools.disabled:` turns it off).
- **Gating:** `domain.ReadOnlyTool` ⇒ `classReadOnly` — ungated in every mode, offered in Plan; `ask_user`/`console_close` are the precedent that `IsReadOnly` is a blast-radius axis.
- **Persistence:** a field on `agentState`, so the list survives `--resume` and compaction.
- **Marker:** `[✔]` done / `[ ]` open — `askCheckedMarker`'s glyph (`internal/tui/ask.go`).

**Derived calls** (writer, 2026-09-03)
- **Position:** prompt → orientation → delegate report → **task list** → context files; last of the engine blocks, ahead of the context files so ADR 0023's 2026-08-26 forgery argument still holds.
- **Ride-along:** the orientation block's rule verbatim — composed only onto an already-non-empty standing message; an empty list renders `""`.
- **Fence:** `tasklist.Fence` is the header's literal opening, and `HeaderFormat` is built FROM it.
- **ADR number:** `0072` — `0071` is reserved by plan `2026-09-02 - 08`; numbers are never reused.
- **TUI label:** code spells `Task List`; `docs/layout/tool-layout.md`'s table is sentence-case throughout (`Git status`), so its row spells `Task list` and its Title-Case preamble list gains `Task List`.

**Standing requirements:** `skills: coding-standards`. Deviations land as a dated NOTES line.

**Out of scope:** any config key · any bench arm · pruning/compaction changes · a TUI surface for the human to edit the list · sharing a list across delegations · handoff items 1, 3, 5.

**Regression check (2026-09-03):** guards folded on all nine items; every site is located by heading
or anchor text, never by the line numbers cited. Revised same day after review: base re-pinned
`ac19410e`→`5bb33e92` (plan 07 is archived there, so item 1 now passes and is kept as a cheap
re-assertion); `CHANGELOG.md` dropped from item 8; the TUI oversize-note widening moved to item 7,
which owns `internal/tui/` and names the test pinning that note verbatim; items 3–9 sharpened where
an implementer could have gone two ways; brevity caps applied.

---

## 1. Verify plan `2026-09-02 - 07` is archived — ✅ DONE (2026-09-03)

NOTES (2026-09-03): gate PASSED at HEAD `94c36e88`. All acceptance checks green — archived plan file present; `git status --short -- internal/agent/ docs/adr/ CONTEXT.md docs/manual/configuration.md` printed nothing; `git ls-files internal/agent/delegatereport.go` printed the path (its `_test.go` is committed too); `grep -c delegateReportFence internal/agent/contextfiles.go` = 1; `grep -c 2026-09-02 docs/adr/0023-*.md` = 1; `grep -in "delegate report" CONTEXT.md` matched lines 833 and 1237 (the latter stating the composition order `prompt → Orientation block → Delegate report block …`); `grep -in "delegate report" docs/manual/configuration.md` matched line 1145.
NOTES (2026-09-03): no CHANGELOG entry — the item changes nothing user-visible.

**What.** This plan composes a fifth part into `standingSystem()`, adds a fourth entry to
`forgesStandingStructure`, extends `withOrientation`, and states a five-part order in prose. Plan
`docs/plans/2026-09-02 - 07 - delegate-report-block-plan.md` owns the fourth part of all four. It
was archived at `5bb33e92`, so this gate is expected to PASS at HEAD; re-assert it rather than
assume it, because a run starting from a different tree would collide. Confirm the plan is under
`docs/plans/archived/`, that nothing plan 07 owns is uncommitted, and that
`internal/agent/delegatereport.go` and its test are committed. On any failure STOP and report — do
not re-derive plan 07's work here. The clean-tree check is SCOPED to plan 07's paths, never the
whole tree: this plan's own document is untracked until the owner commits it, and an unscoped `git
status --short` would fail the gate on that alone.

**Regression guard.** The gate covers plan 07's item 3 as well as its archival — `docs/adr/0023-…`
must carry a dated 2026-09-02 addendum, and `CONTEXT.md` and `docs/manual/configuration.md` must
state the composition order INCLUDING the delegate report block. Items 6 and 8 EXTEND those edits.

**Files:** none (verification only).

**Tests.** None.

**Acceptance.** `test -f "docs/plans/archived/2026-09-02 - 07 - delegate-report-block-plan.md"`;
`git status --short -- internal/agent/ docs/adr/ CONTEXT.md docs/manual/configuration.md` prints
nothing; `git ls-files internal/agent/delegatereport.go` prints the path;
`grep -c "delegateReportFence" internal/agent/contextfiles.go` is ≥ 1;
`grep -c "2026-09-02" docs/adr/0023-*.md` is ≥ 1; `grep -in "delegate report" CONTEXT.md` and
`grep -in "delegate report" docs/manual/configuration.md` each print a line.

**Commit:** none — this item gates, it does not change the tree.

---

## 2. `internal/tasklist` — the list, its render, and its ctx carrier — ✅ DONE (2026-09-03)

NOTES (2026-09-03): `Replace` counts an over-long text in runes (`utf8.RuneCountInString`), so `MaxTextChars` measures characters as the plan words it rather than bytes; the refusal names the task's position in the CALLER's array, not among the kept rows.
NOTES (2026-09-03): no `docmap_test.go` added — the package holds three non-test files, well under the ~10-file threshold the standard sets, and its sibling carriers (`internal/undo`, `internal/console`) carry none either.

**What.** New package `internal/tasklist`, pure policy: no I/O, no events, no config. `Item{Text
string; Done bool}`, json tags `text` / `done,omitempty`. `List` holds `[]Item`, is guarded by a
`sync.Mutex`, and its zero value is an empty list. `New() *List` returns an empty one — the
`undo.New()`/`console.New()` shape item 4 constructs through. `Items() []Item` returns a copy taken
under the mutex, the reader `encodeState` marshals. `Replace(items []Item) error` is the ONLY
mutator: it trims each `Text`, drops empty ones, and returns a descriptive error when the list
exceeds `MaxItems = 40` or any text exceeds `MaxTextChars = 200`; on error the held list is
UNCHANGED. `Render() string` is `""` for an empty list, else `fmt.Sprintf(HeaderFormat, open, done)`
followed by one `"[✔] "`/`"[ ] "` row per item, joined `"\n"`. Declare `Fence = "Task list — "` and
`HeaderFormat = Fence + "yours to maintain; call task_list with the COMPLETE list to update it (%d
open, %d done):"` so the fence cannot drift from the header. `WithList`/`FromContext` copy
`internal/undo/context.go` exactly: unexported key type, nil when absent. One deep module — tool,
block and snapshot all go through this package and none re-implements the render.

**Regression guard.** `New()` and `Items()` are declared HERE and nowhere else: item 4 is their only
caller and creates no API of its own.

**Files:** `internal/tasklist/tasklist.go`, `internal/tasklist/context.go`,
`internal/tasklist/doc.go`, `internal/tasklist/tasklist_test.go`,
`internal/tasklist/context_test.go`

**Tests.** Replace round-trips; a shorter replace drops the tail; over-cap count and over-long text
each error AND leave the previous list intact; empty text is dropped; `Render()` is `""` when empty
and, on a mixed list, exactly the expected string including `[✔]` and `(1 open, 2 done)`;
`strings.HasPrefix(Render(...), Fence)`; `New()`'s `Render()` is `""`; mutating the slice `Items()`
returns does not change the list; `FromContext` of a bare context is nil; carrier round-trips.

**Acceptance.** `go test ./internal/tasklist/`; `go vet ./internal/tasklist/`;
`grep -c "✔" internal/tasklist/tasklist.go`, `grep -c "func New() \*List"
internal/tasklist/tasklist.go` and `grep -c "func (l \*List) Items()"
internal/tasklist/tasklist.go` are each ≥ 1.

**Commit:** `feat(tasklist): the model-owned checklist, its render and its context carrier`

---

## 3. The `task_list` tool — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the schema is built with `fmt.Sprintf` from `tasklist.MaxItems` and
`tasklist.MaxTextChars` rather than hard-coded numbers, so the caps the model is told and the caps
`Replace` enforces cannot drift; the item said "caps inline", which this still is.
NOTES (2026-09-03): `internal/tools/doc.go`'s file-count word was bumped Twenty-nine → Thirty as the
item asks, but the count was already stale before this item (the package's tool files number more
than either figure) — pre-existing drift, left alone.

**What.** `internal/tools/task_list.go` on `console_read.go`'s anatomy: a spec, an args struct, a
stateless `TaskList`, `NewTaskList()`, `ReadOnly() bool { return true }`, both interface assertions;
NOT a `DefaultOffTool`. `Execute` checks ctx, `decodeToolArgs`, reaches `tasklist.FromContext(ctx)`
— `errorResult` when nil, on `console_open.go`'s "consoles are not available in this session"
wording — calls `Replace`, `errorResult` on its error, else `okResult(list.Render())`. Schema: one
required `tasks` array of `{text (required, string), done (bool)}`, each property described, caps
inline. The description caps the discriminating fact — the call REPLACES the whole list rather than
appending — and says the list is "kept in front of you", NEVER "shown in every request" (the
ride-along rule). Register in `builtinTools()` immediately AFTER `NewSubAgentWith(...)` and BEFORE
`NewConsoleOpen(...)`, the last default-ON slot. Update `internal/tools/doc.go`'s file map and
file-count word, and every golden in `registry_test.go`: the default count plus the comment
itemising the 25 (`:34-36`); `:429`'s count AND its "the 25 it held before the Console family"
message; `:86`/`:109` (26→27); `:115` (27→28); the ordered menu list (`:52-62`) gains `"task_list"`
last; and `TestDefaultTools_DeclareReadOnlyNature`'s map gains `"task_list": true` (a missing row
silently compares against `false`).

**Regression guard.** Registration puts `task_list` in `KnownToolNames()`, so
`TestManualListsEveryKnownToolName` is red until the manual names it: this item also inserts the
name `task_list` into the manual's menu-order roster list ("The names this build knows are fixed"),
after `sub_agent`, before the Console four.

**Files:** `internal/tools/task_list.go`, `internal/tools/task_list_test.go`,
`internal/tools/registry.go`, `internal/tools/doc.go`, `internal/tools/registry_test.go`,
`docs/manual/configuration.md`

**Tests.** A replace returns the rendered list; a nil carrier is an error result, not a Go error; a
cancelled ctx IS a Go error; over-cap input is an error result; `TestTaskList_SpecIsModelFacing` on
`TestPresentDocument_SpecIsModelFacing`'s shape pins the name, asserts the description names
`REPLACES`, unmarshals `Schema()` and checks `required` and both item properties;
`TestTaskList_IsRegisteredAndReadOnly`.

**Acceptance.** `go test ./internal/tools/ -run 'TaskList|DefaultRegistry|DefaultTools|KnownToolNames|DocMap|ManualLists'`; `go build ./...`.

**Commit:** `feat(tools): task_list, the model-owned checklist tool`

---

## 4. The engine holds the list, and a child gets its own

**What.** `Agent` gains `tasks *tasklist.List`, constructed non-nil in `construct.go` beside
`undo.New()`/`console.New()`, with the ADR 0008 rationale beside it (`agent.go`'s `consoles` comment
is the model): the tool may not hold it, because `SwapTools` rebuilds tool instances mid-session.
Install it for EVERY call in `dispatch.go` beside `undo.WithJournal`/`console.WithRegistry`: `ctx =
tasklist.WithList(ctx, a.tasks)`. `agentState` gains `Tasks []tasklist.Item` under the json tag
`tasks,omitempty`, written in `encodeState` from `a.tasks.Items()`; `restoreState` sets it
through `a.tasks.Replace(st.Tasks)` — validated, so an over-cap snapshot is a decode error rather
than a silently truncated list, and `Replace` of a nil slice is the clear. `omitempty` makes this
additive both ways, so `domain.SessionVersion` stays 1 (the `session.Meta` ScheduleID precedent) and
this item does not touch it. Reset the list in `Agent.ClearContext` beside the console close. In
`newChildAgentOn` set `child.tasks = tasklist.New()` EXPLICITLY beside the
`child.journal`/`child.consoles` lines, commenting that the ratified call is a fresh empty list and
THIS LINE is the guarantee, not `tasks`' absence from `Config`.

**Regression guard.** `RestoreSession` restores FIRST and resets after (`restoreSnapshot`, then
`a.consoles.CloseAll()`), so a reset beside that close would wipe the list just restored: there is
none — `restoreState`'s unconditional `Replace` IS the clear. And the key-name pin gains a non-zero
`Tasks` and the `tasks` key, or its stated contract goes false.

**Files:** `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/dispatch.go`,
`internal/agent/state.go`, `internal/agent/subagent.go`, `internal/agent/state_test.go`,
`internal/agent/tasklist_test.go`

**Tests.** A dispatched call reads a non-nil list from its ctx at depth 0 and at depth 1; a snapshot
round-trips a three-item list; a snapshot taken with an empty list omits the key — unmarshal
`snap.State` to `map[string]json.RawMessage` and assert no `tasks` key, never a substring grep; a
restore from a snapshot with no `tasks` leaves the list empty; an over-cap snapshot errors;
`ClearContext` empties it; a child from `newChildAgent` has its own empty list, the parent's is
unchanged, and replacing the child's does not touch it; `TestAgentState_EncodesStableKeyNames`
marshals a non-zero `Tasks` and asserts the `tasks` key.

**Acceptance.** `go test ./internal/agent/ -run 'TaskList|Snapshot|Restore|ClearContext|SubAgent|AgentState'`; `go build ./...`.

**Commit:** `feat(agent): the task list is engine session state, carried on the call context`

---

## 5. The standing block

**What.** New `internal/agent/tasklistblock.go`: `func (a *Agent) taskListBlock() string` returning
`a.tasks.Render()` (`""` when empty). Its doc comment records why it rides along, why it precedes
the context files (ADR 0023's 2026-08-26 argument), and that it is the first ENGINE-OWNED block
whose content changes within a session, invalidating the prefix cache on every rewrite. Compose it
in `standingSystem()` (`internal/agent/loop.go`) as the fourth `parts` append — after
`delegateReportBlock()`, before `blocks` — raising `parts` capacity to 5, and extend the
`buildRequest` wire-order and `standingSystem` doc comments to name five parts. Add `tasklist.Fence`
as `forgesStandingStructure`'s fifth clause (`internal/agent/contextfiles.go`) — a CLOSED list every
engine-owned block belongs on. Name the new file in `internal/agent/doc.go` and fix its count.
Declare `const TaskListFence = tasklist.Fence` beside the block and re-export it as
`apogee.TaskListFence` next to `DelegateReportBlock`, keeping the facade's single `internal/agent`
import (ADR 0010). Extend `withOrientation` (`internal/agent/orientation_test.go`) with the new
part.

**Regression guard.** `ContextFilesReport` also measures `standingSystem()`, so the list now costs
`StandingTokens`; item 7 widens the TUI's oversize-note remedy clause to name it. Leave
`TestContextFilesReportMeasuresStandingContent` byte-identical here: its agent holds no list.

**Files:** `internal/agent/tasklistblock.go`, `internal/agent/loop.go`,
`internal/agent/contextfiles.go`, `internal/agent/doc.go`, `apogee.go`,
`internal/agent/tasklistblock_test.go`, `internal/agent/orientation_test.go`

**Tests.** With a prompt and a non-empty list, `standingSystem()` places the block after the
delegate block, before the context-file header, and the whole string equals `withOrientation(...)`;
changes nothing; with no prompt AND no context files it is `""` and the wire carries zero system
messages even with a non-empty list (the ride-along anchor); an `AGENTS.md` spelling
`tasklist.Fence` is fenced with `workspaceTextPrefix`, the engine's block still first.

**Acceptance.** `go test ./internal/agent/ -run 'TaskList|Orientation|PromptSeam|ContextSeam|ContextFilesReport|DocMap'`; `go build ./...`.

**Depends on** items 2 and 4.

**Commit:** `feat(agent): the task list rides as a standing block ahead of the context files`

---

## 6. ADR 0072, the ADR 0023 addendum, and the reversed denial

**What.** Write `docs/adr/0072-the-task-list-is-model-owned-session-state.md` with front matter
`Status: accepted` and `Amends: ADR 0023 (the 2026-08-25 per-session-constant bullet); ADR 0057
decision 3 is untouched` — `docs/adr/0070-…`'s header is the shape. Its `## Decision` carries seven
bold-numbered `**N — …**` paragraphs, the house record shape ADR 0059 and ADR 0069 spell: (1) the
list is the model's, written only through `task_list`, never by human or engine; (2) whole-list
replace — an id the model must track across turns is the failure mode the tool removes; (3) Session
state on `agentState`, not live host state — ADR 0022 §8's line for Consoles falls the other way
here, because a checklist whose value is surviving a long run must survive `--resume`; (4) it
renders as a standing block ahead of the context files, and it is the first ENGINE-OWNED standing
block whose inputs are not per-session constants (the rendered template already varies with its live
`{{mode}}`), so ADR 0023's 2026-08-25 amendment is amended to admit an engine block whose volatility
is under the MODEL's control, at the cost of a prefix re-encode per change; (5) `ReadOnly`, because
`IsReadOnly` is a blast-radius axis (`ask_user`, `console_close`); (6) default-on with no config
key, off via `tools.disabled:`; (7) a delegation gets its own empty list. Add a dated addendum to
`docs/adr/0023-…md` naming ADR 0072, restating the order as five parts and stating the volatility
exception. Reverse the "Task/todo persistence" denial in `docs/design/tool-surface-findings.md` in
place: keep the dated record and append that the owner reversed it on 2026-09-02 (handoff parked
item 2) — a plain tool the model owns is not guided decomposition — with the ADR 0072 link.

**Regression guard.** Locate the ADR 0023 insertion point by the heading of its LAST dated record,
never by a line number — plan `2026-09-02 - 07` item 3 appended a 2026-09-02 addendum there.

**Files:** `docs/adr/0072-the-task-list-is-model-owned-session-state.md`,
`docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`,
`docs/design/tool-surface-findings.md`

**Tests.** None (records).

**Acceptance.** `test -f docs/adr/0072-*.md`; `grep -c "^\*\*[1-7] —" docs/adr/0072-*.md` is ≥ 7;
`grep -n "^Amends:" docs/adr/0072-*.md` prints a line; `grep -c "0072" docs/adr/0023-*.md` is ≥ 1;
`grep -n "0072" docs/design/tool-surface-findings.md` prints a line.

**Commit:** `docs(adr): ADR 0072 — the task list is model-owned session state`

---

## 7. The TUI presents the call

**What.** One `toolRegistry` entry for `task_list` in `internal/tui/toolregistry.go`: `label: "Task
List"`, `verb: "updating the task list"`, no `target` (the list is the target, as the `git_status`
entry has none), `detail: outputDetail` so the rendered list becomes the branch row plus body, and a
`stat` that derives the open count from the result — count the lines of `res.Content` carrying the
`"[ ] "` prefix and return `countedStat(n, "open"), true`; an error result, or content with no rows,
returns `false` so the outcome slot falls back. No `contentArgs` row: the tasks array is the call's
whole point and belongs in the session record. Add the matching row to
`docs/layout/tool-layout.md`'s display table — sentence-case throughout (`Git status`, `Git log`),
so the row reads `Task list` — and add `Task List` to that file's preamble list of labels shipped
Title Case (`Diff Preview`, `Find Files`, `Git Status`, `Ask User`, `Sub-Agent`). Item 5 makes the
task list count toward `StandingTokens`, so widen the oversize-note remedy clause in
`internal/tui/model.go` (`") — trim context files or the system prompt"`) to name the task list, and
update the exact string `TestContextFilesNoticeBudgetWarn` pins in
`internal/tui/contextfiles_test.go` — that note's wording is asserted verbatim there.

**Regression guard.** Build the stat with `countedStat(n, "open")` (`internal/tui/toolview.go`), the
constructor every other counted stat uses: a bare `statValue` literal leaves `kind` at `statPlain`,
so `spell()` returns `""` and the outcome slot renders the table's `—`. The label deviation from the
table is settled by that file's preamble, which this item extends rather than contradicts.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolregistry_test.go`,
`internal/tui/model.go`, `internal/tui/contextfiles_test.go`, `docs/layout/tool-layout.md`

**Tests.** `presentToolCall` on a `task_list` call yields the label and verb, not the raw-name
fallback; the result body carries the `[✔]` row unnumbered (the `TestFileContentBodiesAreNumbered`
walk already covers every entry, so assert the new entry passes it); the outcome slot reads the open
count; an error result yields no counted stat; the oversize note names the task list.

**Acceptance.** `go test ./internal/tui/ -run 'ToolPresent|ToolRegistry|FileContentBodies|ToolSummaryPin|ContextFilesNotice|DocMap'`; `grep -c "task list" internal/tui/model.go` is ≥ 1.

**Depends on** items 3 and 5.

**Commit:** `feat(tui): present the task_list call`

---

## 8. The tool reaches every doc that enumerates the roster

**What.** Item 3 has already put the name `task_list` in the manual's menu-order roster list
(enforced by `internal/tools/manual_drift_test.go`); this item writes a short section in
`docs/manual/configuration.md`, headed for the task list and naming the tool, on `## The Console
family`'s shape: what the tool is, that it replaces rather than appends, that the current list rides
in the standing system content, that it is on by default and `tools.disabled: [task_list]` turns it
off, and that a delegation keeps its own. Define the term in `CONTEXT.md`'s `### Context and
history` section beside `**Tool summary**`, on the `**Console**` entry's template: what it is, its
tool, its state class, its ADR link, an `_Avoid_:` line. **Two rules, not lists.** (a) Every live prose sentence stating the
standing-content ORDER gains the task list in that sentence's own wording, never one canonical
string pasted over several sites — find them with `grep -n "→ context files\|→ Mechanism
directives\|orientation block" CONTEXT.md docs/manual/configuration.md` (today: three in
`CONTEXT.md`, the orientation/delegate prose in the manual's system-prompt section). (b) Every live
prose site stating the roster SIZE goes up by one — `grep -rn "[0-9]\+-tool" README.md CONTEXT.md
AGENTS.md docs/ internal/ --include=*.md --include=*.go | grep -v archived` — correcting each hit
that counts THIS roster (today: `README.md`'s "29-tool"). A dated measurement of a different roster
(ADR 0018's "19-tool menu") is not a hit; `internal/tools/doc.go`'s count word is a FILE count owned
by item 3.

**Regression guard.** Locate every site by heading or anchor text, never by a line number — plan
`2026-09-02 - 07` item 3 edited both files and shifted them. `CONTEXT.md`'s order sentences already
name the delegate report block, so this item INSERTS the task list into each; it never rewrites one
wholesale.

**Files:** `docs/manual/configuration.md`, `CONTEXT.md`, `README.md`, any file rule (b)'s grep names

**Tests.** None beyond the drift test.

**Acceptance.** `go test ./internal/tools/ -run 'ManualLists'`;
``grep -c '`task_list`' docs/manual/configuration.md`` is ≥ 2; `grep -n "Task list" CONTEXT.md`
prints a line; `grep -n "30-tool" README.md` prints a line.

**Depends on** item 3.

**Commit:** `docs: the task list joins the roster, the manual and CONTEXT`

---

## 9. End to end: the list reaches the wire and survives a resume

**What.** New `cmd/apogee/e2e_tasklist_test.go` plus `cmd/apogee/testdata/stubllm/tasklist.yaml`,
following `docs/design/test-drivers.md`'s "Writing a new e2e test" checklist. Every turn matches on
`when:` with `repeat: true`: a title turn above the prompt's own, a turn calling `task_list` with
three items (one done), a `when: {tool_result: task_list}` turn replying in prose, and a trailing
catch-all, with `AssertConsumed` proving the scripted turns fired. Drive it in-process through
`launchTUIConfigured` with a `system-prompt-text:` of its own — the block rides along, so a fixture
seeding nothing proves nothing (`e2e_seat_test.go`'s `seatHome` is the precedent). Assert on the
STUB'S REQUEST LOG, not a fixture-internal spelling: the request following the tool result carries a
system message containing `apogee.TaskListFence` and the exact rendered row `[✔] wire the parser
seam` that `internal/tasklist` emits. Then `sess.RelaunchWith("--continue")` and SEND A SECOND
PROMPT — a resumed session issues no request until asked something — and assert that request still
carries the same row: the persistence claim, end to end. Finally assert the transcript shows the
presented card's label.

**Regression guard.** Follow the checklist: every turn on `when:` with `repeat: true`, a title turn
(`when: {last_message: <the prompt>}`) ABOVE the prompt's own — apogee asks for a session title off
the first prompt — and a trailing catch-all, else the `--continue` run's request matches nothing and
stubllm answers HTTP 500. The relaunch is `sess.RelaunchWith("--continue")`: `e2eSession.Relaunch()`
takes no arguments (`cmd/apogee/e2e_support_test.go`).

**Files:** `cmd/apogee/e2e_tasklist_test.go`, `cmd/apogee/testdata/stubllm/tasklist.yaml`

**Tests.** The file IS the test: `TestE2ETaskListReachesTheWireAndSurvivesAResume`.

**Acceptance.** `go test ./cmd/apogee/ -run 'TestE2ETaskList' -count=1`.

**Depends on** items 5, 7 and 8.

**Commit:** `test(e2e): the task list reaches the wire and survives a resume`

---

**Suggested version bump:** minor — a new tool, a new standing block and a new `agentState` field
are additive engine surface (ADR 0001 §consequences). The owner decides; no item touches `VERSION`.
