# Plan: test drivers kit — stubllm, TUI drivers and an LLM judge, so "manual" stops being a test mode

**Goal:** give apogee a permanent, general-purpose automated-testing kit — a scripted
OpenAI-compatible upstream (`internal/stubllm`), an in-process TUI driver over the real
composition and a black-box PTY driver that both reconstruct frames through one terminal emulator
(`internal/tuitest`), and an env-gated, binding LLM judge for the judgment halves
(`internal/judge`) — and then use it to convert every manual item of
`docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md` into a test that runs under `go test`.
The checklist is the kit's first consumer, not its scope: the kit is written and documented so the
next feature gets a driver test instead of a numbered human step, and the `test-checklist` skill
is taught to refuse a manual step where the kit can observe the claim.

**Date:** 2026-08-27 · **Status:** unexecuted · **Sized for:** ~200k-context host

**Authoritative sources:**

- `docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md` — the 19 manual items (T-03, T-04,
  T-06, T-07, T-10–T-16, T-18–T-25), their steps, "Pass when / Fails if" oracles, and the
  "Suggested automated tests" section this plan executes.
- ADR 0031 (the engine stays sufficient for any Driver; wire-silent engine; benchable-all-the-way-up)
  — the drivers built here are Drivers in that sense: they enter through the composition seam and
  add no test-only hooks to the engine. ADR 0011 (value-copied `Model`, no no-copy types by value).
  ADR 0010 (`internal/` never imports the root module path). ADR 0037 (every settings edit applies
  live). ADR 0059 (Console family, default-off). ADR 0033 (scheduler; `internal/schedule.Clock`).
- `cmd/apogee/root.go:16-30` — `type launcher func(ctx, eng tui.Engine, br *tui.Bridge, opts
  tui.Options) error` and `newRootCommand(launch launcher, …)`; `runRoot(ctx, opts, launch)` is
  the composition entry the existing `wire_test.go` already drives with a recording launcher.
- `internal/tui/tui.go:1511-1570` — `Run` builds the Model (`newModel`), wires
  `m.flushEvents = br.sink.flush`, the `--tui-diag`/`--tui-trace` seams (`opts.DiagPath`,
  `opts.TracePath`, `programOptions`, `tracedOutput`), then `tea.NewProgram(m, teaOpts...)`,
  `br.Bind(program)`, `program.Run()`. `internal/tui/bridge.go:20` — `programSender` is just
  `Send(tea.Msg)`.
- `charm.land/bubbletea/v2 v2.0.8` options (`options.go`): `WithInput`, `WithOutput`,
  `WithEnvironment`, `WithoutSignalHandler`, `WithoutSignals`, `WithoutRenderer`, `WithFilter`,
  `WithColorProfile`, `WithWindowSize`, `WithContext`. `Program.Run()` returns the final Model.
- `github.com/charmbracelet/x/vt` (pseudo-version `v0.0.0-20260823001701-96af6d2cb5f6`, pins the
  repo's exact `x/ansi v0.11.7`): `NewEmulator(w,h)`, `Write`, `String()` (plain), `Render()` (with
  SGR), `CellAt(x,y) *uv.Cell`, `Resize`, `Read` (the terminal's answers — it answers DA1, DECRQM
  incl. mode 2027, DSR/CPR itself: `handlers.go:688-826`, `csi.go:30`), `SetCallbacks`.
  `teatest/v2` was evaluated and rejected (pre-release pinned to bubbletea `v2.0.0-rc.1`).
- `internal/provider` — `NewClient(baseURL, model, opts...)`, SSE shape in
  `stream_test.go:37` (`data: {"choices":[{"delta":{…}}]}`, `reasoning_content`, `tool_calls`,
  `[DONE]`), `usage.prompt_tokens_details.cached_tokens` parsing (`stream_test.go`, both wire
  paths), `Responder` interface (`responder.go:21`).
- `cmd/apogee/testsupport_test.go:83` `upstreamHome(t, endpoint, modelHint…)` (seeded temp home);
  `cmd/apogee/headless_test.go` (`stubRunner`, `TestHeadlessExitCodes`);
  `internal/mcp/transport_test.go:263` (an in-test Go forward proxy), `:334` (an httptest MCP
  server); `internal/present/opener.go:93` `Opener.LookPath func(string) (string, error)` (the
  opener's program-resolution seam); `internal/console/process.go:152`
  (`pty.StartWithAttrs`, `github.com/creack/pty v1.1.24` already a dependency);
  `internal/schedule/schedule.go:22` `MinCycle = 30s`, `clock.go:9` `Clock` interface.
- `Makefile` (`test`, `live-eval` with `LIVE_ENDPOINT ?= http://127.0.0.1:1111`, `check`),
  `.github/workflows/ci.yml` (one `ubuntu-latest` check job + cross matrix, SHA-pinned actions).
- `docs/plans/2026-08-27 - 01 - skill-suggestions-band-plan.md` — takes ADR 0061; this plan takes
  0062.

**Ratified design calls (owner, 2026-08-27, via AskUserQuestion during /grill-me):**

1. **Scope:** the kit is general apogee test infrastructure for all future changes, not a
   one-off for this checklist; the goal is 100 % automation of what previously needed a human,
   integrated into the existing `go test` suite. The LLM judge is in scope.
2. **Judge transport:** an OpenAI-compatible endpoint through the repo's own `internal/provider`
   client, gated by `APOGEE_JUDGE_ENDPOINT` / `APOGEE_JUDGE_MODEL` (defaulting to
   `APOGEE_LIVE_ENDPOINT` / `APOGEE_LIVE_MODEL`); skips when unset, exactly like the live tests.
   No `claude -p`, no CLI on PATH.
3. **Driver level:** the in-process driver enters through the `launcher` seam of `runRoot` and
   drives the REAL composition (Agent, tools, session store, Bridge, filewatch) — not
   `tui.Model` + `fakeEngine`.
4. **Harness library:** our own thin pump over `tea.NewProgram` (`WithInput`/`WithOutput`/
   `WithWindowSize`/`WithoutSignals`/`WithoutSignalHandler`/`WithColorProfile`); `teatest/v2`
   rejected.
5. **Frames:** `github.com/charmbracelet/x/vt` is the frame authority for BOTH drivers — one
   `Frame` type (plain text, cell styles, cursor) reconstructed from the renderer's bytes.
6. **stubllm form:** a library package `internal/stubllm` plus a thin `cmd/stubllm` binary.
7. **stubllm script:** ordered `turns:`, optional `when:` matcher (regex over the last message's
   text, or `tool_result: <name>`), `repeat: true`, strict — an unmatched request is HTTP 500 and
   recorded. Turn kinds: text (per-token delay, chunk size), reasoning, tool_call(s), usage
   (prompt/completion/cached_tokens), http (status/body/redirect), hang(ms). YAML tags on the Go
   structs so binary and tests share one format.
8. **Recorder:** `cmd/stubllm record --upstream URL --out fixture.yaml` proxies `/v1/*` and
   writes a Script with timings and pre-filled `when:` matchers. Recording is an explicit CLI
   act, never a `go test` flag.
9. **Proxies accepted as the automated definition** of the irreducible halves: T-20 glyph → the
   emulator's cell width is the authority; T-24 flicker → bytes written and full-frame repaints
   per streamed token, from the `--tui-trace` seam, pinned against a ceiling; T-19 desktop → a
   logging fake opener through `Opener.LookPath`; T-21 `brew upgrade` → a post-release
   `make release-smoke`; the tag job → actionlint + pin grep; T-11 → an `ubuntu-22.04` CI job;
   T-22 → stays env-gated under `make live-eval`; T-23 newcomer → a judge-driven agent in a clean
   container following README only, against stubllm.
10. **Judge contract:** `internal/judge` — rubric beside the test (copied from the checklist's
    own "Pass when / Fails if"), strict-JSON verdict `{verdict: pass|fail, reasons: []}`,
    temperature 0, one vote, BINDING (a fail fails the Go test, reasons printed) whenever the gate
    is set. Inputs are any named text artifacts (frame, stdout, file, transcript); frames are one
    kind. `judge.Pairwise(before, after)` for "no worse than the last release" claims.
11. **Placement and gating:** `internal/stubllm`, `internal/tuitest`, `internal/judge`; e2e tests
    as `cmd/apogee/e2e_<topic>_test.go` (they need the launcher seam). In-process and PTY e2e
    run in every plain `go test` (PTY skips on Windows / no pty; the whole e2e set stays ≤ ~15 s
    under `-race`); judge tests skip without the gate and join `make live-eval`. No build tags, no
    `-short`.
12. **T-23 host:** local only — `TestNewcomerFollowsTheDocs` skips unless `docker` is on PATH AND
    the judge gate is set; the container gets `--network host`; no secrets in CI.
13. **Goldens:** semantic `WaitFor` assertions by default; byte-for-byte goldens only for the
    rendering surfaces (T-10 pane, T-12 hostile rows, T-13 fold, T-15 outcome slots, T-16 settings
    rows) under `cmd/apogee/testdata/frames/`, refreshed with `go test ./cmd/apogee -update`.
14. **T-03 kill:** both — in-process (program ctx cancel, rebuild on the same home) AND PTY
    (`SIGKILL` on the real pid, relaunch).
15. **PTY mechanics:** the binary is built once in `cmd/apogee` `TestMain` (PTY tests skip if the
    build fails); `TERM=xterm-256color COLORTERM=truecolor`, 100×30; settled = no bytes for
    150 ms; resize = `pty.Setsize` + `SIGWINCH`; the emulator's query answers are pumped back into
    the pty master.
16. **ADR 0062** "test drivers are Drivers": composition-seam entry, no test-only engine hooks,
    stubllm is the only scripted upstream, x/vt is the frame authority, the judge is env-gated
    and binding.
17. **Adoption:** `docs/design/test-drivers.md` (APIs, one worked example each, a "which driver
    observes which claim" table), an AGENTS.md pointer, a `test-checklist` skill gate (a step may
    be MANUAL only when the table says no driver can observe it), and a `coding-standards` Go
    override line. `implement-plan`/`feature-implementation` are NOT changed by this plan.
18. **Plan shape:** one plan, kit first (items 1–8, each shipping with its own example test and
    doc section), then the application items grouped by driver (9–15), then closeout (16).

**Review amendments (owner-approved 2026-08-27, pre-execution; each is folded into the item text
below, so the items are authoritative):**

- A1 (item 4) — `--tui-trace` wraps `os.Stdout` (`programOutput`, `internal/tui/tui.go:1619-1624`);
  with a driver's `WithOutput` winning, the trace file would be EMPTY in-process. `Build` takes the
  output writer explicitly and refuses a trace on a caller-supplied output; the in-process flicker
  proxy reads `Screen`, the PTY one reads the trace file.
- A2 (item 6) — `cmd/apogee/main_test.go:29` already has a `TestMain` (it redirects `HOME` to a
  suite temp dir); the binary build EXTENDS it, no second `TestMain`.
- A3 (item 5) — a custom merged `io.Reader` as program input makes bubbletea fall back to the
  non-cancellable reader (`cancelreader_linux.go:23-25`) → every quit waits `waitForReadLoop`'s
  500 ms and leaks a goroutine. The program input is the read end of an `os.Pipe`.
- A4 (item 10) — the TUI scheduler is built with no `Clock` (`cmd/apogee/wire_live.go:335`); the
  daemon already has the package var `daemonClock` (`daemon.go:249`). The TUI gets the same
  pattern (`tuiScheduleClock`), decided now; the `APOGEE_E2E_SLOW` fallback is gone.
- A5 (item 14) — `Opener.LookPath` is not reachable from the launcher (`presentationRungs`,
  `wire_present.go:46`, installs into the tool layer). A package var `openerLookPath` is the seam.
- A6 (item 13) — a stub `tui.LauncherHost` cannot rebind the session (that goes through
  `w.mover` inside `launcherWiring`, `wire_live.go:299`). The seam is the existing `launcherOps`
  interface (`fakeLauncher`, `launcher_test.go:19`) behind the REAL wiring, via a package var.
- A7 (item 4) — goldens need a redaction pass (temp paths, version, `Session <date>` titles,
  relative ages) or they churn on every run; `Golden` takes redactions, `launchTUI` supplies the
  default set.
- A8 (item 13) — `filewatch.New` runs at 1 s poll + 250 ms settle (`filewatch.go:37,43`) with no
  composition seam; a package var shortens it under test.
- A9 (items 4, 5) — `WaitFor` failures also log the styled render; every in-process test ends
  with a goroutine-leak check.
- A10 (item 7) — a binding local judge is not deterministic at temperature 0; the design doc
  states the re-run rule rather than softening the verdict.
- Baseline measured 2026-08-27: `go test -race -count=1 ./cmd/apogee/` = 5.5 s before this plan.

**Standing requirements:**

- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Never change `VERSION`, a CHANGELOG release heading, or a tag (see closing note).
- Per-item acceptance is targeted; `make check` runs once at closeout.
- **No test may touch the real `~/.apogee`** (checklist T-22 §9): every driver launch runs with a
  `t.TempDir()` home passed as `--config` (or `APOGEE_CONFIG`) and a `t.TempDir()` workspace; the
  kit's constructors take both and refuse an empty one.
- **Wall-clock budget:** the in-process + PTY e2e set added by this plan must finish in ≤ 15 s
  under `go test -race ./cmd/apogee/` (on top of the 5.5 s baseline, so the package stays ≈ 20 s);
  every wait is a bounded `WaitFor` (default 5 s), never a bare sleep. The e2e tests use
  `t.Setenv` and package-var seams, so they are SERIAL (no `t.Parallel`) and every swapped var is
  restored via `t.Cleanup` — the budget is serial wall clock. Judge/live/docker tests are outside
  the budget and always gated.
- ADR 0010: `internal/stubllm`, `internal/tuitest`, `internal/judge` import nothing from the root
  module path (the `make check` grep enforces it). ADR 0011: nothing added to `internal/tui`
  holds a no-copy type by value; `internal/tui` new files are narrated in its `doc.go` (docmap test).
- Every driver, stub and judge API is documented in `docs/design/test-drivers.md` in the SAME
  item that creates it — the doc is part of each kit item's acceptance, not of the closeout.
- Windows: `internal/tuitest` PTY driver is `//go:build !windows`; its tests call `t.Skip` there.
  The in-process driver has no platform gate.
- The `skills/` tree is untracked (public origin): item 8's skill edits are made in place and
  never `git add`ed; only the repo files of that item are committed.

**Out of scope:**

- Replacing or restructuring the existing unit tests; the kit is additive.
- Any change to the engine, provider or tool packages beyond the seams named in items 4, 10, 13
  and 14 (the `tui.Run`/`Build` split; the `cmd/apogee` package vars `tuiScheduleClock`,
  `liveLauncherOps`, `configWatchTiming`, `openerLookPath` — all test-swapped, all defaulting to
  production, the pattern `daemonClock`/`watchSchedules`/`daemonUserHome` already set).
- A hosted judge key in CI, or any CI job that needs a model.
- Teaching `implement-plan` / `feature-implementation` to require driver tests (a later,
  untracked skills plan).
- Bench arms, `apogee-sim`, and the daemon's own manual (the daemon half of T-07 is covered by
  an integration test here, nothing more).
