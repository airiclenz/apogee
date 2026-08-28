# Symlinked skill reads and the scratch-dir forced look — implementation plan

**Goal:** fix the two Auto-mode friction points diagnosed from the live `/code-audit` session
of 2026-08-28 (`~/.apogee/sessions/20260828T052713Z-55b6dbc4.json`): (1) `read_file` and
`copy_file` refuse a skill's bundled files when the home skills library is reached through a
symlink and the model addresses them by the spelling it was given (regression from `8b90a4fc`,
audit finding F-13); (2) the `write-apogee-control-plane` forced look fires on every terminal
command that names the session's own scratch dir under `~/.apogee/scratch/`, so Auto prompts
on each `go test`/`go vet` the model routes through its sanctioned scratch space.

**Date:** 2026-08-28
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `internal/tools/path_read.go` — `readScope` (`resolve`, `open`, `readBounded`, `readRoot`,
  `matchRoot`) at HEAD `b2f9b0c7`; the F-13 change is commit `8b90a4fc` (2026-08-26).
- `internal/security/safeio.go:635` `rootRelative` — the LEXICAL relativisation the bounded
  read pins on; `internal/security/pathsafety.go:37` `ResolveInRoot` — the REAL-path containment
  `resolve()` uses. The bug is the disagreement between the two when the accepting root is an
  extra root and the input is a symlink spelling.
- `internal/security/rules.go:178` rule `write-apogee-control-plane` (Tier 2, `WritesOnly`,
  pattern `homeAnchor + /\.apogee\b`); `internal/security/dangerous.go:131` `Inspect`;
  `internal/security/guard.go:102` `Guards.PreExecute`; call sites
  `internal/agent/dispatch.go:256` and `:394`.
- ADR 0049 §4 (`~/.apogee` is a forced look, never a boundary); ADR 0056 (session scratch dir
  is the box's extra writable path — `internal/domain/confinement.go:95-98`); CONTEXT.md
  "Dangerous-action guard" (line ~780).
- Reproduction (throwaway test, 2026-08-28): `readScope{root: <ws>, extra: [/workspace/repos/skills]}`
  → `readBounded("/root/.apogee/skills/code-audit/prompts/_shared.md")` refuses with
  `ErrPathEscape`; the same call with the resolved input reads 4843 bytes. `list_dir`, `grep`,
  `find_files` (all via `resolve()`) accept the symlink spelling today.

**Ratified design calls:**
- **Exemption scope (owner, 2026-08-28):** the forced look stops firing ONLY for this session's
  own scratch dir (`Agent.ScratchDir()`), in every home spelling. Other sessions' scratch dirs,
  `config.yaml`, `skills/`, `sessions/` keep the Tier-2 look. Never the whole
  `~/.apogee/scratch/`.
- **Skill header (owner, 2026-08-28):** the `files:` line and the `{{SKILL_DIR}}` substitution
  keep announcing the dir AS CONFIGURED (`Skill.Dir` unresolved). The fix is in the read tools,
  which must accept both spellings. This SUPERSEDES the fix proposed in the ISSUES.md entry
  "A dotfiles-symlinked `~/.apogee/skills` hands the model a path the read fence refuses"
  ("stamp `Skill.Dir` from the resolved anchor") — item 2 retires that entry.
- **Mechanism for (1) (plan author, 2026-08-28):** a `readScope` method that answers, per call,
  the root that accepts the input AND the path to hand the fenced open — the input UNCHANGED
  when the workspace root accepts it (a symlinked workspace root, macOS `/tmp`, keeps today's
  behaviour byte-for-byte), the symlink-RESOLVED real path when an extra root does (extra roots
  are real paths by `matchRoot`'s contract, so the lexical `rootRelative` and the real-path
  containment then agree). No change to `security.rootRelative`, `safeOpen`, or the extra-root
  mount contract.
- **Mechanism for (2) (plan author, 2026-08-28):** the guard takes a per-call list of exempt
  paths and MASKS their spellings out of the inspectable text before any rule runs — a generic
  seam (`exemptPaths []string` on `Guards.PreExecute` / `DangerousActionGuard.Inspect`), not a
  rule-specific special case. Dispatch passes the agent's live `ScratchDir()`. Masking runs
  before EVERY rule, so a hard-refuse rule sees the placeholder too — acceptable because the
  masked path is the session's own writable box and no Tier-1 rule targets it; a command that
  names the scratch dir AND `~/.ssh` still hard-refuses on the `~/.ssh` half (test pins it).

**Standing requirements:**
- `skills: coding-standards`
- Any authorised deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; `make check` runs once at closeout.
- Every item's CHANGELOG entry goes in its sidecar (closeout assembles them under
  `[Unreleased]`). No item changes `VERSION`.

