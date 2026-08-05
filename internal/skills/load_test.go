package skills

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeSkill creates <base>/<id>/SKILL.md with the given content (a skill folder).
func writeSkill(t *testing.T, base, id, content string) {
	t.Helper()
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDiscoversSkillFolders(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "alpha", "---\nid: alpha\nsummary: the alpha skill\n---\nbody A")
	writeSkill(t, filepath.Join(home, "skills"), "beta", "---\nid: beta\nsummary: the beta skill\n---\nbody B")

	cat, err := Load(Sources{Home: home})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if got := len(cat.List()); got != 2 {
		t.Fatalf("loaded %d skills, want 2", got)
	}
	a, ok := cat.Get("alpha")
	if !ok {
		t.Fatal("alpha not found")
	}
	if a.Body != "body A" {
		t.Errorf("alpha body = %q, want %q", a.Body, "body A")
	}
	if a.Dir != filepath.Join(home, "skills", "alpha") {
		t.Errorf("alpha Dir = %q, want the skill folder", a.Dir)
	}
}

// ADR 0032: the user's global library wins a cross-source id collision, so a cloned repo cannot
// substitute its own instructions for a skill the user invokes by muscle memory. The loser is
// still one skill, not two, and it is RECORDED — the substitution the old order made silently is
// now nameable in the /skills report.
func TestLoadHomeOverridesWorkspaceOnIDCollision(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "dup", "---\nid: dup\nsummary: home version\n---\nFROM HOME")
	writeSkill(t, filepath.Join(ws, ".apogee", "skills"), "dup", "---\nid: dup\nsummary: ws version\n---\nFROM WORKSPACE")

	cat, _ := Load(Sources{Home: home, Workspace: ws}) // a shadow record joins into the soft error
	dup, _ := cat.Get("dup")
	if dup.Body != "FROM HOME" {
		t.Errorf("collision winner body = %q, want the user's global library to win", dup.Body)
	}
	if got := len(cat.List()); got != 1 {
		t.Errorf("collision produced %d skills, want 1 (the winner, not both)", got)
	}
	assertShadowed(t, cat,
		filepath.Join(ws, ".apogee", "skills", "dup", "SKILL.md"),
		filepath.Join(home, "skills", "dup", "SKILL.md"))
}

// The intra-workspace order is deliberately unchanged by ADR 0032: only the global library moved.
// The bare skills/ dir still outranks .apogee/skills — and that loser is recorded too.
func TestLoadBareProjectSkillsStillBeatDotApogeeOnCollision(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, filepath.Join(ws, ".apogee", "skills"), "dup", "---\nid: dup\nsummary: dot version\n---\nFROM DOT")
	writeSkill(t, filepath.Join(ws, "skills"), "dup", "---\nid: dup\nsummary: bare version\n---\nFROM BARE")

	cat, _ := Load(Sources{Workspace: ws, UseProjectSkills: true})
	dup, _ := cat.Get("dup")
	if dup.Body != "FROM BARE" {
		t.Errorf("collision winner body = %q, want the bare skills/ dir to outrank .apogee/skills", dup.Body)
	}
	assertShadowed(t, cat,
		filepath.Join(ws, ".apogee", "skills", "dup", "SKILL.md"),
		filepath.Join(ws, "skills", "dup", "SKILL.md"))
}

// The same-source case: two folders in ONE dir declaring the same id. This lost one silently
// before ADR 0032 — against the package's own "soft must not mean silent" contract — and the
// walk's lexical order decides, so the later folder is the live copy.
func TestLoadRecordsCollisionWithinOneSourceDir(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "aaa", "---\nid: dup\nsummary: first\n---\nFROM AAA")
	writeSkill(t, filepath.Join(home, "skills"), "zzz", "---\nid: dup\nsummary: second\n---\nFROM ZZZ")

	cat, _ := Load(Sources{Home: home})
	if got := len(cat.List()); got != 1 {
		t.Fatalf("same-dir collision produced %d skills, want 1: %+v", got, cat.List())
	}
	dup, _ := cat.Get("dup")
	if dup.Body != "FROM ZZZ" {
		t.Errorf("collision winner body = %q, want the later-walked folder", dup.Body)
	}
	assertShadowed(t, cat,
		filepath.Join(home, "skills", "aaa", "SKILL.md"),
		filepath.Join(home, "skills", "zzz", "SKILL.md"))
}

