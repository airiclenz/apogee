package agent

// @file references that name a DOCUMENT rather than a text file (plan 2026-08-25 - 05, item 2).
// resolveFileRefs sniffs every resolved reference for a document format and injects the
// extracted text — the same extraction, markers and failure wording read_file produces, because
// both seams call internal/doctext. These tests pin the three outcomes a reference can have (a
// document extracted, a document with no text skipped, a misnamed text file still read as text)
// and that Interject inherits all of it by calling the same resolver.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// copyPDFFixture copies one of doctext's committed PDF fixtures into a workspace under the
// given name. The reference under test is therefore fed exactly the bytes the extractor's own
// tests are fed, and the NAME it carries is chosen per test — which is what makes the
// content-not-name sniff observable from here.
func copyPDFFixture(t *testing.T, dir, fixture, name string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "doctext", "testdata", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatalf("write fixture as %s: %v", name, err)
	}
}

// refAgentInWorkspace builds the standard fixture for these tests: a scripted Agent whose
// workspace is dir, plus the sink that captured its events.
func refAgentInWorkspace(t *testing.T, dir string) (*Agent, *recordingSink) {
	t.Helper()

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, sink
}

// submitRefs submits text plus the given @file references and drives the opening Turn, returning
// the assembled user message.
func submitRefs(t *testing.T, a *Agent, text string, refs ...string) string {
	t.Helper()

	if err := a.Submit(domain.UserInput{Text: text, FileRefs: refs}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	return a.conv.At(0).Content
}

// submitOneRef submits text plus a single @file reference and drives the opening Turn, returning
// the assembled user message.
func submitOneRef(t *testing.T, a *Agent, text, ref string) string {
	t.Helper()

	return submitRefs(t, a, text, ref)
}

// TestResolveFileRefs_InjectsAPDFAsExtractedText is the core fact of item 2: a reference to a
// PDF puts the DOCUMENT'S TEXT in the user message, under a header that says so, and never the
// file's own bytes — the defect that spent a 1.3M-token window on one 27-page whitepaper.
func TestResolveFileRefs_InjectsAPDFAsExtractedText(t *testing.T) {
	dir := t.TempDir()
	copyPDFFixture(t, dir, "minimal.pdf", "minimal.pdf")

	a, sink := refAgentInWorkspace(t, dir)
	got := submitOneRef(t, a, "read it", "minimal.pdf")

	if !strings.Contains(got, "Referenced file `minimal.pdf` (PDF, 1 page; extracted text, read-only):") {
		t.Errorf("annotated header missing from the user message:\n%s", got)
	}
	if !strings.Contains(got, "[Page 1]") {
		t.Errorf("page marker missing from the injected text:\n%s", got)
	}
	if !strings.Contains(got, "Hello Apogee") {
		t.Errorf("the document's words are missing from the injected text:\n%s", got)
	}
	if strings.Contains(got, "%PDF-") {
		t.Errorf("raw PDF bytes reached the conversation:\n%s", got)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("a successfully extracted reference emitted an ErrorEvent")
	}
}

// TestResolveFileRefs_SkipsAPDFWithNoText pins the scanned-document case at this seam: the
// extractor's own model-facing sentence is surfaced as the ref-ignored ErrorEvent every other
// unresolvable reference uses (mirroring TestReadFileRefRefusesOversizeRef), the Turn completes,
// and nothing at all is injected — a wall of binary is not a fallback.
func TestResolveFileRefs_SkipsAPDFWithNoText(t *testing.T) {
	dir := t.TempDir()
	copyPDFFixture(t, dir, "notext.pdf", "scan.pdf")

	a, sink := refAgentInWorkspace(t, dir)
	got := submitOneRef(t, a, "read it", "scan.pdf")

	if !errorEventContaining(sink.events, "no extractable text") {
		t.Error("a PDF with no extractable text did not surface the extractor's message as an ErrorEvent")
	}
	if !errorEventContaining(sink.events, "could not be resolved and was ignored") {
		t.Error("the extraction failure did not use the shared ref-ignored wording")
	}
	if got != "read it" {
		t.Errorf("user message = %q, want just the text (an unextractable ref injects nothing)", got)
	}
}

// TestResolveFileRefs_JudgesAPDFByContentNotName proves the sniff reads bytes, not extensions: a
// text file someone called notes.pdf is injected verbatim under the plain header, with no
// annotation and no extraction attempted.
func TestResolveFileRefs_JudgesAPDFByContentNotName(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "notes.pdf", "PLAIN TEXT MARKER")

	a, sink := refAgentInWorkspace(t, dir)
	got := submitOneRef(t, a, "read it", "notes.pdf")

	if !strings.Contains(got, "Referenced file `notes.pdf`:\n") {
		t.Errorf("a text file named .pdf did not get the plain header:\n%s", got)
	}
	if strings.Contains(got, "extracted text, read-only") {
		t.Errorf("a text file named .pdf was announced as an extracted document:\n%s", got)
	}
	if !strings.Contains(got, "PLAIN TEXT MARKER") {
		t.Errorf("the file's text is missing from the user message:\n%s", got)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("a plain text file named .pdf emitted an ErrorEvent")
	}
}

