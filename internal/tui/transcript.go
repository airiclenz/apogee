package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The transcript (phase-2 detail plan §3 C6)
// ----------------------------------------------------------------------------

// transcript is the scrollback model: an append-only list of typed entries plus a
// single in-progress assistant buffer fed by streamed TokenEvents. It is the C6
// rendering model the viewport displays. apply folds the full event stream into it (P2.3):
// tokens grow the in-progress buffer, which is finalised on a MessageEvent or the first
// ToolCallEvent of a Turn and discarded on a StreamResetEvent; tool calls, results,
// approvals, and recovered faults append their own entries. It renders only — no agent
// logic lives here (C5).
type transcript struct {
	entries []entry // committed, in display order
	pending string  // in-progress assistant tokens for the current Turn (a plain string,
	// not a strings.Builder: the Model is a value type copied on every
	// Update, and a Builder forbids the copy — it panics copyCheck)
	streaming bool // whether pending holds an un-committed assistant buffer
	debug     bool // when set, MechanismFiredEvents are recorded (a hidden debug view)
}

// entryKind tags a transcript entry so the renderer can prefix and style it. The set
// mirrors the C6 entry kinds (user / assistant / tool call+result / error / note).
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryToolCall
	entryToolResult
	entryError
	entryNote
	entryPresented
	entryStartup
	entryInterjected
)

// entry is one committed line-block in the transcript. text is the body (for the text
// kinds); depth is the sub-agent nesting level (Phase 3). A tool call carries its
// presentation view and a callID so the paired result can be folded into the same block:
// callID matches the result by ToolCall.ID, and done marks the call once its result has
// arrived (so a re-used tool pairs each result with the right call). A presented document
// carries no text at all: its facts live in presented, the view render.go composes from.
//
// ephemeral marks an entry as display-only: it renders exactly like its kind normally does, but
// encodeTranscript never writes it to the session record. It generalizes the entryStartup
// exclusion — the box is opening chrome that is re-seeded on every launch, and a resume-time
// notice is the same thing one kind over: re-derived from live state at the moment the view is
// rebuilt, so persisting it would append a fresh copy on every resume until the record was a
// column of "resumed:" notes.
type entry struct {
	kind      entryKind
	text      string
	depth     int
	callID    string
	tool      toolView
	done      bool
	ephemeral bool     // display-only: rendered, never persisted (see encodeTranscript)
	skills    []string // entryUser / entryInterjected: display names of the skills this message invoked
	presented presentedView
	startup   startupView // entryStartup only: the one-time start-up box's logo + session facts
}

// presentedView is the presentation model of a shown document (entryPresented only): the
// deliverable's own name, where it lives, and what the host managed to do with it. It is the
// [toolView] of a presentation — the entry holds the facts and render.go turns them into lines,
// so the wording and the shape stay table-testable without a Model (ADR 0019 §2, rung 0).
//
// Path and Location are carried VERBATIM and rendered as plain text: terminal linkification is
// the whole mechanism, so nothing here may clip, wrap or decorate them.
type presentedView struct {
	Title    string               // the model's optional label; empty when it named none
	Path     string               // the workspace-relative path — always present; its own line under a title, else beside the ▤ marker
	Location string               // the served URL (rung 2); empty on every other rung
	Method   domain.PresentMethod // the rung reached, which the closing status line words
	Reason   string               // why a tried rung did not deliver; empty when none was
}

// startupView is the presentation model of the one-time start-up box (entryStartup only): the
// embedded logo art plus the session facts the box shows. Like [presentedView] the entry holds the
// facts and render.go composes the card, so the box's shape and wording stay table-testable without
// a Model (ADR 0019 §2, rung 0). The box is seeded as entries[0] (newModel, and re-seeded by
// startNewSession on /clear) and rendered fresh at the live width on every repaint, so it reflows on
// resize and reprints on a session reset.
//
// Host and Model trace to config / the CLI, so addStartup escape-strips them as addPresented does
// its untrusted halves — defence in depth even though they are not model output. Logo (this
// program's own embedded asset), Context (formatTokens of an int), and Version (its own build value)
// are trusted and pass through.
type startupView struct {
	Logo    string // the embedded block-art "APOGEE" wordmark
	Host    string // the upstream host label (HostAlias, or the endpoint when none)
	Model   string // the display model id (displayModel-ed)
	Context string // the formatted context-window size (formatTokens, e.g. "32k"); "" when unknown
	Version string // the resolved build version (Options.Version)
}

