package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/projectroot"
)

// projectRootNotice is the line a workspace whose Project root cannot be resolved costs.
const projectRootNotice = "apogee: project config: %v; the project layer is skipped"

// Files is the one config source: which files a resolution reads — the global config.yaml of an
// apogee home and the Project config of the Project root walked from a workspace (ADR 0096 §6) —
// fixed once and carried on [Options.Files], so every later read of the same session asks the same
// files rather than walking to a root again. A `.apogee/` or `git init` that appears below the
// root mid-session therefore never moves, drops or adds the project layer a re-read applies.
//
// The zero value is no source at all and reads nothing; [ResolveFiles] and [FilesAt] are the two
// ways to name one. A Files is a plain value: copies share nothing and may be read concurrently.
type Files struct {
	configDir  string
	workspace  string
	root       string
	globalOnly bool
	resolved   bool
}

// ResolveFiles names the files a resolution reads for the apogee home configDir (empty ⇒ the
// ~/.apogee default) and workspace (empty ⇒ the current directory): the global file, and the
// Project root walked from workspace (internal/projectroot). globalOnly takes no Project root — the
// daemon's startup resolution ([Options.GlobalConfigOnly]). A workspace that cannot be read (a
// `--workspace` that does not exist yet) takes no root either, with one notice to notify, rather
// than a guess at a root above a folder that is not there.
func ResolveFiles(configDir, workspace string, globalOnly bool, notify func(string)) Files {
	files := Files{configDir: configDir, workspace: workspace, globalOnly: globalOnly, resolved: true}
	if !globalOnly {
		files.root = resolveProjectRoot(workspace, notify)
	}
	return files
}

// FilesAt names the files of the apogee home configDir and the Project root projectRoot a caller
// already holds, walking nothing; an empty projectRoot is no project layer. workspace is the
// workspace that root belongs to, carried for the re-read ([Files.Reread]).
func FilesAt(configDir, workspace, projectRoot string) Files {
	return Files{configDir: configDir, workspace: workspace, root: projectRoot, resolved: true}
}

// IsZero reports whether f names no source — the zero value, which reads nothing.
func (f Files) IsZero() bool { return !f.resolved }

// Workspace is the workspace f was resolved for, as given (empty ⇒ the current directory).
func (f Files) Workspace() string { return f.workspace }

// GlobalPath is the global config file f reads, or "" for the zero value or an apogee home that
// cannot be resolved.
func (f Files) GlobalPath() string {
	if f.IsZero() {
		return ""
	}
	return FilePath(f.configDir)
}

// ProjectRoot is the Project root f layers, or "" when it layers none.
func (f Files) ProjectRoot() string { return f.root }

// ProjectPath is the Project config file f layers over the global one, or "" when there is none to
// read: a global-only source, no Project root, or a root whose project file IS the global file (a
// workspace at the apogee home's parent), which is never layered over itself.
func (f Files) ProjectPath() string {
	if f.globalOnly {
		return ""
	}
	path := projectFilePath(f.root)
	if path == "" || filepath.Clean(path) == filepath.Clean(f.GlobalPath()) {
		return ""
	}
	return path
}

// WorkspacesDir is where f's apogee home keeps each Project root's adoption record, or "" when no
// global path is known (the zero value).
func (f Files) WorkspacesDir() string { return workspacesDirOf(f.GlobalPath()) }

// Layered reads the global file with the Project config layered over it, the way a live re-read
// reads them ([LoadLayeredOffer]): it never migrates and never writes, a defect in the global file
// is the error, and one in the project file is a notice. The zero value reads nothing and answers
// the options of no file at all.
func (f Files) Layered(readFile func(string) ([]byte, error),
	notify func(string)) (Options, ProjectOffer, error) {
	return LoadLayeredOffer(f.GlobalPath(), f.ProjectRoot(), readFile, notify)
}

// Reread is the startup resolution ([ApplyConfigOffer]) over f's files alone, for a re-read under
// a running session: no flag was set and no variable is in the environment, so the values are the
// files' own, and nothing migrates — a file still written in a retired shape is refused with
// [ErrRetiredShape], bytes untouched, no `config.yaml.bak-*`. The answered options record f as
// their source. serverFlagBound carries the session's own `--endpoint`/`--model` binding
// ([Options.ServerFlagBound]), the one invocation fact that shapes which server the files select.
// [StartupUndetermined] comes back with the options and the offer filled, as ApplyConfigOffer
// returns it. The zero value reads nothing and answers the zero options.
func (f Files) Reread(serverFlagBound bool, readFile func(string) ([]byte, error),
	notify func(string)) (Options, ProjectOffer, error) {
	if f.IsZero() {
		return Options{}, ProjectOffer{}, nil
	}
	next := Options{
		ConfigDir:        f.configDir,
		Workspace:        f.workspace,
		GlobalConfigOnly: f.globalOnly,
		ServerFlagBound:  serverFlagBound,
		Files:            f,
	}
	offer, err := applyConfigOffer(&next, noFlagSet, noVariableSet, readFile, notify, false)
	return next, offer, err
}

// ConfigFiles is the config source o was resolved from ([Options.Files]), or, for an Options no
// resolution filled, the source its ConfigDir, Workspace and GlobalConfigOnly name, walked fresh
// with any notice dropped.
func (o Options) ConfigFiles() Files {
	if !o.Files.IsZero() {
		return o.Files
	}
	return ResolveFiles(o.ConfigDir, o.Workspace, o.GlobalConfigOnly, func(string) {})
}

// noFlagSet and noVariableSet are the invocation a re-read resolves under: no flag changed and no
// environment variable set, so the files alone answer.
func noFlagSet(string) bool       { return false }
func noVariableSet(string) string { return "" }

// resolveProjectRoot is the Project root of workspace — the current directory when it is empty, as
// resolveRoots resolves it — or "" when there is no project layer to read. A workspace that cannot
// be read (a `--workspace` that does not exist yet) skips the layer with a notice rather than
// guessing at a root above a folder that is not there.
func resolveProjectRoot(workspace string, notify func(string)) string {
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			notify(fmt.Sprintf(projectRootNotice, fmt.Errorf("resolve working directory: %w", err)))
			return ""
		}
		workspace = cwd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		notify(fmt.Sprintf(projectRootNotice, fmt.Errorf("resolve workspace %s: %w", workspace, err)))
		return ""
	}
	if _, err := os.Stat(abs); err != nil {
		notify(fmt.Sprintf(projectRootNotice, err))
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // only the home guard is lost; the walk still stops at the git top-level
	}
	return projectroot.Resolve(abs, home)
}
