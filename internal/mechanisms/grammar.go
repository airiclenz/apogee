package mechanisms

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// grammar registers the response-format grammar-constraint pre-request Mechanism in the catalogue
// constructor table (Phase-4 item 10, Wave 3 request shapers). Default-off (D1) — the config
// surface builds it only when the `mechanisms:` block enables it. It is ported from apogee-sim
// internal/grammar/grammar.go + internal/proxy/proxy.go injectGrammarConstraint @pin: it derives a
// json_schema from the current tool menu and sets it as the request's `response_format` so a model
// that cannot emit native tool calls is constrained to a valid tool-call shape. It is
// backend-capability gated (catalogue Table A/C): the gate is the D3-injected Deps.GrammarConstraint
// (see its doc) — false on every current apogee backend, so grammar no-ops (catalogue Table B: "may
// no-op on all current apogee backends").
func init() { catalogue[grammarID] = newGrammar }

const grammarID domain.MechanismID = "grammar"

// grammarResponseFormatKey is the request extra grammar sets — the OpenAI-compatible structured
// output field (apogee-sim proxy.go:635/657 @pin). NOTE: the provider wire does not carry request
// extras yet (internal/agent/loop.go toProviderRequest drops SetExtra fields — "response_format is
// a Phase-4 concern"), so even when the gate is on, the constraint does not reach the model until
// that carrier lands; grammar's SetExtra is otherwise a faithful, self-regulating fire.
const grammarResponseFormatKey = "response_format"

// grammarMechanism is the pre-request Mechanism that constrains tool-call output shape (catalogue
// Table A `grammar`). constrain is the D3-injected backend capability captured at construction
// (Deps.GrammarConstraint); when false the Mechanism is inert. It carries no other per-Mechanism
// state: the descriptor's strikes-3 policy routes self-regulation through the loop's per-Session
// tracker (item 3).
type grammarMechanism struct {
	constrain bool
}

// newGrammar builds the grammar Mechanism, capturing the backend-capability gate from Deps (D3):
// the capability is probed once at construction, never per fire. With Deps.GrammarConstraint false
// (every current apogee backend) the built Mechanism registers but no-ops.
func newGrammar(deps Deps) (domain.Mechanism, error) {
	return grammarMechanism{constrain: deps.GrammarConstraint}, nil
}

// Descriptor identifies grammar as a strikes-3 proactive-nudge Mechanism (catalogue Table A):
// disabled under Bypass (D5), withdrawn by self-regulation after repeated non-help.
func (grammarMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          grammarID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares no constraints (catalogue Table A: "none — backend-capability gated"): the
// grammar constraint is derived from the tool menu independently of any other shaper.
func (grammarMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PreRequest sets a json_schema `response_format` constraining the model to a valid tool call over
// the current menu, when the backend needs it (m.constrain), tools are present, and no
// `response_format` is already set (an existing one wins — apogee-sim proxy.go:635 @pin). It is a
// no-op — booking no fire (the loop keys acted fires on Request.Revision, R4) — otherwise, or if the
// schema cannot be built (grammar never breaks a request, matching the sim's error swallow).
func (m grammarMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	if !m.constrain {
		return nil
	}
	tools := req.View().Tools()
	if len(tools) == 0 {
		return nil
	}
	if _, has := req.Extra(grammarResponseFormatKey); has {
		return nil
	}
	schema, err := grammarSchemaForTools(tools)
	if err != nil {
		return nil
	}
	wrapper, err := grammarWrapResponseFormat(schema)
	if err != nil {
		return nil
	}
	req.SetExtra(grammarResponseFormatKey, wrapper)
	return nil
}

// grammarSchemaForTools builds a json-schema that admits exactly one tool call: an object with a
// "name" enum over the tool names and an "arguments" object, plus a per-tool oneOf branch pinning
// arguments to each tool's own parameter schema (apogee-sim grammar.SchemaForTools @pin). A tool
// with an unparseable schema falls back to a bare object, matching the sim.
func grammarSchemaForTools(tools []domain.ToolDef) (json.RawMessage, error) {
	names := make([]string, 0, len(tools))
	branches := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)

		var argSchema any = map[string]any{"type": "object"}
		if len(t.Schema) > 0 {
			var parsed any
			if json.Unmarshal(t.Schema, &parsed) == nil {
				argSchema = parsed
			}
		}
		branches = append(branches, map[string]any{
			"properties": map[string]any{
				"name":      map[string]any{"const": t.Name},
				"arguments": argSchema,
			},
		})
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string", "enum": names},
			"arguments": map[string]any{"type": "object"},
		},
		"required": []string{"name", "arguments"},
		"oneOf":    branches,
	}
	return json.Marshal(schema)
}

// grammarWrapResponseFormat wraps a tool-call schema in the OpenAI-compatible json_schema
// response_format envelope (apogee-sim proxy.go:642-649 @pin).
func grammarWrapResponseFormat(schema json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "tool_call",
			"strict": true,
			"schema": schema,
		},
	})
}
