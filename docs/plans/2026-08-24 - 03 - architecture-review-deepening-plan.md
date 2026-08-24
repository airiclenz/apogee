# Architecture-review deepening (2026-08-24 review) — plan

**Goal:** land the four candidates of `docs/reviews/architecture-review-20260824.md` that survived
fact-checking against the code and the ADRs — (a) one journal funnel for every workspace writer
so `/undo` coverage becomes a construction property pinned by a registry-walk test; (b) one
`firingConfig` composer for every unattended run (headless, in-session Schedule, daemon), which
also fixes the in-session Firing's boot-snapshot drift; (c) one table behind the two settings
switches in `wire_settings.go`; (d) the contradictory "never persisted" contract text on
`domain.EditRegions`. Candidates 2 (Mechanism boilerplate) and 5 (typed `git_diff_range`) are
DENIED — see "Review verdicts" below.

**Date:** 2026-08-24
**Status:** ready
**sized for:** ~200k-context host

## Authoritative sources

- `docs/reviews/architecture-review-20260824.md` — the review. **Its claims were fact-checked
  before this plan was written; where an item below disagrees with the review, the item wins**
  (the review's file names `internal/security/path_safety.go` and `internal/tools/view_diff.go`
  do not exist; its counts are corrected in "Review verdicts").
- `docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md` decision 3 ("Capture is
  at the funnel, and that IS the coverage boundary") — items 3–5 make that sentence true.
- `docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md` §1 and
  §5 — item 1 reconciles the two.
- `docs/adr/0037-every-settings-edit-applies-to-the-running-session.md` — items 6 and 9 extend
  its promise to Firings raised inside the session.
- `docs/adr/0033-*` (Firings compose a fresh Config; `internal/run` is runner-agnostic and the
  CALLER composes the Config — `internal/run/run.go:22-25`) and `docs/adr/0031-*` (every Driver
  reaches the same engine behaviour from the same Config values) — item 7's composer lives in
  `cmd/apogee`, never in `internal/run`.
- `docs/adr/0010-*` layering — nothing in `internal/` gains a `cmd/apogee` dependency.
- `docs/design/confinement-execution-contract.md` §"workspaceScopedWriter" (lines ~305-360) —
  the marker family is the writer set items 3–5 cover.
- `internal/tools/network_funnel_test.go:73` (`TestDefaultTools_EveryNetworkToolIsURLFiltered`)
  and `internal/tools/workspace_scoped_test.go:106` (`TestWriteTargetProbesCoverEveryWriter`) —
  the two in-repo precedents for a registry-walk exhaustiveness test; item 5 copies their shape.
- `AGENTS.md` — `ISSUES.md` holds OPEN items only; resolved work goes to `CHANGELOG.md`
  `[Unreleased]` (the verifier owns the CHANGELOG entry per item).

## Review verdicts (fact-check, 2026-08-24)

| # | Review candidate | Verified state | Verdict |
|---|---|---|---|
| 1 | One write funnel | Gap real: copy/move/delete call `capturePreImage`/`commit` by hand (`file_ops.go:145,243-277`, `delete_file.go:101-106`); no test walks the writer registry for undo coverage; ADR 0051 §3 equates "funnel" with the `workspaceScopedWriter` family, false for 3 of its 7 members. A single `body func() error` funnel cannot express move's two-path/two-outcome shape. | **IN** — items 3–5 |
| 2 | Collapse Mechanism boilerplate | Already executed: ADR 0003 amendment 2026-07-25 ratified `register(row{…})` as the seam and clause (c) argues against further collapse; true pass-through count is 16 of 21, not 19; hooks are methods, not closures; 13 rows carry load-bearing ordering. | **DENIED** (owner, 2026-08-24) |
| 3 | Registry as one config home | Mostly shipped: env/flag names single-homed on `config.Key`; `TestRegistryIsBijectionWithFileConfig` and `TestEveryEditableSettingKeyHasAnApply` already exist. One real duplicate: the `unreachable` switch (`wire_settings.go:801`) mirrors the apply switch (`:566`). | **IN, reduced** — item 2 |
| 4 | One unattended-run composer | Two twins (headless `headless.go:243-485`, daemon `daemonfire.go:223-356`), not three; the in-session Schedule (`schedule.go:82-141`) inherits the TUI's BOOT Config and drifts against live `/settings` edits (`tools.disabled`, URL lists, `bypass`, `auto-compact`, `ui.inspector`, `context-files.*`, the `api-key-env` union). | **IN** — items 6–9 |
| 5 | Typed `git_diff_range` outcome | Contradicts ADR 0052 §2 verbatim ("the renderer recovers their positions … from parsing the `@@` headers git already emits"); git computes the diff, so nothing exists to type at source — the parser would only move packages. | **DENIED** (owner, 2026-08-24) |
| 6 | Codec persists render shape | Premise wrong: ADR 0052 §5 ratifies persisting the region structure; replay IS the codec blob (`model.go:582` → `transcript.replay`), nothing else can rebuild the split view. The defect is the contract TEXT (`toolsummary.go:31-34`, `:120-122`, ADR 0052 §1) contradicting §5. | **IN, as doc fix** — item 1 |

## Ratified design calls

Decided by the repo owner (Airic Lenz) on 2026-08-24, before this plan was saved:

1. **Scope** — candidates 1, 3 (reduced), 4, 6 (as doc fix) are IN; 2 and 5 are DENIED and
   recorded here only (no `ISSUES.md` entry).
2. **Undo funnel shape** — content verbs (`write_file`, `edit_existing_file`, the two
   find-and-replace tools) STAY on `safeWriteFile`; copy/move/delete route through ONE sibling
   multi-path helper, `journaledMutation`, beside it in `internal/tools/path_safety.go`. After
   item 4 those two helpers are the only callers of `capturePreImage` / `commit` /
   `commitReadBack`. Rejected: one generic funnel for all seven (the edit tools would have to
   thread their `okEditRegions` pre-bytes through it — double-read risk, more churn).
3. **In-session Firings honour live edits** — a Schedule Firing raised inside a TUI session
   composes from the session's LIVE-overlaid `config.Options` (every key `/settings` or the config
   watcher has applied), not the boot snapshot. Rejected: keep the snapshot and document the
   drift.
