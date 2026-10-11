package auditsplit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/gitexec"
)

// receiptKeys are the keys the recipe's split stage reads, summary included.
var receiptKeys = []string{"conc_parts", "files", "focus", "groups", "part_lines", "parts", "src", "summary", "tests"}

// splitTableLine is one split.txt line: `<part> <files> <lines> conc=<yes|no> group=<name>`, read
// from the right so a part name holding spaces stays whole.
var splitTableLine = regexp.MustCompile(`^(.*) ([0-9]+) ([0-9]+) conc=(yes|no) group=(.*)$`)

// splitOptions are one test split's knobs and inputs besides the workspace and the run folder.
type splitOptions struct {
	scope     string
	focus     string
	partBytes int
	knobs     Knobs
	git       gitexec.Host
}

// splitInto splits workspace's scope into run through the walk (or options.git when set) and
// returns the receipt it printed, failing the test on an error or a malformed receipt.
func splitInto(t *testing.T, workspace, run string, options splitOptions) map[string]string {
	t.Helper()
	receipt, err := trySplit(workspace, run, options)
	if err != nil {
		t.Fatalf("split(%q): %v\n%s", options.scope, err, receipt)
	}
	return parseReceipt(t, receipt)
}

// trySplit splits as splitInto does and returns the raw receipt and the error.
func trySplit(workspace, run string, options splitOptions) (string, error) {
	git := options.git
	if git.Look == nil {
		git = noGit
	}
	var out bytes.Buffer
	err := split(context.Background(), splitRequest{
		plan: planRequest{
			root:      workspace,
			scope:     options.scope,
			partBytes: strconv.Itoa(options.partBytes),
			knobs:     options.knobs,
			git:       git,
		},
		run:   run,
		focus: options.focus,
	}, &out)
	return out.String(), err
}

// parseReceipt reads a receipt as KEY=value lines, failing the test on any other line or a
// repeated key.
func parseReceipt(t *testing.T, out string) map[string]string {
	t.Helper()
	receipt := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" || strings.ToLower(key) != key || strings.ContainsAny(key, " \t") {
			t.Fatalf("the split printed %q, which is not one KEY=value line", line)
		}
		if _, repeated := receipt[key]; repeated {
			t.Fatalf("the split printed the key %q twice", key)
		}
		receipt[key] = value
	}
	return receipt
}

// readLines is a file's non-blank lines.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// fixtureLines is a file of n lines, extra the last.
func fixtureLines(n int, extra string) string {
	return strings.Repeat("x := 1\n", n-1) + extra + "\n"
}

// auditFixture is the tree the split tests cut: 360 source lines over three folders, one test
// file, one file with a concurrency primitive, and the docs and prose a code audit never reads.
func auditFixture(t *testing.T) string {
	t.Helper()
	files := map[string]string{
		"cmd/app/main.go":         fixtureLines(60, "x := 1"),
		"internal/a/a1_test.go":   fixtureLines(20, "x := 1"),
		"internal/b/b1.go":        fixtureLines(30, "go func() {}()"),
		"README.md":               fixtureLines(10, "prose"),
		"docs/design/notes.go":    fixtureLines(10, "x := 1"),
		"internal/a/testdata/f.g": fixtureLines(10, "x := 1"),
	}
	for i := 1; i <= 6; i++ {
		files[fmt.Sprintf("internal/a/a%d.go", i)] = fixtureLines(30, "x := 1")
	}
	for i := 2; i <= 4; i++ {
		files[fmt.Sprintf("internal/b/b%d.go", i)] = fixtureLines(30, "x := 1")
	}
	return writeWorkspace(t, files)
}

