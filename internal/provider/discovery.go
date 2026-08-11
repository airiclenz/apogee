package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// discoveryTimeout bounds a discovery probe so a hung server cannot stall construction
// (matches the TS oracle's DISCOVERY_TIMEOUT_MS).
const discoveryTimeout = 5 * time.Second

// DiscoveredModel is one model the Upstream advertises. ContextWindow is 0 when the
// server does not report it.
type DiscoveredModel struct {
	ID            string
	DisplayName   string
	ContextWindow int
}

// HintResolution records HOW discovery resolved the configured model id (the hint) against
// the advertised list. It is an observation, not a decision: a caller reads it to explain the
// outcome — a non-exact grade is what a startup notice reports — without re-deriving the match.
type HintResolution string

const (
	// HintFirstAdvertised: nothing was configured, so the first advertised model is active.
	HintFirstAdvertised HintResolution = "first-advertised"
	// HintExact: the configured id is advertised verbatim.
	HintExact HintResolution = "exact"
	// HintBaseSlug: the configured id is not advertised but the part before its first ':'
	// is — a variant slug such as "vendor/model:exacto". The FULL configured id stays
	// active; only the context window comes from the base entry.
	HintBaseSlug HintResolution = "base-slug"
	// HintTrusted: the configured id is not advertised and no base entry matched either, so
	// it is used as configured with an unknown (0) context window.
	HintTrusted HintResolution = "trusted"
)

// ModelInfo is the result of discovery: every advertised model, plus the resolved active
// model and its context window. ContextWindow is the *runtime* window reported by llama.cpp
// GET /props when available (the -c/--ctx-size the server was actually launched with);
// otherwise the active model's advertised window from /v1/models (context_length, else
// meta.n_ctx_train). It is 0 when unknown — the context reducers (TDD §8 #8) fall back to a
// configured default.
type ModelInfo struct {
	AvailableModels []DiscoveredModel
	ActiveModel     string
	ContextWindow   int

	// Resolution grades how ActiveModel was reached (see HintResolution). Discover always
	// sets it; the zero value only occurs on the error returns, which carry no model.
	Resolution HintResolution

	// RuntimeContextWindow is the window llama.cpp's GET /props reported, and 0 when that
	// probe found none — a server without /props, or one that did not report n_ctx. It is
	// the same number ContextWindow then carries (the /props value overrides the advertised
	// one); it is reported separately because WHICH probe answered is itself an observation:
	// `apogee probe` states the /v1/models and /props outcomes independently, and a
	// non-zero value here is what makes a server identifiable as llama.cpp-shaped.
	RuntimeContextWindow int

	// TotalSlots is the number of generation slots the same GET /props reported — the `--parallel N`
	// width the server was launched with — and 0 when that probe found none (no /props, or a server
	// that does not report the field). It rides beside RuntimeContextWindow because the two are one
	// fact seen twice: llama.cpp splits its one window into total_slots slots, so the window above is
	// PER SLOT (ADR 0024's per-slot honesty) and this is how many of them there are.
	//
	// It is the DISCOVERY half of the Parallel agents cap (ADR 0039 decision 2): a host whose server
	// entry pins no `parallel-agents:` resolves the cap from this number, and falls back to 1 —
	// strictly serial — when it is 0. Nothing here decides that; the number is reported and the
	// resolution belongs to whoever configured the pin.
	TotalSlots int
}

// Discover resolves the active model and its context window from the Upstream. It runs two
// probes under one deadline: GET /v1/models (the oracle's openai-models strategy — the
// authoritative source for the model list and the active model), then GET /props (the
// oracle's llamacpp-props strategy). When /props reports a runtime context window it
// *overrides* the model's advertised window, because /v1/models on llama.cpp reports the
// model's *training* context (meta.n_ctx_train) — often far larger than the window the
// server was actually loaded with. A non-200 or an unreachable server is an error, as is an
// empty model list when nothing is configured to fall back to; the /props probe is best-effort (a non-llama.cpp server has
// no /props, so any failure there just leaves the /v1/models value untouched). That same probe
// also reports how many generation slots the server was launched with (ModelInfo.TotalSlots).
func (c *Client) Discover(ctx context.Context) (ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	info, err := c.discoverModels(ctx)
	if err != nil {
		return ModelInfo{}, err
	}

	runtime, slots := c.discoverProps(ctx)
	if runtime > 0 {
		info.setRuntimeContextWindow(runtime)
	}
	info.TotalSlots = slots
	return info, nil
}

// discoverModels probes GET /v1/models and resolves the model list plus the active model.
func (c *Client) discoverModels(ctx context.Context) (ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+modelsPath, nil)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("apogee: build discovery request: %w", err)
	}
	c.setAuth(req.Header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("apogee: model discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ModelInfo{}, fmt.Errorf("apogee: model discovery: upstream HTTP %d", resp.StatusCode)
	}

	var decoded modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ModelInfo{}, fmt.Errorf("apogee: decode models: %w", err)
	}

	info := decoded.toModelInfo(c.activeModel())
	// An empty list is only fatal when nothing was configured: with a hint there is still a
	// model to run (trusted as configured), and the list being empty is the server's problem
	// to report on the next completion.
	if info.ActiveModel == "" {
		return ModelInfo{}, errors.New("apogee: model discovery: server returned no models")
	}
	return info, nil
}

