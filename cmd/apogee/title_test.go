package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/title"
)

// stalledReplyBackstop bounds the stalled server below. Nothing under test should reach it — every
// call is abandoned within milliseconds — so it exists purely so a broken deadline leaves a failed
// assertion rather than a hung suite.
const stalledReplyBackstop = 5 * time.Second

// titleRequest is the part of the naming call's body the assertions read back: which model it was
// addressed to, and what the model was actually asked.
type titleRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Authorization string
}

// titleServer stands in for an OpenAI-compatible server answering one naming call with reply. It
// records the request it received so the test can prove which binding built it.
func titleServer(t *testing.T, reply string) (*httptest.Server, *titleRequest) {
	t.Helper()
	var got titleRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode naming request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got.Authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		body, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": reply}}},
		})
		if err != nil {
			t.Errorf("marshal stub reply: %v", err)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// The generator's whole job: one round-trip to the server the session is bound to RIGHT NOW,
// returning the reply untouched. The second half is why the client is built per call rather than
// held — a `/server` switch (or a rebind) moves the binding, and the very next naming call must
// follow it without any seam changing shape.
func TestTitleGeneratorFollowsTheCurrentBinding(t *testing.T) {
	t.Parallel()

	first, firstReq := titleServer(t, "fix the flaky parser test")
	second, secondReq := titleServer(t, "rename the session browser rows")

	binding := upstreamBinding{Endpoint: first.URL, Model: "model-a", APIKey: "key-a"}
	wiring := newTitleWiring(func() upstreamBinding { return binding }, "/home/dev/apogee")

	got, err := wiring.generate(context.Background(), "the parser test fails every other run")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if want := "fix the flaky parser test"; got != want {
		t.Errorf("generate = %q; want the stubbed reply %q", got, want)
	}
	if firstReq.Model != "model-a" {
		t.Errorf("request model = %q; want the bound model-a — the call was not addressed to the binding", firstReq.Model)
	}
	if firstReq.Authorization != "Bearer key-a" {
		t.Errorf("request Authorization = %q; want the binding's key — a keyed server would refuse the naming call",
			firstReq.Authorization)
	}
	// The workspace basename and the first prompt ride as CONTEXT; the browser row shows the
	// workspace itself, so this proves the call was built from this session's facts, not that the
	// title may repeat them.
	user := firstReq.Messages[len(firstReq.Messages)-1].Content
	if !strings.Contains(user, "apogee") || !strings.Contains(user, "the parser test fails every other run") {
		t.Errorf("user message = %q; want the workspace basename and the first prompt", user)
	}

	binding = upstreamBinding{Endpoint: second.URL, Model: "model-b", APIKey: "key-b"}

	got, err = wiring.generate(context.Background(), "the browser rows are unreadable")
	if err != nil {
		t.Fatalf("generate after the switch: %v", err)
	}
	if want := "rename the session browser rows"; got != want {
		t.Errorf("generate after the switch = %q; want %q — the call did not follow the new binding", got, want)
	}
	if secondReq.Model != "model-b" || secondReq.Authorization != "Bearer key-b" {
		t.Errorf("second request = model %q, auth %q; want model-b and the new server's key",
			secondReq.Model, secondReq.Authorization)
	}
}

// A naming call that never lands must fail as an error the caller can drop, not hang the Cmd
// goroutine that fired it — from either bound: the caller's own context, or the wiring's
// request timeout (titleRequestTimeout in production, generous because a queued call is expected).
func TestTitleGeneratorSurfacesADeadline(t *testing.T) {
	t.Parallel()

	// Answers nothing until the request is abandoned, so the only outcome available is a deadline.
	// The body is drained first because that is what puts the server back on the connection, where
	// it can notice the client giving up and cancel the request context; the timer is the backstop
	// that keeps a FAILING assertion from hanging the suite (httptest's Close waits for handlers).
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(stalledReplyBackstop):
		}
	}))
	t.Cleanup(srv.Close)

	binding := func() upstreamBinding { return upstreamBinding{Endpoint: srv.URL, Model: "model-a"} }

	t.Run("the caller's context", func(t *testing.T) {
		t.Parallel()
		wiring := newTitleWiring(binding, "/home/dev/apogee")
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		got, err := wiring.generate(ctx, "name this session")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("generate = (%q, %v); want a context deadline error", got, err)
		}
	})

	t.Run("the wiring's request timeout", func(t *testing.T) {
		t.Parallel()
		wiring := newTitleWiring(binding, "/home/dev/apogee")
		wiring.requestTimeout = 30 * time.Millisecond

		got, err := wiring.generate(context.Background(), "name this session")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("generate = (%q, %v); want a context deadline error from the request timeout", got, err)
		}
	})
}

