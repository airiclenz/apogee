package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// layerFixture writes a global config home and a Project root side by side under one temp dir and
// answers the global file's path and the root. An empty body writes no file, so a case can state
// "no global file" or "no project file" without a second helper.
func layerFixture(t *testing.T, global, project string) (globalPath, projectRoot string) {
	t.Helper()

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	projectRoot = filepath.Join(dir, "project")
	for _, d := range []string{home, filepath.Join(projectRoot, projectConfigDirName)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("create %s: %v", d, err)
		}
	}
	globalPath = filepath.Join(home, "config.yaml")
	writeLayerFile(t, globalPath, global)
	writeLayerFile(t, projectFilePath(projectRoot), project)
	return globalPath, projectRoot
}

// writeLayerFile writes body to path, or nothing when body is empty.
func writeLayerFile(t *testing.T, path, body string) {
	t.Helper()

	if body == "" {
		return
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// loadLayered is LoadLayeredConfig over the real files, answering the resolved options and every
// notice the load produced.
func loadLayered(t *testing.T, globalPath, projectRoot string) (Options, []string) {
	t.Helper()

	var notices []string
	o, err := LoadLayeredConfig(globalPath, projectRoot, os.ReadFile,
		func(n string) { notices = append(notices, n) })
	if err != nil {
		t.Fatalf("LoadLayeredConfig: %v", err)
	}
	return o, notices
}

// A project-param key the project file states outranks the global file's value, and the run
// records that the project layer supplied it.
func TestLayeredConfigOverlaysProjectParamKeys(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t,
		"workflow-retries: 2\nworkflow-continuations: 3\ncontext-files:\n  names: [AGENTS.md]\n",
		"workflow-retries: 5\ncontext-files:\n  names: [CLAUDE.md]\n")

	o, notices := loadLayered(t, globalPath, projectRoot)

	if o.WorkflowRetries != 5 {
		t.Errorf("workflow-retries = %d; want the project's 5", o.WorkflowRetries)
	}
	if o.WorkflowContinuations != 3 {
		t.Errorf("workflow-continuations = %d; want the global 3 the project left alone", o.WorkflowContinuations)
	}
	if !slices.Equal(o.ContextFiles, []string{"CLAUDE.md"}) {
		t.Errorf("context-files.names = %v; want the project's [CLAUDE.md]", o.ContextFiles)
	}
	want := map[string]bool{"workflow-retries": true, "context-files.names": true}
	if !reflect.DeepEqual(o.ProjectKeys, want) {
		t.Errorf("ProjectKeys = %v; want %v", o.ProjectKeys, want)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %q; want none", notices)
	}
}

// A tighten-only list is the union of both files — global entries first — so a project can add a
// disabled tool or a denied host but never remove one; a project list the global one already
// holds contributes nothing.
func TestLayeredConfigUnionsTightenOnlyLists(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t,
		"tools:\n  disabled: [web_fetch, terminal]\nurl-safety:\n  allow-hosts: [a.example]\n"+
			"  deny-hosts: [x.example]\n",
		"tools:\n  disabled: [terminal, web_search]\nurl-safety:\n  deny-hosts: [x.example]\n")

	o, _ := loadLayered(t, globalPath, projectRoot)

	if want := []string{"web_fetch", "terminal", "web_search"}; !slices.Equal(o.ToolsDisabled, want) {
		t.Errorf("tools.disabled = %v; want the union %v", o.ToolsDisabled, want)
	}
	if want := []string{"x.example"}; !slices.Equal(o.URLDenyHosts, want) {
		t.Errorf("url-safety.deny-hosts = %v; want %v", o.URLDenyHosts, want)
	}
	if want := []string{"a.example"}; !slices.Equal(o.URLAllowHosts, want) {
		t.Errorf("url-safety.allow-hosts = %v; want the global %v untouched", o.URLAllowHosts, want)
	}
	if want := map[string]bool{"tools.disabled": true}; !reflect.DeepEqual(o.ProjectKeys, want) {
		t.Errorf("ProjectKeys = %v; want %v — a project list adding nothing contributes nothing",
			o.ProjectKeys, want)
	}
}

// A tighten-only list the global file does not state at all is the project's list as written.
func TestLayeredConfigTakesATightenOnlyListTheGlobalFileLacks(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", "url-safety:\n  deny-hosts: [x.example]\n")

	o, _ := loadLayered(t, globalPath, projectRoot)

	if want := []string{"x.example"}; !slices.Equal(o.URLDenyHosts, want) {
		t.Errorf("url-safety.deny-hosts = %v; want %v", o.URLDenyHosts, want)
	}
}

