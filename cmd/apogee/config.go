package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/tui"
	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// Config precedence (phase-2 detail plan §4 P2.5: flag > env > file > default)
// ----------------------------------------------------------------------------

// The settable upstream/autonomy values resolve from four sources, highest priority
// first: an explicitly-set command-line flag, then an APOGEE_* environment variable,
// then the config file (<apogee-home>/config.yaml), then the built-in default. The
// resolution is split into a pure core (resolveSettings over optional layers) and a
// thin orchestrator (applyConfig) that builds the layers from the live flag set,
// environment, and filesystem — so the precedence rule is table-testable without cobra,
// an environment, or a real file (the P2.5 acceptance).

// settings is the resolved configuration after precedence is applied: the values the
// composition root feeds into the apogee.Config and the TUI Options.
type settings struct {
	endpoint  string
	model     string
	mode      string
	hostAlias string
	bypass    bool

	// apiKey is the upstream bearer token: the value sent as `Authorization: Bearer <key>` on
	// every request to the LLM server (a keyed llama.cpp, LM Studio, a remote vLLM, any keyed
	// OpenAI-compatible proxy). It resolves env > file — `APOGEE_API_KEY` beats `api-key:` in
	// config.yaml — and has NO flag on purpose: a secret passed on the command line lands in
	// shell history and in `ps` output on every OS. Empty (the keyless local-server default)
	// ⇒ no Authorization header at all.
	apiKey string

	// servers is the resolved `servers:` list: the named upstream endpoints a running session can
	// be switched to, in file order. File-only (no flag, no env — like mcpServers): naming another
	// machine's endpoint and its key is a config act, not an invocation one. Absent/empty ⇒ no
	// alternatives are configured, which is the default; the endpoint this session started on
	// stands on its own either way, so this key only ADDS the servers it can move to.
	servers []serverEntry

	// confineToWorkspace is GLOBAL-CONFIG-ONLY (ADR 0012): it is resolved from the config
	// file alone, never from a flag or env, so a hostile repo invoking apogee cannot loosen
	// Auto's blast radius. Default true. (There is no project-level config file today; the
	// file-only resolution is what keeps it un-loosenable by the invocation environment.)
	// It is the EFFECTIVE value: an explicit `confine-to-workspace: false` OR a Host
	// acknowledgement matching this machine resolves it to false (resolveConfineToWorkspace).
	confineToWorkspace bool

	// unconfinedHosts is the Host acknowledgement list as configured (ADR 0012, amendment
	// 2026-07-21) — the machines the user has recorded as disposable. File-only on the same
	// reasoning as confine-to-workspace: a hostile repo must not be able to name your host.
	// It is carried past resolution (which collapses it into confineToWorkspace above) so the
	// session can report the list back and extend it.
	unconfinedHosts []unconfinedHost

	// webSearchEndpoint is the config'd search backend for the web_search tool (P3.11),
	// file-only (empty ⇒ the built-in DuckDuckGo default; "off" disables the tool).
	webSearchEndpoint string

	// useProjectSkills gates whether the workspace's bare skills/ folder is discovered (in
	// addition to the global library and the project's .apogee/skills, which are always loaded).
	// File-only, default TRUE — a project's skills/ is trusted by default, like the @file
	// references the same workspace already feeds the model.
	useProjectSkills bool

	// autoCompact gates the automatic, budget-driven generative Compaction trigger (item 9). File-only,
	// default TRUE — Compaction is structural and load-bearing (it stays on under Bypass, D5/D6), so it
	// runs unless a config explicitly opts out with `auto-compact: false`. The on-demand /compact command
	// is unaffected by it (that always folds on request).
	autoCompact bool

	// contextWindow PINS the model context window in tokens (item 3 / S3). File-only (no flag/env,
	// like autoCompact) and default 0 ⇒ unpinned, so the window is whatever the ten-second
	// heartbeat observes, live, and follows a model switch with it; a positive value is never
	// overridden by a beat (ADR 0024 — the escape hatch for a server that does not advertise a
	// window, or advertises one that is wrong for how it is run). Nothing pre-fills it any more, so
	// > 0 means "the user pinned it" and nothing else. It feeds ContextConfig.MaxContextTokens,
	// which the Budget and automatic Compaction bind against.
	contextWindow int

	// mcpServers is the set of external MCP servers to connect on startup (P3.15), file-only
	// and default-empty (no servers ⇒ the MCP feature is dormant). Their tools surface into the
	// registry as classMCP ExternalEffectTools the disposition gates in Auto.
	mcpServers []mcp.ServerConfig

	// profile is the model profile (CONTEXT: Model profile) — the model's tool-call format and
	// inline thinking-channel style — file-only (a per-model concern, like mcpServers, with no
	// flag/env). A zero ModelProfile is native tool calls with no inline thinking (today's
	// behaviour), so an absent profile block leaves it unchanged.
	profile apogee.ModelProfile

	// mechanisms enables catalogued small-model Mechanisms by canonical ID (Phase 4), file-only
	// (a per-model tuning concern, like mcpServers, with no flag/env) and default-empty. All
	// Mechanisms ship OFF (D1 — default-off until bench-proven); a `true` entry turns one on. The
	// composition root validates every key against the catalogue and hands the enabled IDs to
	// apogee.Config.EnableMechanisms, which the engine builds catalogue rows from (ADR 0015 §1); an
	// unknown ID is a loud startup error. Bypass still wins (an enabled non-off-ramp Mechanism is
	// not dispatched under bypass — ADR 0006 / item 2's gate).
	mechanisms map[string]bool

	// validatedSetsEnable gates the Validated-set runtime surface (ADR 0016 §5's off-switch),
	// file-only and default TRUE — a matching per-model set applies (or is offered) unless the
	// config explicitly opts out with `validated-sets: enable: false`.
	validatedSetsEnable bool

	// validatedSetsAlias is the explicit carry-over map (ADR 0016 §3): runtime fingerprint
	// label → Validated-set entry key. File-only, default-empty. An identity mapping is the
	// low-confidence confirm ("my model is what the label says"); a differing mapping is the
	// explicit transfer to a sibling quant/family member.
	validatedSetsAlias map[string]string

	// present is the resolved `present:` block (ADR 0019): which mechanisms of the presentation
	// ladder this host offers a finished document. File-only (no flag/env, like the newer keys
	// above). Its defaults are auto-open ON — the headline want is that a run on the user's own
	// desktop simply opens the deliverable — with no command override, an ephemeral doc-server
	// port, and a detected advertise host.
	present presentSettings

	// systemPrompt is the resolved system-prompt block (ADR 0023): the global prompt (inline or
	// a file) and the per-model overrides. File-only (no flag/env, like present above), and its
	// zero value is no prompt at all — the promptless request apogee sent before ADR 0023. The
	// composition root collapses it into the ONE template apogee.Config carries, with
	// resolveSystemPrompt, AFTER model resolution: which entry applies is not known until then.
	systemPrompt systemPromptSettings

	// contextFiles is the resolved `context-files:` block: the workspace-root files whose content
	// joins the standing system content (the AGENTS.md / CLAUDE.md behaviour). File-only (no
	// flag/env, like the system-prompt keys it sits beside) and its default is ON with the one
	// name AGENTS.md, so a repo carrying that file works with no configuration at all. The
	// composition root collapses it into the name list apogee.Config.ContextFiles carries.
	contextFiles contextFilesSettings

	// ui is the resolved `ui:` block: how the terminal UI presents itself — today the status-line
	// spinner's animation and its colour loop. File-only (no flag/env, like the blocks above), and
	// its defaults are the renderer's own (defaultUISettings): the default style with the colour
	// loop on. The composition root hands both values to the TUI as Options.
	ui uiSettings

	// cursorShape is the configured shape of the prompt's caret, as the user spelled it (block |
	// underline | bar). File-only like the ui block, and empty ⇒ the renderer's default (block).
	// It is carried as the raw NAME — applyConfig validates it through tui.ParseCursorShape, and
	// the composition root parses it once more into the tea.CursorShape the TUI Options take, so
	// cmd/apogee never restates the vocabulary internal/tui owns.
	cursorShape string
}

