package auditsplit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
)

// The split's bounds and part names, as split.sh carried them.
const (
	// maxPartLines is the part line bound's ceiling, and the bound when the budget is unknown.
	maxPartLines = 8000
	// minPartLines is the part line bound's floor when it is derived from the budget.
	minPartLines = 200
	// bytesPerLine is the source-line size the budget is converted at.
	bytesPerLine = 40
	// minPartFiles is the file count under which a part merges into a neighbour of its directory.
	minPartFiles = 3
	// defaultPartFiles is the part file bound when PART_FILES is unset.
	defaultPartFiles = 15
	// defaultGroupParts is the parts one group folds when GROUP_PARTS is unset.
	defaultGroupParts = 8
	// wholePart is the one part a scope that fits one part is given.
	wholePart = "all"
	// rootPart is the part of the files at the workspace root, and the top-level directory
	// those files are filed under.
	rootPart = "root"
)

// The environment knobs' names, which are also how their errors name them.
const (
	envPartLines  = "PART_LINES"
	envPartFiles  = "PART_FILES"
	envGroupParts = "GROUP_PARTS"
)

// listTimeout bounds the git listing; a git that runs longer is abandoned for the walk.
const listTimeout = 2 * time.Minute

// scopeSeparators are the characters the scope input splits on: the shell's default IFS, which
// split.sh's `for path in $SCOPE` split on.
const scopeSeparators = " \t\n"

// The listing's arguments: NUL-terminated and unquoted, so every name arrives as it is on disk.
var gitListArgs = []string{
	"-c", "core.quotePath=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard",
}

// wholeScopeEntries are the scope entries that name the whole workspace.
var wholeScopeEntries = []string{"", ".", "all", "everything"}

var (
	// testPattern names test files: language-conventional suffixes, test directories, pytest names.
	testPattern = regexp.MustCompile(`(_test\.go|_test\.py|\.test\.[cm]?[jt]sx?|\.spec\.[cm]?[jt]sx?|Tests?\.cs|Tests\.swift|Test\.java|_spec\.rb)$|(^|/)(test|tests|__tests__|spec|specs|testing)/|(^|/)test_[^/]+\.py$`)
	// concPattern matches a concurrency primitive across the supported languages, a line at a
	// time as grep read it; one hit gates the concurrency lens.
	concPattern = regexp.MustCompile(`(?m)go func|(^|[[:space:]])go [A-Za-z_(]|<-|chan |sync\.|atomic\.|errgroup|async |await |Thread|Task\.Run|Parallel\.|pthread_|std::thread|std::mutex|DispatchQueue|actor |Promise\.all|Worker\(|context\.With(Cancel|Timeout|Deadline)|time\.After`)
	// excludedDirs names the folders that never hold source code: docs, dependencies, build
	// output and fixtures.
	excludedDirs = regexp.MustCompile(`^docs/|(^|/)(vendor|node_modules|build|dist|target|bin|obj|testdata|fixtures|__snapshots__)/`)
	// excludedFiles names the files that are never source code: prose, locks, images, generated files.
	excludedFiles = regexp.MustCompile(`\.(md|lock|sum|png|jpe?g|gif|svg|ico|pdf|pb\.go|min\.js)$|CHANGELOG|_generated\.`)
)

// errNothingInScope is the failure of a scope that names no listed file.
var errNothingInScope = errors.New("nothing in scope for")

// Knobs are the split's three tunable bounds as the environment carries them: PART_LINES (the part
// line bound, which overrides the one derived from the budget), PART_FILES (the part file bound)
// and GROUP_PARTS (the parts one group folds). An empty value is an unset knob; a set one must be
// a whole number written in digits.
type Knobs struct {
	PartLines  string
	PartFiles  string
	GroupParts string
}

// KnobsFromEnv reads the three knobs through getenv — os.Getenv for the process environment.
func KnobsFromEnv(getenv func(string) string) Knobs {
	return Knobs{
		PartLines:  getenv(envPartLines),
		PartFiles:  getenv(envPartFiles),
		GroupParts: getenv(envGroupParts),
	}
}

// bounds are the resolved limits one split is made with.
type bounds struct {
	partLines  int
	partFiles  int
	groupParts int
}

// resolveBounds resolves the knobs against the engine's split budget for one child, partBytes
// (bytes, in digits; anything else is an unknown budget). The part line bound is PART_LINES when
// set; otherwise half the budget at bytesPerLine bytes a line — the other half is the child's room
// for everything else — clamped to minPartLines..maxPartLines, and maxPartLines with no budget.
// A knob that is set but not a whole number is an error naming it.
func resolveBounds(knobs Knobs, partBytes string) (bounds, error) {
	partLines, err := resolvePartLines(knobs.PartLines, partBytes)
	if err != nil {
		return bounds{}, err
	}
	partFiles, err := wholeNumberOr(envPartFiles, knobs.PartFiles, defaultPartFiles)
	if err != nil {
		return bounds{}, err
	}
	groupParts, err := wholeNumberOr(envGroupParts, knobs.GroupParts, defaultGroupParts)
	if err != nil {
		return bounds{}, err
	}
	return bounds{partLines: partLines, partFiles: partFiles, groupParts: groupParts}, nil
}

