package tools

import (
	"strings"
)

// maxToolNameDistance is the edit distance past which a registered tool name is no longer a
// near miss of the name the model wrote. Three edits covers a dropped letter, a transposition
// and a wrong separator; beyond it the two names are different words and a suggestion would
// steer the model at a tool it never meant.
const maxToolNameDistance = 3

// ClosestToolName answers the registered name a mis-spelled tool call most plausibly meant, or
// "" when none is close: first a name that BEGINS with what the model wrote (`read_fil` →
// `read_file`, the truncation a small model's decoder produces), and failing that the name
// within maxToolNameDistance edits. Matching ignores case, ties fall to the lexicographically
// smaller name so the same miss always gets the same answer, and an empty want answers "".
//
// It serves the unknown-tool refusal dispatch renders when the tool-call repair Floor guard is
// off (internal/agent/dispatch.go), which is the only path such a call reaches; with the guard
// on the call never gets this far. Report only: nothing is re-routed, the model re-issues the
// call itself.
func ClosestToolName(names []string, want string) string {
	want = strings.ToLower(want)
	if want == "" {
		return ""
	}

	best, bestDistance := "", maxToolNameDistance+1
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, want) {
			if best == "" || bestDistance > 0 || name < best {
				best, bestDistance = name, 0
			}
			continue
		}
		if bestDistance == 0 {
			continue // a prefix match outranks every edit-distance one
		}
		distance := editDistance(lower, want)
		if distance < bestDistance || (distance == bestDistance && name < best) {
			best, bestDistance = name, distance
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b over bytes — tool names are ASCII
// identifiers, so a byte is a character — computed with a two-row table.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
