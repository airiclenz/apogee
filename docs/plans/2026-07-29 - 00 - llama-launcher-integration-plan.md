# Plan — llama-launcher as a library: /load, /unload, /stop

**Date:** 2026-07-29
**Status:** READY (design grilled 2026-07-29; all four TODO forks resolved by the owner — no
needs-design-call escalation expected). **Prerequisite:** llama-launcher **v1.6.1** (the
portability release — its plan `docs/plans/2026-07-29-portability-release-plan.md` *in that
repo*) must be tagged and pushed before item 2 lands; item 1 has no dependency and can go
first in either order.
**Design authority:** [ADR 0029](../adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
(written in the grill session that produced this plan), extending ADR 0024/0028. CONTEXT.md
already carries the **Launch profile** term (same session).
**Source:** owner decision 2026-07-28 (TODO.md "[P1] Server / model switching", the
local-server half) + the grill 2026-07-29.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs, no AI attribution trailers. `make check` green gates every item.

---

## The shape (context for implementers)

The launcher facade (`github.com/airiclenz/llama-launcher/launcher`, ≥ v1.6.1) is imported at
the **composition root only** — `cmd/apogee` — and reaches `internal/tui` exclusively as
closures behind new nil-degrading `tui.Options` seams, exactly how `Heartbeat`/`Rebind`/
`SwitchServer` already work (ADR 0024 D5, ADR 0028 D4). The engine (`internal/agent`) is
untouched except that the existing `SwitchUpstream` is *called* by a new closure. Facade
contract to respect everywhere: lifecycle verbs **block** (up to ~30 s health wait + ~20 s
stop escalation; 5-min model loads) and must be **serialized by the caller** — the TUI's
actuation latch (item 5) is that serialization; read verbs are concurrency-safe; config is
re-read fresh per operation via `launcher.LoadConfig` (never cached, never `Config.Reload`);
notices/progress arrive on the calling goroutine as callbacks.

---

## 1. The `llama-launcher:` config key — file-only, auto-detect by default

**What.** The one new config surface (ADR 0029 D4). `cmd/apogee/config.go`: a
`LlamaLauncher string` field on `fileConfig` (yaml `llama-launcher`), plus the file-only
plumbing in the observable pattern (`options` field beside `servers` at config.go:58, `layer`
field ~:415, the direct assignment in `resolveSettings` beside `s.servers = file.servers`
~:531, the `layer()` projection ~:1024, the write-back ~:1257). Semantics resolved at the
root, not in config parsing: empty/absent → auto (use the facade's `DefaultConfigPath()` iff
that file exists), `off` → disabled, anything else → a config-file path (tilde-expanded).
Validation (a `validateLlamaLauncher` beside `validateServers`, called from `applyConfig`
~:1248): the value may be empty, `off`, or a path — **no existence or reachability check at
startup** (the `servers:` posture, config.go:797-799); a configured-but-missing path degrades
at use time (item 4's ladder). `cmd/apogee/defaults/config.yaml`: a commented block under the
`servers:` block documenting the three values and the auto-detect default.

**Tests.** `config_test.go`, the existing table style: absent key resolves empty; `off` and a
path pass through; file-only (env/flag never set it — same assertion shape as `servers:`);
write-back lands on `options`.

**Acceptance.** Green gate. `apogee` with no new config behaves byte-identically (key absent
→ empty → root-side auto-detect finds nothing in CI).

**Commit.** `feat(config): file-only llama-launcher key — auto-detect, off, or a path`

## 2. The launcher bridge — `cmd/apogee/launcher.go` + the dependency

**What.** The only file that imports the facade, and the seam that makes every later item
fake-testable. `go.mod` gains `github.com/airiclenz/llama-launcher v1.6.1` (tagged — never a
`replace`; local cross-repo dev via untracked `go.work`, which this item documents in the file
header, not in git).

- A small package-private seam interface (the `activationOps` discipline, client-side):

  ```go
  type launcherOps interface {
      loadConfig(path string, notice func(string)) (*launcher.Config, error)
      discover(cfg *launcher.Config) []*launcher.RunningInstance
      loadProfile(cfg *launcher.Config, p *launcher.ResolvedProfile, restart bool,
          progress func(string), notice func(string)) (*launcher.RunningInstance, bool, error)
      stop(addr string) (*launcher.StopResult, error)
      unload(backend, addr string) (*launcher.StopResult, error)
  }
  ```

  with a `realLauncher` adapter of one-line delegations, and the resolution helper
  `launcherConfigPath(opts) (path string, enabled bool)` implementing item 1's
  empty/`off`/path ladder (auto = `launcher.DefaultConfigPath()` + `os.Stat`).
- Row assembly for the picker: `launchProfiles(ops, path)` → fresh `loadConfig` →
  `cfg.ProfileNames()` (the launcher's own order, favourites first) → per name
  `cfg.ResolveProfile` for the backend, resolved `host:port` and merged
  `ProfileParams.ContextSize` (nil-safe — unset stays 0/unknown) → one `discover` pass to mark
  running profiles (match by the instance's attributed profile name; ambiguous attribution
  marks nothing). Resolution errors skip the row with a collected warning, they never sink the
  list.
- Address bookkeeping: `endpointAddr(endpoint string) (host:port, error)` (URL parse) and
  `addrEndpoint(addr) string` (`http://` + addr) — the two directions items 3 and 6 need.

**Tests.** `launcher_test.go` with a `fakeLauncher` (scripted instances/profiles/errors — the
CI-side fake the TODO's settled decision named): row assembly order + ctx + running
attribution; resolve-error row skipped, warning collected; the path ladder (absent file → not
enabled; `off`; explicit path); `endpointAddr` round-trips and rejects garbage. No real
processes, no network.

**Acceptance.** Green gate on linux; `GOOS=windows` and `GOOS=darwin` cross-builds stay green
(this is the item that proves the v1.6.1 prerequisite).

**Commit.** `feat(cmd): launcher bridge — facade dependency, ops seam, profile rows`

## 3. The seams: `tui.Options` members + the wire closures

**What.** The TUI-facing surface (ADR 0029 D1/D2), all nil-degrading like their ADR 0024/0028
siblings (`internal/tui/tui.go`):

  ```go
  LaunchProfiles func() ([]LaunchProfileChoice, error)          // picker rows, fresh per open
  LoadProfile    func(name string, progress func(step string)) (ProfileLoadResult, error)
  UnloadServer   func(endpoint string) (ActuationResult, error)
  StopServer     func(endpoint string) (ActuationResult, error)
  ```

  `LaunchProfileChoice{Name, Backend, Addr string; ContextWindow int; Running bool}`;
  `ProfileLoadResult{Moved bool; Switch ServerSwitchResult; Notices []string}`;
  `ActuationResult{Steps []string; ServerStopped bool}` (the launcher's `StopResult`,
  projected — TUI types never leak facade types, the `heartbeat.Beat` precedent).
- `cmd/apogee/wire.go` builds the closures over item 2's bridge (beside the switch closure,
  wire.go:378-397). `LoadProfile` is the composite verb, ADR 0029 D2: fresh config →
  `ResolveProfile` → `ops.loadProfile` (blocking; progress forwarded) → on success compare the
  profile's `host:port` against the *current* endpoint (`holder`/`opts` at call time, not
  captured at wire time): same address → `Moved: false` (the next beat completes it);
  different → `agent.SwitchUpstream(apogee.UpstreamSpec{Endpoint: addrEndpoint(...), APIKey: key})`
  where `key` is the api-key the launcher config carries for the profile's server (its
  `servers:` mapping form; empty when unset — the common local case)
  + `holder.Swap(heartbeat.NewMonitor(endpoint, "", key))` — **hint empty**, the profile name
  is not a wire model id; alias = the profile name — + `host.SetModel("")`,
  returning `Moved: true` with the `ServerSwitchResult` shape the existing fold understands
  (the switch-closure body at wire.go:378-397 is the template; extract the shared core rather
  than copying it). `UnloadServer`/`StopServer`: fresh config → `discover` → match the
  session endpoint's `host:port` → no match = error `the launcher doesn't manage <endpoint>`;
  match → `ops.unload(backend, addr)` / `ops.stop(addr)`, steps projected. Launcher config
  notices land in `Notices`/as returned warnings, for item 5 to print. When item 1 resolves
  *not enabled*, all four members are wired nil.

**Tests.** `wire_test.go` against the `fakeLauncher`: same-address load → no `SwitchUpstream`,
`Moved` false; cross-address load → agent endpoint moved, monitor swapped (the switch-closure
test at its existing site is the pattern), model unbound; unload/stop endpoint matching incl.
the not-managed error; nil wiring when disabled. Engine untouched — no `internal/agent`
changes, so no tests there.

**Acceptance.** Green gate; `tui.Options` documents each member's nil meaning in the ADR 0024
voice.

**Commit.** `feat(wire): launcher seams — load follows the profile, unload/stop act on the
session's endpoint`

## 4. `/load` — the command and the picker

**What.** ADR 0029 D3's first verb, built on the shipped picker chassis
(`internal/tui/picker.go`). A `pickerProfile` kind in the enum (:41-46); command-table row
`load` (`command.go:96-97` — `takesArgs: true`, not `whileRunning`); dispatch beside
`/model`/`/server` (`model.go:1101-1111`). Bare `/load`: rows from `opts.LaunchProfiles()`
called at open (fresh, per ADR 0029 D4) — label `<name> — <backend>` + `· <ctx>` via
`formatTokens` when known + `(:<port>)` when the address is not the session's + `· running`
when attributed; title `load profile — llama-launcher`. `/load <name>`: exact match, else the
usage note with the candidate list (the `/server` grammar, picker.go:159-167). Degrade
ladder, one sentence each, no overlay: seams nil → `llama-launcher not configured — set
llama-launcher: in config.yaml or install the launcher`; `LaunchProfiles` error (missing
config at an explicit path, config parse failure) → the error text as a note; zero profiles →
`no launch profiles defined — add profiles to <path>`. Accept → hand off to item 5's latch
(this item can stub the accept with a note; the fold ships next item — or land 4+5 together
if the seam makes a stub awkward).

**Tests.** `picker_test.go` style: rows render with ctx/running/port markers; exact-arg match;
each degrade rung; the accept path reaches the latch entry point.

**Acceptance.** Green gate; `/load` browses real rows against the fake; no actuation yet
required.

**Commit.** `feat(tui): /load — the launch-profile picker over the launcher seams`

## 5. The actuation latch, the progress narration, and the completion folds

**What.** The concurrency and UX heart (ADR 0029 D5/D6).

- **Latch:** `m.actuation {inFlight bool; verb string; profile string}` on the value Model.
  While held: the three Exchange-opening paths and all five switching/actuation commands
  (`/load /unload /stop /model /server`) refuse with one note (`profile load in flight —
  <name>`); Esc does **not** cancel (the facade's cancel is `Stop` after return). The latch
  is the facade-required per-address serialization: it is taken before the Cmd spawns and
  released only in the completion fold.
- **Progress pump:** the blocking `opts.LoadProfile` runs in a background `tea.Cmd`
  goroutine; its `progress` callback feeds a buffered channel; a re-arming listen Cmd drains
  it into the transcript as one note per step (the engine-event pump idiom, ADR 0011 — follow
  the existing pattern; generation-guard the pump like `heartbeatLive`). Notices (config
  warnings, the launcher drift notice) land as notes the same way.
- **Footer:** while the latch is held for a load, the model slot shows `loading <profile>…`
  (takes precedence over `connecting…`, `model.go:2311-2328`); for unload/stop, `<verb>…`.
- **Beat shadow:** while the latch is held, `foldBeatFailure` (`model.go:1849-1861`) counts
  nothing toward the offline crossing (ADR 0029 D5's D7(a) symmetry — the server is expected
  to be down mid-restart). Landed beats stay live — a successful beat mid-actuation is
  harmless and the post-actuation beat is the completion.
- **Completion folds:** success + `Moved` → the `foldServerSwitch` shape (`model.go:1811-1824`;
  alias = profile name, fresh heartbeat generation, `switched` flag so the seed announces) +
  immediate `beatCmd()`. Success same-address → release the latch, note
  `profile <name> loaded — waiting for the beat`, immediate `beatCmd()` (the beat prints
  `connected: …` via the announced seed only if `switched`; same-address relies on the
  ordinary `rebindNote` `model changed: A → B`, which is already the honest wording). Error →
  the launcher's error text as a note; when `errors.Is(err, launcher.ErrStartupTimeout)` (the
  v1.6.1 sentinel), append the coda `the heartbeat will bind it if it comes up` — and it
  will, through the unsuppressed landed-beat path. Unload/stop completions: one note per
  recorded step (`ActuationResult.Steps`), then for stop the offline handling owns the
  display (no special casing — the next two idle failures cross it, which is now honest:
  the latch released, the downtime is real).

**Tests.** `model_test.go`/a sibling: latch refusals (send, second `/load`, `/model`); the
pump orders steps into notes; footer strings; beat-failure suppression while held vs counted
after release; both completion folds (fake seams driving `Moved` both ways) incl. the
immediate beat; the timeout coda on the sentinel; latch released on every path (success,
error, panic-safe via the Cmd's return).

**Acceptance.** Green gate incl. `-race` (the pump crosses goroutines). The ADR 0011
no-Builder-by-value rule holds on every new Model field.

**Commit.** `feat(tui): actuation latch + narrated profile loads — the beat completes the move`

## 6. `/unload` and `/stop`

**What.** The two remaining verbs (ADR 0029 D3), no pickers: command rows + dispatch; both
run through item 5's latch and pump (steps → notes). `/unload`: note the managed semantic
when the result reports the server stopped (`model unloaded — this stopped <backend>` vs
`model unloaded — server still up`). `/stop`: `stopping <backend> (<addr>)` then the steps;
afterwards the footer's existing offline crossing narrates the rest. Degrades: the item-3
not-managed error verbatim; seams nil → item 4's not-configured line.

**Tests.** Both verbs through the latch against the fake; managed-vs-external unload wording;
not-managed endpoint; stop then simulated beat failures cross offline normally (post-latch).

**Acceptance.** Green gate; the three verbs coexist and serialize through one latch.

**Commit.** `feat(tui): /unload and /stop — the session's server, freed and stopped`

## 7. Docs pass

**What.** README: the slash-command table gains `/load`, `/unload`, `/stop` — **and the
missing `/model` + `/server` rows** (pre-existing doc gap, verified 2026-07-29) — plus a short
"Local servers via llama-launcher" section (the key, the auto-detect, the MCP-adapter remote
composition). `cmd/apogee/defaults/config.yaml`: item 1 wrote the block; verify voice.
`layout.md`: the footer's `loading …` state, one sentence beside `connecting…`. CHANGELOG:
minor-bump entry (new key, three commands, the dependency); VERSION bumped in step.
TODO.md: the "[P1] Server / model switching" local-server sub-entry body leaves the file for
ADR 0029 + this plan (a pointer line remains; the model-profile half stays live, untouched).
CONTEXT.md and ADR 0029 are already in (grill session) — verify cross-links resolve.

**Tests.** None (docs); `make check` still gates.

**Acceptance.** No doc claims a capability the code lacks; TODO's deferral trail stays
deliberate (nothing silently dropped).

**Commit.** `docs: llama-launcher integration — README, template, layout, TODO close-out`

## 8. Owner-run live pass (same-machine host)

**What.** The end the CI fake cannot see (ADR 0029 consequences): on a host with the real
launcher config and a llama.cpp profile — `/load` a profile cold (server starts, steps
narrate, beat binds, footer completes); `/load` the other profile (restart path, ~20 s stop
escalation narrated); `/load` a profile on a second server (the `Moved` fold — footer alias
becomes the profile name, announced seed prints); `/unload` (managed → stopped wording);
`/stop` → offline crossing; a load that times out (an oversized model) → the coda, then the
late bind when it comes up. Record pass/fail per scenario in this item's NOTES; failures
reopen the relevant item.

**Tests.** This *is* the test. Nothing committed but the NOTES.

**Acceptance.** Every scenario observed on hardware; the plan archives only after this item.

**Commit.** — (notes-only; any fixes commit under their own item)
