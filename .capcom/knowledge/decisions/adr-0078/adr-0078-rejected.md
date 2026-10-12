---
title: ADR 0078 considered and rejected
---

- **A per-vendor provider package (`provider/anthropic`, `provider/openai`)** — the shape Crush and
  goose ship. Rejected: it duplicates the retry, fault, logprobs and capture machinery the one
  Client already carries, and it invites a vendor table where a protocol-family switch is all the
  facts support. A codec is the narrowest seam that closes the gap.
- **Detecting the wire from the endpoint URL** — `api.anthropic.com` is one host; Ollama and
  proxies speak Messages on any host. A heuristic would be wrong exactly where the key is needed
  most, and a wrong wire fails with a 404 rather than a refusal that names the fix.
- **Calling the key `dialect:`** (the bead's original spelling) — ADR 0060 already spent "dialect"
  on the effort spelling, and the two keys sit side by side on the same entry. One word per concept.
- **Requesting `thinking` and dropping the signed blocks on replay** — the API refuses the request;
  the alternative, storing the signature on the transcript, touches the session record and is its
  own decision.
- **Auto-mapping `effort-dialect:` onto the anthropic wire instead of refusing it** — a key that
  is silently ignored reads as configured while doing nothing, the failure ADR 0060's enum refusal
  exists to prevent.
- **A Recorder that also records Messages** — no live Messages server is part of the bench rig,
  and the stub's Messages route is scripted, not replayed.
