package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"gopkg.in/yaml.v3"
)

// The Project config's execution keys — repo MCP servers (ADR 0104)
//
// A Project config's `mcp-servers:` entries are granting: a cloned repo must not launch a program
// or dial an endpoint because it says so. So the layer carries them apart from the node merge
// (layer.go). Each entry is decoded and checked ALONE — a refused one is dropped with a notice
// naming it and never costs the rest of the file (ADR 0104 §7) — then pinned (execpin.go), and
// sorted by the user's adoption record together with the file's Allow rules, in one read of it. An
// entry whose name a global server already uses is dropped before it is offered: the user's own
// entry wins (§5). Only an adopted entry joins the resolved servers, flagged Project and, when it
// is a stdio server that lists no `env-allowlist:`, narrowed to the platform floor (§6); every one
// is reported on Options.Proposals with what its adoption pane shows.

// mcpServersPath is the `mcp-servers:` key's registry path, the granting key whose project entries
// the layer judges one by one.
const mcpServersPath = "mcp-servers"

// serverHandleField is the pinned field that names a server entry, which the pane carries in its
// title rather than its body.
const serverHandleField = "name"

// repoServerRefusedNotice is the line a project `mcp-servers:` entry that cannot be used costs: the
// project file, the entry, and why. The rest of the file still applies.
const repoServerRefusedNotice = "apogee: project config %s: ignoring mcp-servers entry %s: %s"

// repoServerShadowedNotice is the line a project server named like a global one costs: the user's
// own entry wins, and the project's is never offered (ADR 0104 §5).
const repoServerShadowedNotice = "apogee: project config %s: ignoring mcp-servers entry %q — " +
	"your global config defines a server of that name, and it wins"

// projectSkippedKeyNotice is the line a project key whose whole value is refused costs: the file,
// the key, and why — the rest of the file still applies.
const projectSkippedKeyNotice = "apogee: project config %s: ignoring %s: %s"

// ProjectProposals is a Project config's structured granting entries sorted by the user's adoption
// record, each list in file order. Adopted entries are live — an adopted server is also in
// Options.MCPServers; Proposed ones wait on an answer, each carrying what changed since its handle
// was last adopted; Rejected ones the user turned down. Only Adopted grants anything. The zero
// value is no entries.
type ProjectProposals struct {
	Adopted  []domain.Proposal
	Proposed []domain.Proposal
	Rejected []domain.Proposal
}

// projectServers is a Project config's MCP servers resolved by Adoption: the adopted ones as the
// servers they connect as, and every one as a Proposal.
type projectServers struct {
	adopted   []mcp.ServerConfig
	proposals ProjectProposals
}

// effective is the resolved `mcp-servers:` key: the global servers, then the adopted project ones.
// It builds a fresh slice whenever it adds any, so a base resolution's list is never shared.
func (p projectServers) effective(global []mcp.ServerConfig) []mcp.ServerConfig {
	if len(p.adopted) == 0 {
		return global
	}
	return slices.Concat(global, p.adopted)
}

// globalMCPServers is servers less every one the Project config contributed — what a project's
// adopted servers join, whatever layer the resolution they came from took. Built fresh.
func globalMCPServers(servers []mcp.ServerConfig) []mcp.ServerConfig {
	var global []mcp.ServerConfig
	for _, s := range servers {
		if !s.Project {
			global = append(global, s)
		}
	}
	return global
}

// repoMCPServers decodes and checks the project file's `mcp-servers:` value (nil when the file
// states none) one entry at a time, answering the entries that passed in file order. An entry that
// does not decode, has no name, repeats an earlier entry's name, or spells headers or an `auth:`
// combination the global file would be refused for is dropped with one notice naming it; a value
// that is not a list drops every entry with one notice. Neither ever skips the layer.
func repoMCPServers(value *yaml.Node, path string, notify func(string)) []mcpServerConfig {
	if value == nil {
		return nil
	}
	if value.Kind != yaml.SequenceNode {
		notify(fmt.Sprintf(projectSkippedKeyNotice, path, mcpServersPath, "it is not a list of servers"))
		return nil
	}
	var servers []mcpServerConfig
	named := make(map[string]bool, len(value.Content))
	for i, item := range value.Content {
		var m mcpServerConfig
		label := fmt.Sprintf("#%d", i+1)
		err := resolveAlias(item).Decode(&m)
		if err == nil {
			if name := strings.TrimSpace(m.Name); name != "" {
				label = fmt.Sprintf("%q", m.Name)
			}
			err = checkRepoServer(m, named)
		}
		if err != nil {
			notify(fmt.Sprintf(repoServerRefusedNotice, path, label, sansPrefix{err}.Error()))
			continue
		}
		named[m.Name] = true
		servers = append(servers, m)
	}
	return servers
}

// checkRepoServer refuses the repo server m when it has no name, repeats a name in named, or spells
// headers or an `auth:` combination the global file's pass would refuse (fileMCPServers).
func checkRepoServer(m mcpServerConfig, named map[string]bool) error {
	switch {
	case strings.TrimSpace(m.Name) == "":
		return errors.New("it has no name")
	case named[m.Name]:
		return errors.New("an earlier entry already uses that name")
	}
	server := m.toServerConfig()
	if err := server.ValidateHeaders(); err != nil {
		return err
	}
	return server.ValidateAuth()
}

// pinnedServer is a repo server with the Adoption entry its pin builds.
type pinnedServer struct {
	config mcpServerConfig
	entry  adoption.Entry
}

