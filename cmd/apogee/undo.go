package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// `apogee undo <session-id> [confirm <generation>]` — the unattended revert verb
// ----------------------------------------------------------------------------
//
// The TUI's `/undo` is offered to a human who is already in the session. An unattended Firing has
// no such human: `apogee headless` and the daemon report what a run CHANGED and then exit, and
// until ADR 0074 made the journal outlive its process there was nothing anyone could do about it.
// This verb is the other end of that persistence — the session's snapshot store is on disk under
// its own id, so a human who reads the report afterwards can put the exchange back from a fresh
// process (ADR 0074, bead apogee-kk0.7).
//
// It is the SAME two steps and the same listing as `/undo`: a preview that discloses every
// recorded path with what the revert would do to it, then a `confirm` that executes exactly what
// was read. The preview's closing line carries the journal's generation stamp, and the confirm
// quotes it back (ADR 0051 D7, ADR 0074 D8): a journal that moved between the two — another
// exchange recorded, an earlier confirm applied — refuses the stale stamp and re-previews rather
// than reverting a step the human never read, exactly as `/undo confirm` does through
// Agent.UndoRevert. internal/undo composes the rows so the three surfaces cannot drift; only the
// verb — the head of the first line and the line that applies the step — belongs here.
//
// The verb HOLDS the session for its whole run — the same live-instance hold (session.Store.Hold)
// a --resume start takes — because the store it rewrites is the one a live apogee running that
// session would persist over on its next exchange. A held session is refused with the hold's own
// sentence; the verb's own hold is released when it returns. Headless and daemon Firings hold
// their record the same way for as long as run.Once runs, so an undo of a Firing still in flight
// is refused with that sentence too.
//
// What it deliberately does NOT take is a workspace. The store images ONE tree, its index says
// which, and every recorded path is spelled against it: a `--workspace` that disagreed would
// restore one project's files over another's. So the flag is not registered at all — Cobra refuses
// it as unknown — and snapshot.OpenStored reads the workspace out of the index instead; the index's
// name and layout are internal/snapshot's alone.

// undoConfirmArg is the second positional argument that turns the preview into the revert. A word
// rather than a flag, because it is the human's authorization of the listing they just read and
// reads that way in a shell history: `apogee undo <id> confirm <generation>`. The generation that
// follows it is the stamp the preview printed — required, so a confirm always names the step it
// was read from; a bare `confirm` is sent back to the preview.
const undoConfirmArg = "confirm"

// maxUndoIDLen bounds the id this verb will compose a path from, matching the session store's own
// limit (internal/session): an id longer than this can name no record and no store.
const maxUndoIDLen = 200

// newUndoCommand builds `apogee undo`. It holds no engine, contacts no server and reads no config
// beyond the apogee home: everything it needs is the session's own store, which the Firing that
// wrote it left behind.
func newUndoCommand() *cobra.Command {
	var configDir string

	cmd := &cobra.Command{
		Use:   "undo <session-id> [confirm <generation>]",
		Short: "Put back the files one saved session's last exchange changed",
		Long: "apogee undo reverts the last exchange of a saved session — the revert an\n" +
			"unattended run has nobody to offer. `apogee headless` and the daemon end their\n" +
			"written-files report with the exact command, and it works from any directory\n" +
			"and long after the run: the session's snapshots live under ~/.apogee/snapshots.\n\n" +
			"Two steps, like /undo in the session. `apogee undo <session-id>` PREVIEWS: it\n" +
			"lists every recorded path with what the revert would do to it — restore, delete,\n" +
			"or skip with the reason — and closes with the exact confirm line, which carries\n" +
			"the journal's generation stamp. `apogee undo <session-id> confirm <generation>`\n" +
			"applies exactly that step and reports what it did; a stamp the journal has moved\n" +
			"past is refused and the preview is printed afresh, and a bare `confirm` is sent\n" +
			"back to the preview. A file that no longer holds what the agent left is skipped\n" +
			"rather than overwritten, so your own edits since the run are safe.\n\n" +
			"Run it again to walk further back: each confirm reverts one more exchange. The\n" +
			"session is held for the verb's run, so one that is open in a live apogee is\n" +
			"refused rather than rewritten under it.\n\n" +
			"The workspace is read from the session's own snapshot index — there is no\n" +
			"--workspace flag, because the recorded paths belong to the tree they were taken\n" +
			"of. A session recorded without snapshots (undo-snapshots off, or no git on the\n" +
			"host when it ran) has nothing to revert here, and says so.",
		Args:          undoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			generation, err := undoGeneration(args)
			if err != nil {
				return err
			}
			return runUndoVerb(cmd, args[0], len(args) > 1, generation, configDir)
		},
	}

	cmd.Flags().StringVar(&configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}

