package agent

// @file references that name a DOCUMENT rather than a text file (plan 2026-08-25 - 05, item 2).
// resolveFileRefs sniffs every resolved reference for a document format and injects the
// extracted text — the same extraction, markers and failure wording read_file produces, because
// both seams call internal/doctext. These tests pin the three outcomes a reference can have (a
// document extracted, a document with no text skipped, a misnamed text file still read as text)
// and that Interject inherits all of it by calling the same resolver.

import (
	"context"
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

// submitOneRef submits text plus a single @file reference and drives the opening Turn, returning
// the assembled user message.
func submitOneRef(t *testing.T, a *Agent, text, ref string) string {
	t.Helper()

	if err := a.Submit(domain.UserInput{Text: text, FileRefs: []string{ref}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	return a.conv.At(0).Content
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
