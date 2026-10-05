package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// storeClock is the fixed moment the store tests create workflows at.
var storeClock = time.Date(2026, 9, 27, 14, 3, 9, 0, time.UTC)

// newTestStore returns a store under a fresh scratch dir.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// samplePlan is a one-fanout plan with a typed receipt, enough to exercise every file.
func samplePlan(name string) Plan {
	return Plan{Name: name, Stages: []Stage{{
		Name: "find", Kind: StageFanout, Task: "audit {item}",
		Over: &ItemSource{List: []string{"a.go", "b.go"}}, Returns: ReceiptSpec{"findings": "int"},
	}}}
}

// createWorkflow creates samplePlan(name) under hash and fails the test on error.
func createWorkflow(t *testing.T, store *Store, name, hash string, now time.Time) RunStatus {
	t.Helper()
	status, err := store.Create(samplePlan(name), hash, now)
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return status
}

// sampleKey is an item key for a fixed brief and item with no context files.
func sampleKey(t *testing.T, label string) string {
	t.Helper()
	key, err := ItemKey("audit {item}", Item{Label: label, Units: []string{label}}, nil, fstest.MapFS{})
	if err != nil {
		t.Fatalf("ItemKey: %v", err)
	}
	return key
}

func TestNewStoreRefusesAnEmptyOrRelativeRoot(t *testing.T) {
	t.Parallel()
	for _, root := range []string{"", "scratch", "./scratch", "../scratch"} {
		if _, err := NewStore(root); err == nil {
			t.Errorf("NewStore(%q) err = nil, want a refusal", root)
		}
	}
}

func TestCreateLaysOutTheFolder(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	status := createWorkflow(t, store, "Audit internal/", "hash-1", storeClock)

	if status.ID != "20260927-140309-audit-internal" {
		t.Errorf("id = %q, want 20260927-140309-audit-internal", status.ID)
	}
	dir, err := store.Dir(status.ID)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if filepath.Dir(dir) != store.Root() || filepath.Base(store.Root()) != "workflows" {
		t.Errorf("folder %q is not directly under <scratch>/workflows", dir)
	}
	assertPerm(t, dir, 0o700)
	assertPerm(t, filepath.Join(dir, "plan.json"), 0o600)
	assertPerm(t, filepath.Join(dir, "status.json"), 0o600)
	if len(status.Stages) != 1 || status.Stages[0].Name != "find" || status.Stages[0].Phase != PhasePending {
		t.Errorf("stages = %+v, want the one pending fanout", status.Stages)
	}
}

// assertPerm fails when path's permission bits are not want.
func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	plan := samplePlan("audit")
	created, err := store.Create(plan, "hash-1", storeClock)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	key := sampleKey(t, "a.go")
	receipt := Receipt{Status: StatusOK, Summary: "two findings", Fields: map[string]any{"findings": float64(2)}}
	created.Phase = PhaseRunning
	created.Stages[0].Items = []ItemStatus{{Key: key, Label: "a.go", Phase: PhaseDone, Receipt: &receipt}}
	transcript := []domain.Message{{Role: domain.RoleUser, Content: "audit a.go"}, {Role: domain.RoleAssistant, Content: "done"}}

	if err := store.WriteStatus(created); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	if err := store.WriteReceipt(created.ID, key, receipt); err != nil {
		t.Fatalf("WriteReceipt: %v", err)
	}
	if err := store.WriteTranscript(created.ID, key, transcript); err != nil {
		t.Fatalf("WriteTranscript: %v", err)
	}
	if err := store.WriteFile(created.ID, "stages/find/report.md", []byte("# report\n")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gotPlan, err := store.ReadPlan(created.ID)
	if err != nil || !reflect.DeepEqual(gotPlan, plan) {
		t.Errorf("ReadPlan = %+v, %v; want %+v", gotPlan, err, plan)
	}
	gotStatus, err := store.ReadStatus(created.ID)
	if err != nil || !reflect.DeepEqual(gotStatus, created) {
		t.Errorf("ReadStatus = %+v, %v; want %+v", gotStatus, err, created)
	}
	gotReceipt, found, err := store.ReadReceipt(created.ID, key)
	if err != nil || !found || !reflect.DeepEqual(gotReceipt, receipt) {
		t.Errorf("ReadReceipt = %+v, %v, %v; want %+v", gotReceipt, found, err, receipt)
	}
	dir, _ := store.Dir(created.ID)
	lines, err := os.ReadFile(filepath.Join(dir, "items", key, "transcript.jsonl"))
	if err != nil || strings.Count(string(lines), "\n") != len(transcript) {
		t.Errorf("transcript.jsonl = %q, %v; want %d lines", lines, err, len(transcript))
	}
	report, err := os.ReadFile(filepath.Join(dir, "stages", "find", "report.md"))
	if err != nil || string(report) != "# report\n" {
		t.Errorf("report.md = %q, %v", report, err)
	}
}

