package reactions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/testtime"
)

// hostList is the four-entry list the MCP cases arm: an advise, an observe and a gate mcp: entry
// beside a command entry.
func hostList() []domain.Reaction {
	return []domain.Reaction{
		mcpEntry("advice", domain.ClassAdvise),
		mcpHook("record", domain.MCPHandler{Server: "llm", Tool: "status"}),
		mcpEntry("guard", domain.ClassGate),
		commandEntry("log"),
	}
}

// newTestHost builds a Host rooted in a fresh workspace and closes it when the test ends.
func newTestHost(t *testing.T, list []domain.Reaction, o HostOptions) *Host {
	t.Helper()
	if o.Workspace == "" {
		o.Workspace = t.TempDir()
	}
	host, err := NewHost(list, o)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return host
}

// awaitLines waits until log holds want lines, failing the test past the budget.
func awaitLines(t *testing.T, log *reportLog, want int) {
	t.Helper()
	testtime.Eventually(t, awaitDeadline, func() bool { return len(log.all()) >= want },
		"the reaction's report line")
}

// TestHostMCPNoneDisarmsTheListWithOneNoticePerEntry — a root that dials no MCP server drops the
// advise and observe mcp: entries, keeps the gate and the command, says each pinned notice once,
// and holds no resolver to bind.
func TestHostMCPNoneDisarmsTheListWithOneNoticePerEntry(t *testing.T) {
	t.Parallel()
	const (
		inertAdvice = `reaction "advice": its mcp: handler is inert here — MCP servers are not connected in this run`
		inertRecord = `reaction "record": its mcp: handler is inert here — MCP servers are not connected in this run`
		refusedGate = `reaction "guard": its mcp: gate cannot run here — MCP servers are not connected in this ` +
			`run, so every call it gates is refused`
	)
	log := &reportLog{}

	host := newTestHost(t, hostList(), HostOptions{Report: log.add})

	if got, want := log.all(), []string{inertAdvice, inertRecord, refusedGate}; !slices.Equal(got, want) {
		t.Errorf("said %q, want %q", got, want)
	}
	if got, want := reactionIDs(host.Reactions()), []string{"guard/gate", "log/observe"}; !slices.Equal(got, want) {
		t.Errorf("Reactions() = %q, want %q", got, want)
	}
	lanes := host.Lanes()
	if got, want := reactionIDs(lanes.Observe), []string{"log/observe"}; !slices.Equal(got, want) {
		t.Errorf("Lanes().Observe = %q, want %q", got, want)
	}
	if got, want := reactionIDs(lanes.Sync), []string{"guard/gate"}; !slices.Equal(got, want) {
		t.Errorf("Lanes().Sync = %q, want %q", got, want)
	}
	host.BindMCP((&mcpResolverStub{}).resolve)
	if host.MCPResolver() != nil {
		t.Error("MCPResolver() answered a resolver under HostMCPNone; a root that dials nothing holds none")
	}
}

// TestHostMCPLateResolvesThroughTheBoundResolver — a root that dials later keeps every entry; an
// observe mcp: firing before BindMCP is refused as not connected yet, and after it the bound
// resolver is called.
func TestHostMCPLateResolvesThroughTheBoundResolver(t *testing.T) {
	t.Parallel()
	log := &reportLog{}
	host := newTestHost(t, hostList(), HostOptions{Report: log.add, MCP: HostMCPLate})

	if got, want := reactionIDs(host.Reactions()), reactionIDs(hostList()); !slices.Equal(got, want) {
		t.Errorf("Reactions() = %q, want every entry %q", got, want)
	}
	if host.MCPResolver() == nil {
		t.Fatal("MCPResolver() is nil under HostMCPLate; the executor must hold the slot's resolver")
	}

	host.Runner().Emit(turnEvent(1))
	awaitLines(t, log, 1)
	want := "reaction record (turn-finished): MCP servers are not connected yet"
	if got := log.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("reported %q, want exactly %q", got, want)
	}

	stub := &mcpResolverStub{}
	host.BindMCP(stub.resolve)
	host.Runner().Emit(turnEvent(2))
	testtime.Eventually(t, awaitDeadline, func() bool {
		_, _, args := stub.snapshot()
		return len(args) == 1
	}, "the bound resolver's call")

	resolved, _, _ := stub.snapshot()
	if !slices.Equal(resolved, []string{"llm/status"}) {
		t.Errorf("resolved %q, want llm/status once", resolved)
	}
}

// TestHostProjectRunIsLateBoundThroughInstall — an adopted repo `run:` firing before the Agent
// binds is refused with the pinned text; once Install has handed the Config the slot's Bind and the
// Agent binds through it, the bound runner gets the firing.
func TestHostProjectRunIsLateBoundThroughInstall(t *testing.T) {
	t.Parallel()
	entry := commandHook("lint", TurnFinished)
	entry.Project = true
	log := &reportLog{}
	host := newTestHost(t, []domain.Reaction{entry}, HostOptions{Report: log.add})

	host.Runner().Emit(turnEvent(1))
	awaitLines(t, log, 1)
	want := "reaction lint (turn-finished): refusing to run a project reaction before its session's engine is built"
	if got := log.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("reported %q, want exactly %q", got, want)
	}

	var cfg domain.Config
	host.Install(&cfg)
	var ran atomic.Int64
	cfg.BindProjectRun(func(_ context.Context, r domain.Reaction, _ string, _ []string) error {
		if r.ID == entry.ID {
			ran.Add(1)
		}
		return nil
	})
	host.Runner().Emit(turnEvent(2))

	testtime.Eventually(t, awaitDeadline, func() bool { return ran.Load() == 1 }, "the bound project runner")
}

