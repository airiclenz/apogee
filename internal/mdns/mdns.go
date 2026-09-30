// Package mdns resolves an mDNS `.local` host name to its addresses with one multicast query,
// for the release builds whose pure-Go resolver (CGO_ENABLED=0) cannot reach the system's mDNS
// responder. It is a one-shot legacy unicast lookup (RFC 6762 §5.1, §6.7): the query leaves an
// ephemeral UDP port for the IPv4 group 224.0.0.251:5353 and the responder answers that port
// directly. No service discovery, no cache, no IPv6 multicast.
//
// Lookup is the whole surface; building the query and reading the replies stay inside.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// lookupTimeout caps one Lookup when the caller's context allows longer: a responder on the LAN
// answers within milliseconds, so a second of silence means nothing will.
const lookupTimeout = time.Second

// maxPacketSize is the largest reply read; RFC 6762 §17 bounds an mDNS packet at 9000 bytes.
const maxPacketSize = 9000

// classMask clears the top bit of an mDNS record's class, which RFC 6762 §10.2 spends on the
// cache-flush flag rather than the class itself.
const classMask = 0x7fff

// errNoAnswer reports that no responder announced an address for the host before the deadline.
var errNoAnswer = errors.New("mdns: no responder answered")

// resolver holds the knobs a test aims elsewhere: where the query goes and how long it waits.
type resolver struct {
	dest    netip.AddrPort
	timeout time.Duration
}

// defaultResolver is the one Lookup uses: the IPv4 mDNS group and the one-second cap.
var defaultResolver = resolver{
	dest:    netip.MustParseAddrPort("224.0.0.251:5353"),
	timeout: lookupTimeout,
}

// Lookup returns the A and AAAA addresses an mDNS responder on the local network announces for
// host (for example "Apollo-II.local", a trailing dot allowed). It waits until the earlier of
// ctx's deadline and one second, and returns a non-nil error when no responder answered in
// time, when ctx ends first, or when host is not a valid DNS name.
func Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return defaultResolver.lookup(ctx, host)
}

// lookup sends one query for host's A and AAAA records to r.dest and returns the addresses of
// the first reply that answers for host.
func (r resolver) lookup(ctx context.Context, host string) (addrs []netip.Addr, err error) {
	fqdn, err := fullyQualified(host)
	if err != nil {
		return nil, err
	}
	query, err := buildQuery(fqdn)
	if err != nil {
		return nil, fmt.Errorf("mdns: query for %q: %w", host, err)
	}

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("mdns: open socket: %w", err)
	}
	// A close failure joins a failed lookup's error; it never voids addresses already received.
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err != nil {
			err = errors.Join(err, fmt.Errorf("mdns: close socket: %w", closeErr))
		}
	}()

	deadline := time.Now().Add(r.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("mdns: set deadline: %w", err)
	}
	// A cancelled ctx cuts the wait short by pulling the deadline to now.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if _, err := conn.WriteToUDPAddrPort(query, r.dest); err != nil {
		return nil, r.waitError(ctx, host, fmt.Errorf("mdns: send query for %q: %w", host, err))
	}

	buf := make([]byte, maxPacketSize)
	for {
		n, _, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return nil, r.waitError(ctx, host, err)
		}
		if addrs = parseReply(buf[:n], fqdn); len(addrs) > 0 {
			return addrs, nil
		}
	}
}

// waitError turns a failed send or read into the error Lookup reports: ctx's own error when
// ctx ended, errNoAnswer when the deadline passed in silence, the socket error otherwise.
func (r resolver) waitError(ctx context.Context, host string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("mdns: lookup %q: %w", host, ctxErr)
	}
	// The socket deadline can fire a hair before ctx's own timer marks it done.
	if ctxDeadline, ok := ctx.Deadline(); ok && !time.Now().Before(ctxDeadline) {
		return fmt.Errorf("mdns: lookup %q: %w", host, context.DeadlineExceeded)
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return fmt.Errorf("mdns: lookup %q: %w", host, errNoAnswer)
	}
	return fmt.Errorf("mdns: lookup %q: %w", host, err)
}

// fullyQualified returns host with exactly one trailing dot, refusing an empty name.
func fullyQualified(host string) (string, error) {
	name := strings.TrimSuffix(host, ".")
	if name == "" {
		return "", fmt.Errorf("mdns: empty host name %q", host)
	}
	return name + ".", nil
}

// buildQuery encodes one message asking for fqdn's A and AAAA records, class IN.
func buildQuery(fqdn string) ([]byte, error) {
	name, err := dnsmessage.NewName(fqdn)
	if err != nil {
		return nil, err
	}
	builder := dnsmessage.NewBuilder(make([]byte, 0, 64), dnsmessage.Header{ID: uint16(rand.Uint32())})
	if err := builder.StartQuestions(); err != nil {
		return nil, err
	}
	for _, qtype := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		question := dnsmessage.Question{Name: name, Type: qtype, Class: dnsmessage.ClassINET}
		if err := builder.Question(question); err != nil {
			return nil, err
		}
	}
	return builder.Finish()
}

// parseReply returns the A and AAAA addresses packet carries for fqdn, matched
// case-insensitively, from its answer and additional sections. A packet that is not a response,
// does not parse, or answers only for other names yields none; parsing stops at the first
// malformed record and keeps what came before it.
func parseReply(packet []byte, fqdn string) []netip.Addr {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil || !header.Response {
		return nil
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return nil
	}
	addrs := collectAddrs(&parser, fqdn, answerSection, nil)
	if err := parser.SkipAllAuthorities(); err != nil {
		return addrs
	}
	return collectAddrs(&parser, fqdn, additionalSection, addrs)
}

// section names one resource section's header reader and skipper; the parser refuses to
// skip a record outside the section it is in, so each section brings its own pair.
type section struct {
	next func(*dnsmessage.Parser) (dnsmessage.ResourceHeader, error)
	skip func(*dnsmessage.Parser) error
}

// answerSection and additionalSection are the two sections a reply's addresses may ride in.
var (
	answerSection     = section{next: (*dnsmessage.Parser).AnswerHeader, skip: (*dnsmessage.Parser).SkipAnswer}
	additionalSection = section{next: (*dnsmessage.Parser).AdditionalHeader, skip: (*dnsmessage.Parser).SkipAdditional}
)

// collectAddrs walks one resource section, appending to addrs every A or AAAA
// record of class IN that names fqdn. It stops at the section's end or at the first record
// that does not parse.
func collectAddrs(
	parser *dnsmessage.Parser,
	fqdn string,
	sec section,
	addrs []netip.Addr,
) []netip.Addr {
	for {
		header, err := sec.next(parser)
		if err != nil {
			return addrs
		}
		matches := header.Class&classMask == dnsmessage.ClassINET &&
			strings.EqualFold(header.Name.String(), fqdn)
		switch {
		case matches && header.Type == dnsmessage.TypeA:
			record, err := parser.AResource()
			if err != nil {
				return addrs
			}
			addrs = append(addrs, netip.AddrFrom4(record.A))
		case matches && header.Type == dnsmessage.TypeAAAA:
			record, err := parser.AAAAResource()
			if err != nil {
				return addrs
			}
			addrs = append(addrs, netip.AddrFrom16(record.AAAA))
		default:
			if err := sec.skip(parser); err != nil {
				return addrs
			}
		}
	}
}
