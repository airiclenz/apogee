# Audit 2026-08-15 confirmed-findings plan — what survived verification of the 2026-08-11 audit

- **Goal:** close every finding of the 2026-08-11 audit that survived adversarial
  re-verification on 2026-08-15: six still-open findings from the finished security-package
  report and three survivors of the raw (never-verified) TUI findings.
- **Date:** 2026-08-15
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `docs/reviews/2026-08-11 - 00 - code-audit-internal-security.md` — the finished report
    items 1–4 close (its finding 6, `RecordBlocked` untested, was REFUTED on re-verification:
    `internal/agent/guardrails_test.go:255` proves the non-nil-audit append end-to-end).
  - The raw TUI findings under `docs/skill-runs/code-audit/2026-08-11/` (`findings-*.md`) —
    that run stopped after its find phase and produced no verified report; items 5–7 are the
    three findings a 2026-08-15 re-verification CONFIRMED. Every other raw finding was either
    refuted against in-code doc comments and tests or already fixed (see Out of scope).
  - Current-code anchors below were re-verified 2026-08-15 against HEAD (`c8a4d1c`); the
    report's own line numbers are stale.
- **Ratified design calls:**
  - Scope = all four verified groups: the two security fixes, the security test gaps +
    dead-code removal, the `settingsReset` defect, and the two small TUI items (owner,
    2026-08-15, via AskUserQuestion at plan-write time).
  - `Tier.String()` is REMOVED, not wired up — dead code on the guard path, trivially
    re-addable if a caller ever appears (owner, 2026-08-15).
  - The `settingsReset` fix signals the seam: a reset of an empty-default key reaches
    `Options.ApplySetting` with the empty value, and the dispatcher treats `""` as "the file
    no longer sets this key — resolve to the built-in default", so file and engine agree
    again (owner, 2026-08-15).
  - The broken-root sentinel is named `security.ErrRootInaccessible` and its model/human
    wording is "workspace root is not accessible" — following the report's own fix shape
    (plan author, 2026-08-15; mechanical).
  - `relativeTime` clamps a negative duration to zero and renders "just now", mirroring the
    package's own `formatElapsed` precedent for clock skew — no new wording (plan author,
    2026-08-15, following the in-repo precedent).
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item
  text lands as a dated NOTES line under the item.
- **Out of scope:**
  - The report's finding 6 (`RecordBlocked` with a real audit log) — refuted; covered by
    `TestGuardrails_CircuitBreakerTrips`. No unit-test twin is added.
  - Every refuted raw TUI finding. For the record, the 2026-08-15 verification refuted (with
    in-code evidence): the `offerableProfiles` not-running-row claim (doc at
    `picker.go:483-491` ratifies it), the pickerLoad "switch" verb and pickerHint count
    claims, the `caretFileToken` `@"` claim (pinned by `TestCaretFileTokenAtTheEnd`), the
    `foldActuationDone` unconditional-loop claim, the `ErrNoLauncher` two-path claim, the
    fileCache cold-start claim (pinned by `TestAccentRenderNeverWalksTheWorkspace`), the
    session-browser clamp claim (`selected` is a view-index, re-clamped on every filter
    edit), the `mode` live-apply claim (apply-then-mirror, `ParseMode`-validated), the
    colour-scheme validate-after-write claim (validation precedes the write;
    forgiving-scheme loading is ADR 0040), the `bindToServer` stale-`Prebound` claim, the
    `setValue` stale-scroll claim, and the `foldConfigChanged` self-write claim (watcher
    polls `config.yaml` only; self-writes baseline-refreshed, pinned by
    `TestWatchedConfigReportsNothingForApogeesOwnWrite`). The skillRegion finding was fixed
    2026-08-12 (`d3daa42`). Do not re-file any of these.
  - The remaining unverified raw findings (comment-rot and doc-count nits) — dropped by
    owner ratification 2026-08-15 given the refutation rate above.
  - Any version bump (see the closing note).

## 1. `NormalizeURL` strips every trailing dot, and the IDNA fallback gets its test — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the plan's "IDNA-failure row" landed as a table row in
`TestNormalizeURL_ProducesTheDialledForm` PLUS a dedicated `TestNormalizeURL_KeepsAHostIDNARejects`
— a table row alone cannot assert the premise the plan asks for (that the profile verifiably
rejects the input), and the row's `want` is the percent-encoded URL form rather than the raw host.

