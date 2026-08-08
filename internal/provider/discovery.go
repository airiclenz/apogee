package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
// server was actually loaded with. A non-200, an unreachable server, or an empty model list
// from /v1/models is an error; the /props probe is best-effort (a non-llama.cpp server has
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
	if len(info.AvailableModels) == 0 {
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
// value as the runtime one, so a caller can tell WHICH probe supplied the window.
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
// the active model from hint (the configured model), falling back to the first advertised.
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
	if len(models) == 0 {
		return info
	}

	active := models[0]
	if hint != "" {
		for _, m := range models {
			if m.ID == hint {
				active = m
				break
			}
		}
	}
	info.ActiveModel = active.ID
	info.ContextWindow = active.ContextWindow
	return info
}
