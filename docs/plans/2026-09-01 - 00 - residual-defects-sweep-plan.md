# Residual defects sweep — the nineteen findings the 2026-08-28..08-31 closeouts deferred

**Goal:** Close every entry in `ISSUES.md`'s "Open defects" section — the nineteen residuals six plan
closeouts deferred between 2026-08-28 and 2026-08-31 — so the register's defect half is empty. One
item per residual, in the register's order, except where a residual's fix was split at write time.

**Date:** 2026-09-01 · **Status:** unexecuted
**sized for:** ~200k-context host
**Base commit:** `af9341d2`

**Sources:** `ISSUES.md:36-200` (the entries, verbatim) · `docs/plans/archived/2026-08-31 - 03 - sub-agents-server-root-key-and-picker-plan.md` · `docs/plans/archived/2026-08-31 - 00 - base-guidance-and-shipped-skills-plan.md` · `docs/plans/archived/2026-08-30 - 01 - sub-agent-run-view-plan.md` · `docs/plans/archived/2026-08-29 - 02 - tool-surface-transparency-plan.md` · `docs/plans/archived/2026-08-29 - 00 - audit-residue-closeout-plan.md` · `docs/plans/archived/2026-08-28 - 02 - deferred-residuals-sweep-plan.md` · `docs/plans/archived/2026-08-18 - 00 - open-defects-plan.md:25-27` (design call 4) · `docs/adr/0063-sub-agent-runs-are-user-addressable-views.md` · `docs/adr/0064-the-system-prompt-ships-an-embedded-default.md`

**Ratified design calls** (owner, 2026-09-01, this session):
- **Scope:** all nineteen residuals, one plan; F-08 included, its validator written portable so Linux tests it.
- **Predecessor:** plan `2026-08-31 - 06` lands first — item 1 is the gate, item 9 is written against the tree it leaves.
- **Run-view refusal:** the status-line flash carries it inside a view; the depth-0 note still lands. Design call 4 (2026-08-18) stands unchanged.
- **F-08 rule:** a journal Root is cleared only where apogee's own Low label still stands, plus a shape guard refusing volume roots; the decision is pure and table-tested on Linux.
- **Auto row:** the picker's last row is `auto`; accepting it REMOVES the root `sub-agents-server:` key, so the opt-out survives a restart.
- **Headless notice:** one routing-agnostic sentence — item 5 carries the wording verbatim.
- **`idOf`:** an unparseable stack block is keyed on a content hash, never the empty id.
- **`relist`:** its byte-identical push-after-unlock is fixed in item 2 alongside `Retarget`'s.
- **`/skills` header:** asserted off the scrolled-up transcript with its exact count; no second golden. Premise correction: the header scrolled off at `4d1f18c3` when the shipped set grew 2→4, not in the re-record.

**Standing requirements:** `skills: coding-standards`

**Out of scope:**
- `ISSUES.md`'s `## Improvements / Ideas` and `## Parked / deferred work` sections.
- `golangci-lint` / `govulncheck` in `make check` and CI — its own register entry.
- Any version identifier (see the closing note).
- Removing the closed entries from `ISSUES.md` before their item lands: each item deletes its own entry.

**Regression check (2026-09-01, af9341d2):** five independent reviewers read items 1–21 against the
tree; items 2, 13, 14, 15, 16 and 19 came back SAFE. The **Base commit** is corrected `02282e09` →
`af9341d2`, the commit that archives the predecessor plan, so item 1's first acceptance check passes at
the declared base.
- 1: guard folded — the gate's acceptance reads "nothing but the plan file itself".
- 3: guard folded — the `RecordChoice` contract in `internal/tui/tui.go` moves with the behaviour.
- 4: recast — both forms of the verb take `auto`, and the row records a `cleared` clause of its own.
- 5: guard folded — `cmd/apogee/doc.go`'s file map names the flag half's headless notice.
- 6: guard folded — the entry rides `opts.Servers`, and `probeKeyStore` becomes a swappable var.
- 7: guard folded — the sweep grep is scoped to `docs/adr/ internal/` with its expected hits named.
- 8: guard folded — the project gate is restored before the shipped apply; the drafted assertions fail as written.
- 9: guard folded — the walk gets its own movement predicate and collector; `windowLow` cannot serve it.
- 10: guard folded — `apogee_test.go` joins **Files:**; the assertion goes through the public surface.
- 11: guard folded — the flash costs the context gauge, not the `esc back` hint; `runview_test.go` joins
  **Files:** and `refuseChildMessage`'s doc comment is amended.
- 12: guard folded — the helper takes `testing.TB`; the chrome offset is named for the idle one-row frame.
- 17: recast — each Root is judged once before the first clear and its verdict persisted (`Entry.Judged`'s
  pattern); the volume-root refusal is spelled separator-agnostically inside `winlabel`.
- 18: recast — the member view build stays conditional on `m.head.expanded`; only the unspanned `hides` changes.
- 20: guard folded — the refusal keeps naming the server; the wrapped wording is pinned.
- 21: guard folded — `startedSince` gets a pure seam; the acceptance pattern widens.
- 22: added at the check — the owner's ratified manual item for the auto row and the headless notice.

A sixth reviewer re-read items 4, 17, 18 and 22 against the same tree; 22 came back SAFE and changed only
through the owner's call below.
- 4: guard folded (round 2) — `layout.md:1813-1815`'s two-column, "no third cell" pane contract and
  `:1820`'s note clause are superseded by the auto row and are amended in item 22, which owns every
  cross-cutting doc amendment.
- 17: guard folded (round 2) — the root verdict is set in memory, so a root-only journal whose pre-clear
  rewrite fails still clears as it does today; the persisted-verdict assertion moves to `revertibleRoots`
  with a `_windows_test.go` half, since `judgePriors` is Windows-tagged and this item's acceptance runs on
  Linux.
- 18: recast a second time — the new `hides` clause is gated on `subAgentReported(m.head)` so a RUNNING
  member keeps its bare row, `renderSubAgentMemberRows` re-paints the collapsed row itself, and the item
  ADDS a grouped 110-column case rather than citing a pin that does not exist.
- 22: `layout.md` joins its **Files:**, **What** and Acceptance — the pane's row and note contract must
  match the landed code — and it now depends on item 18 as well as 4 and 5.

---

## 1. The predecessor plan is archived and its work is in the tree — ✅ DONE (2026-09-01)

NOTES (2026-09-01): gate passed. `docs/plans/archived/2026-08-31 - 06 - shipped-skill-gaps-plan.md` exists; `git status --porcelain` printed exactly one line, `?? "docs/plans/2026-09-01 - 00 - residual-defects-sweep-plan.md"` (this plan document, the declared exception); `exportShippedHintLines` is present in `internal/tui/skills.go` at :102, :124, :437, :450.

NOTES (2026-09-01): the other two artefacts the item's **What** names are also in the tree — `cmd/apogee/testdata/frames/t12-skills.txt` (the re-recorded golden) and `hostileHome` in `cmd/apogee/e2e_hostile_test.go` (3 occurrences).

**What:** Item 9 is written against the tree plan `2026-08-31 - 06 - shipped-skill-gaps-plan.md`
leaves behind: the `/skills` export hint (`exportShippedHintLines`, `internal/tui/skills.go`), the
re-recorded `t12-skills` golden, and `hostileHome` in `cmd/apogee/e2e_hostile_test.go`. Verify that
plan is archived and the working tree is clean before any other item runs. If either check fails,
STOP and report — never implement around it.

**Regression guard.** The acceptance reads `git status --porcelain` prints nothing but the plan file
itself: this plan document is untracked and unignored (`.gitignore` covers only `/docs/skill-runs/`), so
a correct run prints `?? "docs/plans/2026-09-01 …"` — the execute preflight's own exception (execute.md
phase 0 step 6). A bare "prints nothing" would STOP the run this gate exists to open.

