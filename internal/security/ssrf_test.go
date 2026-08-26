package security

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

// fixedResolver maps a single host to ips so the SSRF tests stay hermetic (no real DNS).
func fixedResolver(ips ...net.IP) func(context.Context, string) ([]net.IP, error) {
	return stubResolver(ips, nil)
}

// stubResolver is fixedResolver's error/empty-capable form: it answers every host with the
// same (ips, err) pair, so a test can drive the floor's two fail-closed resolution branches —
// a lookup failure (nil, errNoSuchHost) and an empty answer ([]net.IP{}, nil) — through the
// same hermetic seam a normal answer uses.
func stubResolver(ips []net.IP, err error) func(context.Context, string) ([]net.IP, error) {
	return func(context.Context, string) ([]net.IP, error) { return ips, err }
}

// errNoSuchHost is the shape a resolver reports for a name that does not exist. It is the
// answer the Go pure resolver gives for every inet_aton numeric encoding, and so the thing
// ssrf.go's numeric-encoding note says the safety rests on.
var errNoSuchHost = errors.New("lookup nowhere.example: no such host")

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

		// RFC 8215 NAT64 local-use prefix (64:ff9b:1::/48) — the sibling prefix an operator
		// uses precisely to translate to NON-global v4, so it is the one most likely to front
		// a private target on an IPv6-only network. Denied OUTRIGHT, not decoded: RFC 8215
		// fixes no translation length, so the v4 can sit at four different bit offsets.
		{"nat64 local-use /96-style loopback", "http://[64:ff9b:1::7f00:1]", nil, true, true},     // 127.0.0.1
		{"nat64 local-use /96-style imds", "http://[64:ff9b:1::a9fe:a9fe]", nil, true, true},      // 169.254.169.254
		{"nat64 local-use /96-style private", "http://[64:ff9b:1::c0a8:1]", nil, true, true},      // 192.168.0.1
		{"nat64 local-use /96-style public v4", "http://[64:ff9b:1::5db8:d822]", nil, true, true}, // 93.184.216.34
		// The exact bypass a low-32-bit-only decode leaves open: a /64-style translator inside
		// the /48 puts the v4 at bits 72-103, so the low 32 bits read a public 254.0.0.0 while
		// the gateway forwards to the IMDS at 169.254.169.254.
		{"nat64 local-use /64-style imds", "http://[64:ff9b:1:abcd:a9:fea9:fe00:0]", nil, true, true},
		// The same evasion via the /48- and /56-style embeddings, whose trailing suffix bits are
		// attacker-controlled and here crafted non-zero so no incidental zero-suffix read helps.
		{"nat64 local-use /48-style imds, crafted suffix", "http://[64:ff9b:1:a9fe:a9:fe11:2233:4455]", nil, true, true},
		{"nat64 local-use /56-style imds, crafted suffix", "http://[64:ff9b:1:22a9:fe:a9fe:9988:7766]", nil, true, true},

		// The obsolete v6 transition / site-local ranges, denied outright.
		{"6to4", "http://[2002:7f00:1::1]", nil, true, true},
		{"ipv4-compatible", "http://[::7f00:1]", nil, true, true},
		{"site-local", "http://[fec0::1]", nil, true, true},

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

// TestURLGuard_FloorFailsClosedOnResolution pins the floor's two resolution fail-closed
// branches in resolveAndCheckFloor: a host the resolver cannot resolve, and a host that
// resolves to an empty answer, are both BLOCKED rather than handed on to the transport.
// Neither was covered — fixedResolver never fails and never answers empty — so "improving DX"
// by letting an unresolvable host through would have been a green change, and that edit is
// precisely what turns a numeric-encoded loopback into a pre-flight pass (see
// TestURLGuard_NumericEncodedHostsFailClosed).
func TestURLGuard_FloorFailsClosedOnResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		ips        []net.IP
		resolveErr error
		wantBlock  bool
		wantReason string // the reason the model-facing message must name
	}{
		{
			name:       "resolution failure is blocked",
			resolveErr: errNoSuchHost,
			wantBlock:  true,
			wantReason: "could not resolve host",
		},
		{
			name:       "empty answer is blocked",
			ips:        []net.IP{},
			wantBlock:  true,
			wantReason: "resolved to no addresses",
		},
		{
			// The negative control: the same seam with a real public answer passes, so the two
			// rows above pin the fail-closed branches and not a blanket refusal.
			name:      "public answer passes",
			ips:       []net.IP{net.ParseIP("93.184.216.34")},
			wantBlock: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := URLGuard{}.WithResolver(stubResolver(tc.ips, tc.resolveErr))

			err := g.Check("https://nowhere.example/path")

			if !tc.wantBlock {
				if err != nil {
					t.Fatalf("Check = %v, want nil (a public answer must pass)", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Check = nil, want blocked (%s)", tc.wantReason)
			}
			if !errors.Is(err, ErrURLBlocked) {
				t.Errorf("Check err = %v, want ErrURLBlocked", err)
			}
			if !strings.Contains(err.Error(), tc.wantReason) {
				t.Errorf("Check err = %q, want it to name %q", err, tc.wantReason)
			}
		})
	}
}

