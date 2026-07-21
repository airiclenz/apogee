package context

import "strings"

// ----------------------------------------------------------------------------
// Tool-result truncation — the rendering both result-capping reducers share
// ----------------------------------------------------------------------------
//
// Two reducers may shrink a single oversized tool result, at different moments and against
// different ceilings: the loop's STRUCTURAL floor clamps a pathologically large result as it
// enters the conversation (internal/agent appendToolResult), and the config-gated
// `tool_result_cap` Mechanism trims older results in the projected request
// (internal/mechanisms). They must render the elision IDENTICALLY — one head/tail shape and
// one marker — so the model learns a single "the middle was dropped, re-read the range" idiom
// no matter which reducer produced it. The rendering therefore lives here, in the package that
// owns the working context, rather than being duplicated in either caller.

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
// apogee's read_file tool takes start_line/end_line (`internal/tools/read_file.go:18-19`), so the
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
