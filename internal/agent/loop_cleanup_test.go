package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// UsageEvent emission hop (loop.go streamResponse — item 8 test gap)
// ----------------------------------------------------------------------------

// usageResponder is echoResponder that also attaches token accounting to its terminal Done —
// the fake for asserting the Delta.Usage → UsageEvent hop.
type usageResponder struct {
	content string
	usage   provider.Usage
}

func (r usageResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		if r.content != "" && !yield(provider.Delta{Kind: provider.DeltaContent, Content: r.content}) {
			return
		}
		u := r.usage
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "stop", Usage: &u})
	}
}

func firstUsageEvent(events []domain.Event) (domain.UsageEvent, bool) {
	for _, e := range events {
		if ue, ok := e.(domain.UsageEvent); ok {
			return ue, true
		}
	}
	return domain.UsageEvent{}, false
}

// TestStreamEmitsUsageEventFromDelta pins the accounting hop: a terminal Done carrying Usage
// becomes a Depth-0 UsageEvent whose fields mirror the provider's counts (loop.go:308).
func TestStreamEmitsUsageEventFromDelta(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), usageResponder{
		content: "hello",
		usage:   provider.Usage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19},
	})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	ue, ok := firstUsageEvent(sink.events)
	if !ok {
		t.Fatal("no UsageEvent emitted for a Done that carried Usage")
	}
	if ue.PromptTokens != 12 || ue.CompletionTokens != 7 || ue.TotalTokens != 19 {
		t.Errorf("UsageEvent counts = {prompt %d, completion %d, total %d}, want {12, 7, 19}",
			ue.PromptTokens, ue.CompletionTokens, ue.TotalTokens)
	}
	if ue.Depth != 0 {
		t.Errorf("UsageEvent Depth = %d, want 0 (top-level agent)", ue.Depth)
	}
}

// TestStreamEmitsNoUsageEventWhenServerOmitsIt pins the nil-Usage arm: a server that omits
// token accounting (echoResponder's Done carries no Usage) fires no UsageEvent — the gauge's
// zero state, not a bogus all-zero event (loop.go:304 — the `if u := delta.Usage; u != nil`).
func TestStreamEmitsNoUsageEventWhenServerOmitsIt(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "hi"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if hasEvent[domain.UsageEvent](sink.events) {
		t.Error("a UsageEvent was emitted for a Done that carried no Usage")
	}
}

// ----------------------------------------------------------------------------
// Combined skills → files → text injection order (loop.go step — item 8 test gap)
// ----------------------------------------------------------------------------

// TestStepInjectsSkillsThenFilesThenText proves the opening Turn assembles one user message in
// the documented order — attached-skill blocks, then @file blocks, then the human's text —
// when all three ride a single Submit (loop.go: skillBlocks + refs + Text). The per-path order
// is covered elsewhere; this pins the composition of all three at once.
func TestStepInjectsSkillsThenFilesThenText(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "data.txt", "FILE_MARKER_CONTENT")
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = dir
	cfg.Skills = fakeSkillResolver{skills: map[string]domain.ResolvedSkill{
		"review": {ID: "review", DisplayName: "Code Review", Body: "SKILL_MARKER_BODY"},
	}}
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{
		Text:     "TEXT_MARKER",
		SkillIDs: []string{"review"},
		FileRefs: []string{"data.txt"},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := a.conv.At(0).Content
	iSkill := strings.Index(got, "SKILL_MARKER_BODY")
	iFile := strings.Index(got, "FILE_MARKER_CONTENT")
	iText := strings.Index(got, "TEXT_MARKER")
	if iSkill < 0 || iFile < 0 || iText < 0 {
		t.Fatalf("a marker is missing from the assembled message (skill=%d file=%d text=%d):\n%s",
			iSkill, iFile, iText, got)
	}
	if !(iSkill < iFile && iFile < iText) {
		t.Errorf("injection order = skill@%d file@%d text@%d, want skill < file < text:\n%s",
			iSkill, iFile, iText, got)
	}
}

// ----------------------------------------------------------------------------
// @file oversize refusal (loop.go readFileRef — item 8 test gap)
// ----------------------------------------------------------------------------

// TestReadFileRefRefusesOversizeRef proves an @ref past the size cap is refused (surfaced as a
// loop ErrorEvent, injecting nothing) — and, per the item-8 fix, the refusal is decided by a
// stat BEFORE the read, so the huge file is never materialized. A sparse file (Truncate, no
// bytes written) exercises the cap without a 10 MiB write.
func TestReadFileRefRefusesOversizeRef(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(maxRefFileBytes + 1); err != nil { // sparse: logical size past the cap
		t.Fatalf("truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "look", FileRefs: []string{"big.bin"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("turn did not complete despite an oversized ref: status = %v", res.Status)
	}
	if !errorEventContaining(sink.events, "too large") {
		t.Error("an oversized @ref did not surface a 'too large' ErrorEvent")
	}
	if got := a.conv.At(0).Content; got != "look" {
		t.Errorf("user message = %q, want just the text (an oversized ref injects nothing)", got)
	}
}
