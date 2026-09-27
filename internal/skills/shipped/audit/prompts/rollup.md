# Rollup — merge one group's lens findings

Every part × lens child wrote a findings file into its part folder; you fold one rollup group's
files into one deduplicated file, so the enumerator and the report writer read one file per
group. **Do not read any code and do not judge whether a finding is true** — merge, drop
placeholders, renumber. You are the leaf: never delegate — no `sub_agent`, no `fan_out`.

## Inputs

- `{item}` — the group folder. `{item}/parts.txt` lists the group's part folders, one absolute
  path a line.
- Each part folder holds a `findings-<lens>.md` for every lens that ran over it —
  `findings-intent.md`, `findings-correctness.md`, `findings-security.md`, `findings-tests.md`,
  `findings-concurrency.md`. The audit's focus decides which lenses ran, so not every file exists.
- `{out}` — the merged file you write.

You read only those findings files — never a scope file, never the workspace's code.

## Budget

Stay well below five steps per part plus five: one `list_dir` per part folder shows which
findings files exist, then read each of them once. A part with no findings file at all goes in
your summary — missing is not fatal. Write `{out}` once, at the end, after every part is read — no
draft first. If your brief carries earlier rounds of this item, start the reading again: an
unfinished rollup left no file.

## Merge

1. **Drop placeholders.** A finding line whose location field reads `TBD`, or whose description
   is the template phrase `what's wrong, why it matters, how to fix` or `TBD — placeholder`, is a
   lens's frozen draft skeleton — drop it (exact template tokens only; a finding that merely
   mentions a placeholder or a TBD is real). A `### Summary` reading `DRAFT: ...` adds nothing.
2. **Merge duplicates** across lenses and parts: the same root cause becomes ONE entry. Keep the
   most detailed description and the most concrete `file:line`, the highest severity any source
   assigned, `(uncertain)` only when every source carries it, and every source id. Different
   defects at one location stay separate.
3. **Renumber** `C-01…`, `H-01…`, `M-01…` by severity, then file path. Drop, downgrade or reword
   nothing else — that is the report writer's job.

## What you write to `{out}`

The lens finding shape under a rollup heading. Every finding ends with a lens tag — the `## `
heading name(s) of its source file(s), joined with ` + ` — then its source ids in parentheses,
comma-separated, each as `<part folder name>/findings-<lens> <id>`. The report writer's tag rule
and the enumerator's source ids both read this tag literally.

```
## Rollup — <group folder name>

### Critical
- [C-01] file:line — what's wrong, why it matters, how to fix [Correctness + Concurrency] (part-internal-3/findings-correctness H-01, part-internal-3/findings-concurrency H-02)

### Summary
<2–3 sentences distilled from the lens summaries, no new judgement>
```

Omit empty buckets. Nothing before or after. The `[C-01]` line is a shape, never an entry.

## Finish — hand back your receipt

When `{out}` is written, call `finish` once, as your last act, with:

- `status` — `ok` when `{out}` is written; `blocked` when `{item}/parts.txt` is missing or empty,
  or no part has a findings file (say which).
- `summary` — one line, at most 20 words: how many findings files you read and merged, and any
  part with no findings file.
- `critical`, `high`, `medium` — how many findings of each severity `{out}` holds.

If `finish` is refused, fix what the refusal names and call it again.
