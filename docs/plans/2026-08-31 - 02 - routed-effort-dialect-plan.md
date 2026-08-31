# Routed sub-agents speak their own server's effort dialect

**Goal:** A child agent routed to the Sub-agent server currently inherits the
ORCHESTRATOR's thinking-effort wire dialect, so its compaction summariser asks for
"no reasoning" in a shape the routed server does not read — the fold burns its whole
output cap on reasoning and faults at every Turn boundary. Route the dialect with the
server, and stop the fault text asserting more than the engine can know.

**Date:** 2026-08-31 · **Status:** unexecuted
**Base commit:** `4debb456` · **sized for:** ~200k-context host

**Sources**
- `docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md`
- `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md` (D3, D8, D9)
- `docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md`
- `docs/plans/archived/2026-08-30 - 00 - compaction-empty-summary-plan.md`

**Ratified design calls** (owner, 2026-08-31)
- **Scope:** the routed-dialect fix plus mandatory-reasoning honesty; the cap/rung work for models that cannot disable reasoning is out.
- **Fallback (owner, 2026-08-31, SECOND call — supersedes the earlier `EffortDialectNone` ratification, which rested on a premise the re-check proved false):** a routed child takes `target.EffortDialect` when the target NAMES one; when the target names none (the zero), the child keeps the parent's `a.effortDialect`, exactly as today.
- **Record:** amend ADR 0045's enumeration; cross-reference ADR 0060 — no new ADR.
- **`reasoning.mandatory`:** acts host-side (ADR 0060 D9 stands); the engine's fault text is reworded, not given a second effort fact.
- **`default_enabled`:** narrowed out of item 1 — nothing reads it and `Mandatory` alone drives the note.

**Regression check (2026-08-31, `4debb456`):**
- 1 — recast, guard folded: `Mandatory` describes the MODEL, so it survives a forced dialect
  whatever the forced value (only `off` zeroes it); `mandatory` is decoded so a non-boolean value
  cannot take the whole dial down with it. Yields to ADR 0060 D1/D9 — their enumerations stand.
- 2 — recast: the ratified zero-fallback's exposure is accepted rather than closed, and
  ANNOUNCED — the item gains a host-side note in the delegation wiring.
- 3 — guard folded: the split is keyed on the request's own `EffortOff`, never on the dialect,
  so a `None`-dialect session whose profile pins `effort: off` is not told it never asked.
- 4 — guard folded: the note's emitting condition is `Supported && Mandatory`.
- 5 — guard folded: the stale enumerations are named — `CONTEXT.md:193-197` and ADR 0045's
  Consequences line 118 (which records "all four", superseded by this item) beside §3's line 54;
  the manual gains item 2's operator remedy instead of an enumeration edit.

Re-check round (2026-08-31, `4debb456`) — extends the block above; where the two disagree, this round governs:
- 1 — guard folded: the surviving-vocabulary case leaves `TestDiscover_MalformedEffortPayloadsStayBestEffort`'s
  table, whose ONE shared assertion reads every row as "no dial" (`internal/provider/discovery_test.go:648-651`),
  and gets a test of its own.
- 2 — recast: the ratified fallback is REVERSED — a target naming no dialect leaves the child on the
  parent's — so the accepted exposure, the "only place observable" claim and the `compact.go:475`/
  `client.go:634` byte-identity argument are struck from the item; the note survives as ADVICE, reworded,
  and the four pinned note counts it breaks are named as sites the item updates.
- 5 — recast: the manual paragraph documents the surviving effect — a tell-less flagged entry leaves its
  delegates on the SESSION server's dialect — instead of a lost off-request.

**Standing requirements**
- `skills: coding-standards`

**Out of scope**
- Raising `compactMaxTokens`, reserving headroom for a mandatory reasoning pass, or asking for the lowest advertised rung instead of off.
- Plumbing `EffortSupport.Efforts` to the Agent; the level vocabulary stays host-side (ADR 0060 D9).
- The `internal/title` call's identical 4096-token cap.
- Any `VERSION` or `CHANGELOG` release-heading change.

## 1. `EffortSupport` records that a model's reasoning cannot be turned off — ✅ DONE (2026-08-31)

