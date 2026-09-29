package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/workflow"
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

// TestLoadCatalogByteCapSkipsTheRest pins the aggregate byte cap (audit 2026-09-20): a tree of
// skills that each pass the per-file cap but together exceed maxSkillCatalogBytes is cut at the
// cap with one recorded skip, while the user's own skill still loads.
func TestLoadCatalogByteCapSkipsTheRest(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "ok", "---\nid: ok\nsummary: fine\n---\nbody")
	repo := filepath.Join(ws, ".apogee", "skills")
	writeByteCapFillingSkills(t, repo, "r", 40)

	cat, _ := Load(Sources{Home: home, Workspace: ws})

	if _, ok := cat.Get("ok"); !ok {
		t.Error("the user's skill is missing: the repo's bulk crowded the library out")
	}
	if got := cat.Len(); got >= 41 {
		t.Errorf("catalog holds %d skills, want the byte cap to have cut the repo's 40 short", got)
	}
	assertByteCappedUnder(t, cat, repo)
}

// TestLoadCatalogByteCapNeverEvictsTheHomeLibrary is the byte cap's twin of
// TestLoadCapNeverEvictsTheHomeLibrary: the cap is first-come across the walk order, so with the
// library walked first, a workspace that fills it can only ever cut into itself.
func TestLoadCatalogByteCapNeverEvictsTheHomeLibrary(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "home-a", "---\nid: home-a\nsummary: s\n---\nFROM HOME A")
	writeSkill(t, filepath.Join(home, "skills"), "home-b", "---\nid: home-b\nsummary: s\n---\nFROM HOME B")
	repo := filepath.Join(ws, ".apogee", "skills")
	writeByteCapFillingSkills(t, repo, "r", 40)

	cat, _ := Load(Sources{Home: home, Workspace: ws})

	for _, id := range []string{"home-a", "home-b"} {
		if _, ok := cat.Get(id); !ok {
			t.Errorf("%s is missing: the repo's bulk crowded the user's library out", id)
		}
	}
	assertByteCappedUnder(t, cat, repo)
}

// TestLoadCatalogByteCapSpansBothTrees pins the counter on the Catalog rather than on one walk:
// 20 MiB under home plus 20 MiB under the workspace is one 40 MiB catalog, so the cap trips in
// the workspace walk — every home skill loaded, exactly one skip, naming the workspace root.
// A per-walk counter would load all 40 MiB and announce nothing.
func TestLoadCatalogByteCapSpansBothTrees(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	library := filepath.Join(home, "skills")
	writeByteCapFillingSkills(t, library, "h", 20)
	repo := filepath.Join(ws, ".apogee", "skills")
	writeByteCapFillingSkills(t, repo, "r", 20)

	cat, _ := Load(Sources{Home: home, Workspace: ws})

	for i := range 20 {
		id := fmt.Sprintf("h%04d", i)
		if _, ok := cat.Get(id); !ok {
			t.Errorf("%s is missing: the workspace's bulk cut into the user's library", id)
		}
	}
	if got := cat.Len(); got >= 40 {
		t.Errorf("catalog holds %d skills, want the byte cap to have spanned both trees", got)
	}
	assertByteCappedUnder(t, cat, repo)
	for _, e := range cat.Skipped() {
		if !strings.Contains(e.Reason(), repo) {
			t.Errorf("byte cap reason = %q, want it to name the workspace root %q", e.Reason(), repo)
		}
	}
}

// writeByteCapFillingSkills plants n skill folders under dir, each one byte under the per-file
// cap so every one loads on its own and together they weigh n MiB. Ids are prefix+NNNN, so a
// prefix chosen to sort after a single-word fixture id keeps that fixture first in the walk.
// The files take the no-frontmatter path (id from the folder, title and summary from the first
// two lines): the frontmatter regex walks every byte of a file it matches, which is seconds per
// 32 MiB catalog on a slow box, while the anchored miss on a `#` first line returns at once.
func writeByteCapFillingSkills(t *testing.T, dir, prefix string, n int) {
	t.Helper()
	for i := range n {
		id := fmt.Sprintf("%s%04d", prefix, i)
		head := "# " + id + "\nsummary s\n"
		writeSkill(t, dir, id, head+strings.Repeat("A", maxSkillFileBytes-1-len(head)))
	}
}