// undoArgs accepts the session id alone, or the id followed by the literal `confirm` and the
// generation the preview printed. A second argument that is anything else is a usage mistake
// rather than a preview: a human who typed `apogee undo <id> yes` meant to revert, and silently
// previewing would leave them believing the files were put back. A `confirm` with no generation
// is sent back to the preview: the stamp is what ties the confirm to the listing that was read,
// and the preview's closing line is where it comes from.
func undoArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.RangeArgs(1, 3)(cmd, args); err != nil {
		return fmt.Errorf("apogee undo: %w (usage: apogee undo <session-id> [confirm <generation>])", err)
	}
	if len(args) > 1 && args[1] != undoConfirmArg {
		return fmt.Errorf("apogee undo: %q is not a second argument this command takes; "+
			"say %q to apply the previewed step", args[1], undoConfirmArg)
	}
	if len(args) == 2 {
		return fmt.Errorf("preview first: apogee undo %s, then run the line it prints", args[0])
	}
	return nil
}

// undoGeneration reads the stamp off a confirm's third argument — the number the preview's closing
// line printed — and is zero for a preview, which quotes none. A third argument that is not such a
// number is the same mistake as a missing one: the human is sent back to the preview.
func undoGeneration(args []string) (uint64, error) {
	if len(args) < 3 {
		return 0, nil
	}
	generation, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("apogee undo: %q is not the generation a preview prints; "+
			"preview first: apogee undo %s, then run the line it prints", args[2], args[0])
	}
	return generation, nil
}

// runUndoVerb previews or applies the top exchange of one saved session's journal; generation is
// the stamp a confirm quotes and is unread on a preview.
//
// The session is held BEFORE the store is opened and for the verb's whole run, so a session that
// is open in a live apogee is refused with the hold's own sentence before anything is read — the
// hold, not the index, is what says whether another process may still persist over this store.
// Past the hold, the order of refusals is snapshot.OpenStored's: the index is read BEFORE the
// store is opened, so an id that names no session answers "nothing to undo" without leaving a
// freshly initialised object database behind under the apogee home. What the hold does leave
// behind is its own `sessions/<id>.lock` — the sessions store's lock file, which only a session
// delete unlinks — the accepted price of holding first.
func runUndoVerb(cmd *cobra.Command, id string, confirm bool, generation uint64, configDir string) error {
	if err := validateUndoID(id); err != nil {
		return err
	}

	// The apogee home on the precedence every other Driver honours — --config over APOGEE_CONFIG
	// over ~/.apogee — and through resolveRoots, never a bare config.ApogeeHome(""), which has no
	// environment fallback at all: a human whose home is set by the variable would be told their
	// session has nothing to undo. The env read is spelled here because this command resolves no
	// config file and so never reaches ApplyConfig, which is where every other command's is done.
	//
	// The workspace half of the roots is unused: this command's workspace comes from the index.
	if configDir == "" {
		configDir = os.Getenv(config.EnvConfig)
	}
	roots, err := resolveRoots(configDir, "")
	if err != nil {
		return err
	}

	release, err := session.NewStore(roots.sessions).Hold(id)
	if err != nil {
		var held *session.HeldError
		if errors.As(err, &held) {
			return held
		}
		return fmt.Errorf("apogee undo: %w", err)
	}
	defer func() { _ = release() }()

	// A session with no index — an id that names none, or a run that recorded nothing — is this
	// verb's "nothing to undo" rather than a failure; an index that is there and wrong IS one.
	journal, reason, err := snapshot.OpenStored(cmd.Context(), roots.config, id)
	switch {
	case errors.Is(err, snapshot.ErrNoIndex):
		return undoNothingToDo(id)
	case err != nil:
		return fmt.Errorf("undo snapshots unavailable: %w", err)
	case reason != "":
		return fmt.Errorf("undo snapshots unavailable: %s", reason)
	}

	if confirm {
		return applyUndoVerb(cmd, journal, id, generation)
	}
	return previewUndoVerb(cmd, journal, id)
}

// previewUndoVerb prints the listing the revert is authorised from, headed by the command the human
// typed and closed by the one that executes it. Nothing is touched.
func previewUndoVerb(cmd *cobra.Command, journal *undo.Journal, id string) error {
	step, ok := journal.Preview()
	if !ok {
		return undoNothingToDo(id)
	}

	printUndoLines(cmd, undoVerbNote("apogee undo "+id, undo.PreviewLines(step),
		undoVerbHint(id, step.Generation)))
	return nil
}

