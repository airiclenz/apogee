package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/projectroot"
	"gopkg.in/yaml.v3"
)

// The Project config layer
//
// A committable `<Project root>/.apogee/config.yaml` is layered over the global config before the
// environment and the flags (ADR 0096 §6). What the project file may do with a key is the key's
// registry Class: a project-param key's value outranks the global one, a tighten-only list is the
// union of both files' entries, and a global-only key is dropped with one notice naming each. The
// one tighten-only key that is not a list, `dangerous-rules:`, is carried apart from the merge:
// its `add:` reaches the security seam as the project's additions and its `remove:` is dropped with
// a notice. A granting key is never layered here — its project entries are live only by Adoption, so
// they are carried apart from this merge: `allow:`'s rules are checked alone with the rest of the
// file, `mcp-servers:` and `reactions:` entries one by one (execlayer.go), then all are sorted by
// the user's adoption record (allow.go), and only the adopted ones grant.
//
// The merge happens on the YAML node tree, before the decode, so the file pass (applyFile) reads
// one document exactly as it reads the global file alone. The project file is a guest: it never
// goes through the legacy migration, a defect in it — a parse error, a value its key refuses —
// degrades to one notice naming its path and the layer is skipped, and it can never stop a start.
//
// A Driver whose sessions run in many workspaces resolves the global file alone at its start and
// layers each workspace's project later: it keeps the global document it read then
// ([GlobalDocument]) and overlays a project onto that resolution ([OverlayProjectLayer]) through
// the same merge, so the global file is read once and every project file fresh.

// projectConfigDirName and projectConfigFileName spell where a Project root keeps its config.
const (
	projectConfigDirName  = ".apogee"
	projectConfigFileName = "config.yaml"
)

// projectSkippedNotice is the one line a project file that cannot be layered costs: its path, why,
// and that the session runs on the global config alone.
const projectSkippedNotice = "apogee: project config %s: %s; the project layer is skipped"

// projectDangerousRemoveNotice is the line a project file's `dangerous-rules: {remove: …}` costs: a
// project may only add dangerous-action rules, never take a shipped one away (ADR 0096 §6).
const projectDangerousRemoveNotice = "apogee: project config %s: ignoring dangerous-rules.remove — " +
	"a project may only add rules; remove a shipped rule in the global config %s"

// dangerousRulesPath is the `dangerous-rules:` key's registry path, the one tighten-only key the
// layer carries apart from the node merge rather than unioning into it.
const dangerousRulesPath = "dangerous-rules"

// allowPath is the `allow:` key's registry path, the granting key whose project rules the layer
// carries apart to the adoption record.
const allowPath = "allow"

// projectRootNotice is the line a workspace whose Project root cannot be resolved costs.
const projectRootNotice = "apogee: project config: %v; the project layer is skipped"

// projectGlobalOnlyNotice is the one line naming every global-only key the project file stated:
// the project path, "key" or "keys", the quoted names, and the global file that does set them.
const projectGlobalOnlyNotice = "apogee: project config %s: ignoring global-only %s %s — set %s in " +
	"the global config %s"

// projectLayer is a project file that passed every check alone: its path, the entries the merge
// applies, in file order, the dangerous-action rules its `dangerous-rules: {add: …}` states, which
// never enter the merge — they reach the security seam as its project additions — and the Allow
// rules its `allow:` states, which never enter it either: they wait on the adoption record, as do
// the MCP servers its `mcp-servers:` states and the reactions its `reactions:` states, each already
// checked alone (execlayer.go).
type projectLayer struct {
	path         string
	entries      []projectEntry
	dangerousAdd []domain.DangerousRule
	allow        []adoption.Entry
	servers      []mcpServerConfig
	reactions    []reactionConfig
}

// projectEntry is one project-capable key the project file states: its registry path, its class
// and the value node as the project file spells it.
type projectEntry struct {
	path  string
	class KeyClass
	value *yaml.Node
}

