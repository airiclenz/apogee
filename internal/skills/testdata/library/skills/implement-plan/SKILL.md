---
name: implement-plan
description: Executes an implementation-plan document item-by-item, with independent verification and one commit per item. Safe to re-run - items already marked done in the plan file are skipped, so the plan file itself is the resume state. Also WRITES new plan documents in the house format — on "create a plan", "write a plan file", "save this as a plan" it saves the plan under docs/plans/ and STOPS — the plan-writer never implements. Use when the user says "implement this plan", "execute the plan", "work through the plan file", "continue the plan", wants a multi-item plan executed without filling the context window, or asks for a plan file to be written. Do NOT use for a single feature (use feature-implementation).
argument-hint: "execute: plan file path (optionally: start at item N; with skills: name1, name2; with routing: hard=<model>, default=<model>) — or write: the goal to plan"
---

Fixture body — see testdata/library/README.md.
