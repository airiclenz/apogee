package main

// The settings seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// Three parts, one subject — a `/settings` key that was committed while the session runs (ADR 0037):
// the live holder that owns the resolved values a re-resolution reads (the mutable half of the
// startup snapshot), the live-apply dispatcher that puts each committed key into effect through the
// narrow seam that key belongs to, and the per-model re-resolution a rebind drives (ADR 0024), which
// is the same resolution runRoot does at startup run again with another model.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/reactions"
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
// What lives here is two readings of ONE configuration, not a second configuration path. boot is the
// `config.Options` this run resolved at launch, held whole and never written; now is the same value
// as the session holds it NOW — boot with every key a `/settings` commit has since applied written
// over it, through the one door (update). A value earns a write into now only once a seam puts it
// into effect (ADR 0031: no value the human can move without the engine hearing about it), and
// options() hands now back whole, so a caller that has to compose a WHOLE run out of this session —
// a Firing raised inside it — reads the unmoved keys beside the moved ones off one value rather than
// off a per-key mirror that has to be projected back onto the launch snapshot (which is how the two
// used to be able to disagree). The keys nothing applies mid-session — the workspace, the confinement
// flag, the MCP block — come back exactly as they were resolved because nothing ever writes them.
//
// Beside the two Options sit the LATCHES: facts about the server the session is on that no config
// key spells (what a beat observed, the bound entry's own pins) and so cannot live in an Options
// value. They are what the live-edited keys are resolved through at a rebind, and they move with the
// session (followEntry, observe) rather than with the file.
//
// Every key whose apply IS an Options edit reads its value through the registry row's Set
// (landSetting): the file-spelled value the pane handed it is landed on a scratch Options under the
// same admission the writer judged it by, the seam is driven off the landed value, and now is
// written once the seam has returned. The HAND-WRITTEN applies are the closed set of keys whose
// apply is not an Options edit: `mode` (an engine push of a domain.Mode, which the engine holds),
// `use-project-skills` and `use-shipped-skills` (the seam is the Provider's skills.Sources),
// `/confine` (bypasses this holder altogether — ADR 0037 2026-08-25 note), `context-files.enable`
// (owns no Options field; the pair below is its home) and the latches, which no key spells.
//
// The mutex is real work rather than ceremony. The writes come from the Update goroutine — the pane's
// keypress, through the live-apply dispatcher below — while a scheduled Firing reads them from the
// Scheduler's own goroutine (scheduleWiring.fire), so the fields are genuinely shared. It is an
// RWMutex because reads are the common case (every rebind takes one) and they never nest.
type liveSettings struct {
	mu sync.RWMutex

	// boot is the `config.Options` this run resolved at launch, held whole and immutable: what a
	// session nobody edits is configured as, and the base now started from. Nothing writes to it.
	boot config.Options

	// now is the session's configuration as it stands NOW: boot with every key a `/settings` commit
	// has since applied written over it. It is the ONE overlay — every setter below writes it, options()
	// clones it — so there is exactly one place a live key lives and exactly one projection a Firing
	// composes from. The lists it holds are the holder's own (cloned from boot at construction, replaced
	// whole by every setter), which is what lets options() hand out copies without a caller ever
	// reaching the backing arrays this session is running on.
	//
	// The `context-files:` block is the one key written under a rule of its own: now.ContextFiles is
	// the RESOLVED list exactly as ApplyConfig resolves it at startup — the names while the switch is
	// on, nil while it is off — and the pair the pane edits is kept beside it (contextFilesOn,
	// contextFileNames below).
	now config.Options

	// The Reaction surface the engine seam takes — the Floor gates, `bypass:`, the two notice
	// switches and the two Reaction lanes in the ONE value a live swap carries (ADR 0076 A8) — is not
	// held beside now but DERIVED from it (generation, generationOf): the seven Floor keys, the switches
	// and the `reactions:` list are all keys on now, so there is nothing a second value could remember
	// that this one does not, and a mirror kept beside it was one more place two edits could come to
	// disagree. The Floor gates are spelled NEGATIVE in the derived value, the engine's own spelling,
	// and floorFromOptions is the one seam that turns the file's seven positive keys into it.

	// observedWindow is the window the last beat could name — remembered because a pin EDIT re-drives
	// the rebind closure with no beat of its own, and a pin CLEARED to 0 must then bind the discovered
	// window rather than unbind it. Nothing outside a beat knows this number.
	observedWindow int
	// effortDialect is the effort wire dialect the last beat reported for this server (ADR 0060),
	// remembered for observedWindow's reason and read back the same way: a `/settings` edit re-drives
	// the rebind closure with no beat of its own, and passing a zero there would tell the engine a
	// server the heartbeat had already dialled advertises no thinking-effort dial at all.
	effortDialect provider.EffortDialect
	// entryWindow is the BOUND `servers:` entry's own `context-window:` pin (ADR 0045), 0 when that
	// entry pins none. It is held beside the top-level key (now.ContextWindow) rather than folded into
	// it because the two are different statements — this one describes the server the session is on,
	// and a move replaces it whole (followEntry) while the key survives every move. Without it a
	// switch's window would live for one beat: the rebind that follows re-resolves from the pin and
	// would bind the new server's observation over the number its entry pinned.
	entryWindow int
	// entryWorking is the BOUND `servers:` entry's own `working-window:` bound, 0 when that entry
	// bounds none and the top-level key (now.WorkingWindow) answers. It is latched beside the window
	// pin, moves with it (followEntry, setServers), and is resolved over the top-level key exactly as
	// that pin is (config.ResolveWorkingWindow) — how much room is affordable describes the server the
	// session is on, so a move must replace it whole rather than work a new server in the retired
	// one's room.
	entryWorking int
	// entryCap is the BOUND entry's own `max-output-tokens:` pin (ADR 0046), 0 when that entry pins
	// none and the engine derives the ceiling from the reply room the Budget reserves. It is held
	// beside the window for the window's reasons — it describes THIS server's slot, a move replaces
	// it whole (followEntry), and a re-read list re-derives it (setServers) — and it is the whole
	// resolved answer rather than one rank of a ladder: ADR 0046 deliberately grew no top-level
	// `max-output-tokens:` key for an entry's pin to outrank, so what the entry says IS what the
	// session is bound to.
	entryCap int
	// entryReserve is the BOUND entry's own `response-reserve:` override, 0 when that entry states
	// none and the top-level share (now.ResponseReserve, file-only — nothing applies an edit of it to
	// a running session) answers. It is latched beside the two token bounds, moves with them
	// (followEntry, setServers), and is resolved over the top-level key exactly as the window is
	// (config.ResolveResponseReserve) — the split describes the server the session is on, so a move
	// must replace it whole rather than divide the new server's window the retired one's way.
	entryReserve float64
	// entryName is that entry's `servers:` name — how a re-read list is matched back to the server
	// this session is on, so a `context-window:` edited on the BOUND entry re-resolves the pin above
	// instead of leaving it describing the file as it was at the last move (parallelAgentsCap.name,
	// for its reason and with its posture: the ephemeral `--endpoint` entry is in no list, matches
	// nothing, and keeps what it was bound with). Empty until something is bound.
	entryName string
	// entry is the BOUND `servers:` entry held WHOLE, as the file spelled it at the last arrival or
	// re-read: the startup entry resolution held on options (config.Options.StartupEntry), replaced
	// by the entry a move lands on (followEntry) and re-derived from a re-read list (setServers)
	// under the same lock the pins above move under. It is what a Firing raised inside this session
	// binds to (firingBinding), read as one value rather than re-assembled from the latches beside
	// it — the latches stay because they are what the live-edited overrides are resolved through,
	// and the whole entry is held because a Firing wants the server, not a resolution of it.
	entry config.ServerEntry

	// contextFilesOn and contextFileNames are the `context-files:` block's two keys as the pane
	// edits them — the named exception to "every key lives on now". The block is two keys and ONE
	// resolved list, and the engine takes the pair (Agent.SetContextFiles): each key's edit has to
	// carry the OTHER half, so switching the block back on installs the names as they stand rather
	// than the ones this run launched with — which means the names have to be kept somewhere while
	// the switch is off, and now.ContextFiles is nil then (the two spellings of "off" are one
	// answer, ApplyConfig's). Both setters write now.ContextFiles from this pair.
	contextFilesOn   bool
	contextFileNames []string
}

// newLiveSettings seeds the holder with what THIS run resolved: boot and now start as the same value,
// and only a `/settings` commit ever parts them.
//
// The context-file PAIR is seeded from the resolved name list, which is the very read the pane's own
// two rows are formatted from (settingsrows.go): the two spellings of "off" collapse into an empty
// list at startup, so an enable read back off that list and a row showing `false` say the same thing.
func newLiveSettings(opts config.Options) *liveSettings {
	return &liveSettings{
		boot: opts,
		// Cloned rather than assigned so the overlay's lists are the holder's own from the first
		// write: a setter replaces a list whole, but a caller that was handed boot elsewhere must never
		// find its arrays shared with the value this session is running on.
		now: cloneOptions(opts),
		// The latch a determined startup binds with, seeded rather than pushed: the entry this
		// session STARTS on is the one resolution holds on options (config.Options.StartupEntry),
		// and its bind runs before this holder exists. A pre-bound start holds the zero entry, so
		// both fields are the honest zero until the human's first pick latches one through
		// followEntry.
		entryWindow:      int(opts.StartupEntry.ContextWindow),
		entryWorking:     opts.StartupEntry.WorkingWindow,
		entryCap:         opts.StartupEntry.MaxOutputTokens,
		entryReserve:     opts.StartupEntry.ResponseReserve,
		entryName:        opts.HostAlias,
		entry:            opts.StartupEntry,
		contextFilesOn:   len(opts.ContextFiles) > 0,
		contextFileNames: opts.ContextFiles,
	}
}

// cloneOptions is the one deep copy of a `config.Options`: the value with every list and map it
// carries copied, so what comes back shares no backing array with what went in. It is what options()
// hands out and what now is seeded from, for one reason: the value travels to another goroutine — a
// Firing composes on the Scheduler's — and a caller that sorted or appended to the roster it was
// given would otherwise be editing the set this session is running.
//
// The eight collections are the ones the value carries; a ninth added to config.Options has to be
// added here, and TestLiveSettingsOptionsFollowEveryApply's clobber is where a missed one shows.
func cloneOptions(opts config.Options) config.Options {
	opts.ToolsDisabled = slices.Clone(opts.ToolsDisabled)
	opts.URLAllowHosts = slices.Clone(opts.URLAllowHosts)
	opts.URLDenyHosts = slices.Clone(opts.URLDenyHosts)
	opts.Reactions = slices.Clone(opts.Reactions)
	opts.Servers = slices.Clone(opts.Servers)
	opts.ModelProfiles = slices.Clone(opts.ModelProfiles)
	opts.ContextFiles = slices.Clone(opts.ContextFiles)
	opts.SystemPrompt.Models = maps.Clone(opts.SystemPrompt.Models)
	return opts
}

// update is the ONE door a live key is written through: it runs fn against now under the write lock
// and nothing else. Every setter below is this call with the field spelled out, and the applies that
// need no more than the write — a toggle no seam takes, a bound the next Firing reads — call it
// directly. There is deliberately no return: a setter that must hand something back under the same
// lock (the context-file pair, setServers' moved answer, the generation writers) spells its own
// locked body instead of composing two doors.
func (s *liveSettings) update(fn func(*config.Options)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.now)
}