// A global-only key in the project file is ignored, and ONE notice names every such key and the
// project path, while the keys the layer honours still land.
func TestLayeredConfigDropsGlobalOnlyKeysWithOneNotice(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "mode: plan\n",
		"mode: auto\nbypass: true\nui:\n  spinner: dots\nworkflow-retries: 4\n")

	o, notices := loadLayered(t, globalPath, projectRoot)

	if o.Mode != "plan" || o.Bypass {
		t.Errorf("mode = %q, bypass = %v; want the global plan and no bypass", o.Mode, o.Bypass)
	}
	if o.WorkflowRetries != 4 {
		t.Errorf("workflow-retries = %d; want the project's 4", o.WorkflowRetries)
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %q; want exactly one", notices)
	}
	for _, want := range []string{projectFilePath(projectRoot), `"mode"`, `"bypass"`, `"ui.spinner"`, globalPath} {
		if !strings.Contains(notices[0], want) {
			t.Errorf("notice %q does not name %s", notices[0], want)
		}
	}
}

// No project layer to read — an empty Project root, a root with no config file, a project file
// that is the global file reached through a symlink — resolves exactly as the global file alone.
func TestLayeredConfigWithoutAProjectLayerIsTheGlobalFileAlone(t *testing.T) {
	t.Parallel()
	const global = "mode: auto\nworkflow-retries: 2\ntools:\n  disabled: [terminal]\n"
	cases := map[string]func(t *testing.T) (globalPath, projectRoot string){
		"empty root": func(t *testing.T) (string, string) {
			globalPath, _ := layerFixture(t, global, "workflow-retries: 9\n")
			return globalPath, ""
		},
		"absent project file": func(t *testing.T) (string, string) {
			return layerFixture(t, global, "")
		},
		"project file links to the global file": func(t *testing.T) (string, string) {
			globalPath, projectRoot := layerFixture(t, global, "")
			if err := os.Symlink(globalPath, projectFilePath(projectRoot)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return globalPath, projectRoot
		},
		"project folder is the apogee home": func(t *testing.T) (string, string) {
			globalPath, _ := layerFixture(t, global, "")
			projectRoot := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(projectRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Dir(globalPath), filepath.Join(projectRoot, projectConfigDirName)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return globalPath, projectRoot
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := arrange(t)

			layered, notices := loadLayered(t, globalPath, projectRoot)
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
		})
	}
}

// LoadFileConfig keeps reading the global file alone, whatever project file stands beside it.
func TestLoadFileConfigReadsTheGlobalFileAlone(t *testing.T) {
	t.Parallel()
	globalPath, _ := layerFixture(t, "workflow-retries: 2\n", "workflow-retries: 9\n")

	o, err := LoadFileConfig(globalPath, os.ReadFile, noNotify)

	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if o.WorkflowRetries != 2 || o.ProjectKeys != nil {
		t.Errorf("workflow-retries = %d, ProjectKeys = %v; want the global 2 and no project keys",
			o.WorkflowRetries, o.ProjectKeys)
	}
}

// A project file that does not parse, or states a value its key refuses, never stops a start: one
// notice names the project path and the session runs on the global file alone.
func TestLayeredConfigSkipsAProjectFileThatFailsItsChecks(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"does not parse":            "workflow-retries: [5\n",
		"refused workflow-wake":     "workflow-retries: 5\nworkflow-wake: true\n",
		"climbing context-file":     "workflow-retries: 5\ncontext-files:\n  names: [../x]\n",
		"list where a block goes":   "workflow-retries: 5\ntools: [terminal]\n",
		"top level is not settings": "- workflow-retries\n",
		"two documents":             "workflow-retries: 5\n---\nworkflow-retries: 6\n",
		"malformed dangerous rule": "workflow-retries: 5\ndangerous-rules:\n  add:\n" +
			"    - {id: r1, pattern: '(', tier: ask}\n",
		"dangerous rules not a block": "workflow-retries: 5\ndangerous-rules: [r1]\n",
	}
	for name, project := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			globalPath, projectRoot := layerFixture(t, "workflow-retries: 2\n", project)

			o, notices := loadLayered(t, globalPath, projectRoot)

			if o.WorkflowRetries != 2 || o.ProjectKeys != nil {
				t.Errorf("workflow-retries = %d, ProjectKeys = %v; want the global 2 and no layer",
					o.WorkflowRetries, o.ProjectKeys)
			}
			if len(notices) != 1 || !strings.Contains(notices[0], projectFilePath(projectRoot)) ||
				!strings.Contains(notices[0], "the project layer is skipped") {
				t.Errorf("notices = %q; want one naming %s and the skipped layer",
					notices, projectFilePath(projectRoot))
			}
		})
	}
}

