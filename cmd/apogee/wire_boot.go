package main

// The boot phase of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// What one run owns before a session exists — the skill catalogue, the Bridge the renderer is
// late-bound through, the presentation ladder this host can walk, the confinement backend it runs
// behind — then the base [apogee.Config] assembled from them, and the sentences this host's
// confinement posture owes the user while stderr is still a safe place to write.

import (
	"fmt"
	"os"
	"runtime"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// The confinement backend label and the Auto-degradation notice used below live in
// internal/probe: `apogee probe` reports the same verdict off-session and the TUI's
// /confine status renders it in-session, so the wording is extracted rather than copied —
// three surfaces, one sentence (Phase-5 item 3 / ADR 0021).

// unconfinedAutoWarning is what Auto says for itself when confinement is switched off
// (`confine-to-workspace: false`) — the only blanket loosen in the system (ADR 0012), so it is
// stated every time it is used rather than assumed remembered. It is a const because two surfaces
// now print it — the interactive launch below and an `apogee headless --mode auto` run — and a
// user who has read it at one of them must not meet a softer wording at the other.
const unconfinedAutoWarning = "apogee: WARNING — auto mode is running UNCONFINED " +
	"(confine-to-workspace: false). This is safe only inside a VM/container; " +
	"the dangerous-action guard is a footgun-net, not a security boundary."

// shouldPrewarmLabelWalk reports whether startup should eagerly run the confinement backend's
// one-time label walk (ADR 0020 §2) rather than let the first confined command pay it mid-session.
// It is the MIRROR of probe.DegradedNotice's gate — the same three inputs, FSWrite inverted: the
// degradation notice fires when Auto asks for confinement a host CANNOT enforce (FSWrite false),
// this fires when it CAN (FSWrite true), which on the Windows token backend is the disk-label pass
// worth pre-warming behind a progress notice. It returns true on the Linux and macOS backends too
// (they also report FSWrite under Auto+confine), where PrewarmLabelWalk is a genuine no-op — only
// the Windows-tagged labelBox pays a walk — so the Windows-vs-not distinction lives in that seam,
// not here. Pure so the decision is table-testable off Windows (the DegradedNotice seam pattern).
func shouldPrewarmLabelWalk(mode apogee.Mode, confineToWorkspace, fsWrite bool) bool {
	return mode == domain.ModeAuto && confineToWorkspace && fsWrite
}

// newRootWiring opens the run: the four host facilities that outlive everything below them, in the
// order the composition root has always built them. Nothing here can fail and nothing here reaches
// the network — which is why runRoot can register the teardown for all of it in one defer before
// the first fallible step.
func newRootWiring(opts config.Options, mode apogee.Mode, roots stateRoots) *rootWiring {
	w := &rootWiring{opts: opts, mode: mode, roots: roots}

	// The one key resolver this run has. Every seam that needs a server's API key — the startup
	// Config below, the bind, a `/server` switch, the Sub-agent server's beat — resolves through
	// THIS value, because the cache is the point: a second resolver would ask the keychain a second
	// time, and a `/server` switch back and forth would prompt the human twice for a key they
	// already gave. It reaches nothing on its own (an entry with a literal key, or none at all,
	// never leaves the process), so it belongs here with the facilities that cannot fail.
	w.keys = config.NewKeyResolver()

	// Discover the user's skills from the layered source dirs: the global library
	// (~/.apogee/skills), the project's .apogee/skills, and — when use-project-skills is on —
	// the project's bare skills/. The Provider holds the current catalog and can Reload it from
	// these same dirs on demand: the merged "/" menu refreshes it each time it opens, so a skill
	// added or edited after launch is picked up without restarting. The initial load error is
	// soft (a missing dir is skipped, a malformed skill is skipped), so the catalog is always
	// usable. The SAME *skills.Provider feeds both the loop (Config.Skills resolves attached IDs
	// into the turn) and the TUI's merged "/" menu (Options.Skills lists/labels them), so a
	// refreshed skill both shows in the menu AND resolves when attached.
	w.skillProvider = skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: opts.UseProjectSkills,
	})

	// The Bridge late-binds the event sink and approval gate to the Bubble Tea program
	// the launcher starts. Its Sink/Approver are installed in Config before construction
	// (apogee.New requires Events; Ask-Before needs the Approver), then bound once the
	// program exists (phase-2 detail plan §3 C2/C3).
	w.bridge = tui.NewBridge()

	// The presentation ladder's host-side mechanisms (ADR 0019), resolved from the `present:`
	// block and THIS session's environment and installed on the Bridge. Installing them is also
	// what makes bridge.Presenter() non-nil, and with it registers present_document — so the tool
	// exists exactly where a presentation can be carried out, which in the TUI is always (rung 0,
	// the transcript line, needs no mechanism at all).
	// It is a HOLDER rather than a value because the four `present.` keys are editable in the
	// `/settings` pane and apply to the running session (ADR 0037): the holder rebuilds the ladder
	// from the new block and re-installs it on the presenter the engine already captured.
	w.presentation = newLivePresentation(
		opts.Present, roots.workspace, runtime.GOOS, os.Getenv, w.bridge.SetPresentation)

	// The host's real Confiner backend, held on the wiring so its Capabilities() can be read by the
	// degradation notice below and by the /confine surface the renderer is handed — the backend
	// probes once at construction, so this is the same value the engine's dispatch disposition will
	// consult.
	w.confiner = platform.NewConfiner()

	return w
}

