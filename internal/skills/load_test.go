package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
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
// walk's lexical order decides, so under keep-first the EARLIER folder is the live copy.
func TestLoadRecordsCollisionWithinOneSourceDir(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "aaa", "---\nid: dup\nsummary: first\n---\nFROM AAA")
	writeSkill(t, filepath.Join(home, "skills"), "zzz", "---\nid: dup\nsummary: second\n---\nFROM ZZZ")

	cat, _ := Load(Sources{Home: home})
	if got := len(cat.List()); got != 1 {
		t.Fatalf("same-dir collision produced %d skills, want 1: %+v", got, cat.List())
	}
	dup, _ := cat.Get("dup")
	if dup.Body != "FROM AAA" {
		t.Errorf("collision winner body = %q, want the first-walked folder", dup.Body)
	}
	assertShadowed(t, cat,
		filepath.Join(home, "skills", "zzz", "SKILL.md"),
		filepath.Join(home, "skills", "aaa", "SKILL.md"))
}

// The global skill cap is first-come across every source dir, so whichever source the walk reaches
// LAST is the only one it can cut into. Walking the user's library FIRST is what stops the cap
// undoing ADR 0032's precedence (audit 2026-08-25 F-06): a repo shipping maxSkills folders used to
// fill the catalog before the library was read at all, and no collision rule can hand back an id
// that was never loaded.
func TestLoadCapNeverEvictsTheHomeLibrary(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "home-a", "---\nid: home-a\nsummary: s\n---\nFROM HOME A")
	writeSkill(t, filepath.Join(home, "skills"), "home-b", "---\nid: home-b\nsummary: s\n---\nFROM HOME B")
	repo := filepath.Join(ws, ".apogee", "skills")
	writeCapFillingSkills(t, repo)

	cat, _ := Load(Sources{Home: home, Workspace: ws})

	for _, id := range []string{"home-a", "home-b"} {
		if _, ok := cat.Get(id); !ok {
			t.Errorf("%s is missing: the repo's %d skills crowded the user's library out", id, maxSkills)
		}
	}
	if got := cat.Len(); got != maxSkills {
		t.Errorf("catalog holds %d skills, want the cap %d", got, maxSkills)
	}
	assertCappedUnder(t, cat, repo)
}

// The cap and the collision rule are one answer rather than two: a repo that BOTH fills the cap and
// collides on an id loses the id to the library and still takes the cap, and both losses are
// recorded instead of silent.
func TestLoadCapAndCollisionStillFavourHome(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "home-a", "---\nid: home-a\nsummary: s\n---\nFROM HOME A")
	writeSkill(t, filepath.Join(home, "skills"), "dup", "---\nid: dup\nsummary: home version\n---\nFROM HOME")
	repo := filepath.Join(ws, ".apogee", "skills")
	// "dup" sorts before every "rNNNN" folder, so the walk meets the collision before the cap.
	writeSkill(t, repo, "dup", "---\nid: dup\nsummary: repo version\n---\nFROM WORKSPACE")
	writeCapFillingSkills(t, repo)

	cat, _ := Load(Sources{Home: home, Workspace: ws})

	dup, _ := cat.Get("dup")
	if dup.Body != "FROM HOME" {
		t.Errorf("dup body = %q, want the user's library to win even as the repo fills the cap", dup.Body)
	}
	assertShadowedAmong(t, cat,
		filepath.Join(repo, "dup", "SKILL.md"),
		filepath.Join(home, "skills", "dup", "SKILL.md"))
	assertCappedUnder(t, cat, repo)
}

// writeCapFillingSkills plants maxSkills skill folders under dir, enough on their own to exhaust
// the global catalog cap. Ids sort after any single-word fixture id used beside them.
func writeCapFillingSkills(t *testing.T, dir string) {
	t.Helper()
	for i := range maxSkills {
		id := fmt.Sprintf("r%04d", i)
		writeSkill(t, dir, id, "---\nid: "+id+"\nsummary: s\n---\nb")
	}
}