// A key the schema does not spell is announced against the project path, not the global one.
func TestLayeredConfigAnnouncesUnknownProjectKeysAgainstTheProjectPath(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "", "workflow-retires: 5\n")

	_, notices := loadLayered(t, globalPath, projectRoot)

	if len(notices) != 1 || !strings.Contains(notices[0], projectFilePath(projectRoot)) ||
		!strings.Contains(notices[0], `"workflow-retires"`) {
		t.Errorf("notices = %q; want one unknown-key line naming the project path", notices)
	}
}

// resolveLayeredStartup runs ResolveOptions for a workspace whose Project root holds project, over
// a home holding global, with the given environment, and answers the options and the notices.
func resolveLayeredStartup(t *testing.T, global, project string, opts Options,
	env map[string]string) (Options, []string) {
	t.Helper()

	globalPath, projectRoot := layerFixture(t, global, project)
	opts.ConfigDir = filepath.Dir(globalPath)
	if opts.Workspace == "" {
		opts.Workspace = projectRoot
	}
	var notices []string
	_, err := ResolveOptions(&opts, func(string) bool { return false },
		func(name string) string { return env[name] }, os.ReadFile,
		func(n string) { notices = append(notices, n) }, testHostID)
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	return opts, notices
}

// Startup layers the Project config below the environment: a variable still beats both files, and
// the project's own keys land beside it.
func TestResolveOptionsLayersTheProjectBelowTheEnvironment(t *testing.T) {
	t.Parallel()

	o, _ := resolveLayeredStartup(t, "mode: plan\nworkflow-retries: 2\n", "workflow-retries: 6\n",
		Options{}, map[string]string{EnvMode: "auto"})

	if o.Mode != "auto" {
		t.Errorf("mode = %q; want APOGEE_MODE's auto", o.Mode)
	}
	if o.WorkflowRetries != 6 || !o.ProjectKeys["workflow-retries"] {
		t.Errorf("workflow-retries = %d, ProjectKeys = %v; want the project's 6, recorded",
			o.WorkflowRetries, o.ProjectKeys)
	}
}

// A flag still beats the files the project layer is merged into.
func TestResolveOptionsLetsAFlagBeatTheProjectLayer(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "mode: plan\n", "workflow-retries: 6\n")
	opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot, Mode: "auto"}

	_, err := ResolveOptions(&opts, func(name string) bool { return name == "mode" },
		func(string) string { return "" }, os.ReadFile, noNotify, testHostID)

	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if opts.Mode != "auto" || opts.WorkflowRetries != 6 {
		t.Errorf("mode = %q, workflow-retries = %d; want the flag's auto and the project's 6",
			opts.Mode, opts.WorkflowRetries)
	}
}

// A Driver that takes no project layer resolves the global file alone, silently.
func TestResolveOptionsTakesNoProjectLayerWhenGlobalConfigOnly(t *testing.T) {
	t.Parallel()

	o, notices := resolveLayeredStartup(t, "workflow-retries: 2\n", "workflow-retries: 6\nmode: auto\n",
		Options{GlobalConfigOnly: true}, nil)

	if o.WorkflowRetries != 2 || o.ProjectKeys != nil || len(notices) != 0 {
		t.Errorf("workflow-retries = %d, ProjectKeys = %v, notices = %q; want the global 2, no layer, "+
			"no notice", o.WorkflowRetries, o.ProjectKeys, notices)
	}
}

