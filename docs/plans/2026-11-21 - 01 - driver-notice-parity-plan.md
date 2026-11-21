# Driver notice parity — config hygiene and skipped skills

**Goal:** The daemon says what headless says about a config holding a plaintext key or a retired `sub-agents: true` flag, from one shared producer. Headless and the daemon report SKILL.md files that failed to load, from wording that moves out of the TUI into `internal/skills`.
**Date:** 2026-11-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** b5d03942

**Regression check (2026-11-21, b5d03942):**
- 1: guard folded (headless wrapper keeps its two-argument shape; scan error keeps the plaintext notice; `reasonHeadless` keeps a reader; lint in Acceptance)
- 2: guard folded (lock-held case; configuration.md names the daemon); yields to keymigrate.go:114-116 / headless.go:870-874 ("silent rather than fatal")
- 3: guard folded (comments naming the moved functions follow the move)
- 4: guard folded (writer's decision: `firingSkillProvider(opts, roots)`; stale nil-catalog comments rewritten)
- 5: guard folded (log through `daemonLogWriter`; home-library skill across two roots; `.apogee/skills` fixtures; stale preamble rewritten)

**Sources:**
- `.capcom/issues/daemon-config-notice-gap.md`, `.capcom/issues/firing-skill-skips-silent.md`
- ADR 0083 §5 (no host-boot module: share wording, never the Driver's verdict), ADR 0047 D8, ADR 0036 D8 (an unattended run gets a notice, never a prompt), ADR 0031 (Driver parity), ADR 0075 D6 (stderr is outside the JSON contract), ADR 0043 (no sub-packages under `internal/tui`)

**Ratified design calls** (owner, 2026-11-21):
- **Daemon wording:** the producer takes the Driver's clause; headless keeps its exact bytes ("headless runs never prompt"), and the daemon says "the daemon never prompts".
- **Firing skip report:** a headless or daemon Firing reports only skills that failed to load; shadowed skills stay in the TUI's `/skills` report alone.
- **Daemon latch:** the skip report is logged once per project root and text (the `latchProjectNotice` shape), to the daemon's stdout log; a changed skip set logs again.

**Standing requirements:**
- skills: coding-standards
- Pi box: run tests only as single-package, single-test runs (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./cmd/apogee/`); never a package pattern, never two runs at once. Pass this on to every sub-agent.
- A test that needs a model scripts `internal/stubllm`; durations go through `internal/testtime`.

**Out of scope:**
- The shared Driver host (`driver-preamble-copied`, parked; ADR 0083 §5).
- Recording each Skill's source root in the loader (the issue's optional "smaller" half).
- Putting skip reports on headless's JSON event stream.
- The TUI's migration offers (unchanged).

## 1. One producer for the config-hygiene notices — ✅ DONE (2026-11-21)

NOTES (2026-11-21): `reasonHeadless` deleted rather than kept: both plaintext reasons now derive from the clause pair `neverPromptsHeadless` / `neverPromptsDaemon` through `neverPromptsReason`, which `configHygieneNotices` reads; the headless bytes are unchanged (pinned literally in `TestConfigHygieneNoticesPerDriver`). The clause-taking inner flag notice is `driverSubAgentsFlagNotice`; `subAgentsFlagNotice(path, names)` stays as its headless wrapper. `neverPromptsDaemon` has only a test reader until item 2 wires the daemon (`make lint` passes).

**What:**
**Goal:** `cmd/apogee` has one function that, given the server entries, the config file path and a Driver clause, returns the plaintext-key notice and the retired-sub-agents-flag notice (each only when it applies). Headless prints its notices through that function, and its stderr bytes are unchanged.
**Approach (assumed at the header base):** In `cmd/apogee/keymigrate.go`, add `configHygieneNotices(entries []config.ServerEntry, path, neverPrompts string) ([]string, error)`. It wraps the existing detectors `plaintextKeyEntries` and `config.RetiredSubAgentsEntries(path)` and formats them with `plaintextKeyNotice` and `subAgentsFlagNotice`. `subAgentsFlagNotice` gains the Driver clause in place of its hard-coded "headless runs never prompt". The plaintext reason becomes `<clause>, so apogee cannot offer to move it into a secret store`. Keep the `reasonHeadless` bytes, and add a daemon reason built from "the daemon never prompts". `runHeadlessBody` (`cmd/apogee/headless.go`) replaces its two inline calls with one `configHygieneNotices` call at the same point in its stderr order. The TUI's `prepareKeyMigration` and `prepareSubAgentsMigration` are unchanged. This follows ADR 0083 §5: share the wording, and let each Driver make the call.
**Regression guard.** Keep `subAgentsFlagNotice(path, names)` as the headless wrapper over a clause-taking inner function, so its two-argument calls at `keymigrate_test.go:146` and `:456` compile unchanged.
`configHygieneNotices` returns every notice it found (plaintext first) alongside a `RetiredSubAgentsEntries` error; headless discards that error exactly as today (`headless.go:870-877`), and a table case pins the plaintext notice surviving a scan error.
Derive both plaintext reasons from one clause constant pair that `configHygieneNotices` reads, so `reasonHeadless` (`keymigrate.go:201`) keeps a reader or is deleted — golangci-lint's `unused` (`.golangci.yml`, `default: standard`) fails `make check` otherwise.
**Files:** cmd/apogee/keymigrate.go; cmd/apogee/headless.go; cmd/apogee/keymigrate_test.go
**Read first:** cmd/apogee/keymigrate.go — plaintextKeyEntries, plaintextKeyNotice, subAgentsFlagNotice, reasonHeadless, reasonNoStore; cmd/apogee/headless.go — runHeadlessBody (plaintext + retired-flag notice block);
internal/config/configmigrate.go — RetiredSubAgentsEntries; cmd/apogee/keymigrate_test.go — TestSubAgentsFlagNoticeNamesTheEntriesAndTheReplacement, TestHeadlessNoticesTheRetiredSubAgentsFlagAndNeverPrompts
**Tests:** A table test `TestConfigHygieneNoticesPerDriver` in `keymigrate_test.go` with these cases: plaintext entry; `plaintext-key-ok: true` (no key notice); flag entry; both; neither; a plaintext entry over a file the flag scan cannot read (the plaintext notice is still returned, with the error). Each case runs for both Driver clauses. The headless clause must match the bytes that `TestSubAgentsFlagNoticeNamesTheEntriesAndTheReplacement` pins. The existing `TestHeadlessNoticesPlaintextKeysAndNeverPrompts` and `TestHeadlessNoticesTheRetiredSubAgentsFlagAndNeverPrompts` pass unchanged.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConfigHygieneNotices|TestPlaintextKeyNotice|TestSubAgentsFlagNotice' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessNoticesPlaintextKeys|TestHeadlessNoticesTheRetiredSubAgentsFlag' ./cmd/apogee/`
- `make lint`
**Commit:** `refactor(headless): produce the config-hygiene notices from one shared function`

## 2. The daemon prints the config-hygiene notices on stderr — ✅ DONE (2026-11-21)

NOTES (2026-11-21): the notices print right after `probe.ResidualNotice` (after the lock and `newDaemonWiring`), so a daemon refused by the lock or by wiring says neither; the config path is `filepath.Join(home, "config.yaml")` with `home` from the `config.ApogeeHome` call `runDaemonWith` already makes (headless's `resolveRoots` derives `roots.config` the same way).
NOTES (2026-11-21): the tests repoint the harness at a `plaintextConfig` home through a small `daemonHarness.rehome` helper (plus `startAndStop` / `assertNarrationClean`), as the plan's Read-first note allows; the plaintext-key fixture is `onePlaintextServer`, the body `plaintextConfig` takes.

**What:** Depends on item 1. Finishes `daemon-config-notice-gap`. This is a Driver-parity gap (ADR 0031), not a regression.
**Goal:** `apogee daemon` started with an unacknowledged plaintext `api-key:` entry, or with a `sub-agents: true` entry, prints the item-1 notices with the daemon clause on stderr, once at start-up. It prints nothing for an entry with `plaintext-key-ok: true`. A daemon refused by the lock prints neither notice, and the stdout log carries neither notice.
**Approach (assumed at the header base):** In `runDaemonWith` (`cmd/apogee/daemon.go`), after the schedules-file seed and the lock are taken, call `configHygieneNotices(opts.Servers…, filepath.Join(config.ApogeeHome(opts.ConfigDir), "config.yaml"), <daemon clause>)` and write each notice to `errOut`, next to the existing `probe.ResidualNotice`. A detector error is dropped, as headless drops it, and never stops the daemon. Add one sentence to `docs/manual/daemon.md` where its start-up stderr lines are described.
**Regression guard.** Add a lock-held case under a `TestDaemonNotices…` name: `acquireDaemonLock` returning `*platform.LockHeldError` over a plaintext config, asserting neither notice on errOut.
`docs/manual/configuration.md` names the daemon ("the daemon never prompts") beside headless in both passages (:2516-2521, the quoted stderr notice; :3001-3002); the `daemon.md` sentence sits beside the :186 refusal-stderr paragraph.
Yields to `keymigrate.go:114-116` / `headless.go:870-874` ("a file the scan stumbles on is silent rather than fatal"): a detector error is dropped, as headless drops it — this replaces the Approach's "printed as one stderr line".
**Files:** cmd/apogee/daemon.go; cmd/apogee/daemon_test.go; docs/manual/daemon.md; docs/manual/configuration.md
**Read first:** cmd/apogee/daemon.go — runDaemonWith (ApplyConfig with GlobalConfigOnly, config.ApogeeHome, acquireDaemonLock, probe.ResidualNotice); cmd/apogee/daemon_test.go — newDaemonHarness,
daemonHarness.run (opts.ConfigDir = h.home, so a fixture config must be written into h.home or h.home/h.dir/h.schedules reassigned), TestDaemonDisclosesTheFenceResidualAtStartup, TestDaemonRefusesWhenAnotherDaemonHoldsTheLock;
cmd/apogee/keymigrate_test.go — plaintextConfig, subAgentsFlagYAML; docs/manual/daemon.md; docs/manual/configuration.md
**Tests:** Add `TestDaemonNoticesPlaintextKeysOnStderr`, `TestDaemonNoticesTheRetiredSubAgentsFlagOnStderr`, `TestDaemonNoticesNothingWhenTheLockIsHeld` and `TestDaemonPlaintextKeyOkSilencesTheNotice` in `daemon_test.go`. Model them on `TestDaemonDisclosesTheFenceResidualAtStartup` (`newDaemonHarness`, `h.errOut` / `h.out`) and use the `plaintextConfig` / `subAgentsFlagYAML` fixtures. Each test asserts "the daemon never prompts" on errOut and its absence from `h.out`.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDaemonNotices|TestDaemonPlaintextKeyOk|TestDaemonDisclosesTheFenceResidual' ./cmd/apogee/`
**Closes:** daemon-config-notice-gap
**Commit:** `feat(daemon): warn on stderr about plaintext keys and the retired sub-agents flag`

## 3. Move the skip-report wording into internal/skills — ✅ DONE (2026-11-21)

NOTES (2026-11-21): `internal/tui/skills_test.go` (named in Files) does not exist; the TUI's `/skills` tests live in `internal/tui/skill_test.go` and pass unchanged, so no TUI test file was edited.
NOTES (2026-11-21): `FailedLines`/`ShadowedLines` each take the whole `[]SkipError` and partition it themselves (unexported `partitionSkips`/`shadowedBy` moved alongside), so `skillCatalogNote` passes `skipped` straight to both; `internal/skills` carries its own unexported `flattenField`/`fieldBreaks` copy of the TUI rule (the TUI's copy stays, still used by other TUI surfaces).

**What:**
**Goal:** `internal/skills` exports the wording for skills that failed to load and for shadowed skills, built from `[]SkipError`. The TUI's `/skills` report renders through it, and its output is byte-identical to the output at base.
**Approach (assumed at the header base):** Move `partitionSkips`, `shadowedBy`, `failedSkillLines` and `shadowedSkillLines` out of `internal/tui/skills.go` into a new `internal/skills/report.go`, exported as `FailedLines(skips []SkipError) []string` and `ShadowedLines(skips []SkipError) []string`. The formats, the singular heads and the 2- and 4-space indents stay exactly as they are. Each field is flattened the way `flattenField` (`internal/tui/transcript.go`) flattens it. If that rule is self-contained, give `internal/skills` its own unexported copy; `internal/skills` must not import `internal/tui`. `skillCatalogNote` calls the new functions. Do not change `skillSource` or `underSkillRoot`.
**Regression guard.** Every comment naming `failedSkillLines`/`shadowedSkillLines`/`partitionSkips`/`shadowedBy` (`grep -rn 'failedSkillLines\|shadowedSkillLines\|partitionSkips\|shadowedBy' --include=*.go`) follows the move to `skills.FailedLines`/`skills.ShadowedLines` — including `assertSkillSectionRows`' doc (`cmd/apogee/e2e_hostile_test.go:707-713`), a comment-only edit.
**Files:** internal/skills/report.go; internal/skills/report_test.go; internal/tui/skills.go; internal/tui/skills_test.go; cmd/apogee/e2e_hostile_test.go
**Read first:** internal/tui/skills.go — skillCatalogNote, partitionSkips, shadowedBy, failedSkillLines, shadowedSkillLines; internal/tui/transcript.go — flattenField, fieldBreaks;
internal/skills/skill.go — SkipError, ShadowedError; cmd/apogee/e2e_hostile_test.go — assertSkillSectionRows
**Tests:** Add `TestFailedLinesAndShadowedLines` in `internal/skills/report_test.go`, which covers one or many skips, a mix of failed and shadowed, and a field with an embedded newline. Existing TUI `/skills` tests and `assertSkillSectionRows` (`cmd/apogee/e2e_hostile_test.go`) pass unchanged.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/skills/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Skill' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Hostile' ./cmd/apogee/`
**Commit:** `refactor(skills): move the skip-report wording beside Catalog.Skipped`

## 4. Headless names skills that failed to load, before submit

**What:** Depends on item 3. Covers the headless half of `firing-skill-skips-silent`.
**Goal:** In a project with a malformed SKILL.md, a headless run prints the failed-skill report (`skills.FailedLines`, escape-stripped) on stderr before the model is called. A run with no failed skills prints nothing new, and a shadowed skill adds no line.
**Approach (assumed at the header base):** In `cmd/apogee/wire_firing.go`, extract the `skills.Sources{…}` construction from `bindFiringConfig` into one helper (`firingSkillProvider(...)`), and keep using it there when `firingInputs.skills` is nil. `runHeadlessBody` builds the provider with that helper, passes it as `firingInputs.skills`, and prints `FailedLines(provider.Skipped())` through `sanitize.StripEscapes` with `cmd.PrintErrln` before `raise`. Update the "skipped WITHOUT notice" paragraph in `internal/run/doc.go`: a per-ref miss is still silent, but a skill that fails to load is reported by the Driver. Add one sentence to `docs/manual/headless.md`.
**Regression guard.** the helper's signature is settled in item 4 as firingSkillProvider(opts, roots) taking the Firing's layered (project-adopted) options and resolved roots, so bindFiringConfig, headless and the daemon (item 5) all build the identical catalog bindFiringConfig builds at base; item 5 calls it with the same arguments its Firing passes to bindFiringConfig
Rewrite every comment claiming headless or the daemon passes a nil skill catalog (`grep -n 'skill catalog\|skills catalog\|pass nil' cmd/apogee/headless.go cmd/apogee/wire_firing.go cmd/apogee/daemonfire.go`): the `firingInputs.skills` doc (`wire_firing.go:70-73`) and the raise preamble (`headless.go:1105-1108`).
**Files:** cmd/apogee/wire_firing.go; cmd/apogee/headless.go; cmd/apogee/headless_test.go; internal/run/doc.go; docs/manual/headless.md
**Read first:** cmd/apogee/wire_firing.go — bindFiringConfig, firingInputs.skills, raise; cmd/apogee/headless.go — runHeadlessBody (raise call, plaintextKeyNotice/subAgentsFlagNotice prints); cmd/apogee/headless_test.go — recipeHeadless;
internal/skills/provider.go — NewProvider, Provider.Skipped; internal/run/doc.go — package doc "skipped WITHOUT notice"; internal/stubllm/log.go — Server.Requests
**Tests:** Add `TestHeadlessNamesASkillThatFailedToLoad` (stubllm, modelled on `recipeHeadless`; the project contains a SKILL.md of `"---\nnope: \"unbalanced\n---\nbody"`). It asserts the skill name on stderr ahead of the first stubllm request, and nothing on stdout. Add `TestHeadlessSaysNothingForAShadowedSkill`.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessNamesASkill|TestHeadlessSaysNothingForAShadowed' ./cmd/apogee/`
**Commit:** `feat(headless): name skills that failed to load before the run`

## 5. The daemon logs skills that failed to load, once per root

**What:** Depends on items 3 and 4.
**Goal:** On a Firing whose skill roots hold a malformed SKILL.md, the daemon writes the failed-skill report to its stdout log once per project root and report text. A second Firing with the same root and text logs nothing new. A changed report, or a different root, logs again.
**Approach (assumed at the header base):** In `fire` (`cmd/apogee/daemonfire.go`), build the provider with item 4's `firingSkillProvider`, pass it as `firingInputs.skills`, and send the joined `skills.FailedLines` to `latchProjectNotice(root, text)` (the existing `daemonWiring` latch, which sanitises for the log) before the Firing submits. Add one sentence to `docs/manual/daemon.md`.
**Regression guard.** `latchProjectNotice` is a pure bool latch and sanitises nothing: on a true latch, write the joined text through `daemonLogWriter{log: w.log}`, exactly as `firingOptions`' `logOnce` closure does (`daemonfire.go:358-363`).
Build the provider with `firingSkillProvider` from the Firing's layered `opts` (`firingOptions`), never `w.opts`, and rewrite the raise preamble's "No skills catalog …" sentence (`daemonfire.go:462-464`; same grep as item 4's guard).
The harness options leave `UseProjectSkills` false, so workspace skills go under `<ws>/.apogee/skills/<id>/SKILL.md`; the per-root half is pinned by one malformed skill in the HOME library (identical text on two roots).
**Files:** cmd/apogee/daemonfire.go; cmd/apogee/daemonfire_test.go; docs/manual/daemon.md
**Read first:** cmd/apogee/daemonfire.go — daemonWiring.fire, latchProjectNotice, firingOptions, daemonLogWriter.Write; cmd/apogee/daemon.go — daemonLog.line;
cmd/apogee/daemonfire_test.go — newDaemonFireHarness, TestDaemonFireSaysOnceWhenTheContextWindowIsUnknown; cmd/apogee/wire_firing.go — firingInputs.skills
**Tests:** Add `TestDaemonFireLogsAFailedSkillOncePerRoot` in `daemonfire_test.go`, modelled on `TestDaemonFireSaysOnceWhenTheContextWindowIsUnknown` (`newDaemonFireHarness`, stubllm). Plant one malformed skill in the home library (`<ConfigDir>/skills/<id>/SKILL.md`, ConfigDir set before `newDaemonFireHarness`): two Firings on one workspace log it once, and a Firing on a second workspace logs the identical text again. As a separate step, a malformed skill planted under one workspace's `.apogee/skills/` changes the text and logs again.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDaemonFireLogsAFailedSkill|TestDaemonFireSaysOnce' ./cmd/apogee/`
**Closes:** firing-skill-skips-silent
**Commit:** `feat(daemon): log skills that failed to load, once per project root`
