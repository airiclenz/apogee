# ISSUES — open defects and parked work

## Conventions

The single register of known issues and deliberately deferred work (it absorbed `TODO.md` on
2026-08-13). Two sections:

- **Open defects** — verified, unfixed problems, each with current file:line evidence.
- **Parked / deferred work** — work deferred by decision, each entry recording *enough* design
  that it is not re-derived when picked up. A deferral or a denial recorded here never becomes a
  silent drop.

This file holds OPEN work only. A resolved or executed item is REMOVED from it and recorded in
`CHANGELOG.md` (under `[Unreleased]` until a release is cut) — the changelog is the closed trail;
no closed-entries section, and no "done" narration, lives here. When a run leaves residuals, record
only the still-open, ACTIONABLE findings — a defect, or a concrete missing test or doc with
`file:line` evidence to act on. Narration of how an item's text and its landed change differ,
costs a plan already ratified, and cosmetic observations belong in the run's closing report (the
closeout commit message), never here; the work the run completed belongs in `CHANGELOG.md`.

[ ] New/Open Items not handled yet
[P] Planned Items - if you add an item to an implementation plan, mark it with `P`

## Open defects

- [ ] an *approved* out-of-workspace write still errors at Execute — the confinement contract's §4
  "WS-write, target out of workspace → gate" row is now half-landed: dispatch classifies the target
  with `resolveTargetUnbounded` (`internal/tools/workspace_scoped.go:176`), so an out-of-workspace
  write reaches the approval Gate instead of being pre-rejected, but the write tool never learned to
  honour that approval — `internal/tools/write_file.go:88` writes through the os.Root fence pinned
  at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the escape
  regardless of the verdict, so the human approves and then gets an error result. Contract §4 says
  the same thing in its "Realisation gap — half-landed" note: the row is no longer unreachable, and
  the `Execute` half is the part still open. Decision pending, and it is an owner call either way:
  land the P3.7 reconciliation the contract promises (resolve against
  `WorkspaceRoot ∪ box.WritablePaths` and honour a dispatch-approved target) or ratify strict
  fencing as the permanent answer and amend §4 to say the Gate's allow is advisory for writes.
  Surfaced by the 2026-08-10 doc-landscape audit
  (`docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md`, Flag 1). Cross-reference: the parked
  *Configurable tool × mode security matrix* entry below sits on the same
  dispatch-gate-vs-tool-level-fence seam; whichever way this call goes constrains that design.

### Run residuals — open (2026-08-13)

The still-open findings the 2026-08-13 plan runs left, merged into one section under the
conventions' actionability bar (the closed and accepted remainder of every run is in
`CHANGELOG.md`); each bullet names its origin run.

- [ ] The Windows home (`%USERPROFILE%` / drive-letter `/users/`) is still unspelled in the
  ssh-key and credential patterns; ADR 0020 already reasons about that port, so it is declared
  debt. (hostile-bytes run)

- [ ] `/server` back onto a configured startup entry resolves that entry's own source, not the
  `APOGEE_API_KEY` overlay (pre-existing; ADR 0036 decision 6). (api-key sources run)

- [ ] A cancel landing inside the 1s hold-off (`restreamHoldoff` / `holdOffRestream`,
  `internal/agent/loop.go:315`, `:320`, taken at `:382`) now ends the Turn `endAbandoned`
  (ErrorEvent, deferred queue cleared) where a cancel 100ms earlier gives the resumable
  `endCancelled` (`internal/agent/turn.go:60`, `:61`) — plan-ratified, narrow window, but a real
  cancel-semantics seam worth tracking. (in-band retry run)

- [ ] `verifiedEntrySplice`'s refusal message still says "did not put the key source on the %q entry"
  (`internal/config/configwrite_keysource.go:280`), now reachable from a model / launch-profile write.
  (remember-model run)