// A pinned Project root outranks the walk from the workspace: a running session's re-read takes the
// layer from the root it booted on, even when the workspace now holds a `.apogee/` of its own, and a
// pinned empty root — the home-folder workspace's answer — takes no layer at all.
func TestResolveOptionsReadsThePinnedProjectRoot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		pinBootRoot bool
		wantRetries int
		wantProject bool
	}{
		{name: "the boot root", pinBootRoot: true, wantRetries: 6, wantProject: true},
		{name: "an empty root", pinBootRoot: false, wantRetries: 2, wantProject: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			globalPath, bootRoot := layerFixture(t, "workflow-retries: 2\n", "workflow-retries: 6\n")
			// The workspace a fresh walk would answer with: it holds a Project config of its own.
			workspace := filepath.Join(t.TempDir(), "elsewhere")
			if err := os.MkdirAll(filepath.Join(workspace, projectConfigDirName), 0o700); err != nil {
				t.Fatalf("create workspace: %v", err)
			}
			writeLayerFile(t, projectFilePath(workspace), "workflow-retries: 9\n")
			opts := Options{ConfigDir: filepath.Dir(globalPath), Workspace: workspace, ProjectRootPinned: true}
			if tt.pinBootRoot {
				opts.ProjectRoot = bootRoot
			}

			if _, err := ResolveOptions(&opts, func(string) bool { return false },
				func(string) string { return "" }, os.ReadFile, noNotify, testHostID); err != nil {
				t.Fatalf("ResolveOptions: %v", err)
			}
			if opts.WorkflowRetries != tt.wantRetries || opts.ProjectKeys["workflow-retries"] != tt.wantProject {
				t.Errorf("workflow-retries = %d, ProjectKeys = %v; want %d from the pinned root (project: %v)",
					opts.WorkflowRetries, opts.ProjectKeys, tt.wantRetries, tt.wantProject)
			}
		})
	}
}

// A workspace that does not exist yet never stops a start: the layer is skipped with a notice.
func TestResolveOptionsSkipsTheLayerForAWorkspaceThatDoesNotExist(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "not-yet")

	o, notices := resolveLayeredStartup(t, "workflow-retries: 2\n", "workflow-retries: 6\n",
		Options{Workspace: missing}, nil)

	if o.WorkflowRetries != 2 {
		t.Errorf("workflow-retries = %d; want the global 2", o.WorkflowRetries)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], missing) ||
		!strings.Contains(notices[0], "the project layer is skipped") {
		t.Errorf("notices = %q; want one naming %s and the skipped layer", notices, missing)
	}
}

// An empty workspace is the current directory, as the composition root resolves it, so the
// Project config is read from the folder apogee was started in. Not parallel: t.Chdir moves the
// whole process.
func TestResolveOptionsReadsTheProjectOfTheCurrentDirectory(t *testing.T) {
	globalPath, projectRoot := layerFixture(t, "workflow-retries: 2\n", "workflow-retries: 6\n")
	t.Chdir(projectRoot)
	opts := Options{ConfigDir: filepath.Dir(globalPath)}

	_, err := ResolveOptions(&opts, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify, testHostID)

	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if opts.WorkflowRetries != 6 {
		t.Errorf("workflow-retries = %d; want the current directory's project 6", opts.WorkflowRetries)
	}
}

// overlayOnStartup resolves a global-only base over a home holding global — as the daemon resolves
// at its start — and overlays the Project config of a root holding project onto it, answering the
// base, the overlaid options and every notice the overlay produced.
func overlayOnStartup(t *testing.T, global, project string) (base, overlaid Options, notices []string) {
	t.Helper()

	globalPath, projectRoot := layerFixture(t, global, project)
	base = Options{ConfigDir: filepath.Dir(globalPath), Workspace: projectRoot, GlobalConfigOnly: true}
	if _, err := ResolveOptions(&base, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify, testHostID); err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	doc, err := ReadGlobalDocument(base.ConfigDir, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("ReadGlobalDocument: %v", err)
	}
	overlaid = OverlayProjectLayer(base, doc, projectRoot, os.ReadFile,
		func(n string) { notices = append(notices, n) })
	return base, overlaid, notices
}

// sansRoots is o without the fields that name where its files sit and which Driver resolved it, so
// two resolutions of the same files from two temp dirs compare equal.
func sansRoots(o Options) Options {
	o.ConfigDir, o.Workspace, o.GlobalConfigOnly = "", "", false
	return o
}

// An overlay resolves every key exactly as a startup resolution that took the same project layer.
func TestOverlayProjectLayerMatchesTheStartupLayer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		global  string
		project string
	}{
		{
			name:   "project-param and tighten-only keys",
			global: "workflow-retries: 2\ntools:\n  disabled: [web_fetch]\ncontext-files:\n  names: [A.md]\n",
			project: "workflow-retries: 6\ntools:\n  disabled: [terminal]\n" +
				"context-files:\n  names: [B.md, C.md]\n",
		},
		{
			name:    "the global names under the project's switch",
			global:  "context-files:\n  names: [A.md]\n",
			project: "context-files:\n  enable: true\n",
		},
		{
			name:    "the project's names under the global switch",
			global:  "context-files:\n  enable: false\n",
			project: "context-files:\n  names: [X.md]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, overlaid, notices := overlayOnStartup(t, tt.global, tt.project)
			startup, _ := resolveLayeredStartup(t, tt.global, tt.project, Options{}, nil)

			if len(notices) != 0 {
				t.Errorf("notices = %q; want none", notices)
			}
			if got, want := sansRoots(overlaid), sansRoots(startup); !reflect.DeepEqual(got, want) {
				t.Errorf("overlay = %+v\nwant the startup resolution %+v", got, want)
			}
		})
	}
}

