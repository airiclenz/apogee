# Code Review — `internal/security/` — 2026-08-11

**Scope:** The `internal/security/` package — dangerous-action guard, circuit breaker, audit log, path safety, URL safety, SSRF floor, Safe I/O, and rule config merge.
**Mission:** A terminal coding agent whose Auto mode is fenced at the OS level, with guardrails that catch catastrophic mistakes without becoming a security boundary.
**Files reviewed:** 20

## Executive Summary

The security package is well-built: every guardrail has live callers, clear placement in the executor bundle, and thorough single-goroutine tests. The multi-layered defence — SSRF floor on resolved IPs, dial-time rebinding protection, TOCTOU-safe I/O via `os.Root`, and a tighten-only config merge — is correctly implemented. Seven findings, all Medium: one piece of dead code on the critical guard path, one consistent error-classification defect across all six Safe I/O primitives, one URL-normalization gap that can bypass `DenyHosts` string matching, and four test-coverage gaps in the circuit breaker, audit, and URL-safety paths. No critical or high-severity issues.

## Intent & Architecture Findings

### Medium — `Tier.String()` is dead code on the critical dangerous-action path `[Intent & Structure + Critical-Path Tests]`

- **Where:** `internal/security/dangerous.go:34`
- **What:** `Tier.String()` is exported, has zero callers anywhere in the codebase (full-project grep confirmed), and sits at 0% statement coverage (tool-confirmed). The method maps `Tier` constants to decision strings the audit trail surfaces — but nothing calls it.
- **Why it matters:** If the switch accidentally returns the wrong string for a `Tier` constant (e.g. `"none"` for `TierHardRefuse`), the audit trail would silently report the wrong decision — no test, no caller, no detection. A future developer wiring it up assumes it works.
- **Fix:** Either remove the method (it has no callers and no planned use surfaced in design docs), or add a table-driven test covering all three `Tier` constants and a caller in the audit-log formatting path.

## Medium Findings

### Medium — `os.OpenRoot` failures are misclassified as path escapes across all six Safe I/O primitives `[Correctness]`

- **Where:** `internal/security/safeio.go:83,217,245,286,365,393`
- **What:** When `os.OpenRoot(root)` fails (workspace root deleted, permissions changed, not a directory), the error is wrapped with `ErrPathEscape` via `fmt.Errorf("%w: %v", ErrPathEscape, err)`. Callers match `errors.Is(err, ErrPathEscape)` and display "path resolves outside the workspace" — but the input path is fine; the root itself is unreachable.
- **Why it matters:** A broken workspace root is a configuration/infrastructure error, not a security violation. Mislabeling it as a path escape produces misleading diagnostics and could trigger unnecessary alarm in Auto mode. Affects `SafeWriteFile`, `SafeReadFile`, `SafeOpen`, `SafeCopyFile`, `SafeRename`, and `SafeRemove` identically.
- **Fix:** Introduce a distinct sentinel (e.g. `ErrRootInaccessible`) for `os.OpenRoot` failures, or wrap with a different error so callers can distinguish "root unreachable" from "path escaped."

### Medium — `NormalizeURL` strips only one trailing dot, bypassing `DenyHosts` string matching `[Security]`

- **Where:** `internal/security/urlsafety.go:182`
- **What:** `NormalizeURL` uses a single `strings.HasSuffix(host, ".")` check to strip a trailing dot. A hostile URL like `https://denied.example.com../path` keeps `denied.example.com.` after the strip — the trailing dot prevents `hostMatches` from matching the `DenyHosts` entry for `example.com`. DNS resolves `denied.example.com.` identically to `denied.example.com` anyway.
- **Why it matters:** An attacker (hostile model response) crafting a double-dot host bypasses `DenyHosts` string-level matching for public domains. The SSRF floor still catches IP-based blocks (loopback/private ranges at the dial layer), but string-level deny entries are defeated.
- **Fix:** Strip trailing dots in a loop: `for strings.HasSuffix(host, ".") { host = host[:len(host)-1] }` so any number of trailing dots reduce to the bare host before matching.

### Medium — CircuitBreaker has no concurrent test despite documented thread-safety guarantee `[Critical-Path Tests]`

