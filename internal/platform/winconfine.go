package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Windows token Confiner — the OS-free half (ADR 0020; confinement-execution-contract §9).
//
// Everything here is a pure function or plain file I/O over JSON, so the Windows
// backend's DECISIONS — which roots get labelled, which are refused outright, what
// happens to a network-deny box, and what an interrupted run leaves behind — are
// table-testable on Linux and macOS exactly as the seatbelt profile generator is
// (seatbelt.go). The OS calls that mint the token and write mandatory labels live in
// confiner_windows.go, which is the only Windows-tagged part.

// windowsFloorBuild is the minimum Windows build this project claims to have tested and
// can service: 10 1809 / build 17763 / Server 2019 (ADR 0020 §5). It is NOT an API floor —
// mandatory integrity control, restricted tokens and label SACLs have existed since Vista —
// so nothing below it is broken, merely unsupported: NewConfiner returns denyConfiner there
// and the existing degradation notice fires unchanged.
const windowsFloorBuild = 17763

// belowWindowsFloor reports whether a host build number is under the floor — the deny-vs-token
// half of selectWindowsConfiner's decision, split out so it is provable on every OS. The
// ambient read (windows.RtlGetNtVersionNumbers) stays Windows-tagged; a windows-tagged test can
// only ever observe the branch its own host is on, and the branch that matters most is the one
// the development machine is never on.
func belowWindowsFloor(build uint32) bool { return build < windowsFloorBuild }

// The mandatory-label SDDL strings the backend writes (ADR 0020 §2).
//
//   - windowsDirLabelSDDL labels a DIRECTORY Low with NO_WRITE_UP, object- and
//     container-inheritable, so objects created inside the box during the run are Low too.
//   - windowsFileLabelSDDL labels an existing FILE Low; inheritance flags are meaningless
//     on a leaf, and the label pass must reach existing files because inheritance covers
//     newly created objects ONLY — a Low child editing a pre-existing source file would
//     otherwise be denied, which is the single most common thing an agent does.
//   - windowsClearLabelSDDL is a NULL SACL: no mandatory label at all, which is the state
//     an unlabelled object is in (implicitly Medium with NO_WRITE_UP). It is what teardown
//     writes to a path that carried no label before the run. Clearing via a NULL SACL keeps
//     the restore inside LABEL_SECURITY_INFORMATION, which needs only WRITE_OWNER on the
//     object; asking additionally for UNPROTECTED_SACL_SECURITY_INFORMATION would drag in
//     the SACL privilege check (SeSecurityPrivilege) and fail for an ordinary user.
const (
	windowsDirLabelSDDL   = "S:(ML;OICI;NW;;;LW)"
	windowsFileLabelSDDL  = "S:(ML;;NW;;;LW)"
	windowsClearLabelSDDL = "S:"
)

// windowsLabelACEPrefix is the SDDL spelling of a mandatory-label ACE. A descriptor string
// containing it carries an explicit label that teardown must put back rather than clear.
const windowsLabelACEPrefix = "(ML;"

// labelJournalDirName is the sub-directory of the apogee home holding the label journals,
// and labelJournalPrefix/Suffix name one run's file. The journal is per-PID rather than
// shared so two concurrent apogee processes cannot overwrite each other's record of what
// they labelled — the file name IS the ownership claim.
//
// labelJournalTempPattern names the file an atomic write lands in before it is renamed into
// place. It deliberately matches NEITHER the prefix NOR the suffix, so a temp file left by a
// crash is invisible to listLabelJournals and can never be read — or reported — as a journal.
const (
	labelJournalDirName    = "confinement"
	labelJournalPrefix     = "labels-"
	labelJournalSuffix     = ".json"
	labelJournalTempPrefix = "writing-"
	labelJournalTempSuffix = ".tmp"
)

// labelJournal is the on-disk record written BEFORE the first mandatory label is applied
// (ADR 0020 §2), so an apogee that is killed mid-run leaves behind enough to undo the disk
// mutation: the next NewConfiner finishes the restore, and `apogee probe host` reports the
// journal so an interrupted cleanup is diagnosable off-session.
type labelJournal struct {
	// PID owns this journal. A journal whose process is still alive belongs to a running
	// apogee and must never be recovered by another one.
	PID int `json:"pid"`
	// Entries are the labelled roots (Root == true) plus every path found already carrying a
	// FOREIGN explicit label, whose prior descriptor teardown puts back verbatim. One entry
	// per path, and never one whose prior is a label apogee itself could have written — see
	// journalLabelEntry, which is the only thing that builds them.
	Entries []labelJournalEntry `json:"entries"`
}

