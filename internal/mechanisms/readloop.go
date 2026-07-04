package mechanisms

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// read_loop registers the read-loop detector in the catalogue constructor table (Phase-4 item 11,
// Wave 3 history-aware family). Default-off (D1). It is ported from apogee-sim
// internal/proxy/read_loop_detector.go @pin and CONSOLIDATES the sim's three variants
// (read_loop_detector, greenfield_read_loop_detector, successful_read_loop_detector) into one
// pre-request Mechanism (catalogue C2): the sim split them only to give each an independent
// suppression counter and declared them pairwise-incompatible so one fires per request, dispatched
// by readLoopCandidate on the greenfield signal. apogee folds that split into internal branch
// selection — the failed-read branch (greenfield or normal) first, else the successful-read branch,
// reproducing the sim proxy's priority chain (proxy.go:476-516 @pin).
func init() { catalogue[readLoopID] = newReadLoop }

const readLoopID domain.MechanismID = "read_loop"

// successfulReadLoopThreshold is the number of times the same file must be read WITHOUT a write
// before the successful-read-loop branch fires (apogee-sim successfulReadLoopThreshold @pin).
const successfulReadLoopThreshold = 3

// readLoopMechanism is the pre-request Mechanism that injects a role-safe hint when the model is
// stuck re-reading files (catalogue Table A `read_loop`). It carries no per-Mechanism state;
// strikes-3 self-regulation routes through the loop's per-Session tracker (item 3).
type readLoopMechanism struct{}

// newReadLoop builds the read_loop Mechanism. It needs no injected Deps (D3): the loop is detected
// entirely from the conversation on the Request it is handed.
func newReadLoop(Deps) (domain.Mechanism, error) { return readLoopMechanism{}, nil }

// Descriptor identifies read_loop as a strikes-3 proactive-nudge Mechanism (catalogue Table A) —
// disabled under Bypass (D5), withdrawn after repeated non-help.
func (readLoopMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          readLoopID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
		// The re-read family is pairwise-exclusive on the same wasted-read symptom (catalogue
		// Table A / C2): in apogee IncompatibleWith is a startup gate, so at most one of the three
		// may be enabled at a time (the sim's per-request exclusivity becomes per-config).
		IncompatibleWith: []domain.MechanismID{cachedContentInterceptID, readRepeatID},
	}
}

// Ordering declares no positive edge (catalogue Table A: the incompatibility edges above are the
// only constraint); read_loop is a request-prep injector with no hard order against the other
// pre-request shapers.
func (readLoopMechanism) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }

// PreRequest injects the appropriate read-loop hint, role-safe and idempotent, when the model is
// failing to read the same file repeatedly (greenfield or normal) or re-reading the same file
// without acting on it. It is a no-op — booking no fire (the loop keys the acted fire on
// Request.Revision, R4) — when no loop is detected or the hint is already present in the request.
func (readLoopMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	conv := req.View().Conversation()

	// Failed-read branch: greenfield (a single miss on an empty workspace) or normal (two misses).
	greenfield := isGreenfieldContext(conv)
	if paths := detectReadLoopPaths(conv, greenfield); len(paths) > 0 {
		nextStep := ""
		if !greenfield {
			nextStep = deriveWriteTarget(conv)
		}
		injectHint(req, conv, buildReadLoopHint(paths, greenfield, nextStep))
		return nil
	}

	// Successful-read branch: the same file read three times without a write.
	if counts := detectSuccessfulReadLoopPaths(conv); len(counts) > 0 {
		injectHint(req, conv, buildSuccessfulReadLoopHint(counts, deriveWriteTarget(conv)))
	}
	return nil
}

// injectHint role-safe-injects hint into req unless it is already present (idempotency — the hint
// is its own marker; see requestContains).
func injectHint(req *domain.Request, conv domain.ConversationView, hint string) {
	if requestContains(conv, hint) {
		return
	}
	req.InjectContext(hint)
}

// isGreenfieldContext reports whether the conversation shows no evidence of an existing workspace —
// no successful read, no write, and any listing came back empty (apogee-sim isGreenfieldContext
// @pin). When true, a repeated read miss is almost certainly the model hunting for a file that has
// not been created yet, and the blunter greenfield hint (threshold 1) is warranted.
func isGreenfieldContext(conv domain.ConversationView) bool {
	greenfield := true
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			return true
		}
		for _, tc := range m.ToolCalls {
			switch {
			case isFileMutatingTool(tc.Tool):
				greenfield = false
			case isReadTool(tc.Tool) && !resultIsReadError(conv, tc.ID):
				greenfield = false
			case isListTool(tc.Tool) && !listResultEmpty(conv, tc.ID):
				greenfield = false
			}
			if !greenfield {
				return false
			}
		}
		return true
	})
	return greenfield
}

