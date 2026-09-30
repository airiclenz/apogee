// Package scope holds the `workspace:` filter both reaction lanes share: the one resolution a
// filter and a root's own workspace are compared as, and the reduction of a list to the entries
// active at one root. It is a leaf over internal/domain and internal/security, so the observe lane
// (internal/reactions) and the sync lane (internal/agent) can both import it without either
// importing the other.
package scope

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// Resolve reduces a workspace path to the one spelling the `workspace:` filter and a
// root's own workspace are compared as: a leading `~` expanded, the path made absolute, and every
// symlink evaluated. Both sides of the comparison go through it, which is the whole point —
// `/tmp/w` and `/private/tmp/w` are one workspace on macOS, and a filter that compared the two
// literally would be silently inactive on exactly the platform where the temp root is a link.
//
// An EMPTY path is the unset filter: it resolves to "" with no error, so a Reaction that scopes
// itself to nothing is active everywhere without the caller special-casing it first.
//
// The `~` rule is config.ExpandUserPath's, reproduced here because this package depends on
// internal/domain and internal/security alone; a `~` that is not leading is a legal filename
// character and is left exactly as written.
func Resolve(path string) (string, error) {
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

// ActiveAt reduces list to the Reactions active at one workspace root: an entry whose `workspace:`
// filter is unset is active everywhere, and a scoped entry only where its filter resolves to root
// exactly. root is taken AS RESOLVED (Resolve), so every entry is compared against one spelling of
// it. Both lanes reduce through here — the reactions Runner its observe lane, the Agent its sync
// lane (SetReactions) — so a scoped `gate:` or `advise:` is inactive outside its
// workspace exactly as a scoped `run:` is. Order is preserved, and when nothing is scoped out the
// list comes back as it stands.
//
// It fails, naming the entry, when a filter cannot be resolved.
func ActiveAt(list []domain.Reaction, root string) ([]domain.Reaction, error) {
	active := make([]domain.Reaction, 0, len(list))
	for _, entry := range list {
		filter, err := Resolve(entry.Workspace)
		if err != nil {
			return nil, fmt.Errorf("reaction %q: %w", entry.ID, err)
		}
		if filter != "" && filter != root {
			continue
		}
		active = append(active, entry)
	}
	if len(active) == len(list) {
		return list, nil
	}
	return active, nil
}