// assertByteCappedUnder checks the scan recorded exactly one catalog-byte-cap skip, that it is
// the scan's only skip, and that it landed in dir — the lowest-priority source, the only one the
// cap may ever cut into.
func assertByteCappedUnder(t *testing.T, cat *Catalog, dir string) {
	t.Helper()
	var capped []SkipError
	for _, e := range cat.Skipped() {
		if strings.Contains(e.Reason(), "catalog byte cap") {
			capped = append(capped, e)
		}
	}
	if len(capped) != 1 {
		t.Fatalf("Skipped() = %+v, want exactly one catalog byte cap record", cat.Skipped())
	}
	if !strings.HasPrefix(capped[0].Path, dir+string(filepath.Separator)) {
		t.Errorf("byte cap fell at %q, want it inside the lower-priority dir %q", capped[0].Path, dir)
	}
	if got := len(cat.Skipped()); got != 1 {
		t.Errorf("Skipped() = %d entries, want the byte cap record alone: %+v", got, cat.Skipped())
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

// TestLoadRecordsAnUnreadableDirEntry pins that an entry the walk cannot read is REPORTED rather
// than dropped: it is a place a skill may have sat, and a silent skip is indistinguishable from an
// empty folder — this package does not let soft mean silent. The readable sibling beside it must
// still load: one unreadable entry ends that branch, not the scan.
func TestLoadRecordsAnUnreadableDirEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0-mode directory is still readable, so the entry never errors")
	}
	home := t.TempDir()
	root := filepath.Join(home, "skills")
	writeSkill(t, root, "alpha", "---\nid: alpha\nsummary: the alpha skill\n---\nbody A")
	writeSkill(t, root, "locked", "---\nid: locked\nsummary: never read\n---\nbody L")

	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	// Restore the bits so t.TempDir's own cleanup can remove the tree.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	cat, err := Load(Sources{Home: home})
	if err == nil {
		t.Error("Load() returned no error; an entry that was not scanned must reach the caller")
	}
	if _, ok := cat.Get("alpha"); !ok {
		t.Error("the readable sibling was dropped; one unreadable entry must not end the walk")
	}
	if _, ok := cat.Get("locked"); ok {
		t.Fatal("the skill under the unreadable directory loaded; the fixture did not take")
	}
	got := cat.Skipped()
	if len(got) != 1 || !strings.Contains(got[0].Reason(), "not scanned") || filepath.Base(got[0].Path) != "locked" {
		t.Errorf("Skipped() = %+v, want one entry naming %s as not scanned", got, locked)
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

// ADR 0065 §1: four skills ship embedded in the binary. This is the standing check that every one
// of them PARSES — a shipped skill that fails the loader is a broken build the /skills report would
// have to explain to a user who cannot fix it, so the whole shipped tree is enumerated from the
// embed rather than from a hard-coded list, and items added later are covered without touching this
// test.
func TestLoadShippedSkillsAllParse(t *testing.T) {
	want := shippedIDs(t)
	if len(want) == 0 {
		t.Fatal("the embedded shipped tree holds no skill folders; go:embed all:shipped is not covering it")
	}

	cat, err := Load(Sources{UseShippedSkills: true})
	if err != nil {
		t.Fatalf("the shipped skills must load cleanly, got: %v", err)
	}
	if got := len(cat.List()); got != len(want) {
		t.Fatalf("shipped load produced %d skills, want %d (%v): %+v", got, len(want), want, cat.Skipped())
	}
	for _, id := range want {
		sk, ok := cat.Get(id)
		if !ok {
			t.Errorf("shipped skill %q did not load: %+v", id, cat.Skipped())
			continue
		}
		if sk.Body == "" {
			t.Errorf("shipped skill %q loaded with an empty body", id)
		}
		// A shipped skill announces its folder under the virtual mount its own tree is served
		// through (ADR 0065 §3), so the injected block's files: line and every {{SKILL_DIR}} in
		// the body name an address the read tools resolve — never a host path nothing can open.
		if want := ShippedMountPrefix + id; sk.Dir != want {
			t.Errorf("shipped skill %q announces Dir = %q, want %q", id, sk.Dir, want)
		}
	}
}

// The shipped catalog's id set is PINNED here, in a list a contributor edits by hand. Every other
// shipped-skill test enumerates the ids from the embed, so adding, renaming or removing a folder
// under shipped/ keeps `go test ./internal/skills/...` green — while it rewrites the /skills
// listing apogee ANNOUNCES, which two cmd/apogee tests pin. This guard is what makes that breakage
// surface here, in the package the change was made in, rather than two packages away.
func TestShippedCatalogIDsArePinned(t *testing.T) {
	t.Parallel()

	// Edit this list ONLY together with the cmd/apogee fixtures the failure message names.
	want := []string{"audit", "code-review", "commit-hygiene", "debugging", "planning"}

	cat, _ := Load(Sources{UseShippedSkills: true})
	got := make([]string, 0, cat.Len())
	for _, s := range cat.List() {
		got = append(got, s.ID)
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Fatalf("the shipped catalog announces %v, want %v.\n"+
			"Adding, renaming or removing a shipped skill rewrites the /skills listing apogee "+
			"announces, which cmd/apogee pins in TestE2EHostileSurfacesKeepTheirOwnRows (golden "+
			"frame cmd/apogee/testdata/frames/t12-skills.txt) and in TestE2ESmokeInProcess. Update "+
			"the list in this test, re-record the golden with `go test ./cmd/apogee -run "+
			"TestE2EHostileSurfacesKeepTheirOwnRows -update`, then run `go test ./cmd/apogee "+
			"-count=1` to confirm TestE2ESmokeInProcess still passes.", got, want)
	}
}

// The debugging skill is the one this item authors, so its identity is pinned by name: an id
// rename would silently break every `/debugging` a user has in muscle memory.
func TestLoadShippedDebuggingSkill(t *testing.T) {
	cat, _ := Load(Sources{UseShippedSkills: true})
	sk, ok := cat.Get("debugging")
	if !ok {
		t.Fatalf("the debugging skill did not load: %+v", cat.Skipped())
	}
	if sk.DisplayName == "" || sk.Summary == "" {
		t.Errorf("debugging loaded without a menu label/hint: displayName=%q summary=%q", sk.DisplayName, sk.Summary)
	}
	if len(sk.Triggers) == 0 {
		t.Error("debugging declares no triggers:, so the suggestion band can never offer it")
	}
}

// The gate's zero value is off, so every caller that predates the shipped source — and every test
// pinning a catalog's exact contents — loads exactly the disk sources it always did.
func TestLoadWithoutShippedSourceIsUnchanged(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "alpha", "---\nid: alpha\nsummary: the alpha skill\n---\nbody A")

	cat, err := Load(Sources{Home: home}) // UseShippedSkills left at its zero value
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if got := len(cat.List()); got != 1 {
		t.Fatalf("loaded %d skills, want only the disk one: %+v", got, cat.List())
	}
	if _, ok := cat.Get("debugging"); ok {
		t.Error("a shipped skill reached the catalog with UseShippedSkills unset")
	}
}

// ADR 0065 §1: shipped is the LOWEST-priority source, so a user who writes their own `debugging`
// keeps it — and the displaced shipped copy is recorded through the same ShadowedError channel
// ADR 0032 built, rather than vanishing.
func TestLoadHomeLibraryShadowsAShippedSkill(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "debugging",
		"---\nid: debugging\nsummary: my own\n---\nFROM THE USER")

	cat, _ := Load(Sources{Home: home, UseShippedSkills: true})
	sk, ok := cat.Get("debugging")
	if !ok {
		t.Fatal("debugging is missing from the catalog entirely")
	}
	if sk.Body != "FROM THE USER" {
		t.Errorf("collision winner body = %q, want the user's own copy to win the shipped one", sk.Body)
	}
	assertShadowedAmong(t, cat,
		filepath.Join(shippedSource, "debugging", skillFileName),
		filepath.Join(home, "skills", "debugging", skillFileName))
}

