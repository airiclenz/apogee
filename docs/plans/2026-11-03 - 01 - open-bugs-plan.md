# Open bugs plan

**Goal:** Fix every open bug in the register that needs no owner action: the wrapper long-option guard bypass, the stray fallback walk after cancelled callers, the per-fault Monitor churn, the in-session Firing's missing description, and the text-tool measure split.
**Date:** 2026-11-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** debd6329

**Regression check (2026-11-03, debd6329):**
- 1: guard folded (with the writer's decision on the strict/lenient `stripWrappers` mode); supersedes the in-code bare-strip stance of `wrapperValueLetters` for long options.
- 3: guard folded (with the writer's decision on `sessionFallback.JoinLanding`); supersedes `adr-0105-amend-1102d` in part (dated amendment added).
- 4: guard folded; supersedes ADR 0083 part 4's acceptance of the in-session description clear (dated amendment added).
- 5: guard folded.

**Sources:**
- `.capcom/issues/wrapper-long-option-values.md`, `.capcom/issues/wrapper-option-values-leader.md`
- `.capcom/issues/await-landing-fresh-walk.md`, `.capcom/issues/session-resolver-monitor-churn.md`
- `.capcom/issues/firing-description-scrub-parity.md`, `.capcom/issues/text-tool-measure-split.md`
- `docs/plans/archived/2026-11-03 - 00 - code-audit-fixes-plan.md` (item 5), `docs/plans/archived/2026-11-02 - 04 - seat-and-resolved-entry-plan.md` (items 3, 7)
- ADR 0031 (Driver parity), ADR 0018 (+ amend-1102, no second estimator), ADR 0069 (server description)

**Ratified design calls** (Airic Lenz, 2026-11-03):
- **Wrapper long options:** per-wrapper full long-option table (no-value / optional-`=` / required-value) resolved by getopt-style unique prefix; an unknown or ambiguous long option fails closed.
- **Landing re-check:** a separate optional interface `FallbackLandingJoiner`, type-asserted by `awaitLanding`; a resolver without it gets no re-latch consult. `FallbackResolver` is unchanged.
- **Firing description:** no Driver scrubs — the in-session Firing carries the bound entry's `description:` like every other Driver.
- **Text-tool measure:** every request measure (room weight, fresh-result bound, predictive guard, calibration, ContextCost) counts the tool-surface bytes actually sent.

**Standing requirements:**
- skills: coding-standards
- Tests run narrowed to one package and a `-run` pattern (AGENTS.md); never two test runs at once.

**Out of scope:**
- `windows-journal-remove-check` (needs a Windows box) and every feature/task issue.
- Value options of `exec -a` and `time -o/-f` as wrappers (item 5 of the code-audit plan left them bare).
- `firingSessionSource` building a Monitor once per Firing run (not per-fault churn).

## 1. Wrapper long options consume their values; unknown ones fail closed — ✅ DONE (2026-11-03)

NOTES (2026-11-03): a strict strip that stops at an unknown or ambiguous long option leaves the option itself as the command's first word, so the target views treat it as an unknown leader and report every operand after it (e.g. `env --frobnicate echo rm .git/hooks/x` → `echo rm .git/hooks/x`); the option word itself is not reported, the same as any unknown leader's name.
NOTES (2026-11-03): added beyond the plan's listed rows: an ambiguous-prefix row (`sudo --h …`), an unknown-with-`=` row, and two `TestProjectConfigControlPlaneTargets` rows that pin the strict mode in projectConfigCommandTargets.
NOTES (2026-11-03): pre-existing, not changed: the file named by `time --output FILE` / `-o FILE` is not reported as a write target (before this change it was taken for the leader and was not reported either); sudo's `-h` is listed as a required-value letter although sudo declares it optional (`-h[host]`).

**What:** fix for a guard bypass: `env --unset echo rm -f .git/hooks/pre-commit` (and the abbreviated `env --u echo …`) hides the hook write from `write-git-control-plane`, because wrapper long options strip bare and their value word is taken as the leader.
**Regression guard.** stripWrappers takes a mode (strict for operandTargets and projectConfigCommandTargets, lenient for cdTarget and readsStdinScript); both modes consume a known long option's value word, and only the strict mode stops at an unknown or ambiguous long option — the lenient mode drops it bare as before. A bare `--` ends the wrapper's options as getopt does: it is dropped and the next word is the leader, never read as an ambiguous empty long option. All nine `wrapperLeaders` get a table, spelled from the man pages: nohup `help version`; command/exec/builtin none; time `append portability quiet verbose help version` plus value names `output format` (the short `-o/-f` stay out of scope); and the no-value sets name sudo `no-update`, env `list-signal-handling debug`, xargs `open-tty show-limits`. Every comment in `shellwrites.go` saying how a wrapper option or its value is stripped (`wrapperValueLetters`, `stripBareOptions`, `takesNextWordValue`; `grep -n 'stripped bare\|long option\|wrapperValueLetters\|stripBareOptions' internal/security/shellwrites.go`) is rewritten — this supersedes, for long options, the bare-strip stance `wrapperValueLetters`' comment records (ADR 0012 footgun guard).
**Goal:** every wrapper in `wrapperLeaders` resolves its long options by exact name or unique prefix against a full per-wrapper table; a required-value long option given without `=` consumes the next word; an optional-argument long option (`xargs --replace/--eof/--max-lines`, `env --block-signal/--default-signal/--ignore-signal`, `sudo --preserve-env`) never consumes the next word; an unknown or ambiguous long option without `=` stops stripping so the target views report every operand from it onward; the short value letters include `env -a` and `sudo -r/-t/-c`.
**Approach (assumed at the header base):** in `internal/security/shellwrites.go` replace the `--` early-return in `takesNextWordValue` with a lookup in a new per-wrapper long-option table beside `wrapperValueLetters` (same owner; no new module). Required-value sets: env `unset chdir split-string argv0`; sudo `user group host prompt close-from chdir chroot command-timeout other-user role type login-class`; nice `adjustment`; xargs `arg-file delimiter max-args max-chars max-procs process-slot-var`. No-value and optional names complete each table so prefixes resolve as getopt does. `--opt=value` stays one dropped word. The fail-closed stop applies to the target views (`operandTargets`, `projectConfigCommandTargets`); `cdTarget` and `readsStdinScript` keep reading past an unknown option as today, so no view loses what it sees now. Add `a` to env's letters and `rtc` to sudo's.
**Files:** internal/security/shellwrites.go, internal/security/shellwrites_test.go, internal/security/dangerous_test.go
**Read first:** internal/security/shellwrites.go — stripWrappers, stripBareOptions, takesNextWordValue, wrapperValueLetters, operandTargets, projectConfigCommandTargets; internal/security/shellwrites_test.go — TestWriteTargetsOf; internal/security/dangerous_test.go — TestShellWriteViewJudgesWhatTheCommandWrites
**Closes:** wrapper-long-option-values; wrapper-option-values-leader
**Tests:** `TestWriteTargetsOf` rows: `env --unset echo rm -f .git/hooks/pre-commit`, `env --u echo rm -f .git/hooks/pre-commit`, `sudo --user root rm .git/config`, `nice --adjustment 5 rm .git/config`, `xargs --max-args 1 rm .git/config`, `xargs --replace rm .git/config` (optional arg: `rm` is the leader), `env --frobnicate echo rm .git/hooks/x` (fails closed, reports the target), `env -a echo rm .git/config`, `sudo -r role rm .git/config`, `sudo -- cat .git/config` → no target, `/usr/bin/time --verbose cat .git/config` → no target; and a row through each lenient view (`cdTarget`, `readsStdinScript`) showing a known long option's value consumed and an unknown one dropped bare. `TestShellWriteViewJudgesWhatTheCommandWrites` row for `env --unset echo rm -f .git/hooks/pre-commit` at `TierHardRefuse`/`write-git-control-plane`.
**Acceptance:**
- `go build ./... && go vet ./internal/security/`
- `go test -race -count=1 -run 'TestWriteTargetsOf|TestShellWriteView|TestProjectConfig' ./internal/security/`
**Commit:** `fix(security): consume wrapper long-option values and fail closed on unknown ones`

## 2. A session seat with no fallback builds no Monitor per fault — ✅ DONE (2026-11-03)

**What:** regression from item 3 of the seat-and-resolved-entry plan: `sessionFallback.resolver()` builds the bound binding's Monitor before `newSeatResolver` returns nil for an entry with no `fallback:`, so every upstream fault builds and discards one.
**Goal:** a fault on a session-seat entry whose primary names no `fallback:` builds no Monitor and leaves no resolver cached; a test pins it.
**Approach (assumed at the header base):** in `cmd/apogee/fallback.go` `(*sessionFallback).resolver`, return nil when `entry.Fallback.Names()` is empty, right after `f.bound()` and before `f.binding()` is called. The test swaps `f.binding` for a counting wrapper of `holder.Binding` (its only caller builds the Monitor) and asserts zero calls after two faults.
**Files:** cmd/apogee/fallback.go, cmd/apogee/fallback_test.go
**Read first:** cmd/apogee/fallback.go — (*sessionFallback).resolver, (*sessionFallback).Resolve, newSessionFallback, newSeatResolver; cmd/apogee/dial.go — upstreamBinding.Monitor; cmd/apogee/fallback_test.go — newTUIFallbackSeat, TestTUIFallbackResolverAnswersForTheBoundEntry; cmd/apogee/wire_live.go — newSessionFallback call site
**Closes:** session-resolver-monitor-churn
**Tests:** new `TestTUIFallbackNoFallbackEntryBuildsNoMonitor`: entry with no `fallback:`, `Resolve` twice → zero answer, binding calls == 0, `f.current == nil`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestTUIFallback|TestSeatResolver' ./cmd/apogee/`
**Commit:** `fix(fallback): build no Monitor for a session seat without a fallback list`

## 3. The post-cancel landing consult joins, never walks

**What:** fix: when every routed caller is cancelled during the grace window, `Agent.awaitLanding` re-consults via `Resolve` on `context.WithoutCancel`; if the run ended (and was dropped) in between, that consult starts a fresh grace window and walk with no caller, contrary to the interface doc's "must join the run still going rather than start another".
**Regression guard.** sessionFallback's JoinLanding reads f.current under its lock and never calls f.resolver(), so item 2's "no resolver cached, no Monitor built" holds. `JoinLanding` is added to `*stubResolver` itself (delegating to its answer func on the detached ctx), so the no-joiner test uses its own comparable pointer type implementing `Resolve` only — never `resolverFunc`, which `isArmedOn` rejects, so `awaitLanding` would return early on either tree — latched as the armed target's Fallback. Every comment naming the engine's post-cancel consult or join is brought in line (`grep -rn "consult after\|once more about\|awaitLanding\|join.* the run" --include=*.go internal/agent cmd/apogee apogee.go`). This supersedes in part `adr-0105-amend-1102d` (the re-latch by "the engine's own consult" on every Driver's resolver): add a dated child `adr-0105-amend-1103` (a resolver without the joiner gets no post-cancel re-latch), named on ADR 0105's Status line and reading-order list.
**Goal:** the engine's post-cancel consult only ever joins an in-flight run; with no run in flight it answers nothing and starts no grace window, walk or land; the joiner is an optional exported interface and `FallbackResolver` is unchanged.
**Approach (assumed at the header base):** declare `FallbackLandingJoiner` in `internal/agent` next to `FallbackResolver` (method e.g. `JoinLanding(ctx, report) (FallbackAnswer, bool)`), alias it in `apogee.go` beside the `FallbackResolver` alias. `awaitLanding` (`internal/agent/rebind.go`) type-asserts the seat's resolver; absent → no consult, no re-latch. Implement on `*fallbackResolver` (`cmd/apogee/fallback.go`: under `mu`, look up `runs[report.Server]`, none → zero/false, else wait on `done` or ctx; only Switch runs re-latch and those are never dropped) — `*seatResolver` gets it by embedding; `*sessionFallback` forwards to `f.current` without building one (nil → zero/false). The `newRelatchRig` stub in `internal/agent/fallback_test.go` implements it. Update the interface doc comment to name the joiner.
**Files:** internal/agent/agent.go, internal/agent/rebind.go, internal/agent/fallback_test.go, apogee.go, cmd/apogee/fallback.go, cmd/apogee/fallback_test.go, .capcom/knowledge/decisions/adr-0105.md, .capcom/knowledge/decisions/adr-0105/adr-0105-amend-1103.md
**Read first:** internal/agent/rebind.go — consultResolver, awaitLanding; internal/agent/delegationtarget.go — isArmedOn; internal/agent/agent.go — FallbackResolver (doc); internal/agent/fallback_test.go — stubResolver;
  cmd/apogee/fallback.go — fallbackResolver.Resolve, sessionFallback; cmd/apogee/fallback_test.go — TestFallbackResolverLandsTheTargetForCancelledCallers
**Depends on:** item 2 (shares `cmd/apogee/fallback.go`)
**Closes:** await-landing-fresh-walk
**Tests:** cmd/apogee: new `TestFallbackResolverJoinAfterADroppedRunStartsNoWalk`: all-down seat, caller cancelled mid-grace, run ends as None, `runs` empty (`testtime.Eventually`), then `JoinLanding` → zero answer, `beats` and `len(lands())` unchanged; `TestFallbackResolverLandsTheTargetForCancelledCallers`'s second consult becomes `seat.JoinLanding(context.Background(), primaryReport())`, asserting ok, the switch onto backup and `Target.Fallback == seat`. internal/agent: new `TestFallbackRoutedLandingWithoutAJoinerConsultsNothing` — a comparable pointer resolver implementing `Resolve` only, latched as the armed target's Fallback, Step cancelled mid-wait; asserts it was armed and gets zero `Resolve` calls after the cancel; existing `TestFallbackRoutedLandingRelatchesAfterCancelledCallers` still re-latches through `*stubResolver`'s `JoinLanding`.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestFallbackRouted|TestFallbackCancel|TestFallbackLanding' ./internal/agent/`
- `go test -race -count=1 -run 'TestFallbackResolver|TestTUIFallback' ./cmd/apogee/`
**Commit:** `fix(agent): join an in-flight landing after cancel instead of starting a walk`

## 4. An in-session Firing orients with the entry's description

**What:** fix for Driver parity (ADR 0031): `(*liveSettings).firingBinding` clears the held entry's `Description`, so an in-session Firing's Delegations bullet omits `— <description>` on its first binding while headless, daemon, the TUI session and the Firing after a fallback all carry it.
**Regression guard.** The new `wire_firing_test.go` test (named `TestFiring…` so the Acceptance pattern runs it) takes `TestFiringOrientationNamesBothSeatsUnderSeatChoice`'s fixture — `SubAgentsChoice: config.SubAgentsChoiceModel` and a `SystemPrompt.Global.Text`, since `delegationSeats` renders no bullet without the seat choice and no block without a system prompt — routed through `newLiveSettings{StartupEntry, HostAlias}.firingBinding`, with the API key taken from the binding (`firingBinding` clears `entry.APIKey`). The prose rule covers every comment, test doc or failure message saying an in-session Firing's description is cleared, dropped or scrubbed (`grep -rn -i 'description' cmd/apogee/ | grep -i 'clear\|drop\|scrub'`); each `Description != "" || EffortDialect != ""` case in `TestFiringBindingHandsOverTheHeldEntry` is split, not flipped, so `EffortDialect` stays asserted "". This supersedes ADR 0083 part 4's acceptance of the in-session description clear (`.capcom/knowledge/decisions/adr-0083/adr-0083-part-4.md`, the "accepted as it stands" paragraph naming the "decision not taken here" comment): add a dated `> **Amended 2026-11-03 (plan 2026-11-03 - 01).**` blockquote under that paragraph (the description is now carried, per ADR 0031; the dialect clear stands), named on ADR 0083's Status line.
**Goal:** an in-session Firing's first binding carries the bound entry's `description:` into `ServerDescription`, and its rendered Delegations bullet reads `run_on "session" = <model> on <name> — <description>` exactly as the headless path renders it; the `EffortDialect` clear is untouched.
**Approach (assumed at the header base):** in `cmd/apogee/wire_settings.go` drop only the `Description` half of the clear in `firingBinding` and rewrite its "decision not taken" comment to state parity as decided. Flip the two `Description == ""` assertions in `TestFiringBindingHandsOverTheHeldEntry`; delete the `ranks.description = ""` override in `wantSessionFiring` (`cmd/apogee/entryrank_test.go`). Prose rule: every comment or test reason citing `firing-description-scrub-parity` or the scrub (`grep -rn 'scrub-parity\|decision not taken' cmd/ internal/`) is updated in this item.
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go, cmd/apogee/entryrank_test.go, cmd/apogee/wire_firing_test.go, .capcom/knowledge/decisions/adr-0083.md, .capcom/knowledge/decisions/adr-0083/adr-0083-part-4.md
**Read first:** cmd/apogee/wire_settings.go — (*liveSettings).firingBinding; cmd/apogee/wire_settings_test.go — TestFiringBindingHandsOverTheHeldEntry; cmd/apogee/entryrank_test.go — wantSessionFiring; cmd/apogee/wire_firing_test.go — TestFiringOrientationNamesBothSeatsUnderSeatChoice;
  cmd/apogee/e2e_seat_test.go — seatFirstDelegationsLine; cmd/apogee/wire_firing.go — bindFiringConfig; internal/agent/orientation.go — delegationSeats, describeDelegationSeat
**Closes:** firing-description-scrub-parity
**Tests:** `TestFiringBindingHandsOverTheHeldEntry` with each description/dialect case split (description carried, dialect still cleared) and its test doc rewritten; entry-rank table; new `TestFiring…` test in `wire_firing_test.go` on the seat-choice fixture above, rendering an in-session Firing's orientation with a Sub-agent server installed and asserting the exact bullet string with the description.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestFiringBinding|TestEntryRank|TestFiring' ./cmd/apogee/`
**Commit:** `fix(firing): carry the entry description into an in-session Firing's orientation`

## 5. Text-tool profiles: every request measure counts the sent tool surface

**What:** fix: on a non-native (text-tool) profile, ContextCost counts the rendered instruction block (the bytes sent), while the request room's weight, the fresh-result bound, the predictive guard and calibration count the raw `ToolDef` menu JSON, which is never sent.
**Regression guard.** Calibration is `domain.PromptChars(st.Messages, nil) + a.<helper>(st.Tools)` over the sent request (`st := req.State()` in `streamResponse`, after `MomentPreRequest`, so a Reaction's `SetTools` is counted), never a room's stored figure; the room's stored figure serves `weight`/`left`/`freshShare`/`exceeds` only (`exceeds` runs before `MomentPreRequest`, so `st.Tools` is the room's menu there). The calibration test asserts after one Turn `a.budget().CharsPerToken == 4*(1-w) + (chars/P)*w`, with `chars = PromptChars(sent msgs, nil) + len(toolInstructions(menu))` and `Usage.Prompt` P chosen so `chars/P` lies inside `[2, 8]` for both the block and the menu-JSON figure (the estimator clamps each sample to that range).
**Goal:** on a text-tool profile every request measure — room weight/left, fresh share, `exceeds`, calibration, ContextCost — counts `len(toolInstructions(menu))` for the tool surface; on a native profile each counts `PromptChars(nil, menu)` as today; one helper computes the figure.
**Approach (assumed at the header base):** add one tool-surface-chars helper beside `toolInstructions` in `internal/agent/wire.go` (block length when non-empty, else `domain.PromptChars(nil, menu)`). `Agent.requestRoom` stores it; `requestroom.go` `weight`/`freshShare`/`exceeds` use it instead of passing `st.Tools` to `PromptChars`; the calibration call in `loop.go` (`a.tokens.Calibrate(domain.PromptChars(st.Messages, st.Tools), …)`) moves to the same figure in this commit, as does `requestContents` and the fresh-share call in `dispatch.go`; `contextcost.go` reads the helper. Prose rule: every comment in `internal/agent` describing the tool menu's weight in a measure (`grep -rn 'PromptChars(.*Tools\|menu JSON\|tool menu' internal/agent/`) is brought in line.
**Files:** internal/agent/wire.go, internal/agent/requestroom.go, internal/agent/loop.go, internal/agent/dispatch.go, internal/agent/contextcost.go, internal/agent/requestroom_test.go, internal/agent/contextcost_test.go
**Read first:** internal/agent/requestroom.go — Agent.requestRoom, weight, exceeds; internal/agent/wire.go — toolInstructions; internal/agent/loop.go — streamResponse (Calibrate), requestContents; internal/agent/contextcost.go — contextCost;
  internal/agent/contextcost_test.go — TestContextCostCalibratedAfterUsage
**Closes:** text-tool-measure-split
**Tests:** new: on a markdown-fenced profile the room's tool-surface weight equals ContextCost's "tool instructions" row and calibration is fed the same chars (observed as `a.budget().CharsPerToken`, with P inside the clamp, per the guard); existing `TestRequestRoom*`, `TestContextCostToolSurfaceIsOneRow`, `TestPromptSeam_NonNativeInjectsMenuAndSuppressesTools` stay green.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `go test -race -count=1 -run 'TestRequestRoom|TestContextCost|TestPromptSeam|TestAppendToolResult|TestFreshResult' ./internal/agent/`
**Commit:** `fix(agent): count the sent tool surface in every request measure on text-tool profiles`
