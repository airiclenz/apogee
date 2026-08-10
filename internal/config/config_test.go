package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/tui"
)

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }
func intptr(i int) *int       { return &i }

// wantUIDefault is the resolved `ui:` block a config that configures none must produce: the
// default spinner style with its colour loop on, and the transcript's scroll bar shown. It is
// spelled out rather than taken from defaultUISettings, so a change to any shipped default shows up
// here as a failure instead of silently agreeing with itself.
var wantUIDefault = UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"}

// wantContextFilesDefault is the resolved `context-files:` block a config that configures none must
// produce: the feature on, looking for the one default name in the workspace root. Spelled out
// rather than taken from defaultContextFilesSettings, like wantUIDefault above, so a change to
// either shipped default shows up here as a failure instead of silently agreeing with itself.
var wantContextFilesDefault = contextFilesSettings{enable: true, names: []string{"AGENTS.md"}}

// testHostID is the machine identity injected into ResolveSettings so the Host
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
		file, env, flag Layer
		want            Settings
	}{
		{
			name: "all empty → defaults",
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "file fills every field",
			file: Layer{StartupServer: strptr("file-box"), Mode: strptr("plan"), Bypass: boolptr(true)},
			want: Settings{StartupServer: "file-box", Mode: "plan", Bypass: true, ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "env beats file, file fills the rest",
			file: Layer{StartupServer: strptr("file-box"), Mode: strptr("plan")},
			env:  Layer{StartupServer: strptr("env-box")},
			want: Settings{StartupServer: "env-box", Mode: "plan", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "flag beats env beats file, per field",
			file: Layer{StartupServer: strptr("file-box"), Mode: strptr("plan")},
			env:  Layer{StartupServer: strptr("env-box"), Mode: strptr("auto")},
			flag: Layer{StartupServer: strptr("flag-box")},
			want: Settings{StartupServer: "flag-box", Mode: "auto", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "explicit false in a higher layer overrides true below it",
			file: Layer{Bypass: boolptr(true)},
			flag: Layer{Bypass: boolptr(false)},
			want: Settings{Mode: "ask-before", Bypass: false, ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			// The servers list is file-only: it describes machines, not this invocation, so neither
			// the environment nor a flag can name one (only `server:` — which of them to start on —
			// rides the layers above the file).
			name: "servers is file-only",
			file: fileConfig{Servers: []ServerEntry{{Name: "box", Endpoint: "http://box:1111"}}}.layer(),
			want: Settings{Mode: "ask-before", Servers: []ServerEntry{{Name: "box", Endpoint: "http://box:1111"}}, ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "confine-to-workspace is file-only and defaults true",
			file: Layer{ConfineToWorkspace: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: false, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "use-project-skills is file-only and defaults true",
			file: Layer{UseProjectSkills: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: false, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "use-project-skills is NOT set by env or flag (file-only)",
			env:  Layer{UseProjectSkills: boolptr(false)},
			flag: Layer{UseProjectSkills: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "auto-compact is file-only and defaults true",
			file: Layer{AutoCompact: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: false, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "auto-compact is NOT set by env or flag (file-only)",
			env:  Layer{AutoCompact: boolptr(false)},
			flag: Layer{AutoCompact: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "auto-title is file-only and defaults true",
			file: Layer{AutoTitle: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: false, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "an explicit auto-title: true resolves to the same value as an absent key",
			file: Layer{AutoTitle: boolptr(true)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "auto-title is NOT set by env or flag (file-only)",
			env:  Layer{AutoTitle: boolptr(false)},
			flag: Layer{AutoTitle: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "context-window is file-only (default 0 ⇒ discover)",
			file: Layer{ContextWindow: intptr(65536)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, ContextWindow: 65536},
		},
		{
			name: "context-window is NOT set by env or flag (file-only)",
			env:  Layer{ContextWindow: intptr(65536)},
			flag: Layer{ContextWindow: intptr(65536)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "confine-to-workspace is NOT loosenable by env or flag (global-config-only)",
			env:  Layer{ConfineToWorkspace: boolptr(false)}, // an env layer cannot carry it in practice; assert it is ignored even if set
			flag: Layer{ConfineToWorkspace: boolptr(false)},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "a matching unconfined-hosts entry resolves confine-to-workspace to false",
			file: Layer{UnconfinedHosts: []UnconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: false, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault,
				UnconfinedHosts: []UnconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
		},
		{
			name: "unconfined-hosts is NOT settable by env or flag (global-config-only)",
			env:  Layer{UnconfinedHosts: []UnconfinedHost{{ID: testHostID}}},
			flag: Layer{UnconfinedHosts: []UnconfinedHost{{ID: testHostID}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "web-search endpoint is file-only (default empty)",
			file: Layer{WebSearchEndpoint: strptr("https://search.example.com")},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, WebSearchEndpoint: "https://search.example.com"},
		},
		{
			name: "mcp servers are file-only (default empty)",
			file: Layer{MCPServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, MCPServers: []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}},
		},
		{
			name: "mcp servers are NOT settable by env or flag (file-only)",
			env:  Layer{MCPServers: []mcp.ServerConfig{{Name: "fromenv"}}},
			flag: Layer{MCPServers: []mcp.ServerConfig{{Name: "fromflag"}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "servers are file-only (default empty)",
			file: Layer{Servers: []ServerEntry{{Name: "workstation", Endpoint: "http://box:1111"}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, Servers: []ServerEntry{{Name: "workstation", Endpoint: "http://box:1111"}}},
		},
		{
			name: "servers are NOT settable by env or flag (file-only)",
			env:  Layer{Servers: []ServerEntry{{Name: "fromenv", Endpoint: "http://env:1111"}}},
			flag: Layer{Servers: []ServerEntry{{Name: "fromflag", Endpoint: "http://flag:1111"}}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "model profile is file-only (default zero)",
			file: Layer{Profile: &domain.ModelProfile{
				ToolCallFormat: domain.FormatMarkdownFenced,
				Thinking:       domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, Profile: domain.ModelProfile{
				ToolCallFormat: domain.FormatMarkdownFenced,
				Thinking:       domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}},
		},
		{
			name: "model profile is NOT settable by env or flag (file-only)",
			env:  Layer{Profile: &domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex}},
			flag: Layer{Profile: &domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "mechanisms are file-only (default empty)",
			file: Layer{Mechanisms: map[string]bool{"validate": true, "syntax": false}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault, Mechanisms: map[string]bool{"validate": true, "syntax": false}},
		},
		{
			name: "mechanisms are NOT settable by env or flag (file-only)",
			env:  Layer{Mechanisms: map[string]bool{"fromenv": true}},
			flag: Layer{Mechanisms: map[string]bool{"fromflag": true}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "the present block is file-only (all four keys)",
			file: Layer{Present: &PresentSettings{AutoOpen: false, Command: "zed {path}", Port: 8934, Host: "10.0.0.2"}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault,
				Present: PresentSettings{AutoOpen: false, Command: "zed {path}", Port: 8934, Host: "10.0.0.2"}, UI: wantUIDefault},
		},
		{
			name: "present is NOT settable by env or flag (file-only ⇒ auto-open stays on)",
			env:  Layer{Present: &PresentSettings{AutoOpen: false, Port: 1}},
			flag: Layer{Present: &PresentSettings{AutoOpen: false, Port: 2}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "the ui block is file-only (all three keys)",
			file: fileConfig{UI: &uiConfig{Spinner: "glitter", SpinnerColor: boolptr(false), ShowScrollbar: boolptr(false)}}.layer(),
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true},
				UI: UISettings{Spinner: tui.SpinnerGlitter, SpinnerColor: false, ShowScrollbar: false, ColorScheme: "dark"}},
		},
		{
			// The keys are independent: naming a style says nothing about the colour loop.
			name: "ui with only spinner: set → the colour loop stays at its default",
			file: fileConfig{UI: &uiConfig{Spinner: "classic"}}.layer(),
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true},
				UI: UISettings{Spinner: tui.SpinnerClassic, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"}},
		},
		{
			// …and the other way round: turning the loop off does not change which style paints.
			name: "ui with only spinner-color: false → the style stays at its default",
			file: fileConfig{UI: &uiConfig{SpinnerColor: boolptr(false)}}.layer(),
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true},
				UI: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: false, ShowScrollbar: true, ColorScheme: "dark"}},
		},
		{
			// The scroll-bar switch is the third independent key: hiding the bar leaves both
			// spinner keys exactly where they were.
			name: "ui with only show-scrollbar: false → the spinner keys stay at their defaults",
			file: fileConfig{UI: &uiConfig{ShowScrollbar: boolptr(false)}}.layer(),
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true},
				UI: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: false, ColorScheme: "dark"}},
		},
		{
			name: "ui is NOT settable by env or flag (file-only ⇒ the defaults hold)",
			env:  Layer{UI: &UISettings{Spinner: tui.SpinnerClassic, ShowScrollbar: false}},
			flag: Layer{UI: &UISettings{Spinner: tui.SpinnerGlitter, ShowScrollbar: false}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
		{
			name: "a context-files block replaces the default name list whole",
			file: fileConfig{ContextFiles: &contextFilesConfig{Names: []string{"CONVENTIONS.md", "AGENTS.md"}}}.layer(),
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault,
				ContextFiles: contextFilesSettings{enable: true, names: []string{"CONVENTIONS.md", "AGENTS.md"}}},
		},
		{
			name: "context-files is NOT settable by env or flag (file-only ⇒ the defaults hold)",
			env:  Layer{ContextFiles: &contextFilesSettings{enable: true, names: []string{"from-env.md"}}},
			flag: Layer{ContextFiles: &contextFilesSettings{enable: false}},
			want: Settings{Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true, AutoTitle: true, ValidatedSetsEnable: true, ContextFiles: wantContextFilesDefault, Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, notices := ResolveSettings(tt.file, tt.env, tt.flag, testHostID)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveSettings = %+v; want %+v", got, tt.want)
			}
			if len(notices) != 0 {
				t.Errorf("ResolveSettings notices = %q; want none for a well-formed config", notices)
			}
		})
	}
}

// The three keys that resolve from more than one source, driven end to end through their registry
// rows: a real fileConfig, a real getenv, a real explicitly-set flag, and the precedence the
// three produce. The WIRE-FACING names — the variable a user exports, the flag they type — are
// spelled out here as literals rather than read from the row, which is what makes this a test of
// the rows and not of itself: resolution now asks the registry which variable and which flag
// carry each key, so editing a row's EnvVar or FlagName to anything else means the value this
// test sets is never seen and the precedence assertions fail.
//
// The raw startup overrides (APOGEE_ENDPOINT, APOGEE_API_KEY, APOGEE_MODEL, --endpoint, --model)
// are deliberately absent: since ADR 0036 they name no config key at all, so they do not ride
// these layers and nothing here should claim they do.
func TestResolveSettingsMultiSourceKeysReadTheRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path            string
		envVar          string // the variable name the row must carry; "" ⇒ the key has none
		flagName        string // the flag name the row must carry; "" ⇒ the key has none
		file, env, flag string // what each source supplies, as that source spells it
		setFile         func(*fileConfig, string)
		setFlag         func(*Options, string)
		resolved        func(Settings) string
	}{
		{
			path: "server", envVar: "APOGEE_SERVER", flagName: "server",
			file: "the-file-box", env: "the-env-box", flag: "the-flag-box",
			setFile:  func(fc *fileConfig, v string) { fc.Server = v },
			setFlag:  func(o *Options, v string) { o.StartupServer = v },
			resolved: func(s Settings) string { return s.StartupServer },
		},
		{
			path: "mode", envVar: "APOGEE_MODE", flagName: "mode",
			file: string(domain.ModePlan), env: string(domain.ModeAllowEdits), flag: string(domain.ModeAuto),
			setFile:  func(fc *fileConfig, v string) { fc.Mode = v },
			setFlag:  func(o *Options, v string) { o.Mode = v },
			resolved: func(s Settings) string { return s.Mode },
		},
		{
			path: "bypass", envVar: "APOGEE_BYPASS", flagName: "bypass",
			file: "true", env: "false", flag: "true",
			setFile: func(fc *fileConfig, v string) { fc.Bypass = boolptr(v == "true") },
			setFlag: func(o *Options, v string) { o.Bypass = v == "true" },
			resolved: func(s Settings) string {
				if s.Bypass {
					return "true"
				}
				return "false"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			row, ok := LookupKey(tt.path)
			if !ok {
				t.Fatalf("no registry row for %q — resolution reads this key's sources from it", tt.path)
			}
			if row.EnvVar != tt.envVar {
				t.Errorf("row %q EnvVar = %q, want %q (the variable users export for this key)", tt.path,
					row.EnvVar, tt.envVar)
			}
			if row.FlagName != tt.flagName {
				t.Errorf("row %q FlagName = %q, want %q (the flag users type for this key)", tt.path,
					row.FlagName, tt.flagName)
			}

			var fc fileConfig
			tt.setFile(&fc, tt.file)
			file := fc.layer()
			if got, _ := ResolveSettings(file, Layer{}, Layer{}, testHostID); tt.resolved(got) != tt.file {
				t.Fatalf("file only: %s = %q, want %q", tt.path, tt.resolved(got), tt.file)
			}

			var env Layer
			if tt.envVar != "" {
				var err error
				env, err = envLayer(func(name string) string {
					if name == tt.envVar {
						return tt.env
					}
					return ""
				})
				if err != nil {
					t.Fatalf("envLayer with %s=%q: %v", tt.envVar, tt.env, err)
				}
				if got, _ := ResolveSettings(file, env, Layer{}, testHostID); tt.resolved(got) != tt.env {
					t.Errorf("%s over the file: %s = %q, want %q (does the row still name %s?)", tt.envVar,
						tt.path, tt.resolved(got), tt.env, tt.envVar)
				}
			}

			if tt.flagName != "" {
				var opts Options
				tt.setFlag(&opts, tt.flag)
				flag := flagLayer(opts, func(name string) bool { return name == tt.flagName })
				if got, _ := ResolveSettings(file, env, flag, testHostID); tt.resolved(got) != tt.flag {
					t.Errorf("--%s over %s: %s = %q, want %q (does the row still name --%s?)", tt.flagName,
						tt.envVar, tt.path, tt.resolved(got), tt.flag, tt.flagName)
				}
			}
		})
	}
}

// ResolveSettings takes its base mode from the registry row rather than a literal, so the row is
// what the "all empty → defaults" expectation above now rests on. This pins that row to the
// autonomy ladder's own default constant: a row edited to another mode — or to nothing — would
// otherwise quietly change what a session with no config starts in.
func TestRegistryModeDefaultIsTheLadderDefault(t *testing.T) {
	t.Parallel()
	row, ok := LookupKey("mode")
	if !ok {
		t.Fatal("no registry row for mode — ResolveSettings takes the default mode from it")
	}
	if row.Default != string(domain.ModeAskBefore) {
		t.Errorf("registry mode default = %q, want %q (the mode a session with no config starts in)",
			row.Default, string(domain.ModeAskBefore))
	}
}

// The binding table over the registry cannot half-describe a key: every row advertising a
// variable or a flag must have the plumbing that reads it, every binding must name a described
// key, and no binding may carry plumbing for a source its row does not name (which would be
// dead code advertising nothing). Without this, adding `EnvVar:` to a row would silently
// advertise a variable resolution never reads.
func TestMultiSourceKeysBindDescribedKeys(t *testing.T) {
	t.Parallel()

	bound := map[string]multiSourceKey{}
	for _, k := range multiSourceKeys {
		if _, ok := LookupKey(k.row.Path); !ok {
			t.Errorf("multiSourceKeys binds %q, which the registry does not describe", k.row.Path)
		}
		if k.overlay == nil {
			t.Errorf("multiSourceKeys entry %q has no overlay, so resolution would never copy it", k.row.Path)
		}
		if k.fromEnv != nil && k.row.EnvVar == "" {
			t.Errorf("multiSourceKeys entry %q reads an environment variable its row does not name", k.row.Path)
		}
		if k.fromFlag != nil && k.row.FlagName == "" {
			t.Errorf("multiSourceKeys entry %q reads a flag its row does not name", k.row.Path)
		}
		bound[k.row.Path] = k
	}
	for _, row := range KeyRegistry {
		if row.EnvVar == "" && row.FlagName == "" {
			continue // file-only, so nothing above the file has to be plumbed
		}
		k, ok := bound[row.Path]
		if !ok {
			t.Errorf("registry row %q names a source above the file but nothing binds it — the variable "+
				"or flag would be advertised and never read", row.Path)
			continue
		}
		if row.EnvVar != "" && k.fromEnv == nil {
			t.Errorf("registry row %q names %s but the binding reads no environment value", row.Path, row.EnvVar)
		}
		if row.FlagName != "" && k.fromFlag == nil {
			t.Errorf("registry row %q names --%s but the binding reads no flag value", row.Path, row.FlagName)
		}
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
		hosts       []UnconfinedHost
		hostID      string
		want        bool
		wantNotices int
	}{
		{name: "nothing configured → the secure default", hostID: testHostID, want: true},
		{name: "explicit global false → unconfined everywhere", explicit: boolptr(false), hostID: testHostID, want: false},
		{name: "explicit global true, no acknowledgement → confined", explicit: boolptr(true), hostID: testHostID, want: true},
		{
			name:   "this host is acknowledged → unconfined here",
			hosts:  []UnconfinedHost{{ID: otherHost}, {ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable container"}},
			hostID: testHostID,
			want:   false,
		},
		{
			name:   "only other machines are acknowledged → still confined here",
			hosts:  []UnconfinedHost{{ID: otherHost}, {ID: "buildbox-000111"}},
			hostID: testHostID,
			want:   true,
		},
		{
			name:     "an explicit true does not veto a match — the entry is the more specific claim",
			explicit: boolptr(true),
			hosts:    []UnconfinedHost{{ID: testHostID}},
			hostID:   testHostID,
			want:     false,
		},
		{
			name:        "a malformed entry is skipped with a notice, the well-formed one still matches",
			hosts:       []UnconfinedHost{{Note: "no id here"}, {ID: testHostID}},
			hostID:      testHostID,
			want:        false,
			wantNotices: 1,
		},
		{
			name:        "a blank id never matches a blank host id — it is malformed, not a wildcard",
			hosts:       []UnconfinedHost{{ID: "   "}},
			hostID:      "",
			want:        true,
			wantNotices: 1,
		},
		{
			name:        "an identity-less host is not acknowledged by an entry naming its shared id",
			hosts:       []UnconfinedHost{{ID: unidentifiedTestHostID, Acknowledged: "2026-07-21"}},
			hostID:      unidentifiedTestHostID,
			want:        true,
			wantNotices: 1,
		},
		{
			name:     "an explicit global false still loosens an identity-less host — step 1 is untouched",
			explicit: boolptr(false),
			hosts:    []UnconfinedHost{{ID: unidentifiedTestHostID}},
			hostID:   unidentifiedTestHostID,
			want:     false,
			// The entry is still reported: the match was refused, and saying so is what keeps
			// the notice honest about why the id cannot stand for one machine.
			wantNotices: 1,
		},
		{
			name:   "an identity-less host with a real machine's entry is simply not that machine",
			hosts:  []UnconfinedHost{{ID: otherHost}},
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
		home := testConfigHome(t, "")
		configYAML := "unconfined-hosts:\n  - id: \"" + platform.HostID() + "\"\n" +
			"    acknowledged: \"2026-07-21\"\n    note: \"disposable container\"\n"
		writeConfigHome(t, home, configYAML)
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if opts.ConfineToWorkspace {
			t.Error("opts.confineToWorkspace = true; want false — this host is acknowledged")
		}
		want := []UnconfinedHost{{ID: platform.HostID(), Acknowledged: "2026-07-21", Note: "disposable container"}}
		if !reflect.DeepEqual(opts.UnconfinedHosts, want) {
			t.Errorf("opts.unconfinedHosts = %+v; want %+v", opts.UnconfinedHosts, want)
		}
	})

	t.Run("another host acknowledged", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "")
		const configYAML = "unconfined-hosts:\n  - id: \"someone-elses-box-abc123\"\n"
		writeConfigHome(t, home, configYAML)
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if !opts.ConfineToWorkspace {
			t.Error("opts.confineToWorkspace = false; want true — the acknowledgement names another machine")
		}
	})

	t.Run("a malformed entry notifies and does not block startup", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "")
		const configYAML = "unconfined-hosts:\n  - note: \"forgot the id\"\n"
		writeConfigHome(t, home, configYAML)
		var got []string
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, noFlags, noEnv, os.ReadFile, func(msg string) { got = append(got, msg) }); err != nil {
			t.Fatalf("ApplyConfig: %v; want a soft skip, not a blocked startup", err)
		}
		if !opts.ConfineToWorkspace {
			t.Error("opts.confineToWorkspace = false; want true — a malformed entry acknowledges nothing")
		}
		if len(got) != 1 || !strings.Contains(got[0], "unconfined-hosts") {
			t.Errorf("notices = %q; want one naming unconfined-hosts", got)
		}
	})
}

// ApplyConfig drives the whole chain end-to-end: a config file on disk, env overrides, and
// an explicit flag, all resolved with the real loader/parser against injected sources.
func TestApplyConfigEndToEnd(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "servers:\n"+
		"  - name: file-box\n    endpoint: http://file:1111\n    model: m-file\n"+
		"  - name: flag-box\n    endpoint: http://flag:1111\n    model: m-flag\n"+
		"server: file-box\nmode: plan\nbypass: true\n")

	// env turns bypass off; the flag names another server, whose own fields come with it.
	getenv := func(k string) string {
		if k == EnvBypass {
			return "false"
		}
		return ""
	}
	changed := func(name string) bool { return name == "server" || name == "config" }
	opts := Options{ConfigDir: home, StartupServer: "flag-box"}

	if err := ApplyConfig(&opts, changed, getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.Endpoint != "http://flag:1111" {
		t.Errorf("endpoint = %q; want the --server entry's", opts.Endpoint)
	}
	if opts.Model != "m-flag" {
		t.Errorf("model = %q; want the --server entry's hint", opts.Model)
	}
	if opts.Mode != "plan" {
		t.Errorf("mode = %q; want the file value", opts.Mode)
	}
	if opts.Bypass {
		t.Error("bypass = true; want the env value false to override the file's true")
	}
}

// A run with no config file, no env, and only defaults resolves cleanly to the defaults.
func TestApplyConfigDefaults(t *testing.T) {
	t.Parallel()
	noEnv := func(string) string { return "" }
	noFlags := func(string) bool { return false }
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server

	if err := ApplyConfig(&opts, noFlags, noEnv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.Model != "" || opts.Bypass {
		t.Errorf("non-default model/bypass: %+v", opts)
	}
	if opts.Mode != string(domain.ModeAskBefore) {
		t.Errorf("mode = %q; want the default %q", opts.Mode, domain.ModeAskBefore)
	}
	if !opts.AutoCompact {
		t.Error("autoCompact = false; want the structural default true (auto-compaction on)")
	}
	if opts.APIKey != "" {
		t.Errorf("apiKey = %q; want empty — an unconfigured key sends no Authorization header", opts.APIKey)
	}
}

// The upstream half of resolution, end to end: the `server:` name picks an entry out of `servers:`,
// and that ONE entry is what the session is built from — endpoint, key, model hint, and the alias
// the footer calls it (ADR 0036: the list is the single definition).
func TestApplyConfigSelectsTheNamedServer(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "servers:\n"+
		"  - name: laptop\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n    api-key: sk-work\n    model: qwen\n"+
		"server: workstation\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.Endpoint != "http://192.168.1.9:1111" {
		t.Errorf("endpoint = %q; want the named entry's", opts.Endpoint)
	}
	if opts.APIKey != "sk-work" {
		t.Errorf("apiKey = %q; want the named entry's", opts.APIKey)
	}
	if opts.Model != "qwen" {
		t.Errorf("model = %q; want the named entry's hint", opts.Model)
	}
	if opts.HostAlias != "workstation" {
		t.Errorf("hostAlias = %q; want the entry's own name — the name IS the alias", opts.HostAlias)
	}
	if opts.StartupServer != "workstation" {
		t.Errorf("startupServer = %q; want the resolved server: value", opts.StartupServer)
	}
	// A startup that came out of the list is not ephemeral, which is what tells the switch list it
	// already holds this server (upstreamChoices).
	if opts.StartupEphemeral {
		t.Error("startupEphemeral = true; want false — the session started on a configured entry")
	}
}

// The launcher a session STARTS with is the selected entry's own key, carried as written for the
// composition root to resolve: the key belongs to the entry the launcher fronts, so a session that
// starts on any other entry starts with the integration off and reaches it by switching. The
// ephemeral entry a raw `--endpoint`/`APOGEE_ENDPOINT` override builds carries no key at all, which
// is the honest answer for an endpoint no entry names.
func TestApplyConfigStartupLauncherComesFromTheSelectedEntry(t *testing.T) {
	t.Parallel()
	const servers = "servers:\n" +
		"  - name: laptop\n    endpoint: http://127.0.0.1:1111\n    llama-launcher: auto\n" +
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n"

	tests := []struct {
		name     string
		start    string
		endpoint string
		want     string
	}{
		{name: "the selected entry names a launcher", start: "laptop", want: "auto"},
		{name: "the selected entry names none", start: "workstation"},
		{name: "an endpoint override names nothing", start: "laptop", endpoint: "http://rented.example:8080/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{ConfigDir: testConfigHome(t, servers+"server: "+tt.start+"\n")}
			getenv := func(name string) string {
				if name == EnvEndpoint {
					return tt.endpoint
				}
				return ""
			}
			if err := ApplyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.StartupLauncher != tt.want {
				t.Errorf("startupLauncher = %q; want %q — the key travels from the SELECTED entry, unresolved",
					opts.StartupLauncher, tt.want)
			}
		})
	}
}

// --server beats APOGEE_SERVER beats `server:` at selection too, not only in resolution: the value
// that wins is the one the entry is looked up by.
func TestApplyConfigStartupServerOverrideSelects(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "servers:\n"+
		"  - name: laptop\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n"+
		"server: laptop\n")
	opts := Options{ConfigDir: home, StartupServer: "workstation"}
	changed := func(name string) bool { return name == "server" }
	if err := ApplyConfig(&opts, changed, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.Endpoint != "http://192.168.1.9:1111" {
		t.Errorf("endpoint = %q; want the --server entry's", opts.Endpoint)
	}
}

// The three ways selection has no answer. Each is a hard error naming the config file and showing
// what to write — the permanent behaviour for the non-interactive drivers — and each carries the
// REASON that lets the TUI answer it by asking instead (ADR 0036 decisions 3, 4 and 7). The resolved
// opts must survive the refusal intact, because the pre-bound TUI asks with exactly that: the
// servers list it offers, and every other key the session runs on.
func TestApplyConfigStartupServerRefusals(t *testing.T) {
	t.Parallel()
	const list = "servers:\n  - name: laptop\n    endpoint: http://127.0.0.1:1111\n"
	tests := []struct {
		name       string
		configYAML string
		wantParts  []string
		wantStart  tui.PreboundStart
	}{
		{
			name:       "an empty list has nothing to start on",
			configYAML: "mode: plan\n",
			wantParts:  []string{"no servers are configured", "config.yaml", "servers:", "server: my-box"},
			wantStart:  tui.PreboundStart{Reason: tui.PreboundNoServers},
		},
		{
			name:       "a list with no choice recorded",
			configYAML: list,
			wantParts:  []string{"no startup server is chosen", "laptop", "--server"},
			wantStart:  tui.PreboundStart{Reason: tui.PreboundFirstBoot},
		},
		{
			name:       "a choice no entry carries",
			configYAML: list + "server: the-old-name\n",
			wantParts:  []string{`names "the-old-name"`, "configured: laptop", "--server"},
			wantStart:  tui.PreboundStart{Reason: tui.PreboundStaleChoice, Name: "the-old-name"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			// serverFlagBound as the root command sets it: these messages name `--server` as the
			// fix, and only a command that registers the flag may say so (see the remedy test).
			opts := Options{ConfigDir: home, Mode: "ask-before", ServerFlagBound: true}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify)
			if err == nil {
				t.Fatal("startup was allowed with no server to talk to")
			}
			for _, want := range tt.wantParts {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q: %v", want, err)
				}
			}
			var undetermined *StartupUndetermined
			if !errors.As(err, &undetermined) {
				t.Fatalf("the refusal is not a StartupUndetermined (%T); the TUI cannot tell it from "+
					"a config error it must print", err)
			}
			if undetermined.Start != tt.wantStart {
				t.Errorf("reason = %+v; want %+v", undetermined.Start, tt.wantStart)
			}
			// Resolution finished despite the refusal: the list to pick from is there, and so is
			// every other key. Nothing is bound, so the upstream fields are empty and the startup
			// is not the ephemeral one either.
			if len(opts.Servers) != strings.Count(tt.configYAML, "  - name:") {
				t.Errorf("opts.servers = %+v; want the file's list, resolved despite the refusal", opts.Servers)
			}
			if opts.Mode != "plan" && tt.configYAML == "mode: plan\n" {
				t.Errorf("opts.mode = %q; want the file's — the write-back must complete", opts.Mode)
			}
			if opts.Endpoint != "" || opts.APIKey != "" || opts.Model != "" || opts.HostAlias != "" {
				t.Errorf("an undetermined startup left upstream fields set: endpoint=%q key-set=%t model=%q alias=%q",
					opts.Endpoint, opts.APIKey != "", opts.Model, opts.HostAlias)
			}
			if opts.StartupEphemeral {
				t.Error("startupEphemeral = true; nothing was selected, so there is no row to synthesize")
			}
		})
	}
}

// The remedy a refusal offers has to be one the command printing it actually HAS. Only the root
// command registers `--server` (root.go's flag block); `apogee headless` and `apogee probe` — the
// drivers that PRINT these refusals rather than asking a human — declare their own flag surface
// without it, so a message naming the flag there would send the user to a parser that rejects it.
// Both name-shaped refusals are pinned, in both directions: what is offered, and what must not be.
func TestSelectStartupServerRemedyFollowsTheFlagSurface(t *testing.T) {
	t.Parallel()
	servers := []ServerEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}}
	tests := []struct {
		name       string
		chosen     string
		serverFlag bool
		want       string
		unwanted   string
	}{
		{name: "nothing chosen, on a command with the flag", serverFlag: true,
			want: "--server <name>", unwanted: "APOGEE_SERVER"},
		{name: "nothing chosen, on a command without it",
			want: "APOGEE_SERVER=<name>", unwanted: "--server"},
		{name: "a stale name, on a command with the flag", chosen: "gone", serverFlag: true,
			want: "--server <name>", unwanted: "APOGEE_SERVER"},
		{name: "a stale name, on a command without it", chosen: "gone",
			want: "APOGEE_SERVER=<name>", unwanted: "--server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := selectStartupServer(tt.chosen, servers, "/home/config.yaml", tt.serverFlag)
			if err == nil {
				t.Fatal("selection answered a question the config cannot answer")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the refusal does not offer %q: %v", tt.want, err)
			}
			if strings.Contains(err.Error(), tt.unwanted) {
				t.Errorf("the refusal offers %q, which this command has no way to accept: %v", tt.unwanted, err)
			}
		})
	}
}

// The raw invocation overrides, end to end through ApplyConfig (ADR 0036 decision 6). An endpoint
// override builds an EPHEMERAL unnamed entry that wins over any recorded or flagged name and
// carries the key and hint overrides as its own fields; without one, those two overlay the
// selected entry's own fields and the list is still what says where the session talks.
func TestApplyConfigStartupOverrides(t *testing.T) {
	t.Parallel()
	const twoServers = "servers:\n" +
		"  - name: laptop\n    endpoint: http://127.0.0.1:1111\n    api-key: file-key\n    model: file-model\n" +
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n" +
		"server: laptop\n"

	tests := []struct {
		name          string
		configYAML    string
		flags         Options
		changed       map[string]bool
		env           map[string]string
		wantEndpoint  string
		wantAPIKey    string
		wantModel     string
		wantHostAlias string
	}{
		{
			name:          "APOGEE_ENDPOINT alone builds the ephemeral entry",
			configYAML:    twoServers,
			env:           map[string]string{EnvEndpoint: "http://rented:8080"},
			wantEndpoint:  "http://rented:8080",
			wantHostAlias: "rented",
		},
		{
			name:          "--endpoint beats APOGEE_ENDPOINT",
			configYAML:    twoServers,
			flags:         Options{Endpoint: "http://flag-box:1111"},
			changed:       map[string]bool{"endpoint": true},
			env:           map[string]string{EnvEndpoint: "http://env-box:1111"},
			wantEndpoint:  "http://flag-box:1111",
			wantHostAlias: "flag-box",
		},
		{
			name:       "the ephemeral entry carries the key and hint overrides",
			configYAML: twoServers,
			flags:      Options{Model: "flag-model"},
			changed:    map[string]bool{"model": true},
			env: map[string]string{
				EnvEndpoint: "http://rented:8080",
				EnvAPIKey:   "sk-rented",
				EnvModel:    "env-model",
			},
			wantEndpoint:  "http://rented:8080",
			wantAPIKey:    "sk-rented",
			wantModel:     "flag-model", // --model beats APOGEE_MODEL, per pair
			wantHostAlias: "rented",
		},
		{
			name:       "an endpoint override ignores server: and --server",
			configYAML: twoServers,
			flags:      Options{StartupServer: "workstation"},
			changed:    map[string]bool{"server": true},
			env:        map[string]string{EnvEndpoint: "http://rented:8080"},
			// Neither the recorded `laptop` nor the flagged `workstation`: the URL is the most
			// explicit thing the invocation said.
			wantEndpoint:  "http://rented:8080",
			wantHostAlias: "rented",
		},
		{
			name:          "an endpoint override rescues a config that lists nothing",
			configYAML:    "mode: plan\n",
			env:           map[string]string{EnvEndpoint: "http://rented:8080", EnvAPIKey: "sk-rented"},
			wantEndpoint:  "http://rented:8080",
			wantAPIKey:    "sk-rented",
			wantHostAlias: "rented",
		},
		{
			name:          "APOGEE_API_KEY overlays the selected entry's own key",
			configYAML:    twoServers,
			env:           map[string]string{EnvAPIKey: "sk-today"},
			wantEndpoint:  "http://127.0.0.1:1111",
			wantAPIKey:    "sk-today",
			wantModel:     "file-model",
			wantHostAlias: "laptop",
		},
		{
			name:          "--model overlays the selected entry's own hint",
			configYAML:    twoServers,
			flags:         Options{Model: "flag-model"},
			changed:       map[string]bool{"model": true},
			wantEndpoint:  "http://127.0.0.1:1111",
			wantAPIKey:    "file-key",
			wantModel:     "flag-model",
			wantHostAlias: "laptop",
		},
		{
			name:          "no override leaves the selected entry exactly as configured",
			configYAML:    twoServers,
			wantEndpoint:  "http://127.0.0.1:1111",
			wantAPIKey:    "file-key",
			wantModel:     "file-model",
			wantHostAlias: "laptop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := tt.flags
			opts.ConfigDir = home
			err := ApplyConfig(&opts, func(name string) bool { return tt.changed[name] },
				func(name string) string { return tt.env[name] }, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.Endpoint != tt.wantEndpoint {
				t.Errorf("endpoint = %q; want %q", opts.Endpoint, tt.wantEndpoint)
			}
			if opts.APIKey != tt.wantAPIKey {
				t.Errorf("apiKey = %q; want %q", opts.APIKey, tt.wantAPIKey)
			}
			if opts.Model != tt.wantModel {
				t.Errorf("model = %q; want %q", opts.Model, tt.wantModel)
			}
			if opts.HostAlias != tt.wantHostAlias {
				t.Errorf("hostAlias = %q; want %q", opts.HostAlias, tt.wantHostAlias)
			}
			// An override describes ONE run: it is never written back, so the file the run read
			// must come out of it byte-identical and the home must gain nothing.
			after, readErr := os.ReadFile(filepath.Join(home, "config.yaml"))
			if readErr != nil {
				t.Fatalf("re-read config: %v", readErr)
			}
			if string(after) != tt.configYAML {
				t.Errorf("ApplyConfig rewrote config.yaml:\n%s\nwant:\n%s", after, tt.configYAML)
			}
			assertHomeHoldsOnlyConfig(t, home, "an override run")
		})
	}
}

// The ephemeral entry is unnamed on purpose — namelessness is what keeps it out of the file — so
// the footer's alias comes from the endpoint's own host, and `server:` keeps naming what the FILE
// records (which is what the next run without the override starts on).
func TestApplyConfigEphemeralEntryIsUnnamed(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	opts := Options{ConfigDir: home}
	getenv := func(name string) string {
		if name == EnvEndpoint {
			return "http://rented.example:8080/v1"
		}
		return ""
	}
	if err := ApplyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.HostAlias != "rented.example" {
		t.Errorf("hostAlias = %q; want the endpoint host — an ephemeral entry has no name", opts.HostAlias)
	}
	if opts.StartupServer != testServerName {
		t.Errorf("startupServer = %q; want the file's %q — an override does not rewrite the record",
			opts.StartupServer, testServerName)
	}
	// And it is marked ephemeral, because nothing in `servers:` names it: the switch list has to
	// synthesize a row for it or the way back to this run's server would be lost.
	if !opts.StartupEphemeral {
		t.Error("startupEphemeral = false; want true — the run started on an override endpoint")
	}
}

// An explicitly-set flag speaks even when its value is empty: `--endpoint ""` says "nothing from
// the command line", and letting the variable slip back in underneath would make the flag mean
// the opposite of what was typed.
func TestResolveStartupOverridesEmptyFlagBeatsTheVariable(t *testing.T) {
	t.Parallel()
	got := resolveStartupOverrides(Options{},
		func(name string) bool { return name == "endpoint" },
		func(name string) string {
			if name == EnvEndpoint {
				return "http://env-box:1111"
			}
			return ""
		})
	if got.endpoint != "" {
		t.Errorf("endpoint = %q; want empty — the explicitly-set flag wins", got.endpoint)
	}
}

// The override table cannot half-describe a source, on TestMultiSourceKeysBindDescribedKeys'
// reasoning: every entry must read the variable it names and the flag it names, the three
// detached variables must each be bound exactly once, and no override name may collide with a
// registry row's — since ADR 0036 these names describe no config key, and an overlap would mean
// two resolvers fighting over one variable or flag.
func TestStartupOverrideSourcesBindTheDetachedNames(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, src := range startupOverrideSources {
		if src.envVar == "" {
			t.Error("a startup override names no environment variable, so nothing could set it")
		}
		if src.into == nil {
			t.Errorf("startup override %s has no projection, so resolution would drop it", src.envVar)
		}
		if (src.flagName == "") != (src.fromFlag == nil) {
			t.Errorf("startup override %s names flag %q but reads %v — a flag that is advertised "+
				"must be read, and one that is read must be advertised", src.envVar, src.flagName, src.fromFlag != nil)
		}
		seen[src.envVar] = true
		for _, row := range KeyRegistry {
			if row.EnvVar == src.envVar {
				t.Errorf("startup override %s is also registry row %q's variable", src.envVar, row.Path)
			}
			if src.flagName != "" && row.FlagName == src.flagName {
				t.Errorf("startup override --%s is also registry row %q's flag", src.flagName, row.Path)
			}
		}
	}
	for _, name := range []string{EnvEndpoint, EnvAPIKey, EnvModel} {
		if !seen[name] {
			t.Errorf("%s is named as a startup override but nothing reads it", name)
		}
	}
	if len(seen) != len(startupOverrideSources) {
		t.Errorf("startupOverrideSources binds %d entries over %d variables; one variable is bound twice",
			len(startupOverrideSources), len(seen))
	}

	// The other half of the claim — that the flags they name are really registered, so none
	// advertises a flag cobra would reject — belongs to the Driver that owns the flag set:
	// cmd/apogee's TestRootCommandRegistersTheStartupOverrideFlags walks StartupOverrideFlags
	// against its own root command.
	if got := StartupOverrideFlags(); len(got) == 0 {
		t.Error("StartupOverrideFlags names nothing, so the Driver-side guard checks nothing")
	}
}

// A config in the retired schema that CANNOT be folded for the user is refused with the block that
// replaces it, key by key: the decoder ignores keys fileConfig no longer has, so without the sniff
// a working endpoint would simply stop being read. Every case here has no endpoint: to move, so
// there is no entry to fold into — the configs the migration DOES rewrite are in
// configmigrate_test.go.
func TestApplyConfigRefusesTheRetiredKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		wantParts  []string
	}{
		{
			name:       "api-key alone",
			configYAML: "api-key: sk-secret\n",
			wantParts:  []string{"retired top-level", "servers:", "api-key: sk-secret"},
		},
		{
			name:       "host-alias alone names the entry",
			configYAML: "host-alias: the-box\n",
			wantParts:  []string{"retired top-level", "- name: the-box", "server: the-box"},
		},
		{
			name:       "model alone",
			configYAML: "model: qwen\n",
			wantParts:  []string{"retired top-level", "model: qwen"},
		},
		{
			name:       "the quadruple without its endpoint",
			configYAML: "api-key: sk-secret\nhost-alias: the-box\nmodel: qwen\nmode: plan\n",
			wantParts: []string{
				"no endpoint:", "- name: the-box", "api-key: sk-secret", "model: qwen", "server: the-box",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.configYAML), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify)
			if err == nil {
				t.Fatal("a config in the retired schema was accepted")
			}
			for _, want := range tt.wantParts {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not carry %q: %v", want, err)
				}
			}
		})
	}
}

