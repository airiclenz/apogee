package main

import (
	"bytes"
	"errors"
	"image/color"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// sampleTake is a small take with every cell attribute, a wide grapheme and interleaved events.
func sampleTake() *Take {
	blank := func() [][]TakeCell {
		return [][]TakeCell{
			{{Rune: " ", Width: 1}, {Rune: " ", Width: 1}, {Rune: " ", Width: 1}},
			{{Rune: " ", Width: 1}, {Rune: " ", Width: 1}, {Rune: " ", Width: 1}},
		}
	}
	painted := blank()
	painted[0][0] = TakeCell{Rune: "a", Width: 1, FG: "@9", BG: "#1e1e2e", Bold: true, Italic: true}
	painted[0][1] = TakeCell{Rune: "漢", Width: 2, Faint: true, Underline: true, Reverse: true}
	painted[0][2] = TakeCell{Rune: "", Width: 0}
	return &Take{
		Cols:    3,
		Rows:    2,
		FPS:     30,
		Started: time.Date(2026, time.September, 25, 9, 30, 0, 0, time.UTC),
		Snapshots: []Snapshot{
			{At: 0, Cursor: Cursor{Visible: true}, Cells: blank()},
			{At: 250 * time.Millisecond, Cursor: Cursor{X: 2, Y: 0}, Cells: painted},
		},
		Events: []TakeEvent{
			{At: 100 * time.Millisecond, Kind: "beat", Detail: "open"},
			{At: 250 * time.Millisecond, Kind: "type", Detail: "a漢"},
			{At: 900 * time.Millisecond, Kind: "exit"},
		},
	}
}

func TestTake_WriteReadRoundTrip(t *testing.T) {
	t.Parallel()
	want := sampleTake()
	path := filepath.Join(t.TempDir(), "hero.take")
	if err := SaveTake(path, want); err != nil {
		t.Fatalf("SaveTake: %v", err)
	}
	got, err := LoadTake(path)
	if err != nil {
		t.Fatalf("LoadTake: %v", err)
	}
	if !got.Started.Equal(want.Started) {
		t.Fatalf("started = %v, want %v", got.Started, want.Started)
	}
	got.Started, want.Started = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the take:\n got %+v\nwant %+v", got, want)
	}
}

func TestReadTake_RejectsWhatIsNotATake(t *testing.T) {
	t.Parallel()
	var sized bytes.Buffer
	if err := WriteTake(&sized, &Take{Cols: 0, Rows: 5, FPS: 30}); err != nil {
		t.Fatalf("WriteTake: %v", err)
	}
	for name, data := range map[string][]byte{
		"plain text":  []byte("not gzip at all"),
		"no terminal": sized.Bytes(),
	} {
		if _, err := ReadTake(bytes.NewReader(data)); !errors.Is(err, errTakeFormat) {
			t.Errorf("%s: err = %v, want errTakeFormat", name, err)
		}
	}
}

func TestTake_AddSnapshotDropsAnUnchangedScreen(t *testing.T) {
	t.Parallel()
	take := &Take{}
	first := sampleTake().Snapshots[1]
	if !take.addSnapshot(first) {
		t.Fatal("the first snapshot was dropped")
	}
	again := first
	again.At = time.Second
	if take.addSnapshot(again) {
		t.Fatal("an unchanged screen added a snapshot")
	}
	moved := sampleTake().Snapshots[1]
	moved.At = 2 * time.Second
	moved.Cursor.Visible = true
	if !take.addSnapshot(moved) {
		t.Fatal("a cursor change was dropped")
	}
	restyled := sampleTake().Snapshots[1]
	restyled.At = 3 * time.Second
	restyled.Cursor.Visible = true
	restyled.Cells[0][0].Bold = false
	if !take.addSnapshot(restyled) {
		t.Fatal("a style change was dropped")
	}
	if len(take.Snapshots) != 3 {
		t.Fatalf("take holds %d snapshots, want 3", len(take.Snapshots))
	}
}

func TestTakeColor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   color.Color
		want TakeColor
		rgb  color.Color
	}{
		{nil, "", nil},
		{ansi.BasicColor(1), "@1", ansi.IndexedColor(1)},
		{ansi.IndexedColor(208), "@208", ansi.IndexedColor(208)},
		{ansi.RGBColor{R: 0x1e, G: 0x1e, B: 0x2e}, "#1e1e2e", ansi.RGBColor{R: 0x1e, G: 0x1e, B: 0x2e}},
	}
	for _, c := range cases {
		got := takeColorOf(c.in)
		if got != c.want {
			t.Errorf("takeColorOf(%v) = %q, want %q", c.in, got, c.want)
		}
		if res := got.Color(); !reflect.DeepEqual(res, c.rgb) {
			t.Errorf("%q.Color() = %v, want %v", got, res, c.rgb)
		}
	}
	for _, bad := range []TakeColor{"@", "@256", "#12345", "#zzzzzz", "red"} {
		if res := bad.Color(); res != nil {
			t.Errorf("%q.Color() = %v, want nil", bad, res)
		}
	}
}

func TestTakeCellOf(t *testing.T) {
	t.Parallel()
	if got := takeCellOf(nil); got != (TakeCell{Rune: " ", Width: 1}) {
		t.Fatalf("nil cell = %+v, want a blank", got)
	}
	cell := &uv.Cell{
		Content: "x",
		Width:   1,
		Style: uv.Style{
			Fg:        ansi.BasicColor(2),
			Attrs:     uv.AttrItalic | uv.AttrReverse | uv.AttrFaint,
			Underline: ansi.UnderlineSingle,
		},
	}
	want := TakeCell{Rune: "x", Width: 1, FG: "@2", Faint: true, Italic: true, Underline: true, Reverse: true}
	if got := takeCellOf(cell); got != want {
		t.Fatalf("takeCellOf = %+v, want %+v", got, want)
	}
}
