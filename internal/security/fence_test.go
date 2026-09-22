package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestFenceGoverns pins the ADR 0049 rule at the one place it is now asked. Each row builds a
// workspace with a file inside it, an outside directory with the permitted target in it, and a
// workspace-internal symlink to that target (the 2026-08-14 amendment case), then asks whether
// the argument under test means the permitted path: no permit governs nothing; an in-workspace
// path is never governed however it is spelled; the permitted target is governed spelled absolute,
// spelled relative, or reached through the workspace link; and a neighbour the permit does not
// name is not.
func TestFenceGoverns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		permitted  bool                              // whether the fence carries the permit for outside/target.md
		input      func(root, outside string) string // the argument under test
		wantGovern bool
	}{
		{
			name:  "no permit governs nothing",
			input: func(_, outside string) string { return filepath.Join(outside, "target.md") },
		},
		{
			name:      "an in-workspace path under a permit is not governed",
			permitted: true,
			input:     func(root, _ string) string { return filepath.Join(root, "inside.md") },
		},
		{
			name:       "the permitted target spelled absolute is governed",
			permitted:  true,
			input:      func(_, outside string) string { return filepath.Join(outside, "target.md") },
			wantGovern: true,
		},
		{
			name:       "the permitted target through a workspace-internal link is governed",
			permitted:  true,
			input:      func(root, _ string) string { return filepath.Join(root, "link.md") },
			wantGovern: true,
		},
		{
			name:      "a diverged argument is not governed",
			permitted: true,
			input:     func(_, outside string) string { return filepath.Join(outside, "elsewhere.md") },
		},
		{
			name:       "a relative spelling that resolves to the permit is governed",
			permitted:  true,
			input:      func(_, _ string) string { return "link.md" },
			wantGovern: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "target.md")
			for _, path := range []string{target, filepath.Join(outside, "elsewhere.md"), filepath.Join(root, "inside.md")} {
				if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}
			if err := os.Symlink(target, filepath.Join(root, "link.md")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
			fence := WorkspaceFence(root)
			if tc.permitted {
				fence.Permit = domain.WriteEscapePermit{Real: EvalRealPath(target)}
			}

			got := fence.Governs(tc.input(root, outside))

			if got != tc.wantGovern {
				t.Fatalf("Governs(%q) = %v, want %v", tc.input(root, outside), got, tc.wantGovern)
			}
		})
	}
}
