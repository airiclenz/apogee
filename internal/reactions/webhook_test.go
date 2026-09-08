package reactions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestWebhookSenderPostsTheJSONWithBothKindsOfHeader is the round trip the webhook half exists
// for: the endpoint sees a POST of exactly the payload document, apogee's own two headers, the
// entry's literal headers, and the `headers-env:` ones read out of the environment at send time.
func TestWebhookSenderPostsTheJSONWithBothKindsOfHeader(t *testing.T) {
	t.Setenv("APOGEE_HOOK_TEST_TOKEN", "s3cr3t")

	type received struct {
		method      string
		contentType string
		userAgent   string
		literal     string
		fromEnv     string
		body        string
	}
	var got received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = received{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			userAgent:   r.Header.Get("User-Agent"),
			literal:     r.Header.Get("X-Source"),
			fromEnv:     r.Header.Get("Authorization"),
			body:        string(body),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	payload := Payload{
		Event:     ExchangeFinished,
		Hook:      "ping",
		Time:      "2026-09-06T12:00:00Z",
		Workspace: "/work/space",
		Status:    "completed",
	}
	hook := domain.Reaction{
		ID:     "ping",
		Origin: domain.OriginUser,
		Class:  domain.ClassObserve,
		On:     []Event{ExchangeFinished},
		Handler: domain.WebhookHandler{
			URL:        server.URL + "/fire",
			Headers:    map[string]string{"X-Source": "apogee-test"},
			HeadersEnv: map[string]string{"Authorization": "APOGEE_HOOK_TEST_TOKEN"},
		},
		Timeout: 10 * time.Second,
	}

	// DefaultExecutor rather than webhookSender directly, so the dispatch on `webhook:` is
	// exercised by the same test that proves what the endpoint receives.
	if err := DefaultExecutor("").Run(context.Background(), hook, payload); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal the payload: %v", err)
	}
	want := received{
		method:      http.MethodPost,
		contentType: "application/json",
		userAgent:   "apogee",
		literal:     "apogee-test",
		fromEnv:     "s3cr3t",
		body:        string(wantBody),
	}
	if got != want {
		t.Errorf("the endpoint received %+v, want %+v", got, want)
	}
}

// TestWebhookSenderReportsANonSuccessStatus proves a refused POST becomes a failure line rather
// than silence — a Hook whose endpoint answers 500 every time is a Hook that is not working.
func TestWebhookSenderReportsANonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the gateway is unwell", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	hook := domain.Reaction{
		ID:      "ping",
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []Event{ExchangeFinished},
		Handler: domain.WebhookHandler{URL: server.URL},
		Timeout: 10 * time.Second,
	}

	err := webhookSender{}.Run(context.Background(), hook, Payload{Event: ExchangeFinished})
	if err == nil {
		t.Fatal("Run against a 503 returned no error")
	}
	if got := err.Error(); got != "HTTP 503" {
		t.Errorf("error = %q, want %q", got, "HTTP 503")
	}
}

// TestWebhookSenderRefusesToSendWhenTheHeaderVariableIsUnset is the whole point of `headers-env:`
// working out at SEND time: an unauthenticated POST would reach the endpoint and be refused there,
// for a reason the user could never see from apogee. It fails here instead, naming the header and
// the variable — and never the value, which is a secret.
func TestWebhookSenderRefusesToSendWhenTheHeaderVariableIsUnset(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	hook := domain.Reaction{
		ID:     "ping",
		Origin: domain.OriginUser,
		Class:  domain.ClassObserve,
		On:     []Event{ExchangeFinished},
		Handler: domain.WebhookHandler{
			URL:        server.URL,
			HeadersEnv: map[string]string{"Authorization": "APOGEE_HOOK_TEST_ABSENT_TOKEN"},
		},
		Timeout: 10 * time.Second,
	}

	err := webhookSender{}.Run(context.Background(), hook, Payload{Event: ExchangeFinished})
	if err == nil {
		t.Fatal("Run with an unset header variable returned no error")
	}
	for _, want := range []string{"Authorization", "APOGEE_HOOK_TEST_ABSENT_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("the endpoint was called %d times; it must not be reached at all", hits.Load())
	}
}

// TestWebhookSenderReportsTheDeadlineOnAStalledEndpoint is the bound that keeps an endpoint which
// accepts the connection and then says nothing from holding a Hook's worker forever.
func TestWebhookSenderReportsTheDeadlineOnAStalledEndpoint(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	// The order matters: Close waits for the handler to return, so the handler has to be let go
	// FIRST — and a defer registered later runs earlier.
	defer server.Close()
	defer close(release)

	hook := domain.Reaction{
		ID:      "ping",
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []Event{ExchangeFinished},
		Handler: domain.WebhookHandler{URL: server.URL},
		Timeout: 150 * time.Millisecond,
	}

	started := time.Now()
	err := webhookSender{}.Run(context.Background(), hook, Payload{Event: ExchangeFinished})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Run against a stalled endpoint returned no error")
	}
	if got := err.Error(); got != "timed out after 150ms" {
		t.Errorf("error = %q, want %q", got, "timed out after 150ms")
	}
	if elapsed > time.Second {
		t.Errorf("Run took %s to give up on a 150ms timeout", elapsed)
	}
}
