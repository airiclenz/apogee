package hooks

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// Event names one of the moments a Hook may fire on — the five NOTICE Moments of the Reaction
// core, which this name is an alias for (ADR 0076): the Hook vocabulary and the notice half of
// the Moment vocabulary are one set, not two that happen to agree. Every one is POST-HOC: it
// reports something that already happened, so a Hook reading it can change nothing about it. The
// set is additive by design — a moment not named here is not a Hook event yet — and deliberately
// excludes the per-token, tool-call, sub-agent-phase, session-save, prune and usage moments
// (ADR 0073 §4).
//
// The string is the spelling a user writes in the `events:` list of a `hooks:` entry, so it is
// also the value that reaches a fired command as APOGEE_HOOK_EVENT and the payload's "event"
// field. It is a stable contract: renaming one breaks every configuration in the wild.
type Event = domain.Moment

const (
	// ExchangeFinished fires when a Depth-0 Turn closed its Exchange — the model produced a final
	// no-tool response, or the loop abandoned or capped the Exchange. The payload's Faulted and
	// StepCapped say which, since neither is derivable from the status alone.
	ExchangeFinished = domain.MomentExchangeFinished
	// TurnFinished fires at every Depth-0 Turn boundary, whatever the Turn's status. A Turn that
	// closed its Exchange produces BOTH this and ExchangeFinished — the boundary is one fact and
	// the closure another, and a Hook may want either without the other.
	TurnFinished = domain.MomentTurnFinished
	// FileChanged fires when a workspace-scoped write tool SUCCEEDED, at any depth. The payload
	// carries the tool and the absolute path the write landed on; a delete, copy or move reports
	// its destination, because that is the path whose content changed.
	FileChanged = domain.MomentFileChanged
	// ApprovalWaiting fires when an Approval was RAISED — before its decision, while the human is
	// still being waited on — so a Hook can ring a bell for a prompt nobody is watching. The
	// verdict is deliberately not an event: a Hook that learned the answer could not act on it.
	ApprovalWaiting = domain.MomentApprovalWaiting
	// Error fires on a localised, recovered engine fault at any depth. It is a Hook event and not
	// an error value; the failures of the Hook machinery ITSELF never reach it, because a failing
	// Hook that fired an `error` Hook would loop (ADR 0073 §8).
	Error = domain.MomentError
)

// allEvents is the vocabulary in the order Events reports it and a config template lists it.
var allEvents = []Event{ExchangeFinished, TurnFinished, FileChanged, ApprovalWaiting, Error}

// Events returns the five Hook events in their documented order. The slice is a fresh copy, so a
// caller listing them for a help text or a validation message cannot disturb the vocabulary.
func Events() []Event {
	return append([]Event(nil), allEvents...)
}

// ParseEvent turns one `events:` entry into an Event, refusing anything outside the vocabulary
// with a message that lists what is allowed — a misspelt event name is the likeliest mistake in
// a `hooks:` block, and a bare "invalid" would leave the user guessing at the spelling.
func ParseEvent(name string) (Event, error) {
	for _, e := range allEvents {
		if string(e) == name {
			return e, nil
		}
	}
	return "", fmt.Errorf("unknown hook event %q — the events are %s", name, eventList())
}

// eventList renders the vocabulary for an error message.
func eventList() string {
	names := make([]string, 0, len(allEvents))
	for _, e := range allEvents {
		names = append(names, string(e))
	}
	return strings.Join(names, ", ")
}

// Hook is one entry of the global `hooks:` list: the events it fires on, the one action it takes,
// and the optional workspace it is scoped to.
//
// Exactly one of Command and Webhook is set. Command is an argv list run DIRECTLY — no shell, no
// interpolation, no word splitting — so a user who wants a shell writes ["sh", "-c", "…"];
// Webhook is an absolute http(s) URL the same JSON payload is POSTed to. Headers and HeadersEnv
// belong to a webhook alone: HeadersEnv maps a header name to the NAME of an environment variable
// holding its value, on the `api-key-env` precedent, so a token never sits in the config file.
//
// Workspace, when set, scopes the Hook to one workspace: the entry is inactive at any root whose
// resolved workspace differs from this one, compared through ResolveWorkspace on both sides so a
// symlinked path cannot read as two different workspaces. An empty Workspace is the unset filter
// and the Hook is active everywhere.
//
// Timeout bounds the command run and the webhook POST alike. The zero value is not a valid
// configured entry — Validate refuses it — because a Hook with no bound could hold a shutdown
// grace open; the config layer defaults an absent `timeout:` before validating.
type Hook struct {
	Name       string
	Events     []Event
	Command    []string
	Webhook    string
	Headers    map[string]string
	HeadersEnv map[string]string
	Workspace  string
	Timeout    time.Duration
}

