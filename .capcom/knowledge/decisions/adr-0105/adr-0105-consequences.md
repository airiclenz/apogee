---
title: ADR 0105 consequences
---

- `config.ServerEntry` gains `fallback:` and `fallback-after:`, as comparable values so the entry
  keeps `!=`. Load-time validation refuses unknown, self and duplicate names.
- The provider marks a fault class on every failed Delta in both codecs, so the engine can tell a
  triggering fault from one that never triggers.
- A new `domain.UpstreamSwitchedEvent` and line kind `upstream_switched` (`adr-0075-amend-1102`).
  The manual's kind list, the public alias list and the kind count follow.
- The engine gains a shared seat upstream for a parent and its unrouted children, a per-seat
  resolver seam, a grace resend, and a mid-Turn rehome returned to the step as its own outcome.
- The Drivers share one fallback resolver. Headless, the daemon, the TUI and the routed Sub-agents
  seat wire it. The Delegation target follows a fallen-over seat (`adr-0045-amend-1102`).
- A session-seat switch sets the session record's model to the fallback's. The daemon logs the
  switch.
- A fallover costs the prompt cache and, onto a smaller window, a fold. Both are visible: the
  event names the move, and the fold fires its own notice.
- Plan: `2026-11-02 - 00 - midrun-model-fallback-plan`; issue `midrun-model-fallback`.
