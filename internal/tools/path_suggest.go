package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxPathSuggestions caps how many sibling entries a not-found refusal offers. Five is a
// hint, not a listing: a model handed every near-miss in a large directory is reading a
// listing it did not ask for, and list_dir is the tool for that.
const maxPathSuggestions = 5

// suggestSiblings answers the entries of a missing path's PARENT whose names begin with the
// missing name — the "did you mean" a refusal can offer instead of only saying no. A model
// that spelled a long filename by its prefix (docs/adr/0025) gets back the name it meant.
//
// root is the root the path was ACCEPTED under (readScope.resolve already answered that);
// rel is the missing path relative to that root in NATIVE separator form (workspaceRelative,
// i.e. filepath.Rel, which is backslash-spelled on Windows), so it is split with filepath and
// handed to the fence in the same spelling; and given is the model's OWN spelling of the path,
// which is what the results are spelled in — each entry is joined onto given's parent, so every
// suggestion is a path the model can hand straight back. (root, rel) alone never carries that
// spelling, which is why given is a parameter rather than derived.
//
// Matching is case-insensitive, results are sorted by name, and at most maxPathSuggestions are
// returned; a directory entry carries a trailing "/" as list_dir renders one. The parent is
// opened THROUGH the fence, never by an absolute path this helper assembled, and a parent that
// is missing, unreadable, not a directory, or refused by the fence yields nil — a refusal never
// becomes a listing of somewhere the caller may not read. An entry is reported by NAME and
// nothing else: a sibling that is a symlink out of the root is named here and refused by the
// fence if the model goes on to open it, which is the same answer it would get for any other
// path. The rendered suggestion is data inside a one-message grammar, so it passes through
// escapeRowBreaks like any other path a tool quotes.
//
// Nothing here judges containment: the helper is only reached AFTER resolve accepted the path,
// so a fence refusal carries no suggestions.
func suggestSiblings(root, rel, given string) []string {
	base := filepath.Base(rel)
	if base == "." || base == string(filepath.Separator) {
		return nil // no name was missing — nothing to be a near-miss of
	}

	parent, err := safeOpen(filepath.Dir(rel), root)
	if err != nil {
		return nil
	}
	defer func() { _ = parent.Close() }()

	entries, err := parent.ReadDir(-1)
	if err != nil {
		return nil // a parent that is not a directory, or one that will not enumerate
	}
	// A directory HANDLE yields entries in filesystem order; the suggestions are sorted by
	// name so the same missing path always answers with the same list in the same order.
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })

	prefix := strings.ToLower(base)
	givenParent := filepath.Dir(given)
	suggestions := make([]string, 0, maxPathSuggestions)
	for _, entry := range entries {
		if len(suggestions) >= maxPathSuggestions {
			break
		}
		name := entry.Name()
		if !strings.HasPrefix(strings.ToLower(name), prefix) {
			continue
		}

		suggestion := filepath.Join(givenParent, name)
		if entry.IsDir() {
			suggestion += "/"
		}
		suggestions = append(suggestions, escapeRowBreaks(suggestion))
	}
	if len(suggestions) == 0 {
		return nil
	}
	return suggestions
}

// notFoundMessage renders a path-not-found refusal for the model: the caller's own wording and
// the path AS THE MODEL SPELLED IT, followed by the near misses suggestSiblings found.
//
// prefix is the refusal's leading text up to and including its separator — "path not found: ",
// "directory not found: ", "file not found: " — so a call site keeps its wording byte-identical
// by passing the literal it used to concatenate. With no suggestions the result IS that former
// message, unchanged to the byte; with suggestions one " — did you mean: " clause is appended,
// several joined by "; ".
func notFoundMessage(prefix, given string, suggestions []string) string {
	message := prefix + given
	if len(suggestions) == 0 {
		return message
	}
	return message + " — did you mean: " + strings.Join(suggestions, "; ")
}
