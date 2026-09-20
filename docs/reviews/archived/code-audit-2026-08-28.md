# Code Review — apogee core (cmd, config, judge, skills, stubllm, tui, tuitest, infra) — 2026-08-28

**Scope:** 123 files across `cmd/apogee`, `cmd/stubllm`, `internal/config`, `internal/judge`, `internal/skills`, `internal/stubllm`, `internal/tui`, `internal/tuitest`, and repo infra (`.github/workflows/ci.yml`, `.gitignore`, `Makefile`, `VERSION`, `scripts/`). Reviewed by intent/structure, correctness, security, critical-path-tests, and concurrency lenses; machine checks (`go vet ./...` clean, `go test -cover`) folded in. The critical-path-tests lens did not produce `internal/tui-code` findings this run (`findings-4-internal-tui-code.md` missing) — test-side coverage of that package comes from the other lenses only.*
**Mission:** A terminal AI coding agent (Go, Bubble Tea TUI) for smaller locally-hosted LLMs whose structural bet is an embeddable Go engine: TUI, bench, `headless` and `daemon` are all Drivers over one engine, gated Mechanisms A/B-tested per model, with Bypass mode as the never-worse off floor.
**Files reviewed:** 123

## Executive Summary

The most serious findings sit on the operational edges, not the engine loop: the live-judge gate can reject a valid model verdict over a stray brace in prose and can send judge prompts unauthenticated through a raw key read; config validation silently accepts whitespace-padded server names and endpoints that then fail byte-for-byte selection and dialing; and `make check` and CI can run different actionlints. Verification confirmed 16 of the candidate findings and refuted 4 (double identity notices, fail-open `mode:` parsing, a footer render-path data race, and a shipped-renderer crash on live shrink — the mode refusal is pinned by `TestRunRootInvalidMode` and the engine seam is lock-guarded). Six candidates dropped from High to Medium on verification, leaving 6 High and 14 Medium findings in a generally healthy codebase whose core Bypass and concurrency invariants held; `go vet ./...` ran clean. The highest-leverage work is the judge gate, the config-validation gaps, and the egress test proxy, whose dial-any fallback contradicts its own "bytes never leave the machine" guarantee.

## Intent & Architecture Findings

### Medium — the judge keys its requests through a raw env read, bypassing the KeyResolver seam ADR 0047 mandates `[Intent]` `[Correctness]`

- **Where:** `internal/judge/judge.go:92-93, 177-178`
- **What:** The judge client is built via `provider.WithAPIKey(os.Getenv(apiKeyEnv))` — a raw, untrimmed read with no per-entry `KeyResolver` seam and no emptiness refusal — while every session seam keys through `config.KeyResolver.Resolve`, whose env source hard-errors on unset or empty variables. `setAuth` omits the `Authorization` header whenever the value is empty, so an unset or stale key sends judge prompts to the remote endpoint unauthenticated. *(independently verified)*
- **Why it matters:** ADR 0047 point 5 mandates a hard error for keyless operation ("Keyless is spelled by naming no source at all"); the judge names a source implicitly and gets a silent degraded pass — typically a 401'd or garbled judge at the live release gate (trigger is narrow: `APOGEE_API_KEY` exported while the judge endpoint is live).
- **Fix:** Route the judge through the same `KeyResolver` seam with the env source's empty-value hard error, and refuse to start the judge when the resolved key is empty.

### Medium — `/skills` can report two catalog snapshots as one `[Correctness]` `[Concurrency]`

- **Where:** `internal/skills/provider.go:105-116`, report splice at `internal/tui/skills.go:86-87`
- **What:** Each accessor (`List`, `Skipped`, `Suggest`, `Get`, `ResolveSkills`) loads the catalog pointer independently via `p.cur.Load()`; `noteSkillCatalog` makes two separate calls (`List()` then `Skipped()`). A `Reload` landing between the two swaps the pointer mid-report, so a just-fixed skill shows up both loaded and skipped. The provider doc claims these "read the SAME snapshot List does," but nothing enforces it. *(independently verified)*
- **Why it matters:** The documented "one scan's answer" / "failures ride with the loaded skills" Library guarantee is violated — a mixed `/skills` report the user and the model both act on.
- **Fix:** Capture `p.cur.Load()` once per report and serve all accessors from that single snapshot, or provide one combined accessor.

