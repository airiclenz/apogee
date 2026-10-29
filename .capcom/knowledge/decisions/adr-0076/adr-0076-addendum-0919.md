---
title: "ADR 0076 addendum of 2026-09-19: D6's seam also carries engine-structural text, under its own header"
---

D6 placed advise text as a fenced trailer on the closing tool result because that position is
role-safe, prefix-cache-friendly and away from host-authored orientation. The same three properties
are what a per-request **engine** instruction needs when it must sit where the model reads next
rather than at the far end of a long system prompt — the first case is a capped delegate's wrap-up
directive, which a child stamped only in the system prompt was seen ignoring as it narrated its
next tool call instead of reporting. The seam therefore now carries two kinds of text:

- an **advise span**, fenced `[advice — reaction <id> (<origin> origin) at <moment>, turn N]`, whose
  header is derived from the Reaction's provenance (D6 unchanged);
- an **engine note**, fenced `[engine — <topic>]` … `[end engine — <topic>]`
  (`domain.RenderEngineNote`, `Message.WithEngineNote`, `Request.NoteOnTail`), whose header names
  the engine's own topic and never a Reaction id — so the model tells a structural instruction from
  a Reaction's advice by the header alone, and no Reaction can wear the engine's header.

Both are rows of the one provenance ledger (`Message.Advice`; an engine note's row carries `Topic`
and `OriginEngine`, no Reaction, Moment or Turn), so the one session-record strip and the one
staleness guard cover both, and an engine note is as ephemeral as advice. An engine note is applied
to the **request projection only** — `NoteOnTail` notes the last message when it is a tool result,
idempotently per topic, and reports false otherwise so the caller can fall back to
`AppendToSystem` — so it never enters the conversation, the snapshot or an event.

The second engine note on this seam is the **delegate ledger** (`[engine — delegations]`,
2026-09-19, `apogee-clb`): once an Exchange holds two or more delegations or any one that did not
complete, every request the coordinator builds carries, on its tail, one line per delegation in
spawn order —
`#<n> <name> — <completed|capped|faulted|cancelled|stopped|refused|skipped>[: <cause>] — output
<present|missing|none>` — rendered per request from the host's own record of what each child did
(`internal/agent/children.go`), with output presence read from the filesystem at render time. It
states facts and asks nothing, is stamped ahead of the wrap-up directive so that directive stays on
the very end, takes the same `AppendToSystem` fallback, and is structural: no key, no Reaction, on
under Bypass, and — like every engine note — never in the conversation or the snapshot.

The Rejected row "a dedicated tail message for every advise" is unaffected: the engine inserts no
message after the tool result. On a tool-less request the wire degrades the noted tool message to
user role whole, fence included — that user-role tail is the wire's rendering of one existing
message, not a new message the engine appended, which is the case the row rejects.

(Amended 2026-10-29 from the code: the outcome set was five words until 2026-09-26. `stopped` is a
child stopped singly or found running by a Turn cancel (ADR 0086 D4, ADR 0088), so `cancelled` now
names only a child the cancel reached before it started; `skipped` is a delegation a queued user
message pre-empted before its child started. A skipped row counts toward neither half of the trigger
but is listed whenever a note fires, so the spawn indices stay contiguous — `delegationLedger.note`
in `internal/agent/children.go`.)
