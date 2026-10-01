package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ----------------------------------------------------------------------------
// Transport selection (stdio / SSE / streamable-http)
// ----------------------------------------------------------------------------
//
// A ServerConfig names one transport; buildTransport turns it into the SDK Transport the
// Client connects through. The two HTTP transports (SSE / streamable-http) ride
// security.URLGuard's scheme/host allow-deny before connecting, and dial under a control that
// PINS the configured endpoint's own resolved addresses — permitting them, and keeping the SSRF
// floor over every other address the transport is later pointed at — and send every request only
// to the endpoint's own origin (security.OriginPinTransport), so an SSE `endpoint` event naming another
// host, port or scheme fails the connect instead of moving the channel. A stdio server is a LOCAL
// launched subprocess — the host chose the command, a different trust model — so no URL floor
// applies; it meets the exec fence instead (an absolute program resolved on PATH, refused if it
// resolves inside the workspace) and is held in a process group / Job Object the Client reaps at
// Close.
//
// Why the endpoint is exempt from the resolved-IP floor while the native network tools are not
// (ADR 0012, Amendment (2026-07-26)): the floor is the anti-MODEL control — it stops a
// prompt-injected model pivoting to loopback / IMDS / the LAN. An `mcp-servers:` endpoint is
// config-file-only and never model-supplied, and a local or LAN MCP server (`http://127.0.0.1:…`,
// `http://192.168.x.y:…`) is the ordinary case, which the blanket floor made unusable AND fatal
// at startup. Pinning is what keeps the carve-out honest: it is one address the user named, not
// "private addresses are fine on this connection".

// Transport identifies which MCP transport a configured server speaks.
type Transport string

const (
	// TransportStdio launches a local server process and speaks over its stdin/stdout. The
	// host chose the Command, so this is a trusted-launch model (no URL floor); the launched
	// tools still gate through Approval in Auto.
	TransportStdio Transport = "stdio"
	// TransportSSE connects to a remote server over the 2024-11-05 SSE transport at an http(s)
	// Endpoint, filtered by url-safety and pinned to the endpoint's own addresses.
	TransportSSE Transport = "sse"
	// TransportStreamableHTTP connects to a remote server over the streamable-http transport at
	// an http(s) Endpoint, filtered and pinned the same way.
	TransportStreamableHTTP Transport = "streamable-http"
)

// ServerConfig is one configured MCP server. It is a plain value the host folds in from its
// configuration; the Client connects to each. Name is the registry alias that qualifies the
// server's tool names (so two servers' identically named tools stay distinct and the human sees
// which server a call reaches). Exactly one transport's fields are meaningful per Transport:
// stdio uses Command/Args/Env/EnvAllowlist; SSE / streamable-http use Endpoint.
type ServerConfig struct {
	// Name is the server alias — the prefix on each surfaced tool's registry name. Required and
	// must be unique across the configured set (the Client rejects a duplicate or empty name).
	Name string
	// Transport selects stdio / sse / streamable-http. An empty/unknown transport is rejected at
	// connect time rather than silently defaulted.
	Transport Transport

	// Command, Args, Env configure a stdio server (the local process to launch). Command is the
	// executable; Env entries are "KEY=VALUE" appended to the child's environment.
	Command string
	Args    []string
	Env     []string

	// EnvAllowlist optionally narrows what a stdio server INHERITS, for a host that wants to run
	// a less-trusted stdio server than the full-environment default the trust note on
	// buildStdioTransport describes. The pointer is load-bearing — it tells an absent key from an
	// explicitly empty one:
	//
	//   nil            the default: the child inherits apogee's whole environment (plus Env).
	//   &[]string{...} only the named keys survive, plus the platform's own essentials, with PATH
	//                  scoped away from the workspace exactly as the git tool's allowlist is.
	//   &[]string{}    the platform floor alone — nothing of apogee's environment carries over.
	//
	// Env is appended last in every case, so a per-server variable always wins.
	EnvAllowlist *[]string

	// Endpoint is the http(s) URL of an SSE / streamable-http server. It passes the host's
	// scheme/host allow-deny before connecting, and its own resolved addresses are what the
	// connection is pinned to — a private endpoint is allowed (you named it), any OTHER private
	// address on that connection is not.
	Endpoint string
}

