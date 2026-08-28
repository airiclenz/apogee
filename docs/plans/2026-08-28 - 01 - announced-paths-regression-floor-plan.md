# Announced-paths invariant and the regression floor — implementation plan

**Goal:** make the class of regression found on 2026-08-28 impossible to ship again. Two
independently correct changes (the F-13 resolved-path mount, 2026-08-26; the ADR 0056 scratch
dir under `~/.apogee` × the ADR 0049 §4 forced look) each passed their own item's tests and
broke the model's journey: a path apogee itself announced (`files: …` skill header, the
orientation's scratch line) was refused or prompted on by apogee's own tools. This plan (1)
records the standing rule that a regression is never deferred, (2) gives stubllm the one
feature needed to script "the model uses exactly what it was told", and (3) adds an e2e suite
over representative host shapes asserting one invariant: **in Auto, every path the engine
announces is usable by every tool with zero approval prompts.**

The `/implement-plan` skill itself was amended the same day (regression triage → always
FOLLOW-UP; verifier bite check for fix items; closeout refuses a deferred regression;
plan-writer "acceptance drives the announced surface"). Those live in the skills repo and are
NOT items here.

**Date:** 2026-08-28
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `docs/plans/2026-08-28 - 00 - symlinked-skill-reads-and-scratch-forced-look-plan.md` — the
  two fixes this plan's fixtures must hold green (item 1 verifies it is archived).
- `docs/design/test-drivers.md` — stubllm script format, matching, request log; tuitest
  drivers. `internal/stubllm/script.go` (`Turn`, `Match`, strict parsing),
  `internal/stubllm/match.go`, `internal/stubllm/server.go`.
- `cmd/apogee/e2e_support_test.go` — `startSession` (home reaches the binary as
  `--config <home>`, workspace as `--workspace <ws>`, line 181), `e2eHome`/`upstreamHome`,
  `e2eWorkspace`, `e2eGuards`, `paneTrace`; `cmd/apogee/e2e_approval_test.go` —
  `approvalMarker = "Always allow this session"`, `awaitApprovalPane`;
  `cmd/apogee/confinement_e2e_test.go` — the injectable-Confiner seam and the FSWrite skip
  idiom; `cmd/apogee/testdata/stubllm/guard.yaml` — the forced-look control command.
