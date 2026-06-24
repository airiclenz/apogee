package security

import (
	"errors"
	"testing"
)

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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.guard.Check(tc.url)
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
