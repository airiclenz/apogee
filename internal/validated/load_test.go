package validated

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ids(names ...string) []domain.MechanismID {
	out := make([]domain.MechanismID, len(names))
	for i, n := range names {
		out[i] = domain.MechanismID(n)
	}
	return out
}

func TestLoadUserDir_MissingDirIsEmpty(t *testing.T) {
	entries, warnings := LoadUserDir(filepath.Join(t.TempDir(), "absent"))
	if entries != nil || warnings != nil {
		t.Fatalf("missing dir: want nil/nil, got %v / %v", entries, warnings)
	}
}

func TestLoadUserDir_SkipsDefectsKeepsGood(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "good.json", `{"version":1,"key":"m1","set":["a"]}`)
	writeFile(t, dir, "bad.json", `{"version":1,`)
	writeFile(t, dir, "newer.json", `{"version":9,"key":"m2","set":["a"]}`)
	writeFile(t, dir, "notes.txt", `ignored — not a .json`)

	entries, warnings := LoadUserDir(dir)
	if len(entries) != 1 || entries[0].Key != "m1" {
		t.Fatalf("want the one good entry, got %+v", entries)
	}
	if entries[0].Source != SourceUser {
		t.Fatalf("want Source stamped %q, got %q", SourceUser, entries[0].Source)
	}
	if len(warnings) != 2 {
		t.Fatalf("want 2 warnings (bad.json, newer.json), got %v", warnings)
	}
	for _, w := range warnings {
		if !strings.Contains(w, "skipping validated-set entry") {
			t.Fatalf("warning missing skip wording: %q", w)
		}
	}
}

func TestLoadUserDir_SortedOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.json", `{"version":1,"key":"kb","set":["a"]}`)
	writeFile(t, dir, "a.json", `{"version":1,"key":"ka","set":["a"]}`)

	entries, _ := LoadUserDir(dir)
	if len(entries) != 2 || entries[0].Key != "ka" || entries[1].Key != "kb" {
		t.Fatalf("want sorted-by-filename order [ka kb], got %+v", entries)
	}
}

func TestMerge_UserWinsShippedSilently(t *testing.T) {
	shipped := []Entry{{Key: "m", Set: ids("a"), Source: SourceShipped}}
	user := []Entry{{Key: "m", Set: ids("b"), Source: SourceUser}}

	merged, warnings := Merge(shipped, user)
	if got := merged["m"]; got.Source != SourceUser || len(got.Set) != 1 || got.Set[0] != "b" {
		t.Fatalf("want the user entry to win, got %+v", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("user-over-shipped is the designed precedence, want no warning, got %v", warnings)
	}
}

func TestMerge_UserDuplicateWarnsLaterWins(t *testing.T) {
	user := []Entry{
		{Key: "m", Set: ids("a"), Source: SourceUser},
		{Key: "m", Set: ids("b"), Source: SourceUser},
	}

	merged, warnings := Merge(nil, user)
	if got := merged["m"]; got.Set[0] != "b" {
		t.Fatalf("want the later user entry to win, got %+v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "duplicate validated-set entries") {
		t.Fatalf("want one duplicate warning, got %v", warnings)
	}
}
