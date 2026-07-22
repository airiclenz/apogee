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

- [x] **3. `apogee probe` — the host half. — ✅ DONE (2026-07-22)** (DEPENDS: 1, 2.) The subcommand reports, without
  running an agent: OS/arch, confinement backend name + capability matrix + `AutoEligible`
  verdict, effective `confine-to-workspace` after host acknowledgement (`hostid`), workspace
  root, config home, endpoint reachability, `/v1/models` + llama.cpp `/props` discovery outcome.
  Reuse the selection/notice logic in `cmd/apogee/wire.go` by extraction, not duplication —
  `/confine status` (TUI) and `apogee probe` (CLI) must not drift apart. Closes the
  `TODO.md:370` residue (the TODO.md edit itself belongs to item 10). Acceptance: unit tests
  drive the report through a fake Confiner + `httptest` endpoint (reachable, unreachable,
  llama.cpp-shaped, bare-OpenAI-shaped); `make check` green.
  NOTES (2026-07-22): the "extraction, not duplication" clause was read at its word and cost
  three touches outside `cmd/apogee`, all recorded here: (a) BOTH `confinerBackendName` and
  `confinementDegradedNotice` MOVED out of `wire.go` into the new `internal/probe`
  (`probe.BackendName` / `probe.DegradedNotice`), taking their two unit tests with them —
  `wire.go`, `wire_test.go` and `confinement_e2e_test.go` now call the extracted functions;
  (b) `internal/tui`'s `/confine status` renders its capability-matrix line through
  `probe.CapabilityLine` (its local `confineAvailability` is gone), so the CLI and the TUI
  cannot word the matrix differently — wording is byte-identical, so the existing tui tests
  pass unchanged; (c) `provider.ModelInfo` gained `RuntimeContextWindow`, because the report
  must state the `/v1/models` and llama.cpp `/props` outcomes SEPARATELY and `Discover`
  previously folded the /props window into `ContextWindow` with no way to tell which probe
  answered — that field is what distinguishes the llama.cpp-shaped server from the
  bare-OpenAI-shaped one in the acceptance tests. Two report choices worth naming: the host
  report closes with the startup degradation notice VERBATIM (keyed on `domain.ModeAuto`,
  since the probe answers "what would auto do here?"), and the probe deliberately does NOT
  seed a starter config the way the root's RunE does — the host half writes nothing, pinned
  by a test. No CHANGELOG/TODO.md/README edits: the Phase-5 roll-up is item 10's. Sanity
  check on this host (`make` absent): `go build ./...`, `go vet ./...`, `go test -count=1
  ./...` (only the 5 known pre-existing failures), all six cross targets, `--help` exit 0
  (a Commands section now appears — the permitted delta), the ADR-0010 grep, and gofmt over
  LF copies of the changed files.

