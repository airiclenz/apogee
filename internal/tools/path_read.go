package tools

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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
	defer func() { _ = f.Close() }()
	return readOpenedBounded(f, path)
}

// readOpenedBounded is that contract from the OPEN onwards — the fstat, the directory and cap
// refusals, the bounded read and the growth backstop — over an already-opened handle named by the
// spelling a refusal should quote. It takes an fs.File rather than an *os.File so a virtual mount,
// which has no descriptor to pin, is bounded by the very same rules a disk read is
// (path_virtual.go): one cap, one set of refusal wordings, one growth backstop.
func readOpenedBounded(f fs.File, path string) ([]byte, string) {
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
//
// The path it quotes — like the sibling "not a file" refusal above — is the one the read was
// PINNED to, never the spelling the model happened to use. Under an extra root those differ:
// readScope.locate hands the read the REAL path so the fence's two containment judgements
// agree, so a directory named through a symlinked extra root is refused as "not a file:
// <real path>". That is deliberate and is the contract: the announced-path invariant governs
// which spellings a tool ACCEPTS, not how a refusal quotes what it examined, and quoting the
// resolved path tells the model which file the host actually looked at.
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

// ReadMounts are the read-only trees a read tool resolves a path over BESIDE its own workspace
// root: Roots names extra DISK roots (host paths the operator opened up — a skills library), and
// Virtual names trees that have no host path at all, keyed by the prefix their addresses are
// spelled under (`shipped:` — path_virtual.go). Both are evaluated LIVE, once per tool call, so a
// mid-session change on the host's side is honoured by the next read with no re-wiring.
//
// The ZERO value is workspace-only and is byte-identical to the fence before either seam existed,
// which is why it is the value every test and every tool-less host passes. It is one struct rather
// than two parameters because the two answer ONE question — what else may this tool read — and a
// tool that grows a third kind of mount should not grow a fourth constructor argument.
type ReadMounts struct {
	// Roots reports extra read-only DISK roots, each the host's symlink-RESOLVED real path
	// (readScope's contract). nil ⇒ the workspace root alone.
	Roots func() []string
	// Virtual reports the host's virtual mounts by prefix, colon included. nil ⇒ none.
	Virtual func() map[string]fs.FS
}

// scope builds the resolver a read tool fences itself with: the workspace root, plus whatever
// mounts the host named. It is the ONE place the two halves are paired, so no tool can be wired
// with the disk roots and without the virtual ones.
func (m ReadMounts) scope(root string) readScope {
	return readScope{root: root, extra: m.Roots, virtual: m.Virtual}
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
	// virtual reports the host's virtual read mounts by prefix, evaluated once per call and
	// consulted BEFORE any disk root (path_virtual.go). nil means disk-only.
	virtual func() map[string]fs.FS
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

// locate answers, for ONE read, BOTH halves of the question a fenced read asks: the root the
// input is accepted under, and the spelling of the path to hand a read pinned to that root.
// The order of roots is resolve's, so there is only ever one copy of it.
//
// The two spellings differ because the fence's two containment judgements do. Under the
// WORKSPACE root the input is handed on AS GIVEN, so the read is byte-for-byte what it was
// before extra roots existed — a symlinked workspace root (macOS /tmp) included. Under an EXTRA
// root it is the REAL path matchRoot already resolved, because resolveInRoot judged containment
// on REAL paths while the bounded read's security.rootRelative relativises LEXICALLY: a symlink
// SPELLING of a file under a real mounted root passes the first check and then fails the second,
// which is how a dotfiles-symlinked ~/.apogee/skills came to be refused by read_file while
// list_dir — which resolves — read it (audit 2026-08-28 F-13). An extra root is its own real path
// by matchRoot's contract, so handing over the real path makes the two judgements agree.
//
// When no root accepts the input the error is the WORKSPACE's ErrPathEscape, so the refusal a
// caller renders is the workspace's own whatever the extra roots happen to be.
func (s readScope) locate(input string) (root, target string, err error) {
	root, resolved, err := s.resolve(input)
	if err != nil {
		return "", "", err
	}
	if root == s.root {
		return s.root, input, nil
	}
	return root, resolved, nil
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
// growth backstop, the same model-facing failure message — with the root, and the spelling of
// the path handed to it, chosen rather than assumed (locate).
//
// A path no root accepts is read at the WORKSPACE root under its own name, which is where it
// fails: the refusal the caller renders is then the workspace's own, exactly as it is with no
// extra roots configured.
func (s readScope) readBounded(input string) ([]byte, string) {
	if v, ok := s.virtualLocate(input); ok {
		return v.readBounded()
	}
	root, target, err := s.locate(input)
	if err != nil {
		return readWorkspaceFileBounded(input, s.root)
	}
	data, failure := readWorkspaceFileBounded(target, root)
	return data, withSiblingSuggestions(failure, root, target)
}

// withSiblingSuggestions appends the near misses of an ABSENT file to a bounded read's failure
// message — the one refusal a mis-spelled name can recover from — and hands every other failure
// back untouched. A fence refusal, a directory, an over-cap file: each keeps its own wording,
// because only absence is a spelling the model can fix.
//
// The absent case is recognised by the message readFileErrorMessage renders for it, which is
// also the path this quotes: target is the PINNED spelling the read was performed under, not
// necessarily the model's, so the suggestions are joined onto the parent of the path the host
// actually looked in (path_read.go's "not found quotes what was examined" contract).
func withSiblingSuggestions(failure, root, target string) string {
	const prefix = "file not found: "
	if failure != prefix+target {
		return failure
	}
	return notFoundMessage(prefix, target, suggestSiblings(root, workspaceRelative(target, root), target))
}

// readRoot answers the root a fenced read of input must be pinned to: the workspace when it
// contains input (always, for a relative path), else the first extra root that does. When NO
// root accepts it the answer is still the workspace root — the read then fails THERE, so the
// refusal the caller renders is the workspace's own, exactly as it is without extra roots.
//
// It is locate's root half, and only that: a caller that also opens the file takes both halves
// from locate instead, so the root and the spelling can never be chosen by two different rules.
func (s readScope) readRoot(input string) string {
	root, _, err := s.locate(input)
	if err != nil {
		return s.root
	}
	return root
}

// searchTarget is the resolved subject of ONE walking read tool's call — grep's and find_files',
// which differ in what they do with a file, never in how they find one. It is the whole of what
// those walks need to know about where the target came from: the tree they enumerate, the target's
// own name, and the two renderings a walked name needs — the spelling open takes, and the spelling
// the tool REPORTS. On disk the two coincide (both root-relative); in a virtual mount they do not
// (a walk name opens as itself and reports as `mount:path`), which is exactly why they are two
// funcs and not one.
//
// tree is nil when the target is a single FILE: naming one file is a legal query for both tools,
// and neither walks then.
type searchTarget struct {
	tree       fs.FS
	rel        string
	openName   func(walkRel string) string
	reportName func(walkRel string) string
	open       func(name string) (fs.File, error)
}

// isDir reports whether the target is a directory the caller should walk.
func (t searchTarget) isDir() bool { return t.tree != nil }

// searchTarget resolves the `path` argument of grep or find_files — over the virtual mounts first,
// then the workspace root, then the extra disk roots — and returns the walk's subject, or the
// model-facing refusal to report (empty on success). given is the argument AS THE MODEL SPELLED
// IT, which every refusal quotes.
//
// It is one function rather than a copy in each tool because the two ask the identical question
// and must give the identical answer: a path one of them can search is a path the other can walk,
// and a refusal one of them renders is the refusal the other renders.
func (s readScope) searchTarget(input, given string) (searchTarget, string) {
	if v, ok := s.virtualLocate(input); ok {
		return virtualSearchTarget(v, given)
	}

	root, resolved, err := s.resolve(input)
	if err != nil {
		return searchTarget{}, err.Error()
	}
	info, err := os.Stat(resolved)
	if err != nil {
		// The path was already accepted by the fence, so an absent one is a mis-spelling the
		// model can fix: offer its near misses in the parent it named.
		siblings := suggestSiblings(root, workspaceRelative(resolved, root), given)
		return searchTarget{}, notFoundMessage("path not found: ", given, siblings)
	}

	// The name is measured from the MATCHED root, symlink-resolved by the shared guard:
	// resolveInRoot hands back real paths, so on a host where a root is reached through a link
	// (macOS's /tmp, a symlinked /home) the raw root is a prefix of nothing.
	targetRel := filepath.ToSlash(workspaceRelative(resolved, root))
	// prefix lifts a walk-relative name to a root-relative one ("" when the search root IS the
	// matched root). fs.WalkDir yields slash-separated names, so the join is path.Join.
	prefix := targetRel
	if prefix == "." {
		prefix = ""
	}
	lift := func(walkRel string) string { return path.Join(prefix, walkRel) }

	target := searchTarget{
		rel:        targetRel,
		openName:   lift,
		reportName: lift,
		// Every file is opened THROUGH the matched root's fence, never by an absolute path the
		// walk handed out, so a walked entry that is a symlink out of that root is refused.
		open: func(name string) (fs.File, error) { return security.SafeOpen(root, name) },
	}
	if info.IsDir() {
		// os.DirFS ENUMERATES only: it is not a boundary, and is not asked to be one — every
		// file the walk names is opened through the fence above.
		target.tree = os.DirFS(resolved)
	}
	return target, ""
}

// virtualSearchTarget is searchTarget's virtual-mount branch (path_virtual.go). There is no fence
// to pin because the mount IS the boundary, and no near-miss suggestions to offer because there is
// no host directory to read them from — the absent case keeps the same "path not found" wording
// the disk branch gives, quoting the address the model spelled.
func virtualSearchTarget(v virtualTarget, given string) (searchTarget, string) {
	info, err := v.stat()
	if err != nil {
		return searchTarget{}, escapeOrMessage(err, "path not found: "+given)
	}

	target := searchTarget{
		rel: v.name(),
		// A walk name opens as ITSELF inside the mount and is reported under the mount's
		// announced address, which is the one spelling the model can hand back to any read tool.
		openName:   func(walkRel string) string { return walkRel },
		reportName: v.child,
	}
	if !info.IsDir() {
		target.open = func(string) (fs.File, error) { return v.open() }
		return target, ""
	}

	tree, err := v.sub()
	if err != nil {
		return searchTarget{}, escapeOrMessage(err, "path not found: "+given)
	}
	target.tree = tree
	target.open = func(name string) (fs.File, error) {
		if !fs.ValidPath(name) {
			return nil, ErrPathEscape
		}
		return tree.Open(name)
	}
	return target, ""
}

// matchRoot reports the first root in roots that is usable AND contains input, with the path
// input resolves to there. A root that is absent or is not a directory — the skills library on
// a box where nothing has created it yet, the creation-deferred convention — is skipped: a
// per-root refusal, never an error of its own.
//
// A root that is not its OWN real path — one reached through a symlink — is skipped the same way
// (audit 2026-08-25 F-13): the host's contract (domain.Config.ExtraReadRoots) is that every root
// it mounts is already symlink-resolved, so a root that is not was never vouched for by anybody.
//
// That rule is also what lets the fence's two containment judgements agree. resolveInRoot judges
// containment on REAL paths while the bounded read's rootRelative relativises LEXICALLY, so the
// pair answers one question only when the root AND the path are both real. Which is why this
// returns the resolved path beside the root it matched, and why readScope.locate hands a read
// under an extra root that pair rather than the input's spelling — a symlink SPELLING of a file
// under a real root is a path the mount accepts, and the two would otherwise part company on it.
//
// The trust decision itself is deliberately NOT taken here. This layer holds a bare list of paths
// with no base to judge them against, so it cannot tell an operator's dotfiles symlink from a
// repo's relocation of the fence. internal/skills owns the anchors and makes that call, handing
// over resolved paths (its readRoots); this is only the check that the two layers agree.
func matchRoot(input string, roots []string) (root, resolved string, ok bool) {
	for _, candidate := range roots {
		if candidate == "" || security.EvalRealPath(candidate) != filepath.Clean(candidate) {
			continue
		}
		if !rootUsable(candidate) {
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
