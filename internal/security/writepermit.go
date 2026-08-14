package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ----------------------------------------------------------------------------
// The permitted escape target (ADR 0049 — the Gate's allow is executable)
// ----------------------------------------------------------------------------
//
// The workspace fence pins every mutation to an os.Root at the workspace root. That is the
// right answer for every call the operator never saw: a path outside the workspace is refused
// and nothing else happens. It is the WRONG answer for the one call the operator was shown in
// full and approved — the out-of-workspace write the ladder GATES
// (docs/design/confinement-execution-contract.md §4), where the human read the resolved path
// on the approval pane and said yes. Until ADR 0049 that yes could not execute: the fence
// refused it exactly as it refuses an unseen one, and a gate whose "Allow" cannot succeed is
// not a gate.
//
// A PERMITTED TARGET is how that yes reaches the filesystem without widening the fence for
// anything else. The permit is one resolved absolute path — the very writeTarget.Real the
// approval pane disclosed, carried on the execution context as domain.WriteEscapePermit and
// handed to the primitives in safeio.go as a string (empty = no permit). The rule it buys is
// exactly one path wide:
//
//   - No permit, or a target inside the workspace root: today's fence, byte-for-byte. Every
//     existing call keeps the behaviour it had — the never-worse floor.
//   - A permit, and the argument RE-RESOLVES to exactly the permitted path: the mutation runs
//     through an os.Root pinned at the deepest EXISTING directory above that path, with any
//     missing parents created inside that root and the final component acted on by NAME rather
//     than followed.
//   - Anything else: an error wrapping ErrPathEscape, and nothing is touched.
//
// The re-resolution is the point. Execute does not TRUST dispatch's classification, it
// REPRODUCES it (the same EvalRealPath over the same root-joined argument), so an argument
// that has come to mean a different path since the disclosure is refused rather than silently
// redirected — and every path that reaches these primitives from OUTSIDE dispatch (a bench, a
// future Driver — the door ADR 0031 keeps open) keeps the same defence.
//
// A permit is write-side only. The read primitives take none and none may be added: an
// approval authorises the write the operator read, not a wider view of the host.
//
// Like the rest of this package this is a GUARD, not a boundary (doc.go): the chain above the
// pinned root is judged as it exists at check time, and an adversary who can already write
// outside the workspace can swap it afterwards. The boundary is the Confiner. What this layer
// guarantees is that an approved escape lands on the path the human read, or lands nowhere.

// openMutationRoot pins the os.Root a mutation of input lands through and returns input's path
// relative to that root. It is the ONE place the fence decides which root bounds a write, so
// every primitive in safeio.go inherits the same rule by construction rather than by repetition:
// the workspace root for an in-workspace target (permit or no permit), the permitted target's
// own ancestor for an approved escape, and a refusal for everything else.
//
// Which branch a call takes is decided by its RESOLVED path, and that question is asked FIRST: an
// argument that re-resolves to exactly the permitted target takes the permitted branch even when
// its spelling sits inside the workspace. That case is the workspace-internal symlink whose target
// lies outside — the path dispatch resolved in order to classify the write and the approval pane
// disclosed in full, so the yes the operator gave names the outside target and the mutation has to
// reach it. Sending it down the lexical branch instead would refuse the one call the human read,
// which is the gate failing to execute its own Allow.
//
// The lexical in-workspace branch remains unconditional for every OTHER call: with no permit, or
// with one naming a different path, a mutation behaves exactly as it did before permits existed —
// the never-worse floor. Permits are minted only for disclosed escape targets (classifyWriteTarget,
// internal/agent/dispatch.go), so an ordinary in-workspace write can never meet the match above.
//
// The caller owns the returned root and must Close it. On any error nothing is left open.
func openMutationRoot(root, input, permitted string) (*os.Root, string, error) {
	if permitted != "" && namesPermittedTarget(root, input, permitted) {
		return openPermittedRoot(root, input, permitted)
	}

	rel, err := rootRelative(input, root)
	if err == nil {
		r, openErr := os.OpenRoot(root)
		if openErr != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrPathEscape, openErr)
		}
		return r, rel, nil
	}
	if permitted == "" {
		return nil, "", err
	}
	return openPermittedRoot(root, input, permitted)
}

