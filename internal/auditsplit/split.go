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

	"github.com/airiclenz/apogee/internal/gitexec"
)

// The run folder's files, as split.sh named them.
const (
	scopeFile     = "scope.txt"
	srcFile       = "src.txt"
	testsFile     = "tests.txt"
	runDirFile    = "run-dir.txt"
	partsFile     = "parts.txt"
	concPartsFile = "conc-parts.txt"
	groupsFile    = "groups.txt"
	splitFile     = "split.txt"
	layoutFile    = "layout.txt"
	capFile       = "cap.txt"
	bundleFile    = "bundle.md"
	// partFolderPrefix and groupFolderPrefix name a part's and a group's folder under the run.
	partFolderPrefix  = "part-"
	groupFolderPrefix = "group-"
)

// The claim caps, the layout key's version and the receipt's words, as split.sh carried them.
const (
	// totalClaims are the claims every group's enumerator may pass on together.
	totalClaims = 60
	// minGroupClaims and maxGroupClaims bound one group's share of totalClaims.
	minGroupClaims = 2
	maxGroupClaims = 12
	// layoutVersion is the layout key's version: a folder split under another one re-splits.
	layoutVersion = "recipe-1"
	// noFocus is the receipt's focus when the input names no focus area.
	noFocus = "none"
	// commandName prefixes a failure's summary and error.
	commandName = "audit-split"
	// answerYes and answerNo are a flag's two values in the receipt and the split table.
	answerYes = "yes"
	answerNo  = "no"
)

// The run folder's permissions, as a shell's default umask leaves them.
const (
	filePermissions   fs.FileMode = 0o644
	folderPermissions fs.FileMode = 0o755
)

// focusAreas are the focus areas the focus input may name.
var focusAreas = []string{"all", "bugs", "security", "tests", "standards"}

// testDirSuffix matches a trailing test-directory segment, which a test file's own directory
// sheds when no part holds that directory itself.
var testDirSuffix = regexp.MustCompile(`(^|/)(test|tests|__tests__|spec|specs|testing)$`)

// concurrencyAnswer matches the ground-truth child's `CONCURRENCY: yes` line, lower-cased, with
// any markdown emphasis around it.
var concurrencyAnswer = regexp.MustCompile(`^[*_[:space:]]*concurrency[*_[:space:]]*:[*_[:space:]]*yes`)

// errWorkflowFolderNotFound is the failure of a run folder that is not a folder.
var errWorkflowFolderNotFound = errors.New("workflow folder not found")

// Split splits the workspace's scope into the audit run folder run and prints the receipt the
// recipe's split stage reads to w: `files`, `src`, `tests`, `parts`, `groups`, `conc_parts`,
// `part_lines`, `focus` and `summary`, one KEY=value a line. Under run it writes, LF-terminated
// and one entry a line:
//
//	scope.txt              every file in scope, workspace-relative and slash-separated
//	src.txt, tests.txt     the scope split into source and test files
//	part-<name>/scope.txt  one part's source files
//	part-<name>/tests.txt  the test files that belong next to that part
//	group-<name>/parts.txt the part folders one rollup child folds
//	group-<name>/cap.txt   how many claims that group's enumerator may pass on
//	run-dir.txt            run itself
//	parts.txt              every part folder, in byte order of `<name>.txt`
//	conc-parts.txt         the part folders holding a concurrency primitive
//	groups.txt             every group folder, in byte order of its name
//	split.txt              one `<part> <files> <lines> conc=<yes|no> group=<name>` line a part
//	layout.txt             the layout key: the layout and bounds the split was made with
//
// Every folder it writes is absolute and native to the host (filepath.Abs of run). scope is
// workspace-relative files or folders, whitespace-separated (`.` or `all` for the whole
// workspace); focus is echoed as `focus` when it names a focus area and is `none` otherwise;
// partBytes is the engine's split budget for one child, zero or less when it is unknown. The
// environment's PART_LINES, PART_FILES and GROUP_PARTS tune the bounds, and the listing runs
// through git when git lists the workspace.
//
// A folder whose scope and layout key are unchanged is left untouched and its receipt re-printed;
// a changed one is cleared of the previous split first. A failure — run not a folder, a malformed
// knob, an empty scope, a file that cannot be written — prints `summary=audit-split: <cause>` to
// w and returns that error.
func Split(workspace, run, scope, focus string, partBytes int, w io.Writer) error {
	return split(context.Background(), splitRequest{
		plan: planRequest{
			root:      workspace,
			scope:     scope,
			partBytes: strconv.Itoa(partBytes),
			knobs:     KnobsFromEnv(os.Getenv),
			git:       gitexec.OS(),
		},
		run:   run,
		focus: focus,
	}, w)
}

