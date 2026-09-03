package floor

import (
	"embed"
	"strings"
)

// The Floor guards' fixed model-facing text. Every sentence a guard puts in front of the model is
// an asset file under prompts/ rather than a Go string literal, so the wording can be read and
// edited as prose (ISSUES.md: hard-coded prompt literals), and go:embed compiles it into the
// binary — the text ships inside the single binary, is never read from disk at runtime, and is
// never user-overridable. Only the fixed text is an asset: the branching, the `%s` substitutions
// and the joining spaces stay in the guard that renders them.
//
//go:embed prompts/*.txt
var promptFS embed.FS

// mustPrompt loads one embedded prompt asset by file name. Every asset ends with exactly one
// trailing newline — a file without one is awkward in an editor and in a diff — and that one
// newline is stripped here, so the loaded text carries no line ending of its own. CRLF endings
// are normalised first, the way the embedded block art is (internal/tui/logo.go), so a
// core.autocrlf checkout cannot bake \r into a prompt. A name that is not in the FS cannot happen
// in a built binary — go:embed fails the build first — so it is a programming error rather than a
// runtime condition.
func mustPrompt(name string) string {
	b, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		panic("apogee: missing embedded prompt asset " + name + ": " + err.Error())
	}
	return strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}