// presentSettings is the resolved `present:` block (ADR 0019), in the form the composition root
// turns into the host-side mechanisms themselves (wire.go's presentationRungs).
//
// It is one struct rather than four fields on settings because the four describe ONE subsystem
// and travel together — from the on-disk block, through resolution, to the wire that builds the
// ladder out of them. Nothing in here can switch presentation off: rung 0, the transcript line
// carrying the path, needs no configuration and is never skipped. These keys only change WHICH
// mechanism above it carries the document.
type presentSettings struct {
	// autoOpen gates rungs 1 and 3 — handing the document to a desktop application, on a LOCAL
	// session. Default true. False wires no opener at all, which covers the command override too:
	// present.command says which application shows a document, not whether one is opened.
	autoOpen bool
	// command is the present.command template (e.g. `zed {path}`), which replaces the built-in OS
	// opener on every OS. Empty ⇒ the per-OS default (open / start / xdg-open).
	command string
	// port is the TCP port the doc server (rung 2) binds. Default 0 ⇒ an ephemeral port, which
	// costs nothing: the URL is printed fresh per presentation, so a stable port buys the user
	// nothing to remember.
	port int
	// host overrides the address the served URL advertises. Empty ⇒ present.AdvertiseHost's own
	// chain ($SSH_CONNECTION's server IP, then an outbound-dial probe, then loopback). It is the
	// fallback for topologies SSH cannot describe rather than a true override — SSH_CONNECTION,
	// when present, is a live and verified-routable address and still wins (see AdvertiseHost).
	host string
}

// validate rejects a present block that cannot be honoured. The port is the only checkable key:
// a command template is a statement about the user's own machine (an unresolvable program is a
// fail-visible opener error at presentation time, ADR 0019 §4 — not a startup error), and the
// host is a display string this process cannot verify. An out-of-range port, by contrast, would
// fail deep inside the first presentation, where all the user sees is a degraded rung — so it is
// caught here, where the message can name the key that is wrong.
func (p presentSettings) validate() error {
	if p.port < 0 || p.port > 65535 {
		return fmt.Errorf("apogee: invalid present.port %d: want a TCP port in 0-65535 "+
			"(0 — the default — takes an ephemeral port)", p.port)
	}
	return nil
}

// promptSource is one configured system prompt (ADR 0023) as the user wrote it: the template
// inline (text) or the path of a file holding it (file). The two are mutually exclusive
// spellings of one prompt — a level that sets both is a startup error (validate below) — and a
// source with neither set configures no prompt.
type promptSource struct {
	text string
	file string
}

// systemPromptSettings is the resolved system-prompt block (ADR 0023): the global prompt plus
// the per-model overrides, keyed by the RESOLVED model name (the label the Validated-set surface
// keys on too). It is one struct rather than three fields on settings for the same reason
// presentSettings is: the keys describe ONE subsystem and travel together, from the on-disk block
// through resolution to the composition root, where resolveSystemPrompt collapses them into the
// single template apogee.Config carries.
//
// Selection is WHOLE-ENTRY replacement: an entry whose key is this session's model replaces the
// global prompt entirely, so a per-model `system-prompt-file` does not inherit a global
// `system-prompt-text`. An entry naming any other model is inert — the `unconfined-hosts`
// posture: it describes a machine/model this run is not, so it is never selected and its file is
// never read (it may only exist elsewhere).
type systemPromptSettings struct {
	global promptSource
	models map[string]promptSource
}

// validate rejects a system-prompt block that is structurally impossible, at EVERY level —
// including entries this host will never select. Setting both spellings at one level is a
// contradiction (which prompt was meant?), and an entry setting neither is far more likely a YAML
// indentation slip than a deliberate "this model gets nothing"; both are machine-independent
// defects in the file itself, so they are caught at config time where the message can name the key.
//
// What is deliberately NOT checked here: whether a file reads, and whether a template's
// placeholders are known. Those are properties of the SELECTED source only (resolveSystemPrompt),
// because a non-matching per-model entry may name a file that exists on another machine — refusing
// to start over it would make one global config unusable everywhere else.
//
// The entries are walked in sorted order so the entry a message names is the same one on every
// run, rather than whichever the map happened to yield first.
func (sp systemPromptSettings) validate() error {
	if sp.global.text != "" && sp.global.file != "" {
		return errors.New("apogee: system-prompt-text and system-prompt-file are both set: " +
			"they are two spellings of one prompt — keep the inline text or the file, not both")
	}
	for _, model := range slices.Sorted(maps.Keys(sp.models)) {
		src := sp.models[model]
		switch {
		case src.text != "" && src.file != "":
			return fmt.Errorf("apogee: system-prompt-models[%q] sets both system-prompt-text and "+
				"system-prompt-file: keep the inline text or the file, not both", model)
		case src.text == "" && src.file == "":
			return fmt.Errorf("apogee: system-prompt-models[%q] sets neither system-prompt-text nor "+
				"system-prompt-file: give the entry a prompt, or remove it", model)
		}
	}
	return nil
}

// defaultContextFileName is the ONE name the `context-files:` block looks for when the config
// names none: the cross-tool agent guide a repo already keeps for other agents. It is the whole
// reason the feature needs no configuration in the common case.
const defaultContextFileName = "AGENTS.md"

// contextFilesSettings is the resolved `context-files:` block: whether workspace context files are
// folded into the standing system content at all, and which names are looked for in the workspace
// root, in inclusion order. It is one struct rather than two fields on settings for the same
// reason presentSettings is: the keys describe ONE subsystem and travel together, from the on-disk
// block through resolution to the composition root, where resolved() collapses them into the
// single name list apogee.Config carries.
//
// The list is an INCLUSION set, not a priority chain — every listed name that EXISTS is included —
// and whether any of them exists is deliberately not a config-time question: discovery is the
// feature, and the default name will not exist in every repo.
type contextFilesSettings struct {
	// enable is the block's off-switch, default TRUE (the validated-sets `enable` posture): a repo
	// carrying an AGENTS.md is picked up with nothing configured, and `enable: false` opts out.
	enable bool
	// names are the workspace-relative names to look for, in inclusion order. Default the one
	// AGENTS.md; an explicitly EMPTY list means "no names", which is the other way to switch the
	// feature off.
	names []string
}

// defaultContextFilesSettings is the resolved block with nothing configured: on, looking for the
// one default name.
func defaultContextFilesSettings() contextFilesSettings {
	return contextFilesSettings{enable: true, names: []string{defaultContextFileName}}
}

// validate rejects a name the workspace lookup must never be handed, at config time where the
// message can name the key that is wrong: an empty entry, a name that is not workspace-relative,
// one that climbs out of the workspace with "..", and one listed twice (which would fold the same
// file into the prompt twice — always a mistake, never an intent).
//
// It runs whatever `enable` says: a bad name is a defect in the FILE, and a block that is off
// today gets switched on months later — by which time the typo has lost its context. This is the
// systemPromptSettings.validate posture (every level is checked, including entries this run will
// never select).
//
// What is deliberately NOT checked: whether any named file EXISTS. Discovery is the feature — a
// missing name is skipped silently by the engine's loader, because one global config is expected
// to travel across repos that carry different files (or none).
//
// Every check is MACHINE-INDEPENDENT (ADR 0023 §3's posture): the same config file is refused on
// every OS, so a Windows-shaped escape (`..\x`, a `C:` drive prefix, a leading `\`) is named on
// Linux too rather than lying dormant until the file travels. The rule itself is domain's
// (workspacename.go) — the same one the engine's construction-time gate applies, so a name refused
// here is refused there and the two cannot drift apart.
func (cf contextFilesSettings) validate() error {
	seen := make(map[string]struct{}, len(cf.names))
	for _, name := range cf.names {
		if strings.TrimSpace(name) == "" {
			return errors.New("apogee: context-files.names: an entry is empty — name a file to look " +
				"for in the workspace root, or remove the entry")
		}
		if !domain.IsWorkspaceRelative(name) {
			return fmt.Errorf("apogee: context-files.names: %q is not workspace-relative: the names are "+
				"looked up in the workspace root, so give a plain name like %q", name, defaultContextFileName)
		}
		if domain.EscapesWorkspace(name) {
			return fmt.Errorf("apogee: context-files.names: %q climbs out of the workspace: context files "+
				"are read from the workspace only (a standing prompt from elsewhere is system-prompt-file)", name)
		}
		// The duplicate check compares NORMAL FORMS, so "AGENTS.md" and "./AGENTS.md" are one name.
		cleaned := domain.CleanWorkspaceName(name)
		if _, dup := seen[cleaned]; dup {
			return fmt.Errorf("apogee: context-files.names: %q is listed twice: each name is included "+
				"once, in list order", name)
		}
		seen[cleaned] = struct{}{}
	}
	return nil
}

