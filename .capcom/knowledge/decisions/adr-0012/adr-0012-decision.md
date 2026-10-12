---
title: ADR 0012 decision
---

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
