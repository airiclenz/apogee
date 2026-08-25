# `@file` references extract PDF text, and carry a structural size floor — implementation plan

**Goal:** an `@"doc.pdf"` reference in a message injects the document's EXTRACTED TEXT — the
same text, page markers and failure wording `read_file` already produces — instead of the raw
PDF bytes; and the `@file` blocks of one message can never outgrow the History allocation, so a
giant reference degrades to the shared head/tail elision instead of dooming the Turn.

**Date:** 2026-08-25
**Status:** ready to execute
**Sized for:** ~200k-context host
**Skills:** coding-standards

## Root cause (the defect this plan fixes)

Session `20260825T150050Z-ef39162e` submitted `can you read the document
@"docs/OP2OL Whitepaper.pdf" ?` on a 1,310,720-token endpoint. The 27-page, 2,334,842-byte PDF
was inlined as RAW BYTES (the transcript's user message is 2,251,789 chars, opening with
`%PDF-1.7`), estimated at ~1.39M tokens — over the window. The emergency fold then overflowed
too (`compaction: apogee: context window exceeded …`): `renderBudgetedTranscript` keeps the most
recent message unconditionally, and that message WAS the PDF.

Two causes:

1. PDF extraction lives only behind the `read_file` tool (`internal/tools/pdf_text.go`,
   `readableText` in `internal/tools/read_file.go`). The `@file` resolver
   (`resolveFileRefs` / `readFileRef`, `internal/agent/loop.go:906-973`) reads bytes and
   injects them verbatim — it never sniffs for `%PDF-`. The same document extracts to
   **45,207 chars** (~12k tokens) — measured with `extractPDFText` against the file.
2. `readFileRef` bounds a reference at `maxRefFileBytes` (10 MiB) — a sanity cap, "not a
   context budget" by its own comment. A tool result has a STRUCTURAL floor
   (`clampToolResult`, `internal/agent/dispatch.go:1051`) against the History allocation; an
   `@file` block has none, so any reference past the window is unrecoverable by every reducer.

## Authoritative sources

- `internal/tools/pdf_text.go` @ `f0087db3` — the extractor's contract (content sniff, recover,
  per-page placeholder, scanned-vs-unreadable wording, `[Page N]` markers). Its BEHAVIOUR is the
  spec; only its home and export surface move.
- `internal/agent/dispatch.go:1012-1064` @ `f0087db3` — `clampToolResult`, the structural floor
  the `@file` floor mirrors (bound = `Budget.History`, else
  `compactUnknownWindowTranscriptTokens`; render = `context.TruncateToolResult`; never grows).
- `internal/agent/loop.go:906-973` @ `f0087db3` — `resolveFileRefs` / `readFileRef`, the seam
  every `@file` crosses (opening message in `step()`, and `Interject`).
- ADR 0006 (structural floors are not Mechanisms), ADR 0007 (tool-level failure = IsError, not a
  Go error), ADR 0018 (overflow protection and the emergency fold), ADR 0031 (engine sufficiency).
- `CONTEXT.md` §"File reference (`@file`)".

## Ratified design calls (owner, 2026-08-25, via AskUserQuestion)

1. **Scope = PDF extraction for `@file` PLUS a structural size floor for `@file` blocks.** The
   floor mirrors `clampToolResult` exactly (same bound, same `TruncateToolResult` rendering, same
   marker, never grows) — one elision idiom for the model whichever seam produced it.
2. **No new tool.** The tool set stays as it is: `read_file` keeps auto-detecting PDFs by content
   and returning extracted text; `@file` gains the identical behaviour. The shared extractor
   moves to a new format-only package **`internal/doctext`** — a package, not a tool.
3. **The model is told the content is extracted, read-only text.** Both headers — `read_file`'s
   `[File: …]` line and the `@file` block's `Referenced file …` line — annotate a PDF as
   `(PDF, N pages; extracted text, read-only)`. Wording is fixed here; `N pages` stays singular
   for one page (`PDF, 1 page; …`), as `pdfDisplayPath` does today.
4. **The `@file` floor splits the History allocation across the references of ONE message.**
   Bound per reference = History allocation ÷ number of references in that message (integer
   division, floor of 1 token). A lone reference keeps the full bound — the same number
   `clampToolResult` uses — so the assembled block of a message can never exceed the allocation
   and the emergency fold's keep-the-most-recent-message rule stays survivable however many
   references a message carries.

