# Implementation plan — merge-plan Phase 5: cross-platform hardening & retirement

**Date:** 2026-07-22. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session **on the owner's Windows machine** (decided 2026-07-22 — see the dated NOTES under Ground
truth), forwarding skills: `coding-standards` (merge-plan Standing Requirement 1 — mandatory for
any new Go). One sub-agent per numbered work item below, verifier before commit, mark items done
in this file.

**Scope source:** `implementation-plan-apogee-merge.md` §4 "Phase 5 — Cross-platform hardening &
retirement". This plan is its numbered work-item breakdown; the merge-plan section stays the scope
authority. **Precedence for design questions:** ADR 0012 + `docs/design/
confinement-execution-contract.md` govern everything confinement-shaped; ADR 0016 + `internal/
domain/fingerprint.go` govern fingerprint identity; `../apogee-sim/mission.md` items 2–3 are the
pinned source for the probe/adaptive-complexity ideas. If an artifact produced by an earlier item
of THIS plan disagrees with those sources, the sources win — stop and consult, don't propagate.

## Why

Phase 5 is the last open phase of the merge plan. Three threads: (a) Windows is still the Phase-0
stub — `platform` ships a trivial `cmd /c` host, no process-tree teardown, and the deny Confiner,
so Auto on Windows runs fully gated; (b) `apogee probe` was promised twice — as the confinement
diagnosis subcommand (`TODO.md:370` residue) and as model capability probing that fills the
`ConfidenceMedium` fingerprint slot (`internal/domain/fingerprint.go:20`) — and neither exists
because the CLI has no subcommand structure at all; (c) the proxy / OpenCode-plugin retirement is
decided but never formally recorded. Deliverable (merge plan): cross-compiled binaries for
Win/Mac/Linux, Auto confined on all three.

## Ground truth (verified 2026-07-22 — anchors, not vibes)

- **The dev environment is Linux with no Windows runtime** (and a landlock-disabled kernel).
  Every item's Verify must run HERE: platform-neutral unit tests through injected seams
  (GOOS strings, env funcs, case-fold flags — the `internal/present` pattern) plus `make cross`
  (six-target `GOOS=… go build ./...`). Real-Windows behaviour goes on the owner-run checklist at
  the bottom, never silently assumed.
  NOTES (2026-07-22): superseded before execution — the owner decided to run this plan on a
  **real Windows machine** (native toolchain, **NOT WSL** — WSL is a Linux kernel and would
  defeat the point; `make` needs a POSIX shell such as Git Bash, else fall back to the
  underlying `go` commands the Makefile wraps). Consequences: (a) the Windows-tagged tests and
  the confinetest escape probes RUN natively as part of items 6–8's acceptance instead of
  deferring to the checklist; (b) the injected seams stay — they keep the logic table-testable
  on every OS, native execution is additional proof, not a replacement; (c) linux-tagged tests
  (landlock) cannot run there, so a final `make check` on the Linux devbox is REQUIRED before
  item 10 closes the plan — see the re-scoped checklist at the bottom.
- The widening TODOs: `internal/platform/platform.go:16` (Shell: environment-scoped execution,
  LookPath semantics, argument quoting) and `:32` (Path: case-folded containment for
  `ConfinementBox.WritablePaths`, PATH lookup). `internal/platform/platform_windows.go:10`
  (validate on real Windows). `internal/tools/exec_pgroup_other.go:16` (Windows job-object
  teardown; `exec_pgroup_unix.go` is the contract mirror, execution-contract §2.4).
- The deny Confiner and its honest `ErrConfinementUnavailable` story:
  `internal/platform/platform.go:49-77`. Backend selection + degradation notice:
  `cmd/apogee/wire.go` (`confinerBackendName`, `confinementDegradedNotice`). The Linux re-exec
  helper the Windows one must rhyme with: `cmd/apogee/confined_exec_linux.go` (42 lines) +
  `main.go`'s `maybeDispatchConfinedExec` sentinel intercept, which runs BEFORE Cobra and must
  stay first.
- CLI today: one Cobra root command, `Args: cobra.NoArgs`, zero subcommands
  (`cmd/apogee/root.go:104-183`). `CONTEXT.md:37` records "headless deferred — no subcommands
  ship yet".
