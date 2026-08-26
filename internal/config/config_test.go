package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/profiles"
)

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }
func intptr(n int) *int       { return &n }

// wantUIDefault is the resolved `ui:` block a config that configures none must produce: the
// default spinner style with its colour loop on, the transcript's scroll bar shown, and the stall
// guard waiting 90 seconds of engine silence out. It is spelled out rather than taken from
// defaultUISettings, so a change to any shipped default shows up here as a failure instead of
// silently agreeing with itself.
var wantUIDefault = UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true,
	ColorScheme: "dark", StallAfter: 90 * time.Second}

// testHostID is the machine identity injected into resolution so the Host acknowledgement
// ladder is pinned off whatever host the tests happen to run on.
const testHostID = "testbox-a1b2c3"

// unidentifiedTestHostID is what platform.HostID() composes on a host that can supply
// neither a hostname nor a machine id: the one value that is identical on every such
// machine, and therefore the one an acknowledgement must never match. It is spelled out
// rather than computed, so a change to the composition shows up here as a failure.
const unidentifiedTestHostID = "unknown-e3b0c4"

// The precedence rule itself: a flag beats an env var beats the file beats the default, resolved
// per field (phase-2 detail plan §4 P2.5). The sources are the real ones — a parsed fileConfig, a
// getenv, an explicitly-changed flag — because there is no carrier between them and the Options
// they resolve onto any more: what the passes write IS the answer.
//
// Each case names only the fields its sources MOVE off the defaults; the defaults themselves are
// wantDefaults', spelled out once. The keys with no environment variable and no flag are file-only
// by construction rather than by convention — their rows carry no accessor for either source, so
// there is no path to close and nothing here can hand them one; that fence is asserted over the
// whole schema by TestMultiSourceKeysReadTheRegistry below.
func TestResolvePrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		file    fileConfig
		env     map[string]string
		flags   Options
		changed []string
		want    func(*Options)
	}{
		{
			name: "nothing stated → the defaults",
		},
		{
			name: "the file fills the keys it states",
			file: fileConfig{Server: "file-box", Mode: "plan", Bypass: boolptr(true)},
			want: func(o *Options) { o.StartupServer = "file-box"; o.Mode = "plan"; o.Bypass = true },
		},
		{
			name: "env beats file, file fills the rest",
			file: fileConfig{Server: "file-box", Mode: "plan"},
			env:  map[string]string{EnvServer: "env-box"},
			want: func(o *Options) { o.StartupServer = "env-box"; o.Mode = "plan" },
		},
		{
			name:    "flag beats env beats file, per field",
			file:    fileConfig{Server: "file-box", Mode: "plan"},
			env:     map[string]string{EnvServer: "env-box", EnvMode: "auto"},
			flags:   Options{StartupServer: "flag-box"},
			changed: []string{"server"},
			want:    func(o *Options) { o.StartupServer = "flag-box"; o.Mode = "auto" },
		},
		{
			// The flag's own zero value is not a setting: only an explicitly-changed flag is read, which
			// is what keeps an unset `--bypass` from shadowing the file below it.
			name:  "an unchanged flag does not shadow the file",
			file:  fileConfig{Bypass: boolptr(true)},
			flags: Options{Bypass: false},
			want:  func(o *Options) { o.Bypass = true },
		},
		{
			name:    "an explicit false in a higher source overrides a true below it",
			file:    fileConfig{Bypass: boolptr(true)},
			flags:   Options{Bypass: false},
			changed: []string{"bypass"},
			want:    func(o *Options) { o.Bypass = false },
		},
		{
			// The servers list is file-only: it describes machines, not this invocation, so neither the
			// environment nor a flag can name one (only `server:` — which of them to start on — has
			// sources above the file).
			name: "servers is file-only",
			file: fileConfig{Servers: []ServerEntry{{Name: "box", Endpoint: "http://box:1111"}}},
			want: func(o *Options) { o.Servers = []ServerEntry{{Name: "box", Endpoint: "http://box:1111"}} },
		},
		{
			name: "confine-to-workspace is global-config-only and defaults true",
			file: fileConfig{ConfineToWorkspace: boolptr(false)},
			want: func(o *Options) { o.ConfineToWorkspace = false },
		},
		{
			name: "use-project-skills is file-only and defaults true",
			file: fileConfig{UseProjectSkills: boolptr(false)},
			want: func(o *Options) { o.UseProjectSkills = false },
		},
		{
			name: "auto-compact is file-only and defaults true",
			file: fileConfig{AutoCompact: boolptr(false)},
			want: func(o *Options) { o.AutoCompact = false },
		},
		{
			name: "delegate-max-steps is file-only and defaults 80",
			file: fileConfig{DelegateMaxSteps: intptr(12)},
			want: func(o *Options) { o.DelegateMaxSteps = 12 },
		},
		{
			// The one value a plain int could not carry: 0 is "unbounded", not "absent", so an
			// explicit zero has to survive resolution instead of resolving back to the default.
			name: "an explicit delegate-max-steps: 0 stays 0 — the documented spelling of unbounded",
			file: fileConfig{DelegateMaxSteps: intptr(0)},
			want: func(o *Options) { o.DelegateMaxSteps = 0 },
		},
		{
			name: "auto-title is file-only and defaults true",
			file: fileConfig{AutoTitle: boolptr(false)},
			want: func(o *Options) { o.AutoTitle = false },
		},
		{
			name: "an explicit auto-title: true resolves to the same value as an absent key",
			file: fileConfig{AutoTitle: boolptr(true)},
		},
		{
			name: "remember-model is file-only and defaults false",
			file: fileConfig{RememberModel: boolptr(true)},
			want: func(o *Options) { o.RememberModel = true },
		},
		{
			name: "context-window is file-only (default 0 ⇒ discover)",
			file: fileConfig{ContextWindow: 65536},
			want: func(o *Options) { o.ContextWindow = 65536 },
		},
		{
			name: "response-reserve is file-only (default 0 ⇒ the engine's own share)",
			file: fileConfig{ResponseReserve: 0.35},
			want: func(o *Options) { o.ResponseReserve = 0.35 },
		},
		{
			name: "a matching unconfined-hosts entry resolves confine-to-workspace to false",
			file: fileConfig{UnconfinedHosts: []UnconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}},
			want: func(o *Options) {
				o.ConfineToWorkspace = false
				o.UnconfinedHosts = []UnconfinedHost{{ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable"}}
			},
		},
		{
			name: "web-search endpoint is file-only (default empty)",
			file: fileConfig{WebSearch: "https://search.example.com"},
			want: func(o *Options) { o.WebSearchEndpoint = "https://search.example.com" },
		},
		{
			name: "mcp servers are file-only (default empty)",
			file: fileConfig{MCPServers: []mcpServerConfig{{Name: "github", Transport: "stdio", Command: "gh-mcp"}}},
			want: func(o *Options) {
				o.MCPServers = []mcp.ServerConfig{{Name: "github", Transport: mcp.TransportStdio, Command: "gh-mcp"}}
			},
		},
		{
			name: "the model-profiles map is file-only and is carried whole",
			file: fileConfig{ModelProfiles: map[string]modelProfileConfig{
				"gemma": {Thinking: thinkingConfig{Style: "delimited", Start: "<think>", End: "</think>"}},
			}},
			want: func(o *Options) {
				o.ModelProfiles = []profiles.Entry{{Pattern: "gemma", Profile: domain.ModelProfile{
					Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"}}}}
			},
		},
		{
			name: "mechanisms are file-only (default empty)",
			file: fileConfig{Mechanisms: map[string]bool{"validate": true, "syntax": false}},
			want: func(o *Options) { o.Mechanisms = map[string]bool{"validate": true, "syntax": false} },
		},
		{
			name: "the present block is file-only (all four keys)",
			file: fileConfig{Present: &presentConfig{AutoOpen: boolptr(false), Command: "zed {path}", Port: 8934, Host: "10.0.0.2"}},
			want: func(o *Options) {
				o.Present = PresentSettings{AutoOpen: false, Command: "zed {path}", Port: 8934, Host: "10.0.0.2"}
			},
		},
		{
			name: "the ui block is file-only (three of its keys stated)",
			file: fileConfig{UI: &uiConfig{Spinner: "glitter", SpinnerColor: boolptr(false), ShowScrollbar: boolptr(false)}},
			want: func(o *Options) {
				o.UI = UISettings{Spinner: domain.SpinnerGlitter, SpinnerColor: false, ShowScrollbar: false,
					ColorScheme: "dark", StallAfter: 90 * time.Second}
			},
		},
		{
			// The keys are independent: naming a style says nothing about the colour loop.
			name: "ui with only spinner: set → the colour loop stays at its default",
			file: fileConfig{UI: &uiConfig{Spinner: "classic"}},
			want: func(o *Options) { o.UI.Spinner = domain.SpinnerClassic },
		},
		{
			// …and the other way round: turning the loop off does not change which style paints.
			name: "ui with only spinner-color: false → the style stays at its default",
			file: fileConfig{UI: &uiConfig{SpinnerColor: boolptr(false)}},
			want: func(o *Options) { o.UI.SpinnerColor = false },
		},
		{
			// The scroll-bar switch is the third independent key: hiding the bar leaves both spinner
			// keys exactly where they were.
			name: "ui with only show-scrollbar: false → the spinner keys stay at their defaults",
			file: fileConfig{UI: &uiConfig{ShowScrollbar: boolptr(false)}},
			want: func(o *Options) { o.UI.ShowScrollbar = false },
		},
		{
			name: "a context-files block replaces the default name list whole",
			file: fileConfig{ContextFiles: &contextFilesConfig{Names: []string{"CONVENTIONS.md", "AGENTS.md"}}},
			want: func(o *Options) { o.ContextFiles = []string{"CONVENTIONS.md", "AGENTS.md"} },
		},
		{
			// Both spellings of "off" collapse to the one value downstream: no names to look for.
			name: "a context-files block switched off resolves to no names",
			file: fileConfig{ContextFiles: &contextFilesConfig{Enable: boolptr(false)}},
			want: func(o *Options) { o.ContextFiles = nil },
		},
		{
			name: "cursor-shape and editor are file-only (default empty)",
			file: fileConfig{CursorShape: "bar", Editor: "hx"},
			want: func(o *Options) { o.CursorShape = "bar"; o.Editor = "hx" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := wantDefaults()
			if tt.want != nil {
				tt.want(&want)
			}
			got, notices := resolveSources(t, tt.file, tt.env, tt.flags, tt.changed)
			if diffs := structDiff(got, want); len(diffs) != 0 {
				t.Errorf("resolution disagrees with the expected options:\n%s", strings.Join(diffs, "\n"))
			}
			if len(notices) != 0 {
				t.Errorf("resolution notices = %q; want none for a well-formed config", notices)
			}
		})
	}
}

// wantDefaults is the Options a config that states nothing resolves to: every key of the schema at
// its built-in default. Spelled out rather than taken from the resolution under test, like
// wantUIDefault it embeds, so a change to a shipped default shows up as a failure here instead of
// silently agreeing with itself. Every other field is the zero value — no key of the schema
// defaults to anything else, and the fields config does not own are not resolution's to fill.
func wantDefaults() Options {
	return Options{
		Mode: "ask-before", ConfineToWorkspace: true, UseProjectSkills: true, AutoCompact: true,
		DelegateMaxSteps: defaultDelegateMaxSteps,
		AutoTitle:        true, ValidatedSetsEnable: true, ContextFiles: []string{"AGENTS.md"},
		Present: PresentSettings{AutoOpen: true}, UI: wantUIDefault,
	}
}

// resolveSources drives the three passes in the order ResolveOptions drives them, off injected
// sources: a parsed file, a getenv over a map, and a flag set whose changed names are listed. It
// stops where ResolveOptions' validation begins, so a case can state a value the validators would
// refuse and still assert what resolution made of it.
func resolveSources(t *testing.T, fc fileConfig, env map[string]string, flags Options, changed []string) (Options, []string) {
	t.Helper()
	var o Options
	applyFile(&o, fc)
	if err := applyEnv(&o, func(name string) string { return env[name] }); err != nil {
		t.Fatalf("applyEnv(%v): %v", env, err)
	}
	applyFlags(&o, flags, func(name string) bool { return slices.Contains(changed, name) })
	confine, notices := resolveConfineToWorkspace(o.ConfineToWorkspace, o.UnconfinedHosts, testHostID)
	o.ConfineToWorkspace = confine
	return o, notices
}

// The three keys that resolve from more than one source, driven end to end through their registry
// rows: a real fileConfig, a real getenv, a real explicitly-set flag, and the precedence the
// three produce. The WIRE-FACING names — the variable a user exports, the flag they type — are
// spelled out here as literals rather than read from the row, which is what makes this a test of
// the rows and not of itself: resolution asks the registry which variable and which flag carry
// each key, so editing a row's EnvVar or FlagName to anything else means the value this test sets
// is never seen and the precedence assertions fail.
//
// The closing check is the file-only fence for the WHOLE schema, which no case of a precedence
// table can make any more: exactly these three rows carry an environment or a flag accessor, so
// every other key of the schema can be read from the config file and from nowhere else. ADR 0012's
// two global-config-only keys are covered by it like the rest.
//
// The raw startup overrides (APOGEE_ENDPOINT, APOGEE_API_KEY, APOGEE_MODEL, --endpoint, --model)
// are deliberately absent: since ADR 0036 they name no config key at all, so they do not ride
// these sources and nothing here should claim they do.
func TestMultiSourceKeysReadTheRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path            string
		envVar          string // the variable name the row must carry; "" ⇒ the key has none
		flagName        string // the flag name the row must carry; "" ⇒ the key has none
		file, env, flag string // what each source supplies, as that source spells it
		setFile         func(*fileConfig, string)
		setFlag         func(*Options, string)
		resolved        func(Options) string
	}{
		{
			path: "server", envVar: "APOGEE_SERVER", flagName: "server",
			file: "the-file-box", env: "the-env-box", flag: "the-flag-box",
			setFile:  func(fc *fileConfig, v string) { fc.Server = v },
			setFlag:  func(o *Options, v string) { o.StartupServer = v },
			resolved: func(o Options) string { return o.StartupServer },
		},
		{
			path: "mode", envVar: "APOGEE_MODE", flagName: "mode",
			file: string(domain.ModePlan), env: string(domain.ModeAllowEdits), flag: string(domain.ModeAuto),
			setFile:  func(fc *fileConfig, v string) { fc.Mode = v },
			setFlag:  func(o *Options, v string) { o.Mode = v },
			resolved: func(o Options) string { return o.Mode },
		},
		{
			path: "bypass", envVar: "APOGEE_BYPASS", flagName: "bypass",
			file: "true", env: "false", flag: "true",
			setFile: func(fc *fileConfig, v string) { fc.Bypass = boolptr(v == "true") },
			setFlag: func(o *Options, v string) { o.Bypass = v == "true" },
			resolved: func(o Options) string {
				if o.Bypass {
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
			got, _ := resolveSources(t, fc, nil, Options{}, nil)
			if tt.resolved(got) != tt.file {
				t.Fatalf("file only: %s = %q, want %q", tt.path, tt.resolved(got), tt.file)
			}

			env := map[string]string{tt.envVar: tt.env}
			got, _ = resolveSources(t, fc, env, Options{}, nil)
			if tt.resolved(got) != tt.env {
				t.Errorf("%s over the file: %s = %q, want %q (does the row still name %s?)", tt.envVar,
					tt.path, tt.resolved(got), tt.env, tt.envVar)
			}

			var flags Options
			tt.setFlag(&flags, tt.flag)
			got, _ = resolveSources(t, fc, env, flags, []string{tt.flagName})
			if tt.resolved(got) != tt.flag {
				t.Errorf("--%s over %s: %s = %q, want %q (does the row still name --%s?)", tt.flagName,
					tt.envVar, tt.path, tt.resolved(got), tt.flag, tt.flagName)
			}
		})
	}

	multiSource := map[string]bool{}
	for _, tt := range tests {
		multiSource[tt.path] = true
	}
	for _, k := range keyAccessors {
		if (k.fromEnv != nil || k.fromFlag != nil) && !multiSource[k.row.Path] {
			t.Errorf("key %q reads an environment variable or a flag, but only the keys listed here do — "+
				"either it stopped being file-only or this table has fallen behind the schema", k.row.Path)
		}
	}
}

// Resolution takes its base mode from the registry row rather than a literal, so the row is what
// the "nothing stated → the defaults" expectation above now rests on. This pins that row to the
// autonomy ladder's own default constant: a row edited to another mode — or to nothing — would
// otherwise quietly change what a session with no config starts in.
func TestRegistryModeDefaultIsTheLadderDefault(t *testing.T) {
	t.Parallel()
	row, ok := LookupKey("mode")
	if !ok {
		t.Fatal("no registry row for mode — resolution takes the default mode from it")
	}
	if row.Default != string(domain.ModeAskBefore) {
		t.Errorf("registry mode default = %q, want %q (the mode a session with no config starts in)",
			row.Default, string(domain.ModeAskBefore))
	}
}

// The accessor table over the registry cannot half-describe a key: EVERY row needs the projection
// that reads its key out of the file and lands it on the Options, exactly once; every row
// advertising a variable or a flag must have the plumbing that reads that source; every accessor
// must name a described key; and no accessor may carry plumbing for a source its row does not name
// (which would be dead code advertising nothing). Without this, a key added to the schema and
// described in the registry could still be a key resolution never reads, and adding `EnvVar:` to a
// row would silently advertise a variable nothing looks at. Since the passes that carry a key ARE
// these accessors, a missing one is a nil call at startup rather than a value quietly left at its
// default — the guard is what turns that into a test failure instead.
func TestKeyAccessorsBindDescribedKeys(t *testing.T) {
	t.Parallel()

	bound := map[string]keyAccessor{}
	for _, k := range keyAccessors {
		if _, ok := LookupKey(k.row.Path); !ok {
			t.Errorf("keyAccessors binds %q, which the registry does not describe", k.row.Path)
		}
		if _, dup := bound[k.row.Path]; dup {
			t.Errorf("keyAccessors binds %q twice — one key, one entry", k.row.Path)
		}
		if k.fromFile == nil {
			t.Errorf("keyAccessors entry %q has no fromFile, so neither the config file nor the key's "+
				"own default would ever reach the Options", k.row.Path)
		}
		if k.fromEnv != nil && k.row.EnvVar == "" {
			t.Errorf("keyAccessors entry %q reads an environment variable its row does not name", k.row.Path)
		}
		if k.fromFlag != nil && k.row.FlagName == "" {
			t.Errorf("keyAccessors entry %q reads a flag its row does not name", k.row.Path)
		}
		bound[k.row.Path] = k
	}
	for _, row := range KeyRegistry {
		k, ok := bound[row.Path]
		if !ok {
			t.Errorf("registry row %q has no accessor — the key would be shown by /settings and never "+
				"read by resolution", row.Path)
			continue
		}
		if row.EnvVar != "" && k.fromEnv == nil {
			t.Errorf("registry row %q names %s but the accessor reads no environment value", row.Path, row.EnvVar)
		}
		if row.FlagName != "" && k.fromFlag == nil {
			t.Errorf("registry row %q names --%s but the accessor reads no flag value", row.Path, row.FlagName)
		}
	}
}

// Every key of the schema, carried end to end: a file that states all of them, resolved onto the
// Options the composition root builds everything from. The assertion is the SET of Options fields
// that moved off what an empty file resolves to — which is what catches the two failures the
// accessor table can have and the completeness guard above cannot see: a row whose accessor writes
// its value onto a NEIGHBOUR's field (the neighbour's name appears, its own does not), and a row
// whose value never reaches opts at all (its name is missing).
//
// It is the set rather than a hand-written expected Options because the values themselves are
// pinned per key by TestResolvePrecedence above and the TestApplyConfig* suites below; what has no
// other home is the claim that every key of the schema reaches construction, and that only the
// fields config owns are touched.
func TestEveryConfigKeyReachesTheOptions(t *testing.T) {
	t.Parallel()

	// The Options fields the config schema owns — every field some registry row writes, and no
	// other. The startup fields (Endpoint, Model, APIKey, the Startup* run) are deliberately absent:
	// since ADR 0036 they name no config key, and they are resolved from the selected servers entry
	// rather than from a source of the precedence ladder.
	want := map[string]bool{
		"Mode": true, "Bypass": true, "Servers": true, "StartupServer": true, "Editor": true,
		"ConfineToWorkspace": true, "UnconfinedHosts": true, "WebSearchEndpoint": true,
		"UseProjectSkills": true, "AutoCompact": true, "DelegateMaxSteps": true,
		"AutoTitle": true, "RememberModel": true,
		"ContextWindow": true, "ResponseReserve": true, "MCPServers": true, "ToolsDisabled": true,
		"URLAllowHosts": true, "URLDenyHosts": true, "ModelProfiles": true, "Mechanisms": true,
		"ValidatedSetsEnable": true, "ValidatedSetsAlias": true, "Present": true,
		"SystemPrompt": true, "ContextFiles": true, "UI": true, "CursorShape": true,
	}

	var got, unset Options
	applyFile(&got, everyKeyFileConfig())
	applyFile(&unset, fileConfig{})

	moved := map[string]bool{}
	for _, diff := range structDiff(got, unset) {
		moved[strings.SplitN(diff, " ", 2)[0]] = true
	}
	for name := range want {
		if !moved[name] {
			t.Errorf("Options.%s is unchanged by a file that states every key — the key that owns it "+
				"is read and then never reaches construction", name)
		}
	}
	for name := range moved {
		if !want[name] {
			t.Errorf("Options.%s was written by a config key, but no key owns it — an accessor is "+
				"writing its value onto a neighbour's field", name)
		}
	}
}

// everyKeyFileConfig is a file that sets EVERY key of the schema to a non-default value, so an
// accessor that reads or writes the wrong field cannot hide behind a key the case left absent. It
// is a function rather than a literal at its call site so the exhaustive file has one home as the
// suites that want it come and go.
func everyKeyFileConfig() fileConfig {
	return fileConfig{
		Mode: "auto", Bypass: boolptr(true), Server: "the-box", Editor: "hx",
		Servers:            []ServerEntry{{Name: "the-box", Endpoint: "http://localhost:9000"}},
		ConfineToWorkspace: boolptr(false),
		UnconfinedHosts:    []UnconfinedHost{{ID: "another-host", Acknowledged: "2026-08-20"}},
		WebSearch:          "https://search.example.com",
		UseProjectSkills:   boolptr(false), AutoCompact: boolptr(false), AutoTitle: boolptr(false),
		DelegateMaxSteps: intptr(12),
		RememberModel:    boolptr(true),
		ContextWindow:    64000, ResponseReserve: 0.3,
		MCPServers:    []mcpServerConfig{{Name: "docs", Command: "mcp-docs"}},
		Tools:         &toolsConfig{Disabled: []string{"web_search"}},
		URLSafety:     &urlSafetyConfig{AllowHosts: []string{"example.com"}, DenyHosts: []string{"evil.example"}},
		ModelProfiles: map[string]modelProfileConfig{"qwen": {ToolCallFormat: "xml"}},
		Mechanisms:    map[string]bool{"decompose": true},
		ValidatedSets: &validatedSetsConfig{Enable: boolptr(false),
			Alias: map[string]string{"local": "qwen-30b"}},
		Present: &presentConfig{AutoOpen: boolptr(false), Command: "open {path}", Port: 8080,
			Host: "box.local"},
		SystemPromptText: "be brief", SystemPromptFile: "prompt.md",
		SystemPromptModels: map[string]systemPromptEntryConfig{"qwen": {Text: "be terse"}},
		ContextFiles:       &contextFilesConfig{Enable: boolptr(false), Names: []string{"CLAUDE.md"}},
		CursorShape:        "bar",
		UI: &uiConfig{Spinner: "glitter", SpinnerColor: boolptr(false), ShowScrollbar: boolptr(false),
			ColorScheme: "nord", StallAfter: strptr("30s"), Inspector: boolptr(true)},
	}
}

// structDiff names the fields two carriers disagree on, one line each, dereferencing the pointers
// a Layer is made of: printing a whole Layer with %+v prints addresses, which says nothing about
// the value that differs, and printing a whole Settings buries the one field that moved in
// twenty-odd that did not.
func structDiff[T any](got, want T) []string {
	var diffs []string
	g, w := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := range g.NumField() {
		if reflect.DeepEqual(g.Field(i).Interface(), w.Field(i).Interface()) {
			continue
		}
		diffs = append(diffs, fmt.Sprintf("%s = %s; want %s", g.Type().Field(i).Name,
			renderFieldValue(g.Field(i)), renderFieldValue(w.Field(i))))
	}
	return diffs
}

// renderFieldValue prints one carrier field as the VALUE it stands for: an unset pointer as
// "unset", a set one as what it points at.
func renderFieldValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "unset"
		}
		return fmt.Sprintf("%+v", v.Elem().Interface())
	}
	return fmt.Sprintf("%+v", v.Interface())
}

