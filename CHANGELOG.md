# Changelog

All notable changes to Apogee are recorded here. The public Go API follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) from `v1.0.0`
onward (ADR 0001 §consequences, as amended at the Phase-3 cut): Events and
hook points stay **additively extensible**, so a new Event variant or hook
point is a **minor** bump, not a breaking change.

## [Unreleased]

Post-`v1.2.0`, **additive** (minor) — the `guided_decomposition` Mechanism (ADR 0014), built
up item-by-item behind the Mechanism catalogue and shipped **default-off** (the bench flips it
on, not this work).

### Guided decomposition (`guided_decomposition`, default-off)

- **The `Requires` stacking relation.** `MechanismDescriptor` gains a `Requires []MechanismID`
  field — the dual of `IncompatibleWith` — and `New` now runs the new
  `MechanismRegistry.ValidateRequirements` gate alongside the ordering and incompatibility
  checks: every registered Mechanism's required peers must also be registered, else the new
  `ErrMissingRequirement` sentinel refuses construction ("X requires Y — enable both or neither;
  they are benched as a stack"), the same startup-gate posture as `ErrOrderingCycle`. It is
  enable-time only (ADR 0014 §4): live suppression of a required peer mid-Session is not
  re-checked. (`internal/domain`, `internal/agent`.)
- **Hook-visible loop depth and post-response tool-call synthesis.** `LoopView` gains a
  `Depth()` method — 0 for a top-level Agent, parent+1 for a sub-agent (ADR 0013) — so a gate
  can steer only the primary call, never a nested delegation (ADR 0014 §5); the loop stamps it
  from the Agent's nesting level through the new engine-seam `Request.SetDepth`. `Response`
  gains `AppendToolCall`, letting a post-response Mechanism add a `sub_agent` delegation the
  model never emitted: the loop reads it back through `ToolCalls()`, commits it on the assistant
  message, and dispatches it through the full per-call Resolution (the ADR 0013 recursion point,
  driving a real nested child) exactly like a model-emitted call. An in-place response mutation
  combined with a returned `ActionDefer` both take effect. (`internal/domain`, `internal/agent`.)
- **The `guided_decomposition` gate and enumeration steer (pre-request half).** The new
  `guided_decomposition` Mechanism (`internal/mechanisms`, catalogue-registered, ordered `After
  toolfilter`) lands its pre-request half: a strikes-3 proactive-nudge, `IncompatibleWith`
  `decompose` and `Requires` `tool_result_cap`. On an oversized PRIMARY call — a known window,
  top-level depth, a `sub_agent` on the final menu, and a measured size signal (a fresh user message
  over the `FileContext` budget, or mid-Exchange history over the `History` budget with the model
  still calling tools) — it injects an enumeration steer asking for ONLY a numbered list of at most 7
  self-contained subtasks. The steer is marker-idempotent and stays quiet once a fan-out directive is
  already steering (no double-steer); the post-response intercept and serialized follow-through land
  next. (`internal/mechanisms`.)
- **The intercept and serialized follow-through (post-response half).** `guided_decomposition` gains
  its `PostResponse` half on the same struct. On the enumeration Turn — the steer outstanding, the
  model's reply a bounded (2..12) subtask list with no tool calls — it parses the list, synthesizes
  the FIRST `sub_agent` delegation onto the response (the enumeration text left verbatim), and defers
  a remaining-items directive carrying the rest plus a compact-report hygiene ask (ADR 0014 §4). Each
  following Turn re-derives the remainder from honest history — the model's own list message and the
  `sub_agent` CALLS in the conversation (never the child results, so a report capped by
  `tool_result_cap` leaves the cursor exact) — minus the just-delegated task, and re-defers the
  shrunken directive until none remain. It carries no per-Mechanism state (snapshot/resume-safe and
  suppression-clean), declines an out-of-bounds list whole, and no-ops on anything else.
  (`internal/mechanisms`.)
- **Wire-up proof and end-to-end fan-out acceptance.** Loop-level tests drive the whole stack through
  the REAL loop with nothing of the Mechanism mocked: an oversized primary call gets the enumeration
  steer, the model's list is intercepted into a REAL nested `sub_agent` fan-out serialized one
  delegation per Turn (child events nesting at `Depth == 1`), the remaining-items directive rides each
  following request and shrinks, and the Exchange ends on a no-tool synthesis with the enumeration
  verbatim and all three child reports in honest history. A snapshot taken mid-fan-out round-trips the
  pending directive (`conversationJSON.Deferred`) and a resumed Agent completes the fan-out; Bypass is
  the silent control arm (ADR 0014 §1); and a cancel during a child rolls back only that parent Turn
  (ADR 0013 §5). Config-surface tests pin the ADR 0014 §4 stacking gates: enabling `guided_decomposition`
  without `tool_result_cap` is the `ErrMissingRequirement` startup error, the stack boots, and adding
  `decompose` is the incompatibility error. The commented `mechanisms:` example in the config template
  gains the stack. (`internal/agent`, `cmd/apogee`.)
- **Docs close-out.** The feature's cross-cutting doc edits are reconciled under this one heading:
  CONTEXT.md's Guided decomposition entry now disambiguates it from the shipped `decompose` Mechanism
  (a prompt-shaping nudge — steers wording, not delegation; the two are declared incompatible), and
  ADR 0014 gains a dated Realisation note recording the decisions locked at implementation — queue
  delivery as one re-derived deferred directive per Turn, `IncompatibleWith: [decompose]`,
  registry-level `Requires` validation, verbatim enumeration text, and the 7/12 subtask bounds — plus
  the authorized per-item deviations. (Docs: `CONTEXT.md`, `docs/adr/0014`.)

### The public Mechanism enable surface (`Config.EnableMechanisms`, ADR 0015)

- **Catalogued descriptors become static, queryable data + a matchable unknown-ID sentinel.** Each
  catalogued Mechanism's `MechanismDescriptor` is now a single package-level value that both the
  built instance's `Descriptor()` returns and the catalogue registers beside the constructor
  (equality by construction), and a new `mechanisms.Descriptors()` returns every row — sorted by ID,
  duplicate-free, slice fields cloned — so a Mechanism's metadata is available without building one
  (the backing for the forthcoming public `CataloguedMechanisms()` query, ADR 0015 §3). A new
  `domain.ErrUnknownMechanism` sentinel is wrapped by `mechanisms.Build`'s unknown-ID error (which
  still names the known IDs), so a typo'd or deferred ID fails loudly AND matchably via `errors.Is`
  (ADR 0015 §4). (`internal/mechanisms`, `internal/domain`.)
- **`Config.EnableMechanisms` arms catalogued Mechanisms by ID at construction.** `Config` gains an
  `EnableMechanisms []MechanismID` field: `New` and `Resume` build each named catalogued Mechanism
  and merge it INTO `Config.Mechanisms` (a fresh registry when nil), so catalogued Mechanisms and
  bench experimental hooks coexist in one arm (ADR 0015 §1–2). The engine derives the build `Deps`
  the way `cmd/apogee/wire.go` does — a Library store rooted at `Config.LibraryDir` and Loaded only
  when `library` is enabled (never an ambient root; a corrupt/absent store degrades to empty and
  never blocks construction), the model fingerprint resolved once, and the grammar seam left inert —
  entirely internal (no `Deps` type on the public surface). IDs build in sorted order for a
  deterministic error surface, then the existing ordering/incompatibility/requirements gates run over
  the merged registry unchanged: an unknown ID (`ErrUnknownMechanism`), an ID listed twice or already
  pre-built (the already-registered rejection), and a half-armed `Requires` stack
  (`ErrMissingRequirement`) each fail `New`/`Resume`; an empty/nil list arms nothing (default-off).
  A spawned sub-agent inherits the parent's already-built registry, so it fires the same Mechanisms
  without re-building them. `cmd/apogee`'s own YAML→registry path is unchanged for now (it collapses
  onto this engine path in a follow-up). (`internal/domain`, `internal/agent`.)
- **`cmd/apogee` collapses to a YAML→ID-list producer.** `cmd/apogee/wire.go` no longer builds a
  registry: `buildMechanismRegistry` and the cmd-side `Deps` derivation (the Library store /
  fingerprint / `LookPath` wiring, now dead) are deleted. The composition root still validates EVERY
  `mechanisms:` key — enabled AND disabled — against the known catalogue at the startup boundary (a
  typo'd DISABLED key, which the engine never sees, must still fail loudly there), then hands the
  sorted enabled IDs to `Config.EnableMechanisms` and lets `New`/`Resume` build them (ADR 0015 §1).
  The YAML `mechanisms:` surface, the config template, and every user-visible behaviour are unchanged
  — the same loud errors refuse to boot at the same startup boundary (unknown key, half-stack,
  incompatibility), only the `%w` chain behind some of them moved from the cmd path onto the engine
  path. (`cmd/apogee`.)
- **The public Mechanism surface: descriptors, catalogue query, and matchable enable errors.** The
  root facade now exposes the enable surface an embedder needs: `MechanismDescriptor`, `Capability`,
  and `SuppressionPolicy` (with their constant values) are re-exported, and a new
  `apogee.CataloguedMechanisms()` returns every catalogued Mechanism's descriptor — sorted by ID,
  duplicate-free, slice fields cloned — so a host can read each Mechanism's Capability, suppression
  policy, and `IncompatibleWith` / `Requires` stacking relations and plan an `EnableMechanisms` arm
  (e.g. a leave-one-out arm by `Requires` traversal) WITHOUT building any Mechanism (ADR 0015 §3). The
  enable-time sentinels `ErrMissingRequirement` (the dual of `ErrIncompatibleMechanisms`) and
  `ErrUnknownMechanism` are re-exported so `errors.Is` matches them through the root (ADR 0015 §4).
  `Config.Mechanisms`' doc comment is reframed as the experimental-hook carrier that points at
  `EnableMechanisms` for catalogued enablement (the field keeps its name under v1 semver — no
  rename). Runnable godoc Examples arm the `guided_decomposition + tool_result_cap` stack and compute
  a leave-one-out arm from the catalogue query. (`apogee.go`, `internal/domain`.)
- **The bench-readiness contract becomes a true external-surface consumer.** `benchreadiness_test.go`
  now arms every arm through the PUBLIC enable surface — catalogued Mechanisms by ID via
  `Config.EnableMechanisms`, experimental hooks via `AddExperimental` — and no longer imports
  `internal/mechanisms` or `internal/library` or builds the catalogue by hand, so a separate module
  (apogee-sim) can now do everything this test does (ADR 0015 Consequences). It adds the acceptance the
  bench campaign needs, all through the root API: a half-armed `Requires` stack refuses construction
  (`apogee.ErrMissingRequirement`), a bogus ID refuses (`apogee.ErrUnknownMechanism`), the
  catalogued+experimental combined arm still co-fires both in deterministic order, and a leave-one-out
  arm set computed from `apogee.CataloguedMechanisms()` — the full compatible stack and every
  member-omitted arm — constructs successfully. (`benchreadiness_test.go`.)
- **Docs close-out.** The enable surface's cross-cutting doc edits are reconciled under this one
  heading: ADR 0015 gains a dated Realisation note recording the authorized implementation
  deviation — a spawned sub-agent inherits the parent's already-built registry (clearing
  `EnableMechanisms`) rather than rebuilding, and a degraded Library store degrades to empty
  rather than failing construction — and the README's Configuration section now names the public
  library enable surface (`Config.EnableMechanisms` / `apogee.CataloguedMechanisms()`) alongside
  the unchanged `mechanisms:` YAML path. CONTEXT.md is unchanged — the grill crystallised no new
  term (ADR 0015 Consequences / locked decision 7). (Docs: `docs/adr/0015`, `README.md`.)

## [1.2.0] — 2026-07-04