// TestHostInstallFillsTheObserveLaneKeysOnly — Install sets Events, ObserveRunner, Observe and
// BindProjectRun, sets Report and PinMoved only when the options gave them, and leaves MCP,
// Reactions and Sync as the Driver put them.
func TestHostInstallFillsTheObserveLaneKeysOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		withCallers  bool
		wantReport   string
		wantPinMoved string
	}{
		{name: "options without Report and PinMoved keep the Config's", wantReport: "config", wantPinMoved: "config"},
		{name: "options with Report and PinMoved replace the Config's", withCallers: true,
			wantReport: "host", wantPinMoved: "host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var reportedBy, cuedBy string
			o := HostOptions{}
			if tt.withCallers {
				o.Report = func(string) { reportedBy = "host" }
				o.PinMoved = func() { cuedBy = "host" }
			}
			host := newTestHost(t, []domain.Reaction{commandEntry("log")}, o)
			mcp := &mcpResolverStub{}
			cfg := domain.Config{
				MCP:       mcp.resolve,
				Reactions: []domain.Reaction{commandEntry("engine")},
				Sync:      []domain.Reaction{mcpEntry("guard", domain.ClassGate)},
				Report:    func(string) { reportedBy = "config" },
				PinMoved:  func() { cuedBy = "config" },
			}

			host.Install(&cfg)

			if cfg.Events != domain.EventSink(host.Runner()) {
				t.Errorf("Events = %v, want the host's Runner", cfg.Events)
			}
			if cfg.ObserveRunner != domain.ObserveRunner(host.Runner()) {
				t.Errorf("ObserveRunner = %v, want the host's Runner", cfg.ObserveRunner)
			}
			if got := reactionIDs(cfg.Observe); !slices.Equal(got, []string{"log/observe"}) {
				t.Errorf("Observe = %q, want the host's observe lane", got)
			}
			if cfg.BindProjectRun == nil {
				t.Error("BindProjectRun is nil, want the project-run slot's Bind")
			}
			cfg.Report("a line")
			cfg.PinMoved()
			if reportedBy != tt.wantReport || cuedBy != tt.wantPinMoved {
				t.Errorf("Report from %q, PinMoved from %q, want %q and %q",
					reportedBy, cuedBy, tt.wantReport, tt.wantPinMoved)
			}
			if _, err := cfg.MCP("llm", "status", false); err != nil {
				t.Errorf("MCP refused: %v, want the Driver's own resolver", err)
			}
			if resolved, _, _ := mcp.snapshot(); !slices.Equal(resolved, []string{"llm/status"}) {
				t.Errorf("the Config's MCP resolved %q, want the Driver's own stub", resolved)
			}
			if got := reactionIDs(cfg.Reactions); !slices.Equal(got, []string{"engine/observe"}) {
				t.Errorf("Reactions = %q, want it untouched", got)
			}
			if got := reactionIDs(cfg.Sync); !slices.Equal(got, []string{"guard/gate"}) {
				t.Errorf("Sync = %q, want it untouched", got)
			}
		})
	}
}

// TestHostWebhookPostsThroughTheURLDenyList — a webhook to a host the options' deny list closes is
// refused before any dial.
func TestHostWebhookPostsThroughTheURLDenyList(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	entry := domain.Reaction{
		ID:      "ping",
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []Event{TurnFinished},
		Handler: domain.WebhookHandler{URL: server.URL},
		Timeout: time.Minute,
	}
	log := &reportLog{}
	host := newTestHost(t, []domain.Reaction{entry}, HostOptions{
		Report: log.add, URLDenyHosts: []string{"127.0.0.1"},
	})

	host.Runner().Emit(turnEvent(1))
	awaitLines(t, log, 1)

	if got := log.all(); !strings.Contains(got[0], security.ErrURLBlocked.Error()) {
		t.Errorf("reported %q, want a url-safety refusal", got)
	}
	if hits.Load() != 0 {
		t.Errorf("the endpoint was called %d times; a denied host must not be dialled", hits.Load())
	}
}

// TestHostCloseOnAnIdleHostReturnsNil — a host with nothing in flight drains cleanly.
func TestHostCloseOnAnIdleHostReturnsNil(t *testing.T) {
	t.Parallel()
	host, err := NewHost([]domain.Reaction{commandEntry("log")}, HostOptions{Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	if err := host.Close(); err != nil {
		t.Errorf("Close = %v, want nil", err)
	}
}

// TestHostRefusesAnUnknownMCPMode — an HostMCP value outside the two known ones fails construction.
func TestHostRefusesAnUnknownMCPMode(t *testing.T) {
	t.Parallel()

	host, err := NewHost(nil, HostOptions{MCP: HostMCPLate + 1})

	if err == nil || host != nil {
		t.Errorf("NewHost = %v, %v; want an error and no host", host, err)
	}
}
