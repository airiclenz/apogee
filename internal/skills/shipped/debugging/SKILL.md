---
id: debugging
displayName: Debugging
summary: Chase a bug down in four steps — reproduce, isolate, fix the cause, verify — with the rules that keep each step honest.
description: >-
  Use when something is broken and the cause is not yet known: a failing test, a panic or
  stack trace, a crash, wrong output, or behaviour that worked before and does not now.
  Walks the reproduce → isolate → fix → verify protocol, insists the cause be named before
  a line is changed, and requires a regression test that fails against the old code.
triggers:
  - debug this
  - fix this bug
  - why is this failing
  - why does this crash
  - this test fails
  - test is failing
  - stack trace
  - panic
  - it worked before
  - track down the cause
  - reproduce the issue
  - root cause
---

# Debugging

Work the four steps in order. Do not skip ahead: a fix written before the cause is
known is a guess, and a guess that happens to hide the symptom is worse than no fix.

## 1. Reproduce

Get a command that fails, every time, before changing anything.

- Write down the exact command and its exact output. That output is your baseline.
- If the report is vague, narrow it until one command fails: a single test, a single
  input file, a single request.
- If you cannot reproduce it, say so and stop. Ask for the version, the platform, the
  inputs, and the full error text. Never "fix" a bug you have never seen fail.
- Prefer the smallest reproducer that still fails — a failing unit test beats a manual
  click-through, because step 4 can run it again.

## 2. Isolate

Find the one place where the program's actual behaviour first diverges from what it
should do. This is the whole job; steps 3 and 4 are short by comparison.

- Read the error message completely, including the stack frames. Most of the answer is
  usually already in it. Open the file and line it names.
- Read the code before theorising about it. Follow the value that is wrong backwards to
  where it is produced, not forwards from where you expected it to be used.
- Form one hypothesis at a time, phrased so it can be false: "the config is empty at the
  call site" — then check that, and only that.
- Test the hypothesis with evidence, not with a rewrite: print or log the value, run the
  narrowed test, inspect the intermediate state.
- Bisect when the space is large. Halve it: comment out a branch, pin an input, check
  whether an older commit passes. Two or three halvings usually beat an hour of reading.
- When it worked before, find what changed — a recent commit, a dependency bump, a
  config value, an environment difference. Compare working and broken side by side.
- Note what you have ruled out as you go, so you do not walk the same path twice.

Common traps, worth checking early:

- The failure is in the test, not the code — a stale fixture, a wrong expectation, a
  test that depends on order or on the clock.
- The code you are reading is not the code that runs: a stale build, a shadowed file, a
  cached artefact, the wrong binary on PATH.
- The value is empty or nil at the boundary — an unset field, a dropped error, a zero
  value that means "off".
- Two things are true at once and only their combination fails; a single-variable test
  will keep passing.

Stop isolating when you can state the cause in one sentence naming a file and a line.
If you cannot, you have not isolated it yet.

## 3. Fix

Fix the cause you named, and nothing else.

- The smallest change that removes the cause is the right one. Resist the rewrite the
  bug tempted you into.
- Do not paper over the symptom: a nil check that hides a value that should never have
  been nil moves the bug rather than removing it.
- If the cause is a missing case, handle the case. If the cause is a wrong assumption,
  fix the assumption at its source — every other caller has it too.
- Keep unrelated cleanups out of the change. They make the fix unreviewable and they
  make step 4 ambiguous.
- Leave a short comment only where the code's correctness is non-obvious — say WHY, not
  what.

## 4. Verify

Prove the fix works, and prove you broke nothing.

- Run the exact reproducer from step 1. It must now pass. Show its output.
- Run the surrounding test suite. Nothing that passed before may fail now.
- Add a regression test that fails against the old code and passes against the new one.
  If it passes both ways, it is not testing the bug.
- Check the neighbours: if the same wrong assumption is copied elsewhere, fix those too
  or say plainly which ones you left.
- If the fix did not work, go back to step 2 with what you just learned. Do not stack a
  second guess on top of the first.

## Reporting

When you are done, say four things and no more:

1. The symptom, as it was reproduced.
2. The cause, naming file and line.
3. The fix, in one sentence.
4. The evidence: the command you ran and its result.

If you are still stuck, report the same four things with the cause left open, plus what
you ruled out and what you would try next. A precise dead end is useful; a vague
"it should work now" is not.
