package adoption

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// newTestStore returns a Store for a fresh Project root, recording under a workspaces folder that
// does not exist yet — the first-run shape.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "workspaces")
	store, err := New(dir, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store, dir
}

// classify is Classify failing the test on an error.
func classify(t *testing.T, s *Store, entries ...Entry) Classification {
	t.Helper()
	c, err := s.Classify(entries)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	return c
}

func TestClassifyAnswersByRecordedFingerprint(t *testing.T) {
	t.Parallel()

	goTest := Entry{Kind: "terminal", Text: "go test"}
	cases := []struct {
		name   string
		record func(*Store) error
		ask    Entry
		want   State
	}{
		{"nothing recorded is proposed", func(*Store) error { return nil }, goTest, Proposed},
		{"adopt then adopted", func(s *Store) error { return s.Adopt(goTest) }, goTest, Adopted},
		{"reject then rejected", func(s *Store) error { return s.Reject(goTest) }, goTest, Rejected},
		{"edited text is proposed", func(s *Store) error { return s.Adopt(goTest) },
			Entry{Kind: "terminal", Text: "go test ./..."}, Proposed},
		{"whitespace-only change is proposed", func(s *Store) error { return s.Adopt(goTest) },
			Entry{Kind: "terminal", Text: "go  test"}, Proposed},
		{"trailing space is proposed", func(s *Store) error { return s.Adopt(goTest) },
			Entry{Kind: "terminal", Text: "go test "}, Proposed},
		{"same text under another kind is proposed", func(s *Store) error { return s.Adopt(goTest) },
			Entry{Kind: "mcp-servers", Text: "go test"}, Proposed},
		{"forget makes it proposed again", func(s *Store) error {
			if err := s.Adopt(goTest); err != nil {
				return err
			}
			return s.Forget(goTest)
		}, goTest, Proposed},
		{"a later answer replaces an earlier one", func(s *Store) error {
			if err := s.Adopt(goTest); err != nil {
				return err
			}
			return s.Reject(goTest)
		}, goTest, Rejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, _ := newTestStore(t)
			if err := tc.record(store); err != nil {
				t.Fatalf("record: %v", err)
			}
			got := classify(t, store, tc.ask)
			if state := stateOf(got, tc.ask); state != tc.want {
				t.Errorf("state = %s, want %s (classification %+v)", state, tc.want, got)
			}
		})
	}
}

// stateOf is the list of c that holds e.
func stateOf(c Classification, e Entry) State {
	for _, list := range []struct {
		entries []Entry
		state   State
	}{{c.Adopted, Adopted}, {c.Rejected, Rejected}, {c.Proposed, Proposed}} {
		for _, got := range list.entries {
			if got == e {
				return list.state
			}
		}
	}
	return ""
}

func TestClassifyKeepsTheGivenOrderPerState(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	a := Entry{Kind: "terminal", Text: "go test"}
	b := Entry{Kind: "terminal", Text: "make check"}
	c := Entry{Kind: "mcp-servers", Text: "github"}
	d := Entry{Kind: "terminal", Text: "go vet"}
	if err := store.Adopt(c, a); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := store.Reject(d); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	got := classify(t, store, a, b, c, d)
	want := Classification{Adopted: []Entry{a, c}, Proposed: []Entry{b}, Rejected: []Entry{d}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Classify = %+v, want %+v", got, want)
	}
}

func TestAnswersPersistAcrossStores(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "workspaces")
	root := t.TempDir()
	adopted := Entry{Kind: "terminal", Text: "go test"}
	rejected := Entry{Kind: "terminal", Text: "rm -rf build"}
	first, err := New(dir, root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := first.Adopt(adopted); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := first.Reject(rejected); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	second, err := New(dir, root)
	if err != nil {
		t.Fatalf("New (second): %v", err)
	}
	got := classify(t, second, adopted, rejected)
	want := Classification{Adopted: []Entry{adopted}, Rejected: []Entry{rejected}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Classify in a new store = %+v, want %+v", got, want)
	}
}

