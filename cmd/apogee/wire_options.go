package main

// The renderer's Options, lifted out of wire.go by concern (ADR 0043).
//
// One projection: everything the composition root hands internal/tui, in one place — the launch-time
// facts, the seams the holders and verbs above sit behind, and the values this binary resolved so
// the renderer never reads a file, parses a config name or learns a path. It is built at the launch
// call and nowhere else, which is why it can be a plain method over the wiring: by the time it runs,
// every field it names is filled.
//
// Below it, the host capabilities that projection names as INTERFACES rather than as bare funcs
// (ADR 0054): settingsHost, the `/settings` pane's four acts over one config file, and schemeHost,
// the three things this program does with the schemes folder. Each is one value the literal hands
// over and one seam a renderer test fakes.

import (
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/tui"
)

// options projects the wired session onto [tui.Options] — the renderer's whole view of this host.
func (w *rootWiring) options() tui.Options {
	// The prompt caret's shape. ApplyConfig already refused a name this build does not know, so the
	// error here cannot fire; ignoring it keeps the parse a single expression, and ParseCursorShape
	// answers an unknown name with the default anyway — a caret is drawn either way.
	cursorShape, _ := tui.ParseCursorShape(w.opts.CursorShape)

	// The config file this session resolved, named once: the `/settings` write half splices it, the
	// apply half re-reads it, and the Mechanism sub-list below does both.
	configPath := filepath.Join(w.roots.config, "config.yaml")

	// The apply dispatcher, built ahead of the literal rather than inside it, because TWO seams reach
	// it now: the pane's own ⏎ ([tui.SettingsHost.Apply]) and the Mechanism toggle, whose write is
	// addressed by catalogue id and therefore cannot be handed to the pane's path-keyed pair — it
	// persists and applies behind one call, through this same dispatcher's `mechanisms` arm (ADR 0037
	// decision 1).
	applySetting := applySettingFor(settingsApplier{
		engine:     w.engine,
		live:       w.live,
		binding:    w.holder.Binding,
		rebind:     w.rebind,
		configPath: configPath,
		skills:     w.skillProvider,
		tools:      w.toolSet,
		mcp:        w.mcpSet,
		roots:      w.roots,
		present:    w.presentation,
		caps:       w.caps,
		delegation: w.delegation,
	})

	return tui.Options{
		// Both upstream facts are now honestly launch-time-only: Model is the configured pin ("" on
		// a cold start, where the footer says "connecting…" until the first beat binds one), and
		// ContextWindow is the `context-window:` pin in force for the server this session STARTS on
		// — the startup entry's own pin over the top-level key (ResolveContextWindow), which is the
		// very number the bind handed the engine, so the gauge and the Budget open on one server's
		// window. 0 when neither scope pins one. Neither is a discovery result any more — the
		// heartbeat and the rebind verb own everything after launch.
		Model:     w.opts.Model,
		Endpoint:  w.opts.Endpoint,
		Mode:      w.mode,
		Bypass:    w.opts.Bypass,
		Workspace: w.roots.workspace,
		// The apogee home THIS run resolved (--config / APOGEE_CONFIG included), so a report that
		// names a path — /skills telling an empty catalog where discovery looked — names the folder
		// the run actually walks rather than the ~/.apogee default it may not be using.
		ConfigHome:    w.roots.config,
		ContextWindow: w.live.window(),
		HostAlias:     w.opts.HostAlias,
		// The whole Upstream seam as one named capability (ADR 0054, wire_server.go): the servers
		// this session can move to, the two verbs that move or first bind it, the recording each
		// move makes for the next session, and the two ADR 0024 acts that keep the display live —
		// the monitor observing on the TUI's cadence and the verb applying what it implies.
		//
		// It is always wired and every act inside it always answers, which is what makes the
		// degrades [tui.ServerHost] documents this binary's to have none of: what a Driver composes
		// deliberately (ADR 0031), a full apogee session simply has.
		Server: serverHost{w: w},
		// Why this session may have no upstream yet — first boot, a `server:` naming an entry that
		// is gone, or nothing configured at all (ADR 0036 decisions 3, 4 and 7). On the ordinary
		// start it is the zero value, which says the engine was constructed before the program began
		// and leaves every flow below exactly as it was; the seam that ENDS the state is
		// [tui.ServerHost.Bind] above.
		Prebound: w.opts.Prebound,
		// And the same persistence one rung in, for the model rather than for the server
		// (`remember-model:`): an explicit `/model` pick becomes the `model:` key on the entry the
		// session is on, so that server comes back on it. Always wired — the toggle is read inside,
		// where a `/settings` flip reaches it — and skipped silently wherever there is nothing this
		// key can honestly record: an unlisted server, or a launcher-fronted entry, whose model is
		// chosen by loading a Launch profile instead.
		RecordModelChoice: w.recordModelChoice,
		// The whole llama-launcher seam as one named capability (ADR 0054, launcher.go): the
		// `/model`-over-profiles, `/unload-model` and `/stop-server` half of ADR 0029 — browse the
		// launcher's profiles, activate one, following it onto another server when it lives there,
		// and free or stop the server this session is on — plus the cheap on/off question the two
		// actuation verbs ask before they latch, the `launch-profile:` a committed load records, and
		// the boot check the TUI's Init asks once.
		//
		// It is always wired here and reports tui.ErrNoLauncher from inside while the integration is
		// off, because `off` is a value the human can change from inside the program (ADR 0037): the
		// nil-host degrade tui.LauncherHost documents is what a Driver composes (ADR 0031), never
		// this binary's.
		Launcher: launcherHost{w: w},
		// Where this session's DELEGATIONS run (ADR 0045, delegation.go): what may be delegated to, the
		// act that re-points the routing at another `servers:` entry mid-session, and the recording of
		// that choice. It is a host over the ROOT rather than the routing wiring itself, because only
		// the retarget is the wiring's: the offered list is the live `servers:` holder's and the
		// recording is a config splice. It is always present in this binary: what a Driver leaves nil
		// (ADR 0031), a full apogee session simply has.
		Delegation: delegationHost{w: w},
		// The resolved `ui:` block: which animation paints the status-line spinner, whether its
		// colour loop runs, whether the transcript's scroll bar is painted at all, and how long the
		// engine may go silent before the status line reports the quiet. Independent values, resolved
		// and validated by ApplyConfig, so the renderer selects rather than parses — the threshold
		// arrives as the duration it means, not as the text it was written as.
		// The scroll bar is the one key whose polarity flips here — the config says show, the
		// renderer's option says hide, so its zero value is the shown default (see tui.Options).
		Spinner:       w.opts.UI.Spinner,
		SpinnerColor:  w.opts.UI.SpinnerColor,
		HideScrollbar: !w.opts.UI.ShowScrollbar,
		StallAfter:    w.opts.UI.StallAfter,
		// And `ui.inspector`, which the ENGINE acts on (domain.Config.Inspector arms the capture) and
		// the renderer only words its empty pane with: /inspect names the key when nothing was
		// captured and it is off.
		Inspector: w.opts.UI.Inspector,
		// And `ui.skill-suggestions`, which is the renderer's alone: whether the band above the input
		// box names the skills that fit the draft (ADR 0061). It reaches no engine seam in either
		// state — a suggestion is a hint for the human, and the catalog stays invisible to the model
		// until a `/token` invokes one.
		SkillSuggestions: w.opts.UI.SkillSuggestions,
		// The `ui.color-scheme:` key, already resolved to the palette itself (wire_live.go): the name
		// so the renderer can say which scheme is in force, and the warnings the resolve produced so
		// it can tell the human why the screen is not the one they asked for.
		ColorScheme:         w.colorScheme,
		ColorSchemeName:     w.opts.UI.ColorScheme,
		ColorSchemeWarnings: w.colorSchemeWarnings,
		// And what keeps the scheme switchable from inside the program: what the picker offers, the
		// resolve behind an answer to it, and the export that CREATES a scheme file at all — one
		// named capability over one folder (ADR 0054), read on every ask so a file written or edited
		// mid-session is offered and loaded without a restart.
		Schemes: schemeHost{folder: w.roots.schemes},
		// The `cursor-shape:` key: the shape the REAL terminal cursor takes at the prompt caret
		// (steady always — there is no blink key). Selected here, like the palette above, so the
		// renderer never parses a config name.
		CursorShape: cursorShape,
		// The two hidden rendering-diagnostic seams (--tui-trace / --tui-diag), passed through as
		// the paths they are: empty on every ordinary run, and the renderer decides what a named
		// one means and when it goes live.
		TracePath: w.opts.TUITrace,
		DiagPath:  w.opts.TUIDiag,
		// The single source of truth (the embedded top-level VERSION file). Version carries the
		// full string (provenance included) that /version prints and --version mirrors; BaseVersion
		// is the release version alone (no provenance), the clean value the start-up box displays.
		Version:     apogee.Version(),
		BaseVersion: apogee.BaseVersion(),
		// The same backend, capabilities, and host id the degradation notice was built
		// from, so /confine status inside the TUI reports the host's real situation rather than
		// re-deriving it. internal/platform is the binary's dependency, not the renderer's.
		Confinement: tui.ConfinementInfo{
			Backend: probe.BackendName(w.confiner),
			Caps:    w.confiner.Capabilities(),
			HostID:  platform.HostID(),
		},
		// The `--save` half of `/confine off --save`: record THIS host in the same config.yaml
		// ApplyConfig read at startup, so the next run resolves unconfined here without the claim
		// following the file onto any other machine. The renderer learns only the path written —
		// the on-disk format is the binary's business, like the session Save seam below.
		SaveHostAcknowledgement: config.HostAcknowledgementSaver(
			filepath.Join(w.roots.config, "config.yaml"), platform.HostID()),
		// The start-up key-migration offer and the two answers that write anything (keymigrate.go,
		// ADR 0047). The renderer is handed the entry NAMES and the store's human name — never a key
		// — and each answer is one call back into this layer, which owns the store, the read-back
		// verification and the file format exactly as it owns the acknowledgement above. All three
		// stay zero on a run with no plaintext key or no usable store, and the renderer then raises
		// nothing.
		KeyMigration:     w.keyOffer,
		MigrateKey:       w.keyMigrator(),
		KeepPlaintextKey: w.plaintextKeyKeeper(),
		// The other start-up offer, in the same shape (keymigrate.go, ADR 0045): the entries whose
		// block still spells the retired `sub-agents: true` flag, and the one answer that writes
		// anything — the rewrite, and the retarget that puts it in force in this session. Both stay
		// zero on a config that carries no retired flag, which is every config written since.
		SubAgentsMigration:     w.subAgentsFlagged,
		MigrateSubAgentsServer: w.subAgentsMigrator(),
		// The whole `/settings` seam as one named capability (ADR 0054): the rows the pane shows,
		// the write and the reset that splice this run's config.yaml, and the apply that puts the
		// key just persisted into effect. Everything behind it — the registry, the file format, the
		// resolution onto a live engine seam — is this layer's, which is what keeps the pane an
		// idiom over rows it did not compose.
		Settings: settingsHost{
			opts:       w.opts,
			live:       w.engine,
			configPath: configPath,
			edits:      w.externalEdits,
			apply:      applySetting,
			// The seed is asked of the holder for the model this session is BOUND to and the config
			// home its prompt files resolve against — the same two inputs rebindSpecFor resolves the
			// running prompt with, so the editor opens on exactly what the run sends.
			promptSeed: func() string {
				return w.live.promptEditorSeed(w.holder.Binding().Model, w.roots.config)
			},
		},
		// The `mechanisms:` block's own two seams — the one row of the pane whose children are edited
		// in a list rather than in the file. What the list OFFERS is the catalogue this build carries,
		// sorted canonically, each id answered from the FILE's manual block (absent ⇒ off) rather than
		// from the resolution this run started on: the block is one a human also edits by hand, and it
		// is re-read per ask so an edit made in another window shows in an open list.
		//
		// A file that cannot be read answers with the catalogue all-off rather than with nothing,
		// which is the same degrade the resolution itself takes: the ids exist whatever the file says,
		// and a list that vanished on an unreadable config would look like a build with no Mechanisms.
		ListMechanisms: func() []tui.MechanismToggle {
			enabled := mechanismBlock(configPath)
			known := mechanisms.KnownIDs()
			toggles := make([]tui.MechanismToggle, 0, len(known))
			for _, id := range known {
				toggles = append(toggles, tui.MechanismToggle{ID: string(id), Enabled: enabled[string(id)]})
			}
			return toggles
		},
		// And the write half: one line spliced into that block and put in force on the same call
		// (writeMechanismFor).
		WriteMechanism: writeMechanismFor(configPath, w.externalEdits.refresh, applySetting),
		// The `$EDITOR` round trip for the keys no row can hold (ADR 0037 decision 5): out through a
		// command line this binary resolves — the file, the key's own line, the editor this environment
		// names — and back through a re-read that says which keys changed. The pane applies them
		// through the same two homes an in-pane commit uses; nothing here applies anything, so the
		// file's authority and the apply's single path both stay where they were (settingsedit.go).
		ExternalEditSpec: w.externalEdits.spec,
		ReloadConfig:     w.externalEdits.changed,
		// And the trigger that does not need an editor at all (ADR 0041 decision 3): one wait on the
		// watcher started in the assembly, answered when the file changes. What the renderer does with
		// the news is exactly what it does when an editor exits — re-read through ReloadConfig, apply
		// through the two homes above — so a saved file applies whoever saved it (decision 5).
		AwaitConfigChange: awaitConfigChangeOn(w.configWatch),
		Skills:            w.skillProvider,
		// Re-scan the skill source dirs when the merged "/" menu opens, swapping in a fresh catalog
		// on the shared Provider — the same one Config.Skills resolves against — so a skill added
		// mid-session both shows and attaches. The error is soft (Provider.Reload never signals
		// unusable), so it is dropped.
		//
		// The renderer calls this from a Cmd goroutine, off its Update loop, so it runs concurrently
		// with the loop resolving skills against the same Provider. That is what Provider is for: it
		// swaps a whole immutable catalog under an atomic pointer, so a reader sees one snapshot or
		// the other and never a torn one (internal/skills/provider.go).
		ReloadSkills: func() { _ = w.skillProvider.Reload() },
		// The store-backed session host drives all persistence (per-Turn saves, /sessions, quit
		// flush); the renderer sees only the SessionHost seam. Resumed carries the startup-replay
		// payload for a --resume/--continue start (nil on a fresh start), so newModel repaints the
		// stored scrollback beneath the start-up box and relights the gauge.
		Sessions: w.host,
		// Prompt recall: this workspace's own list of sent inputs, which the box walks with ↑/↓.
		// The workspace is bound HERE — the renderer resolves no paths — and the store is always
		// wired, because an empty recall file and an unwired seam look identical to the human
		// until they have sent something.
		Recall: newRecallHost(w.roots.prompts, w.roots.workspace),
		// The naming half of the same records: the seam that turns a first prompt into a title, and
		// the `auto-title:` key that says whether a new session names itself without being asked.
		// The seam is wired either way — the key is a preference about automatism, not a ban on the
		// call — so `/rename` regenerates on demand regardless.
		GenerateTitle: w.titles.generate,
		AutoTitle:     w.opts.AutoTitle,
		// The same key gates the DELEGATION namer (ADR 0068), and that one lives on this side: the
		// renderer applies `auto-title:` locally, so without this hook a `/settings` flip would move
		// the session's own naming and leave the host generating delegation names nobody asked for.
		OnAutoTitle: w.namer.setEnabled,
		// The scheduler surface (ADR 0033): the seam /schedule and /schedule-stop drive, the reason
		// auto is unavailable on this host (empty ⇒ it is available, and the picker offers it), and
		// the activity report the Gate above releases a due Firing on. All three are wired together
		// or not at all — the renderer's nil check on the seam speaks for the set.
		Schedules: w.schedules,
		ScheduleAutoBlocked: scheduleAutoBlocked(
			probe.BackendName(w.confiner), w.confiner.Capabilities(), w.opts.ConfineToWorkspace),
		ReportActivity: w.gate.report,
		// engine.InExchange() reads the resumed Agent's open-Exchange state (false on a fresh start,
		// or a cleanly-closed resume; true only when the stored snapshot died mid-task), so newModel
		// appends the interrupted note and /continue picks the work back up. A pre-bound start has
		// no Agent to ask, and answers false: nothing is open until something is bound.
		Resumed: resumedSession(w.resumed, w.engine.InExchange()),
	}
}

