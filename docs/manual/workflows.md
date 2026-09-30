# Workflows and recipes

A **workflow** is one piece of work apogee cuts into many small ones: the same brief run over a
list of items, each item handed to a **fresh helper** — a sub-agent with an empty context window
of its own — that reports back in a short, fixed shape. Your agent never reads the helpers'
prose. It gets **one line per item** and the path of a report, so fifty items cost it fifty
lines, not fifty conversations.

A workflow comes from one of two places, and in both the work is *asked for* — apogee plans
nothing on its own:

- **The model asks for one** with the `fan_out` tool: one brief over a list, optionally followed
  by an adversarial check of each item and one combined report.
- **You wrote one down** as a **recipe** — a skill whose header lists the stages to run. You
  start it with `/<id>`, like any skill; apogee ships one, `audit`.

Every finished item is kept on disk as it lands. A cancel, a quit or a crash loses only the items
that were still running, and starting the same workflow again skips the ones already done.

## Turning it on

**Recipes need nothing turned on.** A message that opens with a recipe skill's `/<id>` runs it —
`/audit internal/` works out of the box.

**The model's tools are off by default.** `fan_out` (the model runs a workflow) and `workflow`
(the model checks on, stops or messages the workflows it sent to the background) are in the
binary but offered to no model until you name them — a new tool on every request is a cost a
small model pays whether or not it uses it, so apogee will not charge it until you decide it is
worth it. Lift them for every model under `tools.enabled:`:

```yaml
# ~/.apogee/config.yaml
tools:
  enabled: [fan_out, workflow]
```

