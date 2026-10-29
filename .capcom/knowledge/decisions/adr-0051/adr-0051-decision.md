---
title: ADR 0051 decision
---

**Undo is a per-Exchange stack of pre-images, captured at the shared write funnel, held in
memory for the life of the process, and applied by a human through a two-step `/undo`.** The
mechanism is `internal/undo` — a library that imports `internal/security` and the standard
library and nothing else, so a headless Driver can reach it exactly as the TUI does (ADR
0031,
ADR 0033).

**1 — The unit of undo is the Exchange, and the stack walks back one at a time.** A human who
says "undo that" means one instruction they gave, not one of the fourteen writes it took —
`CONTEXT.md`'s Exchange is already that unit, so the journal groups by it. Each `/undo` reverts
the most recent un-reverted group; repeating it walks further back. Groups materialize **lazily**
— the boundary is marked when an Exchange opens and the group exists only from its first write —
so an Exchange that wrote nothing never becomes a step the human has to walk past. An
Interjection opens no group: it commits *mid*-Exchange
(ADR 0025), and a remark that
splits one instruction into two undo steps would make the stack lie about what the human asked
for. For the same reason a delegated sub-agent writes into its **parent's** current group and
opens none of its own: one `/undo` takes back the whole instruction however wide the fan-out
(ADR 0039
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
embedder registers (ADR 0002
— tools are an open extension point, so no closed undo contract can be promised over them; ADR
0008 for the subprocess half).
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
back like any other.** ADR 0049
puts its gate between the *model* and danger; `/undo` is a human typing a command about their own
workspace, and the preview — which always shows the journal's **recorded absolute** paths, never
abbreviated (see the 2026-08-19 amendment for which absolute path a record carries) — is the
disclosure surface they authorize it from. Restores and removals go through the same fenced
primitives the funnel wrote through (`security.SafeWriteFile` / `SafeRemove`), so an undo
inherits every symlink and traversal refusal the original write met and can never reach further
than the write it reverses.

> **Amended 2026-09-22 (spelling only; ADR 0049 amend-0814).** The free `security.SafeWriteFile` /
> `SafeRemove` mutators are gone: the root and the permit now travel together as one
> `security.Fence`, each record carries the Fence its write ran under (`undo.Mutation.Fence`), and a
> restore or removal goes back through that Fence's `WriteFile` / `Remove`
> (`internal/security/fence.go`). The guarantee is unchanged.

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
live host state, never session state (ADR
0022 §8), so a resumed session
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
  contract (ADR 0022) is a
  conversation.
- **A git-based revert (`git stash` / `checkout` / a shadow commit per Exchange).** It requires
  the workspace to be a repository, cannot separate the agent's edits from the human's uncommitted
  ones, would either fight or silently rewrite the user's own index and stash, and turns an undo
  into a VCS operation the human must then reason about. It also inverts the conflict policy: git
  resolves, and call 5 wants the human's edit to win untouched. apogee treats external programs
  as optional enhancements, never prerequisites (ADR
  0042).
- **One whole-session revert instead of a stack.** "Undo everything since the session started" is
  simpler to build and almost never what is wanted: the agent got four things right and the fifth
  wrong. The per-Exchange grain is the smallest unit that matches how the human gave the work,
  and repeated `/undo` still reaches the whole session for anyone who wants it.
- **Giving the model an undo tool.** Undo is the human's lever against the model; handing it to
  the model makes it another action to supervise. `delete_file`'s "There is no undo" therefore
  stays true from the model's perspective and its description is unchanged.