## Standing requirements

- `skills: coding-standards` (forwarded by default).
- Every deviation from item text lands as a dated `NOTES:` line under the item.
- Each item's CHANGELOG entry goes under `## [Unreleased]` in `CHANGELOG.md`; no item touches
  `VERSION` or a release heading.
- No AI attribution trailers in commit messages.

## Out of scope

- OCR / scanned PDFs (still refused with `pdfNoTextMessage`).
- Other document formats (DOCX, images) — `doctext` is built so a second format is a sibling
  file, but none is added here.
- Token-aware trimming of `@file` content BELOW the structural floor (the deferred
  context-builder, TDD §8 #8) — the floor is a pathological-case backstop, not a budgeter.
- Shrinking the session transcript already saved with the raw bytes; changing what the TUI shows
  for a resolved reference; the `present` opener's PDF handling.
- A `tool_result_cap`-style Mechanism for `@file` blocks.

---

## 1. Move PDF extraction into `internal/doctext` and word the read-only hint — ✅ DONE (2026-08-25)

NOTES (2026-08-25): `TestReadFile_Execute_RefusesAPDFWithNoText` could not stay literally unchanged — it asserted equality against `pdfNoTextMessage`, which the item keeps unexported in `doctext`; it now asserts the two substrings that pin the scan case at the tool boundary, with the exact wording still pinned by `TestExtractPDF_ReportsAScannedDocument` in `doctext`.
NOTES (2026-08-25): two caller-specific doc-comment lines were re-pointed rather than moved verbatim — `ExtractPDF`'s "read_file never falls back to raw bytes" reads "no caller falls back to raw bytes" now that the package serves two seams, and `ReadFile.Execute`'s "(pdf_text.go)" pointer reads "(internal/doctext)". The three-result shape and the failMessage-is-for-the-model rule are verbatim.

**What:** Create the format-only package `internal/doctext` and move
`internal/tools/pdf_text.go` into it, exporting exactly what two callers need:

- `doctext.IsPDF(data []byte) bool` — the `%PDF-` content sniff, unchanged.
- `doctext.ExtractPDF(data []byte) (text string, pages int, failMessage string)` — the
  parse-and-walk unchanged in behaviour: recover around the parser, `[Page N]` marker line before
  each page's text, exactly one blank line between pages, `[Page N: text extraction failed]`
  placeholder for a failed page, `pdfNoTextMessage` for a parsed-but-empty document,
  `pdfUnreadableFormat` (cause quoted verbatim) for a refused/panicked/zero-page document. The
  three-result shape and the "failMessage is written FOR THE MODEL, hand it on unwrapped" rule
  are kept verbatim from the current doc comment.
- `doctext.PDFAnnotation(pages int) string` — returns `PDF, 1 page; extracted text, read-only`
  for one page and `PDF, N pages; extracted text, read-only` otherwise (design call 3). Both
  headers are built from this one function so they can never disagree.

Keep the message constants unexported in `doctext`. Add `internal/doctext/doc.go` in the house
style (what the package is, why it is not part of `tools` — a document FORMAT has one reason to
change and two consumers, the tool and the loop; the engine must not depend on the tools package
for it). Move the fixtures `internal/tools/testdata/{minimal,nopages,notext}.pdf` to
`internal/doctext/testdata/` and `internal/tools/pdf_text_test.go` to
`internal/doctext/pdf_test.go` (renamed to the exported identifiers; `truncateForMessage`
becomes a local test helper if `doctext` has no equivalent).

Update the consumers in `internal/tools`: `readableText` calls `doctext.IsPDF` /
`doctext.ExtractPDF`; `pdfDisplayPath` becomes `path + " (" + doctext.PDFAnnotation(pages) + ")"`
(so the `read_file` header now reads `[File: docs/x.pdf (PDF, 27 pages; extracted text,
read-only), …]`); `read_file_test.go` reads the fixtures from `../doctext/testdata/` via its
existing `readPDFFixture` helper (moved there from `pdf_text_test.go`, path adjusted) and
`TestPDFDisplayPath` pins the new wording for 0, 1 and 2 pages. Fix the `internal/tools/doc.go`
package map: the file count sentence ("Thirty files carry the built-ins") drops by one, and the
`pdf_text.go` sentence becomes a one-line pointer to `internal/doctext`. Delete
`internal/tools/pdf_text.go`. `go.mod` keeps `github.com/ledongthuc/pdf` (its importer moves).

Binding standards for this item: one deep module (`doctext`) behind three exported functions;
the tool's `readableText` and `pdfDisplayPath` shrink to adapters; no behaviour change other
than the annotation wording.

**Files:** `internal/doctext/doc.go`, `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`,
`internal/doctext/testdata/minimal.pdf`, `internal/doctext/testdata/nopages.pdf`,
`internal/doctext/testdata/notext.pdf`, `internal/tools/pdf_text.go` (deleted),
`internal/tools/pdf_text_test.go` (deleted), `internal/tools/testdata/minimal.pdf` (moved),
`internal/tools/testdata/nopages.pdf` (moved), `internal/tools/testdata/notext.pdf` (moved),
`internal/tools/read_file.go`, `internal/tools/read_file_test.go`, `internal/tools/doc.go`,
`CHANGELOG.md`

**Tests:**
- `internal/doctext/pdf_test.go`: the four moved tests (`TestIsPDF`,
  `TestExtractPDF_ReturnsTheTextWithAPageMarker`, `TestExtractPDF_ReportsAScannedDocument`,
  `TestExtractPDF_ReportsAZeroPageDocumentAsUnreadable`, `TestExtractPDF_ReportsUnreadableBytes`)
  plus `TestPDFAnnotation` (0, 1, 2 pages → exact strings).
- `internal/tools/read_file_test.go`: the three existing PDF tests pass against the moved
  fixtures; `TestPDFDisplayPath` asserts `(PDF, 1 page; extracted text, read-only)` and
  `(PDF, 2 pages; extracted text, read-only)`.
- `go vet ./...` clean; no `pdf` import remains under `internal/tools`.

**Acceptance:**
```
go build ./... && go vet ./internal/doctext ./internal/tools
go test ./internal/doctext ./internal/tools
test ! -e internal/tools/pdf_text.go && ! grep -rq "ledongthuc/pdf" internal/tools/
grep -q "extracted text, read-only" internal/doctext/pdf.go
```

**Commit:** `refactor(doctext): move PDF text extraction into its own package and word the read-only hint`

---

## 2. `@file` references inject a PDF's extracted text — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the item asks to re-point a stale `security.SafeReadFile` mention in the `resolveFileRefs` doc comment — that comment already names `security.SafeOpen` (it was corrected in an earlier change), so nothing was edited. The stale name survives only in `CONTEXT.md`, which item 4 owns.
NOTES (2026-08-25): the two ErrorEvent sites (unreadable ref, unextractable document) emit through one new unexported helper, `Agent.refIgnored`, so both failures keep the one sentence verbatim. It is a failure-channel helper, not a second `readableText` — the sniff-then-extract sequence itself is written once, inline in `resolveFileRefs`, as the item's binding standards require.

Depends on item 1.

**What:** In `internal/agent/loop.go`, make `resolveFileRefs` route every resolved reference
through the same content sniff `read_file` uses:

- `readFileRef` keeps returning the bounded bytes (unchanged fence, cap, growth backstop).
  Change its return type to `[]byte` so no PDF is ever converted to a Go string before the sniff.
- In `resolveFileRefs`, after a successful read: if `doctext.IsPDF(data)`, call
  `doctext.ExtractPDF(data)`. A non-empty `failMessage` is surfaced exactly like every other
  unresolvable ref — `ErrorEvent{Source: "loop", Err: "@<ref> could not be resolved and was
  ignored: <failMessage>"}` — and the ref injects nothing (the Turn proceeds). On success the
  block header carries the annotation from item 1:
  ``Referenced file `docs/x.pdf` (PDF, 27 pages; extracted text, read-only):`` followed by the
  fenced extracted text. A non-PDF block is byte-identical to today's
  ``Referenced file `path`:`` form.
- Update `resolveFileRefs`' doc comment (the PDF branch, and that the sniff is content-based so
  `notes.pdf` holding text still injects text) and the stale `security.SafeReadFile` mention to
  `security.SafeOpen` where the comment names it.

