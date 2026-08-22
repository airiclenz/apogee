# ISSUES residuals cleanup plan

**Goal:** close all 16 open defects in `ISSUES.md` — the doc/comment corrections and small
code fixes left behind as residuals of the 2026-08-19/20 plan runs.

**Date:** 2026-08-22 · **Status:** unexecuted · **Sized for:** ~200k-context host

**Authoritative sources:**

- `ISSUES.md` (Open defects section) — each item below names its entry verbatim; the entry's
  `file:line` evidence is the ground truth for what to change. If an item's summary here
  disagrees with its ISSUES.md entry, the entry wins.
- `internal/tools/path_safety.go:338` (`journalTarget`'s comment) — the current journal path
  rule (item 2).
- `docs/plans/2026-08-19 - 03 - split-diff-display-plan.md` item 2 NOTES — the owner's
  2026-08-19 tiling decision (item 3).
- `docs/plans/2026-08-19 - 05 - diff-background-tint-plan.md` ratified call 3 ("the gutter
  stays chrome") — item 8.
- ADR 0047 (`api-key-env`) and `docs/design/confinement-execution-contract.md` §10.5 — item 11.

**Ratified design calls (owner, 2026-08-22, via AskUserQuestion at plan write time):**

1. `CHANGELOG.md:49` is **edited in place** — the bullet is still under `[Unreleased]`, no
   release has shipped it.
2. `toolCallPath` reports the **destination** for `copy_file`/`move_file` — it matches
   `deriveWriteTarget` semantics; a move's vacated source staying invisible to the path-keyed
   family is accepted and noted in code.
3. The pre-`statValue` session-record break is **accepted and documented**, not papered over
   with a decode fallback — no phrase-parsing code returns.
4. `runOneHook` **books the fire outside** the recover boundary — a panicking Events sink is
   never reported as `errHookPanicked` attributed to an innocent Mechanism.
5. ADR 0053's adoption note **drops the verb** — the decision panes take `move` and
   `highlight` without `key`; `clampSelection` leaves the list.

**Standing requirements:**

- skills: coding-standards
- Every item REMOVES its own entry (or entries) from `ISSUES.md` — the register holds open
  work only; the closed trail is the CHANGELOG entry the verifier records.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**

- Everything under `ISSUES.md` "Parked / deferred work" — each is blocked on a grill, an
  owner call, bench evidence, external demand, or hardware; none rides in here.
- Retiring or renaming the `diff-add`/`diff-del` foreground roles (parked, needs an owner call).
- Any VERSION / CHANGELOG-release-heading change (see the closing note).
- Widening `toolCallPath` to multi-path returns (rejected at ratification in favour of
  destination-only).

---

## 1. Refresh AGENTS.md's docs/design parenthetical — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the added doc is named as the file, `tool-surface-findings.md`, and set off from the three contracts ("plus the ... record") rather than listed inside the parenthetical with them — it is a findings record, not a design contract, and the item's own acceptance greps for the hyphenated spelling.

**What:** Fix the ISSUES.md entry *"`AGENTS.md:9`'s `docs/design/` parenthetical does not
list `tool-surface-findings.md`"*. Rewrite the parenthetical on the `docs/design/` bullet in
`AGENTS.md` to name the design docs actually present at the top level of `docs/design/`
(verify with `ls docs/design/*.md`): add `tool-surface-findings.md`, drop
`hook mutation API` and `technical design` (both now under `docs/design/archived/`). Keep
the line's one-sentence shape. Remove the ISSUES.md entry.

**Files:** `AGENTS.md`, `ISSUES.md`

**Tests:** none (prose).

**Acceptance:**
- `grep -q "tool-surface-findings" AGENTS.md`
- `grep -n "hook mutation API" AGENTS.md` returns nothing
- `grep -c "tool-surface-findings.md" ISSUES.md` shows the entry is gone (no heading for it)

**Commit:** `docs(agents): refresh the docs/design parenthetical`

---

## 2. Correct the journal path rule at the two remaining sites — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the item cites `internal/tui/tui.go:194` and `CHANGELOG.md:49`; both files have grown since the entry was written, so the corrections landed at `tui.go:611` and `CHANGELOG.md:201` — the grep evidence, not the line numbers, located them. The `/undo` bullet was edited in place per ratified call 1.

**What:** Fix the ISSUES.md entry *"Two doc sites still assert the old resolved-path journal
rule"*. The journal records the NAMED path — the argument's root-joined, cleaned spelling
with nothing followed — and only an approved escape (ADR 0049) records the permit's resolved
target; the rationale is `journalTarget`'s comment (`internal/tools/path_safety.go:338`).
Correct both sites to that rule:

- `internal/tui/tui.go:194` — the `UndoPreview` doc on the `Engine` seam: replace "at
  resolved absolute paths" with the named-path wording.
- `CHANGELOG.md:49` — the `[Unreleased]` `/undo` bullet: **edit in place** (ratified call 1);
  replace "every recorded path at its resolved spelling" with the named-path wording.

Remove the ISSUES.md entry.

**Files:** `internal/tui/tui.go`, `CHANGELOG.md`, `ISSUES.md`

**Tests:** none (doc comment + changelog prose).

**Acceptance:**
- `go build ./internal/tui`
- `grep -n "resolved absolute paths" internal/tui/tui.go` returns nothing
- `grep -n "at its resolved spelling" CHANGELOG.md` returns nothing

**Commit:** `docs: correct the journal path rule at the two remaining doc sites`

---

## 3. Amend ADR 0052 §1 to the tiling rule that shipped — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the amendment cites the plan at its archived path, `docs/plans/archived/2026-08-19 - 03 - split-diff-display-plan.md` (where it now lives), rather than the unarchived path the item quotes. The amendment paraphrases the old "merged" wording instead of quoting it verbatim, so the item's `! grep "three.*merged"` acceptance holds over the whole file.

**What:** Fix the ISSUES.md entry *"ADR 0052 §1 still describes MERGED context, not the
tiling rule that shipped"*. In
`docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`
(§1, lines 39–40), replace the "up to three merged unchanged context lines each side"
wording with the shipped tiling rule: neighbours within the gap stay SEPARATE regions whose
context ranges tile the interior lines without overlap, and the renderers omit the `⋯`
separator between contiguous regions so the paint matches a merge. Record it as a dated
amendment (2026-08-22) citing the owner's 2026-08-19 decision from
`docs/plans/2026-08-19 - 03 - split-diff-display-plan.md` item 2 NOTES, in the amendment
style the repo's other ADRs use. Remove the ISSUES.md entry.

**Files:** `docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`, `ISSUES.md`

**Tests:** none (ADR prose).

**Acceptance:**
- `grep -qi "tile" "docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md"`
- `grep -n "three.*merged" "docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md"` returns nothing

**Commit:** `docs(adr): amend ADR 0052 §1 to the tiling rule that shipped`

---

## 4. Drop clampSelection from ADR 0053's decision-pane sentence — ✅ DONE (2026-08-22)

**What:** Fix the ISSUES.md entry *"ADR 0053's adoption note credits the approval and ask
panes with `clampSelection`"*. In
`docs/adr/0053-popup-surfaces-embed-one-list-surface.md` (the adoption note near line 120),
correct the sentence so the two decision panes "take `move` and `highlight` without `key`" —
`clampSelection` leaves the list (ratified call 5). No explanatory aside; the shorter true
statement is the fix. Remove the ISSUES.md entry.

**Files:** `docs/adr/0053-popup-surfaces-embed-one-list-surface.md`, `ISSUES.md`

**Tests:** none (ADR prose).

**Acceptance:**
- The adoption note's decision-pane sentence no longer contains `clampSelection`:
  `grep -n "clampSelection" "docs/adr/0053-popup-surfaces-embed-one-list-surface.md"` shows
  no hit attributing it to the approval/ask panes.

**Commit:** `docs(adr): ADR 0053 stops crediting the decision panes with clampSelection`

---

## 5. Repoint six doc comments left stale by the deepening runs — ✅ DONE (2026-08-22)

**What:** Fix TWO ISSUES.md entries — *"Two shipped comments still point at the home their
subject left"* and *"Four shipped doc comments went stale under the engine-deepening run"*.
Six comment-only edits; each entry's `file:line` evidence names the correction:

- `internal/tui/prompteditor.go:83` — `keyDisambiguation` doc: the setter is
  `foldKeyboardEnhancements` in this file (`prompteditor.go:258`), not "the arm in model.go".
- `internal/tui/model.go:364` — `lineTargets` doc: the `lineTarget`/`lineMark` vocabulary
  lives in `internal/tui/blocktarget.go:47`; `render.go` remains the producer (the pointer at
  `model.go:1832` is already right — leave it).
- `internal/mechanisms/autofix.go:68-70` — the `timeout` field doc: the kill path is bounded
  by the funnel's fixed 5s `processWaitDelay`, not by this field.
- `internal/agent/agent.go:309-311` — `Close`'s doc: the log sink's teardown lives in the
  TUI (`internal/tui/tui.go:1525` defers `diag.Close()`), not in `cmd/apogee`.
- `internal/domain/config.go:300-302` — `ModelProfile.Pattern`'s doc: a pattern set under a
  non-custom-regex format is a REFUSAL at config load, not ignored (mirror the wording the
  shipped default template already carries).
- `internal/domain/hooks.go:295-297` — `LoopView`'s doc: the tool-stage mutable values are
  `ToolCallEdit` and `ToolResultEdit`, not `*ToolCall`/`*ToolResult`.

Remove both ISSUES.md entries.

**Files:** `internal/tui/prompteditor.go`, `internal/tui/model.go`, `internal/mechanisms/autofix.go`, `internal/agent/agent.go`, `internal/domain/config.go`, `internal/domain/hooks.go`, `ISSUES.md`

**Tests:** none (comment-only).

**Acceptance:**
- `go build ./...`
- `go vet ./internal/tui ./internal/mechanisms ./internal/agent ./internal/domain`

**Commit:** `docs(code): repoint six doc comments left stale by the deepening runs`

---

## 6. Record the accepted one-way break for pre-statValue session records — ✅ DONE (2026-08-22)

**What:** Fix the ISSUES.md entry *"A session saved before the typed stat value replays with
blank type-row totals"* the way ratified call 3 decided: **accept and document**, no decode
fallback and no resurrected phrase parsing. At `fromWireStatValue`
(`internal/tui/transcriptcodec.go:270`), extend the comment to state: a record with no
`statValue` (written before 113b3078) decodes as a plain value, plain values do not sum, so
a replayed multi-call run's grouped type row shows an empty total — a deliberate one-way
break; the value is NOT re-derived from the phrase. Remove the ISSUES.md entry; the sidecar's
CHANGELOG text records the acceptance.

**Files:** `internal/tui/transcriptcodec.go`, `ISSUES.md`

**Tests:** none (comment-only; behaviour unchanged).

**Acceptance:**
- `go build ./internal/tui`
- `grep -n "one-way" internal/tui/transcriptcodec.go` (or equivalent wording per the comment
  written) shows the note beside `fromWireStatValue`.

**Commit:** `docs(tui): record the accepted one-way break for pre-statValue session records`

---

## 7. Parse quoted paths in git diff section headers — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the ISSUES entry's premise that git quotes a path holding a SPACE is wrong — verified against git, a bare space needs no escaping (`quote_c_style`), so `diff --git a/my file.go b/my file.go` prints unquoted and the existing greedy pattern already read it. The quoted shape the fix adds is what a control byte, a double quote, a backslash or a non-ASCII byte forces; the item's space case is still covered as a quoted-header test, since the reading accepts a space riding inside a quoted name.

**What:** Fix the ISSUES.md entry *"`git_diff_range` drops the whole diff to plain rendering
when git quotes a path"*. The section-header match at `internal/tui/diffbody.go:377`
(`^diff --git a/(.+) b/(.+)$`) fails on git's quoted form —
`diff --git "a/my file.go" "b/my file.go"` — so `gitDiffWalk.take` (`diffbody.go:448`)
returns false and the all-or-nothing walk (`gitDiffFileSections`, `diffbody.go:421`) drops
the WHOLE body to plain rendering. Teach the header match the quoted form: recognise a
`"a/..." "b/..."` header, unquote per git's C-style quoting (backslash escapes, including
octal byte escapes) enough to recover the two path names, and let the walk proceed. A header
that fits neither form still fails the walk as today.

**Files:** `internal/tui/diffbody.go`, `internal/tui/splitdiff_test.go`, `ISSUES.md`

**Tests:** in `splitdiff_test.go` — a diff whose one file path holds a space (quoted header)
parses into file sections and renders split, not plain; a multi-file diff where ONE path is
quoted keeps split rendering for ALL files; a path with an escaped non-ASCII byte unquotes
without error.

**Acceptance:**
- `go test ./internal/tui -count=1`

**Commit:** `fix(tui): parse quoted paths in git diff section headers`

---

## 8. Hold the stacked frame's line number out of the diff band — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the split needed a place to put the number, so the change reaches past the item's three named files — `detailLine` gained a `Gutter` member (`toolview.go`), the one body painter composes it into the frame's hanging prefix (`toolbody.go`), and it rides the session record additively (`transcriptcodec.go`) so a resumed diff is not replayed numberless. Three further test files (`toolpresent_test.go`, `toolbranch_test.go`, `workspacepath_test.go`) held goldens of the composed row and were moved onto the split shape; `docs/layout/split-diff-layout.md`'s Stacked section gained the chrome rule it now shares with the panes.

NOTES (2026-08-22): the 160-rune detail clip now measures the marker and the code, not the number with them — the number is out of its reach entirely, which is what the split makes true rather than a choice taken beside it.

**What:** Fix the ISSUES.md entry *"The stacked frame's line NUMBER rides inside the diff
band, not the chrome gutter"* — the still-unmet ratified call 3 of the diff-background-tint
plan ("the gutter stays chrome": line numbers untinted). `stackedRow.line`
(`internal/tui/diffbody.go:606`) composes number + marker + text into the detail line's
single `Text` field, so the tint band paints under the number. Binding approach (from the
entry): split the number off the styled text BEFORE the row reaches the style seam — no
wrap-rail change; the rail cannot tell the number from the text once they share one string.
The marker deliberately stays ON the band (plan `2026-08-19 - 05` item 3's ADR 0052
amendment) — only the number moves to chrome. Remove the ISSUES.md entry.

**Files:** `internal/tui/diffbody.go`, `internal/tui/splitdiff_test.go`, `ISSUES.md`

**Tests:** in `splitdiff_test.go` — a rendered stacked row carries its line number outside
the tinted segment (number cell painted chrome, marker + text on the band); alignment of the
right-aligned number column is unchanged.

**Acceptance:**
- `go test ./internal/tui -count=1`

**Commit:** `fix(tui): hold the stacked frame's line number out of the diff band`

---

## 9. Derive the session browser's visible list once per frame

**What:** Fix the ISSUES.md entry *"`sessionBrowser` derives the visible list twice per
frame"*. `filteredView` calls `b.visible(workspace)` (`internal/tui/sessions.go:198`) and
then `unfilteredRows`, which calls it again (`sessions.go:213`); `visible` allocates and
copies over the whole store per call (`sessions.go:164-175`). Remove the duplicate the way
the entry sketches: pass the already-derived visible slice into `unfilteredRows` (or have
one call answer both halves) WITHOUT touching the seam item 15 of the tui-deepening plan
created. Behaviour-preserving — same rows, same order, same filtering. Remove the ISSUES.md
entry.

**Files:** `internal/tui/sessions.go`, `internal/tui/sessions_test.go`, `ISSUES.md`

**Tests:** existing browser tests stay green; extend `sessions_test.go` only if the
signature change needs a call-site adjustment in a test.

**Acceptance:**
- `go test ./internal/tui -count=1 -run 'Session|Browser'` (and the package builds:
  `go build ./internal/tui`)

**Commit:** `refactor(tui): derive the session browser's visible list once per frame`

---

## 10. Teach toolCallPath the copy_file / move_file destination

**What:** Fix the ISSUES.md entry *"The path-keyed history family still cannot see
`copy_file` / `move_file`"*. `toolCallPath` (`internal/mechanisms/offramps.go:59`) reads
`path` / `file_path` / `filePath` / `filename`; `copy_file` and `move_file` carry
`source` / `destination` (`internal/tools/file_ops.go:66-67`) and so report `""` to every
path-keyed consumer. Ratified call 2: report the **destination** — the file the write landed
on, matching `deriveWriteTarget` semantics. Extend `toolCallPath` to read `destination` when
present. Add a short comment recording the accepted limit: a move's vacated source stays
invisible to the path-keyed family (cache invalidation included) — destination-only by
owner call 2026-08-22. Remove the ISSUES.md entry.

**Files:** `internal/mechanisms/offramps.go`, `internal/mechanisms/offramps_test.go`, `ISSUES.md`

**Tests:** in `offramps_test.go` — `toolCallPath` returns the destination for a `copy_file`
call and a `move_file` call; the four existing spellings still resolve; a call carrying both
`path` and `destination` keeps today's precedence for `path` (pin whichever precedence the
implementation chooses as a test).

