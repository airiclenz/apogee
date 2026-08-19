# Undo agent changes — a per-exchange pre-image journal and the two-step `/undo` command

**Goal:** ship the `[P2] Undo all agent changes` item from `ISSUES.md`: the user can revert the
file writes the agent made, one exchange at a time, via `/undo` (preview) + `/undo confirm`
(execute). Terminal/subprocess side effects are documented as not undone.

**Date:** 2026-08-19 · **Status:** ready to execute · **sized for:** ~200k-context host

**Standing requirements:** skills: coding-standards. Any authorized deviation from item text must
land as a dated NOTES line under the item.

## Authoritative sources (ground truth — these win over any item text that disagrees)

- The write funnel and its marker: `internal/tools/path_safety.go` (`safeWriteFile`),
  `internal/security/safeio.go` (`SafeWriteFile`, `SafeCopyFileFrom`, `SafeRename`,
  `SafeRemove`), `internal/tools/workspace_scoped.go` (`workspaceScopedWriter`,
  `IsWorkspaceScopedWriter`, `WorkspaceWriteTarget`).
- `docs/design/confinement-execution-contract.md` §3 (writer marker), §4 (target disclosure).
- ADR 0002 (tools are an open extension point — third-party tools out of undo scope),
  ADR 0008 (only filesystem writes are durable/forkable; terminal/network effects are not),
  ADR 0022 §8 (live host state is not session state),
  ADR 0025 (interjections commit mid-Exchange — they do NOT open a new Exchange),
  ADR 0031 (mechanism is a library; only the command surface lives in the TUI),
  ADR 0033 (libraries must be reachable by headless runs),
  ADR 0039 (delegations fan out concurrently),
  ADR 0049 (approval permits are pinned and context-scoped).
- TUI command pattern: `internal/tui/command.go` (`commandSpecs`), `internal/tui/commandrun.go`
  (`runCommand`), `internal/tui/confine.go` (the one-file-per-verb example),
  `internal/tui/tui.go:129` (`type Engine interface`).
- Prior art note: apogee-code (the VS Code extension) does NOT implement this feature — its TDD
  (`Apogee-Code-TDD.md:164`) describes it but no code exists. There is no behavioral oracle;
  this plan is the design.

## Ratified design calls

Decided by the owner via question round, 2026-08-19:

1. **Per-exchange stack.** Each `/undo` reverts one exchange's writes, most recent first;
   repeated `/undo` walks further back. No redo.
2. **Conflict policy: skip and report.** A file whose current content no longer matches the
   agent's last recorded write (the user edited it since) is skipped; the rest is restored; the
   skipped files are listed in the report.
3. **In-memory, per process.** The journal lives on the engine and dies with the process. A
   resumed session cannot revert writes made by an earlier process. No disk store, no retention.
4. **Two-step confirm.** `/undo` prints a preview note in the transcript; `/undo confirm`
   executes it. No modal.

Author calls (plan author, 2026-08-19, each forced or strongly constrained by the sources above):

5. **Capture at the shared write funnel.** Pre-images are recorded where the writes already
   funnel (`safeWriteFile` and the copy/move/delete sites). Coverage is exactly the
   `workspaceScopedWriter` family; writes by `terminal`/`python_exec`/`run_tests` subprocesses,
   `git_branch`/`git_commit` checkout forms, MCP tools, and embedder-registered tools are out of
   scope and documented as such (ADR 0002, ADR 0008).
6. **Approved out-of-workspace writes are journaled and restored like any other.** `/undo` is a
   human-initiated act and the preview disclosing resolved paths is the authorization surface;
   ADR 0049's gate sits between the *model* and danger, not between the human and their own
   command. The preview always shows resolved paths.
7. **Stale-preview guard.** `/undo confirm` executes only when the journal generation stamped at
   the last preview still matches; otherwise it prints a fresh preview and asks again.
8. **Delegations share the parent's journal.** A sub-agent child's funnel writes join the
   current exchange group; the journal is mutex-guarded (ADR 0039 concurrency).
9. **Only successful mutations are recorded.** Pre-image bytes are captured before the write;
   the record is committed only after the mutation succeeds.
10. **Model-facing tool descriptions stay unchanged.** `delete_file`'s "There is no undo"
    remains true from the model's perspective — the model gets no undo tool; the human does.

## Out of scope (deliberate — do not re-file as gaps)

- Redo, or undoing an undo.
- Journal persistence across processes / resume (ratified call 3).
- Reverting terminal, python, test-runner, or git-checkout side effects (call 5; README will say
  so).
- MCP-tool and third-party (embedder-registered) tool writes (call 5).
- Reconstructing writes from the on-disk session record (the record holds post-images only).
- Exporting the journal on the public root surface (`apogee.go`) for embedders — demand-driven;
  the TUI drives the internal engine seam.
