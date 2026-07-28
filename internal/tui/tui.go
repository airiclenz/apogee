package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
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
	// Load returns a stored record WITHOUT changing which session is active. Activation is
	// deferred to Activate so the /sessions resume flow can switch which file Saves target only
	// after the live RestoreSession has confirmed the switch — a restore that then fails must
	// leave the current session untouched.
	Load(id string) (session.Record, error)
	// Activate makes meta's session the target of subsequent Saves, replacing the current active
	// session (the loaded file continues in place rather than forking a new one). The resume flow
	// calls it only once RestoreSession has succeeded, so a failed restore never redirects saves
	// away from the live conversation.
	Activate(meta session.Meta)
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
// methods (Submit/Step, and Interject at the boundary between them); ClearContext/Compact
// are driven from the Update goroutine but only at idle, when no worker runs — so the
// single-driver contract holds (phase-2 detail plan §3 C1).
type Engine interface {
	// Submit enqueues user input to begin or continue an Exchange.
	Submit(domain.UserInput) error
	// Step advances the loop one Turn and returns at a quiescent boundary.
	Step(context.Context) (domain.StepResult, error)
	// Snapshot captures the serializable conversation state at a boundary.
	Snapshot() (domain.Session, error)
	// Interject commits a user message into the OPEN Exchange, so a remark the human typed
	// while the model was working reaches it mid-task instead of waiting for the Exchange to
	// end (ADR 0025). Called ONLY by the worker goroutine, between Steps of the Exchange it
	// drives — the same class as Snapshot above: the driving goroutine owns the conversation
	// at that boundary, so the boundary is the synchronization and no mutex is involved.
	// It refuses with domain.ErrNoOpenExchange when no Exchange is open, and with an
	// empty-interjection error when the input carries no text, references, or skills; on
	// either refusal the conversation is untouched and the worker holds the remaining rows.
	Interject(domain.UserInput) error
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
	// ContextFilesReport reports what the session's workspace context files contributed and what
	// the standing system content costs against its Budget share — the data behind the session
	// notice. Like InExchange it is a boundary-only read: called at idle, right after the boundary
	// that started the session (startup, /clear|/new, a restore), never while a worker drives a Step.
	ContextFilesReport() domain.ContextFilesReport
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

	// ConfigHome is the resolved apogee home directory — `~/.apogee` by default, or whatever
	// `--config` / `APOGEE_CONFIG` selected. The renderer never derives it (the binary owns path
	// resolution) and reads it only to NAME a path in a report: /skills tells an empty catalog
	// where discovery looked, and looking under a home the run is not using would be a wrong
	// answer to the one question that note exists to answer. Empty ⇒ unwired: the reports fall
	// back to the "~/.apogee" spelling rather than inventing a path.
	ConfigHome string

	// ContextWindow is the active model's context-window size in tokens (0 when unknown), as
	// reported by upstream discovery. The footer renders it statically (e.g. "32k") and it is the
	// denominator of the live status-line context-fill gauge, which lights as each top-level
	// UsageEvent folds the turn's total-token count into ctxUsed (0 leaves the gauge hidden).
	ContextWindow int

	// HostAlias is a short, friendly name for the upstream host shown in the footer (a
	// `host-alias` config key). Empty falls back to the endpoint URL's host at render time.
	HostAlias string

	// Spinner is the status-line animation the `ui.spinner` config key selected. It is a SELECTION,
	// already validated by the binary (cmd/apogee's uiSettings.validate calls ParseSpinnerStyle), so
	// the renderer never parses a name. The zero value is not one of the styles: it resolves to
	// classic, the animation with no registry entry falls back to (spinnerAnim.spec). cmd/apogee
	// always sets a real style, so the zero value only reaches hand-built test Options — where the
	// one-column classic cell is what the existing status-line geometry tests expect.
	Spinner SpinnerStyle

	// SpinnerColor runs the spinner's slow colour loop. It is INDEPENDENT of Spinner — the loop
	// applies to whichever style is selected, and no style carries a colour of its own — so all
	// three styles × colour on/off are valid. The zero value, false, is no colour loop: the glyph
	// keeps the terminal's own text colour, which is also the pre-styles look under classic.
	SpinnerColor bool

	// CursorShape is the shape the prompt's caret is drawn with — what the `cursor-shape` config
	// key selected. apogee draws the REAL terminal cursor (the textarea's simulated one is retired
	// in newPromptEditor) and it never blinks, so the shape is the only axis there is: a Bubble Tea
	// program NAMES a cursor shape on every frame and never emits the DECSCUSR reset while it runs,
	// so "inherit the shape this terminal is configured with" is not expressible and this key is
	// the honest substitute for it. Like Spinner it is a SELECTION, already validated by the binary
	// (cmd/apogee's applyConfig calls ParseCursorShape), so the renderer never parses a name. The
	// zero value is tea.CursorBlock — also the configured default — so hand-built test Options and
	// an unset key agree.
	CursorShape tea.CursorShape

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

	// Heartbeat is one observation of the Upstream: is the server reachable, which model is it
	// serving, in which context window, and what else does it advertise (internal/heartbeat). The
	// binary backs it with a live heartbeat.Monitor; the TUI owns only the CADENCE and the
	// consequences — it fires one beat from Init, re-arms heartbeat.Interval after each LANDED beat
	// (so beats can never overlap), renders the offline state, and refuses a send while the server
	// is not there to answer it. A beat is never an error: an unreachable server is a finding the
	// Beat itself carries, which is why the seam returns no error.
	//
	// nil ⇒ unwired: no tick chain starts, no beat is ever folded, nothing is ever blocked, and the
	// footer keeps the launch-time display — exactly the pre-heartbeat behaviour, which is what
	// every hand-built test Options relies on. It is a func rather than an interface for the same
	// reason SaveHostAcknowledgement is: the TUI needs one call, not a type.
	Heartbeat func(context.Context) heartbeat.Beat

	// Rebind re-resolves and applies the per-model bindings after the heartbeat observed the
	// upstream serving a different model — or the same model in a different window. The binary owns
	// the resolution (the per-model system prompt, ADR 0023; the validated set, ADR 0016; the
	// mechanisms registry and the compaction budget) and the engine mutators; the TUI owns only
	// WHEN, which is the whole of its half: at idle the moment the beat lands, or deferred to the
	// exchange-terminal fold when a worker owns the engine — the quiescent boundary Agent.Rebind
	// demands (ADR 0024). It returns what was actually BOUND, which is not always what was observed
	// (a `context-window:` pin outranks the server's window), plus any notices to surface.
	//
	// nil ⇒ a display-frozen heartbeat: beats still light the offline state and refresh the
	// advertised model list, but no binding ever moves and no note claims one did. It is a func
	// rather than an interface for the same reason Heartbeat is: the TUI needs one call, not a type.
	Rebind func(model string, contextWindow int) (RebindResult, error)

	// Servers are the upstream servers this session can be switched to — the `/server` picker's
	// rows, in the order the binary assembled them: every `servers:` entry from config.yaml,
	// preceded by the endpoint this session started on whenever no entry already names it (so the
	// way back is always offered). It is display and identity only; what a switch actually needs
	// to talk to a server stays in the binary, behind SwitchServer.
	//
	// nil/empty ⇒ nothing to switch to, and `/server` says so rather than opening an empty overlay.
	Servers []ServerChoice

	// SwitchServer moves the whole session to the server named name: the binary re-points the
	// provider client at that endpoint with that key (Agent.SwitchUpstream), swaps in a heartbeat
	// Monitor for it, and restamps the session record. The split is Rebind's exactly — the TUI owns
	// only WHEN, and here that is an explicit act by the human at idle — because every input to the
	// move (the endpoint, the per-server key, that server's discovery hint, the window pin) is
	// config the binary owns.
	//
	// The TUI calls it synchronously on the Update loop: it mutates the engine and constructs a
	// client, and opens no connection of its own — the new server is DISCOVERED by the first beat
	// of the swapped-in Monitor, and the ordinary rebind path binds what that beat reports. The
	// switch therefore lands with NO model bound, which is why the result carries no model: the
	// display says "connecting…" for one beat, exactly as it does on a cold start.
	//
	// An error means nothing moved (the engine's own switch is validate-then-commit, and an
	// unresolvable name never reaches it), so the caller surfaces it as a note and leaves the
	// session where it was. nil ⇒ switching is unwired, and `/server` degrades to a note.
	SwitchServer func(name string) (ServerSwitchResult, error)

	// Resumed is the startup-replay payload when this run resumes a stored session (--resume or
	// --continue); nil on a fresh start. newModel seeds the start-up box as usual, then repaints
	// the resumed scrollback beneath it and relights the context gauge from the stored fill — or,
	// when no scrollback was recorded (a legacy session) or the blob will not decode, degrades to
	// an honest note with the view otherwise fresh. The binary resolves the store record and
	// projects it onto this small value, so the renderer never decodes the record itself.
	Resumed *ResumedSession
}

