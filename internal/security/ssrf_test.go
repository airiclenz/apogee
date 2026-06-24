package security

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

// fixedResolver maps a single host to ips so the SSRF tests stay hermetic (no real DNS).
func fixedResolver(ips ...net.IP) func(context.Context, string) ([]net.IP, error) {
	return func(context.Context, string) ([]net.IP, error) { return ips, nil }
}

func TestURLGuard_SSRFFloor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		url       string
		resolveTo []net.IP // resolver answer for a hostname URL; ignored for an IP-literal URL
		wantErr   bool
		wantFloor bool // expect the error to be ErrSSRFBlocked specifically
	}{
		// A public IP (by name or literal) passes the floor.
		{"public host by name", "https://example.com", []net.IP{net.ParseIP("93.184.216.34")}, false, false},
		{"public ipv4 literal", "https://93.184.216.34", nil, false, false},
		{"public ipv6 literal", "https://[2606:2800:220:1:248:1893:25c8:1946]", nil, false, false},

		// Loopback — by name and by literal, v4 and v6.
		{"loopback by name (localhost)", "http://localhost:8080", []net.IP{net.ParseIP("127.0.0.1")}, true, true},
		{"loopback ipv4 literal", "http://127.0.0.1", nil, true, true},
		{"loopback ipv4 alt", "http://127.5.5.5", nil, true, true},
		{"loopback ipv6 literal", "http://[::1]", nil, true, true},

		// Cloud instance-metadata service (IMDS) — the canonical SSRF target.
		{"IMDS by literal", "http://169.254.169.254/latest/meta-data/", nil, true, true},
		{"IMDS by name", "http://metadata.internal", []net.IP{net.ParseIP("169.254.169.254")}, true, true},

		// Link-local (169.254.0.0/16, fe80::/10).
		{"link-local ipv4", "http://169.254.1.1", nil, true, true},
		{"link-local ipv6", "http://[fe80::1]", nil, true, true},

		// RFC-1918 private ranges — one literal in each, plus a private-resolving hostname.
		{"private 10/8 literal", "http://10.0.0.5", nil, true, true},
		{"private 172.16/12 literal", "http://172.16.4.4", nil, true, true},
		{"private 192.168/16 literal", "http://192.168.1.1", nil, true, true},
		{"private-resolving hostname", "https://intranet.example.com", []net.IP{net.ParseIP("10.1.2.3")}, true, true},

		// IPv6 unique-local (fc00::/7).
		{"ula ipv6 literal", "http://[fd00::1]", nil, true, true},

		// Unspecified / IPv4-mapped private (smuggling a private v4 through a v6-mapped form).
		{"unspecified 0.0.0.0", "http://0.0.0.0", nil, true, true},
		{"ipv4-mapped private", "http://[::ffff:10.0.0.1]", nil, true, true},

		// RFC-6598 carrier-grade NAT (100.64.0.0/10) — IsPrivate() is false, so the floor
		// must catch it explicitly. By literal and by a CGNAT-resolving name.
		{"cgnat literal low", "http://100.64.0.1", nil, true, true},
		{"cgnat literal high", "http://100.127.255.255", nil, true, true},
		{"cgnat by name", "http://metadata.cgnat", []net.IP{net.ParseIP("100.100.0.1")}, true, true},
		// A neighbour just outside CGNAT (100.63/100.128) is public and must pass.
		{"just below cgnat is public", "http://100.63.255.255", nil, false, false},
		{"just above cgnat is public", "http://100.128.0.0", nil, false, false},

		// 0.0.0.0/8 "this host on this network" — only the exact 0.0.0.0 is IsUnspecified;
		// the rest of the /8 routes to localhost on Linux and must be blocked too.
		{"0.0.0.0/8 non-zero", "http://0.1.2.3", nil, true, true},
		{"0.0.0.0/8 high", "http://0.255.255.255", nil, true, true},

		// NAT64 well-known prefix (64:ff9b::/96) embedding a loopback / private v4 — To4() is
		// nil and the v6 predicates say "public", so the floor must decode the embedded v4.
		{"nat64 embeds loopback", "http://[64:ff9b::7f00:1]", nil, true, true}, // 127.0.0.1
		{"nat64 embeds private", "http://[64:ff9b::a00:1]", nil, true, true},   // 10.0.0.1
		{"nat64 embeds imds", "http://[64:ff9b::a9fe:a9fe]", nil, true, true},  // 169.254.169.254
		// A NAT64 address embedding a PUBLIC v4 is a legitimate NAT64 target → passes.
		{"nat64 embeds public", "http://[64:ff9b::5db8:d822]", nil, false, false}, // 93.184.216.34

		// TEST-NET / benchmark documentation ranges — never a legitimate fetch target.
		{"test-net-1", "http://192.0.2.1", nil, true, true},
		{"test-net-2", "http://198.51.100.1", nil, true, true},
		{"test-net-3", "http://203.0.113.1", nil, true, true},
		{"benchmark 198.18/15", "http://198.18.0.1", nil, true, true},

		// A name that resolves to BOTH a public and a private IP is denied (the private
		// answer is the pivot) — DNS-rebinding's first cousin.
		{"mixed public+private resolution", "https://rebind.example.com", []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := URLGuard{}.WithResolver(fixedResolver(tc.resolveTo...))
			err := g.Check(tc.url)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Check(%q) = %v, want nil (public address should pass)", tc.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Check(%q) = nil, want blocked", tc.url)
			}
			if !errors.Is(err, ErrURLBlocked) {
				t.Errorf("Check(%q) err = %v, want ErrURLBlocked", tc.url, err)
			}
			if tc.wantFloor && !errors.Is(err, ErrSSRFBlocked) {
				t.Errorf("Check(%q) err = %v, want ErrSSRFBlocked", tc.url, err)
			}
		})
	}
}

