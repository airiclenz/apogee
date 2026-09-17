//go:build linux

package platform

import "github.com/airiclenz/apogee/internal/domain"

// Linux Confiner selector (confinement-execution-contract §2.6): two rungs, tried in order.
//
//  1. landlock (landlock_linux.go) — the kernel enforces the box on the child's own
//     process, no external binary, network egress fenced from ABI 4. It wins whenever it
//     can fence writes (ABI >= 1), even on a host where bwrap would also work.
//  2. namespace (namespace_linux.go) — user + mount namespaces through bwrap, for the
//     kernels that ship without landlock (Raspberry Pi OS, ENOSYS containers). It is an
//     optional external enhancement (ADR 0042 §4) and its construction probe forks bwrap
//     once for real, so the rung is only ever climbed when landlock has already said no.
//
// When neither fences, the namespace backend is returned anyway, carrying BOTH reasons in
// Capabilities().Unavailable ("landlock unavailable (...); bwrap not on PATH"), so every
// wording surface — the startup notice, `apogee probe host`, /confine — names the namespace
// backend and says what would have to change on this host. The dispatch disposition then
// gates the subprocess surface rather than confining it (Auto is not refused — ADR 0012).
// The selector is build-tagged per OS because both constructors are linux-only.

// NewConfiner returns the host's real Confiner backend for this OS: landlock when it can
// fence, else the namespace backend, else the namespace backend disclosing both reasons
// (see the selector order above). The caps are probed once at construction.
func NewConfiner() domain.Confiner {
	return selectLinuxConfiner(NewLandlockConfiner(), NewNamespaceConfiner)
}

// NewReportConfiner returns the backend `apogee probe host` describes (ADR 0021 §1). On Linux
// it is NewConfiner verbatim: landlock's box is a ruleset handed to the kernel and the
// namespace backend's probe is one bwrap launch of a no-op shell, so nothing about
// constructing either backend touches the user's disk and there is nothing for a read-only
// caller to opt out of. The split exists for Windows, whose session constructor finishes an
// interrupted run's restore and whose report constructor must not (confiner_windows.go).
func NewReportConfiner() domain.Confiner { return NewConfiner() }

// selectLinuxConfiner applies the rung order to an already-constructed landlock backend and
// a namespace constructor. The namespace backend is built lazily — newNS is called only when
// landlock cannot fence — because its probe is a real fork of bwrap and a landlock host has
// no reason to pay for it. The order decision lives in ONE place so the session and report
// selectors cannot disagree about which backend a host gets.
func selectLinuxConfiner(ll *landlockConfiner, newNS func() *namespaceConfiner) domain.Confiner {
	if ll.Capabilities().FSWrite {
		return ll
	}
	ns := newNS()
	if ns.Capabilities().FSWrite {
		return ns
	}
	ns.unavailable = ll.unavailableReason() + "; " + ns.unavailable
	return ns
}