## Critical & High Findings

### High — the live judge gate rejects valid model verdicts on a stray brace in prose `[Correctness]`

- **Where:** `internal/judge/judge.go:337-349`
- **What:** `firstJSONObject` anchors on the first `{` anywhere in the reply and balances the span from there, instead of the first brace that opens a decodable JSON object. A chatty reply quoting shell output (e.g. `…{wrote 3 files}… {"verdict":"fail"}…`) yields the prose span, `json.Unmarshal` fails, and the judge returns a "decode the judge's verdict" error — the valid verdict later in the reply is rejected. The only prose-tolerance test wraps the object in a backtick fence containing no `{`, so the stray-brace path is untested. *(independently verified)*
- **Why it matters:** A valid verdict rejected at the live release gate fails the run on a model that actually answered — the exact false negative the gate exists to avoid.
- **Fix:** Try decoding from each `{` in order (or scan for the first brace that opens an object `json.Unmarshal` accepts), instead of trusting the first brace.

### High — `make check` can run a different actionlint than CI, defeating the documented "cannot drift" invariant `[Infra]`

- **Where:** `Makefile:47` (used at :244-246), `.github/workflows/ci.yml:36`
- **What:** `ACTIONLINT = $(shell command -v actionlint 2>/dev/null || echo 'go run …actionlint@v1.7.12')` short-circuits to whatever `actionlint` is on `PATH` with no version check, while CI always runs the pinned `go run …@v1.7.12`. The Makefile's own "cannot drift" comment is defeated by the fallback it documents as intended (offline/pre-commit case). *(independently verified)*
- **Why it matters:** `make check` and CI can pass/fail differently on the same tree, so the local acceptance gate is not the CI gate.
- **Fix:** Pin one source of truth — always `go run …actionlint@v1.7.12` (or version-check the `PATH` binary against a pinned minimum) so both gates run the same actionlint.

### High — config validation passes whitespace-padded server names, then byte-for-byte matching makes them unselectable `[Correctness]`

- **Where:** `internal/config/config.go:948` (`ValidateServers` refuses only `TrimSpace(name)==""`; selection at :2820-2831, :357, :333; `HostAlias` :298)
- **What:** A padded name (`name: " box "`) passes validation and dedup, while startup selection, `/server`, and footer matching compare `e.Name == name` byte-for-byte. A padded entry can never be selected by name, and two entries differing only in padding both pass. *(independently verified)*
- **Why it matters:** A config that validates cleanly has servers the user cannot reach — startup selection, `/server` and the footer alias all miss it, with no error pointing at the padded line.
- **Fix:** `TrimSpace` (or refuse) the name at validation time, and store/compare the canonical trimmed form everywhere.

### High — a whitespace-padded endpoint passes validation and reaches the wire with literal spaces `[Correctness]`

- **Where:** `internal/config/config.go:1461` (validate at :1435), `internal/provider/client.go:256`
- **What:** `ValidateServers` refuses only `TrimSpace(endpoint)==""`, so `endpoint: "  http://x  "` passes intact; `provider.NewClient` trims only trailing slashes, never whitespace, so chat and model-list requests dial a URL containing literal spaces and fail with a confusing parse error. Every sibling key (`api-key-cmd`, `api-key-env`, `llama-launcher`, `launch-profile`) is refused when whitespace-only. *(independently verified)*
- **Why it matters:** A paste with trailing whitespace in the config produces dial failures with no hint that the endpoint line is the problem.
- **Fix:** Trim surrounding whitespace from the endpoint when decoding (like the sibling keys) or refuse it at validation.

