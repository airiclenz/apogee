// Package docmap is the test-only guard behind the house rule that a package past ~10 files
// carries a doc.go file map naming every non-test file in it (coding-standards; ADR 0043).
//
// A map is only worth the reading if it is COMPLETE, and completeness is exactly what review
// stops enforcing after the third file lands in a hurry. [Check] makes it a build gate instead:
// one line in a package's own test suite fails the moment a file exists that the narration never
// mentions, naming the file and asking for the half-line that describes it. The pattern is
// TestFoldEventCoversEveryEventVariant's — parse the truth out of the source, compare it against
// what the code claims, and make the omission loud — one level up, over files rather than types.
//
// The check is deliberately a NAME check and nothing more. Where in doc.go a file is named, and
// whether the sentence around it is any good, stays a human judgement; a guard that tried to grade
// prose would be gamed by a list of bare file names, which is the one shape of map nobody reads.
package docmap

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docFile is the file every package's map lives in.
const docFile = "doc.go"

// Check fails t when the calling package's doc.go does not name every non-test .go file beside
// it. It reads the working directory, which `go test` sets to the package's own directory, so a
// package enlists by adding one docmap_test.go calling this and nothing else.
//
// A package with no doc.go at all fails: the rule is that the map exists, not that an existing
// one is complete.
func Check(t *testing.T) {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locate the package directory: %v", err)
	}
	missing, err := unmapped(dir)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, name := range missing {
		t.Errorf("%s is not named in %s — add the one line saying what it holds to the package's file map",
			name, filepath.Join(filepath.Base(dir), docFile))
	}
}

// unmapped returns the non-test .go files in dir that dir's doc.go does not name, in directory
// order. A missing or unreadable doc.go is an error rather than an empty result, so an absent map
// can never read as a clean one.
func unmapped(dir string) ([]string, error) {
	doc, err := os.ReadFile(filepath.Join(dir, docFile))
	if err != nil {
		return nil, fmt.Errorf("read the package file map: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the package directory: %w", err)
	}

	var missing []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !names(doc, name) {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// names reports whether doc names the file base — as a name of its own, not as the tail of a
// longer one. Without the boundary rule a map that describes configwatch.go would silently vouch
// for a watch.go nobody ever wrote a line about.
func names(doc []byte, base string) bool {
	for i := 0; i+len(base) <= len(doc); {
		at := bytes.Index(doc[i:], []byte(base))
		if at < 0 {
			return false
		}
		at += i
		if at == 0 || !nameByte(doc[at-1]) {
			return true
		}
		i = at + 1
	}
	return false
}

// nameByte reports whether b can be part of the file name or path that a match is the tail of.
func nameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	default:
		return b == '_' || b == '-' || b == '.' || b == '/'
	}
}
