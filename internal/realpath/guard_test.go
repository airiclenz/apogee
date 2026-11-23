package realpath

import (
	"go/ast"
	"go/parser"
	"go/token"
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

// The files outside this package that may still name filepath.EvalSymlinks, as slash paths from
// the repo root. Each is exempt for the reason its comment gives; nothing else is.
const (
	// exemptConfigSplice answers which file a config write replaces without replacing a link,
	// not where a path really lives, and keeps its own evalSymlinks seam for that.
	exemptConfigSplice = "internal/config/configsplice.go"
	// exemptUpdater: the updater must see the install manager's own spelling (Scoop's `current`
	// junction).
	exemptUpdater = "cmd/apogee/wire_update.go"
)

// leafDir is this package's directory as a slash path from the repo root: the one place the raw
// call belongs.
const leafDir = "internal/realpath"

// TestNoRawEvalSymlinksOutsideTheLeaf holds the one-resolver rule: no non-test .go file outside
// this package calls or references filepath.EvalSymlinks, bar the two named exemptions. It walks
// the module root package's own files plus internal/ and cmd/, skipping every directory whose
// name starts with a dot (a checkout under .claude/worktrees is another tree's business). Each
// file is parsed with go/parser rather than searched as text, so a comment naming the function
// passes and a build-tagged file is checked from any host.
func TestNoRawEvalSymlinksOutsideTheLeaf(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..")
	exempt := map[string]bool{exemptConfigSplice: true, exemptUpdater: true}
	files := productionGoFiles(t, repoRoot)

	parsed := 0
	fset := token.NewFileSet()
	for _, relative := range files {
		if exempt[relative] || strings.HasPrefix(relative, leafDir+"/") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(repoRoot, filepath.FromSlash(relative)), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		parsed++
		for _, pos := range evalSymlinksReferences(file) {
			t.Errorf("%s:%d references filepath.EvalSymlinks; resolve through realpath.Resolve or realpath.ResolveLenient",
				relative, fset.Position(pos).Line)
		}
	}
	// A guard that parsed no files would pass over a tree it never looked at.
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the guard proved nothing")
	}
}

// productionGoFiles lists every non-test .go file the guard covers, as slash paths from repoRoot:
// the root package's own files, and everything under internal/ and cmd/ outside dot-directories.
func productionGoFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	var files []string
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatalf("read the repo root: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && isProductionGoFile(entry.Name()) {
			files = append(files, entry.Name())
		}
	}
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isProductionGoFile(entry.Name()) {
				return nil
			}
			relative, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	return files
}

// isProductionGoFile reports whether name is a .go file that is not a test file.
func isProductionGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// evalSymlinksReferences returns the position of every selector in file that names EvalSymlinks
// on the path/filepath import, under whatever name the file imports that package: a call and a
// bare reference (`var f = filepath.EvalSymlinks`) alike. A file that dot-imports path/filepath
// is reported at the import, since a bare EvalSymlinks there cannot be told from a local name.
func evalSymlinksReferences(file *ast.File) []token.Pos {
	var found []token.Pos
	localName := ""
	for _, imported := range file.Imports {
		if strings.Trim(imported.Path.Value, `"`) != "path/filepath" {
			continue
		}
		switch {
		case imported.Name == nil:
			localName = "filepath"
		case imported.Name.Name == ".":
			found = append(found, imported.Pos())
		case imported.Name.Name != "_":
			localName = imported.Name.Name
		}
	}
	if localName == "" {
		return found
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == localName && selector.Sel.Name == "EvalSymlinks" {
			found = append(found, selector.Pos())
		}
		return true
	})
	return found
}
