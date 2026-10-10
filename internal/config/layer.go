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
// file, then sorted by the user's adoption record (allow.go), and only the adopted ones grant.
//
// The merge happens on the YAML node tree, before the decode, so the file pass (applyFile) reads
// one document exactly as it reads the global file alone. The project file is a guest: it never
// goes through the legacy migration, a defect in it — a parse error, a value its key refuses —
// degrades to one notice naming its path and the layer is skipped, and it can never stop a start.

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
// rules its `allow:` states, which never enter it either: they wait on the adoption record.
type projectLayer struct {
	path         string
	entries      []projectEntry
	dangerousAdd []domain.DangerousRule
	allow        []adoption.Entry
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
	fc, stated, err := parseLayeredConfig(globalPath, projectRoot, readFile, notify, false)
	if err != nil {
		return Options{}, err
	}
	var o Options
	if err := applyFile(&o, fc); err != nil {
		return Options{}, err
	}
	o.ProjectKeys = stated.project
	o.DangerousRules.ProjectAdd = stated.projectDangerous
	o.AllowRules = stated.projectAllow.effective(o.AllowRules.Rules)
	return o, nil
}

// layerKeys is which registry paths each file of a layered load stated: global, the keys the global
// file writes a value for, and project, the paths the project layer contributed to (nil when no
// layer was merged). They are what tell a key's source apart once the files collapse into one
// value ([Options.SourceOf]). projectDangerous rides beside them: the project layer's
// dangerous-action additions, which the merge never sees and resolution lands on
// DangerousRuleSet.ProjectAdd after the file pass, and projectAllow, the project layer's Allow
// rules sorted by the adoption record, which resolution joins to the global rules after it.
type layerKeys struct {
	global           map[string]bool
	project          map[string]bool
	projectDangerous []domain.DangerousRule
	projectAllow     projectAllowRules
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
	layer, ok := readProjectLayer(projectFilePath(projectRoot), globalPath, readFile, notify)
	if !ok {
		return global, stated, nil
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
				stated.projectAllow = classifyProjectRules(layer.allow, layer.path,
					workspacesDirOf(globalPath), projectRoot, notify)
				if len(stated.projectAllow.adopted) > 0 {
					projectKeys[allowPath] = true
				}
				stated.project = projectKeys
				return fc, stated, nil
			}
		}
	}
	// Unreachable for a project file that passed its checks alone, which is every one that gets
	// here — but a layer must never be the reason a start fails, so it degrades like the rest.
	notify(fmt.Sprintf(projectSkippedNotice, layer.path, sansPrefix{err}.Error()))
	return global, stated, nil
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
// cannot also cost it the keys it honours.
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
		allow: allowEntries(checked.AllowRules.Rules)}
	return layer, len(layer.entries) > 0 || len(layer.dangerousAdd) > 0 || len(layer.allow) > 0
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
// the layer refused; the block's `add:` goes into checked alone and never into entries.
type projectWalk struct {
	entries       []projectEntry
	dropped       []string
	droppedRemove bool
	checked       *yaml.Node
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
