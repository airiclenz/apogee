package config

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
	l, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("embedded default config does not parse: %v", err)
	}

	// The one active key: an inline global prompt, no file, no per-model entries.
	if l.SystemPrompt == nil {
		t.Fatal("embedded default config carries no system prompt; the shipped default must be active")
	}
	text := l.SystemPrompt.Global.Text
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
	if l.SystemPrompt.Global.File != "" {
		t.Errorf("shipped system-prompt-file = %q; want empty — the file key stays a commented example",
			l.SystemPrompt.Global.File)
	}
	if l.SystemPrompt.Models != nil {
		t.Errorf("shipped system-prompt-models = %+v; want none — the per-model block stays a commented example",
			l.SystemPrompt.Models)
	}

	// The shipped prompt must survive both gates a user's own prompt faces.
	if err := prompt.Validate(text); err != nil {
		t.Errorf("shipped system prompt fails prompt.Validate: %v", err)
	}
	if err := l.SystemPrompt.Validate(); err != nil {
		t.Errorf("shipped system-prompt block fails validate: %v", err)
	}

	// Everything else still parses to nothing — the surviving half of the old invariant.
	l.SystemPrompt = nil
	if !reflect.DeepEqual(l, Layer{}) {
		t.Errorf("embedded default config sets values beyond the system prompt: %+v", l)
	}
}

// The template documents the upstream api key where a reader meets the server it belongs to —
// inside a `servers:` entry, its only home since ADR 0036 retired the top-level key: the field's
// own spelling, the environment variable that overrides it for the startup server, and the two
// facts a secret in a plain-text file needs (the env var wins; restrict the file otherwise). It
// stays a COMMENTED example — the behaviour-neutrality above already pins that the seeded file
// resolves no key.
//
// The other two KEY SOURCES are documented on the same footing, because the whole point of them is
// that the reader who has just been warned about a secret in a plain-text file needs somewhere else
// to put it: a command spelled for the three managers people actually run, a named environment
// variable, the exactly-one rule that stops a file setting two of them, and the GUI-prompt note
// that says what an interactive backend has to do when apogee gives its command no terminal.
func TestEmbeddedDefaultConfigDocumentsTheAPIKey(t *testing.T) {
	t.Parallel()
	template := string(defaultConfigYAML)
	for _, want := range []string{
		"api-key: sk-rented-token", // the field itself, as a commented example inside an entry
		"APOGEE_API_KEY",           // the environment variable that overrides it
		"no flag",                  // why the third precedence layer is deliberately absent
		"plain text",               // the shared-machine caveat
		"api-key-cmd: pass show apogee/rented-box",                                 // the command source, password-manager spelling
		"api-key-cmd: op read op://",                                               // …its 1Password spelling
		"api-key-cmd: security find-generic-password -s apogee -a keychain-box -w", // …and the macOS keychain one
		"api-key-env: OPENROUTER_API_KEY",                                          // the environment-variable source
		"Exactly ONE of api-key, api-key-cmd and api-key-env per entry",            // the rule ValidateServers enforces
		"pinentry-mac", // how an interactive backend must ask
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
	lines := SplitConfigLines(defaultConfigYAML)
	retired := map[string]bool{"endpoint": true, "api-key": true, "host-alias": true, "model": true}
	for i, line := range lines {
		indent, name, ok := commentedKey(line)
		if ok && indent == 0 && retired[name] {
			t.Errorf("line %d still offers the retired top-level %s: — %q", i+1, name, line)
		}
	}
	// Each surviving key is documented exactly once (CommentedExampleLine refuses a second one),
	// and the line it reports is where a first write of that key lands.
	at, err := CommentedExampleLine(lines, "server")
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

// The template documents `model-profiles:` (ADR 0044) and teaches its retired predecessor to
// nobody. The shipped shape table means a known family needs no entry at all, but the map is the
// only thing to reach for when a model's dialect is wrong or a built-in matched the wrong model —
// and a key the seeded file never names is a key its reader never finds. Five facts carry the
// surface: the map's own spelling, both axes, a shipped pattern (so the built-in table is visible
// at all), and that the global key it replaced is refused rather than ignored.
func TestEmbeddedDefaultConfigDocumentsModelProfiles(t *testing.T) {
	t.Parallel()
	template := string(defaultConfigYAML)
	for _, want := range []string{
		"# model-profiles:",          // the map itself, as a commented example to uncomment
		"tool-call-format",           // the tool-call axis
		"style: delimited",           // the thinking axis, with a value it actually takes
		"effort: medium",             // the effort axis, likewise
		"minimax-m3",                 // a shipped pattern: the built-in table is documented, not hidden
		"is retired",                 // and the global key it replaced is named as retired…
		"refused at startup",         // …loudly, so a stale config's reader knows what happened
		"replaces the WHOLE profile", // an entry is not merged over a built-in
	} {
		if !strings.Contains(template, want) {
			t.Errorf("embedded template does not mention %q; the model-profiles documentation is missing "+
				"or reworded", want)
		}
	}

	// The retired GLOBAL key is never OFFERED — it may only be named as history. A commented
	// top-level `# model-profile:` would be an example a reader uncomments into a config that is
	// then refused at startup, which is the opposite of documenting the retirement.
	for i, line := range SplitConfigLines(defaultConfigYAML) {
		if indent, name, ok := commentedKey(line); ok && indent == 0 && name == "model-profile" {
			t.Errorf("line %d offers the retired global model-profile: key — %q", i+1, line)
		}
	}
}

// SeedDefaultConfig honours an explicit --config home and seeds the embedded template there.
func TestSeedDefaultConfigHonoursConfigFlag(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	flagSet := func(name string) bool { return name == "config" }
	noEnv := func(string) string { return "" }

	created, path, err := SeedDefaultConfig(Options{ConfigDir: home}, flagSet, noEnv)
	if err != nil {
		t.Fatalf("SeedDefaultConfig: %v", err)
	}
	if !created {
		t.Fatal("created = false; want true on first run")
	}
	if filepath.Dir(path) != home {
		t.Errorf("seeded to %q; want a file under the --config home %q", path, home)
	}

	// A second run finds the file and does not recreate it.
	created2, _, err := SeedDefaultConfig(Options{ConfigDir: home}, flagSet, noEnv)
	if err != nil {
		t.Fatalf("SeedDefaultConfig (second run): %v", err)
	}
	if created2 {
		t.Error("created = true on the second run; the existing config should be left alone")
	}
}

// SeedDefaultConfig honours APOGEE_CONFIG when --config is not set.
func TestSeedDefaultConfigHonoursConfigEnv(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	getenv := func(k string) string {
		if k == EnvConfig {
			return home
		}
		return ""
	}
	created, path, err := SeedDefaultConfig(Options{}, func(string) bool { return false }, getenv)
	if err != nil {
		t.Fatalf("SeedDefaultConfig: %v", err)
	}
	if !created || filepath.Dir(path) != home {
		t.Errorf("created=%v path=%q; want a new file under the APOGEE_CONFIG home %q", created, path, home)
	}
}
