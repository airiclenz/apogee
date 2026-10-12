---
title: ADR 0084 decision
---

**1. Both standing parts are measured.** `Allocate` takes the measured token size of the
system-prompt part and of the file-context part (`context.Measured`). A measured part reserves its
measurement plus **10% headroom**, rounded up, floored at **2%** of the working room. `(*Agent).budget()`
does the measuring: it renders the standing blocks it already renders, splits them at the
`context files` row — that row is the file-context part, every other row together is the
system-prompt part — and converts each through the Agent's own `TokenEstimator`, so the reservation
and the triggers that read it share one chars→token scale. The measurement is taken on every
`budget()` call and never memoised: the standing render moves on `SetMode`, `SetScratchDir`, the
date, delegation seats, `ExtraReadRoots` and every `task_list` call.

**2. The 15% / 25% fractions survive only as the unmeasured fallback.** A caller that passes a
negative measurement (`Measured{-1, -1}`) keeps today's fraction for that part. Since `budget()`
always measures, that path is test-only in the shipped engine. A zero measurement is *measured
zero*, not unmeasured, and floors at 2%.

**3. History keeps a floor of 50% of the working room.** The measured reservations are honoured
only down to that floor; when they would push History below it they are scaled down together,
proportionally, so the parts still sum to the working room exactly. Standing content larger than
half the working room is the oversize notice's job to report — never a reason to switch the
structural reducers off. A non-positive History still reads as "window unknown".

**4. The oversize notice keeps a fixed advisory ceiling.** `Allocation.StandingAdvisory` carries
the unchanged 15%-of-working-room share, and `ContextFilesReport.Oversize()` compares the whole
standing content against that, never against the measured reservation. Wording and trigger are
unchanged; the ceiling is a warning line, not an allocation.

**5. One History for every reader, capped where History is produced.** `(*Agent).budget()` sets

    History = min(alloc.History, max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens))

whenever a window is **advertised** (`context-window:`); with no advertised window History stays
the uncapped allocation, exactly as before — a `working-window:`-only session must not collapse
History to the fold's minimum transcript. `deriveGrowthBounds` stays uncapped, and the fill notice,
Prune, `HistoryFill`, the fold trigger and `structuralFloor` all read that ONE number.

**6. Prune band 70% / 50%, six protected Turns.** `pruneHighFraction = 0.7`, `pruneLowFraction = 0.5`,
`PruneKeepTurns = 6` — code constants, not configuration. The band still spans 20 points because a
pass rewrites committed history and invalidates the upstream prefix cache (ADR 0023 §6): one
invalidation per pass is the thing being amortised.

**7. No new configuration keys.** The shares, the headroom, the floors, the band and the protected
window are code constants documented in `CONTEXT.md`; `prune-tool-results:` stays the only key in
this area.
