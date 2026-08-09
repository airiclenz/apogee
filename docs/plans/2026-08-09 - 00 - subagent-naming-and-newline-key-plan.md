# Sub-agent naming + shift+enter regression — implementation plan

**Goal:** a delegation can carry an optional, model-supplied short name that identifies what
the child does, visible in the session chat (collapsed run header, status line, approval/ask
prompts, headless run records); and the shift+enter newline regression in the prompt editor
is hardened against, pinned to its culprit, and fixed.

- **Date:** 2026-08-09 · **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - TODO.md — the *apogee-code feature parity* entry's Remaining list, "Naming Sub-Agents" bullet (closed by item 4).
  - ISSUES.md — the shift+enter line (closed by item 6).
  - [ADR 0039](../adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md) decisions 5–6 (call-ID identity; one block per child; the approval prompt names the asking child's task).
  - [ADR 0005](../adr/0005-sub-agent-privileges-are-bounded-by-the-parent.md) (a child starts fresh with only the delegated task — the name is display identity, never privilege).
  - CONTEXT.md "Sub-agent" (~line 103) and "Parallel agents" (~line 134).
  - layout.md: concurrent delegates get one block each (~840–854), the collapsed sub-agent header (~810–820), the approval/ask prompt body (~315–333), the prompt legend (~1487).
  - Scout facts pinned 2026-08-09 (file:line refs throughout this plan date from that pass; re-locate by symbol if drifted).

**Ratified design calls (owner, 2026-08-09, via AskUserQuestion):**

1. **Name source = an optional `name` parameter on the `sub_agent` tool.** Absent name →
   every display falls back to the task's first line (today's behavior). Guided
   decomposition synthesizes no name — its delegations take the fallback.
2. **Surfaces = collapsed run header + status line + approval/ask prompts + headless run
   records.** The `⤷ sub-agent` rail label does **not** change — explicitly declined; do
   not touch `renderSubAgentLabel`.
3. **Status line with a named child reads `<name> · <phrase>`** — the name replaces the
   generic `sub-agent` word. Unnamed children keep `sub-agent · <phrase>` exactly as today.
4. **Shift+enter:** broke on macOS/Linux while alt+enter still works — so the terminal never
   delivered (or apogee never decoded) the disambiguated `CSI 13;2u`; the fix targets the
   keyboard-protocol handshake. Hardening (the adaptive legend, item 5) ships regardless of
   the culprit; the culprit is pinned by the two-build A/B in item 6.

**Standing requirements:** skills: coding-standards. Never change VERSION, the CHANGELOG
release heading, or any tag (see the closing suggestion note). Any authorized deviation from
item text lands as a dated NOTES line under the item.

**Out of scope:** sub-agent session persistence (a TODO.md deliberate non-goal); the
`⤷ sub-agent` label (declined, call 2); the keyboard collapse/expand ISSUES.md entry; any
Mechanism/catalogue change; persisting the raw name on the transcript wire beyond what
`wireToolView.Target` already carries.

---

## 1. `sub_agent` accepts an optional delegation name — ✅ DONE (2026-08-09)

NOTES (2026-08-09): two additions beyond the item's literal file list. (a) The normalisation is a
named helper `delegationName` in `internal/agent/subagent.go` (first line, trimmed) rather than
inline in `runSubAgent`, so items 2–3 can point at one definition of the rule. (b) The name is
APPENDED to `newChildAgent` — `newChildAgent(spawnCallID, task, name)` — so the 20 existing test
call sites take a pure `, ""` append instead of an inserted middle argument. (c) A CHANGELOG entry
was added under `## [Unreleased]` → `### Added` per the repo convention; no release heading, no
VERSION touched.

**What:**

- `internal/tools/sub_agent.go` — the schema (`subAgentSpec`, :26–32) gains an optional
  string property `name` with a one-line description (keep it short — schema text is context
  cost for the 4B–35B target class), e.g. `"Optional short name for this delegation, shown
  in the UI."`. `required` stays `["task"]`. `SubAgentArgs` (:38–40) gains
  `Name string` with a `name` JSON tag.