// assertCappedUnder checks the scan recorded exactly one skill-cap skip and that it landed in dir —
// the lowest-priority source, the only one the cap may ever cut into.
func assertCappedUnder(t *testing.T, cat *Catalog, dir string) {
	t.Helper()
	var capped []SkipError
	for _, e := range cat.Skipped() {
		if strings.Contains(e.Reason(), "skill cap") {
			capped = append(capped, e)
		}
	}
	if len(capped) != 1 {
		t.Fatalf("Skipped() = %+v, want exactly one skill-cap record", cat.Skipped())
	}
	if !strings.HasPrefix(capped[0].Path, dir+string(filepath.Separator)) {
		t.Errorf("skill cap fell at %q, want it inside the lower-priority dir %q", capped[0].Path, dir)
	}
}

// assertShadowed checks the catalog recorded exactly one skip and that it is the shadowing of
// loser by winner — the clean-scan form, where nothing else was passed over.
func assertShadowed(t *testing.T, cat *Catalog, loser, winner string) {
	t.Helper()
	if skipped := cat.Skipped(); len(skipped) != 1 {
		t.Fatalf("Skipped() = %d entries, want 1 shadow record: %+v", len(skipped), skipped)
	}
	assertShadowedAmong(t, cat, loser, winner)
}

// assertShadowedAmong finds the scan's one shadow record among whatever else was skipped, naming
// loser as the file that lost and winner as the copy that is live — reached through errors.As,
// which is how the /skills report tells a shadowed skill from one that genuinely could not load.
func assertShadowedAmong(t *testing.T, cat *Catalog, loser, winner string) {
	t.Helper()
	var shadow ShadowedError
	var records []SkipError
	for _, e := range cat.Skipped() {
		if errors.As(e.Err, &shadow) {
			records = append(records, e)
		}
	}
	if len(records) != 1 {
		t.Fatalf("Skipped() holds %d shadow records, want 1: %+v", len(records), cat.Skipped())
	}
	if records[0].Path != loser {
		t.Errorf("shadow record Path = %q, want the shadowed file %q", records[0].Path, loser)
	}
	if !errors.As(records[0].Err, &shadow) {
		t.Fatalf("shadow cause = %v, want a ShadowedError reachable via errors.As", records[0].Err)
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

// mustMkdirAll and mustSymlink build the anchor fixtures below: a source dir whose own path
// components are symlinks, which writeSkill cannot express.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
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

// TestLoadAnchorSymlinkRefused pins the ANCHOR, which the test above does not: it covers a symlink
// BELOW the source dir, while os.OpenRoot follows symlinks in every component OF the path naming
// that dir. So a repo shipping `.apogee`, `.apogee/skills` or `skills` as a symlink used to move
// the fence itself and have the walk read a tree apogee never meant to scan — and the refusal must
// be recorded, not silent, or a source that vanishes is indistinguishable from an absent one.
func TestLoadAnchorSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	const escapee = "---\nid: escapee\nsummary: should not load\n---\nLEAKED"

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) Sources
	}{
		{
			name: "workspace .apogee/skills is the symlink",
			setup: func(t *testing.T) Sources {
				ws, outside := t.TempDir(), t.TempDir()
				writeSkill(t, outside, "escapee", escapee)
				mustMkdirAll(t, filepath.Join(ws, ".apogee"))
				mustSymlink(t, outside, filepath.Join(ws, ".apogee", "skills"))
				return Sources{Workspace: ws}
			},
		},
		{
			name: "workspace .apogee is the symlink",
			setup: func(t *testing.T) Sources {
				ws, outside := t.TempDir(), t.TempDir()
				writeSkill(t, filepath.Join(outside, "skills"), "escapee", escapee)
				mustSymlink(t, outside, filepath.Join(ws, ".apogee"))
				return Sources{Workspace: ws}
			},
		},
		{
			name: "workspace skills/ is the symlink",
			setup: func(t *testing.T) Sources {
				ws, outside := t.TempDir(), t.TempDir()
				writeSkill(t, outside, "escapee", escapee)
				mustSymlink(t, outside, filepath.Join(ws, "skills"))
				return Sources{Workspace: ws, UseProjectSkills: true}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cat, err := Load(tc.setup(t))
			if err == nil {
				t.Error("an uncontained source dir was passed over silently; expected a soft error")
			}
			if _, ok := cat.Get("escapee"); ok {
				t.Error("a skill reached through a symlinked anchor component was loaded")
			}
			if got := cat.Len(); got != 0 {
				t.Errorf("catalog holds %d skills, want 0: %+v", got, cat.List())
			}
			if got := cat.Skipped(); len(got) != 1 || !strings.Contains(got[0].Reason(), "not scanned") {
				t.Errorf("Skipped() = %+v, want one entry naming the source dir that was not scanned", got)
			}
		})
	}
}

