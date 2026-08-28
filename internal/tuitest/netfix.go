package tuitest

// The network a driven run is allowed to have. Everything here listens on loopback, and nothing
// here reaches anything that is not in this process — which is the whole point: T-18's claims are
// about apogee's EGRESS decisions (does it use the operator's proxy, does the SSRF floor refuse a
// private destination, does an `mcp-servers:` endpoint go the same way), and a claim about egress
// cannot be settled by watching a stranger's server.
//
// The instrument is a real forward proxy rather than a stub of one. A proxied client speaks a
// different protocol to a different address — absolute request URIs to the proxy, not paths to the
// destination — so a fake that only counted calls would pass whether or not apogee ever set
// http.Transport.Proxy. What this proxy records is what a proxy actually received.
//
// Reaching a destination without leaving loopback is the second half. A destination the SSRF floor
// would refuse (127.0.0.1, 10/8, …) is refused BEFORE the proxy question is asked, so the
// destination has to be an address the floor calls public; and a public address that resolved
// honestly would put a packet on the wire. The route table closes that gap: the caller names a
// public-but-unroutable literal (240.0.0.0/4 — reserved, allocated to nobody, and in none of the
// floor's denied ranges), and the proxy dials the loopback server standing in for it. apogee makes
// a genuinely public request; the bytes never leave the machine.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// hopByHopHeaders are the headers a proxy consumes rather than forwards (RFC 9110 §7.6.1). They
// describe THIS connection, and passing them on is how a proxied request grows a Transfer-Encoding
// the destination never asked for.
var hopByHopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// Proxy is an in-test HTTP forward proxy on loopback with an access log: the operator's egress
// proxy, as a thing a test can point HTTP_PROXY at and then read back.
//
// It is deliberately not an httputil.ReverseProxy — a forward proxy is addressed by absolute
// request URI, and refusing anything else is what makes the log's entries evidence that the CLIENT
// was proxying rather than that something happened to connect.
type Proxy struct {
	srv *httptest.Server
	tr  *http.Transport

	mu  sync.Mutex
	log []string
}

// ForwardProxy starts a forward proxy on loopback and returns once it is listening. It stops with
// the test.
//
// routes maps a destination HOST (no port) to the `host:port` actually dialled for it, which is how
// a public destination is served from loopback — see the file comment. A destination with no route
// is REFUSED rather than dialled as it stands: the header's guarantee that nothing a driven run
// reaches leaves this process is enforced here instead of assumed, and an unmapped host arrives at
// the caller as the proxy's own 502 naming it.
func ForwardProxy(t testing.TB, routes map[string]string) *Proxy {
	t.Helper()

	p := &Proxy{}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	p.tr = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, _, err := net.SplitHostPort(addr); err == nil {
				if to, ok := routes[host]; ok {
					return dialer.DialContext(ctx, network, to)
				}
			}
			return nil, fmt.Errorf("tuitest: no route for %s — every host a driven run may reach is mapped", addr)
		},
		// No proxy of its own: a proxy that honoured the developer's own HTTP_PROXY would send
		// the suite's traffic to it.
		Proxy:               nil,
		MaxIdleConns:        10,
		IdleConnTimeout:     5 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(func() {
		p.srv.Close()
		p.tr.CloseIdleConnections()
	})
	return p
}

// URL is the proxy's own address, in the form HTTP_PROXY takes.
func (p *Proxy) URL() string { return p.srv.URL }

// Log is every request the proxy received, newest last, as `METHOD absolute-URL`. A proxied request
// carries an absolute URL by definition, so the entries name the DESTINATION and not the proxy.
func (p *Proxy) Log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.log...)
}

// Saw counts the logged requests whose destination host is host — the "did this host's traffic go
// through the proxy?" question, asked without the caller having to parse the log.
func (p *Proxy) Saw(host string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, entry := range p.log {
		if proxyLogHost(entry) == host {
			n++
		}
	}
	return n
}

