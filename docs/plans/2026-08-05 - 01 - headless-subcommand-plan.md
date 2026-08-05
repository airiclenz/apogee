# Plan: the `apogee headless` subcommand

- **Goal:** ship `apogee headless` — one prompt, one unattended run over `internal/run.Once`, printed to stdout with meaningful exit codes. The tripwire ADR 0031 names as the first mechanical consequence of the north star: once it exists, TUI-welded capabilities break visibly.
- **Date:** 2026-08-05 · **Status:** not started
- **Authoritative sources:**
  - `docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md` — decision 6 (`internal/run` is the shared core) and the consequence "`apogee headless` shrinks to a thin CLI over `internal/run` … argument parsing and exit codes, not a second runner".
  - `docs/adr/0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md` — headless precedes the daemon as the tripwire.
  - `internal/run/doc.go` + `internal/run/run.go` — the runner's contract (`Spec`, `Result`, `ErrMode`; denier Approver, nil Asker/Presenter and the event tap are composed **inside** `Once` — the caller passes none of them).
  - `cmd/apogee/probe.go` / `cmd/apogee/probemodel.go` — the subcommand pattern (local `options`, `applyConfig`, `resolveRoots`, product→stdout / notices→stderr).
  - `cmd/apogee/schedule.go` — `scheduleWiring.fire` (the per-model Config composition via `rebindSpecFor` to copy) and `scheduleAutoBlocked` (the caller-side Auto gate to mirror).
- **Ratified design calls** (owner, 2026-08-05, this grill session):
  1. Scope is headless only — the daemon is its own future plan against ADR 0034.
  2. Prompt source: positional argument; with no argument the prompt is read from stdin; empty from both is a usage error.
  3. Mode: `--mode` accepts `plan` and `auto` only; default `plan`.
  4. Exit codes: `0` run completed · `1` run started but failed (model/tool error, cancellation, save failure) · `2` never started (usage, config, refused mode) — apogee's first distinct-exit-code convention, introduced deliberately here.
  5. Persistence: save to the shared sessions store by default; `--no-save` opts out.
  - (Posture is not a call for this plan: `run.Once` already imposes the Firing posture — Plan/Auto only, denier, nil delegates — per ADR 0033/0034.)
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item text lands as a dated NOTES line under the item. Line numbers cited below are hints as of 2026-08-05 — concurrent TUI work is in flight, so locate by the quoted phrase/symbol, not the line.
- **Out of scope:** the daemon and everything in ADR 0034 beyond headless; MCP tools in headless runs; a `--timeout` flag; Ask-Before/Allow-Edits modes; carry-over context between runs; any VERSION/CHANGELOG-heading/tag change (see the closing note); TUI changes.

## 1. The headless command — plan-mode core — ✅ DONE (2026-08-05)

