package tui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The cache serves a workspace listing from one walk, holds it for the TTL (so a file created
// while the cache is warm is not yet seen), and re-walks once the TTL lapses.
func TestFileCacheServesThenExpires(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	mustWrite(t, filepath.Join(dir, "internal", "loop.go"), "package internal")

	c := &fileCache{}
	base := time.Unix(1000, 0)

	if got := c.suggest(dir, "loop", 8, base); !reflect.DeepEqual(got, []string{"internal/loop.go"}) {
		t.Fatalf("first suggest = %v, want [internal/loop.go]", got)
	}

	// A file created now is not surfaced while the cached listing is still warm.
	mustWrite(t, filepath.Join(dir, "fresh.go"), "package main")
	if got := c.suggest(dir, "fresh", 8, base.Add(time.Second)); len(got) != 0 {
		t.Errorf("warm cache surfaced a just-created file: %v", got)
	}
	// Past the TTL the next lookup re-walks and finds it.
	if got := c.suggest(dir, "fresh", 8, base.Add(fileCacheTTL+time.Second)); !reflect.DeepEqual(got, []string{"fresh.go"}) {
		t.Errorf("expired cache did not re-walk: %v, want [fresh.go]", got)
	}
}

// A change of workspace root invalidates the cache immediately, even within the TTL; an empty
// root yields nothing.
func TestFileCacheInvalidatesOnRootChange(t *testing.T) {
	a := t.TempDir()
	mustWrite(t, filepath.Join(a, "a.go"), "package a")
	b := t.TempDir()
	mustWrite(t, filepath.Join(b, "b.go"), "package b")

	c := &fileCache{}
	now := time.Unix(1000, 0)
	if got := c.suggest(a, "", 8, now); !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("root a = %v, want [a.go]", got)
	}
	if got := c.suggest(b, "", 8, now); !reflect.DeepEqual(got, []string{"b.go"}) {
		t.Errorf("root change did not invalidate within the TTL: %v, want [b.go]", got)
	}
	if got := c.suggest("", "", 8, now); got != nil {
		t.Errorf("empty root = %v, want nil", got)
	}
}
