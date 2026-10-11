package auditsplit

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// noGit is the listing seam with no git on PATH, so the plan lists by walking the workspace.
var noGit = gitexec.Host{Look: func(string) (string, error) { return "", exec.ErrNotFound }}

// failingGit is the listing seam with a git on PATH that fails every call, as git does outside a
// repository: the plan falls back to the walk.
var failingGit = gitexec.Host{
	Look: func(string) (string, error) { return "/usr/bin/git", nil },
	Spawn: func(context.Context, subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		return subprocess.SubprocessResult{ExitCode: 128, CombinedOutput: "fatal: not a git repository"}, nil
	},
	SpawnTo: func(context.Context, subprocess.SubprocessSpec, io.Writer) (subprocess.SubprocessResult, error) {
		return subprocess.SubprocessResult{ExitCode: 128, CombinedOutput: "fatal: not a git repository"}, nil
	},
}

// body is a file of n newline-terminated lines.
func body(n int) string {
	return strings.Repeat("x := 1\n", n)
}

// writeWorkspace writes files (slash-separated name to content) under a fresh temp root and
// returns it, skipping the names this host's file system cannot hold.
func writeWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		if runtime.GOOS == "windows" && strings.ContainsAny(name, "*?\"\n") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// planOf plans root's scope through the walk with knobs, failing the test on an error.
func planOf(t *testing.T, root, scope string, knobs Knobs) plan {
	t.Helper()
	got, err := makePlan(context.Background(), planRequest{root: root, scope: scope, knobs: knobs, git: noGit})
	if err != nil {
		t.Fatalf("makePlan(%q): %v", scope, err)
	}
	return got
}

// partFiles is the plan's parts as name to files.
func partFiles(p plan) map[string][]string {
	parts := map[string][]string{}
	for _, part := range p.parts {
		parts[part.name] = part.files
	}
	return parts
}

// equalParts reports whether two part maps hold the same parts with the same files in order.
func equalParts(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, files := range a {
		if other, ok := b[name]; !ok || !slices.Equal(files, other) {
			return false
		}
	}
	return true
}

