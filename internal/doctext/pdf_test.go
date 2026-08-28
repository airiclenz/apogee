package doctext

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

			got := IsPDF(testCase.data)

			if got != testCase.want {
				t.Fatalf("IsPDF(%q) = %v, want %v", truncateForMessage(testCase.data), got, testCase.want)
			}
		})
	}
}

// TestExtractPDF_ReturnsTheTextWithAPageMarker is the success path: a one-page document
// comes back as its own text behind a single "[Page 1]" marker line, with the page count the
// header will quote.
func TestExtractPDF_ReturnsTheTextWithAPageMarker(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "minimal.pdf")

	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)

	if failMessage != "" {
		t.Fatalf("ExtractPDF failed: %s", failMessage)
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

// TestExtractPDF_ReportsAScannedDocument covers a document that parses cleanly and carries
// no text operators at all — the scan case, whose message must send the model to the user for a
// text version rather than leave it guessing.
func TestExtractPDF_ReportsAScannedDocument(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "notext.pdf")

	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)

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

// TestExtractPDF_ReportsAZeroPageDocumentAsUnreadable covers the document the reader accepts
// and cannot walk: its page tree yields no pages at all. Nothing was read from the file, so the
// model must be told it is unreadable — telling it the document is a scan would send it to the
// user for a "text version" of a file that never parsed.
func TestExtractPDF_ReportsAZeroPageDocumentAsUnreadable(t *testing.T) {
	t.Parallel()

	data := readPDFFixture(t, "nopages.pdf")

	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)

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

// TestExtractPDF_ReportsUnreadableBytes feeds the parser input it cannot make sense of. The
// parser's ancestor panics on malformed documents, so this asserts both halves of the contract:
// the model gets the could-not-extract sentence, and nothing escapes as a panic.
func TestExtractPDF_ReportsUnreadableBytes(t *testing.T) {
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

			text, pages, failMessage := ExtractPDF(context.Background(), testCase.data, 0)

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

// TestPDFAnnotation pins the header annotation's exact wording across the page count: exactly
// one page reads "1 page", every other count reads "N pages", and every count carries the
// read-only hint. Both headers that quote a document — read_file's [File: …] line and the @file
// block's Referenced file line — are built from this string, so these literals are what the
// model actually reads; zero is in the table because the function treats it as plural and
// nothing else asserts that branch.
func TestPDFAnnotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		pages int
		want  string
	}{
		{name: "zero pages reads as plural", pages: 0, want: "PDF, 0 pages; extracted text, read-only"},
		{name: "one page reads as singular", pages: 1, want: "PDF, 1 page; extracted text, read-only"},
		{name: "two pages reads as plural", pages: 2, want: "PDF, 2 pages; extracted text, read-only"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := PDFAnnotation(testCase.pages)

			if got != testCase.want {
				t.Errorf("PDFAnnotation(%d) = %q, want %q", testCase.pages, got, testCase.want)
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

// hostilePDF assembles a syntactically correct PDF around the object BODIES it is given: the
// signature, one numbered object per body, a cross-reference table with real offsets, and a
// trailer naming object 1 as the catalog. It exists so a test can hand the parser a document
// whose CONTENT is hostile — a /Count no page tree could satisfy, a /Kids entry pointing at its
// own node, more pages than any bound allows — without the parser rejecting the file as
// malformed first, which would prove nothing about the bounds.
func hostilePDF(t *testing.T, objects ...string) []byte {
	t.Helper()

	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for index, body := range objects {
		offsets = append(offsets, document.Len())
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, body)
	}

	xref := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xref)
	return document.Bytes()
}

// contentStream renders one page's text as a content-stream object body. /Length must be the
// stream's exact byte count or the parser reads the wrong span, so it is computed rather than
// written by hand.
func contentStream(text string) string {
	body := "BT\n/F1 24 Tf\n72 720 Td\n(" + text + ") Tj\nET"
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
}

// textPagesPDF builds a document with one text-carrying page per string, behind a flat page
// tree. Flat is right for a handful of pages and wrong for thousands — see treePagesPDF.
func textPagesPDF(t *testing.T, pageTexts ...string) []byte {
	t.Helper()

	count := len(pageTexts)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	kids := make([]string, 0, count)
	streams := make([]string, 0, count)
	for index, text := range pageTexts {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+index))
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
			" /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 4+count+index))
		streams = append(streams, contentStream(text))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), count)
	return hostilePDF(t, append(objects, streams...)...)
}

