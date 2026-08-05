package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// migrationClock is the fixed time the tests date backups with, so the file the migration writes has
// a name they can name back.
var migrationClock = time.Date(2026, 8, 5, 9, 30, 15, 0, time.UTC)

// migrationBackupSuffix is that time as the backup's name carries it.
const migrationBackupSuffix = ".bak-20260805-093015"

// writeMigrationConfig puts a config in a fresh apogee home and returns its path.
func writeMigrationConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// A legacy config is rewritten in place, and the rewrite is a FOLD: the four retired lines go, the
// entry and the pointer that replace them arrive, and every other line — comments, blank lines,
// unrelated settings, the user's own key order — is exactly where it was. The whole file is
// compared, because "what else did the splice touch" is the question this write has to answer.
func TestMigrateLegacyConfigFoldsTheQuadruple(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
		want  string
	}{
		{
			name: "the whole quadruple, with its comments and its neighbours",
			given: "# apogee configuration\n" +
				"\n" +
				"# The server this session talks to.\n" +
				"endpoint: http://box:1111\n" +
				"api-key: sk-secret\n" +
				"host-alias: the-box\n" +
				"model: qwen\n" +
				"\n" +
				"# How much autonomy.\n" +
				"mode: plan\n",
			want: "# apogee configuration\n" +
				"\n" +
				"# The server this session talks to.\n" +
				"\n" +
				"# How much autonomy.\n" +
				"mode: plan\n" +
				"\n" +
				"servers:\n" +
				"  - name: the-box\n" +
				"    endpoint: http://box:1111\n" +
				"    api-key: sk-secret\n" +
				"    model: qwen\n" +
				"\n" +
				"server: the-box\n",
		},
		{
			name:  "the endpoint alone names the entry after its host",
			given: "endpoint: http://box:1111\nmode: plan\n",
			want: "mode: plan\n" +
				"\n" +
				"servers:\n" +
				"  - name: box\n" +
				"    endpoint: http://box:1111\n" +
				"\n" +
				"server: box\n",
		},
		{
			name: "an existing list gains the entry at its own indentation",
			given: "endpoint: http://box:1111\n" +
				"host-alias: the-box\n" +
				"\n" +
				"servers:\n" +
				"  - name: rented\n" +
				"    endpoint: https://llm.example.com\n" +
				"    api-key: sk-rented\n" +
				"\n" +
				"mode: plan\n",
			want: "\n" +
				"servers:\n" +
				"  - name: rented\n" +
				"    endpoint: https://llm.example.com\n" +
				"    api-key: sk-rented\n" +
				"  - name: the-box\n" +
				"    endpoint: http://box:1111\n" +
				"\n" +
				"mode: plan\n" +
				"\n" +
				"server: the-box\n",
		},
		{
			name: "the entry lands below the commented example that documents the list",
			given: "endpoint: http://box:1111\n" +
				"\n" +
				"# The other servers you run models on.\n" +
				"# servers:\n" +
				"#   - name: workstation\n" +
				"#     endpoint: http://192.168.64.1:1111\n" +
				"\n" +
				"# server: my-box\n" +
				"\n" +
				"mode: plan\n",
			want: "\n" +
				"# The other servers you run models on.\n" +
				"# servers:\n" +
				"#   - name: workstation\n" +
				"#     endpoint: http://192.168.64.1:1111\n" +
				"servers:\n" +
				"  - name: box\n" +
				"    endpoint: http://box:1111\n" +
				"\n" +
				"# server: my-box\n" +
				"server: box\n" +
				"\n" +
				"mode: plan\n",
		},
		{
			name:  "a bare servers: key becomes the list",
			given: "endpoint: http://box:1111\nservers:\n",
			want: "servers:\n" +
				"  - name: box\n" +
				"    endpoint: http://box:1111\n" +
				"\n" +
				"server: box\n",
		},
		{
			name:  "a value that would not survive as a bare scalar is quoted back",
			given: "endpoint: http://box:1111\nhost-alias: \"true\"\napi-key: \"123\"\n",
			want: "servers:\n" +
				"  - name: \"true\"\n" +
				"    endpoint: http://box:1111\n" +
				"    api-key: \"123\"\n" +
				"\n" +
				"server: \"true\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			updated, note, err := migrateLegacyConfig(path, []byte(tt.given), migrationClock)
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			if string(updated) != tt.want {
				t.Errorf("migrated config:\n%s\nwant:\n%s", updated, tt.want)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the migrated config: %v", err)
			}
			if string(onDisk) != tt.want {
				t.Errorf("the file on disk is not what the migration returned:\n%s", onDisk)
			}
			if note == "" {
				t.Error("a rewritten config was not announced")
			}
			backup, err := os.ReadFile(path + migrationBackupSuffix)
			if err != nil {
				t.Fatalf("read the backup: %v", err)
			}
			if string(backup) != tt.given {
				t.Errorf("the backup is not the file as it was:\n%s", backup)
			}
		})
	}
}

