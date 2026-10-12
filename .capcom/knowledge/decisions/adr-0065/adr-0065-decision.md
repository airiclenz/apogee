---
title: ADR 0065 decision
---

**1. Four skills ship embedded in the binary, as the LOWEST-priority source.** (Amended
2026-09-30: a fifth, `audit`, ships too — five in all.) `debugging`,
`planning`, `code-review`, `commit-hygiene` — each a directory of bytes compiled in with
`go:embed`, the built-in color schemes' pattern
(ADR 0040 §1) — merged into the
catalog **beneath** all three disk sources. The user's global library still wins any cross-source id
clash (ADR 0032), and now so does either workspace dir: a shipped skill is the weakest claim on an
id in the system, so nobody who has written their own `planning` skill loses it, and the shadowing
is recorded through the same skip channel ADR 0032 built, so `/skills` names both sides.

> **Amended 2026-09-27 (ADR 0087 D6).** The shipped set is five skills: `audit` joins the four above as a
> Recipe — a skill whose header declares inputs and a stage list the engine runs as a Workflow —
> with its split script bundled beside its `SKILL.md`. Every rule of this section binds it
> unchanged: it is the lowest-priority claim on its id, a user's own `code-audit` skill is
> untouched, and a user's own `audit` shadows it.

> **Amended 2026-10-10 (ADR 0087 D6, ADR 0042 §2–§3).** `audit` no
> longer bundles a split script: its `split` stage runs apogee's own hidden `apogee audit-split`
> subcommand through the `{apogee}` placeholder, so the recipe needs no `sh` or coreutils and
> runs on Windows. Its folder carries only `SKILL.md` and its prompts; the rules above bind it
> unchanged.

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
ADR 0057's roster axis). It is **not a Mechanism**: nothing about it is
catalogued, gated by bench evidence, or switched off by `--bypass`
(ADR 0006) — ADR 0002's line between an open tool
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
