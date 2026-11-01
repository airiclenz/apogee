package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ciWorkflowPath is ci.yml, read from the package directory the test runs in.
const ciWorkflowPath = "../../.github/workflows/ci.yml"

// unsupportedGlobCharacters are the GitHub filter glob forms compileGlob does not implement.
const unsupportedGlobCharacters = "?+["

// TestCIPushPathsMatchCIYAML pins the gate's copy of ci.yml's push filter to the workflow
// itself: the same patterns in the same order, none using a glob form the gate's matcher does
// not implement. A change to either side fails here until the other follows.
func TestCIPushPathsMatchCIYAML(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(ciWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", ciWorkflowPath, err)
	}
	var workflow struct {
		On struct {
			Push struct {
				Paths []string `yaml:"paths"`
			} `yaml:"push"`
		} `yaml:"on"`
	}

	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatalf("parse %s: %v", ciWorkflowPath, err)
	}

	paths := workflow.On.Push.Paths
	if len(paths) == 0 {
		t.Fatalf("%s has no on.push.paths filter", ciWorkflowPath)
	}
	if !slices.Equal(paths, ciPushPaths) {
		t.Errorf("ci.yml on.push.paths = %q\nthe gate's ciPushPaths = %q\nchange both together", paths, ciPushPaths)
	}
	for _, pattern := range paths {
		if strings.ContainsAny(pattern, unsupportedGlobCharacters) {
			t.Errorf("ci.yml pattern %q uses one of %q, which the gate's matcher does not implement",
				pattern, unsupportedGlobCharacters)
		}
	}
}

// TestIsSkippedByCI pins how the gate reads ci.yml's push filter: the last matching pattern
// decides, `*` stays within one path segment, and `**` crosses segments.
func TestIsSkippedByCI(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"graphics/apogee-logo-dark.svg":           true,
		"graphics/demo/stage/x.go":                false,
		".capcom/knowledge/decisions/adr-0098.md": false,
		".capcom/knowledge/decisions/adr-0097.md": true,
		".capcom/issues/x.md":                     true,
		"docs/plans/archived/old.md":              true,
		"docs/manual/building.md":                 false,
		"CHANGELOG.md":                            true,
		"VERSION":                                 false,
		"graphics/sub/logo.svg":                   false,
		"cmd/release/gate.go":                     false,
		"docs/design/archived/x/y.md":             true,
		"docs/design/test-drivers.md":             false,
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			got := isSkippedByCI(path)

			if got != want {
				t.Errorf("isSkippedByCI(%q) = %t, want %t", path, got, want)
			}
		})
	}
}

// TestCompileGlob pins the glob forms the matcher implements, beyond those ci.yml uses today.
func TestCompileGlob(t *testing.T) {
	t.Parallel()
	cases := []struct {
		glob    string
		path    string
		isMatch bool
	}{
		{glob: "**", path: "a/b/c.go", isMatch: true},
		{glob: "*.md", path: "README.md", isMatch: true},
		{glob: "*.md", path: "docs/README.md", isMatch: false},
		{glob: "**/README.md", path: "README.md", isMatch: true},
		{glob: "**/README.md", path: "a/b/README.md", isMatch: true},
		{glob: "docs/**", path: "docs/a/b.md", isMatch: true},
		{glob: "docs/**", path: "docsx/a.md", isMatch: false},
		{glob: "a.b", path: "axb", isMatch: false},
	}
	for _, tc := range cases {
		t.Run(tc.glob+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			got := compileGlob(tc.glob).MatchString(tc.path)

			if got != tc.isMatch {
				t.Errorf("glob %q against %q = %t, want %t", tc.glob, tc.path, got, tc.isMatch)
			}
		})
	}
}
