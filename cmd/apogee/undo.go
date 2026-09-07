package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// `apogee undo <session-id> [confirm]` — the unattended revert verb
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
// was read. internal/undo composes the rows so the three surfaces cannot drift; only the verb —
// the head of the first line and the line that applies the step — belongs here.
//
// What it deliberately does NOT take is a workspace. The store images ONE tree, journal.json says
// which, and every recorded path is spelled against it: a `--workspace` that disagreed would
// restore one project's files over another's. So the flag is not registered at all — Cobra refuses
// it as unknown — and the workspace is read out of the index instead.

// undoConfirmArg is the second positional argument that turns the preview into the revert. A word
// rather than a flag, because it is the human's authorization of the listing they just read and
// reads that way in a shell history: `apogee undo <id> confirm`.
const undoConfirmArg = "confirm"

// undoIndexFile is journal.json, the index that sits beside a session's objects. internal/snapshot
// owns the name and keeps it unexported; this verb is the one caller outside that package that has
// to open the file itself, because the workspace it needs to OPEN the journal is recorded inside
// it. Spelled once here, and pinned against snapshot.Dir by TestUndoVerbReadsTheWorkspaceFromTheIndex.
const undoIndexFile = "journal.json"

// maxUndoIDLen bounds the id this verb will compose a path from, matching the session store's own
// limit (internal/session): an id longer than this can name no record and no store.
const maxUndoIDLen = 200

// newUndoCommand builds `apogee undo`. It holds no engine, contacts no server and reads no config
// beyond the apogee home: everything it needs is the session's own store, which the Firing that
// wrote it left behind.
func newUndoCommand() *cobra.Command {
	var configDir string

	cmd := &cobra.Command{
		Use:   "undo <session-id> [confirm]",
		Short: "Put back the files one saved session's last exchange changed",
		Long: "apogee undo reverts the last exchange of a saved session — the revert an\n" +
			"unattended run has nobody to offer. `apogee headless` and the daemon end their\n" +
			"written-files report with the exact command, and it works from any directory\n" +
			"and long after the run: the session's snapshots live under ~/.apogee/snapshots.\n\n" +
			"Two steps, like /undo in the session. `apogee undo <session-id>` PREVIEWS: it\n" +
			"lists every recorded path with what the revert would do to it — restore, delete,\n" +
			"or skip with the reason. `apogee undo <session-id> confirm` applies exactly that\n" +
			"step and reports what it did. A file that no longer holds what the agent left is\n" +
			"skipped rather than overwritten, so your own edits since the run are safe.\n\n" +
			"Run it again to walk further back: each confirm reverts one more exchange.\n\n" +
			"The workspace is read from the session's own snapshot index — there is no\n" +
			"--workspace flag, because the recorded paths belong to the tree they were taken\n" +
			"of. A session recorded without snapshots (undo-snapshots off, or no git on the\n" +
			"host when it ran) has nothing to revert here, and says so.",
		Args:          undoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUndoVerb(cmd, args[0], len(args) > 1, configDir)
		},
	}

	cmd.Flags().StringVar(&configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}

// undoArgs accepts the session id alone or the id followed by the literal `confirm`. A second
// argument that is anything else is a usage mistake rather than a preview: a human who typed
// `apogee undo <id> yes` meant to revert, and silently previewing would leave them believing the
// files were put back.
func undoArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
		return fmt.Errorf("apogee undo: %w (usage: apogee undo <session-id> [confirm])", err)
	}
	if len(args) > 1 && args[1] != undoConfirmArg {
		return fmt.Errorf("apogee undo: %q is not a second argument this command takes; "+
			"say %q to apply the previewed step", args[1], undoConfirmArg)
	}
	return nil
}

// runUndoVerb previews or applies the top exchange of one saved session's journal.
//
// The order of its refusals is the point: the index is read BEFORE the store is opened, so an id
// that names no session answers "nothing to undo" without leaving a freshly initialised object
// database behind under the apogee home.
func runUndoVerb(cmd *cobra.Command, id string, confirm bool, configDir string) error {
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

	workspace, err := undoIndexWorkspace(snapshot.Dir(roots.config, id))
	switch {
	case err != nil:
		return fmt.Errorf("undo snapshots unavailable: %w", err)
	case workspace == "":
		return undoNothingToDo(id)
	}

	// enabled: true unconditionally. `undo-snapshots:` governs whether a RUN images its exchanges,
	// not whether a store that already exists may be read back — refusing to open one because the
	// key has since been turned off would strand the very writes it was on for.
	journal, reason, err := snapshot.OpenJournal(cmd.Context(), roots.config, id, workspace, true)
	switch {
	case err != nil:
		return fmt.Errorf("undo snapshots unavailable: %w", err)
	case reason != "":
		return fmt.Errorf("undo snapshots unavailable: %s", reason)
	}

	if confirm {
		return applyUndoVerb(cmd, journal, id)
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

	printUndoLines(cmd, undoVerbNote("apogee undo "+id, undo.PreviewLines(step), undoVerbHint(id)))
	return nil
}

// applyUndoVerb executes the previewed step and reports what it did — the counts, then every path
// it left alone with the reason. A failure is returned rather than printed: a confirmation that
// appears to do nothing must not be indistinguishable from one that reverted nothing.
func applyUndoVerb(cmd *cobra.Command, journal *undo.Journal, id string) error {
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

// undoVerbHint closes a preview with the whole grammar that executes it. The id is repeated rather
// than abbreviated because a human reading a preview in a terminal has to be able to run the next
// command from what is in front of them.
func undoVerbHint(id string) string {
	return "  apogee undo " + id + " " + undoConfirmArg +
		" applies this; anything else leaves the files alone"
}

// undoNothingToDo is the answer to a session with no step left: an unknown id, a run that changed
// nothing, and a journal already walked back to its start all reach it. It is an ERROR rather than
// a quiet exit 0, because the caller of this verb asked for a revert and did not get one.
func undoNothingToDo(id string) error {
	return fmt.Errorf("nothing to undo for session %s", id)
}

// undoIndexWorkspace reads the workspace one session's snapshots were taken of out of its
// journal.json. It answers "" with no error when there is no index at all — an id that names no
// session, or a run that recorded nothing — which is this verb's "nothing to undo" rather than a
// failure; an index that exists but cannot be read or decoded IS a failure, because a store is
// there and something is wrong with it.
func undoIndexWorkspace(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}

	data, err := os.ReadFile(filepath.Join(dir, undoIndexFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the session's undo index: %w", err)
	}

	var index undo.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return "", fmt.Errorf("decode the session's undo index: %w", err)
	}
	if index.Workspace == "" {
		return "", errors.New("the session's undo index names no workspace")
	}
	return index.Workspace, nil
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
