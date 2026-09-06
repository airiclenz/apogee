package notice_test

import (
	"testing"

	"github.com/airiclenz/apogee/internal/notice"
)

// The refusal names the endpoint and, when the beat had words for why, those too. The sentence is
// pinned verbatim because three Drivers put this exact line in front of a human.
func TestServerOfflineNamesTheEndpointAndTheFailure(t *testing.T) {
	got := notice.ServerOffline("http://box.invalid:1111", "connection refused")

	want := "cannot send — server offline (http://box.invalid:1111): connection refused"
	if got != want {
		t.Errorf("refusal = %q, want %q", got, want)
	}
}

// Nothing observed and nothing to say about it: the endpoint alone, and no dangling colon after
// it — the endpoint is the one fact a reader of an unattended log can act on.
func TestServerOfflineWithoutAFailureNamesTheEndpointAlone(t *testing.T) {
	got := notice.ServerOffline("http://box.invalid:1111", "")

	want := "cannot send — server offline (http://box.invalid:1111)"
	if got != want {
		t.Errorf("refusal = %q, want %q", got, want)
	}
}
