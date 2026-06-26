package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
)

// SkillCatalog is the read-only view of the discovered skills the TUI needs: the full sorted
// list for the /skill picker (List) and a by-id lookup for an attached chip's label (Get). It
// is satisfied by *skills.Catalog; the TUI depends only on this interface so it stays
// unit-testable with a fake, and — being an interface — it is a reference header safe to hold
// in the value-copied Model (ADR 0011). A nil catalog means no skills are wired; every reader
// guards for it.
type SkillCatalog interface {
	List() []skills.Skill
	Get(id string) (skills.Skill, bool)
}

// ----------------------------------------------------------------------------
// The engine seam (phase-2 detail plan §3 C5)
// ----------------------------------------------------------------------------

// Engine is the narrow, local view of the agent the TUI drives. It is satisfied by
// *agent.Agent (= *apogee.Agent), but the TUI depends only on this interface so it
// never imports the root module path (the ADR-0010 invariant) and stays unit-testable
// with a fake engine. The worker goroutine is the only caller of the Exchange-driving
// methods (Submit/Step); ClearContext/Compact are driven from the Update goroutine but
// only at idle, when no worker runs — so the single-driver contract holds (phase-2 detail
// plan §3 C1).
type Engine interface {
	// Submit enqueues user input to begin or continue an Exchange.
	Submit(domain.UserInput) error
	// Step advances the loop one Turn and returns at a quiescent boundary.
	Step(context.Context) (domain.StepResult, error)
	// Snapshot captures the serializable conversation state at a boundary.
	Snapshot() (domain.Session, error)
	// ClearContext drops the model's conversation history (the /clear command); the
	// host's visible transcript is unaffected. Called only at idle (no worker running).
	ClearContext() error
	// Compact triggers generative Compaction on demand (the /compact command); a stub
	// today returning domain.ErrCompactionNotImplemented. Called only at idle.
	Compact(context.Context) error
	// Mode reports the Agent's autonomy mode (for the status line).
	Mode() domain.Mode
	// SetMode changes the Agent's autonomy mode (Shift+Tab cycling). It is goroutine-safe, so
	// the UI may call it while the worker drives a Step; the change takes effect on the next
	// tool call.
	SetMode(domain.Mode)
	// Close releases the Agent's resources.
	Close() error
}

// ----------------------------------------------------------------------------
// Wiring values the binary resolves and the TUI renders
// ----------------------------------------------------------------------------

// Options carries the wiring the binary resolves (the composition root, cmd/apogee) and
// hands the TUI: the display values it renders in its status line but cannot read off the
// Engine (model, endpoint, autonomy mode, bypass flag, workspace root), plus the session
// saver seam.
type Options struct {
	Model     string
	Endpoint  string
	Mode      domain.Mode
	Bypass    bool
	Workspace string

	// ContextWindow is the active model's context-window size in tokens (0 when unknown), as
	// reported by upstream discovery. The footer renders it statically (e.g. "32k"); the live
	// status-line gauge that compares usage against it lights up when Phase 4 routes usage.
	ContextWindow int

	// HostAlias is a short, friendly name for the upstream host shown in the footer (a
	// `host-alias` config key). Empty falls back to the endpoint URL's host at render time.
	HostAlias string

	// Skills is the discovered skill catalog the /skill picker lists and the attached chips
	// label; nil ⇒ no skills are wired (the picker offers nothing, chips fall back to the raw
	// ID). The binary loads it from disk (internal/skills) and the agent loop resolves the same
	// catalog through Config.Skills, so the body the model sees matches what the picker showed.
	Skills SkillCatalog

	// Save persists a snapshot of the conversation; nil disables session saving. The
	// binary supplies a store-backed saver (it owns the path and on-disk format — phase-2
	// detail plan §3 C5). The model calls it only at a quiescent boundary (a clean quit),
	// passing the snapshot it took itself, so the file I/O stays out of the renderer while
	// the "is it safe to snapshot" decision stays with the model that owns the Engine.
	Save func(domain.Session) error
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