// resolveConfig builds the base [apogee.Config] this session is constructed from: the system prompt
// selected for the model as configured, and every value the engine reads that the flags, the file
// and the four facilities above have already settled. It is the first step that can fail, and it
// fails the run rather than degrading — a prompt that cannot be read is structural configuration.
//
// The tool registry and the Mechanism list are deliberately NOT here: both are folded onto the same
// Config by the live-session assembly below, which is where the MCP connections they depend on are.
func (w *rootWiring) resolveConfig() error {
	// The system prompt this session STARTS with (ADR 0023), selected for the model as configured
	// — which on a cold start is no model at all, so this selects the global template and the
	// per-model entry lands seconds later, on the first beat's rebind (rebindSpecFor re-runs
	// exactly this call with the observed model). A selected file that cannot be read, or a
	// template carrying an unknown placeholder, fails startup naming the config key — the prompt is
	// structural configuration, not something to degrade quietly around.
	sysPrompt, err := config.ResolveSystemPrompt(w.opts.SystemPrompt, w.opts.Model, w.roots.config, os.ReadFile)
	if err != nil {
		return err
	}

	// The shape this session STARTS reading responses in (ADR 0044), matched on the model as
	// configured — the system prompt's own story: a cold start names no model, nothing matches, and
	// the first beat's rebind re-runs exactly this resolution against the model the server reports.
	// A built-in match says so on stderr, pre-alt-screen like every other launch notice.
	profile, notice := resolveModelProfile(w.opts.Model, w.opts.ModelProfiles)
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

	// The upstream bearer token this session starts with, resolved from the startup entry's own KEY
	// SOURCE — its literal `api-key:`, the output of its `api-key-cmd:`, or the variable its
	// `api-key-env:` names — through the run's one resolver, so the bind below and every later seam
	// share the single answer. This is the first use of that source, and the only one an ordinary
	// launch pays for.
	//
	// A source that refuses fails the RUN, right here, carrying the entry's name and what the
	// command said: a session pointed at a server it cannot authenticate against can do nothing at
	// all, and sending no header instead would put the user's prompts on that server as anonymous
	// requests they would learn about from a 401 (design call 4). A pre-bound start resolves the
	// zero entry, which names no source and answers "" without running anything.
	apiKey, err := w.keys.Resolve(startupEntry(w.opts))
	if err != nil {
		return err
	}

	w.cfg = apogee.Config{
		Endpoint: w.opts.Endpoint,
		Model:    w.opts.Model,
		// The upstream bearer token resolved above, from the startup `servers:` entry's own key
		// source, which APOGEE_API_KEY overlays. Empty — the keyless local default — sends no
		// Authorization header at all.
		APIKey:       apiKey,
		Mode:         w.mode,
		Bypass:       w.opts.Bypass,
		Events:       w.bridge.Sink(),
		Approver:     w.bridge.Approver(),
		Asker:        w.bridge.Asker(),
		Presenter:    w.bridge.Presenter(),
		ConfigDir:    w.roots.config,
		LibraryDir:   w.roots.library,
		SessionsDir:  w.roots.sessions,
		WorkspaceDir: w.roots.workspace,
		// The host's real Confiner backend for this OS (landlock on Linux, seatbelt on macOS,
		// denyConfiner elsewhere — confinement-execution-contract §2.6). It is no longer
		// denyConfiner, so --mode auto WORKS where fs-confinement exists and gates the
		// subprocess surface where it does not (rather than refusing Auto).
		Confiner:           w.confiner,
		ConfineToWorkspace: w.opts.ConfineToWorkspace,
		WebSearchEndpoint:  w.opts.WebSearchEndpoint,
		// The `tools.disabled:` roster switch: the built-in tools this config takes off the menu.
		// Empty ⇒ the whole roster, exactly the set built before the key existed. It is carried on
		// Config rather than passed to the assembly alone so every Driver — this session, a headless
		// run, an embedder — prunes the same roster from the same value.
		DisabledTools: w.opts.ToolsDisabled,
		// The `url-safety:` host layer: the hosts the network tools may reach and the hosts they
		// may not. Empty ⇒ every host, exactly the reach before this key existed — and never less
		// safe either way, since the guard's SSRF floor is not reachable from configuration. It
		// rides Config for DisabledTools' reason: every Driver must fence the same hosts from the
		// same value.
		URLAllowHosts: w.opts.URLAllowHosts,
		URLDenyHosts:  w.opts.URLDenyHosts,
		// `ui.inspector:` — whether this session captures its own wire traffic for /inspect. It is
		// read ONCE, here, because that is where the engine installs the observer: a mid-session
		// edit of the key changes the file and the next start, never the running engine.
		Inspector: w.opts.UI.Inspector,
		// Every variable this configuration reads an API key out of (`api-key-env:`, ADR 0047),
		// which the execution tools drop from the environment they hand a subprocess. It is the
		// union across ALL configured entries rather than the bound one's: `/server` switches
		// mid-session, and a scrub that followed the binding would leave the other entries' keys
		// readable in every `terminal` / `python_exec` / `run_tests` child until it happened. Empty
		// ⇒ apogee's own APOGEE_API_KEY alone, exactly the scrub before this key existed.
		SecretEnvVars: config.APIKeyEnvNames(w.opts),
		// The Model profile (CONTEXT: Model profile) — tool-call format + thinking channel —
		// resolved above for THIS model out of the `model-profiles:` map and the shipped shape
		// table. A model neither tier knows gets the zero profile: native tool calls with no inline
		// thinking, exactly as an unprofiled model has always behaved.
		Profile: profile,
		// The configured system-prompt TEMPLATE (ADR 0023), which the loop renders fresh per
		// request and seeds as the first system message. Empty ⇒ no prompt: the request opens with
		// the user's own message, exactly as it did before this key existed.
		SystemPrompt: sysPrompt,
		// The workspace context files (`context-files:`, file-only): the names the engine looks for
		// in the workspace root at every session boundary, whose content rides the same first system
		// message as the prompt above — verbatim, never as a template. Nil ⇒ the feature is off, and
		// the request is exactly what it was before the key existed.
		ContextFiles: w.opts.ContextFiles,
		Skills:       w.skillProvider,
		// The skill source dirs, mounted as read-only roots for the model's read tools: an
		// attached skill names its folder, and this is what makes that address readable
		// (read_file, list_dir, grep, find_files — nothing else; the dirs stay unwritable).
		// It is the PROVIDER's method value, so the mount is live in both senses — it follows a
		// mid-session `use-project-skills` flip through SetSources, and it is re-read per tool
		// call rather than frozen here.
		// Sub-agents need no wiring of their own: a child's registry is a Subset of the parent's
		// tool INSTANCES (domain.ToolRegistry.Subset), so the same read tools — and with them
		// this same func — ride along at every depth.
		ExtraReadRoots: w.skillProvider.SourceDirs,
		// The `context-window:` PIN (0 when unpinned — nothing probes at startup any more). It is
		// the budget /compact and the automatic Compaction trigger bound their summary request
		// against so compaction survives high fill (the summary call would otherwise overflow near
		// n_ctx); the same value drives the TUI's footer/gauge below. Unpinned it stays 0 until the
		// first heartbeat rebind binds the observed window. CompactionEnabled carries the
		// `auto-compact` key (default on) — the budget-driven automatic trigger (item 9); the
		// on-demand /compact runs regardless of it.
		// MaxOutputTokens is the startup entry's own `max-output-tokens:` pin (0 ⇒ unpinned, and the
		// engine derives the reply cap from the room its Budget reserves — ADR 0046). It is seeded
		// here as well as at the bind, because this Config is also what a scheduled Firing copies:
		// a Firing running while nobody watches is exactly the case a runaway reply must not.
		// ResponseReserveFraction is the top-level `response-reserve:` share (0 ⇒ unset, and the
		// Budget holds its own built-in fifth back). It rides here for the reply cap's reason: this
		// Config is what a scheduled Firing copies, so the window is divided the same way whether a
		// human is watching or not.
		Context: apogee.ContextConfig{
			MaxContextTokens:        w.opts.ContextWindow,
			ResponseReserveFraction: w.opts.ResponseReserve,
			MaxOutputTokens:         w.opts.StartupMaxOutputTokens,
			CompactionEnabled:       w.opts.AutoCompact,
		},
	}
	return nil
}

