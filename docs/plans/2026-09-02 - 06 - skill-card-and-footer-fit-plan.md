# Skill cards group like delegations; the footer fits by priority

**Goal:** close both open defects in `ISSUES.md`. (1) `load_skill` renders as an unnamed raw tool
that folds nowhere and says `load_skill`; it gains a friendly presenter, is marked never-group, and
adjacent fetches collapse under one `✦ Skill (N)` umbrella the way delegations do. (2) The footer
drops the mode marker whole on a narrow window; it gains a priority-ordered fit that never gives up
the model or the mode.

**Date:** 2026-09-02 · **Status:** unexecuted · **sized for:** ~200k-context host

**Sources (authoritative):**
- `docs/layout/tool-layout.md` — Rules `:44-51`, Vocabulary `:55-64`, Grouped Sub-agents `:213-246`, per-tool table `:261-292`
- `layout.md` — never-group sentence `:679-690`; the footer `:1385-1443`
- `docs/adr/0065-shipped-skills-and-the-load-skill-door.md` (§6, the tool); `docs/adr/0030-…width-authority….md` (§5, measure); `docs/adr/0063-…user-addressable-views.md`
- `ISSUES.md:28` and `ISSUES.md:30-35` — the two defects
- Base commit `46408ce9` — every line number below was verified against it

**Ratified design calls (owner, 2026-09-02):**
- **Skill label:** `Skill` — group `✦ Skill (N)`, lone call `✦ Skill`.
- **Skill row:** target = the loaded skill, outcome slot deliberately blank (`blankStat`).
- **Footer floor:** the mode marker is never clipped; the model truncates and may vanish.
- **Offline:** `✦ offline` is priority 0 — never dropped by the ladder.

**Derived calls (writer, 2026-09-02, from the four above):**
- **Retarget seam:** `toolOutcome` gains `Target string`; `absorbProse` applies it when non-empty. The result carries only the skill's DISPLAY NAME (`<skill: …>` opener), so that is what the row shows; the query stands where nothing was loaded.
- **One grouping mechanism:** the delegation group's derivation becomes name-keyed and serves both; delegation behaviour stays byte-identical.
- **Floor ladder** below the priority drops: truncate the model → drop the model → drop `offline` → drop the marker (only when it cannot seat whole).

**Regression check (2026-09-02, 46408ce9):**
- 1: guard folded — the acceptance runs the whole package; the grouping pin asserts a non-empty `Target`; the `toolsummary_pin_test.go` citation dropped.
- 2: guard folded — the whole-package acceptance reaches the four delegation pins the member swaps sit under.
- 3: guard folded — the narration sweep becomes a rule with a grep, and three `internal/tui` comment sites join **Files:**.
- 4: guard folded — the `unused` concern rejected on the tree; the acceptance runs the package.
- 5: guard folded — the footer's pinned surface reaches `cmd/apogee`; three e2e files join **Files:** and their anchors stay; yields to ADR 0060 decision 6 until item 6 amends it.
- 6: guard folded — the retired sentence's two in-code twins and ADR 0060 decision 6 join the sweep.

**Standing requirements:** `skills: coding-standards`.

**Out of scope:** B1 auto-attach (ADR 0061 D4); any change to what `load_skill` sends the model; the
status line above the box; the daemon/headless drivers; new `blockShape` semantics beyond the one
value item 2 adds; a version bump.

---

## 1. `load_skill` gains a presenter entry and never joins a Tools super-group — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's named test adjustment for `toolpresent_test.go`'s registry walk
turned out to need no code change — `load_skill` joins the prose-floor half of
`TestFileContentBodiesAreNumbered` on its own, so only that test's rule comment was restated to
name the third hook kind (`outcome`) now in that half.

NOTES (2026-09-02): the never-group comment sites the plan's item 3 sweep owns
(`toolview.go:578-586`, `toolbranch.go:367-370`, `transcript.go:1596-1600`, `doc.go`, `layout.md`,
`docs/layout/tool-layout.md`) are left stale here by design — item 3 restates them as a rule.

