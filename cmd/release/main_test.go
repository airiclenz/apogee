package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordedCall is one external command a verb asked the runner for.
type recordedCall struct {
	dir  string
	env  []string
	name string
	args []string
}

// fakeRunner is the recording stand-in for execRunner: it answers each command from respond and
// remembers every call, so a test pins exactly what a verb would have run without running it.
type fakeRunner struct {
	respond func(call recordedCall) (string, error)

	mu    sync.Mutex
	calls []recordedCall
}

// run records the call and answers it.
func (f *fakeRunner) run(_ context.Context, dir string, env []string, name string, args ...string) (string, error) {
	call := recordedCall{dir: dir, env: slices.Clone(env), name: name, args: slices.Clone(args)}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return f.respond(call)
}

// recorded returns the calls made so far whose command is name.
func (f *fakeRunner) recorded(name string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matching []recordedCall
	for _, call := range f.calls {
		if call.name == name {
			matching = append(matching, call)
		}
	}
	return matching
}

// execute runs the program's command tree over commands with args, returning the error main
// would turn into an exit code and everything the command printed.
func execute(t *testing.T, commands runner, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := newRootCommand(commands)
	root.SetArgs(args)
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.ExecuteContext(t.Context())
	return output.String(), err
}

// TestBadCommandLineIsAUsageError pins the difference between the two ways this program can
// fail: a command line it cannot parse never starts a run, so it exits 2 — with the usage text
// when a verb's flags or arguments are wrong — and the runner is never reached.
func TestBadCommandLineIsAUsageError(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args         []string
		isUsageShown bool
	}{
		"unknown flag":   {args: []string{"dist", "--no-such-flag"}, isUsageShown: true},
		"stray argument": {args: []string{"dist", "extra"}, isUsageShown: true},
		"unknown verb":   {args: []string{"no-such-verb"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			commands := &fakeRunner{respond: func(recordedCall) (string, error) {
				return "", errors.New("the runner must not be reached")
			}}

			output, err := execute(t, commands, tc.args...)

			if err == nil {
				t.Fatalf("release %v succeeded", tc.args)
			}
			if code := exitCodeFor(err); code != exitBadUsage {
				t.Errorf("exit code = %d, want %d", code, exitBadUsage)
			}
			if tc.isUsageShown && !strings.Contains(output, "Usage:") {
				t.Errorf("output does not print the usage:\n%s", output)
			}
			if len(commands.calls) != 0 {
				t.Errorf("runner was called %d times, want none", len(commands.calls))
			}
		})
	}
}

// recordPathPattern captures the repo-relative path a decisionRecordPointer names in parentheses.
var recordPathPattern = regexp.MustCompile(`\(([^()]+\.md)\)`)

// TestHelpNamesADecisionRecordThatExists pins the pointer `release --help` closes on: the help
// prints it, and the Note path it names resolves from the module root — so moving or renaming the
// record breaks this test rather than leaving the help pointing nowhere.
func TestHelpNamesADecisionRecordThatExists(t *testing.T) {
	t.Parallel()
	commands := &fakeRunner{respond: func(recordedCall) (string, error) {
		return "", errors.New("the runner must not be reached")
	}}

	output, err := execute(t, commands, "--help")

	if err != nil {
		t.Fatalf("release --help: %v", err)
	}
	if !strings.Contains(output, decisionRecordPointer) {
		t.Fatalf("help does not print %q:\n%s", decisionRecordPointer, output)
	}
	match := recordPathPattern.FindStringSubmatch(decisionRecordPointer)
	if match == nil {
		t.Fatalf("pointer %q names no .md path in parentheses", decisionRecordPointer)
	}
	recordPath := filepath.Join(moduleRoot(t), filepath.FromSlash(match[1]))
	if _, err := os.Stat(recordPath); err != nil {
		t.Errorf("pointer names %s, which does not resolve: %v", match[1], err)
	}
}

// moduleRoot walks up from the test's working directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}