**Acceptance:**
- `go test ./internal/mechanisms -count=1`

**Commit:** `fix(mechanisms): the path-keyed history family sees copy_file and move_file`

---

## 11. Hook subprocesses scrub operator-declared key variables too

**What:** Fix the ISSUES.md entry *"The hook subprocess env scrub drops apogee's own keys
but not the operator-named ones"*. `RunHookSubprocess` calls `subprocessEnv(nil)`
(`internal/tools/exec_common.go:345`), so an `autofix`-spawned formatter inherits the
operator's `api-key-env` names (ADR 0047) while `terminal`/`python_exec`/`run_tests` scrub
them (threaded via `HostTools.SecretEnvVars`, `internal/tools/registry.go:66`, `:170-178`).
Close it the way the entry states: carry the operator-declared secret env names on
`mechanisms.Deps` (`internal/mechanisms/catalogue.go:20`), fill them in the engine's deps
derivation in `internal/agent` from the same source `HostTools.SecretEnvVars` is filled
from, and pass them through the `RunHookSubprocess` door so `subprocessEnv` receives them —
hook children then scrub exactly what the three execution tools scrub. Update the prose that
records the asymmetry: `internal/tools/exec_common.go:325-327` and
`docs/design/confinement-execution-contract.md` §10.5 now describe the symmetric scrub.
Adjust `RunHookSubprocess`'s callers (the autofix Mechanism) for the widened door. Remove
the ISSUES.md entry.