- Font rendering, real desktop applications, a real Homebrew install before a release exists —
  covered by the ratified proxies (call 9), recorded as such in the design doc's table.

---

## 1. ADR 0062, design-doc skeleton, AGENTS.md pointer — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the ADR's cross-links to ADR 0010/0011 use their real filenames
(`0010-package-layout-domain-core-and-thin-root-facade.md`,
`0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md`), not the descriptive slugs the
plan's prose implies; all links verified to resolve.
NOTES (2026-08-27): the claim table's example-test names for tests that do not exist yet are
marked "(planned, item N)" rather than stated bare, so the skeleton makes no false claim before
plan item 16 re-checks them against `go test -list`.

**What:** docs only.

- Write `docs/adr/0062-test-drivers-are-drivers.md` in the house ADR shape (`Status: accepted`
  frontmatter, `# <title>`, `## Context`, `## Decision`, `## Consequences`). Context: 19 of the
  release checklist's items were "manual" because the TUI had no driver, every upstream in tests
  was an ad-hoc `httptest` closure, and judgment halves (wording, "reads as one row") had no
  observer; ADR 0031 already says the engine must be sufficient for any Driver. Decision: (1) test
  drivers enter through the composition seam (`runRoot`'s launcher) and a `tui.Run` split; no
  test-only hooks are added to the engine, the provider or the tools; (2) `internal/stubllm` is
  the ONE scripted upstream — new tests script it rather than writing a handler; (3)
  `github.com/charmbracelet/x/vt` is the frame authority for both the in-process and the PTY
  driver, so a claim about the screen is asserted on cells, never on `View()` strings or raw
  bytes; (4) the LLM judge is a first-class oracle for judgment halves: env-gated
  (`APOGEE_JUDGE_ENDPOINT`), OpenAI-compatible through `internal/provider`, strict-JSON,
  binding when set; (5) the proxies of ratified call 9 are the automated definition of the
  irreducible halves. Consequences: a "manual" step is legitimate only where the design doc's
  table says no driver observes the claim; `teatest`/`vhs`/`tmux` are not to be re-proposed
  (state why each was rejected in one line each); the kit is maintained like production code
  (documented, tested, `make check`).
- Create `docs/design/test-drivers.md` with the final section skeleton every later item fills:
  `# Test drivers`, `## Why`, `## stubllm` (script format, matching, turn kinds, recorder),
  `## tuitest` (`Frame`, in-process `Driver`, `PTYDriver`, goldens), `## judge` (gate, rubric,
  artifacts, pairwise), `## Which driver observes which claim` (a table with columns Claim class ·
  Driver · Example test · Not observable by any driver — seeded now with the rows of ratified
  call 9), `## Writing a new e2e test` (checklist), `## Gates and budgets`. Sections not yet
  implemented carry one line "filled by plan item N".
- AGENTS.md "Where knowledge lives": add one bullet after `docs/design/` naming
  `docs/design/test-drivers.md` — "how to drive the TUI, script an upstream and judge a frame in
  `go test`; a test step is manual only where its table says so."

**Files:** `docs/adr/0062-test-drivers-are-drivers.md`, `docs/design/test-drivers.md`, `AGENTS.md`

**Tests:** none (docs). `grep -n "test-drivers" AGENTS.md` finds the pointer.

**Acceptance:** `test -f docs/adr/0062-test-drivers-are-drivers.md && grep -q "test-drivers.md"
AGENTS.md && grep -q "Which driver observes which claim" docs/design/test-drivers.md`

**Commit:** `docs(adr): 0062 — test drivers are Drivers; design-doc skeleton and AGENTS pointer`

## 2. `internal/stubllm` — Script, matching, SSE server, request log — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item's Files line names five source files; the literal OpenAI
request/reply JSON structs live in a sixth, `internal/stubllm/wire.go`, so server.go stays about
transport and framing — the same split `internal/provider` makes between wirejson.go and
client.go. The file is named in doc.go's file map.
NOTES (2026-08-27): tests (h) YAML round-trip and (i) validation live in `script_test.go`, beside
the code they exercise, rather than in `server_test.go` as the item's Tests paragraph lists them;
`server_test.go` carries (a)–(g) plus the options, Serve and request-log cases.
NOTES (2026-08-27): no CHANGELOG entry from this item — plan item 8 owns the single
`[Unreleased]` entry covering the whole kit (items 2–7).
NOTES (2026-08-27): `go.mod`/`go.sum` are untouched; `gopkg.in/yaml.v3` is already a direct
dependency, and it decodes/encodes `time.Duration` as a Go duration string ("1ms") natively, so
`TokenDelay`/`Hang` keep the field types the item specifies with no custom marshaller.
NOTES (2026-08-27): the design doc's `### Recording a fixture` subsection is left as a
"filled by plan item 3" marker, matching the skeleton's convention — the recorder is item 3's.

**What:** the scripted OpenAI-compatible upstream as a library.

- `internal/stubllm/script.go`: `type Script struct { Model string; Turns []Turn }`;
  `type Turn struct { When *Match; Repeat bool; Text string; TokenDelay time.Duration; ChunkRunes
  int; Reasoning string; ToolCalls []ToolCall; Usage *Usage; HTTP *HTTPReply; Hang time.Duration;
  FinishReason string }`; `type Match struct { LastMessage string /* regexp */; ToolResult string
  /* tool name */ }`; `type Usage struct { Prompt, Completion, Cached int }`; `type HTTPReply
  struct { Status int; Body string; Location string; ContentType string }`. All fields carry
  `yaml:"…"` tags; `Load(path)`/`Parse([]byte)` validate: a turn is exactly one of text /
  tool_calls / http / hang (reasoning and usage may accompany text or tool_calls); `Status` must
  be set with `HTTP`; a `When` regexp must compile. `FinishReason` defaults to `stop`, or
  `tool_calls` when tool calls are present. A `Text: ""` turn with no tool calls is the
  **empty-reply** turn (T-07's abandoned final Turn) and is legal.
- `internal/stubllm/server.go`: `func New(t testing.TB, s Script, opts ...Option) *Server`
  (httptest-backed, closed on `t.Cleanup`) and `func Serve(ctx, addr string, s Script, opts
  ...Option) (*Server, error)` (the binary's entry). Routes: `GET /v1/models` → `{"data":[{"id":
  <Model>}]}`; `POST /v1/chat/completions` → match, then stream (`stream:true` — SSE chunks in the
  provider's shape: `content` deltas split by `ChunkRunes` (default 4) with
  `TokenDelay` between them, `reasoning_content` first when set, `tool_calls` deltas with
  `id`/`function.name`/`arguments` split in two chunks like `stream_test.go:43-45`, a final
  `usage` object when `Usage` is set (with `prompt_tokens_details.cached_tokens` ONLY when
  `Cached > 0`), `finish_reason`, `data: [DONE]`) or the non-stream JSON equivalent; `HTTP`
  turns write status/body/`Location` verbatim and NO SSE; `Hang` sleeps until the duration or the
  request context ends. Options: `WithRequestLog` (default on), `WithAPIKey(key)` (requires
  `Authorization: Bearer key`, else 401), `WithLatency(d)` (before every reply).
- Matching (`match.go`): requests are numbered; the Nth request takes the Nth unconsumed
  turn WITHOUT a `When`, unless an unconsumed turn WITH a `When` matches (first such wins);
  `Repeat: true` turns are never consumed. No match → HTTP 500 body `stubllm: no turn for request
  N` and a log entry with `Unmatched: true`.
- `internal/stubllm/log.go`: `type Request struct { N int; Model string; Messages []Message;
  Tools []string; Stream bool; Unmatched bool; TurnIndex int; At time.Time }`;
  `(*Server).Requests() []Request`, `(*Server).LastMessage(n) string`, `(*Server).Unmatched()
  []Request`. `(*Server).AssertConsumed(t)` fails if any non-repeat turn was never served.
- `internal/stubllm/doc.go` narrates the package (the design doc's `## stubllm` section is
  written in this item too: format, matching rule, turn kinds, a 12-line example script).