4. **Width source** — the in-session Firing keeps the session's `w.width()` parallel-agents cap;
   the composer takes a `width` function parameter (nil ⇒ `discoverSlots`, which is what headless
   and the daemon pass). Rejected: unify all three onto `ResolveParallelAgents` + a probe.
5. **Skills provider** — the in-session Firing shares the session's live `*skills.Provider` (so
   `use-project-skills` flips keep following); the composer takes it as an optional parameter
   (nil ⇒ a fresh provider from the roots, which is what headless and the daemon pass).
   Rejected: a fresh provider per Firing on every Driver.

Write-time calls by the plan author (mechanical, no user-visible difference):

6. ADR 0051 and ADR 0052 get dated **amendments** in place (the format of ADR 0003's 2026-07-25
   amendment), not superseding ADRs — no decision reverses; the prose is made true.
7. The composer lives in a new `cmd/apogee/wire_firing.go` (ADR 0043 file-by-concern), returns
   `(apogee.Config, []string, error)` where the strings are the rebind notices headless prints
   and the other two Drivers drop.
8. `journaledMutation`'s body reports per-path landing (`[]bool`, one per path) so move's split
   failure (`file_ops.go:272`, destination committed, source not) keeps its journal shape exactly.

## Standing requirements

- `skills: coding-standards` (forwarded by Execute mode by default).
- Any authorized deviation from item text lands as a dated `NOTES:` line under the item.
- No item changes `VERSION`, the CHANGELOG release heading, or any tag (see the closing note).
- Existing tests named in an item's **Tests** stay green unless the item says it replaces them.

## Out of scope