**Files:** `internal/tools/exec_common.go`, `internal/tools/exec_common_test.go`, `internal/mechanisms/catalogue.go`, `internal/mechanisms/autofix.go`, `internal/agent/construct.go`, `docs/design/confinement-execution-contract.md`, `ISSUES.md`

**Tests:** a test proving a hook-subprocess child's env drops an operator-named variable
(and still drops `apogeeSecretEnvVars`); a deps-derivation test in `internal/agent` pinning
that the configured names reach `Deps`.

**Acceptance:**
- `go test ./internal/tools ./internal/mechanisms ./internal/agent -count=1`

**Commit:** `fix(tools): hook subprocesses scrub operator-declared key variables too`

---

## 12. Pin construct.go's deliberately empty NetworkAllow

**What:** Fix the ISSUES.md entry *"Nothing pins `construct.go`'s deliberate empty
`NetworkAllow`"*. The tool box built at construction clears `NetworkAllow` on purpose
(`internal/agent/construct.go:345-350` carries the reasoning); deleting the clearing line
leaves `./internal/agent` green today. Add a test beside the existing agent-construction
coverage in `internal/agent/construct_test.go` that builds the agent from a config whose
`ConfinementBox()` WOULD fill `NetworkAllow` and asserts the constructed box's field is
empty — so removing the clearing line fails a test. Test-only item; no production change.
Remove the ISSUES.md entry.

