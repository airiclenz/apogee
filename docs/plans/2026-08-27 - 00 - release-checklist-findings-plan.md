# Plan: release-checklist findings D-1 / D-2 / D-3

**Goal:** close the three code-facing defects the v0.17.1→v0.17.7 release test checklist surfaced
(handoff `docs/handoffs/2026-08-27 - 00 - release-test-checklist-execution.md`): the flaky
delegate shakeout, the stale README archive-install version pin, and the bare-colon upstream
HTTP error message.

**Date:** 2026-08-27 · **Status:** unexecuted · **Sized for:** ~200k-context host

**Authoritative sources:**

- The handoff above (findings D-1, D-2, D-3 — D-4 was DENIED by the owner: the plan it concerns
  is executed and archived; no retroactive edit).
- `internal/agent/loop.go` `maxOutputTokens()` and the `minOutputTokenCap`/`maxOutputTokenCap`
  rationale block (ADR 0046) — the reply ceiling derives from the WORKING room, pin wins outright.
  That design is settled and this plan does not touch it.
- `internal/agent/subagent.go` (`runSubAgent` child construction): an UNROUTED child inherits the
  parent's `Config` wholesale, `Context.MaxOutputTokens` included; a ROUTED child takes the
  target's pin unconditionally.
- `internal/provider/client.go` `NewClient` comment (~line 195): the client never follows a
  redirect by design; a redirecting endpoint must be configured at the URL it redirects to.
- `README.md` Install section, "A prebuilt archive" block; `docs/manual/building.md` (VERSION /
  release conventions).

**Ratified design calls (owner, 2026-08-27, via AskUserQuestion):**

1. **Scope:** D-1, D-2, D-3 are in; D-4 is DENIED (archived plan, paperwork only).
2. **D-1:** fix the SHAKEOUT, not the engine — pin `Context.MaxOutputTokens` in the live test's
   config. The engine's working-room derivation (ADR 0046) stays as is.
3. **D-2:** the README archive-install block auto-resolves `VERSION` from the latest GitHub
   release (curl + sed, no `jq`), with a comment on how to pin a specific one. No hard-coded
   number remains.
4. **D-3:** an empty upstream body renders as `apogee: upstream HTTP <code> <StatusText>`; any 3xx
   additionally appends a redirect hint naming the `Location` header when present. Non-empty
   bodies keep today's exact text.

**Standing requirements:**

- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Never change `VERSION`, a CHANGELOG release heading, or a tag (see closing note).
- Per-item acceptance is targeted; `make check` runs once at closeout.

**Out of scope:**

- D-4 (archived plan's `grep -nF`) — denied.
- Any change to how the engine derives a child's reply cap (`loop.go` `maxOutputTokens`,
  `subagent.go` routed/unrouted pin handling).
- Following redirects in the provider client (settled: never).
- Merging the Dependabot PRs, cutting the release, ticking checklist boxes
  (`/test-checklist` record mode owns those).
- `internal/provider/discovery.go:219` (`model discovery: upstream HTTP %d`) — carries no body and
  no trailing colon; not affected.

---

## 1. Pin the delegate shakeout's reply ceiling (D-1) — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the capped-reply assertion sits after the `budgets` bound checks (still after `budgets := probe.at(1)` as the item states) rather than renumbering the test's existing 1./2./3. comment blocks.
NOTES (2026-08-27): a live re-run of the shakeout is owed at the owner's next `make live-eval` — the verifier does not run the live path.

**What:** `TestLiveDelegateCapAndWorkingWindow` bounds the child to
`liveDelegateWorkingWindow = 32768` and pins no `MaxOutputTokens`, so the child's reply ceiling is
`32768 × 0.20 = 6553` tokens whatever the server advertises (`loop.go` `maxOutputTokens`, working-room
derivation). A reasoning model that thinks at length trips `cappedDelegateReplyErrFmt` before the
5-Turn step cap bites — one FAIL, one PASS on 2026-08-27. The shakeout is about the step cap, the
working window and the parent-growth marker, not the reply cap, so it must state the ceiling it
wants:

