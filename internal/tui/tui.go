package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/undo"
)

// SkillCatalog is the read-only view of the discovered skills the TUI needs: the full sorted
// list for the merged "/" menu (List), a by-id lookup that resolves an inline "/token" (Get), and the
// files discovery could not load (Skipped) so /skills can say why a skill is missing instead of
// silently omitting it — with Report pairing those last two off one snapshot for the /skills
// report itself. It is satisfied by *skills.Catalog; the TUI depends only on this
// interface so it stays unit-testable with a fake, and — being an interface — it is a reference
// header safe to hold in the value-copied Model (ADR 0011). A nil catalog means no skills are
// wired; every reader guards for it.
type SkillCatalog interface {
	List() []skills.Skill
	Get(id string) (skills.Skill, bool)
	Skipped() []skills.SkipError
	// Report returns List's skills and Skipped's failures read off ONE catalog snapshot — what the
	// /skills report takes, so its two halves always describe a single scan. Behind a reloadable
	// catalog (skills.Provider) the single-accessor pair is racy for this use: a rescan can swap
	// the snapshot between the List call and the Skipped call, and the report would then pair a
	// fresh listing with stale failures. The seam stays behavioural — the Driver asks for the
	// answer it needs, it never reaches for the engine's snapshot itself (ADR 0031).
	Report() (list []skills.Skill, skipped []skills.SkipError)
	// Suggest ranks the catalog against the message being typed and returns the closest skills,
	// strongest first — the suggestion band's whole input (suggestband.go, ADR 0061). The ranking
	// is the ENGINE's (skills.Catalog.Suggest): a Driver decides how to show a suggestion, never
	// what a suggestion is. exclude, when non-nil, drops a skill by id before ranking — the band
	// passes the ids the draft already invokes and the ones this session has spent — and limit
	// caps the result (≤ 0 asks for the matcher's own default). A draft with too little evidence
	// in it yields nothing at all rather than a weak guess.
	Suggest(draft string, exclude func(id string) bool, limit int) []skills.Suggestion
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
	// updating that same file thereafter. transcript is the opaque scrollback blob — the neutral
	// wire form internal/session versions, which transcriptbridge.go projects this package's
	// entries onto; title, userMsgs, ctxUsed, usage — the main agent's cumulative token
	// accounting as of this save — and delegateUsage — the sum its sub-agents reported by then —
	// populate the browsable metadata. The two accountings arrive apart and are stored apart: what
	// the SESSION spent is their sum, which is the browser's business rather than the host's.
	Save(
		sess domain.Session,
		transcript []byte,
		title string,
		userMsgs, ctxUsed int,
		usage, delegateUsage session.Usage,
	) error
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

// SettingsHost is the `/settings` pane's whole seam: the rows it shows, the two writes a committed
// row makes to the config file, and the live apply that puts a persisted key into effect on the
// running session — ADR 0035's one key per deliberate edit and ADR 0037's validate → persist →
// apply, named as one host capability rather than spelled as four bare funcs (ADR 0054). It is
// defined here like [SessionHost], so the renderer stays unit-testable with a fake while the
// composition root owns the key registry, the schema, the file format, and the resolution from a
// file-spelled value onto whatever live seam the key moves.
//
// A nil host means `/settings` is unwired whole: the pane has nothing to show and says so, the
// nil-seam degrade every seam in [Options] takes. A host that IS wired but cannot do one of the
// four says so in that method's own answer — no rows, an error out of Write or Reset, an Apply
// that reports nothing — so a Driver that persists without applying (ADR 0031) needs no second
// interface to say it.
type SettingsHost interface {
	// Rows lists every config key the pane shows, in the order the config template presents them,
	// as the binary resolved them THIS run (see [SettingRow]). It is asked on every paint rather
	// than snapshotted, for the reason the picker's rows are derived at render time (picker.go):
	// a row re-read after an edit reflects what the edit made of it, and a selection is clamped
	// against rows that are current.
	//
	// The binary owns everything behind it — the key registry, the schema, the precedence that
	// decided which source won, the masking of a secret — exactly as [Options.SaveHostAcknowledgement]
	// owns the file format. No rows ⇒ `/settings` has nothing to show and says so.
	Rows() []SettingRow

	// Write persists one config key — the pane's whole write half (ADR 0035: one key per
	// deliberate edit). path is the row's registry path ("ui.spinner") and value is the value as
	// the file would spell it ("true", "32768", "ask-before"), which is exactly what
	// [SettingRow.Value] and [SettingRow.EnumValues] carry: the renderer hands back a string it
	// was given rather than a YAML fragment it composed, because the binary owns the file format
	// and it alone knows whether the key may be written at all — the registry's editability, the
	// splice, the verification and the atomic write are all behind this one call.
	//
	// It is synchronous like [Options.SaveHostAcknowledgement]: one small file, spliced and
	// renamed, on a keypress the human is waiting on. An error is REPORTED, never swallowed — the
	// pane shows it on the row and treats the key as unchanged — so a read-only config home
	// surfaces as a refusal rather than as an edit that silently did not happen, and a host that
	// cannot write at all says so through the same answer.
	Write(path, value string) error

	// Reset returns one config key to its default by REMOVING the file's line for it (ADR 0035)
	// rather than writing today's spelling of the default into the file, so the key goes back to
	// being described by the binary and documented by its commented example. A key the file does
	// not set is already at its default: that is a no-op, not an error.
	//
	// Same contract as Write in every other respect — synchronous, path-addressed, errors reported.
	Reset(path string) error

	// Apply makes one persisted key take effect in the RUNNING session — the apply half of every
	// `/settings` edit (ADR 0037 decision 1: validate → persist → apply, on the same ⏎). path and
	// value are Write's, so the pane hands the apply exactly what it handed the write and no second
	// spelling of a value exists; the binary resolves that string into whatever the engine seam
	// takes (a mode, a bool, a name list) because it owns the schema, as it owns the file format
	// behind Write.
	//
	// note is a short boundary sentence for a key that cannot land NOW and lands at a boundary the
	// session will cross anyway — "applies at next clear" for the context files, whose prefix is
	// frozen for the session on purpose (ADR 0026). Empty means it is already in effect, which is
	// the answer for almost every key — and the answer a host with no live apply at all gives for
	// every key, leaving the write to stand on its own: the degrade a bench or headless Driver
	// composes deliberately (ADR 0031). It never defers to a restart: a key that could only take
	// effect the next time the process starts has no business in this seam.
	//
	// An error is REPORTED and does not unwind the write (ADR 0037 decision 1): the file already
	// expresses the intent, so the row says "saved — live apply failed: …" and a re-committed edit
	// retries the apply.
	Apply(path, value string) (note string, err error)
}

// SchemeHost is the colour-scheme seam the TUI drives: what can be switched TO right now, what one
// of those names resolves to, and the export that makes an embedded palette editable at all (ADR
// 0040), named as one host capability rather than as three bare funcs (ADR 0054). It is defined here
// like [SettingsHost] and typed against internal/scheme, so the renderer
// SELECTS a palette and never walks a directory or parses a file (ADR 0011's thin renderer), while
// the composition root owns the schemes folder, the discovery and the shadowing rule.
//
// All three read that folder on every ask rather than answering from a snapshot: a scheme file the
// human drops in or edits mid-session is offered by the picker the moment they open it and loaded
// by the next switch, where a list captured at launch would go stale the first time they wrote one.
//
// A nil host means schemes are unwired: nothing is on offer, no live switch is possible, and no copy
// can be written — the settings row still persists the key (the pane's write half is independent)
// and the new scheme takes effect at the next start.
type SchemeHost interface {
	// List names every scheme that can be switched to — the built-ins plus every `*.yaml` in the
	// schemes folder, a user file shadowing a built-in of the same name (ADR 0040 design call 6).
	// Nothing named ⇒ the picker has no vocabulary and the row opens nothing.
	List() []string

	// Resolve turns one of those names back into a palette, warnings already rendered to lines —
	// the same call the binary made at boot ([Options.ColorScheme]), re-run so a switch re-READS
	// the file and an edited scheme lands on the next switch without a restart. Warnings are the
	// forgiving load's only voice (ADR 0040 design call 8): the palette that comes back is always
	// usable, so a defective key is a sentence in the transcript rather than a failure.
	//
	// ok is false when this host cannot resolve at all — the nil-host degrade, offered per member
	// so a Driver may list the schemes without being able to switch to one. The caller then keeps
	// the palette it has, and the row says the new scheme applies at the next start.
	Resolve(name string) (s scheme.Scheme, warnings []string, ok bool)

	// Export writes an editable copy of the named BUILT-IN scheme into the schemes folder and
	// returns the path it wrote. It is the only way a scheme file comes into existence: the
	// built-ins are embedded in the binary and never installed on disk (ADR 0040 design call 1), so
	// without an export there is nothing to open in an editor and the shadowing rule has nothing to
	// shadow with.
	//
	// It never overwrites (design call 7): an existing file is an error naming it, so an export can
	// never destroy the scheme somebody has been working on. Every error — unknown name, file
	// present, unwritable folder, or a host that cannot write scheme files at all — is REPORTED,
	// because a silent export is indistinguishable from one that worked.
	Export(name string) (path string, err error)
}

// ServerHost is the Upstream seam whole: the servers this session can be on, the two ways it
// arrives on one, the choice it records for the next session, and the observation that says what the
// server it is on is actually serving — one host capability rather than six bare funcs (ADR 0054).
// It is defined here like [SettingsHost], so the renderer selects a server and folds a beat while
// the composition root owns every endpoint, key, discovery hint and window pin behind them.
//
// The split is the same for all six acts: the TUI owns WHEN — the human's pick at idle, the cadence
// of the tick chain, the quiescent boundary a rebind waits for (ADR 0024) — and the host owns WHAT,
// because every input to that decision is config the renderer never reads.
//
// A nil host means the Upstream seam is unwired whole: nothing to switch to, no way to bind,
// nothing observing, and nothing recorded — the pre-heartbeat, pre-ADR-0036 behaviour every
// hand-built test Options has. A host that IS wired but cannot do one of the six says so where the
// act is: List answers with no servers and RecordChoice with false, which are already exactly the
// degrades those two have. The other four are acts the renderer decides ABOUT before it decides to
// perform one — a cadence to open, a picker to raise, a ladder rung to word — so it asks
// [ServerHost.Acts] first, and a host answering true there is taken at its word.
type ServerHost interface {
	// Acts says which of the four askable acts this host performs. It is asked wherever the old
	// per-func nil checks were, and for the same reason they could not become "call it and see": an
	// unobserved session is not one whose server is unreachable, a display-frozen heartbeat must not
	// capture a change it cannot apply, and `/server` refuses up front rather than opening a picker
	// whose accept could move nothing.
	Acts() ServerActs

	// Beat is one observation of the Upstream: is the server reachable, which model is it
	// serving, in which context window, and what else does it advertise (internal/heartbeat). The
	// binary backs it with a live heartbeat.Monitor; the TUI owns only the CADENCE and the
	// consequences — it fires one beat from Init, re-arms heartbeat.Interval after each LANDED beat
	// (so beats can never overlap), renders the offline state, and refuses a send while the server
	// is not there to answer it. A beat is never an error: an unreachable server is a finding the
	// Beat itself carries, which is why the seam returns no error.
	//
	// It is called from a Cmd goroutine rather than from Update, and only while [ServerActs.CanObserve]
	// says something is watching at all.
	Beat(ctx context.Context) heartbeat.Beat

	// Rebind re-resolves and applies the per-model bindings after the heartbeat observed the
	// upstream serving a different model — or the same model in a different window. The binary owns
	// the resolution (the per-model system prompt, ADR 0023; the validated set, ADR 0016; the
	// mechanisms registry and the compaction budget) and the engine mutators; the TUI owns only
	// WHEN, which is the whole of its half: at idle the moment the beat lands, or deferred to the
	// exchange-terminal fold when a worker owns the engine — the quiescent boundary Agent.Rebind
	// demands (ADR 0024). It returns what was actually BOUND, which is not always what was observed
	// (a `context-window:` pin outranks the server's window), plus any notices to surface.
	//
	// effortDialect is the wire shape the SERVER reads a thinking-effort intent in, as the beat
	// that carried this observation reported it (ADR 0060). The TUI passes it through untouched:
	// it is the one effort fact the engine needs, and the only one that crosses — what a model
	// reports as its level vocabulary and its default stays here, in [heartbeatState]. The zero
	// value is "this server advertises no dial", which keeps the historical `chat_template_kwargs`
	// shape and so reproduces the request bytes that predate the dialect seam.
	Rebind(model string, contextWindow int, effortDialect provider.EffortDialect) (RebindResult, error)

	// List names the upstream servers this session can be switched to — the `/server` picker's
	// rows and the `/settings` server row's popup, in the order the binary assembled them: every
	// `servers:` entry from config.yaml, preceded by the endpoint this session started on whenever
	// no entry already names it (so the way back is always offered). It is display and identity
	// only; what a switch actually needs to talk to a server stays in the binary, behind Switch.
	//
	// It is asked on every ask rather than snapshotted, the [SettingsHost.Rows] posture: the
	// `servers:` block is itself something the human can change mid-session (ADR 0037's `$EDITOR`
	// jump), so the list is drawn from what the file says at the moment it is drawn rather than from
	// what it said at launch — an entry added a moment ago is offered without restarting apogee.
	//
	// Nothing named ⇒ nothing to switch to, and `/server` says so rather than opening an empty
	// overlay.
	List() []ServerChoice

	// Switch moves the whole session to the server named name: the binary re-points the
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
	// session where it was — which is also how a host that cannot switch at all answers.
	Switch(name string) (ServerSwitchResult, error)

	// Bind constructs the session's engine on the named `servers:` entry — the pre-bound
	// half of Switch, and the only seam that can end the pre-bound state. It answers with the
	// same [ServerSwitchResult] a switch does, so the display adopts a first binding exactly as it
	// adopts a move.
	//
	// It binds ONCE: a session that already has an engine is answered with an error naming
	// Switch's own verb, because a second construction would leave the first engine running
	// with nothing holding it. Like Switch it is called synchronously on the Update loop and
	// opens no connection of its own — the first beat of the Monitor it installs is what discovers
	// the server. A host that cannot bind answers through the same error, and a pre-bound session
	// then has no way out of the pre-bound state; the binary always wires it beside Switch.
	Bind(name string) (ServerSwitchResult, error)

	// RecordChoice persists the entry this session starts on NEXT time — the `server:` key
	// ADR 0036 decision 2 records on every move to a configured entry. The renderer calls it with the
	// name it just bound or switched to and knows nothing else about it: whether that name belongs to
	// a configured entry (and is therefore worth writing) is the binary's question, because only the
	// binary can tell a configured row from the synthesized one an override startup earns.
	//
	// It answers whether it WROTE, which is what lets the renderer state the recording beside the
	// move ("· server: saved") without claiming one for the moves the binary skips: a name in no
	// `servers:` entry is skipped silently, and false with no error is exactly that outcome — the
	// answer a host that records nothing gives for every name, leaving every switch session-scoped,
	// which is the behaviour this key was introduced to replace.
	//
	// It is best-effort persistence of something that ALREADY happened: the session moved before this
	// is called and stays moved whatever it answers, so an error is a note and never an undo. Like
	// [SettingsHost.Write] it is synchronous — one small file, spliced and renamed — and called on the
	// Update loop.
	RecordChoice(name string) (recorded bool, err error)
}

// ServerActs is a [ServerHost]'s own answer to which of its acts it actually performs — the
// per-member half of the nil-means-unwired contract (ADR 0054 decision 3), for the four acts whose
// absence the renderer has to know about BEFORE it acts rather than after. The zero value is the
// unwired host, which is what a nil [Options.Server] answers with, so one shape covers both
// granularities and no caller writes two checks.
//
// The other two acts need no flag: [ServerHost.List] says it by naming no servers, and
// [ServerHost.RecordChoice] by answering false, which are the same answers those acts give for a
// list that is empty and a name no `servers:` entry holds.
type ServerActs struct {
	// CanObserve is whether anything watches this session's Upstream at all. False opens no tick
	// chain, folds no beat, refuses no send and leaves the footer with what launch gave it — the
	// pre-heartbeat renderer. [ServerHost.Beat] cannot say it: an unreachable server is a finding a
	// beat CARRIES, never a statement that nobody is looking.
	CanObserve bool
	// CanRebind is whether an observed change can be acted on. False is the display-frozen
	// heartbeat: beats still land and still light the offline state, but nothing is captured,
	// nothing moves, and `/model` says the display is read-only.
	CanRebind bool
	// CanSwitch is whether this session can move to another server at all. False is one situation
	// with an empty list for the human — there is nowhere to go — and `/server` says so with one
	// sentence rather than opening an overlay it cannot honour.
	CanSwitch bool
	// CanBind is whether a PRE-BOUND session can leave that state (ADR 0036 decision 3). False
	// leaves the notice and the guidance without the picker they would otherwise open, and answers
	// an accept that reached the seam some other way with a note.
	CanBind bool
}

// LauncherHost is the llama-launcher seam whole: the Launch profiles this machine can be made to
// serve, the verb that activates one, the two verbs that free or stop the server the session is on,
// the pointer a committed load records for the next session, and the boot check that acts on that
// pointer — one host capability rather than seven bare funcs (ADR 0054). It is defined here like
// [ServerHost], so the renderer offers rows and narrates an actuation while the composition root
// owns every config path, address and library type behind them — ADR 0029 D1's rule that no
// launcher vocabulary reaches this package.
//
// The split is [ServerHost]'s exactly: the TUI owns WHEN — the human's pick at idle, the actuation
// latch that serializes every blocking verb, the one boot check [Model.Init] issues — and the host
// owns WHAT, because the launcher's config, the discovery sweep it drives and the addresses they
// imply are things the renderer never reads.
//
// A nil host means the integration is not configured on this machine (`llama-launcher: off`, or
// auto-detect found no launcher config): `/model` then offers what the server itself advertises,
// `/unload-model` and `/stop-server` degrade to a note naming the key, no load is recorded and no
// boot restore is attempted. One answer covers the whole family because these seams were always
// wired together or not at all — the posture of every hand-built Options, and of every Driver that
// is not the interactive TUI.
//
// A host that IS wired says the rest for itself, in two different ways. Whether the integration is
// on RIGHT NOW moved INSIDE the seams when `llama-launcher:` became live-editable (ADR 0037): the
// key belongs to a `servers:` entry, so enablement follows the session's server, and every verb
// answers [ErrNoLauncher] while it is off — the same sentence the nil host above says. Whether this
// host performs the boot restore AT ALL is the one thing a caller must know BEFORE it calls, since
// Init either issues that Cmd or does not, so it is reported by [LauncherHost.Acts] instead
// (ADR 0054 decision 3a).
type LauncherHost interface {
	// Acts says which of this host's acts the renderer has to know about before it performs one.
	// It is asked where the per-func nil check was, and for the same reason it could not become
	// "call it and see": issuing the call IS the act.
	Acts() LauncherActs

	// Enabled reports whether the llama-launcher integration is switched ON right now. Since
	// `llama-launcher:` became editable mid-session (ADR 0037) the verbs below can be wired for the
	// life of the session with the answer moving INSIDE them, so "is there a launcher here" is no
	// longer a question one check settles once — and the two actuation verbs have to settle it
	// BEFORE they take the latch, or a session with no launcher shows a frame of "unloading…" in the
	// footer on its way to the same refusal.
	//
	// It is the cheap, synchronous half of the check every verb makes again for itself: answerable
	// on the Update loop from what the binary already holds, never a config read or a dial.
	//
	// A host that cannot answer says true — being unable to tell is not evidence that the
	// integration is off, and the verbs are then let through to answer for themselves, which is the
	// right posture for a Driver whose seams cannot change their mind.
	Enabled() bool

	// Profiles lists the Launch profiles the launcher's config defines — what `/model` offers on a
	// host with a launcher, re-read FRESH every time the picker opens (ADR 0029 D4), so a profile
	// added in the launcher's own TUI a moment ago is offered here without restarting apogee. The
	// binary owns the read because it is the only layer that knows the launcher exists at all; the
	// renderer receives rows it can label and pick from, and nothing else.
	//
	// The error is the one failure that sinks the list — no config at a configured path, a config
	// that will not parse — and reaches the human as a one-line note rather than an overlay. A single
	// profile that cannot be resolved is NOT that failure: it is simply absent from the rows, because
	// one moved model file must not cost the user their other nine profiles.
	//
	// [ErrNoLauncher] is the integration being off rather than a failure of the list, and `/model`
	// reads it as "offer the models the server itself advertises".
	Profiles() ([]LaunchProfileChoice, error)

	// Load activates the named Launch profile and reports what the session must adopt — the
	// composite verb of ADR 0029 D2. The binary drives the launcher (a BLOCKING call: up to ~30 s
	// waiting for health, plus a stop escalation when a restart displaces an occupant, and minutes
	// for a large model), then decides whether the session has to move at all: a profile that
	// resolves to the endpoint this session is already on moves nothing — no Move in the result, and
	// the next beat observes the new model and rebinds through the ordinary path — while one that
	// resolves elsewhere is FOLLOWED with the same fold `/server` performs, reported as a resolved
	// [ProfileLoadResult.Move] the completion fold commits and hands to that fold. No result here is
	// ever a binding; only a [ServerHost.Beat] binds (ADR 0029 D1).
	//
	// Because it blocks, the TUI runs it on a Cmd goroutine and holds the actuation latch across it —
	// that latch is also the per-address serialization the launcher's contract demands of its caller.
	// progress receives the launcher's lifecycle steps one call per step, ON THE CALLING GOROUTINE, so
	// the TUI pumps them into the transcript rather than rendering from them; nil is safe.
	Load(name string, progress func(step string)) (ProfileLoadResult, error)

	// Unload frees the model of the server this session is talking to, and Stop stops that server
	// outright (ADR 0029 D3). Both take the session's endpoint rather than reading one: the renderer
	// is the side that knows which server the session is on — it may have moved since launch — while
	// the binary is the side that knows which addresses the launcher's config implies. An endpoint the
	// launcher does not manage comes back as an error naming it; neither verb ever guesses an address
	// to act on, because the one mistake available here stops somebody else's server.
	//
	// Both BLOCK for the stop escalation (~20 s worst case) and run under Load's latch, for the same
	// serialization reason. The result carries the steps taken EVEN WHEN the error is non-nil — how
	// far a stop got before it failed is exactly what the human needs to know — so the caller renders
	// the steps first and the error after.
	Unload(endpoint string) (ActuationResult, error)
	Stop(endpoint string) (ActuationResult, error)

	// RecordProfile persists the Launch profile a load just COMMITTED as the one this server comes
	// back on NEXT time — the `launch-profile:` key of a launcher-fronted `servers:` entry, written
	// only while the `remember-model:` toggle is on. It is the launcher class's half of what
	// [Options.RecordModelChoice] does for a plain multi-model server: that class remembers a wire
	// model id, this one remembers the profile that loads one, and no entry carries both — a
	// launcher-fronted `model:` is a deliberately empty discovery hint.
	//
	// The renderer calls it with the profile name the load activated and knows nothing else about it.
	// WHICH entry the pointer belongs on is the binary's question in a stronger sense than it is for
	// [Options.RecordModelChoice]: a load can move the session onto a server no `servers:` entry
	// names, and the pointer's home is the ACTUATING entry either way — the one whose
	// `llama-launcher:` key this session's launcher path follows — which only the binary holds.
	//
	// It answers whether it WROTE, exactly as [ServerHost.RecordChoice] does: the toggle off, and an
	// actuating entry that cannot be identified, are both false with no error, and the renderer then
	// claims no recording. A host that records nothing answers that way for every profile, leaving
	// every load session-scoped, which is the behaviour this key was introduced to replace.
	//
	// Only a COMMITTED load reaches it. A load that failed, one whose health wait timed out, and one the
	// session could not follow record nothing, because this key names what the launcher was made to
	// serve rather than what somebody asked for. `/unload-model` and `/stop-server` leave it exactly
	// where it is: freeing the GPU now is not the same as forgetting which model this server runs.
	//
	// It is best-effort persistence of something that ALREADY happened, and like the two seams it
	// sits beside it is synchronous — one small file, spliced and renamed — and called on the Update
	// loop, so an error is a note and never an undo.
	RecordProfile(profile string) (recorded bool, err error)

	// Restore asks the binary, ONCE at start-up, what to do about a `launch-profile:` the session's
	// server was last left on — the boot half of `remember-model:`, whose recording half is
	// [LauncherHost.RecordProfile]. [Model.Init] issues it as one Cmd beside the first beat, so it is
	// answered off the Update loop: answering it re-reads the launcher's config and probes for running
	// servers, neither of which may block the paint. A host that does not answer it at all says so
	// through [LauncherActs.CanRestore], and no Cmd goes out.
	//
	// Every question behind the answer is the binary's — whether the toggle is on, whether the
	// actuating entry carries a pointer at all, whether the launcher still defines that profile, and
	// whether anything is already serving under that launcher. What crosses the seam is a DECISION:
	// load this, say this, or do nothing. The renderer acts on it through the very latch a human's
	// `/model` pick takes ([Model.startProfileLoad]), which is what keeps the restore one more
	// actuation rather than a second way to bind — the latch blocks what it always blocks, a beat
	// landing in its shadow is stashed rather than driven into the engine, and the completion fold
	// commits whatever moved and fires the beat that binds (ADR 0029 D5).
	//
	// The error is the one failure worth a line: a launcher config that could not be read at the path
	// the entry names. It reaches the transcript as a note and is fatal to nothing — the session is
	// bound to whatever start-up bound it, and a restore that did not happen costs a sentence rather
	// than a server.
	Restore() (ProfileRestore, error)
}

// LauncherActs is a [LauncherHost]'s own answer to the acts whose absence the renderer has to know
// about BEFORE it acts rather than after — the per-member half of the nil-means-unwired contract
// (ADR 0054 decision 3a), and [ServerActs]' counterpart for this family. The zero value is the
// unwired host, which is what a nil [Options.Launcher] answers with, so one shape covers both
// granularities and no caller writes two checks.
//
// One act needs a flag and the other six do not. The four verbs say it in their own answers
// ([ErrNoLauncher], the very sentence an absent launcher earns), [LauncherHost.RecordProfile] says
// it by answering false, and [LauncherHost.Enabled] is a report rather than an act.
type LauncherActs struct {
	// CanRestore is whether this host answers the start-up restore check at all. False issues no
	// start-up Cmd, so [Model.Init]'s batch collapses to exactly what it held before the boot restore
	// existed — the posture of every hand-built Options and of every Driver that is not the
	// interactive TUI, which builds no Model and could not reach the seam anyway.
	// [LauncherHost.Restore] cannot say it: "nothing to restore" is a decision the host MADE, never a
	// statement that it makes none.
	CanRestore bool
}

// DelegationHost is the sub-agent routing seam: where this session's DELEGATIONS run, which is a
// different question from where the session itself is bound ([ServerHost]) — a smart model
// orchestrating while a cheaper one, possibly on another box, does the grunt work (ADR 0045).
//
// The split is [ServerHost]'s exactly. The renderer owns WHEN — a human picking a row — and the host
// owns WHAT: which entries the `servers:` list carries, what a name resolves to, the posture keys on
// it and the second heartbeat that discovers what that box is serving are all things this package
// never reads.
//
// A nil host means this Driver routes nothing: no pane offers the pick, and delegations run wherever
// the composition root left them. That is the posture of every hand-built Options and of every
// Driver that is not the interactive TUI (ADR 0031).
type DelegationHost interface {
	// Retarget points every delegation spawned from now on at the `servers:` entry name names, and
	// with an EMPTY name at this session's own upstream — the opt-out, and the default a config
	// naming no Sub-agent server already has.
	//
	// It may be called while the agent RUNS: children already in flight keep the server they were
	// spawned against, so the pick moves the next spawn and nothing else. It answers an error for the
	// two things that make a name unusable — no entry carries it, or the entry's posture keys are
	// defective — and changes nothing when it does. Success is deliberately SILENT here: the routing
	// change reaches the transcript through the host's own notice path, once, when the newly named
	// server is first observed.
	Retarget(name string) error

	// Targets is the `servers:` entries a delegation may be pointed AT, asked per draw so a list the
	// human edits mid-session (ADR 0037) is offered the moment the edit lands.
	//
	// It is deliberately NOT [ServerHost.List]: that list is what the SESSION can be switched to, and
	// it carries one row the file does not — the synthesized entry a raw `--endpoint` override builds
	// for this run alone (ADR 0036 decision 6). That row names no `servers:` entry, so it can be
	// neither resolved nor recorded here, and offering it would be offering a target that refuses
	// itself. An empty list is "nothing to delegate to", which the verb words itself.
	Targets() []ServerChoice

	// RecordChoice persists the entry this session's delegations run on as the `sub-agents-server:`
	// key, so the NEXT session routes there without being asked — [ServerHost.RecordChoice]'s twin,
	// key for key and answer for answer (ADR 0036 decision 2's shape, ADR 0045's key).
	//
	// It answers whether it WROTE: a name no `servers:` entry holds is skipped silently (false, no
	// error), which is also the answer of a host that records nothing at all. The EMPTY name is the
	// exception, and it holds no entry on purpose: it is the picker's `auto` row, the opt-OUT, and
	// what it records is the ABSENCE of the key — it CLEARS what is written there and reports
	// written (true, no error), including when there was no line to remove. Like its twin it is
	// best-effort persistence of something that ALREADY happened — the retarget landed before this is
	// called and stays landed whatever it answers — so an error is a note and never an undo.
	RecordChoice(name string) (recorded bool, err error)
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
// single-driver contract holds (phase-2 detail plan §3 C1). Two calls stand outside it
// deliberately, and both are engine-side-guarded rather than boundary-guarded: AbortExchange
// and InterjectChild, which the Update goroutine may make while a worker drives.
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
	// InterjectChild queues a user message for the RUNNING sub-agent that the sub_agent call
	// spawnCallID spawned, anywhere in the engine's tree — its own children, and recursively
	// theirs. The message lands at that child's next between-Steps boundary as an ordinary
	// interjection, committed by the goroutine that owns the child's Steps, with the child's own
	// tools, mode and confinement unchanged: addressing a child grants it nothing (ADR 0063).
	//
	// Unlike Interject above it is NOT the worker's call. It is the one engine call besides
	// AbortExchange that the program goroutine may make while a worker drives the loop: a
	// non-blocking enqueue onto a guarded mailbox that touches no conversation, so it needs
	// neither the between-Steps boundary nor the single-driver contract.
	//
	// It refuses with domain.ErrNoSuchChild when spawnCallID names no running sub-agent — the
	// child finished, was cancelled, or never existed — and that refusal is the message's whole
	// account: nothing was queued and no domain.ChildInterjectionEvent follows. On success exactly
	// one such event reports the message's fate, Landed either way, and the fold turns it into the
	// delivered block inside the run or the note that it never got there (transcript.apply).
	InterjectChild(spawnCallID string, in domain.UserInput) error
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
	// A successful restore closes every Console of the outgoing conversation and resets the
	// engine's cumulative usage reading.
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
	// UndoPreview describes what `/undo` would put back: the top un-undone Exchange group,
	// classified against the files as they are now — restore, delete, or skip with a reason for
	// each, at the journal's recorded absolute addresses (a root-joined named path for an ordinary
	// write, the permit-pinned resolved target for an approved escape), since that listing is the
	// disclosure the human authorises the revert from (ADR 0051). It reports false when there is
	// nothing to undo, which an engine whose process made no writes also answers — the journal is
	// memory, not storage, so a resumed session cannot reach an earlier process's writes. It
	// touches no file and does not move the journal. Called only at idle: the command is idle-only
	// precisely because a running Step is writing into the group this describes.
	UndoPreview() (undo.Step, bool)
	// UndoRevert executes the group UndoPreview described and reports what it restored, removed,
	// and skipped. generation is the stamp that preview carried, handed back as proof the human is
	// confirming the step they were shown: a journal that moved since refuses with
	// undo.ErrStaleGeneration and touches nothing, which is the /undo confirm re-preview path, and
	// an empty journal answers undo.ErrNothingToUndo. A path whose content no longer matches what
	// the agent wrote is skipped with its reason rather than overwritten. It MUTATES the workspace,
	// so like ClearContext it is called only at idle (no worker running) — the idle-only command
	// gate is what makes the read-then-revert pair safe.
	UndoRevert(generation uint64) (undo.Report, error)
	// SetEffortOverride states THIS session's Thinking effort (CONTEXT: Thinking effort) — the level
	// layered ABOVE the bound model profile's own `thinking.effort:` (ADR 0050), and the engine half
	// of the /effort command. The zero value CLEARS the override, so the profile's setting stands
	// again; any level in the widened effort vocabulary (domain.ThinkingEffort, ADR 0060) stands
	// until another call moves it. Like SetMode it is goroutine-safe and takes effect on the NEXT
	// request, which is exactly why /effort is safe to run while a worker works: the Turn already in
	// flight is untouched. It is configuration rather than a Mechanism, so it holds under Bypass, and
	// it is never persisted — a session intent that dies with the session.
	SetEffortOverride(domain.ThinkingEffort)
	// ThinkingEffort reports the two layers behind the effort the next request will carry: this
	// session's override (SetEffortOverride) and the bound model profile's own setting, each "" when
	// unset. The note every /effort pick ends on renders BOTH, because the same level means something
	// different depending on which layer it sits in — one survives a model switch, the other is
	// replaced by it.
	// Goroutine-safe like ConfineToWorkspace.
	ThinkingEffort() (override, profile domain.ThinkingEffort)
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
	// resolution) and reads it to NAME a path in a report: /skills tells an empty catalog
	// where discovery looked, and looking under a home the run is not using would be a wrong
	// answer to the one question that note exists to answer. `/skills export <id>` composes the
	// library folder it writes into out of the SAME value (skillscmd.go), so the export and that
	// note can never point at different homes. Empty ⇒ unwired: the reports fall
	// back to the "~/.apogee" spelling rather than inventing a path, and the export says it has
	// nowhere to write.
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
	// already validated by the binary (internal/config's UISettings.Validate calls ParseSpinnerStyle), so
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

	// HideScrollbar takes the scroll bar away from the transcript and from every popup pane — and
	// with it the column the bar hangs in, which the body then takes, because a hidden bar that
	// still ate a column would read as a bug. It is what the `ui.show-scrollbar` config key
	// selected, INVERTED at the composition root (cmd/apogee's wire.go is the one place the
	// polarity flips): the config key is positive and defaults to true, while this field must have
	// the zero value mean today's behaviour — the bar shown — so the hand-built Options of the
	// layout tests keep the width they pin. A `/settings` edit of the key moves it mid-session
	// (ADR 0037) and re-lays out, so the wrap width it decides changes exactly when the human
	// changes it and never on its own.
	HideScrollbar bool

	// StallAfter is how long the ENGINE may go silent, mid-turn, before the status line reports the
	// quiet — what the `ui.stall-after` config key selected, already parsed by the binary
	// (internal/config's UISettings), so the renderer takes a duration and never a spelling of one.
	// Past it a running turn's phrase gains a `· quiet <elapsed>` suffix, which is a REPORT and not a
	// verdict: a slow turn and a dead one are indistinguishable from here, so the honest thing to say
	// is how long nothing has arrived.
	//
	// The zero value is the guard OFF, which is both the config key's own spelling of "off" and what
	// the hand-built Options of the layout tests want: a suffix appearing under a status line whose
	// width they pin would be a change they never asked for.
	StallAfter time.Duration

	// Inspector is what the `ui.inspector` config key selected: whether this session's engine was
	// built with the wire capture armed (domain.Config.Inspector). The renderer does not act on it —
	// the records arrive as events either way (inspector.go) — it WORDS one row with it: an empty
	// /inspect pane says "nothing captured yet" where the key is on and names the key where it is
	// off, and those are different answers to the same silence. The zero value is disarmed, which is
	// both the key's own default and what the hand-built Options of the layout tests want.
	Inspector bool

	// SkillSuggestions paints the skill-suggestion band above the input box — what the
	// `ui.skill-suggestions` config key selected (ADR 0061). With it on, the draft is ranked against
	// [Options.Skills] as it is typed and the closest skills are named in the band; with it off the
	// band never paints and the Tab that opens the menu on it stays inert. It gates a hint on THIS
	// screen and nothing else: no part of the catalog reaches the model either way — a skill is sent
	// only when the human invokes it with a `/token`. A `/settings` edit moves it mid-session
	// (ADR 0037, settingsApplyLocal). The zero value is off, which is what the hand-built Options of
	// the layout tests want: it is the screen apogee rendered before the band existed.
	SkillSuggestions bool

	// CursorShape is the shape the prompt's caret is drawn with — what the `cursor-shape` config
	// key selected. apogee draws the REAL terminal cursor (the textarea's simulated one is retired
	// in newPromptEditor) and it never blinks, so the shape is the only axis there is: a Bubble Tea
	// program NAMES a cursor shape on every frame and never emits the DECSCUSR reset while it runs,
	// so "inherit the shape this terminal is configured with" is not expressible and this key is
	// the honest substitute for it. Like Spinner it is a SELECTION, already validated by the binary
	// (internal/config's ApplyConfig calls ParseCursorShape), so the renderer never parses a name. The
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

	// Schemes is what keeps the palette switchable from inside the program: what the picker offers,
	// the resolve behind an answer to it, and the export that creates a file to edit ([SchemeHost]).
	// All three read the schemes folder on every ask, so a file written mid-session is offered and
	// loaded without a restart. nil ⇒ schemes are unwired and each form says so, the nil-seam degrade
	// every provider here takes.
	Schemes SchemeHost

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

	// Settings is the `/settings` pane's seam onto the config file: the rows it shows, the write and
	// the reset a committed row makes, and the live apply of the key it just persisted
	// ([SettingsHost]). nil ⇒ the pane is unwired: it has nothing to show and says so, the nil-seam
	// degrade every provider here takes.
	Settings SettingsHost

	// ListMechanisms names every catalogued Mechanism and says which of them the config file has
	// switched ON — the vocabulary the `mechanisms` row's toggle sub-list is drawn from. It is a
	// closure rather than a slice for [SchemeHost.List]' reason: the block it reports is one the human
	// also edits by hand, so it is re-read on every ask and an edit made in the file shows up in an
	// open sub-list rather than at the next start.
	//
	// WHICH ids exist is the binary's knowledge, exactly as the key registry behind [SettingsHost.Rows] is:
	// the catalogue names them, an id the file's block does not carry is simply off, and the renderer
	// parses no `mechanisms:` block to work either out.
	//
	// nil ⇒ the row has no vocabulary and ⏎ on it opens nothing, the nil-seam degrade every provider
	// here takes.
	ListMechanisms func() []MechanismToggle

	// WriteMechanism persists one Mechanism's on/off line and puts it in force — the sub-list's whole
	// write half, and [SettingsHost.Write] one level in: the `mechanisms:` block's children are the
	// catalogue's ids rather than registry keys, so the target is named by (id, enabled) while the
	// splice, the verification and the atomic write stay behind the seam exactly as they do there.
	//
	// Toggling OFF writes `<id>: false` rather than removing the line, because the file records what
	// the human DECIDED and a block that emptied itself would hand a matching model's Validated set
	// back without being asked to (ADR 0016). The live apply is the binary's too and happens on this
	// same call (ADR 0037 decision 1), so the seam returns with the session running what the file now
	// says.
	//
	// An error is REPORTED, never swallowed, and `saved` says which HALF of the call it came from —
	// the two outcomes an error alone conflates. false ⇒ the splice refused, so the file is what it
	// was and the pane treats the Mechanism as unchanged; true ⇒ the line LANDED and only the live
	// apply did not, so the file carries the flip while the session does not, and the pane says so in
	// those words (the same sentence a persisted-but-unapplied [SettingsHost.Write] key gets). A landed
	// call returns (true, nil); (false, nil) is not a thing the seam says.
	//
	// nil ⇒ toggling is unavailable and the pane says so, the same degrade [SettingsHost.Write] takes.
	WriteMechanism func(id string, enabled bool) (saved bool, err error)

	// ExternalEditSpec is the command line that opens the config file at path's own line — the
	// nested structures' whole edit idiom (ADR 0037 decision 5): a `servers:` list or a
	// `model-profiles:` map is a shape no row can hold, so ⏎ on such a row hands the human the file
	// itself in their own editor rather than growing a form for each of them.
	//
	// The binary resolves all four parts because it owns all four: the config file's location, the
	// line that key sits on (its own splice writer already parses the document for it), which editor
	// this environment names — the `editor` key, then $VISUAL, then $EDITOR, then the platform's own
	// opener — with a line-jump argument passed only to the editors known to take one, and whether
	// that program takes this terminal ([EditorCommand.Detached], ADR 0041 decision 6). The renderer
	// receives a command it runs and nothing else, exactly as [SettingsHost.Write] hands it a file format
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
	// What is returned is not yet in force — the pane applies each key through [SettingsHost.Apply] and
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
	// [ServerHost.Beat]: one wait is opened at Init, each landed report re-reads through [ReloadConfig],
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
	//
	// It is a BLOCKING disk walk, so every trigger calls it from a Cmd goroutine rather than from
	// Update (skillRescanCmd) — a walk on the Update goroutine stalls the render loop on the
	// keystroke that opened the menu, and equally on the ⏎ that ran /skills, where the report lands
	// when the scan does. Two obligations follow for whoever wires it: it must be safe to call
	// concurrently with the catalog reads (Skills, and the loop's own resolver), and it must return
	// when the scan is done rather than detaching, since the message that repaints the menu — or
	// writes the /skills listing — is sent on its return. The bounded walk and the atomic snapshot
	// swap of skills.Provider satisfy both.
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

	// OnAutoTitle is told when a `/settings` edit — the pane, or the config file the watcher
	// re-reads — moves the `auto-title:` key, with the value the renderer just applied to
	// AutoTitle above. It exists because the key gates TWO namers: this side's session titles,
	// which are Model state and apply themselves, and the HOST's delegation namer (ADR 0068),
	// which is behind the engine's Config and can only be reached by being told.
	//
	// nil ⇒ nobody is listening, which is what every hand-built test Options and every host with
	// no delegation namer passes; the local apply is unaffected either way.
	OnAutoTitle func(enabled bool)

	// Server is the whole Upstream seam as one named capability (ADR 0054): which servers this
	// session can be on, the two verbs that put it on one, the choice each move records for the next
	// session, and the observation that says what the server it is on is actually serving. The six
	// acts were six bare funcs and are one interface for the reason [SettingsHost] is: they are faces
	// of one thing a host either has or has not — an Upstream it owns the endpoints, keys and pins
	// of — and the renderer drives all six the same way, owning WHEN and never WHAT.
	//
	// nil ⇒ unwired whole, and every degrade that follows from it is [ServerHost]'s to state.
	Server ServerHost

	// Prebound says this session started with NO upstream bound, and why (ADR 0036 decisions 3, 4
	// and 7). The zero value is the ordinary start — the binary determined a startup server and the
	// engine was constructed before the program began — and everything below it is unchanged.
	//
	// A non-zero Reason means there is no engine yet: the binary could not tell which server to
	// start on, and the TUI is the one Driver that can ASK rather than refuse (the non-interactive
	// drivers keep their hard error, because they have nobody to ask). Engine calls are answered
	// with a "no server is bound" error until [ServerHost.Bind] lands one, so the renderer's job is
	// to reach that seam before anything needs the engine.
	Prebound PreboundStart

	// KeyMigration is the start-up key-migration offer this session should raise, if any (ADR 0047,
	// keymigration.go): which `servers:` entries were found carrying a plaintext `api-key:` line
	// this machine's secret store could hold instead, and what that store is called. The zero value
	// — no store, no entries — is the ordinary start and raises nothing at all, which is also what a
	// hand-built Options and a headless run get.
	//
	// It carries NAMES and never a key. What each answer does to the file and to the store is
	// entirely behind the two seams below, so nothing this renderer holds, paints or records can be
	// a secret.
	KeyMigration KeyMigrationOffer

	// MigrateKey moves the named entry's key out of the config file and into the machine's secret
	// store, leaving the entry pointing at it with an `api-key-cmd:` line, and returns the file it
	// rewrote so the confirmation can name it.
	//
	// It is the whole move: the store write, the read-back of the key through the very command it
	// is about to persist, and only then the rewrite (ADR 0047's verify-before-rewrite). A failure
	// anywhere in that sequence means the config file was left exactly as it was, and it is
	// REPORTED — the [SettingsHost.Write] contract — because a migration that silently did not happen
	// leaves the human believing their key has moved.
	//
	// Synchronous, on the keypress that answered the offer, like every other config write here.
	// nil ⇒ no offer is raised at all.
	MigrateKey func(entry string) (path string, err error)

	// KeepPlaintextKey records the "never for this entry" answer — `plaintext-key-ok: true` on that
	// entry, the per-entry acknowledgement that ends the offer for good (ADR 0035's deliberate-edit
	// grain) — and returns the file it wrote, so the confirmation can say which line to delete to
	// be asked again. Same contract as MigrateKey in every other respect. nil ⇒ the answer says it
	// cannot be recorded, the nil-seam degrade every seam here takes.
	KeepPlaintextKey func(entry string) (path string, err error)

	// SubAgentsMigration is the OTHER start-up offer, and the one thing it has in common with the
	// pair above is its posture: the `servers:` entries whose block still spells ADR 0045's retired
	// `sub-agents: true` flag, in the file's own order. Nothing decodes that key any more — the root
	// `sub-agents-server:` key replaced it — so a config carrying it delegates nowhere its owner
	// meant, and the start-up offers the one edit that fixes it (keymigration.go).
	//
	// The FIRST name is the entry the pane asks about, and taking the offer drops the flag line from
	// every one of them: two flagged entries were a config the retired refusal used to reject, so a
	// file that has one is a file that never ran, and the choice between them is the human's. Empty —
	// the ordinary case — raises nothing at all.
	SubAgentsMigration []string

	// MigrateSubAgentsServer takes that offer for the named entry: the retired flag lines go, and
	// `sub-agents-server:` names this entry, in one edit of the file — after which the session's own
	// delegations are re-pointed there, so the answer is in force now rather than at the next
	// start-up. It reports the file it rewrote, so the confirmation can name where to look.
	//
	// The [SettingsHost.Write] contract in every other respect: synchronous on the keypress that
	// answered, and a failure means the file was left exactly as it was and is REPORTED. nil ⇒ no
	// offer is raised at all.
	MigrateSubAgentsServer func(entry string) (path string, err error)

	// RecordModelChoice persists the model this session just bound as the one its server comes back on
	// NEXT time — the `model:` key of the `servers:` entry the session is on, written only while the
	// `remember-model:` toggle is on. The renderer calls it with the id the human picked and knows
	// nothing else about it: which entry the session is on, whether that entry is one apogee may write
	// a model onto, and whether the toggle is on at all are the binary's questions, because only the
	// binary can see the file.
	//
	// Only an EXPLICIT pick reaches it — the `/model` picker's accept and `/model <id>`, which share one
	// bind path. A rebind the heartbeat merely OBSERVED records nothing: a server that loaded another
	// model is news about the server rather than a choice this human made, and a session that followed
	// it must not turn that observation into config nobody wrote. The `--model`/`APOGEE_MODEL` startup
	// overrides record nothing for the same reason — they are facts about one invocation.
	//
	// It answers whether it WROTE, exactly as [ServerHost.RecordChoice] does, which is what lets the
	// renderer state the recording without claiming one for the picks the binary skips: the toggle off,
	// a session on no configured entry, and a session on a LAUNCHER-FRONTED entry — whose `model:` is a
	// deliberately empty discovery hint, that class of server remembering its choice as a Launch profile
	// instead — are all false with no error.
	//
	// It is best-effort persistence of something that ALREADY happened: the session is bound before this
	// is called and stays bound whatever it answers, so an error is a note and never an undo. Like
	// [ServerHost.RecordChoice] it is synchronous — one small file, spliced and renamed — and called
	// on the Update loop.
	//
	// nil ⇒ nothing is recorded and every pick is session-scoped, which is the behaviour this key was
	// introduced to replace; every hand-built Options keeps it.
	RecordModelChoice func(model string) (recorded bool, err error)

	// Launcher is the llama-launcher seam whole ([LauncherHost]): the Launch profiles this machine
	// defines, the verb that activates one, the two that free or stop the server this session is on,
	// the `launch-profile:` a committed load records, and the boot check that acts on that pointer.
	// The seven acts were seven bare funcs and are one interface for the reason [ServerHost] is:
	// they are faces of one capability a host either has or does not have (ADR 0054), and this
	// family in particular was always wired together or not at all.
	//
	// nil ⇒ the integration is not configured on this host, and every degrade that follows from it —
	// `/model` falling back to the advertised models, the one sentence both actuation verbs answer
	// with, a load nobody records and a start-up that restores nothing — is [LauncherHost]'s to state.
	Launcher LauncherHost

	// Delegation is the sub-agent routing seam ([DelegationHost]): the one act that re-points this
	// session's delegations at another `servers:` entry, without a relaunch and without the file
	// having to change first.
	//
	// nil ⇒ this Driver routes nothing — a bench or a headless run composes no picker, and nothing
	// in the renderer offers to retarget (ADR 0031's degrade, the posture of every hand-built
	// Options).
	Delegation DelegationHost

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

// ServerChoice is one upstream server the `/server` picker offers. Name does four jobs with one
// value — it labels the row, it is the name [ServerHost.Switch] is called with, it becomes the
// footer's host alias once the session is on that server, and it is the identity the picker marks the
// CURRENT row by — because the name IS the entry's identity in the binary's `servers:` list, the
// single definition of what servers exist: the alias of the server you are on is the name you call
// it (ADR 0036 decision 1). That last job is a string-equality against [Options.HostAlias], which is
// exactly the bound entry's name on every start shape: the launch resolution sets it, a committed
// switch re-adopts it, and an ephemeral override start synthesizes its own row under it (ADR 0036
// decision 6). Endpoint is shown beside it and is display only — two entries may point at one URL
// and still be different entries, with their own key source and their own recorded pin, so moving
// between them is a real switch rather than the already-on answer.
//
// It carries display and identity and nothing else: the per-server api key and discovery hint are
// what the switch needs, and the switch is the binary's half of the seam, so the renderer never
// holds a credential it has no use for.
type ServerChoice struct {
	Name     string // the row's label, the switch argument, the footer alias, and the row's identity
	Endpoint string // the server's base URL, shown beside the name; display only, never identity
	// Description is the entry's free-text `description:` — the human's own words for what the box
	// is FOR (ADR 0069), empty when they wrote none. Only the `/sub-agents-server` picker shows it,
	// because that is the pane where the choice is between two boxes rather than between a box and
	// the one you are already on; `/server`'s rows are unchanged by it.
	Description string
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
// gives when the config alone cannot say which server to start on. The vocabulary lives in [domain]
// (uivocab.go) so the binary resolves the fact without importing the renderer; these aliases keep
// this package's call sites spelling it their own short way. The empty value is the ordinary start:
// a server WAS determined and the engine was constructed before the program began.
type PreboundReason = domain.PreboundReason

const (
	PreboundFirstBoot   = domain.PreboundFirstBoot
	PreboundStaleChoice = domain.PreboundStaleChoice
	PreboundNoServers   = domain.PreboundNoServers
)

// PreboundStart says whether this session started with an upstream bound, and if not, why. The zero
// value — an empty Reason — is the ordinary bound start, so a hand-built Options describes today's
// behaviour without naming this field at all.
type PreboundStart = domain.PreboundStart

// KeyMigrationOffer is what this start-up found to offer about plaintext API keys (ADR 0047): the
// `servers:` entries whose key is a literal `api-key:` line in the config file and that have not
// already answered "never", plus the human name of the store this machine can move them into.
//
// Both halves have to be there for a question to exist — an entry with nowhere to go and a store
// with nothing to hold are each an offer apogee cannot complete — so the zero value raises nothing,
// and that is what a hand-built Options, a machine with no usable store and a headless run all get.
type KeyMigrationOffer struct {
	// StoreName is what the machine's own operating system calls its secret store ("macOS Keychain",
	// "Secret Service"), so the question names something the human can go and look at afterwards.
	// Empty ⇒ there is no store here and no offer to make.
	StoreName string
	// Entries are the `servers:` names to ask about, in the file's own order — one pane each, the
	// next opening where the last one closed.
	Entries []string
}

// LaunchProfileChoice is one Launch profile `/model` offers (CONTEXT.md: a Launch profile
// is the LAUNCH-side description of a model — model file, server, flags — owned by the launcher's
// config, opposite the request-side Model profile). Name does two jobs with one value, the
// [ServerChoice] shape: it labels the row, and it is the name [LauncherHost.Load] is called with.
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

// ProfileRestore is the start-up restore check's answer ([LauncherHost.Restore]): the one Launch
// profile this session opens by loading, or the one line saying why it is not loading one.
//
// The two fields are alternatives, and the ZERO value is the ordinary answer — `remember-model:` off,
// no pointer recorded, no launcher fronting this server, and the case where the restore has already
// happened by itself, the recorded profile being what the launcher is serving right now. None of
// those is news to a human who has just started the program, so none of them earns a line.
//
// Note is the binary's own words, stated verbatim as a transcript note. What it says is a fact about
// the LAUNCHER's world — a profile that is no longer defined, a server already running under it —
// which the renderer cannot see and would only be paraphrasing.
type ProfileRestore struct {
	// Load names the Launch profile to actuate now; "" ⇒ there is nothing to load.
	Load string
	// Note is the single line to state when a recorded profile was NOT restored; "" ⇒ say nothing.
	Note string
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
// resumes a stored session: the opaque transcript blob to repaint (the neutral wire form
// internal/session versions, read back through transcriptbridge.go), the browsable title for the
// "resumed: <title>" note, the last observed
// context fill to relight the status-line gauge, and the stored user-message count. An empty or
// undecodable Transcript degrades to a no-scrollback note rather than a fatal error — a resumed
// legacy session still lists and resumes, it just has no scrollback to repaint.
type ResumedSession struct {
	Transcript []byte // the opaque scrollback blob; empty (a legacy record) ⇒ no replay
	Title      string // the session's browsable title, shown in the resume note
	CtxUsed    int    // the last observed context fill, relighting the gauge on resume
	UserMsgs   int    // the stored user-message count (metadata parity; the transcript re-derives it)
	// Usage is the main agent's cumulative token accounting as the record last stored it, so the
	// reopened session reports its spend rather than nothing. Zero on a record written before the
	// accounting existed — the same nothing-reported state a fresh session opens in.
	Usage session.Usage
	// DelegateUsage is the delegate half of that accounting, restored on the same terms and used on
	// weaker ones: the replayed scrollback brings back the run heads that carry their own readings,
	// and those replace it (Model.delegateUsageTotal). It is what a record whose blob no longer
	// replays has left to say that a delegate spent anything at all.
	DelegateUsage session.Usage
	// InExchange marks a session interrupted mid-task — the resumed Agent reports an open Exchange
	// (the binary reads agent.InExchange() after building it). newModel then appends the interrupted
	// note so the human knows /continue picks up the unfinished work; false for a cleanly-closed
	// session, which resumes without one.
	InExchange bool
}

// ConfinementInfo is the host's confinement situation, resolved once by the composition root
// and rendered by the /confine status report and the footer's confinement word: which Confiner
// backend answered, what that backend can actually enforce here, and the host id an
// `unconfined-hosts:` acknowledgement is matched against (ADR 0012, amendment 2026-07-21).
// It is the diagnostic half of /confine — the *effective* setting is read live off the [Engine],
// not from here, because the user can change it mid-session. The zero value means "the binary
// wired nothing"; the report says unknown rather than guessing, and the footer's word reads it as
// a backend that cannot promise a fence.
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
	// [ServerHost.List], because what this key may hold is whatever THIS config's `servers:` block
	// names — a list the human can change mid-session. It picks from the same sub-list an enum
	// does and never opens a text buffer, and its ⏎ is a SWITCH rather than a write: the session
	// moves ([ServerHost.Switch]) and the move records the choice ([ServerHost.RecordChoice], ADR 0036
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

	EnumValues  []string // the closed vocabulary, non-empty exactly for [SettingEnum] ([SettingServer] reads ServerHost.List instead)
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

// MechanismToggle is one catalogued Mechanism as the `mechanisms` row's sub-list shows it: the
// canonical id, and whether the config file's own block switches it on. Plain data, the [SettingRow]
// posture — the catalogue, the file and the resolution behind that bool are all the binary's
// ([Options.ListMechanisms]), and the renderer paints a row and asks for a flip.
//
// It carries no description, deliberately: what a Mechanism DOES is documented where Mechanisms are
// documented, and a list whose rows each carried a sentence would be a manual rather than the switch
// panel this row opens.
type MechanismToggle struct {
	ID      string
	Enabled bool
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
// were installed in the Agent's Config, and the resolved Options. The construction itself is
// [Build]'s — which builds the Model and the Bubble Tea program and binds the program to br
// (br.Bind) *before* program.Run() starts the loop, so the late-bound event and approval
// delegates reach the live program the moment the first worker emits (phase-2 detail plan §3
// C2/C3; ADR 0011). What Run adds is the terminal: the alternate screen it claims for the whole
// of the run, and os.Stdout as the thing painted into. The program context is ctx, so a
// program-wide shutdown also cancels an in-flight Exchange (C4).
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
	program, cleanup, err := Build(ctx, eng, br, opts, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = program.Run()
	return err
}

// errTraceWithDriverOutput refuses --tui-trace on a caller-supplied output. The trace wraps
// os.Stdout ([programOutput]); a driver's own tea.WithOutput wins over it, so honouring the flag
// here would hand the human an empty file and no way to tell that from a run that painted nothing.
// A Driver traces the bytes it is handed — it already has them.
var errTraceWithDriverOutput = errors.New(
	"tui: --tui-trace wraps the real terminal; a driver output is traced by the driver")

// Build constructs the Bubble Tea program [Run] runs and stops one step short of running it: it
// returns the program — already bound to br — beside the cleanup that closes whatever files the
// options opened, and an error if the two arguments contradict each other.
//
// It exists so a Driver (ADR 0031: the TUI test drivers, a bench harness, anything that has to
// feed the loop its own input and read its own output) enters through the SAME construction the
// binary uses, rather than through a Model assembled beside it. Everything between [newModel] and
// tea.NewProgram lives here, so there is one wiring and both callers get it.
//
// out is the writer the renderer paints into, and it is the whole of the difference between the
// two callers. nil is the production path: [programOptions] runs unchanged, so --tui-trace and the
// Windows sync-query stripper wrap os.Stdout exactly as they always have. A non-nil out is the
// driver path: the output half of [programOptions] is skipped entirely — it wraps os.Stdout, which
// the caller's output would win over — and tea.WithOutput(out) is installed in its place; a trace
// path alongside it is [errTraceWithDriverOutput] rather than a silently blank file.
//
// extra options are appended LAST, so a caller's WithInput / WithWindowSize / WithColorProfile /
// WithEnvironment beats anything built here. On an error nothing is left open and the returned
// cleanup is nil.
func Build(
	ctx context.Context,
	eng Engine,
	br *Bridge,
	opts Options,
	out io.Writer,
	extra ...tea.ProgramOption,
) (*tea.Program, func(), error) {
	if out != nil && opts.TracePath != "" {
		return nil, nil, errTraceWithDriverOutput
	}
	// The per-Turn snapshot notify: the worker sends turnSnapshotMsg through the Bridge's
	// late-bound program sender (the same programRef the Sink pushes Events through), so the
	// Model persists between Steps without any exported API. Bind (below) resolves it to the
	// live program before the first worker can fire.
	m := newModel(ctx, eng, opts, br.prog.send)
	// The Step-boundary flush: the Sink coalesces adjacent tokens behind a short window, and the
	// worker empties that buffer the instant a Step returns, so no token is ever delivered after
	// the Step that emitted it (worker.go, sink.go). It is wired HERE rather than through newModel
	// because the sink is the Bridge's, and Build is where the two meet.
	m.flushEvents = br.sink.flush
	// The environment the painter will read: bubbletea's own default (the process's) everywhere
	// but Windows, and on Windows the terminal-naming rule's slice (environ_windows.go). It is
	// resolved HERE, above the diag log rather than at the programOptions call below, because the
	// log has to report the environment the PAINTER sees — on Windows that is not the process's,
	// and a diagnostic that read the process would misreport the one variable it exists to measure.
	environ := programEnviron()
	// What the caller has to close when the program is done, closed in reverse order. It is a
	// slice rather than a deferred pair because Build RETURNS before the program runs: the files
	// have to outlive this function and still be closed exactly once.
	var closers []func()
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
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
			return nil, nil, fmt.Errorf("--tui-diag: %w", err)
		}
		closers = append(closers, func() { _ = diag.Close() })
		m.diag = diag
		diag.start(os.Getenv, environ, m.th.measure.Method())
	}
	// The --tui-trace half. traced is nil unless a path was named, in which case it is the file
	// this run owns and must close; the options are otherwise exactly what they have always been.
	// Two platform rules can also add an option here, both no-ops off Windows: environ is the
	// terminal apogee names itself to the painter as (environ_windows.go), and
	// programDeclinesSyncOutput is the mode-2026 question it keeps to itself (syncoutput.go).
	teaOpts, traced, err := buildProgramOptions(ctx, opts, environ, programDeclinesSyncOutput(), out)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if traced != nil {
		closers = append(closers, func() { _ = traced.Close() })
	}
	program := tea.NewProgram(m, append(teaOpts, extra...)...)
	// Bind before Run: the program exists now, and the first Send cannot occur until a
	// worker is launched, which only happens after the user submits into the running loop.
	br.Bind(program)
	return program, cleanup, nil
}

// buildProgramOptions picks between [Build]'s two paths. A nil out is the binary's, and is
// [programOptions] verbatim — the pin that says "with nothing switched on the program is built
// with exactly the option it has always had" covers the production path through this call too. A
// non-nil out is a Driver's: the same base options, then the caller's writer, and no output
// wrapper of ours anywhere near it.
func buildProgramOptions(
	ctx context.Context,
	opts Options,
	environ []string,
	declineSyncOutput bool,
	out io.Writer,
) ([]tea.ProgramOption, *tracedOutput, error) {
	if out == nil {
		return programOptions(ctx, opts, environ, declineSyncOutput)
	}
	if opts.TracePath != "" {
		return nil, nil, errTraceWithDriverOutput
	}
	return append(baseProgramOptions(ctx, environ), tea.WithOutput(out)), nil, nil
}

// baseProgramOptions are the options both of [Build]'s paths start from: the program context, and
// the painter's environment when a platform rule built one (nil ⇒ bubbletea keeps os.Environ()).
func baseProgramOptions(ctx context.Context, environ []string) []tea.ProgramOption {
	teaOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if environ != nil {
		teaOpts = append(teaOpts, tea.WithEnvironment(environ))
	}
	return teaOpts
}

// programOptions builds the Bubble Tea options [Build] starts the program with on the production
// path, and opens the traced output when [Options.TracePath] named one — the two are one decision,
// since the traced output IS an option and is also a file the caller has to close.
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
	teaOpts := baseProgramOptions(ctx, environ)
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
