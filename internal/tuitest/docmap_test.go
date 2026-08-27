package tuitest

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map —
// the house rule for a package past ~10 files, which this one now is.
func TestDocMapNamesEveryFile(t *testing.T) {
	docmap.Check(t)
}