// addUser appends a user message — the text the human submitted to open or continue the
// Exchange, plus the display names of any skills attached to it (rendered as chips on the
// block so the attachment stays visible after the send; nil when none). Called from the submit
// path, not the event fold. The skill display names are untrusted (they come from a
// repo-supplied SKILL.md front-matter), so they are escape-stripped like model text — an
// attacker cannot smuggle a terminal control sequence into the transcript through a chip.
func (t *transcript) addUser(text string, skills []string) {
	t.entries = append(t.entries, entry{kind: entryUser, text: text, skills: stripEscapesAll(skills)})
}

// addInterjected appends a message the human interjected into the running Exchange, at the point
// in the scrollback where the model actually RECEIVED it (ADR 0025) — the delivery fold calls it,
// never the staging keypress, so the transcript stays an honest record of what the model saw and
// when. It reads as the human speaking (the user block's styling) but leads with the ⧖ marker
// rather than ❯, and it is deliberately NOT an entryUser: a mid-Exchange remark must not become
// the sticky header (renderView records only entryUser blocks as such), because the prompt the
// on-screen work belongs to is still the one that opened the Exchange.
//
// It carries the skills the remark invoked exactly as addUser does, and for the same reason: a
// skill rides an interjection (ADR 0027), so the delivered block must record what the model was
// given, and a delivered remark differs from a flushed one only in when it landed. The display
// names are escape-stripped on the same untrusted-input grounds.
func (t *transcript) addInterjected(text string, skills []string) {
	t.entries = append(t.entries, entry{kind: entryInterjected, text: text, skills: stripEscapesAll(skills)})
}

// addNote appends a neutral note (e.g. "cancelled") — a transcript record of a UI-level
// event that is not itself an engine Event.
//
// It escape-strips at the SEAM, on behalf of every caller, rather than trusting each producer to
// remember (stripEscapes). A note is worded from the least trustworthy strings in the program — a
// repo SKILL.md's front matter (/skills), the model id a server advertises (rebindNote), a
// launcher profile name, an error string quoting a workspace path — and the per-producer
// discipline that preceded this had in fact missed several of them. A caller that strips first is
// harmless: stripEscapes is idempotent and allocates nothing when there is no ESC byte.
func (t *transcript) addNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: stripEscapes(text)})
}

// addEphemeralNote appends a note that the human sees but the session record never keeps. It is
// addNote in every respect the renderer can observe — same kind, same styling, same position in
// the scrollback — and differs only at the persistence seam, where encodeTranscript skips it.
//
// It is for notices that are RE-DERIVED at each startup or resume rather than earned by the
// conversation: the "resumed: <title>" line, its no-scrollback degrade variant, the
// interrupted-mid-exchange note, and the "context: …" notice naming the workspace files the session
// loaded. Each of those is recomputed from live state every time the view is
// rebuilt, so persisting one adds nothing on the way back in and accumulates a duplicate on the way
// out — five resumes, five stored "resumed:" notes. A note that records something that actually
// happened in the session (a cancellation, a failed save, a server switch) belongs in addNote.
//
// It escape-strips exactly as addNote does, and for a sharper reason: its two biggest callers word
// their notice from a stored session title (resumeLoaded, replayResumed) and from the workspace
// context-file names the session loaded — untrusted DISK input in both cases, since no codec
// sanitizes a session record's Meta and a repo names its own files.
func (t *transcript) addEphemeralNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: stripEscapes(text), ephemeral: true})
}

// addPresented appends the presentation entry for one shown document — rung 0 of the ladder,
// and the reason a failed mechanism above it is never an error (ADR 0019 §4). Like addUser it is
// called from the Update loop rather than the event fold: a presentation is the HOST's act, not
// an engine Event.
//
// The title is untrusted model text, so it is escape-stripped and clipped like any other model
// string reaching the terminal. The path and the URL are escape-stripped too — a filename is
// filesystem data, not this program's — but never clipped: a truncated path is a link that no
// longer opens, which is worse than a long one.
func (t *transcript) addPresented(msg presentedMsg) {
	t.entries = append(t.entries, entry{kind: entryPresented, presented: presentedView{
		Title:    clipDetail(stripEscapes(msg.Title)),
		Path:     stripEscapes(msg.Path),
		Location: stripEscapes(msg.Location),
		Method:   msg.Method,
		Reason:   clipDetail(stripEscapes(msg.Reason)),
	}})
}