// listResultEmpty reports whether the listing paired with callID indicates an empty directory
// (apogee-sim isListResultEmpty @pin). A missing result is treated as non-empty — better to miss
// the greenfield signal than mis-fire on a listing still in flight.
func listResultEmpty(conv domain.ConversationView, callID string) bool {
	res, _, ok := conv.ResultFor(callID)
	if !ok {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(res.Content))
	return lower == "" || lower == "[]" ||
		strings.Contains(lower, "empty directory") || strings.Contains(lower, "no files")
}

// detectReadLoopPaths returns, sorted for deterministic hints, the paths the model has failed to
// read at least threshold times (apogee-sim detectReadLoopPaths @pin) — threshold 1 in a greenfield
// workspace (the file plainly does not exist yet), 2 otherwise ("check then create" is legitimate
// once). The sim returned map-iteration order; apogee sorts for reproducibility (bench).
func detectReadLoopPaths(conv domain.ConversationView, greenfield bool) []string {
	failed := make(map[string]int)
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			return true
		}
		for _, tc := range m.ToolCalls {
			if !isReadTool(tc.Tool) {
				continue
			}
			p := toolCallPath(tc.Arguments)
			if p == "" {
				continue
			}
			if resultIsReadError(conv, tc.ID) {
				failed[p]++
			}
		}
		return true
	})

	threshold := 2
	if greenfield {
		threshold = 1
	}
	var paths []string
	for p, n := range failed {
		if n >= threshold {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

// detectSuccessfulReadLoopPaths returns, per path, how many times the model has SUCCESSFULLY read a
// file without ever writing it — decrementing on a write so a read-then-write does not count
// (apogee-sim detectSuccessfulReadLoopPaths @pin). Only paths at or above the threshold survive.
func detectSuccessfulReadLoopPaths(conv domain.ConversationView) map[string]int {
	counts := make(map[string]int)
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			return true
		}
		for _, tc := range m.ToolCalls {
			p := toolCallPath(tc.Arguments)
			if p == "" {
				continue
			}
			np := normalizePath(p)
			switch {
			case isReadTool(tc.Tool) && !resultIsReadError(conv, tc.ID):
				counts[np]++
			case isFileMutatingTool(tc.Tool) && counts[np] > 0:
				counts[np]--
			}
		}
		return true
	})
	for p, n := range counts {
		if n < successfulReadLoopThreshold {
			delete(counts, p)
		}
	}
	return counts
}

// buildReadLoopHint renders the failed-read hint (apogee-sim buildReadLoopHint @pin), blunt in a
// greenfield workspace (soft phrasing was observed ignored for 10+ turns) and pointing at a derived
// write target when one is available.
func buildReadLoopHint(paths []string, greenfield bool, nextStep string) string {
	var b strings.Builder
	if greenfield {
		if len(paths) == 1 {
			fmt.Fprintf(&b, "STOP. The workspace is empty. The file %q does not exist because nothing has been created yet. ", paths[0])
		} else {
			fmt.Fprintf(&b, "STOP. The workspace is empty. The files %s do not exist because nothing has been created yet. ", strings.Join(paths, ", "))
		}
		b.WriteString("This is a fresh task — there is nothing to read. ")
		b.WriteString("Call write_file now to create the file. Do not call read_file again until you have written something.")
		return b.String()
	}
	if len(paths) == 1 {
		fmt.Fprintf(&b, "The file %q does not exist yet. ", paths[0])
	} else {
		fmt.Fprintf(&b, "The files %s do not exist yet. ", strings.Join(paths, ", "))
	}
	b.WriteString("You have tried to read it multiple times and gotten errors. ")
	if nextStep != "" && !pathInList(nextStep, paths) {
		fmt.Fprintf(&b, "Use write_file to create %q now instead of reading again.", nextStep)
	} else {
		b.WriteString("Use write_file to create the file instead of trying to read it again.")
	}
	return b.String()
}

// buildSuccessfulReadLoopHint renders the re-read-without-acting hint (apogee-sim
// buildSuccessfulReadLoopHint @pin), one line per path in sorted order (the sim iterated the map;
// apogee sorts for reproducibility).
func buildSuccessfulReadLoopHint(counts map[string]int, nextStep string) string {
	paths := make([]string, 0, len(counts))
	for p := range counts {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "You have read %q %d times without making changes. ", p, counts[p])
		if nextStep != "" && normalizePath(nextStep) != normalizePath(p) {
			fmt.Fprintf(&b, "Stop re-reading and call write_file to create %q now.", nextStep)
		} else {
			b.WriteString("If you need to modify this file, use write_file or edit_file now. ")
			b.WriteString("If you need information from a different file, read that file instead.")
		}
	}
	return b.String()
}
