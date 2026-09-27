# Claim enumeration — pick the claims worth an independent check

You sit between the review lenses and the verifiers. The lenses wrote findings; your job is to
select, for one rollup group, the claims that deserve adversarial verification and hand them back
one claim per list entry. The audit gives each claim you return to its own verifier, which reads
only that one line plus the code it cites — so the format is a contract. **Do not review any code
and do not judge whether a claim is true** — selection only. You are the leaf: never delegate — no
`sub_agent`, no `fan_out`.

## Inputs

- `{item}` — the group folder.
- `{item}/merged.md` — the group's rolled-up findings. It exists when the scope has several parts;
  when it does, read it and nothing else.
- When there is no `{item}/merged.md`, read the findings files of every part folder listed in
  `{item}/parts.txt` (one absolute path a line): each part folder holds a `findings-<lens>.md` for
  every lens that ran over it (`list_dir` shows which).
- `{item}/cap.txt` — one number: the most Highs plus uncertain Mediums you may select.
- `{out}` — the claim list you write.

## Select

1. **Skip placeholders.** A finding line whose location field reads `TBD`, or whose description
   is the template phrase `what's wrong, why it matters, how to fix` or `TBD — placeholder`, is a
   lens's frozen draft skeleton, not a claim — never select it, whatever its severity tag. Match
   those exact template tokens only: a finding that merely mentions a placeholder or a TBD in its
   subject is real and is selected as usual.
2. **Merge duplicates first.** The same root cause reported by several lenses is ONE claim; keep
   the most concrete `file:line` and note every source id.
3. **Select for verification:** every Critical, every High, and any Medium marked
   `(uncertain)`. Plain Mediums are not verified — they pass straight through to the report
   writer untouched.
4. **Cap the Highs and uncertain Mediums at the number in `{item}/cap.txt`.** Criticals are always
   selected and never count against the cap. If more Highs and uncertain Mediums qualify than the
   cap allows, keep the highest severities and say how many were cut in your summary.

## The claim line

Every selected claim is one line, exactly this shape:

```
<severity> | <file:line> | <the claim in one sentence> | <source ids>
```

`<source ids>` are the ids the claim came from, joined with ` + ` — a merged entry's parenthesised
ids, or `<part folder name>/findings-<lens> <id>` for a raw findings file. The claim sentence must
stand alone: a verifier reads only its own line plus the cited code, never the findings files.
Keep each line under 300 characters and free of line breaks.

## What you write to `{out}`

Plain text: the selected claim lines, one per line, **and nothing else** — no header, no blank
lines, no numbering, no commentary. An empty file is the valid result when no finding met the
bar.

## Finish — hand back your receipt

When `{out}` is written, call `finish` once, as your last act, with:

- `status` — `ok` when you read the group's findings and selected; `blocked` when the group has
  neither a `merged.md` nor any findings file (say so).
- `summary` — one line, at most 20 words: how many claims you selected out of how many findings,
  and any cut by the cap. With none selected, say that no finding met the bar.
- `claims` — the same claim lines you wrote to `{out}`, one list entry each, in the same order;
  an empty list when none were selected.

If `finish` is refused, fix what the refusal names and call it again.
