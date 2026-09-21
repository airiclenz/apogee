package mcp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestLineBoundedReader pins the bound's contract through Read alone: a line longer than max
// with no newline fails, lines each under max pass whatever their total, and the failure is
// sticky — every Read after it returns the same error and reads nothing more.
func TestLineBoundedReader(t *testing.T) {
	t.Parallel()
	const limit = 4 << 20
	cases := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "a line one byte past the limit without a newline fails", input: strings.Repeat("x", limit+1), wantErr: errMCPMessageTooLarge},
		{name: "two 3 MiB lines pass", input: strings.Repeat("a", 3<<20) + "\n" + strings.Repeat("b", 3<<20) + "\n"},
		{name: "a line exactly at the limit passes", input: strings.Repeat("x", limit) + "\n"},
		{name: "a short line after an overlong one never arrives", input: strings.Repeat("x", limit+1) + "\nshort\n", wantErr: errMCPMessageTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &lineBoundedReader{r: strings.NewReader(tc.input), max: limit}

			got, err := io.ReadAll(r)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadAll error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && string(got) != tc.input {
				t.Fatalf("ReadAll returned %d bytes, want the whole %d-byte input", len(got), len(tc.input))
			}
			if tc.wantErr != nil && len(got) > limit {
				t.Fatalf("ReadAll handed over %d bytes of an overlong line, want at most %d", len(got), limit)
			}
		})
	}
}

// TestLineBoundedReader_ErrorIsSticky proves a tripped reader stays tripped: the Read after the
// failure returns the same error without touching the underlying reader, so the decoder above it
// cannot resume past the cap.
func TestLineBoundedReader_ErrorIsSticky(t *testing.T) {
	t.Parallel()
	underlying := &countingReader{Reader: bytes.NewReader([]byte(strings.Repeat("x", 16) + "\nmore\n"))}
	r := &lineBoundedReader{r: underlying, max: 8}
	buf := make([]byte, 4)

	_, first := r.Read(buf)
	for !errors.Is(first, errMCPMessageTooLarge) {
		if first != nil {
			t.Fatalf("Read failed with %v before the bound tripped", first)
		}
		_, first = r.Read(buf)
	}
	readsAtTrip := underlying.reads

	_, second := r.Read(buf)

	if !errors.Is(second, errMCPMessageTooLarge) {
		t.Fatalf("Read after the trip = %v, want the sticky %v", second, errMCPMessageTooLarge)
	}
	if underlying.reads != readsAtTrip {
		t.Fatalf("Read after the trip reached the underlying reader (%d reads, want %d)", underlying.reads, readsAtTrip)
	}
}

// countingReader counts the Reads that reach it.
type countingReader struct {
	io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.Reader.Read(p)
}
