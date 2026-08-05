package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/prompt"
)

// An absent config is seeded: seedConfig creates the parent directory and writes the
// content, reporting that it created a new file.
func TestSeedConfigCreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "config.yaml") // parent does not exist yet
	content := []byte("# starter\n")

	created, err := seedConfig(path, content)
	if err != nil {
		t.Fatalf("seedConfig: %v", err)
	}
	if !created {
		t.Fatal("created = false; want true for an absent file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("seeded content = %q; want %q", got, content)
	}
}

// An existing config is never overwritten — the user's edits win over the template.
func TestSeedConfigDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	mine := []byte("model: mine\n")
	if err := os.WriteFile(path, mine, 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	created, err := seedConfig(path, []byte("# template\n"))
	if err != nil {
		t.Fatalf("seedConfig: %v", err)
	}
	if created {
		t.Error("created = true; want false — an existing config must not be overwritten")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(mine) {
		t.Errorf("existing config was modified: %q", got)
	}
}

// The embedded starter config is valid YAML and sets EXACTLY one thing: the default system
// prompt (ADR 0023), the template's one deliberately-active key. Every other key stays a
// commented example, so seeding the template on first run changes nothing else about how a
// run resolves. This is the precise form of the old "the template is behaviour-neutral"
// invariant, amended when the shipped prompt went active.
func TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt(t *testing.T) {
	t.Parallel()
	if len(defaultConfigYAML) == 0 {
		t.Fatal("defaultConfigYAML is empty; the embed did not pick up defaults/config.yaml")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, defaultConfigYAML, 0o600); err != nil {
		t.Fatalf("write embedded: %v", err)
	}
	l, err := loadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("embedded default config does not parse: %v", err)
	}

	// The one active key: an inline global prompt, no file, no per-model entries.
	if l.systemPrompt == nil {
		t.Fatal("embedded default config carries no system prompt; the shipped default must be active")
	}
	text := l.systemPrompt.global.text
	for _, want := range []string{
		"You are apogee",
		prompt.PlaceholderWorkspace,
		prompt.PlaceholderDatetime,
		prompt.PlaceholderMode,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("shipped system prompt does not contain %q:\n%s", want, text)
		}
	}
	if l.systemPrompt.global.file != "" {
		t.Errorf("shipped system-prompt-file = %q; want empty — the file key stays a commented example",
			l.systemPrompt.global.file)
	}
	if l.systemPrompt.models != nil {
		t.Errorf("shipped system-prompt-models = %+v; want none — the per-model block stays a commented example",
			l.systemPrompt.models)
	}

	// The shipped prompt must survive both gates a user's own prompt faces.
	if err := prompt.Validate(text); err != nil {
		t.Errorf("shipped system prompt fails prompt.Validate: %v", err)
	}
	if err := l.systemPrompt.validate(); err != nil {
		t.Errorf("shipped system-prompt block fails validate: %v", err)
	}

	// Everything else still parses to nothing — the surviving half of the old invariant.
	l.systemPrompt = nil
	if !reflect.DeepEqual(l, layer{}) {
		t.Errorf("embedded default config sets values beyond the system prompt: %+v", l)
	}
}

// The template documents the upstream api key where a reader meets the server it belongs to —
// inside a `servers:` entry, its only home since ADR 0036 retired the top-level key: the field's
// own spelling, the environment variable that overrides it for the startup server, and the two
// facts a secret in a plain-text file needs (the env var wins; restrict the file otherwise). It
// stays a COMMENTED example — the behaviour-neutrality above already pins that the seeded file
// resolves no key.
func TestEmbeddedDefaultConfigDocumentsTheAPIKey(t *testing.T) {
	t.Parallel()
	template := string(defaultConfigYAML)
	for _, want := range []string{
		"api-key: sk-rented-token", // the field itself, as a commented example inside an entry
		"APOGEE_API_KEY",           // the environment variable that overrides it
		"no flag",                  // why the third precedence layer is deliberately absent
		"plain text",               // the shared-machine caveat
	} {
		if !strings.Contains(template, want) {
			t.Errorf("embedded template does not mention %q; the api-key documentation is missing or reworded", want)
		}
	}
}

// The template teaches the schema ADR 0036 settled, and only that one: the `servers:` list and the
// `server:` startup choice are each documented once, as a commented example the splice writer can
// place a real setting under, and none of the four retired top-level keys is offered any more. A
// template still showing `endpoint:` at the top level would teach a config that no longer resolves,
// and a second example of either surviving key would leave the writer with no one place to write.
func TestEmbeddedDefaultConfigTeachesTheServersSchema(t *testing.T) {
	t.Parallel()
	lines := splitConfigLines(defaultConfigYAML)
	retired := map[string]bool{"endpoint": true, "api-key": true, "host-alias": true, "model": true}
	for i, line := range lines {
		indent, name, ok := commentedKey(line)
		if ok && indent == 0 && retired[name] {
			t.Errorf("line %d still offers the retired top-level %s: — %q", i+1, name, line)
		}
	}
	// Each surviving key is documented exactly once (commentedExampleLine refuses a second one),
	// and the line it reports is where a first write of that key lands.
	at, err := commentedExampleLine(lines, "server")
	if err != nil || at == 0 {
		t.Fatalf("the template documents no single `# server:` example (line %d, err %v); a recorded "+
			"choice would be appended at the end of the file instead", at, err)
	}
	end, err := commentedExampleBlockEnd(lines, "servers")
	if err != nil || end == 0 {
		t.Fatalf("the template documents no `# servers:` example block (end %d, err %v); the migration "+
			"fold would append the list at the end of the file instead", end, err)
	}
	if end >= at {
		t.Errorf("the `# servers:` example runs to line %d, past the `# server:` example on line %d; the "+
			"two examples have run together and a write to one would land inside the other", end, at)
	}
}

// seedDefaultConfig honours an explicit --config home and seeds the embedded template there.
func TestSeedDefaultConfigHonoursConfigFlag(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	flagSet := func(name string) bool { return name == "config" }
	noEnv := func(string) string { return "" }

	created, path, err := seedDefaultConfig(options{configDir: home}, flagSet, noEnv)
	if err != nil {
		t.Fatalf("seedDefaultConfig: %v", err)
	}
	if !created {
		t.Fatal("created = false; want true on first run")
	}
	if filepath.Dir(path) != home {
		t.Errorf("seeded to %q; want a file under the --config home %q", path, home)
	}

	// A second run finds the file and does not recreate it.
	created2, _, err := seedDefaultConfig(options{configDir: home}, flagSet, noEnv)
	if err != nil {
		t.Fatalf("seedDefaultConfig (second run): %v", err)
	}
	if created2 {
		t.Error("created = true on the second run; the existing config should be left alone")
	}
}

// seedDefaultConfig honours APOGEE_CONFIG when --config is not set.
func TestSeedDefaultConfigHonoursConfigEnv(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	getenv := func(k string) string {
		if k == envConfig {
			return home
		}
		return ""
	}
	created, path, err := seedDefaultConfig(options{}, func(string) bool { return false }, getenv)
	if err != nil {
		t.Fatalf("seedDefaultConfig: %v", err)
	}
	if !created || filepath.Dir(path) != home {
		t.Errorf("created=%v path=%q; want a new file under the APOGEE_CONFIG home %q", created, path, home)
	}
}