// Flags prints the ground-truth child's concurrency answer to w as a receipt: `concurrency=yes`
// when a line of run's bundle.md says `CONCURRENCY: yes` (any case, markdown emphasis allowed),
// `concurrency=no` otherwise — a missing or unreadable bundle included — and a summary. Each line
// is matched on its own, so `CONCURRENCY:` and `yes` on separate lines say no. The one error is
// w's.
func Flags(run string, w io.Writer) error {
	answer := answerNo
	if data, err := os.ReadFile(filepath.Join(run, bundleFile)); err == nil && saysConcurrency(data) {
		answer = answerYes
	}
	_, err := fmt.Fprintf(w, "concurrency=%s\nsummary=the ground truth says concurrency %s\n", answer, answer)
	return err
}

// saysConcurrency reports whether a line of bundle answers the concurrency question yes.
func saysConcurrency(bundle []byte) bool {
	for line := range strings.SplitSeq(string(bundle), "\n") {
		if concurrencyAnswer.MatchString(asciiLower(line)) {
			return true
		}
	}
	return false
}

// splitRequest is one split's input: the plan's, and what the run folder needs besides.
type splitRequest struct {
	// plan is the listing and split input, knobs and listing seam included.
	plan planRequest
	// run is the audit run folder, absolute or relative to the working directory.
	run string
	// focus is the focus input.
	focus string
}

// split is Split with its knobs and listing seam in request.
func split(ctx context.Context, request splitRequest, w io.Writer) error {
	info, err := os.Stat(request.run)
	if err != nil || !info.IsDir() {
		return fail(w, fmt.Errorf("%w: %s", errWorkflowFolderNotFound, request.run))
	}
	run, err := filepath.Abs(request.run)
	if err != nil {
		return fail(w, err)
	}
	planned, err := makePlan(ctx, request.plan)
	if err != nil {
		return fail(w, err)
	}

	folder := runFolder{dir: run}
	if !folder.holds(planned) {
		if err := folder.write(planned); err != nil {
			return fail(w, err)
		}
	}
	receipt, err := folder.receipt(planned.bounds.partLines, focusArea(request.focus))
	if err != nil {
		return fail(w, err)
	}
	_, err = io.WriteString(w, receipt)
	return err
}

// fail prints cause as the receipt's summary and returns it, prefixed with the command's name.
func fail(w io.Writer, cause error) error {
	err := fmt.Errorf("%s: %w", commandName, cause)
	if _, writeErr := fmt.Fprintf(w, "summary=%s\n", err); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return err
}

// focusArea is the focus area focus names, matched in ASCII lower case, or noFocus.
func focusArea(focus string) string {
	given := strings.TrimRight(asciiLower(focus), "\n")
	if slices.Contains(focusAreas, given) {
		return given
	}
	return noFocus
}

// asciiLower lower-cases the ASCII letters of text and leaves every other byte as it is, as `tr
// 'A-Z' 'a-z'` and grep -i in the C locale did.
func asciiLower(text string) string {
	lowered := []byte(text)
	for i, b := range lowered {
		if 'A' <= b && b <= 'Z' {
			lowered[i] = b + ('a' - 'A')
		}
	}
	return string(lowered)
}

// layoutKey is what a split is made with: the layout version and the three bounds.
func layoutKey(resolved bounds) string {
	return fmt.Sprintf("%s part_lines=%d part_files=%d group_parts=%d",
		layoutVersion, resolved.partLines, resolved.partFiles, resolved.groupParts)
}

// runFolder is one audit run folder, dir absolute.
type runFolder struct {
	dir string
}

// path is name's path under the folder.
func (f runFolder) path(name string) string {
	return filepath.Join(f.dir, name)
}

// holds reports whether the folder already holds planned: the same scope.txt, byte for byte, and
// the same layout key.
func (f runFolder) holds(planned plan) bool {
	scope, err := os.ReadFile(f.path(scopeFile))
	if err != nil || !bytes.Equal(scope, linesOf(planned.scope)) {
		return false
	}
	layout, err := os.ReadFile(f.path(layoutFile))
	return err == nil && strings.TrimRight(string(layout), "\n") == layoutKey(planned.bounds)
}

