// Package session persists conversations as id-addressed Records — the storage layer the
// history browser lists, resumes, renames, and deletes, and the composition root writes to
// after every Turn.
//
// A Record wraps two opaque payloads in browsable Meta: the engine's Session envelope
// (internal/domain, versioned and resumed by the engine) and the Transcript blob — the
// scrollback, versioned HERE by TranscriptVersion (transcript.go). Each payload owns and
// forward-rejects only its own version; this package versions the wrapper (RecordVersion) and
// rejects a newer wrapper with ErrRecordVersion, and it versions the scrollback separately and
// rejects a newer blob with ErrTranscriptVersion. The store itself still treats both payloads as
// opaque bytes.
//
// transcript.go holds the neutral transcript model and codec — the exported wire form of a
// scrollback (Entry and its views), TranscriptVersion and EncodeTranscript/DecodeTranscript — so
// any Driver, not the TUI alone, can write and replay the Transcript blob (ADR 0031). What a
// replay makes of a call the record left open is the reading Driver's own pass over its entries
// (the TUI's closeInterruptedCalls, internal/tui/transcriptbridge.go).
//
// The Store owns the on-disk format and naming so its callers never duplicate that
// knowledge. Ids from NewID are sortable UTC stamps with a random suffix; Save writes
// atomically (temp file + rename) under <id>.json and updates in place; List/Load skip or
// wrap files they cannot read as records, so pre-plan bare domain.Session files still list,
// load, and resume, and one corrupt file never kills the browser.
//
// An id is a filename, so every id crossing the Store — decoded from a file, saved, loaded,
// held or deleted — must be a single safe path component or it is refused with ErrInvalidID. A
// record read by path declares its own id, and without that gate a planted file would aim
// Apogee's autosaves and deletes anywhere the user can write. The same file is untrusted in its
// size: a record over maxRecordBytes (256 MiB) is refused with ErrRecordTooLarge at the read, and
// a transcript blob over the same bound with ErrTranscriptTooLarge, before any unmarshal — the
// byte cap is what bounds the decode's allocation, not any check inside it.
//
// A session has one live instance. Store.Hold takes the exclusive OS lock on <id>.lock beside the
// record (internal/platform.AcquireLock — kernel-owned, so a dead holder leaves nothing stale) and
// keeps it until released; the Driver running a record holds it from the record's birth to the end
// of the run, and every door that would open the same record in another apogee — a --resume or
// --continue start, the browser's delete — asks first and is refused with a
// *HeldError, whose Error() is the one line those doors print. Delete and Prune hold before they
// remove, so a record another instance is running is never swept out from under it, and unlink
// the lock only after the record is gone and the hold released — the single stated exception to
// the lock file's "never removed" rule.
//
// live.go holds Live, the running session's identity — the id minted at each boundary, the Title,
// CreatedAt and ParentID a later Save must preserve — and the hold that follows it: taken at the
// record's birth, parked ahead of a resume, moved at Rotate and Activate (where the onMove followers
// hear the new id) and released at Close. It is Driver-neutral (ADR 0031): it resolves no resume
// argument and sweeps nothing (ADR 0083 §5).
package session
