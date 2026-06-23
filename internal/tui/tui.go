package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

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
// were installed in the Agent's Config, and the resolved Options. Run builds the Model and
// the Bubble Tea program, then binds the program to br (br.Bind) *before* program.Run()
// starts the loop — so the late-bound event and approval delegates reach the live program
// the moment the first worker emits (phase-2 detail plan §3 C2/C3; ADR 0011). The program
// context is ctx, so a program-wide shutdown also cancels an in-flight Exchange (C4).
func Run(ctx context.Context, eng Engine, br *Bridge, opts Options) error {
	program := tea.NewProgram(newModel(ctx, eng, opts), tea.WithContext(ctx))
	// Bind before Run: the program exists now, and the first Send cannot occur until a
	// worker is launched, which only happens after the user submits into the running loop.
	br.Bind(program)
	_, err := program.Run()
	return err
}
