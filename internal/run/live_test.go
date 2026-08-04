package run

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/session"
)

// TestRunOnceLive drives ONE real Plan-mode Firing end to end against a live
// OpenAI-compatible server: a fresh Agent, a real prompt, a record on disk. Every other test
// in this package scripts the Upstream, so this is the only proof that the whole path — the
// provider client, the loop, the snapshot, the store — actually holds against a real model.
//
// It is opt-in, gated on APOGEE_LIVE_ENDPOINT exactly like the other live tests in this
// repo, so `make check` never depends on a running model:
//
//	APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -race -count=1 \
//	    -run TestRunOnceLive -v ./internal/run/
//
// -count=1 is load-bearing: the live server's loaded model is not a Go-visible input, so
// caching would replay a stale PASS across a model swap. APOGEE_LIVE_MODEL pins the model
// (else it is discovered) and APOGEE_API_KEY carries the bearer token for a keyed server.
func TestRunOnceLive(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live firing")
	}
	apiKey := os.Getenv("APOGEE_API_KEY")

	model := os.Getenv("APOGEE_LIVE_MODEL")
	if model == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := provider.NewClient(endpoint, "", provider.WithAPIKey(apiKey)).Discover(ctx)
		if err != nil {
			t.Fatalf("discover model at %s: %v", endpoint, err)
		}
		model = info.ActiveModel
	}
	t.Logf("live firing: endpoint=%s model=%s", endpoint, model)

	store := session.NewStore(t.TempDir())
	spec := Spec{
		Config: domain.Config{
			Endpoint:     endpoint,
			Model:        model,
			APIKey:       apiKey,
			Mode:         domain.ModePlan,
			WorkspaceDir: t.TempDir(),
		},
		Prompt:       "Reply with exactly one word: ready",
		ScheduleID:   "live-schedule",
		ScheduleName: "Live check",
		Store:        store,
	}

	// The single-slot local server can be slow to first token (ADR 0024's posture: generous
	// patience, no retries hammering the slot), so the ceiling is wide — it exists to fail
	// the test rather than hang the package.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := Once(ctx, spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.SessionID == "" {
		t.Fatal("Result.SessionID is empty; the live firing persisted no record")
	}
	if res.Turns < 1 {
		t.Errorf("Result.Turns = %d, want at least 1", res.Turns)
	}
	if !strings.HasPrefix(res.Title, "Live check — ") {
		t.Errorf("Result.Title = %q, want the derived schedule form", res.Title)
	}

	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load the live firing's record: %v", err)
	}
	if rec.Meta.ScheduleID != "live-schedule" || rec.Meta.ScheduleName != "Live check" {
		t.Errorf("record schedule identity = (%q, %q), want (%q, %q)",
			rec.Meta.ScheduleID, rec.Meta.ScheduleName, "live-schedule", "Live check")
	}
	if rec.Meta.Model != model {
		t.Errorf("record model = %q, want %q", rec.Meta.Model, model)
	}
	if len(rec.Session.State) == 0 {
		t.Error("the record carries no engine state; the conversation was not snapshotted")
	}
}