// pin reports the context-window pin in force right now — what a first binding and a server move
// adopt as the session's window, since the pin is global and survives both.
func (s *liveSettings) pin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now.ContextWindow
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
// newLiveSettings seeds both fields from the held startup entry instead.
func (s *liveSettings) followEntry(entry config.ServerEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entryWindow, s.entryCap, s.entryName = int(entry.ContextWindow), entry.MaxOutputTokens, entry.Name
	// The room inside that window this server is worked in, moving with the pin it sits under and for
	// its reason: a bound describes ONE server, so carrying the retired server's onto the new one
	// would work a 32K slot in the room a 1M one was bounded to.
	s.entryWorking = entry.WorkingWindow
	// The third statement the entry makes about its own slot: how its window is split for the reply.
	// It follows the two pins for their reason — an entry that states no share writes 0, which leaves
	// the top-level key answering — and it is assigned apart from them only because it is the one
	// that is not a token count.
	s.entryReserve = entry.ResponseReserve
	// And the entry itself, whole, so a Firing raised after this move binds to the server the session
	// arrived on rather than to the one it launched against.
	s.entry = entry
}

// reservePin reports the top-level `response-reserve:` share — what a server move resolves an
// entry's own override over, the way pin above is what it resolves an entry's window pin over. 0 is
// the honest "the key is unset", which the engine reads as its own built-in share.
func (s *liveSettings) reservePin() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now.ResponseReserve
}

// workingPin reports the top-level `working-window:` bound — what a server move resolves an entry's
// own bound over, the way pin above is what it resolves an entry's window pin over. 0 is the honest
// "the key is unset", which leaves the whole advertised window as the room the session works in.
func (s *liveSettings) workingPin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now.WorkingWindow
}

// window reports the context window in force right now for the server this session is on: the bound
// entry's own `context-window:` when it names one, else the top-level key — the precedence
// config.ResolveContextWindow spells. It is what a bind and the launch-time projection adopt for the
// display, so the gauge and the engine's Budget cannot describe two different servers. 0 is the
// honest "nobody said", which the unbound model reads as "unknown until the first beat binds one".
func (s *liveSettings) window() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return config.ResolveContextWindow(s.entryWindow, s.now.ContextWindow)
}

// observe records what a landed beat reported about the server: the context window, and the effort
// wire dialect that reaches its thinking-effort dial (ADR 0060).
//
// The two are written under different rules because their zeroes say different things. A beat that
// could not name a window (0) is not evidence the window changed — only that this beat could not say
// — so it is dropped rather than written, exactly as the TUI's own observation treats it. The
// dialect's zero IS the answer, "this server advertises no dial", so it is always written.
func (s *liveSettings) observe(window int, dialect provider.EffortDialect) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if window > 0 {
		s.observedWindow = window
	}
	s.effortDialect = dialect
}

// observed reports the last window a beat could name (0 until one has). It is what a rebind driven by
// something OTHER than a beat — a pin edit — passes as the observation, so an unpinned session lands
// on the server's own window instead of on "unknown".
func (s *liveSettings) observed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.observedWindow
}

// observedDialect reports the effort wire dialect the last beat named for this server (the zero
// until one has, which keeps the historical `chat_template_kwargs` shape and so reproduces the
// request bytes that predate the dialect seam). It is [liveSettings.observed]'s counterpart for
// the dial, and it exists for the same reason: a rebind driven by a `/settings` edit rather than by
// a beat has to re-state the server fact the heartbeat observed instead of clearing it.
func (s *liveSettings) observedDialect() provider.EffortDialect {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effortDialect
}

// serverList reports the `servers:` entries as they stand now — the question the `server:` recording
// seam asks, which is about the FILE's list and not about the switchable rows below.
func (s *liveSettings) serverList() []config.ServerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now.Servers
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
	for _, e := range s.now.Servers {
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

// setSystemPrompt installs a re-read `system-prompt-*` block and the `use-default-prompt:` switch
// that closes it. The caller validates first: this is the commit half of a validate-then-commit, so
// a block the file cannot express never displaces a working prompt. The two move together, under one
// lock, for the reason setContextFilesEnable's pair does — the rebind resolves them as one prompt,
// and installing half of one edit beside half of another would resolve a prompt nobody configured.
func (s *liveSettings) setSystemPrompt(sp config.SystemPromptSettings, useDefault bool) {
	s.update(func(o *config.Options) {
		o.SystemPrompt = sp
		o.UseDefaultPrompt = useDefault
	})
}

// promptEditorSeed is the text the `/settings` pane's `system-prompt-text` editor OPENS on when
// nothing has been written for it: the embedded default's own bytes (config.DefaultSystemPrompt,
// ADR 0064 §1), so the human starts from the prompt this session is ACTUALLY sending rather than
// from a blank field they would have to write a prompt into from nothing. The seed is DISPLAY-only
// — no key is written and no resolution moves until ctrl+s commits it, at which point the text
// becomes explicit config like any other prompt and replaces the default the way an explicit prompt
// always does (ADR 0064 §2).
//
// It is answered here rather than by the registry row's `Text:` projection because the answer is
// this SESSION's, not the file's: the row's blank-when-unset contract is what the external-edit
// diff compares two reads of config.yaml in (settingsedit.go), and a projection that answered the
// embedded default would make every unset config read as a prompt somebody wrote.
//
// The question it asks is the RESOLUTION's, not one key's: the editor seeds the embedded default
// exactly when the whole prompt this session resolves IS the embedded default. That is the empty
// global source — `system-prompt-file` set beside a seeded text field would make the very first
// ctrl+s commit a config the next resolution refuses, since both keys set is an error
// (config.ResolveSystemPrompt), and a config that works today must not be walked into one that does
// not by a field apogee pre-filled — plus an empty `system-prompt-layers:` list, because layers
// stop the default from firing at all (ADR 0067 §3) and a seeded field would show a prompt the run
// does not send, plus the resolution itself coming back as the default for the model this session
// is BOUND to. That last clause is what a per-model entry needs: an entry matching the bound model
// replaces the default, so the editor opens empty, while an entry matching nothing leaves the
// default in force and the editor shows it. `use-default-prompt: false` with nothing configured
// resolves to an empty prompt, which is what the run sends and so what the editor opens on.
//
// The block and its flag are snapshotted under ONE lock for setSystemPrompt's reason — they are one
// prompt — and resolved outside it, since no holder lock is worth holding across a resolution. The
// resolution runs against promptSeedNoDiskRead rather than os.ReadFile: this is asked on the pane's
// render path, once per PAINT, and a paint reads no files. A resolution that FAILS seeds nothing —
// what the run sends is then an error, not the default — and a refused read reaches exactly that.
func (s *liveSettings) promptEditorSeed(model, home string) string {
	s.mu.RLock()
	systemPrompt, useDefault := s.now.SystemPrompt, s.now.UseDefaultPrompt
	s.mu.RUnlock()

	if systemPrompt.Global.Text != "" || systemPrompt.Global.File != "" || len(systemPrompt.Layers) > 0 {
		return ""
	}
	resolved, err := config.ResolveSystemPrompt(systemPrompt, model, home, useDefault, promptSeedNoDiskRead)
	if err != nil || resolved != config.DefaultSystemPrompt() {
		return ""
	}
	return resolved
}

// errPromptSeedNoDiskRead is what a prompt source with a file to read gets on the render path. It is
// never shown to anyone: promptEditorSeed turns every resolution error into "no seed".
var errPromptSeedNoDiskRead = errors.New("apogee: the prompt-editor seed resolves without reading files")

// promptSeedNoDiskRead is the file reader promptEditorSeed resolves with, and it reads nothing. The
// seed is re-asked on every paint of the `/settings` pane (settingsHost.Rows), so an os.ReadFile here
// would put the disk in front of each frame for as long as the pane is open.
//
// The answer loses nothing by refusing. Only ONE resolution still reaches this reader — a
// `system-prompt-models` entry matching the bound model with a `file:` — and its file is one the seed
// would read only to conclude what the entry's mere existence already says: this session sends the
// prompt the human configured for this model, so the editor opens empty (ADR 0067 Consequences,
// ADR 0064 §7). Every other file-bearing source is refused a line above. Keeping the refusal in the
// reader rather than in a fourth guard clause is what keeps the paint off the disk even if those
// clauses are one day loosened.
func promptSeedNoDiskRead(string) ([]byte, error) { return nil, errPromptSeedNoDiskRead }

// setContextFilesEnable flips the `context-files:` off-switch and reports the names to install with
// it. The pair is read and written under ONE lock because the engine takes it as a pair: an enable
// that read the names outside the lock could install a half of one edit beside a half of another.
//
// The resolved list on now is written in the same act, collapsed exactly as ApplyConfig collapses the
// block at startup — the names while the switch is on, nil while it is off — so an unattended run
// raised from a session that switched the block off reads no list at all rather than the names left
// standing behind the switch, and a re-enable installs the names kept aside here.
func (s *liveSettings) setContextFilesEnable(on bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFilesOn = on
	s.now.ContextFiles = nil
	if on {
		s.now.ContextFiles = slices.Clone(s.contextFileNames)
	}
	return s.contextFileNames
}

// setContextFileNames replaces the `context-files.names:` list and reports the switch to install it
// under — setContextFilesEnable's mirror, and the other half of the same pair. The resolved list on
// now follows the same rule: the new names reach it only while the switch is on.
func (s *liveSettings) setContextFileNames(names []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFileNames = names
	if s.contextFilesOn {
		s.now.ContextFiles = slices.Clone(names)
	}
	return s.contextFilesOn
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
// `response-reserve:` key it resolves against is file-only and cannot move mid-session (nothing
// writes now.ResponseReserve), so the raw and resolved answers part only where an entry DROPS an
// override the top-level key already matches — and there this errs toward riding, which reinstalls
// the number the session already holds rather than leaving a moved share waiting for the next bind.
func (s *liveSettings) setServers(servers []config.ServerEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	window, outputCap := config.ResolveContextWindow(s.entryWindow, s.now.ContextWindow), s.entryCap
	reserve := s.entryReserve
	s.now.Servers = servers
	for _, e := range servers {
		if e.Name != "" && e.Name == s.entryName {
			s.entryWindow, s.entryCap = int(e.ContextWindow), e.MaxOutputTokens
			// The re-read entry's own working room travels with them so the next bind, move or Firing
			// works in the room the file names NOW. It is deliberately absent from the moved-answer
			// below: nothing a rebind carries reads it (apogee.RebindSpec states no working room), so
			// riding on it would drive a re-resolution that changes nothing anybody sees.
			s.entryWorking = e.WorkingWindow
			// The re-read entry's own share travels with the two bounds so the next bind, move or
			// Firing divides this server's window the way the file says NOW — and it is part of the
			// moved-answer below, because a rebind now carries the share too
			// (apogee.RebindSpec.ResponseReserveFraction). An edit that moves only this one is in
			// force the moment it commits, through the ride this return value drives, rather than
			// waiting for whichever of those three comes first.
			s.entryReserve = e.ResponseReserve
			// The re-read entry itself travels with its three statements, so a Firing binds to the
			// server as the file spells it NOW rather than as it stood at the last move.
			s.entry = e
			break
		}
	}
	return config.ResolveContextWindow(s.entryWindow, s.now.ContextWindow) != window ||
		s.entryCap != outputCap || s.entryReserve != reserve
}

// modelProfileEntries reports the `model-profiles:` user tier as it stands now. It is the read the
// Sub-agent server's per-beat resolution matches ITS model against (delegation.go), and it goes
// through the holder for the reason every live read does: an edit committed mid-session must reach
// the next resolution rather than the next launch.
func (s *liveSettings) modelProfileEntries() []profiles.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now.ModelProfiles
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
	return s.now.RememberModel
}

// setSubAgentsServer mirrors the `sub-agents-server:` name routing now resolves against, pushed by
// the two seams that can move it: the `/sub-agents-server` pick (delegationHost.Retarget) and a
// re-read of the file (reloadServers). The store is the whole of what it can reach from here — the
// latch, the second heartbeat and the far seat all live in the routing wiring, which has already
// moved by the time this is called — and what it feeds is the run composed NEXT (firingConfig).
func (s *liveSettings) setSubAgentsServer(name string) {
	s.update(func(o *config.Options) { o.SubAgentsServer = name })
}

// generation hands back the Reaction surface as the session is running it — the value a PARTIAL
// edit modifies one field of, and the base the reload arm hangs a re-read observe list on. It is
// derived from now on every read (generationOf) rather than kept beside it: the keys it is made of
// are the overlay's own, so the value the engine seam takes and the projection a Firing composes
// from cannot describe two different edits. The two lanes come back as fresh slices that share no
// array with the overlay, for options()' reason: the value travels to the engine seam and on to a
// Runner that keeps it, and a caller that sorted or appended to the list it was given must never be
// editing the list this session is firing.
func (s *liveSettings) generation() apogee.Generation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return generationOf(s.now)
}

