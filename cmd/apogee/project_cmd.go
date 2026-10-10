package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
)

// ----------------------------------------------------------------------------
// `apogee project adopt` (ADR 0096 §4)
// ----------------------------------------------------------------------------
//
// The terminal twin of the session's adoption pane: the Project config's proposed Allow rules, one
// question each, answered from a shell — after a pull, before a session, or beside a running one,
// which picks the answer up live through its adoption-store watcher. Nothing here grants anything
// the file does not already state: an answer pins the exact text the file holds now, as the pane's
// does, and a teammate's later edit to that text is proposed afresh.

// projectAdoptDeps is what `apogee project adopt` takes from its host rather than deciding for
// itself: the terminal it asks through and whether stdin is one. The zero value is the production
// one.
type projectAdoptDeps struct {
	// terminal is where the questions are printed and answered; its zero fields are this process's
	// own (loginTerminal.withDefaults). Only out and openInput are read.
	terminal loginTerminal
	// isTerminal reports whether stdin is a terminal the command may ask on. Nil checks os.Stdin.
	isTerminal func() bool
}

// withDefaults fills every unset field with this process's own terminal.
func (d projectAdoptDeps) withDefaults() projectAdoptDeps {
	d.terminal = d.terminal.withDefaults()
	if d.isTerminal == nil {
		d.isTerminal = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
	}
	return d
}

// newProjectCommand builds `apogee project` over this process's own terminal.
func newProjectCommand() *cobra.Command {
	return newProjectCommandWith(projectAdoptDeps{})
}

// newProjectCommandWith is newProjectCommand over the terminal the caller states. The bare noun does
// nothing on its own and prints its help, and an unknown word after it is refused; `adopt` is the
// one verb.
func newProjectCommandWith(deps projectAdoptDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Answer the Allow rules a project's .apogee/config.yaml proposes",
		Long: "apogee project manages what the Project config — the committable\n" +
			"<Project root>/.apogee/config.yaml — may grant on this machine. Its `allow:` rules\n" +
			"run nothing until you adopt their exact text; `apogee project adopt` asks about\n" +
			"each proposed one from a terminal, as a session's adoption pane does.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(projectAdoptCommand(deps))
	return cmd
}

// projectAdoptCommand builds `apogee project adopt`: every proposed rule of the workspace's Project
// config, asked about in file order, each answer recorded as it is given.
func projectAdoptCommand(deps projectAdoptDeps) *cobra.Command {
	var opts config.Options
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Adopt or reject each proposed project Allow rule",
		Long: "apogee project adopt lists the Allow rules the Project config proposes — the ones\n" +
			"nobody on this machine has answered yet — and asks about each: y adopts it, so it\n" +
			"answers an ordinary gate from then on; r rejects it, so it is not asked about\n" +
			"again; n, or Enter, leaves it proposed. Answers are kept under\n" +
			"~/.apogee/workspaces, never in the repository, and a running session applies\n" +
			"them without a restart. It asks only on a terminal.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return adoptProjectRules(cmd, deps, &opts)
		},
	}
	addMCPConfigFlag(cmd, &opts)
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "",
		"workspace whose Project root's rules to answer (default: current directory)")
	return cmd
}

// projectAdoptTarget is what one `apogee project adopt` asks about: the Project config's path, the
// rules it proposes, and the record their answers go into.
type projectAdoptTarget struct {
	path     string
	proposed []config.AllowRule
	store    *adoption.Store
}

// adoptProjectRules is the verb: resolve the config the way a session does, report a Project root
// with nothing to ask, refuse a stdin that is not a terminal, and otherwise ask.
func adoptProjectRules(cmd *cobra.Command, deps projectAdoptDeps, opts *config.Options) error {
	deps = deps.withDefaults()
	out := cmd.OutOrStdout()
	target, err := resolveProjectAdoption(cmd, opts)
	if err != nil {
		return err
	}
	if len(target.proposed) == 0 {
		_, _ = fmt.Fprintf(out, "%s proposes no rule that is waiting on an answer.\n", target.path)
		return nil
	}
	if !deps.isTerminal() {
		return fmt.Errorf("apogee project adopt: %s proposes %s, but stdin is not a terminal to ask on — "+
			"run `apogee project adopt` from a terminal", target.path, countSummaryOf(len(target.proposed)))
	}
	deps.terminal.out = out
	return askProjectRules(cmd.Context(), deps.terminal, target)
}

