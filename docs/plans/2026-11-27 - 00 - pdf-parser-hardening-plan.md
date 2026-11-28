# PDF parser hardening — the P0/P1 code-audit findings

**Goal:** a hostile or corrupt PDF read through `read_file` or `@file` can neither kill nor stall
apogee, nor write to stdout, and a valid long PDF extracts within the read budget. Fixes the seven
P0/P1 `internal/doctext` findings of the 2026-11-27 code audit.
**Date:** 2026-11-27
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** d0b401777

**Sources:**
- `docs/reviews/code-audit-2026-11-27.md` (PDF findings, recommended order)
- `.capcom/issues/pdf-*.md` (the seven issues named on the items' `**Closes:**` lines)
- `internal/doctext/pdf.go` file header and `ExtractPDF` "# The bounds" (the stated invariants)
- `internal/doctext/pdfparse/doc.go` "# Local patches" (every pdfparse change is listed there)

**Ratified design calls:**
- **Budget refusal wording:** neutral — `extraction budget exhausted: the document needs more than 200000 reads — it is very large or its object graph does not terminate` (owner, 2026-11-27).
- **Debug print:** deleted, not gated (writer, 2026-11-27).
- **Interpreter stack cap:** `1 << 16` operands; past it the page fails, not the document (writer).
- **CMap past its entry cap:** the CMap is dropped (`ok=false` → `nopEncoder`), as a bad codespace already is (writer).
- **Re-inflation:** memoise the nesting verdict per content-stream object, not a shared byte budget, so valid long documents keep extracting (writer).

**Standing requirements:**
- skills: coding-standards
- Every pdfparse behaviour change adds or extends an entry in `pdfparse/doc.go` "# Local patches", in its existing `//   - Title (files): prose` form.
- A test that needs a duration uses `internal/testtime`; allocation tests follow `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation` (serial, measured).
- This box is a Raspberry Pi: run tests only as one package and one `-run` pattern, prefixed `GOMEMLIMIT=2GiB`; never two test runs at once; never `./...`.

**Out of scope:**
- Fuzz targets (`untrusted-parsers-unfuzzed`), other unbounded reads (`context-files-read-unbounded`, `project-config-read-unbounded`, `write-file-read-uncapped`), the lint rule (`unbounded-reads-unlinted`).
- Raising `pdfMaxReads`, `pdfMaxPages` or `pdfMaxInflatedBytes`.

**Regression check (2026-11-27, d0b401777):**
- 2: guard folded (cap in `Stack.Push`; a CMap block past `MaxStack/2` entries fails its page)
- 3: guard folded (2,000,000 fixture; clamp an over-count, drop only a negative one; acceptance pattern)
- 4: recast (writer's decision: `maxCmapEntries` also counts codespace ranges) + guards folded (overlap and duplicate priority)
- 5: guard folded (`depth_test.go` call sites; counting-context bite check)
- 6: guard folded (writer's decision on the table bound + 2,000,000 fixture, overflow-free compare, newest `/Size` for Prev streams)
- 7: guard folded (writer's decision: `exceedsBound` itself + `xref` token match, three-token entry walk, `%s` stated)
- 8: guard folded (memo keyed on the stream's body offset as well)
- 9: guard folded (bite-able linear-reads sizes; lazy page list)
- 4 (re-check): recast (writer's decision: codespace ranges capped separately by `maxCmapCodespaceRanges` = 256; `maxCmapEntries` bounds bfchar + bfrange only; index re-run per `Tf` stays in item 9) + guards folded (Approach follows append-order priority, no semantic change; fixtures ≤ 100 entries per bf block, bite input sized inside the go test timeout; bfrange bounds stay lexicographic, mismatched-width `hi` in the oracle fixture)

## 1. Remove the unconditional debug print from the PDF dictionary reader

**What:** fix for `pdf-debug-printf-stdout`.
**Goal:** reading a PDF dictionary with a non-name key writes nothing to stdout and still fails as a parse error; no `fmt.Print*` in `internal/doctext/pdfparse` runs outside a `DebugOn` guard.
**Approach (assumed at the header base):** delete the `fmt.Printf("DEBUG: …")` line in `buffer.readDict` (lex.go); the `b.errorf("unexpected non-name key …")` after it stays and already carries the text. Add a "Silent dictionary reader" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/lex.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdfparse/stdout_test.go`
**Read first:** internal/doctext/pdfparse/lex.go — buffer.readDict, buffer.errorf; internal/doctext/pdfparse/eof_test.go — programBuffer; internal/doctext/pdfparse/depth_test.go — panicOf;
internal/doctext/pdfparse/doc.go — "# Local patches"; internal/doctext/pdfparse/read.go — DebugOn
**Closes:** pdf-debug-printf-stdout
**Tests:** `TestReadDict_NonNameKeyWritesNothingToStdout` — serial; redirects `os.Stdout` to an `os.Pipe`, reads a dict `<< (k\x1b]52;c;AAAA\x07) 1 >>` through `programBuffer` / `panicOf`, asserts the pipe holds zero bytes and the read still panics.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReadDict_NonNameKeyWritesNothingToStdout' ./internal/doctext/pdfparse/`
- `go vet ./internal/doctext/pdfparse/`
- `! grep -n 'fmt.Printf("DEBUG' internal/doctext/pdfparse/lex.go`
**Commit:** `fix(pdfparse): drop the debug print that wrote document bytes to stdout`

## 2. Cap the content-stream interpreter stack

**What:** fix for `pdf-interpreter-stack-uncapped`.
**Regression guard.** The cap lives in `Stack.Push` (ps.go), not only before Interpret's own pushes, so every push is counted — including `readCmap`'s callback pushes at `findresource`, `begincmap` and `defineresource` (page.go), which a CMap of repeated `begincmap` would otherwise grow without bound. A CMap holds 2 (bfchar) or 3 (bfrange) operands per entry on the stack until its `end` operator, so one bf block past `MaxStack/2` bfchar (`MaxStack/3` bfrange) entries fails its page: this Goal and the doc.go "Stack bound" entry state that (a conforming block holds at most 100 entries; item 4's whole-CMap cap is a separate rule).
**Goal:** `Interpret` never holds more than `MaxStack` (`1 << 16`) operands; a stream that pushes past it fails with the exported sentinel `ErrStackOverflow` (text names the cap); `Page.GetPlainText` returns that as the page's error, so `ExtractPDF` marks only that page `[Page N: text extraction failed]` and continues.
**Approach (assumed at the header base):** check `stk.Len()` before each push in `Interpret` (ps.go) and panic with `ErrStackOverflow`. The page recovers in page.go must not route it through `rethrowTooDeep` (only `ErrTooDeep` fails the document). A CMap interpreted from `Font.Encoder` inside `GetPlainText` hits the same cap and fails the same page. Add a "Stack bound" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/ps.go`, `internal/doctext/pdfparse/page.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdfparse/stack_test.go`, `internal/doctext/pdf_test.go`
**Read first:** internal/doctext/pdfparse/ps.go — Interpret, Stack.Push; internal/doctext/pdfparse/page.go — Page.GetPlainText, rethrowTooDeep, readCmap;
internal/doctext/pdf.go — walkPages; internal/doctext/pdfparse/depth_test.go — panicOf; internal/doctext/pdf_test.go — deflatedStream
**Closes:** pdf-interpreter-stack-uncapped
**Tests:**
- `TestInterpret_RefusesPastMaxStack` (pdfparse): `MaxStack+1` operands panic with `ErrStackOverflow`; `MaxStack` operands followed by an operator do not; a callback-push case (a callback that pushes on every operator, as `begincmap` does) past `MaxStack` panics with `ErrStackOverflow` too.
- `TestExtractPDF_FailsOnlyThePageWhoseStackOverflows` (doctext): two pages, page 1 a `deflatedStream` of `1 ` × (`MaxStack`+10); page 1 carries the failed marker, page 2's text is present.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestInterpret_RefusesPastMaxStack' ./internal/doctext/pdfparse/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExtractPDF_FailsOnlyThePageWhoseStackOverflows' ./internal/doctext/`
**Commit:** `fix(pdfparse): cap the content-stream interpreter stack`

## 3. Bound a ToUnicode CMap's bfchar/bfrange count by its stack

**What:** fix for `pdf-cmap-count-oom`.
**Regression guard.** A count above the operands on the stack is CLAMPED to `stk.Len()/2` (bfchar) or `stk.Len()/3` (bfrange), not dropped; only a negative count drops the CMap. Today an overstated count (`3 beginbfchar` with two pairs) pops empty values whose entries never match (page.go `cmap.Decode`), so the CMap still decodes its real entries — the clamp keeps that exactly and removes the per-declared-entry allocation. The bite fixture declares 2,000,000, not 4,000,000,000: an unrefused parse then appends ~64 MB and fits in memory (as `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation` sizes its count) instead of OOM-killing the box.
**Goal:** a CMap whose `beginbfchar`/`beginbfrange` count is negative or exceeds the operands actually on the stack (`stk.Len()/2` for bfchar, `/3` for bfrange) is dropped (`readCmap` returns nil) without allocating per declared entry; a well-formed CMap decodes as before.
**Approach (assumed at the header base):** in `readCmap`'s `Interpret` callback (page.go), at `endbfchar` / `endbfrange` compare `n` with the stack before looping; out of range → `ok = false` and skip the loop. Reset `n` after each bf loop. Add a "CMap entry count" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/page.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdf_test.go`
**Read first:** internal/doctext/pdfparse/page.go — readCmap, Font.charmapEncoding; internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation, nestingPDF,
fontWithToUnicode, identityCMap, TestExtractPDF_EndsAtTheEndOfATruncatedStream, deflatedStream
**Closes:** pdf-cmap-count-oom
**Tests:** `TestExtractPDF_RefusesACMapCountPastItsStack` — serial, measured like `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation`: a font whose `/ToUnicode` is a `deflatedStream` of `2000000 beginbfchar endbfchar` (and a bfrange twin); ExtractPDF returns without panicking and total allocation stays under a few MiB. `TestExtractPDF_DecodesACMapWhoseCountOverstatesItsEntries`: a CMap of `3 beginbfchar` with two pairs still decodes both codes. Existing `identityCMap` tests (`TestExtractPDF_EndsAtTheEndOfATruncatedStream`) still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExtractPDF_RefusesACMapCountPastItsStack|CMap|TestExtractPDF_EndsAtTheEndOfATruncatedStream' ./internal/doctext/`
**Commit:** `fix(pdfparse): bound a CMap's entry count by the operands on its stack`

## 4. Index CMap lookups and cap a CMap's entries

**What:** Recast at the regression check (2026-11-27). First half of `pdf-cmap-decode-quadratic` (item 5 is the cancellation half). Depends on item 3.
**Regression guard.** Codespace ranges get their own cap, maxCmapCodespaceRanges = 256 per CMap (a conforming CMap declares a handful); past it the CMap is dropped (ok=false), so cmap.Decode's per-character codespace scan is bounded by 256, not by maxCmapEntries; maxCmapEntries (1<<17) bounds bfchar + bfrange only. The index build is one pass plus one sort; its re-run on every Tf (Font.Encoder's value receiver) is item 9's fix, which stays in item 9.
The two bounds are separate rules and both stand — item 2's MaxStack (1<<16 operands) bounds one bf block, item 4's maxCmapEntries bounds the whole CMap across blocks; a conforming CMap holds at most 100 entries per bf block, so neither binds one.
Also: a duplicate's winner is the FIRST in `m.bfchar` / `m.bfrange` append order (build the index by iterating those slices) — `readCmap` pops each block last-entry-first, so today the last duplicate in a block wins, not the first in document order. Overlapping ranges keep today's priority: resolve them at build time into disjoint intervals in append-order priority (or fall back to the ordered scan when the candidate misses and a prefix-max `hi` says an earlier range may cover the code), then binary-search — nested `<0000> <00FF> <0000>` + `<0041> <0041> <0061>` must still decode `<0050>` through the outer range. No semantic change: the Approach below follows this priority, and `doc.go` records none.
Also: bfrange bounds stay lexicographic string compares (no integer conversion) — `readCmap` never checks `len(hi) == len(lo)` for a bfrange, so `<0041> <42> <0061>` covers `<0050>` today and must still decode "p".
Also: every fixture built through `readCmap` emits bf blocks of ≤ 100 entries and a single codespace range (a 100 000-entry block overflows item 2's MaxStack); size the bite input so the pre-item linear scan exceeds the `testtime.Budget` by ~10x yet finishes well inside the go test timeout under `-race` on this Pi.
**Goal:** decoding one character through a `cmap` costs O(1) for bfchar and O(log n) for bfrange, independent of the entry count; a CMap past `maxCmapEntries` (`1 << 17`, bfchar + bfrange together) or past `maxCmapCodespaceRanges` (256 codespace ranges) is dropped (`ok=false`).
**Approach (assumed at the header base):** in page.go, build after interpretation: bfchar as `map[string]string` keyed by code bytes; bfrange sorted by `lo` and searched with `sort.Search`, bounds compared as strings — duplicates and overlaps resolved in append-order priority exactly as the **Regression guard** states (no semantic change). `cmap.Decode` keeps its order: codespace, then bfchar, then bfrange. In `readCmap`, count bfchar + bfrange entries against `maxCmapEntries` and codespace ranges against `maxCmapCodespaceRanges`, and drop the CMap past either cap. Add a "CMap lookup index" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/page.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdfparse/cmap_test.go`
**Read first:** internal/doctext/pdfparse/page.go — readCmap, cmap.Decode, bfchar, bfrange; internal/doctext/pdfparse/ps.go — Interpret;
internal/doctext/pdfparse/doc.go — "# Local patches"; internal/doctext/pdf_test.go — identityCMap; internal/testtime/testtime.go — Budget
**Tests:** in `cmap_test.go` (every fixture's bf blocks ≤ 100 entries, one codespace range): `TestCmapDecode_MatchesTheLinearScan` (a fixed CMap with duplicates — one inside a single block — adjacent ranges, nested ranges and a mismatched-width `hi` range (`<0041> <42> <0061>`) decodes exactly as the pre-item linear `Decode`, kept in the test file as an oracle, not a hand-written expectation); `TestCmapDecode_CostIndependentOfEntryCount` (a 1 MiB string decodes through a 100 000-entry CMap within `testtime.Budget`); `TestReadCmap_DropsACMapPastTheEntryCap` (bfchar + bfrange past `maxCmapEntries` across blocks, and codespace ranges past `maxCmapCodespaceRanges`, each dropped).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestCmapDecode|TestReadCmap' ./internal/doctext/pdfparse/`
**Commit:** `fix(pdfparse): index CMap lookups and cap a CMap's entries`

## 5. Let cancellation interrupt text extraction inside a page

**What:** second half of `pdf-cmap-decode-quadratic`. Depends on item 4.
**Regression guard.** The signature change updates every caller: `internal/doctext/pdfparse/depth_test.go` (`TestPage_FailsTheDocumentPastMaxDepthAndReadsTheNextPage`, both `GetPlainText(nil)` calls) and `pdf_test.go` (`TestExtractPDF_EncryptedFixtureOpensWithTheEmptyPassword`) take the new ctx argument. The bite check uses no started signal (ExtractPDF emits none) and no clock: a test context whose `Err()` counts its calls and cancels itself on the Kth, K above every `Err()` call the extraction makes without per-operator polling (`budgetedReaderAt.ReadAt` polls on each read, pdf.go) — pre-item the page returns text with `failMessage` ""; post-item it ends with `pdfCancelledCause`. (A plainly cancelled one-page run reports `pdfCancelledCause` today from `walkPages`' post-loop `source.failure()`, so it cannot bite.)
**Goal:** `Page.GetPlainText` takes a `context.Context` and stops within one content-stream operator of its cancellation, returning the context's error; a cancelled `ExtractPDF` mid-page reports `pdfCancelledCause`.
**Approach (assumed at the header base):** `func (p Page) GetPlainText(ctx context.Context, fonts map[string]*Font)`; its `Interpret` callback checks `ctx.Err()` per operator and panics with it, recovered into the page error. `Reader.GetPlainText` in pdfparse passes its own ctx parameter through (add one). `walkPages` (pdf.go) passes `source.ctx`. Update the `ExtractPDF` "# The bounds" prose that says ctx is checked "before every page and on every read" to add "and on every content-stream operator". Add a "Cancellable page text" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/page.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`, `internal/doctext/pdfparse/depth_test.go`
**Read first:** internal/doctext/pdfparse/page.go — Page.GetPlainText; internal/doctext/pdf.go — walkPages, budgetedReaderAt.ReadAt, budgetedReaderAt.failure, ExtractPDF "# The bounds";
internal/doctext/pdfparse/depth_test.go — TestPage_FailsTheDocumentPastMaxDepthAndReadsTheNextPage; internal/doctext/pdf_test.go — TestExtractPDF_HonoursACancelledContext, TestExtractPDF_EncryptedFixtureOpensWithTheEmptyPassword
**Closes:** pdf-cmap-decode-quadratic
**Tests:** `TestExtractPDF_CancelInsideAPageEndsPromptly` — one page of many `Tj` operators over a large CMap; a counting context cancels itself on its Kth `Err()` call (see the guard); ExtractPDF returns `pdfCancelledCause`, where the pre-item tree returns the page's text with `failMessage` "". `TestPage_FailsTheDocumentPastMaxDepthAndReadsTheNextPage` and `TestExtractPDF_EncryptedFixtureOpensWithTheEmptyPassword` compile against the new signature. `TestExtractPDF_HonoursACancelledContext` still passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExtractPDF_CancelInsideAPageEndsPromptly|TestExtractPDF_HonoursACancelledContext' ./internal/doctext/`
- `go build ./... && go vet ./internal/doctext/...`
**Commit:** `fix(pdfparse): poll cancellation on every content-stream operator`

## 6. Refuse an xref subsection that reaches past the document

**What:** in-parser half of `pdf-xref-start-unbounded` (item 7 is the preflight half).
**Regression guard.** Item 6's table bound is exactly exceedsBound's rule in pdf.go (refuse only when the declared count is strictly greater than the document's byte length), so a fixture item 7 refuses in the preflight never reaches item 6's parser error first and vice versa.
Also: compare without adding — refuse `start < 0 || n < 0 || start > bound || n > bound-start` — so `xref\n9223372036854775807 1` cannot wrap `start + n` negative. Every xref stream in the Prev chain is bounded by the newest, already-allocated `size` (read.go `readXrefStream`), not by its own `psize` (a missing /Size reads as 0). The bite fixtures declare ~2,000,000, not 4,000,000,000, so an unguarded parse grows ~64 MB and fails the ceiling rather than the box.
**Goal:** `readXrefTableData` and `readXrefStreamData` refuse a subsection whose `start`, `n` is negative or whose `start + n` exceeds the bound — the file size in bytes for a table, the already-checked `/Size` for a stream `/Index` — before growing the table; no xref growth is unbounded, `/W [0 0 0]` included.
**Approach (assumed at the header base):** in read.go, check each subsection header before its entry loop and fail through the reader's existing error path (`errorf` / returned error, as neighbouring checks do); the table bound uses the reader's file size (`r.end`), matching `exceedsBound`'s "objects in bytes" reasoning in pdf.go. Add an "Xref subsection bound" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/read.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdfparse/xref_test.go`
**Read first:** internal/doctext/pdfparse/read.go — readXrefTableData, readXrefTable, readXrefStreamData, readXrefStream; internal/doctext/pdfparse/lex.go — buffer.readToken;
internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation, xrefStreamPDF; internal/doctext/pdfparse/depth_test.go — panicOf
**Tests:** in `xref_test.go`: a ~100-byte table with `xref\n2000000 1`, a table with `xref\n9223372036854775807 1`, and a stream with `/W [0 0 0] /Index [0 2000000]` (built via `NewReader` on `bytes.Reader`) are each refused with an error and bounded allocation; a valid multi-subsection table still reads; a Prev xref stream without /Size whose /Index rows lie within the newest /Size still reads.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Xref' ./internal/doctext/pdfparse/`
**Commit:** `fix(pdfparse): refuse an xref subsection past the document's bound`

## 7. Preflight xref subsection headers and /Index beside /Size

**What:** preflight half of `pdf-xref-start-unbounded`. Depends on item 6.
**Regression guard.** Item 7's preflight uses exceedsBound itself (the same strict "objects > len(data)" bound item 6 mirrors), and its tests assert the preflight's exact pdfAbsurdSizeFormat failMessage.
Also: take only an `xref` token — the byte before it not regular, as `pdfStreamSites` does for `stream` — so the tail of `startxref` is never read as a subsection header. Walk each entry as three tokens (digits, digits, `f`|`n`) as `readXrefTableData` does, never a fixed 20-byte skip, which misaligns on 19- or 21-byte entry lines; the walk is bounded because `n` is checked first. `%s` in `pdfAbsurdSizeFormat` quotes `start + n` in decimal, or the digit string `exceedsBound` refused on its length alone.
**Goal:** `refuseAbsurdObjectCount` refuses, with `pdfAbsurdSizeFormat`, a document whose xref-table subsection header or `/Type /XRef` `/Index` pair declares `start + n` beyond `exceedsBound` for its byte length, before `NewReader` runs.
**Approach (assumed at the header base):** in pdf.go extend `refuseAbsurdObjectCount`: for each `xref` keyword outside stream bodies, read `start n` pairs (`readPDFDigits`) until `trailer`, checking each before skipping its `n` 20-byte entry lines; for `/Index` reuse `pdfIntegerArrayAt` as `refuseAbsurdXrefWidths` does for `/W`. Update its doc comment.
**Files:** `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`
**Read first:** internal/doctext/pdf.go — refuseAbsurdObjectCount, pdfKeywordIndexes, pdfStreamSites, pdfIntegerArrayAt, exceedsBound; internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnAbsurdXrefSize, xrefStreamPDF;
internal/doctext/pdfparse/read.go — readXrefTableData
**Closes:** pdf-xref-start-unbounded
**Tests:** `TestExtractPDF_RefusesAnAbsurdXrefSubsection` and `TestExtractPDF_RefusesAnAbsurdXrefIndex` — exact `failMessage` built from `pdfUnreadableFormat` + `pdfAbsurdSizeFormat`; serial allocation bound. `TestExtractPDF_ExtractsAnIncrementallyUpdatedDocument`: a two-revision incremental-update document (an object after `startxref … %%EOF`) and a table with 19- and 21-byte entry lines still extract. `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation` still passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExtractPDF_RefusesAnAbsurd|TestExtractPDF_ExtractsAnIncrementallyUpdatedDocument' ./internal/doctext/`
**Commit:** `fix(doctext): preflight xref subsection headers and /Index counts`

## 8. Check a shared content stream's nesting once per document

**What:** fix for `pdf-page-reinflate-budget`.
**Regression guard.** Streams are not always indirect here: `readDict` lexes `<< … >> stream` wherever `allowStream` is on, and `resolve` gives a direct value its parent's ptr, so two direct streams in one page's /Contents array share an object ID. Key the memo on the stream's body offset (`stream.offset`) as well — `ObjectID` also returns it, or a separate accessor does — and drop the "always indirect" claim from the doc comment.
**Goal:** during one `ExtractPDF`, `pageContentNesting` inflates each distinct content-stream object at most once; pages sharing a stream reuse its verdict.
**Approach (assumed at the header base):** add to pdfparse `func (v Value) ObjectID() (id uint32, gen uint16)` returning the indirect object a Value was resolved from (meaningful for streams, which are always indirect; doc comment says so). In pdf.go, a per-document `contentNesting` memo (verdict map keyed by `ObjectID`, plus an `inflations` counter for tests) is built in `walkPages` and passed to `pageContentNesting`. Add a "Value object identity" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/read.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`
**Read first:** internal/doctext/pdf.go — pageContentNesting, walkPages, parserNestingCause; internal/doctext/pdfparse/read.go — Value, Reader.resolve;
internal/doctext/pdfparse/lex.go — buffer.readDict, stream; internal/doctext/pdf_test.go — treePagesPDF (all pages share object 3)
**Closes:** pdf-page-reinflate-budget
**Tests:** `TestPageContentNesting_InflatesASharedStreamOnce` — 50 pages whose `/Contents` all reference one stream; the memo's `inflations` is 1; a too-deep shared stream still fails every page that uses it with `pdfNestingCause`. `TestPageContentNesting_MeasuresEachDirectStream`: two direct streams in one /Contents array, the second too deep, are each inflated and the page fails with `pdfNestingCause`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPageContentNesting|Nesting' ./internal/doctext/`
**Commit:** `fix(doctext): check a shared content stream's nesting once per document`

## 9. Walk the page tree once and name the budget refusal truthfully

**What:** fix for `pdf-read-budget-refusal`. Depends on item 5 (both change `GetPlainText`'s file).
**Regression guard.** Build the page list lazily: extend the `/Kids` walk only until it holds page n, resuming where it stopped, so `Page(n)` never resolves a kid past page n — an eager walk would fail a document whose walk stops early (`maxFileReadBytes`, page cap, phantom run) on a stale-offset kid past the stop, and exhaust the budget on a >~200k-leaf tree that today returns 2000 pages. The linear-reads test compares two sizes that both stay under `pdfMaxReads` before the item (reads(400) ≤ 2.5 × reads(160), ~5.5× today) and asserts neither run exhausted the budget — reads(1000) stops at the budget today, so it cannot bite.
**Goal:** a valid 1000-page flat-tree PDF extracts all pages within `pdfMaxReads`, with reads charged growing linearly in page count; a budget refusal reports exactly `extraction budget exhausted: the document needs more than 200000 reads — it is very large or its object graph does not terminate`.
**Approach (assumed at the header base):** `Reader.Page(n)` (page.go) indexes a page list the Reader builds with one `/Kids` walk on first use (a cycle still runs into the read budget); `Font.Encoder` and `getEncoder` take pointer receivers so the CMap cache persists, callers made addressable. In pdf.go set `pdfBudgetCause` to the ratified text and rewrite the `pdfMaxReads`, `pdfBudgetCause` and `budgetedReaderAt` comments that reason from the per-page re-walk. Expose the reads count to in-package tests through an unexported seam, no exported API. Add a "Page list" entry to `doc.go`.
**Files:** `internal/doctext/pdfparse/page.go`, `internal/doctext/pdfparse/doc.go`, `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`
**Read first:** internal/doctext/pdfparse/page.go — Reader.Page, Font.Encoder, Font.getEncoder; internal/doctext/pdf.go — pdfBudgetCause, pdfMaxReads;
internal/doctext/pdf_test.go — TestExtractPDF_BoundsAReferenceCycle, textPagesPDF, TestExtractPDF_CapsThePageWalk
**Closes:** pdf-read-budget-refusal
**Tests:** `TestExtractPDF_ExtractsAThousandPageFlatTree` (`textPagesPDF` × 1000: every page's text, no failure); `TestExtractPDF_ReadsGrowLinearlyWithPages` (reads(400) ≤ 2.5 × reads(160), neither run exhausting the budget); `TestExtractPDF_PageWalkNeverResolvesPastThePageRead` (a stale-offset kid past the page cap does not fail the document); `TestExtractPDF_BoundsAReferenceCycle` updated to assert the exact new cause.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExtractPDF_ExtractsAThousandPageFlatTree|TestExtractPDF_ReadsGrowLinearlyWithPages|TestExtractPDF_PageWalkNeverResolvesPastThePageRead|TestExtractPDF_BoundsAReferenceCycle' ./internal/doctext/`
- `go build ./...`
**Commit:** `fix(pdfparse): walk the page tree once and name the read-budget refusal truthfully`
