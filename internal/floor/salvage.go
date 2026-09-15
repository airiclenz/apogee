package floor

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// argumentKeys are the object keys a salvaged call may carry its arguments under, in the order the
// guard prefers them: "arguments" is the OpenAI wire spelling, "parameters" and "input" the two
// spellings models reach for when they write the call out as prose instead of on the wire. A call
// object carrying none of them is not salvaged — an argument-less call is indistinguishable from a
// model merely naming a tool, and the guard may never invent a dispatch the model did not write.
var argumentKeys = []string{"arguments", "parameters", "input"}

// fencedBlockPattern matches a markdown fenced block and captures its body. The info string
// (```json and friends) is optional, as is the newline after it, so a one-line fence salvages too.
var fencedBlockPattern = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*[ \t]*\r?\n?(.*?)```")

// toolCallTagPattern matches the <tool_call>…</tool_call> wrapper several instruct-tuned families
// emit when their native channel is unavailable, and captures its body.
var toolCallTagPattern = regexp.MustCompile(`(?is)<tool_call>(.*?)</tool_call>`)

// dsmlToolCallsPattern matches the <｜DSML｜tool_calls>…</｜DSML｜tool_calls> container the DeepSeek
// family writes its native calls in, and captures its body. It is read by HasToolCallMarkup ONLY:
// the body is invoke markup rather than a JSON object, so salvage has nothing to parse there — the
// container is recognised so a reply made of it is named as markup instead of being read as prose.
var dsmlToolCallsPattern = regexp.MustCompile(`(?is)<｜DSML｜tool_calls>(.*?)</｜DSML｜tool_calls>`)

// toolCallContainerPatterns are the vendor containers HasToolCallMarkup recognises. The fenced
// block is deliberately NOT among them: a fence is how a report quotes code, so keying on it would
// fault every report that shows a snippet.
var toolCallContainerPatterns = []*regexp.Regexp{toolCallTagPattern, dsmlToolCallsPattern}

// HasToolCallMarkup reports whether text is a tool call written out in a vendor container rather
// than a report: a <tool_call>…</tool_call> pair or a DSML tool_calls container, where either the
// trimmed text BEGINS with the container or the container's captured body is a JSON object. Both
// guards exist so that prose which merely QUOTES a tag pair — a report explaining what a model
// wrote — is not named as markup; a quoted pair holding a JSON object is one the reader would
// dispatch, and that is markup wherever it sits. It is read by the delegation result
// (internal/agent's delegationResult) to fault a child whose closing text is its unparsed call, and
// it is pure: text in, verdict out.
func HasToolCallMarkup(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, pattern := range toolCallContainerPatterns {
		for _, span := range pattern.FindAllStringSubmatchIndex(trimmed, -1) {
			if span[0] == 0 || isJSONObject(trimmed[span[2]:span[3]]) {
				return true
			}
		}
	}
	return false
}

// isJSONObject reports whether body, trimmed, is one valid JSON object — the shape a written call
// takes, as opposed to the prose or invoke markup a container may otherwise hold.
func isJSONObject(body string) bool {
	body = strings.TrimSpace(body)
	return strings.HasPrefix(body, "{") && json.Valid([]byte(body))
}

// SalvageToolCall is the tool-call salvage guard (the `tool-call-salvage` key, ADR 0071): when a
// model that was given tools answers with NO tool call on the wire but writes one out as JSON in
// its text, the guard reads that text back as the call the model meant and hands it to the engine,
// together with the text stripped of the block it salvaged. fired is false for every other
// response: the no-op case, where the response stands exactly as the model wrote it.
//
// offered is the tool names the model was shown. A salvaged object's "name" must be EXACTLY one of
// them — an unoffered name is a hallucination the repair guard owns, not a call to run, and a model
// merely discussing a tool in prose never wrote a JSON object at all. The object must also carry
// arguments under one of argumentKeys, as an object or as a string holding one (the double-encoded
// shape servers emit for the wire), which is what separates a written call from a written example.
//
// Three containers are read, per the ratified salvage shapes: a markdown fenced block, a
// <tool_call>…</tool_call> tag pair, or the whole trimmed response text. Every container that
// yields a call yields one, in document order, so a model that wrote two calls gets two. Calls are
// shaped exactly as processing.ParseNativeToolCalls shapes a native call — the arguments a JSON
// object, empty normalised to "{}" — except that IDs are left empty for the engine to assign: the
// model never wrote one, and inventing one here would put this package's spelling on the wire.
//
// The guard changes only what the model sees after its OWN failure to use the wire, which is why it
// needs no per-model proof and stays on under Bypass. It is pure: it reads nothing but resp and
// offered — no clock, no filesystem, no state between calls — so the same response always yields
// the same answer.
func SalvageToolCall(resp *domain.Response, offered []string) (calls []domain.ToolCall, text string, fired bool) {
	if len(resp.ToolCalls()) != 0 {
		return nil, "", false
	}
	raw := resp.Text()
	if strings.TrimSpace(raw) == "" {
		return nil, "", false
	}

	matches := salvageFromContainers(raw, offered)
	if len(matches) == 0 {
		call, ok := parseSalvagedCall(raw, offered)
		if !ok {
			return nil, "", false
		}
		return []domain.ToolCall{call}, "", true
	}

	return callsOf(matches), textWithout(raw, matches), true
}

// salvagedBlock is one container in the response text that parsed as a tool call: the byte span the
// container occupies, so the text can be handed back without it, and the call it yielded.
type salvagedBlock struct {
	start int
	end   int
	call  domain.ToolCall
}

// salvageFromContainers finds every fenced block and <tool_call> tag pair in the text that parses as
// a call to an offered tool, in document order. Containers that do not parse are skipped silently:
// a fenced block of Go, or a tag holding prose, is not a failed call but ordinary output.
func salvageFromContainers(raw string, offered []string) []salvagedBlock {
	var blocks []salvagedBlock
	for _, pattern := range []*regexp.Regexp{fencedBlockPattern, toolCallTagPattern} {
		for _, span := range pattern.FindAllStringSubmatchIndex(raw, -1) {
			call, ok := parseSalvagedCall(raw[span[2]:span[3]], offered)
			if !ok {
				continue
			}
			blocks = append(blocks, salvagedBlock{start: span[0], end: span[1], call: call})
		}
	}

	sort.Slice(blocks, func(i, j int) bool { return blocks[i].start < blocks[j].start })
	return blocks
}

// callsOf projects the salvaged blocks onto the calls they yielded, preserving document order.
func callsOf(blocks []salvagedBlock) []domain.ToolCall {
	calls := make([]domain.ToolCall, 0, len(blocks))
	for _, block := range blocks {
		calls = append(calls, block.call)
	}
	return calls
}

// textWithout returns the response text with every salvaged container cut out and the remainder
// trimmed — the narration the model wrote around the call, which is what the user should still see.
// The blocks are in document order and cannot overlap: a container holding another's markers never
// parses as a call, so it is never a block.
func textWithout(raw string, blocks []salvagedBlock) string {
	var remaining strings.Builder
	cut := 0
	for _, block := range blocks {
		remaining.WriteString(raw[cut:block.start])
		cut = block.end
	}
	remaining.WriteString(raw[cut:])
	return strings.TrimSpace(remaining.String())
}

// parseSalvagedCall reads one container's body as a tool call. It is deliberately strict — the body
// must be a single JSON object naming an offered tool and carrying arguments — because the cost of
// a false positive is dispatching a tool the model only wrote about.
func parseSalvagedCall(body string, offered []string) (domain.ToolCall, bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") {
		return domain.ToolCall{}, false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		return domain.ToolCall{}, false
	}

	var name string
	if err := json.Unmarshal(fields["name"], &name); err != nil {
		return domain.ToolCall{}, false
	}
	if !isOfferedTool(name, offered) {
		return domain.ToolCall{}, false
	}

	arguments, ok := salvagedArguments(fields)
	if !ok {
		return domain.ToolCall{}, false
	}
	return domain.ToolCall{Tool: name, Arguments: arguments}, true
}

// isOfferedTool reports whether the salvaged name is exactly one of the tools the model was shown.
func isOfferedTool(name string, offered []string) bool {
	for _, candidate := range offered {
		if name == candidate {
			return true
		}
	}
	return false
}

// salvagedArguments picks the first of argumentKeys the object carries and normalises its value.
// The first key present decides, even when it is malformed: a call whose "arguments" are broken is
// a broken call, not an invitation to read some other key hoping for a better one.
func salvagedArguments(fields map[string]json.RawMessage) (json.RawMessage, bool) {
	for _, key := range argumentKeys {
		if value, present := fields[key]; present {
			return normalizeSalvagedArguments(value)
		}
	}
	return nil, false
}

// normalizeSalvagedArguments turns a salvaged argument value into the JSON object a domain.ToolCall
// carries. An object is taken as written; a string is decoded first, because servers and models
// alike double-encode arguments on the OpenAI wire. An empty string means a no-argument call and
// normalises to "{}", matching processing.ParseNativeToolCalls. Anything else — a number, a list, a
// string holding something that is not an object — is not a tool call's arguments.
func normalizeSalvagedArguments(value json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return nil, false
	}

	switch trimmed[0] {
	case '{':
		return append(json.RawMessage(nil), trimmed...), true
	case '"':
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return nil, false
		}
		inner := strings.TrimSpace(encoded)
		if inner == "" {
			return json.RawMessage("{}"), true
		}
		if !strings.HasPrefix(inner, "{") || !json.Valid([]byte(inner)) {
			return nil, false
		}
		return json.RawMessage(inner), true
	}
	return nil, false
}
