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

func TestCompactSummarizesAndReplacesHistoryKeepingPrefix(t *testing.T) {
	up := &recordingResponder{reply: "COMPACTED-SUMMARY"}
	a, err := newAgent(baseConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Two exchanges → conv = [user "task one", assistant, user "task two", assistant] (Len 4).
	for _, text := range []string{"task one", "task two"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step: %v", err)
		}
	}
	if a.conv.Len() != 4 {
		t.Fatalf("precondition: conv.Len() = %d, want 4", a.conv.Len())
	}

	if skipped, err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	} else if skipped {
		t.Fatal("Compact skipped a conversation with 4 messages past the prefix; want a fold")
	}

	// History folds to the protected prefix (first user message) + one assistant summary.
	if a.conv.Len() != 2 {
		t.Fatalf("conv.Len() = %d after compaction, want 2 (prefix + summary)", a.conv.Len())
	}
	if got := a.conv.At(0); got.Role != domain.RoleUser || got.Content != "task one" {
		t.Errorf("protected prefix not preserved: %+v", got)
	}
	sum := a.conv.At(1)
	if sum.Role != domain.RoleAssistant || !strings.Contains(sum.Content, "COMPACTED-SUMMARY") {
		t.Errorf("summary message wrong: %+v", sum)
	}
	// The summary call carried the summarizer system prompt + the rendered transcript.
	last := up.last
	if len(last.Messages) == 0 || last.Messages[0].Role != "system" {
		t.Fatalf("summary request did not lead with a system prompt: %+v", last.Messages)
	}
	joined := last.Messages[0].Content + "\n" + last.Messages[len(last.Messages)-1].Content
	for _, want := range []string{"compacting", "task one", "task two"} {
		if !strings.Contains(joined, want) {
			t.Errorf("summary request missing %q:\n%s", want, joined)
		}
	}
}

func TestCompactRefusedMidExchange(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.inExchange = true // a mid-Exchange boundary — compacting here would orphan a half-run turn
	if _, err := a.Compact(context.Background()); !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("Compact mid-exchange err = %v, want ErrInputPending", err)
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

// fakeSkillResolver is a deterministic domain.SkillResolver for the loop tests: it returns the
// skills it knows (by ID), in the requested order, skipping unknowns — the catalog's contract.
type fakeSkillResolver struct {
	skills map[string]domain.ResolvedSkill
}

func (f fakeSkillResolver) ResolveSkills(ids []string) []domain.ResolvedSkill {
	var out []domain.ResolvedSkill
	for _, id := range ids {
		if s, ok := f.skills[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func TestResolveSkillRefsInjectsBodyBeforeText(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Skills = fakeSkillResolver{skills: map[string]domain.ResolvedSkill{
		"review": {ID: "review", DisplayName: "Code Review", Body: "REVIEW INSTRUCTIONS"},
	}}
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "please look", SkillIDs: []string{"review"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := a.conv.At(0).Content
	if !strings.Contains(got, "REVIEW INSTRUCTIONS") {
		t.Errorf("skill body not injected into the user message:\n%s", got)
	}
	if !strings.Contains(got, "Code Review") {
		t.Errorf("skill display name not in the labeled block:\n%s", got)
	}
	// The skill block must precede the user's text (per-turn instructions lead the message).
	if strings.Index(got, "REVIEW INSTRUCTIONS") > strings.Index(got, "please look") {
		t.Errorf("skill body should be prepended before the user text:\n%s", got)
	}
}

func TestResolveSkillRefsUnknownIDNoted(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Skills = fakeSkillResolver{skills: map[string]domain.ResolvedSkill{
		"known": {ID: "known", DisplayName: "Known", Body: "KNOWN BODY"},
	}}
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi", SkillIDs: []string{"known", "ghost"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !errorEventContaining(sink.events, "ghost") {
		t.Error("an unknown attached skill ID did not surface an ErrorEvent")
	}
	if got := a.conv.At(0).Content; !strings.Contains(got, "KNOWN BODY") {
		t.Errorf("the known skill was dropped alongside the unknown one:\n%s", got)
	}
}

func TestResolveSkillRefsNilResolverGracefulDrop(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "ok"}) // no Config.Skills
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi", SkillIDs: []string{"foo"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("turn did not complete despite an unresolved skill: status = %v", res.Status)
	}
	if !errorEventContaining(sink.events, "skill") {
		t.Error("attached skills with no resolver were consumed silently; the loop should note them")
	}
	if got := a.conv.At(0).Content; got != "hi" {
		t.Errorf("user message = %q, want just the text (an unresolved skill injects nothing)", got)
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
