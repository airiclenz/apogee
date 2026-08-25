package agent

// The engine-owned orientation block: the third and LAST part of the standing system content
// (loop.go's standingSystem), after the rendered prompt template and the workspace context
// files' blocks. It states the host facts a model needs to get oriented — where the workspace
// is, where its own writable scratch dir is, and which read-only roots it may reach — as
// harness text the engine composes itself, so no edit to the user-editable prompt template can
// lose them and no install seeded before the facts existed is left without them.
//
// It RIDES ALONG (ADR 0023 §6 amendment, 2026-08-25): standingSystem appends it only when a
// standing system message exists anyway, never on its own, so the documented "send no system
// prompt" configuration stays byte-identical on the wire.

import (
	"fmt"
	"strings"
)

// The orientation asset (prompts/orientation.txt) is POSITIONAL: line 0 is the header and every
// line after it is one bullet template carrying exactly one %s for the path or paths it names.
// The constants below are those line numbers, and orientationLineCount is the shape the loader
// enforces — a bullet added to the asset without a constant beside it fails the build's first
// test run rather than rendering as a stray line.
const (
	orientationHeaderLine = iota
	orientationWorkspaceLine
	orientationScratchLine
	orientationRootsLine
	orientationLineCount
)

// orientationTemplate is the embedded asset split into its header and bullet templates. It is a
// build-time constant in everything but name: mustPrompt panics on a missing asset and the
// loader panics on an unexpected shape, both programming errors go:embed makes unreachable in a
// built binary.
var orientationTemplate = mustOrientationTemplate()

// mustOrientationTemplate loads prompts/orientation.txt and splits it into its lines, panicking
// unless it carries exactly the header plus the three bullet templates the constants name.
func mustOrientationTemplate() []string {
	lines := strings.Split(mustPrompt("orientation.txt"), "\n")
	if len(lines) != orientationLineCount {
		panic(fmt.Sprintf("apogee: prompts/orientation.txt has %d lines, want %d "+
			"(header + one template per orientation bullet)", len(lines), orientationLineCount))
	}
	return lines
}

// orientationBlock renders this request's orientation block, or "" when there is no fact to
// state. Every input is read FRESH per request — the workspace from Config, the scratch dir
// through the lock-guarded ScratchDir(), the read roots through the live Config.ExtraReadRoots
// func — so a session boundary that moves the scratch dir or a host that remounts its read
// roots is honoured by the next request with no re-wiring.
//
// A fact the session does not have is OMITTED rather than rendered empty: no scratch dir until
// the host has actually created one (CONTEXT.md: "advertised writable only once it actually
// exists"), no library line without roots (Config.ExtraReadRoots is nil ⇒ workspace-only, so
// the func itself is nil-guarded), and no workspace line for a Driver that scopes the engine to
// none. With every bullet omitted the header would stand alone saying nothing, so the block is
// "" instead and standingSystem appends nothing.
//
// KV cache: all three inputs are per-session constants — the workspace and the roots are the
// host's wiring, and the scratch dir moves only at a session boundary — so the block is
// prefix-cache-stable for the life of a session, exactly like the {{scratch}} placeholder it
// stands beside.
func (a *Agent) orientationBlock() string {
	bullets := make([]string, 0, orientationLineCount-1)
	if workspace := a.cfg.WorkspaceDir; workspace != "" {
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationWorkspaceLine], workspace))
	}
	if scratch := a.ScratchDir(); scratch != "" {
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationScratchLine], scratch))
	}
	if a.cfg.ExtraReadRoots != nil {
		if roots := a.cfg.ExtraReadRoots(); len(roots) > 0 {
			bullets = append(bullets, fmt.Sprintf(
				orientationTemplate[orientationRootsLine],
				strings.Join(roots, ", "),
			))
		}
	}
	if len(bullets) == 0 {
		return ""
	}
	return orientationTemplate[orientationHeaderLine] + "\n" + strings.Join(bullets, "\n")
}
