package mechanisms

import (
	"embed"
	"strings"
)

// promptFS carries this package's prompt text as plain files under prompts/ — the fixed wording of
// every catalogued Mechanism that speaks to the model: the CoT, decompose, completion-check and
// library-injection assets that cot.go, decompose.go, emptyresponse.go and library.go load. They are
// assets rather than Go string literals so the wording can be read and edited as prose (ISSUES.md:
// hard-coded prompt literals), and go:embed compiles them into the binary — the text ships inside
// the single binary, is never read from disk at runtime, and is never user-overridable. Only the
// fixed text is an asset: the branching, the `%s` substitution and the joining spaces stay in the
// Mechanism that renders them (design call 2).
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
