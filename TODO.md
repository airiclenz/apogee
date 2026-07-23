# TODO — parked ideas (not in Phase 3 / not in `v1.0.0`)

Ideas worth doing later, deliberately deferred so they don't bloat the v1 freeze.
Each entry records *enough* design that we don't re-derive it when we pick it up.

---

## apogee-code feature parity — user-facing affordances not yet ported

**Status:** parked 2026-06-25. Post-v1, **additive** (all TUI/UX layers on top of the agent
core, which is already at parity). These are features the original **apogee-code** VS Code
extension (`airic-lenz.apogee-code` v0.2.58) ships that the Go TUI does not. Scope here is
*user-facing* parity only — the by-design Phase-4 items (Mechanisms catalogue, cross-session
Library, context-budget gauge) are tracked separately and excluded.

**Verification note (the source-of-truth correction):** apogee-code's `Apogee-Code-TDD.md`
claims it has *no slash commands, only `@file`*. **That doc is stale.** The shipped webview
(`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js`, array `Ws`) actually
implements a full chat mini-language. When porting, treat `media/chat.js` as the behavioral
oracle, not the TDD. On send the webview posts `{text, skillIds, fileRefs}`; the backend
resolves skill bodies + file contents into context.

**The missing surface, prioritized:**

- **[P0] Chat input mini-language** — a parse layer between the input box and the agent.
  **CORE SHIPPED 2026-06-26** (handoff `docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md`):
  the pure parser/router (`internal/tui/command.go`), the autocomplete overlay for `/`-commands
  **and** `@`-files (`internal/tui/autocomplete.go`, bounded `os.Root` workspace walk), `/clear`
  (→ `Agent.ClearContext()`), `/continue` (→ "Please continue"), a **stubbed** `/compact`
  (→ `Agent.Compact()` returning `ErrCompactionNotImplemented`), and the real agent-side
  `@file` resolver (`loop.go resolveFileRefs`, reusing `security.SafeReadFile`; replaced
  `noteUnresolvedFileRefs`). `domain.UserInput.SkillIDs` is pre-wired (reserved, unresolved).
  **Remaining:**
  - `/compact` real reducer — **SHIPPED 2026-07-01.** The generative Compaction summarizer lives
    in `internal/context` (`Compact`): it summarizes the conversation through one silent upstream
    call and writes the summary back via `Conversation.Replace`, keeping the protected prefix
    (`PrefixEnd`) verbatim. `Agent.Compact` drives it (quiescent-boundary guarded like
    `ClearContext`), and the TUI runs it on a worker goroutine (spinner + `Esc`-cancel + gauge
    reset). Wired as the built-in **default reducer** invoked directly — NOT through
    `runHistoryRewriteHooks` (that stays the experimental-hook / `truncate_history` path, per
    `internal/context/doc.go`). The *automatic* budget-driven trigger — **SHIPPED 2026-07-04
    (Phase-4 item 9):** the loop folds the conversation (the same `Compact`) at a quiescent
    boundary when `internal/context.HistoryExceedsAllocation` reports the history has outgrown its
    Budget allocation (`Agent.autoCompact`, called before the Turn consumes new input). It is
    structural — on by default, on even under Bypass (D5/D6) — with a file-only `auto-compact:
    false` opt-out (`ContextConfig.CompactionEnabled`); the on-demand `/compact` is unaffected by
    the gate. Built on Phase-4 item 8's Budget allocator + usage-calibrated token accounting.
  - `/skill <name>` — **SHIPPED 2026-06-26** with the Skills system below: the "/" menu offers
    `/skill`, which chains into a skill picker (`acSkill` dropdown); a pick pops a chip onto
    `Model.pendingSkills`, and submit copies it into `UserInput.SkillIDs`. The loop
    (`loop.go resolveSkillRefs`, replacing `noteUnresolvedSkillIDs`) prepends each resolved
    body to the turn. `/skill` is intentionally NOT a parser command (attachment is the only
    way it acts), so an unknown `/skill foo` stays an ordinary message.
  - `/server` (switch server) — needs a swappable provider seam (today `upstream` is immutable
    after construction). See **[P1] Server / model switching**.
  - Polish: `@`-file-listing cache **SHIPPED 2026-06-26** (`internal/tui/filecache.go`: a
    `*fileCache` on the Model memoises the workspace listing with a short TTL and filters it in
    memory, so a typing burst reuses one fenced walk instead of re-scanning the disk per
    keystroke). **Still deferred:** mid-string (non-trailing) token completion (the overlay
    completes the word at the cursor/end only) — kept deferred on purpose: it trades the
    "cursor-position-free, robust" design for cursor-tracking edge cases.