// labelJournalEntry is one journalled path: a box root the backend labelled, and/or a path
// that already carried a foreign mandatory label before the run.
type labelJournalEntry struct {
	Path string `json:"path"`
	// Root marks a box root whose whole tree teardown walks and clears.
	Root bool `json:"root,omitempty"`
	// PriorSDDL is the descriptor the path carried before labelling, empty when it carried
	// no label (the overwhelmingly common case) or carried a Low one apogee must never put
	// back (journalLabelEntry); teardown then clears the path instead.
	PriorSDDL string `json:"prior_sddl,omitempty"`
}

// roots returns the journalled box roots, the trees teardown walks.
func (j labelJournal) roots() []string {
	out := make([]string, 0, len(j.Entries))
	for _, entry := range j.Entries {
		if entry.Root {
			out = append(out, entry.Path)
		}
	}
	return out
}

// priorLabels returns the paths that carried an explicit label before the run, mapped to
// the descriptor teardown restores.
func (j labelJournal) priorLabels() map[string]string {
	out := make(map[string]string, len(j.Entries))
	for _, entry := range j.Entries {
		if entry.PriorSDDL != "" {
			out[entry.Path] = entry.PriorSDDL
		}
	}
	return out
}

// foldLabelPath case-folds a path for the journal's one-entry-per-path rule. Windows paths are
// case-insensitive, so C:\Work and c:\work name one location and must never become two journal
// entries — the same upper-casing windowsProtectedRoots dedupes its locations with, and the
// whole-path form of the component-wise fold hostRules.sameComponent applies.
func foldLabelPath(p string) string { return strings.ToUpper(p) }

// windowsLowLabelSIDs are the SDDL spellings of the Low integrity level — the ONE level this
// backend ever writes — in both the alias and the canonical form, so a descriptor is recognised
// whichever way the OS rendered it.
var windowsLowLabelSIDs = map[string]bool{"LW": true, "S-1-16-4096": true}

// isLowLabelSDDL reports whether sddl carries a mandatory-label ACE naming the Low integrity
// level. It is deliberately looser than comparing against windowsDirLabelSDDL /
// windowsFileLabelSDDL verbatim: the same label read back from the OS carries descriptor flags
// (S:AI(…)) and, on a path that inherited it from a labelled root, the inherited ACE flag
// (OICIID), so a string equality test would recognise apogee's own label only in the one
// spelling apogee happens to write it in.
func isLowLabelSDDL(sddl string) bool {
	for rest := sddl; ; {
		start := strings.Index(rest, windowsLabelACEPrefix)
		if start < 0 {
			return false
		}
		rest = rest[start+len(windowsLabelACEPrefix):]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return false
		}
		// What follows the consumed "(ML;" is flags;rights;object;inherit;sid — the integrity
		// level is the trailing SID field.
		if fields := strings.Split(rest[:end], ";"); len(fields) >= 5 &&
			windowsLowLabelSIDs[strings.ToUpper(strings.TrimSpace(fields[4]))] {
			return true
		}
		rest = rest[end+1:]
	}
}

