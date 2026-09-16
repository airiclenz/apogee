package subprocess

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
)

// scratchEnvEntry is one variable a confined toolchain is pointed into the scratch dir with: the
// key and the subdirectory of box.ScratchDir it names.
type scratchEnvEntry struct {
	key    string
	subdir string
}

// scratchEnvEntries are the temp and cache variables a confined run seeds, in the order they are
// emitted. The four temp spellings cover the POSIX and Windows conventions plus Go's own
// (`GOTMPDIR`); `GOCACHE` is the Go build cache; `XDG_CACHE_HOME` is what gh, pip and friends root
// their caches under. Each one lands beneath the session scratch dir, which is already inside the
// box's writable set, so a toolchain that would otherwise have reached for `/tmp` or
// `~/.cache` — both denied under workspace confinement — writes somewhere the fence allows
// (docs/design/confinement-execution-contract.md §7).
var scratchEnvEntries = []scratchEnvEntry{
	{key: "TMPDIR", subdir: "tmp"},
	{key: "TMP", subdir: "tmp"},
	{key: "TEMP", subdir: "tmp"},
	{key: "GOTMPDIR", subdir: "tmp"},
	{key: "GOCACHE", subdir: "go-build"},
	{key: "XDG_CACHE_HOME", subdir: "cache"},
}

// scratchDirMode is the mode the seeded subdirectories are created with: private to the user,
// the same as the session scratch dir they live under.
const scratchDirMode = 0o700

// ScratchEnvKeys returns the variable names ScratchEnv seeds, in emission order — the one list
// every environment allowlist that must let the seeded values through (the Go toolchain's, the
// host's GOROOT probe) reads, so a key added here is passed through everywhere.
func ScratchEnvKeys() []string {
	keys := make([]string, 0, len(scratchEnvEntries))
	for _, entry := range scratchEnvEntries {
		keys = append(keys, entry.key)
	}
	return keys
}

// ScratchEnv returns the "KEY=value" entries that point a confined toolchain's temp and cache
// dirs beneath box.ScratchDir — `<scratch>/tmp`, `<scratch>/go-build` and `<scratch>/cache` —
// creating each directory first, so the child never finds its TMPDIR missing. It is the ONE
// place the seed is built: ConfinementHandoff's prepare hook — what the subprocess funnel (run)
// and console_open both spawn under — calls it, and only on a confined run, so an unconfined
// run's environment stays byte-identical to the host's. A box with no ScratchDir yields nil and
// creates nothing.
//
// The entries are meant to be APPENDED to the child's environment: os/exec resolves a duplicate
// key last-wins, so the seed overrides whatever the host or the tool's own allowlist carried for
// the same key.
func ScratchEnv(box domain.ConfinementBox) ([]string, error) {
	if box.ScratchDir == "" {
		return nil, nil
	}
	env := make([]string, 0, len(scratchEnvEntries))
	for _, entry := range scratchEnvEntries {
		dir := filepath.Join(box.ScratchDir, entry.subdir)
		if err := os.MkdirAll(dir, scratchDirMode); err != nil {
			return nil, fmt.Errorf("create scratch %s dir: %w", entry.subdir, err)
		}
		env = append(env, entry.key+"="+dir)
	}
	return env, nil
}
