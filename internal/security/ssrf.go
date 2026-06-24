package security

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// ----------------------------------------------------------------------------
// SSRF floor (the carried P3.11 /code-review finding) — deny by RESOLVED IP
// ----------------------------------------------------------------------------
//
// The SSRF (server-side request forgery) floor is the default-on, tighten-only
// safety net under URLGuard: it blocks a network tool from reaching addresses that
// are never a legitimate target for a coding-agent fetch and are the classic SSRF
// pivots — loopback, the cloud instance-metadata service (IMDS, 169.254.169.254),
// link-local, the RFC-1918 private ranges, IPv6 unique-local, RFC-6598 carrier-grade
// NAT (100.64.0.0/10), the whole 0.0.0.0/8 "this host/network" block, the TEST-NET /
// benchmark documentation ranges, and the embedded v4 inside a NAT64 well-known-prefix
// address. It is judged by the RESOLVED IP, not the hostname string, so `http://localhost`
// and a DNS name that resolves to a private IP are both caught.
//
// One precision note: a numeric IP encoding the stdlib does NOT parse — decimal
// (`http://2130706433`), octal (`http://0177.0.0.1`), or hex (`http://0x7f.0.0.1`) — is
// NOT normalized to a net.IP here (net.ParseIP returns nil for those forms). Such a host
// falls through to DNS resolution, where the Go resolver does not perform inet_aton-style
// numeric decoding and returns a resolution failure, so the URL is blocked as
// unresolvable. The floor is the bound for every form net.ParseIP DOES decode (dotted-quad
// v4, v6 literals, v4-mapped and the NAT64 well-known prefix); the numeric-encoding safety
// rests on resolution failing, not on the floor.
//
// The floor is a FLOOR: a URLGuard with DenyIPFloor() on (the default) cannot have it
// dissolved by configuration — config can only ADD denials (more DenyHosts / a
// stricter AllowHosts), never remove the floor. This mirrors the dangerous-rule
// tighten-only semantics (MergeDangerousRules): a guardrail can be tightened, never
// loosened, by the invocation environment.
//
// DNS-rebinding / TOCTOU: a pre-flight resolve can be defeated by a name that resolves
// to a public IP at check time and a private IP at connect time. SafeDialControl
// closes that hole by re-validating the ACTUAL connected IP at dial time (the address
// the OS hands the connect syscall), so the floor holds even against a rebinding name.
// The pre-flight Check is the cheap first line; the dial-time control is the real bound.

// ErrSSRFBlocked is returned when an address is denied by the SSRF floor (a resolved IP
// in a blocked range). It wraps ErrURLBlocked so a single errors.Is(err, ErrURLBlocked)
// at the tool boundary catches every url-safety rejection, while a caller that wants to
// distinguish the floor specifically can match ErrSSRFBlocked.
var ErrSSRFBlocked = fmt.Errorf("%w: blocked by the SSRF floor (resolved IP is loopback/private/link-local/metadata/CGNAT/0.0.0.0-8/test-net/NAT64-embedded)", ErrURLBlocked)

// ipResolver resolves a host to its IP addresses. It is a package var (defaulting to the
// real net resolver) so a test can inject a deterministic resolver and the SSRF tests stay
// hermetic — no real DNS, no real network. A URLGuard carries its own optional resolver
// (URLGuard.resolver) so an injected guard overrides this default per-instance.
var defaultIPResolver = func(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// nat64WellKnownPrefix is the IANA NAT64 well-known prefix 64:ff9b::/96 (RFC 6052). A NAT64
// gateway maps the embedded IPv4 (the low 32 bits) onto a real v4 destination, so an address
// in this prefix that embeds a private/loopback v4 reaches that v4 target — a pivot the floor
// must decode and re-check rather than treat as an opaque (public-looking) v6 address.
var nat64WellKnownPrefix = mustCIDR("64:ff9b::/96")

// floorDeniedV4Nets are the IPv4 ranges the floor denies beyond what the stdlib net.IP
// predicate helpers (IsLoopback/IsPrivate/…) already cover: RFC-6598 carrier-grade NAT, the
// whole 0.0.0.0/8 "this host/network" block (only the exact 0.0.0.0 is IsUnspecified), and the
// TEST-NET / benchmarking documentation ranges (never a legitimate fetch target). Parsed once
// as package vars and matched with net.IPNet.Contains so the floor — not a stdlib predicate's
// happening-to-cover — is the explicit bound.
var floorDeniedV4Nets = []*net.IPNet{
	mustCIDR("100.64.0.0/10"),   // RFC-6598 carrier-grade NAT (IsPrivate == false)
	mustCIDR("0.0.0.0/8"),       // "this host on this network" (only 0.0.0.0 is IsUnspecified)
	mustCIDR("192.0.2.0/24"),    // TEST-NET-1 (RFC 5737 documentation)
	mustCIDR("198.51.100.0/24"), // TEST-NET-2
	mustCIDR("203.0.113.0/24"),  // TEST-NET-3
	mustCIDR("198.18.0.0/15"),   // RFC 2544 benchmarking
}

// mustCIDR parses a CIDR at package-init time and panics on a malformed literal — the literals
// are compile-time constants, so a parse failure is a programmer error, not a runtime condition.
func mustCIDR(cidr string) *net.IPNet {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic("security: malformed SSRF floor CIDR " + cidr + ": " + err.Error())
	}
	return n
}

