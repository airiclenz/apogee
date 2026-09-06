package notice_test

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/notice"
)

// The sentence is pinned verbatim because all three Drivers put this exact line in front of a
// human — the TUI as a transcript note on a rebind, headless on stderr, the daemon in its journal —
// and the whole reason it lives here is that the three cannot drift apart.
func TestWindowUnknownIsTheOneSpelling(t *testing.T) {
	want := "context window unknown — automatic compaction and the Budget are inactive; " +
		"set context-window: in config.yaml"
	if notice.WindowUnknown != want {
		t.Errorf("WindowUnknown = %q, want %q", notice.WindowUnknown, want)
	}
}

// The line has to name the remedy as a reader can act on it: the config key, spelled as it is
// written on disk. A sentence that said only that something is inactive would leave its reader
// with nothing to do about it — and the unattended Drivers have no settings pane to point at.
func TestWindowUnknownNamesTheKeyThatFixesIt(t *testing.T) {
	if !strings.Contains(notice.WindowUnknown, "context-window:") {
		t.Errorf("WindowUnknown = %q; it must name the config key that answers it", notice.WindowUnknown)
	}
}
