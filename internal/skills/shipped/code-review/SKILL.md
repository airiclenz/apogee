---
id: code-review
displayName: Code Review
summary: Review for correctness first — report only findings you can trigger and have verified against the tree, and leave style, taste and speculation out.
description: >-
  Use when reviewing code: a diff, a pull request, a branch, a file, or a change you
  just made. Puts correctness ahead of everything else, demands a realistic trigger for
  every finding, requires each finding be verified against the code as it actually is
  before it is reported, and suppresses style nits, spelling and speculative
  "consider maybe" remarks that bury the real defects.
triggers:
  - review this
  - code review
  - review my changes
  - review the diff
  - review this PR
  - look over this code
  - check this code
  - any bugs in this
  - is this correct
  - what did I miss
  - critique this
---

# Code Review

The value of a review is the ratio of real defects found to words written. Ten
confident nits bury one data-loss bug. Report less, and mean it.

## What to read

- Name the review unit before reading: a diff, a branch against its merge base, a PR, or
  the working tree. Say which. 'The whole change' means that unit, and a finding outside
  it is out of scope.
- Read the whole change before commenting on any of it. A line that looks wrong in
  isolation is usually answered three files later.
- Read the code the change CALLS and the code that calls IT. Most real defects live at
  the seam, not in the new lines.
- Read the tests. What they assert is the contract; what they omit is where the bug is.
- If you do not understand what the code is for, find out before judging it. "I would
  have done this differently" is not a finding.

## Priority order

Work down this list. Do not spend the review's budget on the bottom of it.

1. **Correctness** — it does not do what it is meant to do. Wrong logic, inverted
   condition, off-by-one, wrong operator precedence, a case that falls through.
2. **Data loss and corruption** — a write that clobbers, a migration without a
   rollback, an unchecked truncation, a cache that serves stale state as fresh.
3. **Security** — unvalidated input reaching a shell, a query, a path, or a
   deserializer; a secret in a log or a literal; an authorization check that is missing
   or that runs after the effect.
4. **Error handling** — a swallowed error, an error returned but ignored by the caller,
   a partial failure that leaves inconsistent state, a panic on untrusted input.
5. **Concurrency** — a shared value written without synchronisation, a lock held across
   a call that can block, a goroutine or task with no way to stop, an ordering the code
   assumes but does not enforce.
6. **Resource handling** — a file, connection, or handle not closed on every path; an
   unbounded buffer, queue, or retry loop.
7. **Contract breaks** — a changed signature, output shape, exit code, or on-disk
   format that existing callers or users depend on.
8. **Missing tests on the risky path** — but only where the risk is concrete, and name
   the case, not the coverage number.

## Every finding needs a trigger

Before you write a finding, state to yourself the specific circumstance in which it
actually bites: the input, the sequence, the configuration.

- If you cannot name a realistic trigger, it is not a finding. Delete it.
- "This could theoretically overflow" with no path that reaches it is noise.
- A defect that requires the caller to already be broken belongs, at most, in a
  one-line note — not in the list.
- Say the consequence, not just the mechanism: what does the user or the data lose?

## Verify against the tree, not against memory

The most common review defect is a finding that is already handled elsewhere.

- Open the file and read the actual current lines before claiming anything about them.
  Do not review from the diff hunk alone — the guard may be five lines above the hunk.
- Search for the check you think is missing before reporting it missing. It may be in
  the caller, in a constructor, in a validation pass, or in middleware.
- Where you can, prove it: run the test, run the command, write the two-line reproducer.
  A verified finding needs no hedging.
- If verification is impossible, say so explicitly and mark the finding uncertain.
  Never present an unverified guess in the same voice as a proven bug.

## What NOT to report

- Formatting, import order, line length, brace style — the formatter owns these.
- Spelling in comments, and naming preferences that are not actively misleading.
- Rewrites motivated by taste: a different pattern, a different abstraction, a
  different library, when the existing one is correct.
- Speculative performance without a measurement or an obvious complexity blow-up.
- Generated, vendored and third-party files — unless the change is to the generator or
  the pin itself. Read them for context, never for findings.
- Anything the project's own conventions explicitly settle. Follow the repo's
  conventions, do not relitigate them.
- Praise padding. One sentence of context is enough; the review is for the defects.

## Reporting

For each finding, four lines and no more:

- **Where** — `path:line`.
- **What** — the defect, in one sentence.
- **When** — the trigger that makes it bite, and the consequence.
- **Fix** — the smallest change that removes it.

Order the list by severity, not by file order. Group the merely-worth-noting items
under a short "minor" heading, or drop them.

Close with a one-line verdict: what blocks the change, what is optional, and — if you
could not verify something — exactly what you could not check.
