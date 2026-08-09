# Status line: keep the tool verb, drop the path

- **Goal:** the running status line above the prompt shows `spinner · delegation-name · verb · elapsed` plus the right-slot throughput and context gauge — never the tool's target path. The path is redundant with the tool-call block in the transcript and routinely pushed the context gauge off the row.
- **Date:** 2026-08-09
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:** `layout.md` (status-line sections), `internal/tui/activity.go` / `internal/tui/toolpresent.go` / `internal/tui/render.go` at commit `f35568c`, and the ratified design calls below. If item text disagrees with these sources, the sources win.
- **Ratified design calls** (owner, 2026-08-09, via AskUserQuestion):
  1. During tool activity the status line shows the presented **verb only** (`reading`, `running`, …) — no target path, no truncated form of it. Non-tool phrases (`responding`, `stopping`, idle facts) are unchanged.
  2. The elapsed timer **restarts on each new tool call**, keyed on the call's identity (its call ID), not on the phrase text. Same-call streaming updates do not restart it. Non-tool activity kinds keep today's restart-on-phrase-change behaviour.
  3. Scope is the **status line only**: the collapsed sub-agent gist (`subAgentGist`, `internal/tui/render.go:600`) keeps its `name · verb · shortened-path` wording — inside a collapsed run it is the only live view of what the sub-agent touches.
- **Skills:** coding-standards
- **Out of scope:**
  - `toolPhrase`, `statusTargetCells`, `expandTabs` (`internal/tui/activity.go:140–187`) — still required by `subAgentGist`; do not change their behaviour.
  - Transcript tool-call blocks and their presentation registry (`toolpresent.go` verb/target settling stays as is).
  - Engine/domain event types (`domain.ToolCallEvent` is folded by the transcript too — no plumbing changes upstream of the TUI model).
  - Any VERSION / CHANGELOG / tag change (see closing note).

## 1. Verb-only status label with per-call clock restart — ✅ DONE (2026-08-09)

NOTES (2026-08-09): the clock is keyed on `e.Call.ID`, not on the `EventBase.CallID` the item text names — that field is the SPAWNING run's identity (`domain/events.go:40`, already carried as `activity.spawn`) and is identical across every call one agent makes, so it cannot key a per-call restart. Ratified call 2's "the call's identity (its call ID)" is `domain.ToolCall.ID`; sources win over item text.
NOTES (2026-08-09): `fanout_test.go:137,142` were left unchanged — both assert the collapsed run's live tail (`plainRender` → `subAgentGist`), which ratified call 3 preserves; that file holds no status-line assertion.
NOTES (2026-08-09): `TestFoldActivityClockRunsPerPhrase` gained the per-call cases rather than being replaced by them — its token-stream and phrase-change assertions pin the non-tool restart rule, which still stands.

**What:**
In `internal/tui/activity.go`:

- `foldActivity` (`activity.go:234`) stops deriving the tool label from `toolActivityLabel` (`activity.go:158`). Instead it stores only the presented verb — obtained from the same `presentToolCall` view (`toolView.Verb`, `toolpresent.go:166`) — as the `activity.label` for `actTool`. A small verb-only helper beside `toolPhrase` is the expected shape; **do not** alter `toolPhrase` itself (`activity.go:170`) — `subAgentGist` (`render.go:612`) deliberately shares its exact wording and expects the target.
- Clock restart (`setActivity`, `activity.go:193–194`): today the elapsed clock restarts when the label text changes; with verb-only labels, back-to-back same-verb calls would no longer restart it. Per ratified call 2, key the restart on the tool call's identity: carry the event's call ID (available on `domain.ToolCallEvent`'s `EventBase.CallID` in `foldActivity`) into the `activity` struct (`activity.go:47–64`) and restart `since` whenever the call ID changes. Non-tool kinds keep the existing label-change rule. The exact field/parameter shape is the implementer's choice under coding-standards.
- If `toolActivityLabel` and its `widthAuthority` threading are now dead, delete them. The 32-cell cap machinery (`statusTargetCells`, `expandTabs`, the `activity.go:171–185` rationale block) stays — `toolPhrase`/gist still needs it; trim the "keeps the gauge on the row" justification comment at `activity.go:140` to its remaining gist purpose.

**Tests** (all in `internal/tui/`; the phrase edits are one-line assertion swaps from `"reading · <path>"` to `"reading"`):

- `model_test.go:3455` `TestModelStatusLineActivity` — status text for a tool call becomes `reading` (assertion at `:3480`); the `responding` / `stopping` / idle cases stand.
- `activity_test.go` — `TestActivityText` (`:17`), `TestFoldActivitySequence` (`:184`), `TestFoldActivityDepthPrefixesSubAgent` (`:245`), `TestStatusPhraseNamesTheActingDelegation` (`:271`), `TestStatusPhraseDropsTheNameWhenTheParentResumes` (`:312`), `TestFoldActivityBatchStaysOnTool` (`:331`) — verb-only expectations. `TestToolActivityLabel` (`:55`, incl. the cap check at `:105–118`) follows its subject: repoint at the verb-only helper, and move the 32-cell cap coverage to `toolPhrase` if it is not already covered there. `TestFoldActivityClockRunsPerPhrase` (`:222`) becomes the per-call-restart test: two consecutive same-verb calls with distinct call IDs each restart the clock; a streamed update within one call does not.
- `fold_test.go:94,267,288` — `wantPhrase` values become verb-only.
- `fanout_test.go:137,142` — verb-only status expectations (the gist-side path assertions in that file stand, per ratified call 3).
- `workspacepath_test.go:359` `TestActivityLabelSharesTheWorkspaceRelativePath` — its subject (workspace-relative path in the shared phrase) now lives only in `toolPhrase`/gist; repoint the test there.
- `transcript_test.go:684` `TestToolActivityLabelCarriesNoEscape` — repoint the escape-stripping guard at whatever now produces the status phrase and at `toolPhrase`.
- `interject_test.go:753` (`TestSuppressedBandKeepsItsCountOnTheStatusLine`) — seed a verb-only label; the trimming-order behaviour under test is unchanged.
- `paint_test.go:920` `TestPaintedTabBearingToolTargetKeepsTheGauge` — rework its premise: assert that a tool call with a long, tab-bearing target never contributes the target to the status line at all and the gauge renders fully (the `:949–953` gauge-vs-statusLine comparison can stay as the mechanism).
- New (or folded into the reworked paint test): a tool call with a very long path → the rendered status line contains no path fragment and the context gauge is complete at its right edge.

**Acceptance:**
- `go build ./...`
- `go test ./internal/tui/`
- `grep -n '"reading · ' internal/tui/model_test.go internal/tui/fold_test.go internal/tui/fanout_test.go` → any remaining hits are gist/tool-block assertions only, none against the status line.

**Commit:** `feat(tui): the status line keeps the tool verb and drops the path`

## 2. Docs follow the verb-only status line — ✅ DONE (2026-08-09)

NOTES (2026-08-09): the `⣾ read… · 5 queued` example at `layout.md` "And what the left slot sheds" was kept verbatim instead of being rewritten — it is still exactly what renders under a verb-only phrase (20 columns − 2 lead − 11 for ` · 5 queued` = 7 cells; `⣾ reading · 3s` truncated to 7 cells is `⣾ read…`), so restating it would have made the spec wrong. The prose the item asked for was added beside it: verbs are short, so trimming is now the rare case and the stated order is what happens on the windows that still force it.
NOTES (2026-08-09): the gist paragraph's phrase example was corrected from `reading main.go` to `reading · main.go` — `toolPhrase` (`activity.go:175`) joins verb and target with ` · `, and the paragraph was being reworded around that exact claim.
NOTES (2026-08-09): `layout.md:1016` ("The phrase and the elapsed clock beside it…") names no path, so it was left unchanged, as the item's "adjust only if it names the path" allows. Historical `CHANGELOG.md` entries quoting the old phrase were left alone — they are the record of shipped releases, and the plan's closing note bars CHANGELOG edits.

Depends on item 1.

**What:** update every prose description of the status line's tool phrase; the gist's wording claims change from "same as the status line" to naming the shared `toolPhrase` view.

- `layout.md:52` — the master frame sketch: `⠉⠹ reading · main.go · 3s …` loses the path (`⠉⠹ reading · 3s …`).
- `layout.md:546` (in "The rules behind the tool-call sketch") — "The status line's live phrase reads the same shortened path the block beneath it will" is no longer true; reword: the **collapsed sub-agent gist** shares that shortened path; the status line carries the verb alone.
- `layout.md` ~`821–861` (in "Collapsed and expanded blocks") — the gist "reuses the same activity phrase the status line shows": reword to say the gist keeps verb + shortened path while the status line shows the verb only.
- `layout.md:1031` section "The status line's right slot", sub-section "And what the left slot sheds" (~`:1069`) — update the `⣾ read… · 5 queued` example and the trimming-order prose to a verb-only phrase (verbs are short; the phrase-truncation path still exists but rarely fires).
- `layout.md:1007` "The status line's spinner" — check `:1016` ("The phrase and the elapsed clock beside it…"); adjust only if it names the path.
- `internal/tui/doc.go:251–258` — the activity-phrase paragraph (quotes `"reading · main.go · 3s"`) rewritten for verb-only labels and the per-call clock restart rule.
- `internal/tui/doc.go:557` — the escape-stripping list names `toolActivityLabel`; update to the helper that replaced it in item 1.
- `CONTEXT.md` needs no change (it does not describe the status line's contents).

**Tests:** none — prose-only (doc.go compiles: covered by the build).

**Acceptance:**
- `go build ./...`
- `grep -n 'reading · main.go' layout.md internal/tui/doc.go` → no hit describes the status line; remaining hits (if any) are gist/tool-block contexts.
- `grep -n 'toolActivityLabel' internal/tui/doc.go` → no stale reference.

**Commit:** `docs(tui): layout spec and doc comments follow the verb-only status line`

---

**Suggested version bump** (not performed): patch-level — a small user-visible TUI refinement with no API change. Fold a one-line entry into the next release's CHANGELOG section when the owner cuts it; no VERSION/CHANGELOG heading was touched by this plan.
