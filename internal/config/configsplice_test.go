package config

import (
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