// The rule is CONTAINMENT, not "no symlinks": a workspace that keeps its skills in a folder of its
// own and links .apogee/skills at it never leaves the base, so it still loads. Without this the
// fix would read as a ban on symlinked sources, and the next reader would relax the wrong half.
func TestLoadAnchorSymlinkInsideBaseFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws := t.TempDir()
	writeSkill(t, filepath.Join(ws, "vendored"), "kept", "---\nid: kept\nsummary: s\n---\nb")
	mustMkdirAll(t, filepath.Join(ws, ".apogee"))
	mustSymlink(t, filepath.Join("..", "vendored"), filepath.Join(ws, ".apogee", "skills"))

	cat, err := Load(Sources{Workspace: ws})
	if err != nil {
		t.Fatalf("Load soft error on an in-base symlinked source dir: %v", err)
	}
	if _, ok := cat.Get("kept"); !ok {
		t.Error("a source dir symlinked WITHIN the workspace was refused; the fence is containment, not a symlink ban")
	}
}

// The containment above is for repo-authored anchors. The apogee home is the operator's own control
// plane, so a `skills` symlink there was placed by the human — the dotfiles-managed library — and
// discovery follows it wherever it points. Without this the loader would take the global library
// away from the operator in order to defend against the operator.
func TestLoadHomeLibraryAnchorSymlinkFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	home, library := t.TempDir(), t.TempDir()
	writeSkill(t, library, "linked", "---\nid: linked\nsummary: s\n---\nb")
	mustSymlink(t, library, filepath.Join(home, "skills"))

	cat, err := Load(Sources{Home: home})
	if err != nil {
		t.Fatalf("Load soft error on the operator's symlinked home library: %v", err)
	}
	if _, ok := cat.Get("linked"); !ok {
		t.Error("the home library reached through the operator's symlink was not loaded")
	}
	if got := cat.Skipped(); len(got) != 0 {
		t.Errorf("Skipped() = %+v, want none — the library was scanned", got)
	}
}

// Following the operator's symlink RE-PINS the fence at the library it resolves to; it does not
// give the fence up. A symlink inside that library pointing anywhere else is refused exactly as
// TestLoadSymlinkEscapeRefused pins for an unlinked one — otherwise the next reader would relax
// the trusted anchor into "no fence at all".
func TestLoadHomeLibraryEscapeBelowResolvedTargetRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	home, library, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeSkill(t, library, "kept", "---\nid: kept\nsummary: s\n---\nb")
	writeSkill(t, outside, "escapee", "---\nid: escapee\nsummary: should not load\n---\nLEAKED")
	mustSymlink(t, library, filepath.Join(home, "skills"))
	mustSymlink(t, filepath.Join(outside, "escapee"), filepath.Join(library, "escapee"))

	cat, _ := Load(Sources{Home: home})
	if _, ok := cat.Get("escapee"); ok {
		t.Error("a skill reached through a symlink out of the RESOLVED home library was loaded; the fence must pin at the target")
	}
	if _, ok := cat.Get("kept"); !ok {
		t.Error("the library's own skill was dropped; only the escaping symlink must be refused")
	}
}