### High — `ReplayTrace`'s reader error branches are unpinned by any test `[Tests]`

- **Where:** `internal/tuitest/screen.go:224-238`, only test at `internal/tuitest/pty_test.go:122-146` (`//go:build !windows`, happy path only)
- **What:** `ReplayTrace` — the seam the PTY driver's "measure what a black-box run painted" claim rests on — has its only test build-tagged `!windows`, disabled in PTY-less sandboxes, and the fixture replays only valid writes; its `strconv.Unquote` and `s.Write` failure branches (screen.go:233-238) are fed by no test anywhere, and truncated-tail replays (realistic when a killed run's trace is replayed) are unpinned. *(independently verified)*
- **Why it matters:** A trace produced by a killed run, or a reader-side format change, exercises error branches nothing in CI catches; the trace *format* is pinned on all platforms by writer-side tests, but the reader's own branches are not.
- **Fix:** Add non-`!windows` tests for the error branches (truncated tail, unquoted write) so they run everywhere.

### High — the egress-instrument half of T-18 (`ForwardProxy`) has no unit test; three documented behaviours are proven only by PTY e2e `[Tests]`

- **Where:** `internal/tuitest/netfix.go:137-155`
- **What:** `ForwardProxy` and `flushingCopy`, whose claim is that the access log is trustworthy, are referenced only from `cmd/apogee/e2e_egress_test.go` — a PTY-driven test the sandbox blocks. The 400-on-non-absolute branch (:140), the no-route dial (:75) and hop-by-hop stripping (:155) are exercised by no test anywhere, and on Windows every PTY entry point is skipped. *(independently verified)*
- **Why it matters:** This is the exact gap that lets the dial-any-host behaviour (see below) ship undetected — the unit-level proof the seam needs is absent.
- **Fix:** Add direct unit tests for the 400, no-route and hop-by-hop branches in `internal/tuitest`.

## Medium Findings

### Medium — the config watcher's zero baseline re-applies the whole config when a previously-absent file first appears `[Correctness]`

- **Where:** `cmd/apogee/wire_live.go:283` (watcher Start at :290-292), `internal/config/filewatch.go:132-136`
- **What:** When `config.yaml` does not exist at watcher `Start()` time, the baseline is a zero `fileState`; the file's first appearance is then reported as a settled change and flowed through `awaitConfigChangeOn` → `ReloadConfig`, re-resolving every changed key — re-dialing MCP servers and re-latching the launcher seam — as if the user had just edited a file nobody touched. `filewatch.go:132` states the "file appears" case is intentionally reported, and nothing refuses a zero baseline. *(independently verified)*
- **Why it matters:** First-run paths seed the config before the watcher starts, so the trigger is a deleted/wiped or externally-created config; when it fires, the user sees a full config re-apply with no edit behind it.
- **Fix:** On watcher start, treat "file absent" as a valid baseline and skip the first appearance (or emit an explicit "config file created" event instead of a settled change).

### Medium — `captureStderr` leaks the process stderr pipe and a reader goroutine if the wrapped call panics `[Correctness]`

