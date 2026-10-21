package agent

// A USER `advise:` entry on session-started (ADR 0076 D6, 2026-10-21 admission amendment): the
// entry's command or webhook runs through the session-start lane's sync-lane executor, and its
// answer lands fenced ahead of the session's first opening — redacted, capped, fail-open, off under
// Bypass, under its own deadline — booked under the same Detail a post-tool-result firing books.
// These are the session-started twins of advise_argv_test.go and advise_webhook_test.go, driven
// through New and a real Exchange, as a Driver reaches the lane.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// sessionStarted is the one Moment every entry in this file subscribes to.
var sessionStarted = []domain.Moment{domain.MomentSessionStarted}

// userFenceFor is the header line the lane fences a user entry id's advice under at session-started.
func userFenceFor(id string) string {
	return domain.AdviceFencePrefix + id + " (user origin) at session-started"
}

// reportLines is a Config.Report sink safe to read after an Exchange.
type reportLines struct {
	mu    sync.Mutex
	lines []string
}

func (r *reportLines) report(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, msg)
}

func (r *reportLines) read() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// TestSessionAdvise_AUserCommandLandsFencedAndBooksItsText — the command's output rides fenced
// ahead of the first opening's text, and the firing books that text alone, with no fence header.
func TestSessionAdvise_AUserCommandLandsFencedAndBooksItsText(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(sink), responder,
		userAdvise("ci", sessionStarted, "/bin/sh", "-c", `printf '%s\n' "`+sessionAdviceText+`"`))

	runExchange(t, a, "first prompt")
	runExchange(t, a, "second prompt")

	requests := responder.requests()
	first := lastUserContent(t, requests[0])
	if !strings.HasPrefix(first, userFenceFor("ci")) || !strings.Contains(first, "]\n"+sessionAdviceText+"\n") ||
		!strings.HasSuffix(first, "\n\nfirst prompt") {
		t.Errorf("first user message = %q, want the fenced command output ahead of the prompt", first)
	}
	if second := lastUserContent(t, requests[1]); second != "second prompt" {
		t.Errorf("second user message = %q, want the bare prompt", second)
	}
	fired := sessionAdviseFirings(sink, "ci")
	if len(fired) != 1 || fired[0].Action != actionAdvise || fired[0].Origin != domain.OriginUser ||
		fired[0].Moment != domain.MomentSessionStarted {
		t.Fatalf("booked %+v, want one user-origin advise firing at session-started", fired)
	}
	if fired[0].Detail != sessionAdviceText+"\n" {
		t.Errorf("Detail = %q, want the capped advice text %q and no fence header", fired[0].Detail, sessionAdviceText+"\n")
	}
}

// TestSessionAdvise_TheBookedDetailIsByteEqualToAToolResultFiring — the same command booked at
// session-started and at post-tool-result carries the same Detail byte for byte: the admission
// arm's frozen sentence, under the digest apogee-sim attributes an effect by.
func TestSessionAdvise_TheBookedDetailIsByteEqualToAToolResultFiring(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	argv := []string{"/bin/sh", "-c", `printf '%s\n' "` + armSentence + `"`}

	toolSink := &recordingSink{}
	var reported []string
	toolAgent := adviseArgvAgent(t, toolSink, t.TempDir(), false, &reported,
		userAdvise("coach", []domain.Moment{domain.MomentPostToolResult}, argv...))
	call, result := readCall()
	adviseArgvCall(t, toolAgent, call, result)

	sessionSink := &recordingSink{}
	sessionAgent := sessionAdviseAgent(t, baseConfig(sessionSink), echoResponder(t, "ok"),
		userAdvise("coach", sessionStarted, argv...))
	runExchange(t, sessionAgent, "first prompt")

	toolFired, sessionFired := firedAdvice(toolSink), sessionAdviseFirings(sessionSink, "coach")
	if len(toolFired) != 1 || len(sessionFired) != 1 {
		t.Fatalf("firings = %+v / %+v, want one at each Moment", toolFired, sessionFired)
	}
	if sessionFired[0].Detail != toolFired[0].Detail {
		t.Errorf("session-started Detail = %q, want it byte-equal to the post-tool-result Detail %q",
			sessionFired[0].Detail, toolFired[0].Detail)
	}
	if strings.Contains(sessionFired[0].Detail, domain.AdviceFencePrefix) {
		t.Errorf("Detail = %q carries a fence header", sessionFired[0].Detail)
	}
	digest := sha256.Sum256([]byte(sessionFired[0].Detail))
	if got := hex.EncodeToString(digest[:]); got != armDigest {
		t.Errorf("sha256(Detail) = %s, want the pre-registration's pinned %s", got, armDigest)
	}
}

