package domain

import (
	"encoding/json"
	"fmt"
)

// ----------------------------------------------------------------------------
// Sessions (ADR 0001 — snapshot/resume is the user feature; the bench composes fork)
// ----------------------------------------------------------------------------

// SessionVersion is the schema version Snapshot stamps and Resume/DecodeSession
// accept. P0.6 ships v1; a snapshot whose Version exceeds this is from a newer build
// and is rejected with ErrSessionVersion (ADR 0001 — no silent forward migration).
//
// Domain owns the Session *envelope* and its versioning; the engine (internal/agent)
// owns the opaque State payload — it serializes engine state (conversation, and from
// P1.6 the loop counters and deferred actions), so its schema lives with the engine.
const SessionVersion = 1

// Session is the serializable, copyable conversation state — no live handles, no
// process globals (ADR 0001). Deep-copying it yields an independent branch (the
// bench's fork primitive); Encode/Decode persist it (the user's resume feature).
type Session struct {
	Version int             // schema version; Resume rejects an unknown future version
	State   json.RawMessage // opaque serialized conversation state
}

// Encode serializes the session for storage.
func (s Session) Encode() ([]byte, error) { return json.Marshal(s) }

// DecodeSession deserializes a session, returning ErrSessionVersion if the schema
// version is newer than this build understands.
func DecodeSession(data []byte) (Session, error) {
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, fmt.Errorf("apogee: decode session: %w", err)
	}
	if s.Version > SessionVersion {
		return Session{}, ErrSessionVersion
	}
	return s, nil
}
