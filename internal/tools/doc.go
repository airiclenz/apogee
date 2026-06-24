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
// return is reserved for ctx cancellation. NewDefaultRegistry assembles the built-ins
// into a domain.ToolRegistry — the seam the loop's dispatch (P1.2) wires.
//
// Phase 3 (P3.7) adds the file-editing family: single/multi find-replace, a patch-aware
// edit_existing_file, a pure-Go view_diff, and a read-and-locate open_file. The write
// tools among them carry the unexported workspaceScopedWriter marker so the dispatch
// disposition path-bounds rather than confines them (ADR 0012 D1).
//
// Phase 3 (P3.8) adds the execution tools (terminal, python-exec) and (P3.9) the git
// tools (git_branch, git_commit, git_diff_range) — SubprocessTools that shell out to a
// detected program (the system shell/interpreter, the system git) and degrade gracefully
// when it is absent (§3a). The disposition confines the write-capable ones in Auto (or
// gates them when fs-confinement is unavailable); git_diff_range is read-only and runs
// freely. The network and MCP tools land in later phases.
package tools
