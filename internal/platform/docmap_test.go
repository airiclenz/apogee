package platform

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// Half the files here compile on one OS only, so the map is the only place a reader on any host
// can see the whole backend set at once — a build-tagged file left out of it is invisible.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
