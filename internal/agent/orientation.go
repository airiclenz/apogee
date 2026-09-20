package agent

// The engine-owned orientation block: the part of the standing system content (loop.go's
// standingSystem) that rides DIRECTLY AFTER the rendered prompt template and AHEAD of the
// workspace context files' blocks. It states the host facts a model needs to get oriented —
// where the workspace is, where its own writable scratch dir is, which read-only roots it may
// reach and, in Plan, which tool families the mode withholds — as harness text the engine
// composes itself, so no edit to the user-editable
// prompt template can lose them and no install seeded before the facts existed is left without
// them. Where the model holds the sub_agent tool it states the delegation bounds — the width, the
// fan-out ceiling and the step cap the engine enforces — so the first call is composed against the
// numbers rather than discovered from a refusal; and where the host offers the model a Delegation
// seat (ADR 0069) it states that too — what each of the two seats IS — because a choice between
// two opaque labels is not a choice.
//
// Position is a SECURITY property, not a matter of taste: the block is plain text and a
// workspace context file is repo-controlled prose. With the blocks ahead of it, a hostile
// AGENTS.md could open with a forged copy of this block naming its own paths and the real one,
// arriving after, would read as a correction of the forgery rather than the other way round.
// Riding first means no workspace text ever precedes the engine's own facts (F-19); the fence
// contextBlocks applies to the content below is the other half of the same guard.
//
// Its position, its fence and the fact that it RIDES ALONG (ADR 0023 §6 amendment, 2026-08-25)
// are one row of the standingBlocks table (standingblocks.go).

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// The orientation asset (prompts/orientation.txt) is POSITIONAL: line 0 is the header and every
// line after it is one bullet. Each rendered bullet is a template carrying exactly one %s — the
// path or paths it names, the Delegation bounds bullet's rendered clauses, the Delegations
// bullet's rendered seat clauses, or the Mode bullet's scratch-writers clause; the context-files
// bullet is a literal line with no verb at all — it names a header shape rather than a path. The
// constants below are those line numbers, and orientationLineCount is the shape the loader
// enforces — a bullet added to the asset without a constant beside it fails the build's first test
// run rather than rendering as a stray line.
//
// The context-files bullet stays LAST whatever is added, because it is the only one that speaks
// about the content following the block rather than about the host: a bullet between it and those
// blocks would separate the bridge from what it bridges to. The rule binds BULLETS, inside this
// asset. One engine-owned block is ratified into the gap between this block and those files' —
// the delegate report block (delegatereport.go), on delegations only (owner, 2026-09-02) — and
// the bridge survives it: the context files still follow, and the block changes no host fact.
const (
	orientationHeaderLine = iota
	orientationWorkspaceLine
	orientationScratchLine
	orientationRootsLine
	orientationDelegationBoundsLine
	orientationDelegationsLine
	orientationPlanLine
	orientationContextFilesLine
	orientationLineCount
)

// The Delegation bounds bullet's clauses (delegationBounds), each carrying exactly one number: the
// stated width, the fan-out ceiling, and the delegate step cap. The group clause carries none — it
// is the standing fact that a pooled reply's results arrive together (ADR 0039), stated beside the
// numbers because it is what makes the ceiling a bound on blind waiting rather than on work.
const (
	delegationWidthClause   = "up to %d run at once"
	delegationCeilingClause = "a reply may fan out at most %d — calls past that are refused and must be delegated again"
	delegationGroupClause   = "a reply's whole group returns together"
	delegationStepCapClause = "each delegate is capped at %d Turns (a max_steps above that is clamped)"
)