- In `internal/agent/live_delegate_cap_test.go`:
  - Add `liveDelegateOutputCap = 16384` to the shakeout's const block, with a comment stating the
    derivation above (32768 × 0.20 = 6553 < what a reasoning child needs; the pin is the operator's
    statement and wins outright per `maxOutputTokens`), and that the cap must stay ≤
    `liveDelegateWorkingWindow / 2` so the child keeps prompt room.
  - Set `Context.MaxOutputTokens: liveDelegateOutputCap` in the shakeout's `cfg`. The parent is
    UNROUTED (no delegation target), so the child inherits the pin wholesale — say so in the comment.
  - In the test body, after `budgets := probe.at(1)`, assert that no child `UsageEvent` carries the
    capped-reply fault: walk `events` for `domain.ErrorEvent`s at Depth 1 whose text starts with the
    `cappedDelegateReplyErrFmt` prefix (format the constant with the pin to build the expected
    string, as `emptyreply_test.go:285` does) and `t.Errorf` on any hit — the FAIL mode D-1 recorded
    becomes a named assertion rather than a surprise.
- In `internal/agent/budget_test.go`, add one unit test
  `TestUnroutedChildInheritsTheParentsOutputPin`: construct a parent with
  `Context.MaxOutputTokens = 16384`, `MaxContextTokens = 98304`, `WorkingWindow = 32768`, spawn an
  unrouted child through the same helper the existing spawn tests use (`spawn(t, parent)` in
  `routedspawn_test.go` — reuse, do not duplicate), and assert `child.maxOutputTokens() == 16384`.
  This is the offline proof that the shakeout's pin actually reaches the child.

Standards that shape this item: one test helper reused (`spawn`), not a second spawn path; the
const's comment carries the number's derivation so the next reader does not re-derive it.

**Files:** `internal/agent/live_delegate_cap_test.go`, `internal/agent/budget_test.go`

**Tests:** the new `TestUnroutedChildInheritsTheParentsOutputPin`; the existing
`TestMaxOutputTokensDerivesFromTheReplyBudget` / `…FromTheWorkingRoom` stay green (they prove the
engine side is untouched).

**Acceptance:**

```
go build ./... && go vet ./internal/agent/ && go test -race -count=1 -run 'TestUnroutedChildInheritsTheParentsOutputPin|TestMaxOutputTokens|TestRoutedSpawn' ./internal/agent/
```

The live shakeout itself (`make live-eval`, `total_slots: 1` server at `http://apollo-ii.local:1111`)
is NOT run by the verifier — it is the owner's rig; the verifier confirms the pin is set and the
new assertion compiles. Record in the item's NOTES that a live re-run is owed at the owner's next
`make live-eval`.

**Commit:** `test(agent): pin the delegate shakeout's reply ceiling so a reasoning child cannot trip the output cap`

---

## 2. README archive install resolves the latest release (D-2) — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the tripwire reads `../../README.md` from disk as the plan instructs — a deliberate exception to the testing standard's no-real-filesystem rule, since the README itself is the artefact under test.

**What:** `README.md:86` pins `VERSION=0.15.0`, six releases behind the latest cut release (v0.17.1)
and every future one. Replace the hard-coded pin in the "A prebuilt archive" block with a line that
resolves the latest release tag at run time, no `jq`:

```bash
# macOS / Linux — resolves the latest release; to pin one, set VERSION=0.17.1 instead
VERSION=$(curl -fsSL https://api.github.com/repos/airiclenz/apogee/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
PLATFORM=darwin_arm64   # or darwin_amd64 · linux_amd64 · linux_arm64
```

