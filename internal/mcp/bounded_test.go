package mcp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// TestLineBoundedReader pins the bound's contract through Read alone, per framing: a message
// longer than max fails however it is split into lines, messages each under max pass whatever
// their total, and the failure is sticky — every Read after it returns the same error and reads
// nothing more.
func TestLineBoundedReader(t *testing.T) {
	t.Parallel()
	const limit = 4 << 20
	const small = 64
	// manyShortLines is n lines of "x" — each far under any cap, together as long as the caller
	// asks.
	manyShortLines := func(n int, lineEnd string) string {
		return strings.Repeat("x"+lineEnd, n)
	}
	cases := []struct {
		name    string
		framing messageFraming
		max     int
		input   string
		wantErr error
	}{
		{name: "stdio: a line one byte past the limit without a newline fails", framing: frameJSONLines, max: limit, input: strings.Repeat("x", limit+1), wantErr: errMCPMessageTooLarge},
		{name: "stdio: two 3 MiB messages pass", framing: frameJSONLines, max: limit, input: strings.Repeat("a", 3<<20) + "\n" + strings.Repeat("b", 3<<20) + "\n"},
		{name: "stdio: a line exactly at the limit passes", framing: frameJSONLines, max: limit, input: strings.Repeat("x", limit) + "\n"},
		{name: "stdio: a short line after an overlong one never arrives", framing: frameJSONLines, max: limit, input: strings.Repeat("x", limit+1) + "\nshort\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: many short lines of one object over the cap fail", framing: frameJSONLines, max: small, input: "{\n" + manyShortLines(small, "\n") + "}\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: many short lines of one array over the cap fail", framing: frameJSONLines, max: small, input: "[\n" + strings.Repeat("1,\n", small) + "1]\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: a string left open across lines over the cap fails", framing: frameJSONLines, max: small, input: `"` + manyShortLines(small, "\n"), wantErr: errMCPMessageTooLarge},
		{name: "stdio: a newline inside a string does not end the message", framing: frameJSONLines, max: small, input: `{"a":"}` + manyShortLines(small, "\n") + `"}` + "\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: an escaped quote does not close the string", framing: frameJSONLines, max: small, input: `{"a":"\"}` + manyShortLines(small, "\n") + `"}` + "\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: many small messages totalling more than the cap pass", framing: frameJSONLines, max: small, input: strings.Repeat(`{"id":1,"a":[1,"]"]}`+"\n", small)},
		{name: "stdio: many small messages with \\r\\n line ends pass", framing: frameJSONLines, max: small, input: strings.Repeat(`{"id":1}`+"\r\n", small)},
		{name: "sse: many short lines inside one event over the cap fail", framing: frameSSEEvents, max: small, input: "event: message\n" + strings.Repeat("data: x\n", small) + "\n", wantErr: errMCPMessageTooLarge},
		{name: "sse: many short \\r\\n lines inside one event over the cap fail", framing: frameSSEEvents, max: small, input: strings.Repeat("data: x\r\n", small) + "\r\n", wantErr: errMCPMessageTooLarge},
		{name: "sse: many small events totalling more than the cap pass", framing: frameSSEEvents, max: small, input: strings.Repeat("event: message\ndata: {}\n\n", small)},
		{name: "sse: many small \\r\\n events totalling more than the cap pass", framing: frameSSEEvents, max: small, input: strings.Repeat("event: message\r\ndata: {}\r\n\r\n", small)},
		{name: "sse: two 3 MiB events pass", framing: frameSSEEvents, max: limit, input: "data: " + strings.Repeat("a", 3<<20) + "\n\ndata: " + strings.Repeat("b", 3<<20) + "\n\n"},
		{name: "whole body: many short lines over the cap fail", framing: frameWholeBody, max: small, input: manyShortLines(small, "\n"), wantErr: errMCPMessageTooLarge},
		{name: "whole body: a body exactly at the cap passes", framing: frameWholeBody, max: small, input: strings.Repeat("x", small)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &lineBoundedReader{r: strings.NewReader(tc.input), max: tc.max, framing: tc.framing}

			got, err := io.ReadAll(r)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadAll error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && string(got) != tc.input {
				t.Fatalf("ReadAll returned %d bytes, want the whole %d-byte input", len(got), len(tc.input))
			}
			if tc.wantErr != nil && len(got) > tc.max {
				t.Fatalf("ReadAll handed over %d bytes of an overlong message, want at most %d", len(got), tc.max)
			}
		})
	}
}

// TestLineBoundedReader_ChunkingDoesNotMatter feeds a multi-line message one byte per Read, so
// every piece of scan state — nesting, an open string, an escape, a '\r' before a blank line's
// '\n' — has to carry across calls.
func TestLineBoundedReader_ChunkingDoesNotMatter(t *testing.T) {
	t.Parallel()
	const small = 32
	cases := []struct {
		name    string
		framing messageFraming
		input   string
		wantErr error
	}{
		{name: "stdio: one value over the cap fails", framing: frameJSONLines, input: `{"a":"\"` + strings.Repeat("x\n", small) + `"}` + "\n", wantErr: errMCPMessageTooLarge},
		{name: "stdio: small values pass", framing: frameJSONLines, input: strings.Repeat(`{"a":"\"x"}`+"\n", small)},
		{name: "sse: one event over the cap fails", framing: frameSSEEvents, input: strings.Repeat("data: x\r\n", small) + "\r\n", wantErr: errMCPMessageTooLarge},
		{name: "sse: small events pass", framing: frameSSEEvents, input: strings.Repeat("data: x\r\n\r\n", small)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &lineBoundedReader{r: iotest.OneByteReader(strings.NewReader(tc.input)), max: small, framing: tc.framing}

			_, err := io.ReadAll(r)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadAll error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestFramingForContentType pins the HTTP body's framing to the Content-Type's base media type,
// as the SDK reads it: parameters do not change an SSE reply's mode, and anything else is one
// whole-body message.
func TestFramingForContentType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		contentType string
		want        messageFraming
	}{
		{contentType: "text/event-stream", want: frameSSEEvents},
		{contentType: "text/event-stream; charset=utf-8", want: frameSSEEvents},
		{contentType: "Text/Event-Stream", want: frameSSEEvents},
		{contentType: "application/json", want: frameWholeBody},
		{contentType: "text/plain; charset=utf-8", want: frameWholeBody},
		{contentType: "", want: frameWholeBody},
		{contentType: "text/event-stream; =", want: frameWholeBody},
	}
	for _, tc := range cases {
		if got := framingForContentType(tc.contentType); got != tc.want {
			t.Errorf("framingForContentType(%q) = %d, want %d", tc.contentType, got, tc.want)
		}
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
