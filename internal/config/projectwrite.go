package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/adoption"
)

// The Project config writer (ADR 0096 §4). apogee writes `<Project root>/.apogee/config.yaml` only
// on the user's answer, and it runs the same transaction every global write runs (configedit.go) —
// with three differences, each because the file lives in a repository the user may commit:
//
//   - An absent file starts as an EMPTY document, never the global starter template: a project
//     states only what it sets, and a template's commented-out global keys would read, to a
//     teammate, as settings the project chose.
//   - Nothing but the config lands in the repository. The transaction's lock lives under the
//     apogee home beside the root's adoption record ([adoption.ConfigLockPath]), no `.gitignore`
//     is written, and an edit that turns out to have nothing to write takes back the `.apogee/`
//     and the empty file it made for it.
//   - The file is written only where it is. A `.apogee/` or a `config.yaml` that is a symlink, or a
//     file that resolves outside the Project root, is refused rather than followed: a committed link
//     could otherwise steer apogee's write at any file the user can write.

const (
	// projectDirPerm and projectFilePerm are the modes a first write creates `.apogee/` and its
	// config with — an ordinary repository folder and file (the umask applies). The file holds no
	// secret: servers and API keys are global-only and never read from a project. A later write
	// preserves whatever mode the file has (writeConfigAtomically).
	projectDirPerm  os.FileMode = 0o755
	projectFilePerm os.FileMode = 0o644
)

// editProject runs one config-write transaction against the Project config of projectRoot, holding
// a lock kept under workspacesDir (the binary's `~/.apogee/workspaces`). The file and its folder are
// created on the first write; a splice that finds nothing to do, and every refusal, leaves the
// project exactly as it was.
//
// Errors: a refusal when projectRoot is empty or is the user's home, when `.apogee/` or the config
// is a symlink, not a folder or regular file, or resolves outside the root; a refusal naming the
// config when the lock is still held after its wait; the transaction's own errors (editHeld).
func editProject(projectRoot, workspacesDir string, splice editSplice, verify editVerify) error {
	path := projectFilePath(projectRoot)
	if path == "" {
		return errors.New("apogee: cannot write the project config: no Project root is known")
	}
	lockPath, err := adoption.ConfigLockPath(workspacesDir, projectRoot)
	if err != nil {
		return err
	}
	release, err := lockConfigAt(lockPath, path, configLockTimeout)
	if err != nil {
		return err
	}
	defer release()

	made, err := prepareProjectFile(projectRoot, path)
	if err != nil {
		return err
	}
	wrote, err := editHeld(path, readProjectFile, splice, verify)
	if !wrote {
		made.undo()
	}
	return err
}

// readProjectFile is the Project config's read step: the file prepareProjectFile has already
// checked and, when absent, created empty — so the edit starts from the bytes on disk, or from an
// empty document, and never from a seeded template.
func readProjectFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("apogee: read project config %q: %w", path, err)
	}
	return data, nil
}

// projectCreated is what prepareProjectFile made, so an edit that wrote nothing can take it back.
type projectCreated struct {
	dir  string // `.apogee/`, when this write created it
	file string // the empty config, when this write created it
}

// undo removes what the preparation made, file first, and leaves anything else alone: a folder
// that has gained another entry meanwhile is not empty and stays.
func (c projectCreated) undo() {
	if c.file != "" {
		_ = os.Remove(c.file)
	}
	if c.dir != "" {
		_ = os.Remove(c.dir)
	}
}

// prepareProjectFile makes the Project config at path ready for the transaction, under its lock:
// `.apogee/` and an empty config are created when absent, and both are checked to be what they
// claim — a real folder and a regular file, neither a symlink — inside the Project root, which must
// not be the user's home (whose `.apogee/` is the global config).
func prepareProjectFile(projectRoot, path string) (projectCreated, error) {
	root, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return projectCreated{}, fmt.Errorf("apogee: cannot write the project config: resolve Project root %q: %w",
			projectRoot, err)
	}
	if isHome(root) {
		return projectCreated{}, fmt.Errorf("apogee: cannot write the project config: %q is the home folder, "+
			"whose .apogee/ is the global config", projectRoot)
	}
	var made projectCreated
	dir := filepath.Dir(path)
	created, err := ensureProjectEntry(dir, func() error { return os.Mkdir(dir, projectDirPerm) }, fs.ModeDir)
	if err != nil {
		return projectCreated{}, err
	}
	if created {
		made.dir = dir
	}
	created, err = ensureProjectEntry(path, func() error { return createEmptyFile(path) }, 0)
	if err != nil {
		made.undo()
		return projectCreated{}, err
	}
	if created {
		made.file = path
	}
	if err := insideRoot(root, path); err != nil {
		made.undo()
		return projectCreated{}, err
	}
	return made, nil
}