// resolved collapses the block into the list apogee.Config.ContextFiles carries: nil — the feature
// off — when the switch is off or the list resolved empty, and otherwise the names in list order.
// The two spellings of "off" are deliberately one value downstream: the engine's contract is that
// an empty list IS the feature being off, so nothing below has to know which spelling was used.
func (cf contextFilesSettings) resolved() []string {
	if !cf.enable || len(cf.names) == 0 {
		return nil
	}
	return cf.names
}

// uiSettings is the resolved `ui:` block, in the form the composition root hands the renderer
// (wire.go's tui.Options). It is one struct rather than loose fields on settings for the same
// reason presentSettings is: the keys describe ONE subsystem and travel together, from the on-disk
// block through resolution to the wire.
//
// The two are deliberately INDEPENDENT keys. The colour loop is not a property of a style and is
// not folded into the style name: it applies to whichever style spinner names, so all three styles
// × colour on/off are valid combinations. Nothing here or downstream may key one off the other.
type uiSettings struct {
	// spinner names the status-line animation. It is carried as read (a name this build may not
	// know) until validate parses it, so an unknown style is a startup error naming the key rather
	// than a silent fall back to the default.
	spinner tui.SpinnerStyle
	// spinnerColor runs the slow colour loop over whichever style spinner names. Default true;
	// false leaves the glyph in the terminal's own text colour, which is the escape hatch for a
	// terminal whose colour depth turns the gradient into steps.
	spinnerColor bool
}

// defaultUISettings is the resolved `ui:` block with nothing configured: the renderer's own default
// style, with the colour loop on. The style is ASKED of internal/tui (ParseSpinnerStyle's documented
// "" ⇒ the default) rather than restated here, so the vocabulary and its default stay in the one
// package that owns them — the same reason validate does not list the valid names.
func defaultUISettings() uiSettings {
	// ParseSpinnerStyle errors only on a style it does not know; "" is the request for the default,
	// so this cannot fail.
	style, _ := tui.ParseSpinnerStyle("")
	return uiSettings{spinner: style, spinnerColor: true}
}

// validate rejects a ui block naming a spinner style this build has no animation for. Catching it
// here makes a typo a startup error that names the key; left to the renderer it would silently
// resolve to some other style, and the user would be left wondering why their setting did nothing.
// The valid set comes from internal/tui, which owns the vocabulary — this only adds the key the bad
// value was read from, which that package cannot know.
func (u uiSettings) validate() error {
	if _, err := tui.ParseSpinnerStyle(string(u.spinner)); err != nil {
		return fmt.Errorf("apogee: invalid ui.spinner: %w", err)
	}
	return nil
}

// layer is one precedence source. A nil pointer means the source does not set that
// field, so resolution falls through to the next-lower-priority source. A non-nil
// pointer (including a pointer to the zero value) is an explicit setting that wins over
// everything below it.
type layer struct {
	endpoint  *string
	model     *string
	mode      *string
	hostAlias *string
	bypass    *bool

	// apiKey is set by the FILE and ENV layers only; the flag layer never carries it, because
	// there is deliberately no --api-key (a secret must not reach shell history or a process
	// list). It still rides the generic resolution loop, so the loop's own order — file, then
	// env — is what makes `APOGEE_API_KEY` beat `api-key:`. A nil pointer means the source
	// configures no key, so resolution falls through to the empty default: no auth header.
	apiKey *string

	// servers is set only by the FILE layer (the `servers:` list is config'd, default-empty, with
	// no flag/env — like mcpServers). A nil slice means the source names no server, so resolution
	// falls through to the empty default.
	servers []serverEntry

	// confineToWorkspace is set only by the FILE layer (global-config-only, ADR 0012). The
	// env and flag layers leave it nil so the invocation environment cannot loosen it.
	confineToWorkspace *bool

	// unconfinedHosts is set only by the FILE layer (global-config-only on the same reasoning
	// as confineToWorkspace above — ADR 0012, amendment 2026-07-21). A nil slice means the
	// source acknowledges no host, which is the default: every host is confined.
	unconfinedHosts []unconfinedHost

	// webSearchEndpoint is set only by the FILE layer (P3.11 — web-search is config'd,
	// with no flag/env). Empty/absent ⇒ the built-in DuckDuckGo default; "off" disables.
	webSearchEndpoint *string

	// useProjectSkills is set only by the FILE layer (skills are config'd, default-on, with no
	// flag/env). A pointer so an explicit `use-project-skills: false` is distinguishable from
	// an absent key (which keeps the default true).
	useProjectSkills *bool

	// autoCompact is set only by the FILE layer (the automatic Compaction trigger is config'd,
	// default-on, with no flag/env). A pointer so an explicit `auto-compact: false` is
	// distinguishable from an absent key (which keeps the default true).
	autoCompact *bool

	// contextWindow is set only by the FILE layer (the window pin is config'd, no flag/env — like
	// autoCompact). A nil pointer means the source pins no window, so resolution leaves it 0 and
	// the heartbeat's live observation stands; only a positive `context-window:` projects to a
	// non-nil pointer.
	contextWindow *int

	// mcpServers is set only by the FILE layer (P3.15 — MCP servers are config'd, default-empty,
	// with no flag/env). A nil slice means the source does not configure servers (fall through).
	mcpServers []mcp.ServerConfig

	// profile is set only by the FILE layer (the model profile is config'd, default-zero, with no
	// flag/env). A nil pointer means the source does not configure a profile, so resolution falls
	// through to the zero/native default.
	profile *apogee.ModelProfile

	// mechanisms is set only by the FILE layer (Mechanisms are config'd, default-empty, with no
	// flag/env — like mcpServers). A nil map means the source does not enable any Mechanism (fall
	// through to the empty default).
	mechanisms map[string]bool

	// validatedSetsEnable / validatedSetsAlias are set only by the FILE layer (the Validated-set
	// surface is config'd, no flag/env — like mechanisms). A nil enable pointer keeps the default
	// true; a nil alias map means no carry-over is configured.
	validatedSetsEnable *bool
	validatedSetsAlias  map[string]string

	// present is set only by the FILE layer (the presentation ladder is config'd, no flag/env —
	// like mechanisms). A nil pointer means the source configures no `present:` block, so
	// resolution keeps the defaults (auto-open on, an ephemeral port, a detected host).
	present *presentSettings

	// systemPrompt is set only by the FILE layer (the system prompt is config'd, no flag/env —
	// like present above). A nil pointer means the source sets none of the three system-prompt
	// keys, so resolution keeps the zero value: no prompt, today's promptless request.
	systemPrompt *systemPromptSettings

	// contextFiles is set only by the FILE layer (the `context-files:` block is config'd, no
	// flag/env — like systemPrompt above). A nil pointer means the source carries no block, so
	// resolution keeps the defaults: on, looking for AGENTS.md in the workspace root.
	contextFiles *contextFilesSettings

	// ui is set only by the FILE layer (the UI's own presentation is config'd, no flag/env — like
	// present). A nil pointer means the source configures no `ui:` block, so resolution keeps the
	// defaults (the renderer's default spinner style, colour loop on).
	ui *uiSettings

	// cursorShape is set only by the FILE layer (the caret's shape is config'd, no flag/env — like
	// the ui block). A nil pointer means the source names no shape, so resolution leaves it empty
	// and the renderer's default (a steady block) stands.
	cursorShape *string
}

