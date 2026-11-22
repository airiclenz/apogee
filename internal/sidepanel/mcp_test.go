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
	lost := mcpEvent("files", domain.MCPServerDisconnected)
	lost.Reason, lost.Cause = domain.MCPDisconnectLost, "server exited: exit status 1"
	lostEscaped := mcpEvent("files", domain.MCPServerDisconnected)
	lostEscaped.Reason, lostEscaped.Cause = domain.MCPDisconnectLost, "ex\x1b[31mited"
	deniedWithCause := denied
	deniedWithCause.Cause = "ignored"
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
			name:   "a lost server keeps a disconnected row with no tools and the ending's cause",
			events: []domain.MCPServerEvent{connected, lost},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerDisconnected,
				Reason: domain.MCPDisconnectLost, Cause: "server exited: exit status 1",
			}},
			wantLive: true,
		},
		{
			name:   "a lost server's cause is escape-stripped",
			events: []domain.MCPServerEvent{connected, lostEscaped},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerDisconnected,
				Reason: domain.MCPDisconnectLost, Cause: "exited",
			}},
			wantLive: true,
		},
		{
			name:   "a disconnect closed on purpose carries no cause",
			events: []domain.MCPServerEvent{connected, deniedWithCause},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerDisconnected,
				Reason: domain.MCPDisconnectDenied,
			}},
			wantLive: true,
		},
		{
			name:   "a connect after a loss clears the cause",
			events: []domain.MCPServerEvent{connected, lost, connected},
			wantRows: []MCPServerRow{{
				Server: "files", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 7,
			}},
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
	wantEvents := []domain.MCPServerEvent{
		mcpEvent("files", domain.MCPServerDialling), mcpEvent("web", domain.MCPServerDialling),
	}
	if got := original.Events(); !slices.Equal(got, wantEvents) {
		t.Errorf("the copy's fold moved the original: Events() = %+v, want %+v", got, wantEvents)
	}
	if got := updated.Rows(); len(got) != 1 || got[0].Phase != domain.MCPServerConnected {
		t.Errorf("the folded copy answers %+v, want files connected alone", got)
	}
}

// TestMCPServersEventsReplayTheFold: Events restates the stream folded so far as Events a fold
// replays to the same rows the whole stream folds to — a dialling or kept-failed server keeps the
// serving connection's tool count through a connected Event ahead of its last phase, a server
// removed from the configuration drops out, and the servers keep the order they were first reported.
func TestMCPServersEventsReplayTheFold(t *testing.T) {
	t.Parallel()
	connected := func(server string, tools int) domain.MCPServerEvent {
		event := mcpEvent(server, domain.MCPServerConnected)
		event.Tools = tools
		return event
	}
	kept := mcpEvent("kept", domain.MCPServerFailed)
	kept.Cause, kept.Kept = "refused", true
	lost := mcpEvent("lost", domain.MCPServerFailed)
	lost.Cause = "refused"
	removed := mcpEvent("gone", domain.MCPServerDisconnected)
	removed.Reason = domain.MCPDisconnectRemoved
	denied := mcpEvent("closed", domain.MCPServerDisconnected)
	denied.Reason = domain.MCPDisconnectDenied
	stream := []domain.MCPServerEvent{
		connected("dialling", 5), connected("kept", 3), connected("lost", 2), connected("gone", 1),
		connected("closed", 4), mcpEvent("dialling", domain.MCPServerDialling), kept, lost, removed, denied,
	}

	var whole MCPServers
	for _, event := range stream {
		whole.Fold(event)
	}
	seed := whole.Events()
	var seeded MCPServers
	for _, event := range seed {
		seeded.Fold(event)
	}

	if got, want := seeded.Rows(), whole.Rows(); !slices.Equal(got, want) {
		t.Errorf("the seed folds to %+v; want the whole stream's %+v", got, want)
	}
	want := []domain.MCPServerEvent{
		connected("dialling", 5), mcpEvent("dialling", domain.MCPServerDialling),
		connected("kept", 3), kept, lost, denied,
	}
	if !slices.Equal(seed, want) {
		t.Errorf("Events() = %+v; want %+v", seed, want)
	}
}

// TestMCPServersEventsKeepRawText: Events restates the Events' own text, escapes and all, for the
// journal and the seed to record, while Rows strips the same text for display.
func TestMCPServersEventsKeepRawText(t *testing.T) {
	t.Parallel()
	escaped := mcpEvent("ev\x1b[31mil", domain.MCPServerFailed)
	escaped.Transport, escaped.Cause = "std\x1b]0;x\x07io", "no\x1b[2Jpe"
	var servers MCPServers
	servers.Fold(escaped)

	if got, want := servers.Events(), []domain.MCPServerEvent{escaped}; !slices.Equal(got, want) {
		t.Errorf("Events() = %+v; want the raw %+v", got, want)
	}
	wantRows := []MCPServerRow{{Server: "evil", Transport: "stdio", Phase: domain.MCPServerFailed, Cause: "nope"}}
	if got := servers.Rows(); !slices.Equal(got, wantRows) {
		t.Errorf("Rows() = %+v; want the stripped %+v", got, wantRows)
	}
}