// ensureProjectEntry makes sure the entry at path exists as the type want names — a folder
// (fs.ModeDir) or a regular file (0) — creating it with create when it is absent, and reports
// whether it did. An entry that is a symlink, or of another type, is refused: the writer follows no
// link the repository carries.
func ensureProjectEntry(path string, create func() error, want fs.FileMode) (bool, error) {
	created := false
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		switch createErr := create(); {
		case createErr == nil:
			created = true
		case !errors.Is(createErr, fs.ErrExist): // another writer making it first is not a failure
			return false, fmt.Errorf("apogee: cannot write the project config: create %q: %w", path, createErr)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return false, fmt.Errorf("apogee: cannot write the project config: stat %q: %w", path, err)
	}
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		return false, fmt.Errorf("apogee: cannot write the project config: %q is a symlink; "+
			"apogee writes the project config only where it is", path)
	case mode.Type() != want:
		return false, fmt.Errorf("apogee: cannot write the project config: %q is not a %s", path, entryNoun(want))
	}
	return created, nil
}

// entryNoun names the type ensureProjectEntry wanted, for its refusal.
func entryNoun(want fs.FileMode) string {
	if want == fs.ModeDir {
		return "folder"
	}
	return "regular file"
}

// createEmptyFile creates path empty with projectFilePerm, failing when it already exists.
func createEmptyFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, projectFilePerm)
	if err != nil {
		return err
	}
	return f.Close()
}

// insideRoot refuses a config whose resolved location is not under the resolved Project root — the
// belt to the per-entry symlink checks' braces, should the folders above `.apogee/` move under the
// write.
func insideRoot(root, path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("apogee: cannot write the project config: resolve %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("apogee: cannot write the project config: %q resolves to %q, outside the Project root %q",
			path, resolved, root)
	}
	return nil
}

// isHome reports whether the resolved folder root is the user's home. A home that cannot be found
// guards nothing; the Project root resolver never answers it either (internal/projectroot).
func isHome(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	return filepath.Clean(home) == root
}

// ----------------------------------------------------------------------------
// Project rules — the `allow:` lists the writer above edits
// ----------------------------------------------------------------------------

// AddProjectAllowRule appends text to the `allow.<kind>` list of the Project config of projectRoot
// and returns the file it wrote — the "Always in this project…" answer (ADR 0096 §4). It writes the
// rule and nothing else: whether the rule is live is the adoption record's question, which the
// caller answers next (internal/adoption). A list that already holds text is a confirmation, and
// nothing is written.
//
// Errors: a refusal for an unknown kind, a blank text or one that spans lines; the writer's own
// refusals and the transaction's errors (editProject); a refusal naming the part of the file the
// splice could not read — a flow-style `allow:` block or list, or a key holding something else.
func AddProjectAllowRule(projectRoot, workspacesDir string, kind AllowKind, text string) (string, error) {
	if err := checkAllowRule(kind, text); err != nil {
		return "", err
	}
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		if slices.Contains(allowList(before.Allow, kind), text) {
			return nil, nil
		}
		return spliceAllowRuleIn(data, kind, text)
	}
	verify := func(before, after fileConfig, _ []byte) error {
		want := append(slices.Clone(allowList(before.Allow, kind)), text)
		return verifyAllowList(before, after, kind, want, "add the rule by hand")
	}
	return projectFilePath(projectRoot), editProject(projectRoot, workspacesDir, splice, verify)
}

// RemoveProjectAllowRule takes text out of the `allow.<kind>` list of the Project config of
// projectRoot and returns the file it wrote; a list that does not hold it is left alone. Only the
// item's own line goes — the keys above it stay, so an emptied list reads as a bare `terminal:`.
//
// Errors: as [AddProjectAllowRule], plus a refusal for an item that is not one plain line.
func RemoveProjectAllowRule(projectRoot, workspacesDir string, kind AllowKind, text string) (string, error) {
	if err := checkAllowRule(kind, text); err != nil {
		return "", err
	}
	at := -1
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		at = slices.Index(allowList(before.Allow, kind), text)
		if at < 0 {
			return nil, nil
		}
		return spliceAllowRuleOut(data, kind, at)
	}
	verify := func(before, after fileConfig, _ []byte) error {
		want := slices.Delete(slices.Clone(allowList(before.Allow, kind)), at, at+1)
		return verifyAllowList(before, after, kind, want, "remove the rule by hand")
	}
	return projectFilePath(projectRoot), editProject(projectRoot, workspacesDir, splice, verify)
}

// checkAllowRule refuses what no rule can be before the file is opened: a kind the `allow:` key
// does not have, a blank rule — which would match every command — and a rule that spans lines.
func checkAllowRule(kind AllowKind, text string) error {
	switch {
	case kind != AllowTerminal && kind != AllowMCPServers:
		return fmt.Errorf("apogee: allow has no %q list; a rule sits under terminal or mcp-servers", kind)
	case strings.TrimSpace(text) == "":
		return errors.New("apogee: an allow rule names what it allows; this one is empty")
	case strings.ContainsAny(text, "\r\n"):
		return errors.New("apogee: an allow rule is one line; this one spans several")
	}
	return nil
}

