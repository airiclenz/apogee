---
Status: accepted
---

# Undo is a per-exchange in-memory pre-image journal

## Context

`ISSUES.md` carried "**[P2] Undo all agent changes**" from the predecessor's feature sweep: the
human can watch the agent rewrite twenty files and has no way back except the editor's own undo,
file by file, or a `git checkout` that also throws away everything they wrote themselves. The
entry came with a claimed oracle — apogee-code, the VS Code extension, whose TDD describes the
feature at `Apogee-Code-TDD.md:164`. Verified against that source repo at v0.2.58 and at head,
the claim is stale: no implementation exists there. There is no behavioural precedent to port,
so every property below is a decision rather than a match.

Two facts about the codebase bound the design before any preference does. First, the writes
already funnel: every content mutation a first-party tool performs passes `safeWriteFile`
(`internal/tools/path_safety.go`) into `internal/security`'s fenced primitives, and the tools
that do it carry the unexported `workspaceScopedWriter` marker
(`internal/tools/workspace_scoped.go`) that dispatch uses to path-bound rather than confine them.
Second, only filesystem writes are durable and reconstructible at all — [ADR
0008](0008-stateless-tools-and-non-forkable-external-effects.md) settled that terminal, network
and subprocess effects are neither forkable nor undoable, and nothing here reopens it.

The owner resolved the open design questions on 2026-08-19 by question round; the plan author
resolved the rest against the sources above. This record ratifies both sets. It supersedes
nothing.

## Decision

**Undo is a per-Exchange stack of pre-images, captured at the shared write funnel, held in
memory for the life of the process, and applied by a human through a two-step `/undo`.** The
mechanism is `internal/undo` — a library that imports `internal/security` and the standard
library and nothing else, so a headless Driver can reach it exactly as the TUI does ([ADR
0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md),
[ADR 0033](0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)).

**1 — The unit of undo is the Exchange, and the stack walks back one at a time.** A human who
says "undo that" means one instruction they gave, not one of the fourteen writes it took —
`CONTEXT.md`'s Exchange is already that unit, so the journal groups by it. Each `/undo` reverts
the most recent un-reverted group; repeating it walks further back. Groups materialize **lazily**
— the boundary is marked when an Exchange opens and the group exists only from its first write —
so an Exchange that wrote nothing never becomes a step the human has to walk past. An
[Interjection](../../CONTEXT.md#turns-and-stepping) opens no group: it commits *mid*-Exchange
([ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md)), and a remark that
splits one instruction into two undo steps would make the stack lie about what the human asked
for. For the same reason a delegated sub-agent writes into its **parent's** current group and
opens none of its own: one `/undo` takes back the whole instruction however wide the fan-out
([ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
runs those children concurrently, so every journal method is mutex-guarded).

**2 — One record shape covers every verb: a pre-image plus a post-hash.** Each record holds the
bytes that were at the path (or the fact that nothing was) and the SHA-256 of what the mutation
left (or the fact that it left nothing). Within a group the first pre-image and the last
post-state per path win, one entry per path, insertion-ordered. That encoding needs no per-verb
case: a create has no pre-image (undo removes the file), a delete has no post-state (undo writes
the bytes back), an overwrite has both, and a move is simply two records — the source ending
absent, the destination beginning absent or clobbering. The load-bearing consequence is that
undoing group N restores each path to group N−1's post-state, so a file touched in several
consecutive Exchanges passes the conflict check at every step of the walk back instead of
stalling on the second.

**3 — Capture is at the funnel, and that IS the coverage boundary.** Pre-images are recorded
where the writes already converge, so the covered set is exactly the `workspaceScopedWriter`
family — `write_file`, `edit_existing_file`, `single_find_and_replace`,
`multi_find_and_replace`, `copy_file`, `move_file`, `delete_file` — and stays that set as tools
are added, without a second list of tool names kept in sync somewhere in dispatch. Everything
that mutates the workspace *without* passing the funnel is **out of scope and documented as
such**: writes made by `terminal`, `python_exec` and `run_tests` subprocesses, the working-tree
mutations of `git_branch` / `git_commit` checkout forms, MCP tool servers, and any tool an
embedder registers ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)
— tools are an open extension point, so no closed undo contract can be promised over them; [ADR
0008](0008-stateless-tools-and-non-forkable-external-effects.md) for the subprocess half).
README states this boundary in the human's own words rather than leaving it to be discovered.

**4 — Only successful mutations are recorded, and an unreadable pre-image records nothing.** The
pre-image is read before the mutation and the record committed only after it lands, so a refused
or failed write leaves no undo step behind. If the pre-image cannot be read for any reason other
than the file being absent, the write still proceeds but is **not** journaled: a guessed
pre-image would make a later undo destroy content instead of restoring it, which is worse than
having no undo for that path.

