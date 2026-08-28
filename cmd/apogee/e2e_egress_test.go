package main

// T-18 of the v0.17.1 release checklist — "network egress: the operator's proxy, url-safety live,
// MCP over http, and a stream nothing deadlines" — as a test, for the half a human had to read a
// squid log for.
//
// It was manual because the claims are about traffic rather than about a screen: whether apogee
// CONNECTED TO THE PROXY rather than to the destination, whether a refused URL left the process at
// all, whether a configured MCP endpoint goes the same way the network tools do. `internal/tuitest`
// grew the instruments for exactly that (netfix.go): a real forward proxy with an access log, a
// loopback page with a hit counter, and a streamable-http MCP server with one tool.
//
// Two facts shape the whole file and are worth stating before the code makes them look arbitrary.
//
// It is PTY-DRIVEN, unlike most e2e tests here, and t.Setenv would be wrong even where it compiles:
// net/http.ProxyFromEnvironment reads the environment ONCE per process (envProxyOnce) and every
// other test in this binary has already made requests through it, so HTTP_PROXY set inside the test
// process would be silently ignored and the test would pass without apogee ever proxying anything.
// A variable of that class reaches a program only by being in its environment before it starts,
// which means a child, which means the black-box driver.
//
// And the destinations are PUBLIC-BUT-UNROUTABLE literals (240.0.0.0/4, reserved and allocated to
// nobody) rather than loopback. The SSRF floor refuses a private destination in the pre-flight,
// before the proxy question is asked at all, and the composition offers no seam to relax it — so a
// loopback destination could never demonstrate a proxy being used. The in-test proxy holds a route
// table that resolves each literal to a loopback server instead: apogee makes a genuinely public
// request, and nothing leaves the machine.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The three destinations this item reaches for. None is routable, all are outside every range the
// SSRF floor denies (`floorDeniedV4Nets`, internal/security/ssrf.go), and each has a route in the
// in-test proxy — except the redirect target, which has a route precisely so that a follow WOULD be
// visible and its absence is therefore evidence.
const (
	pageHost     = "240.0.0.1"
	mcpHost      = "240.0.0.2"
	redirectHost = "240.0.0.3"
)

// pageBody is what the page server answers with. It is a distinctive token rather than prose so the
// fixture's follow-up turn keys on the page actually having come back, and so the session record
// can be asked whether the BODY reached the model rather than only the status line.
const pageBody = "APOGEE-EGRESS-PAGE: the release notes."

// egressMCPServers is the `mcp-servers:` block the run starts with: one streamable-http server at a
// public destination the proxy carries. The endpoint has no port, so the transport dials port 80 and
// the proxy's route table decides what that means.
const egressMCPServers = "mcp-servers:\n" +
	"    - name: docs\n" +
	"      transport: streamable-http\n" +
	"      endpoint: http://" + mcpHost + "/mcp\n"

