package probe

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// The package holds three halves that must stay legibly apart — the read-only host report, the
// battery that spends real tokens, and the terminal measurement — so the map is where a reader
// learns which half a file belongs to before opening it.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
