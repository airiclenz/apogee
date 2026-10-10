package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/airiclenz/apogee/internal/adoption"
)

// projectWriteFixture is a fresh Project root and the workspaces folder its lock is kept under —
// apart, as `~/.apogee/workspaces` and a repository are.
func projectWriteFixture(t *testing.T) (root, workspaces string) {
	t.Helper()
	return t.TempDir(), filepath.Join(t.TempDir(), "workspaces")
}

// appendSplice is a splice that appends text to whatever the file holds and records the bytes it
// was handed.
func appendSplice(text string, seen *[]byte) editSplice {
	return func(_ fileConfig, data []byte) ([]byte, error) {
		*seen = append([]byte{}, data...)
		return append(append([]byte{}, data...), text...), nil
	}
}

// acceptAll is a verify step that accepts every edit.
func acceptAll(_, _ fileConfig, _ []byte) error { return nil }

// treeOf lists every path under dir, relative and slash-separated, sorted.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(paths)
	return paths
}

func TestProjectWriteFirstWriteCreatesOnlyTheConfig(t *testing.T) {
	t.Parallel()

	root, workspaces := projectWriteFixture(t)
	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err != nil {
		t.Fatalf("editProject: %v", err)
	}

	if len(seen) != 0 {
		t.Errorf("the first write started from %q, want an empty document (never the global template)", seen)
	}
	if got, want := treeOf(t, root), []string{".apogee", ".apogee/config.yaml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the repository holds %v, want only %v (no lock, no .gitignore, no temp file)", got, want)
	}
	data, err := os.ReadFile(filepath.Join(root, ".apogee", "config.yaml"))
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if string(data) != "use-project-skills: false\n" {
		t.Errorf("project config = %q, want the splice's text alone", data)
	}
	lock, err := adoption.ConfigLockPath(workspaces, root)
	if err != nil {
		t.Fatalf("ConfigLockPath: %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the transaction's lock is not under the workspaces folder at %s: %v", lock, err)
	}
}

func TestProjectWriteCreatesWithTheStatedModes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}

	root, workspaces := projectWriteFixture(t)
	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err != nil {
		t.Fatalf("editProject: %v", err)
	}
	for path, stated := range map[string]os.FileMode{
		filepath.Join(root, ".apogee"):                projectDirPerm,
		filepath.Join(root, ".apogee", "config.yaml"): projectFilePerm,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got&^stated != 0 {
			t.Errorf("mode of %s = %o, wider than the stated %o", path, got, stated)
		}
	}
}

func TestProjectWriteKeepsWhatTheFileSaysAndItsMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}

	root, workspaces := projectWriteFixture(t)
	path := filepath.Join(root, ".apogee", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "context-files:\n  enable: false\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err != nil {
		t.Fatalf("editProject: %v", err)
	}
	if string(seen) != existing {
		t.Errorf("the splice was handed %q, want the file's own bytes %q", seen, existing)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if want := existing + "use-project-skills: false\n"; string(data) != want {
		t.Errorf("project config = %q, want %q", data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want the file's own 600 preserved", got)
	}
}

func TestProjectWriteThatWritesNothingLeavesNoTrace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		splice  editSplice
		verify  editVerify
		wantErr bool
	}{
		{"nothing to write", func(fileConfig, []byte) ([]byte, error) { return nil, nil }, acceptAll, false},
		{"splice refuses", func(fileConfig, []byte) ([]byte, error) { return nil, errors.New("no") }, acceptAll, true},
		{"verify refuses", func(_ fileConfig, data []byte) ([]byte, error) {
			return append(data, "use-project-skills: false\n"...), nil
		}, func(_, _ fileConfig, _ []byte) error { return errors.New("no") }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, workspaces := projectWriteFixture(t)
			err := editProject(root, workspaces, tc.splice, tc.verify)
			if (err != nil) != tc.wantErr {
				t.Fatalf("editProject error = %v, want error %v", err, tc.wantErr)
			}
			if got := treeOf(t, root); len(got) != 0 {
				t.Errorf("an edit that wrote nothing left %v in the repository", got)
			}
		})
	}
}

func TestProjectWriteRefusesASymlink(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		link func(t *testing.T, root, elsewhere string) error
	}{
		{"symlinked .apogee", func(_ *testing.T, root, elsewhere string) error {
			return os.Symlink(elsewhere, filepath.Join(root, ".apogee"))
		}},
		{"symlinked config.yaml", func(t *testing.T, root, elsewhere string) error {
			if err := os.Mkdir(filepath.Join(root, ".apogee"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			return os.Symlink(filepath.Join(elsewhere, "config.yaml"), filepath.Join(root, ".apogee", "config.yaml"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, workspaces := projectWriteFixture(t)
			elsewhere := t.TempDir()
			target := filepath.Join(elsewhere, "config.yaml")
			if err := os.WriteFile(target, []byte("mode: plan\n"), 0o600); err != nil {
				t.Fatalf("write target: %v", err)
			}
			if err := tc.link(t, root, elsewhere); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			var seen []byte
			if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err == nil {
				t.Fatal("editProject followed a symlink in the repository")
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			if string(data) != "mode: plan\n" {
				t.Errorf("the link's target was rewritten to %q", data)
			}
			if got, want := treeOf(t, elsewhere), []string{"config.yaml"}; !reflect.DeepEqual(got, want) {
				t.Errorf("the link's folder holds %v, want only %v", got, want)
			}
		})
	}
}

func TestProjectWriteRefusesAnApogeeThatIsNotAFolder(t *testing.T) {
	t.Parallel()

	root, workspaces := projectWriteFixture(t)
	if err := os.WriteFile(filepath.Join(root, ".apogee"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err == nil {
		t.Fatal("editProject wrote under a .apogee that is a file")
	}
}

func TestProjectWriteRefusesAMissingRoot(t *testing.T) {
	t.Parallel()

	_, workspaces := projectWriteFixture(t)
	var seen []byte
	for _, root := range []string{"", filepath.Join(t.TempDir(), "absent")} {
		if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err == nil {
			t.Errorf("editProject(%q) succeeded", root)
		}
	}
}

// Not parallel: it points HOME at the fixture.
func TestProjectWriteRefusesTheHomeFolder(t *testing.T) {
	root, workspaces := projectWriteFixture(t)
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	var seen []byte
	if err := editProject(root, workspaces, appendSplice("use-project-skills: false\n", &seen), acceptAll); err == nil {
		t.Fatal("editProject wrote the home folder's .apogee/config.yaml — the global config")
	}
	if got := treeOf(t, root); len(got) != 0 {
		t.Errorf("a refused write left %v in the home folder", got)
	}
}
