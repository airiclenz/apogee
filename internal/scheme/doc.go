// Package scheme holds apogee's color schemes: one hex color per semantic role
// (the text a user typed, the chrome, an error, each autonomy mode, …), read from
// YAML files.
//
// The package is deliberately plain data — it imports neither lipgloss nor the TUI,
// so a scheme can be resolved and validated by the wiring long before anything is
// drawn, and a Scheme value is a copy-safe struct of strings (ADR 0011: it travels
// inside the value-copied Bubble Tea Model).
//
// Two properties shape the API. First, built-in schemes are compiled into the binary
// with go:embed: they are never installed on disk, never checked for at boot, and
// never downloaded. Second, loading is forgiving — Parse takes a defective file apart
// key by key, keeps every default it cannot replace, and reports what it skipped as
// Warnings instead of an error. A broken scheme file costs a user some color, never a
// crash and never a session.
//
// Schemes come from two places at once. Discover lists what there is to pick from — the
// built-ins plus every *.yaml in the user's schemes directory — and Resolve loads one,
// giving a user file precedence over a built-in of the same name, so a shipped scheme
// can be overridden without being replaced. Export runs the other way: it copies a
// built-in's commented YAML out to that directory as an editable starting point, and
// refuses to overwrite whatever is already there.
package scheme
