package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/reactions/scope"
)

// projectDocsServerYAML is a Project config proposing one stdio server and nothing else.
const projectDocsServerYAML = "mcp-servers:\n  - name: docs\n    transport: stdio\n    command: docs-server\n"

// adoptProposedServers loads the layered config of globalPath and projectRoot and records every
// structured entry it proposes — MCP servers and reactions alike — as adopted, answering the
// proposals it adopted.
func adoptProposedServers(t *testing.T, globalPath, projectRoot string) []domain.Proposal {
	t.Helper()

	_, offer, _ := loadLayeredOffer(t, globalPath, projectRoot)
	store := adoptionStore(t, globalPath, projectRoot)
	for _, p := range offer.Proposals.Proposed {
		entry := adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}
		if err := store.Adopt(entry); err != nil {
			t.Fatalf("Adopt %s: %v", p.Handle, err)
		}
	}
	return offer.Proposals.Proposed
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

	o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(o.MCPServers) != 0 {
		t.Errorf("MCPServers = %v; want none — a proposed server connects nothing", serverNames(o.MCPServers))
	}
	if len(offer.Proposals.Proposed) != 1 || len(offer.Proposals.Adopted)+len(offer.Proposals.Rejected) != 0 {
		t.Fatalf("Proposals = %+v; want the one server proposed", offer.Proposals)
	}
	got := offer.Proposals.Proposed[0]
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
	_, offer, _ := loadLayeredOffer(t, globalPath, projectRoot)
	store := adoptionStore(t, globalPath, projectRoot)
	for _, p := range offer.Proposals.Proposed {
		entry := adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}
		answer := store.Adopt
		if p.Handle == "lint" {
			answer = store.Reject
		}
		if err := answer(entry); err != nil {
			t.Fatalf("answer %s: %v", p.Handle, err)
		}
	}
	resolvers := map[string]func(t *testing.T) (Options, ProjectOffer){
		"LoadLayeredOffer": func(t *testing.T) (Options, ProjectOffer) {
			resolved, offer, _ := loadLayeredOffer(t, globalPath, projectRoot)
			return resolved, offer
		},
		"ResolveOptions": func(t *testing.T) (Options, ProjectOffer) {
			return resolveOfferAt(t, globalPath, projectRoot, false)
		},
		"OverlayProjectOffer": func(t *testing.T) (Options, ProjectOffer) {
			base, _ := resolveOfferAt(t, globalPath, projectRoot, true)
			doc, err := ReadGlobalDocument(base.ConfigDir, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("ReadGlobalDocument: %v", err)
			}
			overlaid, offer := OverlayProjectOffer(base, doc, projectRoot, os.ReadFile, noNotify)
			if got := serverNames(base.MCPServers); !slices.Equal(got, []string{"web"}) {
				t.Errorf("base servers = %q after the overlay; want the base left as it was", got)
			}
			return overlaid, offer
		},
	}
	for name, resolve := range resolvers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved, offer := resolve(t)

			if got, want := serverNames(resolved.MCPServers), []string{"web", "docs (project)"}; !slices.Equal(got, want) {
				t.Errorf("MCPServers = %q; want %q", got, want)
			}
			adopted, rejected := offer.Proposals.Adopted, offer.Proposals.Rejected
			if len(adopted) != 1 || adopted[0].Handle != "docs" || len(rejected) != 1 ||
				rejected[0].Handle != "lint" || len(offer.Proposals.Proposed) != 0 {
				t.Errorf("Proposals = %+v; want docs adopted and lint rejected", offer.Proposals)
			}
			if !resolved.ProjectKeys[mcpServersPath] {
				t.Errorf("ProjectKeys = %v; want mcp-servers recorded once a project server is live",
					resolved.ProjectKeys)
			}
		})
	}
}

// resolveOfferAt resolves the options of the home beside globalPath for a workspace at projectRoot,
// as a Driver's startup does — global-only when globalOnly is set — with the Project config's offer
// the same resolution read beside them.
func resolveOfferAt(t *testing.T, globalPath, projectRoot string, globalOnly bool) (Options, ProjectOffer) {
	t.Helper()

	opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot, GlobalConfigOnly: globalOnly}
	_, offer, err := resolveOptions(&opts, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify, testHostID)
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	return opts, offer
}

