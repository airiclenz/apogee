package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// testHost builds the injected facts every case starts from: a real home with one workspace
// directory under it, one plain server and one the launcher fronts, and an Auto-eligible host.
func testHost(t *testing.T) (Host, string) {
	t.Helper()
	home := t.TempDir()
	workspace := filepath.Join(home, "repos", "apogee")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("make the workspace: %v", err)
	}
	host := Host{
		Home: home,
		LookupServer: func(name string) (ServerFacts, bool) {
			switch name {
			case "openrouter":
				return ServerFacts{}, true
			case "local":
				return ServerFacts{IsLauncherFronted: true}, true
			default:
				return ServerFacts{}, false
			}
		},
		Confinement: HostConfinement{
			Backend: "landlock",
			Caps:    domain.ConfinementCaps{FSWrite: true},
		},
	}
	return host, workspace
}

func TestLoadAppliesTheSchemaDefaults(t *testing.T) {
	t.Parallel()
	host, workspace := testHost(t)
	data := strings.ReplaceAll(`
shutdown-grace: 5m
schedules:
  - name: nightly-audit
    on:
      cycle: 24h
    run:
      prompt: "/code-audit internal/tui"
      workspace: ~/repos/apogee
      mode: auto
      server: openrouter
      model: qwen/qwen3-72b
  - name: morning-sweep
    on:
      cycle: 30s
    run:
      prompt: sweep the log
      workspace: WORKSPACE
`, "WORKSPACE", workspace)

	file, err := Load([]byte(data), host)
	if err != nil {
		t.Fatalf("a valid file must load: %v", err)
	}
	if file.ShutdownGrace != 5*time.Minute {
		t.Errorf("shutdown-grace = %s, want 5m", file.ShutdownGrace)
	}
	want := []Entry{
		{
			Name: "nightly-audit",
			On:   Trigger{Cycle: 24 * time.Hour},
			Run: Action{
				Prompt:    "/code-audit internal/tui",
				Workspace: workspace,
				Mode:      domain.ModeAuto,
				Server:    "openrouter",
				Model:     "qwen/qwen3-72b",
			},
		},
		{
			Name: "morning-sweep",
			On:   Trigger{Cycle: 30 * time.Second},
			Run:  Action{Prompt: "sweep the log", Workspace: workspace, Mode: domain.ModePlan},
		},
	}
	if len(file.Schedules) != len(want) {
		t.Fatalf("parsed %d schedules, want %d", len(file.Schedules), len(want))
	}
	for i, entry := range file.Schedules {
		if entry != want[i] {
			t.Errorf("entry %d =\n%#v\nwant\n%#v", i+1, entry, want[i])
		}
	}
}

func TestLoadTakesTheDefaultShutdownGrace(t *testing.T) {
	t.Parallel()
	host, workspace := testHost(t)
	data := "schedules:\n  - name: a\n    on:\n      cycle: 1h\n    run:\n      prompt: go\n      workspace: " + workspace + "\n"

	file, err := Load([]byte(data), host)
	if err != nil {
		t.Fatalf("a valid file must load: %v", err)
	}
	if file.ShutdownGrace != DefaultShutdownGrace {
		t.Errorf("shutdown-grace = %s, want the default %s", file.ShutdownGrace, DefaultShutdownGrace)
	}
}

func TestLoadAcceptsAnEmptySet(t *testing.T) {
	t.Parallel()
	host, _ := testHost(t)
	for name, data := range map[string]string{
		"no bytes at all":       "",
		"nothing but a comment": "# every schedule is commented out\n",
		"an empty list":         "schedules: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file, err := Load([]byte(data), host)
			if err != nil {
				t.Fatalf("an empty set is legal: %v", err)
			}
			if len(file.Schedules) != 0 {
				t.Errorf("parsed %d schedules, want none", len(file.Schedules))
			}
			if file.ShutdownGrace != DefaultShutdownGrace {
				t.Errorf("shutdown-grace = %s, want the default %s", file.ShutdownGrace, DefaultShutdownGrace)
			}
		})
	}
}

