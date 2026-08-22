---
Status: accepted
Supersedes: ADR 0004
---

# Confinement attaches to blast radius; Auto is `confine-to-workspace`-tunable — supersedes ADR 0004

## Context

ADR 0004 made Auto mode require OS confinement of **both** filesystem-write **and**
network-egress, with network **default-deny** and **no escape hatch** (Linux Auto needing
kernel ≥6.7 for landlock ABI v4's network rules). Two problems surfaced once the design met
the work:

1. **Network-default-deny Auto is impractical for real coding.** `pip` / `go mod` / `npm` /
   `cargo` all fail until every host is allow-listed. An autonomous coding agent that cannot
   fetch a dependency is, in the owner's words, "useless." The pain is specifically the
   **network** axis — it was conflated with the genuinely valuable filesystem axis.
2. **The "confine every tool" framing forced impossible contortions.** Wrapping Apogee's own
   in-process writes in a per-thread landlock is irreversible-per-thread on Linux and has **no
   equivalent on macOS** (seatbelt confines a *subprocess*, not a thread). That route produced a
   thread-discard trick, an unenforceable no-goroutine contract, and a macOS-gates-every-edit
   asymmetry.

The autonomy ladder (Plan → Ask-Before → **Allow-Edits** → Auto) and a blast-radius reframing
dissolve both. This ADR records the resulting model and **supersedes ADR 0004**, reversing its
network-default-deny and no-escape-hatch decisions while **preserving** its capability-matrix and
its "never unsupervised *and* unbounded" core.

## Decision

**Confinement attaches to *blast radius*, not to a mode-wide binary.** The invariant ADR 0004
established is preserved, refined:

> A tool call runs without a human gate only if its blast radius is bounded — **by OS confinement**
> for the unbounded subprocess/network surface, **or by Apogee's own path-safety-to-workspace** for
> its own in-process write tools. Apogee never runs a tool call both unsupervised and unbounded.

OS confinement therefore attaches to the **single, all-OS subprocess granularity** (Linux landlock
applied to the child after fork, before `execve`; macOS `sandbox-exec` wrapping the child). There is
**no per-thread in-process landlock** — Apogee's own writes are bounded by `path_safety` instead — so
the thread-discard trick, the goroutine-escape hole, and the macOS asymmetry are all **deleted**.

**Auto is tunable by a `confine-to-workspace` flag** (the reversal of ADR 0004's both-required
network-deny). The flag is a **global-config key** (`~/.apogee/config.yaml`), default **`true`**, and
is only meaningful in **Auto** (the lower three modes have a human backstopping the unbounded surface):

- **`confine-to-workspace: true` (default).** Filesystem writes are fenced to the workspace; the
  **network is open**.
  - **Subprocess/shell tools** run OS-confined to the workspace. An escape (write outside the
    workspace) is **OS-blocked** — an OS denial (EACCES under Linux landlock, EPERM under macOS
    seatbelt; wording reconciled 2026-08-22, ADR 0056 — this line previously said "EPERM"
    unqualified, which a real Linux denial never prints) — there is no approval prompt; the command
    fails and, since ADR 0056, a streamed denial also **stops the rest of the confined call**
    (kill-on-denial watch) so later script lines never run against a half-done state. Network
    reaches are open (`NetworkAllow` may *tighten* to a deny-list for the security-conscious).
  - **Apogee's own in-process write tools** are bounded by path-safety. An *in-workspace* write
    auto-runs; an *out-of-workspace* write raises an **Approval popup** (Apogee inspects the path
    before executing, so it *can* ask — unlike a subprocess).
  - **Apogee's own network tools** (`web-fetch`, `http-request`) **auto-run** (filtered by
    url-safety). They no longer gate in Auto: a subprocess can already `curl` the same host, and the
    native tool is *safer* because it passes url-safety. (Reverses ADR 0004's "gate even in Auto" for
    these.)
  - **MCP tools gate through Approval** — they execute in an external server Apogee cannot fence, so
    gating is how `confine=true`'s promise stays honest. "Allow for this session" caches at **server**
    grain (approving one `github` tool allows `github.*` for the Session).
  - **Confine-if-you-can, gate-if-you-can't.** If fs-confinement is *unavailable* on the host (no
    landlock; no `sandbox-exec`), subprocess/shell tools **gate through Approval** rather than refusing
    Auto outright — Auto stays useful with the unfenceable surface falling back to a prompt.