// The shipped source is embedded, not installed, so it must never be rendered as a host path: a
// pseudo-path in sourceDirs is a phantom directory in the /skills report, and one in readRoots is
// handed to the read tools and resolved against the process's working directory.
func TestShippedSourceIsNeverRenderedAsAHostPath(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	src := Sources{Home: home, Workspace: ws, UseProjectSkills: true, UseShippedSkills: true}

	for name, got := range map[string][]string{"sourceDirs": sourceDirs(src), "readRoots": readRoots(src)} {
		if len(got) != 3 {
			t.Errorf("%s = %v, want only the three disk anchors", name, got)
		}
		for _, dir := range got {
			if !filepath.IsAbs(dir) {
				t.Errorf("%s produced the non-absolute entry %q", name, dir)
			}
			if filepath.Base(dir) == shippedSource {
				t.Errorf("%s rendered the embedded shipped source as the path %q", name, dir)
			}
		}
	}
}

// shippedIDs lists the folder names of the embedded shipped tree — the ids those skills are
// expected to load under, read from the embed itself so the tests track the tree.
func shippedIDs(t *testing.T) []string {
	t.Helper()
	entries, err := shippedFiles.ReadDir(shippedDir)
	if err != nil {
		t.Fatalf("reading the embedded shipped tree: %v", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids
}

// recipePromptSkill is a recipe skill whose two prompt paths take both spellings an author has:
// relative to the skill folder, and led by {{SKILL_DIR}}.
const recipePromptSkill = "---\nid: sweep\nsummary: sweep a tree\nrecipe:\n" +
	"  - name: find\n    kind: fanout\n    over:\n      list: [a, b]\n    prompt: prompts/find.md\n" +
	"  - name: report\n    kind: merge\n    prompt: \"{{SKILL_DIR}}/prompts/merge.md\"\n" +
	"---\nRun the sweep."

// Once Load places a recipe skill, each prompt path is slash-separated and relative to the skill's
// own folder — on every OS, whichever spelling the author used — so the spawner opens it through
// the recipe's Files rather than as a host path.
func TestLoadRecipePromptsResolveUnderTheSkillDir(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "sweep", recipePromptSkill)

	cat, err := Load(Sources{Home: home})
	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	sk, ok := cat.Get("sweep")
	if !ok || sk.Recipe == nil {
		t.Fatalf("the recipe skill did not load with its recipe: %+v", cat.Skipped())
	}
	want := []string{"prompts/find.md", "prompts/merge.md"}
	got := []string{sk.Recipe.Stages[0].Prompt, sk.Recipe.Stages[1].Prompt}
	if !slices.Equal(got, want) {
		t.Errorf("prompts = %v, want %v", got, want)
	}
}

// A shipped recipe's prompts stay folder-relative too — never a host path, which no host folder
// would answer to — and are read through the recipe's Files on the virtual mount.
func TestShippedRecipePromptsStayVirtual(t *testing.T) {
	cat := newCatalog()
	walkSkills(cat, sourceTree{
		fsys:   fstest.MapFS{"sweep/SKILL.md": {Data: []byte(recipePromptSkill)}},
		name:   shippedSource,
		dirFor: shippedDirFor,
	})
	sk, ok := cat.Get("sweep")
	if !ok || sk.Recipe == nil {
		t.Fatalf("the shipped recipe skill did not load with its recipe: %+v", cat.Skipped())
	}
	want := []string{"prompts/find.md", "prompts/merge.md"}
	got := []string{sk.Recipe.Stages[0].Prompt, sk.Recipe.Stages[1].Prompt}
	if !slices.Equal(got, want) {
		t.Errorf("prompts = %v, want %v", got, want)
	}
}

// A skill whose recipe fails the validator is a recorded skip naming the problem, and its
// siblings still load.
func TestLoadInvalidRecipeIsASkipNamingTheProblem(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "good", "---\nid: good\nsummary: fine\n---\nbody")
	writeSkill(t, filepath.Join(home, "skills"), "sweep",
		"---\nid: sweep\nsummary: s\nrecipe:\n  - name: find\n    kind: fanout\n---\nbody")

	cat, err := Load(Sources{Home: home})
	if err == nil {
		t.Error("Load returned no soft error for the invalid recipe")
	}
	if _, ok := cat.Get("good"); !ok {
		t.Error("the good skill was dropped because a sibling's recipe was invalid")
	}
	if _, ok := cat.Get("sweep"); ok {
		t.Error("the skill with an invalid recipe loaded")
	}
	skipped := cat.Skipped()
	if len(skipped) != 1 || skipped[0].Name() != "sweep" {
		t.Fatalf("Skipped() = %+v, want the one sweep skip", skipped)
	}
	if reason := skipped[0].Reason(); !strings.Contains(reason, `stage "find", field "over"`) {
		t.Errorf("skip reason = %q, want the validator's problem naming the stage and field", reason)
	}
}

