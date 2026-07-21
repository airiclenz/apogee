package present

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests run against a REAL listener rather than httptest: the lazy bind, the shared listener
// and the shutdown are the behaviours under test here, and an injected fake server would test the
// injection instead. The advertised host is pinned to loopback so the URL the code composes is
// also the URL the test fetches — which is the point, since composing it wrongly is a real bug.

// servedURLPattern is the URL shape the ladder promises and the transcript prints: the token is
// exactly 32 hex characters (16 crypto/rand bytes) and the basename survives verbatim.
var servedURLPattern = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/d/[0-9a-f]{32}/[^/]+$`)

// newTestServer starts a doc server on an ephemeral port advertising loopback, closed when the
// test ends so no listener outlives it.
func newTestServer(t *testing.T) *DocServer {
	t.Helper()

	server := &DocServer{Host: "127.0.0.1"}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() = %v, want no error", err)
		}
	})
	return server
}

// writeDoc puts a document in the test's own directory and returns its absolute path — the form
// the tool resolves before a presentation ever reaches the server.
func writeDoc(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the test document: %v", err)
	}
	return path
}

// fetch performs one GET and returns the status, the content type and the body.
func fetch(t *testing.T, target string) (int, string, string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// origin returns the scheme and authority of a served URL, so two presentations can be compared
// on the listener they came from.
func origin(t *testing.T, served string) string {
	t.Helper()

	parsed, err := url.Parse(served)
	if err != nil {
		t.Fatalf("Serve() returned an unparseable URL %q: %v", served, err)
	}
	return parsed.Scheme + "://" + parsed.Host
}

// A granted document is fetchable at exactly the URL Serve returned, with its content byte for
// byte and a content type decided by its extension — the browser-renderable set rung 2 exists for
// (.html, .svg, .pdf) plus the honest fallback for anything else.
func TestDocServerServesAGrantedDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		file     string
		content  string
		wantType string
	}{
		{
			name:     "the deliverable case: an HTML report",
			file:     "architecture-review.html",
			content:  "<html><body>the review</body></html>",
			wantType: "text/html",
		},
		{
			name:     "a diagram",
			file:     "graph.svg",
			content:  `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			wantType: "image/svg+xml",
		},
		{
			name:     "a PDF",
			file:     "report.pdf",
			content:  "%PDF-1.7\n",
			wantType: "application/pdf",
		},
		{
			name:     "an extension nothing knows downloads rather than renders",
			file:     "notes.apogeedoc",
			content:  "just bytes",
			wantType: "application/octet-stream",
		},
		{
			name:     "a name with a space is escaped into the URL and matched back",
			file:     "my report.html",
			content:  "<p>spaces</p>",
			wantType: "text/html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)
			path := writeDoc(t, tt.file, tt.content)

			served, err := server.Serve(path)
			if err != nil {
				t.Fatalf("Serve() = %v, want no error", err)
			}
			if !servedURLPattern.MatchString(served) {
				t.Errorf("Serve() = %q, want the /d/<32-hex>/<basename> shape", served)
			}

			status, contentType, body := fetch(t, served)
			if status != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", served, status)
			}
			if !strings.HasPrefix(contentType, tt.wantType) {
				t.Errorf("Content-Type = %q, want it to start with %q", contentType, tt.wantType)
			}
			if body != tt.content {
				t.Errorf("body = %q, want %q", body, tt.content)
			}
		})
	}
}

// The grant is to the PATH, not to a snapshot: a document rewritten after it was presented serves
// its new content, which is what makes re-presenting an edited deliverable work at all.
func TestDocServerRereadsTheDocumentPerRequest(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	path := writeDoc(t, "review.html", "<p>first draft</p>")

	served, err := server.Serve(path)
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}
	if _, _, body := fetch(t, served); body != "<p>first draft</p>" {
		t.Fatalf("body = %q, want the first draft", body)
	}

	if err := os.WriteFile(path, []byte("<p>second draft, longer</p>"), 0o600); err != nil {
		t.Fatalf("rewriting the document: %v", err)
	}

	status, _, body := fetch(t, served)
	if status != http.StatusOK {
		t.Errorf("GET after the rewrite = %d, want 200", status)
	}
	if body != "<p>second draft, longer</p>" {
		t.Errorf("body = %q, want the rewritten document", body)
	}
}

