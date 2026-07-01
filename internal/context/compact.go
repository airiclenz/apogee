package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Generative Compaction — the default context reducer (context/doc.go)
// ----------------------------------------------------------------------------

// Completer runs ONE upstream completion over msgs and returns the assistant's text. The
// agent implements it over its provider seam (a silent, non-streaming collect); tests inject
// a deterministic fake. Compaction owns the prompt — the messages it passes already carry the
// summarizer system message and the rendered transcript — so the Completer is a thin
// "messages in, text out" call with no knowledge of compaction.
type Completer interface {
	Complete(ctx context.Context, msgs []domain.Message) (string, error)
}

// Result reports what a Compact pass did, for a caller's user-facing note. Skipped is true
// when the conversation was too small to be worth folding (Before == After, conv untouched).
type Result struct {
	Before  int
	After   int
	Skipped bool
}

// minCompactTail is the number of messages past the protected prefix below which compaction
// is a no-op: a conversation that is only the prefix (or one message past it) has nothing
// worth summarizing, and folding it would cost an upstream call to save nothing.
const minCompactTail = 2

// Compact summarizes conv in place: it keeps the protected prefix (PrefixEnd — leading system
// messages and the first user message) verbatim, asks the model to summarize the whole
// conversation, and Replaces the messages after the prefix with a single assistant summary
// message. It is a no-op (Result.Skipped) when there are too few messages past the prefix.
//
// conv is mutated only on success: a Completer error, a cancelled ctx, or an empty summary all
// return with conv untouched, so a failed /compact never corrupts the history. The result is a
// clean prefix → assistant-summary shape with no dangling tool calls, so the next user message
// keeps strict-template role alternation.
func Compact(ctx context.Context, c Completer, conv *domain.Conversation) (Result, error) {
	before := conv.Len()
	prefix := conv.PrefixEnd()
	if before-prefix < minCompactTail {
		return Result{Before: before, After: before, Skipped: true}, nil
	}

	msgs := conv.Messages()
	// Give the model the whole conversation (prefix included) for the best summary, even though
	// the prefix is kept verbatim below — the redundancy is cheap and the context helps.
	req := []domain.Message{
		{Role: domain.RoleSystem, Content: summaryInstruction},
		{Role: domain.RoleUser, Content: renderTranscript(msgs) + "\n\nSummarize the conversation above as instructed."},
	}

	text, err := c.Complete(ctx, req)
	if err != nil {
		return Result{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Result{}, errEmptySummary
	}

	out := make([]domain.Message, 0, prefix+1)
	out = append(out, msgs[:prefix]...)
	out = append(out, summaryMessage(text))
	conv.Replace(out)
	return Result{Before: before, After: len(out)}, nil
}

// errEmptySummary is returned when the model produced no summary text — treated as a failure
// (conv is left untouched) rather than replacing the history with an empty message.
var errEmptySummary = fmt.Errorf("apogee: compaction produced an empty summary")

// summaryInstruction is the summarizer's system prompt. It asks for a self-contained,
// resume-ready brief rather than a chat reply, and to preserve the load-bearing detail a
// coding agent needs to continue.
const summaryInstruction = "You are compacting a conversation between a user and a coding " +
	"agent to free up the context window. Produce a single, self-contained summary that lets " +
	"the agent continue seamlessly. Preserve: the user's goals and constraints; key decisions " +
	"and their rationale; important file paths, identifiers, and commands; the current state of " +
	"the work and any results that matter; and the next steps or open questions. Be concise but " +
	"complete, and omit pleasantries and redundant back-and-forth. Write the summary as notes " +
	"for the agent, not as a message to the user. Output only the summary."

// summaryMessagePrefix labels the folded summary so it reads clearly in scrollback and
// snapshots, and so the model sees it as prior context rather than a fresh instruction.
const summaryMessagePrefix = "Summary of the conversation so far:\n\n"

// summaryMessage wraps the generated summary as the single assistant message that replaces the
// folded history. Assistant role keeps clean user → assistant → user alternation when the next
// user message arrives (the kept prefix ends in the first user message).
func summaryMessage(text string) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, Content: summaryMessagePrefix + text}
}

// renderTranscript flattens a message slice into one plain-text transcript for the summary
// call. Flattening (rather than replaying the native messages) sidesteps strict chat-template
// tool-call pairing and role-alternation rules for this one-off call, and gives the model a
// clean, readable transcript. Each message becomes a "[role]" header and its content;
// assistant tool calls and tool results are rendered inline so the model sees the work that
// was done, not just the prose.
func renderTranscript(msgs []domain.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s]\n", m.Role)
		if m.Content != "" {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&b, "(called tool %s with %s)\n", tc.Tool, string(tc.Arguments))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
