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

	// EffortSupport is what this ENTRY says about its own thinking-effort dial, read by the same
	// rule ModelInfo.EffortSupport is read by (see effortSupport) and carried per model because the
	// dial is a property of the MODEL, not of the server: a host deciding what a switch INTO a model
	// implies — the TUI clearing a session effort override the target rules out (ADR 0060 D8) — must
	// judge against the model it is switching to, and only this field can answer for a model that is
	// not the active one.
	//
	// The zero value is both "no dial" and "no tell to read", exactly as on ModelInfo: an entry
	// without a `reasoning` object says nothing, and a caller must treat that as unknown rather than
	// as a refusal. The /props chat template is deliberately NOT folded in here — it describes the
	// one model the server has LOADED, so it can only answer for the active one.
	EffortSupport EffortSupport
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

	// EffortSupport is what the same two probes said about the ACTIVE model's thinking-effort
	// dial (ADR 0060). Like the window and the slot count it is one best-effort observation: a
	// server that advertises no tell yields the zero value — Supported false — and never an
	// error, so a caller reads "not supported" and "not detectable" the same way.
	EffortSupport EffortSupport
}

// EffortSupport records what discovery saw about the active model's thinking-effort dial: whether
// the dial exists at all, which wire dialect reaches it, and — when the server states them — the
// level vocabulary the model reports and the level it defaults to.
//
// It is detected PASSIVELY, from the two payloads discovery already fetches, never from a probe
// call (ADR 0050's rejected bind-time probe, re-affirmed by ADR 0060): llama.cpp's GET /props
// carries the chat template, and an OpenAI-shaped GET /v1/models may carry a per-model `reasoning`
// object. Neither tell present is indistinguishable from a model with no dial, and both resolve to
// the zero value: Supported false, EffortDialectNone, no vocabulary, no default — which keeps the
// wire byte-identical for a caller that asks for nothing (ADR 0031).
//
// A server whose entry FORCES a dialect (WithEffortDialect) is answered from that instead, because
// the providers the key exists for advertise no tell at all: the forced value fills this same
// struct, so nothing downstream can tell a detected answer from a configured one — one channel from
// either source to the picker, the footer and the wire (ADR 0060 decision 3).
type EffortSupport struct {
	// Supported reports that the dial is usable on this model. Everything below is meaningless
	// when it is false.
	Supported bool
	// Dialect is the wire shape that reaches the dial on this server — the shape the tell that
	// was seen implies, never a guess from the model's family.
	Dialect EffortDialect
	// Efforts is the level set the server reported, and nil when the source states none: the
	// /props chat template proves a dial exists but names no vocabulary, so a caller that needs
	// a list falls back to the canonical levels itself.
	Efforts []string
	// Default is the level the server said it uses when a request names none, and "" when the
	// source states none.
	Default string
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
// Both payloads are read once more for the thinking-effort tell described on EffortSupport, and a
// dialect this Client was built with (WithEffortDialect) overrides whatever they said.
func (c *Client) Discover(ctx context.Context) (ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	info, err := c.discoverModels(ctx)
	if err != nil {
		return ModelInfo{}, err
	}

	runtime, slots, templateEffort := c.discoverProps(ctx)
	if runtime > 0 {
		info.setRuntimeContextWindow(runtime)
	}
	info.TotalSlots = slots
	// The /v1/models `reasoning` object wins when both tells appear: a server that advertises the
	// structured field is speaking the more specific dialect, and its own template may still
	// mention the kwarg it no longer reads.
	if !info.EffortSupport.Supported {
		info.EffortSupport = templateEffort
	}
	// And a dialect the server's own entry FORCED outranks both tells: it is there precisely
	// because this provider advertises none (see WithEffortDialect). It is applied to every
	// advertised entry as well as to the active model, so a host reading the dial of a model it is
	// about to switch to can never get a different answer than it gets for the one it is on — one
	// channel from either source (ADR 0060 decision 3).
	info.EffortSupport = forceEffortDialect(c.effortDialect, info.EffortSupport)
	for i := range info.AvailableModels {
		info.AvailableModels[i].EffortSupport =
			forceEffortDialect(c.effortDialect, info.AvailableModels[i].EffortSupport)
	}
	return info, nil
}

// forceEffortDialect applies a server entry's forced `effort-dialect:` over what the two payloads
// detected (ADR 0060 decision 3). A forced WIRE dialect is also a verdict — the dial exists, because
// the human who wrote the key knows this provider better than a payload that mentions nothing — so
// it declares support as well as the shape. The forced EffortDialectOff is the opposite verdict, and
// is the one place a detected dial is overruled downwards: no vocabulary, no default, nothing on the
// wire, for the server that errors on a kwarg it does not know.
//
// The detected VOCABULARY survives only when the forced dialect is the one that was detected — the
// levels a server reported describe the dial it advertised, and carrying them onto a different wire
// would offer the picker a set from a shape this server does not read. Forcing the dialect detection
// already found is therefore a no-op rather than a downgrade, which is what makes the key safe to
// leave in a file after a provider grows a tell.
//
// It is total: EffortDialectNone forces nothing (the `auto` an absent key means), and any other
// value is applied as the wire dialect it names — the config loader's enum is what refuses a word
// that names no dialect, one boundary further out.
func forceEffortDialect(forced EffortDialect, detected EffortSupport) EffortSupport {
	switch forced {
	case EffortDialectNone:
		return detected
	case EffortDialectOff:
		return EffortSupport{Dialect: EffortDialectOff}
	}
	support := EffortSupport{Supported: true, Dialect: forced}
	if detected.Supported && detected.Dialect == forced {
		support.Efforts, support.Default = detected.Efforts, detected.Default
	}
	return support
}

// EffortDialectFor maps a server entry's `effort-dialect:` value onto the dialect it forces. The
// three wire names are the dialect constants' own spellings, so they map to themselves; `off` is
// the forced-unsupported verdict; and both spellings of "detect for me" — the absent key and an
// explicit `auto` — map to the zero EffortDialectNone, which forces nothing.
//
// It is total, and deliberately silent about a word it does not know: the config loader's enum
// refuses that at startup, naming the entry and the key (config.ValidateServers), so a value that
// reached here unrecognised has already been through the one boundary that can explain it to a
// human. Answering with "detect" is then the safe reading — the behaviour the file would have had
// without the key at all.
func EffortDialectFor(name string) EffortDialect {
	switch EffortDialect(name) {
	case EffortDialectKwargs, EffortDialectReasoning, EffortDialectOpenAI, EffortDialectOff:
		return EffortDialect(name)
	default:
		return EffortDialectNone
	}
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

// discoverProps probes llama.cpp's GET /props for the three facts it reports about how the server
// was launched: the runtime context window (default_generation_settings.n_ctx — the per-slot
// context), the number of generation slots (total_slots — the `--parallel N` width) and whether the
// loaded chat template exposes a thinking-effort dial (chat_template). It is best-effort: a
// non-llama.cpp server returns a non-200 or omits any of them, and any failure (including a
// cancelled context) yields the zero value for all three, so the caller keeps the /v1/models window
// and treats the slot count and the dial as unknown. It shares the caller's discovery deadline.
//
// All three come out of ONE response because they are one observation: asking again would cost a
// second round trip and could straddle a restart that moved every number at once.
func (c *Client) discoverProps(ctx context.Context) (window, slots int, effort EffortSupport) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+propsPath, nil)
	if err != nil {
		return 0, 0, EffortSupport{}
	}
	c.setAuth(req.Header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, EffortSupport{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, EffortSupport{}
	}

	var decoded propsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, 0, EffortSupport{}
	}
	if decoded.DefaultGenerationSettings.NCtx > 0 {
		window = decoded.DefaultGenerationSettings.NCtx
	}
	if decoded.TotalSlots > 0 {
		slots = decoded.TotalSlots
	}
	return window, slots, effortFromTemplate(decoded.ChatTemplate)
}

