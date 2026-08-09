# TODO — parked ideas

Live, deliberately deferred work only. Each entry records *enough* design that we don't
re-derive it when we pick it up. When an entry closes, its body leaves this file for the
authoritative record (plan / ADR / CHANGELOG) and a one-line trail entry lands under
**Closed entries** at the bottom — so a deferral trail never becomes a silent drop, and the
file never becomes an archive.

---

## apogee-code feature parity — user-facing affordances not yet ported

**Status:** parked 2026-06-25; most of the surface has since shipped (ledger at the end of this
entry). Additive TUI/UX layers on top of the agent core, which is already at parity. Scope is
*user-facing* parity with the apogee-code VS Code extension (`airic-lenz.apogee-code` v0.2.58)
only — the by-design Phase-4 items were tracked separately.

**Verification note (the source-of-truth correction):** apogee-code's `Apogee-Code-TDD.md`
claims it has *no slash commands, only `@file`*. **That doc is stale.** When porting, treat the
shipped webview (`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js`, array `Ws`)
as the behavioral oracle, not the TDD. On send the webview posts `{text, skillIds, fileRefs}`.

**Remaining:**

- **[P1] Server / model switching** — **every switch SHIPPED (2026-07-28 the two user-facing ones,
  2026-07-29 the local-server half); the profile half is all that remains.** The shipped bodies have
  left this file for their authoritative records — see the ledger at the end of this entry
  ([ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)
  for `/model` + `/server`,
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
  for the launcher). **Remaining:**
  - The switchable **model-profile** abstraction (sampling params, context-budget %,
    thinking/tool-call format — reuse `internal/processing`), still unstarted and still
    deliberately **global**: `model-profile` is not per-model, and neither a rebind nor a server
    switch touches it. Distinct from the launcher's **Launch profiles**, which are **launch-side**
    (model file, server flags); `model-profile` is **request-side** — the grill it needs must
    keep the two "profile" namespaces from colliding in the UX. Note the producer already exists
    with no switchable consumer: `apogee probe model` prints a suggested `model-profile:` block as
    paste-ready YAML ([ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)),
    and `modelProfileConfig` (`cmd/apogee/config.go`) reads one static block today — `tool-call-format`,
    `tool-call-pattern`, `thinking`, and nothing else. Two ADRs already lean on the layer this item
    would build: [ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
    pins "global, not per-model" into the rebind contract, and
    [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md) §6 defers the
    user-after-tool wire risk to it by name.
  - Deliberate non-goals of the 2026-07-28 work, additive later if wanted: a `--save` form for
    `/server`, a `server:` startup key selecting a named entry, and persisting a switched endpoint
    in the session record (`--resume` returns to the configured one).

- **[P2] Inspector / raw-protocol view** — apogee-code's "Show Code"/Inspector (advanced mode)
  shows wire-level request/response JSON. apogee has only a hidden, non-toggleable debug field in
  `internal/tui/transcript.go`. Add a TUI toggle behind an advanced flag.

- **[P2] Undo all agent changes** — batch revert of a session's file writes (document that
  terminal side-effects are not undone, as the extension does).

- **[P2] A prompt click below a phantom-wrapped line lands imprecisely** — uncovered while fixing
  the caret walk for mid-string completion (2026-07-28). bubbles' `wrap` appends a trailing
  sub-line that `CursorDown` can never enter, so the MOUSE path's `reseatCaret` (which seats a
  click's *visual* row) can land a row short below such a line. Pre-existing, bounded (its loop
  cannot spin), and keyboard-only editing is unaffected — the logical-row walk both
  `caretToOffset` and `reseatInput` express was fixed (`promptEditor.seatCaret`,
  [ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md)). The fix wants the
  same Height-aware step expressed for a visual target.

**Related (parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

**Shipped since parking (full records in the named docs):**

- Chat-input mini-language core (`/clear`, `/continue`, autocomplete for `/` and `@`, the
  agent-side `@file` resolver) — 2026-06-26,
  `docs/handoffs/archived/2026-06-26 - 00 - chat-mini-language-core.md`.
- Skills system + `/skill` picker — 2026-06-26, `docs/plans/archived/skills-system-plan.md`;
  authoring guidance (a report-producing skill ends with `present_document`) in
  [ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md).
- `@`-file-listing cache — 2026-06-26, `internal/tui/filecache.go`.
- Throughput display + live context-fill gauge — 2026-06-26; the context-window discovery fix
  (llama.cpp `/props` runtime `n_ctx`) — 2026-06-28, `internal/provider/discovery.go`.
- `/compact` generative reducer — 2026-07-01 (`internal/context.Compact`); the automatic
  budget-driven trigger — 2026-07-04, Phase-4 item 9 (`Agent.autoCompact`).
- Session management UI (per-Turn autosave, `/sessions` browser, `--continue`, id-or-path
  `--resume`, scrollback replay) — 2026-07-24,
  [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md) +
  `docs/plans/archived/2026-07-24 - 02 - session-system-plan.md`.
- The upstream **Heartbeat** (a ten-second monitor, async startup, offline state, live rebind on
  an observed model/window change, the `/v1/models` data layer) — 2026-07-27,
  [ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) +
  `docs/plans/archived/2026-07-27 - 00 - upstream-heartbeat-plan.md`. It is the engine half of the `/server`
  item above, not that item's close-out.
- **One `/` namespace** — direct `/skill-id` invocation as inline text tokens (the chip strip
  retired), one merged command+skill menu that runs a command at accept without destroying the
  draft, `/skills`, the sole-token typo guard, resolve-gated inline accents, and the per-command
  while-running policy — 2026-07-28,
  [ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md) +
  `docs/plans/archived/2026-07-28 - 03 - slash-skills-inline-plan.md`. It closes the payload half of the
  oracle note above (`{text, skillIds, fileRefs}`) — parity of payload, not of UI.
- **`/model` and `/server`** — the picker UI over the heartbeat's prepared seams and the endpoint
  switch, both halves of the *Server / model switching* item above: the `/model` picker over
  `hb.models` driving the existing `Agent.Rebind`; the discovery hint following the bound model (the
  flap-back fix); `Agent.SwitchUpstream` + `apogee.UpstreamSpec`; the swappable per-server Monitor
  behind the unchanged `tui.Options` seams plus the new `SwitchServer` one; and the file-only
  `servers:` config key — 2026-07-28,
  [ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md) +
  `docs/plans/archived/2026-07-28 - 05 - model-server-picker-plan.md`.
- **Local server start/stop — the launcher half** of the *Server / model switching* item above:
  `/model` over the launcher's **Launch profiles** when one is configured, plus `/unload-model` and
  `/stop-server`, all over llama-launcher **v1.6.1** imported as a library at the composition root,
  behind the file-only `llama-launcher:` key (unset = auto-detect), with **Launch profile** now a
  CONTEXT.md term — 2026-07-29,
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md) +
  `docs/plans/archived/2026-07-29 - 00 - llama-launcher-integration-plan.md`. The owner-run live
  pass closed with it (six of seven scenarios; (7) waived), so nothing of this half is open —
  a later live failure reopens the item it belongs to, not this entry.
