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
	"github.com/airiclenz/apogee/internal/reactions"
	"gopkg.in/yaml.v3"
)

// The Project config's execution keys — repo MCP servers and reactions (ADR 0104)
//
// A Project config's `mcp-servers:` and `reactions:` entries are granting: a cloned repo must not
// launch a program, dial an endpoint or run a command because it says so. So the layer carries them
// apart from the node merge (layer.go). Each entry is decoded and checked ALONE — a refused one is
// dropped with a notice naming it and never costs the rest of the file (ADR 0104 §7) — then pinned
// (execpin.go), and sorted by the user's adoption record together with the file's Allow rules, in
// one read of it. An entry whose name (server) or id (reaction) a global entry already uses is
// dropped before it is offered: the user's own entry wins (§5). Only an adopted entry joins the
// resolved servers or reactions, flagged Project — a stdio server that lists no `env-allowlist:`
// narrowed to the platform floor (§6), a reaction armed with the `on:`, `timeout:`, `enabled:` and
// `workspace:` the file states now, since its pin leaves those parameters out (§2); every one is
// reported on Options.Proposals with what its adoption pane shows.

// mcpServersPath is the `mcp-servers:` key's registry path, the granting key whose project entries
// the layer judges one by one.
const mcpServersPath = "mcp-servers"

// serverHandleField is the pinned field that names a server entry, which the pane carries in its
// title rather than its body.
const serverHandleField = "name"

// reactionsPath is the `reactions:` key's registry path, the granting key whose project entries
// the layer judges one by one.
const reactionsPath = "reactions"

// reactionHandleField is the pinned field that names a reaction entry, carried in the pane's title.
const reactionHandleField = "id"

// repoReactionRefusedNotice is the line a project `reactions:` entry that cannot be used costs: the
// project file, the entry, and why. The rest of the file still applies.
const repoReactionRefusedNotice = "apogee: project config %s: ignoring reactions entry %s: %s"

// repoReactionShadowedNotice is the line a project reaction with a global one's id costs: the
// user's own entry wins, and the project's is never offered (ADR 0104 §5).
const repoReactionShadowedNotice = "apogee: project config %s: ignoring reactions entry %q — " +
	"your global config defines a reaction of that id, and it wins"

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
// record, each list holding the servers in file order and then the reactions in file order.
// Adopted entries are live — an adopted server is also in Options.MCPServers, an adopted reaction
// in Options.Reactions; Proposed ones wait on an answer, each carrying what changed since its
// handle was last adopted; Rejected ones the user turned down. Only Adopted grants anything. The
// zero value is no entries.
type ProjectProposals struct {
	Adopted  []domain.Proposal
	Proposed []domain.Proposal
	Rejected []domain.Proposal
}

// Clone returns p with each of its three lists copied element by element (domain.Proposal.Clone),
// so the copy shares no backing array with p at any depth. A nil list stays nil.
func (p ProjectProposals) Clone() ProjectProposals {
	p.Adopted = cloneProposals(p.Adopted)
	p.Proposed = cloneProposals(p.Proposed)
	p.Rejected = cloneProposals(p.Rejected)
	return p
}