// resolveSettings overlays the layers in increasing priority — the default base, then
// the file, then the environment, then the flags — so a flag beats an environment
// variable beats the file beats the default. Only ask-before (the default mode) is a
// non-zero base; endpoint/model default empty and bypass defaults false.
//
// api-key is the mirror-image case: it rides the same loop, but no flag layer ever sets it
// (there is no --api-key — a secret on the command line lands in shell history and in `ps`
// output), so the loop's order alone resolves it env > file.
//
// confine-to-workspace is the exception: it defaults true and is resolved from the FILE
// layer ONLY (never env or flag), because it is global-config-only (ADR 0012) — a hostile
// repo's invocation environment must not be able to loosen Auto's blast radius. hostID is
// this machine's identity (platform.HostID(), injected so the ladder is testable off any
// host): it selects the Host acknowledgement, if any, that applies here.
//
// It returns the soft notices resolution produced — today only malformed `unconfined-hosts`
// entries, which are skipped rather than fatal (the ADR 0016 posture the validated-set
// surface established: a data defect degrades, it never blocks startup).
func resolveSettings(file, env, flag layer, hostID string) (settings, []string) {
	s := settings{mode: string(modeAskBefore), confineToWorkspace: true, useProjectSkills: true, autoCompact: true,
		validatedSetsEnable: true, present: presentSettings{autoOpen: true}, ui: defaultUISettings(),
		contextFiles: defaultContextFilesSettings()}
	// file-only (ADR 0012 + its 2026-07-21 amendment); env/flag never carry either, so the
	// invocation environment can neither flip the flag nor name a host.
	s.unconfinedHosts = file.unconfinedHosts
	confine, notices := resolveConfineToWorkspace(file.confineToWorkspace, file.unconfinedHosts, hostID)
	s.confineToWorkspace = confine
	if file.webSearchEndpoint != nil {
		s.webSearchEndpoint = *file.webSearchEndpoint
	}
	if file.useProjectSkills != nil {
		s.useProjectSkills = *file.useProjectSkills
	}
	if file.autoCompact != nil {
		s.autoCompact = *file.autoCompact
	}
	if file.contextWindow != nil {
		s.contextWindow = *file.contextWindow
	}
	s.servers = file.servers             // file-only; env/flag never name an upstream server
	s.mcpServers = file.mcpServers       // file-only (P3.15); env/flag never set MCP servers
	s.mechanisms = file.mechanisms       // file-only (Phase 4); env/flag never enable Mechanisms
	if file.validatedSetsEnable != nil { // file-only (ADR 0016); env/flag never touch the surface
		s.validatedSetsEnable = *file.validatedSetsEnable
	}
	s.validatedSetsAlias = file.validatedSetsAlias
	if file.profile != nil { // file-only; env/flag never carry a model profile
		s.profile = *file.profile
	}
	if file.present != nil { // file-only (ADR 0019); env/flag never carry the presentation block
		s.present = *file.present
	}
	if file.systemPrompt != nil { // file-only (ADR 0023); env/flag never carry a system prompt
		s.systemPrompt = *file.systemPrompt
	}
	if file.contextFiles != nil { // file-only, like the system prompt it stands beside
		s.contextFiles = *file.contextFiles
	}
	if file.ui != nil { // file-only; env/flag never carry the UI block
		s.ui = *file.ui
	}
	if file.cursorShape != nil { // file-only, like the UI block above
		s.cursorShape = *file.cursorShape
	}
	for _, l := range []layer{file, env, flag} {
		if l.endpoint != nil {
			s.endpoint = *l.endpoint
		}
		if l.model != nil {
			s.model = *l.model
		}
		if l.mode != nil {
			s.mode = *l.mode
		}
		if l.hostAlias != nil {
			s.hostAlias = *l.hostAlias
		}
		if l.bypass != nil {
			s.bypass = *l.bypass
		}
		if l.apiKey != nil {
			s.apiKey = *l.apiKey
		}
	}
	return s, notices
}

// resolveConfineToWorkspace is Auto's effective blast-radius decision (ADR 0012 as amended
// 2026-07-21), in the order the ADR fixes:
//
//  1. an explicit global `confine-to-workspace: false` → false (the blanket loosen, on every
//     host this config travels to — unchanged meaning);
//  2. else a Host acknowledgement whose id is this machine's → false (the same loosen, at the
//     grain the claim is actually true at);
//  3. else true, the secure default.
//
// An explicit `confine-to-workspace: true` does NOT veto a matching acknowledgement: the
// flag states the global default and the entry states a fact about THIS machine, so the
// more specific statement wins — which is also what makes the list usable beside the
// default-true flag at all.
//
// An entry with no id is a soft skip with a notice naming it: it acknowledges no machine, and
// a malformed line must not block startup (nor, by matching an empty hostID, loosen anything).
// An id that matches nothing is not an error either — the list is expected to accumulate
// machines, so most entries name some other host on any given run.
//
// Step 2 additionally requires that this machine HAVE an identity: on a host that can supply
// neither a hostname nor a machine identifier, platform.HostID() is the same value everywhere
// (platform.IsUnidentifiedHostID), so honouring a match there would let one saved
// acknowledgement loosen every such host — the interlock's whole purpose reversed. That match
// is refused with a notice instead. Step 1 is untouched: an explicit global false still means
// every host, identity or not.
func resolveConfineToWorkspace(explicit *bool, hosts []unconfinedHost, hostID string) (bool, []string) {
	var notices []string
	identified := !platform.IsUnidentifiedHostID(hostID)
	acknowledged := false
	for i, h := range hosts {
		id := strings.TrimSpace(h.ID)
		if id == "" {
			notices = append(notices, fmt.Sprintf(
				"apogee: skipping unconfined-hosts entry %d: it has no id, so it acknowledges no machine "+
					"(this host is %q)", i+1, hostID))
			continue
		}
		if id != hostID {
			continue
		}
		if !identified {
			notices = append(notices, fmt.Sprintf(
				"apogee: ignoring unconfined-hosts entry %d (%q): this machine reports neither a hostname "+
					"nor a machine id, so that id names every such machine rather than this one — auto stays "+
					"confined; use /confine off for this session", i+1, id))
			continue
		}
		acknowledged = true // keep scanning: every malformed entry still gets named
	}
	if explicit != nil && !*explicit {
		return false, notices
	}
	return !acknowledged, notices
}

// ----------------------------------------------------------------------------
// The config file (<apogee-home>/config.yaml)
// ----------------------------------------------------------------------------