- **`/schedule` and `/schedule-stop`** — a prompt put on a cycle for as long as apogee is open, and
  the two library layers under the surface: `internal/schedule` owns every when-and-how decision
  (the cycle floor, the skip-the-tick overlap policy, the quiescence Gate, lifetime), `internal/run`
  performs one **Firing** — a fresh Agent, a fail-safe-denier Approver, the record saved at
  completion — and the TUI owns nothing but the three panes, the status note and the notices. A
  Firing browses as an ordinary session, tagged `⟳ <schedule>` in `/sessions` off two new optional
  `Meta` fields — 2026-08-04,
  [ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md) +
  `docs/plans/archived/2026-08-03 - 08 - scheduler-library-plan.md`. It settles all four branch
  points this bullet parked (overlap, which modes, `Meta`, fresh-vs-resumed) and leaves durability
  to the future daemon over the *same* library, which is the layering the enrichment asked for.

---

## Session system follow-ons — deliberately deferred from the 2026-07-24 session-system plan

**Status:** recorded 2026-07-24 at the session-system close-out
([ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md)). Named
out of scope in the plan and left for later — neither is a live gap.

- **[P2] Retention / pruning policy** — today the store never prunes: `~/.apogee/sessions/`
  grows unbounded and the only discard is the browser's `d` (manual delete). A retention policy
  (age- or count-based, opt-in via config) is deferred; the design intent is that pruning stays
  manual until there is a real need, so any auto-prune ships default-off.
- **[P2] Cross-instance file lock** — concurrent apogee instances are **last-write-wins per
  file**. Ids are per-instance unique, so a clobber requires the *same* session resumed into two
  instances at once; documented and accepted, not locked. A cross-instance lock (or a
  resumed-elsewhere detection) is the follow-on if that narrow case ever bites.

Explicitly **not** deferred (deliberate non-goals, so they are not re-opened as gaps):
session search, session export, sub-agent session persistence, and serializing
mode/approvals/confinement/MCP into the record (the last per ADR 0008). The bench keeps composing
`Snapshot`/`Encode` directly (ADR 0001) — no bench-side session-store TODO.

---

## Phase-4 mechanism catalogue — deliberately dropped / folded / deferred

**Status:** recorded 2026-07-04 at the Phase-4 close-out (`v1.2.0`). Every verdict below is
recorded in full — evidence, rationale, ledger row — in
`docs/design/mechanism-catalogue.md` (Table C, the port ledger, Table B); this entry is the
deferral trail's pointer, not the record. **None is a live gap**; revisit a verdict only if the
bench finds a specific win.

- **`codeinfo` — DROPPED (C7):** its specific missed-call-site signal is not significant
  (OR 0.69, p=0.32, gpt-oss-20b, N=75/arm).
- **`correct_tool_result` — DEFERRED, not dropped (owner-ratified 2026-07-04):** the sim defines
  no production trigger (lab-only, operator-supplied correction); the experimental
  post-tool-result hook is the lab surface. A bench-discovered trigger motivates a **new** plan
  item — the only path to porting.
- **`compress` external-client-compaction sniffing — DROPPED (C3):** apogee owns the loop, no
  external client to sniff. The surviving halves shipped: `tool_result_cap`, generative
  Compaction, `truncate_history`.
- **`intent` / `cot` / `feed_forward_correction` — FOLDED (C4/C5/C6):** absorbed into the inline
  classifier, the three completion nudges, and `validate`'s retry-in-place — no catalogue rows
  of their own.
- **Un-ported sim refinements — bench-pending (R2):** the off-ramps' retry-ladder refinements and
  `tool_loop_interceptor`'s count threshold + wall-clock cooldown; the loop's strikes-3
  self-regulation + `maxPostResponseRetries` substitute.

---

## Configurable tool × mode security matrix

**Status:** parked 2026-06-24 (Phase-3 grill). Post-v1, **additive** — config is additive,
so this is a minor bump, not a freeze break.

**The idea (owner, 2026-06-24):** let the user configure precisely how each tool behaves in
each mode — a `(tool × mode) → disposition` matrix surfaced in config.

**Why it's coherent:** the disposition table *already exists internally* — `needsApproval` /
D5 is exactly `(mode × tool-disposition) → {auto-run, confine, gate, deny}`. v1 ships it as an
explicit internal table; this feature would expose a *user-tunable* layer on top.

**The two constraints that make it safe + shippable (must hold when we build it):**

1. **Tighten-only (the law).** A user override may only make a cell **stricter** than its mode
   default (toward gate/deny) — **never looser**. Loosening a whole tool-class would silently
   dissolve a mode's guarantee (e.g. `terminal → Auto → allow, unconfined` reintroduces the
   "unsupervised *and* unbounded" hole ADR 0004/0012 forbid; `write_file → Plan → allow` breaks
   Plan's read-only promise). The **only** blanket loosen is `confine-to-workspace=false`, which
   is gated behind an explicit "I am the sandbox" acknowledgement. Narrow, explicit, opt-in
   loosens (a `NetworkAllow` host; a `terminal` command-pattern allowlist entry like `go build`,
   `npm test`) are fine — same shape as the per-project allowlist already in `ConfinementBox`.

2. **Freeze cost.** A per-tool×mode config block turns **every tool name into a frozen config
   key** and adds a sizable schema right at the `v1.0.0` cut (fights D7 — keep the v1 surface
   minimal). Deferring it past v1 avoids that; config additivity means it loses nothing by waiting.

**Related "approval-precision" knobs to design *together* with this (also parked):**
- **Command-pattern allowlist for `terminal`** — "auto-allow `go build` / `npm test`" without a
  prompt. This is the thing people usually *actually* want when they say "configure the tools";
  finer than tool-level. A narrow explicit loosen (constraint 1), so it's allowed.
- **Per-host `NetworkAllow` precision** — already a field on `ConfinementBox`; a UI/config layer
  to manage it per project belongs with the matrix.

**What v1 ships instead (so the deferral is safe):** the internal disposition table (D5) +
the `confine-to-workspace` flag (the one blanket loosen) + the existing narrow allowlists.

---

## Dedicated url-safety config key for the network tools

