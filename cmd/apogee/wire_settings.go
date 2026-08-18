package main

// The settings seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// Three parts, one subject — a `/settings` key that was committed while the session runs (ADR 0037):
// the live holder that owns the resolved values a re-resolution reads (the mutable half of the
// startup snapshot), the live-apply dispatcher that puts each committed key into effect through the
// narrow seam that key belongs to, and the per-model re-resolution a rebind drives (ADR 0024), which
// is the same resolution runRoot does at startup run again with another model.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// ----------------------------------------------------------------------------
// The live settings holder (the mutable half of the startup snapshot — ADR 0037)
// ----------------------------------------------------------------------------

// liveSettings owns the resolved values that used to be captured BY VALUE in runRoot's closures and
// were therefore frozen for the life of the process. ADR 0037 decision 1 ends that freeze: a key
// committed in the `/settings` pane applies to the RUNNING session, and the keys that are not engine
// mutators of their own reach the engine by being re-resolved — at the next rebind, the next server
// switch, the next scheduled Firing. Something has to hold what those re-resolutions read, and this
// is it: one place, one mutex, seeded from opts so a session nobody edits behaves exactly as before.
//
// What lives here is the exception list, not a second configuration path. Everything else about a run
// stays in the immutable `config.Options` snapshot the closures still carry, and a value only earns a field
// here once a seam puts it into effect (ADR 0031: no value the human can move without the engine
// hearing about it).
//
// The mutex is real work rather than ceremony. The writes come from the Update goroutine — the pane's
// keypress, through the live-apply dispatcher below — while a scheduled Firing reads them from the
// Scheduler's own goroutine (scheduleWiring.fire), so the fields are genuinely shared. It is an
// RWMutex because reads are the common case (every rebind takes one) and they never nest.
type liveSettings struct {
	mu sync.RWMutex

	// pinnedWindow is the `context-window:` key in tokens: > 0 is the user's pin, which outranks
	// whatever the server reports (ADR 0024 decision 9), and 0 means "discover it, live".
	pinnedWindow int
	// observedWindow is the window the last beat could name — remembered because a pin EDIT re-drives
	// the rebind closure with no beat of its own, and a pin CLEARED to 0 must then bind the discovered
	// window rather than unbind it. Nothing outside a beat knows this number.
	observedWindow int
	// entryWindow is the BOUND `servers:` entry's own `context-window:` pin (ADR 0045), 0 when that
	// entry pins none. It is held beside the top-level key rather than folded into it because the two
	// are different statements — this one describes the server the session is on, and a move replaces
	// it whole (followEntry) while the key above survives every move. Without it a switch's window
	// would live for one beat: the rebind that follows re-resolves from the pin and would bind the new
	// server's observation over the number its entry pinned.
	entryWindow int
	// entryCap is the BOUND entry's own `max-output-tokens:` pin (ADR 0046), 0 when that entry pins
	// none and the engine derives the ceiling from the reply room the Budget reserves. It is held
	// beside the window for the window's reasons — it describes THIS server's slot, a move replaces
	// it whole (followEntry), and a re-read list re-derives it (setServers) — and it is the whole
	// resolved answer rather than one rank of a ladder: ADR 0046 deliberately grew no top-level
	// `max-output-tokens:` key for an entry's pin to outrank, so what the entry says IS what the
	// session is bound to.
	entryCap int
	// pinnedReserve is the top-level `response-reserve:` share, 0 when the key is unset and apogee's
	// own built-in share stands. It is held rather than re-read off the launch snapshot because the
	// entry override below is resolved against it at a `/server` move, and the two must be read as
	// one statement. There is no setter: the key is file-only and nothing applies an edit of it to a
	// running session, so unlike pinnedWindow above it never moves.
	pinnedReserve float64
	// entryReserve is the BOUND entry's own `response-reserve:` override, 0 when that entry states
	// none and the top-level share above answers. It is latched beside the two token bounds, moves
	// with them (followEntry, setServers), and is resolved over the top-level key exactly as the
	// window is (config.ResolveResponseReserve) — the split describes the server the session is on,
	// so a move must replace it whole rather than divide the new server's window the retired one's
	// way.
	entryReserve float64
	// entryName is that entry's `servers:` name — how a re-read list is matched back to the server
	// this session is on, so a `context-window:` edited on the BOUND entry re-resolves the pin above
	// instead of leaving it describing the file as it was at the last move (parallelAgentsCap.name,
	// for its reason and with its posture: the ephemeral `--endpoint` entry is in no list, matches
	// nothing, and keeps what it was bound with). Empty until something is bound.
	entryName string

	// servers is the `servers:` list: the single upstream definition (ADR 0036), which the switch
	// list, the `server:` recording check and the pane's picker all resolve names against.
	servers []config.ServerEntry

	// manualIDs and mechanisms are the two halves of the `mechanisms:` block: the validated enabled
	// ids the engine arms, and the block itself, whose mere non-emptiness is what suppresses a matched
	// Validated set (whole-set-or-nothing, ADR 0016). They move together or the suppression rule and
	// the enable list would describe different configs.
	manualIDs  []apogee.MechanismID
	mechanisms map[string]bool

	// validatedEnable and validatedAlias are the `validated-sets:` block's own two keys — the surface's
	// off-switch and its carry-over map — the other inputs resolveValidatedSet keys a match on.
	validatedEnable bool
	validatedAlias  map[string]string

	// systemPrompt is the `system-prompt-text` / `system-prompt-file` / `system-prompt-models` trio
	// (ADR 0023). It is held whole rather than per key because selection is whole-entry replacement:
	// the three keys are one prompt, and ResolveSystemPrompt collapses them per model at every rebind.
	systemPrompt config.SystemPromptSettings

	// contextFilesEnable and contextFileNames are the `context-files:` block's two keys as the session
	// holds them NOW. They live here because each key's edit has to carry the OTHER half — the engine
	// takes the pair (Agent.SetContextFiles) — so switching the block back on installs the names as
	// they stand rather than the ones this run launched with.
	contextFilesEnable bool
	contextFileNames   []string

	// modelProfiles is the `model-profiles:` map (ADR 0044): the user tier the next per-model
	// resolution matches a model name against. It is held for the `mechanisms:` reason — an edit is
	// an INPUT to a resolution rather than a value the engine keeps — even though its own key also
	// pushes the resolved profile at SetProfile straight away: without it a switch made after the
	// edit would re-resolve against the map this process launched with.
	modelProfiles []profiles.Entry

	// rememberModel is the `remember-model:` toggle as the session holds it NOW. It earns a field here
	// for the holder's own reason and no other: nothing re-resolves it and no engine seam takes it, but
	// the three places that ASK it — the two recording seams and the boot restore (wire_verbs.go,
	// launcher.go) — all ask long after launch, so a value left in the launch snapshot would be frozen
	// for the life of the process and a `/settings` flip would govern nothing until the next start.
	rememberModel bool
}

