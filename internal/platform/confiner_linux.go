//go:build linux

package platform

import "github.com/airiclenz/apogee/internal/domain"

// NewConfiner returns the host's real Confiner backend for this OS (confinement-execution
// -contract §2.6): the Linux landlock backend. Its caps are probed once at construction,
// so a kernel without landlock reports {false, false} and the dispatch disposition gates
// the subprocess surface rather than confining it (Auto is not refused — ADR 0012). The
// selector is build-tagged per OS because NewLandlockConfiner is linux-only.
func NewConfiner() domain.Confiner { return NewLandlockConfiner() }

// NewReportConfiner returns the backend `apogee probe host` describes (ADR 0021 §1). On Linux
// it is NewConfiner verbatim: landlock's box is a ruleset handed to the kernel, so nothing
// about constructing the backend touches the user's disk and there is nothing for a read-only
// caller to opt out of. The split exists for Windows, whose session constructor finishes an
// interrupted run's restore and whose report constructor must not (confiner_windows.go).
func NewReportConfiner() domain.Confiner { return NewConfiner() }
