package realpath

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveMatchesEvalSymlinks pins Resolve to filepath.EvalSymlinks on every path that
// resolves: a plain directory is itself, a symlinked one is its target, and a missing path keeps
// the fs.ErrNotExist class its callers branch on.
func TestResolveMatchesEvalSymlinks(t *testing.T) {
	t.Parallel()

	root := canonicalTempDir(t)
	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatalf("mkdir plain dir: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(plain, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating a symlink needs Developer Mode or elevation here: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "plain directory", path: plain, want: plain},
		{name: "symlinked directory", path: link, want: plain},
		{name: "missing path beneath a symlinked directory", path: filepath.Join(link, "child"), want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(tc.path)

			if tc.want == "" {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("Resolve(%q) error = %v; want an fs.ErrNotExist-classed error", tc.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveFallback drives the branch a Windows junction takes from any host: both package
// seams are swapped, so evalSymlinks fails the way it does under a junction and a stand-in plays
// the OS fallback. It does not run in parallel because it rewrites package state.
func TestResolveFallback(t *testing.T) {
	errUnderJunction := errors.New("the system cannot find the path specified")
	existing := t.TempDir()
	missing := filepath.Join(existing, "missing")
	const resolvedByFallback = `C:\target`

	tests := []struct {
		name       string
		path       string
		fallback   func(string) (string, bool)
		want       string
		wantErr    error
		wantCalled bool
	}{
		{
			name:       "existing path resolves through the fallback",
			path:       existing,
			fallback:   func(string) (string, bool) { return resolvedByFallback, true },
			want:       resolvedByFallback,
			wantCalled: true,
		},
		{
			name:       "fallback that cannot resolve keeps the first error",
			path:       existing,
			fallback:   func(string) (string, bool) { return "", false },
			wantErr:    errUnderJunction,
			wantCalled: true,
		},
		{
			name:     "missing path never reaches the fallback",
			path:     missing,
			fallback: func(string) (string, bool) { return resolvedByFallback, true },
			wantErr:  errUnderJunction,
		},
		{
			name:    "no fallback keeps the first error",
			path:    existing,
			wantErr: errUnderJunction,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			var fallback func(string) (string, bool)
			if tc.fallback != nil {
				fallback = func(p string) (string, bool) {
					called = true
					return tc.fallback(p)
				}
			}
			swapRealPathSeams(t, func(string) (string, error) { return "", errUnderJunction }, fallback, nil)

			got, err := Resolve(tc.path)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Resolve(%q) error = %v; want %v", tc.path, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q; want %q", tc.path, got, tc.want)
			}
			if called != tc.wantCalled {
				t.Errorf("fallback called = %v; want %v", called, tc.wantCalled)
			}
		})
	}
}

// TestResolveThroughAJunction drives the branch a path that IS a Windows junction takes
// from any host: evalSymlinks succeeds with the junction's own spelling, the junction check sees
// it, and the fallback's answer wins. A path with no junction, or a fallback that cannot answer,
// keeps the evalSymlinks answer. It does not run in parallel because it rewrites package state.
func TestResolveThroughAJunction(t *testing.T) {
	const (
		junctionSpelling   = `C:\link`
		resolvedByFallback = `C:\target`
	)

	tests := []struct {
		name       string
		crosses    bool
		fallback   func(string) (string, bool)
		want       string
		wantCalled bool
	}{
		{
			name:       "a junction resolves through the fallback",
			crosses:    true,
			fallback:   func(string) (string, bool) { return resolvedByFallback, true },
			want:       resolvedByFallback,
			wantCalled: true,
		},
		{
			name:       "a fallback that cannot answer keeps the evalSymlinks answer",
			crosses:    true,
			fallback:   func(string) (string, bool) { return "", false },
			want:       junctionSpelling,
			wantCalled: true,
		},
		{
			name:     "a path with no junction never reaches the fallback",
			fallback: func(string) (string, bool) { return resolvedByFallback, true },
			want:     junctionSpelling,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			swapRealPathSeams(t,
				func(string) (string, error) { return junctionSpelling, nil },
				func(p string) (string, bool) {
					called = true
					return tc.fallback(p)
				},
				func(string) bool { return tc.crosses },
			)

			got, err := Resolve(junctionSpelling)

			if err != nil {
				t.Fatalf("Resolve(%q): %v", junctionSpelling, err)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q; want %q", junctionSpelling, got, tc.want)
			}
			if called != tc.wantCalled {
				t.Errorf("fallback called = %v; want %v", called, tc.wantCalled)
			}
		})
	}
}

// swapRealPathSeams installs resolve and fallback as Resolve's two resolvers, and crosses as
// its junction check, for the rest of the test, and restores the package's own when it ends.
func swapRealPathSeams(
	t *testing.T,
	resolve func(string) (string, error),
	fallback func(string) (string, bool),
	crosses func(string) bool,
) {
	t.Helper()
	savedResolve, savedFallback, savedCrosses := evalSymlinks, realPathFallback, crossesJunction
	t.Cleanup(func() {
		evalSymlinks, realPathFallback, crossesJunction = savedResolve, savedFallback, savedCrosses
	})
	evalSymlinks, realPathFallback, crossesJunction = resolve, fallback, crosses
}

// canonicalTempDir is t.TempDir with its own links resolved, so an expected path built under it
// is spelled the way the resolver spells it on a host whose temp dir sits behind a link (macOS
// /var is /private/var).
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp dir: %v", err)
	}
	return dir
}