- **[P0] Skills system** (prerequisite for `/skill`) — **SHIPPED 2026-06-26**
  (`docs/plans/archived/skills-system-plan.md`). New `internal/skills` package discovers **directory +
  `SKILL.md`** skills (not flat `.md` — matches the apogee-code oracle and the Anthropic
  agent-skills convention) from the layered dirs `~/.apogee/skills/`, workspace `.apogee/skills/`,
  and workspace `skills/` (the last gated by the new file-only `use-project-skills` config key,
  default true), later source winning on id collision. YAML frontmatter (`id`|`name`,
  `displayName`, `summary`|`description`) + body, with a no-frontmatter fallback; layered through
  `os.OpenRoot` so a workspace symlink can't escape; a missing dir is skipped and a malformed skill
  is skipped with a soft error. `Catalog` satisfies `domain.SkillResolver` (loop) and the TUI's
  `SkillCatalog` (picker); `wire.go` loads it once and injects both. No builtin/embedded skills and
  no auto-created `~/.apogee/skills` in v1 (additive future hooks).
  - **Authoring guidance (2026-07-21):** a report-producing skill should end by calling
    `present_document` on the file it wrote — that is what surfaces the deliverable instead of
    leaving it behind a one-line `write_file` card ([ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md)).
    Guidance only: skills stay **user-authored** (ADR 0002), apogee ships none, and nothing in the
    `present_document` work edits a builtin skill.

- **[P1] Session management UI** — in-TUI *new session* (reset without relaunch) and a *history
  browser* overlay. Today only `--resume <path>` exists; reuse `internal/session/Store`.

- **[P1] Server / model switching** — `/server` live endpoint switch (re-probe `/v1/models`,
  rebind the `provider` seam; today fixed at startup); a switchable **model-profile** abstraction
  (sampling params, context-budget %, thinking/tool-call format — reuse `internal/processing`);
  and start/stop control for a local llama.cpp server.

- **[P2] Inspector / raw-protocol view** — apogee-code's "Show Code"/Inspector (advanced mode)
  shows wire-level request/response JSON. apogee has only a hidden, non-toggleable debug field in
  `internal/tui/transcript.go`. Add a TUI toggle behind an advanced flag.

- **[P2] Undo all agent changes** — batch revert of a session's file writes (document that
  terminal side-effects are not undone, as the extension does).

- **[P2] Throughput display** — **SHIPPED 2026-06-26.** The server's `stream_options.include_usage`
  accounting (already on every request) now rides a new `domain.UsageEvent` emitted from the loop's
  stream consumer (`agent/loop.go` on the terminal `DeltaDone`); the TUI folds the latest top-level
  (Depth 0) usage to (a) light the live context-fill gauge (`contextGauge` now reads `m.ctxUsed`
  instead of a hard-coded 0) and (b) show a rolling `· N tok/s` readout in the status line, the
  completion timed against the Update clock from the Turn's first token (`model.go foldStats` /
  `throughputSuffix`). Distinct from the excluded context-budget gauge. **Update 2026-06-28:** the
  companion ISSUES bug — the context *window* read wrong from the server (`provider/discovery.go`) —
  is now **FIXED**: `Discover` probes llama.cpp `GET /props` and prefers its runtime
  `default_generation_settings.n_ctx` over the model's training window (`n_ctx_train`), so the gauge
  measures against the correct denominator. The deferred `llamacpp-props` discovery strategy is now
  live; the `ollama-show`/`ollama-tags` strategies remain unported (additive, not needed yet).

