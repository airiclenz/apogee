package tools

import "github.com/airiclenz/apogee/internal/domain"

// DelegationOffer is what one roster publishes for delegation: the variant of its sub_agent tool
// and the variant of its fan_out tool. It is the single type that answers "which delegation
// arguments was this model offered?" — the seat choice (`run_on`), `background` and `context` —
// so no caller has to probe the two tools' concrete types to learn it.
//
// It is comparable, and its zero value means a roster whose sub_agent and fan_out are both absent
// or plain: a nil roster, one holding neither tool, and one holding a foreign tool under either
// name all offer the zero value.
type DelegationOffer struct {
	// SubAgent is the variant the roster's sub_agent tool publishes.
	SubAgent SubAgentOptions
	// FanOut is the variant the roster's fan_out tool publishes.
	FanOut FanOutOptions
}

// OfferOf reads the delegation offer roster publishes. It holds the only type assertion on
// *SubAgent and *FanOut in this package: a nil roster, a missing tool and a foreign tool under
// either name each give the zero value for that tool's half.
func OfferOf(roster *domain.ToolRegistry) DelegationOffer {
	var offer DelegationOffer
	if roster == nil {
		return offer
	}
	if tool, ok := roster.Lookup(SubAgentToolName); ok {
		if spawner, ok := tool.(*SubAgent); ok {
			offer.SubAgent = spawner.opts
		}
	}
	if tool, ok := roster.Lookup(FanOutToolName); ok {
		if fanOut, ok := tool.(*FanOut); ok {
			offer.FanOut = fanOut.opts
		}
	}
	return offer
}

// Child is the offer a delegate's roster carries when its parent's roster publishes o. It keeps
// sub_agent's `context` only: the `sub-agents-context` gate covers nested children too, and the
// engine reads the named files wherever the call is made. Everything else is a depth-0 offer and
// is dropped — the seat choice, because a delegation below the first hop keeps the seat it landed
// on (ADR 0069 D3), and `background` on both tools, because a background workflow belongs to the
// top-level Agent (ADR 0089 D1, ADR 0094 D1). Removing an argument from the schema, rather than
// accepting and discarding it, is the honest form: a schema advertising a knob the engine ignores
// teaches the model a lie about its own leverage. Child is idempotent, and it never adds an
// argument o did not publish, so it is never a privilege change (ADR 0005).
func (o DelegationOffer) Child() DelegationOffer {
	return DelegationOffer{SubAgent: SubAgentOptions{Context: o.SubAgent.Context}}
}

// ChildRoster returns roster with its delegation tools narrowed to the offer's Child. When the
// child offer equals the one roster publishes it returns the same pointer, unchanged — a nil
// roster included. Otherwise it builds a new registry in roster's menu order in which sub_agent —
// a foreign tool under that name included — becomes the child's sub_agent variant, and fan_out
// becomes the plain variant only when roster's own fan_out published a choice, so a plain or
// foreign fan_out keeps its value. Every other tool arrives verbatim, so the subset property
// Subset gives a child's roster (ADR 0005) is untouched.
func ChildRoster(roster *domain.ToolRegistry) *domain.ToolRegistry {
	offer := OfferOf(roster)
	child := offer.Child()
	if child == offer {
		return roster
	}
	narrowed := domain.NewToolRegistry()
	for _, tool := range roster.All() {
		switch {
		case tool.Name() == SubAgentToolName:
			tool = NewSubAgentWith(child.SubAgent)
		case tool.Name() == FanOutToolName && offer.FanOut != (FanOutOptions{}):
			tool = NewFanOutWith(child.FanOut)
		}
		// Cannot fail: the names come from a registry, so each is non-empty and appears once.
		_ = narrowed.Register(tool)
	}
	return narrowed
}
