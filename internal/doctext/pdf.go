package doctext

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDF text extraction. Everything a caller needs to know about the format lives behind the
// three functions below — detection, extraction, the page markers, the model-facing wording of
// every failure, and the header annotation — so a caller decides nothing about PDFs beyond "is
// this one", "what came out" and "how do I say so".
//
// The parser is github.com/ledongthuc/pdf, a fork of rsc.io/pdf whose ancestor PANICS on
// malformed input rather than returning an error. A tool that crashes the agent on a corrupt
// download is not an option, and neither is one that lets a document size the agent's memory or
// its walk, so the whole parse-and-walk runs behind a recover, behind bounds the DOCUMENT cannot
// raise, and a recovered panic reads to the model exactly like any other unreadable file
// (ADR 0007: a tool-level failure is an IsError result, never a Go error).
//
// The bounds are the point of the second half of that sentence. Everything the parser does is
// driven by numbers the document asserts about itself — how many objects its cross-reference
// table holds, how many pages its page tree has, which object each /Kids entry points at — and
// none of them is checked against the bytes actually present. A 594-byte file declaring ten
// million pages cost 51 s and ~142 GiB of churn; a /Size of four billion allocates a table
// before a single object is read; a /Kids array referencing its own node walks forever. So the
// declared object count is checked against the file's length before the parser sees it, the walk
// is capped, and every read the parser makes is charged against a budget and a context
// (audit 2026-08-25 — C-07, F-25, F-26). The same holds for what a stream says about ITSELF: a
// FlateDecode stream inflates to whatever it was written to, a predictor row and a
// cross-reference row are allocated at their declared widths, so those are pre-flighted over
// the raw bytes too, before the parser can act on them (audit 2026-09-20 — see preflightPDF).

const (
	// pdfNoTextMessage is what the model reads when the document parsed cleanly and yielded no
	// characters at all — overwhelmingly a scan. It names the way out (a text version) because
	// nothing the agent can do on its own will make those pixels into words.
	pdfNoTextMessage = "PDF contains no extractable text (likely scanned images; OCR is not supported)" +
		" — ask the user for a text version of this document"

	// pdfUnreadableFormat words a document-level failure: the reader refused the bytes, or the
	// parser panicked on them. The cause is quoted verbatim because "corrupted or encrypted" is a
	// guess and the parser's own sentence is the only evidence the model gets.
	pdfUnreadableFormat = "could not extract text from this PDF: %v" +
		" — the file may be corrupted or encrypted; ask the user for a text version"

	// pdfPageFailedFormat stands in for ONE page the parser choked on. A single bad page must not
	// cost the model the other ninety-nine, so the walk records the hole and carries on.
	pdfPageFailedFormat = "[Page %d: text extraction failed]"

	// pdfPageMarkerFormat labels each page's text. It sits alone on its line so the line-addressed
	// read_file pipeline (start_line, locate) can point at a page the way it points at anything.
	pdfPageMarkerFormat = "[Page %d]"

	// pdfAnnotationSingular and pdfAnnotationFormat word the header annotation every caller
	// stamps on a document it extracted. They say three things in one breath: the format, how
	// much document the text below covers, and that those lines are a RENDERING rather than the
	// file — a model that reads "extracted text, read-only" has been told, before it tries, that
	// there is nothing here to edit in place and nothing to write back.
	pdfAnnotationSingular = "PDF, 1 page; extracted text, read-only"
	pdfAnnotationFormat   = "PDF, %d pages; extracted text, read-only"

	// pdfBudgetCause and pdfCancelledCause are the two causes pdfUnreadableFormat quotes when the
	// bounds AROUND the parser, rather than the parser itself, ended the walk. Both are worded as
	// document-level failures because that is what they are for the model: nothing usable came out,
	// and asking for a text version is still the way forward. The budget's sentence names the
	// object graph rather than the budget, because "does not terminate" is the fact the reader can
	// act on and "200 000 reads" is not.
	pdfBudgetCause    = "extraction budget exhausted: the document's object graph does not terminate"
	pdfCancelledCause = "cancelled"

	// pdfAbsurdSizeFormat words the refusal of a cross-reference table the file cannot possibly
	// hold. The parser allocates one table entry per DECLARED object before it reads anything, so a
	// document declaring four billion objects sizes the agent's memory from its own trailer. Both
	// numbers are quoted because the refusal is only convincing with them side by side.
	pdfAbsurdSizeFormat = "declares %s objects in %d bytes"

	// pdfInflatedCause, pdfPredictorCause and pdfXrefWidthCause word the three pre-flight refusals
	// (see preflightPDF): the document's decoded streams inflate past the budget, a predictor row
	// is wider than any image, or a cross-reference row is wider than any integer. The first
	// quotes the budget because the model can act on "too big"; the other two quote the declared
	// number because, as with pdfAbsurdSizeFormat, the refusal is only convincing with it.
	pdfInflatedCause  = "inflated content exceeds the %d MiB budget"
	pdfPredictorCause = "declares a predictor row of %s columns"
	pdfXrefWidthCause = "declares cross-reference field widths of [%s]"

	// pdfPagesOmittedFormat is the last block of a walk that stopped before the document's final
	// page: the range that was not extracted and why. It is a BLOCK rather than a failure because
	// the pages above it are real text the model can use — the marker exists so the model reads
	// how much document it is NOT holding, the same way the structural clamp's elision marker does.
	pdfPagesOmittedFormat = "[Pages %d–%d not extracted: %s]"

	// The three reasons a walk stops early, as pdfPagesOmittedFormat quotes them.
	pdfStopPageCap          = "page cap"
	pdfStopContentBudget    = "content budget"
	pdfStopPhantomRunFormat = "no text on %d consecutive pages"
)

const (
	// pdfMaxPages bounds the page WALK. A document's /Count is a number it asserts about itself and
	// the walk pays for every page it believes, so the count is a hint here and never an
	// allocation. Two thousand pages is far past any document a coding agent reads and far below
	// the counts a hostile one declares, so a real document never meets the cap and a lying one
	// stops at it.
	pdfMaxPages = 2000

	// pdfPhantomRun is how many CONSECUTIVE pages may yield neither text nor an error before the
	// walk gives up on the rest. That run is the signature of an inflated /Count: the page tree
	// hands back null pages forever, each of them cheap, so the page cap alone would still walk two
	// thousand of them. A real document's blank pages come in ones and twos between pages that
	// carry text, and any page that carries either text or a parse error resets the run.
	pdfPhantomRun = 25

	// pdfMaxReads bounds how many ReadAt calls the parser may make over one document. The parser
	// keeps no value cache (read.go:55), so every reference it resolves is a fresh read — which is
	// what makes a bound on READS a bound on the object graph it walks, including a /Kids entry
	// pointing at its own node, which no page-level bound can catch because it never returns a
	// page at all. Two hundred thousand reads is a hundred per page at the page cap: generous for a
	// real document, finite for a cyclic one.
	pdfMaxReads = 200_000

	// pdfMaxInflatedBytes bounds how far the streams the text path decodes may inflate, summed
	// over the whole document (see preflightPDF). The parser inflates a FlateDecode stream with no
	// ceiling but the stream's own, so an 80 KiB file can ask for gigabytes; 64 MiB of decoded
	// content is far past any document's page text, cross-reference and object streams together.
	pdfMaxInflatedBytes = 64 << 20

	// pdfInflateChunk is how many inflated bytes are drained at a time while measuring a stream
	// against pdfMaxInflatedBytes — the interval at which cancellation is read.
	pdfInflateChunk = 1 << 20

	// pdfMaxPredictorColumns bounds /Columns in a stream's /DecodeParms: the parser's PNG
	// predictor allocates two rows of that many bytes before it reads a byte of the stream.
	// Sixty-four thousand columns is wider than any image a document carries.
	pdfMaxPredictorColumns = 1 << 16

	// pdfMaxXrefFieldWidth and pdfMaxXrefRowWidth bound a cross-reference stream's /W array: the
	// parser allocates one row of the summed widths, and decodeInt reads each field as a
	// big-endian integer, which is never wider than eight bytes.
	pdfMaxXrefFieldWidth = 8
	pdfMaxXrefRowWidth   = 24
)

