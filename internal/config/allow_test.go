package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/adoption"
)

// projectAllowYAML is a Project config stating one rule under each granting list.
const projectAllowYAML = "allow:\n  terminal: [go test]\n  mcp-servers: [docs]\n"

// adoptionStore is the adoption record a layered load beside globalPath reads for projectRoot.
func adoptionStore(t *testing.T, globalPath, projectRoot string) *adoption.Store {
	t.Helper()

	store, err := adoption.New(WorkspacesDir(filepath.Dir(globalPath)), projectRoot)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	return store
}

// The project rules projectAllowYAML states.
var (
	projectGoTest = AllowRule{Kind: AllowTerminal, Text: "go test", Layer: SourceProject}
	projectDocs   = AllowRule{Kind: AllowMCPServers, Text: "docs", Layer: SourceProject}
)

// A rule in the global file is the user's own and is live as written, tagged with its layer.
func TestAllowRulesInTheGlobalFileAreLive(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t,
		"allow:\n  terminal: [go test, make lint]\n  mcp-servers: [docs]\n", "")

	o, notices := loadLayered(t, globalPath, projectRoot)

	want := AllowRules{Rules: []AllowRule{
		{Kind: AllowTerminal, Text: "go test", Layer: SourceGlobal},
		{Kind: AllowTerminal, Text: "make lint", Layer: SourceGlobal},
		{Kind: AllowMCPServers, Text: "docs", Layer: SourceGlobal},
	}}
	if !reflect.DeepEqual(o.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", o.AllowRules, want)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none", notices)
	}
}

// A project rule with no recorded answer is proposed: reported, and granting nothing.
func TestAllowRulesFromTheProjectAreProposedUntilAdopted(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "allow:\n  terminal: [make lint]\n", projectAllowYAML)

	o, notices := loadLayered(t, globalPath, projectRoot)

	want := AllowRules{
		Rules:    []AllowRule{{Kind: AllowTerminal, Text: "make lint", Layer: SourceGlobal}},
		Proposed: []AllowRule{projectGoTest, projectDocs},
	}
	if !reflect.DeepEqual(o.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", o.AllowRules, want)
	}
	if o.ProjectKeys[allowPath] {
		t.Errorf("ProjectKeys = %v; a proposed rule contributes nothing to the run", o.ProjectKeys)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none — a proposal is reported on Proposed, not announced here", notices)
	}
}

