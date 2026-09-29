package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/airiclenz/apogee/internal/workflow"
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

// A displaced skill is recorded, not forgotten (ADR 0032), and the copy already in the map is the
// one that stays: load.go walks the sources highest-priority first, so the first writer of an id
// outranks every later one (audit 2026-08-25 F-06). set is the single choke point every source dir
// funnels through, so pinning it here covers the cross-source and same-source cases alike: the
// loser's own file is named, and the cause carries the winner's path.
func TestCatalogSetKeepsTheFirstAndRecordsTheDisplacedSkill(t *testing.T) {
	c := newCatalog()
	winner := filepath.Join("/home", "skills", "dup", "SKILL.md")
	loser := filepath.Join("/ws", ".apogee", "skills", "dup", "SKILL.md")
	c.set(Skill{ID: "dup", DisplayName: "Dup", Summary: "s", Body: "WINNER"}, winner)
	c.set(Skill{ID: "dup", DisplayName: "Dup", Summary: "s", Body: "LOSER"}, loser)

	if got, _ := c.Get("dup"); got.Body != "WINNER" {
		t.Errorf("live skill body = %q, want the first writer to win", got.Body)
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

func TestCatalogRecipeServesOnlyRecipeSkills(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := workflow.Plan{Name: "audit", Stages: []workflow.Stage{{
		Name: "items", Kind: workflow.StageFanout, Task: "check {item}",
		Over: &workflow.ItemSource{List: []string{"a"}},
	}}}
	c := build(
		Skill{ID: "audit", Recipe: &plan, Inputs: []workflow.InputDecl{{Name: "scope"}}, Dir: dir},
		Skill{ID: "plain", Dir: dir},
	)

	recipe, ok := c.Recipe("audit")
	if !ok || recipe.ID != "audit" || recipe.Dir != dir || len(recipe.Inputs) != 1 || recipe.Files == nil {
		t.Fatalf("Recipe(audit) = %+v, %v; want the recipe with its inputs and folder", recipe, ok)
	}
	recipe.Plan.Stages[0].Over.List[0] = "changed"
	if plan.Stages[0].Over.List[0] != "a" {
		t.Errorf("the served plan shares its item list with the catalog's")
	}
	if _, ok := c.Recipe("plain"); ok {
		t.Errorf("Recipe(plain) served a skill without a recipe")
	}
	if _, ok := c.Recipe("missing"); ok {
		t.Errorf("Recipe(missing) served an unknown id")
	}
	if got := c.RecipeIDs(); !reflect.DeepEqual(got, []string{"audit"}) {
		t.Errorf("RecipeIDs = %v, want [audit]", got)
	}
}

func TestSkillFilesOpensAShippedFolder(t *testing.T) {
	t.Parallel()

	files := skillFiles(ShippedMountPrefix + "debugging")
	if files == nil {
		t.Fatal("skillFiles(shipped:debugging) = nil")
	}
	if _, err := fs.Stat(files, "SKILL.md"); err != nil {
		t.Errorf("the shipped folder does not hold its SKILL.md: %v", err)
	}
	if skillFiles("relative/dir") != nil {
		t.Errorf("a relative folder address opened a folder")
	}
}

// A disk skill's folder is fenced (audit 2026-09-29, "Repo skill recipes can read host files
// through symlinks"): a file inside it reads, a relative symlink that stays inside reads, and a
// symlink resolving outside it — relative or absolute — is refused without its bytes.
func TestSkillFilesFencesADiskFolderAgainstEscapingSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()

	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	mustWrite(t, outside, "HOST SECRET")
	mustWrite(t, filepath.Join(dir, "prompts", "real.md"), "in the folder")
	mustSymlink(t, filepath.Join("prompts", "real.md"), filepath.Join(dir, "inside.md"))
	rel, err := filepath.Rel(dir, outside)
	if err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, rel, filepath.Join(dir, "relative-escape.md"))
	mustSymlink(t, outside, filepath.Join(dir, "absolute-escape.md"))

	files := skillFiles(dir)
	for _, name := range []string{"prompts/real.md", "inside.md"} {
		body, err := fs.ReadFile(files, name)
		if err != nil || string(body) != "in the folder" {
			t.Errorf("ReadFile(%s) = %q, %v; want the folder's file", name, body, err)
		}
	}
	for _, name := range []string{"relative-escape.md", "absolute-escape.md"} {
		body, err := fs.ReadFile(files, name)
		if err == nil || string(body) != "" {
			t.Errorf("ReadFile(%s) = %q, %v; want a refusal without the outside bytes", name, body, err)
		}
	}
}

// A disk folder that no longer opens still yields an FS — one whose every read fails — so a
// reader holding it never mistakes it for "no folder" and falls back to another tree.
func TestSkillFilesServesAnErrorForAnUnopenableFolder(t *testing.T) {
	t.Parallel()

	files := skillFiles(filepath.Join(t.TempDir(), "removed"))
	if files == nil {
		t.Fatal("skillFiles(removed folder) = nil; want an FS that refuses every read")
	}
	if _, err := fs.ReadFile(files, "p.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile on a removed folder = %v; want its not-exist error", err)
	}
}

// mustWrite writes content to path, creating its parent folders.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
