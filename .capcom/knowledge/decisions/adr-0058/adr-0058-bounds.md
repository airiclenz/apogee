---
title: ADR 0058 bounds (stated, not separately ratified)
---

- This is resolution only. `ThinkingProfile` stays one struct on `Config`; the engine still
  receives one resolved profile and sees no layering, exactly as ADR 0057 promised.
- The `/effort` session override (ADR 0050 decision 5) is untouched — it layers above the
  resolved profile's effort, whichever tier supplied it.
- The wire mapping (ADR 0050 decision 2) is untouched — which tier supplied the effort word has
  no bearing on how the provider Client emits it.
- ADR 0057 decision 5's "three axes" still reads as three axes; one of them resolves in two
  halves. No fourth axis is introduced, and the profile's glossary term does not widen.
