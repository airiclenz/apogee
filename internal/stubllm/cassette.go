package stubllm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"
)

// CassetteVersion is the on-disk format version a cassette is written under. A file of any other
// version is refused on load rather than half-read.
const CassetteVersion = 1

// messagesPath is the Anthropic Messages completion route — the second of the two POSTs a
// cassette records, beside chatCompletionsPath.
const messagesPath = "/v1/messages"

// The two wire names a cassette key spells, one per completion route.
const (
	keyWireChat     = "chat"
	keyWireMessages = "messages"
)

// keyDigestLength is how many hex digits of the SHA-256 a key keeps: 64 bits, far past any
// collision a captured session could produce, and short enough to read in a diagnostic.
const keyDigestLength = 16

// rawKeyPrefix marks a key taken over a body that would not decode as either wire's request —
// such a request is still recorded, keyed by its exact bytes, rather than silently dropped.
const rawKeyPrefix = "raw:"

// isoDate matches an ISO-8601 date, with an optional time and zone, wherever it appears in the
// first user message. apogee's orientation stamps today's date there; normalizing it to
// isoDatePlaceholder is what lets a cassette captured on one day replay on the next.
var isoDate = regexp.MustCompile(
	`\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?`,
)

// isoDatePlaceholder is what every ISO date in the first user message is keyed as.
const isoDatePlaceholder = "<date>"

// cassetteFileMode is the permission a written cassette gets — a checked-in data file.
const cassetteFileMode = 0o644

// Cassette is a captured upstream: every completion exchange a recording proxy saw, in capture
// order, and the latest answer to each discovery probe. Unlike a [Script] it holds no
// interpretation of the replies — each exchange is the upstream's raw bytes, chunked as they
// arrived and stamped with when — so a replay reproduces the real server's output and pacing
// byte for byte, whatever the wire.
//
// Exchanges are found by [Exchange.Key], which names the conversation a request carried rather
// than its bytes (see the key rules on [CassetteRecorder]). Several exchanges may share a key;
// they are that key's queue, served in slice order.
type Cassette struct {
	// Version is the format version, [CassetteVersion] on every cassette this package writes.
	Version int `json:"version"`
	// Exchanges are the recorded completions, in the order they were captured.
	Exchanges []Exchange `json:"exchanges"`
	// Probes are the discovery answers, one per path probed — the latest one captured.
	Probes []ProbeReply `json:"probes,omitempty"`
}

// Exchange is one recorded completion: the key of the request that asked, and the upstream's
// reply as it arrived.
type Exchange struct {
	// Key is the conversation key of the request (see [CassetteRecorder]).
	Key string `json:"key"`
	// Status is the HTTP status the upstream answered with.
	Status int `json:"status"`
	// Header is the upstream's reply header, less the members that describe one particular
	// transfer (Content-Length, Date, Set-Cookie) rather than the reply.
	Header http.Header `json:"header,omitempty"`
	// Chunks are the reply body, one per read off the upstream, in arrival order.
	Chunks []Chunk `json:"chunks"`
	// Truncated reports that the reply ended before its EOF — the upstream dropped the
	// connection, or the client hung up mid-stream — so the chunks are all that arrived.
	Truncated bool `json:"truncated,omitempty"`
}

// Chunk is one read of reply body and when it arrived, measured from the moment the proxy
// received the request — so the first chunk's offset is the upstream's time to first byte.
type Chunk struct {
	// Offset is the arrival time since the request reached the proxy.
	Offset time.Duration
	// Bytes is the body the read returned, exactly.
	Bytes []byte
}

// ProbeReply is the upstream's answer to one discovery GET, kept verbatim: a 404 /props is
// stored as the 404 it was, so the replay refuses where the real server refused.
type ProbeReply struct {
	// Path is the path probed: "/v1/models" or "/props".
	Path string `json:"path"`
	// Status is the HTTP status the upstream answered with.
	Status int `json:"status"`
	// ContentType is the reply's Content-Type, empty when it sent none.
	ContentType string `json:"content_type,omitempty"`
	// Body is the reply body, exactly.
	Body string `json:"body"`
}

// chunkJSON is a Chunk's on-disk form. A chunk that is valid UTF-8 — every SSE event and JSON
// body a server sends, unless a read happened to split a rune — is written as readable Text;
// any other is written as Base64, so no byte is ever lost to a replacement character.
type chunkJSON struct {
	Offset string  `json:"offset"`
	Text   *string `json:"text,omitempty"`
	Base64 []byte  `json:"base64,omitempty"`
}

