---
title: "Circuit breaker: the backstop refusing an exact tool call after three straight failures"
---

**Circuit breaker**:
The always-on backstop that refuses an exact tool call (same tool, same arguments) once it has
failed three times back to back with no other call executed in between; the call is allowed again
as soon as a different call runs, and the refusal tells the model so. Distinct from the
**tool-loop breaker** Floor guard, which steers a repeated Turn rather than refusing a call.
_Avoid_: tool-loop breaker (a different guard), "refused forever".
