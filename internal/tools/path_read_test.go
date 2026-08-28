package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadAllBounded pins the bound step of the one-handle read in isolation: at most max
// bytes are accepted, the max+1'th byte flips the verdict, and never more than max+1 bytes
// are drained. This table is the deterministic cover for readWorkspaceFileBounded's
// growth-backstop branch (a file grown past the cap between the fstat and the read), which
// cannot be driven through the shared body without interleaving a writer.
func TestReadAllBounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		input      string
		max        int64
		wantWithin bool
	}{
		{"under the cap", "abc", 4, true},
		{"exactly the cap", "abcd", 4, true},
		{"one past the cap", "abcde", 4, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, within, err := readAllBounded(strings.NewReader(tc.input), tc.max)

			if err != nil {
				t.Fatalf("readAllBounded: %v", err)
			}
			if within != tc.wantWithin {
				t.Errorf("within = %v, want %v", within, tc.wantWithin)
			}
			if within && string(data) != tc.input {
				t.Errorf("data = %q, want %q", data, tc.input)
			}
			if int64(len(data)) > tc.max+1 {
				t.Errorf("drained %d bytes, want at most max+1 = %d", len(data), tc.max+1)
			}
		})
	}
}

// TestReadWorkspaceFileBounded_RefusesOversizeFile drives the shared body's fstat refusal:
// a file statically past maxFileReadBytes is refused with the exact model-facing message
// the twins rendered before, and its bytes are never materialised. The file is sparse (a
// Truncate with nothing written), so the test costs no 10 MiB write.
func TestReadWorkspaceFileBounded_RefusesOversizeFile(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	f, err := os.Create(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(maxFileReadBytes + 1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, failMessage := readWorkspaceFileBounded("big.bin", root)

	if data != nil {
		t.Errorf("data = %d bytes, want nil on a refusal", len(data))
	}
	want := fmt.Sprintf("file too large: %d bytes (max %d)", int64(maxFileReadBytes)+1, int64(maxFileReadBytes))
	if failMessage != want {
		t.Errorf("failMessage = %q, want %q", failMessage, want)
	}
}

// scopeFixture builds a workspace root and one extra read-only root, each holding a file,
// and returns them beside a readScope wired over both. It is the shared setup of the
// readScope tests below.
func scopeFixture(t *testing.T) (workspace, extra string, scope readScope) {
	t.Helper()

	workspace = tempRoot(t)
	extra = tempRoot(t)
	writeFixtureFile(t, filepath.Join(workspace, "in-workspace.txt"), "workspace bytes")
	writeFixtureFile(t, filepath.Join(extra, "skill.md"), "skill bytes")

	return workspace, extra, readScope{root: workspace, extra: func() []string { return []string{extra} }}
}

// TestReadScopeResolve pins which root a path is accepted under: the workspace first
// (relative or absolute), an extra root only for an ABSOLUTE path, and nothing else at all —
// a path under no root is refused with the workspace's own uniform escape message, whatever
// extra roots are configured.
func TestReadScopeResolve(t *testing.T) {
	t.Parallel()

	workspace, extra, scope := scopeFixture(t)
	outside := tempRoot(t)
	writeFixtureFile(t, filepath.Join(outside, "secret.txt"), "not yours")

	cases := []struct {
		name     string
		input    string
		wantRoot string // "" ⇒ the call must be refused
	}{
		{"workspace relative", "in-workspace.txt", workspace},
		{"workspace absolute", filepath.Join(workspace, "in-workspace.txt"), workspace},
		{"absolute under the extra root", filepath.Join(extra, "skill.md"), extra},
		{"the extra root itself", extra, extra},
		{"a relative name an extra root holds stays in the workspace", "skill.md", workspace},
		{"under no root", filepath.Join(outside, "secret.txt"), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, resolved, err := scope.resolve(tc.input)

			if tc.wantRoot == "" {
				if !errors.Is(err, ErrPathEscape) {
					t.Fatalf("resolve(%q) error = %v, want ErrPathEscape", tc.input, err)
				}
				// The refusal must read exactly as it does without any extra roots.
				_, wantErr := resolveInRoot(tc.input, workspace)
				if err.Error() != wantErr.Error() {
					t.Errorf("error = %q, want the workspace's own %q", err, wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve(%q): %v", tc.input, err)
			}
			if root != tc.wantRoot {
				t.Errorf("root = %q, want %q", root, tc.wantRoot)
			}
			target := tc.input
			if !filepath.IsAbs(target) {
				target = filepath.Join(tc.wantRoot, target)
			}
			if want := realPath(t, target); resolved != want {
				t.Errorf("resolved = %q, want %q", resolved, want)
			}
		})
	}

	// The relative case above resolves to a name the workspace does not hold; reading it must
	// report that absence rather than serving the extra root's file of the same name.
	if data, failMessage := scope.readBounded("skill.md"); failMessage != "file not found: skill.md" {
		t.Errorf("readBounded(%q) = (%q, %q), want the workspace's file-not-found", "skill.md", data, failMessage)
	}
}

// TestReadScopeWorkspaceOnlyUnchanged pins the seam's floor: a scope with no extra roots —
// nil func or a func answering an empty slice — resolves and refuses exactly as the
// single-root helper it wraps, so wiring the seam in changes nothing until a root is
// configured.
func TestReadScopeWorkspaceOnlyUnchanged(t *testing.T) {
	t.Parallel()

	workspace := tempRoot(t)
	extra := tempRoot(t)
	writeFixtureFile(t, filepath.Join(workspace, "in-workspace.txt"), "workspace bytes")
	writeFixtureFile(t, filepath.Join(extra, "skill.md"), "skill bytes")

	scopes := map[string]readScope{
		"nil func":    {root: workspace},
		"empty slice": {root: workspace, extra: func() []string { return nil }},
	}

	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root, resolved, err := scope.resolve("in-workspace.txt")
			if err != nil {
				t.Fatalf("resolve in the workspace: %v", err)
			}
			if root != workspace {
				t.Errorf("root = %q, want the workspace %q", root, workspace)
			}
			if want := realPath(t, filepath.Join(workspace, "in-workspace.txt")); resolved != want {
				t.Errorf("resolved = %q, want %q", resolved, want)
			}

			outside := filepath.Join(extra, "skill.md")
			_, _, err = scope.resolve(outside)
			_, wantErr := resolveInRoot(outside, workspace)
			if err == nil || err.Error() != wantErr.Error() {
				t.Errorf("resolve(%q) error = %v, want %v", outside, err, wantErr)
			}
		})
	}
}