// LoadLayeredConfig is [LoadFileConfig] with the Project config of projectRoot layered over the
// global file at globalPath: every key at the value the two files resolve to, with ProjectKeys
// naming what the project layer contributed. An empty projectRoot is no project layer, and so is a
// project file that is absent or is the global file itself. Like LoadFileConfig it never migrates
// and never writes; a defect in the GLOBAL file is the hard error LoadFileConfig returns, while a
// defect in the project file is a notice and the global file alone answers.
func LoadLayeredConfig(globalPath, projectRoot string, readFile func(string) ([]byte, error),
	notify func(string)) (Options, error) {
	o, _, err := LoadLayeredOffer(globalPath, projectRoot, readFile, notify)
	return o, err
}

// LoadLayeredOffer is [LoadLayeredConfig] answering, beside the options, the [ProjectOffer] the
// same read resolved — the proposed and rejected entries the options do not carry — built fresh, so
// it shares no backing array with them. An error answers the zero options and the zero offer.
func LoadLayeredOffer(globalPath, projectRoot string, readFile func(string) ([]byte, error),
	notify func(string)) (Options, ProjectOffer, error) {
	fc, stated, err := parseLayeredConfig(globalPath, projectRoot, readFile, notify, false)
	if err != nil {
		return Options{}, ProjectOffer{}, err
	}
	var o Options
	if err := applyFile(&o, fc); err != nil {
		return Options{}, ProjectOffer{}, err
	}
	o.ProjectKeys = stated.project
	o.DangerousRules.ProjectAdd = stated.projectDangerous
	o.AllowRules = stated.projectAllow.effective(o.AllowRules.Rules)
	o.MCPServers = stated.projectServers.effective(o.MCPServers)
	o.Reactions = stated.projectReactions.effective(o.Reactions)
	return o, stated.offer(), nil
}

// layerKeys is which registry paths each file of a layered load stated: global, the keys the global
// file writes a value for, and project, the paths the project layer contributed to (nil when no
// layer was merged). They are what tell a key's source apart once the files collapse into one
// value ([Options.SourceOf]). projectDangerous rides beside them: the project layer's
// dangerous-action additions, which the merge never sees and resolution lands on
// DangerousRuleSet.ProjectAdd after the file pass, and projectAllow, the project layer's Allow
// rules sorted by the adoption record, which resolution joins to the global rules after it, and
// projectServers and projectReactions, its MCP servers and reactions sorted the same way, whose
// adopted ones resolution joins to the global servers and reactions.
type layerKeys struct {
	global           map[string]bool
	project          map[string]bool
	projectDangerous []domain.DangerousRule
	projectAllow     projectAllowRules
	projectServers   projectServers
	projectReactions projectReactionSet
}

// offer is the layer's [ProjectOffer]: every structured entry it proposed, servers first and then
// reactions, and its Allow rules no answer adopts. Built fresh.
func (s layerKeys) offer() ProjectOffer {
	return ProjectOffer{
		Proposals: concatProposals(s.projectServers.proposals, s.projectReactions.proposals),
		Proposed:  s.projectAllow.proposed,
		Rejected:  s.projectAllow.rejected,
	}
}

// parseLayeredConfig is parseConfigFile for the global file with the Project config of projectRoot
// merged into it, answering the decoded schema and the keys each file stated. Only the global
// file's defects are errors.
func parseLayeredConfig(globalPath, projectRoot string, readFile func(string) ([]byte, error),
	notify func(string), mayMigrate bool) (fileConfig, layerKeys, error) {
	data, present, err := readConfigData(globalPath, readFile, notify, mayMigrate)
	if err != nil {
		return fileConfig{}, layerKeys{}, err
	}
	var global fileConfig
	var stated layerKeys
	if present {
		if global, err = decodeConfigData(globalPath, data, notify); err != nil {
			return fileConfig{}, layerKeys{}, err
		}
		stated.global = statedKeys(data)
	}
	if fc, ok := layerProject(data, globalPath, projectRoot, readFile, notify, &stated); ok {
		return fc, stated, nil
	}
	return global, stated, nil
}

