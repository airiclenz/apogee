//go:build !windows

package platform

import (
	"io"

	"github.com/airiclenz/apogee/internal/domain"
)

// PrewarmLabelWalk is the no-op the Windows label-walk pre-warm collapses to on every non-Windows
// host. The composition root calls it unconditionally once its untagged trigger (Auto ∧
// confine-asked ∧ FSWrite) fires, and only the Windows token backend expresses its box as a
// mandatory disk label whose walk is worth hoisting to startup (ADR 0020 §2): the Linux landlock
// and macOS seatbelt backends hand the kernel a ruleset or profile and touch no disk, so there is
// no walk to pre-warm and nothing to print. Keeping the seam here — rather than a build tag at the
// composition root — is what keeps startup output byte-identical off Windows even where the
// trigger is true (landlock and seatbelt both report FSWrite == true under Auto+confine).
func PrewarmLabelWalk(_ domain.Confiner, _ string, _ io.Writer) {}