// resolvePartLines is resolveBounds' part line bound: PART_LINES, else derived from partBytes.
func resolvePartLines(partLines, partBytes string) (int, error) {
	if partLines != "" {
		return wholeNumberOr(envPartLines, partLines, 0)
	}
	if !isDigits(partBytes) {
		return maxPartLines, nil
	}
	budget, err := strconv.Atoi(partBytes)
	if err != nil {
		// Digits only, so the one failure is a value past the int range: a budget that large
		// clamps to the ceiling like any other above it.
		return maxPartLines, nil
	}
	if budget <= 0 {
		return maxPartLines, nil
	}
	return min(max(budget/(2*bytesPerLine), minPartLines), maxPartLines), nil
}

// wholeNumberOr parses the knob name's value, or returns fallback when it is unset.
func wholeNumberOr(name, value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if !isDigits(value) || err != nil {
		return 0, fmt.Errorf("%s is not a whole number: %s", name, value)
	}
	return number, nil
}

// isDigits reports whether value is one or more ASCII digits and nothing else.
func isDigits(value string) bool {
	return value != "" && strings.Trim(value, "0123456789") == ""
}

// planRequest is one split's input.
type planRequest struct {
	// root is the workspace root every listed name is relative to.
	root string
	// scope is the scope input: workspace-relative files or folders, whitespace-separated.
	scope string
	// partBytes is the engine's split budget for one child, in bytes.
	partBytes string
	// knobs are the environment's bounds.
	knobs Knobs
	// git is the listing seam: the host the git listing resolves and runs through. A host with
	// no git — or a workspace git cannot list — takes the walk.
	git gitexec.Host
}

// plan is one split, computed with no file written.
type plan struct {
	// bounds are the limits the split was made with.
	bounds bounds
	// scope is every file in scope, workspace-relative, sorted in byte order.
	scope []string
	// src and tests are scope split by testPattern. Both are empty for a scope of test files
	// only, whose parts are cut from scope itself.
	src   []string
	tests []string
	// parts are the parts in byte order of their names.
	parts []part
}

// part is one part of a split.
type part struct {
	// name is the part's name: `all`, `root`, a top-level directory, `<dir>-<sub>` for a
	// re-split one, and `-<n>` appended for a chunk.
	name string
	// files are the part's source files.
	files []string
	// lines is the newline count of files together.
	lines int
	// hasConcurrency reports whether any of files holds a concurrency primitive.
	hasConcurrency bool
}

// makePlan lists the request's scope and splits it. It fails when a knob is malformed or when the
// scope names no listed file.
func makePlan(ctx context.Context, request planRequest) (plan, error) {
	resolved, err := resolveBounds(request.knobs, request.partBytes)
	if err != nil {
		return plan{}, err
	}
	scope := listScope(ctx, request.git, request.root, request.scope)
	if len(scope) == 0 {
		return plan{}, fmt.Errorf("%w: %s", errNothingInScope, request.scope)
	}

	var src, tests []string
	for _, name := range scope {
		if testPattern.MatchString(name) {
			tests = append(tests, name)
		} else {
			src = append(src, name)
		}
	}
	sources := src
	if len(src) == 0 {
		sources, tests = scope, nil
	}

	cutter := splitter{bounds: resolved, lines: lineCounter{root: request.root, counts: map[string]int{}}}
	work := cutter.split(sources)
	parts := make([]part, 0, len(work))
	for _, name := range slices.Sorted(maps.Keys(work)) {
		files := work[name]
		parts = append(parts, part{
			name:           name,
			files:          files,
			lines:          cutter.lines.total(files),
			hasConcurrency: holdsConcurrency(request.root, files),
		})
	}
	return plan{bounds: resolved, scope: scope, src: src, tests: tests, parts: parts}, nil
}

// listScope returns the listed workspace files the scope input names, sorted in byte order, each
// a regular file (a symlink to one included).
func listScope(ctx context.Context, git gitexec.Host, root, scope string) []string {
	workspace := listWorkspace(ctx, git, root)
	var wanted []string
	for _, entry := range strings.FieldsFunc(scope, isScopeSeparator) {
		entry = strings.TrimRight(strings.TrimPrefix(entry, "./"), "/")
		if slices.Contains(wholeScopeEntries, entry) {
			wanted = append(wanted, workspace...)
			continue
		}
		for _, name := range workspace {
			if name == entry || strings.HasPrefix(name, entry+"/") {
				wanted = append(wanted, name)
			}
		}
	}
	slices.Sort(wanted)
	wanted = slices.Compact(wanted)

	// A listed name need not be a file: git lists a deleted tracked file and a submodule.
	scoped := wanted[:0]
	for _, name := range wanted {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err == nil && info.Mode().IsRegular() {
			scoped = append(scoped, name)
		}
	}
	return scoped
}