// assertShadowed checks the catalog recorded exactly one shadowing, naming loser as the file that
// lost and winner as the copy that is live — reached through errors.As, which is how the /skills
// report tells a shadowed skill from one that genuinely could not load.
func assertShadowed(t *testing.T, cat *Catalog, loser, winner string) {
	t.Helper()
	skipped := cat.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("Skipped() = %d entries, want 1 shadow record: %+v", len(skipped), skipped)
	}
	if skipped[0].Path != loser {
		t.Errorf("shadow record Path = %q, want the shadowed file %q", skipped[0].Path, loser)
	}
	var shadow ShadowedError
	if !errors.As(skipped[0].Err, &shadow) {
		t.Fatalf("shadow cause = %v, want a ShadowedError reachable via errors.As", skipped[0].Err)
	}
	if shadow.By != winner {
		t.Errorf("ShadowedError.By = %q, want the winning file %q", shadow.By, winner)
	}
}

func TestLoadProjectSkillsGating(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, filepath.Join(ws, "skills"), "proj", "---\nid: proj\nsummary: a project skill\n---\nbody")

	off, err := Load(Sources{Workspace: ws, UseProjectSkills: false})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if _, ok := off.Get("proj"); ok {
		t.Error("workspace skills/ was loaded with UseProjectSkills=false")
	}

	on, err := Load(Sources{Workspace: ws, UseProjectSkills: true})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if _, ok := on.Get("proj"); !ok {
		t.Error("workspace skills/ was NOT loaded with UseProjectSkills=true")
	}
}

func TestLoadMissingDirsTolerated(t *testing.T) {
	// Point at directories that do not exist: Load must not error, just return an empty catalog.
	cat, err := Load(Sources{
		Home:             filepath.Join(t.TempDir(), "nope"),
		Workspace:        filepath.Join(t.TempDir(), "alsonope"),
		UseProjectSkills: true,
	})
	if err != nil {
		t.Fatalf("missing dirs should be tolerated, got error: %v", err)
	}
	if got := len(cat.List()); got != 0 {
		t.Errorf("empty load produced %d skills, want 0", got)
	}
	if cat == nil {
		t.Error("Load returned a nil catalog; it must always be non-nil")
	}
}

func TestLoadMalformedSkillSkippedWithSoftError(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "good", "---\nid: good\nsummary: fine\n---\nbody")
	writeSkill(t, filepath.Join(home, "skills"), "bad", "") // empty SKILL.md → rejected

	cat, err := Load(Sources{Home: home})
	if err == nil {
		t.Error("expected a soft error reporting the malformed skill, got nil")
	}
	if _, ok := cat.Get("good"); !ok {
		t.Error("the good skill was dropped because a sibling was malformed")
	}
	if _, ok := cat.Get("bad"); ok {
		t.Error("the malformed skill was loaded instead of skipped")
	}
}

// A skip must be reported STRUCTURALLY on the catalog, not only as a joined error string: the
// /skills report names the skill and the file, and a caller that drops Load's error (as
// NewProvider does) must still be able to tell the human why a skill vanished. Without this,
// a malformed skill and an absent one are indistinguishable.
func TestLoadRecordsSkippedSkillOnCatalog(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "good", "---\nid: good\nsummary: fine\n---\nbody")
	// Unrecoverable on BOTH paths: invalid YAML (an unbalanced quote), and no recognised key for
	// the lenient scan to salvage — so it is a genuine skip, not one the leniency now rescues.
	writeSkill(t, filepath.Join(home, "skills"), "bad", "---\nnope: \"unbalanced\n---\nbody")

	cat, _ := Load(Sources{Home: home}) // the error is deliberately dropped, as NewProvider does

	skipped := cat.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("Skipped() = %d entries, want 1: %+v", len(skipped), skipped)
	}
	if got := skipped[0].Name(); got != "bad" {
		t.Errorf("skip Name() = %q, want the folder name %q", got, "bad")
	}
	want := filepath.Join(home, "skills", "bad", "SKILL.md")
	if skipped[0].Path != want {
		t.Errorf("skip Path = %q, want %q", skipped[0].Path, want)
	}
	if !strings.Contains(skipped[0].Reason(), "frontmatter") {
		t.Errorf("skip Reason() = %q, want it to name the frontmatter failure", skipped[0].Reason())
	}
	if len(cat.List()) != 1 {
		t.Errorf("the good sibling did not survive the skip: %+v", cat.List())
	}
}

