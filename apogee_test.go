package apogee_test

// Black-box public-API tests (P0.6e): the validation and session paths that the
// public surface exercises without a fake Responder. This package is external
// (apogee_test) precisely because the Auto-gate test injects platform.NewDenyConfiner,
// and internal/platform imports the root apogee package — an internal test package
// could not import it without an import cycle. The fake-Responder capstone lives in the
// white-box harness (harness_internal_test.go).

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/platform"
)

type nopSink struct{}

func (nopSink) Emit(apogee.Event) {}

func validConfig() apogee.Config {
	return apogee.Config{Endpoint: "http://localhost:0", Model: "test-model", Events: nopSink{}}
}

// ---------------------------------------------------------------------------

func TestNew_AutoModeGate(t *testing.T) {
	// Under ADR 0012 the Auto construction gate is CONDITIONAL: a NIL Confiner — no
	// confinement facility injected at all — is refused (ErrAutoUnavailable); a PRESENT
	// but incapable Confiner (deny-all: no fs-confinement on this host) is NOT refused —
	// Auto is entered and the subprocess surface gates through Approval ("confine if you
	// can, gate if you can't"). This reverses ADR 0004's refuse-deny-all behaviour.
	tests := []struct {
		name     string
		confiner apogee.Confiner
		wantErr  bool
	}{
		{name: "auto with no confiner is refused", confiner: nil, wantErr: true},
		{name: "auto with deny-all confiner enters Auto (subprocess gates)", confiner: platform.NewDenyConfiner(), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Mode = apogee.ModeAuto
			cfg.Confiner = tt.confiner

			_, err := apogee.New(cfg)

			if tt.wantErr {
				if !errors.Is(err, apogee.ErrAutoUnavailable) {
					t.Errorf("New err = %v, want ErrAutoUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Errorf("New err = %v, want nil (deny-all confiner enters Auto, subprocess gates)", err)
			}
		})
	}
}

func TestNew_NonAutoModeNeedsNoConfiner(t *testing.T) {
	cfg := validConfig()
	cfg.Mode = apogee.ModeAskBefore

	if _, err := apogee.New(cfg); err != nil {
		t.Errorf("New(ask-before, no confiner) = %v, want nil", err)
	}
}

func TestNew_RequiresMinimumConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apogee.Config)
	}{
		{name: "missing Events", mutate: func(c *apogee.Config) { c.Events = nil }},
		{name: "missing Endpoint", mutate: func(c *apogee.Config) { c.Endpoint = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)

			if _, err := apogee.New(cfg); err == nil {
				t.Error("New = nil error, want a validation error")
			}
		})
	}
}