- Candidate 2 (`registerMechanism` convention helper) and candidate 5 (typed `git_diff_range`,
  `diffLinesStat` from regions, collapsing the TUI's two tag triples) — denied.
- Folding `keyAccessors` into `KeyRegistry` (the explorer's "second-best" for candidate 3) — a
  600-line move that puts resolution closures on an exported struct; not asked for.
- Moving any Firing composition into `internal/run` — contradicts ADR 0033's caller-composes rule.
- Changing what headless or the daemon set on their Configs beyond what unification forces
  (their field-for-field agreement is preserved; only the ROUTE to the same value is shared).
- `TestTranscriptCodecRoundTripsRecordedRegions` and the codec's region members stay exactly as
  they are.

---

## 1. Make the `EditRegions` persistence contract text agree with ADR 0052 §5 — ✅ DONE (2026-08-24)

NOTES (2026-08-24): the `NEVER PERSISTED:` heading of the Tool summary block comment is renamed to
`NO WIRE FORM:` — the item required the contradicting phrase gone while the "no wire form" half stays
load-bearing, and the paragraph needed a heading that says the surviving half.
NOTES (2026-08-24): `CONTEXT.md`'s **Tool summary** and **Edit regions** entries carry the same
contradicting wording ("A summary is **never persisted**", "never in the session record"); the item's
Files list names only the two files above, so they were left untouched — see the DEFER line.

**What.** The domain contract says the region structure is "never persisted in the session
record" (`internal/domain/toolsummary.go:31-34` on `ToolSummary`, `:120-122` on `EditRegions`;
ADR 0052 §1 lines ~47-49) while ADR 0052 §5 (lines ~104-109) ratifies exactly that persistence
and `internal/tui/transcriptcodec.go:158-173, 461-478` performs it. Rewrite the two Go comments
and ADR 0052 §1 so the rule reads: *the summary VALUE never reaches disk and has no wire form
(`domain.Message` carries Content only); the TUI's session codec may mirror the region FACTS it
needs for a width-dependent re-paint onto its own wire type (`wireEditRegion`), per §5.* Add a
dated amendment paragraph to ADR 0052 (below its decision section) that names the two comment
sites and states the reconciliation; do not touch §5 or the codec. Precision rule: the comment at
`toolsummary.go:31-34` must still say the summary has no wire form — that half is true and
load-bearing (`fromWireToolView` never re-runs a presenter, `transcriptcodec.go:554-569`).

**Files:** `internal/domain/toolsummary.go`,
`docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`

**Tests.** None new; comment and doc text only.

**Acceptance.**
- `go build ./internal/domain/... ./internal/tui/...`
- `go vet ./internal/domain/...`
- `grep -n "never persisted" internal/domain/toolsummary.go` returns no line (the phrase is what
  contradicted §5); `grep -n "Amendment (2026-08-24)" docs/adr/0052-*.md` returns one line.

**Commit:** `docs(domain): reconcile the EditRegions persistence contract with ADR 0052 §5`

---

## 2. One table behind the settings dispatcher and its `unreachable` mirror — ✅ DONE (2026-08-24)

NOTES (2026-08-24): six key GROUPS shared one apply body in the old switch (`system-prompt-*`, the two `url-safety:` lists, the four `present.` rows, `validated-sets.*`, and `ui.inspector`+`response-reserve`). A per-key table cannot hold one body in several entries, so those bodies moved verbatim into named package-level applies — `applySystemPromptBlock`, `applyURLSafetyHosts`, `applyPresentation`, `applyValidatedSets`, `applyTheWriteAlone` — which the entries reference instead of an inline closure.
NOTES (2026-08-24): the `reaches` predicates several keys shared likewise became named funcs (`reachesTheEngine`, `reachesTheEngineAndTheHolder`, `reachesTheHolder`, `reachesTheSwapDoor`, `reachesThePresentation`, `reachesWithoutAMember`), with the existing `settingsApplier.rides` used as a method expression for the seven riding keys. The SwapTools group comment therefore lives once on `reachesTheSwapDoor` rather than duplicated on its four entries, its opening clause reworded into godoc form; the `model-profiles`, `remember-model` and `servers` prose sits on those entries as the item asked.
NOTES (2026-08-24): two comment-truth fixes forced by the move — the `servers` apply body's "the top-level `context-window:` key above does" lost the word "above" (the table is in registry order, where `context-window` now sits below `servers`), and `applySettingFor`'s doc comment says "entry" where it said "case".

**What.** `cmd/apogee/wire_settings.go` carries two switches over the same key set: the apply
dispatcher (`:566-770`, one case per editable key) and `settingsApplier.unreachable`
(`:801-…`, one case per key naming which members of `settingsApplier` the apply needs), a drift
risk the file itself calls "taken deliberately and closed by a test" (`:795-798`). Replace both
with ONE ordered table — a package-level slice (registry order) of per-key entries, each carrying
the key string, a `reaches func(settingsApplier) bool` (the predicate today's `unreachable` case
computes) and an `apply` closure with the dispatcher case's body — and make both entry points
lookups over it: the dispatcher finds the entry and runs `apply`; `unreachable` finds the entry
and negates `reaches`. Keep the key-group comments (the "why" prose on `model-profiles`,
`remember-model`, `servers`, the SwapTools group) on the table entries. Keep every existing
apply body byte-for-byte; this item moves code, it does not change any apply. Load-bearing
standards: one source of truth for the key set (the table); the existing test
`TestApplySettingRefusesEveryKeyItCannotReach` (`cmd/apogee/wire_test.go:3599`) is kept and now
pins table completeness — a registry key with no table entry must still fail it; no reflection.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`

**Tests.** `TestEveryEditableSettingKeyHasAnApply` (`wire_test.go:3492`) and
`TestApplySettingRefusesEveryKeyItCannotReach` (`:3599`) stay green unchanged in intent (adjust
only what the table's shape forces). Add `TestSettingsTableIsInRegistryOrder` — the table's keys
are a subsequence of `config.KeyRegistry` paths (so the /settings surface and the table cannot
disagree on order) and contain no duplicates.

**Acceptance.**
- `go build ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestEveryEditableSettingKeyHasAnApply|TestApplySetting|TestSettingsTable'`
- `grep -c 'case "' cmd/apogee/wire_settings.go` is strictly lower than before the item (the
  verifier compares against `git show HEAD:cmd/apogee/wire_settings.go | grep -c 'case "'`).

**Commit:** `refactor(cmd): drive the settings dispatcher and its reachability check from one table`

---

## 3. Add `journaledMutation` — the multi-path undo funnel beside `safeWriteFile`

**What.** In `internal/tools/path_safety.go`, next to `safeWriteFile` (`:70`), add the sibling
helper ratified by design call 2:

```go
// postImage says how a mutated path's after-state reaches the journal once the body lands it.
type postImage int
const (
    postAbsent   postImage = iota // the path no longer exists (delete, move source)
    postReadBack                  // read the bytes back from disk (copy/move destination)
)

type mutationPath struct {
    input string    // the argument spelling — journal identity is target.Named (path_safety.go:350)
    root  string    // the root THIS path resolves against (copy's source root differs from its destination root)
    post  postImage
}

// journaledMutation captures a pre-image for every path, runs body with the write-escape target,
// then commits exactly the paths body reports as landed, each with its own post-image policy.
// A landed path whose read-back fails journals nothing (the safeWriteFile rule, path_safety.go:327-335).
func journaledMutation(ctx context.Context, paths []mutationPath, body func(escape string) (landed []bool, err error)) error
```

`landed` has one entry per path (nil ⇒ none landed); the helper commits landed paths only, in
order, then returns `err` unchanged. Capture happens for ALL paths before the body runs (move
needs both ends' pre-images before `SafeRename`). `journaledMutation` and `safeWriteFile` are the
only two functions allowed to call `capturePreImage`, `commit` and `commitReadBack` — item 4
makes that true and pins it. No caller is ported in this item. Load-bearing standards: the fence
primitive (`security.SafeRename` / `SafeCopyFileFrom` / `SafeRemove`) stays the BODY's choice —
the helper owns only permit lookup, capture, and commit; keep the helper unexported; the
`writeEscapeTarget(ctx)` value is handed to the body rather than re-derived by the caller.

**Files:** `internal/tools/path_safety.go`, `internal/tools/undo_journal_test.go`

**Tests.** In `undo_journal_test.go`, direct unit tests over the helper with a fake body:
`TestJournaledMutationCommitsOnlyLandedPaths` (two paths, body reports `[true,false]` ⇒ one
record), `TestJournaledMutationCapturesEveryPathBeforeTheBody` (body mutates path B, path B's
pre-image is the pre-body bytes), `TestJournaledMutationJournalsNothingWhenTheBodyFails` (body
returns nil, err ⇒ zero records, err returned), `TestJournaledMutationReadBackFailureJournalsNothing`
(landed path deleted by the body under `postReadBack` ⇒ no record for it),
`TestJournaledMutationWritesWithoutAJournal` (nil journal in ctx ⇒ body still runs, no panic).

**Acceptance.**
- `go build ./internal/tools/`
- `go test ./internal/tools/ -run 'TestJournaledMutation|TestWriteFunnel'`
- `go vet ./internal/tools/`

**Commit:** `feat(tools): add journaledMutation, the multi-path undo funnel beside safeWriteFile`

---

## 4. Route copy, move and delete through `journaledMutation`

**Depends on item 3.**

**What.** Port the three non-content writers so their hand-wired capture/commit sequences become
one `journaledMutation` call each, preserving every journal shape the existing tests pin:
- `copy_file` (`internal/tools/file_ops.go:122-151`): one path — the destination
  (`postReadBack`); body = `security.SafeCopyFileFrom(..., escape)`; landed `[true]` on nil err.
- `move_file` (`file_ops.go:187-211` and `move` `:242-277`): two paths — source
  (`postAbsent`), destination (`postReadBack`); body runs the rename fast path, then on the
  triaged fallback the copy+remove; landed `[true,true]` on success, `[false,true]` on the
  split failure at `:272` (copy landed, remove failed), nil on any other error. The permit
  asymmetry stays: the source path's root is the plain read root (`checkFileOpsPathsFrom`,
  `file_ops.go:304-309`), the destination's is the write target.
- `delete_file` (`internal/tools/delete_file.go:79-110`): one path (`postAbsent`); body =
  `security.SafeRemove(..., escape)`. `checkDeletePath`'s directory refusal and the tool
  description's "There is no undo" wording are untouched (ADR 0051 rejected exposing undo to the
  model).
