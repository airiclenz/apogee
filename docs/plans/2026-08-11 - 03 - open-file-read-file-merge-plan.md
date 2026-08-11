# Merge open_file into read_file — plan

**Goal:** `read_file` gains an optional `locate` parameter (reporting the absolute 1-based
line numbers where a substring occurs); the `open_file` tool, its `domain.OpenedFile`
summary variant, and their TUI presenters are deleted; docs record the merge.

**Date:** 2026-08-11 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources** (all file:line cites in this plan are pinned at commit
`a0072a0`; if lines have drifted, the pinned commit's content is the ground truth):

- `internal/tools/open_file.go` @ a0072a0 — the behavior being merged (locate scan loop
  :107-111, "Located" wording :113-119, empty-vs-miss semantics :94-97).
- `internal/tools/read_file.go` @ a0072a0 — the surviving tool (range logic :91-114,
  header :116-117, ReadSpan :118).
- `TODO.md:769-770` — Tool-surface findings item (c), the recorded merge lean.
- `docs/plans/archived/2026-08-10 - 00 - tool-surface-improvements-plan.md` item 10 —
  where that lean was recorded.
- `docs/layout/tool-layout.md:232-233` — the ratified display rows for both tools.

**Ratified design calls:**

1. **Owner, 2026-08-11:** merge NOW, skipping the bench experiment item (c) prescribes —
   an owner-ratified exception to the bench-first rule; the rule itself stands for the
   other item-10 candidates.
2. **Owner, 2026-08-11:** `locate` always scans the WHOLE file and reports absolute
   1-based line numbers, even when `start_line`/`end_line`/`max_lines` narrow the
   returned content. A match outside the returned span is still reported.
3. **Owner, 2026-08-11:** `"open_file"` STAYS in the mechanisms' `readSpellings` family
   (`internal/mechanisms/decompose.go:140`) as a retired spelling models may still emit
   (precedent: `"readFile"`, which was never a registered tool).
4. **Author, 2026-08-11:** the locate facts fold into `domain.ReadSpan` (new fields);
   `read_file` never carries a second summary variant; `domain.OpenedFile` is deleted
   with the tool.
5. **Author, 2026-08-11:** `read_file`'s TUI label stays `Read`/`reading`; the locate
   display mirrors open_file's exactly — `· locate "…"` target qualifier plus the
   single `Located "…" on lines: …` / `on no lines` body line, wording verbatim.
6. **Author, 2026-08-11:** `read_file`'s tool description must advertise locate
   explicitly — small models discover capabilities by name, not by reading parameter
   schemas (2026-08-10 poll method lesson), so the description carries the affordance
   the retired name used to.

**Standing requirements:**

- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No item changes VERSION, a CHANGELOG release heading, or any tag (see closing note).

**Out of scope:**

- Every other tool-surface item-10 candidate — (a) single_find_and_replace, (b)
  patch-only editing, (d) view_diff usage, (e) web_fetch/http_request, (f) patch-mode
  discovery — all still bench-gated.
- `tools.disabled` unknown-name handling: a config still listing `open_file` after the
  merge starts fine, disables nothing, and prints the existing startup notice
  (`internal/tools/registry.go:25-34`, `internal/config/config.go:723-755`). Already
  the designed behavior; no code change.
- Historical CHANGELOG entries and archived docs mentioning open_file (append-only
  record; ~50 mentions stay).
- Per-profile tool rosters; any release act.

## 1. read_file gains `locate` — schema, rendering, ReadSpan fields — ✅ DONE (2026-08-11)

NOTES (2026-08-11): `LocatedOn []int` makes `ReadSpan` uncomparable, so the pre-existing
`TestReadFile_Execute_ReportsTheSpanItRendered` whole-struct `==` (read_file_test.go:214) no
longer compiled and became `reflect.DeepEqual`. The `toolresult.go` cite did shift (the new
`strconv` import moved start_line/end_line by one) and is updated to `read_file.go:21-22`.

**What:**

- `internal/tools/read_file.go`:
  - `readFileSpec` schema gains optional `locate` (string): "Optional substring to
    locate; the result reports the absolute 1-based line numbers where it occurs.
    The whole file is always scanned, even when a line range narrows the returned
    content." `readFileArgs` gains `Locate string`.
  - The spec `description` becomes: "Read the contents of a file by path, optionally
    restricted to a line range, and optionally locating the line numbers where a
    substring occurs." (design call 6).
  - `renderFile`: when `Locate != ""`, scan the FULL split body (design call 2) with
    `strings.Contains` per line, mirroring `open_file.go:107-111`, and emit one line —
    `Located %q on lines: 5, 9` or `Located %q on no lines`, wording verbatim from
    `open_file.go:113-119` — on its own line directly after the header, before the
    content. Empty `locate` = not requested: output byte-identical to today.
- `internal/domain/toolsummary.go`: `ReadSpan` (:43-47) gains `Locate string` and
  `LocatedOn []int` with open_file's semantics documented on the fields (empty Locate =
  none requested; set Locate with empty LocatedOn = requested, matched nothing — from
  `open_file.go:94-97`). The `apogee.go` ReadSpan facade alias needs no edit.
