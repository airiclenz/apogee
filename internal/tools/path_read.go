package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/security"
)

// The READ half of path-safety, carved out of path_safety.go (ADR 0043): the one-handle bounded
// read every read tool goes through, the model-facing wording a fenced failure is rendered as, and
// readScope — the READ-only multi-root resolver that tries the workspace first and then any extra
// read-only roots the host mounts. path_safety.go keeps the fenced primitives themselves and the
// approved escape's write-side pins (ADR 0049); nothing in this file takes a permit, which is that
// decision's write-side-only rule where it is enforceable.

// workspaceRelative renders an already-resolved absolute path in its workspace-relative
// form — the short name a tool both DISPLAYS and OPENS the file by — through the shared
// path-safety guard, so the doc server's own per-request fence (internal/present) derives the
// same name from the same rules rather than keeping a second copy of them.
func workspaceRelative(path, root string) string {
	return security.WorkspaceRelative(path, root)
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
//
// A workspace root that will not open is answered in its OWN words, ahead of both: the file
// the model named may well exist, and "file not found" would send it hunting for a path while
// the real fault — a root deleted or made unreadable under the session — goes unsaid.
func escapeOrMessage(err error, absent string) string {
	if errors.Is(err, security.ErrRootInaccessible) {
		return err.Error()
	}
	if errors.Is(err, ErrPathEscape) {
		return err.Error()
	}
	return absent
}

// readScope resolves the path argument of a READ-ONLY tool over the workspace root plus any
// extra read-only roots the host configured. It is a generic seam: a skills library is the
// first thing mounted through it, but nothing here knows that (ADR 0031 — engine seams stay
// driver-agnostic). Roots are tried in order, workspace first, and every method returns the
// MATCHED root beside its result so the caller pins each later fenced operation to the root
// the path was accepted under — a listing walk that began in an extra root must not measure
// its children against the workspace.
//
// Two properties are deliberate. Extra roots are reachable by ABSOLUTE path only: a relative
// argument keeps resolving against the workspace root alone, so no one name can mean two
// files. And the zero value — extra nil, or a func returning nothing — behaves exactly as the
// single-root helpers it wraps: the fallback never runs, and a refusal carries the workspace's
// own unchanged message.
//
// The roots are read LIVE, once per call, so a mid-session change on the host's side is
// honoured by the next read with no re-wiring.
//
// READ paths only, with exactly ONE sanctioned crossing: copy_file resolves its SOURCE through a
// scope (2026-08-12), because a copy's source is a read — the bytes are read from whichever root
// accepts the path and written to the workspace root the tool pins itself. No write HALF of any
// tool takes a readScope and none may: every write stays workspace-fenced through the
// workspaceScopedWriter discipline (ADR 0012 D1), so a root mounted here never becomes writable.
type readScope struct {
	// root is the workspace root — always tried first, and the only root a relative path is
	// ever resolved against.
	root string
	// extra reports the extra read-only roots, evaluated once per call. nil means
	// workspace-only.
	extra func() []string
}

// extraRoots evaluates the live extra-root func for ONE call, answering nil when there is
// nothing to fall back on: no func, or a relative input, which extra roots never serve.
func (s readScope) extraRoots(input string) []string {
	if s.extra == nil || !filepath.IsAbs(input) {
		return nil
	}
	return s.extra()
}

// resolve resolves input to a real path within the first root that contains it and returns
// that root beside the resolved path. When no root accepts it, the error is the WORKSPACE's
// ErrPathEscape, so the model reads the one uniform "outside the workspace" refusal whatever
// the extra roots happen to be.
func (s readScope) resolve(input string) (root, resolved string, err error) {
	resolved, err = resolveInRoot(input, s.root)
	if err == nil {
		return s.root, resolved, nil
	}
	if extraRoot, extraResolved, ok := matchRoot(input, s.extraRoots(input)); ok {
		return extraRoot, extraResolved, nil
	}
	return "", "", err
}

// open opens input for reading through the fence of the first root that contains it and
// returns that root beside the handle. A containment refusal falls through to the next root;
// a genuine I/O failure under a root that DOES contain the path is returned as it is, so a
// missing file is never disguised as an escape (nor an escape as a missing file).
//
// A workspace root that will not open is not a containment refusal and does NOT fall through:
// the workspace could not answer the question at all, so an extra root that happened to accept
// the path would silently substitute a different file for the one the caller asked about.
func (s readScope) open(input string) (*os.File, string, error) {
	f, err := safeOpen(input, s.root)
	if err == nil {
		return f, s.root, nil
	}
	if errors.Is(err, security.ErrRootInaccessible) {
		return nil, "", err
	}
	if !errors.Is(err, ErrPathEscape) {
		return nil, "", err
	}
	extraRoot, _, ok := matchRoot(input, s.extraRoots(input))
	if !ok {
		return nil, "", err
	}
	f, extraErr := safeOpen(input, extraRoot)
	if extraErr != nil {
		return nil, "", extraErr
	}
	return f, extraRoot, nil
}

// readBounded reads input through the one-handle bounded read, pinned to the root that
// contains it. It is readWorkspaceFileBounded's contract verbatim — the same cap, the same
// growth backstop, the same model-facing failure message — with the root chosen rather than
// assumed.
func (s readScope) readBounded(input string) ([]byte, string) {
	return readWorkspaceFileBounded(input, s.readRoot(input))
}

// readRoot answers the root a fenced read of input must be pinned to: the workspace when it
// contains input (always, for a relative path), else the first extra root that does. When NO
// root accepts it the answer is still the workspace root — the read then fails THERE, so the
// refusal the caller renders is the workspace's own, exactly as it is without extra roots.
func (s readScope) readRoot(input string) string {
	roots := s.extraRoots(input)
	if len(roots) == 0 {
		return s.root
	}
	if _, err := resolveInRoot(input, s.root); err == nil {
		return s.root
	}
	if extraRoot, _, ok := matchRoot(input, roots); ok {
		return extraRoot
	}
	return s.root
}

// matchRoot reports the first root in roots that is usable AND contains input, with the path
// input resolves to there. A root that is absent or is not a directory — the skills library on
// a box where nothing has created it yet, the creation-deferred convention — is skipped: a
// per-root refusal, never an error of its own.
func matchRoot(input string, roots []string) (root, resolved string, ok bool) {
	for _, candidate := range roots {
		if candidate == "" || !rootUsable(candidate) {
			continue
		}
		candidateResolved, err := resolveInRoot(input, candidate)
		if err != nil {
			continue
		}
		return candidate, candidateResolved, true
	}
	return "", "", false
}

// rootUsable reports whether root can anchor a fenced read: it exists and opens as a
// directory — the very os.Root the read that follows is pinned to.
func rootUsable(root string) bool {
	r, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	_ = r.Close()
	return true
}