// addStartup appends the one-time start-up box — the logo and the session's host / model /
// context / version (startupView). It is seeded by newModel as entries[0] (and re-seeded by
// startNewSession when /clear starts a fresh session), not folded from an engine Event: the box is
// the HOST's opening frame, like addPresented's record of a host act. Host
// and Model are escape-stripped (they trace to config / the CLI) so a control sequence can never
// reach the terminal through them; the logo, context (formatTokens of an int), and version are this
// program's own values and pass through untouched.
func (t *transcript) addStartup(v startupView) {
	v.Host = stripEscapes(v.Host)
	v.Model = stripEscapes(v.Model)
	t.entries = append(t.entries, entry{kind: entryStartup, startup: v})
}

// refreshStartup re-states the one-time start-up box's facts in place, leaving it exactly where it
// sits in the scrollback. The box is seeded once (addStartup) from the display Options as they stood
// at construction, and its startupView is a frozen copy: without this a session whose model is bound
// LATE — the async cold start, where the first heartbeat is startup discovery — would keep a box
// saying "connecting" at the top of the scrollback until the next /clear re-seeded it. Only the
// first box is restated; there is never a second (the /clear path resets the transcript before
// re-seeding, and a resumed scrollback carries no start-up entry — the codec never persists one).
// A transcript with no box yet is left untouched.
//
// Host and Model are escape-stripped exactly as addStartup strips them: the facts come from the same
// Options, and a fact that arrived from the server (the observed model id) is even less this
// program's own than the configured one.
func (t *transcript) refreshStartup(v startupView) {
	v.Host = stripEscapes(v.Host)
	v.Model = stripEscapes(v.Model)
	for i := range t.entries {
		if t.entries[i].kind == entryStartup {
			t.entries[i].startup = v
			return
		}
	}
}

// reset returns the transcript to its empty state — no committed entries and no in-progress
// assistant buffer — while preserving the debug flag (a hidden view toggle, not conversation).
// It is the /clear + /new "start a new session" primitive: the caller re-seeds the one-time
// start-up box with addStartup so the fresh view matches a launch. It does NOT touch the engine's
// memory (ClearContext) — that is the caller's separate, fallible step (model.startNewSession).
func (t *transcript) reset() {
	t.entries = nil
	t.pending = ""
	t.streaming = false
	// t.debug is deliberately preserved across a session reset.
}

// replay appends already-decoded committed entries after whatever the transcript already holds —
// the resume path (decodeTranscript) repainting a stored scrollback beneath the freshly-seeded
// start-up box. It is append-only and never touches the in-progress pending buffer: the entries
// are committed history, while streaming state belongs to this fresh process. The entries were
// escape-stripped on decode, so nothing untrusted from disk reaches the terminal unfiltered.
func (t *transcript) replay(entries []entry) {
	t.entries = append(t.entries, entries...)
}

// hasPrompt reports whether the transcript holds at least one committed user message. It is THE
// save-gate predicate — saveSession, persist and saveAtIdle all funnel through it — so a session
// earns a history record only once a prompt was actually sent. Everything this program can put on
// screen by itself leaves the gate shut: the one-time start-up box, slash-command notes (/confine's
// status line, the /skills catalogue, a /model actuation note, the /sessions browser's notices),
// error notices, and the re-derived ephemeral chrome. Without that rule a launch spent poking at
// slash commands and then quitting files a "Session <date>" record reading 0 messages.
//
// entryInterjected is deliberately excluded, mirroring the rationale on userTexts: an interjection
// is a remark steering an Exchange that an entryUser opened (addInterjected), so a transcript
// holding one always holds that opening entry too — on a resume included, because the stored
// scrollback carries it. Counting interjections could therefore never change the answer, and
// leaving them out keeps the gate exactly "a prompt exists" ⇔ "an entryUser exists".
//
// Accepted consequence: resuming a LEGACY record that carries no transcript blob leaves the
// scrollback with no entryUser, so quitting without prompting skips the final quit-flush. Nothing
// is lost — that record is already on disk; only a cosmetic ctxUsed/UpdatedAt refresh is missed.
func (t *transcript) hasPrompt() bool {
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			return true
		}
	}
	return false
}

// userMessageCount reports how many committed user messages the transcript holds — the browsable
// "N msgs" count the session record carries (session.Meta.UserMsgs).
func (t *transcript) userMessageCount() int {
	n := 0
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			n++
		}
	}
	return n
}

// firstUserText returns the text of the first committed user message, or "" when none has been
// sent yet. The session title is derived from it (sessionTitle) — the first thing the human asked
// is the most recognisable label for the session in the history browser.
func (t *transcript) firstUserText() string {
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			return t.entries[i].text
		}
	}
	return ""
}

