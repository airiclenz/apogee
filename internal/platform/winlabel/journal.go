package winlabel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// journalDirName is the sub-directory of the apogee home holding the label journals,
// and journalPrefix/Suffix name one run's file. The journal is per-PID rather than
// shared so two concurrent apogee processes cannot overwrite each other's record of what
// they labelled — the file name IS the ownership claim.
//
// journalTempPrefix/Suffix name the file an atomic write lands in before it is renamed into
// place. They deliberately match NEITHER the prefix NOR the suffix, so a temp file left by a
// crash is invisible to ListJournals and can never be read — or reported — as a journal.
const (
	journalDirName    = "confinement"
	journalPrefix     = "labels-"
	journalSuffix     = ".json"
	journalTempPrefix = "writing-"
	journalTempSuffix = ".tmp"
)

// Record is the on-disk record written BEFORE the first mandatory label is applied
// (ADR 0020 §2), so an apogee that is killed mid-run leaves behind enough to undo the disk
// mutation: the next NewConfiner finishes the restore, and `apogee probe host` reports the
// journal so an interrupted cleanup is diagnosable off-session.
//
// Its JSON tags are a compatibility surface with every journal a previous apogee left on a
// user's disk, and with the probe test that plants one by hand; they do not change.
type Record struct {
	// PID owns this journal. A journal whose process is still alive belongs to a running
	// apogee and must never be recovered by another one.
	PID int `json:"pid"`
	// Entries are the labelled roots (Root == true) plus every path found already carrying a
	// FOREIGN explicit label, whose prior descriptor teardown puts back verbatim. One entry
	// per path, and never one whose prior is a label apogee itself could have written — see
	// RecordEntry, which is the only thing that builds them.
	Entries []Entry `json:"entries"`
}

// Entry is one journalled path: a box root the backend labelled, and/or a path
// that already carried a foreign mandatory label before the run.
type Entry struct {
	Path string `json:"path"`
	// Root marks a box root whose whole tree teardown walks and clears.
	Root bool `json:"root,omitempty"`
	// PriorSDDL is the descriptor the path carried before labelling, empty when it carried
	// no label (the overwhelmingly common case) or carried a Low one apogee must never put
	// back (RecordEntry); teardown then clears the path instead.
	PriorSDDL string `json:"prior_sddl,omitempty"`
}

// Roots returns the journalled box roots, the trees teardown walks.
func (r Record) Roots() []string {
	out := make([]string, 0, len(r.Entries))
	for _, entry := range r.Entries {
		if entry.Root {
			out = append(out, entry.Path)
		}
	}
	return out
}

// PriorLabels returns the paths that carried an explicit label before the run, mapped to
// the descriptor teardown restores.
func (r Record) PriorLabels() map[string]string {
	out := make(map[string]string, len(r.Entries))
	for _, entry := range r.Entries {
		if entry.PriorSDDL != "" {
			out[entry.Path] = entry.PriorSDDL
		}
	}
	return out
}

// RecordEntry folds one about-to-be-labelled path into a journal's entries, returning the
// entries to persist and whether anything actually changed (unchanged ⇒ there is nothing new to
// flush before the label goes on).
//
// It is the only thing that builds an entry, because a journal is an INSTRUCTION to a future
// revert and the two ways it can lie both end in apogee's own Low label being restored rather
// than removed — residue that puts itself back (ADR 0020 §2):
//
//   - One entry per path, first prior wins. Labelling a root twice — a re-Confine after a
//     partial pass, or a second backend over a box another session already labelled — would
//     otherwise read the label apogee just wrote and record it as "the state before the run".
//   - A prior that is itself a LOW label is recorded as NO prior at all, so teardown clears the
//     path to unlabelled rather than putting a Low label back. That covers this backend's own
//     spellings, the inherited variant a labelled root propagates, and a genuinely foreign Low
//     label — which is ambiguous by construction, and where clearing is the SAFE direction:
//     unlabelled means implicitly Medium, i.e. LESS writable, and ADR 0020's manual remedy
//     states an explicit Medium label is behaviourally identical to no label at all.
//
// An entry naming neither a root to walk nor a prior to put back describes no mutation to undo,
// so it is not recorded — that is what keeps a re-walked tree of apogee's own labels out of the
// journal instead of appending (and re-flushing) one useless entry per file.
//
// fold is injected (nil ⇒ FoldPath) so the decision is table-testable on any OS.
func RecordEntry(entries []Entry, entry Entry, fold func(string) string) ([]Entry, bool) {
	if fold == nil {
		fold = FoldPath
	}
	if IsLowLabel(entry.PriorSDDL) {
		entry.PriorSDDL = ""
	}
	if !entry.Root && entry.PriorSDDL == "" {
		return entries, false
	}
	key := fold(entry.Path)
	for i := range entries {
		if fold(entries[i].Path) != key {
			continue
		}
		// The first prior recorded for a path is the only honest one, but a path first seen as
		// a labelled descendant can still be promoted to a ROOT, whose tree teardown walks.
		if entry.Root && !entries[i].Root {
			entries[i].Root = true
			return entries, true
		}
		return entries, false
	}
	return append(entries, entry), true
}