// Validate reports whether this entry is a usable Hook, naming the entry in every message so a
// user with several Hooks is told WHICH line to fix. It is the whole entry shape in one place:
// a name, at least one known event, exactly one action, and — for a webhook — an absolute
// http(s) URL and headers that belong to it.
func (h Hook) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("hooks: an entry has no name: every hook needs a `name:` to be reported by")
	}
	if len(h.Events) == 0 {
		return h.errorf("no events: list at least one of %s under `events:`", eventList())
	}
	for _, e := range h.Events {
		if _, err := ParseEvent(string(e)); err != nil {
			return h.errorf("%v", err)
		}
	}
	if err := h.validateAction(); err != nil {
		return err
	}
	if h.Timeout <= 0 {
		return h.errorf("timeout: %v is not a positive duration — write it as `30s` or `2m`", h.Timeout)
	}
	return nil
}

// validateAction enforces the exactly-one rule and the shape of whichever action was chosen.
func (h Hook) validateAction() error {
	hasCommand, hasWebhook := len(h.Command) > 0, strings.TrimSpace(h.Webhook) != ""
	switch {
	case hasCommand && hasWebhook:
		return h.errorf("both `command:` and `webhook:` are set — an entry takes exactly one")
	case !hasCommand && !hasWebhook:
		return h.errorf("neither `command:` nor `webhook:` is set — an entry takes exactly one")
	case hasCommand:
		if strings.TrimSpace(h.Command[0]) == "" {
			return h.errorf("command: the first element is the program to run and must not be blank")
		}
		if len(h.Headers) > 0 || len(h.HeadersEnv) > 0 {
			return h.errorf("`headers:`/`headers-env:` belong to a `webhook:` entry — a command carries none")
		}
		return nil
	default:
		return h.validateWebhook()
	}
}

// validateWebhook refuses anything that is not an absolute http(s) URL, and any header mapped to
// a blank environment variable name — a header whose value would silently be the empty string.
func (h Hook) validateWebhook() error {
	parsed, err := url.Parse(h.Webhook)
	switch {
	case err != nil:
		return h.errorf("webhook: %q is not a URL: %v", h.Webhook, err)
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return h.errorf("webhook: %q must be an absolute http:// or https:// URL", h.Webhook)
	case parsed.Host == "":
		return h.errorf("webhook: %q names no host", h.Webhook)
	}
	for header, envName := range h.HeadersEnv {
		if strings.TrimSpace(envName) == "" {
			return h.errorf("headers-env: %q maps to no environment variable name — "+
				"the value is the NAME of the variable holding the header, not the header itself", header)
		}
	}
	return nil
}

// errorf builds a message prefixed with the entry it is about.
func (h Hook) errorf(format string, args ...any) error {
	return fmt.Errorf("hook %q: %s", h.Name, fmt.Sprintf(format, args...))
}

// ValidateAll validates every entry and refuses duplicate names. Names must be unique because
// they are the identity a failure notice, a de-dup record and the payload's "hook" field all key
// on: two Hooks called "notify" would report as one.
func ValidateAll(list []Hook) error {
	seen := make(map[string]bool, len(list))
	for _, h := range list {
		if err := h.Validate(); err != nil {
			return err
		}
		if seen[h.Name] {
			return h.errorf("a second entry has this name — hook names must be unique")
		}
		seen[h.Name] = true
	}
	return nil
}

// SubscribedEvents is the union of every listed Hook's events — the set the matcher is built over,
// so an event no Hook asked for costs nothing at all. An empty result means the matcher is a
// no-op for every engine event.
func SubscribedEvents(list []Hook) map[Event]bool {
	subscribed := make(map[Event]bool, len(allEvents))
	for _, h := range list {
		for _, e := range h.Events {
			subscribed[e] = true
		}
	}
	return subscribed
}
