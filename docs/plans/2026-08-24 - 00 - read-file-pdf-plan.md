# read_file PDF extraction plan

**Goal:** `read_file` detects PDF files by content and returns their extracted text to the
model instead of raw binary bytes. Extraction is default-on, needs no new parameters, and
fails with a clear, model-facing error message when a PDF has no extractable text.

**Date:** 2026-08-24
**Status:** ready
**Sized for:** ~200k-context host

**Authoritative sources:**

- `internal/tools/read_file.go` — the tool being extended (pipeline: `readBounded` →
  `renderFile` → `okSummary` + `resolvedTargetNote`).
- `internal/tools/tools.go` — `okSummary`, `errorResult`, `maxFileReadBytes`.
- Library: `github.com/ledongthuc/pdf` (MIT, latest release 2025-05). Its ancestor
  `rsc.io/pdf` panics on malformed input — all calls into it must run behind `recover`.

**Ratified design calls** (decided by the user, 2026-08-24, grilling session):

1. Extractor: pure-Go library `github.com/ledongthuc/pdf`. No `pdftotext` subprocess, no
   hybrid path.
2. Detection: content sniff only — a file whose first bytes are `%PDF-` is a PDF; the file
   name is ignored. A text file named `notes.pdf` reads as text; a real PDF without the
   extension still extracts.
3. Output: extracted text gets a `[Page N]` marker line before each page's text, then flows
   through the EXISTING `renderFile` pipeline unchanged — `start_line`/`end_line`/
   `max_lines`/`locate` operate on the extracted text. No schema change. The header names
   the PDF via the display path (see item 2). Oversized output is the existing
   `toolresultcap` mechanism's job — no new cap.
