package skills

import (
	"errors"
	"fmt"
	"strings"
)

// FailedLines renders the failure half of a skip report: every SKILL.md discovery found but could
// not load, named by the ID it WOULD have had, with the reason and the file to go and fix. It reads
// only the skips that are load failures and ignores the shadowed ones (ShadowedLines' half), and
// returns nil when there are none. Discovery skips a bad skill softly so one malformed file cannot
// sink the catalog; printing the skip is what keeps soft from meaning silent — otherwise a broken
// skill and an absent one look identical from the merged "/" menu, with nowhere to look for the
// difference.
//
// It lives beside Catalog.Skipped so every Driver says the same thing about the same skip: the
// TUI's /skills report renders through it, and an unattended Driver can print it without a TUI.
//
// All three repo-authored fields are FLATTENED (flattenField) — the name, the reason and the path:
// a TUI note is painted one row per line and its strip deliberately keeps "\n" (it sanitizes prose,
// and prose has paragraphs), so a folder name or a multi-line YAML error would otherwise write
// further lines into this report — lines it could shape as a loaded skill's row, source label and
// all, under a heading that counted one fewer, on the very surface that exists to disclose an
// impersonation. Flattening is not escape-stripping: a caller writing these lines to a terminal
// still strips escapes itself.
func FailedLines(skips []SkipError) []string {
	failed, _ := partitionSkips(skips)
	if len(failed) == 0 {
		return nil
	}
	head := fmt.Sprintf("%d skills found but not loaded:", len(failed))
	if len(failed) == 1 {
		head = "1 skill found but not loaded:"
	}
	lines := make([]string, 0, 2*len(failed)+1)
	lines = append(lines, head)
	for _, sk := range failed {
		lines = append(lines, "  "+flattenField(sk.Name())+" — "+flattenField(sk.Reason()))
		lines = append(lines, "    "+flattenField(sk.Path))
	}
	return lines
}

// ShadowedLines renders the collision half of a skip report: one pair per shadowed skill — the
// copy that lost, then the copy that is live. It reads only the skips carrying a ShadowedError and
// returns nil when there are none. Both paths are named because that is the whole question a
// shadow raises ("which of my two files is /<id> actually running?"), and the answer is a file the
// user can open. Nothing here is broken, so the wording deliberately never says "not loaded".
//
// The name, the losing path and the winning one are FLATTENED (flattenField) for the reason
// FailedLines' fields are: a newline in any of them would paint rows this section never authored —
// and here a forged row answers the shadow's one question with a file that is not the live copy.
func ShadowedLines(skips []SkipError) []string {
	_, shadowed := partitionSkips(skips)
	if len(shadowed) == 0 {
		return nil
	}
	head := fmt.Sprintf("%d skills shadowed by another of the same id:", len(shadowed))
	if len(shadowed) == 1 {
		head = "1 skill shadowed by another of the same id:"
	}
	lines := make([]string, 0, 2*len(shadowed)+1)
	lines = append(lines, head)
	for _, sk := range shadowed {
		by, _ := shadowedBy(sk) // partitioned on it above, so it is there
		lines = append(lines, "  "+flattenField(sk.Name())+" — "+flattenField(sk.Path))
		lines = append(lines, "    the live copy is "+flattenField(by))
	}
	return lines
}

// shadowedBy reports the winning SKILL.md's path when this skip is a lost id collision rather
// than a load failure (ShadowedError, reached through the skip's cause). It is the one place the
// report asks "which kind of skip is this?", so the partition and the rendering can never disagree
// about it.
func shadowedBy(sk SkipError) (string, bool) {
	var shadow ShadowedError
	if errors.As(sk, &shadow) {
		return shadow.By, true
	}
	return "", false
}

// partitionSkips splits one scan's skips on their cause: the SKILL.md files that could not be
// turned into a skill, and the ones that parsed fine and simply lost an id collision (ADR 0032).
// They travel down the same channel but are not the same news — a failure is a file to go and fix,
// a shadow is a file that is merely not the copy the id resolves to — so heading them both with
// "found but not loaded" would send the human hunting for a defect in a healthy file. Each half
// keeps discovery order.
func partitionSkips(skipped []SkipError) (failed, shadowed []SkipError) {
	for _, sk := range skipped {
		if _, ok := shadowedBy(sk); ok {
			shadowed = append(shadowed, sk)
			continue
		}
		failed = append(failed, sk)
	}
	return failed, shadowed
}

// flattenField folds every line break in a one-row field — "\n", "\t" and "\r" — to a single
// space, one byte for one byte, so the field paints as exactly one row. It is this package's own
// copy of the TUI's rule (internal/tui/transcript.go's flattenField): the report is built here so a
// Driver without a TUI can print it, and internal/skills must not import internal/tui. The two
// must stay the same rule, so a /skills report reads byte-for-byte as it did when the TUI built it.
func flattenField(s string) string {
	if !strings.ContainsAny(s, "\n\t\r") {
		return s // the ordinary case, unallocated
	}
	return fieldBreaks.Replace(s)
}

// fieldBreaks is flattenField's substitution, built once: each character it folds is a single
// byte in and a single byte out, so the replacer walks the field in one pass and leaves every
// other byte — an invalid one included — exactly as it found it.
var fieldBreaks = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ")
