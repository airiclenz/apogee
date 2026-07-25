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
