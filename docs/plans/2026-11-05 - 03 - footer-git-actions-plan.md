# Footer git actions — click the git segment (or `/git`) for fetch, pull, push, sync, switch and rebase

**Goal:** Clicking the footer's git segment, or running `/git`, opens a picker of git actions on the workspace. Each action runs as the user's own act through the hardened git runner, reports its result as a transcript note, and refreshes the segment's counts. The decision is recorded as an ADR.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 5cedb3a3

**Sources:**
- `.capcom/issues/footer-git-actions.md`
- internal/tui/gitstatus.go (`GitStatusHost`, `refreshGit`); cmd/apogee/gitstatus_host.go (`gitStatusHost`, `gitFetchArgs`, `gitNoPromptEnv`, `gitSSHPassThrough`, `gitBatchModeSSH`, `gitStatusBackstop`); internal/gitexec
- internal/tui/picker.go (`pickerKind`, `pickerOfferings`, `pickerMode`, `pickerWorkflowBoundary`); internal/tui/command.go (`commandSpecs`); internal/tui/engineholds.go
- ADR 0053 (list surfaces), ADR 0054 (Options host interfaces), ADR 0029 (actuation latch), ADR 0012 amendment 0906 and ADR 0056 (refused repositories), ADR 0031 (Drivers)
- layout.md "The git state", "And it is a control", the picker section

**Ratified design calls** (owner, 2026-11-05):
- **Actions:** fetch, pull (`--ff-only`), push (never forced), sync (pull then push, stops at the first failure), switch branch, rebase onto upstream.
- **Gate:** a user action outside the Approver and the mode, like `/undo`; it still uses the hardened runner, and a refused repository stays refused.
- **While busy:** refused with a note while a Turn runs or a decision is pending; the panel opens only when idle.
- **Rebase:** confirm step first, clean tree required; on conflict apogee runs `git rebase --abort` and the note names the conflicting files.
- **Push without upstream:** `push -u origin <branch>` when a remote named `origin` exists, else refused with a note naming the remotes.
- **Writer calls (2026-11-05):** results land as transcript notes (no toast surface exists); the keyboard reach is `/git`; the surface is the picker (two new kinds); switch lists local branches and is refused on a dirty tree; actions needing an upstream (pull, sync, rebase) are listed only when the branch has one; while an action runs, prompts and a second action wait (an engine hold); a slow push or pull is bounded only by a generous backstop (slow-box rule).

**Standing requirements:**
- skills: coding-standards
- Any deviation from item text lands as a dated NOTES line under the item.
- Raspberry Pi 5 host: never `-race`/`-cover` over a whole heavy package or a pattern; targeted `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`; one test run at a time. Forward to every sub-agent.
- The TUI never imports gitexec or subprocess (`TestGitStatusImportsNoGitRunner`); git runs behind an Options host interface (ADR 0054).
- Bubble Tea v2: keys are `tea.KeyPressMsg` matched on `msg.String()`.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

**Out of scope:**
- Force-push, merge, stash, creating or deleting branches, remote branches in the switch list, conflict resolution.
- Git actions in `apogee daemon` / `headless` (TUI Driver only).

