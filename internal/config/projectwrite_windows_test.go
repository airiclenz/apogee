//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// longTempDir is a fresh temp folder in the long-name spelling the resolver reports: a runner's
// TEMP can be the 8.3 short name (RUNNER~1), which a path built from t.TempDir() would keep.
func longTempDir(t *testing.T) string {
	t.Helper()
	dir, err := projectRealPath(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return dir
}

// junctionOrSkip makes link an NTFS junction pointing at the existing folder target. A filesystem
// that refuses junctions skips the test: there is no junction to write under.
func junctionOrSkip(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("junctions are not available here: %v (%s)", err, out)
	}
}

// junctionedProjectRoot makes dir/real and a junction dir/link pointing at it, returning the
// Project root as the junction spells it and the folder it resolves to.
func junctionedProjectRoot(t *testing.T) (underJunction, realDir string) {
	t.Helper()
	dir := longTempDir(t)
	realDir = filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	underJunction = filepath.Join(dir, "link")
	junctionOrSkip(t, underJunction, realDir)
	return underJunction, realDir
}

// A project setting saved under a Project root reached through a junction lands in the junction
// target's `.apogee/config.yaml`.
func TestProjectWriteSavesUnderAJunctionedRoot(t *testing.T) {
	t.Parallel()
	root, realDir := junctionedProjectRoot(t)
	workspaces := filepath.Join(t.TempDir(), "workspaces")

	if err := SaveProjectSetting(root, workspaces, "use-project-skills", "false"); err != nil {
		t.Fatalf("SaveProjectSetting under a junctioned root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(realDir, ".apogee", "config.yaml"))
	if err != nil {
		t.Fatalf("read the junction target's project config: %v", err)
	}
	if string(data) != "use-project-skills: false\n" {
		t.Errorf("project config =\n%s\nwant the one line it states", data)
	}
}

// Under a junctioned Project root the containment check still refuses a config that resolves
// outside the root: a `.apogee/` that is itself a junction to another folder is not written.
func TestProjectWriteUnderAJunctionedRootRefusesAPathOutside(t *testing.T) {
	t.Parallel()
	root, realDir := junctionedProjectRoot(t)
	workspaces := filepath.Join(t.TempDir(), "workspaces")
	elsewhere := filepath.Join(filepath.Dir(realDir), "elsewhere")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(elsewhere, "config.yaml")
	if err := os.WriteFile(target, []byte("mode: plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	junctionOrSkip(t, filepath.Join(realDir, ".apogee"), elsewhere)

	resolvedRoot, err := projectRealPath(root)
	if err != nil {
		t.Fatalf("resolve the junctioned root: %v", err)
	}
	if err := insideRoot(resolvedRoot, projectFilePath(root)); err == nil {
		t.Errorf("insideRoot accepted %q, which resolves to %q outside %q", projectFilePath(root), target, resolvedRoot)
	}

	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err == nil {
		t.Fatal("editProject wrote through a .apogee junction that leaves the Project root")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "mode: plan\n" {
		t.Errorf("the junction's target was rewritten to %q", data)
	}
}
