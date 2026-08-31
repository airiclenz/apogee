package config

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

// The retired top-level llama-launcher: key is refused, never folded — only the user knows which
// entry the launcher fronts. The refusal is the last time the key is ever mentioned, so it has to
// be the whole answer: the line to delete, and the per-entry key to paste in its place carrying the
// value the old one had.
func TestMigrateLegacyConfigRefusesTheRetiredLauncherKey(t *testing.T) {
	t.Parallel()
	// Five lines of new-schema config, so the retired key always lands on line 6.
	const entries = "servers:\n  - name: box\n    endpoint: http://box:1111\n\nserver: box\n"
	tests := []struct {
		name     string
		given    string
		want     []string
		unwanted []string
	}{
		{
			name:  "a path comes back as the entry's own key",
			given: entries + "llama-launcher: ~/elsewhere/launcher.yaml\n",
			want:  []string{"line 6", "servers:", "llama-launcher: ~/elsewhere/launcher.yaml"},
		},
		{
			name:  "auto stays auto",
			given: entries + "llama-launcher: auto\n",
			want:  []string{"line 6", "llama-launcher: auto"},
		},
		{
			name:  "the bare key is the old auto-detect, so the fix is auto",
			given: entries + "llama-launcher:\n",
			want:  []string{"line 6", "llama-launcher: auto"},
		},
		{
			name:     "off is a deletion, not a value to paste",
			given:    entries + "llama-launcher: OFF\n",
			want:     []string{"line 6", "Delete that line"},
			unwanted: []string{"llama-launcher: OFF", "llama-launcher: off", "llama-launcher: auto"},
		},
		{
			name:     "the refusal beats the quadruple fold",
			given:    "endpoint: http://box:1111\nllama-launcher: auto\n",
			want:     []string{"line 2", "llama-launcher: auto"},
			unwanted: []string{"host-alias:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			updated, note, err := migrateLegacyConfig(path, []byte(tt.given), migrationClock)
			if err == nil {
				t.Fatalf("a config still setting the retired launcher key was migrated to:\n%s", updated)
			}
			if note != "" {
				t.Errorf("a refused config announced itself: %s", note)
			}
			for _, want := range append([]string{path, "retired top-level llama-launcher:"}, tt.want...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not carry %q: %v", want, err)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("the refusal carries %q, which it must not: %v", unwanted, err)
				}
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the config: %v", err)
			}
			if string(onDisk) != tt.given {
				t.Errorf("a refused config was rewritten:\n%s", onDisk)
			}
			if entries, err := filepath.Glob(path + ".bak-*"); err != nil || len(entries) != 0 {
				t.Errorf("a refused config was backed up: %v (%v)", entries, err)
			}
		})
	}
}

// The refusal must not depend on the shape of the rest of the file. A config the node tree cannot be
// read from — more than one document, a flow-style top level — is read by the struct instead, and a
// bare `llama-launcher:` there is as set as one carrying a path: an empty value is the old key's
// auto-detect shape, not an absent key. Only the line number is lost with the tree, so the refusal
// names no line and still says what to paste.
func TestMigrateLegacyConfigRefusesTheValuelessLauncherKeyWithoutTheNodeTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
	}{
		{
			name:  "more than one document",
			given: "servers:\n  - name: box\n    endpoint: http://box:1111\nllama-launcher:\n---\nmode: plan\n",
		},
		{
			name:  "a flow-style top level",
			given: "{server: box, llama-launcher: }\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			updated, note, err := migrateLegacyConfig(path, []byte(tt.given), migrationClock)
			if err == nil {
				t.Fatalf("a valueless retired launcher key went unrefused; the config resolved to:\n%s", updated)
			}
			if note != "" {
				t.Errorf("a refused config announced itself: %s", note)
			}
			for _, want := range []string{path, "retired top-level llama-launcher:", "llama-launcher: auto"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not carry %q: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "on line") {
				t.Errorf("the refusal names a line the tree could not give it: %v", err)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the config: %v", err)
			}
			if string(onDisk) != tt.given {
				t.Errorf("a refused config was rewritten:\n%s", onDisk)
			}
			if backups, err := filepath.Glob(path + ".bak-*"); err != nil || len(backups) != 0 {
				t.Errorf("a refused config was backed up: %v (%v)", backups, err)
			}
		})
	}
}