// openPermittedRoot pins the root for a mutation that leaves the workspace WITH an approval
// behind it: input must re-resolve to exactly permitted, and the root is then pinned at the
// deepest existing directory above that target, as close to it as the filesystem allows.
//
// Pinning the ancestor rather than the whole host is what keeps the permit one path wide: the
// only names reachable through that root are the target's own missing parents and the target
// itself, so a mutation that somehow named something else could not resolve to it from here.
//
// The refusals are deliberately specific — the resolved path AND the rule that refused it — for
// the same reason the escape and symlinked-parent messages are: an operator who approved a write
// and got an error needs to know which of the two truths applies, that the argument no longer
// means what the pane showed, or that the target itself has become a link.
func openPermittedRoot(root, input, permitted string) (*os.Root, string, error) {
	named := permittedName(root, input)
	target := filepath.Clean(permitted)

	if resolved := EvalRealPath(named); resolved != target {
		return nil, "", fmt.Errorf(
			"%w: %q resolves to %q, not the approved target %q (an approved write lands on the path the approval disclosed, and nowhere else)",
			ErrPathEscape, input, resolved, target)
	}

	anchor, err := deepestExistingDir(filepath.Dir(target))
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(anchor, target)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %q", ErrPathEscape, input)
	}
	r, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	if err := refusePermittedSymlinkTarget(r, rel, target); err != nil {
		_ = r.Close()
		return nil, "", err
	}
	return r, rel, nil
}

// namesPermittedTarget reports whether input means exactly the permitted target — the single
// question that decides whether a mutation runs through the permitted root instead of the fence.
//
// It is asked twice on purpose. openMutationRoot asks it to ROUTE, ahead of the lexical branch;
// openPermittedRoot asks it again to REFUSE, with the specific message an operator whose approval
// stopped executing needs. Both ask it of the same string through the same resolution, so a path
// can never route one way and be judged another.
func namesPermittedTarget(root, input, permitted string) bool {
	return EvalRealPath(permittedName(root, input)) == filepath.Clean(permitted)
}

// permittedName renders input as the absolute path the permit is judged against: a relative
// argument joins the workspace root exactly as every other mutation resolves it, an absolute one
// stands as given. Cleaning is left to EvalRealPath, which does it on the way to the real path.
func permittedName(root, input string) string {
	if filepath.IsAbs(input) {
		return input
	}
	return filepath.Join(root, input)
}

// deepestExistingDir returns the deepest EXISTING directory at or above path — the anchor an
// approved escape's os.Root is pinned to. A permitted target whose parents do not exist yet is
// ordinary (the approval covered creating the file); the anchor is simply higher, and the
// primitive's own MkdirAll creates the rest as real directories inside the pinned root.
//
// A component that exists but is NOT a real directory ends the climb with a refusal rather than
// with a higher anchor: a symlink there would land the write somewhere the approval never named,
// which is refuseSymlinkedParents' rule stated at the one place an escape can meet it. Because
// the permitted target is itself a symlink-resolved path, that refusal means the chain changed
// under us — the honest thing to report and refuse.
func deepestExistingDir(path string) (string, error) {
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.IsDir() {
				return current, nil
			}
			return "", fmt.Errorf(
				"%w: %q is not a real directory (an approved write reaches its target through real directories)",
				ErrPathEscape, current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%w: %q has no existing parent directory", ErrPathEscape, path)
		}
		current = parent
	}
}

// refusePermittedSymlinkTarget refuses an approved escape whose target NAME is itself a symlink.
// Inside the workspace that name is replaced rather than followed and the question does not
// arise; here it does, because the permitted path is what the operator read: a link at that name
// means the bytes would land on its target instead, under a name the pane never showed.
//
// The case reaches this check only when resolution could not see through the link — a DANGLING
// link, or one planted between the disclosure and the write — since a link that resolves is
// followed by EvalRealPath on both sides and either matches the approval or is already refused
// as a mismatch. An absent name is the ordinary case: the mutation creates it.
func refusePermittedSymlinkTarget(r *os.Root, rel, target string) error {
	info, err := r.Lstat(rel)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return mapRootEscape(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"%w: %q is a symlink (an approved write replaces the disclosed name, never writes through a link)",
			ErrPathEscape, target)
	}
	return nil
}
