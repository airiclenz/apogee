package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// heroStoryboard is the shipped hero clip's storyboard, reached from this package's directory
// (go test runs with cwd cmd/demorig).
const heroStoryboard = "../../graphics/demo/storyboards/hero.yaml"

func TestLoadHeroStoryboard(t *testing.T) {
	t.Parallel()

	board, err := Load(heroStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", heroStoryboard, err)
	}

	if got := len(board.Beats); got != 8 {
		t.Fatalf("beats: want 8, got %d", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(board.Tape), "graphics/demo/tapes/hero.tape") {
		t.Errorf("tape: want a path ending graphics/demo/tapes/hero.tape, got %q", board.Tape)
	}
	if !strings.HasSuffix(filepath.ToSlash(board.Ship), "graphics/demo.gif") {
		t.Errorf("ship: want a path ending graphics/demo.gif, got %q", board.Ship)
	}
	if want := 9960 * time.Millisecond; board.Align.FirstPromptAt != want {
		t.Errorf("align.first_prompt_at: want %s, got %s", want, board.Align.FirstPromptAt)
	}
	if board.Frame.Scale != 2 || board.Frame.Width != 1250 {
		t.Errorf("frame: want width 1250 scale 2, got %+v", board.Frame)
	}

	beats := make(map[int]Beat, len(board.Beats))
	for _, beat := range board.Beats {
		beats[beat.ID] = beat
	}
	if got := beats[1].Anchor; got != (Anchor{Video: VideoFirstPaint}) {
		t.Errorf("beat 1 anchor: want {video: first-paint}, got %+v", got)
	}
	if got := beats[8].Anchor; got != (Anchor{Video: VideoEnd}) {
		t.Errorf("beat 8 anchor: want {video: end}, got %+v", got)
	}
	if got, want := beats[4].Anchor, (Anchor{Beat: 2, Offset: 10300 * time.Millisecond}); got != want {
		t.Errorf("beat 4 anchor: want %+v, got %+v", want, got)
	}
	if expects := beats[4].Expect; len(expects) != 1 || expects[0].Before != 6 ||
		expects[0].Entry == nil || expects[0].Entry.Kind != "interjected" {
		t.Errorf("beat 4 expect: want [{entry: {kind: interjected}, before: 6}], got %+v", expects)
	}
	if got, want := beats[7].Anchor, (Anchor{Video: VideoEnd, Offset: -6500 * time.Millisecond}); got != want {
		t.Errorf("beat 7 anchor: want %+v, got %+v", want, got)
	}
	if len(beats[7].Expect) != 0 {
		t.Errorf("beat 7 expect: want none, got %+v", beats[7].Expect)
	}

	zoom := beats[5].Frame.Zoom
	if zoom == nil {
		t.Fatalf("beat 5 zoom: want one, got none")
	}
	want := Zoom{Region: "edit-card", Factor: 1.5, In: 400 * time.Millisecond, Hold: 2500 * time.Millisecond, Out: 400 * time.Millisecond}
	if *zoom != want {
		t.Errorf("beat 5 zoom: want %+v, got %+v", want, *zoom)
	}
	if _, declared := board.Regions[zoom.Region]; !declared {
		t.Errorf("beat 5 zoom region %q: not declared under regions", zoom.Region)
	}
	if got := beats[5].Frame.Rate(); got != 1.5 {
		t.Errorf("beat 5 speed: want 1.5, got %g", got)
	}
	if got := beats[6].Anchor.Nth; got != NthLast {
		t.Errorf("beat 6 nth: want last, got %d", got)
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

func TestLoadRejectsBadFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fixture string
		wantErr string
	}{
		{"bad-unknown-field.yaml", "field colour not found"},
		{"bad-missing-tape-header.yaml", "beat 2: tape: no `# beat 9` header"},
		{"bad-undeclared-region.yaml", `beat 2: frame: zoom.region: "nowhere" is not declared`},
		{"bad-duplicate-id.yaml", "beat 1: id: duplicate"},
		{"bad-missing-first-paint.yaml", "beat 1: anchor: the first beat must anchor {video: first-paint}"},
		{"bad-missing-end.yaml", "beat 3: anchor: the last beat must anchor {video: end}"},
		{"bad-later-beat-ref.yaml", "beat 2: anchor: beat: 3 is not an earlier beat"},
		{"bad-expect-without-entry.yaml", "beat 2: expect[0]: entry: required"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			_, err := Load(filepath.Join("testdata", tc.fixture))

			if err == nil {
				t.Fatalf("want an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want an error containing %q, got:\n%v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadGoodFixtureResolvesTapeAgainstItsDirectory(t *testing.T) {
	t.Parallel()

	board, err := Load(filepath.Join("testdata", "good.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if want := filepath.Join("testdata", "tiny.tape"); board.Tape != want {
		t.Errorf("tape: want %q, got %q", want, board.Tape)
	}
	if want := filepath.Join("testdata", "tiny.gif"); board.Ship != want {
		t.Errorf("ship: want %q, got %q", want, board.Ship)
	}
}