// End to end: a config that still sets the retired key stops the run rather than starting a session
// whose launcher commands would all answer "not configured".
func TestApplyConfigRefusesTheRetiredLauncherKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	given := "servers:\n  - name: box\n    endpoint: http://box:1111\n\nserver: box\nllama-launcher: auto\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(given), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := Options{ConfigDir: home}
	err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { t.Errorf("a refused config announced %q", msg) })
	if err == nil {
		t.Fatal("a config still setting the retired top-level llama-launcher: key started a session")
	}
	for _, want := range []string{"retired top-level llama-launcher:", "line 6", "llama-launcher: auto"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
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
	opts := Options{ConfigDir: home}
	if err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { notes = append(notes, msg) }); err != nil {
		t.Fatalf("a legacy config was refused instead of migrated: %v", err)
	}
	if opts.Endpoint != "http://box:1111" || opts.APIKey != "sk-secret" || opts.Model != "qwen" {
		t.Errorf("upstream = %q/%q/%q; want the migrated entry's values", opts.Endpoint, opts.APIKey, opts.Model)
	}
	if opts.HostAlias != "the-box" || opts.StartupServer != "the-box" {
		t.Errorf("alias/startup server = %q/%q; want %q for both", opts.HostAlias, opts.StartupServer, "the-box")
	}
	if opts.StartupEphemeral {
		t.Error("the migrated entry was taken for an ephemeral override; it is a configured entry")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "migrated") {
		t.Errorf("startup notices = %q; want exactly the one migration line", notes)
	}
	// And the second launch reads the migrated file back without migrating anything again.
	second := Options{ConfigDir: home}
	if err := ApplyConfig(&second, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { t.Errorf("the second launch announced %q", msg) }); err != nil {
		t.Fatalf("the migrated config was refused on the next launch: %v", err)
	}
	if second.Endpoint != opts.Endpoint || second.StartupServer != opts.StartupServer {
		t.Errorf("the second launch resolved %q/%q; want the first launch's %q/%q",
			second.Endpoint, second.StartupServer, opts.Endpoint, opts.StartupServer)
	}
}

// The other retirement that is a refusal rather than a fold: the global `model-profile:` block of
// ADR 0044. The user's own block comes back nested under a pattern placeholder, so the fix is a
// paste plus the one thing only they know — which models it was written for — and a bare key, which
// configured nothing, is told to delete and nothing more.
func TestMigrateLegacyConfigRefusesTheRetiredProfileKey(t *testing.T) {
	t.Parallel()
	// Five lines of new-schema config, so the retired key always lands on line 6.
	const entries = "servers:\n  - name: box\n    endpoint: http://box:1111\n\nserver: box\n"
	const block = "model-profile:\n  tool-call-format: markdown-fenced\n  thinking:\n    style: delimited\n" +
		"    start: \"<mm:think>\"\n    end: \"</mm:think>\"\n"
	tests := []struct {
		name     string
		given    string
		want     []string
		unwanted []string
	}{
		{
			name:  "the user's own block comes back under a pattern",
			given: entries + block,
			want: []string{"line 6", "model-profiles:", "\"<pattern>\":", "tool-call-format: markdown-fenced",
				"start: \"<mm:think>\""},
		},
		{
			name:     "a bare key configured nothing, so the fix is the deletion alone",
			given:    entries + "model-profile:\n",
			want:     []string{"line 6", "model-profiles:", "Delete that line"},
			unwanted: []string{"Delete that block"},
		},
		{
			name:     "the refusal beats the quadruple fold",
			given:    "endpoint: http://box:1111\n" + block,
			want:     []string{"line 2", "model-profiles:"},
			unwanted: []string{"host-alias:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tt.given)
			updated, note, err := migrateLegacyConfig(path, []byte(tt.given), migrationClock)
			if err == nil {
				t.Fatalf("a config still setting the retired profile key was migrated to:\n%s", updated)
			}
			if note != "" {
				t.Errorf("a refused config announced itself: %s", note)
			}
			for _, want := range append([]string{path, "retired global model-profile:"}, tt.want...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not carry %q: %v", want, err)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("the refusal carries %q, which it must not: %v", unwanted, err)
				}
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the config: %v", err)
			}
			if string(onDisk) != tt.given {
				t.Errorf("a refused config was rewritten:\n%s", onDisk)
			}
		})
	}
}