// RebindResult is what the composition root actually bound in answer to an observed change: the
// model id now going out on the wire, the context window the engine budgets and the gauge measures
// against — the observed one, or the `context-window:` pin that outranks it — and any per-rebind
// lines worth telling the human (a validated set that matched the new model, say), surfaced in
// order as transcript notes. The TUI renders it and never derives it: which window won, and why, is
// the binary's decision, so the renderer needs no knowledge of the pin at all.
type RebindResult struct {
	Model         string   // the model id now bound; what the footer and the start-up box show
	ContextWindow int      // the BOUND window in tokens (a pin wins over the observation); 0 ⇒ unknown
	Notices       []string // per-rebind lines to surface as transcript notes, in order
}

// ServerChoice is one upstream server the `/server` picker offers. Name does three jobs with one
// value — it labels the row, it is the name SwitchServer is called with, and it becomes the
// footer's host alias once the session is on that server — mirroring `host-alias:`, which names the
// startup endpoint exactly that way. Endpoint is shown beside it and is the identity the picker
// marks the CURRENT row by (string-equal to [Options.Endpoint], the same comparison the binary used
// when it decided whether the startup endpoint still needed a row of its own).
//
// It carries display and identity and nothing else: the per-server api key and discovery hint are
// what the switch needs, and the switch is the binary's half of the seam, so the renderer never
// holds a credential it has no use for.
type ServerChoice struct {
	Name     string // the row's label, the switch argument, and the footer alias afterwards
	Endpoint string // the server's base URL; also the identity of the row the session is on
}

