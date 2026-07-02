package skills

import (
	"path/filepath"
	"testing"
)

// TestProviderReloadPicksUpNewSkill is the core of the live-refresh contract: a Provider serves
// the catalog as it stood at construction, and Reload re-scans the same dirs so a skill added
// after launch becomes visible through BOTH consumer seams — List (the /skill picker) and
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
