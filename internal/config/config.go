package config

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
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// Config precedence (phase-2 detail plan §4 P2.5: flag > env > file > default)
// ----------------------------------------------------------------------------

// The settable upstream/autonomy values resolve from four sources, highest priority
// first: an explicitly-set command-line flag, then an APOGEE_* environment variable,
// then the config file (<apogee-home>/config.yaml), then the built-in default. The
// resolution is split into a pure core (ResolveSettings over optional layers) and a
// thin orchestrator (ApplyConfig) that builds the layers from the live flag set,
// environment, and filesystem — so the precedence rule is table-testable without cobra,
// an environment, or a real file (the P2.5 acceptance).

// Settings is the resolved configuration after precedence is applied: the values the
// composition root feeds into the domain.Config and the TUI Options.
type Settings struct {
	Mode   string
	Bypass bool

	// servers is the resolved `servers:` list: the SINGLE definition of the upstream servers this
	// config knows (ADR 0036), in file order — the one a session starts on and every one it can be
	// switched to. File-only (no flag, no env — like mcpServers): naming a machine's endpoint and
	// its key is a config act, not an invocation one. Absent/empty ⇒ no server is configured at
	// all, which selection answers rather than resolution.
	Servers []ServerEntry

	// startupServer is the resolved `server:` key: the NAME of the `servers:` entry this session
	// starts on — the last one chosen, which /server records automatically. It resolves
	// flag > env > file (`--server`, `APOGEE_SERVER`, `server:`), so one invocation can start on
	// another configured machine without editing the file. Empty ⇒ no choice is recorded (or none
	// is asked for). It is deliberately NOT validated against the list here: which names exist is
	// a fact about the resolved `servers:` list, so a name that matches nothing is answered at
	// selection, where the list is in hand.
	StartupServer string

	// editor is the resolved `editor:` key: the command line an external edit is opened with, carried
	// exactly as the user wrote it (`code -w` stays two words until the launch site splits it).
	// File-only, on the `servers:` reasoning: naming the program installed on this machine is a
	// config act, not an invocation one. Empty ⇒ the rest of the ladder decides — $VISUAL, then
	// $EDITOR, then the OS default opener (ADR 0041).
	Editor string

	// confineToWorkspace is GLOBAL-CONFIG-ONLY (ADR 0012): it is resolved from the config
	// file alone, never from a flag or env, so a hostile repo invoking apogee cannot loosen
	// Auto's blast radius. Default true. (There is no project-level config file today; the
	// file-only resolution is what keeps it un-loosenable by the invocation environment.)
	// It is the EFFECTIVE value: an explicit `confine-to-workspace: false` OR a Host
	// acknowledgement matching this machine resolves it to false (resolveConfineToWorkspace).
	ConfineToWorkspace bool

	// unconfinedHosts is the Host acknowledgement list as configured (ADR 0012, amendment
	// 2026-07-21) — the machines the user has recorded as disposable. File-only on the same
	// reasoning as confine-to-workspace: a hostile repo must not be able to name your host.
	// It is carried past resolution (which collapses it into confineToWorkspace above) so the
	// session can report the list back and extend it.
	UnconfinedHosts []UnconfinedHost

	// webSearchEndpoint is the config'd search backend for the web_search tool (P3.11),
	// file-only (empty ⇒ the built-in DuckDuckGo default; "off" disables the tool).
	WebSearchEndpoint string

	// useProjectSkills gates whether the workspace's bare skills/ folder is discovered (in
	// addition to the global library and the project's .apogee/skills, which are always loaded).
	// File-only, default TRUE — a project's skills/ is trusted by default, like the @file
	// references the same workspace already feeds the model.
	UseProjectSkills bool

	// autoCompact gates the automatic, budget-driven generative Compaction trigger (item 9). File-only,
	// default TRUE — Compaction is structural and load-bearing (it stays on under Bypass, D5/D6), so it
	// runs unless a config explicitly opts out with `auto-compact: false`. The on-demand /compact command
	// is unaffected by it (that always folds on request).
	AutoCompact bool

	// autoTitle gates the AUTOMATIC session-naming call — the cosmetic out-of-band completion that
	// names a new Session record from its first prompt. File-only, default TRUE. It is cosmetic, not
	// structural: nothing breaks when it is off (the heuristic title stamped at the first Save
	// stands), which is why it can be turned off at all. It gates only the automatic firing — the
	// generator stays wired either way, so `/rename` still regenerates on demand under
	// `auto-title: false`.
	AutoTitle bool

	// rememberModel gates remembering the model choice: the write into the session's `servers:`
	// entry on an explicit pick, and the TUI's startup restore of what was recorded. File-only, and
	// default FALSE — writing back into the human's own config is opted into, never assumed.
	RememberModel bool

	// contextWindow PINS the model context window in tokens (item 3 / S3). File-only (no flag/env,
	// like autoCompact) and default 0 ⇒ unpinned, so the window is whatever the ten-second
	// heartbeat observes, live, and follows a model switch with it; a positive value is never
	// overridden by a beat (ADR 0024 — the escape hatch for a server that does not advertise a
	// window, or advertises one that is wrong for how it is run). Nothing pre-fills it any more, so
	// > 0 means "the user pinned it" and nothing else. It feeds ContextConfig.MaxContextTokens,
	// which the Budget and automatic Compaction bind against.
	ContextWindow int

	// mcpServers is the set of external MCP servers to connect on startup (P3.15), file-only
	// and default-empty (no servers ⇒ the MCP feature is dormant). Their tools surface into the
	// registry as classMCP ExternalEffectTools the disposition gates in Auto.
	MCPServers []mcp.ServerConfig

	// toolsDisabled is the `tools.disabled:` roster switch: the built-in tools this config takes
	// off the menu, by name. File-only and default-empty (⇒ the whole roster), like mcpServers
	// beside it — which tools a model is offered is a per-machine tuning fact, not an invocation
	// one. It is deliberately GLOBAL: a per-profile roster is a later key that builds on it.
	ToolsDisabled []string

	// modelProfiles is the resolved `model-profiles:` map (ADR 0044): the user's pattern-keyed
	// Model profiles, ordered by pattern so the same file always resolves to the same slice.
	// File-only (a per-model concern, like mcpServers, with no flag/env) and default-empty ⇒ the
	// user configures no shape and the composition root resolves against the shipped table alone.
	// The composition root — never this package — matches a model name against them
	// (profiles.Resolve): which model is bound is not a fact the config file holds.
	ModelProfiles []profiles.Entry

	// mechanisms enables catalogued small-model Mechanisms by canonical ID (Phase 4), file-only
	// (a per-model tuning concern, like mcpServers, with no flag/env) and default-empty. All
	// Mechanisms ship OFF (D1 — default-off until bench-proven); a `true` entry turns one on. The
	// composition root validates every key against the catalogue and hands the enabled IDs to
	// domain.Config.EnableMechanisms, which the engine builds catalogue rows from (ADR 0015 §1); an
	// unknown ID is a loud startup error. Bypass still wins (an enabled non-off-ramp Mechanism is
	// not dispatched under bypass — ADR 0006 / item 2's gate).
	Mechanisms map[string]bool

	// validatedSetsEnable gates the Validated-set runtime surface (ADR 0016 §5's off-switch),
	// file-only and default TRUE — a matching per-model set applies (or is offered) unless the
	// config explicitly opts out with `validated-sets: enable: false`.
	ValidatedSetsEnable bool

	// validatedSetsAlias is the explicit carry-over map (ADR 0016 §3): runtime fingerprint
	// label → Validated-set entry key. File-only, default-empty. An identity mapping is the
	// low-confidence confirm ("my model is what the label says"); a differing mapping is the
	// explicit transfer to a sibling quant/family member.
	ValidatedSetsAlias map[string]string

	// present is the resolved `present:` block (ADR 0019): which mechanisms of the presentation
	// ladder this host offers a finished document. File-only (no flag/env, like the newer keys
	// above). Its defaults are auto-open ON — the headline want is that a run on the user's own
	// desktop simply opens the deliverable — with no command override, an ephemeral doc-server
	// port, and a detected advertise host.
	Present PresentSettings

	// systemPrompt is the resolved system-prompt block (ADR 0023): the global prompt (inline or
	// a file) and the per-model overrides. File-only (no flag/env, like present above), and its
	// zero value is no prompt at all — the promptless request apogee sent before ADR 0023. The
	// composition root collapses it into the ONE template domain.Config carries, with
	// ResolveSystemPrompt, AFTER model resolution: which entry applies is not known until then.
	SystemPrompt SystemPromptSettings

	// contextFiles is the resolved `context-files:` block: the workspace-root files whose content
	// joins the standing system content (the AGENTS.md / CLAUDE.md behaviour). File-only (no
	// flag/env, like the system-prompt keys it sits beside) and its default is ON with the one
	// name AGENTS.md, so a repo carrying that file works with no configuration at all. The
	// composition root collapses it into the name list domain.Config.ContextFiles carries.
	ContextFiles contextFilesSettings

	// ui is the resolved `ui:` block: how the terminal UI presents itself — today the status-line
	// spinner's animation and its colour loop. File-only (no flag/env, like the blocks above), and
	// its defaults are the renderer's own (defaultUISettings): the default style with the colour
	// loop on. The composition root hands both values to the TUI as Options.
	UI UISettings

	// cursorShape is the configured shape of the prompt's caret, as the user spelled it (block |
	// underline | bar). File-only like the ui block, and empty ⇒ the renderer's default (block).
	// It is carried as the raw NAME — ApplyConfig validates it through tui.ParseCursorShape, and
	// the composition root parses it once more into the tea.CursorShape the TUI Options take, so
	// cmd/apogee never restates the vocabulary internal/tui owns.
	CursorShape string
}

// PresentSettings is the resolved `present:` block (ADR 0019), in the form the composition root
// turns into the host-side mechanisms themselves (wire.go's presentationRungs).
//
// It is one struct rather than four fields on Settings because the four describe ONE subsystem
// and travel together — from the on-disk block, through resolution, to the wire that builds the
// ladder out of them. Nothing in here can switch presentation off: rung 0, the transcript line
// carrying the path, needs no configuration and is never skipped. These keys only change WHICH
// mechanism above it carries the document.
type PresentSettings struct {
	// autoOpen gates rungs 1 and 3 — handing the document to a desktop application, on a LOCAL
	// session. Default true. False wires no opener at all, which covers the command override too:
	// present.command says which application shows a document, not whether one is opened.
	AutoOpen bool
	// command is the present.command template (e.g. `zed {path}`), which replaces the built-in OS
	// opener on every OS. Empty ⇒ the per-OS default (open / start / xdg-open).
	Command string
	// port is the TCP port the doc server (rung 2) binds. Default 0 ⇒ an ephemeral port, which
	// costs nothing: the URL is printed fresh per presentation, so a stable port buys the user
	// nothing to remember.
	Port int
	// host overrides the address the served URL advertises. Empty ⇒ present.AdvertiseHost's own
	// chain ($SSH_CONNECTION's server IP, then an outbound-dial probe, then loopback). It is the
	// fallback for topologies SSH cannot describe rather than a true override — SSH_CONNECTION,
	// when present, is a live and verified-routable address and still wins (see AdvertiseHost).
	Host string
}

// validate rejects a present block that cannot be honoured. The port is the only checkable key:
// a command template is a statement about the user's own machine (an unresolvable program is a
// fail-visible opener error at presentation time, ADR 0019 §4 — not a startup error), and the
// host is a display string this process cannot verify. An out-of-range port, by contrast, would
// fail deep inside the first presentation, where all the user sees is a degraded rung — so it is
// caught here, where the message can name the key that is wrong.
func (p PresentSettings) Validate() error {
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("apogee: invalid present.port %d: want a TCP port in 0-65535 "+
			"(0 — the default — takes an ephemeral port)", p.Port)
	}
	return nil
}

// PromptSource is one configured system prompt (ADR 0023) as the user wrote it: the template
// inline (text) or the path of a file holding it (file). The two are mutually exclusive
// spellings of one prompt — a level that sets both is a startup error (validate below) — and a
// source with neither set configures no prompt.
type PromptSource struct {
	Text string
	File string
}

// SystemPromptSettings is the resolved system-prompt block (ADR 0023): the global prompt plus
// the per-model overrides, keyed by the RESOLVED model name (the label the Validated-set surface
// keys on too). It is one struct rather than three fields on Settings for the same reason
// PresentSettings is: the keys describe ONE subsystem and travel together, from the on-disk block
// through resolution to the composition root, where ResolveSystemPrompt collapses them into the
// single template domain.Config carries.
//
// Selection is WHOLE-ENTRY replacement: an entry whose key is this session's model replaces the
// global prompt entirely, so a per-model `system-prompt-file` does not inherit a global
// `system-prompt-text`. An entry naming any other model is inert — the `unconfined-hosts`
// posture: it describes a machine/model this run is not, so it is never selected and its file is
// never read (it may only exist elsewhere).
type SystemPromptSettings struct {
	Global PromptSource
	Models map[string]PromptSource
}

