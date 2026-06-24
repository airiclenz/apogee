package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
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

// DisableIPFloor returns a copy of g with the default-on SSRF floor turned off. It is the
// ONLY way to disable the floor (the floor is on for the zero value and survives any config
// merge), used by a test or a deliberately-unfenced embedder. A config layer cannot reach
// this — it is a code-level opt-out, not a configuration key.
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
// ErrURLBlocked-wrapped error (naming the reason) for a rejected URL and a parse error
// for a malformed one. It runs the string-level scheme/host checks and the resolved-IP
// SSRF floor (resolving with context.Background()); CheckContext threads a caller's ctx
// for cancellable resolution. No network beyond DNS resolution is touched.
func (g URLGuard) Check(raw string) error {
	return g.CheckContext(context.Background(), raw)
}

// CheckContext is Check with a caller-supplied ctx for the SSRF floor's host resolution, so
// a slow/blocked DNS lookup is cancellable. It runs in order: parse, scheme allow, host
// deny/allow (string-level), then the resolved-IP SSRF floor (the default-on tighten-only
// safety net — loopback / IMDS / private / link-local denied by RESOLVED IP, ssrf.go). The
// floor is the pre-flight half of the defence; SafeDialControl re-checks the connected IP at
// dial time to close DNS-rebinding (ssrf.go).
func (g URLGuard) CheckContext(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%w: unparseable url: %v", ErrURLBlocked, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: url has no host", ErrURLBlocked)
	}

	scheme := strings.ToLower(u.Scheme)
	if !schemeAllowed(scheme, g.AllowSchemes) {
		return fmt.Errorf("%w: scheme %q is not permitted", ErrURLBlocked, scheme)
	}

	host := strings.ToLower(u.Hostname())
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
