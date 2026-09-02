package mechanisms

import (
	"context"
	"go/format"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/syntaxcheck"
	"github.com/airiclenz/apogee/internal/tools"
)

// autofix registers the formatter-repair Mechanism's catalogue row (Phase-4 item 5). Default-off
// (D1).
func init() {
	register(row{
		descriptor: autofixDescriptor,
		// Ordering runs autofix after validate and before syntax (catalogue Table A): the sim repairs
		// before it corrects (response_analysis.go:72-88 @pin — detect → tryAutoFix →
		// correct-the-remainder), so syntax's retry covers only what a formatter could not fix.
		ordering: domain.OrderingConstraints{
			After:  []domain.MechanismID{validateID},
			Before: []domain.MechanismID{syntaxID},
		},
		construct: newAutofix,
	})
}

// defaultFormatterTimeout bounds an external formatter subprocess so a hung tool cannot stall the
// Turn. It seeds the per-Mechanism timeout at construction; a test that needs a different bound
// sets the field on its own constructed Mechanism, so no bound is ever shared between concurrent
// tests and production always gets 3s.
const defaultFormatterTimeout = 3 * time.Second

// autofixMechanism is the post-response formatter repair (catalogue Table A `autofix`; ported
// from apogee-sim internal/autofix @pin). For each file-writing tool call whose content is
// syntax-broken (syntaxcheck.Check reports issues) it runs the language's formatter ladder and keeps
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
// that was found; black / rustfmt repair only when their executable was found at construction,
// and a language whose formatters are absent degrades silently to "leave the payload as-is".
//
// Spawning any of those formatters is gated on the hook-time domain.SubprocessPermit
// (docs/design/confinement-execution-contract.md §10): a Mechanism runs outside the per-call
// Resolution, so without a permit on the fire's context the external rungs are skipped entirely
// and only the in-process tail — which spawns nothing — remains. That keeps autofix's repair
// available in every mode, Plan included, while its subprocess surface stays behind the same
// ladder row the tools obey.
type autofixMechanism struct {
	// repairs is the construction-resolved formatter ladder per language: each entry runs one
	// formatter over the content and reports whether it produced output. Ladder order is the
	// sim's registry order (goimports before the gofmt tail); a language with no entry has no
	// available repairer, so its broken content passes through to syntax untouched.
	repairs map[string][]repairer

	// timeout bounds each external formatter subprocess: it narrows the FIRE's context, which is
	// what stops a run. The kill path is NOT this field's — the subprocess funnel pins every
	// child's WaitDelay to its own fixed 5s platform.ProcessWaitDelay. Seeded from
	// defaultFormatterTimeout in newAutofix.
	timeout time.Duration

	// secretEnv names the operator-declared credential variables every external rung drops from
	// its child's environment, beside apogee's own fixed half (tools.RunHookSubprocess). Injected
	// once at construction from Deps.SecretEnvVars (D3) — the same `api-key-env:` names the
	// execution tools scrub — so a formatter spawned here sees no more of the operator's keys than
	// a terminal command does. Nil leaves the fixed half alone.
	secretEnv []string
}

// repairer is one rung of a language's formatter ladder. external is decided at CONSTRUCTION and
// carried on the entry rather than re-derived from the language at fire time, so the permit gate
// can skip a spawning rung without knowing which formatter it would have been. run performs the
// repair and returns the formatted output; ok is false when the rung produced nothing usable — a
// box that could not be established, a subprocess failure/timeout, empty output, or (for the
// in-process Go tail) unparseable input — and the caller tries the ladder's next rung.
type repairer struct {
	external bool
	run      func(ctx context.Context, gate spawnGate, content string) (string, bool)
}

// spawnGate is the fire-time answer to "may an external rung spawn at all, and inside what box",
// read once per PostResponse from the hook-time domain.SubprocessPermit. allowed false is the
// default a bare context yields — absence of a permit is refusal, not permission — and it skips
// every external rung of the ladder.
type spawnGate struct {
	allowed     bool
	confinement *domain.Confinement // nil = the permit authorises an unfenced spawn
	timeout     time.Duration
	secretEnv   []string // operator-declared credential names the child must not inherit
}

