// Package tools holds the built-in Tool implementations that sit behind the public
// domain.Tool interface — an open extension point (ADR 0002). Tools are stateless
// across Turns: their only durable side effect is filesystem writes, and nothing
// live is held across the quiescent boundary (ADR 0008).
//
// Phase 1 (P1.4) lands the minimal local set — read_file, write_file, list_dir, and
// a pure-Go grep (no external programs, §3a) — each scoped to a sandbox root at
// construction (tools.NewReadFile(root), …) so the package carries no dependency on
// domain.Config and a test can point it at a t.TempDir(). Every path argument is
// resolved through path-safety, which rejects traversal escapes outside the root.
//
// A tool reports an expected failure (bad arguments, missing file, path escape) as a
// ToolResult with IsError set, so the model sees and can react to it; the Go error
// return is reserved for ctx cancellation. NewDefaultRegistry assembles the four into
// a domain.ToolRegistry — the seam the loop's dispatch (P1.2) wires. Richer tools
// (patch-edit, terminal, web) and the remaining oracle behaviours are later phases.
package tools