// TestReadScopeExtraRootsAreLive pins the live contract: the func is consulted per call, so a
// root added (or withdrawn) mid-session is honoured by the very next read — no re-wiring.
func TestReadScopeExtraRootsAreLive(t *testing.T) {
	t.Parallel()

	workspace := tempRoot(t)
	extra := tempRoot(t)
	writeFixtureFile(t, filepath.Join(extra, "skill.md"), "skill bytes")

	var mounted bool
	scope := readScope{root: workspace, extra: func() []string {
		if !mounted {
			return nil
		}
		return []string{extra}
	}}
	target := filepath.Join(extra, "skill.md")

	if _, _, err := scope.resolve(target); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("resolve before the root is mounted: err = %v, want ErrPathEscape", err)
	}

	mounted = true

	root, _, err := scope.resolve(target)
	if err != nil {
		t.Fatalf("resolve after the root is mounted: %v", err)
	}
	if root != extra {
		t.Errorf("root = %q, want %q", root, extra)
	}

	mounted = false

	if _, _, err := scope.resolve(target); !errors.Is(err, ErrPathEscape) {
		t.Errorf("resolve after the root is withdrawn: err = %v, want ErrPathEscape", err)
	}
}

// TestReadScopeSkipsUnusableExtraRoot pins the creation-deferred convention: a configured
// root that does not exist (or is not a directory) is a per-root refusal that falls through
// silently — never an error of its own — and a usable root listed after it still serves.
func TestReadScopeSkipsUnusableExtraRoot(t *testing.T) {
	t.Parallel()

	workspace, extra, _ := scopeFixture(t)
	missing := filepath.Join(tempRoot(t), "never-created")
	notADir := filepath.Join(tempRoot(t), "a-file")
	writeFixtureFile(t, notADir, "not a directory")

	scope := readScope{root: workspace, extra: func() []string { return []string{missing, notADir, extra} }}

	root, _, err := scope.resolve(filepath.Join(extra, "skill.md"))
	if err != nil {
		t.Fatalf("resolve past the unusable roots: %v", err)
	}
	if root != extra {
		t.Errorf("root = %q, want the usable root %q", root, extra)
	}

	// A path under the absent root alone is refused, with the workspace's message.
	underMissing := filepath.Join(missing, "skill.md")
	onlyMissing := readScope{root: workspace, extra: func() []string { return []string{missing} }}
	_, _, err = onlyMissing.resolve(underMissing)
	_, wantErr := resolveInRoot(underMissing, workspace)
	if err == nil || err.Error() != wantErr.Error() {
		t.Errorf("resolve(%q) error = %v, want %v", underMissing, err, wantErr)
	}
}

