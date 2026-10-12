---
title: "ADR 0071 decision 1: The Floor-guard test, and the six that pass it"
---

**A behaviour is a Floor guard when it (a) changes only what the model sees after its own failure,
or shapes the request without steering it, (b) needs no per-model proof, and (c) cannot regress
Bypass.** A Floor guard is not a Mechanism. It is plain engine behaviour, on by default, living in
the `internal/floor` policy package with thin call sites in `internal/agent` — no catalogue row, no
`MechanismID`, no descriptor, no Capability, no strikes.

Six catalogued rows pass the test and are promoted, keeping their decision logic verbatim:

| Was | Becomes | Why it passes |
|---|---|---|
| `tool_use_enforcer` | **tool-use enforcer** | Off-ramp (ADR 0070): fires only after a reply that narrated an action instead of calling the tool. |
| `empty_response_recovery` | **empty-response recovery** | Off-ramp (ADR 0070): fires only after a reply with no text and no tool calls. |
| `validate` | **tool-call repair** | Fires only on a tool call the engine has already rejected as malformed — post-failure by construction. |
| `tool_loop_interceptor` | **tool-loop breaker** | Fires only on an identical repeat Turn — a failure the model has already committed twice — or, since the 2026-09-15 amendment, on an exact A-B-A-B alternation whose repeated pair drew identical results. |
| `tool_result_cap` | **tool-result cap** | Shapes the request (a 40%-budget per-result cap, most-recent Turn protected); says nothing to the model about what to do. |
| `cached_content_intercept` | **read cache** | Intercepts a redundant successful re-read; the one row with measured evidence (`catalogue.md:191`). A re-read counts as redundant only while no write to the file and no call to a non-read-only tool has run since the last read (2026-09-26: a shell command can rewrite a file it never names). |

**Floor guards carry no strikes-3 suppression and no Turn-Budget throttle.** The per-Turn
`maxPostResponseRetries` bound is their only limiter. This supersedes the `SuppressStrikesThree`
posture the promoted rows carried as Mechanisms — the two repair rows, tool-call repair and the
tool-loop breaker, most pointedly — under which three judged-harmful Turns withdrew the guard for
the rest of the Session (`internal/agent/selfreg.go:239-250`). Self-regulation is a *tuning*
instrument: it exists to withdraw help that a model may not want. A Floor guard is not help of that
kind — withdrawing the exit from a failed Turn leaves the model with no exit at all, which is the
argument ADR 0006 and catalogue rule C1 already
made for the two off-ramps' `SuppressExempt` status. The other four inherit it rather than the
strikes counter.