**What.** Fixes half of `ISSUES.md:28`: the card reads `✦ load_skill` today because `toolRegistry`
(`internal/tui/toolregistry.go:202`) has no entry and `presentToolCall`'s unregistered fallback
(`internal/tui/toolview.go:739-751`) uses the raw name. Add the entry — `label: "Skill"`,
`verb: "loading a skill"`, `target: stringArg("query")`, `stat: blankStat`, and an
`outcome: loadSkillOutcome` — and, in the SAME item, mark the call never-group, because a target
alone would make `groupable` (`internal/tui/toolbranch.go:373-375`) true and fold skill fetches
into `✦ Tools (N calls)`, which the defect forbids.

`loadSkillOutcome(args, content) toolOutcome` is TOTAL and anchored, the pattern
`toolregistry.go:26-33` licenses: it reads the skill's display name off the `<skill: …>` opener
`internal/tools/load_skill.go:107` writes, returns it as `Target` when present and `""` otherwise
(the query stands), and hands every line of `content` back as `Details` — today's floor, unchanged.
`toolOutcome` (`internal/tui/toolview.go:667-678`) gains a `Target string` field; `absorbProse`
(`internal/tui/toolview.go:1176`) assigns it only when non-empty, so no other tool moves.

Never-group: add `const loadSkillToolName = "load_skill"` beside `askUserToolName`
(`toolregistry.go:164`) and widen `presentToolCall`'s solo line (`toolview.go:760`) to
`tv.solo = call.Tool == subAgentToolName || call.Tool == loadSkillToolName`. Replay must agree:
`fromWireToolView` (`internal/tui/transcriptbridge.go:390`) re-derives solo for `sub_agent` and
answered `ask_user` — add the same name there, so a session recorded before this change replays
never-group.

