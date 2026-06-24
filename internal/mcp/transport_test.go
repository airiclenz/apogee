package mcp

import (
	"context"
	"net"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// publicResolver maps every host to a public IP so a transport that should build (a non-blocked
// endpoint) does, without touching real DNS. (The SSRF-floor REJECTION path — a blocked endpoint —
// is covered in mcp_test.go's TestBuildTransport_HTTPEndpointBlockedBySSRFFloor.)
func publicResolver(_ context.Context, _ string) ([]net.IP, error) {
	return []net.IP{net.IPv4(93, 184, 216, 34)}, nil // example.com's documented address
}

// TestBuildTransportHTTPKinds asserts the two HTTP transports build their SDK types when the
// endpoint passes the floor — the success side of buildTransport for sse / streamable-http.
func TestBuildTransportHTTPKinds(t *testing.T) {
	t.Parallel()
	guard := security.URLGuard{}.WithResolver(publicResolver)

	sse, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://mcp.example.com/"}, guard)
	if err != nil {
		t.Fatalf("sse buildTransport: %v", err)
	}
	if _, ok := sse.(*mcpsdk.SSEClientTransport); !ok {
		t.Errorf("sse transport = %T; want *mcpsdk.SSEClientTransport", sse)
	}

	sh, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"}, guard)
	if err != nil {
		t.Fatalf("streamable-http buildTransport: %v", err)
	}
	if _, ok := sh.(*mcpsdk.StreamableClientTransport); !ok {
		t.Errorf("streamable-http transport = %T; want *mcpsdk.StreamableClientTransport", sh)
	}
}
