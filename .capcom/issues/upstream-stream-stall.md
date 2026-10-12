---
title: "Upstream stream stall / in-band 504: one fixed re-stream, no idle timeout, faulted delegate loses its whole context"
status: done
type: bug
priority: 2
plan: 2026-09-20 - 04 - upstream-stall-resilience-plan
aliases: [apogee-60x]
created: 2026-09-20T13:25:59Z
closed: 2026-09-20T17:42:39Z
reason: plan 2026-09-20 - 04 items 1-7
---

Observed 2026-09-20 in session 20260920T120456Z-82db7883 (openrouter-ds4, deepseek-v4-flash via provider Relace, every call 3-8 min): sub-agent "L1 internal-floor-2" had done 34 tool calls / 13 turns / ~2.3M prompt tokens over 55 min when its next streaming call sat 520 s with no tokens and the wire returned an in-band error chunk {"choices":[],"error":{"code":504,"message":"Upstream idle timeout exceeded"}}. apogee surfaced `loop: apogee: upstream in-band error 504 ...`, the delegate faulted (`sub-agent faulted before finishing the delegated task: ...`), no receipt reached the parent and the child's conversation was discarded. Only the draft findings file the skill prompt had told it to write early survived.

Why apogee could not ride it out:
- HTTP retries (internal/provider/client.go:440-487, maxRetries=2, 200ms/1s base, Retry-After<=30s) cover 429/5xx/transport faults BEFORE the first byte only. Streaming (stream.go:100) has no per-attempt timeout and no inter-chunk idle timeout (stream.go:80-81, pinned by stream_test.go:1023) — a silent stream waits for the aggregator's cut.
- The in-band 504 is classified Retryable (fault.go:53-67, stream.go:201-213), but the loop allows exactly ONE re-stream per Turn after a fixed 1 s hold-off (loop.go:425-457 restreamSpent, restreamHoldoff loop.go:362); the second fault of any class goes emitLoopFault -> turnFailed -> endAbandoned (turn.go:183-199).
- A faulted delegate is not retained: retention + foldForParent run only under res.StepCapped (subagent.go:1043-1053, agent.go:900); `continue` after a fault answers unknownContinueResult (subagent.go:898-903, :184). The child's conv is still intact in memory when the Exchange closes Faulted — it is thrown away only when runSubAgent returns.
- No config key exists for retry count, request timeout or stream idle timeout (only judge/probe/title pass WithRequestTimeout/WithMaxRetries).

Proposed shape (each its own item; A and C are the minimum):
A. Stream idle timeout (`stream-idle-timeout`, generous default, reasoning deltas count as activity) that cuts a silent body read as a Retryable transient, plus a small re-stream budget (2-3) with exponential hold-off replacing the single restreamSpent latch.
B. Retain a faulted delegate the way a capped one is retained — fold via foldForParent (or keep the raw []domain.Message so `continue` re-seeds without re-spending the tokens) and add the continue line to the fault result.
C. Fault result names a surviving draft when output_path exists on disk (`[draft output at %s written before the fault]`), same note slot the cap path uses.

Invariants: engine-side only (ADR 0031); nothing new in front of the model beyond the continue/draft lines the cap path already ships; Bypass-neutral. Related: ADR 0018 (overflow fold + one retry), ADR 0046 (cap retry latch), ADR 0078 (fault classes); apogee-2un (finished siblings lost on cancel — the pool commits only after the group joins, dispatch.go:398-408) is the neighbouring loss and stays its own bead.
