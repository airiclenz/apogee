---
title: Finished tool groups stay open; collapse them to one summary row
status: open
type: feature
priority: 2
plan: 2026-11-18 - 00 - settled-tools-and-watch-panel-width-plan.md
created: 2026-11-18T11:27:57Z
---

A finished tool group stays open in the transcript long after the conversation has moved past it. A `✦ Tools (…)` umbrella with up to `ui.tools-fold-over` type rows (default `5`) starts every session open and stays open until the reader folds it by hand. Each one costs its header plus one row per type row, and a long session fills with blocks nobody is reading any more. The owner wants a group that is no longer active to collapse to **one row**, and that row to say in plain words what the group did, the way Claude Code's "Read 3 files, ran 2 commands" line does.

Spec today: `docs/layout/tool-layout.md` §Vocabulary (super-group, umbrella header), §"Grouped tools folded (large umbrella)", §"Grouped tools folded (small umbrella)". Fold rules: [ADR 0035](.capcom/knowledge/decisions/adr-0035.md) addenda `adr-0035-addendum-0917` and `adr-0035-addendum-0918`. The facts the summary needs are already on the result as data: **Tool summary** (`capcom know show tool-summary`).

## What is wanted

1. **Auto-collapse a settled group.** Once a tool group is no longer active, fold it to its header alone. "No longer active" means a later transcript entry has been placed after it (the model's next narration or reply, a note, the next user message) or the Turn has ended. A group that is still running, or is the last entry of a running Turn, stays as it is today.
2. **Leave a touched group alone.** If the reader opened anything inside the group (a type row, a member, a member's body, a run view from it) or unfolded or folded the umbrella by hand, auto-collapse skips it. The reader's state wins.
3. **A summary header, for every umbrella** (open, folded, running or settled), replacing today's `✦ Tools (3 Read, 4 Terminal)` breakdown.

## Proposed header (the owner asked for a suggestion)

Today: `✦ Tools (3 Read, 1 Grep, 4 Terminal, 2 Edit) ⋯⋯ ▶`. It names tool types (the API's words) in a faint tone and says nothing about how the calls ended, which the reader only finds by opening the type rows.

Proposal: verb phrases built from each call's Tool summary, in the order each kind first ran, then the outcome:

```text
✦ Read 3 files, searched 1 pattern, ran 4 commands, edited 2 files +18 −5 ⋯⋯⋯⋯ ▶
✦ Read 3 files, ran 4 commands · 1 failed ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
✦ Reading 2 files, ran 1 command ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼   (call in flight: blinking ✦, live verb on the running kind)
```

- **Verbs and nouns per tool, not tool labels.** read_file → "read N files", grep / find → "searched N patterns", terminal / python_exec → "ran N commands", the four writing tools → "edited N files" (or "created" for a new file), web_fetch / http_request → "fetched N pages", list_dir → "listed N dirs", git tools → "checked git". Count distinct files where the Tool summary names them (three reads of one file read 1 file). Unknown or MCP tools fall back to `<label> ×N`.
- **Outcome on the line.** The diffstat summed across the edits (`+18 −5`, in the diff colours). A count of failed or denied calls in red (`· 1 failed`, `· 1 denied`) so a settled group that went wrong shows it without being opened. No marker when everything succeeded.
- **Width fallback**, most detail first to go: drop the noun phrases after the first two (`… and 2 more`), then the diffstat, then fall back to `(N calls)`, then clip. A failure count is the last thing dropped. The header stays one line.
- The `✦` and the fold glyph stay where they are. Sub-agent and Skill groups keep their own headers (see open questions).

## Decisions (owner, 2026-11-18)

- **On by default, with an opt-out.** A new registry-declared bool, `ui.tools-auto-collapse` (default `true`), shown in `/settings` and applied live from a hand-edit. Turning it off restores today's behaviour. Only the header change stays on either way.
- **Auto-collapse sits above the size rule.** A settled, untouched umbrella folds whatever its size and whatever `ui.tools-open` says. The size split and `ui.tools-open` still decide a live umbrella and a touched one. One rule for the reader: a finished group is one row unless you opened it. This takes a dated amendment to ADR 0035 (`adr-0035-amend-MMDD`), named on its Status line, that partly supersedes `adr-0035-addendum-0918`'s "size alone" for settled umbrellas.
- **"Touched" is view state.** It is in-session only and never serialized, like a small umbrella's fold. A resumed session shows every settled group collapsed.
- **Scope: `✦ Tools` umbrellas only.** Sub-agent groups, Skill groups and lone tool blocks are follow-up issue `settled-group-collapse-scope`.
- **Glossary.** The verb-phrase header becomes a new term, **Group summary**, defined in the glossary tree.

This is TUI view state only: nothing reaches the model, so the Floor/Bypass invariant is not involved. The engine stays Driver-neutral (ADR 0031): the summary is built in the TUI from the Tool summaries already on each result.

## Why

Settled tool groups pile up and push the conversation off screen. One row that says what happened, and that a failure shows on, keeps the transcript readable without hiding anything one click away (owner request, 2026-11-18).

## Acceptance

- A settled tool umbrella the reader has not touched folds to one row on its own; a running one, or one the reader opened anything in, does not.
- Every umbrella header reads as a verb-phrase summary built from Tool summaries (counts, diffstat, failed/denied count), with the width fallback specified and tested at narrow widths.
- `ui.tools-auto-collapse` (default `true`) is registry-declared, shows in `/settings` and applies live; off restores today's fold behaviour.
- docs/layout/tool-layout.md, docs/manual/configuration.md, a dated ADR 0035 amendment and a **Group summary** glossary Note describe the new rule; drift tests pass.
- Driven tuitest + stubllm tests cover: settle → collapse, a touched group exempt, a resumed session, a failure shown on a collapsed header.
