package reactions

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// HostMCP says whether the Driver root a Host serves dials MCP servers, which decides what becomes
// of a user reaction's mcp: handler on its observe lane (adr-0076-amend-1029).
type HostMCP int

const (
	// HostMCPNone is a Driver that dials no MCP server — every Firing (headless, the daemon, the
	// TUI's /schedule) and `apogee probe context`. NewHost disarms the list's mcp: entries before
	// arming, one notice per entry (DisarmUnconnectedMCP), and the executor holds no resolver.
	HostMCPNone HostMCP = iota
	// HostMCPLate is a Driver that dials its MCP set after the Host is built — the TUI session.
	// The executor resolves through an MCPSlot the Driver fills through BindMCP once the set
	// exists, and an mcp: firing before then is refused as not connected yet.
	HostMCPLate
)

// HostOptions are the facts a Host cannot derive from the reaction list: what its Runner decorates,
// where it is rooted, who it reports to, which hosts its webhooks may reach, and whether the Driver
// dials MCP servers. Every field is optional; the zero value is a root that dials no MCP server,
// reports nothing and leaves the url-safety lists open to the guard's own floor.
type HostOptions struct {
	// Inner is the sink the Runner decorates (Options.Inner).
	Inner domain.EventSink
	// Workspace is the run's workspace root: the Runner's (Options.Workspace) and the exec fence a
	// global `run:` is refused inside (DefaultExecutor).
	Workspace string
	// Schedule names the Schedule a Firing runs for (Options.Schedule); nil everywhere else.
	Schedule *ScheduleRef
	// Report receives the MCP disarm notices and every Runner failure line (Options.Report), and
	// Install hands it on as Config.Report; nil drops them.
	Report func(string)
	// PinMoved is the Driver's re-ask cue (domain.Config.PinMoved): the MCP half's under
	// HostMCPLate, and Install hands it on as Config.PinMoved; nil drops it.
	PinMoved func()
	// URLAllowHosts and URLDenyHosts are the url-safety lists every webhook Reaction posts under —
	// the same two lists the network tools are guarded by (security.NewURLGuard).
	URLAllowHosts, URLDenyHosts []string
	// MCP says whether the Driver dials MCP servers; the zero value is HostMCPNone.
	MCP HostMCP
}

// Host is one Driver root's observe lane, owned whole: the MCP disarm and its notices, the
// url-safety guard and the fenced DefaultExecutor, the project-run and MCP late binds, and the
// Runner with its close grace (ADR 0073, ADR 0076 A8). Each Driver root builds its own — one per
// Firing, one per TUI session — and composes its domain.Config through Install, so no Driver can
// arm the lane without the fence, the guard or the one notice per entry ("nothing safety-relevant
// may exist only in one Driver's surface", glossary: Driver).
//
// The Driver keeps two things. Its sink wrapping: a Driver that renders an Event itself wraps
// Config.Events after Install, which keeps its renderer outermost. And Config.MCP: the engine reads
// a nil resolver as a Driver that dials no server, where an mcp: entry is silent, and an unbound
// one as a connection not made yet, which it reports — so only the Driver that dials may set it.
type Host struct {
	reactions   []domain.Reaction
	lanes       domain.Generation
	runner      *Runner
	projectRuns *ProjectRunSlot
	// mcp is the late bind the executor resolves through; nil under HostMCPNone.
	mcp      *MCPSlot
	report   func(string)
	pinMoved func()
}

// NewHost builds the observe lane over list, the root's whole resolved `reactions:` list, taken as
// validated like New's. Under HostMCPNone it first disarms the list's mcp: entries, telling
// o.Report one notice per entry; the list that remains is the one Reactions answers and both lanes
// are divided from. It then builds the Runner over the observe lane, with a DefaultExecutor fenced
// at o.Workspace, posting through the guard over o's url-safety lists, and late-bound to the
// project-run slot and, under HostMCPLate, to the MCP slot and o.PinMoved.
//
// It fails, with nothing left running, when o.MCP is not a known HostMCP or when the Runner cannot
// be built (New).
func NewHost(list []domain.Reaction, o HostOptions) (*Host, error) {
	h := &Host{projectRuns: &ProjectRunSlot{}, report: o.Report, pinMoved: o.PinMoved}
	var executorPinMoved func()
	switch o.MCP {
	case HostMCPNone:
		list = DisarmUnconnectedMCP(list, o.Report)
	case HostMCPLate:
		h.mcp = &MCPSlot{}
		executorPinMoved = o.PinMoved
	default:
		return nil, fmt.Errorf("reactions: unknown HostMCP %d", o.MCP)
	}
	h.reactions = list
	h.lanes = domain.LanesOf(list)

	guard := security.NewURLGuard(o.URLAllowHosts, o.URLDenyHosts)
	runner, err := New(h.lanes.Observe, Options{
		Inner:     o.Inner,
		Workspace: o.Workspace,
		Schedule:  o.Schedule,
		Report:    o.Report,
		Exec: DefaultExecutor(
			o.Workspace, guard, h.projectRuns.Runner(), h.mcp.Resolver(), executorPinMoved,
		),
	})
	if err != nil {
		return nil, err
	}
	h.runner = runner
	return h, nil
}

// Reactions is the settled list: the one NewHost was handed, less the mcp: entries a HostMCPNone
// root disarmed. It is what a Driver arms the sync lane from and seeds its Generation with.
func (h *Host) Reactions() []domain.Reaction {
	return h.reactions
}

// Lanes is the settled list divided into its two lanes (domain.LanesOf): Observe, the list the
// Runner was built over, and Sync. Every other member is zero; the Floor, Bypass and notice
// members are the Driver's to fill.
func (h *Host) Lanes() domain.Generation {
	return h.lanes
}

// Runner is the observe lane's Runner, which Install sets as Config.Events and
// Config.ObserveRunner.
func (h *Host) Runner() *Runner {
	return h.runner
}

// BindMCP binds the resolver over the Driver's live MCP set, replacing any earlier one, so the
// observe lane's mcp: entries resolve through it from the next firing on. It is a no-op under
// HostMCPNone, whose executor holds no resolver.
func (h *Host) BindMCP(resolve domain.MCPResolver) {
	h.mcp.Bind(resolve)
}

// MCPResolver is the resolver the observe lane's executor holds: the MCP slot's, which refuses as
// not connected yet until BindMCP, or nil under HostMCPNone.
func (h *Host) MCPResolver() domain.MCPResolver {
	return h.mcp.Resolver()
}

// Install composes the observe lane into cfg: Events and ObserveRunner become the Runner, Observe
// the observe lane it was built over, and BindProjectRun the project-run slot's Bind, so the Agent
// the Config builds binds its fenced runner where the executor reaches it. Report and PinMoved are
// set only when HostOptions gave them, so a nil one keeps cfg's own. It never touches MCP,
// Reactions or Sync: MCP is the Driver's (see Host), and the sync lane is armed from Lanes.
func (h *Host) Install(cfg *domain.Config) {
	cfg.Events = h.runner
	cfg.ObserveRunner = h.runner
	cfg.Observe = h.lanes.Observe
	cfg.BindProjectRun = h.projectRuns.Bind
	if h.report != nil {
		cfg.Report = h.report
	}
	if h.pinMoved != nil {
		cfg.PinMoved = h.pinMoved
	}
}

// Close drains the Runner within CloseGrace, the one grace every root gives its Reactions, then
// cancels what is still running. It returns the Runner's error when a Reaction was still running
// at the deadline (Runner.Close), and is idempotent.
func (h *Host) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), CloseGrace)
	defer cancel()
	return h.runner.Close(ctx)
}