// discoverProps probes llama.cpp's GET /props for the two facts it reports about how the server was
// launched: the runtime context window (default_generation_settings.n_ctx — the per-slot context)
// and the number of generation slots (total_slots — the `--parallel N` width). It is best-effort: a
// non-llama.cpp server returns a non-200 or omits either field, and any failure (including a
// cancelled context) yields 0 for both, so the caller keeps the /v1/models window and treats the
// slot count as unknown. It shares the caller's discovery deadline.
//
// Both come out of ONE response because they are one observation: asking twice would cost a second
// round trip and could straddle a restart that moved both numbers at once.
func (c *Client) discoverProps(ctx context.Context) (window, slots int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+propsPath, nil)
	if err != nil {
		return 0, 0
	}
	c.setAuth(req.Header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}

	var decoded propsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, 0
	}
	if decoded.DefaultGenerationSettings.NCtx > 0 {
		window = decoded.DefaultGenerationSettings.NCtx
	}
	if decoded.TotalSlots > 0 {
		slots = decoded.TotalSlots
	}
	return window, slots
}

// propsResponse is the subset of llama.cpp's GET /props payload we read: the runtime context
// window the server was launched with, reported per generation slot, and how many of those slots
// there are (the `--parallel N` width — ADR 0039's discovery source for the Parallel agents cap).
type propsResponse struct {
	DefaultGenerationSettings struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	TotalSlots int `json:"total_slots"`
}

// setRuntimeContextWindow overrides the active model's window with the authoritative runtime
// value from /props, updating both the top-level ContextWindow and the matching
// AvailableModels entry so a later model-switch reads the same number. It also records the
// value as the runtime one, so a caller can tell WHICH probe supplied the window. An active
// model that is not advertised (a base-slug or trusted resolution) matches no entry, so the
// list sync simply no-ops — the top-level window still carries the /props value.
func (info *ModelInfo) setRuntimeContextWindow(n int) {
	info.ContextWindow = n
	info.RuntimeContextWindow = n
	for i := range info.AvailableModels {
		if info.AvailableModels[i].ID == info.ActiveModel {
			info.AvailableModels[i].ContextWindow = n
			return
		}
	}
}

// modelsResponse is the /v1/models payload. context_length wins over meta.n_ctx_train for
// the context window, matching the oracle.
type modelsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextLength int    `json:"context_length"`
		Meta          struct {
			NCtxTrain int `json:"n_ctx_train"`
		} `json:"meta"`
	} `json:"data"`
}

// toModelInfo projects the payload onto ModelInfo, dropping id-less entries and resolving
// the active model from hint (the configured model) per resolveHint.
func (r modelsResponse) toModelInfo(hint string) ModelInfo {
	var models []DiscoveredModel
	for _, m := range r.Data {
		if m.ID == "" {
			continue
		}
		contextWindow := m.ContextLength
		if contextWindow == 0 {
			contextWindow = m.Meta.NCtxTrain
		}
		models = append(models, DiscoveredModel{
			ID:            m.ID,
			DisplayName:   m.Name,
			ContextWindow: contextWindow,
		})
	}

	info := ModelInfo{AvailableModels: models}
	info.ActiveModel, info.ContextWindow, info.Resolution = resolveHint(models, hint)
	return info
}

// resolveHint resolves the configured model id against the advertised list and reports the
// active model, its context window (0 when unknown) and how the two were reached.
//
// A configured id is TRUSTED, never substituted: whenever hint is non-empty it is the active
// model verbatim, so the same hint against the same list always resolves to the same id (the
// binding observer restates the hint every heartbeat and would otherwise ping-pong). An
// advertised entry only supplies the window — either the exact entry, or, for a variant slug
// like "vendor/model:exacto", the entry for the base slug before the first ':'. An unlisted id
// is used as-is with an unknown window, which leaves Budget and auto-compaction inactive
// exactly as an advertised model with no window does, and lets a genuinely wrong id fail loud
// on the next completion instead of silently running someone else's model. Only an empty hint
// falls back to the first advertised entry.
func resolveHint(models []DiscoveredModel, hint string) (active string, window int, grade HintResolution) {
	if hint == "" {
		if len(models) == 0 {
			return "", 0, HintFirstAdvertised
		}
		return models[0].ID, models[0].ContextWindow, HintFirstAdvertised
	}
	if advertised, ok := findAdvertised(models, hint); ok {
		return hint, advertised.ContextWindow, HintExact
	}
	if base, _, hasVariant := strings.Cut(hint, ":"); hasVariant && base != "" {
		if advertised, ok := findAdvertised(models, base); ok {
			return hint, advertised.ContextWindow, HintBaseSlug
		}
	}
	return hint, 0, HintTrusted
}

// findAdvertised returns the advertised model with exactly this id.
func findAdvertised(models []DiscoveredModel, id string) (DiscoveredModel, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return DiscoveredModel{}, false
}
