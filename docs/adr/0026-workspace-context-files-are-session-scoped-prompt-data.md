---
Status: accepted
---

# Workspace context files are discovered, session-scoped prompt DATA

## Context

[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) gave apogee a
system prompt: a template the user writes once in `~/.apogee/config.yaml` and apogee renders into
the first system message of every request. That prompt is **global** — it lives in the home
directory and travels with it — which is right for what it says: who apogee is, how you want to be
answered, which mode it is in.

It leaves the other half unsaid. Every repository has standing facts a coding agent needs before its
first useful move — where knowledge lives, what "done" means here, the conventions no file states
outright — and they belong to the **project**, not to the machine or to the person. The tooling
around apogee already settled on a convention for exactly this: a Markdown file at the repository
root, `AGENTS.md` (Claude Code's `CLAUDE.md`, the same shape), which agents fold into their standing
instructions. This repo keeps one. Without support for it, a user's only options are to paste the
project's conventions into a global prompt — where they follow apogee into every *other* project —
or to retype them into the first message of every session.

What makes this more than "read another file" is that the file is **not the user's own text**. A
global `system-prompt-file` is written by the person who configured apogee; a workspace `AGENTS.md`
is written by whoever wrote the repo, and apogee will meet ones it has never seen, in repos it was
handed a minute ago. Everything below follows from taking that seriously: the content can never be
allowed to fail apogee's startup, can never be interpreted, and can never move under a running
session.

This ADR records the design the owner ratified in the 2026-07-28 grill, implemented by
`docs/plans/2026-07-28 - 00 - workspace-context-files-plan.md`. It **supersedes nothing** and amends
ADR 0023's zero-byte anchor (§4 below, with a dated note in 0023 pointing here).

## Decision

**1. The configuration names NAMES, not paths — discovery is the feature.** The `context-files:`
block carries `names:` (default `[AGENTS.md]`), and each is looked up in the **workspace root only**:
`filepath.Join(WorkspaceDir, name)`, no parent-directory walk-up, no `~/.apogee` context file. A
name that resolves to nothing is **not an error** — it is the normal case, because one global config
is expected to travel across repos that carry different files or none. Whether a named file exists
is therefore deliberately **never** a config-time question; what *is* checked is the name's shape
(below). The two consequences are the point of the design: apogee needs **zero configuration** in a
repo that already keeps an `AGENTS.md`, and the same config works unchanged in a repo that does not.

Root-only scope is a decision, not a simplification. A walk-up would make what apogee sends depend on
where the repository happens to sit on disk — the same repo saying different things on two machines —
and a global context file would duplicate `system-prompt-text` / `system-prompt-file`, which already
are the standing text that follows the *user* rather than the *project*.

**2. Every listed name that exists is included, in list order.** The list is an **inclusion set**,
not a priority chain: `names: [AGENTS.md, CONVENTIONS.md]` folds in both, in that order, each under
its own `## Workspace context: <name>` header naming the file it came from. A first-found-wins chain
would make adding a second name a way to *silence* the first, which is not what a list reads like;
and the header exists so a model handed one merged block can tell whose conventions it is reading and
where one file ends.

**3. Content is DATA, verbatim — it never meets `internal/prompt`.** File content is never run
through `prompt.Validate` or `prompt.Render`: `{{anything}}` in a stranger's `AGENTS.md` passes
through untouched, and a repo whose file happens to contain double braces can never fail apogee's
startup with a template error about a file its author never thought of as a template. ADR 0023 §4's
strict-and-closed placeholder language governs the **user's own** template, which the user can fix;
it must not govern discovered content, which the user often cannot.

The structural consequence is that content is **carried beside the template, never concatenated into
it**: `Config.SystemPrompt` still holds only the validated template, and the cached content lives on
the Agent, joined to the rendered prompt at request time (§4). A design that appended the file to
`Config.SystemPrompt` would have put a repo's braces in front of `prompt.Validate` by construction.

**4. Files seed alone; the ADR 0023 zero-byte anchor narrows to "no prompt AND no context files".**
The two sources are **independent**: `standingSystem()` joins the rendered prompt and the rendered
blocks with a blank line and seeds the position-0 system message when the **result** is non-empty —
prompt alone, files alone, or both. Order on the wire is
**prompt → context files → mechanism directives → tool block**, which falls out of ADR 0023 §6's
position-0 seeding with no other change: the user's standing instructions, then the project's, then
whatever the engine appends after both.

`TestPromptSeam_NativeProfileByteIdentical` therefore still pins a byte-identical request — its
condition is now *no prompt and no context files* rather than *no prompt*. That is a genuine
narrowing of the anchor ADR 0023 §6 leaned on, and the reason it is safe is that the added bytes are
always the user's own configuration: nothing apogee invents.

**5. The read is SESSION-SCOPED: at construction and at every session boundary, nowhere else.** The
cache is filled by `newAgent` and refilled by a **successful** `ClearContext` (`/clear`, `/new`) and
a **successful** `RestoreSession` — a refused boundary (mid-Exchange) changes nothing. It is never
re-read per request. Two reasons, and both would be violated by a hot reload: a mid-session swap
would throw away the local server's prefix KV cache on the message most worth caching, and — worse —
would leave a conversation whose earlier turns were reasoned under instructions the model can no
longer see. Editing `AGENTS.md` while apogee runs lands on the next `/new`, which is a rule a human
can hold.

A restore reads the files **live** rather than restoring what the old session held, which is ADR 0023
§6's resolved-live posture applied unchanged: `cfg` is not serialized, and a resumed session gets the
current standing content ([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)
is untouched — the content is a per-request projection and enters neither history nor the snapshot).

**6. Sub-agents inherit the parent's BYTES, copied rather than re-read.** `newChildAgent` takes the
parent's cache, so a child is in the same session as its parent and reads the same instructions —
even if the file changed or was deleted on disk after the parent's session began. This is ADR 0023
§7's inheritance posture ("a sub-agent is the same assistant on a smaller task",
[ADR 0005](0005-sub-agent-privileges-are-bounded-by-the-parent.md)) with the extra property that
§5's session scoping cannot be defeated by fanning out.

