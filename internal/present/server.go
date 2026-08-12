package present

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

// tokenBytes is the size of a capability token before hex encoding: 16 bytes of crypto/rand,
// 32 hex characters in the URL (ADR 0019). It is the whole of the access control on a served
// document, so it is sized as a secret rather than as an identifier — 128 bits is not guessable
// by anyone who can reach the port, which on a shared devbox is the threat that matters.
const tokenBytes = 16

// docPathPrefix namespaces every served document under one URL path segment, so that a request
// for anything else — /, /favicon.ico, an editor's speculative probe — is refused by the same
// exact-match rule as a wrong token, with no shape of its own to learn from.
const docPathPrefix = "/d/"

// readHeaderTimeout bounds how long a connection may take to send its request headers. The doc
// server is reachable from whatever can route to this box, so a connection that opens and then
// says nothing must not be able to hold a server goroutine open indefinitely.
const readHeaderTimeout = 10 * time.Second

// writeTimeout bounds how long one response may take to write. It is generous — a PDF fetched
// over a slow link is the case rung 2 exists for — but it is finite: without it a peer that
// stops reading mid-response pins the connection, and its goroutine, forever.
const writeTimeout = 2 * time.Minute

// idleTimeout bounds how long a keep-alive connection may sit between requests. Go falls back to
// ReadTimeout when this is zero and to no timeout at all when that is zero too, which is what the
// server used to do: a peer could complete one request and then hold the connection open for the
// life of the process. A served document is a handful of fetches, so a minute of idleness is
// slack, not a limit anyone reaches.
const idleTimeout = 60 * time.Second

// maxConnections caps how many connections the doc server holds at once. The listener is bound to
// every interface and answers unauthenticated peers (a wrong token is still a served 404), so
// without a cap anything that can route here can open connections until this process runs out of
// file descriptors — which breaks the AGENT's own file and network operations, not just the
// presentation. 32 is far above what a browser fetching one document opens (six or so per host)
// and far below any descriptor budget.
const maxConnections = 32

// documentCSP is the Content-Security-Policy every served document carries. Rung 2 is the ONLY
// rung that still shows active content: .html, .htm, .xhtml and .svg left rung 1's allow-list
// precisely because a file:// launch can carry no policy at all (ADR 0019, fourth amendment
// 2026-08-12), so this header is the whole of the bound here — and the DIRECTIVES are the fix, not
// the header's presence. A permissive policy would satisfy an assertion that a policy exists while
// closing nothing.
//
// `default-src 'none'` is the load-bearing directive. It refuses script, fetch, XHR, WebSocket and
// every subresource load, which is what stops a page that need only have ARRIVED in the clone from
// reaching loopback, RFC1918 and 169.254.169.254 from the browser's network position — reach the
// agent's own network tool does not have, because URLGuard filters exactly those destinations.
// `img-src 'self' data:` and `style-src 'unsafe-inline'` are the narrow re-openings that let a
// self-contained report still render: its own images and its own inline stylesheet, neither of
// which can originate a request to another host.
//
// `sandbox` is not redundant beside them. CSP has no directive for `<meta http-equiv="refresh">`,
// so everything above leaves the NAVIGATION half of the attack open; a BARE sandbox grants no
// allow-* token, which withholds allow-top-navigation and closes it (and withholds allow-scripts
// and allow-forms as a second statement of the directives above). `form-action`, `base-uri` and
// `frame-ancestors` are named explicitly because they do not fall back to `default-src` — leaving
// them off would leave three holes in a policy that reads as closed.
const documentCSP = "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; " +
	"form-action 'none'; base-uri 'none'; frame-ancestors 'none'; sandbox"

// errClosed is returned by Serve after Close: the doc server's lifetime is the app's (ADR 0019 §1
// — nothing live crosses the quiescent boundary), so a presentation arriving during or after
// shutdown must fail rather than resurrect a listener that outlives the process's own teardown.
// The caller degrades to the baseline rung, which is the rung that is never wrong.
var errClosed = errors.New("present: doc server is closed")