// UnwindEntry removes the entry for path when it records NO prior, returning the
// surviving entries and whether anything was removed. It is labelBox's undo for a root whose
// label write FAILED right after the entry was journalled: journal-before-label is the correct
// order and stays, but the failure means the entry now describes a mutation that never
// happened, and keeping it turns every later Close and recovery into a failing no-op —
// clearing a label that is not there fails on the same unwritable root, so the journal is
// never retired and Residue alarms forever over a disk carrying no label.
//
// An entry that DOES record a prior is kept even then: whether that prior still sits on the
// path is not knowable from here, and ambiguity resolves toward keeping the record — a
// spurious restore attempt is recoverable, a destroyed record is not.
//
// fold is injected (nil ⇒ FoldPath) so the decision is table-testable on any OS.
func UnwindEntry(entries []Entry, path string, fold func(string) string) ([]Entry, bool) {
	if fold == nil {
		fold = FoldPath
	}
	key := fold(path)
	for i := range entries {
		if fold(entries[i].Path) != key {
			continue
		}
		if entries[i].PriorSDDL != "" {
			return entries, false
		}
		return append(entries[:i], entries[i+1:]...), true
	}
	return entries, false
}

// Home resolves the apogee home the label journals live under, matching the composition
// root's DEFAULT (~/.apogee). NewConfiner takes no arguments — it is the per-OS
// selector every backend shares — so a --config override is deliberately not threaded into it;
// the journal is a crash-recovery aid whose location must be findable without one. Everything
// that reads the journals resolves them through here for the same reason: a reader that used
// the session's configured root instead would report "no outstanding labels" under a
// non-default --config while the labels were sitting in the default home.
func Home() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".apogee")
}

// JournalDir returns the journal directory inside the apogee home.
func JournalDir(home string) string { return filepath.Join(home, journalDirName) }

// JournalPath returns the journal file one process owns.
func JournalPath(home string, pid int) string {
	return filepath.Join(JournalDir(home), fmt.Sprintf("%s%d%s", journalPrefix, pid, journalSuffix))
}

// WriteJournal persists r to path, creating the journal directory. It is called BEFORE
// the first label of a box is applied and again whenever a pre-existing label is discovered,
// so a crash at any point leaves a journal describing at least everything already mutated.
//
// That promise only holds if the file is never observed HALF-written, which a truncate-in-place
// write cannot offer: the process is killed between the truncate and the last byte, and what
// survives is a journal neither recovery nor Residue can decode, describing labels
// that are really on the disk. So the write is atomic — the JSON goes to a temp file in the
// journal directory itself (same volume, so the rename is a metadata operation), is flushed,
// and os.Rename replaces the previous journal in one step. A crash mid-flush therefore leaves
// either the PREVIOUS complete journal or, on the very first write, no journal at all, and the
// caller has not labelled anything yet in that case.
//
// A failure anywhere here is the caller's cue to refuse the box (labelBox): no journal on disk
// means no label on the disk either.
func WriteJournal(path string, r Record) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("apogee: confine: create label journal dir: %w", err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("apogee: confine: encode label journal: %w", err)
	}
	temp, err := os.CreateTemp(dir, journalTempPrefix+"*"+journalTempSuffix)
	if err != nil {
		return fmt.Errorf("apogee: confine: create the label journal temp file: %w", err)
	}
	tempPath := temp.Name()
	// A no-op once the rename has consumed the temp file; on every failure below it is what
	// keeps a partial write from being left behind next to the journal.
	defer func() { _ = os.Remove(tempPath) }()

	if err := writeAndSync(temp, raw); err != nil {
		return fmt.Errorf("apogee: confine: write the label journal %q: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("apogee: confine: replace the label journal %q: %w", path, err)
	}
	return nil
}

