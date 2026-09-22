package agent

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// bindingCells is the slice of a Config a serverBinding may write — one field per binding field,
// read off a Config so a projection is judged on exactly the cells the value owns and nothing the
// parent carries beside them (its tools, its sinks, its mode) can hide a stray write.
type bindingCells struct {
	Endpoint, APIKey, Wire, ServerName, ServerDescription, Model, SystemPrompt string
	MaxContextTokens, WorkingWindow, MaxOutputTokens                           int
	ResponseReserveFraction                                                    float64
	Profile                                                                    domain.ModelProfile
	EffortDialect                                                              domain.EffortDialect
	Bypass                                                                     bool
}

func cellsOf(cfg domain.Config) bindingCells {
	return bindingCells{
		Endpoint:                cfg.Endpoint,
		APIKey:                  cfg.APIKey,
		Wire:                    cfg.Wire,
		ServerName:              cfg.ServerName,
		ServerDescription:       cfg.ServerDescription,
		Model:                   cfg.Model,
		SystemPrompt:            cfg.SystemPrompt,
		MaxContextTokens:        cfg.Context.MaxContextTokens,
		WorkingWindow:           cfg.Context.WorkingWindow,
		MaxOutputTokens:         cfg.Context.MaxOutputTokens,
		ResponseReserveFraction: cfg.Context.ResponseReserveFraction,
		Profile:                 cfg.Profile,
		EffortDialect:           cfg.EffortDialect,
		Bypass:                  cfg.Bypass,
	}
}

// bindingParent seeds every cell with a value no spec in the table below states, so a cell that
// survives a projection is visibly the PARENT's and a cell that moves is visibly the spec's.
func bindingParent() domain.Config {
	cfg := domain.Config{
		Endpoint:          "http://session.local:9999",
		APIKey:            "session-key",
		Wire:              "openai",
		ServerName:        "session",
		ServerDescription: "the orchestrator's box",
		Model:             "smart-70b",
		SystemPrompt:      "parent prompt",
		Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{
			Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>",
		}},
		EffortDialect: domain.EffortDialectKwargs,
		Bypass:        true,
	}
	cfg.Context.MaxContextTokens = 131072
	cfg.Context.WorkingWindow = 100000
	cfg.Context.MaxOutputTokens = 4096
	cfg.Context.ResponseReserveFraction = 0.25
	return cfg
}

