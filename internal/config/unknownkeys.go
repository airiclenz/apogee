package config

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// The unknown-key walk
//
// yaml.v3 ignores a key the struct has no field for, so a misspelled `auto-compct:` or a `spiner:`
// under `ui:` reads as an absent key and the session runs on the default with nothing pointing at
// the line. This file finds those keys — every mapping key the schema does not spell, at the depth
// the schema descends to — so parseConfigFile can announce each one through notify, at startup and
// on every live re-read that reports its notices. It is a notice and never a refusal: an unknown key
// changes nothing the loader does, it only tells the user which line is not doing what they think
// it is.
//
// The schema it walks against is the same one the registry bijection test reads (registry_test.go):
// fileConfig's yaml tags, by reflection, through schemaKeys — so what the walk calls unknown and
// what the registry calls a key cannot drift apart.

// keyAt is one unknown key the walk found: the dotted path the file spells it at (`ui.spiner`,
// `servers[0].foo`, `model-profiles.fast.foo`) and the line its key sits on.
type keyAt struct {
	key  string
	line int
}

// unknownKeyNotice is the one line parseConfigFile prints per unknown key. It names the file, the
// dotted key and the line, and says the key is ignored — which is the whole fact: nothing refuses,
// nothing changes, the key just does not reach the loader.
const unknownKeyNotice = "apogee: config %s: unknown key %q at line %d is ignored"

// unknownKeys walks data — the config bytes AFTER the legacy migration, so every key the migration
// folds or refuses is already gone — against fileConfig's schema and returns every key the schema
// does not have, in file order. It is a pure function over the bytes: it reads no file, prints
// nothing and never fails — a document that does not parse is nil here, and the decoder's own error
// is the one the caller reports.
//
// The walk descends only where the schema does: into a field whose Go type is a struct, a slice of
// structs or a map of structs (`ui:`, `servers[]`, `model-profiles.<name>`, `reactions[]`, …), and
// never into a field typed `any` (`reactions[].run` / `advise` / `gate` — a webhook mapping under
// them is the reaction's own shape, not the schema's) or a scalar. A document without a root
// mapping — the zero-byte and the comment-only config.yaml, both of which parseConfigFile accepts —
// walks to nil.
func unknownKeys(data []byte) []keyAt {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var found []keyAt
	walkUnknownKeys(root, reflect.TypeOf(fileConfig{}), "", &found)
	return found
}

// serverEntryType is the struct the retired `sub-agents:` flag is exempted on (walkUnknownKeys).
var serverEntryType = reflect.TypeOf(ServerEntry{})

// fileConfigType is the struct the retired top-level `step-budget-notice:` key is exempted on
// (walkUnknownKeys).
var fileConfigType = reflect.TypeOf(fileConfig{})

// stepBudgetNoticeKey is the retired top-level switch of the engine's step-budget notice (ADR 0077,
// 2026-09-15 addendum; superseded 2026-09-19): the notice is structural for every delegate now, so
// the key has no field to land on, and a home config seeded from the old starter template still
// carries `step-budget-notice: false`. It is exempted from the walk, never stripped: the startup
// strips beside dropRetiredSubAgentsFlags back up and rewrite the file, and the exemption is
// read-only — startup and a live re-read alike print nothing and rewrite nothing.
const stepBudgetNoticeKey = "step-budget-notice"

// walkUnknownKeys reports into found every key of one mapping node that typ (a config struct) has
// no yaml tag for, and recurses into the values the schema descends into.
//
// Two keys are passed over without a notice. `servers[N].sub-agents` is the retired ADR 0045 flag
// (subAgentsKey): the legacy migration does not consume it — only the consented TUI offer strips
// it, and a declined offer leaves it in place (configmigrate.go, ADR 0035) — so without the
// exemption every start of such a config would print an unknown-key line beside the offer, and
// headless and daemon starts, which raise no offer, would print it forever. The top-level
// `step-budget-notice` (stepBudgetNoticeKey) is exempted on the same terms: nothing consumes it,
// and the old starter template wrote it into every home.
func walkUnknownKeys(node *yaml.Node, typ reflect.Type, prefix string, found *[]keyAt) {
	fields := make(map[string]reflect.StructField, typ.NumField())
	for _, sk := range schemaKeys(typ) {
		if sk.key != "" {
			fields[sk.key] = sk.field
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valueNode := node.Content[i], resolveAlias(node.Content[i+1])
		if keyNode.Tag == "!!merge" {
			continue // `<<: *anchor` is YAML's own key, honoured by the decoder, not a schema key
		}
		name := keyNode.Value
		if typ == serverEntryType && name == subAgentsKey {
			continue
		}
		if typ == fileConfigType && name == stepBudgetNoticeKey {
			continue
		}
		path := joinKeyPath(prefix, name)
		field, ok := fields[name]
		if !ok {
			*found = append(*found, keyAt{key: path, line: keyNode.Line})
			continue
		}
		descendUnknownKeys(valueNode, field.Type, path, found)
	}
}

// descendUnknownKeys continues the walk below one known key, when the schema's type for it is
// a struct, a slice of structs or a map of structs and the file's node has the matching shape. A
// shape mismatch (a list where a block was expected) is the decoder's error to report, not a set
// of unknown keys, so the walk stops there.
func descendUnknownKeys(node *yaml.Node, typ reflect.Type, path string, found *[]keyAt) {
	typ = derefType(typ)
	switch typ.Kind() {
	case reflect.Struct:
		if node.Kind == yaml.MappingNode {
			walkUnknownKeys(node, typ, path, found)
		}
	case reflect.Slice:
		elem := derefType(typ.Elem())
		if elem.Kind() != reflect.Struct || node.Kind != yaml.SequenceNode {
			return
		}
		for i, item := range node.Content {
			if item = resolveAlias(item); item.Kind == yaml.MappingNode {
				walkUnknownKeys(item, elem, fmt.Sprintf("%s[%d]", path, i), found)
			}
		}
	case reflect.Map:
		elem := derefType(typ.Elem())
		if elem.Kind() != reflect.Struct || node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if item := resolveAlias(node.Content[i+1]); item.Kind == yaml.MappingNode {
				walkUnknownKeys(item, elem, joinKeyPath(path, node.Content[i].Value), found)
			}
		}
	}
}

// joinKeyPath spells a child key under its parent's dotted path.
func joinKeyPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// resolveAlias follows a `*anchor` reference to the node it names, so an aliased block is walked
// where it is used; every other node is returned as is.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}

// The schema reader

// schemaKey is one field of a config struct as the file spells it: the yaml key and the Go field
// behind it. key is empty when the field has no on-disk name — untagged, or tagged `-` — which the
// registry bijection test reports and the unknown-key walk treats as a key the file cannot set.
type schemaKey struct {
	key   string
	field reflect.StructField
}

// schemaKeys reads a config struct's yaml tags in declaration order — the ONE reader of the
// on-disk schema, shared by the registry bijection test (registry_test.go) and the unknown-key
// walk, so the two cannot disagree about what a key is called.
func schemaKeys(typ reflect.Type) []schemaKey {
	keys := make([]schemaKey, 0, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "-" {
			name = ""
		}
		keys = append(keys, schemaKey{key: name, field: field})
	}
	return keys
}

// derefType strips pointer indirection so a *uiConfig is walked like a uiConfig.
func derefType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}
