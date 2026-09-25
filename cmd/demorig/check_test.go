package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/session"
)

// checkStoryboard is the check tests' storyboard; its entry expects match heroEntries.
const checkStoryboard = "testdata/check.yaml"

// textSnapshot is a snapshot at at whose rows read lines, one cell per rune.
func textSnapshot(at time.Duration, lines ...string) Snapshot {
	cells := make([][]TakeCell, 0, len(lines))
	for _, line := range lines {
		row := make([]TakeCell, 0, len(line))
		for _, r := range line {
			row = append(row, TakeCell{Rune: string(r), Width: 1})
		}
		cells = append(cells, row)
	}
	return Snapshot{At: at, Cells: cells}
}

// checkFixtureTake is a take of checkStoryboard: its five beats start two seconds apart, its
// session is the committed hero fixture, and its screens show "Auto" at the first paint, the
// typed prompt from 1s — still on screen when beat 2 starts — and the green test line inside
// beat 4 only.
func checkFixtureTake(t *testing.T) *Take {
	t.Helper()
	sessionPath, err := filepath.Abs(heroFixture)
	if err != nil {
		t.Fatalf("abs %s: %v", heroFixture, err)
	}
	take := &Take{
		Cols: 40, Rows: 3, FPS: 24, Session: sessionPath,
		Snapshots: []Snapshot{
			textSnapshot(0, "mode Auto"),
			textSnapshot(sec, "tests are failing"),
			textSnapshot(6500*time.Millisecond, "ok   taskman 0.1s"),
			textSnapshot(10*sec, "done"),
		},
	}
	for beat := 1; beat <= 5; beat++ {
		take.Events = append(take.Events, beatStart(t, time.Duration(beat-1)*2*sec, beat))
	}
	return take
}

// loadCheckStoryboard loads checkStoryboard.
func loadCheckStoryboard(t *testing.T) *Storyboard {
	t.Helper()
	board, err := Load(checkStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", checkStoryboard, err)
	}
	return board
}

// checkRows judges board against the fixture take, with no stage.
func checkRows(t *testing.T, board *Storyboard) []checkRow {
	t.Helper()
	rows, err := checkTake(context.Background(), board, checkFixtureTake(t), "")
	if err != nil {
		t.Fatalf("checkTake: %v", err)
	}
	return rows
}

// rowsFor returns the rows of one subject.
func rowsFor(rows []checkRow, subject string) []checkRow {
	var matched []checkRow
	for _, row := range rows {
		if row.Subject == subject {
			matched = append(matched, row)
		}
	}
	return matched
}

// beatByID returns a pointer to the board's beat id, for a test to rewrite.
func beatByID(t *testing.T, board *Storyboard, id int) *Beat {
	t.Helper()
	for index := range board.Beats {
		if board.Beats[index].ID == id {
			return &board.Beats[index]
		}
	}
	t.Fatalf("no beat %d", id)
	return nil
}

func TestCheckTake_AllPass(t *testing.T) {
	t.Parallel()
	board := loadCheckStoryboard(t)

	rows := checkRows(t, board)

	if failed := countFailed(rows); failed != 0 {
		t.Errorf("want no FAIL row, got %d:\n%s", failed, renderRows(t, rows))
	}
	for _, beat := range board.Beats {
		if len(rowsFor(rows, fmt.Sprintf("beat %d", beat.ID))) == 0 {
			t.Errorf("beat %d: no row in the table", beat.ID)
		}
	}
	beat3 := rowsFor(rows, "beat 3")
	if len(beat3) != 1 || !strings.Contains(beat3[0].Detail, "before beat 4 (entry 8), after beat 2 (entry 2)") {
		t.Errorf("beat 3: want the order against beats 2 and 4's first entry expects, got %+v", beat3)
	}
	if beat5 := rowsFor(rows, "beat 5"); len(beat5) != 1 || beat5[0].Detail != "no expect" {
		t.Errorf("beat 5: want one PASS row reading `no expect`, got %+v", beat5)
	}
	stage := rowsFor(rows, stageSubject)
	if len(stage) != 1 || stage[0].Verdict != verdictSkip {
		t.Errorf("stage: want one SKIP row without --stage, got %+v", stage)
	}
}