- **`confine-to-workspace: false` ("I am the sandbox").** Nothing is fenced and nothing gates
  (subprocess and in-process writes reach the whole filesystem, network is open, MCP runs free) —
  **except the dangerous-action guard floor** (below). This is `apogee-code`'s posture / Claude Code's
  `--dangerously-skip-permissions`. It is **safe only inside a VM/container**, which is the user's
  responsibility. Because it is a global-config key, **a project-level config cannot enable it** (the
  hostile-repo footgun is closed); editing your own global config *is* the deliberate acknowledgement.
  A **per-session startup warning** prints whenever Auto runs unconfined.

**`AutoEligible()` drops to filesystem confinement only.** Since the network is open by default, Auto
no longer requires network-egress confinement. `ConfinementCaps.AutoEligible()` becomes `FSWrite`
alone (was `FSWrite && NetworkEgress`). Consequence: **Linux Auto needs only kernel ≥5.13** (landlock
ABI ≥1), not ≥6.7 — a large widening. Network-egress confinement remains a *reported capability*, used
only when a user opts back into network-deny.

**The dangerous-action guard is the mode-independent floor**, complementing — never replacing —
confinement. It is a **footgun-guard, not a security boundary**: it catches a small model's obvious
catastrophic *mistakes*, and is trivially bypassable by anything determined, so it is **never** what
makes `confine=false` safe (only the VM is). Two tiers, tighten-only, runs before the mode
disposition: **Tier 1 hard-refuse** (`rm -rf` of a root/home/system path, fork bombs, writes to
`~/.ssh`/credential/persistence files) with no per-call override; **Tier 2 force-approval** (`curl |
bash`-class — a legitimate installer idiom, so a speed-bump not a block) forcing the Approver even in
Auto. Default-on; editable in global config (a user may add *or* remove — it is their machine);
project config may only *add* (tighten). Lives in `internal/security` (P3.6).

## Considered options

- **Keep ADR 0004 (network-default-deny, both-required, no escape hatch)** — rejected: impractical for
  package-manager workflows (the concrete complaint), and it was what forced the now-deleted in-process
  per-thread landlock contortions.
- **Fully unconfined Auto by default** (apogee-code / `--dangerously-skip-permissions` as the default)
  — rejected *as the default*: it discards the near-free filesystem fence — the one thing that stops
  the worst autonomous-small-model disaster (writing outside the project: `~/.ssh`, sibling repos) —
  for *every* user, to satisfy the VM-user subset. Offered instead as the explicit `confine=false`
  opt-in, so the default floor stays high and VM users say so once.
- **A configurable per-tool × mode disposition matrix as the loosening mechanism** — deferred
  (`ISSUES.md`), post-v1, additive. When built it is **tighten-only**; the only blanket loosen is
  `confine=false`.

## Consequences

- **ADR 0004 is superseded.** Its network-default-deny, both-required, and no-escape-hatch decisions
  are reversed. What survives, restated here: the capability matrix, the blast-radius "never
  unsupervised *and* unbounded" invariant, and the per-tool teeth (MCP gates).
- **`ConfinementCaps.AutoEligible()` changes** to `FSWrite`-only; the `agent.New` Auto gate and
  `ErrAutoUnavailable` follow. Linux Auto's reach widens from kernel ≥6.7 to ≥5.13.
- **`web-fetch` / `http-request` no longer gate in Auto** (url-safety filtered instead); **MCP gating
  is now conditional** on `confine-to-workspace` (gates when `true`, free when `false`).
- **`confine-to-workspace=false` is a documented footgun.** It is safe *only* via a VM; the
  dangerous-action guard is a mistake-net, not a security layer, and must never be described as making
  unconfined Auto "safe."
- **CONTEXT.md** Agent-mode / Confinement entries and the **Phase-3 plan** (§3 D1/D5, §1 exit #3,
  P3.4/P3.6/P3.8/P3.11) are updated to this model; the plan's §5 reopened block is resolved.
- The P3.1 design pass wrote the *implementation contract* on top of this settled *policy* —
  [`docs/design/confinement-execution-contract.md`](../design/confinement-execution-contract.md): the
  `Confine` signature (prepare-in-place over a `*exec.Cmd`; the closure form is deleted), the
  `workspaceScopedWriter` marker, the per-call disposition table, the capability-honesty rule, and the
  shared escape-probe harness P3.2/P3.3 build to. **Where that contract and this ADR's prose differ on a
  mechanism, that contract is authoritative on the *how*; this ADR remains authoritative on the *policy*.**

