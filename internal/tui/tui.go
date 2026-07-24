package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
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

// SessionHost is the session-persistence seam the TUI drives: it persists the active session
// every Turn (and on quit), rotates to a fresh session on /clear|/new, and backs the /sessions
// browser's list/load/delete/rename. It is defined here — like [SkillCatalog] — and typed against
// internal/session (the Meta/Record the browser renders), so the renderer stays unit-testable with
// a fake and the composition root (cmd/apogee) owns the store, the id minting, and the on-disk
// format. A nil host means persistence is unwired; every caller guards for it exactly as it does
// for [Options.Skills].
type SessionHost interface {
	// Save persists the active session's current state, minting its ID on the first call and
	// updating that same file thereafter. transcript is the TUI's opaque scrollback blob
	// (transcriptcodec.go); title, userMsgs, and ctxUsed populate the browsable metadata.
	Save(sess domain.Session, transcript []byte, title string, userMsgs, ctxUsed int) error
	// Rotate closes the active session so the next Save mints a fresh ID — the /clear|/new and
	// load-a-different-session boundary. It is idempotent on an already-inactive session.
	Rotate()
	// List returns every stored session's browsable metadata, newest first.
	List() ([]session.Meta, error)
	// Load returns a stored record AND makes it the active session, so subsequent Saves update
	// the loaded session's file rather than forking a new one.
	Load(id string) (session.Record, error)
	// Delete removes a stored session's file.
	Delete(id string) error
	// Rename sets a stored session's title.
	Rename(id, title string) error
	// ActiveID reports the active session's ID, or "" before the first Save has minted one.
	ActiveID() string
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
	// AbortExchange discards an Exchange the user cancelled, returning the engine to a clean
	// boundary the next Submit/ClearContext accepts. Called once the worker has returned its
	// cancelledMsg (no worker owns the engine), so the post-Esc /clear or message is not
	// rejected with ErrInputPending.
	AbortExchange()
	// RestoreSession swaps a stored snapshot into the LIVE Agent without a rebuild, so tools,
	// Mechanisms, and MCP wiring stand (the in-TUI resume primitive the /sessions browser drives).
	// Like ClearContext it is called only at idle (no worker running) and refuses mid-Exchange
	// (ErrInputPending); a corrupt or future-version snapshot returns an error and leaves the live
	// conversation untouched. It does not touch the allow-for-session cache, mode, or confinement.
	RestoreSession(domain.Session) error
	// InExchange reports whether a multi-Turn Exchange is currently open — a boundary-only read the
	// host makes after a restore (or at startup) to detect a session interrupted mid-task. Called
	// only at idle.
	InExchange() bool
	// Compact triggers generative Compaction on demand (the /compact command): it summarizes
	// the conversation and replaces the folded history with the summary. A real upstream call,
	// so the TUI drives it on a worker goroutine. Called only at idle. skipped is true when the
	// conversation was too small to fold (no call made, history untouched) so the UI reports
	// "nothing to compact" and leaves the gauge alone; it is always false on error.
	Compact(context.Context) (skipped bool, err error)
	// SetMode changes the Agent's autonomy mode (Shift+Tab cycling). It is goroutine-safe, so
	// the UI may call it while the worker drives a Step; the change takes effect on the next
	// tool call.
	SetMode(domain.Mode)
	// SetConfineToWorkspace changes Auto's blast radius (the /confine off|on command): true —
	// the default — fences confinable subprocess writes to the workspace and gates through
	// Approval what cannot be fenced, while false is the user's explicit "I am the sandbox" and
	// runs every call unconfined with their full privileges (ADR 0012). Like SetMode it is
	// goroutine-safe, so the UI may call it while the worker drives a Step; the change takes
	// effect on the next tool call and affects only this Session — persisting the host
	// acknowledgement is the binary's job, not the engine's.
	SetConfineToWorkspace(bool)
	// ConfineToWorkspace reports the blast radius the NEXT tool call's Resolution will read —
	// the live setting, so it already reflects any earlier SetConfineToWorkspace. The /confine
	// status report renders it, and /confine off|on reads it to say whether the line changed
	// anything. Goroutine-safe like SetMode, though the UI calls it only at idle.
	ConfineToWorkspace() bool
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
	// reported by upstream discovery. The footer renders it statically (e.g. "32k") and it is the
	// denominator of the live status-line context-fill gauge, which lights as each top-level
	// UsageEvent folds the turn's total-token count into ctxUsed (0 leaves the gauge hidden).
	ContextWindow int

	// HostAlias is a short, friendly name for the upstream host shown in the footer (a
	// `host-alias` config key). Empty falls back to the endpoint URL's host at render time.
	HostAlias string

	// Version is the resolved FULL build version (apogee.Version, read from the embedded VERSION
	// file plus build provenance), read only by the /version command — it mirrors what --version
	// prints. The start-up box reads BaseVersion instead, so the TUI never imports the source.
	// Empty ⇒ unwired.
	Version string

	// BaseVersion is the release version WITHOUT build provenance (apogee.BaseVersion — the
	// trimmed VERSION file, e.g. "v1.8.0"), the value the start-up box displays. It is a separate
	// seam from Version so the box reads clean while /version and --version keep the full string;
	// the TUI stays format-agnostic (cmd/apogee resolves both). Empty ⇒ unwired.
	BaseVersion string

	// Confinement is the host's confinement situation as the composition root resolved it, for
	// the /confine status report to name. The TUI never derives it — internal/platform is the
	// binary's dependency, not the renderer's — so an unwired zero value simply reports
	// "unknown" rather than inventing a backend.
	Confinement ConfinementInfo

	// SaveHostAcknowledgement persists THIS host's `unconfined-hosts:` acknowledgement to the
	// global config (the `/confine off --save` half) and returns the file it wrote, so the
	// confirmation can name what changed and how to undo it. nil ⇒ persistence is unavailable
	// and `--save` says so; the session toggle itself never depends on it. Writing config is
	// the binary's job (it owns the path and the file format), exactly like Save below.
	SaveHostAcknowledgement func() (path string, err error)

	// Skills is the discovered skill catalog the /skill picker lists and the attached chips
	// label; nil ⇒ no skills are wired (the picker offers nothing, chips fall back to the raw
	// ID). The binary backs it with a live skills.Provider and the agent loop resolves the SAME
	// provider through Config.Skills, so the body the model sees matches what the picker showed —
	// including skills ReloadSkills swapped in mid-session.
	Skills SkillCatalog

	// ReloadSkills re-scans the skill source dirs and swaps in a fresh catalog, so a skill added
	// or edited after launch is picked up the next time the /skill picker opens. nil disables the
	// refresh (the catalog stays as loaded at launch). The binary wires it to the shared
	// skills.Provider both this picker (Skills) and the agent loop (Config.Skills) read, so a
	// refreshed skill both shows in the picker AND resolves when attached. The picker edge-
	// triggers it on open, not per keystroke; every caller guards for nil.
	ReloadSkills func()

	// Sessions is the session-persistence host (the store-backed [SessionHost] the binary
	// wires); nil disables all persistence. The Model drives it: a per-Turn save through the
	// worker's snapshot, a final save at each idle boundary, and a synchronous flush on a clean
	// quit — each best-effort, so a save failure never interrupts the conversation. The binary
	// owns the path, id minting, and on-disk format, keeping the file I/O out of the renderer
	// while the "is it safe to snapshot" decision stays with the Model that owns the Engine.
	Sessions SessionHost
}

