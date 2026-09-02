package present

import (
	"bufio"
	"errors"
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

// newTestServer starts a doc server on an ephemeral port advertising loopback and fenced to root,
// closed when the test ends so no listener outlives it.
func newTestServer(t *testing.T, root string) *DocServer {
	t.Helper()

	server := &DocServer{Host: "127.0.0.1", Root: root}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() = %v, want no error", err)
		}
	})
	return server
}

// writeDoc puts a document inside root — the workspace the server fences its grants to — and
// returns its absolute path, the form the tool resolves before a presentation reaches the server.
func writeDoc(t *testing.T, root, name, content string) string {
	t.Helper()

	path := filepath.Join(root, name)
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
	defer func() { _ = resp.Body.Close() }()

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

			root := t.TempDir()
			server := newTestServer(t, root)
			path := writeDoc(t, root, tt.file, tt.content)

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

// cspDirectives splits a Content-Security-Policy header into directive name → source list, so a
// test can assert what a directive SAYS rather than that the header exists. Directive names are
// case-insensitive per the CSP grammar; source expressions are not, so only the name is folded.
func cspDirectives(t *testing.T, header string) map[string][]string {
	t.Helper()

	directives := make(map[string][]string)
	for _, raw := range strings.Split(header, ";") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		name := strings.ToLower(fields[0])
		if _, dup := directives[name]; dup {
			t.Errorf("policy states %q twice; browsers honour the first and the second reads as intent that is not applied", name)
		}
		directives[name] = fields[1:]
	}
	return directives
}

