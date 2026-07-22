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
const (
	labelJournalDirName = "confinement"
	labelJournalPrefix  = "labels-"
	labelJournalSuffix  = ".json"
)

// labelJournal is the on-disk record written BEFORE the first mandatory label is applied
// (ADR 0020 §2), so an apogee that is killed mid-run leaves behind enough to undo the disk
// mutation: the next NewConfiner finishes the restore, and `apogee probe host` reports the
// journal so an interrupted cleanup is diagnosable off-session.
type labelJournal struct {
	// PID owns this journal. A journal whose process is still alive belongs to a running
	// apogee and must never be recovered by another one.
	PID int `json:"pid"`
	// Entries are the labelled roots (Root == true) plus every path found already carrying
	// an explicit label, whose prior descriptor teardown puts back verbatim.
	Entries []labelJournalEntry `json:"entries"`
}

// labelJournalEntry is one journalled path: a box root the backend labelled, and/or a path
// that already carried a mandatory label before the run.
type labelJournalEntry struct {
	Path string `json:"path"`
	// Root marks a box root whose whole tree teardown walks and clears.
	Root bool `json:"root,omitempty"`
	// PriorSDDL is the descriptor the path carried before labelling, empty when it carried
	// no label (the overwhelmingly common case) and teardown should clear it instead.
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
func writeLabelJournal(path string, j labelJournal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("apogee: confine: create label journal dir: %w", err)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("apogee: confine: encode label journal: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("apogee: confine: write label journal: %w", err)
	}
	return nil
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
func confinementResidue(home string) string {
	if home == "" {
		return ""
	}
	var roots []string
	for _, path := range listLabelJournals(home) {
		j, err := readLabelJournal(path)
		if err != nil || j.PID == os.Getpid() {
			continue
		}
		roots = append(roots, j.roots()...)
	}
	return windowsResidueNotice(roots)
}

// windowsResidueNotice words the outstanding-journal finding, or "" when there is none. It
// is pure so the wording is table-testable on any host, and it names ADR 0020's manual
// remedy — an explicit Medium label is behaviourally identical to no label at all.
func windowsResidueNotice(roots []string) string {
	if len(roots) == 0 {
		return ""
	}
	return fmt.Sprintf("%d path(s) may still carry apogee's Low integrity label: %s\n"+
		"                 (a run was interrupted, or another apogee holds them now; a new session\n"+
		"                 reverts them automatically, or: icacls <path> /setintegritylevel (OI)(CI)M /T /C)",
		len(roots), strings.Join(roots, ", "))
}
