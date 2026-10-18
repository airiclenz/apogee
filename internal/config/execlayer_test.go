package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
)

// projectDocsServerYAML is a Project config proposing one stdio server and nothing else.
const projectDocsServerYAML = "mcp-servers:\n  - name: docs\n    transport: stdio\n    command: docs-server\n"

// adoptProposedServers loads the layered config of globalPath and projectRoot and records every
// MCP server entry it proposes as adopted, answering the proposals it adopted.
func adoptProposedServers(t *testing.T, globalPath, projectRoot string) []domain.Proposal {
	t.Helper()

	o, _ := loadLayered(t, globalPath, projectRoot)
	store := adoptionStore(t, globalPath, projectRoot)
	for _, p := range o.Proposals.Proposed {
		entry := adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}
		if err := store.Adopt(entry); err != nil {
			t.Fatalf("Adopt %s: %v", p.Handle, err)
		}
	}
	return o.Proposals.Proposed
}

// serverNames is the names of servers, in order, each marked `(project)` when the Project config
// contributed it.
func serverNames(servers []mcp.ServerConfig) []string {
	names := make([]string, len(servers))
	for i, s := range servers {
		names[i] = s.Name
		if s.Project {
			names[i] += " (project)"
		}
	}
	return names
}

// A Project config's server is proposed — with what its pane shows — and connects nothing until
// the user adopts it, even when it is the only key the file states.
func TestRepoMCPServerIsInertUntilAdopted(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", projectDocsServerYAML)

	o, notices := loadLayered(t, globalPath, projectRoot)

	if len(o.MCPServers) != 0 {
		t.Errorf("MCPServers = %v; want none — a proposed server connects nothing", serverNames(o.MCPServers))
	}
	if len(o.Proposals.Proposed) != 1 || len(o.Proposals.Adopted)+len(o.Proposals.Rejected) != 0 {
		t.Fatalf("Proposals = %+v; want the one server proposed", o.Proposals)
	}
	got := o.Proposals.Proposed[0]
	wantFields := []domain.ProposalField{
		{Label: "transport", Values: []string{"stdio"}},
		{Label: "command", Values: []string{"docs-server"}},
	}
	if got.Kind != domain.ProposalMCPServer || got.Handle != "docs" || !reflect.DeepEqual(got.Fields, wantFields) {
		t.Errorf("proposal = %+v; want MCP server entry docs with fields %+v", got, wantFields)
	}
	entry := adoption.Entry{Kind: string(got.Kind), Handle: got.Handle, Text: got.Text}
	if got.Fingerprint != adoption.Fingerprint(entry) {
		t.Errorf("Fingerprint = %q; want the pin of its own text", got.Fingerprint)
	}
	if len(got.Changed) != 0 || len(got.Files) != 0 {
		t.Errorf("Changed = %q, Files = %q; want neither on a first proposal naming no file",
			got.Changed, got.Files)
	}
	if o.ProjectKeys[mcpServersPath] {
		t.Errorf("ProjectKeys = %v; a proposed server contributes nothing to the run", o.ProjectKeys)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none — a proposal is reported on Proposals, not announced", notices)
	}
}