// The split prints exactly the receipt the recipe's split stage declares and cuts the fixture
// into parts that each fit PART_LINES and the file bound, losing no source file, filing every
// test file with a part, and leaving docs and prose out of scope.
func TestSplitFitsPartsUnderTheBudget(t *testing.T) {
	t.Parallel()
	const partLines, partFiles = 100, 15
	workspace, run := auditFixture(t), t.TempDir()

	receipt := splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: strconv.Itoa(partLines)}})

	gotKeys := slices.Sorted(maps.Keys(receipt))
	if !slices.Equal(gotKeys, receiptKeys) {
		t.Errorf("the split printed the keys %v, want the split stage's receipt %v", gotKeys, receiptKeys)
	}
	if receipt["part_lines"] != strconv.Itoa(partLines) || receipt["focus"] != noFocus {
		t.Errorf("receipt = %v, want part_lines=%d read from PART_LINES and focus=none", receipt, partLines)
	}
	parts := readLines(t, filepath.Join(run, partsFile))
	if len(parts) < 2 || strconv.Itoa(len(parts)) != receipt["parts"] {
		t.Fatalf("parts.txt lists %v; want several parts and parts=%s", parts, receipt["parts"])
	}
	var covered, tested []string
	for _, part := range parts {
		sources := readLines(t, filepath.Join(part, scopeFile))
		total := 0
		for _, source := range sources {
			total += len(readLines(t, filepath.Join(workspace, filepath.FromSlash(source))))
		}
		if len(sources) > partFiles || total > partLines {
			t.Errorf("part %s holds %d files and %d lines, over the %d-file, %d-line bound",
				filepath.Base(part), len(sources), total, partFiles, partLines)
		}
		covered = append(covered, sources...)
		tested = append(tested, readLines(t, filepath.Join(part, testsFile))...)
	}
	slices.Sort(covered)
	if src := readLines(t, filepath.Join(run, srcFile)); !slices.Equal(covered, src) {
		t.Errorf("the parts cover %v, want every source file exactly once: %v", covered, src)
	}
	if !slices.Contains(tested, "internal/a/a1_test.go") {
		t.Errorf("the test file is filed with no part: %v", tested)
	}
	for _, name := range readLines(t, filepath.Join(run, scopeFile)) {
		if strings.HasPrefix(name, "docs/") || strings.HasSuffix(name, ".md") || strings.Contains(name, "testdata/") {
			t.Errorf("scope.txt holds %q, which a code audit never reads", name)
		}
	}
	if conc := readLines(t, filepath.Join(run, concPartsFile)); len(conc) != 1 {
		t.Errorf("conc-parts.txt = %v, want the one part holding b1.go's goroutine", conc)
	}
}

// With no PART_LINES the part bound is the window: half the engine's split budget at forty bytes a
// line, clamped to 200..8000, and 8000 when the budget is unknown.
func TestSplitTakesItsBoundFromTheWindow(t *testing.T) {
	t.Parallel()
	workspace := auditFixture(t)
	for _, tc := range []struct {
		name      string
		partLines string
		partBytes int
		want      string
	}{
		{"PART_LINES wins", "150", 80000, "150"},
		{"derived from the budget", "", 80000, "1000"},
		{"clamped low", "", 400, "200"},
		{"clamped high", "", 10000000, "8000"},
		{"unknown window", "", 0, "8000"},
		{"negative window", "", -1, "8000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			receipt := splitInto(t, workspace, t.TempDir(), splitOptions{
				scope: "internal", partBytes: tc.partBytes, knobs: Knobs{PartLines: tc.partLines},
			})

			if receipt["part_lines"] != tc.want {
				t.Errorf("part_lines = %q, want %s", receipt["part_lines"], tc.want)
			}
		})
	}
}

