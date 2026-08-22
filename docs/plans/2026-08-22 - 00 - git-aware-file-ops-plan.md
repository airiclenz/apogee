# Git-aware file operations — stage renames and deletions of tracked files

- **Goal:** `move_file` and `delete_file` reproduce `git mv` / `git rm --cached`-style index
  updates automatically: when the workspace is a git worktree and the affected source file is
  tracked, the tool stages the rename (or deletion) after the filesystem operation succeeds.
  Models — especially small ones — get correct rename tracking for free, with zero prompting;
  an untracked file or a non-repo workspace leaves behavior byte-identical to today.
- **Date:** 2026-08-22
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/tools/git.go` — the hardened git-subprocess conventions every new git invocation
    MUST reuse: `gitProgram` (resolution + exec fence + graceful absence), `runGit` (scrubbed
    env, hardening options, `gitTimeout`). No raw `exec.Command("git", ...)` anywhere.
  - `internal/tools/file_ops.go`, `internal/tools/delete_file.go` — current tool behavior,
    pinned commit `276fadb8`.
  - ADR 0051 (undo journal) — the undo interplay documented in item 1.
  - `docs/design/confinement-execution-contract.md` §3 — the write-tool disposition these tools
    already carry; this plan changes nothing about classification or fencing.
- **Ratified design calls** (owner, 2026-08-22, via chat + AskUserQuestion):
  1. **Scope:** `move_file` and `delete_file` gain staging; `copy_file` is excluded (staging a
     brand-new untracked destination would stage content the user never committed).
  2. **Staging is a deliberate side effect** — exactly the `git mv` contract; no separate
     "suggest only" mode.
  3. **Disclose in the result:** a successful stage appends a note to the tool result so the
     model knows the index changed.
  4. **Staging failure never fails the call:** the file operation stands; the result appends
     `(git staging skipped: <reason>)`.
  5. **Undo interplay = accept + document:** `/undo` restores worktree bytes only; a leftover
     staged entry is visible in `git status` and harmless. Documented, not engineered around.
  6. **No config toggle.** Always on when the source is tracked.
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item text
  must land as a dated NOTES line under the item.
- **Out of scope:** `copy_file` staging; intercepting/rewriting bash `mv` in the exec tool (a
  Mechanism — rejected: brittle, and content-based rename detection recovers those at
  `git add -A` time); extending the undo journal to revert index operations; any config flag;
  any version bump (see closing note).

## 1. `stageGitPaths` helper — best-effort index update for tracked files

**What:** Add `internal/tools/git_stage.go` with one unexported helper the two tools share:

```go
// stageGitPaths(ctx context.Context, root, successNote string, paths ...string) string
```

Behavior, binding:

- Resolve git via `gitProgram(ctx, root)`. Any `!ok` (git absent OR exec-fence refusal) →
  return `""` (silent skip — staging is best-effort; the file operation already succeeded).
- Trackedness probe: run `git ls-files --error-unmatch -- <paths[0]>` via `runGit` with
  `gitTimeout`. Non-zero exit or error → return `""` (covers both "not a repository" and
  "source untracked" in one probe; `ls-files` reads the index, so probing AFTER the rename or
  unlink still sees the old path). `paths[0]` is by contract the pre-operation source path.
- Stage: `git add -A -- <paths...>` via `runGit`. Every pathspec (probe and add alike) is
  prefixed with the `:(literal)` pathspec magic so a filename containing `*`, `?` or `[` is
  never glob-interpreted.
- Return `successNote` on success; on a failed `add`, return
  `" (git staging skipped: <first line of git stderr>)"`.
- Paths are passed exactly as the tool received them; `runGit` runs with the workspace root as
  cwd, and git resolves both relative and absolute-inside-repo pathspecs from there — a
  workspace that is a subdirectory of the repository works without special handling.

The file's doc comment owns the undo-interplay note (ratified call 5): `/undo` restores
worktree bytes from the journal (ADR 0051) and deliberately does not touch the index; after an
undo, the staged rename/deletion remains visible in `git status` and `git add -A` or
`git restore --staged` resolves it. Items 2 and 3 reference this comment rather than restating
it. Add the new file to the package file map in `internal/tools/doc.go`
(`TestDocMapNamesEveryFile` pins this).

One deep helper, wording injected by the caller — the two tools must not grow parallel
staging code paths.

**Files:** `internal/tools/git_stage.go`, `internal/tools/git_stage_test.go`,
`internal/tools/doc.go`

**Tests** (`git_stage_test.go`; reuse the temp-repo fixtures `git_test.go` already has where
they fit):

- In a temp repo with a committed file: rename it on disk, call the helper with old+new
  paths → returns the successNote, and `git status --porcelain` shows a staged `R ` rename.
- Untracked file → returns `""`, index untouched.
- Plain temp dir (no repo) → returns `""`.
- git absent (inject the fake `lookGit` resolver as existing tests do) → returns `""`.
- Filename containing a glob metacharacter (`file[1].txt`) stages correctly.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestStageGitPaths|TestDocMap'`

**Commit:** `feat(tools): add stageGitPaths git-staging helper`

## 2. `move_file` stages the rename of a tracked source

Depends on item 1.

**What:** In `internal/tools/file_ops.go`, after `MoveFile.move` returns success — BOTH
routes: the plain `SafeRename` and the full copy-then-remove fallback — call
`stageGitPaths(ctx, t.root, " (rename staged in git)", args.Source, args.Destination)` and
append its return value to the ok-result text. Binding: staging runs ONLY on full success —
never on any error return, and never on the split-failure route ("copied … but could not
remove the source"), where the worktree does not hold a completed rename. `args.Source` is
`paths[0]` (the trackedness probe); an overwritten tracked destination is covered by the same
`add -A` pathspec pair. Update `moveFileSpec.description` by appending: `"When the source file
is tracked in git, the rename is staged automatically (the effect of git mv)."`

**Files:** `internal/tools/file_ops.go`, `internal/tools/file_ops_test.go`

**Tests:**

- Temp repo, committed file, move via the tool → result text ends with
  `(rename staged in git)` and `git status --porcelain` shows `R ` old → new.
- Untracked file moved → result text identical to today (no note), index untouched.
- Non-repo workspace → result text identical to today.
- Move with `overwrite: true` onto a tracked destination → both the rename and the
  destination's replacement are staged, no unstaged leftovers for those two paths.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestMoveFile|TestCopyFile'`

**Commit:** `feat(tools): stage renames of tracked files in move_file (git mv semantics)`

## 3. `delete_file` stages the deletion of a tracked file

Depends on item 1.

**What:** In `internal/tools/delete_file.go`, after `SafeRemove` succeeds, call
`stageGitPaths(ctx, t.root, " (deletion staged in git)", args.Path)` and append its return
value to the ok-result text (after the existing `resolved` note). Update
`deleteFileSpec.description` by appending: `"A file tracked in git has its deletion staged
automatically."` Keep the description's "permanently … no undo" framing intact — staging
changes what the index records, not whether the bytes survive. Reference item 1's
undo-interplay comment where the file's header comment discusses git.

**Files:** `internal/tools/delete_file.go`, `internal/tools/delete_file_test.go`

**Tests:**

- Temp repo, committed file, delete via the tool → result text ends with
  `(deletion staged in git)` and `git status --porcelain` shows `D `.
- Untracked file deleted → result text identical to today, index untouched.
- Non-repo workspace → result text identical to today.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestDeleteFile'`

**Commit:** `feat(tools): stage deletions of tracked files in delete_file`

---

**Suggested version bump:** micro (one shipped feature: git-aware file operations) — the
owner decides after the run; no plan item touches VERSION or CHANGELOG release headings.
