package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
)

// defaultReactionTimeout bounds an OBSERVE entry that spells no `timeout:`. Every user-origin
// Reaction is bounded, because an unbounded one could hold a root's shutdown grace open on a
// command that never returns; thirty seconds is long enough for a notifier or a webhook round trip
// and short enough that a wedged one is noticed rather than waited on (ADR 0073, ratified call B).
// The sync classes are shorter — the loop, and behind it a person, waits on those — and carry their
// own defaults beside the core that runs them ([domain.DefaultGateTimeout]).
const defaultReactionTimeout = 30 * time.Second

// reactionConfig is the on-disk schema for one entry of the global `reactions:` list — the
// user-origin half of the Reaction core (ADR 0076). It mirrors a [domain.Reaction] with yaml tags
// and the spellings only a FILE has: a Moment list written as plain strings under `on:`, a timeout
// written as a duration like `30s`, an `enabled:` switch that parks an entry without deleting it,
// and ONE action key per class — `run:` for observe and `gate:` for gate, with `advise:` still
// reserved. An entry may spell more than one of them and resolves to one Reaction per key, all
// sharing its id. The mapping across to the core's value type is entryReactions', so the on-disk
// shape and the values the engine fires stay independently evolvable (mcpServerConfig's rule).
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

// entryReactions maps one on-disk entry onto the user-origin Reactions it arms — ONE per action key
// it spells, all carrying the entry's id, `on:` list and `workspace:` filter, so `run:` and `gate:`
// on one entry resolve to an observe Reaction and a gate Reaction that [domain.SplitLanes] later
// sends down their own lanes. The id is checked against the names it may not take, the action key
// stage 3 has not shipped is refused, every `on:` entry is read as a Moment, each action key is
// turned into the handler that runs it, an absent `timeout:` takes the class default, and
// `workspace:` is reduced to the one spelling both sides of the filter are compared as (ADR 0073
// ratified call C — `~` expanded, absolute, symlinks evaluated). It reports the first thing it
// cannot map, naming the entry, so a user with several Reactions is told which line to fix.
//
// The `on:` list reaches every Reaction the entry arms WHOLE, and each is validated on its own, so
// a Moment the entry's own class cannot take is refused by the core's sentence for that class
// (ADR 0076 A7) rather than being silently dropped from one of the two. That is also why a Moment
// outside the notice vocabulary is not refused by [reactionConfig.moments]: which spellings are
// legal depends on the class, which only the core knows.
func (r reactionConfig) entryReactions() ([]domain.Reaction, error) {
	id := strings.TrimSpace(r.ID)
	if id == "" {
		return nil, fmt.Errorf(
			"reactions: an entry has no id: every reaction needs an `id:` to be reported by")
	}
	if slices.Contains(floorGuardKeys, id) {
		return nil, reactionEntryError(id,
			"that is the Floor guard %s: — set the top-level key, not a reactions: entry", id)
	}
	if err := r.refuseReservedActions(id); err != nil {
		return nil, err
	}

	moments, err := r.moments(id)
	if err != nil {
		return nil, err
	}
	workspace, err := r.resolvedWorkspace(id)
	if err != nil {
		return nil, err
	}

	var mapped []domain.Reaction
	if r.Run != nil {
		handler, err := r.handler(id)
		if err != nil {
			return nil, err
		}
		timeout, err := r.classTimeout(id, defaultReactionTimeout)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, domain.Reaction{
			ID:        id,
			Origin:    domain.OriginUser,
			Class:     domain.ClassObserve,
			On:        slices.Clone(moments),
			Handler:   handler,
			Workspace: workspace,
			Timeout:   timeout,
		})
	}
	if r.Gate != nil {
		argv, ok := argvList(r.Gate)
		if !ok {
			return nil, reactionEntryError(id, "gate: is an argv list")
		}
		timeout, err := r.classTimeout(id, domain.DefaultGateTimeout)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, domain.Reaction{
			ID:        id,
			Origin:    domain.OriginUser,
			Class:     domain.ClassGate,
			On:        slices.Clone(moments),
			Handler:   domain.ArgvHandler{Argv: argv},
			Workspace: workspace,
			Timeout:   timeout,
		})
	}
	// An entry that spells no action key at all has been written against a schema apogee does not
	// have, and `run:` is the key it most likely meant, so it earns that key's own sentence.
	if len(mapped) == 0 {
		return nil, runShapeError(id)
	}

	// The core's structural refusal, run here so a seam under `on:` earns the sentence that says
	// which Moments the key it was written under reacts at, rather than the vocabulary listing a
	// misspelling earns.
	for _, reaction := range mapped {
		if err := reaction.Validate(); err != nil {
			return nil, err
		}
	}
	return mapped, nil
}