// A document that vanished after it was granted is a 404, not a stale copy: nothing is cached, so
// the server can only ever answer with what is on disk now.
func TestDocServerAnswers404ForAVanishedDocument(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	path := writeDoc(t, "review.html", "<p>here for now</p>")

	served, err := server.Serve(path)
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the document: %v", err)
	}

	if status, _, _ := fetch(t, served); status != http.StatusNotFound {
		t.Errorf("GET a removed document = %d, want 404", status)
	}
}

// Everything that is not the exact granted path is refused identically. This is the whole security
// posture of the doc server (ADR 0019 §3): the grant map is the only thing that can turn a request
// into a file, so there is no traversal to defend against — but the cases are pinned anyway,
// because a future handler that resolved paths instead would pass every other test in this file.
func TestDocServerRefusesEverythingButTheGrantedPath(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	path := writeDoc(t, "review.html", "<p>granted</p>")
	secret := writeDoc(t, "secrets.env", "TOKEN=hunter2")

	served, err := server.Serve(path)
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}
	parsed, err := url.Parse(served)
	if err != nil {
		t.Fatalf("Serve() returned an unparseable URL %q: %v", served, err)
	}
	base := parsed.Scheme + "://" + parsed.Host
	token := strings.Split(strings.TrimPrefix(parsed.Path, docPathPrefix), "/")[0]

	tests := []struct {
		name string
		path string
	}{
		{name: "the site root", path: "/"},
		{name: "the prefix alone, which lists nothing", path: docPathPrefix},
		{name: "the token without a basename", path: docPathPrefix + token},
		{name: "the token with a trailing slash", path: docPathPrefix + token + "/"},
		{name: "a wrong token", path: docPathPrefix + strings.Repeat("a", 32) + "/review.html"},
		{
			name: "the right basename under no token at all",
			path: docPathPrefix + "review.html",
		},
		{
			name: "another file's name under a valid token",
			path: docPathPrefix + token + "/secrets.env",
		},
		{
			name: "a traversal out of the grant",
			path: docPathPrefix + token + "/../" + filepath.Base(secret),
		},
		{
			name: "a traversal to an absolute system path",
			path: docPathPrefix + token + "/../../../../etc/passwd",
		},
		{
			name: "a percent-encoded traversal, which arrives decoded",
			path: docPathPrefix + token + "/%2e%2e/%2e%2e/etc/passwd",
		},
		{
			name: "the granted path with anything appended",
			path: parsed.Path + "/extra",
		},
		{name: "a favicon probe", path: "/favicon.ico"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := base + tt.path
			status, _, body := fetch(t, target)
			if status != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", target, status)
			}
			if strings.Contains(body, token) || strings.Contains(body, "review.html") {
				t.Errorf("the 404 body %q describes what exists", body)
			}
		})
	}
}

// A capability to read is not a capability to write: a request that is not a fetch is refused as
// not-found rather than not-allowed, so the response never confirms that the token is real.
func TestDocServerRefusesNonFetchMethods(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	path := writeDoc(t, "review.html", "<p>granted</p>")

	served, err := server.Serve(path)
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, err := http.NewRequest(method, served, strings.NewReader(""))
		if err != nil {
			t.Fatalf("building the %s request: %v", method, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, served, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", method, resp.StatusCode)
		}
	}
}