- `internal/agent/loop.go:1161` — the skill header's `files: <Skill.Dir> — this skill's
  bundled files; …` line (the announced skill path). `internal/agent/orientation.go` —
  `orientationTemplate[orientationScratchLine]` (the announced scratch path) and the
  workspace line (the announced workspace path).
- `AGENTS.md` "Conventions not derivable from the code"; `ISSUES.md` header.

**Ratified design calls:**
- **Regressions are never deferred (owner, 2026-08-28):** a regression — behaviour that worked
  at the previous commit/release and does not now, or a path, name or value apogee announces
  to the user or model that a tool, mode or guard refuses or prompts on — is fixed in the run
  that finds it or blocks that run's closeout. It never enters ISSUES.md as deferred work and
  never ships. Item 2 records it in the repo docs.
- **stubllm captures (owner, 2026-08-28):** a `Turn` gains `captures:` — a list of
  `{name, from: system|last_message, pattern}`; `{{name}}` substitutes into that turn's `text`
  and each `tool_calls[].arguments`. Strict like the rest of the stub: a pattern without
  exactly one capture group is a parse error; a `{{name}}` with no capture of that name on the
  same turn is a parse error; a capture that does not match the request is an HTTP 500
  (`stubllm: capture <name> unmatched for request N`) logged with `Unmatched` set. `from:
  system` is the concatenated text of the request's system messages in order. Record mode
  never emits captures.
- **Host shapes covered (plan author, 2026-08-28):** (a) dotfiles-symlinked home — both
  `<fakehome>/.apogee → <real dir>` AND `<home>/skills → <another real dir>`, the shape on
  the owner's devbox; (b) symlinked workspace — the macOS `/tmp` shape; (c) Auto with the
  session scratch dir. A no-landlock host is NOT a shape here: the e2e drives the shipped
  composition, and item 5 takes the confiner seam `confinement_e2e_test.go` already uses.
- **Skill attachment in the fixture (plan author, 2026-08-28):** the skill is attached the
  way a user attaches one — through the input, by the command `docs/manual/commands.md`
  documents for invoking a skill — never by pre-injecting the header into the stub script.
  The `files:` path the stub captures is the one the shipped code rendered.
- **Zero-approval assertion (plan author, 2026-08-28):** "no prompt" is
  `strings.Count(paneTrace(t, sess), approvalMarker) == 0` after the run's last tool result,
  plus `stub.Unmatched()` empty and `stub.AssertConsumed(t)`. A positive control in the same
  test (a command naming `~/.apogee/config.yaml`) must still raise the pane, so a silent
  approver could never make the suite pass.

**Standing requirements:**
- `skills: coding-standards`
- Any authorised deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; `make check` runs once at closeout.
- Every item's CHANGELOG entry goes in its sidecar. No item changes `VERSION`.
- Helpers shared by items 4–6 live in item 4's test file (`e2e_announced_test.go`); items 5
  and 6 depend on 4 and add no edits to `e2e_support_test.go`.

**Out of scope:**
- Any change to `/implement-plan` (done in the skills repo, 2026-08-28).
- A no-landlock host shape in e2e (the dispatch-level test in plan 00 item 5 covers the
  gate cell; the ladder has its own unit tests).
- Templating beyond captures (loops, conditionals) in stubllm.
- Driving these fixtures through the PTY driver — the in-process driver is enough for the
  invariant; PTY coverage belongs to the test-drivers kit's own plan.

---

## 1. Verify plan `2026-08-28 - 00` is archived — ✅ DONE (2026-08-28)

NOTES (2026-08-28): verification passed — `docs/plans/archived/2026-08-28 - 00 - symlinked-skill-reads-and-scratch-forced-look-plan.md` exists; all 5 numbered H2 items carry `✅ DONE (2026-08-28)` (`grep -c "✅ DONE"` prints 5); `git log --oneline -8` shows the five commits landed (36a493e7 readScope resolved path, 1de41dea copy_file source seam, 0fc7e78b symlink spelling for list_dir/grep/find_files, 91be2725 guard masks the session scratch dir, eaa66a50 ADR 0049 amendment) plus the archive commit 4c9f3e97. Items 4–6 of this plan may proceed against a fixed tree.

**What:** confirm `docs/plans/archived/2026-08-28 - 00 - symlinked-skill-reads-and-scratch-forced-look-plan.md`
exists and every H2 in it carries `✅ DONE`, and that `git log --oneline -8` shows its five
commits landed. If it is not archived, report BLOCKED naming the unfinished item — items 4–6
would fail against an unfixed tree and prove nothing.

**Files:** none (verification only; the sidecar records the check).

**Tests:** none.

**Acceptance:**
- `test -f "docs/plans/archived/2026-08-28 - 00 - symlinked-skill-reads-and-scratch-forced-look-plan.md"`
- `grep -c "✅ DONE" "docs/plans/archived/2026-08-28 - 00 - symlinked-skill-reads-and-scratch-forced-look-plan.md"` prints `5`

**Commit:** none — a verification item makes no commit; the verifier marks it done in the plan file only.

---

## 2. Record the regression floor in the repo docs — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the "Where knowledge lives" append reads `… in `ISSUES.md` — never a regression (see Conventions).` — the appended clause sits at the line's end and the pre-existing sentence period moved behind it, so the line stays one grammatical sentence.

**What:**
- `AGENTS.md`, "Conventions not derivable from the code": add one bullet after the
  `make check` bullet — **"Regressions are never deferred.** A regression — behaviour that
  worked at the previous commit or release and does not now, or a path, name or value apogee
  itself announces to the user or model (an orientation line, a skill header, a `{{…}}`
  expansion) that a tool, mode or guard refuses or prompts on — is fixed in the run that
  finds it or blocks that run's closeout. It never enters `ISSUES.md` as deferred work and
  never ships; the 2026-08-26 read-root residual that shipped in v0.18.2 is the case this
  rule closes."
- `ISSUES.md`: add one sentence to the header paragraph (the "open only" statement): "A
  regression is not deferrable work and never belongs here — it is fixed, or it blocks."
- `AGENTS.md` "Where knowledge lives", `ISSUES.md` line: append "— never a regression (see
  Conventions)".

**Files:** `AGENTS.md`, `ISSUES.md`

**Tests:** none (docs).

**Acceptance:**
- `grep -c "Regressions are never deferred" AGENTS.md` prints `1`
- `grep -c "never belongs here" ISSUES.md` prints `1`

**Commit:** `docs(agents): regressions are never deferred — the standing rule, in AGENTS.md and the ISSUES.md header`

---

## 3. stubllm captures — a turn can echo what the request told it — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `expand` is a VALUE receiver (`func (t Turn) expand(r Request) (Turn, error)`),
not the pointer receiver the item's text sketches — every other Turn method in the package takes a
value receiver, and expansion must not be able to mutate the Script. It lives in `match.go` (the
item's own alternative to `server.go`, since that is where the request is already resolved against
a turn); `server.go` holds the wiring only.
NOTES (2026-08-28): two files outside the item's Files list were touched, both to keep an existing
doc honest rather than to add behaviour: `internal/stubllm/log.go` gains `systemText` beside its
sibling `lastText` (the reader a `from: system` capture uses), and `internal/stubllm/doc.go`'s
package file-map line for `match.go` now mentions the expansion the file gained.
NOTES (2026-08-28): `Server.take` now returns `(Turn, error)` instead of `(Request, Turn, bool)` —
the Request return had no reader left once the 500 body is built where the failure is known, and
both failure paths (no turn, unmatched capture) now log and refuse through one seam.
NOTES (2026-08-28): `script_test.go`'s `designDocExample` helper took a `heading` parameter so the
existing `## stubllm` example test and the new `### Captures` example test share one loader rather
than duplicating it.

