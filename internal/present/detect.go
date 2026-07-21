package present

import (
	"net"
	"strings"
)

// Kind names where the session the user is sitting in actually runs: on their own machine
// (Local) or on a box they reached over SSH (Remote). It is the first fact the presentation
// ladder needs, because rung 1 — auto-opening the document — is only ever right on a Local
// session: an opener fired on a remote box either fails or, worse, half-succeeds into a
// display nobody is looking at (ADR 0019).
type Kind string

const (
	// Local: the session runs on the user's own machine, so an opener reaches the display
	// the user is looking at.
	Local Kind = "local"
	// Remote: the session was reached over SSH, so the user's eyes are on another machine
	// and the document has to travel to them (rung 2) or stay as text (rung 0).
	Remote Kind = "remote"
)

// sshEnvVars are the variables OpenSSH sets in a remote session, any one of which is enough
// to call the session Remote. All three are consulted rather than just SSH_CONNECTION
// because they are set by different paths: SSH_TTY only for an interactive pty, SSH_CLIENT
// by older sshd builds, SSH_CONNECTION by the modern one. Over-detecting Remote is the safe
// direction — the cost is a document shown rather than opened, which is rung 0 and never
// wrong; under-detecting fires an opener into nowhere.
var sshEnvVars = []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"}

// desktopEnvVars are the Linux display-server handles: a session that has neither is
// headless (a devbox VM, a container, a CI runner) and has no desktop to open into.
var desktopEnvVars = []string{"DISPLAY", "WAYLAND_DISPLAY"}

// probeTarget is the destination of the outbound-dial probe in outboundIP. It is a
// documentation-range address (RFC 5737 TEST-NET-3) on the discard port, deliberately: the
// probe is a UDP "connect", which only makes the kernel pick the route and bind a local
// address — NO PACKETS ARE SENT — so the target must never be a host that could answer, and
// a reserved address guarantees it is not one.
const probeTarget = "203.0.113.1:9"

// loopbackHost is the last link of the AdvertiseHost chain. It is very often useless (a URL
// only the remote box itself can open), but it is a valid, honest address rather than an
// empty string that would compose a malformed URL — and the baseline rung has already put
// the path in front of the user regardless.
const loopbackHost = "127.0.0.1"

// Locality reports whether the session is Local or Remote, reading the environment through
// the injected env so the answer can be table-tested off any machine (pass os.Getenv in
// production). The session is Remote iff any of SSH_CONNECTION / SSH_TTY / SSH_CLIENT is
// set to a non-blank value.
//
// This is a heuristic about where the user's EYES are, not an authorization decision:
// getting it wrong costs a rung, never safety. A nil env reads as an empty environment.
func Locality(env func(string) string) Kind {
	for _, name := range sshEnvVars {
		if lookup(env, name) != "" {
			return Remote
		}
	}
	return Local
}

