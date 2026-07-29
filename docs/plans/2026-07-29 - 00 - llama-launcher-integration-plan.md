# Plan — llama-launcher as a library: /load, /unload, /stop

**Date:** 2026-07-29
**Status:** IN PROGRESS (design grilled 2026-07-29; all four TODO forks resolved by the owner —
no needs-design-call escalation expected). Items 1–7 landed 2026-07-29. Items 9–12 were added
2026-07-29 from the owner's first live run (see the addendum below) and are next; item 8 (the
owner-run live pass) stays open and should run AFTER they land, so the pass validates the
surface as revised rather than the one it replaced.
**Prerequisite — MET 2026-07-29:** llama-launcher **v1.6.1** (the portability release — its
plan `docs/plans/2026-07-29-portability-release-plan.md` *in that repo*) is tagged and pushed;
it builds on linux/darwin/windows and exports `ErrUnsupported` + `ErrStartupTimeout`. The
portability fix stayed in the **1.6.x** line — there is no v1.7.0 of the launcher.
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

## 1. The `llama-launcher:` config key — file-only, auto-detect by default — ✅ DONE (2026-07-29)

NOTES (2026-07-29): `validateLlamaLauncher` refuses two shapes rather than nothing at all — a
whitespace-only value (the `servers:` blank-name posture: it reads as configured but names
nothing) and a value carrying a `://` scheme (the key takes a LOCAL path; a remote launcher is
an `mcp-servers:` entry — ADR 0029). Still no existence or reachability check, as specified.
The template's example path is a placeholder (`~/path/to/…`) rather than the launcher's real
default config path, which this repo cannot state until item 2 imports the facade.

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

## 2. The launcher bridge — `cmd/apogee/launcher.go` + the dependency — ✅ DONE (2026-07-29)

NOTES (2026-07-29): six deviations from the item's literal text, all mechanical.
(a) The facade is imported **aliased as `llamalauncher`** — `cmd/apogee` already owns the
identifier `launcher` (root.go's function type for "the thing that opens the TUI"), so the seam
reads `*llamalauncher.Config`, not `*launcher.Config`. (b) `go.mod`'s go directive moved
`1.26` → `1.26.3`, forced by the launcher's own go.mod; nothing else in the graph changed but
the new direct requirement and `golang.org/x/term` as indirect. (c) `launchProfiles` returns
`([]launchProfile, []string, error)` — rows, collected warnings, and the one error that sinks
the list — over a cmd-local `launchProfile` row type (item 3 projects it onto
`tui.LaunchProfileChoice`); the plan left the signature open. (d) `endpointAddr` fills a
**scheme default port** (http→80, https→443) for a portless URL rather than refusing it — that
is the address the wire actually connects to; a portless URL whose scheme has no default is
still refused. (e) `fakeLauncher` scripts instances/errors/notices in memory but hands back a
**really parsed** `*llamalauncher.Config` (built from a temp config file through the facade's own
loader), because the launcher's `ServerConfig` type is not exported by the facade and a `Config`
literal therefore cannot be written outside that module — and faking `ProfileNames`/`ResolveProfile`
would fake the very ordering and merging this bridge projects. (f) `.gitignore` gained
`/go.work` + `/go.work.sum`, so the cross-repo dev override the file header documents cannot be
committed by accident.

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

## 3. The seams: `tui.Options` members + the wire closures — ✅ DONE (2026-07-29)

NOTES (2026-07-29): six deviations from the item's literal text.
(a) The four seam BODIES live in `cmd/apogee/launcher.go` (a `launcherWiring` type embedding the
shared mover), not in wire.go: they name facade types — `RunningInstance`, `StopResult` — and item 2
made launcher.go the only file allowed to. wire.go keeps the WIRING (the enable decision and the four
`tui.Options` members), which is what "beside the switch closure" is about.
(b) The shared core the item asks be extracted rather than copied is `sessionMover.move` in
`upstream.go`, over two new package-private seams (`upstreamSwitcher`, `modelStamper`) so the fold is
drivable in a test without constructing an Agent — `/server`'s closure is now resolution + `move`.
(c) `upstreamHolder` gained the current endpoint (`Endpoint()`; `Swap` takes it alongside the
Monitor). A `heartbeat.Monitor` deliberately exposes no endpoint, and the item requires the
same-address comparison read the session's endpoint *at call time* — which after a `/server` switch is
not `opts.endpoint`. The holder is the value the item itself names for that.
(d) The literal seam signatures are kept, so `LaunchProfiles` has no channel for item 2's collected
config warnings and they stop at that closure; the launcher's notices reach the human through
`ProfileLoadResult.Notices` — the one notices channel the fixed result shapes provide, and the verb
that actually acts on the config — while `ActuationResult.Steps` is `/unload`/`/stop`'s whole voice.
(e) `ops.loadProfile` is called with `restart=false` (the item left the flag unstated): the launcher's
idempotent activation, so a server already serving the profile is left alone and a drift notice is
preferred over restarting a server the human may be mid-conversation with.
(f) The move target is the resolved profile's address with the discovered instance's as a fallback,
and an address that stays empty reads as "same server" — an unknown address is never a licence to
re-point the wire. The per-profile key is `cfg.APIKeyFor(profile.Backend)`, an accessor the facade's
type aliases expose without a compatibility promise; it is apogee's only such call and it is confined
to launcher.go with the rest of the library's vocabulary.

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

## 4. `/load` — the command and the picker — ✅ DONE (2026-07-29)

NOTES (2026-07-29): the owner decided items 4 and 5 land together as ONE commit — item 4's accept
path is the real latch hand-off, never a stub — so both items were implemented in one run and are
verified and committed as a unit. Three deviations from this item's literal text.
(a) The zero-profiles note names NO path: `no launch profiles defined — add profiles to the
llama-launcher config`. The seam item 3 fixed carries rows and one error, so the renderer never
learns where the launcher's config lives — and ADR 0029 D1 is why it should not (a renderer that
guessed a path would eventually name the wrong one). The path is still named on the rung where it is
known: a configured-but-missing config comes back as the bridge's own error text.
(b) `picker` gained a `profiles []LaunchProfileChoice` field, the one offering NOT derived at render
time. The chassis' "rows are derived, never captured" rule is about state the Model holds; a Launch
profile lives in a config FILE behind a seam that re-reads it, so once-per-open IS ADR 0029 D4's
freshness — deriving per frame would make a keypress a file read.
(c) The unknown-argument note is `unknown launch profile "x" — configured: a, b` — the `/server`
grammar with CONTEXT.md's term in place of the bare word.

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

