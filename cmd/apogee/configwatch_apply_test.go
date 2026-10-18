package main

// The watcher driving the live apply (ADR 0041 decisions 3, 5, 7 and 8): a file saved by anything at
// all is re-read, diffed against the baseline, and pushed through the very dispatcher an in-pane
// commit uses. Every test below drives a REAL watcher over a real file, because what is being proved
// is that the chain is joined end to end — a fake report would prove only that changed() still works.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	apogee "github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/filewatch"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/projectroot"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tui"
)

// The cadence this file drives a real watcher at. The production poll is a second, which no test
// should wait out; internal/config's Watcher exposes Interval and Settle so a Driver-side test can
// tune them before Start, and configwatch_test.go's own copies of these numbers cover the watcher
// in isolation. The interval and the settle are the watcher's own pacing, so they stay fixed; the
// deadline is a ceiling on a report arriving, so it scales with the box.
const (
	configWatchTestInterval = 10 * time.Millisecond
	configWatchTestSettle   = 150 * time.Millisecond
)

var configWatchTestDeadline = testtime.Budget(3 * time.Second)

// startConfigWatcher starts a watcher over path at the test cadence and stops it with the test.
func startConfigWatcher(t *testing.T, path string) *filewatch.Watcher {
	t.Helper()
	w := filewatch.New(path)
	w.Interval = configWatchTestInterval
	w.Settle = configWatchTestSettle
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

// watchedConfig is the chain the composition root wires, composed over temp files: the polls, the
// baseline the diff is made against, and the seam the renderer parks a wait on. projectPath is the
// Project config the second watcher polls, or "" for the global file alone; projectRoot is the
// Project root it belongs to, whose adoption record the third watcher polls.
type watchedConfig struct {
	path        string
	projectPath string
	projectRoot string
	edits       *externalEdit
	await       func(context.Context) bool
}

// newWatchedConfig seeds the file with body and starts watching it at the test cadence.
func newWatchedConfig(t *testing.T, body string) *watchedConfig {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, body)
	return &watchedConfig{
		path:  path,
		edits: newExternalEdit(config.Options{ConfigDir: home}, "", func(string) string { return "" }),
		await: awaitConfigChangeOn(startConfigWatcher(t, path), nil, nil, nil),
	}
}

// newWatchedLayeredConfig is newWatchedConfig with a Project config beside the global file, and
// the Project root's adoption record watched beside both (the composition root's three watchers): a
// workspace outside any repository, which is its own Project root (internal/projectroot), whose
// `.apogee/config.yaml` is seeded with project — or left absent when project is empty — and watched
// by the second watcher at the path the composition root derives (projectConfigWatchPath).
func newWatchedLayeredConfig(t *testing.T, global, project string) *watchedConfig {
	t.Helper()
	home, workspace := t.TempDir(), t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, global)
	opts := config.Options{ConfigDir: home, Workspace: workspace}
	projectRoot := projectroot.Resolve(workspace, "")
	projectPath := projectConfigWatchPath(opts, projectRoot)
	if projectPath == "" {
		t.Fatal("no Project config path for a workspace that is its own Project root")
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatalf("create the project's .apogee: %v", err)
	}
	if project != "" {
		writeSettingsFixture(t, projectPath, project)
	}
	adoptionPath := adoptionWatchPath(opts, stateRoots{config: home, workspace: workspace, project: projectRoot})
	if adoptionPath == "" {
		t.Fatal("no adoption record path for the fixture's Project root")
	}
	return &watchedConfig{
		path:        path,
		projectPath: projectPath,
		projectRoot: projectRoot,
		edits:       newExternalEdit(opts, workspace, func(string) string { return "" }),
		await: awaitConfigChangeOn(startConfigWatcher(t, path), startConfigWatcher(t, projectPath),
			startConfigWatcher(t, adoptionPath), nil),
	}
}

// reload is exactly what the renderer does with a report: wait for one, then re-read. The wait is
// bounded by the same deadline the watcher's own tests use, so a chain that never reports fails here
// rather than hanging the suite.
func (c *watchedConfig) reload(t *testing.T, why string) ([]tui.AppliedSetting, error) {
	t.Helper()
	reload, err := c.reloadWhole(t, why)
	return reload.Applied, err
}

// reloadWhole is reload answering the whole re-read: the keys it applies and the notices it adds.
func (c *watchedConfig) reloadWhole(t *testing.T, why string) (tui.ConfigReload, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), configWatchTestDeadline)
	defer cancel()
	if !c.await(ctx) {
		t.Fatalf("no change reported for %s", why)
	}
	return c.edits.changed()
}

