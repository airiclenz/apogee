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
// The second capture path. Given a [Snapshotter] and the workspace it images
// ([WithSnapshotter], [WithWorkspace]), a group also carries the pair of whole-tree
// images taken around its Exchange — [Journal.MarkPre] before the first write-capable
// tool call, [Journal.Close] at the Exchange's end — and the diff between them is the
// scope a revert may reach beyond the funnel's own records (ADR 0074). That is what
// puts the writes the funnel never sees — subprocesses, MCP servers, git checkouts —
// back within reach of `/undo`. The two paths never contest a path: where both saw one
// file the funnel's pre-image wins, because it was read at the mutation site, so the
// diff only ever ADDS. An approved out-of-workspace write is outside the work-tree and
// stays the funnel's alone, per process (ADR 0074 decision 9), and a journal given no
// snapshotter — no git, or `undo-snapshots` off — is exactly the ADR 0051 journal
// described above, which is a supported configuration and not a broken one.
//
// Redo. A reverted group moves to a redo stack, and [Journal.Redo] re-applies it under
// the same two-step protocol with the two images swapped: it writes what the agent left
// and expects to find what the undo put back, skipping and reporting a path the human has
// edited since. The next Exchange that actually writes clears the stack — at the first
// record, or at a Close whose diff is non-empty — because a redo across a newer write
// would re-apply an old tree over work just asked for (ADR 0074 decision 6).
//
// Persistence. Given an index path beside those images ([WithIndexPath]) the journal writes
// journal.json after every Close, Revert and Redo, and [Load] reads it back, so `/undo` still
// reaches the exchanges of the process before this one (ADR 0074 decision 5). The file is an
// INDEX and holds no bytes: the ordinals, the generation, each exchange's pair of tree ids and
// the workspace they were taken of, which is checked on the way in — a session resumed against
// a different tree loads nothing and says so ([ErrWorkspaceMismatch]). A group with no trees,
// or whose two trees are equal, is only this process's to take and stays in memory: that is the
// funnel-only group and the approved out-of-workspace write, whose pre-images the store never
// holds (ADR 0074 decision 9). The store itself is host state keyed by session id and lives
// outside the session record, so ADR 0022 §8 holds — the session record still carries no live
// host state.
//
// What this package deliberately is NOT. It is not the object store: it persists tree IDS and
// reads bytes back out through the [Snapshotter], which is what keeps whole-tree images out of
// its own reach. It knows
// nothing about the engine, the tools, or the TUI — it imports internal/security and
// the standard library and nothing else, which is what keeps it reachable from a
// headless Driver (ADR 0031, ADR 0033), and the object store behind a Snapshotter reaches
// it through an interface rather than the other way round.
//
// Restores and removals go through internal/security's fenced primitives —
// SafeWriteFile and SafeRemove, the very ones the funnel wrote through — so an undo
// inherits the same symlink and traversal refusals the original write had. It can
// never reach further than the write it is reversing.
//
// Files:
//   - doc.go — this map and the package's rationale.
//   - journal.go — the Journal and its record, preview, and revert surface.
//   - snapshot.go — the Snapshotter seam, the two capture points, and the diff-only paths.
//   - redo.go — the redo stack: RedoPreview and Redo, Revert's mirror.
//   - notes.go — the rendered listing of a step or a report, the wording every Driver shares.
//   - persist.go — journal.json: the Index, Save's atomic write and Load's materialisation.
//   - context.go — the context seam the engine hands the journal to the write funnel through.
package undo
