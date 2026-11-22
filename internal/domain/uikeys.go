package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// uiKeyDef is one key of the `ui:` block, declared once: everything a layer needs to land, spell,
// default or judge that key is read off its row here rather than restated per layer.
type uiKeyDef struct {
	// path is the key as the config file spells it, dotted under its block (a UIKey* constant).
	path string
	// listed is whether the key is one of [UIKeys], the template list a settings row edits. The
	// structured keys are not: they are edited in the file (or, for the folded sections, by the
	// panel's own toggle).
	listed bool
	// land writes the key's parsed text onto the block, `""` landing the key's own default. It
	// judges only what its parser judges; [UIPrefs.Set] runs the whole block's Validate after it.
	land func(u *UIPrefs, text string) error
	// format spells the key's field as the registry row reads it.
	format func(u UIPrefs) string
	// defaultText is the key's documented default as text — the value a config that names no such
	// key reads as (ui.stall-after `120s`, ui.side-panel.width `44`), and `""` for a structured key,
	// whose default lives in [DefaultUIPrefs] and the starter template. It is never derived by
	// formatting [DefaultUIPrefs]: that would spell the unset width (0) and the threshold's
	// `2m0s`, neither of which is the documented text.
	defaultText string
	// validate is the key's own refusal over a whole block, nil for a key no value of its field is
	// refused for (the colour scheme, judged where it resolves; the footer colours, whose roles are
	// judged by internal/config, and whose segments the field cannot hold wrongly).
	validate func(u UIPrefs) error
}

// uiKeyTable is every key of the `ui:` block in the order the starter template presents them, the
// nested `side-panel:` and `footer:` blocks' keys included. [UIKeys], [AllUIKeys],
// [DefaultUIPrefs], [UIPrefs.Set], [UIPrefs.Validate] and [ValidateUIKey] all derive from it.
var uiKeyTable = []uiKeyDef{
	nameKey(UIKeySpinner, func(u *UIPrefs) *SpinnerStyle { return &u.Spinner }, ParseSpinnerStyle,
		DefaultSpinnerStyle),
	boolKey(UIKeySpinnerColor, func(u *UIPrefs) *bool { return &u.SpinnerColor }, true),
	nameKey(UIKeyGlyphs, func(u *UIPrefs) *GlyphSet { return &u.Glyphs }, ParseGlyphSet, DefaultGlyphSet),
	boolKey(UIKeyShowScrollbar, func(u *UIPrefs) *bool { return &u.ShowScrollbar }, true),
	{
		// Stored as written: an unresolvable name costs a warning and the default palette where it
		// resolves, never a refusal here (ADR 0040).
		path: UIKeyColorScheme, listed: true,
		land: func(u *UIPrefs, text string) error {
			u.ColorScheme = text
			if text == "" {
				u.ColorScheme = DefaultColorSchemeName
			}
			return nil
		},
		format:      func(u UIPrefs) string { return u.ColorScheme },
		defaultText: DefaultColorSchemeName,
	},
	{
		path: UIKeyStallAfter, listed: true,
		land: func(u *UIPrefs, text string) error {
			after, err := ParseStallAfter(text)
			if err != nil {
				return err
			}
			u.StallAfter = after
			return nil
		},
		format:      func(u UIPrefs) string { return u.StallAfter.String() },
		defaultText: "120s",
		validate:    func(u UIPrefs) error { return validStallAfter(u.StallAfter) },
	},
	boolKey(UIKeyInspector, func(u *UIPrefs) *bool { return &u.Inspector }, false),
	boolKey(UIKeySkillSuggestions, func(u *UIPrefs) *bool { return &u.SkillSuggestions }, true),
	boolKey(UIKeyTaskListOpen, func(u *UIPrefs) *bool { return &u.TaskListOpen }, true),
	boolKey(UIKeyToolsOpen, func(u *UIPrefs) *bool { return &u.ToolsOpen }, false),
	{
		path: UIKeyToolsFoldOver, listed: true,
		land: func(u *UIPrefs, text string) error {
			n, err := ParseToolsFoldOver(text)
			if err != nil {
				return err
			}
			u.ToolsFoldOver = n
			return nil
		},
		format:      func(u UIPrefs) string { return strconv.Itoa(u.ToolsFoldOver) },
		defaultText: strconv.Itoa(DefaultToolsFoldOver),
		validate:    func(u UIPrefs) error { return validToolsFoldOver(u.ToolsFoldOver) },
	},
	boolKey(UIKeyToolsAutoCollapse, func(u *UIPrefs) *bool { return &u.ToolsAutoCollapse }, true),
	boolKey(UIKeyPointerShapes, func(u *UIPrefs) *bool { return &u.PointerShapes }, false),
	// `""` lands the shipped default (nameKey's rule), never the off ParseNotifyMode reads the zero
	// value as.
	nameKey(UIKeyNotify, func(u *UIPrefs) *NotifyMode { return &u.Notify }, ParseNotifyMode, DefaultNotifyMode),
	boolKey(UIKeyGitFetch, func(u *UIPrefs) *bool { return &u.GitFetch }, true),
	nameKey(UIKeySidePanelShow, func(u *UIPrefs) *SidePanelShow { return &u.SidePanelShow },
		ParseSidePanelShow, DefaultSidePanelShow),
	{
		path: UIKeySidePanelWidth, listed: true,
		land: func(u *UIPrefs, text string) error {
			if text == "" {
				u.SidePanelWidth = 0 // the unset width, never the default's number
				return nil
			}
			width, err := ParseSidePanelWidth(text)
			if err != nil {
				return err
			}
			u.SidePanelWidth = width
			return nil
		},
		format: func(u UIPrefs) string {
			if u.SidePanelWidth == 0 {
				return strconv.Itoa(DefaultSidePanelWidth) // the width the unset field is drawn at
			}
			return strconv.Itoa(u.SidePanelWidth)
		},
		defaultText: strconv.Itoa(DefaultSidePanelWidth),
		validate:    func(u UIPrefs) error { return validSidePanelWidth(u.SidePanelWidth) },
	},
	{
		path: UIKeySidePanelSections,
		land: func(u *UIPrefs, text string) error {
			entries, err := ParseSidePanelSections(text)
			if err != nil {
				return err
			}
			u.SidePanelSections = SpellSidePanelSections(entries)
			return nil
		},
		format: func(u UIPrefs) string { return string(u.SidePanelSections) },
		validate: func(u UIPrefs) error {
			_, err := ParseSidePanelSections(string(u.SidePanelSections))
			return err
		},
	},
	{
		path: UIKeySidePanelFolded,
		land: func(u *UIPrefs, text string) error {
			folded, err := ParseSidePanelFolded(text)
			if err != nil {
				return err
			}
			u.SidePanelFolded = SpellSidePanelFolded(folded)
			return nil
		},
		// The zero value folds nothing and reads `[]`, a list's empty spelling the key reads back.
		format: func(u UIPrefs) string {
			if u.SidePanelFolded == "" {
				return "[]"
			}
			return string(u.SidePanelFolded)
		},
		validate: func(u UIPrefs) error {
			_, err := ParseSidePanelFolded(string(u.SidePanelFolded))
			return err
		},
	},
	{
		path: UIKeyFooterRows,
		land: func(u *UIPrefs, text string) error {
			rows, err := ParseFooterRows(text)
			if err != nil {
				return err
			}
			u.FooterRows = SpellFooterRows(rows)
			return nil
		},
		format: func(u UIPrefs) string { return string(u.FooterRows) },
		validate: func(u UIPrefs) error {
			_, err := ParseFooterRows(string(u.FooterRows))
			return err
		},
	},
	{
		path: UIKeyFooterColors,
		land: func(u *UIPrefs, text string) error {
			colors, err := ParseFooterColors(text)
			if err != nil {
				return err
			}
			u.FooterColors = colors
			return nil
		},
		format: func(u UIPrefs) string { return u.FooterColors.String() },
	},
}

