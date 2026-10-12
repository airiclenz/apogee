---
title: ADR 0046 context
---

The engine already decides how big a reply is allowed to be and then never says so. `Allocate`
(`internal/context/budget.go`) splits the discovered window into a `ResponseReserve` — 20% by
default — and the working room the prompt's parts draw from, and `budget()`
(`internal/agent/loop.go`) hands that split to every Turn. But neither `domain.NewRequest` site in
the loop sets any sampling, so `MaxTokens` reaches the provider nil, the server is told
`n_predict: -1`, and the reply's only real ceiling is the context wall. The reserve and the request
disagree: one reserves room for a reply of a stated size, the other asks for a reply of unbounded
size.

That gap is invisible while models answer in a few hundred tokens and expensive the moment one does
not. On 2026-08-12 a `/security-audit` run on qwen3.6-35B-A3B (per-slot `n_ctx` 98,304) put one
sub-agent request at 20,653 reasoning tokens over ~50 minutes at ~6.2 tok/s, still generating, with
~81 further minutes of window to burn before the wall stopped it. At that wall the reply —
reasoning only, no visible text — would have failed through `reviewedOutcome` as `upstream returned
an empty reply (finish: length)`: a message that names neither a ceiling nor the twenty thousand
tokens spent reaching it, and that calls the largest reply of the session empty. A thinking model
makes the unbounded request a routine hazard rather than a corner case, and no Mechanism was ever
going to close it: `SamplingParams` (`internal/domain/hooks.go`) is a pre-request HOOK's surface,
and nothing in the engine populates it.

This record ratifies the calls the owner made that day. It supersedes nothing.