- [ ] `/model <id>` naming the already-bound model returns early and records nothing
  (`internal/tui/picker.go:695`), so a user cannot pin the model the heartbeat put them on — faithful
  to the `/server` twin, a feature gap not a defect. (remember-model run)

- [ ] `auto-title` has no case in `applySettingFor` (`cmd/apogee/wire_settings.go:495`) — committing
  it in `/settings` writes the file and answers that it cannot be applied to the running session
  while `tui.Options.AutoTitle` stays launch-frozen; pre-existing. (remember-model run)

- [ ] `flattenLine`'s widened `\t` / `\r` branches are unreachable through any door today — the
  bubbles runeutil sanitizer maps both before the fold sees them (`internal/tui/lineeditor.go:171`,
  `:184`) — so no test exercises the fold itself, only its end state; a direct `lineBreaks.Replace`
  unit test would pin it. (residuals sweep run)

- [ ] `flattenField` folds `\n` and `\t` but not `\r` (`internal/tui/transcript.go:1522`, `:1532`);
  the display seam it guards takes model bytes no sanitizer touches. (residuals sweep run)

- [ ] `internal/tools/diagnostics_test.go` keeps 15 further raw `t.TempDir()` roots (e.g. `:318`,
  `:382`, `:473`) — green today, the same symlinked-TMPDIR hazard if any gains a bare-sentence
  assertion. (residuals sweep run)

- [ ] `internal/config/configwrite_scalar.go` lands at 803 lines, still double the coding-standards
  ~400-line guide the now-removed ISSUES entry cited; the pure-move split relocated that debt rather
  than closing it, and nothing tracks it now. (configwrite split run)

- [ ] The configwrite split left shared plumbing outside `configsplice.go`: `appendBlock` stays in
  `internal/config/configwrite.go:241` though `configwrite_scalar.go:397` and `configmigrate.go:344`
  call it, and `listValue` / `lineCount` sit in `configwrite_keysource.go:328`, `:330` while their
  only callers are the scalar writer's (`configwrite_scalar.go:219`, `:485`, `:800`).
  (configwrite split run)