// Rung 2 is the only rung that still shows active content — .html, .htm and .svg left the
// OS opener's allow-list on 2026-08-12 (ADR 0019, fourth amendment) because a file:// launch can
// carry no policy — so the served document's POLICY is what bounds it, and this test asserts the
// directives rather than the header's presence. That distinction is the whole point: a page served
// under `default-src 'self'` would satisfy a presence assertion while still letting script fetch
// loopback, RFC1918 and 169.254.169.254 from the browser's network position, which is the attack.
// Weakening `default-src 'none'` must fail here.
func TestDocServerServesEveryDocumentUnderARestrictivePolicy(t *testing.T) {
	t.Parallel()

	// Every document gets the same headers, active or not: the policy is a property of the server
	// rather than of the extension, so a format that becomes active later is already covered.
	files := map[string]string{
		"report.html": "<html><body>the review</body></html>",
		"graph.svg":   `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"report.pdf":  "%PDF-1.7\n",
	}

	for file, content := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			server := newTestServer(t, root)
			served, err := server.Serve(writeDoc(t, root, file, content))
			if err != nil {
				t.Fatalf("Serve() = %v, want no error", err)
			}

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(served)
			if err != nil {
				t.Fatalf("GET %s: %v", served, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				t.Fatalf("reading the response body: %v", err)
			}

			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want %q — the extension must keep deciding what is active", got, "nosniff")
			}

			policy := resp.Header.Get("Content-Security-Policy")
			if policy == "" {
				t.Fatal("no Content-Security-Policy: rung 2 is the rung that shows active content, so the policy is the bound")
			}
			directives := cspDirectives(t, policy)

			// The load-bearing directive, asserted as an exact source list. 'self' would still
			// permit script from the served origin, and the origin is a token away.
			if got := directives["default-src"]; len(got) != 1 || got[0] != "'none'" {
				t.Errorf("default-src = %q, want exactly [\"'none'\"] — anything else leaves script, fetch and XHR open", got)
			}
			// These do not fall back to default-src, so each must say 'none' in its own right.
			for _, name := range []string{"form-action", "base-uri", "frame-ancestors"} {
				if got := directives[name]; len(got) != 1 || got[0] != "'none'" {
					t.Errorf("%s = %q, want exactly [\"'none'\"] — this directive has no default-src fallback", name, got)
				}
			}
			// A BARE sandbox is what withholds allow-top-navigation, which is the only answer to
			// <meta http-equiv="refresh"> — CSP has no directive for it. Any allow-* token here
			// hands part of that back.
			tokens, ok := directives["sandbox"]
			if !ok {
				t.Error("no sandbox directive: nothing then stops a meta refresh navigating the browser somewhere the policy never sees")
			}
			if len(tokens) != 0 {
				t.Errorf("sandbox = %q, want it bare — each allow-* token returns a capability the bare form withholds", tokens)
			}
			// The two narrow re-openings that keep a self-contained report readable. They are
			// asserted so that tightening them is a deliberate change with a failing test, not a
			// silent regression in what rung 2 can still show.
			if got := directives["img-src"]; len(got) == 0 {
				t.Error("no img-src: a report with its own images would render blank, which makes the rung useless")
			}
			if got := directives["style-src"]; len(got) == 0 {
				t.Error("no style-src: a report with an inline stylesheet would render unstyled")
			}
		})
	}
}

// The grant is to the PATH, not to a snapshot: a document rewritten after it was presented serves
// its new content, which is what makes re-presenting an edited deliverable work at all.
func TestDocServerRereadsTheDocumentPerRequest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	path := writeDoc(t, root, "review.html", "<p>first draft</p>")

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

	root := t.TempDir()
	server := newTestServer(t, root)
	path := writeDoc(t, root, "review.html", "<p>here for now</p>")

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

// The grant is to a NAME INSIDE THE WORKSPACE, re-checked through the fence on every request — not
// to a path that was inside it once. This is the audit's exfiltration case: the model writes a
// report, presents it (which is what mints the token), and then replaces the report with a symlink
// to a file outside the workspace — a write the fence permits, because it bounds where a file is
// written and not what a link inside it names. The token is already in the transcript, so whoever
// holds it would be served the target from off the box.
func TestDocServerRefusesAPostGrantSymlinkSwap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	secret := writeDoc(t, t.TempDir(), "config.yaml", "api-key: SUPERSECRET_TOKEN")
	path := writeDoc(t, root, "report.html", "<p>the report</p>")

	served, err := server.Serve(path)
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}
	if _, _, body := fetch(t, served); body != "<p>the report</p>" {
		t.Fatalf("body = %q, want the granted document", body)
	}

	// ln -s <outside the workspace> report.html
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the granted document: %v", err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatalf("swapping the granted document for a symlink: %v", err)
	}

	status, _, body := fetch(t, served)
	if status != http.StatusNotFound {
		t.Errorf("GET after the swap = %d, want 404", status)
	}
	if strings.Contains(body, "SUPERSECRET_TOKEN") {
		t.Errorf("the doc server served the symlink's target off the box: %q", body)
	}
}

// A doc server with no workspace to fence its grants to serves nothing. The failure is at Serve,
// where the caller can still degrade to the baseline rung, rather than at the request — a grant
// that cannot be re-checked is one the server must never make.
func TestDocServerWithoutARootServesNothing(t *testing.T) {
	t.Parallel()

	server := &DocServer{Host: "127.0.0.1"}
	t.Cleanup(func() { _ = server.Close() })
	path := writeDoc(t, t.TempDir(), "review.html", "<p>granted</p>")

	served, err := server.Serve(path)
	if err == nil {
		t.Fatalf("Serve() = %q, want an error on a server with no root", served)
	}
	if served != "" {
		t.Errorf("Serve() returned the URL %q alongside its error", served)
	}
	if server.listener != nil {
		t.Error("a refused grant bound a listener")
	}
}

// Everything that is not the exact granted path is refused identically. This is the whole security
// posture of the doc server (ADR 0019 §3): the grant map is the only thing that can turn a request
// into a file, so there is no traversal to defend against — but the cases are pinned anyway,
// because a future handler that resolved paths instead would pass every other test in this file.
func TestDocServerRefusesEverythingButTheGrantedPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	path := writeDoc(t, root, "review.html", "<p>granted</p>")
	secret := writeDoc(t, root, "secrets.env", "TOKEN=hunter2")

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

	root := t.TempDir()
	server := newTestServer(t, root)
	path := writeDoc(t, root, "review.html", "<p>granted</p>")

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
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", method, resp.StatusCode)
		}
	}
}

// Presenting several documents grants several tokens on ONE listener: the port is bound lazily on
// the first presentation and reused by every later one, so a session opens at most one port.
func TestDocServerSharesOneListenerAcrossPresentations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	if server.listener != nil {
		t.Fatal("the doc server bound a port before anything was presented")
	}

	first := writeDoc(t, root, "first.html", "<p>first</p>")
	second := writeDoc(t, root, "second.html", "<p>second</p>")

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

	root := t.TempDir()
	server := newTestServer(t, root)
	urls := make(chan string, presentations)
	errs := make(chan error, presentations)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range presentations {
		path := writeDoc(t, root, "review-"+strconv.Itoa(i)+".html", "<p>document "+strconv.Itoa(i)+"</p>")

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

// The doc server's port answers whoever can route to this box, token or not (a wrong token is a
// served 404), so every resource one unauthenticated peer can take is bounded: how long it may
// take to send its headers, how long its response may take to write, how long an idle keep-alive
// is kept — and how many connections exist at all. An unbounded stage is a connection held for
// free, and the descriptors it exhausts are the AGENT's.
func TestDocServerBoundsEveryConnection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	if _, err := server.Serve(writeDoc(t, root, "review.html", "<p>granted</p>")); err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}

	timeouts := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "ReadHeaderTimeout", got: server.srv.ReadHeaderTimeout, want: readHeaderTimeout},
		{name: "WriteTimeout", got: server.srv.WriteTimeout, want: writeTimeout},
		{name: "IdleTimeout", got: server.srv.IdleTimeout, want: idleTimeout},
	}
	for _, tt := range timeouts {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
		if tt.got <= 0 {
			t.Errorf("%s is unbounded, want a finite bound", tt.name)
		}
	}

	bounded, ok := server.listener.(*limitListener)
	if !ok {
		t.Fatalf("the doc server serves a %T, want the connection cap wrapped around the listener", server.listener)
	}
	if bounded.limit != maxConnections {
		t.Errorf("connection cap = %d, want %d", bounded.limit, maxConnections)
	}
}

// The cap sheds rather than queues, and it lets go: a peer holding the cap's worth of keep-alives
// gets its next connection closed immediately instead of parked in the backlog, and once those
// connections end the document is served again. This is the DoS half of the audit finding — an
// unauthenticated peer must not be able to convert "can reach the box" into "holds this process's
// descriptors".
func TestDocServerShedsConnectionsBeyondTheCap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	served, err := server.Serve(writeDoc(t, root, "review.html", "<p>granted</p>"))
	if err != nil {
		t.Fatalf("Serve() = %v, want no error", err)
	}
	authority := mustAuthority(t, served)

	// Saturate the cap with connections that have completed a request and are now idle
	// keep-alives — the shape the finding describes, and the shape that proves the server
	// ACCEPTED them (a bare dial proves nothing: the kernel completes the handshake into the
	// backlog whether or not anything accepts it).
	held := make([]net.Conn, 0, maxConnections)
	for range maxConnections {
		held = append(held, keepAliveFetch(t, authority, served))
	}
	t.Cleanup(func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	if inFlight := server.listener.(*limitListener).inFlight(); inFlight != maxConnections {
		t.Fatalf("the server holds %d connections, want the cap's %d", inFlight, maxConnections)
	}

	extra, err := net.Dial("tcp", authority)
	if err != nil {
		t.Fatalf("dialling past the cap: %v", err)
	}
	defer func() { _ = extra.Close() }()
	if err := extra.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}
	switch _, err := extra.Read(make([]byte, 1)); {
	case err == nil:
		t.Error("a connection past the cap was answered, want it shed")
	case errors.Is(err, os.ErrDeadlineExceeded):
		t.Error("a connection past the cap was held open, want it shed")
	}

	// The agent's own presentation still works once the flood lets go. The slots come back as the
	// SERVER notices each peer leave, which is prompt but not synchronous with the client's close.
	for _, conn := range held {
		if err := conn.Close(); err != nil {
			t.Fatalf("closing a held connection: %v", err)
		}
	}
	if status := fetchEventually(t, served); status != http.StatusOK {
		t.Errorf("GET after the cap was released = %d, want 200", status)
	}
}

// Close is wired into app shutdown, which cannot know whether this session ever presented
// anything: it must be safe on a server that never started, and safe to call twice. After it, a
// late presentation fails (degrading to the baseline rung) rather than resurrecting a listener.
func TestDocServerCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("a server that never served", func(t *testing.T) {
		t.Parallel()

		server := &DocServer{Host: "127.0.0.1", Root: t.TempDir()}
		if err := server.Close(); err != nil {
			t.Errorf("Close() on an unused server = %v, want no error", err)
		}
		if err := server.Close(); err != nil {
			t.Errorf("Close() again = %v, want no error", err)
		}
	})

	t.Run("a server that served", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		server := &DocServer{Host: "127.0.0.1", Root: root}
		path := writeDoc(t, root, "review.html", "<p>granted</p>")

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
			_ = resp.Body.Close()
			t.Errorf("GET %s after Close() = %d, want a connection failure", served, resp.StatusCode)
		}
	})
}

// A URL is never printed for something that cannot be fetched: the checks that would otherwise
// become a 404 in front of the user happen at Serve, where the caller can still degrade to the
// baseline rung and say what happened.
func TestDocServerRejectsWhatItCannotServe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server := newTestServer(t, root)
	outside := writeDoc(t, t.TempDir(), "secrets.env", "TOKEN=hunter2")
	subdir := filepath.Join(root, "reports")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("making the test subdirectory: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "a blank path is a caller that lost the document", path: "   "},
		{name: "a document that does not exist", path: filepath.Join(root, "missing.html")},
		{name: "a directory", path: subdir},
		{name: "the workspace root itself", path: root},
		{name: "a document outside the workspace the server is fenced to", path: outside},
		{name: "a traversal out of the workspace", path: filepath.Join(root, "..", "elsewhere.html")},
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

			root := t.TempDir()
			server := &DocServer{Host: tt.host, Root: root}
			t.Cleanup(func() { _ = server.Close() })
			path := writeDoc(t, root, "review.html", "<p>granted</p>")

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
	defer func() { _ = occupied.Close() }()
	port := occupied.Addr().(*net.TCPAddr).Port

	root := t.TempDir()
	server := &DocServer{Host: "127.0.0.1", Port: port, Root: root}
	t.Cleanup(func() { _ = server.Close() })

	served, err := server.Serve(writeDoc(t, root, "review.html", "<p>granted</p>"))
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

// keepAliveFetch performs one request over a raw connection and returns that connection still
// open and idle — the state a keep-alive peer parks in, and one the server has demonstrably
// accepted, since it answered.
func keepAliveFetch(t *testing.T, authority, served string) net.Conn {
	t.Helper()

	conn, err := net.Dial("tcp", authority)
	if err != nil {
		t.Fatalf("dialling the doc server: %v", err)
	}
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("setting the connection deadline: %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, served, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	if err := request.Write(conn); err != nil {
		t.Fatalf("writing the request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("draining the response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET over a kept-alive connection = %d, want 200", resp.StatusCode)
	}

	// The request is done; from here the test alone decides when this connection ends.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clearing the connection deadline: %v", err)
	}
	return conn
}

// fetchEventually retries a GET until it succeeds or the deadline passes, and reports the status.
func fetchEventually(t *testing.T, target string) int {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := client.Get(target)
		if err == nil {
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %v", target, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// mustAuthority returns the host:port a served URL is fetched at.
func mustAuthority(t *testing.T, served string) string {
	t.Helper()

	parsed, err := url.Parse(served)
	if err != nil {
		t.Fatalf("Serve() returned an unparseable URL %q: %v", served, err)
	}
	return parsed.Host
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
