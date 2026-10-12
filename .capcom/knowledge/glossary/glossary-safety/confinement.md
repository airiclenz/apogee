---
title: Confinement
---

**Confinement**:
OS-level restriction of the **unbounded subprocess surface** (shell / subprocess), attaching to
**blast radius, not to a mode-wide binary** (ADR 0012, superseding ADR 0004): a tool runs
unsupervised only if its blast radius is bounded — **either** by OS confinement of the subprocess
surface (Linux **landlock** applied pre-`execve` on the child, or **user + mount namespaces** via
`bwrap` where landlock is absent — ADR 0081; macOS **`sandbox-exec`** wrapping
the child; Windows a restricted **low-integrity token** handed to process creation, with the box
expressed as a mandatory label on the disk and reverted on teardown — one clean subprocess
granularity on all three), **or** by Apogee's own
**path-safety to the box's writable set** — the workspace plus the session **Scratch dir**, which
is why that dir is the one target Apogee's own writers run on in every mode, Plan and Ask-Before
included (ADR 0012's second loosen, 2026-09-14) — for its own in-process write tools, **url-safety
for its own network tools**, **and the argv Apogee builds itself for its own read-side git tools** (`git_status`,
`git_log`, `git_diff_range`, `git_show` — the **RO-subproc** class: hooks and fsmonitor off, a repository whose
config names a program git would run refused outright, a scrubbed child environment, nothing
written, so the call is bounded without a box). A third-party tool of any of those kinds, whose
scoping Apogee cannot vouch for, gates instead of running unsupervised. It is a **capability
matrix, not a one-bit flag**: each backend reports what it can enforce (`fs-write`, `network-egress`,
…). In **Auto** the network is **open by default**, so **`AutoEligible()` requires filesystem
confinement only** — Linux Auto needs landlock ABI ≥1 (kernel ≥5.13), not ABI v4, or `bwrap` with
unprivileged user namespaces where landlock is absent; a backend that can fence neither says why in
`ConfinementCaps.Unavailable` (`· why: …` on every wording surface). The unbounded
surface is tuned by the global **`confine-to-workspace`** flag (below). The per-tool teeth remain:
**MCP**, which executes in a server Apogee cannot fence, gates through Approval whenever
`confine-to-workspace` is on; and if fs-confinement is *unavailable* on the host, subprocess tools
gate too ("confine if you can, gate if you can't") rather than refusing Auto. Apogee never runs a
tool call both unsupervised *and* unbounded. Lives behind the `domain.Confiner` interface, its
backends in `internal/platform/` (seatbelt on macOS / landlock on Linux, else `bwrap` user + mount
namespaces / Windows token — AppContainer was **rejected**, ADR 0020); default box =
workspace-write-only + **network-open**, with an optional network tightening list an embedder sets
(`Config.ConfineNetworkAllow` — no config key spells it). Only `fs-write` is ever claimed
on Windows: `network-egress` is not enforceable there, so it is not advertised, and a box that
asks for it fails closed. See
ADR 0012
and ADR 0020.
_Avoid_: "sandbox" (that is the bench's term — see below), "jail".
