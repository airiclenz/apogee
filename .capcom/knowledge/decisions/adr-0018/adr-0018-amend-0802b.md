---
title: "ADR 0018 amendment of 2026-08-02 (second): with no window, every bound shares the fold's ceiling"
---

The amendment above fixed the fold and left the audit's other three inert bounds alone, on the
grounds that a working reactive path is enough. It is not, and the gap is in this ADR's own §8: the
fold's transcript render keeps the most recent message **unconditionally**
(`context.renderBudgetedTranscript`), and with the clamp disabled the most recent message can be a
whole-file read of any size. Bounding the fold therefore rescued every unknown-window session
*except* the one a giant tool result had already poisoned — which is the shape §8 exists for. Left
inert alongside it, the predictive guard removed the only cover against a 400 the provider cannot
classify (§7's second justification, and the reactive path cannot substitute for it), and the
boundary trigger let the history grow until the server rejected it, paying a wasted round-trip per
overflow.

**All four bounds now key on one number when no window is known** —
`compactUnknownWindowTranscriptTokens`, through the same calibrated chars→token ratio. With no
window there is nothing to *allocate* across a request's parts (`context.Allocate` returns the zero
Allocation for precisely that reason), so the parts' fractions are meaningless and the single
meaningful rule is that nothing the fold can shed — the transcript (§7's guard and S2's trigger),
nor one tool result about to enter it (§8) — may exceed what the fold can actually render. Keying
the clamp to the fold's own budget makes the two meet exactly: a result that survives the clamp is,
by construction, one the fold can still shed. The known-window arithmetic in §7 and §8 is untouched,
and the ceiling is a fallback rather than a second threshold a budgeted session must also clear.

**The ceiling being a TRANSCRIPT budget also fixes what each bound measures against it.** §7's guard
measures the whole request — messages *and* tool menu — against the working room, which is the right
quantity when a window was allocated across those parts. Against this ceiling it is the wrong one:
the fixed costs every request carries (the tool menu, the standing system content of ADR 0023) are
not transcript and no fold can shrink them, so a request-sized measure against a transcript-sized
bound fires on a conversation of four short messages and folds it away at every Exchange without
ever getting under the bound — a comfort margin, which §7 forbids outright, and one with no
`compactSat`-style latch to stop it. The default 19-tool menu alone is ~11.5k characters ≈ 3.8k
tokens at a code-heavy 3.0 chars/token, already past the ceiling with an empty conversation. On the
unknown-window path §7's guard therefore measures the conversation alone — byte for byte the
quantity S2's trigger measures, and the quantity the fold rewrites — while the known-window path
keeps the whole-request measure unchanged.

Two consequences the owner accepted with the finding:

- **A large window a server never advertised is managed as if it were small.** The assumption is
  pessimistic by design (it fits llama.cpp's 4096-token default `n_ctx`), so an unbudgeted session
  against a big remote folds and clamps earlier than it needs to. That is the safe direction — the
  alternative is the unbounded growth this amendment removes — and it is recoverable by the user
  rather than silent: `context-window:` replaces the assumption with the truth.
- **The compaction saturation notice now names that remedy too**, on the same rule as the give-up
  event (`unknownWindowRemedy`): with no window there is no "allocation" the notice can honestly
  refer to, and it is the one event a user sees when an assumed ceiling — rather than a real
  window — is what is folding their history.

The Budget itself is unchanged and stays honest: `ContextLimit` is still 0 and `Allocate` still
allocates nothing, so hooks, the context gauge and the window-gated Mechanisms (`library`,
`guided_decomposition`, `tool_result_cap`) all remain inert on an unknown window and never steer on
a guess. The assumption lives only in the engine's structural bounds, which is where a wrong guess
costs a lossy fold rather than a wrong instruction to the model.
