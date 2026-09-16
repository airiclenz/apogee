package apogee

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestEveryDomainEventVariantIsAliased pins the public Event alias block complete: every
// struct in internal/domain/events.go that embeds EventBase is an Event variant, and each
// one must be re-exported as `X = domain.X` in apogee.go, or an embedder that never imports
// internal/* can neither type-switch on it nor reach its methods (the RefClippedEvent gap
// that shipped without its alias is the case this guard closes).
func TestEveryDomainEventVariantIsAliased(t *testing.T) {
	t.Parallel()

	variants := domainEventVariants(t, filepath.Join("internal", "domain", "events.go"))
	if len(variants) == 0 {
		t.Fatal("found no EventBase-embedding structs in internal/domain/events.go")
	}
	aliases := domainAliases(t, "apogee.go")

	for _, name := range variants {
		if aliases[name] != name {
			t.Errorf("internal/domain.%s embeds EventBase but apogee.go has no `%s = domain.%s` alias", name, name, name)
		}
	}
}

// domainEventVariants returns, in source order, the names of every struct type declared in
// path that embeds EventBase.
func domainEventVariants(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				if len(field.Names) != 0 {
					continue // a named field, not an embedding
				}
				if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "EventBase" {
					names = append(names, ts.Name.Name)
					break
				}
			}
		}
	}
	return names
}

// domainAliases returns every `Local = domain.Remote` type alias declared in path, keyed by
// the local name and valued by the domain name.
func domainAliases(t *testing.T, path string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	aliases := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			if !ts.Assign.IsValid() {
				continue // a definition, not an alias
			}
			sel, ok := ts.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "domain" {
				aliases[ts.Name.Name] = sel.Sel.Name
			}
		}
	}
	return aliases
}