// cloneProposals is ProjectProposals.Clone's per-list copy: nil stays nil, and every element is
// deep-copied by its own Clone.
func cloneProposals(list []domain.Proposal) []domain.Proposal {
	if list == nil {
		return nil
	}
	out := make([]domain.Proposal, len(list))
	for i, proposal := range list {
		out[i] = proposal.Clone()
	}
	return out
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

// concatProposals is first's lists followed by second's, each built fresh.
func concatProposals(first, second ProjectProposals) ProjectProposals {
	return ProjectProposals{
		Adopted:  slices.Concat(first.Adopted, second.Adopted),
		Proposed: slices.Concat(first.Proposed, second.Proposed),
		Rejected: slices.Concat(first.Rejected, second.Rejected),
	}
}

// projectReactionSet is a Project config's reactions resolved by Adoption: the Reactions the
// adopted entries arm, and every entry as a Proposal.
type projectReactionSet struct {
	adopted   []domain.Reaction
	proposals ProjectProposals
}

// effective is the resolved `reactions:` key: the global Reactions, then the adopted project ones.
// It builds a fresh slice whenever it adds any, so a base resolution's list is never shared.
func (p projectReactionSet) effective(global []domain.Reaction) []domain.Reaction {
	if len(p.adopted) == 0 {
		return global
	}
	return slices.Concat(global, p.adopted)
}

// globalReactions is list less every Reaction the Project config contributed — what a project's
// adopted reactions join, whatever layer the resolution they came from took. Built fresh.
func globalReactions(list []domain.Reaction) []domain.Reaction {
	var global []domain.Reaction
	for _, r := range list {
		if !r.Project {
			global = append(global, r)
		}
	}
	return global
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

// repoReactionConfigs decodes the project file's `reactions:` value (nil when the file states none)
// one entry at a time, answering the entries that passed in file order. An entry that does not
// decode, has no id, or repeats an earlier entry's id is dropped with one notice naming it; a value
// that is not a list drops every entry with one notice. Neither ever skips the layer. What the
// entry arms is judged later, against the Project root (repoArmed).
func repoReactionConfigs(value *yaml.Node, path string, notify func(string)) []reactionConfig {
	if value == nil {
		return nil
	}
	if value.Kind != yaml.SequenceNode {
		notify(fmt.Sprintf(projectSkippedKeyNotice, path, reactionsPath, "it is not a list of reactions"))
		return nil
	}
	var entries []reactionConfig
	seen := make(map[string]bool, len(value.Content))
	for i, item := range value.Content {
		var r reactionConfig
		label := fmt.Sprintf("#%d", i+1)
		err := resolveAlias(item).Decode(&r)
		id := strings.TrimSpace(r.ID)
		if err == nil {
			if id != "" {
				label = fmt.Sprintf("%q", id)
			}
			err = checkRepoReactionID(id, seen)
		}
		if err != nil {
			notify(fmt.Sprintf(repoReactionRefusedNotice, path, label, sansPrefix{err}.Error()))
			continue
		}
		seen[id] = true
		entries = append(entries, r)
	}
	return entries
}

// checkRepoReactionID refuses a repo reaction whose id is empty or repeats one in seen.
func checkRepoReactionID(id string, seen map[string]bool) error {
	switch {
	case id == "":
		return errors.New("it has no id")
	case seen[id]:
		return errors.New("an earlier entry already uses that id")
	}
	return nil
}

// repoArmed maps the repo reaction r against projectRoot (entryReactionsIn) and runs on it alone
// the checks each lane's owner holds for the global block (validateReactionBlocks), answering the
// Reactions it arms — none while it is parked by `enabled: false`, though a parked entry is still
// checked, so it is never offered for adoption in a shape that could not run once switched on.
func (r reactionConfig) repoArmed(projectRoot string) ([]domain.Reaction, error) {
	mapped, err := r.entryReactionsIn(projectRoot)
	if err != nil {
		return nil, err
	}
	observe, sync := domain.SplitLanes(mapped)
	if err := reactions.ValidateAll(observe); err != nil {
		return nil, err
	}
	if err := (domain.Generation{Sync: sync}).Validate(); err != nil {
		return nil, err
	}
	if r.Enabled != nil && !*r.Enabled {
		return nil, nil
	}
	return mapped, nil
}

// pinnedReaction is a repo reaction with the Reactions it arms and the Adoption entry its pin
// builds.
type pinnedReaction struct {
	armed []domain.Reaction
	entry adoption.Entry
}

// pinRepoReactions maps and pins each of the layer's reactions that no global reaction shadows, in
// file order. One that cannot be mapped — a `workspace:` outside the Project root among the ways —
// or pinned is dropped with a notice naming it.
func pinRepoReactions(layer projectLayer, global []reactionConfig, projectRoot string,
	notify func(string)) []pinnedReaction {
	globalIDs := make(map[string]bool, len(global))
	for _, r := range global {
		globalIDs[strings.TrimSpace(r.ID)] = true
	}
	var pinned []pinnedReaction
	for _, r := range layer.reactions {
		id := strings.TrimSpace(r.ID)
		if globalIDs[id] {
			notify(fmt.Sprintf(repoReactionShadowedNotice, layer.path, id))
			continue
		}
		armed, err := r.repoArmed(projectRoot)
		var entry adoption.Entry
		if err == nil {
			entry, err = r.execPin(projectRoot)
		}
		if err != nil {
			notify(fmt.Sprintf(repoReactionRefusedNotice, layer.path, fmt.Sprintf("%q", id),
				reactionRefusal(id, err)))
			continue
		}
		pinned = append(pinned, pinnedReaction{armed: armed, entry: entry})
	}
	return pinned
}

// reactionRefusal spells err for the refused-entry notice, which names the entry already: without
// the `reaction "id": ` its mapping prefixes, or apogee's own prefix.
func reactionRefusal(id string, err error) string {
	return strings.TrimPrefix(sansPrefix{err}.Error(), fmt.Sprintf("reaction %q: ", id))
}

// resolveRepoReactions turns the reactions' classification into the Reactions the adopted ones arm
// and the Proposals of every one. store is as resolveRepoServers takes it. Each adopted Reaction
// carries its adoption pin (adoptedPin over projectRoot and the adopted text), which its spawn
// checks before it runs (ADR 0104 §4).
func resolveRepoReactions(pinned []pinnedReaction, sorted adoption.Classification,
	store *adoption.Store, projectRoot string) projectReactionSet {
	byID := make(map[string][]domain.Reaction, len(pinned))
	for _, r := range pinned {
		byID[r.entry.Handle] = r.armed
	}
	resolved := projectReactionSet{proposals: kindProposals(sorted, ReactionAdoptionKind, reactionHandleField, store)}
	for _, e := range kindEntries(sorted.Adopted, ReactionAdoptionKind) {
		for _, armed := range byID[e.Handle] {
			armed.Pin = adoptedPin{root: projectRoot, text: e.Text}
			resolved.adopted = append(resolved.adopted, armed)
		}
	}
	return resolved
}

// pinnedServer is a repo server with the Adoption entry its pin builds.
type pinnedServer struct {
	config mcpServerConfig
	entry  adoption.Entry
}

// classifyProjectLayer sorts the layer's Allow rules, MCP servers and reactions by the adoption
// record under workspacesDir, in one read of it. A server or reaction named like one of global's —
// the global file's — is dropped with a notice before it is pinned, and so is one whose pin cannot
// be built (a named project file that cannot be read), since what cannot be pinned cannot be shown
// to stay adopted.
func classifyProjectLayer(layer projectLayer, global fileConfig, workspacesDir, projectRoot string,
	notify func(string)) (projectAllowRules, projectServers, projectReactionSet) {
	servers := pinRepoServers(layer, global.MCPServers, projectRoot, notify)
	pinned := pinRepoReactions(layer, global.Reactions, projectRoot, notify)
	entries := slices.Clone(layer.allow)
	for _, s := range servers {
		entries = append(entries, s.entry)
	}
	for _, r := range pinned {
		entries = append(entries, r.entry)
	}
	sorted, store := classifyProjectEntries(entries, layer.path, workspacesDir, projectRoot, notify)
	return projectAllowRulesOf(sorted), resolveRepoServers(servers, sorted, store, projectRoot),
		resolveRepoReactions(pinned, sorted, store, projectRoot)
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
// no change reason is named. Each adopted server carries its adoption pin (adoptedPin over
// projectRoot and the adopted text), which a stdio server's launch checks before it runs (ADR 0104
// §4).
func resolveRepoServers(servers []pinnedServer, sorted adoption.Classification,
	store *adoption.Store, projectRoot string) projectServers {
	byName := make(map[string]mcpServerConfig, len(servers))
	for _, s := range servers {
		byName[s.entry.Handle] = s.config
	}
	resolved := projectServers{proposals: kindProposals(sorted, MCPServerAdoptionKind, serverHandleField, store)}
	for _, e := range kindEntries(sorted.Adopted, MCPServerAdoptionKind) {
		server := byName[e.Handle].repoServerConfig()
		server.Pin = adoptedPin{root: projectRoot, text: e.Text}
		resolved.adopted = append(resolved.adopted, server)
	}
	return resolved
}

// kindProposals is the Proposals of sorted's entries of one structured kind, each named by
// handleField, a proposed one carrying its change reason read from store.
func kindProposals(sorted adoption.Classification, kind, handleField string,
	store *adoption.Store) ProjectProposals {
	var proposals ProjectProposals
	for _, e := range kindEntries(sorted.Adopted, kind) {
		proposals.Adopted = append(proposals.Adopted, proposalOf(e, handleField))
	}
	for _, e := range kindEntries(sorted.Proposed, kind) {
		proposal := proposalOf(e, handleField)
		proposal.Changed = changedSinceAdopted(store, e)
		proposals.Proposed = append(proposals.Proposed, proposal)
	}
	for _, e := range kindEntries(sorted.Rejected, kind) {
		proposals.Rejected = append(proposals.Rejected, proposalOf(e, handleField))
	}
	return proposals
}

// kindEntries is the entries of entries of one Adoption kind, in order.
func kindEntries(entries []adoption.Entry, kind string) []adoption.Entry {
	var matched []adoption.Entry
	for _, e := range entries {
		if e.Kind == kind {
			matched = append(matched, e)
		}
	}
	return matched
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
			proposal.Fields = append(proposal.Fields, proposalField(label, fields[label]))
		}
	}
	proposal.Files = slices.Sorted(maps.Keys(files))
	return proposal
}

// webhookFieldOrder is the order the pane hangs a webhook action's own fields in: where it posts,
// then what it sends.
var webhookFieldOrder = []string{"url", "headers", "headers-env"}

// proposalField spells one pinned field's canonical value as the pane draws it, with its shape: a
// string as itself, a list element by element, a mapping of strings as one `key: value` per entry
// sorted by key, a mapping of anything else — a webhook action — as a group of its own fields in
// webhookFieldOrder, and anything else as its canonical JSON on one line.
func proposalField(label string, raw json.RawMessage) domain.ProposalField {
	field := domain.ProposalField{Label: label}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		field.Values = []string{text}
		return field
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		field.Shape, field.Values = domain.ProposalList, nonNilList(list)
		return field
	}
	var mapping map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		field.Shape = domain.ProposalMap
		for _, key := range slices.Sorted(maps.Keys(mapping)) {
			field.Values = append(field.Values, key+": "+mapping[key])
		}
		return field
	}
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err == nil {
		field.Shape = domain.ProposalGroup
		keys := slices.Sorted(maps.Keys(group))
		slices.SortStableFunc(keys, func(a, b string) int {
			return rankIn(webhookFieldOrder, a) - rankIn(webhookFieldOrder, b)
		})
		for _, key := range keys {
			field.Fields = append(field.Fields, proposalField(key, group[key]))
		}
		return field
	}
	field.Values = []string{string(raw)}
	return field
}
