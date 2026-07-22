//go:build !linux && !darwin && !windows

package platform

import "github.com/airiclenz/apogee/internal/domain"

// NewConfiner returns the host's Confiner backend on an OS with no real confinement facility
// (the three that have one — Linux landlock, macOS seatbelt, the Windows restricted-token
// backend — each select their own): denyConfiner, which reports {false, false} so the
// dispatch disposition gates the subprocess surface rather than confining it (Auto is not
// refused — ADR 0012). The selector is build-tagged per OS because the real constructors do
// not exist outside their OS.
func NewConfiner() domain.Confiner { return NewDenyConfiner() }

// NewReportConfiner returns the backend `apogee probe host` describes (ADR 0021 §1). Here it
// is NewConfiner verbatim: denyConfiner enforces nothing and constructs nothing, so there is
// no side effect for a read-only caller to opt out of. The split exists for Windows, whose
// session constructor finishes an interrupted run's restore and whose report constructor must
// not (confiner_windows.go).
func NewReportConfiner() domain.Confiner { return NewConfiner() }