// newLiveSettings seeds the holder with what THIS run resolved. manualIDs is passed in rather than
// re-derived because runRoot has already validated the block against the catalogue and holds the
// answer — deriving it twice is how the two spellings of the same list start to drift.
//
// The context-file PAIR is seeded from the resolved name list, which is the very read the pane's own
// two rows are formatted from (settingsrows.go): the two spellings of "off" collapse into an empty
// list at startup, so an enable read back off that list and a row showing `false` say the same thing.
func newLiveSettings(opts config.Options, manualIDs []apogee.MechanismID) *liveSettings {
	return &liveSettings{
		pinnedWindow: opts.ContextWindow,
		// The latch a determined startup binds with, seeded rather than pushed: the entry this
		// session STARTS on is the one the composition root already flattened onto options
		// (startupEntry), and its bind runs before this holder exists. A pre-bound start flattened
		// nothing, so both fields are the honest zero until the human's first pick latches one
		// through followEntry.
		entryWindow:        opts.StartupContextWindow,
		entryCap:           opts.StartupMaxOutputTokens,
		pinnedReserve:      opts.ResponseReserve,
		entryReserve:       opts.StartupResponseReserve,
		entryName:          opts.HostAlias,
		servers:            opts.Servers,
		manualIDs:          manualIDs,
		mechanisms:         opts.Mechanisms,
		validatedEnable:    opts.ValidatedSetsEnable,
		validatedAlias:     opts.ValidatedSetsAlias,
		systemPrompt:       opts.SystemPrompt,
		contextFilesEnable: len(opts.ContextFiles) > 0,
		contextFileNames:   opts.ContextFiles,
		modelProfiles:      opts.ModelProfiles,
		rememberModel:      opts.RememberModel,
	}
}

// pin reports the context-window pin in force right now — what a first binding and a server move
// adopt as the session's window, since the pin is global and survives both.
func (s *liveSettings) pin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinnedWindow
}

// setPin moves the pin. 0 restores discover-live, which the next rebind binds from the observed
// window below rather than from nothing.
func (s *liveSettings) setPin(tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinnedWindow = tokens
}

// followEntry takes the bound entry's own two token pins — its `context-window:` and its
// `max-output-tokens:` — onto the entry a move just landed on, dropping the retired entry's: the
// liveSettings half of what parallelAgentsCap.follow does for the fan-out width, and called at the
// same moment for the same reason: a pin is a fact about one server, and carrying the retired
// server's onto the new one is exactly the bug that would be invisible. An entry that pins nothing
// writes 0 for both, which leaves the top-level key answering for the window and the engine deriving
// the reply ceiling. The name travels with the pins, because it is what a later re-read of `servers:`
// matches this session's server back by.
//
// It is called at every point a session ARRIVES on a server that this holder already exists for — a
// `/server` move, and the first pick that ends a pre-bound start — and AFTER that arrival committed,
// so a refused one leaves the session budgeting against the server it is still on. The determined
// startup's own bind is the one arrival it is NOT called at: it runs before the holder exists, and
// newLiveSettings seeds both fields from the flattened startup entry instead.
func (s *liveSettings) followEntry(entry config.ServerEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entryWindow, s.entryCap, s.entryName = entry.ContextWindow, entry.MaxOutputTokens, entry.Name
	// The third statement the entry makes about its own slot: how its window is split for the reply.
	// It follows the two pins for their reason — an entry that states no share writes 0, which leaves
	// the top-level key answering — and it is assigned apart from them only because it is the one
	// that is not a token count.
	s.entryReserve = entry.ResponseReserve
}

// reservePin reports the top-level `response-reserve:` share — what a server move resolves an
// entry's own override over, the way pin above is what it resolves an entry's window pin over. 0 is
// the honest "the key is unset", which the engine reads as its own built-in share.
func (s *liveSettings) reservePin() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinnedReserve
}

// window reports the context window in force right now for the server this session is on: the bound
// entry's own `context-window:` when it names one, else the top-level key — the precedence
// config.ResolveContextWindow spells. It is what a bind and the launch-time projection adopt for the
// display, so the gauge and the engine's Budget cannot describe two different servers. 0 is the
// honest "nobody said", which the unbound model reads as "unknown until the first beat binds one".
func (s *liveSettings) window() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return config.ResolveContextWindow(s.entryWindow, s.pinnedWindow)
}

// observe records the context window a landed beat reported. A beat that could not name one (0) is
// not evidence the window changed — only that this beat could not say — so it is dropped rather than
// written, exactly as the TUI's own observation treats it.
func (s *liveSettings) observe(window int) {
	if window <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observedWindow = window
}

// observed reports the last window a beat could name (0 until one has). It is what a rebind driven by
// something OTHER than a beat — a pin edit — passes as the observation, so an unpinned session lands
// on the server's own window instead of on "unknown".
func (s *liveSettings) observed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.observedWindow
}

// serverList reports the `servers:` entries as they stand now — the question the `server:` recording
// seam asks, which is about the FILE's list and not about the switchable rows below.
func (s *liveSettings) serverList() []config.ServerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servers
}

// boundEntry is the `servers:` entry this session is ON, as the list stands NOW — the name followEntry
// latched, resolved against the live list the way setServers resolves it, so an entry edited since the
// move comes back as the file now spells it rather than as it was at the move.
//
// false is the honest answer for a session the file does not list, and it is the answer the model
// recording is skipped on: the synthesized ephemeral `--endpoint` row names no entry, and a Launch
// profile's own server may name none either (the profile's name is what the move carried). Nothing is
// guessed from the endpoint — two entries may share one address, and the name is what the file is
// spliced by.
func (s *liveSettings) boundEntry() (config.ServerEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.entryName == "" {
		return config.ServerEntry{}, false
	}
	for _, e := range s.servers {
		if e.Name == s.entryName {
			return e, true
		}
	}
	return config.ServerEntry{}, false
}

// choices assembles the servers this session can be MOVED to from the list as it stands now: the same
// upstreamChoices derivation startup made, re-run against the live list rather than the launch one, so
// the one row it may synthesize — the ephemeral `--endpoint` startup, a fact about the invocation that
// no edit can change — is still exactly where it was.
func (s *liveSettings) choices(base config.Options) []config.ServerEntry {
	base.Servers = s.serverList()
	return upstreamChoices(base)
}