// The shipped audit skill is a recipe (ADR 0087 D6): it loads with its stages through
// workflow.Validate, declares scope as its one required input and focus as an optional one, opens
// on the split script it carries, asks the focus with `all` as the answer a human-less Driver
// takes, and ends on the report merge.
func TestShippedAuditRecipeLoads(t *testing.T) {
	t.Parallel()
	cat, _ := Load(Sources{UseShippedSkills: true})
	sk, ok := cat.Get("audit")
	if !ok || sk.Recipe == nil {
		t.Fatalf("the shipped audit skill did not load with its recipe: %+v", cat.Skipped())
	}
	wantInputs := []workflow.InputDecl{{Name: "scope", Required: true}, {Name: "focus"}}
	if len(sk.Inputs) != len(wantInputs) {
		t.Fatalf("audit declares inputs %+v, want scope and focus", sk.Inputs)
	}
	for i, want := range wantInputs {
		if got := sk.Inputs[i]; got.Name != want.Name || got.Required != want.Required || got.Default != "" {
			t.Errorf("input %d = %+v, want name %q required %v and no default", i, got, want.Name, want.Required)
		}
	}

	stages := sk.Recipe.Stages
	if problems := workflow.Validate(*sk.Recipe); len(problems) > 0 {
		t.Fatalf("the loaded recipe does not validate: %v", problems)
	}
	split := stages[0]
	if split.Kind != workflow.StageScript || !strings.Contains(split.Run, "{{SKILL_DIR}}/split.sh") {
		t.Errorf("the first stage = %+v, want the script stage running the bundled split.sh", split)
	}
	if _, err := fs.Stat(shippedFiles, shippedDir+"/audit/split.sh"); err != nil {
		t.Errorf("the recipe runs split.sh, which the embedded skill folder does not carry: %v", err)
	}
	at := slices.IndexFunc(stages, func(stage workflow.Stage) bool { return stage.Name == "focus" })
	if at < 0 || stages[at].Kind != workflow.StageAsk || stages[at].Default != "all" {
		t.Errorf("the recipe has no focus question defaulting to all: %+v", stages)
	}
	if last := stages[len(stages)-1]; last.Kind != workflow.StageMerge {
		t.Errorf("the last stage = %+v, want the report merge", last)
	}
}