// classifyProjectLayer sorts the layer's Allow rules and MCP servers by the adoption record under
// workspacesDir, in one read of it. A server named like one of global — the global file's servers
// — is dropped with a notice before it is pinned, and so is one whose pin cannot be built (a named
// project file that cannot be read), since what cannot be pinned cannot be shown to stay adopted.
func classifyProjectLayer(layer projectLayer, global []mcpServerConfig, workspacesDir,
	projectRoot string, notify func(string)) (projectAllowRules, projectServers) {
	servers := pinRepoServers(layer, global, projectRoot, notify)
	entries := slices.Clone(layer.allow)
	for _, s := range servers {
		entries = append(entries, s.entry)
	}
	sorted, store := classifyProjectEntries(entries, layer.path, workspacesDir, projectRoot, notify)
	return projectAllowRulesOf(sorted), resolveRepoServers(servers, sorted, store)
}

// pinRepoServers pins each of the layer's servers that no global server shadows, in file order.
func pinRepoServers(layer projectLayer, global []mcpServerConfig, projectRoot string,
	notify func(string)) []pinnedServer {
	globalNames := make(map[string]bool, len(global))
	for _, m := range global {
		globalNames[m.Name] = true
	}
	var servers []pinnedServer
	for _, m := range layer.servers {
		if globalNames[m.Name] {
			notify(fmt.Sprintf(repoServerShadowedNotice, layer.path, m.Name))
			continue
		}
		entry, err := m.execPin(projectRoot)
		if err != nil {
			notify(fmt.Sprintf(repoServerRefusedNotice, layer.path, fmt.Sprintf("%q", m.Name), err))
			continue
		}
		servers = append(servers, pinnedServer{config: m, entry: entry})
	}
	return servers
}

// resolveRepoServers turns the servers' classification into the adopted servers and the Proposals
// of every one. store is the record sorted was read from — nil when it could not be read, and then
// no change reason is named.
func resolveRepoServers(servers []pinnedServer, sorted adoption.Classification,
	store *adoption.Store) projectServers {
	byName := make(map[string]mcpServerConfig, len(servers))
	for _, s := range servers {
		byName[s.entry.Handle] = s.config
	}
	var resolved projectServers
	for _, e := range serverEntries(sorted.Adopted) {
		resolved.adopted = append(resolved.adopted, byName[e.Handle].repoServerConfig())
		resolved.proposals.Adopted = append(resolved.proposals.Adopted, proposalOf(e, serverHandleField))
	}
	for _, e := range serverEntries(sorted.Proposed) {
		proposal := proposalOf(e, serverHandleField)
		proposal.Changed = changedSinceAdopted(store, e)
		resolved.proposals.Proposed = append(resolved.proposals.Proposed, proposal)
	}
	for _, e := range serverEntries(sorted.Rejected) {
		resolved.proposals.Rejected = append(resolved.proposals.Rejected, proposalOf(e, serverHandleField))
	}
	return resolved
}

// serverEntries is the entries of entries that are repo MCP servers, in order.
func serverEntries(entries []adoption.Entry) []adoption.Entry {
	var servers []adoption.Entry
	for _, e := range entries {
		if e.Kind == MCPServerAdoptionKind {
			servers = append(servers, e)
		}
	}
	return servers
}

// repoServerConfig is the adopted repo server m as the client connects it: flagged Project, and a
// stdio server that lists no `env-allowlist:` handed the platform floor alone (ADR 0104 §6) rather
// than apogee's whole environment — the repo lists every variable it needs, and the pin holds them.
func (m mcpServerConfig) repoServerConfig() mcp.ServerConfig {
	server := m.toServerConfig()
	server.Project = true
	if server.Transport == mcp.TransportStdio && server.EnvAllowlist == nil {
		floor := []string{}
		server.EnvAllowlist = &floor
	}
	return server
}

// changedSinceAdopted names what moved in e since the text last adopted for its kind and handle —
// nothing when none is on record, or the record could not be read (store nil).
func changedSinceAdopted(store *adoption.Store, e adoption.Entry) []string {
	if store == nil {
		return nil
	}
	adoptedText, ok := store.Adopted(e.Kind, e.Handle)
	if !ok {
		return nil
	}
	return changedSince(adoptedText, e.Text)
}

// proposalOf is the Proposal of the structured entry e: its pinned fields in the pane's order
// (pinFieldOrder) less handleField, which names it, and the project files its pin holds, by path.
// A text that is not a pin — never one this package built — carries no fields.
func proposalOf(e adoption.Entry, handleField string) domain.Proposal {
	proposal := domain.Proposal{
		Kind:        domain.ProposalKind(e.Kind),
		Handle:      e.Handle,
		Text:        e.Text,
		Fingerprint: adoption.Fingerprint(e),
	}
	fields, files, ok := splitPin(e.Text)
	if !ok {
		return proposal
	}
	labels := slices.Collect(maps.Keys(fields))
	slices.Sort(labels)
	slices.SortStableFunc(labels, func(a, b string) int { return fieldRank(a) - fieldRank(b) })
	for _, label := range labels {
		if label != handleField {
			proposal.Fields = append(proposal.Fields,
				domain.ProposalField{Label: label, Values: fieldValues(fields[label])})
		}
	}
	proposal.Files = slices.Sorted(maps.Keys(files))
	return proposal
}

// fieldValues spells a pinned field's canonical value as the pane draws it: a string as itself, a
// list element by element, a mapping as one `key: value` per entry sorted by key, and anything
// else as its canonical JSON.
func fieldValues(raw json.RawMessage) []string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []string{text}
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return nonNilList(list)
	}
	var mapping map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		values := make([]string, 0, len(mapping))
		for _, key := range slices.Sorted(maps.Keys(mapping)) {
			values = append(values, key+": "+mapping[key])
		}
		return values
	}
	return []string{string(raw)}
}
