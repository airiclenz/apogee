package processing

import (
	"encoding/json"
	"sort"
	"strings"
)

// tryParseValue mirrors the apogee-code oracle's value coercion: a value that is valid JSON is
// kept as that JSON value; anything else is the trimmed text, encoded as a JSON string. The
// result is always a well-formed JSON value, so it slots directly into an arguments object.
func tryParseValue(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	encoded, _ := json.Marshal(trimmed) // a string always marshals
	return json.RawMessage(encoded)
}

// verbatimValue is the markdown-fenced value coercion: a value whose trimmed form is valid JSON
// keeps tryParseValue's JSON value, and every other value is encoded as a JSON string verbatim
// — indentation and inner blank lines intact — less one leading and one trailing line break,
// the breaks that separate the value from its END_ARG marker and from what follows it.
func verbatimValue(value string) json.RawMessage {
	if trimmed := strings.TrimSpace(value); trimmed != "" && json.Valid([]byte(trimmed)) {
		return tryParseValue(value)
	}
	encoded, _ := json.Marshal(trimOneLineBreak(value)) // a string always marshals
	return json.RawMessage(encoded)
}

// trimOneLineBreak drops at most one line break (\n or \r\n) from each end of value.
func trimOneLineBreak(value string) string {
	if rest, ok := strings.CutPrefix(value, "\r\n"); ok {
		value = rest
	} else {
		value = strings.TrimPrefix(value, "\n")
	}
	if rest, ok := strings.CutSuffix(value, "\r\n"); ok {
		return rest
	}
	return strings.TrimSuffix(value, "\n")
}

// marshalArgs assembles a JSON object from per-argument JSON values. Keys are emitted in
// sorted order so the encoding is deterministic (the map iteration order is not). An empty
// map encodes to "{}" — the no-argument call shape.
func marshalArgs(args map[string]json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage("{}")
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(args[k])
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// hasKey reports whether set contains key — a small readability helper over map membership.
func hasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// isSpace reports whether b is an ASCII whitespace byte, matching the oracle's /\s/ test over
// the characters that appear in model output (space, tab, newline, carriage return, form feed,
// vertical tab).
func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