// userTexts returns the text of every committed user message, oldest first, and nil when nothing
// has been asked yet. It is the session's user side as a bare `/rename` reads it (runRename): the
// naming call selects its own bounded window out of this (title.Prompt), so what is owed here is
// the whole ordered list rather than a pre-trimmed one.
//
// Interjections are deliberately left out, following firstUserText's line. An entryInterjected is a
// remark steering work already under way (addInterjected) — it is not a request that opened an
// Exchange, which is why it is not an entryUser in the first place — so counting it among the
// session's requests would let a mid-Exchange "wrong file" outweigh the task it was correcting.
func (t *transcript) userTexts() []string {
	var texts []string
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			texts = append(texts, t.entries[i].text)
		}
	}
	return texts
}

// presentedStatus is the short line that closes a presentation entry. A rung that was tried and
// did not deliver says so and states that the path still stands — the entry is the one thing the
// ladder can always promise, so the wording never leaves the user wondering whether anything
// happened. Everything else is a hint about what to do next: an opened document needs none beyond
// the fact, and a path or a URL is one cmd+click away in every terminal that linkifies (Zed,
// VS Code, iTerm2, WezTerm, kitty).
func presentedStatus(v presentedView) string {
	if v.Reason != "" {
		return v.Reason + " — path shown"
	}
	if v.Method == domain.PresentOpened {
		return "opened on your machine"
	}
	return "cmd+click to open"
}

// apply folds one engine Event into the transcript (the C6 rule). The switch covers the
// eight transcript-rendered variants of the eleven-variant Event set, so the rendered set
// stays honest as the engine evolves; the other three are not transcript entries
// (ReasoningEvent feeds the activity line, UsageEvent the status-line stats, AuditEvent
// nothing in the TUI) and fall to the default case with every future variant. Each
// case folds its event: tokens grow the in-progress buffer; a StreamReset discards it; a
// Message commits it (canonical text); the first ToolCall of a Turn finalises the pre-tool
// narration before recording the call; results, approvals, and recovered faults append
// their own entries; a MechanismFired is surfaced only in the debug view. It renders only —
// no agent logic (C5).
func (t *transcript) apply(e domain.Event) {
	switch e := e.(type) {
	case domain.TokenEvent:
		t.appendToken(e.Text)
	case domain.StreamResetEvent:
		t.discardPending()
	case domain.MessageEvent:
		t.commitAssistant(e.Text, e.Depth)
	case domain.ToolCallEvent:
		t.finalizeNarration(e.Depth)
		t.addToolCall(e.Call, e.Depth)
	case domain.ToolResultEvent:
		t.addToolResult(e.Result, e.Depth)
	case domain.ApprovalEvent:
		t.addApproval(e.Request, e.Decision, e.Depth)
	case domain.MechanismFiredEvent:
		t.addMechanism(e)
	case domain.ErrorEvent:
		t.addError(e.Source, e.Err, e.Depth)
	default:
		// An unknown future variant: tolerate it. The set is sealed and additively
		// versioned, so an unrecognised Event is rendered as nothing rather than a panic.
	}
}

// appendToken grows the in-progress assistant buffer with one streamed chunk. The buffer
// is committed by commitAssistant (a MessageEvent) or finalizeNarration (the first
// ToolCall of the Turn), and is never rendered as a committed entry until then. The chunk is
// escape-stripped as it lands (stripEscapes) so no ESC byte from the model's stream ever
// reaches the terminal — even split across two chunks, since the byte is removed per chunk.
func (t *transcript) appendToken(text string) {
	t.streaming = true
	t.pending += stripEscapes(text)
}

// discardPending drops the in-progress assistant buffer for the current Turn. A
// StreamResetEvent signals the loop is re-streaming the Turn (an ActionRetry post-response
// decision re-called the Upstream), so the tokens accumulated so far are superseded and
// must never be committed (events.go contract). The re-stream's tokens arrive next and the
// Turn's MessageEvent carries the final, accepted text.
func (t *transcript) discardPending() {
	t.streaming = false
	t.pending = ""
}