// generationOf is the one projection from the session's configuration onto the Generation the
// engine seam takes (SetReactions, ADR 0076 A8): the seven positive Floor keys through their one
// negation seam, `bypass:` and the two notice switches as is, and the resolved `reactions:` list
// divided into the observe rows the Runner fires and the sync rows the Agent runs. Both composition
// roots read the seed through it and every writer below hands its answer back through it, so a
// partial edit knows where the fields it does not touch stand without a second value remembering
// them. SplitLanes builds two fresh slices, which is what lets the answer leave without cloning.
func generationOf(o config.Options) apogee.Generation {
	observe, sync := domain.SplitLanes(o.Reactions)
	return apogee.Generation{
		Floor:             floorFromOptions(o),
		Bypass:            o.Bypass,
		ContextFillNotice: o.ContextFillNotice,
		StepBudgetNotice:  o.StepBudgetNotice,
		Observe:           observe,
		Sync:              sync,
	}
}

// setReactionLanes installs BOTH halves of the re-read `reactions:` list the session has just
// accepted — the observe rows its Runner fires and the sync rows its Agent runs — so a Firing
// raised inside this session composes from the Reactions the session is running rather than the
// ones the process launched with (firingBinding).
//
// Both lanes move together because one file arms both: a mirror that carried only the observe half
// would hand a Firing a `gate:` the session had already dropped, or drop one it had just armed.
//
// It is called AFTER the swap returned, never before, for setToolSet's reason: a refused list leaves
// the session firing the Reactions it already had, and a mirror written ahead of the swap would hand
// a Firing a list this session never ran.
//
// The `reactions:` key on now holds BOTH lanes in one list, which is what the resolved key is
// (config.Options.Reactions), so the two the generation keeps apart are folded back together here —
// generationOf splits them again for the engine seam, and a Firing raised from this session for its
// own two halves; one that saw only the observe half would run without the `gate:` the session is
// answering to.
func (s *liveSettings) setReactionLanes(observe, sync []domain.Reaction) {
	s.update(func(o *config.Options) { o.Reactions = append(slices.Clone(observe), sync...) })
}

// setToolSet mirrors the spec the live tool set was just BUILT from — the four keys that reach the
// session through liveTools' swap door. It takes the whole spec rather than the one value that
// moved because that spec IS what the running set was assembled from (liveTools.built), so the
// overlay and the set cannot end up describing two different edits; the profile roster it also
// carries is nobody's config key and is dropped here, and so is the `sub-agents-choice:` gate beside
// it — that one IS a key, but the spec holds it as a bool and the overlay holds the word the file
// spells, so it is recorded in its own language by recordSeatChoice.
//
// It is called after the door returned, never before: a refused SwapTools leaves the session on the
// set it already had, and an overlay written ahead of the swap would hand a Firing a roster this
// session never ran.
func (s *liveSettings) setToolSet(spec toolSetSpec) {
	s.update(func(o *config.Options) {
		o.WebSearchEndpoint, o.ToolsDisabled = spec.endpoint, spec.disabled
		o.URLAllowHosts, o.URLDenyHosts = spec.allowHosts, spec.denyHosts
	})
}

// setBypass moves `bypass:` on the overlay and hands back the WHOLE Generation the engine seam
// must be re-seeded with — setFloorGuard's shape, for setFloorGuard's reason: one swap carries the
// Floor gates and the observe list beside the switch, and a row that composed a fresh Generation
// from its own key alone would take away every guard and every armed Reaction the session has. The
// write and the derivation are one locked act, so the value handed back cannot restate a field a
// concurrent apply had just moved.
//
// The same key is what a Firing composed out of this session is armed from, since that Firing
// builds an Agent of its own that nothing pushed the toggle at.
func (s *liveSettings) setBypass(on bool) apogee.Generation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now.Bypass = on
	return generationOf(s.now)
}

// setFloorGuard flips ONE Floor-guard key on the holder and hands back the WHOLE Generation the
// engine must be re-seeded with. The read and the write are one locked act on purpose: the engine
// seam takes the seven gates, the `bypass:` switch and the observe list at once (SetReactions), so
// an apply that read the rest outside the lock could re-arm a guard a concurrent apply had just
// taken away.
//
// The key is moved in the FILE's positive spelling, on now — the seven positive keys are the overlay's
// own fields — and the engine's negative spelling is re-derived from them through the one negation
// seam, so this row and a start-up read the same seven keys the same way round and neither has to
// spell the negation a second time.
//
// An unknown key is a programming error the seven table rows cannot make, so it changes nothing and
// the generation is handed back as it stands. The key set floorGuardFields answers for is pinned to
// the engine's guard table and the config keys (TestFloorGuardTableMatchesTheConfigKeys).
func (s *liveSettings) setFloorGuard(key string, on bool) apogee.Generation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if field, known := floorGuardFields[key]; known {
		*field(&s.now) = on
	}
	return generationOf(s.now)
}

// floorGuardFields maps each Floor-guard key onto the config.Options field that spells it — the
// key→field mapping setFloorGuard writes through, kept here in the composition root beside
// floorFromOptions because the file's positive spelling is this package's to know. It is a map
// rather than a switch so its key set can be read: a test holds it equal to the engine's guard
// table (apogee.FloorGuardKeys) and the config keys (config.FloorGuardKeys), which is how a guard
// added to the engine without a row here is caught.
var floorGuardFields = map[string]func(*config.Options) *bool{
	"tool-use-enforcer":       func(o *config.Options) *bool { return &o.ToolUseEnforcer },
	"empty-response-recovery": func(o *config.Options) *bool { return &o.EmptyResponseRecovery },
	"tool-call-repair":        func(o *config.Options) *bool { return &o.ToolCallRepair },
	"tool-call-salvage":       func(o *config.Options) *bool { return &o.ToolCallSalvage },
	"tool-loop-breaker":       func(o *config.Options) *bool { return &o.ToolLoopBreaker },
	"tool-result-cap":         func(o *config.Options) *bool { return &o.ToolResultCap },
	"read-cache":              func(o *config.Options) *bool { return &o.ReadCache },
}

// setContextFillNotice moves the `context-fill-notice:` switch on the overlay and hands back the
// WHOLE Generation the engine seam must be re-seeded with — setBypass' shape, for setBypass'
// reason: the switch is one field of the generation the single seam takes (ADR 0077), so a row that
// composed a fresh Generation from its own key alone would take away every guard and every armed
// Reaction the session has. It is NOT a Floor key and never goes through setFloorGuard's negation
// seam: the file spells it positively and the engine reads it positively.
func (s *liveSettings) setContextFillNotice(on bool) apogee.Generation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now.ContextFillNotice = on
	return generationOf(s.now)
}

// setStepBudgetNotice is setContextFillNotice for the `step-budget-notice:` switch — the same
// field-of-one-generation shape, for the same reason, and likewise never a Floor key.
func (s *liveSettings) setStepBudgetNotice(on bool) apogee.Generation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now.StepBudgetNotice = on
	return generationOf(s.now)
}

// floorFromOptions is the ONE negation seam between the seven positive config keys and the engine's
// Disable… gates (ADR 0071: the zero FloorConfig is the full floor). Both composition roots and
// every `/settings` apply go through it, so "off in the file" and "off in the pane" cannot come to
// mean two different things.
func floorFromOptions(o config.Options) apogee.FloorConfig {
	return apogee.FloorConfig{
		DisableToolUseEnforcer:       !o.ToolUseEnforcer,
		DisableEmptyResponseRecovery: !o.EmptyResponseRecovery,
		DisableToolCallRepair:        !o.ToolCallRepair,
		DisableToolCallSalvage:       !o.ToolCallSalvage,
		DisableToolLoopBreaker:       !o.ToolLoopBreaker,
		DisableToolResultCap:         !o.ToolResultCap,
		DisableReadCache:             !o.ReadCache,
	}
}

// options reports this session's configuration as it stands NOW: the Options this run launched with,
// with every key a `/settings` commit has since applied written back over them — now, handed out as
// a copy. It is what an unattended run raised INSIDE the session composes from (ADR 0037's promise
// carried into the runs a session raises) — a Firing that budgeted, fenced and armed itself from the
// launch snapshot would silently ignore every edit made since, which is exactly the drift the
// boot-Config inheritance had.
//
// The overlay is what the APPLY recorded, never a re-read of the config file. The file can lag: a
// value the pane persisted and a seam then refused is written and not in force, and ADR 0037 makes
// the running session — not the file — the authority on what this session is configured as.
//
// What it deliberately does NOT answer is the WIRE. The endpoint and the key belong to the Upstream
// binding rather than to this holder (rebindInputs overlays them from the binding it is handed), and
// a caller composing a run against a named server carries that entry itself. Nor the bound entry's
// own pins: the window and the working room come back as the TOP-LEVEL keys alone, which is each
// field's own meaning (config.Options.ContextWindow, .WorkingWindow) and what a caller resolves the
// bound entry's pin over (firingBinding hands that entry back beside this projection).
//
// Every slice and map handed back is a COPY (cloneOptions). The value travels to another goroutine —
// a Firing composes on the Scheduler's — and a caller that sorted or appended to the roster it was
// given would be editing the set this session is running.
func (s *liveSettings) options() config.Options {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneOptions(s.now)
}

// firingBinding hands out everything a Firing raised inside this session composes from that lives in
// this holder: the live Options (options above) and the `servers:` entry the session is bound to as
// the holder holds it. Both from one call under ONE read lock, for rebindInputs' reason — read
// separately they could describe two different instants of a configuration the human is editing as
// the Scheduler reads it, and a run composed half from one instant and half from the next is a
// configuration nobody ever had.
//
// The entry is the held one (entry above) with ONE overlay. The wire is the Upstream binding's,
// which this holder deliberately does not own (options above), so the endpoint and the model are
// the binding's — a `/model` pick is honoured — and what comes back describes the server the session
// is on NOW, pins and all, whether or not the `servers:` list still names it.
//
// `parallel-agents:` is deliberately zeroed. The cap is parallelAgentsCap's, which already resolves
// that pin against what a beat observed, and a Firing takes it through the width seam instead
// (schedule.go); repeating the pin here would give the composer two answers to one question. The
// key SOURCE fields are cleared for the mirror-image reason: the session already resolved its key,
// and a Firing is handed that value rather than asking the source a second time. The description
// and the forced `effort-dialect:` are cleared too, so an in-session Firing keeps the orientation
// Delegations line it has always had and the session's observed dialect keeps outranking the
// entry's forced one; the Driver-parity alternative (ADR 0031) is a decision not taken here.
func (s *liveSettings) firingBinding(bound upstreamBinding) (config.Options, config.ServerEntry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry := s.entry
	entry.Endpoint, entry.Model = bound.Endpoint, bound.Model
	entry.ParallelAgents = 0
	entry.APIKey, entry.APIKeyCmd, entry.APIKeyEnv, entry.PlaintextKeyOK = "", "", "", false
	entry.Description, entry.EffortDialect = "", ""
	return cloneOptions(s.now), entry
}