**Regression check (2026-11-05, 5cedb3a3):**
- 2: guard folded (decision: next free number, never ADR 0107; ADR index line; hooks-off consequence)
- 3: guard folded (decision: submodule recursion off; doc map; field-preserving extraction; Windows ssh skip is an error; refused-repo test wording)
- 4: guard folded (decision: recursion off on switch and rebase; `lstrip=2` listing and `refs/heads/` check)
- 5: guard folded (decision: per-row span geometry; hold width, groups, fetch and quit; command and picker pins; `/git` whileRunning); supersedes the mouse.go:460 / pointershape.go:31-43 "one CHROME control" comments
- 6: guard folded (latch choice; `Branches` off the loop; confirm carrier; picker enum bound)
- 6: decision applied — the rebase confirm is latched as `raiseAdoption`, never an unlatched kind (replaces the executor's choice)
- 7: guard folded (every mode-marker click/pointer passage; fetch race and pull ordering)

## 1. Verify the footer rows plan is archived — ✅ DONE (2026-11-05)

NOTES (2026-11-05): gate passed — `docs/plans/archived/2026-11-05 - 02 - footer-rows-and-colour-plan.md` exists (archived in 3d8e0070) and the unarchived path is gone; FILES is empty because the item edits nothing.

**What:** this plan clicks a segment that plan `2026-11-05 - 02 - footer-rows-and-colour-plan.md` lays out (per-segment spans, multi-row hit rows).

**Goal:** `docs/plans/archived/2026-11-05 - 02 - footer-rows-and-colour-plan.md` exists.

**Files:** none
**Read first:** docs/plans/2026-11-05 - 02 - footer-rows-and-colour-plan.md — the gated plan (archived under the same basename, per 218ddf4b / fad6d88f); docs/plans/archived — basename-preserving archive precedent

**Tests:** none

**Acceptance:**
- `test -f "docs/plans/archived/2026-11-05 - 02 - footer-rows-and-colour-plan.md"`

**Commit:** none (gate only)

## 2. ADR — footer git actions are the user's own act — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the record is ADR 0108 — 0107 is the next free file number but plan `2026-11-05 - 00 - v1-placeholder-retraction-plan.md` claims it, so the index line `adr-0108` sits directly after `adr-0106` with no 0107 line between them yet.
NOTES (2026-11-05): decision 3 also records the item-3 guard calls the header lists (no submodule recursion; a transport that cannot run safely, e.g. Windows with an operator ssh command, refuses with a note instead of the background fetch's silent skip), so later items implement against the record.

**What:** Depends on item 1.

**Regression guard.** the ADR takes the next free number at run time and never ADR 0107, which plan `2026-11-05 - 00 - v1-placeholder-retraction-plan.md` claims. `capcom check` passes with a missing index line, so add `- adr-NNNN — <title>` to the hand-kept index `.capcom/knowledge/decisions.md` (after `adr-0106` at :110), and the record body names its children in a reading-order sentence as adr-0106.md does. Its consequences state that repo hooks never run on the footer acts (gitexec's `-c core.hooksPath=`, `hardeningOptions`), unlike the user typing the same git — a git-lfs pre-push upload is skipped — so the trade is recorded, not discovered.

**Goal:** a new ADR (the next free number at run time) records the header's ratified calls: the action set, outside the Approver, refused while busy, refused repositories stay refused, rebase auto-abort, push upstream rule, transcript-note results, `/git` as keyboard reach; its consequences name the hardened transport every networked action uses. The glossary gains no term.

**Approach (assumed at the header base):** `capcom know add --parent decisions` with the house ADR sections as children; Status accepted.

**Files:** .capcom/knowledge/decisions/, .capcom/knowledge/decisions.md
**Read first:** .capcom/knowledge/decisions.md — ADR index list; .capcom/knowledge/decisions/adr-0106.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0054.md — one-interface-per-family rule;
internal/gitexec/gitexec.go — hardeningOptions; docs/plans/2026-11-05 - 00 - v1-placeholder-retraction-plan.md — item 7 (ADR 0107 claim)

**Tests:** none

**Acceptance:**
- `capcom check`
- `capcom know show "ADR NNNN"` (the number written) prints the record
- `grep -n 'adr-NNNN' .capcom/knowledge/decisions.md` (the number written) finds the index line

**Commit:** `docs(adr): footer git actions are the user's own act`

## 3. A git actions host: fetch, pull, push, sync

**What:** Depends on item 2.

**Regression guard.** gitexec's command-config refusal covers only the root repository's config, so every verb this item adds that can recurse into a submodule runs with recursion off — `-c submodule.recurse=false` on each call, plus `--recurse-submodules=no` on push (and on the pull's fetch, as the fetch chain does if it already does) — and a host test asserts the flags in the argv.
- The shared helpers are extracted as functions over `gitStatusHost`'s existing fields: it keeps `git`/`lookupEnv`/`goos` as direct fields, so the literals in cmd/apogee/gitstatus_host_test.go (:102, :162) still compile.
- Where `gitStatusHost.Fetch` skips (nil) on Windows with an operator-named ssh command, the actions host returns an error naming why, never a nil result.
- The new file is named in cmd/apogee/doc.go's map (`TestDocMapNamesEveryFile`), beside the gitstatus_host.go narration (:126).

**Goal:** a `GitActionsHost` interface in internal/tui (an `Options` field, nil means no git actions) with `Fetch`, `Pull`, `Push` and `Sync`, each returning a one-line result or an error; the cmd/apogee implementation runs every networked call with the fetch chain's transport hardening and no-prompt env (push with `--receive-pack=git-receive-pack`), pull as hardened fetch + `merge --ff-only @{u}`, push `-u origin <branch>` only without an upstream and with `origin` present, never `--force`; a refused repository runs no git.

**Approach (assumed at the header base):** a sibling of `GitStatusHost` per ADR 0054; cmd/apogee/gitactions_host.go reuses `gitStatusHost`'s env, ssh and `-c` helpers (extract any it needs into shared functions, no copies); wired in cmd/apogee/wire_options.go; budget `gitStatusBackstop`.

**Files:** internal/tui/gitactions.go, internal/tui/tui.go, internal/tui/doc.go, cmd/apogee/gitactions_host.go, cmd/apogee/gitactions_host_test.go, cmd/apogee/gitstatus_host.go, cmd/apogee/wire_options.go, cmd/apogee/doc.go
**Read first:** cmd/apogee/gitstatus_host.go — gitStatusHost.Fetch, gitFetchArgs; internal/gitexec/gitexec.go — hardeningOptions; internal/tui/gitstatus.go — GitStatusHost;
cmd/apogee/gitstatus_host_test.go — statusHostClone, recordingStatusHost, TestGitStatusHostFetchTransportRunsNoCommand; cmd/apogee/doc.go — gitstatus_host.go narration

**Tests:** host tests over `statusHostClone` fixtures (`gitStatusClones` is the e2e one): pull fast-forwards, pull refuses a diverged branch, push, push sets upstream on origin, push without origin refused naming the remotes, sync stops after a failed pull, argv carries the hardening pairs and never `--force`, argv carries `-c submodule.recurse=false` on every call and `--recurse-submodules=no` on push (a `TestGitStatusHostFetchTransportRunsNoCommand`-style marker case for push), Windows with an operator ssh command returns an error, refused repository runs no fetch/merge/push verb (asserted with `recordingStatusHost.ran`, as `TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch` does — gitexec's own probe still spawns git).

**Acceptance:**
- `go build ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitActionsHost|TestGitStatusHost|TestDocMap' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatusImportsNoGitRunner|TestDocMap' ./internal/tui/`

**Commit:** `feat(git): a hardened git actions host for fetch, pull, push and sync`

## 4. Host: branches, switch, rebase

**What:** Depends on item 3.

**Regression guard.** the same recursion-off rule binds switch and rebase (`-c submodule.recurse=false`), asserted in the argv tests. `Branches` lists with `%(refname:lstrip=2)`, never `%(refname:short)` (which yields `heads/x` when a tag `x` exists, and `checkout heads/x --` detaches HEAD), and `Switch` verifies `refs/heads/<name>` exists before the checkout.

**Goal:** `GitActionsHost` adds `Branches` (local branch names, current first), `Switch(name)` (refused on a dirty tree; a leading `-` refused; `--` after the name) and `Rebase` (onto `@{u}`, refused on a dirty tree; on conflict runs `rebase --abort` and returns an error naming the conflicting files, the tree as before).

**Approach (assumed at the header base):** extends cmd/apogee/gitactions_host.go; the switch argv follows `git_branch`'s `checkout <name> --` precedent in internal/tools/git.go.

**Files:** internal/tui/gitactions.go, cmd/apogee/gitactions_host.go, cmd/apogee/gitactions_host_test.go
**Read first:** internal/tools/git.go — git_branch switch argv (`checkout <name> --`), looksLikeOption; cmd/apogee/gitstatus_host.go — gitStatusArgs, gitStatusHost.Status (dirty read);
cmd/apogee/gitstatus_host_test.go — statusHostClone, gitFixture, writeFixtureFile, recordingStatusHost

**Tests:** list order, with a tag-shadowed branch listed by its bare name; switch clean / dirty / option-like name / name with no `refs/heads/` branch; rebase clean success, dirty refusal, conflict → aborted with files named and `HEAD` unchanged; argv carries `-c submodule.recurse=false` on switch, rebase and `rebase --abort`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitActionsHost' ./cmd/apogee/`

**Commit:** `feat(git): git actions host lists, switches and rebases branches`

## 5. The git actions picker, the footer click and `/git`

**What:** Depends on item 3.

**Regression guard.** the git hit and the pointer shape read the git segment's cell span and its row from plan 02's per-segment span table in the per-row `footerRow` layout, never `footerRowY` or `footerLayout.git`/`gitAt` (gone after plan 02); an executor re-derives the approach against the archived plan 02 tree.
- Holds: `engineHold` widens from uint8 (eight bits taken) to uint16 and the new bit gets its `engineHoldSources` row; the bit joins `holdsTakingAgent` and `holdsBlockingResume` and gets a `loadHoldNote` case, so a prompt, wake or workflow resume waits; while it stands `foldGitFetchTick` issues no fetch (it re-arms) and an action waits for an in-flight background fetch to land; a quit under it defers its exit (`holdQuitting`, as the `writeBusy` branch of `quitSession`) until the completion Msg folds.
- `/git` is a `whileRunning: true` row whose run, while busy, notes "git actions wait for the Turn to end" (the /schedule shape) — an idle-only row is queued silently.
- Pins: `"git"` goes between `"fork"` and `"help"` in `TestCommandTableDrivesParserAndMenu`'s `wantParsed`; `TestEveryPickerKindHasAnOffering`'s bounds (`pickerAdoption`) move to the new last kind.
- Supersedes the comments naming the mode marker as the footer's one click/pointer target (mouse.go:460 "the frame's one CHROME control", pointershape.go:31-43 walk order): every such comment (`grep -n 'mode marker\|CHROME control' internal/tui/*.go`) gains the git segment.

**Goal:** a click on the footer's git segment (pointer shape over it) or `/git` opens a `pickerGitActions` listing fetch, push and — only with an upstream — pull, sync and rebase, plus switch branch; choosing fetch/pull/push/sync runs it off the loop under an engine hold, notes `git <action>…` then the result or error, and calls `refreshGit`; while busy, the click and `/git` add a note that git actions wait for the Turn to end; with no git (nil host or no repository apogee can act on) `/git` says so.

**Approach (assumed at the header base):** a `footerGitHit` beside `footerModeHit` over the git segment's span from `footerRow`; a new `pickerOfferings` kind; a `commandSpecs` row kept alphabetical; a new `engineHold` bit; completion folds as a message.

**Files:** internal/tui/picker.go, internal/tui/mouse.go, internal/tui/pointershape.go, internal/tui/command.go, internal/tui/engineholds.go, internal/tui/gitactions.go, internal/tui/gitactions_test.go, internal/tui/mouse_test.go, internal/tui/model.go, internal/tui/gitstatus.go, internal/tui/command_test.go, internal/tui/picker_test.go
**Read first:** internal/tui/mouse.go — handleFooterModeClick, footerModeHit; internal/tui/model.go — footerRow, loadHoldNote, quitSession;
internal/tui/engineholds.go — engineHoldSources, holdsTakingAgent; internal/tui/gitstatus.go — foldGitFetchTick

**Tests:** with `fakeGitHost` and a fake actions host: click opens the picker (hit on the git segment's own row per plan 02's span table; the mouse_test helper follows suit); `/git` opens it; rows follow the upstream; each action's success and failure notes; refresh after; refused while busy (click and `/git` note); a prompt submitted mid-action waits; no background fetch fires mid-action; a quit mid-action exits only after the completion folds.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitAction|TestClick|TestCommandSpecs|TestCommandTableDrivesParserAndMenu|TestEveryPickerKindHasAnOffering' ./internal/tui/`

**Commit:** `feat(tui): click the footer's git segment or run /git for git actions`

## 6. Switch branch and rebase in the picker

**What:** Depends on items 4 and 5.

**Regression guard.** `pickerWorkflowBoundary` opens unlatched (commandrun.go `confirmBoundary`) and `latched` is set only through `openDecisionLatch`: the rebase confirm is latched — `latched: true` + `m.openDecisionLatch()` as `raiseAdoption` does, armed before ⏎ — never an unlatched question kind. `Branches` runs in a Cmd under the git-action hold and `pickerGitBranch` opens on its landing Msg, never synchronously as `pickLaunchProfile` reads `Profiles()`; the names ride on `picker` as a plain `[]string` (ADR 0011). The confirm's carrier is named (a third kind, or a field on `picker`), each new kind moves `TestEveryPickerKindHasAnOffering`'s bound, and item 7's layout.md kind list names every kind this plan adds.

**Goal:** "switch branch" opens a filtering `pickerGitBranch` of `Branches`, accepting runs `Switch`; "rebase onto upstream" opens a confirm step (latched, as `raiseAdoption`) before `Rebase`; each notes its result or error and refreshes the segment.

**Approach (assumed at the header base):** two-step flow as /schedule's cycle then mode; rows read at open as `pickerLoad`'s.

**Files:** internal/tui/picker.go, internal/tui/gitactions.go, internal/tui/gitactions_test.go, internal/tui/picker_test.go
**Read first:** internal/tui/picker.go — pickerOfferings, acceptPicker, pickLaunchProfile, picker.latched; internal/tui/commandrun.go — confirmBoundary, boundaryKey;
internal/tui/approval.go — openDecisionLatch; internal/tui/adoption.go — raiseAdoption

**Tests:** branch list filters; the branch picker opens on `Branches`' landing Msg, not at the click; switch success, dirty refusal note; rebase confirm cancel runs nothing (armed before ⏎); rebase conflict note names the files.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitAction|TestEveryPickerKindHasAnOffering' ./internal/tui/`

**Commit:** `feat(tui): switch branch and rebase from the git actions picker`

## 7. Driven test and docs

**What:** Depends on item 6.

**Regression guard.** every passage naming the footer's mode marker as a click or pointer target gains the git segment (`grep -n 'mode marker' layout.md README.md docs/manual/*.md docs/layout/*.md` — at base layout.md:169 outside-click row, layout.md:186 pointer-shape table, README.md:245). The e2e polls both chains every 250 ms (`gitStatusEvery`), so it relies on item 5's fetch hold against the "cannot lock ref" race, and it clicks pull before making the local commit (`--ff-only` refuses a diverged branch).

**Goal:** an e2e test clicks the git segment of a live TUI over `gitStatusClones`, pulls a pusher's commit and pushes a local one, and sees `N↓`/`N↑` refresh; a refused repository shows no git segment and `/git` refuses; layout.md "The git state" says the segment is a control and the picker section names both kinds; docs/manual/commands.md documents `/git`.

**Approach (assumed at the header base):** extend cmd/apogee/e2e_gitstatus_test.go with `launchTUIInGitPolling` and `click`.

**Files:** cmd/apogee/e2e_gitstatus_test.go, layout.md, docs/manual/commands.md, README.md
**Read first:** cmd/apogee/e2e_gitstatus_test.go — TestE2EGitStatusFollowsTheUpstream, gitStatusClones, gitCommitFile; cmd/apogee/e2e_support_test.go — launchTUIInGitPolling;
cmd/apogee/e2e_popups_test.go — click; cmd/apogee/e2e_smoke_test.go — footerRow; layout.md — "The git state", pointer-shape table

**Tests:** the e2e test above.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EGit' ./cmd/apogee/`
- `grep -n '/git' docs/manual/commands.md`

**Closes:** footer-git-actions

**Commit:** `test(e2e): drive the footer git actions over a live clone`