// journalLabelEntry folds one about-to-be-labelled path into a journal's entries, returning the
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
// fold is injected (nil ⇒ foldLabelPath) so the decision is table-testable on any OS.
func journalLabelEntry(entries []labelJournalEntry, entry labelJournalEntry, fold func(string) string) ([]labelJournalEntry, bool) {
	if fold == nil {
		fold = foldLabelPath
	}
	if isLowLabelSDDL(entry.PriorSDDL) {
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

// unwindLabelEntry removes the entry for path when it records NO prior, returning the
// surviving entries and whether anything was removed. It is labelBox's undo for a root whose
// label write FAILED right after the entry was journalled: journal-before-label is the correct
// order and stays, but the failure means the entry now describes a mutation that never
// happened, and keeping it turns every later Close and recovery into a failing no-op —
// clearing a label that is not there fails on the same unwritable root, so the journal is
// never retired and ConfinementResidue alarms forever over a disk carrying no label.
//
// An entry that DOES record a prior is kept even then: whether that prior still sits on the
// path is not knowable from here, and ambiguity resolves toward keeping the record — a
// spurious restore attempt is recoverable, a destroyed record is not.
//
// fold is injected (nil ⇒ foldLabelPath) so the decision is table-testable on any OS.
func unwindLabelEntry(entries []labelJournalEntry, path string, fold func(string) string) ([]labelJournalEntry, bool) {
	if fold == nil {
		fold = foldLabelPath
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

// descendantLabelDecision is the label walk's three-way decision for one descendant, from
// the outcome of reading its prior mandatory label: shouldJournal reports whether the prior
// must be journalled before any label lands, shouldLabel whether the path may be labelled at
// all.
//
//   - A read ERROR skips the path entirely — no journal entry, no label. Labelling anyway
//     would destroy a possibly-foreign label with no record of how to put it back, which is
//     the one thing ADR 0020 §2's journal-first invariant forbids; the cost is labelTree's
//     tolerated-descendant one — that single path stays opaque to the confined child and
//     never gates the box.
//   - A non-empty prior is journalled and then labelled; what the entry may SAY about the
//     prior remains journalLabelEntry's decision.
//   - No prior (the overwhelmingly common case) is labelled with nothing to journal.
//
// It is pure so the decision is table-testable on any OS — the retireLabelJournal seam
// pattern.
func descendantLabelDecision(prior string, readErr error) (shouldJournal, shouldLabel bool) {
	if readErr != nil {
		return false, false
	}
	return prior != "", true
}

// windowsProtectedRoots lists the locations the backend refuses to label, resolved from the
// environment (ADR 0020 §2's guardrails). Labelling any of them Low would be a catastrophic
// and near-unrevertable mutation of the machine, so a box root that IS one — or that
// CONTAINS one, which a volume root or C:\Users does — is refused rather than labelled.
//
// lookup reads the environment (nil ⇒ os.LookupEnv) and userHome is the resolved user
// profile, passed in rather than looked up so the whole guardrail is testable off Windows.
// Empty values are dropped: an unset variable names no location and must not veto a box.
func windowsProtectedRoots(lookup func(string) (string, bool), userHome string) []string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	out := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(path string) {
		if path == "" {
			return
		}
		fold := strings.ToUpper(path)
		if _, dup := seen[fold]; dup {
			return
		}
		seen[fold] = struct{}{}
		out = append(out, path)
	}
	for _, key := range []string{
		"SystemRoot", "windir",
		"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"ProgramData", "PUBLIC", "USERPROFILE",
	} {
		value, _ := lookup(key)
		add(value)
	}
	add(userHome)
	return out
}

// windowsBoxRoots resolves box to the minimal set of non-overlapping roots the backend
// labels, or an error wrapping ErrConfinementUnavailable when the box cannot be expressed
// on this disk (ADR 0020 §2/§3 — a per-run labelling refusal is a routine Windows outcome
// that contract §4 demotes to a forced Gate, never a silent unconfined run).
//
// It refuses, in this order:
//
//   - A root r cannot resolve — an 8.3 short name no resolver could expand, a device path,
//     a drive-relative "C:work". REFUSING TO LABEL is the only safe answer: Contains
//     reports "not contained" for such a path, which is correct for collapsing but would
//     read as "outside the guardrail" here, i.e. it would wave the path through the fence.
//   - A volume root (C:\, \\server\share). Nothing above the box may be labelled.
//   - A root that is, or contains, a protected location (%SystemRoot%, %ProgramFiles%,
//     the user-profile root, …).
//
// Surviving roots are then collapsed: a root nested inside another is dropped, because a
// tree labelled twice would be journalled twice and restored inconsistently. Duplicates
// keep their first occurrence.
func windowsBoxRoots(r hostRules, box domain.ConfinementBox, protected []string) ([]string, error) {
	for _, path := range protected {
		if _, _, ok := r.split(path); !ok {
			return nil, fmt.Errorf("%w: cannot resolve the protected location %q, so no box root can be checked against it",
				domain.ErrConfinementUnavailable, path)
		}
	}

	kept := make([]string, 0, 1+len(box.WritablePaths))
	for _, root := range append([]string{box.WorkspaceRoot}, box.WritablePaths...) {
		if root == "" {
			continue
		}
		if err := windowsLabelGuardrail(r, root, protected); err != nil {
			return nil, err
		}
		kept = append(kept, root)
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("%w: box names no writable root to label", domain.ErrConfinementUnavailable)
	}

	out := make([]string, 0, len(kept))
	for i, inner := range kept {
		nested := false
		for j, outer := range kept {
			if i == j || !r.Contains(outer, inner) {
				continue
			}
			// Equal paths contain each other; keep the first occurrence only.
			if !r.Contains(inner, outer) || j < i {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, inner)
		}
	}
	return out, nil
}

// windowsLabelGuardrail reports whether root may be labelled, wrapping
// ErrConfinementUnavailable when it may not. See windowsBoxRoots for the three refusals and
// why an unresolvable path is refused rather than treated as "outside".
func windowsLabelGuardrail(r hostRules, root string, protected []string) error {
	_, parts, ok := r.split(root)
	if !ok {
		return fmt.Errorf("%w: refusing to label %q — this host cannot resolve it to a comparable location, so the guardrails cannot be evaluated",
			domain.ErrConfinementUnavailable, root)
	}
	if len(parts) == 0 {
		return fmt.Errorf("%w: refusing to label the volume root %q", domain.ErrConfinementUnavailable, root)
	}
	for _, path := range protected {
		if r.Contains(root, path) {
			return fmt.Errorf("%w: refusing to label %q — it is or contains the protected location %q",
				domain.ErrConfinementUnavailable, root, path)
		}
	}
	return nil
}

// windowsNetworkDenyDecision fails a box closed when it asks for a network tightening the
// token backend cannot enforce (ADR 0020 §4). NetworkAllow is a TIGHTENING list: empty
// leaves the network open (the ADR 0012 default, nothing to enforce); non-empty opts into
// network-deny, which no token or integrity facility can express. Running network-open
// silently would leave a fence the user believes is in place as a no-op, so it returns
// ErrConfinementUnavailable and the dispatch disposition gates the call instead — the same
// position, for the same reason, as landlock_linux.go's networkDenyDecision below ABI 4.
func windowsNetworkDenyDecision(box domain.ConfinementBox) error {
	if len(box.NetworkAllow) == 0 {
		return nil
	}
	return fmt.Errorf("%w: box requests network-deny but the Windows token backend cannot enforce per-host egress; refusing to run network-open silently",
		domain.ErrConfinementUnavailable)
}

// confinementJournalHome resolves the apogee home the label journals live under, matching the
// composition root's DEFAULT (~/.apogee). NewConfiner takes no arguments — it is the per-OS
// selector every backend shares — so a --config override is deliberately not threaded into it;
// the journal is a crash-recovery aid whose location must be findable without one. Everything
// that reads the journals resolves them through here for the same reason: a reader that used
// the session's configured root instead would report "no outstanding labels" under a
// non-default --config while the labels were sitting in the default home.
func confinementJournalHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".apogee")
}

// labelJournalDir returns the journal directory inside the apogee home.
func labelJournalDir(home string) string { return filepath.Join(home, labelJournalDirName) }

// labelJournalPath returns the journal file one process owns.
func labelJournalPath(home string, pid int) string {
	return filepath.Join(labelJournalDir(home), fmt.Sprintf("%s%d%s", labelJournalPrefix, pid, labelJournalSuffix))
}

// writeLabelJournal persists j to path, creating the journal directory. It is called BEFORE
// the first label of a box is applied and again whenever a pre-existing label is discovered,
// so a crash at any point leaves a journal describing at least everything already mutated.
//
// That promise only holds if the file is never observed HALF-written, which a truncate-in-place
// write cannot offer: the process is killed between the truncate and the last byte, and what
// survives is a journal neither recovery nor ConfinementResidue can decode, describing labels
// that are really on the disk. So the write is atomic — the JSON goes to a temp file in the
// journal directory itself (same volume, so the rename is a metadata operation), is flushed,
// and os.Rename replaces the previous journal in one step. A crash mid-flush therefore leaves
// either the PREVIOUS complete journal or, on the very first write, no journal at all, and the
// caller has not labelled anything yet in that case.
//
// A failure anywhere here is the caller's cue to refuse the box (labelBox): no journal on disk
// means no label on the disk either.
func writeLabelJournal(path string, j labelJournal) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("apogee: confine: create label journal dir: %w", err)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("apogee: confine: encode label journal: %w", err)
	}
	temp, err := os.CreateTemp(dir, labelJournalTempPrefix+"*"+labelJournalTempSuffix)
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