// MarshalJSON writes the chunk as readable text where the bytes allow it.
func (c Chunk) MarshalJSON() ([]byte, error) {
	out := chunkJSON{Offset: c.Offset.String()}
	if utf8.Valid(c.Bytes) {
		text := string(c.Bytes)
		out.Text = &text
	} else {
		out.Base64 = c.Bytes
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads either spelling of a chunk back.
func (c *Chunk) UnmarshalJSON(data []byte) error {
	var in chunkJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	offset, err := time.ParseDuration(in.Offset)
	if err != nil {
		return fmt.Errorf("stubllm: chunk offset %q: %w", in.Offset, err)
	}

	c.Offset = offset
	c.Bytes = in.Base64
	if in.Text != nil {
		c.Bytes = []byte(*in.Text)
	}
	return nil
}

// LoadCassette reads a cassette file, refusing one written under another format version.
func LoadCassette(path string) (*Cassette, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("stubllm: read cassette: %w", err)
	}

	var cassette Cassette
	if err := json.Unmarshal(raw, &cassette); err != nil {
		return nil, fmt.Errorf("stubllm: cassette %s: %w", path, err)
	}
	if cassette.Version != CassetteVersion {
		return nil, fmt.Errorf(
			"stubllm: cassette %s is format version %d, want %d", path, cassette.Version, CassetteVersion)
	}
	return &cassette, nil
}

// Save writes the cassette to path as indented JSON, stamping the current format version.
func (c *Cassette) Save(path string) error {
	if path == "" {
		return errors.New("stubllm: a cassette needs a path to write to")
	}

	c.Version = CassetteVersion
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("stubllm: encode cassette: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), cassetteFileMode); err != nil {
		return fmt.Errorf("stubllm: write cassette %s: %w", path, err)
	}
	return nil
}

// Probe returns the stored answer to a discovery path, and whether there is one.
func (c *Cassette) Probe(path string) (ProbeReply, bool) {
	for _, probe := range c.Probes {
		if probe.Path == path {
			return probe, true
		}
	}
	return ProbeReply{}, false
}

// conversation is what a completion request is keyed by: the parts that name WHERE in a session
// it sits and nothing that varies between two runs of the same session. The system text is left
// out (it carries the scratch directory and session id) and so is every tool result (it carries
// timings, paths and ids); the first user message is kept with its dates normalized, and every
// assistant turn is kept whole, because the model's own earlier replies are what distinguish one
// step of a session from the next.
type conversation struct {
	Wire      string          `json:"wire"`
	Stream    bool            `json:"stream"`
	Tools     []string        `json:"tools"`
	FirstUser string          `json:"first_user"`
	Assistant []assistantTurn `json:"assistant"`
}

// assistantTurn is one earlier assistant reply as the key sees it.
type assistantTurn struct {
	Content string     `json:"content"`
	Calls   []callSeen `json:"calls,omitempty"`
}

// callSeen is one tool call an assistant turn issued. The call id is left out: servers mint it
// fresh on every run.
type callSeen struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// conversationOf decodes a completion request posted to path into its keyed parts. It fails on
// a body that does not decode as that route's request.
func conversationOf(path string, body []byte) (conversation, error) {
	switch path {
	case messagesPath:
		var request anthropicRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return conversation{}, err
		}
		return newConversation(keyWireMessages, request.Stream, request.toolNames(), request.messages()), nil
	default:
		var request chatRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return conversation{}, err
		}
		return newConversation(keyWireChat, request.Stream, request.toolNames(), request.messages()), nil
	}
}

// newConversation reduces a decoded request to its keyed parts.
func newConversation(wire string, stream bool, tools []string, messages []Message) conversation {
	sorted := slices.Clone(tools)
	slices.Sort(sorted)
	c := conversation{Wire: wire, Stream: stream, Tools: sorted}

	for _, message := range messages {
		switch message.Role {
		case "user":
			if c.FirstUser == "" {
				c.FirstUser = isoDate.ReplaceAllString(message.Content, isoDatePlaceholder)
			}
		case "assistant":
			c.Assistant = append(c.Assistant, assistantTurnOf(message))
		}
	}
	return c
}

// assistantTurnOf is an assistant message as the key sees it.
func assistantTurnOf(message Message) assistantTurn {
	turn := assistantTurn{Content: message.Content}
	for _, call := range message.ToolCalls {
		turn.Calls = append(turn.Calls, callSeen{Name: call.Name, Arguments: call.Arguments})
	}
	return turn
}

// key is the conversation's digest.
func (c conversation) key() string {
	// Marshalling a struct of strings, bools and slices of them cannot fail.
	canonical, _ := json.Marshal(c)
	return digest(canonical)
}

// cassetteKey is the key a completion request posted to path is recorded and replayed under:
// the digest of its conversation, or — for a body that does not decode — of its exact bytes,
// marked with rawKeyPrefix.
func cassetteKey(path string, body []byte) string {
	c, err := conversationOf(path, body)
	if err != nil {
		return rawKeyPrefix + digest(bytes.TrimSpace(body))
	}
	return c.key()
}

// digest is the truncated hex SHA-256 of b.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:keyDigestLength]
}