// The Host acknowledgement ladder (ADR 0012, amendment 2026-07-21), pinned in the order the
// ADR fixes: a file that loosens the key globally wins; else a match on THIS host's id loosens
// here; else confinement stays on. confine is what the FILE resolves the key to, so an absent key
// and an explicit `confine-to-workspace: true` are the same input — neither loosens anything. A malformed entry degrades softly with a notice, and an entry naming
// another machine is simply not this host — neither is an error. Step 2 additionally requires
// an identity to match: on a host that can supply none, the id stands for every such machine,
// so honouring it would let one saved acknowledgement loosen all of them.
func TestResolveConfineToWorkspace(t *testing.T) {
	t.Parallel()
	const otherHost = "laptop-9f8e7d"
	tests := []struct {
		name        string
		confine     bool
		hosts       []UnconfinedHost
		hostID      string
		want        bool
		wantNotices int
	}{
		{name: "the file does not loosen, nothing acknowledged → the secure default", confine: true, hostID: testHostID, want: true},
		{name: "an explicit global false → unconfined everywhere", hostID: testHostID, want: false},
		{
			name:    "this host is acknowledged → unconfined here",
			confine: true,
			hosts:   []UnconfinedHost{{ID: otherHost}, {ID: testHostID, Acknowledged: "2026-07-21", Note: "disposable container"}},
			hostID:  testHostID,
			want:    false,
		},
		{
			name:    "only other machines are acknowledged → still confined here",
			confine: true,
			hosts:   []UnconfinedHost{{ID: otherHost}, {ID: "buildbox-000111"}},
			hostID:  testHostID,
			want:    true,
		},
		{
			name:    "a file that does not loosen never vetoes a match — the entry is the more specific claim",
			confine: true,
			hosts:   []UnconfinedHost{{ID: testHostID}},
			hostID:  testHostID,
			want:    false,
		},
		{
			name:        "a malformed entry is skipped with a notice, the well-formed one still matches",
			confine:     true,
			hosts:       []UnconfinedHost{{Note: "no id here"}, {ID: testHostID}},
			hostID:      testHostID,
			want:        false,
			wantNotices: 1,
		},
		{
			name:        "a blank id never matches a blank host id — it is malformed, not a wildcard",
			confine:     true,
			hosts:       []UnconfinedHost{{ID: "   "}},
			hostID:      "",
			want:        true,
			wantNotices: 1,
		},
		{
			name:        "an identity-less host is not acknowledged by an entry naming its shared id",
			confine:     true,
			hosts:       []UnconfinedHost{{ID: unidentifiedTestHostID, Acknowledged: "2026-07-21"}},
			hostID:      unidentifiedTestHostID,
			want:        true,
			wantNotices: 1,
		},
		{
			name:   "an explicit global false still loosens an identity-less host — step 1 is untouched",
			hosts:  []UnconfinedHost{{ID: unidentifiedTestHostID}},
			hostID: unidentifiedTestHostID,
			want:   false,
			// The entry is still reported: the match was refused, and saying so is what keeps
			// the notice honest about why the id cannot stand for one machine.
			wantNotices: 1,
		},
		{
			name:    "an identity-less host with a real machine's entry is simply not that machine",
			confine: true,
			hosts:   []UnconfinedHost{{ID: otherHost}},
			hostID:  unidentifiedTestHostID,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !platform.IsUnidentifiedHostID(unidentifiedTestHostID) {
				t.Fatalf("%q is no longer the identity-less host id; the cases below prove nothing",
					unidentifiedTestHostID)
			}
			got, notices := resolveConfineToWorkspace(tt.confine, tt.hosts, tt.hostID)
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

// The window a session STARTS bounded by is the selected entry's own `context-window:` — and the
// room it works INSIDE that window, its `working-window:` — flattened
// exactly as the launcher key above is and for the same reason: the pin belongs to the entry, so the
// composition root resolves it over the top-level key at the bind rather than a beat later. An entry
// that pins none carries a zero, which leaves that top-level key answering — and so does the
// ephemeral entry a raw `--endpoint`/`APOGEE_ENDPOINT` override builds, which is in no list at all.
func TestApplyConfigStartupContextWindowComesFromTheSelectedEntry(t *testing.T) {
	t.Parallel()
	const servers = "servers:\n" +
		"  - name: cloud\n    endpoint: https://openrouter.ai/api/v1\n    context-window: 65536\n" +
		"    working-window: 32768\n" +
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n"

	tests := []struct {
		name        string
		start       string
		endpoint    string
		want        int
		wantWorking int
	}{
		{name: "the selected entry pins its own window", start: "cloud", want: 65536, wantWorking: 32768},
		{name: "the selected entry pins none", start: "workstation"},
		{name: "an endpoint override pins nothing", start: "cloud", endpoint: "http://rented.example:8080/v1"},
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
			if opts.StartupContextWindow != tt.want {
				t.Errorf("startupContextWindow = %d; want %d — the pin travels from the SELECTED entry, unresolved",
					opts.StartupContextWindow, tt.want)
			}
			// And the room INSIDE it, flattened off the same entry and for the same reason: a session
			// that starts on a bounded entry must work in that room from its first Turn.
			if opts.StartupWorkingWindow != tt.wantWorking {
				t.Errorf("startupWorkingWindow = %d; want %d — the bound travels from the SELECTED entry, unresolved",
					opts.StartupWorkingWindow, tt.wantWorking)
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
		wantStart  domain.PreboundStart
	}{
		{
			name:       "an empty list has nothing to start on",
			configYAML: "mode: plan\n",
			wantParts:  []string{"no servers are configured", "config.yaml", "servers:", "server: my-box"},
			wantStart:  domain.PreboundStart{Reason: domain.PreboundNoServers},
		},
		{
			name:       "a list with no choice recorded",
			configYAML: list,
			wantParts:  []string{"no startup server is chosen", "laptop", "--server"},
			wantStart:  domain.PreboundStart{Reason: domain.PreboundFirstBoot},
		},
		{
			name:       "a choice no entry carries",
			configYAML: list + "server: the-old-name\n",
			wantParts:  []string{`names "the-old-name"`, "configured: laptop", "--server"},
			wantStart:  domain.PreboundStart{Reason: domain.PreboundStaleChoice, Name: "the-old-name"},
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

// And unnamed is not the same as unlabelled: the synthesized alias also names the switch row this
// run is offered under, so a host that happens to spell a configured entry's name is suffixed
// rather than left to collide with it. A host that collides with nothing keeps its bare form.
func TestApplyConfigEphemeralAliasAvoidsConfiguredName(t *testing.T) {
	t.Parallel()
	const servers = "servers:\n  - name: workstation\n    endpoint: http://workstation:1111\nserver: workstation\n"
	tests := []struct {
		name      string
		endpoint  string
		wantAlias string
	}{
		{
			name:      "host equal to a configured name is suffixed",
			endpoint:  "http://workstation:8080/v1",
			wantAlias: "workstation (endpoint)",
		},
		{
			name:      "host that collides with nothing stays bare",
			endpoint:  "http://rented.example:8080/v1",
			wantAlias: "rented.example",
		},
		{
			name:      "a case-only near-match is not a collision",
			endpoint:  "http://Workstation:8080/v1",
			wantAlias: "Workstation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{ConfigDir: testConfigHome(t, servers)}
			getenv := func(name string) string {
				if name == EnvEndpoint {
					return tt.endpoint
				}
				return ""
			}
			if err := ApplyConfig(&opts, func(string) bool { return false }, getenv, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.HostAlias != tt.wantAlias {
				t.Errorf("hostAlias = %q; want %q", opts.HostAlias, tt.wantAlias)
			}
		})
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

// The override table cannot half-describe a source, on TestKeyAccessorsBindDescribedKeys'
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

// The remember-model config block parses into opts.rememberModel: a file-only key like auto-title,
// with the BUILT-IN default the other way round — absent ⇒ OFF, so a config that says nothing has
// apogee write nothing back into it. The seeded template is in the table for the same reason it is
// in auto-title's, but it now answers differently: the template ships `remember-model: true` as an
// active line, so a first run comes back ON, and that case is what pins the shipped line reaching
// opts rather than being read past.
func TestApplyConfigRememberModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fileYAML string
		want     bool
	}{
		{name: "absent key ⇒ off", want: false},
		{name: "an explicit true opts in", fileYAML: "remember-model: true\n", want: true},
		{name: "an explicit false is the default, said out loud", fileYAML: "remember-model: false\n", want: false},
		{name: "the seeded template ships it on", fileYAML: string(defaultConfigYAML), want: true},
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
			if opts.RememberModel != tt.want {
				t.Errorf("opts.rememberModel = %v; want %v", opts.RememberModel, tt.want)
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

// The delegate-max-steps key parses into opts.delegateMaxSteps: a file-only key (no flag/env),
// like the context-window pin above it. Three states, because a pointer on disk is what makes the
// third one reachable — a stated bound, an absent key resolving to the built-in default, and an
// explicit 0 that stays 0 because "unbounded" is a value here rather than the absence of one. The
// downstream opts → Config.Delegation.MaxSteps threading is the composition root's, pinned by
// TestBootConfigCarriesTheDelegateStepCap in wire_test.go.
func TestApplyConfigDelegateMaxSteps(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want int
	}{
		{"a stated bound", "delegate-max-steps: 12\n", 12},
		{"an absent key takes the built-in default", "", defaultDelegateMaxSteps},
		{"an explicit 0 is unbounded, not absent", "delegate-max-steps: 0\n", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.file)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.DelegateMaxSteps != tt.want {
				t.Errorf("opts.delegateMaxSteps = %d; want %d", opts.DelegateMaxSteps, tt.want)
			}
		})
	}
}

// The response-reserve config block parses into opts.responseReserve (item 12): a file-only key
// (no flag/env), like the context-window pin it stands beside. What this pins is that an ACCEPTED
// share reaches the composition root unchanged and that an absent key leaves 0 there — the state
// the engine reads as "hold my own built-in fifth back". The range refusals are the loader's own
// business and live in TestLoadFileConfigRefusesAResponseReserveThatIsNotAShare; the downstream
// opts → ContextConfig.ResponseReserveFraction threading is the composition root's.
func TestApplyConfigResponseReserve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fileYAML string
		want     float64
	}{
		{name: "an explicit share resolves to itself", fileYAML: "response-reserve: 0.35\n", want: 0.35},
		{name: "an absent key leaves 0 — the engine's own share stands", want: 0},
		{name: "an explicit 0 is the absent state spelled out", fileYAML: "response-reserve: 0\n", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, tt.fileYAML)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.ResponseReserve != tt.want {
				t.Errorf("opts.responseReserve = %v; want %v", opts.ResponseReserve, tt.want)
			}
		})
	}
}

// A `response-reserve:` that is not a share of the window is REFUSED at load, with the key named:
// a negative share has no meaning, 1 (or anything above it) holds the whole window back and leaves
// no prompt to send, and NaN is not a share at all — it compares false against both bounds, so the
// allocator's own defensive guard would wave it through and multiply the window by it
// (internal/context.Allocate). The refusal has to happen here, where the file and the number the
// user wrote can still be named.
func TestLoadFileConfigRefusesAResponseReserveThatIsNotAShare(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, fileYAML string }{
		{name: "a negative share", fileYAML: "response-reserve: -0.1\n"},
		{name: "the whole window", fileYAML: "response-reserve: 1.0\n"},
		{name: "more than the whole window", fileYAML: "response-reserve: 1.5\n"},
		{name: "not a number at all", fileYAML: "response-reserve: .nan\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.fileYAML), 0o600); err != nil {
				t.Fatalf("write config.yaml: %v", err)
			}
			_, err := LoadFileConfig(path, os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("LoadFileConfig accepted %q; want a refusal", strings.TrimSpace(tt.fileYAML))
			}
			if !strings.Contains(err.Error(), "response-reserve") {
				t.Errorf("the refusal does not name the key the user has to fix: %v", err)
			}
		})
	}
}

