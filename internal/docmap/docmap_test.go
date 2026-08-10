package docmap

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestUnmappedFindsTheFilesTheMapOmits pins the substance of [Check] against temp-dir fixtures:
// what counts as mapped, what is exempt, and the boundary rule that keeps a longer file name from
// vouching for a shorter one. Check itself is the two-line wrapper that hands it os.Getwd.
func TestUnmappedFindsTheFilesTheMapOmits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		doc   string
		files []string
		want  []string
	}{
		{
			name:  "every file named",
			doc:   "// alpha.go leads, beta.go follows.\n// And doc.go this map.\n",
			files: []string{"alpha.go", "beta.go"},
			want:  nil,
		},
		{
			name:  "an unnamed file is reported",
			doc:   "// alpha.go leads.\n// And doc.go this map.\n",
			files: []string{"alpha.go", "orphan.go"},
			want:  []string{"orphan.go"},
		},
		{
			name:  "test files and non-Go files are exempt",
			doc:   "// alpha.go leads.\n// And doc.go this map.\n",
			files: []string{"alpha.go", "alpha_test.go", "testsupport_test.go", "fixture.yaml"},
			want:  nil,
		},
		{
			name:  "a longer name does not vouch for its own tail",
			doc:   "// configwatch.go polls the file.\n// And doc.go this map.\n",
			files: []string{"configwatch.go", "watch.go"},
			want:  []string{"watch.go"},
		},
		{
			name:  "doc.go must name itself",
			doc:   "// alpha.go leads.\n",
			files: []string{"alpha.go"},
			want:  []string{"doc.go"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := fixture(t, tc.doc, tc.files...)
			got, err := unmapped(dir)
			if err != nil {
				t.Fatalf("unmapped: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("unmapped = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUnmappedRejectsAPackageWithNoMap pins the rule that an absent doc.go fails rather than
// reading as a package with nothing left to describe.
func TestUnmappedRejectsAPackageWithNoMap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, "alpha.go", "package fixture\n")

	if _, err := unmapped(dir); err == nil {
		t.Fatal("unmapped accepted a package with no doc.go; a missing map must be a failure")
	}
}

// fixture builds a package directory holding doc as doc.go plus an empty file per name.
func fixture(t *testing.T, doc string, files ...string) string {
	t.Helper()

	dir := t.TempDir()
	write(t, dir, "doc.go", doc)
	for _, name := range files {
		write(t, dir, name, "package fixture\n")
	}
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