func TestReadReceiptOfAnUnfinishedItemIsNotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)

	_, found, err := store.ReadReceipt(status.ID, sampleKey(t, "a.go"))

	if err != nil || found {
		t.Errorf("ReadReceipt of an item with no receipt = found %v, err %v; want not found, nil", found, err)
	}
}

func TestAdoptItemRenamesTheOlderSchemeFolder(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)
	fromKey, toKey := sampleKey(t, "a.go"), sampleKey(t, "b.go")
	if err := store.WriteReceipt(status.ID, fromKey, *okReceipt("a.go")); err != nil {
		t.Fatalf("WriteReceipt: %v", err)
	}
	if err := store.WriteTranscript(status.ID, toKey, nil); err != nil {
		t.Fatalf("WriteTranscript: %v", err)
	}

	err := store.AdoptItem(status.ID, fromKey, toKey)

	if err != nil {
		t.Fatalf("AdoptItem over an unfinished folder: %v", err)
	}
	receipt, found, err := store.ReadReceipt(status.ID, toKey)
	if err != nil || !found || receipt.Summary != "checked a.go" {
		t.Errorf("receipt under the adopted key = %+v, found %v, err %v; want the older folder's", receipt, found, err)
	}
	dir, _ := store.Dir(status.ID)
	if _, err := os.Stat(filepath.Join(dir, itemsDirName, fromKey)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("older folder after adoption: stat err = %v, want it gone", err)
	}
	if _, err := os.Stat(filepath.Join(dir, itemsDirName, toKey, transcriptName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unfinished work under the adopted key: stat err = %v, want it removed", err)
	}
}

func TestAdoptItemNeverOverwritesAFinishedReceipt(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusOK, StatusPartial} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			store := newTestStore(t)
			workflow := createWorkflow(t, store, "audit", "hash-1", storeClock)
			fromKey, toKey := sampleKey(t, "a.go"), sampleKey(t, "b.go")
			for key, summary := range map[string]string{fromKey: "older", toKey: "current"} {
				if err := store.WriteReceipt(workflow.ID, key, Receipt{Status: status, Summary: summary}); err != nil {
					t.Fatalf("WriteReceipt: %v", err)
				}
			}

			err := store.AdoptItem(workflow.ID, fromKey, toKey)

			if !errors.Is(err, ErrItemFinished) {
				t.Errorf("AdoptItem onto a %s receipt: err = %v, want ErrItemFinished", status, err)
			}
			for key, want := range map[string]string{fromKey: "older", toKey: "current"} {
				if receipt, _, _ := store.ReadReceipt(workflow.ID, key); receipt.Summary != want {
					t.Errorf("receipt under %s after a refused adoption = %q, want %q untouched", key[:8], receipt.Summary, want)
				}
			}
		})
	}
}

func TestAdoptItemRefusesInvalidKeys(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)
	key := sampleKey(t, "a.go")

	for _, pair := range [][2]string{
		{"../x", key}, {key, "../x"}, {"", key}, {key, strings.Repeat("A", 64)}, {key, key},
	} {
		if err := store.AdoptItem(status.ID, pair[0], pair[1]); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("AdoptItem(%q, %q) err = %v, want ErrInvalidKey", pair[0], pair[1], err)
		}
	}
}

