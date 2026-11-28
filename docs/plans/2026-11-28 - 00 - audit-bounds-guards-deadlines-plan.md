# Bounded reads, guard siblings and slow-box deadlines — the remaining code-audit findings

**Goal:** the eleven non-PDF findings of the 2026-11-27 code audit are fixed: repo-controlled files are read bounded, two guard sibling gaps and the Adoption pane's hidden rule tail are closed, and five fixed deadlines no longer fail healthy runs on slow hardware.
**Date:** 2026-11-28
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** d0b401777

**Sources:**
- `docs/reviews/code-audit-2026-11-27.md` (non-PDF findings, recommended order 3–5)
- `.capcom/issues/<id>.md` for each id on the items' `**Closes:**` lines
- `AGENTS.md` (slow-box rule), `docs/design/test-drivers.md` §Waiting
- ADR 0026 (context files), ADR 0051 (write journal), ADR 0074 (snapshot undo), ADR 0056 (tree probe), ADR 0108 (git backstop)
- `docs/layout/user-questions-layout.md` §Adoption pane

**Ratified design calls:**
- **Deadline pattern:** each fixed deadline becomes a named slow-box backstop const ("backstop, not a budget" doc comment, as `gitStatusBackstop`), its expiry is said once, the toolchain probe retries, an in-Step undo capture is ended only by the store backstop or the user (the after-cancel host close keeps its 2-minute fence) (owner 2026-11-28; host-close exception writer, 2026-11-28).
- **rm -rf operands:** judge every operand of the same `rm` command; the scan stops at `; | & # < > { }`, a line break and an argument boundary, so `find … -exec rm -rf {} \;`, a later line and another argument never count (owner, 2026-11-28).
- **rm quotes:** a quoted string is one operand; a stray quote ends the scan (owner, 2026-11-28).
- **Adoption pane:** the full Allow rule is shown wrapped in the pane body; while any of it is elided the adopt answers are withheld (owner, 2026-11-28).
- **Context files:** a file over 10 MiB is refused with the existing unreadable notice, never truncated; under the cap nothing changes; ADR 0026 is amended (owner, 2026-11-28).
- **Project config:** symlinks are still followed (junction support); a non-regular target or one over 1 MiB skips the layer with one notice (writer).
- **write_file:** only write_file's pre-read and undo pre-image go through the 64 MiB `readCapped`; over it the diff is one inserted region and the result says the journal kept no copy; delete/move/copy keep their unbounded capture (writer, 2026-11-28).
- **Undo close errors:** only snapshot/diff/wait failures get the coverage sentence, with the cause kept; save errors keep today's text; the Step ctx is threaded through closeExchange (owner, 2026-11-28).
- **Stale index lock:** a killed staging `git add` is reported (the note names `.git/index.lock` when present), never deleted (writer).

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi: run tests only as one package and one `-run` pattern, prefixed `GOMEMLIMIT=2GiB`; never two test runs at once; never `./...`, never `-race`/`-cover` over a whole package.
- A test duration goes through `internal/testtime`; a production backstop a test needs is a package var or field, never a test-only literal.
- A changed decision record gets a dated amendment child Note (`adr-NNNN-amend-1128`) named on its Status line and reading-order list.

**Regression check (2026-11-28, d0b401777):**
- 1: guard folded (non-regular target costs a notice; the read itself is bounded).
- 3: recast — write_file's own capped path; delete/move/copy capture stays unbounded.
- 4: guard folded (family test reaches the launcher through a registry ctx).
- 5: recast — a segmented view for the two rm rules only; every other rule sees today's text.
- 6: guard folded (pane-owned `rule: ` lead; `apogee project adopt` keeps a nil body; adr-0104-amend-1128).
- 7: guard folded (refusal in acceptAdoption; unseated pane counts as elided; driven test parametrised); builds on item 6's layout text.
- 8: guard folded (retries in the probe goroutine; `Bridge.NotifyRuntime`; counter-file fake go).
- 9: recast — in-Step closes on the live Step ctx; host closes keep the 2-minute fence; ErrorEvent reworded.
- 10: guard folded (own-deadline expiries only; `ErrorEvent{Source:"workspace-check"}` at the dispatch.go callers); supersedes ADR 0056 Decision 4's 2 s snapshot timeout and silent skip.
- 11: guard folded (pin through a fake h.run; timed-out add says so; lock found at the .git dir); commitSecretsTimeout out of scope.
- 12: guard folded (zero retransmit fields mean none; Eventually-counted queries); supersedes ADR 0091 D1/D3's one query and one second.
- 3 (round 2): recast held — Approach rewritten to the capped write funnel used by WriteFile.Execute alone, note derived in Execute from its capped pre-read's ErrTooLarge plus `scope.journal != nil`.
- 5 (round 2): recast extended — quote-aware gap (a stray quote ends the scan), three quoted-text never-fire rows; Approach rewritten to the segmented per-rule view.
- 9 (round 2): recast extended — Step ctx threaded through closeExchange/exchangeClosed; only capture/diff/wait failures (undo sentinel) get the coverage sentence with the cause kept; save errors keep today's text; no notice event; supersedes turn.go's "no fire site carries a context" observer doc.
- 3 (round 2b): guards folded — one shared capture body keeps the pair two; ADR change as adr-0051-amend-1128 child Note named on the Status line and reading order (replaces the blockquote); every "capture is unbounded" doc comment names the write_file exception, found by grep.

