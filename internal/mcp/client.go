package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ----------------------------------------------------------------------------
// The MCP client — connect → list tools → surface → close
// ----------------------------------------------------------------------------
//
// Client owns the live sessions to the configured MCP servers and the tools they
// advertise. Its lifecycle is the design surface (ADR 0012 D3): Connect dials every
// server and lists its tools; Tools surfaces the discovered tools for registration into
// the agent's ToolRegistry as classMCP ExternalEffectTools (gated in Auto for free);
// Close tears down every session (no orphan). A resumed session reconnects FRESH — the
// host calls Connect again on resume; no server-side state is restored (ADR 0008).

// clientName / clientVersion identify Apogee to a server in the MCP handshake.
const (
	clientName    = "apogee"
	clientVersion = "v1.0.0"
)

// Client is a connected MCP client: the live sessions to the configured servers plus the
// tools surfaced from them. It is constructed by Connect and torn down by Close. A nil or
// zero-server Client is valid and dormant (no tools, a no-op Close), so a host without MCP
// configured pays nothing. It is not safe for concurrent use; drive it from the goroutine
// that owns the Agent (the SDK sessions themselves are concurrency-safe, but the Client's own
// connection bookkeeping is not).
type Client struct {
	sessions []*mcpsdk.ClientSession // one per connected server, in config order
	tools    []domain.Tool           // surfaced server tools, in (server, tool) order
}

// Connect dials every configured server in order, lists each server's tools, and returns a
// Client owning the live sessions. guard carries the host's url-safety policy plus the
// default-on SSRF floor applied to the HTTP transports (a stdio server is a trusted local
// launch — no URL floor; see the trust boundary in doc.go).
//
// With no servers configured it returns a dormant Client (no error): the MCP feature is simply
// off. A single server's failure (bad transport, blocked endpoint, unreachable process) is
// surfaced as an error AFTER tearing down whatever connected first — Connect is all-or-nothing
// so a half-wired MCP set never reaches the registry. The host decides whether an MCP failure is
// fatal or a logged degradation; this package does not silently swallow it.
//
// ctx bounds the whole connect sweep (every server's handshake and tools/list). A duplicate or
// empty server name is rejected before any connection is made (the alias must uniquely qualify a
// surfaced tool's registry name).
func Connect(ctx context.Context, servers []ServerConfig, guard security.URLGuard) (*Client, error) {
	if len(servers) == 0 {
		return &Client{}, nil
	}
	if err := validateServers(servers); err != nil {
		return nil, err
	}

	c := &Client{}
	for _, cfg := range servers {
		if err := c.connectOne(ctx, cfg, guard); err != nil {
			// Roll back every session opened so far so a partial connect leaves no orphan, then
			// surface the failure — Connect is all-or-nothing.
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// connectOne dials a single server, lists its tools, and appends the session and the surfaced
// tools to the Client. A connect or list failure is returned (the caller rolls the whole set
// back). The session is recorded BEFORE listing tools so a list failure still tears the
// just-opened session down on rollback.
func (c *Client) connectOne(ctx context.Context, cfg ServerConfig, guard security.URLGuard) error {
	transport, err := buildTransport(ctx, cfg, guard)
	if err != nil {
		return err
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: clientName, Version: clientVersion}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp: connect to server %q: %w", cfg.Name, err)
	}
	c.sessions = append(c.sessions, session)

	tools, err := listServerTools(ctx, cfg.Name, session)
	if err != nil {
		return err
	}
	c.tools = append(c.tools, tools...)
	return nil
}

// listServerTools pages through a server's advertised tools and surfaces each as a serverTool
// bound to the live session. It follows the SDK's cursor pagination so a server advertising more
// than one page is fully discovered. A tool with an empty name from the server is skipped (it
// could never be addressed) rather than failing the whole server.
func listServerTools(ctx context.Context, serverAlias string, session *mcpsdk.ClientSession) ([]domain.Tool, error) {
	var (
		out    []domain.Tool
		cursor string
	)
	for {
		res, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("mcp: list tools from server %q: %w", serverAlias, err)
		}
		for _, t := range res.Tools {
			if t == nil || strings.TrimSpace(t.Name) == "" {
				continue
			}
			out = append(out, newServerTool(serverAlias, t, session))
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

// Tools returns the tools surfaced from every connected server, in (server, tool) discovery
// order — the set the host registers into the Agent's ToolRegistry (each a classMCP
// ExternalEffectTool the dispatch disposition gates in Auto for free). The slice is a fresh copy
// so a caller registering it cannot mutate the Client's bookkeeping.
func (c *Client) Tools() []domain.Tool {
	if c == nil || len(c.tools) == 0 {
		return nil
	}
	out := make([]domain.Tool, len(c.tools))
	copy(out, c.tools)
	return out
}

// Close tears down every live session, returning the joined error of any that failed to close
// cleanly (so a single bad session does not hide the others). It is safe to call on a nil or
// dormant Client (no sessions ⇒ nil) and idempotent in effect — after Close the sessions are
// cleared, so a second Close is a no-op. The host calls it at Agent.Close (no orphaned process
// or connection survives).
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	for _, s := range c.sessions {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	c.sessions = nil
	c.tools = nil
	if len(errs) > 0 {
		return fmt.Errorf("mcp: closing sessions: %w", errors.Join(errs...))
	}
	return nil
}

// validateServers rejects an empty or duplicate server name before any connection is made: the
// name is the alias that qualifies a surfaced tool's registry key, so it must be present and
// unique across the configured set, else two servers' tools would collide or be unaddressable.
func validateServers(servers []ServerConfig) error {
	seen := make(map[string]bool, len(servers))
	for i, cfg := range servers {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			return fmt.Errorf("mcp: server #%d has an empty name", i)
		}
		if seen[name] {
			return fmt.Errorf("mcp: duplicate server name %q", name)
		}
		seen[name] = true
	}
	return nil
}
