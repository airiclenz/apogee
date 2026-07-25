package winlabel

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// allowedExternalImport is the ONE non-standard-library dependency D2 permits: the OS
// bindings the Windows-tagged half of this package calls the label APIs through.
const allowedExternalImport = "golang.org/x/sys/windows"

// TestPackageImportsNothingFromApogee holds D2 of the 2026-07-25 winlabel plan: this
// package is a LEAF over the standard library plus golang.org/x/sys/windows, and imports
// nothing from apogee — not internal/domain, not internal/platform. That is what keeps the
// boundary one-way. A single import back up the tree turns the module into the two-way
// seam it was extracted from, and the compiler would happily accept it.
//
// The rule is checked as an ALLOW-list rather than as a ban on one module path, because
// D2's decision is "the standard library and x/sys/windows, nothing else" — a deny-list
// would wave through any third-party package that happened not to be apogee.
//
// The files are read straight off the DIRECTORY and parsed with go/parser rather than
// consulted through the build context, so the //go:build windows half is checked from a
// Linux or macOS run too: the files a `go vet` on the development machine never opens are
// exactly the ones a violation could hide in.
func TestPackageImportsNothingFromApogee(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if path == allowedExternalImport || isStandardLibrary(path) {
				continue
			}
			t.Errorf("%s imports %q; winlabel is a leaf over the standard library and %s. Nothing from apogee may be imported here — package platform wraps the confinement sentinel at its own call site instead",
				name, path, allowedExternalImport)
		}
	}
	// A guard that read no files would pass over a package it never looked at.
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the leaf-dependency guard proved nothing")
	}
}

// isStandardLibrary reports whether path names a standard-library package, by the rule the
// go command itself reserves: an import path whose FIRST element carries no dot cannot be a
// module path, so it belongs to the standard library.
func isStandardLibrary(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
