package security

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Dangerous-action guard (the footgun-guard floor — ADR 0012, NOT a boundary)
// ----------------------------------------------------------------------------

// Tier is the severity of a dangerous-action match. It is tighten-only: the guard runs
// ahead of the mode disposition and can only make a call stricter, never looser.
type Tier int

const (
	// TierNone means the call matched no dangerous-action rule.
	TierNone Tier = iota
	// TierForceApproval forces the Approver even in Auto — a speed-bump for an idiom
	// that is sometimes legitimate (curl | bash-class). A nil Approver downstream ⇒
	// refuse (the caller enforces this; the guard only reports the tier).
	TierForceApproval
	// TierHardRefuse refuses the call outright with a clear error result, in every
	// mode, with no per-call override (rm -rf of root/home/system, fork bombs, writes
	// to ~/.ssh / credential / persistence files).
	TierHardRefuse
)

// String renders a Tier for audit/log lines.
func (t Tier) String() string {
	switch t {
	case TierForceApproval:
		return "force-approval"
	case TierHardRefuse:
		return "hard-refuse"
	default:
		return "none"
	}
}

// Decision is the dangerous-action guard's verdict on a single tool call.
type Decision struct {
	Tier   Tier
	RuleID string // the matched rule's id ("" when Tier==TierNone)
	Reason string // the human-facing why (empty when Tier==TierNone)
}

// Triggered reports whether the call matched any rule (Tier above TierNone).
func (d Decision) Triggered() bool { return d.Tier != TierNone }

// Rule is one dangerous-action pattern. The pattern is matched against the call's
// whitespace-normalized inspectable text — the tool name, its target paths and its command
// lines, but NOT the payload a write carries (see payloadKeys). Matching is deliberately
// narrow literal/regex — there is no obfuscation-chasing (this is a footgun-guard catching
// obvious mistakes, NOT an adversary boundary, ADR 0012).
type Rule struct {
	// ID is the stable identifier used by the config merge to remove a rule (global
	// config may remove by ID). It must be non-empty and unique within a ruleset.
	ID string
	// Pattern is a Go regexp matched against the normalized inspectable text. The text
	// is whitespace-collapsed and lower-cased before matching, so a Pattern should be
	// written against single-spaced, lower-case input.
	Pattern string
	// Tier is the severity this rule asserts when it matches.
	Tier Tier
	// Reason is the human-facing explanation surfaced in the error / approval prompt.
	Reason string

	re *regexp.Regexp // compiled lazily by compile()
}

// DangerousActionGuard inspects a tool call against a tighten-only ruleset before the
// mode disposition runs (D6 / ADR 0012). It is the mode-independent floor: it runs in
// every mode, independent of the Confiner, and can only tighten a call. It is built once
// (rules compiled) and is safe for concurrent read use.
type DangerousActionGuard struct {
	rules []Rule // compiled, ordered: hard-refuse rules are consulted before force-approval
}

// NewDangerousActionGuard compiles rules into a guard, skipping any rule whose Pattern
// fails to compile (a malformed user-supplied rule must not crash the guard — it is
// dropped, never fatal). Rules are ordered so a TierHardRefuse match always wins over a
// TierForceApproval match on the same call.
func NewDangerousActionGuard(rules []Rule) *DangerousActionGuard {
	compiled := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.ID == "" || r.Pattern == "" || r.Tier == TierNone {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue // a malformed pattern is dropped, never fatal (footgun-guard, not a boundary)
		}
		r.re = re
		compiled = append(compiled, r)
	}
	// Hard-refuse first so the strictest matching tier is reported.
	sort.SliceStable(compiled, func(i, j int) bool { return compiled[i].Tier > compiled[j].Tier })
	return &DangerousActionGuard{rules: compiled}
}

// DefaultDangerousActionGuard returns the guard seeded with the built-in default
// ruleset (DefaultDangerousRules) — the default-on floor (ADR 0012).
func DefaultDangerousActionGuard() *DangerousActionGuard {
	return NewDangerousActionGuard(DefaultDangerousRules())
}

// Inspect reports the guard's verdict for call. It extracts the call's inspectable text
// (the tool name plus every string value in its JSON arguments except the payload-bearing
// ones — payloadKeys), normalizes it, and returns the strictest matching rule's Decision
// (TierNone when nothing matches). It never errors and never executes anything — pure
// inspection.
func (g *DangerousActionGuard) Inspect(call domain.ToolCall) Decision {
	text := normalize(inspectableText(call))
	for _, r := range g.rules {
		if r.re.MatchString(text) {
			return Decision{Tier: r.Tier, RuleID: r.ID, Reason: r.Reason}
		}
	}
	return Decision{Tier: TierNone}
}

