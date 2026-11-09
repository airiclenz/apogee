package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
)

// The configured `currency:` label reaches the renderer through the root's options, so every priced
// amount the TUI prints names the currency the human chose (ADR 0093 decision 1) rather than one the
// renderer made up.
func TestRunRootWiresTheConfiguredCurrency(t *testing.T) {
	t.Parallel()

	opts := config.Options{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		StartupEntry: config.ServerEntry{Endpoint: "http://127.0.0.1:1111", Model: "fake"},
		Mode:         "ask-before",
		Workspace:    t.TempDir(),
		ConfigDir:    t.TempDir(),
		Currency:     "EUR",
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	w := newRootWiring(opts, apogee.ModeAskBefore, roots)
	t.Cleanup(w.close)
	if err := w.resolveConfig(); err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}

	if got, want := w.options().Currency, opts.Currency; got != want {
		t.Errorf("options().Currency = %q, want the configured %q", got, want)
	}
}

// A pin, unpin or hide persisted through the host lands in the file, answers the list the file now
// resolves to, and re-takes the external-edit baseline (ADR 0041 decision 8): the watcher reports
// nothing for apogee's own write, so the session does not apply it a second time.
func TestSettingsHostSidePanelSectionsWriteRefreshesTheBaseline(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "ui:\n  side-panel:\n    show: auto\n")
	h := settingsHost{configPath: c.path, edits: c.edits}

	sections, err := h.WriteSidePanelSection(domain.SidePanelTasks, domain.SidePanelModePinned)

	if err != nil {
		t.Fatalf("WriteSidePanelSection: %v", err)
	}
	const want = "[sub-agents: auto, tasks: pinned, changed-files: auto, usage: auto, mcp: auto]"
	if sections != want {
		t.Errorf("sections = %q, want %q", sections, want)
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("read the config back: %v", err)
	}
	if !strings.Contains(string(data), "sections: "+want) {
		t.Errorf("the config does not carry the list:\n%s", data)
	}
	applied, err := c.reload(t, "apogee's own sections write")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("the watcher reported %+v for apogee's own write; it would apply twice", applied)
	}
}

// The `/settings` Reset of `ui.side-panel.show` goes to the block's writer too — the scalar reset
// refuses a path three segments deep, so the pane announced a key it then could not reset — and
// re-takes the baseline as every landed write does. The file reads back at the default.
func TestSettingsHostResetsSidePanelShow(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "ui:\n  spinner: glitter\n  side-panel:\n    show: \"off\"\n    sections: [usage: hidden]\n")
	h := settingsHost{configPath: c.path, edits: c.edits}

	if err := h.Reset(domain.UIKeySidePanelShow); err != nil {
		t.Fatalf("Reset show: %v", err)
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("read the config back: %v", err)
	}
	if want := "ui:\n  spinner: glitter\n  side-panel:\n    sections: [usage: hidden]\n"; string(data) != want {
		t.Errorf("config =\n%s\nwant\n%s", data, want)
	}
	opts, err := config.LoadFileConfig(c.path, os.ReadFile, func(string) {})
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if opts.UI.SidePanelShow != domain.DefaultSidePanelShow {
		t.Errorf("show reads back %q, want the default %q", opts.UI.SidePanelShow, domain.DefaultSidePanelShow)
	}
	applied, err := c.reload(t, "apogee's own side-panel reset")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("the watcher reported %+v for apogee's own reset; it would apply twice", applied)
	}
}

// The `/settings` write of either `ui.side-panel` key goes to the block's writer — the scalar
// writer refuses a path three segments deep — and re-takes the baseline as every landed write does.
func TestSettingsHostWritesBothSidePanelSectionsBlockKeys(t *testing.T) {
	t.Parallel()
	c := newWatchedConfig(t, "ui:\n  side-panel:\n    show: auto\n")
	h := settingsHost{configPath: c.path, edits: c.edits}

	if err := h.Write(domain.UIKeySidePanelShow, "off"); err != nil {
		t.Fatalf("Write show: %v", err)
	}
	if err := h.Write(domain.UIKeySidePanelSections, "[usage: hidden]"); err != nil {
		t.Fatalf("Write sections: %v", err)
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("read the config back: %v", err)
	}
	want := "ui:\n  side-panel:\n    show: \"off\"\n" +
		"    sections: [usage: hidden, sub-agents: auto, tasks: auto, changed-files: auto, mcp: auto]\n"
	if string(data) != want {
		t.Errorf("config =\n%s\nwant\n%s", data, want)
	}
	applied, err := c.reload(t, "apogee's own side-panel writes")
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("the watcher reported %+v for apogee's own writes; it would apply twice", applied)
	}
}
