package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// bytesPerToken is the fixed ratio a split turns a token budget into bytes of source: the
// estimator's uncalibrated starting ratio (internal/context.DefaultCharsPerToken), since a part is
// sized before any child has run to calibrate it.
const bytesPerToken = 4

// splitBriefReserveTokens is the part of a child's window a split leaves free for everything that
// is not the part's own source: the system prompt, the tool menu and the rendered brief.
const splitBriefReserveTokens = 4096

// globStar is the `files:` segment that matches zero or more path segments.
const globStar = "**"

// skippedDirs are the directories a `files:` or `split:` walk never enters below its root: the
// version-control and build-output noise grep and find_files skip (internal/tools grepExcludeDirs,
// which this package may not import — ADR 0087 D10).
var skippedDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "coverage": true, "__pycache__": true,
}

// Item is one child's share of a fanout: the source entries it gets, and the label its result line
// names it by.
type Item struct {
	// Label names the item on one line: the entry itself when there is one, or the first and last
	// entries and their count when a split part or a batch gives the child several.
	Label string `json:"label"`
	// Units are the source's entries, in source order: literal items, non-blank lines, or
	// workspace-relative file paths.
	Units []string `json:"units"`
}

// SplitBudget is how many bytes of source one `split:` part may hold: a child's working context
// window in tokens, less a fixed reserve for its brief, at bytesPerToken bytes a token. Build it
// with NewSplitBudget. A budget of 0 or less means the window is unknown and no part can be sized.
type SplitBudget int

// NewSplitBudget derives the split budget from a child's working context ceiling — the Budget's
// ContextLimit (CONTEXT.md **Budget**, the single authority on context room), never the raw
// advertised n_ctx. A limit of 0 (unknown) or no larger than the brief reserve yields 0.
func NewSplitBudget(contextLimit int) SplitBudget {
	room := contextLimit - splitBriefReserveTokens
	if room <= 0 {
		return 0
	}
	return SplitBudget(room * bytesPerToken)
}

// Expand yields a fanout's items from source over the workspace fsys, in a stable order: a literal
// list in its written order, `files:` matches and `split:` parts in lexical path order, `lines:`
// in file order. source.Batch groups the entries that many per item (0 or 1: one each). budget
// is read by `split:` alone. Every error names the source: a source with no or several kinds set,
// an absolute or `..` path, a malformed glob, a `files:` walk root that is not a directory, an
// unreadable file or one that is not a regular file, a `split:` with a budget of 0 or less, an
// expansion that yields nothing, and a `stage:` source — whose items exist only once that pick
// stage has run.
func Expand(source ItemSource, fsys fs.FS, budget SplitBudget) ([]Item, error) {
	kind, value, err := sourceKind(source)
	if err != nil {
		return nil, err
	}
	if source.Batch < 0 {
		return nil, fmt.Errorf("%s: %s: batch is negative; set it to how many items one child gets", kind, value)
	}

	groups, err := expandGroups(kind, value, source.List, fsys, budget)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", kind, value, err)
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("%s: %s: the source yields no items", kind, value)
	}
	return batchItems(groups, source.Batch), nil
}

// sourceKind names the one kind source sets and its value as an error quotes it.
func sourceKind(source ItemSource) (string, string, error) {
	kinds := []struct {
		name  string
		value string
		isSet bool
	}{
		{"list", strings.Join(source.List, ", "), len(source.List) > 0},
		{"files", source.Files, source.Files != ""},
		{"lines", source.Lines, source.Lines != ""},
		{"split", source.Split, source.Split != ""},
		{"stage", source.Stage, source.Stage != ""},
	}
	var set []int
	for index, kind := range kinds {
		if kind.isSet {
			set = append(set, index)
		}
	}
	if len(set) != 1 {
		return "", "", errors.New("an item source sets exactly one of list, files, lines, split or stage")
	}
	chosen := kinds[set[0]]
	return chosen.name, chosen.value, nil
}

// expandGroups yields the source's entries as groups: one group per entry, except a split part,
// which is one group of the part's files.
func expandGroups(kind, value string, list []string, fsys fs.FS, budget SplitBudget) ([][]string, error) {
	switch kind {
	case "list":
		return singletons(list), nil
	case "stage":
		return nil, errors.New("a pick stage's items exist only once it has run; the runner takes them from its receipt")
	}

	cleaned, err := workspacePath(value)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "files":
		matches, err := globFiles(fsys, cleaned)
		return singletons(matches), err
	case "lines":
		lines, err := nonBlankLines(fsys, cleaned)
		return singletons(lines), err
	default:
		return splitParts(fsys, cleaned, budget)
	}
}

// workspacePath cleans a workspace-relative path or glob (dropping `./` and a trailing `/`) and
// refuses one that is absolute or climbs out of the workspace with `..`.
func workspacePath(given string) (string, error) {
	cleaned := path.Clean(given)
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("the path must be workspace-relative: no leading / and no .. climbing out")
	}
	return cleaned, nil
}

// singletons makes each entry a group of its own.
func singletons(entries []string) [][]string {
	groups := make([][]string, 0, len(entries))
	for _, entry := range entries {
		groups = append(groups, []string{entry})
	}
	return groups
}

// batchItems joins the groups batch at a time into items (batch 0 or 1: one group each), keeping
// their order.
func batchItems(groups [][]string, batch int) []Item {
	batch = max(batch, 1)
	items := make([]Item, 0, (len(groups)+batch-1)/batch)
	for start := 0; start < len(groups); start += batch {
		var units []string
		for _, group := range groups[start:min(start+batch, len(groups))] {
			units = append(units, group...)
		}
		items = append(items, Item{Label: itemLabel(units), Units: units})
	}
	return items
}

