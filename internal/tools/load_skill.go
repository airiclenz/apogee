package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// loadSkillSpec is the door onto the skill catalog (ADR 0065 §6). The description is ONE line and
// names no skill: what the catalog holds is the user's business and this build's, and putting ids
// or summaries in a tool description would put the whole catalog in every request — the thing
// ADR 0061 Decision 2 kept out of the standing prompt and ADR 0065 §7 keeps out still. The model
// learns what exists by asking.
var loadSkillSpec = toolSpec{
	name:        "load_skill",
	description: "Fetch the instructions for a skill — a written procedure for one kind of work, either apogee's own or one this user wrote — by its id, or by a few words describing the task when you do not know the id.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "A skill id, or a few words naming the task you want a procedure for (\"cut a release\", \"review this diff\"). An id returns that skill's instructions; words return the best match, or a short list of ids to pick from when no single skill clearly fits."}
  }
}`),
}

type loadSkillArgs struct {
	Query string `json:"query"`
}

// LoadSkill lets the model pull one skill's instruction text mid-Turn (ADR 0065 §6). It is the
// model-facing half of the same catalog the user reaches with a "/id" chat token: the user's token
// resolves through domain.SkillResolver at the loop, this searches through domain.SkillLookup at a
// tool call, and the host injects ONE catalog into both.
//
// One tool with an adaptive answer, not a list/fetch pair: two calls to reach one body is a round
// trip a small model spends badly, so an exact id returns a body immediately, a confident match
// returns a body plus the ids it beat, and anything less returns ids and summaries to call again
// with. The shape is the lookup's (internal/skills); this renders it.
//
// It is ReadOnly() — fetching prompt text writes nothing — so the disposition runs it in every
// mode, Plan included, which is where a model most wants a procedure before it touches anything.
// It is NOT an ExternalEffectTool: the catalog is in this process, and there is nothing to stub.
// It is default-ON and rides the ordinary roster lever (`tools.disabled:` / `tools.enabled:` and a
// profile's `tools:` axis, ADR 0057); it is not a Mechanism, and Bypass does not touch it.
//
// A nil SkillLookup means the tool is never registered (builtinTools omits it), so by construction
// Execute always has a non-nil lookup; the defensive nil-check keeps a hand-built registry that
// registers it anyway from panicking. Stateless across Turns (ADR 0008): one query, one answer, and
// nothing about a call survives into the next.
type LoadSkill struct {
	toolSpec
	lookup domain.SkillLookup
}

// NewLoadSkill returns a load_skill tool searching lookup. A nil lookup yields a tool whose Execute
// reports the catalog is unavailable (the registry omits it in practice), matching ask_user's
// graceful degradation rather than a panic on the first call.
func NewLoadSkill(lookup domain.SkillLookup) *LoadSkill {
	return &LoadSkill{toolSpec: loadSkillSpec, lookup: lookup}
}

// ReadOnly reports that load_skill performs no writes — reading instructions mutates nothing — so
// the disposition runs it freely in every mode, including Plan.
func (t *LoadSkill) ReadOnly() bool { return true }

// Execute searches the catalog for the query and returns the one answer it earned. Every outcome is
// a successful RESULT rather than a Go error: a miss is information the model can act on (ask again
// in other words, or carry on without a skill), not a fault that should roll the Turn back.
func (t *LoadSkill) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[loadSkillArgs](call)
	if !ok {
		return fail, nil
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return errorResult(call.ID, "query is required: name a skill id, or describe the task in a few words"), nil
	}
	if t.lookup == nil {
		return errorResult(call.ID, "load_skill is unavailable: no skill catalog is configured"), nil
	}

	return okResult(call.ID, renderSkillLookup(query, t.lookup.LookupSkill(query))), nil
}

// renderSkillLookup turns one lookup answer into the text the model reads.
//
// A found skill is rendered the way the loop renders an ATTACHED one (internal/agent's
// resolveSkillRefs): the same <skill: …> wrapper, the same files: line naming the folder its
// bundled resources live under, and the same {{SKILL_DIR}} expansion — so a body means the same
// thing whichever door it came through, and an instruction pointing at a bundled file names an
// address the read tools accept rather than a literal placeholder they refuse.
//
// The "also matched" line and the candidates rung sit OUTSIDE that wrapper: they are this tool
// answering, not the skill speaking, and a model that treats everything inside <skill> as
// instructions must not read a list of ids as one.
func renderSkillLookup(query string, res domain.SkillLookupResult) string {
	var b strings.Builder
	if res.Found {
		s := res.Skill
		fmt.Fprintf(&b, "<skill: %s>\n", s.DisplayName)
		body := s.Body
		if s.Dir != "" {
			fmt.Fprintf(&b, "files: %s — this skill's bundled files; read one (read_file, "+
				"list_dir, grep or find_files) or copy one out (copy_file) only when these "+
				"instructions call for it — use these tools, never terminal commands, to "+
				"touch this folder\n", s.Dir)
			body = strings.ReplaceAll(body, domain.SkillDirToken, s.Dir)
		}
		fmt.Fprintf(&b, "%s\n</skill>\n", body)
		if len(res.Also) > 0 {
			fmt.Fprintf(&b, "\nalso matched, not loaded: %s — call load_skill again with one of "+
				"these ids if this was not the skill you wanted.\n", strings.Join(res.Also, ", "))
		}
		return b.String()
	}

	if len(res.Candidates) == 0 {
		return fmt.Sprintf("no skill matches %q. Carry on without one, or call load_skill again "+
			"describing the task differently.", query)
	}

	fmt.Fprintf(&b, "no single skill clearly matches %q. These did match — call load_skill again "+
		"with one of these ids to load its instructions:\n", query)
	for _, cand := range res.Candidates {
		fmt.Fprintf(&b, "- %s: %s\n", cand.ID, cand.Summary)
	}
	return b.String()
}

var (
	_ domain.Tool         = (*LoadSkill)(nil)
	_ domain.ReadOnlyTool = (*LoadSkill)(nil)
)
