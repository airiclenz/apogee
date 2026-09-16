package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/session"
)

// newCheckCommand judges a take against the storyboard's expects: one row per expect (or per
// beat without one), then the take-level stage row, and exit 1 on any FAIL. The take's video
// is never read — a take is judged by what its session recorded and what it left in the stage.
func newCheckCommand() *cobra.Command {
	var stage string
	cmd := &cobra.Command{
		Use:   "check <storyboard.yaml> <session.json> [--stage <dir>]",
		Short: "Judge a take's saved session against the storyboard's expects",
		Long: "check resolves every anchor of the storyboard against the take's saved session and\n" +
			"evaluates the expects: contains on the anchored entry's text, tool label or stat,\n" +
			"before/after on the entries' order, and expect.stage against the stage repo named by\n" +
			"--stage (reported SKIP when it is not given). One row per expect; exit 1 on any FAIL.",
		Args: cobra.ExactArgs(2),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			entries, err := loadEntries(args[1])
			if err != nil {
				return err
			}
			rows, err := checkTake(cmd.Context(), board, entries, stage)
			if err != nil {
				return err
			}
			if err := writeCheckTable(cmd.OutOrStdout(), rows); err != nil {
				return err
			}
			if failed := countFailed(rows); failed > 0 {
				return fmt.Errorf("%d expect(s) failed", failed)
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&stage, "stage", "",
		"the stage repo expect.stage is judged against; without it the stage row reads SKIP")
	return cmd
}

// verdict is what one row of the check table reports.
type verdict string

// The three verdicts. SKIP is reserved for an expect the command line gave no way to judge; it
// never fails the take.
const (
	verdictPass verdict = "PASS"
	verdictFail verdict = "FAIL"
	verdictSkip verdict = "SKIP"
)

// checkRow is one line of the check table: the subject (`beat N` or `stage`), its verdict and
// the detail that says what was judged.
type checkRow struct {
	Subject string
	Verdict verdict
	Detail  string
}

// stageSubject is the subject of the take-level stage row.
const stageSubject = "stage"

// noVideo is the FirstPainter check hands the resolver: the take's video is never read, so its
// fixed points are placeholders here — a check only ever reads the entries the anchors select.
type noVideo struct{}

func (noVideo) FirstPaint(context.Context) (time.Duration, error) { return 0, nil }

// checkTake resolves the storyboard against the session and judges every expect, in storyboard
// order, with the stage row last when the storyboard declares expect.stage. An anchor that
// resolves to nothing is an error naming its beat, the resolver's own: without every anchor
// resolved, no ordering can be judged. The stage row is SKIP when stage is empty and FAIL when
// git cannot judge the directory it names.
func checkTake(ctx context.Context, board *Storyboard, entries []session.Entry, stage string) ([]checkRow, error) {
	times, err := resolveBeats(ctx, board, entries, noVideo{}, 0)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]BeatTime, len(times))
	for _, beat := range times {
		byID[beat.ID] = beat
	}
	var rows []checkRow
	for _, beat := range board.Beats {
		rows = append(rows, beat.judge(entries, byID)...)
	}
	if board.Expect.Stage != "" {
		row, err := judgeStage(ctx, stage)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// judge evaluates the beat's expects, one row each; a beat without one gets a single PASS row
// so the table still accounts for it.
func (b Beat) judge(entries []session.Entry, times map[int]BeatTime) []checkRow {
	subject := fmt.Sprintf("beat %d", b.ID)
	if len(b.Expect) == 0 {
		return []checkRow{{Subject: subject, Verdict: verdictPass, Detail: "no expect"}}
	}
	rows := make([]checkRow, 0, len(b.Expect))
	for _, expect := range b.Expect {
		row := checkRow{Subject: subject, Verdict: verdictPass}
		row.Verdict, row.Detail = expect.judge(b, entries, times)
		rows = append(rows, row)
	}
	return rows
}

// judge evaluates one expect: the judged entry is the expect's own selector when it has one,
// the beat's anchor entry otherwise; then every clause set — contains, before, after — must
// hold. The detail spells the entry and the first clause that failed, or every clause that
// passed.
func (e Expect) judge(beat Beat, entries []session.Entry, times map[int]BeatTime) (verdict, string) {
	judged, err := e.judgedEntry(beat, entries, times)
	if err != nil {
		return verdictFail, err.Error()
	}
	var passed []string
	for _, clause := range e.clauses() {
		detail, ok := clause(judged, times)
		if !ok {
			return verdictFail, describeEntry(judged) + ": " + detail
		}
		passed = append(passed, detail)
	}
	return verdictPass, describeEntry(judged) + ": " + strings.Join(passed, ", ")
}

// judgedEntry selects the entry an expect is judged on. The storyboard's validation guarantees
// an expect without its own selector sits on a session-anchored beat; the noEntry guard is for
// a Storyboard built without Load.
func (e Expect) judgedEntry(beat Beat, entries []session.Entry, times map[int]BeatTime) (located, error) {
	if e.Entry != nil {
		return findEntry(entries, *e.Entry)
	}
	index := times[beat.ID].Index
	if index == noEntry {
		return located{}, fmt.Errorf("%s anchors no session entry to judge", beat.Anchor)
	}
	return located{Index: index, Entry: entries[index]}, nil
}

// clause judges one assertion of an expect against the judged entry, returning the detail to
// print and whether it held.
type clause func(judged located, times map[int]BeatTime) (string, bool)

// clauses lists the assertions the expect sets, in the order the schema names them.
func (e Expect) clauses() []clause {
	var clauses []clause
	if e.Contains != "" {
		clauses = append(clauses, containsClause(e.Contains))
	}
	if e.Before != 0 {
		clauses = append(clauses, orderClause("before", e.Before, func(judged, other int) bool { return judged < other }))
	}
	if e.After != 0 {
		clauses = append(clauses, orderClause("after", e.After, func(judged, other int) bool { return judged > other }))
	}
	return clauses
}

// containsClause holds when the entry's text, tool label or tool stat contains the substring.
func containsClause(want string) clause {
	return func(judged located, _ map[int]BeatTime) (string, bool) {
		if entryContains(judged.Entry, want) {
			return fmt.Sprintf("contains %q", want), true
		}
		return fmt.Sprintf("does not contain %q", want), false
	}
}

// orderClause holds when the judged entry's list position relates to the other beat's anchor
// entry as holds says. A beat with no session entry cannot be ordered against.
func orderClause(name string, other int, holds func(judged, other int) bool) clause {
	return func(judged located, times map[int]BeatTime) (string, bool) {
		otherIndex := times[other].Index
		if otherIndex == noEntry {
			return fmt.Sprintf("%s beat %d: that beat anchors no session entry to order against", name, other), false
		}
		if holds(judged.Index, otherIndex) {
			return fmt.Sprintf("%s beat %d (entry %d)", name, other, otherIndex), true
		}
		return fmt.Sprintf("is not %s beat %d (entry %d)", name, other, otherIndex), false
	}
}

// entryContains reports whether the entry's text, tool label or tool stat carries the substring.
func entryContains(entry session.Entry, want string) bool {
	if strings.Contains(entry.Text, want) {
		return true
	}
	return entry.Tool != nil &&
		(strings.Contains(entry.Tool.Label, want) || strings.Contains(entry.Tool.Stat, want))
}

// describeEntry spells a located entry for a table row: its index and kind, then the tool card's
// label, target and stat, or the head of its text.
func describeEntry(judged located) string {
	entry := judged.Entry
	parts := []string{fmt.Sprintf("entry %d", judged.Index), entry.Kind}
	if entry.Tool != nil {
		for _, field := range []string{entry.Tool.Label, entry.Tool.Target, entry.Tool.Stat} {
			if field != "" {
				parts = append(parts, field)
			}
		}
	} else if entry.Text != "" {
		parts = append(parts, fmt.Sprintf("%q", headOf(entry.Text)))
	}
	return strings.Join(parts, " ")
}

// textHead is how much of an entry's text a table row shows.
const textHead = 40

// headOf returns the text's first line, cut to textHead runes with an ellipsis.
func headOf(text string) string {
	if line, _, found := strings.Cut(text, "\n"); found {
		text = line + "…"
	}
	if runes := []rune(text); len(runes) > textHead {
		return string(runes[:textHead]) + "…"
	}
	return text
}

// judgeStage evaluates expect.stage: `dirty` holds when `git status --porcelain` in the stage
// repo prints anything. Without a stage directory the row is SKIP. A stage git refuses to
// judge — a directory that is not a work tree, or none at all — is a FAIL row naming git's
// reason, so the beat rows before it still print; only a missing git binary is an error, since
// the rig can then judge nothing.
func judgeStage(ctx context.Context, stage string) (checkRow, error) {
	row := checkRow{Subject: stageSubject}
	if stage == "" {
		row.Verdict, row.Detail = verdictSkip, "stage: dirty not judged — no --stage given"
		return row, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", stage, "status", "--porcelain")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return row, fmt.Errorf("stage: git -C %s status --porcelain: %w", stage, err)
	}
	if err != nil {
		reason := err.Error()
		if line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); line != "" {
			reason = line
		}
		row.Verdict, row.Detail = verdictFail, fmt.Sprintf("stage: %s is not a git work tree (git status: %s)", stage, reason)
		return row, nil
	}
	changed := countLines(out)
	if changed == 0 {
		row.Verdict, row.Detail = verdictFail, "stage: dirty — "+stage+" is clean"
		return row, nil
	}
	row.Verdict, row.Detail = verdictPass, fmt.Sprintf("stage: dirty — %d changed path(s) in %s", changed, stage)
	return row, nil
}

// countLines counts the non-blank lines of a command's output: one per changed path for
// `git status --porcelain`.
func countLines(out []byte) int {
	lines := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	return lines
}

// writeCheckTable prints the rows as `subject | verdict | detail`, one per line.
func writeCheckTable(w io.Writer, rows []checkRow) error {
	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "%-6s | %s | %s\n", row.Subject, row.Verdict, row.Detail); err != nil {
			return err
		}
	}
	return nil
}

// countFailed counts the FAIL rows.
func countFailed(rows []checkRow) int {
	failed := 0
	for _, row := range rows {
		if row.Verdict == verdictFail {
			failed++
		}
	}
	return failed
}