// auditPromptsDir is the shipped audit skill's prompt folder inside the embedded tree.
const auditPromptsDir = shippedDir + "/audit/prompts"

// auditForbiddenPromptText is what an audit prompt ported from a coordinator-driven host must no
// longer carry: that host's delegation and task tools, its six-line text receipt, and its home
// skill folder. A workflow child hands its receipt back through `finish`, and the engine — not the
// child — spawns and stages.
var auditForbiddenPromptText = []string{"Agent(", "Task(", "TodoWrite", "subagent_type", "STATUS:", "~/.claude"}

// The shipped audit recipe's stage prompts (ADR 0087 D6): every prompt a stage names is in the
// embedded skill folder and ends on its receipt handed back through `finish`; every lens brief
// carries the shared rules verbatim, since a brief renders only {item} and {out} and cannot
// include another file; and no prompt in the folder carries another host's tools, text receipt or
// home path.
func TestShippedAuditPromptsAreEmbeddedAndFinishShaped(t *testing.T) {
	t.Parallel()
	cat, _ := Load(Sources{UseShippedSkills: true})
	sk, ok := cat.Get("audit")
	if !ok || sk.Recipe == nil {
		t.Fatalf("the shipped audit skill did not load with its recipe: %+v", cat.Skipped())
	}

	var named []string
	for _, stage := range sk.Recipe.Stages {
		if stage.Prompt == "" {
			continue
		}
		rel := stage.Prompt
		body, err := fs.ReadFile(shippedFiles, shippedDir+"/audit/"+rel)
		if err != nil {
			t.Errorf("stage %q names prompt %s, which the embedded skill folder does not carry: %v", stage.Name, rel, err)
			continue
		}
		named = append(named, path.Base(rel))
		if heading, tail := lastHeading(string(body)); !strings.Contains(heading, "Finish") ||
			!strings.Contains(tail, "call `finish`") {
			t.Errorf("prompt %s does not end on its finish call; its last section is %q", rel, heading)
		}
	}
	wantNamed := []string{
		"enumerate.md", "ground-truth.md", "lens-concurrency.md", "lens-correctness.md", "lens-intent.md",
		"lens-security.md", "lens-tests.md", "machine-checks.md", "report.md", "rollup.md", "verify.md",
	}
	slices.Sort(named)
	if !slices.Equal(named, wantNamed) {
		t.Errorf("the recipe names prompts %v, want %v", named, wantNamed)
	}

	shared, err := fs.ReadFile(shippedFiles, auditPromptsDir+"/_shared.md")
	if err != nil {
		t.Fatalf("the shared lens rules are not embedded: %v", err)
	}
	entries, err := fs.ReadDir(shippedFiles, auditPromptsDir)
	if err != nil {
		t.Fatalf("read the embedded prompt folder: %v", err)
	}
	lenses := 0
	for _, entry := range entries {
		body, err := fs.ReadFile(shippedFiles, auditPromptsDir+"/"+entry.Name())
		if err != nil {
			t.Fatalf("read prompt %s: %v", entry.Name(), err)
		}
		for _, forbidden := range auditForbiddenPromptText {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("prompt %s carries %q, which no workflow child's brief may name", entry.Name(), forbidden)
			}
		}
		if strings.HasPrefix(entry.Name(), "lens-") {
			lenses++
			if !strings.Contains(string(body), string(shared)) {
				t.Errorf("lens prompt %s does not carry _shared.md verbatim; copy the shared rules into it again", entry.Name())
			}
		}
	}
	if lenses != 5 {
		t.Errorf("the prompt folder holds %d lens briefs, want the five lenses", lenses)
	}
}

// lastHeading returns a markdown body's last heading line (any level) and the text after it. A
// heading-shaped line inside a fenced example still counts, so a prompt must close on its own
// section, after every example block.
func lastHeading(body string) (heading, tail string) {
	lines := strings.Split(body, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if strings.HasPrefix(lines[index], "#") {
			return lines[index], strings.Join(lines[index+1:], "\n")
		}
	}
	return "", body
}

