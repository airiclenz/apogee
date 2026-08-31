package tools

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// The VIRTUAL half of read resolution, beside path_read.go's disk half: read-only trees the host
// mounts under a NAME rather than under a host path, because they have none. apogee's own shipped
// skills are compiled into the binary (ADR 0065 §3), so `shipped:debugging` is the only address
// their bundled files can be announced under — and an announced address a tool refuses is exactly
// what this package may not produce.
//
// A mount reference is spelled `<mount>:<path below it>`, resolved through an fs.FS rather than an
// os.Root. There is no fence to pin because there is no filesystem to escape: the whole tree is
// the mount, and a `..` that climbs out of it is refused with the same uniform escape message a
// disk root's fence gives (virtualTarget.open).
//
// Mounts are READ-ONLY by construction — nothing is behind them to write to — so every write
// refuses the spelling outright (refuseVirtualWrite) instead of resolving it to a colon-named file
// inside the workspace, which is what a relative path carrying a colon would otherwise become.

// minVirtualMountNameLen is the shortest mount name a reference may carry, and the whole of what
// keeps `C:\Users\...` from reading as a mount: a Windows drive letter is ONE character, so
// requiring two puts every drive-qualified path back on the disk side where it belongs.
const minVirtualMountNameLen = 2

// virtualMountRef splits a path spelled as a virtual-mount reference — `<mount>:<rel>` — into the
// mount prefix (the colon included, so it is the map key verbatim) and the slash-separated path
// below it, answering ok=false for every path that is not one.
//
// A reference is recognised by SYNTAX alone, not by a registered mount, because the two sides ask
// different questions: a READ falls through to the disk roots when no mount answers to the name
// (virtualLocate), while a WRITE refuses the spelling whether or not anything is mounted — a write
// tool holds no mount table, and a path that reads as a mount reference in one tool must not
// silently become a colon-named workspace file in another.
//
// rel is cleaned but NOT validated: a reference that climbs out of its mount keeps ok=true and
// fails at use, so the refusal is the fence's uniform escape message rather than a fall-through to
// the workspace, where the same spelling would name a different file.
func virtualMountRef(p string) (mount, rel string, ok bool) {
	colon := strings.IndexByte(p, ':')
	if colon < minVirtualMountNameLen {
		return "", "", false
	}
	name := p[:colon]
	if strings.ContainsAny(name, `/\`) {
		return "", "", false
	}
	for _, r := range name {
		if !isVirtualMountRune(r) {
			return "", "", false
		}
	}
	rest := strings.TrimLeft(filepath.ToSlash(p[colon+1:]), "/")
	return p[:colon+1], path.Clean(rest), true
}

// isVirtualMountRune reports whether r may appear in a mount name. The set is deliberately narrow
// — lower-case ASCII, digits, dash and underscore — so an ordinary file name carrying a colon
// ("notes:draft.md") is not mistaken for a mount reference and quietly refused by the writers.
func isVirtualMountRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

// virtualTarget is one resolved hit inside a virtual mount: the mount it was accepted under, the
// tree serving it, and the target's own path within that tree ("." at the mount root). It is the
// virtual counterpart of the (root, resolved) pair readScope.locate hands a disk read.
type virtualTarget struct {
	mount string
	fsys  fs.FS
	rel   string
}

// name renders the target back in the spelling the model uses — the announced address, so a
// refusal quotes what the model wrote rather than an internal path.
func (v virtualTarget) name() string {
	if v.rel == "." {
		return v.mount
	}
	return v.mount + v.rel
}

// child renders the announced address of a name relative to this target, for a walk that reports
// what it found under it.
func (v virtualTarget) child(rel string) string {
	if rel == "" || rel == "." {
		return v.name()
	}
	if v.rel == "." {
		return v.mount + rel
	}
	return v.mount + v.rel + "/" + rel
}

// open opens the target for reading. A rel that is not a valid fs path — one that climbed out of
// the mount with `..` — is refused with the uniform escape message, never with absence: the mount
// is the whole fence, and a climb out of it is the same refusal a disk root gives.
func (v virtualTarget) open() (fs.File, error) {
	return v.openRel(v.rel)
}

// openRel opens a path relative to the MOUNT (not to the target), which is the spelling a walk
// carries: fs.WalkDir over a sub-tree yields names relative to that sub-tree, and grep re-opens a
// match's file by the name it reported it under.
func (v virtualTarget) openRel(rel string) (fs.File, error) {
	if !fs.ValidPath(rel) {
		return nil, fmt.Errorf("%w: %s", ErrPathEscape, v.mount+rel)
	}
	return v.fsys.Open(rel)
}

// sub returns the tree rooted AT the target, for the walking tools that enumerate below it. The
// error is the same escape refusal open gives, so an invalid reference is refused once, in one
// wording, whichever tool asked.
func (v virtualTarget) sub() (fs.FS, error) {
	if !fs.ValidPath(v.rel) {
		return nil, fmt.Errorf("%w: %s", ErrPathEscape, v.name())
	}
	if v.rel == "." {
		return v.fsys, nil
	}
	return fs.Sub(v.fsys, v.rel)
}

// readBounded reads the target through the SAME bounded read a disk file goes through
// (readOpenedBounded): one cap, one set of refusal wordings, one growth backstop. On failure the
// second return is the model-facing message, quoting the announced address rather than an internal
// path; on success it is empty.
//
// It carries no sibling suggestions of its own: a mount is a handful of files the skill's own
// instructions name, so a mis-spelling is recovered by re-reading those instructions rather than
// by a "did you mean" over the mount.
func (v virtualTarget) readBounded() ([]byte, string) {
	f, err := v.open()
	if err != nil {
		return nil, readFileErrorMessage(err, v.name())
	}
	defer f.Close()
	return readOpenedBounded(f, v.name())
}

// stat describes the target, reporting the same escape or absence errors open does.
func (v virtualTarget) stat() (fs.FileInfo, error) {
	f, err := v.open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// virtualLocate resolves input against the host's virtual mounts, answering ok=false when there
// are none, when input is not a mount reference, or when no mount answers to its name — each of
// which leaves the disk roots to resolve it exactly as they did before any mount existed.
//
// Mounts are consulted BEFORE the disk roots by every caller, which costs nothing: a mount
// reference is a spelling no host path can take (virtualMountRef), so the two resolvers can never
// both accept one input.
func (s readScope) virtualLocate(input string) (virtualTarget, bool) {
	if s.virtual == nil {
		return virtualTarget{}, false
	}
	mount, rel, ok := virtualMountRef(input)
	if !ok {
		return virtualTarget{}, false
	}
	fsys := s.virtual()[mount]
	if fsys == nil {
		return virtualTarget{}, false
	}
	return virtualTarget{mount: mount, fsys: fsys, rel: rel}, true
}

// refuseVirtualWrite is the write fence's half of the mount contract: a path spelled as a mount
// reference is refused with the uniform escape message, because a virtual mount is read-only by
// construction — there is no filesystem behind it a write could land in.
//
// It refuses by SYNTAX, without consulting the mount table the write tools do not hold
// (virtualMountRef): the alternative is worse than strict, because a `shipped:…` path a write
// resolved as an ordinary relative name would create a colon-named file inside the workspace and
// report success for a write the model believed had landed in the mount.
//
// nil means "not a mount reference, carry on" — the answer for every ordinary path, so the write
// side is byte-identical to what it was for anything but this one spelling.
func refuseVirtualWrite(input string) error {
	if _, _, ok := virtualMountRef(input); !ok {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPathEscape, input)
}