**Status:** parked 2026-06-24 (P3.11). Post-v1, **additive** (a new config key + a new
optional field on the network tools' `URLGuard` — a minor bump).

**The idea:** surface `URLGuard.AllowHosts` / `DenyHosts` (and the scheme allow-set) from
`config.yaml`, so a user can restrict `web_fetch`/`http_request`/`web_search` to an allow-list
of hosts, or add explicit host denials, per machine/project.

**Why it's deferred:** P3.11 ships the **load-bearing** url-safety: the **default-on SSRF
floor** (loopback / private / IMDS / link-local denied by resolved IP, pre-flight AND at dial
time — DNS-rebinding closed) is **always on** and **tighten-only** (config could only ever ADD
denials, never dissolve the floor — `URLGuard.DisableIPFloor` is a code-level opt-out, not a
config key). The floor is the security-relevant part; a user-tunable host allow/deny layer on
top is a convenience that can wait. This mirrors the **P3.6** deferral of surfacing the
dangerous-rule config + the breaker threshold into `config.yaml` (the merge logic is built and
tested; only the file-key surfacing waits). The `WebSearchEndpoint` key **is** surfaced in
P3.11 (file-only; empty now falls back to the built-in DuckDuckGo default and `off` disables —
the key selects or disables a provider rather than enabling the tool).

**The tighten-only law (must hold when built):** like the dangerous-rule merge and the SSRF
floor, a config url-safety layer may only **tighten** (add `DenyHosts`, narrow `AllowHosts`) —
it can never remove the SSRF floor or widen the scheme set past the safe default.

**The runway is now clear (2026-07-26) — this is a smaller, safer change than it was.** The
normalisation group of the url-safety live-gap audit landed first, on purpose: this key is what
populates `AllowHosts`/`DenyHosts` and would have converted three then-latent defects into live
ones the day it shipped. All three are fixed (`docs/plans/archived/2026-07-26 - 03 -
url-safety-live-gap-plan.md`, items **8–10**): the URL is normalised **once** by
`security.NormalizeURL` and the guard now matches the same string the transport dials (trim, IDNA,
lowercase, one trailing dot stripped — so appending a dot no longer defeats a `DenyHosts` entry),
the unparseable-url error no longer leaks a key-bearing URL, and the RFC 8215 NAT64 local-use
prefix is denied. Whoever builds this key inherits a host-matching path that is already correct;
it needs no normalisation work of its own. **The remaining trap is unchanged and is not this
plan's:** `HostTools` is composed in two places (`internal/agent/construct.go` and
`cmd/apogee/wire.go`, the engine-side one unexported), so a new `HostTools` field must be added in
**both** or it is silently dropped on one path. That duplication is a deferred
`/improve-codebase-architecture` candidate, not a blocker.

---

## An embedder cannot register a *vouched-for* network tool (export the network funnel?)

**Status:** parked 2026-07-25 (the url-safety choke-point plan, D5). Post-v1, **additive** (a new
exported constructor in `internal/tools` re-exported by the facade — a minor bump).

**The situation.** Apogee's own network tools carry the **unexported** url-filter marker, obtainable
only by embedding the network funnel that owns the `URLGuard` (`internal/tools/network.go`), and the
Auto ladder keys the "runs unattended" cell on that marker (ADR 0012 amendment 2026-07-25). So a
host-registered tool that reaches the network **gates in Auto no matter how carefully it filters its
own URLs** — it has no way to route through the funnel and no way to claim the marker. This is
deliberate and it exactly mirrors `workspaceScopedWriter`, under which an embedder's write tool has
gated in Auto since Phase 3: the marker is unfakeable *because* it is unexported, and gating is the
safe direction.

**The natural move if demand appears:** export the funnel as a public `NewNetworkTool`-style
constructor (host-supplied `URLGuard` in, an embeddable value out) so an embedder's tool **inherits**
the marker by actually routing every URL through the guard, rather than being handed a way to
*assert* it. The property to preserve: the marker must remain impossible to obtain without the
guard — never an exported interface, a declared capability, or a config-listed tool name, each of
which would let an under-declaring tool reach the network unfiltered and unattended.

**Why it waits:** the demand is hypothetical (no embedder has asked), the current answer is merely
an extra prompt rather than a broken tool, and the export is purely additive whenever we want it.

---

## Two doors left open by the Mechanism-registration collapse

**Status:** parked 2026-07-25 (`docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`,
"Explicit non-goals" + D4; [ADR 0003](docs/adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)
amendment 2026-07-25). Both were named out of scope by that plan and recorded here so the door is
documented rather than silently shut. **Neither is a live gap.**

- **`MechanismRegistry.Add` does not reject an empty `Descriptor.ID`.** `Add` gates on the reserved
  `experimental` ID, on a duplicate ID, and on the value implementing at least one hook interface —
  not on the ID being non-empty. A row registered with a zero descriptor therefore gets a catalogued
  Mechanism with an empty canonical ID: it sorts first in the stable tiebreak, and `MechanismFiredEvent`
  attribution for it is blank. This is **pre-existing, not introduced by the row shape** — a Mechanism
  whose `Descriptor()` returned the zero value could do exactly the same before — which is why adding
  the guard was refused as a behaviour change riding on a refactor. It is unreachable from the
  catalogue (`register` already panics at `init()` on an empty `descriptor.ID`, and the ID keys the
  table), so the only way in is a hand-built row from an embedder or a test. Worth a guard later, as
  its own small change with its own test: reject an empty `Descriptor.ID` in `Add` alongside the
  reserved-ID gate, with a message in the same voice as the other three.

