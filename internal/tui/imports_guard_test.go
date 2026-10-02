package tui

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// providerImportPath is the package ADR 0024 decision 5 keeps the renderer off: the TUI reads
// effort capabilities and the Beat through internal/domain and internal/heartbeat, and the
// composition root converts at the boundary.
const providerImportPath = "github.com/airiclenz/apogee/internal/provider"

// TestTUIImportsNoProvider pins ADR 0024's "never internal/provider" rule for this package's
// production files. The walk reads every non-test .go file regardless of build tags
// (altscreen_windows.go included), so a provider import behind a GOOS constraint is still
// caught. The live and e2e tests' test-only import is allowed: _test.go files are not scanned.
func TestTUIImportsNoProvider(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	paths := packageGoFiles(t, false)
	if len(paths) == 0 {
		t.Fatal("no non-test .go files were found; the import guard proved nothing")
	}
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", imported.Path.Value, path, err)
			}
			if importPath == providerImportPath {
				t.Errorf("%s imports %q; internal/tui production code reads provider data through internal/domain (ADR 0024 decision 5)",
					path, importPath)
			}
		}
	}
}