// TestSessionAdvise_AUserCommandSeesTheSessionStart — the command reads how the session started
// both from APOGEE_REACTION_START and from the session-started document's `start` member.
func TestSessionAdvise_AUserCommandSeesTheSessionStart(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	a := sessionAdviseAgent(t, baseConfig(sink), echoResponder(t, "ok"),
		userAdvise("ci", sessionStarted, "/bin/sh", "-c",
			`printf '%s\n' "$`+domain.EnvReactionStart+`"; cat`))

	runExchange(t, a, "first prompt")

	fired := sessionAdviseFirings(sink, "ci")
	if len(fired) != 1 {
		t.Fatalf("booked %+v, want one advise firing", fired)
	}
	env, document, ok := strings.Cut(fired[0].Detail, "\n")
	if !ok || env != string(domain.SessionStartNew) {
		t.Errorf("%s = %q, want %q", domain.EnvReactionStart, env, domain.SessionStartNew)
	}
	var doc domain.SeamPayload
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		t.Fatalf("the command read %q on stdin, want the session-started document: %v", document, err)
	}
	if doc.Event != domain.MomentSessionStarted || doc.Start != domain.SessionStartNew || doc.Reaction != "ci" {
		t.Errorf("the command read %+v, want the session-started document with start %q", doc, domain.SessionStartNew)
	}
}

// TestSessionAdvise_AUserCommandIsRedacted — a configured secret's value the command prints never
// reaches the model or the firing's Detail.
func TestSessionAdvise_AUserCommandIsRedacted(t *testing.T) {
	skipWithoutPOSIXShell(t)
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_SECRET", "hunter2-the-real-one")

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_SECRET"}
	responder := echoResponder(t, "ok")
	// The value is a literal in the script, since the child's environment is scrubbed of it.
	a := sessionAdviseAgent(t, cfg, responder,
		userAdvise("ci", sessionStarted, "/bin/sh", "-c", `printf '%s\n' 'token=hunter2-the-real-one'`))

	runExchange(t, a, "first prompt")

	first := lastUserContent(t, responder.requests()[0])
	if strings.Contains(first, "hunter2-the-real-one") || !strings.Contains(first, "token=[redacted]") {
		t.Errorf("first user message = %q, want the secret's value replaced by [redacted]", first)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 || fired[0].Detail != "token=[redacted]\n" {
		t.Errorf("booked %+v, want one advise firing whose Detail is already redacted", fired)
	}
}

// TestSessionAdvise_AUserWebhookIsPostedTheDocumentAndRedacted — the endpoint receives the stamped
// session-started document, and a configured secret coming back in its reply is redacted.
func TestSessionAdvise_AUserWebhookIsPostedTheDocumentAndRedacted(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_SECRET", "hunter2-the-real-one")

	var mu sync.Mutex
	var got domain.SeamPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "token=hunter2-the-real-one\n")
	}))
	t.Cleanup(server.Close)
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_SECRET"}
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, cfg, responder,
		userAdviseWebhook("ci", sessionStarted, domain.WebhookHandler{URL: server.URL}))

	runExchange(t, a, "first prompt")

	mu.Lock()
	doc := got
	mu.Unlock()
	if doc.Event != domain.MomentSessionStarted || doc.Start != domain.SessionStartNew || doc.Reaction != "ci" ||
		doc.Time == "" {
		t.Errorf("the endpoint received %+v, want the stamped session-started document", doc)
	}
	first := lastUserContent(t, responder.requests()[0])
	if !strings.HasPrefix(first, userFenceFor("ci")) || strings.Contains(first, "hunter2-the-real-one") ||
		!strings.Contains(first, "token=[redacted]") {
		t.Errorf("first user message = %q, want the redacted reply fenced ahead of the prompt", first)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 || fired[0].Detail != "token=[redacted]\n" {
		t.Errorf("booked %+v, want one advise firing whose Detail is already redacted", fired)
	}
}

