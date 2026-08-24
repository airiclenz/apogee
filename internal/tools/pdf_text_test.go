package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readPDFFixture returns the bytes of a committed PDF under testdata. The fixtures are
// hand-built minimal documents rather than generated at test time, so what the parser is fed
// here is exactly the bytes a reviewer can read in the repository.
func readPDFFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestIsPDF pins detection to the content sniff and nothing else: the signature decides, and a
// buffer too short to carry it is not a PDF rather than a panic.
func TestIsPDF(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "real pdf fixture", data: readPDFFixture(t, "minimal.pdf"), want: true},
		{name: "signature alone", data: []byte("%PDF-"), want: true},
		{name: "plain text", data: []byte("Hello Apogee, this is not a PDF.\n"), want: false},
		{name: "empty input", data: nil, want: false},
		{name: "shorter than the marker", data: []byte("%PD"), want: false},
		{name: "signature not at the start", data: []byte(" %PDF-1.4"), want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := isPDF(testCase.data)

			if got != testCase.want {
				t.Fatalf("isPDF(%q) = %v, want %v", truncateForMessage(testCase.data), got, testCase.want)
			}
		})
	}
}

// TestExtractPDFText_ReturnsTheTextWithAPageMarker is the success path: a one-page document
// comes back as its own text behind a single "[Page 1]" marker line, with the page count the
// header will quote.
func TestExtractPDFText_ReturnsTheTextWithAPageMarker(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "minimal.pdf")

	text, pages, failMessage := extractPDFText(data)

	if failMessage != "" {
		t.Fatalf("extractPDFText failed: %s", failMessage)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want 1", pages)
	}
	if !strings.Contains(text, "Hello Apogee") {
		t.Errorf("text %q does not contain the fixture's words", text)
	}
	if got := strings.Count(text, "[Page 1]"); got != 1 {
		t.Errorf("text has %d [Page 1] markers, want 1 — text: %q", got, text)
	}
	if lines := strings.Split(text, "\n"); lines[0] != "[Page 1]" {
		t.Errorf("first line = %q, want the marker alone on its line", lines[0])
	}
}

// TestExtractPDFText_ReportsAScannedDocument covers a document that parses cleanly and carries
// no text operators at all — the scan case, whose message must send the model to the user for a
// text version rather than leave it guessing.
func TestExtractPDFText_ReportsAScannedDocument(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "notext.pdf")

	text, pages, failMessage := extractPDFText(data)

	if failMessage != pdfNoTextMessage {
		t.Fatalf("failMessage = %q, want the no-extractable-text message", failMessage)
	}
	if strings.Contains(failMessage, "may be corrupted or encrypted") {
		t.Errorf("failMessage = %q, want the scan message rather than the document-level failure", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
}

// TestExtractPDFText_ReportsAZeroPageDocumentAsUnreadable covers the document the reader accepts
// and cannot walk: its page tree yields no pages at all. Nothing was read from the file, so the
// model must be told it is unreadable — telling it the document is a scan would send it to the
// user for a "text version" of a file that never parsed.
func TestExtractPDFText_ReportsAZeroPageDocumentAsUnreadable(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "nopages.pdf")

	text, pages, failMessage := extractPDFText(data)

	// The cause is the guard's own literal, so this also pins that the guard is the path taken
	// rather than pdf.NewReader's error path, which words the same message with its own cause.
	if !strings.Contains(failMessage, "the document has no pages") {
		t.Fatalf("failMessage = %q, want the zero-page cause the guard supplies", failMessage)
	}
	if !strings.Contains(failMessage, "may be corrupted or encrypted") {
		t.Errorf("failMessage = %q, want the could-not-extract message", failMessage)
	}
	if strings.Contains(failMessage, "likely scanned images") {
		t.Errorf("failMessage = %q, want it NOT to report a scan", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
}

// TestExtractPDFText_ReportsUnreadableBytes feeds the parser input it cannot make sense of. The
// parser's ancestor panics on malformed documents, so this asserts both halves of the contract:
// the model gets the could-not-extract sentence, and nothing escapes as a panic.
func TestExtractPDFText_ReportsUnreadableBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
	}{
		{name: "signature then garbage", data: []byte("%PDF-1.4\n\x00\x01\x02 not a document at all \xff\xfe")},
		{name: "signature alone", data: []byte("%PDF-")},
		{name: "truncated document", data: readPDFFixture(t, "minimal.pdf")[:120]},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			text, pages, failMessage := extractPDFText(testCase.data)

			if !strings.HasPrefix(failMessage, "could not extract text from this PDF:") {
				t.Fatalf("failMessage = %q, want the could-not-extract message", failMessage)
			}
			if !strings.Contains(failMessage, "ask the user for a text version") {
				t.Errorf("failMessage = %q, want it to name the way out", failMessage)
			}
			if text != "" || pages != 0 {
				t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
			}
		})
	}
}

// truncateForMessage keeps a failure message readable when the input under test is a whole
// document rather than a short literal.
func truncateForMessage(data []byte) string {
	const limit = 32

	if len(data) <= limit {
		return string(data)
	}
	return string(data[:limit]) + "…"
}
