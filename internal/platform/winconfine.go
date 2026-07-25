package platform

import (
	"fmt"
	"os"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/winlabel"
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
func retireLabelJournal(path string, j winlabel.Record, revert func(winlabel.Record) ([]winlabel.Entry, error)) ([]winlabel.Entry, error) {
	remaining, err := revert(j)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return remaining, nil
	}
	if len(remaining) > 0 {
		if err := winlabel.WriteJournal(path, winlabel.Record{PID: j.PID, Entries: remaining}); err != nil {
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
// Roots are compared case-folded (winlabel.FoldPath): C:\Work and c:\work name one location.
// alive is injected (processAlive in production, which is Windows-tagged) so the decision is
// table-testable on any OS — the retireLabelJournal seam pattern.
func revertibleRoots(j winlabel.Record, siblings []winlabel.Record, alive func(int) bool) []string {
	claimed := make(map[string]bool)
	for _, sibling := range siblings {
		if !alive(sibling.PID) {
			continue
		}
		for _, root := range sibling.Roots() {
			claimed[winlabel.FoldPath(root)] = true
		}
	}
	roots := j.Roots()
	if len(claimed) == 0 {
		return roots
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if claimed[winlabel.FoldPath(root)] {
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
func restorablePriors(j winlabel.Record, siblings []winlabel.Record) (restore map[string]string, handoff []winlabel.Entry) {
	var claimed []string
	for _, sibling := range siblings {
		for _, root := range sibling.Roots() {
			claimed = append(claimed, winlabel.FoldPath(root))
		}
	}
	if len(claimed) == 0 {
		return j.PriorLabels(), nil
	}
	underClaim := func(path string) bool {
		folded := winlabel.FoldPath(path)
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
// It takes no home: the journals live where the backend writes them (winlabel.Home), not
// under the session's configured root, and a caller that could name a root could name the
// wrong one and report residue-free a disk that is not.
//
// The journal it reads, and the reporting rules, live in winlabel (Residue); this export is
// the name `apogee probe host` already knows.
func ConfinementResidue() string { return winlabel.Residue() }

// WindowsLabelProgressNotice words the "please wait" line the composition root prints on stderr
// before the first Low-labelling walk of root, so the one-time pass (ADR 0020 §2) stops being a
// silent hang on a workspace with a large .git or node_modules — the click-through-frustration
// trap the auto-confinement work was built to avoid.
//
// The wording itself lives in winlabel (ProgressNotice), beside the label mechanism it
// describes and the one spelling of the manual remedy every surface quotes; this export is the
// name the composition root already knows.
func WindowsLabelProgressNotice(root string) string { return winlabel.ProgressNotice(root) }

// ConfinementTeardownNotice words a confinement teardown that could not put the disk back, for
// the composition root to print on stderr at shutdown — the one moment the user can still act
// on it. It returns "" when err is nil, so the caller can state it unconditionally, exactly as
// it does with ConfinementResidue and the degradation notice.
//
// The wording itself lives in winlabel (TeardownNotice), beside the residue report it must not
// drift from.
func ConfinementTeardownNotice(err error) string { return winlabel.TeardownNotice(err) }
