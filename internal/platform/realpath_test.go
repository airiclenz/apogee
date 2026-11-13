package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRealPathMatchesEvalSymlinks pins RealPath to filepath.EvalSymlinks on every path that
// resolves: a plain directory is itself, a symlinked one is its target, and a missing path keeps
// the fs.ErrNotExist class its callers branch on.
func TestRealPathMatchesEvalSymlinks(t *testing.T) {
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

			got, err := RealPath(tc.path)

			if tc.want == "" {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("RealPath(%q) error = %v; want an fs.ErrNotExist-classed error", tc.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("RealPath(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("RealPath(%q) = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestRealPathFallback drives the branch a Windows junction takes from any host: both package
// seams are swapped, so evalSymlinks fails the way it does under a junction and a stand-in plays
// the OS fallback. It does not run in parallel because it rewrites package state.
func TestRealPathFallback(t *testing.T) {
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
			swapRealPathSeams(t, func(string) (string, error) { return "", errUnderJunction }, fallback)

			got, err := RealPath(tc.path)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RealPath(%q) error = %v; want %v", tc.path, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("RealPath(%q) = %q; want %q", tc.path, got, tc.want)
			}
			if called != tc.wantCalled {
				t.Errorf("fallback called = %v; want %v", called, tc.wantCalled)
			}
		})
	}
}

// swapRealPathSeams installs resolve and fallback as RealPath's two resolvers for the rest of
// the test and restores the package's own when it ends.
func swapRealPathSeams(
	t *testing.T,
	resolve func(string) (string, error),
	fallback func(string) (string, bool),
) {
	t.Helper()
	savedResolve, savedFallback := evalSymlinks, realPathFallback
	t.Cleanup(func() { evalSymlinks, realPathFallback = savedResolve, savedFallback })
	evalSymlinks, realPathFallback = resolve, fallback
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
