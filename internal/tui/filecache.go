package tui

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

// ----------------------------------------------------------------------------
// The @file autocomplete's workspace listing + its cache
// ----------------------------------------------------------------------------
//
// The autocomplete overlay re-derives on every keystroke (model.go handleKey), so an "@" token
// would otherwise re-walk the workspace filesystem once per character typed. The cache walks the
// tree once, holds the listing for a short TTL, and filters it in memory for each keystroke — so
// a typing burst reuses one walk instead of re-scanning the disk on every byte.

// fileCacheTTL is how long a workspace listing stays warm before the next "@" lookup re-walks.
// Short enough that a freshly created file shows up promptly; long enough that an interactive
// typing burst reuses one walk.
const fileCacheTTL = 3 * time.Second

// maxCachedFiles caps how many paths one listing holds, so a huge repo cannot make the cache
// unbounded. The overlay only ever shows maxAutocompleteItems of the filtered result, so a
// generous cap keeps substring matches available without listing an entire monorepo.
const maxCachedFiles = 4096

// fileCache memoises the workspace's file listing for the "@" autocomplete. It is held by
// pointer on the Model (a reference field, safe in the value-copied Model — ADR 0011), so every
// Update copy shares and mutates the one cache. Not safe for concurrent use, which is fine: the
// Bubble Tea Update loop is single-goroutine.
type fileCache struct {
	root    string    // the workspace the listing was walked from; a change invalidates it
	files   []string  // workspace-relative file paths, sorted, hidden/.git excluded
	expires time.Time // when the listing goes stale and the next lookup re-walks
}

// suggest returns up to limit workspace-relative paths matching partial (a case-insensitive
// substring), serving them from the cached listing and re-walking only when the cache is empty,
// stale (now past expires), or built from a different root. now is injected so a test can drive
// expiry deterministically. An empty root yields nothing.
func (c *fileCache) suggest(root, partial string, limit int, now time.Time) []string {
	if root == "" {
		return nil
	}
	if c.root != root || c.files == nil || now.After(c.expires) {
		c.root = root
		c.files = walkWorkspaceFiles(root, maxCachedFiles)
		c.expires = now.Add(fileCacheTTL)
	}
	return filterFiles(c.files, partial, limit)
}

// workspaceFiles returns up to limit workspace-relative file paths matching partial — the
// uncached path, used by tests and as the fallback when a Model is built without a cache. It
// walks the fenced workspace tree and filters in one shot.
func workspaceFiles(root, partial string, limit int) []string {
	if root == "" {
		return nil
	}
	return filterFiles(walkWorkspaceFiles(root, maxCachedFiles), partial, limit)
}

// walkWorkspaceFiles lists up to limit workspace-relative file paths via a bounded walk rooted
// at root through os.Root — so the walk cannot escape the workspace or follow a symlink out of
// it. It skips .git and other hidden directories/files, lists files only, sorts the result, and
// stops once limit paths are collected. An empty or unreadable root yields nothing.
func walkWorkspaceFiles(root string, limit int) []string {
	if root == "" {
		return nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer r.Close()

	var out []string
	_ = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil // skip unreadable entries / the root itself, keep walking
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") { // skip .git and other dotted dirs
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil // skip hidden files
		}
		out = append(out, p)
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// filterFiles returns up to limit paths from files whose lowercased form contains the lowercased
// partial (an empty partial matches all). The input is already sorted, so the result is too.
func filterFiles(files []string, partial string, limit int) []string {
	needle := strings.ToLower(partial)
	var out []string
	for _, p := range files {
		if needle == "" || strings.Contains(strings.ToLower(p), needle) {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
