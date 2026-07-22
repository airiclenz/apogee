//go:build windows

package tools

import (
	"strings"
	"testing"
)

// TestSafeGitEnv_WindowsCarriesTheSystemFloor pins the Windows half of the git tools'
// scrubbed environment. safeEnvKeys is POSIX-shaped (HOME, SHELL, TMPDIR …); a Windows
// child built from that list alone starts without %SystemRoot% and fails inside Winsock or
// CryptoAPI with an error that names no variable. platform.Shell.ScopeEnv appends the
// platform floor, so the allowlist stays a policy statement rather than an OS checklist.
func TestSafeGitEnv_WindowsCarriesTheSystemFloor(t *testing.T) {
	t.Parallel()

	env := safeGitEnv()
	for _, key := range []string{"SYSTEMROOT=", "COMSPEC=", "PATHEXT="} {
		var found bool
		for _, entry := range env {
			if strings.HasPrefix(strings.ToUpper(entry), key) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("safeGitEnv() = %q, missing the Windows essential %q", env, strings.TrimSuffix(key, "="))
		}
	}
}
