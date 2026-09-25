package stubllm

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Replayer serves a [Cassette] as an upstream: it answers every completion POST —
// /v1/chat/completions and /v1/messages — with the exchange recorded under the request's key,
// replaying its status, its header and its raw chunks at their recorded offsets, and answers
// GET /v1/models and /props with the stored probe replies.
//
// Unlike a [Server] it interprets nothing: there is no [Script], no [Turn] matching and no wire
// rendering — the bytes are the captured upstream's own, so a replay is that upstream, pacing
// included.
//
// # Queues and retries
//
// A key's exchanges are served in capture order, one per request. A key requested past its
// recorded count re-serves its last exchange, so a client that retries — the provider retries
// a 5xx, a 429 or a transport fault — sees the reply that finally answered it in the capture.
//
// # Unknown keys
//
// A request whose key the cassette does not hold answers HTTP 500, naming the key and the
// request's first user message (as keyed, dates normalized) in the body and in a log line —
// strict for the same reason a [Server] is: an improvised reply would hide the moment a replayed
// session drifted from the captured one.
//
// # Pace
//
// Every chunk is written at its recorded offset times the pace factor, measured from the
// request's arrival: 1 is the captured speed, 0.5 twice as fast, 0 as fast as the client reads.
// Requests on different keys stream concurrently; nothing but the key's queue position is
// shared between them.
type Replayer struct {
	pace   float64
	queues map[string][]Exchange
	probes map[string]ProbeReply
	logf   func(format string, args ...any)
	mu     sync.Mutex
	served map[string]int
}

// NewReplayer returns a Replayer serving c at the given pace. A negative pace is taken as 0.
// The cassette is read once, here: later changes to c do not reach the replay.
func NewReplayer(c *Cassette, pace float64) *Replayer {
	replayer := &Replayer{
		pace:   max(pace, 0),
		queues: make(map[string][]Exchange),
		probes: make(map[string]ProbeReply),
		logf:   log.Printf,
		served: make(map[string]int),
	}
	if c == nil {
		return replayer
	}
	for _, exchange := range c.Exchanges {
		replayer.queues[exchange.Key] = append(replayer.queues[exchange.Key], exchange)
	}
	for _, probe := range c.Probes {
		replayer.probes[probe.Path] = probe
	}
	return replayer
}

// ServeHTTP answers one request from the cassette.
func (r *Replayer) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	arrived := time.Now()
	path := request.URL.Path
	switch {
	case request.Method == http.MethodPost && (path == chatCompletionsPath || path == messagesPath):
		r.serveExchange(w, request, arrived)
	case request.Method == http.MethodGet && (path == modelsPath || path == propsPath):
		r.serveProbe(w, path)
	default:
		http.NotFound(w, request)
	}
}

// serveProbe answers a discovery GET with its stored reply, or 404 when the capture holds none.
func (r *Replayer) serveProbe(w http.ResponseWriter, path string) {
	probe, ok := r.probes[path]
	if !ok {
		http.Error(w, fmt.Sprintf("stubllm: the cassette holds no reply for %s", path), http.StatusNotFound)
		return
	}
	if probe.ContentType != "" {
		w.Header().Set("Content-Type", probe.ContentType)
	}
	w.WriteHeader(probe.Status)
	_, _ = io.WriteString(w, probe.Body)
}

// serveExchange keys a completion request and replays the exchange its queue position names.
func (r *Replayer) serveExchange(w http.ResponseWriter, request *http.Request, arrived time.Time) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("stubllm: read request: %v", err), http.StatusBadRequest)
		return
	}

	key := cassetteKey(request.URL.Path, body)
	exchange, ok := r.next(key)
	if !ok {
		r.refuseUnknown(w, request.URL.Path, key, body)
		return
	}
	r.play(request.Context(), w, exchange, arrived)
}

// next returns the exchange a request under key is answered with and advances the key's queue:
// the next unserved exchange, or the last one once the queue is spent.
func (r *Replayer) next(key string) (Exchange, bool) {
	queue := r.queues[key]
	if len(queue) == 0 {
		return Exchange{}, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	position := min(r.served[key], len(queue)-1)
	r.served[key]++
	return queue[position], true
}

// refuseUnknown answers a request the cassette holds no exchange for.
func (r *Replayer) refuseUnknown(w http.ResponseWriter, path, key string, body []byte) {
	firstUser := "<undecodable request>"
	if c, err := conversationOf(path, body); err == nil {
		firstUser = c.FirstUser
	}
	message := fmt.Sprintf(
		"stubllm: the cassette holds no exchange for key %s (first user message %q)", key, firstUser)
	r.logf("%s", message)
	http.Error(w, message, http.StatusInternalServerError)
}

// play writes an exchange: its header and status, then each chunk at its paced offset from the
// request's arrival, flushed as it goes. A client that hangs up ends the replay; an exchange
// captured truncated ends by dropping the connection, as the upstream did.
func (r *Replayer) play(ctx context.Context, w http.ResponseWriter, exchange Exchange, arrived time.Time) {
	for name, values := range exchange.Header {
		w.Header()[name] = append([]string(nil), values...)
	}
	w.WriteHeader(exchange.Status)
	flusher, _ := w.(http.Flusher)

	for _, chunk := range exchange.Chunks {
		if !r.waitUntil(ctx, arrived.Add(r.paced(chunk.Offset))) {
			return
		}
		if _, err := w.Write(chunk.Bytes); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if exchange.Truncated {
		kill(w)
	}
}

// paced is a recorded offset scaled by the pace factor.
func (r *Replayer) paced(offset time.Duration) time.Duration {
	return time.Duration(float64(offset) * r.pace)
}

// waitUntil blocks until the deadline, reporting false when ctx ends first.
func (r *Replayer) waitUntil(ctx context.Context, deadline time.Time) bool {
	wait := time.Until(deadline)
	if wait <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
