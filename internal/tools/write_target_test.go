package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/undo"
)

// The write side's scope value, one method at a time. Every method is the fenced primitive the
// free function it replaced reached, so the assertions are about the VALUE's contract — one
// resolution per call, the permit pin on the disclosed Real, a refusal that never gains
// suggestions — rather than about the fence, which internal/security pins.

// workspaceTarget resolves input under root with no permit and no journal — the ordinary call.
func workspaceTarget(t *testing.T, root, input string) writeTarget {
	t.Helper()

	target, err := writeScopeOf(context.Background(), root).target(input)
	if err != nil {
		t.Fatalf("target(%q): %v", input, err)
	}
	return target
}

// outsideTarget returns a path in a directory the workspace does not contain, so a call naming it
// is a gated escape rather than an ordinary write.
func outsideTarget(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "outside.txt")
}

func TestWriteScopeTargetResolvesOnceAndRefusesAnEmptyPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	scope := writeScopeOf(context.Background(), root)

	target, err := scope.target(filepath.Join("sub", "f.txt"))
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if target.Named != filepath.Join(root, "sub", "f.txt") {
		t.Errorf("Named = %q, want the root-joined argument", target.Named)
	}
	if target.Real != security.EvalRealPath(target.Named) {
		t.Errorf("Real = %q, want Named resolved", target.Real)
	}
	if target.input != filepath.Join("sub", "f.txt") || target.scope.root != root {
		t.Errorf("value carries input %q under root %q, want the argument under the scope's root", target.input, target.scope.root)
	}

	if _, err := scope.target(""); !errors.Is(err, errPathRequired) {
		t.Errorf("target(\"\") error = %v, want errPathRequired", err)
	}
}

func TestWriteScopeOfReadsThePermitOffTheContext(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := outsideTarget(t)

	if got := writeScopeOf(context.Background(), root).permit; got != "" {
		t.Errorf("permit without a stamp = %q, want empty", got)
	}
	if got := writeScopeOf(escapePermit(outside), root).permit; got != security.EvalRealPath(outside) {
		t.Errorf("permit = %q, want the stamped Real %q", got, security.EvalRealPath(outside))
	}
}

func TestWriteTargetPin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := outsideTarget(t)
	writeFixtureFile(t, filepath.Join(root, "in.txt"), "inside")

	cases := []struct {
		name       string
		ctx        context.Context
		input      string
		wantInput  string
		wantRoot   string
		wantAbsent bool
	}{
		{name: "no permit stays on the workspace", ctx: context.Background(), input: "in.txt", wantInput: "in.txt", wantRoot: root},
		{name: "a permit never moves an in-workspace path", ctx: escapePermit(outside), input: "in.txt", wantInput: "in.txt", wantRoot: root},
		{name: "the permitted target pins to its own parent", ctx: escapePermit(outside), input: outside, wantInput: filepath.Base(outside), wantRoot: filepath.Dir(security.EvalRealPath(outside))},
		{name: "a permit for another path leaves the fence alone", ctx: escapePermit(filepath.Join(filepath.Dir(outside), "other.txt")), input: outside, wantInput: outside, wantRoot: root},
		{name: "a permitted target under a missing parent is absent", ctx: escapePermit(filepath.Join(outside, "deeper", "f.txt")), input: filepath.Join(outside, "deeper", "f.txt"), wantAbsent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target, err := writeScopeOf(tc.ctx, root).target(tc.input)
			if err != nil {
				t.Fatalf("target: %v", err)
			}

			pinInput, pinRoot, absent := target.pin()

			if pinInput != tc.wantInput || pinRoot != tc.wantRoot || absent != tc.wantAbsent {
				t.Errorf("pin() = (%q, %q, %v), want (%q, %q, %v)", pinInput, pinRoot, absent, tc.wantInput, tc.wantRoot, tc.wantAbsent)
			}
		})
	}
}

func TestWriteTargetReadFollowsThePermitAndNothingElse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := outsideTarget(t)
	writeFixtureFile(t, filepath.Join(root, "in.txt"), "inside")
	writeFixtureFile(t, outside, "outside")

	if got, err := workspaceTarget(t, root, "in.txt").read(); err != nil || string(got) != "inside" {
		t.Errorf("read of a workspace file = (%q, %v), want its bytes", got, err)
	}
	if _, err := workspaceTarget(t, root, outside).read(); !errors.Is(err, ErrPathEscape) {
		t.Errorf("read of an outside file without a permit = %v, want ErrPathEscape", err)
	}

	permitted, err := writeScopeOf(escapePermit(outside), root).target(outside)
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if got, err := permitted.read(); err != nil || string(got) != "outside" {
		t.Errorf("read of the permitted target = (%q, %v), want its bytes", got, err)
	}
	if _, err := workspaceTarget(t, root, "missing.txt").read(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read of a missing file = %v, want os.ErrNotExist", err)
	}
}