**What:**
- `internal/stubllm/script.go`: add `Capture struct { Name string; From string; Pattern string }`
  with yaml tags `name`, `from`, `pattern`; `Turn.Captures []Capture \`yaml:"captures,omitempty"\``.
  Validation at parse (same place unknown keys are refused): `name` non-empty and unique per
  turn; `from` ∈ {`system`, `last_message`}; `pattern` compiles and has exactly ONE capture
  group (`regexp.NumSubexp() == 1`); every `{{name}}` occurring in the turn's `text` or any
  `tool_calls[].arguments` names a capture on that turn (unknown placeholder → parse error
  listing the turn index and the placeholder); `captures` on an `http` or `hang` turn is
  refused like `reasoning` is.
- `internal/stubllm/server.go` (or `match.go`, where the request is already decoded): when a
  turn with captures plays, evaluate each against the request — `system`: the request's system
  messages' text concatenated in order with `\n`; `last_message`: the same text `when.last_message`
  matches — take group 1, and substitute `{{name}}` in the served `text`/`arguments`
  (substitute into copies; the script is immutable). An unmatched capture answers HTTP 500
  `stubllm: capture <name> unmatched for request N`, logs the request with `Unmatched: true`,
  and does NOT consume the turn (mirror the no-turn path).
- `record.go`: no change beyond compiling (a recorded turn never carries captures).
- `docs/design/test-drivers.md`: add `### Captures` under "The script format" (after
  "Matching") with the yaml example below and the strictness rules; the example is loaded by
  `script_test.go` like the existing one so the doc cannot rot.

```yaml
model: stub-model
turns:
  - captures:
      - {name: scratch, from: system, pattern: 'scratch directory[^\n]*?(/\S+)'}
    tool_calls:
      - name: terminal
        arguments: '{"command":"mkdir -p {{scratch}}/tmp && echo ok"}'
```

- Standards that bind here: parsing stays strict (an unknown placeholder is an error, never a
  literal `{{x}}` on the wire); the evaluation is one function (`(t *Turn) expand(req) (Turn, error)`)
  with a table test; no global state.

**Files:** `internal/stubllm/script.go`, `internal/stubllm/script_test.go`,
`internal/stubllm/server.go`, `internal/stubllm/match.go`, `internal/stubllm/server_test.go`,
`docs/design/test-drivers.md`

**Tests:**
- `script_test.go`: the doc example parses; a pattern with 0 or 2 groups is refused; an
  unknown `{{x}}` is refused naming the turn; `captures` on an `http` turn is refused;
  duplicate names refused; `Marshal` round-trips a turn with captures.
- `server_test.go`: a request whose system prompt carries `scratch directory: /tmp/x/scratch/abc`
  gets `mkdir -p /tmp/x/scratch/abc/tmp && echo ok` in the tool call's arguments; a request
  without the line gets 500 with the exact message, `Unmatched` set, and the turn still
  available to the next request; `from: last_message` capture substitutes into `text`.

