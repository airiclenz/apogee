# Plan: copy_file source honors extra read-only roots

- **Goal:** `copy_file` can copy FROM a file under a configured extra read-only root
  (e.g. the skills library `~/.apogee/skills`, symlink or not) INTO the workspace. Today
  its source is fenced to the workspace like its destination, so a skill-prescribed copy
  such as `~/.apogee/skills/security-audit/resources/methodology.md →
  docs/skill-runs/...` is refused with the path-escape error even though `read_file` on
  the same source succeeds. Evidence: session
  `~/.apogee/sessions/20260812T120824Z-55b661f1.json` — `list_dir`/`read_file` on
  `/root/.apogee/skills/security-audit/...` all succeed, all five refusals are
  `copy_file` calls (`error: security: path resolves outside the workspace root`).
- **Date:** 2026-08-12
- **Status:** not started
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - `docs/plans/archived/2026-08-12 - 00 - skill-read-roots-plan.md` — the read-roots
    seam this plan extends; its ratified calls (extra roots are READ-ONLY, absolute-path
    access only, live per call, per-root `os.Root` fencing) all still bind.
  - `internal/tools/path_safety.go` — `readScope` (resolve/open/readBounded, matched-root
    contract) and the `statInRoot` helper.
  - `internal/tools/file_ops.go` — `copy_file`/`move_file`/`delete_file`, the
    `checkFileOpsPaths` gate, the family doc-comment block (lines 13–37).
  - `internal/security/safeio.go` — `SafeCopyFile` (line 275) whose staging/rename/mode
    guarantees the new primitive must preserve.
  - ADR 0012 + `docs/design/confinement-execution-contract.md` §3 — the
    `workspaceScopedWriter` destination classification, which this plan must NOT change.
- **Ratified design calls** (owner, 2026-08-12, this session):
  1. Fix scope = `copy_file`'s SOURCE only. The destination stays workspace-fenced, and
     every other write tool stays fully workspace-fenced — `move_file` and `delete_file`
     are explicitly unchanged (a move removes its source: a write, and extra roots are
     read-only). `present_document` widening was offered and deselected.
  2. NO dedicated `get-skill`/`get_skill` tool. The access path for skill bundled files
     is the injected `files:` line plus the four read tools (+ this fix); a skills-aware
     tool would duplicate them, bloat context, and violate the read-roots plan's ratified
     call that the tool layer never learns about skills.
- **Author-resolved calls** (plan author, 2026-08-12 — routine, binding):
  - Mechanism: a two-root primitive `security.SafeCopyFileFrom(srcRoot, srcInput,
    dstRoot, dstInput)`; `SafeCopyFile(root, s, d)` becomes a delegation with both roots
    equal. Each end is fenced by an `os.Root` pinned at ITS root.
  - Source resolution mirrors the `readScope` contract exactly: workspace first; extra
    roots tried only for ABSOLUTE input; relative sources keep resolving against the
    workspace alone; all-refuse ⇒ the existing uniform escape message, unchanged.
  - The `readScope` doctrine comment (`path_safety.go:171` — "No write helper takes a
    readScope and none may") gains an explicit carve-out: `copy_file`'s SOURCE resolution
    is the one sanctioned use, because a copy's source is a read; the write half of any
    tool never takes one.
  - Approval/dispatch classification is untouched: `workspaceWriteTarget` keeps
    resolving the DESTINATION (`destinationArgWriteTarget`), which is where the write
    lands regardless of source root.
- **skills:** coding-standards
- **Out of scope:** `get_skill` tool (denied above); `present_document`/`view_diff`
  widening (deselected); `move_file`/`delete_file`; any change to
  `workspaceScopedWriter`/dispatch/approval classification; the in-flight gate-remedy
  work (`docs/plans/2026-08-12 - 01 - gate-remedy-and-fail-closed-plan.md` and its dirty
  files `internal/agent/dispatch.go`, `internal/agent/resolution.go`,
  `internal/domain/approval.go`, `internal/tui/approval.go` — do not touch them); write
  access to extra roots in any form; version bumps (see closing note).

Every item: add a line under `[Unreleased]` in `CHANGELOG.md` describing the change, and
run the build sanity check before reporting. Any authorized deviation from item text must
land as a dated NOTES line under the item.

## 1. Two-root copy primitive in internal/security — ✅ DONE (2026-08-12)

NOTES (2026-08-12): beyond the item's named doc comment, the `internal/security/doc.go` package
map was updated too — its safeio.go line listed the primitives and claimed each pins "the
workspace root", which the two-root form falsifies.

**What:** In `internal/security/safeio.go`, add `SafeCopyFileFrom(srcRoot, srcInput,
dstRoot, dstInput string) error`: the source is resolved (`rootRelative`), opened and
statted through an `os.Root` pinned at `srcRoot`; the destination keeps every existing
`SafeCopyFile` guarantee through an `os.Root` pinned at `dstRoot` — parent creation
inside the fence, staging file in the destination's parent, rename as the last step,
staging removed on failure, mode taken from the source descriptor, non-regular source
refused. A path escaping its OWN root at either end returns an error wrapping
`ErrPathEscape`; the existing message wording stays uniform. Rewrite `SafeCopyFile` as a
delegation to `SafeCopyFileFrom(root, srcInput, root, dstInput)` — byte-identical
behavior. Update the `SafeCopyFile` doc comment ("a single os.Root pinned at root"
becomes the two-root contract on the new function, with the one-root form documented as
the equal-roots special case).

**Tests:** in the package's existing safeio test file: a copy from a file under temp
root A to a destination under temp root B lands with the source's mode; a source
escaping `srcRoot` (traversal and symlinked-component) is refused with `ErrPathEscape`
and nothing is written; a destination escaping `dstRoot` likewise; a non-regular source
refused; equal-roots delegation — all existing `SafeCopyFile` tests keep passing
unmodified.

**Acceptance:** `go build ./...` && `go test ./internal/security/ -run 'SafeCopy' -v`

**Commit:** `feat(security): two-root SafeCopyFileFrom primitive for cross-root copies`

## 2. copy_file resolves its source over the read scope — ✅ DONE (2026-08-12)

NOTES (2026-08-12): took the item's "equivalent factoring" latitude for the pre-flight — instead of a
`checkCopySourcePath(args, scope)`, `checkFileOpsPaths` became a one-line delegation to a new
`checkFileOpsPathsFrom(args, sourceRoot, destinationRoot)` (the same equal-roots shape
`SafeCopyFile`/`SafeCopyFileFrom` took in item 1), so move_file's behaviour and every refusal string
stay shared rather than duplicated; `CopyFile.Execute` picks the matched source root ONCE and pins
both the stat and the copy to it. Beyond the doc comments the item names, two more it falsifies were
updated in place: the `HostTools.ExtraReadRoots` field contract (`internal/tools/registry.go`) and
`domain.Config.ExtraReadRoots` (`internal/domain/config.go`), both of which stated that only read
tools receive the roots.

Depends on item 1.

**What:**
- `internal/tools/file_ops.go`: `CopyFile` gains a `scope readScope` field;
  `NewCopyFile(root string, extraReadRoots func() []string)` builds it (mirroring
  `NewReadFile`). `Execute` resolves the SOURCE's matched root via the scope (the
  workspace-first / absolute-only order `readScope` already implements) and calls
  `security.SafeCopyFileFrom(matchedSrcRoot, args.Source, t.root, args.Destination)`.
  The destination path and `workspaceWriteTarget` are untouched.
