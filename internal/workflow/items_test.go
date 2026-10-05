package workflow

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// sizedFile is a MapFS file of size bytes.
func sizedFile(size int) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(strings.Repeat("x", size))}
}

// workspace is the tree the Expand cases read.
func workspace() fstest.MapFS {
	return fstest.MapFS{
		"main.go":                    sizedFile(10),
		"README.md":                  sizedFile(10),
		"internal/a.go":              sizedFile(40),
		"internal/b.go":              sizedFile(40),
		"internal/c.go":              sizedFile(40),
		"internal/huge.go":           sizedFile(500),
		"internal/z.go":              sizedFile(30),
		"internal/deep/nest/leaf.go": sizedFile(10),
		"internal/node_modules/x.go": sizedFile(10),
		"node_modules/pkg/skip.go":   sizedFile(10),
		".git/hooks/pre-commit.go":   sizedFile(10),
		"build/gen.go":               sizedFile(10),
		"empty/.keep":                sizedFile(0),
		"todo.txt":                   {Data: []byte("first item\n\n   \n  second item  \r\nthird\n")},
		"blank.txt":                  {Data: []byte("\n  \n\t\n")},
	}
}

// unitsOf lists each item's units, the shape most cases assert.
func unitsOf(items []Item) [][]string {
	units := make([][]string, 0, len(items))
	for _, item := range items {
		units = append(units, item.Units)
	}
	return units
}

func TestExpandYieldsEachSourceKindInStableOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source ItemSource
		budget SplitBudget
		want   [][]string
	}{
		{
			name:   "a literal list keeps its written order",
			source: ItemSource{List: []string{"zeta", "alpha", "mid"}},
			want:   [][]string{{"zeta"}, {"alpha"}, {"mid"}},
		},
		{
			name:   "files ** matches the top-level and the deep file and skips grep's excluded dirs",
			source: ItemSource{Files: "**/*.go"},
			want: [][]string{
				{"internal/a.go"}, {"internal/b.go"}, {"internal/c.go"},
				{"internal/deep/nest/leaf.go"}, {"internal/huge.go"}, {"internal/z.go"}, {"main.go"},
			},
		},
		{
			name:   "files ** between literal segments matches zero segments too",
			source: ItemSource{Files: "./internal/**/leaf.go"},
			want:   [][]string{{"internal/deep/nest/leaf.go"}},
		},
		{
			name:   "files without ** matches one directory level",
			source: ItemSource{Files: "internal/*.go"},
			want:   [][]string{{"internal/a.go"}, {"internal/b.go"}, {"internal/c.go"}, {"internal/huge.go"}, {"internal/z.go"}},
		},
		{
			name:   "files naming an excluded directory as its start still walks it",
			source: ItemSource{Files: "build/*.go"},
			want:   [][]string{{"build/gen.go"}},
		},
		{
			name:   "lines yields the trimmed non-blank lines in file order",
			source: ItemSource{Lines: "todo.txt"},
			want:   [][]string{{"first item"}, {"second item"}, {"third"}},
		},
		{
			name:   "split packs contiguous files under the budget and gives an oversized file its own part",
			source: ItemSource{Split: "internal"},
			budget: 100,
			want: [][]string{
				{"internal/a.go", "internal/b.go"},
				{"internal/c.go", "internal/deep/nest/leaf.go"},
				{"internal/huge.go"},
				{"internal/z.go"},
			},
		},
		{
			name:   "batch groups the entries N per item and keeps the remainder",
			source: ItemSource{List: []string{"a", "b", "c", "d", "e"}, Batch: 2},
			want:   [][]string{{"a", "b"}, {"c", "d"}, {"e"}},
		},
		{
			name:   "batch groups split parts whole",
			source: ItemSource{Split: "internal", Batch: 2},
			budget: 100,
			want: [][]string{
				{"internal/a.go", "internal/b.go", "internal/c.go", "internal/deep/nest/leaf.go"},
				{"internal/huge.go", "internal/z.go"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			items, err := Expand(tc.source, workspace(), tc.budget)

			if err != nil {
				t.Fatalf("Expand: %v", err)
			}
			if got := unitsOf(items); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("units = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandSplitsADirectoryAliasAlike(t *testing.T) {
	t.Parallel()

	want, err := Expand(ItemSource{Split: "internal"}, workspace(), 100)
	if err != nil {
		t.Fatalf("Expand internal: %v", err)
	}

	for _, alias := range []string{"internal/", "./internal", "./internal/"} {
		got, err := Expand(ItemSource{Split: alias}, workspace(), 100)

		if err != nil {
			t.Fatalf("Expand %q: %v", alias, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expand %q = %q, want %q", alias, unitsOf(got), unitsOf(want))
		}
	}
}

func TestExpandLabelsAnItemByItsEntries(t *testing.T) {
	t.Parallel()

	items, err := Expand(ItemSource{List: []string{"a", "b", "c", "d"}, Batch: 3}, workspace(), 0)

	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	labels := []string{items[0].Label, items[1].Label}
	if want := []string{"a … c (3)", "d"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("labels = %q, want %q", labels, want)
	}
}

func TestExpandRefusesWithAnErrorNamingTheSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		source   ItemSource
		budget   SplitBudget
		wantText []string
	}{
		{"no source", ItemSource{}, 100, []string{"exactly one of list, files, lines, split or stage"}},
		{"two sources", ItemSource{Files: "*.go", Lines: "todo.txt"}, 100, []string{"exactly one"}},
		{"a negative batch", ItemSource{List: []string{"a"}, Batch: -1}, 100, []string{"list: a:", "batch is negative"}},
		{"a glob matching nothing", ItemSource{Files: "**/*.rs"}, 100, []string{"files: **/*.rs:", "yields no items"}},
		{"a glob under a missing directory", ItemSource{Files: "nowhere/*.go"}, 100, []string{"files: nowhere/*.go:", "yields no items"}},
		{"a glob whose walk root is a file", ItemSource{Files: "main.go/*.go"}, 100, []string{"files: main.go/*.go:", "main.go is not a directory"}},
		{"a malformed glob", ItemSource{Files: "internal/[a.go"}, 100, []string{"files: internal/[a.go:", "not a valid glob"}},
		{"a lines file with no non-blank line", ItemSource{Lines: "blank.txt"}, 100, []string{"lines: blank.txt:", "yields no items"}},
		{"a missing lines file", ItemSource{Lines: "missing.txt"}, 100, []string{"lines: missing.txt:"}},
		{"a lines path that is a directory", ItemSource{Lines: "internal"}, 100, []string{"lines: internal:", "not a regular file"}},
		{"a split over a directory with no files", ItemSource{Split: "nowhere"}, 100, []string{"split: nowhere:"}},
		{"a split over a file", ItemSource{Split: "main.go"}, 100, []string{"split: main.go:", "takes a directory"}},
		{"a split with a zero budget", ItemSource{Split: "internal"}, 0, []string{"split: internal:", "no context window"}},
		{"a split with a negative budget", ItemSource{Split: "internal"}, -5, []string{"split: internal:", "no context window"}},
		{"an absolute files path", ItemSource{Files: "/etc/*.conf"}, 100, []string{"files: /etc/*.conf:", "workspace-relative"}},
		{"a .. lines path", ItemSource{Lines: "../secrets.txt"}, 100, []string{"lines: ../secrets.txt:", "workspace-relative"}},
		{"a .. that climbs out after cleaning", ItemSource{Split: "internal/../.."}, 100, []string{"split: internal/../..:", "workspace-relative"}},
		{"a pick stage source", ItemSource{Stage: "claims"}, 100, []string{"stage: claims:", "only once it has run"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			items, err := Expand(tc.source, workspace(), tc.budget)

			if err == nil {
				t.Fatalf("Expand = %q, want an error", unitsOf(items))
			}
			for _, want := range tc.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestNewSplitBudgetReadsTheContextLimitLessTheBriefReserve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		contextLimit int
		want         SplitBudget
	}{
		{0, 0},
		{-1, 0},
		{splitBriefReserveTokens, 0},
		{splitBriefReserveTokens + 1, bytesPerToken},
		{32768, SplitBudget((32768 - splitBriefReserveTokens) * bytesPerToken)},
	}
	for _, tc := range cases {
		if got := NewSplitBudget(tc.contextLimit); got != tc.want {
			t.Errorf("NewSplitBudget(%d) = %d, want %d", tc.contextLimit, got, tc.want)
		}
	}
}

func TestItemNameRespellsADerivedLabelForShow(t *testing.T) {
	t.Parallel()
	const stage = "find"
	dir := filepath.Join(string(filepath.Separator), "home", "u", ".apogee", "workflows", "wf-1")
	inDir := func(parts ...string) string { return filepath.Join(append([]string{dir}, parts...)...) }
	derived := func(units ...string) Item { return Item{Label: itemLabel(units), Units: units} }
	manifest := inDir("stages", "report", "manifest.md")

	cases := []struct {
		name string
		item Item
		want string
	}{
		{name: "the workflow folder itself", item: derived(dir), want: stage},
		{name: "the folder with a trailing separator", item: derived(dir + string(filepath.Separator)), want: stage},
		{name: "a part inside the folder", item: derived(inDir("part-foo")), want: "part-foo"},
		{name: "a nested path inside the folder", item: derived(inDir("a", "b")), want: "a/b"},
		{name: "a sibling sharing the folder's prefix", item: derived(dir + "-other"), want: "wf-1-other"},
		{name: "an absolute path outside the folder", item: derived(filepath.Join(string(filepath.Separator), "elsewhere", "x.md")), want: "x.md"},
		{name: "a workspace-relative path", item: derived("src/main.go"), want: "src/main.go"},
		{name: "a literal entry", item: derived("alpha"), want: "alpha"},
		{name: "several absolute units", item: derived(inDir("part-a"), inDir("part-b"), inDir("part-c")), want: "part-a … part-c (3)"},
		{name: "a merge stage's engine-set label", item: Item{Label: "report", Units: []string{manifest}}, want: "report"},
		{name: "an item with no units", item: Item{Label: "lonely"}, want: "lonely"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ItemName(tc.item, dir, stage)

			if got != tc.want {
				t.Errorf("ItemName(%q) = %q, want %q", tc.item.Label, got, tc.want)
			}
		})
	}
}

func TestItemNameFallsBackToTheLabelWhenTheFolderReadsAsNoStage(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(string(filepath.Separator), "workflows", "wf-1")

	got := ItemName(Item{Label: dir, Units: []string{dir}}, dir, "")

	if got != dir {
		t.Errorf("ItemName with a blank stage = %q, want the label %q", got, dir)
	}
}
