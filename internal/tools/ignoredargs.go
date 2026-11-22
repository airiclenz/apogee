package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// undeclaredArgumentsNoter is the unexported opt-in a tool carries when an argument key its schema
// does not declare reaches its Execute and is dropped there without a word — every tool embedding
// toolSpec, whose arguments decode through decodeArgs' stdlib unmarshal. Only a type declared in
// this package can spell the method, so an MCP server's tool or a host-registered tool never
// matches: those validate (or ignore) their own arguments, and a note here would describe a decode
// this package never ran.
type undeclaredArgumentsNoter interface {
	// ignoresUndeclaredArguments reports whether the tool's decode drops an undeclared top-level
	// key silently — true for toolSpec, false for a tool that refuses such a key itself (finish).
	ignoresUndeclaredArguments() bool
}

// NoteIgnoredArguments returns result with one final line naming every top-level argument key in
// call that tool's schema does not declare — `note: ignored unknown argument "line" — read_file
// takes path, start_line, end_line, max_lines, locate` — so a model that invented a parameter reads
// that it had no effect instead of a result that looks as though it did. The call has already run:
// the note refuses nothing and corrects nothing, it only states what the decode dropped.
//
// A key counts as declared when it folds (domain.FoldArgumentKey) to a schema property, because the
// tool's stdlib decode binds keys case-insensitively: `Path` reaches read_file's path and is not
// named. Ignored keys are named once each by exact spelling, in the order the call first wrote
// them, each quoted so a key carrying a line break cannot forge a row. The declared names follow in
// schema order; a tool declaring none "takes no arguments". The note is appended to an error
// result too — the fact still holds and may explain the failure.
//
// result is returned unchanged — the same struct value, Summary included — when every key is
// declared, when the arguments are empty or not a JSON object, or when tool has not opted in
// (undeclaredArgumentsNoter): an MCP or host tool, or finish.
func NoteIgnoredArguments(
	tool domain.Tool,
	call domain.ToolCall,
	result domain.ToolResult,
) domain.ToolResult {
	noter, isNoter := tool.(undeclaredArgumentsNoter)
	if !isNoter || !noter.ignoresUndeclaredArguments() {
		return result
	}
	keys, err := topLevelArgumentKeys(call.Arguments)
	if err != nil || len(keys) == 0 {
		return result
	}
	declared := declaredArgumentNames(tool.Schema())
	ignored := undeclaredKeys(keys, declared)
	if len(ignored) == 0 {
		return result
	}

	result.Content = appendNoteLine(result.Content, ignoredArgumentsNote(tool.Name(), ignored, declared))
	return result
}

// topLevelArgumentKeys returns the member names of the argument object raw, in wire order and with
// any repeat kept. Empty or whitespace-only arguments are the empty object, as decodeArgs treats
// them; arguments that are not one well-formed JSON object are an error.
func topLevelArgumentKeys(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return objectMemberNames(raw)
}

// declaredArgumentNames returns the names under the `properties` member of schema, in schema order.
// A schema with no properties member, or one that does not parse, declares none.
func declaredArgumentNames(schema json.RawMessage) []string {
	var shape struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &shape); err != nil || len(shape.Properties) == 0 {
		return nil
	}
	names, err := objectMemberNames(shape.Properties)
	if err != nil {
		return nil
	}
	return names
}

// objectMemberNames reads raw as one JSON object and returns its member names in order, skipping
// each member's value whole. Anything but a single well-formed object is an error.
func objectMemberNames(raw json.RawMessage) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, isDelim := opening.(json.Delim); !isDelim || delim != '{' {
		return nil, errors.New("not a JSON object")
	}

	var names []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, isString := token.(string)
		if !isString {
			return nil, errors.New("object member name is not a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if _, err := decoder.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing content after the closing brace")
	}
	return names, nil
}

// undeclaredKeys returns the keys no declared name matches under domain.FoldArgumentKey, deduped by
// exact spelling with the first occurrence keeping its place.
func undeclaredKeys(keys, declared []string) []string {
	declaredFolds := make(map[string]struct{}, len(declared))
	for _, name := range declared {
		declaredFolds[domain.FoldArgumentKey(name)] = struct{}{}
	}

	var ignored []string
	seen := map[string]struct{}{}
	for _, key := range keys {
		if _, isDeclared := declaredFolds[domain.FoldArgumentKey(key)]; isDeclared {
			continue
		}
		if _, isSeen := seen[key]; isSeen {
			continue
		}
		seen[key] = struct{}{}
		ignored = append(ignored, key)
	}
	return ignored
}

// ignoredArgumentsNote renders the one note line: the ignored keys quoted, singular or plural, and
// what toolName takes instead.
func ignoredArgumentsNote(toolName string, ignored, declared []string) string {
	quoted := make([]string, 0, len(ignored))
	for _, key := range ignored {
		quoted = append(quoted, strconv.Quote(key))
	}
	noun := "argument"
	if len(ignored) > 1 {
		noun = "arguments"
	}
	takes := "no arguments"
	if len(declared) > 0 {
		takes = strings.Join(declared, ", ")
	}
	return fmt.Sprintf("note: ignored unknown %s %s — %s takes %s", noun, strings.Join(quoted, ", "), toolName, takes)
}

// appendNoteLine adds line as the last line of content, separated by exactly one line break: none
// is added when content is empty or already ends in one.
func appendNoteLine(content, line string) string {
	if content == "" || strings.HasSuffix(content, "\n") {
		return content + line
	}
	return content + "\n" + line
}
