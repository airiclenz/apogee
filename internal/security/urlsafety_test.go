package security

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"golang.org/x/net/idna"
)

// publicResolver maps every host to a fixed public IP so the existing scheme/host tests stay
// hermetic under the SSRF floor: they exercise the string-level allow/deny logic, not the
// floor, so they must resolve to something the floor permits. Floor behaviour has its own
// dedicated tests (ssrf_test.go).
func publicResolver(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil // example.com's documentation IP
}

func TestURLGuard_Check(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		guard   URLGuard
		url     string
		wantErr bool
	}{
		{"zero guard allows https", URLGuard{}, "https://example.com/path", false},
		{"zero guard allows http", URLGuard{}, "http://example.com", false},
		{"zero guard blocks ftp", URLGuard{}, "ftp://example.com/file", true},
		{"zero guard blocks file scheme", URLGuard{}, "file:///etc/passwd", true},
		{"custom scheme allow", URLGuard{AllowSchemes: []string{"https"}}, "http://example.com", true},
		{"deny host blocks", URLGuard{DenyHosts: []string{"localhost"}}, "http://localhost:8080", true},
		{"deny host subdomain blocks", URLGuard{DenyHosts: []string{"internal.corp"}}, "https://db.internal.corp", true},
		{"allow host permits exact", URLGuard{AllowHosts: []string{"example.com"}}, "https://example.com/x", false},
		{"allow host permits subdomain", URLGuard{AllowHosts: []string{"example.com"}}, "https://api.example.com", false},
		{"allow host blocks other", URLGuard{AllowHosts: []string{"example.com"}}, "https://evil.com", true},
		{"allow host does not match sibling prefix", URLGuard{AllowHosts: []string{"example.com"}}, "https://badexample.com", true},
		{"deny wins over allow", URLGuard{AllowHosts: []string{"example.com"}, DenyHosts: []string{"example.com"}}, "https://example.com", true},
		{"no host is blocked", URLGuard{}, "https:///nohost", true},
		{"relative url blocked (no host)", URLGuard{}, "/just/a/path", true},

		// The normalisation group (M-1): the guard must judge the host the TRANSPORT will
		// dial, not the spelling the caller handed it. Each row below is a name that reaches
		// the denied host while spelling it differently.
		{"trailing root dot does not escape a deny entry", URLGuard{DenyHosts: []string{"example.com"}}, "https://example.com./x", true},
		{"trailing root dot on a subdomain does not escape a deny entry", URLGuard{DenyHosts: []string{"example.com"}}, "https://api.example.com./x", true},
		{"a run of trailing root dots does not escape a deny entry", URLGuard{DenyHosts: []string{"example.com"}}, "https://example.com../x", true},
		{"fullwidth unicode host is denied through its dialled form", URLGuard{DenyHosts: []string{"example.com"}}, "https://ｅｘａｍｐｌｅ.com/x", true},
		{"ideographic full stop is denied through its dialled form", URLGuard{DenyHosts: []string{"example.com"}}, "https://example.com。/x", true},
		{"unicode host is judged on what it maps to, not what it looks like", URLGuard{AllowHosts: []string{"example.com"}}, "https://ⓖxample.com/x", true},
		{"upper-case host is denied", URLGuard{DenyHosts: []string{"example.com"}}, "https://EXAMPLE.COM/x", true},
		{"whitespace-padded url is checked on its trimmed form", URLGuard{AllowHosts: []string{"example.com"}}, "  https://example.com/x  ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.guard.WithResolver(publicResolver).Check(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Check(%q) = nil, want error", tc.url)
				}
				if !errors.Is(err, ErrURLBlocked) {
					t.Errorf("Check(%q) err = %v, want ErrURLBlocked", tc.url, err)
				}
			} else if err != nil {
				t.Fatalf("Check(%q) = %v, want nil", tc.url, err)
			}
		})
	}
}