Post-`v1.1.0`, **additive** (minor) — Phase 4 merges the apogee-sim Mechanisms into the
loop (`docs/plans/archived/phase-4-detail-plan.md`; ratified catalogue at
`docs/design/mechanism-catalogue.md`). **No breaking change** (sanity-checked against the
`v1.1.0..HEAD` diff): the public facade (`apogee.go`) only *gains* symbols — the sole new
top-level export is `ErrIncompatibleMechanisms`; nothing exported is removed or re-typed. Every
other new surface is additive — new `Config` fields (the `mechanisms:` block and the `auto-compact`
key) plus the now-consumed `LibraryDir` root (a pre-`v1.1.0` field Phase 4 finally reads, not a new
field), new advisory `domain.Budget` fields, and new `domain` types
(`ModelFingerprint`, `FingerprintResolver`) that are *not* re-exported at the root. The one
changed signature (`domain.NewRequest`'s fired-ledger argument) is an internal engine seam, never
on the public surface — so this is a **minor** bump, not a major one.

### Catalogued Mechanisms now dispatch in a deterministic order behind the Bypass gate

- **Registered Mechanisms finally run.** A Mechanism added to the `MechanismRegistry` via
  `Add` used to be validated but never dispatched — only the bench's experimental hooks
  fired. Now, at each of the five hook points, the loop dispatches the catalogued
  Mechanisms **first**, in a deterministic total order (`MechanismRegistry.Ordered` — a
  topological sort of each Mechanism's `Before`/`After` `OrderingConstraints` with a stable
  tiebreak by canonical `MechanismID`, so a shuffled registration order yields identical
  output, ADR 0003), then the experimental hooks in registration order (unchanged). Each
  fires under the same recover boundary and emits a `MechanismFiredEvent` under its **real**
  `MechanismID` (experimental hooks keep the synthetic `experimental` attribution).
- **`Config.Bypass` now gates dispatch (ADR 0006).** Under Bypass, every catalogued
  non-`off-ramp` Mechanism is skipped — proactive-nudge and response-repair go silent —
  while `off-ramp` recovery guarantees still run; experimental hooks are never Bypass-gated
  (they are the bench's own instruments), and the structural context machinery (Budget,
  Compaction) is unaffected.
- **Incompatible Mechanisms fail loudly at construction.** `New` now also runs
  `MechanismRegistry.ValidateIncompatibilities`, returning the new
  `ErrIncompatibleMechanisms` sentinel when two registered Mechanisms declare each other via
  `MechanismDescriptor.IncompatibleWith` — the same startup-gate posture as
  `ErrOrderingCycle`, so a config that enables two mutually-exclusive Mechanisms is refused
  rather than silently running both. (`internal/domain`, `internal/agent`, root re-exports.)

### Mechanisms now self-regulate: effectiveness tracking, Adaptive Suppression, the Turn Budget

- **A catalogued Mechanism that is not helping is now withdrawn for the rest of the
  Session.** A per-Session tracker judges each Turn on proxy signals — a Turn is
  **productive** when it reads a new file or writes one (a tool error or an empty/no-op
  response is not). **Adaptive Suppression** (per Mechanism): a Mechanism that fires through
  three consecutive non-productive Turns is skipped at dispatch for the rest of the Session,
  with a clear-path that re-opens every Mechanism on the next productive Turn. **The Turn
  Budget** (global): after eight consecutive non-productive Turns every non-exempt Mechanism
  is withdrawn until productive activity resumes. A `SuppressionPolicy: exempt` off-ramp
  bypasses both — suppressing it would leave a failed Turn with no way out (ADR 0006).
- **`LoopView.Fired` finally answers.** The declared-but-inert per-Session fire counter now
  reports real fires, read live within a hook pass (a Mechanism sees a peer's fire from
  earlier in the same pass — the cross-Mechanism coupling seam). No new public surface: the
  tracker is internal to `internal/agent`; `domain.NewRequest` gains a `fired` ledger
  argument on the engine seam only.
- **Reset on Resume.** The tracker is per-Session and not serialized: a resumed Agent starts
  with clean suppression state (the accepted v1 posture — fresh state can only cause a
  withdrawn Mechanism to be re-tried, never wrongly withheld). (`internal/agent`,
  `internal/domain`.)

### A file-only `mechanisms:` config block wires the catalogue into the loop

- **Catalogued Mechanisms are now opt-in from `config.yaml`.** A new file-only `mechanisms:`
  block (no flag/env, like `mcp-servers` / `model-profile`) maps a canonical mechanism ID to
  `enabled: true|false`. Every Mechanism defaults **off** (D1 — default-off until bench-proven);
  a `true` entry turns one on. An **unknown ID is a loud startup error** listing the catalogue
  this build knows, so a typo'd key never silently disables a Mechanism. `--bypass` still wins:
  an enabled non-off-ramp Mechanism is not dispatched under bypass (ADR 0006 / the item-2 gate).
- **The catalogue constructor seam.** `internal/mechanisms` gains `Build(id, deps)` over a
  constructor table (`Deps` carries the construction-injected collaborators — D3; the Library
  store is nil until it lands). The composition root (`cmd/apogee`) drives the table for each
  enabled ID and folds the built Mechanisms into `Config.Mechanisms` before construction. The
  table ships **empty** — the port waves fill one row per Mechanism — so a config with no
  `mechanisms:` block behaves exactly as before. (`cmd/apogee`, `internal/mechanisms`, README +
  starter `config.yaml`.)

### Wave 1: the `validate` / `syntax` / `autofix` response-robustness Mechanisms

- **The measured-win response cascade is ported.** Three post-response Mechanisms — dispatched in
  the deterministic order `validate` → `autofix` → `syntax` (catalogue Table A as amended by the
  reorder entry below; originally shipped `validate` → `syntax` → `autofix`) — now ship in the
  `internal/mechanisms` catalogue (default **off**, D1). `validate` checks each requested tool call
  against the tool menu the model was shown and its own arguments (unknown tool name, empty/malformed
  JSON, missing required parameter); `syntax` checks a file-writing call's content (Go through the
  real parser, other languages through a bracket/string/truncation heuristic); `autofix` repairs
  syntax-broken write content and writes the improved payload back to the call the loop will dispatch.
- **Corrections retry in place (amended C5 — R1; superseding this entry's original ActionDefer
  delivery).** `validate`/`syntax` return `ActionRetry` with the sim's correction message — the
  loop re-streams the corrected request in the same Turn (see the delivery-switch entry below).
  `autofix` intercepts in place via `Response.SetToolCallArguments`, which is effective because a
  Response's tool calls are dispatched only after post-response review.
