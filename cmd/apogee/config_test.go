package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/platform"
)

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }
func intptr(i int) *int       { return &i }

// noNotify drops applyConfig's soft startup notices — the tests below assert resolved
// values, not the wording (resolveConfineToWorkspace's own table covers the notices).
func noNotify(string) {}

// testHostID is the machine identity injected into resolveSettings so the Host
// acknowledgement ladder is pinned off whatever host the tests happen to run on.
const testHostID = "testbox-a1b2c3"

// unidentifiedTestHostID is what platform.HostID() composes on a host that can supply
// neither a hostname nor a machine id: the one value that is identical on every such
// machine, and therefore the one an acknowledgement must never match. It is spelled out
// rather than computed, so a change to the composition shows up here as a failure.
const unidentifiedTestHostID = "unknown-e3b0c4"

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
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "file fills every field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan"), bypass: boolptr(true)},
			want: settings{endpoint: "http://file", model: "m-file", mode: "plan", bypass: true, confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "env beats file, file fills the rest",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file")},
			env:  layer{endpoint: strptr("http://env")},
			want: settings{endpoint: "http://env", model: "m-file", mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "flag beats env beats file, per field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan")},
			env:  layer{endpoint: strptr("http://env"), model: strptr("m-env")},
			flag: layer{endpoint: strptr("http://flag")},
			want: settings{endpoint: "http://flag", model: "m-env", mode: "plan", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "explicit false in a higher layer overrides true below it",
			file: layer{bypass: boolptr(true)},
			flag: layer{bypass: boolptr(false)},
			want: settings{mode: "ask-before", bypass: false, confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "confine-to-workspace is file-only and defaults true",
			file: layer{confineToWorkspace: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: false, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "use-project-skills is file-only and defaults true",
			file: layer{useProjectSkills: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: false, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "use-project-skills is NOT set by env or flag (file-only)",
			env:  layer{useProjectSkills: boolptr(false)},
			flag: layer{useProjectSkills: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "auto-compact is file-only and defaults true",
			file: layer{autoCompact: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: false, validatedSetsEnable: true},
		},
		{
			name: "auto-compact is NOT set by env or flag (file-only)",
			env:  layer{autoCompact: boolptr(false)},
			flag: layer{autoCompact: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "context-window is file-only (default 0 ⇒ discover)",
			file: layer{contextWindow: intptr(65536)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextWindow: 65536},
		},
		{
			name: "context-window is NOT set by env or flag (file-only)",
			env:  layer{contextWindow: intptr(65536)},
			flag: layer{contextWindow: intptr(65536)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "confine-to-workspace is NOT loosenable by env or flag (global-config-only)",
			env:  layer{confineToWorkspace: boolptr(false)}, // an env layer cannot carry it in practice; assert it is ignored even if set
			flag: layer{confineToWorkspace: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "a matching unconfined-hosts entry resolves confine-to-workspace to false",
			file: layer{unconfinedHosts: []unconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
			want: settings{mode: "ask-before", confineToWorkspace: false, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true,
				unconfinedHosts: []unconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
		},
		{
			name: "unconfined-hosts is NOT settable by env or flag (global-config-only)",
			env:  layer{unconfinedHosts: []unconfinedHost{{ID: testHostID}}},
			flag: layer{unconfinedHosts: []unconfinedHost{{ID: testHostID}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "web-search endpoint is file-only (default empty)",
			file: layer{webSearchEndpoint: strptr("https://search.example.com")},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, webSearchEndpoint: "https://search.example.com"},
		},
		{
			name: "mcp servers are file-only (default empty)",
			file: layer{mcpServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, mcpServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
		},
		{
			name: "mcp servers are NOT settable by env or flag (file-only)",
			env:  layer{mcpServers: []mcp.ServerConfig{{Name: "fromenv"}}},
			flag: layer{mcpServers: []mcp.ServerConfig{{Name: "fromflag"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "model profile is file-only (default zero)",
			file: layer{profile: &apogee.ModelProfile{
				ToolCallFormat: apogee.FormatMarkdownFenced,
				Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, profile: apogee.ModelProfile{
				ToolCallFormat: apogee.FormatMarkdownFenced,
				Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
		},
		{
			name: "model profile is NOT settable by env or flag (file-only)",
			env:  layer{profile: &apogee.ModelProfile{ToolCallFormat: apogee.FormatCustomRegex}},
			flag: layer{profile: &apogee.ModelProfile{ToolCallFormat: apogee.FormatMarkdownFenced}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
		{
			name: "mechanisms are file-only (default empty)",
			file: layer{mechanisms: map[string]bool{"validate": true, "syntax": false}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, mechanisms: map[string]bool{"validate": true, "syntax": false}},
		},
		{
			name: "mechanisms are NOT settable by env or flag (file-only)",
			env:  layer{mechanisms: map[string]bool{"fromenv": true}},
			flag: layer{mechanisms: map[string]bool{"fromflag": true}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, notices := resolveSettings(tt.file, tt.env, tt.flag, testHostID)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveSettings = %+v; want %+v", got, tt.want)
			}
			if len(notices) != 0 {
				t.Errorf("resolveSettings notices = %q; want none for a well-formed config", notices)
			}
		})
	}
}

// The Host acknowledgement ladder (ADR 0012, amendment 2026-07-21), pinned in the order the
// ADR fixes: an explicit global false wins; else a match on THIS host's id loosens here; else
// confinement stays on. A malformed entry degrades softly with a notice, and an entry naming
// another machine is simply not this host — neither is an error. Step 2 additionally requires
// an identity to match: on a host that can supply none, the id stands for every such machine,
// so honouring it would let one saved acknowledgement loosen all of them.
func TestResolveConfineToWorkspace(t *testing.T) {
	t.Parallel()
	const otherHost = "laptop-9f8e7d"
	tests := []struct {
		name        string
		explicit    *bool
		hosts       []unconfinedHost
		hostID      string
		want        bool
		wantNotices int
	}{
		{name: "nothing configured → the secure default", hostID: testHostID, want: true},
		{name: "explicit global false → unconfined everywhere", explicit: boolptr(false), hostID: testHostID, want: false},
		{name: "explicit global true, no acknowledgement → confined", explicit: boolptr(true), hostID: testHostID, want: true},
		{
			name:   "this host is acknowledged → unconfined here",
			hosts:  []unconfinedHost{{ID: otherHost}, {ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable container"}},
			hostID: testHostID,
			want:   false,
		},
		{
			name:   "only other machines are acknowledged → still confined here",
			hosts:  []unconfinedHost{{ID: otherHost}, {ID: "buildbox-000111"}},
			hostID: testHostID,
			want:   true,
		},
		{
			name:     "an explicit true does not veto a match — the entry is the more specific claim",
			explicit: boolptr(true),
			hosts:    []unconfinedHost{{ID: testHostID}},
			hostID:   testHostID,
			want:     false,
		},
		{
			name:        "a malformed entry is skipped with a notice, the well-formed one still matches",
			hosts:       []unconfinedHost{{Note: "no id here"}, {ID: testHostID}},
			hostID:      testHostID,
			want:        false,
			wantNotices: 1,
		},
		{
			name:        "a blank id never matches a blank host id — it is malformed, not a wildcard",
			hosts:       []unconfinedHost{{ID: "   "}},
			hostID:      "",
			want:        true,
			wantNotices: 1,
		},
		{
			name:        "an identity-less host is not acknowledged by an entry naming its shared id",
			hosts:       []unconfinedHost{{ID: unidentifiedTestHostID, Acknowledged: "2026-07-21"}},
			hostID:      unidentifiedTestHostID,
			want:        true,
			wantNotices: 1,
		},
		{
			name:     "an explicit global false still loosens an identity-less host — step 1 is untouched",
			explicit: boolptr(false),
			hosts:    []unconfinedHost{{ID: unidentifiedTestHostID}},
			hostID:   unidentifiedTestHostID,
			want:     false,
			// The entry is still reported: the match was refused, and saying so is what keeps
			// the notice honest about why the id cannot stand for one machine.
			wantNotices: 1,
		},
		{
			name:   "an identity-less host with a real machine's entry is simply not that machine",
			hosts:  []unconfinedHost{{ID: otherHost}},
			hostID: unidentifiedTestHostID,
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !platform.IsUnidentifiedHostID(unidentifiedTestHostID) {
				t.Fatalf("%q is no longer the identity-less host id; the cases below prove nothing",
					unidentifiedTestHostID)
			}
			got, notices := resolveConfineToWorkspace(tt.explicit, tt.hosts, tt.hostID)
			if got != tt.want {
				t.Errorf("confineToWorkspace = %v; want %v", got, tt.want)
			}
			if len(notices) != tt.wantNotices {
				t.Errorf("notices = %q; want %d", notices, tt.wantNotices)
			}
			for _, n := range notices {
				if !strings.Contains(n, "unconfined-hosts") {
					t.Errorf("notice %q does not name the offending key", n)
				}
			}
		})
	}
}

// The unconfined-hosts block reaches opts end-to-end: an entry naming THIS machine (the real
// platform.HostID(), which is what production matches against) resolves the effective
// confine-to-workspace to false, and the list itself is carried on opts. This is the
// load-bearing host-scoping proof — the same config on any other machine stays confined.
func TestApplyConfigUnconfinedHosts(t *testing.T) {
	t.Parallel()
	noFlags := func(string) bool { return false }
	noEnv := func(string) string { return "" }

	t.Run("this host acknowledged", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		configYAML := "unconfined-hosts:\n  - id: \"" + platform.HostID() + "\"\n" +
			"    acknowledged: \"2026-07-21\"\n    note: \"disposable container\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		opts := options{configDir: home}
		if err := applyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
			t.Fatalf("applyConfig: %v", err)
		}
		if opts.confineToWorkspace {
			t.Error("opts.confineToWorkspace = true; want false — this host is acknowledged")
		}
		want := []unconfinedHost{{ID: platform.HostID(), Acknowledged: "2026-07-21", Note: "disposable container"}}
		if !reflect.DeepEqual(opts.unconfinedHosts, want) {
			t.Errorf("opts.unconfinedHosts = %+v; want %+v", opts.unconfinedHosts, want)
		}
	})

	t.Run("another host acknowledged", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		const configYAML = "unconfined-hosts:\n  - id: \"someone-elses-box-abc123\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		opts := options{configDir: home}
		if err := applyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
			t.Fatalf("applyConfig: %v", err)
		}
		if !opts.confineToWorkspace {
			t.Error("opts.confineToWorkspace = false; want true — the acknowledgement names another machine")
		}
	})

	t.Run("a malformed entry notifies and does not block startup", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		const configYAML = "unconfined-hosts:\n  - note: \"forgot the id\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		var got []string
		opts := options{configDir: home}
		if err := applyConfig(&opts, noFlags, noEnv, os.ReadFile, func(msg string) { got = append(got, msg) }); err != nil {
			t.Fatalf("applyConfig: %v; want a soft skip, not a blocked startup", err)
		}
		if !opts.confineToWorkspace {
			t.Error("opts.confineToWorkspace = false; want true — a malformed entry acknowledges nothing")
		}
		if len(got) != 1 || !strings.Contains(got[0], "unconfined-hosts") {
			t.Errorf("notices = %q; want one naming unconfined-hosts", got)
		}
	})
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

	if err := applyConfig(&opts, changed, getenv, os.ReadFile, noNotify); err != nil {
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

	if err := applyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.endpoint != "" || opts.model != "" || opts.bypass {
		t.Errorf("non-default endpoint/model/bypass: %+v", opts)
	}
	if opts.mode != string(modeAskBefore) {
		t.Errorf("mode = %q; want the default %q", opts.mode, modeAskBefore)
	}
	if !opts.autoCompact {
		t.Error("autoCompact = false; want the structural default true (auto-compaction on)")
	}
}

// The auto-compact config block parses into opts.autoCompact (item 9): a file-only, default-true
// key, so an explicit `auto-compact: false` is the only way to turn the structural trigger off.
func TestApplyConfigAutoCompactOptOut(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("auto-compact: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.autoCompact {
		t.Error("opts.autoCompact = true; want the file's explicit false to opt out")
	}
}

// The context-window config block parses into opts.contextWindow (item 3): a file-only key (no
// flag/env). This proves the config-file surface lands in opts; the downstream opts →
// ContextConfig.MaxContextTokens threading (which the Budget and Compaction bind against) is
// pinned separately by TestRunRootThreadsContextWindow in wire_test.go.
func TestApplyConfigContextWindow(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("context-window: 65536\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.contextWindow != 65536 {
		t.Errorf("opts.contextWindow = %d; want the file's explicit 65536", opts.contextWindow)
	}
}

// The mcp-servers config block parses into opts.mcpServers (P3.15): a stdio and an HTTP server,
// each mapped across to mcp.ServerConfig, so the composition root can connect them.
func TestApplyConfigMCPServers(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `mcp-servers:
  - name: github
    transport: stdio
    command: gh-mcp
    args: ["--stdio"]
    env: ["TOKEN=x"]
  - name: docs
    transport: streamable-http
    endpoint: https://mcp.example.com/
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := []mcp.ServerConfig{
		{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp", Args: []string{"--stdio"}, Env: []string{"TOKEN=x"}},
		{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"},
	}
	if !reflect.DeepEqual(opts.mcpServers, want) {
		t.Errorf("mcpServers = %+v; want %+v", opts.mcpServers, want)
	}
}

// The mechanisms config block parses into opts.mechanisms (Phase 4): a map of canonical ID →
// enabled, which runRoot drives the catalogue constructor table with. It is file-only, like
// mcp-servers, so this proves the config surface lands end-to-end.
func TestApplyConfigMechanisms(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `mechanisms:
  validate: true
  syntax: true
  truncate_history: false
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := map[string]bool{"validate": true, "syntax": true, "truncate_history": false}
	if !reflect.DeepEqual(opts.mechanisms, want) {
		t.Errorf("opts.mechanisms = %+v; want %+v", opts.mechanisms, want)
	}
}

// With no mechanisms block, opts.mechanisms is nil — every Mechanism default-off (D1), the
// byte-identical anchor: a config without the block behaves exactly as before.
func TestApplyConfigNoMechanismsIsNil(t *testing.T) {
	t.Parallel()
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.mechanisms != nil {
		t.Errorf("opts.mechanisms = %+v; want nil (no block ⇒ nothing enabled)", opts.mechanisms)
	}
}

// The validated-sets config block parses into opts.validatedSetsEnable / opts.validatedSetsAlias
// (ADR 0016 realisation): the §5 off-switch and the §3 explicit carry-over map. File-only, like
// mechanisms, so this proves the config surface lands end-to-end.
func TestApplyConfigValidatedSets(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `validated-sets:
  enable: false
  alias:
    gemma-4-e4b-it-qat: gemma-4-e4b-it-qat
    my-quant: gemma-4-e4b-it-qat
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	if opts.validatedSetsEnable {
		t.Errorf("opts.validatedSetsEnable = true; want false (explicit enable: false)")
	}
	wantAlias := map[string]string{"gemma-4-e4b-it-qat": "gemma-4-e4b-it-qat", "my-quant": "gemma-4-e4b-it-qat"}
	if !reflect.DeepEqual(opts.validatedSetsAlias, wantAlias) {
		t.Errorf("opts.validatedSetsAlias = %+v; want %+v", opts.validatedSetsAlias, wantAlias)
	}
}

// With no validated-sets block, the surface defaults ON with no aliases — a matching set
// applies (≥ medium confidence) or is offered (low) without any config.
func TestApplyConfigNoValidatedSetsDefaultsOn(t *testing.T) {
	t.Parallel()
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if !opts.validatedSetsEnable {
		t.Errorf("opts.validatedSetsEnable = false; want true (default on)")
	}
	if opts.validatedSetsAlias != nil {
		t.Errorf("opts.validatedSetsAlias = %+v; want nil", opts.validatedSetsAlias)
	}
}

// The model-profile config block reaches opts.profile — which runRoot folds directly into
// apogee.Config.Profile: a markdown-fenced tool-call format plus a <think> thinking block map
// across to the domain ModelProfile the loop translates to its parsers at the seam (item 1 has
// no loop consumer yet; this proves the config surface lands end-to-end).
func TestApplyConfigModelProfile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `model-profile:
  tool-call-format: markdown-fenced
  thinking:
    style: delimited
    start: "<think>"
    end: "</think>"
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := apogee.ModelProfile{
		ToolCallFormat: apogee.FormatMarkdownFenced,
		Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}
	if !reflect.DeepEqual(opts.profile, want) {
		t.Errorf("opts.profile = %+v; want %+v", opts.profile, want)
	}
}

// With no model-profile block, opts.profile is the zero ModelProfile — native tool calls with no
// inline thinking (today's behaviour), the byte-identical anchor this item must preserve.
func TestApplyConfigNoProfileIsZero(t *testing.T) {
	t.Parallel()
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if (opts.profile != apogee.ModelProfile{}) {
		t.Errorf("opts.profile = %+v; want the zero ModelProfile", opts.profile)
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
	if err := applyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
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
	if err := applyConfig(&opts, changed, getenv, os.ReadFile, noNotify); err != nil {
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
	err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
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
	err := applyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify)
	if err == nil {
		t.Fatal("invalid APOGEE_BYPASS: want an error, got nil")
	}
}

// With no model configured but an endpoint known, resolveModel asks the server and adopts
// the discovered id — the zero-config single-model case (llama.cpp's llama-server).
func TestResolveModelDiscoversWhenUnset(t *testing.T) {
	t.Parallel()
	discover := func(_ context.Context, endpoint string) (discoveredUpstream, error) {
		if endpoint != "http://server" {
			t.Errorf("discover called with endpoint %q; want the resolved endpoint", endpoint)
		}
		return discoveredUpstream{model: "discovered-model", contextWindow: 32768}, nil
	}
	opts := options{endpoint: "http://server"}
	got, probed, err := resolveModel(context.Background(), &opts, discover)
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if !probed {
		t.Error("probed = false; want true (discovery ran on the no-model path)")
	}
	if got != "discovered-model" || opts.model != "discovered-model" {
		t.Errorf("resolveModel = %q, opts.model = %q; want both %q", got, opts.model, "discovered-model")
	}
	if opts.contextWindow != 32768 {
		t.Errorf("opts.contextWindow = %d; want the discovered window 32768", opts.contextWindow)
	}
}

// A model resolved by a higher layer (flag/env/file) wins: discovery must not run and must
// not overwrite it.
func TestResolveModelSkipsWhenAlreadySet(t *testing.T) {
	t.Parallel()
	discover := func(context.Context, string) (discoveredUpstream, error) {
		t.Fatal("discover must not be called when a model is already configured")
		return discoveredUpstream{}, nil
	}
	opts := options{endpoint: "http://server", model: "m-configured"}
	got, probed, err := resolveModel(context.Background(), &opts, discover)
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if probed {
		t.Error("probed = true; want false (no discovery ran for a configured model)")
	}
	if got != "" {
		t.Errorf("resolveModel = %q; want \"\" (no discovery ran)", got)
	}
	if opts.model != "m-configured" {
		t.Errorf("opts.model = %q; want the configured value preserved", opts.model)
	}
}

// With no endpoint there is nothing to ask, so discovery is a silent no-op — construction
// then surfaces the missing-endpoint error, which is the real problem.
func TestResolveModelNoEndpointIsNoOp(t *testing.T) {
	t.Parallel()
	discover := func(context.Context, string) (discoveredUpstream, error) {
		t.Fatal("discover must not be called with no endpoint")
		return discoveredUpstream{}, nil
	}
	opts := options{}
	got, probed, err := resolveModel(context.Background(), &opts, discover)
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if probed {
		t.Error("probed = true; want false (no discovery ran with no endpoint)")
	}
	if got != "" || opts.model != "" {
		t.Errorf("resolveModel = %q, opts.model = %q; want both empty", got, opts.model)
	}
}

// A discovery failure is surfaced (not swallowed) so the user learns the server is
// unreachable or advertises no model, and the underlying error is wrapped for errors.Is.
func TestResolveModelDiscoveryErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection refused")
	discover := func(context.Context, string) (discoveredUpstream, error) { return discoveredUpstream{}, boom }
	opts := options{endpoint: "http://server"}
	_, _, err := resolveModel(context.Background(), &opts, discover)
	if err == nil {
		t.Fatal("discovery failure: want an error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap the underlying discovery error", err)
	}
	if opts.model != "" {
		t.Errorf("opts.model = %q; want it left empty on failure", opts.model)
	}
}

// On the no-model path a context-window: key (opts.contextWindow already set) WINS over the window
// the server advertises: resolveModel still discovers and adopts the model id, but the pre-set
// window is kept — the key is the Budget/auto-compaction escape hatch, so discovery must not
// clobber it (the `if opts.contextWindow == 0` guard in resolveModel's no-model path; item 5).
// Without this case, mutating that branch to unconditionally take the advertised window survives.
func TestResolveModelContextWindowKeyWinsOverDiscovery(t *testing.T) {
	t.Parallel()
	discover := func(_ context.Context, endpoint string) (discoveredUpstream, error) {
		if endpoint != "http://server" {
			t.Errorf("discover called with endpoint %q; want the resolved endpoint", endpoint)
		}
		return discoveredUpstream{model: "discovered-model", contextWindow: 131072}, nil
	}
	opts := options{endpoint: "http://server", contextWindow: 16384} // the context-window: key, no model set
	got, probed, err := resolveModel(context.Background(), &opts, discover)
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if !probed {
		t.Error("probed = false; want true (discovery still runs on the no-model path)")
	}
	if got != "discovered-model" || opts.model != "discovered-model" {
		t.Errorf("resolveModel = %q, opts.model = %q; want both %q (the discovered id is kept)", got, opts.model, "discovered-model")
	}
	if opts.contextWindow != 16384 {
		t.Errorf("opts.contextWindow = %d; want the context-window: key 16384 kept (it wins over the advertised 131072)", opts.contextWindow)
	}
}

// The no-model startup sequence probes the server exactly once even when it advertises no window
// (contextWindow: 0): resolveModel discovers the model and the window in one probe, and root.go
// skips resolveContextWindow because that discovery already ran — so no redundant window probe
// fires on the no-model path (item 4). The loud-zero notice still surfaces once, because the window
// stayed unknown.
func TestNoModelPathProbesOnce(t *testing.T) {
	t.Parallel()
	var probes int
	discover := func(_ context.Context, _ string) (discoveredUpstream, error) {
		probes++
		return discoveredUpstream{model: "discovered-model", contextWindow: 0}, nil
	}
	opts := options{endpoint: "http://server"}
	// Mirror root.go's startup sequence: resolveModel first, then resolveContextWindow only when
	// model discovery did NOT run this startup (the item-4 gate).
	_, probed, err := resolveModel(context.Background(), &opts, discover)
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if !probed {
		resolveContextWindow(context.Background(), &opts, discover, func(string) {
			t.Error("notify must not fire: resolveContextWindow must be skipped when discovery already ran")
		})
	}
	if probes != 1 {
		t.Errorf("discover called %d times; want exactly one probe for the whole no-model startup", probes)
	}
	if opts.contextWindow != 0 {
		t.Errorf("opts.contextWindow = %d; want 0 (server advertised no window)", opts.contextWindow)
	}
	if contextWindowNotice(opts.contextWindow, true) == "" {
		t.Error("loud-zero notice should still appear once when the window stayed unknown")
	}
}

// A PINNED model (configured id) still gets its context window discovered — resolveModel
// early-returns on the id, so resolveContextWindow probes for the window (item 3 / S3). The
// pinned id is kept regardless; only the window is adopted, so the Budget is no longer disabled.
func TestResolveContextWindowPinnedModelDiscovers(t *testing.T) {
	t.Parallel()
	discover := func(_ context.Context, endpoint string) (discoveredUpstream, error) {
		if endpoint != "http://server" {
			t.Errorf("discover called with endpoint %q; want the resolved endpoint", endpoint)
		}
		return discoveredUpstream{model: "server-says-other", contextWindow: 32768}, nil
	}
	opts := options{endpoint: "http://server", model: "m-pinned"}
	resolveContextWindow(context.Background(), &opts, discover, func(string) {
		t.Error("notify must not fire on a successful probe")
	})
	if opts.model != "m-pinned" {
		t.Errorf("opts.model = %q; want the pinned id kept regardless of what the probe reports", opts.model)
	}
	if opts.contextWindow != 32768 {
		t.Errorf("opts.contextWindow = %d; want the discovered window 32768", opts.contextWindow)
	}
}

// Window discovery for a pinned model is NEVER fatal: a failed probe leaves the window unknown (0)
// and emits a one-line notice, so an offline pinned-model start still works (item 3 / S3).
func TestResolveContextWindowDiscoveryNonFatal(t *testing.T) {
	t.Parallel()
	discover := func(context.Context, string) (discoveredUpstream, error) {
		return discoveredUpstream{}, errors.New("connection refused")
	}
	opts := options{endpoint: "http://server", model: "m-pinned"}
	var notices []string
	resolveContextWindow(context.Background(), &opts, discover, func(s string) { notices = append(notices, s) })
	if opts.contextWindow != 0 {
		t.Errorf("opts.contextWindow = %d; want 0 (window left unknown on a failed probe)", opts.contextWindow)
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %v; want exactly one non-fatal notice", notices)
	}
	if !strings.Contains(notices[0], "context-window") {
		t.Errorf("notice %q does not name the context-window key", notices[0])
	}
}

// A context-window: key (opts.contextWindow already > 0) wins and skips the probe entirely — the
// discoverer must not be called (item 3).
func TestResolveContextWindowKeySkipsProbe(t *testing.T) {
	t.Parallel()
	discover := func(context.Context, string) (discoveredUpstream, error) {
		t.Fatal("discover must not be called when a context-window is already configured")
		return discoveredUpstream{}, nil
	}
	opts := options{endpoint: "http://server", model: "m-pinned", contextWindow: 8192}
	resolveContextWindow(context.Background(), &opts, discover, func(string) {
		t.Error("notify must not fire when the probe is skipped")
	})
	if opts.contextWindow != 8192 {
		t.Errorf("opts.contextWindow = %d; want the configured key value preserved", opts.contextWindow)
	}
}

// With no endpoint there is nothing to ask, so window discovery is a silent no-op (item 3).
func TestResolveContextWindowNoEndpointNoOp(t *testing.T) {
	t.Parallel()
	discover := func(context.Context, string) (discoveredUpstream, error) {
		t.Fatal("discover must not be called with no endpoint")
		return discoveredUpstream{}, nil
	}
	opts := options{model: "m-pinned"}
	resolveContextWindow(context.Background(), &opts, discover, func(string) {
		t.Error("notify must not fire with no endpoint")
	})
	if opts.contextWindow != 0 {
		t.Errorf("opts.contextWindow = %d; want 0 (no probe ran)", opts.contextWindow)
	}
}

// An absent config file is not an error — a config file is optional.
func TestLoadFileConfigAbsentIsEmpty(t *testing.T) {
	t.Parallel()
	l, err := loadFileConfig(filepath.Join(t.TempDir(), "config.yaml"), os.ReadFile)
	if err != nil {
		t.Fatalf("absent config: unexpected error %v", err)
	}
	if !reflect.DeepEqual(l, layer{}) {
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