// boolKey is the row of a template-listed bool key: strconv.ParseBool's words, `""` landing def,
// and no refusal over the block, since a bool field holds no wrong value.
func boolKey(path string, at func(*UIPrefs) *bool, def bool) uiKeyDef {
	return uiKeyDef{
		path: path, listed: true,
		land:        func(u *UIPrefs, text string) error { return landBool(at(u), path, text, def) },
		format:      func(u UIPrefs) string { return strconv.FormatBool(*at(&u)) },
		defaultText: strconv.FormatBool(def),
	}
}

// nameKey is the row of a template-listed key whose value is one name of a vocabulary: `""` lands
// def, any other text is the parser's to accept, and both the parse and the block's refusal name
// the key in one sentence.
func nameKey[S ~string](path string, at func(*UIPrefs) *S, parse func(string) (S, error), def S) uiKeyDef {
	return uiKeyDef{
		path: path, listed: true,
		land: func(u *UIPrefs, text string) error {
			if text == "" {
				*at(u) = def
				return nil
			}
			name, err := parse(text)
			if err != nil {
				return fmt.Errorf("apogee: invalid %s: %w", path, err)
			}
			*at(u) = name
			return nil
		},
		format:      func(u UIPrefs) string { return string(*at(&u)) },
		defaultText: string(def),
		validate: func(u UIPrefs) error {
			if _, err := parse(string(*at(&u))); err != nil {
				return fmt.Errorf("apogee: invalid %s: %w", path, err)
			}
			return nil
		},
	}
}

// uiKeyDefOf is the table's row for key, answering false for a key the block does not carry.
func uiKeyDefOf(key string) (uiKeyDef, bool) {
	for _, def := range uiKeyTable {
		if def.path == key {
			return def, true
		}
	}
	return uiKeyDef{}, false
}

// unknownUIKey is the one refusal of a key the block does not carry.
func unknownUIKey(key string) error {
	return fmt.Errorf("apogee: unknown ui key %q", key)
}

// AllUIKeys returns every key of the `ui:` block in template order, the structured keys [UIKeys]
// leaves out included. It hands back a fresh slice on every call, as [UIKeys] does.
func AllUIKeys() []string {
	keys := make([]string, 0, len(uiKeyTable))
	for _, def := range uiKeyTable {
		keys = append(keys, def.path)
	}
	return keys
}

// ValidateUIKey judges one key's text alone: the text, trimmed, is landed onto a copy of
// [DefaultUIPrefs] and only that key's refusal is run, so the answer never rests on what another
// field's zero value happens to be. A key the block does not carry is refused.
func ValidateUIKey(key, text string) error {
	def, ok := uiKeyDefOf(key)
	if !ok {
		return unknownUIKey(key)
	}
	u := DefaultUIPrefs()
	if err := def.land(&u, strings.TrimSpace(text)); err != nil {
		return err
	}
	if def.validate == nil {
		return nil
	}
	return def.validate(u)
}
