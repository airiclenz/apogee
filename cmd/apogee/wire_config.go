package main

// The one Options→Config projection the two Drivers of this binary share (ADR 0031, ADR 0043).
//
// A session's boot phase (wire_boot.go) and the unattended composer (wire_firing.go) each build an
// [apogee.Config], and most of what they build is the SAME key read off the SAME [config.Options]
// field: the roster rungs, the host lists, the scrubbed variables, the mounts, the delegation
// bounds, the Floor. Two literals that spell those keys identically are two chances for one
// configuration to mean two different runs depending on which Driver read it — the drift ADR
// 0031's benchable-all-the-way-up shape rules out — so the shared keys are filled here, once, and
// each Driver overlays only what genuinely differs between a watched session and a run nobody
// watches: the binding-derived keys (endpoint, model, key, prompt, profile, the window budget) and
// the human seams (Approver, Asker, Presenter, the pre-emption door).

import (
	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/skills"
)

// projectConfig fills every [apogee.Config] key both Drivers read the same way from one resolved
// [config.Options] value: the file-only and flag-bound keys that describe the CONFIGURATION rather
// than the run — which tools are on the menu, which hosts the network tools may reach, which
// variables a subprocess may not read, which files and skill libraries the model may read, how far
// a delegation may run, which Floor guards are switched off. What comes back is the base a caller
// overlays its own keys onto; nothing here is per-server or per-human.
//
// It is called after hostToolchain.start: the read-roots func it composes lists the toolchain
// roots the probe answers, and a caller that composed it first would mount a library that never
// fills. Both Drivers already start the probe before they reach this — a session in newRootWiring,
// a Firing in firingConfig — so the order is a fact about them, stated here so it stays one.
func projectConfig(
	opts config.Options,
	roots stateRoots,
	confiner apogee.Confiner,
	mode apogee.Mode,
	skillProvider *skills.Provider,
) apogee.Config {
	return apogee.Config{
		Mode:         mode,
		Bypass:       opts.Bypass,
		ConfigDir:    roots.config,
		WorkspaceDir: roots.workspace,
		// The OS confinement backend this run is fenced by — the host's real one (landlock on
		// Linux, seatbelt on macOS, denyConfiner elsewhere — confinement-execution-contract §2.6),
		// so Auto WORKS where fs-confinement exists and gates the subprocess surface where it does
		// not. A Firing is handed the session's own, so an Auto Firing sits in the same box an Auto
		// session on this configuration would. The posture beside it is the CONFIGURED one: a
		// `/confine` toggle moves the blast radius on the live engine and nothing mirrors it onto
		// the settings holder, which is the second named exception to "a Firing sees exactly what
		// the session sees" (ADR 0037, note of 2026-08-25).
		Confiner:           confiner,
		ConfineToWorkspace: opts.ConfineToWorkspace,
		WebSearchEndpoint:  opts.WebSearchEndpoint,
		// The `tools.disabled:` roster switch — the built-in tools this configuration takes off the
		// menu (empty ⇒ the whole roster) — and the `tools.enabled:` lift, the same global rung's
		// ADD direction (ADR 0057): the built-in tools it puts back on the menu when the build or
		// the list beside it leaves them off (empty ⇒ nothing is lifted). Both ride Config rather
		// than the assembly alone so every Driver — a session, a headless run, an embedder — prunes
		// and lifts the same roster from the same value.
		DisabledTools: opts.ToolsDisabled,
		EnabledTools:  opts.ToolsEnabled,
		// The `url-safety:` host layer: the hosts the network tools may reach and the hosts they
		// may not. Empty ⇒ every host, exactly the reach before this key existed — and never less
		// safe either way, since the guard's SSRF floor is not reachable from configuration. It
		// rides Config for DisabledTools' reason: every Driver must fence the same hosts from the
		// same value.
		URLAllowHosts: opts.URLAllowHosts,
		URLDenyHosts:  opts.URLDenyHosts,
		// `ui.inspector:` — whether this run captures its own wire traffic for /inspect. A session
		// reads it ONCE, here, because this is where the engine installs the observer: a
		// mid-session edit of the key changes the file and the next start, never the running
		// engine. A Firing has no /inspect pane to show the capture in, but its sink sees the
		// WireEvents like any other, which is the benchable-all-the-way-up shape (ADR 0031).
		Inspector: opts.UI.Inspector,
		// `undo-snapshots:` — whether this run images the workspace around each Exchange into a
		// store of its own (ADR 0074). A session opens the store itself (openSessionJournal,
		// wire_live.go) and reads the switch back off THIS key; run.Once opens a Firing's under
		// the record id its Driver minted. It rides the Config because the Config is what both are
		// composed from, and a flag a Firing could not read there would make the key silently a
		// TUI-only one — the Driver-parity break ADR 0031 rules out. It is what gives
		// `apogee undo <session-id>` something to reverse after a headless or scheduled run.
		UndoSnapshots: opts.UndoSnapshots,
		// Every variable this configuration reads an API key out of (`api-key-env:`, ADR 0047),
		// which the execution tools drop from the environment they hand a subprocess. It is the
		// union across ALL configured entries rather than the bound one's: `/server` switches
		// mid-session, and a scrub that followed the binding would leave the other entries' keys
		// readable in every `terminal` / `python_exec` / `run_tests` child until it happened. Empty
		// ⇒ apogee's own APOGEE_API_KEY alone, exactly the scrub before this key existed.
		// A webhook Reaction's `headers-env:` names variables holding a token too (ADR 0073 §6),
		// and they are scrubbed beside the key sources for exactly the same reason: a token
		// readable out of a `terminal` child is a token the model can read — and a Firing runs the
		// same `reactions:` list a session does, so it scrubs the same variables.
		SecretEnvVars: append(config.APIKeyEnvNames(opts), config.ReactionEnvNames(opts)...),
		// The workspace context files (`context-files:`, file-only): the names the engine looks
		// for in the workspace root at every session boundary, whose content rides the same first
		// system message as the prompt — verbatim, never as a template. Nil ⇒ the feature is off,
		// and the request is exactly what it was before the key existed.
		ContextFiles: opts.ContextFiles,
		// The skill catalog, serving both halves of the skills contract off ONE provider: it
		// resolves an attached ID into the prompt (Skills), and it is the SAME provider behind
		// the model-facing door (SkillLookup, ADR 0065 §6) — load_skill searches the catalog the
		// user's "/id" resolves against, so a skill added or edited mid-session is reachable
		// through both as soon as the next Reload lands, and an unattended run's model can reach a
		// written procedure without a human there to attach one. A nil lookup would simply leave
		// the tool out of the roster; wiring it is what puts the door in the menu.
		Skills:      skillProvider,
		SkillLookup: skillProvider,
		// The skill source dirs, mounted as read-only roots for the model's read tools: an
		// attached skill names its folder, and this is what makes that address readable
		// (read_file, list_dir, grep, find_files — nothing else; the dirs stay unwritable).
		// ReadRoots rather than SourceDirs, which only DISPLAYS the sources: it hands back each
		// dir's symlink-RESOLVED real path and drops a workspace anchor that resolves outside the
		// workspace, so a cloned repo shipping `.apogee/skills` as a symlink to /home or /etc
		// cannot relocate the read fence the way it could before (audit 2026-08-25 F-13).
		// It is the PROVIDER's method value, so the mount is live in both senses — it follows a
		// mid-session `use-project-skills` flip through SetSources, and it is re-read per tool
		// call rather than frozen here.
		// Sub-agents need no wiring of their own: a child's registry is a Subset of the parent's
		// tool INSTANCES (domain.ToolRegistry.Subset), so the same read tools — and with them
		// this same func — ride along at every depth.
		// The toolchain roots the host probed (toolchain_roots.go) follow the skill libraries on
		// the same func, in that order, so the orientation line and the mount list one library:
		// one probe per process, so a Firing raised inside a session composes over the answer the
		// session already has, and a headless or daemon run starts the probe itself.
		ExtraReadRoots: composeReadRoots(skillProvider.ReadRoots, hostToolchain.roots),
		// The same mount for the source that has NO host path: apogee's own shipped skills live in
		// the binary, so their bundled files are reachable only under the `shipped:<id>` address
		// their SKILL.md block announces (ADR 0065 §3). Without this the announced files: line
		// would name a folder every read tool refuses — the one thing an announced path may not
		// do. Like ReadRoots it is the PROVIDER's method value, so a `use-shipped-skills` flip
		// moves the mount with no re-wiring, and sub-agents inherit it through the same tool
		// instances.
		VirtualReadRoots: skillProvider.VirtualReadRoots,
		// The two structural context switches: CompactionEnabled carries the `auto-compact` key
		// (default on) — the budget-driven automatic trigger; the on-demand /compact runs
		// regardless of it — and PruneToolResults the `prune-tool-results` key (default on), the
		// stale-tool-result collapse that runs at a Turn boundary before Compaction is ever
		// reached. The window budget beside them (MaxContextTokens, WorkingWindow,
		// ResponseReserveFraction, MaxOutputTokens) is the caller's: it is resolved off the bound
		// `servers:` entry, which is exactly what differs between the two Drivers.
		Context: apogee.ContextConfig{
			CompactionEnabled: opts.AutoCompact,
			PruneToolResults:  opts.PruneToolResults,
		},
		// How far a delegation may run before the engine ends it: the `delegate-max-steps` key
		// (default 80; 0 ⇒ unbounded, what a delegation cost before the cap existed), how many
		// delegations one reply may fan out: `delegate-fanout-rounds` (default 2 rounds of the
		// width; 0 ⇒ no ceiling), how deep delegation may nest: `delegate-max-depth` (default 1 —
		// the session delegates, its delegates do not), and what one delegation may spend in
		// prompt tokens and wall clock: `delegate-max-tokens` (default 20M) and `delegate-timeout`
		// (default 2h), 0 disabling either. Read off the one Options both Drivers are raised with,
		// so a delegate is bounded the same whether a human is watching or not — and a Firing runs
		// while nobody watches, which is exactly the case a runaway delegation must not be able to
		// become.
		Delegation: apogee.DelegationConfig{
			MaxSteps:     opts.DelegateMaxSteps,
			FanOutRounds: opts.DelegateFanOutRounds,
			MaxDepth:     opts.DelegateMaxDepth,
			MaxTokens:    opts.DelegateMaxTokens,
			Timeout:      opts.DelegateTimeout,
		},
		// Which Floor guards this run goes WITHOUT (ADR 0071). The seven keys are positive in the
		// file and negative at the engine, and floorFromOptions is the one place that turns one
		// spelling into the other — so a guard the human took away, in the file or in `/settings`,
		// is taken away for the runs nobody watches too.
		Floor: floorFromOptions(opts),
		// Whether the engine's context-fill notice is on (ADR 0077): the `context-fill-notice`
		// key, default off. Not a Floor guard, so it is carried as is — no negation — beside
		// Bypass, which switches it off with the rest of the advise class.
		ContextFillNotice: opts.ContextFillNotice,
	}
}
