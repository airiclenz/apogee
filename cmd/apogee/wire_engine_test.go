package main

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee"
)

func TestFriendlyConstructErr(t *testing.T) {
	t.Parallel()

	if got := friendlyConstructErr(apogee.ErrAutoUnavailable); !errors.Is(got, errAutoUnavailable) {
		t.Errorf("friendlyConstructErr(ErrAutoUnavailable) = %v; want errAutoUnavailable", got)
	}

	other := errors.New("some other failure")
	if got := friendlyConstructErr(other); !errors.Is(got, other) {
		t.Errorf("friendlyConstructErr(other) = %v; want passthrough", got)
	}
}

// TestLateEngineInterjectChildRefusesUnbound pins the pre-bound half of the new seam: a session
// that has not chosen a server has no Agent, and so no child tree to reach into. The holder must
// name the way out rather than panic on a nil Agent — the same refusal every other
// conversation-touching call answers with (ADR 0036).
func TestLateEngineInterjectChildRefusesUnbound(t *testing.T) {
	t.Parallel()

	var engine lateEngine
	err := engine.InterjectChild("call-1", apogee.UserInput{Text: "check the docs too"})
	if !errors.Is(err, errNoServerBound) {
		t.Errorf("InterjectChild err = %v; want errNoServerBound", err)
	}
}