// The profile refusal must not depend on the shape of the rest of the file either: a config the node
// tree cannot be read from is read by the struct instead, which still tells a set key from an absent
// one. Only the line number is lost, so the refusal names no line and still says what to paste.
func TestMigrateLegacyConfigRefusesTheProfileKeyWithoutTheNodeTree(t *testing.T) {
	t.Parallel()
	given := "{model-profile: {tool-call-format: markdown-fenced}}\n"
	path := writeMigrationConfig(t, given)
	_, _, err := migrateLegacyConfig(path, []byte(given), migrationClock)
	if err == nil {
		t.Fatal("a flow-style config still setting the retired profile key was not refused")
	}
	for _, want := range []string{"retired global model-profile:", "model-profiles:", "tool-call-format: markdown-fenced"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "line ") {
		t.Errorf("the refusal names a line the struct fallback cannot know: %v", err)
	}
}

// End to end: the retired global block stops STARTUP, with the map spelling in the message — the
// loud error ADR 0044 chose over a back-compat layer.
func TestApplyConfigRefusesTheRetiredProfileKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	given := "servers:\n  - name: box\n    endpoint: http://box:1111\n\nserver: box\n" +
		"model-profile:\n  thinking:\n    style: harmony\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(given), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := Options{ConfigDir: home}
	err := ApplyConfig(&opts, func(string) bool { return false }, func(string) string { return "" },
		os.ReadFile, func(msg string) { t.Errorf("a refused config announced %q", msg) })
	if err == nil {
		t.Fatal("a config still setting the retired global model-profile: key started a session")
	}
	for _, want := range []string{"retired global model-profile:", "line 6", "model-profiles:", "style: harmony"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// The consented `sub-agents:` flag migration (ADR 0045): the detection, and the one edit the offer's
// "move it" row makes.

// A config carrying the retired flag names the entries that carry it, in the file's own order — and
// a config carrying none, or no file at all, offers nothing.
func TestRetiredSubAgentsEntriesNamesTheFlaggedEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "one flagged entry",
			body: "servers:\n  - name: box\n    endpoint: http://a\n    sub-agents: true\nserver: box\n",
			want: []string{"box"},
		},
		{
			name: "two flagged entries keep the file's order",
			body: "servers:\n  - name: first\n    endpoint: http://a\n    sub-agents: true\n" +
				"  - name: second\n    endpoint: http://b\n    sub-agents: true\nserver: first\n",
			want: []string{"first", "second"},
		},
		{
			name: "an explicit false is not the flag",
			body: "servers:\n  - name: box\n    endpoint: http://a\n    sub-agents: false\nserver: box\n",
		},
		{
			name: "no flag at all",
			body: startupServerYAML,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names, err := RetiredSubAgentsEntries(writeMigrationConfig(t, tc.body))
			if err != nil {
				t.Fatalf("RetiredSubAgentsEntries: %v", err)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("named %v, want %v", names, tc.want)
			}
		})
	}
}