// treePagesPDF builds a document of count tiny text pages behind a NESTED page tree — the shape
// a real producer emits at this size, and the shape the PAGE CAP has to be tested against. A flat
// /Kids array of two thousand entries costs the parser a linear scan per page lookup, so the read
// budget would trip long before the page cap did and the test would pin the wrong bound.
//
// Object numbers: 1 the catalog, 2 the font, 3 the content stream every page shares, then the
// leaf pages and the internal nodes above them, root last — so the catalog names the root
// directly instead of the tree having to end up at a fixed number.
func treePagesPDF(t *testing.T, count int) []byte {
	t.Helper()

	const branching = 8

	if count < 2 {
		t.Fatalf("treePagesPDF needs at least two pages to have a tree, got %d", count)
	}

	parents := make(map[int]int)
	kids := make(map[int][]int)
	beneath := make(map[int]int)
	next := 4
	level := make([]int, 0, count)
	for range count {
		beneath[next] = 1
		level = append(level, next)
		next++
	}
	for len(level) > 1 {
		above := make([]int, 0, len(level)/branching+1)
		for start := 0; start < len(level); start += branching {
			chunk := level[start:min(start+branching, len(level))]
			parent := next
			next++
			for _, child := range chunk {
				parents[child] = parent
				beneath[parent] += beneath[child]
			}
			kids[parent] = chunk
			above = append(above, parent)
		}
		level = above
	}

	objects := make([]string, next-1)
	objects[0] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", level[0])
	objects[1] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
	objects[2] = contentStream("apogee")
	for object := 4; object < next; object++ {
		objects[object-1] = pageTreeObject(object, parents, kids, beneath)
	}
	return hostilePDF(t, objects...)
}

// pageTreeObject renders one node of treePagesPDF's page tree: an internal /Pages node when it
// has children, a leaf /Page otherwise. Only the root has no /Parent.
func pageTreeObject(object int, parents map[int]int, kids map[int][]int, beneath map[int]int) string {
	parent := ""
	if above, nested := parents[object]; nested {
		parent = fmt.Sprintf(" /Parent %d 0 R", above)
	}

	children, internal := kids[object]
	if !internal {
		return fmt.Sprintf("<< /Type /Page%s /MediaBox [0 0 612 792]"+
			" /Resources << /Font << /F1 2 0 R >> >> /Contents 3 0 R >>", parent)
	}

	refs := make([]string, 0, len(children))
	for _, child := range children {
		refs = append(refs, fmt.Sprintf("%d 0 R", child))
	}
	return fmt.Sprintf("<< /Type /Pages%s /Kids [%s] /Count %d >>",
		parent, strings.Join(refs, " "), beneath[object])
}