// finalTempDir is canonicalTempDir spelled the way ResolveLenient spells an existing path: through
// the package's final-path call where the host has one (Windows, where CI's temp dir is an 8.3
// alias), and unchanged elsewhere. A test calls it before it swaps the seams.
func finalTempDir(t *testing.T) string {
	t.Helper()
	dir := canonicalTempDir(t)
	if realPathFallback == nil {
		return dir
	}
	if final, ok := realPathFallback(dir); ok {
		return final
	}
	return dir
}

// symlinkedDir makes root/plain and a symlink root/link to it, skipping the test where the host
// refuses to create a symlink (Windows without Developer Mode or elevation).
func symlinkedDir(t *testing.T, root string) (plain, link string) {
	t.Helper()
	plain = filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatalf("mkdir plain dir: %v", err)
	}
	link = filepath.Join(root, "link")
	if err := os.Symlink(plain, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating a symlink needs Developer Mode or elevation here: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}
	return plain, link
}

// TestResolveMissingPathIsNotExist pins the error class Resolve's callers branch on: a path that
// does not exist fails with an fs.ErrNotExist-classed error and no partial answer.
func TestResolveMissingPathIsNotExist(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing", "leaf")

	got, err := Resolve(missing)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Resolve(%q) error = %v; want an fs.ErrNotExist-classed error", missing, err)
	}
	if got != "" {
		t.Errorf("Resolve(%q) = %q; want no answer beside the error", missing, got)
	}
}

