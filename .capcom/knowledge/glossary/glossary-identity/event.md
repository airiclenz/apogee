---
title: "Event: the sealed sum type the engine emits on its EventSink"
---

**Event**:
The unit of everything the loop reports: a value the engine emits on its `EventSink`, a **sealed**
sum type — the variant set is Apogee-owned and grows only additively, so a consumer switches on the
variants it knows and ignores the rest. Every variant carries the emitting agent's nesting Depth,
its **Turn** index and, for a **Sub-agent**, the **run id** of its delegation and the call id of the
`sub_agent` call that spawned it. The run id is the run's identity: the engine mints it once per
delegation (`<prefix>.<n>`, a random prefix per top-level agent and a counter shared by its whole
delegation tree), so no two delegations share one. The call id is the model's or the server's to
choose and may repeat — a text-format parser can hand two siblings of one reply the same id — so it
names the tool-call block a run answers, never the run itself. Emission
is serialized by the engine, so a **Driver** sees one linear stream even under a concurrent depth-0
fan-out — but a linear stream is not a serial one, and the events of one agent are recognised by
their run id (by depth and call id on an event recorded before the run id existed), never by
contiguity. Events are Go values and nothing more: the engine attaches no
timestamp, no sequence number and no session identity, and a Driver stamps those itself if its
surface needs them. See
ADR 0001 and
ADR 0039.
_Avoid_: "message" (that is a conversation message, the `Role`/`Content` kind), "notification",
"notice **Moment**" (that names the thirteen post-hoc points a user-origin
**Reaction** fires on — a lossy projection of some of these variants, not this).