// TestExtractPDF_CapsAPhantomPageCount is the measured attack (audit 2026-08-25, C-07): 240 bytes
// declaring ten million pages behind an EMPTY page tree, which cost 51 s and ~142 GiB of churn
// before the walk was bounded. Every page lookup answers with a null page, so the page cap alone
// would still walk two thousand of them — it is the consecutive-blank run that stops this one.
//
// It does not run in parallel: testing.AllocsPerRun measures the whole process, so a concurrent
// test's allocations would be counted here.
func TestExtractPDF_CapsAPhantomPageCount(t *testing.T) {
	// A bounded walk of this document measures ~1 350 allocations, nearly all of them the parse
	// itself. The ceiling is loose on purpose: what it has to catch is the walk that believes the
	// /Count, which allocates by the million.
	const allocCeiling = 5000

	data := readPDFFixture(t, "phantompages.pdf")

	start := time.Now()
	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)
	elapsed := time.Since(start)

	if failMessage != pdfNoTextMessage {
		t.Fatalf("failMessage = %q, want the no-extractable-text message", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
	if elapsed > 2*time.Second {
		t.Errorf("extraction took %s on a 240-byte document, want it bounded", elapsed)
	}
	if allocs := testing.AllocsPerRun(3, func() {
		ExtractPDF(context.Background(), data, 0)
	}); allocs > allocCeiling {
		t.Errorf("allocations per extraction = %.0f, want at most %d: the walk sized itself off /Count",
			allocs, allocCeiling)
	}
}

// TestExtractPDF_RefusesAnAbsurdXrefSize pins the guard that runs BEFORE the parser (F-25): the
// cross-reference table is allocated one entry per DECLARED object, so a document naming four
// billion of them sizes the agent's memory from its own trailer. The fixture is a real,
// otherwise-readable document with only that number rewritten, so the refusal can come from
// nothing but the declared count.
func TestExtractPDF_RefusesAnAbsurdXrefSize(t *testing.T) {
	t.Parallel()

	readable := readPDFFixture(t, "minimal.pdf")
	data := bytes.Replace(readable, []byte("/Size 6"), []byte("/Size 4000000000"), 1)
	if bytes.Equal(data, readable) {
		t.Fatalf("the fixture's trailer no longer spells /Size 6; the test rewrites nothing")
	}

	start := time.Now()
	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)
	elapsed := time.Since(start)

	if !strings.HasPrefix(failMessage, "could not extract text from this PDF:") {
		t.Fatalf("failMessage = %q, want the could-not-extract message", failMessage)
	}
	if !strings.Contains(failMessage, "declares 4000000000 objects in") {
		t.Errorf("failMessage = %q, want it to name the declared object count", failMessage)
	}
	if !strings.Contains(failMessage, fmt.Sprintf("in %d bytes", len(data))) {
		t.Errorf("failMessage = %q, want it to name the file's own length beside the count", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("refusal took %s, want it decided before the parser allocates", elapsed)
	}
}

// TestExtractPDF_ReadsAnAbsurdSizeInsideAStreamAsContent pins the other half of the guard above:
// a stream body is CONTENT, not a dictionary, so bytes that merely spell /Size in it size nothing
// and must not cost the document its extraction. A compressed image, an embedded font or a nested
// PDF can spell any byte sequence, so a whole-file scan would refuse valid documents at random.
func TestExtractPDF_ReadsAnAbsurdSizeInsideAStreamAsContent(t *testing.T) {
	t.Parallel()

	const hostileText = "/Size 4000000000"

	text, pages, failMessage := ExtractPDF(context.Background(), textPagesPDF(t, hostileText), 0)

	if failMessage != "" {
		t.Fatalf("ExtractPDF failed: %s", failMessage)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want the document's single page", pages)
	}
	if !strings.Contains(text, hostileText) {
		t.Errorf("text = %q, want it to carry the page's own %q", text, hostileText)
	}
}

// TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamDictionary pins the boundary the exclusion is
// drawn at: an xref stream's DICTIONARY always precedes its stream keyword, and that dictionary's
// /Size is exactly the number the parser allocates the cross-reference table from. Skipping stream
// bodies must not skip the dictionary that introduces one, or the guard would be evaded by moving
// the count out of the trailer and into an xref stream — the modern way to write one.
func TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamDictionary(t *testing.T) {
	t.Parallel()

	const crossReferenceRow = "00000000"
	xrefStream := fmt.Sprintf("<< /Type /XRef /Size 4000000000 /W [1 2 1] /Root 1 0 R /Length %d >>"+
		"\nstream\n%s\nendstream", len(crossReferenceRow), crossReferenceRow)
	data := hostilePDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		xrefStream,
	)

	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)

	if !strings.Contains(failMessage, "declares 4000000000 objects in") {
		t.Fatalf("failMessage = %q, want the refusal naming the dictionary's declared count", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
}

// TestExtractPDF_BoundsAReferenceCycle covers F-26: a /Pages node whose /Kids names itself. The
// page-tree descent follows it forever and never returns a page at all, so no page-level bound
// can catch it — only the read budget can, because the parser keeps no value cache and each turn
// of the loop resolves the node again.
func TestExtractPDF_BoundsAReferenceCycle(t *testing.T) {
	t.Parallel()

	// Ten seconds, not the ~2 s this measures: the bound is on READS, so the wall time it buys
	// scales with the machine and roughly triples under the race detector make check runs. What
	// the assertion is for is that the walk terminates at all.
	const bounded = 10 * time.Second

	data := hostilePDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [2 0 R] /Count 1 >>",
	)

	start := time.Now()
	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)
	elapsed := time.Since(start)

	if !strings.Contains(failMessage, "does not terminate") {
		t.Fatalf("failMessage = %q, want the budget's own cause", failMessage)
	}
	if !strings.Contains(failMessage, "ask the user for a text version") {
		t.Errorf("failMessage = %q, want it to name the way out", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
	if elapsed > bounded {
		t.Errorf("extraction took %s on a cyclic page tree, want it bounded", elapsed)
	}
}

// TestExtractPDF_HonoursACancelledContext pins the caller's own bound: a cancelled Turn stops a
// document mid-walk. The context is checked on every read the parser makes, so cancellation
// lands whether it arrives before the parse or during it, and the model is told which of the
// bounds ended the walk rather than reading the parser's complaint about a truncated object.
func TestExtractPDF_HonoursACancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	text, pages, failMessage := ExtractPDF(ctx, textPagesPDF(t, "alpha", "beta", "gamma"), 0)

	if !strings.Contains(failMessage, pdfCancelledCause) {
		t.Fatalf("failMessage = %q, want the cancelled cause", failMessage)
	}
	if text != "" || pages != 0 {
		t.Errorf("failure returned text %q and pages %d, want both empty", text, pages)
	}
}

