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

// localDialer is the `.local` mDNS fallback around one transport's dialer. dial makes ONE dial;
// both the first dial and every mDNS fallback dial go through it. lookup resolves a `.local` host
// over mDNS. withLocalFallback fills a zero field with its production default — dial with the
// wrapped transport's own DialContext, lookup with mdns.Lookup — so production passes the zero
// value and a test hands in stubs for the transport it builds, touching no package state.
type localDialer struct {
	dial   dialFunc
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

// providerTransport is the one transport every Client built without WithHTTPClient shares: a
// clone of net/http's DefaultTransport (same pool limits, proxy handling and 30 s connect cap)
// whose dialer falls back to mDNS for a `.local` host the system resolver cannot find. It is one
// pool for the whole process, never one per Client — title and judge build short-lived Clients,
// and a private pool each would strand idle sockets.
var providerTransport = withLocalFallback(http.DefaultTransport.(*http.Transport).Clone(), localDialer{})

// withLocalFallback wraps t's own DialContext — never a fresh zero net.Dialer, which would drop
// the connect timeout — with the `.local` mDNS fallback d describes, and returns t. A zero d.dial
// dials through that captured DialContext and a zero d.lookup asks mdns.Lookup. t must carry a
// DialContext, as every clone of DefaultTransport does.
func withLocalFallback(t *http.Transport, d localDialer) *http.Transport {
	if d.dial == nil {
		d.dial = t.DialContext
	}
	if d.lookup == nil {
		d.lookup = mdns.Lookup
	}
	t.DialContext = d.dialWithLocalFallback
	return t
}

// dialWithLocalFallback dials address through d.dial. When that fails with a DNS error for a host
// ending in `.local`, it asks mDNS for the host's addresses and dials each with the original port
// until one connects. It returns the first dial's error unchanged when the host is not `.local`,
// the failure is not a DNS error, or mDNS finds nothing — so the caller still unwraps to the
// system resolver's *net.DNSError — and the last address's dial error when mDNS answered but no
// address connected.
func (d localDialer) dialWithLocalFallback(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.dial(ctx, network, address)
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
	addrs, lookupErr := d.lookup(ctx, host)
	if lookupErr != nil || len(addrs) == 0 {
		return nil, err
	}
	return d.dialResolved(ctx, network, addrs, port)
}

// dialResolved dials each of addrs at port in order and returns the first connection, or the last
// dial error when none connects.
func (d localDialer) dialResolved(ctx context.Context, network string, addrs []netip.Addr, port string) (net.Conn, error) {
	var lastErr error
	for _, addr := range addrs {
		conn, err := d.dial(ctx, network, net.JoinHostPort(addr.String(), port))
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