- **Where:** `cmd/apogee/wire_test.go:76-82`
- **What:** `captureStderr` swaps the process-global `os.Stderr` and restores it only in sequential code after `f()` returns — no `defer`, not a `Cleanup`. A panic inside `f()` skips the restore: the pipe write end is never closed and the reader goroutine blocks forever in `io.Copy`. The suite does not hang (the test runner reports the panic and `TestMain`'s `os.Exit` still terminates the process), but the leaked goroutine and corrupted process `os.Stderr` persist for the rest of the run — invisible to `tuitest.CheckLeaks`. *(independently verified)*
- **Why it matters:** A panic in any captured block silently corrupts `os.Stderr` for every later test and leaks a goroutine, with no diagnostic pointing at the leak.
- **Fix:** Restore `os.Stderr` and close the pipe in a `defer` (or `t.Cleanup`) so panics restore the global before the harness sees them.

### Medium — `context-window` is validated only on the settings write path; odd spellings silently floor at load `[Correctness]` `[Intent]`

- **Where:** `internal/config/registry.go:659-666` (write-path only), `internal/config/config.go:1040, 2273-2290` (plain `yaml.Unmarshal` into `ContextWindow int`), `applyFile` :580-585
- **What:** `validateContextWindow` is registered only on the settings write seam; `LoadFileConfig` and `ResolveOptions` never check the scalar. A non-integer *text* value errors at `yaml.Unmarshal` (which every Driver refuses loudly — that half is safe), but fractional values are silently floored (3.5→3) and negatives/zero are silently discarded by `applyFile`'s `ContextWindow > 0` guard — no error, no notice, and nothing points at the offending line. *(independently verified)*
- **Why it matters:** A user who pins a fractional window gets a different window than they typed, with no notice; the mis-pin is bounded but silent.
- **Fix:** Validate the scalar at load/resolve time with the same `validateContextWindow` logic used on the write path, and refuse (or report) fractional/negative values.

### Medium — in a pre-bound session, `/clear` and `/new` report a misleading error and skip the reset `[Correctness]` `[Intent]`

- **Where:** `internal/tui/commandrun.go:159` (routes via :290→126)
- **What:** In a pre-bound session (`Options.Prebound`), `/clear` calls `m.eng.InExchange()` and on failure `m.eng.ClearContext()` against lateEngine methods that return `errNoServerBound`; the resulting note — "could not clear context: no server is bound yet — choose one with /server" — names an action the user never asked for, and the function returns before `transcript.reset()`, so the view is untouched. *(independently verified)*
- **Why it matters:** The corner-case pre-bound startup state (user Escs the auto-opened server picker, then presses `/clear`) yields a UX misfire — no crash, no data loss, but a wrong instruction with no effect.
- **Fix:** Detect the no-server-bind case before the engine calls and answer directly ("pick a server first"), or fall back to a view-only reset the pre-bound state supports.

### Medium — eight of twelve F-key constants are pinned by no test; the "parser change breaks the pin" claim is false for them `[Tests]` `[Intent]`

- **Where:** `internal/tuitest/keys.go:41`, pin test at `internal/tuitest/driver_test.go:135-138`
- **What:** The pin test exercises only F1, F4, F5 and F12; F2, F3, F6, F7, F8, F9, F10, F11 appear only as definitions with zero callers, so an escape-table flip in those eight sails through the suite while the file claims a parser change "breaks the pin". *(independently verified)*
- **Why it matters:** Each unpinned entry drifts the pin claim for the whole file; today the impact is a lying doc comment, but the next key that gains a caller inherits an unpinned escape.
- **Fix:** Pin all twelve keys in the test (or drop the unused constants and the claim).

### Medium — the skills BM25 index is built from the full, uncapped description — a DoS from one hostile clone `[Security]`

- **Where:** `internal/skills/suggest.go:319` (`buildIndex` over `firstNonEmpty(s.Description, s.Summary)`), `internal/skills/load.go:330-337, 438`
- **What:** The index is built from the un-clamped `Description` with only the 1 MiB whole-file `maxSkillFileBytes` as floor; `maxSkills` (1024) × 1 MiB lets one clone feed `finalize` up to ~1 GiB of term text as per-skill `map[string]int` bags, and `finalize` runs on every `Load`/`Reload`.
- **Why it matters:** A hostile repo's bundled skills directory yields a denial of service (minutes of CPU, gigabytes of allocations) over the TUI/loop seam until the dir is removed — the realistic "malicious repo" attacker of the threat model, triggered with no model involvement.
- **Fix:** Cap description length (like `maxSummaryLen`) before it enters the index, and/or cap total corpus size consumed by `buildIndex`.

### Medium — the egress test proxy can dial real hosts, contradicting its own "bytes never leave the machine" invariant `[Security]`

- **Where:** `internal/tuitest/netfix.go:73-79, 138-153` (invariant stated at :9-11)
- **What:** `ForwardProxy`'s `DialContext` forwards any absolute request whose host is not in the caller's route table to its real destination (addr passed through unchanged), and `serve` accepts any absolute-URI forward request from any loopback client — contradicting the top-of-file guarantee that nothing here reaches anything outside this process.
- **Why it matters:** During PTY-driven egress tests the driven apogee runs with `HTTP_PROXY` at the proxy URL; a hostile repo/model can steer the run's web tool at any public host not in the route table and the proxy dials it — exfiltrating run data to the real internet from inside a test asserting egress is safe. (Same surface as the ForwardProxy test gap above; that gap is why this ships undetected.)
- **Fix:** In `DialContext`, return an error when `routes[host]` is absent (require every host explicitly mapped), keeping the loopback shim the only dial target.

### Medium — a workspace file name containing a newline or tab forges extra dropdown rows `[Security]` `[Intent]`

- **Where:** `internal/tui/autocomplete.go:658-659`, `internal/sanitize/sanitize.go:29-32` (`stripEscapes` keeps `\n`/`\t`), `internal/tui/popup.go:576-599`
- **What:** `fileSuggestions` strips escapes but deliberately keeps `\n`/`\t`, and the spliced file-ref value carries those bytes into the composed message, where `@`-ref re-parse can't recognise the path. The dropdown renders such a name as additional rows — a row that does other than it paints. Other dropdown producers already flatten via `flattenField`/`stripEscapes`.
- **Why it matters:** A crafted file name in a hostile repo makes one menu entry paint as several and insert bytes the parser can't resolve — multi-row spoofing plus a broken pick in the hostile-repo threat model.
- **Fix:** Derive both the display cell and the inserted value from a single `flattenField(stripEscapes(p))` (or `StripEscapesToLine`) so a row is exactly one cell and the value shown is exactly what inserts.

### Medium — an unreadable skills-dir entry is silently dropped, indistinguishable from a missing skill `[Security]`

- **Where:** `internal/skills/load.go:209-211`
- **What:** The `fs.WalkDir` callback returns `nil` on every `walkErr != nil`, so an unreadable walk entry is dropped with no `SkipError` — a "broken" skill is silently indistinguishable from a missing one, the exact failure the package's own "soft must not mean silent" rule exists to prevent. A hostile repo shipping an unreadable folder/marker file produces no `/skills` entry and no reason string.
- **Why it matters:** The `/skills` report lies by omission: a scan that failed partway looks identical to one that found nothing, blocking diagnosis and letting hidden content ride along unannounced.
- **Fix:** Before returning `nil` on `walkErr`, record `cat.addSkip(SkipError{Path: …, Err: walkErr})` (skipping the root `p=="."` case) so unreadable directories are reported alongside malformed files.

### Medium — the live judge runs as root in the host network namespace in the newcomer e2e container `[Security]`

- **Where:** `cmd/apogee/e2e_newcomer_test.go:160, 259`
- **What:** `newcomerExec` runs the LLM-supplied command verbatim as `sh -lc` via `docker exec` against a container launched with `--network host`, with no user or network isolation. The judge is root inside the host network namespace and can reach every host loopback listener; `/kit` is read-only but readable; no seccomp or netns guard exists. *(independently verified)*
- **Why it matters:** A hostile judge model gets root-in-container with host-network reach and read access to the mounted kit — a data-exposure path in an env-gated local test whose model is operator-trusted (impact bounded, but the guard gap is real).
- **Fix:** Run the newcomer exec as a non-root user with a private `--network` (or `--network none`) and only the kit volume mounted read-only.

### Medium — the release smoke gate verifies published assets against checksums fetched from the release itself `[Infra]` `[Security]`

- **Where:** `scripts/release-smoke.sh:93` (local check :93-122, remote check :105-122)
- **What:** The two checks are self-contained and never compared: the local archives are verified against the locally built `dist/SHA256SUMS`, and the downloaded assets against the `SHA256SUMS` fetched from the same release channel. No step diffs `dist/SHA256SUMS` against the downloaded file, so a release whose binaries and checksums were both replaced consistently passes. *(independently verified)*
- **Why it matters:** The smoke proves transport integrity but not that the published binary matches what the released tree builds — a tampered-but-self-consistent release sails through.
- **Fix:** After the remote verification, diff the downloaded `SHA256SUMS` against the locally built `dist/SHA256SUMS` (or verify the remote assets against the local file directly).

### Medium — after a `/settings` url-safety edit, network tools and the MCP connection disagree about which hosts are allowed `[Security]`

- **Where:** `cmd/apogee/wire_live.go:96` (guard built at startup), `internal/tui/wire_settings.go:1258-1276, 1574-1582`
- **What:** The MCP egress guard is dialed once under the startup snapshot's host lists; `applyURLSafetyHosts` only rebuilds the tool set (`setDenyHosts`/`setAllowHosts`), and `mcp.Connect` re-runs only on an `mcp-servers:` edit. Between a url-safety edit and the next reconnect, network tools carry the new lists while the MCP connection keeps its original vetting — the code's own "ever disagreeing" comment (wire_live.go:92-95) is false in that window. *(independently verified)*
- **Why it matters:** An operator tightening `url-deny-hosts` mid-session keeps the MCP connection dialing hosts the new list forbids, until the next reconnect — a real gap, but triggered by an operator `/settings` edit, not a model-controlled bypass.
- **Fix:** On a url-safety change, re-apply the guard to the live MCP connection (not just the tool set) at the same seam that rebuilds the tools.

## Recommended Action Order

1. Judge gate: stray-brace decoding plus the KeyResolver/empty-key refusal (`internal/judge`) — both break the live release gate.
2. Config validation: padded names/endpoints and load-time `context-window` validation (`internal/config`) — silent mis-configuration with no error.
3. Egress proxy dial-any host (`internal/tuitest/netfix.go`) — the one finding with a real data-exfiltration consequence; pair with the ForwardProxy unit tests.
4. Skills: BM25 description cap and unreadable-entry `SkipError` (`internal/skills`) — hostile-repo DoS and the Library guarantee.
5. Makefile/CI actionlint pin (`Makefile` + `ci.yml`).
6. Watcher zero baseline (`cmd/apogee/wire_live.go`), `captureStderr` defer (`cmd/apogee/wire_test.go`), pre-bound `/clear` wording (`internal/tui/commandrun.go`).
7. Test seams: `ReplayTrace` error branches, the eight unpinned F-keys, and a same-snapshot `/skills` test (`internal/tuitest`, `internal/skills`).
8. e2e/infra: newcomer container isolation, release-smoke checksum cross-check, MCP url-safety re-apply (`cmd/apogee/e2e_newcomer_test.go`, `scripts/release-smoke.sh`, `internal/tui/wire_settings.go`).

No finding was marked a candidate for `/improve-codebase-architecture`.

## What Looked Good

The Bypass floor held up under five lenses: mode grammar is validated end-to-end — both Drivers refuse `mode: garbage` before the launcher runs, pinned by `TestRunRootInvalidMode`, so the claimed silent fail-open does not occur — and the engine/TUI seam, the run's headline concurrency risk, is lock-guarded: both footer engine reads take their RWMutex, and the one unsynchronized read is provably idle-only. The mid-stream resize panic that the tests lens raised was already fixed and pinned inside the test emulator, and the shipped renderer's path was traced to native terminal clamping rather than an unclamped Go index. The KeyResolver seam and per-entry key sources (ADR 0047) are applied consistently across the product path, `go vet ./...` is clean, three engine-side packages carry 90-97% coverage, and the e2e suite's scripted-upstream design (`internal/stubllm`) keeps tests deterministic and correctly env-gated. `filewatch` even documents the "file appears" case this review argues needs a guard — a codebase that writes its invariants down where it can.