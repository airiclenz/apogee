package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/security"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ----------------------------------------------------------------------------
// Transport selection (stdio / SSE / streamable-http)
// ----------------------------------------------------------------------------
//
// A ServerConfig names one transport; buildTransport turns it into the SDK Transport the
// Client connects through. The two HTTP transports (SSE / streamable-http) ride the
// security.URLGuard SSRF floor: the endpoint URL is checked BEFORE connecting and the
// connected IP is re-validated at DIAL time (DNS-rebinding closed), exactly as the native
// network tools are (the trust boundary in doc.go). A stdio server is a LOCAL launched
// subprocess — the host chose the command, a different trust model — so no URL floor applies.

// Transport identifies which MCP transport a configured server speaks.
type Transport string

const (
	// TransportStdio launches a local server process and speaks over its stdin/stdout. The
	// host chose the Command, so this is a trusted-launch model (no URL floor); the launched
	// tools still gate through Approval in Auto.
	TransportStdio Transport = "stdio"
	// TransportSSE connects to a remote server over the 2024-11-05 SSE transport at an http(s)
	// Endpoint, filtered by the SSRF floor.
	TransportSSE Transport = "sse"
	// TransportStreamableHTTP connects to a remote server over the streamable-http transport at
	// an http(s) Endpoint, filtered by the SSRF floor.
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

	// Endpoint is the http(s) URL of an SSE / streamable-http server. It passes the SSRF floor
	// before connecting (loopback / IMDS / private ranges denied by resolved IP).
	Endpoint string
}

// buildTransport constructs the SDK Transport for cfg, applying the SSRF floor to the two HTTP
// transports. guard carries the host's url-safety policy plus the default-on, resolved-IP SSRF
// floor; it is checked pre-flight here (with ctx bounding the floor's DNS resolution) and
// re-checked at dial time via the http.Client's SafeDialControl (closing DNS-rebinding). An
// unknown transport, a missing command/endpoint, or an endpoint the floor rejects is a
// connect-time error (the Client surfaces it per server).
func buildTransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	switch cfg.Transport {
	case TransportStdio:
		return buildStdioTransport(cfg)
	case TransportSSE:
		return buildSSETransport(ctx, cfg, guard)
	case TransportStreamableHTTP:
		return buildStreamableTransport(ctx, cfg, guard)
	case "":
		return nil, fmt.Errorf("mcp: server %q has no transport configured", cfg.Name)
	default:
		return nil, fmt.Errorf("mcp: server %q has unknown transport %q (want stdio, sse, or streamable-http)", cfg.Name, cfg.Transport)
	}
}

// buildStdioTransport launches the configured local command and speaks over its stdin/stdout
// (CommandTransport). The command is the host's choice (a trusted launch — no URL floor); an
// empty command is refused so a misconfigured server fails loudly rather than launching nothing.
//
// TRUST NOTE (security-review L4): a configured stdio MCP server is launched with Apogee's FULL
// process environment (cmd.Environ()) plus the per-server cfg.Env, so it sees every secret the
// Apogee process holds (API keys, tokens). This is DELIBERATE and is a conscious trust decision,
// not a leak: the stdio command is chosen by the host in global config (the same trust level as
// the toolchain Apogee invokes), and many MCP servers need inherited PATH/HOME/runtime vars to
// function. It is broader than the git tool's allowlisted env (safeGitEnv) on purpose. An
// optional env-allowlist scrub for stdio MCP launches is parked in TODO.md (L4) for a host that
// wants to run a less-trusted stdio server; v1 treats a configured stdio MCP command as fully
// trusted with the process environment.
func buildStdioTransport(cfg ServerConfig) (mcpsdk.Transport, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp: stdio server %q has no command configured", cfg.Name)
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}
	return &mcpsdk.CommandTransport{Command: cmd}, nil
}

// buildSSETransport builds an SSE client transport after vetting the endpoint through the SSRF
// floor, with an http.Client whose dial-time control re-validates the connected IP.
func buildSSETransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	if err := checkEndpoint(ctx, cfg, guard); err != nil {
		return nil, err
	}
	return &mcpsdk.SSEClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: newGuardedHTTPClient(guard),
	}, nil
}

// buildStreamableTransport builds a streamable-http client transport after vetting the endpoint
// through the SSRF floor, with the same dial-time-guarded http.Client.
func buildStreamableTransport(ctx context.Context, cfg ServerConfig, guard security.URLGuard) (mcpsdk.Transport, error) {
	if err := checkEndpoint(ctx, cfg, guard); err != nil {
		return nil, err
	}
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: newGuardedHTTPClient(guard),
	}, nil
}

// checkEndpoint refuses an empty endpoint and runs the pre-flight url-safety check (scheme/host
// allow-deny + the resolved-IP SSRF floor) on an HTTP-transported server's endpoint, so a server
// URL resolving to loopback / IMDS / a private range is rejected before any connection is made.
// ctx bounds the floor's DNS resolution so a slow/blocked lookup during connect is cancellable.
func checkEndpoint(ctx context.Context, cfg ServerConfig, guard security.URLGuard) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return fmt.Errorf("mcp: %s server %q has no endpoint configured", cfg.Transport, cfg.Name)
	}
	if err := guard.CheckContext(ctx, cfg.Endpoint); err != nil {
		return fmt.Errorf("mcp: server %q endpoint blocked by url-safety: %w", cfg.Name, err)
	}
	return nil
}

// newGuardedHTTPClient builds the http.Client the HTTP transports use, whose dialer re-checks the
// ACTUAL connected IP against the SSRF floor at dial time (the DNS-rebinding TOCTOU defence) — the
// same construction the native network tools use, kept here so an MCP HTTP connection can never
// skip the dial-time floor.
func newGuardedHTTPClient(guard security.URLGuard) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: guard.SafeDialControl(), // re-validate the connected IP — closes DNS-rebinding
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
