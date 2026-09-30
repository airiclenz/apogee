package provider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/airiclenz/apogee/internal/mdns"
)

// dialFunc is the shape of http.Transport.DialContext.
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// lookupLocal resolves a `.local` host over mDNS. It is a variable so a test substitutes a stub;
// a test that swaps it runs serially and restores it with t.Cleanup.
var lookupLocal = mdns.Lookup

// dialAddress makes ONE dial through orig, the dialer captured from the transport being wrapped.
// Both the first dial and every mDNS fallback dial go through it, so a test swaps it to stub what
// a host name resolves to while an IP literal still reaches orig. A test that swaps it runs
// serially and restores it with t.Cleanup.
var dialAddress = func(ctx context.Context, orig dialFunc, network, address string) (net.Conn, error) {
	return orig(ctx, network, address)
}

// providerTransport is the one transport every Client built without WithHTTPClient shares: a
// clone of net/http's DefaultTransport (same pool limits, proxy handling and 30 s connect cap)
// whose dialer falls back to mDNS for a `.local` host the system resolver cannot find. It is one
// pool for the whole process, never one per Client — title and judge build short-lived Clients,
// and a private pool each would strand idle sockets.
var providerTransport = withLocalFallback(http.DefaultTransport.(*http.Transport).Clone())

// withLocalFallback wraps t's own DialContext — never a fresh zero net.Dialer, which would drop
// the connect timeout — with the `.local` mDNS fallback, and returns t. t must carry a
// DialContext, as every clone of DefaultTransport does.
func withLocalFallback(t *http.Transport) *http.Transport {
	orig := t.DialContext
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialWithLocalFallback(ctx, orig, network, address)
	}
	return t
}

// dialWithLocalFallback dials address through orig. When that fails with a DNS error for a host
// ending in `.local`, it asks mDNS for the host's addresses and dials each with the original port
// until one connects. It returns the first dial's error unchanged when the host is not `.local`,
// the failure is not a DNS error, or mDNS finds nothing — so the caller still unwraps to the
// system resolver's *net.DNSError — and the last address's dial error when mDNS answered but no
// address connected.
func dialWithLocalFallback(ctx context.Context, orig dialFunc, network, address string) (net.Conn, error) {
	conn, err := dialAddress(ctx, orig, network, address)
	if err == nil {
		return conn, nil
	}
	host, port, splitErr := net.SplitHostPort(address)
	if splitErr != nil || !isLocalHost(host) {
		return nil, err
	}
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		return nil, err
	}
	addrs, lookupErr := lookupLocal(ctx, host)
	if lookupErr != nil || len(addrs) == 0 {
		return nil, err
	}
	return dialResolved(ctx, orig, network, addrs, port)
}

// dialResolved dials each of addrs at port in order and returns the first connection, or the last
// dial error when none connects.
func dialResolved(ctx context.Context, orig dialFunc, network string, addrs []netip.Addr, port string) (net.Conn, error) {
	var lastErr error
	for _, addr := range addrs {
		conn, err := dialAddress(ctx, orig, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// isLocalHost reports whether host is an mDNS name: it ends in ".local", compared
// case-insensitively, with a trailing dot allowed.
func isLocalHost(host string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSuffix(host, ".")), ".local")
}