- `internal/agent/subagent.go` — `runSubAgent` (:59–112) normalizes the name once at the
  boundary: trim whitespace, keep only the first line; empty after normalization = absent.
  `newChildAgent` (:141–188) takes the normalized name and stamps `child.name` beside
  `depth`/`callID`/`task` (:170–172). `internal/agent/agent.go` (:162–164) gains the
  `name string` field with a doc comment mirroring `task`'s ("display identity in words;
  empty = unnamed").
- `internal/mechanisms/guided_decomposition.go` (`guidedDecompositionTaskArgs`, :447–450) —
  **unchanged by decision** (ratified call 1): synthesized delegations carry no name.
- `CONTEXT.md` "Sub-agent" entry — add one sentence: a delegation may carry a model-supplied
  short name via the `sub_agent` call; every display falls back to the task's first line
  when it is absent.

**Tests:** the published schema contains `name` and does not require it; `runSubAgent`
stamps the normalized name on the child (multi-line and padded names collapse to a trimmed
first line; missing name → empty string); existing sub-agent tests stay green.

**Acceptance:** `go test ./internal/tools ./internal/agent -count=1` green;
`grep -n '"name"' internal/tools/sub_agent.go` shows the schema property.

**Commit:** `feat(tools): sub_agent accepts an optional delegation name`

## 2. The name rides approval, ask and run records — ✅ DONE (2026-08-09)

Depends on item 1.

NOTES (2026-08-09): three additions beyond the item's literal file list. (a) `internal/tools/ask_user.go`
stamps `SubAgentName: domain.SubAgentNameFromContext(ctx)` beside the task where it builds the
`AskRequest` — without it the new field has no producer at all (the ask_user tool is the only thing
that constructs an AskRequest), so "the name rides the ask record" would not be true and item 4's
named-ask prompt would have nothing to paint. (b) The ctx install in `dispatch.go` is
UNCONDITIONAL inside the existing `a.task != ""` guard — an unnamed child installs `""` rather than
skipping the install, so it reports its own namelessness instead of letting an outer value stand in.
(c) A CHANGELOG sub-bullet under the existing "A delegation can carry a name" entry, per the repo
convention; no release heading, no VERSION touched. Not done (not named by this item and not covered
by its acceptance command): `cmd/apogee/headless.go` still prints only the task on its sub-agent
line — the record carries the name, the headless RENDER of it does not yet use it.

**What:**

- `internal/domain/approval.go` — `ApprovalRequest` (:37–45) gains additive
  `SubAgentName string` beside `SubAgentTask` (doc comment: empty = unnamed; display only).
- `internal/domain/ask.go` — `AskRequest` (:61–71) gains additive `SubAgentName string`;
  add `WithSubAgentName` / `SubAgentNameFromContext` parallel to the existing task helpers
  (:77–96).
- `internal/agent/dispatch.go` — set the new fields beside the task: the approval fill
  (~:560) and the ask-context install (~:626), both from the child agent's `name`.
- `internal/run/run.go` — `SubAgentUsage` gains additive `Name string`;
  `openSubAgentRun`/`closeSubAgentRun` (:393–418) fill it by parsing the `name` arg beside
  `firstTaskLine` (:424–436), normalized the same way as item 1 (first line, trimmed).
- Public-surface note: all three are additive fields on existing types — no renames, ADR
  0010 placement untouched.

**Tests:** an approval request raised from a named child carries `SubAgentName`; the ask
context round-trips the name; a headless run's `SubAgentUsage` records the name for a named
delegation and empty for an unnamed one.

**Acceptance:** `go test ./internal/agent ./internal/run ./internal/domain -count=1` green.

**Commit:** `feat(agent): the delegation name rides approval, ask and run records`

## 3. The collapsed run header shows the name — ✅ DONE (2026-08-09)

Depends on item 1.

