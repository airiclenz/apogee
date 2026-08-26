package skills

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestProviderReloadPicksUpNewSkill is the core of the live-refresh contract: a Provider serves
// the catalog as it stood at construction, and Reload re-scans the same dirs so a skill added
// after launch becomes visible through BOTH consumer seams — List (the merged "/" menu) and
// ResolveSkills (the agent loop) — without rebuilding the Provider.
func TestProviderReloadPicksUpNewSkill(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, "skills")
	writeSkill(t, skillsDir, "alpha", "---\nid: alpha\nsummary: the alpha skill\n---\nbody A")

	p := NewProvider(Sources{Home: home})

	if got := len(p.List()); got != 1 {
		t.Fatalf("initial List() = %d skills, want 1", got)
	}
	if _, ok := p.Get("beta"); ok {
		t.Fatal("beta resolved before it was created")
	}

	// Add a new skill on disk AFTER the provider was built — it must not be visible yet.
	writeSkill(t, skillsDir, "beta", "---\nid: beta\nsummary: the beta skill\n---\nbody B")
	if _, ok := p.Get("beta"); ok {
		t.Fatal("beta became visible without a Reload; the snapshot must be stable until reloaded")
	}

	if err := p.Reload(); err != nil {
		t.Fatalf("Reload soft error: %v", err)
	}

	if got := len(p.List()); got != 2 {
		t.Fatalf("after Reload List() = %d skills, want 2", got)
	}
	if _, ok := p.Get("beta"); !ok {
		t.Error("beta not visible via Get after Reload")
	}
	resolved := p.ResolveSkills([]string{"beta"})
	if len(resolved) != 1 || resolved[0].Body != "body B" {
		t.Errorf("ResolveSkills([beta]) = %+v, want the reloaded body B — the loop's seam must see the fresh skill too", resolved)
	}
}

// TestProviderReloadReflectsEdits pins that Reload picks up an EDIT to an existing skill (not
// just additions): editing a SKILL.md and reloading swaps in the new body/summary.
func TestProviderReloadReflectsEdits(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, "skills")
	writeSkill(t, skillsDir, "alpha", "---\nid: alpha\nsummary: original\n---\nOLD BODY")

	p := NewProvider(Sources{Home: home})
	if a, _ := p.Get("alpha"); a.Body != "OLD BODY" {
		t.Fatalf("initial body = %q, want OLD BODY", a.Body)
	}

	// Overwrite the same skill folder, then reload.
	writeSkill(t, skillsDir, "alpha", "---\nid: alpha\nsummary: updated\n---\nNEW BODY")
	if err := p.Reload(); err != nil {
		t.Fatalf("Reload soft error: %v", err)
	}

	a, ok := p.Get("alpha")
	if !ok {
		t.Fatal("alpha vanished after Reload")
	}
	if a.Body != "NEW BODY" {
		t.Errorf("body after Reload = %q, want NEW BODY", a.Body)
	}
	if a.Summary != "updated" {
		t.Errorf("summary after Reload = %q, want updated", a.Summary)
	}
}

// TestProviderAlwaysUsable pins the always-non-nil contract: a Provider over missing dirs serves
// an empty (not nil) catalog and Reload stays soft, so callers may drop the error.
func TestProviderAlwaysUsable(t *testing.T) {
	p := NewProvider(Sources{Home: filepath.Join(t.TempDir(), "nope")})
	if p.List() == nil {
		t.Error("List() returned nil; an empty catalog must be a non-nil empty slice or usable list")
	}
	if got := len(p.List()); got != 0 {
		t.Errorf("empty provider List() = %d, want 0", got)
	}
	if err := p.Reload(); err != nil {
		t.Errorf("Reload over a missing dir errored: %v", err)
	}
}

