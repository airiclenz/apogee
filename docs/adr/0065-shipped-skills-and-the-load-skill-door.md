---
Status: accepted
---

# Shipped skills and the load_skill door

## Context

Skills are an **open extension point**: apogee discovers folders holding a `SKILL.md` from three
layered dirs and ships none of its own
([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md),
[ADR 0032](0032-the-user-skill-library-outranks-the-workspace.md)). `internal/skills/doc.go` states
that as a decision and attributes it to
[ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s open-extension-point
posture — "no builtins shipped".

Two facts have accumulated against it.

**A fresh install has an empty catalog.** `/skills` lists nothing, the `/` menu offers nothing, and
the suggestion band ([ADR 0061](0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md))
has nothing to suggest — every one of those surfaces is dead until the user writes a skill folder by
hand. The features exist; the content that makes them worth anything does not, and the audience
least likely to author it is the one apogee is built for.

**The guidance that would go in one has nowhere else to live.**
[ADR 0064](0064-the-system-prompt-ships-an-embedded-default.md) gives *standing* steering a
maintained home in the binary and, in its §5 placement rule, sends **task-shaped** instruction — the
paragraphs you want while debugging and not while writing a commit message — to a Skill instead.
That rule is unusable while no skill can ship.

**And discovery stops at the human.** ADR 0061 Decision 4 named two model-facing shapes and built
neither: **B1**, auto-attaching a body to a message carrying no `/id`; **B2**, a model-callable
`load_skill` tool. B2 was deferred on two grounds — that it needed a Mechanism's gating-and-bench
apparatus, and that it "contradicts CONTEXT.md's *Skill* entry directly (*Avoid*: 'tool')". The
first ground was wrong on inspection: a tool is not a Mechanism (ADR 0002), and the deferral
inherited B1's argument rather than making its own. The second is a real domain-language question,
answered in §6 below. That deferral required an **explicit** supersession, which this record is.

This ADR records the design the owner ratified on 2026-08-31, implemented by
`docs/plans/2026-08-31 - 00 - base-guidance-and-shipped-skills-plan.md`. It **supersedes ADR 0061
Decision 4's B2 deferral only**; B1 stays deferred on its own terms.

## Decision

**1. Four skills ship embedded in the binary, as the LOWEST-priority source.** `debugging`,
`planning`, `code-review`, `commit-hygiene` — each a directory of bytes compiled in with
`go:embed`, the built-in color schemes' pattern
([ADR 0040](0040-color-schemes-are-embedded-roles-with-user-shadowing.md) §1) — merged into the
catalog **beneath** all three disk sources. The user's global library still wins any cross-source id
clash (ADR 0032), and now so does either workspace dir: a shipped skill is the weakest claim on an
id in the system, so nobody who has written their own `planning` skill loses it, and the shadowing
is recorded through the same skip channel ADR 0032 built, so `/skills` names both sides.

**2. Shipped skills are never installed.** No `~/.apogee/skills/<id>/` is written on first run and
no directory is auto-created. The catalog serves the embedded bytes directly, and an upgrade
therefore refreshes every shipped skill for every user — the freeze ADR 0064 §1 removed from the
default prompt, removed here for the same reason and by the same mechanism.

**3. Bundled files are served through a virtual read mount.** A shipped skill may carry files beside
its `SKILL.md` — a checklist, a reference table — which its body points the model at. Those files
have **no host path**, so `Config.ExtraReadRoots` (host directories, real paths) cannot reach them:
the read-only file tools instead resolve a reserved `shipped:` namespace against the embedded tree.
The mount is **read-only by construction** — the write fence names `shipped:` as a refusal, not a
permission question — because there is nothing behind it to write to.

**4. `use-shipped-skills` mirrors `use-project-skills`.** A bool, default on, that switches the
whole embedded source off. It is the same shape, in the same place, with the same reading — a source
toggle, not a per-skill one — so a user who wants only their own library sets one key, and the
lever's absence never means "some shipped skills".

**5. `/skills` gains a source label and an export verb.** Bare `/skills` lists the catalog with each
row labelled by where it came from, shipped included, so the answer to "why does this skill say
that?" is on the row. `/skills export <id>` copies a shipped skill's directory to
`~/.apogee/skills/<id>/` and **refuses to overwrite** an existing directory — the one supported way
to fork a shipped skill, after which rung 1 of §1 makes the copy win by the ordinary rule. Export is
the exception to the verb's while-busy promise: the listing still runs mid-run, the export form does
not.

**6. `load_skill` is a default-on tool with an adaptive single-call shape.** One tool, not a
list/fetch pair, because two calls to reach one body is a round trip a small model spends badly.
What comes back adapts to what the argument matched:

- an **exact id** returns that skill's body;
- a **confident** match — the ADR 0061 matcher's ranking and evidence gate, reused — returns the
  winner's body *and* names the other ids that matched, so the model can see what it did not get;
- anything less returns **candidates as id + summary only**, no bodies, leaving the model to call
  once more with an id it can now spell.

The tool is **default-on** and rides the ordinary tool lever (`tools.enabled` / `tools.disabled`,
resolved per model as
[ADR 0057](0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md)'s roster axis). It is **not a Mechanism**: nothing about it is
catalogued, gated by bench evidence, or switched off by `--bypass`
([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)) — ADR 0002's line between an open tool
surface and a curated Mechanism set is the line it falls on, and ADR 0061 Decision 4 put it on the
wrong side by inheriting B1's argument.

**7. The catalog still never enters the standing prompt, and a skill is still prompt text.** What
the model gains is a **door**, not a listing: `load_skill` appears in the tool menu like any other
tool, and no skill id, description or body is added to the system prompt because the catalog exists.
ADR 0061 Decision 2's property — that nothing about the catalog reaches the model unbidden — is
therefore intact for the standing prompt, and narrowed only where the model itself asks.
CONTEXT.md's *Skill* entry keeps its "*Avoid*: 'tool'": a skill remains prompt text that steers and
adds no capability; `load_skill` is a tool that **fetches** that text, the way `read_file` is a tool
that fetches a file without a file being a tool. The distinction the Avoid line protects is
unharmed; what changes is that a body can now enter a turn by the model's request as well as the
user's `/token`.

**8. B1 stays deferred.** Auto-attaching a body to a message that carries no `/id` puts prompt text
in front of the model that the user did not ask for. That is a Mechanism in the exact sense of ADR
0003 — it changes the model's input on apogee's initiative — and must be catalogued, gated per model
and measured against the Bypass floor before it can ship. Building it still requires an ADR that
supersedes ADR 0061 Decision 4 explicitly, and this record is not it.

## Considered options

- **Keep shipping no skills and document how to write one** — rejected. It leaves every skill
  surface empty on a fresh install and leaves ADR 0064 §5's placement rule with nowhere to send
  task-shaped guidance.
- **Install the four skills into `~/.apogee/skills/` on first run** (the `seedConfig` pattern) —
  rejected on ADR 0064's evidence: a seeded copy is frozen per install and never improves, and it
  would additionally make shipped skills *outrank* the workspace by landing in the highest-priority
  dir, inverting §1.
- **Ship them at the TOP of the priority order** so the user gets apogee's version by default —
  rejected. ADR 0032 settled that a user's own library outranks content they did not write; content
  apogee wrote is no different, and silently replacing a hand-written `planning` skill is the exact
  failure ADR 0032 was opened for.
- **Extract bundled files to a temp dir and mount it as a real read root** — rejected. It writes
  bytes nobody asked for, needs a lifetime and a GC, and makes the read root's contents differ per
  run; a virtual namespace has none of those and is refused for writes by construction.
- **A `list_skills` + `get_skill` tool pair** — rejected by §6: it is two round trips for one
  answer, and the adaptive return gives a model that guessed a name a usable body on the first call.
- **`load_skill` as a Mechanism** — rejected by §6. It adds a tool, not a directive; there is no
  standing text for Bypass to remove and no arm for a bench to measure, since the model's input
  changes only when the model itself calls.
- **`load_skill` default-OFF, opted into per model** — rejected. Default-off tools are for
  capabilities with a blast radius; this one returns text from a catalog the user already installed,
  and `tools.disabled` is there for anyone who disagrees.
- **B1 auto-attach, shipped alongside** — deferred by §8, unchanged from ADR 0061.

## Consequences

- **`internal/skills` stops being builtin-free, and its doc comment's attribution moves here.**
  `doc.go`'s "no builtins shipped — ADR 0002" claim is superseded by §1: ADR 0002 makes tools an
  open extension point, which is an argument for allowing user skills, never one against shipping
  any. The open point is untouched — a shipped skill occupies no privileged position and can be
  shadowed by a folder any user writes.
- **The suggestion band works out of the box.** ADR 0061's matcher indexes the shipped skills like
  any others, so a fresh install gets suggestions on its first draft — the feature's first real
  exercise, and a standing check on the four skills' `triggers:` being written honestly.
- **A shipped body is apogee's text in front of the model, on every upgrade.** Same exposure as ADR
  0064's default prompt and the orientation block, and the same obligation: a bad paragraph reaches
  everyone. The Bypass floor is not engaged — a skill body enters a turn only when invoked — but the
  quality bar is ADR 0064 §6's, not a lower one.
- **The model can now spend tokens on its own initiative.** A `load_skill` call and its body are
  charged to the Budget like any tool result and capped like one; a model that calls it every Turn
  is a roster problem, solved with `tools.disabled`, not a new gate.
- **CONTEXT.md's *Skill* entry gains the shipped source, the `/skills` verb's export form and
  `load_skill`**, keeps its "*Avoid*: 'tool'" with §7's distinction attached, and its
  layered-discovery sentence now names four sources rather than three.