// TestLoadWalkDepthBounded and its width sibling pin the other half of item 11: maxSkills caps the
// CATALOG, so a tree that loads nothing at all — deep or wide — used to be walked in full. Both
// caps must stop the walk while leaving the skills the walk already reached in place.
func TestLoadWalkDepthBounded(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "skills")
	writeSkill(t, root, "shallow", "---\nid: shallow\nsummary: s\n---\nb")

	deep := root
	for range maxSkillDirDepth {
		deep = filepath.Join(deep, "n")
	}
	writeSkill(t, deep, "buried", "---\nid: buried\nsummary: s\n---\nb")

	cat, _ := Load(Sources{Home: home})
	if _, ok := cat.Get("buried"); ok {
		t.Errorf("a skill %d levels down was loaded; the walk must stop at %d", maxSkillDirDepth+1, maxSkillDirDepth)
	}
	if _, ok := cat.Get("shallow"); !ok {
		t.Error("the shallow skill was dropped; the depth cap must not stop the whole walk")
	}
	if got := cat.Skipped(); len(got) != 1 || !strings.Contains(got[0].Reason(), "depth cap") {
		t.Errorf("Skipped() = %+v, want one entry naming the depth cap", got)
	}
}

func TestLoadWalkWidthBounded(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "skills")
	// Folder names sort lexically in numeric order, which is the order WalkDir visits them: the
	// first holds a skill the walk reaches, the last one past the cap holds a skill it must not.
	first, last := "d0000", fmt.Sprintf("d%04d", maxSkillDirs+1)
	writeSkill(t, root, first, "---\nid: "+first+"\nsummary: s\n---\nb")
	for i := 1; i <= maxSkillDirs; i++ {
		mustMkdirAll(t, filepath.Join(root, fmt.Sprintf("d%04d", i)))
	}
	writeSkill(t, root, last, "---\nid: "+last+"\nsummary: s\n---\nb")

	cat, _ := Load(Sources{Home: home})
	if _, ok := cat.Get(last); ok {
		t.Errorf("a skill past the %d-directory cap was loaded; the walk must stop", maxSkillDirs)
	}
	if _, ok := cat.Get(first); !ok {
		t.Error("the skill the walk reached before the cap was dropped")
	}
	if got := cat.Skipped(); len(got) != 1 || !strings.Contains(got[0].Reason(), "directory cap") {
		t.Errorf("Skipped() = %+v, want one entry naming the directory cap", got)
	}
}

// realDir resolves a fixture dir through symlinks — the form readRoots answers in, and the form a
// temp dir needs on a box where /tmp or /var is itself a symlink (macOS).
func realDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", dir, err)
	}
	return real
}