// TestE2EEgress is T-18 against the shipped binary: the operator's proxy is used for a public
// destination and never for loopback, the SSRF floor refuses a private one without anything leaving
// the process, an `mcp-servers:` endpoint travels the same road, the host allow/deny lists are read
// LIVE by both surfaces, and a redirect from the upstream is reported rather than followed.
func TestE2EEgress(t *testing.T) {
	page := tuitest.PageServer(t, pageBody)
	redirectTarget := tuitest.PageServer(t, "the redirect was followed")
	mcpServer := tuitest.MCPEcho(t)
	proxy := tuitest.ForwardProxy(t, map[string]string{
		pageHost:     page.Addr(),
		mcpHost:      mcpServer.Addr(),
		redirectHost: redirectTarget.Addr(),
	})

	stub := stubllm.New(t, loadScript(t, "egress"))
	sess := launchPTYWithEnv(t, stub, egressMCPServers, []string{
		"HTTP_PROXY=" + proxy.URL(),
		"HTTPS_PROXY=" + proxy.URL(),
		// The stub upstream is on loopback and so is the proxy itself. Naming them keeps the
		// operator's own exclusion in the picture even though Go's resolver bypasses a loopback
		// destination on its own — what the assertions below read is that no loopback traffic, the
		// model conversation included, ever went through the proxy.
		"NO_PROXY=127.0.0.1,localhost",
	})
	drv := sess.drv
	drv.WaitText("Send a message")

	// Step 1 — the MCP connect already happened, at startup, and it went through the proxy. This is
	// asserted before any prompt because a connect apogee makes for itself is the one piece of
	// egress no tool call can be blamed for.
	if got := proxy.Saw(mcpHost); got == 0 {
		t.Fatalf("the MCP endpoint's connect did not go through the proxy (log: %v)", proxy.Log())
	}

	// Step 2 — a fetch of a public page. The body is what proves the proxy FORWARDED rather than
	// merely accepted: the page is only reachable through the route table.
	submit(drv, "Fetch the release notes.")
	allowAndAwait(drv, "The notes are fetched.")

	if got := proxy.Saw(pageHost); got != 1 {
		t.Errorf("the proxy saw %d request(s) for %s; want exactly 1 (log: %v)", got, pageHost, proxy.Log())
	}
	if got := page.Hits(); got != 1 {
		t.Errorf("the page server answered %d request(s); want exactly 1", got)
	}
	if record := sessionRecordText(t, sess.Home()); !strings.Contains(record, pageBody) {
		t.Error("the fetched page body never reached the model; a proxy that accepted the request " +
			"but did not forward it would look like this")
	}

	// Step 3 — a loopback destination. The SSRF floor refuses it in the pre-flight, so the refusal
	// is not "the proxy said no": NOTHING was dialled, and the proxy's log is where that is read.
	before := len(proxy.Log())
	submit(drv, "Fetch the discard port.")
	allowAndAwait(drv, "The discard port was refused.")

	if got := proxy.Saw("127.0.0.1"); got != 0 {
		t.Errorf("%d loopback request(s) went through the proxy; a floor-refused URL must leave "+
			"the process not at all, and the model conversation is excluded (log: %v)", got, proxy.Log())
	}
	if got := len(proxy.Log()); got != before {
		t.Errorf("the proxy log grew from %d to %d entries over a refused fetch: %v", before, got, proxy.Log())
	}
	record := sessionRecordText(t, sess.Home())
	if !strings.Contains(record, "url blocked by url-safety (host 127.0.0.1)") {
		t.Error("the loopback fetch was not refused by url-safety; the SSRF floor is what makes " +
			"the operator's proxy a route to the internet rather than into the machine")
	}

	// Step 4 — the MCP server's own tool. Its name carries the entry's alias, which is how a call
	// that reached the configured server is told from one that reached anything else.
	submit(drv, "Echo through the docs server.")
	allowAndAwait(drv, "The docs server answered.")
	if record := sessionRecordText(t, sess.Home()); !strings.Contains(record, "echo: ping") {
		t.Error("docs__echo did not answer; a configured MCP endpoint reached through the proxy " +
			"must surface its tools like any other")
	}

	// Step 5 — the host deny list, landed on a session that is already running. The file is saved
	// under the run rather than typed into `/settings`, which is the same live path: the watcher
	// re-reads, the key applies, and the session says which keys moved.
	appendHomeConfig(t, sess.Home(), "url-safety:\n  deny-hosts: ["+pageHost+", "+mcpHost+"]\n")
	drv.WaitText("applied: url-safety.deny-hosts")
	drv.WaitQuiet(settled)

	proxied := len(proxy.Log())
	submit(drv, "Fetch the release notes once more.")
	allowAndAwait(drv, "The page host is refused now.")
	if got := len(proxy.Log()); got != proxied {
		t.Errorf("the proxy log grew from %d to %d entries over a denied fetch: %v", proxied, got, proxy.Log())
	}
	if record := sessionRecordText(t, sess.Home()); !strings.Contains(record, "is denied") {
		t.Error("the fetch was not refused by the deny list the run had just been given; the " +
			"network tools re-read the host lists live or the row is decoration")
	}

	// Step 6 — the same live list, read by the OTHER surface. The deny list of step 5 names the MCP
	// endpoint too, so that one edit did not only re-point the network tools: it re-admitted the
	// configured servers under the new lists and dropped the one they close. `docs__echo` is gone
	// from a session that was calling it a moment ago, and the model's ask for it comes back as an
	// unknown tool. Before this, the two surfaces disagreed until something else happened to dial.
	submit(drv, "Echo through the docs server once more.")
	drv.WaitText("Nothing else to reach for.")
	awaitRefusedCalls(t, drv, sess, 1)
	if record := sessionRecordText(t, sess.Home()); strings.Contains(record, "echo: still here") {
		t.Error("docs__echo answered after the operator denied its endpoint; the MCP connection and " +
			"the network tools must not disagree about which hosts are closed")
	}

	// And the reconnect an `mcp-servers:` edit drives is refused for the reason it always was — the
	// endpoint is still on the deny list — so renaming the server brings nothing back: no
	// `manuals__echo`, and the same unknown tool for the ask that follows.
	//
	// ADR 0037 decision 7 ("a refused reconnect keeps the old connections") loses its observation
	// here, because after step 5 the previous set is empty. It stays unit-covered, by
	// TestMCPReconnectUsesTheLiveURLSafetyLists (the refusal is the live lists', not the network's)
	// and TestApplySettingMCPReconnectKeepsTheOldSessionsWhenTheDialFails (the old sessions stand).
	renameHomeConfig(t, sess.Home(), "name: docs", "name: manuals")
	drv.WaitText("applied: mcp-servers")
	drv.WaitQuiet(settled)

	submit(drv, "Echo through the docs server once more.")
	awaitRefusedCalls(t, drv, sess, 2)
	if record := sessionRecordText(t, sess.Home()); strings.Contains(record, "manuals__echo") {
		t.Error("the renamed server's tools reached the session; a reconnect whose endpoint the deny " +
			"list still closes brings nothing back")
	}

	// Step 7 — the upstream itself moves. A 308 is reported to the human, never followed: following
	// one would carry a vetted connection to an unvetted host. The target has a route through the
	// proxy, so a follow would show up twice, and neither showing is what settles it.
	submit(drv, "Ask the upstream that moved.")
	drv.WaitText("upstream HTTP 308")
	drv.WaitQuiet(settled)
	if got := proxy.Saw(redirectHost); got != 0 {
		t.Errorf("%d request(s) for the redirect target went through the proxy: %v", got, proxy.Log())
	}
	if got := redirectTarget.Hits(); got != 0 {
		t.Errorf("the redirect target answered %d request(s); a 3xx from the upstream is handed "+
			"back, not followed", got)
	}

	if code := drv.Quit(); code != 0 {
		t.Errorf("the run exited %d; want a clean quit", code)
	}
}

