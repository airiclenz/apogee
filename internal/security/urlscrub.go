package security

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// URL scrubbers — keeping a request URL out of a model-facing string.
//
// A request URL may carry a query and, in it, a config'd API key (security-review M2). The
// network tools therefore never surface the URL itself in a failure message: they name only its
// bare host (SafeHost) and render any transport error with the URL stripped out (ScrubURLError,
// built on RedactSubstring). An MCP server's configured endpoint is cut to its bare origin
// instead (OriginRedactor), since a transport it hands the URL to may surface any path on that
// origin. These live beside the guarded client because they guard the same boundary from the
// other side: the client decides what a request may dial, these decide what its failure may say.
// The two algorithms stay distinct: the exact-URL substring strip and the origin cut.

// SafeHost returns the bare host (no scheme, no path, no query) of rawURL — the only part of
// a request URL safe to surface to the model, since the URL may carry the query and a
// config'd API key (security-review M2). An unparseable URL yields a neutral placeholder
// rather than echoing the raw (possibly key-bearing) string.
func SafeHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "the requested host"
	}
	return u.Hostname()
}

// ScrubURLError renders a transport error WITHOUT the request URL it embeds. Go's
// *url.Error stringifies as `<op> "<url>": <cause>`, and that url may carry a query and an
// API-key parameter; ScrubURLError strips the URL substring so only the operation and the
// underlying cause survive (security-review M2). rawURL is the exact string to remove. A
// non-url.Error is returned unchanged (it carries no URL).
func ScrubURLError(err error, rawURL string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		// Reconstruct from the parts that do NOT include the URL: the op and the cause.
		cause := "request failed"
		if ue.Err != nil {
			cause = ue.Err.Error()
		}
		return strings.TrimSpace(ue.Op) + ": " + redactRequestURL(cause, rawURL)
	}
	return redactRequestURL(err.Error(), rawURL)
}

// redactRequestURL removes the request URL from s in BOTH the form the caller supplied and its
// whitespace-trimmed form. The trimmed form matters because url-safety normalises the TRIMMED
// URL (urlsafety.go), so a nested error keyed on that form — from a model passing
// " http://exa mple.com/?key=SECRET" (note the leading space) — would otherwise be matched
// against a string that never appears, leaking the key (M2).
func redactRequestURL(s, rawURL string) string {
	s = RedactSubstring(s, rawURL)
	return RedactSubstring(s, strings.TrimSpace(rawURL))
}

// RedactSubstring removes any occurrence of secret from s (defence-in-depth in case the
// URL leaks into a nested error's text), returning the cleaned string.
//
// It strips the %q-QUOTED form of secret as well as the raw one, because a plain substring
// search is exactly what an escaping formatter defeats (M-2): fmt's %q — which Go's own
// *url.Error uses to embed the URL in its text — escapes control characters, so a URL carrying
// an interior control byte appears as `…?key=SECRET\x01x` with a LITERAL backslash-x that the
// raw byte sequence never matches. strconv.Quote applies the identical escaping, so its inner
// form (the quoted string without its surrounding quotes) is the string to search for. When
// nothing needed escaping the two forms are the same and the second pass is skipped.
func RedactSubstring(s, secret string) string {
	if secret == "" {
		return s
	}
	s = strings.ReplaceAll(s, secret, "[redacted-url]")
	quoted := strconv.Quote(secret)
	if inner := quoted[1 : len(quoted)-1]; inner != secret {
		s = strings.ReplaceAll(s, inner, "[redacted-url]")
	}
	return s
}

// OriginRedactor cuts a configured endpoint down to its bare origin wherever it appears in error
// text. An HTTP transport reports failures as *url.Error text such as
// `Post "https://host/mcp?token=SECRET": ...`, which carries the configured URL — userinfo, path
// and query, any of which may hold a credential — past the adapter's parse-time scrub. Every
// occurrence of the endpoint's origin, with any userinfo net/http prints before the host and any
// run of non-space, non-quote characters after it, is replaced by `scheme://host[:port]` alone.
// The run covers any path on the same origin, so an SSE session URL the server announced is cut
// as well as the configured one.
//
// A nil *OriginRedactor is the identity: an endpoint-less server has nothing to hide.
type OriginRedactor struct {
	pattern *regexp.Regexp // scheme://[userinfo@]host[:port] followed by a non-space, non-quote run
	origin  string         // scheme://host[:port], the replacement
}

// NewOriginRedactor builds the redactor for endpoint, normalised by NormalizeURL exactly as an
// adapter's pre-flight normalises it so the origin matches the URL the transport was handed. It
// returns nil — the identity — for an endpoint that does not parse or has no scheme or host, which
// never reached a transport.
func NewOriginRedactor(endpoint string) *OriginRedactor {
	u, err := NormalizeURL(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(u.Scheme) + `://(?:[^@/\s"]*@)?` +
		regexp.QuoteMeta(u.Host) + `[^\s"]*`)
	return &OriginRedactor{pattern: pattern, origin: origin}
}

// Redact returns text with every occurrence of the endpoint cut to its bare origin.
func (r *OriginRedactor) Redact(text string) string {
	if r == nil {
		return text
	}
	return r.pattern.ReplaceAllLiteralString(text, r.origin)
}

// RedactErr returns err with its text redacted, keeping the chain intact so a caller's errors.Is
// (context cancellation, the url-safety sentinels) still sees what the transport wrapped.
func (r *OriginRedactor) RedactErr(err error) error {
	if r == nil || err == nil {
		return err
	}
	msg := r.Redact(err.Error())
	if msg == err.Error() {
		return err
	}
	return &redactedError{msg: msg, err: err}
}

// redactedError is an error whose text has been redacted but whose chain is the original's.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
