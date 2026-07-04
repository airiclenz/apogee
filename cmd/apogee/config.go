package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
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

	// confineToWorkspace is GLOBAL-CONFIG-ONLY (ADR 0012): it is resolved from the config
	// file alone, never from a flag or env, so a hostile repo invoking apogee cannot loosen
	// Auto's blast radius. Default true. (There is no project-level config file today; the
	// file-only resolution is what keeps it un-loosenable by the invocation environment.)
	confineToWorkspace bool

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

	// contextWindow overrides the discovered model context window in tokens (item 3 / S3). File-only
	// (no flag/env, like autoCompact) and default 0 ⇒ no override, so the CLI discovers the window
	// from the server; a positive value wins and skips the probe (the escape hatch for a server that
	// does not advertise its window, or an offline pinned-model start). It feeds
	// ContextConfig.MaxContextTokens, which the Budget and automatic Compaction bind against.
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
	// composition root drives the mechanisms catalogue's constructor table for each enabled ID; an
	// unknown ID is a loud startup error. Bypass still wins (an enabled non-off-ramp Mechanism is
	// not dispatched under bypass — ADR 0006 / item 2's gate).
	mechanisms map[string]bool
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

	// confineToWorkspace is set only by the FILE layer (global-config-only, ADR 0012). The
	// env and flag layers leave it nil so the invocation environment cannot loosen it.
	confineToWorkspace *bool

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

	// contextWindow is set only by the FILE layer (the window override is config'd, no flag/env —
	// like autoCompact). A nil pointer means the source does not set a window, so resolution falls
	// through to discovery; only a positive `context-window:` projects to a non-nil pointer.
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
}

