package security

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// plantProgram writes an executable file at dir/name and returns its path — the shape of the
// attack this fence exists for: a confined call may write inside the box, and overwriting an
// existing 0755 file keeps the exec bit even without a chmod.
func plantProgram(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestRefuseExecFromWritablePathRefusesInsideTheBox pins the rule at its centre: a program the
// model could have written is refused, wherever inside the writable box it sits, and the
// refusal NAMES the resolved path — the message an operator needs to see that their in-repo
// virtualenv is the cause rather than a missing install.
func TestRefuseExecFromWritablePathRefusesInsideTheBox(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	extra := t.TempDir()
	outside := t.TempDir()

	inRoot := plantProgram(t, filepath.Join(root, ".venv", "bin"), "python3")
	inExtra := plantProgram(t, extra, "python3")
	elsewhere := plantProgram(t, outside, "python3")

	box := &domain.ConfinementBox{WorkspaceRoot: root, WritablePaths: []string{extra}}

	t.Run("inside the workspace root", func(t *testing.T) {
		t.Parallel()
		err := RefuseExecFromWritablePath(inRoot, root, nil)
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath", err)
		}
		if !strings.Contains(err.Error(), EvalRealPath(inRoot)) {
			t.Errorf("refusal = %q, want it to name the resolved program path %q", err, inRoot)
		}
	})

	t.Run("inside an extra writable path", func(t *testing.T) {
		t.Parallel()
		// The box's extra writable paths are as model-writable as the workspace: a confined
		// call plants there just as easily.
		if err := RefuseExecFromWritablePath(inExtra, root, box); !errors.Is(err, ErrExecFromWritablePath) {
			t.Errorf("err = %v, want ErrExecFromWritablePath for a program inside box.WritablePaths", err)
		}
	})

	t.Run("outside every fence", func(t *testing.T) {
		t.Parallel()
		if err := RefuseExecFromWritablePath(elsewhere, root, box); err != nil {
			t.Errorf("err = %v, want nil for a program the model cannot write", err)
		}
	})

	t.Run("no fence refuses nothing", func(t *testing.T) {
		t.Parallel()
		// A caller that can name no workspace has no policy to apply; inventing one would
		// refuse every program on the host, including the empty-root shape a test uses.
		if err := RefuseExecFromWritablePath(elsewhere, "", nil); err != nil {
			t.Errorf("err = %v, want nil when no fence is named", err)
		}
		if err := RefuseExecFromWritablePath("", root, nil); err != nil {
			t.Errorf("err = %v, want nil for an empty argv[0]", err)
		}
	})
}

// TestRefuseExecFromWritablePathResolvesSymlinksAndRelativeNames pins the two spellings that
// would otherwise walk past a lexical check: a name outside the box that POINTS inside it, and
// a relative argv[0] — what exec.LookPath returns when PATH carries a relative entry, and what
// the child would resolve against a working directory that is the workspace itself.
func TestRefuseExecFromWritablePathResolvesSymlinksAndRelativeNames(t *testing.T) {
	// No t.Parallel: the relative case changes the process working directory.
	root := t.TempDir()
	target := plantProgram(t, filepath.Join(root, "bin"), "python3")

	link := filepath.Join(t.TempDir(), "python3")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if err := RefuseExecFromWritablePath(link, root, nil); !errors.Is(err, ErrExecFromWritablePath) {
		t.Errorf("err = %v, want ErrExecFromWritablePath for a symlink pointing into the box", err)
	}

	t.Chdir(filepath.Join(root, "bin"))
	if err := RefuseExecFromWritablePath("./python3", root, nil); !errors.Is(err, ErrExecFromWritablePath) {
		t.Errorf("err = %v, want ErrExecFromWritablePath for a relative argv[0] resolving into the box", err)
	}
}