// commitAssistant finalises the streamed buffer into a committed assistant entry on a
// MessageEvent. The MessageEvent's text is canonical (it carries the full, accepted
// message), so it is preferred over the accumulated tokens; the tokens are a live preview
// that should reconcile to the same text (§0 event-sequence rule). A canonical text that is
// blank falls back to the accumulated tokens so nothing streamed is lost, and a text that is
// blank either way commits no entry at all — a lone ✦ marker line is itself an unneeded line.
func (t *transcript) commitAssistant(canonical string, depth int) {
	// canonical is the MessageEvent's untrusted model text; strip its escapes (t.pending was
	// already stripped as it streamed, so a double-strip there is a cheap no-op), then drop the
	// blank lines the model padded the message with, so the block sits exactly one blank line
	// from its neighbours instead of two or three (layout.md).
	text := trimBlankLines(stripEscapes(canonical))
	if text == "" {
		text = trimBlankLines(t.pending)
	}
	t.streaming = false
	t.pending = ""
	if text == "" {
		return
	}
	t.entries = append(t.entries, entry{kind: entryAssistant, text: text, depth: depth})
}

// finalizeNarration commits the in-progress buffer as the pre-tool narration when the first
// ToolCallEvent of a Turn arrives (the C6 rule). A tool Turn emits no MessageEvent, so the
// streamed tokens are the canonical narration text. Only the first ToolCall finalises:
// afterwards streaming is false, so the Turn's remaining ToolCalls add no empty entry. A
// Turn that streamed nothing — or only blank lines — before its tool call commits nothing.
func (t *transcript) finalizeNarration(depth int) {
	if !t.streaming {
		return
	}
	text := trimBlankLines(t.pending)
	t.streaming = false
	t.pending = ""
	if text == "" {
		return
	}
	t.entries = append(t.entries, entry{kind: entryAssistant, text: text, depth: depth})
}

// addToolCall appends a tool-call entry: the presentation view (friendly label + target)
// built from the model's requested call, plus the call ID the paired result folds into. The
// view shows the call verbatim where it cannot summarise it (a malformed argument is rendered
// as-is rather than hidden — the human approving a write must see exactly what was asked).
func (t *transcript) addToolCall(call domain.ToolCall, depth int) {
	t.entries = append(t.entries, entry{
		kind:   entryToolCall,
		depth:  depth,
		callID: call.ID,
		tool:   presentToolCall(call),
	})
}

// addToolResult folds a tool result into its call's block. It scans from the tail for the
// most recent un-paired tool-call entry with a matching CallID and enriches that call's view
// with the result's one-line summary, marking it done. A result the tool flagged as an error
// (IsError) is a normal in-band outcome the model reacts to — not a recovered fault (that is
// ErrorEvent) — so it is summarised, not raised. A result that matches no open call (the
// defensive orphan case) is appended as a standalone result block so its outcome is not lost.
func (t *transcript) addToolResult(result domain.ToolResult, depth int) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.kind == entryToolCall && !e.done && e.callID == result.CallID {
			e.tool.enrichWithResult(result)
			e.done = true
			return
		}
	}
	// The orphan branch is the one path a result takes WITHOUT passing enrichWithResult's seam, so
	// it strips the content itself — it is raw tool output, which a malicious repo controls.
	text := stripEscapes(result.Content)
	if result.IsError {
		text = "error: " + text
	}
	t.entries = append(t.entries, entry{kind: entryToolResult, text: text, depth: depth})
}

// hasOpenToolCall reports whether any tool-call entry is still waiting for its result — the
// signal the live status line uses to stay on the tool phrase while a batch of calls runs. Its
// caller is foldEvent (fold.go), which reads it straight after apply and hands the answer to
// foldActivity, so the phrase can never be derived from a pairing that has not happened yet.
// It reads the same call/result pairing addToolResult maintains, so a call is "open" from the
// moment it is recorded until its result folds into it. A call whose result
// never arrived (a run cancelled mid-tool) stays open forever, which at worst holds the tool
// phrase one event longer after some later result; the next reasoning/token/message event
// moves it on.
func (t *transcript) hasOpenToolCall() bool {
	for i := len(t.entries) - 1; i >= 0; i-- {
		if e := &t.entries[i]; e.kind == entryToolCall && !e.done {
			return true
		}
	}
	return false
}

// addApproval records an Approval observationally — the decision already came back through
// the C3 reply channel, so this is a transcript record of what was decided, not the gate.
//
// The tool name is the MODEL's — a dynamic MCP tool is named by its server and an unregistered one
// is echoed raw — so it is escape-stripped like every other note text. This entry is built here
// rather than through addNote (it carries a depth), which is exactly the kind of bypass that left
// producers unstripped before.
func (t *transcript) addApproval(req domain.ApprovalRequest, decision domain.ApprovalDecision, depth int) {
	text := fmt.Sprintf("approval %s: %s", decision, stripEscapes(req.Tool))
	t.entries = append(t.entries, entry{kind: entryNote, text: text, depth: depth})
}

