package domain

import (
	"fmt"
	"slices"
	"strings"
)

// The presentation vocabulary a Driver is CONFIGURED with — the names a `ui.spinner:`,
// a `cursor-shape:` or a prebound start is spelled with, and the parsing those names go
// through. It lives here for the reason ParseMode does (config.go, ADR 0043): the words are
// facts about the configuration surface, not about any one renderer, and the config layer has
// to validate them. With the names in internal/tui, internal/config had to import the renderer
// to check a spelling — a legal sibling import that defeated the split's own motivation.
//
// What stays with the renderer is everything TYPED by a terminal library: the animation each
// SpinnerStyle paints, and the tea.CursorShape each cursor-shape name means. Domain names the
// vocabulary; internal/tui maps it onto pixels. That division is what keeps this package a leaf
// with no dependency beyond the standard library.

// SpinnerStyle names a status-line animation. The vocabulary is declared once, here, so the
// config layer validates a `ui.spinner:` value against the same list the renderer resolves an
// animation from.
type SpinnerStyle string

const (
	SpinnerSnake   SpinnerStyle = "snake"
	SpinnerGlitter SpinnerStyle = "glitter"
	SpinnerClassic SpinnerStyle = "classic"
)

// spinnerStyleNames is the vocabulary [ParseSpinnerStyle] accepts, in the order its error lists
// them: the names this build knows, declared once so no caller re-types the set.
var spinnerStyleNames = []SpinnerStyle{SpinnerSnake, SpinnerGlitter, SpinnerClassic}

// DefaultSpinnerStyle is what an unset config value resolves to.
const DefaultSpinnerStyle = SpinnerSnake

// SpinnerStyleNames returns the styles this build knows, in the order [ParseSpinnerStyle]'s error
// lists them. It hands back a fresh slice on every call so a caller sweeping the vocabulary — a
// settings pane offering it, a test covering it — cannot reorder the one the parser reads.
func SpinnerStyleNames() []SpinnerStyle { return slices.Clone(spinnerStyleNames) }

// ParseSpinnerStyle maps a config value onto a style. "" ⇒ the default; an unknown value is an
// error naming the styles this build knows. The caller names the key it read the value from —
// this package does not know the config schema.
func ParseSpinnerStyle(s string) (SpinnerStyle, error) {
	if s == "" {
		return DefaultSpinnerStyle, nil
	}
	for _, style := range spinnerStyleNames {
		if SpinnerStyle(s) == style {
			return style, nil
		}
	}
	names := make([]string, 0, len(spinnerStyleNames))
	for _, style := range spinnerStyleNames {
		names = append(names, string(style))
	}
	return "", fmt.Errorf("unknown spinner style %q (known styles: %s)", s, strings.Join(names, ", "))
}

// cursorShapeNames is the vocabulary a `cursor-shape:` value is validated against, in the order
// [UnknownCursorShapeError] lists them. Only the NAMES live here: what each one draws is a
// terminal-library constant, and this package stays a leaf, so internal/tui owns the mapping and
// derives the names it accepts from this list.
var cursorShapeNames = []string{"block", "underline", "bar"}

// DefaultCursorShapeName is what an unset `cursor-shape:` resolves to.
const DefaultCursorShapeName = "block"

// CursorShapeNames returns the caret shapes a config value may name, in the order
// [UnknownCursorShapeError] lists them. It hands back a fresh slice on every call, for
// [SpinnerStyleNames]' reason.
func CursorShapeNames() []string { return slices.Clone(cursorShapeNames) }

// ValidCursorShapeName reports whether s names a caret shape this build can draw. The empty
// string is NOT valid here: "" is the config layer's request for the default, which the renderer's
// parse resolves — a validator that accepted it would report an absent key as a present one.
//
// The set is closed at three because that is what a terminal cursor can be. Inheriting the shape
// the terminal itself is configured with is deliberately NOT among them: a full-screen program
// names a shape on every frame and never emits the DECSCUSR reset while it runs, so there is
// nothing to inherit back into — this key is the honest substitute (the terminal's own cursor
// returns on exit).
func ValidCursorShapeName(s string) bool { return slices.Contains(cursorShapeNames, s) }

// UnknownCursorShapeError is the refusal every surface reports for a `cursor-shape:` value no caret
// has: the config layer's validator and the renderer's parse both ASK for the sentence rather than
// spell it, so the wording and the list of known shapes exist once, here, beside the vocabulary the
// list is read from. A caller that speaks for a config key wraps this with the key it read the
// value from — this package does not know the config schema (as [ParseSpinnerStyle]).
func UnknownCursorShapeError(name string) error {
	return fmt.Errorf("unknown cursor shape %q (known shapes: %s)", name, strings.Join(cursorShapeNames, ", "))
}

// PreboundReason names why a session started with NO upstream bound — the three answers ADR 0036
// gives when the config alone cannot say which server to start on. It is a fact the binary resolved
// (it read the file, the flags and the environment); the renderer only renders it and, on the first
// two, asks the human through the picker it already has.
//
// The empty value is the ordinary start: a server WAS determined and the engine was constructed
// before the program began.
type PreboundReason string

const (
	// PreboundFirstBoot: servers are configured, none is chosen yet (no `server:` recorded). The
	// picker asks, and the choice is what records one.
	PreboundFirstBoot PreboundReason = "first-boot"
	// PreboundStaleChoice: the recorded `server:` names an entry the list no longer carries — a
	// renamed or deleted server, or a typo. It is state, not intent: the picker fixes in one
	// keystroke what a refusal would send to file surgery.
	PreboundStaleChoice PreboundReason = "stale-choice"
	// PreboundNoServers: nothing is configured at all, so there is nothing to pick from and the
	// guidance is to add a server to config.yaml.
	PreboundNoServers PreboundReason = "no-servers"
)

// PreboundStart says whether this session started with an upstream bound, and if not, why. The zero
// value — an empty Reason — is the ordinary bound start, so a hand-built Driver configuration
// describes today's behaviour without naming this field at all.
type PreboundStart struct {
	Reason PreboundReason
	// Name is the `server:` value that named no entry, carried for PreboundStaleChoice so the
	// notice can say which one went missing. Empty for every other reason.
	Name string
}
