// Package undo holds the per-exchange pre-image journal behind the human-facing
// `/undo` command: the record of what the agent's file writes replaced, kept so a
// human can put it back one Exchange at a time.
//
// The shape of the answer. Every mutation the shared write funnel performs is
// recorded as a PRE-IMAGE (the bytes that were there, or the fact that nothing was)
// plus a POST-HASH (the SHA-256 of what the mutation left, or the fact that it left
// nothing). Those two halves are all an undo needs and all it may trust: the
// pre-image is what gets written back, and the post-hash is the proof that the file
// on disk is still the one the agent wrote — if it is not, the human edited it since
// and their edit outranks the undo, so that path is SKIPPED and reported rather than
// silently overwritten. The same encoding covers every verb without a per-verb case:
// a create has no pre-image (undo removes the file), a delete has no post-state (undo
// writes the bytes back), an overwrite has both, and a move is simply two records —
// the source ending absent, the destination beginning absent or clobbering.
//
// Grouping is per Exchange, not per write, because that is the unit a human means by
// "undo that": one instruction to the agent, however many tool calls it took. Groups
// materialize LAZILY — [Journal.BeginGroup] only marks the boundary, and the group
// comes into being on the first write after it — so an Exchange that wrote nothing
// never becomes an undo step the human has to walk past. Delegated sub-agents write
// into their parent's current group (ADR 0039 fans them out concurrently, which is
// why every method here is mutex-guarded).
//
// The load-bearing property. Undoing group N restores each path to group N's
// pre-image, which is exactly group N−1's post-state. So a file touched in several
// consecutive Exchanges passes the conflict check at every step of the walk back,
// and repeated undos peel it off one Exchange at a time instead of stalling on the
// second. The generation counter ([Journal.Generation]) is the other half of the
// human protocol: `/undo` previews and stamps the generation, `/undo confirm`
// executes only if the journal has not moved since (ADR 0051).
//
// What this package deliberately is NOT. It is memory, not storage: the journal
// lives on the engine and dies with the process, so a resumed session cannot revert
// an earlier process's writes (ADR 0022 §8 — live host state is never session state),
// and the pre-images it holds are the cost of that choice. It has no redo. It knows
// nothing about the engine, the tools, or the TUI — it imports internal/security and
// the standard library and nothing else, which is what keeps it reachable from a
// headless Driver (ADR 0031, ADR 0033). And it covers only what the funnel hands it:
// subprocess writes (terminal, python, test runners), git checkouts, MCP and
// third-party tools mutate the workspace without passing through here and are
// documented as not undone (ADR 0002, ADR 0008).
//
// Restores and removals go through internal/security's fenced primitives —
// SafeWriteFile and SafeRemove, the very ones the funnel wrote through — so an undo
// inherits the same symlink and traversal refusals the original write had. It can
// never reach further than the write it is reversing.
//
// Files:
//   - doc.go — this map and the package's rationale.
//   - journal.go — the Journal and its record, preview, and revert surface.
//   - context.go — the context seam the engine hands the journal to the write funnel through.
package undo
