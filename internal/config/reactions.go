package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
)

// defaultReactionTimeout bounds an entry that spells no `timeout:`. Every user-origin Reaction is
// bounded, because an unbounded one could hold a root's shutdown grace open on a command that never
// returns; thirty seconds is long enough for a notifier or a webhook round trip and short enough
// that a wedged one is noticed rather than waited on (ADR 0073, ratified call B).
const defaultReactionTimeout = 30 * time.Second

// reactionConfig is the on-disk schema for one entry of the global `reactions:` list — the
// user-origin half of the Reaction core (ADR 0076). It mirrors a [domain.Reaction] with yaml tags
// and the spellings only a FILE has: a Moment list written as plain strings under `on:`, a timeout
// written as a duration like `30s`, an `enabled:` switch that parks an entry without deleting it,
// and ONE action key per class — `run:` for observe, with `advise:` and `gate:` reserved for the
// classes stage 3 ships. The mapping across to the core's value type is toReaction's, so the
// on-disk shape and the value the Runner fires stay independently evolvable (mcpServerConfig's
// rule).
//
// The three action keys are typed `any` and not yaml.Node ON PURPOSE. `run:` carries two shapes —
// a sequence for a command, a mapping for a webhook — so the field has to hold either; a yaml.Node
// would hold the line and column it was read at too, and [sameApartFrom] compares two parses of the
// same file with reflect.DeepEqual, so a single spliced key anywhere above a `reactions:` block
// would move every node in it and make every other write look like a change. A plain `any` decodes
// to []any or map[string]any, which compares by value, and presence is simply non-nil.
type reactionConfig struct {
	ID        string   `yaml:"id"`
	On        []string `yaml:"on"`
	Run       any      `yaml:"run"`
	Advise    any      `yaml:"advise"`
	Gate      any      `yaml:"gate"`
	Workspace string   `yaml:"workspace"`
	Timeout   string   `yaml:"timeout"`
	Enabled   *bool    `yaml:"enabled"`
}

// floorGuardKeys is the seven Floor guards' config keys, which are also the ids their builtin
// Reactions fire under (internal/agent's floorguards.go). An entry that takes one of them as its
// `id:` is refused: the two would report as one reaction, and the user almost certainly meant the
// top-level boolean that switches the guard off. The list is a literal rather than a filter over
// [KeyRegistry] because "is a Floor guard" is not something a row records; a test pins every name
// here as a real registry key so the two cannot drift.
var floorGuardKeys = []string{
	"tool-use-enforcer",
	"empty-response-recovery",
	"tool-call-repair",
	"tool-call-salvage",
	"tool-loop-breaker",
	"tool-result-cap",
	"read-cache",
}

// toReaction maps one on-disk entry onto the user-origin observe Reaction the Runner fires: the id
// checked against the names it may not take, the reserved action keys refused, every `on:` entry
// read as a Moment, `run:` turned into the handler that runs it, an absent `timeout:` defaulted, and
// `workspace:` reduced to the one spelling both sides of the filter are compared as (ADR 0073
// ratified call C — `~` expanded, absolute, symlinks evaluated). It reports the first thing it
// cannot map, naming the entry, so a user with several Reactions is told which line to fix.
//
// A Moment outside the notice vocabulary is NOT refused here when it is a seam: the mapped value
// carries it and the core's own refusal is the sentence the user reads (ADR 0076 A7 — a seam takes
// `advise:`/`gate:`, which stage 2 does not ship), so the two halves of "reactions react to
// notices" are never worded twice.
func (r reactionConfig) toReaction() (domain.Reaction, error) {
	id := strings.TrimSpace(r.ID)
	if id == "" {
		return domain.Reaction{}, fmt.Errorf(
			"reactions: an entry has no id: every reaction needs an `id:` to be reported by")
	}
	if slices.Contains(floorGuardKeys, id) {
		return domain.Reaction{}, reactionEntryError(id,
			"that is the Floor guard %s: — set the top-level key, not a reactions: entry", id)
	}
	if err := r.refuseReservedActions(id); err != nil {
		return domain.Reaction{}, err
	}

	moments, err := r.moments(id)
	if err != nil {
		return domain.Reaction{}, err
	}
	handler, err := r.handler(id)
	if err != nil {
		return domain.Reaction{}, err
	}

	timeout := defaultReactionTimeout
	if spelled := strings.TrimSpace(r.Timeout); spelled != "" {
		parsed, err := time.ParseDuration(spelled)
		if err != nil {
			return domain.Reaction{}, reactionEntryError(id,
				"timeout: %q is not a duration — write it as `30s` or `2m`", spelled)
		}
		timeout = parsed
	}

	workspace, err := r.resolvedWorkspace(id)
	if err != nil {
		return domain.Reaction{}, err
	}

	mapped := domain.Reaction{
		ID:        id,
		Origin:    domain.OriginUser,
		Class:     domain.ClassObserve,
		On:        moments,
		Handler:   handler,
		Workspace: workspace,
		Timeout:   timeout,
	}
	// The core's structural refusal, run here so a seam under `on:` earns the sentence that says
	// reactions react to notices rather than the vocabulary listing a misspelling earns.
	if err := mapped.Validate(); err != nil {
		return domain.Reaction{}, err
	}
	return mapped, nil
}

