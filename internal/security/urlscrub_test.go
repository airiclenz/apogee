package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
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

// refusedAddr returns a loopback host:port nothing listens on, so a request to it is refused.
func refusedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// realURLErrorText is the text net/http itself produces for a failed POST to rawURL — the exact
// shape an HTTP transport surfaces, userinfo spelling included.
func realURLErrorText(t *testing.T, rawURL string) string {
	t.Helper()
	resp, err := http.Post(rawURL, "application/json", strings.NewReader("{}"))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("POST %s succeeded; want a refused connection", rawURL)
	}
	return err.Error()
}

func TestOriginRedactor_CutsTheEndpointToItsOrigin(t *testing.T) {
	t.Parallel()

	addr := refusedAddr(t)
	origin := "http://" + addr
	redactor := NewOriginRedactor(origin + "/mcp?token=SECRET")
	if redactor == nil {
		t.Fatal("NewOriginRedactor returned nil for an HTTP endpoint")
	}

	tests := []struct {
		name string
		text string
	}{
		{"token in query", `Post "` + origin + `/mcp?token=SECRET": dial tcp: connection refused`},
		{"token in path", `Get "` + origin + `/SECRET/mcp": EOF`},
		{"unquoted url", `sending to ` + origin + `/mcp?token=SECRET failed`},
		{"same-origin session url", `Post "` + origin + `/messages?sessionid=SECRET": EOF`},
		{"username-only userinfo", realURLErrorText(t, "http://SECRET@"+addr+"/mcp?token=SECRET")},
		{"user and password userinfo", realURLErrorText(t, "http://SECRET:SECRET@"+addr+"/mcp?token=SECRET")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactor.Redact(tt.text)
			if strings.Contains(got, "SECRET") || strings.Contains(got, "/mcp") || strings.Contains(got, "@") {
				t.Errorf("Redact(%q) = %q; want no userinfo, path or query", tt.text, got)
			}
			if !strings.Contains(got, origin) {
				t.Errorf("Redact(%q) = %q; want the bare origin %q kept", tt.text, got, origin)
			}
		})
	}

	t.Run("unrelated text untouched", func(t *testing.T) {
		const text = `Post "http://other.example/mcp?token=keep": EOF; tool missing`
		if got := redactor.Redact(text); got != text {
			t.Errorf("Redact(%q) = %q; want it unchanged", text, got)
		}
	})

	t.Run("an endpoint with no host is the identity", func(t *testing.T) {
		if r := NewOriginRedactor("not a url"); r != nil {
			t.Errorf("NewOriginRedactor(no host) = %v; want nil", r)
		}
	})

	t.Run("RedactErr keeps the chain", func(t *testing.T) {
		err := redactor.RedactErr(fmt.Errorf("wrapped %s/mcp?token=SECRET: %w", origin, context.Canceled))
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("RedactErr text = %q; want the token cut", err.Error())
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("errors.Is(RedactErr(...), context.Canceled) = false; want the chain intact")
		}
	})
}