// Each split mechanic cuts its fixture into exactly the parts split.sh cut it into.
func TestMakePlanSplitsLikeSplitSh(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		files map[string]string
		knobs Knobs
		want  map[string][]string
	}{
		{
			name:  "a scope that fits is the one part all",
			files: map[string]string{"a/x.go": body(10), "b.go": body(10)},
			want:  map[string][]string{"all": {"a/x.go", "b.go"}},
		},
		{
			name: "top-level split, a lone root file keeping its part",
			files: map[string]string{
				"r.go":   body(10),
				"a/1.go": body(10), "a/2.go": body(10), "a/3.go": body(10),
				"b/1.go": body(10), "b/2.go": body(10), "b/3.go": body(10),
			},
			knobs: Knobs{PartLines: "40"},
			want: map[string][]string{
				"root": {"r.go"},
				"a":    {"a/1.go", "a/2.go", "a/3.go"},
				"b":    {"b/1.go", "b/2.go", "b/3.go"},
			},
		},
		{
			name: "an oversized directory re-splits one level deeper",
			files: map[string]string{
				"a/d1.go": body(10), "a/d2.go": body(10), "a/d3.go": body(10),
				"a/x/1.go": body(10), "a/x/2.go": body(10), "a/x/3.go": body(10),
				"a/y/1.go": body(10), "a/y/2.go": body(10), "a/y/3.go": body(10),
			},
			knobs: Knobs{PartLines: "40"},
			want: map[string][]string{
				"a":   {"a/d1.go", "a/d2.go", "a/d3.go"},
				"a-x": {"a/x/1.go", "a/x/2.go", "a/x/3.go"},
				"a-y": {"a/y/1.go", "a/y/2.go", "a/y/3.go"},
			},
		},
		{
			name: "a flat oversized part chunks in order, no merge pushing a chunk over",
			files: map[string]string{
				"a/1.go": body(10), "a/2.go": body(10), "a/3.go": body(10), "a/4.go": body(10), "a/5.go": body(10),
			},
			knobs: Knobs{PartLines: "25"},
			want: map[string][]string{
				"a-1": {"a/1.go", "a/2.go"},
				"a-2": {"a/3.go", "a/4.go"},
				"a-3": {"a/5.go"},
			},
		},
		{
			name: "the file bound chunks too",
			files: map[string]string{
				"a/1.go": body(1), "a/2.go": body(1), "a/3.go": body(1), "a/4.go": body(1),
			},
			knobs: Knobs{PartFiles: "2"},
			want: map[string][]string{
				"a-1": {"a/1.go", "a/2.go"},
				"a-2": {"a/3.go", "a/4.go"},
			},
		},
		{
			name: "a single huge file keeps its part's name",
			files: map[string]string{
				"a/big.go": body(100),
				"b/1.go":   body(10), "b/2.go": body(10), "b/3.go": body(10),
			},
			knobs: Knobs{PartLines: "50"},
			want: map[string][]string{
				"a": {"a/big.go"},
				"b": {"b/1.go", "b/2.go", "b/3.go"},
			},
		},
		{
			name: "a small part merges into the next of its directory",
			files: map[string]string{
				"a/x/1.go": body(5),
				"a/y/1.go": body(5), "a/y/2.go": body(5), "a/y/3.go": body(5),
				"a/z/1.go": body(10), "a/z/2.go": body(10), "a/z/3.go": body(10),
			},
			knobs: Knobs{PartLines: "35"},
			want: map[string][]string{
				"a-y": {"a/x/1.go", "a/y/1.go", "a/y/2.go", "a/y/3.go"},
				"a-z": {"a/z/1.go", "a/z/2.go", "a/z/3.go"},
			},
		},
		{
			name: "the last small part merges into the previous one kept",
			files: map[string]string{
				"a/v/1.go": body(14), "a/v/2.go": body(13), "a/v/3.go": body(13),
				"a/w/1.go": body(5), "a/w/2.go": body(5), "a/w/3.go": body(5),
				"a/x/1.go": body(5),
			},
			knobs: Knobs{PartLines: "50"},
			want: map[string][]string{
				"a-v": {"a/v/1.go", "a/v/2.go", "a/v/3.go"},
				"a-w": {"a/w/1.go", "a/w/2.go", "a/w/3.go", "a/x/1.go"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeWorkspace(t, tc.files)

			got := planOf(t, root, ".", tc.knobs)

			if parts := partFiles(got); !equalParts(parts, tc.want) {
				t.Errorf("parts = %q, want %q", parts, tc.want)
			}
		})
	}
}

// The parts come in byte order of their names, each with the wc -l line count of its files —
// a last line with no newline uncounted — and whether a file of it holds a concurrency primitive.
func TestMakePlanCountsLinesAndConcurrencyPerPart(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{
		"b/1.go": body(30), "b/2.go": body(30), "b/3.go": body(30) + "no newline",
		"a/1.go": body(30), "a/2.go": body(30), "a/3.go": "go func() {}()\n",
	})

	got := planOf(t, root, ".", Knobs{PartLines: "100"})

	var names []string
	for _, part := range got.parts {
		names = append(names, part.name)
	}
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Fatalf("parts = %q, want a then b", names)
	}
	if a, b := got.parts[0], got.parts[1]; a.lines != 61 || b.lines != 90 || !a.hasConcurrency || b.hasConcurrency {
		t.Errorf("parts = %+v, want a at 61 lines with concurrency and b at 90 without", got.parts)
	}
}

// Test files classify by name; a scope of test files only is split from the scope itself, with
// no source and no test list.
func TestMakePlanClassifiesTestFiles(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{
		"a/x.go": body(1), "a/x_test.go": body(1), "tests/t.py": body(1), "test_y.py": body(1),
		"web/c.spec.ts": body(1), "web/App.test.tsx": body(1), "cs/FooTests.cs": body(1),
	})
	for _, tc := range []struct {
		name, scope        string
		wantSrc, wantTests []string
		wantAll            []string
	}{
		{
			name:      "mixed scope",
			scope:     ".",
			wantSrc:   []string{"a/x.go"},
			wantTests: []string{"a/x_test.go", "cs/FooTests.cs", "test_y.py", "tests/t.py", "web/App.test.tsx", "web/c.spec.ts"},
			wantAll:   []string{"a/x.go"},
		},
		{
			name:    "test-only scope",
			scope:   "tests web",
			wantAll: []string{"tests/t.py", "web/App.test.tsx", "web/c.spec.ts"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := planOf(t, root, tc.scope, Knobs{})

			if !slices.Equal(got.src, tc.wantSrc) || !slices.Equal(got.tests, tc.wantTests) {
				t.Errorf("src = %q, tests = %q; want %q and %q", got.src, got.tests, tc.wantSrc, tc.wantTests)
			}
			if parts := partFiles(got); !equalParts(parts, map[string][]string{"all": tc.wantAll}) {
				t.Errorf("parts = %q, want the one part all = %q", parts, tc.wantAll)
			}
		})
	}
}

