package domain

// This file holds the ONE implementation of "is this a valid Mechanism stack?" — the rule
// the pre-build catalogue check (internal/validated) and the post-build registry gates
// (registry.go) both read. It is the rule only: each caller renders its own wording,
// because a defect means something different at each of the two times it is checked.

// StackDefectKind classifies a Mechanism-stack defect. The two kinds are duals: a declared
// peer that must be present and is not, and a declared peer that must be absent and is not.
type StackDefectKind string

const (
	// StackMissingRequirement — the member declares a Requires peer absent from the stack.
	StackMissingRequirement StackDefectKind = "missing-requirement"
	// StackIncompatible — the member declares an IncompatibleWith peer that IS in the stack.
	StackIncompatible StackDefectKind = "incompatible"
)

// StackDefect is one violation of the stack-validity rule, named from the declaring side:
// Mechanism is the member whose descriptor declares the relation, Peer is the member it
// names. A defect carries no message — the wording belongs to whoever renders it.
type StackDefect struct {
	Kind      StackDefectKind
	Mechanism MechanismID
	Peer      MechanismID
}

// CheckStack reports every way descs is not a valid Mechanism stack (CONTEXT: Mechanism
// descriptor): no member may name a present member IncompatibleWith, and every member's
// Requires peers must themselves be present. It returns nil for a valid stack.
//
// It is the single home of that rule, read at two different TIMES. internal/validated calls
// it PRE-BUILD over catalogue descriptors, where a defect degrades softly — the entry is
// skipped and a warning printed, the floor still works. The registry calls it POST-BUILD
// over the constructed Mechanisms, where a defect is a loud startup failure wrapping
// ErrMissingRequirement or ErrIncompatibleMechanisms (ADR 0003; ADR 0014 §4). Share the
// rule, keep the timing — and keep the wording, which is why this returns structured
// defects rather than a formatted error.
//
// The defect order is load-bearing: defects come in the order descs is given, and within
// one member its Requires defects come before its IncompatibleWith ones. That is what lets
// each caller report the same first defect it reported when it owned its own copy of the
// rule. A requirement chain (A requires B, B requires C) is validated transitively by
// iteration, not recursion: every member's direct requirements are checked against the
// present set, so a broken link anywhere in the chain is reported (checking B catches an
// absent C independently of A).
func CheckStack(descs []MechanismDescriptor) []StackDefect {
	if len(descs) == 0 {
		return nil
	}

	present := make(map[MechanismID]bool, len(descs))
	for _, d := range descs {
		present[d.ID] = true
	}

	var defects []StackDefect
	for _, d := range descs {
		for _, req := range d.Requires {
			if !present[req] {
				defects = append(defects, StackDefect{Kind: StackMissingRequirement, Mechanism: d.ID, Peer: req})
			}
		}
		for _, inc := range d.IncompatibleWith {
			if present[inc] {
				defects = append(defects, StackDefect{Kind: StackIncompatible, Mechanism: d.ID, Peer: inc})
			}
		}
	}
	return defects
}
