---
title: ADR 0007 context
---

The loop is an embeddable, steppable library (ADR 0001):
the bench steps it one unit at a time, snapshots, and resumes; the TUI streams it and lets a
user interrupt. For any of that to be well-defined, the unit of advance and the points at
which state is safe to capture must be precise — and the loop hosts third-party tools and
Mechanisms (ADR 0002),
which can fail in ways that must not take down the embedding host.
