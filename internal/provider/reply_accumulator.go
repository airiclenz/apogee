package provider

import (
	"fmt"
	"io"
	"strings"
)

// replyAccumulator is what one streamed reply accumulates between its SSE payloads and the one
// place such a reply ends, whatever wire it came over. It owns the scan loop (run), the reply
// text cap (charge, text), the open tool calls and their two caps (foldCall), the count of
// payloads a codec could not decode (skipMalformed), the finish reason, usage and served model
// the terminal Done carries, and every terminal path: the wire's terminator and the server
// closing without one (finish), a body read that faults (run), an in-band error (fail), a
// codec-local fault (failLocal), a cap crossed, and a consumer that stops listening.
//
// A codec keeps only its dialect: it decodes each payload, names what the payload carries and
// hands it over through the methods below — never through a field. Which event is the
// terminator, how a stop reason maps, whether usage merges or replaces and which model id wins
// are the codec's own rules, applied before it calls a setter.
//
// The stream ends exactly once. After a terminal Delta is yielded, or yield returns false,
// every method is a no-op reporting ended, so a codec may return whatever a method reports
// and at most one terminal Delta ever reaches the consumer.
type replyAccumulator struct {
	yield func(Delta) bool
	// epilogue, when non-nil, is the codec's last word before the reply's tool calls and its
	// Done: finish runs it after the malformed-only check and before the flush, on the
	// terminator and on a bare EOF alike. Returning false stops the stream without a Done.
	epilogue func() bool
	// open holds every tool call under accumulation; nothing of it is yielded before finish.
	open openToolCalls
	// textBytes is the running total a codec has charged against maxReplyTextBytes. Tool-call
	// bytes are not in it — openToolCalls carries its own cap.
	textBytes int
	// malformed counts the payloads the codec skipped; it rides the terminal Delta of every
	// path but a cap fault and a codec-local fault.
	malformed int
	// finishReason, usage and model are what the reply said about itself so far; they ride the
	// terminal Done. An unset finish reason reads as defaultFinishReason there.
	finishReason string
	usage        *Usage
	model        string
	// ended is set once the stream has ended, by a terminal Delta or a consumer that broke.
	ended bool
}

// defaultFinishReason is the finish reason a reply ends on when the stream named none.
const defaultFinishReason = "stop"

// newReplyAccumulator is the accumulator for one streamed reply. toolFragment, when non-nil,
// runs on every tool-call fragment as it arrives (openToolCalls); yield receives every Delta;
// epilogue, when non-nil, runs inside finish (replyAccumulator.epilogue).
func newReplyAccumulator(toolFragment func(), yield func(Delta) bool, epilogue func() bool) *replyAccumulator {
	return &replyAccumulator{
		yield:    yield,
		epilogue: epilogue,
		open:     openToolCalls{onFragment: toolFragment},
	}
}

// run reads the SSE body line by line and hands the payload of every `data:` line to event,
// until event reports the stream ended or the body does. A read that fails before then is the
// terminal read fault — retryable when the failure is the connection's, not the reply's
// (isTransientReadError) — and a body that simply closes ends the reply on what it sent
// (finish), so a finish reason and usage already received are kept.
func (a *replyAccumulator) run(body io.Reader, event func(data string) (ended bool)) {
	scanner := newSSEScanner(body)
	for !a.ended && scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}
		if event(strings.TrimPrefix(line, sseDataPrefix)) {
			return
		}
	}
	if a.ended {
		return
	}

	if err := scanner.Err(); err != nil {
		a.end(Delta{
			Kind:            DeltaError,
			Err:             fmt.Sprintf("apogee: read stream: %v", err) + malformedChunksNote(a.malformed),
			Retryable:       isTransientReadError(err),
			MalformedChunks: a.malformed,
			Fault:           readFault(err),
		})
		return
	}

	a.finish()
}

// skipMalformed counts one payload the codec could not decode. The payload is skipped so one
// bad chunk cannot kill a stream that is otherwise fine, but never silently: the count rides
// the terminal Delta, and a stream that carried nothing else is faulted on it (finish).
func (a *replyAccumulator) skipMalformed() {
	if a.ended {
		return
	}
	a.malformed++
}

// charge counts n more reply bytes against maxReplyTextBytes. Crossing the cap is terminal and
// NOT retryable — the same request re-streamed would overflow again — and the bytes that
// crossed it are never yielded, so what the consumer received stays under it. The fault
// carries neither the malformed count nor its note.
func (a *replyAccumulator) charge(n int) (ended bool) {
	if a.ended {
		return true
	}
	a.textBytes += n
	if a.textBytes > maxReplyTextBytes {
		return a.end(Delta{
			Kind: DeltaError,
			Err: fmt.Sprintf(
				"apogee: streamed reply exceeded the %d MiB text limit",
				maxReplyTextBytes>>20,
			),
			Fault: UpstreamFault{Class: FaultLocal},
		})
	}
	return false
}