// And the converse: a config in the new schema never trips the sniff, however much it says.
func TestApplyConfigNewSchemaDoesNotTripTheLegacySniff(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "mode: plan\nweb-search-endpoint: \"off\"\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, noNotify); err != nil {
		t.Fatalf("a new-schema config was refused: %v", err)
	}
	if opts.Endpoint != testServerEndpoint || opts.HostAlias != testServerName {
		t.Errorf("endpoint/alias = %q/%q; want the entry's %q/%q", opts.Endpoint, opts.HostAlias,
			testServerEndpoint, testServerName)
	}
}

// The api-key surface end-to-end through ApplyConfig. Since ADR 0036 the key belongs to the
// `servers:` entry the session starts on, not to a top-level key of its own: the selected entry's
// key is the one sent, and an entry that names none resolves empty — which is what makes a keyless
// local server behave exactly as it did before the key existed (no Authorization header).
func TestApplyConfigAPIKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       string
	}{
		{
			name:       "the selected entry's key",
			configYAML: "servers:\n  - name: box\n    endpoint: http://box:1111\n    api-key: box-secret\nserver: box\n",
			want:       "box-secret",
		},
		{
			name: "only the SELECTED entry's key is sent",
			configYAML: "servers:\n  - name: box\n    endpoint: http://box:1111\n    api-key: box-secret\n" +
				"  - name: other\n    endpoint: http://other:1111\nserver: other\n",
			want: "",
		},
		{
			name:       "an entry with no key → empty (no auth header)",
			configYAML: "servers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n",
			want:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, tt.configYAML)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.APIKey != tt.want {
				t.Errorf("opts.apiKey = %q; want %q", opts.APIKey, tt.want)
			}
		})
	}
}