- [x] **4. `apogee probe` — model battery + behavioral fingerprint. — ✅ DONE (2026-07-22)** (DEPENDS: 2, 3.) The
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
  NOTES (2026-07-22): eight deviations from the item's literal text, each forced by a pinned
  source. (a) **`internal/provider` gained a logprobs pair** — `Request.LogProbs` (opt-in,
  emitted as OMITTED pointer fields so every existing caller's bytes are unchanged) and
  `RawResponse.TopCandidates` (the candidate tokens for the first generated position). The
  item requires "logprobs preferred when the server exposes them" and the client had no way
  to ask; this is the same shape of extension item 3 made for `/props`. (b) **The persisted
  record lives in `internal/library`, not `internal/probe`** (`proberecord.go`:
  `ProbeRecord`, `ProbeDir`, `Save`/`LoadProbeRecord`, 0700/0600, soft-degrade). The resolver
  is the record's real consumer and `library` must not import `probe` to read it — so the
  record and its `ProbeBatteryVersion` are homed at the lower layer and `probe.BatteryVersion`
  mirrors the constant; `probe` writes, `library` reads. (c) **`ResolveFingerprint(modelID)`
  is kept** as the two-rung wrapper and the full ladder is the new
  `ResolveFingerprintFrom(Sources{ModelID, Endpoint, ProbeDir})` — the middle rung needs the
  endpoint and the home, which the old signature cannot carry, and keeping the old name
  spared ~10 unrelated test call sites. (d) **`internal/agent/loop.go:156` adopts the new
  ladder too** (one call site, outside `cmd/apogee`): ADR 0021's consequences require the
  Library's keying and the Validated-set match to key IDENTICALLY, which is false if only the
  wire-time call site can reach Medium. It derives the probe dir from the injected
  `cfg.ConfigDir` — an empty one just removes the rung, never an ambient `~/.apogee`
  (ADR 0001). (e) **The behavioral tier promotes the ADVERTISED LABEL rather than minting a new
  one** — `Fingerprint` returns `{Label: <advertised label>, Confidence: Medium}`, and the
  feature vector becomes a separate *behavioral signature*
  (`probe:<battery>:<features>[:lp-<digest>]`) recorded beside the claim as EVIDENCE, never as
  a match key. A synthesised label (the first attempt's `probe:1:<label>:<features>`) matches
  no `validated.Entry.Key` and no user alias, so `apogee probe model` silently DEMOTED the
  model it ran on and dropped an aliased user's applying set — the opposite of ADR 0021 §4 /
  ADR 0016 §5. Chosen over "teach entries and aliases to match behavioral labels" on
  stability (a key encoding battery version + feature set + logprob exposure moves under the
  user's feet), maintainability (one key space across Low and Medium; nothing to dual-look-up
  or re-paste) and modularity (the evidence stays inside `library.ProbeRecord`, whose only
  consumer is the re-probe comparison). Consequences recorded in **ADR 0021's dated
  `## Amendment (2026-07-22)`** (§6 also carries a pointer): `ProbeRecordVersion` 1 → 2 (the
  record's `fingerprint` field is now `behavior`), pre-amendment records are unreadable and
  unmatchable, skipped with a warning naming the one-command fix (**re-run `apogee probe
  model`**), and NO migration tooling is built — the same note ships in `probe model --help`.
  §3's drift detection now compares the SIGNATURE (the label cannot move, by design), and
  `Model.effectLine` computes the counterfactual match at low confidence so it never claims a
  promotion that did not happen (an alias already applying the set) nor an effect a
  session-level off-switch will refuse (Bypass, `enable: false`, an explicit `mechanisms:`
  block — `SaveOutcome` gained `Promoted`/`Suppressed` for exactly these two). (f) **Two now-false doc comments in
  the pinned `internal/domain/fingerprint.go` were corrected** ("no resolver produces it yet"
  on `ConfidenceMedium`, and the resolver seam's Phase-5 wording) — the slot this item fills.
  (g) **CONTEXT.md's *Behavioral fingerprint* term was corrected in place** — one paragraph,
  not a roll-up: item 2 wrote it to ADR 0021 §6's original wording ("the model identity … a
  fuzzy feature match"), which deviation (e) makes false, and a pinned vocabulary entry that
  contradicts the shipped design is the same defect class as a stale ADR. Item 10's
  CHANGELOG/README/TODO.md roll-up is untouched. (h) **`probe model` refuses when neither
  `--model` nor the server names a model** (`errProbeModelNeedsLabel`): under (e) the label IS
  the identity, so with none there is nothing to key a claim on and the battery would spend
  tokens for a report that could record nothing.
  Also: `stateRoots` gained `probe`, `resolveValidatedSet` gained a `probeDir` parameter, and
  an incomplete battery mints NO fingerprint (a hole in the evidence must not become an
  identity). No CHANGELOG/README/TODO.md edits — the Phase-5 roll-up is item 10's. Sanity
  check on this host (`make` absent): `go build ./...`, `go vet ./...`, `go test -count=1
  ./...` (only the 5 known pre-existing failures), all six cross targets, `--help` exit 0,
  the ADR-0010 grep, and gofmt over LF copies of the changed files.

- [x] **5. DESIGN-CALL — Windows Confiner design → ADR 0020 + contract §Windows. — ✅ DONE (2026-07-22)** The merge
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
  NOTES (2026-07-22): the owner ratified the **Q** answers before this item ran — facility: a
  **restricted / Low-integrity token** (`CreateRestrictedToken` + Low integrity handed straight to
  `SysProcAttr.Token`) over AppContainer; floor: **Windows 10 1809 / build 17763 / Server 2019**.
  Both are recorded with reasoning in the new
  `docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md`,
  together with the accepted **disk mutation** (the box's writable half can only be expressed as a
  mandatory label on `WorkspaceRoot ∪ WritablePaths`, reverted on teardown) — the side effect
  landlock and seatbelt never have. Five deviations from this item's literal text, each authorized:
  (a) **there is NO re-exec helper.** This item's text (`:259`) and item 8's (`:291`) both assume a
  `confined_exec_windows.go` "mirroring the Linux 42-liner + the main.go sentinel path"; under the
  token design `Confine` sets `cmd.SysProcAttr.Token` and returns, `cmd.Path`/`cmd.Args` are
  untouched, and `maybeDispatchConfinedExec` gains **no** Windows arm (Linux needs its helper only
  because it must restrict *itself* then `syscall.Exec` in place — `confined_exec_linux.go:37` —
  and Windows has no such API; the restriction is handed to the process-creation call instead).
  ADR 0020 §1 and contract §9.2 state this plainly so item 8 is not written against the wrong shape.
  (b) **`internal/platform/confinetest` is POSIX-shaped and item 8 must widen it**: `sh -c` at
  `:130/:143/:160/:170` needs a `cmd /c` arm; row #4's `$HOME/.ssh` (`:60`) ports as *code*
  (`os.UserHomeDir()` already resolves `%USERPROFILE%`) but not as *intent*; and battery row #6
  (exec inheritance) is Linux-tagged today and must be **ASSERTED** under a token backend. All named
  in ADR 0020 §7 and contract §9.3 so item 8 does not discover it late. (c) the item asks for a
  "disposition table row", but §4's table is keyed on tool-class × mode, not on OS — so the row
  landed as **§9.1's host table** (which §4 cell a Windows host takes), plus a Windows bullet in
  §5's capability list and **two new Windows-only battery rows (#9/#10)** in §6.2. (d) the ratified
  floor's rationale is stated precisely rather than as "matching Go's own minimum": Go's
  supported-Windows floor is lower (Windows 10 / Server 2016), so ADR 0020 records 17763 as the
  oldest branch under any servicing (LTSC 2019), sitting *at or above* Go's floor — the number is
  the owner's, unchanged. (e) decision (b) forced two design outputs the item's list does not name
  but cannot do without: the **restore path** (an optional `io.Closer` on the backend deferred at
  the composition root — `domain.Confiner` does NOT change — plus a pre-labelling **journal** so an
  interrupted cleanup is recoverable and visible to `apogee probe host`), and an **amendment to
  contract §2.2's "performs no I/O"** clause, since the label pass is bounded, idempotent,
  once-per-box I/O inside `Confine`. Recorded from the pinned sources rather than re-decided:
  `NetworkEgress` is **false** on Windows and a non-empty `NetworkAllow` **fails closed** (mirroring
  `landlock_linux.go`'s `networkDenyDecision`); `AutoEligible()` stays FSWrite-only, so Windows is
  Auto-eligible anyway; the honest-capability **split** (`Capabilities()` probes the facility once
  at construction; a per-run labelling failure is a `Confine`-time `ErrConfinementUnavailable`
  feeding §4's precomputed fallback) is called out as the one structural difference from
  Linux/macOS; and the **below-floor path is recorded UNTESTED** (the execution box is build 26200).
  Two facts verified against the pinned deps while writing: `x/sys/windows` v0.45.0 carries the
  whole identity half (`SetTokenInformation`/`TokenIntegrityLevel`/`Tokenmandatorylabel`,
  `CreateWellKnownSid(WinLowLabelSid)`, `Set/GetNamedSecurityInfo`, `SecurityDescriptorFromString`,
  `RtlGetNtVersionNumbers`) but has **no** `CreateRestrictedToken` binding (one `advapi32` LazyProc
  for item 8) and **no** AppContainer surface at all; and `syscall.SysProcAttr` on windows does have
  the `Token` field the design rests on. Docs-only item: no code, and no CHANGELOG/README/CONTEXT.md
  (item 10's roll-up). Sanity check on this host (`make` absent): `go build ./...`, `go vet ./...`.

- [x] **6. Platform `Shell`/`Path` widening. — ✅ DONE (2026-07-22)** (DEPENDS: 5.) Retire the two `TODO(phase-5)`
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
  NOTES (2026-07-22): the widened surface is `Shell{Command, CommandLine, Quote, ScopeEnv}`
  and `Path{ExecExt, Contains}`, all of it in one untagged rule table
  (`internal/platform/host.go`) whose two rule sets are compiled on EVERY target — only
  `Current()`'s choice is build-tagged — so Windows semantics are table-tested from a Linux
  run and natively here. Four deviations from the item's literal text: (a) **no `LookPath`
  method.** `os/exec` already implements per-OS lookup including `%PATHEXT%` (verified
  natively: `exec.LookPath("go")` → `…\go.exe`), so a wrapper would be exactly the dead
  surface this item's acceptance forbids; `Path`'s doc comment records why, and `ExecExt`
  stays as the one lookup-shaped fact `os/exec` does not expose. (b) **`Shell` gained
  `CommandLine`, which the item does not name**, because shipping `Quote` without it would
  hand item 8 a trap: Windows has no argv at the syscall boundary, so `os/exec` joins argv
  with `syscall.EscapeArg`, which escapes an embedded quote as `\"` — a form cmd.exe does
  not understand. Measured on this host: an `exec.Command("cmd", "/c", …)` of
  `echo "hello world"` prints `\"hello world\"`, and a redirect to a quoted spaced path
  fails with "The
  filename, directory name, or volume label syntax is incorrect". `CommandLine` returns the
  verbatim process command line for `syscall.SysProcAttr.CmdLine` ("" on POSIX, where
  execve takes a real argv), which fixes both. (c) **two existing callers adopted** (the
  item's own "or are adopted in this item"): the terminal tool now carries
  `spec.cmdline` through the new `internal/tools/exec_cmdline_unix.go` / `_other.go` (the
  setter only ever sets `CmdLine` and creates `SysProcAttr` if absent, so it composes with
  item 7's teardown and item 8's `Token`), and `safeGitEnv` now runs through `ScopeEnv`, so
  a Windows git child gets `%SystemRoot%`/`%ComSpec%`/`%PATHEXT%`/the profile paths its
  POSIX-shaped allowlist never named — POSIX output is byte-identical, since the POSIX
  platform floor is empty by design. (d) **8.3 is "resolve, else refuse"** (ADR 0020 §6's
  "normalise or be rejected"): `Contains` treats a component as an alias only when it has
  the 8.3 *shape* (so `my~file.txt` stays comparable), expands it through an injected
  `longPath` seam — nil in the pure rule sets, `GetLongPathNameW` wired by `Current()` on
  Windows and walking up to the longest EXISTING prefix, since that API is undefined for a
  path that does not exist yet — and returns false when it cannot expand, which is the safe
  answer for both of ADR 0020's callers. Also normalised: `\\?\` and `\\?\UNC\`; refused as
  non-locations: `C:work` (drive-relative) and `\\.\…` (device). **Flag for item 8:**
  contract §9.3's "ask `platform.Current().Command(line)`" is NOT reachable from
  `internal/platform/confinetest` — `GOOS=darwin go vet ./internal/platform/` proves the
  cycle (`seatbelt_darwin_test.go` is `package platform` and imports confinetest), so item 8
  must pass the shell/quoting in from the caller or move those tests to `package
  platform_test`. TODOs retired: `platform.go:16`, `:32` and `platform_windows.go:10` (only
  item 7's `exec_pgroup_other.go:16` remains). No CHANGELOG/README/CONTEXT.md — item 10's
  roll-up; `internal/platform/doc.go`'s "Windows stub … ships unexercised" sentence was
  corrected in place, as it describes the code this item replaced. Sanity check on this host
  (`make` absent): `go build ./...`, `go vet ./...` (plus `GOOS=linux|darwin|windows go
  vet ./...`, which type-checks the tagged tests too), `go test -count=1 ./...` (only the
  known pre-existing failures), all six cross targets, `--help` exit 0, the ADR-0010 grep,
  and gofmt over LF copies of the changed files.

- [x] **7. Windows process-tree teardown (Job Objects). — ✅ DONE (2026-07-22)** (DEPENDS: 5.) Replace the leader-only
  stub in `internal/tools/exec_pgroup_other.go` (its `:16` TODO) with real job-object teardown
  killing the whole tree, honouring the same §2.4 contract `exec_pgroup_unix.go` implements
  (Cancel + WaitDelay so Wait can never wedge on a held pipe). Extract any decision logic into
  platform-neutral functions with unit tests; the syscall layer is exercised natively on the
  execution machine (Ground-truth NOTES) — a test that cancels a shell command spawning a child
  and asserts the whole tree died. Acceptance: `make cross` green; the native suite green; the
  §2.4 contract comment updated to describe both backends.
  NOTES (2026-07-22): the container is an unnamed **Job Object** created before `Start` with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, terminated by `cmd.Cancel`; `WaitDelay` is unchanged.
  Five deviations from the item's literal text: (a) **`setProcessGroupTeardown` now returns a
  `processTeardown`** and `runSubprocess` runs the cmd through the new `runWithTeardown`
  (Start → contain → Wait) instead of `cmd.Run()`. Forced by the facility: a process can only
  be assigned to a job AFTER `CreateProcess` returns (creating one directly into a job needs
  `PROC_THREAD_ATTRIBUTE_JOB_LIST` on a `STARTUPINFOEX`, which `syscall.SysProcAttr` cannot
  express, and a suspended start is unreachable because `os/exec` closes the initial thread
  handle), so the teardown needs the gap between Start and Wait that `Run` hides. POSIX returns
  `noTeardown{}` and `runWithTeardown` is byte-for-byte what `Run` does there. (b) **the
  platform-neutral decision function is `planTreeKill(started, treeHeld)`** (new untagged
  `exec_teardown.go`, table-tested on every OS) and **`exec_pgroup_unix.go`'s `Cancel` was
  switched onto it too** — semantically identical (POSIX passes `treeHeld=true` unconditionally,
  since `Setpgid` establishes the group at fork), but it is a POSIX edit that cannot be executed
  on this host; type-checked via `GOOS=linux|darwin go vet ./...` and left for the closeout Linux
  pass. Sharing it is what keeps the function from being dead code on the OS it was extracted
  for. (c) **`release()` clears `KILL_ON_JOB_CLOSE` before closing the handle**, so a process the
  command deliberately left running outlives a CLEAN run exactly as a backgrounded process
  outlives its POSIX group leader; the limit's real job is the crash path, and the cancel path
  terminates the job explicitly. Both halves are pinned by native tests. (d) **the Windows
  `syscallKill0` test stub became a real liveness probe** (`OpenProcess` +
  `GetExitCodeProcess`/`STILL_ACTIVE`), which is what makes the shared `pidAlive` helper usable
  here; `waitForPIDFile`'s deadline went 5s → 20s for PowerShell's cold start (it returns as soon
  as the file appears, so POSIX pays nothing). (e) **two behavioural tests, not one**:
  `TestTerminal_WindowsCancelKillsTheProcessTree` (cmd.exe → powershell → a DETACHED `cmd /c
  ping`, whose PID is recorded and asserted gone after cancel) and
  `TestTerminal_WindowsCleanRunLeavesADetachedProcessAlive` (the (c) claim). **Both were verified
  by negative control** — with `contain` stubbed out the first fails ("grandchild survived"), with
  the limit-clear removed the second fails ("died with the completed command") — so neither passes
  vacuously. Also: the residual assign-after-create window and the KILL_ON_JOB_CLOSE/clear
  rationale are documented in the code and in contract §2.4, which was rewritten as
  *Process-**tree** teardown* describing both backends (§9.2's teardown bullet re-worded to match);
  the last in-code `TODO(phase-5)` marker is now gone (item 10 verifies). No
  CHANGELOG/README/CONTEXT.md/technical-design edits — item 10's roll-up. Sanity check on this
  host (`make` absent): `go build ./...`, `go vet ./...` plus `GOOS=linux|darwin|windows go vet
  ./...`, `go test -count=1 ./...` (only the known pre-existing failures), all six cross targets,
  `--help` exit 0, the ADR-0010 grep, and gofmt over LF copies of the changed files.

- [x] **8. Windows Confiner implementation + wiring. — ✅ DONE (2026-07-22)** (DEPENDS: 5, 6, 7.) Implement ADR 0020:
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

  NOTES (2026-07-22): **the "re-exec helper" clause above is VOID** — ADR 0020 §1 (ratified
  after this item was written) deletes it. Under the token design there is no helper process,
  no `confined_exec_windows.go`, no argv sentinel and no argv rewrite: `Confine` mints nothing
  per call, sets `cmd.SysProcAttr.Token` and returns, and `maybeDispatchConfinedExec` gains no
  Windows arm (its `!linux` doc comment now says why). `confinerBackendName` likewise does not
  exist — `probe.BackendName` derives the label from the concrete type, so `*tokenConfiner`
  renders as "token" with no wiring at all; what wire.go DID need was the ADR 0020 §2 teardown
  hook, an optional `io.Closer` assertion deferred beside `rungs.Docs.Close()`.
  NOTES (2026-07-22): `internal/platform/confinetest` cannot import `internal/platform`
  (`seatbelt_darwin_test.go` is `package platform` ⇒ import cycle), so of contract §9.3's two
  routes the **shell is passed in from the caller**: `Probe`/`ProbeNetwork` gained a
  `confinetest.Shell` parameter that `platform.Host` satisfies, and the three call sites hand
  it `Current()`. The shell-DIALECT fragments the battery needs — the write line, the nested
  line, the profile escape target and `SysProcAttr.CmdLine` — are build-tagged inside
  confinetest (`lines_windows.go` / `lines_other.go`), because `platform.Host` models a
  shell's invocation and deliberately not its built-ins.
  NOTES (2026-07-22): correction #3 honoured as the fence's sharpest edge. The guardrail does
  NOT read `Path.Contains`'s false as "outside": it asks `hostRules.split` whether the path is
  comparable at all and REFUSES TO LABEL when it is not (unresolvable 8.3 short name, device
  path, drive-relative `C:work`), and refuses equally when a *protected location* cannot be
  resolved. `Current()` gained an unexported `currentRules()` twin so the backend can reach
  the rule table rather than the `Host` interface.
  NOTES (2026-07-22): correction #4 honoured. The battery now runs `cmd /c` natively via
  `Current().Command`/`CommandLine`/`Quote` (the verbatim command line is mandatory — os/exec's
  `EscapeArg` joining turns the redirect's quotes into "The filename, directory name, or volume
  label syntax is incorrect"). Row #4 is renamed `write_under_user_profile_denied` and targets
  the profile ROOT on Windows, not `%USERPROFILE%\.ssh`, whose missing parent would fail cmd
  for the wrong reason. Row #6 is **asserted** under the token backend, not assumed: a nested
  `cmd /c` is denied, proving descendants inherit the restricted token.
  NOTES (2026-07-22): three implementation calls ADR 0020 leaves open. (a) The label journal
  lives at `<apogee home>/confinement/labels-<pid>.json` — per-PID so two concurrent apogees
  cannot overwrite each other's record — and the home is the DEFAULT `~/.apogee`, because
  `NewConfiner()` is the no-arg per-OS selector every backend shares and threading `--config`
  would ripple into Linux/macOS. (b) Construction's recovery reads one directory that normally
  does not exist and writes only when a crashed run actually left labels, so `apogee probe
  host` stays free/offline/read-only (ADR 0021 §1, verified live); a journal whose owning PID
  is still ALIVE is skipped, or a second apogee would un-fence a running one. (c) Teardown
  clears a label with a NULL SACL (`"S:"`) rather than `UNPROTECTED_SACL_SECURITY_INFORMATION`,
  which trips the `SeSecurityPrivilege` check and fails for an ordinary user; the result is
  behaviourally identical to the pre-run state and is asserted as such.
  NOTES (2026-07-22): the label pass fails CLOSED on a box root (unreadable, unwritable,
  SACL-less, guardrailed ⇒ `ErrConfinementUnavailable` ⇒ contract §4's forced Gate) and
  TOLERATES a failure on an individual descendant — one locked file becomes read-only to the
  confined child, which must not gate a whole session. Symlinks and reparse points are skipped:
  `SetNamedSecurityInfo` follows them, so labelling one would mutate a target outside the box.
  NOTES (2026-07-22): ADR 0020 §2's "`apogee probe host` reports an outstanding journal" is
  implemented here (it is not item 10's doc roll-up): `platform.ConfinementResidue(home)` plus
  an injected `probe.Inputs.Residue`, rendered as a `labels:` line under the confinement block.
  NOTES (2026-07-22): MEASURED COST of the disk mutation, which ADR 0020 accepted but did not
  quantify: ~1 ms per object, so a synthetic 5,051-object tree took **5.2 s to label and 2.2 s
  to revert**. It is paid once per box (the first confined command of a session) and once at
  shutdown, but a workspace with a large `.git`/`node_modules` will make that first `Confine`
  visibly block. Recorded here rather than tuned, because pruning the walk is a change to the
  ratified box semantics; if it needs a remedy the cheap ones are a startup notice or excluding
  ignored trees, and that is an owner call, not this item's.
  NOTES (2026-07-22): LIVE EVIDENCE on this host (windows/arm64, build 26200, go1.26.5).
  Escape battery natively green — in-box and writable-path writes land, out-of-box, user-profile
  and nested-exec writes all die with "Access is denied." and no file. The whole PRODUCT path
  was proven too (a temporary `internal/tools` test, run and removed): the real `Terminal` tool
  under `platform.NewConfiner()` wrote inside the box and was denied outside it with exit 1,
  with the item-7 Job Object and the verbatim command line composing unchanged. The degradation
  notice is GONE — `apogee probe host` now prints `backend: token (fs-write: available ·
  network: unavailable)` / `auto: eligible` and no notice. The below-floor path stays UNTESTED
  (no such host exists here), exactly as ADR 0020's consequences record.

- [x] **9. Proxy / OpenCode-bridge retirement record. — ✅ DONE (2026-07-22)** Confirm by grep that this repo's only
  proxy references are the `@pin` provenance comments (list them in the item's NOTES — they are
  preserved verbatim, per Ground truth); record the retirement decision as a CHANGELOG
  `[Unreleased]` entry (decided in merge-plan §6 #4, executed as: not ported, remains in
  apogee-sim history, apogee *is* the integration); note in the merge plan's Phase 5 bullet that
  retirement is recorded. The apogee-sim-side archival is explicitly out of scope (owner, other
  repo). Acceptance: grep transcript in NOTES; CHANGELOG entry present; no code changed.
  NOTES (2026-07-22): grep transcript, run on the Windows execution machine (Git Bash).
  **Command 1** — `grep -rniE "proxy|opencode" --include="*.go" .` → **45 matches, three
  disjoint buckets, none of them live**: (a) the **`@pin` provenance comments naming apogee-sim's
  `internal/proxy`** — 24 lines in 17 files, all under `internal/mechanisms/`, PRESERVED VERBATIM:
  `autofix.go:35`, `cachedcontent.go:12`, `catalogue.go:46`, `empty_response.go:18`,
  `errorenrich.go:13`, `filehint.go:19`, `grammar.go:13,27,68,132`, `historyhints.go:22`,
  `offramps.go:11`, `readloop.go:14,20`, `readrepeat.go:14`, `robustness.go:15,23,49`,
  `syntax.go:18,26`, `toolloop.go:16`, `tool_result_cap.go:99`, `tool_use_enforcer.go:19`,
  `validate.go:21` (the pin token sits on the comment's continuation line for `robustness.go:49`
  and `syntax.go:18,26`, so `grep "@pin" | grep proxy` alone shows 21 of the 24); (b) four
  narrative history mentions carrying no `@pin` token — `internal/domain/mechanism.go:53`
  ("the proxy could not host it"), `internal/mechanisms/robustness.go:18`,
  `truncate_history.go:32`, `grammar_test.go:93`; (c) the unrelated word senses — self-regulation's
  "proxy signals" (`internal/agent/dispatch.go:57`, `loop.go:441,809`,
  `selfreg.go:7,17,50,102,169,232,248`, `selfreg_test.go:4`) and the `Proxy-*` hop-by-hop header
  refusals (`internal/tools/http_request.go:57,58,72,73,74`, `network_test.go:190`). **Command 2**
  — `grep -rniE "opencode" . --exclude-dir=.git` → **no source match at all**: only docs
  (`CONTEXT.md:635`, `docs/plans/implementation-plan-apogee-merge.md:133,449,492`, this plan) plus
  the gitignored build artefact `apogee.exe`, whose hit is the Go runtime symbol
  `initOpenCodedDefers` — a false positive. **Command 3** — `git ls-files | grep -iE
  "proxy|opencode"` → empty (no proxy/bridge/plugin file is tracked here). Conclusion: nothing
  in this repo calls, embeds, spawns or speaks to a proxy; the retirement is a recording task, as
  the plan says. Outputs: `CHANGELOG.md` gains a `[Unreleased]` → `### Removed` entry (item 10
  adds the rest of the Phase 5 roll-up under the same heading) and the merge plan's Phase 5
  retirement bullet now states the record exists. **No code changed**; sanity check was
  `go build ./...`.

- [x] **10. Docs roll-up — the single owning item for every cross-cutting amendment. — ✅ DONE (2026-07-22)** (DEPENDS:
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
  NOTES (2026-07-22): the named surfaces all landed — README (status, the Auto-blast-radius
  section incl. a Windows *token + disk-label* paragraph carrying item 8's measured ~1 ms/object
  cost, a new `## Diagnosing a host — apogee probe` section, the six-target cross note, and the
  closing Note), `CONTEXT.md:37`, `technical-design.md`'s `:105` note plus §5's *Platform
  (shell/path)* and *CLI/headless/probe* rows, `TODO.md`'s degradation-residue block (the
  `apogee probe` bullet struck through and CLOSED, plus a closing note that the notice now
  narrows rather than fires on every Windows host), the merge plan's Phase 5 (a `*Status: ✅
  Complete*` line pointing at this plan, ✅ on each bullet with a "shipped as" clause, and the
  deliverable marked met with the still-owner-run remainder named), and the CHANGELOG
  `[Unreleased]` roll-up (`### Added` + `### Changed` inserted ABOVE item 9's existing
  `### Removed`, which is untouched). `ISSUES.md` untouched. **Four amendments beyond the
  item's literal file list**, each inside this roll-up's own subject matter rather than a
  drive-by: (a) `CONTEXT.md`'s *Confinement* term named the backend trio as "seatbelt /
  landlock / **AppContainer**" and described only the Linux and macOS mechanisms — ADR 0020
  rejected AppContainer, so a pinned vocabulary entry contradicting the shipped design is the
  same defect class item 4 fixed for *Behavioral fingerprint*; the Windows token, the disk
  label and the not-claimed `NetworkEgress` are now stated there; (b) the merge plan's §3
  architecture row (`:265`) and §5 risk row (`:496`) both still said "Windows AppContainer
  (Phase 5)" — corrected in place, and the risk row marked RETIRED. The three remaining
  "AppContainer" mentions in that file (`:218`, `:251`, `:780`) are left verbatim: each is
  explicitly attributed to the superseded ADR 0004 and reads as history; (c)
  `confinement-execution-contract.md` §2.6 said `NewConfiner()` returns "`denyConfiner`
  elsewhere (Windows until Phase 5)" — that IS a doc claiming Windows Auto falls back
  unconditionally, which this item's acceptance forbids, so a one-clause `*(Amended
  2026-07-22, §9)*` pointer was added rather than rewriting a P3.4 historical record; (d)
  §5's *Confinement* row was updated alongside the two rows the item names, since leaving it
  would have the same table describe a two-backend world one row after the Platform row
  describes three. Acceptance run: `grep -rn "TODO(phase-5)"` matches **no `.go` file at all**
  (and nothing outside this plan's own item text and two archived handoffs, which merely name
  the marker); `go build ./...` and `go vet ./...` green. Historical records were deliberately
  NOT rewritten — `technical-design.md:279`/`:308`, the merge plan's Phase-0 bullet and the
  archived phase plans describe what P0.5/P3 shipped at the time and are true as history.

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