// TestURLGuard_FloorIsTightenOnly proves the floor cannot be dissolved by an AllowHosts
// allow-list: even an explicitly allow-listed host that resolves to a private IP is denied
// (config can add denials, never remove the floor).
func TestURLGuard_FloorIsTightenOnly(t *testing.T) {
	t.Parallel()

	g := URLGuard{AllowHosts: []string{"internal.example.com"}}.
		WithResolver(fixedResolver(net.ParseIP("10.0.0.1")))
	err := g.Check("https://internal.example.com")
	if !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("an allow-listed private-resolving host must still hit the floor; got %v", err)
	}
}

// TestURLGuard_DisableIPFloor confirms the floor can be turned off ONLY by the explicit
// code-level opt-out (a test / unfenced embedder), and that doing so lets a private literal
// through — the negative control that the floor is what was blocking above.
func TestURLGuard_DisableIPFloor(t *testing.T) {
	t.Parallel()

	if err := (URLGuard{}).Check("http://127.0.0.1"); !errors.Is(err, ErrSSRFBlocked) {
		t.Fatalf("floor on by default should block loopback; got %v", err)
	}
	if err := (URLGuard{}).DisableIPFloor().Check("http://127.0.0.1"); err != nil {
		t.Fatalf("floor disabled should permit loopback; got %v", err)
	}
}

// TestSafeDialControl_RebindClosesTOCTOU proves the dial-time control catches a private IP
// the connect syscall actually targets — the DNS-rebinding defence (the connected address is
// validated regardless of what the pre-flight resolve returned).
func TestSafeDialControl_RebindClosesTOCTOU(t *testing.T) {
	t.Parallel()

	ctrl := URLGuard{}.SafeDialControl()
	if ctrl == nil {
		t.Fatal("SafeDialControl returned nil with the floor on")
	}

	cases := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"public connect allowed", "93.184.216.34:443", false},
		{"loopback connect blocked", "127.0.0.1:80", true},
		{"IMDS connect blocked", "169.254.169.254:80", true},
		{"private connect blocked", "10.0.0.1:443", true},
		{"ipv6 loopback connect blocked", "[::1]:80", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ctrl("tcp", tc.address, syscallRawConnNil())
			if tc.wantErr && err == nil {
				t.Fatalf("control(%q) = nil, want blocked", tc.address)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("control(%q) = %v, want allowed", tc.address, err)
			}
			if tc.wantErr && !errors.Is(err, ErrSSRFBlocked) {
				t.Errorf("control(%q) err = %v, want ErrSSRFBlocked", tc.address, err)
			}
		})
	}
}

