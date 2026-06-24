//go:build !linux && !darwin

package platform

import "github.com/airiclenz/apogee/internal/domain"

// NewConfiner returns the host's Confiner backend on an OS with no real confinement
// facility yet (Windows AppContainer is Phase 5): denyConfiner, which reports
// {false, false} so the dispatch disposition gates the subprocess surface rather than
// confining it (Auto is not refused — ADR 0012). The selector is build-tagged per OS
// because the landlock / seatbelt constructors do not exist outside their OS.
func NewConfiner() domain.Confiner { return NewDenyConfiner() }