// emit yields one Delta that costs nothing against the text cap, or whose bytes the codec has
// already charged, and reports ended when the consumer broke.
func (a *replyAccumulator) emit(d Delta) (ended bool) {
	if a.ended {
		return true
	}
	if !a.yield(d) {
		a.ended = true
	}
	return a.ended
}

// text yields one content or thinking fragment of n bytes under maxReplyTextBytes: charge,
// then emit. A fragment of no bytes is charged nothing and not yielded.
func (a *replyAccumulator) text(d Delta, n int) (ended bool) {
	if a.ended {
		return true
	}
	if n == 0 {
		return false
	}
	if a.charge(n) {
		return true
	}
	return a.emit(d)
}

// foldCall folds one tool-call fragment into the open set. Crossing maxToolCallBytes or
// opening a call past maxOpenToolCalls is terminal, and the fault text is the set's own
// (openToolCalls.tripped), so every wire renders one message.
func (a *replyAccumulator) foldCall(frag sseToolCall) (ended bool) {
	if a.ended {
		return true
	}
	if a.open.fold(frag) {
		return a.end(Delta{Kind: DeltaError, Err: a.open.tripped, Fault: UpstreamFault{Class: FaultLocal}})
	}
	return false
}

// setFinish records the reply's finish reason, already in the loop's vocabulary.
func (a *replyAccumulator) setFinish(reason string) {
	if a.ended {
		return
	}
	a.finishReason = reason
}

// setUsage records the reply's usage, replacing what was recorded before; a codec whose
// events each carry a part merges them itself and sets the whole.
func (a *replyAccumulator) setUsage(u Usage) {
	if a.ended {
		return
	}
	a.usage = &u
}

// setModel records the id the server put on the reply; which of several ids wins is the
// codec's rule.
func (a *replyAccumulator) setModel(id string) {
	if a.ended {
		return
	}
	a.model = id
}

// fail ends the stream on an in-band error — the failure a server frames inside its 200
// stream — stamping the malformed count onto the codec's rendering of it. Every tool call
// accumulated so far is dropped with it: none has been yielded, so the reply is faulted, not
// partly usable.
func (a *replyAccumulator) fail(d Delta) (ended bool) {
	if a.ended {
		return true
	}
	d.MalformedChunks = a.malformed
	return a.end(d)
}

// failLocal ends the stream on a fault the codec raised itself, yielded as given: it carries
// no malformed count.
func (a *replyAccumulator) failLocal(d Delta) (ended bool) {
	if a.ended {
		return true
	}
	return a.end(d)
}

// patchOpenCalls rewrites the accumulated arguments of every open tool call through fn. It is
// for the epilogue: a codec's last correction to its calls before finish flushes them.
func (a *replyAccumulator) patchOpenCalls(fn func(args string) string) {
	if a.ended {
		return
	}
	for _, e := range a.open.entries {
		e.call.Function.Arguments = fn(e.call.Function.Arguments)
	}
}

// finish ends the stream on its success path — the wire's terminator, or the server closing
// without one: the codec's epilogue, every accumulated tool call, then the terminal Done.
// Both ends come through here, so the malformed-only fault is judged once: a stream that
// charged no text, opened no tool call and skipped at least one chunk carried nothing the
// consumer could commit, and an empty Done would let it pose as a finished empty reply.
func (a *replyAccumulator) finish() {
	if a.ended {
		return
	}
	if a.textBytes == 0 && len(a.open.entries) == 0 && a.malformed > 0 {
		a.end(Delta{
			Kind:            DeltaError,
			Err:             fmt.Sprintf(malformedOnlyErrFmt, a.malformed),
			MalformedChunks: a.malformed,
			Fault:           UpstreamFault{Class: FaultLocal},
		})
		return
	}
	if a.epilogue != nil && !a.epilogue() {
		a.ended = true
		return
	}
	if a.ended {
		return
	}
	if !a.open.flush(a.yield) {
		a.ended = true
		return
	}
	reason := a.finishReason
	if reason == "" {
		reason = defaultFinishReason
	}
	a.end(Delta{
		Kind:            DeltaDone,
		FinishReason:    reason,
		Usage:           a.usage,
		Model:           a.model,
		MalformedChunks: a.malformed,
	})
}

// end yields the stream's terminal Delta and marks the stream ended; it always reports ended.
func (a *replyAccumulator) end(d Delta) (ended bool) {
	a.ended = true
	a.yield(d)
	return true
}
