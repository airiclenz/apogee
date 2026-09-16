package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// heroGolden is the filtergraph the hero storyboard builds over heroTimes.
const heroGolden = "testdata/filtergraph-hero.txt"

// heroTake is the geometry and length of the take the golden is built over: the tape's 2×
// geometry and a 77-second take.
var heroTake = takeInfo{Size: Size{Width: 2500, Height: 1360}, Duration: 77 * time.Second}

// heroTimes are fixed beat times for the hero storyboard in heroTake, in storyboard order:
// the first paint, the prompt's Enter, the first Tests card, the interjection, the fix card,
// the last Tests card, /undo at end−6.5s and the end.
func heroTimes() []BeatTime {
	seconds := func(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
	return []BeatTime{
		{ID: 1, At: seconds(3.4), Index: noEntry},
		{ID: 2, At: seconds(9.96), Index: 0},
		{ID: 3, At: seconds(17.2), Index: 2},
		{ID: 4, At: seconds(20.26), Index: noEntry},
		{ID: 5, At: seconds(24.5), Index: 4},
		{ID: 6, At: seconds(33), Index: 8},
		{ID: 7, At: seconds(70.5), Index: noEntry},
		{ID: 8, At: seconds(77), Index: noEntry},
	}
}

// TestBuildHero_Golden pins the whole graph the hero storyboard produces: the head dropped,
// speed through setpts, the hold split off at 1×, the zoom as scale…eval=frame plus a
// fixed-size crop with t-only x/y, the empty last beat contributing nothing, and the palette
// tail. Set DEMORIG_UPDATE_FIXTURES=1 to rewrite the golden after a deliberate change.
func TestBuildHero_Golden(t *testing.T) {
	t.Parallel()
	board, err := Load(heroStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", heroStoryboard, err)
	}
	segments := segmentsFrom(board, heroTimes(), heroTake.Duration)

	graph, err := Build(segments, board.Frame, heroTake.Size)

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := []byte(graph + "\n")
	if os.Getenv(updateFixtures) != "" {
		if err := os.WriteFile(heroGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(heroGolden)
	if err != nil {
		t.Fatalf("read %s: %v (set %s=1 to write it)", heroGolden, err, updateFixtures)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale; set %s=1 to rewrite it\nwant %s\ngot  %s", heroGolden, updateFixtures, want, got)
	}
	for _, shape := range []string{
		"scale=w='iw*(1+0.5*clip(",
		":eval=frame,crop=2500:1360:x='1250*(",
		"setpts=(PTS-STARTPTS)/1.5",
		"concat=n=8:v=1:a=0[v];[v]fps=24,scale=1250:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=192[p];[b][p]paletteuse=dither=bayer:bayer_scale=3",
	} {
		if !strings.Contains(graph, shape) {
			t.Errorf("graph lacks %q:\n%s", shape, graph)
		}
	}
	if strings.Contains(graph, "trim=start=77:") {
		t.Errorf("the empty last beat produced a piece:\n%s", graph)
	}
}

func TestSegmentsFrom_SortsByTimeAndRunsToTheEnd(t *testing.T) {
	t.Parallel()
	speed := 2.0
	board := &Storyboard{Beats: []Beat{
		{ID: 1, Frame: Framing{Speed: &speed}},
		{ID: 2},
		{ID: 3, Frame: Framing{Zoom: &Zoom{Region: "card"}}},
	}, Regions: map[string][]float64{"card": {0, 0.5, 1, 0.5}}}
	times := []BeatTime{{ID: 1, At: 10 * time.Second}, {ID: 2, At: 30 * time.Second}, {ID: 3, At: 20 * time.Second}}

	segments := segmentsFrom(board, times, 40*time.Second)

	want := []Segment{
		{Beat: 1, Start: 10 * time.Second, End: 20 * time.Second, Frame: Framing{Speed: &speed}},
		{Beat: 3, Start: 20 * time.Second, End: 30 * time.Second, Frame: Framing{Zoom: &Zoom{Region: "card"}}, Region: []float64{0, 0.5, 1, 0.5}},
		{Beat: 2, Start: 30 * time.Second, End: 40 * time.Second},
	}
	if len(segments) != len(want) {
		t.Fatalf("want %d segments, got %+v", len(want), segments)
	}
	for index, segment := range segments {
		if segment.Beat != want[index].Beat || segment.Start != want[index].Start || segment.End != want[index].End {
			t.Errorf("segment %d: want %+v, got %+v", index, want[index], segment)
		}
	}
	if segments[0].Frame.Rate() != 2 || segments[1].Region == nil || segments[2].Region != nil {
		t.Errorf("framing and region not carried: %+v", segments)
	}
}

func TestBuild_CutOmitsTheSegment(t *testing.T) {
	t.Parallel()
	frame := Frame{Width: 100, Scale: 1, FPS: 10, MaxColors: 16}
	segments := []Segment{
		{Beat: 1, Start: 0, End: 5 * time.Second},
		{Beat: 2, Start: 5 * time.Second, End: 9 * time.Second, Frame: Framing{Cut: true}},
		{Beat: 3, Start: 9 * time.Second, End: 12 * time.Second},
	}

	graph, err := Build(segments, frame, Size{Width: 100, Height: 50})

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := "[0:v]trim=start=0:end=5,setpts=PTS-STARTPTS[s0];[0:v]trim=start=9:end=12,setpts=PTS-STARTPTS[s1];[s0][s1]concat=n=2:v=1:a=0[v];"
	if !strings.HasPrefix(graph, want) {
		t.Errorf("want prefix %q\ngot %q", want, graph)
	}
}

func TestBuild_RejectsBadSegments(t *testing.T) {
	t.Parallel()
	frame := Frame{Width: 100, Scale: 1, FPS: 10, MaxColors: 16}
	cases := []struct {
		name     string
		segments []Segment
		want     string
	}{
		{name: "unsorted", want: "overlaps beat 2", segments: []Segment{
			{Beat: 2, Start: 5 * time.Second, End: 9 * time.Second},
			{Beat: 1, Start: 0, End: 5 * time.Second},
		}},
		{name: "overlapping", want: "beat 2 (4s) overlaps beat 1 (ends 5s)", segments: []Segment{
			{Beat: 1, Start: 0, End: 5 * time.Second},
			{Beat: 2, Start: 4 * time.Second, End: 9 * time.Second},
		}},
		{name: "backwards", want: "beat 1 ends (1s) before it starts (2s)", segments: []Segment{
			{Beat: 1, Start: 2 * time.Second, End: time.Second},
		}},
		{name: "before the take", want: "beat 1 starts before the take", segments: []Segment{
			{Beat: 1, Start: -time.Second, End: time.Second},
		}},
		{name: "zoom without a region", want: "zoom region \"card\" is not a [x, y, w, h] rect", segments: []Segment{
			{Beat: 1, Start: 0, End: time.Second, Frame: Framing{Zoom: &Zoom{Region: "card", Factor: 2}}},
		}},
		{name: "everything cut", want: "every segment is cut or empty", segments: []Segment{
			{Beat: 1, Start: 0, End: time.Second, Frame: Framing{Cut: true}},
			{Beat: 2, Start: time.Second, End: time.Second},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Build(tc.segments, frame, Size{Width: 100, Height: 50})

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestZoomFactor_ZeroRampsAreSteps(t *testing.T) {
	t.Parallel()
	segment := Segment{Start: 10 * time.Second, Frame: Framing{Zoom: &Zoom{Factor: 2, Hold: 3 * time.Second}}}

	got := zoomFactor(segment)

	if want := "(1+1*clip(min(gte(t,10),lt(t,13)),0,1))"; got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

// fakeTools is the test renderTools: a fixed probe and a fixed first paint, counting probes.
type fakeTools struct {
	info   takeInfo
	paint  time.Duration
	probes int
}

func (f *fakeTools) Probe(context.Context, string) (takeInfo, error) {
	f.probes++
	return f.info, nil
}

func (f *fakeTools) Painter(string, float64) FirstPainter { return &fixedPainter{at: f.paint} }

// TestRenderCommand_DryRun drives the subcommand end to end over the hero storyboard and
// fixture with a fake probe: the ffmpeg command line is printed and nothing runs. A take at
// the tape's 2× geometry renders silently; a 1× take earns the zoom warning; the output
// defaults to the storyboard's ship path.
func TestRenderCommand_DryRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		width   int
		args    []string
		wantOut string
		warns   bool
	}{
		{name: "2x take", width: 2500, args: []string{"-o", "out.gif"}, wantOut: " out.gif\n"},
		{name: "1x take warns", width: 1250, args: []string{"-o", "out.gif"}, wantOut: " out.gif\n", warns: true},
		{name: "default output is ship", width: 2500, wantOut: " ../../graphics/demo.gif\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tools := &fakeTools{info: takeInfo{Size: Size{Width: tc.width, Height: 1360}, Duration: 90 * time.Second}, paint: 3400 * time.Millisecond}
			cmd := newRenderCommand(tools)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{heroStoryboard, "take.mp4", heroFixture, "--dry-run"}, tc.args...))

			err := cmd.ExecuteContext(context.Background())

			if err != nil {
				t.Fatalf("render --dry-run: %v\n%s", err, stderr.String())
			}
			line := stdout.String()
			if !strings.HasPrefix(line, `ffmpeg -y -loglevel error -i take.mp4 -filter_complex "[0:v]trim=start=3.4:`) {
				t.Errorf("want the ffmpeg command line, got %q", line)
			}
			if !strings.HasSuffix(line, tc.wantOut) {
				t.Errorf("want the line to end with %q, got %q", tc.wantOut, line)
			}
			if tools.probes != 1 {
				t.Errorf("want one probe, got %d", tools.probes)
			}
			if warned := strings.Contains(stderr.String(), "warning: take.mp4 is 1250px wide"); warned != tc.warns {
				t.Errorf("warning: want %v, got stderr %q", tc.warns, stderr.String())
			}
		})
	}
}

func TestParseProbe(t *testing.T) {
	t.Parallel()
	out := "width=2500\nheight=1360\nduration=77.123000\n"

	info, err := parseProbe("take.mp4", out)

	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if want := (takeInfo{Size: Size{Width: 2500, Height: 1360}, Duration: 77123 * time.Millisecond}); info != want {
		t.Errorf("want %+v, got %+v", want, info)
	}
	if _, err := parseProbe("take.mp4", "width=2500\n"); err == nil || !strings.Contains(err.Error(), "height") {
		t.Errorf("want a height error, got %v", err)
	}
}

func TestHumanSize(t *testing.T) {
	t.Parallel()
	cases := map[int64]string{512: "512B", 1536: "1.5K", 3_400_000: "3.2M", 52_428_800: "50M", 2_147_483_648: "2.0G"}
	for bytes, want := range cases {
		if got := humanSize(bytes); got != want {
			t.Errorf("humanSize(%d): want %s, got %s", bytes, want, got)
		}
	}
}

func TestShellLine(t *testing.T) {
	t.Parallel()
	got := shellLine([]string{"ffmpeg", "-filter_complex", "[0:v]trim=start=1,scale=w='iw*(2)'", "a b.gif", "out.gif"})

	if want := `ffmpeg -filter_complex "[0:v]trim=start=1,scale=w='iw*(2)'" "a b.gif" out.gif`; got != want {
		t.Errorf("want %s\ngot  %s", want, got)
	}
}