// TestE2EEgressDeniedMCPEndpointStopsTheLaunch is the half of T-18 that happens before there is a
// session to observe: an `mcp-servers:` endpoint on the operator's own deny list is refused at
// STARTUP, and the refusal is fatal. Connecting the configured set is all-or-nothing
// (docs/design/mcp-client.md §3; ADR 0012's 2026-07-26 amendment), so a server the policy closes
// is not a session with one tool fewer — it is a launch that does not happen, and says why.
//
// The claim is read off the raw pty stream rather than off a frame: this failure is printed before
// the TUI exists, so it is a line of output and not a picture, and a hundred-column emulator would
// wrap it across rows that neither half is findable in.
func TestE2EEgressDeniedMCPEndpointStopsTheLaunch(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "egress"))
	sess := launchPTYConfigured(t, stub,
		egressMCPServers+"url-safety:\n  deny-hosts: ["+mcpHost+"]\n")
	drv := sess.drv

	const refusal = `server "docs" endpoint blocked by url-safety`
	drv.WaitFor(func() bool { return strings.Contains(string(drv.Bytes()), refusal) },
		tuitest.Awaiting("the launch to refuse the denied MCP endpoint"))

	select {
	case code := <-drv.Exited():
		if code == 0 {
			t.Error("apogee exited 0 after refusing a configured MCP endpoint; a set that cannot " +
				"be connected is a launch that fails, not a session with one tool fewer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("apogee kept running after refusing a configured MCP endpoint")
	}
}

// TestE2EEgressLongStreamIsNotDeadlined is the last of T-18: a reply that takes twenty-five seconds
// to arrive, arriving whole. The provider client deliberately sets no client-level Timeout —
// http.Client.Timeout bounds the whole response INCLUDING the body, so it would cut a stream at its
// own duration however healthy the stream is (internal/provider/client.go) — and a bound that is
// only absent in a comment is a bound somebody re-adds.
//
// It is in-process: nothing here is about a proxy, a real terminal or a real process, and the stub
// is on loopback. It is also the single most expensive test in the package, which is why it stands
// on its own rather than inside [TestE2EEgress] — see the budget in docs/design/test-drivers.md.
func TestE2EEgressLongStreamIsNotDeadlined(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "egress"))
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUI(t, drv, stub)
	drv.WaitText("Send a message")

	submit(drv, "Answer slowly, one rune at a time.")
	// The wait is the assertion: the fixture paces twenty-six deltas a second apart, so the last
	// rune lands twenty-five seconds after the first, and a client-level deadline anywhere under
	// that would end the Turn with an error instead of a sentence.
	drv.WaitFor(func() bool { _, _, ok := drv.Frame().Find("The long answer completes."); return ok },
		tuitest.Within(45*time.Second), tuitest.Awaiting("the twenty-five-second reply to arrive whole"))
	drv.WaitQuiet(settled)

	frame := drv.Frame()
	for _, unwanted := range []string{"context deadline exceeded", "Client.Timeout"} {
		if _, _, ok := frame.Find(unwanted); ok {
			t.Errorf("the long stream ended with %q on screen; nothing may deadline a healthy stream", unwanted)
		}
	}
}

