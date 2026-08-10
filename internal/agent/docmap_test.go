package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// The engine is the package a Driver author reads first, and its loop is split across files by
// concern; a file the map never mentions is a piece of the loop nobody knows to look in.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