func TestLoadNamesEveryDefect(t *testing.T) {
	t.Parallel()
	host, workspace := testHost(t)
	missing := filepath.Join(workspace, "gone")
	notADirectory := filepath.Join(workspace, "a-file")
	if err := os.WriteFile(notADirectory, []byte("x"), 0o600); err != nil {
		t.Fatalf("write the file standing in for a workspace: %v", err)
	}
	entry := func(body string) string {
		return "schedules:\n  - name: nightly\n    on:\n      cycle: 1h\n    run:\n      prompt: go\n" + body
	}
	workspaceLine := "      workspace: " + workspace + "\n"

	cases := []struct {
		name    string
		yaml    string
		mutate  func(*Host)
		wants   []string
		unwants []string
	}{
		{
			name:  "an entry with no name",
			yaml:  "schedules:\n  - on:\n      cycle: 1h\n    run:\n      prompt: go\n" + workspaceLine,
			wants: []string{"entry 1:", "has no name:"},
		},
		{
			name:  "two entries claim one name",
			yaml:  entry(workspaceLine) + "  - name: nightly\n    on:\n      cycle: 2h\n    run:\n      prompt: go\n" + workspaceLine,
			wants: []string{`entry 2 ("nightly")`, "already has that name"},
		},
		{
			name:  "an entry with no cycle",
			yaml:  "schedules:\n  - name: nightly\n    on: {}\n    run:\n      prompt: go\n" + workspaceLine,
			wants: []string{"has no on: cycle:"},
		},
		{
			name:  "a cycle under the floor",
			yaml:  "schedules:\n  - name: nightly\n    on:\n      cycle: 5s\n    run:\n      prompt: go\n" + workspaceLine,
			wants: []string{"cycle: 5s is under the 30s floor"},
		},
		{
			name:  "an entry with no prompt",
			yaml:  "schedules:\n  - name: nightly\n    on:\n      cycle: 1h\n    run:\n" + workspaceLine,
			wants: []string{"has no run: prompt:"},
		},
		{
			name:  "a whitespace-only prompt",
			yaml:  "schedules:\n  - name: nightly\n    on:\n      cycle: 1h\n    run:\n      prompt: \"   \"\n" + workspaceLine,
			wants: []string{"has no run: prompt:"},
		},
		{
			name:  "an entry with no workspace",
			yaml:  entry(""),
			wants: []string{"has no run: workspace:"},
		},
		{
			name:  "a workspace that is not there",
			yaml:  entry("      workspace: " + missing + "\n"),
			wants: []string{"does not exist", missing},
		},
		{
			name:  "a workspace that is a file",
			yaml:  entry("      workspace: " + notADirectory + "\n"),
			wants: []string{"is a file", notADirectory},
		},
		{
			name:   "a leading ~ with no home to expand against",
			yaml:   entry("      workspace: ~/repos/apogee\n"),
			mutate: func(h *Host) { h.Home = "" },
			wants:  []string{"no home directory"},
		},
		{
			name:  "a mode off the Firing ladder",
			yaml:  entry(workspaceLine + "      mode: ask-before\n"),
			wants: []string{`mode: "ask-before"`, "plan or auto only"},
		},
		{
			name:   "auto on a host that cannot confine",
			yaml:   entry(workspaceLine + "      mode: auto\n"),
			mutate: func(h *Host) { h.Confinement = HostConfinement{Backend: "none"} },
			wants:  []string{"mode: auto", "cannot confine a run to its workspace"},
		},
		{
			// The zero value fails CLOSED: a Driver that builds a Host and forgets the confinement
			// facts must be refused, never handed an unattended auto Firing (which is why
			// HostConfinement stores the waiver inverted, as Unconfined).
			name:   "auto on a host that states no confinement facts at all",
			yaml:   entry(workspaceLine + "      mode: auto\n"),
			mutate: func(h *Host) { *h = Host{} },
			wants:  []string{"mode: auto", "cannot confine a run to its workspace"},
		},
		{
			name: "auto on a host whose backend fences the filesystem is legal",
			yaml: entry(workspaceLine + "      mode: auto\n"),
		},
		{
			// `confine-to-workspace: false` is the user's own "I am the sandbox" — the same ladder
			// that lets a session launch in Auto, and a Firing is not held to a stricter bar
			// (ADR 0033 decision 3).
			name:   "auto where the user waived the fence is legal",
			yaml:   entry(workspaceLine + "      mode: auto\n"),
			mutate: func(h *Host) { h.Confinement = HostConfinement{Backend: "none", Unconfined: true} },
		},
		{
			name:  "a server no entry answers to",
			yaml:  entry(workspaceLine + "      server: nowhere\n"),
			wants: []string{`server: "nowhere"`, "no servers: entry in config.yaml"},
		},
		{
			name:  "a model on a launcher-fronted server",
			yaml:  entry(workspaceLine + "      server: local\n      model: qwen3-72b\n"),
			wants: []string{"llama-launcher fronts", "never actuates the launcher"},
		},
		{
			name:    "a model on a plain server is legal",
			yaml:    entry(workspaceLine + "      server: openrouter\n      model: qwen3-72b\n"),
			unwants: []string{"actuates"},
		},
		{
			name:  "a shutdown-grace of nothing",
			yaml:  "shutdown-grace: 0s\n" + entry(workspaceLine),
			wants: []string{"shutdown-grace: 0s", "has to be positive"},
		},
		{
			name:  "a negative shutdown-grace",
			yaml:  "shutdown-grace: -1m\n" + entry(workspaceLine),
			wants: []string{"shutdown-grace: -1m0s"},
		},
		{
			name:    "a misspelled key",
			yaml:    "schedules:\n  - name: nightly\n    on:\n      cycle: 1h\n    run:\n      propmt: go\n" + workspaceLine,
			wants:   []string{`unknown key "propmt"`, "line 6"},
			unwants: []string{"has no run: prompt:"},
		},
		{
			name:  "a second YAML document",
			yaml:  entry(workspaceLine) + "---\n" + entry(workspaceLine),
			wants: []string{"more than one YAML document"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			caseHost := host
			if testCase.mutate != nil {
				testCase.mutate(&caseHost)
			}
			_, err := Load([]byte(testCase.yaml), caseHost)
			if len(testCase.wants) == 0 {
				if err != nil {
					t.Fatalf("this file has no defect, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want a defect naming %v, got none", testCase.wants)
			}
			for _, want := range testCase.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("defect %q missing from:\n%v", want, err)
				}
			}
			for _, unwanted := range testCase.unwants {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("defect unexpectedly mentions %q:\n%v", unwanted, err)
				}
			}
		})
	}
}