4. Failures return an `IsError` result with a cause-specific, model-facing message that
   names the way out (house style). Two cases: parse succeeded but no text ("likely scanned
   images; OCR is not supported — ask the user for a text version"), and any parser error or
   recovered panic ("could not extract text from this PDF: <reason>"). Encrypted PDFs fall
   under the second case. Never fall back to raw bytes.
5. Scope: `read_file` only, PDFs only. grep-in-PDFs and other formats (docx, epub) go to
   `IDEAS.md`, not this plan.

**Standing requirements:**

- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**

- OCR of scanned PDFs.
- PDF support in `grep`, `find_replace`, `web_fetch`, or any tool other than `read_file`.
- Other document formats (docx, epub, rtf).
- Page-addressed parameters (`start_page` etc.) or any tool-schema change.
- Version bump (see closing note).

## 1. PDF text extraction helper — ✅ DONE (2026-08-24)

NOTES (2026-08-24): `go mod tidy` was run after adding the import so `github.com/ledongthuc/pdf` is recorded as a DIRECT requirement in `go.mod` rather than the `// indirect` line `go get` left before any code imported it.

NOTES (2026-08-24): `TestDocMapNamesEveryFile` in this package is red until item 3 adds `pdf_text.go` to `internal/tools/doc.go`'s file map — the package map guard fails on any unnamed non-test file. Item 1's own acceptance (`go build ./...`, `go test ./internal/tools/ -run 'PDF|Pdf' -v`) passes; item 2's acceptance as written (`go test ./internal/tools/`) will also show this one failure until item 3 lands.

**What:** Add the dependency and a self-contained extraction helper in the tools package —
one deep module: detection, extraction, page markers, and failure wording all live here, and
`read_file.go` (item 2) only calls it.

- `go get github.com/ledongthuc/pdf` (pin whatever `@latest` resolves to; record it in
  `go.mod`).
- New file `internal/tools/pdf_text.go` with two package-private functions:
  - `isPDF(data []byte) bool` — true when the data begins with the bytes `%PDF-`.
  - `extractPDFText(data []byte) (text string, pages int, failMessage string)` — parses the
    PDF from memory (`pdf.NewReader` over a `bytes.Reader`), walks the pages in order, and
    returns the concatenated text with a marker line `[Page N]` (alone on its line) before
    each page's text. Exactly one blank line separates a page's text from the next marker.
    Empty result contract: `failMessage != ""` means failure and `text`/`pages` are
    meaningless; `failMessage == ""` means `text` is the document.
  - The ENTIRE parse-and-extract body runs inside a deferred `recover`; a recovered panic
    becomes a failure, never a crash.
  - Failure wording (binding, from ratified call 4):
    - parse succeeded, zero extractable characters across all pages:
      `PDF contains no extractable text (likely scanned images; OCR is not supported) — ask the user for a text version of this document`
    - parser error or recovered panic:
      `could not extract text from this PDF: <error text> — the file may be corrupted or encrypted; ask the user for a text version`
  - Per-page extraction errors on an otherwise-working document do not abort the file: that
    page's text becomes `[Page N: text extraction failed]` and the walk continues. Only a
    document-level failure (reader construction, panic) uses the failure messages above.
- Tests `internal/tools/pdf_text_test.go` with fixtures under `internal/tools/testdata/`:
  - `minimal.pdf` — a hand-written minimal one-page PDF containing the text `Hello Apogee`
    (a valid xref-table PDF is ~1 KB by hand; commit the bytes as a fixture, do not generate
    at test time). Asserts: `isPDF` true, extraction returns the text, one `[Page 1]`
    marker, `pages == 1`.
  - `notext.pdf` — a hand-written valid PDF whose single page has no text operators.
    Asserts the no-extractable-text message.
  - Malformed case built in the test from bytes `%PDF-1.4` + garbage: asserts the
    could-not-extract message and that no panic escapes.
  - `isPDF` false for plain text, empty input, and input shorter than the marker.

**Files:** `go.mod`, `go.sum`, `internal/tools/pdf_text.go`,
`internal/tools/pdf_text_test.go`, `internal/tools/testdata/minimal.pdf`,
`internal/tools/testdata/notext.pdf`

**Tests:** as listed in What — all in `pdf_text_test.go`.

**Acceptance:**

```
go build ./...
go test ./internal/tools/ -run 'PDF|Pdf' -v
```

**Commit:** `feat(tools): pure-Go PDF text extraction helper with page markers`

## 2. Wire extraction into read_file — ✅ DONE (2026-08-24)

Depends on item 1.

NOTES (2026-08-24): `go test ./internal/tools/` shows exactly one failure, `TestDocMapNamesEveryFile` ("pdf_text.go is not named in tools/doc.go") — item 1 predicted it and item 3 owns the fix; every other test in the package passes, including the new PDF ones. Left untouched as out of scope.

NOTES (2026-08-24): item 1's CHANGELOG entry closes with "The helper is not yet wired into any tool; `read_file` still reads a PDF as bytes until plan item 2 lands." That sentence is stale once this item's entry lands — recommend the verifier drop it while applying this entry (the implementer never edits the CHANGELOG).

**What:** In `internal/tools/read_file.go` `Execute`, after `readBounded` succeeds and
before `renderFile`:

- If `isPDF(content)`: call `extractPDFText`. A non-empty `failMessage` returns
  `errorResult(call.ID, failMessage)`. Otherwise render via the EXISTING pipeline by
  passing the extracted text as the content and an annotated display path
  `<path> (PDF, <pages> pages)` (singular `1 page`) so the standard header names the
  format without touching `renderFile`. The `resolvedTargetNote` tail is appended exactly
  as for a plain read.
- Non-PDF files: byte-identical behavior to today.
- Update `readFileSpec.description`: append the sentence
  `PDF files are detected by content and returned as extracted plain text with [Page N] markers.`
  No schema change.
- Tests in `internal/tools/read_file_test.go`, reusing item 1's fixtures via the tool's
  Execute path (place copies or read the same `testdata` files into the test workspace):
  - Reading `minimal.pdf` returns the text, the `[Page 1]` marker, and a header containing
    `(PDF, 1 page)`.
  - `start_line`/`locate` operate on the extracted text (locate the fixture's known word).
  - Reading `notext.pdf` yields an IsError result with the binding no-text message.
  - A `.pdf`-named plain-text file reads as plain text (detection is content, not name).
  - A non-PDF read's output is unchanged (existing tests keep passing).

**Files:** `internal/tools/read_file.go`, `internal/tools/read_file_test.go`

**Tests:** as listed in What.

**Acceptance:**

```
go build ./...
go test ./internal/tools/
```

**Commit:** `feat(tools): read_file returns extracted text for PDF files`

## 3. Documentation and follow-up ideas

Depends on item 2.

**What:**

- `internal/tools/doc.go`: in the package map's "Reading and discovery" section, add one
  sentence naming `pdf_text.go` as read_file's PDF text extraction (detection by `%PDF-`
  sniff, extraction behind recover, model-facing failure wording).
- `IDEAS.md`: add one entry for the deferred follow-ups, dated 2026-08-24: grep matching
  inside PDFs, and extraction for further document formats (docx, epub) — both consciously
  left out of the 2026-08-24 read_file PDF plan.

**Files:** `internal/tools/doc.go`, `IDEAS.md`

**Tests:** none (documentation only).

**Acceptance:**

```
go build ./...
go vet ./internal/tools/
```

**Commit:** `docs(tools): document read_file PDF extraction; record follow-up ideas`

## Suggested version bump

Minor (`v0.17.0`): a new user-visible capability in a built-in tool, no breaking change.
The user decides; no item in this plan changes `VERSION` or the CHANGELOG release headings.