- **`gofmt` is always in-process; other formatters are construction-probed and gracefully absent
  (superseding this entry's original fire-time PATH-gating — see the autofix entry below).** Go is
  formatted with the standard library's `go/format` — no external dependency — with `goimports`
  preferred when found; `black` / `prettier` / `rustfmt` repair only when their executable was
  resolved at construction, and a formatter's absence, failure, or timeout leaves the payload
  untouched (standing requirement #2). What no formatter can improve is left for `syntax` to
  correct. (`internal/mechanisms`.)

### Wave 1: the `empty_response_recovery` / `tool_use_enforcer` off-ramps

- **The two recovery guarantees are ported (catalogue Table A).** Both are post-response Mechanisms
  with Capability **off-ramp** and SuppressionPolicy **exempt**, so they run even under Bypass (D5)
  and are never withdrawn by Adaptive Suppression or the Turn Budget — without them a failed Turn has
  no way out (CONTEXT "Off-ramp"). They ship in the `internal/mechanisms` catalogue, default **off**
  (D1). `empty_response_recovery` fires when the model returns nothing — no text and no tool call —
  mid-task with tools available and recent progress; `tool_use_enforcer` fires when the user asked for
  an action but the model answered with prose twice running, having never used a tool (the sim's
  intent classifier, folded in inline per catalogue C6).
- **Empty replies and narration both retry in place (amended C5 — R1; superseding this entry's
  original retry/defer split).** `empty_response_recovery` returns `ActionRetry` carrying the sim's
  first-attempt completion-check nudge verbatim; `tool_use_enforcer` returns `ActionRetry` with the
  sim's "use a tool" correction, the retried request carrying the superseded narration (the sim's
  `retryForToolUse` exchange). Both stay bounded by the loop's existing `maxPostResponseRetries`
  cap so an always-empty model still terminates. (`internal/mechanisms`.)

### ActionRetry now carries the corrective exchange onto the retried request

- **A post-response retry delivers its correction in the same Turn (R1, amending catalogue
  C5).** `PostResponseDecision.Inject` now rides `ActionRetry` too: when a post-response
  Mechanism retries with a correction, the loop appends the superseded assistant message
  (text + tool calls, when non-empty) and then the correction as a role-safe user message
  to the in-flight request before re-streaming — request-scoped, never committed to history
  — mirroring apogee-sim's own retry builders. Corrections accumulate across attempts (the
  sim's escalating re-asks), bounded by the existing `maxPostResponseRetries` cap; at the
  cap the last response passes through. An `Inject`-less retry stays a bare re-stream, and
  `ActionDefer` keeps its next-request semantics unchanged. (`internal/domain`,
  `internal/agent`.)
- **The retry appendage is hidden from post-response scanners (second-review fix, sim
  parity).** On a retry cycle the request-scoped superseded attempt + correction no longer
  masquerade as committed history to the history-aware Mechanisms: `Request.View()` is now
  bounded to the length frozen at the first `AppendSupersededAssistant`, so `read_repeat`
  never counts a never-executed superseded read as already-read and `tool_loop_interceptor`
  compares the retried response against the last **committed** turn, not the superseded
  attempt. The appendage still reaches the model through `Request.State()` — only the
  mechanism view changes — matching apogee-sim, whose retry builders ran their detectors
  against the unmutated request. (`internal/domain`, `internal/agent`.)
- **The retry-view boundary now survives an empty superseded response (third-review fix).**
  When a retried response is wholly empty, nothing is appended, so the correction lands
  *below* the frozen `committedLen` rather than after it — and the boundary was static, so
  `Request.View()` evicted the real user ask (the insert-before-last-user shape) or the newest
  tool result (the system-prepend shape) from the post-response scanners. `committedLen` is now
  MAINTAINED, not just frozen: a below-boundary `InjectContext` insert and an
  `appendOrCreateSystem` prepend each advance it, so `View()` stays pinned to the same committed
  history. `Request.State()` (the model-facing projection) is byte-identical — the correction
  still reaches the model unchanged; only the mechanism view is corrected. (`internal/domain`;
  tests in `internal/agent`.)

### Wave 1 rides the retry seam: corrections deliver in the same Turn

- **The four shipped Mechanisms switch `ActionDefer` → `ActionRetry` (amended C5, R1).**
  `validate` and `syntax` now short-circuit the response-repair cascade on a failing call —
  the correction re-streams the corrected request in the same Turn instead of waiting for the
  next request — so the catalogue's "short-circuits cascade on fail" holds for real.
  `tool_use_enforcer` re-calls in-cycle exactly like the sim's `retryForToolUse`: the retried
  request carries the superseded narration plus the "use a tool" correction, fixing the review
  finding that the correction sat until the next user Submit. `empty_response_recovery`
  upgrades its bare re-stream to carry the sim's first-attempt completion-check nudge verbatim
  (`empty_recovery.go` @pin); the attempt-2 nudge ladder, system directive, and temperature
  escalation stay recorded bench-pending divergences (R2). Everything remains bounded by
  `maxPostResponseRetries` — an always-empty model terminates, its final reply passing through.
  Proven loop-level through the scripted-responder harness, including both off-ramps firing at
  dispatch (registry-built) under Bypass and through a tripped Turn Budget.
  (`internal/mechanisms`; tests in `internal/agent`.)

### autofix repairs like the sim: construction-probed formatters, issue-count gating, repair-before-correct

- **The formatter table is resolved once at construction (D3).** `mechanisms.Deps` gains
  `LookPath` (nil ⇒ `exec.LookPath`); `newAutofix` probes goimports/black/prettier/rustfmt
  through it exactly once and caches the resolved paths — the sim's LookPath-cached formatter
  table — so a fire never touches PATH. The package-var-at-fire-time probe is deleted, and
  `cmd/apogee` wires the production `exec.LookPath`.
- **Repair only, gated on improvement.** autofix now acts only on syntax-broken write content
  and keeps a formatter's output only when it *reduces* the `checkSyntax` issue count (the
  sim's `AttemptFix` gate) — clean content is never beautified, and a "fix" that fixes nothing
  is discarded. The sim's `sanitizePath` NUL/CR/LF guard is restored alongside the kept `-`
  prefix hardening on formatter argv.
- **Cascade reorder: `validate` → `autofix` → `syntax`.** The sim runs detect → `tryAutoFix` →
  correct-the-remainder (`response_analysis.go:72-88` @pin), so repair now precedes the
  correction stage — `syntax`'s retry covers only what a formatter could not fix, ending the
  review's double-correction finding. Catalogue Table A and the post-response cascade section
  record the amendment. (`internal/mechanisms`, `cmd/apogee`.)

### Self-regulation judges the NEXT Turn on four proxy signals, and only acted fires count

- **Next-Turn judgment (R3).** Fires recorded in Turn N are now judged by Turn N+1's outcome —
  a Mechanism's intervention can only show up in what the model does next — instead of by the
  Turn they fired in. Each completed Turn is classified **three-way**: *productive* (a novel
  file read, or a successful write/action), *harmful* (a tool-result error, or an empty final
  response — both newly-recognized harmful signals; they used to merely be "not productive"),
  or *neutral* (neither — e.g. a substantive text-only answer), with productive winning when
  signals mix. Adaptive-Suppression strikes and the Turn-Budget streak advance **only on a
  harmful Turn**; a neutral Turn freezes both; a productive Turn stays the global clear-path.
  Consequence (the review's point): a pure Q&A session neither strikes Mechanisms nor trips
  the Turn Budget. A cancelled Turn's rollback now also restores the novelty credit of the
  reads it booked, so the mandated re-attempt is not penalized as a wasted re-read.
- **Fired means acted (R4).** A catalogued Mechanism is booked (`recordFire` +
  `MechanismFiredEvent` + the judgment set) only when its invocation **intervened**: it
  returned a non-zero post-response Action, or it mutated its working value —
  `Request`/`Response`/`Conversation` gain an internal revision counter with an engine-seam
  `Revision()` accessor, and the tool-stage hooks compare call/result snapshots. An
  inspect-and-do-nothing invocation is no longer a fire, matching apogee-sim's `FiredCounts`
  (interventions, not invocations); `LoopView.Fired` therefore counts actions. Experimental
  hooks keep the always-booked behaviour under the synthetic ID (bench observability).
- **The experimental sentinel ID is now reserved in domain (R5).** The `"experimental"`
  constant moves to `domain.ExperimentalMechanismID`, and `MechanismRegistry.Add` refuses a
  catalogued Mechanism claiming it — a real Mechanism can no longer masquerade as the bench's
  own instrument. (`internal/agent`, `internal/domain`.)

### Registry + config hardening: duplicate IDs refused, every `mechanisms:` key validated

- **`MechanismRegistry.Add` refuses a duplicate `MechanismID`.** Two Mechanisms registered
  under the same ID used to pass `Add` and be silently collapsed to one by the dispatch
  order's ID map; the second `Add` is now a loud error naming the ID — the same startup-gate
  posture as the reserved-sentinel refusal above.
- **A typo'd `mechanisms:` key now fails startup even when mapped to `false`.** README and
  the starter `config.yaml` always promised a loud unknown-ID error, but only *enabled* keys
  were checked (through the build path) — a misspelled `false` entry was silently accepted.
  Every key is now validated against the catalogue's known IDs (`mechanisms.KnownIDs`):
  disabled keys are checked by name — a disabled Mechanism is still never constructed — and
  the error lists the known catalogue exactly like the enabled-key path.
  (`internal/domain`, `cmd/apogee`.)

### The Phase-4 wave-1 review pass is closed out

- **The 2026-07-04 review of Phase-4 items 1–6 landed as five corrective fixes plus a docs
  close-out** (`docs/plans/phase-4-review-fixes-plan.md`), each detailed in its own entry
  above. The behaviour changes in one line: post-response corrections **retry in place**
  within the same Turn (amended catalogue C5, R1); `autofix` probes formatters **at
  construction** and repairs only when it reduces the issue count, running **before**
  `syntax`; self-regulation judges the **next Turn** three-way on four proxy signals and
  books only **acted** fires; the registry and config **refuse duplicate, reserved, and
  unknown** mechanism IDs loudly. The deliberate divergences from the sim (the R2 retry-
  ladder refinements and per-mechanism throttle counters — bench-pending) are recorded in
  the catalogue, and the Phase-4 detail plan carries the review's NOTES trail under items
  3, 5, and 6. (Docs: `docs/design/mechanism-catalogue.md`,
  `docs/plans/archived/phase-4-detail-plan.md`.)

### Wave 2: the `truncate_history` drop-the-middle history rewrite (`correct_tool_result` deferred)

- **A cheap, structural alternative to generative Compaction is ported (catalogue Table A).**
  `truncate_history` is a history-rewrite Mechanism that drops the middle of the conversation,
  keeping the protected prefix (leading system messages + the first user message,
  `Conversation.PrefixEnd`) and the last few assistant-anchored exchanges, cutting **only** at
  `Conversation.AssistantBoundaries()` so a tool result never gets separated from the assistant
  call that produced it (strict chat templates reject an orphaned tool message). At the cut it
  inserts a single static gap note; when fewer exchanges exist than the keep window it is a
  no-op (and books no fire — the loop keys acted fires on `Conversation.Revision`, R4). Ported
  verbatim from apogee-sim `internal/sim/intervention.go` `truncateHistory` @pin. Capability
  **proactive-nudge** (a context-shaper — disabled under Bypass, D5, while the structural Budget
  and Compaction stay on, D6), SuppressionPolicy **strikes-3**, default **off** (D1). It ships in
  the `internal/mechanisms` catalogue, buildable via the `mechanisms:` config block.
- **No phantom acted-fire on an ungrown, already-truncated history (second-review fix).** Re-running
  `truncate_history` when the conversation has not grown a new assistant boundary since the last cut
  used to re-drop and re-insert the same gap note — rebuilding the identical shape but bumping
  `Conversation.Revision`, which the loop reads as an acted fire (R4). The rewrite now detects that the
  only pending drop is the gap note it inserted last time and returns without mutating, so Revision
  stays put and no `MechanismFiredEvent` is booked. The truncation content stays sim-faithful and the
  grown-history path (real middle to shed) still truncates and books normally. (`internal/mechanisms`.)
- **`correct_tool_result` is deferred, not ported (owner-ratified 2026-07-04).** The pinned sim
  defines no production trigger for it — it is a lab-only intervention with an operator-supplied
  correction — so inventing gating logic would ship behaviour with no evidence. The loop already
  exposes the lab surface (an experimental post-tool-result hook can replace a result via the
  mutation API), so the bench plays the operator without a catalogued Mechanism; a bench-discovered
  trigger would motivate a new plan item. (`internal/mechanisms`; catalogue Table A/B.)

### The Budget allocator + usage-calibrated token accounting make `LoopView.Budget()` honest

- **`LoopView.Budget()` now reports honest token accounting.** The loop's former trivial
  `defaultCharsPerToken = 4.0` estimate is replaced by a per-Session `TokenEstimator`
  (`internal/context`) the loop **calibrates against server-reported usage**: each Turn, the
  reported prompt tokens snap `Budget.Used` to the real context fill, and prompt-tokens vs the
  characters actually sent recompute the chars→token ratio — bounded to a sane range `[2, 8]` and
  smoothed (an exponential moving average) so the ratio converges toward the model's real
  tokenizer across Turns while a single anomalous report cannot swing it. Uncalibrated (a fresh
  or resumed Agent, before its first `UsageEvent`) it reports the default ratio and a zero `Used`.
- **The Budget is now the single authority on how much room each part gets (CONTEXT: Budget).**
  `internal/context.Allocate` splits the discovered context window (`n_ctx`) across a response
  reserve and the prompt's parts — system prompt, file context, conversation history — with the
  parts summing to the window exactly; an unknown window yields the zero allocation (treated as
  unbounded). `domain.Budget` gains the advisory `ResponseReserve`/`SystemPrompt`/`FileContext`/
  `History` fields (additive; the root `apogee.Budget` alias picks them up), which the item-9
  context reducers will consume. It is **structural**, not a Mechanism: it stays live under
  Bypass (D5/D6). Nothing in the request path is reshaped by it yet — the allocation is advisory
  until the reducers land. (`internal/context`, `internal/agent`, `internal/domain`.)

### Tool-result capping + the automatic Compaction trigger — the two Budget consumers

- **`tool_result_cap`: a config-gated tool-result capping Mechanism.** The surviving half of
  apogee-sim's `compress` (catalogue C3 SPLIT), ported as a pre-request Mechanism: any single tool
  result whose content exceeds its fraction of the Budget (40% of the working window — the window
  less the response reserve — in characters, via the calibrated chars→token ratio) is trimmed to a
  head/tail-plus-elision-marker form through `Request.SetMessageContent` (an in-place edit), while
  the **most recent tool-call Turn is always protected**. Default-off (D1); `proactive-nudge` /
  `strikes-3`, so Bypass disables it and it self-regulates like its peers. (`internal/mechanisms`.)
- **Automatic, budget-driven Compaction.** The generative `Compact` (the `/compact` reducer) now
  also fires **automatically**: at a quiescent boundary, before a Turn's request is built, the loop
  folds the conversation when `internal/context.HistoryExceedsAllocation` reports the history has
  outgrown its Budget `History` allocation. It runs the same fold (protected prefix, `Replace`
  write-back) before it consumes new input, so a just-submitted message survives as its own turn;
  it is non-reentrant, and a fold fault surfaces as an `ErrorEvent` leaving history untouched. It is
  **structural**, not a Mechanism: on by default and **on even under Bypass** (a naked model still
  overflows its window — decision 12), with a file-only `auto-compact: false` opt-out
  (`ContextConfig.CompactionEnabled`). The on-demand `/compact` is unaffected by the gate.
  (`internal/context`, `internal/agent`, `cmd/apogee`.)
- **Auto-compaction is Exchange-boundary-only and saturates on an oversized prefix (second-review
  fix).** The automatic trigger now also requires **not** `inExchange`: a mid-Exchange over-budget
  Turn (a tool continuation) defers the fold to the next Exchange opening rather than folding a
  half-finished Turn into a summary (`tool_result_cap` is the mid-Exchange relief valve). A fold that
  still cannot bring the history under its `History` allocation — the protected prefix (system prompt
  + first user message) alone exceeds it — emits exactly one `compaction` `ErrorEvent` and then
  **stands down** until the estimate drops back under the allocation (growth alone no longer thrashes
  the fold every Turn); the on-demand `/compact` ignores saturation. And a mid-Exchange history
  rewrite (`truncate_history`) now **repairs `exchangeStart`** by the drop delta, floored just past
  the prefix + gap note, so `AbortExchange` (Esc) rolls back to exactly the Exchange boundary with no
  orphaned tool results. (`internal/agent`.)
- **The saturation latch is now gated on a fold that ran (third-review fix).** A `Compact` that
  **skips** (too few messages past the protected prefix to be worth folding) folds nothing, so it
  proves nothing about whether folding can help — yet the auto-trigger used to run its post-fold
  saturation check on the skip too, latching off (one `ErrorEvent`) and permanently disabling
  auto-compaction whenever the history was over its allocation but too short to fold. `autoCompact`
  now returns on `Result.Skipped` before the saturation check, so only a fold that **ran** and still
  left the history (protected prefix + summary) over its allocation can saturate; a skipped boundary
  re-checks for free at the next opening. (`internal/agent`.)
- **Context-window discovery for pinned models + a `context-window:` key (second-review fix).** A
  configured `model:` no longer silently disables the Budget and automatic Compaction. Window
  discovery is split out of `resolveModel` and now runs for a pinned model too — keeping the pinned
  id but adopting the server's advertised window — and is **non-fatal**: a failed probe leaves the
  window unknown with a one-line notice, so an offline pinned-model start still works (the no-model
  path keeps its existing fatal semantics). A new file-only `context-window:` key (tokens) overrides
  discovery and skips the probe. When the window is still unknown while Compaction is on, startup
  prints one notice naming the consequence and the key. (`cmd/apogee`, `internal/domain` comment.)
- **No redundant context-window probe on the no-model path (third-review fix).** When the server
  advertised no window on a zero-config (no-model) startup, `resolveModel`'s discovery probe left the
  window at 0, so the separate `resolveContextWindow` self-guard (`opts.contextWindow > 0`) did not
  fire and it probed the server a second time. `resolveModel` now reports whether it probed and the
  root skips `resolveContextWindow` when model discovery already ran — one probe for the whole
  no-model startup, regardless of the advertised window. The pinned-model path is unchanged (still
  probes for its window; a failed probe stays non-fatal). (`cmd/apogee`.)
- **`context-window` precedence and the `ContextConfig` threading are now pinned by tests
  (third-review fix, Tests).** A test proves a `context-window:` key wins over the server-advertised
  window on the no-model path (`resolveModel` keeps the discovered id but not the advertised window),
  and a `runRoot` test proves `opts.contextWindow` reaches `Config.Context.MaxContextTokens` (via the
  loud-zero notice) — closing the mutation gap the pinned-model-only coverage left open. (`cmd/apogee`
  tests.)
- **`cached_content_intercept`'s schema-gate conservative fallbacks are now pinned by tests
  (third-review fix, Tests).** A redundant re-read that would otherwise be capped is proven left
  byte-identical (no fire, R4) when the pending read tool is absent from the (toolfilter-narrowed)
  menu, carries an empty schema, or carries a schema that does not parse — closing the mutation gap
  the earlier coverage left silent. (`internal/mechanisms` tests.)

### Wave 3: the `toolfilter` / `filehint` / `grammar` request shapers

- **`toolfilter`: relevance-scored tool-menu narrowing.** A pre-request Mechanism that trims the
  tool menu for small models, ported from apogee-sim `internal/toolfilter` @pin. It activates
  reactively — only when the menu is large (30+ tools) or the model has hallucinated a tool absent
  from the menu — and never when the menu is already within the keep limit (10). It scores each tool
  against the last user message's keywords (exact name > name-part > description match), keeps every
  recently-used tool whole (plus the read-only exploration tools when the request is analysis-focused),
  and re-sets the menu to the top-scored subset via `Request.SetTools`. The narrowing is
  **request-scoped** (the loop rebuilds the full menu each Turn, so it never mutates the menu
  globally) and deterministic (stable score-tie ordering). It declares `Before decompose` (item 12).
- **`filehint`: role-safe workspace file hints.** A pre-request Mechanism ported from apogee-sim
  `internal/filehint` + `file_hint_detector` @pin. After the model lists a directory but before it
  reads anything, it scores the listed files against the user prompt (a TF-IDF-ish weight plus a
  language-extension boost) and injects a hint suggesting the most relevant files to read, through
  the role-safe `Request.InjectContext` (which folds into the system prompt when the conversation
  ends in a tool result). A stable marker makes the inject **idempotent** (no double-inject), and a
  greenfield-creation task with no files written yet is suppressed.
- **`grammar`: a backend-capability-gated json_schema constraint.** A pre-request Mechanism ported
  from apogee-sim `internal/grammar` + `injectGrammarConstraint` @pin: it derives a `json_schema`
  from the current tool menu and sets it as the request's `response_format` so a model that cannot
  emit native tool calls is constrained to a valid tool-call shape. It is **capability-gated** by the
  new D3-injected `mechanisms.Deps.GrammarConstraint` — false on every current apogee backend (no
  such probe is wired, and the provider wire does not yet carry request extras), so grammar **no-ops
  today** (catalogue Table B). An existing `response_format` always wins.
- All three ship default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under Bypass, D5;
  self-regulating), buildable via the `mechanisms:` config block. (`internal/mechanisms`.)
- **`toolfilter` / `filehint` carry the sim's camelCase spellings (second-review fix).** The
  analysis-keep set (`toolfilter`) now also holds the sim's `readFile`, and the directory-listing set
  (`filehint`) holds the sim's `listFiles` — completing the item-10 "plus the sim spellings" claim so
  a mixed MCP menu with camelCase tool names still narrows and hints. (The write-tool and file-read
  sets already carried every sim spelling.) (`internal/mechanisms`.)
- **The sim-seeded pre-request ordering edges are now declared (second-review fix).** The catalogue's
  §Ordering seeds are now live `OrderingConstraints`, not just prose: the `cot` nudges (`stall_nudge`,
  `list_nudge`, `tool_use_directive`) and `library` inject `Before toolfilter`, and `tool_result_cap`
  runs `After decompose` — so it sorts last among the pre-request shapers, trimming tool results after
  context is assembled. Previously the order rested on the D4 ID tiebreak alone, which matched the sim
  for the nudges/library but sorted `tool_result_cap` *before* `toolfilter`. Table A's "none" cells
  were amended per D7 to record the edges, so §Ordering, Table A, and the code now agree, and a
  regression test pins the resulting order. (`internal/mechanisms`, `docs/design/mechanism-catalogue.md`.)

### Wave 3: the history-aware `error_enrichment` / `read_loop` / `read_repeat` / `tool_loop_interceptor` / `cached_content_intercept` family

The cross-turn aggregators, ported from the pinned apogee-sim source (catalogue Table A/B), each
deciding by scanning the conversation across Turns at its **relocated** hook point. All ship default
**off** (D1), `strikes-3` and non-exempt (so disabled under Bypass, D5), buildable via the
`mechanisms:` block. (`internal/mechanisms`.)

- **`error_enrichment`: repeated-error clarification at post-tool-result.** Ported from apogee-sim
  `internal/proxy/error_enrichment` @pin and relocated to post-tool-result: when a write-tool call
  fails, and the same file already failed the same way earlier this Session, it appends
  category-specific guidance (syntax / import / type / build / permission / runtime) to the failing
  result the model reads next. The current failure uses the authoritative `ToolResult.IsError`; prior
  failures in history are string-classified (a committed tool-result message no longer carries the
  flag). A marker keeps one hint per repeated-error episode.
- **`read_loop`: the consolidated read-loop detector at pre-request.** Ported from apogee-sim
  `internal/proxy/read_loop_detector` @pin, folding the sim's three variants (normal / greenfield /
  successful) into one Mechanism (catalogue C2): a role-safe hint fires on repeated failed reads of
  the same file (threshold 1 on an empty workspace, 2 otherwise) or three successful re-reads without
  a write. The deterministic hint is its own idempotency marker.
