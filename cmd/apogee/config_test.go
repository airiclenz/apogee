package main

import (
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
	"github.com/airiclenz/apogee/internal/tui"
)

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }
func intptr(i int) *int       { return &i }

// noNotify drops applyConfig's soft startup notices — the tests below assert resolved
// values, not the wording (resolveConfineToWorkspace's own table covers the notices).
func noNotify(string) {}

// wantUIDefault is the resolved `ui:` block a config that configures none must produce: the
// default spinner style with its colour loop on. It is spelled out rather than taken from
// defaultUISettings, so a change to either shipped default shows up here as a failure instead of
// silently agreeing with itself.
var wantUIDefault = uiSettings{spinner: tui.SpinnerSnake, spinnerColor: true}

// wantContextFilesDefault is the resolved `context-files:` block a config that configures none must
// produce: the feature on, looking for the one default name in the workspace root. Spelled out
// rather than taken from defaultContextFilesSettings, like wantUIDefault above, so a change to
// either shipped default shows up here as a failure instead of silently agreeing with itself.
var wantContextFilesDefault = contextFilesSettings{enable: true, names: []string{"AGENTS.md"}}

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
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "file fills every field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan"), bypass: boolptr(true)},
			want: settings{endpoint: "http://file", model: "m-file", mode: "plan", bypass: true, confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "env beats file, file fills the rest",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file")},
			env:  layer{endpoint: strptr("http://env")},
			want: settings{endpoint: "http://env", model: "m-file", mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "flag beats env beats file, per field",
			file: layer{endpoint: strptr("http://file"), model: strptr("m-file"), mode: strptr("plan")},
			env:  layer{endpoint: strptr("http://env"), model: strptr("m-env")},
			flag: layer{endpoint: strptr("http://flag")},
			want: settings{endpoint: "http://flag", model: "m-env", mode: "plan", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "explicit false in a higher layer overrides true below it",
			file: layer{bypass: boolptr(true)},
			flag: layer{bypass: boolptr(false)},
			want: settings{mode: "ask-before", bypass: false, confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "api-key comes from the file when the environment sets none",
			file: fileConfig{APIKey: "file-secret"}.layer(),
			want: settings{mode: "ask-before", apiKey: "file-secret", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			// The key is settable by the file and by the environment, and by nothing else: even
			// with an "api-key" flag reported as explicitly set, flagLayer projects no key —
			// there is no --api-key to bind, because a secret on the command line lands in shell
			// history and in `ps` output. So the env value stands over the file's.
			name: "api-key: env beats file, and the flag layer cannot carry one at all",
			file: fileConfig{APIKey: "file-secret"}.layer(),
			env:  layer{apiKey: strptr("env-secret")},
			flag: flagLayer(options{apiKey: "flag-secret"}, func(name string) bool { return name == "api-key" }),
			want: settings{mode: "ask-before", apiKey: "env-secret", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "confine-to-workspace is file-only and defaults true",
			file: layer{confineToWorkspace: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: false, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "use-project-skills is file-only and defaults true",
			file: layer{useProjectSkills: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: false, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "use-project-skills is NOT set by env or flag (file-only)",
			env:  layer{useProjectSkills: boolptr(false)},
			flag: layer{useProjectSkills: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "auto-compact is file-only and defaults true",
			file: layer{autoCompact: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: false, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "auto-compact is NOT set by env or flag (file-only)",
			env:  layer{autoCompact: boolptr(false)},
			flag: layer{autoCompact: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "context-window is file-only (default 0 ⇒ discover)",
			file: layer{contextWindow: intptr(65536)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, contextWindow: 65536},
		},
		{
			name: "context-window is NOT set by env or flag (file-only)",
			env:  layer{contextWindow: intptr(65536)},
			flag: layer{contextWindow: intptr(65536)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "confine-to-workspace is NOT loosenable by env or flag (global-config-only)",
			env:  layer{confineToWorkspace: boolptr(false)}, // an env layer cannot carry it in practice; assert it is ignored even if set
			flag: layer{confineToWorkspace: boolptr(false)},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "a matching unconfined-hosts entry resolves confine-to-workspace to false",
			file: layer{unconfinedHosts: []unconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
			want: settings{mode: "ask-before", confineToWorkspace: false, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault,
				unconfinedHosts: []unconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
		},
		{
			name: "unconfined-hosts is NOT settable by env or flag (global-config-only)",
			env:  layer{unconfinedHosts: []unconfinedHost{{ID: testHostID}}},
			flag: layer{unconfinedHosts: []unconfinedHost{{ID: testHostID}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "web-search endpoint is file-only (default empty)",
			file: layer{webSearchEndpoint: strptr("https://search.example.com")},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, webSearchEndpoint: "https://search.example.com"},
		},
		{
			name: "mcp servers are file-only (default empty)",
			file: layer{mcpServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, mcpServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
		},
		{
			name: "mcp servers are NOT settable by env or flag (file-only)",
			env:  layer{mcpServers: []mcp.ServerConfig{{Name: "fromenv"}}},
			flag: layer{mcpServers: []mcp.ServerConfig{{Name: "fromflag"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "servers are file-only (default empty)",
			file: layer{servers: []serverEntry{{Name: "workstation", Endpoint: "http://box:1111"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, servers: []serverEntry{{Name: "workstation", Endpoint: "http://box:1111"}}},
		},
		{
			name: "servers are NOT settable by env or flag (file-only)",
			env:  layer{servers: []serverEntry{{Name: "fromenv", Endpoint: "http://env:1111"}}},
			flag: layer{servers: []serverEntry{{Name: "fromflag", Endpoint: "http://flag:1111"}}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "model profile is file-only (default zero)",
			file: layer{profile: &apogee.ModelProfile{
				ToolCallFormat: apogee.FormatMarkdownFenced,
				Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, profile: apogee.ModelProfile{
				ToolCallFormat: apogee.FormatMarkdownFenced,
				Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
		},
		{
			name: "model profile is NOT settable by env or flag (file-only)",
			env:  layer{profile: &apogee.ModelProfile{ToolCallFormat: apogee.FormatCustomRegex}},
			flag: layer{profile: &apogee.ModelProfile{ToolCallFormat: apogee.FormatMarkdownFenced}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "mechanisms are file-only (default empty)",
			file: layer{mechanisms: map[string]bool{"validate": true, "syntax": false}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault, mechanisms: map[string]bool{"validate": true, "syntax": false}},
		},
		{
			name: "mechanisms are NOT settable by env or flag (file-only)",
			env:  layer{mechanisms: map[string]bool{"fromenv": true}},
			flag: layer{mechanisms: map[string]bool{"fromflag": true}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "the present block is file-only (all four keys)",
			file: layer{present: &presentSettings{autoOpen: false, command: "zed {path}", port: 8934, host: "10.0.0.2"}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault,
				present: presentSettings{autoOpen: false, command: "zed {path}", port: 8934, host: "10.0.0.2"}, ui: wantUIDefault},
		},
		{
			name: "present is NOT settable by env or flag (file-only ⇒ auto-open stays on)",
			env:  layer{present: &presentSettings{autoOpen: false, port: 1}},
			flag: layer{present: &presentSettings{autoOpen: false, port: 2}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "the ui block is file-only (both keys)",
			file: fileConfig{UI: &uiConfig{Spinner: "glitter", SpinnerColor: boolptr(false)}}.layer(),
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true},
				ui: uiSettings{spinner: tui.SpinnerGlitter, spinnerColor: false}},
		},
		{
			// The two keys are independent: naming a style says nothing about the colour loop.
			name: "ui with only spinner: set → the colour loop stays at its default",
			file: fileConfig{UI: &uiConfig{Spinner: "classic"}}.layer(),
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true},
				ui: uiSettings{spinner: tui.SpinnerClassic, spinnerColor: true}},
		},
		{
			// …and the other way round: turning the loop off does not change which style paints.
			name: "ui with only spinner-color: false → the style stays at its default",
			file: fileConfig{UI: &uiConfig{SpinnerColor: boolptr(false)}}.layer(),
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true},
				ui: uiSettings{spinner: tui.SpinnerSnake, spinnerColor: false}},
		},
		{
			name: "ui is NOT settable by env or flag (file-only ⇒ the defaults hold)",
			env:  layer{ui: &uiSettings{spinner: tui.SpinnerClassic}},
			flag: layer{ui: &uiSettings{spinner: tui.SpinnerGlitter}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
		},
		{
			name: "a context-files block replaces the default name list whole",
			file: fileConfig{ContextFiles: &contextFilesConfig{Names: []string{"CONVENTIONS.md", "AGENTS.md"}}}.layer(),
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, present: presentSettings{autoOpen: true}, ui: wantUIDefault,
				contextFiles: contextFilesSettings{enable: true, names: []string{"CONVENTIONS.md", "AGENTS.md"}}},
		},
		{
			name: "context-files is NOT settable by env or flag (file-only ⇒ the defaults hold)",
			env:  layer{contextFiles: &contextFilesSettings{enable: true, names: []string{"from-env.md"}}},
			flag: layer{contextFiles: &contextFilesSettings{enable: false}},
			want: settings{mode: "ask-before", confineToWorkspace: true, useProjectSkills: true, autoCompact: true, validatedSetsEnable: true, contextFiles: wantContextFilesDefault, present: presentSettings{autoOpen: true}, ui: wantUIDefault},
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
	if opts.apiKey != "" {
		t.Errorf("apiKey = %q; want empty — an unconfigured key sends no Authorization header", opts.apiKey)
	}
}

// The api-key surface end-to-end through applyConfig: the config file sets it, APOGEE_API_KEY
// beats the file, and a key configured nowhere resolves empty — which is what makes a keyless
// local server behave exactly as it did before the key existed (no Authorization header). No flag
// appears in the table because there is no --api-key flag: a secret passed on the command line
// would land in shell history and in `ps` output.
func TestApplyConfigAPIKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fileYAML string
		env      string
		want     string
	}{
		{name: "the file alone", fileYAML: "api-key: file-secret\n", want: "file-secret"},
		{name: "the environment alone", env: "env-secret", want: "env-secret"},
		{
			name:     "the environment beats the file",
			fileYAML: "api-key: file-secret\n",
			env:      "env-secret",
			want:     "env-secret",
		},
		{name: "configured nowhere → empty (no auth header)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if tt.fileYAML != "" {
				if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.fileYAML), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			getenv := func(k string) string {
				if k == envAPIKey {
					return tt.env
				}
				return ""
			}
			opts := options{configDir: home}
			if err := applyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if opts.apiKey != tt.want {
				t.Errorf("opts.apiKey = %q; want %q", opts.apiKey, tt.want)
			}
		})
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

// The servers config block parses into opts.servers: every entry, in file order, with all four
// fields — so the composition root can offer them as the servers this session may move to. It is
// file-only, like mcp-servers, and the two optional keys default empty (a keyless server with no
// model hint), which is what a plain local entry looks like.
func TestApplyConfigServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       []serverEntry
	}{
		{
			name:       "no config file at all ⇒ no servers",
			configYAML: "",
			want:       nil,
		},
		{
			name:       "a config without the block ⇒ no servers",
			configYAML: "model: fake\n",
			want:       nil,
		},
		{
			name: "every entry resolves in file order, with all four fields",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    model: gpt-oss-20b
  - name: rented-box
    endpoint: https://llm.example.com
    api-key: sk-rented-token
    model: qwen2.5-coder
`,
			want: []serverEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", Model: "gpt-oss-20b"},
				{Name: "rented-box", Endpoint: "https://llm.example.com", APIKey: "sk-rented-token", Model: "qwen2.5-coder"},
			},
		},
		{
			name:       "api-key and model are optional (a keyless server, no hint)",
			configYAML: "servers:\n  - name: laptop\n    endpoint: http://127.0.0.1:1111\n",
			want:       []serverEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if tt.configYAML != "" {
				if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			opts := options{configDir: home}
			if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.servers, tt.want) {
				t.Errorf("opts.servers = %#v; want %#v", opts.servers, tt.want)
			}
		})
	}
}

// An entry that could never be switched to is a loud startup error that counts the offending entry
// out — not a row that fails at the moment of the switch, and not a name that silently resolves to
// whichever entry came first.
func TestApplyConfigServersInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		wantErr    []string
	}{
		{
			name:       "an entry with no name",
			configYAML: "servers:\n  - endpoint: http://box:1111\n",
			wantErr:    []string{"servers: entry 1", "http://box:1111", "has no name"},
		},
		{
			name:       "an entry whose name is only whitespace",
			configYAML: "servers:\n  - name: \"   \"\n    endpoint: http://box:1111\n",
			wantErr:    []string{"servers: entry 1", "has no name"},
		},
		{
			name:       "an entry with no endpoint",
			configYAML: "servers:\n  - name: workstation\n",
			wantErr:    []string{"servers: entry 1", "workstation", "has no endpoint"},
		},
		{
			name:       "an entry whose endpoint is only whitespace",
			configYAML: "servers:\n  - name: workstation\n    endpoint: \"  \"\n",
			wantErr:    []string{"servers: entry 1", "workstation", "has no endpoint"},
		},
		{
			name:       "two entries sharing one name",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n  - name: box\n    endpoint: http://two:1111\n",
			wantErr:    []string{"servers: entry 2", "box", "already has that name"},
		},
		{
			// The whole list is checked, so a defect below a usable entry is still named.
			name:       "a defect after a well-formed entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n  - name: other\n",
			wantErr:    []string{"servers: entry 2", "other", "has no endpoint"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := options{configDir: home}
			err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("applyConfig = nil error; want the entry refused (opts.servers = %#v)", opts.servers)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err, want)
				}
			}
		})
	}
}

// The mechanisms config block parses into opts.mechanisms (Phase 4): a map of canonical ID →
// enabled, whose enabled IDs runRoot hands to Config.EnableMechanisms for the engine to build
// catalogue rows from. It is file-only, like mcp-servers, so this proves the config surface lands
// end-to-end.
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

// The present config block parses into opts.present (ADR 0019): all four keys, file-only like
// the blocks around it, so the composition root can build the ladder's mechanisms from them.
func TestApplyConfigPresent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `present:
  auto-open: false
  command: "zed {path}"
  port: 8934
  host: 192.168.64.2
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := presentSettings{autoOpen: false, command: "zed {path}", port: 8934, host: "192.168.64.2"}
	if opts.present != want {
		t.Errorf("opts.present = %+v; want %+v", opts.present, want)
	}
}

// A present block that sets ONE key leaves the other three at their defaults — the reason
// auto-open is a pointer on the on-disk schema. Setting `port:` alone must not read as
// `auto-open: false` and silently disable the rung the feature exists for.
func TestApplyConfigPresentPartialKeepsDefaults(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = "present:\n  port: 8934\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := presentSettings{autoOpen: true, port: 8934}
	if opts.present != want {
		t.Errorf("opts.present = %+v; want %+v (an absent auto-open keeps the default)", opts.present, want)
	}
}

// With no present block at all, the ladder's defaults stand: auto-open ON (the headline want —
// a run on the user's own desktop opens the deliverable), no command override, an ephemeral
// port, and a detected advertise host.
func TestApplyConfigNoPresentDefaultsAutoOpen(t *testing.T) {
	t.Parallel()
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if want := (presentSettings{autoOpen: true}); opts.present != want {
		t.Errorf("opts.present = %+v; want %+v (no block ⇒ auto-open on, everything else zero)", opts.present, want)
	}
}

// An out-of-range present.port is a loud startup error naming the key, not a degraded rung at
// the first presentation: the doc server would fail at the listen, deep inside a Turn, where all
// the user would see is "could not serve" with no hint that a config typo caused it.
func TestApplyConfigPresentPortRangeErrors(t *testing.T) {
	t.Parallel()
	for _, port := range []string{"-1", "65536", "99999"} {
		home := t.TempDir()
		configYAML := "present:\n  port: " + port + "\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		opts := options{configDir: home}
		err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
		if err == nil {
			t.Errorf("applyConfig with present.port %s: want an error, got nil", port)
			continue
		}
		if !strings.Contains(err.Error(), "present.port") {
			t.Errorf("error = %q; want it to name the present.port key", err)
		}
	}

	// The boundaries themselves are valid: 0 (ephemeral, the default) and 65535.
	for _, port := range []string{"0", "65535"} {
		home := t.TempDir()
		configYAML := "present:\n  port: " + port + "\n"
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		opts := options{configDir: home}
		if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
			t.Errorf("applyConfig with present.port %s: %v", port, err)
		}
	}
}

// The three system-prompt keys parse into opts.systemPrompt (ADR 0023): the global prompt and the
// per-model overrides, file-only like the blocks around them. This is the end-to-end proof that
// the keys reach the composition root, which is where resolveSystemPrompt then selects one.
func TestApplyConfigSystemPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       systemPromptSettings
	}{
		{
			name:       "an inline prompt reaches the global source",
			configYAML: "system-prompt-text: \"hi {{workspace}}\"\n",
			want:       systemPromptSettings{global: promptSource{text: "hi {{workspace}}"}},
		},
		{
			// Every key spelling at once, in the only shape that is legal: text and file at ONE
			// level contradict each other (validate refuses that), so the global carries the file
			// spelling and the per-model entries carry one each.
			name: "a global file and both per-model spellings populate every field",
			configYAML: `system-prompt-file: prompts/global.md
system-prompt-models:
  qwen2.5-coder:
    system-prompt-text: "code first, prose second"
  gpt-oss-20b:
    system-prompt-file: ~/prompts/gpt-oss.md
`,
			want: systemPromptSettings{
				global: promptSource{file: "prompts/global.md"},
				models: map[string]promptSource{
					"qwen2.5-coder": {text: "code first, prose second"},
					"gpt-oss-20b":   {file: "~/prompts/gpt-oss.md"},
				},
			},
		},
		{
			name:       "no system-prompt key leaves the zero value — no prompt",
			configYAML: "model: fake\n",
			want:       systemPromptSettings{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := options{configDir: home}
			if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.systemPrompt, tt.want) {
				t.Errorf("opts.systemPrompt = %+v; want %+v", opts.systemPrompt, tt.want)
			}
		})
	}
}

// The structural half of the system-prompt checks (ADR 0023): the contradictions that are defects
// in the FILE, independent of this machine and of the model that will be resolved — so they are
// refused for every level, including entries this host will never select. Whether a file reads and
// whether its placeholders are known belong to the selected source alone (TestResolveSystemPrompt).
func TestSystemPromptSettingsValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sp   systemPromptSettings
		// wantErr are the substrings the message must carry; empty ⇒ the block must validate.
		wantErr []string
	}{
		{name: "an inline prompt alone", sp: systemPromptSettings{global: promptSource{text: "hi"}}},
		{name: "a file prompt alone", sp: systemPromptSettings{global: promptSource{file: "p.md"}}},
		{name: "nothing configured at all", sp: systemPromptSettings{}},
		{
			name:    "both spellings at the global level",
			sp:      systemPromptSettings{global: promptSource{text: "hi", file: "p.md"}},
			wantErr: []string{"system-prompt-text", "system-prompt-file", "both"},
		},
		{
			name:    "both spellings in one model entry",
			sp:      systemPromptSettings{models: map[string]promptSource{"qwen2.5-coder": {text: "hi", file: "p.md"}}},
			wantErr: []string{`system-prompt-models["qwen2.5-coder"]`, "system-prompt-text", "system-prompt-file"},
		},
		{
			name:    "a model entry that sets neither spelling",
			sp:      systemPromptSettings{models: map[string]promptSource{"qwen2.5-coder": {}}},
			wantErr: []string{`system-prompt-models["qwen2.5-coder"]`, "neither"},
		},
		{
			name: "a well-formed model entry beside a global prompt",
			sp: systemPromptSettings{
				global: promptSource{text: "hi"},
				models: map[string]promptSource{"qwen2.5-coder": {file: "p.md"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.sp.validate()
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("validate: %v; want the block to validate", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validate: want an error, got nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err, want)
				}
			}
		})
	}
}

// resolveSystemPrompt collapses the block into the ONE template this session runs with, for the
// RESOLVED model (ADR 0023): whole-entry replacement on an exact model match, an inert entry for
// every other model, `~` and apogee-home-relative file paths, and the two checks that belong to
// the selected source alone — the file must read and the placeholders must be the known three.
func TestResolveSystemPrompt(t *testing.T) {
	// Deliberately NOT parallel: the `~` case redirects the environment os.UserHomeDir reads.
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)        // POSIX
	t.Setenv("USERPROFILE", userHome) // Windows
	home := t.TempDir()               // the apogee home a relative path resolves against
	absFile := filepath.Join(t.TempDir(), "absolute.md")

	files := map[string]string{
		absFile: "from an absolute path",
		filepath.Join(home, "prompts", "relative.md"):  "from the apogee home",
		filepath.Join(userHome, "prompts", "user.md"):  "from the user home",
		filepath.Join(home, "prompts", "per-model.md"): "the per-model file",
	}
	readFile := func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}

	const model = "gpt-oss-20b"
	tests := []struct {
		name string
		sp   systemPromptSettings
		want string
		// wantErr are the substrings the message must carry; empty ⇒ want must be returned.
		wantErr []string
	}{
		{
			name: "the global inline prompt is selected",
			sp:   systemPromptSettings{global: promptSource{text: "hi {{workspace}}"}},
			want: "hi {{workspace}}",
		},
		{
			name: "a matching model entry replaces the global prompt",
			sp: systemPromptSettings{
				global: promptSource{text: "the global prompt"},
				models: map[string]promptSource{model: {text: "the per-model prompt"}},
			},
			want: "the per-model prompt",
		},
		{
			name: "a matching entry with only a file replaces a global text whole",
			sp: systemPromptSettings{
				global: promptSource{text: "the global prompt"},
				models: map[string]promptSource{model: {file: "prompts/per-model.md"}},
			},
			want: "the per-model file",
		},
		{
			name: "an entry naming another model is inert and its file is never read",
			sp: systemPromptSettings{
				global: promptSource{text: "the global prompt"},
				models: map[string]promptSource{"some-other-model": {file: "prompts/absent.md"}},
			},
			want: "the global prompt",
		},
		{name: "no prompt configured anywhere", sp: systemPromptSettings{}, want: ""},
		{
			name: "an absolute file is read as written",
			sp:   systemPromptSettings{global: promptSource{file: absFile}},
			want: "from an absolute path",
		},
		{
			name: "a relative file resolves against the apogee home",
			sp:   systemPromptSettings{global: promptSource{file: filepath.Join("prompts", "relative.md")}},
			want: "from the apogee home",
		},
		{
			name: "a ~-prefixed file resolves against the user home",
			sp:   systemPromptSettings{global: promptSource{file: "~/prompts/user.md"}},
			want: "from the user home",
		},
		{
			name:    "an unreadable selected file names the key and the path",
			sp:      systemPromptSettings{global: promptSource{file: filepath.Join("prompts", "absent.md")}},
			wantErr: []string{"system-prompt-file", filepath.Join(home, "prompts", "absent.md")},
		},
		{
			name:    "an unknown placeholder names the source key and the known three",
			sp:      systemPromptSettings{global: promptSource{text: "hi {{bogus}}"}},
			wantErr: []string{"system-prompt-text", "{{bogus}}", "{{workspace}}", "{{datetime}}", "{{mode}}"},
		},
		{
			name:    "an unknown placeholder in a model entry names that entry",
			sp:      systemPromptSettings{models: map[string]promptSource{model: {text: "hi {{nope}}"}}},
			wantErr: []string{`system-prompt-models["` + model + `"]`, "{{nope}}", "{{workspace}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSystemPrompt(tt.sp, model, home, readFile)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("resolveSystemPrompt: %v", err)
				}
				if got != tt.want {
					t.Errorf("template = %q; want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveSystemPrompt = %q; want an error", got)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err, want)
				}
			}
		})
	}
}

// The context-files block parses into opts.contextFiles — the RESOLVED name list the composition
// root folds into apogee.Config.ContextFiles. The table walks every shape of the block, including
// the two spellings of "off" (which both collapse to nil, the engine's contract for the feature
// being off) and the no-block default that is the whole point: a repo carrying an AGENTS.md works
// with nothing configured.
func TestApplyConfigContextFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       []string
	}{
		{
			name:       "no config file at all ⇒ the default name",
			configYAML: "",
			want:       []string{"AGENTS.md"},
		},
		{
			name:       "a config without the block ⇒ the default name",
			configYAML: "model: fake\n",
			want:       []string{"AGENTS.md"},
		},
		{
			name:       "enable: false ⇒ off",
			configYAML: "context-files:\n  enable: false\n",
			want:       nil,
		},
		{
			name:       "an explicitly empty list ⇒ off (absent is not empty)",
			configYAML: "context-files:\n  names: []\n",
			want:       nil,
		},
		{
			name:       "enable: false wins over a list",
			configYAML: "context-files:\n  enable: false\n  names: [AGENTS.md, CONVENTIONS.md]\n",
			want:       nil,
		},
		{
			name:       "enable alone keeps the default name",
			configYAML: "context-files:\n  enable: true\n",
			want:       []string{"AGENTS.md"},
		},
		{
			name:       "names alone leaves the switch on",
			configYAML: "context-files:\n  names: [CONVENTIONS.md]\n",
			want:       []string{"CONVENTIONS.md"},
		},
		{
			name:       "a custom list is preserved in list order",
			configYAML: "context-files:\n  names: [AGENTS.md, docs/house-style.md, .apogee/context.md]\n",
			want:       []string{"AGENTS.md", "docs/house-style.md", ".apogee/context.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if tt.configYAML != "" {
				if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			opts := options{configDir: home}
			if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.contextFiles, tt.want) {
				t.Errorf("opts.contextFiles = %#v; want %#v", opts.contextFiles, tt.want)
			}
		})
	}
}

// A name that cannot be a workspace file is a loud startup error naming `context-files.names` and
// the value, not a lookup that quietly reaches outside the workspace (or folds one file in twice).
// Every case is machine-independent — the Windows spellings are refused on every OS, so a config
// that travels is refused where it was written rather than where it lands.
func TestApplyConfigContextFilesInvalidNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		wantErr    []string
	}{
		{
			name:       "an empty entry",
			configYAML: "context-files:\n  names: [\"\"]\n",
			wantErr:    []string{"context-files.names", "empty"},
		},
		{
			name:       "a whitespace-only entry",
			configYAML: "context-files:\n  names: [\"   \"]\n",
			wantErr:    []string{"context-files.names", "empty"},
		},
		{
			name:       "an absolute path",
			configYAML: "context-files:\n  names: [/etc/passwd]\n",
			wantErr:    []string{"context-files.names", "/etc/passwd", "workspace-relative"},
		},
		{
			name:       "a Windows drive-relative name",
			configYAML: "context-files:\n  names: [\"C:AGENTS.md\"]\n",
			wantErr:    []string{"context-files.names", "C:AGENTS.md", "workspace-relative"},
		},
		{
			name:       "a name rooted at the current drive",
			configYAML: "context-files:\n  names: [\"\\\\AGENTS.md\"]\n",
			wantErr:    []string{"context-files.names", "workspace-relative"},
		},
		{
			name:       "the parent directory itself",
			configYAML: "context-files:\n  names: [..]\n",
			wantErr:    []string{"context-files.names", "climbs out of the workspace"},
		},
		{
			name:       "a walk-up out of the workspace",
			configYAML: "context-files:\n  names: [../secrets.md]\n",
			wantErr:    []string{"context-files.names", "../secrets.md", "climbs out of the workspace"},
		},
		{
			name:       "a walk-up that only cancels out on the way",
			configYAML: "context-files:\n  names: [docs/../../secrets.md]\n",
			wantErr:    []string{"context-files.names", "climbs out of the workspace"},
		},
		{
			name:       "a backslash-spelled walk-up",
			configYAML: "context-files:\n  names: [\"..\\\\secrets.md\"]\n",
			wantErr:    []string{"context-files.names", "climbs out of the workspace"},
		},
		{
			name:       "the same name twice",
			configYAML: "context-files:\n  names: [AGENTS.md, AGENTS.md]\n",
			wantErr:    []string{"context-files.names", "AGENTS.md", "listed twice"},
		},
		{
			name:       "the same name twice in two spellings",
			configYAML: "context-files:\n  names: [AGENTS.md, ./AGENTS.md]\n",
			wantErr:    []string{"context-files.names", "listed twice"},
		},
		{
			// The names are checked whatever the switch says: a defect in the file outlives the
			// day the block is switched back on.
			name:       "a bad name under enable: false",
			configYAML: "context-files:\n  enable: false\n  names: [../secrets.md]\n",
			wantErr:    []string{"context-files.names", "climbs out of the workspace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := options{configDir: home}
			err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("applyConfig = nil error; want the name refused (opts.contextFiles = %#v)", opts.contextFiles)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err, want)
				}
			}
		})
	}
}

