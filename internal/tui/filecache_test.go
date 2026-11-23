package tui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// walkAndInstall drives one refresh of c by hand: it takes the walk refreshCmd dispatches for root at
// now, runs it as the Cmd goroutine would, and installs the listing it reports as Update would.
func walkAndInstall(t *testing.T, c *fileCache, root string, now time.Time) {
	t.Helper()
	cmd := c.refreshCmd(root, now)
	if cmd == nil {
		t.Fatalf("refreshCmd(%q) = nil; the listing was due a walk", root)
	}
	msg, ok := cmd().(filesListedMsg)
	if !ok {
		t.Fatalf("the walk reported %T, want filesListedMsg", msg)
	}
	if !c.install(msg, root, now) {
		t.Fatalf("install refused the listing of %q", root)
	}
}

// warmFiles warms m's file cache the way the program does — the walk an "@" lookup dispatches, run
// off the loop and folded back through Update — for a test that reaches the "@" region over a real
// workspace. step() drops Cmds, so an "@" keystroke alone never lands a listing.
func warmFiles(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.files.refreshCmd(m.opts.Workspace, time.Now())
	if cmd == nil {
		t.Fatal("warmFiles: no walk was due; the cache is already warm or a walk is in flight")
	}
	return runCmd(t, m, cmd)
}

// countingWalker plugs a counting walker into m's file cache: every walk is counted, then served by
// the real walker.
func countingWalker(m Model) *int {
	walks := new(int)
	m.files.walk = func(root string, limit int) []string {
		*walks++
		return walkWorkspaceFiles(root, limit)
	}
	return walks
}

// fileWalkModel is a ready model over a temp workspace holding main.go and docs/plan.md, with a
// counting walker in its file cache.
func fileWalkModel(t *testing.T) (Model, *int) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	mustWrite(t, filepath.Join(dir, "docs", "plan.md"), "# plan")
	opts := testOpts
	opts.Workspace = dir
	m := newTestModelEng(t, &fakeEngine{}, opts)
	return m, countingWalker(m)
}

// The cache serves a workspace listing from one walk, holds it for the TTL (so a file created
// while the cache is warm is not yet seen, and no walk is due), and asks for a re-walk once the TTL
// lapses — serving the stale listing until that walk lands.
func TestFileCacheServesThenExpires(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	mustWrite(t, filepath.Join(dir, "internal", "loop.go"), "package internal")

	c := &fileCache{}
	base := time.Unix(1000, 0)

	if got := c.suggest(dir, "loop", 8); got != nil {
		t.Fatalf("a never-filled cache suggested %v; it must not walk", got)
	}
	walkAndInstall(t, c, dir, base)
	if got := c.suggest(dir, "loop", 8); !reflect.DeepEqual(got, []string{"internal/loop.go"}) {
		t.Fatalf("first suggest = %v, want [internal/loop.go]", got)
	}

	// A file created now is not surfaced while the cached listing is still warm.
	mustWrite(t, filepath.Join(dir, "fresh.go"), "package main")
	if cmd := c.refreshCmd(dir, base.Add(time.Second)); cmd != nil {
		t.Error("a warm listing asked for a re-walk")
	}
	if got := c.suggest(dir, "fresh", 8); len(got) != 0 {
		t.Errorf("warm cache surfaced a just-created file: %v", got)
	}
	// Past the TTL the stale listing is still served, and the re-walk that lands finds the file.
	later := base.Add(fileCacheTTL + time.Second)
	if got := c.suggest(dir, "fresh", 8); len(got) != 0 {
		t.Errorf("an expired listing walked inline: %v", got)
	}
	walkAndInstall(t, c, dir, later)
	if got := c.suggest(dir, "fresh", 8); !reflect.DeepEqual(got, []string{"fresh.go"}) {
		t.Errorf("expired cache did not re-walk: %v, want [fresh.go]", got)
	}
}

// A change of workspace root invalidates the cache immediately, even within the TTL: a held listing
// of another root filters as empty and is due a walk of the new one. An empty root yields nothing
// and walks nothing.
func TestFileCacheInvalidatesOnRootChange(t *testing.T) {
	t.Parallel()

	a := t.TempDir()
	mustWrite(t, filepath.Join(a, "a.go"), "package a")
	b := t.TempDir()
	mustWrite(t, filepath.Join(b, "b.go"), "package b")

	c := &fileCache{}
	now := time.Unix(1000, 0)
	walkAndInstall(t, c, a, now)
	if got := c.suggest(a, "", 8); !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("root a = %v, want [a.go]", got)
	}
	if got := c.suggest(b, "", 8); got != nil {
		t.Errorf("a listing of root a served root b within the TTL: %v, want nothing", got)
	}
	walkAndInstall(t, c, b, now)
	if got := c.suggest(b, "", 8); !reflect.DeepEqual(got, []string{"b.go"}) {
		t.Errorf("root change did not invalidate within the TTL: %v, want [b.go]", got)
	}
	if got := c.suggest("", "", 8); got != nil {
		t.Errorf("empty root = %v, want nil", got)
	}
	if cmd := (&fileCache{}).refreshCmd("", now); cmd != nil {
		t.Error("an empty root asked for a walk")
	}
}