- **`read_repeat`: redundant re-read retry at post-response.** Ported from apogee-sim
  `internal/proxy/read_repeat_interceptor` @pin: when the whole response only re-reads files already
  read successfully in a recent Turn, it retries in place (`ActionRetry`, R1) with a "you already
  read these, proceed" correction.
- **`tool_loop_interceptor`: identical-repeat-turn detector at post-response.** Ported from apogee-sim
  `internal/proxy/tool_loop_interceptor` @pin (inventory-missed, found in the checkout — catalogue
  Table B): when the response repeats the previous Turn's exact tool-call key, it retries with a
  loop-breaking directive. The sim's per-Session count threshold and 30s cooldown are dropped (R2 —
  self-regulation and the loop retry cap substitute).
- **`cached_content_intercept`: redundant-re-read cap at pre-tool-exec.** Ported from apogee-sim
  `internal/proxy/cached_content_intercept` @pin and relocated to pre-tool-exec: a re-read of a file
  already read successfully and unchanged since is capped to a header-only slice, reclaiming the
  window the full re-dump would cost (the content is already in context). The sim rewrote the result
  post-execution; pre-tool-exec has no result-substitution primitive, so the port expresses the same
  token-saving intent through the pending call's arguments.
- The re-read family (`read_loop` / `read_repeat` / `cached_content_intercept`) is pairwise
  **incompatible** — at most one is enabled at a time (the sim's per-request exclusivity as an apogee
  startup gate). In the post-response cascade the resolved dispatch order is
  `read_repeat → tool_loop_interceptor → validate → autofix → syntax` (the sim's response-side
  priority).
- **Write detection now sees apogee's own edit tools (second-review fix).** The history family's
  "did this call mutate a file / was it a write action" checks (`read_repeat`, `read_loop`,
  `cached_content_intercept`, `error_enrichment`, `tool_loop_interceptor`, the off-ramps,
  `deriveWriteTarget`) moved from the sim-only `isWriteTool` set to a new apogee-complete
  `isFileMutatingTool` predicate that also counts `edit_existing_file` /
  `single_find_and_replace` / `multi_find_and_replace`; the content-repair Mechanisms (`syntax`,
  `autofix`) stay on the narrower sim-only set (their payloads are file fragments, not full files).
  `open_file` joins the family read set (its result places file content in the conversation like
  `read_file`). And `read_repeat` now collects each turn's write paths **before** its reads, so a
  same-turn read-then-write to a path no longer counts that read as a redundant re-read.
- **`cached_content_intercept` gates its cap on the tool schema (second-review fix).** The read cap
  is now applied only when the pending tool's argument schema (via `view.Tools()`) declares a
  `max_lines` property; a read tool lacking it — e.g. a strict MCP server with
  `additionalProperties:false` — is inspected but never handed an argument it would reject, so the
  re-read proceeds uncapped and no fire is booked. This makes the mechanism's "benign no-op" fidelity
  note literally true instead of relying on the third-party tool tolerating an unknown field.
- **The `isFileMutatingTool` history-family sites now have edit-tool coverage (third-review fix, Tests).**
  Tests exercise `edit_existing_file` / `single_find_and_replace` at the three sites the earlier
  suite left untested and that can carry regression-detecting coverage: `empty_response_recovery`
  treats a recent edit as progress worth recovering (`hasRecentProgress`), the `tool_loop_interceptor`
  directive credits an edit as work already done (`extractConversationContext`), and the `read_loop`
  hint excludes an edit-written path from its "create X" suggestion (`writtenPaths`) — each test fails
  when its site is mutated to exclude the edit tools. The fourth site (`wroteRecently` in the
  `tool_use_enforcer`) cannot be pinned: `shouldEnforceToolUse`'s `!hasEverUsedTools` gate stands the
  enforcer down whenever any edit call is present, so the `wroteRecently` edit branch is never the
  deciding factor — documented in place rather than covered by a vacuous test. (`internal/mechanisms`
  tests.)

### Wave 4: the `decompose` request shaper + the `stall_nudge` / `list_nudge` / `tool_use_directive` completion nudges

The last of the request shapers, ported from the pinned apogee-sim source (catalogue Table A/B), each
a pre-request Mechanism shipping default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under
Bypass, D5; self-regulating), buildable via the `mechanisms:` block. (`internal/mechanisms`.)

