package domain

// The workspace-relative NAME rule behind Config.ContextFiles: whether a configured name can only
// ever resolve to a file INSIDE the workspace root. It lives in domain because both gates that ask
// the question speak it — the host's config check, which can name the `context-files.names` key
// that is wrong, and the engine's construction-time gate, which catches an embedder handing the
// same shape to Config directly (ADR 0023 §3's defense-in-depth posture, the shape prompt.Validate
// already has). One rule, two messages: what one gate refuses the other refuses too, and neither
// can drift away from the other.
//
// Every predicate here is MACHINE-INDEPENDENT: it reads a backslash as a separator and a "C:"
// prefix as a drive whatever the host OS is, so a config file — or an embedder's list — that
// travels is refused identically on Linux, macOS and Windows rather than passing here and reaching
// outside the workspace there. The cost is a false positive for a unix filename that genuinely
// contains a backslash, which is not a filename anyone writes here.

import (
	"path"
	"path/filepath"
	"strings"
)

// CleanWorkspaceName is a configured name's machine-independent normal form: backslashes read as
// separators on every OS, then cleaned with slash semantics (path, never path/filepath — whose
// separator is the HOST's). It is what EscapesWorkspace reasons over, and what a caller comparing
// two configured names should compare: "AGENTS.md", "./AGENTS.md" and `.\AGENTS.md` are one name.
func CleanWorkspaceName(name string) string {
	return path.Clean(strings.ReplaceAll(name, `\`, "/"))
}

// IsWorkspaceRelative reports whether a configured name resolves inside the workspace root on
// EVERY OS — the question filepath.IsAbs alone cannot answer for a name that travels:
//
//   - a ROOTED name ("/x", `\x`) is absolute on unix and root-of-the-current-drive on Windows;
//   - a DRIVE-scoped name ("C:AGENTS.md") is drive-relative — filepath.IsAbs reports false for it
//     even on Windows, yet it resolves against that drive's own working directory, so it can name
//     a file anywhere on the machine.
//
// The ".." walk-up is EscapesWorkspace's question, and an empty name is the caller's, so each gate
// can name those cases separately.
func IsWorkspaceRelative(name string) bool {
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	drive := len(name) >= 2 && name[1] == ':' &&
		(('a' <= name[0] && name[0] <= 'z') || ('A' <= name[0] && name[0] <= 'Z'))
	return !drive
}

// EscapesWorkspace reports whether a workspace-relative name climbs above the workspace root once
// cleaned — "..", "../x", `..\x` and "a/../../x" all do, while "a/../x" does not.
func EscapesWorkspace(name string) bool {
	cleaned := CleanWorkspaceName(name)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}