**Related (already parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

---

## Phase-4 mechanism catalogue — deliberately dropped / folded / deferred

**Status:** recorded 2026-07-04 at the Phase-4 close-out (`v1.2.0`). The ratified catalogue
(`docs/design/mechanism-catalogue.md`, Table C + ledger) ported most apogee-sim Mechanisms but
deliberately did **not** port these. Logged here so the deferral trail stays deliberate, not a
silent drop — **none is a live gap**; each is an evidence-backed verdict that can be revisited if
the bench finds a specific win.

- **`codeinfo` — DROPPED (catalogue C7).** Broad plan §2 deprioritized it (modest effect,
  superseded by shell-out diagnostics); the sim's own A/B shows its specific missed-call-site
  signal is **not significant** (OR 0.69, p=0.32 on gpt-oss-20b, N=75/arm). Not ported to any wave.
- **`correct_tool_result` — DEFERRED, not dropped (owner-ratified 2026-07-04).** The pinned sim
  defines **no production trigger** — it is a lab-only intervention with an operator-supplied
  correction — so inventing gating would ship behaviour with no evidence (D7). The loop already
  exposes the lab surface (an experimental post-tool-result hook can replace a result via the
  mutation API), so the bench plays the operator without a catalogued Mechanism. A **bench-discovered
  trigger motivates a NEW plan item** + a fresh Table B verdict — that is the only path to porting it.
- **`compress` external-client-compaction sniffing — DROPPED (C3).** apogee owns the loop; there is
  no external client to sniff pre-compressed content from (broad plan §4). The *surviving* halves of
  `compress` shipped: `tool_result_cap` (Mechanism, item 9), generative Compaction (structural, item
  9), and `truncate_history` (Mechanism, item 7).
- **`intent` / `cot` / `feed_forward_correction` — FOLDED, not standalone Mechanisms (C4/C5/C6).**
  `intent` is a shared inline classifier (no hook/descriptor); `cot` split into the three completion
  nudges (`stall_nudge` / `list_nudge` / `tool_use_directive`, item 12); `feed_forward_correction`
  folded into `validate`'s retry-in-place `ActionRetry{Inject}` delivery (item 5, R1). No catalogue
  rows of their own.
- **Un-ported sim refinements — recorded bench-pending (R2).** The off-ramps' retry-ladder
  refinements (attempt-2 nudge ladder, system directive, temperature escalation, per-Session throttle
  counters) and `tool_loop_interceptor`'s per-Session count threshold + 30s wall-clock cooldown are
  not ported — the loop's strikes-3 self-regulation + the `maxPostResponseRetries` cap substitute, and
  wall-clock time is meaningless in the deterministic bench. Revisit only if the bench shows a specific
  refinement carries a win.

---

## Read/list tool-name detection — CLOSED (spelling families + shared-scan re-shaping both landed)

**Status:** CLOSED 2026-07-19 (architecture-deepening close-out,
`docs/plans/architecture-deepening-plan.md` items 6–7 / D4–D5; previously narrowed 2026-07-06 by
the post-v1.3.0 review-fixes close-out, `docs/plans/archived/post-v1.3.0-review-fixes-plan.md`
item 11 / F8). Both halves are done:

- **The drift half (F8, 2026-07-06):** the read trio (`read_file`/`readFile`/`open_file`) and the
  five list spellings are single-sourced as two spelling families hoisted beside
  `wave4WriteTools`; every read/list set composes from them, and the four diverged sets were
  corrected in that pass.
- **The structural re-shaping (D5, 2026-07-19):** one copy of each shared `conv.Range(...)` scan
  shape now lives in `internal/mechanisms/historyscan.go` beside the families (read-attempt path
  counting with successes/failures separate, successful-read paths over the latest read episode,
  written paths since an index); readloop, readrepeat, and filehint migrated onto them.
  Per-Mechanism membership and thresholds stay at the call sites (the F8 spirit); readloop's
  `isGreenfieldContext` deliberately stays local — a composite write/read/list early-exit scan no
  shared shape expresses (commented at the symbol). Token arithmetic concentrated alongside (D4):
  `Budget.EstimateTokens` / `Budget.HistoryExceedsAllocation` are the one chars→token
  implementation. (One deliberate outlier: the Library's context-fill backoff,
  `libraryContextTooFull`, keeps its own *continuous* usage fraction rather than calling
  `Budget.EstimateTokens` — it needs a fraction of the window, not an int estimate.)

**Deliberately NOT built (so the drop stays a verdict, not a silent one):** the broader
shared-detection-module idea — unifying the Mechanism marker machinery into a framework — was
declined as speculative by the deepening plan ("Explicitly NOT in this plan"): F1 moved fan-out
idempotency onto committed evidence, so the residual marker use is one Mechanism's same-request
guard; a shared marker store concentrates nothing real until a second Mechanism needs one.
Re-surface it at the next architecture pass if that happens.

**Divergences that must NOT be folded in (still hold):** the content-repair Mechanisms
(`syntax`, `autofix`) key on the narrower sim-only `isWriteTool` set, and search/exec tool
spellings stay out of scope.

---

## General system-prompt / template story

**Status:** parked 2026-07-02 (prompt-seam grill — `docs/plans/prompt-seam-wiring-plan.md`,
scope guard). Post-v1, **additive** (a new `Config` field + a template renderer; the
byte-identical native anchor is preserved).

**The idea:** apogee has **no built-in system prompt** — the conversation starts empty. The
prompt-seam plan ships only the **narrow** profile-driven block: the text tool menu + format-
emission instructions rendered for a non-native tool-call format. The apogee-code oracle
assembles a much larger system-prompt template *around* that block — `{{tools_block}}` plus
`{{agent_mode_directive}}` / `{{datetime}}` / `{{workspace}}` / a persona preamble
(`~/Repos/Airic/apogee-code/src/context/context-builder.ts:38-45`). Porting that general
template (a system-prompt `Config` field / template engine) is the separate, larger
feature-parity item, **explicitly out of scope** of the prompt-seam plan per its grilled scope
guard.

**Extension point noted for when it lands:** a **host-override knob** for the rendered
instruction block — D1's *rejected hybrid* in the prompt-seam plan (engine-owned won; an
override is additive later if a real embedder needs to supply or replace the block). Design it
*together with* the general template so the two compose rather than fight.