func TestCheckTake_Seen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		beat    int
		seen    string
		verdict verdict
		detail  string
	}{
		{name: "inside the beat", beat: 4, seen: `ok\s+taskman`, verdict: verdictPass, detail: `seen /ok\s+taskman/ at 6.5s`},
		{name: "the screen showing at the beat's start", beat: 2, seen: `tests are failing`, verdict: verdictPass,
			detail: `seen /tests are failing/ at 1s`},
		{name: "only outside the beat", beat: 4, seen: `Auto`, verdict: verdictFail,
			detail: `not seen /Auto/ on any of the beat's 2 screen(s)`},
		{name: "nowhere", beat: 1, seen: `Plan`, verdict: verdictFail, detail: `not seen /Plan/`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			board := loadCheckStoryboard(t)
			beat := beatByID(t, board, tc.beat)
			beat.Expect = []Expect{{Seen: tc.seen}}

			rows := rowsFor(checkRows(t, board), fmt.Sprintf("beat %d", tc.beat))

			if len(rows) != 1 || rows[0].Verdict != tc.verdict || !strings.Contains(rows[0].Detail, tc.detail) {
				t.Errorf("want one %s row containing %q, got %+v", tc.verdict, tc.detail, rows)
			}
		})
	}
}

func TestCheckTake_Order(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		rewrite func(board *Storyboard)
		verdict verdict
		detail  string
	}{
		{name: "after the beat's first entry expect",
			rewrite: func(board *Storyboard) { board.Beats[2].Expect[0].Before = 0 },
			verdict: verdictPass, detail: "after beat 2 (entry 2)"},
		{name: "out of order",
			rewrite: func(board *Storyboard) { board.Beats[2].Expect[0].After, board.Beats[2].Expect[0].Before = 0, 2 },
			verdict: verdictFail, detail: "is not before beat 2 (entry 2)"},
		{name: "a beat without an entry expect",
			rewrite: func(board *Storyboard) { board.Beats[2].Expect[0].After, board.Beats[2].Expect[0].Before = 5, 0 },
			verdict: verdictFail, detail: "after beat 5: beat 5 locates no session entry to order against"},
		{name: "a beat whose entry expect locates nothing",
			rewrite: func(board *Storyboard) {
				board.Beats[1].Expect[0].Entry = &EntrySelector{Kind: "toolCall", Tool: "Git"}
				board.Beats[2].Expect[0].Before = 0
			},
			verdict: verdictFail, detail: "after beat 2: beat 2 locates no session entry to order against"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			board := loadCheckStoryboard(t)
			tc.rewrite(board)

			rows := rowsFor(checkRows(t, board), "beat 3")

			if len(rows) != 1 || rows[0].Verdict != tc.verdict || !strings.Contains(rows[0].Detail, tc.detail) {
				t.Errorf("want one %s row containing %q, got %+v", tc.verdict, tc.detail, rows)
			}
		})
	}
}

func TestCheckTake_ContainsAndLocation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		expect  Expect
		verdict verdict
		detail  string
	}{
		{name: "tool stat", expect: Expect{Entry: &EntrySelector{Kind: "toolCall", Tool: "Tests"}, Contains: "FAIL"},
			verdict: verdictPass, detail: `entry 2 toolCall Tests FAIL: contains "FAIL"`},
		{name: "text", expect: Expect{Entry: &EntrySelector{Kind: "assistant", Nth: NthLast}, Contains: "suite passes"},
			verdict: verdictPass, detail: `contains "suite passes"`},
		{name: "missing", expect: Expect{Entry: &EntrySelector{Kind: "toolCall", Tool: "Tests"}, Contains: "PASS"},
			verdict: verdictFail, detail: `entry 2 toolCall Tests FAIL: does not contain "PASS"`},
		{name: "unlocated", expect: Expect{Entry: &EntrySelector{Kind: "toolCall", Tool: "Git"}, Contains: "x"},
			verdict: verdictFail, detail: "no entry matches {kind: toolCall, tool: Git}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			board := loadCheckStoryboard(t)
			beatByID(t, board, 5).Expect = []Expect{tc.expect}

			rows := rowsFor(checkRows(t, board), "beat 5")

			if len(rows) != 1 || rows[0].Verdict != tc.verdict || !strings.Contains(rows[0].Detail, tc.detail) {
				t.Errorf("want one %s row containing %q, got %+v", tc.verdict, tc.detail, rows)
			}
		})
	}
}