**Files:** `internal/stubllm/{doc,script,match,server,log}.go`, `internal/stubllm/*_test.go`,
`docs/design/test-drivers.md` (`## stubllm`), `go.mod`/`go.sum` if a YAML dep is not already
present (use the module the config package already uses).

**Tests:** `internal/stubllm/server_test.go` — (a) a text turn streams the exact rune sequence
with the configured chunking and the provider client (`provider.NewClient(srv.URL, …)`) yields the
same text; (b) a tool-call turn round-trips through `provider` as a named, id'd call; (c) `usage`
with `Cached: 0` omits `prompt_tokens_details`, `Cached: 3` includes it; (d) `When.LastMessage`
beats sequential order and `Repeat` is never consumed; (e) unmatched → 500 + `Unmatched`;
(f) `HTTP{Status: 308, Location}` writes no SSE; (g) `Hang` returns when the request context is
cancelled; (h) YAML round-trip `Parse(Marshal(s)) == s`; (i) validation rejects a turn that is
both text and http. `script_test.go` pins the example script in the design doc by loading it.

**Acceptance:** `go test -race -count=1 ./internal/stubllm/` passes; `go vet ./internal/stubllm/`
clean; `grep -c "^### \|^## stubllm" docs/design/test-drivers.md` shows the section is filled.

**Commit:** `test(stubllm): a scripted OpenAI-compatible upstream — ordered turns, matchers, SSE, request log`

## 3. `cmd/stubllm` — serve and record — ✅ DONE (2026-08-27)

NOTES (2026-08-27): `internal/stubllm/doc.go` gains one line for `record.go` in its file map — not
named on the item's Files line, but the map is the package's navigation aid and a file missing
from it is a rotted map.
NOTES (2026-08-27): the item's Tests paragraph names two `cmd/stubllm/main_test.go` cases; the
serve case plays the checked-in `cmd/apogee/testdata/stubllm/example.yaml` rather than a temp
script, so the fixture the acceptance line runs by hand is also exercised by `go test`, and a
third case pins the other half of the exit-code split (a missing script is a run failure, exit 1,
no usage dump). The exit statuses are asserted through `exitCodeFor` — the function `main` exits
with — rather than by spawning the binary.
NOTES (2026-08-27): no CHANGELOG entry from this item — plan item 8 owns the single `[Unreleased]`
entry covering the whole kit (items 2–7), as recorded under item 2.
NOTES (2026-08-27): `make clean` is left untouched (it removes `./apogee` only); the item asked
for the `stubllm` target and the `.gitignore` entry, and `clean`'s scope was not part of it.

**What:** the thin binary around item 2, plus the recorder.

- `cmd/stubllm/main.go` (cobra like `cmd/apogee`, or flag — match the repo's smaller commands):
  `stubllm serve --script f.yaml --listen 127.0.0.1:0` (prints the bound address on stdout as
  `listening http://…` so a shell test can capture it; SIGINT exits 0) and
  `stubllm record --upstream http://host:1111 --listen 127.0.0.1:0 --out fixture.yaml`.
- `internal/stubllm/record.go`: `Recorder` is an `httputil.ReverseProxy` over `/v1/*` that
  captures, per `/v1/chat/completions` request, the last message's text (→ `When.LastMessage`,
  regexp-quoted), the reply's content/reasoning/tool-call deltas re-joined into one `Turn`, the
  measured inter-chunk delay (median → `TokenDelay`, rune count per chunk → `ChunkRunes`), the
  `usage` object incl. `cached_tokens`, and a non-2xx status as an `HTTP` turn. `Model` is taken
  from the first request. On `Close()` it writes the Script YAML with a header comment naming
  the upstream and the date. Non-stream replies are recorded as text turns with `TokenDelay: 0`.
- Makefile: `build` unchanged (still `$(PKG)` only); add `## stubllm: build the scripted upstream`
  target `stubllm: go build -o stubllm ./cmd/stubllm`. `cross`/`dist` untouched — the binary is a
  dev tool, not a release asset. Add `stubllm` to `.gitignore` beside `apogee`.
- Design doc `## stubllm` gains a "Recording a fixture" subsection with the two command lines and
  the rule that fixtures live under `cmd/apogee/testdata/stubllm/` and are named
  `<server>-<what>.yaml`.

**Files:** `cmd/stubllm/main.go`, `internal/stubllm/record.go`, `internal/stubllm/record_test.go`,
`Makefile`, `.gitignore`, `docs/design/test-drivers.md`

**Tests:** `record_test.go` — a recorder in front of an item-2 `Server` playing a three-turn
script (text with delay, tool call, usage with cached tokens) produces a Script that, replayed
through a second `Server`, yields the same provider-level deltas, a `TokenDelay` within ±50 % of
the original, and a `When` that matches the original request; a 308 upstream is recorded as an
`HTTP` turn. `cmd/stubllm/main_test.go` — `serve` on `:0` prints `listening http://…` and answers
`/v1/models`; `record` with no `--upstream` exits 2 with usage.

**Acceptance:** `go test -race -count=1 ./internal/stubllm/ ./cmd/stubllm/` passes; `go run
./cmd/stubllm serve --script cmd/apogee/testdata/stubllm/example.yaml --listen 127.0.0.1:0`
prints a `listening` line (the example fixture is checked in by this item).

**Commit:** `test(stubllm): cmd/stubllm serve and record — fixtures come from real servers`

## 4. `internal/tuitest` Frame over x/vt, and the `tui.Run` split — ✅ DONE (2026-08-27)

