package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestResolveFollowsSymlinks — the filter and the root are compared through this one
// resolution, so a workspace reached through a link must reduce to the same string either way.
func TestResolveFollowsSymlinks(t *testing.T) {
	t.Parallel()

	realDir := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("this host cannot create a symlink: %v", err)
	}

	viaLink, err := Resolve(link)
	direct, directErr := Resolve(realDir)

	if err != nil || directErr != nil {
		t.Fatalf("Resolve returned %v / %v, want no error", err, directErr)
	}
	want, symErr := filepath.EvalSymlinks(realDir)
	if symErr != nil {
		t.Fatalf("EvalSymlinks(%q): %v", realDir, symErr)
	}
	if viaLink != want || direct != want {
		t.Errorf("Resolve(link) = %q and Resolve(real) = %q, want both %q", viaLink, direct, want)
	}
}

// TestResolveEmptyIsTheUnsetFilter — an entry that scopes itself to nothing is active
// everywhere, and callers get that without special-casing the empty string first.
func TestResolveEmptyIsTheUnsetFilter(t *testing.T) {
	t.Parallel()

	got, err := Resolve("")

	if err != nil || got != "" {
		t.Errorf("Resolve(\"\") = (%q, %v), want (\"\", nil)", got, err)
	}
}

// TestResolveExpandsALeadingTilde — a config may name the path the way a shell would.
func TestResolveExpandsALeadingTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("this host has no home directory: %v", err)
	}

	got, err := Resolve("~/projects/apogee")

	if err != nil {
		t.Fatalf("Resolve = %v, want no error", err)
	}
	want, _ := Resolve(filepath.Join(home, "projects", "apogee"))
	if got != want {
		t.Errorf("Resolve(\"~/projects/apogee\") = %q, want %q", got, want)
	}
}

// TestResolveLeavesANonLeadingTildeAlone — `~` is a legal filename character.
func TestResolveLeavesANonLeadingTildeAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	odd := filepath.Join(dir, "backup~")

	got, err := Resolve(odd)

	if err != nil {
		t.Fatalf("Resolve = %v, want no error", err)
	}
	if !strings.HasSuffix(got, "backup~") {
		t.Errorf("Resolve(%q) = %q, want the trailing ~ preserved", odd, got)
	}
}

// TestActiveAtKeepsTheUnscopedAndTheMatchingEntries — an unset filter is active at every root, a
// filter naming this root is active here, and one naming another root is dropped; the survivors
// keep the order the list wrote.
func TestActiveAtKeepsTheUnscopedAndTheMatchingEntries(t *testing.T) {
	t.Parallel()

	root, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	list := []domain.Reaction{
		{ID: "everywhere"},
		{ID: "elsewhere", Workspace: t.TempDir()},
		{ID: "here", Workspace: root},
	}

	active, err := ActiveAt(list, root)

	if err != nil {
		t.Fatalf("ActiveAt = %v, want no error", err)
	}
	if len(active) != 2 || active[0].ID != "everywhere" || active[1].ID != "here" {
		t.Errorf("ActiveAt kept %+v, want [everywhere here]", active)
	}
}

// TestActiveAtWithNoRootDropsEveryScopedEntry — a root with no workspace matches no filter, so only
// the unscoped entries stay active there.
func TestActiveAtWithNoRootDropsEveryScopedEntry(t *testing.T) {
	t.Parallel()

	list := []domain.Reaction{{ID: "scoped", Workspace: t.TempDir()}, {ID: "everywhere"}}

	active, err := ActiveAt(list, "")

	if err != nil || len(active) != 1 || active[0].ID != "everywhere" {
		t.Errorf("ActiveAt = (%+v, %v), want only the unscoped entry", active, err)
	}
}
