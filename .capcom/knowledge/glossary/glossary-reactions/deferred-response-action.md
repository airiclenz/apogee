---
title: "Deferred Response Action vs Request-prep Hint: two sources of a pre-request injection, kept distinct"
---

**Deferred Response Action vs Request-prep Hint**:
Two sources of a pre-request injection, kept distinct because they are debugged
differently. A **Deferred Response Action** is an `Outcome.Defer` a post-response **Reaction**
returned on the *previous* turn, consumed from session state this turn
(look in **session state**). A **Request-prep Hint** is derived fresh from conversation
history at the start of *this* request (look in **conversation history**). Both reach the model at
the `pre-request` **Moment** and are tracked uniformly. A Deferred Response Action is
**Exchange-scoped**: it is a decision about the *next request of the same conversation flow*, so
the queue is cleared whenever an Exchange ends (a completed final answer, a fault, or an abort) and
is truncated-then-restored when a cancelled Turn is rolled back — a stale directive never crosses
an Exchange boundary or survives as two contradictory copies.
