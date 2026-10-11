package skills

import (
	"path/filepath"
	"slices"
	"testing"
)

// The repository's own skills (ADR 0098 §2) are canonical under <repo>/.apogee/skills and load
// as project skills from the Project root alone: that anchor is never flag-gated —
// UseProjectSkills gates only <Workspace>/skills/. The two ids are pinned because the owner's
// personal /cut-release and /release-notes stand aside only when the workspace ships a skill
// whose id starts with "release-" or "release-notes-", so renaming either breaks that guard.
func TestRepoReleaseSkillsLoad(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	cat, err := Load(Sources{ProjectRoot: root})

	if err != nil {
		t.Fatalf("Load soft error: %v", err)
	}
	if skipped := cat.Skipped(); len(skipped) != 0 {
		t.Fatalf("Skipped() = %+v, want empty", skipped)
	}
	var ids []string
	for _, s := range cat.List() {
		ids = append(ids, s.ID)
		if s.Body == "" {
			t.Errorf("skill %q has an empty Body", s.ID)
		}
	}
	slices.Sort(ids)
	if want := []string{"release-apogee", "release-notes-apogee"}; !slices.Equal(ids, want) {
		t.Errorf("repo skill ids = %v, want %v", ids, want)
	}
}