// The auto-compact config block parses into opts.autoCompact (item 9): a file-only, default-true
// key, so an explicit `auto-compact: false` is the only way to turn the structural trigger off.
func TestApplyConfigAutoCompactOptOut(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "auto-compact: false\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.AutoCompact {
		t.Error("opts.autoCompact = true; want the file's explicit false to opt out")
	}
}

// The auto-title config block parses into opts.autoTitle: a file-only, default-TRUE key, so the
// automatic session-naming call runs unless a config explicitly opts out. The seeded template is
// in the table because it is what a first run actually resolves — the key ships commented, so it
// must land on the same default as an empty file.
func TestApplyConfigAutoTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fileYAML string
		want     bool
	}{
		{name: "absent key ⇒ the default", want: true},
		{name: "an explicit false opts out", fileYAML: "auto-title: false\n", want: false},
		{name: "an explicit true is the default, said out loud", fileYAML: "auto-title: true\n", want: true},
		{name: "the seeded template resolves the default", fileYAML: string(defaultConfigYAML), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			if tt.fileYAML != "" {
				writeConfigHome(t, home, tt.fileYAML)
			}
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.AutoTitle != tt.want {
				t.Errorf("opts.autoTitle = %v; want %v", opts.AutoTitle, tt.want)
			}
		})
	}
}