// The headline of ADR 0041 decision 5: a key changed in the file by something that is not apogee
// reaches the session, with nothing pressed and no editor involved. The pane is not open — nothing
// in this test is a pane at all.
func TestWatchedConfigReportsAKeyChangedOutsideTheProgram(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "auto-title: true\n")

	writeSettingsFixture(t, c.path, "auto-title: false\n")
	applied, err := c.reload(t, "a key saved from another window")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "auto-title" || applied[0].Value != "false" {
		t.Fatalf("reload = %+v, want auto-title false — the file's own edit, applied live", applied)
	}
}

// The last-good rule (decision 7): a save the parse refuses applies NOTHING and keeps the baseline it
// had, so the fix that follows is diffed against the config that was last good — and lands whole.
func TestWatchedConfigKeepsTheLastGoodConfigThroughAnUnparseableSave(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "auto-title: true\n")

	writeSettingsFixture(t, c.path, "auto-title: [unterminated\n")
	applied, err := c.reload(t, "a file saved mid-edit")
	if err == nil {
		t.Fatalf("reload = %+v, want the parse's own refusal", applied)
	}
	if len(applied) != 0 {
		t.Fatalf("a refused reload reported %+v; nothing may be applied from a file nobody can read", applied)
	}

	writeSettingsFixture(t, c.path, "auto-title: false\n")
	applied, err = c.reload(t, "the fixed file")
	if err != nil {
		t.Fatalf("changed after the fix: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "auto-title" || applied[0].Value != "false" {
		t.Fatalf("reload = %+v, want auto-title false: the baseline the broken save left standing is "+
			"what the fix is diffed against", applied)
	}
}

// Decision 8, the self-write half: the pane persists a key and applies it in the same keypress, so
// the watcher must see NOTHING for the write apogee itself just made. The refresh the composition
// root does after every landed write is what makes that true — without it the same key applies twice,
// which for `mcp-servers:` is a second dial of every server.
func TestWatchedConfigReportsNothingForApogeesOwnWrite(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "auto-title: true\n")

	if err := config.SaveConfigSetting(c.path, "auto-title", "false"); err != nil {
		t.Fatalf("SaveConfigSetting: %v", err)
	}
	c.edits.refresh() // what tui.SettingsHost.Write does the moment the write lands

	applied, err := c.reload(t, "apogee's own write")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("the watcher reported %+v for a key the pane had already applied; it would apply twice",
			applied)
	}
}

// The composition root's own half of the two rules above: the wait seam is wired for the life of the
// TUI and stopped with it, and the write seam refreshes the baseline the watcher is diffed against.
// runRoot has returned by the time the assertions run — which is exactly why the wait answers false:
// the deferred Stop closed the watch, and a renderer parked on it is released rather than leaked.
func TestRunRootWiresTheConfigWatch(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	home := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	writeSettingsFixture(t, filepath.Join(home, "config.yaml"),
		"editor: "+self+"\nmode: ask-before\nauto-title: true\n")
	opts := config.Options{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		StartupEntry: config.ServerEntry{Endpoint: "http://127.0.0.1:1111", Model: "fake"},
		Mode:         "ask-before",
		Workspace:    t.TempDir(),
		ConfigDir:    home,
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.Config == nil {
		t.Fatal("the composition root wired no config host; an external save would apply to nothing")
	}
	if rec.opts.Config.AwaitConfigChange(context.Background()) {
		t.Error("the wait reported a change after teardown; the watch is stopped with the TUI")
	}

	// The pane's own write applies the key itself, so the baseline must move with it.
	if err := rec.opts.Settings.Write("auto-title", "false"); err != nil {
		t.Fatalf("Settings.Write: %v", err)
	}
	reload, err := rec.opts.Config.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if len(reload.Applied) != 0 {
		t.Errorf("a re-read after apogee's own write reported %+v; the watcher would apply it a second "+
			"time", reload.Applied)
	}
}

// The b5e43fd regression, now through the watcher: repointing the one `mcp-servers:` entry at another
// machine leaves the row reading "1 server" character for character, and the whole point of the
// structured diff is that the reconnect happens anyway — this time with no editor and no keypress
// behind it, only a saved file.
func TestWatchedConfigRepointedMCPServerReconnects(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, mcpServersFixture)

	old := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__search"}}}
	next := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__search"}}}
	var dialled [][]mcp.ServerConfig
	fixture := newMCPFixture(old, "https://search.example.com/s", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		return next, nil
	})
	spy := &applySettingSpy{}
	applier := fakeApplier(t)
	applier.engine = spy
	applier.tools = fixture.tools
	applier.mcp = fixture.set
	applier.configPath = c.path
	apply := applySettingFor(applier)

	writeSettingsFixture(t, c.path, strings.Replace(mcpServersFixture,
		"https://mcp.example.com/", "https://192.0.2.1/mcp", 1))
	applied, err := c.reload(t, "the repointed mcp-servers block")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "mcp-servers" {
		t.Fatalf("reload = %+v, want the repointed mcp-servers block", applied)
	}
	// What the renderer does with what came back: the same dispatcher, the same value spelling.
	if _, err := apply(applied[0].Path, applied[0].Value); err != nil {
		t.Fatalf("apply %s: %v", applied[0].Path, err)
	}
	if len(dialled) != 1 || len(dialled[0]) != 1 {
		t.Fatalf("dialled = %+v, want exactly one dial of one server", dialled)
	}
	if got := dialled[0][0].Endpoint; got != "https://192.0.2.1/mcp" {
		t.Errorf("dialled %q, want the endpoint the file now names", got)
	}
	if !old.closed {
		t.Error("the old sessions are still open; a reconnect that landed leaves no orphan behind it")
	}
}