// Presenting several documents grants several tokens on ONE listener: the port is bound lazily on
// the first presentation and reused by every later one, so a session opens at most one port.
func TestDocServerSharesOneListenerAcrossPresentations(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	if server.listener != nil {
		t.Fatal("the doc server bound a port before anything was presented")
	}

	first := writeDoc(t, "first.html", "<p>first</p>")
	second := writeDoc(t, "second.html", "<p>second</p>")

	firstURL, err := server.Serve(first)
	if err != nil {
		t.Fatalf("Serve(first) = %v, want no error", err)
	}
	listener := server.listener

	secondURL, err := server.Serve(second)
	if err != nil {
		t.Fatalf("Serve(second) = %v, want no error", err)
	}

	if server.listener != listener {
		t.Error("the second presentation replaced the listener, want it reused")
	}
	if origin(t, firstURL) != origin(t, secondURL) {
		t.Errorf("the two presentations advertise %s and %s, want one listener", origin(t, firstURL), origin(t, secondURL))
	}
	if firstURL == secondURL {
		t.Errorf("both presentations returned %q, want a fresh token each", firstURL)
	}

	if _, _, body := fetch(t, firstURL); body != "<p>first</p>" {
		t.Errorf("the first URL served %q", body)
	}
	if _, _, body := fetch(t, secondURL); body != "<p>second</p>" {
		t.Errorf("the second URL served %q", body)
	}

	// The grants are independent: neither token reaches the other document.
	firstPath, secondPath := mustPath(t, firstURL), mustPath(t, secondURL)
	crossed := origin(t, firstURL) + strings.Replace(firstPath, "first.html", "second.html", 1)
	if status, _, _ := fetch(t, crossed); status != http.StatusNotFound {
		t.Errorf("the first token reached the second document (%d), want 404", status)
	}
	if firstPath == secondPath {
		t.Error("the two grants share a URL path")
	}
}

// Presentations arrive on worker goroutines and requests on the server's own, so the lazy start
// and the grant map are both shared state: several presentations racing on the FIRST one must
// still produce exactly one listener, and every grant must be fetchable afterwards. Run under
// -race, this is the test that pins the mutex.
func TestDocServerServesConcurrently(t *testing.T) {
	t.Parallel()

	const presentations = 12

	server := newTestServer(t)
	urls := make(chan string, presentations)
	errs := make(chan error, presentations)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range presentations {
		path := writeDoc(t, "review.html", "<p>document "+strconv.Itoa(i)+"</p>")

		wg.Add(1)
		go func() {
			defer wg.Done()

			<-start
			served, err := server.Serve(path)
			if err != nil {
				errs <- err
				return
			}
			urls <- served
		}()
	}
	close(start)
	wg.Wait()
	close(urls)
	close(errs)

	for err := range errs {
		t.Errorf("Serve() = %v, want no error", err)
	}

	origins := make(map[string]bool)
	granted := 0
	for served := range urls {
		granted++
		origins[origin(t, served)] = true
		if status, _, _ := fetch(t, served); status != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", served, status)
		}
	}
	if granted != presentations {
		t.Errorf("%d presentations produced %d URLs", presentations, granted)
	}
	if len(origins) != 1 {
		t.Errorf("the presentations advertise %d listeners, want 1: %v", len(origins), origins)
	}
}

// Close is wired into app shutdown, which cannot know whether this session ever presented
// anything: it must be safe on a server that never started, and safe to call twice. After it, a
// late presentation fails (degrading to the baseline rung) rather than resurrecting a listener.
func TestDocServerCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("a server that never served", func(t *testing.T) {
		t.Parallel()

		server := &DocServer{Host: "127.0.0.1"}
		if err := server.Close(); err != nil {
			t.Errorf("Close() on an unused server = %v, want no error", err)
		}
		if err := server.Close(); err != nil {
			t.Errorf("Close() again = %v, want no error", err)
		}
	})

	t.Run("a server that served", func(t *testing.T) {
		t.Parallel()

		server := &DocServer{Host: "127.0.0.1"}
		path := writeDoc(t, "review.html", "<p>granted</p>")

		served, err := server.Serve(path)
		if err != nil {
			t.Fatalf("Serve() = %v, want no error", err)
		}
		if err := server.Close(); err != nil {
			t.Errorf("Close() = %v, want no error", err)
		}
		if err := server.Close(); err != nil {
			t.Errorf("Close() again = %v, want no error", err)
		}

		if _, err := server.Serve(path); err == nil {
			t.Error("Serve() after Close() = nil, want an error rather than a new listener")
		}

		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get(served); err == nil {
			resp.Body.Close()
			t.Errorf("GET %s after Close() = %d, want a connection failure", served, resp.StatusCode)
		}
	})
}

