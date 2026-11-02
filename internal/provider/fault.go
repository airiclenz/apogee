package provider

import "net/http"

// fault is the classified shape of one upstream failure, before any surface renders it.
// The four renderers — statusError and inBandError on the blocking surface, statusDelta and
// inBandErrorDelta on the streaming one — used to keep the same three rules by prose, each
// copy promising to mirror the others; classify owns them once and the renderers only read
// the verdict. Issue apogee-6fp's second wire added its one classifier arm here, not four:
// the Messages API frames its class as an error-type slug (anthropicOverloaded and the
// others below) with no numeric code, and classify reads those slugs beside the aggregator's.
type fault struct {
	// overflow reports a context-window rejection: a 400 — or an in-band
	// anthropicInvalidRequest — whose text carries one of the isContextOverflow markers. It
	// renders as ErrContextOverflow / DeltaContextOverflow, and it is never retryable and never
	// hinted — a prompt too long stays too long, and no thinking effort caused it.
	overflow bool
	// code is the status the fault carries into StatusError.Code and the in-band Delta text:
	// the HTTP status, or an in-band error's numeric code (0 when the server sent a slug or
	// none at all).
	code int
	// retryable reports that the fault's class is one the client's own HTTP retry policy
	// covers (isRetryableStatus: 429 and 5xx), an aggregator's "provider_unavailable" slug
	// or the Messages API's overloaded / rate-limited / api_error slugs — transient even when
	// they arrive with a 4xx or a non-numeric code. Only
	// inBandErrorDelta renders it: a status the blocking client already retried, and
	// statusDelta's Delta keeps Retryable false for the same reason (see Delta.Retryable).
	retryable bool
	// hinted reports that thinkingEffortHint belongs on the rendered text: the failed request
	// expressed a thinking effort in some dialect and the fault is not an overflow. The hint
	// rides the wrapping error or the Delta text, never StatusError.Body.
	hinted bool
}

// The Messages API's error-class slugs classify reads (ADR 0078). They ride wireError.ErrorType
// — the anthropic codec maps its `error.type` there — and none carries a numeric code, so the
// slug is the whole verdict: an overload is the wire's 529, a rate limit its 429 and an
// api_error its 500 whatever status framed them (0 in-band), and a prompt too long is an
// invalid request whose message carries the isContextOverflow marker.
const (
	anthropicOverloaded     = "overloaded_error"
	anthropicRateLimited    = "rate_limit_error"
	anthropicAPIError       = "api_error"
	anthropicInvalidRequest = "invalid_request_error"
)

// classify turns one upstream failure into its fault. It is pure and key-less: text is the
// UNSANITISED body (a non-2xx reply) or in-band error message (an error member under a 200)
// — the overflow marker is read off the raw bytes, since sanitize truncates at
// maxErrorLength and the marker may sit past that cut. status is the HTTP status or the
// in-band error's intCode; errType the in-band error_type slug ("" on the status surface);
// effort reports that the failed request carried a thinking effort in some dialect.
// An in-band anthropicRateLimited or anthropicAPIError with no code of its own is given the
// 429 or 500 it stands for (slugStatus), so the rendered text names the class the way a
// status would have and isTransientFault reads it as the status it is.
func classify(status int, errType, text string, effort bool) fault {
	f := fault{code: status}
	if (status == http.StatusBadRequest || errType == anthropicInvalidRequest) && isContextOverflow(text) {
		f.overflow = true
		return f
	}
	if f.code == 0 {
		f.code = slugStatus(errType)
	}
	f.retryable = isTransientFault(f.code, errType)
	f.hinted = effort
	return f
}

// slugStatus is the HTTP status a Messages API slug stands for, or 0 for a slug that names
// none: classify gives a code-0 in-band error carrying anthropicRateLimited or
// anthropicAPIError the 429 or 500 it names. An anthropicOverloaded maps to 0 on purpose — it
// keeps its code 0 and is transient by its slug (isTransientFault), as its rendered
// "in-band error 0:" text has always named it.
func slugStatus(errType string) int {
	switch errType {
	case anthropicRateLimited:
		return http.StatusTooManyRequests
	case anthropicAPIError:
		return http.StatusInternalServerError
	default:
		return 0
	}
}