- **`decompose`: one-step focus + history collapse.** Ported from apogee-sim `internal/decompose`
  @pin. For a small model that stalls on long multi-step prompts it (1) collapses the complex
  multi-step user messages still sitting in conversation history to a short task summary (via
  `Request.SetMessageContent`) so the model cannot re-read a full step-by-step plan from an earlier
  turn, and (2) hints the single next actionable step of the current prompt into the system prompt
  (via the idempotent `Request.AppendToSystem`), leaving the full user message intact. It declares
  `After toolfilter` (trim the menu before the user-message rewrite — the mirror of toolfilter's
  `Before decompose`).
- **The read-loop coupling gates active decomposition (D2).** decompose's `RequestMeta.FiredCounts`
  peek in the sim becomes a live `LoopView.Fired("read_loop")` query: once the consolidated
  `read_loop` Mechanism has **acted** this Session (R4), active decomposition — which would override
  the focus to "step 1: …" and fight the read-loop hint — is muted, while the harmless history
  collapse still runs.
- **The completion nudges are the `cot` family, split three ways (catalogue C4).** apogee-sim's `cot`
  Transform is not itself a tracked Mechanism — it emits three tracked nudges, which apogee ships as
  three independent pre-request Mechanisms: `tool_use_directive` (an action was asked for but the
  model has not used a tool yet → "use a tool"), `stall_nudge` (read-only for the stall threshold of
  turns with a write tool available → "proceed with the modifications"), and `list_nudge` (an analysis
  request that listed directories but read no files → "read the files you found"). Each injects one
  system directive through the idempotent `AppendToSystem`; the "nudge cap" is a stateless window on
  the read-only turn count. `stall_nudge` ⊥ `list_nudge` (contradictory directives) — declared
  `IncompatibleWith`, so at most one is enabled per config (the apogee startup gate subsuming the
  sim's runtime `!wantListNudge` preference).
- **`intent` and `cot` are folded, not ported as Mechanisms (catalogue C4/C6).** The shared intent
  classifier (`hasActionIntent` / `hasAnalysisIntent`) already landed inline in wave 1 and is reused
  here; `cot` ships only as its three nudges. This closes the Phase-4 request-shaper catalogue —
  `library` (item 14) is the only remaining un-ported catalogue Mechanism.

### The Library learning substrate: a confidence-tagged `ModelFingerprint` and a file-backed store

The substrate the Library Mechanisms (item 14) build on — no Mechanism yet, so nothing observes or
injects until item 14 wires it. (`internal/domain`, new `internal/library`.)

- **`ModelFingerprint` — a confidence-tagged model identity.** New `domain.ModelFingerprint`
  (`Label` + `FingerprintConfidence`) and the `FingerprintResolver` seam. `internal/library`'s
  production resolver returns the best available tier: a **weights-hash (high)** when the model id is
  a reachable weight file (`.gguf` / `.ggml` / `.bin` / `.safetensors`) — a SHA-256 over the file size
  plus its head and tail, so two builds sharing a label but differing in weights diverge without
  hashing multi-gigabyte files at startup — else the **metadata label (low)** (the bare model id). The
  **behavioral-probe (medium)** tier is the Phase-5 `apogee probe`: the enum slot and the resolver
  interface exist so it slots in behind the same seam, but no resolver produces it yet (D8).
- **A file-backed, versioned Library store.** New `library.Store`, rooted at an injected directory
  (`Config.LibraryDir`) and **never** an ambient `~/.apogee` (ADR 0001) — the bench's isolated root
  falls out for free (decision 11). It holds per-fingerprint observations (`Entry`) with the sim's
  Bayesian confidence counts (`Score = (observations − successes + 1) / (observations + 2)`, capped at
  0.95), so a pattern the model grows out of stops qualifying for injection without being deleted. It
  persists to a single `library.json` with a schema `Version` (like `domain.Session`), is process-local
  (a mutex guards intra-process access; no cross-process locking claims in v1), and degrades a missing,
  corrupt, or too-new store to **empty-with-a-soft-error** (the skills-catalog posture — a broken
  Library never bricks a run). A zero fingerprint (unidentified model) is inert: nothing is recorded.

### The Library Mechanism: cross-session observe + confidence-gated inject

Item 14 wires the Library substrate (item 13) into the loop as the catalogued `library`
Mechanism — default-off (D1), fully inert under `--bypass` (it is `proactive-nudge`, so item 2's
dispatch gate skips both halves). The single `library` catalogue row is realized as ONE Mechanism
implementing BOTH hooks. Ported from apogee-sim's `library` observer/transform. (`internal/mechanisms`,
`cmd/apogee`.)

- **Observe (post-response).** After each response the Mechanism records completed-Turn outcomes into
  the store, keyed on the model fingerprint: tool-call validation failures (corrections),
  narration-instead-of-acting and shallow-exploration behavioural patterns, examples of valid complex
  tool calls, and the success signal that decays a pattern the model has grown out of. It is a pure
  observer — it never mutates the response and books no fire, so it does not skew self-regulation.
- **Inject (pre-request).** When the fingerprint clears the confidence gate — "prefer not to inject
  under uncertainty", so a low-confidence metadata-label identity does **not** inject — the Mechanism
  appends the highest-scoring qualifying observations to the system prompt (idempotent on a marker),
  intent-filtered and capped to a 200-token injection budget, and backs off when the window is nearly
  full.
- **Store + fingerprint injected at construction (D3).** `cmd/apogee/wire.go` constructs and Loads the
  store under `Config.LibraryDir` (never an ambient `~/.apogee`, ADR 0001) and resolves the model
  fingerprint once, wiring both into the constructor `Deps` only when `library` is enabled — so the
  inject and observe halves share one identity, and a config without `library` reads no store file.
  Two agents on two `LibraryDir`s stay isolated (decision 11). Longitudinal bench validation
  (improves-over-sessions AND never-below-baseline) stays **pending**.
- **Stored observations are now treated as untrusted data (second-review fix, Security).** Library
  entries persist model- and tool-result-derived text and re-inject it into a future system prompt, so
  the store is now hardened against a hostile-repo → store → system-prompt payload channel. A new
  `library.SanitizeContent` strips control characters, folds CR/LF (and any whitespace) into single
  spaces, and collapses runs; it runs at `Store.Record` time — so poison never lands on disk in
  directive-capable form — **and** again when the injection block is rendered, defending stores written
  before this landed. The complex-call "example" observer records only the call **shape** — the tool
  name and its sorted parameter **names** — never argument **values**. The injected block's header now
  opens with an explicit data-not-instructions frame so entries cannot read as directives. No store
  schema bump (entries stay compatible). (`internal/library`, `internal/mechanisms`.)
- **The sanitizer now strips Unicode format characters, and example param names are schema-filtered
  (third-review fix, Security).** `SanitizeContent` stripped only Cc controls (`unicode.IsControl`), so
  bidi overrides, zero-width characters, the BOM and soft hyphens rode through into the store and the
  injected block; the strip now also covers Cf/Co/Cs. And the complex-call "example" recorded the raw
  keys of the model's arguments object — free-form, model-controlled strings — so a junk key bearing
  directive text could land on a clean observation. The recorded names are now the **intersection** of
  the call's argument keys with the tool schema's declared `properties`, and a call whose schema yields
  no properties records no example at all (prefer not to record under uncertainty); the 5+-param
  complexity gate reads the schema, never the argument keys, so junk keys can never promote a simple
  call. (`internal/library`, `internal/mechanisms`.)
- **Bypass leaves a pre-seeded Library store byte-for-byte untouched (second-review fix, test-only).**
  A loop-level test seeds a populated `library.json`, wires a registry-backed agent with `library`
  enabled and `Config.Bypass` on, drives an observe-triggering Exchange, and asserts the store file's
  bytes are unchanged — the item-14 mandate now has its literal regression. (`internal/agent`.)

### Bench-readiness proof: the embeddable two-arm contract is now a permanent regression

Item 15 adds `benchreadiness_test.go`, the executable definition of "benchable" (ADR 0001): a
root-package consumer test that drives the real Agent exactly the way apogee-sim will — the public
`New` / `Resume` / `Submit` / `Step` / `Snapshot` / `Close` surface over the real provider client
dialing one scripted OpenAI-compatible httptest model, catalogued Mechanisms enabled via `Config`
(`toolfilter` / `decompose` / `truncate_history` / `library`), and experimental hooks at all five
hook points. It constructs a mechanisms-on arm and a **Bypass** arm against isolated temp state
roots, Steps both to their quiescent boundaries, then Snapshots and Resumes forks. It asserts: the
enabled shapers ACT in the registry's deterministic dispatch order visible in the
`MechanismFiredEvent` stream (`toolfilter` before `decompose`, then the experimental hook) while an
inspect-only Mechanism books no fire (R4); the Bypass arm fires no catalogued Mechanism yet runs all
five experimental hooks; agent-driven writes stay inside each injected root (the Library store lands
under the mechanisms-on arm's `LibraryDir`, the Bypass arm's stays empty); and forks resumed from one
snapshot diverge independently in their own roots. If a future change breaks the bench contract, this
test breaks first. Test-only — no product change. (root `apogee_test`.)

## [1.1.0] — 2026-07-03

Post-`v1.0.0`, **additive** (minor) — the start of the apogee-code TUI
feature-parity track. See
`docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md` and
`docs/handoffs/2026-06-26 - 01 - skills-system.md`.

### Drag-select-to-copy in the transcript (screen-space)

- **You can now drag-select text in the chat transcript and copy it to the clipboard**, the same
  gesture the prompt box already supported. A left-click-drag inside the transcript viewport
  highlights the span and, on release, copies the rendered text over OSC52 (`tea.SetClipboard` —
  cross-terminal and SSH-safe) with the usual "copied N chars" confirmation. The selection is
  **screen-space** ("copy what you see"): it anchors in content coordinates (rendered-line index +
  display cell) into the cached `m.lines`, so it survives a mid-drag wheel-scroll; on release it
  slices each spanned line with `ansi.Cut`, strips the styling, and trims the block's trailing pad.
  Markers, rail gutters, and soft-wrap breaks are copied verbatim (the accepted terminal-native
  semantics — the one-way render pipeline stays one-way, no line→entry reverse index). The mouse
  handlers arbitrate by region — a point in the input rectangle drives the prompt editor, a point
  in the viewport drives the transcript — so the two selections never coexist. The selection clears
  on any transcript change (a streamed token, a submit) and on resize; a bare click copies nothing.
  Drag auto-scroll at the viewport edge is deferred. (`internal/tui/mouse.go`, `model.go`.) Closes
  the "cannot select text in the transcript" ISSUES entry.

### Chat input lifted into a `promptEditor` module (internal refactor)

