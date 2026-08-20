package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"testing"
)

// entryKindSource is the file the kind-coverage guard reads. It is the table's OWN file, and it is
// parsed off disk rather than reflected over for [TestFoldEventCoversEveryEventVariant]'s reason:
// reflection over an int enum can only see the constants a test already names, which is precisely
// the set that cannot contain the omission. The parser sees the const block itself.
const entryKindSource = "entrykind.go"

// entryKindType is the enum whose const block the guard walks.
const entryKindType = "entryKind"

// TestEntryKindRulesAnswerForEveryKind is the structural guard behind the promise entrykind.go
// makes: every declared entry kind has a row in [entryKindRules], so a new kind cannot reach the
// view with six unanswered questions behind it — an unnamed wire string, a block that will not
// open, a note the walk steps over or does not, a paint memoised when it should not be.
//
// It is [TestFoldEventCoversEveryEventVariant] one level down: parse the truth out of the source,
// compare it against what the code claims, and make the omission loud. The reverse check catches a
// row left behind by a deleted kind — a row for a value no const names is a rule nothing reads.
func TestEntryKindRulesAnswerForEveryKind(t *testing.T) {
	t.Parallel()

	declared := declaredEntryKinds(t)
	// A guard that parsed no constants would pass over an enum it never looked at.
	if len(declared) == 0 {
		t.Fatalf("no %s constants were parsed out of %s; the coverage guard proved nothing", entryKindType, entryKindSource)
	}

	for value, name := range declared {
		if _, ok := entryKindRules[entryKind(value)]; !ok {
			t.Errorf("%s is an %s with no row in entryKindRules: say what it is called on the wire, whether it carries a block state, whether it is a host note, whether it may be cached, whether its header blinks, and whether it heads a prompt",
				name, entryKindType)
		}
	}
	for _, value := range slices.Sorted(maps.Keys(entryKindRules)) {
		if int(value) >= len(declared) {
			t.Errorf("entryKindRules has a row for %s(%d), which %s no longer declares", entryKindType, value, entryKindSource)
		}
	}
}

// TestEntryKindPersistedNamesAreUnique pins the invariant the decode side is built on: the inverse
// map is only a faithful inverse while no two kinds claim the same wire string. Two kinds sharing
// one name would make every stored entry of one of them decode as the other, silently and only in
// records written before the clash.
func TestEntryKindPersistedNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]entryKind, len(entryKindRules))
	for _, kind := range slices.Sorted(maps.Keys(entryKindRules)) {
		name := entryKindRules[kind].persistedName
		if name == "" {
			continue // a kind that is never serialized claims no string at all
		}
		if other, clash := seen[name]; clash {
			t.Errorf("%s(%d) and %s(%d) both persist as %q; the decode inverse can only keep one",
				entryKindType, other, entryKindType, kind, name)
		}
		seen[name] = kind
	}
	if len(seen) != len(entryKindByName) {
		t.Errorf("%d kinds carry a wire name but entryKindByName holds %d", len(seen), len(entryKindByName))
	}
}

// declaredEntryKinds parses entrykind.go's entryKind const block and returns each constant's
// source name keyed by its iota value. The block is a plain iota run, so a constant's position in
// the block IS its value — which is the fact the wire form deliberately does not depend on
// ([entryKindRule.persistedName]) and the fact this guard does.
func declaredEntryKinds(t *testing.T) map[int]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, entryKindSource, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", entryKindSource, err)
	}
	kinds := make(map[int]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || !declaresEntryKind(gen) {
			continue
		}
		for value, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 {
				t.Fatalf("%s's %s block has a spec that is not one plain name; the guard's position-is-value rule no longer holds", entryKindSource, entryKindType)
			}
			kinds[value] = vs.Names[0].Name
		}
	}
	return kinds
}

// declaresEntryKind reports whether a const block is THE entryKind enum — its first spec types
// itself as entryKind, which is how an iota run names the type it counts in.
func declaresEntryKind(gen *ast.GenDecl) bool {
	if len(gen.Specs) == 0 {
		return false
	}
	first, ok := gen.Specs[0].(*ast.ValueSpec)
	if !ok {
		return false
	}
	id, ok := first.Type.(*ast.Ident)
	return ok && id.Name == entryKindType
}
