package mcp

import (
	"bytes"
	"errors"
	"io"
)

// ----------------------------------------------------------------------------
// The line-bounded reader — one message may not exceed 4 MiB
// ----------------------------------------------------------------------------
//
// An MCP server speaks newline-delimited JSON, and the SDK's decoder buffers one whole line
// before it parses it: a server that never sends the newline grows that buffer without bound.
// The reader below sits under the decoder and fails the stream once a single line has run past
// the cap, so the decoder errors, the SDK retires every in-flight call with that error and the
// connection is dead — the read half of the 2026-09-20 audit's "an MCP server's response is
// read with no size cap". It knows nothing of MCP or of transports: the stdio transport wraps
// the server's stdout in it, and an HTTP body can be wrapped the same way.

// maxMCPMessageBytes is the longest newline-delimited message a server may send: 4 MiB. A
// tool result the model is to read is far smaller than that; a line this long is a hostile or
// broken server, not a large answer.
const maxMCPMessageBytes = 4 << 20

// errMCPMessageTooLarge is the read error a line past maxMCPMessageBytes fails the stream with.
// It surfaces to the model through the SDK's retired call, so its text names the cause plainly.
var errMCPMessageTooLarge = errors.New("apogee: mcp message exceeds the 4 MiB limit")

// lineBoundedReader is an io.Reader that counts the bytes read since the last '\n' and fails
// with errMCPMessageTooLarge once that count passes max. The failure is sticky: every Read after
// it returns the same error without touching the underlying reader, and the chunk that tripped
// the bound is dropped whole, so no byte of an oversize line beyond the cap reaches the caller.
type lineBoundedReader struct {
	r    io.Reader
	max  int
	line int   // bytes of the open line seen so far — reset at each '\n'
	err  error // the sticky failure, once tripped
}

// Read reads from the underlying reader and measures every line the chunk touches before handing
// it over; a chunk that grows any line past max is withheld and the bound's error returned.
func (l *lineBoundedReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.r.Read(p)
	if l.exceeds(p[:n]) {
		l.err = errMCPMessageTooLarge
		return 0, l.err
	}
	return n, err
}

// exceeds walks chunk line by line, carrying the open line's count across calls, and reports
// whether any line has grown past max. The count is left at the open line's length on return.
func (l *lineBoundedReader) exceeds(chunk []byte) bool {
	for len(chunk) > 0 {
		newline := bytes.IndexByte(chunk, '\n')
		if newline < 0 {
			l.line += len(chunk)
			return l.line > l.max
		}
		if l.line+newline > l.max {
			return true
		}
		l.line = 0
		chunk = chunk[newline+1:]
	}
	return false
}
