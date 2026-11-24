package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoFixture builds a repository whose root holds a Project config and a subfolder workspace below
// it, answering the apogee home, the repository root (as the walk spells it) and the workspace. The
// `.git` entry is what lets the walk climb from the workspace to the `.apogee/` above it.
func repoFixture(t *testing.T) (home, root, workspace string) {
	t.Helper()
	home, root = t.TempDir(), t.TempDir()
	workspace = filepath.Join(root, "sub")
	for _, dir := range []string{filepath.Join(root, ".git"), filepath.Join(root, projectConfigDirName), workspace} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	writeLayerFile(t, filepath.Join(home, "config.yaml"), "workflow-retries: 2\n")
	writeLayerFile(t, projectFilePath(root), "workflow-retries: 6\n")
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve %s: %v", root, err)
	}
	return home, real, workspace
}

// collectNotices is a notify that keeps every notice for the test to read.
func collectNotices(notices *[]string) func(string) {
	return func(n string) { *notices = append(*notices, n) }
}

func TestResolveFilesWalksFromTheWorkspace(t *testing.T) {
	t.Parallel()
	home, root, workspace := repoFixture(t)
	var notices []string

	files := ResolveFiles(home, workspace, false, collectNotices(&notices))

	if files.ProjectRoot() != root || files.Workspace() != workspace || files.IsZero() {
		t.Errorf("files = root %q, workspace %q, zero %v; want root %q walked from %q",
			files.ProjectRoot(), files.Workspace(), files.IsZero(), root, workspace)
	}
	if want := projectFilePath(root); files.ProjectPath() != want {
		t.Errorf("ProjectPath = %q; want %q", files.ProjectPath(), want)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none", notices)
	}
}

func TestResolveFilesTakesNoRootWhenGlobalOnly(t *testing.T) {
	t.Parallel()
	home, _, workspace := repoFixture(t)

	files := ResolveFiles(home, workspace, true, noNotify)

	if files.ProjectRoot() != "" || files.ProjectPath() != "" {
		t.Errorf("root %q, project path %q; want none for a global-only source",
			files.ProjectRoot(), files.ProjectPath())
	}
	if want := filepath.Join(home, "config.yaml"); files.GlobalPath() != want {
		t.Errorf("GlobalPath = %q; want %q", files.GlobalPath(), want)
	}
}

func TestResolveFilesSkipsAMissingWorkspaceWithANotice(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "not-yet")
	var notices []string

	files := ResolveFiles(t.TempDir(), missing, false, collectNotices(&notices))

	if files.ProjectRoot() != "" || files.IsZero() {
		t.Errorf("root %q, zero %v; want a source with no root", files.ProjectRoot(), files.IsZero())
	}
	if len(notices) != 1 || !strings.Contains(notices[0], missing) ||
		!strings.Contains(notices[0], "the project layer is skipped") {
		t.Errorf("notices = %q; want one naming %s and the skipped layer", notices, missing)
	}
}

func TestZeroFilesReadsNothing(t *testing.T) {
	t.Parallel()
	var files Files
	read := func(path string) ([]byte, error) {
		t.Errorf("the zero source read %s", path)
		return nil, os.ErrNotExist
	}

	_, _, layeredErr := files.Layered(read, noNotify)
	reread, _, rereadErr := files.Reread(false, read, noNotify)

	if layeredErr != nil || rereadErr != nil {
		t.Errorf("Layered = %v, Reread = %v; want no error", layeredErr, rereadErr)
	}
	if !reread.Files.IsZero() {
		t.Errorf("Reread recorded %+v; want the zero source", reread.Files)
	}
	if !files.IsZero() || files.GlobalPath() != "" || files.ProjectPath() != "" || files.WorkspacesDir() != "" {
		t.Errorf("zero source = (%v, %q, %q, %q); want zero with no paths",
			files.IsZero(), files.GlobalPath(), files.ProjectPath(), files.WorkspacesDir())
	}
}

func TestFilesProjectPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	home := filepath.Join(dir, projectConfigDirName)
	repo := filepath.Join(dir, "repo")
	defaultHome, err := ApogeeHome("")
	if err != nil {
		t.Skipf("no user home to default to: %v", err)
	}
	tests := []struct {
		name           string
		files          Files
		wantProject    string
		wantGlobal     string
		wantWorkspaces string
	}{
		{
			name:           "a Project root",
			files:          FilesAt(home, repo, repo),
			wantProject:    projectFilePath(repo),
			wantGlobal:     filepath.Join(home, "config.yaml"),
			wantWorkspaces: filepath.Join(home, workspacesDirName),
		},
		{
			name:           "no Project root",
			files:          FilesAt(home, repo, ""),
			wantGlobal:     filepath.Join(home, "config.yaml"),
			wantWorkspaces: filepath.Join(home, workspacesDirName),
		},
		{
			name:           "a root whose project file is the global file",
			files:          FilesAt(home, dir, dir),
			wantGlobal:     filepath.Join(home, "config.yaml"),
			wantWorkspaces: filepath.Join(home, workspacesDirName),
		},
		{
			name:           "a global-only source",
			files:          ResolveFiles(home, repo, true, noNotify),
			wantGlobal:     filepath.Join(home, "config.yaml"),
			wantWorkspaces: filepath.Join(home, workspacesDirName),
		},
		{
			name:           "the default home",
			files:          FilesAt("", repo, repo),
			wantProject:    projectFilePath(repo),
			wantGlobal:     filepath.Join(defaultHome, "config.yaml"),
			wantWorkspaces: filepath.Join(defaultHome, workspacesDirName),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project, global, workspaces := tt.files.ProjectPath(), tt.files.GlobalPath(), tt.files.WorkspacesDir()

			if project != tt.wantProject || global != tt.wantGlobal || workspaces != tt.wantWorkspaces {
				t.Errorf("paths = (%q, %q, %q); want (%q, %q, %q)", project, global, workspaces,
					tt.wantProject, tt.wantGlobal, tt.wantWorkspaces)
			}
		})
	}
}

// A re-read never migrates: a global file still written in a retired shape is refused, its bytes
// untouched and no backup written beside it.
func TestFilesRereadRefusesARetiredShape(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	const retired = "endpoint: http://box:1111\nmode: plan\n"
	path := filepath.Join(home, "config.yaml")
	writeLayerFile(t, path, retired)

	_, _, err := FilesAt(home, "", "").Reread(false, os.ReadFile, noNotify)

	if !errors.Is(err, ErrRetiredShape) {
		t.Fatalf("Reread = %v; want ErrRetiredShape", err)
	}
	if data, readErr := os.ReadFile(path); readErr != nil || string(data) != retired {
		t.Errorf("config.yaml = %q (%v); want it untouched", data, readErr)
	}
	assertHomeHoldsOnlyConfig(t, home, "Reread")
}

// A re-read takes the layer from the root its source names, never a fresh walk: a workspace that now
// holds a `.apogee/` of its own does not move it, and the options record the source they came from.
func TestFilesRereadKeepsItsRoot(t *testing.T) {
	t.Parallel()
	home, root, workspace := repoFixture(t)
	writeLayerFile(t, filepath.Join(home, "config.yaml"), "workflow-retries: 2\n"+startupServerYAML)
	if err := os.Mkdir(filepath.Join(workspace, projectConfigDirName), 0o700); err != nil {
		t.Fatalf("create the workspace's .apogee: %v", err)
	}
	writeLayerFile(t, projectFilePath(workspace), "workflow-retries: 9\n")
	files := FilesAt(home, workspace, root)

	o, _, err := files.Reread(false, os.ReadFile, noNotify)

	if err != nil {
		t.Fatalf("Reread: %v", err)
	}
	if o.WorkflowRetries != 6 || !o.ProjectKeys["workflow-retries"] {
		t.Errorf("workflow-retries = %d (project %v); want the booted root's 6",
			o.WorkflowRetries, o.ProjectKeys["workflow-retries"])
	}
	if o.Files != files {
		t.Errorf("options recorded %+v; want the source they were read from %+v", o.Files, files)
	}
}

// Every return of the startup resolution leaves the files it read on Options.Files — the refusals
// and the undetermined startup server as well as a clean resolution — so a later ConfigFiles never
// walks again.
func TestResolveOptionsRecordsTheFilesItRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		global  string
		wantErr bool
	}{
		{name: "a clean resolution", global: startupServerYAML},
		{name: "a global file that does not parse", global: "mode: [plan\n", wantErr: true},
		{name: "a refused servers entry", global: "servers:\n  - name: half\n", wantErr: true},
		{name: "an undetermined startup server", global: "mode: plan\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, tt.global, "workflow-retries: 6\n")
			wantRoot, err := filepath.EvalSymlinks(projectRoot)
			if err != nil {
				t.Fatalf("resolve %s: %v", projectRoot, err)
			}
			opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot}

			_, err = ApplyConfigOffer(&opts, func(string) bool { return false },
				func(string) string { return "" }, os.ReadFile, noNotify)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ApplyConfigOffer = %v; want error %v", err, tt.wantErr)
			}
			if opts.Files.GlobalPath() != globalPath || opts.Files.ProjectRoot() != wantRoot {
				t.Errorf("recorded files = (%q, %q); want (%q, %q)",
					opts.Files.GlobalPath(), opts.Files.ProjectRoot(), globalPath, wantRoot)
			}
			if opts.ConfigFiles() != opts.Files {
				t.Errorf("ConfigFiles = %+v; want the recorded %+v", opts.ConfigFiles(), opts.Files)
			}
		})
	}
}