// isScopeSeparator reports whether r separates two scope entries.
func isScopeSeparator(r rune) bool {
	return strings.ContainsRune(scopeSeparators, r)
}

// listWorkspace returns every workspace file a code audit may read, sorted in byte order without
// duplicates: git's listing when git lists the workspace, the walk otherwise, less the excluded
// folders and files and the names holding a newline.
func listWorkspace(ctx context.Context, git gitexec.Host, root string) []string {
	names, err := listGit(ctx, git, root)
	if err != nil {
		// No git, a refused repository or no repository at all: the walk lists the workspace
		// instead, because git is a convenience, never a prerequisite (ADR 0042 decision 2).
		names = walkWorkspace(root)
	}
	kept := names[:0]
	for _, name := range names {
		if name == "" || strings.Contains(name, "\n") || excludedDirs.MatchString(name) || excludedFiles.MatchString(name) {
			continue
		}
		kept = append(kept, name)
	}
	slices.Sort(kept)
	return slices.Compact(kept)
}

// listGit returns git's NUL-separated listing of the tracked and untracked-but-not-ignored files
// under root, or the error that makes the walk list them instead.
func listGit(ctx context.Context, git gitexec.Host, root string) ([]string, error) {
	var listing bytes.Buffer
	if err := git.RunTo(ctx, root, nil, listTimeout, &listing, gitListArgs...); err != nil {
		return nil, err
	}
	return strings.Split(listing.String(), "\x00"), nil
}

// walkWorkspace returns every regular file under root, slash-separated and root-relative, outside
// any `.git` folder. A folder that cannot be read lists nothing and the walk goes on, as `find`
// did.
func walkWorkspace(root string) []string {
	var names []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case entry.IsDir():
			if entry.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		case !entry.Type().IsRegular():
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err == nil {
			names = append(names, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return names
}

// holdsConcurrency reports whether any of files under root holds a concurrency primitive; a file
// that cannot be read holds none.
func holdsConcurrency(root string, files []string) bool {
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err == nil && concPattern.Match(data) {
			return true
		}
	}
	return false
}

// lineCounter counts files' lines under root, reading each file once.
type lineCounter struct {
	root   string
	counts map[string]int
}

// count is name's newline count, as `wc -l` gives it; a file that cannot be read counts 0.
func (c lineCounter) count(name string) int {
	if n, ok := c.counts[name]; ok {
		return n
	}
	n := 0
	if file, err := os.Open(filepath.Join(c.root, filepath.FromSlash(name))); err == nil {
		n = countNewlines(file)
		_ = file.Close() // read-only: a close error loses nothing
	}
	c.counts[name] = n
	return n
}

// total is the summed newline count of files.
func (c lineCounter) total(files []string) int {
	sum := 0
	for _, name := range files {
		sum += c.count(name)
	}
	return sum
}

// countNewlines counts the newline bytes reader yields until it ends or fails.
func countNewlines(reader io.Reader) int {
	buffer := make([]byte, 32*1024)
	n := 0
	for {
		read, err := reader.Read(buffer)
		n += bytes.Count(buffer[:read], []byte{'\n'})
		if err != nil {
			return n
		}
	}
}

// workingParts is a split in progress: each part's name to its files, which split.sh kept as
// one scope-<name>.txt file a part.
type workingParts map[string][]string

// globOrder is the part names in the order split.sh's `scope-*.txt` glob visited their files —
// byte order of `<name>.txt`, which puts `a-b` before `a`.
func globOrder(work workingParts) []string {
	names := slices.Collect(maps.Keys(work))
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(a+".txt", b+".txt")
	})
	return names
}

// splitter cuts source files into parts within its bounds.
type splitter struct {
	bounds bounds
	lines  lineCounter
}

// isOversized reports whether files exceed either part bound.
func (s splitter) isOversized(files []string) bool {
	return len(files) > s.bounds.partFiles || s.lines.total(files) > s.bounds.partLines
}

// split cuts sources into parts: one `all` part when they fit, else the four mechanics in turn.
func (s splitter) split(sources []string) workingParts {
	if !s.isOversized(sources) {
		return workingParts{wholePart: sources}
	}
	work := splitByTopLevelDir(sources)
	for _, name := range globOrder(work) {
		if files, ok := work[name]; ok && name != rootPart && s.isOversized(files) {
			resplitOneLevelDeeper(work, name)
		}
	}
	for _, name := range globOrder(work) {
		if files, ok := work[name]; ok && s.isOversized(files) {
			s.chunkFlatPart(work, name)
		}
	}
	s.mergeSmallParts(work)
	return work
}