// itemLabel names an item by its one entry, or by its first and last entries and their count.
func itemLabel(units []string) string {
	if len(units) == 1 {
		return units[0]
	}
	return fmt.Sprintf("%s … %s (%d)", units[0], units[len(units)-1], len(units))
}

// ItemName is the short name a Driver shows an item by, where Label stays its identity (ItemKey,
// PlanHash, `{item}`, the model-facing result lines). It respells a derived label over each unit:
// an absolute unit inside dir — the workflow's folder — reads relative to it, dir itself reads as
// stage (the stage's name), an absolute unit outside dir reads as its basename, and any other unit
// is unchanged; several units join the way the label does. A label the engine set itself (one that
// is not the units' own label, such as a merge stage's) is returned unchanged, as is the label of
// an item with no units.
func ItemName(item Item, dir, stage string) string {
	if len(item.Units) == 0 || item.Label != itemLabel(item.Units) {
		return item.Label
	}
	shown := make([]string, len(item.Units))
	for index, unit := range item.Units {
		shown[index] = unitName(unit, dir, stage)
	}
	if name := itemLabel(shown); strings.TrimSpace(name) != "" {
		return name
	}
	return item.Label
}

// unitName is one unit as ItemName shows it: relative to dir when it is an absolute path inside
// it, stage when it is dir itself, its basename when it is an absolute path outside dir, and the
// unit unchanged otherwise.
func unitName(unit, dir, stage string) string {
	if !filepath.IsAbs(unit) {
		return unit
	}
	if dir != "" {
		relative, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(unit))
		if err == nil && relative == "." {
			return stage
		}
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.Base(unit)
}

// globFiles returns the regular files under fsys matching pattern in lexical order. A `**`
// segment matches zero or more path segments; every other segment is a path.Match pattern. The
// walk starts at the pattern's leading literal directories and never enters a skippedDirs
// directory below that start.
func globFiles(fsys fs.FS, pattern string) ([]string, error) {
	segments := strings.Split(pattern, "/")
	for _, segment := range segments {
		if _, err := path.Match(segment, ""); err != nil {
			return nil, fmt.Errorf("segment %q is not a valid glob: %w", segment, err)
		}
	}

	var matches []string
	err := walkFiles(fsys, literalPrefix(segments), func(name string, _ fs.DirEntry) error {
		if matchSegments(segments, strings.Split(name, "/")) {
			matches = append(matches, name)
		}
		return nil
	})
	return matches, err
}

// literalPrefix is the directory the pattern's leading segments name before the first one holding
// a glob character (the last segment is always the file pattern); "." when there is none.
func literalPrefix(segments []string) string {
	prefix := "."
	for _, segment := range segments[:len(segments)-1] {
		if strings.ContainsAny(segment, `*?[\`) {
			break
		}
		prefix = path.Join(prefix, segment)
	}
	return prefix
}

// matchSegments reports whether the path segments match the pattern segments, `**` matching zero
// or more of them. Every pattern segment has been checked well-formed.
func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == globStar {
		for skip := 0; skip <= len(name); skip++ {
			if matchSegments(pattern[1:], name[skip:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	isMatch, _ := path.Match(pattern[0], name[0])
	return isMatch && matchSegments(pattern[1:], name[1:])
}

// walkFiles calls visit for every regular file under root in lexical order, never entering a
// skippedDirs directory below root. A root that does not exist has no files; a root that exists
// but is not a directory is refused; any other walk error is returned. Symlinks below root are
// passed over, so a walk never leaves the tree it was given.
func walkFiles(fsys fs.FS, root string, visit func(name string, entry fs.DirEntry) error) error {
	info, err := fs.Stat(fsys, root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	return fs.WalkDir(fsys, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != root && skippedDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return visit(name, entry)
	})
}

// errNotRegularFile refuses a source that is a FIFO, a device, a socket or a directory: reading a
// FIFO with no writer would block the run forever.
var errNotRegularFile = errors.New("not a regular file")

// readRegularFile reads name whole, refusing anything but a regular file before it is opened.
func readRegularFile(fsys fs.FS, name string) ([]byte, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegularFile
	}
	return fs.ReadFile(fsys, name)
}

// nonBlankLines returns the regular file's lines that hold more than whitespace, trimmed, in file
// order.
func nonBlankLines(fsys fs.FS, name string) ([]string, error) {
	content, err := readRegularFile(fsys, name)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines, nil
}

// splitParts cuts the regular files under dir, in lexical order, into contiguous parts whose summed
// size fits budget. A file larger than the budget on its own becomes a part of its own.
func splitParts(fsys fs.FS, dir string, budget SplitBudget) ([][]string, error) {
	if budget <= 0 {
		return nil, fmt.Errorf(
			"no context window to size parts by: the child's window is unknown or no larger than the %d-token brief reserve",
			splitBriefReserveTokens,
		)
	}
	info, err := fs.Stat(fsys, dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("split takes a directory; use files for a single file")
	}

	var parts [][]string
	var part []string
	partSize := int64(0)
	err = walkFiles(fsys, dir, func(name string, entry fs.DirEntry) error {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if len(part) > 0 && partSize+info.Size() > int64(budget) {
			parts = append(parts, part)
			part, partSize = nil, 0
		}
		part = append(part, name)
		partSize += info.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(part) > 0 {
		parts = append(parts, part)
	}
	return parts, nil
}