func TestLoadReportsEveryDefectAtOnce(t *testing.T) {
	t.Parallel()
	host, _ := testHost(t)
	data := `
shutdown-grace: 0s
schedules:
  - name: ""
    on:
      cycle: 5s
    run:
      prompt: ""
      workspace: /nowhere/at/all
      server: nobody
`

	_, err := Load([]byte(data), host)
	if err == nil {
		t.Fatal("want defects, got none")
	}
	for _, want := range []string{
		"shutdown-grace: 0s",
		"has no name:",
		"is under the 30s floor",
		"has no run: prompt:",
		"does not exist",
		"no servers: entry in config.yaml",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("defect %q missing from:\n%v", want, err)
		}
	}
	if unwrapped, ok := err.(interface{ Unwrap() []error }); !ok || len(unwrapped.Unwrap()) != 6 {
		t.Errorf("want six joined defects, got: %v", err)
	}
}

func TestLoadRejectsTheWholeFileOnOneDefect(t *testing.T) {
	t.Parallel()
	host, workspace := testHost(t)
	data := "schedules:\n  - name: good\n    on:\n      cycle: 1h\n    run:\n      prompt: go\n      workspace: " + workspace +
		"\n  - name: bad\n    on:\n      cycle: 1h\n    run:\n      prompt: go\n      workspace: /nowhere\n"

	file, err := Load([]byte(data), host)
	if err == nil {
		t.Fatal("one defective entry rejects the whole file")
	}
	if !strings.Contains(err.Error(), `entry 2 ("bad")`) {
		t.Errorf("the defect must name the entry it belongs to:\n%v", err)
	}
	if len(file.Schedules) != 0 {
		t.Errorf("a rejected file yields no schedules, got %d", len(file.Schedules))
	}
}
