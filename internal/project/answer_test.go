package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
)

// answerFixture is a global config and a Project config side by side under one temp dir, with the
// Project root's adoption store; nothing is answered yet.
type answerFixture struct {
	globalPath string
	root       string
	store      *adoption.Store
}

// newAnswerFixture writes the global config and snapshotProjectYAML, and opens the root's store.
func newAnswerFixture(t *testing.T) answerFixture {
	t.Helper()

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	root := filepath.Join(dir, "project")
	for _, d := range []string{home, filepath.Dir(config.ProjectFilePath(root))} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("create %s: %v", d, err)
		}
	}
	store, err := adoption.New(config.WorkspacesDir(home), root)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	f := answerFixture{globalPath: filepath.Join(home, "config.yaml"), root: root, store: store}
	f.writeGlobal(t, "mode: plan\n")
	f.writeProject(t, snapshotProjectYAML)
	return f
}

// writeGlobal replaces the global config with body.
func (f answerFixture) writeGlobal(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(f.globalPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the global config: %v", err)
	}
}

// writeProject replaces the Project config with body.
func (f answerFixture) writeProject(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(config.ProjectFilePath(f.root), []byte(body), 0o600); err != nil {
		t.Fatalf("write the project config: %v", err)
	}
}

// removeProject takes the Project config away.
func (f answerFixture) removeProject(t *testing.T) {
	t.Helper()
	if err := os.Remove(config.ProjectFilePath(f.root)); err != nil {
		t.Fatalf("remove the project config: %v", err)
	}
}

// entries is what the Project config offers now: every entry of snapshotProjectYAML before an answer.
func (f answerFixture) entries(t *testing.T) []domain.Proposal {
	t.Helper()
	snapshot, err := Load(f.globalPath, f.root, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return snapshot.Entries()
}

// state is the answer the store records for p.
func (f answerFixture) state(t *testing.T, p domain.Proposal) adoption.State {
	t.Helper()
	entry, err := ProposalEntry(p)
	if err != nil {
		t.Fatalf("ProposalEntry %+v: %v", p, err)
	}
	c, err := f.store.Classify([]adoption.Entry{entry})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	switch {
	case len(c.Adopted) == 1:
		return adoption.Adopted
	case len(c.Rejected) == 1:
		return adoption.Rejected
	}
	return adoption.Proposed
}

// assertNoRecord fails when the store's record file exists.
func (f answerFixture) assertNoRecord(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.store.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the adoption record exists (stat: %v); want none written", err)
	}
}

// labelOf names a proposal: a server or reaction by its handle, an Allow rule by its text.
func labelOf(p domain.Proposal) string {
	if p.Handle != "" {
		return p.Handle
	}
	return p.Text
}

// A yes the file still holds is recorded for every kind, and the snapshot Answer returns is the one
// loaded after the write: the rules in force, the servers and reactions adopted, nothing left to ask.
func TestAnswerAdoptRecordsEachKindTheFileHolds(t *testing.T) {
	t.Parallel()
	f := newAnswerFixture(t)
	entries := f.entries(t)

	next, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, true)

	if err != nil || len(notHeld) != 0 {
		t.Fatalf("Answer(adopt) = notHeld %+v, err %v; want every entry adopted", notHeld, err)
	}
	for _, p := range entries {
		if got := f.state(t, p); got != adoption.Adopted {
			t.Errorf("%s %q is recorded %q; want %q", p.Kind, labelOf(p), got, adoption.Adopted)
		}
	}
	if got := next.Entries(); len(got) != 0 {
		t.Errorf("next.Entries() = %+v; want nothing left to ask", got)
	}
	if got := next.Offer().Proposals.Adopted; len(got) != 4 {
		t.Errorf("next.Offer().Proposals.Adopted = %+v; want both servers and both reactions", got)
	}
	var projectRules int
	for _, r := range next.Options().AllowRules.Rules {
		if r.Layer == config.SourceProject {
			projectRules++
		}
	}
	if projectRules != 2 {
		t.Errorf("next holds %d project rules in force; want 2", projectRules)
	}
}

// A yes answers the file as it stands: an entry an edit took out or changed since it was offered
// comes back not held, and nothing is recorded for it, while the rest of the answer is recorded.
func TestAnswerAdoptRefusesWhatTheFileNoLongerHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		edited string
		gone   string
	}{
		{"allow rule", strings.Replace(snapshotProjectYAML, ", go build]", "]", 1), "go build"},
		{"server", strings.Replace(snapshotProjectYAML, "command: srv-b", "command: srv-b2", 1), "srv-b"},
		{"reaction", strings.Replace(snapshotProjectYAML, "  - id: rx-b\n    on: [file-changed]\n    run: [echo, b]\n",
			"", 1), "rx-b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAnswerFixture(t)
			entries := f.entries(t)
			f.writeProject(t, tc.edited)

			_, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, true)

			if err != nil {
				t.Fatalf("Answer(adopt): %v", err)
			}
			if len(notHeld) != 1 || labelOf(notHeld[0]) != tc.gone {
				t.Fatalf("notHeld = %+v; want only %q", notHeld, tc.gone)
			}
			for _, p := range entries {
				want := adoption.Adopted
				if labelOf(p) == tc.gone {
					want = adoption.Proposed
				}
				if got := f.state(t, p); got != want {
					t.Errorf("%s %q is recorded %q; want %q", p.Kind, labelOf(p), got, want)
				}
			}
		})
	}
}

