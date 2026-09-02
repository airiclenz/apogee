package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// runProbeTerminal executes `apogee probe terminal` against streams that are files rather than a
// terminal — which is what CI has, what `apogee probe terminal < /dev/null` produces, and what
// `go test` gives a test process. It returns everything the command printed and the error it
// ended with.
func runProbeTerminal(t *testing.T) (string, error) {
	t.Helper()
	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatalf("create a stand-in for stdin: %v", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("create a stand-in for stdout: %v", err)
	}
	defer func() { _ = out.Close() }()

	previous := probeTerminalStreams
	probeTerminalStreams = func() (*os.File, *os.File) { return in, out }
	t.Cleanup(func() { probeTerminalStreams = previous })

	var printed bytes.Buffer
	cmd := newProbeCommand()
	cmd.SetOut(&printed)
	cmd.SetErr(&printed)
	cmd.SetArgs([]string{"terminal"})
	return printed.String(), cmd.ExecuteContext(context.Background())
}

// The command degrades cleanly when it has no terminal to measure: a clear refusal, a non-zero
// exit and — the part that matters most — no hang. Every measurement it makes writes a sequence
// and waits for the answer, so a redirected stream would otherwise sit through one timeout per
// observation before printing a table of "no answer" rows that read like findings.
//
// This is also how CI hits the command, which is why it is pinned rather than left to the
// interactive path nobody can drive from a test.
func TestProbeTerminalRefusesWhenThereIsNoTerminal(t *testing.T) {
	printed, err := runProbeTerminal(t)

	if err == nil {
		t.Fatal("`apogee probe terminal` ran against streams that are not a terminal")
	}
	if !errors.Is(err, errProbeTerminalNeedsATerminal) {
		t.Fatalf("the refusal is not the not-a-terminal one: %v", err)
	}
	if code := exitCodeFor(err); code == 0 {
		t.Errorf("exit code = %d; a refusal must be non-zero", code)
	}
	for _, want := range []string{"stdin and stdout must both be a terminal", "apogee probe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %q", want, err.Error())
		}
	}
	if printed != "" {
		t.Errorf("a refused run still printed a report: %q", printed)
	}
}

// A MISMATCH is a finding, not a usage mistake, so it earns a non-zero exit of its own — that is
// what lets the acceptance of a plan item, or a script, check the terminal mechanically instead of
// reading the table. The refusal above and this one must stay distinct errors for the same reason.
func TestProbeTerminalMismatchIsItsOwnFailure(t *testing.T) {
	t.Parallel()
	if errors.Is(errProbeTerminalMismatch, errProbeTerminalNeedsATerminal) {
		t.Fatal("the mismatch failure and the not-a-terminal refusal are the same error")
	}
	if code := exitCodeFor(errProbeTerminalMismatch); code == 0 {
		t.Errorf("exit code for a MISMATCH = %d; want non-zero", code)
	}
	if !strings.Contains(errProbeTerminalMismatch.Error(), "MISMATCH") {
		t.Errorf("the failure does not name the verdict it carries: %q", errProbeTerminalMismatch.Error())
	}
}

// The probe group carries three children now, and `terminal` is reachable through the real tree:
// the measurement is only useful if it can be run by the name the report and the plan call it.
func TestProbeTerminalIsRegisteredUnderProbe(t *testing.T) {
	t.Parallel()
	var names []string
	for _, child := range newProbeCommand().Commands() {
		names = append(names, child.Name())
	}
	for _, want := range []string{"host", "model", "terminal"} {
		if !slices.Contains(names, want) {
			t.Errorf("`apogee probe` does not carry a %q child; it carries %v", want, names)
		}
	}
}
