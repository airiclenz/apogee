package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// fixture is a storyboard fixture under testdata.
func fixture(name string) string {
	return filepath.Join("testdata", name)
}

func TestLoadGoodFixture(t *testing.T) {
	t.Parallel()

	board, err := Load(fixture("good.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	dir := "testdata"
	for _, resolved := range []struct{ name, got, want string }{
		{"ship", board.Ship, filepath.Join(dir, "tiny.gif")},
		{"cassette", board.Cassette, filepath.Join(dir, "tiny.cassette")},
		{"fonts", board.Fonts, filepath.Join("..", "..", "graphics", "demo", "fonts")},
	} {
		if resolved.got != resolved.want {
			t.Errorf("%s: want %q, got %q", resolved.name, resolved.want, resolved.got)
		}
	}
	wantFrame := Frame{Cols: 80, Rows: 24, Padding: 16, FontSize: 14, LineHeight: 1.2, Scale: 2, Width: 600, FPS: 24, MaxColors: 128}
	if board.Frame != wantFrame {
		t.Errorf("frame: want %+v, got %+v", wantFrame, board.Frame)
	}
	if got := len(board.Beats); got != 4 {
		t.Fatalf("beats: want 4, got %d", got)
	}

	click := board.Beats[1].Do[0].Click
	if click == nil {
		t.Fatalf("beat 2 do[0]: want a click, got %+v", board.Beats[1].Do[0])
	}
	wantTarget := Target{Text: "Auto", Nth: TargetFirst, Area: AreaFooter}
	if click.Target != wantTarget || click.Times != 2 {
		t.Errorf("beat 2 click: want target %+v times 2, got %+v", wantTarget, *click)
	}
	if zoom := board.Beats[1].Zoom; zoom == nil || zoom.Target.Nth != TargetLast || zoom.Factor != 1.5 {
		t.Errorf("beat 2 zoom: want a last-match target at 1.5×, got %+v", zoom)
	}
	if got := board.Beats[1].Hold; got != time.Second {
		t.Errorf("beat 2 hold: want 1s, got %s", got)
	}
	if got := board.Beats[0].Hold; got != 0 {
		t.Errorf("beat 1 hold: want 0 when unset, got %s", got)
	}

	typed := board.Beats[2].Do
	if !typed[0].Type.IsHumanized() {
		t.Errorf("beat 3 do[0]: want humanized by default")
	}
	if typed[1].Type.IsHumanized() {
		t.Errorf("beat 3 do[1]: want humanize: false honoured")
	}
	if wait := typed[3].Wait; wait == nil || !wait.Gone || wait.Timeout != 3*time.Minute {
		t.Errorf("beat 3 do[3]: want a gone wait with a 3m timeout, got %+v", wait)
	}
	expect := board.Beats[2].Expect[0]
	if expect.Entry == nil || expect.Entry.Kind != "toolCall" || expect.Entry.Nth != NthLast {
		t.Errorf("beat 3 expect: want a last toolCall entry, got %+v", expect.Entry)
	}
	if !board.Beats[3].Cut || board.Beats[3].Duration != 0 {
		t.Errorf("beat 4: want a cut beat without a duration, got %+v", board.Beats[3])
	}
}

func TestLoadRejectsBadFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fixture string
		wantErr string
	}{
		{"bad-retired-header.yaml", "field tape not found"},
		{"bad-retired-anchor.yaml", "field anchor not found"},
		{"bad-missing-fonts.yaml", "fonts: missing"},
		{"bad-max-colors.yaml", "frame.max_colors: want 2..256, got 300"},
		{"bad-duplicate-id.yaml", "beat 1: id: duplicate"},
		{"bad-duration.yaml", "beat 2: duration: want >0"},
		{"bad-hold.yaml", "beat 2: hold: want less than duration 3s, got 3s"},
		{"bad-zoom-factor.yaml", "beat 2: zoom.factor: want in [1, 3], got 4"},
		{"bad-wait-timeout.yaml", "beat 3: do[3]: wait.timeout: want in (0, 3m0s], got 3m1s"},
		{"bad-target-regex.yaml", "beat 2: do[0]: click.target.text: error parsing regexp"},
		{"bad-target-area.yaml", `beat 2: do[0]: click.target.area: want one of footer, status, transcript, any, got "header"`},
		{"bad-target-nth.yaml", `nth: want first, last or a positive integer, got "0"`},
		{"bad-action-forms.yaml", "beat 2: do[1]: want exactly one of type, key, click, wait or pause"},
		{"bad-pause.yaml", "beat 2: do[1]: pause.for: want >0"},
		{"bad-expect-without-entry.yaml", "beat 3: expect[0]: want entry or seen"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			_, err := Load(fixture(tc.fixture))

			if err == nil {
				t.Fatalf("want an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want an error containing %q, got:\n%v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "many.yaml")
	board := `clip: many
frame: {cols: 80, rows: 24, font_size: 14, line_height: 1.2, scale: 2, width: 600, fps: 24, max_colors: 128}
beats:
  - id: 1
    title: open
    duration: 1s
    do: [{wait: {screen: "x", timeout: 0s}}]
`
	if err := os.WriteFile(path, []byte(board), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := Load(path)

	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("want a *ValidationError, got %v", err)
	}
	for _, want := range []string{"ship: missing", "cassette: missing", "fonts: missing", "beat 1: do[0]: wait.timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want a problem containing %q, got:\n%v", want, err)
		}
	}
	if got := len(invalid.Problems); got != 4 {
		t.Errorf("problems: want 4, got %d:\n%v", got, err)
	}
}

func TestNthUnmarshalYAML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		yaml    string
		want    Nth
		wantErr string
	}{
		{yaml: "nth: last", want: NthLast},
		{yaml: "nth: 2", want: 2},
		{yaml: "nth: 0", wantErr: "nth: want a positive integer or last, got \"0\""},
		{yaml: "nth: foo", wantErr: "nth: want a positive integer or last, got \"foo\""},
		{yaml: "nth: [1]", wantErr: "nth: want a positive integer or last, got a sequence"},
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			t.Parallel()

			var into struct {
				Nth Nth `yaml:"nth"`
			}
			err := yaml.Unmarshal([]byte(tc.yaml), &into)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if into.Nth != tc.want {
				t.Errorf("want %d, got %d", tc.want, into.Nth)
			}
		})
	}
}