// A global-only key in the project file is dropped with one notice; the keys the layer honours land.
func TestOverlayProjectLayerDropsAGlobalOnlyKey(t *testing.T) {
	t.Parallel()

	base, overlaid, notices := overlayOnStartup(t, "mode: plan\n", "mode: auto\nworkflow-retries: 6\n")

	if overlaid.Mode != base.Mode || overlaid.WorkflowRetries != 6 {
		t.Errorf("mode = %q, workflow-retries = %d; want the global %q and the project's 6",
			overlaid.Mode, overlaid.WorkflowRetries, base.Mode)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], `global-only key "mode"`) {
		t.Errorf("notices = %q; want one naming the global-only mode", notices)
	}
}

// A project file that cannot be layered costs one notice naming it and leaves the base as it was.
func TestOverlayProjectLayerSkipsABrokenProjectFile(t *testing.T) {
	t.Parallel()

	base, overlaid, notices := overlayOnStartup(t, "workflow-retries: 2\n", "workflow-retries: [6\n")

	if !reflect.DeepEqual(overlaid, base) {
		t.Errorf("overlay = %+v\nwant the base %+v", overlaid, base)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "the project layer is skipped") {
		t.Errorf("notices = %q; want one skipped-layer line", notices)
	}
}

// A key the environment or a flag won keeps the base's value over the project's.
func TestOverlayProjectLayerKeepsAnOverriddenKey(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t, "workflow-retries: 2\n", "workflow-retries: 6\n")
	doc, err := ReadGlobalDocument(filepath.Dir(globalPath), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("ReadGlobalDocument: %v", err)
	}
	base := Options{WorkflowRetries: 4, Overrides: map[string]Source{"workflow-retries": SourceEnv}}

	overlaid := OverlayProjectLayer(base, doc, projectRoot, os.ReadFile, noNotify)

	if overlaid.WorkflowRetries != 4 {
		t.Errorf("workflow-retries = %d; want the overriding 4", overlaid.WorkflowRetries)
	}
}

// The overlay never writes into its input, not even the spare capacity of a tighten-only list the
// union could have grown in place — concurrent overlays of one base share those arrays.
func TestOverlayProjectLayerLeavesTheBaseUnchanged(t *testing.T) {
	t.Parallel()
	globalPath, projectRoot := layerFixture(t,
		"workflow-retries: 2\ntools:\n  disabled: [web_fetch]\nurl-safety:\n  deny-hosts: [bad.example]\n",
		"tools:\n  disabled: [terminal]\n"+
			"url-safety:\n  deny-hosts: [evil.example]\nworkflow-retries: 6\n")
	doc, err := ReadGlobalDocument(filepath.Dir(globalPath), os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("ReadGlobalDocument: %v", err)
	}
	disabled := append(make([]string, 0, 4), "web_fetch")
	denied := append(make([]string, 0, 4), "bad.example")
	base := Options{WorkflowRetries: 2, ToolsDisabled: disabled, URLDenyHosts: denied}
	before := base
	before.ToolsDisabled = slices.Clone(disabled[:cap(disabled)])
	before.URLDenyHosts = slices.Clone(denied[:cap(denied)])

	overlaid := OverlayProjectLayer(base, doc, projectRoot, os.ReadFile, noNotify)

	if overlaid.WorkflowRetries != 6 || !slices.Contains(overlaid.ToolsDisabled, "terminal") {
		t.Fatalf("overlay = %+v; want the project layer applied", overlaid)
	}
	if base.WorkflowRetries != 2 || !slices.Equal(disabled[:cap(disabled)], before.ToolsDisabled) ||
		!slices.Equal(denied[:cap(denied)], before.URLDenyHosts) {
		t.Errorf("base after the overlay: retries %d, tools.disabled %q, deny-hosts %q; want 2, %q, %q",
			base.WorkflowRetries, disabled[:cap(disabled)], denied[:cap(denied)],
			before.ToolsDisabled, before.URLDenyHosts)
	}
}
