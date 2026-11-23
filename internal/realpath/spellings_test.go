package realpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSpellings_WorkspaceThroughALinkedParent pins the linked spelling: a workspace reached through
// a symlinked parent yields the root spelled through that same unresolved prefix, which resolves
// back to Root, and the workspace's resolved form beside the spelling it was given by.
func TestSpellings_WorkspaceThroughALinkedParent(t *testing.T) {
	t.Parallel()
	plain, link := symlinkedDir(t, finalTempDir(t))
	root := filepath.Join(plain, "proj")
	mkdirAll(t, filepath.Join(root, "ws"))
	workspace := filepath.Join(link, "proj", "ws")

	got := Spellings(root, workspace)

	want := RootSpellings{
		Root:          root,
		LinkedRoot:    filepath.Join(link, "proj"),
		Workspace:     workspace,
		RealWorkspace: filepath.Join(root, "ws"),
	}
	if got != want {
		t.Fatalf("Spellings(%q, %q) = %+v; want %+v", root, workspace, got, want)
	}
	resolved, err := Resolve(got.LinkedRoot)
	if err != nil || resolved != got.Root {
		t.Errorf("Resolve(LinkedRoot %q) = %q, %v; want Root %q", got.LinkedRoot, resolved, err, got.Root)
	}
}

// TestSpellings_WorkspaceLinkToTheRoot pins the zero-climb case: a workspace that is a link to the
// root itself is the root's linked spelling.
func TestSpellings_WorkspaceLinkToTheRoot(t *testing.T) {
	t.Parallel()
	root, link := symlinkedDir(t, finalTempDir(t))

	got := Spellings(root, link)

	want := RootSpellings{Root: root, LinkedRoot: link, Workspace: link, RealWorkspace: root}
	if got != want {
		t.Errorf("Spellings(%q, %q) = %+v; want %+v", root, link, got, want)
	}
}

// TestSpellings_LinkedSpellingResolvingElsewhereIsDropped pins the check that keeps a stranger's
// folder out: a workspace that is itself a link climbs, in its own spelling, to its link's parent,
// which does not resolve to the root and so is no spelling of it.
func TestSpellings_LinkedSpellingResolvingElsewhereIsDropped(t *testing.T) {
	t.Parallel()
	base := finalTempDir(t)
	root := filepath.Join(base, "proj")
	realWorkspace := filepath.Join(root, "ws")
	mkdirAll(t, realWorkspace)
	elsewhere := filepath.Join(base, "elsewhere")
	mkdirAll(t, elsewhere)
	workspace := filepath.Join(elsewhere, "wslink")
	symlinkOrSkip(t, realWorkspace, workspace)

	got := Spellings(root, workspace)

	want := RootSpellings{Root: root, Workspace: workspace, RealWorkspace: realWorkspace}
	if got != want {
		t.Errorf("Spellings(%q, %q) = %+v; want %+v (no LinkedRoot: %q is not the root)",
			root, workspace, got, want, elsewhere)
	}
}

// TestSpellings_PlainPaths pins the cases that add no spelling: a workspace that is its own
// resolved form (below the root, the root itself, or outside it), no workspace, a workspace that
// cannot be resolved, and no Project root.
func TestSpellings_PlainPaths(t *testing.T) {
	t.Parallel()
	base := finalTempDir(t)
	root := filepath.Join(base, "proj")
	below := filepath.Join(root, "ws")
	mkdirAll(t, below)
	outside := filepath.Join(base, "outside")
	mkdirAll(t, outside)
	missing := filepath.Join(base, "missing")

	cases := []struct {
		name            string
		root, workspace string
	}{
		{"workspace below the root", root, below},
		{"root equal to workspace", root, root},
		{"workspace outside the root", root, outside},
		{"no workspace", root, ""},
		{"workspace that does not exist", root, missing},
		{"no project root", "", below},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Spellings(tc.root, tc.workspace)

			want := RootSpellings{Root: tc.root, Workspace: tc.workspace}
			if got != want {
				t.Errorf("Spellings(%q, %q) = %+v; want only the two given spellings %+v",
					tc.root, tc.workspace, got, want)
			}
		})
	}
}

// TestSpellings_FinalNameRoot pins the fifth spelling on a real file system: a root whose
// ResolveLenient form differs from the spelling it was given by (a link here) carries that form,
// and a root that is already its own resolved form carries none.
func TestSpellings_FinalNameRoot(t *testing.T) {
	t.Parallel()
	plain, link := symlinkedDir(t, finalTempDir(t))

	if got := Spellings(link, "").FinalRoot; got != plain {
		t.Errorf("Spellings(%q).FinalRoot = %q; want the resolved root %q", link, got, plain)
	}
	if got := Spellings(plain, "").FinalRoot; got != "" {
		t.Errorf("Spellings(%q).FinalRoot = %q; want none for a root that is its own resolved form", plain, got)
	}
}

// TestSpellings_FinalNameRootOfARespelledVolume pins the case the fifth spelling exists for, with
// the final-path call faked: a root on a volume whose final name differs (a subst or mapped drive
// on Windows) keeps its own spelling in Root and carries the final name beside it, while the
// workspace spellings still come from Resolve. It does not run in parallel because it rewrites
// package state.
func TestSpellings_FinalNameRootOfARespelledVolume(t *testing.T) {
	base := canonicalTempDir(t)
	root := filepath.Join(base, "proj")
	mkdirAll(t, root)
	final := filepath.Join(base, "final")
	swapRealPathSeams(t, filepath.EvalSymlinks,
		fakeFinalPath(func(p string) string { return respell(p, root, final) }),
		nil,
	)

	got := Spellings(root, root)

	want := RootSpellings{Root: root, FinalRoot: final, Workspace: root}
	if got != want {
		t.Errorf("Spellings(%q, %q) = %+v; want %+v", root, root, got, want)
	}
}

// mkdirAll creates dir and its parents, failing the test when it cannot.
func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// symlinkOrSkip makes link a symlink to target, skipping the test where the host refuses to create
// one (Windows without Developer Mode or elevation).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating a symlink needs Developer Mode or elevation here: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}
}
