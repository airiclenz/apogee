package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// mergePatch is one decoded RFC 7396 JSON Merge Patch object — a server entry's request-extra
// (ADR 0085). It is decoded once, when the Client is built, and only ever read afterwards: apply
// builds a fresh body per encode and never writes into it, so one Client's parallel encodes can
// share it.
type mergePatch struct {
	keys   []string // the patch's members in their written order
	fields map[string]patchField
}

// patchField is what one patch member does to the target member of the same name: delete it
// (a JSON null), merge into it (an object — recursively), or replace it (anything else).
type patchField struct {
	remove bool
	object *mergePatch
	value  json.RawMessage // compact bytes of a replacing value
}

// parseMergePatch decodes a request-extra patch. "" and an empty object both yield a nil patch,
// which the Client reads as "no merge at all" — the body then stays byte-identical to the
// codec's. Anything but a JSON object is an error: a merge patch that is not an object would
// replace the whole request body.
func parseMergePatch(raw string) (*mergePatch, error) {
	if raw == "" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	patch, err := decodePatchObject(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the patch object")
	}
	if len(patch.keys) == 0 {
		return nil, nil
	}
	return patch, nil
}

// decodePatchObject reads one JSON object off dec, keeping member order so a merge appends the
// patch's new members in the order they were written.
func decodePatchObject(dec *json.Decoder) (*mergePatch, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("want a JSON object, got %v", tok)
	}
	patch := &mergePatch{fields: map[string]patchField{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		field, err := decodePatchField(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if _, seen := patch.fields[key]; !seen {
			patch.keys = append(patch.keys, key)
		}
		patch.fields[key] = field
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return patch, nil
}

// decodePatchField classifies one patch member's value.
func decodePatchField(raw json.RawMessage) (patchField, error) {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case bytes.Equal(trimmed, []byte("null")):
		return patchField{remove: true}, nil
	case len(trimmed) > 0 && trimmed[0] == '{':
		object, err := decodePatchObject(json.NewDecoder(bytes.NewReader(trimmed)))
		if err != nil {
			return patchField{}, err
		}
		return patchField{object: object}, nil
	default:
		var compact bytes.Buffer
		if err := json.Compact(&compact, trimmed); err != nil {
			return patchField{}, err
		}
		return patchField{value: compact.Bytes()}, nil
	}
}

// apply merges the patch over target per RFC 7396 and returns fresh bytes: objects merge
// member by member, null deletes, anything else replaces; a target that is not an object is
// merged as if it were {}. Only the members the patch names are decoded any deeper — every
// other member keeps the target's bytes exactly, and the target's member order is kept, with
// the patch's new members appended after it.
func (p *mergePatch) apply(target []byte) ([]byte, error) {
	keys, members, err := decodeTargetObject(target)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]json.RawMessage, len(members)+len(p.keys))
	for key, value := range members {
		merged[key] = value
	}
	for _, key := range p.keys {
		field := p.fields[key]
		switch {
		case field.remove:
			delete(merged, key)
			continue
		case field.object != nil:
			value, err := field.object.apply(merged[key])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			merged[key] = value
		default:
			merged[key] = field.value
		}
		if _, existed := members[key]; !existed {
			keys = append(keys, key)
		}
	}
	return writeObject(keys, merged)
}

// decodeTargetObject splits a target value into its members, in order, keeping each member's
// bytes as written. Anything but an object (absent, a scalar, an array) decodes as {}.
func decodeTargetObject(target []byte) ([]string, map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(target)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, map[string]json.RawMessage{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	var keys []string
	members := map[string]json.RawMessage{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, seen := members[key]; !seen {
			keys = append(keys, key)
		}
		members[key] = raw
	}
	return keys, members, nil
}

// writeObject renders the surviving members in key order, each value's bytes verbatim.
func writeObject(keys []string, members map[string]json.RawMessage) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	for _, key := range keys {
		value, ok := members[key]
		if !ok {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