// rebindInputs hands back the arguments rebindSpecFor takes: the session's live configuration
// (options above — the overlay, never the launch snapshot, so a rebind after a `/tools` or `/servers`
// commit re-resolves against the lists the session is running on) with the WIRE and the bound
// entry's own two bounds written onto the copy, plus the two token bounds beside it. It is the one
// place that projection is spelled out, so a caller cannot re-resolve half from the holder and half
// from the launch: every re-resolution — the rebind closure, a scheduled Firing — opens with this
// call. The bounds come back beside the copy for that same reason rather than through accessors of
// their own: read under ONE lock, they cannot describe two different instants of a `servers:` list
// that is being installed as they are read.
//
// It stays a projection of its own beside firingBinding rather than a re-read of options() because
// the two answer different questions of the same overlay: a Firing composes a whole run and takes
// the bound entry whole, while a rebind resolves ONE model against the server the session is on and
// wants that entry's pins already applied over the top-level keys (the window, the reserve).
//
// The charter covers the WIRE too, which this settings holder deliberately does not own: bound is the
// upstreamHolder's snapshot, and it is overlaid unconditionally because the holder — not the launch
// snapshot — is the authority on where this session is pointed (ADR 0036: one upstream definition).
// Without it a `/server` switch would leave the copy naming the LAUNCH endpoint, and anything keyed
// on the endpoint — a Firing composes its Config from this copy; the probe record was such an input
// until its startup reader went on 2026-09-16 — would be resolved against a server the session left.
// Both live callers run only after the startup bind, so the snapshot is always a real binding.
func (s *liveSettings) rebindInputs(bound upstreamBinding) (config.Options, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := cloneOptions(s.now)
	now.Endpoint = bound.Endpoint
	now.APIKey = bound.APIKey
	// The pin the resolution applies is the one in force for the server the session is on NOW: the
	// bound entry's `context-window:` when it names one, else the top-level key — the precedence
	// config.ResolveContextWindow spells, called inline because the read lock is already held here.
	// Resolved once, so the copy and the returned pin cannot disagree.
	pin := config.ResolveContextWindow(s.entryWindow, s.now.ContextWindow)
	now.ContextWindow = pin
	// And how that window is split on the server the session is on NOW: the bound entry's
	// `response-reserve:` over the top-level key (config.ResolveResponseReserve, the ranks the
	// window's own resolution spells). It is written ONTO the copy rather than handed back beside the
	// two bounds because `config.Options` spells this number as a session-wide share, which is
	// exactly what a caller reading the copy needs — a Firing composes its Config from it.
	now.ResponseReserve = config.ResolveResponseReserve(s.entryReserve, s.now.ResponseReserve)
	// And the other bound the server states: the reply ceiling the bound entry pins (ADR 0046). It
	// travels beside the window because the spec the caller builds carries it beside the window, and
	// it is handed back rather than written onto the copy because `config.Options` spells this number
	// as the STARTUP entry's — a session that has moved since is bound to the entry it is on now.
	return now, pin, s.entryCap
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
	// rebind is [tui.ServerHost.Rebind]'s own closure: the per-model re-resolution, which reads live
	// through rebindInputs and commits through the engine's idle-only Rebind.
	rebind func(model string, window int, effortDialect provider.EffortDialect) (tui.RebindResult, error)
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
	// hooks is the session's Reaction Runner (ADR 0073), which a re-read `reactions:` list is swapped
	// into wholesale — through the engine's one generation door (ADR 0076 A8), which holds the Runner
	// itself, so nothing here calls it. It is held as the `reactions` row's REACHABILITY: nil ⇒ this
	// Driver composed no Runner and an armed list would fire nowhere, so the key refuses on its own
	// row rather than reporting an edit that reached nothing.
	hooks *reactions.Runner
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

// applySettingFor builds the dispatcher behind [tui.SettingsHost.Apply]: the one place a key the pane has
// just persisted becomes a call on a live seam (ADR 0037 decision 1's apply step). It is keyed by
// REGISTRY PATH because that is the only name the renderer knows a setting by — the pane hands back
// the same path and the same file-spelled value it handed [tui.SettingsHost.Write], and the
// resolution from that string into whatever the seam takes happens here, where the schema lives
// (ADR 0031: the engine is handed values, never config text).
//
// The keys themselves are settingsTable below — one entry per key, carrying both what the apply
// needs composed and the apply itself — so this is a lookup rather than a switch, and the
// reachability question the same lookup read one field over (unreachable).
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
// the moment an external edit starts, so the write alone puts it in force and its entry says success
// with nothing to do (ADR 0041 decision 1). That is not the refusal above, because the file changing
// IS the session changing for a value only ever read from the file. `ui.inspector` and
// `response-reserve` take that same answer from the other side — they are read only at START-UP, so
// the write is the whole of what this session can do and their Descriptions carry the contract
// ("takes effect at the next start") that the row therefore does not have to.
//
// An EMPTY value means one thing for every key: the file no longer SETS this key, so resolve the
// built-in default a fresh start would have resolved. That is what a reset of a key whose default is
// unset hands in (the pane's settingsApplied), and every such key answers it through the entry it
// already has rather than a branch of its own — `web-search-endpoint` resolves "" to the built-in
// provider, `present.command` and `present.host` rebuild the ladder from a block with the field
// cleared, `tools.disabled` parses "" as the empty roster, the two `url-safety:` host lists parse it
// as the empty list the guard tightens nothing from, the `system-prompt-` pair re-read a file that no
// longer carries the key, and `editor` is in force from the write itself.
// TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault holds this side of it.
//
// The READING of a value is one per key, and it is the writer's: an apply that takes the value at
// all lands it through the registry row's Set (landSetting), so a value the pane refused to write
// and a value the apply refuses are refused by one admission in one sentence. The applies that
// never read the value — the structured keys re-read the file — and the hand-written ones the
// holder doc closes are the only ones that do not.
func applySettingFor(a settingsApplier) func(key, value string) (string, error) {
	return func(key, value string) (string, error) {
		// A member this Driver did not compose is a legitimate configuration rather than a bug (ADR
		// 0031: the engine stays sufficient for any Driver), so the key it would have been reached
		// through is refused in the dispatcher's own words — never dereferenced. Asked first, so a
		// key that cannot land does no work on its way to saying so.
		if err := a.unreachable(key); err != nil {
			return "", err
		}
		// A key with no entry at all is the refusal from the other side: this build knows no seam for
		// it, so the file changed and the session did not.
		entry, ok := settingsEntryFor(key)
		if !ok {
			return "", cannotApply(key)
		}
		return entry.apply(a, key, value)
	}
}

// settingsEntry is one key's row in the table below: everything the live apply has to know about
// that key, in one place rather than spread across two switches that could disagree about which
// keys exist at all.
type settingsEntry struct {
	// key is the registry path the pane names this setting by (config.KeyRegistry).
	key string
	// reaches reports whether this applier holds every member the apply below dereferences — the
	// predicate unreachable answers with, negated.
	reaches func(a settingsApplier) bool
	// apply puts the committed value into effect and answers the row's boundary note. It is handed
	// the key as well as the value because one apply can serve a GROUP of rows — the four `present.`
	// keys rebuild one ladder, the two `url-safety:` lists move through one door — and the key is
	// what tells those rows apart.
	apply func(a settingsApplier, key, value string) (string, error)
}

// settingsTable is the ONE list of keys a committed edit can reach the running session through.
// Both entry points are lookups over it — applySettingFor runs the entry's apply, unreachable
// negates the entry's reaches — so the drift the two switches this replaced could fall into (a key
// wired into one and not the other, which is a panic on the Update goroutine) has nowhere to
// happen: there is no second list of keys to keep in step.
//
// It is kept in config.KeyRegistry order, which is the order the pane renders the rows in, so the
// surface and the table can be read side by side (TestSettingsTableIsInRegistryOrder). A key with
// no entry here cannot be applied to a running session at all: applySettingFor refuses it by name,
// and TestEveryEditableSettingKeyHasAnApply is what fails if that key was Editable.
var settingsTable = []settingsEntry{
	{
		key: "servers",
		// The holder alone is enough to ACCEPT this key: the list itself reaches no engine seam (ADR
		// 0036), and the rebind the bound entry's two token pins ride is conditional — asked for only
		// by an edit that moved one of them. Requiring the whole riding triple here would refuse every
		// list edit on a Driver that composed no rebind, for a ride most list edits never ask for.
		reaches: reachesTheHolder,
		apply: func(a settingsApplier, key, value string) (string, error) {
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
			// rebind exactly as the top-level `context-window:` key does, through the same door,
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
		},
	},
	{
		key: "sub-agents-choice",
		// The swap door, `tools.disabled`'s class: what the gate decides is which SCHEMA sub_agent
		// publishes, and that is settled when the tool is constructed — so moving it builds the set
		// again and hands it to the engine (ADR 0037 binding F), rather than writing on a tool.
		reaches: reachesTheSwapDoor,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// An empty value is the pane's RESET, and it means what an absent key means: `fixed`, the
			// seat the `sub-agents-server:` key picks on its own — the row's own Default, which is what
			// landSetting lands for it, so no branch here has to know the default a second time.
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			choice := landed.SubAgentsChoice
			if err := a.tools.setSeatChoice(choice == config.SubAgentsChoiceModel, a.engine); err != nil {
				return "", err
			}
			// AFTER the door returned, for recordToolSet's reason: a refused SwapTools leaves the
			// session on the set it already had, and a holder written ahead of the swap would hand a
			// Firing a gate this session never ran.
			a.recordSeatChoice(choice)
			return seatChoiceNote, nil
		},
	},
	{
		key:     "mode",
		reaches: reachesTheEngine,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// Hand-written rather than landed through the row (landSetting): the apply is an engine
			// push of a domain.Mode, not an Options edit — the row's Set lands the file's word onto
			// the string field, and the ladder's own parse is what turns that word into the mode the
			// seam takes. The overlay is not written either: the engine holds the mode, and a Firing
			// composes its own from the schedule (ADR 0037 2026-08-25 note, with `/confine`).
			mode, err := domain.ParseMode(value)
			if err != nil {
				return "", err
			}
			a.engine.SetMode(mode)
			return "", nil
		},
	},
	{
		key:     "system-prompt-text",
		reaches: settingsApplier.rides,
		apply:   applySystemPromptBlock,
	},
	{
		key:     "system-prompt-file",
		reaches: settingsApplier.rides,
		apply:   applySystemPromptBlock,
	},
	{
		key:     "system-prompt-models",
		reaches: settingsApplier.rides,
		apply:   applySystemPromptBlock,
	},
	{
		// The fourth key of the same one prompt (ADR 0064 §2), so it lands on the same apply: what
		// the switch changes is which prompt the block resolves to, and only the re-resolution can
		// say that.
		key:     "use-default-prompt",
		reaches: settingsApplier.rides,
		apply:   applySystemPromptBlock,
	},
	{
		key:     "context-files.enable",
		reaches: reachesTheEngineAndTheHolder,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// Hand-written rather than landed through the row: the switch owns no Options field (its
			// registry row has no Set — the block resolves to ONE list, and the pair the pane edits is
			// held beside the overlay), so the bool is read here, in the kind's own sentence.
			on, err := strconv.ParseBool(strings.TrimSpace(value))
			if err != nil {
				return "", fmt.Errorf("apogee: %s is true or false, not %q", key, value)
			}
			// The names the session holds NOW: this run's resolution until a names edit replaced them.
			// A block that STARTED off resolved to no names at all (the two spellings of "off" collapse
			// at startup), so what switching it back on installs is whatever the names row has since
			// been given — which is why the two keys share one holder rather than one closure each.
			a.engine.SetContextFiles(on, a.live.setContextFilesEnable(on))
			return contextFileNote, nil
		},
	},
	{
		key:     "context-files.names",
		reaches: reachesTheEngineAndTheHolder,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// The value arrives as the FILE spells it — the one-line flow sequence the writer just
			// rendered — and is read back by the same parse, so the engine is handed the list a reader
			// takes out of the file rather than a second reading of the keystrokes. The switch travels
			// with it: names alone are not a state the engine can be put into.
			names := config.ParseSettingList(value)
			a.engine.SetContextFiles(a.live.setContextFileNames(names), names)
			return contextFileNote, nil
		},
	},
	{
		key:     "web-search-endpoint",
		reaches: reachesTheSwapDoor,
		apply: func(a settingsApplier, key, value string) (string, error) {
			if err := a.tools.setSearchEndpoint(value, a.engine); err != nil {
				return "", err
			}
			a.recordToolSet()
			return "", nil
		},
	},
	{
		key:     "mcp-servers",
		reaches: func(a settingsApplier) bool { return a.mcp != nil && a.tools != nil && a.engine != nil },
		apply: func(a settingsApplier, key, value string) (string, error) {
			// The one key whose value is a set of live CONNECTIONS. It is not pushed and not
			// re-resolved into a holder either: it is dialled, and the session moves onto the servers
			// that answered (ADR 0037 decision 6). The value the pane persisted is not read, for the
			// `servers:` reason — a list of blocks is a shape no single string spells.
			return "", a.reconnectMCP()
		},
	},
	{
		key:     "tools.disabled",
		reaches: reachesTheSwapDoor,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// The roster reaches the session as a whole tool SET rather than as a value on a tool, so
			// this is the swap door and not a re-point (setDisabled). The value arrives as the FILE
			// spells it and is landed through the row the writer rendered it by (landSetting), so the
			// set the session runs and the line the file carries cannot be two readings of one edit.
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			names := landed.ToolsDisabled
			if err := a.tools.setDisabled(names, a.engine); err != nil {
				return "", err
			}
			a.recordToolSet()
			// A name that matches no tool is reported on the row rather than refused, exactly as it is
			// at startup: the rest of the list has already applied, and saying so is the honest note.
			if unknown := config.UnknownToolNames(names); len(unknown) > 0 {
				return "no tool named " + strings.Join(unknown, ", "), nil
			}
			return toolRosterNote, nil
		},
	},
	{
		key:     "url-safety.allow-hosts",
		reaches: reachesTheSwapDoor,
		apply:   applyURLSafetyHosts,
	},
	{
		key:     "url-safety.deny-hosts",
		reaches: reachesTheSwapDoor,
		apply:   applyURLSafetyHosts,
	},
	{
		key:     "use-project-skills",
		reaches: func(a settingsApplier) bool { return a.skills != nil },
		apply: func(a settingsApplier, key, value string) (string, error) {
			return applySkillSourceGate(a, key, value, func(src *skills.Sources, landed config.Options) {
				src.UseProjectSkills = landed.UseProjectSkills
			})
		},
	},
	{
		key:     "use-shipped-skills",
		reaches: func(a settingsApplier) bool { return a.skills != nil },
		apply: func(a settingsApplier, key, value string) (string, error) {
			return applySkillSourceGate(a, key, value, func(src *skills.Sources, landed config.Options) {
				src.UseShippedSkills = landed.UseShippedSkills
			})
		},
	},
	{
		key:     "auto-compact",
		reaches: reachesTheEngine,
		apply: func(a settingsApplier, key, value string) (string, error) {
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetCompactionEnabled(landed.AutoCompact)
			// Mirrored onto the holder so a Firing raised from this session compacts the way the
			// session does — the engine holds the toggle, and a Firing builds an engine of its own.
			if a.live != nil {
				a.live.update(func(o *config.Options) { o.AutoCompact = landed.AutoCompact })
			}
			return "", nil
		},
	},
	{
		key:     "prune-tool-results",
		reaches: reachesTheEngine,
		apply: func(a settingsApplier, key, value string) (string, error) {
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetPruneToolResults(landed.PruneToolResults)
			// Mirrored onto the holder for auto-compact's reason above: a Firing raised from this
			// session prunes the way the session does, off an engine it builds for itself.
			if a.live != nil {
				a.live.update(func(o *config.Options) { o.PruneToolResults = landed.PruneToolResults })
			}
			return "", nil
		},
	},
	// The seven Floor-guard gates (ADR 0071). They share one apply and one engine seam: SetReactions
	// takes the WHOLE Generation, so a row that knows only its own key has to read the other six back
	// off the holder — which is why these are the one bool family that needs the holder as well as the
	// engine. Nothing else about them is special: each is an ordinary editable bool, on by default,
	// in force the moment its apply returns.
	{
		key:     "tool-use-enforcer",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "empty-response-recovery",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "tool-call-repair",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "tool-call-salvage",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "tool-loop-breaker",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "tool-result-cap",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	{
		key:     "read-cache",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyFloorGuard,
	},
	// The context-fill notice's switch (ADR 0077): the seven Floor rows' shape — one field of the
	// generation the single seam takes, so the row needs the holder for the fields it does not
	// name — but not a Floor guard, so it takes no part in their negation seam and ships off.
	{
		key:     "context-fill-notice",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyContextFillNotice,
	},
	// The step-budget notice's switch, the row above's twin (ADR 0077, 2026-09-15 addendum).
	{
		key:     "step-budget-notice",
		reaches: reachesTheEngineAndTheHolder,
		apply:   applyStepBudgetNotice,
	},
	{
		key: "delegate-max-steps",
		// No member of the applier is needed: the bound reaches no engine seam and rides no
		// re-resolution — the holder it is mirrored onto is optional in reloadServers' sense.
		reaches: reachesWithoutAMember,
		apply:   applyDelegateMaxSteps,
	},
	{
		key: "delegate-max-depth",
		// As delegate-max-steps above: no member, no seam, no re-resolution.
		reaches: reachesWithoutAMember,
		apply:   applyDelegateMaxDepth,
	},
	{
		key: "delegate-max-tokens",
		// As delegate-max-steps above: no member, no seam, no re-resolution.
		reaches: reachesWithoutAMember,
		apply:   applyDelegateMaxTokens,
	},
	{
		key: "delegate-timeout",
		// As delegate-max-steps above: no member, no seam, no re-resolution.
		reaches: reachesWithoutAMember,
		apply:   applyDelegateTimeout,
	},
	{
		key:     "undo-snapshots",
		reaches: reachesWithoutAMember,
		apply:   applyUndoSnapshots,
	},
	{
		key: "remember-model",
		// The holder alone, and for once that is the literal whole of the apply: the toggle reaches no
		// engine seam and rides no rebind — the seams it gates read it back out of this holder.
		reaches: reachesTheHolder,
		apply: func(a settingsApplier, key, value string) (string, error) {
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			// The one toggle with no engine seam behind it and no re-resolution to ride: what it gates
			// is a WRITE apogee will make later — the entry key an explicit `/model` pick or a committed
			// profile load records — and a decision the next start-up makes. So the holder store is the
			// whole apply, and the seams that ask (recordModelChoice, recordLaunchProfile,
			// launcherWiring.restore) read it from there at the moment they have something to record.
			a.live.update(func(o *config.Options) { o.RememberModel = landed.RememberModel })
			return "", nil
		},
	},
	{
		key:     "context-window",
		reaches: settingsApplier.rides,
		apply: func(a settingsApplier, key, value string) (string, error) {
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			// 0 keeps meaning discover-live (ADR 0024): the re-drive below binds the window the last
			// beat reported, so clearing a pin hands the session back to the server rather than to
			// "unknown". No note — the pin is what the Budget and Compaction measure against from the
			// moment the rebind commits.
			a.live.update(func(o *config.Options) { o.ContextWindow = landed.ContextWindow })
			return "", a.rideTheRebind()
		},
	},
	{
		key: "working-window",
		// No member of the applier is needed: the room reaches no engine seam and rides no
		// re-resolution — the holder it is mirrored onto is optional in reloadServers' sense.
		reaches: reachesWithoutAMember,
		apply:   applyWorkingWindow,
	},
	{
		key:     "response-reserve",
		reaches: reachesWithoutAMember,
		apply:   applyTheWriteAlone,
	},
	{
		key:     "present.auto-open",
		reaches: reachesThePresentation,
		apply:   applyPresentation,
	},
	{
		key:     "present.command",
		reaches: reachesThePresentation,
		apply:   applyPresentation,
	},
	{
		key:     "present.port",
		reaches: reachesThePresentation,
		apply:   applyPresentation,
	},
	{
		key:     "present.host",
		reaches: reachesThePresentation,
		apply:   applyPresentation,
	},
	{
		key:     "ui.inspector",
		reaches: reachesWithoutAMember,
		apply:   applyInspector,
	},
	{
		// The retention sweep runs once while a session is being WIRED, so nothing here can re-sweep
		// the store for a session already open — and there is no holder to mirror onto either, since
		// a Firing raises no sweep of its own. The write is therefore the whole of the apply, and the
		// Description's closing sentence carries the promise.
		key:     "sessions.max-age",
		reaches: reachesWithoutAMember,
		apply:   applyTheWriteAlone,
	},
	{
		key:     "sessions.max-count",
		reaches: reachesWithoutAMember,
		apply:   applyTheWriteAlone,
	},
	{
		key:     "editor",
		reaches: reachesWithoutAMember,
		apply: func(a settingsApplier, key, value string) (string, error) {
			// The one key with nothing at all behind it to move: the editor ladder reads `editor` off a
			// FRESH projection of the file every time an external edit starts (externalEdit.spec), so the
			// write the pane has just made is the whole of the apply and the very next ⏎ that opens an
			// editor runs the new command (ADR 0041 decision 1: "nothing to dispatch and nothing to
			// journal beyond the write itself"). It is named here rather than left to the default because
			// that default is a refusal — and a refusal over a value already in force would tell the user
			// their change had not taken effect when it had.
			return "", nil
		},
	},
	{
		key: "bypass",
		// The holder as well as the engine, for the seven Floor rows' reason: the switch is one field
		// of the generation the single seam takes, so the row has to read the fields it does not name
		// off the holder rather than compose a generation out of its own key.
		reaches: reachesTheEngineAndTheHolder,
		apply: func(a settingsApplier, key, value string) (string, error) {
			landed, err := landSetting(key, value)
			if err != nil {
				return "", err
			}
			// The floor travels with the session onto the runs it raises, for `auto-compact:`'s
			// reason: the engine holds it, and a Firing constructs one that nobody pushed it at — so
			// the holder move and the engine swap are the one call, on applyFloorGuard's terms.
			return "", a.engine.SetReactions(a.live.setBypass(landed.Bypass))
		},
	},
	{
		key: "reactions",
		// The engine holds the one swap door and the holder holds the generation it swaps, so both
		// are required — and the RUNNER beside them, which is the half that actually fires: a Driver
		// that composed none would take the edit, move the mirror and arm nothing, leaving the
		// session and the Firings it raises on two different lists (ADR 0073 §9 — one library at
		// every root, composed from one list).
		reaches: func(a settingsApplier) bool {
			return a.engine != nil && a.hooks != nil && a.live != nil
		},
		apply: func(a settingsApplier, key, value string) (string, error) {
			// A list of blocks is a shape no single string spells, so the value the pane persisted
			// is not read — the file layer is re-resolved exactly as startup resolved it, the
			// `mcp-servers:` reason. Nothing here is pushed at the engine either: observe Reactions
			// are told what happened, and swapping the list is the whole of what a session can do
			// about them.
			return "", a.reloadReactions()
		},
	},
	{
		key: "model-profiles",
		// The engine for the swap and the binding for the model to resolve the map AGAINST — the one
		// key that both pushes and re-resolves, so it needs a member from each class. The holder is
		// in the list because the map it stores is what the NEXT rebind reads.
		reaches: func(a settingsApplier) bool { return a.engine != nil && a.binding != nil && a.live != nil },
		apply: func(a settingsApplier, key, value string) (string, error) {
			// The map is an INPUT to the per-model resolution — but the model has NOT changed, and re-driving a whole rebind to move one field would refuse the
			// edit whenever an Exchange is open. So it takes the profile's own engine door instead
			// (ADR 0044 ratified call 6: Rebind is the model-switch door, SetProfile the same-model
			// config-edit one). The value the pane persisted is not read — a map of blocks is a shape
			// no single string spells.
			return "", a.reloadModelProfiles()
		},
	},
}