// A repo server named like one of the user's own is dropped with a notice naming it and never
// offered: the global entry stands as written.
func TestRepoMCPServerNameCollisionKeepsTheGlobal(t *testing.T) {
	t.Parallel()
	global := "mcp-servers:\n  - name: docs\n    transport: sse\n    endpoint: https://docs.example.com\n"
	globalPath, projectRoot := layerFixture(t, global, projectDocsServerYAML)

	o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(o.MCPServers) != 1 || o.MCPServers[0].Project || o.MCPServers[0].Transport != mcp.TransportSSE {
		t.Errorf("MCPServers = %+v; want the global docs server alone", o.MCPServers)
	}
	if !reflect.DeepEqual(offer.Proposals, ProjectProposals{}) {
		t.Errorf("Proposals = %+v; want none — a shadowed server is never offered", offer.Proposals)
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

	o, offer, _ := loadLayeredOffer(t, globalPath, projectRoot)

	if len(offer.Proposals.Proposed) != 1 || len(o.MCPServers) != 0 {
		t.Fatalf("Proposals = %+v, MCPServers = %q; want the moved server proposed again and inert",
			offer.Proposals, serverNames(o.MCPServers))
	}
	got := offer.Proposals.Proposed[0]
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

			o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

			if !slices.Contains(o.ToolsDisabled, "web_fetch") {
				t.Errorf("tools.disabled = %q; want the project's web_fetch to still apply", o.ToolsDisabled)
			}
			var proposed []string
			for _, p := range offer.Proposals.Proposed {
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

// projectNotifyReactionYAML is a Project config proposing one observe reaction and nothing else.
const projectNotifyReactionYAML = "reactions:\n  - id: notify\n    on: [file-changed]\n    run: [notify-send, saved]\n"

// reactionIDs is the ids of list, in order, each marked `(project)` when the Project config
// contributed it.
func reactionIDs(list []domain.Reaction) []string {
	ids := make([]string, len(list))
	for i, r := range list {
		ids[i] = r.ID
		if r.Project {
			ids[i] += " (project)"
		}
	}
	return ids
}

// proposalHandles is the handles of proposals, in order.
func proposalHandles(proposals []domain.Proposal) []string {
	var handles []string
	for _, p := range proposals {
		handles = append(handles, p.Handle)
	}
	return handles
}

// A Project config's reaction is proposed — with what its pane shows — and fires nothing until the
// user adopts it; once adopted it joins the global reactions, flagged Project, at every site a
// project layer is resolved.
func TestRepoReactionIsInertUntilAdopted(t *testing.T) {
	t.Parallel()
	global := "reactions:\n  - id: log\n    on: [turn-finished]\n    run: [logger, turn]\n"
	globalPath, projectRoot := layerFixture(t, global, projectNotifyReactionYAML)

	o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if got := reactionIDs(o.Reactions); !slices.Equal(got, []string{"log"}) {
		t.Errorf("Reactions = %q; want the global log alone — a proposed reaction fires nothing", got)
	}
	if len(offer.Proposals.Proposed) != 1 || len(offer.Proposals.Adopted)+len(offer.Proposals.Rejected) != 0 {
		t.Fatalf("Proposals = %+v; want the one reaction proposed", offer.Proposals)
	}
	got := offer.Proposals.Proposed[0]
	wantFields := []domain.ProposalField{{Label: "run", Shape: domain.ProposalList, Values: []string{"notify-send", "saved"}}}
	if got.Kind != domain.ProposalReaction || got.Handle != "notify" || !reflect.DeepEqual(got.Fields, wantFields) {
		t.Errorf("proposal = %+v; want reaction entry notify with fields %+v", got, wantFields)
	}
	if o.ProjectKeys[reactionsPath] {
		t.Errorf("ProjectKeys = %v; a proposed reaction contributes nothing to the run", o.ProjectKeys)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none — a proposal is reported on Proposals, not announced", notices)
	}

	adoptProposedServers(t, globalPath, projectRoot)
	resolvers := map[string]func(t *testing.T) (Options, ProjectOffer){
		"LoadLayeredOffer": func(t *testing.T) (Options, ProjectOffer) {
			resolved, offer, _ := loadLayeredOffer(t, globalPath, projectRoot)
			return resolved, offer
		},
		"ResolveOptions": func(t *testing.T) (Options, ProjectOffer) {
			return resolveOfferAt(t, globalPath, projectRoot, false)
		},
		"OverlayProjectOffer": func(t *testing.T) (Options, ProjectOffer) {
			base, _ := resolveOfferAt(t, globalPath, projectRoot, true)
			doc, err := ReadGlobalDocument(base.ConfigDir, os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("ReadGlobalDocument: %v", err)
			}
			overlaid, offer := OverlayProjectOffer(base, doc, projectRoot, os.ReadFile, noNotify)
			if got := reactionIDs(base.Reactions); !slices.Equal(got, []string{"log"}) {
				t.Errorf("base reactions = %q after the overlay; want the base left as it was", got)
			}
			return overlaid, offer
		},
	}
	for name, resolve := range resolvers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved, offer := resolve(t)

			if got, want := reactionIDs(resolved.Reactions), []string{"log", "notify (project)"}; !slices.Equal(got, want) {
				t.Errorf("Reactions = %q; want %q", got, want)
			}
			if got := proposalHandles(offer.Proposals.Adopted); !slices.Equal(got, []string{"notify"}) {
				t.Errorf("adopted = %q; want notify", got)
			}
			if !resolved.ProjectKeys[reactionsPath] {
				t.Errorf("ProjectKeys = %v; want reactions recorded once a project reaction is live",
					resolved.ProjectKeys)
			}
		})
	}
}