- **`internal/mechanisms` declares which `Deps` a row needs, but does not construct them.** A row now
  carries `needs DepNeeds`, and `DepsNeeded(ids)` ORs the flags for an enabled set, so the engine
  derives exactly the collaborators the enabled rows asked for and its build loop is uniform for every
  ID. The **construction** stays in `internal/agent`'s `deriveDeps` — the library store under
  `Config.LibraryDir`, the corrupt-store-degrades-to-empty stderr notice, and the
  `ResolveFingerprintFrom` identity ladder. Moving that wiring next to the row that needs it reads
  better and was considered, but it contradicts
  [ADR 0015](docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md) §2 (*"Deps stay
  internal; the engine derives them from Config"*) for no gain the declaration does not already give.
  Revisit only if a **second** `Deps`-bearing Mechanism arrives and the derivation genuinely wants to
  live beside its row — and treat it as an ADR 0015 amendment, not a refactor, because §2 is what
  makes "arming the library Mechanism against a different model than the loop runs" unrepresentable.

---

## `Request.InjectContext` placement — a `domain` data type encodes chat-template role-safety policy

**Status:** parked 2026-07-26 — carried out of the 2026-07-24 architecture-deepening review so that
review's ledger could close honestly. It is the review's own verdict that parks it, and the verdict
stands as written: ***Speculative* — it reopens an ADR-0010 line; flagged, NOT recommended without a
grill; the current placement is defensible.** So this is **not a code TODO**: no code change, no ADR
amendment, and no "obvious" move happens here until an owner grill (`grill-with-docs`) settles the
question. It is not a live gap either — no wrong output, no bug, no broken invariant; the ladder
below is doing its job. Full card:
[`docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md`](docs/reviews/archived/2026-07-24%20-%2000%20-%20architecture-deepening-review.md)
(the smaller-deepenings list, last entry).

**The question, in one line.** `Request.InjectContext` — a method on a `domain` **data type** —
encodes *chat-template role-safety policy*, while the engine / `internal/context` layer is where
role-alternation is otherwise owned. Which layer should own the placement decision?

**What the code does today** (`internal/domain/hooks.go:391–422`) — recorded so the grill starts from
behaviour rather than re-reading it. `InjectContext(text)` picks the landing spot with a three-branch
ladder:

1. history ends in a **tool result** → append to the system prompt instead (a user message after a
   tool result breaks strict chat templates);
2. history ends in an **assistant** message → append at the end (the retry-exchange shape: the
   correction answers the superseded assistant message it follows, R1);
3. otherwise → insert **before** the last user message — plus the F2 boundary maintenance, since an
   insert below the frozen `committedLen` shifts every committed message right by one and the
   boundary must advance with it;

with no user message present at all, it appends at the end. Sibling for contrast: `AppendToSystem`
(same file, `:378`) is the marker-idempotent system-prompt inject; `InjectContext` carries **no**
marker of its own (noted at `internal/mechanisms/filehint.go:44`).

**The tension.** Strict-template alternation is otherwise the engine/context layer's business:
`internal/context/compact.go` (`:52`, `:105`, `:113`) shapes the folded history and the summarizer
call to keep clean alternation, and `internal/agent/compact.go:193` asserts alternation holds after
`Conversation.Replace`. Chat-template policy therefore has two homes — a `domain` value type and the
engine's reducers — which is the whole of the observation.

**What it reopens, i.e. why this is not a free move.**

- **An ADR-0010 public-surface line.** ADR 0010's canonical placement rule puts every public type in
  `internal/domain` with the root re-exporting it — `apogee.go:416` is `type Request = domain.Request`
  — and ADR 0010's *Stability* consequence states the public surface **is** the set of root aliases
  (ADR 0001 §18). `InjectContext` is a method on that aliased type and is a documented member of the
  hook mutation API (`docs/design/hook-mutation-api.md` §3, the `Request` pre-request surface, and
  its §7 traceability table — a P0.1 draft, so read its line references as stale). Moving it
  is a **breaking public-surface change**, not a refactor — and ADR 0010's own lowest-layer rule
  ("a type lives at the lowest layer that can define it without importing upward", and pure logic
  intrinsic to those types lives with them) is simultaneously the strongest argument that the current
  home is *correct*, not accidental.
- **An ADR-0017 pairing.** ADR 0017 §1 explicitly routes `InjectContext` (and
  `conversationView.LastUser`) through the single Exchange-boundary derivation "with their public
  behaviour unchanged" — `internal/domain/exchange.go`'s `lastRoleIndex` is THE boundary core. And
  `exchange.go`'s header states the Exchange boundary is stable *because* `InjectContext` places
  injections before the last user message or in the system message, **never after it**. The placement
  policy and the boundary derivation are load-bearing for each other; any move must say what happens
  to that pairing.

**Blast radius, so it is known before the grill, not during.** Five non-test callers:
`internal/agent/loop.go:333` (the retry correction) and `:560` (the deferred-correction drain), and
the Mechanisms `readloop.go:89`, `filehint.go:111`, `guided_decomposition.go:156`. Several Mechanisms
additionally *reason about* where the inject lands — `guided_decomposition.go` (`:175`, `:196`,
`:373`, `:516`) keys its idempotency and boundary logic on the roles `InjectContext` writes — so the
ladder's behaviour is depended on beyond the call itself.

**What a grill has to settle (the branch points, so nothing is re-derived):**

- Is the three-branch ladder **policy** (belongs with whoever owns chat-template shape) or **pure
  logic intrinsic to the `Request` value** (ADR 0010's lowest-layer rule — the argument for today's
  home)? Everything else follows from this one.
- If it is policy: what is the public replacement, given `Request` is a root alias and the hook API
  documents the method? A seam the engine injects, versus keeping the method and moving only the
  *decision* behind it, are different-sized breaks.
- What happens to the ADR-0017 pairing — does the "never after the last user message" property stay
  guaranteed by construction, or does it degrade to a convention the boundary derivation merely hopes
  for?
- Is the honest outcome instead to **close** the question — an ADR 0010 (and/or 0017) note naming
  `domain` the deliberate owner of role-safety placement, so it stops being re-flagged at every
  architecture pass? A review that says "defensible" twice is evidence for this branch.

---

## Deferred security-review Lows (P3 `/security-review`, 2026-06-24)

Recorded so the deferral is deliberate, not a silent drop. Each is an INTENDED-design
acceptance or a future-task re-verification, NOT a live hole.

- **[L1] `MergeDangerousRules` tighten-only path is dead code (floor fixed by absence).** The
  project-config dangerous-rule merge (`security/rules.go`, `projectAdd` tighten-only) is never
  called — `guards` is always `NewDefaultGuards()` — so the "project cannot loosen the floor"
  property is currently true **by absence**, and the merge's tighten-only invariant lives only in
  `rules_test.go`. **Deferred** because there is nothing to fix today: when the project/global
  config merge is wired (the parked "configurable tool × mode matrix" / dangerous-rule config
  surfacing above), re-verify the project/global split end-to-end at that point. No change now.

- **[L3] Confined subprocess can read any host file + open network ⇒ exfiltration is in-design.**
  `platform/landlock_linux.go` handles only WRITE accesses (read/exec unrestricted) and the
  network is open by default. A confined Auto subprocess can `cat ~/.ssh/id_rsa` and POST it out.
  **Deferred — INTENDED per ADR 0012**: the box bounds *writes* (stops clobbering the host), the
  network is open by default, and `confine=false` is the only blanket loosen. Recorded as a
  conscious v1.0.0 acceptance. If read-confinement or default-deny egress is ever wanted it is an
  ADDITIVE box tightening (landlock read-handling + a per-host network filter), not a v1 change.

- **[L4 enhancement] Optional env-allowlist scrub for stdio MCP launches.** A configured stdio
  MCP server inherits Apogee's full process environment (all secrets) — see the trust note in
  `internal/mcp/transport.go`. This is **intended** (a trusted, host-configured launch), so v1
  documents the trust rather than scrubbing (a blanket scrub would break MCP servers needing
  inherited PATH/HOME/runtime vars). **Deferred — optional**: a future per-server `EnvAllowlist`
  (mirroring `safeGitEnv`) for a host that wants to run a less-trusted stdio MCP server. Additive,
  post-v1.

(L2 — the dangerous-action guard normalising only whitespace+case, trivially evadable — needs no
entry: it is ADR-0012 by-design, and `internal/security/doc.go` already states the guard is "NOT
a security boundary." No doc/UI describes it as one, which is exactly what L2 asks for.)

---

## Mid-Exchange auto-compaction (fire at Turn boundaries under budget pressure)

**Status:** parked 2026-07-05 (guided-decomposition grill). Auto-compaction fires only at
Exchange boundaries (`internal/agent/compact.go`), so a long multi-Turn Exchange — e.g. a
serialized sub-agent fan-out, where every child report lands inside *one* Exchange — has no
generative reducer available for its entire life; only `tool_result_cap` (default-off) can
reduce mid-Exchange. Guided decomposition covers this with a descriptor `Requires` on
`tool_result_cap`; the structural alternative is letting auto-compaction also fire at
quiescent *Turn* boundaries under pressure. That changes a structural reducer's contract
(interacts with the saturation logic, the protected prefix, and bench comparability), so it
needs its own grill and bench evidence — deliberately not a rider on the decomposition work.

---

## Startup notices are stderr-only — the in-transcript banner

**Status:** extracted 2026-07-27 from the closed *Auto-mode confinement degradation* entry (its
one still-open residue) so the closed body could leave this file. The underlying parked idea is
the validated-set in-transcript banner — deferred follow-up 04 of the validated-set work
(`docs/plans/archived/validated-set-runtime-surface.md`: "no TUI in-transcript banner in v1,
revisit if the stderr line proves easy to miss").

Every startup notice (the validated-set notice, the Auto-confinement degradation notice) prints
pre-alt-screen on **stderr only**; `/confine status` renders the same facts in the transcript on
demand, but the startup moment does not. Folding startup notices into the transcript means
building the banner rendering the degradation plan deliberately did **not** build (its item 7
used the existing notice path and named follow-up 04 the owner of any framework). No wrong
behaviour — pick it up when a stderr notice proves easy to miss.

---

## A presented document carries no sub-agent depth (the ⤷ label re-opens around it)

**Status:** parked 2026-07-21 (noticed while verifying the `present_document` plan,
`docs/plans/archived/2026-07-21 - 01 - present-document-tool-plan.md`). Cosmetic, no wrong output.

`domain.PresentRequest` carries no sub-agent depth, so `internal/tui`'s presentation entry is
always rendered at depth 0 — unrailed even when a sub-agent presented the document. Because
`renderView` opens the `⤷` label whenever a block descends deeper than the previous one, a
depth-0 presentation inside a sub-agent run splits that run and the label is announced again
after it. Not presentation-specific: any depth-0 entry between two nested blocks does the same
(a `· cancelled` note already can). The fix is to carry the Step's depth on `PresentRequest` and
render the entry at it, which is a domain-seam change and wants its own decision — the loop's
depth is not currently exposed to a host delegate at all (`domain.AskRequest` has the same gap).

---

## Adaptive prompt complexity — request slimming driven by the capability tier

**Status:** parked 2026-07-22 by decision, not by omission
([ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
Q3; `../apogee-sim/mission.md` item 2). Phase 5 ships the **capability tier** as a reported
`apogee probe model` field and stops there.

The idea: a pre-request transform that shapes the outgoing request to what the model can
actually digest — stripping tool descriptions down to names and one line, shortening the system
prompt, simplifying output formatting — selected by the tier the probe observed. It is the
mission's "prompt complexity tier" and aims squarely at the smallest models, the ones this
project exists for.

Why it is not built with the probe: this is model-facing behaviour inside the loop, i.e. a
**Mechanism** by definition, and a Mechanism earns its place on the non-inferiority gate against
Bypass, per model, with a catalogue row and a Table B bench-validation entry
([ADR 0009](docs/adr/0009-the-ab-decision-rule.md); the Phase-5 settled design: nothing
model-facing ships default-on without bench evidence). Shipping it alongside the probe would
mean either an unvalidated default-on transform or a catalogue row with a placeholder where its
evidence belongs. The tier signal costs nothing and is already there when the evidence is.

When picked up: catalogue it as a **pre-request** Mechanism, **default-off**, gated on a stored
probe record's tier (so it no-ops entirely for an un-probed model), and bench it on at least one
small model before any default flips. Open design questions kept warm: whether slimming applies
per-request or per-session (a mid-session change of tool descriptions is a history-consistency
question), and whether the tier or the individual battery findings (native tool calls vs. JSON
vs. multi-step) are the better gate — the findings are strictly more informative, the tier is
strictly easier to reason about.

---

## Host-override knob for the rendered instruction block

**Status:** extracted 2026-07-27 from the closed *system-prompt / template story* entry (its
Residual 1); originally the prompt-seam plan's D1 *rejected hybrid* (engine-owned won —
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)).
Additive-later, demand-driven.

The idea: a host (embedder) knob to override the engine-rendered instruction block. The
obligation the old entry attached to it is discharged: because the user's prompt, the mechanism
directives and the profile's tool block merge into **one** system message, a host-supplied block
would replace the rendered block *inside that same message* without reshaping anything ADR 0023
decided — the two compose; they do not fight. Build only if an embedder asks.

---

## A marker phrase in the standing system content suppresses that Mechanism's directive

**Status:** filed 2026-07-28, the entry
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)'s
Consequences asked for (that ADR carries the full record and the rejected alternatives);
[ADR 0026](docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md) widened it to a
second source. **Accepted, not a defect to fix on sight** — revisit if a real prompt trips it.

`Request.AppendToSystem(marker, text)` is idempotent by `strings.Contains` over the FIRST system
message, and the markers are natural-language phrases — `decompose`'s `"Focus on one action"`,
`cot`'s `"have not read any files yet"`, `library`'s `"[Apogee context notes"`. That message is now
content the user supplies: the configured system prompt (ADR 0023) and, since ADR 0026, the
workspace context files folded in beside it. A prompt — or a repo's own `AGENTS.md` — that happens
to contain one of those phrases therefore reads as "already injected", and the Mechanism stays
quiet.

It is a **suppression, not a corruption**: the request stays well-formed, and the user's own
sentence on that subject is what the model reads instead. The blast radius is bounded to the
catalogued nudge Mechanisms, which are default-off and enabled per model on bench evidence
([ADR 0015](docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md)).

**Standing (do not re-file as new ideas):** both obvious fixes were weighed and declined in ADR
0023 — a non-textual idempotency channel changes the hook API's contract for every Mechanism, and
opaque sentinel strings would put text in front of the model whose only reader is apogee.

---

## Phase-5 verification leftovers — the owner-run passes this machine cannot perform

**Status:** carried forward 2026-07-22 from the "Owner-run checklist" of the archived
[`docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`](docs/plans/archived/2026-07-22%20-%2000%20-%20phase5-cross-platform-hardening-plan.md)
(read it for full context). Phase 5 is **implemented**, and everything runnable here has since
run green (2026-07-23, recorded in the CHANGELOG + the archived plans): `make check` under
`-race` on the Linux devbox including the live landlock enforcement battery, `make live-eval`,
`TestSmokeLiveProfileSeam`, and the **Linux** arm of the live Auto-confined deliverable run.
What remains needs hardware this environment does not have:

- **Live Auto-confined deliverable run on Windows** — the ADR 0020 backend is proven natively
  (escape battery + the real `Terminal` tool under `platform.NewConfiner()`); an end-to-end
  deliverable session under Auto is what remains, if an LLM endpoint is reachable from that
  machine.
- **Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke** — same shape; the
  darwin binary cross-builds clean (amd64+arm64, re-verified 2026-07-23), so what remains on a
  Mac is `--help`, a trivial session, and the confined Auto run.
- **Degradation notice on a below-floor Windows host (< build 17763)** — the deny-vs-token
  decision itself is table-proven (`TestBelowWindowsFloor` in the untagged `winguard.go`
  predicate); only the on-host UX observation of the notice stays untested (recorded so in
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md)'s
  consequences), verifiable only if such a host turns up.