// addMechanism records a fired Mechanism, but only in the debug view (off by default).
// There is no Mechanism catalogue until Phase 4, so a fired event is observability noise
// for the product UI; the switch handles it now so a Phase-4 Mechanism needs no retrofit.
func (t *transcript) addMechanism(e domain.MechanismFiredEvent) {
	if !t.debug {
		return
	}
	text := fmt.Sprintf("mechanism %s @ %s: %s", e.Mechanism, e.Hook, e.Action)
	t.entries = append(t.entries, entry{kind: entryNote, text: text, depth: e.Depth})
}

// addError appends a recovered-fault notice (ADR 0007 — an ErrorEvent does not stop the
// loop). source is the tool name / mechanism ID / "loop"; msg is the error text.
//
// Both halves are escape-stripped at this seam, as addNote strips its own: an error text routinely
// quotes what failed — a path, a command, an upstream body, an MCP server's message — so it is
// untrusted for exactly the reasons the tool card's content is, and source is the model's own tool
// name when a tool faulted.
func (t *transcript) addError(source, msg string, depth int) {
	t.entries = append(t.entries, entry{kind: entryError, text: stripEscapes(source + ": " + msg), depth: depth})
}

// ----------------------------------------------------------------------------
// Formatting helpers
// ----------------------------------------------------------------------------

// stripEscapes removes the ESC control byte (0x1b) from untrusted text so a model- or
// repo-supplied string can never introduce a terminal escape sequence — an OSC 52 clipboard
// write (\x1b]52;...), a CSI cursor/screen game — at the transcript boundary. Every ANSI
// sequence begins with ESC, so dropping that one byte neutralises the sequence regardless of
// how a streamed chunk split it, while leaving ordinary text (including \n and \t) intact. The
// styling the renderer adds afterwards is applied by lipgloss to already-stripped text, so its
// own escapes are unaffected. Not exploitable in the current layout (the footer always renders
// after transcript content, and the cellbuf drops non-SGR escapes when printable cells follow),
// but a trailing-position escape DOES survive the cellbuf — this closes that gap at the source.
func stripEscapes(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s // the overwhelmingly common case: no ESC, no allocation
	}
	return strings.ReplaceAll(s, "\x1b", "")
}

// stripEscapesAll escape-strips every string in xs, returning a new slice (nil for nil), so a
// batch of untrusted labels (attached-skill display names) is sanitized in one call.
func stripEscapesAll(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, len(xs))
	for i, s := range xs {
		out[i] = stripEscapes(s)
	}
	return out
}

// blankLine reports whether ln carries nothing visible — it is empty or whitespace only. It is
// the single definition of "blank" the layout's blank-line hygiene rests on: the commit-time
// trim, the streaming preview's trim, and the markdown collapse all ask this one question.
func blankLine(ln string) bool {
	return strings.TrimSpace(ln) == ""
}

// trimBlankLines drops the leading and trailing blank lines of s, leaving its interior intact.
// Model text routinely arrives padded with a trailing "\n\n" (and sometimes a leading one); each
// such line renders as a blank row on top of the renderer's own one-line block separator, so the
// transcript grows two- and three-line gaps. Trimming at the commit boundary makes layout.md's
// "exactly one empty line between blocks" true rather than aspirational.
func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	i := 0
	for i < len(lines) && blankLine(lines[i]) {
		i++
	}
	j := len(lines)
	for j > i && blankLine(lines[j-1]) {
		j--
	}
	return strings.Join(lines[i:j], "\n")
}

// trimTrailingBlankLines drops only the trailing blank lines of s. It is the render-time trim for
// the still-streaming buffer: a mid-stream "\n\n" may be a paragraph break the model is about to
// continue, so the buffer itself is never touched and a leading blank line is left alone — only
// the trailing gap, which would otherwise wobble as tokens arrive, is held back from the display.
func trimTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	j := len(lines)
	for j > 0 && blankLine(lines[j-1]) {
		j--
	}
	return strings.Join(lines[:j], "\n")
}

// prettyJSON re-renders raw JSON arguments as indented, human-readable text. Empty or null
// arguments render as nothing; arguments that do not parse are returned trimmed-but-verbatim
// so a malformed tool argument is still shown rather than silently dropped.
func prettyJSON(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return trimmed
	}
	return buf.String()
}
