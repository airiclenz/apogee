// Package eventjson renders the engine's Event stream as the Event lines — the one-line-per-Event
// JSON `apogee headless --format json` writes to stdout (ADR 0075).
//
// The contract is ADR 0075 and nothing else: a line is
// `{"event","v","seq","time","session","turn","depth","call_id","data"}` in that key order, every
// member always present and null where the line has no value, with the emitting variant's own
// members nested under `data` rather than flattened into the envelope. The kinds are snake_case
// (`tool_call`, `sub_agent_phase`) and are deliberately a DIFFERENT vocabulary from a notice
// Moment's kebab-case: `turn-finished` is a Depth-0 moment, `turn` is every depth, and spelling
// them apart is what stops a reader assuming the two streams are one.
//
// It shares no shape with internal/reactions. reactions.Payload is a flat document for five events and
// `jq -r .path`; across sixteen variants a flat union becomes some sixty optional keys whose
// names genuinely collide, so this package nests instead and reactions.Payload is untouched.
//
// One direction: it imports internal/domain for the events it reads and nothing else in the tree.
// It composes bytes for a file descriptor its caller owns — the engine stays wire-silent
// (ADR 0031) and hands out Go values exactly as before.
//
// # The files, one line each
//
// encode.go is the per-variant mapping: Encode turns one domain.Event into its kind, its
// EventBase and the `data` value that marshals to the line's object, and Kinds names the whole
// vocabulary including the two frames that are not Events.
//
// writer.go is the line itself: the Writer that stamps the envelope, counts seq, writes and
// flushes one line per Event, forwards to the sink it wraps, and writes the two frames — plus the
// frame structs a Driver fills.
package eventjson
