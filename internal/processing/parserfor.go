package processing

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// ContentStripper separates a model's inline reasoning channel from its visible content. It is
// the unified seam the loop calls uniformly for every thinking style (none / delimited / harmony)
// so the agent never branches on harmony-vs-delimited: ParserFor selects the concrete stripper
// from the model profile, and the loop calls Strip once per reply. A native/no-inline-thinking
// profile gets the no-op stripper, so the content path is byte-identical to the pre-profile loop.
type ContentStripper interface {
	// Strip separates raw into the visible content the user sees and the reasoning removed
	// from it. reasoning is "" when the message carried no inline channel; the loop preserves
	// it as reasoning_content in history.
	Strip(raw string) (visible, reasoning string)
	// IsMidChannel reports whether raw ends inside an unclosed reasoning span — the streaming
	// guard a live-token consumer uses to hold emission while the model is mid-reasoning. It is
	// always false for the no-op stripper, so a native stream emits every delta immediately.
	IsMidChannel(raw string) bool
}

// ParserFor translates a declarative domain.ModelProfile into the loop's two parse-seam
// collaborators: the text-format tool-call parser and the inline-channel content stripper. It is
// the boundary where domain profile data becomes processing behaviour (ADR 0010) — it maps the
// profile onto the package's existing, frozen ToolCallingConfig / ThinkingConfig and calls the
// existing NewToolCallParser, so the oracle-tested parsers and their config types are reused, not
// duplicated in internal/agent. A bad profile (an unknown tool-call format, or an unknown thinking
// style) is returned as an error so construction fails loudly rather than silently falling back to
// native. A zero ModelProfile yields the native no-op parser and the no-op stripper — today's
// exact behaviour.
func ParserFor(p domain.ModelProfile) (ToolCallParser, ContentStripper, error) {
	parser, err := NewToolCallParser(ToolCallingConfig{
		Format:      ToolCallFormat(p.ToolCallFormat),
		CustomRegex: CustomRegexConfig{Pattern: p.Pattern},
	})
	if err != nil {
		return nil, nil, err
	}
	stripper, err := stripperFor(p.Thinking)
	if err != nil {
		return nil, nil, err
	}
	return parser, stripper, nil
}

// stripperFor selects the ContentStripper for a thinking profile. "" and ThinkingNone both mean
// no inline channel (the no-op stripper); ThinkingDelimited wraps StripThinking with the profile's
// literal Start/End tokens; ThinkingHarmony wraps StripHarmony. An unknown style is an error so a
// misconfigured profile fails construction rather than silently leaving reasoning in visible text.
func stripperFor(t domain.ThinkingProfile) (ContentStripper, error) {
	switch t.Style {
	case "", domain.ThinkingNone:
		return noneStripper{}, nil
	case domain.ThinkingDelimited:
		return delimitedStripper{cfg: &ThinkingConfig{StartToken: t.Start, EndToken: t.End}}, nil
	case domain.ThinkingHarmony:
		return harmonyStripper{}, nil
	default:
		return nil, fmt.Errorf("processing: unknown thinking style %q", t.Style)
	}
}

// noneStripper is the no-op stripper for a model with no inline thinking channel: Strip returns
// the content untouched with no reasoning, and IsMidChannel is always false. It is the byte-
// identical anchor — a native/none profile's content path is exactly the pre-profile loop's.
type noneStripper struct{}

func (noneStripper) Strip(raw string) (string, string) { return raw, "" }
func (noneStripper) IsMidChannel(string) bool          { return false }

// delimitedStripper strips a literal Start/End thinking-token pair (e.g.
// <think>…</think>) over the frozen StripThinking / IsThinking oracle functions.
type delimitedStripper struct {
	cfg *ThinkingConfig
}

func (d delimitedStripper) Strip(raw string) (string, string) {
	s := StripThinking(raw, d.cfg)
	return s.Visible, s.Reasoning
}

func (d delimitedStripper) IsMidChannel(raw string) bool { return IsThinking(raw, d.cfg) }

// harmonyStripper strips the full gpt-oss harmony channel set over the frozen StripHarmony /
// IsHarmonyThinking oracle functions. StripHarmony yields three streams but ContentStripper
// returns two, so the stripper folds the commentary (tool-planning) channel into the reasoning
// return — reasoning first, blank-line joined — because both are the model's private channel and
// dropping the tool-planning text would lose it from history (D3).
type harmonyStripper struct{}

func (harmonyStripper) Strip(raw string) (string, string) {
	h := StripHarmony(raw)
	return h.Visible, joinNonEmpty(h.Reasoning, h.Commentary)
}

func (harmonyStripper) IsMidChannel(raw string) bool { return IsHarmonyThinking(raw) }

// joinNonEmpty joins a and b with a blank line, dropping either when empty, so a present channel
// is never padded with a leading or trailing blank line from an absent one.
func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}