// TestReadScopeRefusesSymlinkEscapingExtraRoot pins that per-root fencing survives the
// fallback: a symlink INSIDE an extra root pointing outside it is refused, exactly as one
// inside the workspace is. The extra roots widen where a read may start, never how far it
// may reach.
func TestReadScopeRefusesSymlinkEscapingExtraRoot(t *testing.T) {
	t.Parallel()

	_, extra, scope := scopeFixture(t)
	outside := tempRoot(t)
	writeFixtureFile(t, filepath.Join(outside, "secret.txt"), "not yours")

	escape := filepath.Join(extra, "escape.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), escape); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, _, err := scope.resolve(escape); !errors.Is(err, ErrPathEscape) {
		t.Errorf("resolve(%q) error = %v, want ErrPathEscape", escape, err)
	}
	if _, _, err := scope.open(escape); !errors.Is(err, ErrPathEscape) {
		t.Errorf("open(%q) error = %v, want ErrPathEscape", escape, err)
	}
	if data, failMessage := scope.readBounded(escape); failMessage == "" {
		t.Errorf("readBounded(%q) returned %d bytes, want a refusal", escape, len(data))
	}
}

// TestReadScopeRefusesAnExtraRootReachedThroughASymlink is the MOUNT side of the same fence
// (audit 2026-08-25 F-13): a root that is not its own real path was never resolved by the host,
// so it never matches at all — resolve, open and readBounded all answer the workspace's own
// refusal, and the file behind the symlink stays unread. Mounting the SAME directory by its real
// path still reads, because the rule is "resolved", not "no mounts".
func TestReadScopeRefusesAnExtraRootReachedThroughASymlink(t *testing.T) {
	t.Parallel()

	workspace, target := tempRoot(t), tempRoot(t)
	writeFixtureFile(t, filepath.Join(target, "secret.txt"), "not yours")

	link := filepath.Join(tempRoot(t), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	linked := readScope{root: workspace, extra: func() []string { return []string{link} }}
	throughLink := filepath.Join(link, "secret.txt")

	if _, _, err := linked.resolve(throughLink); !errors.Is(err, ErrPathEscape) {
		t.Errorf("resolve(%q) error = %v, want ErrPathEscape", throughLink, err)
	}
	if _, _, err := linked.open(throughLink); !errors.Is(err, ErrPathEscape) {
		t.Errorf("open(%q) error = %v, want ErrPathEscape", throughLink, err)
	}
	if data, failMessage := linked.readBounded(throughLink); failMessage == "" {
		t.Errorf("readBounded(%q) returned %d bytes, want a refusal", throughLink, len(data))
	}

	resolvedMount := readScope{root: workspace, extra: func() []string { return []string{target} }}
	direct := filepath.Join(target, "secret.txt")
	data, failMessage := resolvedMount.readBounded(direct)
	if failMessage != "" {
		t.Fatalf("readBounded(%q) under the resolved mount: %s", direct, failMessage)
	}
	if string(data) != "not yours" {
		t.Errorf("content = %q, want %q", data, "not yours")
	}
}

// TestReadScopeReadsAnExtraRootFileBySymlinkSpelling is the INPUT side of the mount rule above:
// the root stays its own real path, and only the path the caller SPELLS runs through a symlink —
// the shape a dotfiles-managed ~/.apogee/skills hands the model, which read_file refused while
// list_dir read it (audit 2026-08-28 F-13). The bytes come back, and locate names the real root
// beside the real file, which is what makes the fence's lexical half agree with its real half.
//
// The directory half pins what that resolution does to a REFUSAL's wording: the same spelling
// aimed at a directory is refused in the RESOLVED path's words, never the symlink's, because the
// read was pinned to the real path (readFileErrorMessage's doc states the rule).
func TestReadScopeReadsAnExtraRootFileBySymlinkSpelling(t *testing.T) {
	t.Parallel()

	workspace, extra := tempRoot(t), tempRoot(t)
	target := filepath.Join(extra, "skill", "SKILL.md")
	writeFixtureFile(t, target, "skill bytes")

	link := filepath.Join(tempRoot(t), "lib")
	if err := os.Symlink(extra, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	scope := readScope{root: workspace, extra: func() []string { return []string{extra} }}
	spelled := filepath.Join(link, "skill", "SKILL.md")

	data, failMessage := scope.readBounded(spelled)

	if failMessage != "" {
		t.Fatalf("readBounded(%q): %s", spelled, failMessage)
	}
	if string(data) != "skill bytes" {
		t.Errorf("content = %q, want %q", data, "skill bytes")
	}

	root, located, err := scope.locate(spelled)

	if err != nil {
		t.Fatalf("locate(%q): %v", spelled, err)
	}
	if root != extra {
		t.Errorf("root = %q, want the mounted extra root %q", root, extra)
	}
	if located != target {
		t.Errorf("target = %q, want the resolved real path %q", located, target)
	}

	spelledDir := filepath.Join(link, "skill")

	data, failMessage = scope.readBounded(spelledDir)

	if len(data) != 0 {
		t.Errorf("readBounded(%q) returned %d bytes, want none", spelledDir, len(data))
	}
	if want := "not a file: " + filepath.Join(extra, "skill"); failMessage != want {
		t.Errorf("refusal = %q, want %q", failMessage, want)
	}
	if strings.Contains(failMessage, link) {
		t.Errorf("refusal %q quotes the symlink spelling %q, want the resolved path", failMessage, link)
	}
}

// TestReadScopeLocate pins the other two branches of the seam: a path the WORKSPACE accepts is
// handed on exactly as it was given — relative stays relative, absolute stays absolute, so the
// bounded read under the workspace is byte-for-byte what it was before locate existed — and a
// path no root accepts carries the workspace's own ErrPathEscape, naming the input, with
// readBounded rendering the refusal the workspace alone would have rendered.
func TestReadScopeLocate(t *testing.T) {
	t.Parallel()

	workspace, _, scope := scopeFixture(t)
	outside := tempRoot(t)
	writeFixtureFile(t, filepath.Join(outside, "secret.txt"), "not yours")

	for _, input := range []string{"in-workspace.txt", filepath.Join(workspace, "in-workspace.txt")} {
		root, target, err := scope.locate(input)

		if err != nil {
			t.Fatalf("locate(%q): %v", input, err)
		}
		if root != workspace {
			t.Errorf("locate(%q) root = %q, want the workspace %q", input, root, workspace)
		}
		if target != input {
			t.Errorf("locate(%q) target = %q, want the input unchanged", input, target)
		}
	}

	for _, input := range []string{"/nowhere/x", filepath.Join(outside, "secret.txt")} {
		root, target, err := scope.locate(input)

		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("locate(%q) error = %v, want ErrPathEscape", input, err)
		}
		if !strings.Contains(err.Error(), input) {
			t.Errorf("locate(%q) error %q does not name the input", input, err)
		}
		if root != "" || target != "" {
			t.Errorf("locate(%q) = (%q, %q), want both empty on a refusal", input, root, target)
		}
		if _, failMessage := scope.readBounded(input); failMessage != err.Error() {
			t.Errorf("readBounded(%q) message = %q, want the workspace refusal %q", input, failMessage, err)
		}
	}
}

// TestReadScopeOpen pins the handle half: the matched root comes back beside the file, and a
// missing file under a root that DOES contain the path keeps its own I/O error rather than
// being reported as an escape.
func TestReadScopeOpen(t *testing.T) {
	t.Parallel()

	_, extra, scope := scopeFixture(t)

	f, root, err := scope.open(filepath.Join(extra, "skill.md"))
	if err != nil {
		t.Fatalf("open under the extra root: %v", err)
	}
	defer f.Close()
	if root != extra {
		t.Errorf("root = %q, want %q", root, extra)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read the opened handle: %v", err)
	}
	if string(data) != "skill bytes" {
		t.Errorf("content = %q, want %q", data, "skill bytes")
	}

	if _, _, err := scope.open("in-workspace.txt"); err != nil {
		t.Errorf("open a workspace-relative path: %v", err)
	}
	if _, _, err := scope.open(filepath.Join(extra, "absent.md")); errors.Is(err, ErrPathEscape) {
		t.Errorf("open a missing file under the extra root: err = %v, want a plain I/O error", err)
	} else if err == nil {
		t.Error("open a missing file under the extra root: err = nil, want an I/O error")
	}
}

// TestReadScopeReadBounded pins the bounded-read variant: it picks the root, then reads with
// readWorkspaceFileBounded's contract — same content, and the same model-facing messages for
// a missing file and for a path under no root at all.
func TestReadScopeReadBounded(t *testing.T) {
	t.Parallel()

	_, extra, scope := scopeFixture(t)
	outside := tempRoot(t)
	writeFixtureFile(t, filepath.Join(outside, "secret.txt"), "not yours")

	cases := []struct {
		name        string
		input       string
		wantContent string
		wantFail    string
	}{
		{"workspace relative", "in-workspace.txt", "workspace bytes", ""},
		{"absolute under the extra root", filepath.Join(extra, "skill.md"), "skill bytes", ""},
		{
			name:     "missing under the extra root",
			input:    filepath.Join(extra, "absent.md"),
			wantFail: "file not found: " + filepath.Join(extra, "absent.md"),
		},
		{
			name:     "under no root",
			input:    filepath.Join(outside, "secret.txt"),
			wantFail: fmt.Sprintf("%v: %q", ErrPathEscape, filepath.Join(outside, "secret.txt")),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, failMessage := scope.readBounded(tc.input)

			if failMessage != tc.wantFail {
				t.Fatalf("failMessage = %q, want %q", failMessage, tc.wantFail)
			}
			if tc.wantFail == "" && string(data) != tc.wantContent {
				t.Errorf("content = %q, want %q", data, tc.wantContent)
			}
		})
	}
}