// refuseReservedActions refuses an entry that spells the one action key stage 3 has not shipped. It
// is in the schema now so a file written against it fails with a sentence naming the stage rather
// than being silently ignored as an unknown key.
func (r reactionConfig) refuseReservedActions(id string) error {
	if r.Advise != nil {
		return reactionEntryError(id, "advise: is not yet shipped (ADR 0076 stage 3)")
	}
	return nil
}

// classTimeout resolves the deadline one of the entry's Reactions runs under: the entry's own
// `timeout:` when it spells one — which binds EVERY Reaction the entry arms (ADR 0076 D7) — and the
// class default otherwise. A spelled `0s` is carried through as it is written so the runnable check
// that refuses a non-positive deadline still sees it.
func (r reactionConfig) classTimeout(id string, fallback time.Duration) (time.Duration, error) {
	spelled := strings.TrimSpace(r.Timeout)
	if spelled == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(spelled)
	if err != nil {
		return 0, reactionEntryError(id,
			"timeout: %q is not a duration — write it as `30s` or `2m`", spelled)
	}
	return parsed, nil
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
// number — is refused with the one sentence that spells both shapes, since an entry that spelled
// neither has been written against a schema apogee does not have.
//
// `headers-env:` maps a header name to the NAME of an environment variable holding its value, on
// the `api-key-env` precedent, so a token never sits in the config file. No `${VAR}` interpolation
// exists anywhere in this schema and none is introduced here.
func (r reactionConfig) handler(id string) (domain.Handler, error) {
	if run, ok := r.Run.(map[string]any); ok {
		return webhookFromRun(id, run)
	}
	argv, ok := argvList(r.Run)
	if !ok {
		return nil, runShapeError(id)
	}
	return domain.ArgvHandler{Argv: argv}, nil
}

// argvList reads a decoded YAML value as an argv: a sequence whose every element is text. It
// reports whether the value is one, so each action key can name ITSELF in the sentence a wrong
// shape earns — `run:` spells two shapes and `gate:` only this one.
func argvList(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	argv := make([]string, 0, len(list))
	for _, element := range list {
		text, ok := element.(string)
		if !ok {
			return nil, false
		}
		argv = append(argv, text)
	}
	return argv, true
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

// toReactions maps the whole `reactions:` list, stopping at the first entry it cannot map. One
// entry contributes one Reaction per action key it spells, so the result is longer than the list
// whenever an entry arms more than one class. An entry carrying `enabled: false` is PARKED —
// dropped here rather than carried as reactions nothing fires — so a user can keep a definition in
// the file without arming it. A list that maps to nothing resolves to nil rather than an empty
// slice, so an absent block, an explicitly empty one and a wholly parked one resolve alike.
func toReactions(list []reactionConfig) ([]domain.Reaction, error) {
	if len(list) == 0 {
		return nil, nil
	}
	mapped := make([]domain.Reaction, 0, len(list))
	for _, entry := range list {
		if entry.Enabled != nil && !*entry.Enabled {
			continue
		}
		armed, err := entry.entryReactions()
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, armed...)
	}
	if len(mapped) == 0 {
		return nil, nil
	}
	return mapped, nil
}

// validateReactionBlocks refuses a user-origin block that cannot be run, at PARSE time — beside
// validateModelProfiles — so a mistyped Moment or an entry with no action is a startup refusal
// naming the entry rather than a Reaction that silently never fires. It is the mapping (which owns
// the on-disk shape rules) plus the checks each LANE's owner holds, which is why the resolved list
// is split before either runs: [reactions.ValidateAll] answers for the async observe lane — its
// runnable checks read every Moment as a notice event, which a sync Reaction's never is — and
// [domain.Generation.Validate] for the sync lane, which is the only side that knows a sync entry
// must be user origin, of class advise or gate, and uniquely named within its own lane. The same id
// in both lanes is one entry that armed two classes and is accepted by design.
func validateReactionBlocks(list []reactionConfig) error {
	mapped, err := toReactions(list)
	if err != nil {
		return err
	}
	observe, sync := domain.SplitLanes(mapped)
	if err := reactions.ValidateAll(observe); err != nil {
		return err
	}
	return domain.Generation{Sync: sync}.Validate()
}

// projectReactions writes every Reaction the block resolved to onto the Options — both lanes in one
// list, which each Driver splits for the two halves that fire them.
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