// DocServer is rung 2 of the presentation ladder (ADR 0019): the embedded HTTP server that makes
// a presented document reachable from the USER'S machine when Apogee runs on another one — the
// SSH-remoted devbox case, where an opener would open into a display nobody is watching but a URL
// printed in the transcript is one cmd+click away from the user's own browser.
//
// It is a CAPABILITY-TOKEN ALLOWLIST, NOT A FILE SERVER. Nothing is reachable until Serve grants
// it: each presented file gets a fresh random token and is answerable at exactly one URL path,
// /d/<32-hex>/<basename>. There is no root, no directory listing, no path resolution and no
// traversal to be had — every request that is not an exact match for a granted path, prefix walks
// and ".." included, is a 404 that says nothing about what exists. The alternative (serving the
// workspace under a token) was considered and rejected: a per-file grant is the smaller, auditable
// permission on a box the user may share.
//
// The file is RE-READ FROM DISK PER REQUEST rather than captured at Serve time, so re-presenting a
// document after editing it shows the new content, and a document deleted since is a 404 rather
// than a stale copy the server kept. That re-read goes THROUGH THE WORKSPACE FENCE every time
// (security.SafeOpen, pinned at Root): a grant is a (root, workspace-relative name) pair, never a
// resolved path string, so a document replaced after the grant by a symlink pointing out of the
// workspace — `ln -s ~/.apogee/config.yaml report.html`, a write the fence permits because it
// bounds where a file is written and not what a link inside it names — is refused on the next
// request instead of handing the target to whoever holds the token.
//
// The listener starts LAZILY on the first Serve — an Apogee session that never presents a served
// document never opens a port — and is closed by Close on app shutdown. It binds every interface
// (":<port>"), which is the point: the address the user reaches this box on is not one this
// process can bind selectively without guessing, and the advertised address is a separate question
// answered by AdvertiseHost.
//
// Root is the one field with no usable zero: a server with no workspace to fence its grants to
// serves nothing, because a grant it cannot re-check is one it cannot honour safely. Everything
// else defaults sensibly — an ephemeral port, and a URL advertising loopback (honest, if usually
// useless — the baseline rung carries the path regardless). It holds a mutex and must always be
// used through a pointer; go vet's copylocks check enforces that.
type DocServer struct {
	// Host is the address the served URL advertises — what AdvertiseHost returned, already in
	// URL-authority form (an IPv6 literal bracketed). It is deliberately NOT the address the
	// listener binds: this is where the user's machine can reach this one, which nothing here can
	// derive from a bind address. Empty advertises loopbackHost.
	Host string
	// Port is the TCP port to listen on — present.port, default 0, which takes an ephemeral port
	// from the kernel. A stable port buys nothing, because the URL is printed fresh per
	// presentation and carries whatever port was bound.
	Port int
	// Root is the workspace root every grant is fenced to — the same root the tools resolve
	// paths in, handed down by the composition root. It is REQUIRED: Serve refuses a document
	// when it is empty rather than granting a URL nothing can re-check, because the fence is
	// the whole difference between serving a document and serving whatever a symlink named
	// after the grant points at.
	Root string

	// mu guards everything below it: the lazy start, the grant map the handler reads on every
	// request, and the shutdown flag. The critical sections are all short (a map read, a listen)
	// and never span serving a file, which happens outside the lock.
	mu sync.Mutex
	// listener is nil until the first Serve starts it, and again after Close.
	listener net.Listener
	// srv is the running server, kept only so Close can shut it down.
	srv *http.Server
	// files maps a granted URL path (/d/<token>/<basename>) to the fenced grant it names. It is
	// append-only for the life of the server: grants are small, a session makes a handful, and an
	// old URL that keeps working is a feature (it re-reads, so it shows the current document).
	files map[string]grant
	// closed records that Close has run, so a late Serve fails instead of starting a new listener.
	closed bool
}

// grant is one capability: the workspace root a served document is fenced to, and the document's
// name RELATIVE to that root. The pair is deliberately not a resolved absolute path — it is what
// makes the fence re-checkable, because every request re-opens (root, name) through
// security.SafeOpen rather than trusting a string that was inside the workspace once.
type grant struct {
	root string
	name string
}

// errNotRegular is the refusal for a grant target that is not a regular file — a directory at
// Serve time, or a document replaced by one since. It is a sentinel so Serve can render it in its
// own words while handle answers the same bare 404 it gives everything else.
var errNotRegular = errors.New("not a regular file")