// Every path is read as one name: a tree whose file and directory names hold spaces, `*`, `?` and
// `[`, sized over PART_LINES so the directory split, re-split and merge all run, lands every file
// in exactly one part, counts it in src=, and gives each part the wc -l line count of its files.
// A name holding a newline is skipped.
func TestSplitReadsPathsWithSpacesAndGlobCharacters(t *testing.T) {
	t.Parallel()
	const partLines = 100
	files := map[string]string{
		"a b.go":                          fixtureLines(20, "x := 1"),
		"x*y.go":                          fixtureLines(20, "x := 1"),
		"q?.go":                           fixtureLines(20, "x := 1"),
		"[z].go":                          fixtureLines(20, "x := 1"),
		"z.go":                            fixtureLines(20, "x := 1"),
		"dir with space/one.go":           fixtureLines(30, "go func() {}()"),
		"dir with space/two.go":           fixtureLines(30, "x := 1"),
		"dir with space/sub dir/three.go": fixtureLines(30, "x := 1"),
		"dir with space/sub dir/four.go":  fixtureLines(30, "x := 1"),
		"[g]/g1.go":                       fixtureLines(30, "x := 1"),
		"[g]/g2.go":                       fixtureLines(30, "x := 1"),
		"[g]/g3.go":                       fixtureLines(30, "x := 1"),
		"[g]/h/g4.go":                     fixtureLines(30, "x := 1"),
	}
	workspace := writeWorkspace(t, files)
	var sources []string
	for name := range files {
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(name))); err == nil {
			sources = append(sources, name)
		}
	}
	slices.Sort(sources)
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(workspace, "new\nline.go"), []byte(fixtureLines(20, "x := 1")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := t.TempDir()

	receipt := splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: strconv.Itoa(partLines)}})

	if receipt["src"] != strconv.Itoa(len(sources)) || receipt["files"] != strconv.Itoa(len(sources)) {
		t.Errorf("receipt = %v, want files=src=%d, one for every fixture file", receipt, len(sources))
	}
	tableLines := map[string]string{}
	for _, line := range readLines(t, filepath.Join(run, splitFile)) {
		fields := splitTableLine.FindStringSubmatch(line)
		if fields == nil {
			t.Fatalf("split.txt line %q is not `<part> <files> <lines> conc= group=`", line)
		}
		tableLines[fields[1]] = fields[3]
	}
	parts := readLines(t, filepath.Join(run, partsFile))
	if len(parts) < 4 {
		t.Fatalf("parts.txt lists %v; want the directory split to cut several parts", parts)
	}
	var covered []string
	for _, part := range parts {
		names := readLines(t, filepath.Join(part, scopeFile))
		total := 0
		for _, name := range names {
			data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(name)))
			if err != nil {
				t.Fatalf("part %s names %q, which does not read: %v", filepath.Base(part), name, err)
			}
			total += strings.Count(string(data), "\n")
		}
		name := strings.TrimPrefix(filepath.Base(part), partFolderPrefix)
		if tableLines[name] != strconv.Itoa(total) {
			t.Errorf("split.txt gives part %q %q lines, want %d, the wc -l over its files %v", name, tableLines[name], total, names)
		}
		if total > partLines {
			t.Errorf("part %q holds %d lines, over the %d-line bound", name, total, partLines)
		}
		covered = append(covered, names...)
	}
	slices.Sort(covered)
	if !slices.Equal(covered, sources) {
		t.Errorf("the parts cover %q, want every fixture file exactly once: %q", covered, sources)
	}
	if conc := readLines(t, filepath.Join(run, concPartsFile)); len(conc) != 1 ||
		!slices.Contains(readLines(t, filepath.Join(conc[0], scopeFile)), "dir with space/one.go") {
		t.Errorf("conc-parts.txt = %v, want the one part holding `dir with space/one.go`'s goroutine", conc)
	}
}

// The scope input splits on whitespace into its entries and no entry is glob-expanded: a literal
// `*` names no file and `[z].go` names itself, never `z.go`.
func TestSplitNeverGlobsTheScope(t *testing.T) {
	t.Parallel()
	workspace := writeWorkspace(t, map[string]string{"[z].go": body(1), "z.go": body(1), "a/one.go": body(1)})
	run := t.TempDir()

	receipt := splitInto(t, workspace, run, splitOptions{scope: "* [z].go"})

	if scope := readLines(t, filepath.Join(run, scopeFile)); !slices.Equal(scope, []string{"[z].go"}) {
		t.Errorf("scope.txt = %q for the scope `* [z].go`, want only [z].go", scope)
	}
	if receipt["files"] != "1" {
		t.Errorf("receipt = %v, want files=1", receipt)
	}
}