// buildTransport constructs the SDK Transport for cfg, applying url-safety to the two HTTP
// transports. guard carries the host's url-safety policy plus the default-on, resolved-IP SSRF
// floor; the endpoint is checked pre-flight here for scheme/host (with ctx bounding the DNS
// lookup that pins it) and the connection dials under PinnedDialControl. An unknown transport, a
// missing or unparseable endpoint, an endpoint url-safety denies, and an endpoint that cannot be
// resolved are all connect-time errors (the Client surfaces them per server).
//
// host supplies the egress proxy resolver the two HTTP transports' client honours and the platform
// facilities a stdio server is launched, scoped and torn down through (see Host).
// workspaceRoot is the exec fence a stdio server's command is measured against; it is unused by
// the two HTTP transports, which launch nothing. The returned cmd, ProcessTeardown and CancelFunc
// are the launched process, the container holding its tree, and the cancel that ends the Cmd's own
// context — all three nil for an HTTP transport, and all three the caller's to run (Client.Close).
func buildTransport(ctx context.Context, host Host, cfg ServerConfig, guard security.URLGuard, workspaceRoot string) (mcpsdk.Transport, *exec.Cmd, platform.ProcessTeardown, context.CancelFunc, error) {
	switch cfg.Transport {
	case TransportStdio:
		return buildStdioTransport(host, cfg, workspaceRoot)
	case TransportSSE:
		transport, err := buildSSETransport(ctx, host, cfg, guard)
		return transport, nil, nil, nil, err
	case TransportStreamableHTTP:
		transport, err := buildStreamableTransport(ctx, host, cfg, guard)
		return transport, nil, nil, nil, err
	case "":
		return nil, nil, nil, nil, fmt.Errorf("mcp: server %q has no transport configured", cfg.Name)
	default:
		return nil, nil, nil, nil, fmt.Errorf("mcp: server %q has unknown transport %q (want stdio, sse, or streamable-http)", cfg.Name, cfg.Transport)
	}
}

// buildStdioTransport prepares the configured local command and returns the stdioTransport that
// launches it and speaks over its stdin/stdout. The command is the host's choice (a trusted launch
// — no URL floor); an empty command is refused so a misconfigured server fails loudly rather than
// launching nothing. The transport is START-FREE: the process exists only once the SDK's
// Client.Connect runs the transport's Connect, which is what lets a test build the Cmd and inspect
// or start it itself.
//
// The command is resolved on PATH through the exec fence (security.ResolveProgram), so what is
// launched is an ABSOLUTE program that does not live inside the workspace: a server binary the
// model could have authored — or a PATH entry pointing into the box — is refused at connect time
// rather than executed. Connect being all-or-nothing, the operator meets that refusal at startup,
// where every other misconfigured server is met. cmd.Dir is deliberately NOT set: the server keeps
// starting in apogee's own working directory, the workspace, which is what filesystem-style MCP
// servers expect, and with argv[0] an absolute fenced path there is no relative lookup left for
// the working directory to decide.
//
// The returned ProcessTeardown holds the launched process's whole tree — a POSIX process group, a
// Windows Job Object — so Client.Close reaps every descendant the server spawned rather than only
// the leader apogee's own shutdown ladder (stdinLadder.Close) signals. The Cmd carries a
// CANCELLABLE context, returned beside it, because platform.NewProcessTeardown wires both
// cmd.Cancel (the process-group kill) and cmd.WaitDelay (the post-exit drain bound) — and
// exec.Cmd.Start refuses a non-nil Cancel on a Cmd built without a context. Neither fires while
// that context is live, so the cancel is what makes them real: Client.Close runs it once the
// ladder has returned, bounding a server that outlived it rather than leaving the ladder's
// cmd.Wait blocked with nothing behind it.
// The context is derived from context.Background, never the connect ctx — a stdio server's
// lifetime is the SESSION, and binding it to the sweep that dialled it would kill every server the
// moment Connect returned.
//
// TRUST NOTE (security-review L4): BY DEFAULT — no env-allowlist: key on the server — a configured
// stdio MCP server is launched with Apogee's FULL process environment (cmd.Environ()) plus the
// per-server cfg.Env, so it sees every secret the Apogee process holds (API keys, tokens). That
// default is DELIBERATE and is a conscious trust decision, not a leak: the stdio command is chosen
// by the host in global config (the same trust level as the toolchain Apogee invokes), and many
// MCP servers need inherited PATH/HOME/runtime vars to function. It is broader than the git tool's
// allowlisted env (gitexec.SafeEnv) on purpose. The opt-in for a host that wants to run a LESS-trusted
// stdio server is cfg.EnvAllowlist (`env-allowlist:` in config): naming it scrubs the launch down
// to those keys plus the platform's essentials, with PATH scoped away from the workspace exactly as
// gitexec.SafeEnv scopes git's, and an explicitly empty list hands the child the platform floor alone.
// cfg.Env is appended last either way, so a per-server variable still wins.
func buildStdioTransport(host Host, cfg ServerConfig, workspaceRoot string) (mcpsdk.Transport, *exec.Cmd, platform.ProcessTeardown, context.CancelFunc, error) {
	host = host.withStdioDefaults()
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, nil, nil, nil, fmt.Errorf("mcp: stdio server %q has no command configured", cfg.Name)
	}
	program, err := security.ResolveProgram(nil, cfg.Command, workspaceRoot, nil)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("mcp: stdio server %q: %w", cfg.Name, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, program, cfg.Args...)
	switch {
	case cfg.EnvAllowlist != nil:
		// The opt-in scrub: only the named keys (plus the platform's essentials) reach the child,
		// with PATH scoped away from the workspace as gitexec.SafeEnv scopes git's. cfg.Env is appended
		// last so a per-server variable still wins over an inherited one of the same name.
		cmd.Env = append(host.Shell.ScopeEnv(workspaceRoot, *cfg.EnvAllowlist, nil), cfg.Env...)
	case len(cfg.Env) > 0:
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}
	// Built before the transport starts the command, as the facility requires: on POSIX the
	// process group is a fork-time property of the Cmd, and on Windows the Job Object has to exist
	// before there is a process to assign to it. The transport carries it too, because Connect is
	// where the process starts and so where it must join its container.
	td := host.NewTeardown(cmd)
	return &stdioTransport{cmd: cmd, td: td, terminateDuration: host.TerminateDuration}, cmd, td, cancel, nil
}

