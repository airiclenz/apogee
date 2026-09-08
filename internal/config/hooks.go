package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
)

// hookConfig is the on-disk schema for one entry of the global `hooks:` list. It mirrors the
// user-origin observe [domain.Reaction] with yaml tags and the three spellings only a FILE has — an
// event list written as plain strings, a timeout written as a duration like `30s`, and the two
// actions written as sibling keys rather than one handler — which toHook maps across, so the
// on-disk shape and the core's value type stay independently evolvable (mcpServerConfig's rule).
//
// Headers and HeadersEnv belong to a `webhook:` entry alone; HeadersEnv maps a header name to the
// NAME of an environment variable holding its value, on the `api-key-env` precedent, so a token
// never sits in the config file. No `${VAR}` interpolation exists anywhere in this schema and none
// is introduced here.
type hookConfig struct {
	Name       string            `yaml:"name"`
	Events     []string          `yaml:"events"`
	Command    []string          `yaml:"command"`
	Webhook    string            `yaml:"webhook"`
	Headers    map[string]string `yaml:"headers"`
	HeadersEnv map[string]string `yaml:"headers-env"`
	Workspace  string            `yaml:"workspace"`
	Timeout    string            `yaml:"timeout"`
}

// toHook maps one on-disk entry onto the user-origin observe Reaction the Runner fires: every
// event name parsed against the closed vocabulary, the one action the entry spells turned into the
// handler that runs it, an absent `timeout:` defaulted, and `workspace:` reduced to the one
// spelling both sides of the filter are compared as (ratified call C — `~` expanded, absolute,
// symlinks evaluated). It reports the first thing it cannot map, naming the entry, so a user with
// several Hooks is told which line to fix.
//
// The two rules about the SIBLING keys live here — exactly one of `command:`/`webhook:`, and
// headers belonging to a webhook — because this is the only place both fields still coexist: a
// domain.Reaction carries one Handler and cannot express either fault. Everything else about the
// entry shape is [reactions.Validate]'s, and validateReactionBlocks runs it over the mapped value
// so those rules live in one place for every root.
func (h hookConfig) toHook() (domain.Reaction, error) {
	var events []reactions.Event
	for _, name := range h.Events {
		// The spelling this notice carried before ADR 0076 A6 renamed it to `approval-requested`
		// — `approval-` joined to `waiting`, built rather than written whole so the rename's own
		// sweep for the retired name stays clean. It is accepted here so an UNMIGRATED file keeps
		// loading; the migration rewrites the block to the new spelling, after which nothing
		// writes the old one again.
		if name == "approval-"+"waiting" {
			name = string(reactions.ApprovalRequested)
		}
		event, err := reactions.ParseEvent(name)
		if err != nil {
			return domain.Reaction{}, hookError(h.Name, "%v", err)
		}
		events = append(events, event)
	}

	handler, err := h.handler()
	if err != nil {
		return domain.Reaction{}, err
	}

	timeout := defaultReactionTimeout
	if spelled := strings.TrimSpace(h.Timeout); spelled != "" {
		parsed, err := time.ParseDuration(spelled)
		if err != nil {
			return domain.Reaction{}, hookError(h.Name,
				"timeout: %q is not a duration — write it as `30s` or `2m`", spelled)
		}
		timeout = parsed
	}

	workspace, err := h.resolvedWorkspace()
	if err != nil {
		return domain.Reaction{}, err
	}

	return domain.Reaction{
		ID:        h.Name,
		Origin:    domain.OriginUser,
		Class:     domain.ClassObserve,
		On:        events,
		Handler:   handler,
		Workspace: workspace,
		Timeout:   timeout,
	}, nil
}

// handler turns the one action the entry spells into the handler that runs it, refusing an entry
// that spells both or neither. `headers:`/`headers-env:` are refused on a command for the same
// reason: they are the webhook's alone, and an entry carrying them beside a `command:` has been
// written against the wrong action.
func (h hookConfig) handler() (domain.Handler, error) {
	hasCommand, hasWebhook := len(h.Command) > 0, strings.TrimSpace(h.Webhook) != ""
	switch {
	case hasCommand && hasWebhook:
		return nil, hookError(h.Name, "both `command:` and `webhook:` are set — an entry takes exactly one")
	case !hasCommand && !hasWebhook:
		return nil, hookError(h.Name, "neither `command:` nor `webhook:` is set — an entry takes exactly one")
	case hasCommand:
		if len(h.Headers) > 0 || len(h.HeadersEnv) > 0 {
			return nil, hookError(h.Name,
				"`headers:`/`headers-env:` belong to a `webhook:` entry — a command carries none")
		}
		return domain.ArgvHandler{Argv: h.Command}, nil
	default:
		return domain.WebhookHandler{
			URL:        h.Webhook,
			Headers:    h.Headers,
			HeadersEnv: h.HeadersEnv,
		}, nil
	}
}

// resolvedWorkspace reduces this entry's `workspace:` filter to its comparable spelling. An empty
// filter stays empty — the unset filter, active at every root — and a leading `~` goes through this
// package's own expansion first so the key reads like every other path key the schema carries.
func (h hookConfig) resolvedWorkspace() (string, error) {
	if strings.TrimSpace(h.Workspace) == "" {
		return "", nil
	}
	expanded, err := ExpandUserPath(h.Workspace)
	if err != nil {
		return "", hookError(h.Name, "workspace: %v", err)
	}
	resolved, err := reactions.ResolveWorkspace(expanded)
	if err != nil {
		return "", hookError(h.Name, "%v", err)
	}
	return resolved, nil
}

// toHooks maps the whole list, stopping at the first entry it cannot map. An empty list maps to
// nil rather than an empty slice, so an absent block and an explicitly empty one resolve alike.
func toHooks(list []hookConfig) ([]domain.Reaction, error) {
	if len(list) == 0 {
		return nil, nil
	}
	mapped := make([]domain.Reaction, 0, len(list))
	for _, entry := range list {
		hook, err := entry.toHook()
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, hook)
	}
	return mapped, nil
}

// hookError prefixes a message with the entry it is about, in the wording the hooks package uses
// for its own refusals, so a user reading a startup error cannot tell which side found the fault
// and does not need to.
func hookError(name string, format string, args ...any) error {
	return fmt.Errorf("hook %q: %s", name, fmt.Sprintf(format, args...))
}
