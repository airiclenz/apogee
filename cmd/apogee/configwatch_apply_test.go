package main

// The watcher driving the live apply (ADR 0041 decisions 3, 5, 7 and 8): a file saved by anything at
// all is re-read, diffed against the baseline, and pushed through the very dispatcher an in-pane
// commit uses. Every test below drives a REAL watcher over a real file, because what is being proved
// is that the chain is joined end to end — a fake report would prove only that changed() still works.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apogee "github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/tui"
)

// The cadence this file drives a real watcher at. The production poll is a second, which no test
// should wait out; internal/config's Watcher exposes Interval and Settle so a Driver-side test can
// tune them before Start, and configwatch_test.go's own copies of these numbers cover the watcher
// in isolation.
const (
	configWatchTestInterval = 10 * time.Millisecond
	configWatchTestSettle   = 150 * time.Millisecond
	configWatchTestDeadline = 3 * time.Second
)

// startConfigWatcher starts a watcher over path at the test cadence and stops it with the test.
func startConfigWatcher(t *testing.T, path string) *config.Watcher {
	t.Helper()
	w := config.NewWatcher(path)
	w.Interval = configWatchTestInterval
	w.Settle = configWatchTestSettle
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

// watchedConfig is the chain the composition root wires, composed over one temp file: the poll, the
// baseline the diff is made against, and the seam the renderer parks a wait on.
type watchedConfig struct {
	path  string
	edits *externalEdit
	await func(context.Context) bool
}

// newWatchedConfig seeds the file with body and starts watching it at the test cadence.
func newWatchedConfig(t *testing.T, body string) *watchedConfig {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, body)
	return &watchedConfig{
		path:  path,
		edits: newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" }),
		await: awaitConfigChangeOn(startConfigWatcher(t, path)),
	}
}

// reload is exactly what the renderer does with a report: wait for one, then re-read. The wait is
// bounded by the same deadline the watcher's own tests use, so a chain that never reports fails here
// rather than hanging the suite.
func (c *watchedConfig) reload(t *testing.T, why string) ([]tui.AppliedSetting, error) {
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
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: t.TempDir(),
		ConfigDir: home,
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.AwaitConfigChange == nil {
		t.Fatal("the composition root wired no config watch; an external save would apply to nothing")
	}
	if rec.opts.AwaitConfigChange(context.Background()) {
		t.Error("the wait reported a change after teardown; the watch is stopped with the TUI")
	}

	// The pane's own write applies the key itself, so the baseline must move with it.
	if err := rec.opts.Settings.Write("auto-title", "false"); err != nil {
		t.Fatalf("Settings.Write: %v", err)
	}
	applied, err := rec.opts.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("a re-read after apogee's own write reported %+v; the watcher would apply it a second "+
			"time", applied)
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
	apply := applySettingFor(settingsApplier{
		engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: c.path})

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