// ReadItemTranscript hands back what WriteTranscript saved, in order; an item that saved none is
// not found, and a key that is not an item key is refused before any path is built from it.
func TestReadItemTranscript(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)
	dir, _ := store.Dir(status.ID)
	key := sampleKey(t, "a.go")
	transcript := []domain.Message{{Role: domain.RoleUser, Content: "audit a.go"}, {Role: domain.RoleAssistant, Content: "done"}}
	if err := store.WriteTranscript(status.ID, key, transcript); err != nil {
		t.Fatalf("WriteTranscript: %v", err)
	}

	got, found, err := ReadItemTranscript(dir, key)
	if err != nil || !found || !reflect.DeepEqual(got, transcript) {
		t.Errorf("ReadItemTranscript = %+v, %v, %v; want %+v", got, found, err, transcript)
	}
	if _, found, err := ReadItemTranscript(dir, sampleKey(t, "b.go")); err != nil || found {
		t.Errorf("ReadItemTranscript of an item with none = found %v, err %v; want not found, nil", found, err)
	}
	if _, _, err := ReadItemTranscript(dir, "../escape"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("ReadItemTranscript of a bad key = %v, want ErrInvalidKey", err)
	}
}

// ItemOutputPath names the path the stage's `out:` rendered for the item, or output.md in the
// item's own folder when the stage sets none.
func TestItemOutputPath(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	plan := samplePlan("audit")
	plan.Stages = append(plan.Stages, Stage{Name: "check", Kind: StageFanout, Task: "check {item}", Out: "notes/{item}.md",
		Over: &ItemSource{List: []string{"a.go"}}})
	status, err := store.Create(plan, "hash-1", storeClock)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dir, _ := store.Dir(status.ID)
	key := sampleKey(t, "a.go")

	for _, tc := range []struct {
		stage string
		want  string
	}{
		{"find", filepath.Join(dir, "items", key, "output.md")},
		{"check", "notes/a.go.md"},
	} {
		if got, err := ItemOutputPath(dir, tc.stage, key, "a.go"); err != nil || got != tc.want {
			t.Errorf("ItemOutputPath(%s) = %q, %v; want %q", tc.stage, got, err, tc.want)
		}
	}
	if _, err := ItemOutputPath(dir, "find", "nope", "a.go"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("ItemOutputPath of a bad key = %v, want ErrInvalidKey", err)
	}
}

func TestItemKey(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"CONTEXT.md": {Data: []byte("the terms")}}
	item := Item{Label: "a.go", Units: []string{"a.go"}}
	key := func(t *testing.T, brief string, item Item, fsys fstest.MapFS) string {
		t.Helper()
		got, err := ItemKey(brief, item, []string{"CONTEXT.md"}, fsys)
		if err != nil {
			t.Fatalf("ItemKey: %v", err)
		}
		return got
	}
	base := key(t, "audit {item}", item, fsys)

	if again := key(t, "audit {item}", item, fsys); again != base {
		t.Errorf("same brief, item and context gave %q then %q", base, again)
	}
	if !isValidKey(base) {
		t.Errorf("key %q is not a lower-case hex SHA-256", base)
	}
	changed := fstest.MapFS{"CONTEXT.md": {Data: []byte("the terms, revised")}}
	if other := key(t, "audit {item}", item, changed); other == base {
		t.Error("a changed context file kept the same key")
	}
	if other := key(t, "review {item}", item, fsys); other == base {
		t.Error("a changed brief kept the same key")
	}
	if other := key(t, "audit {item}", Item{Label: "b.go", Units: []string{"b.go"}}, fsys); other == base {
		t.Error("a different item kept the same key")
	}
	if _, err := ItemKey("audit", item, []string{"missing.md"}, fsys); err == nil {
		t.Error("ItemKey with an unreadable context file err = nil, want an error")
	}
}

