package projectroot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/projectroot"
)

// mkdirs creates each slash-separated folder below base.
func mkdirs(t *testing.T, base string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeFile creates the slash-separated file below base, its parents included.
func writeFile(t *testing.T, base, rel, content string) {
	t.Helper()
	path := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// realTempDir is t.TempDir symlink-resolved, so a folder above the workspace — which Resolve
// returns in its resolved spelling — compares equal on a host whose temp dir is itself a symlink.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// ADR 0096 §1: the nearest `.apogee/` from the workspace up to the git top-level, the workspace
// otherwise, and never the user's home.
func TestResolve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		dirs      []string          // folders to create below the base
		files     map[string]string // files to create below the base
		workspace string            // below the base
		home      string            // below the base; "" passes no home
		want      string            // below the base; "-" wants the empty Project root
	}{
		{
			name:      "workspace holds .apogee",
			dirs:      []string{"repo/.git", "repo/.apogee"},
			workspace: "repo",
			want:      "repo",
		},
		{
			name:      "a parent up to the git top holds .apogee",
			dirs:      []string{"repo/.git", "repo/.apogee", "repo/a/b"},
			workspace: "repo/a/b",
			want:      "repo",
		},
		{
			name:      "the nearest .apogee wins",
			dirs:      []string{"repo/.git", "repo/.apogee", "repo/a/.apogee", "repo/a/b"},
			workspace: "repo/a/b",
			want:      "repo/a",
		},
		{
			name:      "a .git file stops the walk below an outer .apogee",
			dirs:      []string{"outer/.git", "outer/.apogee", "outer/wt/sub"},
			files:     map[string]string{"outer/wt/.git": "gitdir: ../.git/worktrees/wt\n"},
			workspace: "outer/wt/sub",
			want:      "outer/wt/sub",
		},
		{
			name:      "no repository means the workspace",
			dirs:      []string{"plain/.apogee", "plain/a"},
			workspace: "plain/a",
			want:      "plain/a",
		},
		{
			name:      "a .apogee file is not a Project root",
			dirs:      []string{"repo/.git", "repo/a"},
			files:     map[string]string{"repo/.apogee": "not a folder"},
			workspace: "repo/a",
			want:      "repo/a",
		},
		{
			name:      "a home git repo with ~/.apogee yields the workspace, not home",
			dirs:      []string{"home/.git", "home/.apogee", "home/proj/a"},
			workspace: "home/proj/a",
			home:      "home",
			want:      "home/proj/a",
		},
		{
			name:      "a home git repo is the git top for a .apogee below it",
			dirs:      []string{"home/.git", "home/.apogee", "home/proj/.apogee", "home/proj/a"},
			workspace: "home/proj/a",
			home:      "home",
			want:      "home/proj",
		},
		{
			name:      "the walk never passes the home folder",
			dirs:      []string{".git", ".apogee", "home/proj"},
			workspace: "home/proj",
			home:      "home",
			want:      "home/proj",
		},
		{
			name:      "the workspace is the home folder",
			dirs:      []string{"home/.apogee"},
			workspace: "home",
			home:      "home",
			want:      "-",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			base := realTempDir(t)
			mkdirs(t, base, c.dirs...)
			for rel, content := range c.files {
				writeFile(t, base, rel, content)
			}
			home := ""
			if c.home != "" {
				home = filepath.Join(base, filepath.FromSlash(c.home))
			}
			want := ""
			if c.want != "-" {
				want = filepath.Join(base, filepath.FromSlash(c.want))
			}

			got := projectroot.Resolve(filepath.Join(base, filepath.FromSlash(c.workspace)), home)

			if got != want {
				t.Errorf("Resolve = %q, want %q", got, want)
			}
		})
	}
}

// A workspace reached through a symlink: the walk follows the link to the real tree, and an answer
// that is the workspace itself keeps the workspace's own spelling.
func TestResolveSymlinkedWorkspace(t *testing.T) {
	t.Parallel()
	base := realTempDir(t)
	mkdirs(t, base, "real/.git", "real/.apogee", "real/a", "plain/b")
	linkToSub := filepath.Join(base, "link-a")
	linkToRoot := filepath.Join(base, "link-root")
	linkToPlain := filepath.Join(base, "link-plain")
	for target, link := range map[string]string{
		filepath.Join(base, "real", "a"):  linkToSub,
		filepath.Join(base, "real"):       linkToRoot,
		filepath.Join(base, "plain", "b"): linkToPlain,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	cases := []struct {
		name, workspace, want string
	}{
		{"a parent root comes back resolved", linkToSub, filepath.Join(base, "real")},
		{"the root itself keeps the workspace spelling", linkToRoot, linkToRoot},
		{"no repository keeps the workspace spelling", linkToPlain, linkToPlain},
	}
	for _, c := range cases {
		if got := projectroot.Resolve(c.workspace, ""); got != c.want {
			t.Errorf("%s: Resolve(%q) = %q, want %q", c.name, c.workspace, got, c.want)
		}
	}
}

// Resolve never fails: a workspace that does not exist — a mistyped `--workspace` — comes back as
// given, and the empty workspace is the empty answer.
func TestResolveUnresolvableWorkspace(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")

	cases := []struct{ name, workspace, want string }{
		{"a missing folder comes back as given", missing, missing},
		{"a relative missing folder comes back as given", "no/such/folder", "no/such/folder"},
		{"the empty workspace is the empty answer", "", ""},
	}
	for _, c := range cases {
		if got := projectroot.Resolve(c.workspace, ""); got != c.want {
			t.Errorf("%s: Resolve(%q) = %q, want %q", c.name, c.workspace, got, c.want)
		}
	}
}