`Interject` (`internal/agent/interject.go:63`) calls the same `resolveFileRefs`, so it inherits
the behaviour with no change.

Binding standards: the loop imports `internal/doctext`, never `internal/tools`, for this; the
sniff-then-extract sequence is written ONCE in `resolveFileRefs` (no second helper duplicating
`readableText`'s shape — the two callers differ in their failure channel, ErrorEvent vs
IsError, which is why they are not one function).

**Files:** `internal/agent/loop.go`, `internal/agent/filerefs_test.go` (new), `CHANGELOG.md`

**Tests** (`internal/agent/filerefs_test.go`, using the existing `baseConfig` / `recordingSink`
/ `echoResponder` / `newAgent` helpers and `t.TempDir()` workspaces; PDF bytes copied from
`../doctext/testdata/`):
- `TestResolveFileRefs_InjectsAPDFAsExtractedText`: copy `minimal.pdf` into the workspace,
  `Submit` with `FileRefs: []string{"minimal.pdf"}`, `Step`; `a.conv.At(0).Content` contains
  `(PDF, 1 page; extracted text, read-only)` and `[Page 1]`, does NOT contain `%PDF-`, and no
  ErrorEvent was emitted.
- `TestResolveFileRefs_SkipsAPDFWithNoText`: `notext.pdf` → an ErrorEvent containing `no
  extractable text` and a user message equal to the submitted text alone (mirrors
  `TestReadFileRefRefusesOversizeRef`).