func TestPlanHash(t *testing.T) {
	t.Parallel()
	items := []Item{{Label: "a.go", Units: []string{"a.go"}}}
	hash := func(plan Plan, inputs map[string]string, items []Item) string {
		t.Helper()
		got, err := PlanHash(plan, inputs, items)
		if err != nil {
			t.Fatalf("PlanHash: %v", err)
		}
		return got
	}
	base := hash(samplePlan("audit"), map[string]string{"path": "internal/", "depth": "2"}, items)

	if again := hash(samplePlan("audit"), map[string]string{"depth": "2", "path": "internal/"}, items); again != base {
		t.Error("the same plan, inputs and items hashed differently")
	}
	if other := hash(samplePlan("audit"), map[string]string{"path": "cmd/", "depth": "2"}, items); other == base {
		t.Error("different bound inputs hashed the same")
	}
	if other := hash(samplePlan("audit"), map[string]string{"path": "internal/", "depth": "2"}, nil); other == base {
		t.Error("a different item list hashed the same")
	}
	if other := hash(samplePlan("review"), map[string]string{"path": "internal/", "depth": "2"}, items); other == base {
		t.Error("a different plan hashed the same")
	}
}

func TestItemStatusNameRoundTripsAndLeavesKeyAndHashAlone(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	created := createWorkflow(t, store, "audit", "hash-1", storeClock)
	item := Item{Label: "/w/wf-1/part-a", Units: []string{"/w/wf-1/part-a"}}
	key := sampleKey(t, item.Label)
	created.Stages[0].Items = []ItemStatus{
		{Key: key, Label: item.Label, Name: "part-a", Phase: PhaseDone},
		{Label: "flags", Phase: PhaseDone},
	}

	if err := store.WriteStatus(created); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	gotStatus, err := store.ReadStatus(created.ID)
	dir, _ := store.Dir(created.ID)
	raw, readErr := os.ReadFile(filepath.Join(dir, "status.json"))
	gotKey, keyErr := ItemKey("audit {item}", item, nil, fstest.MapFS{})
	gotHash, hashErr := PlanHash(samplePlan("audit"), nil, []Item{item})

	if err != nil || !reflect.DeepEqual(gotStatus, created) {
		t.Errorf("ReadStatus = %+v, %v; want %+v", gotStatus, err, created)
	}
	var onDisk struct {
		Stages []struct {
			Items []map[string]any `json:"items"`
		} `json:"stages"`
	}
	if readErr != nil || json.Unmarshal(raw, &onDisk) != nil {
		t.Fatalf("status.json = %s, %v; want it readable", raw, readErr)
	}
	lines := onDisk.Stages[0].Items
	if _, unnamedHasName := lines[1]["name"]; lines[0]["name"] != "part-a" || unnamedHasName {
		t.Errorf("status.json item lines = %v; want \"name\": \"part-a\" on the named line and no name on the other", lines)
	}
	// Pinned at the base before ItemStatus.Name existed: the short name is display only.
	if want := "bfc0c4a939f645150fe89f4d0b573e60103b318ae69b3d903d6e6ce005a744d1"; keyErr != nil || gotKey != want {
		t.Errorf("ItemKey = %q, %v; want %q", gotKey, keyErr, want)
	}
	if want := "e51ae7d97917f094ec4d2e4440bf95263c2ed47f6817ddf6fc432bdc1761a65d"; hashErr != nil || gotHash != want {
		t.Errorf("PlanHash = %q, %v; want %q", gotHash, hashErr, want)
	}
}

func TestFindReturnsTheWorkflowWithThePlanHash(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	createWorkflow(t, store, "other", "hash-other", storeClock)
	older := createWorkflow(t, store, "audit", "hash-1", storeClock)
	older.Phase = PhaseStopped
	if err := store.WriteStatus(older); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, found, err := store.Find("hash-1")

	if err != nil || !found || got.ID != older.ID || got.Phase != PhaseStopped {
		t.Errorf("Find = %+v, %v, %v; want the stopped workflow %q", got, found, err, older.ID)
	}
}

func TestFindPrefersTheNewestMatch(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	createWorkflow(t, store, "audit", "hash-1", storeClock)
	newer := createWorkflow(t, store, "audit", "hash-1", storeClock.Add(time.Minute))

	got, found, err := store.Find("hash-1")

	if err != nil || !found || got.ID != newer.ID {
		t.Errorf("Find = %q, %v, %v; want the newer %q", got.ID, found, err, newer.ID)
	}
}

