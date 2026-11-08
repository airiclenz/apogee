package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeSectionsConfig writes body as a config file in a fresh directory and returns its path.
func writeSectionsConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	return path
}

// readSectionsConfig reads the config file back as a string.
func readSectionsConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config back: %v", err)
	}
	return string(data)
}

// A pin, unpin or hide changes one entry's mode and nothing else: the comments, the other keys of
// both blocks, the entries' order and the line's own note are carried over byte for byte.
func TestSidePanelSectionsWriteKeepsCommentsOrderAndOtherKeys(t *testing.T) {
	t.Parallel()
	path := writeSectionsConfig(t, "# my config\n"+
		"model: qwen\n"+
		"ui:\n"+
		"  spinner: glitter    # the sparkly one\n"+
		"  side-panel:\n"+
		"    show: on\n"+
		"    # the order I like\n"+
		"    sections: [tasks: pinned, usage: auto, sub-agents: hidden]  # mine\n"+
		"  notify: bell\n")

	resolved, err := SaveSidePanelSection(path, domain.SidePanelUsage, domain.SidePanelModeHidden)

	if err != nil {
		t.Fatalf("SaveSidePanelSection: %v", err)
	}
	want := "# my config\n" +
		"model: qwen\n" +
		"ui:\n" +
		"  spinner: glitter    # the sparkly one\n" +
		"  side-panel:\n" +
		"    show: on\n" +
		"    # the order I like\n" +
		"    sections: [tasks: pinned, usage: hidden, sub-agents: hidden]  # mine\n" +
		"  notify: bell\n"
	if got := readSectionsConfig(t, path); got != want {
		t.Errorf("the write changed more than the one mode\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	if want := domain.SidePanelSections("[tasks: pinned, usage: hidden, sub-agents: hidden, changed-files: auto]"); resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// A section the file's list does not name brings in the whole list as the session resolves it, so
// the sections keep the order they show in rather than the new one jumping ahead of the defaults.
func TestSidePanelSectionsWriteOfAnUnnamedSectionKeepsTheDisplayOrder(t *testing.T) {
	t.Parallel()
	path := writeSectionsConfig(t, "ui:\n  side-panel:\n    sections: [tasks: pinned]\n")

	resolved, err := SaveSidePanelSection(path, domain.SidePanelUsage, domain.SidePanelModeHidden)

	if err != nil {
		t.Fatalf("SaveSidePanelSection: %v", err)
	}
	const list = "[tasks: pinned, sub-agents: auto, changed-files: auto, usage: hidden]"
	if got, want := readSectionsConfig(t, path), "ui:\n  side-panel:\n    sections: "+list+"\n"; got != want {
		t.Errorf("config =\n%s\nwant\n%s", got, want)
	}
	if resolved != list {
		t.Errorf("resolved = %q, want %q", resolved, list)
	}
}

// A config with no list — here the seeded template itself, written for an absent file — gets the
// default list with the one mode changed, inserted as one line of the `side-panel:` block it joins.
func TestSidePanelSectionsWriteCreatesTheMissingListFromTheDefault(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	template, err := ReadConfigForWrite(path)
	if err != nil {
		t.Fatalf("seed the template: %v", err)
	}

	if _, err := SaveSidePanelSection(path, domain.SidePanelTasks, domain.SidePanelModePinned); err != nil {
		t.Fatalf("SaveSidePanelSection: %v", err)
	}

	at, added := splicedInsertion(t, string(template), readSectionsConfig(t, path))
	want := "    sections: [sub-agents: auto, tasks: pinned, changed-files: auto, usage: auto]"
	if len(added) != 1 || added[0] != want {
		t.Fatalf("inserted %q at line %d, want the one line %q", added, at, want)
	}
	if above := SplitConfigLines(template)[at-2]; !strings.HasPrefix(strings.TrimSpace(above), "show:") {
		t.Errorf("the list landed under %q, want it in the side-panel block after show:", above)
	}
}

// A file with no `ui.side-panel:` block gets the blocks it lacks created around the setting, joined
// to whatever of them is already there at the indentation that block uses.
func TestSidePanelSectionsWriteIntoAConfigWithNoSidePanelBlock(t *testing.T) {
	t.Parallel()
	const list = "[sub-agents: auto, tasks: auto, changed-files: auto, usage: hidden]"
	cases := []struct {
		name, body, want string
	}{
		{
			name: "no ui block",
			body: "model: qwen\n",
			want: "model: qwen\n\nui:\n  side-panel:\n    sections: " + list + "\n",
		},
		{
			name: "a ui block with keys",
			body: "ui:\n    spinner: snake\nmodel: qwen\n",
			want: "ui:\n    spinner: snake\n    side-panel:\n      sections: " + list + "\nmodel: qwen\n",
		},
		{
			name: "an empty ui block",
			body: "ui:\nmodel: qwen\n",
			want: "ui:\n  side-panel:\n    sections: " + list + "\nmodel: qwen\n",
		},
		{
			name: "an empty side-panel block",
			body: "ui:\n  side-panel:\n  spinner: snake\n",
			want: "ui:\n  side-panel:\n    sections: " + list + "\n  spinner: snake\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSectionsConfig(t, tc.body)

			if _, err := SaveSidePanelSection(path, domain.SidePanelUsage, domain.SidePanelModeHidden); err != nil {
				t.Fatalf("SaveSidePanelSection: %v", err)
			}

			if got := readSectionsConfig(t, path); got != tc.want {
				t.Errorf("config =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// `ui.side-panel.show` is the block's other key and lands through the same writer — rewritten in
// place with its note kept, or inserted beside the list where the file does not set it.
func TestSidePanelSectionsBlockWritesShow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, want string
	}{
		{
			name: "rewritten in place",
			body: "ui:\n  side-panel:\n    show: auto           # by width\n",
			want: "ui:\n  side-panel:\n    show: \"off\"           # by width\n",
		},
		{
			name: "inserted beside the list",
			body: "ui:\n  side-panel:\n    sections: [tasks: pinned]\n",
			want: "ui:\n  side-panel:\n    sections: [tasks: pinned]\n    show: \"off\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSectionsConfig(t, tc.body)

			if err := SaveSidePanelSetting(path, domain.UIKeySidePanelShow, "off"); err != nil {
				t.Fatalf("SaveSidePanelSetting: %v", err)
			}

			if got := readSectionsConfig(t, path); got != tc.want {
				t.Errorf("config =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// The whole list, as a surface hands it over, lands in the canonical spelling with every section
// named.
func TestSidePanelSectionsWholeListWrite(t *testing.T) {
	t.Parallel()
	path := writeSectionsConfig(t, "ui:\n  side-panel:\n    show: auto\n")

	if err := SaveSidePanelSetting(path, domain.UIKeySidePanelSections, "[usage: pinned]"); err != nil {
		t.Fatalf("SaveSidePanelSetting: %v", err)
	}

	want := "ui:\n  side-panel:\n    show: auto\n" +
		"    sections: [usage: pinned, sub-agents: auto, tasks: auto, changed-files: auto]\n"
	if got := readSectionsConfig(t, path); got != want {
		t.Errorf("config =\n%s\nwant\n%s", got, want)
	}
}

// A mode the file already gives the section is a confirmation: the file is not rewritten.
func TestSidePanelSectionsReSetWritesNothing(t *testing.T) {
	t.Parallel()
	const body = "ui:\n  side-panel:\n    sections: [tasks: pinned, usage: auto]\n"
	path := writeSectionsConfig(t, body)
	stale := time.Unix(1, 0)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("age the file: %v", err)
	}

	resolved, err := SaveSidePanelSection(path, domain.SidePanelTasks, domain.SidePanelModePinned)

	if err != nil {
		t.Fatalf("SaveSidePanelSection: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(stale) || readSectionsConfig(t, path) != body {
		t.Error("a re-set of the mode the file already says rewrote the file")
	}
	if want := domain.SidePanelSections("[tasks: pinned, usage: auto, sub-agents: auto, changed-files: auto]"); resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// What the writer cannot do surgically, or must not do at all, it refuses and leaves the file as it
// was: a list written one entry per line, a block in flow style, an unknown section or mode (refused
// before the file is even opened), and a list in the file that no longer parses.
func TestSidePanelSectionsWriteRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		body    string
		section domain.SidePanelSection
		mode    domain.SidePanelMode
		reason  string
	}{
		{
			name:    "a list one entry per line",
			body:    "ui:\n  side-panel:\n    sections:\n      - tasks: pinned\n",
			section: domain.SidePanelTasks, mode: domain.SidePanelModeHidden,
			reason: "one item per line",
		},
		{
			name:    "a side-panel block in flow style",
			body:    "ui:\n  side-panel: {show: auto}\n",
			section: domain.SidePanelTasks, mode: domain.SidePanelModeHidden,
			reason: "flow style",
		},
		{
			name:    "an unknown section",
			body:    "model: qwen\n",
			section: "mcp", mode: domain.SidePanelModeHidden,
			reason: "unknown section",
		},
		{
			name:    "an unknown mode",
			body:    "model: qwen\n",
			section: domain.SidePanelTasks, mode: "folded",
			reason: "unknown mode",
		},
		{
			name:    "a list in the file that no longer parses",
			body:    "ui:\n  side-panel:\n    sections: [tasks: pinned, tasks: auto]\n",
			section: domain.SidePanelUsage, mode: domain.SidePanelModeHidden,
			reason: "named twice",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSectionsConfig(t, tc.body)

			_, err := SaveSidePanelSection(path, tc.section, tc.mode)

			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.reason)
			}
			if got := readSectionsConfig(t, path); got != tc.body {
				t.Errorf("a refused write changed the file:\n%s", got)
			}
		})
	}
}

// A refused value never seeds the template: an absent config stays absent.
func TestSidePanelSectionsRefusedWriteSeedsNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")

	err := SaveSidePanelSetting(path, domain.UIKeySidePanelShow, "sometimes")

	if err == nil {
		t.Fatal("an unknown show mode was accepted")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("a refused write created the config (stat: %v)", statErr)
	}
}