NOTES (2026-08-31): `default_enabled` is decoded by neither pass, per the item's binding narrowing.
NOTES (2026-08-31): `Mandatory` is carried on `modelReasoning` as `json:"-"` and filled by a second, error-checked-but-non-fatal `json.Unmarshal` inside `decodeReasoning` — the item's first offered option; the `json.RawMessage` alternative was not used.

**What:** Recast at the regression check (2026-08-31). `modelReasoning`
(`internal/provider/discovery.go:364`) decodes only `supported_efforts` and `default_effort`,
so the `mandatory` flag an OpenRouter-shaped
`/v1/models` entry carries is dropped. Decode it and carry it onto `EffortSupport` as
`Mandatory bool`, documented as a REPORTED fact like `Efforts` and `Default` — meaningless
when `Supported` is false. In `forceEffortDialect`, `Mandatory` is copied from the DETECTED
support whatever the forced dialect names — deliberately unlike `Efforts`/`Default` — and only
the forced `EffortDialectOff` yields the zero value (see the guard). Binding narrowing:
`default_enabled` is NOT carried — no reader needs it, and a model whose reasoning is
merely on-by-default is already served by the off request apogee sends.

**Regression guard.** `Mandatory` describes the MODEL, not the wire shape, so it survives a
forced dialect whatever the forced value — copy it from the detected support even when the
forced dialect differs from the detected one. This is deliberately UNLIKE `Efforts`/`Default`,
which are vocabulary tied to the dial's shape and rightly do not survive a shape change; only
the forced `EffortDialectOff` yields the zero value, as it does for every other field. This
closes the reviewer's point that item 4's note could never fire for a server whose entry forces
`openai`. Authoritative source for the payload shape, since no fixture, ADR or manual page in
the tree carries the field — verified live 2026-08-31 against `https://openrouter.ai/api/v1/models`:
`z-ai/glm-5.3-flash` and `z-ai/glm-5.3` report
`"reasoning": {"mandatory": true, "default_enabled": true, "supported_efforts": ["max","high","low"], "default_effort": "max"}`,
while `z-ai/glm-5.2` and `~deepseek/deepseek-v4-flash-latest` report `"mandatory": false`. Name
that URL and those model ids in the item as its source, and build the decode tests from those
exact payload shapes.

Decoding it must not cost the dial. A plain `Mandatory bool` on `modelReasoning` makes a
non-boolean value (`{"data":[{"id":"m","reasoning":{"supported_efforts":["low","high"],"mandatory":"always"}}]}`)
fail the WHOLE unmarshal, so `decodeReasoning` (`internal/provider/discovery.go:443`) answers
nil and the dial vanishes — `/effort` leaves the menu (`internal/tui/effort.go:43`) and the
footer segment with it. Decode `mandatory` in a second, error-ignored unmarshal (or hold it as
`json.RawMessage`) so a non-boolean value zeroes only `Mandatory` while the vocabulary survives.

That surviving-vocabulary case does NOT go in `TestDiscover_MalformedEffortPayloadsStayBestEffort`
(`internal/provider/discovery_test.go:613`): every row of that table answers to one shared assertion,
`if info.EffortSupport.Supported { … want the zero value on an unparsable payload }` (`:648-651`),
which a row whose vocabulary survives — `Supported` true — fires. Give the non-boolean-`mandatory`
payload a test of its own, or give that table a per-case `want EffortSupport`, so "the dial survives a
bad `mandatory`" is asserted somewhere it can be.

