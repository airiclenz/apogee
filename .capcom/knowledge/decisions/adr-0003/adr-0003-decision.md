---
title: ADR 0003 decision
---

A Mechanism is a **self-contained module** that (1) implements the interface for one
Hook point, (2) supplies a Mechanism descriptor, and (3) declares its
**ordering constraints** relative to other Mechanisms. The loop discovers Mechanisms from a
**registry** and orders them at each hook point from their declared constraints — it does
**not** hardcode a sequence. The result is a **deterministic total order**: a topological
sort of the declared constraints with a **stable tiebreak by canonical Mechanism ID**, never
Go's randomized map iteration — so a given Mechanism set always fires in the same order and
runs are reproducible. apogee-sim's existing `OrderingConstraints` is the seed of this; the
descriptor's incompatibility set governs stacking.

Adding a Mechanism touches the registry + the new module, not the loop.