// write clears the previous split and writes planned, the layout key last, so a split cut short
// re-splits on the next run.
func (f runFolder) write(planned plan) error {
	if err := f.removePreviousSplit(); err != nil {
		return err
	}
	if err := f.writeFiles(map[string][]byte{
		scopeFile:  linesOf(planned.scope),
		runDirFile: linesOf([]string{f.dir}),
		testsFile:  linesOf(planned.tests),
		srcFile:    linesOf(planned.src),
	}); err != nil {
		return err
	}

	work := workingParts{}
	byName := map[string]part{}
	for _, p := range planned.parts {
		work[p.name] = p.files
		byName[p.name] = p
	}
	if err := f.writeParts(work, byName, assignTests(work, planned.tests)); err != nil {
		return err
	}
	if err := f.writeGroups(work, byName, planned.bounds.groupParts); err != nil {
		return err
	}
	return f.writeFiles(map[string][]byte{layoutFile: linesOf([]string{layoutKey(planned.bounds)})})
}

// removePreviousSplit removes every part and group folder and every list a previous split wrote;
// scope.txt is overwritten instead.
func (f runFolder) removePreviousSplit() error {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, partFolderPrefix) || strings.HasPrefix(name, groupFolderPrefix) {
			if err := os.RemoveAll(f.path(name)); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{
		srcFile, testsFile, runDirFile, partsFile, concPartsFile, groupsFile, splitFile, layoutFile,
	} {
		if err := os.Remove(f.path(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeParts writes one part-<name>/ folder a part with its scope and test lists, then parts.txt
// and conc-parts.txt, all in split.sh's glob order (byte order of `<name>.txt`).
func (f runFolder) writeParts(work workingParts, byName map[string]part, tests map[string][]string) error {
	var folders, concurrent []string
	for _, name := range globOrder(work) {
		folder := f.path(partFolderPrefix + name)
		if err := os.MkdirAll(folder, folderPermissions); err != nil {
			return err
		}
		if err := writeFilesIn(folder, map[string][]byte{
			scopeFile: linesOf(work[name]),
			testsFile: linesOf(tests[name]),
		}); err != nil {
			return err
		}
		folders = append(folders, folder)
		if byName[name].hasConcurrency {
			concurrent = append(concurrent, folder)
		}
	}
	return f.writeFiles(map[string][]byte{partsFile: linesOf(folders), concPartsFile: linesOf(concurrent)})
}

// writeGroups writes one group-<name>/ folder a group with the part folders it folds and its
// claim cap, then groups.txt and the split table.
func (f runFolder) writeGroups(work workingParts, byName map[string]part, groupParts int) error {
	grouping := groupPartsOf(work, groupParts)
	var names []string
	for _, entry := range grouping {
		names = append(names, entry.group)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	claims := max(min(totalClaims/len(names), maxGroupClaims), minGroupClaims)

	folders := make([]string, 0, len(names))
	for _, name := range names {
		var members []string
		for _, entry := range grouping {
			if entry.group == name {
				members = append(members, f.path(partFolderPrefix+entry.part))
			}
		}
		folder := f.path(groupFolderPrefix + name)
		if err := os.MkdirAll(folder, folderPermissions); err != nil {
			return err
		}
		if err := writeFilesIn(folder, map[string][]byte{
			partsFile: linesOf(members),
			capFile:   linesOf([]string{strconv.Itoa(claims)}),
		}); err != nil {
			return err
		}
		folders = append(folders, folder)
	}

	table := make([]string, 0, len(grouping))
	for _, entry := range grouping {
		p := byName[entry.part]
		conc := answerNo
		if p.hasConcurrency {
			conc = answerYes
		}
		table = append(table, fmt.Sprintf("%s %d %d conc=%s group=%s", p.name, len(p.files), p.lines, conc, entry.group))
	}
	return f.writeFiles(map[string][]byte{groupsFile: linesOf(folders), splitFile: linesOf(table)})
}

// writeFiles writes each name's content under the folder.
func (f runFolder) writeFiles(files map[string][]byte) error {
	return writeFilesIn(f.dir, files)
}

// receipt is the split stage's receipt, counted from the lists on disk so a re-run and a fresh
// split print the same thing. A missing list counts zero entries.
func (f runFolder) receipt(partLines int, focus string) (string, error) {
	counts := map[string]int{}
	for _, name := range []string{scopeFile, srcFile, testsFile, partsFile, groupsFile, concPartsFile} {
		n, err := countEntries(f.path(name))
		if err != nil {
			return "", err
		}
		counts[name] = n
	}
	return fmt.Sprintf(
		"files=%d\nsrc=%d\ntests=%d\nparts=%d\ngroups=%d\nconc_parts=%d\npart_lines=%d\nfocus=%s\n"+
			"summary=%d files in %d parts and %d groups, at most %d lines a part\n",
		counts[scopeFile], counts[srcFile], counts[testsFile], counts[partsFile], counts[groupsFile],
		counts[concPartsFile], partLines, focus,
		counts[scopeFile], counts[partsFile], counts[groupsFile], partLines,
	), nil
}

// writeFilesIn writes each name's content under dir, in name order so a failure is reproducible.
func writeFilesIn(dir string, files map[string][]byte) error {
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], filePermissions); err != nil {
			return err
		}
	}
	return nil
}

// countEntries is a list file's entry count — its newline count, as `wc -l` gives it — and zero
// for a missing file.
func countEntries(path string) (int, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }() // read-only: a close error loses nothing
	return countNewlines(file), nil
}

// linesOf is entries one a line, each LF-terminated; no entries is an empty file.
func linesOf(entries []string) []byte {
	var buffer bytes.Buffer
	for _, entry := range entries {
		buffer.WriteString(entry)
		buffer.WriteByte('\n')
	}
	return buffer.Bytes()
}

// assignTests files each test file with parts, as split.sh's write_test_lists did: the parts
// holding a source file of the test's own directory, else of that directory with a trailing test
// segment stripped, else of its top-level directory, else the fallback part — the root part when
// there is one, the first part of the index otherwise — so no test file is dropped. Each part's
// tests keep tests' order.
func assignTests(work workingParts, tests []string) map[string][]string {
	byDir := map[string][]string{}
	byTop := map[string][]string{}
	for _, name := range globOrder(work) {
		for _, file := range work[name] {
			dir, top := dirOf(file), topOf(file)
			if !slices.Contains(byDir[dir], name) {
				byDir[dir] = append(byDir[dir], name)
			}
			if !slices.Contains(byTop[top], name) {
				byTop[top] = append(byTop[top], name)
			}
		}
	}
	fallback := ""
	if index := partIndex(work); len(index) > 0 {
		fallback = index[0].part
	}
	if _, ok := work[rootPart]; ok {
		fallback = rootPart
	}

	assigned := map[string][]string{}
	for _, test := range tests {
		dir := dirOf(test)
		parts, ok := byDir[dir]
		if !ok {
			parts, ok = byDir[stripTestDir(dir)]
		}
		if !ok {
			parts, ok = byTop[topOf(test)]
		}
		if !ok {
			parts = []string{fallback}
		}
		for _, name := range parts {
			assigned[name] = append(assigned[name], test)
		}
	}
	return assigned
}

// dirOf is name's directory, `.` for a name at the workspace root.
func dirOf(name string) string {
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		return name[:slash]
	}
	return "."
}