// TestNormalizeURL_ProducesTheDialledForm pins the normal form itself — the one string both
// the guard and Go's transport agree on. Three spellings used to diverge: whitespace (the
// guard parsed the trimmed URL, the request was built from the padded one and failed to parse
// at all), a non-ASCII host (net/http runs it through idna.Lookup.ToASCII before dialling) and
// the DNS root dot ("evil.com." reaches evil.com but matches no deny entry spelled without it).
// Everything else must survive untouched: scheme, userinfo, port, path, query, and an IPv6
// literal's brackets.
func TestNormalizeURL_ProducesTheDialledForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "already normal", raw: "https://example.com/p?q=1", want: "https://example.com/p?q=1"},
		{name: "whitespace is trimmed", raw: "  https://example.com/p?q=1  ", want: "https://example.com/p?q=1"},
		{name: "host is lower-cased", raw: "https://EXAMPLE.CoM/P", want: "https://example.com/P"},
		{name: "the root dot is stripped", raw: "https://evil.com./x", want: "https://evil.com/x"},
		{name: "two root dots are stripped", raw: "https://evil.com../x", want: "https://evil.com/x"},
		{name: "a run of root dots is stripped", raw: "https://evil.com.../x", want: "https://evil.com/x"},
		{name: "a bare root host keeps its dot", raw: "https://./x", want: "https://./x"},
		{name: "unicode host takes the form the transport dials", raw: "http://ⓖxample.com/", want: "http://gxample.com/"},
		{name: "an ideographic full stop is a label separator", raw: "http://good.example.com。/", want: "http://good.example.com/"},
		{name: "punycode is left alone", raw: "https://xn--mnchen-3ya.de/", want: "https://xn--mnchen-3ya.de/"},
		{name: "a host idna rejects keeps its own bytes", raw: "https://ex_ample.tëst/x", want: "https://ex_ample.t%C3%ABst/x"},
		{name: "userinfo and port survive", raw: " http://user:pw@Example.COM.:8080/p?q=1 ", want: "http://user:pw@example.com:8080/p?q=1"},
		{name: "an ipv6 literal keeps its brackets", raw: "http://[::1]:8080/x", want: "http://[::1]:8080/x"},
		{name: "an ipv4 literal is untouched", raw: "http://127.0.0.1:1/x", want: "http://127.0.0.1:1/x"},
		{name: "a hostless url is untouched", raw: "/just/a/path", want: "/just/a/path"},
		{name: "an unparseable url is an error", raw: "http://a b.com/", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := NormalizeURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeURL(%q) = %q, want an error", tc.raw, u)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeURL(%q): %v", tc.raw, err)
			}
			if got := u.String(); got != tc.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			// The form must be a fixed point: the funnel checks the normalised string and the
			// guard normalises again on its own, so a second pass that moved would put the two
			// back out of step.
			again, err := NormalizeURL(u.String())
			if err != nil {
				t.Fatalf("NormalizeURL is not idempotent — the second pass failed: %v", err)
			}
			if again.String() != u.String() {
				t.Errorf("NormalizeURL is not idempotent: %q then %q", u.String(), again.String())
			}
		})
	}
}

// TestNormalizeURL_KeepsAHostIDNARejects pins the fallback branch: net/http.canonicalAddr
// dials the ORIGINAL host when idna.Lookup.ToASCII refuses to map it, so NormalizeURL must
// hand back those same bytes — not an error, not a partially mapped string — or the guard
// would judge a name the transport never dials. The test asserts the premise too (the profile
// really does reject this host), so a future idna release that starts accepting it fails here
// instead of silently leaving the branch uncovered.
func TestNormalizeURL_KeepsAHostIDNARejects(t *testing.T) {
	t.Parallel()

	const host = "ex_ample.tëst" // non-ASCII, and the underscore is disallowed under STD3 rules

	if mapped, err := idna.Lookup.ToASCII(host); err == nil {
		t.Fatalf("idna.Lookup.ToASCII(%q) = %q, nil — the test needs a host the profile rejects", host, mapped)
	}

	u, err := NormalizeURL("https://" + host + "/x")
	if err != nil {
		t.Fatalf("NormalizeURL(%q): %v", host, err)
	}
	if got := u.Hostname(); got != host {
		t.Errorf("NormalizeURL host = %q, want the original %q", got, host)
	}
}

// TestNormalizeHostPattern_MatchesTheDialledForm pins the config side of the match. A deny entry
// is typed by a human into config.yaml while the host it must block is produced by net/http, so
// the entry only ever bites if both sides land in the same normal form: the same case folding,
// the same IDNA mapping, the same trailing-root-dot removal that NormalizeURL applies to the
// dialled name. Each case therefore asserts the pattern equals what NormalizeURL leaves behind
// for the same spelling — a drift between the two forms fails here rather than as a deny entry
// that quietly stops matching.
func TestNormalizeHostPattern_MatchesTheDialledForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "already normal", raw: "example.com", want: "example.com"},
		{name: "surrounding whitespace is trimmed", raw: "  example.com\t", want: "example.com"},
		{name: "case is folded", raw: "Example.COM", want: "example.com"},
		{name: "the root dot is stripped", raw: "example.com.", want: "example.com"},
		{name: "a run of root dots is stripped", raw: "example.com...", want: "example.com"},
		{name: "case and root dot together", raw: " Example.COM. ", want: "example.com"},
		{name: "unicode takes the form the transport dials", raw: "ⓖxample.com", want: "gxample.com"},
		{name: "an ideographic full stop is a label separator", raw: "good.example.com。", want: "good.example.com"},
		{name: "punycode is left alone", raw: "xn--mnchen-3ya.de", want: "xn--mnchen-3ya.de"},
		{name: "a host idna rejects keeps its own bytes", raw: "ex_ample.tëst", want: "ex_ample.tëst"},
		{name: "a bare root host keeps its dot", raw: ".", want: "."},
		{name: "an ipv4 literal is untouched", raw: "127.0.0.1", want: "127.0.0.1"},
		{name: "an ipv6 literal loses its brackets", raw: "[::1]", want: "::1"},
		{name: "a blank entry normalises to nothing", raw: "   ", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := NormalizeHostPattern(tc.raw)
			if got != tc.want {
				t.Errorf("NormalizeHostPattern(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if got == "" {
				return
			}
			// The load-bearing half: whatever the entry normalises to must be the very string
			// CheckContext compares against, which is NormalizeURL's own host output. The
			// padding is trimmed first because a URL cannot carry it inside its host at all —
			// trimming is the one rule of the form that belongs to the pattern side only.
			u, err := NormalizeURL("https://" + strings.TrimSpace(tc.raw) + "/x")
			if err != nil {
				t.Fatalf("NormalizeURL(%q): %v", tc.raw, err)
			}
			if dialled := u.Hostname(); dialled != got {
				t.Errorf("pattern %q normalises to %q but the dialled host is %q — a deny entry spelled this way would never match", tc.raw, got, dialled)
			}
		})
	}
}