// validate rejects a system-prompt block that is structurally impossible, at EVERY level —
// including entries this host will never select. Setting both spellings at one level is a
// contradiction (which prompt was meant?), and an entry setting neither is far more likely a YAML
// indentation slip than a deliberate "this model gets nothing"; both are machine-independent
// defects in the file itself, so they are caught at config time where the message can name the key.
//
// What is deliberately NOT checked here: whether a file reads, and whether a template's
// placeholders are known. Those are properties of the SELECTED source only (ResolveSystemPrompt),
// because a non-matching per-model entry may name a file that exists on another machine — refusing
// to start over it would make one global config unusable everywhere else.
//
// The entries are walked in sorted order so the entry a message names is the same one on every
// run, rather than whichever the map happened to yield first.
func (sp SystemPromptSettings) Validate() error {
	if sp.Global.Text != "" && sp.Global.File != "" {
		return errors.New("apogee: system-prompt-text and system-prompt-file are both set: " +
			"they are two spellings of one prompt — keep the inline text or the file, not both")
	}
	for _, model := range slices.Sorted(maps.Keys(sp.Models)) {
		src := sp.Models[model]
		switch {
		case src.Text != "" && src.File != "":
			return fmt.Errorf("apogee: system-prompt-models[%q] sets both system-prompt-text and "+
				"system-prompt-file: keep the inline text or the file, not both", model)
		case src.Text == "" && src.File == "":
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
// reason PresentSettings is: the keys describe ONE subsystem and travel together, from the on-disk
// block through resolution to the composition root, where resolved() collapses them into the
// single name list domain.Config carries.
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
// SystemPromptSettings.Validate posture (every level is checked, including entries this run will
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

// resolved collapses the block into the list domain.Config.ContextFiles carries: nil — the feature
// off — when the switch is off or the list resolved empty, and otherwise the names in list order.
// The two spellings of "off" are deliberately one value downstream: the engine's contract is that
// an empty list IS the feature being off, so nothing below has to know which spelling was used.
func (cf contextFilesSettings) resolved() []string {
	if !cf.enable || len(cf.names) == 0 {
		return nil
	}
	return cf.names
}

// UISettings is the resolved `ui:` block, in the form the composition root hands the renderer
// (wire.go's tui.Options). It is one struct rather than loose fields on Settings for the same
// reason PresentSettings is: the keys describe ONE subsystem and travel together, from the on-disk
// block through resolution to the wire.
//
// The two spinner keys are deliberately INDEPENDENT. The colour loop is not a property of a style
// and is not folded into the style name: it applies to whichever style spinner names, so all three
// styles × colour on/off are valid combinations. Nothing here or downstream may key one off the
// other.
type UISettings struct {
	// spinner names the status-line animation. It is carried as read (a name this build may not
	// know) until validate parses it, so an unknown style is a startup error naming the key rather
	// than a silent fall back to the default.
	Spinner tui.SpinnerStyle
	// spinnerColor runs the slow colour loop over whichever style spinner names. Default true;
	// false leaves the glyph in the terminal's own text colour, which is the escape hatch for a
	// terminal whose colour depth turns the gradient into steps.
	SpinnerColor bool
	// showScrollbar paints the transcript's scroll bar and reserves the column it hangs in. Default
	// true; false takes both away together — a hidden bar that still ate a column would read as a
	// bug — and the transcript body takes that width instead. It is process-constant, so the wrap
	// width it decides never changes mid-run.
	ShowScrollbar bool
	// colorScheme names the palette every coloured thing on the screen takes its colour from: a
	// built-in (dark, light) or a `<apogee-home>/schemes/<name>.yaml` the user wrote, which shadows
	// a built-in of the same name. It is carried as a NAME rather than a resolved palette because
	// resolution reads a file, which the composition root does at wiring time (wire.go) — and,
	// unlike spinner, an unknown name is deliberately NOT a startup error: a scheme is cosmetic, so
	// a typo costs a warning and the default palette rather than the session (ADR 0040 design
	// call 8).
	ColorScheme string
	// stallAfter is how long the ENGINE may go silent mid-turn before the status line says so: past
	// it the running phrase gains a `· quiet <elapsed>` suffix, which is the honest fact rather than
	// a verdict — a slow turn and a dead one look identical from out here, and only the human can
	// tell them apart. Default 90s, which clears the ingestion of a large prompt (legitimately
	// silent for a minute or two on a local model); 0 turns the suffix off. It is resolved to a
	// DURATION here, unlike spinner beside it, because the renderer takes a duration and nothing
	// downstream would gain from a second parse of the same text.
	StallAfter time.Duration
	// unparsedStallAfter is a `stall-after:` value time.ParseDuration could make nothing of, kept as
	// it was written so Validate can name the text the human typed rather than the value it failed
	// to become. Empty on every config that resolves — including one that never named the key —
	// because the yaml seam applies no judgement of its own (toUISettings) and Validate is the one
	// place a ui block is refused.
	unparsedStallAfter string
}

// defaultStallAfter is how long the engine may stay silent before the status line reports the
// quiet: long enough that ingesting a large prompt never trips it, short enough that a turn which
// really has died is named while the human is still at the screen.
const defaultStallAfter = 90 * time.Second

// defaultUISettings is the resolved `ui:` block with nothing configured: the renderer's own default
// style, with the colour loop on, the scroll bar shown, the default colour scheme, and the shipped
// quiet threshold the stall guard waits out. The style is
// ASKED of internal/tui (ParseSpinnerStyle's documented "" ⇒ the default) rather than restated here,
// so the vocabulary and its default stay in the one package that owns them — the same reason
// validate does not list the valid names, and the same reason the scheme name comes from
// internal/scheme.
func defaultUISettings() UISettings {
	// ParseSpinnerStyle errors only on a style it does not know; "" is the request for the default,
	// so this cannot fail.
	style, _ := tui.ParseSpinnerStyle("")
	return UISettings{
		Spinner:       style,
		SpinnerColor:  true,
		ShowScrollbar: true,
		ColorScheme:   scheme.DefaultName,
		StallAfter:    defaultStallAfter,
	}
}

// validate rejects a ui block naming a spinner style this build has no animation for, or a
// `stall-after:` that is not a length of time to wait. Catching them here makes a typo a startup
// error that names the key; left to the renderer they would silently resolve to some other style
// and to the shipped threshold, and the user would be left wondering why their setting did nothing.
// The spinner's valid set comes from internal/tui, which owns the vocabulary — this only adds the
// key the bad value was read from, which that package cannot know.
func (u UISettings) Validate() error {
	if _, err := tui.ParseSpinnerStyle(string(u.Spinner)); err != nil {
		return fmt.Errorf("apogee: invalid ui.spinner: %w", err)
	}
	if u.unparsedStallAfter != "" {
		return fmt.Errorf("apogee: invalid ui.stall-after %q: want a length of time like 90s or 2m, "+
			"or 0 to turn the quiet suffix off", u.unparsedStallAfter)
	}
	if u.StallAfter < 0 {
		return fmt.Errorf("apogee: invalid ui.stall-after %s: want 0 or more, where 0 turns the "+
			"quiet suffix off", u.StallAfter)
	}
	return nil
}

// Layer is one precedence source. A nil pointer means the source does not set that
// field, so resolution falls through to the next-lower-priority source. A non-nil
// pointer (including a pointer to the zero value) is an explicit setting that wins over
// everything below it.
type Layer struct {
	Mode   *string
	Bypass *bool

	// startupServer is the `server:` key: the NAME of the `servers:` entry a session starts on.
	// Unlike the list it names — file-only, because naming a machine is a config act — this one
	// key is settable from all three layers (`--server` beats `APOGEE_SERVER` beats the file),
	// because which of those machines THIS run starts on is an invocation fact. A nil pointer
	// means the source names none, so resolution falls through to the empty default.
	StartupServer *string

	// servers is set only by the FILE layer (the `servers:` list is config'd, default-empty, with
	// no flag/env — like mcpServers). A nil slice means the source names no server, so resolution
	// falls through to the empty default.
	Servers []ServerEntry

	// editor is set only by the FILE layer (the editor command is config'd, with no flag/env — like
	// servers above; $VISUAL and $EDITOR are consulted at the launch site, not here, because
	// they are a FALLBACK below this key rather than a layer above it). A nil pointer means the
	// source names no editor, so resolution leaves it empty and the rest of the ladder decides.
	Editor *string

	// confineToWorkspace is set only by the FILE layer (global-config-only, ADR 0012). The
	// env and flag layers leave it nil so the invocation environment cannot loosen it.
	ConfineToWorkspace *bool

	// unconfinedHosts is set only by the FILE layer (global-config-only on the same reasoning
	// as confineToWorkspace above — ADR 0012, amendment 2026-07-21). A nil slice means the
	// source acknowledges no host, which is the default: every host is confined.
	UnconfinedHosts []UnconfinedHost

	// webSearchEndpoint is set only by the FILE layer (P3.11 — web-search is config'd,
	// with no flag/env). Empty/absent ⇒ the built-in DuckDuckGo default; "off" disables.
	WebSearchEndpoint *string

	// useProjectSkills is set only by the FILE layer (skills are config'd, default-on, with no
	// flag/env). A pointer so an explicit `use-project-skills: false` is distinguishable from
	// an absent key (which keeps the default true).
	UseProjectSkills *bool

	// autoCompact is set only by the FILE layer (the automatic Compaction trigger is config'd,
	// default-on, with no flag/env). A pointer so an explicit `auto-compact: false` is
	// distinguishable from an absent key (which keeps the default true).
	AutoCompact *bool

	// autoTitle is set only by the FILE layer (the automatic session-naming call is config'd,
	// default-on, with no flag/env — like autoCompact above). A pointer so an explicit
	// `auto-title: false` is distinguishable from an absent key (which keeps the default true).
	AutoTitle *bool

	// rememberModel is set only by the FILE layer (the toggle is config'd, with no flag/env — like
	// autoTitle above). A pointer so an explicit `remember-model: false` is distinguishable from an
	// absent key; both resolve to off, and the distinction is what tells /settings which of the two
	// the file says.
	RememberModel *bool

	// contextWindow is set only by the FILE layer (the window pin is config'd, no flag/env — like
	// autoCompact). A nil pointer means the source pins no window, so resolution leaves it 0 and
	// the heartbeat's live observation stands; only a positive `context-window:` projects to a
	// non-nil pointer.
	ContextWindow *int

	// mcpServers is set only by the FILE layer (P3.15 — MCP servers are config'd, default-empty,
	// with no flag/env). A nil slice means the source does not configure servers (fall through).
	MCPServers []mcp.ServerConfig

	// toolsDisabled is set only by the FILE layer (the roster is config'd, default-empty, with no
	// flag/env — like mcpServers above). A nil slice means the source disables no tool, so
	// resolution leaves the whole built-in roster standing.
	ToolsDisabled []string

	// modelProfiles is set only by the FILE layer (the map is config'd, default-empty, with no
	// flag/env — like mechanisms). A nil slice means the source configures no profile at all, so
	// a nearer layer that does carries the whole map: the block is replaced entry-and-all, never
	// merged key by key.
	ModelProfiles []profiles.Entry

	// mechanisms is set only by the FILE layer (Mechanisms are config'd, default-empty, with no
	// flag/env — like mcpServers). A nil map means the source does not enable any Mechanism (fall
	// through to the empty default).
	Mechanisms map[string]bool

	// validatedSetsEnable / validatedSetsAlias are set only by the FILE layer (the Validated-set
	// surface is config'd, no flag/env — like mechanisms). A nil enable pointer keeps the default
	// true; a nil alias map means no carry-over is configured.
	ValidatedSetsEnable *bool
	ValidatedSetsAlias  map[string]string

	// present is set only by the FILE layer (the presentation ladder is config'd, no flag/env —
	// like mechanisms). A nil pointer means the source configures no `present:` block, so
	// resolution keeps the defaults (auto-open on, an ephemeral port, a detected host).
	Present *PresentSettings

	// systemPrompt is set only by the FILE layer (the system prompt is config'd, no flag/env —
	// like present above). A nil pointer means the source sets none of the three system-prompt
	// keys, so resolution keeps the zero value: no prompt, today's promptless request.
	SystemPrompt *SystemPromptSettings

	// contextFiles is set only by the FILE layer (the `context-files:` block is config'd, no
	// flag/env — like systemPrompt above). A nil pointer means the source carries no block, so
	// resolution keeps the defaults: on, looking for AGENTS.md in the workspace root.
	ContextFiles *contextFilesSettings

	// ui is set only by the FILE layer (the UI's own presentation is config'd, no flag/env — like
	// present). A nil pointer means the source configures no `ui:` block, so resolution keeps the
	// defaults (the renderer's default spinner style, colour loop on).
	UI *UISettings

	// cursorShape is set only by the FILE layer (the caret's shape is config'd, no flag/env — like
	// the ui block). A nil pointer means the source names no shape, so resolution leaves it empty
	// and the renderer's default (a steady block) stands.
	CursorShape *string
}

// multiSourceKey binds one registry row to the plumbing that carries its key through resolution.
// Three of the schema's keys are settable from more than one source, and they are the only ones
// whose environment-variable and flag NAMES are ever in play — so those names are read from the
// row (EnvVar, FlagName) rather than restated as a literal at each of the three sites that used
// to spell them: the env layer, the flag layer, and ResolveSettings' precedence loop. Source
// metadata therefore has exactly one home, and renaming APOGEE_MODE or --mode is an edit to one
// registry row instead of a three-site edit that can half-land — with the row the /settings
// surface shows as the key's source guaranteed to be the row resolution actually read.
//
// What this table does NOT carry is the raw startup overrides (`--endpoint`, `APOGEE_ENDPOINT`,
// `APOGEE_API_KEY`, `--model`, `APOGEE_MODEL`): since ADR 0036 those name no config key at all —
// they build or overlay a startup server entry — so they are resolved on their own, off the
// registry, rather than pretending to be file keys that no longer exist.
//
// The accessors are what lets the typed Layer/Settings structs stand unchanged (rewriting that
// whole copy chain into table-driven resolution is a separate effort): fromEnv projects a
// variable's text onto a Layer, fromFlag projects the already-parsed flag value, and overlay
// copies a Layer's value onto the resolved Settings when that Layer sets it. A nil fromEnv or
// fromFlag means the key has no source of that kind.
type multiSourceKey struct {
	row      Key
	fromEnv  func(l *Layer, text string) error
	fromFlag func(l *Layer, opts Options)
	overlay  func(s *Settings, l Layer)
}

// multiSourceKeys is that table, in the order the registry lists the keys. The order does not
// affect the outcome — each key overlays its own field, and precedence is the order the LAYERS
// are applied in — it only keeps the table readable beside the registry it is built over.
var multiSourceKeys = []multiSourceKey{
	{
		// The one key of the `servers:` neighbourhood with sources above the file: the list is
		// config, the choice of entry is an invocation.
		row: mustKey("server"),
		fromEnv: func(l *Layer, text string) error {
			l.StartupServer = &text
			return nil
		},
		fromFlag: func(l *Layer, opts Options) {
			v := opts.StartupServer
			l.StartupServer = &v
		},
		overlay: func(s *Settings, l Layer) {
			if l.StartupServer != nil {
				s.StartupServer = *l.StartupServer
			}
		},
	},
	{
		row: mustKey("mode"),
		fromEnv: func(l *Layer, text string) error {
			l.Mode = &text
			return nil
		},
		fromFlag: func(l *Layer, opts Options) {
			v := opts.Mode
			l.Mode = &v
		},
		overlay: func(s *Settings, l Layer) {
			if l.Mode != nil {
				s.Mode = *l.Mode
			}
		},
	},
	{
		row: mustKey("bypass"),
		// The one env value that is parsed rather than carried: a set-but-unparseable flag is a hard
		// error, never a silently-ignored boolean. envLayer adds the variable's name to the message,
		// because the name is the row's to know, not this closure's.
		fromEnv: func(l *Layer, text string) error {
			b, err := strconv.ParseBool(text)
			if err != nil {
				return errors.New("want a boolean")
			}
			l.Bypass = &b
			return nil
		},
		fromFlag: func(l *Layer, opts Options) {
			v := opts.Bypass
			l.Bypass = &v
		},
		overlay: func(s *Settings, l Layer) {
			if l.Bypass != nil {
				s.Bypass = *l.Bypass
			}
		},
	},
}

// ResolveSettings overlays the layers in increasing priority — the default base, then
// the file, then the environment, then the flags — so a flag beats an environment
// variable beats the file beats the default. Only ask-before (the default mode) is a
// non-zero base; `server:` defaults empty and bypass defaults false.
//
// What resolution deliberately does NOT produce is an endpoint: which server a session runs on is
// selected from the resolved `servers:` list afterwards (selectStartupServer), because the answer
// needs the list and the chosen name in one hand (ADR 0036).
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
func ResolveSettings(file, env, flag Layer, hostID string) (Settings, []string) {
	// The default base. mode's default comes from its registry row, so the value resolution starts
	// from and the value /settings shows as "the default" are one string. The remaining defaults
	// stay typed literals on purpose: their rows spell them as TEXT ("true"), and reaching
	// confine-to-workspace's default through a parse of a table entry would leave a safety default
	// one typo away from silently flipping to false.
	s := Settings{Mode: mustKey("mode").Default, ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true,
		AutoTitle: true, ValidatedSetsEnable: true, Present: PresentSettings{AutoOpen: true}, UI: defaultUISettings(),
		ContextFiles: defaultContextFilesSettings()}
	// file-only (ADR 0012 + its 2026-07-21 amendment); env/flag never carry either, so the
	// invocation environment can neither flip the flag nor name a host.
	s.UnconfinedHosts = file.UnconfinedHosts
	confine, notices := resolveConfineToWorkspace(file.ConfineToWorkspace, file.UnconfinedHosts, hostID)
	s.ConfineToWorkspace = confine
	if file.WebSearchEndpoint != nil {
		s.WebSearchEndpoint = *file.WebSearchEndpoint
	}
	if file.UseProjectSkills != nil {
		s.UseProjectSkills = *file.UseProjectSkills
	}
	if file.AutoCompact != nil {
		s.AutoCompact = *file.AutoCompact
	}
	if file.AutoTitle != nil {
		s.AutoTitle = *file.AutoTitle
	}
	if file.RememberModel != nil {
		s.RememberModel = *file.RememberModel
	}
	if file.ContextWindow != nil {
		s.ContextWindow = *file.ContextWindow
	}
	if file.Editor != nil { // file-only (ADR 0041); the env rungs below it are read at launch time
		s.Editor = *file.Editor
	}
	s.Servers = file.Servers             // file-only; env/flag never name an upstream server
	s.MCPServers = file.MCPServers       // file-only (P3.15); env/flag never set MCP servers
	s.ToolsDisabled = file.ToolsDisabled // file-only; env/flag never prune the tool roster
	s.Mechanisms = file.Mechanisms       // file-only (Phase 4); env/flag never enable Mechanisms
	if file.ValidatedSetsEnable != nil { // file-only (ADR 0016); env/flag never touch the surface
		s.ValidatedSetsEnable = *file.ValidatedSetsEnable
	}
	s.ValidatedSetsAlias = file.ValidatedSetsAlias
	// file-only (ADR 0044), like mechanisms above: env/flag never carry a model profile, and a
	// layer that sets the key replaces the map whole rather than merging patterns into it.
	s.ModelProfiles = file.ModelProfiles
	if file.Present != nil { // file-only (ADR 0019); env/flag never carry the presentation block
		s.Present = *file.Present
	}
	if file.SystemPrompt != nil { // file-only (ADR 0023); env/flag never carry a system prompt
		s.SystemPrompt = *file.SystemPrompt
	}
	if file.ContextFiles != nil { // file-only, like the system prompt it stands beside
		s.ContextFiles = *file.ContextFiles
	}
	if file.UI != nil { // file-only; env/flag never carry the UI block
		s.UI = *file.UI
	}
	if file.CursorShape != nil { // file-only, like the UI block above
		s.CursorShape = *file.CursorShape
	}
	// The multi-source keys, lowest-priority layer first: each key's overlay writes its own field,
	// so a later layer that sets the key wins and one that does not leaves the value below it
	// standing. The keys and their sources are the registry's to describe (multiSourceKeys).
	for _, l := range []Layer{file, env, flag} {
		for _, k := range multiSourceKeys {
			k.overlay(&s, l)
		}
	}
	return s, notices
}

// UnknownToolNames returns the entries of a `tools.disabled:` list that name no tool this build
// offers, in the order they were listed and trimmed as the filter trims them. The catalogue is
// internal/tools' own (tools.KnownToolNames), so a tool renamed or added there is answered here
// with no second list to keep in step.
func UnknownToolNames(disabled []string) []string {
	if len(disabled) == 0 {
		return nil
	}
	catalogue := tools.KnownToolNames()
	known := make(map[string]bool, len(catalogue))
	for _, name := range catalogue {
		known[name] = true
	}
	var unknown []string
	for _, name := range disabled {
		if name = strings.TrimSpace(name); name != "" && !known[name] {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

// unknownToolNotice is the ONE line an unrecognised `tools.disabled:` entry produces at startup,
// or "" when every listed name is a tool. It is a notice and not an error on purpose: the list is
// how a roster is pruned on evidence, so a typo in it costs the user the tool they meant to turn
// off — never the session. Every name that IS a tool still applies.
func unknownToolNotice(disabled []string) string {
	unknown := UnknownToolNames(disabled)
	if len(unknown) == 0 {
		return ""
	}
	return fmt.Sprintf("apogee: tools.disabled names %s, which apogee has no tool for — check the "+
		"spelling; the rest of the list applies", strings.Join(quoteAll(unknown), ", "))
}

// quoteAll quotes each name so a list of them reads as names rather than as prose.
func quoteAll(names []string) []string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return quoted
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
func resolveConfineToWorkspace(explicit *bool, hosts []UnconfinedHost, hostID string) (bool, []string) {
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
// fix their servers/autonomy once instead of passing them every invocation.
// Bypass is a pointer so an explicit `bypass: false` is distinguishable from an absent
// key (the former wins over a lower layer; the latter falls through).
//
// The upstream a session talks to is described by exactly two keys, Servers and Server (ADR
// 0036): the list is the single definition of what servers exist, and the pointer says which of
// them this session starts on. The retired top-level `endpoint:`/`api-key:`/`host-alias:`/`model:`
// quadruple said the same things a second time for one privileged server; a config that still
// carries any of them is caught by the legacy sniff (legacyFileConfig) rather than silently
// dropped by the decoder, which ignores keys the schema no longer has.
type fileConfig struct {
	Mode   string `yaml:"mode"`
	Bypass *bool  `yaml:"bypass"`
	// Servers is the single definition of the upstream servers this config knows: the one a
	// session starts on and every one it can be moved to with /server. File-only (no flag/env),
	// like mcp-servers: the list describes machines, not this invocation. Absent/empty ⇒ no server
	// is configured at all (see ServerEntry for what an entry carries).
	Servers []ServerEntry `yaml:"servers"`
	// Server names which entry of the list above a session STARTS on — the last one chosen, which
	// /server records here automatically after a switch. Unlike the list, it has both an env var
	// (`APOGEE_SERVER`) and a flag (`--server`): the list describes machines, and this says which
	// of them this run is on, which is an invocation fact. Absent/empty ⇒ no choice is recorded.
	// A name no entry carries is not refused here — which names exist is what the list says, so
	// selection answers it.
	Server string `yaml:"server"`
	// Editor names the command an external edit is opened with — the ⏎ jump the /settings pane makes
	// on a key no field can hold, and any other edit of this file. A top-level scalar beside Server
	// above, file-only (no flag/env), carried verbatim: `editor: code -w` is split
	// into words at the launch site, so flags travel with the program. Absent/empty ⇒ the rest of the
	// ladder decides — $VISUAL, then $EDITOR, then the OS default opener (ADR 0041). It is free text
	// with no validate hook, like present.command: which programs this machine has is not a fact this
	// process can check from a string, and a name that is not there is answered at launch.
	Editor string `yaml:"editor"`
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
	UnconfinedHosts []UnconfinedHost `yaml:"unconfined-hosts"`
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
	// AutoTitle gates the AUTOMATIC session-naming call: on the first prompt of a new session an
	// out-of-band completion names the Session record from that prompt, applied through the same
	// Rename path the session browser uses. File-only (no flag/env), and a pointer so an explicit
	// `auto-title: false` is distinguishable from an absent key (default true). Unlike auto-compact
	// this one is cosmetic — with it off the heuristic title stands and `/rename` still regenerates
	// on demand, because the key gates only the automatic firing, not the generator.
	AutoTitle *bool `yaml:"auto-title"`
	// RememberModel gates BOTH halves of remembering a model choice: the WRITE apogee makes back
	// into THIS file when the user picks a model explicitly — the picked id into the session's
	// `servers:` entry's `model:` on a plain server, the loaded profile's name into that entry's
	// `launch-profile:` on a launcher-fronted one — and the TUI's startup RESTORE of what was
	// recorded. File-only (no flag/env), and a pointer for the reason auto-title is one; the default
	// is the other way round though: absent ⇒ OFF, because writing a session's choices back into a
	// hand-written config is a thing to ask for rather than to discover. Only an explicit pick
	// records — a rebind the heartbeat merely observed, and the one-shot `--model`/`APOGEE_MODEL`
	// overrides, never do.
	RememberModel *bool `yaml:"remember-model"`
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
	// Tools is the roster block: today its one key is `disabled:`, the built-in tools this config
	// takes off the menu. A pointer so an absent block falls through to the default (every tool)
	// rather than reading as an explicit empty one.
	Tools *toolsConfig `yaml:"tools"`
	// ModelProfiles describes how a model speaks the wire (CONTEXT: Model profile) — its tool-call
	// format and inline thinking-channel style — keyed by a PATTERN the model name contains
	// (ADR 0044). File-only, no flag/env, like mcp-servers. Absent/empty ⇒ nothing the user
	// configured matches any model, so resolution falls back to apogee's shipped shape table and,
	// failing that, to the zero profile (native tool calls, no inline thinking). A matching entry
	// replaces the WHOLE profile, both axes, and outranks every shipped entry.
	//
	// The retired GLOBAL `model-profile:` block this replaces is refused at startup with the map
	// spelling to paste (configmigrate.go): a profile is per-model now, so a config that still
	// spells it must be told rather than silently unread.
	ModelProfiles map[string]modelProfileConfig `yaml:"model-profiles"`
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
	// flag/env), like the blocks above. It stays a raw string here — ApplyConfig parses it once, so
	// an unknown name reaches startup as an error rather than being quietly dropped at the yaml
	// seam (the `ui.spinner` posture).
	CursorShape string `yaml:"cursor-shape"`
	// UI configures how the terminal UI presents itself — today the status-line spinner's animation,
	// its colour loop, and whether the transcript is painted with a scroll bar. File-only (no
	// flag/env), like the blocks above. Absent ⇒ the renderer's default style with the colour loop on
	// and the scroll bar shown. A pointer so an absent block falls through to those defaults rather
	// than being an explicit zero setting, which would read as `spinner-color: false`.
	UI *uiConfig `yaml:"ui"`
}

// UnconfinedHost is one Host acknowledgement (CONTEXT: Host acknowledgement): the user's
// recorded claim that ONE named machine is disposable. ID is what platform.HostID() is
// matched against — the safety interlock that stops an acknowledgement travelling between
// machines unnoticed, NOT authentication: anyone who can edit the config can write any id.
// Acknowledged (a free-form date) and Note are for the human reading the file back months
// later; nothing resolves off them, so neither is required.
type UnconfinedHost struct {
	ID           string `yaml:"id"`
	Acknowledged string `yaml:"acknowledged"`
	Note         string `yaml:"note"`
}

// ServerEntry is one named upstream server (`servers:` in config.yaml): an endpoint this session
// can be moved to, plus what that server needs in order to be talked to. It is ONE type on disk
// and resolved (the UnconfinedHost posture) because there is nothing to map across — every field
// travels to the composition root exactly as the user wrote it.
//
// Name does three jobs with one value: it labels the entry for the user, it is the name the
// session is switched by, and it becomes the footer's host alias once the session is on that
// server — which is why it is required and must be unique (ADR 0036 decision 1: the alias of the
// server you are on is the name you call it). Endpoint is required for the obvious reason.
//
// APIKey and Model are optional. An entry naming no key source at all sends no Authorization
// header, the keyless local-server default; an empty model leaves that server's discovery hint
// unset, so whatever it serves is bound. APIKey is FILE-ONLY on purpose: APOGEE_API_KEY is a
// single value and it overlays the key of the ONE entry this run starts on (ADR 0036 decision 6),
// so every other entry carries its own key here rather than borrowing that one. They carry
// `omitempty` because this type is also RENDERED into a config file — the legacy migration writes
// an entry through the marshaller (configmigrate.go) — and an optional field the user never set
// must not come back as an empty line in their file.
//
// APIKeyCmd and APIKeyEnv are the other two KEY SOURCES that same token can come from, so a
// server's key need not live in this file at all: `api-key-cmd:` is a command whose standard output
// IS the key (`pass show …`, `op read …`, `security find-generic-password …`), and `api-key-env:`
// is the NAME of an environment variable holding it. An entry names at most ONE of the three, and
// ValidateServers refuses a second on the duplicate-name reasoning: two sources for one value is a
// defect in the file rather than a precedence question, and any ranking that invited — literal over
// command, command over variable — would leave a key that is set, read by nobody, and silently
// ignored. Naming none is the keyless state, which is exactly why an empty ANSWER is never read as
// one: a command that exits non-zero, times out or prints nothing, and a variable that is unset or
// empty, are HARD ERRORS carrying this entry's name — a server the user pointed at a key for must
// not quietly go on to talk to it unauthenticated.
//
// Nothing here is resolved at load. A source runs at FIRST USE of that entry's key — the startup
// bind, a `/server` switch onto it, a delegation spawned against it, the probe or heartbeat built
// for it — and the answer is cached for the session, so a server nobody moves onto never runs its
// command and ValidateServers itself stays offline (the endpoint-probe reasoning: validation asks
// the file, never the machine). The command is split into argv and executed DIRECTLY, with no shell
// and no stdin — the `editor:` idiom — so a pipeline needs a wrapper script of the user's own, and
// a backend that must ask the human to unlock has to prompt through a GUI agent (pinentry-mac, the
// Keychain dialog) rather than through this terminal.
//
// PlaintextKeyOK is the answer "never, for this entry" to the startup offer that moves a literal
// `api-key:` into the OS secret store: apogee raises that offer once per run for every entry
// carrying a plaintext key, and this marker is what silences it here for good — consent recorded in
// the file, at ADR 0035's deliberate-edit grain. It is legal ONLY beside a literal `api-key:` and
// refused anywhere else: an entry whose key comes from a command or a variable, or from nowhere at
// all, has no plaintext key to be offered anything about, so the marker there would read as
// configured while doing nothing.
//
// LlamaLauncher is ADR 0029 decision 4's `llama-launcher:` key moved onto the entry it actually
// describes (2026-08-07): a launcher fronts ONE server, so a global key captured `/model` on every
// server of a multi-server config — including remote ones like OpenRouter, whose advertised-model
// discovery it overrode. Per entry, the integration is on only while the session is on THIS server.
// Three shapes, and no fourth:
//
//   - absent ⇒ off for this server (there is no `off` spelling; leaving the key out IS the off
//     state, and two spellings of one state invite drift).
//   - `auto` (any casing, surrounding space ignored) ⇒ the launcher's own default config path,
//     taken verbatim: `auto` is an explicit opt-in, so a machine without that file degrades at the
//     first verb naming the path rather than silently having no local-server verbs.
//   - anything else ⇒ that config file's path, `~` expanded.
//
// The value travels to the composition root exactly as written — only the root knows the launcher,
// so only the root can resolve `auto` (cmd/apogee/launcher.go's entryLauncherPath).
//
// ParallelAgents is ADR 0039 decision 2's cap on how many sub-agents this server may run at once,
// per entry because the width is a property of the SERVER (its slots), not of the session. It is
// the `context-window:` idiom — a PIN, never a preference discovery may overrule — and has three
// states, not four:
//
//   - absent (or 0, which yaml cannot tell from absent) ⇒ discover: the live server's `/props`
//     `total_slots`, and 1 — today's strictly serial behaviour — when it advertises nothing.
//   - N ≥ 1 ⇒ pin N, whatever the server says.
//   - negative ⇒ refused by ValidateServers; there is no meaning to give it.
//
// The trade the number buys is the server operator's and worth saying out loud: more parallel
// agents means a smaller context window each, since `--parallel N` splits one window into N slots
// (ADR 0024 — Apogee's numbers are per-slot-honest either way).
//
// SubAgents is ADR 0045 decision 1's routing flag: the ONE entry spelling `sub-agents: true` is the
// Sub-agent server, and every delegation at every depth runs against it rather than against the
// parent's upstream. Absent is today's behaviour — a child shares the parent's server — and two
// flagged entries are refused by ValidateServers on the duplicate-name reasoning: a delegation
// routes to ONE server, so a second flag is a defect in the file rather than a preference between
// two entries.
//
// Bypass and Mechanisms are that entry's POSTURE (ADR 0045 decision 2), in the top-level keys'
// shapes verbatim: "delegations to this server run with this". A present key replaces the value the
// child would otherwise have inherited WHOLE — a present `mechanisms:` map is the child's entire
// catalogue, with no per-ID merge, which would need an `inherit` spelling to be readable — and an
// absent one inherits the parent's LIVE value at spawn, exactly today's rule. Bypass is a pointer
// for the reason the top-level key is one: an explicit `bypass: false` is a posture, not an absent
// key. Both are refused on an UNflagged entry, where they would describe delegations that never
// arrive; posture rides the ROUTING, so where the parent itself happens to be running is irrelevant
// to what its children run as.
//
// ContextWindow PINS this server's per-slot context window in tokens — the top-level
// `context-window:` key, per entry, and the same three states `parallel-agents` has: absent (or 0,
// which yaml cannot tell from absent) ⇒ whatever the heartbeat observes stands, N ≥ 1 ⇒ that number
// whatever the server advertises, negative ⇒ refused by ValidateServers. It is legal on ANY entry
// because it describes the server, the way `model:` does, and three seams read it: the Delegation
// target it was added for (ADR 0045 decision 3), a session's own `/server` switch, and the BIND that
// puts a session on a server in the first place — the last two carrying the entry's window onto the
// engine beside the entry's reply cap (ResolveContextWindow, ADR 0046's same "the bound is a
// property of the slot" reasoning). It earns its keep in all three: a cloud endpoint advertises no
// window at all, so the pin is how such a server is usable at all — as a Sub-agent server, as one a
// session moves onto, or as the one a session starts on.
//
// MaxOutputTokens PINS the ceiling on ONE reply from this server, in tokens — the number the engine
// states on the wire so a reply stops at a bound it chose rather than at the server's context wall
// (ADR 0046). It is the `context-window:` idiom verbatim, with the same three states and no fourth:
//
//   - absent (or 0, which yaml cannot tell from absent) ⇒ the engine derives the cap from the reply
//     room the Budget already reserves out of the window.
//   - N ≥ 1 ⇒ cap every reply at N tokens, whatever the window says.
//   - negative ⇒ refused by ValidateServers; there is no meaning to give it.
//
// It rides the entry for the reason the window it is derived from does: the ceiling is a property of
// the SLOT, not of the session, so a session moved to another server takes that server's number. And
// it earns its keep exactly where the derivation cannot reach — a cloud endpoint advertises no
// window at all, so the reserve it would derive from is unknown and the cap falls to its clamp
// floor; the pin is the only way to let such a server answer at length.
//
// LaunchProfile names the llama-launcher Launch profile the interactive TUI comes back on when it
// starts on this entry and nothing is serving there yet. apogee writes it itself on a profile-load
// commit while `remember-model:` is on — which is what makes "pick a model, come back on it
// tomorrow" true of a launcher-fronted server — and it is as hand-settable as any other key here,
// so a config can arrive with the choice already made. It rides an entry that carries
// `llama-launcher:`, because the profile is loaded THROUGH that launcher and an entry with none has
// nothing to actuate it (ValidateServers refuses that pairing). A plain multi-model server records
// its choice in `model:` instead: same feature, the key each server class already has for it.
// Whether the named profile still EXISTS is not asked here — the launcher's config is read fresh at
// use time (ADR 0029 D4), so a profile renamed since is a note at startup, not a refusal to start.
type ServerEntry struct {
	Name            string          `yaml:"name"`
	Endpoint        string          `yaml:"endpoint"`
	APIKey          string          `yaml:"api-key,omitempty"`
	APIKeyCmd       string          `yaml:"api-key-cmd,omitempty"`
	APIKeyEnv       string          `yaml:"api-key-env,omitempty"`
	PlaintextKeyOK  bool            `yaml:"plaintext-key-ok,omitempty"`
	Model           string          `yaml:"model,omitempty"`
	LlamaLauncher   string          `yaml:"llama-launcher,omitempty"`
	LaunchProfile   string          `yaml:"launch-profile,omitempty"`
	ParallelAgents  int             `yaml:"parallel-agents,omitempty"`
	SubAgents       bool            `yaml:"sub-agents,omitempty"`
	Bypass          *bool           `yaml:"bypass,omitempty"`
	Mechanisms      map[string]bool `yaml:"mechanisms,omitempty"`
	ContextWindow   int             `yaml:"context-window,omitempty"`
	MaxOutputTokens int             `yaml:"max-output-tokens,omitempty"`
}

// ValidateServers rejects an entry that could never be switched to, at the startup boundary where
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
//
// The entry's KEY SOURCE carries three defects of its own. Setting more than one of `api-key:`,
// `api-key-cmd:` and `api-key-env:` is refused with every set key named, on the duplicate-name
// reasoning again: one key comes from ONE place, so a second source is a defect in the file rather
// than a precedence to resolve. A whitespace-only `api-key-cmd:` or `api-key-env:` is refused on
// the `llama-launcher:` reasoning — it reads as configured while naming nothing, and the file's
// spelling for "no key source" is leaving all three keys out. And `plaintext-key-ok: true` is
// refused on an entry carrying no literal `api-key:`, because that marker's whole job is to silence
// the offer to migrate a plaintext key such an entry does not have. What is deliberately NOT done
// is running the command or reading the variable: the key is a first-use question — this run may
// never move onto that entry — and validation stays offline for the reason it never probes an
// endpoint.
//
// The entry's optional `llama-launcher:` value is checked on the same footing, and for the same
// three defects the retired top-level key was checked for: a value that is only whitespace reads
// as configured but names nothing; a value carrying a URL scheme is the one confusion this key
// invites (a launcher on another machine is an `mcp-servers:` entry, not a local file path); and
// `off` is refused because absent is already the off state, so accepting a second spelling of it
// would let two files mean the same thing differently. Whether the named file EXISTS is
// deliberately not asked — a launcher config is a property of the machine rather than of the
// config that travels between machines, so a missing one degrades at the verb that wants it.
//
// The entry's optional `launch-profile:` pointer carries two defects of its own, on that same
// reasoning. A value that is only whitespace reads as configured while naming no profile. And a
// pointer on an entry with no `llama-launcher:` key is refused because nothing there could ever
// actuate it: the profile is loaded through the launcher, so the pair is what makes the key mean
// anything, and a lone pointer is a defect in the file rather than a preference to honour silently.
// Whether the named profile EXISTS is deliberately not asked, for the reason the launcher config's
// own existence is not: the launcher's file is a property of the machine and is read fresh at use
// time, so a profile that has since been renamed is answered by the startup restore with a note.
//
// The entry's optional `parallel-agents:` value is checked for the one defect it can carry: a
// negative cap. Absent and 0 are the same state (discover — yaml cannot distinguish them) and any
// N ≥ 1 is a pin, so a negative number is the only value with nothing to mean, and saying so here
// beats resolving it to a silent 1 months later. The entry's optional `context-window:` pin is
// checked for that same one defect, on that same reasoning, and its `max-output-tokens:` cap for
// that same one again — three keys carrying one idiom carry one refusal.
//
// The entry's optional `sub-agents:` flag and the posture keys that ride it carry two defects
// between them (ADR 0045 decisions 1 and 2). A SECOND flagged entry is refused with BOTH entries
// named, because the fix is a choice between two entries the file already spells: delegations route
// to one server, so a second flag is the duplicate-name defect wearing another key. And
// `bypass:`/`mechanisms:` on an entry the flag is absent from is refused because they would describe
// delegations that never route there — the posture rides the flag, and a key with nothing to govern
// reads as configured while doing nothing.
func ValidateServers(servers []ServerEntry) error {
	seen := make(map[string]struct{}, len(servers))
	flagged := -1
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
				"OpenAI-compatible URL, for example http://127.0.0.1:1111", i+1, s.Name)
		}
		// Absent is not a defect — it is the off state — so every refusal below is about a value
		// the user did write.
		if set := keySourceKeys(s); len(set) > 1 {
			return fmt.Errorf("apogee: servers: entry %d (%q): sets %s — an entry takes its key from ONE "+
				"source, so keep the one that should answer for this server and remove the rest", i+1, s.Name,
				joinAnd(set))
		}
		if s.APIKeyCmd != "" && strings.TrimSpace(s.APIKeyCmd) == "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): api-key-cmd: is only whitespace — give the "+
				"command whose output IS the key, for example security find-generic-password -s apogee -a %s "+
				"-w, or remove the key to send no Authorization header at all", i+1, s.Name, s.Name)
		}
		if s.APIKeyEnv != "" && strings.TrimSpace(s.APIKeyEnv) == "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): api-key-env: is only whitespace — name the "+
				"environment variable the key is in, for example OPENROUTER_API_KEY, or remove the key to "+
				"send no Authorization header at all", i+1, s.Name)
		}
		if s.PlaintextKeyOK && strings.TrimSpace(s.APIKey) == "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): plaintext-key-ok: true without an api-key: — "+
				"the marker only silences the offer to move a PLAINTEXT key into this machine's secret store, "+
				"so on an entry whose key comes from a command, a variable, or nowhere it says nothing; "+
				"remove it", i+1, s.Name)
		}
		if launcher := s.LlamaLauncher; launcher != "" {
			trimmed := strings.TrimSpace(launcher)
			switch {
			case trimmed == "":
				return fmt.Errorf("apogee: servers: entry %d (%q): llama-launcher: is only whitespace — "+
					"set auto to use the launcher's own config, give the path of a llama-launcher config "+
					"file, or remove the key to disable the launcher for this server", i+1, s.Name)
			case strings.EqualFold(trimmed, "off"):
				return fmt.Errorf("apogee: servers: entry %d (%q): llama-launcher: off is not a value — "+
					"remove the key to disable the launcher for this server", i+1, s.Name)
			case strings.Contains(launcher, "://"):
				return fmt.Errorf("apogee: servers: entry %d (%q): llama-launcher: %q looks like a URL — "+
					"this key takes auto or the path of a LOCAL llama-launcher config file; a launcher on "+
					"another machine is reached as an mcp-servers: entry instead", i+1, s.Name, launcher)
			}
		}
		if profile := s.LaunchProfile; profile != "" {
			switch {
			case strings.TrimSpace(profile) == "":
				return fmt.Errorf("apogee: servers: entry %d (%q): launch-profile: is only whitespace — name "+
					"the llama-launcher launch profile this server should come back on, or remove the key to "+
					"start on whatever the server already has loaded", i+1, s.Name)
			case strings.TrimSpace(s.LlamaLauncher) == "":
				return fmt.Errorf("apogee: servers: entry %d (%q): launch-profile: %q without a "+
					"llama-launcher: key — a launch profile is loaded THROUGH the launcher, so on an entry "+
					"apogee cannot launch there is nothing to actuate it; add llama-launcher: auto, or name "+
					"the model with model: instead", i+1, s.Name, profile)
			}
		}
		if s.ParallelAgents < 0 {
			return fmt.Errorf("apogee: servers: entry %d (%q): parallel-agents: %d is negative — give the "+
				"number of sub-agents this server may run at once (1 or more), or remove the key to take "+
				"the server's own slot count", i+1, s.Name, s.ParallelAgents)
		}
		if s.ContextWindow < 0 {
			return fmt.Errorf("apogee: servers: entry %d (%q): context-window: %d is negative — give the "+
				"context window this server serves, in tokens (1 or more), or remove the key to take the "+
				"window the server advertises", i+1, s.Name, s.ContextWindow)
		}
		if s.MaxOutputTokens < 0 {
			return fmt.Errorf("apogee: servers: entry %d (%q): max-output-tokens: %d is negative — give the "+
				"most tokens one reply from this server may be (1 or more), or remove the key to derive the "+
				"cap from the reply budget", i+1, s.Name, s.MaxOutputTokens)
		}
		if s.SubAgents {
			if flagged >= 0 {
				return fmt.Errorf("apogee: servers: entry %d (%q): sub-agents: true, but entry %d (%q) is "+
					"already flagged — delegations route to ONE server, so flag the entry that should take "+
					"them and remove the flag from the other", i+1, s.Name, flagged+1, servers[flagged].Name)
			}
			flagged = i
		} else if keys := posturedKeys(s); keys != "" {
			return fmt.Errorf("apogee: servers: entry %d (%q): %s without sub-agents: true — those keys say "+
				"what DELEGATIONS to this server run as, so they ride the sub-agents: flag; add "+
				"sub-agents: true to route delegations here, or remove the keys", i+1, s.Name, keys)
		}
	}
	return nil
}