// ipBlockedByFloor reports whether ip falls in a range the SSRF floor denies: loopback
// (127.0.0.0/8, ::1), link-local incl. the cloud IMDS 169.254.169.254 (169.254.0.0/16,
// fe80::/10), the RFC-1918 private ranges (10/8, 172.16/12, 192.168/16), IPv6 unique-local
// (fc00::/7), the unspecified address (0.0.0.0, ::), RFC-6598 carrier-grade NAT (100.64.0.0/10),
// the whole 0.0.0.0/8 block, the TEST-NET / benchmark ranges, the IPv4-mapped form of any of the
// above, and the v4 embedded in a NAT64 well-known-prefix (64:ff9b::/96) address. An
// untrusted/unroutable address is treated as blocked (precision favours safety — a coding agent
// never legitimately fetches these).
func ipBlockedByFloor(ip net.IP) bool {
	if ip == nil {
		return true // an unparseable address is not safe to reach
	}
	// A NAT64 well-known-prefix address (64:ff9b::/96) carries a real IPv4 destination in its
	// low 32 bits; decode it and re-run the v4 checks so an embedded private/loopback target is
	// caught (its To4() is nil and the v6 predicates say "public", so it would otherwise pass).
	if nat64WellKnownPrefix.Contains(ip) {
		if embedded := net.IPv4(ip[12], ip[13], ip[14], ip[15]); ipBlockedByFloor(embedded) {
			return true
		}
	}
	// Normalize an IPv4-mapped IPv6 address (::ffff:a.b.c.d) to its 4-byte form so the
	// IPv4 range checks below catch a private address smuggled through the v6-mapped form.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	if ip.IsLoopback() || // 127.0.0.0/8, ::1
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. IMDS), fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || // 0.0.0.0, ::
		ip.IsPrivate() { // 10/8, 172.16/12, 192.168/16, fc00::/7 (ULA)
		return true
	}
	// The explicit CIDR ranges the stdlib predicates do not cover (CGNAT, 0.0.0.0/8,
	// TEST-NET, benchmark) — checked against the 4-byte form when ip is v4.
	for _, n := range floorDeniedV4Nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resolveAndCheckFloor resolves host and reports an ErrSSRFBlocked error if ANY resolved IP
// falls in a blocked range. It blocks on the first blocked address (a name that resolves to
// both a public and a private IP is denied — the private answer is the SSRF pivot). A
// resolution failure is surfaced as a (wrapped) error so the tool reports it rather than
// reaching out blind.
func (g URLGuard) resolveAndCheckFloor(ctx context.Context, host string) error {
	// A bare IP literal (any form net.ParseIP decodes — dotted-quad v4, a v6 literal, the
	// v4-mapped form, the NAT64 well-known prefix) needs no DNS: classify it directly. A
	// numeric-only encoding net.ParseIP does NOT decode (decimal/octal/hex inet_aton forms)
	// is left to the resolver below, which fails to resolve it and so blocks it as
	// unresolvable — see the package comment.
	if ip := net.ParseIP(host); ip != nil {
		if ipBlockedByFloor(ip) {
			return fmt.Errorf("%w: %s", ErrSSRFBlocked, ip)
		}
		return nil
	}

	resolve := g.resolver
	if resolve == nil {
		resolve = defaultIPResolver
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: could not resolve host %q: %v", ErrURLBlocked, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: host %q resolved to no addresses", ErrURLBlocked, host)
	}
	for _, ip := range ips {
		if ipBlockedByFloor(ip) {
			return fmt.Errorf("%w: %q resolved to %s", ErrSSRFBlocked, host, ip)
		}
	}
	return nil
}

// SafeDialControl returns a net.Dialer Control hook that re-validates the ACTUAL connected
// IP against the SSRF floor at dial time — the defence against DNS-rebinding, where a name
// passes the pre-flight Check (resolving to a public IP) but resolves to a private IP when
// the transport actually connects. The OS hands Control the concrete (network, address) the
// connect syscall will use, so a rebinding name cannot slip a private connect past it.
//
// It is nil-safe to embed in a net.Dialer when the floor is off (returns a Control that
// permits everything), so a tool can always set dialer.Control = guard.SafeDialControl().
func (g URLGuard) SafeDialControl() func(network, address string, c syscall.RawConn) error {
	if !g.floorEnabled() {
		return func(string, string, syscall.RawConn) error { return nil }
	}
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			// No port to split (shouldn't happen for a dialed address); fall back to the
			// whole address so a malformed value fails closed rather than open.
			host = address
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%w: dial address %q is not an IP", ErrSSRFBlocked, address)
		}
		if ipBlockedByFloor(ip) {
			return fmt.Errorf("%w: dial to %s", ErrSSRFBlocked, ip)
		}
		return nil
	}
}

// floorEnabled reports whether the SSRF floor is active for this guard. The floor is ON by
// default (the zero URLGuard has it on); only an explicit DisableIPFloor turns it off (for a
// test or a deliberately-unfenced embedder), and it can never be turned off by config merge.
func (g URLGuard) floorEnabled() bool { return !g.disableFloor }