// setSystemPrompt installs a re-read `system-prompt-*` block. The caller validates first: this is the
// commit half of a validate-then-commit, so a block the file cannot express never displaces a working
// prompt.
func (s *liveSettings) setSystemPrompt(sp config.SystemPromptSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemPrompt = sp
}

// setContextFilesEnable flips the `context-files:` off-switch and reports the names to install with
// it. The pair is read and written under ONE lock because the engine takes it as a pair: an enable
// that read the names outside the lock could install a half of one edit beside a half of another.
func (s *liveSettings) setContextFilesEnable(on bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFilesEnable = on
	return s.contextFileNames
}

// setContextFileNames replaces the `context-files.names:` list and reports the switch to install it
// under — setContextFilesEnable's mirror, and the other half of the same pair.
func (s *liveSettings) setContextFileNames(names []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFileNames = names
	return s.contextFilesEnable
}

// setServers installs a re-read `servers:` list. Nothing in the engine holds one — the picker, the
// switch and the `server:` recording all resolve names against this field (ADR 0036: one upstream
// definition) — so this store IS the whole apply for that key. The caller validates first, the
// setSystemPrompt posture: a list with a nameless or duplicated entry never displaces a working one.
//
// The bound entry's two token pins are re-resolved from the same list, under the same lock, because
// both are DERIVED from it: a `context-window:` or a `max-output-tokens:` the human edits on the
// entry this session is already on is an ADR 0037 key like `parallel-agents:` beside them
// (parallelAgentsCap.relist), and a list installed without re-deriving them would leave the latches
// describing the file as it stood at the last move. Matched back by NAME, which is what identifies
// an entry across a re-read (ADR 0036 decision 1); a list that no longer names this session's server
// leaves both pins exactly where they were — the posture the switch list takes toward an entry the
// human deleted while the session was on it — and an entry that has DROPPED one resolves back to
// what answers without it: the top-level key for the window, the engine's own derivation for the cap.
//
// It reports whether that re-derivation MOVED any of the three bounds this session is held to — the
// RESOLVED answers, compared across the install rather than the entry's own fields. That is what the
// caller's ride turns on (applySettingFor's `servers` case): a latch nobody re-reads describes the
// session only from the next rebind onwards, so an edit that moves one has to drive one, and an edit
// that moves none must not. Resolved-not-raw is what makes "moves none" honest for the window:
// an entry that drops a 65,536 pin onto a top-level key already saying 65,536 has changed the file
// without changing this session's window. For the CAP the resolved answer is the entry's own field —
// ADR 0046 grew no top-level key to fall back to — so the two comparisons read differently while
// asking one question. The SHARE compares the entry's own override for a third reason: the top-level
// `response-reserve:` key it resolves against is file-only and cannot move mid-session (pinnedReserve
// has no setter), so the raw and resolved answers part only where an entry DROPS an override the
// top-level key already matches — and there this errs toward riding, which reinstalls the number the
// session already holds rather than leaving a moved share waiting for the next bind.
func (s *liveSettings) setServers(servers []config.ServerEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	window, outputCap := config.ResolveContextWindow(s.entryWindow, s.pinnedWindow), s.entryCap
	reserve := s.entryReserve
	s.servers = servers
	for _, e := range servers {
		if e.Name != "" && e.Name == s.entryName {
			s.entryWindow, s.entryCap = e.ContextWindow, e.MaxOutputTokens
			// The re-read entry's own share travels with the two bounds so the next bind, move or
			// Firing divides this server's window the way the file says NOW — and it is part of the
			// moved-answer below, because a rebind now carries the share too
			// (apogee.RebindSpec.ResponseReserveFraction). An edit that moves only this one is in
			// force the moment it commits, through the ride this return value drives, rather than
			// waiting for whichever of those three comes first.
			s.entryReserve = e.ResponseReserve
			break
		}
	}
	return config.ResolveContextWindow(s.entryWindow, s.pinnedWindow) != window ||
		s.entryCap != outputCap || s.entryReserve != reserve
}

// setMechanisms installs a re-read `mechanisms:` block: the validated enabled ids and the block
// itself, together, because the block's mere non-emptiness is what suppresses a matched Validated set
// (whole-set-or-nothing, ADR 0016). Written apart they would describe two different configs for as
// long as one rebind takes.
func (s *liveSettings) setMechanisms(ids []apogee.MechanismID, block map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manualIDs, s.mechanisms = ids, block
}

// setModelProfiles installs a re-read `model-profiles:` map — the USER tier of the per-model
// resolution (ADR 0044), which every later rebind and every scheduled Firing matches against. The
// map alone is not a state the engine can be put into, so the key's apply pushes the resolved
// profile through SetProfile as well; this store is what keeps the two from drifting apart the
// moment the session changes model.
func (s *liveSettings) setModelProfiles(entries []profiles.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelProfiles = entries
}

// modelProfileEntries reports the `model-profiles:` user tier as it stands now. It is the read the
// Sub-agent server's per-beat resolution matches ITS model against (delegation.go), and it goes
// through the holder for the reason every live read does: an edit committed mid-session must reach
// the next resolution rather than the next launch.
func (s *liveSettings) modelProfileEntries() []profiles.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.modelProfiles
}

// remember reports whether `remember-model:` is on right now — the question the two recording seams
// ask before they splice a key onto a `servers:` entry, and the first question the boot restore asks
// before it does any launcher I/O at all. All three ask at the moment they have something to decide,
// which is what makes a `/settings` flip govern the very next pick rather than the next process.
//
// It takes the lock like every other read here rather than being a plain field, because one of those
// three callers is answered off the Update loop: the boot restore runs on a Cmd goroutine, and the
// pane's flip is written from Update.
func (s *liveSettings) remember() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rememberModel
}

// setRememberModel flips that toggle. The store IS the whole apply for the key — there is nothing to
// push at the engine and nothing to re-resolve, since what the toggle gates has not happened yet: the
// next explicit `/model` pick, the next committed profile load, the next start-up.
func (s *liveSettings) setRememberModel(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberModel = on
}

// setValidatedSets installs a re-read `validated-sets:` block — the surface's off-switch and its
// carry-over map, the two inputs resolveValidatedSet keys a match on, moved together for
// setMechanisms' reason.
func (s *liveSettings) setValidatedSets(enable bool, alias map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validatedEnable, s.validatedAlias = enable, alias
}