// defaultStdioTerminateDuration is the rung wait a non-positive Host.TerminateDuration maps to:
// 5s, the value the SDK's own CommandTransport defaults to, so replacing that transport with
// stdioTransport changed nothing about how long a clean shutdown is given.
const defaultStdioTerminateDuration = 5 * time.Second

// stdioTransport is apogee's replacement for the SDK's CommandTransport: the same launch-and-speak
// shape, with the server's stdout read through a lineBoundedReader in its JSON-lines framing so
// one message can never grow past maxMCPMessageBytes (bounded.go) however many lines it spans,
// and the shutdown ladder apogee's own (stdinLadder). It is built start-free by
// buildStdioTransport; Connect is what starts the process.
type stdioTransport struct {
	cmd *exec.Cmd
	// td holds the launched process's tree; Connect hands it the process the moment Start
	// returns. Nil only in a transport built by hand without one.
	td                platform.ProcessTeardown
	terminateDuration time.Duration
}

// Connect takes the Cmd's stdout and stdin pipes, starts the process, places it under its teardown
// and connects the SDK's IOTransport over them. The Contain runs immediately after Start, before
// the handshake has sent a byte: on Windows a process joins its Job Object only by assignment after
// CreateProcess, and every descendant it spawns before that assignment escapes the job, so the
// window must be the sub-millisecond gap the tools funnel has (platform.RunWithTeardown), never the
// whole initialize round-trip — and a handshake that goes on to fail still leaves a contained tree
// for connectOne's reap to terminate. A failed Start returns before any session exists, which is
// what lets connectOne reap the never-launched process's teardown. The reader is NopCloser-wrapped, as the
// SDK's CommandTransport wraps it: closing the connection is the stdin ladder alone, never a close
// of the stdout pipe, so a server is asked to exit before it is signalled.
func (t *stdioTransport) Connect(ctx context.Context) (mcpsdk.Connection, error) {
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := t.cmd.Start(); err != nil {
		return nil, err
	}
	if t.td != nil {
		t.td.Contain(t.cmd)
	}
	terminate := t.terminateDuration
	if terminate <= 0 {
		terminate = defaultStdioTerminateDuration
	}
	transport := &mcpsdk.IOTransport{
		Reader: io.NopCloser(&lineBoundedReader{r: stdout, max: maxMCPMessageBytes, framing: frameJSONLines}),
		Writer: &stdinLadder{cmd: t.cmd, stdin: stdin, terminateDuration: terminate},
	}
	return transport.Connect(ctx)
}

