package context

import (
	"strings"
	"unicode/utf8"
)

// ----------------------------------------------------------------------------
// Tool-result truncation — the rendering both result-capping reducers share
// ----------------------------------------------------------------------------
//
// Three reducers may shrink a single oversized tool result, at different moments and against
// different ceilings: the loop's STRUCTURAL floor clamps a pathologically large result as it
// enters the conversation (internal/agent appendToolResult), the config-gated
// tool-result-cap Floor guard trims older results in the projected request
// (internal/floor), and the absolute cap on a sub_agent result cuts a delegation's report
// to a fixed byte size before its notes are appended (internal/agent delegationResult,
// ElideMiddle). They must render the elision IDENTICALLY — a head/tail shape and
// one marker — so the model learns a single "the middle was dropped, re-read the range" idiom
// no matter which reducer produced it. The rendering therefore lives here, in the package that
// owns the working context, rather than being duplicated in any caller.

// toolResultHeadLines and toolResultTailLines are how many leading and trailing lines a
// truncated result keeps — apogee-sim's headLines/tailLines (`compress.go:492-495` @pin,
// 20/20). The head shows the start of a file/output and the tail its end; the middle is elided
// with a marker pointing the model at a targeted re-read.
const (
	toolResultHeadLines = 20
	toolResultTailLines = 20
)

// toolResultElisionMarker replaces the elided middle of a truncated result. apogee-sim's marker
// also carried a codeinfo structural summary (`compress.go:521-526` @pin); codeinfo is DROPPED in
// apogee (catalogue C7), so the marker is the plain elision note plus the same re-read hint —
// apogee's read_file tool takes start_line/end_line (`internal/tools/read_file.go:21-22`), so the
// hint is actionable.
const toolResultElisionMarker = "\n[truncated to fit the context budget — re-read with start_line/end_line for the omitted range]\n\n"

// TruncateToolResult renders content as its first toolResultHeadLines lines, the elision marker,
// and its last toolResultTailLines lines — apogee-sim truncateToolResult (`compress.go:499` @pin)
// minus the dropped codeinfo summary (C7). maxChars is the ceiling the caller is trimming to; the
// shape is line-based, so maxChars only sizes the builder — the caller decides WHETHER to trim (it
// calls this only for content already known to exceed its ceiling) and, because a pathological
// few-very-long-lines result can render longer than it started, whether the rendering actually
// shrank it.
func TruncateToolResult(content string, maxChars int) string {
	lines := strings.Split(content, "\n")

	headN := toolResultHeadLines
	if headN > len(lines) {
		headN = len(lines)
	}
	tailN := toolResultTailLines
	if tailN > len(lines)-headN {
		tailN = len(lines) - headN
	}

	var b strings.Builder
	b.Grow(maxChars + len(toolResultElisionMarker) + 64)
	for i := 0; i < headN; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString(toolResultElisionMarker)
	if tailN > 0 {
		start := len(lines) - tailN
		for i := start; i < len(lines); i++ {
			b.WriteString(lines[i])
			if i < len(lines)-1 {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// ElideMiddle renders content within maxBytes as its first headBytes bytes, the elision marker,
// and as much of its tail as the remaining budget holds — the BYTE-sized form of
// TruncateToolResult, for a caller whose ceiling is an absolute size rather than a line count
// (the sub_agent result cap in internal/agent delegationResult). It renders the same marker so
// the model reads one "the middle was dropped" idiom whichever seam produced it, and the marker
// is paid for out of maxBytes, so the rendering never exceeds it. Each cut backs off to the
// nearest line break inside its budget so neither end tears a line in half — the head ends on a
// full line and the tail starts on one — and a budget with no line break in it is cut at the last
// whole UTF-8 sequence. The caller decides WHETHER to elide: it calls this only for content it
// knows exceeds maxBytes, and headBytes plus the marker must fit inside maxBytes.
func ElideMiddle(content string, maxBytes, headBytes int) string {
	head := content[:min(headBytes, len(content))]
	if i := strings.LastIndexByte(head, '\n'); i >= 0 {
		head = head[:i+1]
	} else {
		head = trimPartialRune(head)
	}
	tailBytes := max(maxBytes-len(head)-len(toolResultElisionMarker), 0)
	tail := content[len(content)-min(tailBytes, len(content)):]
	if i := strings.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	} else {
		tail = skipPartialRune(tail)
	}

	var b strings.Builder
	b.Grow(len(head) + len(toolResultElisionMarker) + len(tail))
	b.WriteString(head)
	b.WriteString(toolResultElisionMarker)
	b.WriteString(tail)
	return b.String()
}

// trimPartialRune drops an incomplete trailing UTF-8 sequence from s, so a byte-offset cut never
// ends on the first bytes of a multi-byte rune.
func trimPartialRune(s string) string {
	for n := 0; n < utf8.UTFMax-1 && len(s) > 0; n++ {
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// skipPartialRune drops an incomplete leading UTF-8 sequence from s — the mirror of
// trimPartialRune for a cut made from the end.
func skipPartialRune(s string) string {
	for n := 0; n < utf8.UTFMax-1 && len(s) > 0 && !utf8.RuneStart(s[0]); n++ {
		s = s[1:]
	}
	return s
}