- Fingerprint ladder: `internal/library/fingerprint.go:39-49` — weights hash ⇒ High, bare label ⇒
  Low, and the Medium slot is explicitly reserved: "The behavioral-probe tier (ConfidenceMedium)
  is Phase 5" (`internal/domain/fingerprint.go:20-23`). Validated sets auto-apply at ≥ Medium
  (ADR 0016), so filling this slot has real consequences — treat it with ADR care.
- Probe sources: `../apogee-sim/mission.md` item 3 (capability battery: tool call, JSON output,
  multi-step → auto-generated profile) and item 2 (adaptive prompt complexity via
  `capabilityTier`). Merge plan adds: the same battery yields the behavioral fingerprint (fuzzy
  feature match, NOT a response hash; logprobs preferred when exposed).
- Proxy references in this repo are exclusively `@pin` provenance comments in
  `internal/mechanisms/*.go` (e.g. `toolloop.go:16`, `grammar.go:13`). **They are history pins,
  not live references — no item may sweep or "clean up" them.**
- Host identity works on Windows already: `internal/platform/hostid.go` falls back to hostname
  when the machine-id files don't exist. No item needed.
- Next free ADR numbers: **0020** and **0021** (`docs/adr/` ends at 0019).

## Settled design (do not re-litigate in work items)

- **ADR 0012 stands.** The Windows Confiner exists to make Auto *confined* on Windows; until it
  lands, the deny backend plus "confine if you can, gate if you can't" is the correct behaviour,
  not a bug. `AutoEligible()` stays FSWrite-only; network stays open by default; NetworkEgress is
  claimed by a backend only if it can honestly enforce it.
- **`Confine` prepares in place** (rewrites the `*exec.Cmd`, never runs it) — the
  confinement-execution-contract's shape is fixed; the Windows backend implements it, it does not
  renegotiate it.
- **Nothing model-facing ships default-on without bench evidence** (the hard constraint,
  ADR 0009/0016). Whatever item 2 decides about adaptive prompt complexity, it cannot ship
  enabled.
- **Bare `apogee` stays byte-identical.** Adding subcommands must not change the no-args
  behaviour (TUI launch), the sentinel intercept order, or any existing flag. `--help` may gain a
  Commands section — that is the only permitted output delta, and `make check`'s `--help` gate
  must stay green.
- **Proxy retirement is a recording task in this repo.** The code lives in apogee-sim's history;
  archival there is the owner's business in that repo, out of scope here.

## Work items

Each item is one sub-agent's task: read the named files first, implement, test, `go vet` + run the
package tests, then mark the item `[x]` here. Follow existing idiom religiously — comment density
and `doc.go` conventions are load-bearing. ADR 0010: `internal/*` may depend only on
`internal/domain` downward, never the root facade. Any authorized deviation from item text lands
as a dated `NOTES (YYYY-MM-DD):` line under the item.

- [x] **1. CLI subcommand skeleton. — ✅ DONE (2026-07-22)** Restructure `cmd/apogee` so the Cobra root accepts
  subcommands while bare `apogee` (and every existing flag/env path) behaves byte-identically.
  Read `cmd/apogee/main.go`, `root.go`, `wire.go` first; `maybeDispatchConfinedExec` must remain
  the first thing `main` does, before Cobra parses anything. No subcommand is added here beyond
  an empty registration seam (`probe` arrives in items 3–4); `Args: cobra.NoArgs` on the root
  RunE is retained for the bare invocation. Acceptance: `./apogee --help` shows the same flags
  (a Commands section may appear); all existing `cmd/apogee` tests pass unmodified except any
  that assert the exact `--help` byte string (update those minimally, noting why); `make check`
  green.
  NOTES (2026-07-22): no existing test needed changing — the seam ships EMPTY, so Cobra adds
  neither its `help` nor its `completion` child and `--help` is byte-identical (no Commands
  section yet; the first real subcommand is what makes one appear). `make` is absent on the
  Windows execution machine, so the gate ran as the commands the Makefile wraps: `go vet ./...`,
  `go build ./...`, `go test -count=1 ./...`, all six cross targets, `--help` exit 0, and gofmt
  over LF copies of the changed files (the checkout is `core.autocrlf=true`, so `gofmt -l .`
  flags every file in the repo on this machine — an environment artefact, not a formatting
  defect). Four test failures are pre-existing on this host and untouched by this item —
  `TestSaveHostAcknowledgement_PreservesTheFileMode` (POSIX file modes), `TestAutofix…`,
  the two `TestDiagnostics_…GoVet…`, `TestFoldActivityClockRunsPerPhrase` — confirmed by
  re-running them on a stashed (clean) tree.