**Files:** `internal/agent/construct_test.go`, `ISSUES.md`

**Tests:** the new pin itself (see What).

**Acceptance:**
- `go test ./internal/agent -count=1`

**Commit:** `test(agent): pin the construction box's deliberately empty NetworkAllow`

---

## 13. Refuse a delimited thinking style with no delimiters

**What:** Fix the ISSUES.md entry *"A `delimited` thinking style with no delimiters loads
and strips nothing"*. `validateThinkingAxes` (`internal/config/config.go:1814-1826`) checks
`style` and `effort` but never `start`/`end` (`thinkingConfig`, `config.go:1721-1726`), so
`style: delimited` with no token pair loads clean and wraps `StripThinking` with two empty
delimiters (`internal/processing/parserfor.go:57`) — reasoning lands in visible content with
no error naming the key. Add the fifth check: refuse `delimited` unless BOTH `start` and
`end` are set, with an error in the style of the existing messages, naming the key
(`model-profiles.<pattern>.thinking.start` / `.end`). A non-delimited style with stray
tokens stays as it behaves today (do not widen the check beyond the entry). Remove the
ISSUES.md entry.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `ISSUES.md`

**Tests:** in `config_test.go` — `delimited` with no tokens refuses naming the key;
`delimited` missing only `end` refuses; `delimited` with both loads; the three other styles
still load without tokens.

