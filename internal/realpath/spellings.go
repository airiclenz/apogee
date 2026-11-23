package realpath

import "path/filepath"

// RootSpellings is a Project root and the workspace it is judged from, in every spelling the file
// system vouches for. It is a plain value: a caller that must read no disk (the dangerous-action
// guard's Project config rule) is handed one already computed.
type RootSpellings struct {
	// Root is the Project root as given, symlink-resolved by the caller; empty is no Project root.
	Root string
	// LinkedRoot is Root spelled through the workspace's own unresolved prefix — the folder the
	// workspace's spelling climbs to — when that differs from Root and resolves to it; empty
	// otherwise.
	LinkedRoot string
	// FinalRoot is Root in the one spelling ResolveLenient gives every path (on Windows the
	// volume's final name, so the underlying location of a subst or mapped drive), when that
	// differs from Root; empty otherwise.
	FinalRoot string
	// Workspace is the workspace as given; empty is no workspace.
	Workspace string
	// RealWorkspace is Workspace with its links resolved (Resolve), when that differs from
	// Workspace; empty otherwise.
	RealWorkspace string
}

// Spellings spells the symlink-resolved Project root root and the workspace as the session names
// it: the workspace resolved through a link or junction (Resolve) when that differs, and the root
// spelled through the workspace's own unresolved prefix — the folder the workspace climbs to as
// many levels as its resolved form climbs to root — kept only when the file system resolves it to
// root. A workspace that is itself a link climbs to some other folder (its link's parent), which
// keeps no spelling, so a home above it is never mistaken for the Project root. A workspace that
// cannot be resolved adds no spelling.
//
// The root is also spelled the way ResolveLenient resolves every path when that differs: on
// Windows a root on a subst or mapped drive keeps its drive letter in root while a resolved path
// names the underlying location.
//
// An empty root yields only Root and Workspace as given; an empty workspace adds no workspace
// spelling.
func Spellings(root, workspace string) RootSpellings {
	spellings := RootSpellings{Root: root, Workspace: workspace}
	if root == "" {
		return spellings
	}
	if final := ResolveLenient(root); final != root {
		spellings.FinalRoot = final
	}
	if workspace == "" {
		return spellings
	}
	realWorkspace, err := Resolve(workspace)
	if err != nil || realWorkspace == workspace {
		return spellings
	}
	spellings.RealWorkspace = realWorkspace
	linked := workspace
	for realWorkspace != root {
		parent := filepath.Dir(realWorkspace)
		if parent == realWorkspace {
			return spellings
		}
		realWorkspace, linked = parent, filepath.Dir(linked)
	}
	if linked == root {
		return spellings
	}
	if resolved, err := Resolve(linked); err == nil && resolved == root {
		spellings.LinkedRoot = linked
	}
	return spellings
}
