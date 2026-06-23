package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ----------------------------------------------------------------------------
// First-run config seeding (the embedded starter config)
// ----------------------------------------------------------------------------

// defaultConfigYAML is the starter config compiled into the binary from
// defaults/config.yaml. //go:embed re-reads that file on every build, so the seeded
// template can never drift from the binary that ships it. It is a fully-commented
// template: parsed, it sets nothing (an empty layer), so seeding it on first run never
// changes how a run resolves — it only drops a documented file the user can edit.
//
//go:embed defaults/config.yaml
var defaultConfigYAML []byte

// seedDefaultConfig writes the embedded starter config to <home>/config.yaml on first run
// — when no config file exists there yet — creating the home directory. It honours
// --config / APOGEE_CONFIG (resolveConfigDir) so the template lands in the same home
// applyConfig later reads. It returns whether it created the file and the path, so the
// caller can show a one-time notice. An existing config is never touched.
func seedDefaultConfig(opts options, changed func(string) bool, getenv func(string) string) (bool, string, error) {
	home, err := apogeeHome(resolveConfigDir(opts.configDir, changed, getenv))
	if err != nil {
		return false, "", err
	}
	path := filepath.Join(home, "config.yaml")
	created, err := seedConfig(path, defaultConfigYAML)
	return created, path, err
}

// seedConfig writes content to path if no file exists there yet, creating the parent
// directory with owner-only permissions. It reports whether it wrote a new file; an
// existing file is left untouched (the user's edits always win over the template).
func seedConfig(path string, content []byte) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("apogee: stat config %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("apogee: create config directory: %w", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return false, fmt.Errorf("apogee: write default config %q: %w", path, err)
	}
	return true, nil
}
