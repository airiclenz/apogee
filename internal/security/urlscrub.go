package security

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// URL scrubbers — keeping a request URL out of a model-facing string.
//
// A request URL may carry a query and, in it, a config'd API key (security-review M2). The
// network tools therefore never surface the URL itself in a failure message: they name only its
// bare host (SafeHost) and render any transport error with the URL stripped out (ScrubURLError,
// built on RedactSubstring). These live beside the guarded client because they guard the same
// boundary from the other side: the client decides what a request may dial, these decide what
// its failure may say.

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