// The context-window config block parses into opts.contextWindow (item 3): a file-only key (no
// flag/env). This proves the config-file surface lands in opts; the downstream opts →
// ContextConfig.MaxContextTokens threading (which the Budget and Compaction bind against) is
// pinned separately by TestRunRootThreadsContextWindow in wire_test.go.
func TestApplyConfigContextWindow(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "context-window: 65536\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.ContextWindow != 65536 {
		t.Errorf("opts.contextWindow = %d; want the file's explicit 65536", opts.ContextWindow)
	}
}

// The mcp-servers config block parses into opts.mcpServers (P3.15): a stdio and an HTTP server,
// each mapped across to mcp.ServerConfig, so the composition root can connect them.
func TestApplyConfigMCPServers(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
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
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := []mcp.ServerConfig{
		{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp", Args: []string{"--stdio"}, Env: []string{"TOKEN=x"}},
		{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"},
	}
	if !reflect.DeepEqual(opts.MCPServers, want) {
		t.Errorf("mcpServers = %+v; want %+v", opts.MCPServers, want)
	}
}

// The `tools:` block round-trips: the disabled roster parses into opts.toolsDisabled in file
// order, an absent block leaves the whole roster standing, and a name matching no tool is a NOTICE
// rather than a startup error — the rest of the list still applies, so pruning a roster can never
// cost the user their session.
func TestApplyConfigToolsDisabled(t *testing.T) {
	t.Parallel()

	t.Run("the listed names resolve in file order", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "")
		writeConfigHome(t, home, "tools:\n  disabled: [view_diff, python_exec]\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if want := []string{"view_diff", "python_exec"}; !reflect.DeepEqual(opts.ToolsDisabled, want) {
			t.Errorf("toolsDisabled = %v; want %v", opts.ToolsDisabled, want)
		}
	})

	t.Run("an absent block disables nothing", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "")
		writeConfigHome(t, home, "mode: plan\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if len(opts.ToolsDisabled) != 0 {
			t.Errorf("toolsDisabled = %v; want nothing disabled", opts.ToolsDisabled)
		}
	})

	t.Run("a name that is no tool warns and is otherwise ignored", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "")
		writeConfigHome(t, home, "tools:\n  disabled: [grepp, grep]\n")
		opts := Options{ConfigDir: home}
		var notices []string
		err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, func(n string) { notices = append(notices, n) })
		if err != nil {
			t.Fatalf("an unrecognised tool name must not stop startup: %v", err)
		}
		if want := []string{"grepp", "grep"}; !reflect.DeepEqual(opts.ToolsDisabled, want) {
			t.Errorf("toolsDisabled = %v; want the list as written %v", opts.ToolsDisabled, want)
		}
		var warned string
		for _, n := range notices {
			if strings.Contains(n, "tools.disabled") {
				warned = n
			}
		}
		switch {
		case warned == "":
			t.Fatalf("no notice named tools.disabled; got %v", notices)
		case !strings.Contains(warned, "grepp"):
			t.Errorf("the notice must name the unrecognised entry: %q", warned)
		case strings.Contains(warned, `"grep"`):
			t.Errorf("the notice must not name the entry that IS a tool: %q", warned)
		}
	})
}

