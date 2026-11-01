package main

import (
	"regexp"
	"strings"
)

// ciPushPaths is the gate's copy of ci.yml's on.push.paths filter, pattern for pattern and in
// the same order, so the gate can tell which commits CI never runs on. TestCIPushPathsMatchCIYAML
// fails when this copy and ci.yml disagree: change both together.
var ciPushPaths = []string{
	"**",
	"!.capcom/**",
	".capcom/knowledge/decisions/adr-0098.md",
	"!docs/plans/**",
	"!docs/handoffs/**",
	"!docs/reviews/**",
	"!docs/layout/**",
	"!docs/adr/**",
	"!docs/design/archived/**",
	"!.claude/**",
	"!.codex/**",
	"!.vscode/**",
	"!CHANGELOG.md",
	"!AGENTS.md",
	"!CLAUDE.md",
	"!SECURITY.md",
	"!LICENSE",
	"!layout.md",
	"!graphics/*.svg",
	"!graphics/demo.gif",
	"!graphics/demo/**",
	"graphics/demo/stage/**",
}

// negationPrefix marks a filter pattern that drops the paths it matches rather than adding them.
const negationPrefix = "!"

// filterPattern is one compiled line of a paths filter: whether it adds or drops what it matches.
type filterPattern struct {
	isNegated bool
	glob      *regexp.Regexp
}

// ciPushFilter is ciPushPaths compiled once.
var ciPushFilter = compilePathsFilter(ciPushPaths)

// compilePathsFilter compiles each pattern of a GitHub paths filter, keeping their order.
func compilePathsFilter(patterns []string) []filterPattern {
	compiled := make([]filterPattern, 0, len(patterns))
	for _, pattern := range patterns {
		glob, isNegated := strings.CutPrefix(pattern, negationPrefix)
		compiled = append(compiled, filterPattern{isNegated: isNegated, glob: compileGlob(glob)})
	}
	return compiled
}

// compileGlob turns a filter glob into an anchored regular expression, as GitHub reads one: `**`
// matches any run of characters including `/` (and a `**/`, wherever it stands, also matches no
// directory at all), `*` matches a run within one path segment, and every other character is
// literal. The `?`, `+` and `[]` forms are not supported; the drift test refuses a ci.yml that
// uses them.
func compileGlob(glob string) *regexp.Regexp {
	var expression strings.Builder
	expression.WriteString("^")
	for rest := glob; rest != ""; {
		switch {
		case strings.HasPrefix(rest, "**/"):
			expression.WriteString("(?:.*/)?")
			rest = rest[len("**/"):]
		case strings.HasPrefix(rest, "**"):
			expression.WriteString(".*")
			rest = rest[len("**"):]
		case strings.HasPrefix(rest, "*"):
			expression.WriteString("[^/]*")
			rest = rest[len("*"):]
		default:
			literalEnd := strings.IndexByte(rest, '*')
			if literalEnd < 0 {
				literalEnd = len(rest)
			}
			expression.WriteString(regexp.QuoteMeta(rest[:literalEnd]))
			rest = rest[literalEnd:]
		}
	}
	expression.WriteString("$")
	return regexp.MustCompile(expression.String())
}

// isSkippedByCI reports whether ci.yml's push filter leaves path out, so a push touching only
// such paths runs no CI. As GitHub evaluates the filter, the last pattern that matches a path
// decides it, and a path no pattern matches is left out.
func isSkippedByCI(path string) bool {
	isIncluded := false
	for _, pattern := range ciPushFilter {
		if pattern.glob.MatchString(path) {
			isIncluded = !pattern.isNegated
		}
	}
	return !isIncluded
}