**Regression guard.** Acceptance drops the -run regex and runs the whole package the item edits —
`go build ./... && go test ./internal/tui/` — so no pin this item touches can be filtered out of its
own gate; this is one package, not the repo full-suite gate. The grouping pin must also assert
`tv.Target != ""`: at base the unregistered fallback leaves `Target` empty, so `groupable`
(`toolbranch.go:373-375`) is already false and the pin would pass vacuously. And
`TestToolSummaryPinUsesRegisteredToolNames` (`toolsummary_pin_test.go:178-184`) is a closed list of
SUMMARY-BEARING tools — `load_skill` returns a plain `okResult` with no `domain.ToolSummary`
(`internal/tools/load_skill.go:88`), so it is not adjusted here.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolview.go`, `internal/tui/transcriptbridge.go`, `internal/tui/toolpresent_test.go`, `internal/tui/toolshape_test.go`, `internal/tui/transcriptbridge_test.go`

**Tests.** A card test driving a real `load_skill` call+result and asserting the painted header is
exactly `✦ Skill` (never `load_skill`), the row's target is the display name off the opener, and
the outcome slot is blank. A miss case: no opener → the query stays the target. A grouping test:
two adjacent `load_skill` calls each carry a non-empty `tv.Target` and still yield `!groupable(tv)`
and `toolSuperGroup(...) == nil` at every index. A bridge test:
a wire record with `Solo:false` replays solo. Adjust the registry-walking tests
(`toolpresent_test.go:3335`, `:3425`) for the new key.

**Acceptance.** `go build ./... && go test ./internal/tui/`

`fix(tui): present load_skill as Skill and keep it out of the Tools super-group`

---

## 2. Adjacent skill fetches group under one `✦ Skill (N)` umbrella — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the plan named `internal/tui/doc.go` under **Files:** for "the new file, if any"
— no new file was needed (the generalised derivation stayed in `transcript.go` beside the rule it
replaces), so `doc.go` and its `docmap_test` are untouched.

NOTES (2026-09-02): `resolveBlock`'s grouped-delegation body was extracted verbatim into
`(*transcript).resolveGroup(..., shape blockShape)` so the skill branch resolves through the SAME
block rule rather than a second copy of it — the plan's "one grouping mechanism" derived call
applied to the resolver as well as to the derivation. The delegation branch is now a one-line call
to it; behaviour is unchanged (the four delegation pins the plan names all pass).

NOTES (2026-09-02): a skill group interrupted by an expanded member paints the spec's `┊` seam
(docs/layout/tool-layout.md:217, "another grouped sub-agent followed after the expanded sub-agent").
The comment beside `closes` said nothing draws it any more since ADR 0063 — still true of
delegations, made false in general by this item, so that comment was corrected in place
(`internal/tui/render.go`, already in FILES). The seam itself is left as the spec words it.

**What.** Closes the rest of `ISSUES.md:28`. After item 1 a skill fetch is solo, so two adjacent
fetches paint as two standalone blocks; the defect asks for the delegation shape — one umbrella,
one row per call. Depends on item 1.

Bind the delegation grouping to a NAME rather than to delegations: generalise
`subAgentGroup` (`internal/tui/transcript.go:1694-1708`) and `subAgentGroupAt` (`:1721-1740`) into
`ownGroup(entries, i, name)` / `ownGroupAt(entries, i, name)` over a renamed `groupBlock`
(was `subAgentBlock`, `transcript.go:1632-1635`), keeping the `at += 1 + span` block step so a
span-0 skill call walks it unchanged; `subAgentGroup`/`subAgentGroupAt` stay as one-line wrappers
passing `subAgentToolName`, so delegation behaviour is byte-identical. One deep mechanism, two
participants — not a second parallel derivation.

`resolveBlock` (`internal/tui/render.go:551-696`) gains a skill-group branch asked AFTER the two
delegation branches (`:566`, `:644`) and BEFORE the super-group branch (`:669`), with a new
`blockShape` value `shapeSkillGroup` (`internal/tui/paintcache.go:64-71`). It paints through the
existing one-layer painter `renderSubAgentGroup` (`internal/tui/subagentblock.go:425-468`): the
umbrella label already comes from `groupLabelOf` (`:556-561`) reading `members[0].head.tool.Label`,
so it reads `Skill` with no new constant. Gate the four delegation-specific member swaps
(`subAgentScheduled`, `collapsedSubAgentView`, `unframedSubAgentView`, `subAgentFinished`,
`:445-458`) on `head.tool.headsRun()`; an unspanned, ungated member already falls to the shared
`renderGroupMember` (`internal/tui/toolblock.go:271`), which is the inline expand a skill row wants.
A skill row must NOT open a run view — `opensRun`/`headsRun` stay name-keyed on `sub_agent`, so
nothing extra is needed there. Name the new file, if any, in `internal/tui/doc.go` (`docmap_test`).

**Regression guard.** Acceptance drops the -run regex and runs `go build ./... && go test
./internal/tui/`, same reason as item 1 — the four delegation pins the edited member swaps sit under
(`TestSubAgentScheduledUntilItStarts`, `TestSubAgentMemberDoneOnItsOwnFinishedPhase`,
`TestUnframedSubAgentShowsThePromptWhenExpanded`, `TestNoteBetweenSiblingDelegationsKeepsOneGroup`)
are what "byte-identical" rests on, and all four are in this package.

**Files:** `internal/tui/transcript.go`, `internal/tui/render.go`, `internal/tui/subagentblock.go`, `internal/tui/paintcache.go`, `internal/tui/doc.go`, `internal/tui/transcript_test.go`, `internal/tui/subagentblock_test.go`, `internal/tui/toolshape_test.go`

**Tests.** A sketch test painting two and three adjacent fetches and asserting the exact rows:
header `✦ Skill (2)`, then `┝`/`┕` member rows carrying each call's target — the same shape
`TestRenderSubAgentGroupSketchStates` (`subagentblock_test.go:59`) pins for delegations. A breaker
test: a narration entry between two fetches yields two groups. A mixed test: a fetch between two
`read_file` calls breaks the run into two super-groups and one skill group, in time order. A
regression test that `subAgentGroup` still returns exactly what it did (delegations untouched), and
that a skill member row opens INLINE, never a run view.

**Acceptance.** `go build ./... && go test ./internal/tui/`

`fix(tui): group adjacent skill fetches under one Skill umbrella`

---

## 3. The skill card's spec and register line

**What.** The spec is canon and currently says neither thing. Depends on item 2.

`docs/layout/tool-layout.md`: extend the Rules bullet at `:44-47` so it names skill fetches beside
sub-agent calls as grouping with their own kind and never joining a super-group; extend the
**super-group** Vocabulary entry's breaker list (`:58-64`) the same way; add a `## Grouped Skills`
sketch after `## Grouped Sub-agents` (`:213-246`) showing `✦ Skill (<group-count>)` over `┝`/`┕`
rows, and stating that a member opens INLINE — the one way this group differs from the delegation
group it borrows its shape from. Add the `load_skill` row to the per-tool table (`:261-292`):
`load_skill | Skill | the loaded skill (the query until one is) | — | the skill body`.