**Native byte-identical anchor (must hold when built):** a zero/native profile must still add
**zero bytes** to the wire request — the template applies only when a profile (or a future
prompt field) asks for it.

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

## Auto-mode confinement degradation is silent — CLOSED (notice + `/confine` + host acknowledgement shipped)

**Status:** CLOSED 2026-07-21 (filed, designed, and implemented the same day —
`docs/plans/auto-confinement-degradation-plan.md`, items 1–10; ADR 0012 amendment 2026-07-21).
Post-`v1.4.0`, **additive**: a startup notice, an in-place accept path, and a config write. The
resolution ladder is unchanged.

**What was wrong:** `resolveLadderAuto` sends a subprocess tool to `Confine` when the backend can
fence it and to `Gate` when it cannot — ADR 0012's *"confine if you can, gate if you can't"*. On a
host reporting `Capabilities().FSWrite == false` that is an Approval prompt on **every** terminal
command, and nothing said so, so Auto read as broken. It is the *common* case, not an edge one:
`landlock_create_ruleset` returns **`ENOSYS`** in most containers regardless of kernel version
(verified on 6.18.15, well past the 5.13 floor).

**What shipped:**

1. **The capability-aware startup notice** (`cmd/apogee/wire.go`) — fires only on the one cell that
   warrants it (Auto **and** confinement asked for **and** `FSWrite == false`), names the backend
   and the consequence, and is worded as the ladder working, never as a malfunction.