or for one model only, under its [model profile](configuration.md#model-profiles--model-profiles):

```yaml
# ~/.apogee/config.yaml
model-profiles:
  my-big-model:
    tools:
      enabled: [fan_out, workflow]
```

A profile's `tools:` replaces a built-in roster for that model whole, so re-list anything the
built-in one lifted — `qwen3.8`'s Console family, for one.

`fan_out` alone is enough for workflows that run to their end inside the reply that asked for
them. Adding `workflow` beside it also gives `fan_out` its `background` switch — see
[Background workflows](#background-workflows). Background workflows exist in the TUI only: a
[`headless`](headless.md) run or a [`daemon`](daemon.md) firing has no conversation to go on
while a workflow runs, so there `fan_out` always blocks and `workflow` is not offered, whatever
the roster says. A delegate — a `sub_agent` child or a workflow's helper — is never offered
`workflow` or `background` either: background workflows belong to your agent alone. The rest of the `tools:` block — `disabled:`, typos, which list wins — is in
[Configuration](configuration.md).

## What the model asks for — `fan_out`

`fan_out` runs one brief over a list of items. Its arguments:

| Argument | What it is |
|---|---|
| `task` | The brief every helper gets. `{item}` is replaced by the helper's item, `{out}` by the file it writes its detail to. Each helper starts fresh, so the brief has to stand on its own |
| `over` | The items: a list of strings, or one of `files` (a workspace glob, `**` allowed — each match is an item), `lines` (a file whose non-blank lines are the items) or `split` (a folder cut into contiguous parts, each sized to fit one helper's context window) |
| `batch` | How many items one helper gets (default 1) |
| `context` | Files every helper reads before its item |
| `returns` | The typed fields each helper reports beside its status and summary, as `name: type` — see [Receipts](#receipts--what-each-helper-hands-back) |
| `out` | Where each helper writes its detail, with `{item}` in the path; unset, each item gets a file in the workflow folder |
| `verify` | One adversarial check per item: a helper whose job is to *refute* the item's report. `when` picks which items (a [condition](#conditions--when) on the `returns` fields; unset checks every one), `task` says what to look at |
| `merge` | One helper that reads every item's output and writes the combined report; `task` is its brief |
| `tools` | Narrows the helpers' tools to these names — it can only remove tools from the agent's own menu, never add one |
| `recipe`, `inputs` | Run a [recipe](#recipes) by its id instead, with its inputs as `name: value`. Nothing else may be given beside `recipe` |
| `background` | Start the workflow in the background and answer at once — offered only while `workflow` is on the roster |
| `run_on` | Where the helpers run, as on `sub_agent` — offered only under [`sub-agents-choice: model`](configuration.md#letting-the-model-pick-the-seat) |

A model-written workflow is deliberately short: exactly one fan-out, then at most one verify,
then at most one merge. Anything longer is a recipe. A call that does not add up is refused
before anything runs, with one line per argument to fix.

The helpers run as many at a time as the server allows — its
[`parallel-agents:`](configuration.md#the-servers-you-run-models-on) width — and the call
answers when the last one has reported. Each helper is spawned exactly as a `sub_agent` child is:
the same privileges as your agent or fewer, the same depth limit, and every tool call it makes
decided by the same mode, guard and approval rules. Calling `fan_out` again with the same
arguments finds the workflow it already started and skips the items that finished. An item is
keyed by its brief, the item itself and the content of its context files, so an item whose input
changed runs again.

When a reply fans out more `sub_agent` calls than
[`delegate-fanout-rounds:`](configuration.md) allows, the refusal ends with
`— for more items, use fan_out` while `fan_out` is on that agent's roster.

## What comes back

The call — or the message that started a recipe — is answered with **one line per item**,
numbered within its stage:

```
#1 internal/agent — ok — retry loop leaks a timer on cancel findings=1
#2 internal/tools — ok — nothing that matters findings=0
#3 internal/tui — blocked — the part did not fit one context window
items 3 · ok 2 · partial 0 · blocked 1
report: /…/workflows/20260928-101500-audit/report.md
```

- Each line is `#<n> <item> — <status> — <summary>`, then the item's `returns` fields as
  `key=value` pairs, then `verdict=…` when a verify stage checked it.
- The **totals line** counts the items by status, and adds `unfinished`, `resumed` and the
  verify verdicts (`confirmed`, `refuted`, `unclear`) when there are any.
- A stage that is not a fan-out — a script, a question, a failed merge, a skipped stage — leaves
  one line of its own, e.g.
  `ask focus: ok — took the default all answer=all (default taken: no one to ask)`.
- `report: <path>` appears when a merge wrote the report.
- **Past 40 items** only the items that did not end `ok` are listed, and `items: <path>` points
  to the full listing, so a big fan-out cannot flood the agent's context.
- A cancelled workflow leads with `stopped by the user: K of N done`, still lists everything
  that finished, and ends with a line saying how to re-run the same workflow, which keeps its
  finished items:
  `` to resume: re-run `/audit internal/` — finished items are kept `` for a recipe you typed,
  `` to resume: run `/<id>` again with the same inputs — finished items are kept `` for one
  started with its inputs already bound, and
  `to resume: call fan_out again with the same arguments — finished items are kept` for a
  `fan_out` call.

### The workflow folder

Each workflow keeps everything it does in a folder of its own under the session's scratch
directory, `workflows/<id>/`, where `<id>` is a timestamp and the workflow's name. The folder
holds the plan, a `status.json`, one folder per item with its receipt, its detail output and the
helper's whole conversation, the answers and script results a resumed run replays, the full
`items.md` listing and the `report.md` a merge writes. It is readable by you alone, like the
session store. [`/workflows`](#the-workflows-view--workflows) is the way to browse it.

## Receipts — what each helper hands back

A helper ends by calling a tool of its own, `finish`, with a **receipt**:

- `status` — `ok`, `partial` or `blocked`;
- `summary` — one line of at most 20 words (the detail belongs in its output file);
- the typed fields the stage declared under `returns:`, each one of
  - `int` — a whole number,
  - `text` — at most 200 characters,
  - `list` — a list of strings,
  - an enum written `a|b|c` (or `enum a|b|c`).

A receipt that does not fit is bounced back to the helper at once, with one line per field to
fix, and the helper keeps working. An `ok` receipt carries every declared field; a `partial` or
`blocked` one may leave any out.

**Each item gets two kinds of second chance.** A helper that runs out of room before it reports is
*continued* — a fresh one picks the item up, seeded with the rounds before it — up to
`workflow-continuations:` times (default **2**). Past that, or after a fault or an ending with no
receipt, the item *starts over* with a fresh helper, up to `workflow-retries:` times (default
**1**). An item out of both ends on the best it has, marked as such, and the rest of the workflow
runs on. Both keys are [in Configuration](configuration.md); `0` switches either off.

## Recipes

A recipe is a skill that carries a `recipe:` list of stages in its header, and usually the
`inputs:` it takes. It lives where any skill does — `~/.apogee/skills/<id>/SKILL.md`, or a
project's own `.apogee/skills` — and the files its stages name (prompt files, scripts) sit
beside it in that folder. A shortened example:

```yaml
---
id: triage
summary: Sort every failing test in a folder into cause buckets, then write one summary.
inputs:
  - name: scope
    required: true
    description: the folder whose tests to triage
  - name: depth
    default: quick
    description: quick or thorough
recipe:
  - name: failing
    kind: script
    run: "sh {{SKILL_DIR}}/failing.sh {scope} {workflow_dir}"
    returns:
      count: int
  - name: tests
    kind: pick
    when: "failing.count > 0"
    file: failing.txt
  - name: diagnose
    kind: fanout
    over:
      stage: tests
    prompt: prompts/diagnose.md
    out: "{item}/diagnosis.md"
    returns:
      cause: "flaky|product|test|environment"
  - name: summary
    kind: merge
    from: diagnose
    task: "Group the diagnoses by cause, one section per cause, most items first."
---

# Triage

What the recipe is for, for the person reading it.
```

A recipe that does not validate — an unknown kind, a key a stage does not read, a stage that
names one after it, a condition that names a field nobody reports — keeps the skill from loading,
and the reason names the stage and the field to fix.

### The seven stage kinds

Stages run in the order written, and a stage may refer only to stages before it. Every stage has
a `name` (lower-case, unique — later stages refer to it by that name), a `kind`, and may have a
`when:` [condition](#conditions--when); a key a kind does not read is refused rather than
ignored.

| Kind | What it does | Its keys |
|---|---|---|
| `fanout` | One fresh helper per item (or per batch), each handing back a receipt | `over` (`list`, `files`, `lines`, `split`, or `stage` — the items of an earlier `pick`; plus `batch`), `task` or `prompt`, `returns`, `out`, `context`, `tools` |
| `verify` | One adversarial helper per item of an earlier fan-out, trying to refute its receipt; its verdict — `confirmed`, `refuted` or `unclear` — is folded into the item. `when:` picks the items, reading each item's receipt | `from` (default: the nearest earlier fan-out), optional `task` or `prompt` (apogee's own refute-it brief always leads), `context`, `tools` |
| `merge` | One helper over a manifest of every item's receipt, verdict and output, which writes `report.md` in the workflow folder | `from`, `task` or `prompt`, `returns`, `context`, `tools` |
| `pick` | Turns a `list` field of an earlier stage's receipts, or the non-blank lines of a file in the workflow folder, into items for a later fan-out. No helper | `from` + `field`, or `file`; `cap` (keep at most this many), `batch` |
| `script` | Runs a command and reads its `KEY=value` stdout lines as the stage's receipt. No helper | `run`, `returns` |
| `ask` | Puts a question to you and stores the answer in the stage's `answer` field. No helper | `question`, `options`, `default` (required — it is the answer taken where no one can be asked) |
| `repeat` | Runs an earlier stage again while its `when:` holds, at most `max:` rounds (10 at most) | `repeat` (the stage's name), `max` |

A **brief** is `task:` written inline or `prompt:`, a file in the skill's folder. In it `{item}`
is the item and `{out}` the file the helper writes its detail to. A verify or merge helper's brief
is led by apogee's own, which names the item's receipt and output to check, or the manifest to
read and where the report goes; yours adds what to look for. A `prompt:` file's content is part
of every item's key, like a context file's: edit it and a re-run of the recipe redoes that stage's
finished items instead of keeping them, and the stage says so in its result and in `/workflows`:
`fanout <stage>: redid <n> finished item(s): their inputs changed since they ran` — the same line
for a context-file edit, or a merge redone because an upstream item changed. A workflow folder
an older apogee started still resumes its finished items after an upgrade, even when a `prompt:`
file was edited before the folder's first run on the new build: that build is the first to key an
item by the file's content, so it cannot tell the edit apart.

A **script** runs as a `terminal` call — under exactly the mode, guard, confinement and approval
rules the model's own shell calls meet, so in Ask-Before you approve it. Plan is the one
exception, because Plan refuses the model's shell outright: there a script stage runs inside
the confinement box with its own workflow folder as the only place it may write (a write to the
workspace fails in the sandbox), and on a host with no confinement backend it is refused — so a
read-only `audit` works in Plan, the default mode of headless runs and daemon firings. A stdout line whose key
the stage's `returns:` declares becomes that field (`summary=` sets the summary); every other line
is ignored. The status is `ok` on exit 0 and `blocked` otherwise. Its `run:` may use three
placeholders beside the inputs: `{{SKILL_DIR}}` (the skill's folder — a copy staged into the
workflow folder for a skill apogee ships), `{workflow_dir}` and `{part_bytes}` (how many bytes a
`split` part may hold — sized to a helper's context window).

An **ask** takes its `default` wherever no one can answer — a headless run, a daemon firing — and
says so on its result line: `(default taken: no one to ask)`. A workflow that is resumed never asks
a question you already answered, nor re-runs a script that already produced a result.

### Inputs

`inputs:` declares what the text after `/<id>` fills in. Each entry has a `name` (one word:
letters, digits, `_` or `-`, starting with a letter), and optionally `required: true`, a `default`
and a `description`. `{<name>}` then stands for the input's value in a brief, a question, an item
source, an output path, a context file, or — shell-quoted — a script's command. A run with other
inputs is another workflow, with a folder of its own.

The text binds with no model call:

- `name=value` binds that input by name;
- every other word binds the next input no `name=value` took, in declared order;
- quotes keep spaces together — `/audit "cmd/ internal/" focus=security`;
- an empty value (`scope=`, `""`) leaves the input to its default.

An input with neither a value nor a default is asked of you, with its description, before
anything runs. Where no one can be asked the run fails as `missing input: <name>`. An unknown key,
a key given twice, a word too many or an unclosed quote is refused with all of them named at once.

### Conditions — `when:`

A condition compares receipt fields: `field op value` terms with `==`, `!=`, `>`, `>=`, `<` or
`<=`, joined by `and`, `or` and `not`, grouped with parentheses (`not` binds tightest, then `and`,
then `or`). A value may be quoted.

- On a **verify** stage it reads **each item's** receipt and picks the items to check —
  `findings > 0`.
- On **every other** stage it reads **earlier stages** and skips this one when false. Each field
  names its stage: `split.parts > 1`, `focus.answer == all`, a script's or a merge's field, or a
  fan-out's tally — `find.ok`, `find.partial`, `find.blocked`.
- On a **repeat** it is the loop condition, and may leave the repeated stage's name off its own
  fields.

A condition is type-checked against what the stages declare, so a typo in a field name or a value
an enum does not allow is caught when the skill loads, not half-way through a run.

### Starting a recipe

- **In the TUI**, send a message that opens with `/<id>`: `/audit internal/`. The workflow runs
  first, drawn in the transcript as one **workflow block** that grows in place, and then the
  model's first request carries your line followed by the result lines. A `/<id>` later in a
  message attaches the skill's body as usual, and a recipe line sent while the agent is working is
  refused and stays in the box. What the block shows, and how you read each helper's work, is
  [below](#watching-a-workflow-in-the-tui).
  If you cancel it with `esc` twice while it runs, your line and the stopped result lines stay in
  the conversation, closed with the usual cancelled note, and no model request follows. The
  model's next request reads what the run did. To pick the workflow up where it stopped, re-run the
  same line — re-running `/audit internal/` skips the items already finished. This re-runs the
  workflow inside the current session; it is not a session resume (`--resume`, `--continue`,
  `/sessions`).
- **In the background**, with [`/bg`](#background-workflows): `/bg /audit internal/`.
- **Headless**, with [`apogee headless --recipe audit internal/`](headless.md).
- **On a schedule**, with a daemon entry's [`workflow:`](daemon.md) in place of `prompt:`.
- **By the model**, with `fan_out{recipe: "audit", inputs: {scope: "internal/"}}`. When the
  model loads a recipe skill with `load_skill`, it is told how the recipe is started: through
  `fan_out` when it has that tool, by you with `/<id>` when it does not.

The shipped `audit` is the worked example: its header, prompts and split script are in
[`internal/skills/shipped/audit/`](../../internal/skills/shipped/audit/SKILL.md), and
`/skills export audit` copies it into your library to make your own.

### Watching a workflow in the TUI

The block reads `✦ Workflow <name> — running` (`waiting for you` while an ask stage's question is
out, then `finished`, `stopped` or `failed`), and beneath it **one row per stage** of the recipe,
all of them from the start (the sketch shortens `audit`'s list):

```
✦ Workflow audit — running
  ┝ split ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ done
  ┝ ground-truth ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 2/5 · running  ▶
  ┕ lens-intent ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ pending
```

- A stage row says where the stage stands: `pending` (dim — it has not started), `running`
  (`2/5 · running` while items finish), `waiting for you`, `done` (with a ✓ when every item came
  back `ok`), `failed` (a merge that wrote no report, a script that blocked), `stopped`, or
  `skipped` (dim — its `when:` was false). A stage a repeat runs again keeps one row and adds the
  round: `round 2/3 · running`.
- Beneath the rows the block keeps one line for each item that did **not** come back `ok`, an ask
  stage's question, and once the workflow ends the totals line and, if it failed, the cause. A
  recipe's block that `stopped` ends on the command that re-runs it:
  `` re-run `/audit internal/` to resume ``, or `` run `/audit` again with the same inputs to resume ``
  for a recipe started with its inputs already bound. A `fan_out` card shows no such line; the
  model reads its own in the call's result.
- Re-running a stopped workflow draws a **new block** for the new run, and the stopped block stays
  frozen as it ended — its rows still open what its helpers did.
- The helpers' own work — their narration and tool calls — never fills the conversation. A row
  wearing **▶** at its right end has work to open: click it, or `⏎` on it with the block cursor.
  A stage of one item opens that helper's **run view** straight away, the same full-transcript view a sub-agent
  opens ([the run view](commands.md)). Any other stage opens a **stage view**: one row per item
  with its receipt's summary, grouped under `round N` for a repeated stage, with a retried item's
  earlier attempts as dim `attempt N` rows beneath it. Each item row opens that helper's run view.
- The black band at the top names where you are — `← main › audit › ground-truth` on the stage
  view, `← main › audit › ground-truth › internal/tui` on one of its items — and `esc`, or a click
  on the band, goes up one level.
- Inside a running helper's view you can message it, and `^x` stops that one helper, as for any
  sub-agent. A stage view itself is read-only — it is a list, not a helper — so the box says so and
  a message typed there is refused. Stopping the whole workflow is `esc` twice at the top level, or
  `^x` in [`/workflows`](#the-workflows-view--workflows).
- A `fan_out` call the model makes draws its own block with **one row per item**, since it has one
  stage; each opens that helper's run view.
- Each item's helper goes by the item's **short name**, not its full entry: a path inside the
  workflow folder reads relative to it (`part-a`, `group-1/x.md`), the workflow folder itself reads
  as the stage's name, and a path outside it reads as its file name. Any other entry reads as
  written. That is the name an approval the helper asks for carries. Two items can share a short
  name — `/a/x.go` and `/b/x.go` both read `x.go` — so the `workflow` tool's `message` still takes
  the item's full entry, or its run id, to tell them apart.
- **After a resume** the block comes back with the same rows, and they open read-only views of what
  each helper did. A workflow that was still running when the session was saved reads `stopped`.
  A replayed `stopped` block keeps its re-run line: after a session resume (`--resume`,
  `--continue`, `/sessions`), re-running that same `/<id>` line in the resumed session continues the
  workflow from its folder, and draws a new block of its own.

## Background workflows

A background workflow runs **beside the conversation**: you and the agent go on working, and it
reports when it ends. There are two ways to start one:

- **You**, with `/bg /<recipe> <text>` — e.g. `/bg /audit internal/`. Its inputs bind from the
  text alone; a required one the text leaves out is refused as `missing input: <name>` rather than
  asked. apogee notes `started <id> in the background`.
- **The model**, with `fan_out`'s `background: true` — offered only while the `workflow` tool is
  on its roster. The call answers at once with the workflow's id and its `status.json` path.

While one runs, the status line reads `1 workflow running` (`N workflows running`). A background
workflow runs one helper short of the server's width, so a slot stays free for the conversation;
on a width-1 server the two take turns on the one slot. Only one background workflow runs per
server at a time — a second one waits `queued` and starts when the first ends. It keeps the model,
server and tools it started with, and `esc` never reaches it: it is stopped from
[`/workflows`](#the-workflows-view--workflows) or by the model's `workflow` tool.

**Questions wait for you.** An approval one of its helpers needs, or an `ask` stage's question,
never interrupts what you are doing. The status line adds `· 1 workflow waiting for you`, and the
question opens in the approval or answer pane as soon as you are idle and no pane is open; `esc`
puts it back to wait until your next exchange ends. You need not wait for either: `^a` in the
workflow's [`/workflows`](#the-workflows-view--workflows) detail opens it at once.

**The wake.** When a background workflow ends — finished, stopped or failed — apogee writes a
one-line finish note into the transcript: its name, how it ended, its items counted by status (and
the verify verdicts) and the path of its report — ending on the
[seat-fallback note](configuration.md#letting-the-model-pick-the-seat) when its helpers asked for
the sub-agents server and ran on the session server instead. Then:

- an **idle** agent is woken: apogee opens a turn of its own on the note, shown as a
  `(background workflow report)` prompt row, which `esc` cancels like any other;
- a **busy** agent gets the note at its next pause between tool calls;
- under `workflow-wake: off` (a file-only key, `on` or `off`, default `on`) nothing is woken, and
  the note rides your next message instead.

### Sessions

- **`/clear`, `/sessions` and `/fork`** with a background workflow running first ask
  `stop running workflows? (y/n)`: `y` stops them and keeps their finished items, `n` keeps them
  running and their finish notes go to the new conversation, `esc` does nothing.
- **Quitting** stops every running workflow and keeps its finished items. A session whose only
  work is a background workflow is still saved.
- **Resuming** — `--resume`, `--continue` or `/sessions` — starts its workflows again from their
  folders, finished items skipped, and a finish note that had not reached the agent yet rides its
  next message.

## The `workflow` tool

`workflow` is the model's handle on the workflows it sent to the background. It takes an
`action`:

| Action | Does |
|---|---|
| `status` | Lists every workflow of the session with its items — or, with `id`, one in detail |
| `stop` | Stops the workflow `id` and keeps its finished items |
| `message` | Sends `text` to one running item's helper, named by `item` (the run id or short name `status` shows, or the item's full entry when two share a short name; `id` narrows it to one workflow). It reaches the helper between its steps, as a note from you would |

The model is woken with a workflow's result when it ends, so it has no need to poll.

## The workflows view — `/workflows`

`/workflows` lists the session's workflows — every `fan_out` and every recipe run, background or
not — one row each with its state (`running`, `waiting for you` while an approval or a question of
it waits on you, `queued`, or how it ended) and its items done of all.

- `⏎` opens one to its stages and their items, each by its short name with its status and
  summary; `⏎` on an item opens it read-only — its receipt, the detail output it wrote and its
  whole conversation.
- `^a` answers what the workflow waits on: the pane closes and its oldest waiting approval or
  question opens — one `esc` sent back included — only while the agent is idle; mid-turn it says
  so and opens nothing.
- `^x` stops the workflow and keeps its finished items.
- `^r` re-runs its `blocked` and faulted items as a new run of the same finished workflow, in the
  background and only while the agent is idle; every item that ended `ok` or `partial` is kept.
- `^s` saves a `fan_out`'s workflow as a **recipe**: it asks for a name and writes
  `~/.apogee/skills/<name>/SKILL.md`, whose recipe runs the same stages — the path the items came
  from becomes its `scope` input — and `/<name>` runs it at once. A name a skill or command already
  answers to, or a folder already there, is refused, never overwritten.
- `esc` goes one level up, and closes the pane from the list.

The full command reference is in [Commands](commands.md).

## The keys, in one place

| Key | Default | What it does |
|---|---|---|
| `tools.enabled:` / a profile's `tools:` | — | Lifts `fan_out` and `workflow` ([Turning it on](#turning-it-on)) |
| `workflow-continuations:` | `2` | How many times a helper that ran out of room is continued |
| `workflow-retries:` | `1` | How many times an item starts over with a fresh helper |
| `workflow-wake:` | `on` | Whether a background workflow's end wakes an idle agent (`on` / `off`) |
| `parallel-agents:` | the server's | How many helpers run at once ([servers](configuration.md#the-servers-you-run-models-on)) |

The three `workflow-` keys are file-only and read when the session is built, so an edit applies at
the next start.