**Not repeated here:** the "pre-existing, NOT Phase 5 scope" group (Linux/macOS live-run
variants already tracked elsewhere) lives in the CHANGELOG's **"Known post-release verification
(owner-run / CI)"** note — that note, not this file, is its tracking home.

---

## Windows Auto: box-local `%TEMP%` / toolchain caches

**Status:** recorded 2026-07-22 (Phase 5 review fixes,
`docs/plans/archived/2026-07-22 - 02 - phase5-review-fixes-plan.md` item 12). Not built by that
plan by decision — it needs its own design session. Sources: ADR 0020 §2 (the "Consequence for the
box builder" paragraph) and `docs/design/confinement-execution-contract.md` §7.

**The gap.** ADR 0020 §2 names the toolchain's cache/temp dirs a **hard prerequisite** on Windows,
not the ergonomic nicety contract §7 treats them as on Linux/macOS: the confined child runs under a
*low-integrity* token, and a Low process cannot write to an unlabelled (Medium) directory at all.
`%TEMP%`, `$GOCACHE`, `~/.npm`, `~/.cargo` and the pip cache all live outside the workspace and are
unlabelled, so under the Windows fence a confined `go build`, `go test`, `pip install` or `npm ci`
fails outright — not with a partial result, but at the first write to its cache or temp dir. The
workspace-scoped writes the fence does cover work fine; toolchain work under Auto does not.

**Why nothing bridges it today.** The box field that would carry those dirs,
`domain.Config.ConfineWritablePaths`, has exactly **one reader** —
`internal/agent/dispatch.go:121–125`, which copies it into `domain.ConfinementBox.WritablePaths` —
and, repo-wide, **no writer**: nothing probes for toolchain caches and nothing surfaces the field in
config, so it is always empty. Contract §7's own recommendation ("seed `WritablePaths` with the
detected toolchain cache + temp dirs by default, probed, not hard-coded") was never implemented on
any platform; on Windows it is the difference between a usable fence and an unusable one.
`internal/platform`'s `ScopeEnv` (the environment-scoping seam ADR 0020 §2 points at) exists and is
used by `git`, but `terminal` and `python_exec` inherit the user's `%TEMP%` unchanged.

**The design question to settle when this is picked up:** a **box-local `%TEMP%`** — point the
confined child's `TMP`/`TEMP` (and `GOTMPDIR`, `GOCACHE`, …) at a labelled directory inside the box
via `ScopeEnv`, so nothing outside the workspace is ever marked — **versus labelling the user's own
cache/temp trees**, which is simpler but marks large, long-lived, shared trees Low (ADR 0020's own
"keep the labelled surface small" argument, and the ~1 ms-per-object walk cost, both cut against
it). ADR 0020 §2 already prefers the box-local answer, and calls it environment-scoped execution
plus box construction, **not** a `Confine` responsibility — so the work lands in the box builder and
the execution tools, and the `Confine` contract (§2) is unaffected. Whichever way it goes, it also
decides whether cache dirs are *probed* per toolchain or named in config, and it is the natural
moment to give `ConfineWritablePaths` its first writer.

---

## The TUI width authority — what it did not convert

**Status:** parked 2026-07-31, the residue of the *width authority* plan (`2026-07-31 - 03`, under
`docs/plans/`, archived on completion) —
[ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md). Nothing
here breaks the absolute width cap; each is a place the package still measures in a measure the
painter may not be using, or mirrors a widget imperfectly. The `inputContentRows` residue was
closed on 2026-07-31 (owner-approved follow-up); its entry stays below with what remains of it. The
`wrapText` residue — the last site ADR 0030 itself named — was closed on 2026-08-03, and so was the
one it uncovered: the boxed surfaces composed by `lipgloss.Style.Width`, which turned out to be two
sites rather than the one the entry named. What is still open here is the two width entries at the
end: the widget mirrors' tab handling, and `hangingPrefixes` at block width 1–2.

**~~The four sites the plan could not touch~~ — FIXED 2026-08-03.** `popup.go` and `interject.go`
measured with `ansi.StringWidth`, and `truncateToWidth` (`popup.go`) both measured and cut in it.
They were left because plan `2026-07-31 - 01 - popup-column-alignment-plan.md` owned those files
while it was live; that plan is archived, so the rename was made — `popupColumnWidths`,
`layoutPopupRow`, `popupElisionMarkerFitting`, `popupTitleLine` and `truncateToWidth` now take
`th theme` and measure (and truncate) with `th.measure`, and `Model.queuedRow` (`interject.go`)
does the same. The drift they left possible — a popup column landing a cell off, and the staged-row
band one column short of the window, on a row carrying VARIATION SELECTOR-16 — is pinned under both
width methods by `TestPaintedPopupColumnsHoldOneOffset`, `TestPopupTruncationFollowsThePainter` and
`TestPaintedQueuedBandFillsTheWindow` (`paint_test.go`).

**~~`wrapText` still wraps with `ansi.Wrap`~~ — FIXED 2026-08-03** (`render.go`). It breaks with
`th.measure.Wrap` now, so the break is CHOSEN in the same measure the cap enforcement holds and the
painter draws in; `render.go` imports no hard-wired `x/ansi` helper at all any more. The re-baselining
the entry expected did not materialise — on content the two measures agree about the two wraps are
identical, so no existing test moved — and what the move buys back is the cell the wrap used to give
away on a line carrying VARIATION SELECTOR-16 under the WcWidth painter.
`TestWrapTextBreaksInThePaintersMeasure` and `TestWrappedSurfacesBreakInThePaintersMeasure`
(`render_test.go`) pin it under both width methods.

The **user block's rows had to move with it** (`render.go`, `renderUserBlock`): they were padded to
the block width by `th.userBlock.Width(width)`, and a lipgloss `Width` style does not merely pad in
GraphemeWidth — past its width it *wraps*. Once the wrap takes its break in the painter's measure, a
prompt line the authority calls exactly the block width can measure wider to lipgloss, and the style
folded it in two, smuggling a `"\n"` into ONE element of the `[]string` the whole line-oriented
renderer counts rows with (the viewport height, the sticky offsets and the `userBlocks` ranges all
count off it). They are squared with `squareLine` now — the painter's measure, the way
`promptMarkerRow` already padded its own row. `TestUserBlockRowsAreOneSquareLineEach` pins it.

**~~The pop-up pane is composed in lipgloss's measure~~ — FIXED 2026-08-03** (`popup.go`,
`render.go`, `model.go`). `blackFill` (`lipgloss.NewStyle()…Width(inner)`), `th.userBlock.Width(inner)`
on the selected row and `th.popupBorder.Width(width)` on the whole box all padded — and past their
width *wrapped* — in GraphemeWidth whatever the painter was doing, so a pane line the authority
called exactly `inner` cells wide was folded into two pane rows and the box outgrew the row budget
`popupBudget` granted it (a single row of `"⚠️ "`×6 at width 20 painted a five-row pane where the
pane composed four). It was pre-existing and independent of the wrap — reachable through pop-up
ROWS, which never touch `wrapText`.