The three lines that follow (`curl -fsSLO …`, `tar -xzf …`, `sudo install …`) are unchanged — they
already interpolate `$VERSION`. Keep the block's surrounding prose as is. Nothing else in the
README names a version number (verified 2026-08-27: the only `0.1x.y` literal in `README.md` and
`docs/manual/` is this one plus `building.md:44`'s illustrative `v0.16.8`, which stays — it is an
example of the file's format, not an install instruction).

Add a one-line guard so the number can never return: in `cmd/apogee/`, a test
`TestReadmeArchiveInstallDoesNotPinAVersion` (new file `cmd/apogee/readme_test.go`) reads
`../../README.md` and fails if any line matches `^VERSION=[0-9]`. Keep it to that one regexp; it is
a tripwire, not a README parser.

**Files:** `README.md`, `cmd/apogee/readme_test.go`

**Tests:** the new tripwire; run the resolver line once in the verifier's shell and confirm it
prints `0.17.1` (the latest published release as of 2026-08-27; any later release tag is also a
pass — the point is a non-empty `MAJOR.MINOR.PATCH`).

**Acceptance:**

```
! grep -nE '^VERSION=[0-9]' README.md && curl -fsSL https://api.github.com/repos/airiclenz/apogee/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' && go test -race -count=1 -run TestReadmeArchiveInstallDoesNotPinAVersion ./cmd/apogee/
```

**Commit:** `docs(readme): resolve the archive-install version from the latest release instead of a stale pin`

---

## 3. Upstream HTTP error names the status and the redirect (D-3) — ✅ DONE (2026-08-27)

**What:** an upstream reply with an empty body renders as `apogee: upstream HTTP 308: ` — a trailing
colon with nothing after it (`internal/provider/client.go:83` `StatusError.Error`,
`internal/provider/stream.go:103` `statusDelta`). The refusal to follow the redirect is correct
(`NewClient` comment); only the message is wrong. Make both surfaces render the same text through
ONE function:

- Add to `internal/provider/client.go` an unexported `upstreamStatusText(code int, body, location string) string`:
  - body non-empty → `fmt.Sprintf("apogee: upstream HTTP %d: %s", code, body)` — byte-identical to
    today (the `reliability_test.go:337` prefix assertion and `overflow_test.go:79` text stay valid).
  - body empty → `fmt.Sprintf("apogee: upstream HTTP %d %s", code, http.StatusText(code))`; when
    `http.StatusText` returns `""` (unknown code) the line is `apogee: upstream HTTP %d` with no
    trailing space.
  - `300 ≤ code ≤ 399` → append ` — redirects are not followed; point endpoint: at the URL the server redirects to` and, when `location != ""`, ` (Location: <location>)`. The hint applies in
    both the empty- and non-empty-body branches.
- `StatusError` gains a `Location string` field (the response's `Location` header, `""` when
  absent); `Error()` becomes `return upstreamStatusText(e.Code, e.Body, e.Location)`.
- `statusError` (`client.go:463`) and `statusDelta` (`stream.go:97`) both read
  `resp.Header.Get("Location")` and pass it through; `statusDelta`'s `message :=` line calls
  `upstreamStatusText`. `inBandError` / `inBandErrorDelta` set no `Location` (an in-band error has no
  response header to read) — leave them as they are apart from compiling against the new field.
- `client.go:370` (`lastErr = fmt.Errorf("apogee: upstream HTTP %d", resp.StatusCode)`) is the
  retry-exhaustion inner error and already carries no colon — unchanged.

Standards that shape this item: one deep function owns the text, both call sites are thin; the
exported `StatusError` stays branchable by `Code` (its contract) and gains one field, no new type.

**Files:** `internal/provider/client.go`, `internal/provider/stream.go`,
`internal/provider/reliability_test.go`

**Tests:** add to `internal/provider/reliability_test.go`:

- `TestStatusError_EmptyBodyNamesTheStatus`: a `httptest.Server` answering 308 with an empty body and
  `Location: http://elsewhere.invalid/v1` → `Respond` returns a `*StatusError` with `Code == 308`,
  `Location == "http://elsewhere.invalid/v1"`, and `Error()` equal to
  `apogee: upstream HTTP 308 Permanent Redirect — redirects are not followed; point endpoint: at the URL the server redirects to (Location: http://elsewhere.invalid/v1)`;
  the same server driven through `Stream` yields one `DeltaError` whose `Err` is that exact string.
- A 503 with an empty body and no Location (retries exhausted, `WithMaxRetries(0)` or the client's
  equivalent) → `Error()` == `apogee: upstream HTTP 503 Service Unavailable`.
- The existing `TestRespond_StatusError…` prefix case for a NON-empty body must still pass unchanged
  — it is the byte-identity proof.

**Acceptance:**

```
go build ./... && go vet ./internal/provider/ && go test -race -count=1 ./internal/provider/ && go test -race -count=1 -run 'TestProbe|TestOverflow|Overflow' ./cmd/apogee/ ./internal/agent/
```

**Commit:** `fix(provider): name the status and the redirect target when an upstream reply has no body`

---

## Suggested version bump

Items 1–3 together are a patch-level change (a test fix, a doc fix, an error-message fix): suggest
`v0.17.8` once the owner wants it — never performed by this plan. Per `AGENTS.md`, the bump is its
own commit and, at a release cut, the last one after the `CHANGELOG.md` rollup.