// fileConfig is the on-disk config schema. It mirrors the settable flags so a user can
// fix their endpoint/model/autonomy once instead of passing them every invocation.
// Bypass is a pointer so an explicit `bypass: false` is distinguishable from an absent
// key (the former wins over a lower layer; the latter falls through).
type fileConfig struct {
	Endpoint  string `yaml:"endpoint"`
	Model     string `yaml:"model"`
	Mode      string `yaml:"mode"`
	HostAlias string `yaml:"host-alias"`
	Bypass    *bool  `yaml:"bypass"`
	// APIKey is the bearer token sent as `Authorization` on every upstream request — what a
	// keyed server wants (llama.cpp's `--api-key`, LM Studio, a remote vLLM, any keyed
	// OpenAI-compatible proxy). `APOGEE_API_KEY` overrides it; there is no flag on purpose (a
	// secret on the command line lands in shell history and in `ps` output). Absent/empty ⇒ no
	// Authorization header, which is the keyless local-server default. This file is plain
	// text: on a shared machine prefer the environment variable, or restrict its permissions.
	APIKey string `yaml:"api-key"`
	// Servers names the upstream endpoints besides the one above — the alternatives a running
	// session can be moved to. File-only (no flag/env), like mcp-servers: the list describes
	// machines, not this invocation. Absent/empty ⇒ none is configured, which changes nothing
	// about the session's own upstream (see serverEntry for what an entry carries).
	Servers []serverEntry `yaml:"servers"`
	// ConfineToWorkspace is global-config-only (ADR 0012): a pointer so an explicit
	// `confine-to-workspace: false` is distinguishable from an absent key (which keeps the
	// secure default true). It has no flag or env — editing the global config IS the
	// deliberate acknowledgement required to run Auto unconfined.
	ConfineToWorkspace *bool `yaml:"confine-to-workspace"`
	// UnconfinedHosts is the Host acknowledgement list (ADR 0012, amendment 2026-07-21): the
	// machines the user has recorded as disposable, so Auto may run unconfined THERE without
	// the claim following this file onto every other host. Global-config-only like the flag
	// above (a hostile repo must not be able to name your host), and absent/empty ⇒ no host is
	// acknowledged, which is the default.
	UnconfinedHosts []unconfinedHost `yaml:"unconfined-hosts"`
	// WebSearch is the search endpoint the web_search tool sends a query to (P3.11).
	// Absent ⇒ the built-in DuckDuckGo default; `off` disables the tool. Empty string is
	// treated as absent.
	WebSearch string `yaml:"web-search-endpoint"`
	// UseProjectSkills gates discovery of the workspace's bare skills/ folder. A pointer so an
	// explicit `use-project-skills: false` is distinguishable from an absent key (default true).
	UseProjectSkills *bool `yaml:"use-project-skills"`
	// AutoCompact gates the automatic, budget-driven generative Compaction trigger (item 9). A pointer
	// so an explicit `auto-compact: false` is distinguishable from an absent key (default true).
	// Compaction is structural (it stays on under Bypass), so this is the only way to turn it off.
	AutoCompact *bool `yaml:"auto-compact"`
	// ContextWindow PINS the model context window in tokens (item 3 / S3). File-only (no flag/env),
	// like auto-compact. Absent or ≤ 0 ⇒ unpinned, so the window follows what the ten-second
	// heartbeat observes, live, across a model switch; a positive value is never overridden by a
	// beat (ADR 0024) — the escape hatch for a server that does not advertise a window, or
	// advertises one that is wrong for how it is run. It feeds ContextConfig.MaxContextTokens,
	// which the Budget and automatic Compaction bind against.
	ContextWindow int `yaml:"context-window"`
	// MCPServers configures external MCP servers to connect on startup (P3.15). Absent/empty ⇒
	// the MCP feature is dormant (no servers, no error). Each server's tools surface into the
	// registry as classMCP ExternalEffectTools the disposition gates in Auto.
	MCPServers []mcpServerConfig `yaml:"mcp-servers"`
	// ModelProfile describes how the configured model speaks the wire (CONTEXT: Model profile) —
	// its tool-call format and inline thinking-channel style. A per-model concern (like
	// mcp-servers): file-only, no flag/env. Absent ⇒ the zero profile (native tool calls, no
	// inline thinking — today's behaviour). A pointer so an absent block falls through to that
	// default rather than being an explicit zero setting.
	ModelProfile *modelProfileConfig `yaml:"model-profile"`
	// Mechanisms enables catalogued small-model Mechanisms by canonical ID (Phase 4): a map of
	// canonical mechanism ID → enabled. File-only (no flag/env), like mcp-servers. Absent/empty ⇒
	// no Mechanism is enabled — ALL default OFF (D1, default-off until bench-proven), so an entry
	// is required to turn one on. An unknown ID is a loud startup error listing the known
	// catalogue; Bypass still disables enabled non-off-ramp Mechanisms (ADR 0006).
	Mechanisms map[string]bool `yaml:"mechanisms"`
	// ValidatedSets configures the Validated-set runtime surface (ADR 0016 and its 2026-07-19
	// realisation). File-only (no flag/env), like mechanisms. Absent ⇒ the surface is ON with no
	// aliases: a per-model set matching the resolved fingerprint auto-applies at ≥ medium
	// confidence and is offered at low. A pointer so an absent block falls through to that
	// default rather than being an explicit zero setting.
	ValidatedSets *validatedSetsConfig `yaml:"validated-sets"`
	// Present configures how a finished document is shown to the user (ADR 0019): the
	// presentation ladder's auto-open switch, the application that stands in for the OS opener,
	// and the doc server's port and advertised host. File-only (no flag/env), like the blocks
	// above. Absent ⇒ auto-open on, no command override, an ephemeral port and a detected host. A
	// pointer so an absent block falls through to those defaults rather than being an explicit
	// zero setting — which would read as `auto-open: false` and silently disable the rung the
	// whole feature exists for.
	Present *presentConfig `yaml:"present"`
	// SystemPromptText, SystemPromptFile and SystemPromptModels configure the system prompt
	// (ADR 0023) — the template apogee renders fresh per request and sends as the first system
	// message. File-only (no flag/env), like the blocks above; absent everywhere ⇒ no system
	// prompt at all (the promptless request apogee sent before ADR 0023).
	//
	// The first two are mutually exclusive spellings of ONE prompt: inline text, or a file to
	// read it from (`~` expands, and a relative path resolves against the apogee home this file
	// lives in). SystemPromptModels keys the RESOLVED model name to an entry using those same two
	// spellings; a matching entry REPLACES the global prompt whole, and an entry naming another
	// model is inert. They are three top-level keys rather than one `system-prompt:` block
	// because the common case — one inline prompt — is then one line, not three.
	SystemPromptText   string                             `yaml:"system-prompt-text"`
	SystemPromptFile   string                             `yaml:"system-prompt-file"`
	SystemPromptModels map[string]systemPromptEntryConfig `yaml:"system-prompt-models"`
	// ContextFiles configures the workspace context files folded into the standing system content
	// at every session start — the AGENTS.md / CLAUDE.md behaviour. File-only (no flag/env), like
	// the system-prompt keys above, and it is a BLOCK rather than loose keys because the two keys
	// (the switch and the list) are meaningless apart. Absent ⇒ on, looking for AGENTS.md in the
	// workspace root. A pointer so an absent block falls through to that default rather than being
	// an explicit zero setting, which would read as `enable: false` and silently disable it.
	ContextFiles *contextFilesConfig `yaml:"context-files"`
	// CursorShape names the shape the prompt's caret is drawn with — block (the default) |
	// underline | bar. apogee draws the REAL terminal cursor, always steady, so this is the one
	// axis there is: nothing blinks, and the shape the terminal itself is configured with cannot be
	// inherited while a full-screen program runs (tui.ParseCursorShape says why). File-only (no
	// flag/env), like the blocks above. It stays a raw string here — applyConfig parses it once, so
	// an unknown name reaches startup as an error rather than being quietly dropped at the yaml
	// seam (the `ui.spinner` posture).
	CursorShape string `yaml:"cursor-shape"`
	// UI configures how the terminal UI presents itself — today the status-line spinner's animation
	// and its colour loop. File-only (no flag/env), like the blocks above. Absent ⇒ the renderer's
	// default style with the colour loop on. A pointer so an absent block falls through to those
	// defaults rather than being an explicit zero setting, which would read as `spinner-color: false`.
	UI *uiConfig `yaml:"ui"`
}

// unconfinedHost is one Host acknowledgement (CONTEXT: Host acknowledgement): the user's
// recorded claim that ONE named machine is disposable. ID is what platform.HostID() is
// matched against — the safety interlock that stops an acknowledgement travelling between
// machines unnoticed, NOT authentication: anyone who can edit the config can write any id.
// Acknowledged (a free-form date) and Note are for the human reading the file back months
// later; nothing resolves off them, so neither is required.
type unconfinedHost struct {
	ID           string `yaml:"id"`
	Acknowledged string `yaml:"acknowledged"`
	Note         string `yaml:"note"`
}

// serverEntry is one named upstream server (`servers:` in config.yaml): an endpoint this session
// can be moved to, plus what that server needs in order to be talked to. It is ONE type on disk
// and resolved (the unconfinedHost posture) because there is nothing to map across — every field
// travels to the composition root exactly as the user wrote it.
//
// Name does three jobs with one value: it labels the entry for the user, it is the name the
// session is switched by, and it becomes the footer's host alias once the session is on that
// server — which is why it is required and must be unique (it mirrors `host-alias:`, which names
// the startup endpoint the same way). Endpoint is required for the obvious reason.
//
// APIKey and Model are optional. An empty key sends no Authorization header, the keyless
// local-server default; an empty model leaves that server's discovery hint unset, so whatever it
// serves is bound. APIKey is FILE-ONLY on purpose: APOGEE_API_KEY is a single value and it belongs
// to the STARTUP server (the top-level `endpoint:`), so a keyed alternative carries its own key
// here rather than borrowing that one.
type serverEntry struct {
	Name     string `yaml:"name"`
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api-key"`
	Model    string `yaml:"model"`
}