## 5. The actuation latch, the progress narration, and the completion folds — ✅ DONE (2026-07-29)

NOTES (2026-07-29): landed together with item 4 as one commit (see that item's note). Five
deviations from this item's literal text.
(a) `errors.Is(err, launcher.ErrStartupTimeout)` cannot be asked in `internal/tui` — ADR 0029 D1
keeps the facade out of the renderer, sentinels included. So the renderer owns
`tui.ErrStartupTimeout` and `cmd/apogee/launcher.go` (the only file that may name the library) marks
the launcher's own timeout with it through a wrapper that adds no text: both sentinels stay reachable
by `errors.Is` and the launcher's words (the PID and log path) reach the transcript unchanged. The
fold matches the projection; that is the whole difference.
(b) The pump is ONE channel carrying the steps AND the completion, not a steps channel beside a
separate done Msg. Two concurrently-run Cmds have no defined order, so a single queue is what makes
"every step is painted before the completion" an ordering guarantee instead of a race.
(c) `m.actuation` therefore also carries `gen` and that channel beside the three fields the item
lists — both are this item's own requirements (the generation-guarded pump), not new state.
(d) The completion's final item is sent from a DEFER, which is what makes "latch released on every
path, panic included" true by construction rather than by inspection.
(e) `startServerActuation` — the latch, pump and completion fold for `/unload`/`/stop` — is
implemented and tested here, since this item owns them, but no command dispatches it yet: item 6
wires the two verbs and owns their wording (the managed-unload sentence, the `stopping …` line).

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

## 6. `/unload` and `/stop` — ✅ DONE (2026-07-29)

NOTES (2026-07-29): four deviations from the item's literal text, all about where this item's wording
gets its facts.
(a) `tui.ActuationResult` gained `Backend` and `Addr`. Item 3 fixed the shape as `{Steps,
ServerStopped}`, but both sentences this item specifies name the backend — and the renderer cannot
derive it: the session holds an endpoint URL, not the address the launcher manages nor the name of the
server answering there, and the launcher's own steps are terse and subject-less ("Sending stop
signal"). `cmd/apogee` fills them from the instance `managedInstance` already discovered rather than
from `StopResult.Instance`, which is nil exactly when the verb failed — the moment naming what it was
acting on matters most.
(b) The `stopping <backend> (<addr>)` line is written at COMPLETION, directly above the steps, not when
the verb is asked for. Item 3's seam takes no progress callback, so an actuation's steps exist only in
the returned result; and the backend is the launcher's answer, unknown until the call returns. The
footer's `stop…` covers the in-flight window, as it does for a load before its first step.
(c) Both wordings degrade rather than print a blank: no backend and no address earns no heading at all,
a bare address heads the steps alone, and an unnamed backend makes the unload sentence `model unloaded
— this stopped the server`.
(d) A failed `/unload` prints no outcome sentence — the launcher's error is the last word, and
"model unloaded" beside it would be a claim the verb did not earn.

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

## 7. Docs pass — ✅ DONE (2026-07-29)

NOTES (2026-07-29): four deviations from the item's literal text.
(a) `layout.md` had NO prose about the footer at all — no `connecting…` sentence to put one beside —
so the `loading <profile>…` state landed as a new short section, *The footer's upstream slot*, which
states the slot's contents and the one-word replacements (`connecting…`, `loading <profile>…`,
`unload…`/`stop…`) and their precedence. The same file's mini-language section carried two claims the
shipped code had made stale — "the three that take arguments" (four now, `/load` joined) and the
picker overlay being `/model` and `/server` only — so both were corrected here rather than left
contradicting item 4.
(b) The template block was more than "verify voice": it claimed the three commands "simply do not
appear" without a launcher, which the shipped command table contradicts (they are static rows that
answer `llama-launcher not configured`). Corrected, and the launcher's real default config path is
now named — item 1's NOTES deferred exactly that until the facade was imported, which item 2 did.
(c) VERSION went `v0.9.9` → `v0.10.0` — the minor bump ADR 0029's consequences call for, rather than
the running patch bump this 0.9.x line has been taking.
(d) Of the three findings handed down from earlier items: the `filepath.IsAbs` guard on auto-detect
(a) IS folded in, as the "under your home directory" clause in the README's ladder and the named
default path in the template — the ladder is documented there, so the guard is user-visible fact. The
untested `restart=false` load seam (b) is NOT a doc change (item 7 has no known-limitations surface)
and is reported as a follow-up defect in item 3's territory instead. This plan's own stale header
line (c) is not in item 7's stated file list and was left for the plan's runner.

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
After items 9–12 land, the scenarios drive `/model` where they say `/load` — same folds, same
latch, the verb renamed under them.

