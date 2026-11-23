//go:build windows

package config

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/realpath/junctiontest"
)

// An exec pin's file named through a junction inside the Project root that points outside it is
// not a project file: it pins nothing, while a file named directly still pins.
func TestExecPinJunctionLeavingTheRootPinsNothing(t *testing.T) {
	root := longTempDir(t)
	outside := longTempDir(t)
	writeProjectFile(t, root, "scripts/fmt.sh", "fmt\n")
	writeProjectFile(t, outside, "evil.sh", "evil\n")
	junctiontest.MakeOrSkip(t, filepath.Join(root, "j"), outside)

	entry := reactionPin(t, root, "id: fmt\nrun: [sh, scripts/fmt.sh, j/evil.sh]\n")

	if got := pinnedPaths(t, entry.Text); !slices.Equal(got, []string{"scripts/fmt.sh"}) {
		t.Errorf("pinned files = %v, want [scripts/fmt.sh]", got)
	}
}

// A file named through a junction whose target stays inside the Project root is a project file
// and keeps pinning, under the path the entry spells.
func TestExecPinJunctionInsideTheRootKeepsPinning(t *testing.T) {
	root := longTempDir(t)
	writeProjectFile(t, root, "real/fmt.sh", "fmt\n")
	junctiontest.MakeOrSkip(t, filepath.Join(root, "j"), filepath.Join(root, "real"))

	entry := reactionPin(t, root, "id: fmt\nrun: [sh, j/fmt.sh]\n")

	if got := pinnedPaths(t, entry.Text); !slices.Equal(got, []string{"j/fmt.sh"}) {
		t.Errorf("pinned files = %v, want [j/fmt.sh]", got)
	}
}
