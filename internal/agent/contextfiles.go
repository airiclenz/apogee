package agent

// Workspace context files (Config.ContextFiles): named files discovered in the workspace root
// whose content becomes part of the session's standing system content — the AGENTS.md /
// CLAUDE.md behaviour. This file owns the discovery half: the per-session cache, the loader
// that fills it, and the construction-time name gate.
//
// Two properties shape the design. Content is DATA — it never meets internal/prompt, so a
// repo's own {{braces}} pass through verbatim and a stranger's file can never fail apogee's
// startup. And the read is SESSION-SCOPED — the cache is filled at construction and refilled
// only at a session boundary (/clear|/new, a live restore), so the bytes seeded into a request
// are stable for the whole of a session and the model server's prefix KV cache survives.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// contextFile is one resolved workspace context file held in the session cache. Exactly one of
// content and err is meaningful: a readable file carries its verbatim bytes and their size, an
// unreadable one carries only the error so the host can report the skip loudly. A file that is
// missing, empty, or whitespace-only never becomes an entry at all.
type contextFile struct {
	name    string // the workspace-relative name as configured, and the header the content rides under
	content string // verbatim file content (never rendered as a template); empty when err is set
	size    int    // content size in bytes, for the session notice
	err     error  // non-nil when the file is present but could not be read
}

// loadContextFiles reads each of names from workspaceDir and returns the entries worth
// carrying, in list order. It is the whole discovery policy in one place:
//
//   - a MISSING file is skipped with no trace — the default name will not exist in every repo,
//     so absence is the common case and must stay silent;
//   - a present-but-UNREADABLE file yields an entry carrying only the error, so the host can
//     say so out loud without failing the session over a discovered (never configured-by-name)
//     file; a name that RESOLVES OUT OF THE WORKSPACE is unreadable in exactly this sense —
//     refused, and reported loudly rather than silently dropped;
//   - an EMPTY or whitespace-only file is skipped with no trace — there is no point heading a
//     block over nothing;
//   - anything else carries the file's verbatim bytes and their size.
//
// An empty workspaceDir returns nil: an embedder with no workspace has no basis to resolve a
// relative name against. Names are assumed already validated (newAgent's gate); readContextFile
// then fences what the name RESOLVES to.
func loadContextFiles(workspaceDir string, names []string) []contextFile {
	if workspaceDir == "" || len(names) == 0 {
		return nil
	}

	files := make([]contextFile, 0, len(names))
	for _, name := range names {
		data, err := readContextFile(workspaceDir, name)
		switch {
		// An unopenable workspace root LEADS: it is the one failure that will hit every name in
		// the list, and reporting it in the escape's words would blame the file rather than the
		// root the session lost.
		case errors.Is(err, security.ErrRootInaccessible):
			files = append(files, contextFile{name: name, err: err})
		// The escape case leads the rest so a refusal can never be mistaken for absence and
		// dropped in silence: a hostile repo's escaping symlink must cost the user a visible
		// notice.
		case errors.Is(err, security.ErrPathEscape):
			files = append(files, contextFile{name: name, err: err})
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			files = append(files, contextFile{name: name, err: err})
		case strings.TrimSpace(string(data)) == "":
			continue
		default:
			files = append(files, contextFile{name: name, content: string(data), size: len(data)})
		}
	}

	if len(files) == 0 {
		return nil
	}
	return files
}