// announceConfinement says what this host's confinement posture actually is, on stderr, while the
// alternate screen has not opened yet and a raw write is still safe — and pre-warms the one cost
// that posture implies. Both branches are per-session and mutually exclusive: Auto unconfined by
// choice, or Auto asking for a confinement this host cannot enforce.
func (w *rootWiring) announceConfinement() {
	// A per-session startup warning whenever Auto runs unconfined (ADR 0012): confine=false
	// is safe only inside a VM, and it is the only blanket loosen in the system.
	if w.mode == domain.ModeAuto && !w.opts.ConfineToWorkspace {
		fmt.Fprintln(os.Stderr, unconfinedAutoWarning)
	}

	// The mirror branch: Auto WITH confinement asked for, on a host whose backend cannot
	// enforce it. The ladder gates every terminal command instead — correct, but silent until
	// now, which is what made Auto look broken (ISSUES.md, 2026-07-21). Say it once, name the
	// backend, and point at /confine.
	if notice := probe.DegradedNotice(probe.BackendName(w.confiner), w.confiner.Capabilities(), w.mode, w.opts.ConfineToWorkspace); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

	// The unknown-window honesty line used to print here, before the alt-screen. It has moved into
	// the TUI's rebind fold (ADR 0024): at this point in startup NOTHING has asked the server yet,
	// so a launch-time notice would fire on every cold start and be wrong ten seconds later. The
	// first beat that binds a window without one is where the sentence is actually true.

	// Eager pre-warm of the confinement label walk (ADR 0020 §2, the plan's approach A). On the
	// Windows token backend the box is a mandatory Low label on the workspace tree, and labelling a
	// large .git or node_modules costs ~1 ms/object; a FIRST confined command that silently blocks
	// for seconds mid-session is the click-through-frustration trap Auto was built to avoid. Under
	// Auto+confine a confined command is effectively certain, so the walk is hoisted to startup —
	// pre-alt-screen, behind WindowsLabelProgressNotice — where a raw stderr write is safe and the
	// first in-session Confine then hits the memo and no-ops. This moves only the TIMING of the
	// already-ratified label pass, never WHAT is labelled (Close still reverts at shutdown),
	// consistent with the owner's "keep semantics". It is a genuine no-op on every other host:
	// PrewarmLabelWalk is empty off Windows, and the Windows backend refuses when FSWrite is false —
	// the same host probe.DegradedNotice above speaks for.
	if shouldPrewarmLabelWalk(w.mode, w.opts.ConfineToWorkspace, w.confiner.Capabilities().FSWrite) {
		platform.PrewarmLabelWalk(w.confiner, w.roots.workspace, os.Stderr)
	}
}
