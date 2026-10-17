package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// outcome is what one run of the root command produced: its exit status, stdout, and the error
// main would print on stderr.
type outcome struct {
	code   int
	stdout string
	err    error
}

// run executes the root command with args, as main would.
func run(t *testing.T, args ...string) outcome {
	t.Helper()

	cmd := newRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.ExecuteContext(context.Background())
	code := 0
	if err != nil {
		code = exitCodeFor(err)
	}
	return outcome{code: code, stdout: stdout.String(), err: err}
}

// sessionsWithFixture is a temp sessions dir holding the hero fixture.
func sessionsWithFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSession(t, dir, "hero.json", readFixture(t), time.Now())
	return dir
}

// readFixture returns the committed hero session record.
func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(heroFixture)
	if err != nil {
		t.Fatalf("read %s: %v", heroFixture, err)
	}
	return data
}

// writeSession saves a session record as dir/name with the given modification time.
func writeSession(t *testing.T, dir, name string, record []byte, modified time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, record, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestRoot_JudgesTheSession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		args   []string
		code   int
		detail string
	}{
		{name: "contains passes", args: []string{"--entry", "kind=interjected", "--contains", "CHANGELOG"},
			code: 0, detail: `entry 7 interjected "also add a CHANGELOG entry for the fix": contains "CHANGELOG"`},
		{name: "contains fails", args: []string{"--entry", "kind=toolCall,tool=Tests", "--contains", "PASS"},
			code: 1, detail: `entry 2 toolCall Tests FAIL: does not contain "PASS"`},
		{name: "after passes", args: []string{"--entry", "kind=toolCall,target=CHANGELOG.md", "--after", "kind=interjected"},
			code: 0, detail: `after {kind: interjected} (entry 7)`},
		{name: "before fails", args: []string{"--entry", "kind=interjected", "--before", "kind=toolCall,tool=Tests"},
			code: 1, detail: `is not before {kind: toolCall, tool: Tests} (entry 2)`},
		{name: "unlocated entry fails", args: []string{"--entry", "kind=toolCall,tool=Git", "--contains", "x"},
			code: 1, detail: `no entry matches {kind: toolCall, tool: Git}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := run(t, append(tc.args, "--sessions", sessionsWithFixture(t))...)

			if got.code != tc.code {
				t.Errorf("exit %d, want %d (err %v)", got.code, tc.code, got.err)
			}
			lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], tc.detail) {
				t.Errorf("stdout = %q, want one line containing %q", got.stdout, tc.detail)
			}
			if tc.code == exitRunFailed && !errors.Is(got.err, errExpectFailed) {
				t.Errorf("err = %v, want errExpectFailed so main prints nothing more", got.err)
			}
		})
	}
}

func TestRoot_UsageErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "entry alone", args: []string{"--entry", "kind=interjected"},
			want: "want at least one of --contains, --before or --after"},
		{name: "no entry", args: []string{"--contains", "x"}, want: `"entry" not set`},
		{name: "bad entry selector", args: []string{"--entry", "kind=nope", "--contains", "x"},
			want: `--entry: kind: unknown entry kind "nope"`},
		{name: "entry selector without kind", args: []string{"--entry", "tool=Tests", "--contains", "x"},
			want: "--entry: want kind="},
		{name: "bad before selector", args: []string{"--entry", "kind=interjected", "--before", "kind=toolCall,colour=red"},
			want: `--before: unknown key "colour"`},
		{name: "bad after selector", args: []string{"--entry", "kind=interjected", "--after", "kind=toolCall,nth=0"},
			want: "--after: nth: want a positive integer or last"},
		{name: "stray argument", args: []string{"--entry", "kind=interjected", "--contains", "x", "extra"},
			want: "unknown command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := run(t, append(tc.args, "--sessions", sessionsWithFixture(t))...)

			if got.code != exitBadUsage {
				t.Errorf("exit %d, want %d (err %v)", got.code, exitBadUsage, got.err)
			}
			if got.err == nil || !strings.Contains(got.err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", got.err, tc.want)
			}
		})
	}
}

func TestRoot_EmptySessionsDirIsARunFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	got := run(t, "--entry", "kind=interjected", "--contains", "CHANGELOG", "--sessions", dir)

	if got.code != exitRunFailed {
		t.Errorf("exit %d, want %d (err %v)", got.code, exitRunFailed, got.err)
	}
	if got.err == nil || !strings.Contains(got.err.Error(), "no session saved under "+dir) {
		t.Errorf("err = %v, want it to name %q", got.err, "no session saved under "+dir)
	}
}

func TestRoot_JudgesTheNewestSession(t *testing.T) {
	t.Parallel()
	mutated := recordOf(t, mutatedHeroEntries())
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	cases := []struct {
		name          string
		heroModified  time.Time
		otherModified time.Time
		code          int
	}{
		{name: "the hero take is newer", heroModified: newer, otherModified: older, code: 0},
		{name: "the mutated take is newer", heroModified: older, otherModified: newer, code: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// The names sort against the mtimes in one case, so a pick by name fails one of them.
			writeSession(t, dir, "a-hero.json", readFixture(t), tc.heroModified)
			writeSession(t, dir, "b-mutated.json", mutated, tc.otherModified)

			got := run(t, "--entry", "kind=interjected", "--contains", "CHANGELOG", "--sessions", dir)

			if got.code != tc.code {
				t.Errorf("exit %d, want %d; stdout %q, err %v", got.code, tc.code, got.stdout, got.err)
			}
		})
	}
}

func TestRoot_SessionsDefaultsUnderPhotonWork(t *testing.T) {
	work := t.TempDir()
	sessions := filepath.Join(work, "home", ".apogee", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", sessions, err)
	}
	writeSession(t, sessions, "hero.json", readFixture(t), time.Now())
	t.Setenv(photonWorkEnv, work)

	got := run(t, "--entry", "kind=interjected", "--contains", "CHANGELOG")

	if got.code != 0 {
		t.Errorf("exit %d, want 0 (err %v)", got.code, got.err)
	}
	if !strings.Contains(got.stdout, `contains "CHANGELOG"`) {
		t.Errorf("stdout = %q, want the passing detail", got.stdout)
	}
}

func TestRoot_PhotonWorkUnsetIsAUsageError(t *testing.T) {
	t.Setenv(photonWorkEnv, "")
	if err := os.Unsetenv(photonWorkEnv); err != nil {
		t.Fatalf("unset %s: %v", photonWorkEnv, err)
	}

	got := run(t, "--entry", "kind=interjected", "--contains", "CHANGELOG")

	if got.code != exitBadUsage {
		t.Errorf("exit %d, want %d (err %v)", got.code, exitBadUsage, got.err)
	}
	if got.err == nil || !strings.Contains(got.err.Error(), photonWorkEnv) {
		t.Errorf("err = %v, want it to name %s", got.err, photonWorkEnv)
	}
}
