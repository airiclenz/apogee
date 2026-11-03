package apogee_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/testtime"
)

// The tests in this file reach the Reaction host through the public facade alone: an embedder
// that never imports internal/* must get the same MCP notice and exec fence as the binary.

// hostReportLog collects the lines a ReactionHost reports; its Runner reports from worker
// goroutines, so every access is locked.
type hostReportLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *hostReportLog) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}

func (l *hostReportLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.lines)
}

// newFacadeHost builds a ReactionHost through the facade, failing the test on a refused list,
// and closes it when the test ends.
func newFacadeHost(t *testing.T, list []apogee.Reaction, o apogee.ReactionHostOptions) *apogee.ReactionHost {
	t.Helper()
	for _, entry := range list {
		if err := entry.Validate(); err != nil {
			t.Fatalf("the test's own reaction %q is invalid: %v", entry.ID, err)
		}
	}
	host, err := apogee.NewReactionHost(list, o)
	if err != nil {
		t.Fatalf("NewReactionHost: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return host
}

// TestReactionHostFromTheFacadeDisarmsAnMCPEntryWithOneNotice — under ReactionHostMCPNone a user
// advise entry with an mcp: handler is reported inert once and kept off the sync lane.
func TestReactionHostFromTheFacadeDisarmsAnMCPEntryWithOneNotice(t *testing.T) {
	t.Parallel()
	const inert = `reaction "advice": its mcp: handler is inert here — MCP servers are not connected in this run`
	entry := apogee.Reaction{
		ID:      "advice",
		Origin:  apogee.OriginUser,
		Class:   apogee.ClassAdvise,
		On:      []apogee.Moment{apogee.MomentPostToolResult},
		Handler: apogee.MCPHandler{Server: "llm", Tool: "status"},
		Timeout: time.Second,
	}
	log := &hostReportLog{}

	host := newFacadeHost(t, []apogee.Reaction{entry}, apogee.ReactionHostOptions{
		Workspace: t.TempDir(),
		Report:    log.add,
		MCP:       apogee.ReactionHostMCPNone,
	})

	if got, want := log.all(), []string{inert}; !slices.Equal(got, want) {
		t.Errorf("reported %q, want %q", got, want)
	}
	for _, armed := range host.Lanes().Sync {
		if armed.ID == entry.ID {
			t.Errorf("the disarmed entry %q is still on the sync lane", entry.ID)
		}
	}
}

// TestReactionHostFromTheFacadeFencesARunInsideTheWorkspace — a user observe argv run: naming a
// program inside the workspace is refused by the stock executor's exec fence, and never runs.
func TestReactionHostFromTheFacadeFencesARunInsideTheWorkspace(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the planted program is a sh script; Windows has no POSIX shell to run it with")
	}
	workspace := t.TempDir()
	marker := filepath.Join(workspace, "it-ran")
	program := filepath.Join(workspace, "hook.sh")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatalf("write the program: %v", err)
	}
	entry := apogee.Reaction{
		ID:      "planted",
		Origin:  apogee.OriginUser,
		Class:   apogee.ClassObserve,
		On:      []apogee.Moment{apogee.MomentTurnFinished},
		Handler: apogee.ArgvHandler{Argv: []string{program}},
		Timeout: 10 * time.Second,
	}
	log := &hostReportLog{}
	host := newFacadeHost(t, []apogee.Reaction{entry}, apogee.ReactionHostOptions{
		Workspace: workspace,
		Report:    log.add,
		MCP:       apogee.ReactionHostMCPNone,
	})

	host.Runner().Emit(apogee.TurnEvent{Status: apogee.StatusTurnComplete})
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool { return len(log.all()) > 0 },
		"the fenced reaction's report line")

	if got := log.all(); !strings.Contains(got[0], "refusing to run") {
		t.Errorf("reported %q, want a refusal of the program inside the workspace", got)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused program ran anyway: %v", err)
	}
}