// ----------------------------------------------------------------------------
// The Project config, watched beside the global file (ADR 0096 §6)
// ----------------------------------------------------------------------------

// A project-param key saved into the Project config reaches the session through the same diff and
// the same dispatcher a global edit does — here a project file created mid-session, which the second
// watcher reports as the change it is.
func TestWatchedProjectConfigAppliesAProjectParamKey(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, "context-files:\n  names: [AGENTS.md]\n", "")

	writeSettingsFixture(t, c.projectPath, "context-files:\n  names: [CLAUDE.md]\n")
	applied, err := c.reload(t, "a project-param key saved into the Project config")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "context-files.names" {
		t.Fatalf("reload = %+v, want context-files.names — the project file's value outranks the global one",
			applied)
	}
	spy := &applySettingSpy{}
	applier := fakeApplier(t)
	applier.engine = spy
	if _, err := applySettingFor(applier)(applied[0].Path, applied[0].Value); err != nil {
		t.Fatalf("apply %s: %v", applied[0].Path, err)
	}
	if len(spy.contextFiles) != 1 || strings.Join(spy.contextFiles[0].names, ",") != "CLAUDE.md" {
		t.Errorf("engine got %+v, want the project's [CLAUDE.md] through the dispatcher", spy.contextFiles)
	}
}

// A global-only key saved into the Project config applies nothing — the layer drops it — and the
// re-read says so once, in the loader's own words.
func TestWatchedProjectConfigIgnoresAGlobalOnlyKeyWithANotice(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, "mode: ask-before\n", "context-files:\n  enable: true\n")

	writeSettingsFixture(t, c.projectPath, "context-files:\n  enable: true\nmode: auto\n")
	reload, err := c.reloadWhole(t, "a global-only key saved into the Project config")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(reload.Applied) != 0 {
		t.Errorf("reload applied %+v; a global-only key in the Project config moves nothing", reload.Applied)
	}
	if len(reload.Notices) != 1 || !strings.Contains(reload.Notices[0], "ignoring global-only key") ||
		!strings.Contains(reload.Notices[0], `"mode"`) {
		t.Errorf("notices = %q, want the one line naming the ignored mode key", reload.Notices)
	}
}

