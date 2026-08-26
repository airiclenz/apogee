package doctext

import (
	"bytes"
	"context"
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
// (audit 2026-08-25 — C-07, F-25, F-26).

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
// page must not cost the model the other ninety-nine.
func (a *pageAccumulator) record(number int, pageText string, pageErr error) {
	switch {
	case pageErr != nil:
		a.keep(number, pageBlock(number, fmt.Sprintf(pdfPageFailedFormat, number)))
	case strings.TrimSpace(pageText) == "":
		a.hold(number, pageBlock(number, ""))
	default:
		a.hasText = true
		a.keep(number, pageBlock(number, strings.TrimRight(pageText, " \t\r\n")))
	}
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

// pdfDeclaredSize matches every /Size entry in the RAW bytes — the trailer's and any xref-stream
// dictionary's alike, because both size the cross-reference table the parser allocates before it
// reads a single object (read.go:233,392). The scan is over the bytes rather than the parsed
// document for the same reason: by the time the parser could report the number, it has already
// allocated for it.
var pdfDeclaredSize = regexp.MustCompile(`/Size\s+(\d+)`)

// refuseAbsurdObjectCount returns the model-facing refusal for a document declaring more objects
// than its own bytes could hold, or "" when every declared count is possible. The bound is the
// loosest sound one — one byte per object, where the smallest real object costs about twenty —
// so it refuses impossible documents and nothing a real producer emits.
func refuseAbsurdObjectCount(data []byte) string {
	for _, match := range pdfDeclaredSize.FindAllSubmatch(data, -1) {
		declared := string(match[1])
		// A count too long for uint64 is refused on its digits alone: a file that cannot hold
		// 2^64 objects cannot hold more than that either.
		size, err := strconv.ParseUint(declared, 10, 64)
		if err == nil && size <= uint64(len(data)) {
			continue
		}
		return fmt.Sprintf(pdfUnreadableFormat, fmt.Sprintf(pdfAbsurdSizeFormat, declared, len(data)))
	}
	return ""
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