// mechanismBlock is the config file's own `mechanisms:` map — which Mechanisms the human has switched
// on and off BY HAND — read fresh from the file rather than taken from the resolution this run
// started on, exactly as the apply dispatcher re-reads it (settingsApplier.reloadMechanisms) and for
// the same reason: the block is one an editor in another window can change under a running session.
//
// A file that cannot be read or parsed answers with an empty block rather than with an error,
// because the only reader is a LIST and an absent block already means the same thing: nothing is
// switched on. The write half is where an unusable config has to be reported, and it reports it.
func mechanismBlock(path string) map[string]bool {
	file, err := config.LoadFileConfig(path, os.ReadFile, func(string) {})
	if err != nil {
		return nil
	}
	return file.Mechanisms
}

// writeMechanismFor builds [tui.Options.WriteMechanism]: one line spliced into the `mechanisms:` block
// and put in force on the same call. It is [settingsHost.Write]'s shape one level in — the splice,
// the baseline re-take and the live apply in the order they are there — with the apply reaching the
// dispatcher's `mechanisms` arm, which re-reads the whole block exactly as it does after an edit
// made in $EDITOR.
// The value handed to it is empty because that arm reads none: the block is a shape no single string
// spells.
//
// The two halves fail differently and the seam says which, because the pane has two sentences for them
// (ADR 0037 decision 1): a refused splice changed no file and answers (false, err); a splice that
// landed under a failed apply answers (true, err), the file ahead of the session. It is named here
// rather than closed over inline for applySettingFor's reason — the chain is the binary's own
// behaviour and is pinned as such (wire_options_test.go).
func writeMechanismFor(
	configPath string,
	refresh func(),
	apply func(key, value string) (string, error),
) func(id string, enabled bool) (bool, error) {
	return func(id string, enabled bool) (bool, error) {
		if err := config.SaveMechanismSetting(configPath, id, enabled); err != nil {
			return false, err
		}
		refresh()
		_, err := apply(settingKeyMechanisms, "")
		return true, err
	}
}

