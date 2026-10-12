---
title: ADR 0058 context
---

ADR 0057 decision 5
made Model-profile resolution axis-wise: each of the three axes — tool-call format, thinking
channel, tool roster — takes the nearest layer whose matching entry spells it, so a tools-only user
entry no longer wipes the wire shape the shipped table carries. Inside the thinking axis the same
trap survived one level down. `Entry.spellsThinking` was true for any non-zero `ThinkingProfile`,
and `Resolve` then took that entry's WHOLE thinking half — `Style`, `Start`, `End` and `Effort`
together. A user entry spelling only `effort:` over the shipped gpt-oss profile therefore won the
axis outright and dropped harmony parsing with it: `Style` resolved to `""`, `Source` was the user
tier, and nothing was announced.

That entry is not an edge case — it is a taught idiom. `README.md` sells "reasoning effort set per
model" as a headline feature, and
ADR 0050 put the dial
on the thinking axis precisely so it rides the profile's resolution. The most natural
edit a gpt-oss user makes to their config silently broke the parsing the shipped table exists to
provide. Recorded as an open defect at the close of the per-profile tool rosters plan
("literally ADR 0057 §5 compliant, so a design call to settle"), settled by the repo owner on
2026-08-24 in the refocus open-items plan.