// In a git repository the split lists through git unquoted, so a name git would quote by default —
// a non-ASCII `café.go`, a `q"t.go` — reaches scope.txt as it is on disk.
func TestSplitListsGitNamesUnquoted(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this host")
	}
	workspace := writeWorkspace(t, map[string]string{"café.go": body(1), "q\"t.go": body(1), "plain.go": body(1)})
	gitIn(t, workspace, "init", "-q")
	run := t.TempDir()

	splitInto(t, workspace, run, splitOptions{scope: ".", git: gitexec.OS()})

	want := []string{"café.go", "plain.go"}
	if runtime.GOOS != "windows" {
		want = append(want, "q\"t.go")
	}
	slices.Sort(want)
	if scope := readLines(t, filepath.Join(run, scopeFile)); !slices.Equal(scope, want) {
		t.Errorf("scope.txt = %q, want every git-listed file as named on disk: %q", scope, want)
	}
}

// A re-run over an unchanged scope and layout touches nothing and prints the same receipt.
func TestSplitReRunIsANoOp(t *testing.T) {
	t.Parallel()
	workspace, run := auditFixture(t), t.TempDir()
	options := splitOptions{scope: ".", focus: "bugs", knobs: Knobs{PartLines: "100"}}
	first, err := trySplit(workspace, run, options)
	if err != nil {
		t.Fatalf("first split: %v\n%s", err, first)
	}
	marker := filepath.Join(readLines(t, filepath.Join(run, partsFile))[0], "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := trySplit(workspace, run, options)

	if err != nil || second != first {
		t.Errorf("re-run printed %q (err %v), want the first receipt %q", second, err, first)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the re-run cleared the part folder holding %s: %v", marker, err)
	}
}

// A changed bound re-splits: the previous split's folders and lists are cleared and the layout
// key records the new bound.
func TestSplitReSplitsOnAChangedBound(t *testing.T) {
	t.Parallel()
	workspace, run := auditFixture(t), t.TempDir()
	splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: "100"}})
	stale := readLines(t, filepath.Join(run, partsFile))

	receipt := splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: "1000"}})

	if receipt["parts"] != "1" || receipt["part_lines"] != "1000" {
		t.Errorf("receipt = %v, want one part under the 1000-line bound", receipt)
	}
	if layout := readLines(t, filepath.Join(run, layoutFile)); !slices.Equal(layout, []string{"recipe-1 part_lines=1000 part_files=15 group_parts=8"}) {
		t.Errorf("layout.txt = %q, want the new bound's layout key", layout)
	}
	for _, folder := range stale {
		if _, err := os.Stat(folder); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the previous split's %s survived the re-split (err %v)", folder, err)
		}
	}
	if parts := readLines(t, filepath.Join(run, partsFile)); !slices.Equal(parts, []string{filepath.Join(run, "part-all")}) {
		t.Errorf("parts.txt = %q, want the one part all", parts)
	}
}

// A scope of test files only is split from the scope itself, with empty source and test lists.
func TestSplitCutsATestOnlyScope(t *testing.T) {
	t.Parallel()
	workspace := writeWorkspace(t, map[string]string{"a/a_test.go": body(5), "a/b_test.go": body(5)})
	run := t.TempDir()

	receipt := splitInto(t, workspace, run, splitOptions{scope: "a"})

	if receipt["files"] != "2" || receipt["src"] != "0" || receipt["tests"] != "0" || receipt["parts"] != "1" {
		t.Errorf("receipt = %v, want files=2 src=0 tests=0 in one part", receipt)
	}
	part := filepath.Join(run, "part-all")
	if scope := readLines(t, filepath.Join(part, scopeFile)); !slices.Equal(scope, []string{"a/a_test.go", "a/b_test.go"}) {
		t.Errorf("part-all/scope.txt = %q, want the test files themselves", scope)
	}
	if tests := readLines(t, filepath.Join(part, testsFile)); len(tests) != 0 {
		t.Errorf("part-all/tests.txt = %q, want it empty", tests)
	}
}