**Acceptance:**
- `go build ./... && go vet ./internal/stubllm`
- `go test ./internal/stubllm -count=1`

**Commit:** `feat(stubllm): captures — a turn substitutes text it captured from the request's system prompt or last message`

---

## 4. e2e: announced skill paths under a dotfiles-symlinked home, zero prompts — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the five read tools ride ONE scripted turn (five `tool_calls` on the turn that
carries the capture) rather than the item's chain of `when: tool_result` turns. A capture is evaluated
against the request its own turn answers, and only the FIRST request carries the skill block — a
chained turn would have to re-capture the folder out of the PRECEDING TOOL'S OUTPUT, which pins each
tool's result format instead of the announcement under test. Issuing the five calls from the one turn
that can read the header keeps every argument the header's own spelling, which is the item's point.
NOTES (2026-08-28): "zero prompts" is asserted with a new in-process pane counter
(`watchApprovalPanes`) rather than the design call's `paneTrace`. `paneTrace` reads the `--tui-trace`
file, and `launchTUI`/`startSession` deliberately never pass `--tui-trace` (the trace wraps
`os.Stdout` and `tui.Build` refuses it beside a driver output), so it takes a `*ptySession` and has
nothing to answer with here — while the item's own step 1 pins this fixture to the in-process
`startSession`, and the plan puts PTY coverage out of scope. The counter polls the frame for
`approvalMarker` and counts rising edges, so it answers the same question and also serves item 5's
"count 0 before turn 2, exactly 1 at the end". Both halves were mutation-checked: pointing it at a
string that IS on screen makes it report 2, and pointing the path assertion at the resolved spelling
makes all five calls fail.
NOTES (2026-08-28): the run is launched through `launchTUIOn` — which is `startSession` on a home the
caller wrote, plus the `e2eGuards` every driven launch owes (leak check, ambient-config refusal, fast
config watcher) — rather than calling `startSession` bare and skipping those guards.
NOTES (2026-08-28): the workspace seeds an `out/` folder before the launch: `copy_file` writes into an
existing directory and does not create its destination's parents, so the item's `out/a.md` destination
needs it there.
NOTES (2026-08-28): the marker line is asserted on the `read_file` and `grep` results only, not on all
five as the item's step 5 reads — `copy_file`, `list_dir` and `find_files` answer with a receipt, a
listing and a match list, so no file CONTENT comes back through them for a marker to be in; all five
results ARE asserted to name `a.md` and none to carry the `outside the workspace root` read-scope
refusal, which is the invariant step 5 is after.

Depends on items 1 and 3.

**What:**
- `cmd/apogee/e2e_announced_test.go`, `TestE2EAnnouncedSkillPathsUnderASymlinkedHome`:
  1. Build the real home with `upstreamHome`/`e2eHome`; create `<tmp>/dotfiles/apogee` by
     MOVING that home there, then `<tmp>/home/.apogee → <tmp>/dotfiles/apogee` (symlink). Create
     the skill library in a THIRD dir `<tmp>/skills-repo/announced/` (`SKILL.md` with id
     `announced`, a body that instructs the model to read, copy, list, grep and find its own
     bundled files, and `prompts/a.md` with a marker line) and symlink
     `<tmp>/dotfiles/apogee/skills → <tmp>/skills-repo`. Pass `<tmp>/home/.apogee` (the
     SYMLINK spelling) as `home` to `startSession`, `--mode auto`.
  2. Add the shared helper `symlinkTo(t, target string) string` (creates a fresh symlink in a
     `t.TempDir()` and returns its path) in this file; items 5 and 6 reuse it.
  3. Attach the skill through the input as a user does (the invocation command
     `docs/manual/commands.md` documents), with a request text the script keys on.
  4. Script `cmd/apogee/testdata/stubllm/announced-skill.yaml`: a capture
     `{name: skilldir, from: last_message, pattern: 'files: (\S+) — this skill'}` on the first
     tool turn; then in order (each `when: tool_result` of the previous): `read_file
     {{skilldir}}/prompts/a.md`, `copy_file {{skilldir}}/prompts/a.md → out/a.md`, `list_dir
     {{skilldir}}`, `grep <marker> in {{skilldir}}`, `find_files a.md under {{skilldir}}`, then
     a closing `text`. The captured `skilldir` MUST be the symlink spelling
     (`<tmp>/home/.apogee/skills/announced`) — assert it via `stub.Requests()` so the test
     fails if the header ever starts announcing the resolved path without this test being
     revisited.
  5. Assert: every tool result the stub received (`stub.LastMessage(n)`) carries the marker
     line and none contains `outside the workspace root`; `out/a.md` exists in the workspace
     with the marker; `strings.Count(paneTrace(t, sess), approvalMarker) == 0`;
     `stub.Unmatched()` empty; `stub.AssertConsumed(t)`.
