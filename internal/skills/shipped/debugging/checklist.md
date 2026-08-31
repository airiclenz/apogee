# Stuck checklist

Read this only when step 2 has stalled — you have a reproducer, you have read the code,
and you still cannot name the line where behaviour first diverges. Work down the list and
stop at the first item that produces a new fact.

## Question the reproducer

- Does the failing command still fail on a clean tree (`git stash`, then run it again)?
  If it passes, the bug is in your working changes, not in the code you were reading.
- Does it fail on the previous commit? Keep halving until you have the first bad commit;
  its diff is usually smaller than the file you were staring at.
- Does it fail in isolation, or only after its neighbours ran? Order-dependent failures
  are shared state, not logic — look for a package-level variable, a cached client, a
  temp directory reused between cases.
- Does it fail on a second machine, or under a different clock, locale, path or user?
  A failure only you can produce is about your environment, and that is the bug.

## Question what you think you read

- Is the code you read the code that runs? Check for a second definition, an override, a
  build tag, a generated file, a vendored copy, a stale build artefact.
- Add one print (or one breakpoint) at the place you believe is correct and prove it.
  Belief is not evidence; a printed value is.
- Bisect the data, not just the code: halve the input until the smallest failing case is
  left. What survives the halving is the trigger.
- Read the error text again, whole. Skipped stack frames and truncated messages hide the
  answer more often than the code does.

## Question the boundaries

Most stubborn bugs sit at a seam, not inside a function. Check each one you cross:

- Types and encodings: a value parsed twice, an empty string that meant "absent", a
  number that arrived as text, a time without a zone.
- Ownership: two writers to one file, map or field; a slice aliased after append; a
  buffer reused after it was handed away.
- Ordering: something read before it was written, a callback that fired twice, a
  cancellation that arrived first.
- Errors: a return value dropped, an error wrapped and then matched by string, a failure
  logged where it should have stopped the work.

## When to stop and report

Stop after two consecutive attempts that produce no new fact. Report, in this order: the
reproducer, everything you have ruled out and how, your best remaining theory, and the
one experiment that would confirm or kill it. A precise dead end is a result.
