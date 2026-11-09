package sidepanel

import (
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// mcpEvent is one MCPServerEvent for server in phase, a stdio global server unless the caller
// sets more.
func mcpEvent(server string, phase domain.MCPServerPhase) domain.MCPServerEvent {
	return domain.MCPServerEvent{Server: server, Transport: "stdio", Phase: phase}
}

func TestMCPServersFold(t *testing.T) {
	t.Parallel()

	connected := mcpEvent("files", domain.MCPServerConnected)
	connected.Tools = 7
	failedKept := mcpEvent("files", domain.MCPServerFailed)
	failedKept.Cause, failedKept.Kept = "dial refused", true
	failedLost := mcpEvent("files", domain.MCPServerFailed)
	failedLost.Cause = "dial refused"
	removed := mcpEvent("files", domain.MCPServerDisconnected)
	removed.Reason = domain.MCPDisconnectRemoved
	denied := mcpEvent("files", domain.MCPServerDisconnected)
	denied.Reason = domain.MCPDisconnectDenied
	project := mcpEvent("web", domain.MCPServerDialling)
	project.Transport, project.Project = "sse", true
	escaped := mcpEvent("ev\x1b[31mil", domain.MCPServerFailed)
	escaped.Transport, escaped.Cause = "std\x1b]0;x\x07io", "no\x1b[2Jpe"

	tests := []struct {
		name     string
		events   []domain.MCPServerEvent
		wantRows []MCPServerRow
		wantLive bool
	}{
		{
			name: "nothing folded holds no row and is not live",
		},
		{
			name:     "dialling opens a live row",
			events:   []domain.MCPServerEvent{mcpEvent("files", domain.MCPServerDialling)},
			wantRows: []MCPServerRow{{Server: "files", Transport: "stdio", Phase: domain.MCPServerDialling}},
			wantLive: true,
		},
		{
			name:     "connected carries the tool count and is not live",
			events:   []domain.MCPServerEvent{mcpEvent("files", domain.MCPServerDialling), connected},
			wantRows: []MCPServerRow{{Server: "files", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 7}},
		},
		{
			name:   "a reconnect's dialling keeps the serving connection's tools",
			events: []domain.MCPServerEvent{connected, mcpEvent("files", domain.MCPServerDialling)},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerDialling, Tools: 7,
			}},
			wantLive: true,
		},
		{
			name:   "a failed reconnect with the previous connection kept keeps its tools",
			events: []domain.MCPServerEvent{connected, mcpEvent("files", domain.MCPServerDialling), failedKept},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerFailed, Tools: 7,
				Cause: "dial refused", Kept: true,
			}},
			wantLive: true,
		},
		{
			name:   "a failure with no connection kept drops the tools",
			events: []domain.MCPServerEvent{connected, failedLost},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerFailed, Cause: "dial refused",
			}},
			wantLive: true,
		},
		{
			name:   "a connect after a failure clears the cause",
			events: []domain.MCPServerEvent{failedLost, connected},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 7,
			}},
		},
		{
			name:     "a removed server drops its row",
			events:   []domain.MCPServerEvent{connected, removed},
			wantRows: []MCPServerRow{},
		},
		{
			name:     "removing a server never reported is a no-op",
			events:   []domain.MCPServerEvent{removed},
			wantRows: []MCPServerRow{},
		},
		{
			name:   "a denied server keeps a disconnected row naming why",
			events: []domain.MCPServerEvent{connected, denied},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerDisconnected,
				Reason: domain.MCPDisconnectDenied,
			}},
			wantLive: true,
		},
		{
			name:   "servers keep the order they were first reported in",
			events: []domain.MCPServerEvent{connected, project, mcpEvent("files", domain.MCPServerDialling)},
			wantRows: []MCPServerRow{
				{Server: "files", Transport: "stdio", Phase: domain.MCPServerDialling, Tools: 7},
				{Server: "web", Transport: "sse", Project: true, Phase: domain.MCPServerDialling},
			},
			wantLive: true,
		},
		{
			name:   "one connected server among unconnected ones leaves the fold live",
			events: []domain.MCPServerEvent{connected, project},
			wantRows: []MCPServerRow{
				{Server: "files", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 7},
				{Server: "web", Transport: "sse", Project: true, Phase: domain.MCPServerDialling},
			},
			wantLive: true,
		},
		{
			name:   "server, transport and cause are escape-stripped",
			events: []domain.MCPServerEvent{escaped},
			wantRows: []MCPServerRow{{
				Server: "evil", Transport: "stdio", Phase: domain.MCPServerFailed, Cause: "nope",
			}},
			wantLive: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var servers MCPServers
			for _, event := range tc.events {
				servers.Fold(event)
			}
			want := tc.wantRows
			if want == nil {
				want = []MCPServerRow{}
			}
			if got := servers.Rows(); !slices.Equal(got, want) {
				t.Errorf("Rows() = %+v, want %+v", got, want)
			}
			if got := servers.IsLive(); got != tc.wantLive {
				t.Errorf("IsLive() = %v, want %v", got, tc.wantLive)
			}
		})
	}
}

// TestMCPServersFoldLeavesACopyAlone pins the value-copy posture the TUI's Model relies on (ADR
// 0011): folding into one copy never moves what another copy answers.
func TestMCPServersFoldLeavesACopyAlone(t *testing.T) {
	t.Parallel()
	var original MCPServers
	original.Fold(mcpEvent("files", domain.MCPServerDialling))
	original.Fold(mcpEvent("web", domain.MCPServerDialling))
	want := original.Rows()

	updated := original
	connected := mcpEvent("files", domain.MCPServerConnected)
	connected.Tools = 3
	updated.Fold(connected)
	removed := mcpEvent("web", domain.MCPServerDisconnected)
	removed.Reason = domain.MCPDisconnectRemoved
	updated.Fold(removed)

	if got := original.Rows(); !slices.Equal(got, want) {
		t.Errorf("the copy's fold moved the original: Rows() = %+v, want %+v", got, want)
	}
	if got := updated.Rows(); len(got) != 1 || got[0].Phase != domain.MCPServerConnected {
		t.Errorf("the folded copy answers %+v, want files connected alone", got)
	}
}