// allowAndAwait drives one tool call to its reply: it answers the approval pane if one comes up and
// then waits for done.
//
// Asking the SCREEN whether a pane is there — rather than assuming one either way — is what the
// network tools' own allowance grain requires. "Always allow this session" for a network tool is
// scoped to the URL it was granted on (the url-filter marker, internal/tools/network.go), so two
// fetches of two different pages ask twice while two fetches of the SAME page ask once, and a step
// that hard-coded either shape would fail on the other.
// awaitRefusedCalls waits until the session record carries want tool calls that found no such tool.
//
// The count is what makes the wait honest across step 6's two asks: the model's fallback reply is
// the same sentence both times and is already on the screen for the second one, so a frame wait
// would return before the turn it is meant to be about had even started. The refusals are counted
// rather than matched whole because the record is JSON and the tool name in "unknown tool %q" is
// escaped there; the only calls in this run that can miss the registry are the two this step makes.
func awaitRefusedCalls(t *testing.T, drv *tuitest.PTYDriver, sess *ptySession, want int) {
	t.Helper()
	drv.WaitFor(func() bool {
		return strings.Count(sessionRecordText(t, sess.Home()), "unknown tool") >= want
	}, tuitest.Awaiting(fmt.Sprintf("%d call(s) on the dropped server to be refused as unknown", want)))
}

func allowAndAwait(drv *tuitest.PTYDriver, done string) {
	const approvalRow = "Always allow this session"

	drv.WaitFor(func() bool {
		f := drv.Frame()
		if _, _, ok := f.Find(done); ok {
			return true
		}
		_, _, ok := f.Find(approvalRow)
		return ok
	}, tuitest.Awaiting("an approval pane, or "+done))

	if _, _, ok := drv.Frame().Find(approvalRow); ok {
		drv.WaitQuiet(settled)
		drv.Type("s")
	}
	drv.WaitText(done)
	drv.WaitQuiet(settled)
}

// renameHomeConfig rewrites one occurrence in a home's config.yaml, for the edits a test makes to a
// key that is already there — [appendHomeConfig] can only add, and a second `mcp-servers:` block
// would be a duplicate YAML key rather than a change of mind.
func renameHomeConfig(t *testing.T, home, old, replacement string) {
	t.Helper()

	path := filepath.Join(home, "config.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the run's config: %v", err)
	}
	if !strings.Contains(string(body), old) {
		t.Fatalf("the run's config does not carry %q, so replacing it would change nothing", old)
	}
	updated := strings.Replace(string(body), old, replacement, 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("write the run's config: %v", err)
	}
}
