---
title: ADR 0045 considered and rejected
---

- **A top-level `sub-agents:` block or flat `sub-agent-*` keys** for routing + posture: the
  owner chose the flag ON the entry — the server that takes the work is where the whole
  feature reads, and ADR 0036's single-definition claim stays intact.
- **Discover-at-spawn (cached)**: a second discovery code path beside the heartbeat, first
  delegation pays the latency, and a grunt-side model swap goes stale — rejected for the
  second monitor.
- **Error-result or bounded-wait on an unusable target**: burns a Turn or stalls a fan-out;
  the parent's server is by definition alive, so the fallback floor is always there.
- **Parent server's cap always**: throttles a big grunt box to the smart server's slots and
  can oversubscribe a small one.
- **Per-ID posture merge**: needs an 'inherit vs off' spelling — the ambiguity replace-whole
  exists to avoid.