// posturedKeys names the sub-agent posture keys this entry carries, in file order, for the refusal
// that reports them — and is empty when it carries neither, which is what makes it the condition of
// that refusal too. A present-but-empty `mechanisms: {}` counts as written: an empty catalogue is a
// posture (run the child with no Mechanisms at all), and only an absent key inherits.
func posturedKeys(s ServerEntry) string {
	switch {
	case s.Bypass != nil && s.Mechanisms != nil:
		return "bypass: and mechanisms:"
	case s.Bypass != nil:
		return "bypass:"
	case s.Mechanisms != nil:
		return "mechanisms:"
	}
	return ""
}

// keySourceKeys names the key-source keys this entry sets, in file order: `api-key:`,
// `api-key-cmd:`, `api-key-env:`. Exactly one is the ordinary case and none is the keyless one, so
// the list is really only read for the refusal a SECOND name in it triggers — it is both that
// refusal's condition and the message's list of what the user has to choose between. A value that
// is only whitespace counts as set: the user wrote it, so it is a source they meant, and its own
// emptiness is the next refusal's business rather than a reason to overlook the pair.
func keySourceKeys(s ServerEntry) []string {
	set := make([]string, 0, 3)
	if s.APIKey != "" {
		set = append(set, "api-key:")
	}
	if s.APIKeyCmd != "" {
		set = append(set, "api-key-cmd:")
	}
	if s.APIKeyEnv != "" {
		set = append(set, "api-key-env:")
	}
	return set
}