// TestURLGuard_NumericEncodedHostsFailClosed pins the invariant ssrf.go's package comment
// states in prose and nobody asserted: a numeric-encoded host — decimal, octal, hex or the
// short "127.1" inet_aton form — is blocked whatever the resolver makes of it. net.ParseIP
// decodes none of these forms, so the pre-flight never classifies them directly and the safety
// rests on what follows resolution; BOTH outcomes must fail closed:
//
//   - the Go pure resolver does no inet_aton decoding and reports "no such host" ⇒ blocked as
//     unresolvable (ErrURLBlocked, not the floor);
//   - a cgo/getaddrinfo resolver DOES decode it and answers 127.0.0.1 ⇒ blocked by the floor
//     (ErrSSRFBlocked) — the case that matters most, because there the decoded address is a
//     real loopback target rather than a name that goes nowhere.
func TestURLGuard_NumericEncodedHostsFailClosed(t *testing.T) {
	t.Parallel()

	// The classic inet_aton encodings of 127.0.0.1: decimal, octal, hex, short form.
	numericForms := []string{"2130706433", "0177.0.0.1", "0x7f.0.0.1", "127.1"}

	t.Run("net.ParseIP decodes none of these forms", func(t *testing.T) {
		t.Parallel()
		// The premise of ssrf.go's numeric-encoding note: because the pre-flight cannot
		// classify these directly, resolution is the bound. If the stdlib ever starts decoding
		// them, that note — not only this test — needs revisiting.
		for _, host := range numericForms {
			if ip := net.ParseIP(host); ip != nil {
				t.Errorf("net.ParseIP(%q) = %v, want nil — ssrf.go's numeric-encoding note assumes these reach the resolver", host, ip)
			}
		}
	})

	modes := []struct {
		name       string
		resolve    func(context.Context, string) ([]net.IP, error)
		wantFloor  bool   // the error must be ErrSSRFBlocked specifically
		wantReason string // the reason the model-facing message must name
	}{
		{
			name:       "go resolver reports no such host",
			resolve:    stubResolver(nil, errNoSuchHost),
			wantReason: "could not resolve host",
		},
		{
			name:       "getaddrinfo decodes it to loopback",
			resolve:    stubResolver([]net.IP{net.ParseIP("127.0.0.1")}, nil),
			wantFloor:  true,
			wantReason: "blocked by the SSRF floor",
		},
	}

	for _, host := range numericForms {
		for _, mode := range modes {
			t.Run(host+" when the "+mode.name, func(t *testing.T) {
				t.Parallel()

				g := URLGuard{}.WithResolver(mode.resolve)

				err := g.Check("http://" + host + "/")

				if err == nil {
					t.Fatalf("Check(http://%s/) = nil, want blocked", host)
				}
				if !errors.Is(err, ErrURLBlocked) {
					t.Errorf("Check(http://%s/) err = %v, want ErrURLBlocked", host, err)
				}
				if mode.wantFloor && !errors.Is(err, ErrSSRFBlocked) {
					t.Errorf("Check(http://%s/) err = %v, want ErrSSRFBlocked", host, err)
				}
				if !strings.Contains(err.Error(), mode.wantReason) {
					t.Errorf("Check(http://%s/) err = %q, want it to name %q", host, err, mode.wantReason)
				}
			})
		}
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

// TestPinnedDialControl_PermitsOnlyTheEndpointsOwnAddresses pins the control that makes the
// MCP endpoint's floor exemption safe (ADR 0012, Amendment 2026-07-26): the addresses the
// configured host itself resolves to pass even though the floor denies them, and EVERY other
// address is judged by the floor exactly as SafeDialControl judges it. The second half is what
// keeps a rebind or a redirect to a different private address refused; without it the exemption
// would be "private addresses are fine on this connection".
func TestPinnedDialControl_PermitsOnlyTheEndpointsOwnAddresses(t *testing.T) {
	t.Parallel()

	// The endpoint is a name resolving to a private address — the localhost/LAN MCP server case.
	g := URLGuard{}.WithResolver(fixedResolver(net.ParseIP("192.168.64.1")))
	ctrl, err := g.PinnedDialControl(context.Background(), "mcp.local")
	if err != nil {
		t.Fatalf("PinnedDialControl: %v", err)
	}

	cases := []struct {
		name      string
		address   string
		wantBlock bool
	}{
		{"the pinned private address is permitted", "192.168.64.1:7331", false},
		{"the pinned address on another port is permitted", "192.168.64.1:9999", false},
		{"the pinned address in v4-mapped form is permitted", "[::ffff:192.168.64.1]:7331", false},
		{"a DIFFERENT private address is blocked", "192.168.64.2:7331", true},
		{"loopback is blocked", "127.0.0.1:7331", true},
		{"the IMDS is blocked", "169.254.169.254:80", true},
		{"a public address is permitted, as the floor permits it", "93.184.216.34:443", false},
		{"a non-IP dial address is blocked", "not-an-ip:80", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ctrl("tcp", tc.address, syscallRawConnNil())
			if tc.wantBlock {
				if err == nil {
					t.Fatalf("control(%q) = nil, want blocked", tc.address)
				}
				if !errors.Is(err, ErrSSRFBlocked) {
					t.Errorf("control(%q) err = %v, want ErrSSRFBlocked", tc.address, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("control(%q) = %v, want permitted", tc.address, err)
			}
		})
	}
}

// TestPinnedDialControl_PinsAnIPLiteralWithoutResolving proves an IP-literal endpoint is pinned
// directly: no lookup happens (the resolver here would fail the build if one did), because the
// address is already the answer.
func TestPinnedDialControl_PinsAnIPLiteralWithoutResolving(t *testing.T) {
	t.Parallel()

	g := URLGuard{}.WithResolver(stubResolver(nil, errNoSuchHost))
	ctrl, err := g.PinnedDialControl(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("PinnedDialControl on an IP literal: %v; want no resolution at all", err)
	}
	if err := ctrl("tcp", "127.0.0.1:7331", syscallRawConnNil()); err != nil {
		t.Errorf("control(pinned loopback) = %v, want permitted", err)
	}
	if err := ctrl("tcp", "127.0.0.2:7331", syscallRawConnNil()); err == nil {
		t.Error("control(a different loopback address) = nil, want blocked")
	}
}

// TestPinnedDialControl_PinsEveryNamedHost pins the variadic form: a caller that has made a
// HOST trust decision about more than one address — an endpoint AND the egress proxy that
// carries it — gets the UNION of their resolved addresses exempted, and nothing else. The
// proxy case is why the union exists: with a proxy in force the transport dials the proxy, so
// its own (often loopback or LAN) addresses have to pass, while every other private address on
// that connection still meets the floor.
func TestPinnedDialControl_PinsEveryNamedHost(t *testing.T) {
	t.Parallel()

	// One resolver, two names: the endpoint and the proxy each resolve to their own private
	// address, and both are named to the control.
	resolve := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "mcp.local":
			return []net.IP{net.ParseIP("192.168.64.1")}, nil
		case "proxy.local":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return nil, errNoSuchHost
	}
	guard := URLGuard{}.WithResolver(resolve)

	ctrl, err := guard.PinnedDialControl(context.Background(), "mcp.local", "proxy.local")
	if err != nil {
		t.Fatalf("PinnedDialControl: %v", err)
	}

	for _, tc := range []struct {
		name      string
		address   string
		wantBlock bool
	}{
		{"the first host's address is permitted", "192.168.64.1:7331", false},
		{"the second host's address is permitted", "127.0.0.1:3128", false},
		{"a third private address is blocked", "10.0.0.1:3128", true},
		{"the IMDS is blocked", "169.254.169.254:80", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ctrl("tcp", tc.address, syscallRawConnNil())

			if tc.wantBlock {
				if !errors.Is(err, ErrSSRFBlocked) {
					t.Fatalf("control(%q) err = %v, want ErrSSRFBlocked", tc.address, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("control(%q) = %v, want permitted", tc.address, err)
			}
		})
	}

	t.Run("no hosts at all fails closed", func(t *testing.T) {
		t.Parallel()

		ctrl, err := guard.PinnedDialControl(context.Background())

		if ctrl != nil {
			t.Error("PinnedDialControl returned a control with nothing to pin")
		}
		if !errors.Is(err, ErrURLBlocked) {
			t.Fatalf("err = %v, want ErrURLBlocked", err)
		}
	})
}

// TestPinnedDialControl_FailsClosedWithoutAddressesToPin pins the fail-closed direction: an
// endpoint whose addresses cannot be learned has nothing to exempt, so no control is returned
// and the caller must refuse the connection rather than dial it unpinned.
func TestPinnedDialControl_FailsClosedWithoutAddressesToPin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		resolver func(context.Context, string) ([]net.IP, error)
		host     string
	}{
		{"resolution failure", stubResolver(nil, errNoSuchHost), "nowhere.example"},
		{"empty answer", stubResolver([]net.IP{}, nil), "nowhere.example"},
		{"no host at all", fixedResolver(net.ParseIP("93.184.216.34")), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl, err := URLGuard{}.WithResolver(tc.resolver).PinnedDialControl(context.Background(), tc.host)
			if err == nil {
				t.Fatal("PinnedDialControl returned a control, want an error")
			}
			if ctrl != nil {
				t.Error("PinnedDialControl returned both a control and an error")
			}
			if !errors.Is(err, ErrURLBlocked) {
				t.Errorf("err = %v, want ErrURLBlocked", err)
			}
		})
	}
}