// The accepted half of the same gate: a share inside the open range loads, and resolves to the
// file's own number rather than to the 0 an absent key resolves to — the distinction the engine
// reads as "hold my own built-in share back".
func TestLoadFileConfigAcceptsAResponseReserveShare(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("response-reserve: 0.2\n"), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	file, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if file.ResponseReserve != 0.2 {
		t.Errorf("resolved response-reserve = %v; want the file's explicit 0.2", file.ResponseReserve)
	}
}

// The working-window config block parses into opts.workingWindow: a file-only key (no flag/env),
// like the context-window pin it stands beside, and spelled the same way — presence IS the positive
// value, so 0 and an absent key both leave the whole advertised window as the working room. The
// downstream opts → ContextConfig.WorkingWindow threading is the composition root's, pinned by
// TestBindServerResolvesTheWorkingWindow in wire_test.go.
func TestApplyConfigWorkingWindow(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want int
	}{
		{"a stated bound", "working-window: 200000\n", 200000},
		{"an absent key leaves the whole window", "", 0},
		{"an explicit 0 is the absent state spelled out", "working-window: 0\n", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.file)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.WorkingWindow != tt.want {
				t.Errorf("opts.workingWindow = %d; want %d", opts.WorkingWindow, tt.want)
			}
		})
	}
}

