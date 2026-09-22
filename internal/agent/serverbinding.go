package agent

// The engine-side server binding as ONE value (2026-09-20 architecture review, plan C item 9).
// Three public specs move a session or a spawn onto a server — RebindSpec for a model change,
// UpstreamSpec for a `/server` switch, DelegationTarget for a routed delegation — and each used
// to carry its own copy of the rule for which Config fields it writes, which it skips, and what
// its zero means. serverBinding is that rule stated once: every field is a POINTER, presence is
// non-nil, and there is exactly one projection. What differs between the three specs is only
// WHICH fields each states, and that lives in the constructors below, where the spec's public
// contract (a plain int whose zero is applied, a pointer whose nil is silence, a window kept when
// the target names none) becomes a present-or-absent choice.
//
// Nothing here dials, locks or mutates an Agent: applyTo is a pure function over a Config copy,
// which is what lets the routed spawn — which has no child Agent yet when it composes the child's
// Config — project through the same value Rebind and SwitchUpstream do (ratified design call:
// "Presence on the engine binding is pointer fields").

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// serverBinding is one server's bindings with presence typed in: a nil field says NOTHING about
// that binding and whatever the Config holds stands; a non-nil field REPLACES the Config's value as
// written, the zero included. There is no third state — a "reset" is a present zero (a Model of
// `""` unbinds the model, a MaxOutputTokens of 0 drops the pin), never a special value, which is
// ADR 0046's own idiom for RebindSpec.MaxOutputTokens applied to every field alike.
type serverBinding struct {
	// The dial facts and the seat's human words (domain.Config's fields of the same name).
	Endpoint          *string
	APIKey            *string
	Wire              *string
	ServerName        *string
	ServerDescription *string
	// The per-model bindings: the wire model id and the system-prompt template.
	Model        *string
	SystemPrompt *string
	// The token bounds (domain.ContextConfig's fields of the same name).
	MaxContextTokens        *int
	WorkingWindow           *int
	MaxOutputTokens         *int
	ResponseReserveFraction *float64
	// Profile is the model profile with its tool-roster axis (ADR 0044, ADR 0057).
	Profile *domain.ModelProfile
	// EffortDialect is stated in the provider's vocabulary, as every spec states it, and is
	// mirrored onto the Config in the domain's through toDomainDialect (wire.go).
	EffortDialect *provider.EffortDialect
	// Bypass is the delegation posture flag (ADR 0045 §2).
	Bypass *bool
}

// applyTo projects the binding onto cfg and returns the result: every present field replaces the
// matching Config field as written, every absent field leaves it as cfg had it. It is PURE — cfg
// is a value, the return is a fresh value, and nothing else is read or written — so a caller that
// validates before committing (Rebind) projects onto a copy and assigns on success, and a caller
// composing a Config for an Agent that does not exist yet (the routed spawn) projects the same way.
func (b serverBinding) applyTo(cfg domain.Config) domain.Config {
	if b.Endpoint != nil {
		cfg.Endpoint = *b.Endpoint
	}
	if b.APIKey != nil {
		cfg.APIKey = *b.APIKey
	}
	if b.Wire != nil {
		cfg.Wire = *b.Wire
	}
	if b.ServerName != nil {
		cfg.ServerName = *b.ServerName
	}
	if b.ServerDescription != nil {
		cfg.ServerDescription = *b.ServerDescription
	}
	if b.Model != nil {
		cfg.Model = *b.Model
	}
	if b.SystemPrompt != nil {
		cfg.SystemPrompt = *b.SystemPrompt
	}
	if b.MaxContextTokens != nil {
		cfg.Context.MaxContextTokens = *b.MaxContextTokens
	}
	if b.WorkingWindow != nil {
		cfg.Context.WorkingWindow = *b.WorkingWindow
	}
	if b.MaxOutputTokens != nil {
		cfg.Context.MaxOutputTokens = *b.MaxOutputTokens
	}
	if b.ResponseReserveFraction != nil {
		cfg.Context.ResponseReserveFraction = *b.ResponseReserveFraction
	}
	if b.Profile != nil {
		cfg.Profile = *b.Profile
	}
	if b.EffortDialect != nil {
		cfg.EffortDialect = toDomainDialect(*b.EffortDialect)
	}
	if b.Bypass != nil {
		cfg.Bypass = *b.Bypass
	}
	return cfg
}