## Amendment (2026-07-21) — the host-scoped acknowledgement, and the tool may offer the loosen

**Why now.** "Confine-if-you-can, gate-if-you-can't" is not the exotic branch it was assumed to be:
`landlock_create_ruleset` returns **`ENOSYS`** in most containers and many VMs *regardless of kernel
version* (verified on kernel 6.18.15, well past this ADR's 5.13 floor — `NewConfiner()` yields a
landlock backend reporting `FSWrite=false`). The probe is right and the facility is genuinely absent,
so for containerised users the degraded path is the **common** path: Auto is entered, every terminal
call raises Approval, and nothing says why. This amendment changes **how the acknowledgement is
scoped** and **how the user reaches it** — not what Auto is allowed to do on its own.

**(a) `confine-to-workspace` is unchanged, and is not deprecated.** It remains a
global-config-only key (`~/.apogee/config.yaml`), default **`true`**, meaningful only in Auto, and
`false` still means the blanket "I am the sandbox" loosen on **every** host the config travels to,
with its per-session unconfined-Auto warning. A project config still cannot set it.

**(b) A host-scoped acknowledgement, because the flag is global but the claim is not.** The claim a
user makes when they loosen is *"**this machine** is disposable"* — a host fact. Carried by a global
key, that claim follows `~/.apogee/config.yaml` from a throwaway container onto a laptop, where it is
false and dangerous, and it does so **silently**. A new file-only list records the claim at the
grain it is actually true at:

```yaml
confine-to-workspace: true      # global default, unchanged

unconfined-hosts:               # explicit per-host acknowledgement
  - id: "devbox-a1b2c3"
    acknowledged: "2026-07-21"
    note: "disposable container, landlock unavailable"
```

Resolution order: an explicit global `confine-to-workspace: false` wins (unchanged meaning); else a
current-host match in `unconfined-hosts[].id` yields an effective `false`; else `true`. A match
counts only where the host has an identity to be matched *by*: a machine that can supply neither a
hostname nor a machine identifier computes the same id as every other such machine, so honouring
that match would make one stored acknowledgement apply to all of them — the exact travel this
interlock exists to prevent. There the match is refused with a notice and `--save` declines to write
the id at all; `/confine off` (session-only) remains available, which is the same safe-direction
answer the ephemeral-container case gets. The list is
**global-config-only** on the same reasoning as the flag — a hostile repo must not be able to
name your host — and an unknown id is simply "not this host", never an error, because the list is
expected to accumulate machines.

The host id is a **safety interlock, not an authentication mechanism**: its whole job is to stop an
acknowledgement travelling between machines unnoticed, not to resist forgery — anyone who can edit
the config can write any id, exactly as the dangerous-action guard "is NOT a security boundary". It
fails in the safe direction: a container with a fresh machine identifier per run does not match its
stored acknowledgement and is confined again, which is an annoyance with a one-command answer
(below), not a hole.

**(c) The tool may now *offer* the loosen — `/confine off`.** When Auto is selected on a backend
reporting `FSWrite == false` while confinement is effectively on, Apogee prints a notice naming the
active backend, saying plainly that commands cannot be fenced on this host and will therefore ask
for approval, and pointing at `/confine off` (this session, writes nothing) and `/confine off
--save` (and record this host in `unconfined-hosts`). This does **not** weaken this ADR's
"editing your own global config *is* the deliberate acknowledgement" posture, because what the offer
removes is the *search cost*, not the *deliberateness*:

- the accept is a **distinct affirmative act** the user types afterwards — there is no default-yes,
  no enter-to-accept, and no remembered "always";
- it is deliberately **not** a startup y/N prompt and **not** an extra choice on the Approval popup:
  both would put the acknowledgement at the moment of peak frustration, which is the
  click-through-consent trap;
- **session scope is offered ahead of persistence**, so the lower-blast-radius answer is the easy
  one and persisting reads as the heavier choice;
- the wording states the blast radius (Auto will run **every** command unfenced with the user's full
  privileges) and must never be phrased as repairing a malfunction — **nothing is broken**; the user
  is choosing to drop a guarantee because their environment is disposable. A persisting write names
  the file it changed and the entry it added.