**Commit.** — (notes-only; any fixes commit under their own item)

---

## Addendum 2026-07-29 — the surface as the first live session found it

**Source:** owner decision 2026-07-29, after the first live run of the shipped verbs. On a
single-model llama.cpp server the `/model` picker is a one-row list — the current model, under
a "⏎ switch" hint (`pickerHint`, picker.go:81) that switches nothing — while the list the
owner actually thinks in, the launcher's profiles, sits behind a separate verb. The owner's
call, four parts: `/model` IS the switching verb and offers the Launch profiles when the
launcher is configured, current one excluded; `/load` disappears from the surface; `/unload`
and `/stop` keep their logic but stop being offered; the dropdown reads alphabetically.
Items 9–12 carry that decision. ADR 0029 D3's three-verb surface is thereby revised —
item 12 records the amendment in the ADR rather than leaving the code to contradict it.

## 9. `/model` follows the launcher — profiles are the offering, the loaded one excluded — ✅ DONE (2026-07-29)

NOTES (2026-07-29): six deviations from the item's literal text.
(a) `currentRowSuffix` did NOT retire — only `currentModelRow` and the constant's `/model` USE did.
The item's own next clause keeps `/server`'s current-row posture untouched, and `serverRows` is the
constant's other caller; the two clauses cannot both be taken literally, so the narrower one wins.
Its doc comment now says the mark is `/server`'s alone and why.
(b) The exclusion is the OFFERING's, not the argument form's ("the row … is not offered"). `/model
<id>` still matches the whole advertised list, so the bound model earns `already bound to …` rather
than being called unknown; `/model <profile>` still matches every DEFINED profile, so a loaded one is
not called unknown either — re-activating it is the launcher's own idempotent no-op. In the launcher
branch the zero-profiles rung therefore stays ABOVE the argument form and the exclusion below it.
(c) `modelUsage` is reworded to `usage: /model [profile|model-id]`: the item retires `loadUsage` and
has `modelUsage` cover both forms, which the old `[model-id]` text did not.
(d) `runModelCommand` branches at the top into two package-private halves — `pickAdvertisedModel` and
`pickLaunchProfile` — rather than holding both ladders inline; `pickerLoad`, `launchProfileRows` and
the title constant keep their names, and only the title's TEXT becomes `switch model —
llama-launcher`.
(e) Two consequences outside the item's stated file list, both created BY it: `"load"` left
`actuationBlocked`'s refusal list (no dispatcher can produce that verb any more), and the doc
comments naming `/load` were reworded across `internal/tui` and `cmd/apogee` — a comment naming a
verb this item deletes is stale by this item, not a drive-by.
(f) `offerableProfiles` excludes nothing when the session endpoint will not parse (`sessionAddr` ⇒
`""`), the direction `elsewherePort` already takes: an address we cannot name is no evidence that a
profile shares it.

**What.** The merge of `/load` into `/model` (owner decision above), and the end of the
one-row picker.

- Command table (`command.go:102-104`): `/model`'s summary widens to cover both offerings
  (e.g. `switch model — the launcher's profiles, or what the server serves`); the `load` row
  is REMOVED. A typed `/load` then falls to the unknown-slash refusal (`unknownSlashNote`) —
  correct for a verb that no longer exists. Dispatch: the `"load"` case (model.go:1149)
  goes; `runLoadCommand` folds into `runModelCommand`.