**What:** Two url-safety closures in one file. (a) In `internal/security/urlsafety.go:179-181`,
replace the single trailing-dot strip (`if len(host) > 1 && strings.HasSuffix(host, ".")`)
with a loop that strips ALL trailing dots (still guarding `len(host) > 1` so a bare `"."`
host is left alone): today `https://denied.example.com../x` normalizes to host
`denied.example.com.` whose residual dot defeats `hostMatches`
(`urlsafety.go:220-231` — neither equal to nor a `"."+entry` suffix of the deny entry),
and the dial path does not re-normalize (`internal/tools/network.go:165` rebuilds the
request from the same normalized URL), so a hostile model bypasses string-level `DenyHosts`
while DNS still resolves the host. (b) Add the missing test for the IDNA-failure fallback
at `urlsafety.go:171-177`: a non-ASCII host `idna.Lookup.ToASCII` rejects (e.g. a
non-ASCII label containing an underscore or an over-long label — pick any input the
profile verifiably rejects, asserting the rejection inside the test) must come back with
the original host unchanged — not an error, not a modified string — so guard and dialler
keep judging the same bytes. Record both under `[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/security/urlsafety.go`, `internal/security/urlsafety_test.go`,
`CHANGELOG.md`

**Tests:** In `internal/security/urlsafety_test.go`, matching the file's table style:
- multi-dot rows beside the existing single-dot rows (`:45-46`, `:91`): `example.com..`,
  `example.com...` → normalized host `example.com`; and an end-to-end guard row proving a
  `DenyHosts: ["example.com"]` guard refuses `https://example.com../x`.
- the IDNA-failure row per (b) above.

**Acceptance:** `go build ./internal/security/ && go test ./internal/security/ -run 'TestNormalizeURL|TestURLGuard' -v`

**Commit:** `fix(security): strip every trailing dot so DenyHosts matching cannot be dotted around`

## 2. A broken workspace root is `ErrRootInaccessible`, not a path escape — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the sentinel is declared in `internal/security/pathsafety.go` — literally "beside `ErrPathEscape`" per the item text — a file the item's Files list does not name.
NOTES (2026-08-15): `internal/tools/path_safety.go` got the one comment line the item asked for and no second alias: the new sentinel is matched as `security.ErrRootInaccessible` at the sites that distinguish it, since the existing alias exists only to keep legacy `ErrPathEscape` checks compiling.
NOTES (2026-08-15): the new error mode is documented once in `safeio.go`'s package header (it governs every primitive there) rather than repeated in six function doc comments.