**Out of scope:**
- The PDF findings (`docs/plans/2026-11-27 - 00 - pdf-parser-hardening-plan.md`), `untrusted-parsers-unfuzzed`, `unbounded-reads-unlinted`, `guard-sibling-cases-missed`, `windows-ci-partial-suite`.
- `internal/gitexec` `probeTimeout` (15 s command-config probe; not an audit finding).
- Changing `subprocess.DefaultSubprocessTimeout` / `MaxSubprocessTimeout` or the SIGKILL teardown.
- The /settings adopt path for a proposed Allow rule (`internal/tui/settingsapply.go`).
- `commitSecretsTimeout` (`internal/agent/secretsguard.go`, 30 s): the commit-secrets pre-check is not a git tool call.

## 1. Bound the Project config read

**What:**
**Goal:** `readProjectLayer` never opens a non-regular Project config target and never reads one larger than 1 MiB; either skips the layer with exactly one `projectSkippedNotice`, and every currently silent case except a non-regular target stays silent.
**Approach (assumed at the header base):** in `internal/config/layer.go` `readProjectLayer`, after `projectRealPath` and `sameConfigFile`, `os.Stat` the resolved target (symlinks still followed); refuse `!Mode().IsRegular()` (`not a regular file`) and `Size() > maxProjectConfigBytes` (new const `1 << 20`, `larger than 1 MiB`) through the existing notice, before the injected `readFile` runs. The `readFile` seam and its production callers are unchanged. The read sites (`layerProject`, `OverlayProjectOffer`, live re-read, daemon Firing) all pass through this function, so no caller changes.
**Regression guard.** A link to `/dev/null` loads silently today and costs one notice after: that is intended — the Goal's silent-case clause excepts a non-regular target, and the `/dev/null` test pins it. The stat check alone does not bound the read (a procfs file stats regular with size 0), so the Project layer's production read also opens the target and reads through `io.LimitReader(f, maxProjectConfigBytes+1)`, refusing (`larger than 1 MiB`) when more arrives; this overrides "the `readFile` seam … unchanged" where they conflict.
**Files:** internal/config/layer.go; internal/config/layer_test.go; internal/config/layer_unix_test.go; docs/manual/configuration.md
**Read first:** internal/config/layer.go — readProjectLayer, projectSkippedNotice, sameConfigFile, layerProject, OverlayProjectOffer; internal/config/layer_test.go — layerFixture, readProjectLayerNotices, assertOneSkippedNotice; internal/config/projectwrite.go — projectRealPath
**Tests:** `layer_unix_test.go` (`//go:build !windows`): a FIFO at `.apogee/config.yaml` and a symlink to `/dev/null` each give one skipped notice and return within `testtime.Budget`; a symlink to a size-0 regular file that yields more than 1 MiB (`/proc/self/pagemap`, skipped where absent) gives one notice; `layer_test.go`: a sparse file of `1<<20 + 1` bytes gives one notice and `readFile` is never called; the link-to-global cases stay silent.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadProjectLayer|TestLayeredConfig|TestOverlayProjectLayer' ./internal/config/`
- `go vet ./internal/config/`
**Commit:** `fix(config): refuse a non-regular or oversized Project config before reading it`
**Closes:** project-config-read-unbounded

## 2. Cap the context-file read at 10 MiB

**What:**
**Goal:** a context file over 10 MiB is not loaded and yields a `domain.ContextFileNote` with a `file too large` error, rendered by `notice.ContextFileNotices` as `context: <name> unreadable — …`; a FIFO at a context-file name never blocks loading; files under the cap load exactly as before.
**Approach (assumed at the header base):** `internal/agent/contextfiles.go` `readContextFile` keeps `security.SafeOpen` (already refuses FIFOs/devices) and reads through the same bounded read `(*Agent).readFileRef` does in `loop.go` (fstat size check, `io.LimitReader(f, maxRefFileBytes+1)`, re-check). Extract that sequence into one package-level helper in `internal/agent` called by both; both share one reason to change. Replace the "deliberately unbounded" comment. Amend ADR 0026 with `adr-0026-amend-1128` ("a memory-safety refusal above 10 MiB is not a context cap: oversize below it stays a warning, never truncated") and the context-files section of `docs/manual/configuration.md`.
**Files:** internal/agent/contextfiles.go; internal/agent/loop.go; internal/agent/contextfiles_test.go; internal/agent/contextfiles_unix_test.go; .capcom/knowledge/decisions/adr-0026.md; .capcom/knowledge/decisions/adr-0026/adr-0026-amend-1128.md; docs/manual/configuration.md
**Read first:** internal/agent/contextfiles.go — readContextFile, loadContextFiles; internal/agent/loop.go — (*Agent).readFileRef, maxRefFileBytes; internal/security/safeio.go — SafeOpen, ErrNotRegular; internal/agent/loop_cleanup_test.go — TestReadFileRefRefusesOversizeRef; internal/agent/contextfiles_test.go — TestLoadContextFiles; internal/notice/contextfiles.go — ContextFileNotices
**Tests:** a sparse AGENTS.md of `maxRefFileBytes+1` bytes gives one unreadable entry naming the size and the other files still load; `contextfiles_unix_test.go`: a FIFO named AGENTS.md returns within `testtime.Budget` as unreadable; the existing `readFileRef` tests still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestLoadContextFiles|TestContextFiles|TestReadFileRef' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run TestContextFileNotices ./internal/notice/`
- `capcom check`
**Commit:** `fix(agent): refuse a context file over 10 MiB instead of reading it whole`
**Closes:** context-files-read-unbounded

## 3. Cap write_file's reads of the existing file

