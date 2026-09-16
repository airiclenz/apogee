package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/session"
)

func TestAlignerVideoTime(t *testing.T) {
	t.Parallel()
	board := &Storyboard{
		Align:    Align{FirstPromptAt: 9960 * time.Millisecond},
		PaintLag: 300 * time.Millisecond,
	}
	entries := []session.Entry{
		{Kind: session.EntryKindNote, At: heroStart.Add(-time.Minute)},
		{Kind: session.EntryKindUser, At: heroStart},
		{Kind: session.EntryKindUser, At: heroStart.Add(time.Hour)},
	}

	align, err := newAligner(board, entries)

	if err != nil {
		t.Fatalf("newAligner: %v", err)
	}
	cases := map[time.Duration]time.Duration{
		0:                        10260 * time.Millisecond, // the pin itself: 9.96s + 0 + 0.3s
		12200 * time.Millisecond: 22460 * time.Millisecond, // 9.96s + 12.2s + 0.3s
		-500 * time.Millisecond:  9760 * time.Millisecond,  // before the pin: 9.96s − 0.5s + 0.3s
	}
	for sinceFirstUser, want := range cases {
		if got := align.videoTime(heroStart.Add(sinceFirstUser)); got != want {
			t.Errorf("videoTime(first user + %s): want %s, got %s", sinceFirstUser, want, got)
		}
	}
}

func TestNewAligner_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		entries []session.Entry
		wantErr string
	}{
		{name: "no user entry", entries: []session.Entry{{Kind: session.EntryKindNote, At: heroStart}},
			wantErr: "no user entry"},
		{name: "unstamped user entry", entries: []session.Entry{{Kind: session.EntryKindUser}},
			wantErr: "carries no timestamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newAligner(&Storyboard{}, tc.entries)

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestParseSeconds(t *testing.T) {
	t.Parallel()
	if got, err := parseSeconds("3.208333"); err != nil || got != 3208333*time.Microsecond {
		t.Errorf("parseSeconds(3.208333): want 3.208333s, got %s, %v", got, err)
	}
	if _, err := parseSeconds("N/A"); err == nil {
		t.Error("parseSeconds(N/A): want an error")
	}
}

// TestFfmpegAdapters drives the real ffmpeg and ffprobe over a synthetic two-second clip that
// cuts from black to white at one second: the first paint is that cut and the duration is the
// clip's. Skipped when the tools are not on PATH.
func TestFfmpegAdapters(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	take := filepath.Join(t.TempDir(), "take.mp4")
	synth := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=1",
		"-f", "lavfi", "-i", "color=c=white:s=64x64:r=10:d=1",
		"-filter_complex", "[0][1]concat=n=2:v=1:a=0",
		"-pix_fmt", "yuv420p", take,
	)
	if out, err := synth.CombinedOutput(); err != nil {
		t.Fatalf("synthesize clip: %v\n%s", err, out)
	}
	ctx := context.Background()

	firstPaint, err := ffmpegFirstPainter{Take: take, Threshold: 0.4}.FirstPaint(ctx)
	if err != nil {
		t.Fatalf("FirstPaint: %v", err)
	}
	duration, err := takeDuration(ctx, take)
	if err != nil {
		t.Fatalf("takeDuration: %v", err)
	}

	const tolerance = 150 * time.Millisecond
	if diff := (firstPaint - time.Second).Abs(); diff > tolerance {
		t.Errorf("first paint: want ~1s, got %s", firstPaint)
	}
	if diff := (duration - 2*time.Second).Abs(); diff > tolerance {
		t.Errorf("duration: want ~2s, got %s", duration)
	}
}