// openGranted opens a granted document THROUGH THE WORKSPACE FENCE and describes the descriptor it
// opened: security.SafeOpen pins the root and refuses any component that leaves it (including a
// symlink swapped in after the grant), and the FileInfo is an fstat of that same descriptor, so
// what is checked is what is served with no path re-walked in between. The caller owns Close.
//
// It is shared by Serve and handle on purpose: the grant-time check and the per-request check are
// the same check, so there is one place for it to be right.
func openGranted(g grant) (*os.File, os.FileInfo, error) {
	doc, err := security.SafeOpen(g.root, g.name)
	if err != nil {
		return nil, nil, err
	}

	info, err := doc.Stat()
	if err != nil {
		doc.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		doc.Close()
		return nil, nil, errNotRegular
	}
	return doc, info, nil
}

// Serve grants access to one document and returns the URL at which the user's machine can fetch
// it: http://<advertised host>:<bound port>/d/<32-hex token>/<basename>. The first call starts the
// listener; every later call reuses it and adds another grant.
//
// path must be ABSOLUTE and is the path the tool already resolved inside the workspace root — the
// model never names a file this server has not been handed by the presentation itself. It is
// checked to be an existing regular file here as well, so that a URL is never printed for
// something that cannot be fetched: a link that 404s in front of the user is worse than the
// baseline rung it would have degraded to. That check is made through the same fence the request
// will be, and what is REMEMBERED is the fenced pair (Root, name) — a resolved path that was
// inside the workspace at grant time proves nothing about the file behind it a minute later.
//
// An error means no URL — the caller degrades to the baseline (ADR 0019 §4) and says so in the
// transcript, because a document the user was told about but cannot reach is the one outcome the
// ladder must never produce silently.
func (s *DocServer) Serve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("present: no document path to serve")
	}

	root := strings.TrimSpace(s.Root)
	if root == "" {
		return "", fmt.Errorf("present: cannot serve %q: no workspace root to fence it to", path)
	}

	// The name is measured against the SYMLINK-RESOLVED root (security.WorkspaceRelative), the
	// same way the tools render the path they display, because what arrives here is a real path
	// and the configured root may be reached through a symlink (macOS /tmp). A name that will not
	// relativise stays absolute, which a fenced open accepts only if it is genuinely inside the
	// root — the safe direction.
	granted := grant{root: root, name: security.WorkspaceRelative(path, root)}
	doc, _, err := openGranted(granted)
	if err != nil {
		return "", fmt.Errorf("present: cannot serve %q: %w", path, err)
	}
	doc.Close()

	token, err := newToken()
	if err != nil {
		return "", err
	}

	docPath := docPathPrefix + token + "/" + filepath.Base(path)
	port, err := s.register(docPath, granted)
	if err != nil {
		return "", err
	}

	// url.URL builds the escaping, so a document whose name contains a space or a non-ASCII
	// character produces a URL the terminal can linkify and the server matches back exactly.
	served := url.URL{Scheme: "http", Host: s.authority(port), Path: docPath}
	return served.String(), nil
}

// Close shuts the doc server down and forgets every grant. It is idempotent — a second call, or a
// call on a server that never started, is a no-op — because it is wired into the app's shutdown
// path, which must not care whether this session ever presented anything.
//
// The shutdown is immediate rather than graceful: an in-flight download at shutdown is not worth
// holding the process open for, and the user still has the path from the baseline rung.
func (s *DocServer) Close() error {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.listener, s.files, s.closed = nil, nil, nil, true
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	if err := srv.Close(); err != nil {
		return fmt.Errorf("present: doc server shutdown: %w", err)
	}
	return nil
}

// register records one grant, starting the listener on the first one, and reports the bound port.
// Start and registration share the lock so that two presentations racing on the first call cannot
// bind two listeners.
func (s *DocServer) register(docPath string, doc grant) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errClosed
	}
	if s.listener == nil {
		if err := s.startLocked(); err != nil {
			return 0, err
		}
	}

	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("present: doc server bound a %T, want a TCP address", s.listener.Addr())
	}

	if s.files == nil {
		s.files = make(map[string]grant)
	}
	s.files[docPath] = doc
	return addr.Port, nil
}