**What:** Recast at the regression check (2026-11-28).
**Goal:** overwriting a file larger than 64 MiB with `write_file` reads at most the cap of it, still writes, journals no pre-image, and its result says so; files under the cap diff and journal as before.
**Approach (assumed at the header base):** in `internal/tools/write_target.go`, add a capped variant of the write funnel beside `writeTarget.write`, used by `(*WriteFile).Execute` alone, both sharing one unexported body that takes the pre-image reader: the capped variant's capture reads through `readCapped` and on `security.ErrTooLarge` journals nothing (ADR 0051 §4's unreadable path). `journaledTargets`' capture for delete/move/copy stays unbounded. Keep all capture logic inside `write_target.go` (`TestUndoCaptureHasExactlyTwoCallers`). In `internal/tools/write_file.go` `(*WriteFile).Execute`, the diff-region pre-read uses `target.readCapped()`; `ErrTooLarge` falls through to `okInsertedRegion`, and Execute derives the note from that capped pre-read's `ErrTooLarge` plus `scope.journal != nil`, appending the guard's note wording to `wrote %d bytes to %s`. Amend the `writeTarget.read` and `security.SafeReadFileCapped` doc comments to name the write_file exception (they keep saying the journal reads unbounded for the other verbs); record the change as an `adr-0051-amend-1128` child Note.
**Regression guard.** Cap only write_file's own path — a capped variant of the write funnel inside write_target.go used by WriteFile.Execute alone; journaledTargets' capture for delete/move/copy stays unbounded and the `writeTarget.read` / `SafeReadFileCapped` doc comments keep saying so for those verbs (amend them to name the write_file exception). Execute derives the note from its own capped pre-read's ErrTooLarge plus `scope.journal != nil` (no mark on the value-typed target). Note wording, true with or without snapshots: `; the previous content is over 64 MiB, so the write journal kept no copy of it — /undo can restore it only from the workspace snapshot`.
**Regression guard (capture pair, ADR, prose).** `writeTarget.write` and the capped variant share one unexported body that takes the pre-image reader, so `TestUndoCaptureHasExactlyTwoCallers` still sees two capture sites and ADR 0051 amend-0824/0916's "the pair is the whole of the package's capture" stays true. The ADR change lands as `adr-0051-amend-1128` (`capcom know add --id adr-0051-amend-1128 --parent adr-0051`), named on `adr-0051.md`'s Status line and its reading-order list — no blockquote. Every doc comment saying the journal capture is unbounded, or that the capped read never serves the journal, names the write_file exception; find them with `grep -n -i 'undo journal\|must cover' internal/security/safeio.go internal/tools/write_target.go` (incl. the safeio.go SCOPE note).
**Files:** internal/tools/write_file.go; internal/tools/write_target.go; internal/tools/write_file_unix_test.go; internal/security/safeio.go; .capcom/knowledge/decisions/adr-0051.md; .capcom/knowledge/decisions/adr-0051/adr-0051-amend-1128.md (new); docs/manual/commands.md
**Read first:** internal/tools/write_target.go — writeTarget.write, capturePreImage, readCapped, journaledTargets; internal/tools/write_file.go — (*WriteFile).Execute; internal/tools/undo_journal_test.go — TestUndoCaptureHasExactlyTwoCallers; internal/tools/file_edit_unix_test.go — plantOverCapFile; internal/security/safeio.go — SafeReadFileCapped
**Tests:** `write_file_unix_test.go`: overwrite a sparse `64<<20+1` file (`plantOverCapFile` shape) under a journal — the write lands, the journal has no entry for it, the result carries the guard's note wording, one inserted region is recorded; with no journal the result carries no note; an over-cap `delete_file` under a journal still journals its pre-image; an under-cap overwrite still journals (existing `TestWriteFunnelJournalsEveryContentVerb`). `TestUndoCaptureHasExactlyTwoCallers` passes with its `undoCaptureSites` and doc comment unchanged (the shared body keeps the capture calls two).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestWriteFile|TestWriteTarget|TestWriteFunnel|TestUndoCapture' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run TestSafeRead ./internal/security/`
**Commit:** `fix(tools): cap write_file's pre-read and undo pre-image at the edit-tool limit`
**Closes:** write-file-read-uncapped

## 4. Scrub a rule-answered console_open's environment

**What:**
**Goal:** every tool that declares an `ArgRoleShellCommand` argument starts its process without `CDPATH`, `BASH_ENV`, `ENV` and `BASH_FUNC_*` when `domain.RuleAnswered(ctx)`, and keeps them otherwise; one test enumerates those tools from the builtin registry.
**Approach (assumed at the header base):** in `internal/tools/console_open.go` `(*ConsoleOpen).Execute`, build the env from `t.host.subprocessEnvScopedPath(..., consoleTermVar)` into a local, apply `scrubRuleAnsweredEnv` under `domain.RuleAnswered(ctx)` exactly as `Terminal.Execute` does, and pass it as `console.OpenSpec.Env`. The family test iterates `builtinToolsWith(root, HostTools{}, h)` with `h.run` and `h.openConsole` capturing, keeps tools where `domain.ArgKeysWithRole(tool, domain.ArgRoleShellCommand)` is non-empty, fails if none match, and asserts the scrub for each under `WithRuleAnswered`.
**Regression guard.** The family test builds its ctx with `console.WithRegistry(..., console.New())` (as `consoleTestCtx`) under `WithRuleAnswered`, so console_open reaches its opener; it asserts each tool reached its launcher (captured `Argv` non-empty) before checking the scrub, and the fake opener returns `(nil, errors.New("not launched"))` — a nil console with a nil error panics in `consoleOpenTail`.
**Files:** internal/tools/console_open.go; internal/tools/console_open_test.go; internal/tools/shellenv_scrub_test.go
**Read first:** internal/tools/console_open.go — (*ConsoleOpen).Execute; internal/tools/terminal.go — (*Terminal).Execute; internal/tools/exec_common.go — scrubRuleAnsweredEnv, ruleAnsweredEnvVars; internal/tools/registry.go — builtinToolsWith; internal/tools/exec_host.go — subprocessEnvScopedPath; internal/tools/console_open_test.go — consoleTestCtx; internal/tools/terminal_test.go — TestTerminal_RuleAnsweredLineDropsEnvVectors
**Tests:** `TestConsoleOpen_RuleAnsweredLineDropsEnvVectors` (mirrors `TestTerminal_RuleAnsweredLineDropsEnvVectors`, `t.Setenv`, not parallel); `TestEveryShellCommandToolScrubsARuleAnsweredEnv` in `shellenv_scrub_test.go`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestConsoleOpen|TestTerminal_RuleAnswered|TestEveryShellCommandTool|TestScrubRuleAnsweredEnv' ./internal/tools/`
**Commit:** `fix(tools): scrub the shell-startup env of a rule-answered console_open`
**Closes:** console-open-env-unscrubbed