NOTES (2026-08-09): three additions beyond the item's literal text. (a) The registry's target became two
named helpers in `internal/tui/toolpresent.go` — `subAgentName` (the tool's own normalisation: trimmed
first line, `clipDetail`-capped) and `subAgentTarget` (name, else `firstLineArg("task")`) — rather than an
inline closure, so the presenter and item 4's lookup point at one definition of the rule; `presentToolCall`
stamps `tv.agentName = subAgentName(args)` under an explicit `call.Tool == subAgentToolName` branch (the
registry's `target` signature returns only a string and cannot set a second field). (b) `agentName` is added
to `toolView.sanitize` — it is painted display text like `Target`, and the item's "escape-stripped" is not
otherwise true of the field item 4 reads. (c) A CHANGELOG sub-bullet under the existing "A delegation can
carry a name" entry, per the repo convention; no release heading, no VERSION touched.

**What:**

- `internal/tui/toolpresent.go` — the `sub_agent` registry entry (:443–448): the target
  becomes the `name` argument when present (escape-stripped, `clipDetail`-capped, same
  treatment as `firstLineArg` output), else `firstLineArg("task")` exactly as today. Label
  `"Sub-Agent"` and verb `"delegating"` unchanged.
- The presenter also records the normalized name on `toolView` as a new unexported field
  (e.g. `agentName`) — consumed by item 4's status lookup. It is **not** added to the wire
  form: the status line is live-only, and the persisted header display already rides
  `wireToolView.Target`.
- `layout.md` collapsed-header paragraph (~810–820): the branch's target slot is the
  delegation's name when one was given, else the delegated task's first line.

**Tests:** presenter unit tests for a named and an unnamed `sub_agent` call (target = name
vs. first task line); a transcript-codec round-trip test that a named head's `Target`
survives encode/decode and that `wireEntry`/`wireToolView` gained no new members; existing
collapsed-header paint tests stay green.

**Acceptance:** `go test ./internal/tui -count=1` green;
`grep -n 'agentName' internal/tui/transcriptcodec.go` returns nothing.

**Commit:** `feat(tui): the sub-agent run header shows the delegation name`

## 4. The status line and the prompts name the asking child — ✅ DONE (2026-08-09)

Depends on items 1, 2, 3.

NOTES (2026-08-09): four additions beyond the item's literal text. (a) The status seam chosen (the item
left it to the implementer) is a parameter: `activity.text(name string)`, with `runningPhrase`
resolving `m.transcript.runName(m.act.spawn)` per frame — activity.go stays pure and there is one
definition of the prefix rule. This widened two existing signatures, so `setActivity` gained a fourth
`spawn string` parameter and its 5 production call sites (all top-level, `""`) plus the `text()`/
`setActivity` call sites in `activity_test.go`, `fold_test.go` and `interject_test.go` take a
mechanical append. (b) The two prompt bodies share one new helper `subAgentPromptLine(name, task)` in
`model.go` rather than each composing the line inline — the item requires the two panes to read
identically, and the ask body's own doc comment already says a dialect there would be a defect. (c) A
CHANGELOG sub-bullet under the existing "A delegation can carry a name" entry, per the repo
convention; no release heading, no VERSION touched. (d) Not done (not named by this item, and item 2's
own NOTES already flagged it): `cmd/apogee/headless.go` still prints only the task on its sub-agent
line.

**What:**

- `internal/tui/activity.go` — `activity` (:48–54) gains `spawn string`; `foldActivity`
  (:196–224) passes the emitting event's spawning call-ID alongside `Depth`. Mind the
  `AuditEvent` shadowing: the spawn id is `ev.EventBase.CallID`
  (`internal/domain/events.go:177–188`).
- `internal/tui/transcript.go` — a lookup `runName(spawn string) string`: find the run-head
  entry (`entryToolCall`, `callID == spawn`, `tool.name == "sub_agent"`, reusing the
  `runEnd` scan shape :272–284) and return its `agentName` (empty when unnamed).
- Status compose (`activity.text` :59–84, `statusLeft`/`runningPhrase`
  `model.go:3916–3968`): for `depth > 0`, prefix = the resolved name when non-empty, else
  `subAgentLabel` — i.e. `<name> · reading · main.go` vs. today's
  `sub-agent · reading · main.go` (ratified call 3). The exact seam for handing the name to
  the composer (parameter on `text` vs. composition in `statusLeft`) is the implementer's
  mechanical choice; the rendered behavior above is binding.
- Prompts: the approval body (`model.go:~4402`) and the ask body (`model.go:~4636`) — when
  `SubAgentName` is non-empty the line reads `Sub-agent: <name> — <task clip>` (name
  escape-stripped; the whole line still clipped to the existing rune budget); unnamed lines
  are byte-identical to today.
