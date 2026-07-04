package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// enrich fires error_enrichment once against result, with call as the originating call and history
// as the conversation so far, and reports whether it appended the enrichment.
func enrich(t *testing.T, history []domain.Message, call domain.ToolCall, result *domain.ToolResult) bool {
	t.Helper()
	hook, ok := mustBuild(t, errorEnrichmentID).(domain.PostToolResultHook)
	if !ok {
		t.Fatal("error_enrichment does not implement PostToolResultHook")
	}
	before := result.Content
	if err := hook.PostToolResult(context.Background(), call, result, historyView(history)); err != nil {
		t.Fatalf("PostToolResult: %v", err)
	}
	return result.Content != before
}

// A second write to the same file failing the same way earns a category-specific hint appended to
// the failing result — the "≥2 same-file errors → one enriched hint" case (apogee-sim
// detectRepeatedErrors @pin: the last error matches an earlier one by file + category).
func TestErrorEnrichmentEnrichesRepeatedWriteError(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("fix a.go"),
		assistantCall(writeCall("w1", "a.go", "package main")),
		toolResult("w1", "syntax error: unexpected token near }"),
		assistantCall(writeCall("w2", "a.go", "package main")),
	}
	result := &domain.ToolResult{CallID: "w2", Content: "syntax error: unexpected }", IsError: true}
	if !enrich(t, history, writeCall("w2", "a.go", "package main"), result) {
		t.Fatal("a repeated same-file same-category write error should be enriched")
	}
	if !strings.Contains(result.Content, errorEnrichmentMarker) {
		t.Errorf("enriched result missing the marker; got %q", result.Content)
	}
	if !strings.Contains(result.Content, "read_file") {
		t.Error("the syntax-error guidance should suggest reading the file")
	}
}

// The FIRST failure on a file is not enriched — there is no earlier same-file/same-category error to
// match, so the guidance is held until the repeat.
func TestErrorEnrichmentFirstErrorNotEnriched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("fix a.go"),
		assistantCall(writeCall("w1", "a.go", "package main")),
	}
	result := &domain.ToolResult{CallID: "w1", Content: "syntax error: unexpected }", IsError: true}
	if enrich(t, history, writeCall("w1", "a.go", "package main"), result) {
		t.Error("a first-time error must not be enriched")
	}
}

// Once an earlier result carries the enrichment for a file, a further failure on that file is not
// enriched again — one hint per repeated-error episode (marker-deduped).
func TestErrorEnrichmentDedupedAcrossEpisode(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("fix a.go"),
		assistantCall(writeCall("w1", "a.go", "x")),
		toolResult("w1", "syntax error in a.go"),
		assistantCall(writeCall("w2", "a.go", "y")),
		toolResult("w2", "syntax error in a.go\n\n"+errorEnrichmentMarker),
		assistantCall(writeCall("w3", "a.go", "z")),
	}
	result := &domain.ToolResult{CallID: "w3", Content: "syntax error in a.go again", IsError: true}
	if enrich(t, history, writeCall("w3", "a.go", "z"), result) {
		t.Error("a file already enriched this episode must not be enriched again")
	}
}

// error_enrichment acts only on write-tool errors of a known, actionable category: a read error, a
// non-error result, an unknown category, and a plain missing-file are all left untouched (apogee-sim
// detectRepeatedErrors skips read tools and the errUnknown/errMissingFile categories @pin).
func TestErrorEnrichmentSkipsNonActionable(t *testing.T) {
	t.Parallel()
	priorWrite := []domain.Message{
		userMsg("fix a.go"),
		assistantCall(writeCall("w1", "a.go", "x")),
		toolResult("w1", "syntax error near }"),
		assistantCall(writeCall("w2", "a.go", "y")),
	}
	cases := []struct {
		name    string
		history []domain.Message
		call    domain.ToolCall
		result  *domain.ToolResult
	}{
		{"read tool error", priorWrite, readCall("w2", "a.go"), &domain.ToolResult{CallID: "w2", Content: "syntax error", IsError: true}},
		{"not an error", priorWrite, writeCall("w2", "a.go", "y"), &domain.ToolResult{CallID: "w2", Content: "wrote a.go", IsError: false}},
		{"unknown category", priorWrite, writeCall("w2", "a.go", "y"), &domain.ToolResult{CallID: "w2", Content: "the operation did not complete", IsError: true}},
		{"missing file", priorWrite, writeCall("w2", "a.go", "y"), &domain.ToolResult{CallID: "w2", Content: "no such file or directory", IsError: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if enrich(t, c.history, c.call, c.result) {
				t.Errorf("%s should not be enriched; got %q", c.name, c.result.Content)
			}
		})
	}
}

// error_enrichment builds from the production catalogue (the config surface's path).
func TestErrorEnrichmentBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	if m := mustBuild(t, errorEnrichmentID); m.Descriptor().ID != errorEnrichmentID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, errorEnrichmentID)
	}
}