// A name whose files do NOT exist is deliberately fine: discovery is the feature, so one global
// config travels across repos that carry different context files, or none. Nothing in the config
// layer may touch the filesystem to decide this.
func TestApplyConfigContextFilesDoesNotRequireTheFilesToExist(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = "context-files:\n  names: [AGENTS.md, docs/nothing-here.md]\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	want := []string{"AGENTS.md", "docs/nothing-here.md"}
	if !reflect.DeepEqual(opts.contextFiles, want) {
		t.Errorf("opts.contextFiles = %#v; want %#v (existence is the engine's question, not config's)",
			opts.contextFiles, want)
	}
}

// The ui config block parses into opts.ui: both keys, file-only like the blocks around it, so the
// composition root can hand the renderer a style and a colour flag it never has to parse.
func TestApplyConfigUI(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const configYAML = `ui:
  spinner: glitter
  spinner-color: false
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	want := uiSettings{spinner: tui.SpinnerGlitter, spinnerColor: false}
	if opts.ui != want {
		t.Errorf("opts.ui = %+v; want %+v", opts.ui, want)
	}
}

// The two ui keys are INDEPENDENT, from the on-disk block onward: a block that names a style leaves
// the colour loop at its default, and one that turns the loop off leaves the style at its default.
// This is the reason spinner-color is a pointer on the on-disk schema — `spinner: classic` alone
// must not read as "and no colour", which is a different look from what was asked for.
func TestApplyConfigUIPartialKeepsTheOtherDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want uiSettings
	}{
		{
			name: "only spinner: → the colour loop stays on",
			yaml: "ui:\n  spinner: classic\n",
			want: uiSettings{spinner: tui.SpinnerClassic, spinnerColor: true},
		},
		{
			name: "only spinner-color: false → the style stays the default",
			yaml: "ui:\n  spinner-color: false\n",
			want: uiSettings{spinner: tui.SpinnerSnake, spinnerColor: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := options{configDir: home}
			if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if opts.ui != tt.want {
				t.Errorf("opts.ui = %+v; want %+v", opts.ui, tt.want)
			}
		})
	}
}

// With no ui block at all, the renderer's own defaults stand: the default spinner style with its
// colour loop on. This is the anchor for "an absent block changes nothing".
func TestApplyConfigNoUIDefaults(t *testing.T) {
	t.Parallel()
	opts := options{configDir: t.TempDir()} // empty dir → no config.yaml
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if opts.ui != wantUIDefault {
		t.Errorf("opts.ui = %+v; want %+v (no block ⇒ the default style, colour loop on)", opts.ui, wantUIDefault)
	}
}

// The `cursor-shape:` key resolves like the ui block beside it: a flat, file-only name carried raw
// into opts (the composition root parses it once for the renderer), empty when the key is absent —
// which is the request for the default, a steady block. An unknown name is a loud startup error
// that names the key AND lists the shapes that would have worked, for the same reason a bad
// spinner style is: silently drawing a block would leave the user wondering why their key did
// nothing. The vocabulary comes from internal/tui (ParseCursorShape), so this also pins that the
// message the user sees carries it.
func TestCursorShapeConfigParses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		yaml    string
		want    string
		wantErr []string // substrings the startup error must carry; nil ⇒ the config is accepted
	}{
		{name: "absent ⇒ empty, the request for the default", yaml: "", want: ""},
		{name: "block", yaml: "cursor-shape: block\n", want: "block"},
		{name: "underline", yaml: "cursor-shape: underline\n", want: "underline"},
		{name: "bar", yaml: "cursor-shape: bar\n", want: "bar"},
		{
			name:    "an unknown shape errors, naming the key and the options",
			yaml:    "cursor-shape: beam\n",
			wantErr: []string{"cursor-shape", "beam", "block", "underline", "bar"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if tt.yaml != "" {
				if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.yaml), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			opts := options{configDir: home}
			err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("applyConfig with %q: want an error, got nil", tt.yaml)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q; want it to contain %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("applyConfig: %v", err)
			}
			if opts.cursorShape != tt.want {
				t.Errorf("opts.cursorShape = %q; want %q", opts.cursorShape, tt.want)
			}
			// The renderer takes a shape, not a name: what the binary hands the TUI must parse.
			if _, err := tui.ParseCursorShape(opts.cursorShape); err != nil {
				t.Errorf("the resolved shape %q does not parse for the renderer: %v", opts.cursorShape, err)
			}
		})
	}
}

// A ui.spinner naming a style this build has no animation for is a loud startup error that names
// the key AND lists the styles that would have worked — not a silent fall back, which would leave
// the user watching a spinner their config did not ask for with nothing pointing at the typo. The
// valid set comes from internal/tui (ParseSpinnerStyle), so this also pins that the message the
// user sees carries it.
func TestApplyConfigUIUnknownSpinnerErrors(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("ui:\n  spinner: sparkle\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := options{configDir: home}
	err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
	if err == nil {
		t.Fatal("applyConfig with ui.spinner: sparkle: want an error, got nil")
	}
	for _, want := range []string{"ui.spinner", "sparkle", "snake", "glitter", "classic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q; want it to contain %q", err, want)
		}
	}

	// Every style this build knows is accepted, so the check rejects only what it should.
	for _, style := range []tui.SpinnerStyle{tui.SpinnerSnake, tui.SpinnerGlitter, tui.SpinnerClassic} {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("ui:\n  spinner: "+string(style)+"\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		opts := options{configDir: home}
		if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
			t.Errorf("applyConfig with ui.spinner: %s: %v", style, err)
		}
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