// summaryShapedTranscript carries a Replace and a Tests entry exactly as apogee saves them: the
// outcome rides in tool.summary.text and the stat is left empty.
const summaryShapedTranscript = `{"version": 1, "entries": [
	{"kind": "toolCall", "callID": "call_1", "done": true, "tool": {"label": "Replace", "verb": "editing",
		"target": "task.go", "name": "single_find_and_replace",
		"summary": {"text": "+1 \u22121", "stat": {"added": 1, "removed": 1}}}},
	{"kind": "toolCall", "callID": "call_2", "done": true, "tool": {"label": "Tests",
		"verb": "running tests", "name": "run_tests", "summary": {"text": "PASS"}}}
]}`

func TestEntryContains_ReadsTheToolSummary(t *testing.T) {
	t.Parallel()
	entries, err := session.DecodeTranscript([]byte(summaryShapedTranscript))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cases := []struct {
		name  string
		entry session.Entry
		want  string
		holds bool
	}{
		{name: "edit diffstat", entry: entries[0], want: "+1 −1", holds: true},
		{name: "tests verdict", entry: entries[1], want: "PASS", holds: true},
		{name: "absent", entry: entries[1], want: "FAIL", holds: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := entryContains(tc.entry, tc.want); got != tc.holds {
				t.Errorf("entryContains(%q) = %v, want %v", tc.want, got, tc.holds)
			}
		})
	}
}

func TestCheckTake_TakeWithoutSessionIsAnError(t *testing.T) {
	t.Parallel()
	take := checkFixtureTake(t)
	take.Session = ""

	_, err := checkTake(context.Background(), loadCheckStoryboard(t), take, "")

	if err == nil || !strings.Contains(err.Error(), "names no saved session") {
		t.Fatalf("want an error naming the missing session, got %v", err)
	}
}

// stageRepo makes a git repo in a temp dir with one committed file, and returns its path.
func stageRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "task.go"), []byte("package taskman\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.email=demo@local", "-c", "user.name=demo", "commit", "-q", "-m", "seed"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestJudgeStage(t *testing.T) {
	t.Parallel()
	clean := stageRepo(t)
	dirty := stageRepo(t)
	if err := os.WriteFile(filepath.Join(dirty, "task.go"), []byte("package taskman // fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		stage   string
		verdict verdict
		detail  string
	}{
		{name: "no stage", stage: "", verdict: verdictSkip, detail: "no --stage given"},
		{name: "clean", stage: clean, verdict: verdictFail, detail: "is clean"},
		{name: "dirty", stage: dirty, verdict: verdictPass, detail: "1 changed path(s)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row, err := judgeStage(context.Background(), tc.stage)

			if err != nil {
				t.Fatalf("judgeStage: %v", err)
			}
			if row.Subject != stageSubject || row.Verdict != tc.verdict || !strings.Contains(row.Detail, tc.detail) {
				t.Errorf("want %s containing %q, got %+v", tc.verdict, tc.detail, row)
			}
		})
	}
}

// TestJudgeStage_NotARepoIsAFailRow pins that a stage git refuses to judge — a directory with
// no .git, or none at all — is a FAIL row naming the path and git's reason, never an error:
// the beat rows before it must still print.
func TestJudgeStage_NotARepoIsAFailRow(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	cases := map[string]string{
		"no .git":     t.TempDir(),
		"nonexistent": filepath.Join(t.TempDir(), "missing"),
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			row, err := judgeStage(context.Background(), stage)

			if err != nil {
				t.Fatalf("want a row, got error %v", err)
			}
			if row.Subject != stageSubject || row.Verdict != verdictFail {
				t.Fatalf("want a %s FAIL row, got %+v", stageSubject, row)
			}
			wantPrefix := "stage: " + stage + " is not a git work tree (git status: "
			if !strings.HasPrefix(row.Detail, wantPrefix) || !strings.HasSuffix(row.Detail, ")") {
				t.Errorf("want detail %q…%q, got %q", wantPrefix, ")", row.Detail)
			}
			if strings.Contains(row.Detail, "\n") {
				t.Errorf("want a one-line detail, got %q", row.Detail)
			}
		})
	}
}

// saveCheckTake writes the fixture take into dir as the check storyboard's clip take, and returns
// its path.
func saveCheckTake(t *testing.T, dir string) string {
	t.Helper()
	path := takeFile(dir, "checked")
	if err := SaveTake(path, checkFixtureTake(t)); err != nil {
		t.Fatalf("SaveTake: %v", err)
	}
	return path
}

