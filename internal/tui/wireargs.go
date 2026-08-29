package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// wireArgsFieldCap is the largest a single string value may be before [wireArgs] replaces it with
// its own size. 1 KB is far past what a path, a pattern or a shell command spends and far short of
// what a pasted file body does, so the elision only ever fires on a value a reviewer would have
// scrolled past anyway.
const wireArgsFieldCap = 1024

// wireArgsCap is the largest a whole call's stored arguments may be. A payload still over it after
// the per-field elisions is stored as its own size instead: the record is a review aid, not a
// transport, and one pathological call must not be able to dominate a session file.
const wireArgsCap = 4096

// contentArgs names, per write/edit tool, the argument keys whose value is file content, patch
// text or replacement pairs — read off those tools' own schemas in internal/tools. Those values
// are dropped from the stored arguments entirely rather than elided, because the card's Regions
// and Details already carry what the edit did (ADR 0052): a second copy on the wire would double
// the record of an edit and say nothing new about it.
var contentArgs = map[string][]string{
	"write_file":              {"content"},
	"edit_existing_file":      {"content"},
	"single_find_and_replace": {"oldText", "newText"},
	"multi_find_and_replace":  {"replacements"},
}

// wireArgs returns the bounded, compact JSON a saved transcript keeps as one tool call's
// arguments, or nil where there is nothing worth keeping. It is a pure function of the tool's
// name and the raw arguments the model sent, applying four rules in order:
//
//  1. Arguments that are empty, that are not a JSON object, or that do not parse at all yield
//     nil — as does an object left with no keys once rule 2 has run.
//  2. For the write/edit tools the content-carrying keys ([contentArgs]) are dropped.
//  3. Every remaining string value longer than [wireArgsFieldCap], at any nesting depth, becomes
//     "…[N bytes]" — the size it had, in place of the bytes it spent.
//  4. A result still longer than [wireArgsCap] once encoded becomes {"elided":"N bytes"}, N being
//     the size of that encoding.
//
// The arguments are decoded into a map with [json.Decoder.UseNumber] so a large integer is stored
// as the model spelled it rather than re-spelled through a float64, and re-encoded with
// [json.Marshal]: sorted keys, HTML-escaped, compact. That is exactly what encodeTranscript's own
// Marshal would do to these bytes when it re-compacts them as a [json.RawMessage] member, so the
// value this returns survives that encoder byte-for-byte instead of shifting under it.
//
// Nothing here is sanitised for display: this form is stored, never painted (the ratified
// store-only call for plan 2026-08-29 - 02). A surface that later shows it is the surface that
// must strip it, the way every other card field is stripped at [toolView.finishDisplay].
func wireArgs(tool string, raw json.RawMessage) json.RawMessage {
	args := decodeArgsPreservingNumbers(raw)
	for _, key := range contentArgs[tool] {
		delete(args, key)
	}
	if len(args) == 0 {
		return nil
	}

	for key, value := range args {
		args[key] = boundedArgValue(value)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil // a value no encoder can spell is a value no record can keep
	}
	if len(encoded) > wireArgsCap {
		return json.RawMessage(fmt.Sprintf(`{"elided":"%d bytes"}`, len(encoded)))
	}
	return json.RawMessage(encoded)
}

// decodeArgsPreservingNumbers decodes a call's raw arguments into a map, keeping every number as
// the [json.Number] the model wrote instead of a float64 — the one difference from [parseArgs],
// and the reason this decode is its own: a float64 round-trip re-spells a large integer (an id, a
// byte offset) as something the model never sent. Anything that is not a JSON object — a bare
// array, a fragment, empty bytes — yields a nil map, which the caller reads as "nothing to keep".
func decodeArgsPreservingNumbers(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil
	}
	return args
}

// boundedArgValue returns value with every over-long string in it replaced by its own size. It
// recurses through objects and arrays so a long string nested inside one — a replacement pair, an
// MCP tool's structured payload — is bounded where it sits, rather than being left to push the
// whole call over [wireArgsCap] and collapse the useful keys beside it.
func boundedArgValue(value any) any {
	switch typed := value.(type) {
	case string:
		if len(typed) > wireArgsFieldCap {
			return fmt.Sprintf("…[%d bytes]", len(typed))
		}
		return typed
	case map[string]any:
		for key, member := range typed {
			typed[key] = boundedArgValue(member)
		}
		return typed
	case []any:
		for i, member := range typed {
			typed[i] = boundedArgValue(member)
		}
		return typed
	default:
		return value
	}
}