func TestRecordFileModesAreOwnerOnly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}

	store, dir := newTestStore(t)
	if err := store.Adopt(Entry{Kind: "terminal", Text: "go test"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	for path, want := range map[string]os.FileMode{dir: dirPerm, store.Path(): filePerm} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode of %s = %o, want %o", path, got, want)
		}
	}
}

func TestRecordIsNamedByTheFullDigestOfTheResolvedRoot(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "workspaces")
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	sum := sha256.Sum256([]byte(resolved))
	wantPath := filepath.Join(dir, hex.EncodeToString(sum[:])+recordExt)

	store, err := New(dir, root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store.Path() != wantPath || store.Root() != resolved {
		t.Errorf("New(%q) = path %q root %q, want path %q root %q", root, store.Path(), store.Root(), wantPath, resolved)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink, err := New(dir, link)
	if err != nil {
		t.Fatalf("New via link: %v", err)
	}
	if viaLink.Path() != wantPath {
		t.Errorf("a symlinked spelling of the root records at %q, want %q", viaLink.Path(), wantPath)
	}
}

func TestRecordStatesItsRootAndAnswers(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	entry := Entry{Kind: "terminal", Text: "go test"}
	if err := store.Adopt(entry); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	for _, want := range []string{"root: " + store.Root(), Fingerprint(entry) + ": adopted"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("record lacks %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), entry.Text) {
		t.Errorf("record spells the entry's text; it should hold only its fingerprint:\n%s", data)
	}
}

func TestRecordOfAnotherRootIsRefused(t *testing.T) {
	t.Parallel()

	store, dir := newTestStore(t)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	foreign := "root: /somewhere/else\nentries:\n  " + Fingerprint(Entry{Kind: "terminal", Text: "go test"}) + ": adopted\n"
	if err := os.WriteFile(store.Path(), []byte(foreign), filePerm); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if _, err := store.Classify([]Entry{{Kind: "terminal", Text: "go test"}}); err == nil {
		t.Error("Classify trusted a record naming another Project root")
	}
	if err := store.Adopt(Entry{Kind: "terminal", Text: "make"}); err == nil {
		t.Error("Adopt overwrote a record naming another Project root")
	}
}

func TestUnknownAnswerReadsAsProposedAndSurvivesARewrite(t *testing.T) {
	t.Parallel()

	store, dir := newTestStore(t)
	odd := Entry{Kind: "terminal", Text: "go test"}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "root: " + store.Root() + "\nentries:\n  " + Fingerprint(odd) + ": maybe\n"
	if err := os.WriteFile(store.Path(), []byte(content), filePerm); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if state := stateOf(classify(t, store, odd), odd); state != Proposed {
		t.Errorf("an unknown answer reads as %s, want %s", state, Proposed)
	}
	if err := store.Adopt(Entry{Kind: "terminal", Text: "make"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if !strings.Contains(string(data), Fingerprint(odd)+": maybe") {
		t.Errorf("a rewrite dropped an entry it could not read:\n%s", data)
	}
}

func TestFingerprintPinsKindAndExactText(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte("terminal\x00go test"))
	if got, want := Fingerprint(Entry{Kind: "terminal", Text: "go test"}), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("Fingerprint = %s, want %s", got, want)
	}
}

func TestNewAndConfigLockPathRefuseMissingInputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "absent")
	for _, tc := range []struct{ name, dir, root string }{
		{"no dir", "", root},
		{"no root", t.TempDir(), ""},
		{"root that does not exist", t.TempDir(), missing},
	} {
		if _, err := New(tc.dir, tc.root); err == nil {
			t.Errorf("New with %s succeeded", tc.name)
		}
		if _, err := ConfigLockPath(tc.dir, tc.root); err == nil {
			t.Errorf("ConfigLockPath with %s succeeded", tc.name)
		}
	}
}

func TestConfigLockPathSitsBesideTheRecord(t *testing.T) {
	t.Parallel()

	store, dir := newTestStore(t)
	lock, err := ConfigLockPath(dir, store.Root())
	if err != nil {
		t.Fatalf("ConfigLockPath: %v", err)
	}
	if want := strings.TrimSuffix(store.Path(), recordExt) + configLockExt; lock != want {
		t.Errorf("ConfigLockPath = %q, want %q", lock, want)
	}
}
