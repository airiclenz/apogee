---
title: "ADR 0022 addendum of 2026-08-10: a committed tool result carries its own verdict, and it rides the snapshot"
---

*(Ratified 2026-08-02 with the fix; recorded here from the amendment in the archived
`docs/design/archived/hook-mutation-api.md` §5, whose design draft is no longer a live document.)*

**The loss was at the commit seam, not on the wire.** `ToolResult.IsError` is authoritative on the
**live** seam — the tool reports it, and `PostToolResult` receives the `*ToolResult` — but the commit
into history copied only `Content`. So every **cross-Turn** Mechanism asking "did that earlier call
fail?" had nothing to read but the text, and fell back to the string matching apogee-sim was forced
into because a proxy only ever saw text. Over a successful `read_file` that text is *the file*.
`read_loop` therefore classified any source file containing `error:` or "does not exist" as a failed
read, and told the model to write the file it had just finished reading — a persisted record that
was honest about the bytes and wrong about what happened.

**The verdict is a field, stamped once.** The committed message carries
`domain.Message.ToolOutcome`, a tri-state — `""` unrecorded / `"ok"` / `"error"` — projected from
`IsError` by `domain.ToolOutcomeOf` at the **one** seam every tool result crosses into history
(`internal/agent` `appendToolResult`), so no route commits an unmarked result: a plain call, a
refusal, a gate denial, a sub-agent delegation. Classification of *what kind* of error (syntax,
import, missing file) stays mechanism-internal and is not a field on the type.

**It persists as an `omitempty` sibling, and needs no version bump.** `tool_outcome` rides the
engine envelope beside `interjected`, on the same terms: it is Apogee-owned and process-local (the
wire projection maps fields explicitly, so the marker never reaches a provider), and `omitempty`
keeps it off every non-tool message. That is what keeps `domain.SessionVersion` where it is under
decision 5's layer-ownership rule — an older snapshot simply lacks the key and decodes as
`ToolOutcomeUnrecorded`, and an older binary round-trips the key as an unknown sibling rather than
dropping it.

**Unrecorded is a route, not a gap.** A record snapshotted before the marker existed still resumes
into Mechanisms that must judge it, so the text sniffing survives — but only as the fallback that
`ToolOutcomeUnrecorded` selects, and only anchored to the result's **first line**, which is where a
tool's error text lives and where a file's contents do not answer for it. A restored pre-marker
session therefore degrades to the old heuristic on its old messages while every result committed
after the restore carries the real verdict, which is the same per-payload posture decision 5 takes
everywhere else: each layer reads what it owns, and an older payload degrades rather than lying.
