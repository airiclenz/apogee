---
Status: accepted
Amends: ADR 0018 §7/§8 (the "~60% of the working room / ~48% of the window" History figures, and the crossover the structural floor rested on); ADR 0026 §8 (the oversize notice reads an advisory ceiling, not an allocation); ADR 0077 (the "48% of the working ceiling" Compaction line)
---

# The Budget reserves what the standing content measures

## Context

The [Budget](../../CONTEXT.md#context-and-history) split the working room — the working ceiling
less the reply reserve — into three fixed shares: 15% for the system-prompt part, 25% for the file
context, and whatever remained (48% of the window at the default 20% reserve) for History. The two
standing shares were reserved whether or not that content existed, and `Budget.FileContext` had no
reader at all: nothing in the engine ever consumed the 25% it held back. A session with no
workspace context file and a short prompt therefore gave a quarter of its working room to nobody,
and paid for it in History.

The reducers read History, so the cost landed on them. Pruning fires above a fraction of the
History allocation, and with History at 48% of the window the 60% band put the first prune pass at
roughly 29% of the context window. In live session `20260921T173133Z-718e0f6c` (160k window,
`ctxUsed` 55,885) that produced three prune passes — 18:04, 18:32, 19:01 — stubbing 14 tool results
while the window was barely a third full, and the model re-read the same files three and four
times to recover what had been stubbed. Four protected Turns is a narrow window for a model that
reads files in 400-line chunks: the chunks of one file can fall outside it while the read is still
in progress.

Two further facts shaped the fix. The oversize warning for [Context files](../../CONTEXT.md#context-and-history)
compares the standing content against `Budget.SystemPrompt` (ADR 0026 §8) — under a measured
reservation that comparison becomes a tautology, since the reservation *is* the measurement plus
headroom and can never be exceeded. And ADR 0018 §8's structural floor sits below the emergency
fold's transcript budget only while `0.6 × (window − reserve) < window − 4608` — above a ~8.9k
crossover. A History that grows when the standing content is small would push that crossover
upward if nothing bounded it.

## Decision

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

## Consequences

- A session with no workspace context file and a modest prompt gets most of the working room as
  History rather than 48% of the window; a session with a large `AGENTS.md` gets a larger
  `FileContext` reservation and a correspondingly smaller History, never below the 50% floor.
- The first prune pass now sits at 70% of a larger History instead of 60% of a fixed 48% share, so
  the thrash the live session showed needs a genuinely full window to reproduce. Six protected
  Turns covers a multi-chunk read in progress.
- Decision 5 makes ADR 0018 §8's survivability ordering — the structural floor below the fold's
  transcript budget — hold at *every* advertised window rather than only above the ~8.9k crossover
  §8 computes. The three allocation parts no longer sum to `ContextLimit − ResponseReserve` when
  the cap bites; `Budget`'s field comments say so.
- ADR 0077 decision 3's "the notice and the fold never disagree" holds unchanged, precisely because
  the cap lives in `budget()` and not in `deriveGrowthBounds`: there is one History, and every
  reader reads it.
- ADR 0026 §8's comparison stops being a tautology. Its rejection of a configurable size limit
  "because the Budget already allocates a system-prompt share" no longer states a live fact — the
  advisory ceiling of decision 4 is what carries that reasoning now.
- Fixtures that pinned a History figure for a named window move: at an 8192-token window History is
  3584 tokens (the cap) where it was 3933. Tests re-derive from `a.budget().History` and from
  `PruneKeepTurns` rather than pinning either.

## Rejected

- **Leaving `FileContext` unread and shrinking its fraction.** It moves the number without fixing
  the cause: any fixed fraction is wrong for a session whose standing content it cannot see.
- **Configurable shares or a configurable band.** The window is already configurable twice
  (`context-window:`, `working-window:`); a user has no way to know what fraction their standing
  content deserves, and every key here is a key the bench has to sweep.
- **Capping History inside `deriveGrowthBounds`.** It leaves two Historys in the tree — the one the
  fold derives and the one the fill notice reads — which is exactly the disagreement ADR 0077 §3
  forbids.
- **Dropping the History floor and letting the standing content take what it measures.** A
  workspace text larger than the working room would then silently starve the conversation; the
  oversize notice exists to say so out loud instead.
- **Memoising the measurement.** The standing render is not stable within a session, and a stale
  reservation is worse than a render per `budget()` call.