**Files:** none — a gate.
**Tests:** none.
**Acceptance:**
- `ls "docs/plans/archived/2026-08-31 - 06 - shipped-skill-gaps-plan.md"`
- `git status --porcelain` prints nothing but the plan file itself
- `grep -n 'exportShippedHintLines' internal/tui/skills.go`
**Commit:** none — a gate commits nothing.

## 2. `Retarget` and `relist` push the cleared target under the lock — ✅ DONE (2026-09-01)

NOTES (2026-09-01): the two ordering tests reuse the item's named fixture (`retargetableWiring`, `gatedDelegationSpy`) through one new helper, `racedClearAgainstALanding`, instead of being written into the body of `TestDelegationRetargetMidRunDropsTheOldServersLanding` — the `relist` half is not that test's path, and both halves need the same choreography. Both fail against the pre-item tree (`pushes = [target, nil]`) and pass after the fix.

NOTES (2026-09-01): `ISSUES.md` joins **Files:** — the plan's Closeout has every item remove its own register entry in its own commit, and the item's own **Files:** list is code-only. The section's `**Status:**` block stays: three of its four entries are still open.

**What:** Fixes the residual deferred out of the 2026-08-31 sub-agents-server routing closeout
(`ISSUES.md:44`). `Retarget` (`cmd/apogee/delegation.go:609-637`) swaps the server and bumps the
generation under `d.mu`, unlocks at `:634`, then calls `d.engine.SetDelegationTarget(nil)` at
`:636`; `relist` repeats it at `:566-570`. A new-generation beat landing in that window is clobbered
by the nil. Move BOTH pushes inside the existing critical section, exactly as `land` already does
(`:401`) — its doc at `:378-388` names this bug class and justifies holding the mutex across the
push: the seam takes one short lock of its own and calls nothing back into the holder
(`cmd/apogee/wire_engine.go:381`, `internal/agent/delegationtarget.go:162`). The notice `land`
deliberately sends AFTER unlocking (`:386-388`) stays where it is. No new generation mechanism: the
file has one.

**Files:** `cmd/apogee/delegation.go`, `cmd/apogee/delegation_test.go`
**Tests:** one ordering test per path, extending `TestDelegationRetargetMidRunDropsTheOldServersLanding`
(`delegation_test.go:1090`) rather than duplicating its fixture: the `delegationSetter` spy blocks on
entry until the test has started a competing `land` of the NEW generation on another goroutine and
given it a chance to run, and the assertion is on the ORDER of pushes — the last push is the new
target, never the nil. It must fail against the pre-item tree, where the free mutex lets `land` land
first.
**Acceptance:** `go test -race -count=1 -run 'TestDelegation' ./cmd/apogee/`
**Commit:** `fix(delegation): push the cleared delegation target under the wiring mutex`

## 3. The composition root can clear the `sub-agents-server:` key — ✅ DONE (2026-09-01)

NOTES (2026-09-01): `ResetSubAgentsServer` sits below `saveScalar` and directly above
`ResetConfigSetting` rather than immediately after `SaveSubAgentsServer`, so `saveScalar`'s "the write
both of those share" still follows the two writes it names and the two resets stay adjacent.

**What:** Groundwork for item 4's ratified opt-out, split from it because it crosses two packages.
`recordSubAgentsServerChoice` (`cmd/apogee/wire_verbs.go:188`) can only WRITE a name, and
`config.ResetConfigSetting` refuses this key: `writableKey` admits Editable keys only, and
`sub-agents-server` is deliberately not Editable (`SaveSubAgentsServer`'s doc,
`internal/config/configwrite_scalar.go:60-71`). Add `config.ResetSubAgentsServer(path string) error`
beside `SaveSubAgentsServer` — `LookupKey(subAgentsServerPath)`, then `scalarResetEdit` + `edit`, the
same shape `ResetConfigSetting` uses past its admission test — and give it the same one-line doc
reason its sibling carries. Then let `recordSubAgentsServerChoice("")` call it and answer
`(true, nil)`: an empty name skips the `configuredServer` guard at `:189`, and a key the file does
not set is already at its default — a no-op, not an error, per `ResetConfigSetting`'s own rule.

**Regression guard.** The seam contract must move with the behaviour: `DelegationHost.RecordChoice`'s
doc (`internal/tui/tui.go:605-613`) states "a name no `servers:` entry holds is skipped silently (false,
no error)", and `""` holds none. Amend it — and `delegationHost.RecordChoice`'s doc
(`cmd/apogee/delegation.go:70-72`) — to name the empty name's clear-and-report-written case.

**Files:** `internal/config/configwrite_scalar.go`, `internal/config/configwrite_scalar_test.go`, `cmd/apogee/wire_verbs.go`, `cmd/apogee/wire_verbs_test.go`, `internal/tui/tui.go`, `cmd/apogee/delegation.go`
**Tests:** `internal/config`: the key line is removed from a real fixture file, comments and sibling
entries survive the splice, and a file that never set the key is unchanged with no error. `cmd/apogee`:
a new `wire_verbs_test.go` drives `recordSubAgentsServerChoice("")` against a temp config home and
asserts the file no longer carries the key and the call reports it wrote.
**Acceptance:** `go test -race -count=1 -run 'SubAgentsServer' ./internal/config/ ./cmd/apogee/`
**Commit:** `feat(config): clear the sub-agents-server key from the config file`

## 4. The `/sub-agents-server` picker offers the `auto` row — ✅ DONE (2026-09-01)

NOTES (2026-09-01): `ISSUES.md` joins **Files:** — the item's own **Files:** is code-only, while the plan's Closeout has every item remove its own register entry in its own commit. The section's `**Status:**` block stays; two of its entries are still open.
NOTES (2026-09-01): the ` · sub-agents-server: cleared` clause is conditional on the host reporting a write, exactly as ` · sub-agents-server: saved` is — a clause states a write that happened. The composition root reports written for the empty name (`recordSubAgentsServerChoice`), so the full note the item specifies is what a real session paints; a host that records nothing claims no clause, and a subtest pins that.

**What:** Recast at the regression check (2026-09-01). Fixes the residual at `ISSUES.md:49` — the
ratified empty-key opt-out (`Retarget("")`, `cmd/apogee/delegation.go:609-611`) is reachable only by hand-editing `config.yaml`. Depends on item 3.
Follow the effort picker exactly (`internal/tui/effort.go:100-107`, `:120-131`): `subAgentsServerRows`
(`internal/tui/picker.go:502-511`) appends, LAST, `popupRow{"auto", "— no routing; delegations run on
this session's own server"}`, and `acceptPicker`'s `pickerSubAgentsServer` case (`:860-866`) maps an
index past the targets to the empty name instead of no-opping — the index mapping is the effort
picker's ("absence, not a level"). `recordSubAgentsChoice` (`:559`) drops its `name == ""` early
return so the empty name reaches `RecordChoice` and clears the key. The note reads
`sub-agents server: auto · this session's own server` plus the existing `subAgentsSavedClause`
(`:452`) when the write happened. The deliberate absence of a `· current` cell (`:497-501`) stands —
the auto row is an action, not a marker.