- The pre-flight check: `checkFileOpsPaths` is shared with `move_file`, whose both ends
  must stay workspace-fenced — split the SOURCE stat for the copy path (e.g. a
  `checkCopySourcePath(args, scope)` used by `copy_file` while `move_file` keeps
  `checkFileOpsPaths` verbatim, or an equivalent factoring): the copy's source stat goes
  through the scope's matched root (`statInRoot(source, matchedRoot)`), everything else
  (destination stat, overwrite refusal, directory refusals, message wording) is
  byte-identical for both tools.
- `internal/tools/registry.go`: `DefaultToolsWithHost` passes `host.ExtraReadRoots` to
  `NewCopyFile`; update the doc comment at `registry.go:127` (the roots now feed the four
  read-only tools AND `copy_file`'s source). Update all other `NewCopyFile` callers
  (`cmd/apogee/wire_tools.go` if it constructs directly, tests).
- `copy_file`'s tool `description` gains one clause, e.g. "; the source may also be an
  absolute path under a configured read-only root (such as the skills library) — the
  destination must stay within the workspace". `move_file`/`delete_file` descriptions
  unchanged.
- Doc comments that this item falsifies, updated in place: the file_ops.go family block
  ("a source outside the workspace is refused by the same os.Root the copy reads
  through", lines 22–25) now states the source may match an extra READ root while the
  destination classification still bounds the only write; the `path_safety.go:171`
  doctrine line gains the ratified carve-out (author-resolved call above).

**Tests:** in `file_ops_test.go` (+ `path_safety_test.go` if the factoring lands there):
copy_file copies a file from a temp extra root into the workspace by absolute source
path (content and mode verified); a RELATIVE source naming an extra-root file is refused
(workspace-only resolution for relative input); a destination under an extra root is
refused with the uniform escape message (read-only roots take no writes); a source under
no root keeps today's refusal; overwrite/existing-destination behavior unchanged;
move_file with a source under an extra root is STILL refused; `workspaceWriteTarget`
still classifies the destination for a call whose source is under an extra root.

**Acceptance:** `go build ./...` && `go test ./internal/tools/ -run 'CopyFile|MoveFile|FileOps|ReadScope' -v`

**Commit:** `feat(tools): copy_file source honors configured read-only roots`

---

**Suggested version bump:** one micro bump once both items land (house convention:
VERSION micro-bumps per shipped feature) — suggestion only; whether and when to bump is
the owner's call.