// An adopted repo server joins the global servers, flagged Project, at every site a project layer
// is resolved — a layered load, a startup resolution and a Driver's per-workspace overlay — and a
// rejected one is reported apart and connects nothing.
func TestAdoptedRepoMCPServerIsMergedWithProjectFlag(t *testing.T) {
	t.Parallel()
	global := "mcp-servers:\n  - name: web\n    transport: streamable-http\n    endpoint: https://mcp.example.com\n"
	project := projectDocsServerYAML +
		"  - name: lint\n    transport: stdio\n    command: lint-server\n"
	globalPath, projectRoot := layerFixture(t, global, project)
	o, _ := loadLayered(t, globalPath, projectRoot)
	store := adoptionStore(t, globalPath, projectRoot)
	for _, p := range o.Proposals.Proposed {
		entry := adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}
		answer := store.Adopt
		if p.Handle == "lint" {
			answer = store.Reject
		}
		if err := answer(entry); err != nil {
			t.Fatalf("answer %s: %v", p.Handle, err)
		}
	}
	resolvers := map[string]func(t *testing.T) Options{
		"LoadLayeredConfig": func(t *testing.T) Options {
			resolved, _ := loadLayered(t, globalPath, projectRoot)
			return resolved
		},
		"ResolveOptions": func(t *testing.T) Options {
			return resolveAt(t, globalPath, projectRoot, false)
		},
		"OverlayProjectLayer": func(t *testing.T) Options {
			base := resolveAt(t, globalPath, projectRoot, true)
			doc, err := ReadGlobalDocument(base.ConfigDir, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("ReadGlobalDocument: %v", err)
			}
			overlaid := OverlayProjectLayer(base, doc, projectRoot, os.ReadFile, noNotify)
			if got := serverNames(base.MCPServers); !slices.Equal(got, []string{"web"}) {
				t.Errorf("base servers = %q after the overlay; want the base left as it was", got)
			}
			return overlaid
		},
	}
	for name, resolve := range resolvers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved := resolve(t)

			if got, want := serverNames(resolved.MCPServers), []string{"web", "docs (project)"}; !slices.Equal(got, want) {
				t.Errorf("MCPServers = %q; want %q", got, want)
			}
			adopted, rejected := resolved.Proposals.Adopted, resolved.Proposals.Rejected
			if len(adopted) != 1 || adopted[0].Handle != "docs" || len(rejected) != 1 ||
				rejected[0].Handle != "lint" || len(resolved.Proposals.Proposed) != 0 {
				t.Errorf("Proposals = %+v; want docs adopted and lint rejected", resolved.Proposals)
			}
			if !resolved.ProjectKeys[mcpServersPath] {
				t.Errorf("ProjectKeys = %v; want mcp-servers recorded once a project server is live",
					resolved.ProjectKeys)
			}
		})
	}
}

// resolveAt resolves the options of the home beside globalPath for a workspace at projectRoot, as
// a Driver's startup does — global-only when globalOnly is set.
func resolveAt(t *testing.T, globalPath, projectRoot string, globalOnly bool) Options {
	t.Helper()

	opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot, GlobalConfigOnly: globalOnly}
	if _, err := ResolveOptions(&opts, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify, testHostID); err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	return opts
}

// A repo server named like one of the user's own is dropped with a notice naming it and never
// offered: the global entry stands as written.
func TestRepoMCPServerNameCollisionKeepsTheGlobal(t *testing.T) {
	t.Parallel()
	global := "mcp-servers:\n  - name: docs\n    transport: sse\n    endpoint: https://docs.example.com\n"
	globalPath, projectRoot := layerFixture(t, global, projectDocsServerYAML)

	o, notices := loadLayered(t, globalPath, projectRoot)

	if len(o.MCPServers) != 1 || o.MCPServers[0].Project || o.MCPServers[0].Transport != mcp.TransportSSE {
		t.Errorf("MCPServers = %+v; want the global docs server alone", o.MCPServers)
	}
	if !reflect.DeepEqual(o.Proposals, ProjectProposals{}) {
		t.Errorf("Proposals = %+v; want none — a shadowed server is never offered", o.Proposals)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], `mcp-servers entry "docs"`) ||
		!strings.Contains(notices[0], "global config") {
		t.Errorf("notices = %q; want one naming the shadowed docs entry", notices)
	}
}

// An adopted repo stdio server that lists no `env-allowlist:` inherits the platform floor alone; a
// listed allowlist stands as written, and an HTTP server, which launches nothing, is left as it is.
func TestRepoStdioServerDefaultsToTheEnvFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry string
		want  *[]string
	}{
		{
			name:  "stdio with no allowlist gets the floor",
			entry: "  - name: s\n    transport: stdio\n    command: srv\n",
			want:  &[]string{},
		},
		{
			name:  "stdio with an allowlist keeps it",
			entry: "  - name: s\n    transport: stdio\n    command: srv\n    env-allowlist: [HOME]\n",
			want:  &[]string{"HOME"},
		},
		{
			name:  "an HTTP server has no child to narrow",
			entry: "  - name: s\n    transport: streamable-http\n    endpoint: https://s.example.com\n",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, "", "mcp-servers:\n"+tt.entry)
			adoptProposedServers(t, globalPath, projectRoot)

			o, _ := loadLayered(t, globalPath, projectRoot)

			if len(o.MCPServers) != 1 {
				t.Fatalf("MCPServers = %q; want the adopted server", serverNames(o.MCPServers))
			}
			if got := o.MCPServers[0].EnvAllowlist; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnvAllowlist = %v; want %v", derefList(got), derefList(tt.want))
			}
		})
	}
}

