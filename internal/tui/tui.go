package tui

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
)

// SkillCatalog is the read-only view of the discovered skills the TUI needs: the full sorted
// list for the merged "/" menu (List), a by-id lookup that resolves an inline "/token" (Get), and the
// files discovery could not load (Skipped) so /skills can say why a skill is missing instead of
// silently omitting it. It is satisfied by *skills.Catalog; the TUI depends only on this
// interface so it stays unit-testable with a fake, and — being an interface — it is a reference
// header safe to hold in the value-copied Model (ADR 0011). A nil catalog means no skills are
// wired; every reader guards for it.
type SkillCatalog interface {
	List() []skills.Skill
	Get(id string) (skills.Skill, bool)
	Skipped() []skills.SkipError
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

// RecallHost is the prompt-recall seam the TUI drives: record an input the human sent
// (AppendPrompt) and read back what this workspace has already recorded (LoadPrompts), which is
// what the prompt box walks with Up/Down. It is defined here — like [SessionHost] — so the
// renderer stays unit-testable with a fake while the composition root owns the store, the file
// format, and the directory it lives in.
//
// The WORKSPACE is pre-bound by the host side and appears in neither method. Recall is
// per-workspace (internal/recall keys its files on the absolute path), and resolving that path is
// exactly the kind of ambient lookup the renderer does not do (the [Options.ConfigHome] posture):
// binding it once at the composition root leaves this surface with one workspace it cannot get
// wrong.
//
// A nil host means recall is unwired: nothing is loaded at start-up, nothing is recorded, and the
// arrows keep their cursor duty — the pre-recall behaviour every hand-built test Options relies on.
// Both methods are called OFF the Update loop on Cmd goroutines (they touch disk), so an
// implementation must be safe to call from one; neither error is ever fatal, because recall is a
// convenience and a session must never fail over one.
type RecallHost interface {
	// AppendPrompt records text as this workspace's newest sent input. Fire-and-forget: the
	// caller swallows the error, since a prompt that failed to record has still been sent.
	AppendPrompt(text string) error
	// LoadPrompts returns this workspace's recorded inputs oldest→newest. An error leaves recall
	// simply empty.
	LoadPrompts() ([]string, error)
}

// Scheduler is the scheduling seam the TUI drives: put a standing instruction on the clock
// (Add), take one off it (Stop), and list what is live (List) — the whole of what a surface
// needs from the scheduler library (ADR 0033). It is defined here like [SessionHost] and typed
// against internal/schedule (the Spec the pickers compose and the Status the rows render), so
// the renderer stays unit-testable with a fake while the composition root owns the runner, the
// Gate and the clock. It is satisfied by *schedule.Scheduler.
//
// Every when-and-how DECISION stays behind it: the cycle floor, the mode validation, the
// overlap policy and the naming of an unnamed Schedule are the library's (schedule.Spec), which
// is why this surface pre-checks none of them and words the errors it gets back instead.
//
// A nil Scheduler means scheduling is unwired: /schedule and /schedule-stop say so in a note,
// exactly as a nil GenerateTitle makes a bare /rename report that naming is unavailable.
type Scheduler interface {
	// Add validates spec, puts the Schedule on the clock and returns its id.
	Add(spec schedule.Spec) (string, error)
	// Stop takes the Schedule with this id off the clock.
	Stop(id string) error
	// List reports every live Schedule in creation order.
	List() []schedule.Status
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

	// HostAlias is a short, friendly name for the upstream host shown in the footer — the bound
	// `servers:` entry's own name, which is what a server's alias now is (ADR 0036 decision 1).
	// Empty falls back to the endpoint URL's host at render time.
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

	// HideScrollbar takes the transcript's scroll bar away — and with it the column the bar hangs
	// in, which the body then takes, because a hidden bar that still ate a column would read as a
	// bug. It is what the `ui.show-scrollbar` config key selected, INVERTED at the composition root
	// (cmd/apogee's wire.go is the one place the polarity flips): the config key is positive and
	// defaults to true, while this field must have the zero value mean today's behaviour — the bar
	// shown — so the hand-built Options of the layout tests keep the width they pin. A `/settings`
	// edit of the key moves it mid-session (ADR 0037) and re-lays out, so the wrap width it decides
	// changes exactly when the human changes it and never on its own.
	HideScrollbar bool

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

	// ColorScheme is the palette every style in the theme is built from — what the
	// `ui.color-scheme` config key selected, already RESOLVED by the binary (cmd/apogee calls
	// scheme.Resolve, which reads the user's schemes folder). Handing over the palette rather than
	// the name is what keeps file reading out of the renderer at boot: the TUI selects, it never
	// parses. The zero value is the empty Scheme, which is not a palette at all — hand-built test
	// Options leave it so, and colorScheme() answers it with the built-in default.
	ColorScheme scheme.Scheme

	// ColorSchemeName is the name that palette was loaded under, so a report can SAY which scheme is
	// in force (`/color-scheme` lists it as the current one) without the renderer having to
	// recognize a palette it was handed. Empty ⇒ unwired, and the reports fall back to the default
	// scheme's name.
	ColorSchemeName string

	// ColorSchemeWarnings is what resolving that scheme cost, already rendered to lines: an unknown
	// name, an unreadable file, a defective key. Each becomes one ephemeral transcript note at
	// construction (ADR 0040 design call 11) — the load itself is forgiving, so a warning is the
	// only thing standing between a silently-wrong palette and a human who can fix it. Nil on the
	// ordinary run, where the scheme loaded cleanly.
	ColorSchemeWarnings []string

	// ListSchemes names every scheme that can be switched TO right now — the built-ins plus every
	// `*.yaml` in the schemes folder, a user file shadowing a built-in of the same name (ADR 0040
	// design call 6). It is a closure rather than a slice for the reason [Servers] is: a folder the
	// human drops a file into mid-session is offered by the settings picker the moment they open it,
	// and a list snapshotted at launch would go stale the first time they wrote a scheme. Discovery
	// itself stays in the binary — the renderer never walks a directory (ADR 0011's thin renderer).
	//
	// nil ⇒ the picker has no vocabulary and the row opens nothing, the nil-seam degrade every
	// provider here takes.
	ListSchemes func() []string

	// ResolveScheme turns one of those names back into a palette, warnings already rendered to
	// lines — the same call the binary made at boot ([ColorScheme]), re-run so a switch re-READS the
	// file and an edited scheme lands on the next switch without a restart. Warnings are the
	// forgiving load's only voice (design call 8): the palette that comes back is always usable, so a
	// defective key is a sentence in the transcript rather than a failure.
	//
	// nil ⇒ no live switch is possible; the settings row still persists the key (the pane's write
	// half is independent) and the new scheme takes effect at the next start.
	ResolveScheme func(name string) (scheme.Scheme, []string)

	// ExportScheme writes an editable copy of the named BUILT-IN scheme into the schemes folder and
	// returns the path it wrote. It is the only way a scheme file comes into existence: the built-ins
	// are embedded in the binary and never installed on disk (ADR 0040 design call 1), so without an
	// export there is nothing to open in an editor and the shadowing rule has nothing to shadow with.
	//
	// It never overwrites (design call 7): an existing file is an error naming it, so an export can
	// never destroy the scheme somebody has been working on. Every error — unknown name, file
	// present, unwritable folder — is REPORTED, the SaveHostAcknowledgement contract, because a
	// silent export is indistinguishable from one that worked.
	//
	// nil ⇒ `/color-scheme export` says so and writes nothing, the nil-seam degrade every provider
	// here takes.
	ExportScheme func(name string) (path string, err error)

	// Version is the resolved FULL build version (apogee.Version, read from the embedded VERSION
	// file plus build provenance), read only by the /version command — it mirrors what --version
	// prints. The start-up box reads BaseVersion instead, so the TUI never imports the source.
	// Empty ⇒ unwired.
	Version string

	// BaseVersion is the release version WITHOUT build provenance (apogee.BaseVersion — the
	// trimmed VERSION file, e.g. "vX.Y.Z"), the value the start-up box displays. It is a separate
	// seam from Version so the box reads clean while /version and --version keep the full string;
	// the TUI stays format-agnostic (cmd/apogee resolves both). Empty ⇒ unwired.
	BaseVersion string

	// TracePath and DiagPath are the two hidden diagnostic seams (`--tui-trace` and `--tui-diag`,
	// cmd/apogee/root.go), each empty unless a path was named on the command line and each costing
	// nothing when it is. TracePath collects every byte the renderer writes to the terminal, one
	// quoted Go string per write, which is what makes a rendering bug arguable from the stream that
	// caused it rather than from a screenshot. DiagPath collects what the terminal said about
	// itself — TERM and the emulator's own variables, the size, the resolved colour profile, every
	// mode report, and the width method the painter ends up on.
	//
	// They are paths rather than writers because the binary resolves them from flags and the
	// renderer is the side that knows WHEN the seams must be live: the trace has to be installed as
	// bubbletea's output before the program is built, and the diag log has to be on the Model
	// before the first message reaches Update. See diagnostics.go for the one constraint that
	// decides the trace's shape.
	TracePath string
	DiagPath  string

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

	// SettingsRows lists every config key the `/settings` pane shows, in the order the config
	// template presents them, as the binary resolved them THIS run (see [SettingRow]). It is a
	// provider rather than a slice for the reason the picker's rows are derived at render time
	// (picker.go): the pane calls it each time it paints, so a row re-read after an edit reflects
	// what the edit made of it and a selection is clamped against rows that are current.
	//
	// The binary owns everything behind it — the key registry, the schema, the precedence that
	// decided which source won, the masking of a secret — exactly as SaveHostAcknowledgement owns
	// the file format. nil ⇒ unwired: `/settings` has nothing to show and says so, the nil-seam
	// degrade every other provider here takes.
	SettingsRows func() []SettingRow

	// WriteSetting persists one config key — the `/settings` pane's whole write half (ADR 0035:
	// one key per deliberate edit). path is the row's registry path ("ui.spinner") and value is
	// the value as the file would spell it ("true", "32768", "ask-before"), which is exactly what
	// [SettingRow.Value] and [SettingRow.EnumValues] carry: the renderer hands back a string it
	// was given rather than a YAML fragment it composed, because the binary owns the file format
	// (the SaveHostAcknowledgement precedent) and it alone knows whether the key may be written
	// at all — the registry's editability, the splice, the verification and the atomic write are
	// all behind this one call.
	//
	// It is synchronous like SaveHostAcknowledgement: one small file, spliced and renamed, on a
	// keypress the human is waiting on. An error is REPORTED, never swallowed — the pane shows it
	// on the row and treats the key as unchanged — so a read-only config home surfaces as a
	// refusal rather than as an edit that silently did not happen. nil ⇒ writing is unavailable
	// and the pane says so, the nil-seam degrade every provider here takes.
	WriteSetting func(path, value string) error

	// ResetSetting returns one config key to its default by REMOVING the file's line for it (ADR
	// 0035) rather than writing today's spelling of the default into the file, so the key goes
	// back to being described by the binary and documented by its commented example. A key the
	// file does not set is already at its default: that is a no-op, not an error.
	//
	// Same contract as WriteSetting in every other respect — synchronous, path-addressed,
	// errors reported, nil ⇒ unavailable.
	ResetSetting func(path string) error

	// ApplySetting makes one persisted key take effect in the RUNNING session — the apply half of
	// every `/settings` edit (ADR 0037 decision 1: validate → persist → apply, on the same ⏎). path
	// and value are WriteSetting's, so the pane hands the apply exactly what it handed the write and
	// no second spelling of a value exists; the binary resolves that string into whatever the engine
	// seam takes (a mode, a bool, a name list) because it owns the schema, as it owns the file
	// format behind WriteSetting.
	//
	// note is a short boundary sentence for a key that cannot land NOW and lands at a boundary the
	// session will cross anyway — "applies at next clear" for the context files, whose prefix is
	// frozen for the session on purpose (ADR 0026). Empty means it is already in effect, which is
	// the answer for almost every key. It never defers to a restart: a key that could only take
	// effect the next time the process starts has no business in this seam.
	//
	// An error is REPORTED and does not unwind the write (ADR 0037 decision 1): the file already
	// expresses the intent, so the row says "saved — live apply failed: …" and a re-committed edit
	// retries the apply. nil ⇒ no live apply at all: the pane persists and applies nothing, the
	// degrade a bench or headless Driver composes deliberately (ADR 0031).
	ApplySetting func(path, value string) (note string, err error)

	// ExternalEditSpec is the command line that opens the config file at path's own line — the
	// nested structures' whole edit idiom (ADR 0037 decision 5): a `servers:` list or a
	// `model-profile:` block is a shape no row can hold, so ⏎ on such a row hands the human the file
	// itself in their own editor rather than growing a form for each of them.
	//
	// The binary resolves all four parts because it owns all four: the config file's location, the
	// line that key sits on (its own splice writer already parses the document for it), which editor
	// this environment names — the `editor` key, then $VISUAL, then $EDITOR, then the platform's own
	// opener — with a line-jump argument passed only to the editors known to take one, and whether
	// that program takes this terminal ([EditorCommand.Detached], ADR 0041 decision 6). The renderer
	// receives a command it runs and nothing else, exactly as [WriteSetting] hands it a file format
	// it never composes.
	//
	// An error is REPORTED on the row (an unreadable config, a file shape the parse refuses, a
	// program this machine cannot run) and nothing is launched. nil ⇒ no external edit is available
	// and ⏎ on those rows does nothing, the nil-seam degrade every provider here takes.
	ExternalEditSpec func(path string) (EditorCommand, error)

	// ReloadConfig re-reads the config file after that external edit and reports which keys came
	// back different — the return half of the same round trip. The binary re-runs the startup
	// resolution over the file it alone can parse and diffs it against what the file said when the
	// editor was launched (ExternalEditSpec takes that baseline), so what comes back is the human's
	// edit and nothing else.
	//
	// A parse or validation failure returns the error with NOTHING applied and the file untouched:
	// the human's own text stays exactly as they left it, and the row that launched the edit carries
	// the reason so they can go back in and fix it.
	//
	// What is returned is not yet in force — the pane applies each key through [ApplySetting] and
	// its own renderer-local keys itself, the same two homes an in-pane commit uses (ADR 0037
	// decision 1), so a key edited in the file and a key edited on the row land the same way. The
	// keys the reload never reports are the confinement pair, fenced to `/confine` (ADR 0012), and
	// `server:`, whose live move is a deliberate act at the picker rather than a consequence of
	// re-reading a file. nil ⇒ the round trip ends at the editor, and the pane says so.
	ReloadConfig func() ([]AppliedSetting, error)

	// AwaitConfigChange blocks until the config file has changed on disk, and reports whether the
	// watch is still open: false means it has ENDED — the program is shutting down, or the binary
	// stopped watching — and nothing will ever be reported again.
	//
	// It is the second trigger for the round trip [ReloadConfig] answers, and the reason there is a
	// second one (ADR 0041 decision 3). ADR 0037 made the editor's EXIT the end-of-edit signal, which
	// a desktop opener cannot give: `open`, `xdg-open` and `cmd /c start` return the moment the file
	// is handed to the application that owns `.yaml`, long before the human has typed anything, so the
	// diff behind that signal reads unchanged bytes and concludes they edited nothing. The file itself
	// is the signal instead — and then it does not matter who wrote it: the editor this pane launched,
	// a GUI editor left open in another window, or a `vim ~/.apogee/config.yaml` in a second terminal
	// all apply the same way (decision 5).
	//
	// The Model owns the cadence and the consequences and nothing else, exactly as it does for
	// [Heartbeat]: one wait is opened at Init, each landed report re-reads through [ReloadConfig],
	// journals and applies what came back through the same two homes an in-pane commit uses, and opens
	// the next wait. WHICH file is watched, how, and how often is the binary's alone.
	//
	// nil ⇒ nothing is watched, the pre-watcher behaviour: a foreground editor still applies on exit
	// and a detached one applies at the next relaunch.
	AwaitConfigChange func(ctx context.Context) bool

	// Skills is the discovered skill catalog the merged "/" menu lists and an inline "/token"
	// resolves against; nil ⇒ no skills are wired (the menu offers no skills and no token
	// resolves). The binary backs it with a live skills.Provider and the agent loop resolves the SAME
	// provider through Config.Skills, so the body the model sees matches what the menu showed —
	// including skills ReloadSkills swapped in mid-session.
	Skills SkillCatalog

	// ReloadSkills re-scans the skill source dirs and swaps in a fresh catalog, so a skill added
	// or edited after launch is picked up the next time the merged "/" menu opens. nil disables the
	// refresh (the catalog stays as loaded at launch). The binary wires it to the shared
	// skills.Provider both this menu (Skills) and the agent loop (Config.Skills) read, so a
	// refreshed skill both shows in the menu AND resolves when attached. The menu edge-
	// triggers it on open, not per keystroke; every caller guards for nil.
	ReloadSkills func()

	// Sessions is the session-persistence host (the store-backed [SessionHost] the binary
	// wires); nil disables all persistence. The Model drives it: a per-Turn save through the
	// worker's snapshot, a final save at each idle boundary, and a synchronous flush on a clean
	// quit — each best-effort, so a save failure never interrupts the conversation. The binary
	// owns the path, id minting, and on-disk format, keeping the file I/O out of the renderer
	// while the "is it safe to snapshot" decision stays with the Model that owns the Engine.
	Sessions SessionHost

	// Recall is the prompt-recall host (the store-backed [RecallHost] the binary wires with this
	// run's workspace already bound); nil ⇒ recall is off and the arrows never leave cursor duty.
	// The Model drives it exactly twice: one load at start-up, and one fire-and-forget append per
	// input sent — both off the Update loop, both best-effort, so neither can interrupt a session.
	// The binary owns the directory and the on-disk format, keeping the file I/O out of the
	// renderer as [Sessions] does.
	Recall RecallHost

	// GenerateTitle names a Session record from a WINDOW of the user's requests, oldest first — the
	// cosmetic, out-of-band naming completion (ADR 0022 addendum, 2026-07-31). The automatic call at
	// first-prompt submit passes exactly one prompt, which is not a restriction but an identity: one
	// is all that exists when it fires. It is NOT a Turn and NOT a Mechanism: it never goes through
	// the Engine (whose single-goroutine contract it would otherwise break, ADR 0011), fires at no
	// Hook point, emits no Token/Usage event, never enters the transcript, and nothing in the
	// conversation depends on its result. The binary backs it with its own provider.Client over the
	// server and model this session is bound to AT CALL TIME, so a `/server` switch or a rebind
	// carries the naming call with it; the renderer owns only WHEN it fires and whether the answer
	// is applied.
	//
	// It returns the model's RAW reply — cleaning it up is title.Sanitize's job, kept on this side
	// of the seam so a generated title and a manual `/rename <text>` pass through one pipeline and
	// can never disagree about what a title may contain.
	//
	// nil ⇒ naming is unwired, exactly as a nil Sessions is: the automatic call never fires and a
	// bare `/rename` reports that generation is unavailable. Never an error. It is a func rather
	// than an interface for the same reason SaveHostAcknowledgement is: the TUI needs one call, not
	// a type.
	GenerateTitle func(ctx context.Context, prompts []string) (string, error)

	// AutoTitle gates only the AUTOMATIC naming call — the `auto-title:` config key, default true.
	// False leaves GenerateTitle wired, so a bare `/rename` still regenerates on demand: the toggle
	// answers "name my sessions for me", not "may apogee ever ask for a title". The zero value,
	// false, is therefore what hand-built test Options get: no session names itself unless the
	// binary said so.
	AutoTitle bool

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
	// rows and the `/settings` server row's popup, in the order the binary assembled them: every
	// `servers:` entry from config.yaml, preceded by the endpoint this session started on whenever
	// no entry already names it (so the way back is always offered). It is display and identity
	// only; what a switch actually needs to talk to a server stays in the binary, behind
	// SwitchServer.
	//
	// A PROVIDER rather than a slice, the SettingsRows posture: the `servers:` block is itself
	// something the human can change mid-session (ADR 0037's `$EDITOR` jump), so the list is asked
	// for every time one is drawn rather than frozen at launch — an entry added a moment ago is
	// offered without restarting apogee.
	//
	// nil, or an empty answer ⇒ nothing to switch to, and `/server` says so rather than opening an
	// empty overlay.
	Servers func() []ServerChoice

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

	// Prebound says this session started with NO upstream bound, and why (ADR 0036 decisions 3, 4
	// and 7). The zero value is the ordinary start — the binary determined a startup server and the
	// engine was constructed before the program began — and everything below it is unchanged.
	//
	// A non-zero Reason means there is no engine yet: the binary could not tell which server to
	// start on, and the TUI is the one Driver that can ASK rather than refuse (the non-interactive
	// drivers keep their hard error, because they have nobody to ask). Engine calls are answered
	// with a "no server is bound" error until BindServer below lands one, so the renderer's job is
	// to reach that seam before anything needs the engine.
	Prebound PreboundStart

	// BindServer constructs the session's engine on the named `servers:` entry — the pre-bound
	// half of SwitchServer, and the only seam that can end the pre-bound state. It answers with the
	// same [ServerSwitchResult] a switch does, so the display adopts a first binding exactly as it
	// adopts a move.
	//
	// It binds ONCE: a session that already has an engine is answered with an error naming
	// SwitchServer's own verb, because a second construction would leave the first engine running
	// with nothing holding it. Like SwitchServer it is called synchronously on the Update loop and
	// opens no connection of its own — the first beat of the Monitor it installs is what discovers
	// the server.
	//
	// nil ⇒ unwired, and a pre-bound session has no way out of the pre-bound state; the binary
	// always wires it beside SwitchServer.
	BindServer func(name string) (ServerSwitchResult, error)

	// RecordServerChoice persists the entry this session starts on NEXT time — the `server:` key
	// ADR 0036 decision 2 records on every move to a configured entry. The renderer calls it with the
	// name it just bound or switched to and knows nothing else about it: whether that name belongs to
	// a configured entry (and is therefore worth writing) is the binary's question, because only the
	// binary can tell a configured row from the synthesized one an override startup earns.
	//
	// It answers whether it WROTE, which is what lets the renderer state the recording beside the
	// move ("· server: saved") without claiming one for the moves the binary skips: a name in no
	// `servers:` entry is skipped silently, and false with no error is exactly that outcome.
	//
	// It is best-effort persistence of something that ALREADY happened: the session moved before this
	// is called and stays moved whatever it answers, so an error is a note and never an undo. Like
	// WriteSetting it is synchronous — one small file, spliced and renamed — and called on the Update
	// loop.
	//
	// nil ⇒ nothing is recorded and every switch is session-scoped, which is exactly the behaviour
	// this key was introduced to replace; every hand-built Options keeps it.
	RecordServerChoice func(name string) (recorded bool, err error)

	// LaunchProfiles lists the Launch profiles the launcher's config defines — what `/model` offers on
	// a host with a launcher, re-read FRESH every time the picker opens (ADR 0029 D4), so a profile
	// added in the launcher's own TUI a moment ago is offered here without restarting apogee. The
	// binary owns the read because it is the only layer that knows the launcher exists at all; the
	// renderer receives rows it can label and pick from, and nothing else.
	//
	// The error is the one failure that sinks the list — no config at a configured path, a config
	// that will not parse — and reaches the human as a one-line note rather than an overlay. A single
	// profile that cannot be resolved is NOT that failure: it is simply absent from the rows, because
	// one moved model file must not cost the user their other nine profiles.
	//
	// nil ⇒ the llama-launcher integration is not configured on this host (`llama-launcher: off`, or
	// auto-detect found no launcher config): `/model` then offers what the server itself advertises,
	// and `/unload-model`/`/stop-server` degrade to a note naming the key. The four launcher seams
	// are wired together or not at all, so one nil check speaks for all of them.
	LaunchProfiles func() ([]LaunchProfileChoice, error)

	// LoadProfile activates the named Launch profile and reports what the session must adopt — the
	// composite verb of ADR 0029 D2. The binary drives the launcher (a BLOCKING call: up to ~30 s
	// waiting for health, plus a stop escalation when a restart displaces an occupant, and minutes
	// for a large model), then decides whether the session has to move at all: a profile that
	// resolves to the endpoint this session is already on moves nothing — no Move in the result, and
	// the next beat observes the new model and rebinds through the ordinary path — while one that
	// resolves elsewhere is FOLLOWED with the same fold `/server` performs, reported as a resolved
	// [ProfileLoadResult.Move] the completion fold commits and hands to that fold. No result here is
	// ever a binding; only a Beat binds (ADR 0029 D1).
	//
	// Because it blocks, the TUI runs it on a Cmd goroutine and holds the actuation latch across it —
	// that latch is also the per-address serialization the launcher's contract demands of its caller.
	// progress receives the launcher's lifecycle steps one call per step, ON THE CALLING GOROUTINE, so
	// the TUI pumps them into the transcript rather than rendering from them; nil is safe.
	//
	// nil ⇒ unwired; see LaunchProfiles.
	LoadProfile func(name string, progress func(step string)) (ProfileLoadResult, error)

	// UnloadServer frees the model of the server this session is talking to, and StopServer stops
	// that server outright (ADR 0029 D3). Both take the session's endpoint rather than reading one:
	// the renderer is the side that knows which server the session is on — it may have moved since
	// launch — while the binary is the side that knows which addresses the launcher's config implies.
	// An endpoint the launcher does not manage comes back as an error naming it; neither verb ever
	// guesses an address to act on, because the one mistake available here stops somebody else's
	// server.
	//
	// Both BLOCK for the stop escalation (~20 s worst case) and run under LoadProfile's latch, for
	// the same serialization reason. The result carries the steps taken EVEN WHEN the error is
	// non-nil — how far a stop got before it failed is exactly what the human needs to know — so the
	// caller renders the steps first and the error after.
	//
	// nil ⇒ unwired; see LaunchProfiles.
	UnloadServer func(endpoint string) (ActuationResult, error)
	StopServer   func(endpoint string) (ActuationResult, error)

	// LauncherEnabled reports whether the llama-launcher integration is switched ON right now. Since
	// `llama-launcher:` became editable mid-session (ADR 0037) the four seams above can be wired for
	// the life of the session with the answer moving INSIDE them, so "is there a launcher here" is no
	// longer a question a nil check settles once — and the two actuation verbs have to settle it
	// BEFORE they take the latch, or a session with no launcher shows a frame of "unloading…" in the
	// footer on its way to the same refusal.
	//
	// It is the cheap, synchronous half of the check every verb makes again for itself: answerable
	// on the Update loop from what the binary already holds, never a config read or a dial.
	//
	// nil ⇒ unknown, and the verbs are let through to answer for themselves — the posture of every
	// hand-built Options, and the right one for a Driver whose seams cannot change their mind.
	LauncherEnabled func() bool

	// Schedules is the scheduler this session's Schedules live in (the [Scheduler] seam the binary
	// backs with a live schedule.Scheduler); nil ⇒ scheduling is unwired and both verbs say so.
	// The Model drives it synchronously on the Update loop — Add, Stop and List touch no engine
	// and open no Exchange, which is why /schedule may be typed mid-task.
	//
	// It is wired TOGETHER with the event route that answers it: the binary hands the library a
	// Notify seam that sends a scheduleEventMsg into the running program, and the notices those
	// events render are the only confirmation a created or stopped Schedule gets — this surface
	// deliberately adds no second sentence of its own (scheduleEventNote).
	Schedules Scheduler

	// ScheduleAutoBlocked is why a Schedule may NOT be created in auto mode on this host — the
	// reason half of the same Auto-eligibility ladder that gates launching in auto (ADR 0033,
	// decision 3: a schedule's mode is chosen explicitly and is never silently escalated). Empty ⇒
	// auto schedules are allowed.
	//
	// One string is both the gate and the wording, so the disabled picker row and the refusal the
	// `auto` argument earns can never disagree about whether — or why — auto is unavailable. The
	// binary resolves it (the renderer imports no platform knowledge, exactly as [ConfinementInfo]
	// keeps internal/platform out of here) and the value stands for the process lifetime.
	ScheduleAutoBlocked string

	// ReportActivity publishes what this session is DOING, as a fact and never as a request: true
	// while a worker owns the Agent (an Exchange, a compaction, a blocked approval or question), a
	// launcher verb owns the server, or a row the human typed is still waiting to go out — false at
	// the boundary where none of that holds. It is called from the Update loop on TRANSITIONS of that
	// value, so a listener sees each change once and nothing between them.
	//
	// It exists for the scheduler's Gate (ADR 0033). A Firing is a SECOND Agent against the same
	// single-slot server, so a due one waits for this session to be quiescent rather than contending
	// with the task in front of the human — and the release point is the Exchange's end rather than a
	// Turn's, because Exchanges span Turns (ADR 0025) and a Firing let go mid-Exchange is exactly the
	// contention the Gate exists to prevent. The renderer publishes and decides nothing: what the
	// value MEANS for a Firing is the binary's Gate, and the cycle policy behind it is the library's.
	//
	// nil ⇒ nobody is listening, which is the whole of the degrade.
	ReportActivity func(busy bool)

	// Resumed is the startup-replay payload when this run resumes a stored session (--resume or
	// --continue); nil on a fresh start. newModel seeds the start-up box as usual, then repaints
	// the resumed scrollback beneath it and relights the context gauge from the stored fill — or,
	// when no scrollback was recorded (a legacy session) or the blob will not decode, degrades to
	// an honest note with the view otherwise fresh. The binary resolves the store record and
	// projects it onto this small value, so the renderer never decodes the record itself.
	Resumed *ResumedSession
}

// colorScheme is the palette to build the theme from: what the binary resolved, or the built-in
// default when nothing was wired. The zero Scheme carries no colours at all — every role is the
// empty string — so it cannot be handed to newTheme as if it were a palette; hand-built Options
// (every renderer test, and any future Driver that does not care about colour) leave it zero and
// mean "whatever apogee ships with", which is exactly Default().
func (o Options) colorScheme() scheme.Scheme {
	if o.ColorScheme == (scheme.Scheme{}) {
		return scheme.Default()
	}
	return o.ColorScheme
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
// footer's host alias once the session is on that server — because the name IS the entry's identity
// in the binary's `servers:` list, the single definition of what servers exist: the alias of the
// server you are on is the name you call it (ADR 0036 decision 1). Endpoint is shown beside it and
// is the identity the picker marks the CURRENT row by (string-equal to [Options.Endpoint]); the
// comparison is the picker's own, since the binary builds the list from `servers:` verbatim and
// prepends a row only for an ephemeral override start (ADR 0036 decision 6).
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

// PreboundReason names why a session started with NO upstream bound — the three answers ADR 0036
// gives when the config alone cannot say which server to start on. It is a fact the binary resolved
// (it read the file, the flags and the environment); the renderer only renders it and, on the first
// two, asks the human through the picker it already has.
//
// The empty value is the ordinary start: a server WAS determined and the engine was constructed
// before the program began.
type PreboundReason string

const (
	// PreboundFirstBoot: servers are configured, none is chosen yet (no `server:` recorded). The
	// picker asks, and the choice is what records one.
	PreboundFirstBoot PreboundReason = "first-boot"
	// PreboundStaleChoice: the recorded `server:` names an entry the list no longer carries — a
	// renamed or deleted server, or a typo. It is state, not intent: the picker fixes in one
	// keystroke what a refusal would send to file surgery.
	PreboundStaleChoice PreboundReason = "stale-choice"
	// PreboundNoServers: nothing is configured at all, so there is nothing to pick from and the
	// guidance is to add a server to config.yaml.
	PreboundNoServers PreboundReason = "no-servers"
)

// PreboundStart says whether this session started with an upstream bound, and if not, why. The zero
// value — an empty Reason — is the ordinary bound start, so a hand-built Options describes today's
// behaviour without naming this field at all.
type PreboundStart struct {
	Reason PreboundReason
	// Name is the `server:` value that named no entry, carried for PreboundStaleChoice so the
	// notice can say which one went missing. Empty for every other reason.
	Name string
}

// LaunchProfileChoice is one Launch profile `/model` offers (CONTEXT.md: a Launch profile
// is the LAUNCH-side description of a model — model file, server, flags — owned by the launcher's
// config, opposite the request-side Model profile). Name does two jobs with one value, the
// [ServerChoice] shape: it labels the row, and it is the name [Options.LoadProfile] is called with.
// Everything else is what the choice is made on and is display only.
//
// It carries the launcher's facts PROJECTED, never a launcher type — the same posture that keeps
// heartbeat.Beat out of the facade's vocabulary — so the renderer holds no dependency on the library
// and this seam survives a facade that changes shape.
type LaunchProfileChoice struct {
	Name          string // the profile's key in the launcher config: the row's label and the load argument
	Backend       string // the server it runs on — llamacpp, ollama, lmstudio
	Addr          string // the host:port it would serve at; "" when the launcher could not resolve one
	ContextWindow int    // the merged context_size, or 0 for UNKNOWN — unset leaves the server's own default
	Running       bool   // discovery attributes a live instance to this profile right now
}

// ProfileLoadResult is what a completed profile load hands back: whether the session has to MOVE to
// follow the profile, and — when it does — the move itself, RESOLVED but not yet performed, as one
// call that answers with the same [ServerSwitchResult] a `/server` switch produces, so the completion
// fold that already exists understands both without a second shape (ADR 0029 D2).
//
// Move nil is the ordinary local case, not a failure: the profile loaded into the very server this
// session is talking to, nothing needs re-pointing, and the next beat observes the model change and
// rebinds through the ordinary path.
//
// Move is a CALL rather than three fields because of WHERE it must run. The load itself blocks for
// minutes on a Cmd goroutine, while the move mutates the engine (Agent.SwitchUpstream) and the
// Update loop is the only boundary that synchronizes such a mutation against the heartbeat's own
// rebinds — so the seam resolves the move where it has the launcher's answers (the endpoint, the
// alias, that server's key) and the completion fold COMMITS it where mutating is safe. It also keeps
// the credential out of the renderer, exactly as [ServerChoice] does: this package carries the move,
// never its inputs. An error means nothing moved — the engine's own switch is validate-then-commit —
// so the session stays where it was and the failure is a transcript note.
//
// Notices are the launcher's own lines worth telling the human — config warnings, the drift notice a
// non-restarting activation emits — surfaced in order as transcript notes. They are carried even
// alongside an error, because a load that failed after warning about the config still warned.
type ProfileLoadResult struct {
	// Move commits the resolved move and reports what the display adopts. nil ⇒ the session stays on
	// the server it is already on. Called at most once, on the Update goroutine.
	Move    func() (ServerSwitchResult, error)
	Notices []string // launcher notices to surface as transcript notes, in order
}

// ActuationResult is what `/unload-model` and `/stop-server` did: the launcher's own [StopResult]
// projected. Steps are the orchestration steps it recorded, in order, rendered one transcript note
// each — and they are present even on an error, carrying how far the verb got before it failed.
//
// ServerStopped tells the two unload outcomes apart, which is the whole reason `/unload-model` can
// be honest about what it did: on a MANAGED backend the model is baked into the server's process
// arguments, so unloading it means stopping the server (true), while an external backend takes an
// API unload and keeps running (false). `/stop-server` always reports true on success.
//
// Backend and Addr name WHAT was acted on — the instance discovery answered the session's endpoint
// with. The renderer cannot derive either and the steps do not carry them: the launcher's own steps
// are terse and subject-less ("Sending stop signal", "Unloading model"), and the session holds an
// endpoint URL rather than the address the launcher manages or the name of the server program
// answering there. They are display facts only, and empty when the launcher did not say.
type ActuationResult struct {
	Steps         []string // the orchestration steps taken, in order; present even on failure
	ServerStopped bool     // the server process itself was stopped, not just its model freed
	Backend       string   // the acted-on server's backend — llamacpp, ollama, lmstudio; "" when unknown
	Addr          string   // the host:port acted on, as the launcher spells it; "" when unknown
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

// SettingKind is what a settings row HOLDS, which is what decides how the pane renders the value
// and which edit idiom ⏎ opens on it. The binary PROJECTS its own registry kind onto this
// vocabulary rather than the renderer importing one, the [ServerChoice] posture: internal/tui
// knows how to toggle a bool, pick an enum value and buffer a string or an int, and knows that a
// structured value is one it must not try to edit at all. An unknown kind is therefore not a
// worry the pane carries — a value it cannot edit reads as structured, which is the safe end.
type SettingKind string

const (
	SettingBool       SettingKind = "bool"
	SettingInt        SettingKind = "int"
	SettingString     SettingKind = "string"
	SettingEnum       SettingKind = "enum"
	SettingStructured SettingKind = "structured" // a list, a map, or a block
	// SettingText is a multi-line text value — the system prompt. It is not a string row with a
	// longer value: no row holds prose, so the pane's [SettingRow.Value] carries a summary of it
	// ("8 lines") and the text itself travels in [SettingRow.Text], which is what ⏎ opens a
	// multi-line editor over (ADR 0037 decision 10). ctrl+s commits it, esc discards it, and ⏎ is
	// free to mean a newline — the one edit idiom in this pane whose keys are not the list's.
	SettingText SettingKind = "text"
	// SettingServer is the `server:` row: an enum whose vocabulary is not in the row at all but in
	// Servers above, because what this key may hold is whatever THIS config's `servers:` block
	// names — a list the human can change mid-session. It picks from the same sub-list an enum
	// does and never opens a text buffer, and its ⏎ is a SWITCH rather than a write: the session
	// moves (SwitchServer) and the move records the choice (RecordServerChoice, ADR 0036
	// decision 2), which is this key's whole persistence.
	SettingServer SettingKind = "server"
)

// SettingSource is which precedence source supplied the value a row shows. The zero value is the
// ordinary case — the config file, or the built-in default below it — and the other two are the
// higher-precedence sources that BEAT the file for that key this run (flag > env > file > default).
//
// The pane needs it for one reason: a row the environment is overriding must say so, because a
// value the file does not contain would otherwise look like the file's, and an edit persisted into
// the file would appear to do nothing for as long as the override stands.
type SettingSource string

const (
	SettingFromFile SettingSource = ""     // the config file or the built-in default; nothing overrode it
	SettingFromEnv  SettingSource = "env"  // an APOGEE_* environment variable won
	SettingFromFlag SettingSource = "flag" // an explicitly-set command-line flag won
)

// SettingRow is one row of the `/settings` pane: a config key as the binary resolved it this run.
// It is plain data — the [ServerChoice] posture — projected from the binary's declarative key
// registry, so the renderer never reads the config schema, the file, or an environment variable,
// and cannot disagree with the surface that writes them.
//
// Value is the EFFECTIVE value, formatted as the config file would spell it ("true", "32768",
// "ask-before"), with a structured block summarized instead ("3 servers") — never a YAML fragment,
// because nothing in the pane can edit one. Default is the built-in default in that same spelling,
// so the two are comparable on sight; an empty Default means the key defaults to unset.
//
// A Masked row's Value is ALREADY the mask (`••••`) — the secret is not carried at all. That is
// deliberate: a value the renderer never holds cannot reach a transcript, a paint cache, or a
// crash dump, and the pane has no use for it (item 8's editor buffers what the human types, it
// never reveals what was stored).
//
// EditPointer is where a row this pane will not write IS edited — the ⏎-opens-an-editor affordance
// for a structured block, "use /confine" for the confinement keys, whose acknowledgement interlock
// stays single-homed in `/confine` (ADR 0012). It is non-empty exactly when Editable is false.
type SettingRow struct {
	Path    string      // the key's yaml path with `.` between levels ("ui.spinner") — its display key and its identity
	Section string      // the section header this row sits under, matching the config template's own grouping
	Kind    SettingKind // what the key holds, and with it the edit idiom ⏎ opens
	Value   string      // the effective value as the file would spell it; a mask when Masked, a summary when structured
	Default string      // the built-in default in the same spelling; "" ⇒ the key defaults to unset

	// Text is the value ITSELF for a [SettingText] row, where Value is only a summary of it — the
	// prose a multi-line editor opens on, and empty for every other kind. It is a field of its own
	// rather than a longer Value because the two are read in different places: the row paints the
	// summary and the editor is seeded with the text, and a row that painted its prompt would be a
	// pane's worth of text on one line.
	Text string

	// Source and SourceName are the override marker: which higher-precedence source beat the file
	// for this key this run, and what it is CALLED ("APOGEE_MODE", "--mode") so the note can name
	// it. SourceName is empty exactly when Source is [SettingFromFile].
	Source     SettingSource
	SourceName string

	EnumValues  []string // the closed vocabulary, non-empty exactly for [SettingEnum] ([SettingServer] reads Options.Servers instead)
	Editable    bool     // this pane may write the key — and, since ADR 0037, apply it on the same ⏎
	Masked      bool     // Value is a mask, not the value (api-key)
	EditPointer string   // where a non-Editable key is edited instead; "" exactly when Editable
	Desc        string   // the one-line description shown for the selected row

	// ExternalEdit says ⏎ on this row opens the human's own editor ([Options.ExternalEditSpec]).
	// It is DECLARED by the binary rather than inferred from the kind, for [SettingServer]'s reason:
	// the confinement keys are structured and read-only too, and their interlock stays single-homed in
	// `/confine` (ADR 0012), so "which read-only rows open an editor" is a fact about the schema and
	// not a shape the renderer can read off a row. False for every editable row — those are written
	// here — and false for the confinement pair, whose own pointer says where they go instead.
	ExternalEdit bool
}

// EditorCommand is one resolved external edit — the OUT half of the round trip
// ([Options.ExternalEditSpec]): what to run, and whether this terminal goes with it.
//
// Both facts are the binary's, for the same reason the argv is: which programs need a tty is a fact
// about the PROGRAMS, resolved beside the ladder that named one (ADR 0041 decision 6), and a
// renderer that classified editors itself would be holding a table of the world it has no business
// holding (ADR 0011's thin renderer).
type EditorCommand struct {
	// Argv is the program and its arguments — the editor, its flags, the line jump for the editors
	// that take one, and the config file. Empty means there was nothing to run and the row says so.
	Argv []string

	// Detached says this program must NOT be handed the terminal: it is started without the TUI's
	// stdin/stdout, nothing waits for it, and the pane stays up while it is open — a GUI editor, or
	// a desktop opener stub that returns before the editor is even on screen. False keeps the
	// suspending path this seam has always had, which is the only way a terminal editor is usable at
	// all, and false is also the ZERO value on purpose: a Driver that answers with an argv alone
	// gets exactly today's behaviour.
	Detached bool
}

// AppliedSetting is one key a config reload found CHANGED — the return half of the `$EDITOR` round
// trip (ADR 0037 decision 5), one entry per key whose value came back different from what the file
// said when the editor was launched.
//
// Value is that new value in the same spelling a committed edit carries: the file's own spelling for
// a scalar ("true", "32768"), the row's summary for a block ("3 servers"), and the prose ITSELF for
// a [SettingText] key, whose row shows only a summary of it. That is what makes one struct enough
// for both halves of what the pane does with it — journal the key (its ` *` marker and its new
// value) and apply it, through exactly the two homes an in-pane commit applies through.
//
// It carries no note and no error: nothing is in force yet when the reload returns it, so what a
// key had to say about landing is what the apply says, on the row, a moment later.
type AppliedSetting struct {
	Path  string // the key's registry path, as [SettingRow.Path] spells it
	Value string // its new value, in the spelling the pane journals and applies
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
	// The alternate screen is claimed HERE, before the program starts, rather than being left to
	// the first frame's AltScreen field — see claimAltScreen for why the order cannot be left to
	// Bubble Tea. Only on a real terminal: with stdout redirected there is no scroll bar to put
	// out and the sequences would be noise in the file.
	if stdoutIsTerminal() {
		release, err := claimTerminalScreen(os.Stdout)
		if err != nil {
			return err
		}
		defer release()
	}
	// The per-Turn snapshot notify: the worker sends turnSnapshotMsg through the Bridge's
	// late-bound program sender (the same programRef the Sink pushes Events through), so the
	// Model persists between Steps without any exported API. Bind (below) resolves it to the
	// live program before the first worker can fire.
	m := newModel(ctx, eng, opts, br.prog.send)
	// The Step-boundary flush: the Sink coalesces adjacent tokens behind a short window, and the
	// worker empties that buffer the instant a Step returns, so no token is ever delivered after
	// the Step that emitted it (worker.go, sink.go). It is wired HERE rather than through newModel
	// because the sink is the Bridge's, and Run is where the two meet.
	m.flushEvents = br.sink.flush
	// The environment the painter will read: bubbletea's own default (the process's) everywhere
	// but Windows, and on Windows the terminal-naming rule's slice (environ_windows.go). It is
	// resolved HERE, above the diag log rather than at the programOptions call below, because the
	// log has to report the environment the PAINTER sees — on Windows that is not the process's,
	// and a diagnostic that read the process would misreport the one variable it exists to measure.
	environ := programEnviron()
	// The --tui-diag half of the diagnostic seam (diagnostics.go). It is opened HERE, between
	// newModel and the program, because that is the only window in which both halves of its
	// contract can be met: the Model exists, so the log can be put on it before any message
	// reaches Update, and the loop has not started, so the start-up facts are recorded before
	// anything can change them. nil ⇒ off, and every observation point is nil-safe.
	if opts.DiagPath != "" {
		diag, err := newDiagLog(opts.DiagPath)
		if err != nil {
			// Named so the human knows which of the two paths they got wrong; the wrapped error
			// already carries the path and what was refused about it.
			return fmt.Errorf("--tui-diag: %w", err)
		}
		defer func() { _ = diag.Close() }()
		m.diag = diag
		diag.start(os.Getenv, environ, m.th.measure.Method())
	}
	// The --tui-trace half. traced is nil unless a path was named, in which case it is the file
	// this run owns and must close; the options are otherwise exactly what they have always been.
	// Two platform rules can also add an option here, both no-ops off Windows: environ is the
	// terminal apogee names itself to the painter as (environ_windows.go), and
	// programDeclinesSyncOutput is the mode-2026 question it keeps to itself (syncoutput.go).
	teaOpts, traced, err := programOptions(ctx, opts, environ, programDeclinesSyncOutput())
	if err != nil {
		return err
	}
	if traced != nil {
		defer func() { _ = traced.Close() }()
	}
	program := tea.NewProgram(m, teaOpts...)
	// Bind before Run: the program exists now, and the first Send cannot occur until a
	// worker is launched, which only happens after the user submits into the running loop.
	br.Bind(program)
	_, err = program.Run()
	return err
}

// programOptions builds the Bubble Tea options [Run] starts the program with, and opens the
// traced output when [Options.TracePath] named one — the two are one decision, since the traced
// output IS an option and is also a file the caller has to close.
//
// environ is the environment the painter should read, or nil to leave bubbletea on os.Environ()
// — the Windows terminal-naming rule, and nothing at all anywhere else (environ_windows.go).
// declineSyncOutput asks for the mode-2026 filter over the output — the other Windows-only rule
// (syncoutput.go). Both arrive as parameters rather than being read in here so both of their
// branches are testable without a real terminal underneath the test.
//
// It is a function of its own so a test can pin the thing this seam most needs pinning: with
// nothing switched on, the program is constructed with EXACTLY the option it has always had and no
// wrapper at all. An always-on wrapper would be invisible in every other test while quietly
// changing what the renderer believes about its terminal on every run — see tracedOutput.
func programOptions(ctx context.Context, opts Options, environ []string, declineSyncOutput bool) ([]tea.ProgramOption, *tracedOutput, error) {
	teaOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if environ != nil {
		teaOpts = append(teaOpts, tea.WithEnvironment(environ))
	}
	out, traced, err := programOutput(opts.TracePath, declineSyncOutput)
	if err != nil {
		return nil, nil, err
	}
	if out == nil {
		return teaOpts, nil, nil
	}
	return append(teaOpts, tea.WithOutput(out)), traced, nil
}

// programOutput builds the terminal bubbletea paints into, and returns it beside the traced output
// the caller has to close — nil for the second when --tui-trace named no path, and nil for BOTH
// when neither wrapper is wanted, which is the signal to leave tea.NewProgram on its own default
// output (os.Stdout, tea.go:620).
//
// The stacking order is the decision this function exists to hold: bubbletea → stripper → tracer →
// os.Stdout. The stripper is nearest bubbletea so the trace records the bytes that actually reach
// the terminal rather than the ones bubbletea offered, because a trace is evidence only while it
// agrees with the wire — the 2026-08 investigation diffs traces against pseudoconsole captures
// byte for byte. Every layer is a term.File answering Fd() with os.Stdout's descriptor, so however
// many are stacked the renderer sees the terminal it has always seen.
func programOutput(tracePath string, declineSyncOutput bool) (term.File, *tracedOutput, error) {
	if tracePath == "" && !declineSyncOutput {
		return nil, nil, nil
	}
	var out term.File = os.Stdout
	var traced *tracedOutput
	if tracePath != "" {
		t, err := newTracedOutput(os.Stdout, tracePath)
		if err != nil {
			return nil, nil, fmt.Errorf("--tui-trace: %w", err)
		}
		traced, out = t, t
	}
	if declineSyncOutput {
		out = newSyncQueryStripper(out)
	}
	return out, traced, nil
}

// The three screen-control sequences apogee sends on its own behalf. Everything else on the wire
// is the renderer's.
const (
	ansiEnterAltScreen  = "\x1b[?1049h" // switch to the alternate screen, saving the primary one
	ansiExitAltScreen   = "\x1b[?1049l" // switch back, restoring the primary screen
	ansiEraseScrollback = "\x1b[3J"     // drop the terminal's saved lines (xterm E3)
)

// claimTerminalScreen is apogee's terminal prologue: everything [Run] does to the terminal before
// the program starts, and the one release that undoes all of it. It is a function of its own — and
// not four lines inside [Run] — because the ORDER of what it does is the fix for the Windows
// ghosting bug, and an ordering nothing can test is an ordering the next refactor can quietly
// reverse (conpty_windows_test.go drives this function for exactly that reason).
//
// The order, and why it is that order:
//
//   - The console mode comes FIRST, before the alternate-screen switch. On Windows the mode word is
//     per screen buffer, so a flag set once the alternate buffer is already live lands on the buffer
//     nobody writes to any more — which is how bubbletea's own DISABLE_NEWLINE_AUTO_RETURN was being
//     defeated, and with it every bare LF the renderer emits. altscreen_windows.go carries the full
//     mechanism. Everywhere else prepareAltScreenConsole is a no-op.
//   - The release then runs in the mirror order: the exit sequence is written while the mode this
//     set is still in force (it is what makes the console interpret the sequence at all), and only
//     then is the shell's own console mode put back, on the primary buffer the exit returned to.
//     A caller that defers the release therefore covers every later error return and every panic
//     with one defer, and the console mode is the last thing to go back either way.
//
// The exit sequence is written unconditionally on release, not only when the renderer failed to
// restore. The renderer restores the primary screen on every shutdown it reaches, but a program
// that dies before it ever paints never entered the alternate screen by its own bookkeeping and so
// never leaves it either — the shell would come back to a screen apogee took and kept. When the
// renderer did restore, this second one lands on a primary screen already restored, where it is a
// cursor restore to the position it just returned to.
func claimTerminalScreen(f *os.File) (func(), error) {
	restoreConsole := prepareAltScreenConsole(f)
	if err := claimAltScreen(f); err != nil {
		restoreConsole()
		return nil, err
	}
	return func() {
		_, _ = io.WriteString(f, ansiExitAltScreen)
		restoreConsole()
	}, nil
}

// claimAltScreen switches w to the alternate screen and then erases the terminal's saved lines,
// in that order and in one write.
//
// The order is the whole point. macOS Terminal.app copies the primary screen into its scrollback
// when a program switches to the alternate screen, and its own scroll bar then stays lit for the
// entire run — the thing llama-launcher never suffers, because it never leaves the primary screen
// at all. Erasing the saved lines afterwards puts that scroll bar out; erasing them first clears a
// scrollback the switch immediately refills, which is why this cannot be a command returned from
// Init: Bubble Tea writes RawMsg straight through, but defers the alt-screen switch to the next
// ticker-driven flush, so an Init command reliably lands BEFORE the switch — the useless order.
//
// Claiming the screen up here makes the renderer's own switch, one frame later, a no-op on a
// screen that is already alternate: the primary buffer is untouched by it, so nothing new reaches
// the scrollback, and the renderer's shutdown restores the primary screen exactly as it always did.
//
// The cost is deliberate and documented in layout.md: the shell scrollback from before the launch
// does not survive apogee starting. That is the trade the scroll bar demands — there is no
// sequence that hides it while leaving the saved lines in place.
func claimAltScreen(w io.Writer) error {
	_, err := io.WriteString(w, ansiEnterAltScreen+ansiEraseScrollback)
	return err
}

// stdoutIsTerminal reports whether stdout is a character device, i.e. something with a scroll bar
// to worry about. A redirected stdout gets no screen-control sequences from us.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