// HasDesktop reports whether goos, in the environment env, has a desktop session an opener
// could hand a document to. macOS and Windows always do (their GUI is not optional and not
// separately advertised); Linux does iff DISPLAY or WAYLAND_DISPLAY is set; every other OS
// answers false, which degrades to the baseline rather than guessing.
//
// It is deliberately independent of Locality: a *local* Linux box can be headless and a
// *remote* one can have a desktop nobody is watching, so the ladder gates rung 1 on both
// facts (Local AND HasDesktop) and this function answers only its own half. Callers inject
// goos (runtime.GOOS in production) and env (os.Getenv) so both halves are testable; a nil
// env reads as an empty environment.
func HasDesktop(goos string, env func(string) string) bool {
	switch goos {
	case "darwin", "windows":
		return true
	case "linux":
		for _, name := range desktopEnvVars {
			if lookup(env, name) != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// AdvertiseHost returns the host part of the URL the doc server advertises — the address at
// which the USER'S machine, not this one, can reach a served document (ADR 0019). The
// result is already in URL-authority form: an IPv6 literal comes back bracketed.
//
// The chain, highest confidence first:
//
//  1. the server IP from SSH_CONNECTION (its third field) — the address the user's SSH
//     client actually reached this box on, so it is routable from their machine BY
//     CONSTRUCTION and cannot go stale the way a written-down address can;
//  2. override — the present.host config value, for the remote topologies SSH_CONNECTION
//     cannot describe (a container, a VM reached some other way, a NATted address the user
//     must be pointed at by name);
//  3. the outbound-dial probe (outboundIP) — this box's own routable address, the best
//     guess available when nobody has said anything;
//  4. loopbackHost.
//
// NOTE (2026-07-21): SSH_CONNECTION is consulted BEFORE the override deliberately — the
// settled design of ADR 0019 fixes that order, and it is the reason the config key is a
// *fallback for the cases SSH cannot answer* rather than a true override of a live,
// verified-routable address. Do not "fix" this into override-first without reopening the ADR.
//
// A nil env reads as an empty environment. This is display plumbing, never a security
// decision: an address that turns out to be unreachable costs the user a link, and the
// baseline rung still carries the path.
func AdvertiseHost(env func(string) string, override string) string {
	return advertiseHost(env, override, outboundIP)
}

// advertiseHost is AdvertiseHost with its one un-injected source — the network probe — taken
// as a parameter, so the precedence chain can be table-tested without depending on the
// routing table of the machine running the tests.
func advertiseHost(env func(string) string, override string, dial func() string) string {
	if ip := sshServerIP(env); ip != "" {
		return hostForURL(ip)
	}
	if host := strings.TrimSpace(override); host != "" {
		return hostForURL(host)
	}
	if ip := dial(); ip != "" {
		return hostForURL(ip)
	}
	return loopbackHost
}

// sshServerIP returns the server-side address of the current SSH session, or "" when there
// is none to be had. SSH_CONNECTION is written by sshd as four space-separated fields —
// "<client ip> <client port> <server ip> <server port>" — and the third is this box as the
// client reached it.
//
// It is parsed defensively: too few fields, or a third field that is not a numeric IP, means
// the variable is not what we assume (a shell that rewrote it, a session manager that faked
// it) and the caller falls through to the next link rather than advertising garbage in a
// URL. A zoned link-local (fe80::1%eth0) fails that check too, which is the right outcome:
// a scoped address is meaningless on the machine we are advertising it to.
func sshServerIP(env func(string) string) string {
	fields := strings.Fields(lookup(env, "SSH_CONNECTION"))
	if len(fields) < 3 {
		return ""
	}
	if net.ParseIP(fields[2]) == nil {
		return ""
	}
	return fields[2]
}

// outboundIP returns this machine's routable local address by asking the kernel which one it
// would use to reach a far-off destination. The UDP "dial" is connectionless: it binds a
// local address and sends NOTHING (see probeTarget), so the probe costs no traffic, needs no
// reachable peer, and cannot hang on a firewall.
//
// It returns "" when there is no route at all (an offline or network-namespaced box) or when
// the kernel hands back an unspecified address — both are "no answer", not an error, because
// the caller has a further fallback and the baseline rung is unaffected either way.
func outboundIP() string {
	conn, err := net.Dial("udp", probeTarget)
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil || addr.IP.IsUnspecified() {
		return ""
	}
	return addr.IP.String()
}

// hostForURL puts host into the form a URL authority needs: an IPv6 literal must be
// bracketed ("[2001:db8::2]") or the colons read as a port separator. A colon is the tell —
// no hostname and no IPv4 address contains one — and an already-bracketed value is left
// alone so the function is idempotent over its own output.
func hostForURL(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// lookup reads name through the injected env, treating a nil env as an empty environment and
// trimming the result. Trimming matters because these variables are compared against "" to
// decide a rung: a variable set to whitespace (an sshd config quirk, a shell rc that exported
// an empty-looking value) must read as unset rather than as evidence of a remote session.
func lookup(env func(string) string, name string) string {
	if env == nil {
		return ""
	}
	return strings.TrimSpace(env(name))
}