**What:** In `internal/security`, an `os.OpenRoot(root)` failure (root deleted, permissions
changed, not a directory) is today wrapped as `fmt.Errorf("%w: %v", ErrPathEscape, err)` —
callers then render "path resolves outside the workspace" for an input path that is fine.
Introduce `ErrRootInaccessible` (sentinel beside `ErrPathEscape`, doc comment stating it
means the ROOT is unreachable, not that a path escaped; wording "workspace root is not
accessible") and wrap the OpenRoot-failure sites with it instead: `safeio.go:344`, `:372`,
`:431`, `:561` and the two `openMutationRoot` sites in `writepermit.go:80`, `:122`. Leave
every path-escape wrap (including permit-resolution failures) on `ErrPathEscape` — only the
"could not open the root at all" branch moves. Then give the new sentinel a distinct arm at
the enumerated caller sites that match `ErrPathEscape` today, rendering the root-inaccessible
wording: `internal/tools/file_ops.go:224`, `:286`, `internal/tools/path_read.go:97`, `:167`,
`internal/agent/contextfiles.go:63` (each keeps its existing escape arm; the new arm comes
first). The comment at `internal/tools/path_safety.go:20` about `errors.Is` matching stays
true — extend it with one line naming the second sentinel. Record under `[Unreleased]` in
`CHANGELOG.md`.

**Files:** `internal/security/safeio.go`, `internal/security/writepermit.go`,
`internal/security/safeio_test.go`, `internal/tools/file_ops.go`,
`internal/tools/path_read.go`, `internal/tools/path_safety.go`,
`internal/agent/contextfiles.go`, `CHANGELOG.md`

**Tests:** In `internal/security/safeio_test.go`: point a Safe I/O primitive at a root that
does not exist (and one that is a file, not a directory) and assert
`errors.Is(err, ErrRootInaccessible)` is true AND `errors.Is(err, ErrPathEscape)` is false;
one existing escape-path test asserts the reverse pairing so the two sentinels cannot be
collapsed later.

**Acceptance:** `go build ./internal/security/ ./internal/tools/ ./internal/agent/ && go test ./internal/security/ -run TestSafe -v && go test ./internal/tools/ ./internal/agent/`

**Commit:** `fix(security): a broken workspace root reports ErrRootInaccessible, not a path escape`

## 3. Remove the dead `Tier.String()` — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the `%v` → `%d` verb adjustment reached two files beyond the item's Files list — `internal/security/rules_test.go` and `internal/tools/file_ops_test.go` carry the same `%v`-on-`Tier` dispatch the item names by example (`dangerous_test.go:61`), so every site of the pattern got the same mechanical swap; message wording is unchanged everywhere, since each already names the expected tier constant.
NOTES (2026-08-15): the item's acceptance grep cannot reach `0` as written — its second alternation `\.String()` also matches the two pre-existing `strings.Builder.String()` calls at `internal/security/dangerous.go:247` and `:250`. The substantive check passes: `grep -c "func (t Tier) String" internal/security/dangerous.go` is `0`, and `Tier.String` has no match anywhere outside `docs/`.

**What:** Delete `Tier.String()` (`internal/security/dangerous.go:34`) — exported, zero
explicit callers repo-wide, 0% coverage; the only reachable path is implicit Stringer
dispatch from `%v` verbs inside test FAILURE messages (e.g. `dangerous_test.go:61`), so
check those messages still read sensibly with the numeric tier (adjust the verb to `%d` or
name the tier in the message text where clarity needs it). Record the removal under
`[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/security/dangerous.go`, `internal/security/dangerous_test.go`,
`CHANGELOG.md`

**Tests:** existing suite only — the item removes an untested method.

**Acceptance:** `go build ./... && go test ./internal/security/ -run TestDangerous -v && grep -rn "Tier.String\|\.String()" internal/security/dangerous.go | wc -l | grep -q '^0$'`

**Commit:** `chore(security): remove the dead Tier.String on the guard path`

## 4. CircuitBreaker: the concurrency claim and the post-trip recovery get tests — ✅ DONE (2026-08-15)

**What:** Test-only, both in `internal/security/circuitbreaker_test.go`. (a) The doc at
`circuitbreaker.go:24` claims "safe for concurrent use" and no test spawns a goroutine
against it: add a test that runs N goroutines (e.g. 8) concurrently calling `Record` (mixed
success/failure) and `Tripped` on overlapping and distinct signatures, then asserts a
deterministic final state on a signature driven only by one goroutine — under `make test`'s
`-race` this proves the guarantee. (b) The post-trip recovery branch
(`delete(b.tripped, sig)` at `circuitbreaker.go:69`) is never reached:
`TestCircuitBreaker_SuccessResetsStreak` records only 2 failures against threshold 3. Add a
test that trips a signature with 3 identical failures, records a success, and asserts
`Tripped` returns false — a regression leaving `tripped` stale would otherwise block the
signature forever. Record under `[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/security/circuitbreaker_test.go`, `CHANGELOG.md`

**Tests:** the item is its own tests (above).

**Acceptance:** `go test -race ./internal/security/ -run TestCircuitBreaker -v`

**Commit:** `test(security): pin the CircuitBreaker's concurrency claim and post-trip recovery`

## 5. A reset of an empty-default key reaches the engine

**What:** `settingsApplied` (`internal/tui/settings.go:1343-1355`, guard at `:1347`) skips
`settingsApplyLive` whenever `edit.value == ""`, so resetting a key whose registry default
is `""` (`web-search-endpoint`, `editor`, `present.command`, `present.host`,
`system-prompt-text`, `system-prompt-file` — the empty-default keys with live-apply cases
at `cmd/apogee/wire_settings.go:611-649`) removes the file's line but never tells the
engine: it keeps the old value until restart, and the config watcher cannot heal it because
`ResetSetting` refreshes the self-write baseline (`cmd/apogee/wire_options.go:228-234`).
Per the ratified design call: let a RESET through the guard — change it so an edit with
`reset` set applies even when the recorded default is empty (a non-reset empty value keeps
the current skip), and in the dispatcher (`cmd/apogee/wire_settings.go`) make each of the
six enumerated keys' apply cases treat `""` as "the file no longer sets this key": resolve
to the same built-in default a fresh start would use (e.g. `web-search-endpoint` falls back
to the built-in provider default, `editor` falls back to the `$VISUAL`/`$EDITOR`/OS-opener
ladder of ADR 0041). Update the `settingsApplied` doc paragraph that currently rationalizes
the skip (`:1335-1338`) to state the new contract. Keys whose apply case cannot express
"unset" for a structural reason get a dated NOTES line naming the key and why, rather than a
silent skip. Record under `[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/tui/settings.go`, `internal/tui/settings_test.go`,
`cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`, `CHANGELOG.md`

**Tests:**
- In `internal/tui/settings_test.go`: a reset of an empty-default key calls the ApplySetting
  seam with `""` (stub seam records the call); a reset of a non-empty-default key still
  applies its default; a plain empty-value non-reset edit still skips.
- In `cmd/apogee/wire_test.go`: for at least `web-search-endpoint` and `editor`, applying
  `""` lands the same state as a fresh start with the key absent (assert through the same
  observation point the key's existing apply tests use).

**Acceptance:** `go build ./internal/tui/ ./cmd/apogee/ && go test ./internal/tui/ -run TestSettings -v && go test ./cmd/apogee/ -run 'TestSetting|TestApplySetting' -v`

**Commit:** `fix(config): a reset of an empty-default key reaches the engine instead of stalling at the file`

## 6. `relativeTime` clamps a future timestamp

**What:** `relativeTime` (`internal/tui/sessions.go:666-669`) computes `d := now.Sub(t)`
and its first arm `d < time.Minute` accepts any negative duration, so a record whose
`UpdatedAt` sits ahead of the wall clock (clock skew, an NTP step back, a restored
snapshot) renders "just now" by accident rather than by decision. Clamp `d` to zero when
negative before the switch — mirroring `formatElapsed`'s clamp — so the "just now" answer
for skew is deliberate and any future rearrangement of the switch arms cannot turn a
negative duration into garbage. Record under `[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/tui/sessions.go`, `internal/tui/sessions_test.go`, `CHANGELOG.md`

**Tests:** add a future-timestamp row to `TestRelativeTime`
(`internal/tui/sessions_test.go:739-760`): `t` several days ahead of `now` → "just now".

**Acceptance:** `go test ./internal/tui/ -run TestRelativeTime -v`

**Commit:** `fix(tui): relativeTime treats a future timestamp as just now by decision`

## 7. Pin `discardPending`'s parked-sibling drop

**What:** Test-only. The behaviour at `internal/tui/transcript.go:866-869` — a
`StreamResetEvent` for a run that is NOT the slot-holding streamer drops that run's parked
text via `t.unpark(run)` and leaves the slot's live stream untouched — is intentional
(the doc at `:819` owns it) but unpinned: `TestStreamResetOnlyDiscardsItsOwnDepth`
(`transcript_test.go:1624-1633`) stages no parked text for the resetting run, so a
refactor reordering the unpark and the early return would change the semantic silently.
Add a test that streams sibling A into the slot, parks text for sibling B, sends B's
`StreamResetEvent`, then continues A — asserting A's stream lands intact, B's parked text
is gone from `parked`, and B's dropped tokens never reach the transcript. Record under
`[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/tui/transcript_test.go`, `CHANGELOG.md`

**Tests:** the item is its own test (above).

**Acceptance:** `go test ./internal/tui/ -run 'TestStreamReset|TestAppendToken' -v`

**Commit:** `test(tui): pin that a parked sibling's stream reset drops only its own text`

## Suggested version bump

Patch-level micro-bump once the plan lands (one real security fix, two small behaviour
fixes, a sentinel split, and test hardening). The bump is the owner's call; no item in this
plan changes `VERSION` or a `CHANGELOG` release heading.