func TestWriteTargetStatAndPerm(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "exec.sh"), "#!/bin/sh\n", 0o755)
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	info, err := workspaceTarget(t, root, "dir").stat()
	if err != nil || !info.IsDir() {
		t.Errorf("stat of a directory = (%v, %v), want a directory FileInfo", info, err)
	}
	if got := workspaceTarget(t, root, "exec.sh").perm(); got != 0o755 {
		t.Errorf("perm of an executable = %o, want 755", got)
	}
	if got := workspaceTarget(t, root, "missing.txt").perm(); got != 0 {
		t.Errorf("perm of a missing file = %o, want 0", got)
	}
	if _, err := workspaceTarget(t, root, "shipped:debugging").stat(); !errors.Is(err, ErrPathEscape) {
		t.Errorf("stat of a virtual-mount reference = %v, want ErrPathEscape", err)
	}
}

func TestWriteTargetRefuseVirtual(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	if err := workspaceTarget(t, root, "notes.md").refuseVirtual(); err != nil {
		t.Errorf("refuseVirtual on an ordinary path = %v, want nil", err)
	}
	err := workspaceTarget(t, root, "shipped:debugging/x.md").refuseVirtual()
	if !errors.Is(err, ErrPathEscape) || !strings.Contains(err.Error(), "shipped:debugging/x.md") {
		t.Errorf("refuseVirtual on a mount reference = %v, want ErrPathEscape naming the argument", err)
	}
}

func TestWriteTargetNotFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "docs", "notes.md"), "n")
	outside := outsideTarget(t)

	absent := workspaceTarget(t, root, filepath.Join("docs", "note"))
	_, readErr := absent.read()
	if got, want := absent.notFound(readErr, "file not found: "), "file not found: docs/note — did you mean: docs/notes.md"; got != want {
		t.Errorf("notFound on an absence = %q, want %q", got, want)
	}

	refused := workspaceTarget(t, root, outside)
	_, readErr = refused.read()
	got := refused.notFound(readErr, "file not found: ")
	if !strings.Contains(got, "outside the workspace") || strings.Contains(got, "did you mean") {
		t.Errorf("notFound on a refusal = %q, want the fence's own words and no suggestions", got)
	}
}

