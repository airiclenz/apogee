---
title: "Write the Reaction stage 2 plan: reactions: config in the global file (gated on stage 1 landing)"
status: done
type: task
priority: 2
deps: [reaction-core-plan]
aliases: [apogee-pjx]
created: 2026-09-07T20:11:06Z
closed: 2026-09-09T05:58:55Z
reason: Stage 2 plan written and executed; archived as docs/plans/archived/2026-09-08 - 00 - reaction-core-stage-2-plan.md (cebd60fd).
---

Second plan after docs/plans/2026-09-07 - 01 - reaction-core-stage-1-plan.md lands: ADR 0076 D10 (reactions: list, embedded → ~/.apogee → repo layers, execution keys proposed-not-live, adoption pin + 'apogee reactions adopt' / settings row, repo config path on the write deny list), D11 (hooks: alias through one migration table + one-time load notice; approval-requested/approval-decided/turn-finished notices), D3/D5/D6/D7/D8 user cells (gate as Approver stage 5s escalate-to-ask; advise 10s fail-open 8 KiB trailer slot, provenance ledger ephemeral on resume, terminal redaction, exec fence; fixed-text machinery bench arm in apogee-sim gates advise), /settings 'what the model saw this Turn'. Written via /implement-plan Write mode once stage 1 is archived.

## Log

- 2026-09-09 beads: SCOPE NARROWED by the 2026-09-08 stage-2 grill (ADR 0076 amendment A1-A9). Stage 2 = GLOBAL FILE ONLY: reactions: key (user origin only; a builtin id is a load error), entry schema (id/on/run polymorphic/timeout/workspace/enabled; advise+gate rejected as not-yet-shipped), auto-migration of hooks: -> reactions: in the user's file via the migrateLegacyConfig idiom (backup + atomic rewrite + one note; refuses if both keys present; strips dead mechanisms: key), hard rename approval-waiting -> approval-requested + new approval-decided + APOGEE_HOOK_* -> APOGEE_REACTION_* + payload hook -> reaction, five seam-closing notice Moments (sink-only, no headless kinds), internal/hooks -> internal/reactions taking []domain.Reaction observe rows (keeps its EventSink seat), one SetReactions generation swap retiring SetBypass/SetFloor/Runner.Replace, one read-only structured /settings reactions row, validated-sets: deleted, docs/manual/hooks.md -> reactions.md. REPO LAYER + adoption pin are stage 2b -> apogee-089. USER CELLS (advise/gate) are stage 3 -> own bead. Write via /implement-plan Write mode once stage 1 is archived.