**Regression guard.** Both forms of the verb take `auto`, spelled by ONE shared constant. The argument
form resolves through `serverNamed` (`internal/tui/picker.go:472-476`), so a picker offering an `auto`
row while `/sub-agents-server auto` answers `unknown server "auto"` would break the documented invariant
that the two forms can never disagree about what exists (`picker.go:459-462`, restated at
`picker_test.go:2696-2697`) — `/effort` is a safe model for a synthetic row only because it takes no
argument. Resolve a CONFIGURED entry named `auto` FIRST so a real entry keeps winning; only when no
entry matches does the label mean the opt-out and call `retargetSubAgents("")`. The argument form gets
its own test. Second, the auto row must NOT append `subAgentsSavedClause`: accepting it DELETES the key,
while that const's doc says the clause means the key now names the entry (`picker.go:447-450`). Add a
sibling const ` · sub-agents-server: cleared`, used by the auto row only, and amend
`subAgentsSavedClause`'s doc to point at it (owner, 2026-09-01, this session). The auto row's full note
is `sub-agents server: auto · this session's own server · sub-agents-server: cleared`. Tests must cover
both forms and both clauses. Third, this row supersedes the pane contract recorded in `layout.md`
(the repo's TUI layout spec): `:1813-1815` fixes the rows as "those same entries in two columns only"
with "deliberately no third cell", and `:1820` fixes the transcript note as one line naming the entry
with `· sub-agents-server: saved` after it — a synthetic `auto` row that is no entry, and a `cleared`
sibling clause, are neither. `layout.md` is amended by ITEM 22, which owns every cross-cutting doc
amendment in this plan (owner, 2026-09-01), so this item's **Files:** stays code-only and must not land
with the spec left stale.

**Files:** `internal/tui/picker.go`, `internal/tui/picker_test.go`
**Tests:** the row is present, last, and two-celled — `TestSubAgentsServerPickerListsTheConfiguredTargets`
(`picker_test.go:2611`) asserts `len(rows) == len(twoServers)` today and must move to `+1`; accepting
it calls `Retarget("")` and `RecordChoice("")` on the fake host and paints
`sub-agents server: auto · this session's own server · sub-agents-server: cleared`; accepting a real
target is unchanged (`:2654`) and still carries `subAgentsSavedClause`; an out-of-range index still
no-ops. And the argument form beside `TestSubAgentsServerArgumentFormSkipsThePicker`
(`picker_test.go:2698`): `/sub-agents-server auto` takes the same path and paints the same note, while a
CONFIGURED entry literally named `auto` still resolves to itself in BOTH forms.
**Acceptance:** `go test -race -count=1 -run 'TestSubAgentsServer' ./internal/tui/`
**Commit:** `fix(tui): offer the auto row in the sub-agents-server picker`

## 5. A headless run names a retired `sub-agents: true` flag — ✅ DONE (2026-09-01)

NOTES (2026-09-01): `ISSUES.md` joins **Files:** — the item's own **Files:** is code-only, while the plan's Closeout has every item remove its own register entry in its own commit. The section's `**Status:**` block stays: its "both-offers" entry (item 6) is still open.
NOTES (2026-09-01): the headless emission reads the file path through `filepath.Join(roots.config, "config.yaml")` and drops a scan error silently (`err == nil && len(names) > 0`), matching `prepareSubAgentsMigration`'s own "a file the scan stumbles on is silent rather than fatal" rule — a start-up question is never a reason a run fails.
NOTES (2026-09-01): the wording is pinned whole by `TestSubAgentsFlagNoticeNamesTheEntriesAndTheReplacement` (string equality against the ratified sentence); the headless drive then asserts stderr CONTAINS `subAgentsFlagNotice(path, []string{"cheaper"})`, so it pins emission, the resolved path and the entry name without restating the sentence in a second place where the two could drift apart.
NOTES (2026-09-01): the headless drive is placed immediately above the file's `The start-up sub-agents-flag migration (ADR 0045)` divider, beside `TestHeadlessNoticesPlaintextKeysAndNeverPrompts` it is modelled on, so the two headless notice tests sit together; the string test is beside `TestPlaintextKeyNoticeNamesTheEntriesAndTheAlternatives` exactly as the item says.
NOTES (2026-09-01): failure against the pre-item tree confirmed — with the emission removed, `TestHeadlessNoticesTheRetiredSubAgentsFlagAndNeverPrompts` fails with `stderr = "turns: 1 · denied: 0\n"`.
NOTES (2026-09-01): `docs/manual/` is deliberately untouched — item 22 owns this notice's manual amendment.

**What:** Fixes the residual at `ISSUES.md:54`. Detection and offer are interactive-only
(`cmd/apogee/keymigrate.go:113`, `cmd/apogee/wire.go:131`, `cmd/apogee/wire_options.go:192`), so a
headless session whose config still carries the flag says nothing — where the plaintext-key
precedent prints a stderr notice (`cmd/apogee/headless.go:277-278`). Add
`subAgentsFlagNotice(path string, names []string) string` beside `plaintextKeyNotice`
(`keymigrate.go:206`), and emit it in `runHeadless` right after `:278` via `cmd.PrintErrln`, gated on
`config.RetiredSubAgentsEntries(filepath.Join(roots.config, "config.yaml"))` — headless never builds
a `rootWiring`, so `prepareSubAgentsMigration` is unreachable there. Ratified wording, verbatim, with
`%s` for the comma-joined entry names and the resolved path:

> `apogee: %s still carries the retired sub-agents: true flag in %s, and headless runs never prompt, so apogee cannot offer to migrate it. The flag no longer routes anything — set sub-agents-server: <entry> at the root of the file and drop the flag.`

It is routing-agnostic on purpose: the flag can be stale while the root key IS set, and a notice
claiming where delegations run would then be wrong.

**Regression guard.** `cmd/apogee/doc.go:85-92` is the package file map, and its keymigrate.go entry
gives the FLAG half only "the raw-YAML detection, and the rewrite-then-retarget" while the KEY half
already names its notice. Extend that entry so the flag half names its headless notice beside the offer;
`cmd/apogee/doc.go` joins **Files:**.

**Files:** `cmd/apogee/keymigrate.go`, `cmd/apogee/headless.go`, `cmd/apogee/keymigrate_test.go`, `cmd/apogee/doc.go`
**Tests:** the string test beside `TestPlaintextKeyNoticeNamesTheEntriesAndTheAlternatives`
(`keymigrate_test.go:121`), and a headless drive modelled on
`TestHeadlessNoticesPlaintextKeysAndNeverPrompts` (`:366`) using the `subAgentsFlagYAML` fixture
(`:415`): `cmd.SetErr`, assert stderr carries the entry name and the exact sentence above, stdout is
clean, and the config file is not edited.
**Acceptance:** `go test -race -count=1 -run 'TestHeadless|TestSubAgentsFlagNotice' ./cmd/apogee/`
**Commit:** `fix(headless): name a retired sub-agents flag on stderr`

## 6. The both-offers pairing is carried through the composition root — ✅ DONE (2026-09-01)

NOTES (2026-09-01): consequential edit — ISSUES.md: made necessary by this item's fix — the closed
entry is deleted, and with it the now-empty "Residuals deferred out of the 2026-08-31
sub-agents-server routing plan" section heading and Status block, whose last entry this was.
NOTES (2026-09-01): the new test is deliberately NOT `t.Parallel()` — it swaps the package-level
`probeKeyStore`, which the three parallel `TestRunRoot*` tests read; Go runs sequential top-level
tests to completion before resuming parallel ones, so the swap cannot race. Verified with
`go test -race -count=1 ./cmd/apogee/` (whole package, clean).
NOTES (2026-09-01): the must-fail direction was proved both ways — unwiring `prepareKeyMigration`
fails the KeyMigration/MigrateKey assertions, unwiring `prepareSubAgentsMigration` fails the
SubAgentsMigration/MigrateSubAgentsServer ones; `wire.go` was restored afterwards.

**What:** Closes the residual at `ISSUES.md:62`. `TestSubAgentsMigrationGivesWayToTheKeyMigration`
(`internal/tui/keymigration_test.go:370`) hands the renderer hand-built Options, so no test carries a
real config file with `api-key:` plus the retired flag through the composition root. Add one
`runRoot` test in `cmd/apogee` following the house pattern
(`TestRunRootWiresTheConfigWatch`, `cmd/apogee/configwatch_apply_test.go:148`, with
`recordingLauncher` from `root_test.go:22`): a `testConfigHome` whose YAML carries an `api-key:`
entry (the `onePlaintextServer` shape, `keymigrate_test.go:91`) AND a second entry with
`sub-agents: true` (`subAgentsFlagYAML`, `:415`), then assert `rec.opts.KeyMigration`,
`rec.opts.SubAgentsMigration` and `rec.opts.MigrateSubAgentsServer` all arrive populated from that
one file. It pins the file-shaped pairing only; which offer wins stays the renderer's test.

**Regression guard.** `runRoot` takes `config.Options` as a parameter and never re-reads the file, so
the plaintext entry must ride `opts.Servers` as well as the fixture (`prepareKeyMigration`,
`keymigrate.go:79`) — the file half of the pairing is only the retired flag (`:114`). And `wire.go:127`
hard-wires `probeKeyStore`, whose `keystore.Probe` answers `ErrNoStore` on any store-less machine
(`internal/keystore/keystore.go:114-127`): make it a package-level var the test swaps for a fake store,
or the assertion is green on macOS and red on every Linux CI run.

**Files:** `cmd/apogee/keymigrate_test.go`, `cmd/apogee/keymigrate.go`, `cmd/apogee/wire.go`
**Tests:** the one test above. It must fail against a tree where either preparer is unwired from
`runRoot`.
**Acceptance:** `go test -race -count=1 -run 'TestRunRoot' ./cmd/apogee/`
**Commit:** `test(cmd): carry both migration offers through the composition root`

## 7. ADR 0023 §1 counts the fourth system-prompt key — ✅ DONE (2026-09-01)

NOTES (2026-09-01): `ISSUES.md` joins **Files:** — the item's own **Files:** names only the ADR, while the plan's Closeout has every item remove its own register entry in its own commit. The section heading and its `**Status:**` block stay: two of its three entries (items 8 and 9) are still open.
NOTES (2026-09-01): retry cleanup — a previous attempt at this item had also deleted item 8's register entry ("The skill-gate applier test pins only one direction of the mirror"); per the dispatch DECISION it is restored verbatim in its original position, so the tree carries item 7's removal alone. `cmd/apogee/wire_settings_test.go` is item 8's dirty file and was left exactly as found.
NOTES (2026-09-01): the parenthetical's wording was tightened over the previous attempt's — `use-default-prompt:` gates the embedded-default rung of ADR 0064 §2's ladder (it is not itself the bottom rung; rung four is "nothing"). No blockquote header, no status change, and the count is struck through rather than rewritten, so the sweep grep still reports `0023:40`.
NOTES (2026-09-01): sweep confirmed — `grep -rn 'three top-level' docs/adr/ internal/` reports exactly the two expected hits; `internal/config/config.go:1221` counts the trio against a `system-prompt:` block spelling and is correct as written, so it is left untouched.

**What:** Closes the residual at `ISSUES.md:73`. The sentence at
`docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md:40` — "The
configuration surface is file-only: three top-level keys" — is stale now that `use-default-prompt:`
exists (`internal/config/config.go:1105`, ADR 0064 §2). ADR 0064 supersedes §8 "and nothing else"
(`docs/adr/0064-…:36-38`), so §1 is drifted by an amendment the same file already carries
(`0023:310`), not superseded by a new decision. Amend §1 in place in the house clause-level style —
the inline `*(Superseded YYYY-MM-DD by [ADR NNNN](…): …)*` parenthetical used at `0026-…:208`,
`0044-…:74`, `0028-…:235` — so the count names the fourth key and points at ADR 0064 §2 and the
existing 2026-08-31 amendment. No new blockquote header, no status change.
**Rule for the sweep** (not a closed list): every sentence that counts the system-prompt
CONFIGURATION SURFACE is amended; a sentence counting the trio's spelling is not. Find them with
`grep -rn 'three top-level' docs/adr/ internal/` — `internal/config/config.go:1221` ("They are three
top-level keys rather than one `system-prompt:` block") counts the trio against a block spelling and
is correct as written: leave it.

**Regression guard.** The sweep grep is scoped to `docs/adr/ internal/`: over `docs/` it also hits
`docs/plans/archived/2026-07-26 - 02 - configurable-system-prompt-plan.md:533` and this plan document
itself, so the acceptance fails on a correct run. Its expected hits are `internal/config/config.go:1221`,
plus `0023:40` itself when the count is struck through in the house `~~…~~ *(Superseded …)*` style rather
than rewritten.

**Files:** `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`
**Tests:** none — prose.
**Acceptance:** `grep -n 'use-default-prompt' "docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md"`; `grep -rn 'three top-level' docs/adr/ internal/` shows only `internal/config/config.go:1221`, plus `0023:40` where the count is struck through rather than rewritten.
**Commit:** `docs(adr): ADR 0023 §1 counts the fourth system-prompt key`

## 8. The skill-gate applier test pins both directions of the mirror — ✅ DONE (2026-09-01)

NOTES (2026-09-01): `ISSUES.md` joins **Files:** — the item's own **Files:** is code-only, while the plan's Closeout has every item remove its own register entry in its own commit. The section's `**Status:**` block stays: its `/skills` golden entry (item 9) is still open.
NOTES (2026-09-01): retry — the test half of this item survived in the working tree from the first attempt; the `ISSUES.md` entry, which item 7's own retry restored, is removed again here. Nothing else was carried over.
NOTES (2026-09-01): mutation-checked as **Tests** requires — with `applySkillSourceGate` (`cmd/apogee/wire_settings.go`) edited to build `skills.Sources{}` instead of reading the sibling back off the Provider, both added assertions fail (`wire_settings_test.go:1383`, `:1386`); the edit was reverted and the file is untouched.

**What:** Closes the residual at `ISSUES.md:80`. `TestApplySettingSkillGatesLeaveEachOtherAlone`
(`cmd/apogee/wire_settings_test.go:1338`) asserts `UseShippedSkills` survives an
`apply("use-project-skills", …)` (`:1359-1366`), but after `apply("use-shipped-skills","false")`
(`:1368`) it asserts only the shipped effect — the symmetric zeroing it exists to forbid would pass,
and the third apply at `:1373` overwrites the clobbered value before it is ever read. Insert the
mirror assertions between `:1370` and `:1373`: `provider.Sources().UseProjectSkills` still true, and
`provider.Get("project-only")` still resolves — the fixture already writes that skill at
`:1348-1349` and `:1376` already proves it is the right probe for the gate.

**Regression guard.** Both drafted assertions fail as written: `apply("use-project-skills","false")`
(`wire_settings_test.go:1359`) has already turned the project gate off, so at the insert point
`Sources().UseProjectSkills` is false and `Get("project-only")` does not resolve. Restore the gate —
`apply("use-project-skills","true")` immediately BEFORE the shipped apply at `:1369` — then assert the
mirror after it; the item's `:1368`/`:1370`/`:1373` locators are the surrounding `if` bodies, not the applies.

**Files:** `cmd/apogee/wire_settings_test.go`
**Tests:** the two added assertions. They must fail against a tree where `applySkillSourceGate`
(`cmd/apogee/wire_settings.go:1302`) is edited to zero the sibling field.
**Acceptance:** `go test -race -count=1 -run 'TestApplySettingSkillGates' ./cmd/apogee/`
**Commit:** `test(cmd): pin both directions of the skill-gate mirror`

## 9. The `· N skills available:` header returns to end-to-end coverage

**What:** Closes the residual at `ISSUES.md:86`, with its premise corrected: the header did not fall
off in the re-record — it scrolled off the 30-row viewport at `4d1f18c3`, when the shipped set grew
from two skills to four, and the export hint pushes it three rows further. So a golden of the
visible frame cannot hold it. In `TestE2EHostileSurfacesKeepTheirOwnRows`
(`cmd/apogee/e2e_hostile_test.go:110-124`), after `tuitest.Golden` at `:123`, page the transcript up
with the bounded-scroll pattern this package already uses (`waitForScroll` / `scrollbackNumbers`,
`cmd/apogee/e2e_stream_test.go:631-649`; `tuitest.PgUp`, `internal/tuitest/keys.go:33`) and assert
the collected rows contain the EXACT header the run emits — the literal `N skills available:` with
the count the fixture actually yields, read off the run at implementation time, not a computed
number and not the bare phrase. The string is produced at `internal/tui/skills.go:298`; the `· ` lead
is the note renderer's (`internal/tui/render.go:848`). Depends on item 1.

**Regression guard.** `waitForScroll` / `scrollbackNumbers` cannot carry this walk: both measure through
`windowLow` (`cmd/apogee/e2e_stream_test.go:683`), which answers `math.MaxInt` on any frame without the
stream fixture's numbered rows — so the first press reports "did not move" and the collector returns an
empty map. Give the walk its own movement predicate (the frame's rows changed, or the header found) and
its own row collector; the header this fixture yields at `af9341d2` is the literal `· 5 skills
available:`, reached after one `PgUp`.

**Files:** `cmd/apogee/e2e_hostile_test.go`
**Tests:** the scroll-and-assert above, in the existing test. It must fail against the pre-item tree,
where nothing outside `internal/tui` sees the header at all.
**Acceptance:** `go test -race -count=1 -run 'TestE2EHostileSurfacesKeepTheirOwnRows' ./cmd/apogee/`
**Commit:** `test(cmd): assert the skills-available header off the scrolled transcript`

## 10. `ErrNoSuchChild` is re-exported for embedders

**What:** Closes the residual at `ISSUES.md:94`. Every other Agent-surface sentinel is aliased in the
root package (`apogee.go:580-586`), but `InterjectChild`'s refusal is only `domain.ErrNoSuchChild`
(`internal/domain/errors.go:67`), and `internal/` is unimportable from outside the module (ADR 0010),
so an embedder cannot `errors.Is` it — while `cmd/apogee/wire_engine.go:220-223` already names
`apogee.ErrNoSuchChild` in prose. Add the alias to the same `var` block, doc-commented in the house
form the two neighbours use ("returned by …; match with errors.Is"), and add it to the compile-surface
block in `example_test.go:184-194` that pins every root sentinel.

**Regression guard.** `apogee_test.go` joins **Files:** — **Tests** puts the `errors.Is` assertion there
and the list named only `apogee.go` / `example_test.go`. That file is `package apogee_test` and no root
test imports `internal/domain`, so the assertion may be written through the public surface
(`Agent.InterjectChild` on an unknown id — `Agent` is the alias at `apogee.go:54`) rather than by
importing `internal/domain`.

**Files:** `apogee.go`, `example_test.go`, `apogee_test.go`
**Tests:** the `example_test.go` compile-surface entry, plus an `errors.Is` assertion in
`apogee_test.go` proving the alias is the same sentinel `domain` returns.
**Acceptance:** `go test -race -count=1 ./ ` and `go build ./...`
**Commit:** `fix(api): re-export ErrNoSuchChild for embedders`

## 11. A refusal raised inside a run view flashes on the status line

**What:** Closes the residual at `ISSUES.md:101`. `childGoneNote` (`internal/tui/interject.go:226`)
and the not-running note (`:232`) are committed by `refuseChildMessage` (`:311-321`) as host notes at
depth 0, so a reader inside a delegate's view sees nothing until they back out. Ratified: the note
STAYS at depth 0 — design call 4 of `docs/plans/archived/2026-08-18 - 00 - open-defects-plan.md:25-27`
is unchanged and `refuseChildMessage`'s doc comment keeps saying so — and `refuseChildMessage`
additionally sets `m.flash` to the same sentence and batches the `flashClearMsg` tick, the existing
frame-level ephemeral slot (`internal/tui/model.go:411`, `internal/tui/mouse.go:138-142`, consumed by
`statusRight`, `model.go:3416`), which is painted in every view. Set the flash only when
`m.inRunView()` (`runview.go:44`): at depth 0 the note is already visible and a flash would displace
the context gauge for nothing. Accept that inside a view the flash takes the CONTEXT GAUGE's slot for
its two seconds — `statusRight` reads `m.flash` (`model.go:3426`) before `contextGauge()` (`:3430`), and
the `esc back` hint (`:3438`) already yields to the gauge — `statusRight`'s existing priority order,
unchanged.

**Regression guard.** `internal/tui/runview_test.go` joins **Files:** — **Tests** extends
`TestRunViewEnterRefusesANonRunningChild` there (`:607`) while the list named only `interject.go` and
`interject_test.go`. And `refuseChildMessage`'s doc comment (`internal/tui/interject.go:311-313`) — "the
note, and NOTHING else moved" — is superseded by this item's ratified flash and is amended in the same edit.

**Files:** `internal/tui/interject.go`, `internal/tui/interject_test.go`, `internal/tui/runview_test.go`
**Tests:** in a run view, a `⏎` on a finished child and on a not-started child each set `m.flash` to
the note's exact sentence and return a command that clears it; at depth 0 neither sets it; the depth-0
note lands in both cases (the existing `noteInTranscript` helper, `runview_test.go:535`). Extend
`TestRunViewChildGoneKeepsTheDraft` (`interject_test.go:1758`) and
`TestRunViewEnterRefusesANonRunningChild` (`runview_test.go:607`) rather than restating their fixtures.
**Acceptance:** `go test -race -count=1 -run 'TestRunView|TestInterject' ./internal/tui/`
**Commit:** `fix(tui): flash a child-message refusal inside the run view`

## 12. `assertLastBodyRow` anchors on the chrome, not on the first `▔`

**What:** Closes the residual at `ISSUES.md:110`. The helper
(`cmd/apogee/e2e_subagent_view_test.go:336-357`) scans `f.Rows()` for the FIRST row starting with `▔`
and treats it as the transcript's end, so a session title or a body row beginning with that glyph, or
a layout change that moves the rule, silently retargets every assertion instead of failing it. The
frame's chrome is fixed below the rule (rule, status line, prompt box, footer, bottom rule —
`internal/tui/model.go:1961-1982`, `:2576`), so: scan from the BOTTOM, take the last `▔`-prefixed row,
and `t.Fatalf` unless it sits at the expected offset from `f.Height()` — the offset declared as a named
constant in the test file with a comment pointing at `layout.md`, since `internal/tui`'s height
constants are unexported and `internal/tuitest` exposes no region API. A frame with no such row, or
one at an unexpected offset, fails loudly.

**Regression guard.** The negative case is not writable as drafted: `assertLastBodyRow`
(`cmd/apogee/e2e_subagent_view_test.go:335`) reaches its failure through `t.Fatalf`, which Goexits, and
`cmd/apogee` has no fake `testing.TB` anywhere. Sign the helper `testing.TB`, return explicitly after
each `t.Fatalf`, and assert against a small recording TB in the same file — or extract the scan as a pure
`func(rows []string, h int) (int, bool)`. And the chrome offset is NOT fixed: `stackInputSlot`
(`internal/tui/model.go:2581`) seats the dropdown and the staged-interjection strip between the status
line and the box, and the box grows to `maxInputRows` = 10 (`:1975`) — so the constant is named for the
IDLE one-row frame (rule at `Height()-7`, per `cmd/apogee/testdata/frames/t17-run-view.txt:24`) and its
comment says so, lest a later call site with a staged band or a grown box read as a layout regression.

**Files:** `cmd/apogee/e2e_subagent_view_test.go`
**Tests:** the helper's own guard, driven against a small recording `testing.TB` in the same file (or
against the extracted pure scan) — a synthetic `tuitest.Frame` whose body carries a row starting with
`▔` resolves to the real rule, and one whose rule row is missing fails rather than returning a body row.
**Acceptance:** `go test -race -count=1 -run 'TestE2ESubAgentView' ./cmd/apogee/`
**Commit:** `test(cmd): anchor assertLastBodyRow on the rule above the status line`

## 13. The steering cell gets a real-engine assertion

**What:** Closes the residual at `ISSUES.md:116`. `TestE2ESubAgentView`
(`cmd/apogee/e2e_subagent_view_test.go:52`, assertions at `:176-178`) pins the steered run's collapsed
row with `countedOutcome = "tool calls · done"`, a `Contains` that passes with or without the steering
cell — so nothing outside `internal/tui` proves `· steered by 1 message` reaches the painted row.
Assert the exact composed slot instead: `"tool calls · done · steered by 1 message"`, the spelling
`delegationSteeredCell` produces (`internal/tui/toolregistry.go:764`, lead
`delegationSteeredLead = "steered by "`, `plural(n, "message")`, joined to the verdict by
`slotSeparator`). Keep `countedOutcome` for the runs that are not steered. No new golden — the step-cap
half's golden (`t04-step-cap-block`, `cmd/apogee/e2e_delegation_test.go:236`) already pins that
geometry, and a second one re-pins a churning surface.

**Files:** `cmd/apogee/e2e_subagent_view_test.go`
**Tests:** the changed assertion. It must fail against a tree where `delegationSteeredCell` is not
composed into the slot.
**Acceptance:** `go test -race -count=1 -run 'TestE2ESubAgentView' ./cmd/apogee/`
**Commit:** `test(cmd): assert the steered cell on the collapsed run row`

## 14. `openRunAt`'s self-redirect guard is pinned

**What:** Closes the residual at `ISSUES.md:122`. Deleting `m.viewedRun() == ref`
(`internal/tui/runview.go:170`) leaves both suites green: `TestRunViewOwnHeadDoesNotReopenItself`
(`internal/tui/runview_test.go:117-162`) now clicks a `targetTask` row, which takes
`toggleBlockAt`'s task branch (`internal/tui/mouse.go:673-680`) and never calls `openRunAt`. Add a
direct-call unit test beside it: build a framed run, enter it with `enterOnLastBlock`
(`runview_test.go:34`), then call `m.openRunAt(idx)` with the viewed run's own head index and assert
it refuses (`opened == false`) and the view stack is unchanged. Direct call is the honest pin — inside
a rooted paint the head's rows are `targetBreadcrumb` and `targetTask`, so no click can reach the
guard, and the existing test's comment already says the row's kind is what settles it.

**Files:** `internal/tui/runview_test.go`
**Tests:** the test above. It must fail with the guard deleted.
**Acceptance:** `go test -race -count=1 -run 'TestRunView' ./internal/tui/`
**Commit:** `test(tui): pin openRunAt's self-redirect guard`

## 15. A sibling suggestion carrying a row break is driven end to end

**What:** Closes the residual at `ISSUES.md:131`. `suggestSiblings` escapes each rendered suggestion
with `escapeRowBreaks` (`internal/tools/path_suggest.go:75`; the escaper is
`internal/tools/tools.go:71-79`) and `notFoundMessage` assembles them (`:91`), but no test drives a
sibling whose NAME carries a newline, so neither helper is proven on the path a hostile filename
takes. Grow `seedSuggestTree` (`internal/tools/path_suggest_test.go:13`) with a NEW subdirectory
holding a file whose name carries `\n` — a new dir so the existing cap-at-five and sort expectations
at `:72-83` do not shift — and add one `TestSuggestSiblings` case asserting the returned suggestion is
the ESCAPED spelling. Guard it the way the repo already does for such fixtures: a
`runtime.GOOS == "windows"` skip inside the new case only (`cmd/apogee/e2e_hostile_test.go:336-338`),
never a build tag, since `make check`'s Windows vet covers only `internal/platform` and
`internal/probe`.

**Files:** `internal/tools/path_suggest_test.go`
**Tests:** the added fixture entry and case; plus one `notFoundMessage` case built from that
suggestion, so the assembled sentence is proven to carry no raw break.
**Acceptance:** `go test -race -count=1 -run 'TestSuggestSiblings|TestNotFoundMessage' ./internal/tools/`
**Commit:** `test(tools): drive a sibling suggestion whose name carries a row break`

## 16. `contentArgs` is cross-checked against the tools' own schemas

**What:** Closes the residual at `ISSUES.md:137`. The write/edit content keys the wire form drops are
spelled a second time at `internal/tui/wireargs.go:25-30` and matched by tool NAME at `:55`, so a
schema rename in `internal/tools` would silently stop dropping file content onto the wire. Add a
test-only cross-check in `internal/tui`: `tools.NewDefaultRegistry(t.TempDir())`
(`internal/tools/registry.go:131`), then for every entry in `contentArgs` assert the tool name
resolves via `Lookup` and every key it names exists in that tool's `Schema()` properties — with
`multi_find_and_replace`'s `replacements` read as a top-level property and its nested `oldText` /
`newText` under `properties.replacements.items.properties`. Test-only import: `internal/tui` already
imports `internal/tools` in `e2e_test.go:22`, both are the same tier under ADR 0010, and `make
check`'s only import invariant is that `internal/` must not import the root module path.

**Files:** `internal/tui/wireargs_test.go`
**Tests:** the cross-check above, failing on both halves — an unknown tool name and a key no schema
carries.
**Acceptance:** `go test -race -count=1 -run 'TestWireArgs|TestContentArgs' ./internal/tui/`
**Commit:** `test(tui): cross-check contentArgs against the tools' schemas`

## 17. F-08: the revert clears only roots apogee's own label vouches for

**What:** Recast at the regression check (2026-09-01). Fixes the untouched second prong of security
finding F-08 (`ISSUES.md:150`). The restore side is vouched (`priorRestorable`, `internal/platform/winlabel/retire.go:94`, driven by `judgePriors`,
`walk_windows.go:310`), but the CLEAR side is not: `revertibleRoots` (`retire.go:120`) filters only on
sibling liveness and hands every `Root` a journal names to `ClearTree` (`walk_windows.go:367`, root
write at `:194`, `clearSDDL = "S:"` — a NULL SACL), so a journal planted or corrupted under
`~/.apogee` can make apogee clear an arbitrary tree. `windowsProtectedRoots` (`internal/platform/winguard.go:53`)
guards the LABEL path only and is unreachable here — `winlabel` may import nothing from apogee
(`deps_test.go`). Ratified rule, implemented as a PURE decision beside `priorRestorable` in
`retire.go` so Linux tests it: `rootClearable(root, current string, readErr error) bool` clears only
when the current label reads cleanly AND `IsLowLabel(current)` (`sddl.go:51`) — apogee's own mark is
what makes the tree apogee's to clear — and refuses a volume root (`filepath.Split`-shaped check, the
same refusal `windowsLabelGuardrail` makes). Wire it in `revertSparingLiveSiblings`
(`walk_windows.go:260-272`) with the label read injected as a func seam, exactly as `alive func(int)
bool` already is at `retire.go:120`; a root that fails the test is skipped, not an abort.