func TestFindWithNoMatch(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	if _, found, err := store.Find("hash-1"); err != nil || found {
		t.Errorf("Find in a never-written store = found %v, err %v; want not found, nil", found, err)
	}
	createWorkflow(t, store, "audit", "hash-1", storeClock)

	if _, found, err := store.Find("hash-2"); err != nil || found {
		t.Errorf("Find of an unknown hash = found %v, err %v; want not found, nil", found, err)
	}
}

func TestTwoCreatesInOneSecondMakeTwoFolders(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	first := createWorkflow(t, store, "audit", "hash-1", storeClock)
	second := createWorkflow(t, store, "audit", "hash-1", storeClock)
	third := createWorkflow(t, store, "audit", "hash-1", storeClock)

	want := []string{"20260927-140309-audit", "20260927-140309-audit-2", "20260927-140309-audit-3"}
	if got := []string{first.ID, second.ID, third.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestSlugStaysUnderTheRoot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want string
	}{
		{"", "workflow"},
		{"../evil", "evil"},
		{"../../..", "workflow"},
		{"/etc/apogee-victim", "etc-apogee-victim"},
		{`sub\dir`, "sub-dir"},
		{"two\nlines", "two-lines"},
		{"nul\x00byte", "nul-byte"},
		{"session\u202egpj.exe", "session-gpj-exe"},
		{"Über Größe", "ber-gr-e"},
		{strings.Repeat("x", 100), strings.Repeat("x", slugMaxLen)},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			store := newTestStore(t)

			status := createWorkflow(t, store, tc.name, "hash-1", storeClock)

			if want := "20260927-140309-" + tc.want; status.ID != want {
				t.Errorf("name %q gave id %q, want %q", tc.name, status.ID, want)
			}
			entries, err := os.ReadDir(filepath.Dir(store.Root()))
			if err != nil || len(entries) != 1 || entries[0].Name() != "workflows" {
				t.Errorf("scratch dir holds %v (err %v), want only workflows/", entries, err)
			}
		})
	}
}