// joinAnd writes a short list the way a refusal reads it out loud: "a and b", "a, b and c". Fewer
// than two names is the degenerate case its only caller never asks for, and returns the names
// themselves unadorned.
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// SubAgentServer answers the one question ADR 0045 decision 1 asks of a resolved `servers:` list:
// which entry, if any, takes the delegations. ValidateServers has already refused a second flagged
// entry by the time anything calls this, so the first match is THE match — and false means the
// flag is absent from the whole list, which is not a defect but today's behaviour: children share
// the parent's upstream, no second monitor runs, and nothing latches.
func SubAgentServer(entries []ServerEntry) (ServerEntry, bool) {
	for _, e := range entries {
		if e.SubAgents {
			return e, true
		}
	}
	return ServerEntry{}, false
}

// ResolveParallelAgents answers the one question ADR 0039 decision 2 asks about a bound server: how
// many sub-agents may run against it at once. pinned is that server entry's `parallel-agents:` value
// (0 when the key is absent, which yaml cannot tell from an explicit 0) and discovered is what the
// live server's `/props` reported as `total_slots` (0 when nothing was observed, or nothing asked).
//
// It is the `context-window:` idiom, and the ranks are the whole of the decision: a PIN is never
// overruled by discovery, discovery answers when nothing is pinned, and 1 — strictly serial, exactly
// today's behaviour — is what a session falls back to when neither can say. There is no fourth
// answer and deliberately no "0 means unlimited": a width nobody bounded is a width that outruns the
// server's slots, and the honest floor for an unknown server is one agent at a time.
//
// Both inputs are guarded rather than trusted: ValidateServers already refuses a negative pin at
// startup, and a server is free to advertise nonsense, so anything below 1 simply falls through to
// the next rank.
func ResolveParallelAgents(pinned, discovered int) int {
	if pinned >= 1 {
		return pinned
	}
	if discovered >= 1 {
		return discovered
	}
	return 1
}

