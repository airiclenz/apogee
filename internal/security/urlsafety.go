package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// ----------------------------------------------------------------------------
// URL-safety guard (for the network tools — web-fetch / http-request, P3.11)
// ----------------------------------------------------------------------------

// ErrURLBlocked is returned by URLGuard.Check for a URL the guard rejects (a denied or
// non-allowed scheme, or a denied/non-allowed host). Callers match it (errors.Is) to
// surface a uniform "blocked by url-safety" message.
var ErrURLBlocked = errors.New("security: url blocked by url-safety")

// URLGuard is the scheme/host allow-deny filter the network tools pass a URL through
// before reaching out (D6). It is a value type with no live state, so it is safe to copy
// and to thread into a sub-agent. The zero value permits any http/https URL: an empty
// AllowSchemes falls back to the http/https default, and empty allow/deny host lists
// permit any host.
//
// Precedence is deny-first: a host or scheme on a deny list is blocked even if it also
// matches an allow list. This is the network analogue of path-safety — a guardrail the
// tool inherits from the executor, not a tool's own concern.
type URLGuard struct {
	// AllowSchemes is the set of permitted URL schemes (lower-case, no trailing ":").
	// Empty ⇒ the default {"http", "https"}. A scheme not in the set is blocked.
	AllowSchemes []string
	// AllowHosts, when non-empty, restricts to exactly these hosts (and their
	// subdomains): a host is permitted only if it equals, or is a subdomain of, an
	// entry. Empty ⇒ every host is permitted (subject to DenyHosts).
	AllowHosts []string
	// DenyHosts blocks these hosts (and their subdomains) regardless of AllowHosts —
	// the floor that keeps loopback / metadata endpoints unreachable by default when a
	// caller seeds it.
	DenyHosts []string

	// disableFloor turns the default-on SSRF floor off — only for a test or a deliberately
	// unfenced embedder. It is unexported and set only via DisableIPFloor, so a config
	// merge can never dissolve the floor (tighten-only, mirroring the dangerous-rule
	// semantics): config can ADD denials, never remove the floor (ssrf.go).
	disableFloor bool
	// resolver injects host→IP resolution for the SSRF floor so the tests stay hermetic
	// (no real DNS). nil ⇒ the real net resolver (defaultIPResolver). It is unexported and
	// set via WithResolver; an IP-literal host needs no resolver.
	resolver func(ctx context.Context, host string) ([]net.IP, error)
}

// NewURLGuard builds the network tools' guard from a host's CONFIGURED host lists (the
// `url-safety:` block's allow-hosts / deny-hosts), normalising every entry to the same form
// NormalizeURL puts a dialled host in — so an entry written as "Example.COM." matches the host
// the transport actually reaches. Empty lists ⇒ the zero guard, byte-identical to the default
// before the key existed.
//
// It is the ONE place a Driver turns configuration into a guard, and deliberately so:
// tools.HostTools is composed by hand in two places (the engine's own assembly in
// internal/agent, and the CLI's MCP-aware assembly), so a second hand-written copy of the
// normalise-and-fill loop is exactly how one of those paths would quietly stop applying the
// user's policy while every test stayed green.
//
// The result can only ever TIGHTEN the guard: it fills AllowHosts/DenyHosts and nothing else,
// and the default-on SSRF floor lives behind the unexported disableFloor field (DisableIPFloor
// is the only door and is code-level), so no configuration can dissolve it — the tighten-only
// law this key was deferred under.
func NewURLGuard(allowHosts, denyHosts []string) URLGuard {
	return URLGuard{
		AllowHosts: normalizeHostPatterns(allowHosts),
		DenyHosts:  normalizeHostPatterns(denyHosts),
	}
}