// Docs, dependencies, build output, fixtures and generated files never enter the scope, and
// neither does a `.git` folder's content.
func TestMakePlanExcludesWhatIsNeverSource(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{
		"docs/a.go": body(1), "vendor/v.go": body(1), "sub/node_modules/n.go": body(1),
		"a/testdata/f.go": body(1), "web/dist/app.js": body(1), "README.md": body(1), "go.sum": body(1),
		"CHANGELOG.txt": body(1), "x_generated.go": body(1), "api.pb.go": body(1), "logo.png": body(1),
		".git/config": body(1), "keep.go": body(1), "sub/keep.go": body(1),
	})

	got := planOf(t, root, ".", Knobs{})

	if want := []string{"keep.go", "sub/keep.go"}; !slices.Equal(got.scope, want) {
		t.Errorf("scope = %q, want only %q", got.scope, want)
	}
}

// Every name is one file: spaces, `*`, `?` and `[` are never a glob or a word split, a scope
// entry is never glob-expanded, and a name holding a newline is skipped.
func TestMakePlanReadsNamesAsTheyAre(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{
		"a b.go": body(1), "x*y.go": body(1), "q?.go": body(1), "[z].go": body(1), "z.go": body(1),
		"dir with space/one.go": body(1), "new\nline.go": body(1),
	})
	for _, tc := range []struct {
		name, scope string
		want        []string
	}{
		{"whole workspace", ".", []string{"[z].go", "a b.go", "dir with space/one.go", "q?.go", "x*y.go", "z.go"}},
		{"literal glob characters", "* [z].go", []string{"[z].go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := planOf(t, root, tc.scope, Knobs{})

			want := slices.DeleteFunc(slices.Clone(tc.want), func(name string) bool {
				return runtime.GOOS == "windows" && strings.ContainsAny(name, "*?")
			})
			if !slices.Equal(got.scope, want) {
				t.Errorf("scope = %q, want %q", got.scope, want)
			}
		})
	}
}

// A scope entry names a file or a folder and nothing that merely shares its prefix; `./`, a
// trailing `/` and the whole-workspace words are read as split.sh read them, entries split on
// whitespace and overlapping entries list a file once.
func TestMakePlanResolvesScopeEntries(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{
		"a/x.go": body(1), "ab/y.go": body(1), "c.go": body(1),
	})
	for _, tc := range []struct {
		scope string
		want  []string
	}{
		{"a", []string{"a/x.go"}},
		{"./a/", []string{"a/x.go"}},
		{"a/x.go", []string{"a/x.go"}},
		{"a\tc.go\na", []string{"a/x.go", "c.go"}},
		{"all", []string{"a/x.go", "ab/y.go", "c.go"}},
		{"everything", []string{"a/x.go", "ab/y.go", "c.go"}},
		{"./", []string{"a/x.go", "ab/y.go", "c.go"}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()

			got := planOf(t, root, tc.scope, Knobs{})

			if !slices.Equal(got.scope, tc.want) {
				t.Errorf("scope = %q, want %q", got.scope, tc.want)
			}
		})
	}
}

// A scope naming no listed file fails, naming the scope.
func TestMakePlanFailsOnAnEmptyScope(t *testing.T) {
	t.Parallel()
	root := writeWorkspace(t, map[string]string{"a/x.go": body(1)})

	_, err := makePlan(context.Background(), planRequest{root: root, scope: "missing", git: noGit})

	if !errors.Is(err, errNothingInScope) || !strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want errNothingInScope naming the scope", err)
	}
}