**7. Skips are silent, LOUD, or fatal, by whose mistake it is.** A **missing** file and an
**empty/whitespace-only** file are skipped with no trace: absence is the common case, and there is no
point heading a block over nothing. A file that is **present but unreadable** is skipped **loudly** —
a note in the transcript naming the file and the error — and never fails startup: it was discovered,
not named, and a permission bit somewhere in a repo must not stop apogee from running. That is the
deliberate contrast with `system-prompt-file`, which the user named and whose unreadability *is* a
startup error (ADR 0023 §3).

A bad **name**, however, is the user's own defect and is fatal, checked twice: in the host's config
`validate` pass, where the message can name `context-files.names` and the offending value (an empty
entry, a name that is not workspace-relative, a `..` walk-up, a duplicate), and again in `newAgent`,
the defense-in-depth construction gate that catches a Go embedder's typo. Both gates apply ONE
shared name rule (`internal/domain`), so neither can drift away from the other, and that rule is
**machine-independent** (ADR 0023 §3's posture): a Windows-shaped escape — `..\x`, a leading `\`, a
drive-scoped `C:AGENTS.md` — is refused on Linux too, so a travelling config (or an embedder's list)
fails on the machine that wrote it rather than on the one that inherits it. Validation runs whatever `enable` says: a
defect in the file outlives the day the block is switched back off.

**8. Oversize is a NOTICE and a WARNING against the Budget's own share — never a cap.** At every
session boundary the host reports what was loaded and its size, and warns when the estimated tokens
of the **whole** standing system content (rendered prompt + blocks) exceed `Budget.SystemPrompt` —
the allocation share that already exists (`internal/context`'s 15% of working room). No new constant,
no threshold to configure, and no truncation: apogee never silently drops half of the conventions a
project deliberately wrote down. An unknown window allocates nothing, so the share is 0 and nothing
warns (the sizes are still reported). Because the window may bind seconds after a cold start — the
first heartbeat ([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md))
— a cold start typically cannot warn while the same session's next `/new` can. That is a documented
limit of measuring against a window nobody has named yet, not a defect.

**9. The surface is a config-file-only `context-files:` block, and one additive `Config` field.**
`enable` (default **true**, the `validated-sets:` precedent) and `names` (default `[AGENTS.md]`
**only when the key is absent** — an explicit `names: []` means "no names"). Both spellings of off
collapse at resolution into one downstream value: `Config.ContextFiles == nil` **is** the feature
being off, so nothing below the composition root has to know which spelling was used. No flag and no
environment variable, with the rest of the system-prompt region (ADR 0023 §1): what a project stands
for is not a per-invocation switch. The notice path is a read-only `Agent.ContextFilesReport()` plus
the `domain` report types the TUI renders — there is no engine-side note event, and the host already
owns all three boundaries where the notice belongs.

## Considered options

- **Concatenating the content into `Config.SystemPrompt` at the composition root** — rejected, and it
  is the option most of this ADR exists to refuse. It would run a stranger's file through
  `prompt.Validate`, so a repo whose `AGENTS.md` mentions `{{foo}}` would stop apogee from starting;
  it would also freeze the content at process start, losing §5's per-boundary re-read, and would make
  the prompt template and the discovered data one indistinguishable string in the Budget, the
  snapshot path and every error message.
- **A priority chain (first name found wins)** — rejected with §2. Adding a fallback name would then
  silently disable the primary one in every repo carrying both, and there is no reading of a
  configured *list* under which that is the obvious behaviour.
- **Walking up parent directories, git-style** — rejected with §1: the same repo would say different
  things depending on where it was cloned, and a file above the workspace is outside the fence every
  other read in apogee respects.
- **A global `~/.apogee/AGENTS.md`** — rejected: `system-prompt-text` / `system-prompt-file` already
  *are* the standing text that follows the user, and a second spelling of the same thing would only
  raise the question of how the two merge.
- **Re-reading per request, or watching the files** — rejected with §5: it would invalidate the
  server's prefix cache mid-session and leave a conversation reasoned partly under text that is no
  longer present. "Takes effect on your next `/new`" is both cheaper and easier to predict.
- **Truncating or capping oversize content to its Budget share** — rejected with §8. The one thing
  worse than a project's conventions costing tokens is *half* of them reaching the model, with the
  cut falling wherever the estimator happened to land. The human is told; the human decides.
- **A configurable size limit (a new constant or key)** — rejected: the Budget already allocates a
  system-prompt share, and a second, unrelated number would be one more thing to tune wrongly.
- **Failing startup on an unreadable context file** — rejected with §7: it makes a repo able to stop
  apogee from starting, which discovery must never do.
- **A `--context-file` flag / `APOGEE_CONTEXT_FILES`** — rejected with the other newer keys
  (ADR 0023 §1): the precedence ladder is for per-invocation switches.
- **Modelling it as a Mechanism (so `--bypass` would disable it)** — rejected. It is standing
  configuration, like the system prompt, not a self-regulating loop behaviour
  ([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)); a bypass run must still know what
  project it is in, or the two arms of an A/B are not comparable on anything but the Mechanisms.
- **An engine-side notice/note event carrying the report** — rejected: `internal/domain/events.go`
  has no note event, session notes are TUI-owned (`transcript.addNote`), and the host already owns
  the three boundaries the notice attaches to. A read-only report method keeps the new surface to one
  method and no new event variant.

## Consequences

- **`apogee.Config` gains one additive field, `ContextFiles []string`** — no exported name changes
  (ADR 0010), so this is a **minor** bump. `internal/domain` gains `ContextFileNote` /
  `ContextFilesReport` (aliased from the root facade, since an embedder cannot name an `internal`
  type), `Agent` gains the read-only `ContextFilesReport()`, and the TUI's `Engine` interface grows
  the matching method. An embedder that sets nothing is byte-identical to before.
- **A fresh install picks up this repo's own `AGENTS.md`** the first time apogee is started in it,
  and says so. That is the intended behaviour and it is also the largest behavioural change in the
  feature: standing content the user did not type now reaches the model. It is bounded by being the
  user's *own workspace* — the same trust boundary as every file the model reads anyway — by riding a
  header that names its source, and by `enable: false`.
- **The Budget measures it.** The seeded message is part of `req.State()`, so the predictive overflow
  guard and the estimator account for the content — honest accounting, at the price of less room for
  history on a small window. On a ~32k window a large `AGENTS.md` is a real cost, which is what §8's
  warning exists to surface.
- **ADR 0023 §6's zero-byte anchor is narrowed**, from "no prompt" to "no prompt AND no context
  files". ADR 0023 is left as written with a dated note pointing here.
- **ADR 0023's marker-suppression residual widens.** `Request.AppendToSystem(marker, text)` is
  idempotent by `strings.Contains` over the first system message, so a **workspace** file that
  happens to contain a Mechanism's marker phrase now suppresses that directive too — the same
  accepted suppression, from one more source, with the same bounded blast radius (catalogued nudge
  Mechanisms, default-off, enabled per model on bench evidence). Still recorded as a residual in
  `TODO.md` rather than fixed here.
- **The shipped template documents the block, commented.** `system-prompt-text:` remains the ONE
  active key a fresh install gets (ADR 0023 §8), and `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt`
  still holds — the defaults come from resolution, not from the file.
- **CONTEXT.md gains one term — *Context files*** — worded to match this ADR and distinguished from
  the System prompt it rides with, from an `@file` reference, and from a Skill.