// topOf is name's top-level directory, rootPart for a name at the workspace root.
func topOf(name string) string {
	if top, _, nested := strings.Cut(name, "/"); nested {
		return top
	}
	return rootPart
}

// stripTestDir is dir with a trailing test-directory segment removed, `.` when nothing is left.
func stripTestDir(dir string) string {
	match := testDirSuffix.FindStringIndex(dir)
	if match == nil {
		return dir
	}
	if stripped := dir[:match[0]]; stripped != "" {
		return stripped
	}
	return "."
}

// groupedPart is one part and the group that folds it.
type groupedPart struct {
	part  string
	group string
}

// groupPartsOf assigns every part its group, as split.sh's write_groups did: per top-level
// directory in byte order, its parts in index order fold into one group named for the directory —
// or, when the directory holds more than groupParts parts, one group per second path segment of
// each part's first file. A part of the workspace root's files is filed under rootPart.
func groupPartsOf(work workingParts, groupParts int) []groupedPart {
	index := partIndex(work)
	var dirs []string
	for _, entry := range index {
		dirs = append(dirs, entry.top)
	}
	slices.Sort(dirs)

	var grouping []groupedPart
	for _, dir := range slices.Compact(dirs) {
		var members []string
		for _, entry := range index {
			if entry.top == dir {
				members = append(members, entry.part)
			}
		}
		for _, name := range members {
			group := dir
			if len(members) > groupParts {
				group = secondLevelGroup(work[name][0])
			}
			grouping = append(grouping, groupedPart{part: name, group: group})
		}
	}
	return grouping
}

// secondLevelGroup is the group a file's part falls into when its directory is split by second
// path segment: `<top>-<second>` for a file two folders deep or more, its top-level directory
// otherwise.
func secondLevelGroup(file string) string {
	segments := strings.Split(file, "/")
	switch {
	case len(segments) > 2:
		return segments[0] + "-" + segments[1]
	case len(segments) > 1:
		return segments[0]
	default:
		return rootPart
	}
}
