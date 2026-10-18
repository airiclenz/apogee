package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/tui"
)

// ----------------------------------------------------------------------------
// `apogee project adopt` (ADR 0096 §4, ADR 0104 §12)
// ----------------------------------------------------------------------------
//
// The terminal twin of the session's adoption pane: the Project config's proposed entries — its
// Allow rules, MCP servers and reactions — one question each, answered from a shell — after a pull,
// before a session, or beside a running one, which picks the answer up live through its
// adoption-store watcher. A server's or reaction's question prints the pane's body whole, so the
// human rules on the bytes that will run. Nothing here grants anything the file does not already
// state: an answer pins the exact text the file holds now, as the pane's does, a yes is re-checked
// against the file the moment it is given — an entry edited out while the question waited is
// refused, not adopted — and a teammate's later edit to that text is proposed afresh.

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
		Short: "Answer what a project's .apogee/config.yaml proposes",
		Long: "apogee project manages what the Project config — the committable\n" +
			"<Project root>/.apogee/config.yaml — may grant on this machine. Its `allow:` rules,\n" +
			"`mcp-servers:` entries and `reactions:` entries run nothing until you adopt their\n" +
			"exact text; `apogee project adopt` asks about each proposed one from a terminal,\n" +
			"as a session's adoption pane does.",
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

// projectAdoptCommand builds `apogee project adopt`: every proposed entry of the workspace's Project
// config, asked about in the adoption pane's order, each answer recorded as it is given.
func projectAdoptCommand(deps projectAdoptDeps) *cobra.Command {
	var opts config.Options
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Adopt or reject each proposed project entry",
		Long: "apogee project adopt lists the Allow rules, MCP servers and reactions the Project\n" +
			"config proposes — the ones nobody on this machine has answered yet — and asks about\n" +
			"each, printing what a server or reaction would run: y adopts it, so it applies\n" +
			"from then on; r rejects it, so it is not asked about again until it changes; n, or\n" +
			"Enter, leaves it proposed. Answers are kept under\n" +
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
		"workspace whose Project root's entries to answer (default: current directory)")
	return cmd
}

// projectAdoptTarget is what one `apogee project adopt` asks about: the Project config's path, the
// entries it proposes (proposalsOf's order — the Allow rules, then the servers and reactions), the
// record their answers go into, and the Project root and global config path a yes re-reads the
// file through (projectRulesHeld).
type projectAdoptTarget struct {
	path        string
	proposed    []apogee.Proposal
	store       *adoption.Store
	projectRoot string
	globalPath  string
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
			"run `apogee project adopt` from a terminal", target.path, countSummaryOf(target.proposed))
	}
	deps.terminal.out = out
	return askProjectRules(cmd.Context(), deps.terminal, target)
}

// resolveProjectAdoption reads the config files the way a session does — flags over the
// environment over the files — and returns the workspace's Project config with every entry it
// proposes: its Allow rules, MCP servers and reactions. A run with no Project config, and a Project config that is the global file itself, are
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
	proposed := proposalsOf(*opts)
	if len(proposed) == 0 {
		return projectAdoptTarget{path: path}, nil
	}
	store, err := adoption.New(config.WorkspacesDir(roots.config), roots.project)
	if err != nil {
		return projectAdoptTarget{}, err
	}
	return projectAdoptTarget{
		path:        path,
		proposed:    proposed,
		store:       store,
		projectRoot: roots.project,
		globalPath:  config.FilePath(roots.config),
	}, nil
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

// askProjectRules asks about each proposed entry in turn and records each answer before the next
// question, so a run ended part-way keeps what was already answered. A yes re-reads the Project
// config first: an entry it no longer holds as offered is refused by name, nothing is recorded for
// it, and the questions go on. An unreadable reply ends the questions there, leaving the rest
// proposed.
func askProjectRules(ctx context.Context, terminal loginTerminal, target projectAdoptTarget) error {
	out := terminal.out
	_, _ = fmt.Fprintf(out, "%s proposes %s:\n", target.path, countSummaryOf(target.proposed))
	var tally projectAnswerTally
	for _, proposal := range target.proposed {
		entry, err := proposalEntry(proposal)
		if err != nil {
			return err
		}
		printProjectQuestion(out, proposal)
		reply, err := readAnswer(ctx, terminal)
		if err != nil {
			_, _ = fmt.Fprintln(out)
			tally.report(out, len(target.proposed))
			return fmt.Errorf("apogee project adopt: no answer was read, so the rest stay proposed: %w", err)
		}
		switch parseProjectAnswer(reply) {
		case answerAdopt:
			if err := adoptHeldRule(out, target, proposal, entry, &tally); err != nil {
				return err
			}
		case answerReject:
			if err := target.store.Reject(entry); err != nil {
				return err
			}
			tally.rejected++
		}
	}
	tally.report(out, len(target.proposed))
	return nil
}