// proxyLogHost is the destination host of one log entry, port stripped.
func proxyLogHost(entry string) string {
	_, raw, ok := strings.Cut(entry, " ")
	if !ok {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// serve forwards one proxied request and records it. A request that is not in the absolute form a
// proxy is addressed with is refused rather than guessed at: it means the client was not proxying,
// which is the failure the test is looking for.
func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "tuitest: the forward proxy was addressed with a non-absolute request URI",
			http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.log = append(p.log, r.Method+" "+r.URL.String())
	p.mu.Unlock()

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Close = false
	for _, h := range hopByHopHeaders {
		out.Header.Del(h)
	}
	resp, err := p.tr.RoundTrip(out)
	if err != nil {
		http.Error(w, "tuitest: forward proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for name, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	flusher, _ := w.(http.Flusher)
	w.WriteHeader(resp.StatusCode)
	// Flushed before a single body byte is copied, because WriteHeader only BUFFERS the status
	// line. An MCP session's standalone SSE stream opens with headers and then says nothing until
	// the server has something to push, so a proxy that waited for the first byte to flush would
	// leave the client blocked on headers that had already arrived — the connect hangs, and the
	// program hangs with it.
	if flusher != nil {
		flusher.Flush()
	}
	flushingCopy(w, flusher, resp.Body)
}

// flushingCopy streams a body through, flushing after every chunk. An MCP streamable-http reply is
// an SSE stream that stays open for the session, so a copy that buffered would hold the whole
// connection until it closed — the client would see nothing and the connect would hang.
func flushingCopy(w io.Writer, flusher http.Flusher, body io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// Page is a loopback server standing in for one web page, with a hit counter. The counter is what
// makes "the redirect target got no request" a claim: an absence is only evidence when something
// was watching the place the request would have arrived.
type Page struct {
	srv  *httptest.Server
	hits atomic.Int64
}

// PageServer starts a server on loopback that answers every path with body, and stops with the test.
func PageServer(t testing.TB, body string) *Page {
	t.Helper()

	p := &Page{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// Addr is the `host:port` the page is actually served from — what a route table maps a public
// destination onto.
func (p *Page) Addr() string { return p.srv.Listener.Addr().String() }

// URL is the page's own loopback URL, for a caller that reaches it directly.
func (p *Page) URL() string { return p.srv.URL }

// Hits is how many requests the page has answered.
func (p *Page) Hits() int { return int(p.hits.Load()) }

// echoToolSchema is the input schema [MCPEcho]'s one tool advertises, so a surfaced MCP tool
// carries a real schema rather than an empty object.
var echoToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"text": map[string]any{"type": "string", "description": "text to echo back"},
	},
	"required": []any{"text"},
}

// EchoServer is a streamable-http MCP server on loopback exposing a single `echo` tool.
type EchoServer struct {
	srv *httptest.Server
}

// MCPEcho starts that server and stops it with the test. It is the HTTP twin of internal/mcp's own
// stdio fixture: one deterministic tool, so a test can assert that a configured `mcp-servers:` entry
// reached a real MCP handshake — tool discovery and all — rather than that a socket accepted.
//
// DNS-rebinding protection is off because this fixture exists to be reached THROUGH a proxy: the
// forwarded request arrives on loopback carrying the destination's own Host header, which is exactly
// the shape the SDK's default rejects with 403.
func MCPEcho(t testing.TB) *EchoServer {
	t.Helper()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "apogee-tuitest-echo", Version: "v0.0.1"}, nil)
	server.AddTool(
		&mcpsdk.Tool{Name: "echo", Description: "Echo the text argument back.", InputSchema: echoToolSchema},
		func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var args struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(req.Params.Arguments, &args)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo: " + args.Text}},
			}, nil
		},
	)
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	e := &EchoServer{srv: httptest.NewServer(handler)}
	t.Cleanup(e.srv.Close)
	return e
}

// Addr is the `host:port` the MCP server is actually served from.
func (e *EchoServer) Addr() string { return e.srv.Listener.Addr().String() }
