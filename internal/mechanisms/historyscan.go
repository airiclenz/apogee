package mechanisms

import (
	"github.com/airiclenz/apogee/internal/domain"
)

// The shared history-scan shapes (architecture deepening item 7, D5): each history-inspecting
// Mechanism used to hand-roll the same conversation walks — readloop's read-attempt counting,
// readrepeat's recent-successful-reads, the write-path collection deriveWriteTarget filters
// against — differing subtly in role, window, and success handling. This file owns ONE copy of
// each shared shape, beside the F8 spelling families it composes with (readSpellings /
// listSpellings, decompose.go; wave4WriteTools as the write side's single source): the SCAN is
// shared, while per-Mechanism MEMBERSHIP (which tool-name set counts) and THRESHOLDS (how many
// hits matter) stay at the call site — passed as set parameters, applied to the returned counts.
// A composite walk no shared shape expresses without contortion stays local to its Mechanism with
// a comment naming the difference (isGreenfieldContext, readloop.go; fileHintDetectOpportunity,
// filehint.go).

// readAttemptCounts scans the whole conversation and counts the model's read attempts per path,
// successes and failures separately (the readloop shape: apogee-sim detectReadLoopPaths +
// detectSuccessfulReadLoopPaths @pin, folded into one pass). A read call in readSet with a
// non-empty path counts as a failure when its paired result reads as a read error, else as a
// success — a call without a committed result is not yet a failure (resultIsReadError's
// conservative contract). A write call in writeSet with a non-empty path decrements that path's
// success count (never below zero), so a read-then-write does not read as an unacted re-read.
// The two maps deliberately key differently, preserving each caller's behaviour: failures key by
// the LITERAL path spelling (the read-loop hint echoes the model's own spelling back at it),
// successes by the normalized path (so a "./a.go" read cancels against an "a.go" write). Callers
// apply their own thresholds to the returned counts (D5).
func readAttemptCounts(conv domain.ConversationView, readSet, writeSet map[string]bool) (successes, failures map[string]int) {
	successes = make(map[string]int)
	failures = make(map[string]int)
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			return true
		}
		for _, tc := range m.ToolCalls {
			p := toolCallPath(tc.Arguments)
			if p == "" {
				continue
			}
			np := normalizePath(p)
			switch {
			case readSet[tc.Tool] && resultIsReadError(conv, tc.ID):
				failures[p]++
			case readSet[tc.Tool]:
				successes[np]++
			case writeSet[tc.Tool] && successes[np] > 0:
				successes[np]--
			}
		}
		return true
	})
	return successes, failures
}

// recentSuccessfulReadPaths returns the set of normalized paths read successfully in the most
// recent Turn that issued a read, excluding any path written this Session (the readrepeat shape:
// apogee-sim recentSuccessfulReads @pin) — a file the model wrote to may legitimately be re-read.
// The recent window is structural, not numeric: scanning walks back and stops at the first
// assistant message where a successful unshadowed read lands, so only the latest read episode
// counts.
//
// Per assistant message the write paths are collected in a FIRST pass over its ToolCalls, then
// the reads are built in a SECOND pass excluding any written path — so a read SUPERSEDED by a
// same-turn write to the same path (order-independent within the turn) never lands in the result
// (C-02: a same-turn [read a.go, write a.go] must not make the next read of a.go a redundant
// re-read). The writes map accumulates across turns walking back, so a write in a LATER turn also
// shadows an earlier read of that path.
func recentSuccessfulReadPaths(conv domain.ConversationView, readSet, writeSet map[string]bool) map[string]bool {
	reads := make(map[string]bool)
	writes := make(map[string]bool)
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !writeSet[tc.Tool] {
				continue
			}
			if p := toolCallPath(tc.Arguments); p != "" {
				writes[normalizePath(p)] = true
			}
		}
		for _, tc := range m.ToolCalls {
			if !readSet[tc.Tool] {
				continue
			}
			p := toolCallPath(tc.Arguments)
			if p == "" {
				continue
			}
			np := normalizePath(p)
			if writes[np] {
				continue
			}
			if !resultIsReadError(conv, tc.ID) {
				reads[np] = true
			}
		}
		if len(reads) > 0 {
			break
		}
	}
	return reads
}

// writtenPathsSince collects the normalized paths the model has issued a write-tool call for in
// the messages from index start onward (the next_step shape: apogee-sim writtenPaths @pin,
// generalized by write set and starting index — start 0 scans the whole conversation). A negative
// start is clamped to 0. deriveWriteTarget (via writtenPaths, historyhints.go) filters its
// suggestion against this so it always points at remaining work.
func writtenPathsSince(conv domain.ConversationView, writeSet map[string]bool, start int) map[string]bool {
	out := make(map[string]bool)
	if start < 0 {
		start = 0
	}
	for i := start; i < conv.Len(); i++ {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !writeSet[tc.Tool] {
				continue
			}
			if p := toolCallPath(tc.Arguments); p != "" {
				out[normalizePath(p)] = true
			}
		}
	}
	return out
}