// A Project config removed mid-session changes nothing live: a deletion is no report
// (internal/filewatch skips a failed stat), so the layer it carried stands until the next launch.
func TestWatchedProjectConfigRemovalChangesNothingLive(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, "", "context-files:\n  names: [CLAUDE.md]\n")

	if err := os.Remove(c.projectPath); err != nil {
		t.Fatalf("remove the Project config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testtime.Fixed(4*configWatchTestSettle))
	defer cancel()
	if c.await(ctx) {
		t.Error("removing the Project config was reported as a change; it applies at the next launch")
	}
}

// The second watcher's path is the Project config the layered load reads, and nothing when there is
// no second file: a resolution on the global config alone, the empty Project root, or a project
// file that is the global file itself.
func TestProjectConfigWatchPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projectFile := filepath.Join(root, ".apogee", "config.yaml")
	for _, tc := range []struct {
		name string
		opts config.Options
		root string
		want string
	}{
		{"a Project root", config.Options{ConfigDir: t.TempDir()}, root, projectFile},
		{"the global config alone", config.Options{ConfigDir: t.TempDir(), GlobalConfigOnly: true}, root, ""},
		{"no Project root", config.Options{ConfigDir: t.TempDir()}, "", ""},
		{"the global file itself", config.Options{ConfigDir: filepath.Join(root, ".apogee")}, root, ""},
	} {
		if got := projectConfigWatchPath(tc.opts, tc.root); got != tc.want {
			t.Errorf("%s: projectConfigWatchPath = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The /settings applies of the global-only keys re-read the global file alone
// (settingsApplier.fileConfig), so a Project config that does not even parse cannot fail one of them.
// (The `mcp-servers:` and `reactions:` applies read the layered set, where the loader skips such a
// layer and the re-read degrades to the global entries — settingsApplier.layeredConfig.)
func TestWatchedMalformedProjectConfigLeavesAServersApplyWorking(t *testing.T) {
	t.Parallel()
	applier := fakeApplier(t)
	projectFile := config.ProjectFilePath(applier.roots.project)
	if projectFile == "" {
		t.Fatal("the fixture's workspace resolved no Project root")
	}
	if err := os.MkdirAll(filepath.Dir(projectFile), 0o700); err != nil {
		t.Fatalf("create the project's .apogee: %v", err)
	}
	writeSettingsFixture(t, projectFile, "servers: [\n")
	writeSettingsFixture(t, applier.configPath,
		"servers:\n  - name: local\n    endpoint: http://127.0.0.1:1111\n")

	if _, err := applySettingFor(applier)("servers", "1 server"); err != nil {
		t.Fatalf("apply servers beside a malformed Project config: %v", err)
	}
	if got := applier.live.serverList(); len(got) != 1 || got[0].Name != "local" {
		t.Errorf("servers after the apply = %+v, want the global file's one local entry", got)
	}
}

// ----------------------------------------------------------------------------
// Adopted repo servers and reactions, live (ADR 0104 §12)
// ----------------------------------------------------------------------------

// projectWikiServerYAML is a Project config proposing one HTTP server whose bearer token is read
// out of an environment variable.
const projectWikiServerYAML = "mcp-servers:\n" +
	"  - name: wiki\n" +
	"    transport: streamable-http\n" +
	"    endpoint: https://wiki.example.com/mcp\n" +
	"    headers-env:\n" +
	"      Authorization: WIKI_MCP_TOKEN\n"

// projectNotifyReactionYAML is a Project config proposing one observe-lane reaction.
const projectNotifyReactionYAML = "reactions:\n  - id: notify\n    on: [file-changed]\n    run: [notify-send, saved]\n"

// adoptEveryProposal records every entry the layered config of globalPath and projectRoot proposes
// as adopted — exactly what an `apogee project adopt` in another terminal writes — and fails the
// test when there is none to adopt.
func adoptEveryProposal(t *testing.T, globalPath, projectRoot string) {
	t.Helper()
	o, err := loadLayeredOptions(globalPath, projectRoot)
	if err != nil {
		t.Fatalf("load the layered config: %v", err)
	}
	if len(o.Proposals.Proposed) == 0 {
		t.Fatal("the Project config proposes nothing to adopt")
	}
	store, err := adoption.New(config.WorkspacesDir(filepath.Dir(globalPath)), projectRoot)
	if err != nil {
		t.Fatalf("open the adoption record: %v", err)
	}
	for _, p := range o.Proposals.Proposed {
		if err := store.Adopt(adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}); err != nil {
			t.Fatalf("Adopt %s: %v", p.Handle, err)
		}
	}
}

// layeredApplier is fakeApplier over c's two files: the global config it re-reads and the Project
// root whose Project config and adoption record it layers over it, with a live scrub holder.
func layeredApplier(t *testing.T, c *watchedConfig) settingsApplier {
	t.Helper()
	applier := fakeApplier(t)
	applier.configPath = c.path
	applier.roots.project = c.projectRoot
	applier.secretEnv = apogee.NewSecretEnv(nil)
	return applier
}

// appliedPath is the applied setting at path in applied, failing the test when the re-read did not
// report it.
func appliedPath(t *testing.T, applied []tui.AppliedSetting, path string) tui.AppliedSetting {
	t.Helper()
	for _, a := range applied {
		if a.Path == path {
			return a
		}
	}
	t.Fatalf("reload = %+v, want %s among the applied keys", applied, path)
	return tui.AppliedSetting{}
}

// A Project config's server adopted mid-session — the answer recorded by another terminal, so
// neither config file moves — is reported by the adoption-record watcher, and the reconnect it
// drives dials it beside the global servers, marked as the project's own.
func TestWatchedProjectConfigAdoptedServerConnects(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, mcpServersFixture, projectWikiServerYAML)
	var dialled [][]mcp.ServerConfig
	fixture := newMCPFixture(&fakeMCPSession{}, "", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		return &fakeMCPSession{}, nil
	})
	applier := layeredApplier(t, c)
	applier.engine = &applySettingSpy{}
	applier.tools = fixture.tools
	applier.mcp = fixture.set

	adoptEveryProposal(t, c.path, c.projectRoot)
	applied, err := c.reload(t, "the repo server adopted outside this session")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	row := appliedPath(t, applied, "mcp-servers")
	if _, err := applySettingFor(applier)(row.Path, row.Value); err != nil {
		t.Fatalf("apply %s: %v", row.Path, err)
	}

	if len(dialled) != 1 {
		t.Fatalf("dialled %d times, want one reconnect", len(dialled))
	}
	got := dialled[0]
	if len(got) != 2 || got[0].Name != "docs" || got[0].Project || got[1].Name != "wiki" || !got[1].Project {
		t.Errorf("dialled %+v, want the global docs server and the adopted repo wiki server", got)
	}
}

// A repo reaction adopted mid-session reaches the running lanes: the record watcher reports it, and
// the reload the `reactions:` row drives hangs the layered list — the adopted entry, marked as the
// project's — on the generation it swaps in.
func TestWatchedAdoptionRecordReloadsARepoReaction(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, "", projectNotifyReactionYAML)
	spy := &applySettingSpy{}
	applier := layeredApplier(t, c)
	applier.engine = spy

	adoptEveryProposal(t, c.path, c.projectRoot)
	applied, err := c.reload(t, "the repo reaction adopted outside this session")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	row := appliedPath(t, applied, "reactions")
	if _, err := applySettingFor(applier)(row.Path, row.Value); err != nil {
		t.Fatalf("apply %s: %v", row.Path, err)
	}

	if len(spy.generations) != 1 {
		t.Fatalf("SetReactions called %d times, want one swap", len(spy.generations))
	}
	observe := spy.generations[0].Observe
	if len(observe) != 1 || observe[0].ID != "notify" || !observe[0].Project {
		t.Errorf("observe lane = %+v, want the adopted repo reaction notify", observe)
	}
}

