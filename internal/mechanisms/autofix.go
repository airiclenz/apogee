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

// autofix registers the formatter-repair Mechanism in the catalogue constructor table (Phase-4
// item 5). Default-off (D1).
func init() { catalogue[autofixID] = newAutofix }

// formatterTimeout bounds an external formatter subprocess so a hung tool cannot stall the Turn.
// It is a var (not a const) purely so the tests that exercise the real subprocess path can raise
// it to load-independent headroom under the concurrent suite; production keeps the 3s bound.
var formatterTimeout = 3 * time.Second

// autofixMechanism is the post-response formatter repair (catalogue Table A `autofix`; ported
// from apogee-sim internal/autofix @pin). For each file-writing tool call whose content is
// syntax-broken (checkSyntax reports issues) it runs the language's formatter ladder and keeps
// the output only when it REDUCES the issue count (the sim's AttemptFix gate), writing the
// repaired payload back through Response.SetToolCallArguments so the tool the loop dispatches
// receives it. Clean content is never touched — autofix is a repairer, not a beautifier — and a
// "fix" that does not improve the content is discarded, so the payload only ever gets better.
//
// It runs after validate and before syntax (catalogue Table A): the sim's cascade is detect →
// tryAutoFix → correct-the-remainder (internal/proxy/response_analysis.go:72-88 @pin), so repair
// precedes the correction stage and syntax's retry covers only what a formatter could not fix.
//
// The formatter table is resolved ONCE at construction through Deps.LookPath (D3) and cached on
// the Mechanism — a fire never probes PATH. Go always keeps the in-process gofmt tail
// (go/format.Source — no external dependency, standing requirement #2) behind goimports when
// that was found; black / prettier / rustfmt repair only when their executable was found at
// construction, and a language whose formatters are absent degrades silently to "leave the
// payload as-is".
type autofixMechanism struct {
	// repairs is the construction-resolved formatter ladder per language: each entry runs one
	// formatter over (path, content) and reports whether it produced output. Ladder order is the
	// sim's registry order (goimports before the gofmt tail); a language with no entry has no
	// available repairer, so its broken content passes through to syntax untouched.
	repairs map[string][]repairer
}

// repairer runs one resolved formatter over content, returning the formatted output. ok is false
// when the formatter produced nothing usable — a subprocess failure/timeout, empty output, or
// (for the in-process Go tail) unparseable input — and the caller tries the ladder's next rung.
type repairer func(path, content string) (string, bool)

// newAutofix builds the autofix Mechanism, probing each external formatter's executable exactly
// once through deps.LookPath (nil ⇒ exec.LookPath) and caching the resolved paths into the
// per-language repair ladder — the sim's LookPath-cached formatter table, injected at
// construction per D3 so fires never touch PATH. An absent executable simply leaves its rung
// out; Go's in-process gofmt tail is always appended.
func newAutofix(deps Deps) (domain.Mechanism, error) {
	look := deps.LookPath
	if look == nil {
		look = exec.LookPath
	}

	resolved := map[string]string{} // command → path ("" = absent); each command probed once
	probe := func(command string) string {
		path, done := resolved[command]
		if !done {
			p, err := look(command)
			if err != nil {
				p = ""
			}
			resolved[command] = p
			path = p
		}
		return path
	}

	repairs := make(map[string][]repairer)
	for _, entry := range externalFormatters {
		cmdPath := probe(entry.spec.command)
		if cmdPath == "" {
			continue
		}
		spec := entry.spec
		repairs[entry.language] = append(repairs[entry.language],
			func(path, content string) (string, bool) {
				return runExternalFormatter(cmdPath, spec, path, content)
			})
	}

	// Go's always-available tail: the in-process gofmt (go/format), keeping the sim's
	// goimports → gofmt ladder shape with no external dependency (standing requirement #2).
	// It cannot repair what the parser cannot read — that content stays for syntax to correct.
	repairs["go"] = append(repairs["go"], func(_, content string) (string, bool) {
		out, err := format.Source([]byte(content))
		if err != nil {
			return content, false
		}
		return string(out), true
	})

	return autofixMechanism{repairs: repairs}, nil
}

