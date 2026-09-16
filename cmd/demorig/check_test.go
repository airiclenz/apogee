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

	"github.com/airiclenz/apogee/internal/session"
)

// heroCheck judges the shipped hero storyboard against the given entries, with no stage.
func heroCheck(t *testing.T, entries []session.Entry) (*Storyboard, []checkRow) {
	t.Helper()
	board, err := Load(heroStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", heroStoryboard, err)
	}
	rows, err := checkTake(context.Background(), board, entries, "")
	if err != nil {
		t.Fatalf("checkTake: %v", err)
	}
	return board, rows
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

func TestCheckTakeHero_AllPass(t *testing.T) {
	t.Parallel()
	entries, err := loadEntries(heroFixture)
	if err != nil {
		t.Fatalf("loadEntries: %v", err)
	}

	board, rows := heroCheck(t, entries)

	if failed := countFailed(rows); failed != 0 {
		t.Errorf("want no FAIL row, got %d:\n%s", failed, renderRows(t, rows))
	}
	for _, beat := range board.Beats {
		if len(rowsFor(rows, fmt.Sprintf("beat %d", beat.ID))) == 0 {
			t.Errorf("beat %d: no row in the table", beat.ID)
		}
	}
	beat4 := rowsFor(rows, "beat 4")
	if len(beat4) != 1 || beat4[0].Verdict != verdictPass || !strings.Contains(beat4[0].Detail, "before beat 6") {
		t.Errorf("beat 4: want one PASS row on `before: 6`, got %+v", beat4)
	}
	stage := rowsFor(rows, stageSubject)
	if len(stage) != 1 || stage[0].Verdict != verdictSkip {
		t.Errorf("stage: want one SKIP row without --stage, got %+v", stage)
	}
}

// TestCheckTakeHero_BeforeFiveFails rewrites beat 4's ordering expect to `before: 5` — the
// delivery order the fixture mirrors puts the interjection AFTER the fix card — and checks the
// row fails.
func TestCheckTakeHero_BeforeFiveFails(t *testing.T) {
	t.Parallel()
	board, err := Load(heroStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", heroStoryboard, err)
	}
	for index := range board.Beats {
		if board.Beats[index].ID == 4 {
			board.Beats[index].Expect[0].Before = 5
		}
	}

	rows, err := checkTake(context.Background(), board, heroEntries(), "")

	if err != nil {
		t.Fatalf("checkTake: %v", err)
	}
	beat4 := rowsFor(rows, "beat 4")
	if len(beat4) != 1 || beat4[0].Verdict != verdictFail || !strings.Contains(beat4[0].Detail, "is not before beat 5") {
		t.Errorf("beat 4: want a FAIL row on `before: 5`, got %+v", beat4)
	}
	if countFailed(rows) != 1 {
		t.Errorf("want exactly one FAIL row, got:\n%s", renderRows(t, rows))
	}
}

// TestCheckTakeHero_MissedInterjectionNamesTheKind mutates the fixture the way a missed
// interjection looks in a real take — the entry reads `user` instead of `interjected` — and
// checks beat 4 fails naming the kind it wanted.
func TestCheckTakeHero_MissedInterjectionNamesTheKind(t *testing.T) {
	t.Parallel()
	entries := heroEntries()
	for index := range entries {
		if entries[index].Kind == session.EntryKindInterjected {
			entries[index].Kind = session.EntryKindUser
		}
	}

	_, rows := heroCheck(t, entries)

	beat4 := rowsFor(rows, "beat 4")
	if len(beat4) != 1 || beat4[0].Verdict != verdictFail {
		t.Fatalf("beat 4: want one FAIL row, got %+v", beat4)
	}
	if !strings.Contains(beat4[0].Detail, "interjected") {
		t.Errorf("beat 4: want the detail to name the kind interjected, got %q", beat4[0].Detail)
	}
	if countFailed(rows) != 1 {
		t.Errorf("want exactly one FAIL row, got:\n%s", renderRows(t, rows))
	}
}

// TestCheckTakeHero_BeatWithoutExpectPasses pins beat 7: a video anchor with no expect needs
// no session entry and still reports a row.
func TestCheckTakeHero_BeatWithoutExpectPasses(t *testing.T) {
	t.Parallel()

	_, rows := heroCheck(t, heroEntries())

	beat7 := rowsFor(rows, "beat 7")
	if len(beat7) != 1 || beat7[0].Verdict != verdictPass || beat7[0].Detail != "no expect" {
		t.Errorf("beat 7: want one PASS row reading `no expect`, got %+v", beat7)
	}
}

func TestCheckTake_UnresolvedAnchorIsAnError(t *testing.T) {
	t.Parallel()
	board := testBoard(Beat{ID: 3, Anchor: Anchor{Kind: "toolCall", Tool: "Git"}, Expect: []Expect{{Contains: "x"}}})

	_, err := checkTake(context.Background(), board, heroEntries(), "")

	if err == nil || !strings.HasPrefix(err.Error(), "beat 3: no entry matches") {
		t.Fatalf("want the resolver's error naming beat 3, got %v", err)
	}
}

func TestExpectJudge_Contains(t *testing.T) {
	t.Parallel()
	entries := heroEntries()
	times := map[int]BeatTime{3: {ID: 3, Index: 2}, 8: {ID: 8, Index: noEntry}, 9: {ID: 9, Index: 9}}
	cases := []struct {
		name    string
		beat    Beat
		expect  Expect
		verdict verdict
		detail  string
	}{
		{name: "tool stat", beat: Beat{ID: 3}, expect: Expect{Contains: "FAIL"},
			verdict: verdictPass, detail: `entry 2 toolCall Tests FAIL: contains "FAIL"`},
		{name: "tool label", beat: Beat{ID: 3}, expect: Expect{Contains: "Tests"},
			verdict: verdictPass, detail: `contains "Tests"`},
		{name: "text", beat: Beat{ID: 9}, expect: Expect{Contains: "suite passes"},
			verdict: verdictPass, detail: `contains "suite passes"`},
		{name: "missing", beat: Beat{ID: 3}, expect: Expect{Contains: "PASS"},
			verdict: verdictFail, detail: `entry 2 toolCall Tests FAIL: does not contain "PASS"`},
		{name: "own selector", beat: Beat{ID: 3}, expect: Expect{Entry: &Anchor{Kind: "user"}, Contains: "failing"},
			verdict: verdictPass, detail: `entry 0 user`},
		{name: "no session entry", beat: Beat{ID: 8, Anchor: Anchor{Video: VideoEnd}}, expect: Expect{Contains: "x"},
			verdict: verdictFail, detail: `{video: end} anchors no session entry to judge`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			verdict, detail := tc.expect.judge(tc.beat, entries, times)

			if verdict != tc.verdict || !strings.Contains(detail, tc.detail) {
				t.Errorf("want %s containing %q, got %s %q", tc.verdict, tc.detail, verdict, detail)
			}
		})
	}
}

