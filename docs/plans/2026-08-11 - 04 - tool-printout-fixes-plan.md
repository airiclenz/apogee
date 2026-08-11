# Tool print-out fixes — grouped sub-agent display & fold indicator

- **Goal:** fix six owner-reported tool print-out defects: fold-indicator gap, sub-agent
  details lost on expand, missing blank line before the delegated prompt, per-member done
  status in grouped sub-agents, a "scheduled" state for queued sub-agents, and removal of
  the flickering ongoing-action text.
- **Date:** 2026-08-11
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - Owner defect report 2026-08-11 (transcribed into the binding **What** lines below).
  - `docs/layout/tool-layout.md` — canon spec for tool blocks (`<tool-top-level-details>`
    vocabulary at :65-82, Grouped Sub-agents at :207-238, per-tool table `sub_agent` row
    at :276). Items amend it; where an item's What and the current spec text disagree,
    the item's What wins and the spec is updated to match.
  - ADR 0039 (parallel sub-agents) — decision 4 (history independent of completion
    order) is a hard constraint on items 2–4: the `ToolResultEvent` commit burst and
    history order must not change.
- **Ratified design calls** (owner, 2026-08-11, via AskUserQuestion):
  1. The ongoing-action (gist) removal applies to **grouped AND lone** sub-agent runs.
  2. A queued sub-agent stops being "scheduled" and becomes expandable **the moment it
     starts running** (per-child started signal), not on first output.
  3. The per-child finished event **carries the result payload** so an early-done
     member's report is readable immediately; the history commit burst stays unchanged.
  4. The gap between a tool row's content and the fold indicator (▶/▼) is **exactly one
     space**, uniformly for every tool row (from the owner's example in the report).
- **Standing requirements:**
  - skills: coding-standards
  - Run `make check` before every commit.
  - Each user-visible item adds one bullet to `CHANGELOG.md` under `[Unreleased]`.
  - Never change `VERSION` or add a CHANGELOG release heading (see closing note).
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Changing history/commit-order semantics of ADR 0039 (event ADDITIONS only).
  - Exposing `parallel-agents` in the settings pane (stays a file-only key).
  - Status-line (`internal/tui/activity.go`) wording — `toolPhrase`/`runningPhrase`
    behavior there is untouched; only the sub-agent summary slot changes.
  - Guided-decomposition batching (`internal/mechanisms/guideddecomposition.go`) — its
    deferred items are not sub-agents and get no "scheduled" display.

## 1. Single-space gap before the fold indicator — ✅ DONE (2026-08-11)

NOTES (2026-08-11): `docs/layout/tool-layout.md` already drew a one-space gap, so no sketch there
needed changing; the three-space gap lived in `layout.md`'s opening sketch (:31, :34, :43, :51)
instead, and those four rows were updated (the two cells go to the dot leader, right edge unmoved).
Two goldens changed shape beyond the gap because the two freed cells alter the width arithmetic:
`TestRenderMarksTheWholeBlock`'s 11-column row now clips its target to `a …` instead of dropping it,
and `TestExpandedSubAgentOpensWithItsPrompt`'s 34-column head now promotes its one-line report into
the outcome slot (so it paints no body row and no see-less footer).

**What:** Change `groupIndicatorGap` from 3 to 1 in `internal/tui/toolleader.go`
(constant near :337; consumed by `groupIndicatorCells` and `indicatorRow` in
`internal/tui/toolblock.go:386-389`). This is the reserved field between a row's
content and the right-aligned ▶/▼ column; the change is global and intended (ratified
call 4): `⋯⋯ exit 0 · +10 more lines ▶` with one space, everywhere. Update any literal
goldens; tests built on the `leaderEdgeRow` helper (`internal/tui/render_test.go:2230`)
adapt automatically. If sketches in `docs/layout/tool-layout.md` (width/overflow
section :83-95 and the fold-state sketches) show the old three-space gap, update them.

**Tests:** existing render/transcript goldens updated to the one-space gap; no new test
needed beyond the goldens.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes; a golden shows
exactly one space between the slot text and `▶`.

Commit: `fix(tui): single-space gap before the fold indicator`

## 2. Engine: per-delegation lifecycle events — ✅ DONE (2026-08-11)

NOTES (2026-08-11): two points the item's What left open were settled at write time. (a) The event's
`EventBase` carries the CHILD run's identity — `Depth` = parent depth + 1 beside the delegation's
`CallID` — so it is the same stamp the child's own events carry and equals the TUI's `runRef` for
that run; item 3 still matches the parent's tool-call block by `CallID` alone, exactly as
`addToolResult` does. (b) A delegation ending in a CANCELLATION emits `started` but no `finished`:
the cancelled group is dropped unappended and never becomes a result, so a finished phase would
report a delegation the Turn is about to roll back (the phase pair is left open exactly as the
tool call is). Two variant-coverage guards had to be taught the new variant, both of which fail
deliberately on an unknown one: `eventBaseOf` (`internal/agent/subagent_test.go`) and the TUI's
`foldCases` table (`internal/tui/fold_test.go`), where it is recorded as inert in the view for now —
item 3 gives it its behavior. Added a CHANGELOG bullet, since a new Event variant is a public-API
addition per the file's own versioning note.

**What:** Add a new event type in `internal/domain` alongside `ToolResultEvent`:
`SubAgentPhaseEvent{ EventBase, Phase, Result }` where `EventBase` carries the CallID,
`Phase` is one of the constants `SubAgentStarted` / `SubAgentFinished`, and `Result`
holds the child's `ToolResult` payload on `finished` (zero value on `started`) —
ratified call 3. Emit it in `internal/agent/dispatch.go`:
- fan-out path: the pool worker (`runDelegationPool`, :246-265) emits `started` when it
  dequeues a job and `finished` (with the result) when the child returns;
- the serial/lone delegation path emits the same pair, so a lone run is never stuck
  looking scheduled (consumed by items 3–4).

The commit machinery is untouched: `ToolCallEvent`s still all emit up front
(:179-181, :210), `commitDelegation`/`appendToolResult` (:295-308, :758-767) still
burst all `ToolResultEvent`s in emitted-call order after the pool joins (ADR 0039
decision 4).

**Tests:** `internal/agent` test with a recording event sink: N delegations at cap
w<N → all `ToolCallEvent`s first; per child, `started` precedes `finished`; at most w
children are started-but-not-finished at any point; `ToolResultEvent`s arrive as a
trailing burst in emitted-call order regardless of completion order; the serial path
emits the pair; a failing child's `finished` event carries the error result.

**Acceptance:** `go test ./internal/agent/` passes; `make check` passes.

Commit: `feat(agent): emit per-delegation lifecycle phase events`

## 3. TUI: per-member done as each sub-agent finishes — ✅ DONE (2026-08-11)

Depends on item 2.

NOTES (2026-08-11): two points beyond the item's literal text. (a) The finished phase does NOT set
`entry.done`: that flag is the call/result PAIRING the whole transcript keys on, and setting it early
would send the burst `ToolResultEvent` down `addToolResult`'s orphan branch (a stray result block) and
skip `closeRun`. The double-apply guard is therefore a phase check inside `addToolResult` —
`enrichWithResult` appends to the body, so the second fold is the one skipped — and done-ness for
DISPLAY is read through a new `subAgentReported(head)` (`done || phase == finished`). (b) Three display
reads take that helper, not just `subAgentFinished`: `subAgentGist` too (else a phase-finished member
shows no `done` in its slot at all, which the item's own acceptance asks for), and the lone run's
`live` in `renderSubAgentRun` with the matching `blockKey` call in `render.go` (a ✓ over a blinking
star would contradict itself). Also corrected `transcript.apply`'s doc-comment variant counts, which
item 2's new Event variant left stale (eight-of-eleven → nine-of-twelve).

**What:** The transcript tracks the delegation phase per entry: handle
`SubAgentPhaseEvent` in `transcript.apply` (`internal/tui/transcript.go`, event switch
near :663-687), matching on CallID, storing the phase on the `entry` (new field next to
`entry.done`, :163). On `finished`, apply the carried result to the entry's display via
the existing `enrichWithResult` path; the later burst `ToolResultEvent` must stay
harmless — `addToolResult`'s `!e.done` guard (:884) must not double-apply. Derive
done-ness for display from the phase so `subAgentFinished`
(`internal/tui/subagentblock.go:308-310`) shows ✓ + `done` (red text on failure, via
the existing `failedSummary` path) the moment THAT member finishes — not when the whole
group joins. Add the phase to the paint key (`spanFlags`,
`internal/tui/paintcache.go:131-147`) so rows repaint on phase change. Update the
done/running sketch note in `docs/layout/tool-layout.md:232-238` to state that ✓
appears per member as it finishes.

**Tests:** TUI golden: two-member group, one member finished via phase event only (no
result burst yet) → that member shows ✓ + `done`, the other still live; after the burst
the visual is unchanged; expanding the early-done member shows its report body
(ratified call 3).

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

Commit: `fix(tui): mark grouped sub-agent members done as each finishes`

## 4. TUI: "scheduled" state for queued sub-agents — ✅ DONE (2026-08-11)

Depends on item 3.

NOTES (2026-08-11): the "no `started` phase ⇒ scheduled" rule is narrowed by two further
un-schedulings. (a) An arrived RESULT (the dispatched decision): a delegation refused at the depth
bound or failed by a hook never starts but does answer, and would otherwise read as queued forever.
(b) Being FRAMED (`subAgentFramed`): entries already committed behind it, or the row being open. That
one is for producers that emit no phases at all — hand-built test transcripts and replayed records —
and for the live streaming preview, whose delegate has manifestly started while its span is still
empty; it also makes scheduled and framed mutually exclusive, so a queued row can never be handed a
frame. No `blockstate.go` change was needed: the scheduled view drops the body, and the ordinary
member painter already gives a bodyless member no indicator and no click target (`targetNone`), which
is exactly the inert row the item asks for. One existing golden
(`TestSubAgentStreamFramesAnOpenGroupMember`) gained a `started` phase for its second member, which
is what its prose already claimed of it ("the sibling still working").

**What:** A sub-agent entry whose `ToolCallEvent` arrived but whose `started` phase has
not (item 3's tracking) renders as scheduled: its `<tool-top-level-details>` slot shows
exactly `scheduled` (no tool-call count, no token fill, no gist), and the row is
non-expandable — no ▶ indicator and an inert toggle (the `targetNone` path in
`internal/tui/blockstate.go` / `renderSubAgentMemberRows` in
`internal/tui/subagentblock.go:333-351`; clicks are a no-op). At `started` the row
flips to the normal live display and becomes expandable (ratified call 2). This
naturally covers the cap case (model requests 20, `parallel-agents` = 5 → 15 show
`scheduled`); lone runs start immediately and never show it — no special-casing. Spec:
add the scheduled state to `docs/layout/tool-layout.md` Grouped Sub-agents (:207-238)
and the `sub_agent` row of the per-tool table (:276).

**Tests:** golden: three-member group where the third has no `started` phase → shows
`scheduled`, no ▶, toggle/click is a no-op; after its `started` event → normal live
row, expandable.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

Commit: `feat(tui): scheduled state for queued sub-agents`

## 5. TUI: drop the ongoing-action text from sub-agent summaries — ✅ DONE (2026-08-11)

NOTES (2026-08-11): one deviation and one thing deliberately left. (a) `toolPhrase` was the only
consumer of the `measure widthAuthority` threaded into `subAgentGist`, `subAgentSummary` and
`collapsedSubAgentView`, so dropping the phrase left the parameter dead in all three; it was removed
from the three signatures and their two call sites rather than left as rot (no behavior change, no
test touched it). (b) `toolPhrase` and `statusTargetCells` (`internal/tui/activity.go`) now have no
production caller at all — the item's text orders activity.go untouched, so they stand, and their doc
comments still describe the gist as their surface. Worth a follow-up to remove both or repoint their
prose. Also pinned the "most recent open call" half of the `delegating` rule: a grandchild's own open
call is the newest one, so the word goes again while it works. (c) the package map
`internal/tui/doc.go` asserted the removed behavior — the collapsed run's gist at :73-74 and
`toolPhrase` "still" serving that surface at :295-297 — so both passages were rewritten to the
shipped rule (docs only, no code change).

**What:** In `internal/tui/subagentblock.go`, `subAgentSummary` (:387-402) and
`subAgentGist` (:434-450): remove the live `toolPhrase` gist for BOTH grouped members
and lone runs (ratified call 1). While running the summary reads `N tool calls ·
<fill>`; append ` · delegating` if and only if the most-recent open call in the span is
a `sub_agent` (a running sub-sub-agent); ` · done` when finished stays as is.
`toolPhrase` itself and all `internal/tui/activity.go` status-line uses are untouched.
Update the gist wording in `layout.md:549-552` and the Grouped Sub-agents rules in
`docs/layout/tool-layout.md:209-211` to match.

**Tests:** goldens: running lone run and running grouped member show `N tool calls ·
<fill>` with no action phrase; a member with a running nested delegation shows
`· delegating`; the done case is unchanged.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

Commit: `fix(tui): drop live action text from sub-agent summaries`

## 6. TUI: expanded sub-agents keep their top-level details — ✅ DONE (2026-08-11)

Depends on item 5.

NOTES (2026-08-11): the rule was taken literally, and two of its consequences are worth stating. (a) An
open head whose span is still EMPTY — a delegate streaming its first words, nothing committed behind it
yet — now says `0 tool calls` where the slot used to be blank; that is the same reading its collapsed
row would give, and narrowing it would be a second wording of the summary the item forbids. (b) At
narrow widths the promote-guard now bites on the open head: the run summary is a quoted line over the
`done` stat, so where it leaves the name under 15 cells it demotes to the head's first body row and the
typed `done` takes the slot (`TestExpandedSubAgentOpensWithItsPrompt`'s 34-column golden). That is the
existing guard applied to the new slot, and collapsed and open answer it identically since it depends on
width alone. Structurally, `collapsedSubAgentView` is now the open reading minus its body
(`expandedSubAgentView`), so one wording of "what does a delegation say" serves both fold states. Docs
beyond the item's named sketch: `internal/tui/doc.go`'s package map asserted that an open run's header
row "says only what the delegation IS", and `layout.md`'s expanding sentence was silent on the slot —
both now state the shipped rule.

**What:** Expanded sub-agent rows currently revert to the raw head view and lose the
summary slot: `renderSubAgentRun` (`internal/tui/subagentblock.go:159-164`, lone) and
`renderSubAgentGroup` (:275-278, grouped member) substitute `collapsedSubAgentView`
only when NOT expanded. Change both so the expanded view carries the same
`<tool-top-level-details>` as the collapsed one (item 5's summary: `N tool calls ·
<fill>[ · delegating| · done]`) — only the body/prompt/frame rendering differs between
fold states. Applies to running and done states. Update the expanded sketch in
`docs/layout/tool-layout.md:213-230` to show the slot.

**Tests:** goldens: expanded running member shows `N tool calls · <fill>` in the slot;
expanded done member/lone run shows `… · done`; collapsed rendering unchanged.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

Commit: `fix(tui): keep top-level details on expanded sub-agents`

## 7. TUI: blank line between sub-agent name and initial prompt — ✅ DONE (2026-08-11)

NOTES (2026-08-11): closed as a GOLDEN-LOCK — the item's own second branch. Reproduction found the
railed blank standing in every expanded rendering: a sweep of 5 fixture shapes (lone, grouped first /
grouped last, nested, streaming-only) × 5 prompt shapes (one-liner, markdown, leading blanks, fenced,
list) × 3 report shapes (none, promoted gist, laid-out body) × 5 widths (20/34/48/80/120), rendered
both cold and through a warmed paint cache, put a spacer immediately before the prompt in all of them,
and removing the `railSpacer` from `subAgentPromptRows` fails all six new goldens. One thing the
reproduction did turn up: a fixture whose child work is NOT stamped with the spawning call id lands
that work behind whichever delegation was announced last, leaving the earlier member spanless,
unframed and so prompt-less — an artifact of hand-built transcripts only (the engine stamps every
child event, `Agent.base`), and the reason the new fixture stamps it too. A delegation that genuinely
produced no span (refused at the depth bound, failed by a hook) still shows no prompt at all when
opened; that is `subAgentFramed`'s rule, identical in the lone and grouped paths, and not a blank-line
defect. No CHANGELOG bullet: nothing user-visible changed. No spec change either — the Grouped
Sub-agents sketch in `docs/layout/tool-layout.md` already draws the blank row under the `┌─┶` header.

**What:** The owner reports no blank line between an expanded sub-agent's name row and
the delegated prompt. The code intends one (`railSpacer` prepended in
`subAgentPromptRows`, `internal/tui/subagentblock.go:208-214`) and the lone-run golden
shows it (`internal/tui/transcript_test.go:1236-1241`) — so first REPRODUCE where it is
lost (prime suspect: the grouped-member path `renderSubAgentMemberRows`, :333-351, or a
downstream blank-collapsing/trim pass), then guarantee exactly one railed blank line
immediately before the prompt in EVERY expanded rendering: lone and grouped member,
with and without a report body. If reproduction shows the blank already present in
every path, close the item as a golden-lock — add the grouped-member goldens asserting
the blank — and record that outcome as a dated NOTES line here.

**Tests:** goldens asserting `name row → … → blank railed line → prompt` for lone and
grouped expanded views, both running and done.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

Commit: `fix(tui): blank line before the delegated prompt in expanded sub-agents`

---

**Suggested version bump:** one micro bump (v0.12.15 → v0.12.16) covering the batch,
per the per-shipped-feature micro-bump convention. The owner decides; no item touches
`VERSION` or release headings.