// ----------------------------------------------------------------------------
// The host capabilities Options names as interfaces (ADR 0054)
// ----------------------------------------------------------------------------

// settingsHost is this binary's [tui.SettingsHost]: the four acts of the `/settings` pane, which
// are four faces of one config file — the rows it shows off the resolution THIS run made, the write
// and the reset that splice that file, and the live apply of the key just persisted (ADR 0035, ADR
// 0037). It holds what those acts need rather than four closures over the wiring, so the family
// crosses to the renderer as one value and is faked in a test as one seam.
type settingsHost struct {
	// opts is the resolved snapshot the rows are projected from, so the pane reports the resolution
	// THIS run made: a key persisted mid-session is applied by apply below and shown from the pane's
	// own journal, marked ` *` (ADR 0037 decision 8).
	opts config.Options
	// live is the running session's answer for the two keys the resolution above cannot answer: the
	// autonomy mode and Auto's blast radius move during a session without the file hearing about it,
	// so Rows overlays them from the engine (settingsrows.go). nil where there is no engine to ask.
	live runningSettings
	// configPath is the file both writes splice — the same config.yaml the host acknowledgement is
	// recorded in and the watcher is looking at.
	configPath string
	// edits is the external-edit baseline every landed write re-takes (ADR 0041 decision 8).
	edits *externalEdit
	// apply is the live-apply dispatcher (wire_settings.go). It stays a func because the Mechanism
	// toggle reaches the same dispatcher by catalogue id, on a path this seam has no method for.
	apply func(key, value string) (string, error)
	// promptSeed is what the `system-prompt-text` editor opens on when the session's whole prompt
	// resolution IS the embedded default ([liveSettings.promptEditorSeed], wire_settings.go) —
	// apogee's embedded default prompt, or nothing at all when anything the user configured (a
	// global prompt, a per-model entry matching the bound model, a layer) is what the run sends
	// instead. It is a func because the answer is the SESSION's and is re-asked per paint, like
	// every other row: a prompt written through the pane stops the seeding from the moment it
	// lands. A nil func seeds nothing, the answer a Driver that composed this host without a
	// settings holder honestly has.
	promptSeed func() string
}

