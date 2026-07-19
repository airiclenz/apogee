package validated

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadUserDir reads the user-local entries under dir (~/.apogee/validated), one entry
// per *.json file — the drop-in write model for future user-run validation tooling: a
// writer creates one file, never read-modify-writes a shared store. A missing dir is
// the normal no-entries case. Every per-file defect is SOFT (ADR 0016 realisation:
// auto-enable is a convenience layer above a safe floor, and the defect is in data the
// user did not necessarily author) — the file is skipped and a warning line returned
// for the caller to print; only the dir being unreadable is reported the same way,
// never as an error. Files are read in sorted name order so downstream duplicate
// resolution is deterministic.
func LoadUserDir(dir string) (entries []Entry, warnings []string) {
	items, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("skipping validated-set dir %s: %v", dir, err)}
	}

	names := make([]string, 0, len(items))
	for _, it := range items {
		if it.IsDir() || !strings.HasSuffix(it.Name(), ".json") {
			continue
		}
		names = append(names, it.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping validated-set entry %s: %v", path, err))
			continue
		}
		e, err := decodeEntry(data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping validated-set entry %s: %v", path, err))
			continue
		}
		e.Source = SourceUser
		entries = append(entries, e)
	}
	return entries, warnings
}

// Merge folds the two sources into the lookup map Match consumes. User-local wins a key
// collision with shipped silently — that precedence is the design (the user's evidence,
// their exact quant; the notice names the winning source). A duplicate WITHIN the
// user-local dir is a data smell, so the later file (sorted order) wins with a warning
// naming the shadowed one.
func Merge(shipped, user []Entry) (map[string]Entry, []string) {
	merged := make(map[string]Entry, len(shipped)+len(user))
	var warnings []string
	for _, e := range shipped {
		merged[e.Key] = e
	}
	for _, e := range user {
		if prev, ok := merged[e.Key]; ok && prev.Source == SourceUser {
			warnings = append(warnings, fmt.Sprintf(
				"duplicate validated-set entries for %q in the user dir; using the later file", e.Key))
		}
		merged[e.Key] = e
	}
	return merged, warnings
}
