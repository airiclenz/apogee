package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }

// The precedence rule itself: a flag beats an env var beats the file beats the default,
// resolved per field (phase-2 detail plan §4 P2.5).
func TestResolveSettingsPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		file, env, flag layer
		want            settings
	}{
		{
			name: "all empty → defaults",
			want: settings{mode: "ask-before"},
		},
		{
			name: "file fills every field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan"), bypass: boolptr(true)},
			want: settings{endpoint: "http://file", model: "m-file", mode: "plan", bypass: true},
		},
		{
			name: "env beats file, file fills the rest",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file")},
			env:  layer{endpoint: strptr("http://env")},
			want: settings{endpoint: "http://env", model: "m-file", mode: "ask-before"},
		},
		{
			name: "flag beats env beats file, per field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan")},
			env:  layer{endpoint: strptr("http://env"), model: strptr("m-env")},
			flag: layer{endpoint: strptr("http://flag")},
			want: settings{endpoint: "http://flag", model: "m-env", mode: "plan"},
		},
		{
			name: "explicit false in a higher layer overrides true below it",
			file: layer{bypass: boolptr(true)},
			flag: layer{bypass: boolptr(false)},
			want: settings{mode: "ask-before", bypass: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveSettings(tt.file, tt.env, tt.flag); got != tt.want {
				t.Errorf("resolveSettings = %+v; want %+v", got, tt.want)
			}
		})
	}
}

// applyConfig drives the whole chain end-to-end: a config file on disk, env overrides, and
// an explicit flag, all resolved with the real loader/parser against injected sources.
func TestApplyConfigEndToEnd(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = "endpoint: http://file\nmodel: m-file\nmode: plan\nbypass: true\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// env overrides model and turns bypass off; the flag overrides endpoint.
	getenv := func(k string) string {
		switch k {
		case envModel:
			return "m-env"
		case envBypass:
			return "false"
		default:
			return ""
		}
	}
	changed := func(name string) bool { return name == "endpoint" || name == "config" }
	opts := options{configDir: home, endpoint: "http://flag"}

	if err := applyConfig(&opts, changed, getenv, os.ReadFile); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.endpoint != "http://flag" {
		t.Errorf("endpoint = %q; want the flag value", opts.endpoint)
	}
	if opts.model != "m-env" {
		t.Errorf("model = %q; want the env value", opts.model)
	}
	if opts.mode != "plan" {
		t.Errorf("mode = %q; want the file value", opts.mode)
	}
	if opts.bypass {
		t.Error("bypass = true; want the env value false to override the file's true")
	}
}

// A run with no config file, no env, and only defaults resolves cleanly to the defaults.
func TestApplyConfigDefaults(t *testing.T) {
	t.Parallel()
	noEnv := func(string) string { return "" }
	noFlags := func(string) bool { return false }
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml

	if err := applyConfig(&opts, noFlags, noEnv, os.ReadFile); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.endpoint != "" || opts.model != "" || opts.bypass {
		t.Errorf("non-default endpoint/model/bypass: %+v", opts)
	}
	if opts.mode != string(modeAskBefore) {
		t.Errorf("mode = %q; want the default %q", opts.mode, modeAskBefore)
	}
}

// APOGEE_CONFIG / APOGEE_WORKSPACE fill the config dir and workspace when their flags are
// not set, and the config file is then read from that env-resolved home.
func TestApplyConfigEnvDirsAndFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("model: m-file\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ws := t.TempDir()
	getenv := func(k string) string {
		switch k {
		case envConfig:
			return home
		case envWorkspace:
			return ws
		default:
			return ""
		}
	}
	opts := options{}
	if err := applyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.configDir != home {
		t.Errorf("configDir = %q; want the APOGEE_CONFIG value %q", opts.configDir, home)
	}
	if opts.workspace != ws {
		t.Errorf("workspace = %q; want the APOGEE_WORKSPACE value %q", opts.workspace, ws)
	}
	if opts.model != "m-file" {
		t.Errorf("model = %q; want it read from the env-resolved config home", opts.model)
	}
}

// An explicit --config flag wins over APOGEE_CONFIG (the flag is not overlaid by env).
func TestApplyConfigFlagDirBeatsEnvDir(t *testing.T) {
	t.Parallel()
	flagHome := t.TempDir()
	getenv := func(k string) string {
		if k == envConfig {
			return filepath.Join(t.TempDir(), "ignored")
		}
		return ""
	}
	changed := func(name string) bool { return name == "config" }
	opts := options{configDir: flagHome}
	if err := applyConfig(&opts, changed, getenv, os.ReadFile); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.configDir != flagHome {
		t.Errorf("configDir = %q; want the flag value %q (env must not overlay a set flag)", opts.configDir, flagHome)
	}
}

// A malformed config file is a hard error — a typo'd setting must not be silently ignored.
func TestApplyConfigMalformedFileErrors(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("endpoint: [not a string\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile)
	if err == nil {
		t.Fatal("malformed config: want an error, got nil")
	}
}

// A set-but-unparseable APOGEE_BYPASS is a hard error rather than a silently-ignored flag.
func TestApplyConfigBadBypassEnvErrors(t *testing.T) {
	t.Parallel()
	getenv := func(k string) string {
		if k == envBypass {
			return "yes-please"
		}
		return ""
	}
	opts := options{configDir: t.TempDir()}
	err := applyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile)
	if err == nil {
		t.Fatal("invalid APOGEE_BYPASS: want an error, got nil")
	}
}

// An absent config file is not an error — a config file is optional.
func TestLoadFileConfigAbsentIsEmpty(t *testing.T) {
	t.Parallel()
	l, err := loadFileConfig(filepath.Join(t.TempDir(), "config.yaml"), os.ReadFile)
	if err != nil {
		t.Fatalf("absent config: unexpected error %v", err)
	}
	if l != (layer{}) {
		t.Errorf("absent config produced a non-empty layer: %+v", l)
	}
}

// A read error other than not-exist propagates (it is not swallowed as "absent").
func TestLoadFileConfigReadErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	readFile := func(string) ([]byte, error) { return nil, boom }
	_, err := loadFileConfig("/some/config.yaml", readFile)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read error = %v; want it propagated (not treated as absent)", err)
	}
}