// retireLabelJournal reverts one journal's disk mutation through revert and then decides the
// journal FILE's fate: it is removed only when the revert succeeded AND left nothing behind. A
// failed revert leaves the file exactly where it is, because the journal is the only record of
// the labels still sitting on the disk — deleting it would strand them permanently, whereas
// keeping it means the next NewConfiner retries the restore and, until one does,
// ConfinementResidue reports it (ADR 0020 §2).
//
// A revert may also succeed while HANDING OFF entries it deliberately did not act on — a
// foreign prior under a root a sibling journal still claims (restorablePriors). Those are not
// failures, but they are still undischarged instructions, so the journal is REWRITTEN to carry
// exactly them (under its original owner) rather than removed: the record of the foreign label
// survives sibling teardown ordering, and the first construction after the claiming journals
// are gone completes the restore. The remaining entries are returned so a session backend can
// keep its in-memory journal in step; nil means the journal is fully retired. On a revert
// error the return is nil and the file keeps everything it had.
//
// revert is injected — revertSparingLiveSiblings' closure over revertLabelJournal in
// production, which is Windows-tagged — so the
// retention rule itself is table-testable on any OS, the same seam every other decision in this
// file is behind. path may be "" for a backend that keeps no journal file: there is then nothing
// to remove or rewrite and the revert outcome passes through unchanged.
func retireLabelJournal(path string, j labelJournal, revert func(labelJournal) ([]labelJournalEntry, error)) ([]labelJournalEntry, error) {
	remaining, err := revert(j)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return remaining, nil
	}
	if len(remaining) > 0 {
		if err := writeLabelJournal(path, labelJournal{PID: j.PID, Entries: remaining}); err != nil {
			return nil, err
		}
		return remaining, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("apogee: confine: remove the label journal %q: %w", path, err)
	}
	return nil, nil
}