// The two Jinja names a llama.cpp chat template reads when it exposes a thinking-effort dial. The
// server forwards `chat_template_kwargs` verbatim into the template, so a template that mentions
// either name is a template that acts on the kwargs dialect — and a template with no dial mentions
// neither.
const (
	templateEffortName   = "reasoning_effort"
	templateThinkingName = "enable_thinking"
)

// effortFromTemplate reads the /props chat template for the llama.cpp tell. A hit proves only that
// a dial EXISTS: the template text states no vocabulary and no default, so the level set and the
// default stay empty and a caller falls back to the canonical levels. A miss — including an absent
// or unparsable template — is the zero value, i.e. unsupported.
func effortFromTemplate(template string) EffortSupport {
	if !strings.Contains(template, templateEffortName) && !strings.Contains(template, templateThinkingName) {
		return EffortSupport{}
	}
	return EffortSupport{Supported: true, Dialect: EffortDialectKwargs}
}

// propsResponse is the subset of llama.cpp's GET /props payload we read: the runtime context
// window the server was launched with, reported per generation slot, how many of those slots
// there are (the `--parallel N` width — ADR 0039's discovery source for the Parallel agents cap),
// and the Jinja chat template the server loaded (the thinking-effort tell — see EffortSupport).
type propsResponse struct {
	DefaultGenerationSettings struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	TotalSlots   int    `json:"total_slots"`
	ChatTemplate string `json:"chat_template"`
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
		Reasoning json.RawMessage `json:"reasoning"`
	} `json:"data"`
}