// stdinLadder is the write half of a stdio connection: writes go to the server's stdin, and Close
// is the spec-shaped shutdown ladder — the SDK's pipeRWC.Close (mcp/cmd.go:62-99 at v1.6.1)
// re-implemented here so the connection's close stays exactly what it was under CommandTransport.
type stdinLadder struct {
	cmd               *exec.Cmd
	stdin             io.WriteCloser
	terminateDuration time.Duration
}

// Write hands p to the server's stdin.
func (s *stdinLadder) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

// Close runs the stdio shutdown ladder the spec prescribes, in the SDK's pipeRWC.Close order:
// close stdin → wait up to terminateDuration for the server to exit → SIGTERM → wait again →
// SIGKILL → wait again. cmd.Wait runs once, in a goroutine, so each rung can give up on it
// without losing the exit; a failed SIGTERM skips its wait and escalates at once (the SDK's
// Windows behaviour, where the signal is unsupported). The ladder reaches the LEADER alone —
// Client.Close cancels the Cmd's context and reaps the process group after it returns.
func (s *stdinLadder) Close() error {
	if err := s.stdin.Close(); err != nil {
		return fmt.Errorf("closing stdin: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()
	wait := func() (bool, error) {
		select {
		case err := <-exited:
			return true, err
		case <-time.After(s.terminateDuration):
			return false, nil
		}
	}
	if done, err := wait(); done {
		return err
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err == nil {
		if done, err := wait(); done {
			return err
		}
	}
	if err := s.cmd.Process.Kill(); err != nil {
		return err
	}
	if done, err := wait(); done {
		return err
	}
	return errors.New("unresponsive subprocess")
}

// buildSSETransport builds an SSE client transport after vetting the endpoint, over an
// http.Client pinned to that endpoint's own addresses.
func buildSSETransport(ctx context.Context, host Host, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	endpoint, client, err := vetEndpoint(ctx, host, cfg, guard)
	if err != nil {
		return nil, err
	}
	return &mcpsdk.SSEClientTransport{
		Endpoint:   endpoint,
		HTTPClient: client,
	}, nil
}

// buildStreamableTransport builds a streamable-http client transport the same way — same vetting,
// same pinned client.
func buildStreamableTransport(ctx context.Context, host Host, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	endpoint, client, err := vetEndpoint(ctx, host, cfg, guard)
	if err != nil {
		return nil, err
	}
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: client,
	}, nil
}

// vetEndpoint turns a configured HTTP endpoint into the two things an SDK transport needs: the
// ONE normalised endpoint string and the http.Client to speak it over. Both come from the same
// checked form, which is the point — the SDK used to be handed the RAW cfg.Endpoint while the
// guard judged the normalised one, the same check-one-string/dial-another divergence the native
// funnel removed (M-1). Whatever this returns as the endpoint is exactly what url-safety
// approved.
//
// The client is security's guarded client (URLGuard.GuardedClient) under DialPinDestination: the
// connection is pinned to the endpoint's own resolved addresses — and, when host's egress proxy
// applies to this endpoint, to the PROXY's addresses too, because that is what the transport
// actually dials. This is where a private endpoint (or a proxy on loopback or the LAN) becomes
// reachable — and where a redirect or a rebind to a DIFFERENT private address stays refused. An
// endpoint, or a proxy, that cannot be resolved fails the connect here, as the endpoint did under
// the pre-flight floor. Redirects are not followed (a server that redirects must be configured
// at the URL it redirects to), and there is no client timeout: the connection is session-long,
// its requests bounded by their contexts and the per-call deadline instead.
//
// Above the dial control, every request is pinned to the endpoint's canonical origin by
// security's OriginPinTransport, the outermost layer, worded with this package's refusal; the
// SSE transport resolves a server's `endpoint` event with no origin check, and the pin is what
// keeps that event from moving the POST channel anywhere the operator's allow/deny decision was
// never made. Beneath the pin, boundedBodyTransport holds every response body to the message
// bound.
//
// Every refusal names the server — the settings row's reconnect note has no other source for it —
// and none names a proxy value or its credentials.
func vetEndpoint(ctx context.Context, host Host, cfg ServerConfig, guard security.URLGuard) (string, *http.Client, error) {
	u, err := checkEndpoint(ctx, cfg, guard)
	if err != nil {
		return "", nil, err
	}
	client, err := guard.GuardedClient(ctx, u, security.GuardedClientOptions{
		Proxy:  host.Proxy,
		Policy: security.DialPinDestination,
		OriginRefusal: fmt.Errorf("mcp: server %q: %w: a request left the configured endpoint's origin",
			cfg.Name, security.ErrURLBlocked),
		WrapTransport: func(next http.RoundTripper) http.RoundTripper {
			return &boundedBodyTransport{next: next}
		},
	})
	if err != nil {
		return "", nil, endpointRefusal(cfg.Name, err)
	}
	return u.String(), client, nil
}

// endpointRefusal words a GuardedClient refusal in this package's own sentences, unchanged from
// the ones the connect has always produced. An unusable proxy and an origin-less endpoint wrap
// security.ErrURLBlocked bare; a dial target that could not be pinned wraps the pin's own
// failure, never security's PinError text, so the sentence names the host that failed and
// nothing else.
func endpointRefusal(serverName string, err error) error {
	var pinErr *security.PinError
	switch {
	case errors.Is(err, security.ErrProxyUnusable):
		// The proxy value is deliberately NOT interpolated: the resolver quotes it back and a
		// proxy URL may carry credentials — the same reasoning checkEndpoint's bare wording rests on.
		return fmt.Errorf("mcp: server %q: %w: the configured egress proxy is not a usable URL",
			serverName, security.ErrURLBlocked)
	case errors.Is(err, security.ErrNoOrigin):
		return fmt.Errorf("mcp: server %q: %w: the endpoint has no origin to pin", serverName, security.ErrURLBlocked)
	case errors.As(err, &pinErr):
		return fmt.Errorf("mcp: server %q endpoint blocked by url-safety: %w", serverName, pinErr.Err)
	default:
		return fmt.Errorf("mcp: server %q endpoint blocked by url-safety: %w", serverName, err)
	}
}

// ErrEndpointDenied marks the one refusal of an HTTP-transported endpoint that is the OPERATOR's
// own policy rather than a malformed value: the url-safety host lists closed it. It is a sentinel
// because a caller that partitions a set (Admit) has to word the two differently — a closed host is
// news about the policy the human just edited, an unparseable endpoint is news about their typo —
// and the wrapped guard error alone does not separate them. The sentence it contributes is unchanged
// from the one this check has always produced.
var ErrEndpointDenied = errors.New("endpoint blocked by url-safety")

// checkEndpoint refuses an empty or unparseable endpoint, runs the pre-flight url-safety check
// on an HTTP-transported server's endpoint, and returns the ONE normalised form the transport is
// to be given.
//
// The check is scheme/host allow-deny ONLY: the guard's resolved-IP SSRF floor is deliberately
// disabled for it (the single production use of DisableIPFloor). The floor exists to stop the
// MODEL pivoting to internal addresses; an `mcp-servers:` endpoint is config-file-only, so the
// floor there refused the user's own localhost/LAN server and — Connect being all-or-nothing —
// made apogee fail to start, with no config escape. The user's allow/deny host policy still
// applies, and the connection is pinned to this endpoint's addresses by vetEndpoint. See ADR
// 0012, Amendment (2026-07-26).
//
// ctx is threaded into the guard for the same reason it always was — a check that resolves is a
// check that can hang — even though the floorless form performs no lookup itself. The one lookup
// this path now makes is pinning's, in vetEndpoint, under the same ctx.
func checkEndpoint(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (*url.URL, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("mcp: %s server %q has no endpoint configured", cfg.Transport, cfg.Name)
	}
	u, err := security.NormalizeURL(cfg.Endpoint)
	if err != nil {
		// The parse error's own text is deliberately NOT interpolated: it quotes the endpoint
		// back, and a configured endpoint may carry a token in its query — the same reasoning
		// security's own bare "unparseable url" rests on.
		return nil, fmt.Errorf("mcp: server %q has an unparseable endpoint", cfg.Name)
	}
	if err := guard.DisableIPFloor().CheckContext(ctx, u.String()); err != nil {
		return nil, fmt.Errorf("mcp: server %q %w: %w", cfg.Name, ErrEndpointDenied, err)
	}
	return u, nil
}

// endpointRedactor cuts a configured endpoint down to its bare origin wherever it appears in error
// text this package surfaces. The SDK's HTTP transports report failures as *url.Error text such as
// `Post "https://host/mcp?token=SECRET": ...`, which carries the configured URL — userinfo, path
// and query, any of which may hold a credential — past checkEndpoint's parse-time scrub. Every
// occurrence of the endpoint's origin, with any userinfo net/http prints before the host and any
// run of non-space, non-quote characters after it, is replaced by `scheme://host[:port]` alone.
// The run covers any path on the same origin, so an SSE session URL the server announced is cut
// as well as the configured one.
//
// A nil *endpointRedactor is the identity: a stdio server has no endpoint to hide.
type endpointRedactor struct {
	pattern *regexp.Regexp // scheme://[userinfo@]host[:port] followed by a non-space, non-quote run
	origin  string         // scheme://host[:port], the replacement
}

// newEndpointRedactor builds the redactor for an HTTP-transported server from its configured
// endpoint, normalised exactly as checkEndpoint normalises it so the origin matches the URL the
// transport was handed. It returns nil — the identity — for a stdio server, and for an endpoint
// with no scheme or host, which never reached a transport.
func newEndpointRedactor(cfg ServerConfig) *endpointRedactor {
	if cfg.Transport != TransportSSE && cfg.Transport != TransportStreamableHTTP {
		return nil
	}
	u, err := security.NormalizeURL(cfg.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(u.Scheme) + `://(?:[^@/\s"]*@)?` +
		regexp.QuoteMeta(u.Host) + `[^\s"]*`)
	return &endpointRedactor{pattern: pattern, origin: origin}
}

// redact returns text with every occurrence of the endpoint cut to its bare origin.
func (r *endpointRedactor) redact(text string) string {
	if r == nil {
		return text
	}
	return r.pattern.ReplaceAllLiteralString(text, r.origin)
}

// redactErr returns err with its text redacted, keeping the chain intact so a caller's errors.Is
// (context cancellation, the url-safety sentinels) still sees what the transport wrapped.
func (r *endpointRedactor) redactErr(err error) error {
	if r == nil || err == nil {
		return err
	}
	msg := r.redact(err.Error())
	if msg == err.Error() {
		return err
	}
	return &redactedError{msg: msg, err: err}
}

// redactedError is an error whose text has been redacted but whose chain is the original's.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// boundedBodyTransport is the RoundTripper beneath the origin pin: it hands every
// response body to the SDK wrapped in a boundedBody, so an HTTP-transported server meets the
// same 4 MiB message bound (bounded.go) as a stdio server's stdout. The bound is cumulative per
// message, never per line, and the message is read off the Content-Type's base media type: a
// text/event-stream body is bounded per event (the count resets at each blank line), so a
// long-lived SSE stream is never cut for having carried many events, only for one event that
// cannot fit; any other body — a streamable JSON reply above all — is one message, and the read
// errors (never silently truncates) once the whole body passes the cap. The HTTP-lane outcome
// differs from stdio's dead connection: the body read errors; a plain JSON reply fails its call,
// and a streamable SSE reply stalls the call until its ctx, the SDK's retry budget or the
// 5-minute per-call deadline (mcpCallTimeout, tool.go) ends it.
type boundedBodyTransport struct {
	next http.RoundTripper
}

// RoundTrip forwards the request and wraps a successful response's body; an error, or a
// response with no body, passes through untouched.
func (t *boundedBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = &boundedBody{
		lineBoundedReader: lineBoundedReader{
			r:       resp.Body,
			max:     maxMCPMessageBytes,
			framing: framingForContentType(resp.Header.Get("Content-Type")),
		},
		Closer: resp.Body,
	}
	return resp, nil
}

// boundedBody is the io.ReadCloser a wrapped response carries: reads go through the message bound,
// and Close closes the ORIGINAL body. The SDK closes resp.Body itself — the SSE stream in Close,
// handleJSON after its ReadAll, processStream on drain — so a NopCloser here would never close
// the real body and every connection would leak.
type boundedBody struct {
	lineBoundedReader
	io.Closer
}