- Version identifiers: no VERSION/CHANGELOG-release-heading change (see the closing note).

---

## 1. The `internal/undo` journal library — ✅ DONE (2026-08-19)

NOTES (2026-08-19): `Record` takes a `Mutation` struct that adds `Root`, `Permitted` (ADR 0049) and `Perm` to the plan's record shape — `security.SafeWriteFile`/`SafeRemove` demand the fenced root and the permit, which the item authorises adapting for.
NOTES (2026-08-19): the record takes the post-image BYTES and hashes them internally rather than a caller-supplied `PostHash`, so every call site hashes the same way; the stored field is the plan's `[32]byte`.
NOTES (2026-08-19): the current-state read-back uses `security.SafeReadFile` pinned at the workspace root, or — for an approved out-of-workspace target, since security's read primitives take no permit by design — at the target's own parent directory; an unopenable fence root is read as "absent", which errs toward skipping.
NOTES (2026-08-19): unstated details settled here — constructor is `undo.New()`; `Revert` on an empty journal returns the `ErrNothingToUndo` sentinel; a per-path restore/removal that FAILS is reported as a skip carrying the refusal instead of aborting the revert; a `Revert` closes the current group so the next `Record` starts a new one.

**What:** New package `internal/undo` — one deep module, no wiring, no engine imports.

- `Journal` — mutex-guarded, holds an ordered list of exchange groups. `BeginGroup()` marks a
  boundary lazily: the group materializes on the first `Record` after it, so an exchange with no
  writes never becomes an (empty) undo step. A `Record` with no group open starts one.
- One record shape per path per group: `{Path string; Pre []byte + PreExisted bool; PostHash
  [32]byte + PostExists bool}` (SHA-256). Within a group the FIRST pre-image and the LAST
  post-hash per path win (merge on `Record`) — one entry per path per group, insertion-ordered.
  This uniformly encodes create (`PreExisted=false`), overwrite, delete (`PostExists=false`),
  and both halves of a move (source: post-absent; destination: pre-absent or pre-image when
  clobbered).
- `Generation() uint64` — increments on every `Record`, `BeginGroup` materialization, and
  `Revert`. This is the stale-preview stamp (ratified call 7).
- `Preview() (Step, bool)` — describes the top un-undone group without touching the filesystem
  beyond reads: per path, classify **restore** (pre-image exists), **delete** (pre-absent →
  file will be removed), or **skip** (current content does not match the recorded post-state —
  hash mismatch, or existence mismatch; ratified call 2). Returns the group ordinal, the
  classified paths (resolved/absolute), and the generation.
- `Revert() (Report, error)` — executes the top group in reverse insertion order with the same
  conflict-skip rule, then pops the group. `Report` carries `Restored`, `Deleted`, `Skipped`
  (path + one-line reason) lists. Restores and removals go through the symlink-safe primitives
  the funnel itself uses (`internal/security.SafeWriteFile` / `SafeRemove`) — plain `os` calls
  are not acceptable; if those signatures demand call context that does not fit here, adapt and
  leave a dated NOTES line.
- Undoing group N restores each path to group N's pre-image, which equals group N−1's post-state
  — so walking the stack back through a file touched in several exchanges passes the conflict
  check at every step. This property is load-bearing; test it explicitly.

**Files:** `internal/undo/journal.go`, `internal/undo/journal_test.go`, `internal/undo/doc.go`

**Tests:** create-then-undo deletes the file; delete-then-undo restores bytes; overwrite-then-undo
restores the pre-image; move encoded as two records round-trips; conflict (hand edit after agent
write) is skipped with a reason while siblings restore; the multi-exchange same-file walk-back
described above; lazy group materialization (BeginGroup with no writes adds no step); generation
increments; concurrent `Record` from multiple goroutines is race-clean.

**Acceptance:** `go build ./internal/undo && go test ./internal/undo -count=1 -race`

**Commit:** `feat(undo): add the per-exchange pre-image journal`

---

