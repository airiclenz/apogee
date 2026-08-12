// Package security holds Apogee's human-in-the-loop safety guardrails — the layer
// that runs in EVERY mode, distinct from Auto-mode Confinement (the OS-level
// Confiner in package platform). These are footgun-guards and trustworthy-trail
// machinery, NOT a security boundary: the boundary is the Confiner; this layer
// catches a small model's obvious catastrophic mistakes and bounds its blast radius
// (ADR 0012, D6).
//
// The guards, all reusable and tighten-only:
//
//   - Path-safety (ResolveInRoot / EvalRealPath, ErrPathEscape): the consolidated,
//     symlink-aware, traversal-rejecting workspace boundary the file tools call —
//     one guard instead of a copy per tool. The actual read/write the write tools then
//     perform goes through the TOCTOU-safe SafeReadFile / SafeWriteFile (safeio.go),
//     which operate via an os.Root pinned at the workspace root so the validated path IS
//     the path written — an escaping-symlink component (incl. one swapped in concurrently
//     by a confined subprocess) is refused, not followed (closes the H1 symlink-swap race).
//     A bounded read goes through SafeOpen, which returns the pinned handle itself: the
//     caller fstats and limit-reads the very descriptor it opened, so the size bound
//     shares the guarantee (see the SCOPE note in safeio.go).
//   - URL-safety (URLGuard): scheme/host allow-deny for the network tools
//     (web-fetch / http-request, P3.11), deny-first precedence.
//   - The dangerous-action guard (DangerousActionGuard): the default-on footgun
//     floor, two tiers (hard-refuse / force-approval), narrow precision-over-recall
//     literal/regex matching, with config-merge semantics (global may add OR remove,
//     project may only add — MergeDangerousRules). It matches a call's ACTION text —
//     the tool, its target paths, its command lines and code — and never the payload a
//     write carries, so a document that merely quotes a guarded path is not an action
//     (payloadKeys in dangerous.go).
//   - The circuit-breaker (CircuitBreaker): halts a runaway loop of identical failing
//     calls, surfacing an ErrorEvent rather than spinning.
//   - The audit record (AuditLog): an append-only call / decision / result trail.
//
// Guards bundles the executor-facing always-on set (dangerous-action + breaker +
// audit) the tool executor threads around every call so all tools — and a sub-agent
// (D2) — inherit them. The package imports only internal/domain and the standard
// library (ADR 0010): it is imported BY internal/tools and internal/agent, never the
// other way, so there is no cycle.
//
// # The files, one line each
//
// The executor bundle. guard.go is Guards itself — the always-on set the tool executor threads
// around every call (PreExecute, RecordExecution, RecordBlocked) — plus ForSubAgent, the split
// that hands a sub-agent its own breaker and its own audit trail while keeping the
// dangerous-action floor shared and read-only, which is the floor it must not be able to
// re-derive (ADR 0013).
//
// The dangerous-action guard, in two files so the ruleset reads as a list rather than as
// machinery. dangerous.go is the mechanism: the two Tiers, the Rule and Decision types, Inspect,
// and the inspectable-text derivation that reads a call's ACTION — tool name, target paths,
// command lines, code — while skipping the payload keys a write carries, so a document that
// merely quotes a guarded path is not an action. rules.go is the content: DefaultDangerousRules,
// the narrow precision-over-recall built-in floor with a comment per rule saying where its
// boundary is, and MergeDangerousRules, which encodes who may loosen it (global may add or
// remove, project may only add — ADR 0012).
//
// The runaway halt and the trail. circuitbreaker.go trips after DefaultCircuitBreakerThreshold
// consecutive identical FAILING calls, keyed by a (tool, arguments) signature that any success
// clears. audit.go is the append-only call / decision / result record: the AuditDecision
// vocabulary, the capped ring that counts what it drops rather than silently forgetting, and the
// result truncation that keeps one record bounded.
//
// The filesystem boundary, split check-from-use because the gap between them is the race.
// pathsafety.go is the CHECK — ResolveInRoot (symlink-aware, traversal-rejecting, validating a
// not-yet-existing write target against its nearest existing ancestor), ErrPathEscape, and the
// EvalRealPath / WorkspaceRelative helpers every surface that prints a path goes through.
// safeio.go is the USE — SafeReadFile, SafeWriteFile, SafeOpen, SafeCopyFile, SafeCopyFileFrom,
// SafeRename and SafeRemove, each performed through an os.Root pinned at the root it is fenced
// by so the validated path IS the path touched (H1), with the same-directory staging file and
// rename that makes a write atomic at the target name. All but one pin every end at the SAME
// (workspace) root; SafeCopyFileFrom is the exception that pins a root at each end, because a
// copy's source is a read and may come from a read-only root the destination fence knows nothing
// about — its write half is bounded by the destination root exactly as the others are.
// execsafety.go measures that same boundary in the other direction: RefuseExecFromWritablePath
// keeps an argv[0] that resolves inside the writable box from ever being executed, so bytes a
// confined call was allowed to WRITE cannot become the program a later unconfined call RUNS.
//
// The network boundary, likewise in two layers. urlsafety.go is URLGuard, judged on the URL as
// WRITTEN: scheme and host allow-deny with deny-first precedence, plus NormalizeURL and its
// IDNA / non-ASCII handling. ssrf.go is the floor beneath it, judged on the RESOLVED IP instead —
// the denied v4 and v6 ranges, the NAT64-embedded decode, and the dial-time SafeDialControl /
// PinnedDialControl that re-check the address the connection actually goes to, so a name that
// rebinds between the check and the connect cannot walk past it.
//
// And doc.go this map.
package security
