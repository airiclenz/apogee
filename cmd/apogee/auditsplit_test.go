package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runAuditSplit runs `apogee audit-split args...` through the shipped root and returns its stdout
// and the error main would print and exit on.
func runAuditSplit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"audit-split"}, args...))
	err := root.Execute()
	if stderr.Len() != 0 {
		t.Errorf("audit-split wrote to stderr itself; main owns that print:\n%s", stderr.String())
	}
	return stdout.String(), err
}

// writeAuditSplitFile writes content to name under dir, creating its folders.
func writeAuditSplitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the folder of %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestAuditSplitSplitsTheWorkingDirectory pins the split form: the working directory is the
// workspace, the receipt reaches stdout and the run folder holds the scope.
// Not parallel: t.Chdir moves the whole process, and the working directory IS the workspace here.
func TestAuditSplitSplitsTheWorkingDirectory(t *testing.T) {
	workspace, run := t.TempDir(), t.TempDir()
	writeAuditSplitFile(t, workspace, "src/app.go", "package src\n\nfunc main() {}\n")
	writeAuditSplitFile(t, workspace, "src/app_test.go", "package src\n")
	writeAuditSplitFile(t, workspace, "other/skip.go", "package other\n")
	t.Chdir(workspace)

	stdout, err := runAuditSplit(t, run, "src", "BUGS", "64000")
	if err != nil {
		t.Fatalf("audit-split: %v\n%s", err, stdout)
	}
	for _, want := range []string{"files=2\n", "src=1\n", "tests=1\n", "parts=1\n", "focus=bugs\n", "summary=2 files in 1 parts"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the receipt lacks %q:\n%s", want, stdout)
		}
	}
	scope, err := os.ReadFile(filepath.Join(run, "scope.txt"))
	if err != nil {
		t.Fatalf("read scope.txt: %v", err)
	}
	if got, want := string(scope), "src/app.go\nsrc/app_test.go\n"; got != want {
		t.Errorf("scope.txt = %q, want %q", got, want)
	}
}

// TestAuditSplitAnswersTheFlagsForm pins `--flags <RUN>`: the ground truth's concurrency answer as
// a receipt, with no workspace involved.
func TestAuditSplitAnswersTheFlagsForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		bundle string
		want   string
	}{
		{name: "says yes", bundle: "# Ground truth\n**CONCURRENCY:** yes\n", want: "concurrency=yes\n"},
		{name: "says no", bundle: "# Ground truth\nCONCURRENCY: no\n", want: "concurrency=no\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := t.TempDir()
			writeAuditSplitFile(t, run, "bundle.md", tc.bundle)

			stdout, err := runAuditSplit(t, "--flags", run)
			if err != nil {
				t.Fatalf("audit-split --flags: %v", err)
			}
			if !strings.HasPrefix(stdout, tc.want) || !strings.Contains(stdout, "summary=") {
				t.Errorf("stdout = %q, want it to open with %q and carry a summary", stdout, tc.want)
			}
		})
	}
}

// TestAuditSplitFailsABadArgvWithASummary pins the refusals: a malformed argv or a missing run
// folder exits non-zero, prints a `summary=audit-split: ` line on stdout for the script stage, and
// hands main that cause as the one error line it prints.
func TestAuditSplitFailsABadArgvWithASummary(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent")
	cases := []struct {
		name  string
		args  []string
		cause string
	}{
		{name: "no arguments", args: nil, cause: "usage: apogee audit-split <RUN>"},
		{name: "run only", args: []string{"run"}, cause: "usage: apogee audit-split <RUN>"},
		{name: "five arguments", args: []string{"a", "b", "c", "d", "e"}, cause: "usage: apogee audit-split <RUN>"},
		{name: "flags without a run", args: []string{"--flags"}, cause: "usage: apogee audit-split --flags"},
		{name: "flags with two runs", args: []string{"--flags", "a", "b"}, cause: "usage: apogee audit-split --flags"},
		{name: "missing run folder", args: []string{missing, "."}, cause: "workflow folder not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, err := runAuditSplit(t, tc.args...)
			if err == nil {
				t.Fatalf("audit-split %q succeeded; want a failure", tc.args)
			}
			if code := exitCodeFor(err); code == 0 {
				t.Errorf("exit code = 0, want non-zero")
			}
			if !strings.Contains(stdout, "summary=audit-split: "+tc.cause) {
				t.Errorf("stdout lacks the summary naming %q:\n%s", tc.cause, stdout)
			}
			if message := err.Error(); !strings.HasPrefix(message, "audit-split: "+tc.cause) || strings.Contains(message, "\n") {
				t.Errorf("error = %q, want one line opening with %q", message, "audit-split: "+tc.cause)
			}
		})
	}
}

// TestAuditSplitIsHiddenFromHelp pins the command's standing: registered on the shipped root,
// silent about usage and errors (main owns both), and absent from `apogee --help`.
func TestAuditSplitIsHiddenFromHelp(t *testing.T) {
	t.Parallel()

	var found *cobra.Command
	for _, sub := range subcommands() {
		if sub.Name() == "audit-split" {
			found = sub
		}
	}
	if found == nil {
		t.Fatal("`audit-split` is not registered in subcommands()")
	}
	if !found.Hidden {
		t.Error("audit-split is not hidden; it is no public contract")
	}
	if !found.SilenceUsage || !found.SilenceErrors {
		t.Error("the command dumps usage or prints its own error; main owns both")
	}

	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)
	var help strings.Builder
	root.SetOut(&help)
	root.SetErr(&help)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("apogee --help: %v", err)
	}
	if strings.Contains(help.String(), "audit-split") {
		t.Errorf("`apogee --help` names audit-split:\n%s", help.String())
	}
}