**(d) The ladder is untouched; auto-loosening stays forbidden.** `resolveLadderAuto` still confines
what it can and gates what it cannot, and no code may run an unconfined subprocess in Auto on its
own initiative when the backend is incapable — that is the "unsupervised *and* unbounded" hole this
ADR and ADR 0004 exist to close. The only thing that ever loosens is a user act; this amendment adds
a smaller-scoped way to express one and a shorter route to making it, and nothing else.

Implementation lives in [`docs/plans/auto-confinement-degradation-plan.md`](../plans/auto-confinement-degradation-plan.md);
CONTEXT.md carries the term **Host acknowledgement**.

## Amendment (2026-07-25) — url-safety is vouched-for by construction; an unvouched network tool gates

**Why now.** The decision above scopes Auto's network auto-run to **Apogee's own** network tools:
*"Apogee's own network tools (`web-fetch`, `http-request`) auto-run (filtered by url-safety)"*. The
ladder, however, keyed that cell on the **declared effect kind** — any tool reporting
`domain.EffectNetwork` landed in the vouched-for network class and auto-ran unattended in Auto — so a
host-registered tool that declared the kind reached the network with **no** url-safety at all, and
dispatch could not tell the difference. The `URLGuard` was construction-injected into Apogee's own
tools (`NewWebFetch(host.URLGuard)`), so a third-party network tool could not have been url-filtered
even if it wanted to be: there was no ctx handle for url-safety the way there is for Confinement.
The ladder had generalized a promise this ADR scoped narrowly. This amendment makes the class mean
what the ADR always said, and changes nothing about what Apogee's own tools may do.

