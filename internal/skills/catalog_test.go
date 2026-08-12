package skills

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// build assembles a catalog directly from skills, bypassing disk, for catalog-shape tests. Each
// skill is given a synthetic source path, since set records the file a collision would displace.
func build(skills ...Skill) *Catalog {
	c := newCatalog()
	for _, s := range skills {
		c.set(s, filepath.Join("/src", s.ID, "SKILL.md"))
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

// The skill's folder must survive the mapping to the loop-facing type: the loop names it in the
// injected block, and an address that stops at the catalog boundary would be no address at all.
func TestCatalogResolveSkillsCarriesTheSkillDir(t *testing.T) {
	dir := filepath.Join("/src", "x")
	c := build(Skill{ID: "x", DisplayName: "X", Summary: "s", Body: "the body", Dir: dir})
	got := c.ResolveSkills([]string{"x"})
	if len(got) != 1 {
		t.Fatalf("ResolveSkills returned %d, want 1", len(got))
	}
	if got[0].Dir != dir {
		t.Errorf("ResolveSkills dropped the skill folder: Dir = %q, want %q", got[0].Dir, dir)
	}
}

// A skill with no folder must map to an empty Dir rather than an invented one — the loop keys the
// files: line on emptiness, so a placeholder here would promise a directory that does not exist.
func TestCatalogResolveSkillsWithoutDirLeavesItEmpty(t *testing.T) {
	c := build(Skill{ID: "x", DisplayName: "X", Summary: "s", Body: "the body"})
	got := c.ResolveSkills([]string{"x"})
	if len(got) != 1 {
		t.Fatalf("ResolveSkills returned %d, want 1", len(got))
	}
	if got[0].Dir != "" {
		t.Errorf("a dirless skill resolved with Dir = %q, want empty", got[0].Dir)
	}
}

// A replaced skill is recorded, not forgotten (ADR 0032). set is the single choke point every
// source dir funnels through, so pinning it here covers the cross-source and same-source cases
// alike: the loser's own file is named, and the cause carries the winner's path.
func TestCatalogSetRecordsTheDisplacedSkill(t *testing.T) {
	c := newCatalog()
	loser := filepath.Join("/ws", ".apogee", "skills", "dup", "SKILL.md")
	winner := filepath.Join("/home", "skills", "dup", "SKILL.md")
	c.set(Skill{ID: "dup", DisplayName: "Dup", Summary: "s", Body: "LOSER"}, loser)
	c.set(Skill{ID: "dup", DisplayName: "Dup", Summary: "s", Body: "WINNER"}, winner)

	if got, _ := c.Get("dup"); got.Body != "WINNER" {
		t.Errorf("live skill body = %q, want the last writer to win", got.Body)
	}
	skipped := c.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("Skipped() = %d entries, want 1 shadow record: %+v", len(skipped), skipped)
	}
	if skipped[0].Path != loser {
		t.Errorf("shadow record Path = %q, want the LOSING file %q", skipped[0].Path, loser)
	}
	var shadow ShadowedError
	if !errors.As(skipped[0].Err, &shadow) {
		t.Fatalf("shadow record cause = %v, want a ShadowedError reachable via errors.As", skipped[0].Err)
	}
	if shadow.By != winner {
		t.Errorf("ShadowedError.By = %q, want the winning file %q", shadow.By, winner)
	}
}

// A skill with no id collision records nothing: set must not manufacture a shadow entry for the
// ordinary case, or every clean scan would grow a phantom failures section.
func TestCatalogSetWithoutCollisionRecordsNothing(t *testing.T) {
	c := build(
		Skill{ID: "one", DisplayName: "One", Summary: "s"},
		Skill{ID: "two", DisplayName: "Two", Summary: "s"},
	)
	if got := c.Skipped(); len(got) != 0 {
		t.Errorf("Skipped() = %+v, want empty when no id collided", got)
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