// projectQuestionPrompt ends every question with the three answers it takes.
const projectQuestionPrompt = "adopt? [y]es / [n]ot now / [r]eject: "

// projectQuestionIndent is how far a question's body hangs under its title, so the body's own
// column zero — where the pane's labels start and the file's text never does — stays a column of
// its own.
const projectQuestionIndent = "    "

// printProjectQuestion asks about proposal on out. An Allow rule is one line, its kind and text; a
// server or reaction is the adoption pane's title, its body printed whole under it
// (tui.AdoptionQuestion), and the answers on a line of their own.
func printProjectQuestion(out io.Writer, proposal apogee.Proposal) {
	title, body := tui.AdoptionQuestion(proposal)
	if body == nil {
		_, _ = fmt.Fprintf(out, "  %s `%s` — %s", proposal.Kind, proposal.Text, projectQuestionPrompt)
		return
	}
	_, _ = fmt.Fprintf(out, "  %s\n", title)
	for _, line := range body {
		_, _ = fmt.Fprintln(out, strings.TrimRight(projectQuestionIndent+line, " "))
	}
	_, _ = fmt.Fprintf(out, "  %s", projectQuestionPrompt)
}

// adoptHeldRule is a yes to proposal, whose adoption entry is entry: adopted when the Project config
// still holds it as offered, and otherwise refused on out by name with nothing recorded.
//
// Errors: a Project config that cannot be re-read, and the store's.
func adoptHeldRule(out io.Writer, target projectAdoptTarget, proposal apogee.Proposal, entry adoption.Entry,
	tally *projectAnswerTally) error {
	held, err := projectRulesHeld(target.globalPath, target.projectRoot)
	if err != nil {
		return err
	}
	if !held[adoption.Fingerprint(entry)] {
		_, _ = fmt.Fprintf(out, "  %s no longer holds %s, so it was not adopted.\n", target.path, heldName(proposal))
		tally.gone++
		return nil
	}
	if err := target.store.Adopt(entry); err != nil {
		return err
	}
	tally.adopted++
	return nil
}

// heldName names proposal in the refusal of a yes the file no longer holds: an Allow rule by its
// text, a server or reaction as the pane names it, as it was offered.
func heldName(proposal apogee.Proposal) string {
	if proposal.Handle == "" {
		return "`" + proposal.Text + "`"
	}
	return tui.AdoptionEntryName(proposal) + " as it was offered"
}

// projectAnswerTally counts what the answers did: entries adopted, entries rejected, and entries a
// yes found gone from the Project config.
type projectAnswerTally struct {
	adopted, rejected, gone int
}

// report closes the questions with what they did; every entry of the asked not adopted, rejected or
// gone — answered "not now", or never reached — is still proposed.
func (t projectAnswerTally) report(out io.Writer, asked int) {
	_, _ = fmt.Fprintf(out, "Adopted %d, rejected %d, left %d proposed", t.adopted, t.rejected,
		asked-t.adopted-t.rejected-t.gone)
	if t.gone > 0 {
		_, _ = fmt.Fprintf(out, ", %d no longer in the file", t.gone)
	}
	_, _ = fmt.Fprintln(out, ".")
}

// countSummaryOf counts proposed: "1 rule" or "N rules" when every one is an Allow rule, and "1
// entry" or "N entries" once a server or reaction is among them.
func countSummaryOf(proposed []apogee.Proposal) string {
	one, many := "rule", "rules"
	if slices.ContainsFunc(proposed, func(p apogee.Proposal) bool { return p.Handle != "" }) {
		one, many = "entry", "entries"
	}
	if len(proposed) == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", len(proposed), many)
}
