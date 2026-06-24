package tools

import "github.com/airiclenz/apogee/internal/domain"

// NewDefaultRegistry assembles the built-in tool set — the read/write/list/grep base
// (P1.4) plus the file-editing family (P3.7) — each scoped to root, into a
// domain.ToolRegistry. It is the seam the engine uses to give an Agent its default tools
// (the loop's dispatch wires it in P1.2); an embedder can equally build a registry by
// hand and Register its own.
//
// Registration cannot fail here: the names are distinct and non-empty, the only
// conditions Register rejects.
func NewDefaultRegistry(root string) *domain.ToolRegistry {
	registry := domain.NewToolRegistry()
	for _, tool := range DefaultTools(root) {
		_ = registry.Register(tool)
	}
	return registry
}

// DefaultTools returns the built-in tools scoped to root, in menu order. It is exposed
// so a caller can register a subset, or add them to a registry that already holds
// host-supplied tools. The file-editing family (P3.7) follows the base set; the write
// tools among them (find-replace, edit_existing_file) carry the workspaceScopedWriter
// marker so the dispatch disposition path-bounds rather than confines them (ADR 0012 D1).
func DefaultTools(root string) []domain.Tool {
	return []domain.Tool{
		NewReadFile(root),
		NewWriteFile(root),
		NewListDir(root),
		NewGrep(root),
		NewSingleFindReplace(root),
		NewMultiFindReplace(root),
		NewEditExistingFile(root),
		NewViewDiff(root),
		NewOpenFile(root),
	}
}
