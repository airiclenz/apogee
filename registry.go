package apogee

// This file holds the MechanismRegistry internals the public methods in apogee.go
// delegate to: experimental-hook storage, hook-interface assertions, and the
// startup ordering-cycle check (ADR 0003). P0.6 builds only cycle detection — the
// deterministic total order beyond it is Phase-4 registry work (plan §6).

// implementsAnyHook reports whether m satisfies at least one of the five hook
// interfaces. A Mechanism that hooks nowhere is a configuration error (ADR 0002 — a
// Mechanism is a behaviour at a hook point), so Add rejects it.
func implementsAnyHook(m Mechanism) bool {
	switch m.(type) {
	case PreRequestHook, PostResponseHook, PreToolExecHook, PostToolResultHook, HistoryRewriter:
		return true
	default:
		return false
	}
}

// hookImplements reports whether hook satisfies the interface required at the hook
// point at — the gate AddExperimental applies so a mis-registered hook fails at
// registration, not silently at fire time.
func hookImplements(at HookPoint, hook any) bool {
	switch at {
	case HookPreRequest:
		_, ok := hook.(PreRequestHook)
		return ok
	case HookPostResponse:
		_, ok := hook.(PostResponseHook)
		return ok
	case HookPreToolExec:
		_, ok := hook.(PreToolExecHook)
		return ok
	case HookPostToolResult:
		_, ok := hook.(PostToolResultHook)
		return ok
	case HookHistoryRewrite:
		_, ok := hook.(HistoryRewriter)
		return ok
	default:
		return false
	}
}

// detectOrderingCycle reports ErrOrderingCycle if the Before/After constraints among
// mechs form a cycle (ADR 0003 — a constraint cycle is a startup error). It builds a
// directed graph over the registered Mechanism IDs — an After=[Y] on X is an edge
// Y→X, a Before=[Z] on X is an edge X→Z — and runs a three-colour DFS; constraints
// naming an unregistered ID are ignored (they cannot close a cycle here). This is the
// whole of P0.6's ordering work; the deterministic total order is Phase 4.
func detectOrderingCycle(mechanisms []Mechanism) error {
	if len(mechanisms) == 0 {
		return nil
	}

	known := make(map[MechanismID]bool, len(mechanisms))
	for _, m := range mechanisms {
		known[m.Descriptor().ID] = true
	}

	adjacency := make(map[MechanismID][]MechanismID, len(mechanisms))
	for _, m := range mechanisms {
		id := m.Descriptor().ID
		ordering := m.Ordering()
		for _, before := range ordering.Before {
			if known[before] {
				adjacency[id] = append(adjacency[id], before)
			}
		}
		for _, after := range ordering.After {
			if known[after] {
				adjacency[after] = append(adjacency[after], id)
			}
		}
	}

	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	color := make(map[MechanismID]int, len(mechanisms))

	var hasCycleFrom func(node MechanismID) bool
	hasCycleFrom = func(node MechanismID) bool {
		color[node] = onStack
		for _, next := range adjacency[node] {
			switch color[next] {
			case onStack:
				return true
			case unvisited:
				if hasCycleFrom(next) {
					return true
				}
			}
		}
		color[node] = done
		return false
	}

	for id := range known {
		if color[id] == unvisited && hasCycleFrom(id) {
			return ErrOrderingCycle
		}
	}
	return nil
}
