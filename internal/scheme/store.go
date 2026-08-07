package scheme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// fileExt is the suffix a scheme file carries, in the embedded set and in the user's
// schemes directory alike. A scheme's name is its file name without it.
const fileExt = ".yaml"

// builtinNames lists the shipped schemes, sorted. It is computed once because the
// embedded set is fixed at build time and cannot change while apogee runs.
var builtinNames = sync.OnceValue(func() []string {
	entries, err := builtinFS.ReadDir("schemes")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), fileExt); ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
})

// ValidName reports whether name can be a scheme's file name — a name, not a path. Resolve joins
// the name onto the schemes directory, so a separator ("sub/dark"), a traversal ("../../.ssh/id_rsa")
// or a volume would read a file from outside that directory, which is never what naming a scheme
// means. Resolve and Export both ask this before they touch the filesystem, so the package is safe
// however it is called; cmd/apogee asks the same question again before it WRITES the
// `ui.color-scheme` key, so a human hears about a bad name at the keystroke rather than at the next
// start.
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return false
	}
	// A Windows drive-relative spelling ("C:dark") carries no separator yet still leaves the
	// directory. VolumeName is empty on every other OS, so this costs nothing there.
	return !filepath.IsAbs(name) && filepath.VolumeName(name) == ""
}

// Discover lists the scheme names a user can choose from: every built-in plus every
// *.yaml in userDir, sorted and deduplicated. A user file that shadows a built-in of the
// same name appears once, under the one name they share — Resolve decides which of the
// two that name loads.
//
// No file is opened, so listing stays cheap enough to run on every draw of a picker, and
// a defective file costs nothing until someone selects it. A userDir that is missing or
// unreadable simply contributes nothing: not having written a scheme yet is the normal
// case, not an error.
func Discover(userDir string) []string {
	names := slices.Clone(builtinNames())
	entries, err := os.ReadDir(userDir)
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(entry.Name(), fileExt)
		if !ok || name == "" {
			continue
		}
		if slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Resolve loads the named scheme: the user's <userDir>/<name>.yaml first, so a file
// there shadows a built-in of the same name (design call 6), then the embedded built-ins.
// A name that is neither, a file that will not open, and a file with defective keys all
// cost the user warnings and never the run — every path returns a usable Scheme.
func Resolve(name, userDir string) (Scheme, []Warning) {
	if !ValidName(name) {
		// Refused before the join below, which would otherwise read <userDir>/../../x.yaml —
		// outside the schemes directory entirely.
		return Default(), []Warning{{File: name, Reason: fmt.Sprintf("a scheme is named, not a path — using the %q scheme", DefaultName)}}
	}
	file := name + fileExt
	if userDir != "" {
		// An empty userDir would make this a relative read out of the working directory,
		// which is never what a caller means; it means "no user schemes".
		data, err := os.ReadFile(filepath.Join(userDir, file))
		switch {
		case err == nil:
			return Parse(file, data)
		case !errors.Is(err, fs.ErrNotExist):
			// The file is there but will not open — a permission or I/O fault. Say so
			// rather than quietly loading a built-in that happens to share the name,
			// which would look like the user's edits had no effect.
			return Default(), []Warning{{File: file, Reason: fmt.Sprintf("unreadable (%v) — using the %q scheme", err, DefaultName)}}
		}
	}
	if data, ok := builtinBytes(name); ok {
		return Parse(file, data)
	}
	return Default(), []Warning{{File: name, Reason: fmt.Sprintf("unknown — using the %q scheme", DefaultName)}}
}

// Export writes an editable copy of a built-in scheme to <userDir>/<name>.yaml: the
// embedded bytes verbatim, comments and all, because those comments are the
// documentation a user edits against.
//
// Only built-ins are exportable — the point is to obtain a starting copy, and a user
// file already is one. An existing file is never overwritten, so an export can never
// destroy a scheme someone has been working on; they are told to edit or delete it
// instead. It returns the path written.
func Export(name, userDir string) (string, error) {
	// A name that is really a path is refused by name, not as a side effect of the lookup
	// below happening to miss: the rule is stated once (ValidName) and both entry points ask it.
	if !ValidName(name) {
		return "", fmt.Errorf("apogee: %q is not a color-scheme name — a scheme is named, not a path; built-ins are: %s", name, strings.Join(builtinNames(), ", "))
	}
	// Past that, only an exact built-in name gets through, so nothing but a shipped scheme's
	// own name ever reaches the write below.
	data, ok := builtinBytes(name)
	if !ok {
		return "", fmt.Errorf("apogee: no built-in color-scheme %q — built-ins are: %s", name, strings.Join(builtinNames(), ", "))
	}
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return "", fmt.Errorf("apogee: create color-scheme directory %q: %w", userDir, err)
	}
	path := filepath.Join(userDir, name+fileExt)
	// O_EXCL is the refusal itself: asking "does it exist?" and creating it are one
	// operation, so no answer can go stale between the two.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("apogee: color-scheme %q already exists — edit it or delete it first", path)
		}
		return "", fmt.Errorf("apogee: create color-scheme %q: %w", path, err)
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// A half-written scheme would both render wrong and block the retry that fixes
		// it, so the failed attempt takes its own file with it.
		os.Remove(path)
		return "", fmt.Errorf("apogee: write color-scheme %q: %w", path, err)
	}
	return path, nil
}