// isTransientFault reports whether an upstream fault with this code and error-type slug is of a
// transient class — 429 or 5xx (isRetryableStatus, which a code-0 in-band anthropicRateLimited
// or anthropicAPIError reaches as the 429 or 500 classify gave it), an aggregator's
// providerUnavailable, or the Messages API's anthropicOverloaded (its mid-stream 529) —
// whatever framed it. It is the one
// rule behind classify's retryable verdict and UpstreamFault.FallbackEligible's status and
// in-band arms, so the two can never disagree on which class is the upstream's to recover from.
func isTransientFault(code int, errType string) bool {
	return isRetryableStatus(code) ||
		errType == providerUnavailable ||
		errType == anthropicOverloaded
}

// FaultClass names where one terminal DeltaError came from. The zero value, FaultNone, is what
// every non-error Delta carries and what a Responder that classifies nothing (a test fake) leaves
// on its faults; the Client sets one of the others on every DeltaError it yields.
type FaultClass string

const (
	// FaultNone is the zero class: no fault classified.
	FaultNone FaultClass = ""
	// FaultTransport is a request that never got a response: every attempt send made failed at
	// the connection (refused, reset, unresolvable host) and the retry budget is spent.
	FaultTransport FaultClass = "transport"
	// FaultTimeout is the idle cut (errStreamIdle — an upstream silent for the whole
	// WithStreamIdleTimeout window, before its headers or mid-body) or a network timeout on a
	// body read.
	FaultTimeout FaultClass = "timeout"
	// FaultStatus is a non-2xx response; UpstreamFault.Code carries its HTTP status.
	FaultStatus FaultClass = "status"
	// FaultCut is a body read that failed before the stream's terminator for a reason other than
	// a timeout — the connection dropped mid-chunk (io.ErrUnexpectedEOF) or was reset.
	FaultCut FaultClass = "cut"
	// FaultInBand is an error member the server sent under a 200; UpstreamFault.Code carries its
	// numeric code (0 when it sent a slug or none) and UpstreamFault.ErrorType its slug.
	FaultInBand FaultClass = "in-band"
	// FaultLocal is a fault apogee raised itself over a request or a reply the upstream did not
	// fault: a request that would not marshal, a reply past the text cap or the tool-call caps, a
	// line past the scanner's buffer, a reasoning block that would not encode, or a stream that
	// decoded nothing but malformed chunks.
	FaultLocal FaultClass = "local"
	// FaultCancelled is the caller's context ending the stream, before or after the headers.
	FaultCancelled FaultClass = "cancelled"
)

// UpstreamFault is the machine-readable class of one terminal DeltaError (Delta.Fault): where it
// came from and, for a status or an in-band error, the code and slug it carried. It sits beside
// Delta.Retryable rather than replacing it: Retryable answers "may this same request be
// re-streamed on this upstream", and a status fault keeps it false because send already retried
// it; UpstreamFault answers "what failed", which is what a caller deciding to move off the
// upstream altogether reads (FallbackEligible).
type UpstreamFault struct {
	// Class is where the fault came from.
	Class FaultClass
	// Code is the HTTP status of a FaultStatus, or the numeric code of a FaultInBand as the
	// rendered "in-band <code>:" text names it (an anthropicRateLimited slug's 429 and an
	// anthropicAPIError slug's 500 included);
	// 0 on every other class.
	Code int
	// ErrorType is a FaultInBand's error-type slug as the server sent it ("" when it sent none,
	// and on every other class).
	ErrorType string
}

// FallbackEligible reports whether the fault is the upstream's own failure — one that a
// different server could be expected not to share: a connection that failed after send's
// retries, the idle cut or a network timeout, a stream cut mid-body, a 429 or 5xx status (529
// included), or an in-band error of the same transient classes (isTransientFault: 429, 5xx —
// an anthropicAPIError's 500 among them — providerUnavailable, anthropicOverloaded). Every other fault is false — any other 4xx (the
// request itself is wrong, and a fallback would be sent the same request), a fault apogee raised
// itself, the caller's cancel, and FaultNone. A context overflow is never a DeltaError, so it
// never reaches here.
func (f UpstreamFault) FallbackEligible() bool {
	switch f.Class {
	case FaultTransport, FaultTimeout, FaultCut:
		return true
	case FaultStatus, FaultInBand:
		return isTransientFault(f.Code, f.ErrorType)
	default:
		return false
	}
}
