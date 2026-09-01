package agent

// The engine-owned orientation block: the part of the standing system content (loop.go's
// standingSystem) that rides DIRECTLY AFTER the rendered prompt template and AHEAD of the
// workspace context files' blocks. It states the host facts a model needs to get oriented —
// where the workspace is, where its own writable scratch dir is, and which read-only roots it
// may reach — as harness text the engine composes itself, so no edit to the user-editable
// prompt template can lose them and no install seeded before the facts existed is left without
// them. Where the host offers the model a Delegation seat (ADR 0069) it states that too — what
// each of the two seats IS — because a choice between two opaque labels is not a choice.
//
// Position is a SECURITY property, not a matter of taste: the block is plain text and a
// workspace context file is repo-controlled prose. With the blocks ahead of it, a hostile
// AGENTS.md could open with a forged copy of this block naming its own paths and the real one,
// arriving after, would read as a correction of the forgery rather than the other way round.
// Riding first means no workspace text ever precedes the engine's own facts (F-19); the fence
// contextBlocks applies to the content below is the other half of the same guard.
//
// It RIDES ALONG (ADR 0023 §6 amendment, 2026-08-25): standingSystem appends it only when a
// standing system message exists anyway, never on its own, so the documented "send no system
// prompt" configuration stays byte-identical on the wire.

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/tools"
)

// The orientation asset (prompts/orientation.txt) is POSITIONAL: line 0 is the header and every
// line after it is one bullet. Each rendered bullet is a template carrying exactly one %s — the
// path or paths it names, or the Delegations bullet's rendered seat clauses; the context-files
// bullet is a literal line with no verb at all — it names a header shape rather than a path. The
// constants below are those line numbers, and orientationLineCount is the shape the loader
// enforces — a bullet added to the asset without a constant beside it fails the build's first test
// run rather than rendering as a stray line.
//
// The context-files bullet stays LAST whatever is added, because it is the only one that speaks
// about the content following the block rather than about the host: a bullet between it and those
// blocks would separate the bridge from what it bridges to.
const (
	orientationHeaderLine = iota
	orientationWorkspaceLine
	orientationScratchLine
	orientationRootsLine
	orientationDelegationsLine
	orientationContextFilesLine
	orientationLineCount
)

// orientationTemplate is the embedded asset split into its header and bullet templates. It is a
// build-time constant in everything but name: mustPrompt panics on a missing asset and the
// loader panics on an unexpected shape, both programming errors go:embed makes unreachable in a
// built binary.
var orientationTemplate = mustOrientationTemplate()

// mustOrientationTemplate loads prompts/orientation.txt and splits it into its lines, panicking
// unless it carries exactly one line per constant above — the header plus every bullet they name.
func mustOrientationTemplate() []string {
	lines := strings.Split(mustPrompt("orientation.txt"), "\n")
	if len(lines) != orientationLineCount {
		panic(fmt.Sprintf("apogee: prompts/orientation.txt has %d lines, want %d "+
			"(header + one template per orientation bullet)", len(lines), orientationLineCount))
	}
	return lines
}

// orientationHeader returns the block's header line: the one line a workspace file would have to
// spell to pass its own prose off as the engine's orientation. It is reachable outside this file
// because contextBlocks (contextfiles.go) fences context-file content against it — the two halves
// of the anti-forgery guard have to name the same string, and this is where it lives.
func orientationHeader() string { return orientationTemplate[orientationHeaderLine] }