// The per-entry half of the working room: a `servers:` entry's own `working-window:` outranks the
// top-level one for the server the session is ON, and 0 at either scope is the absent state that
// falls through — to the top-level key first, then to the 0 that leaves the advertised window as the
// whole room. The ranks are ResolveContextWindow's, one key over, so the table is that one's.
//
// The negative rows are the defensive floor rather than a second validation: ValidateServers and the
// registry refuse those numbers where the file can still be named, so anything reaching here arrived
// from a caller that never saw the file — and falling through beats budgeting against it.
func TestResolveWorkingWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		entry, top int
		want       int
	}{
		{name: "the entry's own bound wins", entry: 200000, top: 65536, want: 200000},
		{name: "an entry bounding nothing falls through to the top-level key", top: 65536, want: 65536},
		{name: "neither bounds anything — the whole window is the room", want: 0},
		{name: "a negative entry bound falls through", entry: -1, top: 65536, want: 65536},
		{name: "a negative bound at both ranks falls through to 0", entry: -1, top: -1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveWorkingWindow(tt.entry, tt.top); got != tt.want {
				t.Errorf("ResolveWorkingWindow(%d, %d) = %d; want %d", tt.entry, tt.top, got, tt.want)
			}
		})
	}
}

// The per-entry half of the same key (item 13): a `servers:` entry's own `response-reserve:`
// outranks the top-level one for the server the session is ON, and 0 at either scope is the absent
// state that falls through — to the top-level key first, then to the 0 the engine reads as "hold my
// own built-in share back". The ranks are ResolveContextWindow's, one key over.
//
// The out-of-range rows are the defensive floor rather than a second validation: ValidateServers and
// LoadFileConfig refuse those numbers where the file can still be named, so anything reaching here
// arrived from a caller that never saw the file — and falling through beats multiplying a window by
// it.
func TestResolveResponseReserve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		entry, session float64
		want           float64
	}{
		{name: "the entry's own share wins", entry: 0.35, session: 0.2, want: 0.35},
		{name: "an entry stating none falls through to the top-level key", session: 0.2, want: 0.2},
		{name: "neither states one — the engine's own share stands", want: 0},
		{name: "an entry share of the whole window falls through", entry: 1, session: 0.2, want: 0.2},
		{name: "a negative entry share falls through", entry: -0.1, session: 0.2, want: 0.2},
		{name: "NaN falls through at both ranks", entry: math.NaN(), session: math.NaN(), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveResponseReserve(tt.entry, tt.session); got != tt.want {
				t.Errorf("ResolveResponseReserve(%v, %v) = %v; want %v", tt.entry, tt.session, got, tt.want)
			}
		})
	}
}

// A `response-reserve:` on a `servers:` entry is refused at startup by the range rule its top-level
// twin is refused by — one number, one meaning, wherever in the file it is written — and the refusal
// names the entry as well as the key, because a list of servers is where a typo hides longest. 0 is
// the absent state and passes, exactly as it does at the top level.
func TestValidateServersRefusesAnEntryResponseReserveThatIsNotAShare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reserve float64
		wantErr bool
	}{
		{name: "a share inside the open range", reserve: 0.35},
		{name: "the key absent", reserve: 0},
		{name: "a negative share", reserve: -0.1, wantErr: true},
		{name: "the whole window", reserve: 1, wantErr: true},
		{name: "more than the whole window", reserve: 1.5, wantErr: true},
		{name: "not a number at all", reserve: math.NaN(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateServers([]ServerEntry{{
				Name: "workstation", Endpoint: "http://127.0.0.1:1111", ResponseReserve: tt.reserve,
			}})
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateServers refused response-reserve: %v — %v", tt.reserve, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateServers accepted response-reserve: %v; want a refusal", tt.reserve)
			}
			if !strings.Contains(err.Error(), "response-reserve") || !strings.Contains(err.Error(), "workstation") {
				t.Errorf("the refusal names neither the key nor the entry the user has to fix: %v", err)
			}
		})
	}
}