// The backup is the user's undo, so it carries the config's own permissions: a file that may hold an
// api key must not be copied into a more readable one.
func TestMigrateLegacyConfigBackupKeepsThePermissions(t *testing.T) {
	t.Parallel()
	given := "endpoint: http://box:1111\n"
	path := writeMigrationConfig(t, given)
	if _, _, err := migrateLegacyConfig(path, []byte(given), migrationClock); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	info, err := os.Stat(path + migrationBackupSuffix)
	if err != nil {
		t.Fatalf("stat the backup: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode = %v; want the config's own 0600", got)
	}
}

// The one startup line is the user's only notice that a file they own was rewritten, so it names
// what moved, what the entry is called, and where the previous bytes are.
func TestMigrateLegacyConfigAnnouncesWhatMoved(t *testing.T) {
	t.Parallel()
	given := "endpoint: http://box:1111\napi-key: sk-secret\nhost-alias: the-box\n"
	path := writeMigrationConfig(t, given)
	_, note, err := migrateLegacyConfig(path, []byte(given), migrationClock)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, want := range []string{path, "endpoint:", "api-key:", "host-alias:", `"the-box"`, path + migrationBackupSuffix} {
		if !strings.Contains(note, want) {
			t.Errorf("the announcement does not carry %q: %s", want, note)
		}
	}
	if strings.Contains(note, "model:") {
		t.Errorf("the announcement claims a model: moved, which the config never set: %s", note)
	}
	if strings.Contains(note, "\n") {
		t.Errorf("the announcement is more than one line: %s", note)
	}
	if strings.Contains(note, "sk-secret") {
		t.Errorf("the announcement prints the api key: %s", note)
	}
}