// layerProject merges the Project config of projectRoot into the global document data (the
// migrated bytes of the global file at globalPath, nil when there is none) and answers the decoded
// merged schema, recording on stated what the layer contributed: its keys, its dangerous-action
// additions, and its Allow rules, MCP servers and reactions sorted by the adoption record. ok is false when there is no layer
// to take — no project file, one that states nothing, or one skipped with a notice — and stated is
// then left as it was. It is the one merge both a startup resolution and a later overlay
// ([OverlayProjectLayer]) run, so a project file means the same thing to both.
func layerProject(data []byte, globalPath, projectRoot string, readFile func(string) ([]byte, error),
	notify func(string), stated *layerKeys) (fileConfig, bool) {
	layer, ok := readProjectLayer(projectFilePath(projectRoot), globalPath, readFile, notify)
	if !ok {
		return fileConfig{}, false
	}
	merged, projectKeys, err := mergeProjectLayer(data, layer)
	if err == nil {
		var fc fileConfig
		if err = merged.Decode(&fc); err == nil {
			if err = checkFileConfig(&fc); err == nil {
				if len(layer.dangerousAdd) > 0 {
					projectKeys[dangerousRulesPath] = true
					stated.projectDangerous = layer.dangerousAdd
				}
				stated.projectAllow, stated.projectServers, stated.projectReactions = classifyProjectLayer(
					layer, fc, workspacesDirOf(globalPath), projectRoot, notify)
				if len(stated.projectAllow.adopted) > 0 {
					projectKeys[allowPath] = true
				}
				if len(stated.projectServers.adopted) > 0 {
					projectKeys[mcpServersPath] = true
				}
				if len(stated.projectReactions.adopted) > 0 {
					projectKeys[reactionsPath] = true
				}
				stated.project = projectKeys
				return fc, true
			}
		}
	}
	// Unreachable for a project file that passed its checks alone, which is every one that gets
	// here — but a layer must never be the reason a start fails, so it degrades like the rest.
	notify(fmt.Sprintf(projectSkippedNotice, layer.path, sansPrefix{err}.Error()))
	return fileConfig{}, false
}

// GlobalDocument is the global config file as a Driver read it once at its start: its path and its
// bytes, already in the current shape. It is what [OverlayProjectLayer] merges each Project config
// into, so a Driver that layers many projects over one startup resolution — the daemon, once per
// Firing — never reads the global file again (ADR 0055). The zero value is no global file.
type GlobalDocument struct {
	path string
	data []byte
}

// ReadGlobalDocument reads the global config file of the apogee home configDir for later overlays.
// It never migrates: the startup resolution it follows has already folded a retired shape, so one
// still standing is refused with that resolution's error ([ErrRetiredShape]). An absent file is
// the empty document, not an error; a file that cannot be read is.
func ReadGlobalDocument(configDir string, readFile func(string) ([]byte, error),
	notify func(string)) (GlobalDocument, error) {
	path := FilePath(configDir)
	data, _, err := readConfigData(path, readFile, notify, false)
	if err != nil {
		return GlobalDocument{}, err
	}
	return GlobalDocument{path: path, data: data}, nil
}

// OverlayProjectLayer answers a copy of base — a resolution that took no project layer
// ([Options.GlobalConfigOnly]) over the global file global holds — with the Project config of
// projectRoot layered in as a startup resolution would have layered it (ADR 0096 §6). The project
// file is read fresh on every call; the global file never is.
//
// Every project-param and tighten-only key is resolved again from the global document with the
// project file merged into it, through the key's own file pass, so a tighten-only list is the union
// of both files and a block whose keys share one field (`context-files:`) keeps its global half. A
// key an environment variable or a flag set for this run ([Options.Overrides]) keeps base's value:
// the precedence stays default < global < project < env < flag. Global-only keys are dropped with
// one notice, a project's `allow:` rules, `mcp-servers:` entries and `reactions:` entries are
// sorted by the adoption record and only the adopted ones join base's global rules, servers and
// reactions (the offer reports them all), and ProjectKeys and DangerousRules.ProjectAdd say what
// the layer contributed. No project file, one that states nothing, or one that cannot be layered
// answers base itself — the last after one notice naming the file. Notices go to notify, which must
// be non-nil.
//
// base is never changed: every field the overlay writes is replaced by a freshly built value, so
// overlays of one base may run concurrently.
func OverlayProjectLayer(base Options, global GlobalDocument, projectRoot string,
	readFile func(string) ([]byte, error), notify func(string)) Options {
	overlaid, _ := OverlayProjectOffer(base, global, projectRoot, readFile, notify)
	return overlaid
}

