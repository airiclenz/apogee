package notice

// ServerOffline words the refusal a Driver hands its user when the startup beat never answered:
// the endpoint the session is bound to and, when the observation had words for it, the failure's
// own. It is the ONE spelling of that sentence — the TUI's transcript note, the headless CLI's
// never-started error and a daemon Firing's failure all read the same line, so a human who has
// watched a session refuse a send reads it again from an unattended run.
//
// Only the wording lives here. Each caller keeps its own guard (the TUI's live monitor state, the
// two unattended Drivers' one-shot beat), its own endpoint and failure sources, and its own
// delivery — those three differ, and the sentence does not.
//
// An empty failure yields the endpoint alone, with no trailing colon: that is the honest answer
// when nothing was observed and there is nothing to say about it, and the endpoint is the one
// fact a reader acts on either way.
func ServerOffline(endpoint, failure string) string {
	note := "cannot send — server offline (" + endpoint + ")"
	if failure != "" {
		note += ": " + failure
	}
	return note
}

// UnresolvedHost words the failure detail ServerOffline carries when the beat never reached the
// server because its host name did not resolve. A bare `lookup … no such host` behind "server
// offline" reads as a dead box; naming the host and the two ways out — its IP address, or an
// /etc/hosts entry — tells the reader the box may be fine and the name is what failed.
func UnresolvedHost(name string) string {
	return "host name " + name + " did not resolve — use the server's IP address or add it to /etc/hosts"
}
