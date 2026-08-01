// Package session persists conversations as id-addressed Records — the storage layer the
// history browser lists, resumes, renames, and deletes, and the composition root writes to
// after every Turn.
//
// A Record wraps two opaque payloads in browsable Meta: the engine's Session envelope
// (internal/domain, versioned and resumed by the engine) and the TUI's Transcript blob
// (versioned by internal/tui). Each layer owns and forward-rejects only its own version;
// this package versions the wrapper (RecordVersion) and rejects a newer wrapper with
// ErrRecordVersion.
//
// The Store owns the on-disk format and naming so its callers never duplicate that
// knowledge. Ids from NewID are sortable UTC stamps with a random suffix; Save writes
// atomically (temp file + rename) under <id>.json and updates in place; List/Load skip or
// wrap files they cannot read as records, so pre-plan bare domain.Session files still list,
// load, and resume, and one corrupt file never kills the browser.
//
// An id is a filename, so every id crossing the Store — decoded from a file, saved, loaded,
// or deleted — must be a single safe path component or it is refused with ErrInvalidID. A
// record read by path declares its own id, and without that gate a planted file would aim
// Apogee's autosaves and deletes anywhere the user can write.
package session