- `layout.md`: the status-line concurrency sentence (~848–850 — the line can still name only
  one delegate at a time, now by its name when given) and the prompt-body paragraph
  (~315–333) amended.
- **TODO.md close-out (this item owns it):** delete the "Naming Sub-Agents" bullet from the
  parity entry's Remaining list and add a one-line trail row under **Closed entries** naming
  this plan.

**Tests:** a model-level test drives events from a named child and asserts the status line
reads `<name> · responding` (and an unnamed child keeps `sub-agent · responding`); prompt
tests for a named approval and a named ask; existing activity/status tests stay green.

**Acceptance:** `go test ./internal/tui -count=1` green; `grep -c 'Naming Sub-Agents'
TODO.md` returns 0 in the Remaining list (the Closed-entries trail line may mention it).

**Commit:** `feat(tui): status line and prompts name the asking delegation`

## 5. The newline legend follows the negotiated keyboard protocol — ✅ DONE (2026-08-09)

No dependencies.

NOTES (2026-08-09): four additions beyond the item's literal text. (a) The negotiated flag is stored on
`promptEditor` (`keyDisambiguation`), not on the Model proper — the editor owns the legend and the two new
seams touch only its own fields (the partial-lift rule in prompteditor.go): `idleLegend()` resolves which
form to paint and `setKeyDisambiguation(bool)` records the answer and swaps an already-painted idle legend
in place. It still reads as `m.keyDisambiguation`/`m.idleLegend()` (anonymous embedding), and model.go's new
`tea.KeyboardEnhancementsMsg` arm is what calls the setter, per the item. Consequence: the two
`m.setPlaceholder(idlePlaceholder)` call sites in `model.go` (the ask borrowing the box, and the return to
idle) now pass `m.idleLegend()`. (b) The legend is two constants: `idlePlaceholder` KEEPS its name and
becomes the not-yet-negotiated `⌥⏎`-only form, `idleShiftPlaceholder` is the `⇧⏎/⌥⏎` form — so
`interject_test.go`'s existing placeholder assertions stay correct untouched (nothing there negotiates).
(c) Tests landed in two files: an editor-direct one in `prompteditor_test.go` (default, both flips, and the
running legend left alone) and the Model-level one in `model_test.go` beside the untouched
`TestModelNewlineKeysInsertLineBreak`. (d) A CHANGELOG entry under `## [Unreleased]` → `### Changed` per the
repo convention; no release heading, no VERSION touched. The accessor on bubbletea v2.0.8 is
`KeyboardEnhancementsMsg.SupportsKeyDisambiguation()` (`Flags > 0`), verified against the module source.

**What:** shift+enter needs the kitty/enhanced keyboard protocol; today the legend
advertises `⇧⏎` unconditionally and apogee never learns what the terminal negotiated
(no `tea.KeyboardEnhancementsMsg` handling anywhere — verified 2026-08-09).

