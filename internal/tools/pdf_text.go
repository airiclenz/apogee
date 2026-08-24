package tools

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// The PDF text extraction read_file leans on. Everything the rest of the package needs to know
// about the format lives behind the two functions below — detection, extraction, the page
// markers, and the model-facing wording of every failure — so a caller decides nothing about
// PDFs beyond "is this one" and "what came out".
//
// The parser is github.com/ledongthuc/pdf, a fork of rsc.io/pdf whose ancestor PANICS on
// malformed input rather than returning an error. A tool that crashes the agent on a corrupt
// download is not an option, so the whole parse-and-walk runs behind a recover and a recovered
// panic reads to the model exactly like any other unreadable file (ADR 0007: a tool-level
// failure is an IsError result, never a Go error).

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
)

// pdfMagic is the signature every PDF file opens with. Detection is a content sniff and nothing
// else: a text file someone named notes.pdf must read as text, and a real PDF saved without the
// extension must still extract.
var pdfMagic = []byte("%PDF-")

// isPDF reports whether data is a PDF document, judged solely by its leading bytes. Input
// shorter than the signature — the empty file included — is not a PDF.
func isPDF(data []byte) bool {
	return bytes.HasPrefix(data, pdfMagic)
}

// extractPDFText parses an in-memory PDF and returns its text with a "[Page N]" marker line
// before each page's text, exactly one blank line between one page's text and the next marker.
//
// The three results are one of two shapes, never a mix: failMessage != "" is a failure and both
// text and pages are meaningless; failMessage == "" means text is the whole document and pages
// is how many pages it has. The failure string is written FOR THE MODEL — it is returned to the
// caller to hand straight to errorResult, not wrapped or re-worded.
//
// A panic out of the parser is a failure like any other; nothing escapes this function. A page
// that fails on its own does NOT fail the document: its text becomes a
// "[Page N: text extraction failed]" placeholder and the walk continues. A document that parses
// to at least one page and yields no characters anywhere is the scanned-image case and fails with
// pdfNoTextMessage — read_file never falls back to raw bytes, because a wall of binary teaches the
// model nothing. A document that parses to NO pages is a document-level failure instead: nothing
// was read from it, so it cannot be reported as a scan.
func extractPDFText(data []byte) (text string, pages int, failMessage string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text, pages, failMessage = "", 0, fmt.Sprintf(pdfUnreadableFormat, recovered)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, fmt.Sprintf(pdfUnreadableFormat, err)
	}

	pages = reader.NumPage()
	// Zero pages is the reader accepting bytes it could not walk: the page tree yields nothing, so
	// the walk below never runs and every page-level signal stays untouched. That is a
	// document-level failure — nothing was read from this file — and not the scan
	// pdfNoTextMessage describes, so it goes out with the cause named.
	if pages <= 0 {
		return "", 0, fmt.Sprintf(pdfUnreadableFormat, "the document has no pages")
	}

	blocks := make([]string, 0, pages)
	hasText := false
	for number := 1; number <= pages; number++ {
		pageText, pageErr := reader.Page(number).GetPlainText(nil)
		if pageErr != nil {
			blocks = append(blocks, pageBlock(number, fmt.Sprintf(pdfPageFailedFormat, number)))
			continue
		}

		pageText = strings.TrimRight(pageText, " \t\r\n")
		if strings.TrimSpace(pageText) != "" {
			hasText = true
		}
		blocks = append(blocks, pageBlock(number, pageText))
	}

	if !hasText {
		return "", 0, pdfNoTextMessage
	}
	return strings.Join(blocks, "\n\n"), pages, ""
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