`layout.md:679-690` enumerates the never-group tools ("the `Sub-Agent` call, which heads a whole
delegation …  That flag is the one deliberate never-group switch"): it now names two, so state the
RULE — a presenter marks a call solo when it groups with its own kind instead — and name both.

`internal/tui/doc.go:653-664` narrates `groupable`/`solo` and lists the solo tools; add the skill
fetch there.

Then retire `ISSUES.md:28`: delete the entry (`AGENTS.md` — a resolved item leaves this file; the
changelog is the closed trail). Leave the second defect's entry standing.

**Regression guard.** The sweep is a RULE, not the three sites named above: every comment or spec
line enumerating the never-group/solo tools or the super-group's breakers is restated, found with
`grep -rn 'solo\|never-group\|breaks the run\|never joins' internal/tui/*.go layout.md
docs/layout/tool-layout.md`. It reaches three in-tree comments that name the sub-agent call as the
only one and go stale here: `toolview.go:578-586`, `toolbranch.go:367-370`, `transcript.go:1596-1600`.
Acceptance runs `go build ./... && go test ./internal/tui/` plus the two greps already named.

**Files:** `docs/layout/tool-layout.md`, `layout.md`, `internal/tui/doc.go`, `internal/tui/toolview.go`, `internal/tui/toolbranch.go`, `internal/tui/transcript.go`, `ISSUES.md`

**Tests.** None — documentation. The behaviour these paragraphs describe is pinned by item 2.

**Acceptance.** `go build ./... && go test ./internal/tui/`; `grep -c 'load_skill' docs/layout/tool-layout.md` ≥ 1; `grep -c 'Load-skill tool calls' ISSUES.md` = 0.

`docs(layout): spec the Skill card and its own group`

---

## 4. `footerFit` — the footer's priority-ordered fit, as a pure function

**What.** The arithmetic half of `ISSUES.md:30-35`. Today `footerModeSpan`
(`internal/tui/model.go:3009-3022`) returns `ok=false` the moment the left run and the marker do
not both fit, and `footerContent` (`:2919-2924`) then truncates the whole left run and drops the
mode marker whole. New pure function `footerFit` in a NEW file `internal/tui/footerfit.go`, no
behaviour wired yet (item 5 does that).

Shape: one composer returning the WHOLE layout, so the painter and the pointer read one value
rather than two arithmetics that agree today — `footerFit(in footerInput) footerLayout` where
`footerInput` carries the five plain segment strings (host, model, effort, workdir, offline), the
already-composed mode marker text, the window width, the margin width and a `widthAuthority`, and
`footerLayout` carries `{info, offline, mode string, col int, hasMode bool}`.

Binding rules. Measure with the injected `widthAuthority` only (ADR 0030 §5) — no `lipgloss.Width`,
no `ansi.StringWidth`. Segments join with `" ✦ "` and a dropped one leaves WITH its separator, the
existing `nonEmpty` idiom (`model.go:3588-3597`). "Fits" is today's test: at least one blank column
between the left run and the marker. Ladder, in order, stopping at the first fit: drop the effort
word (priority 3), drop the workdir (2), drop the host (1); then, with only the priority-0
elements left — truncate the model with `…`, drop the model whole, drop `offline` whole; and only
if the marker cannot seat whole between its two margins, `hasMode=false` and the left run is
truncated to the window (today's floor, kept so a clipped mode word never names a wrong blast
radius). The mode marker is ONE atom: Auto's blast-radius word never splits from it.
Name the new file in `internal/tui/doc.go` (`docmap_test.go:15`).

**Regression guard.** The `unused` concern is rejected on the tree: `.golangci.yml` sets no
`run.tests: false`, so golangci-lint v2 counts a function used from `footerfit_test.go` as used, and
`make check` is the closeout's single run (AGENTS.md), by which time item 5 has wired it — no merge
or split of items 4 and 5. Acceptance runs `go build ./... && go test ./internal/tui/`.

**Files:** `internal/tui/footerfit.go`, `internal/tui/footerfit_test.go`, `internal/tui/doc.go`

**Tests.** A table over widths asserting the exact drop order for a full five-segment footer, and
that `mode` is non-empty at every width where the marker seats. Cases for: each single drop; the
priority-0 floor (model truncated, then gone, then `offline` gone); `hasMode=false` below the
marker's own floor; a footer whose effort word is already absent skipping straight to the workdir;
`offline` surviving every ladder drop. Assert `col + Width(mode) + margin == w` whenever
`hasMode`.

**Acceptance.** `go build ./... && go test ./internal/tui/`

`feat(tui): a pure priority-ordered fit for the footer's segments`

---

## 5. The footer paints from `footerFit`

**What.** Closes `ISSUES.md:30-35`: on a narrow window the mode marker vanishes today, so the human
cannot see which blast radius the session is running in. Depends on item 4.

Route both halves through the one composer: `footerLeftText`
(`internal/tui/model.go:2973-2985`) hands its five plain segments to `footerFit` instead of joining
them itself; `footerModeSpan` (`:3009-3022`) returns `(layout.mode, layout.col, layout.hasMode)`;
`footerContent` (`:2916-2958`) paints `layout.info`, `layout.offline` and `layout.mode` — so the
painted cells and the cells `handleFooterModeClick` (`internal/tui/mouse.go:497-511`) addresses stay
one value. Keep every existing property: the `stripEscapes` seam on host, model and effort default;
`bodyIndent` lead; the black field padded to the full window; the marker ending `bodyIndent` short
of the edge. One fix rides along — in today's narrow branch the whole line goes through one
`footerText.Render` and `offline` loses its error tone; painting from the layout gives it back its
own styled run at every width, as `:2950` already does in the wide branch.

**Regression guard.** The footer's pinned surface is NOT confined to internal/tui. Files gains
`cmd/apogee/e2e_hostile_test.go`, `cmd/apogee/e2e_delegation_test.go` and
`cmd/apogee/e2e_subagent_view_test.go`; the item must keep green the two 60-column smoke assertions
that anchor workdir redaction on the model's trailing " ✦ " (`cmd/apogee/e2e_hostile_test.go:520`,
`cmd/apogee/e2e_delegation_test.go:436`) and the golden frames whose chrome row is "  model ✦ dir"
(`cmd/apogee/e2e_subagent_view_test.go:507,530,605`) — a segment this plan may now drop cannot be the
anchor those pins rely on. Acceptance runs `go build ./... && go test ./internal/tui/ ./cmd/apogee/`.
Two more conditions: the model slot handed to `footerFit` is
`strings.Join(nonEmpty(m.upstreamSegments()...), " "+glyphAssistant+" ")`, never `[0]`, so that
slice keeps its stated promise (`model.go:3044-3045`); and ADR 0060 decision 6
(`docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md:96-108`) still says
the effort word truncates with the left run instead of dropping whole — the ladder supersedes that
sentence, and item 6's sweep is where the ADR is amended.