// brokenProjectConfigYAML is a Project config saved half-way through an edit: it does not parse,
// so the loader skips the project layer with a notice rather than refusing the re-read.
const brokenProjectConfigYAML = "mcp-servers: [\n"

// reloadAndApply waits for the watcher's report of why, re-reads, and applies the row at path
// through the dispatcher an in-pane commit uses — what the renderer does with a report — answering
// the whole re-read so a caller can read its notices.
func reloadAndApply(t *testing.T, c *watchedConfig, applier settingsApplier, path, why string) tui.ConfigReload {
	t.Helper()
	reload, err := c.reloadWhole(t, why)
	if err != nil {
		t.Fatalf("changed (%s): %v", why, err)
	}
	row := appliedPath(t, reload.Applied, path)
	if _, err := applySettingFor(applier)(row.Path, row.Value); err != nil {
		t.Fatalf("apply %s (%s): %v", row.Path, why, err)
	}
	return reload
}

// assertSkippedLayerNotice fails the test unless the re-read told the human the project layer was
// skipped — the one line that explains why the repo entries just left the session.
func assertSkippedLayerNotice(t *testing.T, reload tui.ConfigReload) {
	t.Helper()
	for _, n := range reload.Notices {
		if strings.Contains(n, "the project layer is skipped") {
			return
		}
	}
	t.Errorf("notices = %q, want one naming the skipped project layer", reload.Notices)
}