// With no git on PATH, or a git that cannot list the workspace, the walk lists every regular
// file — ignored ones included, since nothing reads .gitignore without git.
func TestMakePlanWalksWithoutGit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		git  gitexec.Host
	}{
		{"no git on PATH", noGit},
		{"git fails", failingGit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeWorkspace(t, map[string]string{".gitignore": "ignored.go\n", "ignored.go": body(1), "kept.go": body(1)})

			got, err := makePlan(context.Background(), planRequest{root: root, scope: ".", git: tc.git})

			if want := []string{".gitignore", "ignored.go", "kept.go"}; err != nil || !slices.Equal(got.scope, want) {
				t.Errorf("scope = %q (err %v), want the walk's %q", got.scope, err, want)
			}
		})
	}
}

// In a git repository the listing is git's: tracked and untracked files, never an ignored one,
// never a tracked file gone from disk, and a name git would quote by default as it is on disk.
func TestMakePlanListsThroughGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this host")
	}
	root := writeWorkspace(t, map[string]string{
		".gitignore": "ignored.go\n", "ignored.go": body(1), "kept.go": body(1),
		"café.go": body(1), "q\"t.go": body(1), "gone.go": body(1),
	})
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", "gone.go", "kept.go")
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}

	got, err := makePlan(context.Background(), planRequest{root: root, scope: ".", git: gitexec.OS()})

	want := []string{".gitignore", "café.go", "kept.go"}
	if runtime.GOOS != "windows" {
		want = append(want, "q\"t.go")
	}
	slices.Sort(want)
	if err != nil || !slices.Equal(got.scope, want) {
		t.Errorf("scope = %q (err %v), want git's listing %q", got.scope, err, want)
	}
}

// gitIn runs one git command in dir with no inherited GIT_* variable and no system or global
// config, so the fixture's own repository alone answers.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The part line bound is PART_LINES when set, else half the budget at forty bytes a line clamped
// to 200..8000, and 8000 with no budget; PART_FILES and GROUP_PARTS default to 15 and 8; a knob
// that is not a whole number is an error naming it.
func TestResolveBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		knobs     Knobs
		partBytes string
		want      bounds
		wantError string
	}{
		{"defaults", Knobs{}, "", bounds{8000, 15, 8}, ""},
		{"PART_LINES wins", Knobs{PartLines: "150"}, "80000", bounds{150, 15, 8}, ""},
		{"derived from the budget", Knobs{}, "80000", bounds{1000, 15, 8}, ""},
		{"clamped low", Knobs{}, "400", bounds{200, 15, 8}, ""},
		{"clamped high", Knobs{}, "10000000", bounds{8000, 15, 8}, ""},
		{"past the int range", Knobs{}, "99999999999999999999999", bounds{8000, 15, 8}, ""},
		{"unknown budget", Knobs{}, "0", bounds{8000, 15, 8}, ""},
		{"a budget not in digits", Knobs{}, "8k", bounds{8000, 15, 8}, ""},
		{"file and group knobs", Knobs{PartFiles: "4", GroupParts: "2"}, "", bounds{8000, 4, 2}, ""},
		{"PART_LINES malformed", Knobs{PartLines: "12x"}, "", bounds{}, "PART_LINES is not a whole number: 12x"},
		{"PART_FILES malformed", Knobs{PartFiles: "-1"}, "", bounds{}, "PART_FILES is not a whole number: -1"},
		{"GROUP_PARTS malformed", Knobs{GroupParts: " 8"}, "", bounds{}, "GROUP_PARTS is not a whole number:  8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveBounds(tc.knobs, tc.partBytes)

			if tc.wantError != "" {
				if err == nil || err.Error() != tc.wantError {
					t.Errorf("err = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("resolveBounds = %+v (err %v), want %+v", got, err, tc.want)
			}
		})
	}
}

// KnobsFromEnv reads PART_LINES, PART_FILES and GROUP_PARTS through the lookup it is handed.
func TestKnobsFromEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"PART_LINES": "100", "PART_FILES": "4", "GROUP_PARTS": "2", "OTHER": "x"}

	got := KnobsFromEnv(func(key string) string { return env[key] })

	if want := (Knobs{PartLines: "100", PartFiles: "4", GroupParts: "2"}); got != want {
		t.Errorf("KnobsFromEnv = %+v, want %+v", got, want)
	}
}
