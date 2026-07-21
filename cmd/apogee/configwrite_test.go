package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The fixed clock every test writes with, so the rendered `acknowledged:` date is a constant the
// golden files can name.
var saveClock = time.Date(2026, 7, 21, 15, 4, 5, 0, time.UTC)

// The host being acknowledged, and the entry the writer must produce for it at saveClock.
const (
	savedHostID = "devbox-a1b2c3"
	savedEntry  = "  - id: devbox-a1b2c3\n" +
		"    acknowledged: \"2026-07-21\"\n" +
		"    note: added by /confine off --save; delete to confine this machine again\n"
)

// writeTestConfig drops content at <tempdir>/config.yaml and returns the path.
func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}

// readTestConfig reads a config file back as a string.
func readTestConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config back: %v", err)
	}
	return string(data)
}

// acknowledgedIDs parses a config the way apogee itself does and lists the ids it acknowledges —
// the property that actually matters, as opposed to the bytes the golden files pin.
func acknowledgedIDs(t *testing.T, content string) []string {
	t.Helper()
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(content), &fc); err != nil {
		t.Fatalf("the written config does not parse: %v\n%s", err, content)
	}
	ids := make([]string, 0, len(fc.UnconfinedHosts))
	for _, h := range fc.UnconfinedHosts {
		ids = append(ids, h.ID)
	}
	return ids
}

func TestSaveHostAcknowledgement_SeedsAbsentConfigThenAppends(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")

	written, entry, err := saveHostAcknowledgement(path, savedHostID, saveClock)
	if err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	if written != path {
		t.Errorf("written path = %q, want %q", written, path)
	}
	want := unconfinedHost{ID: savedHostID, Acknowledged: "2026-07-21", Note: hostAcknowledgementNote}
	if entry != want {
		t.Errorf("entry = %+v, want %+v", entry, want)
	}

	got := readTestConfig(t, path)
	// The absent config is seeded from the embedded template, never left as a bare fragment —
	// and the template is ENTIRELY comments, so this is also the sharpest test that the writer
	// does not round-trip through unmarshal→marshal (which would have deleted all of it).
	for _, doc := range []string{
		"# apogee configuration — ~/.apogee/config.yaml",
		"# confine-to-workspace: true",
		"#   - id: \"devbox-a1b2c3\"                # this machine's host id (apogee prints it)",
		"# validated-sets:",
	} {
		if !strings.Contains(got, doc) {
			t.Errorf("the seeded template's documentation was lost: %q is gone", doc)
		}
	}
	if !strings.HasSuffix(got, "unconfined-hosts:\n"+savedEntry) {
		t.Errorf("the acknowledgement block is not at the end of the file:\n%s", got)
	}
	if ids := acknowledgedIDs(t, got); len(ids) != 1 || ids[0] != savedHostID {
		t.Errorf("acknowledged ids = %v, want [%s]", ids, savedHostID)
	}
}

func TestSaveHostAcknowledgement_AppendsToAnExistingList(t *testing.T) {
	t.Parallel()
	const before = `# apogee configuration — hand-edited
model: qwen2.5-coder        # the pinned model

# Machines I have acknowledged as disposable.
unconfined-hosts:
  - id: "laptop-aaa111"
    acknowledged: "2026-07-01"
    note: "the old box"

# Search endpoint.
web-search-endpoint: "off"
`
	const want = `# apogee configuration — hand-edited
model: qwen2.5-coder        # the pinned model

# Machines I have acknowledged as disposable.
unconfined-hosts:
  - id: "laptop-aaa111"
    acknowledged: "2026-07-01"
    note: "the old box"
  - id: devbox-a1b2c3
    acknowledged: "2026-07-21"
    note: added by /confine off --save; delete to confine this machine again

# Search endpoint.
web-search-endpoint: "off"
`
	path := writeTestConfig(t, before)
	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	if got := readTestConfig(t, path); got != want {
		t.Errorf("config after save:\n%s\nwant:\n%s", got, want)
	}
}

// A config the user has since reordered and reformatted by hand — the acknowledgement list first,
// its items at column 0, a missing optional field — must still take the entry, and still only the
// entry.
func TestSaveHostAcknowledgement_HandReorderedConfig(t *testing.T) {
	t.Parallel()
	const before = `unconfined-hosts:
- id: "laptop-aaa111"
  note: "no acknowledged date, on purpose"
endpoint: http://192.168.64.1:1111
mode: auto
`
	const want = `unconfined-hosts:
- id: "laptop-aaa111"
  note: "no acknowledged date, on purpose"
- id: devbox-a1b2c3
  acknowledged: "2026-07-21"
  note: added by /confine off --save; delete to confine this machine again
endpoint: http://192.168.64.1:1111
mode: auto
`
	path := writeTestConfig(t, before)
	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	if got := readTestConfig(t, path); got != want {
		t.Errorf("config after save:\n%s\nwant:\n%s", got, want)
	}
}