// The lists keep split.sh's two orders: parts.txt and conc-parts.txt in byte order of
// `<name>.txt` (its `scope-*.txt` glob, so `x-y` precedes `x`), groups.txt and split.txt by name.
func TestSplitKeepsBothOrders(t *testing.T) {
	t.Parallel()
	files := map[string]string{}
	for _, dir := range []string{"x", "x-y"} {
		for i := 1; i <= 3; i++ {
			files[fmt.Sprintf("%s/%d.go", dir, i)] = "go func() {}()\n" + body(9)
		}
	}
	workspace, run := writeWorkspace(t, files), t.TempDir()

	splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: "40"}})

	globOrdered := []string{filepath.Join(run, "part-x-y"), filepath.Join(run, "part-x")}
	if parts := readLines(t, filepath.Join(run, partsFile)); !slices.Equal(parts, globOrdered) {
		t.Errorf("parts.txt = %q, want the glob order %q", parts, globOrdered)
	}
	if conc := readLines(t, filepath.Join(run, concPartsFile)); !slices.Equal(conc, globOrdered) {
		t.Errorf("conc-parts.txt = %q, want the glob order %q", conc, globOrdered)
	}
	nameOrdered := []string{filepath.Join(run, "group-x"), filepath.Join(run, "group-x-y")}
	if groups := readLines(t, filepath.Join(run, groupsFile)); !slices.Equal(groups, nameOrdered) {
		t.Errorf("groups.txt = %q, want name order %q", groups, nameOrdered)
	}
	table := []string{"x 3 30 conc=yes group=x", "x-y 3 30 conc=yes group=x-y"}
	if got := readLines(t, filepath.Join(run, splitFile)); !slices.Equal(got, table) {
		t.Errorf("split.txt = %q, want name order %q", got, table)
	}
}

// A directory of more than GROUP_PARTS parts groups by second path segment, and every group's cap
// is the claims shared out, clamped to 2..12; a test file follows the part of its directory, then
// of that directory less a test segment, then of its top-level directory, then the root part.
func TestSplitGroupsAndFilesTests(t *testing.T) {
	t.Parallel()
	workspace := writeWorkspace(t, map[string]string{
		"r1.go": body(10), "r2.go": body(10), "r3.go": body(10),
		"a/p/1.go": body(10), "a/p/2.go": body(10), "a/p/3.go": body(10),
		"a/q/1.go": body(10), "a/q/2.go": body(10), "a/q/3.go": body(10),
		"a/p/p_test.go": body(1), "a/q/tests/q_test.go": body(1), "a/z/z_test.go": body(1),
		"other/o_test.go": body(1),
	})
	run := t.TempDir()

	splitInto(t, workspace, run, splitOptions{scope: ".", knobs: Knobs{PartLines: "40", GroupParts: "1"}})

	table := []string{"a-p 3 30 conc=no group=a-p", "a-q 3 30 conc=no group=a-q", "root 3 30 conc=no group=root"}
	if got := readLines(t, filepath.Join(run, splitFile)); !slices.Equal(got, table) {
		t.Errorf("split.txt = %q, want %q", got, table)
	}
	if capacity := readLines(t, filepath.Join(run, "group-a-p", capFile)); !slices.Equal(capacity, []string{"12"}) {
		t.Errorf("group-a-p/cap.txt = %q, want 60/3 clamped to 12", capacity)
	}
	for part, want := range map[string][]string{
		"a-p":  {"a/p/p_test.go", "a/z/z_test.go"},
		"a-q":  {"a/q/tests/q_test.go", "a/z/z_test.go"},
		"root": {"other/o_test.go"},
	} {
		if got := readLines(t, filepath.Join(run, partFolderPrefix+part, testsFile)); !slices.Equal(got, want) {
			t.Errorf("part-%s/tests.txt = %q, want %q", part, got, want)
		}
	}
}

