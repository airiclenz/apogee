package floor

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// The guards are the behaviour every model runs with whether or not it asked for it, so a file the
// map never mentions is a decision nobody knows to look for.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