// Skipped returns a copy, so the catalog stays read-only after Load — a caller mutating the
// returned slice must not corrupt the snapshot the menu and the loop share.
func TestSkippedIsACopy(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "bad", "")

	cat, _ := Load(Sources{Home: home})
	first := cat.Skipped()
	if len(first) != 1 {
		t.Fatalf("Skipped() = %d entries, want 1", len(first))
	}
	first[0] = SkipError{Path: "clobbered"}

	if got := cat.Skipped()[0].Path; got == "clobbered" {
		t.Error("mutating the returned slice mutated the catalog's own skips")
	}
}

// A clean scan reports no skips and no error — the negative case, so the report never invents a
// failures section for a healthy library.
func TestLoadCleanScanHasNoSkips(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "good", "---\nid: good\nsummary: fine\n---\nbody")

	cat, err := Load(Sources{Home: home})
	if err != nil {
		t.Fatalf("Load soft error on a clean scan: %v", err)
	}
	if got := cat.Skipped(); len(got) != 0 {
		t.Errorf("Skipped() = %+v, want empty on a clean scan", got)
	}
}

// TestLoadOversizeSkillFileRefused pins the bounded read (item 8): a SKILL.md past the byte
// cap is refused as a soft error and never materialized, while a well-sized sibling still loads
// — a hostile repo cannot OOM discovery with a giant marker file.
func TestLoadOversizeSkillFileRefused(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "ok", "---\nid: ok\nsummary: fine\n---\nbody")
	big := "---\nid: huge\nsummary: s\n---\n" + strings.Repeat("A", maxSkillFileBytes+1)
	writeSkill(t, filepath.Join(home, "skills"), "huge", big)

	cat, err := Load(Sources{Home: home})
	if err == nil {
		t.Error("expected a soft error reporting the oversized skill file, got nil")
	}
	if _, ok := cat.Get("huge"); ok {
		t.Error("an oversized SKILL.md was loaded instead of refused")
	}
	if _, ok := cat.Get("ok"); !ok {
		t.Error("the well-sized skill was dropped because a sibling was oversized")
	}
}

func TestLoadDottedDirsSkipped(t *testing.T) {
	home := t.TempDir()
	// A SKILL.md hidden inside a dotted dir must not be discovered.
	writeSkill(t, filepath.Join(home, "skills", ".hidden"), "secret", "---\nid: secret\nsummary: s\n---\nb")
	writeSkill(t, filepath.Join(home, "skills"), "visible", "---\nid: visible\nsummary: s\n---\nb")

	cat, err := Load(Sources{Home: home})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if _, ok := cat.Get("secret"); ok {
		t.Error("a skill under a dotted dir was discovered; dotted dirs must be skipped")
	}
	if _, ok := cat.Get("visible"); !ok {
		t.Error("the visible skill was not loaded")
	}
}

func TestLoadSymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	home := t.TempDir()
	outside := t.TempDir()
	// A real skill sitting OUTSIDE the skills root.
	writeSkill(t, outside, "escapee", "---\nid: escapee\nsummary: should not load\n---\nLEAKED")

	skillsRoot := filepath.Join(home, "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink a folder inside the skills root to the outside skill folder. The os.Root walk must
	// refuse to follow it out of the fence, so the escapee never loads.
	if err := os.Symlink(filepath.Join(outside, "escapee"), filepath.Join(skillsRoot, "escapee")); err != nil {
		t.Fatal(err)
	}

	cat, _ := Load(Sources{Home: home})
	if _, ok := cat.Get("escapee"); ok {
		t.Error("a skill reached through an escaping symlink was loaded; the os.Root fence failed")
	}
}