// applyUndoVerb executes the previewed step and reports what it did — the counts, then every path
// it left alone with the reason. generation is the stamp the preview printed: a journal that has
// moved past it is refused with undo.ErrStaleGeneration and the fresh preview is printed in its
// place, so what the human reads next is the step a confirm would now apply — the shape
// Agent.UndoRevert gives `/undo confirm`. An empty journal answers "nothing to undo" BEFORE the
// stamp is compared, as the TUI's confirm does: there is no step for a stamp to be stale against.
// A failure is returned rather than printed: a confirmation that appears to do nothing must not
// be indistinguishable from one that reverted nothing.
func applyUndoVerb(cmd *cobra.Command, journal *undo.Journal, id string, generation uint64) error {
	step, ok := journal.Preview()
	if !ok {
		return undoNothingToDo(id)
	}
	if live := journal.Generation(); live != generation {
		printUndoLines(cmd, append([]string{undoVerbMovedLead},
			undoVerbNote("apogee undo "+id, undo.PreviewLines(step), undoVerbHint(id, step.Generation))...))
		return fmt.Errorf("%w: previewed at generation %d, journal is at %d",
			undo.ErrStaleGeneration, generation, live)
	}

	report, err := journal.Revert()
	switch {
	case errors.Is(err, undo.ErrNothingToUndo):
		return undoNothingToDo(id)
	case err != nil:
		return fmt.Errorf("undo failed: %w", err)
	}

	printUndoLines(cmd, undoVerbNote("undone", undo.ReportLines(report), ""))
	return nil
}

// printUndoLines writes the composed listing to stdout, one line at a time. Stdout, because the
// listing IS this command's product — unlike `apogee headless`, whose stdout contract is the
// model's answer alone and whose narration therefore all goes to stderr.
//
// Escape-stripped to a single line apiece: every path on it traces to a model-chosen tool argument,
// and a listing a human authorises a revert from must read as the bytes on disk say it does.
func printUndoLines(cmd *cobra.Command, lines []string) {
	for _, line := range lines {
		cmd.Println(sanitize.StripEscapesToLine(line))
	}
}

// undoVerbNote heads internal/undo's Driver-neutral listing with the verb and closes it with the
// line that applies it — the TUI's revertNote for a command line. The listing itself is not this
// Driver's to word: `/undo`, `/redo` and this verb show the same rows on purpose (ADR 0074).
func undoVerbNote(verb string, lines []string, hint string) []string {
	noted := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		if i == 0 {
			line = verb + " — " + line
		}
		noted = append(noted, line)
	}
	if hint != "" {
		noted = append(noted, hint)
	}
	return noted
}

// undoVerbMovedLead is the line printed above the re-preview a stale confirmation earns — the TUI's
// undoMovedLead, verbatim: the journal moved since the stamp was read, nothing was undone, and the
// listing under it is the fresh preview, headed and closed exactly as a bare preview is.
const undoVerbMovedLead = "the journal moved since that preview — nothing was undone"

// undoVerbHint closes a preview with the whole grammar that executes it, generation stamp
// included. The id is repeated rather than abbreviated because a human reading a preview in a
// terminal has to be able to run the next command from what is in front of them.
func undoVerbHint(id string, generation uint64) string {
	return "  apogee undo " + id + " " + undoConfirmArg + " " + strconv.FormatUint(generation, 10) +
		" applies this; anything else leaves the files alone"
}

// undoNothingToDo is the answer to a session with no step left: an unknown id, a run that changed
// nothing, and a journal already walked back to its start all reach it. It is an ERROR rather than
// a quiet exit 0, because the caller of this verb asked for a revert and did not get one.
func undoNothingToDo(id string) error {
	return fmt.Errorf("nothing to undo for session %s", id)
}

// validateUndoID refuses an id that is not a single clean path component, before it is joined onto
// the apogee home. It is internal/session's own rule for a record id — non-empty, bounded, not
// dot-prefixed, one component with no separator of either OS, and free of control and
// bidi-formatting characters — restated here because that package keeps it unexported and this
// verb takes its id straight off a command line rather than from a store.
//
// The bidi characters are refused rather than stripped for the same reason they are there: an id
// carrying one names a directory whose displayed spelling is not the one on disk, and every line
// this command prints quotes the id back at the human.
func validateUndoID(id string) error {
	switch {
	case id == "":
		return errors.New("apogee undo: empty session id")
	case len(id) > maxUndoIDLen:
		return fmt.Errorf("apogee undo: session id %q is longer than %d bytes", id, maxUndoIDLen)
	case strings.HasPrefix(id, "."):
		return fmt.Errorf("apogee undo: session id %q must not start with a dot", id)
	case id != filepath.Base(id) || strings.ContainsAny(id, `/\`):
		return fmt.Errorf("apogee undo: session id %q must be a single path component", id)
	case strings.ContainsFunc(id, func(r rune) bool {
		return r < 0x20 || r == 0x7f || sanitize.BidiControl(r)
	}):
		return errors.New("apogee undo: the session id contains a control or bidi-formatting character")
	}
	return nil
}
