package present

import (
	"net"
	"sync"
)

// limitListener bounds how many connections a listener holds at once. It is the doc server's
// answer to the one resource an unauthenticated peer can spend without a token: connections. The
// listener binds every interface and a wrong token is still a served 404, so anything that can
// route to this box can otherwise open keep-alives until the PROCESS runs out of file descriptors
// — and the descriptors it runs out of are the agent's, not the doc server's.
//
// It SHEDS rather than queues. golang.org/x/net/netutil's LimitListener stops accepting while it
// is full, which leaves the excess sitting in the kernel's backlog: the peer's connect succeeds,
// nothing answers, and the flood is merely parked. Closing the excess immediately keeps the accept
// loop draining, hands the peer an unambiguous answer, and holds each shed connection for the
// microseconds between accept and close. The cost is that a legitimate fetch arriving while the
// cap is saturated is refused rather than delayed — an honest connection error the user can retry,
// against a rung whose baseline (the path in the transcript) is already in front of them.
//
// A slot is held from accept until the connection is closed, and released exactly once however
// often Close is called — net/http closes a connection on its own error paths as well as at the
// end of serving it.
type limitListener struct {
	net.Listener
	limit int

	mu   sync.Mutex
	held int
}

// newLimitListener wraps inner so that at most limit connections are held at once.
func newLimitListener(inner net.Listener, limit int) *limitListener {
	return &limitListener{Listener: inner, limit: limit}
}

// Accept returns the next connection there is room for, closing and skipping any that arrives
// while the cap is saturated. It only returns an error when the underlying listener does — a
// shed connection is not a failure of the listener, so the accept loop above it never sees one.
func (l *limitListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if !l.acquire() {
			// Nothing to say to a peer we are not serving, and nothing to do about a failure to
			// close a connection we are already dropping.
			_ = conn.Close()
			continue
		}
		return &limitConn{Conn: conn, release: l.release}, nil
	}
}

// acquire takes a slot, reporting whether there was one.
func (l *limitListener) acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.held >= l.limit {
		return false
	}
	l.held++
	return true
}

// release returns a slot.
func (l *limitListener) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.held > 0 {
		l.held--
	}
}

// inFlight reports how many connections are currently held. It exists for the tests that pin the
// cap: everything else asks the operating system.
func (l *limitListener) inFlight() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.held
}

// limitConn is a served connection that returns its slot when it closes.
type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

// Close releases this connection's slot — once, however many times it is called — and closes the
// connection underneath.
func (c *limitConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}