// ServerSwitchResult is what the display adopts once a switch has committed: the endpoint now on
// the wire, the alias the footer calls it (the chosen entry's own name), and the context window the
// gauge measures against — the global `context-window:` pin, which is not a per-server value and so
// survives the move, or 0 when unpinned. The window rides the result for the same reason
// [RebindResult] carries one: which window wins, and why, is the binary's decision, so the renderer
// keeps needing no knowledge of the pin.
//
// No model is reported, deliberately: a switch UNBINDS the model rather than guessing what the new
// server serves (ADR 0024), and the first beat of the new server binds one through the ordinary
// rebind path — one code path with the cold start.
type ServerSwitchResult struct {
	Endpoint      string // the new Upstream's base URL, adopted by the footer and the start-up box
	HostAlias     string // the chosen server's name, now the footer's host label
	ContextWindow int    // the `context-window:` pin that survives the switch; 0 ⇒ unpinned/unknown
}

// ResumedSession is the startup-replay payload the composition root hands the TUI when a run
// resumes a stored session: the opaque transcript blob to repaint (the TUI's own wire form,
// transcriptcodec.go), the browsable title for the "resumed: <title>" note, the last observed
// context fill to relight the status-line gauge, and the stored user-message count. An empty or
// undecodable Transcript degrades to a no-scrollback note rather than a fatal error — a resumed
// legacy session still lists and resumes, it just has no scrollback to repaint.
type ResumedSession struct {
	Transcript []byte // the TUI's opaque scrollback blob; empty (a legacy record) ⇒ no replay
	Title      string // the session's browsable title, shown in the resume note
	CtxUsed    int    // the last observed context fill, relighting the gauge on resume
	UserMsgs   int    // the stored user-message count (metadata parity; the transcript re-derives it)
	// InExchange marks a session interrupted mid-task — the resumed Agent reports an open Exchange
	// (the binary reads agent.InExchange() after building it). newModel then appends the interrupted
	// note so the human knows /continue picks up the unfinished work; false for a cleanly-closed
	// session, which resumes without one.
	InExchange bool
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
