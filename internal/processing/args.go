package processing

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
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

// verbatimValue is the markdown-fenced value coercion: every value is encoded as a JSON string
// verbatim — indentation and inner blank lines intact, JSON-looking text included — less one
// leading and one trailing line break, the breaks that separate the value from its END_ARG
// marker and from what follows it. A value meant as a number, boolean, array or object is
// decoded later, against the tool menu's schema (DecodeSchemaTypedArgs), because only the
// schema can tell `123` the integer from `123` the string.
func verbatimValue(value string) json.RawMessage {
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

// DecodeSchemaTypedArgs JSON-decodes the string-valued arguments of a text-format tool call
// whose property in the tool's argument schema does not admit a string: a text format carries
// every value as text, so `42` for an integer `start_line` arrives as the string "42" and would
// fail the tool's typed decode. A property admits a string when its schema names no type, names
// "string" (alone or in a type list), or — for `anyOf`/`oneOf` — has a member that admits one;
// such a value stays the verbatim string even when it looks like JSON. The property is found by
// its exact name first and then by domain.FoldArgumentKey, matching the tool's case-insensitive
// decode, so `START_LINE` reaches `start_line`; a folded spelling two properties share, a key
// the schema does not name, an already-typed value and a string that is not valid JSON are all
// left alone — the last so the tool's own decode error names the value. args is returned
// unchanged (the same bytes) when nothing is decoded or when args or schema is not an object;
// otherwise the object is re-encoded in its original key order, duplicates kept.
func DecodeSchemaTypedArgs(args, schema json.RawMessage) json.RawMessage {
	props := schemaProperties(schema)
	if len(props.exact) == 0 {
		return args
	}
	members, ok := objectMembers(args)
	if !ok {
		return args
	}
	changed := false
	for i, m := range members {
		prop, found := props.lookup(m.key)
		if !found || admitsString(prop) {
			continue
		}
		var text string
		if json.Unmarshal(m.value, &text) != nil {
			continue // not a string: already typed
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" && json.Valid([]byte(trimmed)) {
			members[i].value = json.RawMessage(trimmed)
			changed = true
		}
	}
	if !changed {
		return args
	}
	return encodeMembers(members)
}

// schemaProps maps an argument schema's property names to their schemas, with a folded index
// for the case-insensitive fallback lookup.
type schemaProps struct {
	exact  map[string]json.RawMessage
	folded map[string][]json.RawMessage
}

// schemaProperties reads the `properties` object of an argument schema; a schema that is not
// an object or names no properties yields an empty set.
func schemaProperties(schema json.RawMessage) schemaProps {
	var top struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &top) != nil || len(top.Properties) == 0 {
		return schemaProps{}
	}
	folded := make(map[string][]json.RawMessage, len(top.Properties))
	for name, prop := range top.Properties {
		key := domain.FoldArgumentKey(name)
		folded[key] = append(folded[key], prop)
	}
	return schemaProps{exact: top.Properties, folded: folded}
}

// lookup finds the schema of the property an argument key names: the exact name, else the one
// property whose folded name matches. found is false when no property, or more than one,
// matches.
func (p schemaProps) lookup(key string) (json.RawMessage, bool) {
	if prop, ok := p.exact[key]; ok {
		return prop, true
	}
	if matches := p.folded[domain.FoldArgumentKey(key)]; len(matches) == 1 {
		return matches[0], true
	}
	return nil, false
}

// admitsString reports whether a property schema accepts a JSON string. Its `type` (a name or a
// list of names) and its `anyOf`/`oneOf` member lists all constrain the value, so a string is
// admitted only when every present constraint admits one; a schema with none of them admits
// anything. A schema that does not parse is treated as admitting a string, keeping its value
// verbatim.
func admitsString(prop json.RawMessage) bool {
	var s struct {
		Type  json.RawMessage   `json:"type"`
		AnyOf []json.RawMessage `json:"anyOf"`
		OneOf []json.RawMessage `json:"oneOf"`
	}
	if json.Unmarshal(prop, &s) != nil {
		return true
	}
	return typeAdmitsString(s.Type) && membersAdmitString(s.AnyOf) && membersAdmitString(s.OneOf)
}

// typeAdmitsString reports whether a `type` keyword — absent, one name, or a list of names —
// admits "string".
func typeAdmitsString(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == "string"
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil {
		return true
	}
	for _, name := range many {
		if name == "string" {
			return true
		}
	}
	return false
}

// membersAdmitString reports whether an `anyOf`/`oneOf` list admits a string: an absent list
// does, and a present one does when any member does.
func membersAdmitString(members []json.RawMessage) bool {
	if len(members) == 0 {
		return true
	}
	for _, m := range members {
		if admitsString(m) {
			return true
		}
	}
	return false
}

// argMember is one key/value pair of an arguments object, in wire order.
type argMember struct {
	key   string
	value json.RawMessage
}

// objectMembers splits a JSON object into its members in wire order, duplicates kept. ok is
// false when raw is not a well-formed object.
func objectMembers(raw json.RawMessage) ([]argMember, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var members []argMember
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, isKey := tok.(string)
		if !isKey {
			return nil, false
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return nil, false
		}
		members = append(members, argMember{key: key, value: value})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); err == nil {
		return nil, false // trailing data after the object
	}
	return members, true
}

// encodeMembers writes members back as a JSON object in their given order.
func encodeMembers(members []argMember) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key) // a string always marshals
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
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
