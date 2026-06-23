package main

import (
	"os"
	"path/filepath"
	"testing"
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

// The embedded starter config is valid YAML and behaviour-neutral: parsed, it sets nothing
// (a fully-commented template), so seeding it on first run never changes how a run resolves.
func TestEmbeddedDefaultConfigIsNeutral(t *testing.T) {
	t.Parallel()
	if len(defaultConfigYAML) == 0 {
		t.Fatal("defaultConfigYAML is empty; the embed did not pick up defaults/config.yaml")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, defaultConfigYAML, 0o600); err != nil {
		t.Fatalf("write embedded: %v", err)
	}
	l, err := loadFileConfig(path, os.ReadFile)
	if err != nil {
		t.Fatalf("embedded default config does not parse: %v", err)
	}
	if l != (layer{}) {
		t.Errorf("embedded default config sets values (not behaviour-neutral): %+v", l)
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