// settingsEntryFor finds the table row for one registry path. The scan is linear because the table
// is short and an apply happens at human speed — one keypress in the settings pane — so an index
// beside it would be a second structure to keep in step for no gain anybody could measure.
func settingsEntryFor(key string) (settingsEntry, bool) {
	for _, entry := range settingsTable {
		if entry.key == key {
			return entry, true
		}
	}
	return settingsEntry{}, false
}

// applySystemPromptBlock is the one apply behind all three `system-prompt-*` rows and the
// `use-default-prompt:` switch beneath them: they spell ONE prompt (ADR 0023, whole-entry selection;
// ADR 0064 §2 for the switch), so whichever row was committed re-resolves the block.
func applySystemPromptBlock(a settingsApplier, key, value string) (string, error) {
	// The value the pane persisted is deliberately not read here: these four keys are ONE
	// prompt (ADR 0023, whole-entry selection) and `system-prompt-models:` is a map no single
	// string spells, so the block is re-read from the file the pane just wrote and re-resolved
	// per model by the rebind — exactly the resolution startup made.
	if err := a.reloadSystemPrompt(); err != nil {
		return "", err
	}
	return "", a.rideTheRebind()
}

// applySkillSourceGate commits one of the two skill-source gates — `use-project-skills` and
// `use-shipped-skills` — with set copying the field that key owns off the landed value onto the
// Sources. Both are booleans over ONE skills.Sources value, so the layering is spelled the way
// startup spells it (this session's resolved roots) but the SIBLING gate is read back off the
// Provider rather than recomposed here: the two keys are committed independently, and a literal
// built from the key in hand alone would zero whichever gate the human is not currently editing.
//
// The apply is hand-written past the parse — the seam is the Provider's skills.Sources, not an
// Options field, and the overlay is not written: a Firing composes its own Sources from the same
// roots (ADR 0037). Re-pointing alone would change nothing anybody sees — the Provider serves the
// catalogue it has until asked for a fresh one — so the re-scan is part of the same act.
func applySkillSourceGate(
	a settingsApplier,
	key, value string,
	set func(src *skills.Sources, landed config.Options),
) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	src := a.skills.Sources()
	src.Home, src.Workspace = a.roots.config, a.roots.workspace
	set(&src, landed)
	a.skills.SetSources(src)
	// The scan's error is soft and is dropped here for the reason the "/" menu's reload drops it:
	// Load never signals an unusable catalogue — a malformed skill is skipped and shown in the
	// /skills report — so a partial scan is not a failed apply.
	_ = a.skills.Reload()
	return "", nil
}

