package domain

import (
	"strings"
	"testing"
)

// The presentation vocabulary's whole job is to be ONE list: the config layer validates a
// spelling against it and the renderer resolves the same spelling into something it can draw.
// These tests hold the two halves of that promise — every name the vocabulary offers parses (and
// parses back to itself), and nothing the vocabulary does not offer slips through — plus the two
// properties that make the list safe to hand out: the defaults are members of it, and a caller
// sweeping it cannot reorder the copy the parser reads.

func TestParseSpinnerStyle(t *testing.T) {
	t.Parallel()

	for _, want := range SpinnerStyleNames() {
		got, err := ParseSpinnerStyle(string(want))
		if err != nil {
			t.Errorf("ParseSpinnerStyle(%q) errored: %v", want, err)
		}
		if got != want {
			t.Errorf("ParseSpinnerStyle(%q) = %q, want %q", want, got, want)
		}
	}

	got, err := ParseSpinnerStyle("")
	if err != nil {
		t.Errorf(`ParseSpinnerStyle("") errored: %v`, err)
	}
	if got != DefaultSpinnerStyle {
		t.Errorf(`ParseSpinnerStyle("") = %q, want the default %q`, got, DefaultSpinnerStyle)
	}

	// The error is the config layer's only way to tell a human what this build knows, so it has to
	// name every style rather than only say the value was wrong.
	_, err = ParseSpinnerStyle("sparkle")
	if err == nil {
		t.Fatal(`ParseSpinnerStyle("sparkle") accepted an unknown style`)
	}
	for _, style := range SpinnerStyleNames() {
		if !strings.Contains(err.Error(), string(style)) {
			t.Errorf("error %q does not name the known style %q", err, style)
		}
	}
}

// TestDefaultSpinnerStyleIsInTheVocabulary pins the one way the default can be wrong: a default
// that is not itself a known name would make an UNSET `ui.spinner:` resolve to a style the
// renderer has no animation for, which is the exact failure the shared vocabulary exists to
// prevent.
func TestDefaultSpinnerStyleIsInTheVocabulary(t *testing.T) {
	t.Parallel()

	if _, err := ParseSpinnerStyle(string(DefaultSpinnerStyle)); err != nil {
		t.Errorf("the default style %q does not parse: %v", DefaultSpinnerStyle, err)
	}
}

// TestVocabularyListsAreCopies pins the promise both name accessors make. They are read by
// surfaces that sort and filter what they offer (a settings pane, a registry row), and the list
// the parsers read is package state — handing out the backing array would let one caller's sort
// change what every other caller's spelling means.
func TestVocabularyListsAreCopies(t *testing.T) {
	t.Parallel()

	styles := SpinnerStyleNames()
	styles[0] = "clobbered"
	if SpinnerStyleNames()[0] == "clobbered" {
		t.Error("SpinnerStyleNames hands out the list the parser reads")
	}

	shapes := CursorShapeNames()
	shapes[0] = "clobbered"
	if CursorShapeNames()[0] == "clobbered" {
		t.Error("CursorShapeNames hands out the list the validator reads")
	}
}

func TestValidCursorShapeName(t *testing.T) {
	t.Parallel()

	for _, name := range CursorShapeNames() {
		if !ValidCursorShapeName(name) {
			t.Errorf("ValidCursorShapeName(%q) refused a name the vocabulary offers", name)
		}
	}

	// "" is the config layer's request for the DEFAULT, resolved by the renderer's parse. A
	// validator that accepted it would report an absent key as a present one, so it is refused
	// here even though the parse accepts it.
	for _, name := range []string{"", "beam", "Block", "block "} {
		if ValidCursorShapeName(name) {
			t.Errorf("ValidCursorShapeName(%q) accepted a name that is not in the vocabulary", name)
		}
	}
}

// TestDefaultCursorShapeNameIsInTheVocabulary is TestDefaultSpinnerStyleIsInTheVocabulary's twin:
// an unset `cursor-shape:` resolves to this name, so a default outside the list would be a caret
// the renderer cannot map.
func TestDefaultCursorShapeNameIsInTheVocabulary(t *testing.T) {
	t.Parallel()

	if !ValidCursorShapeName(DefaultCursorShapeName) {
		t.Errorf("the default cursor shape %q is not in the vocabulary", DefaultCursorShapeName)
	}
}