// startLocked binds the listener and runs the server. The caller holds s.mu.
//
// The listener is wrapped in the shedding connection cap and every stage of a connection's life
// is bounded by a timeout, because this port answers whoever can route to it: an unauthenticated
// peer must not be able to convert "can reach the box" into "holds this process's descriptors".
//
// The server's ErrorLog is discarded on purpose, twice over: net/http logs to stderr by default,
// which would scribble straight across the Bubble Tea screen and corrupt the frame (the same
// reason the opener detaches its child's streams), and its messages quote request paths — which
// here contain capability tokens, and tokens are never logged (ADR 0019 §3).
func (s *DocServer) startLocked() error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(s.Port))
	if err != nil {
		return fmt.Errorf("present: doc server could not listen on port %d: %w", s.Port, err)
	}
	bounded := newLimitListener(listener, maxConnections)

	srv := &http.Server{
		// A bare HandlerFunc, deliberately NOT an http.ServeMux: the mux cleans request paths and
		// answers a cleaned path with a 301 to the tidy form, which would both hide traversal
		// attempts from the exact-match rule and echo a token back in a Location header.
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          log.New(io.Discard, "", 0),
	}

	s.listener, s.srv = bounded, srv
	go func() { _ = srv.Serve(bounded) }()
	return nil
}

// handle answers one request. There is exactly one way to get a 200 — a GET or HEAD whose path is
// an exact match for a granted URL, naming a file that is still a regular file on disk — and every
// other request in the world gets the same bare 404, with nothing in it to distinguish "wrong
// token" from "no such route" from "the file moved".
func (s *DocServer) handle(w http.ResponseWriter, r *http.Request) {
	// A read-only capability is a read-only capability: anything but a fetch is refused as
	// not-found rather than not-allowed, since a 405 would confirm that the token is real.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	// r.URL.Path is the DECODED path, so a percent-encoded traversal ("%2e%2e") arrives here in
	// the same form a literal one does — and neither matches a granted path, which is the whole
	// defence: this map lookup is the only thing that can turn a request into a file.
	granted, ok := s.lookup(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Re-opened through the fence on EVERY request, not resolved once at grant time: deleted,
	// renamed, no longer a regular file, or swapped for a symlink that leaves the workspace —
	// all of them are the same 404, and the error text stays out of the response rather than
	// describing this box's filesystem or naming the refusal that produced it.
	doc, info, err := openGranted(granted)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer doc.Close()

	// The content type is decided here rather than left to ServeContent's sniffing, so the answer
	// is a function of the extension alone. ServeContent then only adds what it is good at: range
	// requests (a PDF viewer fetches a document in pieces) and conditional GETs.
	w.Header().Set("Content-Type", contentType(granted.name))
	// And nosniff makes that decision binding on the browser too: a document whose extension says
	// text/plain cannot be re-read as markup by content sniffing, so the extension keeps deciding
	// which documents are active at all. The policy below is what bounds the ones that say markup
	// honestly.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", documentCSP)
	http.ServeContent(w, r, "", info.ModTime(), doc)
}

// lookup resolves a request path to the grant it names, under the lock the handler shares with
// Serve. It takes no part in deciding the answer beyond exact equality — and it hands back the
// fenced pair rather than a path, so the handler cannot accidentally open anything else.
func (s *DocServer) lookup(urlPath string) (grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.files[urlPath]
	return doc, ok
}

// authority composes the URL authority the served link carries: the advertised host (loopback when
// none was configured) with the port actually bound. hostForURL is applied here too and is
// idempotent, so a host that arrived already bracketed from AdvertiseHost stays that way and one
// handed over raw is still composed into a legal URL.
func (s *DocServer) authority(port int) string {
	host := strings.TrimSpace(s.Host)
	if host == "" {
		host = loopbackHost
	}
	return hostForURL(host) + ":" + strconv.Itoa(port)
}

// newToken mints one capability token: 16 bytes of cryptographic randomness, hex encoded. The
// error is impossible in practice on every platform Apogee builds for, but it is returned rather
// than ignored — a token from a degraded source would be an access grant nobody meant to make.
func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("present: could not mint a doc server token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// contentType names the media type of a document from its extension, and only from its extension:
// the system table first (mime.TypeByExtension, which knows .html, .svg and .pdf — the browser-
// renderable set rung 2 exists for — as well as whatever the machine adds), then an explicit
// answer for HTML in case a stripped or hostile system table has none, then octet-stream.
//
// The last fallback makes an unrecognised document download instead of rendering, which is the
// conservative direction: the server stays extension-agnostic (ADR 0019 keeps a markdown rendering
// rung additive), and deciding what to do with an unknown type is the browser's job, not ours.
func contentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if declared := mime.TypeByExtension(ext); declared != "" {
		return declared
	}
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
