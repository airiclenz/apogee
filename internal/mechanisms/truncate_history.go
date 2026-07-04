package mechanisms

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
)

// truncate_history registers the drop-the-middle history-rewrite Mechanism in the catalogue
// constructor table (Phase-4 item 7). Default-off (D1) — the config surface builds it only when
// the `mechanisms:` block enables it. It is the cheap A/B alternative to generative Compaction,
// validated bench-side later; correct_tool_result, the other lab-only intervention, is DEFERRED
// (owner-ratified 2026-07-04 — no production trigger to port; the bench plays the sim's operator
// via an experimental post-tool-result hook — see docs/design/mechanism-catalogue.md Table B).
func init() { catalogue[truncateHistoryID] = newTruncateHistory }

const truncateHistoryID domain.MechanismID = "truncate_history"

// defaultKeepLastTurns is how many assistant-anchored exchanges the tail keeps — an assistant
// message plus everything after it. The sim supplied this per operator command (KeepLastTurns);
// a catalogued Mechanism has no per-Mechanism config surface (item 4's `mechanisms:` block is
// enabled-only), so it is a built-in default. Four keeps the immediate working set — the model's
// recent tool calls and their results — while shedding the older middle; it is a conservative
// balance for a drop-the-middle truncator that a bench win would motivate exposing as config.
const defaultKeepLastTurns = 4

// truncateGapNote is the static user-role gap note inserted at the cut (the sim's optional
// TruncateNote, here a fixed string since no operator supplies one). apogee-sim relied on the
// proxy's role-alternation sanitizer to merge this note into the preceding user message; apogee
// has no such sanitizer, so the note stands as its own user message after the protected prefix
// (a faithful port of the drop-the-middle + static gap-note behaviour — intervention.go:180-181
// @pin — not the sim's merge side effect).
const truncateGapNote = "[Earlier conversation history was omitted to keep the context window within budget.]"

// truncateHistoryMechanism is the history-rewrite Mechanism that drops the middle of the
// conversation (catalogue Table A `truncate_history`; ported from apogee-sim
// internal/sim/intervention.go:99-183 @pin — truncateHistory). It keeps the protected prefix
// (leading system messages + the first user message, Conversation.PrefixEnd) and the last
// keepLastTurns assistant-anchored exchanges, cutting only at Conversation.AssistantBoundaries()
// so a tool result stays adjacent to the assistant call that produced it (strict chat templates
// reject an orphaned tool message). When something is dropped it inserts gapNote as a user
// message at the cut; when nothing would drop it is a no-op (and books no fire — the loop keys
// acted fires on Conversation.Revision, R4). It carries no per-Mechanism state: the descriptor's
// strikes-3 policy routes self-regulation through the loop's per-Session tracker (item 3).
type truncateHistoryMechanism struct {
	keepLastTurns int
	gapNote       string
}

// newTruncateHistory builds the truncate_history Mechanism with the built-in defaults. It needs no
// injected Deps (D3): truncation reads and mutates only the Conversation it is handed.
func newTruncateHistory(Deps) (domain.Mechanism, error) {
	return truncateHistoryMechanism{keepLastTurns: defaultKeepLastTurns, gapNote: truncateGapNote}, nil
}

// Descriptor identifies truncate_history as a strikes-3 proactive-nudge Mechanism (catalogue
// Table A, footnote 2: a context-shaper is neither off-ramp nor response-repair; proactive-nudge
// carries the Bypass semantics — disabled under Bypass, D5 — while the structural Budget and
// Compaction stay on, D6). It is withdrawn by self-regulation after repeated non-help.
func (truncateHistoryMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          truncateHistoryID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares no constraints (catalogue Table A: "none — cut only at AssistantBoundaries(),
// never PrefixEnd()"): the truncator rewrites history independently of any other Mechanism.
func (truncateHistoryMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// RewriteHistory drops the middle of conv, keeping the protected prefix and the last keepLastTurns
// assistant-anchored exchanges, then inserts the gap note at the cut. It is a no-op — no mutation —
// when fewer than keepLastTurns exchanges exist beyond the prefix, or the tail already begins at
// the prefix (nothing to drop), matching apogee-sim truncateHistory's guard so a pointless gap
// note is never inserted. It is also a no-op when re-run on an already-truncated, ungrown history:
// re-dropping and re-inserting the same gap note would rebuild the identical shape while bumping
// Revision, booking a phantom acted-fire (R4).
func (m truncateHistoryMechanism) RewriteHistory(_ context.Context, conv *domain.Conversation) error {
	prefixEnd := conv.PrefixEnd()
	tailStart, ok := m.tailStart(conv, prefixEnd)
	if !ok {
		return nil
	}
	// Already-truncated, ungrown history: when the only message the pending drop would remove is
	// the gap note we inserted last time (drop range == the single message at prefixEnd, and that
	// message IS the gap note), re-dropping and re-inserting an identical note rebuilds the same
	// shape but bumps Revision twice — the phantom acted-fire the loop keys on (R4). Return
	// without mutating instead. The truncation CONTENT stays sim-faithful (apogee-sim re-drops
	// and re-inserts here); only apogee's fire booking is wrong, so the grown-history path
	// (tailStart > prefixEnd+1, where real middle sits after the old note) is untouched.
	if m.gapNote != "" && tailStart == prefixEnd+1 {
		if at := conv.At(prefixEnd); at.Role == domain.RoleUser && at.Content == m.gapNote {
			return nil
		}
	}
	// DropRange first so the tail slides to prefixEnd; then Insert the note before it, yielding
	// prefix + gap note + tail — the exact shape apogee-sim truncateHistory builds.
	conv.DropRange(prefixEnd, tailStart)
	if m.gapNote != "" {
		conv.Insert(prefixEnd, domain.Message{Role: domain.RoleUser, Content: m.gapNote})
	}
	return nil
}

// tailStart is the index the kept tail begins at: the keepLastTurns-th assistant boundary counting
// back from the end, considering only boundaries at or beyond prefixEnd. ok is false when there are
// fewer than keepLastTurns such boundaries, or the tail would start at/inside the protected prefix
// — the "nothing to drop" cases (apogee-sim: `remaining > 0 || tailStart <= prefixEnd`). A
// non-positive keepLastTurns is treated as nothing-to-drop (the sim's KeepLastTurns <= 0 no-op).
func (m truncateHistoryMechanism) tailStart(conv *domain.Conversation, prefixEnd int) (int, bool) {
	if m.keepLastTurns <= 0 {
		return 0, false
	}
	boundaries := conv.AssistantBoundaries() // ascending assistant indices
	kept := 0
	tailStart := conv.Len()
	for i := len(boundaries) - 1; i >= 0; i-- {
		if boundaries[i] < prefixEnd {
			break // ascending: everything earlier is inside the prefix too
		}
		tailStart = boundaries[i]
		kept++
		if kept == m.keepLastTurns {
			break
		}
	}
	if kept < m.keepLastTurns || tailStart <= prefixEnd {
		return 0, false
	}
	return tailStart, true
}