// A reaction's webhook action is proposed as a group of its own fields, in the order the pane hangs
// them — where it posts, then what it sends — and an argv action stays a list even when its one
// element reads like a mapping, so the pane can never draw a program as a webhook.
func TestRepoReactionProposalCarriesItsActionShapes(t *testing.T) {
	t.Parallel()
	project := "reactions:\n" +
		"  - id: odd\n" +
		"    on: [file-changed]\n" +
		"    run: ['{\"url\":\"https://example.com\"}']\n" +
		"  - id: warden\n" +
		"    on: [pre-tool-exec]\n" +
		"    gate:\n" +
		"      url: https://hooks.example.com/gate\n" +
		"      headers-env:\n" +
		"        Authorization: HOOK_TOKEN\n"
	globalPath, projectRoot := layerFixture(t, "", project)

	_, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(offer.Proposals.Proposed) != 2 {
		t.Fatalf("Proposals = %+v (notices %q); want both reactions proposed", offer.Proposals, notices)
	}
	want := [][]domain.ProposalField{
		{{Label: "run", Shape: domain.ProposalList, Values: []string{`{"url":"https://example.com"}`}}},
		{{Label: "gate", Shape: domain.ProposalGroup, Fields: []domain.ProposalField{
			{Label: "url", Values: []string{"https://hooks.example.com/gate"}},
			{Label: "headers-env", Shape: domain.ProposalMap, Values: []string{"Authorization: HOOK_TOKEN"}},
		}}},
	}
	for i, p := range offer.Proposals.Proposed {
		if !reflect.DeepEqual(p.Fields, want[i]) {
			t.Errorf("%s fields = %+v; want %+v", p.Handle, p.Fields, want[i])
		}
	}
}

// A repo reaction's `mcp:` mapping pins whole and pins the same on every load: the entry is
// Proposed under one Fingerprint across two loads, and its server and tool names are never searched
// for project files — even when a file in the root shares the tool's name — so the pin carries none.
func TestRepoMCPReactionPinIsStableAcrossLoads(t *testing.T) {
	t.Parallel()
	project := "reactions:\n" +
		"  - id: launcher\n" +
		"    on: [session-started]\n" +
		"    advise:\n" +
		"      mcp:\n" +
		"        server: llama\n" +
		"        tool: status\n" +
		"        args:\n" +
		"          verbose: true\n" +
		"          profile: coder\n" +
		"          detail: full\n" +
		"        payload-arg: event\n"
	globalPath, projectRoot := layerFixture(t, "", project)
	writeLayerFile(t, filepath.Join(projectRoot, "status"), "#!/bin/sh\n")

	_, first, firstNotices := loadLayeredOffer(t, globalPath, projectRoot)
	_, second, _ := loadLayeredOffer(t, globalPath, projectRoot)

	if len(first.Proposals.Proposed) != 1 || len(second.Proposals.Proposed) != 1 {
		t.Fatalf("Proposals = %+v / %+v (notices %q); want the launcher reaction proposed on both loads",
			first.Proposals, second.Proposals, firstNotices)
	}
	a, b := first.Proposals.Proposed[0], second.Proposals.Proposed[0]
	if a.Handle != "launcher" || a.Fingerprint == "" {
		t.Fatalf("proposal = %+v; want the launcher reaction under a non-empty Fingerprint", a)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Errorf("Fingerprint moved between loads: %q then %q; want one pin for one unchanged entry",
			a.Fingerprint, b.Fingerprint)
	}
	if len(a.Files) != 0 {
		t.Errorf("Files = %q; want none — an mcp mapping's names are not project files", a.Files)
	}
}