- Standards that bind here: the fixture builds the owner's real shape (two symlinks), not an
  abstraction of it; assertions read the stub's request log, not the frame, wherever the fact
  is on the wire.

**Files:** `cmd/apogee/e2e_announced_test.go`, `cmd/apogee/testdata/stubllm/announced-skill.yaml`

**Tests:** the test above.

**Acceptance:**
- `go vet ./cmd/apogee`
- `go test ./cmd/apogee -run 'E2EAnnouncedSkillPaths' -count=1`

**Commit:** `test(e2e): announced skill paths under a dotfiles-symlinked home run every read tool with zero prompts`

---

## 5. e2e: the announced scratch dir runs unprompted in Auto; the control plane still prompts — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's "no pane before turn 2's request reached the stub, `paneTrace` count exactly 1 at the end" is asserted as "the FIRST pane of the run is the control plane's, and the run raised exactly one" — the pane's own text is checked to name `guard-probe.txt` and NOT to name `TMPDIR`. A count read after a wait on the tool result is racy (the guard's pane can rise inside one poll of that wait); the first-pane form answers the same question with no window at all, and it is what actually failed under the mutation check. The counter is item 4's in-process `watchApprovalPanes`, per that item's own note, not `paneTrace` (which reads a pty run's trace file).
NOTES (2026-08-28): the item's Confiner seam did not exist for a driven run — `wire_boot.go` built `platform.NewConfiner()` directly while `headless.go` already owned the `newConfiner` seam for the same stated reason. One line of `wire_boot.go` now goes through it, so `installFenceableConfiner` can hand a no-landlock host the caps-only stand-in the headless tests already keep (`fenceableHost`). Production behaviour is unchanged.
NOTES (2026-08-28): `cmd/apogee`'s `TestMain` gained `maybeDispatchConfinedExec()` as its first statement. On Linux a confined subprocess is run by re-invoking `/proc/self/exe` with the `__confined-exec` sentinel, which under the in-process driver is the TEST binary — without the interception the helper child fell through to `m.Run()` and the fixture's first tool result was the whole suite's output instead of its command's. This is a pre-existing hole in the driver kit that no test had hit before: it is why `TestE2EApprovalForcedLookSurvivesAutoMode` cancels rather than allows its Auto pane.
NOTES (2026-08-28): the fixture home moved from `<tmp>/home/.apogee` to `<tmp>/home/<user>/.apogee` (`announcedUser`). The guard's control-plane rule is anchored on a home SPELLING (`homeAnchor` = `~|/home/<user>|/users/<user>|/root|$home|%userprofile%`), so under the old shape the rule never fired at all and the test passed with the scratch exemption removed — it pinned nothing. Item 4's assertions all derive from `fx.home` and are unaffected.
NOTES (2026-08-28): the fixture's home also needed a `system-prompt-text:` key (`announcedStandingPrompt`). The orientation block RIDES ALONG on a standing system message (ADR 0023 §6 amendment) and the e2e home's `config.yaml` sets no prompt, so without it the run sent no system message and announced nothing for a `from: system` capture to read.
NOTES (2026-08-28): the two scripted terminal calls carry explicit `id:`s. A call's id is numbered by position within its own turn, so two turns each emitting one call both sent `call_1` and the id-keyed readers collapsed them into one.
NOTES (2026-08-28): `assertEveryToolCallNames` (item 4's) now reads through a new shared `toolCalls(stub)` walker instead of carrying its own copy of the dedup loop, which item 5's `announcedScratchDir` also needs. Same behaviour, same file, no other item-4 code touched.
NOTES (2026-08-28): observed while reading the fixture's orientation — the "Read-only library roots" line announces the RESOLVED skill-library path (`<tmp>/skills-repo`) rather than the `<home>/skills` symlink spelling the operator configured. Not a regression: that resolved path IS a mounted read root, so nothing refuses it, and item 4 proves the symlink spelling works too. Recorded only because the roots line is an announcement and this suite is the place that would catch it changing.

Depends on items 1, 3 and 4.

**What:**
- `cmd/apogee/e2e_announced_test.go`, `TestE2EAnnouncedScratchDirRunsUnpromptedInAuto`:
  1. Same symlinked home as item 4 (reuse its builder), `--mode auto`, confine-to-workspace
     on (the default). Take the Confiner through the seam `confinement_e2e_test.go` uses: the
     host backend when `Capabilities().FSWrite`, else a caps-only stand-in that reports
     `FSWrite` and confines nothing — this test asserts PROMPTING, not fencing (the fence has
     its own suite).
  2. Script `cmd/apogee/testdata/stubllm/announced-scratch.yaml`: capture `{name: scratch,
     from: system, pattern: <the orientation's scratch line, read from
     orientationTemplate[orientationScratchLine] at write time — one group around the path>}`;
     turn 1 `terminal`: `export TMPDIR={{scratch}}/tmp && mkdir -p "$TMPDIR" && echo probe > "$TMPDIR/probe" && echo ok`
     (the live-session shape: an env export naming the scratch dir); on its result, turn 2
     `terminal`: `touch ~/.apogee/guard-probe.txt` (guard.yaml's positive control); closing text.
  3. Assert: the captured scratch path is `<tmp>/home/.apogee/scratch/<id>` (symlink spelling —
     the orientation announces the configured home); turn 1's tool result contains `ok` and
     `<home real>/scratch/<id>/tmp/probe` exists; NO approval pane appeared before turn 2's
     request reached the stub (`paneTrace` count 0 at that point); turn 2 raises
     `awaitApprovalPane` with `forcedReason` (the guard still fires on the control plane) —
     deny it; final `paneTrace` count is exactly 1.
- Standards that bind here: the positive control lives in the same test as the negative
  assertion, so a broken approver cannot pass it.

**Files:** `cmd/apogee/e2e_announced_test.go`, `cmd/apogee/testdata/stubllm/announced-scratch.yaml`

**Tests:** the test above.

**Acceptance:**
- `go test ./cmd/apogee -run 'E2EAnnouncedScratchDir' -count=1`

**Commit:** `test(e2e): the announced scratch dir runs unprompted in Auto while the control plane still forces a look`

---

## 6. e2e: a symlinked workspace (the macOS /tmp shape), every tool, zero prompts

Depends on items 3 and 4.

**What:**
- `cmd/apogee/e2e_announced_test.go`, `TestE2EAnnouncedWorkspaceThroughASymlink`: workspace
  `e2eWorkspace(t)` reached through `symlinkTo(t, ws)`; pass the SYMLINK spelling as `ws`;
  plain home; `--mode auto`.
- Script `cmd/apogee/testdata/stubllm/announced-workspace.yaml`: capture `{name: workspace,
  from: system, pattern: <the orientation's workspace line — one group around the path>}`;
  turns in order: `read_file {{workspace}}/a.txt`, `write_file {{workspace}}/b.txt`,
  `edit_file` on `{{workspace}}/b.txt` (or the repo's replace tool), `list_dir {{workspace}}`,
  `terminal cat {{workspace}}/a.txt`, closing text.
- Assert: the captured workspace is the symlink spelling; every result is a success (no
  `outside the workspace root`, no error prefix); `b.txt` exists under the REAL workspace with
  the edited content; `paneTrace` count 0; `Unmatched` empty; `AssertConsumed`.

**Files:** `cmd/apogee/e2e_announced_test.go`, `cmd/apogee/testdata/stubllm/announced-workspace.yaml`

**Tests:** the test above.

**Acceptance:**
- `go test ./cmd/apogee -run 'E2EAnnouncedWorkspace' -count=1`
- `go test ./cmd/apogee -run 'E2EAnnounced' -count=1` (all three together)

**Commit:** `test(e2e): a symlinked workspace is usable by every tool through its announced spelling with zero prompts`

---

## Suggested version bump

None on its own — items 2–6 are docs, a test-kit feature and tests. If cut together with plan
`2026-08-28 - 00`, that plan's suggested patch bump (`v0.18.3`) covers both; the owner decides.