// rebindInputs projects the live values onto a COPY of the startup snapshot and hands back the
// arguments rebindSpecFor takes them as. It is the one place the overlay is spelled out, so a caller
// cannot re-resolve half from the holder and half from the launch: every re-resolution — the rebind
// closure, a scheduled Firing — opens with this call. The two token bounds come back beside the copy
// for that same reason rather than through accessors of their own: read under ONE lock, they cannot
// describe two different instants of a `servers:` list that is being installed as they are read.
//
// The charter covers the WIRE too, which this settings holder deliberately does not own: bound is the
// upstreamHolder's snapshot, and it is overlaid unconditionally because the holder — not the launch
// snapshot — is the authority on where this session is pointed (ADR 0036: one upstream definition).
// Without it a `/server` switch would leave the resolution keyed on the LAUNCH endpoint, and every
// input that is keyed on the endpoint — the probe record behind the identity ladder's middle rung,
// and so the Validated-set decision above it — would be resolved against a server the session left.
// Both live callers run only after the startup bind, so the snapshot is always a real binding.
func (s *liveSettings) rebindInputs(base config.Options, bound upstreamBinding) (config.Options, []apogee.MechanismID, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	base.Endpoint = bound.Endpoint
	base.APIKey = bound.APIKey
	// The pin the resolution applies is the one in force for the server the session is on NOW: the
	// bound entry's `context-window:` when it names one, else the top-level key — the precedence
	// config.ResolveContextWindow spells, called inline because the read lock is already held here.
	// Resolved once, so the copy and the returned pin cannot disagree.
	pin := config.ResolveContextWindow(s.entryWindow, s.pinnedWindow)
	base.ContextWindow = pin
	// And how that window is split on the server the session is on NOW: the bound entry's
	// `response-reserve:` over the top-level key (config.ResolveResponseReserve, the ranks the
	// window's own resolution spells). It is written ONTO the copy rather than handed back beside the
	// two bounds because `config.Options` spells this number as a session-wide share, which is
	// exactly what a caller reading the copy needs — a Firing composes its Config from it.
	base.ResponseReserve = config.ResolveResponseReserve(s.entryReserve, s.pinnedReserve)
	base.Servers = s.servers
	base.Mechanisms = s.mechanisms
	base.ValidatedSetsEnable = s.validatedEnable
	base.ValidatedSetsAlias = s.validatedAlias
	base.SystemPrompt = s.systemPrompt
	base.ModelProfiles = s.modelProfiles
	// And the other bound the server states: the reply ceiling the bound entry pins (ADR 0046). It
	// travels beside the window because the spec the caller builds carries it beside the window, and
	// it is handed back rather than written onto the copy because `config.Options` spells this number
	// as the STARTUP entry's — a session that has moved since is bound to the entry it is on now.
	return base, s.manualIDs, pin, s.entryCap
}

// settingsApplier is everything a committed key can have to reach, in one value rather than in a
// constructor that grows an argument per key class. It is composed at the composition root, where all
// six members already exist, and it is what makes the dispatcher exercisable without a session: every
// member is either a narrow interface, a closure, or a plain value a test can supply.
type settingsApplier struct {
	// engine is the anytime-safe mutator class: a key here is in force the moment it returns.
	engine settingsEngine
	// live is the startup snapshot's mutable half — where a key that is re-RESOLVED rather than
	// pushed (the window pin, the system prompt) is written before the re-resolution is driven, and
	// where the one key that is neither (`remember-model:`) simply lands: what it gates has not
	// happened yet, so the store is the whole apply.
	live *liveSettings
	// binding reads the Upstream binding as it stands now; wired to upstreamHolder.Binding. Its Model
	// is what a rebind must be driven FOR — an empty one means nothing is bound yet.
	binding func() upstreamBinding
	// rebind is [tui.Options.Rebind]'s own closure: the per-model re-resolution, which reads live
	// through rebindInputs and commits through the engine's idle-only Rebind.
	rebind func(model string, window int) (tui.RebindResult, error)
	// configPath is the config.yaml this session resolved — re-read whole for the keys whose value is
	// a structure no single string can spell.
	configPath string
	// skills is the shared skill catalogue: the SAME Provider the loop resolves attached ids against
	// and the "/" menu lists, so re-pointing it at another source layering moves both at once.
	skills settingsSkills
	// tools is the session's live tool set — where a tool is re-pointed in place, and the door a
	// set-level change goes through.
	tools *liveTools
	// mcp is the session's live MCP connections: the one key whose apply is a reconnect rather than a
	// write, and the source of half the tool set the door above swaps.
	mcp *liveMCP
	// present is the presentation ladder, which rebuilds from a changed `present:` block and
	// re-installs itself on the presenter the engine holds.
	present *livePresentation
	// roots are this session's resolved state dirs, which is what the skill source layering is spelled
	// out of (the same three fields runRoot builds the startup Sources from).
	roots stateRoots
	// caps is the session's Parallel agents cap (ADR 0039), reached by exactly one key: a re-read
	// `servers:` list can carry a new `parallel-agents:` for the entry this session is already on, and
	// that is a value the engine holds. nil ⇒ this Driver composed no cap, so the list still applies
	// and only the width stands still.
	caps *parallelAgentsCap
	// delegation is the Sub-agent server (ADR 0045), reached by that same one key: the flag lives on
	// a `servers:` entry, so adding, removing or re-pointing it is a `servers:` edit. nil ⇒ this
	// Driver routes no delegations, and the list applies with routing left where it was.
	delegation *delegationWiring
}