Then update `internal/tools/doc.go` (~line 117), which today says copy/move "Neither reaches
`safeWriteFile`, so each takes its own undo pre-image at its own mutation site" — rewrite it to
name the two funnels. Add a source-scan test (the shape of
`internal/mechanisms/docmap_test.go`) `TestUndoCaptureHasExactlyTwoCallers`: walk
`internal/tools/*.go` (non-test) and assert `capturePreImage(` and `.commit(` /
`.commitReadBack(` occur only in `path_safety.go`. Load-bearing standards: no behaviour change —
every existing journal test passes without edits to its assertions; `stageGitPaths` calls stay
outside the funnel (they are not journal concerns); delete/copy/move keep their `okResult`
strings byte-for-byte.

**Files:** `internal/tools/file_ops.go`, `internal/tools/delete_file.go`, `internal/tools/doc.go`,
`internal/tools/undo_journal_test.go`

**Tests.** Existing, unchanged in assertion: `TestCopyFileJournalsTheDestinationOnly` (`:357`),
`TestMoveFileJournalsBothEnds` (`:416`), `TestMoveFileClobberingJournalsThePreImage` (`:446`),
`TestMoveFileFallbackJournalsBothEndsLikeTheRename` (`:481`),
`TestMoveFileFallbackSplitFailureJournalsTheDestinationAlone` (`:514`),
`TestDeleteFileJournalsThePreImage` (`:570`), `TestFileOpsJournalNothingWhenRefused` (`:610`),
and the permit tests `TestMoveSourceStaysInWorkspaceUnderAPermit`
(`internal/tools/write_permit_test.go:319`), `TestPermitWidensNoRead` (`:339`). New:
`TestUndoCaptureHasExactlyTwoCallers`.

