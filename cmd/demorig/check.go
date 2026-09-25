package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/session"
)

// newCheckCommand judges a take against the storyboard's expects: one row per expect (or per
// beat without one), then the take-level stage row, and exit 1 on any FAIL.
func newCheckCommand() *cobra.Command {
	var stage string
	cmd := &cobra.Command{
		Use:   "check <storyboard.yaml> [<take>] [--stage <dir>]",
		Short: "Judge a take against the storyboard's expects",
		Long: "check judges every expect of the storyboard against a take: an entry expect on the\n" +
			"session the take saved (contains on the entry's text, tool label or stat, before/after\n" +
			"on the order of the entries the beats' first entry expects locate), a seen expect on the\n" +
			"screens the take recorded inside its beat, and expect.stage against the stage repo named\n" +
			"by --stage (reported SKIP when it is not given). One row per expect; exit 1 on any FAIL.\n" +
			"The take defaults to <work>/<clip>.take, the work dir following $" + workDirEnv + ".",
		Args: cobra.RangeArgs(1, 2),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			path, err := takeArg(args[1:], board.Clip)
			if err != nil {
				return err
			}
			take, err := LoadTake(path)
			if err != nil {
				return err
			}
			rows, err := checkTake(cmd.Context(), board, take, stage)
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

// takeEvidence is what a take offers its expects: the transcript entries of the session it
// saved, the entry each beat's first entry expect locates (noEntry when it locates none, or the
// beat has no entry expect), and the snapshots on screen during each beat.
type takeEvidence struct {
	entries []session.Entry
	anchors map[int]int
	screens map[int][]Snapshot
}

// checkTake judges every expect of the storyboard against the take, in storyboard order, with
// the stage row last when the storyboard declares expect.stage. The session is read only when
// some expect judges an entry, and the beats are located in the take only when some expect
// judges a screen; a take that cannot serve what its expects need is an error. The stage row is
// SKIP when stage is empty and FAIL when git cannot judge the directory it names.
func checkTake(ctx context.Context, board *Storyboard, take *Take, stage string) ([]checkRow, error) {
	evidence, err := gatherEvidence(board, take)
	if err != nil {
		return nil, err
	}
	var rows []checkRow
	for _, beat := range board.Beats {
		rows = append(rows, beat.judge(evidence)...)
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

// gatherEvidence reads what the storyboard's expects need out of the take.
func gatherEvidence(board *Storyboard, take *Take) (takeEvidence, error) {
	evidence := takeEvidence{anchors: make(map[int]int, len(board.Beats))}
	judgesEntries, judgesScreens := false, false
	for _, beat := range board.Beats {
		for _, expect := range beat.Expect {
			judgesEntries = judgesEntries || expect.Entry != nil
			judgesScreens = judgesScreens || expect.Seen != ""
		}
	}
	if judgesEntries {
		if take.Session == "" {
			return takeEvidence{}, errors.New("the take names no saved session, so its entry expects cannot be judged")
		}
		entries, err := loadEntries(take.Session)
		if err != nil {
			return takeEvidence{}, err
		}
		evidence.entries = entries
	}
	for _, beat := range board.Beats {
		evidence.anchors[beat.ID] = beat.anchorEntry(evidence.entries)
	}
	if judgesScreens {
		spans, err := SpansFrom(board, take)
		if err != nil {
			return takeEvidence{}, err
		}
		evidence.screens = beatScreens(take.Snapshots, spans)
	}
	return evidence, nil
}

// anchorEntry is the index of the entry the beat's first entry expect locates — what another
// beat's before/after orders against — or noEntry.
func (b Beat) anchorEntry(entries []session.Entry) int {
	for _, expect := range b.Expect {
		if expect.Entry == nil {
			continue
		}
		found, err := findEntry(entries, *expect.Entry)
		if err != nil {
			return noEntry
		}
		return found.Index
	}
	return noEntry
}

// beatScreens groups the snapshots by the beat they were on screen during: a snapshot belongs to
// every beat whose span its display interval — from its own time to the next snapshot's —
// overlaps, so the screen showing when a beat starts counts for that beat. Spans are half-open
// except the last, which keeps the take's final instant.
func beatScreens(snapshots []Snapshot, spans []BeatSpan) map[int][]Snapshot {
	screens := make(map[int][]Snapshot, len(spans))
	for index, span := range spans {
		last := index == len(spans)-1
		for at, snapshot := range snapshots {
			shownUntil := time.Duration(math.MaxInt64)
			if at+1 < len(snapshots) {
				shownUntil = snapshots[at+1].At
			}
			startsInside := snapshot.At < span.End || (last && snapshot.At <= span.End)
			if startsInside && shownUntil > span.Start {
				screens[span.Beat] = append(screens[span.Beat], snapshot)
			}
		}
	}
	return screens
}

// judge evaluates the beat's expects, one row each; a beat without one gets a single PASS row
// so the table still accounts for it.
func (b Beat) judge(evidence takeEvidence) []checkRow {
	subject := fmt.Sprintf("beat %d", b.ID)
	if len(b.Expect) == 0 {
		return []checkRow{{Subject: subject, Verdict: verdictPass, Detail: "no expect"}}
	}
	rows := make([]checkRow, 0, len(b.Expect))
	for _, expect := range b.Expect {
		row := checkRow{Subject: subject}
		row.Verdict, row.Detail = expect.judge(evidence, evidence.screens[b.ID])
		rows = append(rows, row)
	}
	return rows
}

// judge evaluates one expect: its entry clauses — contains, before, after — on the entry its
// selector locates, then its seen clause on the beat's screens; every clause must hold. The
// detail spells the entry and the first clause that failed, or every clause that passed.
func (e Expect) judge(evidence takeEvidence, screens []Snapshot) (verdict, string) {
	var prefix string
	var judged located
	if e.Entry != nil {
		found, err := findEntry(evidence.entries, *e.Entry)
		if err != nil {
			return verdictFail, err.Error()
		}
		judged, prefix = found, describeEntry(found)+": "
	}
	var passed []string
	for _, clause := range e.clauses(evidence.anchors, screens) {
		detail, ok := clause(judged)
		if !ok {
			return verdictFail, prefix + detail
		}
		passed = append(passed, detail)
	}
	return verdictPass, prefix + strings.Join(passed, ", ")
}

// clause judges one assertion of an expect, returning the detail to print and whether it held.
// The entry clauses read the judged entry; the seen clause ignores it.
type clause func(judged located) (string, bool)

// clauses lists the assertions the expect sets, in the order the schema names them.
func (e Expect) clauses(anchors map[int]int, screens []Snapshot) []clause {
	var clauses []clause
	if e.Contains != "" {
		clauses = append(clauses, containsClause(e.Contains))
	}
	if e.Before != 0 {
		clauses = append(clauses, orderClause("before", e.Before, anchors[e.Before], func(judged, other int) bool { return judged < other }))
	}
	if e.After != 0 {
		clauses = append(clauses, orderClause("after", e.After, anchors[e.After], func(judged, other int) bool { return judged > other }))
	}
	if e.Seen != "" {
		clauses = append(clauses, seenClause(e.Seen, screens))
	}
	return clauses
}

// containsClause holds when the entry's text, tool label or tool stat contains the substring.
func containsClause(want string) clause {
	return func(judged located) (string, bool) {
		if entryContains(judged.Entry, want) {
			return fmt.Sprintf("contains %q", want), true
		}
		return fmt.Sprintf("does not contain %q", want), false
	}
}

// orderClause holds when the judged entry's list position relates to the entry beat other's
// first entry expect locates (otherIndex) as holds says. A beat that locates no entry cannot be
// ordered against, and the row names it.
func orderClause(name string, other, otherIndex int, holds func(judged, other int) bool) clause {
	return func(judged located) (string, bool) {
		if otherIndex == noEntry {
			return fmt.Sprintf("%s beat %d: beat %d locates no session entry to order against", name, other, other), false
		}
		if holds(judged.Index, otherIndex) {
			return fmt.Sprintf("%s beat %d (entry %d)", name, other, otherIndex), true
		}
		return fmt.Sprintf("is not %s beat %d (entry %d)", name, other, otherIndex), false
	}
}

// seenClause holds when the regex matches the screen of any of the beat's snapshots; the detail
// names the first that matched, on the take's clock.
func seenClause(pattern string, screens []Snapshot) clause {
	return func(located) (string, bool) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Sprintf("seen /%s/: %v", pattern, err), false
		}
		for _, screen := range screens {
			if re.MatchString(screenText(screen)) {
				return fmt.Sprintf("seen /%s/ at %s", pattern, screen.At.Round(time.Millisecond)), true
			}
		}
		return fmt.Sprintf("not seen /%s/ on any of the beat's %d screen(s)", pattern, len(screens)), false
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
