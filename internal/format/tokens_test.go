package format

import "testing"

// TestTokens pins the coarse spellings the whole product reads by. The power-of-two rows are the
// point of the binary ladder (131072 must be "128k"); the decimal rows are its accepted price
// (128000 is "125k"); the rows around 1023488 are the k→M handover, where the rule bites.
func TestTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		n    int
		want string
	}{
		{"zero is unknown", 0, ""},
		{"negative is unknown", -1, ""},
		{"bare below a thousand", 999, "999"},
		{"a thousand rounds up into k", 1000, "1k"},
		{"just above a thousand stays 1k", 1010, "1k"},
		{"half-up, not truncation", 1999, "2k"},
		{"power of two 18k", 18432, "18k"},
		{"power of two 32k", 32768, "32k"},
		{"power of two 64k", 65536, "64k"},
		{"the 128k window", 131072, "128k"},
		{"decimal 128000 reads slightly small", 128000, "125k"},
		{"decimal million reads slightly small", 1000000, "977k"},
		{"last value that fits in k", 1023487, "999k"},
		{"first value that must leave k", 1023488, "1M"},
		{"a mebi-token is 1M, never 1024k", 1048576, "1M"},
		{"M keeps a tenth that says something", 1153434, "1.1M"},
		{"M drops the tenth at ten", 10485760, "10M"},
		{"whole G drops its decimal point", 2147483648, "2G"},
		{"nothing above G, so it spells out wide", 1 << 50, "1048576G"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Tokens(tc.n); got != tc.want {
				t.Errorf("Tokens(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// TestTokensFine pins the comparison form: the same ladder and sentinel, but a tenth in every
// unit — including the zero tenth of "32.0k", which is what keeps two compared counts spelled at
// the same width.
func TestTokensFine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		n    int
		want string
	}{
		{"zero is unknown", 0, ""},
		{"negative is unknown", -1, ""},
		{"bare below a thousand", 999, "999"},
		{"a tenth of k", 4242, "4.1k"},
		{"keeps the zero tenth", 32768, "32.0k"},
		{"keeps the zero tenth in M too", 1048576, "1.0M"},
		{"a tenth of M", 1258291, "1.2M"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TokensFine(tc.n); got != tc.want {
				t.Errorf("TokensFine(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}
