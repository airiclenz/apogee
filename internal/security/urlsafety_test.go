package security

import (
	"context"
	"errors"
	"net"
	"testing"
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
		{name: "unicode host takes the form the transport dials", raw: "http://ⓖxample.com/", want: "http://gxample.com/"},
		{name: "an ideographic full stop is a label separator", raw: "http://good.example.com。/", want: "http://good.example.com/"},
		{name: "punycode is left alone", raw: "https://xn--mnchen-3ya.de/", want: "https://xn--mnchen-3ya.de/"},
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