// assertStillAdopted fails the test unless every entry the Project config proposes is adopted and
// none awaits an answer: the entries that came back did so on the record already kept, unasked.
func assertStillAdopted(t *testing.T, c *watchedConfig, want int) {
	t.Helper()
	o, err := loadLayeredOptions(c.path, c.projectRoot)
	if err != nil {
		t.Fatalf("load the layered config: %v", err)
	}
	if p := o.Proposals; len(p.Adopted) != want || len(p.Proposed) != 0 || len(p.Rejected) != 0 {
		t.Errorf("proposals = %+v, want %d adopted and nothing awaiting an answer", p, want)
	}
}

// A Project config that stops parsing mid-session degrades the layered re-read to the global set
// (settingsApplier.layeredConfig): the adopted repo server is torn down by a reconnect onto the global
// servers alone, with the skipped-layer notice saying why. The first save that parses again dials it
// back on the adoption already recorded — nothing re-asks — and the scrub it widened stays wide.
func TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, mcpServersFixture, projectWikiServerYAML)
	var dialled [][]mcp.ServerConfig
	var sessions []*fakeMCPSession
	fixture := newMCPFixture(&fakeMCPSession{}, "", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		s := &fakeMCPSession{}
		sessions = append(sessions, s)
		return s, nil
	})
	applier := layeredApplier(t, c)
	applier.engine = &applySettingSpy{}
	applier.tools = fixture.tools
	applier.mcp = fixture.set

	adoptEveryProposal(t, c.path, c.projectRoot)
	reloadAndApply(t, c, applier, "mcp-servers", "the repo server adopted outside this session")

	writeSettingsFixture(t, c.projectPath, brokenProjectConfigYAML)
	assertSkippedLayerNotice(t, reloadAndApply(t, c, applier, "mcp-servers", "the Project config that stopped parsing"))

	writeSettingsFixture(t, c.projectPath, projectWikiServerYAML)
	reloadAndApply(t, c, applier, "mcp-servers", "the Project config that parses again")

	if len(dialled) != 3 {
		t.Fatalf("dialled %d times, want three reconnects (adopted, broken, repaired)", len(dialled))
	}
	if got := dialled[1]; len(got) != 1 || got[0].Name != "docs" || got[0].Project {
		t.Errorf("while broken dialled %+v, want the global docs server alone", got)
	}
	if !sessions[0].closed {
		t.Error("the session holding the adopted repo server is still open after the teardown")
	}
	if got := dialled[2]; len(got) != 2 || got[0].Name != "docs" || got[0].Project ||
		got[1].Name != "wiki" || !got[1].Project {
		t.Errorf("once repaired dialled %+v, want the global docs server and the adopted repo wiki server", got)
	}
	assertStillAdopted(t, c, 1)
	if !slices.Contains(applier.secretEnv.Names(), "WIKI_MCP_TOKEN") {
		t.Errorf("scrub = %q, want WIKI_MCP_TOKEN kept through the teardown", applier.secretEnv.Names())
	}
}

// The reaction half: a Project config that stops parsing takes the adopted repo reaction off the
// lanes on the swap the `reactions:` row drives, and the first save that parses again hangs it back
// on — the adopted entry, marked as the project's — without asking again.
func TestWatchedBrokenProjectConfigDropsRepoReactionUntilItParses(t *testing.T) {
	t.Parallel()
	c := newWatchedLayeredConfig(t, "", projectNotifyReactionYAML)
	spy := &applySettingSpy{}
	applier := layeredApplier(t, c)
	applier.engine = spy

	adoptEveryProposal(t, c.path, c.projectRoot)
	reloadAndApply(t, c, applier, "reactions", "the repo reaction adopted outside this session")

	writeSettingsFixture(t, c.projectPath, brokenProjectConfigYAML)
	assertSkippedLayerNotice(t, reloadAndApply(t, c, applier, "reactions", "the Project config that stopped parsing"))

	writeSettingsFixture(t, c.projectPath, projectNotifyReactionYAML)
	reloadAndApply(t, c, applier, "reactions", "the Project config that parses again")

	if len(spy.generations) != 3 {
		t.Fatalf("SetReactions called %d times, want three swaps (adopted, broken, repaired)", len(spy.generations))
	}
	if observe := spy.generations[1].Observe; len(observe) != 0 {
		t.Errorf("while broken the observe lane = %+v, want it empty", observe)
	}
	observe := spy.generations[2].Observe
	if len(observe) != 1 || observe[0].ID != "notify" || !observe[0].Project {
		t.Errorf("once repaired the observe lane = %+v, want the adopted repo reaction notify", observe)
	}
	assertStillAdopted(t, c, 1)
}