// OverlayProjectOffer is [OverlayProjectLayer] answering, beside the overlaid options, the
// [ProjectOffer] the same read resolved — the proposed and rejected entries the options do not
// carry — built fresh, so it shares no backing array with them. When no project layer is
// taken — no project file, one that states nothing, or one that cannot be layered — the options are
// base and the offer is the zero value: there is no project to offer anything.
func OverlayProjectOffer(base Options, global GlobalDocument, projectRoot string,
	readFile func(string) ([]byte, error), notify func(string)) (Options, ProjectOffer) {
	var stated layerKeys
	fc, ok := layerProject(global.data, global.path, projectRoot, readFile, notify, &stated)
	if !ok {
		return base, ProjectOffer{}
	}
	overlaid := base
	if err := applyProjectRows(&overlaid, fc, base.Overrides); err != nil {
		notify(fmt.Sprintf(projectSkippedNotice, projectFilePath(projectRoot), sansPrefix{err}.Error()))
		return base, ProjectOffer{}
	}
	overlaid.ProjectKeys = stated.project
	overlaid.DangerousRules.ProjectAdd = stated.projectDangerous
	overlaid.AllowRules = stated.projectAllow.effective(globalAllowRules(base.AllowRules.Rules))
	overlaid.MCPServers = stated.projectServers.effective(globalMCPServers(base.MCPServers))
	overlaid.Reactions = stated.projectReactions.effective(globalReactions(base.Reactions))
	return overlaid, stated.offer()
}

// applyProjectRows is the file pass (applyFile) narrowed to the keys a Project config may set —
// project-param and tighten-only — less the ones overridden names as won by the environment or a
// flag. It runs the merged document's `context-files:` check startup runs, so a layer startup would
// refuse is refused here too.
func applyProjectRows(o *Options, fc fileConfig, overridden map[string]Source) error {
	for _, k := range KeyRegistry {
		if k.Class != ClassProjectParam && k.Class != ClassTightenOnly {
			continue
		}
		if _, won := overridden[k.Path]; won {
			continue
		}
		if err := k.fromFile(o, fc); err != nil {
			return err
		}
	}
	return fc.contextFiles().validate()
}

// globalAllowRules is the global layer's rules of rules, in order — what a project's adopted rules
// join, whatever layer the resolution they came from took.
func globalAllowRules(rules []AllowRule) []AllowRule {
	var global []AllowRule
	for _, r := range rules {
		if r.Layer == SourceGlobal {
			global = append(global, r)
		}
	}
	return global
}

// statedKeys is the registry paths the config document in data writes a value for — a bare `key:`
// states nothing, the projectWalk rule. data has already decoded, so a document that fails to
// parse here answers no keys rather than an error. YAML merge keys (`<<:`) are not followed: a
// key stated only through one reads as the default's.
func statedKeys(data []byte) map[string]bool {
	doc, err := Document(data)
	if err != nil || doc == nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	stated := make(map[string]bool)
	for _, k := range KeyRegistry {
		if value := valueAtPath(root, k.Path); value != nil && !isNullNode(value) {
			stated[k.Path] = true
		}
	}
	return stated
}

// ProjectFilePath is where the Project root projectRoot keeps its Project config, or "" for the
// empty root — no project layer. It is the one spelling of that path outside this package: the
// composition root watches the file the layered load reads, and the two must not drift apart.
func ProjectFilePath(projectRoot string) string { return projectFilePath(projectRoot) }

// projectFilePath is the Project config's path under projectRoot, or "" for the empty root that
// means no project layer.
func projectFilePath(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, projectConfigDirName, projectConfigFileName)
}

// layerProjectRoot is the Project root opts reads its project layer from: the pinned root when a
// running session pinned one (Options.ProjectRootPinned), else the root walked from the workspace.
func layerProjectRoot(opts *Options, notify func(string)) string {
	if opts.ProjectRootPinned {
		return opts.ProjectRoot
	}
	return resolveProjectRoot(opts.Workspace, notify)
}

