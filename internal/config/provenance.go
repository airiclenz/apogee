package config

// Per-key provenance: which source supplied a key's value, what that source is called, and how
// precedence ranks the sources — config's answer, so a surface that marks a row (the binary's
// `/settings` rows) reads it here rather than re-deriving the rank order or an override's name.

// Provenance is where the value this run resolved for one key came from.
//
// Source is the precedence source that supplied it ([Options.SourceOf]). Name is what an override
// is CALLED — the environment variable or the flag that won (`APOGEE_MODE`, `--mode`) — so a
// surface can name it instead of saying "something"; it is empty for the two files and the
// default. ProjectUnion reports a tighten-only list the Project config added entries to: its value
// is the union of both files (ADR 0096 §6), so it is the project's in part even where an override
// outranks both files, which is why it is read off the project layer and not off Source.
type Provenance struct {
	Source       Source
	Name         string
	ProjectUnion bool
}

// ProvenanceOf answers [Provenance] for k in this resolution. An environment override on a key
// whose row names no variable is APOGEE_NO_UPDATE_CHECK's, the one override config applies by
// hand rather than through the row (applyNoUpdateCheck, ADR 0043's exception, ADR 0097): it forces
// `update-check` off.
func (o *Options) ProvenanceOf(k Key) Provenance {
	p := Provenance{
		Source:       o.SourceOf(k.Path),
		ProjectUnion: k.Class == ClassTightenOnly && o.ProjectKeys[k.Path],
	}
	switch p.Source {
	case SourceEnv:
		p.Name = k.EnvVar
		if p.Name == "" {
			p.Name = EnvNoUpdateCheck
		}
	case SourceFlag:
		p.Name = "--" + k.FlagName
	}
	return p
}

// sourceRank is the precedence order, lowest first: the built-in default, the global file, the
// Project config over it, then an environment variable, then an explicitly-set flag. A value this
// table does not name — the zero Source a hand-built row carries — ranks below all of them.
var sourceRank = map[Source]int{
	SourceDefault: 1,
	SourceGlobal:  2,
	SourceProject: 3,
	SourceEnv:     4,
	SourceFlag:    5,
}

// Outranks reports whether a value from s wins over one from other: whether precedence takes s's
// value when both state the key. It is the one statement of the rank order the [Source] constants
// are documented in.
func (s Source) Outranks(other Source) bool {
	return sourceRank[s] > sourceRank[other]
}

// WithFileLayers is o with its two file layers — which keys the global file and the Project config
// state (GlobalKeys, ProjectKeys) — taken from files instead, except for the keys keep names, which
// keep o's own entries. Everything else, the values and the env and flag Overrides included, stays
// o's. The two maps are fresh: neither o's nor files' is written into or handed back.
//
// It is how a surface marks keys off a later reading of the files while some keys hold the value
// the resolution o launched with (the `/settings` host's keys a re-read never reports). A nil keep
// keeps nothing.
func (o Options) WithFileLayers(files Options, keep func(Key) bool) Options {
	o.GlobalKeys = layerWithKept(files.GlobalKeys, o.GlobalKeys, keep)
	o.ProjectKeys = layerWithKept(files.ProjectKeys, o.ProjectKeys, keep)
	return o
}

// layerWithKept is a copy of current with every key keep names taken from kept instead.
func layerWithKept(current, kept map[string]bool, keep func(Key) bool) map[string]bool {
	keys := make(map[string]bool, len(current))
	for path, set := range current {
		keys[path] = set
	}
	if keep == nil {
		return keys
	}
	for _, k := range KeyRegistry {
		if !keep(k) {
			continue
		}
		delete(keys, k.Path)
		if kept[k.Path] {
			keys[k.Path] = true
		}
	}
	return keys
}