// resolveProjectAdoption reads the config files the way a session does — flags over the
// environment over the files — and returns the workspace's Project config with the rules it
// proposes. A run with no Project config, and a Project config that is the global file itself, are
// refused: there is nothing a project proposes there.
func resolveProjectAdoption(cmd *cobra.Command, opts *config.Options) (projectAdoptTarget, error) {
	notify := func(msg string) { cmd.PrintErrln(msg) }
	err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, notify)
	// A config that names no server yet still resolves every rule; only a session needs a server.
	var undetermined *config.StartupUndetermined
	if err != nil && !errors.As(err, &undetermined) {
		return projectAdoptTarget{}, err
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return projectAdoptTarget{}, err
	}
	path := projectConfigWatchPath(*opts, roots.project)
	if path == "" {
		return projectAdoptTarget{}, fmt.Errorf(
			"apogee project adopt: %s has no Project config to answer — no .apogee/ folder between it "+
				"and the repository's top, or that folder is your apogee home", roots.workspace)
	}
	if len(opts.AllowRules.Proposed) == 0 {
		return projectAdoptTarget{path: path}, nil
	}
	store, err := adoption.New(config.WorkspacesDir(roots.config), roots.project)
	if err != nil {
		return projectAdoptTarget{}, err
	}
	return projectAdoptTarget{path: path, proposed: opts.AllowRules.Proposed, store: store}, nil
}

// projectAnswer is one reply to a rule's question.
type projectAnswer int

const (
	// answerNotNow leaves the rule proposed: inert, and asked about again.
	answerNotNow projectAnswer = iota
	// answerAdopt adopts the rule's exact text.
	answerAdopt
	// answerReject rejects it, so it is never asked about again while its text stays the same.
	answerReject
)

// parseProjectAnswer reads a reply: `y`/`yes` adopts, `r`/`reject` rejects, and anything else — `n`,
// an empty line above all — is the question's default, not now.
func parseProjectAnswer(reply string) projectAnswer {
	switch strings.ToLower(strings.TrimSpace(reply)) {
	case "y", "yes":
		return answerAdopt
	case "r", "reject":
		return answerReject
	}
	return answerNotNow
}

// askProjectRules asks about each proposed rule in file order and records each answer before the
// next question, so a run ended part-way keeps what was already answered. An unreadable reply ends
// the questions there, leaving the rest proposed.
func askProjectRules(ctx context.Context, terminal loginTerminal, target projectAdoptTarget) error {
	out := terminal.out
	_, _ = fmt.Fprintf(out, "%s proposes %s:\n", target.path, countSummaryOf(len(target.proposed)))
	var adopted, rejected int
	for _, rule := range target.proposed {
		_, _ = fmt.Fprintf(out, "  %s `%s` — adopt? [y]es / [n]ot now / [r]eject: ", rule.Kind, rule.Text)
		reply, err := readAnswer(ctx, terminal)
		if err != nil {
			_, _ = fmt.Fprintln(out)
			reportProjectAnswers(out, adopted, rejected, len(target.proposed))
			return fmt.Errorf("apogee project adopt: no answer was read, so the rest stay proposed: %w", err)
		}
		entry := adoption.Entry{Kind: string(rule.Kind), Text: rule.Text}
		switch parseProjectAnswer(reply) {
		case answerAdopt:
			if err := target.store.Adopt(entry); err != nil {
				return err
			}
			adopted++
		case answerReject:
			if err := target.store.Reject(entry); err != nil {
				return err
			}
			rejected++
		}
	}
	reportProjectAnswers(out, adopted, rejected, len(target.proposed))
	return nil
}

// reportProjectAnswers closes the questions with what they did; every rule not adopted or rejected
// — answered "not now", or never reached — is still proposed.
func reportProjectAnswers(out io.Writer, adopted, rejected, asked int) {
	_, _ = fmt.Fprintf(out, "Adopted %d, rejected %d, left %d proposed.\n", adopted, rejected, asked-adopted-rejected)
}

// countSummaryOf is "1 rule" or "N rules".
func countSummaryOf(n int) string {
	if n == 1 {
		return "1 rule"
	}
	return fmt.Sprintf("%d rules", n)
}