**The start-up card was the same defect at a second site**, and this is what closing the entry
turned up: `th.startupBorder.Width(width)` (`render.go`, both layouts) folded a VS16-carrying info
row the same way — seven painted lines from six composed, with the fold splitting the model name
across two rows. One class, two instances, one fix.

The fix is `drawBox` (`model.go`, beside `squareLine`): the box's own rows are **drawn** — border
glyphs, padding and the squaring pad emitted as separately styled runs, the content squared to the
inner width with `th.measure` — rather than delegated to `lipgloss.Style.Width`, which ADR 0030 §5's
reasoning rules out for a bordered surface. `lipgloss.JoinVertical` went with it, being the same
GraphemeWidth pad one level in. `TestPaintedBoxRowsAreNotFolded` (`paint_test.go`) pins
composed-rows == painted-rows, and every row's right border, for both surfaces under both width
methods. Both `lipgloss.Style.Width` sites that remain are correct where they stand: the prompt box
(`inputView`) frames a widget that wraps in GraphemeWidth itself — the widget-mirror exception of
ADR 0030 §6 — and nothing else in the package sets a `Width`.

**~~`inputContentRows` is an unfaithful widget mirror~~ — FIXED 2026-07-31** (`render.go`). It sized
the prompt box to the rows it *thought* the textarea drew, and it was measurably wrong: against a
real textarea's `LineInfo.Height` it said three rows for `"hello world"` at width 5 where the widget
draws four, two for `"a b  c"` at 3 where the widget draws three, four for `"a-b-c-d"` at 3 where
the widget draws three, and it differed on ~41% of 4000 random prompt-shaped inputs — under-counting,
which is the box-one-row-short failure its own docstring describes. It is now
`Σ len(wrapRowStarts(line, w))`, so the box's height and the rows the accent pass paints on come off
one ruler instead of two derivations of the same wrap.
`TestInputContentRowsMirrorsTheWidget` pins it to the
widget itself — `DynamicHeight` makes a real textarea publish its own `totalVisualLines` as its
height, which is the whole-value counterpart of the per-line `LineInfo.Height` oracle
`wrapRowStarts` is pinned to — over the three cases above plus 2100 generated drafts.

