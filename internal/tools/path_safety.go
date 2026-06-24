package tools

import (
	"errors"
	"os"

	"github.com/airiclenz/apogee/internal/security"
)

// Path-safety is consolidated into the shared internal/security guard (P3.6 / D6):
// one symlink-aware, traversal-rejecting boundary every guarded tool inherits, in
// every mode. These package-local aliases keep the built-in tools (and their tests)
// calling the same names while the implementation lives in one place. Behaviour is
// unchanged — security.ResolveInRoot is the verbatim move of the former local code.

// ErrPathEscape is returned when a tool argument resolves to a path outside the
// sandbox root. It is the security guard's sentinel, re-exported here so existing
// errors.Is(err, ErrPathEscape) checks in the tools and their tests keep matching.
var ErrPathEscape = security.ErrPathEscape

// resolveInRoot resolves input within root via the shared path-safety guard, returning
// ErrPathEscape for a path that escapes the workspace (symlinks followed).
func resolveInRoot(input, root string) (string, error) {
	return security.ResolveInRoot(input, root)
}

// safeWriteFile writes data to input within root through the shared TOCTOU-safe guard:
// the workspace fence is enforced at WRITE time (os.Root-pinned), so a symlinked path
// component swapped to point outside the root — including a concurrent swap by a confined
// subprocess — is refused rather than followed (security review H1). It replaces the
// former resolveInRoot+os.WriteFile pair, which re-walked the path with a check/use gap.
func safeWriteFile(input, root string, data []byte, perm os.FileMode) error {
	return security.SafeWriteFile(root, input, data, perm)
}

// safeReadFile reads input within root through the shared TOCTOU-safe guard, with the
// workspace fence enforced at READ time so an escaping symlink component is refused
// rather than followed (security review H1). It replaces the former resolveInRoot+
// os.ReadFile pair for the write tools' read-modify-write step.
func safeReadFile(input, root string) ([]byte, error) {
	return security.SafeReadFile(root, input)
}

// readFileErrorMessage renders a safeReadFile failure for the model: a path that escapes
// the workspace surfaces the uniform escape message (not "file not found", which would
// hide the refusal), while any other read error (a genuinely missing file) keeps the
// "file not found" phrasing the write tools used before the H1 fix.
func readFileErrorMessage(err error, path string) string {
	if errors.Is(err, ErrPathEscape) {
		return err.Error()
	}
	return "file not found: " + path
}
