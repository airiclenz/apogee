package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Context controls: ClearContext / Compact (the /clear, /compact seams)
// ----------------------------------------------------------------------------

func TestClearContextEmptiesConversationKeepsTurnIndex(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "hi there"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "remember 42"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if a.conv.Len() == 0 {
		t.Fatal("conversation empty after a turn; nothing to clear")
	}
	turnBefore := a.turnIndex

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	if a.conv.Len() != 0 {
		t.Errorf("conversation not cleared: Len = %d", a.conv.Len())
	}
	if a.turnIndex != turnBefore {
		t.Errorf("turnIndex changed by clear: %d → %d (the counter must keep advancing)", turnBefore, a.turnIndex)
	}
}

func TestClearContextRefusedMidExchange(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.inExchange = true // a mid-Exchange boundary — clearing here would orphan a half-run turn
	if err := a.ClearContext(); !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("ClearContext mid-exchange err = %v, want ErrInputPending", err)
	}
}

func TestCompactIsNotImplementedStub(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Compact(context.Background()); !errors.Is(err, domain.ErrCompactionNotImplemented) {
		t.Errorf("Compact err = %v, want ErrCompactionNotImplemented", err)
	}
}

// ----------------------------------------------------------------------------
// @file reference resolution (replaces noteUnresolvedFileRefs)
// ----------------------------------------------------------------------------

func TestResolveFileRefsInjectsContent(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "data.txt", "SECRET CONTENT 123")
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "look at this", FileRefs: []string{"data.txt"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := a.conv.At(0).Content // the user message the loop appended
	if !strings.Contains(got, "SECRET CONTENT 123") {
		t.Errorf("resolved file content not injected into the user message:\n%s", got)
	}
	if !strings.Contains(got, "look at this") {
		t.Errorf("original user text missing from the message:\n%s", got)
	}
}

func TestResolveFileRefsMissingRefEmitsErrorAndProceeds(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi", FileRefs: []string{"nope.txt"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("turn did not complete despite a bad ref: status = %v", res.Status)
	}
	if !errorEventContaining(sink.events, "nope.txt") {
		t.Error("missing @ref did not surface an ErrorEvent")
	}
	if got := a.conv.At(0).Content; got != "hi" {
		t.Errorf("user message = %q, want just the text (a bad ref injects nothing)", got)
	}
}

func TestResolveFileRefsEscapeRefused(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi", FileRefs: []string{"../../etc/passwd"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !errorEventContaining(sink.events, "passwd") {
		t.Error("a workspace-escaping @ref was not refused with an ErrorEvent")
	}
	if got := a.conv.At(0).Content; strings.Contains(got, "root:") {
		t.Error("escaping ref leaked host file content into the conversation")
	}
}

func TestUnresolvedSkillIDsNoted(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi", SkillIDs: []string{"foo"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !errorEventContaining(sink.events, "SkillIDs") {
		t.Error("reserved SkillIDs were consumed silently; the loop should note them")
	}
}

// errorEventContaining reports whether any emitted ErrorEvent's message contains substr.
func errorEventContaining(events []domain.Event, substr string) bool {
	for _, e := range events {
		if ee, ok := e.(domain.ErrorEvent); ok && strings.Contains(ee.Err, substr) {
			return true
		}
	}
	return false
}

func writeWorkspaceFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