// The share a session STARTS with is the selected entry's own, flattened exactly as its
// `context-window:` pin is and for that pin's reason: the number belongs to the entry, so the
// composition root resolves it over the top-level key at the bind rather than a beat later. An entry
// stating none carries a zero, which leaves that top-level key answering — and so does the ephemeral
// entry a raw `--endpoint`/`APOGEE_ENDPOINT` override builds, which is in no list at all.
func TestApplyConfigStartupResponseReserveComesFromTheSelectedEntry(t *testing.T) {
	t.Parallel()
	const servers = "servers:\n" +
		"  - name: cloud\n    endpoint: https://openrouter.ai/api/v1\n    response-reserve: 0.35\n" +
		"  - name: workstation\n    endpoint: http://192.168.1.9:1111\n"

	tests := []struct {
		name     string
		start    string
		endpoint string
		want     float64
	}{
		{name: "the selected entry states its own share", start: "cloud", want: 0.35},
		{name: "the selected entry states none", start: "workstation"},
		{name: "an endpoint override states nothing", start: "cloud", endpoint: "http://rented.example:8080/v1"},
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
			if opts.StartupResponseReserve != tt.want {
				t.Errorf("startupResponseReserve = %v; want %v — the share travels from the SELECTED "+
					"entry, unresolved", opts.StartupResponseReserve, tt.want)
			}
		})
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

// The `tools:` block's OTHER half round-trips the same way (ADR 0057 decision 3): `enabled:` parses
// into opts.ToolsEnabled in file order, each half stands on its own so a block naming one leaves the
// other empty, and an absent block adds nothing back — which is the whole menu the build offers.
func TestApplyConfigToolsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("the listed names resolve in file order", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "tools:\n  enabled: [web_search, grep]\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if want := []string{"web_search", "grep"}; !reflect.DeepEqual(opts.ToolsEnabled, want) {
			t.Errorf("toolsEnabled = %v; want %v", opts.ToolsEnabled, want)
		}
		if len(opts.ToolsDisabled) != 0 {
			t.Errorf("toolsDisabled = %v; a block naming one half must leave the other empty", opts.ToolsDisabled)
		}
	})

	t.Run("both halves of one block resolve independently", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "tools:\n  disabled: [view_diff]\n  enabled: [web_search]\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if want := []string{"view_diff"}; !reflect.DeepEqual(opts.ToolsDisabled, want) {
			t.Errorf("toolsDisabled = %v; want %v", opts.ToolsDisabled, want)
		}
		if want := []string{"web_search"}; !reflect.DeepEqual(opts.ToolsEnabled, want) {
			t.Errorf("toolsEnabled = %v; want %v — the two halves must not alias one another",
				opts.ToolsEnabled, want)
		}
	})

	t.Run("an absent block adds nothing back", func(t *testing.T) {
		t.Parallel()
		home := testConfigHome(t, "mode: plan\n")
		opts := Options{ConfigDir: home}
		if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
			os.ReadFile, noNotify); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		if len(opts.ToolsEnabled) != 0 {
			t.Errorf("toolsEnabled = %v; want nothing added back", opts.ToolsEnabled)
		}
	})
}

// A `model-profiles:` entry's `tools:` axis round-trips onto the profile it describes (ADR 0057
// decision 1): both lists reach domain.ModelProfile.Tools in file order, an entry that spells no
// axis carries the zero delta, and the axis rides the same entry as the wire-shape axes rather than
// a map of its own.
func TestApplyConfigProfileToolsAxis(t *testing.T) {
	t.Parallel()

	home := testConfigHome(t, `model-profiles:
  big-model:
    thinking:
      style: harmony
    tools:
      disabled: [view_diff]
      enabled: [web_search, grep]
  small-model:
    thinking:
      style: none
`)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	byPattern := map[string]domain.ModelProfile{}
	for _, e := range opts.ModelProfiles {
		byPattern[e.Pattern] = e.Profile
	}
	big := byPattern["big-model"].Tools
	want := domain.ToolRosterDelta{Disabled: []string{"view_diff"}, Enabled: []string{"web_search", "grep"}}
	if !reflect.DeepEqual(big, want) {
		t.Errorf("big-model tools axis = %+v; want %+v", big, want)
	}
	// The other axes of the same entry are untouched by the new one.
	if got := byPattern["big-model"].Thinking.Style; got != domain.ThinkingHarmony {
		t.Errorf("big-model thinking style = %q; want harmony — the roster axis must not disturb its neighbours", got)
	}
	// And an entry that spells no axis carries no deltas, which is what keeps it the anchor it was.
	if small := byPattern["small-model"].Tools; !reflect.DeepEqual(small, domain.ToolRosterDelta{}) {
		t.Errorf("small-model tools axis = %+v; want the zero delta", small)
	}
}

// Axis PRESENCE is a fact of the FILE and is kept there (ADR 0057 decision 5): an entry that spells
// `tools:` — empty lists included — is distinguishable from one that leaves the key out, which is
// exactly what axis-wise resolution needs and what the domain value, carrying only the deltas, can
// no longer say once both arrive as the zero delta.
func TestProfileEntryRecordsWhetherItSpellsTheToolsAxis(t *testing.T) {
	t.Parallel()

	home := testConfigHome(t, `model-profiles:
  spells-it:
    tools: {}
  leaves-it-out:
    thinking:
      style: none
`)
	fc, err := parseConfigFile(filepath.Join(home, "config.yaml"), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}
	if !fc.ModelProfiles["spells-it"].spellsToolsAxis() {
		t.Error("an entry writing `tools: {}` does not report the axis as spelled; an explicit empty " +
			"axis is how a deeper layer's roster is overridden")
	}
	if fc.ModelProfiles["leaves-it-out"].spellsToolsAxis() {
		t.Error("an entry with no `tools:` key reports the axis as spelled; absence is the inherit spelling")
	}
	// Both project to the same domain value, which is why presence cannot live there.
	spelled := fc.ModelProfiles["spells-it"].toModelProfile().Tools
	absent := fc.ModelProfiles["leaves-it-out"].toModelProfile().Tools
	if !reflect.DeepEqual(spelled, absent) {
		t.Errorf("the two entries project to %+v and %+v; both are the zero delta, and the difference "+
			"between them is a file fact only", spelled, absent)
	}
}

// The projection into the resolver carries the roster axis's PRESENCE beside the profile (ADR 0057
// decision 5), because that is the fact axis-wise resolution turns on: an entry that leaves `tools:`
// out defers the axis to the shipped tier, while one that writes it empty overrides that tier with
// no deltas at all — and the two project to the same domain value, so the profile alone cannot say
// which was written.
//
// The third spelling is the degenerate one YAML allows and a human writes by accident: a bare
// `tools:` key with nothing under it parses as NULL, which reaches the pointer as absent. It reads
// as "I said nothing about the roster", which is the safe half of the pair — the entry inherits
// rather than silently emptying a roster it never meant to touch.
func TestProfileEntriesCarryTheToolsAxisPresence(t *testing.T) {
	t.Parallel()

	home := testConfigHome(t, `model-profiles:
  empty-axis:
    tools: {}
  listed-axis:
    tools:
      disabled: [web_search]
  null-axis:
    tools:
  no-axis:
    thinking:
      style: none
`)
	fc, err := parseConfigFile(filepath.Join(home, "config.yaml"), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}

	spelled := map[string]bool{}
	for _, entry := range toProfileEntries(fc.ModelProfiles) {
		spelled[entry.Pattern] = entry.SpellsTools
	}

	for pattern, want := range map[string]bool{
		"empty-axis":  true,
		"listed-axis": true,
		"null-axis":   false,
		"no-axis":     false,
	} {
		if spelled[pattern] != want {
			t.Errorf("entry %q reports the tools axis spelled = %v, want %v", pattern, spelled[pattern], want)
		}
	}
}

// Every roster list a config can spell is checked for names that are no tool, and every roster BLOCK
// for a name written under both halves — and each notice says WHICH key it is about, because the
// same list can now be written in four places and a complaint that does not name one sends the user
// hunting. None of them is ever a refusal: a roster being tuned must not be able to stop a session.
func TestApplyConfigRosterNoticesNameTheOffendingKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		yaml     string
		wantKey  string
		wantName string
	}{
		{
			name:     "an unknown name in the global disabled list",
			yaml:     "tools:\n  disabled: [grepp, grep]\n",
			wantKey:  "tools.disabled",
			wantName: "grepp",
		},
		{
			name:     "an unknown name in the global enabled list",
			yaml:     "tools:\n  enabled: [web_searchh, web_search]\n",
			wantKey:  "tools.enabled",
			wantName: "web_searchh",
		},
		{
			name:     "an unknown name in a profile's disabled list",
			yaml:     "model-profiles:\n  big-model:\n    tools:\n      disabled: [view_diffff]\n",
			wantKey:  "model-profiles.big-model.tools.disabled",
			wantName: "view_diffff",
		},
		{
			name:     "an unknown name in a profile's enabled list",
			yaml:     "model-profiles:\n  big-model:\n    tools:\n      enabled: [grepp]\n",
			wantKey:  "model-profiles.big-model.tools.enabled",
			wantName: "grepp",
		},
		{
			name:     "a name written under both halves of the global block",
			yaml:     "tools:\n  disabled: [grep]\n  enabled: [grep]\n",
			wantKey:  "tools lists",
			wantName: "grep",
		},
		{
			name:     "a name written under both halves of a profile's axis",
			yaml:     "model-profiles:\n  big-model:\n    tools:\n      disabled: [grep]\n      enabled: [grep]\n",
			wantKey:  "model-profiles.big-model.tools lists",
			wantName: "grep",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, tc.yaml)
			opts := Options{ConfigDir: home}
			var notices []string
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, func(n string) { notices = append(notices, n) }); err != nil {
				t.Fatalf("a roster complaint must not stop startup: %v", err)
			}
			var reported string
			for _, n := range notices {
				if strings.Contains(n, tc.wantKey) {
					reported = n
				}
			}
			switch {
			case reported == "":
				t.Fatalf("no notice named %q; got %v", tc.wantKey, notices)
			case !strings.Contains(reported, tc.wantName):
				t.Errorf("the notice must name the offending tool %q: %q", tc.wantName, reported)
			}
		})
	}
}

