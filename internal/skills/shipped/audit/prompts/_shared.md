## Shared rules — every lens follows these

These rules bind every review lens; the lens text above them says only what to look for. Every
lens brief carries this same text, so it is already in front of you — nothing to open.

### Your inputs

- `{item}` — your part folder. `{item}/scope.txt` lists one workspace-relative path per line:
  **that list is the entire review scope.** Read those files. Do not review anything outside the
  list. Reading outside it is allowed only to settle one specific finding — a caller check, a
  duplicate's second location — and costs at most 2 tool calls per finding. Never survey the
  repository.
- `bundle.md`, in the folder `{item}` sits in (the workflow folder) — the ground truth: mission,
  invariants, coding standards, deployment context, testing expectations, language. **Treat it as
  authoritative and do not re-read the project's documents yourself** — that work was already
  done. Read a specific document only when a finding hinges on its exact wording, and then only
  that document.
- `tools.md`, in the same workflow folder — *may be missing.* The distilled output of the
  project's own analysis tools. Use its **Coverage** and **Leads** sections to direct your
  reading; never report anything listed under **Tool-confirmed findings** — those reach the report
  directly without you. The absence of a tool warning is not evidence of health.
- `{item}/tests.txt` — one path per line: the test files that belong to this part. They are
  **reference, not review scope**: only the tests lens consults them (its text says how); any
  other lens never opens them.
- `{out}` — your findings file, the one file you write.

If your brief carries earlier rounds of this item — you are continuing a run that reached its
limit — read `{out}` first and carry on from what it holds. Never start over.

### Severity ladder — the floor is Medium

- **Critical** — data loss, corruption, or deadlock in normal use; a stated invariant broken in
  normal use; a remote unauthenticated exploit.
- **High** — a wrong result, crash, race, or exploit under realistic conditions; a remote exploit
  with limited access, or a local privilege escalation.
- **Medium** — a wrong result in plausible edge cases; an exploit under realistic but specific
  conditions; dead or duplicated code that has already caused divergence; a critical path with no
  meaningful test.

Anything below Medium is not a finding. Discard it silently.

### Universal rules

1. **Finding budget: 8.** More candidates: keep the 8 most impactful and discard the rest. Do not
   pad — a lens with two real findings reports two.
2. **Evidence required.** Every finding cites `file:line`, names the realistic trigger, and
   proposes a concrete fix. Code excerpts are at most 5 lines.
3. **Read the code.** Open the files. Never review from file names, grep hits or excerpts alone.
4. **Respect documented intent.** Code that matches the bundle's invariants is not a finding. One
   exception: a documented decision *actively causing defects you found in this review* is
   reported by the intent lens, marked **"warrants revisiting `<doc or ADR>`"**. Never silently
   drop it; never relitigate a decision that is not causing harm.
5. **Mark uncertainty.** Prefix the finding with `(uncertain)` and say what would confirm it. Do
   not pad the file with maybes. When only running code would settle a finding, write it
   `(uncertain)` and put the exact command a human would run in the "what would confirm it"
   clause.
6. **Out of scope, every lens:** style, naming, formatting, doc comments, spelling, import order;
   coverage percentages; hardening wishes with no concrete attack; hypothetical bugs that need
   impossible inputs; a bare `return err` without wrapping (style, not a bug).
7. **No design work.** Where the structure needs re-shaping, state the friction and mark it a
   candidate for an architecture review. Do not design the refactor.
8. **Step budget: be done within about half your step limit.** A step is one of your turns, with
   every tool call in it. The engine ends your run at its step limit (80 steps unless the user set
   another) and then grants one last turn in which only `finish` can be called. At the halfway
   mark you should be writing, not reading: finish `{out}` with what you have and hand back
   `status: partial`, naming the unreviewed scope files in the summary. What keeps you inside:
   - Read each scope file **exactly once, whole** — no line-range slices, never a second read of
     the same path. Take your notes from the first read.
   - A caller or duplicate check is at most 2 tool calls per finding, and only for a finding you
     are about to write. Never grep to explore.
   - Files outside `{item}/scope.txt` are not yours to review; open one only to settle a specific
     finding, and count it.
9. **Write first, refine in place.** Write the first draft of `{out}` — in the format below — as
   soon as the 3rd scope file is read or at your 10th step, whichever comes first, before you read
   any further scope file. Thereafter rewrite `{out}` at least every 10 steps and after every
   finding change; a finding never lives only in your head. Whatever `{out}` holds when the step
   limit ends your run is what the audit gets.
   **The draft holds only findings you have actually formed.** Never copy the format's example
   lines — `[C-01] file:line — what's wrong, why it matters, how to fix` is a shape, not a
   finding, and a `TBD` or `placeholder` entry is not one either; the report writer would read a
   frozen skeleton's `[C-01]` line as a real Critical. A draft with no finding yet is exactly the
   lens heading plus `### Summary` reading `DRAFT: <k> of <m> scope files read` — overwrite that
   line the moment the first finding exists.
10. **You are the leaf.** Never delegate any part of the review — no `sub_agent`, no `fan_out`.
    Do the reading yourself, inside the budget.
11. **Read-only review — no probes.** You never create, modify, copy or delete a file or directory
    anywhere — workspace, scratch, temp — except `{out}`. You never execute project code, tests,
    builds or scripts (`go run`, `go test`, `go build`, `pytest`, `npm test`, `cargo` are
    examples, not the list). Read with `read_file`, `grep`, `find_files`, `list_dir`, `git_log`
    and `git_show`; if you must use the terminal, it is for read-only inspection only (`git blame`,
    `wc`, `ls`). The machine-checks stage already ran the project's tools; a lens never runs them
    itself. A probe asks the human for approval and stalls an unattended run.

### What you write to `{out}`

Markdown, exactly this shape, under the heading your lens names above. Omit empty buckets.
Nothing before or after.

```
## <Lens heading>

### Critical
- [C-01] file:line — what's wrong, why it matters, how to fix

### High
- [H-01] file:line — what's wrong, why it matters, how to fix

### Medium
- [M-01] file:line — what's wrong, why it matters, how to fix

### Summary
<2–3 sentences on overall health from this perspective. If the code is solid from this angle,
say so plainly — do not invent findings to fill space.>
```

### Finish — hand back your receipt

When `{out}` holds your final findings, call `finish` once, as your last act, with:

- `status` — `ok` when you reviewed the whole scope; `partial` when you reviewed only some of it
  (say which fraction in the summary); `blocked` when you could not review at all (say why).
- `summary` — one line, at most 20 words: the single most important thing you found, or that
  this part is clean from your lens's angle.
- `critical`, `high`, `medium` — how many findings of each severity `{out}` holds.

The findings belong in `{out}`, never in the receipt. If `finish` is refused, fix what the refusal
names and call it again.
