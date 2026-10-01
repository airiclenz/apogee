package security

import (
	"fmt"
	"testing"
)

// urlScrubSecretKey stands in for a config'd API key carried in a request URL's query.
const urlScrubSecretKey = "SUPER-SECRET-API-KEY-1234"

// TestRedactSubstring_StripsTheQuotedFormToo pins the defence-in-depth half of M-2 at its own
// seam: a plain substring search is defeated by any formatter that ESCAPES what it prints, and
// Go's *url.Error embeds the request URL under %q. RedactSubstring must therefore strip the
// quoted spelling as well as the raw one — the surrounding quotes are the formatter's, not the
// secret's, so they stay.
func TestRedactSubstring_StripsTheQuotedFormToo(t *testing.T) {
	t.Parallel()

	secret := "http://example.com/?key=" + urlScrubSecretKey + "\x01x"

	cases := []struct {
		name   string
		in     string
		secret string
		want   string
	}{
		{
			name:   "the raw form is stripped",
			in:     "cause: " + secret,
			secret: secret,
			want:   "cause: [redacted-url]",
		},
		{
			name:   "the %q-escaped form is stripped",
			in:     fmt.Sprintf("parse %q: net/url: invalid control character in URL", secret),
			secret: secret,
			want:   `parse "[redacted-url]": net/url: invalid control character in URL`,
		},
		{
			name:   "a secret needing no escaping is unaffected by the second pass",
			in:     "Get \"http://example.com/?key=" + urlScrubSecretKey + "\": dial failed",
			secret: "http://example.com/?key=" + urlScrubSecretKey,
			want:   `Get "[redacted-url]": dial failed`,
		},
		{
			name:   "an empty secret redacts nothing",
			in:     "no url here",
			secret: "",
			want:   "no url here",
		},
		{
			name:   "an absent secret leaves the text alone",
			in:     "no url here",
			secret: secret,
			want:   "no url here",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactSubstring(tc.in, tc.secret); got != tc.want {
				t.Errorf("RedactSubstring() = %q, want %q", got, tc.want)
			}
		})
	}
}