- `runModelCommand` (picker.go:108-127) branches on the wired seams, ONCE, at the top:
  - **Launcher mode** (`opts.LaunchProfiles != nil && opts.LoadProfile != nil`): the verb
    is the old `/load`, whole — rows via `opts.LaunchProfiles()` read at open (ADR 0029 D4
    freshness), `/load`'s own degrade ladder (config error → the launcher's words as a note;
    zero profiles → `noProfilesNote`), the argument form matching profile names with the
    `profileNameList` unknown-note, accept → `startProfileLoad` through the actuation latch
    (actuation.go) — which is where the owner's `auto_stop_server`/`auto_unload` semantics
    live: the LAUNCHER honors its own config when a load displaces a running server or model
    (facade contract, item 2); apogee adds nothing. Deliberately NOT consulted here:
    `modelSwitchBlocked`'s offline rung — like `/server`, bringing a server up is the one
    useful act while the current one is down, and `/load` never consulted it either.
  - **Fallback** (seams nil — launcher `off` or absent): today's advertised-models behavior
    (`m.hb.models`, `modelSwitchBlocked` ladder, `bindPickedModel` accept), unchanged but for
    the exclusion below. A multi-model or remote server without the launcher keeps its picker.
- **The exclusion** (both modes — the feedback's headline): the row for what the session is
  already bound to is not offered. Launcher mode: drop a profile that is `Running` AND whose
  resolved `Addr` equals `sessionAddr(m.opts.Endpoint)` (the `elsewherePort` comparison);
  ambiguous attribution (item 2: marks nothing) excludes nothing. Fallback: drop the row where
  `offered.ID == m.opts.Model` (`currentRowSuffix` and `currentModelRow` retire with it —
  `/server`'s current-row posture is NOT touched). Zero rows after exclusion is a note, no
  overlay: launcher mode `the only launch profile is already loaded`, fallback `the server
  serves no other model`. With every offered row switchable, `pickerHint`'s `⏎ switch` is
  true by construction — the hint's honesty is this item's acceptance, not a wording change.
- `pickerLoad`'s kind, rows renderer (`launchProfileRows`) and title stay; the launcher-mode
  title reads as `/model`'s (`switch model — llama-launcher`). `loadUsage` retires with the
  verb; `modelUsage` covers both forms.

**Tests.** `picker_test.go`/`actuation_test.go` style: launcher-mode `/model` lists profiles
minus the loaded one (running-elsewhere rows stay, port-marked); accept reaches the latch;
argument form matches a profile and refuses an unknown one naming the candidates; fallback
lists advertised models minus current; both zero-row notes; offline + launcher mode still
opens (the /server posture); `/load` earns the unknown-slash refusal.

**Acceptance.** Green gate incl. `-race`. No path shows a picker whose only row is the thing
already loaded.

**Commit.** `feat(tui): /model follows the launcher — profiles are the offering, the loaded
one excluded`

## 10. `/unload` and `/stop` go hidden — recognised, never offered — ✅ DONE (2026-07-29)

NOTES (2026-07-29): one deviation from the item's literal text. The acceptance line asks that item
6's tests stay green untouched, and they did — but two MENU tests in `command_test.go`
(`TestCommandTableDrivesParserAndMenu`, `TestCommandSuggestionsTagIdleOnlyRowsWhileBusy`) asserted
"every row of `commandSpecs` is offered" and had to be narrowed to "every non-hidden row", which is
this item's own change read from the other side: a menu assertion that ignored the new flag would
contradict the item's first test. No verb test was touched.

**What.** The owner keeps the verbs' logic but takes them off the menu. `commandSpec`
(command.go:66-72) gains a `hidden` flag beside `menuOnly` — the inverse posture: `menuOnly`
is offered-never-parsed, `hidden` is parsed-never-offered. `commandSuggestions`
(autocomplete.go:264-...) skips hidden rows; `matchCommand` and dispatch are untouched, so
the typed forms still act exactly as item 6 shipped them (latch, wording, folds). `unload`
and `stop` are marked hidden. One consequence stated in the table comment: a hidden verb
still shadows a same-named skill (shadowing follows the PARSER, command.go:117-119, which
still recognises it) — the price of keeping the verb typed-reachable, and `stop`/`unload`
are names a skill should not take anyway.

**Tests.** Dropdown suggestions omit both verbs (bare `/` and prefix forms); typed `/unload`
and `/stop` still dispatch through the latch against the fake; the unknown-slash refusal does
NOT claim them.

**Acceptance.** Green gate; item 6's tests stay green untouched — hiding changed the menu,
not the verbs.

**Commit.** `feat(tui): /unload and /stop go hidden — typed they act, listed they are not`

## 11. The dropdown reads alphabetically

**What.** `commandSpecs` (command.go:95-110) reorders alphabetically — the literal itself,
not a render-time sort: the table is THE registry and display order is one of the things it
declares (its own comment says so, command.go:92-94). That comment's one ordering dependency
— `/skill` before `/skills`, because the dropdown prefix-matches in table order — is
PRESERVED by alphabetical order (a strict prefix sorts first), so the comment is updated to
say the order is alphabetical and that the dependency holds by construction. A test asserts
sortedness, so a future verb added out of place fails loudly instead of quietly un-sorting
the menu.

**Tests.** The sortedness assertion; the existing `/skill`-completes-to-the-picker behavior
re-asserted over the new order.

**Acceptance.** Green gate; bare `/` lists every visible verb alphabetically.

**Commit.** `feat(tui): the command dropdown reads alphabetically`

## 12. Docs pass — the revised surface

**What.** Every claim items 9–11 made stale, plus the design record. README: the
slash-command table loses `/load`, rewords `/model` (the launcher offering when configured,
the advertised one otherwise), and moves `/unload`/`/stop` to a short "typed-only" note —
documented, since they still act, but marked as not listed; the "Local servers via
llama-launcher" section rewords `/load` → `/model`. `cmd/apogee/defaults/config.yaml`: the
`llama-launcher:` comment block's verb sentence (item 1 wrote it around `/load`) rewords the
same way. `layout.md`: the mini-language section's takesArgs count (three now: `/confine
/model /server`) and the picker-overlay verb list (`/model /server` — `/load` gone); the
footer section's `loading <profile>…` state stays, it was never verb-named. ADR 0029: an
**amendment note under D3**, dated 2026-07-29, recording the owner's decision (the
three-verb surface folded into `/model`; `/unload`/`/stop` hidden; source: first live run)
— the ADR stays the design authority by saying what changed rather than being contradicted.
CONTEXT.md: verify the **Launch profile** entry names no verb, or reword it. CHANGELOG +
VERSION: minor bump (command-surface change — the item 7(c) posture), `v0.10.0` → `v0.11.0`.

**Tests.** None (docs); `make check` still gates.

**Acceptance.** No doc claims a capability or a verb the code lacks; `grep -r "/load"` over
README, layout.md, CONTEXT.md and the template finds only the ADR amendment's history and
this plan.

**Commit.** `docs: the launcher surface folds into /model — README, template, layout, ADR
0029 amendment`