// TestNewURLGuard_BuildsATightenOnlyGuardFromConfig covers the constructor both composition roots
// go through: the configured lists arrive normalised (so a human's spelling still blocks the host
// the transport reaches), blank YAML entries are dropped rather than turned into an allow-list
// that matches nothing, and nothing it fills can weaken the guard — the SSRF floor is on in the
// result exactly as it is in the zero value.
func TestNewURLGuard_BuildsATightenOnlyGuardFromConfig(t *testing.T) {
	t.Parallel()

	t.Run("entries are normalised to the dialled form", func(t *testing.T) {
		t.Parallel()

		g := NewURLGuard(nil, []string{"Example.COM."}).WithResolver(publicResolver)
		if err := g.CheckContext(context.Background(), "https://example.com/x"); !errors.Is(err, ErrURLBlocked) {
			t.Errorf("a config deny spelled %q did not block the host it names: %v", "Example.COM.", err)
		}
		if err := g.CheckContext(context.Background(), "https://other.example/x"); err != nil {
			t.Errorf("the deny entry blocked a host it does not name: %v", err)
		}

		// An IPv6 entry is written the way a URL carries it — in brackets — while the dialled host
		// is compared bracket-free, so the entry only bites if the constructor takes the brackets
		// off. The address is from the documentation range on purpose: the SSRF floor lets it
		// through, so the deny entry is what blocks here and nothing else can pass the test for it.
		const bracketed = "[2001:DB8::1]"
		v6 := NewURLGuard(nil, []string{bracketed}).WithResolver(publicResolver)
		if err := v6.CheckContext(context.Background(), "https://[2001:db8::1]/x"); !errors.Is(err, ErrURLBlocked) {
			t.Errorf("a config deny spelled %q did not block the address it names: %v", bracketed, err)
		}
		if err := v6.CheckContext(context.Background(), "https://[2001:db8::2]/x"); err != nil {
			t.Errorf("the deny entry %q blocked an address it does not name: %v", bracketed, err)
		}
	})

	t.Run("an allow list restricts to its own hosts", func(t *testing.T) {
		t.Parallel()

		g := NewURLGuard([]string{" DOCS.Example.com. "}, nil).WithResolver(publicResolver)
		if err := g.CheckContext(context.Background(), "https://api.docs.example.com/x"); err != nil {
			t.Errorf("a subdomain of the allowed host was refused: %v", err)
		}
		if err := g.CheckContext(context.Background(), "https://elsewhere.example/x"); !errors.Is(err, ErrURLBlocked) {
			t.Errorf("a host outside the allow list was permitted: %v", err)
		}
	})

	t.Run("blank entries do not become an allow-list of nothing", func(t *testing.T) {
		t.Parallel()

		g := NewURLGuard([]string{"", "  "}, []string{" "}).WithResolver(publicResolver)
		if g.AllowHosts != nil || g.DenyHosts != nil {
			t.Errorf("blank entries survived: allow=%q deny=%q", g.AllowHosts, g.DenyHosts)
		}
		if err := g.CheckContext(context.Background(), "https://example.com/x"); err != nil {
			t.Errorf("a list of blanks fenced the whole network off: %v", err)
		}
	})

	t.Run("empty lists leave the zero guard", func(t *testing.T) {
		t.Parallel()

		if got := NewURLGuard(nil, nil); got.AllowHosts != nil || got.DenyHosts != nil ||
			got.AllowSchemes != nil || got.floorEnabled() != (URLGuard{}).floorEnabled() {
			t.Errorf("NewURLGuard(nil, nil) = %+v, want the zero guard", got)
		}
	})

	t.Run("the floor is untouched by configuration", func(t *testing.T) {
		t.Parallel()

		// The tighten-only law from the other side: whatever the config lists say, a loopback
		// host stays refused, because NewURLGuard fills the host lists and nothing else.
		g := NewURLGuard([]string{"localhost"}, nil).
			WithResolver(func(context.Context, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("127.0.0.1")}, nil
			})
		if err := g.CheckContext(context.Background(), "http://localhost/x"); !errors.Is(err, ErrURLBlocked) {
			t.Errorf("config allowing a loopback host dissolved the SSRF floor: %v", err)
		}
	})
}
