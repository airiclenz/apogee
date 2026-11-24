package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// projectrootImport is the import path of the Project root walk, the one package a config source
// ([Files]) resolves its root through.
const projectrootImport = "github.com/airiclenz/apogee/internal/projectroot"

// projectrootReaders are the directories, as slash paths from the repo root, whose production files
// may import the walk: this package, which owns the one config source, and the walk itself.
var projectrootReaders = []string{"internal/config/", "internal/projectroot/"}

// TestOnlyConfigImportsProjectroot holds the one-source rule: no production .go file outside
// internal/config and internal/projectroot imports internal/projectroot, so a Driver learns its
// Project root from the config source ([Files.ProjectRoot]) rather than walking to one of its own
// that a later re-read could disagree with. Each file's imports are parsed (parser.ImportsOnly) and
// only the import paths are judged, so a comment naming the package passes, and a build-tagged file
// is checked from any host. It walks the module root package's own files plus internal/ and cmd/,
// skipping every directory whose name starts with a dot.
func TestOnlyConfigImportsProjectroot(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..")
	parsed := 0
	fset := token.NewFileSet()
	for _, relative := range productionGoFiles(t, repoRoot) {
		if mayImportProjectroot(relative) {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(repoRoot, filepath.FromSlash(relative)), nil,
			parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		parsed++
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", relative, spec.Path.Value, err)
			}
			if path == projectrootImport {
				t.Errorf("%s:%d imports %s; take the Project root from the config source (config.Files)",
					relative, fset.Position(spec.Pos()).Line, projectrootImport)
			}
		}
	}
	// A guard that parsed no files would pass over a tree it never looked at.
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the guard proved nothing")
	}
}

// configImport is this package's import path, the one the read entries below are reached through.
const configImport = "github.com/airiclenz/apogee/internal/config"

// sourceBypasses are the package-level read entries that take paths rather than a config source.
// They stay exported for this package's own tests and for the test fixtures elsewhere; a production
// read asks a [Files] instead ([Files.Global], [Files.Layered], [Files.GlobalDocument],
// [Files.Overlay]), so the files it reads are the ones the session's resolution named.
var sourceBypasses = map[string]bool{
	"LoadFileConfig":      true,
	"LoadLayeredConfig":   true,
	"LoadLayeredOffer":    true,
	"ReadGlobalDocument":  true,
	"OverlayProjectLayer": true,
	"OverlayProjectOffer": true,
}

// TestProductionReadsGoThroughTheConfigSource holds the one-source rule for config reads: no
// production .go file outside internal/config references a package-level read entry that takes
// paths (sourceBypasses), so every live re-read asks the session's config source ([Files]) and
// cannot pick a file or a Project root of its own. The Drivers' boot calls (ApplyConfig,
// ApplyConfigOffer) and the test seam ResolveOptions are not read entries of that kind and stay.
// Each file is parsed and only selectors on the config import are judged — under whatever name the
// file imports it — so a comment naming an entry passes and a build-tagged file is checked from
// any host. It walks the module root package's own files plus internal/ and cmd/, skipping every
// directory whose name starts with a dot.
func TestProductionReadsGoThroughTheConfigSource(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..")
	parsed := 0
	fset := token.NewFileSet()
	for _, relative := range productionGoFiles(t, repoRoot) {
		if strings.HasPrefix(relative, "internal/config/") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(repoRoot, filepath.FromSlash(relative)), nil,
			parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		parsed++
		for _, ref := range sourceBypassReferences(file) {
			t.Errorf("%s:%d references config.%s; read through the config source (config.Files)",
				relative, fset.Position(ref.Pos()).Line, ref.Name)
		}
	}
	// A guard that parsed no files would pass over a tree it never looked at.
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the guard proved nothing")
	}
}

// sourceBypassReferences returns every selector in file that names a read entry of sourceBypasses
// on the config import, under whatever name the file imports it: a call and a bare reference
// alike. A file that dot-imports the package is reported at the import, since a bare entry name
// there cannot be told from a local one.
func sourceBypassReferences(file *ast.File) []*ast.Ident {
	var found []*ast.Ident
	localName := ""
	for _, imported := range file.Imports {
		if strings.Trim(imported.Path.Value, `"`) != configImport {
			continue
		}
		switch {
		case imported.Name == nil:
			localName = "config"
		case imported.Name.Name == ".":
			found = append(found, imported.Name)
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
		if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == localName && sourceBypasses[selector.Sel.Name] {
			found = append(found, selector.Sel)
		}
		return true
	})
	return found
}

// mayImportProjectroot reports whether the file at the slash path relative sits in a directory
// allowed to import the walk.
func mayImportProjectroot(relative string) bool {
	for _, dir := range projectrootReaders {
		if strings.HasPrefix(relative, dir) {
			return true
		}
	}
	return false
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