// clearTreeOutcome is clearLabelTree's below-root verdict: nil when failures is zero —
// every descendant is verifiably cleared or gone — else an error carrying the count and the
// first failure. Returning an error is what makes retireLabelJournal KEEP the journal, so
// labels the walk could not remove stay recorded, ConfinementResidue reports them meanwhile,
// and the next session or recovery retries them; a nil verdict over remaining failures would
// retire the journal above labels still on the disk (ADR 0020 §2's "verifiably reverted").
// It is pure so the accounting is table-testable on any OS — the retireLabelJournal seam
// pattern.
func clearTreeOutcome(root string, failures int, first error) error {
	if failures == 0 {
		return nil
	}
	return fmt.Errorf("apogee: confine: %d path(s) under %q could not be cleared of the mandatory label (first failure: %w)",
		failures, root, first)
}

// readLabelJournal loads one journal file.
func readLabelJournal(path string) (labelJournal, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return labelJournal{}, err
	}
	var j labelJournal
	if err := json.Unmarshal(raw, &j); err != nil {
		return labelJournal{}, fmt.Errorf("apogee: confine: decode label journal %q: %w", path, err)
	}
	return j, nil
}

// listLabelJournals returns the journal files under home, sorted, or nil when the directory
// does not exist — the normal case on every OS but Windows and on a Windows host that has
// never confined anything.
func listLabelJournals(home string) []string {
	entries, err := os.ReadDir(labelJournalDir(home))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, labelJournalPrefix) || !strings.HasSuffix(name, labelJournalSuffix) {
			continue
		}
		out = append(out, filepath.Join(labelJournalDir(home), name))
	}
	sort.Strings(out)
	return out
}

// siblingLabelJournals reads every journal under home EXCEPT the one at own — the other
// sessions whose journals may still claim a root the caller is about to clear. An
// undecodable sibling is skipped: it names no owner to check alive and no roots to spare,
// and erring toward clearing is the safe direction — a cleared label is less privilege,
// never more (the posture recoverLabelJournals takes with the same file). home may be ""
// (no resolvable user profile), where no journal exists and there is nothing to read.
func siblingLabelJournals(home, own string) []labelJournal {
	if home == "" {
		return nil
	}
	var out []labelJournal
	for _, path := range listLabelJournals(home) {
		if strings.EqualFold(path, own) {
			continue
		}
		j, err := readLabelJournal(path)
		if err != nil {
			continue
		}
		out = append(out, j)
	}
	return out
}

