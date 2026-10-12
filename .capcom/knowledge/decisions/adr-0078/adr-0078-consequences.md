---
title: ADR 0078 consequences
---

- `ServerEntry` gains `Wire` (`yaml:"wire,omitempty"`); `ValidateServers` refuses an unknown wire and
  the anthropic-plus-dialect pair; the seeded template, `docs/manual/configuration.md` and
  `CONTEXT.md` (**Wire**, and **Upstream** widened past "the OpenAI HTTP surface") state the key.
- `internal/provider` grows the codec seam and the Messages encoder, decoder and SSE parser; the
  dial seams in `cmd/apogee` thread the wire; `stubllm` serves `/v1/messages`; the Inspector reads
  Anthropic SSE. Each is its own plan item under `docs/plans/2026-09-16 - 03`.
- Signed-thinking replay on the anthropic wire is filed as a follow-up bead when the codec lands.
  *(Delivered — see the Amendment (2026-10-02) below.)*
- ADR 0036's entry shape and ADR 0060 decision 3 carry this ADR as the dated widening.
