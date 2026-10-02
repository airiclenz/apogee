package webhook

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// TestWebhookPostSendsTheBodyWithBothKindsOfHeader is the round trip the package exists for: the endpoint
// sees a POST of exactly the body, apogee's own two headers, the handler's literal headers, and the
// `headers-env:` ones read out of the environment at send time — and the caller gets the 2xx reply
// back to read.
func TestWebhookPostSendsTheBodyWithBothKindsOfHeader(t *testing.T) {
	t.Setenv("APOGEE_TEST_WEBHOOK_TOKEN", "s3cr3t")

	type received struct {
		method, contentType, userAgent, literal, fromEnv, body string
	}
	var got received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = received{r.Method, r.Header.Get("Content-Type"), r.Header.Get("User-Agent"),
			r.Header.Get("X-Source"), r.Header.Get("Authorization"), string(body)}
		_, _ = io.WriteString(w, "noted")
	}))
	defer server.Close()
	handler := domain.WebhookHandler{
		URL:        server.URL + "/fire",
		Headers:    map[string]string{"X-Source": "apogee-test"},
		HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_WEBHOOK_TOKEN"},
	}

	response, err := Post(context.Background(), security.URLGuard{}, handler, 10*time.Second, []byte(`{"event":"x"}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	reply, _ := io.ReadAll(response.Body)

	want := received{http.MethodPost, "application/json", "apogee", "apogee-test", "s3cr3t", `{"event":"x"}`}
	if got != want {
		t.Errorf("the endpoint received %+v, want %+v", got, want)
	}
	if string(reply) != "noted" {
		t.Errorf("reply = %q, want the endpoint's body handed back to the caller", reply)
	}
}

// TestWebhookPostReportsANonSuccessStatusAndClosesTheReply pins the shape of a refused POST: the error
// names the status and nothing else, and the reply is not handed back — its body was drained and
// closed here, so a caller cannot read an answer the endpoint refused to give.
func TestWebhookPostReportsANonSuccessStatusAndClosesTheReply(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "allow", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	response, err := Post(context.Background(), security.URLGuard{}, domain.WebhookHandler{URL: server.URL},
		10*time.Second, nil)

	if response != nil {
		t.Errorf("Post returned a response %v on a 503, want nil", response.Status)
	}
	if err == nil || err.Error() != "HTTP 503" {
		t.Errorf("error = %v, want %q", err, "HTTP 503")
	}
}

// TestWebhookPostRefusesToSendWhenTheHeaderVariableIsUnset is the whole point of `headers-env:` working out
// at SEND time: an unauthenticated POST would reach the endpoint and be refused there, for a reason
// the user could never see from apogee. It fails here instead, naming the header and the variable —
// and never the value, which is a secret.
func TestWebhookPostRefusesToSendWhenTheHeaderVariableIsUnset(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	handler := domain.WebhookHandler{
		URL:        server.URL,
		HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_WEBHOOK_ABSENT_TOKEN"},
	}

	_, err := Post(context.Background(), security.URLGuard{}, handler, 10*time.Second, nil)

	if err == nil {
		t.Fatal("Post with an unset header variable returned no error")
	}
	for _, want := range []string{"Authorization", "APOGEE_TEST_WEBHOOK_ABSENT_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("the endpoint was called %d times; it must not be reached at all", hits.Load())
	}
}

// TestWebhookPostFailureWordsTheFailureWithoutTheURL pins the two readings of a transport error: a
// deadline is reported as the timeout it was, and any other failure is reported without the URL
// net/http wraps it in — a webhook URL is exactly the kind of thing that carries a token.
func TestWebhookPostFailureWordsTheFailureWithoutTheURL(t *testing.T) {
	t.Parallel()

	refused := errors.New("connection refused")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a deadline",
			err:  &url.Error{Op: "Post", URL: "https://example.test/hook?token=secret", Err: timeoutErr{}},
			want: "timed out after 150ms",
		},
		{
			name: "a refused connection",
			err:  &url.Error{Op: "Post", URL: "https://example.test/hook?token=secret", Err: refused},
			want: "POST failed: connection refused",
		},
		{
			name: "an error net/http did not wrap",
			err:  refused,
			want: "POST failed: connection refused",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := PostFailure(150*time.Millisecond, c.err)

			if got.Error() != c.want {
				t.Errorf("PostFailure = %q, want %q", got, c.want)
			}
			if strings.Contains(got.Error(), "secret") {
				t.Errorf("PostFailure = %q leaks the URL", got)
			}
		})
	}
}

// timeoutErr is a net error that reports itself as a deadline, the way net/http's own does.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestWebhookPackageImportsOnlyDomainAndSecurityFromApogee holds the package's one-direction rule:
// it is a leaf over the standard library plus internal/domain and internal/security, so the observe
// lane (internal/reactions) and the sync lane (internal/agent) can both import it without either
// importing the other. The files are read straight off the directory and parsed with go/parser, on
// internal/platform/winlabel's pattern, so a build-tagged file could not hide a violation from the
// development machine's build.
func TestWebhookPackageImportsOnlyDomainAndSecurityFromApogee(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"github.com/airiclenz/apogee/internal/domain",
		"github.com/airiclenz/apogee/internal/security",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if slices.Contains(allowed, path) || isStandardLibrary(path) {
				continue
			}
			t.Errorf("%s imports %q; webhook is a leaf over the standard library and %v", name, path, allowed)
		}
	}
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the leaf-dependency guard proved nothing")
	}
}

// isStandardLibrary reports whether path names a standard-library package, by the rule the go
// command itself reserves: an import path whose FIRST element carries no dot cannot be a module path.
func isStandardLibrary(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// TestWebhookPostHandsBackARedirectWithoutFollowingIt is the reason the post goes through the
// guarded client: a plain client followed a 3xx and carried the `headers-env:` token to whatever
// host the endpoint named. The redirect is the reply now — reported as its status — and the host it
// pointed at never hears from apogee.
func TestWebhookPostHandsBackARedirectWithoutFollowingIt(t *testing.T) {
	t.Setenv("APOGEE_TEST_WEBHOOK_REDIRECT_TOKEN", "s3cr3t")

	var targetHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	handler := domain.WebhookHandler{
		URL:        redirector.URL + "/fire",
		HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_WEBHOOK_REDIRECT_TOKEN"},
	}

	response, err := Post(context.Background(), security.URLGuard{}, handler, 10*time.Second, nil)

	if response != nil {
		t.Errorf("Post returned a response %v on a redirect, want nil", response.Status)
	}
	if err == nil || err.Error() != "HTTP 307" {
		t.Errorf("error = %v, want %q — the redirect is the reply, never followed", err, "HTTP 307")
	}
	if targetHits.Load() != 0 {
		t.Errorf("the redirect target was called %d times; it must never be reached", targetHits.Load())
	}
}

// TestWebhookPostRefusesAHostTheURLSafetyListsClose pins the pre-flight: an endpoint on the deny
// list, or off a non-empty allow list, is refused before anything is dialled, with a sentence that
// names the url-safety refusal and never the URL's path or query.
func TestWebhookPostRefusesAHostTheURLSafetyListsClose(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		guard security.URLGuard
		want  string
	}{
		{name: "a denied host", guard: security.NewURLGuard(nil, []string{"127.0.0.1"}), want: "is denied"},
		{name: "a host off the allow list", guard: security.NewURLGuard([]string{"hooks.example"}, nil),
			want: "is not on the allow-list"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			handler := domain.WebhookHandler{URL: server.URL + "/fire?token=secret"}

			_, err := Post(context.Background(), c.guard, handler, 10*time.Second, nil)

			if err == nil {
				t.Fatal("Post to a closed host returned no error")
			}
			if !errors.Is(err, security.ErrURLBlocked) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want a url-safety refusal saying %q", err, c.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("error = %q leaks the URL", err)
			}
			if hits.Load() != 0 {
				t.Errorf("the endpoint was called %d times; a closed host must not be dialled", hits.Load())
			}
		})
	}
}

// TestWebhookPostReachesALoopbackEndpointUnderTheZeroGuard is the operator-named half of the
// posture: the resolved-IP floor that keeps the MODEL off loopback and the LAN does not apply to a
// webhook the user configured, so a hook on 127.0.0.1 with no url-safety lists still gets the post.
func TestWebhookPostReachesALoopbackEndpointUnderTheZeroGuard(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	response, err := Post(context.Background(), security.URLGuard{}, domain.WebhookHandler{URL: server.URL},
		10*time.Second, nil)
	if err != nil {
		t.Fatalf("Post to a loopback endpoint: %v", err)
	}
	Drain(response)

	if hits.Load() != 1 {
		t.Errorf("the loopback endpoint was called %d times, want 1", hits.Load())
	}
}

// TestWebhookPostWordsADeadlineInThePinAsATimeout pins the send's deadline over the pin step: the
// pin resolves the endpoint's host under the post's own timeout — the sync lane's ctx carries no
// deadline — and a lookup that outlasts it reads as the timeout it was, like a stalled endpoint.
func TestWebhookPostWordsADeadlineInThePinAsATimeout(t *testing.T) {
	t.Parallel()

	stalled := security.URLGuard{}.WithResolver(func(ctx context.Context, _ string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	handler := domain.WebhookHandler{URL: "http://hooks.example/fire?token=secret"}

	started := time.Now()
	_, err := Post(context.Background(), stalled, handler, 50*time.Millisecond, nil)
	elapsed := time.Since(started)

	if err == nil || err.Error() != "timed out after 50ms" {
		t.Errorf("error = %v, want %q", err, "timed out after 50ms")
	}
	if elapsed > time.Second {
		t.Errorf("Post took %s to give up on a 50ms timeout", elapsed)
	}
}
