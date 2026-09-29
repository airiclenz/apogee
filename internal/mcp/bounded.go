package mcp

import (
	"bytes"
	"errors"
	"io"
	"mime"
)

// ----------------------------------------------------------------------------
// The message-bounded reader — one message may not exceed 4 MiB
// ----------------------------------------------------------------------------
//
// The SDK buffers one whole message before it parses it: the stdio decoder a whole JSON value,
// the SSE scanner a whole event, the streamable JSON reply a whole body. A server that never
// finishes a message grows that buffer without bound. The reader below sits under the SDK and
// fails the stream once a single message has run past the cap, so the SDK's read errors, the
// in-flight call is retired with that error and (on stdio) the connection is dead — the read half
// of the 2026-09-20 audit's "an MCP server's response is read with no size cap".
//
// The count is cumulative over the whole message, never per line (2026-09-29 audit: "MCP size cap
// is per line"): a value or event split into many short lines is charged every byte of every line
// until its boundary. What the boundary is depends on the wire, so the reader carries a framing:
// stdio resets at each completed message line, SSE at each blank line, and a plain HTTP body never
// resets. It knows nothing else of MCP or of transports: the stdio transport wraps the server's
// stdout in it, and the HTTP transports wrap every response body in it (boundedBodyTransport,
// transport.go).

// maxMCPMessageBytes is the longest message a server may send: 4 MiB. A tool result the model is
// to read is far smaller than that; a message this long is a hostile or broken server, not a large
// answer.
const maxMCPMessageBytes = 4 << 20

// errMCPMessageTooLarge is the read error a message past maxMCPMessageBytes fails the stream
// with. It surfaces to the model through the SDK's retired call, so its text names the cause
// plainly.
var errMCPMessageTooLarge = errors.New("apogee: mcp message exceeds the 4 MiB limit")

// messageFraming names where one message ends on the wire the reader bounds — the point at which
// the cumulative count resets.
type messageFraming int

const (
	// frameJSONLines is stdio's framing: a message completes at a newline at JSON depth 0 outside
	// a string. The SDK's json.Decoder accepts newlines inside a value, so a newline alone is not a
	// boundary — a value split across many short lines is still one message.
	frameJSONLines messageFraming = iota
	// frameSSEEvents is an SSE body's framing: an event completes at a blank line, which may be
	// "\n" or "\r\n" (the SDK's scanEvents trims both before testing for empty).
	frameSSEEvents
	// frameWholeBody is a plain HTTP body's framing: the body is one message and never resets.
	frameWholeBody
)

// framingForContentType picks an HTTP response body's framing from its Content-Type, compared by
// base media type as the SDK's baseMediaType does: text/event-stream (with any parameters) is an
// SSE stream, and everything else — application/json, an error page, an unparseable header — is
// one whole-body message.
func framingForContentType(contentType string) messageFraming {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType == "text/event-stream" {
		return frameSSEEvents
	}
	return frameWholeBody
}

// lineBoundedReader is an io.Reader that counts the bytes read since the last message boundary
// of its framing and fails with errMCPMessageTooLarge once that count passes max. The failure is
// sticky: every Read after it returns the same error without touching the underlying reader, and
// the chunk that tripped the bound is dropped whole, so no byte of an oversize message beyond the
// cap reaches the caller.
type lineBoundedReader struct {
	r       io.Reader
	max     int
	framing messageFraming
	msg     int   // bytes of the open message seen so far — reset at each boundary
	err     error // the sticky failure, once tripped

	// frameJSONLines scan state, carried across Reads.
	depth    int  // open objects and arrays
	inString bool // inside a JSON string
	escaped  bool // the previous string byte was a backslash

	// frameSSEEvents scan state: the open line holds a byte other than '\r', so it is not blank.
	lineHasContent bool
}

// Read reads from the underlying reader and measures the chunk against the open message before
// handing it over; a chunk that grows the message past max is withheld and the bound's error
// returned.
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

// exceeds charges chunk to the open message under the reader's framing and reports whether the
// message has grown past max. The count and scan state are left at the open message on return.
func (l *lineBoundedReader) exceeds(chunk []byte) bool {
	switch l.framing {
	case frameSSEEvents:
		return l.exceedsSSE(chunk)
	case frameWholeBody:
		l.msg += len(chunk)
		return l.msg > l.max
	default:
		return l.exceedsJSONLines(chunk)
	}
}

// exceedsJSONLines tracks string and nesting state byte by byte; a newline at depth 0 outside a
// string completes the message and is not charged to it.
func (l *lineBoundedReader) exceedsJSONLines(chunk []byte) bool {
	for _, b := range chunk {
		if b == '\n' && l.depth == 0 && !l.inString {
			l.msg = 0
			continue
		}
		l.msg++
		if l.msg > l.max {
			return true
		}
		switch {
		case l.escaped:
			l.escaped = false
		case l.inString:
			if b == '\\' {
				l.escaped = true
			} else if b == '"' {
				l.inString = false
			}
		case b == '"':
			l.inString = true
		case b == '{' || b == '[':
			l.depth++
		case (b == '}' || b == ']') && l.depth > 0:
			l.depth--
		}
	}
	return false
}

// exceedsSSE walks chunk line by line; every line of an event, newline included, is charged to
// it, and a blank line — nothing but '\r' before its '\n' — completes the event.
func (l *lineBoundedReader) exceedsSSE(chunk []byte) bool {
	for len(chunk) > 0 {
		newline := bytes.IndexByte(chunk, '\n')
		segment := chunk
		if newline >= 0 {
			segment = chunk[:newline]
		}
		if len(bytes.TrimLeft(segment, "\r")) > 0 {
			l.lineHasContent = true
		}
		l.msg += len(segment)
		if l.msg > l.max {
			return true
		}
		if newline < 0 {
			return false
		}
		if l.lineHasContent {
			l.msg++
			if l.msg > l.max {
				return true
			}
		} else {
			l.msg = 0
		}
		l.lineHasContent = false
		chunk = chunk[newline+1:]
	}
	return false
}