- `TestResolveFileRefs_JudgesAPDFByContentNotName`: a text file named `notes.pdf` injects its
  text under the plain ``Referenced file `notes.pdf`:`` header.
- `TestInterjectInjectsAPDFAsExtractedText` (add to `filerefs_test.go`): the interjection path
  produces the same annotated block.

**Acceptance:**
```
go build ./... && go vet ./internal/agent
go test ./internal/agent -run 'FileRef|Interject'
go test ./internal/agent
```

**Commit:** `fix(agent): resolve an @file PDF reference to its extracted text, never its bytes`

---

## 3. Structural size floor for `@file` blocks

Depends on item 2.

**What:** Give every `@file` block the same STRUCTURAL floor a tool result has, at the one seam
every reference crosses (`resolveFileRefs`), so a block larger than the whole History allocation
is elided to the shared head/tail-plus-marker shape BEFORE it is appended to the conversation:

- In `internal/agent/dispatch.go`, factor the bound-and-clamp core of `clampToolResult` into
  two unexported pieces: `structuralFloor() int` (= `Budget.History`, or
  `compactUnknownWindowTranscriptTokens` when the allocation is zero) and
  `clampToBound(content string, bound int) string` (return `content` unchanged when
  `EstimateTokens(len(content)) <= bound`; render with
  `apogeectx.TruncateToolResult(content, int(float64(bound)*b.CharsPerToken))`; return the
  original when the rendering did not shrink it). `clampToolResult` becomes
  `clampToBound(content, structuralFloor())`; its doc comment moves to the shared pieces and
  gains a paragraph on the second caller.
- In `resolveFileRefs`, compute `bound := structuralFloor() / len(refs)` once per message
  (never below 1 — design call 4) and clamp each reference's CONTENT (the extracted text or the
  file text, before the fence and header are added) with `clampToBound(content, bound)`. The
  divisor is the number of references SUBMITTED, not resolved: a ref that fails to resolve still
  counted, which only makes the bound stricter, never looser. The header is added after the
  clamp, so the model still reads which file the elided block came from and, for a PDF, its
  page count. The marker's "re-read with start_line/end_line" hint is actionable here because
  `read_file` takes the same path.
- Record the floor in the `resolveFileRefs` doc comment: structural (ADR 0006), consults no
  config, never disabled under Bypass, cannot be withdrawn; edits the conversation itself (the
  raw block never reaches history or a snapshot), which is the price of a floor every later
  reducer can rely on — and why the emergency fold's keep-the-most-recent-message rule can no
  longer be defeated by a reference.

