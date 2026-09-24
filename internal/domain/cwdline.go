package domain

import "strings"

// CwdLinePrefix opens the first line of every result a subprocess tool renders from a run
// that has a working directory: `cwd: /path/to/dir`. The line says what the command's own
// relative paths were relative to — the workspace root, or the `workdir` the call named — so a
// model reading `./build/out` in the output knows where that is without a second call, and a
// model that changed directories inside the line is reminded it did not change where the NEXT
// call starts. The tools package writes it; StripCwdLine is its one reader on the host side; the
// model reads it as text. It lives here, beside the ToolResult it opens, so a host reads the
// line's shape without importing the package that writes it.
const CwdLinePrefix = "cwd: "

// StripCwdLine takes the `cwd:` line (CwdLinePrefix) off the front of a terminal or python_exec
// result's content and returns the rest, or content unchanged when no such line opens it. It is
// the ONE strip every host-side consumer of that content shares — the TUI's success detail, the
// TUI's failure body and headless narration — so the three cannot drift into different readings
// of where the output begins: the line is written for the model, and a card or a narration line
// that already names the command has nothing to gain from repeating the directory above its
// first line of output.
func StripCwdLine(content string) string {
	if !strings.HasPrefix(content, CwdLinePrefix) {
		return content
	}
	if _, rest, found := strings.Cut(content, "\n"); found {
		return rest
	}
	return ""
}