NOTES (2026-07-31): **no clamping adjustment was needed** for the taller counts the fix can now
return. `promptEditor.rows` already holds the count to `[minInputRows, maxInputRows]` and `layout()`
sizes the viewport from the *clamped* box height, so a larger raw count only means the box reaches
its 10-row cap sooner and the widget scrolls internally from there;
`TestPromptEditorRowsClampsTheWidgetCount` pins that the editor's height is exactly the clamp of the
unclamped count. Two stale test comments claiming `inputContentRows` deliberately disagrees with the
widget (`prompteditor_test.go`'s `wrappedRowsOf`, `skill_test.go`'s wrapped-first-line premise) were
corrected in the same change — both oracles still ask bubbles rather than the mirror, which is now
the reason rather than the disagreement.

**What is left of this entry:** both mirrors are still wrong on **tabs**, which the widget's input
sanitizer expands and neither mirror does. Fixing it means expanding tabs the same way before
measuring, in `wrapRowStarts` (which both mirrors then inherit).

**`hangingPrefixes` can draw three cells at block width 1–2** (`render.go`). It floors its wrap
width at 1 column and then prepends a two-column marker, so a bullet list in a two-column block
produces three-cell lines. Pre-existing and untouched by the width work; a real fix has to decide
what a marker means when the block cannot hold it, which is a `layout.md` question, not a
measurement one.

---

## A model-facing `schedule` tool — daemon-era, not v1

**Status:** parked 2026-08-04 (assessment at the `/schedule` close-out,
[ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)).
The question: should the model be able to create Schedules itself, via a tool, instead of telling
the human to run `/schedule`?

**Why not now — two costs, one thin benefit.** The benefit is a keystroke: a TUI-hosted Schedule
dies with the process, so the model would only ever set up something short-lived the human can
create with one command. Against that: (1) **catalogue dilution** — every schema costs context and
tool-selection accuracy for the 4B–35B target class, tools are not per-model curated the way
Mechanisms are, and a tool relevant in one session in fifty is exactly what the never-worse
invariant says to refuse; (2) **authorization inversion** — ADR 0033 decision 3 makes the *human's*
creation choice the mode authorization, and a model-created Auto schedule is the model granting its
future self unattended runs; inside a Firing it is self-replication (a firing scheduling more
firings).

