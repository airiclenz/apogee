---
title: ADR 0001 decision
---

The eval/simulation harness stays **out of the `apogee` binary** and lives in
**`apogee-sim`** (the bench). The bench reaches the agent by **importing
`github.com/airiclenz/apogee` as a Go module** and driving the agent loop as an
**embeddable library**, in-process — not by shipping the harness inside the binary, and
not over a serialized control protocol.

To make this possible, the loop is designed as an embeddable, steppable component:

- it owns **no process-global state and no implicit global filesystem state** — an agent
  is a value the caller constructs, and *every* state root (Library, sessions, config) is
  injected via `Config`, never assumed at `~/.apogee`;
- **conversation state is a cleanly copyable value** (no live handles, no process globals)
  — a hygiene property Apogee owns; the bench builds forking *on top of* it, but the loop
  itself exposes no fork;
- it exposes **Hook points as Go interfaces**, so the bench can register a temporary
  **experimental hook** to test a behaviour that is not yet a Mechanism, and the loop
  can be **stepped** one Turn at a time;
- session **snapshot/resume** (a real user feature) doubles as the bench's
  snapshot/restore primitive.

The shipped binary links none of the bench's code.

**What Apogee exposes vs. what the bench composes.** Apogee exposes only **snapshot/resume**
(a real user feature) and **clean-library hygiene** (Config-injected state roots, no process
globals, copyable conversation state, an injectable tool registry, Hook-point interfaces).
**Forking is *not* an Apogee feature** — the bench *composes* forking, record/replay, and
counterfactual scoring on its side from those primitives (deep-copy the copyable state, copy
the sandbox directory, drive two branches from byte-identical state). No fork or record/replay
code ships in the binary. (External effects — MCP, network — are non-forkable; the bench
disables them with deterministic stubs for v1 — see
ADR 0008.)