// underDir reports whether path is dir or sits below it, both already resolved.
func underDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// TestReadRootsRefuseARelocatedWorkspaceAnchor is TestLoadAnchorSymlinkRefused's mount half. The
// walk already refuses a workspace anchor whose own path leaves the workspace; until F-13 the
// MOUNT did not, so a cloned repo shipping `.apogee/skills` as a symlink to /home or /etc handed
// grep, read_file, list_dir and find_files the tree discovery would not scan. The relocated anchor
// is dropped from the list entirely, and only it — the other sources still mount.
func TestReadRootsRefuseARelocatedWorkspaceAnchor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}

	for _, tc := range []struct {
		name string
		// setup plants the fixture and answers the sources plus the workspace roots that must
		// SURVIVE it — the relocated anchor is the only one that goes.
		setup func(t *testing.T, ws, outside string) (src Sources, keptWorkspaceRoots []string)
	}{
		{
			name: "workspace .apogee/skills is the symlink",
			setup: func(t *testing.T, ws, outside string) (Sources, []string) {
				mustMkdirAll(t, filepath.Join(ws, ".apogee"))
				mustSymlink(t, outside, filepath.Join(ws, ".apogee", "skills"))
				return Sources{Workspace: ws}, nil
			},
		},
		{
			name: "workspace .apogee is the symlink",
			setup: func(t *testing.T, ws, outside string) (Sources, []string) {
				mustSymlink(t, outside, filepath.Join(ws, ".apogee"))
				return Sources{Workspace: ws}, nil
			},
		},
		{
			name: "workspace skills/ is the symlink",
			setup: func(t *testing.T, ws, outside string) (Sources, []string) {
				mustMkdirAll(t, filepath.Join(ws, ".apogee", "skills"))
				mustSymlink(t, outside, filepath.Join(ws, "skills"))
				return Sources{Workspace: ws, UseProjectSkills: true},
					[]string{filepath.Join(realDir(t, ws), ".apogee", "skills")}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, outside, home := t.TempDir(), t.TempDir(), t.TempDir()
			mustMkdirAll(t, filepath.Join(home, "skills"))
			src, kept := tc.setup(t, ws, outside)
			src.Home = home

			roots := readRoots(src)
			// The trusted home library heads the list — the walk's own highest-priority-first order.
			want := append([]string{filepath.Join(realDir(t, home), "skills")}, kept...)
			if !slices.Equal(roots, want) {
				t.Errorf("readRoots() = %v, want %v — the relocated anchor must be dropped and nothing else with it",
					roots, want)
			}
			// Belt and braces: no entry may REACH the outside tree either, however it is spelled.
			for _, root := range roots {
				if underDir(security.EvalRealPath(root), realDir(t, outside)) {
					t.Errorf("readRoots() = %v mounts %s, which resolves outside the workspace; the read fence moved",
						roots, root)
				}
			}
		})
	}
}

// The rule is CONTAINMENT, not "no symlinks", and the mount says so the same way the walk does: an
// anchor symlinked WITHIN the workspace still mounts — as the path it RESOLVES to, which is the
// only spelling the read fence and the mount can both agree on.
func TestReadRootsResolveAnInBaseSymlinkedAnchor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws := t.TempDir()
	mustMkdirAll(t, filepath.Join(ws, "vendored"))
	mustMkdirAll(t, filepath.Join(ws, ".apogee"))
	mustSymlink(t, filepath.Join("..", "vendored"), filepath.Join(ws, ".apogee", "skills"))

	want := []string{filepath.Join(realDir(t, ws), "vendored")}
	if got := readRoots(Sources{Workspace: ws}); !slices.Equal(got, want) {
		t.Errorf("readRoots() = %v, want the resolved in-workspace target %v", got, want)
	}
}

// The home library is the operator's own, so its symlink is followed and the mount pinned at what
// it resolves to — the dotfiles-managed library reads, exactly as
// TestLoadHomeLibraryAnchorSymlinkFollowed pins for the walk.
func TestReadRootsFollowTheHomeLibrarySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	home, library := t.TempDir(), t.TempDir()
	mustSymlink(t, library, filepath.Join(home, "skills"))

	want := []string{realDir(t, library)}
	if got := readRoots(Sources{Home: home}); !slices.Equal(got, want) {
		t.Errorf("readRoots() = %v, want the resolved library %v", got, want)
	}
}

// A source dir nothing has created yet is still listed, exactly as sourceDirs lists it: readRoots
// reports where skills come from, and the mount side skips an unusable root of its own accord.
func TestReadRootsListADirThatDoesNotExist(t *testing.T) {
	ws, home := t.TempDir(), t.TempDir()

	want := []string{
		filepath.Join(realDir(t, home), "skills"),
		filepath.Join(realDir(t, ws), "skills"),
		filepath.Join(realDir(t, ws), ".apogee", "skills"),
	}
	got := readRoots(Sources{Workspace: ws, Home: home, UseProjectSkills: true})
	if !slices.Equal(got, want) {
		t.Errorf("readRoots() = %v, want every source listed in scan order %v", got, want)
	}
}
