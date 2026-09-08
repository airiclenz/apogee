package reactions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/security"
)

// ResolveWorkspace reduces a workspace path to the one spelling the `workspace:` filter and a
// root's own workspace are compared as: a leading `~` expanded, the path made absolute, and every
// symlink evaluated. Both sides of the comparison go through it, which is the whole point —
// `/tmp/w` and `/private/tmp/w` are one workspace on macOS, and a filter that compared the two
// literally would be silently inactive on exactly the platform where the temp root is a link.
//
// An EMPTY path is the unset filter: it resolves to "" with no error, so a Hook that scopes itself
// to nothing is active everywhere without the caller special-casing it first.
//
// The `~` rule is config.ExpandUserPath's, reproduced here because this package depends on
// internal/domain and internal/security alone; a `~` that is not leading is a legal filename
// character and is left exactly as written.
func ResolveWorkspace(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", path, err)
	}
	return security.EvalRealPath(absolute), nil
}

// expandHome resolves a leading `~` (alone, or as `~/…`) against the user's home directory.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("workspace %q: this host has no home directory to expand the leading ~ "+
			"against; write the path in full: %w", path, err)
	}
	if home == "" {
		return "", errors.New("workspace " + path + ": this host has no home directory to expand the " +
			"leading ~ against; write the path in full")
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[len("~/"):]), nil
}