// validateServers rejects an entry that could never be switched to, at the startup boundary where
// the message can count the entry out for the user: one with no name (nothing to select it by, and
// nothing for the footer to call it), one with no endpoint (nothing to talk to), and one whose
// name an earlier entry already took — a name resolves to ONE server, so a repeat is a defect in
// the file rather than a preference between two entries.
//
// It runs over the whole list rather than stopping at the first usable entry, on the
// contextFilesSettings.validate reasoning: a defect in the file outlives the day it was written,
// and a typo found months later has lost its context. What is deliberately NOT checked is whether
// an endpoint answers — that is what the heartbeat asks, live, and a server that is merely off
// today must still be listed.
func validateServers(servers []serverEntry) error {
	seen := make(map[string]struct{}, len(servers))
	for i, s := range servers {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): has no name — the name is what selects "+
				"the server, and what the status footer calls it once the session is on it", i+1, s.Endpoint)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("apogee: servers: entry %d (%q): an earlier entry already has that name — "+
				"one name names one server, so give this one its own", i+1, s.Name)
		}
		seen[s.Name] = struct{}{}
		if strings.TrimSpace(s.Endpoint) == "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): has no endpoint — give the server's "+
				"OpenAI-compatible URL, the same kind of value the top-level endpoint: takes", i+1, s.Name)
		}
	}
	return nil
}

// validatedSetsConfig is the on-disk schema for the Validated-set surface (ADR 0016):
// `enable` is the §5 off-switch (a pointer so an explicit `enable: false` is
// distinguishable from an absent key, default true); `alias` is the §3 explicit
// carry-over map, runtime fingerprint label → entry key. A dangling alias target is a
// loud startup error (the ADR 0015 removed-ID posture — it is the user's own config).
type validatedSetsConfig struct {
	Enable *bool             `yaml:"enable"`
	Alias  map[string]string `yaml:"alias"`
}

// presentConfig is the on-disk schema for the `present:` block (ADR 0019). It mirrors
// presentSettings with yaml tags; toPresentSettings maps it across so the on-disk shape and the
// resolved value stay independently evolvable (as mcpServerConfig does for mcp.ServerConfig).
type presentConfig struct {
	// AutoOpen is a pointer so an explicit `auto-open: false` is distinguishable from an absent
	// key (which keeps the default true).
	AutoOpen *bool `yaml:"auto-open"`
	// Command is the opener template with {path} where the document goes; empty ⇒ the OS default.
	Command string `yaml:"command"`
	// Port is the doc server's TCP port; 0 (the default) takes an ephemeral one.
	Port int `yaml:"port"`
	// Host is the address served URLs advertise; empty ⇒ detected (see presentSettings.host).
	Host string `yaml:"host"`
}

// toPresentSettings maps the on-disk present block onto the resolved value, applying the
// auto-open default (true) when the key is absent. A block that sets one key therefore leaves the
// other three at their defaults, which is what makes it usable a line at a time.
func (p presentConfig) toPresentSettings() presentSettings {
	s := presentSettings{autoOpen: true, command: p.Command, port: p.Port, host: p.Host}
	if p.AutoOpen != nil {
		s.autoOpen = *p.AutoOpen
	}
	return s
}

// systemPromptEntryConfig is the on-disk schema for one `system-prompt-models:` entry (ADR 0023).
// Its two keys are the SAME spellings as the top-level ones (owner decision: the inner text key is
// `system-prompt-text`, not a bare `text`), so moving a prompt from the global keys into a
// per-model entry is a re-indent rather than a rename, and neither spelling has to be learned twice.
type systemPromptEntryConfig struct {
	Text string `yaml:"system-prompt-text"`
	File string `yaml:"system-prompt-file"`
}

// toSystemPromptSettings maps the three on-disk system-prompt keys onto the resolved value (the
// toPresentSettings shape), mapping the per-model entries across one by one so the on-disk schema
// and the resolved one stay independently evolvable. It applies no defaults and rejects nothing:
// an empty source is simply "no prompt configured here", and the contradictions are
// systemPromptSettings.validate's to name.
func (fc fileConfig) toSystemPromptSettings() systemPromptSettings {
	s := systemPromptSettings{global: promptSource{text: fc.SystemPromptText, file: fc.SystemPromptFile}}
	if len(fc.SystemPromptModels) > 0 {
		s.models = make(map[string]promptSource, len(fc.SystemPromptModels))
		for model, e := range fc.SystemPromptModels {
			s.models[model] = promptSource{text: e.Text, file: e.File}
		}
	}
	return s
}

// contextFilesConfig is the on-disk schema for the `context-files:` block: `enable` is the
// off-switch (a pointer so an explicit `enable: false` is distinguishable from an absent key,
// default true — the validated-sets posture), and `names` the workspace-relative names to look for
// in inclusion order.
//
// `names` is a slice rather than a pointer because YAML already tells absent from empty here: an
// absent (or null) key decodes to a nil slice and keeps the default [AGENTS.md], while an explicit
// `names: []` decodes to an empty non-nil slice and means "no names" — the second spelling of off.
type contextFilesConfig struct {
	Enable *bool    `yaml:"enable"`
	Names  []string `yaml:"names"`
}

// toContextFilesSettings maps the on-disk context-files block onto the resolved value, applying the
// defaults for the keys the block leaves out — so `enable: false` alone, or `names:` alone, is a
// usable one-line block (the toPresentSettings shape). It applies no validation: the names are
// contextFilesSettings.validate's to refuse.
func (c contextFilesConfig) toContextFilesSettings() contextFilesSettings {
	s := defaultContextFilesSettings()
	if c.Enable != nil {
		s.enable = *c.Enable
	}
	if c.Names != nil { // present-but-empty is "no names", not "the default"
		s.names = c.Names
	}
	return s
}

// uiConfig is the on-disk schema for the `ui:` block. It mirrors uiSettings with yaml tags;
// toUISettings maps it across so the on-disk shape and the resolved value stay independently
// evolvable (as presentConfig does for presentSettings).
type uiConfig struct {
	// Spinner names the status-line animation — snake | glitter | classic. Empty ⇒ the default.
	// It stays a raw string here: uiSettings.validate parses it once, so an unknown name reaches
	// startup as an error rather than being quietly dropped at the yaml seam.
	Spinner string `yaml:"spinner"`
	// SpinnerColor gates the colour loop over whichever style Spinner names — an INDEPENDENT key,
	// not a property of a style. A pointer so an explicit `spinner-color: false` is distinguishable
	// from an absent key (which keeps the default true).
	SpinnerColor *bool `yaml:"spinner-color"`
}

// toUISettings maps the on-disk ui block onto the resolved value, applying the defaults for the keys
// the block leaves out. A block that sets one key therefore leaves the other at its default, which
// is what keeps the two axes independent from the on-disk shape onward: naming a style does not turn
// the colour loop off, and turning the loop off does not change the style.
func (u uiConfig) toUISettings() uiSettings {
	s := defaultUISettings()
	if u.Spinner != "" {
		s.spinner = tui.SpinnerStyle(u.Spinner) // validated by uiSettings.validate, not here
	}
	if u.SpinnerColor != nil {
		s.spinnerColor = *u.SpinnerColor
	}
	return s
}

// mcpServerConfig is the on-disk schema for one MCP server (P3.15). It mirrors mcp.ServerConfig
// with yaml tags; toServerConfig maps it across so the on-disk shape and the package's value
// type stay independently evolvable.
type mcpServerConfig struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Env       []string `yaml:"env"`
	Endpoint  string   `yaml:"endpoint"`
}

// toServerConfig maps the on-disk MCP server schema onto the mcp.ServerConfig value the client
// connects with.
func (m mcpServerConfig) toServerConfig() mcp.ServerConfig {
	return mcp.ServerConfig{
		Name:      m.Name,
		Transport: mcp.Transport(m.Transport),
		Command:   m.Command,
		Args:      m.Args,
		Env:       m.Env,
		Endpoint:  m.Endpoint,
	}
}

// modelProfileConfig is the on-disk schema for the model profile (CONTEXT: Model profile). It
// mirrors apogee.ModelProfile with yaml tags; toModelProfile maps it across so the on-disk shape
// and the value type stay independently evolvable (as mcpServerConfig does for mcp.ServerConfig).
type modelProfileConfig struct {
	ToolCallFormat  string         `yaml:"tool-call-format"`
	ToolCallPattern string         `yaml:"tool-call-pattern"`
	Thinking        thinkingConfig `yaml:"thinking"`
}