- [ ] The configwrite split left prose pointing across files: `configwrite_keysource.go:22`'s carried
  banner still reads "the same contract the two writers above are" (a self-reference that no longer
  resolves in its new file), and `configwrite.go:273`, `:319`, `:404` ("Each writer above", "the
  writers above's contract", "the verification below") plus `configwrite_scalar.go:19` ("the
  acknowledgement writer above") now name text in other files. (configwrite split run)

- [ ] The input-width mirror matches the widget's sanitizer on tabs only: `expandInputTabs`
  (`internal/tui/inputaccent.go:286`) expands `\t`, while `runeutil.NewSanitizer`'s defaults also
  fold `\r`/`\n` and drop RuneError and other control runes. Unreachable by construction on every
  in-package path — the widget's own value cannot carry them, the argument `cellToRuneOffset`
  records at `internal/tui/mouse.go:262` for the accent overlay's column math — and tracked nowhere
  today. (small-guards run)

## Parked / deferred work

Live, deliberately deferred work only. Each entry records *enough* design that we don't re-derive
it when we pick it up. When an entry closes, its body leaves this file for the authoritative record
(plan / ADR / CHANGELOG) and the close is noted in `CHANGELOG.md` — so a deferral trail never
becomes a silent drop, and the file never becomes an archive.

### apogee-code feature parity — user-facing affordances not yet ported

**Status:** parked 2026-06-25; most of the surface has since shipped (`CHANGELOG.md` and the
archived plans hold that record). Additive TUI/UX layers on top of the agent core, which is
already at parity. Scope is
*user-facing* parity with the apogee-code VS Code extension (`airic-lenz.apogee-code` v0.2.58)
only — the by-design Phase-4 items were tracked separately.

**Verification note (the source-of-truth correction):** apogee-code's `Apogee-Code-TDD.md`
claims it has *no slash commands, only `@file`*. **That doc is stale.** When porting, treat the
shipped webview (`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js`, array `Ws`)
as the behavioral oracle, not the TDD. On send the webview posts `{text, skillIds, fileRefs}`.

**Remaining:**

- **[P1] Server / model switching** — **every switch SHIPPED (2026-07-28 the two user-facing ones,
  2026-07-29 the local-server half, 2026-08-12 the profile half); only two request-side knobs
  remain.** The shipped bodies have
  left this file for their authoritative records
  ([ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)
  for `/model` + `/server`,
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
  for the launcher,
  [ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md) for the profile).
  **Remaining:**
  - The **request-side knobs the Model profile still does not carry** — sampling params and the
    context-budget %. The profile itself is per-model and switchable now (a `model-profiles:`
    pattern map resolved user-entry ▸ shipped table ▸ zero, riding every rebind — ADR 0044), but
    its two axes are wire dialect only: tool-call format and thinking channel. **Partially answered
    elsewhere (2026-08-13):** the `MaxTokens` axis shipped with ADR 0046 as a per-`servers:`-entry
    pin (`max-output-tokens:` / `context-window:`, riding every rebind) — engine behaviour, NOT a
    Model-profile field. Still open here: the other sampling params (ADR 0046 deliberately leaves
    temperature et al. unset) and the context-budget % (`ResponseReserve` is code-only,
    `internal/context/budget.go:53`). Distinct from the
    launcher's **Launch profiles**, which are **launch-side** (model file, server flags); these
    knobs are **request-side** — the grill they need must keep the two "profile" namespaces from
    colliding in the UX. Its one prerequisite is discharged: the field-wise `SetSampling` merge
    landed 2026-08-13, so a profile that sets only temperature no longer nils the engine's
    stamped cap.
    [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md) §6 still
    defers the user-after-tool wire risk to this layer by name.

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
  `caretToOffset` and `reseatInput` express was fixed (`seatCaret` — the caret family now lives in
  `internal/tui/lineeditor.go`, `reseatCaret` at `:212`, `seatCaret` at `:246`;
  [ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md)). The fix wants the
  same Height-aware step expressed for a visual target.

**Related (parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

**Standing rules the shipped work left behind:** the `model-profiles:` shape table grows one
entry per *sighting*, never per guess ([ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md)).
What has shipped out of this entry since it was parked is recorded in `CHANGELOG.md` and the
archived plans and ADRs it names — only the open remainder above lives here.

---

### Session system follow-ons — deliberately deferred from the 2026-07-24 session-system plan

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

### Phase-4 mechanism catalogue — deliberately dropped / folded / deferred

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

### Configurable tool × mode security matrix

**Status:** parked 2026-06-24 (Phase-3 grill). Post-v1, **additive** — config is additive,
so this is a minor bump, not a freeze break. Cross-reference: the open defect *an approved
out-of-workspace write still errors at Execute* (Open defects above) sits on the same
dispatch-gate-vs-tool-level-fence seam; its pending owner call constrains this design.

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

### Dedicated url-safety config key for the network tools

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
plan's:** `HostTools` is composed in two places (`internal/agent/construct.go:402` and
`cmd/apogee/wire_tools.go:158` — the latter moved out of `wire.go` in the ADR 0043 file split —
the engine-side one unexported), so a new `HostTools` field must be added in
**both** or it is silently dropped on one path. That duplication is a deferred
`/improve-codebase-architecture` candidate, not a blocker.

---

### An embedder cannot register a *vouched-for* network tool (export the network funnel?)

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

### A door left open by the Mechanism-registration collapse

**Status:** parked 2026-07-25 (`docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`,
"Explicit non-goals" + D4; [ADR 0003](docs/adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)
amendment 2026-07-25). It was named out of scope by that plan and recorded here so the door is
documented rather than silently shut. **It is not a live gap.**

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

### `Request.InjectContext` placement — a `domain` data type encodes chat-template role-safety policy

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

**What the code does today** (`internal/domain/hooks.go:504–529`) — recorded so the grill starts from
behaviour rather than re-reading it. `InjectContext(text)` picks the landing spot with a three-branch
ladder:

1. history ends in a **tool result** → append to the system prompt instead (a user message after a
   tool result breaks strict chat templates);
2. history ends in an **assistant** message → append at the end (the retry-exchange shape: the
   correction answers the superseded assistant message it follows, R1);
3. otherwise → insert **before** the last Exchange's opening (`lastExchangeOpening`,
   interjection-aware — drifted since this entry was written, which recorded "before the last user
   message"; re-verified 2026-08-13) — plus the F2 boundary maintenance, since an
   insert below the frozen `committedLen` shifts every committed message right by one and the
   boundary must advance with it;

with no user message present at all, it appends at the end. Sibling for contrast: `AppendToSystem`
(same file, `:481`) is the marker-idempotent system-prompt inject; `InjectContext` carries **no**
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
  hook mutation API (`docs/design/archived/hook-mutation-api.md` §3, the `Request` pre-request surface, and
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
`internal/agent/loop.go:369` (the retry correction) and `:699` (the deferred-correction drain), and
the Mechanisms `readloop.go:89`, `filehint.go:111`, `guideddecomposition.go:159`. Several Mechanisms
additionally *reason about* where the inject lands — `guideddecomposition.go` (`:175`, `:196`,
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

### Deferred security-review Lows (P3 `/security-review`, 2026-06-24)

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

- **[L5 accepted cost, 2026-08-12] The exec fence refuses an in-repo virtualenv and
  `node_modules/.bin`, with no way to switch it off.** Every exec site now refuses an `argv[0]`
  resolving inside the workspace or a configured writable path
  (`security.RefuseExecFromWritablePath`, hostile-bytes plan item 2 — it closes the
  plant-then-exec chain: a confined call writes an executable inside its box, a later unconfined
  call runs it outside). `lookInterpreter` and `lookTestProgram` resolve against apogee's own
  inherited `PATH`, so the two most common Python and Node layouts — an activated
  `<repo>/.venv/bin/python3`, a `node_modules/.bin` entry ahead of the system ones — land inside
  the root and start refusing. That is the correct security answer and it is also a real
  regression for real developers. **Accepted as-is** (owner ratification, 2026-08-12): no config
  opt-in and no allow-list carve-out for the conventional tool directories, because a key here
  would be a new operator-armed footgun and a carve-out would name exactly the directories an
  attacker plants in. The refusal is made legible instead — it names the resolved path and the
  rule, so the venv is visibly the cause. Two alternatives were considered and rejected: keying
  the refusal on "resolved via `PATH` rather than an absolute config value" (a venv interpreter
  *is* found via `PATH`) and "was this file written during the session" (stateful and racy). If
  the cost proves unacceptable in practice, a config key is a small later addition.

- **[L6 accepted cost, 2026-08-12] A process a `terminal` call deliberately backgrounded is reaped
  when the call returns.** Process-group teardown used to run on cancellation only — `cmd.Cancel`
  was the single group kill — so a command that backgrounded a server left it running after a clean
  exit, and because `exec.ErrWaitDelay` is not an `*exec.ExitError` a wedged post-exit drain was
  reported as exit code 0. The teardown now also runs after a normal `Wait`, and an expired
  `WaitDelay` is surfaced on the result (hostile-bytes plan item 18). **Accepted as-is:** this is
  what the tool's own contract already promised — "one-shot, a fresh process per call, no persistent
  shell" (ADR 0008) — so it closes a gap between contract and code rather than choosing a new
  policy, and a descendant outliving its call is exactly the persistence primitive the audit named.
  The cost is that anyone who was relying on `terminal` to leave a dev server up now finds it gone,
  with no opt-out: a long-running process belongs outside the one-shot tool.

(L2 — the dangerous-action guard normalising only whitespace+case, trivially evadable — needs no
entry: it is ADR-0012 by-design, and `internal/security/doc.go` already states the guard is "NOT
a security boundary." No doc/UI describes it as one, which is exactly what L2 asks for.)

**Triage note, 2026-08-11 — the external security audit was checked against L2/L3/L4 before any
finding was accepted.** The audit's own strongest observation is that a deferred acceptance here is
the single biggest predictor of a finding being answered "intended", so every one of its 14 ranked
positions was tested against these three first. **None of the fourteen dies on them**, and the
reasons are worth recording so the acceptances are not re-argued at the next audit
(`docs/reviews/2026-08-11 - 01 - external-audit-triage.md`; fixes in
`docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md`):

- **L2 does not dismiss the approval-pane findings** (newline-forged rows, head-only truncation and
  wire-order duplicate keys, the unshown resolved path, surviving bidi overrides — plan items 6, 7,
  8, 17). L2 is about the *guard* being evadable; these are about the *disclosure* surface — what
  the human is shown before they authorise — on which L2 says nothing. The one floor change in the
  batch (item 14, adding `.git/` and `~/.apogee`) tightens `DefaultDangerousRules` only and leaves
  `MergeDangerousRules` semantics alone, so L1 and L2 stand exactly as written.
- **L3 does not dismiss the unbounded-execution findings** (plan items 2, 3, 5, 18). L3 accepts
  read-and-exfiltrate *from inside the box*; these are code running **outside** it (an `argv[0]` the
  model planted inside the writable box, executed later unconfined), code the operator never saw
  running inside it (the workspace shadowing the Python stdlib), a process spawned with no approval
  and a nil box (the bare-name opener exec), and a process outliving the call that spawned it. What
  they violate is ADR 0012's invariant — a call runs ungated only if its blast radius is bounded —
  which L3 does not weaken.
- **L4 does not dismiss either environment finding.** L4 is specifically about a *configured stdio
  MCP server* inheriting the full environment, a trusted host-configured launch. It does not cover
  plan item 3's much narrower change (dropping apogee's **own** credentials — `APOGEE_API_KEY` and
  any configured server key — from what `python_exec` and `terminal` inherit), which is not the
  blanket allowlist L4 deferred; nor plan item 19, where the defect is that the Go toolchain was
  handed *git's* allowlist (`safeGitEnv`), stripping the operator's own Go hardening and putting
  nothing back.

The audit findings these acceptances **did** dismiss are recorded in the triage's exclusion buckets
rather than here, together with the operator-armed footguns, the attacks on apogee's own build and
release, and the hostile-inference-endpoint set — all different threat models from the hostile-bytes
one the batch answers.

**Closeout, 2026-08-12 — what the batch deliberately left** (what it closed is in `CHANGELOG.md`).
**Left, deliberately:** everything in the plan's **Out of scope**
section — operator-armed footguns (`present.command`, `--workspace /`, `APOGEE_MODE=auto`, the
stdio MCP command surface), attacks presupposing the audited workspace *is* the apogee repo, the
hostile-inference-endpoint set, the gate-reason wording (owned by
`docs/plans/archived/2026-08-11 - 03 - subprocess-gate-reason-plan.md`), and the human-timing attacks on the
gate. **Two residuals sit inside items that did land**, documented at the code rather than fixed: a
`.gitattributes` clean/smudge **filter** driver takes its command from the repository's own
`.git/config` and git offers no switch that refuses configured filters, so only the read-path
textconv/ext-diff half is closed (`gitHardeningEnv`'s doc comment); and `GOENV=off` means the
operator's persisted `GOPROXY`/`GOPRIVATE`/`GOMODCACHE` are unread, so a cold-cache `go vet` can
fail to resolve dependencies — which degrades to a reported finding, not a tool error (`goVetEnv`'s
doc comment). L1–L4 above are untouched by the batch; L5 and L6 are its two new accepted costs.

---

### Mid-Exchange auto-compaction (fire at Turn boundaries under budget pressure)

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

### Startup notices are stderr-only — the in-transcript banner

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

### A presented document carries no sub-agent depth (it breaks the run's rail)

**Status:** parked 2026-07-21 (noticed while verifying the `present_document` plan,
`docs/plans/archived/2026-07-21 - 01 - present-document-tool-plan.md`). Cosmetic, no wrong output.

`domain.PresentRequest` carries no sub-agent depth, so `internal/tui`'s presentation entry is
always rendered at depth 0 — unrailed even when a sub-agent presented the document. A depth-0
block sitting inside a sub-agent run breaks the rail framing it: the rail stops above the
presentation and picks up again below it, with neither a `┊` closing the first stretch nor a
`┌─┶` header opening the second, so one run reads as two railed stretches with an unframed gap
between them. Not presentation-specific: any depth-0 entry between two nested blocks does the
same (a `· cancelled` note already can). The fix is to carry the Step's depth on `PresentRequest` and
render the entry at it, which is a domain-seam change and wants its own decision — the loop's
depth is not currently exposed to a host delegate at all (`domain.AskRequest` has the same gap:
ADR 0039 gave it `SubAgentTask` and `SubAgentName`, so a delegate now learns *which* child is
asking, but still not *how deep* it sits).

---

### Adaptive prompt complexity — request slimming driven by the capability tier

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

### Host-override knob for the rendered instruction block

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

### A marker phrase in the standing system content suppresses that Mechanism's directive

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

### Phase-5 verification leftovers — the owner-run passes this machine cannot perform

**Status:** carried forward 2026-07-22 from the "Owner-run checklist" of the archived
[`docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`](docs/plans/archived/2026-07-22%20-%2000%20-%20phase5-cross-platform-hardening-plan.md)
(read it for full context). What the checklist's runnable passes covered is in `CHANGELOG.md` and
the archived plans; what remains open needs hardware this environment does not have:

- **Live Auto-confined deliverable run on Windows** — the ADR 0020 backend is proven natively
  (escape battery + the real `Terminal` tool under `platform.NewConfiner()`); an end-to-end
  deliverable session under Auto is what remains, if an LLM endpoint is reachable from that
  machine.
- **Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke** — same shape; the
  darwin binary cross-builds clean (amd64+arm64, re-verified 2026-07-23), so what remains on a
  Mac is `--help`, a trivial session, and the confined Auto run. That run should also probe the
  `/dev/null` seatbelt exemption live — `internal/platform/seatbelt_darwin_test.go` only
  delegates to the shared `confinetest.Probe` battery, so the exemption is pinned by the hermetic
  profile-string tests alone (folded here from the /dev/null confinement run's residuals,
  2026-08-13).
- **Degradation notice on a below-floor Windows host (< build 17763)** — the deny-vs-token
  decision itself is table-proven (`TestBelowWindowsFloor` in the untagged `winguard.go`
  predicate); only the on-host UX observation of the notice stays untested (recorded so in
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md)'s
  consequences), verifiable only if such a host turns up.

**Not repeated here:** the "pre-existing, NOT Phase 5 scope" group (Linux/macOS live-run
variants already tracked elsewhere) lives in the CHANGELOG's **"Known post-release verification
(owner-run / CI)"** note — that note, not this file, is its tracking home.

---

### Windows Auto: box-local `%TEMP%` / toolchain caches

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
`domain.Config.ConfineWritablePaths`, has only **readers** —
`internal/agent/dispatch.go:417` and `:457`, which copy it into
`domain.ConfinementBox.WritablePaths` for a tool call and for a hook-time subprocess respectively —
and, repo-wide, **no writer**: nothing probes for toolchain caches and nothing surfaces the field in
config, so it is always empty. Contract §7's own recommendation ("seed `WritablePaths` with the
detected toolchain cache + temp dirs by default, probed, not hard-coded") was never implemented on
any platform; on Windows it is the difference between a usable fence and an unusable one.
`internal/platform`'s `ScopeEnv` (the environment-scoping seam ADR 0020 §2 points at) exists and is
used by `git` and the Go toolchain, but `terminal` and `python_exec` inherit the user's `%TEMP%`
unchanged.

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

### The TUI width authority — what it did not convert

**Status:** parked 2026-07-31, the residue of the *width authority* plan (`2026-07-31 - 03`, under
`docs/plans/`, archived on completion) —
[ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md). Nothing
here breaks the absolute width cap; each is a place the package still measures in a measure the
painter may not be using, or mirrors a widget imperfectly. What is open here is the one width entry
below: `hangingPrefixes` at block width 1–2. The rest of the residue is closed — see `CHANGELOG.md`
for what landed.

**Standing rules the closed work left behind** (they bind future work, so they are not in the
changelog alone): the one `lipgloss.Style.Width` still in the package — the prompt box framing a
widget that wraps in GraphemeWidth itself — is ADR 0030 §6's widget-mirror exception, so do not
re-file it, and never put a `Width` style on a *bordered* surface (§5). `inputContentRows`' taller
counts need no clamping change: `promptEditor.rows` and `layout()` already clamp
(`TestPromptEditorRowsClampsTheWidgetCount`).

**`hangingPrefixes` can draw three cells at block width 1–2** (now `internal/tui/wrap.go:31`). It floors its wrap
width at 1 column and then prepends a two-column marker, so a bullet list in a two-column block
produces three-cell lines. Pre-existing and untouched by the width work; a real fix has to decide
what a marker means when the block cannot hold it, which is a `layout.md` question, not a
measurement one.

---

### A model-facing `schedule` tool — daemon-era, not v1

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
at that layer; the TUI still never has to register it. The daemon's shape has since been settled —
[ADR 0034](docs/adr/0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md),
an in-repo subcommand over a declarative trigger-action file — so the trigger is now a dated record
to build against rather than an open question, but the daemon itself is unbuilt and this entry stays
parked until it exists. **Not the vehicle:** MCP — schedules are
in-process driver state, Firings run without MCP, and ADR 0031 forbids first-party connectors.

---

### The published binaries are not code-signed

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

### Tool-surface findings (4-poll round, 2026-08-10)

**Status:** recorded 2026-08-10 at the close of the tool-surface plan (`2026-08-10 - 00`, under
`docs/plans/`, archived on completion). Four polls of the target class — Qwen3.6-35B-A3B ×2,
Gemma-4-26B-A4B, Gemma-4-E4B — were asked what they wanted from apogee's tool surface. The plan
shipped the uncontested improvements and the promoted new tools (`find_files`, `git_status`,
`git_log`, `copy_file`/`move_file`/`delete_file`, `run_tests`) plus the global `tools.disabled`
switch; everything the round raised that did **not** ship is recorded here, so a deferral or a
denial never becomes a silent drop. **None of it is a live gap.**

**Bench experiments required before any tool removal.** Models are unreliable narrators about their
own tooling: the E4B poll preferred patch-only editing — the format small models are measurably
worst at — and a repeat Qwen poll returned a substantially different list, so only REPLICATED
findings count. Nothing leaves the roster on poll evidence alone; each arm below is a bench
experiment, not a decision:

- **(a)** remove `single_find_and_replace` — flagged in all four polls.
- **(b)** patch-only vs find-replace editing — Qwen vs both Gemmas, a falsifiable disagreement.
- **(c)** `open_file`/`read_file` merge — **open watch-item, not an experiment arm.** This arm was
  decided by owner call on 2026-08-11 and shipped without the bench experiment the standing rule
  otherwise requires (an owner-ratified exception for this arm alone; the rule still binds (a),
  (b), (d), (e) and (f) — see `CHANGELOG.md` and
  `docs/plans/archived/2026-08-11 - 03 - open-file-read-file-merge-plan.md` for what landed). What
  stays open is what the skipped experiment would have measured: whether sub-35B models find
  `read_file`'s `locate` *parameter* as readily as they found the `open_file` *name*. Method
  lesson 2 below says they may not; `read_file`'s description advertises locate by name to hedge
  it, and a sighting of models no longer locating reopens this arm rather than re-filing it as a
  gap.
- **(d)** measure whether sub-35B models use `view_diff` at all.
- **(e)** `web_fetch` → `http_request` merge — the real question is whether sub-35B models
  distinguish GET from POST; if they don't, the separate named GET tool earns its slot. Both are
  ExternalEffect-classified, so gating doesn't decide it.
- **(f)** do sub-35B models ever discover `edit_existing_file`'s patch mode unprompted? — a
  discovery experiment feeding the explicit-patch-param idea.

**Needs a grill session:** per-profile tool rosters (builds on the `tools.disabled` key this plan
shipped); a unified `git` tool with subcommands vs the growing `git_*` family.

**Deferred candidates:** env-var parameters on `terminal`/`python_exec` (stable across both Qwen
sessions); `directory_create`/`directory_delete`; `git_stash`; `git_tag`; `file_metadata`; batch
rename/replace operations; `workspace_summary`.

**Engine-level notes (not tools):** context-window introspection for the model (Mechanism
territory); streaming/progress for long-running tools; structured JSON tool outputs.

**Denied, with reasons** (do not re-file as gaps):

- `database_query` — [ADR 0031](docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md):
  no first-party connectors, that is MCP's job.
- standalone `apply_patch` — it already exists inside `edit_existing_file`; models missing it is a
  *discovery* problem, tracked as the explicit-patch-param idea in (f) above.
- concurrent `terminal` — parallelism lands at the sub-agent layer
  ([ADR 0039](docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)).
- `inspect_environment` — `terminal` covers it.

**Method lessons** (they generalise past this round):

1. Models reliably converge on **problems** but not on **solutions** — removals need measurement.
2. Models discover capabilities by tool **name**, not by parameters — three sightings in one round:
   `list_dir`'s `recursive` missed twice, `edit_existing_file`'s patch mode missed once. Naming and
   descriptions are the discovery surface.

---

### A delegation that never ran shows no prompt when expanded

**Status:** parked 2026-08-11 — flagged while executing the tool-printout-fixes plan
(`docs/plans/archived/2026-08-11 - 04 - tool-printout-fixes-plan.md`, item 7's NOTES) and deferred by owner
call the same day. **Pre-existing** — that plan neither introduced it nor changed it. Not a defect to
fix on sight: what a never-run delegation should show is an open design call.

A delegation that is over and left nothing behind it — a child refused at the depth bound, one that
failed a hook before its first event — is drawn as an ordinary tool block rather than as a run.
`subAgentFramed` (`internal/tui/subagentblock.go`) frames only a delegation with a span behind it or
one that is still open and expanded, and the initial prompt is painted *inside* that frame
(`subAgentPromptRows`), so expanding such a delegation shows what the ordinary tool block shows and
never the prompt the delegation carried. The rule is one rule for both rendering paths — the lone run
(`renderView`, `internal/tui/render.go`) and the grouped member (`renderSubAgentGroup`) — so the
behaviour is identical in each and there is nothing path-specific to fix.

**The framing itself is not the thing to reopen:** a frame opened over an empty span would enclose
nothing and be closed again by the very next row, which is exactly why `subAgentFramed` is written the
way it is (its doc comment carries the reasoning). The open question is whether "what was asked" is
worth showing for work that never ran and, if it is, where it goes on an *unframed* block — the
task's first line already rides the header as the name fallback, so a second rendering of the prompt
beneath it has to earn its rows. Settle that before touching either path.
