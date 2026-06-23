package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// The live-model confirmation (phase-2 detail plan §4 P2.6; the open Phase-1 live eval)
// ----------------------------------------------------------------------------
//
// TestE2ELiveModel drives the real Agent against a real local model through the same seam and
// real Model the hermetic e2e uses (newE2EEngine + runExchange) — only the endpoint changes
// from the scripted httptest server to a live OpenAI-compatible server. It holds a real
// file-edit conversation: the model streams, requests write_file, the write is approved
// through the real approval rendezvous (auto-allowed here, the headless stand-in for a human
// pressing "a"), the tool writes into a temp workspace, and the final message renders. This
// closes the one open Phase-1 runtime step (the live file-edit eval) over the product surface.
//
// It is opt-in: skipped unless APOGEE_LIVE_ENDPOINT is set, so the default suite and `make
// check` never depend on a running model. Run it against a tool-capable model with, e.g.:
//
//	APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test -race -run TestE2ELiveModel -v ./internal/tui/
//
// APOGEE_LIVE_MODEL pins the model name; left empty, the model is discovered from the server.
func TestE2ELiveModel(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live-model eval")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	model := os.Getenv("APOGEE_LIVE_MODEL")
	if model == "" {
		info, err := provider.NewClient(endpoint, "").Discover(ctx)
		if err != nil {
			t.Fatalf("discover model at %s: %v", endpoint, err)
		}
		model = info.ActiveModel
	}
	t.Logf("live run: endpoint=%s model=%s", endpoint, model)

	workspace := t.TempDir()
	bridge := NewBridge()
	h := newUIHarness()
	bridge.Bind(h)
	eng := newE2EEngine(t, endpoint, model, workspace, bridge.Sink(), bridge.Approver())

	m := step(t, newModel(ctx, eng, e2eOptions(endpoint, workspace)), tea.WindowSizeMsg{Width: 100, Height: 30})

	m, term := h.runExchange(t, ctx, m, eng,
		"Use the write_file tool to create a file named greeting.txt containing exactly: Hello, Apogee!")

	// Surface the whole conversation so a human can eyeball the live transcript.
	t.Logf("terminal: %T %+v", term, terminalResult(term))
	t.Logf("approvals resolved: %d", h.approvals)
	t.Logf("transcript:\n%s", plainTranscript(m))

	if e, ok := term.(errMsg); ok {
		t.Fatalf("live exchange returned a loop error: %v", e.Err)
	}

	// The model should have driven a write through the approval gate and a file should have
	// landed in the workspace — the live file-edit deliverable.
	if h.approvals < 1 {
		t.Errorf("no write was gated for approval; the model did not call a write tool")
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("no file was written to the workspace")
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("workspace files: %v", names)

	if !strings.Contains(plainTranscript(m), "write_file") {
		t.Errorf("transcript does not show the write_file tool call")
	}
}