**Acceptance:**
- `go test ./internal/config -count=1`

**Commit:** `fix(config): refuse a delimited thinking style with no delimiters`

---

## 14. Book the mechanism fire outside the hook recover boundary

**What:** Fix the ISSUES.md entry *"`runOneHook`'s recover boundary now also covers the fire
booking"* per ratified call 4: **book outside**. `runOneHook` arms
`a.recoverHook(turn, id, &err)` for the whole function (`internal/agent/hookrun.go:137`) and
calls `a.fired(...)` inside it (`:146`), so a panicking Events sink is recovered and
reported as `errHookPanicked` attributed to the Mechanism (`hookrun.go:271-280`). Move the
`a.fired(...)` booking outside the recover boundary's scope — restoring the pre-collapse
shape, where every fire ran outside a hook boundary — so a sink panic is never attributed to
the Mechanism whose hook happened to be running. Keep the hook body's own coverage
unchanged. Remove the ISSUES.md entry.

**Files:** `internal/agent/hookrun.go`, `internal/agent/hookrun_test.go`, `ISSUES.md`

**Tests:** in `hookrun_test.go` — with an Events sink that panics on
`MechanismFiredEvent`, the run does NOT produce an `errHookPanicked` degradation naming the
Mechanism (pin the pre-collapse behaviour the implementation restores); a hook body that
panics is still recovered and attributed as today.

