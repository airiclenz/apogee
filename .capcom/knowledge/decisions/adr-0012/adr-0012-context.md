---
title: ADR 0012 context
---

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
