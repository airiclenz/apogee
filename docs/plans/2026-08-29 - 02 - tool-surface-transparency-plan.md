# Tool-surface transparency — implementation plan

**Goal:** close the four gaps a review of session `20260829T175710Z-230c75f9` (Qwen3.8-27B running
`/implement-plan` Write mode) surfaced: a search result and its transcript row never say WHERE the
search ran, a path-not-found refusal offers nothing to recover with, the stack facts the model
guessed wrong are written nowhere it reads, and a saved session keeps no tool-call arguments — so a
delegate's tool use cannot be reviewed after the fact. 7 items; CHANGELOG entries land at the
closeout from the sidecars.

**Date:** 2026-08-29
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `internal/tools/grep.go` (`renderMatches`, `Execute`), `internal/tools/find_files.go`
  (`renderFoundPaths`, `Execute`), `internal/tools/list_dir.go`, `internal/tools/path_read.go`
  (`readScope.resolve`, `readFileErrorMessage`, `escapeOrMessage`), `internal/tools/read_file.go`.
- `internal/tui/toolregistry.go` (`grepTarget` ~L991, `qualifiedTarget` ~L938, the `find_files`
  presenter entry ~L230), `internal/tui/toolview.go` (`toolView.args` ~L555–566 and its comment,
  the build path ~L700–724), `internal/tui/transcriptcodec.go` (`wireToolView` ~L182,
  `toWireToolView` ~L448, `fromWireToolView` ~L621, `TestTranscriptCodecGoldenV1`).
- `AGENTS.md` ("Where knowledge lives", "Conventions"); `ISSUES.md` ~L150–153 (the
  "deliberately not deferred" non-goals sentence); `docs/manual/sessions.md`.
- ADR 0022 (sessions persist per turn as dual-representation records), ADR 0052 (edit regions are
  tool-recorded), the read-fence contract in `docs/design/confinement-execution-contract.md`.
- Line numbers verified on 2026-08-29 against `main` at `90b437a2`; the symbol is the anchor.

**Ratified design calls (owner, 2026-08-29, via the plan-writer's questions):**
- **Scope:** items A (scope echo), B (transcript row), C (path suggestions), D (AGENTS.md stack
  facts), E (args on the wire) are all in.
- **Scope echo is unconditional:** every grep/find_files header and no-match line names the
  searched scope; an unscoped search says `in the workspace`; an include glob rides along.
- **Suggestions are prefix siblings, max 5:** entries of the parent directory whose name starts
  with the missing basename (case-insensitive), sorted, read through the fence; no parent → plain
  error. Applied to read_file, list_dir, grep, find_files.
- **Args persist as compact JSON, 4 KB cap per call:** oversize string fields elided to
  `…[N bytes]`; write/edit content fields elided entirely (Regions/Details already carry them).
- **Store only:** no new transcript rows; UI disclosure of persisted args is a later plan.

**Standing requirements:** `skills: coding-standards`; any authorized deviation from item text
lands as a dated NOTES line under the item; no version bump (see closing note).

**Out of scope:** surfacing persisted args in the TUI (▶ rows, `/inspect`); persisting child
conversations (still a non-goal); re-framing the child's system prompt (it inherits the parent's
delegate-advice paragraph and `sub_agent` itself — observed, not harmful in the reviewed session);
the ISSUES.md grouped-delegation ▶ item; the ESC double-tap feature the reviewed session was
planning.

**Regression check (2026-08-29, 90b437a2):** two independent reviewers, verdicts folded:
- 1: guard folded (writer's decision — `foundFilesStat` prefix match, toolpresent + pagination
  test re-pins; the toolregistry.go doc sentence stays true, reworded).