// normalizeHostPatterns normalises every entry of a configured host list and drops the blanks a
// hand-edited YAML list carries. Dropping them is the permissive reading the file-only keys share
// (`tools.disabled:` normalises the same way): a list of nothing but blanks becomes nil — "every
// host, subject to the floor" — rather than an allow-list that matches nothing and blocks the
// whole network over a stray dash.
func normalizeHostPatterns(list []string) []string {
	normalized := make([]string, 0, len(list))
	for _, entry := range list {
		if e := NormalizeHostPattern(entry); e != "" {
			normalized = append(normalized, e)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// DisableIPFloor returns a copy of g with the default-on SSRF floor turned off. It is the
// ONLY way to disable the floor (the floor is on for the zero value and survives any config
// merge), used by a test or a deliberately-unfenced embedder. A config layer cannot reach
// this — it is a code-level opt-out, not a configuration key.
//
// One production path takes it deliberately and at ONE call: internal/mcp checks a configured
// server endpoint through a floor-disabled copy, because a `mcp-servers:` endpoint is the
// user's own config-file address and is never model-supplied, so the anti-model floor is the
// wrong control over it (ADR 0012, Amendment (2026-07-26)). The copy is used for that one
// scheme/host check and threaded nowhere: the connection itself dials under
// PinnedDialControl, which permits that endpoint's own addresses and keeps the floor over
// every other one.
func (g URLGuard) DisableIPFloor() URLGuard {
	g.disableFloor = true
	return g
}

// WithResolver returns a copy of g whose SSRF floor resolves hosts through r — the seam the
// network-tool tests inject a deterministic resolver through so they reach no real DNS.
func (g URLGuard) WithResolver(r func(ctx context.Context, host string) ([]net.IP, error)) URLGuard {
	g.resolver = r
	return g
}

// defaultURLSchemes is the scheme allow-set when URLGuard.AllowSchemes is empty.
var defaultURLSchemes = []string{"http", "https"}

// Check parses raw and reports whether the URL passes the guard, returning an
// ErrURLBlocked-wrapped error (naming the reason) for a rejected URL and for a malformed
// one — the latter a bare "unparseable url", because the parse error's own text quotes the
// URL back and a quoted URL defeats the caller's redaction (M-2, see CheckContext). It runs
// the string-level scheme/host checks and the resolved-IP SSRF floor (resolving with
// context.Background()); CheckContext threads a caller's ctx for cancellable resolution. No
// network beyond DNS resolution is touched.
func (g URLGuard) Check(raw string) error {
	return g.CheckContext(context.Background(), raw)
}

// CheckContext is Check with a caller-supplied ctx for the SSRF floor's host resolution, so
// a slow/blocked DNS lookup is cancellable. It runs in order: normalise (NormalizeURL — the
// guard judges the same string the transport dials), scheme allow, host
// deny/allow (string-level), then the resolved-IP SSRF floor (the default-on tighten-only
// safety net — loopback / IMDS / private / link-local denied by RESOLVED IP, ssrf.go). The
// floor is the pre-flight half of the defence; SafeDialControl re-checks the connected IP at
// dial time to close DNS-rebinding (ssrf.go).
func (g URLGuard) CheckContext(ctx context.Context, raw string) error {
	u, err := NormalizeURL(raw)
	if err != nil {
		// The parse error's own text is deliberately NOT interpolated (M-2). url.Error
		// stringifies as `parse "<url>": <cause>` with a %q verb, and %q ESCAPES what it
		// quotes — so a URL carrying an interior ASCII control character rode out as
		// `…?key=SECRET\x01x` with a LITERAL backslash-x, which the funnel's substring
		// redaction (tools/network.go) searches for as raw bytes and therefore never matched:
		// the key reached the model. The cause names nothing the caller cannot state itself —
		// the reason is that the URL did not parse — so the reason is a constant here and the
		// URL never enters the error text at all.
		return fmt.Errorf("%w: unparseable url", ErrURLBlocked)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: url has no host", ErrURLBlocked)
	}

	scheme := strings.ToLower(u.Scheme)
	if !schemeAllowed(scheme, g.AllowSchemes) {
		return fmt.Errorf("%w: scheme %q is not permitted", ErrURLBlocked, scheme)
	}

	// Already lower-cased, IDNA-mapped and root-dot-stripped by NormalizeURL: the string
	// matched here is the string the transport will dial.
	host := u.Hostname()
	if hostMatches(host, g.DenyHosts) {
		return fmt.Errorf("%w: host %q is denied", ErrURLBlocked, host)
	}
	if len(g.AllowHosts) > 0 && !hostMatches(host, g.AllowHosts) {
		return fmt.Errorf("%w: host %q is not on the allow-list", ErrURLBlocked, host)
	}

	if g.floorEnabled() {
		if err := g.resolveAndCheckFloor(ctx, host); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeURL parses raw and returns it in the ONE form that both this guard and Go's
// transport agree on, so a caller can check and then request the same string. Three
// divergences made "the URL the guard judged" and "the URL the transport dialled" different
// names before it existed:
//
//   - whitespace — the guard parsed the TRIMMED string while the request was built from the
//     untrusted one, which then failed to parse at all;
//   - Unicode — net/http's canonicalAddr runs a non-ASCII host through idna.Lookup.ToASCII
//     before dialling, so http://ⓖxample.com/ was checked as "ⓖxample.com" and dialled as
//     "gxample.com";
//   - the DNS root dot — "evil.com." and "evil.com" resolve identically and virtually every
//     virtual-host server accepts both, but they are different strings to a deny-list match,
//     so appending a dot defeated a DenyHosts entry. Stripping exactly one left the next one
//     behind ("evil.com.." normalised to "evil.com."), which matched no entry either, so the
//     whole trailing run goes — only a bare "." host is left alone.
//
// The normal form is: whitespace-trimmed, host IDNA-mapped (only when non-ASCII, exactly as
// net/http does, and left as-is when the mapping fails so the two still agree), lower-cased,
// with every trailing root dot removed (a bare "." host is left alone). Everything else — scheme, userinfo, port, path,
// query — is untouched. A parse failure is returned as-is; the caller decides the wording.
func NormalizeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}

	host := u.Hostname()
	if host == "" {
		return u, nil
	}
	host = normalizeHostName(host)
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // an IPv6 literal keeps its brackets inside URL.Host
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	u.Host = host
	return u, nil
}

// NormalizeHostPattern reduces one configured host entry (a `url-safety:` allow-hosts /
// deny-hosts line) to the same normal form NormalizeURL leaves a dialled host in, so the two are
// compared as one name rather than two spellings of it: whitespace-trimmed, stripped of one
// surrounding bracket pair (an IPv6 literal is written "[::1]" and dialled as "::1"), IDNA-mapped
// when non-ASCII, lower-cased, with every trailing DNS root dot removed. A blank or all-whitespace
// entry normalises to "" — the caller decides what to do with it (NewURLGuard drops it).
//
// It exists because config is written by a human and the dialled host is produced by net/http:
// "Example.COM." in a deny list must block the host the transport reaches as "example.com", and
// only normalising BOTH sides through the same rules makes that true by construction rather than
// by the author of each list remembering.
func NormalizeHostPattern(pattern string) string {
	return normalizeHostName(strings.TrimSpace(pattern))
}

// normalizeHostName is the host normal form both sides of a match go through: one surrounding
// bracket pair removed, IDNA-mapped when non-ASCII (kept as-is when the mapping fails, exactly as
// net/http does), lower-cased, and with the whole trailing run of root dots removed — a bare "."
// host is left alone. It is deliberately shared by NormalizeURL and NormalizeHostPattern: were the
// config side to grow its own copy, the two forms could drift apart and a correctly-spelled deny
// entry would silently stop matching.
func normalizeHostName(host string) string {
	// An IPv6 host is written in a URL — and therefore in a config entry — inside brackets, but
	// the dialled name the guard compares against carries none (url.URL.Hostname strips them), so
	// "[::1]" in a deny list matched nothing at all. The pair comes off FIRST: idna would refuse
	// the bracketed string, and a trailing "]" is not a dot, so the root-dot loop below could
	// never reach the dots of a bracketed entry either. It is a no-op for NormalizeURL, whose
	// input is already an unbracketed Hostname().
	if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	if !isASCII(host) {
		// net/http.canonicalAddr converts ONLY a non-ASCII host, and keeps the original when
		// the conversion fails — mirroring that exactly is what keeps the checked name and
		// the dialled name the same string whatever idna says.
		if mapped, mapErr := idna.Lookup.ToASCII(host); mapErr == nil {
			host = mapped
		}
	}
	host = strings.ToLower(host)
	for len(host) > 1 && strings.HasSuffix(host, ".") {
		host = host[:len(host)-1]
	}
	return host
}

// isASCII reports whether s is entirely ASCII — the same test net/http applies before
// deciding whether a host needs IDNA conversion at all.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// schemeAllowed reports whether scheme is in allow (or the http/https default when
// allow is empty).
func schemeAllowed(scheme string, allow []string) bool {
	if len(allow) == 0 {
		allow = defaultURLSchemes
	}
	for _, s := range allow {
		if scheme == strings.ToLower(s) {
			return true
		}
	}
	return false
}

// hostMatches reports whether host equals, or is a subdomain of, any entry in list. A
// "sibling-prefix" host (badexample.com vs example.com) does not match — only an exact
// host or a true subdomain (sub.example.com) does.
func hostMatches(host string, list []string) bool {
	for _, entry := range list {
		e := strings.ToLower(strings.TrimSpace(entry))
		if e == "" {
			continue
		}
		if host == e || strings.HasSuffix(host, "."+e) {
			return true
		}
	}
	return false
}