// A yes no entry is held for opens no record: every entry comes back not held, the snapshot is the
// zero one, and no record file is written.
func TestAnswerAdoptWithNothingHeldWritesNoRecord(t *testing.T) {
	t.Parallel()
	f := newAnswerFixture(t)
	entries := f.entries(t)
	f.removeProject(t)

	next, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, true)

	if err != nil {
		t.Fatalf("Answer(adopt): %v", err)
	}
	if len(notHeld) != len(entries) {
		t.Errorf("notHeld = %+v; want all %d entries", notHeld, len(entries))
	}
	if len(next.Entries()) != 0 || len(next.Options().AllowRules.Rules) != 0 {
		t.Errorf("next = %+v; want the zero snapshot", next)
	}
	f.assertNoRecord(t)
}

// A no is recorded without a held check: an entry the file no longer holds is rejected all the same.
func TestAnswerRejectRecordsWithoutAHeldCheck(t *testing.T) {
	t.Parallel()
	f := newAnswerFixture(t)
	entries := f.entries(t)
	f.removeProject(t)

	_, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, false)

	if err != nil || notHeld != nil {
		t.Fatalf("Answer(reject) = notHeld %+v, err %v; want every entry rejected", notHeld, err)
	}
	for _, p := range entries {
		if got := f.state(t, p); got != adoption.Rejected {
			t.Errorf("%s %q is recorded %q; want %q", p.Kind, labelOf(p), got, adoption.Rejected)
		}
	}
}

// Record writes each answer with no held check — a reject, and a forget that undoes an adopt, both
// of text the file no longer holds — and an empty answer writes no record at all.
func TestRecordWritesWithoutAHeldCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		replies []Reply
		want    adoption.State
	}{
		{"reject", []Reply{(*adoption.Store).Reject}, adoption.Rejected},
		{"adopt", []Reply{(*adoption.Store).Adopt}, adoption.Adopted},
		{"forget", []Reply{(*adoption.Store).Adopt, (*adoption.Store).Forget}, adoption.Proposed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAnswerFixture(t)
			p := f.entries(t)[0]
			entry, err := ProposalEntry(p)
			if err != nil {
				t.Fatalf("ProposalEntry: %v", err)
			}
			f.removeProject(t)

			for _, reply := range tc.replies {
				if err := Record(f.store, []adoption.Entry{entry}, reply); err != nil {
					t.Fatalf("Record: %v", err)
				}
			}

			if got := f.state(t, p); got != tc.want {
				t.Errorf("%q is recorded %q; want %q", labelOf(p), got, tc.want)
			}
		})
	}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		f := newAnswerFixture(t)

		if err := Record(f.store, nil, (*adoption.Store).Forget); err != nil {
			t.Fatalf("Record(no entries): %v", err)
		}

		f.assertNoRecord(t)
	})
}

// A global config that does not resolve cannot stop a yes the Project config backs: the answer is
// recorded and the next snapshot's load failure comes back typed, told apart from a store failure,
// which records nothing and comes back as the store said it.
func TestAnswerUnderABrokenGlobalOrStore(t *testing.T) {
	t.Parallel()

	t.Run("broken global", func(t *testing.T) {
		t.Parallel()
		f := newAnswerFixture(t)
		entries := f.entries(t)
		f.writeGlobal(t, "model: [unclosed\n")

		_, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, true)

		var loadErr *NextLoadError
		if !errors.As(err, &loadErr) {
			t.Fatalf("Answer over a broken global = %v; want a *NextLoadError", err)
		}
		if len(notHeld) != 0 {
			t.Errorf("notHeld = %+v; want none", notHeld)
		}
		for _, p := range entries {
			if got := f.state(t, p); got != adoption.Adopted {
				t.Errorf("%s %q is recorded %q; want %q", p.Kind, labelOf(p), got, adoption.Adopted)
			}
		}
	})

	t.Run("broken store", func(t *testing.T) {
		t.Parallel()
		f := newAnswerFixture(t)
		entries := f.entries(t)
		const garbage = "entries: [unclosed\n"
		if err := os.MkdirAll(filepath.Dir(f.store.Path()), 0o700); err != nil {
			t.Fatalf("create the record dir: %v", err)
		}
		if err := os.WriteFile(f.store.Path(), []byte(garbage), 0o600); err != nil {
			t.Fatalf("break the record: %v", err)
		}

		_, _, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, entries, true)

		var loadErr *NextLoadError
		var heldErr *HeldCheckError
		if err == nil || errors.As(err, &loadErr) || errors.As(err, &heldErr) {
			t.Fatalf("Answer over a broken record = %v; want the store's own error", err)
		}
		if data, readErr := os.ReadFile(f.store.Path()); readErr != nil || string(data) != garbage {
			t.Errorf("the record = %q (%v); want it untouched", data, readErr)
		}
	})
}

// The held check reads the Project config with no global config, so a repo server a global one of
// the same name shadows is still held: the yes to it is recorded.
func TestAnswerAdoptsARepoServerAGlobalOneShadows(t *testing.T) {
	t.Parallel()
	f := newAnswerFixture(t)
	var server domain.Proposal
	for _, p := range f.entries(t) {
		if p.Handle == "srv-a" {
			server = p
		}
	}
	f.writeGlobal(t, "mode: plan\nmcp-servers:\n  - name: srv-a\n    transport: stdio\n    command: global-srv\n")

	_, notHeld, err := Answer(f.globalPath, f.root, os.ReadFile, f.store, []domain.Proposal{server}, true)

	if err != nil || len(notHeld) != 0 {
		t.Fatalf("Answer(adopt srv-a) = notHeld %+v, err %v; want it adopted", notHeld, err)
	}
	if got := f.state(t, server); got != adoption.Adopted {
		t.Errorf("srv-a is recorded %q; want %q", got, adoption.Adopted)
	}
}