// revertibleRoots returns the journalled roots a revert may clear: j's roots minus every
// root also named (Root == true) by a sibling journal whose owning process is still ALIVE.
// Two sessions confining one workspace journal the same root, and the first to tear down
// must not strip the label out from under the survivor — its memoised label pass would never
// re-label, and every later confined write in that session would be denied.
//
// A spared root is NOT a failed revert and must not keep this journal: the live sibling's
// own journal names the root as a Root entry, so the clear obligation lives on in THAT
// journal — its teardown or, after a crash, recovery clears the root once no live session
// claims it — and this journal may still retire. A DEAD sibling spares nothing: its journal
// is an interrupted run whose roots recovery will clear anyway, and clearing them here first
// is the same idempotent operation.
//
// Roots are compared case-folded (foldLabelPath): C:\Work and c:\work name one location.
// alive is injected (processAlive in production, which is Windows-tagged) so the decision is
// table-testable on any OS — the retireLabelJournal seam pattern.
func revertibleRoots(j labelJournal, siblings []labelJournal, alive func(int) bool) []string {
	claimed := make(map[string]bool)
	for _, sibling := range siblings {
		if !alive(sibling.PID) {
			continue
		}
		for _, root := range sibling.roots() {
			claimed[foldLabelPath(root)] = true
		}
	}
	roots := j.roots()
	if len(claimed) == 0 {
		return roots
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if claimed[foldLabelPath(root)] {
			continue
		}
		out = append(out, root)
	}
	return out
}

