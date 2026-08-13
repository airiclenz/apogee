# Hostile-bytes residuals plan

**Goal:** close the 13 open follow-ups of the hostile-bytes hardening run (ISSUES.md, "Hostile-bytes
hardening run — open follow-ups (2026-08-12)") plus the one test residual of the sub-agent
prompt-guard exemption run (ISSUES.md, "Sub-agent prompt-guard exemption run — residuals
(2026-08-13)", first bullet). Threat model unchanged: the operator is trusted, the bytes they
operate on are not, and neither is the model.

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host
**Skills:** coding-standards

**Authoritative sources:**

- `ISSUES.md` — the two sections named above; every citation there was re-verified against the
  working tree on 2026-08-13 and re-verified again by a read-only exploration pass the day this
  plan was written. If an item's line numbers have drifted, the ISSUES.md entry's description of
  the defect is the ground truth, not this plan's line numbers.
- `docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md` — the parent run whose
  items these residuals extend (items 6, 8, 13 are the precedents cited below).
- ADR 0012 (blast-radius invariant), `docs/design/confinement-execution-contract.md` §7
  (environment scoping), ADR 0026/0023 (system content), `internal/security/doc.go` (guard
  contract).

**Ratified design calls (owner, 2026-08-13, via AskUserQuestion at plan-write time):**

1. **Scope**: this plan covers the security-residuals set only. Excluded by owner triage the same
   day: the approved-out-of-workspace-write defect (pending owner call on contract §4), the
   `SetSampling` field-wise merge (deferred by owner 2026-08-13), and the three UX defects
   (clipboard, `/server`+`/model` autocomplete, prompts-to-files).
2. **Non-JSON `Arguments` blobs render as indented body rows** — every fallback line gets the same
   indent treatment a JSON value's body rows get, so no argument-derived line renders flush-left
   and none can masquerade as a labelled `Reason:`-style row. Content stays visible; no upstream
   provider-layer rejection.
3. **`go_vet`'s scope reaches the pane via a generic engine field** — a new optional `Scope string`
   on `domain.ApprovalRequest`, populated by the engine from a new optional tool-marker interface,
   rendered as one flattened line. No TUI special-casing of go_vet.
4. **`read_file`'s resolves-to disclosure is a result-string note** — the same
   ` → resolves to <path>` tail the writers use, appended to the read result when the resolved
   path differs. Mirrors ratified design call 2 of the parent run ("symlinked reads: follow, but
   show the resolution"); item 12 below records result strings as the wire-silence exception.
5. **Symlink-parent refusal covers every mutated chain** — `SafeRename` refuses symlinked parents
   on BOTH the old and the new path (both ends are mutated); `SafeRemove` refuses on the target's
   chain (the unlink lands where the symlink points); `SafeCopyFileFrom` refuses on the
   destination chain only — the source side is a read, which ratified design call 2 of the parent
   run says follows. `MoveFile.move` validates both chains up front and treats
   `ErrSymlinkedParent` as terminal (like `ErrPathEscape`), so the copy-then-remove fallback can
   never half-complete a move.

**Out of scope:**

- The approved-out-of-workspace-write Execute gap, the `SetSampling` merge, the three UX defects
  (see ratified call 1).
- Everything the parent plan's own Out-of-scope section excluded: operator-armed footguns, attacks
  presupposing the audited workspace is the apogee repo, the hostile-inference-endpoint set,
  gate-timing attacks.
- The other two prompt-guard residuals (doc-wording notes only — recorded in ISSUES.md as
  narrower-than-admitted item text, not defects).
- Any version identifier change (see the closing note).

**Standing requirements:**

- **Skills-folder access is a binding non-regression (owner, 2026-08-13).** The model's
  out-of-workspace read access to the skill source dirs flows through `Config.ExtraReadRoots`
  (wired from `skillProvider.SourceDirs` at `cmd/apogee/wire_boot.go:180` and
  `cmd/apogee/headless.go:360`) into `readScope` (`internal/tools/path_safety.go:176-266`), which
  `read_file`, `list_dir`, `grep`, `find_files` and `copy_file`'s SOURCE resolve over. No item of
  this plan may narrow that path: the symlink refusals (item 4) gate mutated chains only, never a
  `readScope` resolution; the disclosure notes (items 5, 6) add text, never a refusal. Items 4
  and 6 carry explicit extra-read-root tests for this.
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; the repo's full gate (`make check`) runs once, at closeout.
- ISSUES.md cleanup: each item's verifier removes that item's bullet from ISSUES.md as part of the
  item (the file's own contract — a resolved item is removed and recorded in the CHANGELOG).

---

## 1. `run_tests` stops inheriting apogee's credentials — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the spec-capture seam is a new package var `runTestsSubprocess = runSubprocess`
in `run_tests.go` (the shape `runPythonSubprocess` already uses), not the `shellHost` swap the item's
Tests line pointed at — `run_tests` does not route through `shellHost`, so that pattern could not
observe the spec it builds.
NOTES (2026-08-13): also corrected `internal/tools/doc.go:136`, a third comment the item did not
list, which described run_tests as running with "the inherited environment the toolchains need" —
now "…, minus apogee's own credentials".

**What:** `run_tests` is the one execution tool whose child inherits the parent environment whole:
the `subprocessSpec` literal at `internal/tools/run_tests.go:251-255` leaves `env` nil, so
`os/exec` hands a repo-authored test runner the full environment including `APOGEE_API_KEY` — the
credential `terminal` and `python_exec` deliberately withhold via `subprocessEnv()`
(`internal/tools/exec_common.go:80`, strip list `apogeeSecretEnvVars` at `:69`). Add
`env: subprocessEnv(),` to that spec literal. Update the two comments that assert the old
behaviour: `run_tests.go:247-250` (which documents the nil-env inheritance as deliberate) and the
`env` field doc at `exec_common.go:44-50` (which names run_tests as the inheriting caller).

**Files:** `internal/tools/run_tests.go`, `internal/tools/exec_common.go`,
`internal/tools/run_tests_test.go`

**Tests:** a unit test asserting the spec `run_tests` builds carries a non-nil env that excludes
`APOGEE_API_KEY` (follow the existing terminal-env test pattern — `shellHost` is test-swappable,
`internal/tools/terminal_test.go:228`).

**Acceptance:** `go test ./internal/tools/`

**Commit:** `fix(tools): run_tests no longer hands the test runner apogee credentials`

## 2. `terminal` and `python_exec` scope PATH away from the workspace — ✅ DONE (2026-08-13)

NOTES (2026-08-13): also amended `docs/design/confinement-execution-contract.md` (not in the item's Files list) — its 2026-08-12 amendment names `ScopeEnv`/git/Go as the whole of the child-side PATH scrub, which this item widens, so a new dated amendment paragraph records the `ScopeInheritedEnv` counterpart and why `run_tests` is excepted.
NOTES (2026-08-13): under the item's seam latitude, `interpreterVersion` gained a `workspaceRoot` parameter and its spec moved into a new `pythonVersionSpec` helper (the probe at the old `python_exec.go:110` needs the root to scope against, and a callable seam to assert its environment without launching an interpreter), and `terminal.go` gained a `runTerminalSubprocess` package var mirroring `runPythonSubprocess`/`runTestsSubprocess` so the shell tool's spec is capturable on every platform.

**What:** `terminal` (`internal/tools/terminal.go:98`) and `python_exec`
(`internal/tools/python_exec.go:110`, `:241`) inherit an unscoped `PATH` through
`subprocessEnv`, which strips only apogee's own credentials. PATH scoping — dropping the entries
that resolve inside the workspace, and non-absolute entries — landed for git
(`internal/tools/git.go:82`) and the Go toolchain (`internal/tools/diagnostics.go:308`) via
`shellHost.ScopeEnv`, but `ScopeEnv` is allowlist-based
(`internal/platform/platform.go:59`, impl `internal/platform/host.go:110`) and cannot express
"the whole environment inherited, with only PATH scoped" — and these two tools deliberately
inherit everything else (`exec_common.go:71-79`).

Binding behaviour: both tools keep inheriting the full environment minus credentials, but their
child's `PATH` drops entries that are non-absolute or resolve inside the workspace root — the
same per-entry rule `Host.ScopeEnv`'s PATH handling already applies. `python_exec`'s
`pythonSafePathVar` extra keeps its last-wins position. How the seam is shaped (a new `Host`
method, or the existing PATH-scoping logic extracted into a helper both paths share) is
implementer latitude under the forwarded coding standards; both tools already hold the package
`shellHost` var (`exec_common.go:288`) and their workspace root (`Terminal.root`,
`PythonExec.root`), so no new wiring crosses a package boundary.

**Files:** `internal/platform/platform.go`, `internal/platform/host.go`,
`internal/platform/host_test.go`, `internal/tools/exec_common.go`, `internal/tools/terminal.go`,
`internal/tools/python_exec.go`, `internal/tools/terminal_test.go`,
`internal/tools/python_exec_test.go`

**Tests:** a PATH entry inside the workspace root is dropped and one outside survives, for both
tools; non-absolute entries dropped; every non-PATH variable (other than the credential strip)
survives unchanged; `pythonSafePathVar` still appended last.

**Acceptance:** `go test ./internal/tools/ ./internal/platform/`

**Commit:** `fix(tools): terminal and python_exec scope PATH entries out of the workspace`

## 3. The ssh-key and credential rules learn the macOS home — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the two rules had no existing coverage in `rules_test.go` at all, so the item's
rows landed as a new `TestDefaultDangerousRules_HomeAnchoredRulesMatchTheMacOSHome` table rather
than as additions to the existing control-plane tables (whose stated scope is `.git/` and
`~/.apogee` only); it carries three near-miss rows, not one.

**What:** the `write-ssh-keys` (`internal/security/rules.go:53`) and
`write-credential-persistence` (`:63-64`) dangerous-action rules anchor on
`(?:~|/home/[^/\s]+|/root|\$home)` with no macOS alternative, so `/Users/<name>/.ssh/id_rsa` and
`/Users/<name>/.aws/credentials` never match — confirmed inert on the desktop persona. Insert the
`|/users/[^/\s]+` alternative after the `/home/[^/\s]+` one in both patterns, copying the shape
the newer `write-apogee-control-plane` rule already uses (`rules.go:102`). Lower-case `/users/`
is correct — `normalize` lower-cases the whole inspectable text (`internal/security/dangerous.go:294`).

**Files:** `internal/security/rules.go`, `internal/security/rules_test.go`

**Tests:** table rows asserting `writeCall("/Users/alice/.ssh/id_rsa")` and
`writeCall("/Users/alice/.aws/credentials")` now trip their rules (follow the macOS row pattern
at `rules_test.go:226`), plus a near-miss row that must stay unmatched.

**Acceptance:** `go test ./internal/security/`

**Commit:** `fix(security): ssh-key and credential rules match macOS home paths`

## 4. Rename, remove and copy refuse symlinked parents; the move fallback cannot half-complete — ✅ DONE (2026-08-13)

NOTES (2026-08-13): also amended `internal/security/doc.go` (not in the item's Files list) — its package map stated the parent-chain refusal as "applied by SafeWriteFile", which this item falsifies; it now names every chain a primitive here mutates and says the read chains are exempt.
NOTES (2026-08-13): design call 5's "validate both chains up front" lands inside `SafeRename` — it refuses both chains before its MkdirAll and rename — rather than as a second walk in `MoveFile.move`, which needs only the terminal `ErrSymlinkedParent` treatment: any OTHER rename error therefore already proves both chains cleared the gate, so the fallback can never half-complete, and `internal/security` gains no exported chain-validator it would otherwise need for one caller.

**What:** `SafeRename` (`internal/security/safeio.go:517`), `SafeRemove` (`:549`) and
`SafeCopyFileFrom` (`:403`) still follow symlinked parents inside the root:
`refuseSymlinkedParents` (`:177`) is applied by `SafeWriteFile` alone (`:120`), so `move_file` /
`delete_file` / `copy_file` keep the `docs → .git` redirection the write path closed. Apply
ratified design call 5:

- `SafeRename` refuses symlinked parents on BOTH `oldInput`'s and `newInput`'s chains;
- `SafeRemove` refuses on the target's chain;
- `SafeCopyFileFrom` refuses on the destination chain only (the source side is a read).

`MoveFile.move` (`internal/tools/file_ops.go:200-217`) must change in the same item: today it
early-returns only on `errors.Is(err, ErrPathEscape)` (`:205`), so a symlinked-parent refusal from
`SafeRename` would fall through to the `SafeCopyFile` + `SafeRemove` fallback — mis-worded at
best, and a split refusal (copy succeeds, remove refuses) would duplicate the file and
half-complete the move. Treat `ErrSymlinkedParent` (`safeio.go:158`) as terminal exactly like
`ErrPathEscape`, and validate both chains before attempting the rename so the fallback runs only
when both ends already passed the fence.

**Files:** `internal/security/safeio.go`, `internal/security/safeio_test.go`,
`internal/tools/file_ops.go`, `internal/tools/file_ops_test.go`

**Tests:** in `internal/security`: each of the three functions refuses a symlinked parent on each
chain design call 5 names, and still succeeds through clean chains; `SafeCopyFileFrom` still
follows a symlinked SOURCE parent (the read side). In `internal/tools`: a `move_file` whose
destination parent is a symlink refuses cleanly with the rename's wording, leaves the source in
place, and creates nothing at the redirect target; a genuine cross-directory move still works
(exercising the fallback path where the platform makes it reachable). **Skills-access guard
(standing requirement above):** a `copy_file` whose SOURCE lives under an extra read root
(a temp dir standing in for a skill source dir, mounted via `extraReadRoots` as
`internal/tools/file_ops.go:101` wires it) into a workspace destination still succeeds — including
when the source chain crosses a symlink.

**Acceptance:** `go test ./internal/security/ ./internal/tools/`

**Commit:** `fix(security): rename, remove and copy refuse symlinked parents on every mutated chain`

## 5. `copy_file`, `move_file` and `delete_file` disclose the resolved target — ✅ DONE (2026-08-13)

Depends on item 4 (same file: `internal/tools/file_ops.go`).

**What:** the result-string half of the parent run's item 8 reaches only four of the seven
workspace-scoped writers: `resolvedTargetNote` (`internal/tools/workspace_scoped.go:104`) is
called from `write_file.go:81`, `file_edit.go:90` and `find_replace.go:107,229`, while
`copy_file`, `move_file` and `delete_file` (`internal/tools/file_ops.go`) still echo the literal
argument in their success sentences for a write that landed somewhere else. Append
`resolvedTargetNote(<path>, root)` to each success sentence, following the `write_file.go:92-94`
pattern: for `copy_file` and `move_file` the note covers the destination argument; for
`delete_file` the removed target. (With item 4 landed, a symlinked PARENT no longer reaches
success on the mutated chains — the note still fires for a symlinked leaf or any other
resolution difference, and for copy's followed source side no note is added: the source is a
read, and `resolvedTargetNote` is the writers' disclosure.)

**Files:** `internal/tools/file_ops.go`, `internal/tools/file_ops_test.go`

**Tests:** for each of the three tools, a success whose resolved target differs from the argument
(e.g. a symlinked leaf) carries ` → resolves to <real>` in its result string, and an ordinary
success carries no note.

**Acceptance:** `go test ./internal/tools/`

**Commit:** `fix(tools): copy, move and delete disclose the resolved target in their results`

## 6. `read_file` discloses what a symlinked read resolved to — ✅ DONE (2026-08-13)

**What:** `read_file` carries no resolves-to disclosure: `internal/tools/read_file.go:94` returns
`okSummary` naming the literal argument, and the tool is not a `workspaceScopedWriter`, so none of
the parent run's plumbing reaches it — ratified design call 2 of that run ("symlinked reads:
follow, but show the resolution") is only half-landed. Per ratified design call 4 above, append
the same ` → resolves to <path>` tail to the read's result text when the resolved path differs
from the argument. `read_file` is in `package tools`, so `resolvedTargetNote` is directly
callable — but note the resolution root: the read goes through `t.scope.readBounded(args.Path)`
(`read_file.go:88`), whose scope may be a configured read-only root rather than `t.root`, so
resolve against the root the read actually used, not unconditionally against the workspace root.
The note lands appended to the rendered text (`renderFile` builds header + body, `:93`) so both
the model and the transcript see it.

**Files:** `internal/tools/read_file.go`, `internal/tools/read_file_test.go`

**Tests:** reading through a symlink yields a result carrying ` → resolves to <real>`; an ordinary
read carries no note; a read bounded by a configured read-only root resolves against that root
(`readScope.readRoot`, `internal/tools/path_safety.go:266`, already answers which root served the
read). **Skills-access guard (standing requirement above):** a `read_file` of a path under an
extra read root still succeeds and, when its resolution differs, carries the note — never a
refusal.

**Acceptance:** `go test ./internal/tools/`

**Commit:** `fix(tools): read_file discloses the resolved path of a symlinked read`

## 7. A non-JSON `Arguments` blob can no longer paint a labelled approval row — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the indent landed inside `prettyJSONDetails` rather than at the
`argumentDetails` call site — the item's conditional made the call-site placement contingent on the
helper having callers whose rows are not argument-derived, and it has exactly one caller
(`argumentDetails`), so every line it emits is argument bytes.
NOTES (2026-08-13): also amended `layout.md` (not in the item's Files list) — its approval-pane
spec stated unlabelled arguments as "shown exactly as they arrived", which this item qualifies;
the paragraph now records the indent and the column rule that makes it load-bearing.
NOTES (2026-08-13): `internal/tui/render_test.go` (not in the item's Files list) had one pinned
collapsed-block rendering of an unregistered tool's array blob whose two branch rows carried the
old flush-left text; its expected strings gained the indent (line count unchanged).

**What:** a non-JSON `Arguments` blob bypasses `orderedArgs`
(`internal/tui/toolpresent.go:2375`, which returns false for anything that is not a JSON object)
and falls through to `prettyJSONDetails` (`:2251`), which emits every line verbatim as an
unindented row (`:2252-2262`) — so a blob can still paint a forged `Reason:` row on the approval
pane, the very row the parent run's item 6 flattened the labelled path to stop. On the JSON path,
"labelled" means zero indent plus a trailing colon (`argumentDetails`, `:2307-2320`), and body
rows carry `argumentValueIndent` (`:2268`). Per ratified design call 2: on the fallback path
reached from `argumentDetails:2309`, prefix every emitted line with `argumentValueIndent` so no
argument-derived line renders flush-left. If `prettyJSONDetails` has other callers whose rows are
not argument-derived, indent at the `argumentDetails` call site rather than inside the helper —
the binding behaviour is about argument bytes, not about the helper.

**Files:** `internal/tui/toolpresent.go`, `internal/tui/toolpresent_test.go`

**Tests:** a non-JSON blob containing a line `Reason: forged` renders with every line indented by
`argumentValueIndent` (no flush-left row); the JSON-object path's labelled/body shape is
unchanged.

**Acceptance:** `go test ./internal/tui/`

**Commit:** `fix(tui): non-JSON argument blobs render as indented body rows only`

## 8. Popup titles fold newlines — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's Files list named `internal/tui/approval_test.go`, which did not exist
— created it (the package's other approval-pane tests live in `model_test.go`; the new file follows
the repo's file-per-source-file test layout and reuses that file's `approvalBodyRows` helper).

**What:** the approval pane title is `"Approve " + stripEscapes(req.Tool) + "?"`
(`internal/tui/approval.go:231`) with no `flattenField`, and `popupTitleLine`
(`internal/tui/popup.go:1245`) applies no sanitisation of its own — so a newline in a tool NAME
paints a second, unindented row above the pane's own body. Reachable via an MCP-supplied tool
name, which apogee does not author. Fix both layers: wrap the tool name in `flattenField`
(`internal/tui/transcript.go:1517` — same package) at the approval title site, and fold the title
inside `popupTitleLine` as the backstop for every popup (it already truncates to width; folding
first keeps the arithmetic on one line).

**Files:** `internal/tui/approval.go`, `internal/tui/popup.go`, `internal/tui/approval_test.go`,
`internal/tui/popup_test.go`

**Tests:** an approval request whose `Tool` contains `\n` renders a single title line; a popup
spec with a multi-line title renders a single title line.

**Acceptance:** `go test ./internal/tui/`

**Commit:** `fix(tui): popup and approval titles fold embedded newlines`

## 9. `go_vet`'s scope reaches the approval pane — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the CHANGELOG entry belongs under `### Added` in `[Unreleased]` (a new engine seam, matching the item's `feat(approval)` commit), not under the `### Fixed` heading this plan's earlier items used.
NOTES (2026-08-13): also updated `layout.md` (not in the item's Files list) — it is the TUI rendering spec and already specs the pane's `Reason:` and `→ resolves to` lines, so a new pane line left it stale; one paragraph added, no other change.
NOTES (2026-08-13): `vettedPackageLine` was split into a shared `vettedPackageScope` clause the marker and the result string both build on, rather than duplicating the derivation — the item's "deriving the same package-directory text" read as literally the same text. Only the verb tense differs (the pane speaks of a call about to run, the result of one that did).

**What:** `go_vet`'s package-directory scope is disclosed on the tool description
(`internal/tools/diagnostics.go:61`) and on both vet result strings (via `vettedPackageLine`,
`:326`) but NOT on the approval pane — the one surface the human actually decides on.
`domain.ApprovalRequest` (`internal/domain/approval.go:33`) carries no scope field, and tools do
not populate approval requests at all — the engine builds the request at its single construction
site (`internal/agent/dispatch.go:643-655`), reading optional tool markers (the
`internal/domain/tools.go` family) for tool-derived facts. Per ratified design call 3, build the
generic seam:

- a new optional marker interface in `internal/domain/tools.go` (alongside the existing family;
  name per coding standards, e.g. `ApprovalScoper`) through which a tool states a one-line,
  human-readable scope for a given call's arguments;
- a new `Scope string` field on `domain.ApprovalRequest` (additive — the type is root-aliased at
  `apogee.go:194`, so no breaking change), populated at `dispatch.go:643` when the tool implements
  the marker;
- `Diagnostics` implements the marker, deriving the same package-directory text
  `vettedPackageLine` derives from the call's arguments;
- the TUI renders one flattened `Scope: …` line in `approvalPrompt`'s `parts` slice
  (`internal/tui/approval.go:181-217`), styled and flattened like its `Reason:`/`Fix:` neighbours,
  omitted when empty.

**Files:** `internal/domain/approval.go`, `internal/domain/tools.go`,
`internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`, `internal/tools/diagnostics.go`,
`internal/tools/diagnostics_test.go`, `internal/tui/approval.go`, `internal/tui/approval_test.go`

**Tests:** engine: a tool implementing the marker yields an `ApprovalRequest` with its scope, one
that does not yields empty. tools: go_vet's marker text matches what `vettedPackageLine` derives
for the same arguments. tui: a request with `Scope` set renders one flattened `Scope:` line; with
it empty, no line.

**Acceptance:** `go test ./internal/domain/ ./internal/agent/ ./internal/tools/ ./internal/tui/`

**Commit:** `feat(approval): tool-declared scope rides the request and renders on the pane`

## 10. `/skills` reloads off the update goroutine — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's Files list named `internal/tui/skills_test.go`, which does not exist —
the package's `/skills` tests (and the `runSkillsNote` helper this item had to rework) live in
`internal/tui/skill_test.go`, so the new off-loop test landed there beside them rather than in a new
file that would split one subject's tests across two.
NOTES (2026-08-13): rather than duplicate the reload closure, `reloadSkillsCmd`'s body was generalised
in place (`internal/tui/autocomplete.go`) into `skillRescanCmd(done tea.Msg)` — one nil guard and one
by-value capture (ADR 0011) for both triggers — with `reloadSkillsCmd` kept as the menu's named
wrapper. `/skills` carries its own `skillsRescannedMsg` (implementer latitude per the item), because
the two folds owe different repaints: the menu's re-derives the dropdown, this one writes the note.
NOTES (2026-08-13): also corrected three comments this item's own change falsified, none in the item's
Files list — `internal/tui/commandrun.go`'s `"skills"` case ("Synchronous like /version"),
`internal/tui/tui.go`'s `Options.ReloadSkills` doc (which stated "/skills report still calls it
inline" as deliberate) and its neighbouring sentence about which message rides the scan's return.

**What:** `/skills` still re-walks the skill source dirs synchronously on the Bubble Tea update
goroutine: `runSkills` calls `m.opts.ReloadSkills()` inline (`internal/tui/skills.go:44-46`). The
parent run's item 12 moved the merged `/` menu's re-scan off that goroutine — `reloadSkillsCmd`
(`internal/tui/autocomplete.go:228`) runs the reload in a `tea.Cmd` goroutine and returns
`skillsReloadedMsg{}` (`:213`), folded at `internal/tui/model.go:869-874` — but left this trigger
on it, so the same render-loop block (ADR 0011) survives behind a different key. Rework
`runSkills` to the same shape: return a `tea.Cmd` that performs the reload off the update
goroutine and delivers a message; on fold, render the listing from the reloaded catalog. Whether
`/skills` reuses `skillsReloadedMsg` with a discriminator or carries its own message type is
implementer latitude — the binding behaviour is that no `/skills` invocation walks the filesystem
on the update goroutine, and the listing reflects the completed re-scan.

**Files:** `internal/tui/skills.go`, `internal/tui/skills_test.go`,
`internal/tui/autocomplete.go`, `internal/tui/model.go`

**Tests:** invoking `/skills` performs no reload call during `Update` itself (assert via an
instrumented `Options.ReloadSkills`) and produces a Cmd whose message, folded back, renders the
listing after the reload ran exactly once.

**Acceptance:** `go test ./internal/tui/`

**Commit:** `fix(tui): /skills re-scans off the update goroutine`

## 11. `promptEditor.reset` clears the skill-region edge trigger — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the edge-retrigger half of the item's Tests line landed in
`internal/tui/skill_test.go`, not in the item's `internal/tui/prompteditor_test.go` — it needs a
Model (`recomputeAutocomplete` is a Model method) and the reload-counting fixtures it asserts
through (`reloadOpts`, `runCmd`) live in skill_test.go beside the other edge-trigger tests, while
prompteditor_test.go's stated scope is editor-direct tests with no Model and no Update loop. The
`skillRegion`-after-`reset()` half landed in prompteditor_test.go as the item names.
NOTES (2026-08-13): also corrected the trailing comment at `internal/tui/model.go:1383` (not in the
item's Files list), which named `reset`'s effects as "empties the textarea and closes the overlay" —
this item's change gives it a third.

**What:** `promptEditor.reset()` (`internal/tui/prompteditor.go:223`) clears the textarea and the
autocomplete overlay but not `skillRegion` (`:61`), so submitting on an exact `/skill` token
leaves the edge-trigger true and the next `/` menu opens with no re-scan — listing a stale
catalog. `dismissAutocomplete` (`internal/tui/autocomplete.go:668-670`) clears both, which is the
shape `reset` should share: add `e.skillRegion = false` to `reset`.

**Files:** `internal/tui/prompteditor.go`, `internal/tui/prompteditor_test.go`

**Tests:** after `reset()`, `skillRegion` is false; a subsequent entry into a `/` region
re-triggers the re-scan edge.

**Acceptance:** `go test ./internal/tui/`

**Commit:** `fix(tui): prompt reset clears the skill-region edge trigger`

## 12. Two falsified comments tell the truth again — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the entry belongs under `### Changed` in `[Unreleased]`, following the
"Documented the dangerous-action guard's prompt-key exemption" precedent for a comment-only change,
not under the `### Fixed` heading this plan's behaviour items used.
NOTES (2026-08-13): this item closes TWO ISSUES.md bullets (the `events.go:122` one and the
`.xhtml` one).
NOTES (2026-08-13): the `events.go` rewording also names `read_file` beside the workspace-scoped
writers, because items 5 and 6 of this plan (both ✅ done) widened the note to it — a comment naming
only the writers would have been falsified the day it was written.

**What:** two doc comments the parent run's own fixes falsified, plus a phantom format:

- `internal/domain/events.go:122` still states the wire-silent invariant as "nothing is added to
  a tool's arguments or its result", which the parent run's item 8 falsified:
  `internal/tools/workspace_scoped.go:109` appends ` → resolves to <path>` to the result string
  of every write whose target differs from its argument (and items 5 and 6 of this plan widen
  that to the remaining writers and to `read_file`). The arguments half of the invariant is
  intact; reword the comment to name result-string disclosure notes as the deliberate exception.
- `internal/present/server.go:61` and `internal/present/server_test.go:180` both list `.xhtml`
  among what rung 2 shows, but `.xhtml` is in neither `browserRenderableExts`
  (`internal/tui/presenter.go:181`) nor `openerRenderableExts` (`internal/present/opener.go:297`).
  The parent run's item 4 dropped it from rung 1 without adding it to rung 2 — remove `.xhtml`
  from both comments (including the CSP rationale one of them carries) so they name only formats a
  rung serves.

Comment-only changes; no behaviour moves.

**Files:** `internal/domain/events.go`, `internal/present/server.go`,
`internal/present/server_test.go`

**Tests:** none (comment-only).

**Acceptance:** `go build ./... && go test ./internal/present/` — and the verifier confirms by
reading the three hunks that the new wording matches the shipped behaviour cited above.

**Commit:** `docs(code): wire-silence exception and rung-2 formats stated truthfully`

## 13. The prompt-guard union branch gets its pinning test — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the new test was mutation-checked against the branch it pins — dropping only
`sources` and only `prompts` in turn each makes it fail on the corresponding assertion — so it
cannot pass on a half-implemented union.

**What:** no test covers a tool declaring BOTH prompt keys and read-source keys — the union
branch in `Inspect` (`internal/security/dangerous.go:147-153`, which appends `prompts` and
`sources` into one `dropped` slice for the write-shaped view). Every `stubTool` in
`internal/security/dangerous_test.go` declares one class or neither (`:384` sourceKeys, `:423`
and `:470` promptKeys), and no shipped tool declares both today, so the branch is
correct-by-inspection but unpinned. Add a test using a both-classes stub —
`stubTool{name: "...", sourceKeys: []string{"source"}, promptKeys: []string{"task"}}`
(`stubTool` already carries both fields, `:305-310`) — pinning that the write-shaped view drops
BOTH classes: dangerous text under a prompt key and under a source key trips no `WritesOnly`
rule, while the same text under an ordinary argument still does.

**Files:** `internal/security/dangerous_test.go`

**Tests:** the item IS a test (see What).

**Acceptance:** `go test ./internal/security/`

**Commit:** `test(security): pin the prompt-and-source union branch of the guard`

---

**Suggested version bump:** one micro bump (`v0.13.11` → `v0.13.12`) at the run's close — the plan
ships a batch of hardening fixes plus one small approval-surface feature (item 9), which is
CHANGELOG-worthy under `[Unreleased]`. No item changes VERSION; the bump is the owner's call after
the run.