// splitByTopLevelDir is mechanic 1: one part per top-level directory of sources, plus a root part
// for the files at the workspace root when there are any. A directory named `root` replaces the
// root files' part, as its file overwrote theirs in split.sh.
func splitByTopLevelDir(sources []string) workingParts {
	work := workingParts{}
	byDir := workingParts{}
	for _, name := range sources {
		dir, _, nested := strings.Cut(name, "/")
		if !nested {
			work[rootPart] = append(work[rootPart], name)
			continue
		}
		byDir[dir] = append(byDir[dir], name)
	}
	maps.Copy(work, byDir)
	return work
}

// resplitOneLevelDeeper is mechanic 2: the top-level part name splits into `<name>-<sub>` per
// second-level directory, the files directly under it keeping the part — which is dropped when
// there are none. A `<name>-<sub>` part that already existed is replaced.
func resplitOneLevelDeeper(work workingParts, name string) {
	var direct []string
	bySub := workingParts{}
	for _, file := range work[name] {
		segments := strings.Split(file, "/")
		switch {
		case len(segments) == 2:
			direct = append(direct, file)
		case len(segments) > 2:
			bySub[segments[1]] = append(bySub[segments[1]], file)
		}
	}
	for sub, files := range bySub {
		work[name+"-"+sub] = files
	}
	if len(direct) > 0 {
		work[name] = direct
	} else {
		delete(work, name)
	}
}

// chunkFlatPart is mechanic 3: the part name is cut in its order into `<name>-<n>` chunks, a chunk
// closing when the next file would push it over either bound. A part that fits one chunk — a
// single huge file — keeps its name. A chunk adds to a `<name>-<n>` part that already existed,
// as split.sh appended to its file.
func (s splitter) chunkFlatPart(work workingParts, name string) {
	files := work[name]
	delete(work, name)
	chunk, lines, count := 1, 0, 0
	for _, file := range files {
		n := s.lines.count(file)
		if count > 0 && (lines+n > s.bounds.partLines || count >= s.bounds.partFiles) {
			chunk, lines, count = chunk+1, 0, 0
		}
		key := name + "-" + strconv.Itoa(chunk)
		work[key] = append(slices.Clip(work[key]), file)
		lines, count = lines+n, count+1
	}
	if chunk == 1 {
		first := name + "-1"
		work[name] = work[first]
		delete(work, first)
	}
}

// mergeSmallParts is mechanic 4: per top-level directory, a part under minPartFiles files merges
// into the next part of that directory in name order — or, the last one, into the previous part
// kept — unless the merge pushes that part over either bound. A directory with one part keeps it.
func (s splitter) mergeSmallParts(work workingParts) {
	var dirs []string
	for _, entry := range partIndex(work) {
		dirs = append(dirs, entry.top)
	}
	slices.Sort(dirs)
	for _, dir := range slices.Compact(dirs) {
		var names []string
		for _, entry := range partIndex(work) {
			if entry.top == dir {
				names = append(names, entry.part)
			}
		}
		s.mergeSmallPartsOf(work, names)
	}
}

// mergeSmallPartsOf merges the small parts among names, one directory's parts in name order.
func (s splitter) mergeSmallPartsOf(work workingParts, names []string) {
	previous := ""
	for i, current := range names {
		files := work[current]
		if len(files) >= minPartFiles {
			previous = current
			continue
		}
		var target string
		switch {
		case i < len(names)-1:
			target = names[i+1]
		case previous != "":
			target = previous
		default:
			previous = current
			continue
		}
		merged := slices.Concat(work[target], files)
		slices.Sort(merged)
		if s.isOversized(merged) {
			previous = current
			continue
		}
		work[target] = merged
		delete(work, current)
	}
}

// indexEntry is one part's place: its name and the top-level directory of its first file.
type indexEntry struct {
	part string
	top  string
}

// partIndex is every part's place, sorted as split.sh sorted its `<part>\t<top>` lines. A part's
// directory comes from its first file, so it never depends on the part's name.
func partIndex(work workingParts) []indexEntry {
	entries := make([]indexEntry, 0, len(work))
	for name, files := range work {
		if len(files) == 0 {
			continue
		}
		top := rootPart
		if dir, _, nested := strings.Cut(files[0], "/"); nested {
			top = dir
		}
		entries = append(entries, indexEntry{part: name, top: top})
	}
	slices.SortFunc(entries, func(a, b indexEntry) int {
		return strings.Compare(a.part+"\t"+a.top, b.part+"\t"+b.top)
	})
	return entries
}