// TestNew_ModelMayBeBoundLater pins the construction relaxation async startup needs (ADR 0024):
// Config.Model is no longer part of the minimum surface, so a host that starts before its
// Upstream answers constructs anyway and binds the observed model later through Rebind. The
// engine's own guard moved to Submit, which refuses while nothing is bound.
func TestNew_ModelMayBeBoundLater(t *testing.T) {
	cfg := validConfig()
	cfg.Model = ""

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with an empty Model = %v, want nil", err)
	}
	defer func() { _ = agent.Close() }()

	if err := agent.Submit(apogee.UserInput{Text: "too early"}); err == nil {
		t.Error("Submit with no model bound = nil error, want a refusal")
	}
	if err := agent.Rebind(apogee.RebindSpec{Model: "late-bound"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if err := agent.Submit(apogee.UserInput{Text: "now it flows"}); err != nil {
		t.Errorf("Submit after Rebind = %v, want nil", err)
	}
}

func TestSession_RoundTrip(t *testing.T) {
	agent, err := apogee.New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap, err := agent.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	encoded, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := apogee.DecodeSession(encoded)
	if err != nil {
		t.Fatalf("DecodeSession: %v", err)
	}

	if decoded.Version != snap.Version {
		t.Errorf("round-trip Version = %d, want %d", decoded.Version, snap.Version)
	}
}

func TestDecodeSession_FutureVersion(t *testing.T) {
	// A version far beyond any near-term schema is from a newer build → rejected.
	future := []byte(`{"Version":999,"State":null}`)

	if _, err := apogee.DecodeSession(future); !errors.Is(err, apogee.ErrSessionVersion) {
		t.Errorf("DecodeSession err = %v, want ErrSessionVersion", err)
	}
}

func TestResume_FutureVersion(t *testing.T) {
	if _, err := apogee.Resume(validConfig(), apogee.Session{Version: 999}); !errors.Is(err, apogee.ErrSessionVersion) {
		t.Errorf("Resume err = %v, want ErrSessionVersion", err)
	}
}

// TestNew_InvalidReaction_MatchableThroughRoot proves the Reaction arming refusal is matchable
// through the root re-export: an embedder outside the module cannot import internal/domain
// (ADR 0010), so apogee.ErrInvalidReaction must BE the sentinel New wraps when Config.Reactions
// carries a reaction the engine will not accept. Two cases, one per handler kind: a Go handler
// whose On list names a Moment it cannot serve, and an argv handler pointed at a seam — the
// async lane reacts to notices only. Both are the mistake a host makes by hand, refused at
// construction rather than silently never firing.
func TestNew_InvalidReaction_MatchableThroughRoot(t *testing.T) {
	cfg := validConfig()
	cfg.Reactions = []apogee.Reaction{{
		ID:     "wrong-seam",
		Origin: apogee.OriginEngine,
		Class:  apogee.ClassObserve,
		On:     []apogee.Moment{apogee.MomentPostResponse},
		Handler: apogee.PreRequestFunc(func(context.Context, *apogee.Request) (apogee.Outcome, error) {
			return apogee.Outcome{}, nil
		}),
	}}

	if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrInvalidReaction) {
		t.Errorf("New(mis-seamed reaction) err = %v, want ErrInvalidReaction", err)
	}

	cfg = validConfig()
	cfg.Reactions = []apogee.Reaction{{
		ID:      "notify-on-a-seam",
		Origin:  apogee.OriginUser,
		Class:   apogee.ClassObserve,
		On:      []apogee.Moment{apogee.MomentPreRequest},
		Handler: apogee.ArgvHandler{Argv: []string{"/usr/bin/notify"}},
	}}

	if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrInvalidReaction) {
		t.Errorf("New(argv reaction on a seam) err = %v, want ErrInvalidReaction", err)
	}
}

// TestInterjectChild_NoSuchChildMatchableThroughRoot proves the child-addressing refusal is
// matchable through the root re-export: an embedder outside the module cannot import
// internal/domain (ADR 0010), so apogee.ErrNoSuchChild must BE the sentinel InterjectChild
// returns. A spawn call-ID naming no running sub-agent is the refusal's own case.
func TestInterjectChild_NoSuchChildMatchableThroughRoot(t *testing.T) {
	a, err := apogee.New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = a.InterjectChild("no-such-call-id", apogee.UserInput{Text: "hello"})
	if !errors.Is(err, apogee.ErrNoSuchChild) {
		t.Errorf("InterjectChild(unknown id) err = %v, want ErrNoSuchChild", err)
	}
}

// TestFacadeExportsEventLines drives the Event lines through the ROOT package alone: an embedder
// that never imports internal/* must be able to build the same documented JSONL an
// `apogee headless --format json` run writes. It asserts the whole re-export — the type, its
// options, both frame structs and the forwarding constructor — by producing a real bracketed
// stream, which no compile-time alias reference on its own would prove.
func TestFacadeExportsEventLines(t *testing.T) {
	var out bytes.Buffer

	lines := apogee.NewEventLines(&out, apogee.EventLinesOptions{
		Session: "sess-1",
		Now:     func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) },
	})
	sink := lines.Wrap(nopSink{})
	lines.RunStarted(apogee.RunStarted{Session: "sess-1", Mode: "plan"})
	sink.Emit(apogee.MessageEvent{Text: "hi"})
	lines.RunFinished(apogee.RunFinished{ExitCode: 0, Turns: 1, Saved: true})

	got := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	wantKinds := []string{"run_started", "message", "run_finished"}
	if len(got) != len(wantKinds) {
		t.Fatalf("wrote %d lines, want %d:\n%s", len(got), len(wantKinds), out.String())
	}
	for i, kind := range wantKinds {
		if !strings.HasPrefix(got[i], `{"event":"`+kind+`","v":2,`) {
			t.Errorf("line %d is not a v:2 %s line: %s", i+1, kind, got[i])
		}
	}
}