func TestStoreRefusesUnsafeIDsKeysAndNames(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)
	key := sampleKey(t, "a.go")

	for _, id := range []string{"", "..", "../evil", "20260927-140309-../x", "20260927-140309-a/b", "/etc", "x"} {
		if _, err := store.Dir(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Dir(%q) err = %v, want ErrInvalidID", id, err)
		}
		if err := store.WriteReceipt(id, key, Receipt{}); !errors.Is(err, ErrInvalidID) {
			t.Errorf("WriteReceipt(id %q) err = %v, want ErrInvalidID", id, err)
		}
	}
	for _, badKey := range []string{"", "..", "../x", strings.Repeat("A", 64), key[:63]} {
		if err := store.WriteReceipt(status.ID, badKey, Receipt{}); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("WriteReceipt(key %q) err = %v, want ErrInvalidKey", badKey, err)
		}
	}
	for _, name := range []string{"", "..", "../escape.md", "/etc/passwd", `a\b`, "a/../../b"} {
		if err := store.WriteFile(status.ID, name, []byte("x")); !errors.Is(err, ErrInvalidName) {
			t.Errorf("WriteFile(name %q) err = %v, want ErrInvalidName", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(store.Root()))
	if err != nil || len(entries) != 1 {
		t.Errorf("scratch dir holds %v (err %v), want only workflows/", entries, err)
	}
}

func TestFailedWritesLeaveNoPartialFile(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	status := createWorkflow(t, store, "audit", "hash-1", storeClock)
	key := sampleKey(t, "a.go")
	dir, _ := store.Dir(status.ID)
	// A directory where the output file should go makes the final rename fail after the temp
	// file has been written in full.
	blocker := filepath.Join(dir, "report.md")
	if err := os.MkdirAll(filepath.Join(blocker, "inside"), 0o700); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	unencodable := Receipt{Status: StatusOK, Summary: "x", Fields: map[string]any{"bad": make(chan int)}}

	renameErr := store.WriteFile(status.ID, "report.md", []byte("# report\n"))
	encodeErr := store.WriteReceipt(status.ID, key, unencodable)

	if renameErr == nil || encodeErr == nil {
		t.Fatalf("errors = %v, %v; want both writes to fail", renameErr, encodeErr)
	}
	assertNoTempFiles(t, dir)
	if _, found, err := store.ReadReceipt(status.ID, key); found || err != nil {
		t.Errorf("a failed receipt write left a receipt (found %v, err %v)", found, err)
	}
}

// assertNoTempFiles fails when any atomic-write temp file is left anywhere under root.
func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// subAgentArgs is the sub_agent call a subAgentPlan's item runs, compact as plan.json keeps it.
const subAgentArgs = `{"task":"survey the repo","name":"surveyor"}`

// subAgentPlan is a one-item plan on the sub_agent path, as a background sub_agent's is (ADR 0094).
// Its stage sets an `out:`, which the sub_agent path leaves unread: the item's report is kept in
// its own folder.
func subAgentPlan() Plan {
	return Plan{Name: "survey", Stages: []Stage{{
		Name: "sub_agent", Kind: StageFanout, Task: "survey the repo", Out: "notes/{item}.md",
		Over:     &ItemSource{List: []string{"surveyor"}},
		SubAgent: json.RawMessage(subAgentArgs),
	}}}
}

func TestRunStatusOriginRoundTripsAndStaysOffOtherFolders(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	created, err := store.Create(subAgentPlan(), "hash-sub", storeClock)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gotStatus, statusErr := store.ReadStatus(created.ID)
	gotPlan, planErr := store.ReadPlan(created.ID)
	other := createWorkflow(t, store, "audit", "hash-1", storeClock)
	otherDir, _ := store.Dir(other.ID)
	otherRaw, otherErr := os.ReadFile(filepath.Join(otherDir, "status.json"))

	if created.Origin != OriginSubAgent {
		t.Errorf("created origin = %q, want %q", created.Origin, OriginSubAgent)
	}
	if statusErr != nil || !reflect.DeepEqual(gotStatus, created) {
		t.Errorf("ReadStatus = %+v, %v; want %+v", gotStatus, statusErr, created)
	}
	// plan.json is written indented, so the arguments come back re-laid but the same JSON.
	var gotArgs bytes.Buffer
	if planErr != nil || len(gotPlan.Stages) != 1 || json.Compact(&gotArgs, gotPlan.Stages[0].SubAgent) != nil || gotArgs.String() != subAgentArgs {
		t.Errorf("ReadPlan = %+v, %v; want the stage's sub_agent arguments %s back", gotPlan, planErr, subAgentArgs)
	}
	if other.Origin != "" || otherErr != nil || strings.Contains(string(otherRaw), `"origin"`) {
		t.Errorf("a plan off the sub_agent path: origin %q, status.json %s (%v); want no origin at all", other.Origin, otherRaw, otherErr)
	}
}

func TestPlanHashOfAPlanOffTheSubAgentPathIsUnchanged(t *testing.T) {
	t.Parallel()
	item := Item{Label: "/w/wf-1/part-a", Units: []string{"/w/wf-1/part-a"}}
	withSubAgent := samplePlan("audit")
	withSubAgent.Stages[0].SubAgent = json.RawMessage(subAgentArgs)

	got, err := PlanHash(samplePlan("audit"), nil, []Item{item})
	changed, changedErr := PlanHash(withSubAgent, nil, []Item{item})
	canonical, marshalErr := json.Marshal(samplePlan("audit"))

	// Pinned at the base before Stage.SubAgent existed: a plan that leaves it unset hashes as it did.
	if want := "e51ae7d97917f094ec4d2e4440bf95263c2ed47f6817ddf6fc432bdc1761a65d"; err != nil || got != want {
		t.Errorf("PlanHash = %q, %v; want %q", got, err, want)
	}
	if changedErr != nil || changed == got {
		t.Errorf("a plan on the sub_agent path hashed %q (%v), the same as the plan without it", changed, changedErr)
	}
	if marshalErr != nil || strings.Contains(string(canonical), "sub_agent") {
		t.Errorf("plan JSON = %s (%v); want no sub_agent key when it is unset", canonical, marshalErr)
	}
}
