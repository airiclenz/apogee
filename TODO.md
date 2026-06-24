# TODO — parked ideas (not in Phase 3 / not in `v1.0.0`)

Ideas worth doing later, deliberately deferred so they don't bloat the v1 freeze.
Each entry records *enough* design that we don't re-derive it when we pick it up.

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