## 5. Judge every operand of an rm -rf

**What:** Recast at the regression check (2026-11-28).
**Goal:** `rm-rf-root-home-system` and `rm-fr-root-home-system` hard-refuse a protected target in any operand position of the same `rm` command (`rm -rf build /etc`, `rm -rf ./out /`, `rm -rf a b ~`), and never fire on `find . -exec rm -rf {} \;`, `rm -rf build > /dev/null`, `rm -rf build 2>/dev/null`, `rm -rf ./out # /`, `rm -rf build && ls /etc`, a line break followed by an absolute path, or a `terminal` call whose other argument is an absolute path.
**Approach (assumed at the header base):** in `internal/security/rules.go` insert the gap `(?:(?:"[^"]*"|'[^']*'|[^\s;|&#<>{}"'])+\s+)*?` ahead of `quoteOpen + deleteTargetAnchor` in both rm patterns, so a quoted string is one operand and a stray quote ends the scan. In `internal/security/dangerous.go`, the two rm rules alone inspect a segmented view of the call, computed once per inspection and selected by a per-rule engine-internal flag beside `ShellWriteView` (the view is defined in the guard paragraph), so neither a later line nor another argument can be read as an operand. `normalize` and `collectArgs` are unchanged, and every other rule sees exactly today's text. Rules stay pure regex (`domain.DangerousRule`).
**Regression guard.** Do NOT change `normalize` or `collectArgs` for all rules. Only the two rm rules see a segmented view: line breaks become ` ; ` except after a backslash continuation (folded to one space before the `\`→`/` fold) or after a trailing `| & ; { (` (folded to a space), and ` ; ` is written between top-level argument values only (one space between leaves of one value, never after the last). Mechanism: a per-rule engine-internal view flag beside `ShellWriteView` on `domain.DangerousRule` (not a config key a user rule can set — verify how ShellWriteView is set and mirror it), computed once per inspection. Every other shipped and user-merged rule sees exactly today's text, so rules.go's remote-pipe-to-shell comment and configuration.md's user-rule text description stay true. Add never-fire rows `rm -rf build \<NL> dist`, a two-argument terminal call, and keep a Tier1 row for `{"argv":["rm","-rf","/"]}` on an unknown tool plus `curl … |<NL> sh` and the multi-line fork bomb as unchanged-behaviour rows. Add internal/domain/config.go to Files. Use the gap `(?:(?:"[^"]*"|'[^']*'|[^\s;|&#<>{}"'])+\s+)*?` so a stray quote ends the scan; add never-fire rows `grep -rn "rm -rf build" /home/u/proj/scripts`, `sed -i 's/rm -rf build/rm -rf dist/' /home/u/proj/Makefile`, `git log -S "rm -rf dist" -- /work/repo`; keep refusing `rm -rf "my dir" /etc` and `rm -rf 'a' "b" "/etc"`.
**Files:** internal/security/rules.go; internal/security/dangerous.go; internal/security/dangerous_test.go; internal/security/rules_test.go; internal/domain/config.go; docs/manual/configuration.md
**Read first:** internal/security/dangerous.go — Inspect, Rule.ShellWriteView, collectArgs; internal/security/rules.go — DefaultDangerousRules, deleteTargetAnchor, quoteOpen; internal/domain/config.go — DangerousRule; internal/config/config.go — the dangerous-rules projection (rules from a config file never set the view flags)
**Tests:** extend `TestDangerousActionGuard_Tier1HardRefuse` with first/middle/last operand rows, `rm -rf "my dir" /etc`, `rm -rf 'a' "b" "/etc"`, the `{"argv":["rm","-rf","/"]}` unknown-tool row and a multi-line fork bomb, and `TestDangerousActionGuard_PrecisionNearMissesNotBlocked` with every never-fire case in the Goal plus `rm -rf build \<NL> dist`, `grep -rn "rm -rf build" /home/u/proj/scripts`, `sed -i 's/rm -rf build/rm -rf dist/' /home/u/proj/Makefile` and `git log -S "rm -rf dist" -- /work/repo`; `TestDefaultDangerousRules_RemotePipeToShellCatchesAnyPipelineStage` gains a `curl … |<NL> sh` row; a two-argument `terminal` call (`command` + an absolute-path argument) run 50 times never fires; every existing row still passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDangerousActionGuard|TestDefaultDangerousRules|TestInspect|TestMergeDangerousRules' ./internal/security/`
**Commit:** `fix(security): refuse a protected rm -rf target in any operand position`
**Closes:** rm-rf-later-operand

## 6. Show a project Allow rule whole in the Adoption pane body

**What:**
**Goal:** an Allow rule's Adoption pane carries the full rule text, word-wrapped to the pane's inner width, as its body; the layout spec says so.
**Approach (assumed at the header base):** `internal/tui/adoptionbody.go` `adoptionBody` stops returning `""` for a non-structured head: it renders the flattened rule (`flatText(p.Text)`) through `wrapBodyLine(m.th, line, popupInnerWidth(m.th, m.slotWidth()))`, the machinery the server/reaction bodies use. The title stays as is (cut to width). Amend `docs/layout/user-questions-layout.md` §Adoption pane (an Allow rule's pane now has a body; the unpinned-line note) and the doc comments in `adoption.go` and `adoptionbody.go` that say it has none.
**Regression guard.** The rule line carries a pane-owned lead at column zero (`rule: `) and hangs its text `adoptionHang` in (`bodyLine{lead, text, hang: adoptionHang}`), so the pane's "no line that starts with the file's text starts at column zero" invariant holds. `AdoptionQuestion` keeps its nil body for an Allow rule (`apogee project adopt` prints the title whole; `printProjectQuestion` unchanged). This supersedes adr-0104-amend-1113's "An Allow rule's pane has no body" through a dated `adr-0104-amend-1128`, named on ADR 0104's Status line and reading order.
**Files:** internal/tui/adoptionbody.go; internal/tui/adoption.go; internal/tui/adoptionbody_test.go; docs/layout/user-questions-layout.md; .capcom/knowledge/decisions/adr-0104.md; .capcom/knowledge/decisions/adr-0104/adr-0104-amend-1128.md
**Read first:** internal/tui/adoptionbody.go — adoptionBody, wrapBodyLine, bodyLine, AdoptionQuestion; internal/tui/adoption.go — isStructured, adoptionNotice, flatText; internal/tui/adoptionbody_test.go — adoptionFrame, paneInteriors, TestAdoptionPaneNeverStartsTheFilesTextAtColumnZero; cmd/apogee/project_cmd.go — printProjectQuestion; docs/layout/user-questions-layout.md — §Adoption pane
**Tests:** recast `TestAdoptionPaneBodyIsOnlyAStructuredEntrys` (an Allow rule now has a body; pane rows title, body, adopt row); extend `TestAdoptionPaneNeverStartsTheFilesTextAtColumnZero` to an Allow rule; new `TestAdoptionPaneShowsALongAllowRuleWhole` at 60 columns: every word of a 200-character rule's tail appears inside the pane box below its title row (`paneInteriors` minus the title), not merely in the frame (the transcript notice already prints it whole).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestAdoption' ./internal/tui/`
- `grep -n 'rule: ' docs/layout/user-questions-layout.md` finds the Allow rule's body line in §Adoption pane
- `capcom check`
**Commit:** `fix(tui): show a project Allow rule whole in the Adoption pane body`

## 7. Withhold adoption while the rule is elided

**What:** Depends on item 6.
**Goal:** while any line of an Allow rule's Adoption body is elided (short terminal), the adopt answers cannot be triggered by key or click, the pane hint reads `enlarge the window to read the whole rule before adopting`, and "not now" still works; once the window grows, adoption is offered again.
**Approach (assumed at the header base):** the list surface already counts elided body lines (`maxBodyRows`, the `… (+N more lines)` marker); expose that count to the adoption answer path (`pickerOfferings[pickerAdoption]` / the adoption answer handler in `internal/tui/adoption.go`) and refuse the adopt answers while it is non-zero for an Allow-rule head. `TestAdoptionPaneKeepsItsAnswersOnAShortTerminal` keeps the answers visible (answers beat body).
**Regression guard.** Item 7 builds on item 6's layout-doc text (Depends on item 6 already stated). The /settings adopt path (settingsapply.go) is out of scope: add "the /settings adopt path for a proposed Allow rule (`internal/tui/settingsapply.go`)" to the header's Out of scope list. The refusal sits in `acceptAdoption`, which both ⏎ and a second click reach, and keeps the pane open. A pane `listSpec` does not seat (below the row floor) counts as elided: the count comes from the same `listSpec` + `popupBodyLineSpan` the painter uses, computed without re-entering `pickerListContent`. `docs/manual/configuration.md` gains one sentence on the withheld adopt and its hint.
**Files:** internal/tui/adoption.go; internal/tui/picker.go; internal/tui/listsurface.go; internal/tui/adoption_test.go; cmd/apogee/e2e_approval_test.go; docs/layout/user-questions-layout.md; docs/manual/configuration.md
**Read first:** internal/tui/adoption.go — acceptAdoption, raiseAdoption, adoptionRows; internal/tui/picker.go — pickerListContent, pickerKey, pickerOfferings[pickerAdoption]; internal/tui/listsurface.go — listSpec; internal/tui/popup.go — popupBodyLineSpan; internal/tui/mouse.go — handlePickerClick; cmd/apogee/e2e_approval_test.go — answerAdoptionArmed, assertAdoptionRecord, adoptionTitleMarker
**Tests:** `adoption_test.go`: at 80x12 with a long rule, the adopt key leaves the rule unadopted and the hint shows; two clicks on the adopt row leave it unadopted too; a window too short to seat the pane refuses the adopt; after a resize to 80x40 the same key adopts. Driven: `TestE2EAdoptionShowsALongRuleWholeAtNarrowWidth` in `cmd/apogee/e2e_approval_test.go` (`tuitest.NewDriver`, `writeProjectAllow`, `narrowSize`): `answerAdoptionArmed` and `assertAdoptionRecord` take the pane marker (the adopt gloss `runs without asking here`, which the cut title may lose) and the rule as parameters; the test sends `tuitest.Up` before the armed ⏎ (the pane opens on "not now") and finds the rule's tail inside the pane's box rows before adopting it.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestAdoption' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestE2EAdoption' ./cmd/apogee/`
**Commit:** `fix(tui): withhold adopting an Allow rule while part of it is hidden`
**Closes:** adoption-rule-title-truncated

## 8. Give the toolchain probe a backstop and retries

**What:**
**Goal:** a slow `go env GOROOT GOMODCACHE` no longer drops the read roots for the run: one attempt is bounded by a 300 s backstop, a timed-out attempt is retried (at most three attempts in all), and a final give-up is said once to the user.
**Approach (assumed at the header base):** `cmd/apogee/toolchain_roots.go`: `toolchainProbeTimeout` becomes `toolchainProbeBackstop = 300*time.Second` (doc comment: backstop, not a budget, like `vetTimeout`); `probeToolchainRoots` tells a `TimedOut` result apart from "no go". `toolchainLibrary` re-launches a background probe from `roots()` after a timed-out attempt; `wait()` still returns at the first final answer (success, non-timeout failure, or the third timeout). The give-up line `toolchain: go env did not answer in three attempts; GOROOT and GOMODCACHE are not readable this run` goes through a sink injected at `start` (`wire_boot.go`, `wire_firing.go`): stderr for headless/daemon, the TUI's runtime notice for the TUI. Reword "once at start-up" in `docs/manual/commands.md`.
**Regression guard.** The TUI give-up line goes through a new `tui.Bridge.NotifyRuntime(text string)` that adds a transcript note (bridge.go + its msg + the model fold; add those files to Files); the sink is a closure that reads `w.bridge` late, so `start` may stay where it is. Headless/daemon: stderr. The up-to-three attempts run in the probe goroutine `start()` launches; `roots()` never launches a probe (`apogee probe context` reaches only `wait()`), and the item states that probe context may now wait up to 3×300 s. `toolchainProbeBackstop` is a package var. Every comment or doc claiming the probe runs once or is silent is reworded: `grep -rn -i "once\|silent" cmd/apogee/toolchain_roots.go cmd/apogee/wire_boot.go cmd/apogee/wire_firing.go docs/manual/commands.md`.
**Files:** cmd/apogee/toolchain_roots.go; cmd/apogee/toolchain_roots_test.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_firing.go; internal/tui/bridge.go; internal/tui/messages.go; internal/tui/model.go; docs/manual/commands.md
**Read first:** cmd/apogee/toolchain_roots.go — toolchainLibrary.start, toolchainLibrary.roots, toolchainLibrary.wait, probeToolchainRoots; cmd/apogee/probecontext.go — probeContextConfig; cmd/apogee/wire_boot.go — newRootWiringOn; cmd/apogee/wire_firing.go — bindFiringConfig; internal/tui/bridge.go — NotifyHook, programRef.send; cmd/apogee/toolchain_roots_test.go — installFakeGoIn, TestToolchainLibraryProbesOnceAndAnswersLive
**Tests:** with `toolchainProbeBackstop` set to `testtime.Budget(2*time.Second)` (not a tight fixed clock) and an `installFakeGo` variant keeping a counter file whose sleeping call runs `sleep 3600` (the group teardown kills it): the second attempt's roots appear; three sleeping attempts give exactly one give-up line; `wait()` alone (no `roots()` read) returns after three timed-out attempts; `TestToolchainLibraryProbesOnceAndAnswersLive` still holds for a healthy probe.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestProbeToolchainRoots|TestToolchainLibrary' ./cmd/apogee/`
**Commit:** `fix(apogee): retry a timed-out toolchain probe under a slow-box backstop`
**Closes:** toolchain-probe-fixed-deadline

## 9. Let the Exchange-close undo capture finish or be cancelled

**What:** Recast at the regression check (2026-11-28).
**Goal:** an in-Step Exchange close (final reply, fault, step cap) has no deadline but the snapshot store's own and ends when the Step ctx is cancelled; a host close (`SettleExchange`, `AbortExchange`) keeps the 2-minute `undoCloseTimeout` fence; a capture, diff or wait failure is `domain.ErrorEvent{Source:"undo"}` reading `undo: this Exchange's workspace snapshot did not complete — /undo covers only the edits apogee's file tools made: <cause>`, and a journal save error keeps today's text.
**Approach (assumed at the header base):** in `internal/undo/snapshot.go`, `closeGroup` and `awaitWalk` wrap capture, diff and wait failures in an undo sentinel; a save error is not wrapped. In `internal/agent/turn.go`, `closeExchange(ctx)` and `exchangeObserver.exchangeClosed(ctx)` take a ctx (the Agent's `exchangeClosed` in `internal/agent/construct.go` hands it to `closeUndoGroup`); `end()` passes the Step ctx from its `internal/agent/loop.go` call sites and from `finishAtStepCap` in `agent.go`, while `abort()` and `settle()` pass the bounded background ctx (`undoCloseTimeout`). `closeUndoGroup` runs `journal.Close` under the ctx it is handed and, on the sentinel (`errors.Is`), emits the coverage sentence with the cause kept; any other error keeps today's text. Reword turn.go's observer doc ("no fire site carries a context") and the `closeUndoGroup` doc comment.
**Regression guard.** Closes that run inside Step (final reply, fault, step cap) use the live Step ctx with no outer fence — the store's `workTreeBackstop` bounds them and Esc ends them. Host closes (`SettleExchange`, `AbortExchange`, run on the TUI Update goroutine after a cancel) keep today's bounded background ctx at the 2-minute `undoCloseTimeout`, documented in its doc comment as the one remaining fence because a longer wait would freeze the UI loop. No new domain event: the closing-image failure stays `domain.ErrorEvent{Source:"undo"}`; MarkPre's pre-image report and its subtests stay unchanged. This supersedes, for the in-Step closes only, `closeUndoGroup`'s doc comment (agent.go) choosing the background ctx "rather than the Step's", and turn.go's observer doc saying "no fire site carries a context or an Agent to hand one".
**Files:** internal/agent/agent.go; internal/agent/turn.go; internal/agent/loop.go; internal/agent/construct.go; internal/agent/undo_group_test.go; internal/agent/turn_test.go; internal/undo/snapshot.go; internal/undo/snapshot_test.go (if the sentinel test lands there)
**Read first:** internal/agent/agent.go — closeUndoGroup, undoCloseTimeout, finishAtStepCap; internal/agent/turn.go — turnLifecycle.end, closeExchange, exchangeObserver; internal/undo/snapshot.go — closeGroup; internal/agent/undo_group_test.go — TestACaptureFailureIsReportedAndNeverFailsTheExchange
**Tests:** recast only the closing-image subtest of `TestACaptureFailureIsReportedAndNeverFailsTheExchange` for the reworded `ErrorEvent{Source:"undo"}` (coverage sentence plus cause); its pre-image subtest and `TestAFailedCaptureIsReportedOncePerExchange` stay as they are; a failing journal save after a complete snapshot keeps today's `undo: save the journal index …` text; `closeGroup`/`awaitWalk` failures match the undo sentinel with `errors.Is` and a save error does not; new: a fake snapshotter whose Capture blocks on a channel and records `ctx.Deadline()` — an in-Step close sees no deadline, completes once released and is journaled; a Step-ctx cancel during the hold yields the reworded error; a host close (`SettleExchange`) still sees the 2-minute deadline. The `end(…)`/`closeExchange()` calls in `undo_group_test.go` and `turn_test.go`, and `countingObserver`, take the new ctx parameter.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestACapture|TestAFailedCapture|TestExchangeEndClosesTheGroup|TestUndoClose' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestTurnEnd|TestTurnLifecycle|TestOpenExchange' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestClose|TestMarkPre' ./internal/undo/`
**Commit:** `fix(agent): bound the Exchange-close undo capture by the store backstop and the user`
**Closes:** undo-capture-fixed-deadline

## 10. Size the tree snapshot for a slow box and say when it is skipped

**What:**
**Goal:** a tree snapshot is bounded by a 30 s backstop; a snapshot that expires is announced to the user once per session (across sub-agents), and three expiries latch the check off for the session; the model-facing mutation warning is unchanged when the snapshot completes.
**Approach (assumed at the header base):** `internal/agent/treesnapshot.go`: `treeSnapshotTimeout` becomes `treeSnapshotBackstop = 30*time.Second` (as `treeProbeTimeout`), a package var for tests; the expiry count and the once-per-session flag live in the Agent's shared handles so sub-agents and the background host share them (each `newAgent` today builds its own snapshotter). The notice uses the same event as item 9: `workspace-change check skipped: git status did not answer in 30 s`. Latching mirrors `treeProbeTransientLimit`.
**Regression guard.** No new domain event: the once-per-session skip is a `domain.ErrorEvent{Source:"workspace-check"}` (the undo report's shape — it never fails the Exchange or changes a Driver's exit status), emitted at the dispatch.go callers of beforeCall/mutationWarning; add internal/agent/dispatch.go to Files; the test is a new timeout test, not the overrun one. An expiry counts only when the status run's own deadline fired (`DeadlineExceeded` with the call's ctx not done); a cancel, an output-cap overrun (`errTreeStatusTooLarge`) and a git error are not expiries, and `snapshot()` reports the expiry to its caller. The shared count lives outside the snapshotter `withEngineGit` swaps. This supersedes ADR 0056 Decision 4's and adr-0056-amend-1103's "2 s timeout and silent skip" through `adr-0056-amend-1128`; secretsguard.go's `treeSnapshotTimeout` mention follows the rename.
**Files:** internal/agent/treesnapshot.go; internal/agent/construct.go; internal/agent/dispatch.go; internal/agent/secretsguard.go; internal/agent/treesnapshot_test.go; .capcom/knowledge/decisions/adr-0056.md; .capcom/knowledge/decisions/adr-0056/adr-0056-amend-1128.md
**Read first:** internal/agent/treesnapshot.go — treeSnapshotter.snapshot, statusListing, runProbe, noteUnansweredProbe; internal/agent/dispatch.go — beforeCall/mutationWarning call sites; internal/agent/agent.go — sharedHandles, tree; internal/agent/construct.go — newAgent (tree, gitHost); internal/agent/background.go — backgroundHost (tree: a.tree); internal/agent/treesnapshot_test.go — scriptedGit, withEngineGit, TestTreeSnapshot_TimedOutProbeIsRetried
**Tests:** a new timeout test (not `TestTreeSnapshot_OverrunListingSkipsTheCheck`, which stays the output-cap case) where `scriptedGit` returns `SubprocessResult{TimedOut:true, ExitCode:-1}` for status: one `ErrorEvent{Source:"workspace-check"}` for two expiries across a parent and a delegate (`withEngineGit` applied to the child); three expiries latch off; a cancelled call ctx counts no expiry and emits nothing; a completed snapshot still yields `[warning: this command changed workspace files: …]`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestTreeSnapshot' ./internal/agent/`
- `capcom check`
**Commit:** `fix(agent): give the tree snapshot a slow-box backstop and announce a skip`
**Closes:** tree-snapshot-fixed-deadline

## 11. Replace the git tool timeouts with one backstop

**What:**
**Goal:** every git tool call (reads, writes, staging in `move_file`/`delete_file`) is bounded by one 10-minute backstop; a staging add that times out or is cancelled says so, and names `.git/index.lock` when that file exists afterwards; apogee never deletes it.
**Approach (assumed at the header base):** `internal/tools/git.go`: `gitTimeout` and `gitDiffTimeout` collapse into `gitToolBackstop = 10*time.Minute` (doc comment cites `gitStatusBackstop`); `gitReadTimeout` goes. `internal/tools/git_stage.go` `stageGitPaths`: after a `TimedOut` or ctx-error add, `stagingSkipped` appends `; .git/index.lock was left behind — remove it once no git process is running` when the lock exists. Update `docs/manual/commands.md` (the 15/30 s sentence).
**Regression guard.** `commitSecretsTimeout` (internal/agent/secretsguard.go, 30 s) is not a git tool call: add it to the header's Out of scope list. On `add.TimedOut` the reason reads `git add timed out after <gitToolBackstop>` before the lock suffix. The lock is looked for in the directory holding `.git`, walking up from the root (a workspace may be a subdirectory), named relative to the root; where `.git` is a file (a linked worktree) the lock clause is skipped. `gitReadCall.verb`'s doc stops naming `gitReadTimeout`. The item's changelog entry says it supersedes the 30 s diff/show ceiling (CHANGELOG.md, which stays as written).
**Files:** internal/tools/git.go; internal/tools/git_stage.go; internal/tools/git_stage_test.go; internal/tools/git_test.go; docs/manual/commands.md
**Read first:** internal/tools/git.go — runGit, gitRead, gitWrite, gitReadTimeout, gitReadCall; internal/tools/git_stage.go — stageGitPaths, stagingSkipped; internal/tools/git_test.go — TestGitReadTimeoutGivesDiffAndShowAtLeastTheOrdinaryBudget, TestGitStatus_LaunchesThroughTheHostRun; internal/tools/git_stage_test.go — TestStageGitPaths_SkippedNoteKeepsOneLine; internal/tools/exec_host.go — execHost.git; internal/subprocess/subprocess.go — RunSubprocess; cmd/apogee/gitstatus_host.go — gitStatusBackstop
**Tests:** `TestStageGitPaths_TimedOutAddNamesAStaleIndexLock` (fake `h.run` answers SplitStdout probes empty, `ls-files` exit 0, `add` `TimedOut`; the test plants `.git/index.lock`) asserts both the `timed out after` reason and the lock suffix; a workspace in a repository subdirectory finds the lock at the repository's `.git`; `TestStageGitPaths_SkippedNoteKeepsOneLine` still passes; delete `TestGitReadTimeoutGivesDiffAndShowAtLeastTheOrdinaryBudget` and pass `gitToolBackstop` at the two `runGitUnchecked` sites in `git_test.go`; a test through a fake `h.run` (the `TestGitStatus_LaunchesThroughTheHostRun` pattern) asserts every non-SplitStdout `SubprocessSpec.Timeout` a git tool and `stageGitPaths` launch equals `gitToolBackstop`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestStageGitPaths|TestGit' ./internal/tools/`
**Commit:** `fix(tools): bound git tool calls by a slow-box backstop and report a stale index lock`
**Closes:** git-tool-fixed-deadline

## 12. Retransmit the mDNS query

**What:**
**Goal:** a `.local` lookup sends its query at 0, 250 ms and 750 ms within a 3 s window (cut short by ctx) and returns the first matching answer, so a responder that answers only the second or third query is found.
**Approach (assumed at the header base):** `internal/mdns/mdns.go`: `lookupTimeout` becomes 3 s; `resolver.lookup` keeps one socket and re-sends the query on a timer at the two retransmit offsets (fields on `resolver` so tests can shrink them) while the read loop runs; the deadline and `context.AfterFunc` handling are unchanged. Update `docs/manual/configuration.md` ("one mDNS query … at most a second").
**Regression guard.** This supersedes ADR 0091 D1/D3 and its consequences ("one mDNS query", "at most one second", "up to one second" per failed lookup) through `adr-0091-amend-1128` (up to three queries, a 3 s window, up to three seconds per failed lookup), named on its Status line and reading order; the glossary Upstream Note and the `mdns.go` package/`Lookup`/`lookupTimeout`/`defaultResolver` docs follow. Zero retransmit fields mean no retransmit. A retransmit send error is ignored, never returned, and the timer is stopped before the socket closes.
**Files:** internal/mdns/mdns.go; internal/mdns/mdns_test.go; docs/manual/configuration.md; .capcom/knowledge/decisions/adr-0091.md; .capcom/knowledge/decisions/adr-0091/adr-0091-amend-1128.md; .capcom/knowledge/glossary/glossary-identity/upstream.md
**Read first:** internal/mdns/mdns.go — resolver, defaultResolver, lookupTimeout, resolver.lookup, resolver.waitError; internal/mdns/mdns_test.go — startResponder, TestLookupTimesOutWithoutMatchingAnswer, TestLookupReturnsPromptlyWhenContextCancelled; internal/provider/localdial.go — localDialer.dialWithLocalFallback; internal/provider/localdial_test.go — mdnsStub; .capcom/knowledge/decisions/adr-0091/adr-0091-decision.md
**Tests:** `startResponder` gains a drop-the-first-N-queries mode and a query counter; `TestLookupAnswersOnARetransmit` (drop 1, then 2) finds the address; `TestLookupTimesOutWithoutMatchingAnswer` sets retransmit offsets well below its timeout through `testtime` and waits for three counted queries with `testtime.Eventually`, not reading the count the moment `lookup` returns; a resolver with zero retransmit fields sends one query; the ctx tests still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/mdns/`
- `GOMEMLIMIT=2GiB go test -count=1 -run TestLocalFallback ./internal/provider/`
- `capcom check`
**Commit:** `fix(mdns): retransmit the lookup query inside a three-second window`
**Closes:** mdns-single-query-timeout