// The fixture tree the split tests cut: 360 source lines over three folders, one test file, one
// file with a concurrency primitive, and the docs and prose a code audit never reads.
func auditFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	lines := func(n int, extra string) string {
		return strings.Repeat("x := 1\n", n-1) + extra + "\n"
	}
	files := map[string]string{
		"cmd/app/main.go":         lines(60, "x := 1"),
		"internal/a/a1_test.go":   lines(20, "x := 1"),
		"internal/b/b1.go":        lines(30, "go func() {}()"),
		"README.md":               lines(10, "prose"),
		"docs/design/notes.go":    lines(10, "x := 1"),
		"internal/a/testdata/f.g": lines(10, "x := 1"),
	}
	for i := 1; i <= 6; i++ {
		files[fmt.Sprintf("internal/a/a%d.go", i)] = lines(30, "x := 1")
	}
	for i := 2; i <= 4; i++ {
		files[fmt.Sprintf("internal/b/b%d.go", i)] = lines(30, "x := 1")
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// runAuditSplit runs the embedded split.sh from workspace over scope, as the recipe's script stage
// does, with partLines as its PART_LINES ("" leaves it unset) and partBytes as the engine's split
// budget, and returns the workflow folder and the receipt lines it printed.
func runAuditSplit(t *testing.T, workspace, scope, partLines, partBytes string) (string, map[string]string) {
	t.Helper()
	// GIT_DIR at a missing folder makes `git ls-files` fail, so the listing takes the find
	// fallback over the fixture whatever repository the temp dir happens to sit in.
	env := append(os.Environ(), "PART_LINES="+partLines, "GIT_DIR="+filepath.Join(workspace, "no-git"))
	return runAuditSplitWithEnv(t, workspace, scope, partBytes, env)
}

// runAuditSplitWithEnv runs the embedded split.sh as runAuditSplit does, in exactly env.
func runAuditSplitWithEnv(t *testing.T, workspace, scope, partBytes string, env []string) (string, map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX sh on this host to run split.sh")
	}
	script, err := fs.ReadFile(shippedFiles, shippedDir+"/audit/split.sh")
	if err != nil {
		t.Fatalf("read the embedded split.sh: %v", err)
	}
	run := t.TempDir()
	scriptPath := filepath.Join(run, "split.sh")
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", scriptPath, run, scope, "", partBytes)
	cmd.Dir = workspace
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("split.sh failed: %v\n%s", err, out)
	}
	receipt := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" || strings.ToLower(key) != key || strings.ContainsAny(key, " \t") {
			t.Fatalf("split.sh printed %q, which is not one KEY=value line", line)
		}
		if _, repeated := receipt[key]; repeated {
			t.Fatalf("split.sh printed the key %q twice", key)
		}
		receipt[key] = value
	}
	return run, receipt
}

// split.sh prints exactly the receipt the recipe's split stage declares — one KEY=value a line,
// every declared field and a summary — and cuts the fixture into parts that each fit PART_LINES
// and the file bound, losing no source file, filing every test file with a part, and leaving
// docs and prose out of scope.
func TestAuditSplitFitsPartsUnderTheBudget(t *testing.T) {
	t.Parallel()
	cat, _ := Load(Sources{UseShippedSkills: true})
	sk, ok := cat.Get("audit")
	if !ok || sk.Recipe == nil {
		t.Fatalf("the shipped audit skill did not load with its recipe: %+v", cat.Skipped())
	}
	const partLines, partFiles = 100, 15
	workspace := auditFixture(t)
	run, receipt := runAuditSplit(t, workspace, ".", strconv.Itoa(partLines), "")

	wantKeys := []string{workflow.FieldSummary}
	for key := range sk.Recipe.Stages[0].Returns {
		wantKeys = append(wantKeys, key)
	}
	gotKeys := make([]string, 0, len(receipt))
	for key := range receipt {
		gotKeys = append(gotKeys, key)
	}
	slices.Sort(wantKeys)
	slices.Sort(gotKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("split.sh printed the keys %v, want the split stage's receipt %v", gotKeys, wantKeys)
	}
	if receipt["part_lines"] != strconv.Itoa(partLines) || receipt["focus"] != "none" {
		t.Errorf("receipt = %v, want part_lines=%d read from PART_LINES and focus=none", receipt, partLines)
	}

	parts := readLines(t, filepath.Join(run, "parts.txt"))
	if len(parts) < 2 || strconv.Itoa(len(parts)) != receipt["parts"] {
		t.Fatalf("parts.txt lists %v; want several parts and parts=%s", parts, receipt["parts"])
	}
	var covered, tested []string
	for _, part := range parts {
		sources := readLines(t, filepath.Join(part, "scope.txt"))
		total := 0
		for _, source := range sources {
			total += len(readLines(t, filepath.Join(workspace, source)))
		}
		if len(sources) > partFiles || total > partLines {
			t.Errorf("part %s holds %d files and %d lines, over the %d-file, %d-line bound",
				filepath.Base(part), len(sources), total, partFiles, partLines)
		}
		covered = append(covered, sources...)
		tested = append(tested, readLines(t, filepath.Join(part, "tests.txt"))...)
	}
	slices.Sort(covered)
	if src := readLines(t, filepath.Join(run, "src.txt")); !slices.Equal(covered, src) {
		t.Errorf("the parts cover %v, want every source file exactly once: %v", covered, src)
	}
	if !slices.Contains(tested, "internal/a/a1_test.go") {
		t.Errorf("the test file is filed with no part: %v", tested)
	}
	for _, name := range readLines(t, filepath.Join(run, "scope.txt")) {
		if strings.HasPrefix(name, "docs/") || strings.HasSuffix(name, ".md") || strings.Contains(name, "testdata/") {
			t.Errorf("scope.txt holds %q, which a code audit never reads", name)
		}
	}
	if conc := readLines(t, filepath.Join(run, "conc-parts.txt")); len(conc) != 1 {
		t.Errorf("conc-parts.txt = %v, want the one part holding b1.go's goroutine", conc)
	}
}