2. **`/confine`** — `status` reports the backend, its capabilities, the host id, and the effective
   setting; `off` / `on` swap Auto's blast radius for the running session through
   `Agent.SetConfineToWorkspace`; `off --save` also persists. A slash command was chosen over a
   startup y/N prompt or an extra Approval choice precisely to keep the accept away from the moment
   of peak frustration (the click-through-consent trap).
3. **`unconfined-hosts:`** — the host-scoped acknowledgement, resolving the open scope question in
   favour of *host*: `confine-to-workspace` keeps its global meaning, while "this machine is
   disposable" is recorded per machine against `platform.HostID()`, so a throwaway container's
   acknowledgement never follows `~/.apogee/config.yaml` onto a laptop.
4. **A comment-preserving config writer** — `--save` splices the entry as text guided by the parsed
   node positions (never unmarshal→marshal, which would delete the template's documentation),
   idempotent, atomic, mode-preserving, and re-parse-verified before the write.

**The constraint that must keep holding:** do **not** loosen the ladder. `resolveLadderAuto` must
never auto-run unconfined subprocesses on its own initiative when the backend is incapable — that
reintroduces the "unsupervised *and* unbounded" hole ADR 0004/0012 forbid, *without the user ever
choosing it*. What shipped is the tool making the user's own decision reachable, never the tool
deciding.

**Deliberately deferred residue:**

- **Surfacing the startup notice in the transcript, not just stderr.** *(Still open.)*
  `/confine status` renders in the transcript, but the startup notice is stderr-only (it is
  printed pre-alt-screen, like every other startup notice). Folding it into the UI belongs to the
  parked validated-set in-transcript banner work (deferred follow-up 04) — this plan explicitly
  did **not** build a banner framework.
- ~~**`apogee probe`** — reporting the confinement backend and its capability matrix as a
  subcommand, diagnosable without running an agent.~~ **CLOSED 2026-07-22 (Phase 5, items 1/3 —
  `docs/plans/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`;
  [ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)).**
  `apogee probe` (and `apogee probe host`, its scriptable twin) prints the host report — backend,
  capability matrix, `AutoEligible()` verdict, the effective `confine-to-workspace` after the host
  acknowledgement, the roots, endpoint reachability — free, offline and read-only, with no agent
  and no model call. It does not duplicate `/confine status`: both render one extracted verdict
  (`internal/probe`'s `BackendName` / `DegradedNotice` / `CapabilityLine`), so the CLI and the TUI
  cannot word the same matrix two different ways, and the report closes with the startup
  degradation notice verbatim.

Also closed by Phase 5, though it was never the residue's own bullet: **the degradation notice no
longer fires on a capable Windows host.** The notice's trigger cell (Auto + confinement asked for
+ `FSWrite == false`) is unchanged — what changed is that Windows now has a backend that reports
`FSWrite: true` (ADR 0020's low-integrity token, floor build 17763), so the notice narrows to the
hosts where it was always the honest answer: an older Windows, and the containers where landlock
returns `ENOSYS`.

---

## A presented document carries no sub-agent depth (the ⤷ label re-opens around it)

**Status:** parked 2026-07-21 (noticed while verifying the `present_document` plan,
`docs/plans/2026-07-21 - 01 - present-document-tool-plan.md`). Cosmetic, no wrong output.

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

## Validated-set twin ladders — probe and startup ask the same question twice

**Status: DONE 2026-07-22** — consolidated by the Phase 5 second review fixes
(`docs/plans/2026-07-22 - 03 - phase5-second-review-fixes-plan.md` item 6), pulled forward from
the `/improve-codebase-architecture` backlog after the SAME twin-ladder shape produced a second
divergence in one day (the post-fixes review's High: `probe model`'s claim ignored the unpinned-
model and weights-file rungs of startup's identity ladder).

What shipped: one shared ladder, `startupSetDecision` (`cmd/apogee/validatedsets.go`) — the
off-switches, `library.ResolveFingerprintFrom`, `validated.Match`, the explicit `mechanisms:`
precedence, and `validated.Validate` in startup's order — returning a decision value both call
sites render in their own voice: `resolveValidatedSet` enacts it (notices + the applied set),
and `probe model`'s `autoApplyKeys` (`cmd/apogee/probemodel.go`) reports it, running the ladder
once against the home as the probe just left it and once with the record rung removed to compute
the promotion. The recorded genuine differences survived: startup stays loud on the dangling
alias where the probe stays silent, and the probe still asks the question at two confidences.
Sources: ADR 0016 §5 + its 2026-07-19 realisation, ADR 0021 §4.

---

## Phase-5 verification leftovers — the owner-run passes this machine cannot perform

**Status:** carried forward 2026-07-22 at the Phase-5 close-out, out of the "Owner-run checklist"
of the now-archived
[`docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`](docs/plans/archived/2026-07-22%20-%2000%20-%20phase5-cross-platform-hardening-plan.md)
— read it for the full context (the ground truth, the settled design, and every work item's
NOTES). Phase 5 is **implemented**: all ten items shipped, the whole-plan gate is green (build,
vet on all three GOOSes, six cross targets, tests, `--help`). What is parked here is the
**verification** the Windows execution machine physically cannot do, plus one measurement that
wants an owner decision. Logged here rather than left in the archive so it isn't buried.

- **✅ DONE (2026-07-23) — Closeout Linux pass — `make check` on the Linux devbox.** Ran green,
  exit 0, on an Ubuntu devbox (kernel **7.0.0-28-generic** aarch64) with the race detector on
  (cgo enabled, gcc 15.2): gofmt, `go vet`, `go build`, `go test -race ./...` across all 24
  packages, the ADR-0010 import invariant, all six cross-compile targets, and `apogee --help`.
  This is the first run of the build-tagged Linux code paths (previously only cross-compiled from
  the Windows host), so it closes the Linux execution gap for Phase 5, the Phase-5 *review fixes*
  plan (`docs/plans/archived/2026-07-22 - 02 - phase5-review-fixes-plan.md`, item 13), and the
  Phase-5 *second* review-fixes plan (`.../2026-07-22 - 03 - ...`, whose untagged platform tables
  in `winconfine_test.go`/`host_test.go` now also run natively on Linux). Bonus: this box has
  `landlock` live, so the landlock-tagged enforcement battery ran live rather than self-skipping —
  see the CHANGELOG "Known post-release verification" note, "Linux landlock live enforcement",
  now also closed.
- **✅ DONE (2026-07-23) — Both automated live checks against the real endpoint.**
  (a) `make live-eval` (`TestE2ELiveModel`) passed under `-race` against
  `http://192.168.64.1:1111` (gemma-4-E4B-it-QAT): streamed, requested `write_file`, cleared
  Approval, file landed in the temp workspace — closes the open Phase-1 live eval (also flipped
  in `docs/design/technical-design.md` §"Suggested next-session entry point").
  (b) `TestSmokeLiveProfileSeam` passed under `-race` against the same endpoint: all three
  subtests (thinking-delimiter strip, native control, fenced tool-call parse+dispatch) green.
  Remaining from that trio: the **manual, owner-run live Auto-confined deliverable run** —
  tracked in the CHANGELOG "Known post-release verification" note. **Linux (landlock) arm
  ✅ done (2026-07-23)** on the Ubuntu devbox (kernel 7.0.0-28-generic aarch64): out-of-workspace
  write OS-denied with no prompt, in-workspace write succeeded, `demo__ping` MCP tool still
  raised Approval, delegated sub-agent's `NOTES.md` write rendered, escape file confirmed
  absent afterwards. Only the macOS (seatbelt) and Windows arms remain.
- **Live Auto-confined deliverable run on Windows**, if an LLM endpoint is reachable from that
  machine. The ADR 0020 backend itself is proven natively (escape battery + the real `Terminal`
  tool under `platform.NewConfiner()`, item 8's live evidence); an end-to-end deliverable
  session under Auto is what remains.
- **Degradation notice on a below-minimum-version Windows host** (below build 17763). Recorded
  **UNTESTED** in
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md)'s
  consequences — no such host exists here, and it is only verifiable if one turns up.
- **macOS cross-binary smoke:** `--help` plus a trivial session. Linux and Windows are each
  covered by an execution machine; the macOS binary is cross-built and never run.
- **OWNER CALL — whether to prune the Windows disk-label walk.** Item 8 measured what ADR 0020
  accepted but never quantified: the label pass costs **~1 ms per object**, so a synthetic
  5,051-object tree took **5.2 s to label and 2.2 s to revert**. It is paid once per box per
  session (first confined command, then shutdown), but a workspace with a large `.git` or
  `node_modules` will make that first `Confine` visibly block. It was recorded rather than tuned,
  because pruning the walk changes the ratified box semantics; the cheap remedies are a startup
  notice while it runs, or excluding ignored trees from it. No decision to defer to — this is the
  owner's call.

**Not repeated here:** the same checklist carried a "pre-existing, NOT Phase 5 scope" group
(Linux landlock live enforcement on a landlock-enabled kernel; the Linux/macOS live Auto-confined
runs). Those live in the CHANGELOG's **"Known post-release verification (owner-run / CI)"** note
and stay there — that note, not this file, is their tracking home.

---

## Windows Auto: box-local `%TEMP%` / toolchain caches

**Status:** recorded 2026-07-22 (Phase 5 review fixes,
`docs/plans/2026-07-22 - 02 - phase5-review-fixes-plan.md` item 12). Not built by that plan by
decision — it needs its own design session. Sources: ADR 0020 §2 (the "Consequence for the box
builder" paragraph) and `docs/design/confinement-execution-contract.md` §7.

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

## `internal/platform`'s two Windows confinement files exceed the file-size guideline

**Status:** recorded 2026-07-22 (Phase 5 review-fixes follow-up). Flagged for
**`/improve-codebase-architecture`** — a shape observation, not a defect: both files are
correct, tested and idiomatic; they are simply past the size at which a file stops being
navigable in one pass.

The coding standards keep files under **~400 lines**. The two files that carry the Windows
confinement backend are over it, and grew further during the Phase 5 review fixes (journal
retention helper, own-label guard, atomic writes, unreadable-journal residue, the
recovery-free report constructor, the build-floor predicate):

- `internal/platform/winconfine.go` — **581 lines** (untagged: the label journal's
  read/write/list/revert-decision half, SDDL and fold helpers, guardrails, residue and
  teardown notice wording, `belowWindowsFloor`).
- `internal/platform/confiner_windows.go` — **572 lines** (`//go:build windows`: token
  construction and the two selectors, the label/clear tree walks, `labelBox`, journal flush,
  crash recovery).

**The shape when picked up:** the split is by build tag today, not by concern, so each file
holds two or three of them. The obvious seams are the *journal* (record type, atomic
read/write/list, retention decision, revert) as its own unit, the *label walk* (read/set/clear
SDDL over a tree, reparse-point skipping, per-descendant tolerance) as another, and the
*notice wording* (`windowsResidueNotice`, `ConfinementTeardownNotice`, the shared `icacls`
remedy) as a third — leaving the confiner itself as the thing that composes them. Any such
move must keep the untagged/table-testable-on-Linux property the Phase 5 work deliberately
bought: the decision logic stays out of `//go:build windows` files so it can be tested on
every OS.