// readContextFile reads one context file's verbatim bytes THROUGH the workspace fence, using the
// same pinned-root open every other in-workspace read in the engine goes through (readFileRef,
// loop.go). validateContextFileNames gates the configured NAME; this gates what that name
// resolves to on disk, which is a different class: a clone shipping `AGENTS.md -> ../../.ssh/id_rsa`
// passes the name gate and, read with a bare os.ReadFile, folds a stranger's file verbatim into the
// standing SYSTEM message of every request. security.SafeOpen refuses a component that leaves the
// root instead of following it (ADR 0026's fence), returning ErrPathEscape for the loader to book
// as present-but-unreadable.
//
// The read itself is deliberately unbounded — context files are the user's own conventions, and
// oversize is reported by ContextFilesReport against the Budget's system share rather than
// truncated here.
func readContextFile(workspaceDir, name string) ([]byte, error) {
	f, err := security.SafeOpen(workspaceDir, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(f)
}

// validateContextFileNames rejects a name the loader must never be handed: an empty one, one that
// is not workspace-relative (rooted, or Windows drive-scoped), or one whose cleaned form climbs
// out of the workspace. It is the engine's defense-in-depth gate (ADR 0023 §3's posture, mirroring
// the prompt.Validate one beside it): for config users the host's own check fires first and names
// the config key, so this one exists to make a Go embedder's typo fail construction loudly rather
// than reach for a file outside the workspace. Every error names the offending entry.
//
// The name rule itself is domain's (workspacename.go) — the SAME rule the host's config check
// applies, so an embedder is refused exactly what a config user is, and the two gates cannot
// drift. It is machine-independent by construction: `C:AGENTS.md` and `..\x` are refused on Linux
// and macOS too, rather than passing here and reaching outside the workspace on Windows.
func validateContextFileNames(names []string) error {
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return errors.New("apogee: Config.ContextFiles: empty name")
		}
		if !domain.IsWorkspaceRelative(name) {
			return fmt.Errorf("apogee: Config.ContextFiles: %q is not workspace-relative; names are "+
				"looked up in the workspace root", name)
		}
		if domain.EscapesWorkspace(name) {
			return fmt.Errorf("apogee: Config.ContextFiles: %q escapes the workspace", name)
		}
	}
	return nil
}

// reloadContextFiles refills the session cache from the workspace. It is called at every
// session boundary and NOWHERE else — construction, a successful ClearContext (/clear, /new),
// and a successful RestoreSession — so the content seeded into requests is fixed for the life
// of a session while a repo whose context files changed is picked up by the next one.
//
// It reads the LIVE name list (contextFileList), so a mid-session SetContextFiles is picked up
// here — at the next boundary — and nowhere earlier: the names move when the session does.
func (a *Agent) reloadContextFiles() {
	a.contextFiles = loadContextFiles(a.cfg.WorkspaceDir, a.contextFileList())
}

// contextFileHeader introduces each file's content in the standing system message, so a model
// reading one merged block can tell whose conventions it is being handed — and where one file's
// content ends and the next one's begins.
const contextFileHeader = "## Workspace context: "

// contextFileFooter closes each file's content under the same name the header opened it with, so
// the extent of a repo-controlled block is stated at BOTH ends. A header alone marks where
// content begins and leaves where it ends to be inferred from the next header — which is exactly
// the inference a file can play with by spelling structure of its own.
const contextFileFooter = "## End of workspace context: "

// workspaceTextPrefix marks a content line that spells one of the standing system message's own
// structural lines. It is the ONE rewrite ADR 0026 §3's "content is data, verbatim" admits: the
// line still travels, every byte of it, behind a prefix that says who wrote it.
const workspaceTextPrefix = "[workspace text] "

// hasContextBlocks reports whether this session holds at least one READABLE context file — the
// exact condition under which contextBlocks renders anything, asked without building the string.
// The orientation block (orientation.go) states that workspace blocks follow only when they do.
func (a *Agent) hasContextBlocks() bool {
	for _, f := range a.contextFiles {
		if f.err == nil && f.content != "" {
			return true
		}
	}
	return false
}

// forgesStandingStructure reports whether one content line spells a line the standing system
// message uses as its OWN furniture: a context-file header or footer, the orientation block's
// header, the delegate report block's opening sentence, or the task list block's header opening.
// Leading whitespace is trimmed before
// the test — an indented forgery reads as furniture to a model just as well as a flush one — but
// the line itself is never trimmed, only prefixed.
//
// It is a CLOSED list and every engine-owned block belongs on it: a block this list does not know
// can be forged by a repo file that reaches the model after the real one, where it reads as a
// correction rather than as the workspace prose it is (F-19, orientation.go).
func forgesStandingStructure(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, contextFileHeader) ||
		strings.HasPrefix(trimmed, contextFileFooter) ||
		strings.HasPrefix(trimmed, orientationHeader()) ||
		strings.HasPrefix(trimmed, delegateReportFence) ||
		strings.HasPrefix(trimmed, tasklist.Fence)
}

