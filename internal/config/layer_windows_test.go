//go:build windows

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// junctionedGlobalHome makes a global config home under dir/real-home holding body as its
// config.yaml, and a junction dir/home pointing at it, answering the global file's path as the
// junction spells it and the home folder it resolves to — a `~/.apogee` reached through a junction.
func junctionedGlobalHome(t *testing.T, dir, body string) (globalPath, realHome string) {
	t.Helper()
	realHome = filepath.Join(dir, "real-home")
	if err := os.Mkdir(realHome, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLayerFile(t, filepath.Join(realHome, "config.yaml"), body)
	home := filepath.Join(dir, "home")
	junctionOrSkip(t, home, realHome)
	return filepath.Join(home, "config.yaml"), realHome
}

// A project file under a Project root reached through a junction is read and layered, with no
// notice: the resolver crosses the junction instead of failing on it.
func TestReadProjectLayerUnderAJunctionedRoot(t *testing.T) {
	t.Parallel()
	root, realDir := junctionedProjectRoot(t)
	if err := os.Mkdir(filepath.Join(realDir, projectConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayerFile(t, filepath.Join(realDir, projectConfigDirName, "config.yaml"), "workflow-retries: 9\n")
	globalPath := filepath.Join(longTempDir(t), "config.yaml")
	writeLayerFile(t, globalPath, "workflow-retries: 2\n")

	var notices []string
	layer, ok := readProjectLayer(projectFilePath(root), globalPath, os.ReadFile,
		func(n string) { notices = append(notices, n) })

	if !ok {
		t.Fatalf("readProjectLayer took no layer under the junctioned root %q; notices = %q", root, notices)
	}
	if want := filepath.Join(root, projectConfigDirName, "config.yaml"); layer.path != want {
		t.Errorf("layer path = %q; want %q, the path as the junction spells it", layer.path, want)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none", notices)
	}
	o, notices := loadLayered(t, globalPath, root)
	if o.WorkflowRetries != 9 {
		t.Errorf("workflow-retries = %d; want the project's 9", o.WorkflowRetries)
	}
	if len(notices) != 0 {
		t.Errorf("layered load notices = %q; want none", notices)
	}
}

// A Project root whose `.apogee/` is a junction to the apogee home, itself reached through a
// junction, is the global file and never layered over itself; both identity checks see the
// junctioned home as the home it resolves to.
func TestSameConfigFileSeesAJunctionedGlobalHome(t *testing.T) {
	t.Parallel()
	dir := longTempDir(t)
	globalPath, realHome := junctionedGlobalHome(t, dir, "mode: auto\nworkflow-retries: 2\n")
	root := filepath.Join(dir, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	junctionOrSkip(t, filepath.Join(root, projectConfigDirName), realHome)

	resolved, err := projectRealPath(projectFilePath(root))
	if err != nil {
		t.Fatalf("resolve the junctioned project file: %v", err)
	}
	if want := filepath.Join(realHome, "config.yaml"); resolved != want {
		t.Errorf("resolved project file = %q; want %q in long-name form", resolved, want)
	}
	if !sameConfigFile(resolved, globalPath) {
		t.Errorf("sameConfigFile(%q, %q) = false; want the junctioned home's file", resolved, globalPath)
	}
	if home := filepath.Dir(globalPath); !samePath(home, realHome) {
		t.Errorf("samePath(%q, %q) = false; want the junction and its target to be one home", home, realHome)
	}

	layered, notices := loadLayered(t, globalPath, root)
	alone, err := LoadFileConfig(globalPath, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if !reflect.DeepEqual(layered, alone) {
		t.Errorf("layered load differs from the global file alone:\n got %+v\nwant %+v", layered, alone)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none — the global file is never layered over itself", notices)
	}
}