// TestSafeDialControl_FloorOff confirms a floor-disabled guard's control permits everything
// (so an embedder that opted out is not surprised by a dial-time block).
func TestSafeDialControl_FloorOff(t *testing.T) {
	t.Parallel()

	ctrl := URLGuard{}.DisableIPFloor().SafeDialControl()
	if err := ctrl("tcp", "127.0.0.1:80", syscallRawConnNil()); err != nil {
		t.Fatalf("floor-off control should permit loopback; got %v", err)
	}
}

// TestIPBlockedByFloor table-tests the resolved-IP classifier directly (the single bound both
// the pre-flight resolve and the dial-time control share), so each range's boundary is pinned
// independently of URL parsing/resolution. It covers the SEC-01 additions — CGNAT (100.64/10),
// the whole 0.0.0.0/8, the NAT64 well-known prefix embedding private/loopback v4, and the
// TEST-NET / benchmark ranges — alongside the original loopback/private/link-local/IMDS floor
// and the public-address negative controls.
func TestIPBlockedByFloor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ip   string
		want bool
	}{
		// Original floor — must stay blocked.
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 alt", "127.5.5.5", true},
		{"loopback v6", "::1", true},
		{"imds", "169.254.169.254", true},
		{"link-local v4", "169.254.1.1", true},
		{"link-local v6", "fe80::1", true},
		{"private 10/8", "10.0.0.5", true},
		{"private 172.16/12", "172.16.4.4", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"ula v6", "fd00::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"v4-mapped private", "::ffff:10.0.0.1", true},

		// SEC-01: RFC-6598 carrier-grade NAT (100.64.0.0/10).
		{"cgnat low", "100.64.0.0", true},
		{"cgnat mid", "100.100.50.1", true},
		{"cgnat high", "100.127.255.255", true},
		{"just below cgnat (public)", "100.63.255.255", false},
		{"just above cgnat (public)", "100.128.0.0", false},

		// SEC-01: the whole 0.0.0.0/8, not just the exact unspecified address.
		{"0.0.0.0/8 non-zero", "0.1.2.3", true},
		{"0.0.0.0/8 high", "0.255.255.255", true},
		{"1.0.0.0 is public (boundary)", "1.0.0.0", false},

		// SEC-01: NAT64 well-known prefix 64:ff9b::/96 embeds a real v4 in its low 32 bits.
		{"nat64 embeds loopback 127.0.0.1", "64:ff9b::7f00:1", true},
		{"nat64 embeds private 10.0.0.1", "64:ff9b::a00:1", true},
		{"nat64 embeds imds 169.254.169.254", "64:ff9b::a9fe:a9fe", true},
		{"nat64 embeds cgnat 100.64.0.1", "64:ff9b::6440:1", true},
		{"nat64 embeds public 93.184.216.34", "64:ff9b::5db8:d822", false},

		// SEC-01: TEST-NET / benchmark documentation ranges.
		{"test-net-1", "192.0.2.1", true},
		{"test-net-2", "198.51.100.1", true},
		{"test-net-3", "203.0.113.1", true},
		{"benchmark 198.18/15 low", "198.18.0.1", true},
		{"benchmark 198.18/15 high", "198.19.255.255", true},
		{"just above benchmark (public)", "198.20.0.0", false},

		// Public addresses — must pass.
		{"public v4", "93.184.216.34", false},
		{"public v6", "2606:2800:220:1:248:1893:25c8:1946", false},

		// A nil IP (unparseable) is treated as blocked.
		{"nil ip", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.ip) // "" → nil, the explicit nil-handling case
			if got := ipBlockedByFloor(ip); got != tc.want {
				t.Errorf("ipBlockedByFloor(%q) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// syscallRawConnNil returns a nil syscall.RawConn — SafeDialControl never touches the conn
// (it validates the address string), so a nil is fine for the unit test.
func syscallRawConnNil() syscall.RawConn { return nil }