// pdfMagic is the signature every PDF file opens with. Detection is a content sniff and nothing
// else: a text file someone named notes.pdf must read as text, and a real PDF saved without the
// extension must still extract.
var pdfMagic = []byte("%PDF-")

// IsPDF reports whether data is a PDF document, judged solely by its leading bytes. Input
// shorter than the signature — the empty file included — is not a PDF.
func IsPDF(data []byte) bool {
	return bytes.HasPrefix(data, pdfMagic)
}

// ExtractPDF parses an in-memory PDF and returns its text with a "[Page N]" marker line
// before each page's text, exactly one blank line between one page's text and the next marker.
//
// The three results are one of two shapes, never a mix: failMessage != "" is a failure and both
// text and pages are meaningless; failMessage == "" means text is as much of the document as the
// bounds below allowed and pages is how many pages that text covers. The failure string is
// written FOR THE MODEL — it is returned to the caller to hand straight to errorResult, not
// wrapped or re-worded.
//
// A panic out of the parser is a failure like any other; nothing escapes this function. A page
// that fails on its own does NOT fail the document: its text becomes a
// "[Page N: text extraction failed]" placeholder and the walk continues. A document that yields
// no characters on any page it kept is the scanned-image case and fails with pdfNoTextMessage —
// no caller falls back to raw bytes, because a wall of binary teaches the model nothing. A
// document that parses to NO pages is a document-level failure instead: nothing was read from
// it, so it cannot be reported as a scan.
//
// # The bounds
//
// ctx cancels the walk: it is checked before every page and on every read the parser makes, so a
// cancelled Turn stops a document mid-walk instead of after it. maxTextBytes caps the text this
// returns — <= 0 is unbounded — and stops the walk after the page that crosses it, so a caller
// that will clamp the result to a budget does not pay to extract what it is about to drop.
// Three bounds the caller does not set apply always: pdfMaxPages caps the walk, pdfPhantomRun
// abandons a document whose page tree has run out of real pages, and pdfMaxReads bounds the
// object graph the parser may walk. A document declaring more objects than its own byte length
// could hold is refused before the parser allocates for them.
//
// When any of those stopped the walk before the document's last page, the text ends with one
// "[Pages N+1–M not extracted: <reason>]" block and pages is N — the last page actually
// extracted — so the model reads both what it has and what it does not.
func ExtractPDF(ctx context.Context, data []byte, maxTextBytes int) (text string, pages int, failMessage string) {
	source := &budgetedReaderAt{ctx: ctx, source: bytes.NewReader(data)}
	defer func() {
		if recovered := recover(); recovered != nil {
			text, pages, failMessage = "", 0, source.failureFor(recovered)
		}
	}()

	if refusal := refuseAbsurdObjectCount(data); refusal != "" {
		return "", 0, refusal
	}
	if cause := preflightPDF(ctx, data); cause != "" {
		return "", 0, fmt.Sprintf(pdfUnreadableFormat, cause)
	}

	reader, err := pdf.NewReader(source, int64(len(data)))
	if err != nil {
		return "", 0, source.failureFor(err)
	}

	declared := reader.NumPage()
	// Zero pages is the reader accepting bytes it could not walk: the page tree yields nothing, so
	// the walk below never runs and every page-level signal stays untouched. That is a
	// document-level failure — nothing was read from this file — and not the scan
	// pdfNoTextMessage describes, so it goes out with the cause named.
	if declared <= 0 {
		return "", 0, source.failureFor("the document has no pages")
	}

	return walkPages(reader, source, declared, maxTextBytes)
}

// walkPages renders a parsed document's pages under all three walk bounds at once — the page
// cap, the phantom run and the caller's output ceiling — and returns ExtractPDF's three results.
// It is separate from ExtractPDF because the two answer different questions: ExtractPDF decides
// whether there is a document here at all, walkPages decides how much of it comes back.
func walkPages(reader *pdf.Reader, source *budgetedReaderAt, declared, maxTextBytes int) (string, int, string) {
	walk := min(declared, pdfMaxPages)
	acc := pageAccumulator{blocks: make([]string, 0, min(walk, 64))}
	stopped := ""

	for number := 1; number <= walk && stopped == ""; number++ {
		if failure := source.failure(); failure != "" {
			return "", 0, failure
		}
		pageText, pageErr := reader.Page(number).GetPlainText(nil)
		acc.record(number, pageText, pageErr)
		switch {
		case acc.phantom >= pdfPhantomRun:
			acc.dropPending()
			stopped = fmt.Sprintf(pdfStopPhantomRunFormat, pdfPhantomRun)
		case maxTextBytes > 0 && acc.textBytes > maxTextBytes:
			stopped = pdfStopContentBudget
		}
	}
	// A bound can trip on the LAST read of the last page the walk was going to make, which the
	// check at the top of the loop would never see. A document whose graph did not terminate is a
	// failure however late that becomes visible.
	if failure := source.failure(); failure != "" {
		return "", 0, failure
	}

	if stopped == "" {
		// A blank run that ended because the DOCUMENT ended is the document's own trailing blank
		// pages, not the phantom tail of a lying /Count, so it belongs in the output.
		acc.commitPending()
		if walk < declared {
			stopped = pdfStopPageCap
		}
	}
	if !acc.hasText {
		return "", 0, pdfNoTextMessage
	}
	// The marker is a claim about pages that were NOT extracted, so it is only true when some were
	// left: the output ceiling can be crossed by the document's very last page, which stops a walk
	// that had nothing left to do anyway.
	if stopped != "" && acc.lastKept < declared {
		acc.blocks = append(acc.blocks, fmt.Sprintf(pdfPagesOmittedFormat, acc.lastKept+1, declared, stopped))
	}
	return strings.Join(acc.blocks, "\n\n"), acc.lastKept, ""
}

// pageAccumulator collects one walk's rendered pages. It exists because a blank page only means
// something in context: a blank page between two pages that carry text is the document's own,
// while pdfPhantomRun blank pages in a row are the tail of a /Count that outran the page tree.
// So blank pages are HELD until a later page — or the end of the walk — decides which they were.
type pageAccumulator struct {
	blocks      []string
	pending     []string
	pendingLast int
	phantom     int
	textBytes   int
	hasText     bool
	lastKept    int
}

// record renders one page's outcome. A page the parser could not read is a HOLE in the document
// rather than a blank page: it is kept, and it resets the phantom run, because one unreadable
// page must not cost the model the other ninety-nine. A page that carries text is joined into
// lines (joinWrappedLines) before it is kept: the parser breaks a line at every text object, and
// a producer that emits one object per word hands the model one word per line.
func (a *pageAccumulator) record(number int, pageText string, pageErr error) {
	switch {
	case pageErr != nil:
		a.keep(number, pageBlock(number, fmt.Sprintf(pdfPageFailedFormat, number)))
	case strings.TrimSpace(pageText) == "":
		a.hold(number, pageBlock(number, ""))
	default:
		a.hasText = true
		a.keep(number, pageBlock(number, joinWrappedLines(strings.TrimRight(pageText, " \t\r\n"))))
	}
}