// thinkingConfig is the on-disk schema for a model's inline thinking channel (part of the model
// profile). It mirrors apogee.ThinkingProfile with yaml tags.
type thinkingConfig struct {
	Style string `yaml:"style"`
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

// toModelProfile maps the on-disk model-profile schema onto the apogee.ModelProfile value the
// loop translates to its parsers at the seam. An empty tool-call-format / thinking style resolves
// to the native, no-inline-thinking default downstream.
func (p modelProfileConfig) toModelProfile() apogee.ModelProfile {
	return apogee.ModelProfile{
		ToolCallFormat: apogee.ToolCallFormat(p.ToolCallFormat),
		Pattern:        p.ToolCallPattern,
		Thinking: apogee.ThinkingProfile{
			Style: apogee.ThinkingStyle(p.Thinking.Style),
			Start: p.Thinking.Start,
			End:   p.Thinking.End,
		},
	}
}

// layer projects a parsed file config onto a precedence layer: a present (non-empty)
// field becomes an explicit setting, an absent one stays nil to fall through.
func (fc fileConfig) layer() layer {
	var l layer
	if fc.Endpoint != "" {
		l.endpoint = &fc.Endpoint
	}
	if fc.Model != "" {
		l.model = &fc.Model
	}
	if fc.Mode != "" {
		l.mode = &fc.Mode
	}
	if fc.HostAlias != "" {
		l.hostAlias = &fc.HostAlias
	}
	if fc.Bypass != nil {
		l.bypass = fc.Bypass
	}
	if fc.APIKey != "" {
		l.apiKey = &fc.APIKey
	}
	if fc.ConfineToWorkspace != nil {
		l.confineToWorkspace = fc.ConfineToWorkspace
	}
	if len(fc.UnconfinedHosts) > 0 {
		l.unconfinedHosts = fc.UnconfinedHosts
	}
	if len(fc.Servers) > 0 {
		l.servers = fc.Servers
	}
	if fc.WebSearch != "" {
		l.webSearchEndpoint = &fc.WebSearch
	}
	if fc.UseProjectSkills != nil {
		l.useProjectSkills = fc.UseProjectSkills
	}
	if fc.AutoCompact != nil {
		l.autoCompact = fc.AutoCompact
	}
	if fc.ContextWindow > 0 {
		l.contextWindow = &fc.ContextWindow
	}
	if len(fc.MCPServers) > 0 {
		servers := make([]mcp.ServerConfig, len(fc.MCPServers))
		for i, m := range fc.MCPServers {
			servers[i] = m.toServerConfig()
		}
		l.mcpServers = servers
	}
	if fc.ModelProfile != nil {
		p := fc.ModelProfile.toModelProfile()
		l.profile = &p
	}
	if len(fc.Mechanisms) > 0 {
		l.mechanisms = fc.Mechanisms
	}
	if fc.ValidatedSets != nil {
		l.validatedSetsEnable = fc.ValidatedSets.Enable
		if len(fc.ValidatedSets.Alias) > 0 {
			l.validatedSetsAlias = fc.ValidatedSets.Alias
		}
	}
	if fc.Present != nil {
		p := fc.Present.toPresentSettings()
		l.present = &p
	}
	// Three top-level keys rather than a block, so the projection asks whether ANY of them is
	// present: one inline prompt, one file, or a per-model map alone all configure the subsystem.
	if fc.SystemPromptText != "" || fc.SystemPromptFile != "" || len(fc.SystemPromptModels) > 0 {
		sp := fc.toSystemPromptSettings()
		l.systemPrompt = &sp
	}
	if fc.ContextFiles != nil {
		c := fc.ContextFiles.toContextFilesSettings()
		l.contextFiles = &c
	}
	if fc.UI != nil {
		u := fc.UI.toUISettings()
		l.ui = &u
	}
	if fc.CursorShape != "" {
		l.cursorShape = &fc.CursorShape
	}
	return l
}

// loadFileConfig reads and parses the config file, returning an empty layer when the
// file is absent (the common case — a config file is optional). A malformed file is a
// hard error: silently ignoring it would mask a typo'd setting. readFile is injected so
// the loader is testable without touching the filesystem.
func loadFileConfig(path string, readFile func(string) ([]byte, error)) (layer, error) {
	if path == "" {
		return layer{}, nil
	}
	data, err := readFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return layer{}, nil
		}
		return layer{}, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return layer{}, fmt.Errorf("apogee: parse config %q: %w", path, err)
	}
	return fc.layer(), nil
}

// ----------------------------------------------------------------------------
// The environment and flag layers
// ----------------------------------------------------------------------------

// Environment variable names, prefixed APOGEE_ to namespace the process environment.
const (
	envEndpoint  = "APOGEE_ENDPOINT"
	envModel     = "APOGEE_MODEL"
	envMode      = "APOGEE_MODE"
	envBypass    = "APOGEE_BYPASS"
	envAPIKey    = "APOGEE_API_KEY"
	envConfig    = "APOGEE_CONFIG"
	envWorkspace = "APOGEE_WORKSPACE"
)

// envLayer reads the APOGEE_* variables into a precedence layer; an unset variable
// stays nil to fall through. A set-but-unparseable APOGEE_BYPASS is a hard error rather
// than a silently-ignored boolean. getenv is injected so the layer is testable without
// mutating the process environment.
func envLayer(getenv func(string) string) (layer, error) {
	var l layer
	if v := getenv(envEndpoint); v != "" {
		l.endpoint = &v
	}
	if v := getenv(envModel); v != "" {
		l.model = &v
	}
	if v := getenv(envMode); v != "" {
		l.mode = &v
	}
	if v := getenv(envBypass); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return layer{}, fmt.Errorf("apogee: invalid %s %q: want a boolean", envBypass, v)
		}
		l.bypass = &b
	}
	// The upstream bearer token: the env layer is the RECOMMENDED source (it beats `api-key:`
	// and never touches the config file), and the only one above the file — no flag carries it.
	if v := getenv(envAPIKey); v != "" {
		l.apiKey = &v
	}
	return l, nil
}

// flagLayer projects the parsed flags onto a precedence layer, including a field only
// when its flag was explicitly set (changed reports cobra's per-flag Changed). An unset
// flag carries its zero default, which must not shadow a lower layer — so it is omitted.
func flagLayer(opts options, changed func(string) bool) layer {
	var l layer
	if changed("endpoint") {
		v := opts.endpoint
		l.endpoint = &v
	}
	if changed("model") {
		v := opts.model
		l.model = &v
	}
	if changed("mode") {
		v := opts.mode
		l.mode = &v
	}
	if changed("bypass") {
		v := opts.bypass
		l.bypass = &v
	}
	return l
}

// ----------------------------------------------------------------------------
// The orchestrator
// ----------------------------------------------------------------------------