// The conflict notice says which way the clash is resolved, because a line that only reported the
// disagreement would leave the user guessing which tool they end up with. Disabled wins — the
// roster fails CLOSED (ADR 0057 decision 4) — and the wording has to say so.
func TestRosterConflictNoticeSaysDisabledWins(t *testing.T) {
	t.Parallel()

	notice := rosterConflictNotice("tools", domain.ToolRosterDelta{
		Disabled: []string{"grep", " view_diff "},
		Enabled:  []string{"view_diff", "grep"},
	})
	for _, want := range []string{"tools", "grep", "view_diff", "disabled wins"} {
		if !strings.Contains(notice, want) {
			t.Errorf("conflict notice %q does not carry %q", notice, want)
		}
	}
	// A block whose lists disagree about nothing says nothing at all.
	if quiet := rosterConflictNotice("tools", domain.ToolRosterDelta{
		Disabled: []string{"grep"}, Enabled: []string{"view_diff"},
	}); quiet != "" {
		t.Errorf("two lists naming different tools produced %q; want silence", quiet)
	}
}

// The `url-safety:` block round-trips to Options the way the roster above it does: both host lists
// parse in file order, each key stands on its own so a block naming one leaves the other empty, and
// an absent block configures no host layer at all — which is every host, subject to the SSRF floor
// no config key can reach. Entries travel VERBATIM through this package: normalising them to the
// form the transport dials belongs where the guard is built, not at the yaml seam.
func TestApplyConfigURLSafety(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		yaml      string
		wantAllow []string
		wantDeny  []string
	}{
		{
			name:      "both lists resolve in file order",
			yaml:      "url-safety:\n  allow-hosts: [docs.example.com, api.github.com]\n  deny-hosts: [ads.example.com]\n",
			wantAllow: []string{"docs.example.com", "api.github.com"},
			wantDeny:  []string{"ads.example.com"},
		},
		{
			name:     "a block naming only the deny list leaves the allow list empty",
			yaml:     "url-safety:\n  deny-hosts: [ads.example.com]\n",
			wantDeny: []string{"ads.example.com"},
		},
		{
			name:      "an entry travels as written, for the guard to normalise",
			yaml:      "url-safety:\n  allow-hosts: [Example.COM.]\n",
			wantAllow: []string{"Example.COM."},
		},
		{
			name: "an absent block configures no host layer",
			yaml: "mode: plan\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.yaml)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if len(opts.URLAllowHosts) != len(tt.wantAllow) || (tt.wantAllow != nil &&
				!reflect.DeepEqual(opts.URLAllowHosts, tt.wantAllow)) {
				t.Errorf("urlAllowHosts = %v; want %v", opts.URLAllowHosts, tt.wantAllow)
			}
			if len(opts.URLDenyHosts) != len(tt.wantDeny) || (tt.wantDeny != nil &&
				!reflect.DeepEqual(opts.URLDenyHosts, tt.wantDeny)) {
				t.Errorf("urlDenyHosts = %v; want %v", opts.URLDenyHosts, tt.wantDeny)
			}
		})
	}
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
			// The recorded launch profile rides the entry whose launcher loads it, and reaches the
			// root as written: whether that profile still exists is asked at use time, not here.
			name: "the optional launch-profile pointer travels per entry, as written",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    llama-launcher: auto
    launch-profile: gpt-oss-20b
  - name: laptop
    endpoint: http://127.0.0.1:1111
    llama-launcher: ~/elsewhere/launcher.yaml
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", LlamaLauncher: "auto",
					LaunchProfile: "gpt-oss-20b"},
				{Name: "laptop", Endpoint: "http://127.0.0.1:1111", LlamaLauncher: "~/elsewhere/launcher.yaml"},
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
		{
			// The Sub-agent server's four keys (ADR 0045): the routing flag, the posture that rides
			// it — an explicit `bypass: false` surviving as a value, not as an absent key — and the
			// window pin, which is legal on any entry because it describes the server.
			name: "the sub-agents flag, its posture keys, and the context-window pin travel per entry",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    context-window: 65536
  - name: grunt-box
    endpoint: http://192.168.64.1:2222
    model: qwen3-4b
    sub-agents: true
    bypass: false
    mechanisms:
      validate: true
      syntax: false
    context-window: 32768
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", ContextWindow: 65536},
				{
					Name: "grunt-box", Endpoint: "http://192.168.64.1:2222", Model: "qwen3-4b",
					SubAgents: true, Bypass: boolptr(false),
					Mechanisms:    map[string]bool{"validate": true, "syntax": false},
					ContextWindow: 32768,
				},
			},
		},
		{
			// An entry names ONE key source, and all three spellings travel to the composition root
			// exactly as written: resolving one is a first-use question there, never resolution's.
			// The plaintext marker rides the literal key it silences the migration offer for.
			name: "each key source travels per entry, the plaintext marker beside the literal",
			configYAML: `servers:
  - name: rented-box
    endpoint: https://llm.example.com
    api-key: sk-rented-token
    plaintext-key-ok: true
  - name: keychain-box
    endpoint: https://llm.example.com
    api-key-cmd: security find-generic-password -s apogee -a keychain-box -w
  - name: openrouter
    endpoint: https://openrouter.ai/api/v1
    api-key-env: OPENROUTER_API_KEY
  - name: laptop
    endpoint: http://127.0.0.1:1111
server: laptop
`,
			want: []ServerEntry{
				{
					Name: "rented-box", Endpoint: "https://llm.example.com",
					APIKey: "sk-rented-token", PlaintextKeyOK: true,
				},
				{
					Name: "keychain-box", Endpoint: "https://llm.example.com",
					APIKeyCmd: "security find-generic-password -s apogee -a keychain-box -w",
				},
				{Name: "openrouter", Endpoint: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY"},
				{Name: "laptop", Endpoint: "http://127.0.0.1:1111"},
			},
		},
		{
			// The reply ceiling is the window pin's idiom verbatim (ADR 0046): a positive value
			// travels as written, and both spellings of unset — the absent key and an explicit 0 —
			// resolve to the same zero, which is what tells the engine to derive the cap instead.
			name: "the optional max-output-tokens cap travels per entry, absent and 0 alike",
			configYAML: `servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    max-output-tokens: 8192
  - name: explicit-zero
    endpoint: http://192.168.64.1:2222
    max-output-tokens: 0
  - name: openrouter
    endpoint: https://openrouter.ai/api/v1
server: workstation
`,
			want: []ServerEntry{
				{Name: "workstation", Endpoint: "http://192.168.64.1:1111", MaxOutputTokens: 8192},
				{Name: "explicit-zero", Endpoint: "http://192.168.64.1:2222"},
				{Name: "openrouter", Endpoint: "https://openrouter.ai/api/v1"},
			},
		},
		{
			// The forced thinking-effort dialect rides the entry it describes and reaches the root
			// as written (ADR 0060 decision 3): mapping the word onto a wire is the provider
			// package's job, and `auto` — like the absent key — travels as the word the user wrote.
			name: "the optional effort-dialect override travels per entry, as written",
			configYAML: `servers:
  - name: openai
    endpoint: https://api.openai.com/v1
    effort-dialect: openai
  - name: vllm
    endpoint: http://192.168.64.1:8000
    effort-dialect: kwargs
  - name: fussy
    endpoint: https://llm.example.com
    effort-dialect: off
  - name: detected
    endpoint: http://192.168.64.1:1111
    effort-dialect: auto
  - name: absent
    endpoint: http://192.168.64.1:2222
server: openai
`,
			want: []ServerEntry{
				{Name: "openai", Endpoint: "https://api.openai.com/v1", EffortDialect: "openai"},
				{Name: "vllm", Endpoint: "http://192.168.64.1:8000", EffortDialect: "kwargs"},
				{Name: "fussy", Endpoint: "https://llm.example.com", EffortDialect: "off"},
				{Name: "detected", Endpoint: "http://192.168.64.1:1111", EffortDialect: "auto"},
				{Name: "absent", Endpoint: "http://192.168.64.1:2222"},
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
			// One key comes from ONE place, so a second source is the duplicate-name defect wearing
			// another key — and the refusal names every source the entry set, because choosing
			// between them is the fix.
			name: "an entry setting both a literal key and a command",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key: sk-token\n    api-key-cmd: pass show apogee/box\n",
			wantErr: []string{"servers: entry 1", "box", "sets api-key: and api-key-cmd:", "ONE source"},
		},
		{
			name: "an entry setting both a literal key and a variable name",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key: sk-token\n    api-key-env: BOX_API_KEY\n",
			wantErr: []string{"servers: entry 1", "box", "sets api-key: and api-key-env:", "ONE source"},
		},
		{
			name: "an entry setting both a command and a variable name",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key-cmd: pass show apogee/box\n    api-key-env: BOX_API_KEY\n",
			wantErr: []string{"servers: entry 1", "box", "sets api-key-cmd: and api-key-env:", "ONE source"},
		},
		{
			name: "an entry setting all three key sources",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key: sk-token\n    api-key-cmd: pass show apogee/box\n    api-key-env: BOX_API_KEY\n",
			wantErr: []string{
				"servers: entry 1", "box", "sets api-key:, api-key-cmd: and api-key-env:", "ONE source",
			},
		},
		{
			// The `llama-launcher:` reasoning: a value that is only whitespace reads as configured
			// while naming nothing, and leaving the key out is already how "no key" is spelled.
			name: "an entry whose api-key-cmd is only whitespace",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key-cmd: \"   \"\n",
			wantErr: []string{"servers: entry 1", "box", "api-key-cmd: is only whitespace", "remove the key"},
		},
		{
			name: "an entry whose api-key-env is only whitespace",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key-env: \" \"\n",
			wantErr: []string{"servers: entry 1", "box", "api-key-env: is only whitespace", "remove the key"},
		},
		{
			// The marker only silences the offer to migrate a PLAINTEXT key, so on an entry whose
			// key comes from anywhere else it is configured while doing nothing.
			name: "the plaintext marker on an entry whose key comes from a command",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    api-key-cmd: pass show apogee/box\n    plaintext-key-ok: true\n",
			wantErr: []string{"servers: entry 1", "box", "plaintext-key-ok: true without an api-key:", "remove it"},
		},
		{
			name: "the plaintext marker on a keyless entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"  - name: other\n    endpoint: http://two:1111\n    plaintext-key-ok: true\n",
			wantErr: []string{"servers: entry 2", "other", "plaintext-key-ok: true without an api-key:"},
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
			// The pointer names a profile the launcher loads, so on an entry apogee cannot launch
			// there is nothing to actuate it — the lone-key defect the `llama-launcher` checks share.
			name: "an entry pointing at a launch profile with no launcher to load it",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    launch-profile: gpt-oss-20b\n",
			wantErr: []string{"servers: entry 1", "box", "launch-profile: \"gpt-oss-20b\" without a llama-launcher:",
				"llama-launcher: auto"},
		},
		{
			name: "an entry whose launch-profile value is only whitespace",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    llama-launcher: auto\n    launch-profile: \"   \"\n",
			wantErr: []string{"servers: entry 1", "box", "launch-profile: is only whitespace", "remove the key"},
		},
		{
			name: "a launch-profile defect below a well-formed entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n    llama-launcher: auto\n" +
				"  - name: other\n    endpoint: http://two:1111\n    launch-profile: qwen3\n",
			wantErr: []string{"servers: entry 2", "other", "without a llama-launcher:"},
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
		{
			// The window pin is refused on the parallel-agents reasoning: absent and 0 already mean
			// observe, every N ≥ 1 is a pin, so a negative number has nothing left to mean.
			name: "an entry whose context-window pin is negative",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    context-window: -8192\n",
			wantErr: []string{"servers: entry 1", "box", "context-window: -8192 is negative", "1 or more"},
		},
		{
			// The working room is refused on the window pin's reasoning one key over: absent and 0
			// already mean "work in the whole window", every N ≥ 1 is a bound, so a negative one has
			// nothing left to mean.
			name: "an entry whose working-window is negative",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    working-window: -8192\n",
			wantErr: []string{"servers: entry 1", "box", "working-window: -8192 is negative", "1 or more"},
		},
		{
			// And the one refusal the room has that no other pin does: it is the space INSIDE the
			// window, so a bound above this entry's own pin is a ceiling above its roof. The refusal
			// names both numbers, because which of the two is wrong is the user's call.
			name: "an entry working in more room than its own context-window pins",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    context-window: 200000\n    working-window: 300000\n",
			wantErr: []string{"servers: entry 1", "box", "working-window: 300000 is larger",
				"context-window: 200000"},
		},
		{
			// The reply ceiling is refused on that same reasoning (ADR 0046): absent and 0 mean
			// derive, every N ≥ 1 is a pin, so a negative cap has nothing left to mean.
			name: "an entry whose max-output-tokens cap is negative",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    max-output-tokens: -4096\n",
			wantErr: []string{"servers: entry 1", "box", "max-output-tokens: -4096 is negative", "1 or more"},
		},
		{
			// Delegations route to ONE server (ADR 0045 decision 1), so the second flag is the
			// duplicate-name defect in another key — and the refusal names BOTH entries, because
			// choosing between them is the fix.
			name: "two entries flagged sub-agents",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n    sub-agents: true\n" +
				"  - name: other\n    endpoint: http://two:1111\n    sub-agents: true\n",
			wantErr: []string{"servers: entry 2", "other", "sub-agents: true", "entry 1", "box", "ONE server"},
		},
		{
			name: "posture keys on an entry the sub-agents flag is absent from",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    bypass: true\n    mechanisms:\n      validate: true\n",
			wantErr: []string{
				"servers: entry 1", "box", "bypass: and mechanisms: without sub-agents: true",
				"ride the sub-agents: flag",
			},
		},
		{
			// The dialect key is an ENUM, so its defect is a word that names nothing rather than a
			// number nothing can spend (ADR 0060 decision 3) — and the refusal names the entry, the
			// key and the words that may stand there.
			name: "an entry whose effort-dialect names no dialect",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n" +
				"    effort-dialect: bogus\n",
			wantErr: []string{
				"servers: entry 1", "box", `effort-dialect: "bogus" is not a dialect`,
				"auto", "kwargs", "reasoning", "openai", "off",
			},
		},
		{
			// Either key alone is refused, and the message names only the one that is there.
			name: "a lone mechanisms map on an unflagged entry",
			configYAML: "servers:\n  - name: box\n    endpoint: http://one:1111\n    sub-agents: true\n" +
				"  - name: other\n    endpoint: http://two:1111\n    mechanisms:\n      syntax: true\n",
			wantErr: []string{"servers: entry 2", "other", "mechanisms: without sub-agents: true"},
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

// SubAgentServer is the lookup the composition root runs once it has a validated list: the flagged
// entry, or nothing. Nothing is not a defect — it is today's behaviour, no routing at all — which is
// why the second return value says "found" rather than the error a missing entry would deserve.
func TestSubAgentServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []ServerEntry
		want    string
	}{
		{
			name: "the flagged entry is returned whole, wherever it sits in the list",
			entries: []ServerEntry{
				{Name: "workstation", Endpoint: "http://one:1111"},
				{Name: "grunt-box", Endpoint: "http://two:1111", SubAgents: true},
				{Name: "rented-box", Endpoint: "https://three:1111"},
			},
			want: "grunt-box",
		},
		{
			name: "no flag anywhere is not found — delegations stay on the parent's server",
			entries: []ServerEntry{
				{Name: "workstation", Endpoint: "http://one:1111"},
				{Name: "rented-box", Endpoint: "https://three:1111"},
			},
		},
		{name: "an empty list is not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, found := SubAgentServer(tt.entries)
			if found != (tt.want != "") {
				t.Fatalf("SubAgentServer found = %v, want %v (entry %q)", found, tt.want != "", tt.want)
			}
			if got.Name != tt.want {
				t.Errorf("SubAgentServer = %q, want %q", got.Name, tt.want)
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
// the selected source alone — the file must read and the placeholders must be the known four.
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
			name:    "an unknown placeholder names the source key and the known four",
			sp:      SystemPromptSettings{Global: PromptSource{Text: "hi {{bogus}}"}},
			wantErr: []string{"system-prompt-text", "{{bogus}}", "{{workspace}}", "{{datetime}}", "{{mode}}", "{{scratch}}"},
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
// composition root can hand the renderer a style, a colour flag, a scroll-bar flag, a colour scheme
// and a quiet threshold it never has to parse.
func TestApplyConfigUI(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `ui:
  spinner: glitter
  spinner-color: false
  show-scrollbar: false
  color-scheme: light
  stall-after: 2m
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := UISettings{Spinner: domain.SpinnerGlitter, SpinnerColor: false, ShowScrollbar: false, ColorScheme: "light",
		StallAfter: 2 * time.Minute}
	if opts.UI != want {
		t.Errorf("opts.ui = %+v; want %+v", opts.UI, want)
	}
}

// The `inspector:` key, end to end: YAML → Options, in the three shapes a bool key has. It is the
// arming switch for the raw-protocol capture, and the arming happens ONCE at startup — so what the
// resolved Options say is exactly what the session will and will not capture, and a default that
// drifted to true would turn every session into a capturing one silently.
func TestApplyConfigUIInspector(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		yaml string
		want bool
	}{
		{name: "armed", yaml: "ui:\n  inspector: true\n", want: true},
		{name: "an explicit false is the shipped default said out loud", yaml: "ui:\n  inspector: false\n"},
		{name: "an absent key leaves the capture disarmed", yaml: "ui:\n  spinner: classic\n"},
		{name: "no ui block at all", yaml: "mode: plan\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.yaml)
			opts := Options{ConfigDir: home}
			if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
				os.ReadFile, noNotify); err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.UI.Inspector != tt.want {
				t.Errorf("opts.ui.inspector = %v; want %v", opts.UI.Inspector, tt.want)
			}
		})
	}
}

// Naming one ui key must not disturb the ones beside it — the property the pointer-typed schema
// buys — so an armed Inspector leaves the spinner, the bar, the scheme and the threshold at their
// defaults, and naming any of those leaves the Inspector disarmed.
func TestApplyConfigUIInspectorIsIndependentOfTheOtherUIKeys(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	writeConfigHome(t, home, "ui:\n  inspector: true\n")
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	want := defaultUISettings()
	want.Inspector = true
	if opts.UI != want {
		t.Errorf("opts.ui = %+v; want %+v", opts.UI, want)
	}
}

// The `stall-after:` key, in every shape it takes: the durations Go spells, the explicit `0` that
// turns the quiet qualifier off, and the two refusals — a length of time that is negative, and text
// that is no length of time at all. The zero and the absent key are the pair the pointer exists
// for: one disables the guard, the other keeps the shipped 90 seconds.
func TestApplyConfigStallAfter(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		yaml    string
		want    time.Duration
		wantErr string
	}{
		{name: "the shipped default, said out loud", yaml: "ui:\n  stall-after: 90s\n", want: 90 * time.Second},
		{name: "minutes", yaml: "ui:\n  stall-after: 2m\n", want: 2 * time.Minute},
		{name: "a compound duration", yaml: "ui:\n  stall-after: 1m30s\n", want: 90 * time.Second},
		{name: "a bare 0 turns the guard off", yaml: "ui:\n  stall-after: 0\n", want: 0},
		{name: "no ui block at all keeps the default", yaml: "", want: 90 * time.Second},
		{name: "the block without the key keeps the default", yaml: "ui:\n  spinner: classic\n", want: 90 * time.Second},
		{
			name:    "a negative wait is refused",
			yaml:    "ui:\n  stall-after: -5s\n",
			wantErr: "invalid ui.stall-after -5s",
		},
		{
			name:    "text that is no duration is refused, quoted as written",
			yaml:    "ui:\n  stall-after: soonish\n",
			wantErr: `invalid ui.stall-after "soonish"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tt.yaml)
			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ApplyConfig accepted %q; want it refused", tt.yaml)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyConfig: %v", err)
			}
			if opts.UI.StallAfter != tt.want {
				t.Errorf("opts.ui.stallAfter = %s; want %s", opts.UI.StallAfter, tt.want)
			}
		})
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
			want: UISettings{Spinner: domain.SpinnerClassic, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark",
				StallAfter: 90 * time.Second},
		},
		{
			name: "only spinner-color: false → the style stays the default and the bar stays shown",
			yaml: "ui:\n  spinner-color: false\n",
			want: UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: false, ShowScrollbar: true, ColorScheme: "dark",
				StallAfter: 90 * time.Second},
		},
		{
			name: "only show-scrollbar: false → the bar goes, the spinner keys stay put",
			yaml: "ui:\n  show-scrollbar: false\n",
			want: UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: true, ShowScrollbar: false, ColorScheme: "dark",
				StallAfter: 90 * time.Second},
		},
		{
			// The explicit `true` and the absent key resolve alike — pinned so the pointer's
			// present-and-true branch is exercised, not just its nil one.
			name: "only show-scrollbar: true → the shipped default, said out loud",
			yaml: "ui:\n  show-scrollbar: true\n",
			want: UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark",
				StallAfter: 90 * time.Second},
		},
		{
			name: "only color-scheme: → the spinner keys and the bar stay put",
			yaml: "ui:\n  color-scheme: light\n",
			want: UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "light",
				StallAfter: 90 * time.Second},
		},
		{
			// And the newest key is independent in both directions: turning the stall guard off says
			// nothing about the look, and none of the four keys above moves it.
			name: "only stall-after: 0 → the look is untouched and only the guard goes",
			yaml: "ui:\n  stall-after: 0\n",
			want: UISettings{Spinner: domain.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark",
				StallAfter: 0},
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
// nothing. The vocabulary comes from internal/domain (CursorShapeNames), so this also pins that
// the message the user sees carries it.
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
			// The renderer is handed a NAME to draw: what survives resolution must be one the
			// vocabulary knows, or the empty request for the default.
			if opts.CursorShape != "" && !domain.ValidCursorShapeName(opts.CursorShape) {
				t.Errorf("the resolved shape %q is not a name the vocabulary carries", opts.CursorShape)
			}
		})
	}
}