- `internal/context/toolresult.go:32` cites `read_file.go:18-19` by line number in a
  comment; update the cite if the schema edit shifts those lines.

**Tests** (`internal/tools/read_file_test.go`):

- locate hit → "Located" sentence lists absolute 1-based lines; summary `Locate`/
  `LocatedOn` match the sentence.
- locate miss → `on no lines`, summary has set Locate + empty LocatedOn.
- no locate → no "Located" line; header and body byte-identical to pre-change output.
- locate + `start_line`/`end_line` where the only match lies OUTSIDE the returned
  span → still reported, absolute numbers (design call 2).

**Acceptance:**

- `go build ./...`
- `go test ./internal/tools/ ./internal/domain/ ./internal/context/`

**Commit:** `feat(tools): read_file gains a locate parameter reporting absolute line numbers`

## 2. TUI presents read_file's locate — target qualifier and Located body line — ✅ DONE (2026-08-11)

NOTES (2026-08-11): line cites had drifted (registry entry :460-467, `readFileTarget` :1497,
`openFileBody` :1899; the tool-layout read_file row is :249, not :232) — content matched, approach
unchanged. Two tests added beyond the four listed: an outcome-split row pinning that a located
read's body is the Located line ALONE (the "never the content" half, which the substring-matching
table cannot assert), and `TestReadFileBodyRecordsTheLocateReport` — the mirror of the open_file
unit test item 4 deletes, keeping its clipping and wrong-summary coverage.

Depends on item 1.

**What:**

- `internal/tui/toolpresent.go`:
  - `toolRegistry["read_file"]` (:447-453): `readFileTarget` (:1488-1502) additionally
    appends `· locate "…"` via `qualifiedTarget` when the call carries locate,
    composing with the existing range form — both present renders as
    `path:12–80 · locate "…"` (en dash range as today).
  - Add `body: readFileBody`, mirroring `openFileBody` (:1886-1899) but reading
    `domain.ReadSpan`: when `Locate != ""`, exactly one clipped detail line
    (`Located "x" on lines: 5, 9` / `on no lines`); `nil` when `Locate == ""` —
    unchanged rendering for locate-less reads. Label `Read`, verb `reading`, stat
    `readSpanStat` all unchanged (design call 5).
- `docs/layout/tool-layout.md` read_file row (:232): target column becomes
  ``path (`:12–80` when ranged, `· locate "…"` when set)``; body column becomes
  `the returned content + located line numbers`.

**Tests** (`internal/tui/toolpresent_test.go`), mirroring the open_file cases at
:219-249:

- locate hit → the Located line, never the content.
- locate miss → `on no lines`.
- no locate → stat only, no body rows.
- target with range AND locate → `path:12–80 · locate "…"`.

**Acceptance:**

- `go build ./...`
- `go test ./internal/tui/`

**Commit:** `feat(tui): read_file presents its locate term and located lines`

## 3. Deregister and delete the open_file tool — ✅ DONE (2026-08-11)

NOTES (2026-08-11): three registry_test.go count assertions beyond the enumerated ones needed the
same −1 (`:87` Asker 27→26, `:110` Presenter 27→26, `:116` both 28→27) — the item's own acceptance
fails without them. Two comments naming the deleted tool were also corrected: `registry_test.go:221`
("diff/open-file read" → "diff reads") and `internal/agent/resolution.go:247` (dropped `open_file`
from the no-marker example list; no item enumerates that file). Left untouched:
`docs/design/confinement-execution-contract.md:312`, which names `open-file` inside a historical
"P3.7 adds" record — the same append-only category the plan puts CHANGELOG history in.

Depends on item 1 (the roster must never lose the locate capability).

**What** (deletion-heavy; every file enumerated, no new logic):

- Delete `internal/tools/open_file.go` and `internal/tools/open_file_test.go` (its
  fence pins at :167-296 duplicate existing `read_file_test.go` twins over the shared
  `readWorkspaceFileBounded` path — nothing is lost).
- `internal/tools/registry.go:123`: remove the `NewOpenFile(root)` slot.
- `internal/tools/registry_test.go`: membership list (:21), `All()` count 26→25 (:36),
  menu-order list (:55), `IsReadOnly` map (:226).
- `internal/tools/find_replace_test.go:297,308`: drop `NewOpenFile` from the
  no-writer-marker assertion.
- `internal/tools/doc.go`: "Exactly SEVEN built-ins attach one" (:21-24) → six, drop
  open_file; the "read-and-locate open_file" mention (:34) folds into read_file's;
  "Twenty-three files" (:140) → twenty-two; file map (:146-147) drops open_file.go and
  describes read_file.go as the line-spanned, locate-capable read.
