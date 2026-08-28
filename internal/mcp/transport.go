package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
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
// floor over every other address the transport is later pointed at. A stdio server is a LOCAL
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
// stdio uses Command/Args/Env; SSE / streamable-http use Endpoint.
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
// workspaceRoot is the exec fence a stdio server's command is measured against; it is unused by
// the two HTTP transports, which launch nothing. The returned cmd, ProcessTeardown and CancelFunc
// are the launched process, the container holding its tree, and the cancel that ends the Cmd's own
// context — all three nil for an HTTP transport, and all three the caller's to run (Client.Close).
func buildTransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard, workspaceRoot string) (mcpsdk.Transport, *exec.Cmd, platform.ProcessTeardown, context.CancelFunc, error) {
	switch cfg.Transport {
	case TransportStdio:
		return buildStdioTransport(cfg, workspaceRoot)
	case TransportSSE:
		transport, err := buildSSETransport(ctx, cfg, guard)
		return transport, nil, nil, nil, err
	case TransportStreamableHTTP:
		transport, err := buildStreamableTransport(ctx, cfg, guard)
		return transport, nil, nil, nil, err
	case "":
		return nil, nil, nil, nil, fmt.Errorf("mcp: server %q has no transport configured", cfg.Name)
	default:
		return nil, nil, nil, nil, fmt.Errorf("mcp: server %q has unknown transport %q (want stdio, sse, or streamable-http)", cfg.Name, cfg.Transport)
	}
}

// buildStdioTransport launches the configured local command and speaks over its stdin/stdout
// (CommandTransport). The command is the host's choice (a trusted launch — no URL floor); an
// empty command is refused so a misconfigured server fails loudly rather than launching nothing.
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
// the leader the SDK's own shutdown signals. The Cmd carries a CANCELLABLE context, returned beside
// it, because platform.NewProcessTeardown wires both cmd.Cancel (the process-group kill) and
// cmd.WaitDelay (the post-exit drain bound) — and exec.Cmd.Start refuses a non-nil Cancel on a Cmd
// built without a context. Neither fires while that context is live, so the cancel is what makes
// them real: Client.Close runs it once the SDK's spec-shaped shutdown has returned, bounding a
// server that outlived it rather than leaving the SDK's cmd.Wait blocked with nothing behind it.
// The context is derived from context.Background, never the connect ctx — a stdio server's
// lifetime is the SESSION, and binding it to the sweep that dialled it would kill every server the
// moment Connect returned.
//
// TRUST NOTE (security-review L4): a configured stdio MCP server is launched with Apogee's FULL
// process environment (cmd.Environ()) plus the per-server cfg.Env, so it sees every secret the
// Apogee process holds (API keys, tokens). This is DELIBERATE and is a conscious trust decision,
// not a leak: the stdio command is chosen by the host in global config (the same trust level as
// the toolchain Apogee invokes), and many MCP servers need inherited PATH/HOME/runtime vars to
// function. It is broader than the git tool's allowlisted env (safeGitEnv) on purpose. An
// optional env-allowlist scrub for stdio MCP launches is parked in ISSUES.md (L4) for a host that
// wants to run a less-trusted stdio server; v1 treats a configured stdio MCP command as fully
// trusted with the process environment.
func buildStdioTransport(cfg ServerConfig, workspaceRoot string) (mcpsdk.Transport, *exec.Cmd, platform.ProcessTeardown, context.CancelFunc, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, nil, nil, nil, fmt.Errorf("mcp: stdio server %q has no command configured", cfg.Name)
	}
	program, err := security.ResolveProgram(nil, cfg.Command, workspaceRoot, nil)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("mcp: stdio server %q: %w", cfg.Name, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, program, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}
	// Built before the SDK starts the command, as the facility requires: on POSIX the process
	// group is a fork-time property of the Cmd, and on Windows the Job Object has to exist before
	// there is a process to assign to it.
	td := platform.NewProcessTeardown(cmd)
	return &mcpsdk.CommandTransport{Command: cmd, TerminateDuration: stdioTerminateDuration}, cmd, td, cancel, nil
}

// stdioTerminateDuration is how long the SDK's stdio shutdown waits at each rung of its ladder
// (stdin close → SIGTERM → SIGKILL) before escalating. Zero means the SDK's own 5s default, which
// is what production runs on: it is a package var only to give the drain test a seam short enough
// to run in milliseconds (a test that shrinks it must not run in parallel).
var stdioTerminateDuration time.Duration

