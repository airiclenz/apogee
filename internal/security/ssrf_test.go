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

// syscallRawConnNil returns a nil syscall.RawConn — SafeDialControl never touches the conn
// (it validates the address string), so a nil is fine for the unit test.
func syscallRawConnNil() syscall.RawConn { return nil }