Binding standards: one floor computation and one clamp rendering shared by both seams (they have
one reason to change: the fold's arithmetic); no new config key; no new Mechanism; the clamp
never grows content.

**Files:** `internal/agent/dispatch.go`, `internal/agent/loop.go`,
`internal/agent/filerefs_test.go`, `CHANGELOG.md`

**Tests** (`internal/agent/filerefs_test.go`):
- `TestResolveFileRefs_ClampsAReferencePastTheHistoryAllocation`: `cfg.Context.MaxContextTokens
  = 8192`; write a 400-line text file whose size in chars is well past
  `budget().History * CharsPerToken` (≥ 200,000 chars); `Submit` + `Step`; the user message
  contains the elision marker `[truncated to fit the context budget`, still starts with
  ``Referenced file `big.txt`:``, keeps the file's first and last line, and is shorter than the
  file. A 20-line file under the bound is injected whole (no marker).
- `TestResolveFileRefs_ClampsAPDFPastTheHistoryAllocation`: same window; a PDF whose extracted
  text exceeds the bound is not available as a fixture, so pin the ORDER instead — a `minimal.pdf`
  reference under the bound keeps its full annotated header and `[Page 1]`; assert the header is
  outside the clamped region by construction (header present even when the content is the
  clamped 400-line text of the previous test).
- `TestResolveFileRefs_SplitsTheFloorAcrossReferences`: same 8192 window; two references each
  sized to ~70% of the single-reference bound — injected whole when submitted alone, both
  clamped (marker present in both blocks) when submitted together in one `FileRefs` slice; the
  whole user message's estimated tokens stay ≤ `budget().History`.
- `TestClampToBound_SharedByToolResultsAndFileRefs`: with the same Budget, an identical
  oversized string clamps to an identical rendering through `clampToolResult` and through a
  single-reference `@file` path (byte-equal bodies below the header).
- Existing `clampToolResult` tests in `internal/agent` stay green unchanged.

**Acceptance:**
```
go build ./... && go vet ./internal/agent
go test ./internal/agent -run 'FileRef|Clamp|ToolResult'
go test ./internal/agent
```

**Commit:** `fix(agent): clamp an @file block to the structural floor tool results already have`

---

## 4. Documentation: `@file` PDF extraction and the size floor

Depends on item 3.

**What:** Bring the prose in line with items 1–3 — every cross-cutting doc amendment is owned
here, not left to the code items:

- `CONTEXT.md` §"File reference (`@file`)": add that a reference whose bytes are a PDF injects
  its extracted text with `[Page N]` markers under the `(PDF, N pages; extracted text,
  read-only)` annotation — the same extraction `read_file` performs (`internal/doctext`) — and
  that a block past its share of the History allocation (the allocation split across the
  message's references) is elided to the shared head/tail marker (structural floor, ADR 0006),
  with the sentence pointing at `resolveFileRefs`. Replace the
  stale `security.SafeReadFile` name with `security.SafeOpen`.
- `docs/manual/commands.md` file-references section: one paragraph for users — an `@` reference
  to a PDF reads the document's text (scanned PDFs are refused with a message; ask for a text
  version), and a very large reference is shown to the model as its head and tail with a note to
  read ranges via `read_file`.
- `docs/manual/README.md` line 9 (Commands row): mention PDF references only if the row's
  wording lists what references do; otherwise leave it.
- `CHANGELOG.md` `[Unreleased]`: one entry for the doc pass (items 1–3 each wrote their own).
- `ISSUES.md`: nothing to add or remove — verify no open item describes this defect (grep for
  `@file` and `PDF`); if one exists, remove it (resolved items live only in the CHANGELOG).

**Files:** `CONTEXT.md`, `docs/manual/commands.md`, `docs/manual/README.md`, `CHANGELOG.md`,
`ISSUES.md`

**Tests:** none (docs only — the repo convention skips `make check` for docs-only commits; the
closeout runs it once for the whole plan).

**Acceptance:**
```
grep -q "extracted text, read-only" CONTEXT.md
grep -qi "pdf" docs/manual/commands.md
! grep -q "security.SafeReadFile" CONTEXT.md
```

**Commit:** `docs(file-refs): document PDF extraction and the structural floor for @file references`

---

## Manual proof (owner, after item 2 lands — not an executor step)

Start apogee in this repo and submit `can you read the document @"docs/OP2OL Whitepaper.pdf" ?`
(the 27-page document at `/workspace/repos/apogee/docs/OP2OL Whitepaper.pdf`, kept out of git via
`.git/info/exclude` — owner call 2026-08-25). The
user message should carry `(PDF, 27 pages; extracted text, read-only)` and ~45k chars of text;
no context-window error.

## Suggested version bump

Micro bump (0.17.0 → 0.17.1) once items 1–3 land: a user-visible defect fix (`@file` PDF
references overflowed the window) plus a new structural guarantee. The owner decides; no item
changes `VERSION`.