**Out of scope:**
- Changing what the skill header announces (ratified above).
- Widening the exemption beyond the session's own scratch dir (ratified above).
- The other two residuals in the same ISSUES.md section (Firing-mount revert test, fail-closed
  proxy tests) — they stay in ISSUES.md.
- Moving the scratch dir out of `~/.apogee` (option (b), rejected in favour of (a)).
- Any change to `security.rootRelative` / `safeOpen` semantics or to `matchRoot`'s
  "extra root must be its own real path" rule.

---

## 1. `readScope` hands the resolved path to a fenced read under an extra root — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `locate` is implemented over the existing `resolve` (root == workspace ⇒ input as given, else the resolved path) rather than open-coding `resolveInRoot` + `matchRoot` a second time — same three steps, same behaviour, one copy of the root order, as the item's own standards line requires.
NOTES (2026-08-28): `readRoot` is now `locate`'s root half (the pure simplification the item permits); its short-circuit for "no extra roots" is gone but the answer is identical in every branch.
NOTES (2026-08-28): tests take the real extra root from `tempRoot(t)`, which already applies `realPath`, instead of wrapping it in `security.EvalRealPath` — equivalent, and it keeps `internal/security` out of the test file's imports.
NOTES (2026-08-28): the two "workspace-root branch" and "no root accepts" bullets are one test, `TestReadScopeLocate`; the no-root branch covers both `/nowhere/x` and a real out-of-root file, and asserts `readBounded`'s message equals the workspace refusal verbatim.
NOTES (2026-08-28): confirmed `resolvedTargetNote(args.Path, t.scope.readRoot(args.Path))` still discloses the real path for the symlink spelling — no change needed in `read_file.go`; the new `read_file` test pins it.

**What:**
- In `internal/tools/path_read.go` add `func (s readScope) locate(input string) (root, target string, err error)`:
  1. `resolveInRoot(input, s.root)` succeeds → return `(s.root, input, nil)` — the input AS GIVEN
     (workspace behaviour unchanged, including a symlinked workspace root).
  2. else `matchRoot(input, s.extraRoots(input))` accepts → return
     `(extraRoot, resolvedRealPath, nil)` — the REAL path `matchRoot` already computed.
  3. else → `("", "", <the workspace's ErrPathEscape from step 1>)`.