// TestPinnedDialControl_FloorOff confirms a floor-disabled guard pins nothing and permits
// everything — the same nil-safety SafeDialControl offers, and with no lookup performed.
func TestPinnedDialControl_FloorOff(t *testing.T) {
	t.Parallel()

	ctrl, err := URLGuard{}.DisableIPFloor().WithResolver(stubResolver(nil, errNoSuchHost)).
		PinnedDialControl(context.Background(), "mcp.local")
	if err != nil {
		t.Fatalf("floor-off PinnedDialControl: %v; want no resolution and no error", err)
	}
	if err := ctrl("tcp", "127.0.0.1:80", syscallRawConnNil()); err != nil {
		t.Errorf("floor-off control should permit loopback; got %v", err)
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

		// M-3: the RFC 8215 NAT64 LOCAL-USE prefix 64:ff9b:1::/48 is denied WHOLESALE, not
		// decoded. RFC 8215 fixes no translation prefix length, so the embedded v4 sits at a
		// different bit offset for each of RFC 6052's /48, /56, /64 and /96 forms and the
		// leftover suffix bits are attacker-controlled — any single decode guesses at the
		// operator's translator, and a wrong guess reads a public-looking value while the
		// gateway forwards to a private one. The whole /48 is reserved local-use space with no
		// legitimate public destination, so the range goes rather than a decode of it.
		{"nat64 local-use /96-style loopback 127.0.0.1", "64:ff9b:1::7f00:1", true},
		{"nat64 local-use /96-style imds 169.254.169.254", "64:ff9b:1::a9fe:a9fe", true},
		{"nat64 local-use /96-style private 192.168.0.1", "64:ff9b:1::c0a8:1", true},
		{"nat64 local-use /96-style private 10.0.0.1", "64:ff9b:1::a00:1", true},
		{"nat64 local-use /96-style cgnat 100.64.0.1", "64:ff9b:1::6440:1", true},
		// The bypass a low-32-bit-only decode left open (found by the M-3 verifier): a /64-style
		// translator inside the /48 puts the v4 at bits 72-103, so the low 32 bits read a public
		// 254.0.0.0 while the gateway forwards to the IMDS at 169.254.169.254.
		{"nat64 local-use /64-style imds 169.254.169.254", "64:ff9b:1:abcd:a9:fea9:fe00:0", true},
		// The /48- and /56-style embeddings of the same IMDS address, with a CRAFTED non-zero
		// suffix so nothing is caught incidentally by a zero suffix reading as 0.0.0.0.
		{"nat64 local-use /48-style imds, crafted suffix", "64:ff9b:1:a9fe:a9:fe11:2233:4455", true},
		{"nat64 local-use /56-style imds, crafted suffix", "64:ff9b:1:22a9:fe:a9fe:9988:7766", true},
		// The /48-style embedding of 127.0.0.1 with a zero suffix — blocked by the wholesale
		// deny, not (as a low-32-bit decode would have it) by reading the zero suffix as 0.0.0.0.
		{"nat64 local-use /48-style loopback, zero suffix", "64:ff9b:1:7f00:0:100::", true},
		// A local-use address embedding a PUBLIC v4 is denied too — the deny is the whole /48.
		{"nat64 local-use /96-style public 93.184.216.34", "64:ff9b:1::5db8:d822", true},
		// The boundary control: a neighbour just outside the local-use /48 is not caught by it.
		{"just outside the local-use /48 (public)", "64:ff9b:2::7f00:1", false},

		// M-3: the obsolete v6 transition / site-local ranges, denied outright (no coding-agent
		// fetch legitimately targets them, and each can still carry traffic to a v4 destination).
		{"6to4 embedding loopback", "2002:7f00:1::1", true},
		{"6to4 embedding a public v4", "2002:5db8:d822::1", true},
		{"just outside 6to4 (public)", "2003::1", false},
		{"ipv4-compatible loopback", "::7f00:1", true},
		{"ipv4-compatible public", "::5db8:d822", true},
		{"site-local low", "fec0::1", true},
		{"site-local high", "feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},

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