// An adopted project rule joins the effective set after the global rules, tagged `project`; a
// rejected one is reported apart and grants nothing.
func TestAllowRulesFromTheProjectAreLiveOnceAdopted(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "allow:\n  terminal: [make lint]\n", projectAllowYAML)
	store := adoptionStore(t, globalPath, projectRoot)
	if err := store.Adopt(adoption.Entry{Kind: "terminal", Text: "go test"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := store.Reject(adoption.Entry{Kind: "mcp-servers", Text: "docs"}); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	o, _ := loadLayered(t, globalPath, projectRoot)

	want := AllowRules{
		Rules: []AllowRule{
			{Kind: AllowTerminal, Text: "make lint", Layer: SourceGlobal},
			projectGoTest,
		},
		Rejected: []AllowRule{projectDocs},
	}
	if !reflect.DeepEqual(o.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", o.AllowRules, want)
	}
	if !o.ProjectKeys[allowPath] || o.SourceOf(allowPath) != SourceProject {
		t.Errorf("ProjectKeys = %v; want `allow` recorded as the project's once a rule of it is live",
			o.ProjectKeys)
	}
}

// An adoption pins the rule's exact text: a project rule edited after it was adopted — even by
// whitespace alone — is proposed again, and the old text grants nothing.
func TestAllowRulesFromTheProjectAreProposedAgainOnceEdited(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", "allow:\n  terminal: [go test]\n")
	if err := adoptionStore(t, globalPath, projectRoot).Adopt(
		adoption.Entry{Kind: "terminal", Text: "go test"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	writeLayerFile(t, projectFilePath(projectRoot), "allow:\n  terminal: ['go  test']\n")

	o, _ := loadLayered(t, globalPath, projectRoot)

	want := AllowRules{Proposed: []AllowRule{{Kind: AllowTerminal, Text: "go  test", Layer: SourceProject}}}
	if !reflect.DeepEqual(o.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", o.AllowRules, want)
	}
}

// The same text under the other granting key is a different rule: adopting a terminal prefix does
// not adopt an MCP server of that name.
func TestAllowRulesAdoptionIsPinnedToTheKind(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", "allow:\n  mcp-servers: [go test]\n")
	if err := adoptionStore(t, globalPath, projectRoot).Adopt(
		adoption.Entry{Kind: "terminal", Text: "go test"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	o, _ := loadLayered(t, globalPath, projectRoot)

	if len(o.AllowRules.Rules) != 0 || len(o.AllowRules.Proposed) != 1 {
		t.Errorf("AllowRules = %+v; want the mcp-servers rule proposed, nothing live", o.AllowRules)
	}
}

// An adoption record that cannot be read leaves every project rule proposed — a rule that cannot be
// shown to be adopted is not — and says so once, naming the project file.
func TestAllowRulesStayProposedWhenTheRecordIsUnreadable(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", projectAllowYAML)
	store := adoptionStore(t, globalPath, projectRoot)
	if err := store.Adopt(adoption.Entry{Kind: "terminal", Text: "go test"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte("entries: [not a map\n"), 0o600); err != nil {
		t.Fatalf("corrupt the record: %v", err)
	}

	o, notices := loadLayered(t, globalPath, projectRoot)

	want := AllowRules{Proposed: []AllowRule{projectGoTest, projectDocs}}
	if !reflect.DeepEqual(o.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", o.AllowRules, want)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], projectFilePath(projectRoot)) ||
		!strings.Contains(notices[0], "stay proposed") {
		t.Errorf("notices = %q; want one naming the project file and the proposed rules", notices)
	}
}

// A blank rule would match every command, so the global file's refuses the start naming it, and a
// project file's costs the project layer — one notice, and the global file alone answers.
func TestAllowRulesRefuseABlankRule(t *testing.T) {
	t.Parallel()
	for name, block := range map[string]string{
		"empty terminal rule": "allow:\n  terminal: ['']\n",
		"blank mcp rule":      "allow:\n  mcp-servers: [docs, '  ']\n",
	} {
		t.Run(name+" in the global file", func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, block, "")

			_, err := LoadLayeredConfig(globalPath, projectRoot, os.ReadFile, func(string) {})

			if err == nil || !strings.Contains(err.Error(), "allow.") {
				t.Errorf("LoadLayeredConfig error = %v; want a refusal naming the allow list", err)
			}
		})
		t.Run(name+" in the project file", func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, "workflow-retries: 2\n", "workflow-retries: 5\n"+block)

			o, notices := loadLayered(t, globalPath, projectRoot)

			if o.WorkflowRetries != 2 || len(o.AllowRules.Proposed) != 0 {
				t.Errorf("workflow-retries = %d, AllowRules = %+v; want the global 2 and no project rules",
					o.WorkflowRetries, o.AllowRules)
			}
			if len(notices) != 1 || !strings.Contains(notices[0], "the project layer is skipped") {
				t.Errorf("notices = %q; want one skipping the project layer", notices)
			}
		})
	}
}

// A project `allow:` entry never enters the merged document the one decode reads — adopted or
// not, it reaches the run only through the adoption record — so the global file's block decodes
// exactly as written.
func TestAllowRulesFromTheProjectNeverReachTheMergedDocument(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		global string
		want   *allowConfig
	}{
		{"no global block", "workflow-retries: 2\n", nil},
		{"a global block", "allow:\n  terminal: [make lint]\n", &allowConfig{Terminal: []string{"make lint"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, tc.global, "workflow-retries: 5\n"+projectAllowYAML)
			if err := adoptionStore(t, globalPath, projectRoot).Adopt(
				adoption.Entry{Kind: "terminal", Text: "go test"}); err != nil {
				t.Fatalf("Adopt: %v", err)
			}

			fc, _, err := parseLayeredConfig(globalPath, projectRoot, os.ReadFile, func(string) {}, false)

			if err != nil {
				t.Fatalf("parseLayeredConfig: %v", err)
			}
			if !reflect.DeepEqual(fc.Allow, tc.want) {
				t.Errorf("merged allow block = %+v; want the global file's %+v alone", fc.Allow, tc.want)
			}
			if fc.WorkflowRetries == nil || *fc.WorkflowRetries != 5 {
				t.Errorf("workflow-retries = %v; want the project's 5 — the layer itself was merged",
					fc.WorkflowRetries)
			}
		})
	}
}

// Startup resolves the same effective set: the adoption record is read from the workspaces folder
// of the apogee home the run resolved.
func TestResolveOptionsResolvesTheEffectiveAllowRules(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "allow:\n  terminal: [make lint]\n", projectAllowYAML)
	if err := adoptionStore(t, globalPath, projectRoot).Adopt(
		adoption.Entry{Kind: "mcp-servers", Text: "docs"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot}

	_, err := ResolveOptions(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(string) {}, testHostID)

	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	want := AllowRules{
		Rules: []AllowRule{
			{Kind: AllowTerminal, Text: "make lint", Layer: SourceGlobal},
			projectDocs,
		},
		Proposed: []AllowRule{projectGoTest},
	}
	if !reflect.DeepEqual(opts.AllowRules, want) {
		t.Errorf("AllowRules = %+v; want %+v", opts.AllowRules, want)
	}
}

// The daemon takes no project layer, so a project's rules are not even proposed there.
func TestResolveOptionsReadsNoProjectAllowRulesWhenGlobalConfigOnly(t *testing.T) {
	t.Parallel()

	o, _ := resolveLayeredStartup(t, "", projectAllowYAML, Options{GlobalConfigOnly: true}, nil)

	if !reflect.DeepEqual(o.AllowRules, AllowRules{}) {
		t.Errorf("AllowRules = %+v; want none without a project layer", o.AllowRules)
	}
}

// The row's summary counts what grants and what waits on an answer, and is empty for no rules.
func TestAllowRulesSummary(t *testing.T) {
	t.Parallel()
	rule := AllowRule{Kind: AllowTerminal, Text: "go test", Layer: SourceGlobal}
	for _, tc := range []struct {
		name string
		set  AllowRules
		want string
	}{
		{"none", AllowRules{}, ""},
		{"live only", AllowRules{Rules: []AllowRule{rule, rule}}, "2 rules"},
		{"proposed only", AllowRules{Proposed: []AllowRule{rule}}, "1 proposed"},
		{"both", AllowRules{Rules: []AllowRule{rule}, Proposed: []AllowRule{rule}, Rejected: []AllowRule{rule}},
			"1 rule, 1 proposed"},
	} {
		if got := allowRulesSummary(tc.set); got != tc.want {
			t.Errorf("%s: allowRulesSummary = %q; want %q", tc.name, got, tc.want)
		}
	}
}

// WorkspacesDir is the adoption records' folder under the apogee home, and nothing for no home.
func TestWorkspacesDir(t *testing.T) {
	t.Parallel()

	if got, want := WorkspacesDir("/home/u/.apogee"), filepath.Join("/home/u/.apogee", "workspaces"); got != want {
		t.Errorf("WorkspacesDir = %q; want %q", got, want)
	}
	if got := WorkspacesDir(""); got != "" {
		t.Errorf("WorkspacesDir(\"\") = %q; want empty — no home, no records", got)
	}
}