// Rows is every key the registry describes, with the value this run resolved and the marker for a
// key an environment variable or a flag overrode (settingsrows.go), and — for the two keys the
// engine rather than the file holds — the value the session is RUNNING (overlayLiveSettings). The
// one text row's prose is seeded with apogee's embedded default prompt where that default is what
// the session resolves (seedPromptEditor), so the editor ⏎ opens starts from the prompt in force
// rather than blank.
// It is re-derived per ask because the pane derives its rows on every paint — the picker's
// convention — which is also what makes the overlay and the seed enough: a mode cycled with the
// pane open shows on the next paint, and a prompt written through the pane stops seeding from the
// keypress that lands it, with nothing to invalidate.
func (h settingsHost) Rows() []tui.SettingRow {
	rows := overlayLiveSettings(settingsRows(h.opts), h.live)
	if h.promptSeed == nil {
		return rows
	}
	return seedPromptEditor(rows, h.promptSeed())
}

// Write persists one key per deliberate edit, spliced into the config file (ADR 0035). The registry
// decides what may be written and the splice writer owns the file (internal/config/configwrite.go) —
// the renderer hands over a path and the value as the file spells it, and learns only whether it
// landed.
//
// Every landed write re-takes the external edit's baseline (ADR 0041 decision 8). The pane applies
// the key it just persisted in the same keypress, and the watcher is looking at the very file this
// wrote: without the refresh, apogee's own write comes back a second later as somebody's edit and
// applies twice — which for `mcp-servers:` is a second dial of every server. A write that FAILED
// changed no file and refreshes nothing.
func (h settingsHost) Write(key, value string) error {
	if err := config.SaveConfigSetting(h.configPath, key, value); err != nil {
		return err
	}
	h.edits.refresh()
	return nil
}

