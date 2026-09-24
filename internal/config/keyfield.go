package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// The typed key descriptor: one declaration of WHERE a key's value lives — its Options field and
// the fileConfig field that states it — from which the registry row's Read, its Set landing, its
// Copy, resolution's file projection and, for a key with a variable or a flag, the env and flag
// passes are all derived, instead of each being written out beside the others as a restatement of
// the same field. A row that carries one (Key.field) writes none of them by hand; bindRows derives
// Read, Set, Copy and the file, env and flag projections onto the row as the table is built, and
// the resolution passes call them off the row.
//
// What the descriptor deliberately does NOT do is route the file pass through the row's Set. The
// file value is an UNVALIDATED TYPED COPY: the yaml decode already typed it, and the key's own
// "stated" predicate (the file func answering nil) decides whether the file's value or the row's
// default lands. A block the file carries is judged where it always was — the block's validator
// at ResolveOptions, the Set admission at the settings pane and the env pass — so a file edit
// that leaves one neighbour of a key out of range still loads, as it did before the descriptor.
// The exceptions are the keys the file spells as TEXT a parse can fail on (fileText): the five
// whose file pass has always landed through the row's Set (sub-agents-choice, delegate-timeout,
// stream-idle-timeout, re-stream-budget, cursor-shape), so a bad value refuses at the pass in the
// row's own sentence; `ui.stall-after`, refused at the pass by the block's own duration parser;
// and `sessions.max-age`, whose unparseable text is carried as written for the block's validator
// to refuse at ResolveOptions.
//
// The environment pass lands a variable's text through the row's bound Set — admitted exactly as
// a value typed at the settings pane is — while the flag pass copies the already-parsed flag
// value onto the field unjudged, as the flag pass always has (`--mode` is refused by the Driver's
// own parse, after resolution).

// fieldSpec is what bindRows derives a row's accessors from. It is an interface over the one
// generic implementation (scalarField) so the registry — a slice of one Key type — can hold
// descriptors of every value type side by side.
type fieldSpec interface {
	// read spells the resolved value the way the file spells it: the row's Read.
	read(o Options) string
	// landing lands canonical text on the field: the row's Set before bindRows binds the
	// admission in front of it.
	landing() func(canonical string, o *Options) error
	// fileProjection is resolution's file pass for the row: the file's value where the file
	// states the key, the row's default where it does not. set is the row's BOUND Set, which a
	// key the file spells as checked text lands through. The default text is parsed once, here,
	// so a row whose Default its own parse refuses fails at init rather than at a read.
	fileProjection(path, defaultText string, set func(value string, o *Options) error) func(o *Options, fc fileConfig) error
	// flagCopy is the flag pass for a row with a flag: the parsed flag value copied onto the
	// field, unvalidated.
	flagCopy() func(o *Options, flags Options)
	// copy moves the field's value from src onto dst and nothing else — only the key's own field
	// of a block, never the block: the row's Copy.
	copy(dst, src *Options)
}

// scalarField is the descriptor of a key held in one typed field of Options.
//
//   - at names the Options field the value lives in: Read reads it, the landing writes it, the
//     file pass writes it.
//   - file names what the file states for the key, nil where the file states nothing — the row's
//     own "stated" predicate, expressed as the pointer it answers with. The value is copied
//     across as typed.
//   - fileText, in file's place, names the TEXT the file states for a key a parse can refuse, nil
//     where the file states nothing; fileLand lands it (nil: through the row's bound Set).
//   - parse and format are the key's spelling: canonical text to T and back.
//   - land is the landing a row routes through a block's own parser or validator instead of
//     straight onto the field (landUI, landIn); nil lands through parse onto at.
//   - zeroUnstated resolves a key the file does not state to T's zero value rather than to the
//     row's Default — `cursor-shape`, whose empty name is the renderer's request for its default.
type scalarField[T any] struct {
	at           func(*Options) *T
	file         func(fileConfig) *T
	fileText     func(fileConfig) *string
	fileLand     func(text string, o *Options) error
	parse        func(string) (T, error)
	format       func(T) string
	land         func(canonical string, o *Options) error
	zeroUnstated bool
}

func (f scalarField[T]) read(o Options) string { return f.format(*f.at(&o)) }

func (f scalarField[T]) landing() func(canonical string, o *Options) error {
	if f.land != nil {
		return f.land
	}
	return land(f.parse, f.at)
}