// applySettingFor builds the [tui.Options.ApplySetting] dispatcher: the one place a key the pane has
// just persisted becomes a call on a live seam (ADR 0037 decision 1's apply step). It is keyed by
// REGISTRY PATH because that is the only name the renderer knows a setting by — the pane hands back
// the same path and the same file-spelled value it handed [tui.Options.WriteSetting], and the
// resolution from that string into whatever the seam takes happens here, where the schema lives
// (ADR 0031: the engine is handed values, never config text).
//
// It returns the row's boundary note and the apply's refusal. A key this build cannot apply is an
// ERROR naming the key rather than a silent success: the write has already landed, so the honest
// report is that the file changed and the session did not.
//
// Two classes of key and no third. One is PUSHED at an engine mutator and is in force on return. The
// other is re-RESOLVED: the new value lands in the holder and the per-model resolution is re-driven
// over it (rideTheRebind), which is how the window pin and the system prompt reach an engine that has
// no setter for either — the same path a heartbeat-observed model change already takes.
//
// A key nothing HOLDS is a third answer rather than a third class: `editor` is read off the file at
// the moment an external edit starts, so the write alone puts it in force and its case says success
// with nothing to do (ADR 0041 decision 1). That is not the refusal above, because the file changing
// IS the session changing for a value only ever read from the file.
//
// An EMPTY value means one thing for every key: the file no longer SETS this key, so resolve the
// built-in default a fresh start would have resolved. That is what a reset of a key whose default is
// unset hands in (the pane's settingsApplied), and every such key answers it through the case it
// already has rather than a branch of its own — `web-search-endpoint` resolves "" to the built-in
// provider, `present.command` and `present.host` rebuild the ladder from a block with the field
// cleared, `tools.disabled` parses "" as the empty roster, the two `url-safety:` host lists parse it
// as the empty list the guard tightens nothing from, the `system-prompt-` pair re-read a file that no
// longer carries the key, and `editor` is in force from the write itself.
// TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault holds this side of it.
func applySettingFor(a settingsApplier) func(key, value string) (string, error) {
	return func(key, value string) (string, error) {
		// A member this Driver did not compose is a legitimate configuration rather than a bug (ADR
		// 0031: the engine stays sufficient for any Driver), so the key it would have been reached
		// through is refused in the dispatcher's own words — never dereferenced. Asked first, so a
		// key that cannot land does no work on its way to saying so.
		if err := a.unreachable(key); err != nil {
			return "", err
		}
		switch key {
		case "mode":
			mode, err := domain.ParseMode(value)
			if err != nil {
				return "", err
			}
			a.engine.SetMode(mode)
		case "bypass":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetBypass(on)
		case "auto-compact":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetCompactionEnabled(on)
		case "remember-model":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			// The one toggle with no engine seam behind it and no re-resolution to ride: what it gates
			// is a WRITE apogee will make later — the entry key an explicit `/model` pick or a committed
			// profile load records — and a decision the next start-up makes. So the holder store is the
			// whole apply, and the seams that ask (recordModelChoice, recordLaunchProfile,
			// launcherWiring.restore) read it from there at the moment they have something to record.
			a.live.setRememberModel(on)
		case "context-files.enable":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			// The names the session holds NOW: this run's resolution until a names edit replaced them.
			// A block that STARTED off resolved to no names at all (the two spellings of "off" collapse
			// at startup), so what switching it back on installs is whatever the names row has since
			// been given — which is why the two keys share one holder rather than one closure each.
			a.engine.SetContextFiles(on, a.live.setContextFilesEnable(on))
			return contextFileNote, nil
		case "context-files.names":
			// The value arrives as the FILE spells it — the one-line flow sequence the writer just
			// rendered — and is read back by the same parse, so the engine is handed the list a reader
			// takes out of the file rather than a second reading of the keystrokes. The switch travels
			// with it: names alone are not a state the engine can be put into.
			names := config.ParseSettingList(value)
			a.engine.SetContextFiles(a.live.setContextFileNames(names), names)
			return contextFileNote, nil
		case "use-project-skills":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			// The same layering startup spells, with the flag as it now stands. Re-pointing alone would
			// change nothing anybody sees — the Provider serves the catalogue it has until asked for a
			// fresh one — so the re-scan is part of the same act.
			a.skills.SetSources(skills.Sources{
				Home:             a.roots.config,
				Workspace:        a.roots.workspace,
				UseProjectSkills: on,
			})
			// The scan's error is soft and is dropped here for the reason the "/" menu's reload drops
			// it: Load never signals an unusable catalogue — a malformed skill is skipped and shown in
			// the /skills report — so a partial scan is not a failed apply.
			_ = a.skills.Reload()
		case "web-search-endpoint":
			return "", a.tools.setSearchEndpoint(value, a.engine)
		case "tools.disabled":
			// The roster reaches the session as a whole tool SET rather than as a value on a tool, so
			// this is the swap door and not a re-point (setDisabled). The value arrives as the FILE
			// spells it and is read back by the same parse the writer rendered it with, so the set the
			// session runs and the line the file carries cannot be two readings of one edit.
			names := config.ParseSettingList(value)
			if err := a.tools.setDisabled(names, a.engine); err != nil {
				return "", err
			}
			// A name that matches no tool is reported on the row rather than refused, exactly as it is
			// at startup: the rest of the list has already applied, and saying so is the honest note.
			if unknown := config.UnknownToolNames(names); len(unknown) > 0 {
				return "no tool named " + strings.Join(unknown, ", "), nil
			}
			return toolRosterNote, nil
		case "url-safety.allow-hosts", "url-safety.deny-hosts":
			// The host lists reach the session the way the roster above does and for a related
			// reason: the guard is built WITH the set (registryWithMCP hands one URLGuard to every
			// network tool) and no tool has a setter for it, so this is the swap door rather than a
			// re-point. The value arrives as the FILE spells it and is read back by the same parse
			// the writer rendered it with, and an EMPTY value is the empty list — which is the
			// built-in default a fresh start resolves: no allow-list narrowing, no configured deny,
			// and the guard's own SSRF floor still standing under both (it is not reachable from
			// configuration). An entry that normalises to nothing is dropped where the guard is
			// built, exactly as it is at startup, so there is nothing to report on the row.
			hosts := config.ParseSettingList(value)
			move := a.tools.setAllowHosts
			if key == "url-safety.deny-hosts" {
				move = a.tools.setDenyHosts
			}
			if err := move(hosts, a.engine); err != nil {
				return "", err
			}
			// The same boundary the roster reports, because it is the same boundary: the set is
			// swapped on commit and the next request runs against the guard it carries.
			return toolRosterNote, nil
		case "editor":
			// The one key with nothing at all behind it to move: the editor ladder reads `editor` off a
			// FRESH projection of the file every time an external edit starts (externalEdit.spec), so the
			// write the pane has just made is the whole of the apply and the very next ⏎ that opens an
			// editor runs the new command (ADR 0041 decision 1: "nothing to dispatch and nothing to
			// journal beyond the write itself"). It is named here rather than left to the default because
			// that default is a refusal — and a refusal over a value already in force would tell the user
			// their change had not taken effect when it had.
		case "present.auto-open", "present.command", "present.port", "present.host":
			return "", a.present.apply(key, value)
		case "context-window":
			tokens, err := settingInt(key, value)
			if err != nil {
				return "", err
			}
			// 0 keeps meaning discover-live (ADR 0024): the re-drive below binds the window the last
			// beat reported, so clearing a pin hands the session back to the server rather than to
			// "unknown". No note — the pin is what the Budget and Compaction measure against from the
			// moment the rebind commits.
			a.live.setPin(tokens)
			return "", a.rideTheRebind()
		case "system-prompt-text", "system-prompt-file", "system-prompt-models":
			// The value the pane persisted is deliberately not read here: these three keys are ONE
			// prompt (ADR 0023, whole-entry selection) and `system-prompt-models:` is a map no single
			// string spells, so the block is re-read from the file the pane just wrote and re-resolved
			// per model by the rebind — exactly the resolution startup made.
			if err := a.reloadSystemPrompt(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "servers":
			// Most of the `servers:` list reaches no engine seam at all: it is the single upstream
			// definition (ADR 0036) that the picker, the `/server` switch and the choice recording
			// resolve names against, and all three read the holder — so installing the re-read list is
			// most of the apply. The value the pane persisted is not read, for the system-prompt keys'
			// reason: a list of blocks is a shape no single string spells.
			moved, err := a.reloadServers()
			if err != nil {
				return "", err
			}
			// The rest of it is three numbers the engine is already holding: the window the BOUND entry
			// pins (ADR 0045 decision 3), the reply ceiling it pins beside it (ADR 0046) and the share
			// of that window it holds back for the reply. Installing those on the latches alone would
			// leave an edited bound describing the session only from the next rebind onwards — the next
			// beat that happens to observe a change, seconds or minutes away — so this key rides the
			// rebind exactly as the top-level `context-window:` key above does, through the same door,
			// for the same reason: not one of the three has an engine setter of its own.
			//
			// Only when a resolved bound actually MOVED, though — ANY of them, since all three ride the
			// one spec and one ride carries them all. A rebind is not free: it re-resolves every per-model
			// binding, resets the token estimator and the compaction latch, and is idle-only, so an
			// edit to some OTHER entry that drove one would refuse mid-Exchange to install numbers
			// nobody changed. And a Driver that composed no rebind to ride installs the list and stands
			// still on both bounds, the posture reloadServers' own optional members take.
			if !moved || !a.rides() {
				return "", nil
			}
			return "", a.rideTheRebind()
		case "mechanisms":
			// Neither block is a value the engine holds either: they are INPUTS to the per-model
			// resolution — the enable list and the whole-set-or-nothing suppression rule (ADR 0016) —
			// so they land in the holder and are committed by the rebind, the one door a model change
			// and a config change share (rideTheRebind).
			if err := a.reloadMechanisms(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "validated-sets.enable", "validated-sets.alias":
			// Two rows, one apply: the block's off-switch and its alias map are a single input to the
			// per-model resolution (ADR 0016 — a set applies whole or not at all), so the re-read
			// installs both whichever row asked for it. The pane writes only the off-switch; the alias
			// map arrives from the human's own editor and comes back through this same door.
			if err := a.reloadValidatedSets(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "mcp-servers":
			// The one key whose value is a set of live CONNECTIONS. It is not pushed and not
			// re-resolved into a holder either: it is dialled, and the session moves onto the servers
			// that answered (ADR 0037 decision 6). The value the pane persisted is not read, for the
			// `servers:` reason — a list of blocks is a shape no single string spells.
			return "", a.reconnectMCP()
		case "model-profiles":
			// The map is an INPUT to the per-model resolution, like `mechanisms:` above — but the
			// model has NOT changed, and re-driving a whole rebind to move one field would refuse the
			// edit whenever an Exchange is open. So it takes the profile's own engine door instead
			// (ADR 0044 ratified call 6: Rebind is the model-switch door, SetProfile the same-model
			// config-edit one). The value the pane persisted is not read — a map of blocks is a shape
			// no single string spells.
			return "", a.reloadModelProfiles()
		default:
			return "", cannotApply(key)
		}
		return "", nil
	}
}

// cannotApply is the dispatcher's one refusal for a key that will not reach the session at all —
// because this build knows no seam for it, or because this Driver composed the dispatcher without
// the member that seam lives behind. It names the key, since the row it lands on is that key's, and
// it is deliberately the SAME sentence for both: to the human they are one fact, that the file
// changed and the session did not.
func cannotApply(key string) error {
	return fmt.Errorf("apogee: %s cannot be applied to the running session", key)
}

// unreachable reports, for one key, that this applier was composed without something that key's
// apply has to reach. Every member is optional by design — a Driver builds the dispatcher out of
// what it HAS, and a bench or a daemon has no presenter and no skill catalogue (ADR
// 0031) — so a nil member has to degrade to the refusal above rather than panic on the Update
// goroutine, halfway through an edit that has already been written to the file.
//
// It is a second switch over the same keys, which is a drift risk taken deliberately and closed by a
// test: TestApplySettingRefusesEveryKeyItCannotReach drives EVERY registry key through a zero
// applier, so a key added to the dispatcher without a line here fails as the panic it would be.
func (a settingsApplier) unreachable(key string) error {
	reaches := true
	switch key {
	case "mode", "bypass", "auto-compact":
		reaches = a.engine != nil
	case "model-profiles":
		// The engine for the swap and the binding for the model to resolve the map AGAINST — the one
		// key that both pushes and re-resolves, so it needs a member from each class. The holder is
		// in the list because the map it stores is what the NEXT rebind reads.
		reaches = a.engine != nil && a.binding != nil && a.live != nil
	case "context-files.enable", "context-files.names":
		reaches = a.engine != nil && a.live != nil
	case "remember-model":
		// The holder alone, and for once that is the literal whole of the apply: the toggle reaches no
		// engine seam and rides no rebind — the seams it gates read it back out of this holder.
		reaches = a.live != nil
	case "use-project-skills":
		reaches = a.skills != nil
	case "web-search-endpoint", "tools.disabled", "url-safety.allow-hosts", "url-safety.deny-hosts":
		// The engine as well as the tool set: a registry with no web_search to re-point is rebuilt and
		// handed through SwapTools, which is the swap door and not this holder's to skip — and the
		// roster switch and the two host lists are that door every time.
		reaches = a.tools != nil && a.engine != nil
	case "present.auto-open", "present.command", "present.port", "present.host":
		reaches = a.present != nil
	case "context-window", "system-prompt-text", "system-prompt-file", "system-prompt-models",
		"mechanisms", "validated-sets.enable", "validated-sets.alias":
		reaches = a.rides()
	case "servers":
		// The holder alone is enough to ACCEPT this key: the list itself reaches no engine seam (ADR
		// 0036), and the rebind the bound entry's two token pins ride is conditional — asked for only
		// by an edit that moved one of them. Requiring the whole riding triple here would refuse every
		// list edit on a Driver that composed no rebind, for a ride most list edits never ask for.
		reaches = a.live != nil
	case "mcp-servers":
		reaches = a.mcp != nil && a.tools != nil && a.engine != nil
	}
	if reaches {
		return nil
	}
	return cannotApply(key)
}

// rides reports whether this applier was composed with everything a rebind-riding key needs: the
// value lands in the holder and the per-model resolution is re-driven over it, so all three members
// together are what makes that apply an apply.
//
// It is asked in two voices. unreachable asks it about the keys that are NOTHING but a ride — a
// missing member there is the honest refusal, since the file changed and the session cannot. The
// `servers:` case asks it about a ride that is one part of a larger apply, where a missing member
// leaves the list installed and only the entry's two token bounds standing still, exactly as a nil
// caps or a nil delegation leaves the width and the routing standing still.
func (a settingsApplier) rides() bool {
	return a.live != nil && a.binding != nil && a.rebind != nil
}

// rideTheRebind re-drives the per-model resolution for the model the session is bound to right now.
// It is how a key with no engine setter of its own is applied: the value is already in the holder,
// rebindSpecFor reads it there, and Agent.Rebind commits the whole per-model binding atomically —
// one door for a model change and a config change alike, rather than a second, subtly different way
// to move the same four fields.
//
// With no model bound — a cold start before the first beat, or the gap a `/server` switch opens —
// there is nothing to rebind and nothing to report: the holder carries the change, and the first beat
// that binds a model resolves it in. A refusal from the engine (Rebind is idle-only, so an open
// Exchange is one) is returned, which the pane renders as the row's apply failure over the value it
// has already persisted; re-committing the edit retries it at a quieter moment.
func (a settingsApplier) rideTheRebind() error {
	model := a.binding().Model
	if model == "" {
		return nil
	}
	_, err := a.rebind(model, a.live.observed())
	return err
}

// reloadSystemPrompt re-reads the `system-prompt-*` block from the config file and installs it on the
// holder, validate-then-commit: a block the file cannot express — both spellings of one prompt at
// once, an entry with neither — is refused before it displaces a prompt that works.
//
// Only the FILE layer carries these keys (there is no flag or environment variable for a prompt), so
// re-reading that one layer resolves them exactly as startup resolved them. The migration notice is
// dropped rather than surfaced: a file still in the retired schema was already migrated and announced
// at launch, and this read happens after the pane has just written to it.
func (a settingsApplier) reloadSystemPrompt() error {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	var sp config.SystemPromptSettings
	if l.SystemPrompt != nil {
		sp = *l.SystemPrompt
	}
	if err := sp.Validate(); err != nil {
		return err
	}
	a.live.setSystemPrompt(sp)
	return nil
}

// reloadServers re-reads the `servers:` block and installs it on the holder, validate-then-commit:
// an entry that could never be switched to — no name, no endpoint, or a name an earlier entry took —
// is refused by the SAME check startup runs, before it can displace a list that works.
//
// Only the FILE layer carries the list (no flag, no environment variable names an upstream), so
// re-reading that one layer resolves it exactly as startup resolved it — reloadSystemPrompt's own
// reasoning, and the reason the migration notice is dropped here too.
//
// It reports whether the re-read moved either token bound the BOUND entry resolves to — its window
// or its reply ceiling (setServers' own answer) — which is what tells the caller a rebind has to ride
// this apply. A refusal reports false with the error: nothing was installed, so nothing moved.
func (a settingsApplier) reloadServers() (bool, error) {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return false, err
	}
	if err := config.ValidateServers(l.Servers); err != nil {
		return false, err
	}
	// The Sub-agent server the list now flags (ADR 0045), re-pointed BEFORE anything is installed:
	// it is the one part of this apply that can still refuse — a flagged entry whose `mechanisms:`
	// map this build does not know — and a refusal has to leave the session on the list it was
	// already running, not half-way onto a new one.
	if a.delegation != nil {
		if err := a.delegation.relist(l.Servers); err != nil {
			return false, err
		}
	}
	moved := a.live.setServers(l.Servers)
	// One thing in that list the engine holds and can be PUSHED: the fan-out width of the server this
	// session is on (ADR 0039 decision 2). Re-resolving it here is what makes `parallel-agents:` an
	// ADR 0037 key like the rest — moved in the pane, in force in the running session — rather than one
	// that waits for the next switch. The entry is matched back by name and the observed slot count is
	// kept: the file changed, the server did not. (The other two such numbers, the bound entry's window
	// pin and its reply ceiling, have no setter to push at and reach the engine on the caller's ride
	// instead.)
	if a.caps != nil {
		a.caps.relist(l.Servers)
	}
	return moved, nil
}

// reloadMechanisms re-reads the `mechanisms:` block and installs both halves of it. The ids are
// derived through the startup producer (mechanismIDs), which validates EVERY key of the block —
// enabled and disabled alike — so a Mechanism id this build does not know is refused here rather than
// silently arming nothing, exactly as it is at launch (ADR 0015 §1).
func (a settingsApplier) reloadMechanisms() error {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	ids, err := mechanismIDs(l.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return err
	}
	a.live.setMechanisms(ids, l.Mechanisms)
	return nil
}

// reloadValidatedSets re-reads the `validated-sets:` block. An absent off-switch resolves to ON, the
// default ResolveSettings starts from, so a block the human deleted goes back to the surface being
// available rather than to it being off — the difference between "unset" and "false" that the file
// layer's pointer carries.
func (a settingsApplier) reloadValidatedSets() error {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	enable := true
	if l.ValidatedSetsEnable != nil {
		enable = *l.ValidatedSetsEnable
	}
	a.live.setValidatedSets(enable, l.ValidatedSetsAlias)
	return nil
}

// reconnectMCP re-reads the `mcp-servers:` block and moves the session onto it. The dial and the
// registry swap are liveMCP.reconnect's; what belongs here is the same one thing every structured
// key's apply does — resolve the file layer exactly as startup resolved it — because only the FILE
// carries this key (no flag, no environment variable names an MCP server).
//
// A file that no longer parses is refused before anything is dialled, so a typo in an unrelated key
// cannot cost the session the servers it is talking to.
func (a settingsApplier) reconnectMCP() error {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	return a.mcp.reconnect(l.MCPServers, a.tools, a.engine)
}

// reloadModelProfiles re-reads the `model-profiles:` map, installs it on the holder, and swaps the
// profile it resolves for the model the session is bound to RIGHT NOW into the running engine
// (ADR 0044). The resolution is the composition root's — profiles.Resolve over the user map and this
// build's shipped table, exactly as startup and every rebind resolve it — and the VALIDATION is the
// engine's: SetProfile builds the profile's two collaborators before it commits, so a tool-call
// format this build cannot parse leaves the session reading responses exactly as it did, and says so
// on the row.
//
// A map the human emptied resolves to whatever the shipped table says for this model, and to the zero
// profile when it says nothing — which is what startup would have resolved it to. Deleting an entry
// asks for apogee's own answer back, not for whatever the process happened to launch with.
//
// With no model bound — a cold start before the first beat, or the gap a `/server` switch opens —
// there is nothing to resolve against and the holder carries the change alone, rideTheRebind's own
// posture: the first beat that binds a model resolves it in.
func (a settingsApplier) reloadModelProfiles() error {
	l, err := config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	a.live.setModelProfiles(l.ModelProfiles)
	model := a.binding().Model
	if model == "" {
		return nil
	}
	// The notice is dropped rather than returned: it is a resolution's narration for a model change
	// nobody made, and the row the pane is about to paint already says the edit applied.
	profile, _ := resolveModelProfile(model, l.ModelProfiles)
	return a.engine.SetProfile(profile)
}

// settingInt reads a whole count the same way (KindInt's own validators do, validateContextWindow),
// so a value the registry accepted is a value this parses. Negative is refused here too rather than
// trusted from the file: the pane is not the only thing that can write one.
func settingInt(key, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("apogee: %s is a count of 0 or more, not %q", key, value)
	}
	return n, nil
}

// settingBool reads a bool exactly as the splice writer renders one (renderSettingValue), so the
// value a key was persisted with is the value it is applied with. The message names the key, because
// it lands on that key's row.
func settingBool(key, value string) (bool, error) {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("apogee: %s is true or false, not %q", key, value)
	}
	return on, nil
}