// A ui.spinner naming a style this build has no animation for is a loud startup error that names
// the key AND lists the styles that would have worked — not a silent fall back, which would leave
// the user watching a spinner their config did not ask for with nothing pointing at the typo. The
// valid set comes from internal/domain (ParseSpinnerStyle), so this also pins that the message the
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
	for _, style := range []domain.SpinnerStyle{domain.SpinnerSnake, domain.SpinnerGlitter, domain.SpinnerClassic} {
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

// The `model-profiles:` map reaches opts.modelProfiles as entries the composition root matches a
// model name against (ADR 0044): each pattern keeps every axis of the block it was given — the
// projection carries the file's word across intact, and which axis WINS against the shipped table is
// the resolver's business — and the entries come back ordered by pattern, whatever order the map
// decoded in.
func TestApplyConfigModelProfiles(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `model-profiles:
  minimax-m3:
    thinking:
      style: delimited
      start: "<mm:think>"
      end: "</mm:think>"
  gemma:
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

	want := []profiles.Entry{
		{
			Pattern: "gemma",
			Profile: domain.ModelProfile{
				ToolCallFormat: domain.FormatMarkdownFenced,
				Thinking:       domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			},
		},
		{
			Pattern: "minimax-m3",
			Profile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<mm:think>", End: "</mm:think>"},
			},
		},
	}
	if !reflect.DeepEqual(opts.ModelProfiles, want) {
		t.Errorf("opts.modelProfiles = %+v; want %+v", opts.ModelProfiles, want)
	}
}

// The thinking block's `effort:` reaches the domain profile as the typed level (ADR 0050), across
// the whole widened vocabulary (ADR 0060) rather than just the original four, and an entry that
// leaves the key out keeps the ZERO effort — the anchor that means "emit nothing", so a config
// written before the key existed still produces byte-identical requests.
func TestApplyConfigModelProfileEffort(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `model-profiles:
  qwen3.8:
    thinking:
      style: delimited
      start: "<think>"
      end: "</think>"
      effort: low
  no-effort-here:
    thinking:
      style: harmony
  wide-effort:
    thinking:
      effort: xhigh
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := []profiles.Entry{
		{
			Pattern: "no-effort-here",
			Profile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony},
			},
		},
		{
			Pattern: "qwen3.8",
			Profile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{
					Style:  domain.ThinkingDelimited,
					Start:  "<think>",
					End:    "</think>",
					Effort: domain.EffortLow,
				},
			},
		},
		{
			Pattern: "wide-effort",
			Profile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Effort: domain.EffortXHigh},
			},
		},
	}
	if !reflect.DeepEqual(opts.ModelProfiles, want) {
		t.Errorf("opts.modelProfiles = %+v; want %+v", opts.ModelProfiles, want)
	}
}

