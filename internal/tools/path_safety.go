package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
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

// refuseExecFromWritablePath refuses a program that resolves inside the writable confinement
// box through the shared exec fence (security.RefuseExecFromWritablePath): every tool that
// resolves an executable on PATH — git, python_exec, run_tests, diagnostics — calls it at the
// resolution site, so bytes the model was allowed to write can never become argv[0]. The error
// names the RESOLVED path, which is what tells an operator their in-repo virtualenv is the
// cause rather than a missing install.
func refuseExecFromWritablePath(argv0, root string, box *domain.ConfinementBox) error {
	return security.RefuseExecFromWritablePath(argv0, root, box)
}

// confinementBox returns the box a confined call runs inside, or nil when no Confinement
// handle rides on ctx — the gated/unconfined case, where the workspace root is the whole
// fence. It is the small read the exec sites need to pass the box on to the fence above.
func confinementBox(ctx context.Context) *domain.ConfinementBox {
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		return &conf.Box
	}
	return nil
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
func escapeOrMessage(err error, absent string) string {
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
func (s readScope) open(input string) (*os.File, string, error) {
	f, err := safeOpen(input, s.root)
	if err == nil {
		return f, s.root, nil
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