- **Where:** `internal/security/circuitbreaker.go:28-31`
- **What:** The doc comment says `CircuitBreaker` is "safe for concurrent use (the executor and any observer may touch it)," and the bundle confirms concurrency is a core mechanism. All existing tests are single-goroutine.
- **Why it matters:** A data race on the `failures` or `tripped` maps would silently pass `go test` because no concurrent goroutines exercise the breaker. In production, concurrent `Record`/`Tripped` calls from the executor and an observer goroutine could race.
- **Fix:** Add a test that starts N goroutines calling `Record` and `Tripped` concurrently on distinct and overlapping signatures, runs under `-race`, and asserts final failure counts and trip state are consistent.

### Medium — CircuitBreaker post-trip recovery path is untested `[Critical-Path Tests]`

- **Where:** `internal/security/circuitbreaker.go:74`
- **What:** The `delete(b.tripped, sig)` branch on a successful call after a trip is never reached in tests. `TestCircuitBreaker_SuccessResetsStreak` calls `Record(false)` after only 2 failures (threshold=3), so the breaker never tripped.
- **Why it matters:** A regression that leaves `tripped` stale after a successful call would permanently block the signature — the breaker would never recover from a trip.
- **Fix:** Add a test: trip a signature with 3 consecutive failures, call `Record(false)`, assert `Tripped()` returns false.

### Medium — `Guards.RecordBlocked` is never tested with a real audit log `[Critical-Path Tests]`

- **Where:** `internal/security/guard.go:135-139`
- **What:** `RecordBlocked` is a public method on the executor-facing guardrail bundle. The only test calling it uses a zero-value `Guards` with `Audit=nil`. No test proves it appends a record when `Audit` is non-nil.
- **Why it matters:** A silent regression in `RecordBlocked` would drop blocked-call records from the audit trail — decisions the guard made would disappear from the log with no warning.
- **Fix:** Add a test: create a non-nil `Guards`, call `RecordBlocked` with a blocked decision, assert `Audit.Len() == 1` and the record carries the expected decision and reason.

### Medium — IDNA failure path in `NormalizeURL` is untested `[Critical-Path Tests]`

- **Where:** `internal/security/urlsafety.go:173-177`
- **What:** `NormalizeURL` has an explicit fallback when `idna.Lookup.ToASCII` fails: it keeps the original host unchanged so the guard and transport judge the same string. No test covers this path.
- **Why it matters:** If a future edit treats an IDNA failure as an error instead of preserving the original host, the guard and the dialler would evaluate different host strings, creating a mismatch that could allow a bypass or cause a spurious block.
- **Fix:** Add a test that injects or constructs a host `idna.Lookup.ToASCII` rejects, and asserts `NormalizeURL` returns the URL with the original host unchanged (not an error and not a modified string).

## Recommended Action Order

1. **`NormalizeURL` trailing-dot fix** — the only concrete security bypass; one-line loop change with a test.
2. **`os.OpenRoot` error wrapping** — affects all six Safe I/O primitives; distinct sentinel prevents misleading diagnostics.
3. **`Tier.String()`** — decide: remove dead code or add caller + table-driven test.
4. **CircuitBreaker concurrent test** — the documented thread-safety guarantee needs a `-race` proof.
5. **CircuitBreaker post-trip-reset test** — close the recovery-path gap.
6. **`RecordBlocked` audit test** and **IDNA failure-path test** — quick, table-driven coverage additions.

## What Looked Good

The package's architecture is a model of clear layering: every guardrail is a focused type with a single responsibility, composed into the `Guards` bundle at the executor boundary. The ring-buffer unrolling in `AuditLog`, the TOCTOU-safe I/O through `os.Root`, the SSRF floor's exhaustive range coverage, the tighten-only config merge in `rules.go`, and the `sync.Mutex` usage in `AuditLog`/`CircuitBreaker` are all correctly implemented with no lock-ordering issues. The concurrency lens confirmed zero defects — the shared mutable state is properly guarded. Tests for the primary guardrails (dangerous-action matching, SSRF floor ranges, Safe I/O fence enforcement, config-merge semantics) are thorough and well-structured.