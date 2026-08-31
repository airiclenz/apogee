package config

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
// template can never drift from the binary that ships it. Every key it states is commented
// out (ADR 0064 §4), so parsed, it resolves nothing: seeding it on first run changes nothing
// about how a run resolves, and everything it drops is documentation the user can edit. The
// default system prompt used to be its one active key; it is defaultSystemPrompt below now,
// because a key seeded once is frozen per install and can never be improved again.
//
//go:embed defaults/config.yaml
var defaultConfigYAML []byte

// defaultSystemPrompt is the base steering apogee runs on when no prompt is configured
// (ADR 0064 §1): the standing instructions ResolveSystemPrompt falls back to on the third
// rung of the ladder, unless `use-default-prompt: false` says to send none. It is EMBEDDED
// rather than seeded, for the reason the built-in color schemes are (ADR 0040 §1): every
// upgrade ships the current text to every user, including one whose config.yaml was written
// a year ago. It is never installed anywhere — there is no export command for it, because a
// second copy on disk would re-freeze exactly what embedding unfroze; the settings editor
// hands the bytes to whoever wants to edit them.
//
//go:embed defaults/prompt.txt
var defaultSystemPrompt string

// DefaultSystemPrompt is the embedded default system-prompt template — the text
// [ResolveSystemPrompt] falls back to, and the text the settings editor pre-fills the
// `system-prompt-text` field with when nothing is configured. It is a TEMPLATE like any
// configured prompt: the placeholders are substituted per request, and it passes the same
// prompt.Validate the configured ones face.
func DefaultSystemPrompt() string { return defaultSystemPrompt }

// SeedDefaultConfig writes the embedded starter config to <home>/config.yaml on first run
// — when no config file exists there yet — creating the home directory. It honours
// --config / APOGEE_CONFIG (resolveConfigDir) so the template lands in the same home
// ApplyConfig later reads. It returns whether it created the file and the path, so the
// caller can show a one-time notice. An existing config is never touched.
func SeedDefaultConfig(opts Options, changed func(string) bool, getenv func(string) string) (bool, string, error) {
	home, err := ApogeeHome(resolveConfigDir(opts.ConfigDir, changed, getenv))
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
