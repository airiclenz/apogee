---
Status: accepted
---

# Undo is a per-Exchange snapshot pair in a session-owned git object database

## Context

[ADR 0051](0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md) built `/undo` as a
per-Exchange stack of pre-images captured at `internal/tools`' write funnel, held in memory for
the life of the process. Two of its calls were deliberate cost decisions taken at that phase, and
both were written down as revisitable: decision 3 makes the funnel the coverage boundary, so a
file rewritten by `terminal`, `python_exec`, `run_tests`, a git-checkout form, an MCP server or an
embedder-registered tool is outside `/undo` by construction; decision 8 keeps the journal in
memory with no redo, so a relaunch — or a crash, or the end of a headless run — takes the way back
with it. Its rejected-alternatives list closed the git door on four specific objections.

The 2026-09-06 contender assessment (`docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md`,
gap 1) is the sighting ADR 0051 asked for. Rivals ship exactly the missing half and ship it the
way that answers the objections: opencode snapshots the work-tree into a **separate** git object
database — never the user's index, stash or branches — and offers `/undo` and `/redo` across
restarts. The same gap has a second face inside apogee: an Auto [Firing](../../CONTEXT.md#identity-and-shape)
run by the daemon writes files with nobody watching and, until now, no revert surface at all (bead
`apogee-kk0.7`).

The owner resolved the design by question round on 2026-09-06; this record ratifies those calls and
the ones the plan's regression check settled on the same day. It supersedes part of ADR 0051.

## Decision

**Undo is a pair of tree snapshots per Exchange, taken into a git object database the session owns
outside the workspace, indexed by a `journal.json` that survives the process.** The funnel journal
of ADR 0051 does not go away — it becomes the second of two capture paths, and the authority
wherever both see a path.

**1 — The store is session-owned, git-plumbing-backed, and never inside the workspace.**
`~/.apogee/snapshots/<session-id>/` holds a bare `GIT_DIR`, a private index file beside it, and
nothing else; the workspace is passed as the work-tree for the duration of a capture. Nothing is
ever written inside the workspace — no repository of ours, no index, no ignore file, no lock, no
stray object. The user's own repository, index, stash, branches and reflog are untouched and
unread, a workspace that is no repository at all snapshots identically, and `git status` in the
user's tree never shows apogee's state because there is none to show.

**2 — git absent is a supported configuration, and `undo-snapshots: false` is the same state.**
[ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md) decision 2 —
every external program is a runtime-detected optional enhancement — binds here as it binds
everywhere. Where git is missing or unusable, ADR 0051's in-memory funnel journal stays exactly as
it is and `/undo` names the reason in the same breath as what it can still reach, so a thinner
answer is never mistakable for a broken one. The config key `undo-snapshots` (named after
`tool-call-repair`, as `tool-call-salvage` is) set to `false` behaves as git absent: same fallback,
same sentence.

**3 — Two capture points per Exchange, both lazy, and a no-diff Exchange leaves no step.** The
**pre** snapshot is taken lazily, immediately before the first write-capable tool call of a depth-0
[Exchange](../../CONTEXT.md#turns-and-stepping); the **post** snapshot at Exchange close, and only
where a pre exists. An Exchange that never reaches a write-capable call costs nothing and never
becomes a step the human has to walk past, and an Exchange whose two trees are identical leaves no
step either. ADR 0051 decision 1 is unchanged and now governs both paths: an
[Interjection](../../CONTEXT.md#turns-and-stepping) opens no pair, and a delegated sub-agent's
writes fall inside its parent's pair, so one `/undo` still takes back one instruction however wide
the fan-out.

**4 — Revert is diff-scoped, and skip-and-report is untouched.** A revert writes only the paths
that **differ between that Exchange's pre and post trees**. Everything else in the workspace —
including every file the human changed by hand outside the diff — is not read, not written, not
considered. Within that scope ADR 0051 decision 5 holds verbatim: a path whose current content no
longer matches the post snapshot is left exactly as it is and named, with its reason, in both the
preview and the report, and a per-path restore that fails is reported the same way rather than
aborting the rest. No merge is ever run; git resolves nothing, and the human's own edit still
outranks the undo.

**5 — `journal.json` beside the objects is the index, and it holds one session's Exchanges.** It
carries the ordinals, the generation counter, the pre and post tree ids per Exchange, and the
workspace path the session ran against. On resume the store is opened **only when that recorded
workspace matches the workspace the resumed run has**: a session resumed against a different tree
reverts nothing and says so, rather than writing one tree's paths over another's.

**6 — `/redo` exists, under `/undo`'s own protocol.** Two-step confirm, generation stamp,
idle-only — ADR 0051 decision 7 governs both commands. A revert pushes the Exchange onto a redo
stack; `/redo` re-applies the post tree over the same diff scope with the same skip-and-report. The
stack is cleared by the next Exchange that actually writes — at group materialisation, the same
moment a group comes into existence — because a redo across a newer write would re-apply an old
tree over work the human just asked for. This supersedes ADR 0051 decision 8's "no redo": a redo
stack is cheap to model once the steps are durable and named, and the objection that killed it was
the doubled state of an ephemeral feature.

**7 — Rotate and Activate reopen the journal under the session id now in force.** `/new` and
`/clear` rotate the session id; `/sessions` resume activates another. In every case the journal is
reopened under the new id, and the previous session's Exchanges stay revertible — by resuming that
session, or unattended with `apogee undo <old-id>`. This supersedes the rule that the journal
**survives** `/clear` (`internal/agent/agent.go`, `ClearContext`'s doc comment and ADR 0051
decision 8's process scope): that rule existed because an in-memory journal keyed to nothing had
nowhere else to live. A store keyed by session id does, so the boundary the human drew by typing
`/clear` is the boundary the journal keeps, and nothing is lost by keeping it.

**8 — Unattended revert is a top-level verb.** `apogee undo <session-id> [confirm]` runs the same
preview-then-confirm outside any TUI. The written-files report a headless run and a daemon Firing
already print ends with the exact command for that run's own session, so the answer to "the Auto
Firing wrote the wrong thing" is a line the operator can copy rather than a manual `git checkout`
they must compose. This is what closes bead `apogee-kk0.7`.

**9 — An approved out-of-workspace write stays funnel-journaled and per-process.** A snapshot's
work-tree is the workspace, so a write executed under an
[ADR 0049](0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md)
permit outside it is invisible to the tree pair. ADR 0051 decision 6 keeps it: its pre-image is
captured at the funnel as today, is reverted through the same permit-pinned path, and dies with the
process. The persistent store therefore never holds a byte from outside the tree the user pointed
apogee at, which is what keeps its confidentiality surface bounded and legible.

**10 — Coverage never narrows.** Every path the funnel journals keeps its **full pre-image**, and
the snapshot diff only ever **adds** paths the funnel never saw — the subprocess, git-checkout, MCP
and embedder-registered writes ADR 0051 decision 3 documented as out of scope. Where both paths see
one file, the funnel's pre-image is the authority: it was read at the mutation site, with no
pipeline between it and the bytes.

**11 — `Capture` ignores the operator's global `core.excludesFile`.** A snapshot that silently
dropped whatever a user's personal global ignore file happens to list would make undo lie about its
own coverage, and would make that lie machine-specific. The capture runs with the global excludes
file disabled, so what a snapshot covers is a property of the workspace the human can see, not of a
dotfile in their home directory.

**12 — The residue RULE: what git's add pipeline transforms or refuses is outside the snapshot, and
the funnel pre-image stays authoritative for it.** A rule, not a list, because the boundary is
simply *what `git add` would do*: a path a filter driver rewrites (a git-lfs pointer stored in
place of the bytes), a nested repository, or a commit-less one, that the pipeline will not descend into, and a
workspace `.gitignore` match are all outside — and so is any filter or configuration nobody
anticipated, which is the point of stating it as a rule. Nothing in that residue loses cover it had
before: decision 10 stands, and for every such path the funnel journal is what `/undo` uses.

**13 — State class: persisted host state, keyed by session id, outside the session record.**
[ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md) §8 holds unchanged —
the session record is a conversation and gains no file bodies here. The store is a sibling of the
per-session scratch dir instead: under `~/.apogee/`, keyed by the session id, invisible to the
record's schema. GC is that sibling's too — the startup scratch sweep's age rule removes an
untouched store, and a store goes with its session record when session retention drops it.
[ADR 0059](0059-a-console-is-live-host-state-the-model-drives-across-turns.md) §1's "per process,
dies with it" is **not** superseded and not amended by this record: that is the Console's own class,
and a Console is a live process that cannot be persisted at all. This record yields to it — where
0059 cites the undo journal as a fellow member it is citing ADR 0051's journal, which still exists
and still behaves that way wherever decision 2's fallback is in force.

### What this keeps and what it supersedes in ADR 0051

Superseded: **decision 3** (the funnel is the coverage boundary) — the funnel remains a capture
path and an authority, but the boundary is now the workspace tree; **decision 8** (in memory, per
process, no redo) — decisions 5, 6, 7 and 13 above replace it whole; and the **rejected git-based
revert** (`0051:136`), whose four objections this shape answers one by one: it needs no repository
in the workspace because it brings a bare object database of its own (decision 1); it cannot fight
the user's index or stash because it never touches them (decision 1); it separates the agent's
edits from the human's by scoping the revert to the Exchange's own diff (decision 4); and it never
inverts the conflict policy because no merge runs and the post-state check still lets the human's
edit win (decision 4). ADR 0042's optional-enhancement objection is answered by decision 2 rather
than dismissed.

Kept verbatim: **decision 1** (the Exchange is the unit, groups materialize lazily, an Interjection
opens none, a sub-agent writes into its parent's group), **decision 2** (the funnel record's shape),
**decision 4** (only successful mutations recorded; an unreadable pre-image records nothing),
**decision 5** (skip and report), **decision 6** (an approved escape is journaled and put back like
any other), **decision 7** (two-step confirm, generation stamp, idle-only), and both of its
amendments (the named-path identity of a record, and the funnel-is-a-pair capture property with its
two tests).

### Rejected alternatives

- **A pure-Go snapshot store.** Re-implements an object database, its packing and its diff, and
  every byte of that is ours to maintain and to get right on three platforms. The machine without
  git already has an answer (decision 2), and it is the answer that ships today.
- **The object database inside the workspace** (a second `.git`, or `.apogee/`). It puts apogee's
  state into the user's diffs, their ignore file, their backups and eventually their commits, and
  it makes an undo store something a `rm -rf` in the workspace can take. Decision 1 exists to
  refuse this.
- **Copying whole file trees per Exchange.** Content-addressed objects deduplicate across a
  session; a copy multiplies the workspace by the number of Exchanges that wrote.
- **Persisting the out-of-workspace pre-images too.** It would put bytes from outside the tree the
  user pointed apogee at into a durable store under their config home — a confidentiality surface
  with no bound the user can see. Decision 9 keeps those per-process.
- **A checkpoint that restores the conversation as well as the tree.** Restoring a conversation is
  the session record's job and `/sessions` already does it (ADR 0022). Folding the two gives one
  command two blast radiuses, and the human has to model which half a given restore moved.
- **Giving the model an undo tool.** ADR 0051's refusal stands unchanged: undo is the human's lever
  against the model, and `delete_file`'s "There is no undo" stays true from where the model sits.

## Consequences

- `/undo` survives a relaunch, a crash and the end of a headless run, and it reaches the writes of
  `terminal`, `python_exec`, MCP servers and git checkout forms that ADR 0051 documented as
  uncovered. `/redo` exists for the first time.
- apogee gains a durable store holding copies of workspace file contents under `~/.apogee/`. Its
  bound is the workspace (decision 9), its lifetime the session record's plus the scratch sweep's
  age rule (decision 13), and both are the first things to revisit if the footprint is ever a
  sighting.
- Undo becomes a two-tier feature: full on a machine with git, ADR 0051's behaviour without it.
  Every test that covers undo gains a git-absent arm, and the sentence `/undo` prints in that state
  is part of the contract, not an error message.
- The engine keeps two capture paths where it had one. That is not redundancy: decision 10 gives
  each an area the other cannot see, and decision 12 names the residue where only the funnel can
  answer.
- A daemon Firing and a headless run get a revert surface they never had — one command, printed
  where the run reports what it wrote (bead `apogee-kk0.7`).
- A Console's lifetime is untouched: ADR 0059 §1 keeps its own class, and this record claims no
  authority over it.