// With no PART_LINES in the environment the part bound is the window: half the engine's split
// budget at forty bytes a line, clamped to 200..8000, and 8000 when the budget is unknown.
func TestAuditSplitTakesItsBoundFromTheWindow(t *testing.T) {
	t.Parallel()
	workspace := auditFixture(t)
	for _, tc := range []struct {
		name, partLines, partBytes, want string
	}{
		{"PART_LINES wins", "150", "80000", "150"},
		{"derived from the budget", "", "80000", "1000"},
		{"clamped low", "", "400", "200"},
		{"clamped high", "", "10000000", "8000"},
		{"unknown window", "", "0", "8000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, receipt := runAuditSplit(t, workspace, "internal", tc.partLines, tc.partBytes)
			if receipt["part_lines"] != tc.want {
				t.Errorf("part_lines = %q, want %s", receipt["part_lines"], tc.want)
			}
		})
	}
}

// hostCanName reports whether this host's file system can hold name: Windows refuses `*`, `?`,
// `"` and newlines in a file name, so a fixture holding one is skipped there.
func hostCanName(name string) bool {
	return runtime.GOOS != "windows" || !strings.ContainsAny(name, "*?\"\n")
}

// writeSplitFixture writes files (slash-separated name to body) under root, skipping the names
// this host cannot hold, and returns the names it wrote, sorted.
func writeSplitFixture(t *testing.T, root string, files map[string]string) []string {
	t.Helper()
	var written []string
	for name, body := range files {
		if !hostCanName(name) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		written = append(written, name)
	}
	slices.Sort(written)
	return written
}

// splitTableLine is one split.txt line: `<part> <files> <lines> conc=<yes|no> group=<name>`,
// read from the right so a part name holding spaces stays whole.
var splitTableLine = regexp.MustCompile(`^(.*) ([0-9]+) ([0-9]+) conc=(yes|no) group=(.*)$`)

