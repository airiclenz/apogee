# Plan — The `/model` and `/server` pickers over the heartbeat's prepared seams

**Date:** 2026-07-28
**Status:** READY (not owner-grilled — the *Design decisions* below derive from ADR 0024's
explicit preparation for exactly this work, the TODO entry's own scoping, and the provider
client's documented contract; each call records its reasoning so the owner can veto before the
run).
**Source:**
- `TODO.md` — "[P1] Server / model switching — unblocked 2026-07-27; the UI half remains":
  the `/model` / `/server` picker UI over `hb.models` / `RebindSpec` / `Agent.Rebind`, and the
  endpoint switch that "swaps the `heartbeat` monitor and the provider client behind the same
  two `tui.Options` seams".
- [ADR 0024](../adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
  — decision 8 ("the beat carries the whole `/v1/models` offering… the deliberate data layer
  for the future `/model` picker and `/server` switch") and the consequence "the picker work is
  now cheap and unblocked… `Rebind` deliberately never touches `Endpoint`, and
  `errMissingEndpoint` stands".

**Precondition:** plan `2026-07-28 - 04` (gauge inset + quiet connect) must be fully landed and
archived before this plan starts — its item 2 edits the same `model.go` heartbeat-fold region,
`heartbeat_test.go`, and `internal/tui/doc.go` this plan builds on (`rebindIntent.quietSeed` is
load-bearing for item 6 here).

**Terminology:** the **picker** is a modal single-select overlay painted through the shared
popup module (`renderPopup`, `internal/tui/popup.go`) — the `/sessions` browser's simpler
sibling. A **server switch** is the whole move to another endpoint: a new provider client and a
new heartbeat Monitor behind the same two `tui.Options` seams, the model **unbound**, and the
new server's first beat completing the move through the ordinary rebind path. **Rehome** is not
introduced as a noun — the operation is `Agent.SwitchUpstream`, and CONTEXT.md's existing
nouns (Upstream, Heartbeat, Beat, Rebind) carry the rest.
**Track:** post-`v0.9.7` working tree. CHANGELOG entries under `## [Unreleased]`; `VERSION`
untouched (rides the next cut — which must be a **minor** bump: new public Go surface and a new
config key).
**Public API:** grows `Agent.SwitchUpstream(UpstreamSpec)` and the `apogee.UpstreamSpec` alias
(additive, ADR 0010). `tui.Options` gains `Servers` and `SwitchServer` (internal package, but
treated with the same care — every seam nil-degrades).
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive). ADR 0011 stands: the value-copied `Model` gains only plain-value
state (no no-copy types); `TestModelNoBuilderByValue` must pass untouched. ADR 0010/0024
stand: `internal/tui` imports `internal/heartbeat` for the `Beat` value and **never**
`internal/provider`.

Per-item green gate:

```
gofmt -l .                # empty
make check                # vet + lint + go test -race -count=1 ./...
```

**Dependencies.** Item 2 is independent. Item 4 needs items 1, 2 and 3. Item 5 needs item 2
(behaviourally — the hint must follow a picked model or the next beat rebinds it away). Item 6
needs items 4 and 5 (it reuses item 5's picker state). Item 7 (docs) needs all. The tree is
coherent after any item: a landed engine seam or Options member without its TUI consumer is
additive and dormant.

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document.
2. [ADR 0024](../adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
   — decisions 3 (one code path for cold start / late seed / switch), 4 (rebind is idle-only,
   the boundary is the synchronization), 5 (two seams, split by job), 7 (offline posture),
   8 (the beat carries the offering), and the consequences block.
3. `internal/tui/model.go`, the heartbeat fold and rebind orchestration (`heartbeatState`,
   `foldBeat`, `observeBinding`, `applyRebind`, `rebindNote`, `blockedUpstream` — the region
   plan 04 last touched): fold order, the fail-once posture, `rebindNote`'s "" contract, and
   the generation-guard rule (`heartbeatLive`: a Msg carrying a retired gen is inert).
4. `internal/provider/client.go` — `SetModel`'s doc: it "deliberately does NOT cover the
   endpoint — switching servers means a new Client, not a mutated one". This plan obeys that
   line rather than amending it.
5. `internal/tui/command.go` (`commandSpecs`, `parseInput`, `matchCommand`) and `layout.md`'s
   command-menu prose — the one-table command registry and the accept-runs / takesArgs-completes
   policy (ADR 0027).
6. ADR 0011 / `internal/tui/doc.go` — the thin-renderer contract and the value-Model rule.

---

## Design decisions (2026-07-28, derived from the sources above)

### The `/model` pick drives the EXISTING rebind path — no third way to bind

A picked model becomes a `rebindIntent{model, window}` fed to `applyRebind` — the same
orchestration a heartbeat-observed change takes (seam call, fail-once note, start-up box
restated, notices surfaced). The picker also **records the pick as the last observation**
(`hb.observedModel/observedWindow`), exactly as `observeBinding` records one, so the next beat
that reports the picked model measures as "nothing new" rather than as a fresh change. ADR
0024's "cold start, late seed and mid-session switch are ONE code path" extends to the
user-initiated switch by construction, and `rebindNote`/`rebindFailNote` word it with no new
strings.

### The discovery hint FOLLOWS the binding (the flap-back defect this plan must not ship)

The Monitor is constructed once with the config'd `model:` as its discovery hint, and nothing
moves that hint today. If `/model` bound B while the hint stayed A, the next beat on a
multi-model server still serving A would resolve `ActiveModel: A` and the orchestration would
dutifully rebind **back to A** ten seconds after the user picked B. The fix is principled, not
defensive: the hint is "which of the served models do I mean" — that is a property of the
**binding**, so the composition root's rebind closure updates the Monitor's hint with every
successful rebind (heartbeat-driven ones included, where it is a no-op re-statement). This is
item 2, load-bearing for item 5, and it also fixes a latent pre-existing flap: after any
observed rebind away from the config'd model, a server that later serves BOTH models would
yank the session back to the stale config hint.

### A server switch UNBINDS the model; the new server's first beat completes the switch

`Agent.SwitchUpstream` swaps the provider client (a new `provider.NewClient` — per the
client's own contract), moves `cfg.Endpoint`/`cfg.APIKey`, and sets `cfg.Model = ""`. It does
**not** guess the new server's model, window, prompt or Mechanism set — the first beat of the
new Monitor discovers reality and the ordinary rebind binds it, with `errNoModelBound` /
`blockedUpstream` gating sends in the gap exactly as the async cold start does. Keeping the
old binding "until corrected" was rejected as dishonest (up to ten seconds of a footer and
Budget describing a server the wire is no longer pointed at); tearing down and reconstructing
the Agent was rejected because the conversation, approvals, mode, and confinement state all
survive a switch by design (the same posture Rebind takes). The Mechanism registry stays armed
for the old model until the first-beat rebind replaces it — unreachable in the gap, since no
request can open while nothing is bound.

What resets engine-side, with Rebind's own rationale: the token estimator and the compaction
saturation latch. What the TUI resets: the whole `heartbeatState` (fresh generation so stale
beats and ticks from the old chain are inert; empty `models`; the offline debounce back to
cold-start posture — a dead new server is believed on its first failed beat, which is
correct). `ctxUsed` survives, like it survives a model rebind.

### The switch is SESSION-scoped

`/server` changes the running session only: config.yaml is not rewritten, and the next launch
starts at the configured `endpoint:` again (the `/confine off`-without-`--save` posture). A
`--save` form, and a `server:` startup key selecting a named entry, are explicit non-goals —
additive later if wanted. Likewise the session record keeps storing the model only; resuming a
session does not restore a switched endpoint.

### The connected note after a switch is NOT quiet

Plan 04 made first contact silent because the start-up box "a few rows above" already shows
the facts. After a mid-session switch the box is deep in scrollback and the human just acted —
the bind IS the confirmation. `heartbeatState` gains `switched bool`, set by the switch fold
and never cleared; `foldBeat`'s first-contact capture becomes
`!everOnline && failures == 0 && !switched`. Launch stays quiet; every post-switch seed
announces `connected: <model>`.

### Two verbs, one picker overlay, idle-only

- `commandSpecs` gains `{model}` and `{server}`, both `takesArgs`, neither `whileRunning` —
  so the merged menu tags them `— idle only`, the dropdown **completes** them (the /confine
  posture, ADR 0027) rather than running them, and mid-run acceptance earns
  `commandsAtIdleNote`. Idle-only is deliberate: both verbs end in idle-only engine calls, and
  a user-initiated switch racing the deferred `pendingRebind` path would make "latest wins"
  ambiguous between the human and the server.
- Bare `/model` and `/server` open the picker; `/model <id>` and `/server <name>` switch
  directly (exact match), with an unknown argument earning a note that points at the bare
  form. More than one argument is a usage note.
- One `picker` state struct serves both verbs (a `kind` enum, not a callback field): rows are
  re-derived at render time — `/model` from `m.hb.models` live (a beat landing under an open
  picker refreshes it; selection clamped, the `sessionBrowser.clampSelection` posture),
  `/server` from `m.opts.Servers` (static per session). The row whose identity matches the
  current binding (`opts.Model` by id / `opts.Endpoint` by endpoint) carries a faint
  "· current" suffix. Accept on the current row closes with an "already …" note — an explicit
  act deserves an answer, where `rebindNote`'s "" contract is about *unasked* observations.
- Degrades, each an honest note and no overlay: heartbeat unwired; upstream offline; no
  models advertised yet; `Rebind` nil (display-frozen); no `servers:` configured;
  `SwitchServer` nil.

### `servers:` is a config LIST; the startup endpoint is synthesized as a row

A new file-only key (shape in item 1): named entries carrying `endpoint` plus optional
`api-key` and `model` hint. The entry `name` is the row label, the `/server` argument, AND the
footer's host alias after a switch — one field, three jobs, mirroring `host-alias:`. Keys are
per-server and file-only (`APOGEE_API_KEY` keeps winning for the **startup** server only —
documented in the template). The composition root assembles the picker's choices: every
configured entry, plus — when no entry already names the startup endpoint — a synthesized row
for it (name = the resolved `hostAlias`, key = the resolved startup key, hint = the config'd
`model:`), so switching away is always reversible without config surgery.

### The two seams stay; a HOLDER swaps what is behind them

Per the TODO's own words, `Options.Heartbeat` and `Options.Rebind` keep their signatures; the
composition root introduces a small mutex-guarded holder owning the *current* Monitor.
`Options.Heartbeat` becomes the holder's `Beat` (the beat Cmd goroutine reads the current
monitor under the mutex; a swap mid-beat is safe because the retired generation makes the
landing inert), the rebind closure moves the hint through the holder, and the new
`Options.SwitchServer` seam is the ONLY new verb: resolve the named entry, `agent.
SwitchUpstream` (the commit point — nothing after it can fail), swap the monitor, restamp the
session host's model to "" (a save in the gap must not claim the old model), and return the
display facts (`Endpoint`, `HostAlias`, and the pinned window — the global `context-window:`
pin survives a switch, so the renderer keeps needing no knowledge of it). Swapping the
`Options` func values TUI-side was rejected: the renderer would own upstream lifecycle it is
deliberately ignorant of.

---

## The ground (verified 2026-07-28 against the working tree at `7881591` + plan 04's item-2 work)

**The data layer is already flowing.** `heartbeat.Beat.AvailableModels` (`internal/heartbeat/
heartbeat.go`) rides every beat; `foldBeat` stashes it into `m.hb.models` with the comment
"the future picker's data layer … nothing renders it". `heartbeatState` also carries `gen`
(generation guard, 0 = unwired, seeded 1 in `newModel`), the offline debounce
(`failures`/`everOnline`/`offline`/`lastFailure`), the observation baseline
(`observedModel`/`observedWindow`), `pendingRebind` and `lastRebindFailed`.
`rebindIntent{model, window, quietSeed}` is the orchestration's unit; `applyRebind` owns the
seam call, the fail-once note, `transcript.refreshStartup(newStartupView(m.opts))`, `rebindNote`
/ `rebindFailNote` / `unknownWindowNote`, and the notices loop. `blockedUpstream` refuses the
three Exchange-opening paths while offline or unbound; `hostDisplay` and `displayModel` are the
single label sources.

**The engine half.** `Agent.Rebind(RebindSpec)` (`internal/agent/rebind.go`) is idle-only
(`a.turns.inExchange` → `domain.ErrInputPending`), validate-then-commit, refuses an empty
model and a pre-built `Config.Mechanisms`, moves the wire model through the optional
`SetModel(string)` interface on `a.upstream`, and resets `a.tokens` + `a.compactSat`.
Construction: `agent.New`/`Resume` bind `provider.NewClient(cfg.Endpoint, cfg.Model,
provider.WithAPIKey(cfg.APIKey))`; `validateConfig` keeps `errMissingEndpoint`;
`errNoModelBound` gates `Submit`. `provider.Client.SetModel` is mutex-guarded and documented
as model-only ("switching servers means a new Client").

**The composition root.** `cmd/apogee/wire.go`: `monitor := heartbeat.NewMonitor(opts.endpoint,
opts.model, opts.apiKey)` (its doc currently says "the endpoint never changes mid-session …
this one key holds for the Monitor's whole life" — item 4 rewrites that paragraph);
`pinnedWindow := opts.contextWindow`; the `rebind` closure = `rebindSpecFor(opts, roots,
manualIDs, model, window, pinnedWindow)` → `agent.Rebind(spec)` → `host.SetModel(spec.Model)`
→ `tui.RebindResult`. `Options{…, Heartbeat: monitor.Beat, Rebind: rebind}`. `applyConfig`
guarantees `opts.hostAlias` is non-empty (falls back to `hostFromEndpoint`).

**The TUI chrome.** `renderPopup` (`popup.go`) paints every overlay; `sessionBrowser`
(`sessions.go`) is the modal precedent — inline zero-value-closed state on the Model, first
claim on keys while open, `maxSessionRows = 8`, a one-line key-legend hint.
`commandSpecs` (`command.go`) is the one table feeding parser + menu; its comment currently
says "/server is deferred (it needs a swappable provider seam) and so is absent" — this plan
removes that sentence. `parsedInput` carries `confine confineArgs` but no generic args;
`matchCommand` already returns the argument tokens. `layout.md`'s command-menu prose names
"/confine … and /skill" as "the two verbs" that complete instead of run.

**Existing tests to build with.** `internal/tui/heartbeat_test.go`: `wireHeartbeat` /
`wireRebind` / `unbound`, `upBeat` / `downBeat`, `foldBeatMsg`, `noteTexts` / `countNotes`,
plus the plan-04 pins (`TestLateSeedBindsThroughRebind` wants zero connected notes on a quiet
seed; `TestDelayedConnectNotesOnce`). `internal/agent/rebind_test.go` has the Rebind harness
patterns (fake responder implementing `SetModel`). `cmd/apogee`: `config_test.go` parse
tables; `wire_test.go` seam-level tests; `defaults_test.go` pins that the embedded template
parses. `internal/heartbeat/heartbeat_test.go` runs a fake `/v1/models` server.

---

## 1. The `servers:` config key — named upstream endpoints — ✅ DONE (2026-07-28)

**What.** A file-only list of named servers the `/server` picker offers.

- `cmd/apogee/config.go`: `fileConfig` gains `Servers []serverEntry` (`yaml:"servers"`);
  `serverEntry{Name string; Endpoint string; APIKey string `yaml:"api-key"`; Model string}`.
  Validation at the same startup boundary as every other key, loud and naming the offender:
  a missing/empty `name`, a duplicate `name`, or a missing/empty `endpoint` is an error
  (`apogee: servers: entry N (...): ...`). `api-key` and `model` are optional (empty = keyless
  / no hint). The resolved list lands on `options.servers []serverEntry` (file layer only — no
  flag, no env, like `mcp-servers`).
- `cmd/apogee/defaults/config.yaml`: a new commented block in the template's voice, placed
  after `host-alias:`/`model:` — documenting: what an entry is; that `name` labels the picker
  row, is the `/server` argument, and becomes the footer alias; per-server `api-key` is
  file-only (`APOGEE_API_KEY` overrides the STARTUP server's key only); `model` is that
  server's discovery hint; a `/server` switch is session-only (next launch starts at
  `endpoint:` again); and that the startup endpoint is always offered even when unlisted.

**Tests.** `cmd/apogee/config_test.go`, house table style: a multi-entry block resolves in
order with all four fields; absent block ⇒ empty list; empty name / duplicate name / empty
endpoint each error naming the entry; per-entry `api-key`/`model` default empty. The existing
template-parses pin in `defaults_test.go` stays green (the new block is commented out).

**Acceptance.** Green gate passes. A config with a `servers:` block starts up clean; a
duplicate name fails startup with a message naming the entry.

**Commit.** `feat(config): servers: — named upstream endpoints for the /server picker`

## 2. The discovery hint follows the bound model — ✅ DONE (2026-07-28)

**What.** A successful rebind moves the Monitor's discovery hint to the model it bound, so
the next beat measures the binding as "nothing new" — the load-bearing half of the `/model`
picker (without it, a pick on a multi-model server flaps back within ten seconds).

- `internal/heartbeat/heartbeat.go`: `func (m *Monitor) SetModel(model string)` delegating to
  `m.client.SetModel` (already mutex-guarded for exactly this concurrent use). Doc comment in
  the house voice: the hint is a property of the BINDING — the host re-states it whenever a
  rebind commits, so discovery keeps resolving the model the session actually runs, not the
  one config named at launch.
- `cmd/apogee/wire.go`: the rebind closure calls `monitor.SetModel(spec.Model)` after
  `agent.Rebind(spec)` succeeds, beside `host.SetModel` (item 4 re-points this through the
  holder; here it is the direct monitor variable).

**Tests.** `internal/heartbeat/heartbeat_test.go`: against the existing fake multi-model
server, a Monitor constructed with hint A resolves A; after `SetModel("model-b")` the next
`Beat` resolves B's id and window (pinning that the hint, not the advertisement order, wins).
TUI-level flap-back protection is pinned in item 5's tests (the closure wiring itself is one
line, verified there end-to-end through the seam fakes).

**Acceptance.** Green gate passes. The heartbeat package's tests prove a moved hint changes
what the next beat resolves.

**Commit.** `feat(heartbeat): Monitor.SetModel — the discovery hint follows the binding`

## 3. `Agent.SwitchUpstream` — the engine half of a server switch — ✅ DONE (2026-07-28)

NOTES (2026-07-28): the "client actually swapped" test could not use the item's literal shape —
a *fake* new responder observing `SetModel("new")` — because `SwitchUpstream` constructs a real
`provider.NewClient` by contract, so nothing fake can be behind the seam after a switch. Same
guarantee, stronger evidence: `TestSwitchUpstreamSwapsTheProviderClient` points the switch at an
httptest OpenAI-compatible server, then asserts the post-switch `Rebind` + Exchange lands ONE
request there carrying `model: "new-model"` and `Authorization: Bearer new-key`, while the
retired fake responder records neither a `SetModel` nor a further request.

**What.** The one entry point that moves a session to another endpoint, honouring
`provider.Client`'s "new Client, not a mutated one" contract and Rebind's boundary discipline.

- `internal/agent/rebind.go` (beside `Rebind` — same file, same lifecycle concern):

  ```go
  type UpstreamSpec struct {
      Endpoint string // required — errMissingEndpoint stands
      APIKey   string // the new server's bearer token; "" sends no auth header
  }
  func (a *Agent) SwitchUpstream(spec UpstreamSpec) error
  ```

  Gates, in order: `a.turns.inExchange` → `domain.ErrInputPending` (idle-only, the Rebind
  posture); empty `spec.Endpoint` → `errMissingEndpoint` (the same sentinel construction
  uses — the requirement did not vanish, it moved). Commit (nothing can fail past the gates):
  `a.upstream = provider.NewClient(spec.Endpoint, "", provider.WithAPIKey(spec.APIKey))`;
  `a.cfg.Endpoint`, `a.cfg.APIKey` updated; `a.cfg.Model = ""` (the model is UNBOUND — the
  new server's first beat rebinds through the ordinary path, and `errNoModelBound` guards
  `Submit` in the gap, exactly the async-cold-start shape); `a.tokens` and `a.compactSat`
  reset (Rebind's own rationale). Doc comment records what deliberately stands: the
  conversation and Turn counters, mode, approvals, confinement, tools, profile — and that the
  Mechanism registry stays armed for the old model until the follow-up Rebind replaces it
  (unreachable meanwhile: no request can open unbound).
- `apogee.go`: the root alias `type UpstreamSpec = agent.UpstreamSpec` beside `RebindSpec`
  (ADR 0010 — the method rides the existing `*Agent`).

**Tests.** `internal/agent/rebind_test.go` (or a sibling `switchupstream_test.go`), the
existing harness patterns: mid-Exchange refusal (`ErrInputPending`, bindings untouched);
empty-endpoint refusal; after a switch, `Submit` fails `errNoModelBound`; the conversation
survives (history length and content unchanged across the switch); a subsequent
`Rebind{Model: "new"}` succeeds and the fake new responder observes `SetModel("new")` while
the OLD responder observes nothing (the client actually swapped); estimator/latch reset
(white-box, the Rebind tests' own style).

**Acceptance.** Green gate passes. The public surface builds with the alias; an embedder can
switch endpoints at idle and the session survives it unbound-but-intact.

**Commit.** `feat(agent): SwitchUpstream — move the session to another endpoint at the boundary`

## 4. The composition root: a swappable Monitor and the two new seams — ✅ DONE (2026-07-28)

NOTES (2026-07-28): two precisions on the item's literal text. (a) The holder landed in the
authorized sibling `cmd/apogee/upstream.go`, so its tests landed in `cmd/apogee/upstream_test.go`
rather than `wire_test.go` — same package, same seam-level style, co-located with the source per
the Go testing standard. (b) `upstreamHolder.Beat` holds the mutex only for the pointer read and
releases it before the observation: holding it across a beat (seconds against a hung server) would
stall the Update goroutine's next `Swap`/`SetModel` behind it, and the item's own "a swap mid-beat
is safe because the retired generation makes the landing inert" clause presumes exactly this.

**What.** The holder that makes "the same two seams" true, the picker's server data, and the
switch closure.

- `cmd/apogee/wire.go` (or a sibling `upstream.go` in `cmd/apogee` if `wire.go` crowds): an
  unexported `upstreamHolder{mu sync.Mutex; monitor *heartbeat.Monitor}` with `Beat(ctx)`
  (delegate under the mutex — safe from the beat Cmd goroutine), `SetModel(string)`, and
  `Swap(*heartbeat.Monitor)`. Seeded with the startup Monitor; `Options.Heartbeat` becomes
  `holder.Beat`; the rebind closure's item-2 line becomes `holder.SetModel(spec.Model)`.
  `heartbeat.NewMonitor`'s "the endpoint never changes mid-session" doc paragraph is
  rewritten: the MONITOR is per-server — a server switch swaps the whole Monitor (key and all),
  `SetModel` moves only the hint.
- Choice assembly: from `opts.servers` (item 1) build the closure-side entry list and the
  display list. When no entry's endpoint equals `opts.endpoint`, prepend a synthesized entry
  `{name: opts.hostAlias, endpoint: opts.endpoint, apiKey: opts.apiKey, model: opts.model}`.
- `internal/tui/tui.go`: `Options.Servers []ServerChoice` with
  `ServerChoice{Name, Endpoint string}` (display + identity only — keys and hints never reach
  the renderer), and
  `Options.SwitchServer func(name string) (ServerSwitchResult, error)` with
  `ServerSwitchResult{Endpoint, HostAlias string; ContextWindow int}`. Doc comments in the
  Options voice: the TUI owns WHEN, the binary owns everything the switch touches; `nil` /
  empty ⇒ `/server` degrades to a note; the result carries what the display adopts —
  including the pinned window, so the renderer keeps no knowledge of the pin.
- The `SwitchServer` closure in `runRoot`: resolve `name` against the assembled entries
  (unknown ⇒ error naming the known names); `agent.SwitchUpstream(apogee.UpstreamSpec{…})` —
  the commit point; then (nothing can fail) `holder.Swap(heartbeat.NewMonitor(entry.endpoint,
  entry.model, entry.apiKey))`, `host.SetModel("")` (a save in the unbound gap must not claim
  the old model), and return `{entry.endpoint, entry.name, pinnedWindow}`.

**Tests.** `cmd/apogee/wire_test.go`, seam-level like the existing wiring tests: the holder's
`Beat` reflects a `Swap` (fake monitors distinguishable by their fake servers' answers, or by
distinct failure strings); choice assembly — configured entries pass through in order, the
synthesized current row appears exactly when the startup endpoint is absent and carries the
resolved alias; the switch closure — unknown name errors naming candidates and touches
nothing; a successful switch swaps the holder and restamps the host model to "" (observable
via a follow-up `Save`'s metadata, the sessionHost tests' style).

**Acceptance.** Green gate passes. `apogee` runs exactly as before when no `servers:` block
exists (one synthesized choice, seams wired, nothing else observable).

**Commit.** `feat(wire): a swappable upstream Monitor behind the heartbeat seam + the SwitchServer seam`

## 5. `/model` — the model picker over `hb.models` — ✅ DONE (2026-07-28)

NOTES (2026-07-28): three precisions on the item's literal text. (a) The degrade ladder is asked for
BOTH forms, not only the bare one: an argument form that reached the accept path with `Rebind == nil`
would call `applyRebind`, which returns early on a nil seam — moving nothing and saying nothing, the
one outcome a command must never have. Surplus arguments still short-circuit to the usage note ahead
of the ladder. (b) `· current` is composed as plain row text rather than "in the faint style":
`renderPopup`'s contract takes rows escape-stripped and styles them whole (faint, or the highlight
bar on the selection) before truncating, so a per-fragment style could not survive it — unselected
rows already render faint. (c) `TestCommandDropdownOffersSkill` (skill_test.go) now asserts the
computed offering instead of the painted pane: the dropdown paints a scrolled `maxAutocompleteItems`
window around the selection, so the tenth verb pushed `/skill` out of the visible rows from row 0 —
pinning the render there would silently cap how many verbs `commandSpecs` may hold.

**What.** The verb, the shared picker overlay, and the accept path that drives the existing
rebind orchestration.

- `internal/tui/command.go`: `commandSpecs` gains
  `{name: "model", summary: "switch to another model the server serves", takesArgs: true}`
  (not `whileRunning` — idle-only by the table policy; the menu's `— idle only` tag and
  `commandsAtIdleNote` come free). `parsedInput` gains `args []string`, populated from
  `matchCommand`'s tokens for every `takesArgs` verb (`/confine` keeps its dedicated parse on
  top); the "only /confine reads the arguments" comments update.
- New `internal/tui/picker.go`: the shared single-select overlay —
  `picker{open bool; kind pickerKind; selected int}` (zero value closed; plain values, ADR
  0011). Rows are derived at render: `pickerModel` reads `m.hb.models` live (a beat under an
  open picker refreshes the list; selection clamped), each row
  `displayModel(id) — <formatTokens(window)>` (window omitted when 0), the row whose id equals
  `m.opts.Model` suffixed `· current` in the faint style. Painted through `renderPopup`:
  title `switch model — <hostDisplay(m.opts)>`, `maxRows` 8 (the `maxSessionRows` posture),
  hint `↑/↓ select · ⏎ switch · esc close`. While open it has first claim on keys
  (the `sessionBrowser` routing precedent).
- `runCommand` case `"model"`: with no argument — degrade ladder, each a note and no overlay:
  heartbeat unwired ⇒ `"/model needs the upstream monitor — not wired"`; offline ⇒ the
  existing offline wording via `upstreamBlockNote`'s facts; `m.opts.Rebind == nil` ⇒
  `"model switching is unavailable — the display is read-only"`; `len(m.hb.models) == 0` ⇒
  `"the server has not advertised any models yet"`. Otherwise open, pre-selecting the current
  model's row. With one argument: exact-id match against `m.hb.models` ⇒ the same accept path;
  no match ⇒ `"unknown model \"…\" — /model with no argument lists what the server serves"`;
  more than one ⇒ usage note (`usage: /model [model-id]`).
- Accept (⏎, shared by picker row and argument form): picked id equals `m.opts.Model` ⇒
  close + note `already bound to <displayModel(id)>`. Otherwise: record the pick as the last
  observation (`m.hb.observedModel/observedWindow = picked id/window` — the observeBinding
  posture, so the next beat is "nothing new"), close the overlay, and run
  `applyRebind(rebindIntent{model: id, window: summary.ContextWindow})` — every consequence
  (seam call, fail-once note, box restated, `rebindNote`'s "model changed: A → B" wording,
  notices, unknown-window honesty) comes from the one existing path.
- `internal/tui/doc.go` + the `m.hb.models` stash comment: "nothing renders it" becomes "the
  /model picker renders it"; the package narration gains the picker in the house voice.

**Tests.** `internal/tui/heartbeat_test.go` + a new `picker_test.go`, existing helpers:

- *Open lists the offering:* after an `upBeat` carrying two models, `/model` opens; the view
  contains both display names and the current row's `· current` marker.
- *Accept rebinds through the seam:* accepting the other row calls the `wireRebind` fake with
  its id and window, notes `model changed: … → …`, restates the box, and adopts the result.
- *The next beat is quiet (the flap-back pin):* after the pick, folding a beat that reports
  the picked model yields NO further rebind call and no note (`observedModel` moved with the
  pick).
- *Rebind failure:* a failing seam notes `rebindFailNote`'s wording once, bindings unmoved.
- *Degrades:* offline, empty offering, nil Rebind, unwired heartbeat — each notes and opens
  nothing.
- *Argument form:* `/model <known-id>` switches without an overlay; unknown id and surplus
  args earn their notes; `/model` mid-run earns `commandsAtIdleNote` (the table policy pin).
- *Current pick is a note, not a rebind:* accepting the current row calls no seam.

**Acceptance.** Green gate passes. Live against the multi-model host: `/model` lists what
`/v1/models` serves with the bound one marked; picking another rebinds within the same
keypress (footer, box, note), and the session STAYS on it across later beats.

**Commit.** `feat(tui): /model — pick among the models the server serves`

## 6. `/server` — the endpoint switch — ✅ DONE (2026-07-28)

NOTES (2026-07-28): four precisions on the item's literal text. (a) The "/server is deferred" sentence
was already gone — item 5 rewrote that whole `commandSpecs` paragraph — so this item extended the
rewritten clause to name `/server` beside `/model` instead of deleting a sentence that no longer
existed. (b) The switch fold landed in `model.go` as `Model.foldServerSwitch`, beside the rebind
orchestration whose `heartbeatState` it replaces (the item's Accept bullet names no file); `picker.go`
keeps the verb, the overlay and the accept path that calls it. (c) The refused-switch note is prefixed
`could not switch server: <err>` rather than being the bare error text — a lone error string in the
scrollback has no subject, the `rebindFailNote` posture. (d) The switching note's `(<endpoint>)` clause
is omitted when the alias IS the endpoint (an aliasless server would otherwise be named twice).

**What.** The second picker kind and the switch fold that rehomes the session.

- `internal/tui/command.go`: `commandSpecs` gains
  `{name: "server", summary: "switch to another configured server", takesArgs: true}`; the
  "/server is deferred" comment sentence is removed.
- `picker.go`: `pickerServer` rows from `m.opts.Servers` (`Name — Endpoint`), current row =
  endpoint equal to `m.opts.Endpoint`, suffixed `· current`; title `switch server`, same
  hint/caps.
- `runCommand` case `"server"`: degrades — `m.opts.SwitchServer == nil` or
  `len(m.opts.Servers) == 0` ⇒ `"no servers configured — add a servers: block to config.yaml"`
  (one wording; an empty list and a nil seam are the same user situation). One argument:
  exact name match against `m.opts.Servers` ⇒ accept path; no match ⇒
  `"unknown server \"…\" — configured: <names>"`; surplus ⇒ usage note.
- Accept: chosen endpoint equals `m.opts.Endpoint` ⇒ close + note
  `already on <name> (<endpoint>)`. Otherwise call `m.opts.SwitchServer(name)` synchronously
  on the Update loop (it is engine mutation + construction, no network — the applyRebind
  posture). Error ⇒ note it, nothing moves. Success ⇒ the switch fold:
  `m.opts.Endpoint/HostAlias/ContextWindow` adopt the result; `m.opts.Model = ""` (unbound —
  the footer says `connecting…`); `m.hb = heartbeatState{gen: m.hb.gen + 1, switched: true}`
  (retired generation ⇒ every in-flight beat/tick of the old chain is inert; cold-start
  offline posture against the new server; empty offering); the start-up box restated
  (`transcript.refreshStartup(newStartupView(m.opts))`); note
  `switching server: <old hostDisplay> → <name> (<endpoint>)`; return `m.beatCmd()` so the
  new chain's first beat fires NOW rather than in ten seconds.
- `heartbeatState.switched bool` (plain value): set only here, never cleared; `foldBeat`'s
  first-contact capture becomes `!m.hb.everOnline && m.hb.failures == 0 && !m.hb.switched`,
  with the doc comments (foldBeat's and `rebindNote`'s quiet-seed clause) growing the
  post-switch exception in the same voice: after a switch the box is not "a few rows above",
  and the human asked — the bind is the answer.
- `internal/tui/doc.go`: the narration's heartbeat section gains the switch (one code path
  with the cold start, the generation retirement, the announced post-switch seed).

**Tests.** `heartbeat_test.go` / `picker_test.go`, seam fakes for `SwitchServer`:

- *The happy path, end to end:* two configured servers; accept the other one ⇒ the seam is
  called with its name; opts adopt the result (endpoint, alias, pinned window); model
  unbound; box restated; the switching note present; a beat Cmd returned. Folding that new
  chain's `upBeat` then drives a rebind through the seam AND notes `connected: <model>`
  (`switched` defeats the quiet seed — the pin distinguishing this from plan 04's launch
  case).
- *The old chain is dead:* a beat/tick Msg carrying the pre-switch generation folds to
  nothing after the switch.
- *Blocked in the gap:* between the switch fold and the first bind, a send is refused with
  the connecting wording naming the NEW endpoint.
- *Cold-start posture against a dead new server:* the new chain's first `downBeat` notes
  offline immediately (`everOnline` reset did its job).
- *Error path:* a failing seam notes the error; opts, hb, and the transcript box unchanged.
- *Already-on, unknown name, no-servers, surplus args, mid-run* — each note pinned.
- `cmd/apogee` (item 4's tests already cover the closure): nothing new here.

**Acceptance.** Green gate passes. Live: with two entries configured, `/server` to the second
shows `switching server: …`, the footer flips to `connecting…` under the new alias, and within
a beat the new server's model binds with `connected: <model>`; `/server` back restores the
first. Killing the new server before its first beat shows offline immediately; the session's
conversation survives the whole round trip.

**Commit.** `feat(tui): /server — switch to another configured server mid-session`

## 7. Documentation close-out — ✅ DONE (2026-07-28)

NOTES (2026-07-28): one addition beyond the item's literal text. The TODO entry's restated remainder
carries a THIRD bullet naming this plan's deliberate non-goals (a `--save` form for `/server`, a
`server:` startup key, persisting a switched endpoint in the session record) — the item names only
the llama.cpp and model-profile halves, but those non-goals are decided-and-parked work stated in
this plan's own *Design decisions* and in ADR 0028's Considered options, and TODO.md is where a
reader looks for them. Also: `internal/tui/doc.go` needed nothing here — items 5 and 6 already grew
the narration's picker and switch sections, so the item's docs list is complete without it.

**What.**

- **ADR 0028** — `docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md`
  (Status: accepted), recording this plan's settled design in the ADR voice: the pickers are
  UI over ADR 0024's prepared layers, never a third binding path; the hint follows the
  binding (the flap-back defect); `SwitchUpstream` unbinds and the first beat completes the
  switch (one code path with the cold start); the Monitor is per-server, swapped whole behind
  the unchanged seams; `servers:` shape and the session-scoped switch; the announced
  post-switch seed. Considered-and-rejected options from the *Design decisions* section
  (mutating the client's endpoint; keeping the stale binding; Agent reconstruction; TUI-side
  seam swapping; a `--save` form) land in its Considered options.
- **CONTEXT.md**: the Heartbeat/Rebind entry gains the switch — `/model` drives Rebind by
  hand, `/server` swaps the Monitor and the provider client and lets the first Beat complete
  the move — worded to match ADR 0028 and cross-referenced from the Upstream entry.
- **layout.md**: the command-menu prose's "the two verbs" exception grows to name `/model` and
  `/server` (takesArgs ⇒ complete-not-run); a short overlay paragraph for the picker beside
  the `/sessions` browser's, in the existing voice.
- **TODO.md**: the "[P1] Server / model switching" entry's picker-UI and endpoint-switch
  halves CLOSE — move them to the entry's "Shipped since parking" ledger pointing at ADR 0028
  and this plan; what remains of the entry is **local llama.cpp start/stop** (rebuild over
  `heartbeat.Beat`) and the **model-profile** abstraction, restated as the remainder.
- **CHANGELOG.md** under `## [Unreleased]`: `### Added` — the `/model` picker; the `/server`
  switch with the `servers:` config key; `Agent.SwitchUpstream` + `apogee.UpstreamSpec`
  (public surface — the next cut is a minor bump). `### Fixed` — the discovery hint now
  follows the bound model (the flap-back). `VERSION` untouched.

**Tests.** None new; `make check` green.

**Acceptance.** Green gate passes. A reader can go from TODO.md's trimmed entry to ADR 0028
to the code without re-deriving a decision; CONTEXT.md's nouns cover everything the
transcript now prints.

**Commit.** `docs: ADR 0028 + CONTEXT/layout/TODO/CHANGELOG for the model and server pickers`
