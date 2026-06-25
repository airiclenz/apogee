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

- **[P0] Chat input mini-language** — a parse layer between the input box and the agent. apogee's
  `submit()` (`internal/tui/model.go:355`) sends the raw string straight through; it should route
  `/`-prefixed lines to command handlers, extract `@` tokens into refs, and resolve `/skill`
  tokens. Needs an autocomplete overlay (mirror the existing approval-prompt overlay). Commands to
  port (from `Ws`): `/clear` (drop model context, keep transcript), `/compact` (LLM summary),
  `/continue` ("Please continue"), `/server` (switch server), `/skill <name>` (attach a skill).
  - `@<file>` is **half-scaffolded**: `domain.UserInput.FileRefs` exists
    (`internal/domain/config.go:138`) but is deliberately unresolved — `loop.go:410
    noteUnresolvedFileRefs` errors that refs "are not yet resolved into context and were ignored."
    Port needs both a TUI `@`-parser **and** a real resolver (path-bounded by the existing `os.Root`
    workspace pin). Add a `SkillIDs []string` field next to `FileRefs`.
  - `/compact` can trigger the **existing** generative Compaction reducer in `internal/context`
    (today only budget-driven); expose an `Agent.Compact()` entry point.
  - `/clear` needs a new `Agent.ClearContext()` (only `Snapshot()` exists today, `agent.go:148`).

- **[P0] Skills system** (prerequisite for `/skill`) — apogee has **zero** skills code
  (`grep -ri skill --include=*.go` → 0). New `internal/skills` package: discover `.md` skills from
  layered dirs (`~/.apogee/skills/`, workspace `.apogee/skills/`, workspace `skills/`) with YAML
  frontmatter → `{id, displayName, summary, body}`; add a `useProjectSkills`-equivalent config key
  (default true) in `cmd/apogee/config.go`; inject attached skill bodies into the turn.

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

- **[P2] Throughput display** — rolling tokens/sec readout in the status line
  (`internal/tui/model.go` footer/status render). Distinct from the excluded context-budget gauge.

**Related (already parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

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
P3.11 (file-only, default-off) because web_search is unusable without it.

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
