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
//   - URL-safety (URLGuard): scheme/host allow-deny for the network tools
//     (web-fetch / http-request, P3.11), deny-first precedence.
//   - The dangerous-action guard (DangerousActionGuard): the default-on footgun
//     floor, two tiers (hard-refuse / force-approval), narrow precision-over-recall
//     literal/regex matching, with config-merge semantics (global may add OR remove,
//     project may only add — MergeDangerousRules).
//   - The circuit-breaker (CircuitBreaker): halts a runaway loop of identical failing
//     calls, surfacing an ErrorEvent rather than spinning.
//   - The audit record (AuditLog): an append-only call / decision / result trail.
//
// Guards bundles the executor-facing always-on set (dangerous-action + breaker +
// audit) the tool executor threads around every call so all tools — and a sub-agent
// (D2) — inherit them. The package imports only internal/domain and the standard
// library (ADR 0010): it is imported BY internal/tools and internal/agent, never the
// other way, so there is no cycle.
package security
