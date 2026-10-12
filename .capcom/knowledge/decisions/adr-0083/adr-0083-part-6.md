---
title: "ADR 0083 part 6: Wire traits stay inside the codec; the `== WireAnthropic` branches outside the adapters are tolerated (2026-09-15, review 09-14 #8)"
---

**Proposed.** The provider Client owns the effort dialect end-to-end — dialled with the pin,
latching its own detection, answering "can I ask this server for no reasoning?" — with the
request stating intent only; the `domain` mirror enum, the two mappers in `internal/agent/wire.go`
and the agent, spec and target dialect fields would disappear.

**Denied** (owner, 2026-09-15; plan `2026-09-15 - 01`, verdict table: "ADR 0060
D9 ratifies the channel; detection happens in the Monitor's Client"). The dialect is detected
passively by the heartbeat Monitor's Client, not the completion Client, so it *has* to travel:
ADR 0060 decision 9 makes the dialect the one fact that reaches the engine and names the channel
it rides; a completion Client that latches its own detection would either detect twice or dial
the Monitor's, which is the ferrying the candidate objected to under a different name. What did
land — later, and on its own record — is ADR 0078
(2026-09-16): the *wire* (`openai | anthropic`) is an unexported `wireCodec` inside the one
`provider.Client`, with "no engine-visible branch", and it implies the effort mapping (its
decision 3). That is where wire traits live. The branches on the wire that sit OUTSIDE the two
adapters — the 2026-09-20 review counted seven at `f30c37e0` (`provider/discovery.go`,
`provider/stream.go`, `config.go`, `probe/host.go`); at this record's HEAD they are the Client's
own codec selection and discovery arm (`selectCodec`, `WireFor`, `discovery.go`), the config's
`"anthropic"` rules (the `effort-dialect:` composition refusal, the wire enum, the keyed
parallel-agents default in `DefaultParallelAgents`) and `probe/host.go`'s two — are
**tolerated**: each is a validation or a reporting rule about the entry, or the one place the
codec is chosen, not a codec decision leaking out, and a count that drifts with the tree is not
the rule.

**Reopens on.** A third wire. Two arms are a branch; three are a table, and the seven tolerated
branches become fourteen at the same time — at which point the codec's trait table (ADR 0078
decision 7's seam) grows the answers those sites ask for and the branches fold into it. Or: a
routed child inheriting the wrong dialect again, the incident that motivated the candidate,
recurring after ADR 0078's dial seam.