**Acceptance:**
- `go test ./internal/agent -count=1 -run 'Hook'` (and `go build ./internal/agent`)

**Commit:** `fix(agent): book the mechanism fire outside the hook recover boundary`

---

## 15. Guard hookPoints and hookImplements against HookPoint drift

**What:** Fix the ISSUES.md entry *"`hookPoints` and `hookImplements` duplicate the
HookPoint set with no drift guard"*. The five `HookPoint` constants are re-enumerated in
`internal/domain/registry.go` by `hookPoints` (`:53-59`) and `hookImplements`'s switch
(`:28-47`); a sixth constant forgotten in either fails nothing today. Add a drift-pin test
in `internal/domain` that fails when the constant set and either enumeration diverge — e.g.
pin the canonical list once in the test and assert `hookPoints` matches it exactly AND
`hookImplements` accepts a registration shape for every member (and refuses an out-of-range
value), so extending `mechanism.go` without extending both lists breaks the build's tests.
Test-only item; no production change. Remove the ISSUES.md entry.

**Files:** `internal/domain/registry_ordered_test.go`, `ISSUES.md`

**Tests:** the drift pin itself (see What).

**Acceptance:**
- `go test ./internal/domain -count=1`

**Commit:** `test(domain): guard hookPoints and hookImplements against HookPoint drift`

---

## Suggested version bump

A micro bump (v0.15.9 → v0.15.10) once the plan lands — 16 defects closed, two of them
user-visible rendering fixes (items 7, 8) and one a credential-hygiene fix (item 11). The
bump is the owner's call; no item performs it.
