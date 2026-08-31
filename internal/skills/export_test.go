package skills

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// ExportShipped — the one way a shipped skill reaches disk
// ----------------------------------------------------------------------------

// shippedTree reads the embedded folder of one shipped skill as a rel-path → bytes map, which is
// what an export is measured against: the copy has to be the same files with the same contents.
func shippedTree(t *testing.T, id string) map[string][]byte {
	t.Helper()
	want := map[string][]byte{}
	root := path.Join(shippedDir, id)
	err := fs.WalkDir(shippedFiles, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(shippedFiles, p)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		want[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("reading the embedded %q tree: %v", id, err)
	}
	if len(want) == 0 {
		t.Fatalf("the embedded shipped skill %q holds no files", id)
	}
	return want
}

// The export is a copy, not a render: every bundled file comes out beside the SKILL.md with its
// bytes unchanged, because the copy's whole purpose is to be the same skill in a folder the human
// can open.
func TestExportShippedWritesEveryFileVerbatim(t *testing.T) {
	t.Parallel()

	lib := filepath.Join(t.TempDir(), "skills")
	dir, err := ExportShipped("debugging", lib)
	if err != nil {
		t.Fatalf("ExportShipped: %v", err)
	}

	if want := filepath.Join(lib, "debugging"); dir != want {
		t.Errorf("exported dir = %q, want %q", dir, want)
	}
	for rel, want := range shippedTree(t, "debugging") {
		got, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Errorf("the export is missing %q: %v", rel, readErr)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%q was not copied verbatim (%d bytes, want %d)", rel, len(got), len(want))
		}
	}
}

// A second export never overwrites the first: the copy is a file the human has been editing, and
// silently replacing it is exactly the loss the refusal exists to prevent. The edit survives.
func TestExportShippedRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	lib := filepath.Join(t.TempDir(), "skills")
	dir, err := ExportShipped("debugging", lib)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	edited := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(edited, []byte("MY OWN VERSION"), 0o600); err != nil {
		t.Fatalf("editing the copy: %v", err)
	}

	if _, err := ExportShipped("debugging", lib); err == nil {
		t.Fatal("the second export was allowed; it would replace work in progress")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal = %v, want it to say the skill is already there", err)
	}
	if got, _ := os.ReadFile(edited); string(got) != "MY OWN VERSION" {
		t.Errorf("the edited copy = %q, want it untouched by the refused export", got)
	}
}

// Only a shipped id is exportable, and a miss teaches the vocabulary rather than only reporting a
// miss — the id came from a human typing it, so the list of what they could have meant is the
// useful half of the refusal.
func TestExportShippedRefusesAnUnknownID(t *testing.T) {
	t.Parallel()

	lib := filepath.Join(t.TempDir(), "skills")
	_, err := ExportShipped("no-such-skill", lib)
	if err == nil {
		t.Fatal("an unknown id exported something")
	}
	for _, want := range append([]string{"no-such-skill"}, ShippedIDs()...) {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	if _, statErr := os.Stat(lib); statErr == nil {
		t.Error("a refused export created the library directory anyway")
	}
}

// An id is a NAME, never a path: the refusal is stated on the spelling rather than reached by the
// lookup happening to miss, so nothing about what the embedded tree contains can make one of these
// resolve.
func TestExportShippedRefusesAPathShapedID(t *testing.T) {
	t.Parallel()

	lib := filepath.Join(t.TempDir(), "skills")
	for _, id := range []string{"", ".", "..", "../debugging", "sub/debugging", `sub\debugging`, ".hidden", "two words"} {
		if _, err := ExportShipped(id, lib); err == nil {
			t.Errorf("ExportShipped(%q) was allowed; an id is a name, not a path", id)
		}
	}
}

// An unresolved library root is refused rather than turned into a relative write out of whatever
// directory the process happens to be standing in.
func TestExportShippedRefusesAnEmptyLibraryDir(t *testing.T) {
	t.Parallel()

	if _, err := ExportShipped("debugging", ""); err == nil {
		t.Fatal("an empty library dir was accepted; the export would land in the working directory")
	}
}

// ShippedIDs is the vocabulary both the refusals and the /skills export form quote, so it must name
// exactly the folders the embedded tree carries — sorted, as fs.ReadDir hands them over.
func TestShippedIDsNamesTheEmbeddedFolders(t *testing.T) {
	t.Parallel()

	ids := ShippedIDs()
	if len(ids) == 0 {
		t.Fatal("no shipped skills are compiled in")
	}
	for i := range ids {
		if i > 0 && ids[i-1] >= ids[i] {
			t.Errorf("ShippedIDs() = %v, want it sorted and free of duplicates", ids)
			break
		}
		if _, err := fs.Stat(shippedFiles, path.Join(shippedDir, ids[i], skillFileName)); err != nil {
			t.Errorf("shipped id %q has no %s: %v", ids[i], skillFileName, err)
		}
	}
}