**Files:** `internal/tui/model.go`, `internal/tui/model_test.go`, `internal/tui/mode_test.go`, `internal/tui/mouse_test.go`, `cmd/apogee/e2e_hostile_test.go`, `cmd/apogee/e2e_delegation_test.go`, `cmd/apogee/e2e_subagent_view_test.go`

**Tests.** A width sweep over `m.footerContent(w)` asserting the emitted row itself: at 120/80/60/40
it names `⏵⏵ auto`, and the segments leave in ladder order (effort, then workdir, then host) with no
dangling `✦`. A sweep like `TestStatusLineIndentFitsNarrowWindow` (`model_test.go:4347`) asserting
`m.th.measure.Width(m.footerContent(w)) == w` for `w` in `{0,1,2,3,10,20,40,80}` — the footer has
no such sweep today. Revise `TestFooterMarkerSaysWhetherAutoIsConfined` (`model_test.go:5947-5951`),
which pins the DEFECT: at 30 columns the marker must now be present. Revise the narrow case of
`TestClickOnTheFooterModeMarkerOpensTheModePicker` (`mouse_test.go:4500-4510`) to a width where the
marker truly cannot seat. Keep `TestFooterModeMarkerSpanAgreesWithThePaintedCells`
(`model_test.go:6309`) green, extended to a narrow width. An offline test asserting the error tone
survives the narrow branch. Move the narrow case of `TestFooterContentStripsEscapes`
(`model_test.go:5893-5906`, today's width 30) to a width below the marker's own floor, and correct
the comment saying why two widths are read: at 30 the marker now seats, so both widths would take
the wide branch and the narrow branch's escape strip would be covered by nothing.

**Acceptance.** `go build ./... && go test ./internal/tui/ ./cmd/apogee/`

`fix(tui): keep the mode marker on the footer by dropping segments in priority order`

---

## 6. The footer's spec, narration and register line

**What.** `layout.md:1437-1443` states the behaviour this plan replaces, verbatim: *"A window too
narrow for both ends keeps the older shape: the left info truncates with an ellipsis and the mode
drops whole …"*. Depends on item 5.

Rewrite that sentence as the priority rule: the row is composed TO the width, spending it in the
order the row is read for — the effort word goes first, then the workdir, then the host; the model,
the `✦ offline` marker and the mode marker are what the row never gives up, the model truncating
before it goes; and the marker drops only where it cannot seat whole, which is still why a clipped
mode word never names a blast radius the session is not in. Keep the click paragraph at `:1425-1435`
consistent — "where the window drops the marker whole … a click on the footer names nothing" now
describes a much narrower window, not the ordinary narrow one.

`internal/tui/doc.go:29-32` narrates the footer as "host alias ✦ model ✦ workdir" — stale before
this plan (no effort word) and stale after it (no fit rule); restate it to name the five segments
and the priority fit.

Then retire `ISSUES.md:30-35`: delete the entry and the "Open defects" section with it if both
entries are now gone, leaving the heading in place per the file's own conventions.

**Regression guard.** The rewrite is a RULE, not the two prose sites named above: every comment, ADR
paragraph or spec sentence naming the footer's narrow branch is restated, found with
`grep -rn 'drops WHOLE\|drops whole\|truncates with an ellipsis' internal/tui layout.md docs/adr`.
That reaches `internal/tui/model.go:2908-2909` and `:3002-3003`, and ADR 0060 decision 6
(`docs/adr/0060-…:96-108`), whose 2026-08-28 amendment this plan supersedes — say so there.
Acceptance runs `go build ./... && go test ./internal/tui/` plus the two greps already named.

**Files:** `layout.md`, `internal/tui/doc.go`, `internal/tui/model.go`, `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`, `ISSUES.md`

**Tests.** None — documentation. The behaviour is pinned by items 4 and 5.

**Acceptance.** `go build ./... && go test ./internal/tui/`; `grep -c 'the mode drops whole' layout.md` = 0; `grep -c 'bottom most status bar' ISSUES.md` = 0.

`docs(layout): spec the footer's priority-ordered fit`

---

**Suggested version bump (owner's call, not performed):** a patch bump — both items are user-visible
fixes to shipped surfaces with no config or API change.
