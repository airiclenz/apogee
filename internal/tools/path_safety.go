package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// safeOpen opens input for reading within root through the shared TOCTOU-safe guard, with
// the workspace fence enforced at OPEN time (os.Root-pinned). The returned handle pins the
// file's identity: what is statted and read through it is the file that was opened,
// regardless of any rename after. The caller owns Close and any size policy.
func safeOpen(input, root string) (*os.File, error) {
	return security.SafeOpen(root, input)
}

// statInRoot stats path within root through ONE pinned descriptor: the file is opened
// through the workspace fence (os.Root-pinned) and the FileInfo is an fstat of THAT
// descriptor, so what is described is what was opened. It replaces the resolveInRoot +
// os.Stat pair, whose second half re-walked the path string and would follow a component
// swapped to point outside the workspace after the check passed (the H1 check-then-use gap).
// A directory opens successfully and is reported by its FileInfo, so each caller keeps its
// own "not a file" / "not a directory" wording.
func statInRoot(path, root string) (os.FileInfo, error) {
	f, err := safeOpen(path, root)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// workspaceRelative renders an already-resolved absolute path in its workspace-relative
// form — the short name a tool both DISPLAYS and OPENS the file by. It measures against the
// SYMLINK-RESOLVED root because resolveInRoot returns a real path: on a box where the root is
// reached through a symlink (macOS /tmp) a plain Rel against the configured root would answer
// with a "../.."-laden path. Anything that still will not relativise falls back to the
// absolute path, which is longer but never wrong — and which a fenced open then accepts only
// if it is genuinely inside the root, the safe direction.
func workspaceRelative(path, root string) string {
	rel, err := filepath.Rel(security.EvalRealPath(filepath.Clean(root)), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// readWorkspaceFileBounded reads path within root through ONE pinned handle: open through
// the fence, fstat the opened descriptor, refuse a directory or an over-cap size, then
// read through a limit bounded to maxFileReadBytes+1 — the size check and the read cannot
// disagree about which file they describe, and no more than the cap+1 bytes are ever
// materialised even if the file grows mid-call (the growth backstop re-fstats the SAME fd
// for the size it reports). On failure the second return is the model-facing message,
// rendered exactly as the read tools rendered it before; on success it is empty.
func readWorkspaceFileBounded(path, root string) ([]byte, string) {
	f, err := safeOpen(path, root)
	if err != nil {
		return nil, readFileErrorMessage(err, path)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, readFileErrorMessage(err, path)
	}
	if info.IsDir() {
		return nil, "not a file: " + path
	}
	if info.Size() > maxFileReadBytes {
		return nil, fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxFileReadBytes)
	}

	data, within, err := readAllBounded(f, maxFileReadBytes)
	if err != nil {
		return nil, readFileErrorMessage(err, path)
	}
	if !within {
		// The file grew past the cap between the fstat above and the read. Report the
		// refusal with a fresh size from the same descriptor, falling back to the bytes
		// actually drained if even that fstat fails.
		size := int64(len(data))
		if fresh, statErr := f.Stat(); statErr == nil {
			size = fresh.Size()
		}
		return nil, fmt.Sprintf("file too large: %d bytes (max %d)", size, maxFileReadBytes)
	}
	return data, ""
}

// readAllBounded reads at most max bytes from r, reporting within=false when r holds more —
// the max+1 idiom skills/load.go readBounded uses, here as its own step so the growth
// backstop is table-testable without interleaving a writer. data holds what was drained
// (max+1 bytes when within is false); a non-nil err is a genuine read failure, under which
// within is meaningless.
func readAllBounded(r io.Reader, max int64) (data []byte, within bool, err error) {
	data, err = io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) <= max, nil
}

// readFileErrorMessage renders a safeReadFile failure for the model: a path that escapes
// the workspace surfaces the uniform escape message (not "file not found", which would
// hide the refusal), while any other read error (a genuinely missing file) keeps the
// "file not found" phrasing the write tools used before the H1 fix.
func readFileErrorMessage(err error, path string) string {
	return escapeOrMessage(err, "file not found: "+path)
}

// escapeOrMessage renders a fenced-I/O failure for the model: a refusal by the workspace
// fence surfaces the uniform escape message, NEVER disguised as absence — a refusal reported
// as "not found" reads to the model as a missing file and invites a retry, hiding the one
// thing the host wants said out loud. Any other error keeps the caller's own phrasing for an
// absent path, which differs per tool ("file not found", "directory not found").
func escapeOrMessage(err error, absent string) string {
	if errors.Is(err, ErrPathEscape) {
		return err.Error()
	}
	return absent
}
