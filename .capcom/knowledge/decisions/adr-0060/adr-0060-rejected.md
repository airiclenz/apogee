---
title: ADR 0060 considered and rejected
---

- **A bind-time `/apply-template` probe** to learn the dial — already rejected by ADR 0050
  (llama.cpp-only, a request per bind, no fallback story) and re-rejected here: the two
  payloads discovery already fetches answer the question for free.
- **Auto-detecting the top-level `reasoning_effort` dialect** — OpenAI and Groq advertise no
  passive tell on `/v1/models`, so any detection would be a provider-name or URL heuristic.
  The `effort-dialect:` key covers that class explicitly instead of guessing.
- **A per-request UI-facts channel into the engine** (reported levels, default) — the host
  already receives them on the Beat; a second path would duplicate state and give the engine a
  fact it has no use for.
- **A greyed-out disabled `/effort` row in the menu** — new machinery for one command, where
  omission already reads correctly and the typed verb still explains itself.
- **Keeping the `/effort <level>` text grammar beside the picker** — two spellings of one
  question, and the word grammar cannot express a model's reported vocabulary.
- **A per-family dialect table** — ADR 0050 decision 2's standing refusal. Dialects grow per
  *sighting*, and the endpoint, not the family, is what determines the wire shape.
- **Clearing the override on every model switch** — rejected in ADR 0050 decision 5 and still
  rejected; decision 8's exception fires only on a reported set that provably excludes it.
- **Threading the dialect into the out-of-band title/naming call** — deferred, not denied: the
  namer keeps its `chat_template_kwargs` `off` mapping, so on an OpenRouter thinking model a
  generated title may reason to the cap. Pre-existing edge, recorded rather than fixed here.
  *Resolved 2026-08-25* — the namer now states the dialect the beat observed (`title.Prompt`
  takes it; the composition root reads it off the same live latch the rebind path writes), so
  `off` reaches every dialect in its own shape and the fallback re-send is skipped where the
  dialect put nothing on the wire.