NOTES (2026-08-27): `Screen` gained `Close()` and a permanent answer-drain goroutine, neither
named on the item's API list. `vt.Emulator`'s reply pipe is an unbuffered `io.Pipe`: an
undrained DA1/DECRQM/CPR answer blocks the emulator mid-write and with it the program painting
into it, so the drain cannot be the caller's job. `vt.Emulator.Close()` is deliberately NOT
used to stop it — it writes a `closed` flag the blocked `Read` reads with no happens-before,
which `-race` reports — so a NUL-framed sentinel written through `Emulator.InputPipe()` ends
the pump instead.
NOTES (2026-08-27): `Frame` gained `Styled()` (the SGR render captured inside the same
snapshot) so a `WaitFor` failure prints a plain frame and a styled one that are the same
instant; `Style`, `SameColor`, `Redact` and `ApplyRedactions` are the supporting exports the
item's field lists imply but do not name.
NOTES (2026-08-27): `CheckLeaks` ignores `go test`'s own harness goroutines
(`testing.tRunner(`, `testing.(*M).Run(`, `testing.runTests(`) as well as its own inspection
frame. A test function that LIVES in a marked package — every test in `internal/tuitest`, every
driver test in `cmd/apogee` — names that package in its own stack, so without this exemption
the check reports the goroutine that called it. This is the concrete form of the item's
"standard library and `testing` goroutines are ignored".
NOTES (2026-08-27): the failure paths (`WaitFor`'s timeout message, `Golden`'s mismatch diff,
`CheckLeaks`' report) are asserted against the pure functions that build them —
`waiter.failure`, `compareGolden`, `unifiedDiff`, `leakedGoroutines` — rather than against a
fake `testing.TB`. `testing.TB` carries an unexported method and cannot be implemented outside
the `testing` package, so a stub TB is not available to any test in this repo.
NOTES (2026-08-27): the `Build` option assembly is an unexported `buildProgramOptions` over a
shared `baseProgramOptions`, so `programOptions` keeps its exact signature and its four
existing pins (diagnostics_test.go, environ_test.go, syncoutput_test.go,
conpty_windows_test.go) are untouched, as the item requires.
NOTES (2026-08-27): two of the four `Build` tests run a real `tea.Program` (input disabled,
window size fixed, stopped by cancelling the program context). `tea.Program`'s output field is
unexported, so "the caller's option lands last" and "the driver's writer is where frames
actually land" can only be proven behaviourally; both finish in ~25 ms.
NOTES (2026-08-27): no `docmap_test.go` for `internal/tuitest` — `doc.go` carries a complete
file map, but the package is at 6 non-test files, under the ~10 the house rule triggers at.
Plan items 5 (driver.go, keys.go) and 6 (pty.go, pty_windows.go) take it to 10; item 6 should
add the one-line `docmap.Check(t)` guard.
NOTES (2026-08-27): no CHANGELOG entry from this item — plan item 8 owns the single
`[Unreleased]` entry covering the whole kit (items 2–7), as recorded under items 2 and 3.

**What:** the shared frame type and the composition seam both drivers use.

- `go get github.com/charmbracelet/x/vt@v0.0.0-20260823001701-96af6d2cb5f6` (pins the repo's
  `x/ansi`; note the pseudo-version in `go.mod` with a comment line in the design doc).
- `internal/tuitest/frame.go`: `type Frame struct{ … }` snapshot of an emulator —
  `String() string` (rows joined, trailing spaces trimmed), `Rows() []string`, `Cell(x, y)
  Cell` (`Rune string; Width int; FG, BG color; Bold, Faint, Reverse bool`), `Find(text) (x, y
  int, ok bool)`, `Row(y) string`, `StyleRuns(y) []Run` (contiguous cells sharing FG/BG/attrs
  — the colour assertion primitive), `Cursor() (x, y)`, `Width()/Height()`. `Screen` wraps
  `*vt.Emulator` behind a mutex: `Write` (io.Writer for the renderer), `Snapshot() Frame`,
  `Resize`, `Answers() io.Reader` (the emulator's DA1/DECRQM/CPR replies), `Quiet(d) bool`
  (no bytes for d), `BytesWritten() int64`, `FullRepaints() int` (count of writes containing a
  clear-screen / cursor-home sequence — the T-24 flicker proxy).
- `internal/tuitest/wait.go`: `WaitFor(t, func() bool, opts…)` polling at 20 ms with a
  default 5 s deadline that fails with the LAST frame printed plain AND its styled render
  (`Screen.Render()`, SGR intact) logged beneath it, so a colour bug is visible in the failure
  output; when `TUITEST_ARTIFACTS=<dir>` is set both are also written to
  `<dir>/<test name>.{txt,ansi}` (A9). `WaitText(t, screen, text)`, `WaitGone(t, screen, text)`,
  `WaitQuiet(t, screen, 150ms)`.
- `internal/tuitest/golden.go`: `Golden(t, name, frame, redact ...Redaction)` compares
  `frame.String()` — after every `Redaction{Pattern *regexp.Regexp; With string}` is applied —
  against `testdata/frames/<name>.txt` relative to the calling test package; `-update` flag
  (registered once via `flag.Bool` in the package) rewrites the REDACTED text; on mismatch prints
  a unified diff. The redaction set is the caller's (item 5's `launchTUI` supplies the default one:
  the temp home and workspace paths → `<home>`/`<ws>`, the build version → `<version>`, today's
  date in `Session 2006-01-02` titles → `<date>`, relative ages such as `3 min ago` → `<age>`),
  because a golden that carries a `t.TempDir()` path or today's date churns on every run (A7).
- `internal/tuitest/leak.go`: `CheckLeaks(t)` registers a `t.Cleanup` that snapshots
  `runtime.Stack(all)` and fails if a goroutine whose frame names `internal/tui`, `bubbletea`,
  `internal/tuitest`, `internal/filewatch` or `internal/heartbeat` outlives the test by more than
  2 s of polling; standard library and `testing` goroutines are ignored. No new dependency (A9).
- **`tui.Run` split** (`internal/tui/tui.go`): extract `func Build(ctx, eng Engine, br *Bridge,
  opts Options, out io.Writer, extra ...tea.ProgramOption) (*tea.Program, func(), error)` —
  everything `Run` does between `newModel` and `tea.NewProgram` (flushEvents, diag log,
  programOptions) plus `br.Bind(program)`, returning the program and a cleanup that closes
  diag/trace files. `out == nil` is the production path: `programOptions` runs unchanged (the
  trace and the sync-query stripper wrap `os.Stdout` exactly as today). `out != nil` is the
  driver path: `programOptions`' output half is SKIPPED (no `programOutput` call) and
  `tea.WithOutput(out)` is added; a non-empty `opts.TracePath` together with a non-nil `out`
  is an error (`tui: --tui-trace wraps the real terminal; a driver output is traced by the driver`)
  rather than a silently empty trace file (A1 — `programOutput` wraps `os.Stdout`,
  `tui.go:1619-1624`, so a caller's `WithOutput` winning would leave the trace blank). `extra`
  options are appended LAST so a caller's `WithInput`/`WithWindowSize`/`WithColorProfile`/
  `WithEnvironment` win. `Run` becomes: claim the alt screen if stdout is a terminal,
  `Build(…, nil)`, `program.Run()`, cleanup. `internal/tui/doc.go` narrates nothing new (no new
  file); the existing `programOptions` test keeps its "exactly the option it has always had"
  pin; new tests pin that `Build(nil)` yields the same options as before, that `Build(out)`
  carries `WithOutput(out)` and no traced wrapper, that trace+out is refused, and that `extra`
  lands last.
- Design doc `## tuitest` → "Frame" subsection: the cell model, why cells not strings, the
  golden rule (call 13), the `-update` flag.

**Files:** `go.mod`, `go.sum`, `internal/tuitest/{doc,frame,screen,wait,golden,leak}.go`,
`internal/tuitest/*_test.go`, `internal/tui/tui.go`, `internal/tui/tui_test.go` (or the file
that pins `programOptions`), `docs/design/test-drivers.md`

**Tests:** `frame_test.go` — write a known ANSI sequence (two coloured words, a wide rune, a
cursor move) into `Screen` and assert `String()`, `Cell(...)`.Width == 2 for the wide rune,
`StyleRuns` reports the red run's bounds, `Find`, `Resize` reflows, `FullRepaints` counts a
`\x1b[2J`; `wait_test.go` — `WaitFor` fails with the frame text AND the styled render in the
message after its deadline, and writes both files when `TUITEST_ARTIFACTS` is set;
`golden_test.go` — mismatch reports a diff, `-update` rewrites (use a temp `testdata` via an
internal hook), a redaction replaces a temp path before comparing; `leak_test.go` — a goroutine
parked in a `internal/tuitest` frame trips `CheckLeaks`, a finished one does not. `internal/tui`:
`TestBuildNilOutputMatchesRun`, `TestBuildDriverOutputSkipsTrace`, `TestBuildRefusesTraceWithOutput`,
`TestBuildAppendsCallerOptionsLast`, the existing `programOptions` pin and
`TestModelNoBuilderByValue` all green.

**Acceptance:** `go test -race -count=1 ./internal/tuitest/ ./internal/tui/` passes; `go build
./...` clean; `grep -n "func Build" internal/tui/tui.go` exists.

**Commit:** `test(tuitest): Frame over x/vt, WaitFor and goldens; tui.Build splits program construction from Run`

## 5. `internal/tuitest` in-process Driver over the launcher seam, and the first e2e (T-25 in-process) — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item names the cmd/apogee half's type `session`; `cmd/apogee` already
imports `internal/session` under that name, so a `session` type in package main does not
compile (nine existing files declare `session` as the package identifier). It is `e2eSession`
here, and `launchTUI` returns it.
NOTES (2026-08-27): `WithStub(stub)` is folded into `launchTUI(t, drv, stub, args...)` as a
parameter rather than an option constructor, so `args ...string` stays the variadic tail the
item's signature gives it. A nil stub seeds an unreachable endpoint, which startup accepts (ADR
0024 decision 8).
NOTES (2026-08-27): `Driver` gained `Attach(prog, cancel)` and `Finished(err)`, which the
item's API list implies but does not name: it names `Resize` (sends through the program),
`Kill` (cancels the program ctx) and `Done()` (the run's result) without saying how the driver
comes by any of the three. `Quit()` returns that result rather than nothing, so T-25 step 10's
"Quit() returns nil" is a claim the test can make.
NOTES (2026-08-27): `Driver.Output()` is the Screen behind an ONLCR translation, not the Screen
itself. With a non-tty input bubbletea puts the renderer in map-newline mode (tea.go:1075, a
workaround for emulated ptys left in cooked mode): it then moves the cursor down with a bare LF
*and assumes the column reset to 0* (ultraviolet/terminal_renderer.go:1382). A raw terminal
holds the column, so without the translation the renderer's model of where it is drifts at the
first full-width row and every frame after it is unreadable. The driver is therefore the
terminal bubbletea believes it is talking to — a line discipline with ONLCR. The PTY driver
(item 6) must NOT do this, which is a second reason the two drivers' byte counters are not
comparable (the item already records the first).
NOTES (2026-08-27): a lone `Esc` is followed by a 70 ms gap before the driver writes again. No
reader can tell the Escape KEY from the start of an escape SEQUENCE without a timeout
(ultraviolet's is 50 ms), so `Esc` then `/` typed microseconds apart is delivered as one
`alt+/` — which is exactly what happened to `/version` in the smoke test. It is the one place a
driver waits on a clock rather than on the screen.
NOTES (2026-08-27): `Resize` waits for the repaint it caused, rather than leaving that to the
caller. The emulator resizes the instant it is asked, so a frame read straight after is the OLD
frame at the NEW size — and a settle check passes, because the screen has genuinely been quiet:
the program was never given a chance to paint. The in-test form of the trap cost the
wide-resize assertion of T-25 step 9.
NOTES (2026-08-27): `Kill` ends the input BEFORE it cancels. A killed bubbletea program skips
`waitForReadLoop` and closes its cancel reader out from under the live read loop
(tea.go:1249-1255), which `-race` reports as a data race inside muesli/cancelreader, and which
can also leave the read loop parked in `EpollWait` on a closed epoll fd. Ending the input first
lets the read loop finish on EOF — which does not quit the program, so what the cancel kills is
still a running program.
NOTES (2026-08-27): item 4's `CheckLeaks` gained one exemption — goroutines parked in
`bubbletea.Tick.func1`. `tea.Tick` starts a goroutine that waits out its whole interval and
cannot be cancelled by design, so every tick a TUI has in flight when it quits outlives the
test; reporting those makes the check unusable against any program that ticks. They hold
nothing and are already over. `TestDriverBurstOfKeysArrivesWhole` was added alongside, pinning
that a burst of presses reaches the model whole — the pin that says a lost keystroke in a
driver test is the program's doing, not the driver's.
NOTES (2026-08-27): T-25 step 5's walk is one lap of the settings list, one key at a time, and
five rows back up rather than the full list back. Three findings forced the shape: the pane
WRAPS (so "past the last row" never stops moving and the walk ends when the selection returns
to where it started), the pane is taller than the terminal (so the ten section headers can only
be collected across frames), and the pane drops rapid ↓ presses (see the DEFER line) — so each
press waits for the selection to move. The full walk back up cost another ~2 s of the package's
budget and proved nothing the lap had not.
NOTES (2026-08-27): T-25 step 10's "Session saved · resume with" line is written straight to
`os.Stdout` by `runRoot` (wire.go), not through the cobra command's out, so a driven run cannot
capture it; the step asserts the session record on disk instead, which is the claim behind the
sentence.
NOTES (2026-08-27): step 12 asserts the RENDERED transcript — "Write", "entries" — rather than
the wire tool names `list_dir`/`write_file`, which the transcript never shows. The item's step
list names the wire spellings.
NOTES (2026-08-27): `cmd/apogee/testdata/stubllm/smoke.yaml` carries two `repeat: true` turns
the item's script sketch does not: apogee asks for a session title off the first prompt (that
request's text CONTAINS the prompt, so it must be matched first or it takes the prompt's turn),
and a trailing catch-all answers anything else asked on apogee's own account. The four
conversation turns stay non-repeat, so `AssertConsumed` proves each of them fired.
NOTES (2026-08-27): `## Writing a new e2e test` is filled here. The skeleton's placeholder said
"filled by plan item 8", but item 5's own text assigns it to this item; item 8's text covers
the claim table, the skill gate and the coding-standards line. The in-process half of `## Gates
and budgets` (which the skeleton assigns to items 5, 6, 7 and 16) is filled too, with the
measured 8.5 s.
NOTES (2026-08-27): no golden is recorded for the smoke test — goldens are for the rendering
surfaces of items 9–15 (ratified call 13). `e2eSession.Redactions()` exists, is documented, and
is what those items pass to `tuitest.Golden`.
NOTES (2026-08-27): measured wall clock — `go test -race -count=1 -run
'TestE2ESmokeInProcess|TestDriver' ./cmd/apogee/ ./internal/tuitest/` = 9.0 s (acceptance:
under 15 s); the whole `cmd/apogee` package under `-race` = 12.9 s against the plan's 5.5 s
baseline, so the in-process e2e adds ~7.3 s of the ~15 s the plan budgets for the whole set.
Item 6's PTY smoke has ~7.7 s of that left.
NOTES (2026-08-27): no CHANGELOG entry from this item — plan item 8 owns the single
`[Unreleased]` entry covering the whole kit (items 2–7), as recorded under items 2, 3 and 4.

**What:** the driver that runs the real composition inside the test binary.

- The launcher seam is in package `main` (`cmd/apogee`), so the driver is split in two: the
  generic half in `internal/tuitest/driver.go` — `type Driver struct{ … }`; `func NewDriver(t,
  Size{W,H}) *Driver` creates the `Screen` and ONE `os.Pipe`: the program's input is the pipe's
  READ END (an `*os.File`, so bubbletea's cancel reader takes the epoll path and `Quit` returns
  at once — a custom merged `io.Reader` would fall back to the non-cancellable reader,
  `cancelreader_linux.go:23-25`, costing `waitForReadLoop`'s 500 ms per quit and a leaked
  goroutine, A3); test keystrokes and a pump goroutine over `Screen.Answers()` both write to
  the pipe's write end under one mutex; `Close()` closes the write end, which is what ends the
  read loop. `(*Driver).ProgramOptions() []tea.ProgramOption` returns
  `WithInput(pipeReader)`, `WithWindowSize(W,H)`, `WithoutSignals()`,
  `WithoutSignalHandler()`, `WithColorProfile(colorprofile.TrueColor)`, `WithEnvironment([]string{
  "TERM=xterm-256color","COLORTERM=truecolor"})` — the OUTPUT is not an option: it is the
  `out` argument of `tui.Build` (item 4, A1), `(*Driver).Output() io.Writer` = the `Screen`;
  `Type(text)`, `Press(key Key)` (Enter, Esc,
  Tab, ShiftTab, Up, Down, PgUp, Space, CtrlC, AltUp, AltDown, F-keys — each the exact byte
  sequence bubbletea's v2 key parser decodes; a table test pins each against `tea` by feeding
  it through a tiny model), `Resize(w,h)` (emulator resize + a `tea.WindowSizeMsg` via the
  program's `Send` — the in-process stand-in for SIGWINCH), `Frame()`, `Screen()`, `Wait…`
  passthroughs, `Quit()` (Ctrl+C twice through the pipe, then wait for `Run` to return), `Kill()`
  (cancel the program ctx without Quit), `Done() <-chan error` (the `program.Run` result).
  The `cmd/apogee` half in `cmd/apogee/e2e_support_test.go` — `launchTUI(t, drv *tuitest.Driver,
  args ...string) *session` builds a temp home + workspace (reusing `upstreamHome`'s seeding, plus
  `--config <home> --workspace <ws>`), runs `runRoot(ctx, opts, launch)` in a goroutine where
  `launch` calls `tui.Build(ctx, eng, br, opts, drv.Output(), drv.ProgramOptions()...)` and
  `program.Run()`; `session.Relaunch()` runs the same again on the same home/workspace with a
  fresh Driver (the T-03 reopen). `WithStub(stub *stubllm.Server)` seeds `servers:` with its URL
  and model. `launchTUI` calls `tuitest.CheckLeaks(t)` first (A9) and builds the default golden
  redactions — `session.Redactions()`: home, workspace, version, date, age (A7) — so every
  `Golden` call in `cmd/apogee` passes `sess.Redactions()...`. It never passes `--tui-trace`
  (item 4 refuses it on a driver output).
- Repaint measure, in-process: `session.BytesWritten()` and `session.FullRepaints()` read the
  Driver's `Screen` counters — what the renderer wrote into the driver's output, byte for byte.
  The `--tui-trace` file is the PTY driver's measure (item 6). The two are NOT comparable:
  bubbletea maps `\n` → `\r\n` when its input is not a tty (`tea.go:1075`), so item 9's ceiling
  is measured and pinned PER DRIVER (A1).
- First consumer — `cmd/apogee/e2e_smoke_test.go` `TestE2ESmokeInProcess` = checklist **T-25**
  steps 1–13 against a stubllm script (reply with a `list_dir` tool call and a summary; an
  `append_file`/`write_file` call for `a.txt`; a plain reply for the restored-session prompt):
  frame draws with header/transcript/prompt/footer and the footer ends with `◐ ask before`; the
  tool block appears; the approval pane offers `Allow`/`Always allow this session`/`Deny`/
  `Cancel`; `a` runs the write and the file on disk carries the line; `/settings` opens with the
  ten section headers and ↓ past the last row is safe; `/usage` shows non-zero numbers; `/skills`
  shows the honest empty line; `/version` prints the build version; `Resize(60,20)` then
  `(120,40)` reflows with the footer truncating from the left with `…`; `Quit()` returns nil;
  `Relaunch()` → `/sessions` lists the session with its hint line; ⏎ restores; the transcript
  holds the earlier prompts, replies and tool blocks with outcomes; one more prompt answers and
  the record on disk grows. Step 10's "terminal left clean" is the PTY variant's (item 6).
- Design doc `## tuitest` → "In-process Driver" subsection with the smoke test as the worked
  example, and `## Writing a new e2e test` filled (home/workspace rule, stub script first,
  WaitFor not sleep, golden only for rendering, budget).

**Files:** `internal/tuitest/{driver,keys}.go`, `internal/tuitest/driver_test.go`,
`cmd/apogee/e2e_support_test.go`, `cmd/apogee/e2e_smoke_test.go`,
`cmd/apogee/testdata/stubllm/smoke.yaml`, `docs/design/test-drivers.md`

**Tests:** `driver_test.go` — every `Key` decodes to the intended `tea.KeyPressMsg` through a
probe model; `Type` delivers runes in order; `Resize` reaches the model as a `WindowSizeMsg`; the
merged reader forwards the emulator's DA1 answer to the program. `TestE2ESmokeInProcess` as
described, ≤ 5 s under `-race`.

**Acceptance:** `go test -race -count=1 -run 'TestE2ESmokeInProcess|TestDriver' ./cmd/apogee/
./internal/tuitest/` passes in under 15 s; the real `~/.apogee` is untouched (assert
`APOGEE_CONFIG` was set to the temp home for the whole run: the test checks
`os.Getenv("APOGEE_CONFIG")` is unset and never writes outside `t.TempDir()`).

**Commit:** `test(tuitest): in-process Driver over the launcher seam; T-25 smoke runs under go test`

## 6. `internal/tuitest` PTYDriver, TestMain build, and the PTY smoke (T-25 black-box)

**What:** the black-box driver for claims only a real terminal shows.

- `internal/tuitest/pty.go` (`//go:build !windows`): `func NewPTYDriver(t, bin string, args
  []string, env []string, Size) *PTYDriver` — `pty.StartWithAttrs` with
  `TERM=xterm-256color COLORTERM=truecolor` plus the caller's env, master → `Screen.Write` pump
  goroutine, `Screen.Answers()` → master pump (the emulator IS the terminal); `Type`, `Press`,
  `Resize(w,h)` (`pty.Setsize` + `SIGWINCH`), `Frame`, `Wait…`, `Quit()` (Ctrl+C twice, wait,
  return exit code), `Kill()` (`SIGKILL`, wait), `Pid()`, `Exited() <-chan int`. On `t.Cleanup`
  a still-running child is killed. `pty_windows.go` provides the type with `t.Skip` bodies.
- `cmd/apogee/main_test.go`: the package ALREADY has a `TestMain` (line 29 — it redirects
  `HOME`/`USERPROFILE` to a suite temp dir; a second one does not compile, A2). Extend it: after
  the home is set, `go build -o <suiteTempHome>/apogee .` once (`-race` off — the binary under
  test is the shipped shape); set `e2eBinary`; on failure set `e2eBuildErr` and PTY tests
  `t.Skip` with it. No `-run`-aware laziness: always build — it is one cached `go build`
  (~1–2 s) and a conditional build is a second thing to get wrong. `launchPTY(t, stub,
  args...)` in `cmd/apogee/e2e_support_test.go` mirrors `launchTUI` (temp home/ws, `--config`,
  `--workspace`) and DOES pass `--tui-trace <tempfile>` — the PTY driver is where the trace seam
  is exercised; `session.TraceBytes()`/`session.TraceFullRepaints()` read it back.
- `cmd/apogee/e2e_smoke_test.go` `TestE2ESmokePTY` — T-25 again through the binary, adding the
  black-box-only claims: step 9 resize via real `SIGWINCH`; step 10 after `Quit()` the master
  has received the alt-screen leave (`\x1b[?1049l`), cursor-show (`\x1b[?25h`) and no
  dangling SGR (last SGR seen is a reset) — "no `stty sane` needed"; exit code 0; the shell
  prompt is not part of the PTY (no shell), so the tty-state claim is asserted on the escape
  sequences the emulator consumed plus `termios` read back from the slave before close (echo
  and canonical mode restored).
- Design doc `## tuitest` → "PTYDriver" subsection: when to use it (colour, wide runes, resize,
  tty teardown, real pids, SIGKILL), the TestMain build, the settle rule, Windows skip.

**Files:** `internal/tuitest/{pty,pty_windows}.go`, `internal/tuitest/pty_test.go`,
`cmd/apogee/e2e_support_test.go`, `cmd/apogee/e2e_smoke_test.go`, `docs/design/test-drivers.md`

**Tests:** `pty_test.go` — drive `/bin/sh -c 'printf "\033[31mred\033[0m ok\n"; read x; echo
$x'` and assert the red run via `StyleRuns`, `Type("hi\n")` echoes, `Resize` changes the
child's `stty size`, `Kill` reaps. `TestE2ESmokePTY` as described, ≤ 8 s.

**Acceptance:** `go test -race -count=1 -run 'TestE2ESmoke|TestPTY' ./cmd/apogee/
./internal/tuitest/` passes; `GOOS=windows go vet ./internal/tuitest/` compiles.

**Commit:** `test(tuitest): PTYDriver over creack/pty + x/vt; the binary is built once per test run; T-25 black-box smoke`

## 7. `internal/judge` — gated, binding LLM verdicts; `make live-eval` gains the gate

**What:** the judgment oracle.

- `internal/judge/judge.go`: `type Rubric struct { Item, Claim, PassWhen, FailsIf string;
  Extra []string }`; `type Artifact struct { Name string; Kind Kind /* frame|stdout|file|
  transcript|trace */; Text string }`; `type Verdict struct { Pass bool; Reasons []string; Raw
  string }`; `func Enabled() bool` (`APOGEE_JUDGE_ENDPOINT` or `APOGEE_LIVE_ENDPOINT` set);
  `func Skip(t)` (`t.Skip("set APOGEE_JUDGE_ENDPOINT (and optionally APOGEE_JUDGE_MODEL) to run
  judge tests")`); `func Verdict(ctx, r Rubric, a ...Artifact) (Verdict, error)` — one
  `provider.NewClient` call, temperature 0, a fixed system prompt ("you are a release tester;
  answer ONLY the JSON object …"), the rubric and artifacts fenced and labelled; the reply is
  parsed strictly (first `{…}` object, `verdict` ∈ {pass, fail}, `reasons` array) and a parse
  failure is an `error`, not a fail; `func Require(t, ctx, r, a...)` = `Skip` if not enabled,
  `t.Fatal` on error, `t.Errorf` with the reasons on fail; `func Pairwise(ctx, r Rubric, before,
  after Artifact) (Verdict, error)` — "is `after` no worse than `before` under this rubric".
  `FrameArtifact(name, tuitest.Frame, withStyles bool)` serialises a frame; with styles, runs
  are wrapped `⟨red⟩…⟨/red⟩`/`⟨bold⟩…⟨/bold⟩` from `StyleRuns`, resolving the scheme's error
  tone to `red` by comparing against the model's theme colours passed by the caller.
- Model choice: `APOGEE_JUDGE_MODEL`, else `APOGEE_LIVE_MODEL`, else the endpoint's first
  `/v1/models` entry (via `provider` discovery). `APOGEE_API_KEY` is honoured as the live tests
  do.
- Makefile `live-eval`: add `APOGEE_JUDGE_ENDPOINT=$(JUDGE_ENDPOINT)` with `JUDGE_ENDPOINT ?=
  $(LIVE_ENDPOINT)` and widen `-run` to `'TestE2ELiveModel|TestLiveDelegateCapAndWorkingWindow|
  TestJudge'` over `./internal/tui/ ./internal/agent/ ./cmd/apogee/`.
- Design doc `## judge`: the gate, the contract, how to write a rubric (copy the checklist's
  Pass when / Fails if verbatim; one claim per call), when Pairwise, that a verdict is binding,
  and a note that a weak local judge is a reason to improve the rubric, not to make the verdict
  advisory. State the determinism limit plainly (A10): temperature 0 on a local server is not
  bit-reproducible (sampler seed, batch composition), so a judge failure is re-run ONCE by hand
  (`go test -run <name> -count=1`) before it is believed; two fails in a row are a real fail;
  `TestJudgeSelfCheck` runs first in `make live-eval` so a broken judge is reported as such and
  not as twenty rubric failures. The verdict stays binding.

**Files:** `internal/judge/{doc,judge,artifact}.go`, `internal/judge/judge_test.go`, `Makefile`,
`docs/design/test-drivers.md`

**Tests:** `judge_test.go` (no live model) — a stubllm server scripted to answer
`{"verdict":"fail","reasons":["x"]}` yields `Pass=false`; prose around the JSON still parses;
`{"verdict":"maybe"}` is an error; `Require` skips when the gate is unset (subtest with the env
cleared); the request body carries the rubric's four fields and every artifact name; `Pairwise`
sends both artifacts labelled. `TestJudgeSelfCheck` (gated) — a rubric "the text says hello"
against `hello` passes and against `goodbye` fails, the kit's own sanity probe for the
configured judge.

**Acceptance:** `go test -race -count=1 ./internal/judge/` passes with the gate unset (the
self-check skips); `grep -n "JUDGE_ENDPOINT" Makefile` shows the live-eval wiring.

**Commit:** `test(judge): env-gated, binding LLM verdicts over named artifacts through the provider client`

## 8. Adoption — the "which driver" table, the `test-checklist` skill gate, the coding-standards line

**What:** make the kit the default for future work.

- `docs/design/test-drivers.md` `## Which driver observes which claim`: fill the table for
  every claim class the checklist used — stream order/completeness (in-process), pane text and
  wording (in-process + golden + judge), colour/tone (PTY or in-process `StyleRuns`), wide-rune
  alignment (`Frame.Cell.Width`), resize/reflow (in-process `Resize`, PTY `SIGWINCH`), tty
  teardown (PTY), real process lifetime / SIGKILL (PTY), session record on disk (either), config
  file watch (in-process, write the file), daemon/headless output (no driver needed — stdout +
  stubllm), network egress (in-test proxy + stubllm), MCP (in-test httptest MCP server), desktop
  opener (`Opener.LookPath` fake), landlock ABI (CI runner only), docs-as-a-newcomer (docker +
  judge), and the "Not observable" column for font tofu, felt flicker, real desktop
  applications, `brew upgrade` before a release.
- `test-checklist` skill (untracked tree, edit in place, never committed): in
  `/root/.claude/skills/test-checklist/prompts/items.md` (the Phase 3 item writer) add a gate
  before an item may be marked `MANUAL` or `MIXED`: read `docs/design/test-drivers.md`'s table if
  the file exists; a step is manual ONLY when its claim class is in the "Not observable" column,
  otherwise the item is `AUTO` and the item text names the driver and the test to write (or the
  existing test to extend); Phase 4's auto batch then writes/extends that test rather than a
  human step. The same rule in the sequential variant's prompt. Add a `SKILL.md` "Hard rules"
  line: "a manual step needs a table row that says no driver observes it".
- `coding-standards` skill (untracked): `coding-standards.go.md` testing section gains one
  rule: "Claims about the TUI, a streamed reply, an approval pane or a session record are
  asserted through `internal/tuitest` + `internal/stubllm`; judgment claims through
  `internal/judge`. Do not add `httptest` upstream closures — script stubllm."
- CHANGELOG `[Unreleased]`: one entry for the kit (items 2–7) under Added.

**Files:** `docs/design/test-drivers.md`, `CHANGELOG.md`; untracked: `/root/.claude/skills/
test-checklist/prompts/items.md`, `/root/.claude/skills/test-checklist/SKILL.md`,
`/root/.claude/skills/test-checklist-sequential/…` (same rule), `/root/.claude/skills/
coding-standards/coding-standards.go.md`

**Tests:** none (docs/skills). `grep -c "|" docs/design/test-drivers.md` ≥ 18 table rows;
`grep -n "test-drivers" /root/.claude/skills/test-checklist/prompts/items.md` finds the gate.

**Acceptance:** `grep -q "Not observable" docs/design/test-drivers.md && grep -q "tuitest"
CHANGELOG.md`; `git status --porcelain | grep -c skills/` is 0 (nothing from the skills tree is
staged).

**Commit:** `docs(design): which driver observes which claim; changelog entry for the test-drivers kit` (skill edits stay uncommitted by rule)

## 9. T-24 + T-25 residue — streaming, resize mid-stream, cancel, interleaved delegations, flicker ceiling

**What:** the streaming buffer rewrite's visible behaviour.

- `cmd/apogee/e2e_stream_test.go`, in-process: stubllm script — a 400-line numbered reply
  (`ChunkRunes: 3, TokenDelay: 1ms`); `TestE2EStreamCommitsCompleteAndInOrder` waits for the
  commit and asserts the transcript's committed text (read from the session record on disk AND
  from the frame after scrolling to top) numbers 1..400 contiguous, no repeats, no mid-word
  joins (every line matches `^\d+\. \S`); mid-stream `Resize(60,20)` then `(100,30)` — no
  duplicated block (each number appears once in the final scrollback), no line wider than the
  width at any snapshot; `TestE2EStreamCancelKeepsWhatArrived` presses Esc mid-stream: the
  transcript keeps a prefix (≥ 1 line, < 400), the next prompt starts a new entry;
  `TestE2EDelegationsStreamIntoTheirOwnBlocks`: a script whose main turn issues two `sub_agent`
  tool calls and whose child turns (`When.ToolResult`/`LastMessage` matched) each stream a
  distinct marker word 50× — every marker lands only inside its own block and none in the main
  reply (assert on the session record's block ownership and the frame).
- Flicker proxy: `TestE2EStreamRepaintCeiling` — in-process, from `session.BytesWritten()`
  (the Driver's `Screen`), bytes per streamed rune ≤ a ceiling and `session.FullRepaints()` == 0
  during the stream (a clear-screen per token is the failure); the PTY twin inside
  `TestE2EStreamPTY` reads `session.TraceBytes()`/`TraceFullRepaints()` from the trace file.
  Each driver's ceiling is measured on its own first green run and pinned with a comment naming
  the run — the two numbers differ by design (A1: non-tty input maps `\n` → `\r\n`).
- PTY: `TestE2EStreamPTY` re-runs the 400-line stream through the binary with a real
  `SIGWINCH` mid-stream; same assertions on the emulator.
- Judge (gated): `TestJudgeStreamFrames` hands three mid-stream frames + the final one to
  `judge.Require` with T-24's Pass when / Fails if.
- Checklist T-25 residue: steps not in item 5 (none besides the tty claim done in item 6) —
  record in the design doc's example list that T-25 is fully covered.

**Files:** `cmd/apogee/e2e_stream_test.go`, `cmd/apogee/testdata/stubllm/{stream400,delegate2}.yaml`

**Tests:** the five tests above.

**Acceptance:** `go test -race -count=1 -run 'TestE2EStream|TestE2EDelegationsStream' ./cmd/apogee/`
passes in ≤ 6 s; with the judge gate set `TestJudgeStreamFrames` runs.

**Commit:** `test(e2e): streamed reply commits complete and in order across resize, cancel and concurrent delegations (T-24)`

## 10. T-03 + T-04 + T-07 TUI — delegation record on kill/reopen, step-cap marker, firing block `faulted`

**What:** the delegation and scheduler surfaces.

- `cmd/apogee/e2e_delegation_test.go`: **T-03** — script: main turn calls `sub_agent`; the
  child's first turn calls `read_file`, its second turn is `Hang: 1h`. In-process:
  `WaitText("read_file")`, then poll the home's `sessions/` dir until a record exists while the
  block is still running (`WaitFor`), `Kill()`, `Relaunch()`, `/sessions`, ⏎ on the top row;
  assert every in-flight call reads `interrupted — the run did not finish`, nothing paints as
  running, and the notes carry `resumed: <title>` plus the verbatim "saved while a delegation was
  still running" line; the no-delegation edge (plain reply, kill, reopen) shows `resumed:` only.
  PTY variant `TestE2EDelegationRecordSurvivesSIGKILL` does the same with `Kill()` = `SIGKILL`.
  **T-04** — home seeded with `delegate-max-steps: 3`; child script `Repeat: true` tool-call
  turn; assert the child stops after 3 steps (stubllm request count for the child ≤ 3 + 1), the
  error line is the verbatim cap sentence, the parent's result's first line is the verbatim
  `[delegate stopped at its step cap (3 steps); partial result — …]` marker, the block is NOT
  in the error tone (`StyleRuns` on the outcome slot ≠ the scheme's error colour), `max_steps:
  50` in the call still stops at 3, and `delegate-max-steps: 0` runs past 3. Golden of the
  expanded block; judge (gated) with T-04's oracle over the block frame.
  **T-07** — the TUI firing block: script = the empty-reply turn (`Text: ""`, `FinishReason:
  stop`). Clock seam (A4, decided): the TUI scheduler is built at `cmd/apogee/wire_live.go:335`
  with `schedule.Config{Fire, Gate, Notify}` and NO `Clock`, while the daemon's at
  `daemon.go:246-250` already takes the package var `daemonClock`. Add
  `var tuiScheduleClock schedule.Clock` (nil = system) in `cmd/apogee/schedule.go`, passed as
  `Clock:` in that `schedule.New` call — the same shape as `daemonClock`, a `cmd/apogee`-level
  seam, no `tui.Options` field and nothing in the engine (the ADR names it under "no engine
  hooks" as the Driver-level seam it is; the scheduler is a Driver concern per ADR 0033). The
  fake clock is the one `internal/schedule`'s own tests use (`clock.go`'s `Ticker` shape); the
  test swaps both vars and restores them in `t.Cleanup`. With a fake clock: `/schedule 30s say
  hi`, advance the clock, assert the block's stats line ends `· faulted` and the expanded body
  reads `final turn abandoned — upstream returned an empty reply (finish: stop)` plus the
  `saved as` pointer; `/schedule-stop`. Daemon half: `TestDaemonFaultedVerbColumn` runs
  `apogee daemon` in-process (as `headless_test.go` does for its verb) with `daemonClock`
  swapped for the same fake. No `MinCycle`/`APOGEE_E2E_SLOW` fallback exists any more.

**Files:** `cmd/apogee/e2e_delegation_test.go`, `cmd/apogee/e2e_schedule_test.go`,
`cmd/apogee/testdata/stubllm/{delegate-hang,delegate-cap,empty-reply}.yaml`,
`cmd/apogee/testdata/frames/t04-*.txt`, `cmd/apogee/schedule.go` (`tuiScheduleClock`),
`cmd/apogee/wire_live.go` (the one `Clock:` line)

**Tests:** as listed; goldens for the T-04 block.

**Acceptance:** `go test -race -count=1 -run 'TestE2EDelegation|TestE2EFiring|TestDaemonFaulted'
./cmd/apogee/` passes; the T-03 PTY variant runs on Linux/macOS.

**Commit:** `test(e2e): delegation records survive SIGKILL, the step-cap marker, and the faulted firing block (T-03, T-04, T-07)`

## 11. T-06 + T-10 + T-13 — usage with cached tokens, the forced-approval `Fix:` pane, key arming

**What:** accounting and approval surfaces.

- **T-06** `cmd/apogee/e2e_usage_test.go`: fixture `cmd/apogee/testdata/stubllm/cached-usage.yaml`
  (hand-written now; the design doc notes it is to be re-recorded with `stubllm record` against a
  prefix-caching server when one is at hand) — turn 1 usage `{prompt: 900, completion: 40}`,
  turn 2 `{prompt: 950, completion: 30, cached: 880}`, a delegation turn with its own usage.
  Assert `/usage` header `agent · calls · prompt · cached · completion · total · ctx` with
  `cached` directly after `prompt`, the `main` row's cached ≤ prompt in the same unit spelling,
  one row per delegate, `session` = main + delegates; `/sessions` row ends with `· <total>` equal
  to the session row; `Relaunch()` with `--continue` + one message → totals continue; headless
  (`headlessRun`) stderr carries `· cached N` only on a cached call; the negative fixture (no
  usage details) draws NO `cached` column.
- **T-10** `cmd/apogee/e2e_approval_test.go`: the guard matches the literal text `~/.apogee`, so
  the test runs WITH `--config <temp>` (the guard is textual, not path-based — checklist
  precondition) and prompts the stub to call `terminal` with `ls /tmp` (control: pane has
  `Reason: subprocess execution`, no `Fix:`), then `ls ~/.apogee` (pane: `Reason: dangerous-action
  guard forced approval`, next line `Fix: a terminal command is refused whenever …` verbatim;
  golden `t10-forced-pane.txt`; at 60 columns the continuation rows hang under the `Fix:` indent
  — assert column of each continuation row), `d` → the tool result carries the same hint,
  `touch ~/.apogee/guard-probe.txt` (HOME pointed at a temp dir for the test) allowed with `a`
  runs; `--mode auto` still panes. Judge (gated): the pane frame against T-10's step-5/6
  judgment ("does the Fix line read as help beside an open question").
- **T-13** same file: `TestE2EApprovalKeysAreArmedAfterPaint` — the driver writes `a` into the
  input pipe BEFORE the pane is due (immediately after the prompt), the stub's tool call arrives,
  assert the pane PAINTS (its text appears in the trace bytes and the frame) and is NOT resolved
  by the early key (the pane still shows after `WaitQuiet`); then a deliberate `a` resolves it
  immediately (≤ 200 ms); `d` denies; Esc on the first frame cancels (send Esc the instant
  `WaitText("Allow")` returns); `j`/`k`/PgUp scroll and leave the pane; `s` takes "Always allow
  this session" and the next identical call runs with no pane. Trace evidence: the trace file
  contains `Always allow this session` at least once per pane.

**Files:** `cmd/apogee/e2e_usage_test.go`, `cmd/apogee/e2e_approval_test.go`,
`cmd/apogee/testdata/stubllm/{cached-usage,guard}.yaml`, `cmd/apogee/testdata/frames/t10-*.txt`

**Tests:** as listed.

**Acceptance:** `go test -race -count=1 -run 'TestE2EUsage|TestE2EApproval' ./cmd/apogee/`
passes.

**Commit:** `test(e2e): cached-token accounting, the forced-approval Fix line, and key arming after paint (T-06, T-10, T-13)`

## 12. T-12 + T-20 — hostile text on every surface; `[✔]`, wide-rune width authority across a scheme switch

**What:** the rendering-integrity items.

- **T-12** `cmd/apogee/e2e_hostile_test.go`: build the checklist's hostile fixture workspace
  verbatim in `t.TempDir()` (escape in the root dir name, `evil\x1b[31mRED\x1b[0m.txt`,
  `two\nrows.txt`, `carriage\rreturn.txt`, bidi override, a skill dir with ESC + newline holding
  an empty `SKILL.md`, the `dupe` shadow pair). Assertions, in-process at 100×30 and 60×24:
  `apogee probe --workspace` output (via `headlessRun`-style capture) is one line for the root
  with zero raw ESC bytes; the footer with `--model "gpt\x1b[31m-oss-20b"` shows the label in
  the footer's own colours (`StyleRuns` of the footer row have no red run) and the rest intact;
  `/skills` note has exactly two rows under "found but not loaded" and two under "shadowed",
  the heading count equals the rows (golden `t12-skills.txt`); `/settings` → `mode` → the
  `auto` sentence wraps under its own column at 60 cols (assert continuation-row indent);
  `list_dir`/`find_files`/`grep` results (stub tool calls) show `\n`/`\r` literally on single
  rows; an approval pane with a 300-char `echo` argument at 60 cols: `command:` on its own row,
  value indented two spaces, continuation rows under that indent (golden `t12-pane-60.txt`).
  The emulator's colour state after every step is the theme's (no leaked red into later rows).
- **T-20** `cmd/apogee/e2e_width_test.go`: multi-select `ask_user` (stub tool call with
  `multi_select: true`, three options) → each row starts with `[ ]` in the same column; Space,
  ↓, Space → two rows read `[✔]` and `Frame.Cell` at the tick is width 1 (the emulator's cell
  width is the authority — call 9); labels align and a long label wraps under the label. Width
  authority: a reply containing a markdown table with `日本語テキスト` and an emoji; record the
  column of the table's right border and the scroll bar; `/settings` → Interface → `ui.color-scheme`
  → `light` → esc; assert the same columns (the emulator answered mode 2027 — assert the diag log
  or the trace shows the DECRQM 2027 query was answered `2`/`1` and the theme rebuild kept the
  Unicode-core measure); switch back to `dark`. Steps 8–9 (syntax regex) are already automated
  (checklist) — add nothing.
- Judge (gated): `/skills` block and the wrapped pane frames against T-12's "reads as one row"
  oracle.

**Files:** `cmd/apogee/e2e_hostile_test.go`, `cmd/apogee/e2e_width_test.go`,
`cmd/apogee/testdata/stubllm/{hostile,widths}.yaml`, `cmd/apogee/testdata/frames/t12-*.txt`

**Tests:** as listed.

**Acceptance:** `go test -race -count=1 -run 'TestE2EHostile|TestE2EWidth' ./cmd/apogee/` passes;
`grep -c $'\x1b' cmd/apogee/testdata/frames/t12-skills.txt` is 0.

**Commit:** `test(e2e): hostile names cannot forge rows; multi-select ticks and the width authority survive a scheme switch (T-12, T-20)`

## 13. T-15 + T-16 — outcome-slot tone and elision; live state follows the running session

**What:** the tone/layout item and the live-state item.

- **T-15** `cmd/apogee/e2e_outcome_test.go`: cancel a delegation with Esc mid-run → the outcome
  slot reads `cancelled`/`interrupted — the run did not finish`, `StyleRuns` on that slot is the
  scheme's error colour and the row carries no `✓`; block cursor ⌥↓ + ⏎ expands it and shows the
  delegate's task prompt as the body (`task:` lead) — golden `t15-cancelled-delegation.txt`; a
  `terminal` call whose stdout is `error: 3 errors found` with exit 0 → the body quotes it, the
  slot is NOT the error colour; two `write_file`/edit calls producing adjacent and non-adjacent
  regions → the expanded diff draws `⋯` only between regions that do not meet (assert on the
  frame rows). PTY variant for the colour claim (`TestE2EOutcomeTonePTY`) — the binary's real
  SGR, through the emulator.
- **T-16** `cmd/apogee/e2e_livestate_test.go`: Shift-Tab ×3 → footer `⏵⏵ auto · <word>` where
  the word is what `apogee probe` on this host reports (read it via the probe command in-test);
  `/confine status` note agrees; `/confine off` → the word becomes `unconfined` in the error
  tone (`StyleRuns`), `/confine on` → back in the mode colour; `/settings` `mode` row reads
  `auto`, `confine-to-workspace` reads the live value with `use /confine`; esc, Shift-Tab, reopen
  → `plan` (golden `t16-settings-rows.txt`); watcher seam (A8): `filewatch.New(path)` at
  `wire_live.go:257` runs on the package defaults (1 s poll + 250 ms settle, `filewatch.go:37,43`)
  and the `Options{Interval, Settle}` form already exists — add `var configWatchTiming =
  filewatch.Options{}` (zero = defaults) in `cmd/apogee/wire_live.go` and pass it, and let
  `launchTUI` (item 5) set `{Interval: 50ms, Settle: 50ms}` for every driver launch (restored in
  `t.Cleanup`), so a watcher step costs ~0.1 s, not ≥ 1.3 s; write two live keys (`ui.color-scheme: light`,
  `auto-compact`) into the home's `config.yaml` from the test → exactly ONE transcript line
  `config changed on disk — applied: <paths>`; the rows carry ` ~`; editing one in the pane flips
  it to ` *`. Launcher half (steps 11–12, A6): NOT a stub `tui.LauncherHost` — the rebind the
  step asserts runs through `w.mover` inside `launcherWiring` (`cmd/apogee/wire_live.go:299`),
  which a stub host would bypass. The seam is the `launcherOps` interface the wiring already
  abstracts (`launcher.go:56`; `fakeLauncher` in `launcher_test.go:19` implements it): add
  `var liveLauncherOps launcherOps = realLauncher{}` in `cmd/apogee/launcher.go` and use it at
  the `ops:` line of `wire_live.go:299`; the test swaps in a `fakeLauncher` scripted with two
  Launch profiles whose second answers with the second stubllm server's address, and restores
  it in `t.Cleanup`. `/model` lists them, picking the second shows `loading <name>…`, the
  session rebinds to the second stubllm server (its request log receives the next prompt), and a
  three-way `sub_agent` fan-out afterwards has > 1 block live at once (stubllm request log shows
  overlapping child requests).

**Files:** `cmd/apogee/e2e_outcome_test.go`, `cmd/apogee/e2e_livestate_test.go`,
`cmd/apogee/testdata/stubllm/{outcome,livestate,fanout}.yaml`,
`cmd/apogee/testdata/frames/{t15,t16}-*.txt`, `cmd/apogee/launcher.go` (`liveLauncherOps`),
`cmd/apogee/wire_live.go` (the `ops:` line and `configWatchTiming`), `cmd/apogee/e2e_support_test.go`
(`launchTUI` sets the watch timing)

**Tests:** as listed.

**Acceptance:** `go test -race -count=1 -run 'TestE2EOutcome|TestE2ELiveState' ./cmd/apogee/`
passes.

**Commit:** `test(e2e): outcome slots carry the tool's verdict and tone; footer, settings and watcher follow the live session (T-15, T-16)`

## 14. T-14 + T-18 + T-19 — real Consoles across restore, egress through an in-test proxy and MCP, the opener allow-list

**What:** the host-state, network and desktop items.

- **T-14** `cmd/apogee/e2e_console_test.go` (`!windows`): home with the console family enabled;
  a second saved session made first; PTY driver (real pids matter): `console_open sleep 987` →
  `pgrep`-equivalent via `/proc`/`ps` shows one child of the apogee pid; a delegation opening
  `sleep 654` and reading console 1 → `no console 1 (open consoles: 2)`; after the delegation
  `sleep 654` is gone and `sleep 987` stands; `/sessions` → restore the other → `sleep 987` is
  gone; `console_read 1` → `no console 1 (open consoles: none)`; the refused mid-Exchange restore
  (a `Hang` turn keeps the Exchange open) leaves `sleep 987` running; `Quit()` reaps everything.
  An engine-level twin (`internal/agent` restore through the public API) is already green — do
  not duplicate it.
- **T-18** `cmd/apogee/e2e_egress_test.go`: an in-test forward proxy (lift
  `internal/mcp/transport_test.go:263`'s into `internal/tuitest/netfix.go` as
  `ForwardProxy(t)` with an access log, plus `MCPEcho(t)` — a streamable-http MCP server
  exposing one `echo` tool, built on the `:334` fixture) and a page server. Steps as the
  checklist: `HTTP(S)_PROXY` + `NO_PROXY` for the stub → `web_fetch` of the page goes through
  the proxy log; `http://127.0.0.1:9/` refused by the SSRF floor with NO proxy entry; `NO_PROXY`
  incl. the page host → no new proxy line; `mcp-servers:` entry → `docs__echo` tool present and
  the proxy log shows the MCP connect; `url-safety.deny-hosts` with that host → startup fails
  with `mcp: server "docs" endpoint blocked by url-safety`; live re-read: edit the row in
  `/settings` → the network tools refuse the host; a `name:` change in the file → reconnect
  refused with the live list; a `308` stub turn → `upstream HTTP 308` error, no follow (assert
  the redirect target got no request); the long stream: a 25 s `TokenDelay`-paced turn
  completes with no deadline error (this one test may exceed the per-test norm; keep it under
  30 s and note it in the budget line of the design doc). Environment variables are set with
  `t.Setenv` — the in-process driver runs in the test process, so the proxy env reaches the
  real transport.
- **T-19** `cmd/apogee/e2e_present_test.go`: fixtures `report.md chart.png notes.pdf sheet.csv
  deck.odp text.odt book.epub page.html`; opener seam (A5): the `present.Opener` is built by
  `presentationRungs` (`cmd/apogee/wire_present.go:46`) and installed into the TOOL layer through
  `livePresentation.install`, so the launcher closure never sees it — add `var openerLookPath
  func(string) (string, error)` (nil = `exec.LookPath`) in `cmd/apogee/wire_present.go`, set as
  the Opener's `LookPath` (`internal/present/opener.go:93`) in `presentationRungs`; the test
  points it at a script that appends its argv to a log and restores it in `t.Cleanup`.
  `DISPLAY` set so rung 1 is
  eligible on Linux; `present_document` on each → `.md/.png/.pdf/.csv` logged once with the
  `opened on the user's machine` wording, `.odt/.odp/.epub/.html` NOT logged with `the path is
  shown in the transcript for the user to open`, never a tool error; the served rung (force rung
  2 by making `LookPath` fail) → the transcript shows an `http://…/<token>` URL while the tool
  RESULT (session record) carries neither `http://` nor the token; reopen from `/sessions` → no
  live token URL in the replay.

**Files:** `cmd/apogee/e2e_console_test.go`, `cmd/apogee/e2e_egress_test.go`,
`cmd/apogee/e2e_present_test.go`, `internal/tuitest/netfix.go`, `cmd/apogee/wire_present.go`
(`openerLookPath`),
`cmd/apogee/testdata/stubllm/{console,egress,present}.yaml`

**Tests:** as listed.

**Acceptance:** `go test -race -count=1 -run 'TestE2EConsole|TestE2EEgress|TestE2EPresent'
./cmd/apogee/` passes on Linux/macOS; `TestE2EConsole` skips on Windows.

**Commit:** `test(e2e): consoles die with their owner, egress obeys proxy and url-safety live, the opener allow-list (T-14, T-18, T-19)`

## 15. T-11 + T-21 + T-22 + T-23 — CI matrix, release smoke, docs drift, the newcomer container

**What:** the infra proxies of ratified call 9.

- **T-11** `.github/workflows/ci.yml`: a `landlock-abi-1-2` job on `ubuntu-22.04` (kernel
  5.15, ABI 1–2) running `go test -run 'TestLandlockProbe$|TestLandlockResidualsMatchHostABI|
  TestLandlockCapabilitiesHonest' ./internal/platform/` plus `go run ./cmd/apogee probe | grep
  -q 'unfenced: truncate(2)'` and a `--mode auto` start whose stderr carries the residual notice
  (headless, unreachable endpoint is fine); same SHA-pinned actions as the check job. The
  existing `ubuntu-latest` job gains the negative: `probe` output must NOT contain `unfenced:`.
  Step 8 (empty `tool_calls:[{}]` placeholder): `cmd/apogee/probemodel_test.go` gains a stubllm
  turn with `ToolCalls: [{}]` and asserts the "none with a name and an id" finding and no tier
  raise.
- **T-21**: an `actionlint` step in the check job (pinned action) and a `scripts/check-pins.sh`
  asserting every `uses:` is `@<40-hex> # vX.Y.Z`, run from `make check`; `make release-smoke`
  (`scripts/release-smoke.sh`): given `VERSION=` it downloads the six archives for that release,
  verifies `SHA256SUMS`, runs the host's binary `--version`, and — when `brew` is on PATH —
  `brew update && brew upgrade apogee && apogee --version` expecting the version; documented in
  the memory-held release procedure's repo counterpart `docs/manual/building.md` "Releasing"
  (add the subsection if absent). Steps 1–5, 7, 9 stay as the already-green greps/`gh` checks
  the agent ran — encode steps 5 and 9 as `scripts/release-smoke.sh` pre-checks.
- **T-22**: nothing new beyond item 7's judge gate joining `make live-eval`; add
  `TestLiveEvalNeverTouchesTheRealHome` — not a test but a `make live-eval` post-step that
  counts `~/.apogee/sessions` and `~/.apogee/scratch` before/after and fails on growth.
- **T-23** `cmd/apogee/docs_env_test.go`: `TestManualListsEveryEnvironmentOverride` — every
  `APOGEE_[A-Z_]+` read in `cmd/apogee` (grep the source via `go/ast` or a scripted grep) appears
  in `docs/manual/configuration.md` "Environment overrides" and vice versa (the tool-name twin
  already exists); `APOGEE_MODE=fast` / `APOGEE_BYPASS=maybe` start with an error naming variable
  and value (headless); `--tui-trace`/`--tui-diag` write files and are absent from `--help`;
  `APOGEE_CONFIG`/`APOGEE_WORKSPACE` roots (in-process driver, assert record location and a
  refused out-of-workspace read); the url-safety prose grep in manual + seeded template. The
  container: `cmd/apogee/e2e_newcomer_test.go` `TestNewcomerFollowsTheDocs` — skips unless
  `docker` is on PATH and `judge.Enabled()`; builds the archive for the host platform (`make
  dist` for one target), starts `stubllm serve` on the host, runs a container (`golang:1.26`
  image is NOT needed — use `debian:stable-slim`) with `--network host`, a copy of ONLY
  `README.md` + `docs/manual/`, the archive, and a driver script that asks the JUDGE model (via
  `internal/judge`'s client, tool-use loop of ≤ 20 steps with a `run` tool executing inside the
  container through `docker exec`) to "install apogee from the archive path and reach a working
  session against <stub URL> using only these docs; report every step that did not work as
  written"; the verdict is judged against T-23's oracle; the finding "the example pins
  `VERSION=0.15.0`" already recorded is the kind of output expected. Steps 1 (Homebrew) and 4
  (OpenRouter key) are recorded in the design doc's table as not observable without a release /
  a key.

**Files:** `.github/workflows/ci.yml`, `scripts/check-pins.sh`, `scripts/release-smoke.sh`,
`Makefile`, `docs/manual/building.md`, `cmd/apogee/probemodel_test.go`,
`cmd/apogee/docs_env_test.go`, `cmd/apogee/e2e_newcomer_test.go`,
`cmd/apogee/testdata/stubllm/{placeholder-toolcall,newcomer}.yaml`

**Tests:** as listed; `actionlint` and `check-pins.sh` run in `make check`.

**Acceptance:** `go test -race -count=1 -run 'TestManualListsEveryEnvironmentOverride|TestProbeModel.*Placeholder|TestDocsEnv' ./cmd/apogee/`
passes; `scripts/check-pins.sh` exits 0; `actionlint .github/workflows/*.yml` exits 0 (install
via `go run github.com/rhysd/actionlint/cmd/actionlint@<pinned>` in `make check`);
`TestNewcomerFollowsTheDocs` skips cleanly without docker.

**Commit:** `test(ci): landlock ABI 1–2 job, pinned-action lint, release smoke, env-override drift test, newcomer container (T-11, T-21, T-22, T-23)`

## 16. Closeout — checklist reconciliation, ISSUES.md residue, design-doc final, `make check`

**What:** land the paperwork and prove the budget.

- Re-run the `test-checklist` skill's record mode on `docs/test-checklists/2026-08-27 - 00 -
  since-v0.17.1.md` for every T-item this plan automated, citing the test name per item
  (`record: T-24 pass: TestE2EStreamCommitsCompleteAndInOrder …`); items whose owner-tested boxes
  (T-03, T-04, T-06) are untouched by this plan are left for the owner. Add a one-paragraph
  "Automated since" note at the top of the checklist naming this plan.
- `docs/design/test-drivers.md`: final pass — every example test name resolves (`go test -list
  'TestE2E.*' ./cmd/apogee/` is the source), the budget line states the measured e2e wall clock,
  the "Not observable" rows are exactly: font tofu (T-20), felt flicker (T-24, proxied by the
  repaint ceiling), real desktop applications (T-19), `brew upgrade` before a release and the
  Homebrew/OpenRouter newcomer steps (T-21/T-23).
- ISSUES.md: add a short "Test drivers — residue" section listing those four not-observable
  rows as accepted proxies (no open work), and remove any line that this plan closed.
- CHANGELOG `[Unreleased]`: the application items (9–15) as one Changed/Added entry naming the
  e2e set and the CI jobs; item 8's kit entry stays.
- Timing proof: `go test -race -count=1 -run 'TestE2E' ./cmd/apogee/ -v 2>&1 | grep -E '^(---|ok)'`
  captured into the design doc's budget line; if the set exceeds 15 s, the slowest test is
  moved behind `APOGEE_E2E_SLOW=1` with a NOTES line, never deleted.
- `make check` (once, here — the multi-item run's single full gate).

**Files:** `docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md`,
`docs/design/test-drivers.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `make check` green; the e2e timing capture.

**Acceptance:** `make check` exits 0; `grep -c "TestE2E" "docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md"`
≥ 12; `grep -q "Test drivers — residue" ISSUES.md`.

**Commit:** `docs(test-checklists): record the automated items; test-drivers residue; changelog`

---

**Closing note:** no item changes `VERSION`, a CHANGELOG release heading, or a tag. At closeout the
implementer emits a `VERSION-SUGGESTION` line (a micro-bump is warranted: a new dev binary
`cmd/stubllm`, new CI jobs, and `make check` gaining actionlint and the pin check are shipped
changes); the owner decides.