// The key ships COMMENTED OUT in the template, so the writer must insert a block rather than
// substitute a value — and must leave the commented documentation exactly where it found it.
func TestSaveHostAcknowledgement_InsertsWhenTheKeyIsCommentedOut(t *testing.T) {
	t.Parallel()
	const before = `# Machines you have acknowledged as disposable.
# unconfined-hosts:
#   - id: "devbox-a1b2c3"
#     acknowledged: "2026-07-21"
model: qwen2.5-coder
`
	const want = `# Machines you have acknowledged as disposable.
# unconfined-hosts:
#   - id: "devbox-a1b2c3"
#     acknowledged: "2026-07-21"
model: qwen2.5-coder

` + hostAcknowledgementHeader + `
unconfined-hosts:
` + savedEntry
	path := writeTestConfig(t, before)
	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	if got := readTestConfig(t, path); got != want {
		t.Errorf("config after save:\n%s\nwant:\n%s", got, want)
	}
}

// The bare key with nothing under it (a user who uncommented only the key) starts the list rather
// than appending a second `unconfined-hosts:`.
func TestSaveHostAcknowledgement_StartsAnEmptyList(t *testing.T) {
	t.Parallel()
	const before = `unconfined-hosts:
model: qwen2.5-coder
`
	const want = `unconfined-hosts:
` + savedEntry + `model: qwen2.5-coder
`
	path := writeTestConfig(t, before)
	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	got := readTestConfig(t, path)
	if got != want {
		t.Errorf("config after save:\n%s\nwant:\n%s", got, want)
	}
	if ids := acknowledgedIDs(t, got); len(ids) != 1 || ids[0] != savedHostID {
		t.Errorf("acknowledged ids = %v, want [%s]", ids, savedHostID)
	}
}

// Saving the same host twice records it once: the second call reports the entry already on disk
// (dates and all) and leaves the file byte-identical.
func TestSaveHostAcknowledgement_IsIdempotent(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, "model: qwen2.5-coder\n")

	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first := readTestConfig(t, path)

	later := saveClock.AddDate(0, 1, 0)
	_, entry, err := saveHostAcknowledgement(path, savedHostID, later)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got := readTestConfig(t, path); got != first {
		t.Errorf("a repeated save rewrote the config:\n%s\nwant:\n%s", got, first)
	}
	// The entry reported back is the one on disk, not the one the second call would have written.
	if entry.Acknowledged != "2026-07-21" {
		t.Errorf("reported acknowledged = %q, want the stored 2026-07-21", entry.Acknowledged)
	}
	if ids := acknowledgedIDs(t, first); len(ids) != 1 {
		t.Errorf("acknowledged ids = %v, want exactly one", ids)
	}
}

// A config may hold endpoint details, so the rewrite must not widen its permissions.
func TestSaveHostAcknowledgement_PreservesTheFileMode(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, "model: qwen2.5-coder\n")
	const mode = 0o640
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err != nil {
		t.Fatalf("saveHostAcknowledgement: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("mode after save = %04o, want %04o", got, mode)
	}
}

// Every shape the writer refuses: it must say so rather than report a save that did not happen,
// and must leave the file exactly as it found it.
func TestSaveHostAcknowledgement_Errors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		content string
		hostID  string
		wantMsg string
	}{
		{
			name:    "no host id to record",
			content: "model: qwen2.5-coder\n",
			hostID:  "   ",
			wantMsg: "no id",
		},
		{
			name:    "a config that does not parse",
			content: "unconfined-hosts: [oops\n",
			hostID:  savedHostID,
			wantMsg: "update config",
		},
		{
			name:    "a flow-style list has no line to append to",
			content: "unconfined-hosts: [{id: laptop-aaa111}]\n",
			hostID:  savedHostID,
			wantMsg: "flow style",
		},
		{
			name:    "a second document apogee would never read",
			content: "model: qwen2.5-coder\n---\nmodel: other\n",
			hostID:  savedHostID,
			wantMsg: "more than one YAML document",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.content)
			written, entry, err := saveHostAcknowledgement(path, tt.hostID, saveClock)
			if err == nil {
				t.Fatalf("want an error, got path %q entry %+v", written, entry)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantMsg)
			}
			if written != "" {
				t.Errorf("a failed save reported the path %q", written)
			}
			if got := readTestConfig(t, path); got != tt.content {
				t.Errorf("a failed save changed the file:\n%s", got)
			}
		})
	}
}

// An unwritable destination surfaces as an error, not a silent success. A directory where the
// config file should be is the portable way to say "this path cannot be written" — unlike file
// permissions, it holds for root too.
func TestSaveHostAcknowledgement_UnwritablePath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if written, _, err := saveHostAcknowledgement(path, savedHostID, saveClock); err == nil {
		t.Fatalf("want an error writing over a directory, got path %q", written)
	}
	if written, _, err := saveHostAcknowledgement("", savedHostID, saveClock); err == nil {
		t.Fatalf("want an error with no config path, got path %q", written)
	}
}

// The seam the TUI actually calls: it reports the file that now records this host, and nothing
// about the format.
func TestHostAcknowledgementSaver(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, "model: qwen2.5-coder\n")

	written, err := hostAcknowledgementSaver(path, savedHostID)()
	if err != nil {
		t.Fatalf("saver: %v", err)
	}
	if written != path {
		t.Errorf("written path = %q, want %q", written, path)
	}
	if ids := acknowledgedIDs(t, readTestConfig(t, path)); len(ids) != 1 || ids[0] != savedHostID {
		t.Errorf("acknowledged ids = %v, want [%s]", ids, savedHostID)
	}
}
