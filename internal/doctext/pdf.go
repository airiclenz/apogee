package doctext

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
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

// pdfStreamBody matches one stream object's body: the `stream` keyword — never the tail of
// `endstream`, which the word boundary excludes — its mandatory end-of-line, and every byte up to
// the nearest `endstream`.
var pdfStreamBody = regexp.MustCompile(`(?s)\bstream\r?\n.*?endstream`)

// withoutStreamBodies returns the spans of data that lie outside every stream body, in order, in
// one pass. A `stream` keyword with no `endstream` after it terminates nothing and opens no span:
// its bytes stay in the result, so a truncated stream cannot become a place to hide the trailer
// from a guard that reads these spans.
func withoutStreamBodies(data []byte) [][]byte {
	bodies := pdfStreamBody.FindAllIndex(data, -1)

	spans := make([][]byte, 0, len(bodies)+1)
	cursor := 0
	for _, body := range bodies {
		spans = append(spans, data[cursor:body[0]])
		cursor = body[1]
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
// far the FlateDecode streams the text path decodes inflate (read.go applyFilter inflates a
// stream with no ceiling but the stream's own), how wide a predictor row is (a pngUpReader
// allocates two buffers of /Columns bytes each), and how wide a cross-reference row is (read.go
// readXrefStreamData allocates the sum of /W). Every bound is read the way the parser's lexer
// would read it, so a comment or a NUL between a key and its number hides nothing.
//
// The inflate budget is ONE budget for the whole document, charged only to the streams the text
// path decodes — see chargesInflation for the rule — so a document that embeds a large image or
// font goes uncharged for it while a bomb wired as page content is caught before the parser
// materialises it. A stream that fails to inflate is skipped, not refused: the parser will report
// it. A page dictionary compressed inside an object stream hides its /Contents reference from
// this raw scan; that gap is known and bounded — an object stream is itself charged.
func preflightPDF(ctx context.Context, data []byte) string {
	for _, columns := range declaredIntegers(data, "Columns") {
		if exceedsBound(columns, pdfMaxPredictorColumns) {
			return fmt.Sprintf(pdfPredictorCause, columns)
		}
	}

	objects := indexPDFObjects(data)
	decoded := decodedReferences(data, objects)
	remaining := int64(pdfMaxInflatedBytes)
	for _, object := range objects {
		if object.body == nil {
			continue
		}
		if cause := refuseAbsurdXrefWidths(object.value); cause != "" {
			return cause
		}
		if !chargesInflation(object, decoded) {
			continue
		}
		inflated := inflatedSize(ctx, object.body, countPDFNames(object.value, "FlateDecode"), remaining)
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

// pdfObject is one `N G obj` header the raw scan found outside every stream body: its number,
// the bytes that follow the keyword — the whole dictionary for a stream object, the leading
// value for any other — and, for a stream object, the body between `stream` and `endstream`.
type pdfObject struct {
	number int64
	value  []byte
	body   []byte
}

// indexPDFObjects lists every object header in the document, in file order, pairing the last
// header before each stream keyword with that stream's body. A stream whose keyword follows no
// header is unreachable — the cross-reference table addresses objects by their headers — so it
// is not listed and never charged.
func indexPDFObjects(data []byte) []pdfObject {
	bodies := pdfStreamBody.FindAllIndex(data, -1)

	var objects []pdfObject
	cursor := 0
	for gap := 0; gap <= len(bodies); gap++ {
		gapEnd := len(data)
		if gap < len(bodies) {
			gapEnd = bodies[gap][0]
		}
		headers := pdfObjectHeaders(data[cursor:gapEnd])
		for index, header := range headers {
			valueEnd := gapEnd
			if index+1 < len(headers) {
				valueEnd = cursor + headers[index+1].start
			}
			object := pdfObject{number: header.number, value: data[cursor+header.end : valueEnd]}
			if index == len(headers)-1 && gap < len(bodies) {
				object.body = streamBodyBytes(data, bodies[gap])
			}
			objects = append(objects, object)
		}
		if gap < len(bodies) {
			cursor = bodies[gap][1]
		}
	}
	return objects
}

// streamBodyBytes returns the content bytes of one pdfStreamBody match: what lies between the
// keyword's end-of-line and `endstream`.
func streamBodyBytes(data []byte, match []int) []byte {
	start := match[0] + len("stream")
	if data[start] == '\r' {
		start++
	}
	start++
	return data[start : match[1]-len("endstream")]
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
					if object, ok := byNumber[number]; ok && object.body == nil {
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
// only a FlateDecode stream the text path will decode does. An untyped stream — page content is
// untyped — is always charged. A stream whose dictionary carries a /Type or /Subtype is charged
// when its type is one the parser decodes (/XRef, /ObjStm, /CMap) or a /Contents or /ToUnicode
// reference names it, and skipped otherwise: an image or form XObject, an embedded font, an
// attached file. Type and reference win over any label, so a /Subtype /Image on an xref stream
// exempts nothing.
func chargesInflation(object pdfObject, decoded map[int64]bool) bool {
	if countPDFNames(object.value, "FlateDecode") == 0 {
		return false
	}
	if len(pdfNameSites(object.value, "Type")) == 0 && len(pdfNameSites(object.value, "Subtype")) == 0 {
		return true
	}
	switch pdfNameValue(object.value, "Type") {
	case "XRef", "ObjStm", "CMap":
		return true
	}
	return decoded[object.number]
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

// inflatedSize reports how many bytes body inflates to through the given number of zlib layers,
// reading no further than limit+1 bytes so a bomb is never materialised: a result above limit
// means "more than the budget", never the exact count. An inflate error — the bytes were not
// zlib, or ran out early — ends the count where it stands; the parser will report the error,
// and what inflated before it is still charged. Cancellation is read between chunks and reported
// through the context, which the caller consults.
func inflatedSize(ctx context.Context, body []byte, layers int, limit int64) int64 {
	var reader io.Reader = bytes.NewReader(body)
	for range layers {
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

// countPDFNames reports how many times the `/key` name token occurs in span.
func countPDFNames(span []byte, key string) int {
	return len(pdfNameSites(span, key))
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