**Regression guard.** Judge each journal Root ONCE, before the first clear, and persist the verdict on
the entry the way `Entry.Judged` already persists the restore side
(`internal/platform/winlabel/journal.go:56-64`) — every later revert filters on the PERSISTED verdict,
never on a fresh label read. A revert that cleared a root but failed a descendant keeps the journal
(`clearTreeOutcome`, `retire.go:58`; the retry is recorded at `session.go:186-189`), and a verdict
re-taken on that retry would read the NULL SACL `ClearTree` itself wrote (`walk_windows.go:194`), skip
the root, and let `retire.go:44` delete the journal with Low labels still standing. So `rootClearable`
is consulted from the same pre-clear pass that judges the priors (`judgePriors`, `walk_windows.go:310`)
and its answer is written onto the entry; `internal/platform/winlabel/journal.go` joins the item's
**Files:**. Separately, spell the volume-root refusal separator-agnostically INSIDE `winlabel` — a
drive-letter or UNC prefix tested over both `\` and `/`, in `foldPath`'s style — never via
`filepath.Split`, which on Linux returns the whole of `C:\` as the file half and would make the table
case pass only on Windows; `windowsLabelGuardrail`'s own check runs through `hostRules.split`, which
`winlabel` may not import.

Third, the persisted verdict must not make the journal write a PRECONDITION of clearing. `judgePriors`
writes the file back only when a prior was judged (`walk_windows.go:329`, `if !judged || own == ""`),
and a root-only journal is the overwhelmingly common case (`journal.go:53`), so persisting a ROOT
verdict would make every teardown rewrite the file first — and there "a write failure aborts the revert
with NOTHING cleared" (`walk_windows.go:295`), so an unwritable apogee home would strand every label on
runs that clear cleanly today. Set the root verdict in memory (the in-place `r.Entries` write
`judgePriors` already relies on) and let a root-only journal whose pre-clear rewrite fails fall through
to the clear it performs today; only a PRIOR's verdict keeps the abort. Fourth, the persisted-verdict
assertion cannot be written against `judgePriors` itself: that pass lives in the `//go:build windows`
file (`walk_windows.go:1`, `:310`) and would never run under this item's own acceptance (`go test
./internal/platform/winlabel/` on Linux; `go vet` compiles no test). State it as a `revertibleRoots`
filter test — a persisted verdict beats a NULL-SACL label read through the injected seam, pure in
`retire.go` — and put the "skips an already-judged root" half in a new
`internal/platform/winlabel/walk_windows_test.go`.

**Files:** `internal/platform/winlabel/retire.go`, `internal/platform/winlabel/retire_test.go`, `internal/platform/winlabel/walk_windows.go`, `internal/platform/winlabel/walk_windows_test.go`, `internal/platform/winlabel/journal.go`
**Tests:** a table beside `TestPriorRestorableTable` (`retire_test.go:376`) covering: Low label ⇒
clear; clean non-Low read ⇒ skip; unreadable ⇒ skip; volume root ⇒ skip regardless of label, over both
`\` and `/` separators so the case runs on Linux; and the filter composed with `revertibleRoots`'
sibling rule so a planted root is dropped while a live session's own root survives. Plus the persisted
verdict itself, asserted where Linux runs it: a `revertibleRoots` case where an entry carrying the
persisted verdict survives a label read seam returning the NULL SACL `ClearTree` wrote, so a revert
that failed a descendant does not re-judge its already-cleared root on the retry. The Windows-only
half — that `judgePriors` skips an already-judged root and leaves a root-only journal unwritten when
the rewrite fails — goes in the new `walk_windows_test.go`, behind the same build tag.
**Acceptance:** `go test -race -count=1 ./internal/platform/winlabel/` and `GOOS=windows go vet ./internal/platform/...`
**Commit:** `fix(winlabel): clear only journal roots apogee's own label vouches for`

