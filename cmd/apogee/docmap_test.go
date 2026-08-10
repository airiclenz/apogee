package main

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in the composition root is missing from doc.go's
// file map — including a wire_*.go seam file, which is exactly where the junk drawer would
// otherwise re-form unannounced.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
