package main

import (
	"fmt"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/session"
)

// loadEntries reads a saved session record by path and decodes its transcript into the
// committed entries, in list order. The shapes are internal/session's own — a storyboard's
// session anchors are matched against exactly what apogee wrote, never a redeclared mirror. A
// record with no transcript is an error: a take whose session recorded no scrollback has nothing
// to anchor to.
func loadEntries(path string) ([]session.Entry, error) {
	record, err := session.NewStore(filepath.Dir(path)).LoadPath(path)
	if err != nil {
		return nil, err
	}
	entries, err := session.DecodeTranscript(record.Transcript)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: the session recorded no transcript entries", path)
	}
	return entries, nil
}