- `internal/agent/dispatch_test.go:174`: remove the open-file classification row.
- `internal/agent/planmenu_test.go:141`: remove open_file from the Plan-menu floor list.
- `internal/tui/toolsummary_pin_test.go`: remove the open_file pin case (:113-116),
  count 7→6 (:118-121), registered-names list (:150) → six.

Binding behavior note: no `tools.disabled` code changes — unknown names are already a
startup notice, not an error (see Out of scope).

**Tests:** the edited registry/pin tests ARE the tests; no new ones.

**Acceptance:**

- `go build ./... && go test ./internal/tools/ ./internal/agent/ ./internal/tui/`
- `grep -rn "NewOpenFile" internal/ cmd/ apogee.go` → no matches.

**Commit:** `feat(tools): remove open_file now that read_file carries locate`

## 4. Delete domain.OpenedFile and its remaining consumers

Depends on items 1 and 3.

**What:**

- `internal/domain/toolsummary.go`: delete `OpenedFile` (:79-87); variant-count doc
  (:37-38) seven → six.
- `internal/domain/toolsummary_test.go`: membership assertion (:18), variant list
  (:34), `const want = 7` (:37) → 6.
- `apogee.go:313-315`: delete the `OpenedFile` facade alias.
- `internal/tui/toolpresent.go`: delete the `toolRegistry["open_file"]` entry
  (:515-522), `openedLinesStat` (:1158-1167), `openFileTarget` (:1505-1512),
  `openFileBody` (:1878-1899); re-point the doc comments naming open_file as the
  typed-summary body example (:395-396, :431, :1059, :1873) at read_file, which owns
  the body hook since item 2.
- `internal/tui/toolpresent_test.go`: remove the open_file presenter cases (:219-249 —
  superseded by item 2's read_file mirrors), the `openedLinesStat` row (:1155), the
  stat-declines entry (:1226), and `TestOpenFileBodyRecordsTheLocateReport`
  (:1245-1265).
- `internal/agent/hookrun.go:310-312`: the uncomparable-summary prose example becomes
  ReadSpan (uncomparable since item 1 via `LocatedOn []int`).
- `internal/agent/hookrun_test.go` (:4-6, :18-21, :99, :153): rebuild the
  `openedFileSummary()` helper on `domain.ReadSpan` with `LocatedOn` set. Binding: the
  regression pin for the uncomparable-summary panic MUST survive and still exercise a
  slice-bearing summary — do not delete or weaken it.
- `internal/mechanisms/decompose.go`: `:138` comment re-pointed from
  `open_file.go renderOpenFile` to read_file's render; `:140` `readSpellings` KEEPS
  `"open_file"` (design call 3) — add a comment noting it is a retired tool name kept
  as a model-emitted spelling, like `"readFile"`.

**Tests:** the edited ones; the hookrun pin passing against a ReadSpan-with-LocatedOn
summary is the item's key evidence.

**Acceptance:**

- `go build ./... && go test ./internal/domain/ ./internal/tui/ ./internal/agent/ ./internal/mechanisms/`
- `grep -rn "OpenedFile" internal/ cmd/ apogee.go` → no matches in code (CHANGELOG and
  archived docs excluded).

**Commit:** `refactor(domain): drop the OpenedFile summary variant and its presenters`

## 5. Docs closeout — CONTEXT.md, tool-layout, TODO item (c), CHANGELOG

Depends on items 3 and 4.

**What:**

- `CONTEXT.md:856-858` (Tool summary glossary entry): seven → six, drop `open_file`
  from the built-ins list; note that `read_file`'s summary now also carries the locate
  facts.
- `docs/layout/tool-layout.md:233`: delete the open_file row (the read_file row was
  updated by item 2).
- `TODO.md:769-770`, item (c): close it per the file's house style for resolved
  entries, recording — resolved 2026-08-11 by owner call; merge shipped WITHOUT the
  bench experiment (owner-ratified exception to the bench-first rule); `read_file`
  kept, `locate` added, `open_file` removed; watch-item: the untested discovery risk —
  whether sub-35B models find a locate PARAM as readily as they found an open_file
  NAME. The section intro's standing bench-first rule (:761-765) stays untouched for
  the remaining candidates.
- `CHANGELOG.md` under `[Unreleased]`: Changed — `read_file` gains `locate`
  (whole-file scan, absolute 1-based line numbers). Removed — `open_file`, merged into
  `read_file`; a `tools.disabled` entry naming it now draws the standard unknown-name
  startup notice. No release heading is added or moved.

**Tests:** none (docs only).

**Acceptance:**

- `go build ./...`
- `grep -n "open_file" CONTEXT.md TODO.md "docs/layout/tool-layout.md"` → only the
  TODO closed-entry record (and the standing-rule intro if it names no tool).

**Commit:** `docs: record the open_file merge into read_file across CONTEXT, layout, TODO and CHANGELOG`

## Suggested version bump

Micro bump (v0.12.0 → v0.12.1): a model-visible tool-surface change (one tool removed,
one parameter added) matching the "VERSION micro-bumps per shipped feature" policy. Not
performed by any item — the owner decides after execution.
