package processing

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// The parse seam is one file per tool-call format and one per thinking style, and a reader arrives
// here asking which of them owns the shape in front of them: an unlisted file is a format nobody
// can find.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
