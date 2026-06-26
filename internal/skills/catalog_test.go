package skills

import (
	"reflect"
	"testing"
)

// build assembles a catalog directly from skills, bypassing disk, for catalog-shape tests.
func build(skills ...Skill) *Catalog {
	c := newCatalog()
	for _, s := range skills {
		c.set(s)
	}
	return c
}

func TestCatalogListSortedByDisplayName(t *testing.T) {
	c := build(
		Skill{ID: "z", DisplayName: "Zebra", Summary: "s"},
		Skill{ID: "a", DisplayName: "Apple", Summary: "s"},
		Skill{ID: "m", DisplayName: "Mango", Summary: "s"},
	)
	var got []string
	for _, s := range c.List() {
		got = append(got, s.DisplayName)
	}
	if want := []string{"Apple", "Mango", "Zebra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("List order = %v, want %v", got, want)
	}
}

func TestCatalogListTieBreaksByID(t *testing.T) {
	c := build(
		Skill{ID: "b", DisplayName: "Same", Summary: "s"},
		Skill{ID: "a", DisplayName: "Same", Summary: "s"},
	)
	list := c.List()
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Errorf("equal display names not tie-broken by ID: got %q then %q", list[0].ID, list[1].ID)
	}
}

func TestCatalogResolveOrderAndUnknownSkip(t *testing.T) {
	c := build(
		Skill{ID: "one", DisplayName: "One", Summary: "s", Body: "B1"},
		Skill{ID: "two", DisplayName: "Two", Summary: "s", Body: "B2"},
	)
	got := c.Resolve([]string{"two", "missing", "one"})
	if len(got) != 2 {
		t.Fatalf("Resolve returned %d skills, want 2 (unknown skipped)", len(got))
	}
	if got[0].ID != "two" || got[1].ID != "one" {
		t.Errorf("Resolve did not preserve id order: got %q, %q", got[0].ID, got[1].ID)
	}
}

func TestCatalogResolveSkillsToDomain(t *testing.T) {
	c := build(Skill{ID: "x", DisplayName: "X", Summary: "s", Body: "the body"})
	got := c.ResolveSkills([]string{"x", "nope"})
	if len(got) != 1 {
		t.Fatalf("ResolveSkills returned %d, want 1", len(got))
	}
	if got[0].ID != "x" || got[0].DisplayName != "X" || got[0].Body != "the body" {
		t.Errorf("ResolveSkills mapped fields wrong: %+v", got[0])
	}
}

func TestCatalogGet(t *testing.T) {
	c := build(Skill{ID: "x", DisplayName: "X", Summary: "s"})
	if _, ok := c.Get("x"); !ok {
		t.Error("Get(known) reported not found")
	}
	if _, ok := c.Get("nope"); ok {
		t.Error("Get(unknown) reported found")
	}
}