// A config the fold cannot make safely is left EXACTLY as it was — no rewrite, and no backup either,
// since a copy is a write too — and the refusal carries the block to paste in by hand.
func TestMigrateLegacyConfigRefusesRatherThanGuess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		given   string
		wantWhy string
	}{
		{
			name:    "a name the list already uses",
			given:   "endpoint: http://box:1111\nhost-alias: rented\nservers:\n  - name: rented\n    endpoint: https://llm.example.com\n",
			wantWhy: "already has an entry called",
		},
		{
			name:    "a startup choice the file already made",
			given:   "endpoint: http://box:1111\nserver: rented\nservers:\n  - name: rented\n    endpoint: https://llm.example.com\n",
			wantWhy: "already starts on server:",
		},
		{
			name:    "keys with no endpoint among them",
			given:   "api-key: sk-secret\nmodel: qwen\n",
			wantWhy: "no endpoint:",
		},
		{
			name:    "a list written in flow style",
			given:   "endpoint: http://box:1111\nservers: [{name: rented, endpoint: https://llm.example.com}]\n",
			wantWhy: "flow style",
		},
		{
			name:    "a retired key written as a block scalar",
			given:   "endpoint: >-\n  http://box:1111\n",
			wantWhy: "multi-line block",
		},
		{
			name:    "a second document apogee would never read",
			given:   "endpoint: http://box:1111\n---\nmode: plan\n",
			wantWhy: "more than one YAML document",
		},
		{
			name:    "a servers: key holding something else",
			given:   "endpoint: http://box:1111\nservers: nonsense\n",
			wantWhy: "does not parse into settings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			updated, note, err := migrateLegacyConfig(path, []byte(tt.given), migrationClock)
			if err == nil {
				t.Fatalf("a config that cannot be folded was migrated to:\n%s", updated)
			}
			if note != "" {
				t.Errorf("a refused migration announced itself: %s", note)
			}
			for _, want := range []string{"retired top-level", tt.wantWhy, "servers:", "server: "} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not carry %q: %v", want, err)
				}
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the config: %v", err)
			}
			if string(onDisk) != tt.given {
				t.Errorf("a refused migration rewrote the config:\n%s", onDisk)
			}
			if entries, err := filepath.Glob(path + ".bak-*"); err != nil || len(entries) != 0 {
				t.Errorf("a refused migration left a backup: %v (%v)", entries, err)
			}
		})
	}
}

// A config already in the new schema is never opened for writing: the sniff is the only trigger, so
// every launch after the one that migrated leaves the file completely alone.
func TestMigrateLegacyConfigLeavesANewSchemaConfigAlone(t *testing.T) {
	t.Parallel()
	given := "servers:\n  - name: box\n    endpoint: http://box:1111\n\nserver: box\nmode: plan\n"
	path := writeMigrationConfig(t, given)
	updated, note, err := migrateLegacyConfig(path, []byte(given), migrationClock)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if string(updated) != given {
		t.Errorf("a new-schema config was changed:\n%s", updated)
	}
	if note != "" {
		t.Errorf("a new-schema config was announced as migrated: %s", note)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read the apogee home: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the apogee home holds %d files; the migration must not have written one", len(entries))
	}
}

// End to end: a legacy config resolves, on the launch that migrates it, to the server it always
// described — the point of folding rather than refusing — and the run says so once.
func TestApplyConfigMigratesTheRetiredKeys(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	given := "endpoint: http://box:1111\napi-key: sk-secret\nhost-alias: the-box\nmodel: qwen\nmode: plan\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(given), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var notes []string
	opts := options{configDir: home}
	if err := applyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { notes = append(notes, msg) }); err != nil {
		t.Fatalf("a legacy config was refused instead of migrated: %v", err)
	}
	if opts.endpoint != "http://box:1111" || opts.apiKey != "sk-secret" || opts.model != "qwen" {
		t.Errorf("upstream = %q/%q/%q; want the migrated entry's values", opts.endpoint, opts.apiKey, opts.model)
	}
	if opts.hostAlias != "the-box" || opts.startupServer != "the-box" {
		t.Errorf("alias/startup server = %q/%q; want %q for both", opts.hostAlias, opts.startupServer, "the-box")
	}
	if opts.startupEphemeral {
		t.Error("the migrated entry was taken for an ephemeral override; it is a configured entry")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "migrated") {
		t.Errorf("startup notices = %q; want exactly the one migration line", notes)
	}
	// And the second launch reads the migrated file back without migrating anything again.
	second := options{configDir: home}
	if err := applyConfig(&second, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { t.Errorf("the second launch announced %q", msg) }); err != nil {
		t.Fatalf("the migrated config was refused on the next launch: %v", err)
	}
	if second.endpoint != opts.endpoint || second.startupServer != opts.startupServer {
		t.Errorf("the second launch resolved %q/%q; want the first launch's %q/%q",
			second.endpoint, second.startupServer, opts.endpoint, opts.startupServer)
	}
}