// derefList spells an optional list for a failure message: <nil>, or its elements.
func derefList(list *[]string) any {
	if list == nil {
		return nil
	}
	return *list
}

// A server whose pin moved since it was adopted is proposed again, naming each changed field and
// then each changed project file; its pane lists the files the pin holds.
func TestRepoMCPProposalCarriesTheChangeReason(t *testing.T) {
	t.Parallel()
	entry := "mcp-servers:\n  - name: docs\n    transport: stdio\n    command: sh\n" +
		"    args: [scripts/serve.sh]\n    env: [%s]\n"
	globalPath, projectRoot := layerFixture(t, "", strings.Replace(entry, "%s", "MODE=a", 1))
	script := filepath.Join(projectRoot, "scripts", "serve.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeLayerFile(t, script, "echo one\n")
	adoptProposedServers(t, globalPath, projectRoot)
	writeLayerFile(t, script, "echo two\n")
	writeLayerFile(t, projectFilePath(projectRoot), strings.Replace(entry, "%s", "MODE=b", 1))

	o, _ := loadLayered(t, globalPath, projectRoot)

	if len(o.Proposals.Proposed) != 1 || len(o.MCPServers) != 0 {
		t.Fatalf("Proposals = %+v, MCPServers = %q; want the moved server proposed again and inert",
			o.Proposals, serverNames(o.MCPServers))
	}
	got := o.Proposals.Proposed[0]
	if want := []string{"env", "scripts/serve.sh"}; !slices.Equal(got.Changed, want) {
		t.Errorf("Changed = %q; want %q", got.Changed, want)
	}
	if want := []string{"scripts/serve.sh"}; !slices.Equal(got.Files, want) {
		t.Errorf("Files = %q; want %q", got.Files, want)
	}
	if env := got.Fields[slices.IndexFunc(got.Fields, isEnvField)]; !slices.Equal(env.Values, []string{"MODE=b"}) {
		t.Errorf("env field = %+v; want the entry shown in full as KEY=VALUE", env)
	}
}

// isEnvField reports whether f is a Proposal's `env:` field.
func isEnvField(f domain.ProposalField) bool { return f.Label == pinEnvKey }

// A repo server that cannot be used is dropped alone with a notice naming it: the file's other
// servers are still offered and its tighten-only keys still apply.
func TestRepoMCPServerRefusedAloneKeepsTheTightenOnlyKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		servers      string
		wantProposed []string
		wantNotices  []string
	}{
		{
			name: "bad entries beside a good one",
			servers: "mcp-servers:\n" +
				"  - transport: stdio\n    command: nameless\n" +
				"  - name: ok\n    transport: stdio\n    command: ok-server\n" +
				"  - name: ok\n    transport: stdio\n    command: again\n" +
				"  - name: bad\n    transport: sse\n    endpoint: https://b.example.com\n" +
				"    headers: {\"X Tenant\": v}\n" +
				"  - name: oauth\n    transport: stdio\n    command: o\n    auth: oauth\n",
			wantProposed: []string{"ok"},
			wantNotices: []string{
				"mcp-servers entry #1: it has no name",
				`mcp-servers entry "ok": an earlier entry already uses that name`,
				`mcp-servers entry "bad": `,
				`mcp-servers entry "oauth": `,
			},
		},
		{
			name:        "a value that is not a list",
			servers:     "mcp-servers: docs\n",
			wantNotices: []string{"ignoring mcp-servers: it is not a list of servers"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, "", "tools:\n  disabled: [web_fetch]\n"+tt.servers)

			o, notices := loadLayered(t, globalPath, projectRoot)

			if !slices.Contains(o.ToolsDisabled, "web_fetch") {
				t.Errorf("tools.disabled = %q; want the project's web_fetch to still apply", o.ToolsDisabled)
			}
			var proposed []string
			for _, p := range o.Proposals.Proposed {
				proposed = append(proposed, p.Handle)
			}
			if !slices.Equal(proposed, tt.wantProposed) {
				t.Errorf("proposed = %q; want %q", proposed, tt.wantProposed)
			}
			if len(notices) != len(tt.wantNotices) {
				t.Fatalf("notices = %q; want %d", notices, len(tt.wantNotices))
			}
			for i, want := range tt.wantNotices {
				if !strings.Contains(notices[i], want) {
					t.Errorf("notice %d = %q; want it to contain %q", i, notices[i], want)
				}
			}
		})
	}
}
