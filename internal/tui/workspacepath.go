package tui

import (
	"path/filepath"
	"strings"
)

// ----------------------------------------------------------------------------
// Workspace-relative paths (presentation only)
// ----------------------------------------------------------------------------
//
// A tool call names files the way the tool loop needs them — absolutely, because a confinement
// check and an os.Open both want an unambiguous path. A transcript reader needs the opposite:
// they are looking at ONE project, every interesting file is inside it, and repeating the same
// long prefix on every branch line spends the row's width saying what the reader already knows.
// So the workspace root is shortened out of the paths a tool block NAMES — the target it acts on,
// and the one-line summary of its outcome when that summary is the block's own wording — and out of
// nothing else. What the block QUOTES is not in that set: a body (a diff's hunk lines, an edit's
// replacement string, an unregistered tool's verbatim arguments) and equally a line promoted onto
// the branch beside the target — a one-line output, or the answer a human typed into an ask_user
// question — where an absolute path is content and has to reach the screen exactly as it was
// written, or the block misrepresents what a write will land on disk, or what a person actually
// said. Which lines are quoted is marked where they are built (branchSummary, toolBody),
// never guessed from the text. The shortening is a spelling applied on the way to the screen, never
// a rewrite of the arguments the model sent or the output a tool returned (layout.md, "The rules
// behind the tool-call sketch").

// workspaceRoot is the project root a tool card's paths are printed relative to. It is a resolved
// root rather than a raw string so the cleaning and the validation happen ONCE per session
// (newWorkspaceRoot) instead of on every line of every repaint, and so a caller cannot hand the
// shortener an unchecked path: the zero value is the root that shortens nothing, which is what an
// unwired Options.Workspace and every hand-built test view get.
type workspaceRoot struct {
	// root is the cleaned absolute workspace path, or "" when there is nothing to shorten against.
	root string
}

// newWorkspaceRoot resolves the root paths are printed relative to. An empty or relative path
// yields the zero value: with no absolute root there is no "inside the workspace" to test a path
// against, and guessing one from the process's working directory would make the transcript's
// spelling depend on where apogee was launched from rather than on what it was pointed at.
//
// A filesystem root ("/", or a bare volume root on Windows) is refused for the neighbouring
// reason: it would make every absolute path on the machine "inside the workspace", so `/etc/passwd`
// would print as `etc/passwd` and the one distinction this whole file exists to preserve — in the
// project versus genuinely elsewhere — would collapse.
func newWorkspaceRoot(path string) workspaceRoot {
	p := strings.TrimSpace(path)
	if p == "" || !filepath.IsAbs(p) {
		return workspaceRoot{}
	}
	clean := filepath.Clean(p)
	if clean == filepath.VolumeName(clean)+string(filepath.Separator) {
		return workspaceRoot{}
	}
	return workspaceRoot{root: clean}
}

// shorten rewrites one display line so every path it names inside the workspace reads relative to
// it — `/home/me/proj/docs/plan.md` becomes `docs/plan.md`, with no leading `./` and no leading
// separator — and the workspace root itself reads `.`, the ordinary relative spelling of "here"
// and the one filepath.Rel returns for it.
//
// Three things are deliberately left alone. A path OUTSIDE the workspace keeps its absolute form:
// it genuinely is elsewhere, and a relative spelling would say it was not — which matters most
// exactly where it is most dangerous, on the write outside the project the human is deciding
// about. A path that arrived relative is already in the form this function produces. And ordinary
// prose is untouched, because the only thing recognised here is the workspace root's own spelling:
// a line that merely contains a slash is not a path to this function, so nothing about a URL, a
// regex, a date or a fraction can be mangled by it.
//
// It works on a LINE rather than on a lone path because a path slot rarely holds a lone path — a
// target reads `cp a.go b.go`, a summary reads "replaced text in <path>" — so the root is
// shortened wherever such a line mentions it, with the boundary rules in mentionAt keeping a
// sibling directory that merely opens with the same spelling (`…/apogee-old`) whole. Which lines
// are handed here is the caller's rule, not this function's (toolView.shortenPaths).
func (w workspaceRoot) shorten(s string) string {
	if w.root == "" || !strings.Contains(s, w.root) {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], w.root)
		if j < 0 {
			out.WriteString(s[i:])
			return out.String()
		}
		start := i + j
		text, next, ok := w.mentionAt(s, start, start+len(w.root))
		if !ok {
			// A false match: copy through its first byte and keep looking, so a later real mention
			// on the same line is still found.
			out.WriteString(s[i : start+1])
			i = start + 1
			continue
		}
		out.WriteString(s[i:start])
		out.WriteString(text)
		i = next
	}
	return out.String()
}