// TestSessionAdvise_AUserWebhookIsCappedWithTheMarker — a reply four times the cap lands capped
// with the truncation marker, and the firing books exactly that capped text.
func TestSessionAdvise_AUserWebhookIsCappedWithTheMarker(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", 4*domain.AdviceCap)
	server := replyWith(t, new(atomic.Int64), http.StatusOK, body)
	sink := &recordingSink{}
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(sink), responder,
		userAdviseWebhook("ci", sessionStarted, domain.WebhookHandler{URL: server.URL}))

	runExchange(t, a, "first prompt")

	capped := domain.CapAdvice(body)
	first := lastUserContent(t, responder.requests()[0])
	if !strings.Contains(first, "]\n"+capped+"\n") || len(first) > 2*domain.AdviceCap {
		t.Errorf("first user message is %d bytes, want the reply capped with the marker", len(first))
	}
	fired := sessionAdviseFirings(sink, "ci")
	if len(fired) != 1 || fired[0].Detail != capped {
		t.Fatalf("booked %d firings, want one whose Detail is the capped text", len(fired))
	}
	if !strings.HasSuffix(fired[0].Detail, "[advice truncated at 8 KiB]") {
		t.Errorf("Detail ends %q, want the truncation marker", fired[0].Detail[len(fired[0].Detail)-40:])
	}
}

// TestSessionAdvise_AUserHandlerFailsOpen — a failing command or request adds nothing to the
// opening, books one failed firing and reports one line, whichever way it failed.
func TestSessionAdvise_AUserHandlerFailsOpen(t *testing.T) {
	skipWithoutPOSIXShell(t)

	stalled := func(t *testing.T) domain.WebhookHandler {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		// Close waits for the handler, so the handler is released first: a later cleanup runs earlier.
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })
		return domain.WebhookHandler{URL: server.URL}
	}
	cases := []struct {
		name    string
		handler func(t *testing.T) domain.Handler
		timeout time.Duration
		detail  string
	}{
		{
			name:    "a non-zero exit",
			handler: func(*testing.T) domain.Handler { return domain.ArgvHandler{Argv: []string{"/bin/sh", "-c", "exit 3"}} },
			detail:  "exited 3",
		},
		{
			name:    "a command that outlives its deadline",
			handler: func(*testing.T) domain.Handler { return domain.ArgvHandler{Argv: []string{"/bin/sh", "-c", "sleep 5"}} },
			timeout: testtime.Fixed(200 * time.Millisecond),
		},
		{
			name: "a non-2xx status",
			handler: func(t *testing.T) domain.Handler {
				return domain.WebhookHandler{URL: replyWith(t, new(atomic.Int64), http.StatusServiceUnavailable, "advice").URL}
			},
			detail: "HTTP 503",
		},
		{
			name:    "an endpoint that outlives its deadline",
			handler: func(t *testing.T) domain.Handler { return stalled(t) },
			timeout: testtime.Fixed(200 * time.Millisecond),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			reports := &reportLines{}
			cfg := baseConfig(sink)
			cfg.Report = reports.report
			entry := domain.Reaction{
				ID:      "ci",
				Origin:  domain.OriginUser,
				Class:   domain.ClassAdvise,
				On:      sessionStarted,
				Timeout: tc.timeout,
				Handler: tc.handler(t),
			}
			responder := echoResponder(t, "ok")
			a := sessionAdviseAgent(t, cfg, responder, entry)

			start := time.Now()
			runExchange(t, a, "first prompt")

			if elapsed := time.Since(start); elapsed > testtime.Budget(10*time.Second) {
				t.Errorf("the first Exchange took %s, want it bounded by the entry's deadline", elapsed)
			}
			if got := lastUserContent(t, responder.requests()[0]); got != "first prompt" {
				t.Errorf("first user message = %q, want the bare prompt", got)
			}
			fired := sessionAdviseFirings(sink, "ci")
			if len(fired) != 1 || fired[0].Action != actionFailed {
				t.Fatalf("booked %+v, want exactly one failed firing", fired)
			}
			if !strings.Contains(fired[0].Detail, tc.detail) {
				t.Errorf("Detail = %q, want it to name %q", fired[0].Detail, tc.detail)
			}
			if got := reports.read(); len(got) != 1 || !strings.HasPrefix(got[0], "reaction ci (session-started): ") {
				t.Errorf("reported %q, want one line naming the reaction", got)
			}
		})
	}
}

