---
id: planning
displayName: Planning
summary: Plan before you build — restate the goal, enumerate the steps against real files, then execute one step at a time and verify each before starting the next.
description: >-
  Use when a task is big enough that writing code first would go wrong: a multi-file
  change, a feature, a refactor, a migration, or any request whose shape is not yet
  clear. Turns the request into a restated goal, a short ordered list of steps naming
  the files each one touches, and a one-step-at-a-time execution loop where every step
  is verified before the next begins.
triggers:
  - make a plan
  - plan this
  - plan out
  - how should I approach
  - break this down
  - what are the steps
  - implement this feature
  - where do I start
  - refactor this
  - multi-step task
  - step by step
---

# Planning

A plan is not a ritual — it is the cheapest place to be wrong. Getting the order and
the file list wrong on paper costs a minute; getting it wrong in the working tree
costs the rest of the session.

## 1. Restate the goal

Before anything else, write one or two sentences: what will be true when this is done
that is not true now.

- State it as an observable outcome, not as an activity. "The config loader accepts a
  `timeout` key and the CLI honours it" — not "work on config".
- Name what is explicitly NOT in scope. Most plans fail by growing, not by being wrong.
- If the request is ambiguous, resolve it now. Ask one sharp question, or state the
  assumption you are proceeding on in the plan itself so it can be corrected cheaply.
- If you cannot restate the goal without hedging, you do not understand it yet. Read
  the code first — a plan built on a guess about the codebase is a guess.

## 2. Survey before you enumerate

Steps that do not name real files are wishes.

- Find the code the change lives in: search for the symbols, the config keys, the
  strings the user sees. Read the seams you intend to change, not just their names.
- Note what already exists that you can extend. The best step is often "add a case to
  the existing switch", not "build a new subsystem".
- Note the tests that cover this area now — they tell you the contract you must not
  break, and they are where your own tests will go.
- Note the docs that will become false. A doc line that contradicts the new behaviour
  is part of the work, not an afterthought.

## 3. Enumerate the steps

Write an ordered, numbered list. For each step, three things:

- **What** changes, in one sentence.
- **Which files** it touches, by path.
- **How you will know it worked** — the command to run, or the observable result.

Rules that make the list useful:

- Order by dependency: each step compiles and passes tests on its own. If step 3 cannot
  build without step 4, they are one step.
- Keep steps small enough to hold in your head — roughly one coherent change each. A
  step that touches a dozen files across three layers is two or three steps.
- Put the risky or uncertain step EARLY. If the approach is going to fail, fail on step
  1, not after four steps of scaffolding built on it.
- Include the tests and the doc updates as part of the step that needs them, not as a
  trailing "step N: write tests". Tests written at the end are written to pass.
- Stop at the goal. A step that is not needed for the restated goal does not belong in
  the list, however tempting.

The list is ready when you can say, in one line: the goal is true when [observable], N
steps, files [paths], first verification command [cmd]. If you cannot fill that line,
the list is not finished — a missing observable means step 1 is missing.

## 4. Execute one step at a time

- Do step 1 completely. Then verify it. Only then read step 2.
- Verify with the check you wrote down, not with a glance at the diff. Run the build,
  run the tests, run the command.
- Do not fold later steps into an earlier one because you are "already in the file".
  That is how a two-file change becomes an unreviewable one.
- Keep the working tree buildable between steps. If you must break it, that break lives
  inside one step and is repaired before the step ends.

## 5. Adapt out loud

Plans meet the codebase and the codebase wins.

- When a step turns out to be wrong, say so, say why, and amend the remaining steps
  before continuing. Silently improvising past a broken plan is how scope escapes.
- When you discover work the plan missed, decide explicitly: fold it in (it blocks the
  goal), or note it and leave it (it does not). Do not do it by reflex.
- When a step reveals the goal itself was wrong, stop and go back to step 1. That is a
  success — it happened before the code was written.

## Reporting

Lead with that one-line readiness statement, then the numbered list itself: step, files,
verification. No preamble.

While executing, report per step: what was done, the verification command, its result.
At the end, state the goal as it now stands and name anything you deliberately left
undone.