// ConfinementInfo is the host's confinement situation, resolved once by the composition root
// and rendered by the /confine status report: which Confiner backend answered, what that
// backend can actually enforce here, and the host id an `unconfined-hosts:` acknowledgement is
// matched against (ADR 0012, amendment 2026-07-21). It is the diagnostic half of /confine —
// the *effective* setting is read live off the [Engine], not from here, because the user can
// change it mid-session. The zero value means "the binary wired nothing"; the report says
// unknown rather than guessing.
type ConfinementInfo struct {
	Backend string                 // the backend's human label ("landlock", "seatbelt", "deny"); "" ⇒ unknown
	Caps    domain.ConfinementCaps // what it can enforce on THIS host — FSWrite false is the degraded case
	HostID  string                 // platform.HostID(), the id --save records; "" ⇒ unknown
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
	// The per-Turn snapshot notify: the worker sends turnSnapshotMsg through the Bridge's
	// late-bound program sender (the same programRef the Sink pushes Events through), so the
	// Model persists between Steps without any exported API. Bind (below) resolves it to the
	// live program before the first worker can fire.
	program := tea.NewProgram(newModel(ctx, eng, opts, br.prog.send), tea.WithContext(ctx))
	// Bind before Run: the program exists now, and the first Send cannot occur until a
	// worker is launched, which only happens after the user submits into the running loop.
	br.Bind(program)
	_, err := program.Run()
	return err
}