// The servers config block parses into opts.servers: every entry, in file order, with all four
// fields — so the composition root can offer them as the servers this session may move to. It is
// file-only, like mcp-servers, and the two optional keys default empty (a keyless server with no
// model hint), which is what a plain local entry looks like. The optional `llama-launcher:` key
// travels the same way and UNRESOLVED, since only the composition root knows what `auto` means.
func TestApplyConfigServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       []ServerEntry
	}{
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
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", Model: "gpt-oss-20b"},
				{Name: "rented-box", Endpoint: "https://llm.example.com", APIKey: "sk-rented-token", Model: "qwen2.5-coder"},
			},
		},
		{
			name:       "api-key and model are optional (a keyless server, no hint)",
			configYAML: "servers:\n  - name: laptop\n    endpoint: http://127.0.0.1:1111\nserver: laptop\n",
			want:       []ServerEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
		},
		{
			// The launcher key rides on the entry it describes, and reaches the root as written:
			// resolving `auto` is the composition root's job, not resolution's.
			name: "the optional llama-launcher key travels per entry, as written",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    llama-launcher: auto
  - name: rented-box
    endpoint: https://llm.example.com
    llama-launcher: ~/elsewhere/launcher.yaml
  - name: openrouter
    endpoint: https://openrouter.ai/api/v1
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", LlamaLauncher: "auto"},
				{Name: "rented-box", Endpoint: "https://llm.example.com", LlamaLauncher: "~/elsewhere/launcher.yaml"},
				{Name: "openrouter", Endpoint: "https://openrouter.ai/api/v1"},
			},
		},
		{
			// The cap is a pin per entry (ADR 0039 decision 2): a positive value travels as written,
			// and both spellings of unset — the absent key and an explicit 0 — resolve to the same
			// zero, which is what tells the resolver to ask the server instead.
			name: "the optional parallel-agents cap travels per entry, absent and 0 alike",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    parallel-agents: 4
  - name: single-slot
    endpoint: http://192.168.64.1:2222
    parallel-agents: 1
  - name: explicit-zero
    endpoint: http://192.168.64.1:3333
    parallel-agents: 0
  - name: openrouter
    endpoint: https://openrouter.ai/api/v1
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", ParallelAgents: 4},
				{Name: "single-slot", Endpoint: "http://192.168.64.1:2222", ParallelAgents: 1},
				{Name: "explicit-zero", Endpoint: "http://192.168.64.1:3333"},
				{Name: "openrouter", Endpoint: "https://openrouter.ai/api/v1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, tt.configYAML)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.Servers, tt.want) {
				t.Errorf("opts.servers = %#v; want %#v", opts.Servers, tt.want)
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
		{
			name: "an entry whose llama-launcher value is only whitespace",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    llama-launcher: \"   \"\n",
			wantErr: []string{"servers: entry 1", "box", "llama-launcher: is only whitespace", "set auto"},
		},
		{
			// Absent is already the off state, so a second spelling of it is a defect in the file.
			name: "an entry spelling the launcher off",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    llama-launcher: off\n",
			wantErr: []string{"servers: entry 1", "box", "off is not a value", "remove the key"},
		},
		{
			name: "an entry spelling the launcher OFF in another casing",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    llama-launcher: \" OFF \"\n",
			wantErr: []string{"servers: entry 1", "box", "off is not a value", "remove the key"},
		},
		{
			// A launcher on another machine is an mcp-servers: entry; this key takes a local path.
			name: "an entry whose llama-launcher value is a URL",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    llama-launcher: http://192.168.64.1:7331/mcp\n",
			wantErr: []string{"servers: entry 1", "box", "looks like a URL", "mcp-servers:"},
		},
		{
			name: "a launcher defect below a well-formed entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"  - name: other\n    endpoint: http://two:1111\n    llama-launcher: off\n",
			wantErr: []string{"servers: entry 2", "other", "off is not a value"},
		},
		{
			// Absent and 0 already mean discover and every N ≥ 1 is a pin, so a negative cap is the
			// one parallel-agents value with nothing left to mean.
			name: "an entry whose parallel-agents cap is negative",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    parallel-agents: -1\n",
			wantErr: []string{"servers: entry 1", "box", "parallel-agents: -1 is negative", "1 or more"},
		},
		{
			name: "a negative cap below a well-formed entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"  - name: other\n    endpoint: http://two:1111\n    parallel-agents: -4\n",
			wantErr: []string{"servers: entry 2", "other", "parallel-agents: -4 is negative"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.configYAML)
			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("ApplyConfig = nil error; want the entry refused (opts.servers = %#v)", opts.Servers)
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
	home := testConfigHome(t, "")
	const configYAML = `mechanisms:
  validate: true
  syntax: true
  truncate_history: false
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := map[string]bool{"validate": true, "syntax": true, "truncate_history": false}
	if !reflect.DeepEqual(opts.Mechanisms, want) {
		t.Errorf("opts.mechanisms = %+v; want %+v", opts.Mechanisms, want)
	}
}

// With no mechanisms block, opts.mechanisms is nil — every Mechanism default-off (D1), the
// byte-identical anchor: a config without the block behaves exactly as before.
func TestApplyConfigNoMechanismsIsNil(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.Mechanisms != nil {
		t.Errorf("opts.mechanisms = %+v; want nil (no block ⇒ nothing enabled)", opts.Mechanisms)
	}
}

// The validated-sets config block parses into opts.validatedSetsEnable / opts.validatedSetsAlias
// (ADR 0016 realisation): the §5 off-switch and the §3 explicit carry-over map. File-only, like
// mechanisms, so this proves the config surface lands end-to-end.
func TestApplyConfigValidatedSets(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `validated-sets:
  enable: false
  alias:
    gemma-4-e4b-it-qat: gemma-4-e4b-it-qat
    my-quant: gemma-4-e4b-it-qat
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	if opts.ValidatedSetsEnable {
		t.Errorf("opts.validatedSetsEnable = true; want false (explicit enable: false)")
	}
	wantAlias := map[string]string{"gemma-4-e4b-it-qat": "gemma-4-e4b-it-qat", "my-quant": "gemma-4-e4b-it-qat"}
	if !reflect.DeepEqual(opts.ValidatedSetsAlias, wantAlias) {
		t.Errorf("opts.validatedSetsAlias = %+v; want %+v", opts.ValidatedSetsAlias, wantAlias)
	}
}

// The present config block parses into opts.present (ADR 0019): all four keys, file-only like
// the blocks around it, so the composition root can build the ladder's mechanisms from them.
func TestApplyConfigPresent(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `present:
  auto-open: false
  command: "zed {path}"
  port: 8934
  host: 192.168.64.2
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := PresentSettings{AutoOpen: false, Command: "zed {path}", Port: 8934, Host: "192.168.64.2"}
	if opts.Present != want {
		t.Errorf("opts.present = %+v; want %+v", opts.Present, want)
	}
}

// A present block that sets ONE key leaves the other three at their defaults — the reason
// auto-open is a pointer on the on-disk schema. Setting `port:` alone must not read as
// `auto-open: false` and silently disable the rung the feature exists for.
func TestApplyConfigPresentPartialKeepsDefaults(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = "present:\n  port: 8934\n"
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := PresentSettings{AutoOpen: true, Port: 8934}
	if opts.Present != want {
		t.Errorf("opts.present = %+v; want %+v (an absent auto-open keeps the default)", opts.Present, want)
	}
}

// With no present block at all, the ladder's defaults stand: auto-open ON (the headline want —
// a run on the user's own desktop opens the deliverable), no command override, an ephemeral
// port, and a detected advertise host.
func TestApplyConfigNoPresentDefaultsAutoOpen(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if want := (PresentSettings{AutoOpen: true}); opts.Present != want {
		t.Errorf("opts.present = %+v; want %+v (no block ⇒ auto-open on, everything else zero)", opts.Present, want)
	}
}

// An out-of-range present.port is a loud startup error naming the key, not a degraded rung at
// the first presentation: the doc server would fail at the listen, deep inside a Turn, where all
// the user would see is "could not serve" with no hint that a config typo caused it.
func TestApplyConfigPresentPortRangeErrors(t *testing.T) {
	t.Parallel()
	for _, port := range []string{"-1", "65536", "99999"} {
		home := testConfigHome(t, "")
		configYAML := "present:\n  port: " + port + "\n"
		writeConfigHome(t, home, configYAML)
		opts := Options{ConfigDir: home}
		err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
		if err == nil {
			t.Errorf("ApplyConfig with present.port %s: want an error, got nil", port)
			continue
		}
		if !strings.Contains(err.Error(), "present.port") {
			t.Errorf("error = %q; want it to name the present.port key", err)
		}
	}

	// The boundaries themselves are valid: 0 (ephemeral, the default) and 65535.
	for _, port := range []string{"0", "65535"} {
		home := testConfigHome(t, "")
		configYAML := "present:\n  port: " + port + "\n"
		writeConfigHome(t, home, configYAML)
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
			t.Errorf("ApplyConfig with present.port %s: %v", port, err)
		}
	}
}

// The three system-prompt keys parse into opts.systemPrompt (ADR 0023): the global prompt and the
// per-model overrides, file-only like the blocks around them. This is the end-to-end proof that
// the keys reach the composition root, which is where ResolveSystemPrompt then selects one.
func TestApplyConfigSystemPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configYAML string
		want       SystemPromptSettings
	}{
		{
			name:       "an inline prompt reaches the global source",
			configYAML: "system-prompt-text: \"hi {{workspace}}\"\n",
			want:       SystemPromptSettings{Global: PromptSource{Text: "hi {{workspace}}"}},
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
			want: SystemPromptSettings{
				Global: PromptSource{File: "prompts/global.md"},
				Models: map[string]PromptSource{
					"qwen2.5-coder": {Text: "code first, prose second"},
					"gpt-oss-20b":   {File: "~/prompts/gpt-oss.md"},
				},
			},
		},
		{
			name:       "no system-prompt key leaves the zero value — no prompt",
			configYAML: "mode: plan\n",
			want:       SystemPromptSettings{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.configYAML)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.SystemPrompt, tt.want) {
				t.Errorf("opts.systemPrompt = %+v; want %+v", opts.SystemPrompt, tt.want)
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
		sp   SystemPromptSettings
		// wantErr are the substrings the message must carry; empty ⇒ the block must validate.
		wantErr []string
	}{
		{name: "an inline prompt alone", sp: SystemPromptSettings{Global: PromptSource{Text: "hi"}}},
		{name: "a file prompt alone", sp: SystemPromptSettings{Global: PromptSource{File: "p.md"}}},
		{name: "nothing configured at all", sp: SystemPromptSettings{}},
		{
			name:    "both spellings at the global level",
			sp:      SystemPromptSettings{Global: PromptSource{Text: "hi", File: "p.md"}},
			wantErr: []string{"system-prompt-text", "system-prompt-file", "both"},
		},
		{
			name:    "both spellings in one model entry",
			sp:      SystemPromptSettings{Models: map[string]PromptSource{"qwen2.5-coder": {Text: "hi", File: "p.md"}}},
			wantErr: []string{`system-prompt-models["qwen2.5-coder"]`, "system-prompt-text", "system-prompt-file"},
		},
		{
			name:    "a model entry that sets neither spelling",
			sp:      SystemPromptSettings{Models: map[string]PromptSource{"qwen2.5-coder": {}}},
			wantErr: []string{`system-prompt-models["qwen2.5-coder"]`, "neither"},
		},
		{
			name: "a well-formed model entry beside a global prompt",
			sp: SystemPromptSettings{
				Global: PromptSource{Text: "hi"},
				Models: map[string]PromptSource{"qwen2.5-coder": {File: "p.md"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.sp.Validate()
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

// ResolveSystemPrompt collapses the block into the ONE template this session runs with, for the
// RESOLVED model (ADR 0023): whole-entry replacement on an exact model match, an inert entry for
// every other model, `~` and apogee-home-relative file paths, and the two checks that belong to
// the selected source alone — the file must read and the placeholders must be the known three.
func TestResolveSystemPrompt(t *testing.T) {
	// Deliberately NOT parallel: the `~` case redirects the environment os.UserHomeDir reads.
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)        // POSIX
	t.Setenv("USERPROFILE", userHome) // Windows
	home := testConfigHome(t, "")     // the apogee home a relative path resolves against
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
		sp   SystemPromptSettings
		want string
		// wantErr are the substrings the message must carry; empty ⇒ want must be returned.
		wantErr []string
	}{
		{
			name: "the global inline prompt is selected",
			sp:   SystemPromptSettings{Global: PromptSource{Text: "hi {{workspace}}"}},
			want: "hi {{workspace}}",
		},
		{
			name: "a matching model entry replaces the global prompt",
			sp: SystemPromptSettings{
				Global: PromptSource{Text: "the global prompt"},
				Models: map[string]PromptSource{model: {Text: "the per-model prompt"}},
			},
			want: "the per-model prompt",
		},
		{
			name: "a matching entry with only a file replaces a global text whole",
			sp: SystemPromptSettings{
				Global: PromptSource{Text: "the global prompt"},
				Models: map[string]PromptSource{model: {File: "prompts/per-model.md"}},
			},
			want: "the per-model file",
		},
		{
			name: "an entry naming another model is inert and its file is never read",
			sp: SystemPromptSettings{
				Global: PromptSource{Text: "the global prompt"},
				Models: map[string]PromptSource{"some-other-model": {File: "prompts/absent.md"}},
			},
			want: "the global prompt",
		},
		{name: "no prompt configured anywhere", sp: SystemPromptSettings{}, want: ""},
		{
			name: "an absolute file is read as written",
			sp:   SystemPromptSettings{Global: PromptSource{File: absFile}},
			want: "from an absolute path",
		},
		{
			name: "a relative file resolves against the apogee home",
			sp:   SystemPromptSettings{Global: PromptSource{File: filepath.Join("prompts", "relative.md")}},
			want: "from the apogee home",
		},
		{
			name: "a ~-prefixed file resolves against the user home",
			sp:   SystemPromptSettings{Global: PromptSource{File: "~/prompts/user.md"}},
			want: "from the user home",
		},
		{
			name:    "an unreadable selected file names the key and the path",
			sp:      SystemPromptSettings{Global: PromptSource{File: filepath.Join("prompts", "absent.md")}},
			wantErr: []string{"system-prompt-file", filepath.Join(home, "prompts", "absent.md")},
		},
		{
			name:    "an unknown placeholder names the source key and the known three",
			sp:      SystemPromptSettings{Global: PromptSource{Text: "hi {{bogus}}"}},
			wantErr: []string{"system-prompt-text", "{{bogus}}", "{{workspace}}", "{{datetime}}", "{{mode}}"},
		},
		{
			name:    "an unknown placeholder in a model entry names that entry",
			sp:      SystemPromptSettings{Models: map[string]PromptSource{model: {Text: "hi {{nope}}"}}},
			wantErr: []string{`system-prompt-models["` + model + `"]`, "{{nope}}", "{{workspace}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSystemPrompt(tt.sp, model, home, readFile)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("ResolveSystemPrompt: %v", err)
				}
				if got != tt.want {
					t.Errorf("template = %q; want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ResolveSystemPrompt = %q; want an error", got)
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
// root folds into domain.Config.ContextFiles. The table walks every shape of the block, including
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
			configYAML: "mode: plan\n",
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
			home := testConfigHome(t, "")
			if tt.configYAML != "" {
				writeConfigHome(t, home, tt.configYAML)
			}
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if !reflect.DeepEqual(opts.ContextFiles, tt.want) {
				t.Errorf("opts.contextFiles = %#v; want %#v", opts.ContextFiles, tt.want)
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
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.configYAML)
			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("ApplyConfig = nil error; want the name refused (opts.contextFiles = %#v)", opts.ContextFiles)
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
	home := testConfigHome(t, "")
	const configYAML = "context-files:\n  names: [AGENTS.md, docs/nothing-here.md]\n"
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	want := []string{"AGENTS.md", "docs/nothing-here.md"}
	if !reflect.DeepEqual(opts.ContextFiles, want) {
		t.Errorf("opts.contextFiles = %#v; want %#v (existence is the engine's question, not config's)",
			opts.ContextFiles, want)
	}
}

// The ui config block parses into opts.ui: every key, file-only like the blocks around it, so the
// composition root can hand the renderer a style, a colour flag, a scroll-bar flag and a colour
// scheme it never has to parse.
func TestApplyConfigUI(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `ui:
  spinner: glitter
  spinner-color: false
  show-scrollbar: false
  color-scheme: light
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := UISettings{Spinner: tui.SpinnerGlitter, SpinnerColor: false, ShowScrollbar: false, ColorScheme: "light"}
	if opts.UI != want {
		t.Errorf("opts.ui = %+v; want %+v", opts.UI, want)
	}
}

// A `color-scheme:` naming a scheme that does not exist is NOT a startup error — the one ui key
// that is deliberately forgiving (ADR 0040 design call 8). The name travels through resolution as
// written and the palette is resolved later, where an unresolvable one costs a warning and the
// default colours rather than the session. Pinned because the two keys beside it do the opposite:
// an unknown spinner style fails startup loudly, and the temptation is to make this one match.
func TestApplyConfigUnknownColorSchemeIsNotAStartupError(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "ui:\n  color-scheme: no-such-scheme\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig refused an unknown colour scheme: %v", err)
	}
	if opts.UI.ColorScheme != "no-such-scheme" {
		t.Errorf("opts.ui.colorScheme = %q; want the name as written, for the resolver to warn about",
			opts.UI.ColorScheme)
	}
}

