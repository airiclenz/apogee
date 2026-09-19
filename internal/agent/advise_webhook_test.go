package agent

// The USER half of the advise slot over a WEBHOOK (ADR 0076 D2, D6): an `advise:` entry's `run:
// url:` mapping is POSTed the seam document while the loop waits, and the reply body reaches the
// model as the fenced trailer on the closing tool result. These tests drive it through the same
// seam the argv canaries do (firePostToolResult → appendToolResult), because what is under test is
// the route's decisions around the request — which document the endpoint receives, what the reply
// is redacted and bounded to, when the request is sent at all, and what a failure costs the Turn —
// and not the POST itself, which internal/webhook's tests own.

import (
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

// userAdviseWebhook is one user-origin advise entry over a webhook, as the `reactions:` file will
// resolve it once the config accepts the mapping: the Moments it subscribed to and the URL it
// POSTs to, with the class default deadline.
func userAdviseWebhook(id string, on []domain.Moment, handler domain.WebhookHandler) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassAdvise,
		On:      on,
		Handler: handler,
	}
}

// replyWith is an endpoint that answers every POST with status and body, counting the hits.
func replyWith(t *testing.T, hits *atomic.Int64, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// The round trip: the endpoint receives the post-tool-result document with the identity half
// stamped and the header read from the environment, and its reply body is the trailer the model
// reads and the firing books.
func TestAdviseWebhookInjectsTheReplyBody(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_TOKEN", "s3cr3t")

	var got domain.SeamPayload
	var contentType, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "consider running the tests\n")
	}))
	defer server.Close()
	sink := &recordingSink{}
	var reported []string
	a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported,
		userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, domain.WebhookHandler{
			URL:        server.URL,
			HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_ADVISE_TOKEN"},
		}))

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if contentType != "application/json" || authorization != "s3cr3t" {
		t.Errorf("headers = %q / %q, want application/json and the token read from the environment",
			contentType, authorization)
	}
	if got.Event != domain.MomentPostToolResult || got.Tool != "list_dir" || got.Reaction != "coach" ||
		got.Workspace != a.cfg.WorkspaceDir || got.Time == "" {
		t.Errorf("the endpoint received %+v, want the stamped post-tool-result document", got)
	}
	if !strings.Contains(msg.Content, "consider running the tests") || len(msg.Advice) != 1 {
		t.Errorf("advised content = %q with %d spans, want the reply fenced onto the result", msg.Content, len(msg.Advice))
	}
	fired := firedAdvice(sink)
	if len(fired) != 1 || fired[0].Detail != "consider running the tests\n" {
		t.Errorf("firings = %+v, want one advise firing carrying the reply", fired)
	}
	if len(reported) != 0 {
		t.Errorf("reporter said %q, want nothing on a successful request", reported)
	}
}

// An empty reply is the entry having nothing to say: no fence, no firing of any kind — the same
// silence a command that printed nothing buys.
func TestAdviseWebhookAnswersNothingOnAnEmptyReply(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := replyWith(t, &hits, http.StatusNoContent, "")
	sink := &recordingSink{}
	var reported []string
	a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported,
		userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, domain.WebhookHandler{URL: server.URL}))

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if hits.Load() != 1 {
		t.Fatalf("the endpoint was called %d times, want 1", hits.Load())
	}
	if len(msg.Advice) != 0 || msg.Content != result.Content {
		t.Errorf("message = %q with %d spans, want the bare result", msg.Content, len(msg.Advice))
	}
	if fired := firedAdvice(sink); len(fired) != 0 {
		t.Errorf("firings = %+v, want none for an empty reply", fired)
	}
}

// The reply is read one byte past the advice cap and no further, so the downstream cap still sees
// an over-long reply and marks the truncation while a server that streams without end holds
// nothing of the Turn's memory.
func TestAdviseWebhookBoundsTheReplyAtTheAdviceCap(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := replyWith(t, &hits, http.StatusOK, strings.Repeat("a", 4*domain.AdviceCap))
	sink := &recordingSink{}
	var reported []string
	a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported,
		userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, domain.WebhookHandler{URL: server.URL}))

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if len(msg.Advice) != 1 {
		t.Fatalf("message carries %d spans, want the one capped trailer", len(msg.Advice))
	}
	if !strings.Contains(msg.Content, "[advice truncated at 8 KiB]") {
		t.Error("advised content carries no truncation marker for a reply four times the cap")
	}
	if len(msg.Content) > len(result.Content)+2*domain.AdviceCap {
		t.Errorf("advised content is %d bytes; the reply was not bounded near the cap", len(msg.Content))
	}
}