// Every axis of a `model-profiles:` entry is checked at LOAD, and a bad value on any of them is a
// startup error rather than a silently-dropped setting — because each one fails invisibly further
// down: an unknown format or style fails the first Rebind naming no config key, a pattern that does
// not compile parses no call at all, and an unmapped effort reaches the wire as nothing (a model
// that ignores an effort dial and a model that was never sent one answer alike). So every message
// has to name the full key path it is about, and spell out the vocabulary that key takes.
func TestApplyConfigBadModelProfileAxisErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configYAML string
		wantIn     []string
	}{
		{
			name: "a tool-call-format outside the three the parse seam knows",
			configYAML: `model-profiles:
  qwen3.8:
    tool-call-format: markdown-fencd
`,
			wantIn: []string{
				"model-profiles.qwen3.8.tool-call-format", "markdown-fencd",
				"native", "markdown-fenced", "custom-regex",
			},
		},
		{
			name: "a custom-regex format with no pattern to parse with",
			configYAML: `model-profiles:
  qwen3.8:
    tool-call-format: custom-regex
`,
			wantIn: []string{"model-profiles.qwen3.8.tool-call-pattern", "custom-regex"},
		},
		{
			name: "a tool-call-pattern that does not compile",
			configYAML: `model-profiles:
  qwen3.8:
    tool-call-format: custom-regex
    tool-call-pattern: "(?<name>\\w+"
`,
			wantIn: []string{`model-profiles.qwen3.8.tool-call-pattern`, `(?<name>\w+`},
		},
		{
			name: "a tool-call-pattern under a format that never reads one",
			configYAML: `model-profiles:
  qwen3.8:
    tool-call-format: markdown-fenced
    tool-call-pattern: "(?<name>\\w+)"
`,
			wantIn: []string{
				"model-profiles.qwen3.8.tool-call-pattern",
				"model-profiles.qwen3.8.tool-call-format", "custom-regex",
			},
		},
		{
			name: "a thinking style outside the three strippers",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      style: delimted
`,
			wantIn: []string{
				"model-profiles.qwen3.8.thinking.style", "delimted",
				"none", "delimited", "harmony",
			},
		},
		{
			name: "an effort outside the widened vocabulary",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      effort: hihg
`,
			wantIn: []string{
				"model-profiles.qwen3.8.thinking.effort", "hihg",
				"off", "low", "medium", "high", "minimal", "xhigh", "max", "none",
			},
		},
		{
			name: "a delimited style with no token pair to strip with",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      style: delimited
`,
			wantIn: []string{
				"model-profiles.qwen3.8.thinking.style", "delimited",
				"model-profiles.qwen3.8.thinking.start",
				"model-profiles.qwen3.8.thinking.end",
			},
		},
		{
			name: "a delimited style holding only half its token pair",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      style: delimited
      start: "<think>"
`,
			wantIn: []string{
				"model-profiles.qwen3.8.thinking.style", "delimited",
				"model-profiles.qwen3.8.thinking.end",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tc.configYAML)

			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)

			if err == nil {
				t.Fatalf("%s — want a load error, got nil", tc.name)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q — it must name the offending key path and the vocabulary", err, want)
				}
			}
		})
	}
}

// A profile that sets all four axes to legitimate values loads, and reaches the composition root
// whole — the other half of the check above: validation refuses typos, never a config that means
// exactly what it says.
func TestApplyConfigFullyValidModelProfileLoads(t *testing.T) {
	t.Parallel()
	home := testConfigHome(t, "")
	const configYAML = `model-profiles:
  qwen3.8:
    tool-call-format: custom-regex
    tool-call-pattern: "(?<name>[a-z_]+)\\s+(?<args>.*)"
    thinking:
      style: delimited
      start: "<think>"
      end: "</think>"
      effort: medium
`
	writeConfigHome(t, home, configYAML)
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := []profiles.Entry{
		{
			Pattern: "qwen3.8",
			Profile: domain.ModelProfile{
				ToolCallFormat: domain.FormatCustomRegex,
				Pattern:        `(?<name>[a-z_]+)\s+(?<args>.*)`,
				Thinking: domain.ThinkingProfile{
					Style:  domain.ThinkingDelimited,
					Start:  "<think>",
					End:    "</think>",
					Effort: domain.EffortMedium,
				},
			},
		},
	}
	if !reflect.DeepEqual(opts.ModelProfiles, want) {
		t.Errorf("opts.modelProfiles = %+v; want %+v", opts.ModelProfiles, want)
	}
}

// The other three styles need no token pair, so none of them is dragged into the delimited check: a
// profile naming them — or naming no style at all — loads with `start:`/`end:` left out entirely.
func TestApplyConfigTokenlessThinkingStylesLoad(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configYAML string
		wantStyle  domain.ThinkingStyle
	}{
		{
			name: "the none style passes content through untouched",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      style: none
`,
			wantStyle: domain.ThinkingNone,
		},
		{
			name: "the harmony style reads gpt-oss channels",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      style: harmony
`,
			wantStyle: domain.ThinkingHarmony,
		},
		{
			name: "no style at all, just an effort",
			configYAML: `model-profiles:
  qwen3.8:
    thinking:
      effort: high
`,
			wantStyle: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := testConfigHome(t, "")
			writeConfigHome(t, home, tc.configYAML)

			opts := Options{ConfigDir: home}
			err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify)

			if err != nil {
				t.Fatalf("ApplyConfig: %v — a style that needs no tokens must load without them", err)
			}
			if len(opts.ModelProfiles) != 1 || opts.ModelProfiles[0].Profile.Thinking.Style != tc.wantStyle {
				t.Errorf("opts.ModelProfiles = %+v; want one qwen3.8 entry with style %q",
					opts.ModelProfiles, tc.wantStyle)
			}
		})
	}
}

// With no `model-profiles:` map, opts.modelProfiles is empty — the user configures no shape, so the
// composition root resolves against the shipped table alone and, failing that, the zero profile.
func TestApplyConfigNoProfilesIsEmpty(t *testing.T) {
	t.Parallel()
	opts := Options{ConfigDir: testConfigHome(t, "")} // nothing but a startup server
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if len(opts.ModelProfiles) != 0 {
		t.Errorf("opts.modelProfiles = %+v; want no entries", opts.ModelProfiles)
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

// An absent config file is not an error — a config file is optional — and resolves to exactly
// what an empty one does: every key of the schema at its built-in default.
func TestLoadFileConfigAbsentIsTheDefaults(t *testing.T) {
	t.Parallel()
	file, err := LoadFileConfig(filepath.Join(t.TempDir(), "config.yaml"), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("absent config: unexpected error %v", err)
	}
	if diffs := structDiff(file, wantDefaults()); len(diffs) != 0 {
		t.Errorf("an absent config does not resolve to the defaults:\n%s", strings.Join(diffs, "\n"))
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

// overrideSources must agree with the PASSES themselves about which source beat the config file:
// the marker it feeds tells the user their file's value is not the one in force, so a marker naming
// a source that did not win would be a lie in both directions. The cases mirror applyFlags' and
// applyEnv's own predicates, including the shape that is easy to get wrong — an empty variable is
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