// The ui keys are INDEPENDENT, from the on-disk block onward: a block that names a style leaves the
// colour loop at its default, one that turns the loop off leaves the style at its default, and one
// that hides the scroll bar leaves both spinner keys alone. This is the reason spinner-color and
// show-scrollbar are pointers on the on-disk schema — `spinner: classic` alone must not read as
// "and no colour, and no scroll bar", which is a different look from what was asked for.
func TestApplyConfigUIPartialKeepsTheOtherDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want UISettings
	}{
		{
			name: "only spinner: → the colour loop stays on and the bar stays shown",
			yaml: "ui:\n  spinner: classic\n",
			want: UISettings{Spinner: tui.SpinnerClassic, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"},
		},
		{
			name: "only spinner-color: false → the style stays the default and the bar stays shown",
			yaml: "ui:\n  spinner-color: false\n",
			want: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: false, ShowScrollbar: true, ColorScheme: "dark"},
		},
		{
			name: "only show-scrollbar: false → the bar goes, the spinner keys stay put",
			yaml: "ui:\n  show-scrollbar: false\n",
			want: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: false, ColorScheme: "dark"},
		},
		{
			// The explicit `true` and the absent key resolve alike — pinned so the pointer's
			// present-and-true branch is exercised, not just its nil one.
			name: "only show-scrollbar: true → the shipped default, said out loud",
			yaml: "ui:\n  show-scrollbar: true\n",
			want: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"},
		},
		{
			name: "only color-scheme: → the spinner keys and the bar stay put",
			yaml: "ui:\n  color-scheme: light\n",
			want: UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "light"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.yaml)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.UI != tt.want {
				t.Errorf("opts.ui = %+v; want %+v", opts.UI, tt.want)
			}
		})
	}
}