// A nil cache — a bare Model literal's — holds nothing, asks for no walk and installs nothing.
func TestFileCacheNilSafe(t *testing.T) {
	t.Parallel()
	var c *fileCache
	if got := c.suggest("/ws", "", 8); got != nil {
		t.Errorf("nil suggest = %v", got)
	}
	if cmd := c.refreshCmd("/ws", time.Now()); cmd != nil {
		t.Error("nil refreshCmd returned a walk")
	}
	if c.install(filesListedMsg{root: "/ws", files: []string{"a.go"}}, "/ws", time.Now()) {
		t.Error("nil install reported an install")
	}
	if c.holds("/ws", "a.go") {
		t.Error("nil holds answered true")
	}
}

// An "@" keystroke never walks the disk on the Update goroutine: it hands back a Cmd, and only
// running that Cmd walks — once — reporting the listing as a filesListedMsg that opens the menu.
func TestFileWalkNeverRunsInUpdate(t *testing.T) {
	t.Parallel()
	m, walks := fileWalkModel(t)

	m, cmd := stepCmd(t, m, keyRune('@'))
	if *walks != 0 {
		t.Fatalf("the @ keystroke walked inline (%d walks); the walk belongs on the Cmd goroutine", *walks)
	}
	if cmd == nil {
		t.Fatal("the @ keystroke returned no Cmd; the listing must be walked off the loop")
	}
	if m.autocomplete.active {
		t.Fatalf("a cold cache opened a menu before its walk landed: %+v", m.autocomplete)
	}

	msg, ok := cmd().(filesListedMsg)
	if !ok {
		t.Fatalf("the @ keystroke's Cmd reported %T, want filesListedMsg", msg)
	}
	if *walks != 1 {
		t.Fatalf("running the Cmd walked %d times, want 1", *walks)
	}
	m = step(t, m, msg)
	if !m.autocomplete.active || m.autocomplete.kind != acFile || len(m.autocomplete.items) != 2 {
		t.Fatalf("the landed listing did not open the file menu: %+v", m.autocomplete)
	}
}

// A burst of keys typed while a walk is in flight starts that one walk and no other, and once it has
// landed the menu filters it by everything typed meanwhile.
func TestFileWalkBurstStartsOneWalk(t *testing.T) {
	t.Parallel()
	m, walks := fileWalkModel(t)

	m, first := stepCmd(t, m, keyRune('@'))
	if first == nil {
		t.Fatal("the @ keystroke returned no Cmd")
	}
	for _, r := range "mai" {
		var cmd tea.Cmd
		m, cmd = stepCmd(t, m, keyRune(r))
		if cmd != nil {
			t.Fatalf("key %q dispatched a second Cmd while the first walk was in flight", r)
		}
	}
	m = runCmd(t, m, first)
	if *walks != 1 {
		t.Fatalf("the burst walked %d times, want 1", *walks)
	}
	if !m.autocomplete.active || len(m.autocomplete.items) != 1 || m.autocomplete.items[0].value != "main.go" {
		t.Fatalf("menu after the walk landed = %+v, want the one main.go row", m.autocomplete)
	}
	if _, cmd := stepCmd(t, m, keyRune('n')); cmd != nil {
		t.Error("a key over a fresh listing dispatched a walk")
	}
}

// A landing walk repaints only a live "@" region: the first walk opens the menu, a menu esc dismissed
// while the walk ran stays closed when it lands, and a listing of another root installs nothing.
func TestFileWalkLandingOpensOnlyALiveFileMenu(t *testing.T) {
	t.Parallel()

	t.Run("the first walk landing opens the menu", func(t *testing.T) {
		t.Parallel()
		m, _ := fileWalkModel(t)
		m, cmd := stepCmd(t, m, keyRune('@'))
		m = runCmd(t, m, cmd)
		if !m.autocomplete.active || m.autocomplete.kind != acFile {
			t.Fatalf("the first walk landing left the menu closed: %+v", m.autocomplete)
		}
	})

	t.Run("an esc-dismissed menu stays closed", func(t *testing.T) {
		t.Parallel()
		m, _ := fileWalkModel(t)
		m = warmFiles(t, m)
		m.files.expires = time.Time{} // stale: the next lookup serves it and asks for a re-walk
		m, cmd := stepCmd(t, m, keyRune('@'))
		if !m.autocomplete.active || cmd == nil {
			t.Fatalf("a stale listing: menu %+v, Cmd %v; want the stale rows and a re-walk", m.autocomplete, cmd != nil)
		}
		m = step(t, m, keyEsc())
		if m.autocomplete.active {
			t.Fatal("esc did not dismiss the file menu")
		}
		m = runCmd(t, m, cmd)
		if m.autocomplete.active {
			t.Fatalf("the walk landing re-opened a menu esc dismissed: %+v", m.autocomplete)
		}
		if !m.files.holds(m.opts.Workspace, "main.go") {
			t.Error("the landed listing was not installed")
		}
	})

	t.Run("a listing of another root installs nothing", func(t *testing.T) {
		t.Parallel()
		m, _ := fileWalkModel(t)
		m = step(t, m, keyRune('@'))
		other := t.TempDir()
		m = step(t, m, filesListedMsg{root: other, files: []string{"x.go"}})
		if m.autocomplete.active {
			t.Fatalf("a foreign listing opened the menu: %+v", m.autocomplete)
		}
		if m.files.holds(other, "x.go") || m.files.suggest(m.opts.Workspace, "", 8) != nil {
			t.Error("a listing of another root was installed")
		}
	})
}
