package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strconv"

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
	s := settings{mode: string(modeAskBefore), confineToWorkspace: true}
	if file.confineToWorkspace != nil {
		s.confineToWorkspace = *file.confineToWorkspace
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

// resolveModel fills opts.model (and opts.contextWindow) by asking the server when no model
// was configured by any layer (flag, env, and file all empty) but an endpoint is known — so a single-model
// server (e.g. llama.cpp's llama-server, which serves whatever model was loaded) runs with
// no model set at all. It returns the discovered id ("" when discovery did not run) so the
// caller can surface a one-line notice. A discovery failure is returned rather than
// swallowed: the user learns the server is unreachable or advertises no model, instead of
// silently sending a model-less request. With no endpoint there is nothing to ask, so it is
// a no-op — construction then surfaces the missing-endpoint error, the real problem.
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
	opts.contextWindow = got.contextWindow
	return got.model, nil
}