- **The chat input cluster now lives in its own type**, `promptEditor` (`internal/tui/prompteditor.
  go`), instead of scattered across the god-Model. It gathers the five loose input-side concerns the
  architecture review (candidate #3) called one coherent concept — the textarea, the autocomplete
  overlay (+ its `skillRegion` edge-trigger), the staged-skill chips, the workspace file cache, and
  the prompt drag-selection. The `Model` embeds it **anonymously**, so the fields and the
  self-contained methods promote onto the Model (`m.input`, `m.pendingSkills`, `m.caretTo(...)` all
  resolve through it) and every existing call site — and all package tests — stay unchanged. Model
  top-level field count drops **32 → 27**; the six input-cluster fields now have a single home.
- **Purely structural — no behaviour changes.** Only methods that touch nothing but the editor's own
  fields moved to it (`newPromptEditor`, `submitParse`, `reset`, `rows`, and the caret re-seat trio
  `caretTo`/`reseatCaret`/`reseatInput`); methods that also read Model-owned state (theme, window
  size, `Options`, lifecycle) deliberately stay on the Model rather than duplicate that state. The
  Model stays the coordinator (lifecycle state machine, transcript + render cache, stats/gauge,
  theme, layout); the editor never touches the engine — it only turns typed input into
  send-ingredients the Model routes. New editor-direct unit tests exercise the lifted logic without
  a Model or a fake engine (`internal/tui/prompteditor_test.go`).

### Model profile config surface (tool-call format + thinking channels)

- **`Config` gains a `Profile ModelProfile` seam** describing how the configured model speaks the
  wire (CONTEXT: Model profile) — its tool-call format (native / markdown-fenced / custom-regex)
  and its inline thinking-channel style (none / delimited `<think>…</think>` / gpt-oss harmony).
  The new public domain types are re-exported from the root facade (`apogee.ModelProfile`,
  `ToolCallFormat`, `ThinkingProfile`, `ThinkingStyle` and their consts) — an **additive minor**
  (decision #18). A **zero profile is native tool calls with no inline thinking**, so every
  shipped model behaves exactly as before (the byte-identical anchor).
- **Plumbed from `config.yaml`** as a file-only `model-profile:` block (a per-model concern, like
  `mcp-servers` — no flag/env), mapped to the domain type at the host boundary. **No loop consumer
  yet**: the loop's parse seam is crossed in a following change, so this is a pure, provably
  behaviour-neutral config-surface addition.

### Model profile wired into the loop (fenced/regex tool calls + thinking/harmony stripping)

- **The loop now consumes `Config.Profile` at the parse seam.** A new `processing.ParserFor(domain.
  ModelProfile)` translates the declarative profile onto `internal/processing`'s existing, frozen
  `ToolCallingConfig`/`ThinkingConfig` and returns the text-format `ToolCallParser` plus a unified
  `ContentStripper` (the `none`/`delimited`/`harmony` thinking styles behind one `Strip` +
  `IsMidChannel` interface). `internal/agent` selects both once in `newAgent`, so the oracle config
  types never surface in the loop and a bad profile (unknown format / thinking style) fails
  construction loudly rather than falling back to native.
- **At the seam:** the reply's inline thinking/harmony channel is stripped out of the visible
  content and preserved as `reasoning_content` in history (the harmony `commentary` channel folds
  into reasoning); when the structured **native** path produced no calls, a markdown-fenced or
  custom-regex tool call is recovered from the *stripped* visible content, its markup removed from
  the committed assistant text, and it is assigned a deterministic `text_call_<turn>` ID (not the
  oracle's wall-clock ID, so snapshot/resume and tests stay stable). Native calls always win when
  present.
- **A recorded, deliberate divergence from the apogee-code oracle:** a text-parsed call is stored
  **structurally** on the assistant message (`ToolCalls`), so dispatch, events, and snapshot/resume
  keep **one** path for every format; the oracle instead commits stripped text with only a
  tool-role result. Chat templates tolerate native-shaped history better than the loop tolerates two
  history shapes.
- **A zero profile is byte-identical** to the pre-change loop: the no-op stripper and no-op parser
  leave `reply.content` and the native calls untouched, so every shipped (native) model behaves
  exactly as before. The frozen `internal/processing` oracle types, parsers, and parity tests are
  unchanged — only the new `ParserFor`/`ContentStripper` and the loop caller were added. **Live
  in-flight token suppression while streaming is a following change; this fixes committed history
  and the final message.**

### In-flight thinking/harmony tokens held off the live stream (native unchanged)

- **`streamResponse` now emits a `TokenEvent` for the newly-revealed *visible* content**, not the
  raw content delta, using the same `ContentStripper`. While the accumulated content ends inside an
  unclosed inline reasoning span (`IsMidChannel`), token emission is held, so a model that inlines
  `<think>…</think>` or gpt-oss harmony channels no longer flashes that markup (or its reasoning)
  onto a live UI before the post-stream strip; the visible text is revealed once the span closes.
- **A native / no-inline-thinking profile is byte-identical, event-for-event:** the no-op stripper
  is never mid-channel and returns the content untouched, so every content delta emits verbatim and
  unbuffered exactly as before. A channel start token split across two deltas briefly reveals its
  partial prefix live (matching the oracle's `isThinking`); this recorded edge is accepted — the
  post-stream strip still removes it from the committed message and final `MessageEvent`.

### Fenced/regex models now receive a text tool menu + emission instructions (native unchanged)

- **A new `processing.InstructionsFor(domain.ModelProfile, []domain.ToolDef)` renders the emit
  side of a non-native profile:** the text tool menu (name, description, JSON-schema parameters)
  plus the format-specific tool-call instructions and a live example — ported from the apogee-code
  context builder, driven by the *same* profile knobs and defaults the parser reads, so what the
  model is told and what the loop parses cannot drift. It is the request-seam mirror of `ParserFor`.
- **`toProviderRequest` now injects the block and suppresses the native `tools` array for a
  non-native tool-call format.** The rendered menu + instructions are folded into the wire request's
  system channel (appended to a hook-seeded system message, else a sole system message at position
  0) and the native `tools` array is dropped — sending both would double-tell the model in two
  formats, and a chat template without tool support can error on the array. For a non-native profile
  the text menu is the **only** channel the model learns its tools from; before this change a
  fenced/regex model received a native array its template may not render and no instructions.
- **Wire-only, tracked per request:** the block never enters domain history, the snapshot, or any
  event — exactly like the native `tools` array, which is also rebuilt per request and never
  persisted. It is re-rendered over each request's **mode-filtered** menu, so a Plan-mode switch (or
  any menu change) is reflected on the next Turn with no history rewrite.
- **A native/zero profile is byte-identical:** `InstructionsFor` returns `""`, so there is no
  injection and no suppression — the native `tools` array and the message list are exactly today's.

### Dispatch decision collapsed into one Resolution verdict (internal refactor)

- **The per-call dispatch decision is now one `Resolution`**, computed by a single pure resolver
  (`internal/agent/resolution.go`): the tighten-only guard floor, the autonomy-ladder × blast-radius
  table, the confinement-capability check, and the precomputed runtime-demote contingency are all
  decided in full before anything executes. `internal/agent/dispatch.go` is now a thin executor that
  gathers facts, calls the resolver once, and carries the verdict out — it holds no ladder,
  guard-tier, or demote decision of its own. The old `disposition.go` decision path is retired.
  **No behavior change**: unexported and internal-only (no public API / semver impact). The term
  "disposition" is retired from code, surviving in prose only as the historical name of the
  post-guard ladder stage. `docs/design/confinement-execution-contract.md` §4 amended in place.

### MCP "allow for this session" now caches at server grain (ADR 0012 conformance)

- **Approving one of an MCP server's tools "for this session" now clears the whole server**, not
  just that one qualified tool: approving `github__search` pre-clears `github__create_issue` and
  every other `github__*` tool for the Session, honouring ADR 0012's server-grain promise (the
  cache had always keyed on the qualified tool name, so each `github__*` tool re-prompted). The
  allow-for-session cache key for an `mcp` gate is now `mcp-server:<alias>`; the `mcp-server:`
  prefix keeps that grain collision-proof against ordinary tool names, and a **different** server
  (`jira__*`) is never pre-cleared by another's approval. A **forced** gate (a Tier-2
  dangerous-action speed-bump) still skips the cache and re-prompts, unchanged. Every non-MCP
  class keeps the tighter tool-name grain, so nothing else loosens.

### Compact tool print-outs in the chat (full built-in coverage)

- **The TUI's tool-presentation registry now covers every built-in tool**, not just the
  Phase-2 four: the edit family, `view_diff`, `open_file`, `terminal`, `python_exec`, the
  git family, `diagnostics`, `web_fetch`, `http_request`, `web_search`, `sub_agent`, and
  `ask_user` each render as `✦ [Label] target` — no more raw tool names with JSON argument
  braces in the transcript. Only a dynamic (MCP) tool keeps the raw-name + JSON fallback.
- **Results no longer dump raw into the chat**: `web_search` shows "N results", the fetch/
  request tools their `HTTP 200 OK` status line, free-form output (a command run, a
  diagnostics or sub-agent report) its first line plus a "+N more lines" count, `open_file`
  its Located line or a line count. `view_diff` renders red/green diff lines (the reserved
  diff detail kinds get their first producer), capped at 20 lines.
- Detail and target lines are clipped at 160 runes so a minified blob cannot flood a row.
  The approval dialog still shows the full pretty-printed arguments — the security surface
  (the model's request is never hidden) is unchanged.

### Web search works out of the box (DuckDuckGo default)

- **`web_search` is now default-ON**: with no `web-search-endpoint` configured it uses a
  built-in DuckDuckGo HTML provider — no config, no API key (reverses the P3.11 default-off
  decision; the predecessor apogee-code shipped the same built-in). Set
  `web-search-endpoint: off` (or `none`/`disabled`) to disable the tool — a graceful
  "web search is disabled" result, no request made.
- **The DuckDuckGo provider POSTs the query** as a form field, the way DDG's own search
  form submits: the HTML front-end answers a plain GET with its bot-challenge ("anomaly")
  page — zero result anchors, so every search rendered "No results found". A custom
  endpoint keeps the `q` GET-parameter contract unchanged.
- **An explicitly configured DuckDuckGo endpoint selects the built-in provider**: an
  endpoint whose host is `html.duckduckgo.com` (with or without scheme) now gets the same
  POST + browser-header treatment as the default, instead of degrading to the
  custom-endpoint GET that DDG answers with the challenge page.
- **Results are auto-cleaned**: the DuckDuckGo page (and any custom endpoint's HTML
  response, by Content-Type or body sniff) is parsed into numbered `title / url / snippet`
  results; a custom endpoint's JSON/text response still passes through verbatim. A
  rate-limit/consent page degrades to "No results found", never a crash.
- **Non-2xx responses are now tool errors** naming only the status and endpoint host
  (previously the status + raw body passed through as a normal result). The M2 key
  redaction (`endpointHost`/`scrubURLError`) and the always-on SSRF floor are unchanged.
- **Scheme-less custom endpoints self-heal**: an endpoint like `search.example.com/s`
  (no `https://`) used to parse with an empty host and every request was rejected by
  url-safety; it now self-heals to `https://`. This repairs hand-edited configs — the
  shipped config template never carried a broken value (its endpoint line was always
  commented out), and first-run seeding never overwrites an existing config.

### Context compaction (`/compact`)

- **`/compact` now performs real generative compaction** (replaces the
  `ErrCompactionNotImplemented` stub). The new `internal/context.Compact` reducer
  summarizes the conversation through a single upstream call and replaces the folded
  history with one assistant summary message, keeping the protected prefix (leading
  system messages + the first user message, `Conversation.PrefixEnd`) verbatim so the
  original task framing survives. A conversation with too little past the prefix is
  skipped; a summary-call failure or cancellation leaves the history untouched.
- **Wired through `Agent.Compact`** (guarded to a quiescent boundary like `ClearContext`,
  returning `ErrInputPending` mid-Exchange). The summary call is *silent* — it reuses the
  loop's request projection but emits no `TokenEvent`/`UsageEvent`, so it neither streams
  into the transcript nor moves the live gauge; it runs at low temperature.
- **TUI** drives `/compact` on a worker goroutine (it is a real upstream call and must not
  block the `Update` loop — ADR 0011): the spinner runs, `Esc` cancels, and on success a
  "context compacted" note lands while the context-fill gauge resets so the next Turn
  re-measures the smaller fill.
- **Removed** the now-unused `ErrCompactionNotImplemented` sentinel (it was never in a
  released version).

### Fixes

- **Prompt box no longer scrolls the first line out of view as it auto-grows.** Typing past the
  wrap width grew the input box, but bubbles' `textarea.SetHeight` only repositions its internal
  view when the caret falls *outside* it — never when the box grows — so a stale downward scroll
  offset survived: the first line was hidden above and a phantom blank row showed below, with the
  caret pinned to the top visual row. `layout` (`internal/tui/model.go`) now re-seats the caret
  after a height change through the shared `reseatCaret` idiom (`MoveToBegin` "unscrolls" to the
  top, then the widget's own `CursorDown` walks back to the caret's real row, re-clamping the
  offset with none of the textarea's wrap re-derived); it runs only on an actual height change, so
  vertical caret navigation keeps the widget's sticky goal column. A companion fix corrects
  `inputContentRows` (`internal/tui/render.go`) to count the trailing row the textarea reserves for
  a logical line that exactly fills the width, so the box no longer sizes one row short at a
  wrap-fill boundary (which had stranded the same offset the re-seat could not then reach). At the
  `maxInputRows` cap the textarea's legitimate internal scrolling is preserved (offset =
  contentRows − height). Closes the prompt-scroll and auto-sizing ISSUES entries.

- **Auto mode now works on macOS — seatbelt fences the workspace correctly.** The
  `sandbox-exec` profile embedded the box's writable roots verbatim, but seatbelt
  matches a write against its *kernel-canonical* path; on macOS `/tmp` and `/var`
  are symlinks into `/private`, so a box rooted at `/var/folders/...` never matched
  the resolved `/private/var/folders/...` and seatbelt denied **every** in-workspace
  write — Auto mode could not write at all. `seatbeltProfile`
  (`internal/platform/seatbelt.go`) now resolves each writable root through symlinks
  (`filepath.EvalSymlinks`, falling back to the cleaned path for a not-yet-created
  root) before emitting the `(subpath ...)`, so the profile matches the kernel's view
  and agrees with path-safety (which already resolves the same way). Landlock is
  unaffected — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel resolves
  symlinks to the inode the rule keys on. Closes the `v1.0.0` "Box-root
  canonicalization" post-release residual; verified on real macOS hardware
  (`TestSeatbeltProbe` in-box write rows now pass under live `sandbox-exec`).

- **Context window now reads the runtime size from llama.cpp `/props`.** Discovery
  (`internal/provider.Discover`) probes `GET /props` after `/v1/models` and prefers
  its `default_generation_settings.n_ctx` — the `-c`/`--ctx-size` the server was
  actually launched with — over the model's advertised *training* window
  (`context_length`, else `meta.n_ctx_train`), which is often far larger than the
  loaded window. This fixes the live context-fill gauge measuring usage against the
  wrong denominator (it barely moved on a server loaded well under its training
  context). Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models`
  value, and a probe failure never fails discovery. Ports the oracle's previously
  deferred `llamacpp-props` strategy; the `ollama-show`/`ollama-tags` strategies
  remain unported (additive, not needed yet).

- **`/compact` and the context gauge now tell the truth.** Four fixes to the
  compaction/gauge seam that had it reporting outcomes it did not produce:
  (a) an Esc landing *after* a compaction committed reported "cancelled" while the
  history had already folded — `startCompact` (`internal/tui/worker.go`) now
  classifies the outcome from `Compact`'s returned error (`context.Canceled`), not a
  post-hoc `ctx.Err()` read, so a committed fold reports as compacted;
  (b) a no-op compaction (conversation too small to fold — the reducer's
  `Result.Skipped`) printed "context compacted" and hid the gauge — `Agent.Compact`
  now returns the skip signal through the `Engine` seam and the TUI says "nothing to
  compact" and leaves the gauge untouched;
  (c) `/clear` left the gauge and tok/s readout lit from the discarded session —
  `ClearContext` now zeroes `ctxUsed`/`tokPerSec` like a fold does;
  (d) a cancelled or faulted stream emits no terminal `UsageEvent`, so the
  generation clock survived into the next turn and mistimed its tok/s — `finishWorker`
  now clears `genStart` on every terminal message.

- **A loop fault no longer risks re-wedging the engine.** The `errMsg` handler
  (`internal/tui/model.go`) now calls `AbortExchange` before returning to the errored
  state, mirroring the `cancelledMsg` recovery: if a `Step` ever faults mid-Exchange
  the interrupted Exchange is discarded so the next `/clear` or message is accepted
  rather than refused with `ErrInputPending`. A latent fix — `Step` surfaces faults as
  an `ErrorEvent` at a boundary today — but it closes the error flavour of the post-Esc
  un-wedge. The `/compact` failure/cancel spine (both `startCompact` outcomes and the
  reducer's overflow/cancel/silence faults) is now covered by tests.

- **`/compact` now survives high context fill.** The reducer sent the *entire* rendered
  transcript as one summary request, so near `n_ctx − compactMaxTokens` the summary call
  itself overflowed (`DeltaContextOverflow`) — compaction deterministically failed at exactly
  the fill it exists to relieve, leaving `/clear` as the only recovery. `internal/context.Compact`
  now bounds the rendered transcript to a character budget derived from the discovered context
  window: it keeps the protected prefix and a budgeted tail of the most recent messages (the
  latest is always kept) and elides the middle with a `[... N earlier message(s) omitted ...]`
  marker, so the summary call stays within the window. The budget is computed in
  `Agent.compactTranscriptChars` from `Context.MaxContextTokens` (now threaded from upstream
  discovery in `cmd/apogee/wire.go`) minus the response reserve and prompt overhead; it is 0
  (render everything, as before) when the window is unknown, since there is no safe basis to
  bound. The overflow test flips from "errors cleanly" to "succeeds via the budget"; the
  unbudgetable case (no discovered window, or a server that rejects even a minimal prompt) still
  surfaces the fault cleanly with the conversation untouched. This makes on-demand `/compact`
  robust; the automatic compaction trigger (which fires *at* high fill by definition) is still
  parked in `TODO.md`.

- **Mouse selection and bracketed paste now handle the prompt correctly.** Two input
  fixes on shipped TUI behaviour:
  (a) a click or drag on a prompt row with wide glyphs (CJK, emoji) landed the caret on
  the wrong rune — `caretTo` (`internal/tui/mouse.go`) fed a display-**cell** column into
  the textarea's rune-indexed `SetCursorColumn` (clamped by cell width, not rune count),
  so a drag-copy could put **different text on the clipboard than was highlighted**. It
  now converts the cell column to a rune offset by walking the visual sub-line's runes and
  accumulating `runewidth` (the same width the widget's own cursor math uses), clamped by
  rune count;
  (b) bracketed paste (default-on in bubbletea v2) fell into `Update`'s `default:` case,
  so the textarea inserted the text but skipped the post-edit refresh — a multi-line paste
  rendered unwrapped until the next keypress, the autocomplete overlay went stale, and a
  live drag-selection's cached offsets no longer matched the value (a later copy took the
  wrong runes). A new `tea.PasteMsg` case (`internal/tui/model.go`) mirrors the keypress
  edit path: it clears the selection, inserts, recomputes autocomplete, and re-lays out;
  a paste while a worker runs is dropped, as keystrokes are.

- **A sub-agent now sees a mid-delegation mode tightening (ADR 0013).** `newChildAgent`
  froze the parent's mode at spawn, so a Shift+Tab from Auto down to Plan while a sub-agent
  ran (many Turns on a small model) flipped the footer but left the child auto-approving
  writes until its Exchange ended — a tighten-direction ADR-0005 violation. The orchestrator
  now injects a tighten-only view of the parent's live mode into the child (`Agent.liveMode`,
  the parent's `modeMu`-guarded `Mode` accessor captured as a closure — never the shared field
  or mutex). The child's disposition (`effectiveMode`) takes `TighterMode(parentLive,
  spawnMode)` — a new ladder-index helper in `internal/domain/config.go` where Plan <
  Ask-Before < Allow-Edits < Auto — so a parent tightening below the child's spawn mode
  gates/refuses the child's next call, while a parent loosening can never loosen a child
  spawned tighter (loosening mid-flight stays impossible). A top-level agent (nil view)
  behaves exactly as before.

- **Cleanup batch — leaked cancels, bounded untrusted reads, escape hardening, quit race,
  dead code.** A sweep of small hardening fixes on shipped behaviour:
  - *Leaked cancels.* `finishWorker` (`internal/tui/model.go`) nil'd the worker's `CancelFunc`
    without calling it, leaking one cancellable child context (and its timer resources) per
    completed exchange for the session. It now cancels before clearing.
  - *Bounded reads of untrusted files.* Skills discovery read `SKILL.md` unbounded at startup
    (`.apogee/skills` is always scanned — a hostile-repo OOM), and the `@file` 10 MB cap was
    checked only *after* `SafeReadFile` had already slurped the whole file. Both now bound
    before materializing — skills via an `io.LimitReader` (1 MiB/file) plus a global skill-count
    cap, `@file` via a new `security.SafeStat` fenced size check — mirroring the read_file tool.
  - *Terminal-escape hardening.* Untrusted model text and skill display names are now
    escape-stripped at the transcript boundary (`internal/tui/transcript.go`), so a
    model- or `SKILL.md`-supplied `\x1b]52;…` (OSC 52 clipboard write) or CSI payload can never
    reach the terminal. Not exploitable in the current layout (verified empirically at review),
    but this closes it at the source rather than relying on the cellbuf and footer ordering.
  - *Quit-while-busy teardown race.* `quit()` returned `tea.Quit` without joining the in-flight
    worker, so `runRoot`'s deferred `Close()` teardown could race a worker still inside `Step`
    (benign while `Close` is a no-op, a use-after-close the moment it gains real teardown). The
    exit is now deferred until the worker's single terminal Msg arrives.
  - *Dead code.* Removed the zero-caller `Engine.Mode()` seam method, the unused `fitLeftRight`
    footer helper, and the standalone `workspaceFiles` walk plus its unreachable `m.files == nil`
    autocomplete fallback (`newModel` always installs the cache). The three skill-chip
    render/ID-resolution copies were merged onto one `renderSkillChip` renderer and the shared
    `skillDisplayNames` resolver.
  - *Test gaps.* Added coverage for the loop's `UsageEvent` emission hop (Delta.Usage → event
    fields/Depth, and no event when Usage is nil), the combined skills→files→text injection
    order in one Submit, the `@file` oversize refusal, the escape-strip boundary, and the
    bounded skill-file read.

### TUI

- **Context-fill gauge restyled** to match `llama-launcher`: a solid two-tone strip —
  full blocks for the filled cells, an eighth-block partial cell (`▏▎▍▌▋▊▉`) for
  sub-cell granularity, and a solid dark-gray track behind the remainder — replacing
  the old `█░` dotted bar. Periwinkle fill, a min-sliver floor so any nonzero usage
  shows at least `▏`, and a clamp at the window limit. Bar width is now 10 cells (was
  6). The status line composes the gauge raw rather than re-wrapping it in a
  background style, so the bar keeps its own per-cell backgrounds.

### Skills system + `/skill` (apogee-code feature-parity)

- **`internal/skills` package** discovers user-authored skills — a folder
  containing a `SKILL.md` (YAML frontmatter `id`|`name`, `displayName`,
  `summary`|`description`, plus a Markdown body; a no-frontmatter fallback sniffs
  the first lines) — from three layered dirs: `~/.apogee/skills`, the workspace's
  `.apogee/skills`, and (when `use-project-skills` is on) the workspace's bare
  `skills/`. Later source wins on an ID collision. Each dir is walked through
  `os.OpenRoot` so a symlink can't escape it; a missing dir is skipped and a
  malformed skill is skipped with a soft error (one bad file never blanks the
  catalog). No builtin/embedded skills and no auto-created `~/.apogee/skills` ship
  in v1 (additive future hooks).
- **`/skill` in the TUI** — the `/` menu offers `/skill`, which chains into a skill
  picker; a pick pops a chip above the input, and submit attaches the chosen IDs.
  An empty message with skills attached is a valid send. `/skill` is deliberately
  **not** a parser command (attachment is the only way it acts), so an unknown
  `/skill foo` is still sent as an ordinary message. `/clear` and `/compact` drop
  staged chips; `/continue` carries them.
- **Attached skills now resolve** (replaces the `SkillIDs` "reserved/ignored"
  stub): the loop maps each `UserInput.SkillIDs` entry through `Config.Skills` and
  prepends its body to the user message for that one Turn (order: skills → `@file`
  blocks → user text). An unknown ID, or any ID with no resolver wired, is reported
  via an `ErrorEvent` and dropped — never silently ignored.

### Configuration

- **`use-project-skills`** (config-file only, default **true**) gates discovery of
  the workspace's bare `skills/` folder (the global library and the project's
  `.apogee/skills` are always loaded). Documented in the seeded `config.yaml`.

### Chat input mini-language (core)

- **Parse/route layer** between the TUI input box and the agent: `/`-prefixed
  lines route to local command handlers, `@file` tokens are extracted as
  references, and an autocomplete overlay (commands + workspace files, the latter
  via a bounded `os.Root` walk) mirrors the approval-prompt overlay.
- **Commands**: `/clear` (drop the model's context, keep the visible transcript),
  `/continue` ("Please continue"), and `/compact` (generative compaction — the command
  surface and the `Agent.Compact` seam landed here; the reducer that folds the history
  through them shipped in the same track, see the "Context compaction (`/compact`)"
  section above).
- **`@file` references now resolve** (behaviour change): the loop reads each
  `UserInput.FileRefs` entry within the workspace fence (`security.SafeReadFile`,
  `os.Root`-pinned) and injects its content into the user message — replacing the
  prior "refs ignored" `ErrorEvent`. A missing, oversized, or escaping ref is
  reported and skipped; the Turn still proceeds.

### Public API (additive — minor)

- `Agent.ClearContext() error` — drop the conversation history at a quiescent
  boundary (the host's transcript is unaffected); refused mid-Exchange.
- `Agent.Compact(context.Context) (skipped bool, err error)` — on-demand generative
  Compaction: summarizes the conversation and folds the history at a quiescent boundary
  (refused mid-Exchange with `ErrInputPending`, like `ClearContext`). `skipped` is true
  when the conversation was too small past the protected prefix to fold — no upstream
  call, history untouched; always false on error.
- `UserInput.SkillIDs []string` — the skills attached in chat; the loop resolves
  each through `Config.Skills` and prepends its body to the Turn (was reserved).
- `Config.Skills SkillResolver` — host-supplied resolver for attached skill IDs
  (nil ⇒ attached IDs are reported and dropped). `SkillResolver` and its return
  type `ResolvedSkill` are re-exported on the root facade; the disk-backed catalog
  stays internal (`internal/skills`).

## [1.0.0] — 2026-06-25

The first stable release. `v1.0.0` cuts the public Go API after Phase 3 brought
the agent to feature-parity with apogee-code's non-UI behaviour, with **Auto
mode confined** on Linux (landlock) and macOS (seatbelt). Every consumer — the
TUI, the bench, and the embeddable library surface — has exercised the API, so
semver now begins (ADR 0001 §18, amended).

The public surface is the root `apogee` package: `Agent` (`New`/`Resume`),
`Config` and its host delegates (`EventSink`, `Approver`, `Asker`,
`ExternalEffects`), the four-rung `Mode` ladder, the `Tool`/`ToolRegistry`
extension point with the `ReadOnlyTool`/`ExternalEffectTool` markers, the
`Event` variants, and the hook points. Tools live behind the registry (an open
extension point, ADR 0002), not as root types.

### Confinement (Auto mode is real)

- **Blast-radius confinement model** (ADR 0012, supersedes ADR 0004): a tool
  call runs without a human gate only if its blast radius is bounded — by **OS
  confinement** for the unbounded subprocess/network surface, or by Apogee's own
  **path-safety-to-workspace** for its own in-process writes. Confinement
  attaches to blast radius, at a single **subprocess granularity** on every OS
  (no in-process per-thread landlock, no thread-discard).
- **Four-rung autonomy ladder**: Plan → Ask-Before → **Allow-Edits** → Auto.
  The new `ModeAllowEdits` rung auto-approves Apogee's own workspace-scoped
  writes (no confinement needed; identical on every OS) and gates everything
  else.
- **Linux landlock backend** (`//go:build linux`): ABI probed at startup; an
  honest capability matrix (`FSWrite` at ABI ≥1 / kernel ≥5.13, `NetworkEgress`
  at ABI ≥4 / kernel ≥6.7); a confined subprocess applies the landlock domain
  after fork, before `execve`, so the child is fenced and the parent stays
  unrestricted. Raw `golang.org/x/sys/unix` syscalls (now a direct dependency).
- **macOS seatbelt backend** (`//go:build darwin`): a `sandbox-exec` profile
  generated from the `ConfinementBox` (workspace-write-only + network-open by
  default), presence-probed, no new Go dependency.
- **`Confine(ctx, box, *exec.Cmd)`** prepare-in-place contract: the tool builds
  an idiomatic `*exec.Cmd`; the backend rewrites it to launch confined. The
  `confine-to-workspace` global-config key (default `true`) tunes Auto's blast
  radius; `confine-to-workspace=false` is the explicit "I am the sandbox"
  (VM-only) opt-out. `AutoEligible()` requires filesystem confinement only;
  where confinement is unavailable, subprocess tools gate through Approval
  ("confine if you can, gate if you can't") rather than refusing Auto.

### Tools (feature-parity with apogee-code's non-UI surface)

- **File-editing family**: find-replace (single + multi), `edit`/apply-edit,
  `diff`, `open-file` — pure-Go, stateless, carrying the unexported
  `workspaceScopedWriter` marker so Allow-Edits/Auto bound them by path-safety.
- **Execution tools**: `terminal` and `python-exec` — one-shot, stateless, the
  first `Confiner` consumers; process-group teardown on cancel
  (`Setpgid` + `cmd.Cancel` + `WaitDelay`).
- **`git` tool**: branch / commit / diff-range over the system `git`, detected
  and graceful-degrading when absent.
- **`diagnostics` tool**: in-process `go/parser` + optional `go vet`,
  read-only, graceful when the toolchain is absent.
- **Network + host tools**: `web_fetch`, `http_request`, `web_search`
  (external-effect, Approval-gated as MCP-kind / auto-run url-filtered as
  network-kind per the disposition table) and `ask_user` (the new `Asker` host
  delegate). These are routed through the `ExternalEffects.Do` boundary
  (ADR 0008) so the bench can stub them.
- The existing `read_file` / `write_file` / `list_dir` / `grep` built-ins carry
  forward; `write_file` carries the workspace-scoped-writer marker.

### Processing (parity-complete port)

- **All apogee-code tool-call formats parse**: native/JSON `tool_calls`,
  markdown-fenced, and custom-regex, each gated by **ported TS test vectors**.
- **Full harmony / thinking-channel set** handled, with a `processor-factory`
  that selects the format per model/response. The package stays `domain`-only.

### Security guardrails (the human-in-the-loop layer)

- **`internal/security`** consolidates the Phase-1 per-tool path-safety into one
  reusable guard and adds **url-safety**, an **arg-guard**, a **circuit-breaker**
  (halts a runaway tool-loop), and an **audit record** (bounded ring buffer with
  a dropped-count). These run in all modes and a sub-agent inherits them.
- **Two-tier dangerous-action guard** (a footgun-guard, NOT a security
  boundary): a hard-refuse tier (`rm -rf` of root/home/system, fork bombs,
  `~/.ssh`/credential/persistence writes) and a force-approval tier
  (`curl | bash`-class). It runs first and is **tighten-only**; project config
  may only add rules, never dissolve a floor rule by ID.
- **Default-on SSRF floor** for the network tools: loopback / private ranges /
  IMDS `169.254.169.254` / link-local / CGNAT / `0.0.0.0` / NAT64 denied by
  **resolved IP** (pre-flight and at dial time, closing DNS-rebinding),
  tighten-only.

### Sub-agents

- **Sub-agent orchestrator** (ADR 0013): a sub-agent is the embeddable `Agent`,
  constructed through an internal orchestrator that threads the parent's `Mode`,
  `Approver`, `Confiner`, and guardrails verbatim (or stricter) with a tool
  **`Subset` ≤ the parent's** (ADR 0005). It is exposed as a
  dispatch-transparent **`sub_agent`** recursion point — never confined or gated
  as a unit; each child tool call gets the full per-call disposition one level
  down.
- **Isolated live guard state** (`Guards.ForSubAgent`): a sub-agent gets a fresh
  circuit-breaker and audit log over a shared read-only dangerous ruleset.
- Nested events re-emit into the parent stream at **`Depth = parent.Depth + 1`**.
- Stepping is **top-level-only for v1** behind a swappable driver; a sub-agent
  runs atomically within the parent Turn (no mid-sub-agent snapshot; cancel
  rolls back to the parent's pre-`sub_agent` boundary).

### MCP

- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk` v1.6.1):
  stdio / SSE / streamable-http transports. Server tools surface into the
  registry as `ExternalEffectTool` of kind `mcp`, so they **Approval-gate in
  Auto** under `confine-to-workspace=true` (an external server Apogee cannot
  fence). **Resume reconnects fresh** — no server-side-state promise (ADR 0008).

### TUI

- **Nested-event rendering**: `Depth > 0` sub-agent events render as a framed,
  labelled block (Phase-2's "tolerate" → "render").

### Notes

- Cross-build stays green on all 6 targets (linux/darwin/windows ×
  amd64/arm64, `CGO_ENABLED=0`); OS-specific confinement is build-tagged behind
  the `denyConfiner` (Windows/other) fallback. **Windows confinement is Phase 5**
  — Auto is simply unavailable on Windows until then.
- The `internal/` packages never import the root module path (ADR 0010).
- Direct dependency additions this release: `golang.org/x/sys` (landlock),
  `github.com/google/shlex` (terminal command splitting),
  `github.com/modelcontextprotocol/go-sdk` (MCP client).

### Known post-release verification (owner-run / CI)

These confinement **enforcement** proofs cannot run in the development
environment and are deferred to an owner-run / CI verification after the tag.
They are not acceptance failures — the hermetic disposition/logic tests (caps
honesty, generated profile strings, command rewriting, fail-closed paths) run
on every host and pass, and the live escape-probe batteries **self-skip loudly**
where the OS cannot enforce:

- **Linux landlock live enforcement** — the dev-host kernel has
  `CONFIG_SECURITY_LANDLOCK` **off**, so `confinetest.Probe` self-skips here.
  Confirm on a landlock-enabled kernel (≥5.13 fs, ≥6.7 net) that a confined
  subprocess's out-of-workspace write and non-allowlisted connect are OS-denied
  while the parent stays unrestricted.
- **macOS seatbelt live enforcement** — ✅ **confirmed on macOS hardware
  (2026-07-02).** `confinetest.Probe` now runs under live `sandbox-exec` on a real
  Mac: a confined subprocess is fenced to the workspace, out-of-box and `~/.ssh`
  writes are OS-denied, the parent stays unrestricted, and network-deny tightens
  while network-open connects. (This surfaced and fixed the box-root canonicalization
  bug below.) The Linux landlock arm above is still open.
- **Live Auto-confined deliverable run** — the opt-in `APOGEE_LIVE_ENDPOINT`
  end-to-end run (a real coding conversation in Auto, a shell write outside the
  workspace OS-denied, an MCP tool still raising Approval, a sub-agent delegated
  and its nested work rendered) is owner-run on Linux (landlock) and macOS
  (seatbelt). *(Still open.)*
- **Box-root canonicalization** — ✅ **resolved (2026-07-02).** Was a real bug, not
  just a verification gap: seatbelt embedded box roots verbatim and denied every
  in-workspace write when the root passed through a symlink (macOS `/var`, `/tmp`).
  Fixed by resolving each writable root through symlinks in `seatbeltProfile`; see
  the `[1.1.0]` Fixes entry.

[1.1.0]: https://github.com/airiclenz/apogee/releases/tag/v1.1.0
[1.0.0]: https://github.com/airiclenz/apogee/releases/tag/v1.0.0