// orientationBlock renders this request's orientation block, or "" when there is no fact to
// state. Every input is read FRESH per request — the workspace from Config, the scratch dir
// through the lock-guarded ScratchDir(), the read roots through the live Config.ExtraReadRoots
// func — so a session boundary that moves the scratch dir or a host that remounts its read
// roots is honoured by the next request with no re-wiring.
//
// A fact the session does not have is OMITTED rather than rendered empty: no scratch dir until
// the host has actually created one (CONTEXT.md: "advertised writable only once it actually
// exists"), no library line without roots (Config.ExtraReadRoots is nil ⇒ workspace-only, so
// the func itself is nil-guarded), no workspace line for a Driver that scopes the engine to
// none, and no context-files line for a session that loaded none. With every bullet omitted the
// header would stand alone saying nothing, so the block is "" instead and standingSystem
// appends nothing.
//
// The last bullet is the one that speaks about what follows the block rather than about the
// host: it names the header the workspace blocks ride under and says they are project text, so
// the fenced content below cannot be read as more harness facts.
//
// KV cache: every input is a per-session constant — the workspace and the roots are the host's
// wiring, the scratch dir moves only at a session boundary, the context-file cache is refilled
// only at one too (ADR 0026 §5), and the Delegation seats move only on the human's own `/server`,
// `/model` and `/sub-agents-server` doors — so the block is prefix-cache-stable for the life of
// a session, exactly like the {{scratch}} placeholder it stands beside.
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
	if seats := a.delegationSeats(); seats != "" {
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationDelegationsLine], seats))
	}
	if a.hasContextBlocks() {
		bullets = append(bullets, orientationTemplate[orientationContextFilesLine])
	}
	if len(bullets) == 0 {
		return ""
	}
	return orientationTemplate[orientationHeaderLine] + "\n" + strings.Join(bullets, "\n")
}

// delegationSeats renders the clause list the Delegations bullet states — what each value of the
// sub_agent tool's `run_on` argument means in this session, and which one an absent value equals
// (ADR 0069) — or "" when the bullet is not rendered at all.
//
// The ROSTER is the gate, and the only one: the bullet is rendered exactly when this Agent's own
// sub_agent tool published `run_on`, so the model is told about a choice precisely when it has one.
// A child's tool is the plain variant (withoutSeatChoice) and so is every tool built under
// `sub-agents-choice: fixed`, which is why nothing here needs a depth check or a flag of its own —
// asking the published schema is what keeps the prompt and the tool menu from ever disagreeing.
//
// A seat with nothing to say is DROPPED rather than rendered empty, the block's rule everywhere:
// a session whose host names no server describes no session seat, and a session with no Sub-agent
// server installed names only the near one and says an unset `run_on` stays there. With neither
// seat describable there is no clause to state and the bullet is omitted entirely — the schema's
// two values are still legal, and item 11's result note is what tells a parent where its work
// actually ran.
func (a *Agent) delegationSeats() string {
	if !publishesSeatChoice(a.tools) {
		return ""
	}
	// The session seat is the Upstream this Agent is bound to, read from the live Config the way
	// every request reads the model it sends: a `/server` switch moves both together
	// (SwitchUpstream), so the line can never name the retired box.
	clauses := make([]string, 0, 3)
	unset := tools.RunOnSession
	if session := describeDelegationSeat(a.cfg.Model, a.cfg.ServerName, a.cfg.ServerDescription); session != "" {
		clauses = append(clauses, fmt.Sprintf("run_on %q = %s", tools.RunOnSession, session))
	}
	if seat := a.subAgentsSeat(); seat != nil {
		if far := describeDelegationSeat(seat.Model, seat.Name, seat.Description); far != "" {
			clauses = append(clauses, fmt.Sprintf("run_on %q = %s", tools.RunOnSubAgentsServer, far))
			unset = tools.RunOnSubAgentsServer
		}
	}
	if len(clauses) == 0 {
		return ""
	}
	return strings.Join(append(clauses, "unset = "+unset), "; ")
}

// describeDelegationSeat renders ONE seat as `<model> on <entry name> — <description>`, dropping
// whichever part the host did not supply and returning "" when it supplied none. The shape is the
// same for both seats deliberately: a model comparing them is comparing like with like, and a form
// that described the far seat differently would read as a difference in kind rather than in box.
//
// What it never carries is AVAILABILITY. Every part is a per-session constant the human wrote or
// switched to, so the rendered line is stable for the life of a binding (ADR 0023 §6); a seat that
// is momentarily unreachable is reported to the parent model by its delegation's own result note,
// where it can still act on it, rather than by a standing prompt it would have to re-read.
func describeDelegationSeat(model, name, description string) string {
	seat := model
	switch {
	case seat == "":
		seat = name
	case name != "":
		seat += " on " + name
	}
	if seat == "" {
		return ""
	}
	if description != "" {
		seat += " — " + description
	}
	return seat
}