// TestProviderSetSourcesLandsOnTheNextReload is the live half of the source layering: WHICH dirs are
// sources is itself configuration (`use-project-skills:` gates the workspace's bare skills/ folder),
// and a Provider whose sources were frozen at construction could only answer a change of it by being
// rebuilt — which would strand the loop and the "/" menu on two different catalogues. SetSources
// re-points the scan; the catalogue in force is untouched until the Reload that is the caller's own
// decision, exactly as an edited skill on disk is.
func TestProviderSetSourcesLandsOnTheNextReload(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "global",
		"---\nid: global\nsummary: the library skill\n---\nbody G")
	writeSkill(t, filepath.Join(workspace, "skills"), "project",
		"---\nid: project\nsummary: the bare project-folder skill\n---\nbody P")

	src := Sources{Home: home, Workspace: workspace, UseProjectSkills: true}
	p := NewProvider(src)
	if _, ok := p.Get("project"); !ok {
		t.Fatal("the project skill is not discovered with the flag on; the fixture proves nothing")
	}

	src.UseProjectSkills = false
	p.SetSources(src)
	if _, ok := p.Get("project"); !ok {
		t.Error("SetSources dropped the project skill on its own; installing the new sources is not a scan")
	}

	if err := p.Reload(); err != nil {
		t.Fatalf("Reload soft error: %v", err)
	}
	if _, ok := p.Get("project"); ok {
		t.Error("the project skill survived a reload with the flag off; the scan used the old sources")
	}
	if len(p.ResolveSkills([]string{"project"})) != 0 {
		t.Error("the loop's seam still resolves the project skill; both consumers read the one snapshot")
	}
	if _, ok := p.Get("global"); !ok {
		t.Error("the library skill went with it; only the workspace's bare skills/ folder is gated")
	}
}

// TestProviderSourceDirsFollowSetSources pins the read-root seam's live-ness at its source: the
// host mounts SourceDirs as the read tools' extra read-only roots (domain.Config.ExtraReadRoots),
// and a mount frozen at construction would leave the model reading a dir the catalogue no longer
// scans — or refusing one it does. SourceDirs answers off the CURRENT sources, so a
// `use-project-skills` flip moves the mount with no Reload and no re-wiring, and it needs no
// catalogue at all: it reports where skills come from, not which ones loaded.
func TestProviderSourceDirsFollowSetSources(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "skills")

	src := Sources{Home: home, Workspace: workspace, UseProjectSkills: true}
	p := NewProvider(src)

	if got, want := p.SourceDirs(), sourceDirs(src); !slices.Equal(got, want) {
		t.Fatalf("SourceDirs() = %v, want the layered scan list %v", got, want)
	}
	if !slices.Contains(p.SourceDirs(), projectDir) {
		t.Fatalf("SourceDirs() = %v, missing the bare project folder the flag admits", p.SourceDirs())
	}

	src.UseProjectSkills = false
	p.SetSources(src)

	if slices.Contains(p.SourceDirs(), projectDir) {
		t.Errorf("SourceDirs() = %v still lists the project folder after the flag went off; the mount is stale",
			p.SourceDirs())
	}
	if !slices.Contains(p.SourceDirs(), filepath.Join(home, "skills")) {
		t.Errorf("SourceDirs() = %v dropped the global library; only the gated dir moves", p.SourceDirs())
	}
}

// TestProviderReadRootsFollowSetSources pins the read-root seam's live-ness where the host now
// takes it: ReadRoots, not SourceDirs. A mount frozen at construction would leave the model reading
// a dir the catalogue no longer scans — or refusing one it does — so a `use-project-skills` flip
// must move it with no Reload and no re-wiring, exactly as it moves the display list.
func TestProviderReadRootsFollowSetSources(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	projectRoot := filepath.Join(realDir(t, workspace), "skills")

	src := Sources{Home: home, Workspace: workspace, UseProjectSkills: true}
	p := NewProvider(src)

	if got, want := p.ReadRoots(), readRoots(src); !slices.Equal(got, want) {
		t.Fatalf("ReadRoots() = %v, want the layered mount list %v", got, want)
	}
	if !slices.Contains(p.ReadRoots(), projectRoot) {
		t.Fatalf("ReadRoots() = %v, missing the bare project folder the flag admits", p.ReadRoots())
	}

	src.UseProjectSkills = false
	p.SetSources(src)

	if slices.Contains(p.ReadRoots(), projectRoot) {
		t.Errorf("ReadRoots() = %v still mounts the project folder after the flag went off; the mount is stale",
			p.ReadRoots())
	}
	if !slices.Contains(p.ReadRoots(), filepath.Join(realDir(t, home), "skills")) {
		t.Errorf("ReadRoots() = %v dropped the global library; only the gated dir moves", p.ReadRoots())
	}
}