func (f scalarField[T]) fileProjection(path, defaultText string,
	set func(value string, o *Options) error) func(o *Options, fc fileConfig) error {
	if (f.file == nil) == (f.fileText == nil) {
		panic("apogee: config registry row " + path + " must name exactly one of a typed file value and a file text")
	}
	var def T
	if !f.zeroUnstated {
		parsed, err := f.parse(defaultText)
		if err != nil {
			panic(fmt.Sprintf("apogee: config registry row %s defaults to %q, which its own parse refuses: %v",
				path, defaultText, err))
		}
		def = parsed
	}
	if f.fileText != nil {
		landText := f.fileLand
		if landText == nil {
			landText = set
		}
		return func(o *Options, fc fileConfig) error {
			*f.at(o) = def
			text := f.fileText(fc)
			if text == nil {
				return nil
			}
			return landText(*text, o)
		}
	}
	return func(o *Options, fc fileConfig) error {
		v := def
		if stated := f.file(fc); stated != nil {
			v = *stated
		}
		*f.at(o) = v
		return nil
	}
}

// zeroWhenUnstated is the field with zeroUnstated set: a key the file leaves out resolves to T's
// zero value rather than to the row's Default.
func (f scalarField[T]) zeroWhenUnstated() scalarField[T] {
	f.zeroUnstated = true
	return f
}

func (f scalarField[T]) flagCopy() func(o *Options, flags Options) {
	return func(o *Options, flags Options) { *f.at(o) = *f.at(&flags) }
}

func (f scalarField[T]) copy(dst, src *Options) { *f.at(dst) = *f.at(src) }

// boolField is the descriptor of a bool key held in its own Options field, stated in the file by
// a pointer that is nil when the file leaves the key out.
func boolField(at func(*Options) *bool, file func(fileConfig) *bool) scalarField[bool] {
	return scalarField[bool]{at: at, file: file, parse: strconv.ParseBool, format: boolValue}
}

// stringField is the descriptor of a name, a path or a command line held in its own Options field
// and copied across as the file spells it.
func stringField(at func(*Options) *string, file func(fileConfig) *string) scalarField[string] {
	return scalarField[string]{at: at, file: file, parse: asIs, format: spelled[string]}
}

// intField is the descriptor of a count held in its own Options field; its file func is where the
// key's own "stated" predicate lives (a negative, a zero or an absent value that resolves to the
// default says so by answering nil).
func intField(at func(*Options) *int, file func(fileConfig) *int) scalarField[int] {
	return scalarField[int]{at: at, file: file, parse: strconv.Atoi, format: strconv.Itoa}
}

// floatField is the descriptor of the one fractional key, spelled in the shortest form that reads
// back as the same number.
func floatField(at func(*Options) *float64, file func(fileConfig) *float64) scalarField[float64] {
	return scalarField[float64]{at: at, file: file, parse: parseFloat, format: formatFloat}
}

// listField is the descriptor of a name list held in its own Options field. The file states the
// list only when it names at least one entry: an absent or empty list resolves to the row's
// default, which parseList reads as nil — the same nil Set lands `[]` as, so "nothing set" and an
// emptied list read back alike.
func listField(at func(*Options) *[]string, file func(fileConfig) []string) scalarField[[]string] {
	return scalarField[[]string]{
		at: at,
		file: func(fc fileConfig) *[]string {
			names := file(fc)
			if len(names) == 0 {
				return nil
			}
			return &names
		},
		parse: parseList, format: listValue,
	}
}

// checkedField is the descriptor of a key the file spells as TEXT that lands through the row's own
// Set, so a value the key cannot take is refused at the file pass in the row's sentence — the
// posture of the keys whose parse can fail (a duration's text) or whose vocabulary the yaml decode
// cannot judge (an enum's word).
func checkedField[T any](parse func(string) (T, error), format func(T) string, at func(*Options) *T,
	text func(fileConfig) *string) scalarField[T] {
	return scalarField[T]{at: at, fileText: text, parse: parse, format: format}
}

// uiField is the descriptor of a key of the `ui:` block. Its Set lands through the block's own
// parser (landUI, ADR 0043 — no third parser); its file value is the typed copy every descriptor
// takes, so a file whose `ui:` block holds a spinner name the block validator refuses still loads.
func uiField[T any](key string, parse func(string) (T, error), format func(T) string,
	at func(*domain.UIPrefs) *T, file func(*uiConfig) *T) scalarField[T] {
	return scalarField[T]{
		at: func(o *Options) *T { return at(&o.UI) },
		file: func(fc fileConfig) *T {
			if fc.UI == nil {
				return nil
			}
			return file(fc.UI)
		},
		parse: parse, format: format, land: landUI(key),
	}
}

// uiBool is uiField for a bool of the block.
func uiBool(key string, at func(*domain.UIPrefs) *bool, file func(*uiConfig) *bool) scalarField[bool] {
	return uiField(key, strconv.ParseBool, boolValue, at, file)
}

