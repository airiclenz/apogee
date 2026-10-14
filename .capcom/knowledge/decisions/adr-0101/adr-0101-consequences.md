---
title: ADR 0101 consequences
---

- A run that finds a regression grows by the fix, or stops at closeout. Plan and skill
  closeouts check for one.
- The announced-paths e2e suite (`cmd/apogee`) holds the second kind of regression, so a change
  to an announced path fails until its tools accept it.
