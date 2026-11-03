package agent

import (
	"fmt"
	"strings"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// The request room's arithmetic as tables, built with no Agent (plan 2026-11-02 - 05, item 2). The
// fixture budget reads 4 chars per token, an 8192-token window less a 1024-token reply reserve —
// a 7168-token room — and a 4000-token History allocation, so a fresh result's share is clamped
// to [500, 4000].
const (
	roomTestCharsPerToken = 4
	roomTestWindow        = 8192
	roomTestReserve       = 1024
	roomTestRoom          = roomTestWindow - roomTestReserve
	roomTestHistory       = 4000
	roomTestFloor         = roomTestHistory / freshResultFloorDivisor
)

// roomTestBudget is the fixture budget; used is Budget.Used, zero for an uncalibrated ratio.
func roomTestBudget(used int) domain.Budget {
	return domain.Budget{
		Window:          roomTestWindow,
		ContextLimit:    roomTestWindow,
		Used:            used,
		CharsPerToken:   roomTestCharsPerToken,
		ResponseReserve: roomTestReserve,
		History:         roomTestHistory,
	}
}

// roomTestMessages is one user message weighing exactly tokens at the fixture ratio.
func roomTestMessages(tokens int) []domain.Message {
	return []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("x", tokens*roomTestCharsPerToken)}}
}

func TestRequestRoomWeightCountsStandingAndMenu(t *testing.T) {
	t.Parallel()

	standing := strings.Repeat("s", 100*roomTestCharsPerToken)
	menu := []domain.ToolDef{{Name: "read_file", Description: strings.Repeat("d", 100*roomTestCharsPerToken-len("read_file"))}}
	tests := []struct {
		name     string
		standing string
		menu     []domain.ToolDef
		msgs     []domain.Message
		want     int
	}{
		{name: "empty request", want: 0},
		{name: "messages alone", msgs: roomTestMessages(300), want: 300},
		{name: "standing seeded ahead", standing: standing, msgs: roomTestMessages(300), want: 400},
		{name: "standing and menu", standing: standing, menu: menu, msgs: roomTestMessages(300), want: 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRequestRoom(roomTestBudget(1), tt.standing, tt.menu)
			if got := r.weight(tt.msgs); got != tt.want {
				t.Errorf("weight = %d, want %d", got, tt.want)
			}
			if got, want := r.left(tt.msgs), roomTestRoom-tt.want; got != want {
				t.Errorf("left = %d, want %d", got, want)
			}
		})
	}
}

func TestRequestRoomFreshShare(t *testing.T) {
	t.Parallel()

	standing := strings.Repeat("s", 200*roomTestCharsPerToken)
	unknown := domain.Budget{CharsPerToken: roomTestCharsPerToken, Used: 1}
	workingOnly := unknown
	workingOnly.History = 2000
	tests := []struct {
		name     string
		budget   domain.Budget
		standing string
		msgs     []domain.Message
		pending  int
		want     int
	}{
		{name: "ceiling: an empty request's room is held under History", budget: roomTestBudget(1), pending: 1, want: roomTestHistory},
		{name: "share of the room left", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom - 3000), pending: 1, want: 3000},
		{name: "standing content spends the room", budget: roomTestBudget(1), standing: standing, msgs: roomTestMessages(roomTestRoom - 3200), pending: 1, want: 3000},
		{name: "zero pending reads as one", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom - 3000), pending: 0, want: 3000},
		{name: "split across two pending", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom - 3000), pending: 2, want: 1500},
		{name: "split across three pending", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom - 3000), pending: 3, want: 1000},
		{name: "floor: a thin share is held at 1/8 of History", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom - 3000), pending: 10, want: roomTestFloor},
		{name: "floor: no room left at all", budget: roomTestBudget(1), msgs: roomTestMessages(roomTestRoom + 500), pending: 1, want: roomTestFloor},
		{name: "unknown window: the fold's conservative ceiling", budget: unknown, msgs: roomTestMessages(roomTestRoom), pending: 4, want: compactUnknownWindowTranscriptTokens},
		{name: "unknown window: a working-window History", budget: workingOnly, pending: 4, want: 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRequestRoom(tt.budget, tt.standing, nil)
			if got := r.freshShare(tt.msgs, tt.pending); got != tt.want {
				t.Errorf("freshShare(pending %d) = %d, want %d", tt.pending, got, tt.want)
			}
		})
	}
}