// newAutofix builds the autofix Mechanism, resolving each external formatter's executable exactly
// once through security.ResolveProgram — deps.LookPath is the injected lookup (nil ⇒ exec.LookPath)
// and the resolver applies the exec fence in the same step, so the lookup cannot be had without it
// — and caching the resolved paths into the per-language repair ladder: the sim's LookPath-cached
// formatter table, injected at construction per D3 so fires never touch PATH. An absent or refused
// executable simply leaves its rung out; Go's in-process gofmt tail is always appended.
func newAutofix(deps Deps) (any, error) {
	// The fence the construction probe judges a formatter against, and the same root the spawn
	// door re-judges argv[0] with at fire time.
	workspaceRoot := deps.WritableBox.WorkspaceRoot

	resolved := map[string]string{} // command → path ("" = absent); each command probed once
	probe := func(command string) string {
		path, done := resolved[command]
		if !done {
			// A formatter that resolves inside the writable box — or that PATH answers
			// with a relative entry — is treated exactly as an absent one, its rung left
			// out of the ladder, which is why every error the resolver returns collapses
			// to the same "". The refusal has to sit HERE, at the single construction-time
			// resolution, rather than at the spawn: a permit may authorise an unfenced
			// spawn (nil Confinement on a host with no confinement backend), which is
			// precisely the run that must not execute bytes the model wrote. Autofix has no
			// result surface to explain a refusal on, and a skipped rung is its documented
			// degradation, so the fence is applied silently.
			p, err := security.ResolveProgram(deps.LookPath, command, workspaceRoot, &deps.WritableBox)
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
		repairs[entry.language] = append(repairs[entry.language], repairer{
			external: true,
			run: func(ctx context.Context, gate spawnGate, content string) (string, bool) {
				return runExternalFormatter(ctx, cmdPath, spec, workspaceRoot, gate, content)
			},
		})
	}

	// Go's always-available tail: the in-process gofmt (go/format), keeping the sim's
	// goimports → gofmt ladder shape with no external dependency (standing requirement #2).
	// It cannot repair what the parser cannot read — that content stays for syntax to correct.
	// Spawning nothing, it is not external, so no permit gates it.
	repairs["go"] = append(repairs["go"], repairer{
		run: func(_ context.Context, _ spawnGate, content string) (string, bool) {
			out, err := format.Source([]byte(content))
			if err != nil {
				return content, false
			}
			return string(out), true
		},
	})

	return autofixMechanism{
		repairs:   repairs,
		timeout:   defaultFormatterTimeout,
		secretEnv: deps.SecretEnvVars,
	}, nil
}

// autofixDescriptor identifies autofix as a strikes-3 response-repair Mechanism (catalogue Table A).
var autofixDescriptor = domain.MechanismDescriptor{
	ID:          autofixID,
	Capability:  domain.CapResponseRepair,
	Suppression: domain.SuppressStrikesThree,
}

// PostResponse attempts a repair of every syntax-broken write tool call, writing each improved
// payload back to the call the loop will dispatch. The decision is ActionIntercept when at least
// one call was repaired (the response was altered in place), and a no-op decision otherwise —
// autofix never defers or retries: correcting the remainder is syntax's job, one stage later.
func (m autofixMechanism) PostResponse(ctx context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	gate := m.permitGate(ctx)
	changed := false
	for i, call := range resp.ToolCalls() {
		if !isWriteTool(call.Tool) {
			continue
		}
		path, content, ok := writePathContent(call.Arguments)
		if !ok {
			continue
		}
		fixed, did := m.attemptFix(ctx, gate, path, content)
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

// permitGate reads the hook-time subprocess permit ONCE per fire and freezes the answer for the
// whole pass. Absence is refusal (domain.SubprocessPermit): no permit means allowed stays false and
// every external rung is skipped, leaving only the in-process tail.
func (m autofixMechanism) permitGate(ctx context.Context) spawnGate {
	permit, granted := domain.SubprocessPermitFromContext(ctx)
	return spawnGate{
		allowed:     granted,
		confinement: permit.Confinement,
		timeout:     m.timeout,
		secretEnv:   m.secretEnv,
	}
}

// attemptFix ports the sim's AttemptFix (internal/autofix/autofix.go @pin): act only on a sane
// path whose content is syntax-broken, run the language's ladder in order, and keep the FIRST
// output that reduces the issue count. fixed is false when nothing improved the content — the
// caller leaves the payload untouched and syntax corrects the remainder one stage later.
func (m autofixMechanism) attemptFix(ctx context.Context, gate spawnGate, path, content string) (fixed string, did bool) {
	if !sanitizePath(path) {
		return content, false
	}
	lang := syntaxcheck.Language(path)
	if lang == "" {
		return content, false
	}
	original := syntaxcheck.Check(path, content)
	if original.Valid {
		return content, false
	}
	for _, repair := range m.repairs[lang] {
		// No permit, no spawn: an unauthorised external rung is skipped before a command is ever
		// built, rather than attempted and failed.
		if repair.external && !gate.allowed {
			continue
		}
		out, ok := repair.run(ctx, gate, content)
		if !ok {
			continue
		}
		if len(syntaxcheck.Check(path, out).Errors) < len(original.Errors) {
			return out, true
		}
	}
	return content, false
}

// sanitizePath is the sim's path guard (internal/autofix/autofix.go sanitizePath @pin): refuse a
// write path carrying NUL or CR/LF control characters before it can reach the repair path at all.
// No formatter argv carries the path any more (the one formatter that keyed on the filename is
// gone), so this is now purely attemptFix's entry guard — but it is the guard that keeps a
// hostile path out of every downstream consumer, including the syntax checker's language sniff.
func sanitizePath(path string) bool {
	return path != "" && !strings.ContainsAny(path, "\x00\n\r")
}

// formatterSpec describes an external formatter: the command to run and the args that make it
// read the payload from stdin. No spec interpolates the file path into its argv — the remaining
// formatters read only DATA configuration from the repo, never repo-authored code.
type formatterSpec struct {
	command string
	args    []string
}

// externalFormatters is the sim's formatter registry (internal/autofix/formatters.go @pin) minus
// gofmt — Go's always-available tail is the in-process go/format the constructor appends — and
// minus the two JavaScript/TypeScript rungs, dropped in the Wave-3 audit fix: that formatter
// `require()`s repo-authored JavaScript from the workspace it is formatting, which makes running
// it arbitrary code execution rather than a data-driven reformat. The three that remain read
// declarative config only. Slice order is the sim's ladder order, and construction probes each
// command exactly once.
var externalFormatters = []struct {
	language string
	spec     formatterSpec
}{
	{"go", formatterSpec{command: "goimports"}},
	{"python", formatterSpec{command: "black", args: []string{"-", "--quiet"}}},
	{"rust", formatterSpec{command: "rustfmt"}},
}

// runExternalFormatter runs the construction-resolved formatter at cmdPath over content via
// stdin, returning the formatted output. It spawns through internal/tools' subprocess funnel
// (tools.RunHookSubprocess) rather than an exec.Command of its own, so the one subprocess this
// package launches carries every protection the execution tools' do: apogee's API key and the
// operator-declared key variables (gate.secretEnv) are scrubbed out of the child environment, the
// process TREE is torn down when the run ends, the output is capped, and the timeout is clamped.
// The FIRE's ctx bounds the run — a user cancel stops an in-flight formatter instead of leaving the
// loop waiting on it — narrowed by gate.timeout.
//
// workspaceRoot is the construction-time fence (deps.WritableBox.WorkspaceRoot) handed on to the
// door so it re-judges argv[0] at spawn time: the construction probe answered before the run, and
// what is on disk at that path may have changed since.
//
// The permit's box reaches the funnel the way every subprocess site names one: on the context
// (confinement-execution-contract §2.2). A permit carrying no box authorises an unfenced spawn, so
// nothing is installed and the funnel runs the command as-is; a box with no Confiner behind it is
// broken wiring and the funnel refuses the run. ok is false when the box cannot be established,
// when the subprocess fails or times out, or when it produced empty output: every failure mode
// degrades silently to "leave the payload as-is" (standing requirement #2), and a refused
// confinement NEVER degrades to running unconfined.
func runExternalFormatter(ctx context.Context, cmdPath string, spec formatterSpec, workspaceRoot string, gate spawnGate, content string) (string, bool) {
	if conf := gate.confinement; conf != nil {
		ctx = domain.WithConfinement(ctx, *conf)
	}

	argv := append([]string{cmdPath}, spec.args...)
	out, err := tools.RunHookSubprocess(ctx, argv, "", workspaceRoot, gate.secretEnv, gate.timeout, content)
	if err != nil || strings.TrimSpace(out) == "" {
		return content, false
	}
	return out, true
}