## 2. Engine owns the journal; the main write funnel records pre-images — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the journal reaches the funnel through a new context seam, `internal/undo/context.go` (`WithJournal` / `FromContext`) — the same shape `ConfinementBox` and the write-escape permit already use, which the item's "the same way the ConfinementBox already reaches a tool call" text asks for. It is the threading file the `undo` package needed; item 1's "no wiring" stands for `journal.go` itself, which is untouched.
NOTES (2026-08-19): the journal is installed on the execution context in `executeTool` (dispatch.go) for EVERY call rather than only for workspace-scoped writers — that is what keeps the coverage boundary the funnel itself rather than a second list of tools kept in dispatch.
NOTES (2026-08-19): a record's `Path` is the path the argument NAMES (root-joined and cleaned), not its symlink-resolved twin, because `internal/security`'s fenced primitives relativise lexically against the workspace root — a resolved path would be refused as an escape on any host whose root is reached through a symlink (macOS `/tmp`). The approved out-of-workspace escape is the one exception and records the resolved permitted target, recognised by exactly the test `escapeTargetPin` uses.
NOTES (2026-08-19): a pre-image that cannot be READ for any reason other than the file being absent is not journalled at all (the write still proceeds) — recording a guessed pre-image would make a later undo destroy content instead of restoring it.
NOTES (2026-08-19): `Agent.journal` is documented as optional (nil = nothing recording) and the one `BeginGroup` site guards for nil, so a hand-built `Agent` value cannot panic in the loop.

Depends on item 1.

**What:** The engine (`internal/agent`) constructs one `undo.Journal` at build time and owns it
for the engine's lifetime (in-memory only — ratified call 3; consistent with ADR 0022 §8: it is
live host state, never session state).

- **Exchange boundary:** call `BeginGroup()` at the seam where a new Exchange opens — the loop
  site that stamps `ExchangeStart` / flips `InExchange` when a new user input commits
  (`internal/agent/state.go:47` fields). Interjections do NOT open a group (ADR 0025 — they
  commit mid-Exchange); only a genuine new-Exchange open does.
- **Capture:** thread the journal handle from the engine to the write funnel the same way the
  `ConfinementBox` already reaches a tool call (the dispatch seam around
  `internal/agent/dispatch.go:417`), so `safeWriteFile` (`internal/tools/path_safety.go:62`)
  records: read the target's current bytes (or absent) before mutating, and commit the record —
  with the SHA-256 of the bytes written — only after the mutation succeeds (ratified call 9).
  This covers `write_file`, `edit_existing_file`, `single_find_and_replace`, and
  `multi_find_and_replace` in one place. Out-of-workspace permitted writes are journaled too
  (ratified call 6).
- A funnel write that fails records nothing.

**Files:** `internal/agent/loop.go`, `internal/agent/dispatch.go`, `internal/agent/state.go`,
`internal/tools/path_safety.go`, plus the one construction/threading file each package needs
(locate at implement time; keep the touched set minimal) and their test files.

**Tests:** each of the four content-writing tools produces exactly one journal record per path
with correct pre-image and post-hash; a failed write records nothing; a new user input opens a
new group while an interjection does not (agent-level test at the state seam).

**Acceptance:** `go build ./... && go test ./internal/agent ./internal/tools -count=1`

**Commit:** `feat(agent): journal pre-images for every funnel write, grouped per exchange`

---

## 3. Copy, move, and delete capture; delegations share the journal

Depends on item 2.

**What:**

- `copy_file` (`internal/tools/file_ops.go`): journal the destination (pre-image when it
  clobbers with `overwrite:true`, pre-absent otherwise).
- `move_file` (`internal/tools/file_ops.go`, `move()`): two records — source (pre-image bytes,
  post-absent) and destination (pre-absent or pre-image, post-hash of the moved content). Both
  the rename fast path and the copy+remove fallback must journal identically.
- `delete_file` (`internal/tools/delete_file.go`): pre-image bytes, post-absent.
- **Delegations:** the child engine a `sub_agent` call constructs (the delegation-prepare seam
  near `internal/agent/dispatch.go:236`) receives the PARENT's journal instance, so child writes
  join the current exchange group (ratified call 8). The journal mutex covers the concurrent
  fan-out (ADR 0039).
- Amend the stale "no undo" narration in `internal/tools/doc.go` (`:99`) to name the journal
  capture seam. The model-facing `delete_file` description text stays unchanged (ratified
  call 10).

**Files:** `internal/tools/file_ops.go`, `internal/tools/delete_file.go`,
`internal/tools/doc.go`, `internal/agent/dispatch.go`, plus the matching test files.

**Tests:** copy/move/delete each produce the record shapes above; a move undone via the journal
restores the source and removes the destination; a delegation child's write lands in the
parent's current group.

**Acceptance:** `go test ./internal/tools ./internal/agent -count=1`

**Commit:** `feat(tools): journal copy, move and delete pre-images and share the journal with delegations`

---

## 4. The engine surface the driver calls

Depends on item 2.

**What:** Two methods on the concrete engine type behind the TUI's `Engine` interface
(`internal/tui/tui.go:129`) — the same surface that carries `ConfineToWorkspace` /
`SetConfineToWorkspace`:

- `UndoPreview() (undo.Step, bool)` — delegates to the journal; `false` when there is nothing
  to undo.
