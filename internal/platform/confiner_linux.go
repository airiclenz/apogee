//go:build linux

package platform

import "github.com/airiclenz/apogee/internal/domain"

// NewConfiner returns the host's real Confiner backend for this OS (confinement-execution
// -contract §2.6): the Linux landlock backend. Its caps are probed once at construction,
// so a kernel without landlock reports {false, false} and the dispatch disposition gates
// the subprocess surface rather than confining it (Auto is not refused — ADR 0012). The
// selector is build-tagged per OS because NewLandlockConfiner is linux-only.
func NewConfiner() domain.Confiner { return NewLandlockConfiner() }
