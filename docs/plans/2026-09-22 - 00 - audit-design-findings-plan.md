# Audit 2026-09-20 — the seven design-heavy findings (plan E) — plan

**Goal:** close the seven audit findings that were beaded rather than fixed, because each needed
design before code: the undo-journal lock scope (`apogee-m6k`), the rung-3 opener
(`apogee-2we`), the net-deny capability-honesty gap (`apogee-qi3`), the two winlabel journal
defects (`apogee-73s`, `apogee-ea3`), snapshot ingestion (`apogee-mre`), and the `.beads/hooks`
repo-policy call (`apogee-242`). No plan chain gate — A/B/C/D are archived.

**Date:** 2026-09-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** `a803bb46`
**Standing requirements:** skills: coding-standards; any authorized deviation from item text lands as a dated NOTES line under the item; no version identifier changes; `CHANGELOG.md` entries travel in item sidecars and land at closeout.

**Regression check (2026-09-22, a803bb46):**
- 1: guard folded — ordinal captured before the pop, generation-compared re-take preserving ADR 0074 decision 6, lock-biting tests, `-race` off this box's Acceptance.
- 2: guard folded — `TestOpenerCommandOverride` joins the rewrite list; tests (a)/(c) copy the per-row `look` closures.
- 3: guard folded — `internal/domain/present.go`, the five further `Presenter` implementers, `go vet ./...`, the `Local` clause; re-pointed at round 2 — the fact's consumer is item 4's degrade path, not `resolutionInput`; departs from `internal/domain/present.go:37-40`.
- 4: recast (round 2) — NO human prompt at all: a new file-only `present.command-on-model-documents` opt-in gates an execution-capable rung 3, the Presenter's fire-and-forget contract stands untouched, `present_document` stays a `ClassReadOnly` Run, so ADR 0019 §1, its 2026-07-26 amendment and the confinement contract's :550-553 all stand.
- 5: recast — re-scoped to ADR 0019 §5 plus CONTEXT and the manual; the Acceptance greps the phrases directly and gains the truncate-prose check; the Presenter-contract half of the sweep is dropped (item 4 no longer falsifies it).
- 6: guard folded — the ci.yml residual step, the `confiner_linux_test.go` landlock row, the host-derived residual expectation, the impossible three-token row dropped; the two tokens live in `internal/domain`, and this item now carries item 8's `ResidualNotice` network filter so no commit renders a wrong sentence.
- 7: guard folded — the `confiner_linux_test.go` namespace row and the widened `-run`; supersedes ADR 0081 §4's "nothing is residual".
- 8: recast — `ResidualNotice` stays write-class and asserts item 6's filter rather than introducing it; `CapabilityLine` is the honesty surface; supersedes `internal/platform/landlock_linux.go:209-210`.
- 9: guard folded — the undisclosed branch is keyed on the listener, never the child's exit status.
- 10: guard folded — the spared root is handed off unjudged; `walk_windows.go` and `session.go` join Files; supersedes the rule that a spared root does not keep the journal.
- 11: guard folded — the carry gets a bounded life (supersedes retire.go:80-84's rationale), `!entry.Judged` never reaches `restore`, and the compile-only `GOOS=windows` spelling.
- 12: guard folded — the per-message cap becomes `maxFileReadBytes`; supersedes state.go:262-268's mid-history-system-message comment.
- 13: guard folded — `TaskListFence` and the orientation header are excluded, the `FileRefs` refusal is dropped, `standingblocks_test.go` joins Files, the `InjectContext` premise is corrected.
- 14: guard folded — a per-file Acceptance that can fail, and `CLAUDE.md` dropped from the prose guard.

## Authoritative sources

- `docs/reviews/code-audit-2026-09-20.md` — the findings; **where an item disagrees with the audit,
  the item wins** (three findings are corrected below on evidence).
- The seven beads (`bd show apogee-m6k` etc.) — the owner's sequencing notes.
- ADR 0012 (blast radius), ADR 0019 §1/§5 (presentation ladder), ADR 0049 §4 (control-plane
  writes), ADR 0051 + ADR 0074 (undo generation protocol), ADR 0020 (Windows label box),
  ADR 0023 (no committed RoleSystem); `SECURITY.md`; `docs/design/confinement-execution-contract.md`.
- `docs/handoffs/archived/2026-09-21 - 01 - audit-follow-through-and-plan-order.md` — this is its "plan E".

## Ratified design calls

- **Batch is all seven beads** (owner, 2026-09-22).
- **`apogee-242` is documented, not coded** (owner, 2026-09-22): hydration trusts the checkout's hooks; not an apogee code change.
- **`apogee-m6k` fixes Revert/Redo only** (owner, 2026-09-22): the `Close`/`MarkPre` git-subprocess holds stay, beaded at closeout.
- **`apogee-2we` fences argv[0] *and* holds an execution-capable rung 3 behind a new opt-in key** (owner, 2026-09-22; recast at the round-2 regression check): argv[0] is resolved and fenced exactly as rung 1's is (item 2), and a non-empty `present.command` runs from `present_document` only when the file-only `present.command-on-model-documents` key is true — absent or false, the ladder degrades to rung 0 and the tool result says why. No ladder cell moves, no approval gate and no human prompt is added, so ADR 0019 §1 is NOT superseded; only §5's "nothing here runs a model-chosen command" takes a dated addendum.
- **The Presenter's fire-and-forget contract is preserved** (owner, 2026-09-22, round 2): `domain.Presenter` "awaits no human rendezvous and must never block on the user" (`internal/domain/present.go:22`, `internal/tui/presenter.go:49`, `internal/tui/doc.go:231`, `internal/tui/parkedcall.go:24`) stands untouched and is not superseded — the rung-3 call asks no Approver and no Asker, acquires no `PromptSlot`, and adds no human surface.
- **`apogee-qi3` ships the disclosure half only** (owner, 2026-09-22). Audit corrected: `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` and the UDP rights exist in no shipped kernel and not in `x/sys` v0.47.0 — the rights half is not codeable.
- **`apogee-mre` refuses the whole restore, live state untouched** (owner, 2026-09-22).
- **`apogee-73s` ships its Linux-provable half** (owner, 2026-09-22); the per-run secret and creation-time liveness get a new bead at closeout.
- **Author calls (2026-09-22):** cap and token values are the constants named in each item; the undo fix pops before it walks (the audit's "walk lock-free" premise is unsafe as written — see item 1).

## Out of scope

- The undo `Close`/`MarkPre`/`Preview` lock holds; the winlabel per-run secret and `GetProcessTimes` liveness — both beaded at closeout, not fixed here.
- Landlock net/scope *rights* (no kernel API); per-port landlock allow rules.
- The thirteen audit findings already shipped by plan D (`docs/plans/archived/2026-09-21 - 01`).
- `apogee-se0` (gitexec pre-init memo), `apogee-c8l`, and every non-audit bead.

---

## 1. A revert pops its group under the lock and walks it lock-free — ✅ DONE (2026-09-22)

NOTES (2026-09-22): `j.persist()` stays inside the second hold, as the item's Approach states ("re-take for the redo-stack move and `j.persist()`") — it encodes the journal's own two stacks and cannot read them lock-free. The item's Goal sentence reads "no filesystem write … while `Journal.mu` is held"; the walk, which is what the audit's Critical names, is lock-free, and the one small atomic index write is not.

NOTES (2026-09-22): the step is parked on the Snapshotter (the item allows "a fence or Snapshotter call the walk makes"): `security.Fence` is a concrete struct with no seam, and a diff-only path's restore reads its bytes through `Snapshotter.Content`. The `gatedSnapshotter` stand-in therefore sits in `snapshot_test.go` beside `fakeSnapshotter` (both files are in the item's Files list); the four cases themselves are in `journal_test.go` on `funnelWrite`/`seedFile`/`assertContent`.

NOTES (2026-09-22): test (c) is a redo after a revert that raced NOTHING — a revert that raced a `Record` drops its redo group by the item's own ratified generation rule, so "Redo after (a)" cannot be taken literally. It parks the redo's own walk as well, and additionally pins that a group opened mid-walk stays on top of the re-applied one; test (a) gained the matching assertion that a revert which raced a write offers no redo at all (ADR 0074 decision 6). Both rules are mechanisms this item introduces, so neither ships untested.

NOTES (2026-09-22): `go test -race ./internal/undo/...` cannot run on this box — `FATAL: ThreadSanitizer: unsupported VMA range (Found 47 - Supported 48)`, the limitation `docs/manual/building.md:124-128` records. It is a CI check, as the item states. The three lock-biting cases were verified red against the pre-item tree by restoring `journal.go`/`redo.go` from HEAD: all three time out (deadlock), and the ordinal guard passes there as intended.

**What.** `fix(undo)`: closes `apogee-m6k`, the audit's Critical "the undo journal holds its mutex
across filesystem writes".

**Regression guard.** Capture `ordinal := len(j.groups)` (Revert) / `len(j.redo)` (Redo) BEFORE the
pop and pass it to `runStep`, so `ReportLines` keeps today's ordinal and `cmd/apogee/undo_test.go`'s
`TestUndoVerbPreviewsThenReverts` stays green. On the re-take, compare a generation captured before
the release: if it moved, drop the push onto `j.redo` (Revert) and re-insert below any group opened
during the walk (Redo) — preserving ADR 0074 decision 6 rather than reversing it. The tests bite on
the LOCK, not on `-race`: block inside a fence or Snapshotter call the walk makes and assert a
concurrent `Record`/`Generation()` returns within a timeout. `-race` leaves this item's Acceptance
(`docs/manual/building.md:124-128` — no ThreadSanitizer on this box); the raced run is a CI check.

**Goal:** `Journal.Revert` and `Journal.Redo` perform no filesystem write and no git read while
`Journal.mu` is held; a `Record` arriving during a step lands in a group the step is not walking; a
concurrent `Record`/`Generation()` returns while a step walks instead of blocking until it ends, and
`go test -count=2 ./internal/undo/...` covers that interleave and passes.

**Approach (assumed at the header base):** **The audit's own fix is unsafe as written**: the group
is not popped before the walk, and `Record`'s merge branch mutates a live `*entry`'s
`post`/`postKept`/`postHash`/`postExists` in place. So: under the first hold, pop the top group off
`j.groups`, set `j.pending = true`, bump `j.generation`; release; run `runStep` over the popped
group; re-take for the redo-stack move and `j.persist()` — with `j.pending` set and the group
unreachable, `openGroup` opens a fresh group for a racing `Record`. Binding: `Redo`'s generation
check stays inside the first hold; `Agent.UndoRevert` and `applyUndoVerb` are untouched; the conflict
rule (`plan` compares a pre-image before `apply` writes) is unchanged; the journal's observable state
after a failing step matches base.

**Files:** internal/undo/journal.go, internal/undo/redo.go, internal/undo/doc.go, internal/undo/journal_test.go, internal/undo/snapshot_test.go
**Read first:** internal/undo/journal.go — Journal.Revert, Journal.runStep, Journal.Record, Journal.openGroup; internal/undo/redo.go — Journal.Redo, Journal.RedoPreview; internal/undo/snapshot.go — Journal.reach, Journal.closeGroup, Journal.dropIfEmpty; internal/undo/persist.go — Journal.persist, Journal.saveTo; internal/undo/notes.go — ReportLines, PreviewLines; internal/undo/journal_test.go — funnelWrite, funnelDelete, preImage, TestRecord_ConcurrentWritersAndReaders_IsRaceClean; cmd/apogee/undo_test.go — TestUndoVerbPreviewsThenReverts
**Tests.** New in `journal_test.go`, on the existing `funnelWrite`/`funnelDelete`/`preImage`
stand-ins, biting on the lock: (a) a `Revert` blocked inside a fence or Snapshotter call its walk
makes, with a concurrent `Record` and `Generation()` asserted to return within a timeout, and every
post-revert `Record` landing in a *new* group (assert `Generation()` and the group count, not
timing); (b) a `Record` issued while a revert walks does not alter the reverted file's restored
content; (c) `Redo` after (a) restores what the revert undid; (d) the ordinal `ReportLines` prints
for a revert and for a redo is unchanged from base. (a)–(b) must fail (deadlock or wrong content)
against the pre-item tree. Must keep passing:
`TestRecord_ConcurrentWritersAndReaders_IsRaceClean`,
`TestSnapshotSurface_ConcurrentCaptureRecordAndPreview_IsRaceClean`, every `persist_test.go` case,
`cmd/apogee/undo_test.go`'s `TestUndoVerbPreviewsThenReverts`. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test -count=2 ./internal/undo/... && go test ./internal/agent/... -run 'Undo|Revert|Redo'` — the raced run `go test -race ./internal/undo/...` is a CI/other-box check, not this box's.
**Commit:** `fix(undo): a revert pops its group under the lock and walks it lock-free`

## 2. A present.command override's argv[0] is resolved and fenced as rung 1's is — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the doc comments inside `internal/present/opener.go` that asserted the program bound applies to "the OS table alone" were corrected in place — the `Opener` type doc, `argv`'s fourth-bound paragraph, the `WorkspaceRoot`/`LookPath` field docs, `resolveProgram`'s opening and `launchDetached`'s "on rung 3 … resolved the way their shell would" sentence. All sit in the item's own Files. ADR 0019's two "the bound stops at rung 3" passages (:210-213, :252-256) are scoped to the EXTENSION and NAME bounds, which this item preserves, so neither is falsified and neither was touched; ADR 0019 §5's addendum is item 5's.

NOTES (2026-09-22): `docs/design/confinement-execution-contract.md:571-576` (the dated 2026-08-30 amendment) enumerates `security.ResolveProgram`'s call sites and names "rung 1's OS opener"; that enumeration is now one site short. Left as written — it is a dated historical record, true of its date — and flagged here because item 5's prose-guard grep (`present_document|present\.command|presentation ladder`) does not match that phrasing.

**What.** `fix(present)`: closes the argv[0] half of `apogee-2we`, the audit's High "the rung-3
opener runs a model-chosen program unapproved and unconfined in every mode".

**Regression guard.** `TestOpenerCommandOverride` (opener_test.go:462-551) joins the rewrite list
beside the two bound tests: its table's Opener gains `LookPath: lookInBin` and each `want` is wrapped
in `resolvedArgv(...)` (opener_test.go:61, :73) — its 11 rows pin a bare argv[0] and carry no
`LookPath`, so every row fails once argv[0] is resolved. Tests (a) and (c) cannot use `lookInBin`,
which always answers an absolute path under `os.TempDir` outside any workspace root
(opener_test.go:52-62): copy `TestOpenerRefusesAProgramInsideTheWorkspace` (opener_test.go:646-665) —
its per-row `look` closures plus a `WorkspaceRoot: t.TempDir()`.

**Goal:** A `present.command` override's argv[0] gets exactly rung 1's three outcomes: a program
that resolves inside the workspace fence is refused and the refusal reaches the user with the same
wording rung 1 emits; a relative or `.`-resolved program is refused; a program that is simply absent
degrades to the baseline rung. An override naming an absolute program outside the fence still runs,
and rung 3 stays neither extension-bounded nor name-bounded.

**Approach (assumed at the header base):** `Opener.argv` returns `overrideArgv(template, path)`
verbatim before rung 1's gates, so argv[0] is never resolved and PATH is searched at exec time. Route
it through the existing `Opener.resolveProgram`
(`security.ResolveProgram(o.LookPath, program, o.WorkspaceRoot, nil)`) before the argv is returned,
mapping its outcomes exactly as `osArgv` does: absent → `ErrNoOpener`, relative → the
`ErrExecFromWritablePath` refusal, resolved-inside-the-fence → the
`"present: refusing to launch %s: %w"` error. The substituted `{path}` argument is item 4's.

**Files:** internal/present/opener.go, internal/present/opener_test.go
**Read first:** internal/present/opener.go — Opener.argv, overrideArgv, osArgv, resolveProgram; internal/present/opener_test.go — TestOpenerCommandOverride, TestOpenerCommandOverrideIsNotExtensionBounded, TestOpenerRefusesAProgramInsideTheWorkspace, lookInBin, resolvedArgv; internal/security/execsafety.go — ResolveProgram, ErrExecFromWritablePath; internal/tui/presenter_test.go — openerRunning, lookInTestBin; cmd/apogee/wire_present.go — presentationRungs, openerLookPath
**Tests.** In `opener_test.go`, with the existing `recordingRunner` double and the per-row `look`
closures plus a `WorkspaceRoot: t.TempDir()`: (a) an override whose program resolves inside the
workspace root is refused, and the error string contains the exact emitted prefix
`present: refusing to launch `; (b) an override naming an absent program yields `ErrNoOpener`
(degrade, not error); (c) an override naming `./thing` is refused with `ErrExecFromWritablePath`;
(d) an override naming an absolute program outside the fence still produces the substituted argv.
`TestOpenerCommandOverride`, `TestOpenerCommandOverrideIsNotExtensionBounded` and
`TestOpenerCommandOverrideIsNotNameBounded` pin the *unfenced* behaviour today — rewrite all three to
keep their real subject (the override's argv shape; rung 3 ignores the extension allow-list and the
OS opener names) while resolving through the fence. (a)–(c) must fail against the pre-item tree. Must
keep passing: every other `TestOpener*`, `TestLaunchDetachedReportsWhatHappened`, `cmd/apogee`'s
`TestE2EPresent*`. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/present/... && go test ./cmd/apogee/... -run 'E2EPresent|presentationRungs'`
**Commit:** `fix(present): a present.command override's argv[0] is resolved and fenced as rung 1's is`

## 3. The Presenter states whether the wired opener is execution-capable — ✅ DONE (2026-09-22)

NOTES (2026-09-22): the predicate is named `IsExecutionCapable() bool` — the plan names the fact ("execution-capable") but not the symbol; the `is` prefix follows the coding standards' boolean rule. `uiPresenter` answers it from the ladder snapshot exactly as the item's regression guard specifies (`rungs.Local && rungs.Opener != nil && strings.TrimSpace(rungs.Opener.CommandOverride) != ""`), and `PresentDocument` forwards its delegate's answer, false for a nil delegate.

NOTES (2026-09-22): `cmd/apogee/wire_present.go` is in the item's **Files:** but needed no edit — `presentationRungs` already carries `p.Command` onto `present.Opener.CommandOverride` (wire_present.go:62), which is the field the predicate reads, so there is nothing to wire. Left untouched rather than edited for the sake of the list.

NOTES (2026-09-22): the deliberate departure from `internal/domain/present.go`'s additive-growth record is documented in place, in a paragraph above the `Presenter` interface: a new METHOD breaks every out-of-tree implementer of the re-exported `apogee.Presenter`, unlike an added struct field, and that is taken knowingly.

NOTES (2026-09-22): "must fail against the pre-item tree" holds in its strongest form for both new tests — `TestPresenterIsExecutionCapable` and `TestPresentDocument_IsExecutionCapableForwardsTheDelegate` call a method that does not exist at base, so the packages do not compile there. `go vet ./...` (in this item's Acceptance) is what proves the five further implementers compile, since `go build ./...` skips test packages.

**What.** `feat(present)`: the per-call fact item 4's degrade reads. Depends on item 2.

**Regression guard.** `**Files:**` named a file that does not exist — the `Presenter` interface is at
`internal/domain/present.go:34`. The predicate answers `rungs.Local && rungs.Opener != nil &&
strings.TrimSpace(rungs.Opener.CommandOverride) != ""`, and test (a) gains a `Local: false` row.
Five further `domain.Presenter` implementers become compile breaks and join **Files:** —
internal/agent/planmenu_test.go:34, internal/agent/delegationname_test.go:126,
internal/tools/registry_test.go:679, internal/run/harness_test.go:174,
cmd/apogee/wire_boot_test.go:1128 — so the Acceptance gains `go vet ./...`, which compiles every test
package `go build ./...` does not. Re-pointed at the round-2 call (2026-09-22): the fact's consumer is
item 4's presenter/opener degrade path, NOT resolution — nothing is precomputed into
`resolutionInput`, and dispatch is untouched. This departs from `internal/domain/present.go:37-40`,
which records post-v1 growth as additive so out-of-tree implementers do not break: a new method on
`Presenter` — re-exported as `apogee.Presenter` (apogee.go:368) — does break them, taken deliberately.

**Goal:** `domain.Presenter` answers whether the opener rung the host has wired can execute a
program of the user's choosing, and `*tools.PresentDocument` exposes that answer to the engine
without executing anything. The answer is true exactly when a non-empty `present.command` is
configured, and false for every rung-0/1/2-only wiring, including a nil opener and a remote session.

**Approach (assumed at the header base):** Add one predicate to the `domain.Presenter` interface —
the host answers it, the engine reads it — answered by `uiPresenter` from
`rungs.Opener.CommandOverride` being non-empty after `strings.TrimSpace`. `PresentDocument` holds the
delegate reference already; give it a method that forwards the answer, so the seam can ask without a
live handle crossing the quiescent boundary (ADR 0008). Binding: no resolution, classification or
mode behaviour changes — `PresentDocument` stays `ReadOnlyTool`; every other `domain.Presenter`
implementation (test doubles included) gains the predicate answering false.

**Files:** internal/domain/present.go, internal/tui/presenter.go, internal/tools/present_document.go, internal/tools/present_document_test.go, internal/tui/presenter_test.go, cmd/apogee/wire_present.go, internal/agent/planmenu_test.go, internal/agent/delegationname_test.go, internal/tools/registry_test.go, internal/run/harness_test.go, cmd/apogee/wire_boot_test.go
**Read first:** internal/domain/present.go — Presenter, PresentRequest; internal/tui/presenter.go — uiPresenter, ladder, setRungs, Presentation; internal/tools/present_document.go — PresentDocument, NewPresentDocument, ReadOnly; internal/tui/presenter_test.go — TestPresenterLadderPicksRung, openerRunning, lookInTestBin; internal/tools/present_document_test.go — scriptedPresenter, cancellingPresenter; cmd/apogee/wire_present.go — presentationRungs
**Tests.** (a) `uiPresenter` answers true for a `Presentation` whose `Opener.CommandOverride` is set,
false for one where it is empty, whitespace-only, the `Opener` is zero-valued, or `Local` is false
while the override is set; (b) `PresentDocument` forwards its delegate's answer, and answers false
for a nil delegate; (c) `TestPresentDocument_IsReadOnly` and `TestPresentDocument_IsNotExternalEffect`
still pass unchanged. Must keep passing: `internal/tui/presenter_test.go` in full,
`cmd/apogee/wire_boot_test.go`'s `presentationRungs` table. CHANGELOG sidecar, `[Unreleased]/Added`.
**Acceptance.** `go build ./... && go vet ./... && go test ./internal/domain/... ./internal/present/... ./internal/tools/... -run 'Present' && go test ./internal/tui/... -run 'Present|Bridge'`
**Commit:** `feat(present): the Presenter states whether the wired opener is execution-capable`

## 4. An execution-capable rung 3 runs only behind a file-only opt-in — ✅ DONE (2026-09-22)

NOTES (2026-09-22): deviation — `presentConfig` gains a plain `bool`, not the `*bool` the item's approach names: the key's default is FALSE, so an absent key and the zero value are the same answer and a pointer would have nothing to tell apart (unlike `auto-open`'s true default, which is why that one is a pointer).

NOTES (2026-09-22): deviation — the opt-in lands on `tui.Presentation` rather than on `present.Opener`: the Opener decides WHAT to run and the ladder decides WHETHER a rung runs at all (internal/present/opener.go:55-58), the Opener is handed a model-named document on every rung so it could not tell this case apart, and `internal/present` is not among the item's Files.

NOTES (2026-09-22): deviation — the registry row is `Editable: false`, the one `present.` key the `/settings` pane will not write; `editPointer` gives it the existing "⏎ opens $EDITOR" affordance, which is what makes "file-only" real here, and it is why no `livePresentation.apply` case or `wire_settings.go` dispatcher entry was added.

NOTES (2026-09-22): deviation — test (b)'s "all four modes" is pinned as the ladder's inert half (no override ⇒ rung 1 opens with the key both ways): `uiPresenter.climb` never sees a mode and the presenter is mode-independent by construction, so mode coverage stays where it already is — `TestPresentDocument_IsReadOnly`, `TestPresentDocument_IsNotExternalEffect` and the untouched `-run 'Classify|Present'` cells.

NOTES (2026-09-22): the existing `presenter_test.go` row "local with a present.command opens on a machine with no desktop" now sets `CommandOnModelDocuments: true` — it is about the DESKTOP gate, and the opt-in is what keeps it reaching the runner.

NOTES (2026-09-22): consequential edit — internal/config/defaults/config.yaml: made necessary by the new registry key (the shipped template documents every `present:` key, ADR 0019).

NOTES (2026-09-22): consequential edit — internal/config/registry_test.go: made necessary by the new registry key (`TestRegistrySetIsTheInverseOfRead`'s `owns` table names the Options field of every row that has a Set).

NOTES (2026-09-22): consequential edit — internal/config/config_test.go: made necessary by the new registry key (`everyKeyFileConfig` must state every key at a non-default value, and the file-only present row enumerates the block's keys).

NOTES (2026-09-22): consequential edit — cmd/apogee/settingsrows_test.go: made necessary by the new registry key (`TestSettingsRowsFormatEffectiveValues` pins one value per registry key and counts them).

NOTES (2026-09-22): `domain.Presenter.IsExecutionCapable` and `tools.PresentDocument.IsExecutionCapable` (item 3) still have no production consumer: the climb gates on the same predicate, factored as `Presentation.executionCapable` so the question and the walk read ONE ladder snapshot (a second `IsExecutionCapable()` call inside climb would re-take the lock and could answer about a ladder the walk never used). The tool words the degrade from `PresentOutcome.CommandWithheld`, as the item's approach directs.

**What.** Recast at the regression check (2026-09-22). `fix(agent)`: closes the gating half of
`apogee-2we`. Depends on item 3.

**Regression guard.** RECAST AGAIN, ratified (owner, 2026-09-22): NO human prompt at all — no
Approver, no Asker, no `PromptSlot`, no Choice mapping; `domain.Presenter`'s fire-and-forget contract
(`internal/domain/present.go:22`, `internal/tui/presenter.go:49`, `internal/tui/doc.go:231`,
`internal/tui/parkedcall.go:24`) stands untouched and is NOT superseded. The gate is the new
file-only `present.command-on-model-documents` key, default false; `present_document` stays
`ClassReadOnly` and a Run, no ladder cell moves and no verdict changes, so ADR 0019 §1, its
2026-07-26 amendment and `docs/design/confinement-execution-contract.md:550-553` all stand — only
ADR 0019 §5 needs the dated addendum, which is item 5's.

**Goal:** With a non-empty `present.command` configured, `present_document` reaches rung 3 only when
`present.command-on-model-documents` is true; with the key absent or false the ladder degrades to
rung 0 and the tool result states the reason in words the user can act on — naming the key and how to
turn it on. With no `present.command` configured, all four modes behave exactly as at base.
`present_document` stays a Run in `resolveLadder` with `ClassReadOnly`, and no Approver, Asker or
`PromptSlot` is involved anywhere on this path.

**Approach (assumed at the header base):** The key is file-only, like every other `present.` key.
`presentConfig` gains `CommandOnModelDocuments *bool` (`yaml:"command-on-model-documents"`),
`toPresentSettings` defaults it FALSE (absent ⇒ off, unlike `auto-open`'s true),
`config.PresentSettings` gains the resolved bool, and `internal/config/registry.go` gains a
`KindBool, Default: "false"` row beside `present.command`, read and set through `presentOf`/`landIn`
as its neighbours are. `cmd/apogee/wire_present.go`'s `presentationRungs` carries it onto
`present.Opener`. `uiPresenter.climb` reads item 3's execution-capable predicate: execution-capable
with the key off ⇒ rung 3 is skipped and the ladder falls to rung 0, never to the OS opener. The
reason travels back as an additive `domain.PresentOutcome` field (freeze-safe, present.go:80-82), not
a live handle across the quiescent boundary (ADR 0008), and `renderPresented` appends one pinned
sentence from a `const` beside `presentedMountNote` in `internal/tools/present_document.go`. Binding:
one decision site (the climb); `resolveLadder`, `resolveLadderAuto`, `planAdmits`, `gateReason` and
`tools.Classify` are untouched; `docs/manual/configuration.md` gains the key's row.

**Files:** internal/config/config.go, internal/config/options.go, internal/config/registry.go, internal/tui/presenter.go, internal/tui/presenter_test.go, internal/domain/present.go, internal/tools/present_document.go, internal/tools/present_document_test.go, cmd/apogee/wire_present.go, docs/manual/configuration.md
**Read first:** internal/tui/presenter.go — uiPresenter, climb, Present, Presentation; internal/domain/present.go — Presenter, PresentRequest, PresentOutcome; internal/present/opener.go — Opener.argv, launchGrace; internal/tools/present_document.go — Execute, renderPresented, presentedMountNote; internal/tui/presenter_test.go — presentOnce, openerRunning, TestPresenterLadderPicksRung; internal/config/config.go — presentConfig, toPresentSettings, PresentSettings; internal/config/registry.go — the `present.auto-open` row, presentOf, landIn; cmd/apogee/wire_present.go — presentationRungs
**Tests.** (a) In `presenter_test.go`, with an override set: the key true ⇒ the recorded argv shows
the override launched; the key false, and the key absent, ⇒ no argv is recorded and the outcome is
rung 0. (b) With no override configured, the ladder is unchanged in all four modes. (c)
`present_document`'s result for an opted-out degrade carries the pinned sentence — read from the emitting
constant, never retyped from this plan — and that sentence names the key; the opted-in result is
unchanged. (d) `internal/config` round-trips the key: absent ⇒ false, `command-on-model-documents:
true` ⇒ true, and the registry row reads and sets it. (e) `TestPresentDocument_IsReadOnly` and
`TestPresentDocument_IsNotExternalEffect` still pass unchanged, and `-run 'Classify|Present'` stays
the untouched-cells check. (a) and (c) must fail against the pre-item tree. Must keep passing: every
`resolution_test.go` cell, `internal/tools/present_document_test.go`,
`cmd/apogee/e2e_present_test.go`, `config_test.go`'s present rows. CHANGELOG sidecar,
`[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go vet ./... && go test ./internal/config/... -run 'Present|Registry|Option' && go test ./internal/tui/... -run 'Present' && go test ./internal/tools/... -run 'Classify|Present' && go test ./internal/agent/... -run 'Resolve|Ladder|Present' && go test ./cmd/apogee/... -run 'E2EPresent'`
**Commit:** `fix(present): an execution-capable present.command runs only behind a file-only opt-in`

## 5. ADR 0019 and CONTEXT record the opt-in rung-3 opener

**What.** Recast at the regression check (2026-09-22). `docs(adr)`: the prose half of `apogee-2we`.
Depends on item 4.

**Regression guard.** Re-scoped to ADR 0019 §5 alone: item 4 produces no gate verdict and no human
prompt, so §1, the 2026-07-26 amendment and the Presenter's "never blocks on the human" contract are
NOT superseded and are left as written. Only §5's "nothing here runs a model-chosen command" gains
the dated addendum; CONTEXT.md and the manual's `present.command` row follow it. The Acceptance greps
the phrases DIRECTLY and drops the `&&`-chained `present_document` filter, which matches no line
today and so exits 0 either way; its second check covers the truncate-gap prose at
`docs/manual/configuration.md:1970-1976` and `docs/manual/probe.md:29-34`, whose claim that the
truncate gap is *the* incomplete Linux fence becomes false after items 6-7 (owner, 2026-09-22) — so
this item lands after items 4, 6 and 7.

**Goal:** ADR 0019 §5 carries a dated addendum stating that a `present_document` call reaches an
execution-capable rung 3 only when `present.command-on-model-documents` is set, and names what that
supersedes; CONTEXT.md's presentation vocabulary agrees with the shipped behaviour. No sentence in
the repo still tells a reader that the rung-3 opener runs a model-chosen command unconditionally, and
the two manual pages no longer say the truncate gap is the one incomplete Linux fence.

**Approach (assumed at the header base):** ADR 0019 §5 rests on "the model never supplies a command"
and "`present.command` is the user's own configuration". Both hold for the *command* and fail for the
*argument*: `{path}` is model-chosen, so `sh {path}` on a model-written workspace file executes it.
Amend in place with a dated addendum in the ADR's own idiom (§5 already carries a "clarified
2026-07-21" precedent) rather than superseding the ADR; §1 and the 2026-07-26 amendment stay as
written. **Prose guard:** the rule is *every* line that tells a reader the rung-3 opener runs a
model-chosen command without an opt-in — find them all with
`rg -n 'present_document|present\.command|presentation ladder' docs/ CONTEXT.md README.md internal/ --glob '!*_test.go' --glob '!docs/**/archived/**'`
and re-point each; the in-code claims at internal/domain/present.go:18-19,
internal/tools/present_document.go:40-42 and internal/present/opener.go:64-67/:204-206 are in scope,
archived plans, reviews and designs are left as written, and this item's named files are not the
closed list.

**Files:** docs/adr/0019-documents-are-presented-not-opened.md, CONTEXT.md, docs/manual/configuration.md, docs/manual/probe.md, internal/tools/doc.go, internal/domain/present.go, internal/tools/present_document.go, internal/present/opener.go
**Read first:** docs/adr/0019-documents-are-presented-not-opened.md — §1 decision, §5, the 2026-07-26 amendment (:218-219); CONTEXT.md — the presentation-ladder section (:2015); docs/design/confinement-execution-contract.md — the 2026-08-12 amendment (:543-558); internal/tools/doc.go — the present_document paragraph; internal/domain/present.go — Presenter doc comment; internal/present/opener.go — argv ("the bound stops at rung 3"), resolveProgram; docs/manual/configuration.md — the `present:` yaml block (:1891-1898), the truncate paragraph (:1970-1976); docs/manual/probe.md — the `backend:` line paragraph (:29-34)
**Tests.** Docs-only — no Go test (the in-code edits are comments). Acceptance is the three checks
below: the phrase grep returning only lines under the dated addendum, the manual grep read against
the amended wording, and `go build ./...` still passing after the comment edits.
**Acceptance.** `rg -n 'outside the Approval gate|never routes through the Approval gate|runs a model-chosen command' docs/adr/0019-*.md CONTEXT.md internal/` returns only lines sitting under the dated addendum; `rg -n 'incomplete|truncate' docs/manual/probe.md docs/manual/configuration.md` shows no line calling the truncate gap the one incomplete Linux fence; then `go build ./...`.
**Commit:** `docs(adr): ADR 0019 records that an execution-capable rung-3 opener needs an opt-in`

## 6. A landlock net-deny box discloses the egress it cannot fence

**What.** `fix(platform)`: the landlock half of `apogee-qi3`, the audit's High "a network-deny box
does not fence UDP or pathname-UNIX-socket egress, and reports no Residual". **Audit corrected:** the
rights half is not codeable — `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` and every UDP right are absent from
the kernel ABI and from `x/sys` v0.47.0, which defines only `LANDLOCK_ACCESS_NET_BIND_TCP` and
`LANDLOCK_ACCESS_NET_CONNECT_TCP`. Only the disclosure ships.

**Regression guard.** `.github/workflows/ci.yml:62-76` greps the probe report for `unfenced:` and
exits 1; ubuntu-latest is landlock ABI ≥ 4, so every CI run of a correct tree would fail — narrow
that step's grep to `unfenced:.*truncate(2)`, the one `ci.yml` edit carried by whichever of items 6
and 7 lands first. `internal/platform/confiner_linux_test.go:40-43` pins `wantCaps` (via
`reflect.DeepEqual`) and `wantLine` for `&landlockConfiner{abi:4}` and goes red: update that row and
widen the Acceptance to `-run 'Landlock|SelectLinuxConfiner'`, which `-run 'Landlock'` does not match.
RATIFIED (owner, 2026-09-22): the network disclosure rides the CAPABILITY LINE only —
`probe.ResidualNotice` stays write-class and must not fire for a network-only set, so no Linux host
gains a startup banner. **The notice's network filter lands WITH THIS ITEM** (moved from item 8 at
the round-2 check): items 6 and 8 are separate commits, so a filter introduced only by item 8 would
leave every landlock ABI ≥ 4 host printing `ResidualNotice`'s truncate sentence naming
"connect(2) UDP" in between. Item 8 asserts the filter; it does not introduce it.

**Goal:** On a host whose landlock ABI can deny TCP at all, `Capabilities()` names the two egress
classes a deny box leaves open — UDP and pathname-UNIX sockets — in `Residuals`, alongside
`truncate(2)` where that also applies, using `domain.ResidualUDPEgress = "connect(2) UDP"` and
`domain.ResidualUnixEgress = "connect(2) AF_UNIX"` declared in `internal/domain/confinement.go`
beside `ConfinementCaps.Residuals`, so `internal/platform` and `internal/probe` name one definition
rather than two that drift. `probe.ResidualNotice` filters network tokens out and fires nothing for a
network-only set. The ABI ladder, the fs mask, the fail-closed `networkDenyDecision` and the
`truncate(2)` disclosure are unchanged.

**Approach (assumed at the header base):** `(*landlockConfiner).Capabilities()` sets
`Residuals = []string{"truncate(2)"}` for ABI 1–2 and nothing else; the doc comment on
`domain.ConfinementCaps.Residuals` calls residuals *write-class*, which a network residual widens —
amend that comment here. The disclosure is the standing fact "a deny box leaves these open", appended
whenever `NetworkEgress` is true, not a per-box answer. The `ResidualNotice` half is a DENY-list on
the two `domain.Residual*` tokens, so an unknown write-class token still reaches the sentence.
Binding: `AutoEligible()` still reads `FSWrite` alone, and this item must not make a Linux host
announce a residual at startup where it announces none today.

**Files:** internal/platform/landlock_linux.go, internal/platform/landlock_linux_test.go, internal/platform/confiner_linux_test.go, internal/domain/confinement.go, internal/domain/confinement_test.go, internal/probe/confinement.go, internal/probe/confinement_test.go, .github/workflows/ci.yml
**Read first:** internal/platform/landlock_linux.go — Capabilities, landlockABINetwork, networkDenyDecision; internal/platform/landlock_linux_test.go — TestLandlockCapabilitiesHonest, TestLandlockResidualsMatchHostABI; internal/platform/confiner_linux_test.go — TestSelectLinuxConfiner; internal/domain/confinement.go — ConfinementCaps.Residuals, AutoEligible; internal/probe/confinement.go — ResidualNotice, CapabilityLine; .github/workflows/ci.yml — "probe discloses no residual on a modern kernel"; cmd/apogee/wire_boot.go — announceConfinement
**Tests.** (a) `TestLandlockCapabilitiesHonest`'s `wantResiduals` column gains the two network tokens
for every ABI ≥ 4 row and keeps `truncate(2)` where it stands — the ABI 1–2 rows carry `truncate(2)`
alone and the ABI 4/6 rows the network tokens alone, the two sets disjoint by construction
(`landlock_linux.go:217`), so `TestAccessMaskForABI` gains nothing; (b)
`TestLandlockResidualsMatchHostABI`'s host-derived expectation ALSO gains the two network tokens when
`c.abi >= landlockABINetwork`, keeping the skip for a landlock-less kernel; (c) a domain-level case
pins that a caps value carrying only network residuals is still `AutoEligible`; (d)
`TestSelectLinuxConfiner`'s landlock row carries the new `wantCaps`/`wantLine`; (e) a STANDALONE
probe case — not a `residualSets` row, which item 8 owns — pins that a network-only caps value yields
an empty `ResidualNotice`, while `TestResidualNotice`'s existing matrix passes unchanged. (a), (d)
and (e) must fail against the pre-item tree. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/platform/... -run 'Landlock|SelectLinuxConfiner' && go test ./internal/domain/... -run 'Confinement|Caps' && go test ./internal/probe/...`
**Commit:** `fix(platform): a landlock net-deny box discloses the UDP and UNIX-socket egress it leaves open`

## 7. A bwrap net-deny box discloses its pathname-UNIX-socket egress

**What.** `fix(platform)`: the namespace half of `apogee-qi3`. `--unshare-net` cuts UDP and abstract
sockets, but a pathname UNIX socket is filesystem-scoped, so `/var/run/docker.sock` and
`/run/user/<uid>/bus` stay reachable through the `--ro-bind / /` mount.

**Regression guard.** `internal/platform/confiner_linux_test.go:44-53`'s
`namespace_when_landlock_cannot_fence` row asserts `reflect.DeepEqual(caps, {FSWrite:true,
NetworkEgress:true})` and a `wantLine` with no residual; the new token fails both. That file joins
**Files:**, the row's `wantCaps`/`wantLine` are updated, and the Acceptance widens to
`-run 'Namespace|SelectLinuxConfiner'`, which `-run 'Namespace'` does not match. The same
`.github/workflows/ci.yml:62-76` step trips wherever the selector falls to the namespace backend —
its grep narrows to `unfenced:.*truncate(2)`, the one `ci.yml` edit carried by whichever of items 6
and 7 lands first (stated in item 6). This item supersedes
`docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md` §4 (:109-111, "Exit 0 ⇒
`{FSWrite:true, NetworkEgress:true, Residuals:nil}` … and nothing is residual") with a dated
amendment note in that ADR; the two `docs/design/confinement-execution-contract.md` lines (:843,
:990) are item 9's.

**Goal:** `(*namespaceConfiner).Capabilities()` names `connect(2) AF_UNIX` in `Residuals` when bwrap
is present, and names nothing when bwrap is absent (that path stays wholly `Unavailable`). The
package's own claim that this backend "is complete or absent, never partial" no longer stands
anywhere in its prose or its tests.

**Approach (assumed at the header base):** `Capabilities()` returns `{FSWrite: true,
NetworkEgress: true}` and never sets `Residuals`; the file header and the method's doc comment both
assert nothing is residual, and `TestNamespaceCapabilitiesHonest` fails if `caps.Residuals != nil`.
Set the one token, and correct both comments and that assertion. Reuse item 6's
`domain.ResidualUnixEgress` rather than spelling the string twice — one definition, in
`internal/domain`. **Prose guard:** the rule is every comment or doc line asserting this backend
fences completely; find them with
`rg -n 'never partial|complete or absent|nothing is residual' internal/platform/ docs/`.

**Files:** internal/platform/namespace_linux.go, internal/platform/namespace_linux_test.go, internal/platform/confiner_linux_test.go, docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md
**Read first:** internal/platform/namespace_linux.go — Capabilities, NewNamespaceConfiner, file header block; internal/platform/namespace_linux_test.go — TestNamespaceCapabilitiesHonest, TestNamespaceProbe; internal/platform/confiner_linux_test.go — TestSelectLinuxConfiner, TestNewConfinerOnThisHost; docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md — §4; internal/platform/confinetest/confinetest.go — discloses, truncate_outside_box
**Tests.** (a) `TestNamespaceCapabilitiesHonest`'s residual assertion is replaced: bwrap present ⇒
`Residuals` is exactly `[]string{"connect(2) AF_UNIX"}`; bwrap absent ⇒ `Unavailable` non-empty and
`Residuals` empty; (b) the existing `TestNamespaceProbe`/`TestNamespaceProbeNetwork` rows keep
passing unchanged on this box (bwrap is present here); (c) `TestSelectLinuxConfiner`'s
`namespace_when_landlock_cannot_fence` row carries the new `wantCaps`/`wantLine`. (a) and (c) must
fail against the pre-item tree. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/platform/... -run 'Namespace|SelectLinuxConfiner'`
**Commit:** `fix(platform): a bwrap net-deny box discloses its pathname-UNIX-socket egress`

## 8. The residual notice words each residual it names

**What.** Recast at the regression check (2026-09-22). `fix(probe)`: the user-facing half of
`apogee-qi3`. Depends on items 6 and 7.

**Regression guard.** RATIFIED (owner, 2026-09-22): `ResidualNotice` stays write-class, and its
network filter LANDS WITH ITEM 6 (stated there) so no commit in the sequence renders the truncate
sentence naming a network token — this item ASSERTS the filter, never introduces it. The tokens are
`domain.ResidualUDPEgress`/`domain.ResidualUnixEgress`, declared once by item 6 beside
`ConfinementCaps.Residuals` and referenced from both packages, and the filter stays a DENY-list so an
unknown write-class token still gets its neutral clause instead of being swallowed. Matrix changes,
explicitly: `residualSets` gains `{"connect(2) UDP"}` and `{"truncate(2)","connect(2) UDP"}`, `want`
(confinement_test.go:135) becomes "the set holds a write-class token", the counter at :166 becomes
`fired != 2`, and the `DegradedNotice`-exclusivity assertion is kept. This item supersedes
`internal/platform/landlock_linux.go:209-210` ("every surface that words the caps says so …
ResidualNotice") — after it `ResidualNotice` deliberately does not, and `CapabilityLine` alone is the
honesty surface. The manual's truncate-gap prose is item 5's by ratified call.

**Goal:** `ResidualNotice` stays the write-class notice it is today: it words the residuals it
reports, keeps network tokens out of its sentence entirely, and produces no notice at all for a caps
value carrying only network residuals. `CapabilityLine` lists the new tokens — it is the honesty
surface for them. No rendered sentence pairs a residual token with a consequence that does not follow
from it.

**Approach (assumed at the header base):** `ResidualNotice` (`internal/probe/confinement.go`)
interpolates the whole residual list into one sentence hard-wired to truncate ("cannot fence %s — a
confined command can still empty an existing file outside the workspace"). Item 6's filter keeps the
network tokens out; this item words whatever write-class tokens remain, giving an unknown one a
neutral clause rather than a wrong one, and completes the matrix. `CapabilityLine`'s
`" · unfenced: "` join already renders a list and needs no shape change — it is where the network
tokens surface. Binding: the firing condition (auto + confine + FSWrite + non-empty *write-class*
residuals) is otherwise unchanged, and no Linux host may announce a residual at startup where it
announces none today.

**Files:** internal/probe/confinement.go, internal/probe/confinement_test.go
**Read first:** internal/probe/confinement.go — ResidualNotice, CapabilityLine, DegradedNotice, availability; internal/probe/confinement_test.go — TestResidualNotice, TestCapabilityLine; internal/platform/landlock_linux.go — Capabilities, landlockABINetwork; internal/domain/confinement.go — ConfinementCaps.Residuals, AutoEligible; cmd/apogee/wire_boot.go — announceConfinement; cmd/apogee/headless.go, cmd/apogee/daemon.go — the other ResidualNotice call sites; internal/tui/confine.go — the /confine status line
**Tests.** (a) `TestResidualNotice`'s `residualSets` gains `{"connect(2) UDP"}` and
`{"truncate(2)","connect(2) UDP"}`, `want` becomes "the set holds a write-class token" and the
counter becomes `fired != 2`: the network-only set fires nothing, the mixed set fires the truncate
sentence carrying no network token; (b) `TestCapabilityLine`'s pinned literals gain a network row —
`"landlock (fs-write: available · network: available · unfenced: connect(2) UDP, connect(2) AF_UNIX)"`
as the code emits it, read from the emitting function, never retyped from this plan; (c) the
never-co-fires-with-`DegradedNotice` assertion stands; (d) an unknown write-class token gets the
neutral clause. (a), (b) and (d) must fail against the pre-item tree. CHANGELOG sidecar,
`[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/probe/... && go test ./internal/tui/... -run 'Confine'`
**Commit:** `fix(probe): the residual notice words each residual it names`

## 9. A confinetest row drives UDP egress under a net-deny box

**What.** `test(confinetest)`: the battery row the audit asks for, plus the contract document's
record of it. Depends on items 6 and 7.

**Regression guard.** The undisclosed branch is keyed on the LISTENER, never on the child's exit
status — verified false for bwrap on 2026-09-22: `bwrap --unshare-net --ro-bind / / --dev /dev --proc
/proc bash -c 'exec 3<>/dev/udp/127.0.0.1/N; printf x >&3'` exits 0, because connect+write on a
SOCK_DGRAM are local to the new netns while the parent's listener receives nothing —
`TestNamespaceProbeNetwork` (namespace_linux_test.go:254) would go red on its first run. So:
`discloses(…, "connect(2) UDP")` false ⇒ the parent's `net.ListenPacket` read hits its deadline with
no datagram; true ⇒ the datagram arrives. The **Goal:** line and §6.2's new table cell carry that
same phrasing — "the datagram does not reach the listener" / "is delivered" — mirroring row #12's
`Residuals`-keyed wording rather than row #7's "OS-denied".

**Goal:** `confinetest.ProbeNetwork` carries a row that really sends UDP from inside a net-deny box
and asserts the outcome against the backend's own disclosure — the datagram does not reach the
listener when the backend does not disclose UDP egress, and is delivered when it does — and the
confinement execution contract's §5 and §6.2 record both the row and the widened meaning of a
residual. The row skips cleanly where its shell dialect or its backend is unavailable.

**Approach (assumed at the header base):** A row is a `t.Run` inside `ProbeNetwork`; copy
`truncate_outside_box`, which pairs a `(line, ok)` dialect helper with
`discloses(c.Capabilities().Residuals, "truncate(2)")` to key its assertion on the disclosure, not
the kernel. `runConnectProbe` hard-codes `bash` for `/dev/tcp`, so a `/dev/udp` helper in
`lines_other.go` takes the same constraint and returns `ok == false` where bash is absent. The
listener is a real `net.ListenPacket` on loopback, read with a deadline. §6.2's table gains the row
with an amendment note in the style of the 2026-07-22 / 2026-08-22 / 2026-08-26 entries; §5's
capability-honesty text gains the network classes. **This row cannot be exercised against landlock on
this box** — it runs against bwrap here, the landlock half is compile-checked only; say so in a NOTES
line rather than reporting it as verified.

**Files:** internal/platform/confinetest/confinetest.go, internal/platform/confinetest/lines_other.go, internal/platform/confinetest/lines_windows.go, docs/design/confinement-execution-contract.md
**Read first:** internal/platform/confinetest/confinetest.go — ProbeNetwork, runConnectProbe, discloses, truncate_outside_box; internal/platform/confinetest/lines_other.go — truncateLine, writeLine; internal/platform/confinetest/lines_windows.go — truncateLine stub; internal/platform/namespace_linux_test.go — TestNamespaceProbeNetwork; internal/platform/landlock_linux_test.go — TestLandlockProbeNetwork; docs/design/confinement-execution-contract.md — §5, §6.2 row table
**Tests.** The row itself is the test, driven by the existing `TestNamespaceProbeNetwork` (passes
here) and `TestLandlockProbeNetwork` (skips here). Assert: with `connect(2) UDP` disclosed, the send
succeeds and the listener receives the datagram; undisclosed, the listener's deadline expires with no
datagram — the confined child's exit status is not an observable for connectionless UDP and is never
asserted on. Must keep passing: every existing `confinetest` row on this box, and `go vet` for the
Windows dialect file. CHANGELOG sidecar, `[Unreleased]/Added`.
**Acceptance.** `go build ./... && go test ./internal/platform/... -run 'ProbeNetwork' && GOOS=windows go vet ./internal/platform/...`
**Commit:** `test(confinetest): a battery row drives UDP egress under a net-deny box`

## 10. A root spared for a live sibling is handed off, never deleted

**What.** `fix(winlabel)`: closes `apogee-ea3`, the audit's High "two confining sessions each spare
the shared root, then strand the Low label on disk unrecoverably".

**Regression guard.** The spared root is handed off UNJUDGED (`RootJudged: false`): `revertibleRoots`
skips a sibling-claimed root with a bare `continue` BEFORE the `rootClearable` read
(retire.go:219-222), so nothing ever read its label — handing it off judged would make a later
`Recover` skip the read (retire.go:223-226) and NULL-SACL that tree whatever it now carries,
reopening F-08's clear prong. `walk_windows.go` — `revertSparingLiveSiblings` (:261-271), the only
site joining the spared set to `restorablePriors`' handoff into `remaining` — and `session.go`, whose
`Journal.Retire` doc states the removal rule, join **Files:**; both are `//go:build windows`, so a
signature change there is invisible to `go build ./...`. Test (d) is worded end-to-end, because
`ResidueIn` already reports every `Root` entry today (journal.go:78-86). This item supersedes the
RULE that a spared root must not keep this journal — every comment stating it, found with a grep;
retire.go:194-199, walk_windows.go:372-374, session.go:181-190 and `Recover`'s doc
(walk_windows.go:409-413) are the known sites, never the closed list.

**Goal:** When two sessions confine one workspace and their teardowns overlap, the session that
spares the shared root because its sibling is alive keeps that root as an undischarged entry in its
own journal, and `retire` never deletes a journal file that still carries one. After both sessions
close in either order, either the Low label is cleared or a journal survives on disk that a later
`Recover` can clear it from.

**Approach (assumed at the header base):** `restorablePriors` builds its `handoff` list only from
entries whose `PriorSDDL` is non-empty, so a root `revertibleRoots` spared with a bare `continue`
never becomes part of `remaining` and `retire` falls through to its single `os.Remove`. Make a spared
root produce a handoff entry for itself — carrying `Root` and `RootJudged` — and have `retire` treat
a remaining root entry exactly as it already treats a remaining prior: rewrite the file, never delete
it. Binding standards: the decision stays pure and parameter-injected (`alive func(int) bool`,
`readLabel func(string) (string, error)`) — this package's "retire seam pattern"; no package var, no
syscall inside a decider.

**Files:** internal/platform/winlabel/retire.go, internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/session.go, internal/platform/winlabel/retire_test.go, internal/platform/winlabel/journal.go
**Read first:** internal/platform/winlabel/retire.go — retire, revertibleRoots, restorablePriors, rootClearable; internal/platform/winlabel/walk_windows.go — revertSparingLiveSiblings, judgePriors, revertJournal, Recover; internal/platform/winlabel/session.go — Journal.Retire, forgetLabelled; internal/platform/winlabel/journal.go — Entry.RootJudged, Record.Roots, ResidueIn, WriteJournal; internal/platform/winlabel/retire_test.go — TestRevertibleRootsSparesOnlyALiveSiblingsRoots, TestRetireLabelJournalRewritesTheFileToTheHandedOffEntries, TestRestorablePriorsHandsOffSiblingClaimedTrees; internal/platform/winlabel/journal_state_test.go — TestJournalRetireKeepsTheHandoffAndReopensTheMemo
**Tests.** Pure table tests, all runnable on Linux: (a) two sibling records over one root, sibling
alive ⇒ `retire` rewrites the file and the rewritten record still carries the root entry; (b) the
same pair with the sibling dead ⇒ the root is reverted and the file is removed, as today; (c) the
overlapping-close interleave driven twice, once in each order, ending with no journal on disk only
when the label was actually cleared; (d) end to end — drive `retire` over the sparing case, then
assert the journal FILE still exists on disk and `ResidueIn` names its root. (a), (c), (d) must fail
against the pre-item tree. Must keep passing: every case in `retire_test.go`, `journal_test.go`,
`journal_state_test.go`, `journal_race_test.go`. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/platform/winlabel/... && GOOS=windows go vet ./internal/platform/winlabel/...`
**Commit:** `fix(winlabel): a root spared for a live sibling is handed off, never deleted`

## 11. A persisted verdict skips the label read, never the guardrail

**What.** `fix(winlabel)`: the Linux-provable half of `apogee-73s`, the audit's High "a forgeable
confinement journal drives the victim's next Recover/Retire to NULL-SACL or label-write arbitrary
paths". Depends on item 10. The per-run secret and creation-time liveness are out of scope by
ratified call and are beaded at closeout.

**Regression guard.** RATIFIED (owner, 2026-09-22): the carry gets a BOUNDED LIFE — the `Entry` gains
one additive JSON field (a carry count or a first-carried timestamp) and a carried prior is dropped
once a bounded number of `Recover` sweeps have passed, so the journal always retires. That explicitly
supersedes retire.go:80-84, which records the drop as intended because "keeping the entry would
re-attempt the write forever". `restorablePriors` must route `!entry.Judged` entries into `handoff`
and NEVER into `restore`: it puts every entry with `PriorSDDL != ""` into `restore`
(retire.go:276-283) and `revertJournal` hands that map to `SetSDDL` (walk_windows.go:398-402), so a
carried prior would be written over a live foreign label — F-08's restore prong reopened; every prior
restorable today carries `Judged == true` (walk_windows.go:343-345). The Acceptance's
`GOOS=windows go test -run XXX` spelling runs the cross-compiled binary on the host and fails every
correct run — use the house compile-only form
`GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`. `go build ./...` does NOT cover
this package's joiners: every caller of these functions is behind `//go:build windows`.

**Goal:** (a) A journal entry carrying `root_judged: true` is still refused by `isVolumeRoot`, so a
planted `{"path":"C:\\","root":true,"root_judged":true}` never reaches a clear. (b) A journalled
foreign prior whose path currently carries neither apogee's own Low label nor no label at all is
neither restored nor destroyed on that run: it survives in the journal for a later one, and the run
does not abort.

**Approach (assumed at the header base):** (a) `revertibleRoots` skips the whole read-and-judge block
when `RootJudged` is set, and `judgePriors` computes `judgeRoot := entry.Root && !entry.RootJudged` —
in both the flag skips the *label re-read*, never the guardrail, so apply `isVolumeRoot`
unconditionally. (b) `priorRestorable`'s final case is `return false, true`, which permanently empties
`PriorSDDL` whenever the current label is anything but apogee's Low, including unlabelled
(`IsLowLabel("") == false`). Give the predicate a third, explicitly-named "carry this entry forward"
outcome, distinct from the unknown-read case that aborts the revert, and thread it through
`judgePriors` and `restorablePriors`; `judgePriors` is Windows-tagged, so keep every decision it
makes in a pure function testable on Linux.

**Files:** internal/platform/winlabel/retire.go, internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/journal.go, internal/platform/winlabel/retire_test.go
**Read first:** internal/platform/winlabel/retire.go — priorRestorable, restorablePriors, revertibleRoots, isVolumeRoot, rootClearable; internal/platform/winlabel/walk_windows.go — judgePriors, revertJournal, revertSparingLiveSiblings; internal/platform/winlabel/journal.go — Entry.Judged, Entry.RootJudged, recordEntry; internal/platform/winlabel/retire_test.go — TestPriorRestorableTable, TestRevertibleRootsClearsOnlyRootsApogeesOwnLabelVouchesFor, TestRootClearableTable; internal/platform/winlabel/walk_windows_test.go — TestJudgePriorsSkipsAnAlreadyJudgedRoot, TestJudgePriorsClearsARootOnlyJournalItCannotRewrite; internal/platform/winlabel/sddl.go — IsLowLabel, clearSDDL
**Tests.** (a) A row crossing the two existing cases in
`TestRevertibleRootsClearsOnlyRootsApogeesOwnLabelVouchesFor` — a volume root that also carries
`RootJudged` is dropped; (b) `priorRestorable` table rows for: current label is apogee's Low ⇒
restore; path missing ⇒ drop; read error ⇒ the existing abort; unlabelled ⇒ carry; foreign label ⇒
carry; (c) an end-to-end `retire` case where a carried prior survives a first run and restores on a
second once the path is Low again; (d) a table over the new carry field: an entry carried past its
bound is dropped, and a `!Judged` entry is routed into `handoff`, never into `restore`. All four must
fail against the pre-item tree. Must keep passing: every `retire_test.go` and `journal_test.go` case,
and item 10's new ones. CHANGELOG sidecar, `[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/platform/winlabel/... && GOOS=windows go vet ./internal/platform/winlabel/... && GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`
**Commit:** `fix(winlabel): a persisted verdict skips the label read, never the volume-root guardrail`

## 12. A restored snapshot's roles, counts and sizes are checked before it is applied

**What.** `fix(agent)`: the shape half of `apogee-mre`, the audit's Medium "session-snapshot
ingestion restores untrusted history as committed conversation".

**Regression guard.** The per-message cap is raised to `maxRestoredMessageBytes = maxFileReadBytes`
(10 MiB), so no size a shipped tool can commit is refused: a `read_file` with an explicit `end_line`
over a one-line file is byte-uncapped (internal/tools/read_file.go:286-296) and survives
`clampToolResult` whole, because `clampToBound` returns the content untouched when the line-based
`TruncateToolResult` cannot shrink it (internal/agent/dispatch.go:1771-1780) — at `1 << 20` a
legitimate session would be refused permanently. `maxRestoredMessages = 4096` stands. `decodeState`
is also the fork primitive behind public `apogee.CutSession` (apogee.go:700), so every threshold must
sit above what apogee's own tools commit: a refusal makes a session unresumable AND unforkable at
once. The mid-history `RoleSystem` refusal supersedes internal/agent/state.go:262-268, whose doc
comment records leaving such a message alone as deliberate (`Conversation.PrefixEnd` tolerates it) —
rewrite that comment in this same item.

**Goal:** `decodeState` refuses a payload whose conversation carries a message with a role outside
the four `domain.Role` constants, more than `maxRestoredMessages = 4096` messages, or a single
message over `maxRestoredMessageBytes = maxFileReadBytes` (10 MiB), with a named sentinel error. A
refused payload leaves the live session exactly as it was — on `--resume`, on the browser's
`RestoreSession`, and on `CutSession` — and a legitimate session of any realistic size still restores.

**Approach (assumed at the header base):** Today's only validation is the version check,
`tasklist.Replace`'s caps, and `dropLeadingSystem`, which strips only a *leading* run of System
messages — a crafted `[user, system, assistant, …]` keeps its system message and the Anthropic wire
hoists it into the system prompt; `domain.Message.UnmarshalJSON` assigns `Role` with no enum check.
Put the checks in `decodeState`, the one decode seam all three readers share, and refuse before
anything is swapped in — the idiom is `internal/session/store.go`'s
`maxRecordBytes`/`ErrRecordTooLarge` and `tasklist.Replace`'s refuse-and-leave-unchanged. Binding: a
`RoleSystem` message past the leading run is refused, not stripped; the ratified call is refusal.

**Files:** internal/agent/state.go, internal/agent/state_test.go, internal/agent/restoresession_test.go
**Read first:** internal/agent/state.go — decodeState, restoreState, restoreSnapshot, CutSession, dropLeadingSystem; internal/agent/dispatch.go — clampToolResult, clampToBound, structuralFloor; internal/agent/state_test.go — TestRestore_RejectsAnOverCapTaskList, TestAgentState_EncodesStableKeyNames, TestSnapshot_RestoresPendingInput; internal/agent/restoresession_test.go — TestRestoreSession_RejectsCorruptPayloadUntouched, TestRestoreSession_RefusalLeavesConsolesAndTallyStanding; internal/agent/promptseam_test.go — TestRestoreSeam_StoredSystemMessageDroppedAndNotDoubledOnTheWire, TestRestoreSeam_ExchangeBoundaryShiftsWithTheDroppedPrefix; apogee.go — CutSession; internal/session/store.go — maxRecordBytes, ErrRecordTooLarge
**Tests.** Hand-marshalled `agentState` payloads, following `TestRestore_RejectsAnOverCapTaskList`:
(a) a role of `"developer"` refuses; (b) a `RoleSystem` message at index 2 refuses; (c) 4097 messages
refuses; (d) a message over `maxRestoredMessageBytes` refuses, while a 2 MiB message — a size
`read_file` can commit — still restores; (e) each refusal leaves the live conversation, task list,
consoles and usage tally standing, on both `RestoreSession` and `CutSession`; (f) a 4096-message
payload of legitimate roles still restores. (a)–(d) must fail against the pre-item tree. Must keep
passing: `TestAgentState_EncodesStableKeyNames` (add no JSON key), every `restoresession_test.go`
case, `TestSnapshot_RestoresPendingInput`, and `promptseam_test.go`'s two `TestRestoreSeam_*` cases,
which seed a *leading* `RoleSystem` message and must keep restoring. CHANGELOG sidecar,
`[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/agent/... -run 'Restore|Snapshot|CutSession' && go test ./internal/session/...`
**Commit:** `fix(agent): a restored snapshot's roles, counts and sizes are checked before it is applied`

## 13. A restored snapshot cannot forge the engine's own structure

**What.** `fix(agent)`: the content half of `apogee-mre`. Depends on item 12.

**Regression guard.** The refused set is ONLY the fences apogee never commits — the advice pair, the
engine-note pair, `contextFileHeader`/`contextFileFooter`, `delegateReportFence`. `TaskListFence`
("Task list — ", internal/tasklist/tasklist.go:28) and the orientation header (line 1 of
internal/agent/prompts/orientation.txt) are explicitly EXCLUDED: both are committed verbatim by
shipped default-on tools (`task_list` returns `okResult(call.ID, list.Render())`,
internal/tools/task_list.go:103, default-on at internal/tools/registry.go:334), so refusing them
would make every ordinary session unresumable and unforkable. Test (b) drops its task-list case and
gains a keep-passing case that a `task_list` result still restores. Test (e)'s `FileRefs` refusal is
dropped: `decodeState` is Agent-less and workspace-blind, and an escaping ref is already fenced by
`security.SafeOpen` in `readFileRef` (internal/agent/loop.go:1300-1307) — only the size bound on
`pendingInput.Text` stays at the decode seam. `internal/agent/standingblocks_test.go` joins
**Files:**: its pinned `want` list, its name and its comment gain the two engine-note prefixes
(`TestStandingBlocks_FencesAreTheTableColumnPlusTheAdviceFence`, :136-149). The Approach's premise is
corrected: `Request.InjectContext` inserts an unattributed USER message at the role-safe position
(internal/domain/hooks.go:544-558), not into the system message.

**Goal:** A restored message, task row, deferred-correction string or `pendingInput` whose content
carries a standing-block fence, an advice fence or an engine-note marker refuses the whole payload
with item 12's sentinel; a restored `pendingInput` is bounded in size; a legitimate session whose
text merely mentions those words — or whose transcript carries a `task_list` result or the
orientation header — still restores.

**Approach (assumed at the header base):** Reuse the existing detector, never reinvent it:
`forgesStandingStructure` (`internal/agent/contextfiles.go`) prefix-matches a trimmed line against
`standingFences()` (`internal/agent/standingblocks.go`). `standingFences()` does not yet carry
`EngineNoteFencePrefix`/`EngineNoteFenceClosePrefix` (`internal/domain/advice.go`) — add both, which
also tightens the context-file path sharing the list. The ratified call is refusal, so this path uses
the *detector*, not `fenceContent` (which prefixes rather than drops). The deferred-correction queue
round-trips through `conversationJSON` into `Request.InjectContext`, the same
unattributable-injection vector, so it is checked too; `pendingInput` is kept, checked and bounded
rather than refused outright.

**Files:** internal/agent/state.go, internal/agent/standingblocks.go, internal/agent/state_test.go, internal/agent/standingblocks_test.go, internal/agent/contextfiles_test.go
**Read first:** internal/agent/standingblocks.go — standingFences, standingBlocks, standingFenceList; internal/agent/contextfiles.go — forgesStandingStructure, fenceContent, contextFileHeader, contextFileFooter; internal/agent/standingblocks_test.go — TestStandingBlocks_FencesAreTheTableColumnPlusTheAdviceFence; internal/tasklist/tasklist.go — Fence, HeaderFormat, Render; internal/tools/task_list.go — Execute; internal/domain/advice.go — EngineNoteFencePrefix, EngineNoteFenceClosePrefix, recordContent; internal/agent/loop.go — resolveFileRefs, readFileRef; internal/domain/hooks.go — Request.InjectContext, Conversation.Defer, conversationJSON
**Tests.** (a) A restored assistant message whose body opens a line with the engine-note prefix
refuses; (b) the same for an advice fence, a context-file header and the delegate-report fence;
(c) a deferred-correction string carrying a fence refuses; (d) a `pendingInput.Text` carrying a fence
refuses, and one over the bound refuses; (e) a transcript carrying a `task_list` result and one
carrying the orientation header both still restore — those two are excluded by design; (f) a message
whose prose merely contains the words "engine" and "advice" mid-line still restores;
(g) `standingFences()` now contains both engine-note prefixes, asserted through
`forgesStandingStructure` on a context-file line. (a)–(d) must fail against the pre-item tree. Must
keep passing: every `contextfiles_test.go` case, item 12's tests. CHANGELOG sidecar,
`[Unreleased]/Fixed`.
**Acceptance.** `go build ./... && go test ./internal/agent/... -run 'Restore|Snapshot|ContextFile|StandingBlock|Fence'`
**Commit:** `fix(agent): a restored snapshot cannot forge the engine's own structure`

## 14. SECURITY.md and AGENTS.md state what hook hydration trusts

**What.** `docs(security)`: closes `apogee-242`, the audit's Critical "repo-shipped `.beads/hooks/*`
become live git hooks with the user's full privileges", by the ratified won't-fix-and-document call.
Not an apogee code change: the hooks are this repo's own attribution stripper and `bd init` guard,
and the activation is `bd`'s.

**Regression guard.** The Acceptance as written already exits 0 at the base commit — AGENTS.md:23
matches both patterns and SECURITY.md matches neither — so it cannot tell a finished item from an
untouched tree. Make it per-file and pin the new wording:
`rg -q 'core\.hooksPath' SECURITY.md && rg -q 'core\.hooksPath' AGENTS.md && rg -q 'trust' SECURITY.md`,
two greps that each fail against the pre-item tree. Drop `CLAUDE.md` from the prose guard's path
list: it does not exist in this repo (only `AGENTS.md`), so the grep errors out; the remaining hits
sit in `docs/reviews/`, `docs/adr/` and archived plans, historical and correctly outside **Files:**.

**Goal:** `SECURITY.md` names repo-shipped git hooks as a trust boundary the threat model states
explicitly — that `bd init` / `bd hooks install` points `core.hooksPath` at a checkout-controlled
directory, that the next `git commit`/`checkout`/`push` then runs that repo's shell with the user's
privileges, and that a clone of an untrusted fork must read `.beads/hooks/` before hydrating.
`AGENTS.md`'s hooks paragraph carries the same warning at the point it tells a reader to run
`bd hooks install`. No line in either file implies apogee fences this.

**Approach (assumed at the header base):** `SECURITY.md` already carries the MCP-environment
precedent from plan D — a stated exception where the posture is a deliberate trust decision rather
than a control. Follow that shape and that voice. `AGENTS.md`'s "Git hooks live in `.beads/hooks/`"
paragraph already explains the marker handling; the warning belongs in it, not as a new section.
**Prose guard:** the rule is every line that instructs a reader or an agent to run `bd hooks install`
or `bd init` without qualification — find them with
`rg -n 'bd hooks install|bd init|core\.hooksPath' AGENTS.md README.md SECURITY.md docs/ .agents/`.

**Files:** SECURITY.md, AGENTS.md
**Read first:** SECURITY.md — the "What counts" bullet list, the stdio-MCP env-allowlist exception, "Out of scope"; AGENTS.md — the "Git hooks live in `.beads/hooks/`" bullet; docs/reviews/code-audit-2026-09-20.md — the Critical `.beads/hooks/*` finding and its Fix line; .beads/hooks/ — commit-msg, prepare-commit-msg
**Tests.** Docs-only — no Go test. Acceptance is the two per-file greps below, each of which fails
against the pre-item tree, plus no unqualified hydration instruction left.
**Acceptance.** `rg -q 'core\.hooksPath' SECURITY.md && rg -q 'core\.hooksPath' AGENTS.md && rg -q 'trust' SECURITY.md && go build ./...`
**Commit:** `docs(security): SECURITY.md and AGENTS.md state what git-hook hydration trusts`
