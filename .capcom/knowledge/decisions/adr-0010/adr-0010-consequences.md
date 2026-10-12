---
title: ADR 0010 consequences
---

- **P1.0 is the first Phase-1 task and a precondition of every other body.** The throwaway
  P0.6 internals in the root package (`loop.go`, `conversation.go`, `registry.go`) move into
  `internal/{agent,domain}`; the public methods in `apogee.go` become aliases / forwarders.
  P1.0 is a **pure move** — the existing 12 tests must still pass — landing with verify
  (`gofmt` / `vet` / `build` / `test -race` + the 6-target cross-build) green before any real
  provider/loop work.
- `internal/agent.Responder` (the provider seam) moves to its real home, `internal/provider`,
  beside the HTTP client that will implement it. The wire types
  (`provider.Request` / `RawResponse` / `Message`) stay provider-local and root-type-free; the
  loop translates `domain` conversation state ↔ wire shape at that boundary. The doc-only
  `internal/agent` placeholder is superseded by the real engine package.
- The plan §3 layout (loop under `internal/agent/{loop,subagent,modes}`) is **confirmed**, not
  changed — P0.6's root-package loop was the deviation. The only structural addition is
  `internal/domain` beneath everything, plus the explicit "internal never imports root"
  invariant.
- **New cost:** the root facade carries mechanical re-export boilerplate (~30 consts, the
  sentinels, ~6 constructors). One-time, drift-guarded by the `example_test` completeness
  check — the price of an engine that lives in its own testable package.
- **CONTEXT.md gains a code home.** `internal/domain` is the glossary as Go: adding a domain
  term is adding a `domain` type and (if public) one root alias — a two-line mechanical
  extension that keeps the public surface and the language in lockstep.
- The bench's isolation and fake-driven access pattern (ADR 0001) becomes the **native** shape
  of the system: the engine consumes `domain` ports, so driving it against a fake
  provider / tool / Mechanism is ordinary, not special-cased.
- **Stability (ADR 0001 §18).** The public surface *is* the set of root aliases. Variants stay
  additively extensible: a new `Event` variant is a new `domain` type + a new root alias (a
  minor bump), with the sealing (an unexported method on the `domain` interface) preserved
  through the alias.
