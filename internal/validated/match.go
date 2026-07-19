package validated

import (
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// DecisionKind is what the match concluded for this session.
type DecisionKind int

const (
	// KindNone — no entry matches: the model runs the D1 floor, silently (an unknown
	// model needs no banner).
	KindNone DecisionKind = iota

	// KindApplied — the set applies this session (≥ medium confidence on a direct
	// match, or any confidence through the user's explicit alias).
	KindApplied

	// KindOffered — a direct label match at LOW confidence: automatism stays gated
	// (ADR 0016 §5), so the set is offered via the per-session notice naming the
	// one-line alias that applies it — the explicit human decision §3 requires.
	KindOffered
)

// Decision is the outcome of matching the resolved fingerprint against the merged
// entries. Entry is meaningful for KindApplied and KindOffered; ViaAlias records that
// the user's alias made the decision (the confidence gate was replaced by the human
// one), with AliasFrom naming the runtime label the alias translated.
type Decision struct {
	Kind      DecisionKind
	Entry     Entry
	ViaAlias  bool
	AliasFrom string
}

// DanglingAliasError is the LOUD startup refusal for a validated-sets alias whose
// target entry key does not exist — the user's own config referencing nothing, the
// exact posture ADR 0015 set for a removed Mechanism ID ("a loud unknown-ID error,
// never a silent no-op"). It lists the known keys so the fix is in the message.
type DanglingAliasError struct {
	Label  string   // the runtime fingerprint label the alias maps from
	Target string   // the entry key the alias names
	Known  []string // sorted known entry keys
}

func (e *DanglingAliasError) Error() string {
	known := "(none)"
	if len(e.Known) > 0 {
		known = strings.Join(e.Known, ", ")
	}
	return fmt.Sprintf("apogee: validated-sets alias %q -> %q names no known Validated-set entry; known: %s",
		e.Label, e.Target, known)
}

// Match resolves what the Validated-set surface does this session, given the resolved
// fingerprint (label + confidence), the user's alias map, and the merged entries.
//
// The alias is consulted first and at ANY confidence (ADR 0016 realisation): an
// identity mapping is the low-confidence confirm, a differing mapping is the §3
// transfer — either way a human decided, so the confidence gate is replaced, not
// weakened. A direct label match auto-applies only at ≥ medium confidence and is
// offered at low. A zero label matches nothing.
func Match(label string, confidence domain.FingerprintConfidence, alias map[string]string, entries map[string]Entry) (Decision, error) {
	if label == "" {
		return Decision{Kind: KindNone}, nil
	}

	if target, ok := alias[label]; ok {
		e, ok := entries[target]
		if !ok {
			return Decision{}, &DanglingAliasError{Label: label, Target: target, Known: sortedKeys(entries)}
		}
		return Decision{Kind: KindApplied, Entry: e, ViaAlias: true, AliasFrom: label}, nil
	}

	e, ok := entries[label]
	if !ok {
		return Decision{Kind: KindNone}, nil
	}
	if confidence >= domain.ConfidenceMedium {
		return Decision{Kind: KindApplied, Entry: e}, nil
	}
	return Decision{Kind: KindOffered, Entry: e}, nil
}

// sortedKeys renders the known entry keys for the dangling-alias message.
func sortedKeys(entries map[string]Entry) []string {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
