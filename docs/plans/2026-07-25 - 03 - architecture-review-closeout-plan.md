# Plan — Close the architecture deepening review: structured tool results, one Event fold, four small deepenings

**Date:** 2026-07-25
**Status:** READY (scope and the two shape decisions resolved with the owner 2026-07-25).
**Source:** the **whole remaining** ledger of
`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md` — candidate **03**
("Hand the view structured tool results", *Strong*, the review's own top remaining pick),
candidate **06** ("Decode each engine Event once", *Worth exploring*), and the **four**
smaller deepenings still open (`workspaceWriteTarget` helper, `read_file` →
`SafeStat`/`SafeReadFile`, the POSIX `Confine` argv-wrap helper, the self-regulator read
model). Candidates 01, 02, 04, 05 and 07 landed 2026-07-24/25; the session-store lifecycle
was absorbed by ADR 0022. **When this plan is done the review's ledger is empty.**
**Track:** post-`v0.8.0` architecture deepening — the fifth and final card set off the
2026-07-24 review.
**Public API:** items 1–5 are **additive** to the public surface (`apogee.ToolSummary` plus
seven variants; one new optional field on `apogee.ToolResult`) — a **minor** bump under
ADR 0010, back-compatible with ADR 0002's open tool extension point. Items 6–10 are
`internal/` only and change no exported name.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                                              # empty
go vet ./... && go test ./... && go test -race ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Item 9 only** adds two commands, because roughly half of what it touches is
`//go:build !windows` and the seatbelt half never compiles as `GOOS=linux`:

```
GOOS=darwin go vet ./internal/platform/...
GOOS=darwin go test -c -o /dev/null ./internal/platform
```

**Items are independent below the group boundary.** 1 → 2 → 3 → 4 → 5 is a chain (03);
6 stands alone (06); 7, 8, 9, 10 stand alone and in any order. `/implement-plan` may stop
after any completed item and the tree is coherent.

---

## The problem (grounded, verified 2026-07-25 against the working tree)

### A. The view scavenges prose to find out what a tool did (candidate 03)

`internal/tui/toolpresent.go` holds a **21-entry** name-keyed registry (`toolRegistry`,
L110–237 — the review's "24-entry" figure **overcounted**; the keys are `read_file`,
`write_file`, `list_dir`, `grep`, `single_find_and_replace`, `multi_find_and_replace`,
`edit_existing_file`, `view_diff`, `open_file`, `terminal`, `python_exec`, `git_branch`,
`git_commit`, `git_diff_range`, `diagnostics`, `web_fetch`, `http_request`, `web_search`,
`sub_agent`, `ask_user`, `present_document`). Seven of those entries reconstruct a **fact the
tool already knew** by pattern-matching the free-text result string that was written for the
model:

| Fact | Where the view re-derives it | Where the tool computed it |
| --- | --- | --- |
| read range | `reReadRange` ✓L243 → `detailFromPattern` L365 | `read_file.go` L117 `showing lines %d-%d` |
| bytes written | `reWriteBytes` ✓L244 | `write_file.go` L89 `wrote %d bytes to %s` |
| entries listed | `reListEntries` ✓L245 | `list_dir.go` L154 `[%d entries total%s]` |
| matches found | `reGrepMatches` ✓L246 + a `"No matches"` prefix test (`grepDetail` L377–386) | `grep.go` L254 header, L230 sentinel |
| diffstat | `diffDetail` L449–483 counts leading `+`/`-` over the rendered diff | `diff.go` `unifiedLineDiff` L83 builds the ops it renders |
| search hits | `reSearchHit` L434 counts `^\d+\. ` lines (`searchDetail` L426) | `web_search_render.go` `renderStructuredResults` L148 numbers them |
| open-file outcome | `openFileDetail` L488–498 tests `lines[1]` for a `"Located "` prefix, else counts `len(lines)-2` | `open_file.go` `renderOpenFile` L90–107 |

It is a **stringly-typed cross-package contract with no type**: `internal/tui` does not import
`internal/tools` (verified — only `e2e_test.go` and `smoke_live_test.go` do), so nothing
connects the two ends. A wording change in `internal/tools` silently degrades a card to its
verbatim first line, with no compiler nudge and no failing test in the package that changed.

Two aggravating facts found this session, both worth naming because they argue the same way:

- **The header the regexes depend on is not guaranteed to survive.** `internal/agent`
  `appendToolResult` (`dispatch.go` L414–422) rewrites `result.Content` through
  `clampToolResult` **before** emitting `ToolResultEvent`. Today the head/tail elision happens
  to keep the first line, so the regexes happen to keep matching. Correctness rests on a
  coincidence between a compression policy and a display regex.
- **The `PostToolResult` hook hands Mechanisms a `*domain.ToolResult`** and
  `errorenrich.go` L112 already rewrites `Content` on that seam. Any future Mechanism that
  prepends to a successful result breaks the seven extractors at once.

### B. One Event, three switches, ordering by comment (candidate 06)

`model.go` L259–265 folds each engine Event three times — `foldStats` (L1481), then
`transcript.apply` (`transcript.go` L231), then `foldActivity` (`activity.go` L158). The
order is **load-bearing and enforced by prose**: `foldActivity`'s `ToolResultEvent` case reads
`m.transcript.hasOpenToolCall()` (`activity.go` L176 → `transcript.go` L359), which only
`apply` establishes. Three comments say so (`model.go` L262–264, `activity.go` L148–149,
`transcript.go` L354). The Event set has grown 8 → 11 additively; a twelfth variant needing
two folders needs two edits in two files with no compiler nudge, and `model.go` is **1,772
lines**, so `foldStats` sits in a god-file it has nothing to do with.

### C. The four smaller deepenings, re-verified

1. **`workspaceWriteTarget` is four identical bodies.** `write_file.go` L52, `find_replace.go`
   L69 and L186, `file_edit.go` L51 — each decodes its own args struct and calls
   `resolveTargetUnbounded(args.Path, t.root)` (`workspace_scoped.go` L58). All four args
   structs spell the field `json:"path"` (`write_file.go` L25, `find_replace.go` L42 and L162,
   `file_edit.go` L27). The **methods** must stay per-type (the marker is a method set,
   contract §3.2) — only the bodies collapse.
2. **`read_file` never adopted the TOCTOU-safe primitive written for it.**
   `security.SafeStat`'s doc comment (`safeio.go` L101) literally says *"the stat-then-read
   discipline the read_file tool uses"* — and `read_file.go` L65–85 still does
   `resolveInRoot → os.Stat → os.ReadFile`, re-walking the path with a check/use gap.
   `open_file.go` L65–84 is the **same trio, byte for byte**; the review named only
   `read_file`, but leaving its twin behind would be a half-fix (see D9).
3. **landlock and seatbelt share a verbatim argv-wrap skeleton.** `landlock_linux.go`
   L136–176 and `seatbelt.go` L97–132: the same empty-argv guard, the same
   resolved-`cmd.Path`-with-`Args[0]`-fallback dance, the same `orig := append([]string{prog},
   cmd.Args[1:]...)` rewrite, the same `SysProcAttr.Setpgid` block with the same two-line
   comment. Only the launcher and its prefix args differ.
4. **The self-regulator has no read seam.** `selfreg.go` L85–100 keeps `strikes`,
   `suppressed`, `budgetTripped`, `harmfulStreak` unexported with no accessor; **32** test
   sites reach through `r.` / `a.tracker.` to assert on them (`selfreg_test.go`).

---

## Decisions (resolved with the owner 2026-07-25)

- **D1 — the summary is a SEALED sum type in `internal/domain`, in its own file.**
  `domain.ToolSummary` is an interface with an **unexported** marker method, exactly like
  `domain.Event` (`events.go` L17–19). Variants are plain structs in
  `internal/domain/toolsummary.go`. Rejected: a flat `struct{Kind; A, B int; Text string}` (the
  field meanings would depend on `Kind`, so the view keeps interpreting rather than reading)
  and a `map[string]string` (re-creates the stringly contract one level up).
  **Consequence, accepted:** an embedder's own tool can never emit a summary — the sum is
  Apogee's. It **can read** every variant (the root re-exports the concrete types), which is
  what feeds a future headless/bench host. A third-party tool that emits none renders exactly
  as it does today. Unlike `EventBase` there is **no exported base struct**: each variant
  carries its own one-line marker method, because an exported embeddable base would re-open
  the sum.
- **D2 — seven tools, and only seven.** Summaries go to the tools whose outcome the view
  currently **re-derives**: `read_file`, `write_file`, `list_dir`, `grep`, `view_diff`,
  `web_search`, `open_file`. The `firstLineDetail` family (`single_find_and_replace`,
  `multi_find_and_replace`, `edit_existing_file`, `web_fetch`, `http_request`, `ask_user`,
  `present_document`) and the `outputDetail` family (`terminal`, `python_exec`, the three
  `git_*`, `diagnostics`, `sub_agent`) **stay on prose**: quoting a tool's fixed one-line
  sentence, or compressing free-form stdout to "first line + N more", is *rendering*, not
  scavenging. Nothing there is re-derived, so nothing there is fixed by a type.
- **D3 — a summary carries what the tool already computed for its own header, and nothing
  invented.** `ReadSpan` carries the same three numbers `read_file`'s header prints;
  `ListedEntries` carries the total and the skipped count `list_dir` already has. No
  speculative field is added for a consumer that does not exist.
- **D4 — the acceptance oracle is that the rendered output does not change, byte for byte.**
  This plan reshapes a seam; it is not a UI change. `toolpresent_test.go`'s `wantDetail`
  values (L29+) must stay **literally unchanged** while their input rows gain a summary. Any
  wording improvement the new freedom makes possible is a separate, later, owner-visible
  change.
- **D5 — the view owns its own wording; the match with the tool's is the oracle, not a
  contract.** `open_file`'s summary carries `{Lines, Locate, LocatedOn}`; the view formats
  `Located %q on lines: …` itself. That the two strings coincide today is what makes D4
  checkable — it is not a coupling, and after this plan the view may reword without touching
  a tool. This is ADR 0011's thin-renderer intent read the right way round: the *tool* stops
  owning the human's sentence.
- **D6 — no summary ⇒ today's prose path, unchanged.** The seven registry entries **keep** a
  `detail` extractor as their fallback (`firstLineDetail`), so a summary-less result from a
  future path degrades to its first line exactly as it does now — never to a raw dump of a
  whole file into the transcript. `view_diff` with no summary is the `"No changes detected"`
  sentinel, which `firstLineDetail` renders identically.
- **D7 — candidate 06 is one `foldEvent` owner, NOT a `viewDelta` union type.** The three
  folds produce genuinely different things (Model scalars, transcript entries, an activity
  phrase); a delta struct that carried all three would mirror its consumers and hide nothing
  — an indirection, not a deepening. Instead: one `foldEvent` owns the order in one place,
  `foldActivity` **takes the open-call fact as a parameter** so the ordering becomes a data
  dependency the compiler enforces, and a variant-coverage test supplies the "you added an
  Event and forgot a fold" nudge. Recorded so nobody re-proposes the union later.
- **D8 — item 9's helper lives in a `//go:build !windows` file.** `landlock_linux.go` is
  linux-tagged and `seatbelt.go` is `!windows`; `!windows` is the only tag both compile under.
  It does **not** go in `platform_posix.go`, which is the Host rule set and a different
  concern.
- **D9 — item 8 covers `open_file` as well as `read_file`.** They are the same trio, and the
  card's point (the safe primitive exists and is unused) is equally true of both. Stated here
  as a deliberate widening of the card rather than left as a silent extra.
- **D10 — item 10 is test-ergonomics only, and is the plan's weakest item.** The review rated
  it *Speculative, test-only*. It is last, it is independent, and dropping it costs nothing
  else in this plan. It migrates **read** sites only; arrange-side writes in same-package
  tests stay as they are (a test seeding its own fixture is legitimate).

---

## Explicit non-goals

- **Exit codes and structured output for the `outputDetail` family** (`terminal`,
  `python_exec`, `git_*`, `diagnostics`, `sub_agent`). Tempting — the tools know the exit code
  — but it changes what the human sees. Out of scope (D2, D4); a follow-on card if wanted.
- **`HTTPStatus` summaries for `web_fetch` / `http_request`.** Same reason: the view quotes
  the tool's `HTTP 200 OK` first line rather than re-deriving it.
- **The edit trio's `"applied N replacements to <path>"`.** Rendering it from a summary would
  force the view to rebuild the tool's sentence, path and all — wording duplication with no
  scavenging removed.
- **A `viewDelta` union type** (D7).
- **Splitting `model.go` (1,772 lines) further** than moving `foldStats` out with `foldEvent`.
  The file-size finding is recorded in the review under candidate 01 and stays there.
- **`Request.InjectContext` placement.** The review flags it *Speculative*, reopens an ADR
  0010 line, and says explicitly: **not recommended without a grill**. It stays open in the
  review; this plan does not touch it.
- **The `/code-audit` on the live url-safety gap** (still open from candidate 02). That is a
  correctness audit run under a different skill, not implementation work. It stays on the
  review's "Recommended next step" list after this plan lands.
- **Any change to the session wire format.** `transcriptcodec.go` persists the **rendered**
  `toolView` (L67, L193, L248), not the `ToolResult`, so a structured summary never reaches
  disk. `transcriptcodec_test.go` must pass untouched — that is item 4's proof.

---

## 1. `domain.ToolSummary` — the sealed sum and the optional field — ✅ DONE (2026-07-25)

NOTES (2026-07-25): `TestToolResultZeroValueHasNoSummary` pins the "unchanged in every other
field" half with a three-field `ToolResult` composite literal rather than with "the existing
constructors": `okResult`/`errorResult` live in `internal/tools`, which imports `domain`, so
`domain`'s own test cannot call them without an import cycle, and calling them from a new
`internal/tools` test would break this item's "no file outside `internal/domain` and
`apogee.go` changed" acceptance. Item 2's per-tool tests exercise the constructor path.

**What:**

- **New `internal/domain/toolsummary.go`** — the sum and its seven variants, with a file
  header explaining: what a summary is (the structured half of a tool's outcome, for a
  *host*, beside the prose half that is for the *model*); that it is sealed the way `Event`
  is and why (the variant set stays Apogee's, additively versioned); that it is **optional**
  by construction, so ADR 0002's open extension point is untouched — a tool that emits none
  renders through the prose path; and that it is **never persisted** (`internal/tui`'s codec
  stores the rendered view, and `domain.Message` carries only `Content`).

  ```go
  type ToolSummary interface{ isToolSummary() }

  type ReadSpan struct{ Start, End, Total int }        // read_file
  type WroteBytes struct{ Bytes int }                  // write_file
  type ListedEntries struct{ Total, Skipped int }      // list_dir
  type MatchedLines struct{ Total int }                // grep
  type DiffStat struct{ Added, Removed int }           // view_diff
  type SearchHits struct{ Count int }                  // web_search
  type OpenedFile struct {                             // open_file
      Lines     int    // lines in the file body
      Locate    string // the requested locate term; "" when none was
      LocatedOn []int  // 1-based line numbers it was found on
  }
  ```

  Each variant gets its own `func (X) isToolSummary() {}` — no exported base (D1). Each
  carries a one-line doc comment naming the tool that emits it and the fact it records.
- **`internal/domain/tools.go`** — `ToolResult` (L98–103) gains one field, documented as
  optional and view-facing:

  ```go
  Summary ToolSummary // optional structured outcome; nil ⇒ the host reads Content
  ```

  The doc comment says what nil means, that a Mechanism rewriting `Content` on the
  `PostToolResult` seam does **not** invalidate it (the summary describes what the tool did,
  not what the text says), and that `clampToolResult` elision likewise leaves it true.
- **`apogee.go`** — root aliases beside the existing `ToolResult` alias (L260):
  `ToolSummary`, `ReadSpan`, `WroteBytes`, `ListedEntries`, `MatchedLines`, `DiffStat`,
  `SearchHits`, `OpenedFile`, each with the one-line doc the facade uses for every alias. The
  sealing method stays unexported in `domain`, so an embedder can **read** every variant and
  **add** none — the `Event` precedent, stated in the facade comment.

**Tests:** new `internal/domain/toolsummary_test.go`:

- `TestToolSummaryVariantsAreSealed` — a compile-time block asserting each variant satisfies
  `ToolSummary` (`var _ ToolSummary = ReadSpan{}` ×7), plus a comment recording that the
  marker is unexported so no external package can add one.
- `TestToolResultZeroValueHasNoSummary` — the zero `ToolResult` has `Summary == nil`, and a
  result built by the existing constructors is unchanged in every other field.
- `internal/domain/tools_test.go` stays untouched (nothing existing changes shape).

**Acceptance:** gates green; `grep -n "isToolSummary" internal/domain/toolsummary.go` shows
**seven** marker methods plus the interface; `grep -rn "ToolSummary" apogee.go` shows the
alias block; no file outside `internal/domain` and `apogee.go` changed. Commit:
`feat(domain): tools may report a structured summary beside their prose result`.

---

## 2. The four header-shaped tools attach their summary — ✅ DONE (2026-07-25)

NOTES (2026-07-25): two additions beyond the item's literal "one case per tool". The new
tests pin `Content` as a byte-exact string (the existing cases pin substrings of the same
text, so nothing they assert changed), `read_file` gets a third row for the `max_lines`
truncation path because it is the one place `End` is not `Total`, and
`TestReadFile_Execute_ErrorCarriesNoSummary` pins the item's "an error result never carries
a summary" claim, which no other test covered. `renderFile`/`renderEntries`/`renderMatches`
each took the `(string, domain.X)` return the item offered as the first option.

**What:**

- **`internal/tools/tools.go`** — one new constructor beside `okResult` (L44):

  ```go
  // okSummary builds a success ToolResult carrying both halves of the outcome: the prose
  // content the model reads and the structured summary a host renders.
  func okSummary(callID, content string, summary domain.ToolSummary) domain.ToolResult
  ```

  `okResult` and `errorResult` are untouched — an error result never carries a summary (the
  view summarises `IsError` itself, `toolpresent.go` L280–282).
- **`read_file.go`** — `renderFile` (L88) already computes `totalLines`, `start+1` and
  `start+len(selected)` for its header. Return them alongside the string (a small
  `(string, domain.ReadSpan)` return, or compute the span in `Execute` from the same values —
  the implementer picks whichever keeps `renderFile` readable) and attach via `okSummary`.
  **The header string does not change.**
- **`write_file.go`** L89 — `okSummary(call.ID, …, domain.WroteBytes{Bytes: len(args.Content)})`,
  the same number the sentence already prints.
- **`list_dir.go`** — `renderEntries` (L~140) computes `total` and the skipped count for
  `[%d entries total%s]` (L154); surface both and attach `domain.ListedEntries{Total, Skipped}`.
- **`grep.go`** — `renderMatches` (L~225) has `total` for the `[%d total matches…]` header
  (L254) and returns the `"No matches found"` sentinel (L230) when there are none. Attach
  `domain.MatchedLines{Total: n}` on **both** paths — `n == 0` for the sentinel, which is what
  makes the view's existing `"0 matches"` come from a number rather than a prefix test.

**Tests:** in each tool's existing test file (`read_file_test.go`, `write_file_test.go`,
`list_dir_test.go`, `grep_test.go`) add one case per tool asserting **both** halves: the
`Content` is byte-identical to what the file's current tests already pin, **and** `Summary`
type-asserts to the expected variant with the expected numbers. The grep test covers the
zero-match sentinel explicitly. No existing assertion changes.

**Acceptance:** gates green; `grep -n "okSummary" internal/tools/*.go` shows the constructor
plus **four** call sites; every pre-existing tool test passes unchanged. Commit:
`feat(tools): read_file, write_file, list_dir and grep report their outcome as data`.

---

## 3. The three computed-fact tools attach their summary — ✅ DONE (2026-07-25)

NOTES (2026-07-25): three additions beyond the item's literal text. `renderStructuredResults`
returns its count too — the number rendered AFTER `ddgRenderMax` — so `SearchHits.Count` can
never exceed the numbered lines the text actually carries, which is what the view counts.
`diff.go` gained `tagContext`/`tagRemoved`/`tagAdded` constants so the stat's switch and the
line-emitting code cannot drift apart on a tag spelling. The new tests pin `Content`
byte-exactly (the pre-existing cases pin substrings of the same text, so nothing they assert
changed), and `open_file`'s `Lines == len(splitLines(rendered)) - 2` oracle is pinned in its
own test over four body shapes, the empty file included.

Kept separate from item 2 because each of these three needs a small, reviewable change to a
render helper — the fact has to come from the computation, not from re-reading the output.

**What:**

- **`diff.go`** — `unifiedLineDiff` (L83) builds `[]diffOp` via `diffLines` (L110) and renders
  from them. Return the stat with the text (`(string, domain.DiffStat)`), counted **from the
  ops**, and attach it at L76. The `"No changes detected"` path (L74) attaches **no summary**
  (D6) — there is no diff to describe, and the view's prose fallback renders that sentence
  identically. This deletes the reasoning the view carries today about why counting leading
  `+`/`-` is exact (`toolpresent.go` L444–448): the count no longer comes from the rendered
  text at all.
- **`web_search.go` / `web_search_render.go`** — `renderSearch` (L57) returns
  `(string, int)`, the int being the number of **structured** hits
  (`renderStructuredResults`, L148) and **0** on every other path (the `No results found for:`
  sentinels, the cleaned-HTML fallback, `renderSearchResult`'s verbatim pass-through). L182
  attaches `domain.SearchHits{Count: n}` **only when `n > 0`** — precisely the condition the
  view's `searchDetail` (L426–431) tests today, so the fallback rows are untouched.
- **`open_file.go`** — `renderOpenFile` (L90) already walks the content collecting match line
  numbers (L97–101). Return them with the text and attach
  `domain.OpenedFile{Lines: <lines in the body>, Locate: args.Locate, LocatedOn: matches}`.
  `Lines` must equal what the view counts today (`len(splitLines(content)) - 2`, i.e. the
  body's own line count) — pin that equality in the test, since it is the D4 oracle for this
  tool.

**Tests:** `diff_test.go`, `web_search_render_test.go` (plus the `web_search` execute path if
it is covered there), `open_file_test.go` — one case each asserting the summary's contents
and the unchanged `Content`. Explicit cases for the three no-summary paths: `view_diff` with
no changes, `web_search` with an unstructured/`No results` response, and — for `open_file` —
a locate term that matches **no** line (`LocatedOn` empty, `Locate` still set, which is what
distinguishes it from "no locate requested").

**Acceptance:** gates green; `grep -n "okSummary" internal/tools/*.go` shows **seven** call
sites; `go test ./internal/tools/...` green with no pre-existing assertion edited. Commit:
`feat(tools): view_diff, web_search and open_file report their outcome as data`.

---

## 4. The view reads fields — retire the regexes and the prose sniffers — ✅ DONE (2026-07-25)

NOTES (2026-07-25): four deviations from the item's literal text, all forced by the change
itself. (a) **Test fixtures outside `toolpresent_test.go`** gained the summaries their tools
now attach: `render_test.go`'s `readCall` helper (its `from`/`to` became ints so it can build
a `ReadSpan`) and its four `view_diff` blocks, plus three `read_file` results in
`transcript_test.go`. Those tests assert on rendered card lines, so without the summary they
would have pinned the degraded prose floor instead of the line they claim to test; every
`want` string is unchanged. (b) **`TestDiffDetailStat` did not survive as a stat test** — its
content-driven counting cases ("a line starting with + is tagged, not counted", "the count
spans the whole diff") tested counting that now lives in `internal/tools` and is covered by
`diff_test.go`. It is replaced by `TestSummaryLine` (the exhaustive variant→line table,
including the `"1 entries"`/`"1 matches"` fixed-plural traps) and
`TestDiffStatSpansTheWholeDiff` (the stat still describes the whole diff when `diffBody` stops
at the cap). (c) **`internal/tui/doc.go` and `render.go` each had one dangling `diffDetail`
symbol reference** renamed to `diffBody` — item 4's own acceptance grep spans `internal/tui/`,
so the name could not survive; doc.go's tool-presentation *paragraph* is untouched and stays
item 5's. (d) **web_search's no-summary row already existed** (the `No results found for:`
sentinel is exactly the path the tool attaches no summary on), so the six added fallback rows
plus that one cover all seven tools.

The point of the card. Everything before this was groundwork; nothing has changed behaviour
yet.

**What in `internal/tui/toolpresent.go`:**

- **New `summaryLine(s domain.ToolSummary) (detailLine, bool)`** — the one exhaustive type
  switch, returning `false` for nil and for a variant the view has no line for. The rendered
  strings are **exactly** today's (D4):
  | variant | rendered |
  | --- | --- |
  | `ReadSpan{S,E,_}` | `"<S> - <E>"` |
  | `WroteBytes{N}` | `"+<N> bytes"` |
  | `ListedEntries{T,_}` | `"<T> entries"` — built by hand, **not** `plural` |
  | `MatchedLines{T}` | `"<T> matches"` — built by hand, **not** `plural` |
  | `DiffStat{A,R}` | `"+<A> -<R>"` |
  | `SearchHits{N}` | `plural(N, "result")` |
  | `OpenedFile` | locate set → `clipDetail(fmt.Sprintf("Located %q on lines: %s", …))`, or `Located %q on no lines` when `LocatedOn` is empty; no locate → `plural(Lines, "line")` |
  Two traps in that table, both load-bearing under D4. First, `plural` (L521) appends a bare
  `"s"`, so `plural(n, "match")` would render `"matchs"`. Second, the *entries* and *matches*
  forms are **count-independent today** — `reListEntries`/`reGrepMatches` interpolate the
  number into a fixed plural word, so a single match renders `"1 matches"`. Keep both built by
  hand exactly as they are; `plural` is correct only where the current code already calls it
  (`result`, `line`).
- **`enrichWithResult` (L279–294)** gains one branch **before** the registry lookup, leaving
  the existing path untouched beneath it:

  ```go
  if result.IsError { … }                      // unchanged, still first
  p, known := toolRegistry[tv.name]
  if line, ok := summaryLine(result.Summary); ok {
      tv.Summary = line
      if known && p.body != nil {              // view_diff's coloured body
          tv.Details = append(tv.Details, p.body(result.Content)...)
      }
      return
  }
  … today's prose path, verbatim …
  ```
- **`toolPresenter` gains `body func(content string) []detailLine`** — used by exactly one
  entry (`view_diff`), because it is the only tool with a summary *and* a body. The field's
  doc says so, so the asymmetry reads as intentional.
- **Deleted:** `reReadRange`, `reWriteBytes`, `reListEntries`, `reGrepMatches` (L242–247),
  `reSearchHit` (L434), `detailFromPattern` (L365–373), `grepDetail` (L377–386),
  `searchDetail` (L426–431), `openFileDetail` (L488–498), and `diffDetail` (L449–483) —
  replaced by `diffBody`, the same colouring + `diffDetailCap` + remainder loop with the
  counting removed.
- **The seven registry entries** swap their `detail` for `firstLineDetail` (D6 — the
  degraded-path floor) and `view_diff` additionally gets `body: diffBody`. Labels, verbs and
  every `target` extractor are untouched: presentation vocabulary is the view's, and stays.
- **The file header comment (L12–30)** is rewritten to say what the file now is: labels,
  verbs, targets and the rendering of a typed summary — and that the result *semantics* live
  in `internal/tools`.

**Tests:**

- **`toolpresent_test.go`** — the `TestPresentToolCall` table (L29+) keeps every `wantDetail`
  value **character for character** (D4); each of the seven rows gains the `Summary` its tool
  now emits. Add a row per tool for the **no-summary fallback** (prose in, first line out).
  `TestDiffDetail`/`TestDiffDetailStat` (L337, L371) split accordingly: the stat comes from
  `DiffStat`, the body from `diffBody`.
- **New `internal/tui/toolsummary_pin_test.go`** — the cross-package pin, and the test that
  makes this whole card hold: for each of the seven tools, **execute the real tool** against a
  temp workspace (the package's tests already import `internal/tools` in `e2e_test.go`, so
  there is no cycle) and assert the presenter renders the expected line. This is what catches
  a tool that stops attaching its summary — the failure mode the old regexes had and the new
  seam would otherwise inherit silently.
- **`transcriptcodec_test.go` must pass untouched** — the wire form is the rendered view, so
  a session written before this change still reopens identically.

**Acceptance:** gates green;
`grep -n "reReadRange\|reWriteBytes\|reListEntries\|reGrepMatches\|reSearchHit\|detailFromPattern\|grepDetail\|searchDetail\|openFileDetail\|diffDetail" internal/tui/`
is **empty**; `grep -c "regexp" internal/tui/toolpresent.go` shows the import is **gone**;
`toolpresent_test.go`'s `wantDetail` literals are unchanged in the diff (check the diff, not
the file). Commit: `refactor(tui): the view reads a tool's outcome instead of re-parsing it`.

---

## 5. Documentation — the summary is domain vocabulary

**What:**

- **`CONTEXT.md`** — a new term **Tool summary** immediately after **Tool-result capping**
  (### Context and history, ~L456): the structured half of a tool's outcome, sealed and
  Apogee-owned, optional by construction, for *hosts* — the prose `Content` remains what the
  *model* reads; never persisted; a Mechanism rewriting `Content` does not invalidate it.
  `_Avoid_:` "tool metadata", "tool result type" (the result already has a type; this is its
  structured outcome).
- **`docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md`** — a dated
  **2026-07-25 note** (not an amendment; nothing is superseded): a tool may now attach a
  typed summary, the sum is sealed like `Event`, and **omitting it is fully supported** —
  an embedder's tool renders exactly as before. The open extension point is unchanged.
- **`docs/adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md`** — a dated note:
  the view no longer re-derives tool outcomes from prose written for the model; it renders a
  typed summary and owns its own wording (D5).
- **`CHANGELOG.md` (Unreleased → Added)** — in the user's terms: tool outcome cards no longer
  degrade when a tool's wording changes, and embedders can read a tool's outcome as data
  (`apogee.ToolSummary` and its variants; one new optional field on `apogee.ToolResult`).
  Name it as **additive** — no existing code changes shape.
- **`internal/tools/doc.go`** — one paragraph: a built-in tool reports *two* halves, and which
  seven carry a summary and why the rest do not (D2).
- **`internal/tui/doc.go`** — the tool-presentation paragraph updated to match item 4.
- **The review doc** — candidate 03's card gets its ✅ LANDED note in the same style as 01,
  02, 04, 05 and 07: what was built against the sketch, the corrected 21-entry figure, and
  the line-count delta **reported straight** (this card is expected to *add* lines in
  `internal/domain` and `internal/tools` and *remove* them in `internal/tui` — say so plainly
  rather than claiming a collapse).

**Tests:** none new. `go test ./...` still green (the only Go files touched are doc comments).

**Acceptance:** gates green; `grep -n "Tool summary" CONTEXT.md` hits; both ADRs carry a
2026-07-25 dated note; the CHANGELOG entry exists under Unreleased → Added. Commit:
`docs(context,adr,changelog): a tool's outcome has a structured half`.

---

## 6. One `foldEvent` owns the Event fold and its order (candidate 06)

**What:**

- **New `internal/tui/fold.go`** — the Event fold gets a home:
  - `foldStats` **moves here verbatim** from `model.go` (L1474–1509), thinning a 1,772-line
    file by the one function that was only there because `Model` is.
  - **`func (m Model) foldEvent(e domain.Event) Model`** — the single owner of the order:

    ```go
    m = m.foldStats(e)
    m.transcript.apply(e)
    // The activity's ToolResultEvent rule needs to know whether any call is still open, and
    // apply is what pairs this result with its call — so the fact is READ here and PASSED,
    // making the order a data dependency instead of a comment (D7).
    return m.foldActivity(e, m.transcript.hasOpenToolCall())
    ```
  - The file header states the rule: **every** engine Event enters the view through
    `foldEvent` and nowhere else.
- **`internal/tui/activity.go`** — `foldActivity` takes `openCall bool` and its
  `ToolResultEvent` case tests the parameter instead of reaching into `m.transcript` (L176).
  Its 15-line ordering comment shrinks to a sentence about what the parameter means; the
  comment that survives is the one that carries a *reason* (sticky `actStopping`).
- **`internal/tui/model.go`** — the `eventMsg` case (L259–265) becomes
  `m = m.foldEvent(msg.Event); m.refreshViewport(); return m, nil`, and its three-line
  ordering comment is deleted (the order is now in the code that owns it).
- **`internal/tui/transcript.go`** — `hasOpenToolCall`'s doc (L352–358) is re-pointed at
  `foldEvent` as its caller.

**Tests:**

- **New `internal/tui/fold_test.go`:**
  - `TestFoldEventCoversEveryEventVariant` — parse `internal/domain/events.go` with
    `go/parser`, collect every exported struct type that embeds `EventBase`, and fail if any
    is missing from the test's own variant table. This is the compile-adjacent nudge D7 asks
    for, and it is the repo's existing idiom (`internal/platform/winlabel/deps_test.go` parses
    package files the same way). It must refuse to pass over zero parsed types.
  - The variant table then feeds **every** variant through `foldEvent` on a fresh Model,
    each row stating what it expects (a transcript entry, a stats change, an activity phrase,
    or explicitly **nothing** — `AuditEvent`, `UsageEvent` at depth > 0, and friends are
    deliberately inert, and saying so in a table is the documentation the three switches never
    had).
  - `TestFoldEventPairsResultWithCallBeforeActivity` — a `ToolCallEvent` then its
    `ToolResultEvent`, asserting the activity leaves the tool phrase (the behaviour the old
    ordering comment protected), plus the parallel-batch case where a second call is still
    open and the phrase must **stay**.
- **`model_test.go` / `activity_test.go`** — existing tests that call `foldActivity` directly
  gain the new argument; existing expectations are unchanged. Tests that drive `Update` with
  an `eventMsg` are untouched, which is itself part of the acceptance (the path through the
  Update loop behaves identically).

**Acceptance:** gates green;
`grep -rn "foldStats\|transcript.apply(\|foldActivity(" internal/tui/*.go` (excluding tests)
shows each called from **`fold.go` only**; `grep -n "hasOpenToolCall" internal/tui/activity.go`
is **empty**; `model.go` is ~40 lines shorter. Commit:
`refactor(tui): one foldEvent owns the Event fold and its order`.

---

## 7. One shared `workspaceWriteTarget` body

**What:**

- **`internal/tools/workspace_scoped.go`** — one helper beside `resolveTargetUnbounded` (L58):

  ```go
  // pathArgWriteTarget is the shared body of every workspaceScopedWriter's
  // workspaceWriteTarget: decode the call's "path" argument and resolve it against root
  // WITHOUT the containment check, because dispatch needs an out-of-workspace target to
  // resolve rather than error (contract §3). It decodes into a minimal one-field struct
  // rather than each tool's args type — every write tool spells the argument "path", and
  // TestWriteTargetsAgreeOnPath is what keeps that true.
  func pathArgWriteTarget(call domain.ToolCall, root string) (string, bool)
  ```
- **`write_file.go` L52, `find_replace.go` L69 and L186, `file_edit.go` L51** — each body
  becomes `return pathArgWriteTarget(call, t.root)`. The **methods stay** (the marker is a
  method set — contract §3.2), and each keeps its own doc comment: `write_file`'s longer one
  explains the marker for all four and is the one to preserve in full.

**Tests:** `internal/tools/workspace_scoped_test.go` gains
`TestWriteTargetsAgreeOnPath` — a table over all four tools constructed on the same root,
each handed the same `{"path":"sub/f.txt"}` call through `tools.WorkspaceWriteTarget` (L46),
asserting the same absolute path and `ok == true`; plus the two negative cases (undecodable
arguments, empty path → `ok == false`). This is the guard that makes the shared decode safe:
if a tool ever renames its path argument, this fails instead of the marker silently returning
`ok=false` and dispatch mis-classifying a write as in-bounds.

**Acceptance:** gates green;
`grep -n "resolveTargetUnbounded" internal/tools/*.go` shows it called from
**`workspace_scoped.go` only**; each of the four methods is a single `return` line; the four
`var _ workspaceScopedWriter` assertions still compile. Commit:
`refactor(tools): one body for every write tool's workspace target`.

---

## 8. `read_file` and `open_file` read through the TOCTOU-safe primitives

**What:**

- **`read_file.go` L65–85** — `resolveInRoot → os.Stat → os.ReadFile` becomes
  `security.SafeStat(t.root, args.Path)` → the same `IsDir` / `Size` checks → `safeReadFile`
  (`path_safety.go` L40), with failures rendered by the existing `readFileErrorMessage`
  (L47) that the write tools already use. The `path` local disappears with its check/use gap.
- **`open_file.go` L65–84** — the identical change (D9).
- Both files' Execute doc comments gain the sentence the write tools carry: the workspace
  fence is enforced at stat/read time through an `os.Root`, so a symlink component swapped to
  point outside the root — including by a confined subprocess mid-call — is refused rather
  than followed.

**Error-message parity, checked before writing this plan:** an escape caught by
`security.ResolveInRoot` renders `%w: %q` (`pathsafety.go` L45); an escape caught by
`SafeStat`/`SafeReadFile`'s `rootRelative` renders the **same** `%w: %q`
(`safeio.go` L135/L139). A missing file still yields `"file not found: <path>"` through
`readFileErrorMessage`. The one genuinely new message is `mapRootEscape`'s `%w: %v`
(`safeio.go` L163) for an escape only detectable **inside** the `os.Root` — a case the old
code did not detect at all, because it followed the symlink. `errors.Is(err, ErrPathEscape)`
holds on every path, before and after.

**Tests:** `read_file_test.go` and `open_file_test.go` — the existing escape/missing/dir/
oversize cases must pass **unchanged** (that is the parity claim, tested rather than asserted).
Add to each: a symlink inside the workspace pointing outside it, asserting the read is
**refused** with an `ErrPathEscape`-matching error (the behaviour the old trio did not have),
skipped on hosts where the test cannot create a symlink.

**Acceptance:** gates green;
`grep -n "os.Stat\|os.ReadFile" internal/tools/read_file.go internal/tools/open_file.go` is
**empty**; `go test ./internal/tools/... ./internal/security/...` green. Commit:
`refactor(tools): read_file and open_file read through the fence, not around it`.

---

## 9. One POSIX argv-wrap helper for landlock and seatbelt

**What:**

- **New `internal/platform/confine_posix.go`** (`//go:build !windows` — D8):

  ```go
  // wrapArgvUnderLauncher rewrites cmd to run under launcher, with prefix inserted between
  // the launcher and the original argv. It carries the RESOLVED program path (cmd.Path),
  // falling back to Args[0] only when Path is unset, because the wrapped child re-execs
  // without a PATH lookup (confinement-execution-contract §2.3). It prepares in place and
  // does not run cmd (§2.2).
  func wrapArgvUnderLauncher(cmd *exec.Cmd, launcher string, prefix ...string) error

  // setConfinedPgid puts the wrapped child and its descendants in one process group so the
  // tool's negative-PID kill reaps the whole group (§2.4).
  func setConfinedPgid(cmd *exec.Cmd)
  ```

  `wrapArgvUnderLauncher` owns the empty-argv guard and returns the **existing** message
  verbatim: `apogee: confine: cmd has no argv`.
- **`landlock_linux.go` L136–176** — after the self-executable resolution and `encodeBox`,
  the tail becomes
  `if err := wrapArgvUnderLauncher(cmd, self, confinedExecSentinel, encoded, "--"); err != nil { return err }` +
  `setConfinedPgid(cmd)`. The empty-argv check at L137 moves into the helper — note the
  ordering change this implies and keep it: landlock currently checks argv **before**
  resolving the binary, so the helper call must stay where the check was, or the
  no-argv error would become a different error on a host that cannot resolve its own
  executable. Simplest correct shape: call `wrapArgvUnderLauncher` **after** `self` and
  `encoded` are computed, and keep a bare `if len(cmd.Args) == 0` guard first — the
  duplication is two lines and preserves the error ordering exactly. Whichever the
  implementer picks, a test must pin the empty-argv error on **both** backends.
- **`seatbelt.go` L97–132** — the same substitution with
  `wrapArgvUnderLauncher(cmd, profiler, "-p", profile)`.
- The two ~8-line comment blocks that explain the resolved-path rule and Setpgid move **onto
  the helpers**, once, with their contract references intact — nothing explaining *why* is
  deleted.

**Tests:** new `internal/platform/confine_posix_test.go` (`//go:build !windows`) — a table
over `wrapArgvUnderLauncher`: resolved `Path` preserved, `Args[0]` fallback when `Path` is
empty, prefix ordering, empty argv → the exact error string; and `setConfinedPgid` on a fresh
`*exec.Cmd` and on one that already has a `SysProcAttr` (the non-nil branch). The existing
`landlock_linux_test.go` and `seatbelt_test.go` argv assertions must pass **unchanged** —
that is the acceptance oracle for "the wrapper moved, the argv did not".

**Acceptance:** all gates green **including** the two `GOOS=darwin` commands;
`grep -n "Setpgid" internal/platform/*.go` shows it in **`confine_posix.go` only**;
`landlock_linux.go` and `seatbelt.go` each lost ~15 lines. Commit:
`refactor(platform): landlock and seatbelt share one argv wrapper`.

---

## 10. The self-regulator gets a read model

The plan's weakest item by the review's own rating (*Speculative, test-only*) — last,
independent, and droppable (D10).

**What:**

- **`internal/agent/selfreg.go`** — one accessor beside `judgment` (L172):

  ```go
  // selfRegView is the self-regulator's observed state: what it has decided so far, as data.
  // It exists so a reader — a test today, a status view or an audit surface later — does not
  // have to reach through unexported fields to see what the regulator is doing. It is a
  // SNAPSHOT: the maps are copied, so a caller cannot mutate the regulator through it.
  type selfRegView struct {
      Strikes       map[domain.MechanismID]int
      Suppressed    []domain.MechanismID // sorted, for a deterministic read
      BudgetTripped bool
      HarmfulStreak int
  }

  func (r *selfRegulator) observed() selfRegView
  ```
- **`internal/agent/selfreg_test.go`** — the **read** sites (the assertions among the 32
  reach-ins) go through `observed()`. Arrange-side writes that seed a fixture
  (`selfreg_test.go` L519–522 and friends) **stay as they are** — a same-package test seeding
  its own state is legitimate, and routing it through an accessor would mean adding a setter
  that production code does not need.

**Tests:** `TestObservedIsASnapshot` — mutating the returned `Strikes` map or `Suppressed`
slice does not change the regulator, and a subsequent `endTurn` still reports the true state.
Every migrated assertion keeps its original expectation; no test's *meaning* changes in this
item.

**Acceptance:** gates green; `grep -rn "\.strikes\|\.suppressed\|\.budgetTripped\|\.harmfulStreak" internal/agent/*_test.go`
returns **only** arrange-side writes (each one an assignment, `=` on the left-hand side —
report the count straight in the commit body); `go test ./internal/agent/...` green. Commit:
`refactor(agent): the self-regulator has a read model`.

---

## Whole-plan verification (run after item 10, before declaring done)

1. **Full gate**, all five commands, plus item 9's two `GOOS=darwin` commands.
2. **The regexes are gone:**
   `grep -rn "reReadRange\|reWriteBytes\|reListEntries\|reGrepMatches\|reSearchHit" internal/`
   → empty.
3. **The view no longer parses results:** `grep -n "regexp" internal/tui/toolpresent.go` →
   empty.
4. **Every summary-bearing tool actually attaches one:** `toolsummary_pin_test.go` (item 4)
   covers all seven by executing the real tool; confirm it names seven tools, not six.
5. **One fold owner:** `grep -rn "foldStats\|transcript.apply(\|foldActivity(" internal/tui/*.go`
   (non-test) → `fold.go` only.
6. **The safe primitives are used:**
   `grep -n "os.Stat\|os.ReadFile" internal/tools/read_file.go internal/tools/open_file.go`
   → empty.
7. **One argv wrapper:** `grep -n "Setpgid" internal/platform/*.go` → `confine_posix.go` only.
8. **Public surface, stated straight:** `go doc github.com/airiclenz/apogee | grep -i summary`
   lists the sum and its seven variants; nothing was **removed** from the facade. Confirm the
   CHANGELOG calls it additive and that `VERSION` (`v0.8.4`) is bumped, or record deliberately
   that the bump rides the next release — an owner call, not the implementer's.
9. **Line counts, reported straight** (this plan is expected to ADD lines net: a new domain
   file, a new TUI file, four new test files). Report the deltas per package rather than a
   total, and say plainly that the win is one typed seam, one fold owner and four
   duplications removed — **not** a line count. A net increase is not a finding here.
10. **The review's ledger is updated and empty:** candidates 03 and 06 carry their ✅ LANDED
    notes, the four smaller deepenings are marked landed, and the "Recommended next step"
    section is rewritten to say the only thing still open from the 2026-07-24 review is the
    `/code-audit` on the live url-safety gap (plus `Request.InjectContext`, still deliberately
    un-grilled). Then archive this plan under `docs/plans/archived/`.

## Manual verification (owner — the automated suite cannot do this)

**Build the TUI and drive one Turn that uses all seven summary-bearing tools** — read a file,
write one, list a directory, grep, view a diff, open a file with a `locate` term, and run a
web search — and confirm each card's one-line summary reads **exactly** as it did before this
plan (D4). The automated pin (item 4) proves the strings match the table; only eyes prove the
table matched reality. Also confirm the status line's live phrase still tracks a tool call
through to its result (item 6's ordering change is invisible to every test that drives
`Update`, which is precisely why it is worth looking at once).
