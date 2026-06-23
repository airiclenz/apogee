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
// model and its context window (the configured model when the server lists it, else the
// first advertised). ContextWindow is 0 when unknown — the context reducers (TDD §8 #8)
// fall back to a configured default.
type ModelInfo struct {
	AvailableModels []DiscoveredModel
	ActiveModel     string
	ContextWindow   int
}

// Discover probes GET /v1/models and resolves the active model. It ports the oracle's
// openai-models strategy (the OpenAI-compatible path P1.1 targets); the ollama and
// llama.cpp /props strategies are deferred to the context phase, which needs the runtime
// context window. A non-200, an unreachable server, or an empty list is an error.
func (c *Client) Discover(ctx context.Context) (ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

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

	info := decoded.toModelInfo(c.model)
	if len(info.AvailableModels) == 0 {
		return ModelInfo{}, errors.New("apogee: model discovery: server returned no models")
	}
	return info, nil
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
