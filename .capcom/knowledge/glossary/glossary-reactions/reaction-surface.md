---
title: Reaction surface
---

**Reaction surface**:
The **origin × class** policy matrix every *"apogee should react when X happens"* is placed in
(ADR 0076 D2). Sibling
to ADR 0064's placement rule for a
new sentence of **guidance**; this one places a new **Reaction**. One
reaction takes exactly **one cell**:

| origin ↓ / class → | observe | advise | gate | shape (view) | shape (work) |
|---|---|---|---|---|---|
| **engine** (builtin or bench-armed) | ✓ | ✓ | ✓ | ✓ | ✓ |
| **user** (configured) | ✓ | ✓ | ✓ | ✓ *reserved* | ✗ |

- **observe** — no return; nothing the model sees changes. Runs after the fact, off the loop's
  path, never waited on.
- **advise** — returns text the model sees: fenced, capped, **fail-open** (no advice is just no
  advice), landed as a trailer on the closing tool result or at the request's tail, never in the
  system prompt, **ephemeral** on resume, every span on the **provenance ledger**.
- **gate** — returns allow / deny / ask at `pre-tool-exec` as a stage of the **Approver**, never
  as a seam edit; deny text stays engine-authored. A script saying No is the same act as a human
  saying No. A gate that fails or times out **escalates to ask**.
- **shape (view)** — edits what the model *sees* (`post-tool-result`, `pre-request`,
  `history-rewrite`). The seven Floor guards are the engine builtins here. The
  **user** cell is in the matrix but **reserved** — no case, no ledger design for edits — until its
  own grill.
- **shape (work)** — edits what the model *does* (tool-call arguments at `pre-tool-exec`).
  **Engine only**: once a later reaction can mutate arguments an earlier gate approved, no gate is
  sound.
- **continue** is **not a class**; nothing may take the loop's next step for it.

The line the user row draws: a user Reaction may change the model's **view** and may say **No**; it
may never change the model's **work** or make its **choices**. Of the user row, **observe**,
**advise** and **gate** ship — a `reactions:` entry's `run:`, `advise:` and `gate:` keys — and
**shape (view)** is the one reserved cell. **Bypass** switches off the advise
and shape classes of user and bench-armed origin, plus the engine-origin context-fill notice (the
one advise builtin), and nothing else. A **Tool** is not a cell: the
model asks for it by name, and it is admitted on a **replicated** ask across models
(`docs/design/tool-surface-findings.md`,
ADR 0057 D3).
_Avoid_: "rung" / "the four homes" (the exclusive ladder this matrix replaced), "hook" as the name
of this surface, "Mechanism" (retired — see Retired terms).