NOTES (2026-08-05): `--api-key` was NOT added. The codebase refuses that flag deliberately and in four places (`cmd/apogee/root.go` options doc, `cmd/apogee/config.go` layer + api-key comments, the root's `Long` help, `probemodel.go`): "a secret on the command line lands in shell history and in `ps` output", and `flagLayer` carries no api-key by construction. Headless still authenticates exactly as a session does — `opts.apiKey` is resolved by `applyConfig` from `APOGEE_API_KEY` > `api-key:` and reaches `Config.APIKey` — so no capability is lost. Owner call needed if headless should be the exception that breaks that decision.

NOTES (2026-08-05): mode precedence is flag > env > file > **plan**, with one collapse. `applyConfig`'s bottom layer is the interactive ladder's `ask-before` (`resolveSettings`), so an unset `--mode` would otherwise resolve to a mode headless refuses — the bare `apogee headless "..."` would fail on every host that has not spelled a mode out. `runHeadless` therefore rewrites an *unflagged* resolved `ask-before` to `plan`, which also collapses an explicit `mode: ask-before` in config.yaml (indistinguishable from the default post-resolution) into plan rather than a refusal. An explicit `--mode ask-before` still refuses, loudly.

NOTES (2026-08-05): the answer is printed with `fmt.Fprintln(cmd.OutOrStdout(), text)`, NOT the item's literal `cmd.Println`. Cobra's entire `Print`/`Printf`/`Println` family resolves to `OutOrStderr()`, so `cmd.Println` would put the model's answer on **stderr** in every real invocation — the fallback only differs once a caller has wired an out writer, i.e. in tests, which is exactly how the first attempt's `cmd.SetOut(&outBuf)` hid the defect. `PrintErrln` does target the err stream and is left in place for notices/summaries. `TestHeadlessAnswerLandsOnTheProcessStdout` guards the split over the process's real `os.Stdout`/`os.Stderr` with no out writer wired. Out of scope here: `probe.go`/`probemodel.go` carry the same latent mistake — a run-level follow-up, not this item's.

NOTES (2026-08-05): the acceptance line `printf '' | go run ./cmd/apogee headless; test $? -eq 2` cannot pass as written — `go run` swallows the child's status, printing `exit status 2` and itself exiting 1. Verified against the built binary instead: `go build -o /tmp/apogee ./cmd/apogee && printf '' | /tmp/apogee headless; echo $?` ⇒ `2`.

NOTES (2026-08-05): exit 2 reaches two refusal classes the item's text implies but its named mechanism cannot carry. (a) Cobra validates flags and the argument count BEFORE `RunE`, so `--bogus` and an unquoted multi-word prompt never pass through `runHeadless` and exited 1; `newHeadlessCommand` now sets `SetFlagErrorFunc` and wraps `cobra.MaximumNArgs(1)` in `headlessArgs`, both returning `notStarted`. (b) The never-started branch after `runOnce` no longer keys on `apogee.ErrAutoUnavailable` alone — that left `Config.Endpoint is required` (the fresh-host case) at exit 1 *and* printed a "turns: 0" summary for a run that never happened. It now keys on the structural signal `run.Once` actually gives: the zero Result (`res.Turns == 0`) from each of its three pre-run exits, versus the ≥1 Turn every driven run carries. `friendlyConstructErr` still frames the Auto case and passes the rest through. Verified on the built binary: `headless a b`, `headless --bogus x`, `headless --config <fresh> "hi"`, `printf '' | headless` ⇒ all `2`, with empty stdout and no summary line.

**What:** New `cmd/apogee/headless.go` (+ `headless_test.go`) with `newHeadlessCommand()`, registered in `subcommands()` (`cmd/apogee/subcommands.go`). Follow the probe pattern exactly: local `options`, `cobra.Command{Use: "headless [prompt]", Args: cobra.MaximumNArgs(1), SilenceUsage: true, SilenceErrors: true, RunE: …}`, flags declared with plain pflag vars.

- **Flags:** `--config`, `--workspace`, `--endpoint`, `--model`, `--api-key`, `--mode` (default `plan`), `--no-save`. All except `--no-save` feed `applyConfig` (flag > env > file > default; the existing `APOGEE_*` env names apply unchanged). Bypass, mechanisms, system prompt, context files etc. flow from config file/env through `opts` — no dedicated flags for them.
- **Prompt resolution:** the positional argument; when absent, read all of stdin. Trim; if the result is empty, usage error (exit 2, message to stderr).
- **Mode:** parse with `parseMode` (`cmd/apogee/wire.go`, "parseMode"). `ask-before`/`allow-edits` are refused before any composition with a one-line message naming plan|auto (exit 2); `run.Once`'s `ErrMode` stays as backstop only. The Auto eligibility gate is item 2 — in this item `--mode auto` composes and runs without the caller-side gate.
- **Composition** (copy `scheduleWiring.fire`, `cmd/apogee/schedule.go`): `applyConfig` → `resolveRoots(opts.configDir, opts.workspace)` → `platform.NewConfiner()` with deferred `Close()` where provided → `rebindSpecFor(opts, roots, …, model, 0, …)` for the per-model system prompt / validated set / enable list / window → build the `apogee.Config` with the same fields `runRoot` sets minus TUI-only concerns; `Events`, `Approver`, `Asker`, `Presenter` are left nil (`Once` composes its own). No MCP tools (Firing posture, ADR 0034). Store: `session.NewStore(roots.sessions)` unless `--no-save` (then `Spec.Store` nil). `Spec.ScheduleID`/`ScheduleName` empty; `Title` empty (derived).
- **Signals:** wrap the command context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`; a cancelled run flows out of `Once` as a run failure (exit 1).
- **Output contract:** `Result.FinalText`, escape-stripped (strip ANSI/terminal escape sequences — `FinalText` is raw model output per `internal/run/run.go`), to **stdout** via `cmd.Println`. Everything else to **stderr**: resolution notices (probe pattern), and after the run one summary line `session: <id> · turns: <n> · denied: <n>` (omit `session:` segment when unsaved). On a run error with a non-empty `Result.SessionID`, stderr notes the partial record id (mirror the wording in `scheduleWiring.fire`'s partial-save wrap). On a save failure after a good run, still print `FinalText` to stdout first, then the error to stderr, exit 1.
- **Exit-code mechanism:** `main.go` exits 1 on any error today. Introduce a small unexported error type in `cmd/apogee` carrying an exit code (e.g. `exitError{code int; err error}`); `main.go`'s error path does `errors.As` and uses the carried code, defaulting to 1. RunE returns it normally so deferred teardown (confiner `Close`) still runs — never `os.Exit` inside RunE. Mapping: usage/config/composition refusals → 2 (including `apogee.ErrAutoUnavailable`, mapped through `friendlyConstructErr`); any error returned by `Once` → 1; success → 0. `Denied > 0` with a completed run is still exit 0 — denials are visible in the summary line, not the exit code.
- **Testability:** factor the RunE body around an injectable package-level runner seam `runOnce = run.Once` (the `root_test.go` launcher-stub pattern), so tests never need a live model.

**Tests:** table tests in `headless_test.go` — prompt resolution (arg / stdin / both-empty→2); mode parsing incl. refusal of ask-before/allow-edits (exit 2); exit-code mapping through the error type (stubbed runner returning success / run error / save-failure-after-success); `--no-save` ⇒ `Spec.Store == nil`; output routing (FinalText on out buffer, summary line on err buffer); escape-strip applied.

**Acceptance:** `go build ./...` · `go vet ./...` · `go test ./cmd/apogee/ -run Headless -count=1` · `printf '' | go run ./cmd/apogee headless; test $? -eq 2`

**Commit:** `feat(cli): apogee headless runs one prompt over internal/run`

## 2. The Auto eligibility gate and unattended-run notices — ✅ DONE (2026-08-05)

Depends on item 1.

NOTES (2026-08-05): `probe.DegradedNotice` is NOT printed by headless, contrary to the third bullet. Its cell and the gate's are the same cell: the notice speaks iff `mode==auto && confineToWorkspace && !FSWrite`, and the gate refuses iff `mode==auto && confineToWorkspace && !AutoEligible()` — and `AutoEligible() == FSWrite`. With the refusal placed first (as the first bullet requires) the print could never fire, and its remedies (`/confine off`, `/confine off --save`) are slash commands a headless run has nobody to type. What the TUI degrades to, this command refuses. The equivalence is pinned rather than assumed: `TestHeadlessAutoDegradedCellIsARefusalNotANotice` walks the capability matrix and asserts that wherever `DegradedNotice` would speak the command exits 2 with no runner call and no notice text on stderr — it fails loudly if the two predicates ever drift apart.

NOTES (2026-08-05): the shared sentence is `autoUnattendedBlocked(subject, backend, caps, confineToWorkspace)` in `cmd/apogee/schedule.go` (the item's "generalize the existing helper with a noun parameter" option); `scheduleAutoBlocked` is kept as the one-line wrapper passing "a firing", so the TUI's wording and its callers are byte-identical to before. Headless passes "a headless run" and frames it the way its sibling mode refusal is framed — `apogee headless: --mode auto cannot run on this host — <sentence> (use --mode plan, or run unconfined with confine-to-workspace: false …)`. The golden sentence itself is unchanged; only the prefix and the remedy clause are added, because a CLI refusal that names no way forward strands the user.

NOTES (2026-08-05): the unconfined-Auto warning is now the package const `unconfinedAutoWarning` in `cmd/apogee/wire.go`, and `runRoot` prints that const instead of its inline literal (a one-line change in the file the item names as the wording's source). Mirroring by copy would have left two literals of a security warning free to drift; two surfaces now print one string.

NOTES (2026-08-05): testability needed a second seam — `var newConfiner = platform.NewConfiner` in `headless.go`, beside `runOnce` and for the same reason: what a backend can enforce is a property of the machine the suite runs on (this container's landlock reports no fs-write, a developer laptop's seatbelt does), so without it the gate's rows would assert opposite things on different hosts. `headlessRun` installs a fenceable fake by default, which also makes item 1's existing `--mode auto` tests host-independent. Verified on the built binary: unfenceable host ⇒ exit 2 with the refusal naming the landlock backend; `confine-to-workspace: false` ⇒ the WARNING on stderr and the run proceeding.

**What:** the caller-side Auto gate for `--mode auto`, mirroring the schedule surface ("the surface that offers Auto is the one that refuses it", ADR 0033 decision 3):

- With `opts.confineToWorkspace` true and `confiner.Capabilities().AutoEligible()` false → refuse before composing the agent, exit 2. Reuse the `scheduleAutoBlocked` predicate shape (`cmd/apogee/schedule.go`, same package); the message is the same sentence with the run noun adapted: "…so auto falls back to approval — and a headless run has nobody to ask". Either generalize the existing helper with a noun parameter or add a sibling — keep one source for the shared sentence structure.
- With `opts.confineToWorkspace` false → proceed, printing the unconfined-Auto warning to stderr (mirror the `runRoot` wording, `cmd/apogee/wire.go` "unconfined").
- Print `probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), mode, opts.confineToWorkspace)` to stderr when non-empty (mirror `runRoot`).

**Tests:** table tests over fake capability sets: eligible-confined (runs, no warning), ineligible-confined (exit 2, message golden), unconfined (runs, warning on stderr), degraded-notice routing.

**Acceptance:** `go build ./...` · `go test ./cmd/apogee/ -run 'Headless.*[Aa]uto|Auto.*Headless' -count=1` (adjust the run pattern to the test names actually written, and say so in NOTES if changed)

**Commit:** `feat(cli): headless auto is gated by the eligibility ladder`

## 3. Docs sweep — headless is no longer deferred

Depends on items 1 and 2.

**What:** retire every "deferred" claim and stale tense; one owning item for all of it. Locate by phrase, not line:

- `CONTEXT.md` — **Embeddable agent** entry ("still deferred — the subcommand surface now exists and carries `apogee probe`, but `headless` is not built"): headless now built. **Driver** entry ("Two exist today — the TUI and the bench — with `apogee headless` deferred"): three Drivers exist; the daemon stays anticipated. **Firing** entry ("the deferred `apogee headless` runner"): drop "deferred".
- Code comments: `cmd/apogee/subcommands.go` ("headless stays deferred"), `cmd/apogee/main.go` (same phrase), `cmd/apogee/root_test.go` ("a stand-in for a real subcommand (probe, later headless)").
- Doc prose: `internal/run/doc.go` (the "deferred subcommand" phrasing), `apogee.go` (the two "headless" mentions — adjust tense; "the headless CLI's flags" is now literal), `internal/schedule/doc.go` ("a headless CLI's flags" — verify tense, adjust if needed).
- `README.md` — add a short `apogee headless` usage block beside how `apogee probe` is presented: one-line description, an example invocation, the prompt/stdin rule, the mode default, and the 0/1/2 exit codes.
- `CHANGELOG.md` — add the feature entry under the **current** in-progress heading, citing ADR 0033/0034. Do NOT create or alter any version heading (version policy below).
- ADRs are historical records — 0033/0034's "deferred" wording stays untouched.

**Tests:** none (docs). **Acceptance:** `grep -rn "headless" CONTEXT.md cmd/apogee/subcommands.go cmd/apogee/main.go internal/run/doc.go | grep -i "deferred"` returns nothing · `go build ./...` still green.

**Commit:** `docs: headless ships — retire the deferred wording across docs`

---

**Suggested version bump** (not performed by this plan): minor — a new user-facing subcommand and apogee's first distinct-exit-code convention warrant `v0.11.0`. Whether and when to bump is the owner's call; no item touches VERSION, CHANGELOG headings, or tags.