// A URL is never printed for something that cannot be fetched: the checks that would otherwise
// become a 404 in front of the user happen at Serve, where the caller can still degrade to the
// baseline rung and say what happened.
func TestDocServerRejectsWhatItCannotServe(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{name: "a blank path is a caller that lost the document", path: "   "},
		{name: "a document that does not exist", path: filepath.Join(dir, "missing.html")},
		{name: "a directory", path: dir},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served, err := server.Serve(tt.path)
			if err == nil {
				t.Fatalf("Serve(%q) = %q, want an error", tt.path, served)
			}
			if served != "" {
				t.Errorf("Serve(%q) returned the URL %q alongside its error", tt.path, served)
			}
		})
	}
}

// The advertised host is the address the USER'S machine reaches this one on (AdvertiseHost's
// answer), which is why it is a field rather than something read off the listener: the bind
// address knows nothing about that. IPv6 has to arrive at the URL bracketed exactly once.
func TestDocServerAdvertisesTheConfiguredHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "the devbox case: the server IP from SSH_CONNECTION",
			host: "192.168.64.2",
			want: "192.168.64.2",
		},
		{
			name: "an unset host advertises loopback rather than composing a broken URL",
			host: "",
			want: "127.0.0.1",
		},
		{
			name: "a whitespace-only host reads as unset",
			host: "   ",
			want: "127.0.0.1",
		},
		{
			name: "an IPv6 literal is bracketed for the URL authority",
			host: "2001:db8::2",
			want: "[2001:db8::2]",
		},
		{
			name: "an already-bracketed literal is not bracketed twice",
			host: "[2001:db8::2]",
			want: "[2001:db8::2]",
		},
		{
			name: "a hostname passes through",
			host: "devbox.local",
			want: "devbox.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := &DocServer{Host: tt.host}
			t.Cleanup(func() { _ = server.Close() })
			path := writeDoc(t, "review.html", "<p>granted</p>")

			served, err := server.Serve(path)
			if err != nil {
				t.Fatalf("Serve() = %v, want no error", err)
			}
			parsed, err := url.Parse(served)
			if err != nil {
				t.Fatalf("Serve() returned an unparseable URL %q: %v", served, err)
			}
			if host := parsed.Hostname(); host != strings.Trim(tt.want, "[]") {
				t.Errorf("Serve() advertised host %q, want %q", host, tt.want)
			}
			if !strings.HasPrefix(parsed.Host, tt.want+":") {
				t.Errorf("Serve() authority = %q, want it to start with %q", parsed.Host, tt.want+":")
			}
		})
	}
}

// present.port is the port that actually gets bound — proven the deterministic way round, by
// naming a port this test is itself holding: only a server that binds s.Port can collide with it.
// The failure is the other half of the contract (ADR 0019 §4): a doc server that cannot bind
// returns an error naming the port, so the caller degrades to the baseline rung and the transcript
// can say why, instead of a presentation quietly producing no link.
func TestDocServerBindsTheConfiguredPort(t *testing.T) {
	t.Parallel()

	occupied, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("taking a port for the test: %v", err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port

	server := &DocServer{Host: "127.0.0.1", Port: port}
	t.Cleanup(func() { _ = server.Close() })

	served, err := server.Serve(writeDoc(t, "review.html", "<p>granted</p>"))
	if err == nil {
		t.Fatalf("Serve() = %q, want the bind failure on port %d", served, port)
	}
	if served != "" {
		t.Errorf("Serve() returned the URL %q alongside its error", served)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Errorf("Serve() = %v, want the message to name port %d", err, port)
	}
}

// Tokens are secrets, so they must be unpredictable: every grant mints a fresh one, and no two
// collide.
func TestNewTokenMintsFreshHexTokens(t *testing.T) {
	t.Parallel()

	hexToken := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := make(map[string]bool, 64)

	for i := 0; i < 64; i++ {
		token, err := newToken()
		if err != nil {
			t.Fatalf("newToken() = %v, want no error", err)
		}
		if !hexToken.MatchString(token) {
			t.Fatalf("newToken() = %q, want 32 hex characters", token)
		}
		if seen[token] {
			t.Fatalf("newToken() repeated %q", token)
		}
		seen[token] = true
	}
}

// mustPath returns the URL path of a served document.
func mustPath(t *testing.T, served string) string {
	t.Helper()

	parsed, err := url.Parse(served)
	if err != nil {
		t.Fatalf("Serve() returned an unparseable URL %q: %v", served, err)
	}
	return parsed.Path
}