// planScratchWritersClause is the Mode bullet's one %s: the clause stating that Plan runs Apogee's
// own writers on the session scratch dir (ADR 0012 second loosen), rendered exactly when a scratch
// dir is set — the same condition planOffers puts those writers on the menu under — and dropped
// otherwise, so the bullet never offers a target the menu does not.
const planScratchWritersClause = ", plus Apogee's own writers into the session scratch dir only"

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
// The scratch bullet is stated in EVERY mode, Plan included: the session scratch dir is the one
// target Plan runs Apogee's own writers on and the one write Ask-Before does not gate (ADR 0012
// second loosen, 2026-09-14), so a directory the block calls "writable" is writable on every rung
// of the ladder — the announced-path regression the 2026-09-14 Plan gate existed to avoid cannot
// arise, and the gate is gone with it.
//
// The live mode IS an input of this block, for one bullet: in Plan, and only in Plan, the Mode
// bullet says what the menu withholds — the subprocess and network families (terminal, run_tests,
// python_exec, web_fetch, web_search, http_request) and MCP tools — and asks the model to report
// what it would run, because the prompt template tells every mode to verify by running the
// project's tests and a Plan model that was told nothing else would reach for a tool it cannot
// see. The bullet is worded from the live menu: its writers clause rides exactly when the menu
// offers the scratch-dir writers (planOffers's scratchSet), and the families it names are the
// classes planOffers never admits (contract §4), so the announcement and the menu cannot disagree
// — the e2e in cmd/apogee holds the two against each other. Every other mode renders no Mode
// bullet: their menus withhold nothing the template promises.
//
// The last bullet is the one that speaks about what follows the block rather than about the
// host: it names the header the workspace blocks ride under and says they are project text, so
// the fenced content below cannot be read as more harness facts. On a delegation the delegate
// report block (delegatereport.go) sits between this block and those headers; both of the
// bullet's clauses stay true across it — the workspace blocks still follow under those headers,
// and an engine-owned block changes none of the facts above.
//
// KV cache: every input moves only on a session-level door — the workspace and the roots are
// the host's wiring (the roots settle once, when the host's off-boot toolchain probe answers,
// which is one early re-encode and never a per-turn one), the scratch dir moves only at a
// session boundary, the context-file cache is
// refilled only at one too (ADR 0026 §5), the Delegation seats move only on the human's own
// `/server`, `/model` and `/sub-agents-server` doors, the Delegation bounds' width is LATCHED per
// seat (statedDelegationWidth) and so moves only on those same doors and on a heartbeat's slot
// discovery — a cap's first statement, never a flap; its rounds and step cap are Config — and the
// mode moves only on the human's own Shift+Tab — so the block is prefix-cache-stable between
// those doors, exactly like the {{scratch}} and {{mode}} placeholders it stands beside. The mode re-joined the block's inputs
// on 2026-09-15 (it had gated the scratch bullet for one day on 2026-09-14, from the Plan omission
// to the second loosen, and then left again): a flip into or out of Plan re-encodes the prefix
// from the Mode bullet down, which is the re-encode {{mode}} already pays for on the same
// keypress — the block adds no door of its own.
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
	if bounds := a.delegationBounds(); bounds != "" {
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationDelegationBoundsLine], bounds))
	}
	if seats := a.delegationSeats(); seats != "" {
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationDelegationsLine], seats))
	}
	if a.Mode() == domain.ModePlan {
		writers := ""
		if a.ScratchDir() != "" {
			writers = planScratchWritersClause
		}
		bullets = append(bullets, fmt.Sprintf(orientationTemplate[orientationPlanLine], writers))
	}
	if a.hasContextBlocks() {
		bullets = append(bullets, orientationTemplate[orientationContextFilesLine])
	}
	if len(bullets) == 0 {
		return ""
	}
	return orientationTemplate[orientationHeaderLine] + "\n" + strings.Join(bullets, "\n")
}

// delegationBounds renders the clause list the Delegation bounds bullet states — the numbers the
// engine enforces on this agent's delegations, told to the model before its first sub_agent call
// so a reply is composed against them rather than against a refusal — or "" when the bullet is
// not rendered at all.
//
// The ROSTER is the gate: the bullet is rendered exactly when this Agent's own tool set holds
// sub_agent, whichever variant — a delegate below the depth bound holds the plain one and is told
// its own bounds (width 1, ceiling `rounds`), a delegate AT the bound holds none and is told
// nothing, since a bound on a tool it cannot call is noise. A nil roster is a tool-less Agent and
// reports nothing, as publishesSeatChoice does.
//
// The clauses, in order: the width (always — a group runs at least one at a time), the fan-out
// ceiling (only with `delegate-fanout-rounds` on — 0 is no ceiling and no clause), the
// returns-together fact (always), and the step cap (only with `delegate-max-steps` on — 0 is
// unbounded and no clause). The width and the ceiling read the same seams dispatch enforces by —
// statedDelegationWidth and fanOutCeiling — so what the model is told is exactly what
// refusePastCeiling lets through, and a clause with nothing to bound is DROPPED rather than
// rendered as a zero, the block's rule everywhere.
func (a *Agent) delegationBounds() string {
	if a.tools == nil {
		return ""
	}
	if _, ok := a.tools.Lookup(tools.SubAgentToolName); !ok {
		return ""
	}
	clauses := make([]string, 0, 4)
	clauses = append(clauses, fmt.Sprintf(delegationWidthClause, a.statedDelegationWidth()))
	if ceiling := a.fanOutCeiling(); ceiling > 0 {
		clauses = append(clauses, fmt.Sprintf(delegationCeilingClause, ceiling))
	}
	clauses = append(clauses, delegationGroupClause)
	if steps := a.cfg.Delegation.MaxSteps; steps > 0 {
		clauses = append(clauses, fmt.Sprintf(delegationStepCapClause, steps))
	}
	return strings.Join(clauses, "; ")
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
