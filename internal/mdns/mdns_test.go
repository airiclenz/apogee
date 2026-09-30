package mdns

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// cacheFlushIN is class IN with the mDNS cache-flush bit set, as real responders send it.
const cacheFlushIN = dnsmessage.Class(0x8001)

// record is one address record a fake responder puts in its answer section.
type record struct {
	name string
	addr netip.Addr
}

// replyFunc builds the packets a fake responder sends back for one received query.
type replyFunc func(t *testing.T, query []byte) [][]byte

// startResponder runs a loopback UDP responder that answers every query with reply's packets,
// and returns its address. The socket closes when the test ends.
func startResponder(t *testing.T, reply replyFunc) netip.AddrPort {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, maxPacketSize)
		for {
			n, from, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			for _, packet := range reply(t, slices.Clone(buf[:n])) {
				if _, err := conn.WriteToUDPAddrPort(packet, from); err != nil {
					return
				}
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).AddrPort()
}

// answering replies with one response carrying records in its answer section.
func answering(records ...record) replyFunc {
	return func(t *testing.T, _ []byte) [][]byte {
		return [][]byte{buildResponse(t, records)}
	}
}

// buildResponse encodes an mDNS response whose answer section holds records.
func buildResponse(t *testing.T, records []record) []byte {
	t.Helper()
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	if err := builder.StartAnswers(); err != nil {
		t.Fatalf("start answers: %v", err)
	}
	for _, rec := range records {
		header := dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(rec.name), Class: cacheFlushIN, TTL: 120}
		var err error
		if rec.addr.Is4() {
			err = builder.AResource(header, dnsmessage.AResource{A: rec.addr.As4()})
		} else {
			err = builder.AAAAResource(header, dnsmessage.AAAAResource{AAAA: rec.addr.As16()})
		}
		if err != nil {
			t.Fatalf("add record: %v", err)
		}
	}
	packet, err := builder.Finish()
	if err != nil {
		t.Fatalf("finish response: %v", err)
	}
	return packet
}

func TestLookupReturnsAnnouncedAddresses(t *testing.T) {
	t.Parallel()
	v4 := netip.MustParseAddr("192.168.1.42")
	v6 := netip.MustParseAddr("fe80::1")
	tests := []struct {
		name  string
		host  string
		reply replyFunc
		want  []netip.Addr
	}{
		{
			name:  "A answer",
			host:  "apollo-ii.local",
			reply: answering(record{"apollo-ii.local.", v4}),
			want:  []netip.Addr{v4},
		},
		{
			name:  "AAAA answer only",
			host:  "apollo-ii.local",
			reply: answering(record{"apollo-ii.local.", v6}),
			want:  []netip.Addr{v6},
		},
		{
			name:  "A and AAAA answers",
			host:  "apollo-ii.local.",
			reply: answering(record{"apollo-ii.local.", v4}, record{"apollo-ii.local.", v6}),
			want:  []netip.Addr{v4, v6},
		},
		{
			name:  "name matched case-insensitively",
			host:  "Apollo-II.local",
			reply: answering(record{"APOLLO-ii.LOCAL.", v4}),
			want:  []netip.Addr{v4},
		},
		{
			name: "malformed packet then valid answer",
			host: "apollo-ii.local",
			reply: func(t *testing.T, _ []byte) [][]byte {
				valid := buildResponse(t, []record{{"apollo-ii.local.", v4}})
				truncated := valid[:len(valid)-3]
				return [][]byte{{0xff, 0x00, 0x13}, truncated, valid}
			},
			want: []netip.Addr{v4},
		},
		{
			name: "other name's answer skipped for the matching one",
			host: "apollo-ii.local",
			reply: func(t *testing.T, _ []byte) [][]byte {
				return [][]byte{
					buildResponse(t, []record{{"gemini.local.", netip.MustParseAddr("10.0.0.9")}}),
					buildResponse(t, []record{{"gemini.local.", v6}, {"apollo-ii.local.", v4}}),
				}
			},
			want: []netip.Addr{v4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := resolver{dest: startResponder(t, tt.reply), timeout: 5 * time.Second}
			got, err := r.lookup(t.Context(), tt.host)
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("addresses = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLookupAsksForAAndAAAA(t *testing.T) {
	t.Parallel()
	questions := make(chan []dnsmessage.Question, 1)
	dest := startResponder(t, func(t *testing.T, query []byte) [][]byte {
		var msg dnsmessage.Message
		if err := msg.Unpack(query); err != nil {
			t.Errorf("unpack query: %v", err)
			return nil
		}
		questions <- msg.Questions
		return [][]byte{buildResponse(t, []record{{"apollo-ii.local.", netip.MustParseAddr("192.168.1.42")}})}
	})
	r := resolver{dest: dest, timeout: 5 * time.Second}
	if _, err := r.lookup(t.Context(), "Apollo-II.local"); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	got := <-questions
	want := []dnsmessage.Question{
		{Name: dnsmessage.MustNewName("Apollo-II.local."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		{Name: dnsmessage.MustNewName("Apollo-II.local."), Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("questions = %v, want %v", got, want)
	}
}

func TestLookupTimesOutWithoutMatchingAnswer(t *testing.T) {
	t.Parallel()
	dest := startResponder(t, answering(record{"gemini.local.", netip.MustParseAddr("10.0.0.9")}))
	r := resolver{dest: dest, timeout: 200 * time.Millisecond}
	got, err := r.lookup(t.Context(), "apollo-ii.local")
	if !errors.Is(err, errNoAnswer) {
		t.Fatalf("err = %v, want errNoAnswer", err)
	}
	if got != nil {
		t.Fatalf("addresses = %v, want none", got)
	}
}

func TestLookupReturnsPromptlyWhenContextCancelled(t *testing.T) {
	t.Parallel()
	silent := startResponder(t, func(*testing.T, []byte) [][]byte { return nil })
	r := resolver{dest: silent, timeout: time.Minute}
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := r.lookup(ctx, "apollo-ii.local")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("lookup took %v after cancel, want prompt return", elapsed)
	}
}

func TestLookupHonoursEarlierContextDeadline(t *testing.T) {
	t.Parallel()
	silent := startResponder(t, func(*testing.T, []byte) [][]byte { return nil })
	r := resolver{dest: silent, timeout: time.Minute}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := r.lookup(ctx, "apollo-ii.local")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestLookupRefusesEmptyHost(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"", "."} {
		if _, err := Lookup(t.Context(), host); err == nil {
			t.Fatalf("Lookup(%q) returned no error", host)
		}
	}
}