// A repo reaction with the id of one of the user's own is dropped with a notice naming it and never
// offered: the global entry stands as written.
func TestRepoReactionIDCollisionKeepsTheGlobal(t *testing.T) {
	t.Parallel()
	global := "reactions:\n  - id: notify\n    on: [turn-finished]\n    run: [logger, turn]\n"
	globalPath, projectRoot := layerFixture(t, global, projectNotifyReactionYAML)

	o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(o.Reactions) != 1 || o.Reactions[0].Project || o.Reactions[0].On[0] != domain.MomentTurnFinished {
		t.Errorf("Reactions = %+v; want the global notify alone", o.Reactions)
	}
	if !reflect.DeepEqual(offer.Proposals, ProjectProposals{}) {
		t.Errorf("Proposals = %+v; want none — a shadowed reaction is never offered", offer.Proposals)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], `reactions entry "notify"`) ||
		!strings.Contains(notices[0], "global config") {
		t.Errorf("notices = %q; want one naming the shadowed notify entry", notices)
	}
}

// An adopted repo reaction that spells no `workspace:` carries no filter: the layer already keeps
// it to sessions under its Project root (ADR 0104 §7).
func TestRepoReactionUnsetWorkspaceLeavesTheFilterEmpty(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", projectNotifyReactionYAML)
	adoptProposedServers(t, globalPath, projectRoot)

	o, _ := loadLayered(t, globalPath, projectRoot)

	if len(o.Reactions) != 1 || !o.Reactions[0].Project {
		t.Fatalf("Reactions = %+v; want the adopted notify, flagged Project", o.Reactions)
	}
	if got := o.Reactions[0].Workspace; got != "" {
		t.Errorf("Workspace = %q; want the filter left empty", got)
	}
}

// A repo reaction's spelled `workspace:` must name the Project root or a directory inside it, a
// relative one resolving against the root; one outside refuses that entry alone with a notice.
func TestRepoReactionWorkspaceOutsideTheRootIsRefused(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	tests := []struct {
		name      string
		workspace string
		want      func(projectRoot string) string
		refused   bool
	}{
		{name: "an absolute path outside", workspace: outside, refused: true},
		{name: "a relative path climbing out", workspace: "../elsewhere", refused: true},
		{name: "a home-relative path", workspace: "~/elsewhere", refused: true},
		{
			name: "a relative path inside resolves against the root", workspace: "sub",
			want: func(projectRoot string) string { return filepath.Join(projectRoot, "sub") },
		},
		{
			name: "the root itself", workspace: ".",
			want: func(projectRoot string) string { return projectRoot },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			project := "reactions:\n  - id: scoped\n    on: [file-changed]\n    run: [notify-send]\n" +
				"    workspace: " + tt.workspace + "\n" + projectNotifyReactionYAML[len("reactions:\n"):]
			globalPath, projectRoot := layerFixture(t, "", project)
			adoptProposedServers(t, globalPath, projectRoot)

			o, notices := loadLayered(t, globalPath, projectRoot)

			if tt.refused {
				if got := reactionIDs(o.Reactions); !slices.Equal(got, []string{"notify (project)"}) {
					t.Errorf("Reactions = %q; want the refused entry dropped and notify kept", got)
				}
				if len(notices) != 1 || !strings.Contains(notices[0], `reactions entry "scoped"`) ||
					!strings.Contains(notices[0], "outside the Project root") {
					t.Errorf("notices = %q; want one refusing scoped as outside the root", notices)
				}
				return
			}
			if len(notices) != 0 || len(o.Reactions) != 2 {
				t.Fatalf("Reactions = %q, notices = %q; want both entries live and no notice",
					reactionIDs(o.Reactions), notices)
			}
			want, err := scope.Resolve(tt.want(projectRoot))
			if err != nil {
				t.Fatalf("scope.Resolve: %v", err)
			}
			if got := o.Reactions[0].Workspace; got != want {
				t.Errorf("Workspace = %q; want %q", got, want)
			}
		})
	}
}

