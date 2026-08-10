# Doc-landscape cleanup plan — 2026-08-10

Status: not started

Source: `docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md` (full evidence
and file:line cites live there; items below carry the operative subset).

## Ratified design calls (owner, 2026-08-10)

- Scope: live core docs + `docs/design/` + `docs/skill-runs/`; archives and
  CHANGELOG untouched.
- Prune = real docs → `archived/` folders; machine junk deleted + gitignored.
- `layout.md` + `docs/layout/` stay normative specs; README stays the full
  manual; `docs/design/` is normative-or-archive with why-salvage into ADRs.
- Code wins over descriptive docs; spec/code mismatches were flagged both ways
  (result: zero code bugs in the spec slices; one code gap recorded in item 9).
- Plan-writer calls (overridable at execution): dependency policy salvage lands
  as a **new ADR**; hook-mutation salvage lands as amendments to ADR 0001 and
  ADR 0022; archived design docs move to a new `docs/design/archived/`
  (mirrors `plans/archived/` and `handoffs/archived/`); the prompt-box mockup
  moves to a new `docs/layout/archived/`.
- Out of scope, deliberately: the `docs/layout/tool-layout.md` working-tree
  sketch (owner's in-flight design; needs its own plan), the model.go/wire.go/
  config-cluster refactors (separate plan if wanted), any VERSION/CHANGELOG
  release-heading change, and the coding-standards skill edit (separate
  owner decision).
- CHANGELOG: each item adds its line under `[Unreleased]` per house practice;
  no release headings, no VERSION change.

## 1. Archive superseded docs and stop tracking skill runs — ✅ DONE (2026-08-10)

What: `git mv "docs/handoffs/2026-08-03 - 00 - readme-drift-fix.md"` to
`docs/handoffs/archived/`. Create `docs/layout/archived/` and `git mv`
`docs/layout/prompt-box-layout.md` into it. `git rm -r` the finished skill-run
dirs (`docs/skill-runs/code-audit-sequential/2026-08-09/`,
`docs/skill-runs/implement-plan/2026-08-01_-_00_-_rename-prompt-window-plan/`,
`docs/skill-runs/implement-plan/2026-08-03_-_01_-_session-name-on-the-top-rule-plan/`,
`docs/skill-runs/implement-plan/2026-08-03_-_02_-_naming-call-thinking-budget-plan/`,
`docs/skill-runs/refocus/2026-08-02/`), keep
`docs/skill-runs/implement-plan/2026-08-10_-_00_-_tool-surface-improvements-plan/`
on disk AND tracked until that plan closes, and add `docs/skill-runs/` to
`.gitignore` (the live dir stays tracked because gitignore does not untrack;
remove it with `git rm -r --cached` at that plan's closeout).

Tests: none (docs only).

Acceptance: `git status` clean of skill-run noise after a fresh skill run
writes files; the two moved docs exist only under their `archived/` homes;
the live 2026-08-10 ledger is still tracked; `make check` green.

Commit: `chore(docs): archive superseded docs and stop tracking skill runs`

NOTES (2026-08-10): the tree contradicted two of this item's assumptions, so
the end state was reached differently. (a) Nothing under `docs/skill-runs/` was
ever tracked — an untracked `docs/skill-runs/.gitignore` holding `*` was
already suppressing the whole tree — so `git rm -r` had no index entries to
remove and the five finished run dirs were deleted with `rm -rf`. The root
`.gitignore` gains `/docs/skill-runs/` (anchored, matching the file's style) so
the rule is checked in rather than living in an untracked file; the redundant
nested `.gitignore` was left alone as untracked machine-local state. (b)
`implement-plan/2026-08-10_-_00_-_tool-surface-improvements-plan/` does not
exist (that plan was archived in `94d63d5`), so there was no live ledger to
keep tracked; the only live run dir is this plan's own
`2026-08-10_-_01_-_doc-landscape-cleanup-plan/`, kept on disk and untracked
like the rest.

## 2. AGENTS.md and README truth pass — ✅ DONE (2026-08-10)

What: AGENTS.md — replace the stale distribution bullet with: Homebrew tap
`airiclenz/tap` + six prebuilt archives per release (`make dist`);
build-from-source supported; never `go install @latest` (stale v1.7.0 proxy
cache). Reword the "CHANGELOG + VERSION in step" bullet to describe actual
practice (per-feature VERSION micro-bumps; CHANGELOG headings at release cut).
README — fix `:70` Status to `v0.12.x`; refresh the `:90` "Newest on `main`"
sentence; fix the `:1014–1017` install-order sentence to match
`Makefile:60` `INSTALL_CANDIDATES` (`~/.local/bin` before `/opt/homebrew/bin`)
and fix the same inversion in the Makefile comment at :47–50; complete the
`:1011` `make check` row (ADR-0010 import invariant + `--help` smoke); soften
`:1035` to "standard on any Unix-like box"; bump the `:130` example to the
current release; add a ~5-line "Reading the code?" pointer to `AGENTS.md`
(single map, no duplicated repo tour).

Tests: none (docs + one Makefile comment).

Acceptance: no README claim contradicts the working tree on the audited points;
`grep -n "v0.11" README.md` returns nothing; `make check` green.

Commit: `docs: truth pass over AGENTS.md and README against the working tree`

## 3. CONTEXT.md micro-fixes and their code-comment twins — ✅ DONE (2026-08-10)

What: `:305` add the Schedule pair to the mid-run verb list (ADR 0033);
`:356` respell `web_fetch`/`http_request`; `:636–637` name error enrichment as
the post-tool-result resident and mark `correct_tool_result` "(deferred
2026-07-04; bench-side experimental hook)". Fix the same staleness in the code
comments at `internal/tui/command.go:262` and
`internal/domain/mechanism.go:53–54`.

Tests: existing suite only (comment-only code edits).

Acceptance: the three CONTEXT.md spots match code; both code comments name the
current truth; `make check` green.

Commit: `docs(context): sync mid-run verbs, tool spellings, and the post-tool-result resident`

## 4. layout.md and user-questions-layout.md drift fixes — ✅ DONE (2026-08-10)

What: layout.md — `:2–4` gutter numbers to one-beside-bar / two-at-edge (agree
with :448); `:459–462` scrollbar paragraph to "one re-wrap per deliberate
change — /settings applies live (ADR 0037), config file read at start-up";
`:54` current placeholder legend; `:42–46` redraw the Sub Agent mockup to the
⤷-railed run with `N tool calls · fill · gist` head; `:1318–1320` "the verbs
that take arguments — today six" (drop the hard count or list all six).
user-questions-layout.md — strike the `[1]/[2]/[3]` digit-shortcut rule with an
amendment note (ratified out 2026-08-04), add the hint row to the multi-option
mockup, lowercase `command:`. Fix the riding stale code comments:
`internal/tui/theme.go:112–113` ("fixed for the process lifetime") and
`internal/tui/autocomplete.go:31` ("the one verb — /confine").

Tests: existing suite only (comment-only code edits).

Acceptance: every audited layout.md mismatch reads to the code's truth; the two
code comments match behavior; `make check` green.

Commit: `docs(layout): sync spec drift and the two stale renderer comments`

NOTES (2026-08-10): two departures from the item's literal text. (a) Redrawing
the `:42–46` mockup also corrected its label from `Sub Agent` to the `Sub-Agent`
the presenter actually paints (`toolpresent.go:501`), so the one companion
reference to that sketch (`layout.md:758`) was respelled with it — otherwise the
file would name a block it no longer draws. (b) In
`user-questions-layout.md` the struck digit rule got the file's own
pinned-sketch treatment: the prose rule is struck through and a blockquote
amendment note (the convention the multi-select section already uses) says the
sketch's `[1]/[2]/[3]` cells are not painted, rather than the cells being erased
from the owner's pinned mockup. Left alone as out of scope: the file's `:2`
Status blockquote still cites the pre-archive path of the 2026-08-04 - 03 plan.

## 5. mechanism-catalogue: add the missing guided_decomposition row — ✅ DONE (2026-08-10)

What: add the `guided_decomposition` row to Table A (sourced from ADR 0014 and
`internal/mechanisms/guided_decomposition.go`: proactive-nudge capability,
strikes-3 suppression, `Requires [tool_result_cap]`,
`IncompatibleWith [decompose, truncate_history]`, `After toolfilter`,
depth-0 gate) plus its Table B / ledger line (shipped via ADR 0014, default
off, bench status per the ADR's gate). Update the one-sided F7 note in the
`truncate_history` row to cite the full coupling.

Tests: none (doc only; `internal/validated/shipped_test.go` still passes —
no shipped.json change).

Acceptance: Table A row count equals the code catalogue's 21 registrations;
README's ":21 mechanisms → see the catalogue" promise is honest.

Commit: `docs(catalogue): add the guided_decomposition row (ADR 0014)`

NOTES (2026-08-10): acceptance arithmetic, stated exactly. Table A now holds 22
physical rows: the 21 code registrations plus the `correct_tool_result` DEFER
row, which has never been a registration (owner-ratified 2026-07-04, not
ported). So "Table A row count equals the code catalogue's 21 registrations"
holds on registered rows — the count the audit used when it read Table A as
listing 20. Two additions beyond the item's literal text, both to keep the
surrounding text true once the row exists: a footnote ⁵ marking
`guided_decomposition` as the first catalogue row that is not a port (the Sim
columns are empty by construction and the table's header prose otherwise
promises a sim spelling for every row), and one sentence in the ledger preamble
saying "closed" means the porting question is settled, not that the table is
frozen — the preamble declared the ledger closed 2026-07-04 and a new line
under it would otherwise read as a contradiction.

## 6. Salvage orphaned "why"s into ADRs — ✅ DONE (2026-08-10)

What: (a) new ADR — the dependency policy from technical-design.md §3
(:142–147): external programs are runtime-detected optional enhancements,
never hard prerequisites; one bounded exception (confinement); single static
CGO-free binary; stdlib-first, lean module graph. Cite `autofix.go` standing
requirement #2 as realisation. (b) Amend ADR 0001 with two recorded decisions
from hook-mutation-api.md §8: mutation is index-addressed, never raw-slice
(hooks get snapshots; the loop keeps backing storage), and `Message.Content`
is string-only with unknown structure preserved in `Extra` (revisit on a
vision target). (c) Amend ADR 0022 with the `ToolOutcome` tri-state rationale
from hook-mutation-api.md §5 amendment 2026-08-02 (read_loop IsError loss,
string-sniffing failure, `tool_outcome` snapshot sibling + pre-marker
fallback).

Tests: none (docs only).

Acceptance: each salvaged "why" is findable in `docs/adr/` without opening the
source docs; new ADR follows house numbering/format.

Commit: `docs(adr): salvage dependency policy, mutation discipline, and ToolOutcome rationale`

NOTES (2026-08-10): two departures from the item's literal text. (a) All three
salvages cite the two source docs at their **post-item-7 archived paths**
(`docs/design/archived/technical-design.md`, `.../hook-mutation-api.md`) rather
than their current live paths — item 7's acceptance grep forbids the live paths
in any non-archived `*.md`, and it does not list the new ADRs among the
references it repoints, so the forward cite is what makes the two items
consistent; the links resolve the moment item 7 lands. (b) The ADR 0022 salvage
is headed "Addendum", the word that file uses for all five of its own later
sections, rather than the "Amendment" heading ADRs 0001/0012/0013/0018 use; ADR
0001's salvage uses "Amendment" for the same reason. Nothing else in either
amended ADR was touched.

## 7. Archive technical-design.md and hook-mutation-api.md — ✅ DONE (2026-08-10)

What: create `docs/design/archived/`; `git mv` both docs into it. Repoint live
references to the new paths: `TODO.md:362` (keep its stale-line-refs caveat),
`docs/design/mechanism-catalogue.md` (×4 hook-mutation cites). Code comments
citing §-numbers stay (they cite history). Add a one-line tombstone note at the
top of each archived doc: superseded-by pointers (ADRs from item 6, the
catalogue, CONTEXT.md). Note for future plan runs: technical-design §5 rows
are no longer amended — component narration stops here.

Tests: none (docs only).

Acceptance: `docs/design/` contains exactly the three kept contracts;
`grep -rn "docs/design/technical-design\|docs/design/hook-mutation-api" --include="*.md" .`
(excluding `archived/` and CHANGELOG) returns nothing; item 6 landed first.

Commit: `chore(docs): archive technical-design and hook-mutation-api after ADR salvage`

NOTES (2026-08-10): three things worth stating. (a) The "note for future plan
runs" (§5 rows no longer amended) is written into the technical-design tombstone
itself, since that is where a future plan run would read it. (b) Only one of the
catalogue's four hook-mutation cites (`:24`) was a full `docs/design/` path; the
other three (`:73`, `:102`, `:204`) name the file bare, so they were repointed in
their own style to `archived/hook-mutation-api.md` — still correct relative to
the catalogue's own directory — rather than being rewritten to full paths. (c)
The acceptance grep still returns two hits that are not live references and were
deliberately left: `docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md:39–40`,
the audit's own verdict table recording where the docs lived when it judged them
(same treatment item 1 gave that table's `prompt-box-layout.md` row), and this
plan file's own `:218`, which contains the grep pattern as a literal.

## 8. Amend the two kept contracts

What: confinement-execution-contract.md — §6.1 pinned signatures gain the
`Shell` third parameter (`confinetest.go:45,122`); §6.2 shell-line refs point
at the per-OS files (`lines_other.go`, `lines_windows.go`); reword the §4
"unreachable row" paragraph to the half-landed truth (row now reachable via
`resolveTargetUnbounded`; Execute-side reconciliation still open — cite the
ISSUES entry from item 9). mcp-client.md — `:93` `dispoGate`/"disposition
table" → `resolveGate`/"the Resolution ladder (`internal/agent/resolution.go`)";
`:26–27` "disposition (D5)" → "Resolution (D5)".

Tests: none (docs only).

Acceptance: both contracts read true against the working tree on the audited
points.

Commit: `docs(design): amend confinement contract and mcp-client to current truth`

## 9. TODO.md / ISSUES.md prune, refresh, and the new confinement gap — ✅ DONE (2026-08-10)

What: delete the OBSOLETE `/server` persistence bullet (`:46–48`, shipped via
ADR 0036); compress the four struck-through FIXED width narratives
(`:635–709`) to house-style closed-trail one-liners; refresh stale refs
(InjectContext → `hooks.go:504`; ConfineWritablePaths readers →
`dispatch.go:365/405`; AskRequest parenthetical notes `SubAgentTask`/
`SubAgentName` exist but depth still absent; schedule-tool entry cites
ADR 0034). ISSUES.md: remove the closed `[X]` shift+enter line. ADD one new
ISSUES entry: "approved out-of-workspace write errors at Execute" — the Gate
now reaches the row but `write_file.go:82`'s os.Root fence ignores approval;
decision pending (land the P3.7 reconciliation or ratify strict fencing and
amend contract §4).

Tests: none (docs only).

Acceptance: every remaining TODO item is LIVE per the audit; the new ISSUES
entry cites the contract §4 and both code sites; file shrinks by roughly the
audited 9–15%.

Commit: `docs(todo): prune shipped items, refresh stale refs, record the confinement execute gap`

NOTES (2026-08-10): four departures plus one acceptance number that cannot be met
as written. (a) The deleted `/server` persistence bullet was replaced by a
one-line entry in its parent's "Shipped since parking" ledger — TODO.md's own
header rule says a deferral trail never becomes a silent drop, and that ledger,
not the bottom **Closed entries** list, is where a sub-bullet of a still-live
entry closes. (b) The `AppendToSystem` sibling ref in the same paragraph as the
`InjectContext` one was refreshed with it (`:378` → `:481`) — the same file's
same drift, one sentence apart. (c) `ConfineWritablePaths` now has *two* readers
(`dispatch.go:365` tool call, `:405` hook-time subprocess), so the entry's
"exactly **one** reader" claim was reworded rather than renumbered; its
load-bearing "no writer" half still holds. (d) The new ISSUES entry cites
`workspace_scoped.go:102`, the `resolveTargetUnbounded` *definition*, where the
audit cited `:77`, one of its two call sites. (e) The 9–15% shrink is
arithmetically unreachable: the audit measured TODO.md at 854 lines, and the
2026-08-10 tool-surface section has since taken it to 910, so even deleting every
audited prunable line (3 + 75 = 78) is 8.6% — and this item also *adds* required
lines (the trail entry, two refreshed parentheticals, the ADR 0034 note).
Actual: 910 → 863 (−5.2%), with all the audited prunable content gone.

## 10. cmd/apogee package map

What: add `cmd/apogee/doc.go` at the standard the internal packages already
meet — package comment plus a file map of the 25 non-test files (config
cluster, wire.go, launcher, probe subcommands, settings rows, headless),
in the style of `internal/tui/doc.go`'s map (scaled down; no invariants
prose beyond what the binary layer actually owns).

Tests: none (comment-only file; compiles under `make check`).

Acceptance: `cmd/apogee` has a doc.go naming every non-test file with a
half-line role each; `gofmt` clean; `make check` green.

Commit: `docs(cmd): add the cmd/apogee package map`
