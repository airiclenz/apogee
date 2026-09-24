package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The typed key descriptor: one declaration of WHERE a key's value lives — its Options field and
// the fileConfig field that states it — from which the registry row's Read, its Set landing and
// resolution's file projection are all derived, instead of each being written out beside the
// others as a restatement of the same field. A row that carries one (Key.field) writes none of the
// three by hand; bindRows derives them as the table is built, and accessorsOver builds
// resolution's accessor table over the rows so a derived row needs no hand-written entry there.
//
// What the descriptor deliberately does NOT do is route the file pass through the row's Set. The
// file value is an UNVALIDATED TYPED COPY: the yaml decode already typed it, and the key's own
// "stated" predicate (the file func answering nil) decides whether the file's value or the row's
// default lands. A block the file carries is judged where it always was — the block's validator
// at ResolveOptions, the Set admission at the settings pane and the env pass — so a file edit
// that leaves one neighbour of a bool out of range still loads, as it did before the descriptor.

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
	// states the key, the row's default where it does not. The default text is parsed once,
	// here, so a row whose Default its own parse refuses fails at init rather than at a read.
	fileProjection(path, defaultText string) func(o *Options, fc fileConfig) error
}

// scalarField is the descriptor of a key held in one typed field of Options.
//
//   - at names the Options field the value lives in: Read reads it, the landing writes it, the
//     file pass writes it.
//   - file names what the file states for the key, nil where the file states nothing — the row's
//     own "stated" predicate, expressed as the pointer it answers with.
//   - parse and format are the key's spelling: canonical text to T and back.
//   - land is the landing a row routes through a block's own parser or validator instead of
//     straight onto the field (landUI, landIn); nil lands through parse onto at.
type scalarField[T any] struct {
	at     func(*Options) *T
	file   func(fileConfig) *T
	parse  func(string) (T, error)
	format func(T) string
	land   func(canonical string, o *Options) error
}

func (f scalarField[T]) read(o Options) string { return f.format(*f.at(&o)) }

func (f scalarField[T]) landing() func(canonical string, o *Options) error {
	if f.land != nil {
		return f.land
	}
	return land(f.parse, f.at)
}

func (f scalarField[T]) fileProjection(path, defaultText string) func(o *Options, fc fileConfig) error {
	def, err := f.parse(defaultText)
	if err != nil {
		panic(fmt.Sprintf("apogee: config registry row %s defaults to %q, which its own parse refuses: %v",
			path, defaultText, err))
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

// boolField is the descriptor of a bool key held in its own Options field, stated in the file by
// a pointer that is nil when the file leaves the key out.
func boolField(at func(*Options) *bool, file func(fileConfig) *bool) scalarField[bool] {
	return scalarField[bool]{at: at, file: file, parse: strconv.ParseBool, format: boolValue}
}

// uiBool is the descriptor of a bool key of the `ui:` block. Its Set lands through the block's own
// parser (landUI, ADR 0043 — no third parser); its file value is the typed copy every descriptor
// takes, so a file whose `ui:` block holds a spinner name the block validator refuses still loads.
func uiBool(key string, at func(*domain.UIPrefs) *bool, file func(*uiConfig) *bool) scalarField[bool] {
	f := boolField(func(o *Options) *bool { return at(&o.UI) }, func(fc fileConfig) *bool {
		if fc.UI == nil {
			return nil
		}
		return file(fc.UI)
	})
	f.land = landUI(key)
	return f
}

// presentBool is the descriptor of a bool key of the `present:` block. Its Set re-runs the block's
// validator over the edited copy (landIn, ADR 0043's 2026-09-16 amendment); its file value is the
// typed copy. The file func may point at a plain bool as well as a pointer: the block's
// `command-on-model-documents` is a plain bool on disk, stated whenever the block is.
func presentBool(at func(*PresentSettings) *bool, file func(*presentConfig) *bool) scalarField[bool] {
	f := boolField(func(o *Options) *bool { return at(presentOf(o)) }, func(fc fileConfig) *bool {
		if fc.Present == nil {
			return nil
		}
		return file(fc.Present)
	})
	f.land = landIn(presentOf, PresentSettings.Validate, strconv.ParseBool, at)
	return f
}

// accessorsOver builds resolution's accessor table over the registry rows, in the rows' order: a
// row that carries a field gets the file projection bindRows derived for it, keeping any env or
// flag plumbing its hand-written entry carries; every other row takes its hand-written entry
// whole. It panics — at init, since keyAccessors is a package-level table — on the three ways the
// two tables can disagree: a field row whose hand-written entry carries a fromFile of its own (two
// file projections for one key), a row with neither a field nor a hand-written fromFile (a key the
// file pass never reads), and a hand-written entry naming a row the table does not have or naming
// one twice.
func accessorsOver(rows []Key, handWritten []keyAccessor) []keyAccessor {
	byPath := make(map[string]keyAccessor, len(handWritten))
	for _, k := range handWritten {
		if _, dup := byPath[k.row.Path]; dup {
			panic("apogee: config key " + k.row.Path + " has two hand-written accessors — one key, one entry")
		}
		byPath[k.row.Path] = k
	}
	accessors := make([]keyAccessor, 0, len(rows))
	for _, row := range rows {
		k, written := byPath[row.Path]
		delete(byPath, row.Path)
		switch {
		case row.field != nil && k.fromFile != nil:
			panic("apogee: config key " + row.Path + " derives its file value from its field, " +
				"and its hand-written accessor carries a fromFile as well")
		case row.field != nil:
			k.fromFile = row.fromFile
		case !written || k.fromFile == nil:
			panic("apogee: config key " + row.Path + " has neither a field nor a hand-written fromFile, " +
				"so the file pass would never read it")
		}
		k.row = row
		accessors = append(accessors, k)
	}
	if len(byPath) > 0 {
		stray := make([]string, 0, len(byPath))
		for path := range byPath {
			stray = append(stray, path)
		}
		sort.Strings(stray)
		panic("apogee: hand-written config accessors name keys the registry does not describe: " +
			strings.Join(stray, ", "))
	}
	return accessors
}
