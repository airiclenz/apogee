//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// junctionConfigOrSkip makes dir/real/config.yaml holding old and an NTFS junction dir/home
// pointing at dir/real, returning the config path under the junction and the real file. A
// filesystem that refuses junctions skips the test: there is no junction to save through.
func junctionConfigOrSkip(t *testing.T, old string) (underJunction, target string) {
	t.Helper()
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(realDir, "config.yaml")
	if err := os.WriteFile(target, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "home")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, realDir).CombinedOutput(); err != nil {
		t.Skipf("junctions are not available here: %v (%s)", err, out)
	}
	return filepath.Join(junction, "config.yaml"), target
}

// assertStillAJunction fails the test unless the junction directory above path is still a reparse
// point — a save must write through it, never replace it.
func assertStillAJunction(t *testing.T, path string) {
	t.Helper()
	junction := filepath.Dir(path)
	info, err := os.Lstat(junction)
	if err != nil {
		t.Fatal(err)
	}
	data, isWindowsData := info.Sys().(*syscall.Win32FileAttributeData)
	if !isWindowsData {
		t.Fatalf("Lstat(%q).Sys() is %T, want *syscall.Win32FileAttributeData", junction, info.Sys())
	}
	if data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Errorf("%q is no longer a junction after the save", junction)
	}
}

// A config whose path passes under a junction is saved: the junction's target takes the new bytes,
// the junction stays a junction, and no temp file is left beside the config.
func TestWriteConfigAtomicallyWritesThroughAJunction(t *testing.T) {
	t.Parallel()
	path, target := junctionConfigOrSkip(t, "old: true\n")

	if err := writeConfigAtomically(path, []byte("new: true\n")); err != nil {
		t.Fatalf("writeConfigAtomically under a junction: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new: true\n" {
		t.Errorf("the junction's target holds %q, want the new bytes", got)
	}
	assertStillAJunction(t, path)
	if names := onlyFileIn(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("the target's directory holds %v, want only the config", names)
	}
}

// The settings save that first failed under a junction — a side-panel setting — lands on disk when
// the config home is reached through one.
func TestWriteConfigAtomicallySavesASidePanelSettingThroughAJunction(t *testing.T) {
	t.Parallel()
	path, target := junctionConfigOrSkip(t, "ui:\n  side-panel:\n    show: auto\n")

	if err := SaveSidePanelSetting(path, domain.UIKeySidePanelShow, "off"); err != nil {
		t.Fatalf("SaveSidePanelSetting under a junction: %v", err)
	}

	if got := readSectionsConfig(t, target); !strings.Contains(got, "show: \"off\"") {
		t.Errorf("config =\n%s\nwant it to set show: \"off\"", got)
	}
	assertStillAJunction(t, path)
	names := onlyFileIn(t, filepath.Dir(target))
	slices.Sort(names)
	if !slices.Equal(names, configAndItsLock) {
		t.Errorf("the target's directory holds %v, want %v", names, configAndItsLock)
	}
}
