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
    workspace) is **OS-blocked (EPERM)** — there is no approval prompt; the command simply fails and
    the model routes around it. Network reaches are open (`NetworkAllow` may *tighten* to a deny-list
    for the security-conscious).
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
  (`TODO.md`), post-v1, additive. When built it is **tighten-only**; the only blanket loosen is
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
- The P3.1 design pass now writes the *implementation contract* (the `Confine(fn)` signature, the
  shared escape-probe harness, the `workspaceScopedWriter` marker) on top of this settled *policy*.
