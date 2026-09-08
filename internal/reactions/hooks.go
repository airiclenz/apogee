package reactions

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Event names one of the moments a Hook may fire on — the NOTICE Moments of the Reaction core,
// which this name is an alias for (ADR 0076): the Hook vocabulary and the notice half of the
// Moment vocabulary are one set, not two that happen to agree. Every one is POST-HOC: it reports
// something that already happened, so a Hook reading it can change nothing about it. The set is
// additive by design — a moment not named here is not a Hook event yet — and deliberately
// excludes the per-token, tool-call, sub-agent-phase, session-save, prune and usage moments
// (ADR 0073 §4).
//
// Only the five named below have a constant here; the vocabulary itself is domain.Notices(), so
// `approval-decided` and the five `<seam>-finished` closings are accepted under `events:` from the
// same commit that added them to the core, and are matched by their spelling.
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

// Events returns the Hook events in their documented order. It IS domain.Notices() — the two
// vocabularies are one set rather than two that happen to agree, so a notice added to the Reaction
// core is a Hook event from the same commit. The slice is a fresh copy, so a caller listing them
// for a help text or a validation message cannot disturb the vocabulary.
func Events() []Event {
	return domain.Notices()
}

// ParseEvent turns one `events:` entry into an Event, refusing anything outside the vocabulary
// with a message that lists what is allowed — a misspelt event name is the likeliest mistake in
// a `hooks:` block, and a bare "invalid" would leave the user guessing at the spelling.
func ParseEvent(name string) (Event, error) {
	for _, e := range domain.Notices() {
		if string(e) == name {
			return e, nil
		}
	}
	return "", fmt.Errorf("unknown hook event %q — the events are %s", name, eventList())
}

// eventList renders the vocabulary for an error message.
func eventList() string {
	events := domain.Notices()
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, string(e))
	}
	return strings.Join(names, ", ")
}

// Validate reports whether one entry is a usable observe reaction, naming the entry in every
// message so a user with several of them is told WHICH line to fix. It is this package's own
// RUNNABLE checks — a name to report by, at least one known event, an action the executors can
// actually run, and a bounded timeout — run BEFORE [domain.Reaction.Validate], so a malformed
// entry earns the sentence that names the config key rather than the core's structural refusal.
//
// The exactly-one-action and headers-belong-to-a-webhook rules are NOT here: a domain.Reaction
// carries one Handler, so those two rules live where both fields still coexist — the config
// layer's own mapping (internal/config's hooks.go).
func Validate(r domain.Reaction) error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("hooks: an entry has no name: every hook needs a `name:` to be reported by")
	}
	if len(r.On) == 0 {
		return reactionError(r.ID, "no events: list at least one of %s under `events:`", eventList())
	}
	for _, e := range r.On {
		if _, err := ParseEvent(string(e)); err != nil {
			return reactionError(r.ID, "%v", err)
		}
	}
	if err := validateHandler(r); err != nil {
		return err
	}
	if r.Timeout <= 0 {
		return reactionError(r.ID, "timeout: %v is not a positive duration — write it as `30s` or `2m`", r.Timeout)
	}
	return r.Validate()
}

// validateHandler checks whichever action the entry carries is one the executors can run. A
// handler kind this package does not run at all is left to [domain.Reaction.Validate], which
// owns the origin × class × handler rules; the events check above has already refused it, since
// a Go handler's seam is not in the notice vocabulary.
func validateHandler(r domain.Reaction) error {
	switch handler := r.Handler.(type) {
	case domain.ArgvHandler:
		if len(handler.Argv) == 0 || strings.TrimSpace(handler.Argv[0]) == "" {
			return reactionError(r.ID, "command: the first element is the program to run and must not be blank")
		}
		return nil
	case domain.WebhookHandler:
		return validateWebhook(r.ID, handler)
	}
	return nil
}

// validateWebhook refuses anything that is not an absolute http(s) URL, and any header mapped to
// a blank environment variable name — a header whose value would silently be the empty string.
func validateWebhook(id string, handler domain.WebhookHandler) error {
	parsed, err := url.Parse(handler.URL)
	switch {
	case err != nil:
		return reactionError(id, "webhook: %q is not a URL: %v", handler.URL, err)
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return reactionError(id, "webhook: %q must be an absolute http:// or https:// URL", handler.URL)
	case parsed.Host == "":
		return reactionError(id, "webhook: %q names no host", handler.URL)
	}
	for header, envName := range handler.HeadersEnv {
		if strings.TrimSpace(envName) == "" {
			return reactionError(id, "headers-env: %q maps to no environment variable name — "+
				"the value is the NAME of the variable holding the header, not the header itself", header)
		}
	}
	return nil
}

// reactionError builds a message prefixed with the entry it is about.
func reactionError(id string, format string, args ...any) error {
	return fmt.Errorf("reaction %q: %s", id, fmt.Sprintf(format, args...))
}

// ValidateAll validates every entry and refuses duplicate names. Names must be unique because
// they are the identity a failure notice, a de-dup record and the payload's "hook" field all key
// on: two entries called "notify" would report as one.
func ValidateAll(list []domain.Reaction) error {
	seen := make(map[string]bool, len(list))
	for _, r := range list {
		if err := Validate(r); err != nil {
			return err
		}
		if seen[r.ID] {
			return reactionError(r.ID, "a second entry has this name — hook names must be unique")
		}
		seen[r.ID] = true
	}
	return nil
}

// SubscribedEvents is the union of every listed entry's events — the set the matcher is built
// over, so an event nothing asked for costs nothing at all. An empty result means the matcher is
// a no-op for every engine event.
func SubscribedEvents(list []domain.Reaction) map[Event]bool {
	subscribed := make(map[Event]bool, len(domain.Notices()))
	for _, r := range list {
		for _, e := range r.On {
			subscribed[e] = true
		}
	}
	return subscribed
}