**The shape when built (so it is not re-derived).** Layering is *not* the blocker — the precedent
is `ask_user`: a driver-supplied delegate seam, nil = tool not registered, and
[ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s open
registry means the driver that owns the scheduler registers the tool without touching the engine.
Creation routes through the **Approver** as a gated (non-ReadOnly) action, which buys the key
property for free: a Firing's fail-safe denier refuses it, so self-replication is structurally
impossible with no special-casing, and Ask-Before preserves decision 3's human authorization at the
gate. The residual hole is Auto (gated-but-eligible runs unsupervised — the exact bad case):
"create an Auto schedule" belongs in the footgun-guard's dangerous tier, or the tool caps
model-creatable schedules at Plan. Sub-agents don't inherit it (ADR 0005).

**The trigger to pick it up: the daemon.** Durable schedules that survive quit are where a model
setting up its own monitoring has real value, and the companion feature is the cross-session
approval surface ADR 0033 already named when it rejected Ask-Before schedules. Build the tool once,
at that layer; the TUI still never has to register it. **Not the vehicle:** MCP — schedules are
in-process driver state, Firings run without MCP, and ADR 0031 forbids first-party connectors.

---

## The published binaries are not code-signed

**Status:** parked 2026-08-05, the day the first prebuilt binaries shipped (v0.11.0 — all six
targets as release assets, plus the `airiclenz/tap` Homebrew formula that installs them). The
README states the gap where the download is rather than burying it, so this entry exists to keep
that statement honest and to hold the design once it is picked up.

**What the gap actually is, per platform** — it is two different problems wearing one name:

- **macOS.** The Go linker already ad-hoc signs `darwin/arm64` (verified on the v0.11.0 asset:
  `LC_CODE_SIGNATURE`, 162 KB), which is the *load* requirement on Apple Silicon — without it the
  kernel SIGKILLs the process, so the binaries do run. What is missing is **Developer ID signing +
  notarisation**, which is the *Gatekeeper* requirement: a browser download carries
  `com.apple.quarantine` and is refused until `xattr -d com.apple.quarantine` clears it. A `curl`
  download never sets the attribute, and Homebrew's own download does not either — so the brew path
  and the documented `curl` path are both unaffected today. Only the browser path is.
  `darwin/amd64` carries no signature and needs none to load.
- **Windows.** No signature at all, so SmartScreen warns about an unrecognised publisher. An
  Authenticode certificate is the only fix, and reputation accrues per-certificate over time.

**Why it is deferred, not dropped.** Both halves cost money and identity, not engineering: an Apple
Developer Program membership (notarisation also needs a per-release upload to Apple's service, so
it puts a network round-trip and a credential into `make dist`'s path) and an Authenticode
certificate from a CA. Neither belongs in a pre-production 0.x release cut from a laptop, and
`SHA256SUMS` — published as a release asset, and what the Homebrew formula pins per platform — is
the integrity check that is actually available today.

**The design question to settle when this is picked up:** whether signing stays a *release-time*
step layered over `make dist` (the target keeps producing unsigned archives, and a separate
signing/notarisation pass rewrites them before upload) or moves *into* it. The layered answer is
strongly preferred: `make dist` is cross-platform by construction — one CGO-free `go build` per
target from whichever machine cuts the release — whereas `codesign`/`notarytool` run only on macOS
and `signtool` only on Windows. Folding either in would make the whole matrix un-buildable from a
single host, which is the property the target exists to have. That points at CI (a signing job per
platform, secrets held there) rather than at the Makefile, and CI-cut releases are not a thing this
repo does yet — settle that first.

---

## Closed entries — the one-line trail

Full records live in the named docs; a line here keeps the deferral trail deliberate and carries
any standing constraint that must not be re-filed.

- **Naming Sub-Agents** — CLOSED 2026-08-09, **shipped rather than deferred**
  (`docs/plans/2026-08-09 - 00 - subagent-naming-and-newline-key-plan.md` items 1–4): the `sub_agent`
  tool takes an optional `name`, and it shows in the session chat on the collapsed run header, the
  status line (`<name> · <phrase>`), both prompt panes and a headless run's records. **Standing:**
  the name is display identity only, never privilege (ADR 0005), it stays OPTIONAL with the task's
  first line as the fallback everywhere, and the `⤷ sub-agent` rail label is deliberately not part
  of it — do not re-file either as a gap.
- **VS Code names agent CLIs from an allowlist — get `apogee` onto it** — CLOSED 2026-08-03, **moot
  rather than declined** (`docs/plans/2026-08-03 - 01 - session-name-on-the-top-rule-plan.md`
  item 1): apogee no longer sets a terminal title at all, so there is nothing an allowlist entry
  could surface. The session's name rides the `▔` top rule instead, on a row apogee paints itself.
  **Standing:** the terminal's own title bar and tab are a closed route, not a deferred one — do not
  re-file an escape sequence, a settings recipe or an upstream PR for them.
- **Mid-string (non-trailing) token completion** — CLOSED 2026-07-28, **shipped rather than
  deferred** ([ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md) decision 5;
  `docs/plans/archived/2026-07-28 - 03 - slash-skills-inline-plan.md` item 6). The deferral's own rationale
  — that the trailing-token rule bought a "cursor-position-free, robust" design — was the defect:
  it is why a draft already in the box had no `/` namespace at all. All three regions (`/`, `@`,
  `/skill <partial>`) now complete the token at the caret. **Standing:** the caret walk is
  `promptEditor.seatCaret` (a `CursorEnd`-then-`CursorDown` step over LOGICAL rows) — bubbles'
  phantom trailing sub-line makes a naive `CursorDown` loop stall or spin, so do not "simplify" it
  back; the remaining mouse-path residue is its own entry above.
- **Read/list tool-name detection** — CLOSED 2026-07-19 (spelling families: post-v1.3.0
  review-fixes item 11/F8; shared `internal/mechanisms/historyscan.go` scan shapes + token
  arithmetic: architecture-deepening items 6–7/D4–D5). The broader shared-detection framework
  stays declined as speculative. **Standing:** `syntax`/`autofix` keep the narrower sim-only
  `isWriteTool` set, and search/exec tool spellings stay out of scope — do not fold in.
- **General system-prompt / template story** — CLOSED 2026-07-26
  ([ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md);
  `docs/plans/archived/2026-07-26 - 02 - configurable-system-prompt-plan.md`). The accepted
  marker-phrase-suppression interaction is recorded in ADR 0023's Consequences and is now its own
  entry above; the native byte-identical anchor (`TestPromptSeam_NativeProfileByteIdentical`) still
  holds. The host-override residual is now its own entry above.
- **Auto-mode confinement degradation is silent** — CLOSED 2026-07-21 (ADR 0012 amendment;
  `docs/plans/archived/auto-confinement-degradation-plan.md`): capability-aware startup notice,
  `/confine`, the host-scoped `unconfined-hosts:` acknowledgement, the comment-preserving config
  writer. `apogee probe` closed it further 2026-07-22 (ADR 0021). **Standing:** never loosen
  `resolveLadderAuto` — the user's decision must stay reachable, the tool never decides. The one
  live residue is the *startup notices are stderr-only* entry above.
- **Validated-set twin ladders** — DONE 2026-07-22 (one shared `startupSetDecision`,
  `cmd/apogee/validatedsets.go`; phase5-second-review-fixes item 6; ADR 0016 §5, ADR 0021 §4).
- **Windows disk-label walk kept full-tree + progress notice** — DECIDED and SHIPPED 2026-07-23
  (`docs/plans/archived/2026-07-23 - 00 - windows-label-walk-progress-notice-plan.md`; the ~1 ms
  per-object measurement lives there). Pruning the walk stays rejected — it would dissolve the
  fence for excluded trees; the related cache/temp question is the live *box-local `%TEMP%`*
  entry above.
- **`internal/platform` Windows confinement file split** — CLOSED 2026-07-25
  (`docs/plans/archived/2026-07-25 - 02 - windows-label-module-plan.md`): the label mechanism is
  the leaf module `internal/platform/winlabel`; `winguard.go` (was `winconfine.go`) keeps the
  host's path rules. **Standing (do not re-file):** `confiner_windows_test.go` (1377 lines),
  `host.go` (434) and `winlabel/journal_test.go` (433) are over the ~400-line guideline **by
  decision** (plan D7 + owner calls); if `host.go` is ever picked up it wants its own entry.