// A repo reaction that cannot be used is dropped alone with a notice naming it: the file's other
// reactions are still offered and its tighten-only keys still apply.
func TestRepoReactionRefusedAloneKeepsTheRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		reactions    string
		wantProposed []string
		wantNotices  []string
	}{
		{
			name: "bad entries beside a good one",
			reactions: "reactions:\n" +
				"  - on: [file-changed]\n    run: [nameless]\n" +
				"  - id: typo\n    on: [file-chnaged]\n    run: [x]\n" +
				projectNotifyReactionYAML[len("reactions:\n"):] +
				"  - id: actionless\n    on: [file-changed]\n" +
				"  - id: slow\n    on: [file-changed]\n    run: [x]\n    timeout: soon\n" +
				"  - id: shape\n    on: [file-changed]\n    run: echo\n",
			wantProposed: []string{"notify"},
			wantNotices: []string{
				"reactions entry #1: it has no id",
				`reactions entry "typo": `,
				`reactions entry "actionless": `,
				`reactions entry "slow": timeout: "soon" is not a duration`,
				`reactions entry "shape": run: is an argv list`,
			},
		},
		{
			name:        "a value that is not a list",
			reactions:   "reactions: notify\n",
			wantNotices: []string{"ignoring reactions: it is not a list of reactions"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, "", "tools:\n  disabled: [web_fetch]\n"+tt.reactions)

			o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

			if !slices.Contains(o.ToolsDisabled, "web_fetch") {
				t.Errorf("tools.disabled = %q; want the project's web_fetch to still apply", o.ToolsDisabled)
			}
			if got := proposalHandles(offer.Proposals.Proposed); !slices.Equal(got, tt.wantProposed) {
				t.Errorf("proposed = %q; want %q", got, tt.wantProposed)
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

// A second repo reaction with an id an earlier one took is refused alone; the first is offered.
func TestRepoReactionRepeatedIDIsRefused(t *testing.T) {
	t.Parallel()
	project := projectNotifyReactionYAML + "  - id: notify\n    on: [turn-finished]\n    run: [other]\n"
	globalPath, projectRoot := layerFixture(t, "", project)

	_, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(offer.Proposals.Proposed) != 1 {
		t.Fatalf("Proposals = %+v; want the first notify alone proposed", offer.Proposals)
	}
	if got := offer.Proposals.Proposed[0].Fields; !reflect.DeepEqual(got,
		[]domain.ProposalField{{Label: "run", Shape: domain.ProposalList, Values: []string{"notify-send", "saved"}}}) {
		t.Errorf("proposed fields = %+v; want the first entry's", got)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], `reactions entry "notify": an earlier entry already uses that id`) {
		t.Errorf("notices = %q; want one refusing the repeated notify", notices)
	}
}

// The parameters a reaction's pin leaves out — `on:`, `timeout:`, `enabled:` and `workspace:` —
// apply to an adopted entry as the file states them now, without proposing it again; a change to
// what it runs proposes it again and leaves it inert.
func TestAdoptedRepoReactionTakesParameterEditsLive(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", projectNotifyReactionYAML)
	adoptProposedServers(t, globalPath, projectRoot)
	writeLayerFile(t, projectFilePath(projectRoot), "reactions:\n  - id: notify\n"+
		"    on: [turn-finished]\n    run: [notify-send, saved]\n    timeout: 9s\n    workspace: sub\n"+
		"    enabled: true\n")

	o, offer, notices := loadLayeredOffer(t, globalPath, projectRoot)

	if len(notices) != 0 || len(offer.Proposals.Proposed) != 0 || len(o.Reactions) != 1 {
		t.Fatalf("Reactions = %q, Proposals = %+v, notices = %q; want notify still adopted and live",
			reactionIDs(o.Reactions), offer.Proposals, notices)
	}
	wantWorkspace, err := scope.Resolve(filepath.Join(projectRoot, "sub"))
	if err != nil {
		t.Fatalf("scope.Resolve: %v", err)
	}
	got := o.Reactions[0]
	if !slices.Equal(got.On, []domain.Moment{domain.MomentTurnFinished}) || got.Timeout != 9*time.Second ||
		got.Workspace != wantWorkspace || !got.Project {
		t.Errorf("reaction = %+v; want on [turn-finished], 9s, workspace %q, flagged Project", got, wantWorkspace)
	}

	writeLayerFile(t, projectFilePath(projectRoot), projectNotifyReactionYAML+"    enabled: false\n")
	parked, parkedOffer, _ := loadLayeredOffer(t, globalPath, projectRoot)
	if len(parked.Reactions) != 0 || len(parkedOffer.Proposals.Adopted) != 1 {
		t.Errorf("Reactions = %q, Proposals = %+v; want notify still adopted but parked",
			reactionIDs(parked.Reactions), parkedOffer.Proposals)
	}

	writeLayerFile(t, projectFilePath(projectRoot), strings.Replace(projectNotifyReactionYAML, "saved", "done", 1))
	moved, movedOffer, _ := loadLayeredOffer(t, globalPath, projectRoot)
	if len(moved.Reactions) != 0 || len(movedOffer.Proposals.Proposed) != 1 ||
		!slices.Equal(movedOffer.Proposals.Proposed[0].Changed, []string{"run"}) {
		t.Errorf("Reactions = %q, Proposals = %+v; want notify proposed again for its run and inert",
			reactionIDs(moved.Reactions), movedOffer.Proposals)
	}
}

// An adopted repo server carries its adoption pin to the MCP client's launch (ADR 0104 §4): its
// Admit hands the launch the Project root and the pinned project files — the program and a file an
// env value names — and refuses as moved, naming that file, once one of them is edited.
func TestAdoptedRepoMCPServerCarriesItsPin(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "",
		"mcp-servers:\n  - name: lint\n    transport: stdio\n    command: ./bin/lint-server\n"+
			"    env: [LINT_CONFIG=conf/lint.json]\n")
	program := filepath.Join(projectRoot, "bin", "lint-server")
	config := filepath.Join(projectRoot, "conf", "lint.json")
	for _, f := range []string{program, config} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(f, []byte("v1\n"), 0o700); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	adoptProposedServers(t, globalPath, projectRoot)

	o, _ := loadLayered(t, globalPath, projectRoot)

	if len(o.MCPServers) != 1 || o.MCPServers[0].Pin == nil {
		t.Fatalf("MCPServers = %+v; want the adopted lint, carrying its pin", o.MCPServers)
	}
	pin := o.MCPServers[0].Pin
	assertPinAdmits(t, pin, domain.PinLaunch{Dir: projectRoot, Programs: []string{program, config}})
	if err := os.WriteFile(config, []byte("v2\n"), 0o700); err != nil {
		t.Fatalf("edit the config: %v", err)
	}
	assertPinMoved(t, pin, []string{"conf/lint.json"})
}

// An adopted repo reaction carries its adoption pin to the spawn (ADR 0104 §4): its Admit hands
// the spawn the Project root it runs in and the pinned script as the one program it may run inside
// the fence, and refuses as moved, naming the script, once its content moves.
func TestAdoptedRepoReactionCarriesItsPin(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "",
		"reactions:\n  - id: fmt\n    on: [file-changed]\n    run: [./scripts/fmt.sh]\n")
	script := filepath.Join(projectRoot, "scripts", "fmt.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write the script: %v", err)
	}
	adoptProposedServers(t, globalPath, projectRoot)

	o, _ := loadLayered(t, globalPath, projectRoot)

	if len(o.Reactions) != 1 || o.Reactions[0].Pin == nil {
		t.Fatalf("Reactions = %+v; want the adopted fmt, carrying its pin", o.Reactions)
	}
	pin := o.Reactions[0].Pin
	assertPinAdmits(t, pin, domain.PinLaunch{Dir: projectRoot, Programs: []string{script}})
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncurl evil.example\n"), 0o700); err != nil {
		t.Fatalf("edit the script: %v", err)
	}
	assertPinMoved(t, pin, []string{"scripts/fmt.sh"})
}

// assertPinAdmits fails the test unless pin admits its launch as want.
func assertPinAdmits(t *testing.T, pin domain.ProjectPin, want domain.PinLaunch) {
	t.Helper()
	launch, err := pin.Admit()
	if err != nil {
		t.Fatalf("Admit before an edit: %v; want the launch admitted", err)
	}
	if !reflect.DeepEqual(launch, want) {
		t.Errorf("Admit = %+v; want %+v", launch, want)
	}
}

// assertPinMoved fails the test unless pin refuses its launch as moved, naming files.
func assertPinMoved(t *testing.T, pin domain.ProjectPin, files []string) {
	t.Helper()
	_, err := pin.Admit()
	var moved *domain.PinMovedError
	if !errors.As(err, &moved) {
		t.Fatalf("Admit after an edit = %v; want a *domain.PinMovedError", err)
	}
	if !reflect.DeepEqual(moved.Files, files) || moved.Err != nil {
		t.Errorf("PinMovedError = {Files: %q, Err: %v}; want {Files: %q, Err: nil}", moved.Files, moved.Err, files)
	}
}