// Reset is the same write in reverse: the key's active line is REMOVED, so the value goes back to
// the binary's default rather than being pinned to today's spelling of it. It refreshes the same
// baseline for the same reason — a removed line is a change to the file like any other.
func (h settingsHost) Reset(key string) error {
	if err := config.ResetConfigSetting(h.configPath, key); err != nil {
		return err
	}
	h.edits.refresh()
	return nil
}

// Apply is the apply half of the same keypress (ADR 0037): what the file now says, the session now
// runs. The dispatcher owns the resolution from a registry path and a file-spelled value onto a live
// engine seam — the renderer holds neither schema nor engine mutator.
func (h settingsHost) Apply(key, value string) (string, error) { return h.apply(key, value) }

// schemeHost is this binary's [tui.SchemeHost]: the schemes folder, and the three things the program
// does with it (ADR 0040). All three read the folder on the ask rather than answering from a
// snapshot, which is what lets a file the human writes mid-session be offered, loaded and shadowed
// without a restart.
type schemeHost struct {
	// folder is the user's schemes directory — the one this run resolved (roots.schemes), never a
	// path the renderer knows.
	folder string
}

// List names every scheme that can be switched to: the built-ins plus every `*.yaml` in the folder,
// a user file shadowing a built-in of the same name (ADR 0040 design call 6).
func (h schemeHost) List() []string { return scheme.Discover(h.folder) }

// Resolve turns one of those names back into a palette — the same call this binary made at boot
// (wire_live.go), re-run so a switch re-READS the file. ok is always true: this host can always
// resolve, because the load is forgiving and answers a defective file with warnings and a usable
// palette rather than with a failure.
func (h schemeHost) Resolve(name string) (scheme.Scheme, []string, bool) {
	s, warnings := resolveColorScheme(name, h.folder)
	return s, warnings, true
}

// Export copies a built-in into the same folder, which is what makes an embedded palette editable at
// all — and never overwrites, so an export cannot destroy work in progress (design call 7).
func (h schemeHost) Export(name string) (string, error) { return scheme.Export(name, h.folder) }