// binding states a model change as a serverBinding: the per-model bindings — Model, SystemPrompt,
// MaxContextTokens, Profile — and the server's EffortDialect are always present, applied as the
// spec states them, the zero included (see each field's contract in rebind.go); the two optional
// bounds pass through as the pointers they already are, so a spec silent about a bound leaves it
// exactly where the bind or the move that set it put it. The dial facts, the seat's words and the
// working window are never a model change's to state, so they stay absent.
func (s RebindSpec) binding() serverBinding {
	return serverBinding{
		Model:                   &s.Model,
		SystemPrompt:            &s.SystemPrompt,
		MaxContextTokens:        &s.MaxContextTokens,
		MaxOutputTokens:         s.MaxOutputTokens,
		ResponseReserveFraction: s.ResponseReserveFraction,
		Profile:                 &s.Profile,
		EffortDialect:           &s.EffortDialect,
	}
}

// binding states a server switch as a serverBinding: the dial facts and the seat's human words are
// present, and so are all four token bounds — applied as the spec states them, the zeroes included,
// because an absent pin is a fact about the new server rather than a licence to keep the retired
// one's number (see each field's contract in rebind.go). Model is present as `""`: a switch UNBINDS
// the model rather than guessing what the new server serves (ADR 0024), and that unbinding is a
// present zero here, not an absence. The system prompt, the profile, the effort dialect and the
// posture stand until the new server's first observed model binds through Rebind.
func (s UpstreamSpec) binding() serverBinding {
	unbound := ""
	return serverBinding{
		Endpoint:                &s.Endpoint,
		APIKey:                  &s.APIKey,
		Wire:                    &s.Wire,
		ServerName:              &s.ServerName,
		ServerDescription:       &s.ServerDescription,
		Model:                   &unbound,
		MaxContextTokens:        &s.MaxContextTokens,
		WorkingWindow:           &s.WorkingWindow,
		MaxOutputTokens:         &s.MaxOutputTokens,
		ResponseReserveFraction: &s.ResponseReserveFraction,
	}
}

// binding states a routed delegation's target as a serverBinding, with every field's contract from
// delegationtarget.go read as presence: the dial facts, Model, WorkingWindow, MaxOutputTokens and
// Profile are always present (their zeroes ARE the target's answer); ContextWindow is present only
// when positive, since a target that names no window leaves the parent's standing rather than
// building the child windowless, and a negative cannot be meant so it folds in with 0;
// ResponseReserveFraction is present only when positive, since an entry that states no share
// leaves the parent's run-wide split standing; EffortDialect is present only when the target names
// one, since the zero there says "this target names none" and keeps the parent's shape; Bypass
// passes through as the pointer it already is (ADR 0045 §2's replace-or-inherit rule). The seat's
// human words and the system prompt are not a target's to state.
func (t *DelegationTarget) binding() serverBinding {
	b := serverBinding{
		Endpoint:        &t.Endpoint,
		APIKey:          &t.APIKey,
		Wire:            &t.Wire,
		Model:           &t.Model,
		WorkingWindow:   &t.WorkingWindow,
		MaxOutputTokens: &t.MaxOutputTokens,
		Profile:         &t.Profile,
		Bypass:          t.Bypass,
	}
	if t.ContextWindow > 0 {
		b.MaxContextTokens = &t.ContextWindow
	}
	if t.ResponseReserveFraction > 0 {
		b.ResponseReserveFraction = &t.ResponseReserveFraction
	}
	if t.EffortDialect != provider.EffortDialectNone {
		b.EffortDialect = &t.EffortDialect
	}
	return b
}
