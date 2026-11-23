//go:build windows

package security

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/realpath"
	"github.com/airiclenz/apogee/internal/realpath/junctiontest"
)

// TestJunctionEscapeIsRefused is the defect the one resolver closes: a junction inside the root
// that points outside it reads as an escape, for a file that exists beneath it and for one about
// to be created there.
func TestJunctionEscapeIsRefused(t *testing.T) {
	t.Parallel()

	root := finalTempDir(t)
	outside := finalTempDir(t)
	writeTestFile(t, filepath.Join(outside, "file"))
	junctiontest.MakeOrSkip(t, filepath.Join(root, "j"), outside)

	for _, input := range []string{`j\file`, `j\missing\leaf`, filepath.Join(root, "j", "file")} {
		got, err := ResolveInRoot(input, root)

		if !errors.Is(err, ErrPathEscape) {
			t.Errorf("ResolveInRoot(%q) = %q, %v; want ErrPathEscape", input, got, err)
		}
	}
}

// TestJunctionInsideRootResolves pins the other side: a junction that stays inside the root is
// followed, and the path comes back in the target's spelling.
func TestJunctionInsideRootResolves(t *testing.T) {
	t.Parallel()

	root := finalTempDir(t)
	realDir := filepath.Join(root, "real")
	mkdirTest(t, realDir)
	writeTestFile(t, filepath.Join(realDir, "file"))
	junctiontest.MakeOrSkip(t, filepath.Join(root, "j"), realDir)

	got, err := ResolveInRoot(`j\file`, root)

	if err != nil {
		t.Fatalf(`ResolveInRoot("j\file"): %v`, err)
	}
	if want := filepath.Join(realDir, "file"); !strings.EqualFold(got, want) {
		t.Errorf(`ResolveInRoot("j\file") = %q; want the target spelling %q`, got, want)
	}
}

// TestJunctionedRootContainsItsFiles pins a root that is itself reached through a junction: its
// own files are inside it, and WorkspaceRelative names them relative to it.
func TestJunctionedRootContainsItsFiles(t *testing.T) {
	t.Parallel()

	base := finalTempDir(t)
	target := filepath.Join(base, "target")
	mkdirTest(t, target)
	writeTestFile(t, filepath.Join(target, "file"))
	root := filepath.Join(base, "root")
	junctiontest.MakeOrSkip(t, root, target)

	got, err := ResolveInRoot("file", root)

	if err != nil {
		t.Fatalf(`ResolveInRoot("file") under a junctioned root: %v`, err)
	}
	if want := filepath.Join(target, "file"); !strings.EqualFold(got, want) {
		t.Errorf(`ResolveInRoot("file") = %q; want %q`, got, want)
	}
	if rel := WorkspaceRelative(got, root); rel != "file" {
		t.Errorf("WorkspaceRelative(%q) = %q; want %q", got, rel, "file")
	}
}

// TestSubstSpelledRootContainsAJunctionedFile is the regression the one-spelling rule guards: a
// root spelled through a subst drive and a file beneath an in-root junction must resolve to one
// spelling, or the junctioned file (final-name spelled) would read as outside the drive-letter
// root. A subst drive needs no administrator rights.
func TestSubstSpelledRootContainsAJunctionedFile(t *testing.T) {
	// Not parallel: a drive letter is machine-wide state.
	dir := finalTempDir(t)
	realDir := filepath.Join(dir, "real")
	mkdirTest(t, realDir)
	writeTestFile(t, filepath.Join(realDir, "file"))
	junctiontest.MakeOrSkip(t, filepath.Join(dir, "j"), realDir)
	root := substDriveOrSkip(t, dir)

	got, err := ResolveInRoot(`j\file`, root)

	if err != nil {
		t.Fatalf(`ResolveInRoot("j\file", %q): %v`, root, err)
	}
	if want := filepath.Join(realDir, "file"); !strings.EqualFold(got, want) {
		t.Errorf(`ResolveInRoot("j\file", %q) = %q; want %q`, root, got, want)
	}
	if rel := WorkspaceRelative(got, root); !strings.EqualFold(rel, `real\file`) {
		t.Errorf("WorkspaceRelative(%q, %q) = %q; want %q", got, root, rel, `real\file`)
	}
}

// finalTempDir is t.TempDir in its final on-disk spelling. CI's t.TempDir is the 8.3 RUNNER~1
// form, and the resolver never answers with that, so an expected path is built from this one.
func finalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if final, ok := realpath.FinalPathName(dir); ok {
		return final
	}
	return dir
}

func mkdirTest(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// substDriveOrSkip maps a free drive letter to dir with subst and returns the drive's root
// (`X:\`), removing the mapping at cleanup. When no letter can be mapped the test skips — except
// under GITHUB_ACTIONS, where it fails, so CI cannot go green without having run the proof.
func substDriveOrSkip(t *testing.T, dir string) string {
	t.Helper()
	var failures []string
	for letter := 'Z'; letter >= 'D'; letter-- {
		drive := string(letter) + ":"
		if _, err := os.Stat(drive + `\`); err == nil {
			continue // the letter is taken
		}
		out, err := exec.Command("subst", drive, dir).CombinedOutput()
		if err != nil {
			failures = append(failures, drive+" "+strings.TrimSpace(string(out)))
			continue
		}
		t.Cleanup(func() {
			if out, err := exec.Command("subst", drive, "/D").CombinedOutput(); err != nil {
				t.Errorf("subst %s /D: %v (%s)", drive, err, out)
			}
		})
		return drive + `\`
	}
	message := "subst mapped no free drive letter: " + strings.Join(failures, "; ")
	if os.Getenv("GITHUB_ACTIONS") != "" {
		t.Fatal(message)
	}
	t.Skip(message)
	return ""
}