- `UndoRevert(generation uint64) (undo.Report, error)` — refuses with a typed sentinel error
  when the generation no longer matches the journal (ratified call 7), otherwise reverts the
  top group.

Both are documented as valid at the quiescent boundary only; the TUI's idle-only command gate is
the enforcement. Add both methods to the `Engine` interface and to every test fake that
implements it. This stays on the internal surface — no `apogee.go` root export (out of scope
list).

**Files:** `internal/tui/tui.go`, the engine's public-methods file in `internal/agent` (the file
defining `SetConfineToWorkspace` — locate at implement time), and the `internal/tui` test fakes
the interface change breaks.

**Tests:** preview/revert delegate to the journal; a stale generation returns the sentinel and
reverts nothing.

**Acceptance:** `go build ./... && go test ./internal/agent ./internal/tui -count=1`

**Commit:** `feat(agent): expose undo preview and revert on the engine surface`

---

## 5. The `/undo` command in the TUI

Depends on item 4.

**What:** One new verb, following the `/confine` pattern end-to-end:

- `commandSpecs` row `undo` in `internal/tui/command.go` — alphabetical placement (the existing
  `TestCommandSpecsReadAlphabetically` enforces it), `takesArgs: true`, `whileRunning: false`
  (idle-only: it mutates the workspace).
- Grammar: bare `/undo` = preview; `/undo confirm` = execute; any other argument = usage-error
  note. Route via a new `case "undo":` in `runCommand` (`internal/tui/commandrun.go`).
- `internal/tui/undo.go`, `runUndo`: synchronous, nil Cmd, transcript notes via `addNote` (the
  `confine.go` shape). Preview: one note listing the exchange ordinal and the classified paths —
  restore / delete / skip with reasons, resolved paths — ending with an instruction to run
  `/undo confirm`; stash the preview's generation on the model. Confirm: call
  `UndoRevert(generation)`; on the stale sentinel, print a fresh preview note saying the state
  changed and ask to confirm again; on success, print the report note (counts + the skipped
  list with reasons). Nothing to undo → a plain note saying so, and that the journal is
  per-process.
- Name the new file in `internal/tui/doc.go`'s narration (`TestDocMapNamesEveryFile` fails
  otherwise).

**Files:** `internal/tui/command.go`, `internal/tui/commandrun.go`, `internal/tui/undo.go`,
`internal/tui/undo_test.go`, `internal/tui/doc.go`

**Tests:** parse forms (bare, `confirm`, junk arg); preview note content and generation stash;
confirm happy path; stale-generation re-preview path; nothing-to-undo note; the command refuses
while the model works (idle-only gate).

**Acceptance:** `go test ./internal/tui -count=1`

**Commit:** `feat(tui): add the two-step /undo command`

---

## 6. ADR 0051, README, CONTEXT.md, and the ISSUES.md close

Depends on items 3 and 5.

**What:**

- **ADR 0051** — `docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md` (house
  ADR format): record the ratified calls — per-exchange stack, in-memory per-process, funnel
  capture and the exact coverage boundary (the `workspaceScopedWriter` family; terminal /
  python / test-runner / git-checkout / MCP / third-party writes out), conflict-skip, the
  human-initiated restore including permitted out-of-workspace paths, the two-step confirm with
  the generation guard, no redo — with the rejected alternatives (on-disk journal, git-based
  revert, whole-session one-shot) and why.
- **README.md** — one `/undo` row in the command table (`README.md:253` area, idle-only `—`
  tag), plus a short prose paragraph in the same section: what `/undo` covers, the per-exchange
  stack, the skip-on-conflict rule, and explicitly what is NOT undone (subprocess/terminal
  side effects, git working-tree mutations, MCP/third-party tools, writes from an earlier
  process).
- **CONTEXT.md** — a short "Undo journal" domain-term entry beside the Session/Exchange terms
  (per-exchange pre-image journal, engine-owned, per-process).
- **ISSUES.md** — remove the `[P2] Undo all agent changes` entry (this plan ships it; the
  CHANGELOG records the close via the run's normal sidecar convention).

**Files:** `docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md`, `README.md`,
`CONTEXT.md`, `ISSUES.md`

**Tests:** none (docs).

**Acceptance:** `test -f "docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md" && grep -q "/undo" README.md && grep -qi "undo journal" CONTEXT.md && ! grep -q "Undo all agent changes" ISSUES.md`

**Commit:** `docs(undo): record ADR 0051 and document /undo`

---

## Suggested version bump

Minor (new user-facing feature: the `/undo` command and the journal beneath it). No item changes
VERSION or cuts a release — whether and when to bump is the owner's call.
