package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestSessionEncodeDecodeRoundTrip proves the Session envelope persists its opaque State
// payload verbatim across an Encode/Decode boundary.
func TestSessionEncodeDecodeRoundTrip(t *testing.T) {
	orig := Session{Version: SessionVersion, State: json.RawMessage(`{"turnIndex":3}`)}
	data, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeSession(data)
	if err != nil {
		t.Fatalf("DecodeSession: %v", err)
	}
	if got.Version != SessionVersion || string(got.State) != `{"turnIndex":3}` {
		t.Errorf("round-trip = %+v, want version %d state {\"turnIndex\":3}", got, SessionVersion)
	}
}

// TestDecodeSessionRejectsFutureVersion proves a snapshot from a newer build is rejected
// rather than silently and partially decoded (ADR 0001 — no silent forward migration).
func TestDecodeSessionRejectsFutureVersion(t *testing.T) {
	future := Session{Version: SessionVersion + 1, State: json.RawMessage(`{}`)}
	data, err := future.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := DecodeSession(data); !errors.Is(err, ErrSessionVersion) {
		t.Errorf("DecodeSession(future) err = %v, want ErrSessionVersion", err)
	}
}
