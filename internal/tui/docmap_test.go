package tui

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's narration.
// The narration IS the map here — it names every file in the package and says what each one is
// for — so a file it never mentions is a file nobody can find by reading the package.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
