---
title: An embedder cannot register a vouched-for network tool (export the network funnel?)
status: parked
type: feature
priority: 4
labels: [security]
aliases: [apogee-37s]
created: 2026-09-03T18:40:20Z
---

**Status:** parked 2026-07-25 (the url-safety choke-point plan, D5). Post-v1, **additive** (a new
exported constructor in `internal/tools` re-exported by the facade — a minor bump).

**The situation.** Apogee's own network tools carry the **unexported** url-filter marker, obtainable
only by embedding the network funnel that owns the `URLGuard` (`internal/tools/network.go`), and the
Auto ladder keys the "runs unattended" cell on that marker (ADR 0012 amendment 2026-07-25). So a
host-registered tool that reaches the network **gates in Auto no matter how carefully it filters its
own URLs** — it has no way to route through the funnel and no way to claim the marker. This is
deliberate and it exactly mirrors `workspaceScopedWriter`, under which an embedder's write tool has
gated in Auto since Phase 3: the marker is unfakeable *because* it is unexported, and gating is the
safe direction.

**The natural move if demand appears:** export the funnel as a public `NewNetworkTool`-style
constructor (host-supplied `URLGuard` in, an embeddable value out) so an embedder's tool **inherits**
the marker by actually routing every URL through the guard, rather than being handed a way to
*assert* it. The property to preserve: the marker must remain impossible to obtain without the
guard — never an exported interface, a declared capability, or a config-listed tool name, each of
which would let an under-declaring tool reach the network unfiltered and unattended.

**Why it waits:** the demand is hypothetical (no embedder has asked), the current answer is merely
an extra prompt rather than a broken tool, and the export is purely additive whenever we want it.