- 3: guard folded (`filepath.Dir`/`filepath.Base` on the `filepath.Rel` form; a `given` parameter
  carries the model's spelling).
- 4: guard folded (wire the read_file site at `readScope.readBounded`, `readFileErrorMessage`
  signature untouched); the read_file message yields to `path_read.go` ~L88–94 — it quotes the
  PINNED path, not the model's spelling.
- 6: guard folded (rule (4) decodes into `map[string]any` with `UseNumber` and re-`Marshal`s —
  sorted keys, HTML-escaped, byte-idempotent under `encodeTranscript`; `json.Compact` dropped).
- 7: guard folded (writer's decision — the field is `argsWire`, the func `wireArgs(...)`; both
  `presentToolCall` exits set it, with an unregistered-tool test row).
- 2, 5: SAFE.

---

## 1. grep and find_files name the scope they searched — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the Tests line's re-pin of `TestFindFiles_Execute_PaginatesWithTruncationNote` to `[5 files found in the workspace, showing 1-2]` omitted the glob clause the item's Exact forms and the ratified "an include glob rides along" call require; pinned to `[5 files found in the workspace (*.txt), showing 1-2]` (the call passes `pattern: "*.txt"`).

NOTES (2026-08-29): `TestGrep_SearchesAnExtraRootBySymlinkSpelling` / `TestFindFiles_SearchesAnExtraRootBySymlinkSpelling` compared the WHOLE content of a symlink spelling against the real spelling's; the header now legitimately echoes the caller's spelling, so both use a new `spelledLikeRealBelowTheHeader` helper (grep_test.go) pinning every row BELOW the header byte-for-byte — the invariant those tests exist for. `spelledLikeReal` and its list_dir/read_file callers are untouched.

NOTES (2026-08-29): the acceptance's `go test ./internal/tui/ -run 'TestToolPresent|TestFoundFiles'` matches no test — the re-pinned `find files states the empty case` row lives in `TestToolStat`. Verified instead with the whole `go test ./internal/tui/` package (ok) and `go test ./...` (all green).

**What:** `internal/tools/grep.go` `renderMatches` and `internal/tools/find_files.go`
`renderFoundPaths` gain the searched scope. Scope spelling is the `path` argument AS THE MODEL
GAVE IT (announced-path rule), `the workspace` when `path` is empty or `.`; an `include`
(grep) / `pattern` (find_files) glob list is appended in parentheses when set. Exact forms:
- grep header: `[39 total matches in internal/tui/model.go (*.go), showing 1-39]`; no-match:
  `No matches found in internal/tui/model.go (*.go)` — the `No matches found` prefix and the
  `[N total matches` / `, showing a-b]` shape are kept verbatim, only the scope clause is inserted.
- find_files header: `[12 files found in internal/tui (*.go), showing 1-12]`; no-match:
  `No files found in internal/tui (*.go)`; the truncation note is unchanged.
- The `(capped at N)` clause stays where it is, before the scope clause.
The scope is data inside the header row: pass it through `escapeRowBreaks` like a path. Thread the
scope into the two render funcs as parameters (they are pure; keep them so). `domain.MatchedLines`
totals are untouched — hosts read the number, never the sentence.

**Regression guard.** The find_files zero-hit stat in internal/tui/toolregistry.go (`foundFilesStat`,
~L747) matches the first line with strings.HasPrefix(head, "No files found") instead of an exact
compare, so the transcript row keeps its `0 files` slot; add internal/tui/toolregistry.go and
internal/tui/toolpresent_test.go to this item's Files, re-pin the toolpresent_test content (~L1573)
to `No files found in the workspace`, and name TestFindFiles_Execute_PaginatesWithTruncationNote in
Tests re-pinned to `[5 files found in the workspace, showing 1-2]`; the toolregistry.go ~L743-744
doc sentence stays true (the slot still reads the tool's own empty-result sentence) — reword it to
say the sentence is matched by its `No files found` prefix. Acceptance adds
`go test ./internal/tui/ -run 'TestToolPresent|TestFoundFiles'`.

**Files:** `internal/tools/grep.go`, `internal/tools/find_files.go`, `internal/tools/grep_test.go`,
`internal/tools/find_files_test.go`, `internal/tui/toolregistry.go`, `internal/tui/toolpresent_test.go`

**Tests:** update the pinned strings in `TestGrep_Execute_ReportsMatchCount`,
`TestGrep_Execute_ContextLines*`, `TestGrep_Execute_ExcludesNoiseDirs`, the find_files
`No files found` assertions and `TestFindFiles_Execute_PaginatesWithTruncationNote` (re-pinned to
`[5 files found in the workspace, showing 1-2]`); re-pin the `find files states the empty case`
row in `internal/tui/toolpresent_test.go` (~L1573) to `No files found in the workspace` and keep
its `0 files` expectation; add one table each for grep and find_files covering: unscoped
(`the workspace`), a subdirectory path, a single-file path (grep), a glob, path + glob, and a
zero-match scoped search — asserting the exact header line.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestGrep|TestFindFiles' && go test ./internal/tui/ -run 'TestToolPresent|TestFoundFiles'`

**Commit:** `feat(tools): grep and find_files name the scope they searched`

## 2. The transcript row for grep and find_files shows the path scope

**What:** `internal/tui/toolregistry.go`: `grepTarget` renders `pattern · <path> · <include>`
(each qualifier only when present and `path` not `.`), via `qualifiedTarget` chained; add a
`findFilesTarget` of the same shape (`pattern · <path>`) and point the `find_files` presenter's
`target` at it (today `stringArg("pattern")`). The path is a display path: it goes through the
same `shortenPaths`/`finishDisplay` seam every target already does (no new sanitising code).

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolregistry_test.go` (new)

**Tests:** `TestGrepTarget` / `TestFindFilesTarget` tables: pattern only; pattern+path;
pattern+include; all three; `path: "."` omitted. One transcript-level test in the new file builds a
grep `ToolCallEvent` with `{"pattern":"KeyMsg","path":"internal/tui/model.go"}` and asserts the
rendered branch line contains `KeyMsg · internal/tui/model.go`.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TestGrepTarget|TestFindFilesTarget|TestModelNoBuilderByValue'`

**Commit:** `feat(tui): grep and find_files rows show the path they were scoped to`

## 3. A fenced "did you mean" helper for missing paths

**What:** new `internal/tools/path_suggest.go`: `suggestSiblings(root, rel string) []string` —
given the root a path was ACCEPTED under (`readScope.resolve` already answered that) and the
root-relative missing path, open the parent directory through the fence (`security.SafeOpen`
on `path.Dir(rel)`, `ReadDir(-1)` as `list_dir.collectEntries` does), keep entries whose name
starts with `path.Base(rel)` case-insensitively, sort by name, cap at 5, and return them spelled
as the model's parent spelling joined with the entry name (directories with a trailing `/`). A
parent that is missing, unreadable, or refused by the fence yields nil. A companion
`notFoundMessage(prefix, given string, suggestions []string) string` renders
`path not found: docs/adr/0025 — did you mean: docs/adr/0025-interjections-….md` (`; `-joined
when several; no suffix when none). Suggestions NEVER accompany a fence refusal: the helper is
only reached after `resolve` accepted the path, so `escapeOrMessage`'s escape branch is untouched.
Binding standard: one deep helper, no per-tool copies; the entry names are data and pass through
`escapeRowBreaks`.

**Regression guard.** `rel` comes from `workspaceRelative` = `filepath.Rel` (`internal/security/pathsafety.go`
~L65), separator-native on Windows (a shipped GOOS): split it with `filepath.Dir`/`filepath.Base`,
never `path.Dir`/`path.Base`, and hand `SafeOpen` the filepath form (`list_dir.go` ~L84 already
does). The helper receives the model's spelling explicitly — signature
`suggestSiblings(root, rel, given string) []string`, joining entries onto `Dir(given)` — since
`(root, rel)` alone never carries it.

**Files:** `internal/tools/path_suggest.go`, `internal/tools/path_suggest_test.go`

**Tests:** `TestSuggestSiblings` (builds `rel` with `filepath.Join`, passes `given` explicitly and
asserts the results are joined onto the given parent spelling): prefix hit (several, sorted, capped
at 5 of 7), case-insensitive hit, no hit → nil, missing parent → nil, directory entries carry `/`, a sibling that is a symlink
out of the root is listed by name only (the fence refuses it later, never here). `TestNotFoundMessage`
pins the three renderings (none / one / several).

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestSuggestSiblings|TestNotFoundMessage'`

**Commit:** `feat(tools): a fenced sibling-suggestion helper for path-not-found refusals`

## 4. read_file, list_dir, grep and find_files suggest siblings on a missing path

**What:** Depends on item 3. Wire `suggestSiblings` + `notFoundMessage` into the four not-found
sites: `internal/tools/grep.go` (`path not found:` after `os.Stat`), `internal/tools/find_files.go`
(same site), `internal/tools/list_dir.go` (`directory not found:` — both the `safeOpen` and the
`Stat` sites; keep the `not a directory:` refusal as is), `internal/tools/path_read.go`
`readFileErrorMessage` (`file not found:` — only on the non-escape branch of `escapeOrMessage`,
and only when the pinned path's parent exists). The message prefixes (`path not found:`,
`directory not found:`, `file not found:`) stay byte-identical; the suffix is appended. Every
site passes the root `resolve`/`locate` accepted and the model's own spelling of the path.

**Regression guard.** `readFileErrorMessage` (`path_read.go` ~L95) holds no root and is shared by
`find_replace.go` (~L104, ~L231), `file_edit.go` (~L91) and, via `readWorkspaceFileBounded`, `diff.go`
(~L79): wire the read_file site at `readScope.readBounded` (`path_read.go` ~L236–243, read_file's
only caller, which holds root and target) and leave `readFileErrorMessage`'s and
`readWorkspaceFileBounded`'s signatures alone, so the write/edit/diff refusals gain no suffix. The
read_file message yields to `path_read.go` ~L88–94: it quotes the PINNED path, not the model's
spelling — suggestions there are joined onto the pinned path's parent.

**Files:** `internal/tools/grep.go`, `internal/tools/find_files.go`, `internal/tools/list_dir.go`,
`internal/tools/path_read.go`, `internal/tools/grep_test.go`, `internal/tools/find_files_test.go`,
`internal/tools/list_dir_test.go`, `internal/tools/read_file_test.go`

**Tests:** per tool, one test that creates `docs/adr/0025-interjections.md` and asks for
`docs/adr/0025`, asserting the exact message
`path not found: docs/adr/0025 — did you mean: docs/adr/0025-interjections.md` (prefix per tool);
one that asks for a path whose parent is missing and asserts the old message unchanged; the
existing escape tests (`TestGrep_Execute_ToolErrors`, `TestFindFiles_Execute_RefusesPathEscape`,
read_file "path escape is a tool error") still pass unchanged — a refusal carries no suggestions;
the read_file case asserts the message quotes the pinned path, and `single_find_and_replace`,
`edit_file` and `diff` missing-file refusals stay byte-identical (existing tests unchanged).

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestGrep|TestFindFiles|TestListDir|TestReadFile'`

**Commit:** `feat(tools): missing-path refusals suggest sibling entries the model can use`

## 5. AGENTS.md states the stack facts the model guessed wrong

**What:** two edits to `AGENTS.md`. In "Where knowledge lives" change the `layout.md` bullet to:
``- `layout.md` (repo root, not `docs/`) — the TUI layout/rendering spec in prose.``
In "Conventions not derivable from the code" add, directly before the value-copied `Model`
bullet:
``- **Stack facts:** the TUI is Bubble Tea **v2** (`charm.land/bubbletea/v2`): key events are `tea.KeyPressMsg` matched on `msg.String()` (`"esc"`, `"ctrl+c"`); the v1 names (`tea.KeyMsg`, `tea.KeyCtrlC`) do not exist here. Sub-agent briefs that name an API must use the v2 names.``
No other line changes; `CLAUDE.md` stays the `@AGENTS.md` stub.

**Files:** `AGENTS.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -n 'Bubble Tea \*\*v2\*\*' AGENTS.md && grep -n 'layout.md. (repo root' AGENTS.md`

**Commit:** `docs(agents): state the Bubble Tea v2 names and where layout.md lives`

## 6. A bounded, compact wire form of a tool call's arguments

**What:** new `internal/tui/wireargs.go`: `wireArgs(tool string, raw json.RawMessage) json.RawMessage`
producing the argument JSON a saved transcript keeps. Rules, in order: (1) invalid or empty JSON →
nil; (2) for the write/edit tools the content-carrying keys are dropped entirely — the keys are
exactly those the tools' schemas in `internal/tools/*.go` name for file content / patch text /
replacement pairs (implementer reads them off `write_file`, `edit_existing_file`,
`single_find_and_replace`, `multi_find_and_replace` schemas) — because Regions/Details already
carry the diff (ADR 0052); (3) any remaining string value over 1 KB becomes `…[N bytes]`; (4) the raw JSON is decoded into a
`map[string]any` (a `json.Decoder` with `UseNumber`, so a large int is not re-spelled as a float),
(2)/(3) are applied to the map, and the map is `json.Marshal`led — sorted keys, HTML-escaped,
byte-idempotent under the outer encoder; (5) if it
still exceeds 4 KB the whole value becomes `{"elided":"N bytes"}`. Constants `wireArgsFieldCap`
(1024) and `wireArgsCap` (4096). Pure function, no display sanitising (it is never painted —
store only per the ratified call).

**Regression guard.** `encodeTranscript`'s `json.Marshal` (`internal/tui/transcriptcodec.go` ~L366)
re-compacts every `RawMessage` with HTML escaping, so the wire form must already be what that
encoder emits: decode → map → `json.Marshal` (rule (4)); never `json.Compact`, which neither sorts
keys nor escapes `<>&`, and would make item 7's encode→decode compare fail on raw model bytes.

**Files:** `internal/tui/wireargs.go`, `internal/tui/wireargs_test.go`, `internal/tui/doc.go`

**Tests:** table: small grep args pass through compact and key-sorted; unsorted keys and a string
holding `<` come out sorted and `\u003c`-escaped, and re-marshalling the result as a `RawMessage`
field is byte-identical; a large integer survives unchanged (`UseNumber`); `write_file` `content`
dropped while `path` survives; a 2 KB `command` string elided to `…[2048 bytes]`; a 20-key
payload over 4 KB collapses to `{"elided":"N bytes"}`; invalid JSON → nil; `doc.go` file map names
`wireargs.go` (`docmap` guard).

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TestWireArgs|TestDocMap|Docmap'`

**Commit:** `feat(tui): a bounded compact wire form of tool-call arguments`

## 7. Every tool card keeps its arguments on the wire

**What:** Depends on item 6. `wireToolView` gains `Args json.RawMessage \`json:"args,omitempty"\``;
`toolView` gains `argsWire json.RawMessage` set at build time from the call's raw
`Arguments` via `wireArgs(name, raw)` — for EVERY call at every depth, independent of the
presenter-only `args` map, which keeps its current lifecycle. `toWireToolView` copies it;
`fromWireToolView` restores it (nothing reads it on replay yet — store only). Supersede the
`toolView.args` comment sentence "It is deliberately not on the wire either" with the new fact
(bounded args ride the wire; the presenter map still does not). Amend `ISSUES.md` ~L151: the
non-goals sentence keeps "sub-agent session persistence" and adds "(tool-call arguments ARE
recorded per card, bounded, since plan 2026-08-29 - 01)". `docs/manual/sessions.md`: one sentence
under what a record stores — each tool card keeps a bounded copy of the call's arguments. Additive
member, no `transcriptVersion` bump (the wireEntry rule: omitempty, old readers ignore it).

**Regression guard.** The toolView field is named `argsWire` (not `wireArgs`) so it does not share the
package-level func's identifier from item 6; every mention in the item's What and Tests uses
`argsWire` for the field and `wireArgs(...)` for the func. `presentToolCall` has two exits — the
unregistered-tool branch (`toolview.go` ~L666–673) and the registered build (~L675–724): set
`tv.argsWire` before each `finishDisplay` exit, so an MCP call (e.g. `tail_log`) keeps its args too.

**Files:** `internal/tui/transcriptcodec.go`, `internal/tui/toolview.go`, `internal/tui/transcript.go`,
`internal/tui/transcriptcodec_test.go`, `ISSUES.md`, `docs/manual/sessions.md`

**Tests:** `TestTranscriptCodecGoldenV1` unchanged (no args → byte-identical); a new golden case
with a grep card carrying `{"pattern":"KeyMsg","path":"internal/tui/model.go"}` pins
`"args":{"path":"internal/tui/model.go","pattern":"KeyMsg"}` in the entry; a round-trip test
encodes → decodes and compares `argsWire`; a depth-1 (delegate) card carries args too; an
unregistered tool name with `{"a":"b"}` round-trips its args; a blob without `args` decodes with nil.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TestTranscriptCodec|TestModelNoBuilderByValue' && go test ./internal/session/`

**Commit:** `feat(tui): tool cards persist a bounded copy of their arguments in the session record`

---

**Suggested version bump:** micro (`0.18.6` → `0.18.7`) at closeout — model-facing tool output
changes (items 1, 4) and a new session-record member (item 7); the owner decides.