## 18. A grouped never-ran delegation wears its ▶ and shows its prompt

**What:** Recast at the regression check (2026-09-01). Fixes the residual at `ISSUES.md:167`. The
2026-08-28 sweep fixed the single-block reading only (`subAgentHidesPrompt`, `internal/tui/subagentblock.go:288-308`, asked by
`blockHidesWhenCollapsed`, `blockstate.go:162`). A grouped member's indicator and click target come
from body length alone — `renderGroupMember`'s `hides = tv.Details.len() > 0`
(`internal/tui/toolblock.go:325`) — and `renderSubAgentGroup` applies `unframedSubAgentView` only when
the member is already expanded (`subagentblock.go:451-458`), so a folded never-ran delegation whose
refusal stays promoted has no body, no ▶, and an unreachable prompt at every width. Ask the member the
same prompt-shaped question: make the unspanned path's `hides` be `tv.Details.len() > 0 ||
(subAgentHidesPrompt(tv) && subAgentReported(m.head))`, and have `renderSubAgentMemberRows`
(`subagentblock.go:557-575`) re-paint the unspanned collapsed row itself — `indicatorRow(th,
leaderRow(…), width, glyphCollapsed)` — when the prompt clause is what makes it hide;
`renderGroupMember`'s rule for every tool is not changed. The SPANNED member is untouched: its row opens
the run's own view (ADR 0063).

**Regression guard.** Keep the member view build conditional on `m.head.expanded`
(`internal/tui/subagentblock.go:451-458`). Building it with `unframedSubAgentView` unconditionally runs
`view.demoted()` (`:346`), which at wide widths swaps a never-ran member's promoted refusal for the typed
stat and breaks the binding "no unconditional demote" (`subagentblock_test.go:797-798`,
`render.go:823-826`). Gate the new clause on the member being OVER, not merely unspanned: the unspanned
member's `hides` becomes `tv.Details.len() > 0 || (subAgentHidesPrompt(tv) && subAgentReported(m.head))`
(`internal/tui/subagentblock.go:494`) — the same permission `setExpanded` grants
(`internal/tui/transcript.go:1353`, which requires `head.done`) — asked of the UN-demoted view, so the
▶ and the click target arrive with no demote and the click that sets `expanded` is what builds the
prompt body. Without that gate a RUNNING span-0 member gains a ▶ and a `targetHeader` whose click is
then refused, breaking the pin at `internal/tui/transcript_test.go:1770-1771` ("  ┝ survey the tests ⋯",
no indicator, for two started members); `internal/tui/transcript_test.go` joins the item's **Files:** and
its Acceptance pattern. Second, `renderGroupMember` returns the plain row whenever its OWN `hides` is
false (`internal/tui/toolblock.go:324-326`), so OR-ing the clause after the call yields a click target
with no glyph: `renderSubAgentMemberRows` re-paints the unspanned collapsed row itself, `indicatorRow(th,
leaderRow(…), width, glyphCollapsed)`, when the prompt clause is what makes it hide. Third, the
110-column pin the first round cited does not exist — `subagentblock_test.go:756` is a width-80 golden
(`const width = 80` at `:695`) and the table at `:814-816` is a LONE block, with no test folding a
GROUPED member at 110 — so this item must ADD a grouped 110-column case: a second member in
`TestNeverRanDelegationRowIsExpandableAtEveryWidth`'s table, or a new subtest, proving the folded grouped
member keeps its promoted refusal in the outcome slot. (Owner ratified all three, 2026-09-01.)

**Files:** `internal/tui/subagentblock.go`, `internal/tui/subagentblock_test.go`, `internal/tui/transcript_test.go`
**Tests:** the subtest at `subagentblock_test.go:735` ("a grouped member opens onto the same rows")
asserts today's defect — that the shut sibling row wears no indicator — and must be re-pinned to the
fixed behaviour: the row carries the collapsed glyph, is a `targetHeader`, and expands onto the same
`task:` rows the single block shows. A member with an empty task still wears nothing (the rule
`subAgentHidesPrompt` already encodes), and so does a member still RUNNING — the two started span-0
members at `transcript_test.go:1770-1771` keep their bare `┝ survey the tests ⋯` rows, which the gate on
`subAgentReported` is what preserves. Plus a NEW grouped 110-column case — a second member added to
`TestNeverRanDelegationRowIsExpandableAtEveryWidth`'s table (`subagentblock_test.go:814-816`, a LONE
block today) or a new subtest of its own — proving the folded GROUPED member still carries its promoted
refusal in the outcome slot, no demote arriving with the indicator.
**Acceptance:** `go test -race -count=1 -run 'TestUnframedSubAgent|TestNeverRanDelegation|TestGroupMember|TestExpandedGroupMember|TestSubAgentStream|TestEveryToolBodyFrameKeepsItsOwnFraming' ./internal/tui/`
**Commit:** `fix(tui): grant a grouped never-ran delegation its prompt indicator`

## 19. The settings note is measured against the post-marker width

**What:** Fixes the residual at `ISSUES.md:177`. `settingsNoteWidth` (`internal/tui/settings.go:1556`)
measures the columns from `settingRowCells` as the rows stand, and `autoBlastRadiusNote`
(`internal/tui/settingsapply.go:221`) chooses the whole sentence whenever it fits — but
`settingsApplied` records the edit only AFTER computing the note (`settingsapply.go:159` then `:161`),
so `settingsValueCell` (`settings.go:1442-1455`) has not yet appended `settingsEditMarker = " *"`
(`settings.go:304`) and the value column is measured two cells narrow. Measure post-marker: give
`settingsNoteWidth` the pending edit (a `settingRowCells` variant that consults it) so the measured
value column carries the marker the apply will add. Do NOT reorder `settingsApplied` — the
record-after-apply ordering is deliberate and documented at `settingsapply.go:129-137`. Note the
widening only bites when the marked row is the widest in its column (`popupColumnWidths`,
`internal/tui/popup.go:549`), which is why 80 and 160 columns never showed it.

**Files:** `internal/tui/settings.go`, `internal/tui/settingsapply.go`, `internal/tui/settings_test.go`
**Tests:** a width case at the boundary the fix exists for — an applied row whose value cell is the
column's widest and whose note lands within two cells of the edge — asserting the clause fallback
(`autoBlastRadiusClause`) is chosen, not the elided full sentence. The existing cases at
`settings_test.go:1324-1339` (80 and 160 columns) must stay green.
**Acceptance:** `go test -race -count=1 -run 'TestSettings' ./internal/tui/`
**Commit:** `fix(tui): measure the settings note against the post-marker value column`

## 20. MCP's unusable-proxy refusal wraps `security.ErrURLBlocked`

**What:** Fixes the residual at `ISSUES.md:184`. `vetEndpoint` returns a bare `fmt.Errorf` when the
egress proxy is not a usable URL (`internal/mcp/transport.go:228`), while its sibling three lines down
wraps the guard's error (`:235`) and both `internal/tools` funnel paths wrap the sentinel
(`internal/tools/network.go:354`, `:358`) — so a caller matching on `security.ErrURLBlocked` sees an
MCP unusable-proxy refusal as an unrelated error. Wrap it, following `network.go:354`'s
sentinel-first form so the existing wording assertion still holds: the message must keep naming
`"not a usable URL"` and must still never print the proxy's password.

**Regression guard.** `transport.go:229` is the ONLY place the unusable-proxy refusal names the server,
and the `network.go:354` form carries no name — dropping `mcp: server %q:` would leave the settings row's
reconnect note (`cmd/apogee/wire_settings.go:1429`, `"; mcp " + err.Error()`) with no server name at all.
The pinned wording: `fmt.Errorf("mcp: server %q: %w: the configured egress proxy is not a usable URL",
cfg.Name, security.ErrURLBlocked)`.

**Files:** `internal/mcp/transport.go`, `internal/mcp/transport_test.go`
**Tests:** `TestVetEndpoint_AnUnusableProxyRefusesTheEndpoint` (`transport_test.go:354`) gains the
`errors.Is(err, security.ErrURLBlocked)` assertion its sibling at `:400-402` already makes, alongside
its existing wording and no-password checks, AND asserts the refusal still names the server `proxied`
beside the sentinel. It must fail against the pre-item tree.
**Acceptance:** `go test -race -count=1 -run 'TestVetEndpoint_' ./internal/mcp/`
**Commit:** `fix(mcp): wrap ErrURLBlocked on the unusable-proxy refusal`

## 21. `idOf` keys an unparseable stack block on its content

**What:** Fixes the residual at `ISSUES.md:192`. `idOf` (`internal/tuitest/leak.go:132-144`) returns
the empty id for any block whose header does not parse, and `leakedGoroutines` stores blocks by that
id (`leak.go:124`), so two unparseable blocks collapse into one map entry and one present at snapshot
time makes `startedSince` (`leak.go:91-102`) forgive every later one — exactly what the function's own
doc says cannot happen. Ratified: make the id unique. Key an unparseable block on a short content hash
of its trimmed text, prefixed so it can never collide with a real goroutine id, and correct the doc
comment to state the new rule and its one residual: two goroutines with byte-identical unparseable
stacks still share a key, which is the same forgiveness a matching id pair would earn.

**Regression guard.** `startedSince` calls `leakedGoroutines()` itself (`internal/tuitest/leak.go:91`),
reading the live `runtime.Stack` dump with no seam to inject a block — and the runtime never emits an
unparseable `goroutine ` header — so the drafted `startedSince` case cannot be written as described.
**What** must name the decomposition it needs: a pure `leakedIn(dump string) map[goroutineID]string`, or
`startedSince(snapshot, current map[goroutineID]string)`, so that case drives a seam rather than the live
dump. And the acceptance pattern gains `|TestStartedSince` (or the case is named under the
`TestCheckLeaks…` prefix) — as drafted it matches no test named for `startedSince` and would not run at
its own gate.

**Files:** `internal/tuitest/leak.go`, `internal/tuitest/leak_test.go`
**Tests:** `idOf` gets its first direct test — a well-formed header still yields the bare number; two
DIFFERENT malformed blocks yield two different non-empty ids; the same malformed block yields the same
id across calls, so a snapshot still forgives it. Plus one `startedSince` case proving a malformed
block present at snapshot no longer forgives a different malformed block.
**Acceptance:** `go test -race -count=1 -run 'TestLeaked|TestCheckLeaks|TestIdOf|TestStartedSince' ./internal/tuitest/`
**Commit:** `fix(tuitest): key an unparseable stack block on its content`

## 22. The manual documents the auto row and the headless flag notice

**What:** `docs/manual/configuration.md:596-603` describes `/sub-agents-server` as a pick over the
`servers:` entries recorded as `sub-agents-server: <name>`, and `:623-629` describes the retired
`sub-agents: true` migration as an in-app offer; `docs/manual/commands.md:14,18` lists the verb and
`:45` is its table row. None of them knows about item 4's `auto` row, what accepting it clears, the
`/sub-agents-server auto` argument spelling, or item 5's headless stderr notice. Amend them to match what
the code emits, taking every quoted string from the LANDED code (`internal/tui/picker.go`,
`cmd/apogee/keymigrate.go`) and never from this plan document. Put the headless notice where the manual
already describes the retired flag (`configuration.md:623-629`) and, if `docs/manual/headless.md` carries
a notices section, there too — the plaintext-key notice is documented at `configuration.md:744-745` and
nowhere in `headless.md`, so follow whichever placement that precedent sets.

This item also owns `layout.md` (repo root — the TUI layout/rendering spec named in `AGENTS.md`), which
carries the `/sub-agents-server` pane's row and note contract that items 4 and 18 change and neither
touches: `:1813-1815` fixes the rows as "those same entries in two columns only" with "deliberately no
third cell", and `:1820` fixes the transcript note as one line naming the entry with
`· sub-agents-server: saved` after it. Bring that contract to what the landed code emits — the `auto`
row's label and its hint cell, the ` · sub-agents-server: cleared` clause the auto row alone carries,
and the grouped never-ran delegation's indicator rule item 18 lands (a folded member with no body wears
the collapsed glyph and opens onto its prompt once the delegation is over) — taking every quoted string
from the landed code, never from this plan. Keeping every cross-cutting doc amendment in this one owning
item is the owner's ratified call (2026-09-01). Depends on items 4, 5 and 18.
(Added at the regression check, 2026-09-01: the owner's answer to reviewer 1's finding that no item
touched `docs/manual/` while items 4 and 5 are user-facing.)

**Files:** `docs/manual/configuration.md`, `docs/manual/commands.md`, `docs/manual/headless.md`, `layout.md`
**Tests:** none — prose.
**Acceptance:** a grep proving the manual names the auto row and the cleared key, a grep proving it names
the headless notice, a grep proving `layout.md`'s `/sub-agents-server` pane section names the `auto` row,
the `cleared` clause and the grouped never-ran member's indicator, and a check that each quoted string is
byte-identical to the constant it quotes.
**Commit:** `docs(manual): document the sub-agents auto row and the headless flag notice`

## Closeout

Every item removes its own entry from `ISSUES.md` in its commit; when item 21 lands, the
`## Open defects` section holds nothing but its heading and the six `**Status:**` blocks go with the
last entry each covers. The closeout runs `make check` once for the whole plan and rolls the entries
into `CHANGELOG.md` under `[Unreleased]` — including a correction of the F-08 line at
`CHANGELOG.md:1277`, which records the finding as closed while only its restore prong was remediated.

## Suggested version bump

Nineteen defect fixes, three of them user-visible (the `auto` row, the headless notice, the run-view
flash) and one a security remediation. A patch-level `VERSION` micro-bump is warranted at the
closeout — the owner decides; no item touches `VERSION`, `CHANGELOG.md`'s release heading, or a tag.