// applyURLSafetyHosts is the one apply behind both `url-safety:` host lists — the key it is given
// is what picks which of the two lists moves.
func applyURLSafetyHosts(a settingsApplier, key, value string) (string, error) {
	// The host lists reach the session the way the roster above does and for a related
	// reason: the guard is built WITH the set (registryWithMCP hands one URLGuard to every
	// network tool) and no tool has a setter for it, so this is the swap door rather than a
	// re-point. The value arrives as the FILE spells it and is read back by the same parse
	// the writer rendered it with, and an EMPTY value is the empty list — which is the
	// built-in default a fresh start resolves: no allow-list narrowing, no configured deny,
	// and the guard's own SSRF floor still standing under both (it is not reachable from
	// configuration). An entry that normalises to nothing is dropped where the guard is
	// built, exactly as it is at startup, so there is nothing to report on the row.
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	// The lists the live connections were admitted under, read BEFORE the swap door moves them:
	// whether the MCP half below has anything to do is a question about the DIFFERENCE the edit
	// makes, and after the rebuild there is nothing left to compare against.
	before := a.tools.built()
	hosts, move := landed.URLAllowHosts, a.tools.setAllowHosts
	if key == "url-safety.deny-hosts" {
		hosts, move = landed.URLDenyHosts, a.tools.setDenyHosts
	}
	if err := move(hosts, a.engine); err != nil {
		return "", err
	}
	a.recordToolSet()
	// The same boundary the roster reports, because it is the same boundary: the set is
	// swapped on commit and the next request runs against the guard it carries. The MCP
	// connections are the other surface these lists bind, and they follow here rather than on
	// the next `mcp-servers:` edit — its note rides this one.
	return toolRosterNote + a.readmitMCP(before), nil
}

// readmitMCP moves the live MCP connections onto the servers the host lists NOW admit, and answers
// with what the url-safety row has to add about it — the empty string when there is nothing to say.
//
// It exists because the two surfaces the `url-safety:` lists bind are reached differently. Every
// network tool is handed the guard at construction, so the swap door above puts the new lists in
// force the moment it returns; the MCP guard is consumed at CONNECT time and never retained
// (internal/mcp/transport.go), so the only way a live connection follows a list is to dial again.
// Without this, an operator who closed a host kept talking to the MCP server on it until something
// else happened to reconnect (audit 2026-08-28: "after a `/settings` url-safety edit, network tools
// and the MCP connection disagree about which hosts are allowed").
//
// Connect is all-or-nothing, so the set is PARTITIONED first (mcp.Admit) and only the admitted
// servers are dialled: a server the new lists close is dropped and named, rather than costing the
// session every other server it was talking to.
//
// Three things it deliberately does not do.
//
// It does not dial when the admitted set is unchanged. A reconnect is a new process per stdio
// server and a handshake per HTTP one, run synchronously on the Update goroutine — so dialling on
// every host-list edit would freeze the frame, reset every server's state and break a stdio server
// that holds a lock or a port, all for a verdict that did not move. The common edit (a web-tool host,
// while a stdio server is connected) changes no verdict at all and must stay as instant as it is
// today, which is what comparing the admission under the OLD lists against the NEW one buys.
//
// It does not return an error. The tool rebuild above has already COMMITTED — the primary effect of
// the edit is in force — so a failed dial that came back as the row's error would report `saved —
// live apply failed` about an edit that applied. It lands in the note instead, in the same sentence
// liveMCP.reconnect words for the `mcp-servers:` row, so the human is told the same two things:
// what failed, and that the connections they had are still theirs.
//
// And it does nothing at all for a Driver that composed no MCP holder or no config path (ADR 0031's
// documented embedder: wire_mcp.go). The host lists still reach that Driver's tools; there is simply
// no connection for them to reach.
func (a settingsApplier) readmitMCP(before toolSetSpec) string {
	if a.mcp == nil || a.configPath == "" {
		return ""
	}
	// Only the FILE names an MCP server (no flag, no environment variable), so the set to partition
	// is read exactly as reconnectMCP reads it. A file that no longer parses leaves the connections
	// where they are and says so, for the reason above: this half cannot fail the row.
	file, err := a.fileConfig()
	if err != nil {
		return mcpNoteFor(mcpReconnectFailed(err))
	}
	now := a.tools.built()
	was, _ := mcp.Admit(file.MCPServers, mcpGuard(before.allowHosts, before.denyHosts))
	admitted, denied := mcp.Admit(file.MCPServers, mcpGuard(now.allowHosts, now.denyHosts))
	if sameServerNames(was, admitted) {
		return ""
	}
	if err := a.mcp.reconnect(admitted, a.tools, a.engine); err != nil {
		return mcpNoteFor(err)
	}
	var note strings.Builder
	for _, d := range denied {
		note.WriteString("; mcp server " + d.Name + " disconnected — " + deniedReason(d))
	}
	return note.String()
}

// deniedReason is what the note says about ONE server the re-admission dropped. A host the operator
// closed is reported as the policy decision it is — they have just edited that policy and the server
// name is the whole news. Anything else (an endpoint that is empty, or that does not parse) is a
// fault in the file the human has to see spelled out, and calling it "denied" would send them
// looking at the host lists for a typo that is not there.
func deniedReason(d mcp.Denied) string {
	if errors.Is(d.Err, mcp.ErrEndpointDenied) {
		return "its endpoint is denied"
	}
	return d.Err.Error()
}

// mcpNoteFor turns a failed re-admission into the clause the url-safety row carries. liveMCP's own
// sentence is reused verbatim rather than re-worded here so the two rows that can report a failed
// reconnect — `mcp-servers:` and this one — tell the human the same thing.
//
// The leading `mcp ` is the row's own label for whose subsystem is speaking, not part of the
// sentence, so the sentence it labels is taken de-labelled (mcpSentence): internal/mcp words its own
// refusals with an `mcp: ` lead of its own, and pasting one under the other stutters the word at the
// reader — `; mcp mcp: server "docs": …`, or `; mcp reconnect failed: mcp: server "docs": …` once
// mcpReconnectFailed has composed it. One label, at the front, where the row's sibling clause for a
// disconnected server already puts it. An error that never carried internal/mcp's label reads exactly
// as it did before, `; mcp <text>`, byte for byte.
func mcpNoteFor(err error) string { return "; mcp " + mcpSentence(err) }

// sameServerNames reports whether two admitted sets name the same servers in the same order. Both
// come from one partition of one slice, so order is the file's and equal order is equal membership;
// the NAME is the identity because that is what the connection is keyed by and what the tools it
// surfaces are prefixed with.
func sameServerNames(a, b []mcp.ServerConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// applyPresentation is the one apply behind all four `present.` rows: the ladder rebuilds from the
// whole block, so the committed row only names which field of it changed.
func applyPresentation(a settingsApplier, key, value string) (string, error) {
	return "", a.present.apply(key, value)
}

// applyTheWriteAlone is the apply for a key whose whole live effect IS the write the pane has
// already made, so there is nothing left here to dispatch.
func applyTheWriteAlone(a settingsApplier, key, value string) (string, error) {
	// `response-reserve` and the two `sessions:` keys are the ones that are genuinely START-UP only:
	// the reserve is read straight off the file into the budget the session opens with, and the
	// retention rules are read while the session is being wired, so the store has already been swept
	// by the time a pane can edit them. None of the three has a setter anywhere behind it — there is
	// no seam this build could reach and none they are candidates for.
	//
	// So they take `editor`'s answer rather than the default refusal, for `editor`'s reason turned
	// around: the write IS everything this session can do about the key, and a refusal would
	// report a failure over a save that did exactly what the key promises. The promise is the
	// Description's own closing sentence ("takes effect at the next start"), which the pane's
	// header carries — no row note, since ADR 0037 decision 3 gives a settings row a boundary
	// note only when the session itself moves.
	return "", nil
}

// applyDelegateMaxSteps is `delegate-max-steps:`, which is the write alone for THIS session and not
// for the runs it raises. The bound is a field of the Config an Agent was CONSTRUCTED with, so
// nothing here can tighten the session's own delegations — but a Firing builds a Config of its own
// out of options(), and mirroring the number onto the holder is what lets a bound the human just
// set bound the delegations of the runs this session raises.
//
// It answers exactly as applyTheWriteAlone does — success, no note, the Description's "takes effect
// at the next start" carrying the promise — while parsing the value, for applyInspector's reason: a
// value that is to be recorded has to be read.
func applyDelegateMaxSteps(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.DelegateMaxSteps = landed.DelegateMaxSteps })
	}
	return "", nil
}

