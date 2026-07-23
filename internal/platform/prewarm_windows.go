//go:build windows

package platform

import (
	"fmt"
	"io"

	"github.com/airiclenz/apogee/internal/domain"
)

// PrewarmLabelWalk eagerly runs the Windows token backend's one-time Low-label walk of
// workspaceRoot at startup (ADR 0020 §2), printing WindowsLabelProgressNotice to out FIRST so the
// ~1 ms/object pass over a large .git or node_modules reads as an explained wait rather than a
// silent hang on the first confined command — the click-through-frustration trap Auto was built to
// avoid. The composition root calls it pre-alt-screen, where a raw stderr write is safe
// (cmd/apogee/wire.go); the first in-session Confine of the same workspace then hits the c.labelled
// memo and no-ops, so the disk mutation's TIMING moves to startup while WHAT is labelled — and
// Close's revert at shutdown — is unchanged (the plan's "keep semantics").
//
// It is a no-op unless c is the Windows token backend with a minted Low token (FSWrite == true): a
// below-floor host, a token-mint failure, or any other backend returns immediately and prints
// nothing — the same FSWrite == false hosts probe.DegradedNotice speaks for, and the caller's
// untagged trigger already gates on FSWrite. The walk is best-effort: a box the guardrails refuse
// or a journal that cannot be written surfaces at the first real Confine as a forced Gate exactly
// as it does today, so a labelBox failure here is dropped rather than aborting startup. It labels
// ONLY the workspace root; an extra writable root added mid-session still walks lazily and
// silently, which is acceptable since the workspace is the bulk (the plan's approach A). labelBox
// and labelTree are untouched — this is purely an emission seam.
func PrewarmLabelWalk(c domain.Confiner, workspaceRoot string, out io.Writer) {
	tc, ok := c.(*tokenConfiner)
	if !ok || !tc.caps.FSWrite || tc.token == 0 {
		return
	}
	fmt.Fprintln(out, WindowsLabelProgressNotice(workspaceRoot))
	_ = tc.labelBox(domain.ConfinementBox{WorkspaceRoot: workspaceRoot})
}