func TestWriteTargetNote(t *testing.T) {
	t.Parallel()

	// The root by its real path: the note discloses any difference between the named and the
	// resolved target, and on macOS t.TempDir() itself sits behind the /var → /private/var link,
	// which would put a note on the ordinary path for a reason the test is not about.
	root := security.EvalRealPath(t.TempDir())
	if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got := workspaceTarget(t, root, filepath.Join("real", "f.txt")).note(); got != "" {
		t.Errorf("note on an ordinary path = %q, want empty", got)
	}
	if got := workspaceTarget(t, root, "shipped:debugging").note(); got != "" {
		t.Errorf("note on a virtual-mount reference = %q, want empty", got)
	}
	linked := workspaceTarget(t, root, filepath.Join("link", "f.txt"))
	if want := " → resolves to " + linked.Real; linked.note() != want || linked.Real == linked.Named {
		t.Errorf("note through a symlink = %q, want %q", linked.note(), want)
	}

	// A workspace root that is ITSELF reached through a symlink (macOS /tmp → /private/tmp, a
	// `cd` through a link) puts a difference between Named and Real on every path under it. That
	// difference is the root's, not the argument's: the ordinary path stays quiet, a link the
	// argument's own path passes through is still named — by its fully resolved path — and an
	// absolute argument outside the root keeps the plain comparison.
	base := security.EvalRealPath(t.TempDir())
	realRoot := filepath.Join(base, "real-root")
	linkRoot := filepath.Join(base, "link-root")
	if err := os.MkdirAll(filepath.Join(realRoot, "real"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("symlink root: %v", err)
	}
	if err := os.Symlink("real", filepath.Join(realRoot, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	ordinary := workspaceTarget(t, linkRoot, filepath.Join("real", "f.txt"))
	if got := ordinary.note(); got != "" {
		t.Errorf("note on an ordinary path under a symlinked root = %q, want empty", got)
	}
	if ordinary.redirected() {
		t.Errorf("redirected() on an ordinary path under a symlinked root = true, want false")
	}
	if want := filepath.Join(linkRoot, "real", "f.txt"); ordinary.Named != want {
		t.Errorf("Named under a symlinked root = %q, want %q (the spelling the journal and the fences take)", ordinary.Named, want)
	}
	if want := filepath.Join(realRoot, "real", "f.txt"); ordinary.Real != want {
		t.Errorf("Real under a symlinked root = %q, want %q", ordinary.Real, want)
	}

	through := workspaceTarget(t, linkRoot, filepath.Join("link", "f.txt"))
	if want := " → resolves to " + filepath.Join(realRoot, "real", "f.txt"); through.note() != want {
		t.Errorf("note through a per-file link under a symlinked root = %q, want %q", through.note(), want)
	}
	if !through.redirected() {
		t.Errorf("redirected() through a per-file link under a symlinked root = false, want true")
	}

	outside := workspaceTarget(t, linkRoot, filepath.Join(base, "elsewhere.txt"))
	if outside.expected != outside.Named {
		t.Errorf("expected for an absolute argument outside the root = %q, want Named %q", outside.expected, outside.Named)
	}
	if outside.redirected() || outside.note() != "" {
		t.Errorf("an absolute argument outside a symlinked root reads as redirected: note = %q", outside.note())
	}
}

func TestResolvedTargetNoteIsQuietOnASymlinkedRoot(t *testing.T) {
	t.Parallel()

	// read_file's producer: a reader holds no writeTarget and reads the tail off the root-only
	// spelling, so it must fold the root's own link out the same way the writers do.
	base := security.EvalRealPath(t.TempDir())
	realRoot := filepath.Join(base, "real-root")
	linkRoot := filepath.Join(base, "link-root")
	if err := os.MkdirAll(filepath.Join(realRoot, "real"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("real", filepath.Join(realRoot, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if got := resolvedTargetNote(filepath.Join("real", "f.txt"), linkRoot); got != "" {
		t.Errorf("resolvedTargetNote on an ordinary path under a symlinked root = %q, want empty", got)
	}
	want := " → resolves to " + filepath.Join(realRoot, "real", "f.txt")
	if got := resolvedTargetNote(filepath.Join("link", "f.txt"), linkRoot); got != want {
		t.Errorf("resolvedTargetNote through a per-file link under a symlinked root = %q, want %q", got, want)
	}
}

func TestWriteTargetWriteLandsAndJournals(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "f.txt"), "before")
	journal := undo.New()
	target, err := writeScopeOf(undo.WithJournal(context.Background(), journal), root).target("f.txt")
	if err != nil {
		t.Fatalf("target: %v", err)
	}

	if err := target.write([]byte("after"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mustContain(t, filepath.Join(root, "f.txt"), "after")
	if wrote := journal.Wrote(); len(wrote) != 1 || wrote[0] != filepath.Join(root, "f.txt") {
		t.Errorf("journal.Wrote() = %v, want the one path the write landed on", wrote)
	}
	if err := workspaceTarget(t, root, "shipped:debugging").write([]byte("x"), 0o644); !errors.Is(err, ErrPathEscape) {
		t.Errorf("write to a virtual-mount reference = %v, want ErrPathEscape", err)
	}
}

func TestWriteTargetJournaled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doomed := filepath.Join(root, "doomed.txt")
	writeFixtureFile(t, doomed, "bytes")
	journal := undo.New()
	target, err := writeScopeOf(undo.WithJournal(context.Background(), journal), root).target("doomed.txt")
	if err != nil {
		t.Fatalf("target: %v", err)
	}

	gotEscape := "unset"
	err = target.journaled(postAbsent, func(escape string) (bool, error) {
		gotEscape = escape
		return true, os.Remove(doomed)
	})

	if err != nil {
		t.Fatalf("journaled: %v", err)
	}
	if gotEscape != "" {
		t.Errorf("body was handed escape %q, want empty without a permit", gotEscape)
	}
	if wrote := journal.Wrote(); len(wrote) != 1 || wrote[0] != doomed {
		t.Errorf("journal.Wrote() = %v, want the removed path", wrote)
	}

	bodyErr := errors.New("refused")
	err = workspaceTarget(t, root, "other.txt").journaled(postAbsent, func(string) (bool, error) { return false, bodyErr })
	if !errors.Is(err, bodyErr) {
		t.Errorf("journaled returned %v, want the body's error unchanged", err)
	}
}