// ----------------------------------------------------------------------------
// Per-model re-resolution (the composition root's half of a rebind — ADR 0024)
// ----------------------------------------------------------------------------

// rebindSpecFor re-resolves every per-model binding for a model the heartbeat observed, and returns
// the whole answer as one [apogee.RebindSpec] plus the per-session notices worth telling the human.
// It is the same resolution runRoot does at startup, run again with a different model — deliberately
// so: a session that switches models mid-conversation must land in exactly the state a session
// started on that model would have been in, or "rebind" would be a second, subtly different way of
// configuring the engine.
//
// What it re-resolves:
//   - the system-prompt template, because `system-prompt-models:` keys on the model name (ADR 0023);
//   - the validated Mechanism set, because a set is matched against the model's identity fingerprint
//     (ADR 0016) — the opts copy carries the new id so the fingerprint re-keys on it;
//   - the enable list, applying the same precedence startup applies: an explicit `mechanisms:` block
//     is manual control and suppresses any matched set (whole-set-or-nothing, never a merge), which
//     is why manualIDs is passed in rather than re-derived from the map here;
//   - the context window, applying the pin: pinnedWindow > 0 is the user's `context-window:` key and
//     outranks whatever the server reports (decision 9), else the observed window is bound as-is;
//   - the reply ceiling, which is not per-model at all and is re-stated here anyway: outputCap is the
//     bound `servers:` entry's `max-output-tokens:` (ADR 0046), and stating it on EVERY spec is what
//     lets a live edit of that pin ride a rebind — including an edit that DROPS it, which arrives as
//     the zero that means "derive the cap again" rather than as silence. The spec's field is a
//     pointer so silence is still expressible; this resolver simply never has anything to be silent
//     about, since it always knows what the bound entry says;
//   - the response-reserve share, for the ceiling's reason and on the ceiling's terms: the split is
//     not per-model either, has no engine setter of its own, and is re-stated on EVERY spec so a
//     `response-reserve:` edited on the bound entry is in force at once — an edit that DROPS the
//     override arriving as the stated 0 that hands the split back to apogee's own default;
//   - the Model profile, because `model-profiles:` keys on the model name and the shipped shape
//     table matches on it too (ADR 0044) — the shape a model speaks the wire in travels with the
//     model, so it rides the same atomic Rebind as the prompt and the Mechanisms.
//
// What it deliberately does NOT touch: the endpoint, the mode, the tools, and the conversation, none
// of which a model change has any claim on.
//
// A resolution failure is returned rather than swallowed — an unreadable per-model prompt file or a
// dangling validated-sets alias is the user's own config being wrong about the new model — and the
// caller then leaves the engine bound to what it had, which is the honest outcome.
func rebindSpecFor(
	opts config.Options,
	roots stateRoots,
	manualIDs []apogee.MechanismID,
	model string,
	window, pinnedWindow, outputCap int,
) (apogee.RebindSpec, []string, error) {
	next := opts
	next.Model = model

	sysPrompt, err := config.ResolveSystemPrompt(next.SystemPrompt, model, roots.config, os.ReadFile)
	if err != nil {
		return apogee.RebindSpec{}, nil, err
	}

	vset, notices, err := resolveValidatedSet(next, roots.validated, roots.probe)
	if err != nil {
		return apogee.RebindSpec{}, nil, err
	}
	enable := manualIDs
	if len(vset) > 0 {
		enable = vset
	}

	bound := window
	if pinnedWindow > 0 {
		bound = pinnedWindow
	}

	// The shape the NEW model speaks the wire in (ADR 0044). A built-in match announces itself on the
	// same channel the validated-set lines travel, because to the human they are one kind of fact:
	// something apogee decided about this model that nobody typed.
	profile, notice := resolveModelProfile(model, next.ModelProfiles)
	if notice != "" {
		notices = append(notices, notice)
	}

	// The share the bound entry resolves to, which rebindInputs already wrote onto the copy this
	// resolver was handed. Taken into a local because the spec states it by pointer, the ceiling's
	// contract: nil would mean "the current share stands", and this resolver always has something to
	// say — including the 0 that says the override is gone and apogee's own default divides again.
	reserve := next.ResponseReserve

	return apogee.RebindSpec{
		Model:                   model,
		SystemPrompt:            sysPrompt,
		MaxContextTokens:        bound,
		MaxOutputTokens:         &outputCap,
		ResponseReserveFraction: &reserve,
		EnableMechanisms:        enable,
		Profile:                 profile,
	}, notices, nil
}
