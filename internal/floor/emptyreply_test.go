package floor

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// An empty reply (no text, no tool calls) mid-task draws the sim's completion-check nudge, which
// the engine re-streams the request with as a role-safe user correction. The hard attempt cap that
// stops an always-empty model is the loop's maxPostResponseRetries (verified in internal/agent); at
// the guard level the guarantee is that the trigger yields the verbatim nudge.
func TestRecoverEmptyNudgesOnEmptyReply(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("please implement the parser"),
		assistantText("Starting on it."),
	}
	nudge, ok := RecoverEmpty(guardResponse(history, guardMenu(), "")) // empty: no text, no calls

	if !ok {
		t.Fatalf("ok = false, want the guard to fire on an empty reply mid-task")
	}
	if nudge != completionCheckNudge {
		t.Errorf("nudge = %q, want the sim's completion-check nudge verbatim", nudge)
	}
}

// The guard stands down for every response that is not an empty reply worth recovering.
func TestRecoverEmptyNoOpCases(t *testing.T) {
	t.Parallel()
	// A progress-bearing, action-request history so only the tweaked condition suppresses the fire.
	progress := []domain.Message{
		userMsg("build the feature"),
		assistantText("On it."),
	}
	tests := []struct {
		name string
		resp *domain.Response
	}{
		{
			name: "response has text (the enforcer's domain, not empty)",
			resp: guardResponse(progress, guardMenu(), "Here is what I found."),
		},
		{
			name: "response has a tool call",
			resp: guardResponse(progress, guardMenu(), "", readCall("c1", "main.go")),
		},
		{
			name: "no tools were offered",
			resp: guardResponse(progress, nil, ""),
		},
		{
			name: "no user message to recover for",
			resp: guardResponse([]domain.Message{assistantText("hello")}, guardMenu(), ""),
		},
		{
			name: "no recent progress — spinning on one file past the early-turn grace",
			resp: guardResponse([]domain.Message{
				userMsg("do it"),
				assistantCall(readCall("c1", "a.go")),
				assistantCall(readCall("c2", "a.go")),
				assistantCall(readCall("c3", "a.go")),
				assistantCall(readCall("c4", "a.go")),
			}, guardMenu(), ""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			nudge, ok := RecoverEmpty(tt.resp)
			if ok || nudge != "" {
				t.Errorf("RecoverEmpty = (%q, %v), want the no-op (\"\", false)", nudge, ok)
			}
		})
	}
}

// The guard counts an apogee EDIT tool as a file write, so an empty reply after an edit is progress
// worth recovering even past the early-turn grace and with fewer than two distinct reads — the same
// branch a write_file would take. Without the edit the identical spinning-reads history has no
// progress and the guard is inert; the edit is the only difference, so it is what drives the
// isFileMutatingTool write branch. (Moved here with the guard: this is the write-detection pin the
// 2026-08-10 edit-tool gap called for.)
func TestRecoverEmptyTreatsRecentEditAsProgress(t *testing.T) {
	t.Parallel()
	// >3 assistant turns (past the grace) re-reading one file (fewer than two distinct paths): no
	// progress on its own, so the guard is inert — the control the edit is measured against.
	spinning := []domain.Message{
		userMsg("do it"),
		assistantCall(readCall("c1", "a.go")),
		assistantCall(readCall("c2", "a.go")),
		assistantCall(readCall("c3", "a.go")),
		assistantCall(readCall("c4", "a.go")),
	}
	if _, ok := RecoverEmpty(guardResponse(spinning, guardMenu(), "")); ok {
		t.Fatal("control fired: spinning reads of one file are not progress")
	}

	for _, tool := range []string{"edit_existing_file", "single_find_and_replace"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			withEdit := append(spinning[:len(spinning):len(spinning)],
				assistantCall(domaintest.Call("e1", tool, map[string]string{"path": "a.go"})),
			)
			nudge, ok := RecoverEmpty(guardResponse(withEdit, guardMenu(), ""))
			if !ok || nudge != completionCheckNudge {
				t.Errorf("RecoverEmpty = (%q, %v), want the nudge: a recent %s is progress worth recovering", nudge, ok, tool)
			}
		})
	}
}