// resolveProjectRoot is the Project root of workspace — the current directory when it is empty, as
// resolveRoots resolves it — or "" when there is no project layer to read. A workspace that cannot
// be read (a `--workspace` that does not exist yet) skips the layer with a notice rather than
// guessing at a root above a folder that is not there.
func resolveProjectRoot(workspace string, notify func(string)) string {
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			notify(fmt.Sprintf(projectRootNotice, fmt.Errorf("resolve working directory: %w", err)))
			return ""
		}
		workspace = cwd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		notify(fmt.Sprintf(projectRootNotice, fmt.Errorf("resolve workspace %s: %w", workspace, err)))
		return ""
	}
	if _, err := os.Stat(abs); err != nil {
		notify(fmt.Sprintf(projectRootNotice, err))
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // only the home guard is lost; the walk still stops at the git top-level
	}
	return projectroot.Resolve(abs, home)
}

// readProjectLayer reads the project file at path and checks it alone, answering the layer and
// whether there is one to merge. No file, the global file itself reached through another name, and
// a file that states nothing are no layer, silently; every other way the file can fail is one
// notice naming its path. Unknown keys are announced against the project path, and the global-only
// keys it states are named in one notice and dropped before the checks, so a key the layer ignores
// cannot also cost it the keys it honours. Its `mcp-servers:` and `reactions:` entries are taken
// out of that check and judged one by one once it passes (repoMCPServers, repoReactionConfigs), so
// a refused entry costs only itself.
func readProjectLayer(path, globalPath string, readFile func(string) ([]byte, error),
	notify func(string)) (projectLayer, bool) {
	if path == "" {
		return projectLayer{}, false
	}
	resolved, err := filepath.EvalSymlinks(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return projectLayer{}, false
	case err != nil:
		notify(fmt.Sprintf(projectSkippedNotice, path, err))
		return projectLayer{}, false
	case sameConfigFile(resolved, globalPath):
		return projectLayer{}, false
	}
	data, err := readFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return projectLayer{}, false
	case err != nil:
		notify(fmt.Sprintf(projectSkippedNotice, path, err))
		return projectLayer{}, false
	}
	for _, unknown := range unknownKeys(data) {
		notify(fmt.Sprintf(unknownKeyNotice, path, unknown.key, unknown.line))
	}
	doc, err := Document(data)
	if err != nil {
		notify(fmt.Sprintf(projectSkippedNotice, path, err))
		return projectLayer{}, false
	}
	if doc == nil || len(doc.Content) == 0 {
		return projectLayer{}, false
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		notify(fmt.Sprintf(projectSkippedNotice, path, "its top level is not a mapping of settings"))
		return projectLayer{}, false
	}
	walk := projectWalk{checked: newMapping()}
	walk.mapping(root, "")
	if len(walk.dropped) > 0 {
		notify(globalOnlyNotice(path, globalPath, walk.dropped))
	}
	if walk.droppedRemove {
		notify(fmt.Sprintf(projectDangerousRemoveNotice, path, globalPath))
	}
	checked, err := checkProjectLayer(walk.checked)
	if err != nil {
		notify(fmt.Sprintf(projectSkippedNotice, path, sansPrefix{err}.Error()))
		return projectLayer{}, false
	}
	layer := projectLayer{path: path, entries: walk.entries, dangerousAdd: checked.DangerousRules.Add,
		allow: allowEntries(checked.AllowRules.Rules), servers: repoMCPServers(walk.servers, path, notify),
		reactions: repoReactionConfigs(walk.reactions, path, notify)}
	return layer, len(layer.entries) > 0 || len(layer.dangerousAdd) > 0 || len(layer.allow) > 0 ||
		len(layer.servers) > 0 || len(layer.reactions) > 0
}

// sameConfigFile reports whether the symlink-resolved project file is the global config file —
// a Project root whose `.apogee/` is the apogee home, or links to it — so the global file is never
// layered over itself. The global path is compared symlink-resolved where it exists.
func sameConfigFile(resolvedProject, globalPath string) bool {
	if globalPath == "" {
		return false
	}
	global := filepath.Clean(globalPath)
	if resolved, err := filepath.EvalSymlinks(globalPath); err == nil {
		global = resolved
	}
	return resolvedProject == global
}