// TestSessionAdvise_AUserEntryTakesItsOwnTimeoutElseTheAdviseDefault — an entry's `timeout:` is its
// run's deadline, and an entry that set none runs under domain.DefaultAdviseTimeout, for a command
// and a webhook alike.
func TestSessionAdvise_AUserEntryTakesItsOwnTimeoutElseTheAdviseDefault(t *testing.T) {
	t.Parallel()

	server := replyWith(t, new(atomic.Int64), http.StatusNoContent, "")
	own := 3 * time.Second
	argvOwn := userAdvise("argv-own", sessionStarted, "/bin/sh", "-c", "true")
	argvOwn.Timeout = own
	hookOwn := userAdviseWebhook("hook-own", sessionStarted, domain.WebhookHandler{URL: server.URL})
	hookOwn.Timeout = own
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), echoResponder(t, "ok"),
		userAdvise("argv-default", sessionStarted, "/bin/sh", "-c", "true"),
		argvOwn,
		userAdviseWebhook("hook-default", sessionStarted, domain.WebhookHandler{URL: server.URL}),
		hookOwn,
	)

	want := map[string]time.Duration{
		"argv-default": domain.DefaultAdviseTimeout,
		"argv-own":     own,
		"hook-default": domain.DefaultAdviseTimeout,
		"hook-own":     own,
	}
	runs := a.sessionAdvice.waiting()
	if len(runs) != len(want) {
		t.Fatalf("%d runs waiting, want %d", len(runs), len(want))
	}
	for _, run := range runs {
		if run.timeout != want[run.spec.ID] {
			t.Errorf("run %s is under %s, want %s", run.spec.ID, run.timeout, want[run.spec.ID])
		}
	}
}

// TestSessionAdvise_AUserEntryIsNeverRunUnderBypass — Bypass switches advise off (D9) before a
// handler is reached: the command is never spawned and the request never sent.
func TestSessionAdvise_AUserEntryIsNeverRunUnderBypass(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	run := func(t *testing.T, bypass bool) (spawned bool, hits int64, first string) {
		t.Helper()

		var hitCount atomic.Int64
		server := replyWith(t, &hitCount, http.StatusOK, "advice\n")
		marker := filepath.Join(t.TempDir(), "the-command-ran")
		cfg := baseConfig(&recordingSink{})
		cfg.Bypass = bypass
		responder := echoResponder(t, "ok")
		a := sessionAdviseAgent(t, cfg, responder,
			userAdvise("argv", sessionStarted, "/bin/sh", "-c", `: > "`+marker+`"; printf 'advice\n'`),
			userAdviseWebhook("hook", sessionStarted, domain.WebhookHandler{URL: server.URL}))

		runExchange(t, a, "first prompt")

		_, err := os.Stat(marker)
		return err == nil, hitCount.Load(), lastUserContent(t, responder.requests()[0])
	}

	t.Run("Bypass off runs both", func(t *testing.T) {
		t.Parallel()
		spawned, hits, first := run(t, false)
		if !spawned || hits != 1 || !strings.Contains(first, domain.AdviceFencePrefix) {
			t.Errorf("spawned=%v hits=%d first=%q, want both run and advised — the canary pins nothing", spawned, hits, first)
		}
	})
	t.Run("Bypass on runs neither", func(t *testing.T) {
		t.Parallel()
		spawned, hits, first := run(t, true)
		if spawned || hits != 0 || first != "first prompt" {
			t.Errorf("spawned=%v hits=%d first=%q under Bypass, want neither run and the bare prompt", spawned, hits, first)
		}
	})
}
