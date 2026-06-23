# Apogee — Phase-0 Detail Plan (P0.3)

**Date:** 2026-06-23 · **Status:** 🟢 active — the task-level breakdown of Phase 0.
**Parent:** [`implementation-plan-apogee-merge.md`](./implementation-plan-apogee-merge.md) §4
(Phase 0 is intentionally coarse there). **Backlog item:** TDD
[§8 P0.3](../design/technical-design.md). **Standing Requirements** (plan §, "⚠️ Standing
requirements") apply to every task below — chiefly **`/coding-standards` is mandatory for all
new Go**, and **the module graph stays lean** (§3a).

This document refines TDD §8's coarse **P0.3** ("write the Phase-0 detail plan … then the
capstone harness") into a numbered, acceptance-tested task list, fixes the **dependency
version pins**, and specifies **CI**. It is authoritative for the *order and acceptance* of
the remaining Phase-0 work; the TDD §8 backlog points here.

> **Why a detail plan now.** The broad plan's Phase 0 mixes a settled architectural
> checkpoint (P0.1/P0.2 — the keystone compiles) with work that needs concrete decisions
> (which dependency versions, what CI gates, what "the capstone harness is exercised by a
> test" actually means against `panic`-stub bodies). This doc makes those calls so the
> implementation tasks are mechanical.

---

## 0. Where Phase 0 stands

| Backlog | Deliverable | State |
|---|---|---|
| P0.1 | `apogee.go` public API facade + hook mutation surface | ✅ committed `e401daa` |
| P0.2 | `go.mod` (no deps) + `cmd/apogee` `--help` stub + empty `internal/` skeleton | ✅ committed `79c77a0` |
| — | `apogee --help` runs; tree `gofmt`/`go vet`/`go build`/`go vet -race` clean | ✅ verified |
| **P0.3** | **this detail plan** (pins, CI, acceptance) | **this doc** |

Everything past P0.2 is **`panic`-stub bodies** — nothing executes a loop yet. The remaining
Phase-0 deliverables from plan §4 are: CI/build; the `platform/` seam (shell + path + the
already-public `Confiner`, with stub backends); cancellation as a Phase-0 API primitive
(ADR 0007); and the **capstone harness** — the first slice that needs *real* bodies.

---

## 1. Dependency version pins (decided 2026-06-23)

Pins resolved against the Go module proxy on 2026-06-23. Per plan §3a, **a pin is a decision,
not a `go get`**: the version is fixed here; the dependency is actually added by the *task
that first needs it*, so the module graph never carries a dep ahead of its use. **Phase 0 —
including the capstone harness — adds zero modules** (stdlib + `net/http/httptest` only).

| Module | Pinned version | Line | First needed | Notes |
|---|---|---|---|---|
| `github.com/spf13/cobra` | **v1.10.2** | stable v1 | Phase 1 (first subcommand) | CLI tree; mature, ubiquitous. The P0.2 `cmd/apogee` hand-rolls `--help` deliberately to defer this. |
| `github.com/charmbracelet/bubbletea/v2` | **v2.0.7** | **v2** | Phase 2 (TUI) | See §1.1 — v2 chosen for a greenfield TUI; v1 `v1.3.10` is the fallback. |
| `github.com/charmbracelet/lipgloss/v2` | **v2.0.4** | **v2** | Phase 2 | Matches the Bubble Tea v2 line. |
| `github.com/charmbracelet/bubbles/v2` | **v2.1.0** | **v2** | Phase 2 | Matches the Bubble Tea v2 line. |
| `github.com/modelcontextprotocol/go-sdk` | **v1.6.1** | **v1 GA** | Phase 3 (MCP client) | See §1.2 — **maturity re-verified: now post-v1.0.0 GA.** |
| `gopkg.in/yaml.v3` | **v3.0.1** | — | Phase 1 (config loading) | Already in apogee-sim's `go.sum`; same pin. |
| `github.com/google/shlex` | **v0.0.0-20191202100458-e7afc7fbc510** | pseudo | Phase 3 (`terminal` tool) | Same pseudo-version apogee-sim pins; command-line splitting. |
| `github.com/oklog/ulid/v2` | **v2.1.1** | — | Phase 1 (session/turn IDs) | Same pin apogee-sim carries. |

Stdlib stays the default everywhere else (plan §3a): `net/http` (provider, web tools),
`encoding/json`, `os/exec`, `io/fs`, `regexp`. Don't add a library where ~50 lines of stdlib
will do. Each addition above is re-justified in its phase's notes when the `go get` lands.

### 1.1 Charm stack: pin the **v2** line

The Charm TUI stack shipped a stable **v2** major (Bubble Tea `v2.0.7`, Lipgloss `v2.0.4`,
Bubbles `v2.1.0`) in addition to the v1 line (`v1.3.10` / `v1.1.0` / `v1.0.0`). **Decision:
target v2.** Rationale: the TUI is greenfield (no code exists before Phase 2, so there is no
migration cost), v2 is the maintained-forward line, and pinning v1 now would mean a major
upgrade mid-project. **Risk + fallback:** if a needed Bubbles widget or a community component
lags the v2 API at Phase 2, fall back to the v1 trio (`bubbletea v1.3.10` + `lipgloss v1.1.0`
+ `bubbles v1.0.0`) — they are API-stable and battle-tested. This pin is **revisited at the
Phase-2 entry**, not load-bearing until then; recorded now only so the choice is deliberate.

### 1.2 MCP `go-sdk` maturity — re-verified (closes a plan §5 / §6 risk)

The plan flagged MCP SDK maturity twice — risk table §5 ("Go SDK is new and moves fast …
mark3labs fallback if needed") and Phase 3 ("re-verify SDK maturity at this point"). **Re-
verified 2026-06-23: `github.com/modelcontextprotocol/go-sdk` is at `v1.6.1`** — it crossed
`v1.0.0` GA and has shipped six stable minors since, on semver. The "new and moves fast,
might need a fallback" risk is **substantially retired**: stay on the *official* SDK, and
drop `mark3labs/mcp-go` from the active fallback set. Phase 3 still **re-confirms at entry**
(per the plan), but the default is now "official SDK, pinned `v1.6.x`," not "evaluate two."

---

## 2. CI (Phase-0 deliverable — `.github/workflows/ci.yml`)

Closes the TDD §7 "No CI" gap. CI gates **formatting and vetting now**, and **tests as they
land** (handoff directive — there are no tests until the capstone harness, P0.6). Two jobs:

**Job `check`** (ubuntu-latest, Go `1.26.x` via `actions/setup-go`):
1. **`gofmt`** — `test -z "$(gofmt -l .)"`; fail (and print the offending files) if non-empty.
2. **`go vet ./...`**
3. **`go build ./...`**
4. **`go test -race ./...`** — passes trivially today (no `_test.go` files ⇒ exit 0); becomes
   the real gate when P0.6's harness test lands. `-race` is on from day one so the harness's
   concurrency (loop goroutine + `EventSink` observer, `apogee.go:49`) is checked the moment
   it exists.

**Job `cross`** (matrix `GOOS ∈ {linux, darwin, windows} × GOARCH ∈ {amd64, arm64}`,
`CGO_ENABLED=0`): `go build ./...` **and** `go build ./cmd/apogee` for each target — proves
the single static binary cross-compiles to all v1 platforms (Windows is fast-follow but must
keep compiling — plan §6 #3). The race detector is **not** run on cross targets (it is
host-only); `check` covers it on linux/amd64.

Triggers: `push` and `pull_request` (the project commits to `main` directly while
pre-production, but PR triggers cost nothing and arm the gate if that policy changes —
handoff "Branching policy"). No module cache config beyond `setup-go`'s built-in; no external
services; **no new Go deps** (CI exercises the current dep-free tree).

**Acceptance:** the workflow is green on the current tree (6 cross targets build; `check`
passes); a deliberately mis-`gofmt`'d file makes `check` red (spot-verified locally with
`gofmt -l`, not committed).

---

## 3. Phase-0 task list (remaining work, ordered)

IDs continue the backlog's `P0.x` scheme. Dependencies are noted; **P0.4 and P0.5 are
independent** and can land in either order; **P0.6 depends on P0.5** (the Auto-gate check in
`New` needs a `Confiner` backend to test against).

| ID | Task | Depends | New deps |
|---|---|---|---|
| **P0.4** | CI workflow (§2) | — | none |
| **P0.5** | `platform/` seam: shell + path interfaces (POSIX impl, Windows stub) + stub `Confiner` backend | — | none |
| **P0.6** | **Capstone harness** — minimal real bodies for construct→`Step`→`Snapshot`→`Resume`→`AddExperimental`, exercised by a hermetic test | P0.5 | none |

Housekeeping carried from the handoff (decided, not yet propagated — **not Phase-0-blocking**,
tracked here so it isn't lost): ratify the five §4.1 TDD sketch-decisions into the plan/ADRs
(esp. **public `Confiner`** → plan §3 + ADR 0004); fix `README.md:68` (bench "headless"
wording contradicts ADR 0001). Best done as their own focused doc change — see §6.

### P0.4 — CI

Per §2. Single file `.github/workflows/ci.yml`. No code change to the tree.

**Acceptance:** §2's acceptance. Locally reproducible:
`gofmt -l . ; go vet ./... ; go build ./... ; go test -race ./...` all clean, and
`GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...` (and the other five targets) succeed.

### P0.5 — `platform/` seam

Define `internal/platform` (today only a `doc.go`). The **public** `Confiner` interface and
its caps/box types already live in `apogee.go` (lines ~762–789) — this task adds the
*internal* seam and a **stub backend**, not the public interface.

- **`Shell` + `Path` interfaces** (POSIX implementation; Windows stub behind
  `//go:build windows`). Minimal surface — just enough to compile and to give Phase 1/3 a
  single seam to implement, per plan §3 ("Windows is one interface to implement, not a
  call-site audit"). Do **not** design the full shell abstraction here; stub the methods the
  Phase-0 harness needs (none yet) and leave `// TODO(phase-3)` for the rest.
- **Stub `Confiner` backend** — a `denyConfiner` (or `nopConfiner`) implementing
  `apogee.Confiner` whose `Capabilities()` reports `ConfinementCaps{FSWrite: false,
  NetworkEgress: false}` (so `AutoEligible()` is false) and whose `Confine` runs `fn`
  unchanged. This is the stub the plan §4 calls for ("stub backends; real ones land Phase
  3"). It is what lets `New` *test* the Auto gate (ADR 0004) in P0.6 without a real seatbelt/
  landlock backend.

**Acceptance:** `internal/platform` builds on the full cross matrix (linux + windows + darwin,
amd64 + arm64); `denyConfiner` satisfies `apogee.Confiner` (compile-time
`var _ apogee.Confiner = (*denyConfiner)(nil)`); a table test asserts
`denyConfiner{}.Capabilities().AutoEligible() == false`.

### P0.6 — the capstone harness (the first real bodies)

Plan §4 Phase-0 deliverable: *"a throwaway in-process harness that constructs an `Agent`,
`Step`s it, snapshots + resumes, and registers an experimental hook — proving the bench's
access pattern works before real subsystems exist,"* and *"the in-process step/snapshot/hook
pattern is exercised by a test."* This **cannot run against `panic` stubs** — it forces the
first *minimal real* implementations behind the keystone. It is explicitly **throwaway/
minimal**: a deliberately thin slice (non-streaming, single Turn, no real tools required)
that validates the **API seam**, not the production loop. The real provider + loop port
(Phase 1) supersedes the internals; the *test* and the *seam* it proves carry forward.

#### Scope discipline — what is and isn't built

| In scope (minimal real) | Out of scope (stays stub / Phase 1+) |
|---|---|
| `New(Config)` validates: Auto-gate (ADR 0004) + ordering cycle (ADR 0003) | streaming; real OpenAI-compatible HTTP client |
| One conversation-state value + JSON `Snapshot`/`Resume` (versioned) | concrete Session schema/migration (TDD §5 Sessions row) |
| One **Turn**: `Submit`→`Step`→build `Request`→ask Upstream→ `MessageEvent`→ quiescent boundary | real tool dispatch / approval / Mechanisms catalogue |
| `ctx` cancellation → `StatusCancelled`, resumable state (ADR 0007 / plan §6 #24a) | tool-call parsing for all formats (Phase 3 `processing/`) |
| Recover-at-extension-boundary: a panicking hook → `ErrorEvent`, clean boundary (plan §6 #24c) | sub-agents, modes, confinement backends |
| `MechanismRegistry.AddExperimental` → a pre-request hook fires → `MechanismFiredEvent` | descriptor-carrying catalogued Mechanisms, topo-order beyond cycle-check |

#### The hermetic-Upstream seam (the one design call P0.6 must make)

`Step` inherently needs an Upstream response, but the **real provider is Phase 1**. The public
`Config` exposes only `Endpoint`/`Model` — no provider-injection seam, and none should be
*added to the public API* for a test. Resolution: the minimal internal loop is built over an
**unexported `internal/agent` `upstream` seam** —

```go
// internal/agent (unexported): the loop depends on this, not on net/http.
type responder interface {
    respond(ctx context.Context, req *request) (rawResponse, error)
}
```

— and P0.6 implements a **deterministic fake responder** (returns a canned no-tool assistant
message, or blocks on `ctx` for the cancellation test, or is driven to exercise a hook). The
**real HTTP provider implementing the same internal seam lands in Phase 1** (and an
`httptest.Server` upgrade of the harness can then exercise the wire path). This keeps Phase 0
**dep-free and hermetic**, keeps the public surface untouched, and leaves the provider as an
internally-injectable seam — which is the correct architecture anyway. (Tests live in the
same module and may reach `internal/`, so no public seam is needed.)

#### Sub-tasks

- **P0.6a — `New`/`Config` validation.** Real `New`: reject `Mode==Auto` with a `Confiner`
  that fails `Capabilities().AutoEligible()` → `ErrAutoUnavailable`; detect an ordering cycle
  in the (Phase-0-trivial) registry → `ErrOrderingCycle`; require `Events`. Wire the
  `denyConfiner` (P0.5) in the test.
- **P0.6b — conversation state + Session.** A minimal copyable conversation value; `Snapshot`
  serializes it to `Session{Version, State}`; `Resume`/`DecodeSession` round-trip it and
  reject an unknown future `Version` → `ErrSessionVersion`.
- **P0.6c — the minimal Turn.** `Submit` enqueues `UserInput` (mid-Exchange `Submit` →
  `ErrInputPending`); `Step` runs pre-request hooks, calls the `responder`, emits
  `MessageEvent`, returns `StatusExchangeComplete` at the quiescent boundary. Honor `ctx`:
  cancel mid-`respond` → `StatusCancelled` + serializable state. Recover a panic in a
  hook/tool at the extension boundary → `ErrorEvent`, degrade to boundary, no host unwind.
- **P0.6d — experimental hook.** `MechanismRegistry.AddExperimental(HookPreRequest, hook)`
  registers a bare `PreRequestHook`; the loop runs it during `Step` and emits a
  `MechanismFiredEvent`. (No descriptor ⇒ no self-regulation — ADR 0002.)
- **P0.6e — the harness test.** `apogee_test.go` (black-box `apogee_test` package where the
  public path suffices; white-box for the internal fake responder). Asserts the full path:
  construct → `Submit` → `Step` (observe `MechanismFiredEvent` + `MessageEvent` +
  `StatusExchangeComplete`) → `Snapshot` → `Resume` → `Step` again. Plus a **cancellation**
  test (`StatusCancelled`, snapshot still valid) and a **panic-recovery** test (`ErrorEvent`,
  loop survives). Hermetic — the fake responder, **no network, no new deps.**

**Acceptance (P0.6):**
- `go test -race ./...` passes; the harness test drives construct→`Step`→`Snapshot`→`Resume`→
  `AddExperimental` end-to-end against the fake responder with **zero new module deps**.
- Cancelling `ctx` mid-`Step` yields `StatusCancelled` and a `Snapshot` that `Resume`s and
  continues (no half-streamed state — ADR 0007).
- A panicking experimental hook yields an `ErrorEvent` and the loop reaches a clean quiescent
  boundary (the host is never unwound — plan §6 #24c / ADR 0002).
- `New` returns `ErrAutoUnavailable` for Auto + `denyConfiner`, and `ErrOrderingCycle` for a
  cyclic registry.
- Tree stays `gofmt`/`go vet` clean; CI (P0.4) green including the new test.

---

## 4. Phase-0 "done" definition

Phase 0 is complete (plan §4 "Deliverable") when **all** hold:

1. Repo builds; `apogee --help` runs. *(done — P0.2)*
2. Public API + `platform/` seams exist (Confiner public; backends stubbed). *(P0.5)*
3. The in-process **step/snapshot/hook pattern is exercised by a passing test** — the capstone
   harness. *(P0.6)*
4. CI gates `gofmt`/`vet`/`build`/`test -race` and cross-compiles Win/Mac/Linux. *(P0.4)*
5. Dependency versions are pinned-by-decision (this doc §1); the module graph is still empty.

On completion, Phase 1 (the embeddable agent core — real provider, loop, minimal tools,
sessions, `apogee-sim` pointed at the Go API) can begin; the capstone's internal seams
(`responder`, conversation state) are the thin precursors Phase 1 replaces with real bodies.

---

## 5. Acceptance-criteria summary (quick gate)

A reviewer can check Phase 0 with:

```
gofmt -l .                          # empty
go vet ./...                        # clean
go build ./...                      # ok
go test -race ./...                 # harness test passes
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...   # + the other 5 cross targets
./apogee --help                     # prints usage, exit 0
```

…plus the four behavioral assertions in P0.6's acceptance (cancellation, panic-recovery,
Auto-gate, ordering-cycle).

---

## 6. Out of scope for Phase 0 (explicit non-goals — keep the slice thin)

Deferred deliberately so the keystone stays a *seam*, not a half-built engine:

- **Real provider / streaming** — Phase 1 (the `responder` seam stays fake in P0.6).
- **Mechanism catalogue, descriptors, full topo-order** — only cycle-detection is built in
  P0.6; the deterministic total order beyond that is Phase 4's registry work (ADR 0003). The
  catalogue→hook mapping stays a Phase-4 prerequisite session driven by sim traces.
- **Real Confiner backends** (seatbelt/landlock/AppContainer) — Phase 3 + the dedicated
  Confinement design session (ADR 0004). P0.5 ships only the deny-all stub.
- **`mechanisms/` package-per-hook layout** — stays flat and provisional (TDD §6.4); resolved
  by the Phase-4 catalogue-mapping session.
- **Doc-propagation housekeeping** (ratify §4.1 public `Confiner` into plan §3 + ADR 0004; fix
  `README.md:68`) — decided, low-risk, but a separate focused change so it doesn't entangle
  scaffold commits. Track, don't fold.

---

## 7. Suggested skills

- **`/coding-standards`** — **mandatory** for P0.5 and P0.6 (load `coding-standards.go.md` +
  `testing.go.md`); the harness writes real Go and tests (table-driven + the §testing
  conventions). The plan doc (P0.3) and CI yaml (P0.4) are not Go, but everything they
  schedule is.
- **`run` / `verify`** — once P0.6 lands, to drive the harness/`apogee --help` and confirm.
- **`pr-lifecycle`** — only if the branch policy reverts from "commit to `main`" (handoff).