// fenceContent prefixes every line of a context file that spells the standing message's own
// structure with workspaceTextPrefix, and returns the content untouched when no line does — the
// common case, which costs one Split and no copy.
//
// This is the fence's second half: the header/footer pair states where a repo's text begins and
// ends, and this keeps the text between them from spelling either one, or an engine-owned block's
// own opening line, convincingly. Nothing else is rewritten — {{braces}}, markdown, indentation and every
// other byte travel exactly as written (ADR 0026 §3).
func fenceContent(content string) string {
	lines := strings.Split(content, "\n")
	fenced := false
	for i, line := range lines {
		if !forgesStandingStructure(line) {
			continue
		}
		lines[i] = workspaceTextPrefix + line
		fenced = true
	}
	if !fenced {
		return content
	}
	return strings.Join(lines, "\n")
}

// contextBlocks renders the session's cached content as the blocks that ride the seeded system
// message: one block per readable entry, in list order, each opened by a header and closed by a
// footer naming the file, joined by a blank line. An entry carrying only an error contributes
// nothing (an unreadable file is reported to the user, never to the model), so a cache of
// nothing but errors renders "".
//
// The content goes out verbatim — no internal/prompt anywhere on this path — so a repo's own
// {{braces}} reach the model as written; the ONE exception is the fence above, which prefixes a
// line that spells this message's own structure. Only trailing newlines are trimmed, so the
// blank line between blocks is exactly one however the file happened to end.
//
// The result is a pure function of the cache, and the cache only moves at a session boundary:
// every request of a session therefore seeds byte-identical content and the server's prefix KV
// cache survives the whole session.
func (a *Agent) contextBlocks() string {
	if len(a.contextFiles) == 0 {
		return ""
	}

	blocks := make([]string, 0, len(a.contextFiles))
	for _, f := range a.contextFiles {
		if f.err != nil || f.content == "" {
			continue
		}
		blocks = append(blocks, contextFileHeader+f.name+"\n\n"+
			fenceContent(strings.TrimRight(f.content, "\r\n"))+"\n\n"+
			contextFileFooter+f.name)
	}
	return strings.Join(blocks, "\n\n")
}

// ContextFilesReport reports what this session's workspace context files contributed and what
// the standing system content they ride in costs — one note per cache entry (loaded or
// unreadable, in list order), the estimated token size of the WHOLE seeded system content, and
// the Budget share that content is allocated. It is the data behind the host's session notice;
// nothing on this path reaches the model.
//
// The measure is taken at CALL time, not at the boundary that filled the cache, and that is the
// point: the context window may bind seconds after a cold start (the first heartbeat), so a
// report taken then has no allocation to compare against and simply reports a zero share, while
// the same session's next /new — after the window bound — can say the content is over its share.
//
// StandingTokens measures standingSystem(), the very string buildRequest seeds, so what the user
// is told is what the model is sent rather than a second, drifting estimate of it. Like the
// session boundaries beside it this is an idle-only read: it renders the prompt (Mode, date) and
// reads the token accounting, so the host calls it with no worker driving the Agent.
func (a *Agent) ContextFilesReport() domain.ContextFilesReport {
	report := domain.ContextFilesReport{}
	if len(a.contextFiles) > 0 {
		report.Files = make([]domain.ContextFileNote, 0, len(a.contextFiles))
		for _, f := range a.contextFiles {
			note := domain.ContextFileNote{Name: f.name, Bytes: f.size}
			if f.err != nil {
				note.Err = f.err.Error()
			}
			report.Files = append(report.Files, note)
		}
	}

	budget := a.budget()
	report.StandingTokens = budget.EstimateTokens(len(a.standingSystem()))
	report.SystemShare = budget.SystemPrompt
	return report
}
