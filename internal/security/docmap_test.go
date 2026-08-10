package security

import (
	"testing"

	"github.com/airiclenz/apogee/internal/docmap"
)

// TestDocMapNamesEveryFile fails when a file in this package is missing from doc.go's file map.
// Every file here is a guard, and the guards split check-from-use in two places (path vs safeio,
// url vs ssrf): a half that goes undescribed is a half a reader can call without knowing the other
// exists.
func TestDocMapNamesEveryFile(t *testing.T) {
	t.Parallel()

	docmap.Check(t)
}
