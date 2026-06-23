package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The engine seam (phase-2 detail plan §3 C5)
// ----------------------------------------------------------------------------

// Engine is the narrow, local view of the agent the TUI drives. It is satisfied by
// *agent.Agent (= *apogee.Agent), but the TUI depends only on this interface so it
// never imports the root module path (the ADR-0010 invariant) and stays unit-testable
// with a fake engine. The worker goroutine is the only caller of these methods, which
// preserves the Agent's single-goroutine contract (phase-2 detail plan §3 C1).
type Engine interface {
	// Submit enqueues user input to begin or continue an Exchange.
	Submit(domain.UserInput) error
	// Step advances the loop one Turn and returns at a quiescent boundary.
	Step(context.Context) (domain.StepResult, error)
	// Snapshot captures the serializable conversation state at a boundary.
	Snapshot() (domain.Session, error)
	// Mode reports the Agent's autonomy mode (for the status line).
	Mode() domain.Mode
	// Close releases the Agent's resources.
	Close() error
}

// ----------------------------------------------------------------------------
// Wiring values the binary resolves and the TUI renders
// ----------------------------------------------------------------------------

// Options carries the display and wiring values the binary resolves (the composition
// root, cmd/apogee) and the TUI renders in its status line but cannot read off the
// Engine — the model, endpoint, autonomy mode, bypass flag, and workspace root.
type Options struct {
	Model     string
	Endpoint  string
	Mode      domain.Mode
	Bypass    bool
	Workspace string
}

// ----------------------------------------------------------------------------
// Entry point
// ----------------------------------------------------------------------------

// Run launches the interactive terminal UI over eng. It is the single entry point the
// binary calls: cmd/apogee hands it the constructed Agent, the Bridge whose Sink/Approver
// were installed in the Agent's Config, and the resolved Options. Run creates the Bubble
// Tea program and binds it to br (br.Bind) before any worker can run, so the late-bound
// event and approval delegates reach the live program (phase-2 detail plan §3 C2/C3).
//
// P2.1 lands the concurrency seam underneath this entry point — the teaSink event bridge,
// the uiApprover rendezvous, and the worker driver (phase-2 detail plan §3 C1–C5), all
// proven under -race against a stub program. The Bubble Tea model/update/view that drives
// them lands in P2.2–P2.4; until then Run binds nothing and is a clean no-op that reports
// the build state and quits, so the wired binary constructs and exits cleanly without
// pretending to be interactive.
func Run(ctx context.Context, eng Engine, br *Bridge, opts Options) error {
	_ = ctx
	_ = eng
	_ = br // P2.2 creates the tea.Program and calls br.Bind(program) before running it.
	fmt.Fprintf(
		os.Stdout,
		"apogee: configured for %s @ %s (mode %s) — the interactive TUI lands in P2.2.\n",
		opts.Model,
		opts.Endpoint,
		opts.Mode,
	)
	return nil
}