// With no ui block at all, the renderer's own defaults stand: the default spinner style with its
// colour loop on, and the scroll bar shown. This is the anchor for "an absent block changes
// nothing".
func TestApplyConfigNoUIDefaults(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.UI != wantUIDefault {
		t.Errorf("opts.ui = %+v; want %+v (no block ⇒ the default style, colour loop on, bar shown)", opts.UI, wantUIDefault)
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
			home := testConfigHome(t, "")
			if tt.yaml != "" {
				writeConfigHome(t, home, tt.yaml)
			}
			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("ApplyConfig with %q: want an error, got nil", tt.yaml)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q; want it to contain %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.CursorShape != tt.want {
				t.Errorf("opts.cursorShape = %q; want %q", opts.CursorShape, tt.want)
			}
			// The renderer takes a shape, not a name: what the binary hands the TUI must parse.
			if _, err := tui.ParseCursorShape(opts.CursorShape); err != nil {
				t.Errorf("the resolved shape %q does not parse for the renderer: %v", opts.CursorShape, err)
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
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "ui:\n  spinner: sparkle\n")
	opts := Options{ConfigDir: home}
	err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
	if err == nil {
		t.Fatal("ApplyConfig with ui.spinner: sparkle: want an error, got nil")
	}
	for _, want := range []string{"ui.spinner", "sparkle", "snake", "glitter", "classic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q; want it to contain %q", err, want)
		}
	}

	// Every style this build knows is accepted, so the check rejects only what it should.
	for _, style := range []tui.SpinnerStyle{tui.SpinnerSnake, tui.SpinnerGlitter, tui.SpinnerClassic} {
		home := testConfigHome(t, "")
		writeConfigHome(t, home, "ui:\n  spinner: "+string(style)+"\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
			t.Errorf("ApplyConfig with ui.spinner: %s: %v", style, err)
		}
	}
}

