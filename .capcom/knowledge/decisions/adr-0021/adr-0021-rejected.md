---
title: ADR 0021 considered options
---

- **One `apogee probe` that reports the host always and probes the model whenever the endpoint
  happens to be reachable** (Q1's alternative). Rejected: it makes a token-spending, disk-writing,
  automatism-enabling act fire on the basis of *whether a port answered*. The costs are too
  asymmetric to hide behind one noun, and "it wrote a fingerprint because your server was up" is
  not a sentence this project wants to have to explain.
- **`probe` as a bare parent with no own `RunE` (help only), everything in children.** Rejected
  as a papercut: the overwhelmingly common question ("what is this box doing to Auto?") would
  cost a second word for no gain, and the free half is exactly the half that should be reflexive.
- **Print-only v1, persistence as a recorded follow-on** (Q2's alternative). Rejected on the code
  above — see §3.
- **Writing the suggested profile into `config.yaml`** (with a backup, or behind `--write`).
  Rejected: `configwrite.go` exists and could do it safely, but the offer precedent is the right
  one — an automatic profile edit changes how the engine speaks the wire on the next run, from a
  command the user ran to *look* at something.
- **Building adaptive prompt complexity now, default-off and catalogued** (Q3's alternative).
  Rejected for this plan: a catalogued Mechanism owes the catalogue a bench-validation entry
  (ADR 0009), and manufacturing a row we cannot yet fill trades a real design for a placeholder.
  It stays a `ISSUES.md` follow-on with its design recorded — and if it is ever built, it is
  default-off and bench-gated, never a rider on the probe.
- **A `probe --json` machine format in v1.** Deliberately not decided here; the report is
  human-first and a stable machine format is additive. Named so a later ADR is not surprised.
