package provider

import "net/http"

// fault is the classified shape of one upstream failure, before any surface renders it.
// The four renderers — statusError and inBandError on the blocking surface, statusDelta and
// inBandErrorDelta on the streaming one — used to keep the same three rules by prose, each
// copy promising to mirror the others; classify owns them once and the renderers only read
// the verdict. Bead apogee-6fp's second dialect adds one classifier arm here, not four.
type fault struct {
	// overflow reports a context-window rejection: a 400 whose text carries one of the
	// isContextOverflow markers. It renders as ErrContextOverflow / DeltaContextOverflow, and
	// it is never retryable and never hinted — a prompt too long stays too long, and no
	// thinking effort caused it.
	overflow bool
	// code is the status the fault carries into StatusError.Code and the in-band Delta text:
	// the HTTP status, or an in-band error's numeric code (0 when the server sent a slug or
	// none at all).
	code int
	// retryable reports that the fault's class is one the client's own HTTP retry policy
	// covers (isRetryableStatus: 429 and 5xx) or an aggregator's "provider_unavailable"
	// slug — transient even when it arrives with a 4xx or a non-numeric code. Only
	// inBandErrorDelta renders it: a status the blocking client already retried, and
	// statusDelta's Delta keeps Retryable false for the same reason (see Delta.Retryable).
	retryable bool
	// hinted reports that thinkingEffortHint belongs on the rendered text: the failed request
	// expressed a thinking effort in some dialect and the fault is not an overflow. The hint
	// rides the wrapping error or the Delta text, never StatusError.Body.
	hinted bool
}

// classify turns one upstream failure into its fault. It is pure and key-less: text is the
// UNSANITISED body (a non-2xx reply) or in-band error message (an error member under a 200)
// — the overflow marker is read off the raw bytes, since sanitize truncates at
// maxErrorLength and the marker may sit past that cut. status is the HTTP status or the
// in-band error's intCode; errType the in-band error_type slug ("" on the status surface);
// effort reports that the failed request carried a thinking effort in some dialect.
func classify(status int, errType, text string, effort bool) fault {
	f := fault{code: status}
	if status == http.StatusBadRequest && isContextOverflow(text) {
		f.overflow = true
		return f
	}
	f.retryable = isRetryableStatus(status) || errType == providerUnavailable
	f.hinted = effort
	return f
}