// applyDelegateMaxDepth is `delegate-max-depth:`, on applyDelegateMaxSteps's footing exactly: the
// bound is a field of the Config an Agent was CONSTRUCTED with, so the write is the whole of the
// apply for this session, and the mirror onto the holder is what bounds the delegations of the
// Firings it raises. Success, no note, the Description's "takes effect at the next start" carrying
// the promise; the value is parsed because a value that is to be recorded has to be read.
func applyDelegateMaxDepth(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.DelegateMaxDepth = landed.DelegateMaxDepth })
	}
	return "", nil
}

// applyDelegateMaxTokens is `delegate-max-tokens:`, on applyDelegateMaxSteps's footing exactly: the
// budget is a field of the Config a child Agent is constructed with at spawn, so the write is the
// whole of the apply for this session, and the mirror onto the holder is what bounds the delegations
// of the Firings it raises. Success, no note, the Description's "takes effect at the next start"
// carrying the promise; the value is parsed because a value that is to be recorded has to be read.
func applyDelegateMaxTokens(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.DelegateMaxTokens = landed.DelegateMaxTokens })
	}
	return "", nil
}

// applyDelegateTimeout is `delegate-timeout:`, on the same footing, landed through the row whose
// validator and landing are both the ONE parser of the key (config.ParseDelegateTimeout) — the
// reading startup resolves by — rather than a second duration parse with an empty-value rule of
// its own: an empty value is the built-in default there, as it is for an absent key, and `0` is
// unbounded. A negative one is refused there too rather than trusted from the file, in the
// parser's own words.
func applyDelegateTimeout(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.DelegateTimeout = landed.DelegateTimeout })
	}
	return "", nil
}

// applyWorkingWindow is `working-window:`, which is the write alone for THIS session and not for the
// runs it raises. The room is a field of the Config an Agent was CONSTRUCTED with, so nothing here
// can re-bound the session's own Budget — but a Firing builds a Config of its own out of options(),
// and a `/server` move resolves an entry's bound over this number, so mirroring it onto the holder
// is what lets a room the human just set bound both.
//
// It answers exactly as applyDelegateMaxSteps does — success, no note, the Description's "takes
// effect at the next start" carrying the promise — while parsing the value, for that apply's reason:
// a value that is to be recorded has to be read.
func applyWorkingWindow(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.WorkingWindow = landed.WorkingWindow })
	}
	return "", nil
}

// applyInspector is `ui.inspector:`, which is the write alone for THIS session and not for the runs
// it raises. The wire observer is installed while a provider client is CONSTRUCTED (wire_boot.go),
// so nothing here can arm one on a session already talking to a server — but a Firing builds a
// client of its own out of options(), and mirroring the flip onto the holder is what lets it.
//
// It therefore answers exactly as applyTheWriteAlone does — success, no note, the Description's
// "takes effect at the next start" carrying the promise — while parsing the value, because a value
// that is to be recorded has to be read. The holder is optional in the reloadServers sense: a
// Driver that composed none has no Firing to compose either, and refusing the key over its absence
// would report a failure over a save that did what the key promises.
func applyInspector(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.UI.Inspector = landed.UI.Inspector })
	}
	return "", nil
}

// applyUndoSnapshots is `undo-snapshots:`, which is the write alone for THIS session and not for the
// runs it raises. The session's undo store is opened while the session id is minted and the journal
// is injected into the Agent then (ADR 0074), so nothing here can open — or close — the store of a
// session already running; mirroring the flip onto the holder is what lets the next Firing open its
// own store the way the human has just asked for.
//
// It answers exactly as applyInspector does — success, no note, the Description's "takes effect at
// the next start" carrying the promise — while parsing the value, for that apply's reason: a value
// that is to be recorded has to be read. The holder is optional in the same sense too: a Driver that
// composed none has no Firing to compose either.
func applyUndoSnapshots(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	if a.live != nil {
		a.live.update(func(o *config.Options) { o.UndoSnapshots = landed.UndoSnapshots })
	}
	return "", nil
}

// applyFloorGuard is the shared apply behind all seven Floor-guard keys. It writes the one key onto
// the holder and pushes the GENERATION that read hands back at the single engine seam, so the six
// keys this row does not name — and the `bypass:` switch and the observe list beside them — keep the
// values they had; the whole reason the write and the read are one locked act inside setFloorGuard.
//
// The refusal cannot arrive here in practice: a generation whose observe list did not move never
// reaches the Runner (lateEngine.SetReactions), and the engine half validates a Generation whose
// only moved field is a boolean — the sync lane beside it is the one it already armed. It is
// returned rather than discarded because that is the seam's contract, and a row that swallowed a
// refusal would be a row that lies about an edit the file already carries.
func applyFloorGuard(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	// The landed value is read back off the scratch through the same key→field map the holder
	// writes through, so the row and the overlay cannot name two different fields for one key.
	field, known := floorGuardFields[key]
	if !known {
		return "", cannotApply(key)
	}
	return "", a.engine.SetReactions(a.live.setFloorGuard(key, *field(&landed)))
}

// applyContextFillNotice is the `context-fill-notice:` row's apply, on applyFloorGuard's terms: the
// switch is written onto the holder and the GENERATION that write hands back is pushed at the single
// engine seam, so the seven Floor gates, the `bypass:` switch and the two lanes beside it keep the
// values they had. The engine half rebuilds its builtin ladder from the moved switch
// (Agent.SetReactions), so the notice is armed or disarmed the moment this returns; the refusal it
// returns cannot arrive here, for applyFloorGuard's reason.
func applyContextFillNotice(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	return "", a.engine.SetReactions(a.live.setContextFillNotice(landed.ContextFillNotice))
}

// applyStepBudgetNotice is the `step-budget-notice:` row's apply, on applyContextFillNotice's terms
// exactly: the switch is written onto the holder and the whole generation pushed at the single
// engine seam, which rebuilds its builtin ladder from the moved switch.
func applyStepBudgetNotice(a settingsApplier, key, value string) (string, error) {
	landed, err := landSetting(key, value)
	if err != nil {
		return "", err
	}
	return "", a.engine.SetReactions(a.live.setStepBudgetNotice(landed.StepBudgetNotice))
}

// reachesTheEngine reports whether the anytime-safe mutator class is composed: the keys that are
// PUSHED at the engine and are in force the moment their apply returns.
func reachesTheEngine(a settingsApplier) bool { return a.engine != nil }

// reachesTheEngineAndTheHolder reports whether the engine and the startup snapshot's mutable half
// are BOTH composed — the pair the two `context-files.` rows need, since either row installs the
// switch and the names together and only the holder remembers the half the row did not carry, and
// the pair the seven Floor-guard rows, the `bypass` row and the two notice rows need for
// the same shape of reason: SetReactions takes one whole Generation and only the holder remembers
// the fields those rows did not carry.
func reachesTheEngineAndTheHolder(a settingsApplier) bool { return a.engine != nil && a.live != nil }

// reachesTheHolder reports whether the live holder is composed. It is the whole of what two keys
// need, for two different reasons their entries give.
func reachesTheHolder(a settingsApplier) bool { return a.live != nil }

// reachesTheSwapDoor reports whether the tool set and the engine are both composed: a registry with
// no web_search to re-point is rebuilt and handed through SwapTools, which is the swap door and not
// this holder's to skip — and the roster switch and the two host lists are that door every time.
func reachesTheSwapDoor(a settingsApplier) bool { return a.tools != nil && a.engine != nil }

// reachesThePresentation reports whether the presentation ladder is composed — the one member every
// `present.` row's apply rebuilds through.
func reachesThePresentation(a settingsApplier) bool { return a.present != nil }

// reachesWithoutAMember is the predicate for a key whose apply reaches no member at all, so there
// is nothing a Driver could have been composed without: it answers yes for every applier, the zero
// one included. That is the honest answer for the keys whose apply is the write itself.
func reachesWithoutAMember(settingsApplier) bool { return true }

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
// It reads the same table the dispatcher applies out of, one field over, so the two can no longer
// disagree about which keys exist. A key with no entry is not unreachable here at all — it is
// refused by the dispatcher's own lookup, in the same sentence. TestApplySettingRefusesEveryKeyItCannotReach
// drives EVERY registry key through a zero applier and holds both halves of that.
func (a settingsApplier) unreachable(key string) error {
	entry, ok := settingsEntryFor(key)
	if !ok || entry.reaches(a) {
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

// recordToolSet mirrors the spec the live tool set was just built from onto the holder, so the four
// keys that reach the session through the swap door (`web-search-endpoint:`, `tools.disabled:` and
// the two `url-safety:` lists) are in the Options a Firing composes from. It reads the spec back off
// liveTools rather than taking the committed value, which is what keeps the overlay and the running
// set describing one edit — including the endpoint's fast path, where the tool is re-pointed in
// place and no set was built at all.
//
// It is called only after the door has RETURNED, so a refused swap leaves the overlay on the set
// the session is still running. A Driver that composed no holder records nothing, the posture
// reloadServers takes toward its own optional members (ADR 0031): the tool set moved either way.
func (a settingsApplier) recordToolSet() {
	if a.live == nil {
		return
	}
	a.live.setToolSet(a.tools.built())
}

// recordSeatChoice mirrors the `sub-agents-choice:` gate the swap door has just built the set under.
// It is recordToolSet's sibling rather than a line inside it because the two speak different
// languages about one edit: the spec carries the gate as the BOOL the tool takes, while a Firing
// composed out of this session is armed from the config word, and translating back from the bool
// would be a second place that decides what `fixed` means.
func (a settingsApplier) recordSeatChoice(choice config.SubAgentsChoice) {
	if a.live == nil {
		return
	}
	a.live.update(func(o *config.Options) { o.SubAgentsChoice = choice })
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
	// The server facts a beat observed, re-stated rather than re-derived: this rebind is driven by a
	// config edit, so the window and the effort dialect the heartbeat last named are what it has to
	// carry (ADR 0060 for the dialect, [liveSettings.observe] for both).
	_, err := a.rebind(model, a.live.observed(), a.live.observedDialect())
	return err
}

// fileConfig re-reads the config file this session resolved and answers its file layer — the one
// place that knows how a `/settings` apply reads the file the pane has just written to.
//
// Every structured key's apply starts here for the same reason: only the FILE carries those keys —
// no flag and no environment variable names a prompt, an upstream, an MCP server, a Reaction or a
// model profile — so resolving the file layer exactly as startup resolved it IS how the pane
// re-resolves them, and the loader answers with the same Options resolution starts from.
//
// Nothing is cached across applies: a row applies against the file as it is at that moment. The
// notice hook is a no-op because a live read never migrates: a file still in the retired schema is
// refused with the start-up refusal rather than folded under a running session (LoadFileConfig).
//
// A file that no longer parses is reported back unchanged: what a refusal COSTS differs per key —
// connections that stay where they are, a list nothing displaces — so each caller says that in its
// own words rather than being answered for here.
func (a settingsApplier) fileConfig() (config.Options, error) {
	return config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})
}

