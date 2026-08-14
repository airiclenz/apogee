# Open-residuals sweep — clear the ISSUES.md Open-defects register

- **Goal:** resolve all 12 open run-residual defects in `ISSUES.md` (the four "Run residuals —
  open" sections dated 2026-08-13/14), leaving the Open-defects section empty. Seven are
  doc-truth fixes, two are test gaps, three are small behavioral fixes whose design calls are
  ratified below.
- **Date:** 2026-08-14
- **Status:** not started
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `ISSUES.md` Open-defects sections as of commit `bf4a28d` — each item names its bullet.
  - ADR 0047 (`docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md`) §6 — the
    overlay-scope decision item 5 realises.
  - ADR 0049 — the permit/lexical-vs-resolved split item 2's wording must describe.
  - ADR 0030 — the width-mirror fidelity framing behind item 6.
  - Scout evidence (verified 2026-08-14, this session) is embedded per item as file:line.
- **Ratified design calls (owner, 2026-08-14, via AskUserQuestion in the planning session):**
  1. `/server` back onto the startup entry dropping the `APOGEE_API_KEY` overlay is **ratified
     as intended** (ADR 0047 §6's literal reading). No behavior change; item 5 is doc-only.
  2. A cancel inside the restream hold-off becomes **resumable `endCancelled`** — uniform
     cancel semantics (item 11).
  3. The ephemeral-endpoint label collision is fixed **at alias synthesis** in
     `internal/config` (collision-aware fallback `HostAlias`), not in the picker (item 10).
  4. The ISSUES demotion edit was committed standalone (`bf4a28d`) so this plan starts clean.
- **Standing requirements:** skills: coding-standards. Every item that closes an ISSUES bullet
  REMOVES that bullet from `ISSUES.md` (and the containing "Run residuals" section header when
  the item empties it); the item's CHANGELOG entry goes through the run's sidecar per the
  executor's convention. Any authorized deviation from item text lands as a dated NOTES line
  under the item.
- **Out of scope:** the `[P2]` context-budget % config key (stays in ISSUES); changing the
  `APOGEE_API_KEY` overlay behavior (ratified against, call 1); everything under ISSUES.md
  "Parked / deferred work"; any VERSION/CHANGELOG-release-heading change.

## 1. Scrollbar prose covers popup panes — ✅ DONE (2026-08-14)

NOTES (2026-08-14): retry — restored `ISSUES.md` with `git checkout --` per the dispatch DECISION and re-applied only item 1's own bullet removal; the sibling items' source edits already in the tree (`internal/security/doc.go`, `internal/config/configsplice.go`, `internal/tools/exec_common.go`) were left untouched.
NOTES (2026-08-14): `CHANGELOG.md` was edited directly because the item's What and Acceptance name that file (the "No pane opts in yet." sentence); the verifier remains the sole writer of the `[Unreleased]` entry above, which is left to it.
NOTES (2026-08-14): also tightened the `ui:` block's summary line (`internal/config/defaults/config.yaml:510`, "whether the transcript shows a scroll bar" → "whether a scroll bar is shown") — the same transcript-only framing, in the same file and block, one line beyond the two the item names.

**What:** Two places still describe the scroll bar as the transcript's alone, now that every
overflowing popup paints one. Reword the `show-scrollbar` prose in
`internal/config/defaults/config.yaml:539-542` and the same claim on the key's own line at
`:565` to say the bar applies to the transcript and to overflowing popups. In `CHANGELOG.md`,
find the popup-scrollbar entry ending "No pane opts in yet." (near line 72 — locate by that
exact text, line numbers shift) and replace that sentence so it no longer contradicts the
entry directly below it. Remove the corresponding ISSUES bullet (settings-coverage section).

**Files:** `internal/config/defaults/config.yaml`, `CHANGELOG.md`, `ISSUES.md`

**Tests:** none (prose only).

**Acceptance:** `grep -c "No pane opts in yet" CHANGELOG.md` → 0;
`grep -n "show-scrollbar" internal/config/defaults/config.yaml` shows the reworded prose;
`go build ./internal/config/`.

**Commit:** `docs(config): scrollbar prose covers popup panes`

## 2. openMutationRoot doc describes the permit case — ✅ DONE (2026-08-14)

NOTES (2026-08-14): retry — a prior batch attempt had already written a doc.go reword into the tree; checked it against `openMutationRoot`/`rootRelative` and corrected it. Its "It routes on the RESOLVED path, not the spelling" was over-broad (only the permit match is resolved-based; the fallback branch is lexical, `internal/security/safeio.go:629`), and "with no permit it answers with today's workspace root" omitted the no-permit out-of-workspace refusal. Final wording states the resolved-first permit question and the unchanged lexical branch with its refusal.

NOTES (2026-08-14): the sibling items' in-flight edits (`internal/config/configsplice.go`, `internal/tools/exec_common.go`) were left untouched per the dispatch DECISION; only this item's `ISSUES.md` bullet was removed (5 lines, section keeps its other bullets).

**What:** `internal/security/doc.go:103-104` reads imprecisely since the lexical and resolved
readings were split: the sentence on `openMutationRoot` answering "with today's workspace
root, byte-for-byte" no longer describes the permit-plus-workspace-internal-target case the
ADR 0049 fix introduced. Reword the sentence so it matches what `openMutationRoot` actually
answers in both cases (check the function's current behavior in `internal/security` before
writing). Doc-only, same package. Remove the corresponding ISSUES bullet (ISSUES-sweep
section).

**Files:** `internal/security/doc.go`, `ISSUES.md`

**Tests:** none (doc comment only).

**Acceptance:** `go build ./internal/security/`; `go vet ./internal/security/`.

**Commit:** `docs(security): openMutationRoot doc covers the permit case`

## 3. ReadConfigForWrite caller list includes settingsedit — ✅ DONE (2026-08-14)

NOTES (2026-08-14): retry — a prior batch attempt had already written the caller paragraph into `internal/config/configsplice.go`; checked it against `externalEdit.spec` and corrected it. Its "takes the seed and the bytes alone" left the bytes' purpose unstated; the final wording says the read serves the seed and the bytes the key's line is located in (`settingKeyLine`, `cmd/apogee/settingsedit.go:184`), and that the baseline is taken after the seed (`:168-172`). The prior attempt's narrowing of the key-source sentence to "the one exception among the splicers" was correct and kept.

NOTES (2026-08-14): only this item's `ISSUES.md` bullet was removed (3 lines + separator; the ISSUES-sweep section keeps its other bullets). The sibling item's in-flight edit in `internal/tools/exec_common.go` was left untouched per the dispatch DECISION.

**What:** `ReadConfigForWrite`'s doc comment (`internal/config/configsplice.go:231-235`)
enumerates its splicing callers but omits the non-splicing one,
`cmd/apogee/settingsedit.go:164`, so the list reads as exhaustive when it is not. Add the
missing caller (noting it is non-splicing) or reword the list as illustrative — prefer adding
the caller, keeping the list exhaustive. Remove the corresponding ISSUES bullet (ISSUES-sweep
section).

**Files:** `internal/config/configsplice.go`, `ISSUES.md`

**Tests:** none (doc comment only).

**Acceptance:** `go build ./internal/config/`; `grep -n "settingsedit" internal/config/configsplice.go` shows the added caller.

**Commit:** `docs(config): ReadConfigForWrite doc lists its non-splicing caller`

## 4. Teardown and git-filter claims align with the code — ✅ DONE (2026-08-14)

NOTES (2026-08-14): retry — the prior batch attempt's `exec_common.go` reword was re-checked against `setProcessGroupTeardown` (`internal/tools/exec_pgroup_unix.go:11-38`), `planTreeKill` (`exec_teardown.go`) and `doc.go:175-188`, and corrected: it omitted that the escaped descendant stays inside the Confiner's fence (loss of supervision, not of confinement — the framing doc.go uses), which matters directly above the confinement bullet; the file-path citation was dropped as same-package noise in favour of naming `setProcessGroupTeardown` alone.

NOTES (2026-08-14): the `ISSUES.md` closeout paragraph's untouched tail (the `GOENV=off` residual sentence) was re-wrapped, without wording changes, because the reworded first residual left a ragged short line mid-paragraph.

**What:** Two stale doc claims from the security-audit-fixes run. (a) `runSubprocess`'s
overview bullet still states absolutely that a cancelled or timed-out command "never orphans
its children" (`internal/tools/exec_common.go:184`); qualify it with the setsid escape,
consistent with the teardown docs it summarises (`internal/tools/exec_teardown.go:37`,
`internal/tools/doc.go:183`, `internal/tools/exec_pgroup_unix.go:63`). (b) The 2026-08-12
batch closeout in `ISSUES.md` (the Deferred security-review Lows entry, "Closeout, 2026-08-12"
paragraph) still narrates the configured-filter residual as fully open — "git offers no switch
that refuses configured filters, so only the read-path textconv/ext-diff half is closed" —
which is stale for repo-local scopes since `runGit` refuses a repo-local
`filter.*.clean/smudge/process` driver before running (commit `dff4e7b`); reword so the
residual survives only for global config, which is the operator's. Remove both corresponding
ISSUES bullets (security-audit-fixes section).

**Files:** `internal/tools/exec_common.go`, `ISSUES.md`

**Tests:** none (doc/prose only).

**Acceptance:** `go build ./internal/tools/`;
`grep -c "never orphans its children" internal/tools/exec_common.go` → 0.

**Commit:** `docs(tools): qualify the orphan claim; update the git-filter residual narration`

## 5. ADR 0047 notes the overlay drops on switch-back by design — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the note adds one clause the item text did not name — that the synthesized
EPHEMERAL start-up row does carry the overlaid key (`opts.APIKey`, `upstreamChoices`) — so the
"dropped on switch-back" claim is not read as covering the `--endpoint`/`APOGEE_ENDPOINT` override
case, where it is false. Verified against `cmd/apogee/upstream.go:291-301`.

NOTES (2026-08-14): the 2026-08-13 "Run residuals — open" section header was kept — removing this
item's bullet leaves the hold-off-cancel bullet (item 11's) in it, so the section is not empty.

**What:** Realise ratified call 1. Add a dated note to ADR 0047
(`docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md`, beside §6 or in
Consequences): switching `/server` away from the startup entry and back resolves the entry's
own configured source — the `APOGEE_API_KEY` overlay applies to the startup entry before
resolution only and is deliberately dropped on the switch back (owner-ratified 2026-08-14;
the code path is `sessionMover.move` → `m.keys.Resolve(entry)` at
`cmd/apogee/upstream.go:248` over picker rows built from `opts.Servers` verbatim). Remove the
corresponding ISSUES bullet (the 2026-08-13 section's `/server` bullet). No code change.

**Files:** `docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md`, `ISSUES.md`

**Tests:** none (doc only).

**Acceptance:** `grep -n "2026-08-14" "docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md"` shows the dated note.

**Commit:** `docs(adr): 0047 notes the key overlay drops on switch-back by design`

## 6. inputContentRows folds a bare CR as a row boundary — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the item's parenthetical "(and `\r\n` as one boundary, matching the widget)" is contradicted by the widget it names, so the item's own "verify the widget's exact behavior in the bubbles textarea before writing" instruction governed: the sanitizer rewrites EACH `'\r'` and EACH `'\n'` as one newline independently (`runeutil.go:68-76`), so `"a\r\nb"` is THREE widget rows, not two — confirmed empirically against a real `textarea` via the suite's own `widgetContentRows` oracle before writing. `"\r\n"` is therefore mirrored as two boundaries, and the new mirror-test cases (`a CRLF pair`) pin that against the widget rather than against the assumption.

NOTES (2026-08-14): the tests landed in `internal/tui/render_test.go`, not the `internal/tui/chromelayout_test.go` the item's Files line names — that file does not exist, and the "existing `inputContentRows` suite" the item's Tests line points at (`TestInputContentRows` plus the widget-oracle `TestInputContentRowsMirrorsTheWidget`) lives in `render_test.go`. Adding the cases beside their suite, and to both halves of it, was preferred over splitting two cases into a new file away from the oracle helper they need.

NOTES (2026-08-14): one clause of `sanitizeInputLine`'s doc comment (`internal/tui/inputaccent.go:296-301`) — "the callers split on `'\n'` for the same reason ([inputCellSpans], inputContentRows in chromelayout.go)" — is falsified by this change, so it was corrected in place to state each caller's actual split. `inputCellSpans` itself was NOT touched: it measures the widget's own value, which cannot carry a CR.

**What:** `inputContentRows` (`internal/tui/chromelayout.go:44`, the split at `:48`) splits
the value on `"\n"` only, while the widget it mirrors also folds a bare `"\r"` into a row
boundary — a value carrying one would size the prompt box a row short. Make the split treat a
bare `\r` (and `\r\n` as one boundary, matching the widget — verify the widget's exact
behavior in the bubbles textarea before writing) the same way the widget does. Pre-existing
width-mirror fidelity gap of the kind ADR 0030 tracks. Remove the corresponding ISSUES bullet
(ISSUES-sweep section).

**Files:** `internal/tui/chromelayout.go`, `internal/tui/chromelayout_test.go`, `ISSUES.md`

**Tests:** a case in the existing `inputContentRows` suite: a value carrying a bare `\r`
(and one with `\r\n`) counts the same rows the widget produces.

**Acceptance:** `go test ./internal/tui/`.

**Commit:** `fix(tui): inputContentRows folds a bare CR like the widget`

## 7. Companion test suites split beside their sources

**What:** The companion test suites were not split when their sources were:
`internal/tools/file_ops_test.go` (950 lines) still holds the `delete_file` tests that belong
beside `delete_file.go` — move them to `internal/tools/delete_file_test.go`; and
`internal/tools/path_safety_test.go` (425 lines) still holds the read-half tests that belong
beside `path_read.go` — move them to `internal/tools/path_read_test.go`. Pure moves: no test
logic changes, shared helpers stay where both halves can reach them (package-level). Both
originals should land at or under the coding-standards ~400-line threshold; if a shared
helper block makes that impossible, note the residual as a dated NOTES line rather than
restructuring helpers. Remove the corresponding ISSUES bullet (ISSUES-sweep section).

**Files:** `internal/tools/file_ops_test.go`, `internal/tools/delete_file_test.go`,
`internal/tools/path_safety_test.go`, `internal/tools/path_read_test.go`, `ISSUES.md`

**Tests:** the moved suites themselves — unchanged content, new homes.

**Acceptance:** `go test ./internal/tools/`; `wc -l internal/tools/file_ops_test.go internal/tools/path_safety_test.go` shows both reduced.

**Commit:** `refactor(tools): split companion test suites beside their sources`

## 8. Pin the setsid-escape teardown residual with a test

**What:** The setsid-escape residual is documented (`internal/tools/exec_teardown.go:37`,
`internal/tools/doc.go:183`, `internal/tools/exec_pgroup_unix.go:63`) but untested — nothing
asserts what a descendant that called `setsid`/`setpgid(0,0)` does across teardown. Add a
unix-only test (build-tagged like `exec_pgroup_unix.go`) that runs a command spawning a
`setsid`-detached descendant (e.g. via `sh -c 'setsid sleep 30 & echo $!'`, skipping with
`t.Skip` when `setsid` is not on PATH), triggers teardown, and asserts the DOCUMENTED
behavior — the escaped descendant survives the group kill — then kills and reaps the
straggler itself so the test leaks nothing. If the observed behavior contradicts the docs,
the test failing is the finding: report it, do not silently re-document. Remove the
corresponding ISSUES bullet (security-audit-fixes section — this empties that section; remove
its header too).

**Files:** `internal/tools/exec_teardown_unix_test.go` (new), `ISSUES.md`

**Tests:** the new test is the deliverable.

**Acceptance:** `go test ./internal/tools/ -run 'Setsid|Teardown'` passes (and the new test
is not skipped on this Linux host); `go vet ./internal/tools/`.

**Commit:** `test(tools): pin the setsid-escape teardown residual`

## 9. WriteMechanism distinguishes saved-but-apply-failed

**What:** `WriteMechanism`'s error-only signature conflates "the splice failed" with "the
splice landed and the live apply did not". Change the seam
(`internal/tui/tui.go:444`, contract doc at `:430-443`) to
`WriteMechanism func(id string, enabled bool) (saved bool, err error)`. The production wiring
(`cmd/apogee/wire_options.go:259-266`) already holds both halves separately: return
`false, err` when `config.SaveMechanismSetting` fails and `true, err` when the follow-up
`applySetting` fails. The consumer (`Model.settingsToggleMechanism`,
`internal/tui/settings.go:653-671`, error arm at `:660-664`) becomes a two-arm branch:
`saved && err != nil` prefixes `settingsApplyFailedNote` (`settings.go:1517`, "saved — live
apply failed: "), matching `settingsApplied`'s behavior at `:1342`; `!saved` keeps the plain
refusal. Amend the seam's contract doc ("treats the Mechanism as unchanged" now describes
only the `!saved` arm). Update the test doubles (`internal/tui/settings_test.go:2989`,
`:3153`) and add TUI test cases for both arms. Also add `cmd/apogee/wire_options_test.go`
pinning the closure chain (splice failure → `false, err`; splice landed but apply failed →
`true, err`) — the package currently has no test for this seam. Remove the corresponding
ISSUES bullet (settings-coverage section — this empties that section; remove its header too).

**Files:** `internal/tui/tui.go`, `internal/tui/settings.go`, `internal/tui/settings_test.go`,
`cmd/apogee/wire_options.go`, `cmd/apogee/wire_options_test.go` (new), `ISSUES.md`

**Tests:** TUI cases for both note arms; the new `wire_options_test.go` seam pins.

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/`; `go build ./...`.

**Commit:** `fix(tui): mechanisms row distinguishes saved-but-apply-failed`

## 10. Ephemeral endpoint alias avoids configured-name collision

**What:** Realise ratified call 3. The fallback `opts.HostAlias = hostFromEndpoint(...)`
(`internal/config/config.go:2118-2122`, `hostFromEndpoint` at `:2349-2358`) can equal a
configured entry's `name`, making two picker rows both draw `· current`
(`internal/tui/picker.go:1043-1045`) and letting name-keyed lookups (`findServer`
`cmd/apogee/upstream.go:320`, `configuredServer` `:334`) resolve the wrong row. Fix at
synthesis: when the fallback alias equals any configured `servers:` entry name
(case per the existing name-comparison semantics — match what `findServer` does), suffix it
`" (endpoint)"` — e.g. `workstation (endpoint)`. Single pass, no loop: a configured name that
already equals the suffixed form is an operator-armed corner, accepted as-is with a one-line
code comment. The explicit `--server-name`/configured-alias path (a name the user typed) is
NOT touched — only the synthesized fallback. Update the doc claim at
`cmd/apogee/upstream.go:287-290` to state the enforcement instead of asserting it. Remove the
corresponding ISSUES bullet (ISSUES-sweep section).

**Files:** `internal/config/config.go`, `cmd/apogee/upstream.go`,
`internal/config/config_test.go`, `ISSUES.md`

**Tests:** a config test: an ephemeral endpoint whose host equals a configured entry name
yields the suffixed alias; a non-colliding host stays bare.

**Acceptance:** `go test ./internal/config/ ./cmd/apogee/ ./internal/tui/`.

**Commit:** `fix(config): ephemeral endpoint alias avoids configured-name collision`

## 11. A cancel inside the restream hold-off stays resumable

**What:** Realise ratified call 2. In `respondAndReview` (`internal/agent/loop.go:361-388`),
the retryable-fault branch latches the restream and waits out the 1s hold-off
(`holdOffRestream(ctx)` at `:382`); when the wait ends because ctx was cancelled, execution
falls through to the failure exit (`:386-387`: `ErrorEvent` + `turnFailed` → `endAbandoned`).
Change the false arm to re-check `ctx.Err()` (or have `holdOffRestream` return the reason)
and, when it is cancellation, return `nil, turnCancelled, ""` WITHOUT emitting the
`ErrorEvent` — mirroring the existing guard at `:366-368`. `step`'s `turnCancelled →
endCancelled` arm (`:146-148`) and `turn.go` are unchanged. The `StreamResetEvent` already
emitted at `:381` is consistent with a rolled-back Turn. Update the deliberate-fall-through
comment at `loop.go:376-379` to the new rationale. Remove the corresponding ISSUES bullet
(the 2026-08-13 section's cancel bullet — with item 5 done this empties that section; remove
its header, and with it the last Open-defects section, leaving the Open defects heading with
no entries).

**Files:** `internal/agent/loop.go`, `internal/agent/loop_test.go`, `ISSUES.md`

**Tests:** an agent test: cancel during the hold-off → Turn ends `endCancelled`
(`StatusCancelled`, Exchange stays open, deferred queue restored, no `ErrorEvent` emitted);
the pre-existing non-cancel hold-off expiry path still restreams.

**Acceptance:** `go test ./internal/agent/`.

**Commit:** `fix(agent): a cancel inside the restream hold-off stays resumable`

---

**Suggested version bump:** micro (v0.14.x + 1) once executed — three user-visible fixes
(cancel semantics, mechanisms-row note, alias collision) plus doc/test hardening. Not
performed by this plan; the owner decides.
