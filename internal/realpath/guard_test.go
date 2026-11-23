package realpath

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retiredDelegate is the name of the delegate internal/platform once kept over Resolve. It is
// assembled from two halves so this file never spells it either.
const retiredDelegate = "platform." + "RealPath"

// TestNoProductionFileNamesPlatformRealPath holds the delegate's retirement: no non-test .go file
// under internal/ or cmd/ names it, in code or in a comment, so a caller cannot grow back on it
// and no comment points a reader at a function that is gone. The files are read as text, so a
// build-tagged file is checked from any host.
func TestNoProductionFileNamesPlatformRealPath(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..")
	scanned := 0
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for number, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, retiredDelegate) {
					t.Errorf("%s:%d names %s; call realpath.Resolve", path, number+1, retiredDelegate)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no .go files were scanned; the guard proved nothing")
	}
}