// writeAndSync writes raw to f, flushes it to the disk and closes it, so the rename that
// follows can only ever publish a complete file. os.CreateTemp already creates f 0600, the
// mode the journal has always carried.
func writeAndSync(f *os.File, raw []byte) error {
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadJournal loads one journal file.
func ReadJournal(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return Record{}, fmt.Errorf("apogee: confine: decode label journal %q: %w", path, err)
	}
	return r, nil
}

// ListJournals returns the journal files under home, sorted, or nil when the directory
// does not exist — the normal case on every OS but Windows and on a Windows host that has
// never confined anything.
func ListJournals(home string) []string {
	entries, err := os.ReadDir(JournalDir(home))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, journalPrefix) || !strings.HasSuffix(name, journalSuffix) {
			continue
		}
		out = append(out, filepath.Join(JournalDir(home), name))
	}
	sort.Strings(out)
	return out
}

// SiblingJournals reads every journal under home EXCEPT the one at own — the other
// sessions whose journals may still claim a root the caller is about to clear. An
// undecodable sibling is skipped: it names no owner to check alive and no roots to spare,
// and erring toward clearing is the safe direction — a cleared label is less privilege,
// never more (the posture recoverLabelJournals takes with the same file). home may be ""
// (no resolvable user profile), where no journal exists and there is nothing to read.
func SiblingJournals(home, own string) []Record {
	if home == "" {
		return nil
	}
	var out []Record
	for _, path := range ListJournals(home) {
		if strings.EqualFold(path, own) {
			continue
		}
		r, err := ReadJournal(path)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Residue reports mandatory-label journals left by a run that did not get to revert them —
// the Windows-specific line ADR 0021's host report gains (ADR 0020 §2). It returns "" when
// there is nothing outstanding, which is every OS but Windows and the normal case on Windows,
// so the caller can state it unconditionally.
//
// It takes no home: the journals live where the backend writes them (Home), not under the
// session's configured root, and a caller that could name a root could name the wrong one and
// report residue-free a disk that is not.
func Residue() string { return ResidueIn(Home()) }

// ResidueIn is Residue against a given home, so the reporting rules are testable against a
// temporary directory — including from package platform's Windows-tagged lifecycle tests,
// which assert on the residue of a t.TempDir() home (D6).
//
// A journal belonging to THIS process is skipped: an in-session report must not describe the
// session's own live labels as residue. A journal belonging to another live apogee is still
// listed, because from the reader's point of view "there are Low labels on your disk right
// now" is the fact worth stating, and the wording names both causes.
//
// A journal that cannot be READ is reported rather than skipped. It is the worst state on this
// list — recoverLabelJournals cannot revert what it cannot decode, so it stays on the disk
// forever — and skipping it made the one surface that could tell the user silent about it.
// Its owner cannot be identified either, so it is reported even though it MIGHT be this
// process's own: since journals are written atomically, a live session's own file is never
// mid-write, and an unreadable one is a genuine finding whoever wrote it.
func ResidueIn(home string) string {
	if home == "" {
		return ""
	}
	var roots, unreadable []string
	for _, path := range ListJournals(home) {
		r, err := ReadJournal(path)
		if err != nil {
			unreadable = append(unreadable, path)
			continue
		}
		if r.PID == os.Getpid() {
			continue
		}
		roots = append(roots, r.Roots()...)
	}
	return residueNotice(roots, unreadable)
}
