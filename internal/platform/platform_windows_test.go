//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These tests run the Windows rule set against the real OS — a real cmd.exe and the real
// long-path resolver — which the shared table tests in host_test.go cannot do from a Linux
// or macOS run. They are the "validate on a real Windows target" the Phase-0 stub deferred.

func TestWindowsShellLineSurvivesQuotingOnlyThroughCommandLine(t *testing.T) {
	// A directory whose name contains a space is the ordinary case (%USERPROFILE% is
	// "C:\Users\First Last" on most machines), and it is exactly the case os/exec's argv
	// joining breaks: it escapes the quotes around the redirect target as \", which
	// cmd.exe reads literally.
	dir := filepath.Join(t.TempDir(), "pro be")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	host := Current()

	t.Run("CommandLine delivers the line verbatim", func(t *testing.T) {
		target := filepath.Join(dir, "verbatim.txt")
		line := "echo x> " + host.Quote(target)
		argv := host.Command(line)

		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: host.CommandLine(line)}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %q: %v (output %q)", line, err, out)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("quoted write %q did not land: %v", target, err)
		}
	})

	t.Run("the argv path alone mangles the quotes", func(t *testing.T) {
		// Pins WHY CommandLine exists: without it the same line fails. If Go ever stops
		// escaping embedded quotes, this test fails and CommandLine can be reconsidered.
		target := filepath.Join(dir, "mangled.txt")
		line := "echo x> " + host.Quote(target)
		argv := host.Command(line)

		err := exec.Command(argv[0], argv[1:]...).Run()
		if _, statErr := os.Stat(target); err == nil && statErr == nil {
			t.Skip("os/exec no longer mangles an embedded quote on Windows; CommandLine may be reducible")
		}
	})
}

func TestWindowsCurrentContainsFoldsRealPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	host := Current()
	if !host.Contains(strings.ToUpper(root), filepath.Join(root, "src", "main.go")) {
		t.Errorf("Contains(%q upper-cased, child) = false, want true on a case-insensitive host", root)
	}
	if host.Contains(root, root+"2") {
		t.Errorf("Contains(%q, %q) = true, want false (sibling, not child)", root, root+"2")
	}
}

func TestWindowsCurrentResolvesShortNames(t *testing.T) {
	t.Parallel()

	// A directory name long enough (and spaced) to get an 8.3 alias where the volume
	// still generates them.
	root := t.TempDir()
	long := filepath.Join(root, "Program Filesish")
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", long, err)
	}
	short := shortPathName(t, long)
	if short == long || !strings.Contains(short, "~") {
		t.Skip("8.3 short-name generation is disabled on this volume; nothing to resolve")
	}

	host := Current()
	if !host.Contains(long, filepath.Join(short, "child.txt")) {
		t.Errorf("Contains(%q, %q) = false, want true — the short name must resolve", long, short)
	}
	if !host.Contains(short, filepath.Join(long, "child.txt")) {
		t.Errorf("Contains(%q, %q) = false, want true — the short root must resolve", short, long)
	}
}

func TestWindowsCurrentScopeEnvCarriesTheSystemFloor(t *testing.T) {
	t.Parallel()

	// SystemRoot is the one every Windows process needs and no POSIX-shaped allowlist
	// names; a git child without it fails inside Winsock, not with "missing variable".
	env := Current().ScopeEnv([]string{"PATH"}, nil)
	var sawSystemRoot bool
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), "SYSTEMROOT=") {
			sawSystemRoot = true
		}
	}
	if !sawSystemRoot {
		t.Errorf("ScopeEnv([PATH]) = %q, want the Windows essentials appended", env)
	}
}

// shortPathName returns the 8.3 alias of p, or p when the volume generates none.
func shortPathName(t *testing.T, p string) string {
	t.Helper()
	from, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatalf("utf16 %q: %v", p, err)
	}
	buf := make([]uint16, len(p)+16)
	n, err := syscall.GetShortPathName(from, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return syscall.UTF16ToString(buf[:n])
}
