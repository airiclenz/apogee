---
title: "ADR 0016 runtime-surface realisation of 2026-07-19: authorized refinements"
---

The runtime surface (the first Consequence) was designed against the shipped resolver
reality, and the design grill crystallised four refinements to the Decision's letter:

- **Below medium confidence the surface offers instead of staying silent.** §5's "below
  medium confidence nothing auto-enables" stands — but as specified it made the surface a
  guaranteed no-op: the resolver's only tiers today are weights-hash (high — whose label
  is a `sha256:…`, never equal to a name key) and metadata label (low; the behavioral
  probe that would produce medium does not exist yet), so no shipped entry could ever
  fire — precisely the "evidence's value would never reach users" defect this ADR
  rejected offer-only semantics for. Resolution: automatism stays gated at ≥ medium
  exactly as decided; at low confidence an exact label match emits the per-session notice
  as an **offer** naming the one-line config alias that applies the set. The enabling act
  below medium is therefore always an explicit human decision — §3's own mechanism, not a
  weakening of the gate.
- **The §3 alias is a config surface consulted at any confidence.** `validated-sets:
  alias:` maps a runtime fingerprint label to an entry key. An identity mapping is the
  low-confidence confirm ("my model is what the label says"); a differing mapping is the
  explicit transfer §3 blesses (including from a weights-hash `sha256:…` label). An
  aliased match applies without the confidence gate — the human decision replaces it. A
  dangling alias (no such entry key) is a loud startup error, matching ADR 0015's
  removed-ID posture; it is the user's own config.
- **Whole-set-or-nothing.** A Validated set applies verbatim or not at all: the gate test
  validated exactly that enable set, so a subset or a merge is an *unvalidated* stack and
  must not carry the validated banner. A non-empty explicit `mechanisms:` config means
  manual control (the set is not applied; the notice says so); Bypass suppresses even the
  offer; an entry defective for this binary (unknown Mechanism ID after catalogue
  evolution, now-invalid stacking relations, malformed file) is skipped with a one-line
  warning and the session runs at the floor — never a partial application, never a
  blocked startup for data the user did not write.
- **The decision lives at product wire time, not in the engine.** `cmd/apogee` resolves
  the fingerprint, matches entries, and folds an applying set into
  `Config.EnableMechanisms` before construction. ADR 0015's single enable path is
  untouched, and bench arms cannot be contaminated — a Bypass control arm on a validated
  model stays empty without any opt-out. Embedder access is deferred until a second
  consumer asks.

Storage (embedded shipped entries + user-local `~/.apogee/validated/*.json`, user-local
winning a key collision), notice wording, and drift handling are implementation detail —
see `docs/plans/validated-set-runtime-surface.md`.

---