// One naming call is ONE POST. The Client's default policy re-POSTs a faulted attempt twice, which
// on a single-slot server would spend three queue slots — each bounded by the generous
// titleRequestTimeout — ahead of the user's next Exchange, all for a cosmetic result the caller drops
// the moment it fails. The wiring therefore turns retries off, and this is what pins that.
func TestTitleGeneratorDoesNotRetry(t *testing.T) {
	t.Parallel()

	// 503 is retryable under the default policy: the one answer that would draw a second POST out of
	// a client that still had a retry budget, and so the only one that can prove the budget is gone.
	var attempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	binding := upstreamBinding{Endpoint: srv.URL, Model: "model-a"}
	wiring := newTitleWiring(func() upstreamBinding { return binding }, "/home/dev/apogee")

	got, err := wiring.generate(context.Background(), "name this session")
	if err == nil {
		t.Fatalf("generate = (%q, nil); want the 503 surfaced as an error the caller can drop", got)
	}
	if n := attempts.Load(); n != 1 {
		t.Errorf("server saw %d naming POSTs; want exactly 1 — a retried title re-enters the queue", n)
	}
}

// The composition root always hands the TUI a naming seam: `auto-title:` gates only the AUTOMATIC
// firing, so a bare `/rename` can still regenerate on demand with the toggle off (Ratified design 7,
// 2026-07-31 grill). The gate itself travels as its own flag.
func TestRunRootWiresTheTitleSeam(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		autoTitle bool
	}{
		{name: "auto-title on", autoTitle: true},
		{name: "auto-title off", autoTitle: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := options{
				endpoint:  "http://127.0.0.1:1111",
				model:     "fake",
				mode:      "ask-before",
				workspace: t.TempDir(),
				autoTitle: tc.autoTitle,
			}

			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.GenerateTitle == nil {
				t.Error("opts.GenerateTitle is nil; want the seam wired regardless of the auto-title gate")
			}
			if rec.opts.AutoTitle != tc.autoTitle {
				t.Errorf("opts.AutoTitle = %v; want %v — the config gate did not reach the renderer",
					rec.opts.AutoTitle, tc.autoTitle)
			}
		})
	}
}

// The live half: a scripted server proves the plumbing, never that a real model answers a naming
// prompt with something the sanitizer accepts. Opt-in on APOGEE_LIVE_ENDPOINT exactly like
// TestE2ELiveModel, so the default suite stays offline and deterministic:
//
//	APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -count=1 -run TestLiveGenerateTitle -v ./cmd/apogee/
//
// APOGEE_LIVE_MODEL pins the model; left empty, the call is addressed to whatever the server is
// serving — the same "no model bound yet" case a cold start has.
func TestLiveGenerateTitle(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live title generation")
	}

	binding := upstreamBinding{Endpoint: endpoint, Model: os.Getenv("APOGEE_LIVE_MODEL")}
	wiring := newTitleWiring(func() upstreamBinding { return binding }, "/home/dev/apogee")

	ctx, cancel := context.WithTimeout(context.Background(), titleRequestTimeout)
	defer cancel()

	raw, err := wiring.generate(ctx,
		"The session browser truncates long titles mid-word. Fix the width calculation in the row "+
			"renderer and add a test for a CJK title.")
	if err != nil {
		t.Fatalf("live naming call against %s: %v", endpoint, err)
	}
	t.Logf("raw reply: %q", raw)

	got, ok := title.Sanitize(raw)
	if !ok {
		t.Fatalf("title.Sanitize rejected the live reply %q — nothing usable survived the pipeline", raw)
	}
	t.Logf("title: %q", got)
}