// TestExtractPDF_StopsAtTheTextBudget pins the caller's output ceiling: the walk stops after the
// page that crosses it, and what comes back says how much document it is not. Extracting a
// thousand pages for a caller that will clamp the block to one is time and memory spent on bytes
// dropped on arrival.
func TestExtractPDF_StopsAtTheTextBudget(t *testing.T) {
	t.Parallel()

	data := textPagesPDF(t, "alpha", "beta", "gamma")

	text, pages, failMessage := ExtractPDF(context.Background(), data, 1)

	if failMessage != "" {
		t.Fatalf("ExtractPDF failed: %s", failMessage)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want the single page the budget allowed", pages)
	}
	if !strings.Contains(text, "alpha") {
		t.Errorf("text %q lost the page the budget did allow", text)
	}
	if strings.Contains(text, "beta") || strings.Contains(text, "gamma") {
		t.Errorf("text %q carries pages past the budget", text)
	}
	if want := "[Pages 2–3 not extracted: content budget]"; !strings.HasSuffix(text, want) {
		t.Errorf("text %q does not end with %q", text, want)
	}
}

// TestExtractPDF_CapsThePageWalk pins the page cap on a document whose pages are all REAL: no
// blank run stops this one and nothing is malformed about it, so the cap is the only bound that
// can. The marker is the point as much as the cap is — a model that reads two thousand pages of
// a longer document must be told that is what happened.
func TestExtractPDF_CapsThePageWalk(t *testing.T) {
	t.Parallel()

	data := treePagesPDF(t, pdfMaxPages+1)

	text, pages, failMessage := ExtractPDF(context.Background(), data, 0)

	if failMessage != "" {
		t.Fatalf("ExtractPDF failed: %s", failMessage)
	}
	if pages != pdfMaxPages {
		t.Errorf("pages = %d, want the page cap of %d", pages, pdfMaxPages)
	}
	if !strings.Contains(text, fmt.Sprintf("[Page %d]", pdfMaxPages)) {
		t.Errorf("the last page inside the cap is missing from the text:\n%.200s", text)
	}
	if want := fmt.Sprintf("[Pages %d–%d not extracted: %s]", pdfMaxPages+1, pdfMaxPages+1, pdfStopPageCap); !strings.HasSuffix(text, want) {
		t.Errorf("text does not end with %q; tail: %q", want, text[max(0, len(text)-80):])
	}
}
