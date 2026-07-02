package skills

import (
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

func TestLoadWorkspaceOverridesHomeOnIDCollision(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "dup", "---\nid: dup\nsummary: home version\n---\nFROM HOME")
	writeSkill(t, filepath.Join(ws, ".apogee", "skills"), "dup", "---\nid: dup\nsummary: ws version\n---\nFROM WORKSPACE")

	cat, err := Load(Sources{Home: home, Workspace: ws})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	dup, _ := cat.Get("dup")
	if dup.Body != "FROM WORKSPACE" {
		t.Errorf("collision winner body = %q, want the workspace version to override home", dup.Body)
	}
	if got := len(cat.List()); got != 1 {
		t.Errorf("collision produced %d skills, want 1 (the override, not both)", got)
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