// Every path the split writes into a list is absolute and native, even for a run folder named
// relative to the working directory.
func TestSplitWritesAbsolutePaths(t *testing.T) {
	t.Parallel()
	workspace, run := auditFixture(t), t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, run)
	if err != nil {
		t.Skipf("the temp dir has no path relative to the working directory: %v", err)
	}

	splitInto(t, workspace, relative, splitOptions{scope: ".", knobs: Knobs{PartLines: "100", GroupParts: "1"}})

	lists := []string{filepath.Join(run, runDirFile), filepath.Join(run, partsFile), filepath.Join(run, concPartsFile), filepath.Join(run, groupsFile)}
	for _, group := range readLines(t, filepath.Join(run, groupsFile)) {
		lists = append(lists, filepath.Join(group, partsFile))
	}
	for _, list := range lists {
		for _, path := range readLines(t, list) {
			if !filepath.IsAbs(path) {
				t.Errorf("%s names %q, which is not absolute", list, path)
			}
		}
	}
	if dir := readLines(t, filepath.Join(run, runDirFile)); !slices.Equal(dir, []string{run}) {
		t.Errorf("run-dir.txt = %q, want %q", dir, run)
	}
}

// A failure prints `summary=audit-split: <cause>` as the receipt and returns that error.
func TestSplitFailsWithASummary(t *testing.T) {
	t.Parallel()
	workspace := writeWorkspace(t, map[string]string{"a.go": body(1)})
	for _, tc := range []struct {
		name    string
		run     string
		options splitOptions
		want    string
	}{
		{"a missing run folder", filepath.Join(workspace, "missing"), splitOptions{scope: "."}, "workflow folder not found: "},
		{"an empty scope", t.TempDir(), splitOptions{scope: "nowhere"}, "nothing in scope for: nowhere"},
		{"a malformed knob", t.TempDir(), splitOptions{scope: ".", knobs: Knobs{PartLines: "lots"}}, "PART_LINES is not a whole number: lots"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := trySplit(workspace, tc.run, tc.options)

			wantPrefix := "summary=audit-split: " + tc.want
			if err == nil || !strings.HasPrefix(out, wantPrefix) || strings.Count(out, "\n") != 1 {
				t.Errorf("split printed %q (err %v), want one line starting %q", out, err, wantPrefix)
			}
			if err != nil && "summary="+err.Error()+"\n" != out {
				t.Errorf("error %q is not the summary %q", err, out)
			}
		})
	}
}

// Split, the public entry, fails a run folder that is not a folder before it lists anything.
func TestSplitRefusesAMissingRunFolder(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	var out bytes.Buffer

	err := Split(t.TempDir(), missing, ".", "", 0, &out)

	if !errors.Is(err, errWorkflowFolderNotFound) || out.String() != "summary="+err.Error()+"\n" {
		t.Errorf("Split printed %q (err %v), want the workflow-folder summary", out.String(), err)
	}
}

// The focus input names a focus area in any case, or it is none.
func TestSplitEchoesTheFocusArea(t *testing.T) {
	t.Parallel()
	workspace := writeWorkspace(t, map[string]string{"a.go": body(1)})
	for focus, want := range map[string]string{"": "none", "Security": "security", "bugs": "bugs", "speed": "none"} {
		t.Run(focus, func(t *testing.T) {
			t.Parallel()

			receipt := splitInto(t, workspace, t.TempDir(), splitOptions{scope: ".", focus: focus})

			if receipt["focus"] != want {
				t.Errorf("focus = %q for %q, want %q", receipt["focus"], focus, want)
			}
		})
	}
}

// Flags answers yes only for a bundle line saying `CONCURRENCY: yes`, in any case and emphasis.
func TestFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		bundle string
		want   string
	}{
		{"plain yes", "# Bundle\nCONCURRENCY: yes\n", "yes"},
		{"emphasised, any case", "intro\n**Concurrency:** YES — goroutines\n", "yes"},
		{"no", "CONCURRENCY: no\n", "no"},
		{"the answer on the next line", "CONCURRENCY:\nyes\n", "no"},
		{"no bundle", "", "no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := t.TempDir()
			if tc.bundle != "" {
				if err := os.WriteFile(filepath.Join(run, bundleFile), []byte(tc.bundle), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer

			err := Flags(run, &out)

			want := fmt.Sprintf("concurrency=%s\nsummary=the ground truth says concurrency %s\n", tc.want, tc.want)
			if err != nil || out.String() != want {
				t.Errorf("Flags printed %q (err %v), want %q", out.String(), err, want)
			}
		})
	}
}
