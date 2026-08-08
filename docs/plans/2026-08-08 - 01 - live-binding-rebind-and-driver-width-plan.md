# Live-binding rebind inputs + parallel-agents width for every driver — implementation plan

**Goal:** Fix ISSUES.md entries 1 and 2 (plus one adjacent gap found while scouting): (a) after a `/server` switch, the rebind resolver and scheduled Firings still key spec resolution on the LAUNCH endpoint — carry the live bound upstream into the rebind inputs instead; (b) the parallel-agents cap reaches the TUI driver only — install it in `apogee headless` (pin + one-shot probe) and in scheduled Firings, so every Driver gets the same width.

**Date:** 2026-08-08
**Status:** unexecuted
**Skills:** coding-standards

**Authoritative sources:**
- `ISSUES.md` entries 1–2 as of commit `4b7a120` (fix shapes are owner-authored there).
- ADR 0039 (`docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md`) — decision 2: no signal means cap 1, "strictly today's serial behavior".
- ADR 0031 — benchable-all-the-way-up: every Driver reaches the same engine behavior.
- `docs/plans/archived/2026-08-05 - 05 - servers-single-definition-plan.md` "Out of scope" — names the launch-endpoint drift and constrains the fix to the rebind path (a late `servers:` `model:` hint deliberately does NOT re-resolve at bind time; that stays the rebind path's job).
- `docs/plans/archived/2026-08-07 - 03 - parallel-sub-agents-plan.md` item 8 — recorded the TUI-only cap reach in ISSUES.md, README, CHANGELOG.

**Ratified design calls** (owner, 2026-08-08, via AskUserQuestion in the plan-writing session):
1. **Seam for the binding overlay:** `liveSettings.rebindInputs` gains a `bound upstreamBinding` parameter and overlays `Endpoint`/`APIKey` onto the base opts copy. This realizes the ISSUES.md fix shape ("carry the bound upstream into the rebind inputs … one seam serving both callers") literally; the rejected alternative (giving `liveSettings` its own binding accessor or endpoint field) would duplicate the `upstreamHolder`'s authority. The third `rebindSpecFor` caller, `headless.go`, stays untouched — its launch opts genuinely IS the binding.
2. **Headless cap source:** pin **plus a one-shot discovery probe** (owner picked over pin-only). The probe is skipped when the pin already decides (`pin >= 1`, per `resolveParallelAgents` precedence); probe failure degrades to 0 (→ serial floor), never an error.
3. **Scope:** the scouted third gap — scheduled Firings copy the pre-bind base config and therefore never carry `ParallelAgents` — is IN scope as item 4 (owner picked over recording it in ISSUES.md).

**Out of scope:**
- ISSUES.md entry 3 (keyboard collapse/expand path).
- Any engine change — the engine already honors `Config.ParallelAgents` (`internal/agent/construct.go:113`) and `SetParallelAgents` (`internal/agent/agent.go:419`); this plan wires drivers only.
- Re-resolving prompt/validated set at bind/switch time (settled: rebind path's job).
- Version bump (see closing note).

**Standing requirements:** any authorized deviation from item text lands as a dated NOTES line under the item. If ISSUES.md's prose and this plan disagree, the Ratified design calls above win (decided later, by the owner).

---

## 1. Overlay the live binding onto the rebind inputs

**Authoritative source:** ISSUES.md entry 1; ratified design call 1.

**What:**
- `cmd/apogee/wire.go` — change `liveSettings.rebindInputs(base options)` (wire.go:1064–1078) to `rebindInputs(base options, bound upstreamBinding) (options, []apogee.MechanismID, int)`. Overlay unconditionally: `base.endpoint = bound.Endpoint`, `base.apiKey = bound.APIKey` (the holder is the wire authority; both live callers run only after the startup bind). Update the method's doc comment — it already declares itself "the one place the overlay is spelled out"; extend that charter to the bound upstream.
- `cmd/apogee/wire.go:427` (rebind closure, wire.go:422–450) — call `live.rebindInputs(opts, holder.Binding())`; `holder` is already captured (it calls `holder.SetModel` at :444).
- Mechanically update the direct test callers: `cmd/apogee/wire_test.go:2967` (hand-reconstructed rebind closure) and `cmd/apogee/settingsedit_test.go:400`.
- Do NOT touch `headless.go`'s direct `rebindSpecFor` call or `schedule.go` (item 2 owns that caller).

**Tests:**
- New test in `cmd/apogee/wire_test.go`: with base opts endpoint ≠ bound endpoint, assert the returned options carry the bound endpoint and API key.
- New endpoint-keying test through the rebind path: place a probe record fixture keyed to the BOUND endpoint+model (`library.LoadProbeRecord` keys on `(ProbeDir, Endpoint, ModelID)` — `internal/library/fingerprint.go:83–108`), reconstruct the rebind closure as wire_test.go:2966–2974 does with a launch-opts endpoint that differs, and assert the direct-match validated set is APPLIED (`setApplied`), not merely offered — proving `startupSetDecision` saw the bound endpoint. (Before this fix the record is missed → ConfidenceLow → `setOffered`, validatedsets.go:128–131.)
- Existing `TestRebindSpecForSelectsPerModelBindings` (wire_test.go:151) stays green.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'Rebind' -count=1` passes, including the new endpoint-keying test.
- `make check` passes.

Commit: `fix(wire): rebind inputs overlay the live upstream binding, not launch opts`

## 2. Firings resolve their spec against the live binding

Depends on item 1.

**Authoritative source:** ISSUES.md entry 1 (the `scheduleWiring.fire` half); ratified design call 1.

**What:**
- `cmd/apogee/schedule.go:75` — `w.live.rebindInputs(w.opts, binding)`; the `binding := w.binding()` snapshot is already taken at :68. This ends the split where the wire (`cfg.Endpoint`, `cfg.APIKey` at :82) was live but the spec resolution keyed on the launch endpoint.
- Update the `scheduleWiring` doc comment (schedule.go:29–54 area) where it describes the opts/binding split.
- `ISSUES.md` — mark entry 1 `[X]` (both callers now fixed; this item owns the edit).

**Tests:**
- Extend `TestScheduleFiringRunsAgainstTheCurrentBinding` (cmd/apogee/schedule_test.go:86) — it already fakes launch opts `http://launch.invalid` vs a live binding: add the probe-record fixture keyed to the bound endpoint (same technique as item 1's test) and assert the fired spec resolution honored it.
- Existing `TestScheduleFiringReportsAPerModelResolutionFailure` (schedule_test.go:174) stays green.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'ScheduleFiring' -count=1` passes.
- `make check` passes.

Commit: `fix(schedule): firing spec resolves against the live binding, not launch opts`

## 3. Headless installs the parallel-agents cap (pin + one-shot probe)

**Authoritative source:** ISSUES.md entry 2; ADR 0039 decision 2; ratified design call 2.

**What:**
- `cmd/apogee/headless.go` — add a package-level seam beside `runOnce` (headless.go:78–82), shape: `var discoverSlots = func(endpoint, model, apiKey string) int` (context parameter if the producer needs one), implemented with the same producer the TUI's beat reads — a single `heartbeat` beat / `provider.Client.Discover` pass yielding `TotalSlots` (`internal/heartbeat/heartbeat.go:115–124`, `internal/provider/discovery.go:79`). Any failure or absent `/props` returns 0; never an error, never a retry.
- In `runHeadless`, beside the existing Context/window-pin block (headless.go:292–330): read the pin via `startupEntry(*opts).ParallelAgents` (wire.go:2422–2430; `opts.startupParallelAgents` is already populated by `applyConfig` at headless.go:195). If `pin >= 1`, skip the probe (pin wins in `resolveParallelAgents` anyway — no pointless network call in an unattended run); else `slots = discoverSlots(...)`. Then `cfg.ParallelAgents = resolveParallelAgents(pin, slots)` (resolver at cmd/apogee/config.go:1058).
- `README.md` — update the TUI-only-cap caveat line (added by the archived 2026-08-07 - 03 plan, item 8) to say headless now resolves the cap the same way.
- `CHANGELOG.md` — add an entry under the current unreleased section if one exists; touch no release heading and no VERSION (version policy).
- `ISSUES.md` — mark entry 2 `[X]` (this item owns the edit).

**Tests:**
- `cmd/apogee/headless_test.go`, via the existing harness (`headlessRunOn` swapping `runOnce`/`newConfiner`, `stubRunner` capturing the `run.Spec`, temp config home):
  a. `servers:` entry with `parallel-agents: 3` → `stub.spec.Config.ParallelAgents == 3` AND the `discoverSlots` stub was NOT called.
  b. No pin, `discoverSlots` stub returns 4 → cap 4.
  c. No pin, stub returns 0 (probe failed) → cap 1 (ADR 0039 serial floor).

**Acceptance:**
- `go test ./cmd/apogee/ -run 'Headless' -count=1` passes, including the three new cases.
- `make check` passes.

Commit: `fix(headless): install the parallel-agents cap from the pin and a one-shot probe`

## 4. Firings carry the current parallel-agents width

Depends on item 2 (same file, `cmd/apogee/schedule.go`).

**Authoritative source:** ratified design call 3; ADR 0039; ADR 0031 (every Driver, same width).

**What:**
- `cmd/apogee/upstream.go` — add a read-only accessor on `parallelAgentsCap` (upstream.go:340), suggested name `current() int`: under the mutex, return `resolveParallelAgents(pinned, observed)` WITHOUT mutating state and WITHOUT pushing to the engine (unlike `follow`/`observe`/`relist`).
- `cmd/apogee/schedule.go` — add a `width func() int` field to `scheduleWiring`; in `fire`, set `cfg.ParallelAgents = w.width()` beside the `cfg.Endpoint, cfg.APIKey` assignment (:82), replacing the zero value copied from the pre-bind `w.base`.
- `cmd/apogee/wire.go:589–596` — construct `scheduleWiring` with `width: caps.current` (`caps` from wire.go:369).

**Tests:**
- `cmd/apogee/upstream_test.go`: `current()` returns the resolved width and does not touch the `parallelAgentsSpy` engine.
- `cmd/apogee/schedule_test.go`: a fired run's `Config.ParallelAgents` equals the wired width func's value.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'ParallelAgentsCap|ScheduleFiring' -count=1` passes.
- `make check` passes.

Commit: `fix(schedule): fired runs carry the live parallel-agents width`

---

**Suggested version bump** (not performed — owner's call, after the run): patch (v0.12.1) if this is read as a fix wave; minor (v0.13.0) if the headless one-shot probe counts as new driver capability. No VERSION/CHANGELOG-heading/tag change is part of this plan.