// The request failing in any way — a refused status, an unset `headers-env:` variable, a dead
// endpoint, a stalled one — costs the Turn nothing: the bare result is committed, one "failed"
// firing is booked and one operator line is reported, on the same terms as a command that failed.
func TestAdviseWebhookFailsOpenOnNon2xx(t *testing.T) {
	cases := []struct {
		name    string
		handler func(t *testing.T) domain.WebhookHandler
		timeout time.Duration
		detail  string
	}{
		{
			name: "a non-2xx status",
			handler: func(t *testing.T) domain.WebhookHandler {
				return domain.WebhookHandler{URL: replyWith(t, new(atomic.Int64), http.StatusServiceUnavailable, "advice").URL}
			},
			detail: "HTTP 503",
		},
		{
			name: "an unset headers-env variable",
			handler: func(t *testing.T) domain.WebhookHandler {
				return domain.WebhookHandler{
					URL:        replyWith(t, new(atomic.Int64), http.StatusOK, "advice").URL,
					HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_ADVISE_ABSENT_TOKEN"},
				}
			},
			detail: "APOGEE_TEST_ADVISE_ABSENT_TOKEN is not set",
		},
		{
			name: "a dead endpoint",
			handler: func(t *testing.T) domain.WebhookHandler {
				server := httptest.NewServer(http.NotFoundHandler())
				server.Close()
				return domain.WebhookHandler{URL: server.URL}
			},
			detail: "POST failed",
		},
		{
			name: "an endpoint that outlives the deadline",
			handler: func(t *testing.T) domain.WebhookHandler {
				release := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}))
				// Close waits for the handler, so the handler is released FIRST — a later defer
				// runs earlier.
				t.Cleanup(server.Close)
				t.Cleanup(func() { close(release) })
				return domain.WebhookHandler{URL: server.URL}
			},
			timeout: 150 * time.Millisecond,
			detail:  "timed out after 150ms",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			var reported []string
			entry := userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, tc.handler(t))
			entry.Timeout = tc.timeout
			a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported, entry)

			call, result := readCall()
			msg := adviseArgvCall(t, a, call, result)

			if len(msg.Advice) != 0 || msg.Content != result.Content {
				t.Errorf("message = %q with %d spans, want the bare result", msg.Content, len(msg.Advice))
			}
			fired := firedAdvice(sink)
			if len(fired) != 1 || fired[0].Action != actionFailed {
				t.Fatalf("firings = %+v, want exactly one booked failed", fired)
			}
			if !strings.Contains(fired[0].Detail, tc.detail) {
				t.Errorf("Detail = %q, want it to name %q", fired[0].Detail, tc.detail)
			}
			if len(reported) != 1 || !strings.HasPrefix(reported[0], "reaction coach (post-tool-result): ") {
				t.Errorf("reporter said %q, want one observe-lane sentence", reported)
			}
		})
	}
}

// The class default is what an entry that set no `timeout:` runs under, for a webhook exactly as
// for a command (ADR 0076 D7); pinned on the value rather than by a stalled endpoint.
func TestAdviseWebhookTakesTheAdviseClassDeadline(t *testing.T) {
	t.Parallel()

	entry := userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult},
		domain.WebhookHandler{URL: "https://example.test/advise"})
	if got := syncTimeout(entry); got != domain.DefaultAdviseTimeout {
		t.Errorf("syncTimeout = %s, want the advise class default %s", got, domain.DefaultAdviseTimeout)
	}
}

// A reply is redacted before it is anything else: a configured secret's value coming back from a
// server the request carried it to never reaches the model, the transcript or the firing's Detail.
func TestAdviseWebhookRedactsTheReply(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_SECRET", "hunter2-the-real-one")

	var hits atomic.Int64
	server := replyWith(t, &hits, http.StatusOK, "token=hunter2-the-real-one\n")
	sink := &recordingSink{}
	var reported []string
	a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported,
		userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, domain.WebhookHandler{URL: server.URL}))
	a.cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_SECRET"}

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if strings.Contains(msg.Content, "hunter2-the-real-one") {
		t.Errorf("advised content = %q, want the secret's value replaced", msg.Content)
	}
	if !strings.Contains(msg.Content, "token=[redacted]") {
		t.Errorf("advised content = %q, want it to carry token=[redacted]", msg.Content)
	}
	fired := firedAdvice(sink)
	if len(fired) != 1 || fired[0].Detail != "token=[redacted]\n" {
		t.Errorf("firings = %+v, want one advise firing whose Detail is already redacted", fired)
	}
}

// Bypass switches the model-shaping half of the surface off (D9), and for a webhook "off" means the
// request is never SENT — the hit counter is how the two readings are told apart.
func TestAdviseWebhookIsSkippedUnderBypass(t *testing.T) {
	t.Parallel()

	send := func(t *testing.T, bypass bool) int64 {
		t.Helper()

		var hits atomic.Int64
		server := replyWith(t, &hits, http.StatusOK, "advice\n")
		sink := &recordingSink{}
		var reported []string
		a := adviseArgvAgent(t, sink, t.TempDir(), bypass, &reported,
			userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult}, domain.WebhookHandler{URL: server.URL}))

		call, result := readCall()
		adviseArgvCall(t, a, call, result)
		return hits.Load()
	}

	t.Run("Bypass off sends it", func(t *testing.T) {
		t.Parallel()
		if got := send(t, false); got != 1 {
			t.Errorf("the endpoint was called %d times, want 1", got)
		}
	})
	t.Run("Bypass on never sends it", func(t *testing.T) {
		t.Parallel()
		if got := send(t, true); got != 0 {
			t.Errorf("the endpoint was called %d times under Bypass, want 0", got)
		}
	})
}