// The share rolls forward: the room a small result leaves unused is there for the next one, because
// the share is re-weighed over the conversation the earlier result was appended to.
func TestRequestRoomFreshShareRollsForward(t *testing.T) {
	t.Parallel()

	r := newRequestRoom(roomTestBudget(1), "", nil)
	msgs := roomTestMessages(roomTestRoom - 3000)
	first := r.freshShare(msgs, 2)
	small := domain.Message{Role: domain.RoleTool, Content: strings.Repeat("r", 100*roomTestCharsPerToken)}
	second := r.freshShare(append(msgs, small), 1)
	if first != 1500 {
		t.Fatalf("first share = %d, want 1500", first)
	}
	if want := 3000 - 100; second != want {
		t.Errorf("second share = %d, want %d: the room the small first result left did not roll forward", second, want)
	}
}

func TestRequestRoomExceeds(t *testing.T) {
	t.Parallel()

	unknown := func(used int) domain.Budget {
		return domain.Budget{CharsPerToken: roomTestCharsPerToken, Used: used}
	}
	margin := uncalibratedRoomMargin
	fallback := compactUnknownWindowTranscriptTokens
	tests := []struct {
		name   string
		budget domain.Budget
		req    []domain.Message
		conv   []domain.Message
		want   bool
	}{
		{name: "calibrated: a request filling the room fits", budget: roomTestBudget(1), req: roomTestMessages(roomTestRoom), want: false},
		{name: "calibrated: one token past the room", budget: roomTestBudget(1), req: roomTestMessages(roomTestRoom + 1), want: true},
		{name: "uncalibrated: past the room but inside the margin", budget: roomTestBudget(0), req: roomTestMessages(roomTestRoom + 1), want: false},
		{name: "uncalibrated: past the margin", budget: roomTestBudget(0), req: roomTestMessages(margin*roomTestRoom + 1), want: true},
		{name: "known window weighs the request, not the conversation", budget: roomTestBudget(1), req: roomTestMessages(10), conv: roomTestMessages(10 * roomTestRoom), want: false},
		{name: "unknown window weighs the conversation, not the request", budget: unknown(1), req: roomTestMessages(10 * fallback), conv: roomTestMessages(10), want: false},
		{name: "unknown window: conversation past the fallback", budget: unknown(1), conv: roomTestMessages(fallback + 1), want: true},
		{name: "unknown window, uncalibrated: inside the margin", budget: unknown(0), conv: roomTestMessages(fallback + 1), want: false},
		{name: "unknown window, uncalibrated: past the margin", budget: unknown(0), conv: roomTestMessages(margin*fallback + 1), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRequestRoom(tt.budget, "", nil)
			req := domain.NewRequest("m", tt.req, nil, tt.budget, 0)
			if got := r.exceeds(req, tt.conv); got != tt.want {
				t.Errorf("exceeds = %v, want %v", got, tt.want)
			}
		})
	}
}

// fit renders exactly the clamp it was moved from: the line elision when that brings the body under
// bound, the byte elision when it does not, and the body untouched when it is within bound.
func TestRequestRoomFit(t *testing.T) {
	t.Parallel()

	const bound = 500
	maxChars := bound * roomTestCharsPerToken
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %05d", i)
	}
	manyLines := strings.Join(lines, "\n")
	longLine := strings.Repeat("{\"k\":1},", 20_000)
	within := strings.Repeat("w", maxChars)
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "within bound passes untouched", content: within, want: within},
		{name: "many-line body takes the line elision", content: manyLines, want: apogeectx.TruncateToolResult(manyLines, maxChars)},
		{name: "long-line body takes the byte elision", content: longLine, want: apogeectx.ElideToBytes(longLine, maxChars)},
	}
	r := newRequestRoom(roomTestBudget(1), "", nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := r.fit(tt.content, bound)
			if got != tt.want {
				t.Errorf("fit rendered %d bytes, want the %d-byte rendering byte for byte", len(got), len(tt.want))
			}
			if tokens := r.budget.EstimateTokens(len(got)); tokens > bound {
				t.Errorf("fit left %d tokens, over the %d bound", tokens, bound)
			}
		})
	}
}