// TestResolveProgram pins the fence's complete form: what it answers with, and — the point of
// the three-way split — which of the three refusals a caller gets, because "absent", "relative"
// and "inside the box" send an operator to three different places.
func TestResolveProgram(t *testing.T) {
	// No t.Parallel: the nil-look case sets PATH for the whole process.
	root := t.TempDir()
	elsewhere := plantProgram(t, t.TempDir(), "tool")
	inRoot := plantProgram(t, filepath.Join(root, "node_modules", ".bin"), "tool")
	absent := errors.New("executable file not found in $PATH")

	t.Run("a program outside the fence resolves to itself", func(t *testing.T) {
		got, err := ResolveProgram(func(string) (string, error) { return elsewhere, nil }, "tool", root, nil)
		if err != nil {
			t.Fatalf("err = %v, want nil for a program the model cannot write", err)
		}
		if got != elsewhere {
			t.Errorf("ResolveProgram() = %q, want the resolved absolute path %q", got, elsewhere)
		}
	})

	t.Run("a program inside the fence is refused by name", func(t *testing.T) {
		got, err := ResolveProgram(func(string) (string, error) { return inRoot, nil }, "tool", root, nil)
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath", err)
		}
		if !strings.Contains(err.Error(), EvalRealPath(inRoot)) {
			t.Errorf("refusal = %q, want it to name the resolved program path %q", err, inRoot)
		}
		if got != inRoot {
			t.Errorf("ResolveProgram() = %q, want the refused path %q handed back beside the error", got, inRoot)
		}
	})

	t.Run("a relative answer is refused", func(t *testing.T) {
		// A relative answer with no error at all. Go's own exec.LookPath does not spell it this
		// way — it pairs the relative path with exec.ErrDot, the subtest below — but an injected
		// look may, and the child would resolve it against a working directory that is the
		// workspace itself.
		_, err := ResolveProgram(func(string) (string, error) { return "node_modules/.bin/tool", nil }, "tool", root, nil)
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath for a relative program path", err)
		}
		if !strings.Contains(err.Error(), "resolves to a relative program path") {
			t.Errorf("refusal = %q, want the relative-program-path sentence", err)
		}
	})

	t.Run("a relative PATH entry is refused, not reported absent", func(t *testing.T) {
		// The shape the production default actually produces: exec.LookPath answers a relative
		// PATH entry with the relative path AND exec.ErrDot. Classifying on the error alone would
		// send the one case this branch exists for down the absent path.
		relative := "node_modules/.bin/tool"
		got, err := ResolveProgram(func(string) (string, error) { return relative, exec.ErrDot }, "tool", root, nil)
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath for a relative PATH entry", err)
		}
		if !strings.Contains(err.Error(), "resolves to a relative program path") {
			t.Errorf("refusal = %q, want the relative-program-path sentence", err)
		}
		if errors.Is(err, exec.ErrNotFound) {
			t.Errorf("err = %v, want a relative PATH entry NOT to read as an absent program", err)
		}
		if got != relative {
			t.Errorf("ResolveProgram() = %q, want the refused path %q handed back beside the error", got, relative)
		}
	})

	t.Run("a lookup failure is absent, not refused", func(t *testing.T) {
		_, err := ResolveProgram(func(string) (string, error) { return "", absent }, "tool", root, nil)
		if !errors.Is(err, absent) {
			t.Fatalf("err = %v, want the lookup error wrapped", err)
		}
		if errors.Is(err, ErrExecFromWritablePath) {
			t.Errorf("err = %v, want an absent program NOT to read as a fence refusal", err)
		}
	})

	t.Run("a nil look resolves on PATH", func(t *testing.T) {
		self, err := os.Executable()
		if err != nil {
			t.Skipf("os.Executable unavailable on this host: %v", err)
		}
		t.Setenv("PATH", filepath.Dir(self))
		got, err := ResolveProgram(nil, filepath.Base(self), root, nil)
		if err != nil {
			t.Fatalf("err = %v, want the test binary resolved off PATH", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("ResolveProgram() = %q, want an absolute path", got)
		}
	})
}

// TestAdmitPinnedProgram pins the admission's one rule: a refused program is let through only when,
// symlinks evaluated, it IS one of the pinned absolute paths — never a relative spelling of either.
func TestAdmitPinnedProgram(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pinned := plantProgram(t, filepath.Join(root, "scripts"), "fmt.sh")
	beside := plantProgram(t, filepath.Join(root, "scripts"), "lint.sh")
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	cases := []struct {
		name       string
		program    string
		pinned     []string
		isAdmitted bool
	}{
		{"an absolute pinned program is admitted", pinned, []string{pinned}, true},
		{"a relative program is refused", "scripts/fmt.sh", []string{pinned}, false},
		{"a relative pinned entry admits nothing", pinned, []string{"scripts/fmt.sh"}, false},
		{"a root spelled through a symlink is the same file", filepath.Join(link, "scripts", "fmt.sh"), []string{pinned}, true},
		{"an unpinned program beside a pinned one is refused", beside, []string{pinned}, false},
		{"no pins admit nothing", pinned, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := AdmitPinnedProgram(tc.program, tc.pinned)

			if got != tc.isAdmitted {
				t.Errorf("AdmitPinnedProgram(%q, %q) = %v, want %v", tc.program, tc.pinned, got, tc.isAdmitted)
			}
		})
	}
}

// TestResolveProjectProgram pins the project form of the fence: a relative command with a path
// separator resolves against the Project root, a bare name stays a PATH lookup, and a program
// inside the fence passes only when it is pinned.
func TestResolveProjectProgram(t *testing.T) {
	// No t.Parallel: the bare-name case sets PATH for the whole process.
	projectRoot := t.TempDir()
	pinned := plantProgram(t, filepath.Join(projectRoot, "scripts"), "fmt.sh")
	plantProgram(t, filepath.Join(projectRoot, "scripts"), "lint.sh")
	plantProgram(t, projectRoot, "tool")
	onPath := plantProgram(t, t.TempDir(), "tool")

	t.Run("a relative multi-segment command resolves against the root and a pinned one is admitted", func(t *testing.T) {
		got, err := ResolveProjectProgram("./scripts/fmt.sh", projectRoot, projectRoot, nil, []string{pinned})
		if err != nil {
			t.Fatalf("err = %v, want the pinned in-root program admitted", err)
		}
		if got != pinned {
			t.Errorf("ResolveProjectProgram() = %q, want %q", got, pinned)
		}
	})

	t.Run("an unpinned in-root program stays refused", func(t *testing.T) {
		_, err := ResolveProjectProgram("scripts/lint.sh", projectRoot, projectRoot, nil, []string{pinned})
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath for a program nobody pinned", err)
		}
	})

	t.Run("a box path is fenced as ResolveProgram fences it", func(t *testing.T) {
		box := &domain.ConfinementBox{WritablePaths: []string{projectRoot}}
		_, err := ResolveProjectProgram("scripts/lint.sh", projectRoot, "", box, nil)
		if !errors.Is(err, ErrExecFromWritablePath) {
			t.Fatalf("err = %v, want ErrExecFromWritablePath for a program inside the box", err)
		}
	})

	t.Run("a bare name is looked up on PATH, not joined to the root", func(t *testing.T) {
		t.Setenv("PATH", filepath.Dir(onPath))
		got, err := ResolveProjectProgram("tool", projectRoot, projectRoot, nil, nil)
		if err != nil {
			t.Fatalf("err = %v, want the PATH program outside the fence", err)
		}
		if got != onPath {
			t.Errorf("ResolveProjectProgram() = %q, want the PATH answer %q", got, onPath)
		}
	})
}