// A config that is not there yet carries nothing retired: no names, and no error — a start-up
// question is never a reason a start fails.
func TestRetiredSubAgentsEntriesIsSilentWithoutAConfig(t *testing.T) {
	t.Parallel()
	names, err := RetiredSubAgentsEntries(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil || len(names) != 0 {
		t.Fatalf("RetiredSubAgentsEntries on an absent config = %v, %v; want nothing at all", names, err)
	}
}

// The edit itself: every flag line goes, the root key arrives naming the entry that was answered
// with, and every other line of the user's file is exactly where it was.
func TestMigrateSubAgentsServerDropsTheFlagsAndWritesTheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry string
		given string
		want  string
	}{
		{
			name:  "one flagged entry, with its comments and its neighbours",
			entry: "cheap",
			given: "# my apogee\nservers:\n  - name: box\n    endpoint: http://a\n" +
				"  - name: cheap\n    endpoint: http://b\n    sub-agents: true  # the delegation box\n" +
				"server: box\nauto-title: false\n",
			want: "# my apogee\nservers:\n  - name: box\n    endpoint: http://a\n" +
				"  - name: cheap\n    endpoint: http://b\n" +
				"server: box\nauto-title: false\n\nsub-agents-server: cheap\n",
		},
		{
			name:  "two flagged entries lose both lines; the answered one is the key",
			entry: "first",
			given: "servers:\n  - name: first\n    endpoint: http://a\n    sub-agents: true\n" +
				"  - name: second\n    endpoint: http://b\n    sub-agents: true\nserver: first\n",
			want: "servers:\n  - name: first\n    endpoint: http://a\n" +
				"  - name: second\n    endpoint: http://b\nserver: first\n\nsub-agents-server: first\n",
		},
		{
			name:  "the key is already there: only the flag line goes",
			entry: "cheap",
			given: "servers:\n  - name: cheap\n    endpoint: http://b\n    sub-agents: true\n" +
				"server: cheap\nsub-agents-server: cheap\n",
			want: "servers:\n  - name: cheap\n    endpoint: http://b\n" +
				"server: cheap\nsub-agents-server: cheap\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeMigrationConfig(t, tc.given)
			if err := MigrateSubAgentsServer(path, tc.entry); err != nil {
				t.Fatalf("MigrateSubAgentsServer: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("the migrated file reads\n%s\nwant\n%s", data, tc.want)
			}
		})
	}
}

// The rewrite has to survive the read-back the seam performs: the whole-file gate zeroes BOTH paths
// the edit changes, so a config whose OTHER settings the splice left alone passes, and the resolved
// key is the entry that was answered with.
func TestMigrateSubAgentsServerResolvesToTheAnsweredEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfigHome(t, dir, "servers:\n  - name: testbox\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: cheap\n    endpoint: http://127.0.0.1:2222\n    sub-agents: true\nserver: testbox\n")
	path := filepath.Join(dir, "config.yaml")
	if err := MigrateSubAgentsServer(path, "cheap"); err != nil {
		t.Fatalf("MigrateSubAgentsServer: %v", err)
	}
	opts := Options{ConfigDir: dir}
	if err := ApplyConfig(&opts, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("the migrated config would not load: %v", err)
	}
	if opts.SubAgentsServer != "cheap" {
		t.Errorf("sub-agents-server resolved to %q, want cheap", opts.SubAgentsServer)
	}
}

// A name no entry carries is refused with the writer's "by hand" idiom, and the file is left exactly
// as it was: the offer answers with an entry name, and a name the list does not hold is a name
// nothing can be written for.
func TestMigrateSubAgentsServerRefusesANameNoEntryCarries(t *testing.T) {
	t.Parallel()
	given := "servers:\n  - name: box\n    endpoint: http://a\n    sub-agents: true\nserver: box\n"
	path := writeMigrationConfig(t, given)
	err := MigrateSubAgentsServer(path, "nope")
	if err == nil {
		t.Fatal("a name no entry carries was accepted")
	}
	if !strings.Contains(err.Error(), "edit the file by hand") {
		t.Errorf("refusal reads %q, want the writer's by-hand idiom", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != given {
		t.Errorf("the refused edit rewrote the file:\n%s", data)
	}
}