- [x] **2. DESIGN-CALL — `apogee probe` scope → ADR 0021. — ✅ DONE (2026-07-22)** Reconcile the two probe stories into
  one command design and write `docs/adr/0021-*.md` (house style; read 0019 and 0016 for form)
  plus CONTEXT.md vocabulary (*Probe*, *capability tier*, *behavioral fingerprint*). Sources to
  reconcile: `TODO.md:370` (host/confinement diagnosis "without running an agent"),
  `../apogee-sim/mission.md` items 2–3, the merge-plan Phase 5 probe paragraph, and the
  `ConfidenceMedium` slot contract (`internal/domain/fingerprint.go:20`).
  **Q1:** one `apogee probe` that reports host always and model when the endpoint is reachable,
  or split subcommands (`probe host` / `probe model`)? **Q2:** does the model probe persist
  anything (write a suggested `model-profile` block? record the behavioral fingerprint for
  Library keying?) or is v1 print-only, persistence a recorded follow-on? **Q3:** adaptive
  prompt complexity — build now as a default-off catalogued Mechanism (then it needs a
  bench-validation entry in the catalogue's Table B), or record as a TODO.md follow-on and ship
  only the `capabilityTier` signal from the probe report? (Recommend the latter —
  validated-not-assumed.) Acceptance: ADR answers all three with the owner's decisions;
  cross-references 0012 (host half reports the confinement matrix), 0016 (Medium-confidence
  consequences) and ADR 0009 (why complexity adaptation is bench-gated).
  NOTES (2026-07-22): the pinned source `../apogee-sim/mission.md` was read directly (it is
  reachable now) and its items 2–3 agree with this plan's paraphrase — nothing to reconcile
  against. Owner's answers, recorded in ADR 0021: **Q1** `probe` parent whose own RunE prints
  the host report, plus `probe host` (scriptability) and `probe model` children — the host half
  is free/offline/read-only, the model half costs live calls AND writes, so it is an explicit
  act. **Q2** the model probe DOES persist a versioned, owner-private (0700/0600) probe record
  at `ConfidenceMedium`, soft-degrading on any defect, keyed on endpoint + advertised label +
  timestamp; print-only was rejected because identity resolves through a pure offline call
  (`cmd/apogee/validatedsets.go:38`), so a Medium tier that is never written down could never be
  observed — ADR 0016's 2026-07-19 defect. Suggested `model-profile` knobs are PRINTED as
  paste-ready YAML (the `offerNotice` precedent), never written to config; the ADR states
  explicitly that writing a Medium fingerprint promotes a model from "offer" to auto-apply
  (ADR 0016 §5) and mandates `--no-save`. **Q3** adaptive complexity is NOT built — only the
  `capabilityTier` signal ships. Two scope notes: (a) TODO.md gained the *adaptive prompt
  complexity* follow-on section here, because recording it is Q3's own answer — item 10's TODO
  edits (the `:370` residue and the degradation block) are untouched; (b) no CHANGELOG entry —
  the Phase-5 roll-up is item 10's, and this item changes no behaviour. Sanity check:
  `go build ./... && go vet ./...` (docs-only change; `make` is absent on this host).

- [ ] **3. `apogee probe` — the host half.** (DEPENDS: 1, 2.) The subcommand reports, without
  running an agent: OS/arch, confinement backend name + capability matrix + `AutoEligible`
  verdict, effective `confine-to-workspace` after host acknowledgement (`hostid`), workspace
  root, config home, endpoint reachability, `/v1/models` + llama.cpp `/props` discovery outcome.
  Reuse the selection/notice logic in `cmd/apogee/wire.go` by extraction, not duplication —
  `/confine status` (TUI) and `apogee probe` (CLI) must not drift apart. Closes the
  `TODO.md:370` residue (the TODO.md edit itself belongs to item 10). Acceptance: unit tests
  drive the report through a fake Confiner + `httptest` endpoint (reachable, unreachable,
  llama.cpp-shaped, bare-OpenAI-shaped); `make check` green.

- [ ] **4. `apogee probe` — model battery + behavioral fingerprint.** (DEPENDS: 2, 3.) The
  capability battery per `mission.md` item 3 — native tool call, JSON/structured output,
  multi-step tool chain — produces (a) a capability report with suggested profile knobs
  (`tool-call-format`, `thinking.style`, …), and (b) a behavioral `domain.ModelFingerprint` at
  `ConfidenceMedium` — a fuzzy feature match over battery outcomes, logprobs preferred when the
  server exposes them, never a response hash (merge-plan Phase 5 wording governs). Wire the
  resolver fallback ladder in `internal/library/fingerprint.go` (High → **Medium via stored
  probe result** → Low) exactly as its `:40` comment reserves. Persistence per the item-2
  decision. Acceptance: `httptest` fake server scripted per battery outcome (all-pass,
  no-native-tools, JSON-fails) drives deterministic reports and fingerprints; a live smoke
  behind `APOGEE_LIVE_ENDPOINT` is added but skipped by default; `make check` green.

- [ ] **5. DESIGN-CALL — Windows Confiner design → ADR 0020 + contract §Windows.** The merge
  plan's risk table calls this "own design session" — treat it as one. Decide the facility
  (AppContainer vs. restricted token vs. Job-Object-only) and what `ConfinementCaps` it can
  HONESTLY report (per Settled design, FSWrite is the Auto gate; claim NetworkEgress only if
  truly enforced); the helper re-exec shape mirroring `cmd/apogee/confined_exec_linux.go`; the
  minimum Windows version and the degradation story below it (older host ⇒ deny backend +
  notice, same as today); what case-folded `WritablePaths` containment the backend needs from
  `platform.Path` (this feeds item 6's surface). Deliverables: `docs/adr/0020-*.md` and a new
  Windows section in `docs/design/confinement-execution-contract.md` (disposition table row,
  probe expectations). **Q:** facility choice + minimum Windows version — recommend one with
  rationale, owner decides.

- [ ] **6. Platform `Shell`/`Path` widening.** (DEPENDS: 5.) Retire the two `TODO(phase-5)`
  markers at `internal/platform/platform.go:16` and `:32` by widening exactly to the surface
  items 7–8 and the existing `terminal` tool consume — environment-scoped execution, executable
  lookup (`LookPath` semantics + `ExecExt`/PATH), argument quoting, and the case-folded
  containment helper ADR 0020 specifies. **No dead surface: the verifier rejects any added
  method with no caller landed by the end of this plan.** POSIX and Windows implementations
  both; Windows semantics unit-tested on Linux through injected seams (case-fold as a pure
  function, GOOS as a parameter — the `internal/present` pattern). Acceptance: table tests for
  both hosts including case-fold containment edges (`C:\Work` vs `c:\work`, short/long case
  collisions); `make cross`; existing callers compile unchanged or are adopted in this item.
  Per the Ground-truth NOTES, the Windows-tagged tests also run natively on the execution
  machine, not just compile.

- [ ] **7. Windows process-tree teardown (Job Objects).** (DEPENDS: 5.) Replace the leader-only
  stub in `internal/tools/exec_pgroup_other.go` (its `:16` TODO) with real job-object teardown
  killing the whole tree, honouring the same §2.4 contract `exec_pgroup_unix.go` implements
  (Cancel + WaitDelay so Wait can never wedge on a held pipe). Extract any decision logic into
  platform-neutral functions with unit tests; the syscall layer is exercised natively on the
  execution machine (Ground-truth NOTES) — a test that cancels a shell command spawning a child
  and asserts the whole tree died. Acceptance: `make cross` green; the native suite green; the
  §2.4 contract comment updated to describe both backends.

- [ ] **8. Windows Confiner implementation + wiring.** (DEPENDS: 5, 6, 7.) Implement ADR 0020:
  `confiner_windows.go` (narrow `confiner_other.go`'s build tags so Windows selects the real
  backend), the re-exec helper (`confined_exec_windows.go` mirroring the Linux 42-liner + the
  `main.go` sentinel path), honest `Capabilities()`, prepare-in-place `Confine`. Wire
  `confinerBackendName` in `cmd/apogee/wire.go`; the degradation notice must vanish on a capable
  Windows host and persist below the minimum version. Add build-tagged escape-probe acceptance
  tests through `internal/platform/confinetest` — **executed natively in this item** (Ground-truth
  NOTES), the Windows counterpart of `landlock_linux_test.go`/`seatbelt_darwin_test.go`.
  Acceptance: `make cross` + `GOOS=windows go vet ./...` green; the native suite green
  **including the escape probes**; deny-backend behaviour on remaining OSes untouched
  (`confiner_other.go` still compiles for them; re-verified at the closeout Linux pass).

- [ ] **9. Proxy / OpenCode-bridge retirement record.** Confirm by grep that this repo's only
  proxy references are the `@pin` provenance comments (list them in the item's NOTES — they are
  preserved verbatim, per Ground truth); record the retirement decision as a CHANGELOG
  `[Unreleased]` entry (decided in merge-plan §6 #4, executed as: not ported, remains in
  apogee-sim history, apogee *is* the integration); note in the merge plan's Phase 5 bullet that
  retirement is recorded. The apogee-sim-side archival is explicitly out of scope (owner, other
  repo). Acceptance: grep transcript in NOTES; CHANGELOG entry present; no code changed.

- [ ] **10. Docs roll-up — the single owning item for every cross-cutting amendment.** (DEPENDS:
  all.) README (Windows Auto now confined on capable hosts + probe usage + six-target cross
  note); `CONTEXT.md:37` ("no subcommands ship yet" → `probe` ships, `headless` stays
  deferred); `docs/design/technical-design.md` §5 rows (Platform shell/path, CLI/probe — and its
  `:105` "Windows keeps denyConfiner until Phase 5" note); `TODO.md` (close the `:370` probe
  residue; update the confinement-degradation residue block that references it); the merge plan
  (mark Phase 5 ✅ with its deliverable line, pointing at this plan). CHANGELOG `[Unreleased]`
  gets the full Phase 5 roll-up. `ISSUES.md` untouched. Acceptance: every in-code
  `TODO(phase-5)` marker is gone (`grep -rn "TODO(phase-5)"` returns nothing — items 6–8
  removed them; this item only verifies); no doc still claims Windows Auto "falls back to
  asking" unconditionally.

## Non-goals / deferred (record, don't build)

- **Adaptive prompt complexity as a shipping Mechanism** — per the item-2 decision; if built at
  all it is default-off and bench-gated (ADR 0009); otherwise a TODO.md follow-on.
- **`apogee headless`** — stays deferred (CONTEXT.md), the subcommand skeleton merely makes it
  possible later.
- **apogee-sim repo archival** of the proxy/plugin code — owner, other repo.
- **Network confinement on Windows beyond ADR 0012's open-by-default stance.**
- The parked product backlog (`/server`, session UI, inspector, tool×mode matrix, …) — separate
  TODO.md threads, not Phase 5.

## Owner-run checklist (after implementation — the plan is not "done done" until these)

Re-scoped 2026-07-22 (Ground-truth NOTES): native execution on the Windows machine folds the
former "real Windows target" validation — shell/path behaviour, job-object tree kill, the
escape probes, `platform_windows.go:10`'s TODO — into items 6–8's acceptance. Still outstanding:

- **Closeout Linux pass — REQUIRED, gates item 10:** `make check` on the Linux devbox; the
  linux-tagged landlock tests cannot run on the Windows machine.
- **Live Auto-confined deliverable run on Windows** — during or after item 8 if an LLM endpoint
  is reachable from that machine; otherwise it stays here as owner-run.
- **Degradation notice on a below-minimum-version Windows host** — only if such a host exists;
  otherwise record as untested in ADR 0020's consequences.
- **macOS cross-binary smoke:** `--help` + a trivial session (Linux and Windows are covered by
  the two execution machines).
- **Pre-existing, NOT Phase 5 scope** (CHANGELOG "known post-release verification", carried for
  visibility): Linux landlock live enforcement on a landlock-enabled kernel; the Linux/macOS
  live Auto-confined runs.