// checkProjectLayer decodes the project-capable part of the project file ALONE and runs the checks
// resolution would refuse it with — every row's file pass, and the context-files names — so a
// project value that would refuse the start skips the layer instead. What it answers is that part
// resolved alone, which is where the layer reads its dangerous-action additions and its Allow rules
// from: the same file pass that judged each rule has converted it.
func checkProjectLayer(checked *yaml.Node) (Options, error) {
	var fc fileConfig
	if err := checked.Decode(&fc); err != nil {
		return Options{}, err
	}
	var scratch Options
	if err := applyFile(&scratch, fc); err != nil {
		return Options{}, err
	}
	if err := fc.contextFiles().validate(); err != nil {
		return Options{}, err
	}
	return scratch, nil
}

// projectWalk sorts the keys of a project file by their registry class: entries the merge applies,
// global-only paths it drops, and checked — a document of everything the layer keeps, which is what
// is decoded alone before anything is merged. A granting key goes into checked alone, never into
// entries: its rules are judged with the rest of the file and then wait on the adoption record. droppedRemove records a `dangerous-rules: {remove: …}`
// the layer refused; the block's `add:` goes into checked alone and never into entries. servers is
// the `mcp-servers:` value and reactions the `reactions:` value, kept out of checked too: their
// entries are judged one by one, so one bad entry never skips the layer.
type projectWalk struct {
	entries       []projectEntry
	dropped       []string
	droppedRemove bool
	checked       *yaml.Node
	servers       *yaml.Node
	reactions     *yaml.Node
}

// mapping walks one mapping node of the project file at the dotted prefix. A key that is a registry
// row is kept or dropped by its class; a block that holds rows is descended into; anything else is
// a key the schema does not spell, which unknownKeys has already announced. A block written as
// something other than a mapping is kept as written in checked, so the decode reports its shape;
// a null block states nothing. YAML merge keys (`<<:`) are not layered.
func (w *projectWalk) mapping(node *yaml.Node, prefix string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, value := node.Content[i], resolveAlias(node.Content[i+1])
		if keyNode.Tag == "!!merge" {
			continue
		}
		path := joinKeyPath(prefix, keyNode.Value)
		if k, ok := LookupKey(path); ok {
			w.key(k, value)
			continue
		}
		if !holdsRegistryKeys(path) {
			continue
		}
		switch {
		case value.Kind == yaml.MappingNode:
			w.mapping(value, path)
		case !isNullNode(value):
			setAtPath(w.checked, path, value)
		}
	}
}

// key sorts one registry row the project file states. A null value — a bare `key:` — states
// nothing, so it neither overlays the global value nor counts as a dropped key.
func (w *projectWalk) key(k Key, value *yaml.Node) {
	if isNullNode(value) {
		return
	}
	if k.Path == dangerousRulesPath {
		w.dangerousRules(value)
		return
	}
	if k.Path == mcpServersPath {
		w.servers = value
		return
	}
	if k.Path == reactionsPath {
		w.reactions = value
		return
	}
	switch k.Class {
	case ClassProjectParam, ClassTightenOnly:
		w.entries = append(w.entries, projectEntry{path: k.Path, class: k.Class, value: value})
		setAtPath(w.checked, k.Path, value)
	case ClassGranting:
		setAtPath(w.checked, k.Path, value)
	case ClassGlobalOnly:
		w.dropped = append(w.dropped, k.Path)
	}
}

// dangerousRules sorts the project file's `dangerous-rules:` block: its `remove:` is dropped — a
// project may only add, so a cloned repo cannot dissolve the floor by naming a shipped id — and the
// rest of the block, its `add:`, is kept in checked so the project's rules are judged alone before
// the layer is taken. It is never an entry: the node merge would put the project's rules where the
// global file's stand, and the seam trusts the two differently (MergeDangerousRules). A block that
// is not a mapping is kept as written, so the decode reports its shape.
func (w *projectWalk) dangerousRules(value *yaml.Node) {
	if value.Kind != yaml.MappingNode {
		setAtPath(w.checked, dangerousRulesPath, value)
		return
	}
	kept := newMapping()
	for i := 0; i+1 < len(value.Content); i += 2 {
		key, item := value.Content[i], value.Content[i+1]
		if key.Value == "remove" {
			w.droppedRemove = w.droppedRemove || !isNullNode(resolveAlias(item))
			continue
		}
		kept.Content = append(kept.Content, key, item)
	}
	setAtPath(w.checked, dangerousRulesPath, kept)
}