- `internal/tui/model.go` — handle `tea.KeyboardEnhancementsMsg` in `Update` (beside the
  existing `tea.KeyPressMsg` arm, ~:573): store whether key disambiguation was negotiated
  (check the exact accessor on bubbletea v2.0.8's message type).
- `internal/tui/prompteditor.go` — the legend (:90) becomes a function of that flag:
  negotiated → today's `⏎ send · ⇧⏎/⌥⏎ newline · …`; not (yet) negotiated →
  `⏎ send · ⌥⏎ newline · …`. Startup default = not negotiated: kitty-capable terminals
  confirm within the first frames, and the pessimistic default is the honest one on
  terminals that never will. (`ctrl+j` stays a working, undocumented fallback.)
- Docs: one sentence each in `README.md` (~:265) and `layout.md` (~:1487): the `⇧⏎` chord
  is advertised only on terminals that negotiated the enhanced keyboard protocol.

**Tests:** a synthetic `KeyboardEnhancementsMsg` flips the rendered legend both ways; the
default (no message) shows the `⌥⏎`-only variant; `TestModelNewlineKeysInsertLineBreak`
(`model_test.go:528–554`) is untouched and green.

**Acceptance:** `go test ./internal/tui -count=1` green.

**Commit:** `feat(tui): the newline legend follows the negotiated keyboard protocol`

## 6. Pin and fix the shift+enter regression — ✅ DONE (2026-08-09)

Depends on item 5.

NOTES (2026-08-09): outcome = the pre-decided **Neither** branch, on the owner's check against a real
terminal — so the two-build A/B was moot and was NOT performed: the alt-screen pre-claim
(`claimTerminalScreen`) stands untouched and `go.mod`/`go.sum` are byte-unchanged (no ultraviolet pin).
Owner-confirmed: shift+enter inserts a newline on the affected terminal, normally. The failure was
load behavior, not an apogee regression — the keypress was simply not recognized while a local LLM was
saturating the host GPU (100% utilization). Environment: VS Code's integrated terminal, SSH'd from VS
Code into a Mac container, with the LLM running on the Mac host. Item 5's adaptive legend is the
standing answer, so this item changes no code: its whole deliverable is the ISSUES.md close-out (that
line now reads `[X]` with the outcome clause) plus this record. Consequently no CHANGELOG entry — no
user-facing behavior changed here beyond what item 5 already recorded.

**What:** the dispatch layer is provably intact (the existing model-level test passes; the
`handleKey` fall-through to the textarea is unchanged since the 2026-07-19 baseline), so the
break is below it — bytes-to-KeyPressMsg. Two suspects (scout, 2026-08-09):

- `76efa09` (2026-08-03) — apogee pre-claims the alt screen with raw
  `\x1b[?1049h\x1b[3J` **before** bubbletea's disable→alt-screen→enable keyboard
  handshake (`internal/tui/tui.go:1089–1095`, `:1270–1273`).
- `8cdbbf8` (2026-08-06) — dependency bump; the operative half is ultraviolet
  `20260525132238 → 20260803092147` (the key decoder; bubbletea 2.0.7→2.0.8 is
  source-identical).

**A/B recipe (owner-run — this machine has no TTY).** When this item executes in an
environment without a real terminal, the implementer performs any preparatory work, then
returns STATUS BLOCKED carrying this recipe verbatim; the outcome arrives through the
consult and re-enters as the DECISION line:

1. Build A — comment out the `claimTerminalScreen` call (`tui.go:1089`),
   `go build -o /tmp/apogee-A ./cmd/apogee`, restore the file.
2. Build B — `git show 8cdbbf8~1:go.mod | grep ultraviolet` to get the old pseudo-version;
   `go get github.com/charmbracelet/ultraviolet@<that version> && go mod tidy`;
   `go build -o /tmp/apogee-B ./cmd/apogee`; restore `go.mod`/`go.sum`.
3. On the affected macOS/Linux terminal, try shift+enter in each build.

**Fix per outcome (all pre-decided — no open call):**

- **A fixes it** → remove the raw pre-claim and re-achieve its intent through bubbletea
  (the view already sets `AltScreen`; carry any scrollback-clear into the first-frame
  path). Binding constraint: apogee writes no screen-switching sequences before
  bubbletea's keyboard handshake. Read `76efa09`'s commit message first and re-verify its
  original symptom does not return.
- **B fixes it** → pin ultraviolet at the pre-bump pseudo-version with a dated `go.mod`
  comment naming the ISSUES entry; record the upstream defect (the two pseudo-versions as
  the repro) in a dated NOTES line under this item. Un-pinning later is its own change.
- **Neither** → apogee did not break it (the terminal app changed underneath); close the
  ISSUES line as terminal-behavior, with item 5's adaptive legend as the standing answer,
  and record the terminal name+version in a NOTES line.

**ISSUES.md close-out (this item owns it):** mark the shift+enter line `[X]` with one
clause naming the outcome.

**Tests:** `go test ./internal/tui -count=1` green on whichever branch lands; if branch A
lands and a cheap unit seam exists, pin that no screen-switch bytes are written before
program start.

**Acceptance:** a dated NOTES line under this item recording the culprit and the
owner-confirmed "shift+enter inserts a newline on the affected terminal"; `make check`
green.

**Commit:** `fix(tui): restore shift+enter newline on enhanced-keyboard terminals`
(adjust the subject to the branch that lands).

---

**Suggested version bump (not performed):** minor — a new user-facing feature (named
delegations) plus a model-facing tool-schema addition and a user-visible fix. The owner
decides whether and when.

**Owner-run residue:** item 6's A/B needs a real terminal; everything else verifies
in-repo.