// split.sh reads every path as one name: a tree whose file and directory names hold spaces, `*`,
// `?` and `[`, sized over PART_LINES so the directory split, re-split and merge all run, lands
// every file in exactly one part, counts it in src=, and gives each part the line count wc -l
// gives its files. A name holding a newline is skipped, and no scope line names anything but a
// regular file.
func TestAuditSplitReadsPathsWithSpacesAndGlobCharacters(t *testing.T) {
	t.Parallel()
	const partLines = 100
	workspace := t.TempDir()
	lines := func(n int, extra string) string {
		return strings.Repeat("x := 1\n", n-1) + extra + "\n"
	}
	sources := writeSplitFixture(t, workspace, map[string]string{
		"a b.go":                          lines(20, "x := 1"),
		"x*y.go":                          lines(20, "x := 1"),
		"q?.go":                           lines(20, "x := 1"),
		"[z].go":                          lines(20, "x := 1"),
		"z.go":                            lines(20, "x := 1"),
		"dir with space/one.go":           lines(30, "go func() {}()"),
		"dir with space/two.go":           lines(30, "x := 1"),
		"dir with space/sub dir/three.go": lines(30, "x := 1"),
		"dir with space/sub dir/four.go":  lines(30, "x := 1"),
		"[g]/g1.go":                       lines(30, "x := 1"),
		"[g]/g2.go":                       lines(30, "x := 1"),
		"[g]/g3.go":                       lines(30, "x := 1"),
		"[g]/h/g4.go":                     lines(30, "x := 1"),
	})
	newlineName := "new\nline.go"
	if hostCanName(newlineName) {
		if err := os.WriteFile(filepath.Join(workspace, newlineName), []byte(lines(20, "x := 1")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run, receipt := runAuditSplit(t, workspace, ".", strconv.Itoa(partLines), "")

	if receipt["src"] != strconv.Itoa(len(sources)) || receipt["files"] != strconv.Itoa(len(sources)) {
		t.Errorf("receipt = %v, want files=src=%d, one for every fixture file", receipt, len(sources))
	}
	for _, name := range readLines(t, filepath.Join(run, "scope.txt")) {
		if info, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(name))); err != nil || !info.Mode().IsRegular() {
			t.Errorf("scope.txt names %q, which is no regular file (err %v)", name, err)
		}
	}

	tableLines := map[string]string{}
	for _, line := range readLines(t, filepath.Join(run, "split.txt")) {
		fields := splitTableLine.FindStringSubmatch(line)
		if fields == nil {
			t.Fatalf("split.txt line %q is not `<part> <files> <lines> conc= group=`", line)
		}
		tableLines[fields[1]] = fields[3]
	}
	parts := readLines(t, filepath.Join(run, "parts.txt"))
	if len(parts) < 4 {
		t.Fatalf("parts.txt lists %v; want the directory split to cut several parts", parts)
	}
	var covered []string
	for _, part := range parts {
		files := readLines(t, filepath.Join(part, "scope.txt"))
		total := 0
		for _, name := range files {
			data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(name)))
			if err != nil {
				t.Fatalf("part %s names %q, which does not read: %v", filepath.Base(part), name, err)
			}
			total += strings.Count(string(data), "\n")
		}
		name := strings.TrimPrefix(filepath.Base(part), "part-")
		if tableLines[name] != strconv.Itoa(total) {
			t.Errorf("split.txt gives part %q %q lines, want %d, the wc -l over its files %v", name, tableLines[name], total, files)
		}
		if total > partLines {
			t.Errorf("part %q holds %d lines, over the %d-line bound", name, total, partLines)
		}
		covered = append(covered, files...)
	}
	slices.Sort(covered)
	if !slices.Equal(covered, sources) {
		t.Errorf("the parts cover %q, want every fixture file exactly once: %q", covered, sources)
	}
	if conc := readLines(t, filepath.Join(run, "conc-parts.txt")); len(conc) != 1 ||
		!slices.Contains(readLines(t, filepath.Join(conc[0], "scope.txt")), "dir with space/one.go") {
		t.Errorf("conc-parts.txt = %v, want the one part holding `dir with space/one.go`'s goroutine", conc)
	}
}

// The scope input splits on whitespace into its entries and no entry is glob-expanded: a literal
// `*` names no file (there is none called `*`) and `[z].go` names itself, never `z.go`. A pin —
// the scope loop already ran under `set -f` before split.sh read paths one a line.
func TestAuditSplitNeverGlobsTheScope(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	writeSplitFixture(t, workspace, map[string]string{
		"[z].go":   "x := 1\n",
		"z.go":     "x := 1\n",
		"a/one.go": "x := 1\n",
	})
	run, receipt := runAuditSplit(t, workspace, "* [z].go", "", "")
	if scope := readLines(t, filepath.Join(run, "scope.txt")); !slices.Equal(scope, []string{"[z].go"}) {
		t.Errorf("scope.txt = %q for the scope `* [z].go`, want only [z].go", scope)
	}
	if receipt["files"] != "1" {
		t.Errorf("receipt = %v, want files=1", receipt)
	}
}

// In a git repository split.sh lists through git unquoted, so a name git would quote by default —
// a non-ASCII `café.go`, a `q"t.go` — reaches scope.txt as it is on disk.
func TestAuditSplitListsGitNamesUnquoted(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this host")
	}
	workspace := t.TempDir()
	sources := writeSplitFixture(t, workspace, map[string]string{
		"café.go":  "x := 1\n",
		"q\"t.go":  "x := 1\n",
		"plain.go": "x := 1\n",
	})
	// The fixture's own repository alone: no inherited GIT_* (a hook's GIT_DIR, GIT_INDEX_FILE)
	// and no system or global config.
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "PART_LINES=") {
			env = append(env, entry)
		}
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	initRepo := exec.Command("git", "init", "-q")
	initRepo.Dir = workspace
	initRepo.Env = env
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	run, _ := runAuditSplitWithEnv(t, workspace, ".", "", env)
	if scope := readLines(t, filepath.Join(run, "scope.txt")); !slices.Equal(scope, sources) {
		t.Errorf("scope.txt = %q, want every git-listed file as named on disk: %q", scope, sources)
	}
}

// readLines is a file's non-blank lines.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
