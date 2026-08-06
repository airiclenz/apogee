//go:build windows

package tui

import (
	"os"
	"slices"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestInjectTerminalEnvNamesTheTerminal is the rule itself, in the three shapes a Windows
// environment arrives in. The gate is TERM: a shell that already named a terminal knows more
// about it than a hardcoded default does, and apogee leaves that whole description — COLORTERM
// included — alone.
func TestInjectTerminalEnvNamesTheTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		base  []string
		added []string // "" entries mean nothing is added at all
	}{
		{
			name:  "both keys when neither is set",
			base:  []string{"WT_SESSION=abc"},
			added: []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name:  "only TERM when COLORTERM is already set",
			base:  []string{"COLORTERM=truecolor"},
			added: []string{"TERM=xterm-256color"},
		},
		{
			name: "nothing when TERM is already set",
			base: []string{"TERM=xterm-ghostty"},
		},
		{
			name: "nothing when a WSL-style shell set both",
			base: []string{"TERM=screen-256color", "COLORTERM=24bit"},
		},
		{
			// An empty TERM names no terminal type and is exactly the xtermCaps("") → noCaps case
			// this rule exists to remove, so it counts as unset rather than as a description to
			// respect.
			name:  "an empty TERM counts as unset",
			base:  []string{"TERM="},
			added: []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := injectTerminalEnv(tc.base)
			if len(tc.added) == 0 {
				if got != nil {
					t.Fatalf("injectTerminalEnv(%v) = %v, want nil", tc.base, got)
				}
				return
			}
			want := append(slices.Clone(tc.base), tc.added...)
			if !slices.Equal(got, want) {
				t.Errorf("injectTerminalEnv(%v) = %v, want %v", tc.base, got, want)
			}
		})
	}
}

// TestInjectTerminalEnvNeverMutatesTheEnvironmentItIsGiven: the injected slice is bubbletea's
// alone. Writing through to the caller's environment would leak an invented TERM into every tool,
// $EDITOR and shell apogee spawns, which is the one thing this rule must not do.
func TestInjectTerminalEnvNeverMutatesTheEnvironmentItIsGiven(t *testing.T) {
	t.Parallel()
	base := []string{"WT_SESSION=abc", "PATH=C:\\bin"}
	before := slices.Clone(base)
	got := injectTerminalEnv(base)
	if !slices.Equal(base, before) {
		t.Errorf("injectTerminalEnv rewrote its argument: %v, want %v", base, before)
	}
	// A fresh backing array, not a re-slice of the caller's: appending to the result must not be
	// observable in the argument either.
	got[0] = "WT_SESSION=rewritten"
	if base[0] != "WT_SESSION=abc" {
		t.Errorf("the result shares its backing array with the argument: base[0] = %q", base[0])
	}
}

// TestProgramEnvironInjectsNothingWhenStdoutIsNotATerminal: with stdout redirected there is no
// painter to configure, so the environment bubbletea reports back to the model stays the
// machine's own. A `go test` run is that case, which is what lets the assertion be direct.
func TestProgramEnvironInjectsNothingWhenStdoutIsNotATerminal(t *testing.T) {
	t.Parallel()
	if stdoutIsTerminal() {
		t.Skip("stdout is a character device in this run; the redirected branch cannot be exercised")
	}
	if env := programEnviron(); env != nil {
		t.Errorf("programEnviron() = %v with stdout redirected, want nil", env)
	}
}

// TestInjectedEnvironmentResolvesTheSameColorProfile is the anti-regression pin, and it compares
// PROFILES rather than asserting "truecolor" — the claim is that naming the terminal changes what
// the painter can do and nothing about what it can paint with.
//
// colorprofile.Detect reads the same slice bubbletea hands the renderer (tea.go:1083). Today, on
// a Windows terminal with TERM empty, it resolves through the Windows API path; with
// TERM=xterm-256color it would resolve through the terminfo path to ANSI256 wherever WT_SESSION
// is absent, visibly flattening the palette and the spinner gradient. The COLORTERM clause in
// [injectTerminalEnv] is what holds the two equal, and this test is what would notice if it were
// dropped.
//
// TTY_FORCE is colorprofile's own hook for exactly this: it makes Detect take the terminal branch
// without a terminal underneath the test (colorprofile/env.go, isTTYForced).
func TestInjectedEnvironmentResolvesTheSameColorProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		base []string
	}{
		{name: "windows terminal", base: []string{"TTY_FORCE=1", "WT_SESSION=6d8b8f1e"}},
		{name: "vs code integrated terminal", base: []string{"TTY_FORCE=1", "TERM_PROGRAM=vscode", "COLORTERM=truecolor"}},
		{name: "conhost", base: []string{"TTY_FORCE=1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			injected := injectTerminalEnv(tc.base)
			if injected == nil {
				t.Fatalf("injectTerminalEnv(%v) = nil, want an injected environment", tc.base)
			}
			today := colorprofile.Detect(os.Stdout, tc.base)
			named := colorprofile.Detect(os.Stdout, injected)
			if named != today {
				t.Errorf("naming the terminal moved the colour profile: %v -> %v (env %v)", today, named, injected)
			}
		})
	}
}
