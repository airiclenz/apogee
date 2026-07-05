package domain

import (
	"fmt"
	"sort"
)

// This file holds the MechanismRegistry's pure logic the methods in mechanism.go
// delegate to: hook-interface assertions, the startup ordering-cycle check, the
// deterministic per-hook-point total order, and the incompatibility gate (ADR 0003).

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

// topoSort returns mechs in the deterministic total order the loop dispatches them: a
// topological sort of their Before/After constraints with a stable tiebreak by canonical
// MechanismID (ADR 0003 / D4), so a shuffled registration order yields identical output. An
// edge u→v ("u before v") comes from u's Before=[v] or v's After=[u]; a constraint naming an ID
// not in mechs is ignored (ordering is relative to the co-located Mechanisms only). The graph is
// validated acyclic at construction (detectOrderingCycle), so Kahn's algorithm below always
// drains; a defensively-detected leftover cycle appends its members in ID order rather than
// looping, keeping the result deterministic.
func topoSort(mechs []Mechanism) []Mechanism {
	if len(mechs) <= 1 {
		return mechs
	}

	byID := make(map[MechanismID]Mechanism, len(mechs))
	ids := make([]MechanismID, 0, len(mechs))
	for _, m := range mechs {
		id := m.Descriptor().ID
		byID[id] = m
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	present := make(map[MechanismID]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}
	indegree := make(map[MechanismID]int, len(ids))
	successors := make(map[MechanismID][]MechanismID, len(ids))
	addEdge := func(before, after MechanismID) {
		successors[before] = append(successors[before], after)
		indegree[after]++
	}
	for _, id := range ids {
		ord := byID[id].Ordering()
		for _, b := range ord.Before {
			if present[b] {
				addEdge(id, b)
			}
		}
		for _, aft := range ord.After {
			if present[aft] {
				addEdge(aft, id)
			}
		}
	}

	// Kahn's algorithm, always taking the lowest-ID ready node (ids is sorted, so the first
	// non-emitted in-degree-0 id is the smallest available) — the stable tiebreak of D4.
	out := make([]Mechanism, 0, len(ids))
	emitted := make(map[MechanismID]bool, len(ids))
	for len(out) < len(ids) {
		next, found := MechanismID(""), false
		for _, id := range ids {
			if !emitted[id] && indegree[id] == 0 {
				next, found = id, true
				break
			}
		}
		if !found {
			// A cycle slipped past validation (should not happen): emit the remainder in ID
			// order so the result is deterministic instead of spinning.
			for _, id := range ids {
				if !emitted[id] {
					out = append(out, byID[id])
					emitted[id] = true
				}
			}
			break
		}
		out = append(out, byID[next])
		emitted[next] = true
		for _, v := range successors[next] {
			indegree[v]--
		}
	}
	return out
}

// detectIncompatibility reports ErrIncompatibleMechanisms if two registered Mechanisms
// declare each other incompatible (MechanismDescriptor.IncompatibleWith). Incompatibility is
// GLOBAL, not per-hook-point: two Mechanisms that must never co-fire — e.g. read_loop and
// cached_content_intercept, which sit at different hook points — cannot both be enabled. A
// declaration is directional in the data but symmetric in effect (either side naming the other
// trips it), so it fails loudly at startup the same way an ordering cycle does (ADR 0003).
func detectIncompatibility(mechanisms []Mechanism) error {
	if len(mechanisms) < 2 {
		return nil
	}
	present := make(map[MechanismID]bool, len(mechanisms))
	for _, m := range mechanisms {
		present[m.Descriptor().ID] = true
	}
	for _, m := range mechanisms {
		desc := m.Descriptor()
		for _, other := range desc.IncompatibleWith {
			if present[other] {
				return fmt.Errorf("apogee: mechanisms %q and %q: %w", desc.ID, other, ErrIncompatibleMechanisms)
			}
		}
	}
	return nil
}

// detectRequirements reports ErrMissingRequirement if any registered Mechanism declares a required
// peer (MechanismDescriptor.Requires) absent from the registry — the dual of detectIncompatibility.
// A requirement chain (A requires B, B requires C) is validated transitively by iteration, not
// recursion: every Mechanism's direct requirements are checked against the present set, so a broken
// link anywhere in the chain trips (checking B catches an absent C independently of A). The error
// names both IDs and the reason so the config author sees which stack to complete. It fails loudly
// at startup the same way an ordering cycle or an incompatibility does (ADR 0003; ADR 0014 §4).
func detectRequirements(mechanisms []Mechanism) error {
	present := make(map[MechanismID]bool, len(mechanisms))
	for _, m := range mechanisms {
		present[m.Descriptor().ID] = true
	}
	for _, m := range mechanisms {
		desc := m.Descriptor()
		for _, req := range desc.Requires {
			if !present[req] {
				return fmt.Errorf("apogee: mechanism %q requires %q — enable both or neither; they are benched as a stack: %w", desc.ID, req, ErrMissingRequirement)
			}
		}
	}
	return nil
}