// TestResolveLenientMissingLeafUnderASymlink is ResolveLenient's reason to exist: a file about to
// be created beneath a symlinked directory resolves to the link target's spelling with the
// missing rest re-joined, and an existing path resolves like Resolve.
func TestResolveLenientMissingLeafUnderASymlink(t *testing.T) {
	t.Parallel()

	root := finalTempDir(t)
	plain, link := symlinkedDir(t, root)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "the symlinked directory itself", path: link, want: plain},
		{name: "a missing leaf", path: filepath.Join(link, "leaf"), want: filepath.Join(plain, "leaf")},
		{name: "a missing leaf two levels down", path: filepath.Join(link, "missing", "leaf"), want: filepath.Join(plain, "missing", "leaf")},
		{name: "an uncleaned spelling", path: link + string(filepath.Separator) + "." + string(filepath.Separator) + "leaf", want: filepath.Join(plain, "leaf")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveLenient(tc.path)

			if got != tc.want {
				t.Errorf("ResolveLenient(%q) = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveLenientUnderAFakedJunction drives the Windows junction branch from any host: a plain
// directory stands in for the junction, the crossesJunction seam names it and the final-path
// seam answers with the target, as GetFinalPathNameByHandle does. An existing file and a missing
// leaf beneath the junction both come back in the target's spelling, never the junction's own.
// It does not run in parallel because it rewrites package state.
func TestResolveLenientUnderAFakedJunction(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "target")
	junction := filepath.Join(root, "junction")
	for _, dir := range []string{target, junction} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write a file beneath the target: %v", err)
	}
	swapRealPathSeams(t, filepath.EvalSymlinks,
		fakeFinalPath(func(p string) string { return respell(p, junction, target) }),
		func(p string) bool { return hasPathPrefix(p, junction) },
	)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "the junction itself", path: junction, want: target},
		{name: "a file beneath the junction", path: filepath.Join(junction, "inner"), want: filepath.Join(target, "inner")},
		{name: "a missing leaf beneath the junction", path: filepath.Join(junction, "missing", "leaf"), want: filepath.Join(target, "missing", "leaf")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveLenient(tc.path)

			if got != tc.want {
				t.Errorf("ResolveLenient(%q) = %q; want the target spelling %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveLenientSpellsPathAndRootAlike pins the one-spelling rule: where the final-path call
// respells the whole volume (a subst or mapped drive), a path beneath a junction and its
// junction-free root both come back in that spelling, so the path still reads as inside the root.
// Resolve alone would respell only the junctioned path. It does not run in parallel because it
// rewrites package state.
func TestResolveLenientSpellsPathAndRootAlike(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "target")
	junction := filepath.Join(root, "junction")
	for _, dir := range []string{target, junction} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	finalVolume := filepath.Join(root, "final-volume")
	swapRealPathSeams(t, filepath.EvalSymlinks,
		fakeFinalPath(func(p string) string {
			return respell(respell(p, junction, target), root, finalVolume)
		}),
		func(p string) bool { return hasPathPrefix(p, junction) },
	)

	gotRoot := ResolveLenient(root)
	gotPath := ResolveLenient(filepath.Join(junction, "missing", "leaf"))

	if gotRoot != finalVolume {
		t.Errorf("ResolveLenient(root) = %q; want the final-name spelling %q though the root crosses no junction", gotRoot, finalVolume)
	}
	if want := filepath.Join(finalVolume, "target", "missing", "leaf"); gotPath != want {
		t.Errorf("ResolveLenient(path under the junction) = %q; want %q", gotPath, want)
	}
	if !hasPathPrefix(gotPath, gotRoot) {
		t.Errorf("the path resolved to %q and its root to %q; want the path spelled beneath its root", gotPath, gotRoot)
	}
}

// TestResolveLenientFallsBackWhenTheFinalNameCallFails pins the failure branch: on a volume where
// the final-path call answers for no path at all, ResolveLenient returns the Resolve-based climb
// — symlinks resolved — and never the bare absolute path. It does not run in parallel because it
// rewrites package state.
func TestResolveLenientFallsBackWhenTheFinalNameCallFails(t *testing.T) {
	root := canonicalTempDir(t)
	plain, link := symlinkedDir(t, root)
	finalNameCalls := 0
	swapRealPathSeams(t, filepath.EvalSymlinks,
		func(string) (string, bool) {
			finalNameCalls++
			return "", false
		},
		nil,
	)
	path := filepath.Join(link, "missing", "leaf")

	got := ResolveLenient(path)

	if want := filepath.Join(plain, "missing", "leaf"); got != want {
		t.Errorf("ResolveLenient(%q) = %q; want the symlink-resolved climb %q", path, got, want)
	}
	if finalNameCalls == 0 {
		t.Error("the final-path call was never tried; the test proved nothing about its failure")
	}
}

// TestResolveLenientNeverFails pins the last two rungs: a relative path with no existing
// ancestor to resolve through still comes back, made absolute.
func TestResolveLenientNeverFails(t *testing.T) {
	swapRealPathSeams(t, func(string) (string, error) { return "", fs.ErrNotExist }, nil, nil)
	const relative = "missing/leaf"
	want, err := filepath.Abs(relative)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	got := ResolveLenient(relative)

	if got != want {
		t.Errorf("ResolveLenient(%q) = %q; want the absolute spelling %q", relative, got, want)
	}
}

// TestIsLink pins what counts as a link: the link itself, never a plain directory, and never a
// path that merely lies beneath a link. A missing path is an error, not "no link".
func TestIsLink(t *testing.T) {
	t.Parallel()

	root := canonicalTempDir(t)
	plain, link := symlinkedDir(t, root)
	if err := os.WriteFile(filepath.Join(plain, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write a file beneath the plain dir: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "a symlink", path: link, want: true},
		{name: "a plain directory", path: plain, want: false},
		{name: "a file beneath a symlink", path: filepath.Join(link, "inner"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := IsLink(tc.path)

			if err != nil {
				t.Fatalf("IsLink(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("IsLink(%q) = %v; want %v", tc.path, got, tc.want)
			}
		})
	}

	t.Run("a missing path is an error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(root, "missing")

		if _, err := IsLink(missing); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("IsLink(%q) error = %v; want an fs.ErrNotExist-classed error", missing, err)
		}
	})
}

// TestStripLongPathPrefix pins the prefix strip on every host: it is pure string work.
func TestStripLongPathPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		want         string
		wantStripped bool
	}{
		{name: "an extended-length drive path", path: `\\?\C:\work`, want: `C:\work`, wantStripped: true},
		{name: "an extended-length UNC path", path: `\\?\UNC\server\share\x`, want: `\\server\share\x`, wantStripped: true},
		{name: "a lower-case UNC marker", path: `\\?\unc\server\share`, want: `\\server\share`, wantStripped: true},
		{name: "an ordinary path", path: `C:\work`, want: `C:\work`},
		{name: "a device path", path: `\\.\PhysicalDrive0`, want: `\\.\PhysicalDrive0`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, stripped := StripLongPathPrefix(tc.path)

			if got != tc.want || stripped != tc.wantStripped {
				t.Errorf("StripLongPathPrefix(%q) = %q, %v; want %q, %v", tc.path, got, stripped, tc.want, tc.wantStripped)
			}
		})
	}
}

