# Tool-surface improvements — context, atomicity, discovery, new tools, toggleability

- **Goal:** land the tool-surface work ratified from the 2026-08-10 four-poll model
  feedback round (Qwen3.6-35B-A3B ×2, Gemma-4-26B-A4B, Gemma-4-E4B): the three
  uncontested improvements, the promoted new tools (find_files, git_status, git_log,
  copy/move/delete, run_tests), a global tool on/off switch so evidence can prune the
  roster later, and a TODO.md record of everything deferred or denied.
- **Date:** 2026-08-10
- **Status:** not started
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/tools/grep.go` (grep schema, match format, `include` glob semantics,
    `grepExcludeDirs`)
  - `internal/security/safeio.go` (SafeWriteFile — the single write path all five write
    tools funnel through via `internal/tools/path_safety.go:34`)
  - `internal/tools/registry.go` (`DefaultTools` / `NewDefaultRegistry` — where the
    roster is assembled)
  - `internal/tools/git.go` (conventions for git tools: safety blocks, output shape)
  - `cmd/apogee/config.go` + `cmd/apogee/configwrite.go` (config loading and the
    settings surface)
  - ADR 0012 (dangerous-action guard, confinement policy), ADR 0035/0037/0041 (settings
    persistence, live-apply, watched config),
    `docs/design/confinement-execution-contract.md` (workspace-fence semantics,
    workspaceScopedWriter, confined exec)
- **Ratified design calls** (owner, 2026-08-10, via in-session question tool):
  1. grep context is ONE symmetric `context_lines` parameter (grep `-C` style).
  2. Atomic write lives in `security.SafeWriteFile` itself so all write tools benefit;
     on overwrite the existing file's mode is preserved.
  3. Glob file-discovery ships as a DEDICATED `find_files` tool, not a list_dir
     parameter (supersedes the same-day list_dir-param call: two independent polls
     missed list_dir's `recursive` parameter — models discover capabilities by tool
     name, not by parameters). `list_dir` is untouched. Glob syntax carries over:
     comma-separated basename globs mirroring grep's `include`.
  4. Promotions: `run_tests`, `git_status`, `git_log`, and `copy_file`/`move_file`/
     `delete_file` are implemented NOW. Owner directive: every tool must be modular and
     easily switched on/off, because too many tools may confuse smaller models — the
     roster gets pruned later on bench evidence, not guesswork.
  5. The v1 on/off switch is a GLOBAL `tools.disabled` list in `~/.apogee/config.yaml`,
     live-applied via the watched config; per-profile rosters stay a future grill and
     will build on this key.
  6. `run_tests` v1 auto-detects three runners by project markers: `go.mod` → `go test`,
     pytest markers (`pytest.ini` / `pyproject.toml` with pytest config) → `pytest`,
     `package.json` with a test script → `npm test`.
  7. `copy_file`/`move_file` refuse an existing destination unless `overwrite: true`.
  8. `delete_file` deletes FILES ONLY (directories refused) and is classified under the
     ADR 0012 dangerous-action guard policy.
- **Standing requirements:** skills: coding-standards. Run `make check` before every
  commit. Any authorized deviation from item text lands as a dated NOTES line under the
  item.
- **Out of scope:** removing or merging any tool (open_file/read_file, view_diff,
  single_find_and_replace, web_fetch — bench experiments first, recorded by item 10);
  per-profile tool rosters; env-var parameters; directory_create/delete; git_stash,
  git_tag, or a unified git tool; streaming/progress output; structured JSON tool
  output; context-window introspection; any version bump (see closing note).

## 1. grep gains a `context_lines` parameter — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item, deviating from the
repo's usual changelog convention — the owner ratified that NO item in this plan touches
CHANGELOG.md (the closing note's rule, applied to every item).

**What:** In `internal/tools/grep.go`, add an optional integer `context_lines`
(default 0, silently clamped to the range 0–10) to the grep schema. For each match,
emit up to N lines before and N lines after (grep `-C` semantics — ratified call 1).
Output format: match lines keep the existing `file:line:text` form; context lines use
`file:line-text` (dash separator, the grep/ripgrep convention); a `--` line separates
non-adjacent match groups within the same file. Overlapping or adjacent context regions
merge — no line is ever printed twice. `max_results`, `offset`, and the truncation note
count MATCHES only; context lines ride along free. With `context_lines` absent or 0,
output is byte-identical to today's.

**Tests:** extend `internal/tools/grep_test.go`: context before/after a mid-file match;
match at line 1 and at EOF (no out-of-range lines); two nearby matches with overlapping
context merge without duplicates; `--` separator between distant groups; pagination
still counts matches only; default output unchanged.

**Acceptance:** `go test ./internal/tools/ -run TestGrep -v` passes; `make check` passes.

**Commit:** `feat(tools): grep gains a context_lines parameter`

## 2. SafeWriteFile writes atomically via temp + rename — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that
NO item in this plan touches CHANGELOG.md. Beyond the item's "update the function's doc
comment", the TOCTOU-safe-I/O paragraph in `docs/design/technical-design.md` gained the
new atomicity + replace-the-name contract, since that doc is the one describing
`SafeWriteFile`'s write-path guarantees and would otherwise be stale.

**What:** In `internal/security/safeio.go`, change `SafeWriteFile` to write the data to
a temporary file in the TARGET'S parent directory inside the pinned `os.Root` (a name
like `.apogee-tmp-<random>`; same directory guarantees same filesystem, which rename
atomicity requires), then rename it over the target through the root handle
(`r.Rename`) — ratified call 2. A crash mid-write can no longer leave a truncated
target. Mode handling: before writing, stat the target through the root; if it exists,
apply its mode to the temp file so the rename preserves it; if it does not exist, use
the `perm` argument as today. On any failure after the temp file is created, remove the
temp file before returning. Fence semantics are unchanged: every escape path that is
refused today (traversal, absolute-outside, symlinked component, concurrent swap) is
still refused, and the `MkdirAll` parent-creation behavior is untouched. One deliberate
semantic change, binding: when the target NAME is an in-root symlink, the rename
replaces the symlink itself with a regular file (today's write-through went to the
link's in-root target) — replace-the-name is the new contract. No fsync —
rename atomicity (no torn file visible at the target name) is the goal; power-loss
durability is out of scope. Update the function's doc comment to state the new
contract.

**Tests:** extend `internal/security` tests: overwrite of a 0755 file preserves 0755;
new file gets the `perm` argument; content is fully replaced; no `.apogee-tmp-*` file
remains after success; no temp remains after a forced rename failure (e.g. target path
is an existing directory); path-escape inputs still refuse with `ErrPathEscape` and
write nothing — including no temp file outside the root; an in-root symlink at the
target name is replaced by a regular file (not written through).

**Acceptance:** `go test ./internal/security/...` passes; `make check` passes.

**Commit:** `feat(security): SafeWriteFile writes atomically via temp+rename`

## 3. New `find_files` tool — glob discovery by name — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that NO
item in this plan touches CHANGELOG.md. Beyond the item's literal text (new tool + registry
registration), three roster-facing places were updated so they stay true of the shipped set:
`internal/tools/doc.go` (the package's tool narrative), `internal/tui/toolpresent.go` (its
`toolRegistry` documents itself as covering the full built-in set, so the new tool gets a card
entry — six lines, no renderer change), and the tool-suite counts in
`docs/design/technical-design.md` (21 → 22). The tool attaches NO `domain.ToolSummary`: that sum
type is sealed and its seven carriers are pinned by name in `CONTEXT.md`, `doc.go` and a TUI pin
test, so adding an eighth is a change this item does not own — the header prose carries the
count instead. Registry-roster test expectations (names, ordering, counts, the read-only table)
were updated because the new registration necessarily changes them.

**What:** New file `internal/tools/find_files.go`: a read-only tool `find_files` that
walks the workspace (or an optional `path` subtree) and returns workspace-relative
paths whose BASENAME matches a required `pattern` of comma-separated globs — same
syntax and malformed-glob handling as grep's `include` (ratified call 3; mirror
`internal/tools/grep.go`). Recursive by default (that is the tool's point — no
`recursive` flag). Skips the same directories grep skips (`grepExcludeDirs`). Supports
`max_results` (default 50, hard cap mirroring grep's collection bound) and `offset`
pagination with a truncation note. Path resolution and fencing follow the existing
read-tool pattern (workspace fence at open/walk time). `list_dir` is NOT modified.
Register the tool in `internal/tools/registry.go` alongside the other defaults. Schema
description must say it finds files by NAME pattern — basename globs only, path
patterns like `src/**/*.go` unsupported — and that `grep` is for content; a model
reading only the tool list must be able to pick correctly.

**Tests:** new `internal/tools/find_files_test.go`: flat and nested matches; match in a
deep subdirectory; `*.go` matches both `src/main.go` and `test/main_test.go`
(basename-only — never a path-prefix pattern); comma-separated multi-glob; excluded
dirs (e.g. `.git`) never
searched; pagination + truncation note; malformed glob behaves exactly as grep's
`include`; path-escape input refused; registry includes the tool.

**Acceptance:** `go test ./internal/tools/ -run TestFindFiles -v` passes; `make check`
passes.

**Commit:** `feat(tools): add find_files name-glob discovery tool`

## 4. Global `tools.disabled` config key — the roster switch — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that NO item in
this plan touches CHANGELOG.md. Three things beyond the item's literal text: (a) the filter is fed
through a new `domain.Config.DisabledTools` field (threaded by `internal/agent/construct.go`'s
`hostTools`) as well as through `tools.HostTools.Disabled`, because `apogee headless` leaves
`Config.Tools` nil and lets the engine assemble the registry — without the Config field the key would
silently not apply to a headless run of the same config; (b) the switch prunes the BUILT-IN roster
only — an MCP server's tools come and go with the server, so `mcp-servers:` remains the way to drop
those, and `registryWithMCP`'s doc comment says so; (c) README's Configuration section gained the
key's user-facing paragraph, the repo's home for file-only config keys, and the seeded template gained
the commented example the item asked for.

**What:** Add a `tools.disabled` string-list key to the config (loading in
`cmd/apogee/config.go`, seeded template untouched except a commented example; the key
is optional and defaults to empty = all tools enabled). Binding behavior (ratified
call 5): a disabled tool is neither OFFERED in the tool list the engine sends nor
DISPATCHABLE — a call naming it is refused as an unknown tool. Whether the filter
lives in registry assembly (`internal/tools/registry.go` `DefaultTools` /
`NewDefaultRegistry` and their WithHost variants) or in the engine's request-time
tool-list build is the implementer's choice after reading the engine wiring —
whichever makes the live-reapply below simplest, provided both halves of the binding
behavior hold. A listed name
matching no known tool produces a one-line warning through the existing startup/notice
path, never an error. Live behavior follows ADR 0037/0041: a watched-config edit to
`tools.disabled` re-applies to the running session the same way other watched keys do,
taking effect from the next request's tool list. This key is deliberately global; the
per-profile roster (future grill, item 10 records it) will build on it.

**Tests:** registry filter drops exactly the listed tools; empty/absent key changes
nothing; unknown name warns and is otherwise ignored; a disabled tool is absent from
the tool list the engine offers AND a call to it is refused as unknown (existing
registry/engine tests extended); config round-trip parses the key.

**Acceptance:** `go test ./internal/tools/... ./cmd/apogee/...` passes; `make check`
passes.

**Commit:** `feat(config): tools.disabled key toggles tools off at registry level`

## 5. New `git_status` tool — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that NO item
in this plan touches CHANGELOG.md. Four things beyond the item's literal text, all consequences of
adding a tool rather than choices about it: (a) the tool is registered in
`internal/tools/registry.go` (an unregistered tool is not a tool), which necessarily moved the
registry tests' name lists, ordering and counts; (b) the three roster-facing places item 3 already
established are updated so they stay true of the shipped set — `internal/tools/doc.go`,
`internal/tui/toolpresent.go` (a card entry; no renderer change, and no `target` because the tool
takes no arguments), and the tool-suite counts in `docs/design/technical-design.md` (22 → 23);
(c) the git-family header comment in `git.go` said "Three one-shot tools", now four. Two shape
calls the item left open: no parameters at all (the existing git tools take `files`/`paths`, never
a plain optional `path`, so the item's conditional resolves to none), and an UNMERGED path is
reported once under Unstaged — work the tree still owes — rather than in both lists its XY code
would otherwise select.

**What:** In `internal/tools/git.go`, add a read-only `git_status` tool: current
branch, ahead/behind upstream counts when an upstream exists, and bounded lists of
staged, unstaged, and untracked paths (cap each list, note truncation). Implemented
over porcelain output following the existing git-tool conventions in that file (same
exec path, same error shape). No parameters beyond an optional `path` if the existing
git tools already take one — otherwise none.

**Tests:** extend `internal/tools/git_test.go`: clean tree; staged + unstaged + 
untracked mix; detached HEAD; not-a-repo error shape matches the other git tools.

**Acceptance:** `go test ./internal/tools/ -run TestGitStatus -v` passes; `make check`
passes.

**Commit:** `feat(tools): add git_status tool`

## 6. New `git_log` tool — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that NO
item in this plan touches CHANGELOG.md. Beyond the item's literal text, the same roster-facing
set item 5 established was updated so it stays true of the shipped suite, all consequences of
adding a tool rather than choices about it: registration in `internal/tools/registry.go` (an
unregistered tool is not a tool), which moved the registry tests' name lists, ordering and
counts; `internal/tools/doc.go`; a card entry in `internal/tui/toolpresent.go` (with a small
`gitLogTarget` extractor that defaults the shown ref to HEAD exactly as the tool does, the
precedent being `methodURLTarget`'s GET default); and the tool-suite counts in
`docs/design/technical-design.md` (23 → 24, including the present_document ordinal item 5
maintained as a live count). The git-family header comment said "Four one-shot tools", now
five. Three shape calls the item left open: (a) the date is `--date=iso-strict`, whose
timestamp is space-free so each line splits positionally into hash / date / subject; (b) a
`max_count` of 0 or less takes the default rather than the clamp's floor of 1 — Go's zero
value carries no "was it supplied?" bit, and this is the reading grep's `max_results` already
uses, so every accepted value still lands in 1–100; (c) an empty repository is git's own
error surfaced as an IsError result (the not-a-repo shape the item names), never a success
claiming an empty history. One safety addition the item did not name: the argv ends in `--`,
because `git log <name>` on a name that is a tracked PATH rather than a ref is a pathspec log
that answers a different question with exit 0 — a model's typo'd branch name would otherwise
return a plausible, wrong history reported as success (the same class `buildBranchArgs` closes
for checkout; pinned by `TestGitLog_PathShapedRefIsNotAPathspecLog`).

**What:** In `internal/tools/git.go`, add a read-only `git_log` tool: optional `ref`
(default HEAD) and `max_count` (default 20, clamped 1–100). Output one line per commit:
short hash, ISO date, subject — following the existing git-tool conventions. Not-a-repo
and unknown-ref errors match the other git tools' error shape.

**Tests:** extend `internal/tools/git_test.go`: default HEAD log; max_count clamp;
explicit ref; unknown ref error; empty repo.

**Acceptance:** `go test ./internal/tools/ -run TestGitLog -v` passes; `make check`
passes.

**Commit:** `feat(tools): add git_log tool`

## 7. New `copy_file` and `move_file` tools — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry was written for this item — the owner ratified that NO item
in this plan touches CHANGELOG.md. Four things beyond the item's literal text. (a) The fenced
operations these tools need had NO primitive: `internal/tools` cannot rename inside the pinned
`os.Root`, and doing it on a resolved path STRING is exactly the TOCTOU gap item 2 closed — so
`internal/security/safeio.go` gained `SafeCopyFile` (staged + renamed like `SafeWriteFile`,
streamed so a large copy costs no memory, landing the SOURCE's mode because a 0755 script copied
0644 is a broken copy), `SafeRename` and `SafeRemove`, each fencing BOTH ends at operation time,
with tests. (b) `workspace_scoped.go` gained `destinationArgWriteTarget`: the existing marker
tests require every carrier to declare a REQUIRED `path` argument, which these two do not have,
so the probe table now carries the target key per writer (`pathField`/`callArgs` take it as a
parameter) rather than assuming one spelling — the fence's rename guard stays exhaustive for both
shapes. (c) The roster-facing set items 3/5/6 established was updated so it stays true of the
shipped suite: registration in `internal/tools/registry.go` (which necessarily moved the registry
tests' name lists, ordering and counts), `internal/tools/doc.go`, two card entries in
`internal/tui/toolpresent.go` (with a `sourceDestinationTarget` extractor rendering
`source → destination`, since a row naming one half cannot say what the call did), and the
tool-suite counts in `docs/design/technical-design.md` (24 → 26, in both places). (d) Neither tool
attaches a `domain.ToolSummary`, for item 3's reason: that sum type is sealed and its seven
carriers are pinned by name in three places. Two shape calls the item left open: a DIRECTORY
destination is refused outright (a model with `cp foo bar/` habits would otherwise create a file
named after the directory), and the copy-then-remove fallback is not retried after a fence
refusal — the fallback would refuse identically, and reporting the escape once is what tells the
model the truth.

**What:** New file `internal/tools/file_ops.go`: two write tools. Both take `source`,
`destination`, and optional `overwrite` (default false). Both paths must resolve inside
the workspace fence (both are validated; the write target for dispatch classification
is the destination — and for move, the source removal is part of the same fenced
operation). Destination exists and `overwrite` is false → IsError naming the conflict
(ratified call 7). `copy_file` copies content and preserves the source's mode.
`move_file` renames within the root, falling back to copy-then-remove if rename fails.
Parent directories of the destination are created as needed (mirroring write_file).
Both carry the `workspaceScopedWriter` marker per the confinement execution contract.
Missing source, source-is-directory, and path escapes are IsError results. Register
both in `internal/tools/registry.go`.

**Tests:** new `internal/tools/file_ops_test.go`: copy preserves content + mode; move
removes the source; overwrite refusal and `overwrite: true` force; destination parent
creation; directory source refused; escape refused for source AND destination; registry
includes both.

**Acceptance:** `go test ./internal/tools/ -run 'TestCopyFile|TestMoveFile' -v` passes;
`make check` passes.

**Commit:** `feat(tools): add copy_file and move_file workspace tools`

## 8. New `delete_file` tool — ✅ DONE (2026-08-10)

NOTES (2026-08-10): no CHANGELOG entry — the owner ratified that NO item in this plan touches
CHANGELOG.md. The item's conditional ("if the guard's disposition table needs a row") resolved to
NO new row and no code change in `internal/security`: ADR 0012's ruleset is precision-over-recall
(almost-never-legitimate AND catastrophic) and deleting one in-workspace file is ordinary
refactoring — the exact near-miss the shipped rules are written not to fire on, and a rule there
would gate normal work. What delete_file inherits instead is structural and already in place: its
argument is `path`, which is not a `payloadKey`, so the guard reads a delete target as inspectable
text and the shipped credential/persistence rules hard-refuse it in every mode ahead of the ladder;
`TestDeleteFile_DangerousActionClassification` pins both halves (ordinary path ⇒ TierNone, `~/.ssh`
⇒ TierHardRefuse). Beyond the item's literal text, the roster-facing set items 3/5/6/7 established
was updated so it stays true of the shipped suite: registration in `internal/tools/registry.go`
(which moved the registry tests' name lists and all four counts), `internal/tools/doc.go`, a card
entry in `internal/tui/toolpresent.go`, a probe row in `internal/tools/workspace_scoped_test.go`
(without it `TestWriteTargetProbesCoverEveryWriter` fails — every marker carrier needs one), and the
tool-suite counts in `docs/design/technical-design.md` (26 → 27, both places). One shape call the
item left open: the directory refusal is a real check, not just wording, because `os.Remove` would
unlink an EMPTY directory; a name swapped between the stat and the remove can therefore cost at most
one empty directory inside the fence, which is inside the blast radius the call already declared.

**What:** In `internal/tools/file_ops.go`, add a `delete_file` write tool taking
`path`. Files only: a directory target is refused with a clear IsError (ratified
call 8). Deletion goes through the pinned `os.Root` (fence at REMOVE time). The tool
carries the `workspaceScopedWriter` marker, and its classification follows the ADR 0012
dangerous-action guard policy — the implementer reads ADR 0012 and
`docs/design/confinement-execution-contract.md` and wires `delete_file` the way that
policy classifies destructive in-workspace actions; if the guard's disposition table
needs a row, adding it is in scope for this item. Missing file and path escape are
IsError results. Register in `internal/tools/registry.go`.

**Tests:** extend `internal/tools/file_ops_test.go`: deletes a file; refuses a
directory; missing file IsError; escape refused; guard classification asserted the way
existing guarded actions are tested; registry includes the tool.

**Acceptance:** `go test ./internal/tools/ -run TestDeleteFile -v` passes; `make check`
passes.

**Commit:** `feat(tools): add delete_file with dangerous-action guard classification`

## 9. New `run_tests` tool — auto-detected, condensed output

**What:** New file `internal/tools/run_tests.go`: a tool that detects the project's
test runner by markers — `go.mod` → `go test ./...`, pytest config (`pytest.ini`, or
`pyproject.toml` containing pytest configuration) → `pytest`, `package.json` with a
`test` script → `npm test` — in that precedence order (ratified call 6). Optional
params: `path` (subtree to test — passed to the runner in its native form) and `filter`
(test-name pattern, mapped to `-run` / `-k` / the npm equivalent). No marker found →
IsError telling the model which markers were looked for and suggesting `terminal`.
Execution goes through the same confined subprocess path as `terminal`
(`Confine(*exec.Cmd)` per the confinement execution contract), same non-read-only
disposition, with a bounded runtime. Output is CONDENSED text, not the raw stream:
overall PASS/FAIL, counts when the runner reports them, then up to 10 failing tests
with each failure's first error lines, then a truncation note with the total output
size. The whole result is hard-capped at 8192 bytes, and the schema description states
that the model gets a summary, never the full log — flooding small contexts is the
problem this tool exists to solve. Register in `internal/tools/registry.go`.

**Tests:** new `internal/tools/run_tests_test.go`: marker detection precedence
(fixture dirs); go-test success and failure condensing (run against a tiny fixture
module); no-marker IsError; filter mapping per runner (command construction asserted
without needing pytest/npm installed); output stays under the 8192-byte cap on a
noisy failure fixture AND on a verbose success fixture.

**Acceptance:** `go test ./internal/tools/ -run TestRunTests -v` passes; `make check`
passes.

**Commit:** `feat(tools): add run_tests with runner auto-detection and condensed output`

## 10. TODO.md records the deferred and denied tool-surface findings

**What:** Add a `## Tool-surface findings (4-poll round, 2026-08-10)` section to
`TODO.md` with these lists, content binding:

- *Bench experiments required before any tool removal* (models are unreliable narrators
  about their own tooling — the E4B poll preferred patch-only editing, the format small
  models are measurably worst at; and a repeat Qwen poll returned a substantially
  different list, so only REPLICATED findings count): (a) remove
  `single_find_and_replace` arm (flagged in all four polls); (b) patch-only vs
  find-replace editing arm (Qwen vs both Gemmas — falsifiable disagreement); (c)
  `open_file`/`read_file` merge — lean: keep `read_file`, add a `locate` parameter
  (Qwen chose `read_file` as survivor in both sessions); (d) measure whether sub-35B
  models use `view_diff` at all; (e) `web_fetch` → `http_request` merge — the real
  question is whether sub-35B models distinguish GET from POST; if they don't, the
  separate named GET tool earns its slot (both are ExternalEffect-classified, so
  gating doesn't decide it); (f) do sub-35B models ever discover
  `edit_existing_file`'s patch mode unprompted? — a discovery experiment feeding the
  explicit-patch-param idea.
- *Needs a grill session:* per-profile tool rosters (builds on the `tools.disabled`
  key from this plan); a unified `git` tool with subcommands vs the growing `git_*`
  family.
- *Deferred candidates:* env-var parameters on terminal/python_exec (stable across both
  Qwen sessions); `directory_create`/`directory_delete`; `git_stash`; `git_tag`;
  `file_metadata`; batch rename/replace operations; workspace_summary.
- *Engine-level notes (not tools):* context-window introspection for the model
  (Mechanism territory); streaming/progress for long-running tools; structured JSON
  tool outputs.
- *Denied, with reasons:* `database_query` (ADR 0031: no first-party connectors —
  MCP's job); standalone `apply_patch` (already exists inside `edit_existing_file`;
  models missing it is a discovery problem, tracked as the explicit-patch-param idea);
  concurrent `terminal` (parallelism lands at the sub-agent layer, ADR 0039);
  `inspect_environment` (`terminal` covers).
- *Method lessons:* (1) models reliably converge on PROBLEMS but not on SOLUTIONS —
  removals need measurement; (2) models discover capabilities by tool NAME, not by
  parameters (three sightings: list_dir recursion missed twice, edit patch mode missed
  once) — descriptions and naming are the discovery surface.

**Tests:** none (documentation).

**Acceptance:** `grep -q "Tool-surface findings (4-poll round, 2026-08-10)" TODO.md`
exits 0; `make check` passes.

**Commit:** `docs(todo): record 4-poll tool-surface findings, experiments, and denials`

---

**Suggested version bump:** minor (v0.12.9 → v0.13.0) once items 1–9 land — six new
tools, two schema extensions, a new config key, and a write-path robustness guarantee;
feature-level, not a patch. The owner decides; no item in this plan touches VERSION or
CHANGELOG.