// uiStallAfter is the descriptor of `ui.stall-after`, the one `ui:` key the file spells as text a
// parse can fail on. The file pass reads it through domain.ParseStallAfter — the parser every
// surface reads the key with — so the refusal is that parser's sentence, quoting the text as
// written, made at the file pass as it always was; the empty value is the parser's "the default".
func uiStallAfter() scalarField[time.Duration] {
	f := uiField(domain.UIKeyStallAfter, domain.ParseStallAfter, time.Duration.String,
		func(u *domain.UIPrefs) *time.Duration { return &u.StallAfter }, nil)
	f.file = nil
	f.fileText = func(fc fileConfig) *string {
		if fc.UI == nil {
			return nil
		}
		return fc.UI.StallAfter
	}
	f.fileLand = land(domain.ParseStallAfter, f.at)
	return f
}

// presentField is the descriptor of a key of the `present:` block. Its Set re-runs the block's
// validator over the edited copy (landIn, ADR 0043's 2026-09-16 amendment); its file value is the
// typed copy, so a block carrying an out-of-range port still loads and is refused at
// ResolveOptions. The file func may point at a plain field as well as a pointer: every key of the
// block but `auto-open` is a plain value on disk, stated whenever the block is.
func presentField[T any](parse func(string) (T, error), format func(T) string,
	at func(*PresentSettings) *T, file func(*presentConfig) *T) scalarField[T] {
	return scalarField[T]{
		at: func(o *Options) *T { return at(presentOf(o)) },
		file: func(fc fileConfig) *T {
			if fc.Present == nil {
				return nil
			}
			return file(fc.Present)
		},
		parse: parse, format: format,
		land: landIn(presentOf, PresentSettings.Validate, parse, at),
	}
}

// presentBool is presentField for a bool of the block.
func presentBool(at func(*PresentSettings) *bool, file func(*presentConfig) *bool) scalarField[bool] {
	return presentField(strconv.ParseBool, boolValue, at, file)
}

// sessionsMaxCount is the descriptor of `sessions.max-count`: a count the block's validator judges
// (landIn), copied across as typed so a negative one is refused at ResolveOptions, not at the load.
func sessionsMaxCount() scalarField[int] {
	at := func(s *SessionSettings) *int { return &s.MaxCount }
	return scalarField[int]{
		at: func(o *Options) *int { return at(sessionsOf(o)) },
		file: func(fc fileConfig) *int {
			if fc.Sessions == nil {
				return nil
			}
			return fc.Sessions.MaxCount
		},
		parse: strconv.Atoi, format: strconv.Itoa,
		land: landIn(sessionsOf, SessionSettings.Validate, strconv.Atoi, at),
	}
}

// sessionsMaxAge is the descriptor of `sessions.max-age`, a duration the file spells as text. The
// file pass judges nothing (toSessionSettings): text no duration can be made of is carried AS
// WRITTEN beside the default age, for SessionSettings.Validate to refuse and quote at
// ResolveOptions — so the landing runs on every pass, the absent key reading as the empty text,
// and a pass over a file that no longer carries the bad text clears it.
func sessionsMaxAge() scalarField[time.Duration] {
	at := func(s *SessionSettings) *time.Duration { return &s.MaxAge }
	return scalarField[time.Duration]{
		at: func(o *Options) *time.Duration { return at(sessionsOf(o)) },
		fileText: func(fc fileConfig) *string {
			if fc.Sessions == nil || fc.Sessions.MaxAge == nil {
				return new(string)
			}
			return fc.Sessions.MaxAge
		},
		fileLand: func(text string, o *Options) error {
			read := sessionsConfig{MaxAge: &text}.toSessionSettings()
			o.Sessions.MaxAge, o.Sessions.unparsedMaxAge = read.MaxAge, read.unparsedMaxAge
			return nil
		},
		parse: parseSessionsMaxAge, format: time.Duration.String,
		land: landIn(sessionsOf, SessionSettings.Validate, parseSessionsMaxAge, at),
	}
}

// The file-side "stated" predicates the rows share: a string the file leaves empty, a count the
// file leaves out or spells below the key's floor, states nothing, so the row's default lands.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonEmptyName[S ~string](s string) *S {
	if s == "" {
		return nil
	}
	name := S(s)
	return &name
}

func atLeast(floor int, n *int) *int {
	if n == nil || *n < floor {
		return nil
	}
	return n
}

// spelled is the format of a key whose value IS its spelling: a name, a path, a vocabulary word.
func spelled[S ~string](s S) string { return string(s) }

// named is the parse of a vocabulary word the block's own validator judges, not this parse.
func named[S ~string](s string) (S, error) { return S(s), nil }

// formatFloat spells the one fractional key in the shortest form that reads back as the same number.
func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