// restorablePriors splits j's prior-label restores into what may be restored NOW and what
// must be HANDED OFF to a later run: a prior sitting at or under a root any sibling journal
// still names as a Root entry is deferred, everything else is restored by this revert.
//
// The deferral is what keeps a foreign prior on a shared root from being lost to sibling
// teardown ordering. Restoring it while the sibling journal's clear obligation is
// undischarged would first overwrite the Low label the sibling's session may still be fenced
// by, and then be wiped anyway by that sibling's clearLabelTree — at its own teardown, or at
// recovery once it is dead — with no record left anywhere, because the sibling saw only
// apogee's own Low label and journalled no prior. Liveness is deliberately NOT consulted
// here, unlike revertibleRoots: a sibling journal FILE is the undischarged claim whether its
// owner is alive (its Close will clear) or dead (recovery will), and either clear destroys a
// label restored now. The handed-off entries are returned VERBATIM — Root flag included, so
// the surviving journal still anchors the residue report and the eventual re-clear — and
// retireLabelJournal persists them as the journal's remains; the restore then happens at the
// first construction after the claiming journals are gone (ADR 0020 §2's "the journal
// survives until a real session's constructor finishes the restore").
//
// Containment is the case-folded whole-path prefix: a descendant's journalled path is the
// label walk's own spelling — the root plus its relative path — so the lexical test is exact
// here and nothing needs re-resolution.
func restorablePriors(j labelJournal, siblings []labelJournal) (restore map[string]string, handoff []labelJournalEntry) {
	var claimed []string
	for _, sibling := range siblings {
		for _, root := range sibling.roots() {
			claimed = append(claimed, foldLabelPath(root))
		}
	}
	if len(claimed) == 0 {
		return j.priorLabels(), nil
	}
	underClaim := func(path string) bool {
		folded := foldLabelPath(path)
		for _, root := range claimed {
			if folded == root || strings.HasPrefix(folded, root+`\`) {
				return true
			}
		}
		return false
	}
	restore = make(map[string]string, len(j.Entries))
	for _, entry := range j.Entries {
		if entry.PriorSDDL == "" {
			continue
		}
		if underClaim(entry.Path) {
			handoff = append(handoff, entry)
			continue
		}
		restore[entry.Path] = entry.PriorSDDL
	}
	return restore, handoff
}

// ConfinementResidue reports mandatory-label journals left by a run that did not get to
// revert them — the Windows-specific line ADR 0021's host report gains (ADR 0020 §2). It
// returns "" when there is nothing outstanding, which is every OS but Windows and the normal
// case on Windows, so the caller can state it unconditionally.
//
// It takes no home: the journals live where the backend writes them (confinementJournalHome),
// not under the session's configured root, and a caller that could name a root could name the
// wrong one and report residue-free a disk that is not.
func ConfinementResidue() string { return confinementResidue(confinementJournalHome()) }

// confinementResidue is ConfinementResidue against a given home, so the reporting rules are
// testable against a temporary directory.
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
func confinementResidue(home string) string {
	if home == "" {
		return ""
	}
	var roots, unreadable []string
	for _, path := range listLabelJournals(home) {
		j, err := readLabelJournal(path)
		if err != nil {
			unreadable = append(unreadable, path)
			continue
		}
		if j.PID == os.Getpid() {
			continue
		}
		roots = append(roots, j.roots()...)
	}
	return windowsResidueNotice(roots, unreadable)
}

// windowsLabelRemedy is ADR 0020 §2's manual undo — an explicit Medium label is behaviourally
// identical to no label at all. Every surface that reports outstanding labels quotes THIS
// string, so the off-session host report and the end-of-session teardown warning hand the user
// one remedy rather than two spellings of it.
const windowsLabelRemedy = "icacls <path> /setintegritylevel (OI)(CI)M /T /C"

// windowsResidueIndent aligns a continuation line under the host report's "labels:" field,
// which renders the notice verbatim (probe.Host.Report).
const windowsResidueIndent = "                 "

// windowsResidueNotice words the outstanding-journal findings, or "" when there are none. It
// is pure so the wording is table-testable on any host, and it names ADR 0020's manual
// remedy — an explicit Medium label is behaviourally identical to no label at all.
//
// The two findings are worded separately because their remedies genuinely differ: labels a
// readable journal describes are reverted by the next session automatically, whereas an
// UNREADABLE journal is a dead end for every automatic path — recovery skips what it cannot
// decode — so the manual undo is the only remedy there is.
func windowsResidueNotice(roots, unreadable []string) string {
	var lines []string
	if len(roots) > 0 {
		lines = append(lines,
			fmt.Sprintf("%d path(s) may still carry apogee's Low integrity label: %s", len(roots), strings.Join(roots, ", ")),
			"(a run was interrupted, or another apogee holds them now; a new session",
			fmt.Sprintf("reverts them automatically, or: %s)", windowsLabelRemedy))
	}
	if len(unreadable) > 0 {
		for _, path := range unreadable {
			lines = append(lines, fmt.Sprintf("journal present but unreadable: %s", path))
		}
		lines = append(lines,
			"(a crash or an edit left it undecodable, so no run can revert what it names;",
			fmt.Sprintf("delete it once the paths it covered are back to: %s)", windowsLabelRemedy))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n"+windowsResidueIndent)
}

// WindowsLabelProgressNotice words the "please wait" line the composition root prints on stderr
// before the first Low-labelling walk of root, so the one-time pass (ADR 0020 §2) stops being a
// silent hang on a workspace with a large .git or node_modules — the click-through-frustration
// trap the auto-confinement work was built to avoid.
//
// It takes NO object count on purpose: labelTree streams via filepath.WalkDir, so a pre-count is
// a second full walk that doubles the ~1 ms/object cost, and an after-the-fact summary prints
// after the wait it was meant to explain. So the notice is indeterminate and upfront. It is
// worded as the fence doing its job, never as a malfunction, matching probe.DegradedNotice's
// tone, and it quotes windowsLabelRemedy verbatim where it names the manual undo, so the wait
// notice, the teardown warning and the host report all hand the user ONE spelling of the remedy
// rather than three. Pure, so the wording is table-testable on any host.
func WindowsLabelProgressNotice(root string) string {
	return fmt.Sprintf(
		"apogee: labelling the workspace %s Low so the confined child can write in it; "+
			"a large .git or node_modules may take several seconds (undo manually: %s)",
		root, windowsLabelRemedy)
}

// ConfinementTeardownNotice words a confinement teardown that could not put the disk back, for
// the composition root to print on stderr at shutdown — the one moment the user can still act
// on it. It returns "" when err is nil, so the caller can state it unconditionally, exactly as
// it does with ConfinementResidue and the degradation notice.
//
// err is the backend's Close error; on Windows it already names the journal that SURVIVED the
// failed revert, which is what makes the labels recoverable. The remedy is windowsResidueNotice's
// verbatim, because this is the same situation seen from inside the session that caused it.
func ConfinementTeardownNotice(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("apogee: WARNING — confinement teardown did not put every path back: %v; "+
		"a new session reverts them automatically, or: %s", err, windowsLabelRemedy)
}