// runRoot runs the demorig command line args and returns what it printed and its error.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

// TestCheckCommand_ExitStatus drives the subcommand: the fixture take passes with exit 0 and the
// stage row SKIP when no --stage is given, a clean stage fails the run with exit 1, and a
// --stage dir git cannot judge fails the same way with every beat row still printed.
func TestCheckCommand_ExitStatus(t *testing.T) {
	t.Parallel()
	clean := stageRepo(t)
	take := saveCheckTake(t, t.TempDir())
	var beatRows []string
	for _, beat := range loadCheckStoryboard(t).Beats {
		beatRows = append(beatRows, fmt.Sprintf("beat %d |", beat.ID))
	}
	cases := []struct {
		name     string
		args     []string
		exitCode int
		want     []string
	}{
		{name: "no stage", args: []string{"check", checkStoryboard, take},
			exitCode: 0, want: []string{"stage  | SKIP |"}},
		{name: "clean stage", args: []string{"check", checkStoryboard, take, "--stage", clean},
			exitCode: exitRunFailed, want: []string{"stage  | FAIL |"}},
		{name: "missing stage", args: []string{"check", checkStoryboard, take, "--stage", filepath.Join(clean, "missing")},
			exitCode: exitRunFailed, want: append(beatRows, "stage  | FAIL |")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := runRoot(t, tc.args...)

			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("want the table to carry %q, got:\n%s", want, out)
				}
			}
			if tc.exitCode == 0 {
				if err != nil {
					t.Fatalf("want a clean run, got %v", err)
				}
				return
			}
			if err == nil || exitCodeFor(err) != tc.exitCode {
				t.Fatalf("want exit %d, got %v", tc.exitCode, err)
			}
		})
	}
}

// TestCheckCommand_DefaultTakeFollowsTheWorkDir leaves the take off the command line: check reads
// <$APOGEE_DEMO_WORK>/<clip>.take.
func TestCheckCommand_DefaultTakeFollowsTheWorkDir(t *testing.T) {
	work := t.TempDir()
	t.Setenv(workDirEnv, work)
	saveCheckTake(t, work)

	out, err := runRoot(t, "check", checkStoryboard)

	if err != nil {
		t.Fatalf("want a clean run from the default take, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "beat 4 | PASS | entry 8 toolCall Tests PASS") {
		t.Errorf("want the default take judged, got:\n%s", out)
	}
}

func TestTakeArgDefaultsIntoTheWorkDir(t *testing.T) {
	work := t.TempDir()
	t.Setenv(workDirEnv, work)

	defaulted, err := takeArg(nil, "hero")
	if err != nil {
		t.Fatalf("takeArg: %v", err)
	}
	given, err := takeArg([]string{"elsewhere.take"}, "hero")
	if err != nil {
		t.Fatalf("takeArg: %v", err)
	}

	if want := filepath.Join(work, "hero.take"); defaulted != want {
		t.Errorf("default take: want %q, got %q", want, defaulted)
	}
	if given != "elsewhere.take" {
		t.Errorf("given take: want it kept, got %q", given)
	}
}

func TestWriteCheckTable(t *testing.T) {
	t.Parallel()
	rows := []checkRow{
		{Subject: "beat 3", Verdict: verdictPass, Detail: `entry 2 toolCall Tests FAIL: contains "FAIL"`},
		{Subject: stageSubject, Verdict: verdictSkip, Detail: "stage: dirty not judged — no --stage given"},
	}

	got := renderRows(t, rows)

	want := "beat 3 | PASS | entry 2 toolCall Tests FAIL: contains \"FAIL\"\n" +
		"stage  | SKIP | stage: dirty not judged — no --stage given\n"
	if got != want {
		t.Errorf("table:\nwant %q\ngot  %q", want, got)
	}
}

func TestHeadOf(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"short":                   "short",
		"first line\nsecond line": "first line…",
		strings.Repeat("é", 50):   strings.Repeat("é", textHead) + "…",
	}
	for text, want := range cases {
		if got := headOf(text); got != want {
			t.Errorf("headOf(%q): want %q, got %q", text, want, got)
		}
	}
}

// renderRows prints the rows through writeCheckTable.
func renderRows(t *testing.T, rows []checkRow) string {
	t.Helper()
	var out bytes.Buffer
	if err := writeCheckTable(&out, rows); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	return out.String()
}