// applyConfig resolves the upstream/autonomy settings by precedence and writes them
// back into opts before construction. The config file lives at <apogee-home>/config.yaml,
// where the home follows --config > APOGEE_CONFIG > ~/.apogee; the file cannot set the
// home (it lives inside it), so --config / APOGEE_CONFIG are overlaid onto opts first.
// The workspace honours --workspace > APOGEE_WORKSPACE > cwd the same way. changed,
// getenv, and readFile are injected so the whole chain is testable end-to-end.
//
// This is where the live machine identity enters resolution: platform.HostID() selects the
// Host acknowledgement, if any, that applies on this host (ADR 0012, amendment 2026-07-21).
// notify receives resolution's soft notices — a malformed acknowledgement is reported and
// skipped, never fatal — on stderr, like the other pre-TUI startup lines.
func applyConfig(opts *options, changed func(string) bool, getenv func(string) string, readFile func(string) ([]byte, error), notify func(string)) error {
	opts.configDir = resolveConfigDir(opts.configDir, changed, getenv)
	if !changed("workspace") {
		if v := getenv(envWorkspace); v != "" {
			opts.workspace = v
		}
	}

	file, err := loadFileConfig(configFilePath(opts.configDir), readFile)
	if err != nil {
		return err
	}
	env, err := envLayer(getenv)
	if err != nil {
		return err
	}

	s, notices := resolveSettings(file, env, flagLayer(*opts, changed), platform.HostID())
	for _, n := range notices {
		notify(n)
	}
	// A present block that cannot be honoured is a hard error, before anything is written back
	// into opts: an out-of-range port would otherwise surface as a degraded rung at the first
	// presentation, long after the typo (ADR 0019 §4 degrades MECHANISM failures, not config).
	if err := s.present.validate(); err != nil {
		return err
	}
	// A ui block naming a spinner style this build has no animation for is the same kind of loud
	// startup error: silently resolving it to another style would leave the user staring at a
	// spinner their config did not ask for, with nothing pointing at the typo.
	if err := s.ui.validate(); err != nil {
		return err
	}
	// A `cursor-shape:` naming a shape no terminal cursor has is the same kind of loud startup
	// error, for the same reason: drawing a block instead would leave the user staring at a caret
	// their config did not ask for. internal/tui owns the vocabulary (ParseCursorShape lists the
	// shapes); this only adds the key the bad value was read from, which that package cannot know.
	if _, err := tui.ParseCursorShape(s.cursorShape); err != nil {
		return fmt.Errorf("apogee: invalid cursor-shape: %w", err)
	}
	// A system-prompt block that contradicts itself — both spellings of one prompt at one level,
	// or a per-model entry carrying no prompt at all — is a defect in the FILE, independent of
	// this machine and of which model this run resolves, so it is refused here for every level.
	// Whether the SELECTED source's file reads and its placeholders are known is
	// resolveSystemPrompt's job, after model resolution (ADR 0023).
	if err := s.systemPrompt.validate(); err != nil {
		return err
	}
	// A context-file NAME that cannot be a workspace file — empty, rooted, drive-scoped, climbing
	// out with "..", or listed twice — is the same kind of machine-independent defect in the file,
	// refused here where the message can name `context-files.names` and the value. Whether the
	// named files exist is deliberately not asked: discovery is the feature (contextFilesSettings).
	if err := s.contextFiles.validate(); err != nil {
		return err
	}
	// A `servers:` entry that could never be switched to — no name, no endpoint, or a name an
	// earlier entry already took — is refused here for the same reason: it is a defect in the file,
	// independent of this machine and of whether the session ever reaches for that server. Left to
	// the moment of the switch it would surface as an entry that cannot be selected, or a name
	// resolving to whichever entry happened to come first, long after the line was written.
	if err := validateServers(s.servers); err != nil {
		return err
	}
	opts.endpoint = s.endpoint
	opts.model = s.model
	opts.mode = s.mode
	opts.bypass = s.bypass
	opts.hostAlias = s.hostAlias
	opts.apiKey = s.apiKey
	opts.servers = s.servers
	opts.confineToWorkspace = s.confineToWorkspace
	opts.unconfinedHosts = s.unconfinedHosts
	opts.webSearchEndpoint = s.webSearchEndpoint
	opts.useProjectSkills = s.useProjectSkills
	opts.autoCompact = s.autoCompact
	opts.contextWindow = s.contextWindow
	opts.mcpServers = s.mcpServers
	opts.profile = s.profile
	opts.mechanisms = s.mechanisms
	opts.validatedSetsEnable = s.validatedSetsEnable
	opts.validatedSetsAlias = s.validatedSetsAlias
	opts.present = s.present
	opts.systemPrompt = s.systemPrompt
	opts.contextFiles = s.contextFiles.resolved()
	opts.ui = s.ui
	opts.cursorShape = s.cursorShape
	if opts.hostAlias == "" {
		opts.hostAlias = hostFromEndpoint(opts.endpoint)
	}
	return nil
}

// hostFromEndpoint extracts the bare host (without scheme or port) from an endpoint URL —
// the footer's fallback when no host-alias is configured. A URL that does not parse, or
// carries no host, falls back to the raw endpoint so the footer still shows something
// identifiable. An empty endpoint stays empty.
func hostFromEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return endpoint
	}
	return u.Hostname()
}

// resolveConfigDir returns the apogee home honouring --config > APOGEE_CONFIG, falling
// back to the passed value (empty ⇒ the ~/.apogee default, applied downstream by
// apogeeHome). The config file lives inside the home, so it cannot set it. Shared by the
// first-run seeder and applyConfig so both agree on where config lives.
func resolveConfigDir(configDir string, changed func(string) bool, getenv func(string) string) string {
	if !changed("config") {
		if v := getenv(envConfig); v != "" {
			return v
		}
	}
	return configDir
}

// configFilePath returns the config.yaml path under the resolved apogee home, or "" if
// the home cannot be resolved (no config file then — loadFileConfig treats "" as absent,
// and resolveRoots surfaces the home-resolution failure later with a clearer message).
func configFilePath(configDir string) string {
	home, err := apogeeHome(configDir)
	if err != nil {
		return ""
	}
	return filepath.Join(home, "config.yaml")
}

// Upstream discovery is deliberately absent from this file. It used to live here as the
// lowest-priority resolution layer (flag > env > file > discover), probing the server before the
// first paint; it now belongs to the heartbeat, which asks the same question every ten seconds from
// inside the running session (ADR 0024). `model:` and `context-window:` are what they always were —
// a pinned model id and a pinned window — but neither is filled in from the wire any more: an unset
// model stays empty until the first beat binds one, and an unset window stays 0, which is why
// opts.contextWindow > 0 now means "the user pinned it" and nothing else.

// ----------------------------------------------------------------------------
// System-prompt selection (ADR 0023: after the model is resolved)
// ----------------------------------------------------------------------------

// resolveSystemPrompt collapses the resolved system-prompt block into the ONE template
// apogee.Config.SystemPrompt carries for the model this session is BOUND to. The composition root
// calls it AFTER model resolution, with the model as configured — which on a cold start is no
// model at all, so the global template is selected — and rebindSpecFor re-runs exactly this call
// with the model the first beat observes, so the per-model entry lands the moment a model is
// bound and again on every switch (ADR 0024).
//
// Selection is whole-entry replacement: an entry keyed on model replaces the global prompt
// entirely (a per-model file does not inherit a global text), and every other entry is inert —
// never selected, and its file never read. home is the apogee home (the directory config.yaml
// itself lives in), the base a relative system-prompt-file resolves against: the key lives in a
// global file that travels with that home, so resolving against the workspace would break one
// config across projects. readFile is injected so selection is testable without a filesystem.
//
// Everything that is only checkable for the SELECTED source happens here — the file must read,
// and the template's placeholders must be the known three. Both errors name the config key the
// prompt came from, because the same two spellings appear at every level.
func resolveSystemPrompt(sp systemPromptSettings, model, home string, readFile func(string) ([]byte, error)) (string, error) {
	src := sp.global
	modelKey := "" // non-empty ⇒ the selected prompt came from a system-prompt-models entry
	if m, ok := sp.models[model]; ok {
		src, modelKey = m, model
	}

	template := src.text
	if src.file != "" {
		path, err := expandUserPath(src.file)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(home, path)
		}
		data, err := readFile(path)
		if err != nil {
			return "", fmt.Errorf("apogee: read %s %q: %w", systemPromptKey("system-prompt-file", modelKey), path, err)
		}
		template = string(data)
	}
	if err := prompt.Validate(template); err != nil {
		field := "system-prompt-text"
		if src.file != "" {
			field = "system-prompt-file"
		}
		return "", fmt.Errorf("apogee: %s: %w", systemPromptKey(field, modelKey), err)
	}
	return template, nil
}

// systemPromptKey names a system-prompt key for an error message, qualified by the model when the
// value came from a system-prompt-models entry — so the message points at the line to edit rather
// than at one of the several places the same key spelling appears.
func systemPromptKey(field, model string) string {
	if model == "" {
		return field
	}
	return fmt.Sprintf("system-prompt-models[%q].%s", model, field)
}

// expandUserPath expands a leading `~` (alone, or as `~/…`) to the user's home directory, so a
// config may name a file the way the user would type it in a shell. Any other path is returned
// unchanged — including one whose `~` is not leading, which is a legal filename character.
func expandUserPath(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("apogee: resolve home directory for %q: %w", p, err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[len("~/"):]), nil
}