// allowList is the list of kind in an `allow:` block as the file spells it, nil for no block.
func allowList(ac *allowConfig, kind AllowKind) []string {
	if ac == nil {
		return nil
	}
	if kind == AllowMCPServers {
		return ac.MCPServers
	}
	return ac.Terminal
}

// verifyAllowList is the gate both rule edits pass: the list of kind must read exactly want, the
// other list must not have moved, and nothing outside `allow:` may have changed.
func verifyAllowList(before, after fileConfig, kind AllowKind, want []string, byHand string) error {
	other := AllowTerminal
	if kind == AllowTerminal {
		other = AllowMCPServers
	}
	if !slices.Equal(allowList(after.Allow, kind), want) ||
		!slices.Equal(allowList(after.Allow, other), allowList(before.Allow, other)) ||
		!sameApartFrom(before, after, "allow") {
		return fmt.Errorf("the edit would have changed more than the allow.%s list; %s", kind, byHand)
	}
	return nil
}

// spliceAllowRuleIn inserts text as the last item of `allow.<kind>`. The shapes it meets, outermost
// first: no `allow:` key — append a block; a bare `allow:` — start the mapping under it; a mapping
// without the list — add the list after its last line; a bare list key — start the list; a list
// with items — append one at their indentation. A flow-style block or list has no line to add to,
// and is refused rather than rewritten.
func spliceAllowRuleIn(data []byte, kind AllowKind, text string) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	allowKey, allow := mappingEntry(root, "allow")
	switch {
	case allowKey == nil:
		item, err := renderAllowItem(text, 2*listIndent)
		if err != nil {
			return nil, err
		}
		block := []string{"allow:", indentLine(listIndent, string(kind)+":")}
		return joinConfigLines(appendBlock(lines, append(block, item...))), nil
	case isNullNode(allow):
		return insertAllowList(lines, kind, text, listIndent, allowKey.Line)
	case allow.Kind != yaml.MappingNode || allow.Style&yaml.FlowStyle != 0:
		return nil, errors.New("its allow: key is not a block of lists; add the rule by hand")
	}
	listKey, list := mappingEntry(allow, string(kind))
	subject := "its allow." + string(kind) + ": list"
	switch {
	case listKey == nil:
		return insertAllowList(lines, kind, text, allow.Column-1, maxNodeLine(allow))
	case isNullNode(list):
		item, err := renderAllowItem(text, listKey.Column-1+listIndent)
		if err != nil {
			return nil, err
		}
		return insertAt(lines, item, listKey.Line, subject)
	case list.Kind != yaml.SequenceNode || list.Style&yaml.FlowStyle != 0 || len(list.Content) == 0:
		return nil, fmt.Errorf("%s is not a block list; add the rule by hand", subject)
	}
	item, err := renderAllowItem(text, list.Column-1)
	if err != nil {
		return nil, err
	}
	return insertAt(lines, item, maxNodeLine(list.Content[len(list.Content)-1]), subject)
}

// insertAllowList inserts a `<kind>:` key at indent, holding the one item text, after line at.
func insertAllowList(lines []string, kind AllowKind, text string, indent, at int) ([]byte, error) {
	item, err := renderAllowItem(text, indent+listIndent)
	if err != nil {
		return nil, err
	}
	insert := append([]string{indentLine(indent, string(kind)+":")}, item...)
	return insertAt(lines, insert, at, "its allow: key")
}

// renderAllowItem renders text as one list item through the YAML marshaller — which owns the
// quoting, so no rule can smuggle a syntax break into the file — indented to indent.
func renderAllowItem(text string, indent int) ([]string, error) {
	out, err := yaml.Marshal([]string{text})
	if err != nil {
		return nil, fmt.Errorf("render the allow rule: %w", err)
	}
	rendered := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, l := range rendered {
		rendered[i] = indentLine(indent, l)
	}
	return rendered, nil
}

// spliceAllowRuleOut removes the item at index at of `allow.<kind>` — its one line. An item that is
// not a plain scalar on the line its dash opens is refused: removing that line alone would leave
// the rest of it behind.
func spliceAllowRuleOut(data []byte, kind AllowKind, at int) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	subject := "its allow." + string(kind) + ": list"
	_, allow := mappingEntry(root, "allow")
	_, list := mappingEntry(allow, string(kind))
	if list == nil || list.Kind != yaml.SequenceNode || list.Style&yaml.FlowStyle != 0 || at >= len(list.Content) {
		return nil, fmt.Errorf("%s is not a block list; remove the rule by hand", subject)
	}
	item := list.Content[at]
	if item.Kind != yaml.ScalarNode || item.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return nil, fmt.Errorf("%s holds the rule as more than one line; remove the rule by hand", subject)
	}
	lines := SplitConfigLines(data)
	line := item.Line
	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("%s points at line %d, which is outside the file", subject, line)
	}
	return joinConfigLines(slices.Delete(slices.Clone(lines), line-1, line)), nil
}