func TestExpectJudge_OrderAgainstBeatWithoutEntryFails(t *testing.T) {
	t.Parallel()
	times := map[int]BeatTime{2: {ID: 2, Index: 0}, 7: {ID: 7, Index: noEntry}}

	verdict, detail := (Expect{Before: 7}).judge(Beat{ID: 2}, heroEntries(), times)

	if verdict != verdictFail || !strings.Contains(detail, "beat 7: that beat anchors no session entry") {
		t.Errorf("want FAIL naming beat 7, got %s %q", verdict, detail)
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

func TestJudgeStage_NotARepoIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	_, err := judgeStage(context.Background(), t.TempDir())

	if err == nil || !strings.HasPrefix(err.Error(), "stage: git -C") {
		t.Fatalf("want a git error, got %v", err)
	}
}

// TestCheckCommand_ExitStatus drives the subcommand: the fixture passes with exit 0 and the
// stage row SKIP when no --stage is given, and a clean stage fails the run with exit 1.
func TestCheckCommand_ExitStatus(t *testing.T) {
	t.Parallel()
	clean := stageRepo(t)
	cases := []struct {
		name     string
		args     []string
		exitCode int
		want     string
	}{
		{name: "no stage", args: []string{"check", heroStoryboard, heroFixture},
			exitCode: 0, want: "stage  | SKIP |"},
		{name: "clean stage", args: []string{"check", heroStoryboard, heroFixture, "--stage", clean},
			exitCode: exitRunFailed, want: "stage  | FAIL |"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := newRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs(tc.args)

			err := root.ExecuteContext(context.Background())

			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("want the table to carry %q, got:\n%s", tc.want, out.String())
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
