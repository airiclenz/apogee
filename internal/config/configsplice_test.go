package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// symlinkConfigOrSkip makes dir/real/config.yaml holding old and links dir/home/config.yaml to it,
// returning the link and its target. A platform or filesystem that refuses symlinks skips the test:
// there is no link to save through.
func symlinkConfigOrSkip(t *testing.T, old string) (link, target string) {
	t.Helper()
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	homeDir := filepath.Join(dir, "home")
	for _, d := range []string{realDir, homeDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target = filepath.Join(realDir, "config.yaml")
	if err := os.WriteFile(target, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(homeDir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	return link, target
}

// A config reached through a symlink is saved through it: the link stays a link, its target takes
// the new bytes with its mode unchanged, and no temp file is left beside either of them.
func TestWriteConfigAtomicallyWritesThroughASymlink(t *testing.T) {
	link, target := symlinkConfigOrSkip(t, "old: true\n")

	if err := writeConfigAtomically(link, []byte("new: true\n")); err != nil {
		t.Fatalf("writeConfigAtomically through a link: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the config link was replaced by a %v file", info.Mode().Type())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new: true\n" {
		t.Errorf("the link's target holds %q, want the new bytes", got)
	}
	if runtime.GOOS != "windows" {
		targetInfo, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if perm := targetInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("the target's mode is %o after the save, want 600", perm)
		}
	}
	if names := onlyFileIn(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("the target's directory holds %v, want only the config", names)
	}
	if names := onlyFileIn(t, filepath.Dir(link)); len(names) != 1 {
		t.Errorf("the link's directory holds %v, want only the link", names)
	}
}

// A link whose target sits in a directory the save cannot write (a home-manager link into
// /nix/store) is refused with an error naming the resolved target, and the link is never swapped
// for a regular file in its place.
func TestWriteConfigAtomicallyRefusesALinkIntoAReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits do not stop file creation on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only directory regardless of its mode")
	}
	link, target := symlinkConfigOrSkip(t, "old: true\n")
	targetDir := filepath.Dir(target)
	if err := os.Chmod(targetDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(targetDir, 0o700) })

	err := writeConfigAtomically(link, []byte("new: true\n"))
	if err == nil {
		t.Fatal("writeConfigAtomically saved through a link into a read-only directory, want a refusal")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("the refusal %q does not name the resolved target %q", err, target)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the config link was replaced by a %v file", info.Mode().Type())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old: true\n" {
		t.Errorf("the link's target holds %q after a refused save, want it untouched", got)
	}
}

// forceResolveFailure makes every evalSymlinks call fail, as filepath.EvalSymlinks does for a path
// under a Windows junction, until the test ends. A test that calls it must not run in parallel:
// the package's parallel tests reach writeConfigAtomically through editHeld.
func forceResolveFailure(t *testing.T) {
	t.Helper()
	previous := evalSymlinks
	evalSymlinks = func(string) (string, error) {
		return "", errors.New("forced resolve failure")
	}
	t.Cleanup(func() { evalSymlinks = previous })
}

// A config the resolver cannot resolve although it exists is still saved through its link: the
// link is followed by hand, its target takes the new bytes, and no temp file is left behind.
func TestWriteConfigAtomicallyFallsBackWhenResolveFails(t *testing.T) {
	link, target := symlinkConfigOrSkip(t, "old: true\n")
	forceResolveFailure(t)

	if err := writeConfigAtomically(link, []byte("new: true\n")); err != nil {
		t.Fatalf("writeConfigAtomically with the resolver failing: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the config link was replaced by a %v file", info.Mode().Type())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new: true\n" {
		t.Errorf("the link's target holds %q, want the new bytes", got)
	}
	if names := onlyFileIn(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("the target's directory holds %v, want only the config", names)
	}
	if names := onlyFileIn(t, filepath.Dir(link)); len(names) != 1 {
		t.Errorf("the link's directory holds %v, want only the link", names)
	}
}

// A path that is missing as well as unresolvable is refused with the resolver's error, as it was
// before the fallback existed: there is nothing to write through.
func TestWriteConfigAtomicallyKeepsTheResolveErrorForAMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "config.yaml")
	forceResolveFailure(t)

	err := writeConfigAtomically(path, []byte("new: true\n"))

	if err == nil {
		t.Fatal("writeConfigAtomically saved to a missing path, want a refusal")
	}
	if !strings.Contains(err.Error(), "resolve config") || !strings.Contains(err.Error(), "forced resolve failure") {
		t.Errorf("the refusal %q is not the resolve error", err)
	}
	if _, statErr := os.Stat(filepath.Dir(path)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the refused save left %q behind (stat: %v)", filepath.Dir(path), statErr)
	}
}