**(a) The vouched-for network class now requires a marker, not a declaration.** Apogee's own network
tools reach the network through **one funnel** (`internal/tools`' `networkTool.do` — check the guard,
build the client, make the request, render the failure message host-scoped), and embedding that
funnel is the **only** way to obtain an unexported url-filter marker. The marker cannot exist without
the guard, so it is unfakeable outside the package. Classification keys on the marker: an
`EffectNetwork` tool that carries it keeps the **auto-run** cell (Apogee vouches that every outbound
URL passed the host's `URLGuard`); an `EffectNetwork` tool **without** it is a distinct
third-party-network class that **gates through Approval**, with its own prompt reason (`unfiltered
network reach` versus `network reach`). This is the same answer the ladder already gives MCP and
third-party writes, on this ADR's own reasoning — *gating is how `confine=true`'s promise stays
honest* — and with no Approver configured the gate resolves to a Refuse by the existing rule.

**(b) Strictly tighten-only, and `confine-to-workspace: false` is unaffected.** A call that gated
before still gates; a call that auto-ran before only ever moves to a prompt, never the reverse.
Apogee's own `web-fetch` / `http-request` / `web-search` are in the marked class and behave exactly
as this ADR describes. `confine-to-workspace: false` ("I am the sandbox") returns its verdict
**before** the class switch, so it still runs everything unattended — the loosen is untouched. In the
lower three modes every non-read-only tool already gates, so the only visible change there is the
more honest prompt reason.

**(c) The trade-off: an embedder cannot mint a vouched-for network tool.** Because the marker is
unexported, a host-registered network tool gates in Auto no matter how carefully it filters its own
URLs. That is the **same second-class status the write axis has had since Phase 3** — an embedder's
write tool cannot carry `workspaceScopedWriter` either, and gates. We accept it rather than export
the funnel now: gating is the safe direction, the demand is hypothetical, and exporting a
`NewNetworkTool`-style constructor later is purely additive (it would let an embedder **inherit** the
marker by routing through the guard, never claim it). The door is parked in `ISSUES.md`, not shut.

**(d) The floor this rests on is unchanged.** The `URLGuard`'s default-on, resolved-IP SSRF floor
(pre-flight **and** at dial time) is moved into the funnel, not revisited; the MCP transport's
connect-time endpoint check is a different lifecycle and stays where it is; subprocess network reach
remains out of scope by this ADR's own reasoning (*"a subprocess can already `curl` the same
host"*). One security property generalizes as a side effect: the host-only, key-scrubbed failure
message that was `web_search`'s private discipline is now **every** network tool's, by construction.

Implementation lives in [`docs/plans/2026-07-25 - 00 - url-safety-choke-point-plan.md`](../plans/2026-07-25%20-%2000%20-%20url-safety-choke-point-plan.md);
the class row and gate reason are in
[`docs/design/confinement-execution-contract.md`](../design/confinement-execution-contract.md) §4,
and CONTEXT.md's **Confinement** and **Safety guardrails** entries carry the prose.

## Amendment (2026-07-26) — the SSRF floor is the anti-*model* control, so a configured MCP endpoint is exempt from it and pinned instead

**Why now.** The amendment above says the MCP transport's connect-time endpoint check *"stays where
it is"*. Where it is turned out to be wrong, and this amendment is the line that changes. The check
ran the **resolved-IP SSRF floor** over the `endpoint:` a user typed into their own
`~/.apogee/config.yaml`, so `http://127.0.0.1:7331/mcp` or `http://192.168.64.1:7331/mcp` — the
ordinary way to run an MCP server — was refused as loopback/private. `Connect` is all-or-nothing and
`cmd/apogee` treats an MCP failure as fatal, so **apogee would not start**, and there was no config
escape (`DisableIPFloor` is deliberately code-level only). The asymmetry was sharp: the LLM
`endpoint:` — the same category of user-chosen, config-supplied, routinely private address, and the
shipped template's own default — is dialled with an unguarded client.

**(a) The floor is scoped to what the model drives.** The SSRF floor exists to stop a
prompt-injected **model** pivoting to loopback / IMDS / the LAN. An `mcp-servers:` endpoint is
config-file-only (a project config cannot set it) and is never model-supplied, so the floor is the
wrong control over it: it does not bound a model, it overrides a **host** decision the user already
made. The pre-flight check on a configured endpoint therefore runs **scheme/host allow-deny only**
— the user's own policy still applies, including a `DenyHosts` entry — and not the resolved-IP
floor. Nothing model-driven changes: `web_fetch` / `http_request` / `web_search`, and every future
model-supplied URL, keep the blanket floor pre-flight **and** at dial.

**(b) The dial-time control becomes endpoint-aware, not absent.** Exempting the pre-flight alone
would have been a no-op — the dial control refused every private address, so a localhost server
still could not connect — and dropping it would have made the endpoint a hole. Instead the MCP
transport dials under a control **pinned** to the endpoint's own resolved addresses
(`URLGuard.PinnedDialControl`): those pass, and **every other address on that connection still meets
the floor**. A DNS rebind, an SSE `endpoint` event, or a redirect pointing that transport at a
DIFFERENT private address is refused exactly as before. The carve-out is one address the user named,
not "private addresses are fine on this connection". The addresses are resolved once, at connect,
so nothing the transport learns later can widen the set, and an endpoint that cannot be resolved is
a connect-time error rather than an unpinned connection.

**(c) Redirects on an MCP transport are no longer followed.** The MCP client's HTTP client
reproduced the funnel's builder field-for-field but omitted its `CheckRedirect` policy, so MCP
transports auto-followed redirects while Apogee's own tools refuse to. A redirect could carry a
vetted connection to a host the endpoint's string-level allow/deny decision never saw. It now
returns the redirect response instead of following it, on the funnel's own reasoning. A server that
redirects must be configured at the URL it redirects to.

**(d) Everything else in this ADR is untouched.** The per-call disposition of an **MCP tool** is not
in question: the `mcp` class still gates through Approval in Auto with its server-grain
allow-for-session cache (contract §4's **mcp** row, unmoved). This amendment is about the
**connection** to a configured server, not about what its tools may do. `confine-to-workspace` is
unaffected, and subprocess network reach remains out of scope.

**The trade-off.** A user who types a private `endpoint:` reaches that address, which is the
intent — the same standing their LLM server's own endpoint and their stdio `command:` already have. What is
NOT accepted is that trust widening past the address they named, which is what the pin prevents.
The floor remains a floor everywhere it bounds a model.

Implementation lives in [`docs/plans/archived/2026-07-26 - 03 - url-safety-live-gap-plan.md`](../plans/archived/2026-07-26%20-%2003%20-%20url-safety-live-gap-plan.md)
(item 14); the client shape is in
[`docs/design/mcp-client.md`](../design/mcp-client.md), and `internal/mcp`'s package
documentation carries the trust boundary in prose.