// refuseReservedActions refuses an entry that spells one of the two action keys stage 3 ships. They
// are in the schema now so a file written against them fails with a sentence naming the stage
// rather than being silently ignored as an unknown key.
func (r reactionConfig) refuseReservedActions(id string) error {
	for _, reserved := range []struct {
		key   string
		value any
	}{{"advise", r.Advise}, {"gate", r.Gate}} {
		if reserved.value != nil {
			return reactionEntryError(id, "%s: is not yet shipped (ADR 0076 stage 3)", reserved.key)
		}
	}
	return nil
}

// moments reads the `on:` list. A spelling in EITHER half of the Moment vocabulary is carried
// across — a seam so the core can say why it is not one a `run:` entry may take — and anything
// outside both earns the vocabulary listing, which is the likeliest mistake in a `reactions:` block
// and the one a bare "invalid" would leave the user guessing at.
func (r reactionConfig) moments(id string) ([]domain.Moment, error) {
	var moments []domain.Moment
	for _, name := range r.On {
		moment := domain.Moment(strings.TrimSpace(name))
		if moment.IsNotice() || moment.IsSeam() {
			moments = append(moments, moment)
			continue
		}
		if _, err := reactions.ParseEvent(string(moment)); err != nil {
			return nil, reactionEntryError(id, "%v", err)
		}
	}
	return moments, nil
}

// handler turns `run:` into the handler that runs it: a SEQUENCE of strings is the argv of a
// command run out of process, and a MAPPING is a webhook POST. Anything else — a bare string, a
// number, an absent key — is refused with the one sentence that spells both shapes, since an entry
// that spelled neither has been written against a schema apogee does not have.
//
// `headers-env:` maps a header name to the NAME of an environment variable holding its value, on
// the `api-key-env` precedent, so a token never sits in the config file. No `${VAR}` interpolation
// exists anywhere in this schema and none is introduced here.
func (r reactionConfig) handler(id string) (domain.Handler, error) {
	switch run := r.Run.(type) {
	case []any:
		argv := make([]string, 0, len(run))
		for _, element := range run {
			text, ok := element.(string)
			if !ok {
				return nil, runShapeError(id)
			}
			argv = append(argv, text)
		}
		return domain.ArgvHandler{Argv: argv}, nil
	case map[string]any:
		return webhookFromRun(id, run)
	default:
		return nil, runShapeError(id)
	}
}

// webhookFromRun reads the webhook mapping's three keys. An unknown key is refused with the same
// sentence a wrong shape earns — it lists exactly the keys the mapping takes — rather than being
// dropped, so a misspelt `header-env:` is a startup refusal and not a token that never gets sent.
func webhookFromRun(id string, run map[string]any) (domain.Handler, error) {
	handler := domain.WebhookHandler{}
	for key, value := range run {
		switch key {
		case "url":
			text, ok := value.(string)
			if !ok {
				return nil, runShapeError(id)
			}
			handler.URL = text
		case "headers":
			headers, err := stringMap(id, value)
			if err != nil {
				return nil, err
			}
			handler.Headers = headers
		case "headers-env":
			headers, err := stringMap(id, value)
			if err != nil {
				return nil, err
			}
			handler.HeadersEnv = headers
		default:
			return nil, runShapeError(id)
		}
	}
	return handler, nil
}