// modelReasoning is the per-model `reasoning` object an OpenRouter-shaped /v1/models entry carries.
// Its mere PRESENCE is the tell (see EffortSupport): a server that describes the dial at all
// supports it, even when it names neither a vocabulary nor a default — so the entry holds the field
// raw and decodeReasoning distinguishes an absent object from an empty one.
type modelReasoning struct {
	SupportedEfforts []string `json:"supported_efforts"`
	DefaultEffort    string   `json:"default_effort"`
}

// jsonNullLiteral is the encoding of an explicit JSON null, which a raw field carries as bytes
// rather than as an absent value.
const jsonNullLiteral = "null"

// toModelInfo projects the payload onto ModelInfo, dropping id-less entries and resolving
// the active model from hint (the configured model) per resolveHint. Every advertised entry is
// read for its own thinking-effort tell per effortSupport, and the resolved active model once
// more — the same rule twice, so an entry and the active model can never disagree about the
// model they both describe.
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
			EffortSupport: r.effortSupport(m.ID),
		})
	}

	info := ModelInfo{AvailableModels: models}
	info.ActiveModel, info.ContextWindow, info.Resolution = resolveHint(models, hint)
	info.EffortSupport = r.effortSupport(info.ActiveModel)
	return info
}

// effortSupport reports what the advertised entry describing the active model says about its
// thinking-effort dial. It resolves that entry the way resolveHint sources the context window — the
// exact id, else the base slug before the first ':' of a routing variant — so a variant that the
// server does not list separately inherits the base model's answer instead of reading as a model
// with no dial. No entry, or an entry with no `reasoning` object, is the zero value: unsupported.
func (r modelsResponse) effortSupport(active string) EffortSupport {
	reasoning := r.reasoningFor(active)
	if reasoning == nil {
		if base, _, hasVariant := strings.Cut(active, ":"); hasVariant {
			reasoning = r.reasoningFor(base)
		}
	}
	if reasoning == nil {
		return EffortSupport{}
	}
	return EffortSupport{
		Supported: true,
		Dialect:   EffortDialectReasoning,
		Efforts:   reasoning.SupportedEfforts,
		Default:   reasoning.DefaultEffort,
	}
}

// reasoningFor returns the `reasoning` object of the entry with exactly this id, and nil when no
// entry has it or the entry carries none. An empty id matches nothing: an id-less entry is dropped
// from the advertised list, so it must not answer for an unresolved active model either.
func (r modelsResponse) reasoningFor(id string) *modelReasoning {
	if id == "" {
		return nil
	}
	for _, m := range r.Data {
		if m.ID == id {
			return decodeReasoning(m.Reasoning)
		}
	}
	return nil
}

// decodeReasoning decodes one entry's raw `reasoning` value into the object it should be. An
// absent, null or malformed value yields nil: the field is a passive tell, so a server that writes
// something unexpected there reads as "no tell" and never fails the discovery that surrounds it.
func decodeReasoning(raw json.RawMessage) *modelReasoning {
	if len(raw) == 0 || string(raw) == jsonNullLiteral {
		return nil
	}
	var reasoning modelReasoning
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		return nil
	}
	return &reasoning
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