// TestInterjectInjectsAPDFAsExtractedText proves the interjection path inherits the behaviour
// with no code of its own: Interject calls the same resolveFileRefs, so a document delivered
// mid-Exchange lands as the same annotated block.
func TestInterjectInjectsAPDFAsExtractedText(t *testing.T) {
	dir := t.TempDir()
	cfg := interjectConfig(&recordingSink{})
	cfg.WorkspaceDir = dir

	a, _ := interjectAgentAtBoundary(t, cfg)

	copyPDFFixture(t, dir, "minimal.pdf", "minimal.pdf")

	if err := a.Interject(domain.UserInput{Text: "and this doc", FileRefs: []string{"minimal.pdf"}}); err != nil {
		t.Fatalf("Interject: %v", err)
	}

	got := a.conv.At(a.conv.Len() - 1).Content
	if !strings.Contains(got, "Referenced file `minimal.pdf` (PDF, 1 page; extracted text, read-only):") {
		t.Errorf("annotated header missing from the interjection:\n%s", got)
	}
	if !strings.Contains(got, "[Page 1]") {
		t.Errorf("page marker missing from the interjection:\n%s", got)
	}
	if strings.Contains(got, "%PDF-") {
		t.Errorf("raw PDF bytes reached the interjected message:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// The STRUCTURAL size floor on @file blocks (plan 2026-08-25 - 05, item 3).
// A block past its share of the History allocation is elided to the same
// head/tail-plus-marker shape a tool result gets, at the one seam every
// reference crosses — so no reference can hand the emergency fold a message it
// cannot render. These tests budget against floorWindow (toolresultfloor_test.go),
// the window the tool-result floor's own tests use, because the two seams are
// deliberately measured by one arithmetic.
// ---------------------------------------------------------------------------

// elisionMarker is the head of the shared truncation note (internal/context toolresult.go). Both
// clamps render it, which is the point: the model learns one re-read idiom, not two.
const elisionMarker = "[truncated to fit the context budget"

// refFiller is the body of one synthetic reference line — short enough that the head/tail elision
// can genuinely shrink a file built from it (a handful of very long lines could not be shrunk at
// all), long enough that a readable number of lines overshoots a small window's allocation.
const refFiller = "referenced text that repeats until the file is large enough to matter"

// clampingRefAgent builds a ref agent whose context window is KNOWN and small, and returns the
// single-reference structural bound in characters (the History allocation × the chars→token
// ratio) — the number the reference files below are sized around.
func clampingRefAgent(t *testing.T, dir string) (*Agent, int) {
	t.Helper()

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	b := a.budget()
	if b.History <= 0 {
		t.Fatalf("History allocation = %d, want a positive allocation from window %d", b.History, floorWindow)
	}
	return a, int(float64(b.History) * b.CharsPerToken)
}

// refLine renders one numbered line of a synthetic reference file. The index makes the first and
// last lines distinguishable, which is what makes a preserved head and tail observable.
func refLine(index int, filler string) string {
	return fmt.Sprintf("line %04d: %s", index, filler)
}

// writeRefFile writes a workspace file of the given number of numbered filler lines and returns
// its content.
func writeRefFile(t *testing.T, dir, name string, lines int, filler string) string {
	t.Helper()

	rows := make([]string, lines)
	for i := range rows {
		rows[i] = refLine(i, filler)
	}
	content := strings.Join(rows, "\n")
	writeWorkspaceFile(t, dir, name, content)
	return content
}

// blockBody returns the fenced body of the first @file block in a user message — the region the
// structural clamp rewrites, below the header it never touches.
func blockBody(t *testing.T, message string) string {
	t.Helper()

	_, afterOpen, opened := strings.Cut(message, "```\n")
	if !opened {
		t.Fatalf("no fenced block in the user message:\n%.200s", message)
	}
	body, _, closed := strings.Cut(afterOpen, "\n```")
	if !closed {
		t.Fatalf("unterminated fenced block in the user message:\n%.200s", message)
	}
	return body
}

// TestResolveFileRefs_ClampsAReferencePastTheHistoryAllocation is item 3's core fact: a reference
// bigger than the whole History allocation enters the conversation elided, not whole — the state
// no later reducer could have rescued, since the emergency fold keeps the most recent message
// unconditionally. A reference under the bound is still injected verbatim: this is a floor, not a
// budgeter.
func TestResolveFileRefs_ClampsAReferencePastTheHistoryAllocation(t *testing.T) {
	dir := t.TempDir()
	longFiller := strings.Repeat("referenced text ", 32)
	big := writeRefFile(t, dir, "big.txt", 400, longFiller)

	a, floorChars := clampingRefAgent(t, dir)
	if len(big) <= floorChars {
		t.Fatalf("payload of %d chars does not exceed the bound of %d chars; the test proves nothing", len(big), floorChars)
	}

	got := submitOneRef(t, a, "read it", "big.txt")

	if !strings.HasPrefix(got, "Referenced file `big.txt`:\n") {
		t.Errorf("an elided block lost its header:\n%.200s", got)
	}
	if !strings.Contains(got, elisionMarker) {
		t.Errorf("an oversized reference was not clamped (no elision marker):\n%.200s", got)
	}
	if !strings.Contains(got, refLine(0, longFiller)) {
		t.Error("the file's first line did not survive the clamp")
	}
	if !strings.Contains(got, refLine(399, longFiller)) {
		t.Error("the file's last line did not survive the clamp")
	}
	if len(got) >= len(big) {
		t.Errorf("user message is %d chars for a %d-char file; the clamp did not shrink it", len(got), len(big))
	}

	small := writeRefFile(t, dir, "small.txt", 20, longFiller)
	if len(small) >= floorChars {
		t.Fatalf("the under-the-bound payload of %d chars is not under the bound of %d chars", len(small), floorChars)
	}
	under, _ := clampingRefAgent(t, dir)

	whole := submitOneRef(t, under, "read it", "small.txt")

	if strings.Contains(whole, elisionMarker) {
		t.Error("a reference under the bound was clamped; the floor is not a budgeter")
	}
	if !strings.Contains(whole, small) {
		t.Error("a reference under the bound was not injected verbatim")
	}
}

// TestResolveFileRefs_ClampsAPDFPastTheHistoryAllocation pins the ORDER the floor and the document
// annotation are applied in — the property that survives whatever a document extracts to. No
// fixture extracts past the bound, so the two halves are proved separately: an extracted document
// under the bound keeps its full annotated header and page markers, and a block whose body IS
// clamped still carries its header, because the header is added after the clamp.
func TestResolveFileRefs_ClampsAPDFPastTheHistoryAllocation(t *testing.T) {
	dir := t.TempDir()
	copyPDFFixture(t, dir, "minimal.pdf", "minimal.pdf")
	writeRefFile(t, dir, "big.txt", 400, strings.Repeat("referenced text ", 32))

	a, _ := clampingRefAgent(t, dir)
	document := submitOneRef(t, a, "read it", "minimal.pdf")

	if !strings.Contains(document, "Referenced file `minimal.pdf` (PDF, 1 page; extracted text, read-only):") {
		t.Errorf("annotated header missing from a document under the bound:\n%.200s", document)
	}
	if !strings.Contains(document, "[Page 1]") {
		t.Errorf("page marker missing from a document under the bound:\n%.200s", document)
	}
	if strings.Contains(document, elisionMarker) {
		t.Error("a document under the bound was clamped")
	}

	clamping, _ := clampingRefAgent(t, dir)
	clamped := submitOneRef(t, clamping, "read it", "big.txt")

	header := strings.Index(clamped, "Referenced file `big.txt`:")
	marker := strings.Index(clamped, elisionMarker)
	if header != 0 {
		t.Errorf("header index = %d, want the clamped block to still open with it", header)
	}
	if marker <= header {
		t.Errorf("elision marker at %d, header at %d: the clamp swallowed the header", marker, header)
	}
}

// TestResolveFileRefs_SplitsTheFloorAcrossReferences pins design call 4: the bound is the History
// allocation divided across the references of ONE message. Two files that each pass alone are both
// clamped when submitted together, and the assembled message still fits the allocation — which is
// what keeps the fold's keep-the-most-recent-message rule survivable however many refs a message
// carries.
func TestResolveFileRefs_SplitsTheFloorAcrossReferences(t *testing.T) {
	dir := t.TempDir()
	alone, floorChars := clampingRefAgent(t, dir)

	lines := floorChars * 7 / 10 / (len(refLine(0, refFiller)) + 1)
	first := writeRefFile(t, dir, "first.txt", lines, refFiller)
	writeRefFile(t, dir, "second.txt", lines, refFiller)
	if len(first) >= floorChars || len(first) <= floorChars/2 {
		t.Fatalf("payload of %d chars must sit between half the bound and the bound (%d chars)", len(first), floorChars)
	}

	solo := submitOneRef(t, alone, "read it", "first.txt")
	if strings.Contains(solo, elisionMarker) {
		t.Fatalf("a %d-char reference was clamped alone under a %d-char bound; the split proves nothing", len(first), floorChars)
	}

	together, _ := clampingRefAgent(t, dir)
	got := submitRefs(t, together, "read both", "first.txt", "second.txt")

	if markers := strings.Count(got, elisionMarker); markers != 2 {
		t.Errorf("elision marker appears %d times, want one per block: the floor was not split across the references", markers)
	}
	b := together.budget()
	if estimated := b.EstimateTokens(len(got)); estimated > b.History {
		t.Errorf("the assembled message estimates %d tokens, past the History allocation of %d", estimated, b.History)
	}
}

// TestClampToBound_SharedByToolResultsAndFileRefs proves the two seams share ONE rendering rather
// than growing two elision idioms: the same oversized payload comes out byte-identical whether it
// arrives as a tool result or as a single @file block.
func TestClampToBound_SharedByToolResultsAndFileRefs(t *testing.T) {
	dir := t.TempDir()
	longFiller := strings.Repeat("referenced text ", 32)
	big := writeRefFile(t, dir, "big.txt", 400, longFiller)

	a, _ := clampingRefAgent(t, dir)
	viaToolResult := a.clampToolResult(big) // taken before the Turn, so both readings share one Budget
	if viaToolResult == big {
		t.Fatalf("the payload of %d chars was not clamped at all; the comparison would be vacuous", len(big))
	}

	viaFileRef := blockBody(t, submitOneRef(t, a, "read it", "big.txt"))

	if viaFileRef != viaToolResult {
		t.Errorf("the @file block renders %d chars and the tool result %d; the two seams diverged", len(viaFileRef), len(viaToolResult))
	}
}
