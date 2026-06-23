package tools

import "github.com/airiclenz/apogee/internal/domain"

// NewDefaultRegistry assembles the built-in tool set — read_file, write_file,
// list_dir, grep — each scoped to root, into a domain.ToolRegistry. It is the seam
// the engine uses to give an Agent its default tools (the loop's dispatch wires it in
// P1.2); an embedder can equally build a registry by hand and Register its own.
//
// Registration cannot fail here: the four names are distinct and non-empty, the only
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
// host-supplied tools.
func DefaultTools(root string) []domain.Tool {
	return []domain.Tool{
		NewReadFile(root),
		NewWriteFile(root),
		NewListDir(root),
		NewGrep(root),
	}
}