// holdsRegistryKeys reports whether some registry row sits below the dotted path — whether path is
// a block of the schema (`ui`, `tools`) rather than a key of its own.
func holdsRegistryKeys(path string) bool {
	prefix := path + "."
	for _, k := range KeyRegistry {
		if strings.HasPrefix(k.Path, prefix) {
			return true
		}
	}
	return false
}

// globalOnlyNotice spells the one line naming every global-only key the project file stated.
func globalOnlyNotice(path, globalPath string, dropped []string) string {
	noun, pronoun := "key", "it"
	if len(dropped) > 1 {
		noun, pronoun = "keys", "them"
	}
	return fmt.Sprintf(projectGlobalOnlyNotice, path, noun, strings.Join(quoteAll(dropped), ", "),
		pronoun, globalPath)
}

// mergeProjectLayer layers the project entries into the global document (data, the migrated global
// bytes — nil or empty when there is no global file) and answers the merged root mapping with the
// registry paths the layer contributed to: every project-param key it states, and every
// tighten-only list it added an entry to that the global list lacks.
func mergeProjectLayer(data []byte, layer projectLayer) (*yaml.Node, map[string]bool, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, nil, err
	}
	root := newMapping()
	if doc != nil && len(doc.Content) > 0 {
		root = resolveAlias(doc.Content[0])
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil, errors.New("the global config's top level is not a mapping of settings")
	}
	contributed := make(map[string]bool, len(layer.entries))
	for _, e := range layer.entries {
		value := e.value
		if e.class == ClassTightenOnly {
			var added bool
			if value, added = unionSequence(valueAtPath(root, e.path), e.value); !added {
				continue
			}
		}
		setAtPath(root, e.path, value)
		contributed[e.path] = true
	}
	return root, contributed, nil
}

// unionSequence is a tighten-only list's merge: the global list's entries in their order, then each
// project entry the global list does not already hold. added reports whether the project list
// contributed anything. A global list that is absent or null is the project list as written. The
// result is a fresh node, so an anchored global list is never changed where else it is used.
func unionSequence(global, project *yaml.Node) (merged *yaml.Node, added bool) {
	if global == nil || global.Kind != yaml.SequenceNode || project.Kind != yaml.SequenceNode {
		return project, len(project.Content) > 0
	}
	union := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	held := make(map[string]bool, len(global.Content)+len(project.Content))
	for _, item := range global.Content {
		union.Content = append(union.Content, item)
		held[resolveAlias(item).Value] = true
	}
	for _, item := range project.Content {
		if value := resolveAlias(item).Value; !held[value] {
			union.Content = append(union.Content, item)
			held[value] = true
			added = true
		}
	}
	return union, added
}

// newMapping is an empty block mapping node.
func newMapping() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// valueAtPath is the value node at the dotted path below root, aliases followed, or nil.
func valueAtPath(root *yaml.Node, path string) *yaml.Node {
	node := root
	for _, segment := range strings.Split(path, ".") {
		value, ok := mappingValue(node, segment)
		if !ok {
			return nil
		}
		node = value
	}
	return node
}

// setAtPath sets the value at the dotted path below root, creating the blocks on the way. A block
// on the way that is null, or an alias, is replaced by a mapping of its own — a copy of the aliased
// one — so an anchored global block is never changed where else it is used.
func setAtPath(root *yaml.Node, path string, value *yaml.Node) {
	segments := strings.Split(path, ".")
	parent := root
	for _, segment := range segments[:len(segments)-1] {
		at := entryIndex(parent, segment)
		if at < 0 {
			child := newMapping()
			parent.Content = append(parent.Content, scalarKey(segment), child)
			parent = child
			continue
		}
		child := resolveAlias(parent.Content[at])
		block := newMapping()
		if child.Kind == yaml.MappingNode {
			block.Content = append(block.Content, child.Content...)
		}
		parent.Content[at] = block
		parent = block
	}
	last := segments[len(segments)-1]
	if at := entryIndex(parent, last); at >= 0 {
		parent.Content[at] = value
		return
	}
	parent.Content = append(parent.Content, scalarKey(last), value)
}

// entryIndex is the index in mapping.Content of the VALUE node under key, or -1.
func entryIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

// scalarKey is a mapping key node spelling name.
func scalarKey(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
}