**5 — Conflict policy is skip and report; the human's own edit outranks the undo.** A path whose
current content no longer matches the recorded post-state — hash mismatch or existence mismatch —
is left exactly as it is, and the rest of the group is reverted around it. Silently overwriting
would destroy work the human did by hand, which is the failure mode an undo feature exists to
prevent. Skips are never summarized away: both the preview and the report name each skipped path
with its reason, because a silent skip reads exactly like a revert that worked. A per-path
restore that *fails* is reported the same way, carrying the refusal, rather than aborting the
whole revert.

**6 — The restore is human-initiated, so an approved out-of-workspace write is journaled and put
back like any other.** [ADR 0049](0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md)
puts its gate between the *model* and danger; `/undo` is a human typing a command about their own
workspace, and the preview — which always shows **resolved** paths, never abbreviated — is the
disclosure surface they authorize it from. Restores and removals go through the same fenced
primitives the funnel wrote through (`security.SafeWriteFile` / `SafeRemove`), so an undo
inherits every symlink and traversal refusal the original write met and can never reach further
than the write it reverses.

**7 — Two-step confirm, guarded by a generation stamp.** Bare `/undo` prints a preview note in
the transcript and `/undo confirm` executes it — no modal, because the disclosure is a list of
paths the human should be able to read at leisure, scroll back to, and compare against what they
remember asking for. Every `Record`, group materialization and `Revert` bumps a generation
counter; the preview quotes it, the confirmation hands it back, and a journal that moved in
between refuses the revert and prints a fresh preview instead. The guard is what keeps "confirm"
meaning *the step I just read* rather than *whatever is on top now*. `/undo` is **idle-only**: it
writes to the workspace, and the group it would revert is the one a running Step is still
filling.

**8 — In memory, per process, no redo.** The journal lives on the engine and dies with it. It is
live host state, never session state ([ADR
0022](0022-sessions-persist-per-turn-as-dual-representation-records.md) §8), so a resumed session
cannot revert an earlier process's writes and says so in the same breath as "nothing to undo" —
an empty answer after a resume must not be mistakable for a lost one. There is no redo and no
undoing an undo: the inverse of a revert is asking the agent again, and a redo stack doubles the
state a human has to model for a feature whose whole value is being obvious.

### Rejected alternatives

- **An on-disk journal that survives the process.** It buys the one case call 8 gives up —
  undoing after a restart — and costs a retention policy, a garbage collector, a schema to
  version, pre-image bytes written into the user's disk or config home, and a new confidentiality
  surface holding copies of source files. The undo a human reaches for is the one about the
  instruction they just gave; paying persistent-storage costs for the tail of that distribution
  is the wrong trade at this phase. Revisit on a sighting, not on a guess.
- **Reconstructing the revert from the on-disk session record.** The record holds post-images
  only — what the tool wrote, not what it replaced — so it cannot answer the question undo asks.
  Making it answer would mean storing pre-images in the record, which is the rejected on-disk
  journal wearing the session schema's clothes, and would put file bodies into a document whose
  contract ([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)) is a
  conversation.
- **A git-based revert (`git stash` / `checkout` / a shadow commit per Exchange).** It requires
  the workspace to be a repository, cannot separate the agent's edits from the human's uncommitted
  ones, would either fight or silently rewrite the user's own index and stash, and turns an undo
  into a VCS operation the human must then reason about. It also inverts the conflict policy: git
  resolves, and call 5 wants the human's edit to win untouched. apogee treats external programs
  as optional enhancements, never prerequisites ([ADR
  0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)).
- **One whole-session revert instead of a stack.** "Undo everything since the session started" is
  simpler to build and almost never what is wanted: the agent got four things right and the fifth
  wrong. The per-Exchange grain is the smallest unit that matches how the human gave the work,
  and repeated `/undo` still reaches the whole session for anyone who wants it.
- **Giving the model an undo tool.** Undo is the human's lever against the model; handing it to
  the model makes it another action to supervise. `delete_file`'s "There is no undo" therefore
  stays true from the model's perspective and its description is unchanged.

## Consequences

- The human has a real way back from a bad Exchange that does not touch what they wrote
  themselves, and a preview that names every resolved path before anything moves.
- The coverage boundary is a property of the funnel, so it holds automatically for tools added
  later — and the uncovered classes (subprocess, git checkout, MCP, embedder-registered) are a
  documented contract rather than an omission. A future MCP or subprocess undo would need its own
  capture mechanism and its own record; it is not blocked by anything decided here.
- The engine gains one long-lived in-memory structure holding pre-image bytes for the current
  process. Its size is bounded only by what the agent wrote this run — the accepted cost of call
  8, and the first thing to revisit if a long session's footprint is ever a sighting.
- `internal/undo` stays engine-, tool- and TUI-ignorant, so the bench and any future Driver can
  drive undo through the same seam the TUI does. The surface is deliberately internal: no export
  on `apogee.go` until an embedder asks for one.
- The generation guard makes the two-step protocol safe at the quiescent boundary only; the
  Driver's idle-only command gate is the enforcement, and both engine methods document it.