**Acceptance.**
- `go build ./internal/tools/`
- `go test ./internal/tools/ -run 'TestCopyFile|TestMoveFile|TestDeleteFile|TestFileOps|TestUndoCapture|TestMoveSource|TestPermit|TestWriteFunnel|TestJournaledMutation'`
- `grep -c 'capturePreImage(' internal/tools/file_ops.go internal/tools/delete_file.go` reports
  `0` for both.

**Commit:** `refactor(tools): route copy, move and delete through the journaledMutation funnel`

---

## 5. Pin undo coverage by walking the writer registry; amend ADR 0051 §3

**Depends on item 4.**

**What.** Add `TestUndoJournalCoversEveryWriter` to `internal/tools/undo_journal_test.go`,
modelled on `TestDefaultTools_EveryNetworkToolIsURLFiltered`
(`internal/tools/network_funnel_test.go:73`) and `TestWriteTargetProbesCoverEveryWriter`
(`workspace_scoped_test.go:106`): iterate `DefaultTools(...)`, and for every tool that satisfies
`IsWorkspaceScopedWriter`, look up a probe in a test-local table (tool name → a function that
seeds a temp workspace and returns a representative successful call's arguments); drive the call
under a journal and assert the journal holds ≥1 record afterwards. A writer with no probe entry
FAILS the test with a message naming the tool ("new workspace writer <name> has no undo probe —
add one; a writer that skips the funnel silently loses /undo coverage"). Then amend ADR 0051:
under decision 3 add a dated amendment stating that the "funnel" is the PAIR `safeWriteFile`
(content verbs) + `journaledMutation` (copy/move/delete), that `TestUndoCaptureHasExactlyTwoCallers`
pins that no third capture site exists, and that `TestUndoJournalCoversEveryWriter` is the
coverage boundary the decision's "holds automatically for tools added later" now rests on.
Also correct the ADR's Context sentence "every content mutation a first-party tool performs
passes `safeWriteFile`" to name both funnels. Load-bearing standards: the probe table lives in
the test file only; the test uses the real `DefaultTools` constructor, never a hand-listed set of
tool names (that is the second list the ADR refuses).

**Files:** `internal/tools/undo_journal_test.go`,
`docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md`

**Tests.** New: `TestUndoJournalCoversEveryWriter`. Negative check the verifier performs by hand:
temporarily delete one probe entry, run the test, confirm it fails naming that tool, restore.

**Acceptance.**
- `go test ./internal/tools/ -run 'TestUndoJournalCoversEveryWriter|TestUndoCapture|TestWriteTargetProbes'`
- `grep -n "Amendment (2026-08-24)" docs/adr/0051-*.md` returns one line.

**Commit:** `test(tools): walk the writer registry to pin undo coverage; amend ADR 0051 §3`

---

## 6. `liveSettings` exposes the session's live-overlaid `config.Options`

**Depends on item 2** (same file; the table is where the overlay hooks in).

**What.** Today `liveSettings` (`cmd/apogee/wire_settings.go:47`, constructed at `:142` with the
boot `config.Options`) re-reads only servers / mechanisms / validated-sets / prompt / profiles /
window / reserve in `rebindInputs` (`:446`); every other editable key — `tools.disabled`,
`url-safety.allow-hosts`, `url-safety.deny-hosts`, `web-search-endpoint`, `bypass`,
`auto-compact`, `ui.inspector`, `context-files.enable`, `context-files.names`, `servers` (for the
`api-key-env` union) — is pushed to the engine by its apply but never written back onto the
holder's Options, so anything that composes from those Options (item 9's Firing) sees the boot
snapshot. Add `func (s *liveSettings) options() config.Options` returning the holder's Options
with every applied key overlaid, and make each table entry's `apply` (item 2) that changes one of
those keys also record the new value on the holder (under the holder's existing mutex). Keys the
apply already records (the rebind set) need no change. Load-bearing standards: the overlay is
written at the apply, never re-read from the config file (the file may lag the apply — ADR 0037's
"applies to the running session" is the source of truth); `options()` returns a copy, never the
holder's slice-backed value (slices like `DisabledTools` are cloned).

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`

**Tests.** `TestLiveSettingsOptionsFollowEveryApply` — table-driven over the ten keys above:
construct a `liveSettings` from boot Options, drive the apply for the key with a new value
through `settingsApplier` (real engine seams replaced by the test doubles `wire_test.go` already
uses for the apply tests), assert `options()` reflects the new value and that a second
`options()` call after mutating the returned slice is unaffected (copy semantics).

**Acceptance.**
- `go build ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestLiveSettingsOptionsFollowEveryApply|TestEveryEditableSettingKeyHasAnApply|TestApplySetting'`

**Commit:** `feat(cmd): let liveSettings hand out the session's live-overlaid Options`

---

## 7. `firingConfig` composer; port headless onto it

**What.** Create `cmd/apogee/wire_firing.go` holding the one composer for an unattended run's
`apogee.Config` (design calls 3–5, 7):

```go
type firingInputs struct {
    opts      config.Options        // the Driver's Options — headless: startup; daemon: startup; session: liveSettings.options()
    entry     config.ServerEntry    // the bound servers: entry (endpoint, key source, pins)
    apiKey    string                // "" ⇒ resolve through keys
    keys      *config.KeyResolver   // nil ⇒ config.NewKeyResolver()
    roots     stateRoots
    manualIDs []apogee.MechanismID
    confiner  apogee.Confiner
    model     string                // "" ⇒ entry.Model
    mode      domain.Mode
    skills    *skills.Provider      // nil ⇒ skills.NewProvider from roots (design call 5)
    width     func(ctx context.Context, endpoint, model, key string) int // nil ⇒ discoverSlots (design call 4)
    recordID  string                // the Firing's record id; ScratchDir = firingScratch(recordID)
}

// firingConfig composes the Config every unattended run shares. The notices are the rebind
// notices headless prints and the other Drivers drop.
func firingConfig(ctx context.Context, in firingInputs) (cfg apogee.Config, notices []string, err error)
```

It sets exactly the field set `runHeadless` sets today (`headless.go:386-453, 469`), taking
each value by ONE route: `SecretEnvVars` = `config.APIKeyEnvNames(in.opts)`;
`Context.ResponseReserveFraction` read back off the resolved spec (the daemon's route,
`daemonfire.go:281-284` — headless's identical-value route at `headless.go:364-367` is retired);
`ExtraReadRoots`/`Skills` from the provider; `ParallelAgents` from `config.ResolveParallelAgents`
over `in.width`; `Tools`/`Events`/`Approver`/`Asker`/`Presenter` left nil for the caller. It calls
the existing helpers (`rebindSpecFor`, `startupEntry`/`serverFor`, `resolveRoots` stays the
caller's, `firingScratch`, `mechanismIDs`, `config.ResolveContextWindow`,
`config.ResolveResponseReserve`) — it composes, it does not re-implement. Then port `runHeadless`
(`headless.go:243-485`): the composition block becomes building `firingInputs` and one
`firingConfig` call; headless keeps its own pre-steps (`gcScratchDirs`, `plaintextKeyNotice`,
`autoUnattendedBlocked`, the plan/auto mode gate) and prints the returned notices. Load-bearing
standards: `firingConfig` is a deep module — one function, one struct of inputs, no
Driver-specific branches inside it (a Driver difference is an INPUT, never an `if driver ==`);
`internal/run` and `internal/*` are not touched; the daemon and schedule are NOT ported here
(items 8, 9).

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/headless.go`,
`cmd/apogee/headless_test.go`

**Tests.** New `wire_firing_test.go`: `TestFiringConfigSetsEveryUnattendedField` — compose with
fixed inputs and assert every field named above, in particular the ones no Driver test asserts
today: `SecretEnvVars`, `URLAllowHosts`, `URLDenyHosts`, `DisabledTools`, `EnabledTools`,
`Inspector`, `ContextFiles`, `ExtraReadRoots`, `Bypass`, `ConfineToWorkspace`,
`Context.CompactionEnabled`, `Context.MaxOutputTokens`; `TestFiringConfigDefaultsItsSeams` —
nil `keys`/`skills`/`width` take the documented defaults; `TestFiringConfigLeavesTheDriverSeamsNil`.
Existing headless tests stay green: `TestHeadlessComposesTheRunnerSpec` (`headless_test.go:391`),
`…InstallsTheParallelAgentsCap` (`:437`), `…BudgetsAgainstTheBoundEntrysPins` (`:485`),
`…SpecStatesTheShareTheConfigDividesBy` (`:540`), `…RunGetsItsOwnScratchDirAndSweepsStaleOnes`
(`:570`), `…NoSaveDropsTheStore` (`:596`), `…AutoEligibilityGate` (`:240`).

**Acceptance.**
- `go build ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestFiringConfig|TestHeadless'`
- `grep -c 'apogee.Config{' cmd/apogee/headless.go` reports `0`.

**Commit:** `refactor(cmd): add the firingConfig composer and compose headless runs through it`

---

## 8. Port the daemon Firing onto `firingConfig`

**Depends on item 7.**

**What.** Replace `daemonWiring.configFor` (`cmd/apogee/daemonfire.go:222-357`) with building
`firingInputs` (`opts: w.opts`, `entry` from `serverFor`, `keys: w.keys`, `mode` and `recordID`
from `fire` `:158-211`, `skills: nil`, `width: nil`) and one `firingConfig` call; drop the
returned notices. Delete the "mirrors runHeadless field for field" comment (`:213-216`) — the
composer is now the mirror. Load-bearing standards: no behaviour change — every daemon test
below passes with its assertions untouched; `fire` keeps stamping nothing the composer already
sets (Mode and ScratchDir now arrive through the inputs).

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`

**Tests.** Existing, unchanged in assertion: `TestDaemonFireBindsTheServerTheEntryNames`
(`daemonfire_test.go:97`), `…FallsBackToTheStartupServer` (`:126`),
`…GivesEachFiringItsOwnScratchDir` (`:153`), `…OverlaysTheEntrysModel` (`:179`),
`…RunsInTheEntrysWorkspace` (`:317`), `…RunsTheModeTheScheduleFired` (`:245`),
`…CompositionNamesNoLauncher` (`:226`).

**Acceptance.**
- `go build ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestDaemonFire|TestFiringConfig'`
- `grep -c 'apogee.Config{' cmd/apogee/daemonfire.go` reports `0`.

**Commit:** `refactor(cmd): compose daemon Firings through firingConfig`

---

## 9. Port the in-session Schedule Firing onto `firingConfig`; pin the drift fix; amend ADR 0037

**Depends on items 6 and 8.**

**What.** Replace `scheduleWiring.fire`'s inherit-and-override block (`cmd/apogee/schedule.go:82-141`,
which copies the TUI's boot Config `w.cfg` from `wire_live.go:302-310` and overrides 13 fields)
with `firingInputs` built from the session: `opts` = the holder's `options()` (item 6, design
call 3), `entry`/`apiKey` from `w.binding()`, `keys` the session's resolver, `roots` the session's
`stateRoots`, `skills` the session's live provider (design call 5), `width` = `w.width()` wrapped
as the function parameter (design call 4), `mode`/`recordID` as today. The Firing therefore
runs the session's LIVE `tools.disabled`, URL allow/deny lists, `web-search-endpoint`, `bypass`,
`auto-compact`, `ui.inspector`, `context-files.*` and `api-key-env` union. Remove the
`base: w.cfg` plumbing from `wire_live.go` if nothing else reads it after the port. Amend ADR
0037 with a dated paragraph: a Firing raised inside a session composes from the session's live
Options, so every settings edit applies to the session's Firings too; name `firingConfig` and
`liveSettings.options()`. Load-bearing standards: the composer gains no schedule-specific branch;
the session's Approver/Presenter/Events stay nil on the Firing exactly as today (ADR 0033: a
Firing constructs a fresh Agent).

**Files:** `cmd/apogee/schedule.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/wire_live.go`,
`docs/adr/0037-every-settings-edit-applies-to-the-running-session.md`

**Tests.** Existing, unchanged in assertion: `TestScheduleFiringRunsAgainstTheCurrentBinding`
(`schedule_test.go:122`), `…CarriesTheParallelAgentsWidth` (`:283`), `…GetsItsOwnScratchDir`
(`:323`), `…IsBoundedByTheEntryTheSessionMovedOnto` (`:377`),
`…SplitsTheWindowTheEntryTheSessionMovedOntoStates` (`:459`),
`…ReportsAPerModelResolutionFailure` (`:249`). New: `TestScheduleFiringFollowsLiveSettingsEdits` —
boot a session wiring, apply `tools.disabled`, `url-safety.deny-hosts` and add a `servers:` entry
with an `api-key-env` through the settings applier, raise a Firing, assert the composed Config's
`DisabledTools`, `URLDenyHosts` and `SecretEnvVars` carry the edited values (the three fields the
fact-check found stale). `TestScheduleFiringSharesTheSessionsSkillsProvider` — the Firing's
`Skills`/`ExtraReadRoots` come from the session's provider instance.

**Acceptance.**
- `go build ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestScheduleFiring|TestFiringConfig|TestLiveSettings'`
- `grep -n "Amendment (2026-08-24)" docs/adr/0037-*.md` returns one line.
- `grep -c 'apogee.Config{' cmd/apogee/schedule.go` reports `0`.

**Commit:** `fix(cmd): compose in-session Firings from the live Options through firingConfig`

---

## Suggested version bump

Item 9 changes user-visible behaviour (a Schedule Firing now honours live `/settings` edits) and
items 3–5 make `/undo` coverage a construction guarantee; the rest is internal. Suggest a **minor**
bump (`0.16.7` → `0.17.0`) at closeout — the owner decides; no item touches `VERSION`.