// TestServerBindingApplyTo pins the one presence rule — absent keeps, present replaces as written,
// the zero included — across the three constructors that own today's per-spec conventions. Each
// row states the cells it expects to MOVE off the seeded parent; every cell it does not name must
// come through untouched.
func TestServerBindingApplyTo(t *testing.T) {
	t.Parallel()

	parent := bindingParent()
	// The values the specs state, chosen to differ from every parent cell.
	const (
		endpoint = "http://grunt.local:1111"
		key      = "grunt-key"
		wire     = "anthropic"
		model    = "cheap-4b"
		window   = 32768
		working  = 24000
		ceiling  = 512
	)
	const share = 0.4
	profile := domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"web_search"}}}
	yes, no := true, false
	intPtr := func(v int) *int { return &v }
	floatPtr := func(v float64) *float64 { return &v }

	tests := []struct {
		name    string
		binding serverBinding
		want    func(c *bindingCells)
	}{
		{
			name:    "an empty binding keeps every cell",
			binding: serverBinding{},
			want:    func(*bindingCells) {},
		},
		{
			name: "rebind states the per-model bindings and the dialect, the zero included",
			binding: RebindSpec{
				Model:            model,
				SystemPrompt:     "",
				MaxContextTokens: 0,
				Profile:          domain.ModelProfile{},
				EffortDialect:    provider.EffortDialectNone,
			}.binding(),
			want: func(c *bindingCells) {
				c.Model = model
				c.SystemPrompt = ""
				c.MaxContextTokens = 0
				c.Profile = domain.ModelProfile{}
				c.EffortDialect = domain.EffortDialectNone
			},
		},
		{
			name: "rebind's two optional bounds pass through when stated, the zero included",
			binding: RebindSpec{
				Model:                   model,
				SystemPrompt:            "child prompt",
				MaxContextTokens:        window,
				MaxOutputTokens:         intPtr(0),
				ResponseReserveFraction: floatPtr(0),
				Profile:                 profile,
				EffortDialect:           provider.EffortDialectOpenAI,
			}.binding(),
			want: func(c *bindingCells) {
				c.Model = model
				c.SystemPrompt = "child prompt"
				c.MaxContextTokens = window
				c.MaxOutputTokens = 0
				c.ResponseReserveFraction = 0
				c.Profile = profile
				c.EffortDialect = domain.EffortDialectOpenAI
			},
		},
		{
			name: "switch states the dial facts, the seat's words and all four bounds, and unbinds the model",
			binding: UpstreamSpec{
				Endpoint:                endpoint,
				APIKey:                  key,
				Wire:                    wire,
				ServerName:              "grunt",
				ServerDescription:       "the cheap box",
				MaxContextTokens:        window,
				WorkingWindow:           working,
				MaxOutputTokens:         ceiling,
				ResponseReserveFraction: share,
			}.binding(),
			want: func(c *bindingCells) {
				c.Endpoint = endpoint
				c.APIKey = key
				c.Wire = wire
				c.ServerName = "grunt"
				c.ServerDescription = "the cheap box"
				c.Model = ""
				c.MaxContextTokens = window
				c.WorkingWindow = working
				c.MaxOutputTokens = ceiling
				c.ResponseReserveFraction = share
			},
		},
		{
			name:    "switch applies its zero bounds and clears the seat's words as written",
			binding: UpstreamSpec{Endpoint: endpoint}.binding(),
			want: func(c *bindingCells) {
				c.Endpoint = endpoint
				c.APIKey = ""
				c.Wire = ""
				c.ServerName = ""
				c.ServerDescription = ""
				c.Model = ""
				c.MaxContextTokens = 0
				c.WorkingWindow = 0
				c.MaxOutputTokens = 0
				c.ResponseReserveFraction = 0
			},
		},
		{
			name: "target states the dial facts, the model, the window, both inner bounds, the share, the profile, the dialect and the posture",
			binding: (&DelegationTarget{
				Endpoint:                endpoint,
				APIKey:                  key,
				Wire:                    wire,
				Model:                   model,
				ContextWindow:           window,
				WorkingWindow:           working,
				MaxOutputTokens:         ceiling,
				ResponseReserveFraction: share,
				Profile:                 profile,
				EffortDialect:           provider.EffortDialectReasoning,
				Bypass:                  &no,
			}).binding(),
			want: func(c *bindingCells) {
				c.Endpoint = endpoint
				c.APIKey = key
				c.Wire = wire
				c.Model = model
				c.MaxContextTokens = window
				c.WorkingWindow = working
				c.MaxOutputTokens = ceiling
				c.ResponseReserveFraction = share
				c.Profile = profile
				c.EffortDialect = domain.EffortDialectReasoning
				c.Bypass = false
			},
		},
		{
			name: "target with no window, no share, no dialect and no posture keeps the parent's four and zeroes the rest",
			binding: (&DelegationTarget{
				Endpoint: endpoint,
				Model:    model,
			}).binding(),
			want: func(c *bindingCells) {
				c.Endpoint = endpoint
				c.APIKey = ""
				c.Wire = ""
				c.Model = model
				c.WorkingWindow = 0
				c.MaxOutputTokens = 0
				c.Profile = domain.ModelProfile{}
			},
		},
		{
			name: "target's negative window folds in with none",
			binding: (&DelegationTarget{
				Endpoint:      endpoint,
				Model:         model,
				ContextWindow: -1,
				Bypass:        &yes,
			}).binding(),
			want: func(c *bindingCells) {
				c.Endpoint = endpoint
				c.APIKey = ""
				c.Wire = ""
				c.Model = model
				c.WorkingWindow = 0
				c.MaxOutputTokens = 0
				c.Profile = domain.ModelProfile{}
				c.Bypass = true
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := cellsOf(parent)
			tc.want(&want)

			got := cellsOf(tc.binding.applyTo(parent))

			if !reflect.DeepEqual(got, want) {
				t.Errorf("applyTo cells:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestServerBindingApplyToIsPure pins that a projection never writes through to the Config it was
// handed — the property Rebind's validate-then-commit and the routed spawn's Config composition
// both rest on.
func TestServerBindingApplyToIsPure(t *testing.T) {
	t.Parallel()

	parent := bindingParent()
	before := cellsOf(parent)

	_ = UpstreamSpec{Endpoint: "http://elsewhere.local:1"}.binding().applyTo(parent)

	if got := cellsOf(parent); !reflect.DeepEqual(got, before) {
		t.Errorf("applyTo wrote through to its argument:\n got %+v\nwant %+v", got, before)
	}
}
