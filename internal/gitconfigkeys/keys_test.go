package gitconfigkeys_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/gitconfigkeys"
)

// The literals below are hand-spelled copies of the strings internal/security held before the
// catalogue moved here (commit 1f9f5369). They are not built from the package's constants on
// purpose: a change to which keys are refused or judged a write must fail here first.
const (
	pinnedSource = `core\.(sshcommand|editor|pager|askpass|gitproxy|alternaterefscommand)|sequence\.editor|diff\.external|diff\..*\.(command|textconv)|merge\..*\.driver|mergetool\..*\.cmd|difftool\..*\.cmd|filter\..*\.(clean|smudge|process)|credential\.helper|credential\..*\.helper|gpg\.program|gpg\..*\.program|gpg\.ssh\.defaultkeycommand|uploadpack\.packobjectshook|remote\..*\.(proxy|vcs)|pager\..*`

	pinnedWriteViewSuffix = `core\.hookspath|core\.fsmonitor|interactive\.difffilter|include\.path|includeif\..*\.path|remote\..*\.(uploadpack|receivepack)|url\.ext::.*\.(insteadof|pushinsteadof)|protocol\.allow|protocol\.ext\.allow|submodule\..*\.update`
)

func TestSourceIsUnchanged(t *testing.T) {
	if gitconfigkeys.Source != pinnedSource {
		t.Errorf("Source changed:\n got %s\nwant %s", gitconfigkeys.Source, pinnedSource)
	}
	if gitconfigkeys.WriteViewSuffix != pinnedWriteViewSuffix {
		t.Errorf("WriteViewSuffix changed:\n got %s\nwant %s", gitconfigkeys.WriteViewSuffix, pinnedWriteViewSuffix)
	}
	if got, want := gitconfigkeys.Anchored(gitconfigkeys.Source), `^(`+pinnedSource+`)$`; got != want {
		t.Errorf("anchored probe pattern changed:\n got %s\nwant %s", got, want)
	}
	wantWriteView := `^(` + pinnedSource + `|` + pinnedWriteViewSuffix + `)$`
	if got := gitconfigkeys.Anchored(gitconfigkeys.WriteViewSource); got != wantWriteView {
		t.Errorf("anchored write-view pattern changed:\n got %s\nwant %s", got, wantWriteView)
	}
}

func TestMergeDriverSourceMatchesOnlyDriverKeys(t *testing.T) {
	pattern := regexp.MustCompile(gitconfigkeys.Anchored(gitconfigkeys.MergeDriverSource))
	cases := []struct {
		name string
		want bool
	}{
		{"merge.x.driver", true},
		{"merge.a.b.driver", true},
		{"merge.x.name", false},
		{"merge.x.recursive", false},
		{"merge.driver", false},
		{"xmerge.x.driver", false},
		{"merge.x.driver.extra", false},
	}
	for _, c := range cases {
		if got := pattern.MatchString(c.name); got != c.want {
			t.Errorf("merge-driver pattern on %q = %v, want %v", c.name, got, c.want)
		}
	}
	if !regexp.MustCompile(gitconfigkeys.Anchored(gitconfigkeys.Source)).MatchString("merge.x.driver") {
		t.Error("Source no longer names a merge driver key")
	}
}

func TestValueIsBenign(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		removal bool
		want    bool
	}{
		{"core.fsmonitor", "true", false, true},
		{"core.fsmonitor", "FALSE", false, true},
		{"core.fsmonitor", "0", false, true},
		{"core.fsmonitor", "off", true, true},
		{"core.fsmonitor", "./hook.sh", false, false},
		{"protocol.allow", "never", false, true},
		{"protocol.ext.allow", "Never", false, true},
		{"protocol.ext.allow", "always", false, false},
		{"protocol.allow", "never", true, false},
		{"submodule.lib.update", "rebase", false, true},
		{"submodule.lib.update", "none", false, true},
		{"submodule.lib.update", "!./run.sh", false, false},
		{"submodule.lib.update", "  !./run.sh", false, false},
		{"submodule.lib.update", "rebase", true, false},
		{"core.hookspath", "false", false, false},
		{"core.sshcommand", "never", false, false},
	}
	for _, c := range cases {
		if got := gitconfigkeys.ValueIsBenign(c.name, c.value, c.removal); got != c.want {
			t.Errorf("ValueIsBenign(%q, %q, removal=%v) = %v, want %v", c.name, c.value, c.removal, got, c.want)
		}
	}
}

// TestImportsStandardLibraryOnly keeps the package a leaf: internal/security and internal/gitexec
// both import it, so an import of either — or of anything that reaches them — is a cycle.
func TestImportsStandardLibraryOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found in the package directory")
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: import path %s: %v", file, spec.Path.Value, err)
			}
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %q: the package imports the standard library only", file, path)
			}
		}
	}
}
