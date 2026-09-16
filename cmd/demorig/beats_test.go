package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestWriteBeats(t *testing.T) {
	t.Parallel()
	times := []BeatTime{
		{ID: 1, Title: "opens", At: 3200 * time.Millisecond, Index: noEntry},
		{ID: 2, Title: "the prompt", At: 10260 * time.Millisecond, Index: 0},
	}

	var table, asJSON bytes.Buffer
	if err := writeBeatsTable(&table, times); err != nil {
		t.Fatalf("writeBeatsTable: %v", err)
	}
	if err := writeBeatsJSON(&asJSON, times); err != nil {
		t.Fatalf("writeBeatsJSON: %v", err)
	}

	if want := "  1     3.200s  opens\n  2    10.260s  the prompt\n"; table.String() != want {
		t.Errorf("table:\nwant %q\ngot  %q", want, table.String())
	}
	var rows []beatRow
	if err := json.Unmarshal(asJSON.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, asJSON.String())
	}
	if len(rows) != 2 || rows[0] != (beatRow{ID: 1, Title: "opens", At: 3.2, Entry: -1}) || rows[1].Entry != 0 {
		t.Errorf("json rows: got %+v", rows)
	}
}