The item YIELDS to ADR 0060 D1 (`docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md:46`,
which enumerates the object as `(supported_efforts, default_effort)`) and D9 (`:127-128`, the
beat's host-side facts): neither enumeration is amended by this plan — `Mandatory` is a
host-side fact under D9's own rule, exactly as the ratified call records.

**Files:** `internal/provider/discovery.go`, `internal/provider/discovery_test.go`

**Tests:** `mandatory` true, false, absent, `null`, a non-boolean `mandatory` (the vocabulary
survives — asserted in its OWN test, never as a row of
`TestDiscover_MalformedEffortPayloadsStayBestEffort`), and a malformed
`reasoning` object — the true and false cases built from the OpenRouter payload shapes the
guard names; a routing variant inheriting its base slug's `Mandatory` through `effortSupport`;
the `forceEffortDialect` matrix (same dialect keeps it, a DIFFERENT dialect keeps it too, `off`
zeroes it).

**Acceptance:** `go build ./internal/provider/... && go test ./internal/provider/...`

`feat(provider): EffortSupport reports when a model's reasoning cannot be disabled`

## 2. A routed child speaks the Sub-agent server's effort dialect

**What:** Recast at the regression check (2026-08-31). Regression from the ADR 0060 dialect
seam: before it every request carried the `chat_template_kwargs` anchor, so a routed child on
a llama.cpp Sub-agent server DID
silence its summariser's reasoning; since it, `internal/agent/subagent.go:425` hands the
child `a.effortDialect` — the ORCHESTRATOR's server's shape — and
`compactCompleter`'s `EffortOff` goes out as a field the routed server ignores.
Add `EffortDialect provider.EffortDialect` to `DelegationTarget`
(`internal/agent/delegationtarget.go`) as a plain value whose field doc states what its zero means
HERE: "this target names no dialect — the child keeps the parent's", NOT the wire anchor that
`RebindSpec.EffortDialect`'s zero stands for. The ladder is two rungs and binding (owner,
2026-08-31, second call, superseding the earlier `EffortDialectNone` ratification): a ROUTED spawn
takes `target.EffortDialect` when the target NAMES one, and when the target names none — the zero —
the child keeps the parent's `a.effortDialect`, exactly as today; an UNROUTED spawn is untouched and
still inherits `a.effortDialect`. Rewrite the comment at `:425`, whose
argument that a dialect is not a routed fact is what this item reverses.
`resolveDelegationTarget` (`cmd/apogee/delegation.go:408`) fills it
`provider.EffortDialectFor(entry.EffortDialect)` ▸ `observed.EffortSupport.Dialect`,
the same order `cmd/apogee/wire_firing.go:200-206` already uses. Update the facade's
routed-field list at `apogee.go:77-82` and the stale enumeration of what the target carries at
`internal/agent/delegationtarget.go:6-7` ("endpoint, key, model, window, fan-out width, model
profile … Bypass, Mechanisms").

**Regression guard.** Two additions. **(a)** The stale enumeration at
`internal/agent/delegationtarget.go:6-7` is named in the What beside the `apogee.go:77-82` line,
and `delegationtarget.go` stays in **Files** — it is already there for the struct change.
**(b)** With the reversed fallback NOTHING is lost — a target naming no dialect leaves the child
speaking exactly what it speaks today — so this item's host-side note ADVISES rather than announces
a loss: a tell-less flagged entry leaves its delegates speaking the ORCHESTRATOR's shape, the very
bug class this plan fixes, and without the key that is unavoidable. It goes where the delegation
wiring already announces routing state changes (`cmd/apogee/delegation.go` — the per-state-change
note described at its `:330`, produced by `delegationWiring.stateChange` at `:301`) and fires when
the resolved target dialect is the zero, i.e. the flagged server advertised no tell and its entry
pins no key. The wording is binding and must be asserted verbatim by a test built from the emitting
function, carrying that producer's `sub-agents: ` prefix — its every other note has one, and none
uses backticks:

    sub-agents: <server> advertises no thinking-effort dialect — delegates there speak this session's; set effort-dialect: on its entry

The operator-facing manual half of the remedy is item 5's, not this item's.

**Files:** `internal/agent/delegationtarget.go`, `internal/agent/subagent.go`, `internal/agent/routedspawn_test.go`, `cmd/apogee/delegation.go`, `cmd/apogee/delegation_test.go`, `apogee.go`

**Tests:** in `routedspawn_test.go` beside `TestRoutedSpawnBuildsFromTheTarget` — a routed
child holds the target's dialect; a target naming none leaves the child on the PARENT's, with the
parent holding `EffortDialectReasoning`; `TestUnroutedSpawnInheritsTheParent` still holds. In
`delegation_test.go` — forced `effort-dialect:` wins, else the observed one, else the zero; and
the routing note fires with the guard's EXACT string (built from `stateChange`, not a fixture)
when the resolved target's dialect is the zero, and does not when it names one. That note breaks
four pinned note-count assertions, which this item UPDATES rather than works around:
`cmd/apogee/delegation_test.go:452` (a `t.Fatalf`, 3 → 4 expected notes), `:615`, `:675` (where
`notes[1]` stops being "sub-agents: routing to cheaper (tiny-3b)") and `:712` — all four sit inside
this item's own `-run 'Delegation'` acceptance.
The journey: a routed child whose target is `kwargs` sends `chat_template_kwargs.enable_thinking=false`
on its summary call, asserted end-to-end rather than on the field alone
(`TestBuildBody_EffortOffEmitsEnableThinkingKwarg`, `internal/provider/client_test.go:503`,
is the existing byte-level half — do not duplicate it).

**Acceptance:** `go build ./... && go test ./internal/agent/... ./cmd/apogee/... -run 'Routed|Unrouted|Delegation|Spawn|Latch'`

`fix(agent): a routed child speaks the Sub-agent server's effort dialect`

## 3. The compaction fault asserts only what the engine knows

**What:** `cappedSummaryErrFmt` (`internal/agent/compact.go:430`) ends
"the summarizer asked for no reasoning; this server's template did not honour that" — on
every dialect, including the three where `compactCompleter` never sent an off request at
all (`:475` gates the override on `Kwargs`/`Reasoning`), and as a verdict on a server the
engine cannot inspect. Split it: when the request DID carry an off intent, say the call
asked for no reasoning and the server reasoned anyway, without diagnosing why; when it did
not, say the summariser's cap was spent on reasoning this server was never asked to skip.
Both keep the cap and the estimated spend, and neither invites a retry (ADR 0046). The
`FinishLength`-with-visible-text path (the kept, marked summary) is untouched.
`EffortDialectNone` and `EffortDialectKwargs` are byte-identical on the wire
(`internal/provider/client.go:634`), so `internal/agent/compact.go:475` is the only place the
split fault text is observable — the per-dialect tests therefore drive that gate rather than
inspecting request bytes.

**Regression guard.** The split is keyed on the REQUEST's own
`ThinkingEffort == provider.EffortOff`, as the What says, and never on the dialect: a session
whose profile pins `effort: off` (`internal/agent/wire.go:59-63`) carries `EffortOff` under
`EffortDialectNone` too, where `applyEffort` emits `enable_thinking=false` for it
(`client.go:634`) — a dialect-keyed split would tell that session it was "never asked to skip".

**Files:** `internal/agent/compact.go`, `internal/agent/compact_test.go`

**Tests:** the fault text for a capped reasoning-only summary under each of `Kwargs`,
`Reasoning`, `OpenAI`, `None` and `Off` with no effort resolved, asserting the EXACT emitted
string; plus a `None`-dialect session whose profile pins `effort: off`, which DID ask and must
therefore read as the asked-for-no-reasoning half; a capped summary that DID produce visible
text still returns the truncation marker and no error.

**Acceptance:** `go test ./internal/agent/... -run 'Compact|Summary|Fold'`

`fix(agent): the compaction fault names only what the engine can know`

## 4. The host says when a bound model cannot disable reasoning

Depends on item 1.

**What:** `EffortSupport.Mandatory` is a host-side fact (ADR 0060 D9), so the human learns
it the way ADR 0060 D8's excluded-override already tells them: a transcript note at the
bind. In `applyRebind` (`internal/tui/heartbeat.go:406-408`), after the override-clear
check, add a note when `intent.effort.Supported && intent.effort.Mandatory`. Fires per rebind,
exactly as `effortClearedNote` does — no once-per-session latch. The model is rendered through
`displayModel` like every id the chrome shows. The wording is decided here and is binding:

    <model> cannot switch its reasoning off — compaction and title calls spend part of their output cap on it

**Regression guard.** The emitting condition is `intent.effort.Supported && intent.effort.Mandatory`,
never `Mandatory` alone: `EffortSupport` documents every field below `Supported` as meaningless
when it is false (`internal/provider/discovery.go:115-117`), and this item's own "no note when
`Supported` is false" test drives a beat carrying `provider.EffortSupport{Mandatory: true}`
through the `upBeat`/`foldBeatMsg` harness (`internal/tui/heartbeat_test.go:733-735`) — on
`Mandatory` alone that test sees the note and goes red.

**Files:** `internal/tui/heartbeat.go`, `internal/tui/heartbeat_test.go`

**Tests:** the note fires with that EXACT string (built from the emitting function, not a
fixture) when a bound model reports `Mandatory`; no note when it does not, when
`Supported` is false, or when the beat reports nothing; a rebind that also clears an
excluded override emits both notes in a stable order.

**Acceptance:** `go test ./internal/tui/... -run 'Effort|Rebind|Note|Heartbeat'`

`feat(tui): a note says when the bound model cannot disable reasoning`

## 5. ADR 0045 names the effort dialect among what routing replaces

Depends on item 2.

**What:** ADR 0045 enumerates what a routed spawn replaces (endpoint, key, model, window,
fan-out, profile, the two posture keys); the effort dialect belongs in that list, because
ADR 0060 D3 makes the dialect a property of the SERVER and a routed child is on another
one. Amend the enumeration and record why, citing the defect this plan closes. Add a
cross-reference in ADR 0060 D9: the dialect now reaches the engine through a second
channel — the delegation target — alongside the rebind spec, and it is still the ONLY
effort fact that crosses. Update `CONTEXT.md`'s Sub-agent server entry (`CONTEXT.md:181-191`)
for the routed dialect itself — that entry and ADR 0045's enumeration are amended for the routed
dialect exactly as already planned. `docs/manual/configuration.md:583-589` enumerates nothing about what the flagged Sub-agent entry
supplies, so it gains no enumeration edit; with the reversed fallback it documents no lost
off-request either. What that paragraph gains is the surviving effect and its fix: a flagged
Sub-agent entry on a server that advertises no passive tell leaves its delegates speaking the
SESSION server's effort dialect, and `effort-dialect:` on that entry is the remedy.

**Regression guard.** The rule, not a list: every prose site asserting that a dialect is
not routed, or that the rebind spec is the sole channel carrying one, is amended. Find
them with `grep -rn "dialect is neither\|only the dialect\|ONLY effort fact\|rebind spec" docs/ internal/ cmd/ CONTEXT.md`
and settle each hit — amend or confirm still true. Item 2 owns the `subagent.go` comment;
this item owns everything else.

That grep hits `CONTEXT.md` nowhere, so it does not reach the enumerations that actually go
stale. Widen the rule to every prose enumeration of what the Delegation target CARRIES or what a
routed spawn REPLACES, and find those with
`grep -rn "per-slot window\|Parallel-agents cap\|all four" CONTEXT.md docs/adr/ internal/`.
Two sites are named outright: `CONTEXT.md:193-197` — the **Delegation target** entry, whose
"endpoint and key plus its *observed* facts — model, per-slot window, Parallel-agents cap, model
profile" is the list the new field joins (the Sub-agent server entry at 181-191 is a second, not
a substitute) — and ADR 0045's Consequences line 118, "the spawn asks the latch for upstream,
window, profile, and posture instead of inheriting **all four** from the parent". That "all
four" is a documented decision this item SUPERSEDES: it is amended alongside §3's line 54, and
the acceptance below counts both enumerations rather than one.

**Files:** `docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md`, `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`, `CONTEXT.md`, `docs/manual/configuration.md`

**Tests:** none — prose only.

**Acceptance:** `grep -c "effort dialect" "docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md"` counts BOTH amended enumerations (§3's line 54 and the Consequences line that carried "all four"), and both guard greps above leave no unsettled hit.

`docs(adr): ADR 0045 routes the effort dialect with the Sub-agent server`

## Suggested version bump

Not performed. Items 2 and 3 close a user-visible regression in delegated compaction; a
micro bump (`VERSION` patch) plus a `[Unreleased]` rollup would be warranted at closeout —
the owner's call.