// ResolveContextWindow answers the same question one key over: which context window a session
// running on a `servers:` entry measures its Budget against. entry is that entry's own
// `context-window:` value (ADR 0045 — 0 when the key is absent, which yaml cannot tell from an
// explicit 0) and session is the top-level `context-window:` key the whole run carries (ADR 0024
// decision 9), which a `/settings` edit can move mid-session.
//
// The ranks are ResolveParallelAgents's, one scope further out: the SPECIFIC pin wins, because a
// number written on the entry describes THAT server's slot while the top-level key describes
// whatever server the session happens to be pointed at — so a session moved onto an entry that pins
// its own window budgets against the server it is actually on. Neither pinned ⇒ 0, which is not a
// window but the honest "nobody said": the heartbeat's own observation binds it at the next Rebind,
// and until then the Budget stays inactive exactly as it does before a session's first beat.
//
// Discovery is deliberately NOT a rank here, unlike the width above: what a beat observed is held by
// the caller that beats (the composition root's live settings), and folding it in would give this
// function a third input only one of its callers could ever supply.
//
// Both inputs are guarded rather than trusted — ValidateServers refuses a negative entry pin at
// startup, and the registry refuses a negative top-level one — so anything below 1 falls through to
// the next rank.
func ResolveContextWindow(entry, session int) int {
	if entry >= 1 {
		return entry
	}
	if session >= 1 {
		return session
	}
	return 0
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
// PresentSettings with yaml tags; toPresentSettings maps it across so the on-disk shape and the
// resolved value stay independently evolvable (as mcpServerConfig does for mcp.ServerConfig).
type presentConfig struct {
	// AutoOpen is a pointer so an explicit `auto-open: false` is distinguishable from an absent
	// key (which keeps the default true).
	AutoOpen *bool `yaml:"auto-open"`
	// Command is the opener template with {path} where the document goes; empty ⇒ the OS default.
	Command string `yaml:"command"`
	// Port is the doc server's TCP port; 0 (the default) takes an ephemeral one.
	Port int `yaml:"port"`
	// Host is the address served URLs advertise; empty ⇒ detected (see PresentSettings.host).
	Host string `yaml:"host"`
}

// toPresentSettings maps the on-disk present block onto the resolved value, applying the
// auto-open default (true) when the key is absent. A block that sets one key therefore leaves the
// other three at their defaults, which is what makes it usable a line at a time.
func (p presentConfig) toPresentSettings() PresentSettings {
	s := PresentSettings{AutoOpen: true, Command: p.Command, Port: p.Port, Host: p.Host}
	if p.AutoOpen != nil {
		s.AutoOpen = *p.AutoOpen
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
// SystemPromptSettings.Validate's to name.
func (fc fileConfig) toSystemPromptSettings() SystemPromptSettings {
	s := SystemPromptSettings{Global: PromptSource{Text: fc.SystemPromptText, File: fc.SystemPromptFile}}
	if len(fc.SystemPromptModels) > 0 {
		s.Models = make(map[string]PromptSource, len(fc.SystemPromptModels))
		for model, e := range fc.SystemPromptModels {
			s.Models[model] = PromptSource{Text: e.Text, File: e.File}
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

// uiConfig is the on-disk schema for the `ui:` block. It mirrors UISettings with yaml tags;
// toUISettings maps it across so the on-disk shape and the resolved value stay independently
// evolvable (as presentConfig does for PresentSettings).
type uiConfig struct {
	// Spinner names the status-line animation — snake | glitter | classic. Empty ⇒ the default.
	// It stays a raw string here: UISettings.Validate parses it once, so an unknown name reaches
	// startup as an error rather than being quietly dropped at the yaml seam.
	Spinner string `yaml:"spinner"`
	// SpinnerColor gates the colour loop over whichever style Spinner names — an INDEPENDENT key,
	// not a property of a style. A pointer so an explicit `spinner-color: false` is distinguishable
	// from an absent key (which keeps the default true).
	SpinnerColor *bool `yaml:"spinner-color"`
	// ShowScrollbar gates the scroll bar — in the transcript and in every popup pane alike — and the
	// column it hangs in. A pointer so an explicit `show-scrollbar: false` is distinguishable from an
	// absent key (which keeps the default true).
	ShowScrollbar *bool `yaml:"show-scrollbar"`
	// ColorScheme names the palette the screen is drawn in — a built-in or a file in
	// `<apogee-home>/schemes/`. Empty ⇒ the default scheme. A plain string with no pointer and no
	// validation at this seam: every name is admissible on disk because an unresolvable one is
	// answered with a warning and the default palette rather than a startup error (ADR 0040).
	ColorScheme string `yaml:"color-scheme"`
	// StallAfter is how long the engine may go silent before the status line reports the quiet — a
	// length of time as `time.ParseDuration` spells it (`90s`, `2m`), or `0` to turn the suffix off.
	// A pointer for ShowScrollbar's reason turned inside out: here it is the explicit `0` — the
	// documented spelling of "off" — that must be distinguishable from an absent key, which keeps
	// the 90s default. It stays a raw string at this seam because the parse can FAIL, and a ui block
	// is refused in one place (UISettings.Validate), never at the yaml boundary.
	StallAfter *string `yaml:"stall-after"`
}

// toUISettings maps the on-disk ui block onto the resolved value, applying the defaults for the keys
// the block leaves out. A block that sets one key therefore leaves the others at their defaults,
// which is what keeps the axes independent from the on-disk shape onward: naming a style does not
// turn the colour loop off, turning the loop off does not change the style, and neither says
// anything about the scroll bar.
func (u uiConfig) toUISettings() UISettings {
	s := defaultUISettings()
	if u.Spinner != "" {
		s.Spinner = tui.SpinnerStyle(u.Spinner) // validated by UISettings.Validate, not here
	}
	if u.SpinnerColor != nil {
		s.SpinnerColor = *u.SpinnerColor
	}
	if u.ShowScrollbar != nil {
		s.ShowScrollbar = *u.ShowScrollbar
	}
	if u.ColorScheme != "" {
		s.ColorScheme = u.ColorScheme // resolved against the schemes folder by wire.go, not here
	}
	if u.StallAfter != nil {
		// An empty value reads as an absent key, the posture the two string keys above take: what the
		// pointer buys is telling an explicit `0` — the documented spelling of "off" — from a block
		// that never named the key. Text no duration can be made of is carried AS WRITTEN for
		// UISettings.Validate to refuse and to quote; this seam judges nothing.
		if text := strings.TrimSpace(*u.StallAfter); text != "" {
			if after, err := time.ParseDuration(text); err == nil {
				s.StallAfter = after
			} else {
				s.unparsedStallAfter = text
			}
		}
	}
	return s
}

// toolsConfig is the on-disk `tools:` block — the tool roster this config runs with. Its one key
// is the switch: the built-in tools to leave OFF the menu, by name.
//
// It is a BLOCK rather than a top-level `disabled-tools:` list because the roster is a subject
// that will grow (per-profile rosters build on this key), and a block is where the next key of the
// same subject goes without a second top-level name that means almost the same thing.
type toolsConfig struct {
	// Disabled names the built-in tools this config takes off the menu, so the model is neither
	// offered them nor able to call them. Absent/empty ⇒ every tool, the default. A name matching
	// no tool is a warning at startup, never an error: a roster the user is pruning must not be
	// able to stop a session from starting.
	Disabled []string `yaml:"disabled"`
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
// mirrors domain.ModelProfile with yaml tags; toModelProfile maps it across so the on-disk shape
// and the value type stay independently evolvable (as mcpServerConfig does for mcp.ServerConfig).
type modelProfileConfig struct {
	ToolCallFormat  string         `yaml:"tool-call-format"`
	ToolCallPattern string         `yaml:"tool-call-pattern"`
	Thinking        thinkingConfig `yaml:"thinking"`
}

// thinkingConfig is the on-disk schema for a model's inline thinking channel (part of the model
// profile). It mirrors domain.ThinkingProfile with yaml tags.
type thinkingConfig struct {
	Style  string `yaml:"style"`
	Start  string `yaml:"start"`
	End    string `yaml:"end"`
	Effort string `yaml:"effort"`
}

// toModelProfile maps the on-disk model-profile schema onto the domain.ModelProfile value the
// loop translates to its parsers at the seam. An empty tool-call-format / thinking style resolves
// to the native, no-inline-thinking default downstream.
func (p modelProfileConfig) toModelProfile() domain.ModelProfile {
	return domain.ModelProfile{
		ToolCallFormat: domain.ToolCallFormat(p.ToolCallFormat),
		Pattern:        p.ToolCallPattern,
		Thinking: domain.ThinkingProfile{
			Style:  domain.ThinkingStyle(p.Thinking.Style),
			Start:  p.Thinking.Start,
			End:    p.Thinking.End,
			Effort: domain.ThinkingEffort(p.Thinking.Effort),
		},
	}
}

// validateModelProfiles rejects a `model-profiles:` map naming a thinking effort outside the four
// levels the wire mapping knows (ADR 0050). It runs at LOAD, before the layer is built, because a
// typo'd `effort:` would otherwise reach the wire as an unmapped value and quietly emit nothing —
// the one failure the user cannot see from the outside, since a model that ignores an effort dial
// and a model that was never sent one produce the same reply.
//
// The patterns are walked in sorted order so a file with two bad entries reports the same one on
// every run (the reason toProfileEntries sorts too).
func validateModelProfiles(m map[string]modelProfileConfig) error {
	for _, pattern := range slices.Sorted(maps.Keys(m)) {
		effort := domain.ThinkingEffort(m[pattern].Thinking.Effort)
		if !effort.Valid() {
			return fmt.Errorf("apogee: invalid model-profiles.%s.thinking.effort %q: want off, low, "+
				"medium, or high, or leave the key out for the model's own default", pattern, string(effort))
		}
	}
	return nil
}

// toProfileEntries projects the on-disk `model-profiles:` map onto the ordered entry list the
// composition root matches a model name against (ADR 0044). The map is sorted BY PATTERN because a
// Go map has no order and three surfaces read this slice — the /settings row's diff, the resolution
// itself, and any test pinning it — so an unordered projection would report a change the user did
// not make and pick a different winner between two runs of the same file.
//
// Order is a determinism property only: profiles.Resolve ranks by pattern length and breaks ties
// lexicographically, so the winner is the same whatever order the entries arrive in.
func toProfileEntries(m map[string]modelProfileConfig) []profiles.Entry {
	patterns := slices.Sorted(maps.Keys(m))

	entries := make([]profiles.Entry, 0, len(patterns))
	for _, pattern := range patterns {
		entries = append(entries, profiles.Entry{Pattern: pattern, Profile: m[pattern].toModelProfile()})
	}
	return entries
}

// layer projects a parsed file config onto a precedence layer: a present (non-empty)
// field becomes an explicit setting, an absent one stays nil to fall through.
func (fc fileConfig) layer() Layer {
	var l Layer
	if fc.Mode != "" {
		l.Mode = &fc.Mode
	}
	if fc.Bypass != nil {
		l.Bypass = fc.Bypass
	}
	if fc.ConfineToWorkspace != nil {
		l.ConfineToWorkspace = fc.ConfineToWorkspace
	}
	if len(fc.UnconfinedHosts) > 0 {
		l.UnconfinedHosts = fc.UnconfinedHosts
	}
	if len(fc.Servers) > 0 {
		l.Servers = fc.Servers
	}
	if fc.Server != "" {
		l.StartupServer = &fc.Server
	}
	if fc.Editor != "" {
		l.Editor = &fc.Editor
	}
	if fc.WebSearch != "" {
		l.WebSearchEndpoint = &fc.WebSearch
	}
	if fc.UseProjectSkills != nil {
		l.UseProjectSkills = fc.UseProjectSkills
	}
	if fc.AutoCompact != nil {
		l.AutoCompact = fc.AutoCompact
	}
	if fc.AutoTitle != nil {
		l.AutoTitle = fc.AutoTitle
	}
	if fc.RememberModel != nil {
		l.RememberModel = fc.RememberModel
	}
	if fc.ContextWindow > 0 {
		l.ContextWindow = &fc.ContextWindow
	}
	if len(fc.MCPServers) > 0 {
		servers := make([]mcp.ServerConfig, len(fc.MCPServers))
		for i, m := range fc.MCPServers {
			servers[i] = m.toServerConfig()
		}
		l.MCPServers = servers
	}
	if fc.Tools != nil && len(fc.Tools.Disabled) > 0 {
		l.ToolsDisabled = fc.Tools.Disabled
	}
	if len(fc.ModelProfiles) > 0 {
		l.ModelProfiles = toProfileEntries(fc.ModelProfiles)
	}
	if len(fc.Mechanisms) > 0 {
		l.Mechanisms = fc.Mechanisms
	}
	if fc.ValidatedSets != nil {
		l.ValidatedSetsEnable = fc.ValidatedSets.Enable
		if len(fc.ValidatedSets.Alias) > 0 {
			l.ValidatedSetsAlias = fc.ValidatedSets.Alias
		}
	}
	if fc.Present != nil {
		p := fc.Present.toPresentSettings()
		l.Present = &p
	}
	// Three top-level keys rather than a block, so the projection asks whether ANY of them is
	// present: one inline prompt, one file, or a per-model map alone all configure the subsystem.
	if fc.SystemPromptText != "" || fc.SystemPromptFile != "" || len(fc.SystemPromptModels) > 0 {
		sp := fc.toSystemPromptSettings()
		l.SystemPrompt = &sp
	}
	if fc.ContextFiles != nil {
		c := fc.ContextFiles.toContextFilesSettings()
		l.ContextFiles = &c
	}
	if fc.UI != nil {
		u := fc.UI.toUISettings()
		l.UI = &u
	}
	if fc.CursorShape != "" {
		l.CursorShape = &fc.CursorShape
	}
	return l
}

// LoadFileConfig reads and parses the config file, returning an empty layer when the
// file is absent (the common case — a config file is optional). A malformed file is a
// hard error: silently ignoring it would mask a typo'd setting. readFile is injected so
// the loader is testable without touching the filesystem.
//
// A config still written in the retired schema is migrated here, before the layer is built: the
// decoder ignores keys the struct no longer has, so without the sniff a working `endpoint:` would
// simply stop being read and the session would report no server configured, with nothing pointing
// at the four lines that ARE the configuration. This is the one place the loader can WRITE — the
// one-time fold of ADR 0036 decision 9, which rewrites path itself and announces the change through
// notify; a file already in the new schema is never touched, so the write happens at most once per
// config and the injected readFile stays the only reader on every other launch.
func LoadFileConfig(path string, readFile func(string) ([]byte, error), notify func(string)) (Layer, error) {
	if path == "" {
		return Layer{}, nil
	}
	data, err := readFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Layer{}, nil
		}
		return Layer{}, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	data, note, err := migrateLegacyConfig(path, data, time.Now())
	if err != nil {
		return Layer{}, err
	}
	if note != "" {
		notify(note)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return Layer{}, fmt.Errorf("apogee: parse config %q: %w", path, err)
	}
	if err := validateModelProfiles(fc.ModelProfiles); err != nil {
		return Layer{}, err
	}
	return fc.layer(), nil
}

// legacyFileConfig is the retired half of the schema — and ONLY that half: the top-level
// `endpoint:`/`api-key:`/`host-alias:`/`model:` quadruple ADR 0036 folded into the `servers:` list.
// It exists so the four keys can still be READ off a config that has them, which fileConfig can no
// longer do; a plain unmarshal ignores unknown keys, so a config apogee has quietly stopped
// understanding is indistinguishable from one that never said anything.
//
// Nothing resolves from it. Its only job is to answer "was this file written in the old schema,
// and what did it say" — the two facts the migration needs, both when it performs the one-time
// verified rewrite for the user (configmigrate.go) and when it has to refuse and spell the
// replacement out for them to paste.
type legacyFileConfig struct {
	Endpoint  string `yaml:"endpoint"`
	APIKey    string `yaml:"api-key"`
	HostAlias string `yaml:"host-alias"`
	Model     string `yaml:"model"`
}

// isEmpty reports whether the file carried none of the retired keys — the new-schema case, which
// is every config from here on.
func (lc legacyFileConfig) isEmpty() bool {
	return lc.Endpoint == "" && lc.APIKey == "" && lc.HostAlias == "" && lc.Model == ""
}

// name is what the entry these keys fold into is called: the old `host-alias:` if the config gave
// one — it did exactly this job for the startup endpoint — and otherwise the endpoint's own host,
// which is the alias the footer would have fallen back to anyway (hostFromEndpoint).
func (lc legacyFileConfig) name() string {
	if alias := strings.TrimSpace(lc.HostAlias); alias != "" {
		return alias
	}
	if host := hostFromEndpoint(lc.Endpoint); host != "" {
		return host
	}
	return "my-box"
}

// block renders the retired keys as the `servers:` entry and `server:` pointer that replace them —
// the paste-able answer that makes a refused migration a two-minute edit rather than a research
// task. The api key is echoed back because it is part of the configuration being moved; it came out
// of this same file and goes back into it.
func (lc legacyFileConfig) block() string {
	var b strings.Builder
	name := lc.name()
	b.WriteString("servers:\n")
	b.WriteString("  - name: " + name + "\n")
	b.WriteString("    endpoint: " + lc.Endpoint + "\n")
	if lc.APIKey != "" {
		b.WriteString("    api-key: " + lc.APIKey + "\n")
	}
	if lc.Model != "" {
		b.WriteString("    model: " + lc.Model + "\n")
	}
	b.WriteString("\nserver: " + name + "\n")
	return b.String()
}

// ----------------------------------------------------------------------------
// The environment and flag layers
// ----------------------------------------------------------------------------

// Environment variable names, prefixed APOGEE_ to namespace the process environment.
//
// They fall in three groups. EnvServer/EnvMode/EnvBypass are read through the registry rows their
// keys carry (multiSourceKeys). EnvConfig/EnvWorkspace are read by ApplyConfig directly, because
// they name the roots resolution itself runs in and so cannot be config keys. EnvEndpoint/
// EnvModel/EnvAPIKey are the raw startup overrides ADR 0036 DETACHED from the schema: they no
// longer describe config keys — they build or overlay the startup server entry — so they are
// named here and resolved by the startup-override resolver rather than by a layer.
const (
	EnvEndpoint  = "APOGEE_ENDPOINT"
	EnvServer    = "APOGEE_SERVER"
	EnvModel     = "APOGEE_MODEL"
	EnvMode      = "APOGEE_MODE"
	EnvBypass    = "APOGEE_BYPASS"
	EnvAPIKey    = "APOGEE_API_KEY"
	EnvConfig    = "APOGEE_CONFIG"
	EnvWorkspace = "APOGEE_WORKSPACE"
)

// envLayer reads the APOGEE_* variables into a precedence layer; an unset variable stays nil to
// fall through. Which variable carries which key is the registry's to say (multiSourceKeys), so
// this reads names out of the rows rather than repeating them: a key whose row names no variable
// has no environment source at all. The variables that name no config key — APOGEE_ENDPOINT,
// APOGEE_API_KEY, APOGEE_MODEL — are not read here at all: since ADR 0036 they override the
// startup SERVER rather than a file key, so they are resolved outside this loop. A
// set-but-unparseable APOGEE_BYPASS is a hard error rather than a silently-ignored boolean,
// reported with the name the row carries. getenv is injected so the layer is testable without
// mutating the process environment.
func envLayer(getenv func(string) string) (Layer, error) {
	var l Layer
	for _, k := range multiSourceKeys {
		if k.fromEnv == nil || k.row.EnvVar == "" {
			continue
		}
		v := getenv(k.row.EnvVar)
		if v == "" {
			continue
		}
		if err := k.fromEnv(&l, v); err != nil {
			return Layer{}, fmt.Errorf("apogee: invalid %s %q: %w", k.row.EnvVar, v, err)
		}
	}
	return l, nil
}

// flagLayer projects the parsed flags onto a precedence layer, including a field only when its
// flag was explicitly set (changed reports cobra's per-flag Changed). An unset flag carries its
// zero default, which must not shadow a lower layer — so it is omitted. The flag NAMES come from
// the registry rows, like the variable names above: a key whose row names no flag cannot be
// carried by one. `--endpoint` and `--model` are absent for the same reason APOGEE_ENDPOINT is
// (ADR 0036): they name no config key, so they are not resolved through the layers.
func flagLayer(opts Options, changed func(string) bool) Layer {
	var l Layer
	for _, k := range multiSourceKeys {
		if k.fromFlag == nil || k.row.FlagName == "" {
			continue
		}
		if !changed(k.row.FlagName) {
			continue
		}
		k.fromFlag(&l, opts)
	}
	return l
}

// Source names which precedence source supplied a key's value. The zero value is the
// ordinary case — the config file, or the built-in default below it — and the other two are the
// sources that can BEAT the file (flag > env > file > default).
type Source string

const (
	SourceFile Source = ""     // the config file, or the default below it: nothing overrode the key
	SourceEnv  Source = "env"  // an APOGEE_* variable set the key
	SourceFlag Source = "flag" // an explicitly-set command-line flag set the key
)

// overrideSources reports which higher-precedence source beat the config file for each key this
// run, keyed by registry path. Resolution COLLAPSES the layers into one value, so afterwards the
// winner is no longer recoverable from the resolved settings — and a surface that shows a key's
// value has to be able to say "this is not what your file says": a `/settings` row rendered
// without the marker would present an environment variable's value as the file's, and offer to
// persist an edit that the override would keep swallowing for the rest of the run.
//
// The predicates are deliberately the SAME two the layers themselves are built from (flagLayer's
// changed, envLayer's non-empty getenv), read off the same registry rows, so the marker cannot
// claim a source that did not actually win. Keys absent from the map resolved from the file or the
// default, which is the majority and needs no entry.
func overrideSources(changed func(string) bool, getenv func(string) string) map[string]Source {
	sources := make(map[string]Source, len(multiSourceKeys))
	for _, k := range multiSourceKeys {
		switch {
		case k.fromFlag != nil && k.row.FlagName != "" && changed(k.row.FlagName):
			sources[k.row.Path] = SourceFlag
		case k.fromEnv != nil && k.row.EnvVar != "" && getenv(k.row.EnvVar) != "":
			sources[k.row.Path] = SourceEnv
		}
	}
	return sources
}

// ----------------------------------------------------------------------------
// The orchestrator
// ----------------------------------------------------------------------------

// ApplyConfig resolves the upstream/autonomy settings by precedence and writes them
// back into opts before construction. The config file lives at <apogee-home>/config.yaml,
// where the home follows --config > APOGEE_CONFIG > ~/.apogee; the file cannot set the
// home (it lives inside it), so --config / APOGEE_CONFIG are overlaid onto opts first.
// The workspace honours --workspace > APOGEE_WORKSPACE > cwd the same way. changed,
// getenv, and readFile are injected so the whole chain is testable end-to-end.
//
// This is where the live machine identity enters resolution: platform.HostID() selects the
// Host acknowledgement, if any, that applies on this host (ADR 0012, amendment 2026-07-21).
// notify receives resolution's soft notices — a malformed acknowledgement is reported and
// skipped, never fatal — on stderr, like the other pre-TUI startup lines. The one-time legacy
// migration announces itself the same way (LoadFileConfig): a file apogee rewrote on the user's
// behalf must say so where they will see it.
//
// One error is deliberately returned LAST, after every value has been written back:
// [StartupUndetermined], the refusal that says the config could not name a startup server. Every
// Driver still receives it, so a driver that cannot ask a human refuses exactly as it did before
// (item 3's hard errors); but the TUI answers it by starting pre-bound (ADR 0036 decisions 3 and
// 7), and it can only do that if the rest of the resolution — the servers list to pick from, the
// mode, the roots — is standing in opts by the time it reads the refusal.
func ApplyConfig(opts *Options, changed func(string) bool, getenv func(string) string, readFile func(string) ([]byte, error), notify func(string)) error {
	opts.ConfigDir = resolveConfigDir(opts.ConfigDir, changed, getenv)
	if !changed("workspace") {
		if v := getenv(EnvWorkspace); v != "" {
			opts.Workspace = v
		}
	}

	file, err := LoadFileConfig(FilePath(opts.ConfigDir), readFile, notify)
	if err != nil {
		return err
	}
	env, err := envLayer(getenv)
	if err != nil {
		return err
	}

	s, notices := ResolveSettings(file, env, flagLayer(*opts, changed), platform.HostID())
	for _, n := range notices {
		notify(n)
	}
	// A present block that cannot be honoured is a hard error, before anything is written back
	// into opts: an out-of-range port would otherwise surface as a degraded rung at the first
	// presentation, long after the typo (ADR 0019 §4 degrades MECHANISM failures, not config).
	if err := s.Present.Validate(); err != nil {
		return err
	}
	// A ui block naming a spinner style this build has no animation for is the same kind of loud
	// startup error: silently resolving it to another style would leave the user staring at a
	// spinner their config did not ask for, with nothing pointing at the typo.
	if err := s.UI.Validate(); err != nil {
		return err
	}
	// A `cursor-shape:` naming a shape no terminal cursor has is the same kind of loud startup
	// error, for the same reason: drawing a block instead would leave the user staring at a caret
	// their config did not ask for. internal/tui owns the vocabulary (ParseCursorShape lists the
	// shapes); this only adds the key the bad value was read from, which that package cannot know.
	if _, err := tui.ParseCursorShape(s.CursorShape); err != nil {
		return fmt.Errorf("apogee: invalid cursor-shape: %w", err)
	}
	// A system-prompt block that contradicts itself — both spellings of one prompt at one level,
	// or a per-model entry carrying no prompt at all — is a defect in the FILE, independent of
	// this machine and of which model this run resolves, so it is refused here for every level.
	// Whether the SELECTED source's file reads and its placeholders are known is
	// ResolveSystemPrompt's job, after model resolution (ADR 0023).
	if err := s.SystemPrompt.Validate(); err != nil {
		return err
	}
	// A context-file NAME that cannot be a workspace file — empty, rooted, drive-scoped, climbing
	// out with "..", or listed twice — is the same kind of machine-independent defect in the file,
	// refused here where the message can name `context-files.names` and the value. Whether the
	// named files exist is deliberately not asked: discovery is the feature (contextFilesSettings).
	if err := s.ContextFiles.validate(); err != nil {
		return err
	}
	// A `servers:` entry that could never be switched to — no name, no endpoint, or a name an
	// earlier entry already took — is refused here for the same reason: it is a defect in the file,
	// independent of this machine and of whether the session ever reaches for that server. Left to
	// the moment of the switch it would surface as an entry that cannot be selected, or a name
	// resolving to whichever entry happened to come first, long after the line was written.
	if err := ValidateServers(s.Servers); err != nil {
		return err
	}
	// Which server this session starts on. It is the last step of resolution rather than part of it
	// (ADR 0036): the answer needs the resolved list, the resolved name and the raw invocation
	// overrides together, and a name that matches nothing is a fact about that triple, not about
	// any one key. The overrides are read from the SAME opts the flag layer was built from, before
	// the write-back below overwrites the flag-bound endpoint/model fields with the answer.
	//
	// A selection that cannot be made is NOT returned here: the refusal is held and returned at the
	// very end, so a Driver that answers it by asking the human (the TUI's pre-bound start) still
	// receives a fully-resolved opts to ask WITH. The startup entry is the zero one in that case,
	// which writes the empty endpoint/model/key a session with no upstream honestly has.
	raw := resolveStartupOverrides(*opts, changed, getenv)
	startup, startupErr := resolveStartupEntry(raw, s.StartupServer, s.Servers,
		FilePath(opts.ConfigDir), opts.ServerFlagBound)
	opts.Endpoint = startup.Endpoint
	opts.Model = startup.Model
	opts.APIKey = startup.APIKey
	// And the other two spellings of that same token: the command to run for it and the variable to
	// read it from, carried as written for the composition root to RESOLVE at the seam that needs
	// the key (KeyResolver). They travel beside the literal rather than in place of it because the
	// overlay above is a literal — an `APOGEE_API_KEY` run is a literal-source run whatever the
	// entry named — and because nothing here may run a command: resolution stays offline at load,
	// exactly as ValidateServers does (design call 4).
	opts.APIKeyCmd = startup.APIKeyCmd
	opts.APIKeyEnv = startup.APIKeyEnv
	opts.HostAlias = startup.Name
	// Whether that entry came out of the list or out of the invocation. A configured entry always
	// has a name (ValidateServers refuses one without) and the ephemeral override entry never does,
	// so namelessness IS the distinction — the same invariant the alias fallback below leans on.
	// An undetermined startup is neither: nothing was selected, so there is nothing to synthesize a
	// switch row for either.
	opts.StartupEphemeral = startupErr == nil && startup.Name == ""
	// And which launcher config — if any — that entry fronts its server with. The key belongs to the
	// entry (this plan, 2026-08-07: ADR 0029 decision 4's global key moved onto the `servers:` list),
	// so what the session starts with is the SELECTED entry's own value, carried as written for the
	// composition root to resolve. The ephemeral override entry carries none, which is the honest
	// answer for an endpoint no entry names: `/server` onto a launcher-fronted entry turns it on.
	opts.StartupLauncher = startup.LlamaLauncher
	// And how wide a fan-out that entry's server will take (ADR 0039 decision 2). Same reasoning as
	// the launcher key above: it belongs to the entry, so what the session starts with is the
	// SELECTED entry's own value, carried as written for the composition root to resolve against
	// what the server itself advertises. The ephemeral override entry pins nothing, which leaves an
	// override run discovering — and falling back to one agent at a time.
	opts.StartupParallelAgents = startup.ParallelAgents
	// And how long a single reply from that entry's server may run (ADR 0046). Same reasoning again:
	// the ceiling belongs to the entry, so what the session starts with is the SELECTED entry's own
	// value, carried as written for the composition root to hand the engine. The ephemeral override
	// entry pins nothing, which leaves an override run deriving the cap from its reply budget.
	opts.StartupMaxOutputTokens = startup.MaxOutputTokens
	// And what that entry's server BOUNDS a session to (ADR 0045 decision 3). It is flattened for the
	// reply ceiling's reason and travels the same way — the SELECTED entry's own value, carried as
	// written for the composition root to resolve over the top-level `context-window:` key
	// (ResolveContextWindow) at the bind. The ephemeral override entry pins nothing, which leaves an
	// override run on that top-level key and, unpinned there too, on what the first beat observes.
	opts.StartupContextWindow = startup.ContextWindow
	opts.Mode = s.Mode
	opts.Bypass = s.Bypass
	opts.Servers = s.Servers
	opts.StartupServer = s.StartupServer
	opts.Editor = s.Editor
	opts.ConfineToWorkspace = s.ConfineToWorkspace
	opts.UnconfinedHosts = s.UnconfinedHosts
	opts.WebSearchEndpoint = s.WebSearchEndpoint
	opts.UseProjectSkills = s.UseProjectSkills
	opts.AutoCompact = s.AutoCompact
	opts.AutoTitle = s.AutoTitle
	opts.RememberModel = s.RememberModel
	opts.ContextWindow = s.ContextWindow
	opts.MCPServers = s.MCPServers
	opts.ToolsDisabled = s.ToolsDisabled
	// A `tools.disabled:` name that matches no tool is a NOTICE, never a refusal: the list is how a
	// roster is pruned on evidence, and a typo in it must cost the user the tool they meant to
	// disable rather than the session. It is reported here, at the same startup boundary the
	// confinement notices come out of, because this is where the resolved list first exists.
	if n := unknownToolNotice(s.ToolsDisabled); n != "" {
		notify(n)
	}
	opts.ModelProfiles = s.ModelProfiles
	opts.Mechanisms = s.Mechanisms
	opts.ValidatedSetsEnable = s.ValidatedSetsEnable
	opts.ValidatedSetsAlias = s.ValidatedSetsAlias
	opts.Present = s.Present
	opts.SystemPrompt = s.SystemPrompt
	opts.ContextFiles = s.ContextFiles.resolved()
	opts.UI = s.UI
	opts.CursorShape = s.CursorShape
	// Which source won, for the keys where more than one could have: the resolved values above no
	// longer carry that fact, and the /settings pane has to mark a row the environment or a flag is
	// overriding (see overrideSources). Recorded from the same predicates the layers were built
	// from, a few lines up.
	opts.Overrides = overrideSources(changed, getenv)
	// A configured entry always has a name (ValidateServers refuses one without), so this fallback
	// is for the entry that has none: the unnamed startup entry a raw `--endpoint`/`APOGEE_ENDPOINT`
	// override builds, which has no label but still has to be called something in the footer — and,
	// since that label is also what its synthesized switch row is called, something no configured
	// entry already answers to.
	if opts.HostAlias == "" {
		opts.HostAlias = aliasFromEndpoint(opts.Endpoint, opts.Servers)
	}
	// Held from the selection step above: everything is resolved, so the caller that can ask a
	// human has what it needs to ask, and the caller that cannot refuses with the same message it
	// has always printed.
	return startupErr
}

// startupOverrides carries the raw invocation overrides ADR 0036 detached from the config schema:
// the endpoint this run is pointed at, the bearer token it sends, and the model hint it asks for.
// They are no longer config keys with a value that could be edited or persisted — they describe
// ONE run's upstream, which is why they are resolved on their own rather than through a layer, and
// why nothing ever writes them back. An empty field means the run named no override of that kind.
type startupOverrides struct {
	endpoint string
	apiKey   string
	model    string
}

// startupOverride binds one raw override to the sources it is read from. It exists for the same
// reason multiSourceKey does — so the environment-variable and flag NAMES have exactly one home,
// and a source that is advertised is a source that is read — but it deliberately hangs off no
// registry row: since ADR 0036 these names describe no config key, and the bijection guard pins
// registry rows to `fileConfig` tags that no longer exist for them.
//
// fromFlag is nil for an override with no flag, which today is the api key: there is no
// --api-key on purpose, because a secret on the command line lands in shell history and in `ps`
// output. into is what projects a resolved text onto the struct above.
type startupOverride struct {
	envVar   string
	flagName string
	fromFlag func(opts Options) string
	into     func(o *startupOverrides, text string)
}

// startupOverrideSources is that table, in the order the overrides compose a server entry.
var startupOverrideSources = []startupOverride{
	{
		envVar:   EnvEndpoint,
		flagName: "endpoint",
		fromFlag: func(opts Options) string { return opts.Endpoint },
		into:     func(o *startupOverrides, text string) { o.endpoint = text },
	},
	{
		envVar: EnvAPIKey,
		into:   func(o *startupOverrides, text string) { o.apiKey = text },
	},
	{
		envVar:   EnvModel,
		flagName: "model",
		fromFlag: func(opts Options) string { return opts.Model },
		into:     func(o *startupOverrides, text string) { o.model = text },
	},
}

// StartupOverrideFlags names the command-line flags startup-override resolution reads — the flag
// half of the detached `APOGEE_ENDPOINT`/`APOGEE_API_KEY`/`APOGEE_MODEL` trio, which describe no
// config key and so appear in no registry row. Resolution asks the Driver's flag set whether each
// was set (cobra's Changed), so a name here that the Driver never registers is a question nothing
// can answer: the composition root's own test walks this list against its command to keep the two
// from drifting, which is why the table is readable from outside rather than restated there.
func StartupOverrideFlags() []string {
	names := make([]string, 0, len(startupOverrideSources))
	for _, src := range startupOverrideSources {
		if src.flagName != "" {
			names = append(names, src.flagName)
		}
	}
	return names
}

// resolveStartupOverrides reads the raw overrides off the flags and the environment, the flag
// beating the variable within each pair — the same precedence the config keys ride, applied to
// values that are not config keys. An explicitly-set flag wins even when its value is empty: the
// user spelled it out, and letting a variable slip back in underneath would make `--endpoint ""`
// mean something other than "no endpoint from the command line". An unset flag carries its zero
// default, which must not shadow the variable, so only cobra's Changed lets a flag speak.
func resolveStartupOverrides(opts Options, changed func(string) bool, getenv func(string) string) startupOverrides {
	var o startupOverrides
	for _, src := range startupOverrideSources {
		if src.fromFlag != nil && src.flagName != "" && changed(src.flagName) {
			src.into(&o, src.fromFlag(opts))
			continue
		}
		if v := getenv(src.envVar); v != "" {
			src.into(&o, v)
		}
	}
	return o
}

// overlay applies the key and hint overrides to a CONFIGURED entry — the no-endpoint-override
// case, where the run still starts on a listed server but sends a one-off key, or asks that
// server for a different model than its own `model:` hint names. An override that was not given
// leaves the entry's own value standing.
func (o startupOverrides) overlay(entry ServerEntry) ServerEntry {
	if o.apiKey != "" {
		entry.APIKey = o.apiKey
	}
	if o.model != "" {
		entry.Model = o.model
	}
	return entry
}

// resolveStartupEntry answers which server this session is built from, overrides first (ADR 0036
// decision 6).
//
// A raw endpoint override constructs an EPHEMERAL, unnamed entry for this run alone. It wins over
// any `server:` / `--server` name, because pointing apogee at a URL is the most explicit thing a
// user can say about where this run talks; it carries the key and hint overrides as its own
// fields; and it rescues a startup the list alone could not answer, so `APOGEE_ENDPOINT=… apogee`
// works against a config that lists nothing at all. It is unnamed because there is nothing to
// name it — the footer falls back to the endpoint's host (hostFromEndpoint) — and namelessness is
// exactly what keeps it out of the file: nothing persists an entry that has no name to persist.
//
// With no endpoint override the list is the single definition, and the key and hint overrides
// overlay the selected entry's own two optional fields.
func resolveStartupEntry(o startupOverrides, name string, servers []ServerEntry, configPath string,
	serverFlag bool) (ServerEntry, error) {
	if o.endpoint != "" {
		return ServerEntry{Endpoint: o.endpoint, APIKey: o.apiKey, Model: o.model}, nil
	}
	entry, err := selectStartupServer(name, servers, configPath, serverFlag)
	if err != nil {
		return ServerEntry{}, err
	}
	return o.overlay(entry), nil
}

// selectStartupServer resolves the server a session starts on: the `servers:` entry named by the
// post-precedence `server:` value (`--server` > `APOGEE_SERVER` > `server:`). The entry IS the
// answer — its endpoint, key and model hint are what the session is built from, and its name is
// the alias the footer calls it — which is what makes the list the single definition (ADR 0036).
//
// The three ways there is no answer are all refused here, with the config path and a block to
// paste, because a session with no upstream can do nothing at all:
//
//   - the list is empty: nothing is configured yet;
//   - no name is chosen (first boot, or a config that never recorded one);
//   - the chosen name matches no entry — a renamed or deleted server, or a typo.
//
// None of them is reached when a raw endpoint override already answered the question
// (resolveStartupEntry, which calls this only for the no-override case).
//
// ADR 0036 gives the TUI a better answer for all three — it asks, through the `/server` picker, or
// points at `/settings` when nothing is configured — so each refusal is typed with the REASON it
// carries (StartupUndetermined) and the TUI reads that instead of printing it. The message itself
// stays the permanent answer for the non-interactive drivers (headless, probe, bench): they have no
// one to ask.
//
// serverFlag says whether the command printing that message registers `--server`, which is what
// decides the remedy the two name-shaped refusals offer (startupServerRemedy).
func selectStartupServer(name string, servers []ServerEntry, configPath string, serverFlag bool) (ServerEntry, error) {
	chosen := strings.TrimSpace(name)
	switch {
	case len(servers) == 0:
		return ServerEntry{}, &StartupUndetermined{
			Start: tui.PreboundStart{Reason: tui.PreboundNoServers},
			Msg: fmt.Sprintf("apogee: no servers are configured — apogee needs a server to "+
				"talk to.\n\nAdd one to %s and start apogee again:\n\n%s", configPath, exampleServersBlock),
		}
	case chosen == "":
		return ServerEntry{}, &StartupUndetermined{
			Start: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
			Msg: fmt.Sprintf("apogee: no startup server is chosen — %s configures %s but "+
				"records no server:.\n\nName the one to start on (%s):\n\nserver: %s\n",
				configPath, ServerNameList(servers), startupServerRemedy(serverFlag), servers[0].Name),
		}
	}
	for _, s := range servers {
		if s.Name == chosen {
			return s, nil
		}
	}
	return ServerEntry{}, &StartupUndetermined{
		Start: tui.PreboundStart{Reason: tui.PreboundStaleChoice, Name: chosen},
		Msg: fmt.Sprintf("apogee: server: names %q, which no servers: entry in %s carries "+
			"(configured: %s).\n\nFix the name (%s).", chosen, configPath,
			ServerNameList(servers), startupServerRemedy(serverFlag)),
	}
}

// startupServerRemedy is the OTHER way to answer "which server", offered by the two refusals above
// that a name would fix. It follows the flag surface of the command the message is printed by: the
// root command registers `--server`, and the non-interactive commands that actually PRINT these
// refusals — `apogee headless`, `apogee probe` — do not, so naming the flag there would send the
// user to a parser that rejects it. What every command has is the environment variable, beside the
// `server:` key the message already names.
func startupServerRemedy(serverFlag bool) string {
	if serverFlag {
		return "or pass --server <name>"
	}
	return "or set APOGEE_SERVER=<name>"
}

// StartupUndetermined is selection's refusal: the config, the flags and the environment together
// could not say which server this session starts on. It is an ERROR first — every Driver receives
// it, and one that has nobody to ask prints it and stops, which is the permanent behaviour for
// headless, probe and bench — and a reason second: the TUI recognises the type, takes the reason
// out of it, and starts pre-bound instead (ADR 0036 decisions 3, 4 and 7 — the three reasons it
// carries), because asking through the picker fixes in one keystroke what a refusal would send to
// file surgery.
//
// The message is carried rather than formatted here so each of the three cases keeps the wording
// that names ITS remedy, and so the type has exactly one job: pairing that message with the reason.
type StartupUndetermined struct {
	Start tui.PreboundStart
	Msg   string
}

// Error is the message the non-interactive drivers print — unchanged from the plain errors this
// type replaced, because the refusal a human reads is the same refusal it always was.
func (e *StartupUndetermined) Error() string { return e.Msg }

// exampleServersBlock is the smallest config that starts a session, shown by the refusals above.
// It is spelled here rather than in each message so the shape a user is told to write is one
// string, and the same one the seeded template teaches.
const exampleServersBlock = "servers:\n" +
	"  - name: my-box\n" +
	"    endpoint: http://127.0.0.1:1111\n" +
	"\nserver: my-box\n"

// hostFromEndpoint extracts the bare host (without scheme or port) from an endpoint URL —
// the fallback alias for a startup entry that carries no name. A URL that does not parse, or
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

// aliasFromEndpoint is that fallback made collision-aware, because the label is not only the
// footer's: the ephemeral startup entry is also synthesized into a switch row beside the
// configured `servers:` rows, and a bare host that happens to equal one of their names would make
// two rows answer to one label — both drawn `· current`, and name-keyed lookups resolving whichever
// row comes first. The synthesized one takes a `" (endpoint)"` suffix to say which it is. The
// comparison is the exact one those lookups switch on, so a label that only collides on case is
// left alone — nothing confuses it. One pass, no loop: a configured name that already spells the
// suffixed form is an operator who armed the collision by hand, and is accepted as-is.
func aliasFromEndpoint(endpoint string, servers []ServerEntry) string {
	alias := hostFromEndpoint(endpoint)
	if alias == "" {
		return ""
	}
	for _, s := range servers {
		if s.Name == alias {
			return alias + " (endpoint)"
		}
	}
	return alias
}

// ApogeeHome resolves the absolute apogee home directory: the configDir override when
// set, else ~/.apogee (the single uniform dotdir on every OS — owner decision, not XDG).
// It is shared by resolveRoots (the state roots) and FilePath (where config.yaml
// lives), so both agree on the home.
func ApogeeHome(configDir string) (string, error) {
	home := configDir
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("apogee: resolve home directory: %w", err)
		}
		home = filepath.Join(userHome, ".apogee")
	}
	return filepath.Abs(home)
}

// ServerNameList renders the switchable names for findServer's error (an empty list renders
// "(none)", matching knownMechanismList's shape for the same job).
func ServerNameList(entries []ServerEntry) string {
	if len(entries) == 0 {
		return "(none)"
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return strings.Join(names, ", ")
}

// resolveConfigDir returns the apogee home honouring --config > APOGEE_CONFIG, falling
// back to the passed value (empty ⇒ the ~/.apogee default, applied downstream by
// ApogeeHome). The config file lives inside the home, so it cannot set it. Shared by the
// first-run seeder and ApplyConfig so both agree on where config lives.
func resolveConfigDir(configDir string, changed func(string) bool, getenv func(string) string) string {
	if !changed("config") {
		if v := getenv(EnvConfig); v != "" {
			return v
		}
	}
	return configDir
}

// FilePath returns the config.yaml path under the resolved apogee home, or "" if
// the home cannot be resolved (no config file then — LoadFileConfig treats "" as absent,
// and resolveRoots surfaces the home-resolution failure later with a clearer message).
func FilePath(configDir string) string {
	home, err := ApogeeHome(configDir)
	if err != nil {
		return ""
	}
	return filepath.Join(home, "config.yaml")
}

// Upstream discovery is deliberately absent from this file. It used to live here as the
// lowest-priority resolution layer (flag > env > file > discover), probing the server before the
// first paint; it now belongs to the heartbeat, which asks the same question every ten seconds from
// inside the running session (ADR 0024). A `servers:` entry's `model` hint and `context-window:`
// are what they always were — a pinned model id and a pinned window — but neither is filled in
// from the wire any more: an unset
// model stays empty until the first beat binds one, and an unset window stays 0, which is why
// opts.contextWindow > 0 now means "the user pinned it" and nothing else.

// ----------------------------------------------------------------------------
// System-prompt selection (ADR 0023: after the model is resolved)
// ----------------------------------------------------------------------------

// ResolveSystemPrompt collapses the resolved system-prompt block into the ONE template
// domain.Config.SystemPrompt carries for the model this session is BOUND to. The composition root
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
func ResolveSystemPrompt(sp SystemPromptSettings, model, home string, readFile func(string) ([]byte, error)) (string, error) {
	src := sp.Global
	modelKey := "" // non-empty ⇒ the selected prompt came from a system-prompt-models entry
	if m, ok := sp.Models[model]; ok {
		src, modelKey = m, model
	}

	template := src.Text
	if src.File != "" {
		path, err := ExpandUserPath(src.File)
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
		if src.File != "" {
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

// ExpandUserPath expands a leading `~` (alone, or as `~/…`) to the user's home directory, so a
// config may name a file the way the user would type it in a shell. Any other path is returned
// unchanged — including one whose `~` is not leading, which is a legal filename character.
func ExpandUserPath(p string) (string, error) {
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
