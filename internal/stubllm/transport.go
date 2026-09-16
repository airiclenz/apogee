package stubllm

import (
	"errors"
	"io"
	"net/http"
	"sync"
)

// pipeTransport serves every request from one handler in process. It is what
// [Server.Transport] returns: the handler runs on its own goroutine, writing into an io.Pipe
// whose read end is the response body, so the client sees the reply exactly as it is flushed
// — a streamed delta at a time, a held `await:` reply not at all until released — without a
// socket between them.
type pipeTransport struct {
	handler http.Handler
}

// errKilledBeforeReply is the RoundTrip error for a handler aborted before it wrote a status:
// the socket equivalent is a connection reset before any byte of the reply.
var errKilledBeforeReply = errors.New("stubllm: connection killed before the reply")

// RoundTrip runs the handler and returns as soon as the status and headers are known — the
// moment a real client returns from Do — leaving the body to stream through the pipe. A
// handler that returns without writing a status is served as a 200 with whatever it wrote,
// the way net/http does; one that aborts with http.ErrAbortHandler before any status is a
// transport error, and one that aborts mid-body closes the pipe with io.ErrUnexpectedEOF.
func (t pipeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	reader, writer := io.Pipe()
	response := &pipeResponse{header: http.Header{}, body: writer, ready: make(chan struct{})}
	go t.serve(response, request, writer)

	<-response.ready
	if response.aborted {
		_ = reader.Close()
		return nil, errKilledBeforeReply
	}
	return &http.Response{
		Status:     http.StatusText(response.status),
		StatusCode: response.status,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     response.sent,
		Body:       reader,
		Request:    request,
	}, nil
}

// serve runs the handler to completion on its own goroutine and ends the pipe the way the
// handler ended: cleanly when it returned, with an unexpected EOF when kill aborted it.
func (t pipeTransport) serve(response *pipeResponse, request *http.Request, writer *io.PipeWriter) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if !errors.Is(toError(recovered), http.ErrAbortHandler) {
				panic(recovered)
			}
			response.abort()
			_ = writer.CloseWithError(io.ErrUnexpectedEOF)
			return
		}
		response.commit(http.StatusOK)
		_ = writer.Close()
	}()
	t.handler.ServeHTTP(response, request)
}

// toError is the recovered panic value as an error, so an aborted handler is told apart from
// any other panic — which is re-raised, because it is a bug and not a scripted fault.
func toError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return errors.New("stubllm: handler panic")
}

// pipeResponse is the http.ResponseWriter the in-process transport hands a handler. It is a
// Flusher and deliberately NOT a Hijacker: kill falls through to the abort path on it, which
// is the one way a pipe can end the way a dropped socket does.
type pipeResponse struct {
	header http.Header
	body   *io.PipeWriter

	once    sync.Once
	ready   chan struct{}
	status  int
	sent    http.Header
	aborted bool
}

func (r *pipeResponse) Header() http.Header { return r.header }

// WriteHeader commits the status and a snapshot of the headers, exactly once; later calls
// are ignored as net/http ignores them.
func (r *pipeResponse) WriteHeader(status int) { r.commit(status) }

// Write commits an implicit 200 on the first write, then streams the bytes into the pipe.
// The pipe write blocks until the client reads, so a handler is paced by its reader as it is
// by a socket's window — and errors once the client has closed the body, ending the handler.
func (r *pipeResponse) Write(p []byte) (int, error) {
	r.commit(http.StatusOK)
	return r.body.Write(p)
}

// Flush commits the status if nothing has yet. Bytes already written are already readable —
// an unbuffered pipe has nothing to flush — so committing the headers is the whole effect,
// and it is the effect that matters: a handler that flushes its headers before the first
// delta is telling the client the stream has started.
func (r *pipeResponse) Flush() { r.commit(http.StatusOK) }

// commit records the status and headers and unblocks RoundTrip, once.
func (r *pipeResponse) commit(status int) {
	r.once.Do(func() {
		r.status = status
		r.sent = r.header.Clone()
		close(r.ready)
	})
}

// abort marks the reply as killed before any status was committed, so RoundTrip fails
// instead of returning a response; after a commit it is a no-op, and the pipe's close error
// carries the fault.
func (r *pipeResponse) abort() {
	r.once.Do(func() {
		r.aborted = true
		close(r.ready)
	})
}