// allowedExternalImport is the one non-standard-library dependency the leaf may carry: the OS
// bindings its Windows-tagged file calls the final-path API through.
const allowedExternalImport = "golang.org/x/sys/windows"

// TestPackageImportsNothingFromApogee holds the leaf rule: realpath imports the standard library
// and golang.org/x/sys/windows, and nothing from apogee, so every package that needs a real path
// can import it without a cycle. The non-test files are read off the directory and parsed with
// go/parser, so the //go:build windows file is checked from any host; test files are left out
// because they may import the package's own test-support subpackage.
func TestPackageImportsNothingFromApogee(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if first, _, _ := strings.Cut(path, "/"); path == allowedExternalImport || !strings.Contains(first, ".") {
				continue
			}
			t.Errorf("%s imports %q; realpath is a leaf over the standard library and %s", name, path, allowedExternalImport)
		}
	}
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the leaf-dependency guard proved nothing")
	}
}

// fakeFinalPath builds a stand-in for the final-path call from a pure respelling: like
// GetFinalPathNameByHandle it answers only for a path that exists.
func fakeFinalPath(respelled func(string) string) func(string) (string, bool) {
	return func(p string) (string, bool) {
		if _, err := os.Stat(p); err != nil {
			return "", false
		}
		return respelled(p), true
	}
}

// respell returns p with its leading from replaced by to when p is from or lies beneath it, and
// p unchanged otherwise.
func respell(p, from, to string) string {
	if !hasPathPrefix(p, from) {
		return p
	}
	return to + strings.TrimPrefix(p, from)
}

// hasPathPrefix reports whether p is prefix or lies beneath it, by whole components.
func hasPathPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+string(filepath.Separator))
}