// reloadSystemPrompt re-reads the `system-prompt-*` block from the config file and installs it on the
// holder, validate-then-commit: a block the file cannot express — both spellings of one prompt at
// once, an entry with neither — is refused before it displaces a prompt that works.
//
// Only the FILE carries these keys (there is no flag or environment variable for a prompt), so
// re-reading the file resolves them exactly as startup resolved them — the loader answers with the
// same Options resolution starts from. The migration notice is dropped rather than surfaced: a file
// still in the retired schema was already migrated and announced at launch, and this read happens
// after the pane has just written to it.
func (a settingsApplier) reloadSystemPrompt() error {
	file, err := a.fileConfig()
	if err != nil {
		return err
	}
	sp := file.SystemPrompt
	if err := sp.Validate(); err != nil {
		return err
	}
	a.live.setSystemPrompt(sp, file.UseDefaultPrompt)
	return nil
}

// reloadServers re-reads the `servers:` block and installs it on the holder, validate-then-commit:
// an entry that could never be switched to — no name, no endpoint, or a name an earlier entry took —
// is refused by the SAME check startup runs, before it can displace a list that works.
//
// Only the FILE carries the list (no flag, no environment variable names an upstream), so
// re-reading it resolves the list exactly as startup resolved it — reloadSystemPrompt's own
// reasoning, and the reason the migration notice is dropped here too.
//
// It reports whether the re-read moved either token bound the BOUND entry resolves to — its window
// or its reply ceiling (setServers' own answer) — which is what tells the caller a rebind has to ride
// this apply. A refusal reports false with the error: nothing was installed, so nothing moved.
func (a settingsApplier) reloadServers() (bool, error) {
	file, err := a.fileConfig()
	if err != nil {
		return false, err
	}
	if err := config.ValidateServers(file.Servers); err != nil {
		return false, err
	}
	// The Sub-agent server the file now names (ADR 0045), re-pointed BEFORE anything is installed:
	// a re-list that cannot be installed has to leave the session on the list it was already
	// running, not half-way onto a new one. Both halves come from the SAME re-read: the
	// root `sub-agents-server:` key names the entry and the list carries it, so a save that moves
	// both resolves as one act.
	if a.delegation != nil {
		if err := a.delegation.relist(file.SubAgentsServer, file.Servers); err != nil {
			return false, err
		}
		// What the re-read RESOLVED to, mirrored onto the projection a Firing composes from
		// (delegationHost.Retarget's reason). It is read back off the wiring rather than taken from
		// the file, because the two are not the same answer: a save about some other entry leaves a
		// `/sub-agents-server` pick standing, and a projection that took the file's key as gospel
		// would send the next `/schedule` Firing to the server the human just moved off.
		a.live.setSubAgentsServer(a.delegation.targetName())
	}
	moved := a.live.setServers(file.Servers)
	// One thing in that list the engine holds and can be PUSHED: the fan-out width of the server this
	// session is on (ADR 0039 decision 2). Re-resolving it here is what makes `parallel-agents:` an
	// ADR 0037 key like the rest — moved in the pane, in force in the running session — rather than one
	// that waits for the next switch. The entry is matched back by name and the observed slot count is
	// kept: the file changed, the server did not. (The other two such numbers, the bound entry's window
	// pin and its reply ceiling, have no setter to push at and reach the engine on the caller's ride
	// instead.)
	if a.caps != nil {
		a.caps.relist(file.Servers)
	}
	return moved, nil
}

// reconnectMCP re-reads the `mcp-servers:` block and moves the session onto it. The dial and the
// registry swap are liveMCP.reconnect's; what belongs here is the same one thing every structured
// key's apply does — resolve the file layer exactly as startup resolved it — because only the FILE
// carries this key (no flag, no environment variable names an MCP server).
//
// A file that no longer parses is refused before anything is dialled, so a typo in an unrelated key
// cannot cost the session the servers it is talking to.
func (a settingsApplier) reconnectMCP() error {
	file, err := a.fileConfig()
	if err != nil {
		return err
	}
	return a.mcp.reconnect(file.MCPServers, a.tools, a.engine)
}

// reloadReactions re-reads the `reactions:` block and moves the session onto it (ADR 0073 §9, ADR
// 0076). It is reconnectMCP's shape for reconnectMCP's reason — only the FILE carries this key, so
// resolving the file layer as startup resolved it IS most of the apply — and it differs in what it
// does with the answer: the re-read list is hung on the generation the session is RUNNING and pushed
// through the one swap door, so the Floor gates and `bypass:` cross the edit untouched while the
// Runner's retired generation finishes what it already holds in the background and then stops.
//
// A file that no longer parses is refused before anything is swapped, and so is a Generation the
// engine will not arm (a sync entry named after a builtin — Agent.SetReactions) or a list the Runner
// will not take (an unresolvable `workspace:`): a broken edit costs the session nothing, and it
// keeps firing the Reactions it already had. A file that has gone BACK to the
// retired `hooks:` spelling is refused too, and nothing is written: the fold into `reactions:` is a
// startup act, because apogee does not rewrite a config file out from under a running session.
//
// The holder is written only once the swap has COMMITTED, so a refused edit leaves the session and
// the runs it raises describing the same list. That is also why nothing here reports: the swap
// drains the retired generation on a goroutine of its own, and this arm runs on the Update loop — a
// synchronous Report would deadlock the program against the send it is waiting for (bridge.go).
func (a settingsApplier) reloadReactions() error {
	file, err := a.fileConfig()
	if err != nil {
		return err
	}
	observe, sync := domain.SplitLanes(file.Reactions)
	gen := a.live.generation()
	gen.Observe, gen.Sync = observe, sync
	if err := a.engine.SetReactions(gen); err != nil {
		return err
	}
	a.live.setReactionLanes(observe, sync)
	return nil
}

// reloadModelProfiles re-reads the `model-profiles:` map, installs it on the holder, and swaps the
// profile it resolves for the model the session is bound to RIGHT NOW into the running engine
// (ADR 0044). The resolution is the composition root's — profiles.Resolve over the user map and this
// build's shipped table, exactly as startup and every rebind resolve it — and the VALIDATION is the
// engine's: SetProfile builds the profile's two collaborators before it commits, so a tool-call
// format this build cannot parse leaves the session reading responses exactly as it did, and says so
// on the row.
//
// The profile's roster axis follows through the tool set's own swap door, and only once the dialect
// has committed: SetProfile is validate-then-commit, so a profile this build cannot parse must move
// the tool set no more than it moves the parser. The engine's re-compose seam stands down under the
// registry this root injects (ADR 0057's Bounds), so the host that built the set re-composes it here.
// A refused swap IS returned — this path reports onto the settings row, which is the surface the
// human is looking at.
//
// A map the human emptied resolves to whatever the shipped table says for this model, and to the zero
// profile when it says nothing — which is what startup would have resolved it to. Deleting an entry
// asks for apogee's own answer back, not for whatever the process happened to launch with.
//
// With no model bound — a cold start before the first beat, or the gap a `/server` switch opens —
// there is nothing to resolve against and the holder carries the change alone, rideTheRebind's own
// posture: the first beat that binds a model resolves it in.
func (a settingsApplier) reloadModelProfiles() error {
	file, err := a.fileConfig()
	if err != nil {
		return err
	}
	a.live.update(func(o *config.Options) { o.ModelProfiles = file.ModelProfiles })
	model := a.binding().Model
	if model == "" {
		return nil
	}
	// The notice is dropped rather than returned: it is a resolution's narration for a model change
	// nobody made, and the row the pane is about to paint already says the edit applied.
	profile, _ := resolveModelProfile(model, file.ModelProfiles)
	if err := a.engine.SetProfile(profile); err != nil {
		return err
	}
	return a.tools.setProfileRoster(profile.Tools, a.engine)
}

// landSetting reads one committed value through its registry row's Set — the key's own validate
// hook, then the kind's parse, then the landing onto the typed field (config.Key.Set, the inverse of
// the row's Read) — onto a SCRATCH Options, and hands that Options back for the apply to read the
// field off. It is the one reading a live apply makes of the file-spelled value the pane handed it,
// and it is the writer's reading: a value refused here is refused in the sentence the settings pane
// already refuses the same keystrokes with, and a value it accepts is one the row admitted. Nothing
// the session holds is touched by it — the seam is driven off the scratch value, and the overlay is
// written by the apply itself only once the seam has returned (recordToolSet's order: a refused
// swap leaves the overlay on the set the session is still running).
//
// An EMPTY value is the pane's reset of a key whose default is unset, and it means one thing for
// every key (applySettingFor): the file no longer sets the key, so the row's own Default is what is
// landed — the built-in a fresh start resolves from a file that does not carry it, which is what
// the registry's defaults pin holds every Default to. A row this package cannot land — no row, or a
// row with no spelled inverse — is the dispatcher's refusal, since no apply of that key can read a
// value it has no reading of.
func landSetting(key, value string) (config.Options, error) {
	row, ok := config.LookupKey(key)
	if !ok || row.Set == nil {
		return config.Options{}, cannotApply(key)
	}
	if strings.TrimSpace(value) == "" {
		value = row.Default
	}
	var landed config.Options
	if err := row.Set(value, &landed); err != nil {
		return config.Options{}, err
	}
	return landed, nil
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
//     model, so it rides the same atomic Rebind as the prompt. Its THIRD axis,
//     the tool roster, travels with it (ADR 0057 decision 7) and is announced here when it moves.
//
// What it deliberately does NOT touch: the endpoint, the mode, the approvals and the conversation,
// none of which a model change has any claim on.
//
// A resolution failure is returned rather than swallowed — an unreadable per-model prompt file is
// the user's own config being wrong about the new model — and the caller then leaves the engine
// bound to what it had, which is the honest outcome.
func rebindSpecFor(
	opts config.Options,
	roots stateRoots,
	model string,
	window, pinnedWindow, outputCap int,
) (apogee.RebindSpec, []string, error) {
	next := opts
	next.Model = model

	// The per-model pair — the template and the shape the NEW model speaks the wire in — through
	// the one resolution startup uses too (resolveModelBindings), so the two doors cannot drift.
	// What it says out loud travels on the per-session notice channel, because to the human a
	// built-in shape match and the roster deltas it brings are one kind of fact: something apogee
	// decided about this model that nobody typed.
	bindings, err := resolveModelBindings(next, roots.config, model)
	if err != nil {
		return apogee.RebindSpec{}, nil, err
	}
	notices := bindings.Notices

	bound := window
	if pinnedWindow > 0 {
		bound = pinnedWindow
	}

	// The share the bound entry resolves to, which rebindInputs already wrote onto the copy this
	// resolver was handed. Taken into a local because the spec states it by pointer, the ceiling's
	// contract: nil would mean "the current share stands", and this resolver always has something to
	// say — including the 0 that says the override is gone and apogee's own default divides again.
	reserve := next.ResponseReserve

	return apogee.RebindSpec{
		Model:                   model,
		SystemPrompt:            bindings.SystemPrompt,
		MaxContextTokens:        bound,
		MaxOutputTokens:         &outputCap,
		ResponseReserveFraction: &reserve,
		Profile:                 bindings.Profile,
	}, notices, nil
}
