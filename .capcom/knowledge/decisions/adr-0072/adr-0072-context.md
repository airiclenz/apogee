---
title: ADR 0072 context
---

A long run forgets what it set out to do. Compaction is the mechanism — it summarises the
conversation and drops the messages behind it, and the plan a model wrote in its third reply is
exactly the kind of prose a summary compresses into a sentence. The standing system content is the
one place that survives that, because it is re-seeded at position 0 on every request
(ADR 0023 §6), which is
why the host's own facts already live there.

Nothing in that content is the model's. The rendered template is the user's persona, the
orientation block and the delegate report block are the engine's statements about the host, and
the workspace context files are the repository's. A model that decomposes a job into eight steps
has nowhere durable to put the eight steps, so it re-derives them, drops one, or re-does one it had
already finished.

The obvious answer was denied once. The first tool-surface poll recorded "task/todo persistence" as
**Mechanism territory (guided decomposition), not a tool**
(`docs/design/tool-surface-findings.md`), and the denial was right about what it was refusing: a
**Mechanism** that inspects a request and injects a decomposition the model did not ask for is
tuning, and tuning is gated on bench evidence. It was wrong about the thing being asked for. A tool
the model calls when it judges the work worth tracking prescribes no decomposition, fires on no
Turn the model did not open itself, and is exactly the same shape as every other affordance on the
roster: a name, a schema, and nothing that happens unless the model calls it. The owner reversed
the denial on 2026-09-02 on that distinction.

Two adjacent records already answered most of the design questions this raises.
ADR 0059 settled how a tool
reaches state the engine holds — the engine owns it, the call context carries it — and
ADR 0022 §8 settled what is and
is not written into a Session record. The one genuinely new question is the standing block: every
engine-owned part of that content has so far been a per-session constant, and a checklist is not.