// resolveSettings overlays the layers in increasing priority — the default base, then
// the file, then the environment, then the flags — so a flag beats an environment
// variable beats the file beats the default. Only ask-before (the default mode) is a
// non-zero base; endpoint/model default empty and bypass defaults false.
//
// confine-to-workspace is the exception: it defaults true and is resolved from the FILE
// layer ONLY (never env or flag), because it is global-config-only (ADR 0012) — a hostile
// repo's invocation environment must not be able to loosen Auto's blast radius.
func resolveSettings(file, env, flag layer) settings {
	s := settings{mode: string(modeAskBefore), confineToWorkspace: true, useProjectSkills: true, autoCompact: true}
	if file.confineToWorkspace != nil {
		s.confineToWorkspace = *file.confineToWorkspace
	}
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
	s.mcpServers = file.mcpServers // file-only (P3.15); env/flag never set MCP servers
	s.mechanisms = file.mechanisms // file-only (Phase 4); env/flag never enable Mechanisms
	if file.profile != nil {       // file-only; env/flag never carry a model profile
		s.profile = *file.profile
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
	}
	return s
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
	// ConfineToWorkspace is global-config-only (ADR 0012): a pointer so an explicit
	// `confine-to-workspace: false` is distinguishable from an absent key (which keeps the
	// secure default true). It has no flag or env — editing the global config IS the
	// deliberate acknowledgement required to run Auto unconfined.
	ConfineToWorkspace *bool `yaml:"confine-to-workspace"`
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
	// ContextWindow overrides the discovered model context window in tokens (item 3 / S3). File-only
	// (no flag/env), like auto-compact. Absent or ≤ 0 ⇒ the CLI discovers the window from the server
	// (for a pinned model too); a positive value wins and skips the probe — the escape hatch for a
	// server that does not advertise its window, or an offline pinned-model start. It feeds
	// ContextConfig.MaxContextTokens, which the Budget and automatic Compaction bind against.
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
	if fc.ConfineToWorkspace != nil {
		l.confineToWorkspace = fc.ConfineToWorkspace
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
func applyConfig(opts *options, changed func(string) bool, getenv func(string) string, readFile func(string) ([]byte, error)) error {
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

	s := resolveSettings(file, env, flagLayer(*opts, changed))
	opts.endpoint = s.endpoint
	opts.model = s.model
	opts.mode = s.mode
	opts.bypass = s.bypass
	opts.hostAlias = s.hostAlias
	opts.confineToWorkspace = s.confineToWorkspace
	opts.webSearchEndpoint = s.webSearchEndpoint
	opts.useProjectSkills = s.useProjectSkills
	opts.autoCompact = s.autoCompact
	opts.contextWindow = s.contextWindow
	opts.mcpServers = s.mcpServers
	opts.profile = s.profile
	opts.mechanisms = s.mechanisms
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

// ----------------------------------------------------------------------------
// Model discovery (the lowest-priority resolution layer: flag > env > file > discover)
// ----------------------------------------------------------------------------

// discoveredUpstream is what a discovery probe resolves: the server's active model id and its
// context-window size in tokens (0 when the server does not advertise one).
type discoveredUpstream struct {
	model         string
	contextWindow int
}

// modelDiscoverer asks the Upstream at endpoint for its active model and context window. It
// is injected so auto-discovery is testable without a live server; the production discoverer
// (wire.go) probes /v1/models through the provider client.
type modelDiscoverer func(ctx context.Context, endpoint string) (discoveredUpstream, error)

// resolveModel fills opts.model (and, in the same probe, opts.contextWindow) by asking the server
// when no model was configured by any layer (flag, env, and file all empty) but an endpoint is
// known — so a single-model server (e.g. llama.cpp's llama-server, which serves whatever model was
// loaded) runs with no model set at all. It returns the discovered id ("" when discovery did not
// run) so the caller can surface a one-line notice. A discovery failure is returned rather than
// swallowed: the user learns the server is unreachable or advertises no model, instead of
// silently sending a model-less request. With no endpoint there is nothing to ask, so it is
// a no-op — construction then surfaces the missing-endpoint error, the real problem. A PINNED
// model early-returns here; its context window is resolved separately by resolveContextWindow
// (item 3 / S3), so a configured model no longer disables the Budget.
func resolveModel(ctx context.Context, opts *options, discover modelDiscoverer) (string, error) {
	if opts.model != "" || opts.endpoint == "" {
		return "", nil
	}
	got, err := discover(ctx, opts.endpoint)
	if err != nil {
		return "", fmt.Errorf(
			"apogee: no model configured and discovery from %s failed: %w; "+
				"set one with --model, APOGEE_MODEL, or model: in config.yaml", opts.endpoint, err)
	}
	opts.model = got.model
	if opts.contextWindow == 0 { // a context-window: key wins over what the server advertises
		opts.contextWindow = got.contextWindow
	}
	return got.model, nil
}

// resolveContextWindow discovers the model context window when it is still unknown — including for
// a PINNED model, whose configured id makes resolveModel early-return before it can probe (item 3 /
// S3: a configured model must not silently disable the Budget and automatic Compaction). It is a
// no-op when a context-window: key already supplied the window or model discovery already learned
// it (opts.contextWindow > 0 — the key/probe wins and this probe is skipped), and when no endpoint
// is known (nothing to ask). The pinned model id is kept regardless of what the probe reports; only
// the window is adopted. Unlike model discovery this is NEVER fatal: a pinned-model user can start
// offline, so a failed probe leaves the window unknown (0) and emits a one-line notice via notify —
// the Budget then stays inactive, which runRoot surfaces once at startup.
func resolveContextWindow(ctx context.Context, opts *options, discover modelDiscoverer, notify func(string)) {
	if opts.contextWindow > 0 || opts.endpoint == "" {
		return
	}
	got, err := discover(ctx, opts.endpoint)
	if err != nil {
		notify(fmt.Sprintf("apogee: context-window discovery from %s failed: %v; "+
			"the Budget and automatic compaction stay inactive — set context-window: in config.yaml", opts.endpoint, err))
		return
	}
	opts.contextWindow = got.contextWindow
}