- `readBounded` becomes: `root, target, err := s.locate(input)`; on error call
  `readWorkspaceFileBounded(input, s.root)` exactly as today (so the refusal text stays the
  workspace's own); otherwise `readWorkspaceFileBounded(target, root)`.
- `readRoot` stays as-is (read_file's `resolvedTargetNote` and copy_file's disclosure still
  use it); implement it via `locate` only if that is a pure simplification — no behaviour change.
- Rewrite the `matchRoot` doc paragraph that today says the mount and the fence "would disagree
  … resolveInRoot judges containment on REAL paths while the bounded read's rootRelative
  relativises LEXICALLY": that disagreement is exactly this bug, and `locate` is now the seam
  that makes them agree (real root + real target). Keep the "root must be its own real path"
  rule and its F-13 reasoning.
- `internal/tools/read_file.go` needs no code change beyond what `readBounded` fixes; confirm
  `resolvedTargetNote(args.Path, t.scope.readRoot(args.Path))` still discloses the resolved
  path for the symlink spelling (it resolves through `ResolvedWriteTarget`-style real-pathing —
  verify in the test below, adjust only if the note comes out empty or wrong).
- Standards that bind here: one seam (`locate`) answers both "which root" and "which path";
  no second copy of the fallback logic in callers; the workspace-root branch is byte-identical
  to today.

**Files:** `internal/tools/path_read.go`, `internal/tools/path_read_test.go`,
`internal/tools/read_file_test.go`

**Tests:**
- `path_read_test.go` — `TestReadScopeReadsAnExtraRootFileBySymlinkSpelling`: `extra :=
  security.EvalRealPath(tempRoot(t))` holding `skill/SKILL.md`; a second temp dir holding
  symlink `lib -> extra`; scope `{root: ws, extra: []string{extra}}`; `readBounded(filepath.Join(link,
  "skill", "SKILL.md"))` returns the bytes and an empty message; `locate` returns
  `(extra, <real path of the file>, nil)`.
- Same file — the workspace-root branch: `locate("in-workspace.txt")` returns
  `(ws, "in-workspace.txt", nil)` (input unchanged, relative) and an absolute in-workspace path
  comes back unchanged too.
- Same file — no root accepts: `locate("/nowhere/x")` returns `ErrPathEscape` naming the
  input; `readBounded` message equals today's workspace refusal.
- `read_file_test.go` — `TestReadFile_Execute_ReadsAnExtraRootFileBySymlinkSpelling`: same
  fixture through `NewReadFile(ws, func() []string { return []string{extra} })`, path given as
  the symlink spelling; content equals the fixture; the result's resolved-target note names the
  real path (mirror `TestReadFile_Execute_DisclosesTheResolvedPathUnderAnExtraReadRoot`).
- Existing `TestReadScopeRefusesAnExtraRootReachedThroughASymlink` and
  `TestReadScopeRefusesSymlinkEscapingExtraRoot` must stay green unchanged — the symlink must be
  in the INPUT, never in the mounted root.

**Acceptance:**
- `go build ./... && go vet ./internal/tools`
- `go test ./internal/tools -run 'ReadScope|ReadFile' -count=1`

**Commit:** `fix(tools): read an extra-root file by its symlink spelling — hand the fenced read the resolved path`

---

## 2. `copy_file` sources through the same seam; retire the ISSUES entry — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `checkFileOpsPathsFrom` takes the source spelling as a new `sourcePath` parameter (the signature option the item offers, not the `args`-copy one), so its refusals keep naming `args.Source` — the spelling the model wrote — and every existing refusal string is byte-identical. `checkFileOpsPaths` passes `args.Source`, so move_file is unchanged.
NOTES (2026-08-28): `err := journaledMutation(...)` became `err =` because `locate` now declares `err` earlier in `CopyFile.Execute`; no other change to that block.
NOTES (2026-08-28): `TestCopyFile_ExtraReadRootRefusals`' inline table literal is now a named `refusalCase` slice so the new symlink-spelling case can be appended only when `os.Symlink` succeeds — a `t.Skipf` would have cost the three existing cases on a symlink-less platform.
NOTES (2026-08-28): the item text says "the other two bullets ... stay" in that ISSUES.md section; there are in fact three other bullets (Firing-mount revert test, fail-closed proxy tests, and the `--model` "hint" help text). Only the first bullet was removed; all three others and the section heading stay.
NOTES (2026-08-28): regression proof — reverting only the `SafeCopyFileFrom` argument to `args.Source` makes `TestCopyFile_CopiesFromAnExtraRootBySymlinkSpelling` fail with the escape refusal, so the new test pins the fix rather than passing incidentally.

Depends on item 1.

**What:**
- `internal/tools/file_ops.go` copy path (`CopyFile.Execute`, around line 131): replace
  `sourceRoot := t.scope.readRoot(args.Source)` with `sourceRoot, source, err :=
  t.scope.locate(args.Source)`; on error fall back to `(t.root, args.Source)` so the refusal
  stays the workspace's own. Hand `source` (not `args.Source`) to
  `checkFileOpsPathsFrom(ctx, args, sourceRoot, t.root)` — thread it as the source path that
  function checks (adjust its signature or the `args` copy it inspects; keep destination
  handling untouched) — and to `security.SafeCopyFileFrom(sourceRoot, source, t.root,
  args.Destination, escape)`. The destination side, `journaledMutation`, the disclosure note
  and the tool description are unchanged: the destination stays workspace-fenced (ADR 0012 D1).
- Update the `file_ops.go` doc comment that describes the source's root being "chosen ONCE per
  call" to say the source PATH is chosen with it.
- `ISSUES.md`: remove the entry "A dotfiles-symlinked `~/.apogee/skills` hands the model a path
  the read fence refuses" (the first bullet under "Read-fence / egress / docs-truth residuals —
  deferred out of the 2026-08-26 run"). The other two bullets and the section heading stay. The
  sidecar CHANGELOG entry records the fix (and that the ratified fix differs from the entry's
  proposed one: tools accept both spellings; the header stays as configured).

**Files:** `internal/tools/file_ops.go`, `internal/tools/file_ops_test.go`, `ISSUES.md`

**Tests:**
- `file_ops_test.go` — `TestCopyFile_CopiesFromAnExtraRootBySymlinkSpelling`: fixture as in
  item 1 (real extra root mounted, symlink spelling in the `source` argument); destination
  `prompts/x.md` inside the workspace; bytes and mode copied; result is not an error.
- Same file — the symlink spelling of a path that resolves OUTSIDE every root is still refused
  with `ErrPathEscape` (extend `TestCopyFile_ExtraReadRootRefusals` with one case).
- `TestCopyFile_ExtraReadRootSourceFollowsSymlinksIntoTheWorkspace` stays green unchanged.

**Acceptance:**
- `go build ./... && go vet ./internal/tools`
- `go test ./internal/tools -run 'CopyFile|FileOps|ReadScope' -count=1`
- `grep -c "dotfiles-symlinked" ISSUES.md` prints `0`

**Commit:** `fix(tools): copy_file reads an extra-root source by its symlink spelling`

---

## 3. Lock in the symlink spelling for `list_dir`, `grep`, `find_files`

Tests only — these three already resolve through `readScope.resolve()` and accept the symlink
spelling; the tests pin that so a future change to `resolve()` cannot regress them the way
`readBounded` did. Independent of items 1–2 (disjoint files).

**What:** one regression test per tool, same fixture shape as item 1 (real extra root mounted;
the tool's path argument is the symlink spelling): `list_dir` lists `skill/SKILL.md`; `grep`
finds a line in it; `find_files` returns it by glob. Each asserts the returned path names the
real (resolved) location, matching what the tools return today for the real spelling.

**Files:** `internal/tools/list_dir_test.go`, `internal/tools/grep_test.go`,
`internal/tools/find_files_test.go`

**Tests:** `TestListDir_ListsAnExtraRootBySymlinkSpelling`,
`TestGrep_SearchesAnExtraRootBySymlinkSpelling`,
`TestFindFiles_SearchesAnExtraRootBySymlinkSpelling`.

**Acceptance:**
- `go test ./internal/tools -run 'ListDir|Grep|FindFiles' -count=1`

**Commit:** `test(tools): pin the symlink spelling of an extra read root for list_dir, grep and find_files`

---

## 4. The dangerous-action guard masks the session's own scratch dir

**What:**
- `internal/security/dangerous.go`: `Inspect(call domain.ToolCall, tool domain.Tool, exemptPaths []string) Decision`.
  Before rule matching, mask every exempt path's spellings out of BOTH views (`full` and
  `writes`) with a placeholder that no rule can match through (`<exempt>` — no slash, no dot).
  For each non-empty exempt path `p`:
  1. `n := normalize(p)`; replace every literal occurrence of `n`;
  2. if `n` contains `/.apogee/`, also replace every match of
     `homeAnchor + "/" + regexp.QuoteMeta(n[idx(".apogee/"):])` — so `~/…`, `$HOME/…`,
     `/root/…`, `/home/<u>/…`, `/Users/<u>/…` spellings of the same dir all mask, without the
     guard knowing the home dir. Compile these per call (a handful of `QuoteMeta` regexps; cache
     by path string if `regexp.Compile` shows in a benchmark, otherwise don't).
  A trailing separator or a deeper path under the exempt dir masks with it (the pattern is a
  prefix match on the dir, `\b`-anchored at the dir's end so `<id>x` does not mask). An empty or
  nil `exemptPaths` leaves the text untouched — existing behaviour byte-for-byte.
- `internal/security/guard.go`: `PreExecute(call domain.ToolCall, tool domain.Tool, exemptPaths []string) PreCheck`
  threads the slice to `Inspect`. Doc the parameter: "paths whose spellings no rule may see —
  today the session's own scratch dir, the box's extra writable path (ADR 0056); a nil slice is
  the guard as before".
- `internal/agent/dispatch.go:256` and `:394`: pass `a.guardExemptions()` — a small method
  returning `[]string{a.ScratchDir()}` when `ScratchDir() != ""`, else nil. Each agent (root or
  sub-agent) passes its OWN `ScratchDir()`; `Guards.ForSubAgent()` is unchanged (the guard
  stays shared and stateless — the exemption is a per-call argument, never guard state).
- Update every other `PreExecute`/`Inspect` caller the compiler finds (tests included) with
  `nil`.
- `internal/security/rules.go`: extend the `write-apogee-control-plane` comment with one
  sentence: the session's own scratch dir under `~/.apogee/scratch/` is masked out of the text
  before this rule runs (dispatch passes it as an exemption) — the box already declares it
  writable, so a look there would answer nothing.
- Standards that bind here: the exemption is an ARGUMENT of the inspection, not guard state
  (the guard is shared across agents); masking is one function (`maskExempt(text string,
  exempt []string) string`) with its own table test; no rule text changes.

**Files:** `internal/security/dangerous.go`, `internal/security/guard.go`,
`internal/security/rules.go`, `internal/security/dangerous_test.go`,
`internal/security/guard_test.go`, `internal/agent/dispatch.go`, `internal/agent/agent.go`
(the `guardExemptions` helper beside `ScratchDir`)

**Tests:**
- `dangerous_test.go` — `TestInspectMasksTheSessionScratchDir` (table): exempt =
  `/root/.apogee/scratch/20260828T052713Z-55b6dbc4`; terminal command text from the live
  session (`export GOCACHE=/root/.apogee/scratch/20260828T052713Z-55b6dbc4/gocache … && go vet ./...`)
  → `TierNone`; the same with `~/.apogee/scratch/<id>/tmp` → `TierNone`; with
  `$HOME/.apogee/scratch/<id>` → `TierNone`; with `/root/.apogee/scratch/<other-id>/x` →
  `TierForceApproval` (`write-apogee-control-plane`); a command naming the exempt dir AND
  `/root/.apogee/config.yaml` → `TierForceApproval`; naming the exempt dir AND `rm -rf ~/.ssh`
  → `TierHardRefuse`; `<id>x` suffix → still fires; exempt `nil` → fires (today's behaviour).
- `guard_test.go` — `PreExecute` with the exemption returns `GuardProceed`; without it
  `GuardForceApproval`.
- Existing `TestDefaultDangerousRules_*` and `TestWritesOnly*` tests pass with `nil`.

**Acceptance:**
- `go build ./... && go vet ./internal/security ./internal/agent`
- `go test ./internal/security -count=1`
- `go test ./internal/agent -run 'Dispatch|Guard' -count=1`

**Commit:** `fix(security): the dangerous-action guard masks the session's own scratch dir, so Auto no longer forces a look on every scratch-routed command`

---

## 5. Agent-level regression and the documents that state the floor

Depends on item 4.

**What:**
- `internal/agent/dispatch_test.go`: `TestDispatch_ScratchDirCommandIsConfinedNotForcedInAuto` —
  Auto, confine-to-workspace on, a Confiner whose caps report `FSWrite`, `SetScratchDir` to a
  path under a fake home's `.apogee/scratch/<id>`; a `terminal` call whose command names that
  dir resolves to `resolveConfine` and the Approver is NEVER consulted; the same call naming
  `<home>/.apogee/config.yaml` resolves to a forced Gate (reason `forceApprovalReason`,
  rule `write-apogee-control-plane`). Mirror the fixture style of
  `TestDispatch_ApprovedForcedGateRunsConfined`.
- `docs/adr/0049-…md`: append `## Amendment (2026-08-28) — the session's own scratch dir is
  outside the forced look` stating: §4's forced look on `~/.apogee` stands; the ONE carve-out is
  the session's own scratch dir (ADR 0056's extra writable path), masked from the guard's text
  per call, because the box already declares it writable and a look there would answer
  nothing; other sessions' scratch dirs and every other control-plane path keep the look;
  cite the 2026-08-28 session where each `go test` routed through scratch prompted.
- `CONTEXT.md` "Dangerous-action guard" paragraph (line ~786): after "a write under
  `~/.apogee` … (ADR 0049 §4)" add "— except the session's own scratch dir, which the box
  already declares writable (ADR 0049 amendment 2026-08-28)".
- `internal/security/doc.go`: if it enumerates the `~/.apogee` forced look, add the same
  one-clause exception; if it does not mention it, change nothing.

**Files:** `internal/agent/dispatch_test.go`,
`docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md`,
`CONTEXT.md`, `internal/security/doc.go`

**Tests:** the dispatch test above.

**Acceptance:**
- `go test ./internal/agent -run 'Dispatch_ScratchDir|Dispatch_ApprovedForcedGate|Dispatch_ForcedGate' -count=1`
- `grep -c "Amendment (2026-08-28)" docs/adr/0049-*.md` prints `1`
- `grep -c "own scratch dir" CONTEXT.md` prints at least `1`

**Commit:** `docs(adr): 0049 amended — the session's own scratch dir is outside the ~/.apogee forced look; dispatch test pins it`

---

## Suggested version bump

Patch (`v0.18.3`): two user-visible fixes on the shipped `v0.18.2` line — a skill-library
read regression for symlinked `~/.apogee/skills`, and spurious Auto-mode approval prompts on
every scratch-routed terminal command. The owner decides; no item changes `VERSION`.
