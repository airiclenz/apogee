package mechanisms

import (
	"bytes"
	"context"
	"go/format"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// autofix registers the formatter Mechanism in the catalogue constructor table (Phase-4 item 5).
// Default-off (D1).
func init() { catalogue[autofixID] = newAutofix }

// formatterTimeout bounds an external formatter subprocess so a hung tool cannot stall the Turn.
const formatterTimeout = 3 * time.Second

// lookPath resolves an external formatter's executable. It is a package var (defaulting to
// exec.LookPath) so a test can inject formatter availability without a real PATH — the standing
// requirement #2 "gracefully absent" behaviour is exercised by pointing it at a not-found stub.
var lookPath = exec.LookPath

// autofixMechanism is the post-response formatter (catalogue Table A `autofix`; ported from
// apogee-sim internal/autofix @pin). For each file-writing tool call it formats the content and,
// when formatting changed it, writes the result back through Response.SetToolCallArguments so the
// tool the loop dispatches receives the tidied payload.
//
// Go is formatted with the standard library's in-process gofmt (go/format.Source) ALWAYS — no
// external dependency (standing requirement #2) — with goimports preferred when it is on PATH
// (it is a superset that also fixes imports). Every other language formats only through its
// external formatter (black / prettier / rustfmt) when detected on PATH, and is left untouched
// otherwise. autofix never defers or retries: a formatter cannot repair genuinely broken syntax
// (that is syntax's job, one stage earlier), so it either improves the payload in place or no-ops.
type autofixMechanism struct{}

// newAutofix builds the autofix Mechanism. External-formatter availability is discovered from
// PATH at fire time (via the injectable lookPath), not through Deps, so item 5 stays within
// internal/mechanisms; threading availability through Deps (D3) is a later refinement.
func newAutofix(Deps) (domain.Mechanism, error) { return autofixMechanism{}, nil }

// Descriptor identifies autofix as a strikes-3 response-repair Mechanism (catalogue Table A).
func (autofixMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          autofixID,
		Capability:  domain.CapResponseRepair,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering runs autofix after syntax (catalogue Table A) — last of the response-repair cascade.
func (autofixMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{After: []domain.MechanismID{syntaxID}}
}

// PostResponse formats every write tool call's content in place, writing back any change. The
// decision is ActionIntercept when at least one call was reformatted (the intercept path — the
// response was altered in place), and a no-op decision otherwise.
func (autofixMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	calls := resp.ToolCalls()
	changed := false
	for i, call := range calls {
		if !isWriteTool(call.Tool) {
			continue
		}
		path, content, ok := writePathContent(call.Arguments)
		if !ok {
			continue
		}
		formatted, did := formatContent(path, content)
		if !did || formatted == content {
			continue
		}
		newArgs, err := replaceContentArg(call.Arguments, formatted)
		if err != nil {
			continue
		}
		resp.SetToolCallArguments(i, newArgs)
		changed = true
	}
	if changed {
		return domain.PostResponseDecision{Action: domain.ActionIntercept}, nil
	}
	return domain.PostResponseDecision{}, nil
}

// formatContent formats content by its language. did is false when nothing formatted it — an
// unrecognised language, an absent external formatter, or a formatter that could not improve the
// input (e.g. gofmt on unparseable Go) — in which case the caller leaves the payload untouched.
func formatContent(path, content string) (out string, did bool) {
	switch detectLanguage(path) {
	case "":
		return content, false
	case "go":
		return formatGo(content)
	default:
		f, ok := externalFormatters[detectLanguage(path)]
		if !ok {
			return content, false
		}
		return runExternalFormatter(f, path, content)
	}
}

// formatGo formats Go source: goimports when it is on PATH (imports + gofmt), else the in-process
// gofmt that is always available. Broken Go that the parser cannot read is returned unchanged
// (did == false) — a formatter is not a syntax repairer.
func formatGo(content string) (string, bool) {
	if out, ok := runExternalFormatter(goimportsFormatter, "", content); ok {
		return out, true
	}
	out, err := format.Source([]byte(content))
	if err != nil {
		return content, false
	}
	return string(out), true
}

// formatter describes an external formatter: its command, its stdin args, and whether a
// placeholder in those args is replaced with the real file path (prettier keys formatting on the
// filename).
type formatter struct {
	command      string
	args         []string
	usesFilePath bool
}

// goimportsFormatter is the optional Go import-fixer; formatGo prefers it over in-process gofmt
// when present.
var goimportsFormatter = formatter{command: "goimports"}

// externalFormatters maps a detected language to its external formatter (gofmt is excluded — Go
// formats in-process). Each is used only when present on PATH.
var externalFormatters = map[string]formatter{
	"python":     {command: "black", args: []string{"-", "--quiet"}},
	"javascript": {command: "prettier", args: []string{"--stdin-filepath", "file.js"}, usesFilePath: true},
	"typescript": {command: "prettier", args: []string{"--stdin-filepath", "file.ts"}, usesFilePath: true},
	"rust":       {command: "rustfmt"},
}

// runExternalFormatter runs f over content via stdin, returning the formatted output. ok is false
// when the command is not on PATH, the subprocess fails or times out, or it produced empty output
// — every failure mode degrades silently to "leave the payload as-is" (standing requirement #2).
func runExternalFormatter(f formatter, path, content string) (string, bool) {
	cmdPath, err := lookPath(f.command)
	if err != nil {
		return content, false
	}

	args := append([]string(nil), f.args...)
	if f.usesFilePath && path != "" {
		safe := path
		if strings.HasPrefix(safe, "-") {
			safe = "./" + safe
		}
		for i, a := range args {
			if a == "file.ts" || a == "file.js" {
				args[i] = safe
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), formatterTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	cmd.Stdin = strings.NewReader(content)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return content, false
	}
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		return content, false
	}
	return out, true
}