// Rules returns a copy of the guard's compiled rules (id/pattern/tier/reason) for
// inspection and audit — without exposing the internal slice.
func (g *DangerousActionGuard) Rules() []Rule {
	out := make([]Rule, len(g.rules))
	for i, r := range g.rules {
		out[i] = Rule{ID: r.ID, Pattern: r.Pattern, Tier: r.Tier, Reason: r.Reason}
	}
	return out
}

// payloadKeys are the argument keys whose value is inert TEXT the tool stores, transmits or
// searches for — a file body, a replacement string, a search pattern, a commit message —
// rather than something the host will act on. Their values are excluded from the
// inspectable text.
//
// The rules describe ACTIONS, and an action is the tool plus its target path, command line
// or code — never the payload it carries. Matching payload text made the guard fire on
// documents that merely MENTION a guarded literal: writing this repo's own ADR 0012 (it
// names ~/.ssh) was hard-refused with no per-call override, and grepping for that literal
// was refused as a write. That is precision-over-recall inverted — the near-miss failure
// mode ADR 0012 forbids.
//
// This is a DENY-list, not an allow-list: an unrecognized key — an MCP tool's arbitrary
// argument — stays inspected, so recall narrows only for keys deliberately classified here.
// The keys that decide what the host does are deliberately absent: `command`, `code`,
// `path`, `url` and `task` are all still inspected, so a shell heredoc writing to ~/.ssh
// still matches (the heredoc lives in `command`).
var payloadKeys = map[string]bool{
	"content":    true, // file_edit: the body written
	"newcontent": true, // diff: the proposed body
	"oldtext":    true, // find_replace: the text searched for
	"newtext":    true, // find_replace: the text swapped in
	"pattern":    true, // grep: the search regex
	"message":    true, // git: the commit message
	"query":      true, // web_search: the search terms
	"question":   true, // ask_user: the prompt shown
	"choices":    true, // ask_user: the answers offered
	"title":      true, // present_document: the display heading
	"body":       true, // http_request: the request payload (its `url` stays inspected)
}

// keyPunctuation strips the separators that distinguish spellings of one argument name.
var keyPunctuation = strings.NewReplacer("_", "", "-", "")

// isPayloadKey reports whether an argument key carries payload text rather than an action.
// The key is folded to lower case with separators removed first, so `newContent`,
// `new_content` and `new-content` all resolve to the same payloadKeys entry.
func isPayloadKey(key string) bool {
	return payloadKeys[keyPunctuation.Replace(strings.ToLower(key))]
}

// inspectableText pulls the strings the guard matches against out of a tool call: the
// tool name and every string leaf in the JSON arguments (command lines, paths, scripts)
// except the payload-bearing ones (isPayloadKey). A non-object / malformed argument
// payload degrades to the raw argument bytes, so a guard rule still sees the text even
// when the shape is unexpected.
func inspectableText(call domain.ToolCall) string {
	var b strings.Builder
	b.WriteString(call.Tool)
	b.WriteByte(' ')

	var decoded any
	if err := json.Unmarshal(call.Arguments, &decoded); err != nil {
		b.Write(call.Arguments) // unparseable args: match against the raw bytes
		return b.String()
	}
	collectStrings(decoded, &b)
	return b.String()
}

// collectStrings walks a decoded JSON value appending every string leaf (space-joined) so
// the guard inspects command lines and paths regardless of which argument key carries them.
// A payload-bearing key's value is skipped whole, at any depth — that covers a nested
// payload such as a find_replace `replacements[].newText`, whose enclosing array stays
// inspected so any future action-bearing sibling key is still seen.
func collectStrings(v any, b *strings.Builder) {
	switch t := v.(type) {
	case string:
		b.WriteString(t)
		b.WriteByte(' ')
	case []any:
		for _, e := range t {
			collectStrings(e, b)
		}
	case map[string]any:
		for k, e := range t {
			if isPayloadKey(k) {
				continue
			}
			collectStrings(e, b)
		}
	}
}

// normalize collapses all whitespace runs to single spaces and lower-cases the text, so
// rule patterns are written against a single, predictable shape. This is the only
// "normalization" the guard does — it is deliberately not an obfuscation-resistant
// canonicaliser (ADR 0012: this is not the adversary game).
var whitespaceRun = regexp.MustCompile(`\s+`)

func normalize(s string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(strings.ToLower(s), " "))
}