// With no validated-sets block, the surface defaults ON with no aliases — a matching set
// applies (≥ medium confidence) or is offered (low) without any config.
func TestApplyConfigNoValidatedSetsDefaultsOn(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if !opts.ValidatedSetsEnable {
		t.Errorf("opts.validatedSetsEnable = false; want true (default on)")
	}
	if opts.ValidatedSetsAlias != nil {
		t.Errorf("opts.validatedSetsAlias = %+v; want nil", opts.ValidatedSetsAlias)
	}
}

// The model-profile config block reaches opts.profile — which runRoot folds directly into
// domain.Config.Profile: a markdown-fenced tool-call format plus a <think> thinking block map
// across to the domain ModelProfile the loop translates to its parsers at the seam (item 1 has
// no loop consumer yet; this proves the config surface lands end-to-end).
func TestApplyConfigModelProfile(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `model-profile:
  tool-call-format: markdown-fenced
  thinking:
    style: delimited
    start: "<think>"
    end: "</think>"
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := domain.ModelProfile{
		ToolCallFormat: domain.FormatMarkdownFenced,
		Thinking:       domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}
	if !reflect.DeepEqual(opts.Profile, want) {
		t.Errorf("opts.profile = %+v; want %+v", opts.Profile, want)
	}
}

// With no model-profile block, opts.profile is the zero ModelProfile — native tool calls with no
// inline thinking (today's behaviour), the byte-identical anchor this item must preserve.
func TestApplyConfigNoProfileIsZero(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if (opts.Profile != domain.ModelProfile{}) {
		t.Errorf("opts.profile = %+v; want the zero ModelProfile", opts.Profile)
	}
}

// APOGEE_CONFIG / APOGEE_WORKSPACE fill the config dir and workspace when their flags are
// not set, and the config file is then read from that env-resolved home.
func TestApplyConfigEnvDirsAndFile(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "cursor-shape: bar\n")
	ws := t.TempDir()
	getenv := func(k string) string {
		switch k {
		case EnvConfig:
			return home
		case EnvWorkspace:
			return ws
		default:
			return ""
		}
	}
	opts := Options{}
	if err := ApplyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.ConfigDir != home {
		t.Errorf("configDir = %q; want the APOGEE_CONFIG value %q", opts.ConfigDir, home)
	}
	if opts.Workspace != ws {
		t.Errorf("workspace = %q; want the APOGEE_WORKSPACE value %q", opts.Workspace, ws)
	}
	if opts.CursorShape != "bar" {
		t.Errorf("cursorShape = %q; want it read from the env-resolved config home", opts.CursorShape)
	}
}

// An explicit --config flag wins over APOGEE_CONFIG (the flag is not overlaid by env).
func TestApplyConfigFlagDirBeatsEnvDir(t *testing.T) {
	t.Parallel()
	flagHome := testConfigHome(t, "")
	getenv := func(k string) string {
		if k == EnvConfig {
			return filepath.Join(t.TempDir(), "ignored")
		}
		return ""
	}
	changed := func(name string) bool { return name == "config" }
	opts := Options{ConfigDir: flagHome}
	if err := ApplyConfig(&opts, changed, getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if opts.ConfigDir != flagHome {
		t.Errorf("configDir = %q; want the flag value %q (env must not overlay a set flag)", opts.ConfigDir, flagHome)
	}
}

// A malformed config file is a hard error — a typo'd setting must not be silently ignored.
func TestApplyConfigMalformedFileErrors(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "endpoint: [not a string\n")
	opts := Options{ConfigDir: home}
	err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)
	if err == nil {
		t.Fatal("malformed config: want an error, got nil")
	}
}

// A set-but-unparseable APOGEE_BYPASS is a hard error rather than a silently-ignored flag.
func TestApplyConfigBadBypassEnvErrors(t *testing.T) {
	t.Parallel()
	getenv := func(k string) string {
		if k == EnvBypass {
			return "yes-please"
		}
		return ""
	}
	opts := Options{ConfigDir: t.TempDir()}
	err := ApplyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify)
	if err == nil {
		t.Fatal("invalid APOGEE_BYPASS: want an error, got nil")
	}
}

// An absent config file is not an error — a config file is optional.
func TestLoadFileConfigAbsentIsEmpty(t *testing.T) {
	t.Parallel()
	l, err := LoadFileConfig(filepath.Join(t.TempDir(), "config.yaml"), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("absent config: unexpected error %v", err)
	}
	if !reflect.DeepEqual(l, Layer{}) {
		t.Errorf("absent config produced a non-empty layer: %+v", l)
	}
}

// A read error other than not-exist propagates (it is not swallowed as "absent").
func TestLoadFileConfigReadErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	readFile := func(string) ([]byte, error) { return nil, boom }
	_, err := LoadFileConfig("/some/config.yaml", readFile, noNotify)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read error = %v; want it propagated (not treated as absent)", err)
	}
}

// ----------------------------------------------------------------------------
// Which source won (the /settings override marker's input)
// ----------------------------------------------------------------------------

// overrideSources must agree with the LAYERS themselves about which source beat the config file:
// the marker it feeds tells the user their file's value is not the one in force, so a marker naming
// a source that did not win would be a lie in both directions. The cases mirror flagLayer's and
// envLayer's own predicates, including the shape that is easy to get wrong — an empty variable is
// not a setting.
func TestOverrideSourcesNameTheWinningSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		changed map[string]bool
		env     map[string]string
		want    map[string]Source
	}{
		{
			name: "nothing set leaves every key on the file",
			want: map[string]Source{},
		},
		{
			name: "an environment variable beats the file",
			env:  map[string]string{EnvServer: "workstation"},
			want: map[string]Source{"server": SourceEnv},
		},
		{
			name:    "an explicitly-set flag beats its own variable",
			changed: map[string]bool{"mode": true},
			env:     map[string]string{EnvMode: "auto"},
			want:    map[string]Source{"mode": SourceFlag},
		},
		{
			name: "an empty variable is not a setting",
			env:  map[string]string{EnvServer: ""},
			want: map[string]Source{},
		},
		{
			// The raw startup overrides name no config key since ADR 0036, so nothing marks a row
			// for them: a marker for a key the pane does not show would point at nothing.
			name:    "the raw endpoint override marks no row",
			changed: map[string]bool{"endpoint": true},
			env:     map[string]string{EnvEndpoint: "http://box:1111", EnvAPIKey: "sk-token"},
			want:    map[string]Source{},
		},
		{
			name:    "several keys are marked independently",
			changed: map[string]bool{"bypass": true},
			env:     map[string]string{EnvServer: "workstation"},
			want:    map[string]Source{"bypass": SourceFlag, "server": SourceEnv},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := overrideSources(
				func(name string) bool { return tc.changed[name] },
				func(name string) string { return tc.env[name] },
			)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("overrideSources() = %v; want %v", got, tc.want)
			}
		})
	}
}

// ApplyConfig records the winning sources on the resolved options, because that is the only place
// the layers still exist: the /settings pane is handed the resolved options long after they have
// been collapsed into single values.
func TestApplyConfigRecordsOverrideSources(t *testing.T) {
	t.Parallel()
	getenv := func(name string) string {
		if name == EnvServer {
			return testServerName
		}
		return ""
	}
	opts := Options{ConfigDir: testConfigHome(t, ""), Mode: "auto"}
	changed := func(name string) bool { return name == "mode" }
	if err := ApplyConfig(&opts, changed, getenv, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	want := map[string]Source{"server": SourceEnv, "mode": SourceFlag}
	if !reflect.DeepEqual(opts.Overrides, want) {
		t.Errorf("opts.overrides = %v; want %v", opts.Overrides, want)
	}
}

// The Parallel agents cap has three ranks and no fourth (ADR 0039 decision 2): a pin is never
// overruled, discovery answers when nothing is pinned, and 1 — strictly serial, today's behaviour —
// is what a session falls back to when neither can say.
func TestResolveParallelAgents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pinned     int
		discovered int
		want       int
	}{
		{name: "pin beats discovery", pinned: 2, discovered: 8, want: 2},
		{name: "a pin of 1 is still a pin", pinned: 1, discovered: 8, want: 1},
		{name: "discovery beats the default", pinned: 0, discovered: 4, want: 4},
		{name: "neither says anything", pinned: 0, discovered: 0, want: 1},
		{name: "a nonsense pin falls through", pinned: -3, discovered: 4, want: 4},
		{name: "a nonsense slot count falls through", pinned: 0, discovered: -1, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveParallelAgents(tt.pinned, tt.discovered); got != tt.want {
				t.Errorf("ResolveParallelAgents(%d, %d) = %d, want %d", tt.pinned, tt.discovered, got, tt.want)
			}
		})
	}
}