// buildSSETransport builds an SSE client transport after vetting the endpoint, over an
// http.Client pinned to that endpoint's own addresses.
func buildSSETransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	endpoint, client, err := vetEndpoint(ctx, cfg, guard)
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
func buildStreamableTransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	endpoint, client, err := vetEndpoint(ctx, cfg, guard)
	if err != nil {
		return nil, err
	}
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: client,
	}, nil
}

// proxyForRequest resolves the operator's egress proxy for one outbound request: the process's
// HTTP_PROXY / HTTPS_PROXY / NO_PROXY, the same environment the LLM client already honours
// through Go's default transport. There is no per-server `proxy:` config key — the environment
// is the whole surface.
//
// It is a package var only to give the tests a seam (a test that swaps it must not run in
// parallel); production never reassigns it.
var proxyForRequest = http.ProxyFromEnvironment

// vetEndpoint turns a configured HTTP endpoint into the two things an SDK transport needs: the
// ONE normalised endpoint string and the http.Client to speak it over. Both come from the same
// checked form, which is the point — the SDK used to be handed the RAW cfg.Endpoint while the
// guard judged the normalised one, the same check-one-string/dial-another divergence the native
// funnel removed (M-1). Whatever this returns as the endpoint is exactly what url-safety
// approved.
func vetEndpoint(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (string, *http.Client, error) {
	u, err := checkEndpoint(ctx, cfg, guard)
	if err != nil {
		return "", nil, err
	}
	// Pin the connection to the endpoint's own resolved addresses — and, when an egress proxy
	// applies to this endpoint, to the PROXY's addresses too, because that is what the transport
	// actually dials. This is where a private endpoint (or a proxy on loopback or the LAN)
	// becomes reachable — and where a redirect or a rebind to a DIFFERENT private address stays
	// refused. An endpoint, or a proxy, that cannot be resolved fails the connect here, as the
	// endpoint did under the pre-flight floor.
	pinned := []string{u.Hostname()}
	proxyURL, err := proxyForRequest(&http.Request{URL: u})
	if err != nil {
		// The proxy value is deliberately NOT interpolated: the resolver quotes it back and a
		// proxy URL may carry credentials — the same reasoning checkEndpoint's bare wording rests on.
		return "", nil, fmt.Errorf("mcp: server %q: the configured egress proxy is not a usable URL", cfg.Name)
	}
	if proxyURL != nil {
		pinned = append(pinned, proxyURL.Hostname())
	}
	control, err := guard.PinnedDialControl(ctx, pinned...)
	if err != nil {
		return "", nil, fmt.Errorf("mcp: server %q endpoint blocked by url-safety: %w", cfg.Name, err)
	}
	return u.String(), newGuardedHTTPClient(control), nil
}

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
		return nil, fmt.Errorf("mcp: server %q endpoint blocked by url-safety: %w", cfg.Name, err)
	}
	return u, nil
}

// newGuardedHTTPClient builds the http.Client the HTTP transports use: control is the dial-time
// check on the ACTUAL connected IP (PinnedDialControl — the endpoint's own addresses, and the
// egress proxy's when one applies, pass; every other address meets the SSRF floor), so an MCP
// HTTP connection can never skip it.
//
// The order of judgement matches the native network funnel's. The pre-flight (checkEndpoint)
// judges the DESTINATION by string — the operator's scheme/host allow-deny lists — whether or not
// a proxy applies, so a denied host is refused before anything leaves the process. The dial-time
// control judges what is actually DIALLED: the proxy when one is in force, the endpoint itself
// otherwise.
//
// Redirects are NOT followed, the same policy the native network tools apply: a redirect could
// send a vetted connection to an unvetted host, sidestepping the endpoint check. The dial-time
// control would still refuse a private redirect target, but a string-level allow/deny decision
// is made once, on the endpoint, and auto-following would step around it. A server that
// redirects must be configured at the URL it redirects to.
//
// (internal/tools builds the same shape per CALL and drains its pool on the way out; this one is
// long-lived on purpose — it is a session-long server connection, not a one-shot tool call. The
// two builders are deliberately NOT consolidated here; that seam is an architecture-deepening
// candidate, not this change.)
func newGuardedHTTPClient(control func(network, address string, c syscall.RawConn) error) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: control,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxyForRequest,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