// stringMap reads one of the webhook's two header mappings, refusing a value that is not text.
func stringMap(id string, value any) (map[string]string, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, runShapeError(id)
	}
	mapped := make(map[string]string, len(raw))
	for name, text := range raw {
		spelled, ok := text.(string)
		if !ok {
			return nil, runShapeError(id)
		}
		mapped[name] = spelled
	}
	return mapped, nil
}

// runShapeError is the one sentence every wrong `run:` earns, spelled once so the two shapes a
// user may write are always listed together.
func runShapeError(id string) error {
	return reactionEntryError(id,
		"run: is an argv list or a webhook mapping {url:, headers:, headers-env:}")
}

// resolvedWorkspace reduces this entry's `workspace:` filter to its comparable spelling. An empty
// filter stays empty — the unset filter, active at every root — and a leading `~` goes through this
// package's own expansion first so the key reads like every other path key the schema carries.
func (r reactionConfig) resolvedWorkspace(id string) (string, error) {
	if strings.TrimSpace(r.Workspace) == "" {
		return "", nil
	}
	expanded, err := ExpandUserPath(r.Workspace)
	if err != nil {
		return "", reactionEntryError(id, "workspace: %v", err)
	}
	resolved, err := reactions.ResolveWorkspace(expanded)
	if err != nil {
		return "", reactionEntryError(id, "%v", err)
	}
	return resolved, nil
}

// toReactions maps the whole `reactions:` list, stopping at the first entry it cannot map. An entry
// carrying `enabled: false` is PARKED — dropped here rather than carried as a reaction nothing
// fires — so a user can keep a definition in the file without arming it. A list that maps to
// nothing resolves to nil rather than an empty slice, so an absent block, an explicitly empty one
// and a wholly parked one resolve alike.
func toReactions(list []reactionConfig) ([]domain.Reaction, error) {
	if len(list) == 0 {
		return nil, nil
	}
	mapped := make([]domain.Reaction, 0, len(list))
	for _, entry := range list {
		if entry.Enabled != nil && !*entry.Enabled {
			continue
		}
		reaction, err := entry.toReaction()
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, reaction)
	}
	if len(mapped) == 0 {
		return nil, nil
	}
	return mapped, nil
}

// validateReactionBlocks refuses a user-origin block that cannot be run, at PARSE time — beside
// validateModelProfiles — so a mistyped Moment or an entry with no action is a startup refusal
// naming the entry rather than a Reaction that silently never fires. It is the mapping (which owns
// the on-disk shape rules) plus the two checks the reactions package owns: [reactions.Validate] per
// entry, and [reactions.ValidateAll] for the uniqueness of the ids every failure notice and payload
// keys on.
func validateReactionBlocks(list []reactionConfig) error {
	mapped, err := toReactions(list)
	if err != nil {
		return err
	}
	return reactions.ValidateAll(mapped)
}

// projectReactions writes the resolved observe list onto the Options.
//
// A mapping FAILURE leaves the list empty rather than half-applied: parseConfigFile has already
// refused any file this could fail on (validateReactionBlocks), so the only way to reach it is a
// fileConfig built in code, and a partially fired reaction set is a worse answer there than none.
func projectReactions(o *Options, fc fileConfig) {
	o.Reactions = nil
	if mapped, err := toReactions(fc.Reactions); err == nil {
		o.Reactions = mapped
	}
}

// ReactionEnvNames is every environment variable name the resolved Reactions read a webhook header
// out of, sorted and deduplicated. A root appends it to the names APIKeyEnvNames already
// contributes to [domain.Config.SecretEnvVars], so the `terminal` tool cannot read a webhook token
// back out of the environment it inherits. Sorted because the names come off a map, and a set that
// reordered between runs would make every caller's own output unstable.
func ReactionEnvNames(o Options) []string {
	var names []string
	seen := make(map[string]bool)
	for _, reaction := range o.Reactions {
		handler, ok := reaction.Handler.(domain.WebhookHandler)
		if !ok {
			continue
		}
		for _, envName := range handler.HeadersEnv {
			name := strings.TrimSpace(envName)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// reactionEntryError prefixes a message with the entry it is about, in the wording the reactions
// package uses for its own refusals, so a user reading a startup error cannot tell which side found
// the fault and does not need to.
func reactionEntryError(id string, format string, args ...any) error {
	return fmt.Errorf("reaction %q: %s", id, fmt.Sprintf(format, args...))
}