func TestExpectValidate(t *testing.T) {
	t.Parallel()
	ids := map[int]bool{1: true, 2: true}
	entry := &EntrySelector{Kind: "toolCall"}
	cases := []struct {
		name   string
		expect Expect
		want   []string
	}{
		{name: "seen alone", expect: Expect{Seen: `Auto`}},
		{name: "entry with contains", expect: Expect{Entry: entry, Contains: "PASS"}},
		{name: "entry, order and seen", expect: Expect{Entry: entry, After: 1, Seen: `PASS`}},
		{name: "neither entry nor seen", expect: Expect{}, want: []string{"want entry or seen"}},
		{name: "contains without entry", expect: Expect{Seen: "x", Contains: "PASS"},
			want: []string{"contains, before and after judge an entry: set entry"}},
		{name: "entry without a clause", expect: Expect{Entry: entry},
			want: []string{"entry: want at least one of contains, before or after with it"}},
		{name: "bad seen regex", expect: Expect{Seen: "("}, want: []string{"seen: error parsing regexp"}},
		{name: "order against itself", expect: Expect{Entry: entry, Before: 2},
			want: []string{"before: 2 is not another beat"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string

			tc.expect.validate(2, ids, func(format string, args ...any) {
				got = append(got, fmt.Sprintf(format, args...))
			})

			if len(got) != len(tc.want) {
				t.Fatalf("want %d problem(s) %q, got %q", len(tc.want), tc.want, got)
			}
			for index, want := range tc.want {
				if !strings.Contains(got[index], want) {
					t.Errorf("problem %d: want %q, got %q", index, want, got[index])
				}
			}
		})
	}
}
