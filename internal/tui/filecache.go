package tui

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The @file autocomplete's workspace listing and its cache.
//
// The autocomplete overlay re-derives on every keystroke (model.go handleKey), so an "@" token
// would otherwise re-walk the workspace filesystem once per character typed. The cache walks the
// tree once, holds the listing for a short TTL, and filters it in memory for each keystroke — so
// a typing burst reuses one walk instead of re-scanning the disk on every byte.
//
// The walk never runs on the Update goroutine (ADR 0011's division: work that touches the disk runs
// on a worker and lands as a message). A keystroke filters the listing the cache holds NOW — stale,
// or empty on first use, in which case no menu opens — and, when that listing is due a refresh,
// hands back a Cmd that walks off the loop (refreshCmd). The walk lands as a filesListedMsg, which
// Update installs (install) and, while the "@" region is still open, re-derives the menu over
// (foldFilesListed, autocomplete.go). Stale-then-refresh, with no visual state of its own.

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
// Update copy shares and mutates the one cache. Not safe for concurrent use, and it need not be:
// only the Update goroutine reads or writes it. The walk itself runs on a Cmd goroutine, but it
// touches none of these fields — it captures the root and the walker by value and hands its result
// back as a filesListedMsg for Update to install.
//
// Every method is nil-safe, since tests build bare Model literals that never went through
// newPromptEditor: a nil cache holds nothing and never asks for a walk.
type fileCache struct {
	root    string    // the workspace the listing was walked from; a change invalidates it
	files   []string  // workspace-relative file paths, sorted, hidden/.git excluded
	expires time.Time // when the listing goes stale and the next lookup asks for a re-walk

	// walking is the root a walk is in flight for ("" when none), so a burst of keystrokes while one
	// walk runs dispatches no second one. install clears it when that walk lands.
	walking string

	// walk is the walker refreshCmd dispatches: nil means walkWorkspaceFiles. It is the test seam a
	// counting walker plugs into, read on the Update goroutine when the Cmd is built.
	walk func(root string, limit int) []string
}

// filesListedMsg reports a finished workspace walk: the listing of root, for Update to install in
// the file cache (foldFilesListed). It carries the result, not just a signal, because the walk ran
// on a Cmd goroutine that may not touch the cache.
type filesListedMsg struct {
	root  string
	files []string
}

// Compile-time assertion that the listing Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = filesListedMsg{}

// suggest returns up to limit workspace-relative paths matching partial (a case-insensitive
// substring), filtered from the listing the cache holds now. It never walks: a cache that has never
// been filled, or was filled from a different root, yields nothing (holds' rule), and an expired
// listing is still served — refreshCmd is what asks for the fresh one. An empty root yields nothing.
func (c *fileCache) suggest(root, partial string, limit int) []string {
	if c == nil || root == "" || c.root != root {
		return nil
	}
	return filterFiles(c.files, partial, limit)
}

// refreshCmd returns the Cmd that re-walks root off the Update goroutine when the held listing is
// due one — never filled, filled from another root, or past its TTL at now — and marks the walk in
// flight, so the keystrokes that follow while it runs dispatch nothing. It returns nil when no walk
// is due, one is already in flight, root is empty, or the cache is nil. now is injected so a test
// can drive expiry deterministically.
func (c *fileCache) refreshCmd(root string, now time.Time) tea.Cmd {
	if c == nil || root == "" || c.walking != "" {
		return nil
	}
	if c.root == root && c.files != nil && !now.After(c.expires) {
		return nil
	}
	walk := c.walk
	if walk == nil {
		walk = walkWorkspaceFiles
	}
	c.walking = root
	return func() tea.Msg {
		return filesListedMsg{root: root, files: walk(root, maxCachedFiles)}
	}
}

// install lands a finished walk: it clears the in-flight mark the walk was dispatched under and, when
// the listing is of workspace — the root this Model serves — holds it until now plus the TTL. A
// listing of any other root installs nothing. It reports whether the listing was installed.
func (c *fileCache) install(msg filesListedMsg, workspace string, now time.Time) bool {
	if c == nil {
		return false
	}
	if c.walking == msg.root {
		c.walking = ""
	}
	if msg.root == "" || msg.root != workspace {
		return false
	}
	files := msg.files
	if files == nil {
		files = []string{} // an empty workspace is a held listing, not an unfilled cache
	}
	c.root = msg.root
	c.files = files
	c.expires = now.Add(fileCacheTTL)
	return true
}

// holds reports whether path is in the listing the cache is CURRENTLY holding — a pure lookup that
// never walks, which is what makes it safe to call from the render path (inputaccent.go paints an
// "@ref" only when its path resolves, once per frame per token). Everything else about it follows
// from that one rule:
//
//   - a cache that has never been filled, or was filled from another workspace root, knows nothing
//     and answers false; the accent simply stays off until the walk the "@" overlay's next lookup
//     dispatches lands;
//   - an EXPIRED listing still answers, deliberately: the TTL exists to keep refreshCmd's walk fresh,
//     and honouring it here would blink a resolved token's accent off three seconds after the human
//     stopped typing, with nothing in the draft having changed. A stale listing is the best answer
//     available without touching the disk, and the next walk heals it.
//
// The listing is sorted (walkWorkspaceFiles), so the lookup is a binary search rather than a scan
// of up to maxCachedFiles paths per token per frame.
func (c *fileCache) holds(root, path string) bool {
	if c == nil || root == "" || path == "" || c.root != root {
		return false
	}
	i := sort.SearchStrings(c.files, path)
	return i < len(c.files) && c.files[i] == path
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
	defer func() { _ = r.Close() }()

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
