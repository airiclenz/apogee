//go:build windows

package security

import (
	"path/filepath"
	"testing"
)

// TestAllowRuleJunctionLeavingTheRootDoesNotMatch pins the escape the one resolver closes for
// Allow rules: a `cd` through a junction inside the root that points outside it lands outside the
// workspace, so the line no longer matches a rule and asks.
func TestAllowRuleJunctionLeavingTheRootDoesNotMatch(t *testing.T) {
	t.Parallel()

	root := finalTempDir(t)
	outside := finalTempDir(t)
	makeJunctionOrSkip(t, filepath.Join(root, "j"), outside)
	site := AllowSite{Root: root, IsPOSIXShell: true}

	used, isAllowed := MatchAllowRules("cd j && go test", []string{"go"}, site)

	if isAllowed || len(used) != 0 {
		t.Errorf("MatchAllowRules through a junction leaving the root = %v, %v; want no match", used, isAllowed)
	}
}

// TestAllowRuleJunctionInsideTheRootKeepsMatching pins the other side: a `cd` through a junction
// whose target stays inside the root still lands in the workspace, so the rule keeps matching.
func TestAllowRuleJunctionInsideTheRootKeepsMatching(t *testing.T) {
	t.Parallel()

	root := finalTempDir(t)
	realDir := filepath.Join(root, "real")
	mkdirTest(t, realDir)
	makeJunctionOrSkip(t, filepath.Join(root, "j"), realDir)
	site := AllowSite{Root: root, IsPOSIXShell: true}

	used, isAllowed := MatchAllowRules("cd j && go test", []string{"go"}, site)

	if !isAllowed || len(used) != 1 || used[0] != 0 {
		t.Errorf("MatchAllowRules through an in-root junction = %v, %v; want [0], true", used, isAllowed)
	}
}
