package platform

import "github.com/airiclenz/apogee/internal/realpath"

// RealPath returns p with every link resolved, a Windows junction included. It is
// realpath.Resolve, kept under this name for the callers that have not moved to the leaf yet.
//
// Errors: realpath.Resolve's own, unchanged (fs.ErrNotExist-classed for a missing path).
func RealPath(p string) (string, error) {
	return realpath.Resolve(p)
}
