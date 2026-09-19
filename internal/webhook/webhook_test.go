package webhook

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
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

	response, err := Post(context.Background(), handler, 10*time.Second, []byte(`{"event":"x"}`))
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

	response, err := Post(context.Background(), domain.WebhookHandler{URL: server.URL}, 10*time.Second, nil)

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

	_, err := Post(context.Background(), handler, 10*time.Second, nil)

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

// TestWebhookPackageImportsOnlyDomainFromApogee holds the package's one-direction rule: it is a leaf over
// the standard library plus internal/domain, so the observe lane (internal/reactions) and the sync
// lane (internal/agent) can both import it without either importing the other. The files are read
// straight off the directory and parsed with go/parser, on internal/platform/winlabel's pattern, so
// a build-tagged file could not hide a violation from the development machine's build.
func TestWebhookPackageImportsOnlyDomainFromApogee(t *testing.T) {
	t.Parallel()

	const allowed = "github.com/airiclenz/apogee/internal/domain"
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
			if path == allowed || isStandardLibrary(path) {
				continue
			}
			t.Errorf("%s imports %q; webhook is a leaf over the standard library and %s", name, path, allowed)
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