// mentionAt decides what an occurrence of the root at s[start:end] actually is, and reports what
// takes its place (text), where the line resumes after it (next), and whether it is a mention of
// the workspace root at all (ok).
//
// The two boundaries are the whole rule. On the LEFT the root must open a path rather than end one:
// a name byte or a separator before it means the match is the tail of something longer
// (`/mnt/backup/home/me/proj`), which is a different directory that must not be shortened into the
// current one. On the RIGHT, a separator means a path inside the workspace — the prefix simply goes
// away, leaving the remainder as the relative path — while a name byte means a sibling whose name
// merely opens with the root's spelling (`/home/me/project`), and anything else (end of line,
// punctuation, a space) means the mention was the root itself, which prints `.`.
func (w workspaceRoot) mentionAt(s string, start, end int) (text string, next int, ok bool) {
	if start > 0 && (pathNameByte(s[start-1]) || pathSepByte(s[start-1])) {
		return "", 0, false
	}
	if end == len(s) {
		return ".", end, true
	}
	if !pathSepByte(s[end]) {
		if pathNameByte(s[end]) {
			return "", 0, false
		}
		return ".", end, true
	}
	rest := end
	for rest < len(s) && pathSepByte(s[rest]) {
		rest++
	}
	if rest == len(s) || !pathNameByte(s[rest]) {
		// A trailing separator and nothing that continues the path: still the root itself.
		return ".", rest, true
	}
	return "", rest, true
}

// workdirDisplay respells the workspace path the way the footer names it: the cleaned path with
// the home directory written as `~`. The footer has one line to say where this session is rooted,
// and `~/Repos/apogee` is both the spelling a human recognises their own project by and the one
// that leaves the mode marker its room (layout.md, "The footer's upstream slot").
//
// home is a parameter rather than an os.UserHomeDir call so the respelling is a pure function that
// can be proven off the real environment; the one caller resolves it once per session.
//
// The substitution happens only at a COMPONENT boundary — the path IS home, or home is followed by
// a separator — so a sibling whose name merely opens with home's spelling (`/Users/username-other`
// against home `/Users/username`) is left whole, the same false match mentionAt refuses above.
// Anything else, an empty home included, comes back cleaned but unrespelled, and an empty path
// comes back empty so the footer drops the segment rather than printing filepath.Clean's ".".
func workdirDisplay(path, home string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	h := strings.TrimSpace(home)
	if h == "" {
		return p
	}
	h = filepath.Clean(h)
	if p == h {
		return "~"
	}
	// A filesystem-root home cleans to a bare separator, which the boundary test below would
	// otherwise have to see doubled; trimming it lets the shared test cover that degenerate case too.
	h = strings.TrimSuffix(h, string(filepath.Separator))
	if rest, ok := strings.CutPrefix(p, h); ok && rest != "" && pathSepByte(rest[0]) {
		return "~" + rest
	}
	return p
}

// pathSepByte reports whether b separates two path components. Both spellings count on every OS:
// the root is cleaned to the host separator, but a tool's own output (a command's, a compiler's)
// routinely prints the other one, and a boundary test that missed it would refuse a real mention.
func pathSepByte(b byte) bool {
	return b == '/' || b == filepath.Separator
}

// pathNameByte reports whether b can be part of a path component's NAME — what the boundary tests
// above use to tell "the mention ends here" from "the name goes on". The set is deliberately
// generous: everything that continues a name must be in it (a missing byte would shorten
// `/home/me/proj-old` into `-old`), while a byte wrongly included at worst leaves one path
// unshortened. Any byte of a multi-byte rune counts, so a non-ASCII component name is a name.
func pathNameByte(b byte) bool {
	if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' {
		return true
	}
	switch b {
	case '.', '-', '_', '~', '+', '@', '%':
		return true
	}
	return b >= 0x80
}
