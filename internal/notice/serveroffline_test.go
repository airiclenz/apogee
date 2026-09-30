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

// An unresolved host name is the one failure detail worded here rather than carried through
// verbatim: the refusal names the host and the two ways out, pinned verbatim as the full sentence
// a Driver shows.
func TestServerOfflineNamesAnUnresolvedHost(t *testing.T) {
	got := notice.ServerOffline("http://Apollo-II.local:1111", notice.UnresolvedHost("Apollo-II.local"))

	want := "cannot send — server offline (http://Apollo-II.local:1111): host name Apollo-II.local did not " +
		"resolve — use the server's IP address or add it to /etc/hosts"
	if got != want {
		t.Errorf("refusal = %q, want %q", got, want)
	}
}
