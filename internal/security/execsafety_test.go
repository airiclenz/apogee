package security

import (
	"errors"
	"os"
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