// sentenceEnds are the characters a line ends with when it ends a sentence or a clause — the one
// signal, in text the parser has already flattened, that the next line starts something new
// rather than continuing this one.
const sentenceEnds = ".:;?!"

// joinWrappedLines joins the line breaks the parser put INSIDE a sentence back into spaces. The
// parser writes a newline at every text object (BT) and every explicit line move, so a page laid
// out one object per word or per wrapped line comes back one fragment per line, and the model
// reads a column of words where the page showed prose. A line carrying text that does not end
// in a sentence end (sentenceEnds) and is followed by another text line is joined to it with a
// single space; a line ending a sentence keeps its break, and a paragraph — a text line followed
// by a blank line — survives. A blank (whitespace-only) line is never a join source and is
// kept verbatim, so a page whose text begins with a newline still renders its marker, the blank,
// then the text, exactly as before. The whitespace either side of a joined break collapses into
// the one space; a line that keeps its break is written as it came.
func joinWrappedLines(pageText string) string {
	lines := strings.Split(pageText, "\n")
	if len(lines) < 2 {
		return pageText
	}
	var b strings.Builder
	b.Grow(len(pageText))
	joined := false
	for i, line := range lines {
		if joined {
			line = strings.TrimLeft(line, " \t")
		}
		joined = i < len(lines)-1 && joinsWithNext(line, lines[i+1])
		if joined {
			b.WriteString(strings.TrimRight(line, " \t\r"))
			b.WriteByte(' ')
			continue
		}
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// joinsWithNext says whether line continues onto next: both carry text, and line does not end a
// sentence.
func joinsWithNext(line, next string) bool {
	text := strings.TrimRight(line, " \t\r")
	if text == "" || strings.TrimSpace(next) == "" {
		return false
	}
	return !strings.ContainsRune(sentenceEnds, rune(text[len(text)-1]))
}

// keep commits a page that belongs in the output, together with any blank run before it — those
// blanks now sit BETWEEN content, so they are the document's own.
func (a *pageAccumulator) keep(number int, block string) {
	a.commitPending()
	a.blocks = append(a.blocks, block)
	a.textBytes += len(block)
	a.lastKept = number
}

// hold parks a blank page. It reaches the output only if the run it belongs to ends before
// pdfPhantomRun.
func (a *pageAccumulator) hold(number int, block string) {
	a.pending = append(a.pending, block)
	a.pendingLast = number
	a.phantom++
}

// commitPending moves a blank run that ended into the output. The blocks are copied out, so
// reusing pending's storage afterwards cannot disturb them.
func (a *pageAccumulator) commitPending() {
	if len(a.pending) == 0 {
		return
	}
	a.blocks = append(a.blocks, a.pending...)
	for _, held := range a.pending {
		a.textBytes += len(held)
	}
	a.lastKept = a.pendingLast
	a.dropPending()
}

// dropPending discards a blank run and resets the count — the phantom tail of a document whose
// /Count outran its pages, which is exactly what must NOT reach the model as ninety-nine empty
// page markers.
func (a *pageAccumulator) dropPending() {
	a.pending = a.pending[:0]
	a.phantom = 0
}

// errExtractionBudget is what a budgeted read returns once a bound has tripped. The parser turns
// a read error into an error or a panic of its own; either way failure() and failureFor() word
// the outcome for the model, so this sentinel is never what the model reads.
var errExtractionBudget = errors.New("pdf extraction budget exhausted")

// budgetedReaderAt is the only door the parser has onto the document's bytes, and it bounds what
// the parser may do there: every read is charged against pdfMaxReads and checked against the
// caller's context. It is the bound on the parser's WALK, which no page-level cap can be: the
// walk is driven by the document's own references, a /Kids array pointing at its own node loops
// without ever returning a page, and the parser keeps no value cache, so each turn of that loop
// is a fresh read. Bounding the reads makes the loop finite without forking the parser.
//
// It is not safe for concurrent use and does not need to be: one ExtractPDF call drives one
// parser on one goroutine.
type budgetedReaderAt struct {
	ctx       context.Context
	source    io.ReaderAt
	reads     int
	exhausted bool
}

// ReadAt charges one read against the budget and refuses every read once a bound has tripped.
// The refusal is sticky: past the bound the parser must not make progress on any other path
// either, because the walk that exhausted the budget is the walk still running.
func (b *budgetedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.exhausted || b.reads >= pdfMaxReads {
		b.exhausted = true
		return 0, errExtractionBudget
	}
	b.reads++
	return b.source.ReadAt(p, off)
}

// failure reports the document-level failure a tripped bound has already decided on, worded for
// the model, or "" while the walk is still within its bounds. Cancellation is checked first: a
// cancelled Turn is why the reads stopped, whether or not the budget also ran out.
func (b *budgetedReaderAt) failure() string {
	switch {
	case b.ctx.Err() != nil:
		return fmt.Sprintf(pdfUnreadableFormat, pdfCancelledCause)
	case b.exhausted:
		return fmt.Sprintf(pdfUnreadableFormat, pdfBudgetCause)
	}
	return ""
}

// failureFor words a failure the parser reported — an error or a recovered panic value — using
// the bound's own sentence when a bound is what actually stopped it. The parser's complaint
// about a malformed object is true but useless when the real answer is that the walk was cut
// off, so the bound speaks for it.
func (b *budgetedReaderAt) failureFor(reported any) string {
	if failure := b.failure(); failure != "" {
		return failure
	}
	return fmt.Sprintf(pdfUnreadableFormat, reported)
}

// withoutStreamBodies returns the spans of data that lie outside every stream body, in order, in
// one pass over the streams locatePDFStreams finds. Each body is skipped from its `stream`
// keyword to its skipEnd — the SHORTER of its declared /Length and its first literal
// `endstream` — so neither a /Length that runs past the object nor a decoy `endstream` can make
// the skip longer than the old literal match was. A `stream` keyword with no `endstream` after it
// skips nothing: its bytes stay in the result, so a truncated stream cannot become a place to
// hide the trailer from a guard that reads these spans. A keyword inside a body already skipped
// opens no second skip.
func withoutStreamBodies(data []byte) [][]byte {
	streams := locatePDFStreams(data)

	spans := make([][]byte, 0, len(streams)+1)
	cursor := 0
	for _, stream := range streams {
		if stream.keyword < cursor || stream.skipEnd == stream.keyword {
			continue
		}
		spans = append(spans, data[cursor:stream.keyword])
		cursor = stream.skipEnd
	}
	return append(spans, data[cursor:])
}

// refuseAbsurdObjectCount returns the model-facing refusal for a document declaring more objects
// than its own bytes could hold, or "" when every declared count is possible. The bound is the
// loosest sound one — one byte per object, where the smallest real object costs about twenty —
// so it refuses impossible documents and nothing a real producer emits.
//
// Every /Size in the RAW bytes is read — the trailer's and any xref-stream dictionary's alike,
// because both size the cross-reference table the parser allocates before it reads a single
// object (read.go:233,392). The scan is over the bytes rather than the parsed document for the
// same reason: by the time the parser could report the number, it has already allocated for it.
// It is applied only OUTSIDE stream bodies (see withoutStreamBodies): a dictionary keying the
// xref table always precedes its stream keyword, so every /Size that sizes an allocation stays
// covered, while compressed or otherwise arbitrary stream CONTENT — an image, an embedded font,
// another PDF — cannot spell one by accident.
func refuseAbsurdObjectCount(data []byte) string {
	for _, declared := range declaredIntegers(data, "Size") {
		// A count too long for uint64 is refused on its digits alone: a file that cannot hold
		// 2^64 objects cannot hold more than that either.
		if exceedsBound(declared, int64(len(data))) {
			return fmt.Sprintf(pdfUnreadableFormat, fmt.Sprintf(pdfAbsurdSizeFormat, declared, len(data)))
		}
	}
	return ""
}

// preflightPDF returns the cause of a refusal when the document's raw bytes name an allocation
// or an inflation the parser must not be allowed to make, or "" when it may run. It sits between
// refuseAbsurdObjectCount and pdf.NewReader and bounds the three numbers that guard does not: how
// far the streams the text path decodes inflate (read.go applyFilter inflates a stream with no
// ceiling but the stream's own), how wide a predictor row is (a pngUpReader allocates two buffers
// of /Columns bytes each), and how wide a cross-reference row is (read.go readXrefStreamData
// allocates the sum of /W). Every bound is read the way the parser's lexer would read it, so a
// comment or a NUL between a key and its number hides nothing.
//
// The inflate budget is ONE budget for the whole document, charged only to the streams the text
// path decodes — see chargesInflation for the rule — so a document that embeds a large image or
// font goes uncharged for it while a bomb wired as page content is caught before the parser
// materialises it. Each stream is charged over the bytes the parser reads for it and through the
// filter chain the parser applies to it (see locatePDFStreams), never over a literal `endstream`
// or a count of filter names, both of which the document's own bytes can forge. A stream that
// fails to inflate is skipped, not refused: the parser will report it. A page dictionary
// compressed inside an object stream hides its /Contents reference from this raw scan; that gap
// is known and bounded — an object stream is itself charged.
func preflightPDF(ctx context.Context, data []byte) string {
	for _, columns := range declaredIntegers(data, "Columns") {
		if exceedsBound(columns, pdfMaxPredictorColumns) {
			return fmt.Sprintf(pdfPredictorCause, columns)
		}
	}

	decoded := decodedReferences(data, indexPDFObjects(data))
	remaining := int64(pdfMaxInflatedBytes)
	for _, stream := range locatePDFStreams(data) {
		if !stream.owned {
			continue
		}
		if cause := refuseAbsurdXrefWidths(stream.dictionary); cause != "" {
			return cause
		}
		if !chargesInflation(stream, decoded) {
			continue
		}
		inflated := largestInflation(ctx, stream, remaining)
		if ctx.Err() != nil {
			return pdfCancelledCause
		}
		if inflated > remaining {
			return fmt.Sprintf(pdfInflatedCause, pdfMaxInflatedBytes>>20)
		}
		remaining -= inflated
	}
	return ""
}

// largestInflation charges one stream: the most any of its candidate decode chains inflates its
// body to, measured no further than limit+1 bytes (see inflatedSize). A stream whose chain was
// read carries exactly one candidate; one whose /Filter could not be read carries the chains
// pdfUnreadChains names, and is charged for the worst of them.
func largestInflation(ctx context.Context, stream pdfStream, limit int64) int64 {
	var largest int64
	for _, chain := range stream.chains {
		largest = max(largest, inflatedSize(ctx, stream.body, chain, limit))
		if largest > limit || ctx.Err() != nil {
			break
		}
	}
	return largest
}

// pdfObject is one `N G obj` header the raw scan found outside every stream body: its number and
// the bytes that follow the keyword — the whole dictionary for a stream object, the leading value
// for any other. It is what decodedReferences resolves an array object through.
type pdfObject struct {
	number int64
	value  []byte
}

// indexPDFObjects lists every object header outside the document's stream bodies, in file
// order, each with the bytes up to the next header or the end of its span.
func indexPDFObjects(data []byte) []pdfObject {
	var objects []pdfObject
	for _, span := range withoutStreamBodies(data) {
		headers := pdfObjectHeaders(span)
		for index, header := range headers {
			valueEnd := len(span)
			if index+1 < len(headers) {
				valueEnd = headers[index+1].start
			}
			objects = append(objects, pdfObject{number: header.number, value: span[header.end:valueEnd]})
		}
	}
	return objects
}

const (
	// pdfStreamKeyword and pdfEndStreamKeyword are the two keywords that frame a stream body.
	pdfStreamKeyword    = "stream"
	pdfEndStreamKeyword = "endstream"

	// pdfFlateDecode and pdfASCII85Decode are the two filters the parser decodes (read.go
	// applyFilter); any other name panics before a byte is read.
	pdfFlateDecode   = "FlateDecode"
	pdfASCII85Decode = "ASCII85Decode"

	// pdfMaxOwnerCandidates bounds how many `N G obj` headers before one stream keyword are tried
	// as the one whose dictionary introduces it. The nearest header is the owner in every real
	// document; the ones before it are tried only because a header-shaped string inside the
	// dictionary itself can sit between the two, and the bound keeps a file full of such strings
	// from making the scan quadratic.
	pdfMaxOwnerCandidates = 8

	// pdfMaxNesting bounds how deeply the dictionary reader descends into nested dictionaries
	// and arrays, so a document cannot size the preflight's stack. No real stream dictionary
	// nests more than a handful of levels.
	pdfMaxNesting = 64
)

// pdfUnreadChains are the decode chains a stream is charged for when its /Filter could not be
// read — an indirect reference, or a dictionary the reader below could not follow: plain
// FlateDecode, and FlateDecode behind ASCII85Decode. Between them they cover every chain real
// producers write, and the worst of them is what the stream is charged.
var pdfUnreadChains = [][]string{{pdfFlateDecode}, {pdfASCII85Decode, pdfFlateDecode}}

// pdfStream is one `stream` keyword the locator found. keyword is the keyword's index; skipEnd
// is where the span the dictionary scans skip ends (== keyword when nothing is skipped). A stream
// is owned when an `N G obj` header before it introduces it: only then does it carry the owner's
// number, the dictionary bytes between `obj` and the keyword, the body the parser may decode, and
// the candidate decode chains (none when the parser inflates nothing). An unowned stream is
// unreachable — the cross-reference table addresses objects by their headers — and is never
// charged.
type pdfStream struct {
	keyword    int
	skipEnd    int
	owned      bool
	number     int64
	dictionary []byte
	body       []byte
	chains     [][]string
}

// pdfStreamSite is one `stream` keyword: the keyword's index and the index its body starts at.
type pdfStreamSite struct {
	keyword, bodyStart int
}

// locatePDFStreams finds every stream the parser could decode and what it would decode of each.
// The parser reads a stream by its declared /Length and never looks for `endstream` (read.go
// Value.Reader), and it reaches an object through the cross-reference table, which can point at
// any header — one inside another stream's body included. So every `stream` keyword in the file
// is located, nested ones too, and each is described from its own dictionary:
//
//   - body: a direct integer /Length is exactly what the parser reads, clamped to the end of the
//     file. Any other /Length — an `N G R` reference, a missing or malformed one — is charged
//     through the end of the file. A reference is never looked up in the raw bytes: the parser
//     resolves it through the cross-reference table, which a hostile file can point at a
//     definition the raw scan never sees (inside another stream's body, or compressed in an
//     object stream), while a raw lookup would read whatever header-shaped bytes a decoy planted.
//     Charging through the end costs a real stream nothing — inflation stops at the stream's own
//     end-of-data marker — and bounds every length the parser could resolve.
//   - chains: the ordered /Filter name or array, decoded in order (see pdfDecodeChains).
//
// skipEnd is the shorter of the declared body and the first literal `endstream` after it (see
// withoutStreamBodies). The streams come back in file order.
func locatePDFStreams(data []byte) []pdfStream {
	sites := pdfStreamSites(data)
	ends := pdfKeywordIndexes(data, pdfEndStreamKeyword)

	streams := make([]pdfStream, 0, len(sites))
	windowStart, nextEnd := 0, 0
	for _, site := range sites {
		for nextEnd < len(ends) && ends[nextEnd] < site.bodyStart {
			nextEnd++
		}
		skipEnd := site.keyword
		if nextEnd < len(ends) {
			skipEnd = ends[nextEnd] + len(pdfEndStreamKeyword)
		}

		stream, declaredEnd := describePDFStream(data, windowStart, site)
		if declaredEnd >= 0 && skipEnd > site.keyword {
			skipEnd = min(skipEnd, declaredEnd)
		}
		stream.skipEnd = skipEnd
		streams = append(streams, stream)
		windowStart = site.bodyStart
	}
	return streams
}

// describePDFStream finds the header that owns one stream keyword and reads the stream from its
// dictionary. The owner is searched for between windowStart — the previous keyword's body start —
// and the keyword, nearest header first: the nearest header whose dictionary the reader follows
// right up to this keyword is the owner. When headers exist but none of them reads — a
// dictionary the reader cannot follow — the nearest one is taken as the owner and the stream is
// charged for pdfUnreadChains through the end of the file. It also returns where a direct
// /Length ends the body, or -1 when the length is not a direct integer.
func describePDFStream(data []byte, windowStart int, site pdfStreamSite) (pdfStream, int) {
	stream := pdfStream{keyword: site.keyword}
	headers := pdfObjectHeaders(data[windowStart:site.keyword])
	if len(headers) == 0 {
		return stream, -1
	}

	// The reader sees the keyword and nothing after it, so no candidate can read past it.
	span := data[:site.keyword+len(pdfStreamKeyword)]
	for tried := 0; tried < min(len(headers), pdfMaxOwnerCandidates); tried++ {
		header := headers[len(headers)-1-tried]
		dictionary, ok := readPDFStreamDictionary(span, windowStart+header.end, site.keyword)
		if !ok {
			continue
		}
		stream.owned, stream.number = true, header.number
		stream.dictionary = data[windowStart+header.end : site.keyword]
		filter, hasFilter := dictionary["Filter"]
		stream.chains = pdfDecodeChains(filter, hasFilter)
		bodyEnd := len(data)
		declaredEnd := -1
		if length, ok := directPDFLength(dictionary); ok {
			bodyEnd = site.bodyStart + int(min(length, int64(len(data)-site.bodyStart)))
			declaredEnd = bodyEnd
		}
		stream.body = data[site.bodyStart:bodyEnd]
		return stream, declaredEnd
	}

	nearest := headers[len(headers)-1]
	stream.owned, stream.number = true, nearest.number
	stream.dictionary = data[windowStart+nearest.end : site.keyword]
	stream.chains = pdfUnreadChains
	stream.body = data[site.bodyStart:]
	return stream, -1
}

// directPDFLength returns the stream dictionary's /Length when it is a direct, non-negative
// integer — the only form whose value the raw bytes state for certain.
func directPDFLength(dictionary map[string]pdfValue) (int64, bool) {
	length, ok := dictionary["Length"]
	if !ok || length.kind != pdfValueInteger {
		return 0, false
	}
	value, err := strconv.ParseInt(length.text, 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// pdfDecodeChains returns the decode chains a stream's /Filter value makes the parser apply, in
// the order it applies them (read.go Value.Reader): a name is a one-filter chain, an array is its
// names in order. It returns nil — nothing to charge — when there is no filter, when the chain
// contains no FlateDecode (only FlateDecode inflates), or when the parser would refuse the chain
// before reading a byte: a filter it does not know, an array member that is not a name, a value
// of any other kind. A reference, as the value or as an array member, cannot be read from the
// raw bytes and is charged as pdfUnreadChains.
func pdfDecodeChains(filter pdfValue, present bool) [][]string {
	if !present {
		return nil
	}
	switch filter.kind {
	case pdfValueName:
		return inflatingChain([]string{filter.text})
	case pdfValueReference:
		return pdfUnreadChains
	case pdfValueArray:
		names := make([]string, 0, len(filter.members))
		for _, member := range filter.members {
			switch member.kind {
			case pdfValueName:
				names = append(names, member.text)
			case pdfValueReference:
				return pdfUnreadChains
			default:
				return nil
			}
		}
		return inflatingChain(names)
	}
	return nil
}

// inflatingChain returns names as the stream's single candidate chain when the parser decodes
// every filter in it and at least one of them inflates, or nil otherwise.
func inflatingChain(names []string) [][]string {
	inflates := false
	for _, name := range names {
		switch name {
		case pdfFlateDecode:
			inflates = true
		case pdfASCII85Decode:
		default:
			return nil
		}
	}
	if !inflates {
		return nil
	}
	return [][]string{names}
}

// pdfStreamSites returns every `stream` keyword in data, in order. A keyword is matched as the
// lexer reads one: a token of its own — the byte before it is not a regular byte, which excludes
// the tail of `endstream` — followed by the end-of-line the parser requires, CR LF, LF or a lone
// CR (lex.go readDict). The body starts just past that end-of-line.
func pdfStreamSites(data []byte) []pdfStreamSite {
	var sites []pdfStreamSite
	for _, at := range pdfKeywordIndexes(data, pdfStreamKeyword) {
		if at > 0 && isPDFRegular(data[at-1]) {
			continue
		}
		bodyStart := at + len(pdfStreamKeyword)
		switch {
		case bytes.HasPrefix(data[bodyStart:], []byte("\r\n")):
			bodyStart += 2
		case bodyStart < len(data) && (data[bodyStart] == '\r' || data[bodyStart] == '\n'):
			bodyStart++
		default:
			continue
		}
		sites = append(sites, pdfStreamSite{keyword: at, bodyStart: bodyStart})
	}
	return sites
}

// pdfKeywordIndexes returns the index of every occurrence of keyword in data, in order.
func pdfKeywordIndexes(data []byte, keyword string) []int {
	var indexes []int
	for from := 0; ; {
		at := bytes.Index(data[from:], []byte(keyword))
		if at < 0 {
			return indexes
		}
		indexes = append(indexes, from+at)
		from += at + len(keyword)
	}
}

// pdfASCII85Cleaner is the parser's alphaReader (ascii85.go) reproduced byte for byte, so the
// ASCII85 layer the preflight measures decodes exactly what the parser's does: every byte outside
// `!`…`u` reads as a NUL, which the decoder skips — `z` included, which the parser drops rather
// than expands — and a `>` after a `~` ends the chunk being read, not the stream.
type pdfASCII85Cleaner struct {
	source io.Reader
}

// Read reads one chunk from the source and cleans it in place.
func (c pdfASCII85Cleaner) Read(p []byte) (int, error) {
	n, err := c.source.Read(p)
	if err != nil {
		return n, err
	}
	tilde := false
	for i := range n {
		switch {
		case p[i] == '>' && tilde:
			clear(p[i:n])
			return n, nil
		case p[i] == '~':
			tilde = true
			p[i] = 0
		case p[i] < '!' || p[i] > 'u':
			p[i] = 0
		}
	}
	return n, nil
}

// pdfValueKind classifies the values readPDFStreamDictionary distinguishes; every kind the
// preflight never inspects — a string, a number other than an integer, a boolean, null, a nested
// dictionary — reads as pdfValueOther.
type pdfValueKind int

const (
	pdfValueOther pdfValueKind = iota
	pdfValueName
	pdfValueInteger
	pdfValueReference
	pdfValueArray
)

// pdfValue is one value of a stream dictionary: its kind, a name's decoded text or an integer's
// digits, and an array's members.
type pdfValue struct {
	kind    pdfValueKind
	text    string
	members []pdfValue
}

// pdfTokenKind classifies the lexer's tokens. pdfTokenInvalid covers both the end of the span and
// every byte sequence the parser's lexer panics on.
type pdfTokenKind int

const (
	pdfTokenInvalid pdfTokenKind = iota
	pdfTokenDelimiter
	pdfTokenName
	pdfTokenString
	pdfTokenWord
)

// pdfToken is one token: its kind, its text — a delimiter's bytes, a name's decoded text, a
// word's bytes — and the index it starts at.
type pdfToken struct {
	kind  pdfTokenKind
	text  string
	start int
}

// pdfLexer reads a stream dictionary the way the parser's lexer and object reader do (lex.go
// readToken, readObject, readDict), so the /Length and /Filter it reports are the entries the
// parser reads: a comment, a string or a nested dictionary that merely spells `/FlateDecode`
// names no filter, and a key written twice means its last value. Anything the parser would panic
// on fails the read instead.
type pdfLexer struct {
	span  []byte
	at    int
	depth int
}

// readPDFStreamDictionary reads the dictionary that starts at `from` and reports it only when the
// token right after it is the `stream` keyword at keywordAt — i.e. when this is the dictionary
// the parser reads for that stream.
func readPDFStreamDictionary(span []byte, from, keywordAt int) (map[string]pdfValue, bool) {
	lexer := pdfLexer{span: span, at: from}
	if open := lexer.next(); open.kind != pdfTokenDelimiter || open.text != "<<" {
		return nil, false
	}
	dictionary, ok := lexer.readDictionary()
	if !ok {
		return nil, false
	}
	keyword := lexer.next()
	return dictionary, keyword.kind == pdfTokenWord && keyword.text == pdfStreamKeyword && keyword.start == keywordAt
}

// readDictionary reads a dictionary's entries after its `<<`, through its `>>`.
func (l *pdfLexer) readDictionary() (map[string]pdfValue, bool) {
	if l.depth++; l.depth > pdfMaxNesting {
		return nil, false
	}
	defer func() { l.depth-- }()

	dictionary := map[string]pdfValue{}
	for {
		key := l.next()
		if key.kind == pdfTokenDelimiter && key.text == ">>" {
			return dictionary, true
		}
		if key.kind != pdfTokenName {
			return nil, false
		}
		value, ok := l.readValue()
		if !ok {
			return nil, false
		}
		dictionary[key.text] = value
	}
}

// readArray reads an array's members after its `[`, through its `]`.
func (l *pdfLexer) readArray() (pdfValue, bool) {
	if l.depth++; l.depth > pdfMaxNesting {
		return pdfValue{}, false
	}
	defer func() { l.depth-- }()

	array := pdfValue{kind: pdfValueArray}
	for {
		mark := l.at
		if token := l.next(); token.kind == pdfTokenDelimiter && token.text == "]" {
			return array, true
		} else if token.kind == pdfTokenInvalid {
			return pdfValue{}, false
		}
		l.at = mark
		member, ok := l.readValue()
		if !ok {
			return pdfValue{}, false
		}
		array.members = append(array.members, member)
	}
}

// readValue reads one value. A stray `>>` reads as null, as the parser's readObject has it.
func (l *pdfLexer) readValue() (pdfValue, bool) {
	token := l.next()
	switch token.kind {
	case pdfTokenName:
		return pdfValue{kind: pdfValueName, text: token.text}, true
	case pdfTokenString:
		return pdfValue{}, true
	case pdfTokenWord:
		return l.readWord(token)
	case pdfTokenDelimiter:
		switch token.text {
		case "<<":
			_, ok := l.readDictionary()
			return pdfValue{}, ok
		case "[":
			return l.readArray()
		case ">>":
			return pdfValue{}, true
		}
	}
	return pdfValue{}, false
}

// readWord reads a value that starts with a word token: null, a boolean, a real, an integer, or
// an `N G R` reference — an integer that fits an object number followed by one that fits a
// generation and the `R` keyword. An `N G obj` definition nested inside a value, and any other
// keyword, fail the read.
func (l *pdfLexer) readWord(token pdfToken) (pdfValue, bool) {
	switch {
	case token.text == "null" || token.text == "true" || token.text == "false" || isPDFReal(token.text):
		return pdfValue{}, true
	case !isPDFInteger(token.text):
		return pdfValue{}, false
	}
	number, err := strconv.ParseInt(token.text, 10, 64)
	if err != nil {
		return pdfValue{}, false
	}
	integer := pdfValue{kind: pdfValueInteger, text: token.text}
	if number < 0 || number > math.MaxUint32 {
		return integer, true
	}

	mark := l.at
	generation := l.next()
	if generation.kind == pdfTokenWord && isPDFInteger(generation.text) {
		value, err := strconv.ParseInt(generation.text, 10, 64)
		if err == nil && value >= 0 && value <= math.MaxUint16 {
			switch keyword := l.next(); {
			case keyword.kind == pdfTokenWord && keyword.text == "R":
				return pdfValue{kind: pdfValueReference, text: token.text}, true
			case keyword.kind == pdfTokenWord && keyword.text == "obj":
				return pdfValue{}, false
			}
		}
	}
	l.at = mark
	return integer, true
}

// next reads one token after skipping whitespace and comments.
func (l *pdfLexer) next() pdfToken {
	l.at = skipPDFSpace(l.span, l.at)
	start := l.at
	if start >= len(l.span) {
		return pdfToken{start: start}
	}
	switch c := l.span[start]; {
	case (c == '<' || c == '>') && start+1 < len(l.span) && l.span[start+1] == c:
		l.at += 2
		return pdfToken{kind: pdfTokenDelimiter, text: string(l.span[start:l.at]), start: start}
	case c == '[' || c == ']' || c == '{' || c == '}':
		l.at++
		return pdfToken{kind: pdfTokenDelimiter, text: string(c), start: start}
	case c == '<':
		return l.readHexString(start)
	case c == '(':
		return l.readLiteralString(start)
	case c == '/':
		name, end := readPDFName(l.span, start+1)
		if end < len(l.span) && l.span[end] == '#' {
			return pdfToken{start: start}
		}
		l.at = end
		return pdfToken{kind: pdfTokenName, text: name, start: start}
	case isPDFDelimiter(c):
		return pdfToken{start: start}
	}
	end := start
	for end < len(l.span) && isPDFRegular(l.span[end]) {
		end++
	}
	l.at = end
	return pdfToken{kind: pdfTokenWord, text: string(l.span[start:end]), start: start}
}

// readHexString reads a `<…>` string: hex digits in pairs, whitespace anywhere between them.
func (l *pdfLexer) readHexString(start int) pdfToken {
	digits := 0
	for at := start + 1; at < len(l.span); at++ {
		c := l.span[at]
		switch {
		case c == '>':
			if digits%2 != 0 {
				return pdfToken{start: start}
			}
			l.at = at + 1
			return pdfToken{kind: pdfTokenString, start: start}
		case isPDFSpace(c):
		case isPDFHexDigit(c):
			digits++
		default:
			return pdfToken{start: start}
		}
	}
	return pdfToken{start: start}
}

// readLiteralString reads a `(…)` string: balanced parentheses, and the escapes the parser
// accepts — any other escape, or an octal one above 255, is a read the parser panics on.
func (l *pdfLexer) readLiteralString(start int) pdfToken {
	depth := 1
	for at := start + 1; at < len(l.span); at++ {
		switch l.span[at] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				l.at = at + 1
				return pdfToken{kind: pdfTokenString, start: start}
			}
		case '\\':
			next, ok := skipPDFEscape(l.span, at+1)
			if !ok {
				return pdfToken{start: start}
			}
			at = next - 1
		}
	}
	return pdfToken{start: start}
}

// skipPDFEscape returns the index just past the escape whose first byte (after the backslash) is
// at `at`, and false for an escape the parser refuses.
func skipPDFEscape(span []byte, at int) (int, bool) {
	if at >= len(span) {
		return at, false
	}
	switch c := span[at]; {
	case strings.IndexByte("nrbtf()\\\n", c) >= 0:
		return at + 1, true
	case c == '\r':
		if at+1 < len(span) && span[at+1] == '\n' {
			return at + 2, true
		}
		return at + 1, true
	case c >= '0' && c <= '7':
		value, end := int(c-'0'), at+1
		for ; end < len(span) && end < at+3 && span[end] >= '0' && span[end] <= '7'; end++ {
			value = value*8 + int(span[end]-'0')
		}
		return end, value <= math.MaxUint8
	}
	return at, false
}

// isPDFInteger and isPDFReal are the lexer's number shapes (lex.go isInteger, isReal): an
// optional sign, then digits — with exactly one dot for a real.
func isPDFInteger(word string) bool {
	word = withoutPDFSign(word)
	return word != "" && strings.Trim(word, "0123456789") == ""
}

func isPDFReal(word string) bool {
	word = withoutPDFSign(word)
	return strings.Count(word, ".") == 1 && strings.Trim(word, "0123456789.") == ""
}

// withoutPDFSign drops the one leading sign a number may carry.
func withoutPDFSign(word string) string {
	if word != "" && (word[0] == '+' || word[0] == '-') {
		return word[1:]
	}
	return word
}

func isPDFHexDigit(b byte) bool {
	return isPDFDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

// pdfObjectHeader locates one `N G obj` keyword in a span: the object number, the index of its
// first digit and the index just past `obj`.
type pdfObjectHeader struct {
	number     int64
	start, end int
}

// pdfObjectHeaders finds every `N G obj` header in span, in order. The keyword is matched as a
// token — `endobj` does not end in one — and the two integers before it must be whole tokens
// too, so `19 0 obj` is object nineteen and never nine.
func pdfObjectHeaders(span []byte) []pdfObjectHeader {
	const keyword = "obj"

	var headers []pdfObjectHeader
	for from := 0; ; {
		at := bytes.Index(span[from:], []byte(keyword))
		if at < 0 {
			return headers
		}
		at += from
		from = at + len(keyword)
		if from < len(span) && isPDFRegular(span[from]) {
			continue
		}
		generationEnd := skipPDFSpaceBackwards(span, at)
		generationStart := skipPDFDigitsBackwards(span, generationEnd)
		numberEnd := skipPDFSpaceBackwards(span, generationStart)
		numberStart := skipPDFDigitsBackwards(span, numberEnd)
		if generationStart == generationEnd || numberStart == numberEnd ||
			generationStart == numberEnd || numberStart > 0 && isPDFRegular(span[numberStart-1]) {
			continue
		}
		number, err := strconv.ParseInt(string(span[numberStart:numberEnd]), 10, 64)
		if err != nil {
			continue
		}
		headers = append(headers, pdfObjectHeader{number: number, start: numberStart, end: from})
	}
}

// decodedReferences collects the numbers of every object a /Contents or /ToUnicode key names —
// the two ways the text path reaches a stream by reference (page.go GetPlainText, readCmap).
// A direct reference, an inline array of references and a reference to an array object are all
// read; the array object is resolved one level, because that is how far the parser looks.
func decodedReferences(data []byte, objects []pdfObject) map[int64]bool {
	byNumber := make(map[int64]pdfObject, len(objects))
	for _, object := range objects {
		byNumber[object.number] = object
	}

	named := map[int64]bool{}
	for _, span := range withoutStreamBodies(data) {
		for _, key := range []string{"Contents", "ToUnicode"} {
			for _, site := range pdfNameSites(span, key) {
				for _, number := range pdfReferencesAt(span, site) {
					named[number] = true
					// A stream object's value is its dictionary, which names no reference at
					// its start, so only an array object resolves to members here.
					if object, ok := byNumber[number]; ok {
						for _, member := range pdfReferencesAt(object.value, 0) {
							named[member] = true
						}
					}
				}
			}
		}
	}
	return named
}

// chargesInflation decides whether a stream's inflation counts against the document's budget:
// only a stream with an inflating decode chain (see pdfDecodeChains) that the text path will
// decode does. An untyped stream — page content is
// untyped — is always charged. A stream whose dictionary carries a /Type or /Subtype is charged
// when its type is one the parser decodes (/XRef, /ObjStm, /CMap) or a /Contents or /ToUnicode
// reference names it, and skipped otherwise: an image or form XObject, an embedded font, an
// attached file. Type and reference win over any label, so a /Subtype /Image on an xref stream
// exempts nothing.
func chargesInflation(stream pdfStream, decoded map[int64]bool) bool {
	if len(stream.chains) == 0 {
		return false
	}
	if len(pdfNameSites(stream.dictionary, "Type")) == 0 && len(pdfNameSites(stream.dictionary, "Subtype")) == 0 {
		return true
	}
	switch pdfNameValue(stream.dictionary, "Type") {
	case "XRef", "ObjStm", "CMap":
		return true
	}
	return decoded[stream.number]
}

// refuseAbsurdXrefWidths returns the cause for a cross-reference stream dictionary whose /W
// array sizes a row the parser must not allocate, or "". Only a dictionary naming /Type /XRef
// is read: a CIDFont's glyph-width /W is a different key in a different dictionary and is never
// read as one. Each field is at most eight bytes — decodeInt reads a big-endian integer, and no
// integer is wider — and a row at most twenty-four.
func refuseAbsurdXrefWidths(dictionary []byte) string {
	if pdfNameValue(dictionary, "Type") != "XRef" {
		return ""
	}
	for _, site := range pdfNameSites(dictionary, "W") {
		widths, ok := pdfIntegerArrayAt(dictionary, site)
		if !ok {
			continue
		}
		var row int64
		for _, width := range widths {
			if exceedsBound(width, pdfMaxXrefFieldWidth) {
				return fmt.Sprintf(pdfXrefWidthCause, strings.Join(widths, " "))
			}
			field, _ := strconv.ParseInt(width, 10, 64)
			row += field
		}
		if row > pdfMaxXrefRowWidth {
			return fmt.Sprintf(pdfXrefWidthCause, strings.Join(widths, " "))
		}
	}
	return ""
}

// inflatedSize reports how many bytes body decodes to through chain — FlateDecode and
// ASCII85Decode filters, applied in order as the parser applies them — reading no further than
// limit+1 bytes so a bomb is never materialised: a result above limit means "more than the
// budget", never the exact count. A FlateDecode layer whose zlib header does not read charges
// nothing: the parser panics on it before a byte is decoded. An inflate error past the header —
// the bytes ran out early or went bad — ends the count where it stands; the parser will report
// the error, and what inflated before it is still charged. Cancellation is read between chunks
// and reported through the context, which the caller consults.
func inflatedSize(ctx context.Context, body []byte, chain []string, limit int64) int64 {
	var reader io.Reader = bytes.NewReader(body)
	for _, filter := range chain {
		if filter == pdfASCII85Decode {
			reader = ascii85.NewDecoder(pdfASCII85Cleaner{source: reader})
			continue
		}
		inflater, err := zlib.NewReader(reader)
		if err != nil {
			return 0
		}
		reader = inflater
	}

	var total int64
	for total <= limit && ctx.Err() == nil {
		count, err := io.CopyN(io.Discard, reader, pdfInflateChunk)
		total += count
		if err != nil {
			break
		}
	}
	return total
}

// declaredIntegers returns, as digit strings, the value written after every `/key` outside the
// document's stream bodies, read the way the parser's lexer reads it: whitespace — NUL, TAB, LF,
// FF, CR and SP — and % comments between the key and its digits are skipped. A key followed by
// anything but digits declares no integer and is not listed; the parser will refuse it as a type
// error without allocating for it.
func declaredIntegers(data []byte, key string) []string {
	var values []string
	for _, span := range withoutStreamBodies(data) {
		for _, site := range pdfNameSites(span, key) {
			if digits, _ := readPDFDigits(span, site); digits != "" {
				values = append(values, digits)
			}
		}
	}
	return values
}

// exceedsBound reports whether a digit string names an integer above bound — including one too
// long for int64, which is above every bound this file sets.
func exceedsBound(digits string, bound int64) bool {
	value, err := strconv.ParseInt(digits, 10, 64)
	return err != nil || value > bound
}

// pdfNameSites returns the index just past every `/key` name token in span. A name is read the
// way the lexer reads it — its regular bytes up to the next whitespace or delimiter, with `#xx`
// hex escapes decoded — so `/Size` is found as `/Siz#65` and never inside `/Sizes`.
func pdfNameSites(span []byte, key string) []int {
	var sites []int
	for from := 0; ; {
		at := bytes.IndexByte(span[from:], '/')
		if at < 0 {
			return sites
		}
		name, end := readPDFName(span, from+at+1)
		if name == key {
			sites = append(sites, end)
		}
		from = end
	}
}

// pdfNameValue returns the name written as the value of the first `/key` in span — "XRef" for
// `/Type /XRef` — or "" when the key is absent or its value is not a name.
func pdfNameValue(span []byte, key string) string {
	sites := pdfNameSites(span, key)
	if len(sites) == 0 {
		return ""
	}
	at := skipPDFSpace(span, sites[0])
	if at >= len(span) || span[at] != '/' {
		return ""
	}
	value, _ := readPDFName(span, at+1)
	return value
}

// readPDFName decodes the name whose first byte (after its slash) is at `at`, returning it and
// the index just past it. A malformed `#` escape ends the name where it stands.
func readPDFName(span []byte, at int) (string, int) {
	var name []byte
	for at < len(span) && isPDFRegular(span[at]) {
		if span[at] != '#' {
			name = append(name, span[at])
			at++
			continue
		}
		if at+2 >= len(span) {
			break
		}
		escaped, err := hex.DecodeString(string(span[at+1 : at+3]))
		if err != nil {
			break
		}
		name = append(name, escaped...)
		at += 3
	}
	return string(name), at
}

// pdfReferencesAt reads the object references written at `at`: one `N G R`, or an inline array
// of them. It returns nil when what is written there is neither — an annotation's /Contents is
// a string, and a string names no object.
func pdfReferencesAt(span []byte, at int) []int64 {
	at = skipPDFSpace(span, at)
	if at < len(span) && span[at] == '[' {
		var members []int64
		at++
		for {
			number, next, ok := readPDFReference(span, at)
			if !ok {
				return members
			}
			members = append(members, number)
			at = next
		}
	}
	number, _, ok := readPDFReference(span, at)
	if !ok {
		return nil
	}
	return []int64{number}
}

// readPDFReference reads one `N G R` at `at`, returning the object number, the index just past
// the `R`, and whether a reference was there at all.
func readPDFReference(span []byte, at int) (int64, int, bool) {
	number, next := readPDFDigits(span, at)
	generation, next := readPDFDigits(span, next)
	next = skipPDFSpace(span, next)
	if number == "" || generation == "" || next >= len(span) || span[next] != 'R' {
		return 0, at, false
	}
	value, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, at, false
	}
	return value, next + 1, true
}

// pdfIntegerArrayAt reads an array of integers written at `at` — `[1 2 1]` — as digit strings,
// and reports false when what is written there is not one.
func pdfIntegerArrayAt(span []byte, at int) ([]string, bool) {
	at = skipPDFSpace(span, at)
	if at >= len(span) || span[at] != '[' {
		return nil, false
	}
	at++
	var members []string
	for {
		digits, next := readPDFDigits(span, at)
		if digits == "" {
			break
		}
		members = append(members, digits)
		at = next
	}
	at = skipPDFSpace(span, at)
	if at >= len(span) || span[at] != ']' {
		return nil, false
	}
	return members, true
}

// readPDFDigits returns the run of decimal digits that follows `at` once whitespace and comments
// are skipped, and the index just past it; "" when no digit is there.
func readPDFDigits(span []byte, at int) (string, int) {
	at = skipPDFSpace(span, at)
	end := at
	for end < len(span) && isPDFDigit(span[end]) {
		end++
	}
	return string(span[at:end]), end
}

// skipPDFSpace returns the index of the first byte at or after `at` that is neither whitespace
// nor part of a % comment — exactly what the parser's lexer skips before every token (lex.go).
func skipPDFSpace(span []byte, at int) int {
	for at < len(span) {
		switch {
		case isPDFSpace(span[at]):
			at++
		case span[at] == '%':
			for at < len(span) && span[at] != '\r' && span[at] != '\n' {
				at++
			}
		default:
			return at
		}
	}
	return at
}

// skipPDFSpaceBackwards returns the index just past the last non-whitespace byte before `at`.
func skipPDFSpaceBackwards(span []byte, at int) int {
	for at > 0 && isPDFSpace(span[at-1]) {
		at--
	}
	return at
}

// skipPDFDigitsBackwards returns the index of the first digit of the run that ends at `at`.
func skipPDFDigitsBackwards(span []byte, at int) int {
	for at > 0 && isPDFDigit(span[at-1]) {
		at--
	}
	return at
}

// isPDFSpace, isPDFDelimiter, isPDFRegular and isPDFDigit are the lexer's byte classes
// (lex.go isSpace, isDelim): a name or keyword token is a run of regular bytes.
func isPDFSpace(b byte) bool {
	switch b {
	case '\x00', '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

func isPDFDelimiter(b byte) bool {
	switch b {
	case '<', '>', '(', ')', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isPDFRegular(b byte) bool {
	return !isPDFSpace(b) && !isPDFDelimiter(b)
}

func isPDFDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// PDFAnnotation is the parenthetical every header quotes for an extracted document: the format,
// the page count, and the standing fact that what follows is extracted text rather than the
// file's own bytes. It is returned WITHOUT surrounding parentheses so each header punctuates it
// in its own idiom.
//
// One page reads "1 page" and every other count reads "N pages" — zero included, which is the
// plural because a header is read verbatim and "0 pages" is the sentence a reader expects.
// Every caller builds its header from this one function, so two headers can never disagree about
// what the same document is.
func PDFAnnotation(pages int) string {
	if pages == 1 {
		return pdfAnnotationSingular
	}
	return fmt.Sprintf(pdfAnnotationFormat, pages)
}

// pageBlock renders one page as its marker line plus its text. An empty page is its marker
// alone, so joining the blocks with a blank line keeps the "exactly one blank line before the
// next marker" rule true whether or not the page had anything on it.
func pageBlock(number int, text string) string {
	marker := fmt.Sprintf(pdfPageMarkerFormat, number)
	if text == "" {
		return marker
	}
	return marker + "\n" + text
}
