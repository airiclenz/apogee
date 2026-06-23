package provider

import (
	"context"
	"errors"
)

// Placeholder is the Responder the engine binds when no real provider exists yet
// (all of Phase 0). It never answers — a Step against it surfaces a loop ErrorEvent —
// because the real OpenAI-compatible client lands in Phase 1 (P1.1); tests inject a
// deterministic fake instead. This keeps the pre-provider slice hermetic and
// dependency-free, and it lives here beside the wire seam so the real client replaces
// it in place.
type Placeholder struct{}

// Respond always fails: there is no Upstream provider until Phase 1.
func (Placeholder) Respond(context.Context, Request) (RawResponse, error) {
	return RawResponse{}, errors.New("apogee: no Upstream provider configured (lands in Phase 1)")
}

// Placeholder satisfies the provider seam at compile time.
var _ Responder = Placeholder{}