// Descriptor identifies autofix as a strikes-3 response-repair Mechanism (catalogue Table A).
func (autofixMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          autofixID,
		Capability:  domain.CapResponseRepair,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering runs autofix after validate and before syntax (catalogue Table A): the sim repairs
// before it corrects (response_analysis.go:72-88 @pin — detect → tryAutoFix →
// correct-the-remainder), so syntax's retry covers only what a formatter could not fix.
func (autofixMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{
		After:  []domain.MechanismID{validateID},
		Before: []domain.MechanismID{syntaxID},
	}
}

// PostResponse attempts a repair of every syntax-broken write tool call, writing each improved
// payload back to the call the loop will dispatch. The decision is ActionIntercept when at least
// one call was repaired (the response was altered in place), and a no-op decision otherwise —
// autofix never defers or retries: correcting the remainder is syntax's job, one stage later.
func (m autofixMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	changed := false
	for i, call := range resp.ToolCalls() {
		if !isWriteTool(call.Tool) {
			continue
		}
		path, content, ok := writePathContent(call.Arguments)
		if !ok {
			continue
		}
		fixed, did := m.attemptFix(path, content)
		if !did || fixed == content {
			continue
		}
		newArgs, err := replaceContentArg(call.Arguments, fixed)
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

// attemptFix ports the sim's AttemptFix (internal/autofix/autofix.go @pin): act only on a sane
// path whose content is syntax-broken, run the language's ladder in order, and keep the FIRST
// output that reduces the issue count. fixed is false when nothing improved the content — the
// caller leaves the payload untouched and syntax corrects the remainder one stage later.
func (m autofixMechanism) attemptFix(path, content string) (fixed string, did bool) {
	if !sanitizePath(path) {
		return content, false
	}
	lang := detectLanguage(path)
	if lang == "" {
		return content, false
	}
	original := checkSyntax(path, content)
	if original.valid {
		return content, false
	}
	for _, repair := range m.repairs[lang] {
		out, ok := repair(path, content)
		if !ok {
			continue
		}
		if len(checkSyntax(path, out).errors) < len(original.errors) {
			return out, true
		}
	}
	return content, false
}

// sanitizePath is the sim's path guard (internal/autofix/autofix.go sanitizePath @pin): refuse a
// write path carrying NUL or CR/LF control characters before it can reach an external
// formatter's argv. The "-" prefix hardening on the argv substitution itself stays in
// runExternalFormatter.
func sanitizePath(path string) bool {
	return path != "" && !strings.ContainsAny(path, "\x00\n\r")
}

// formatterSpec describes an external formatter: its command, its stdin args, and whether a
// placeholder in those args is replaced with the real file path (prettier keys formatting on the
// filename).
type formatterSpec struct {
	command      string
	args         []string
	usesFilePath bool
}

// externalFormatters is the sim's formatter registry (internal/autofix/formatters.go @pin) minus
// gofmt — Go's always-available tail is the in-process go/format the constructor appends. Slice
// order is the sim's ladder order, and construction probes each command exactly once (prettier
// backs two languages but is looked up once).
var externalFormatters = []struct {
	language string
	spec     formatterSpec
}{
	{"go", formatterSpec{command: "goimports"}},
	{"python", formatterSpec{command: "black", args: []string{"-", "--quiet"}}},
	{"typescript", formatterSpec{command: "prettier", args: []string{"--stdin-filepath", "file.ts"}, usesFilePath: true}},
	{"javascript", formatterSpec{command: "prettier", args: []string{"--stdin-filepath", "file.js"}, usesFilePath: true}},
	{"rust", formatterSpec{command: "rustfmt"}},
}

// runExternalFormatter runs the construction-resolved formatter at cmdPath over content via
// stdin, returning the formatted output. ok is false when the subprocess fails or times out, or
// when it produced empty output — every failure mode degrades silently to "leave the payload
// as-is" (standing requirement #2).
func runExternalFormatter(cmdPath string, spec formatterSpec, path, content string) (string, bool) {
	args := append([]string(nil), spec.args...)
	if spec.usesFilePath && path != "" {
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
