package domain

// The workspace-relative name rule, pinned where both gates that apply it can see it. The point of
// the table is MACHINE-INDEPENDENCE: every Windows spelling below must decide the same way on
// Linux, macOS and Windows, so a config file (or an embedder's list) that travels is refused where
// it was written rather than where it lands.

import "testing"

func TestWorkspaceNameRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title       string
		name        string
		wantClean   string
		wantRelativ bool
		wantEscapes bool
	}{
		{
			title: "a plain name is the workspace-relative case the feature is for",
			name:  "AGENTS.md", wantClean: "AGENTS.md", wantRelativ: true,
		},
		{
			title: "a nested name stays inside the workspace",
			name:  "docs/NESTED.md", wantClean: "docs/NESTED.md", wantRelativ: true,
		},
		{
			title: "a backslash-spelled nested name is the SAME name, on every OS",
			name:  `docs\NESTED.md`, wantClean: "docs/NESTED.md", wantRelativ: true,
		},
		{
			title: "a walk-up that cancels out on the way never leaves the workspace",
			name:  "docs/../AGENTS.md", wantClean: "AGENTS.md", wantRelativ: true,
		},
		{
			title: "a dot-prefixed spelling normalises to the plain name (the duplicate key)",
			name:  `.\AGENTS.md`, wantClean: "AGENTS.md", wantRelativ: true,
		},
		{
			title: "a name merely STARTING with dots is not a walk-up",
			name:  "..hidden.md", wantClean: "..hidden.md", wantRelativ: true,
		},
		{
			title: "a unix absolute path is not workspace-relative",
			name:  "/etc/passwd", wantClean: "/etc/passwd",
		},
		{
			title: "a name rooted at the current drive is not workspace-relative anywhere",
			name:  `\AGENTS.md`, wantClean: "/AGENTS.md",
		},
		{
			title: "a Windows DRIVE-RELATIVE name is refused though filepath.IsAbs says false",
			name:  "C:AGENTS.md", wantClean: "C:AGENTS.md",
		},
		{
			title: "a lowercase drive letter is a drive too",
			name:  "c:AGENTS.md", wantClean: "c:AGENTS.md",
		},
		{
			title: "a Windows drive-absolute name is refused on every OS",
			name:  `C:\secrets.md`, wantClean: "C:/secrets.md",
		},
		{
			title: "the parent directory itself climbs out",
			name:  "..", wantClean: "..", wantRelativ: true, wantEscapes: true,
		},
		{
			title: "a walk-up climbs out",
			name:  "../secrets.md", wantClean: "../secrets.md", wantRelativ: true, wantEscapes: true,
		},
		{
			title: "a BACKSLASH-spelled walk-up climbs out on every OS",
			name:  `..\secrets.md`, wantClean: "../secrets.md", wantRelativ: true, wantEscapes: true,
		},
		{
			title: "a walk-up hidden mid-path climbs out once cleaned",
			name:  "docs/../../secrets.md", wantClean: "../secrets.md", wantRelativ: true, wantEscapes: true,
		},
		{
			title: "a backslash-spelled walk-up hidden mid-path climbs out too",
			name:  `docs\..\..\secrets.md`, wantClean: "../secrets.md", wantRelativ: true, wantEscapes: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()

			if got := CleanWorkspaceName(tc.name); got != tc.wantClean {
				t.Errorf("CleanWorkspaceName(%q) = %q, want %q", tc.name, got, tc.wantClean)
			}
			if got := IsWorkspaceRelative(tc.name); got != tc.wantRelativ {
				t.Errorf("IsWorkspaceRelative(%q) = %v, want %v", tc.name, got, tc.wantRelativ)
			}
			if got := EscapesWorkspace(tc.name); got != tc.wantEscapes {
				t.Errorf("EscapesWorkspace(%q) = %v, want %v", tc.name, got, tc.wantEscapes)
			}
		})
	}
}
