package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// fireReadLoop runs read_loop's pre-request hook once against a request over msgs and reports
// whether it injected (Revision moved) and the injected hint text (empty when it did not fire).
func fireReadLoop(t *testing.T, msgs []domain.Message) (fired bool, hint string) {
	t.Helper()
	hook, ok := mustBuild(t, readLoopID).Hook.(domain.PreRequestHook)
	if !ok {
		t.Fatal("read_loop does not implement PreRequestHook")
	}
	req := shaperRequest(msgs, nil)
	before := req.Revision()
	if err := hook.PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		return false, ""
	}
	// The hint lands wherever InjectContext places it role-safely (a fresh system message when the
	// conversation ends in a tool result, else before the last user message), so scan every message
	// for the read-loop wording rather than assuming a role.
	for _, m := range req.State().Messages {
		for _, mk := range []string{"workspace is empty", "does not exist yet", "without making changes"} {
			if strings.Contains(m.Content, mk) {
				return true, m.Content
			}
		}
	}
	return true, ""
}

// A single failed read on an empty workspace fires the blunt greenfield hint (threshold 1): the file
// cannot exist because nothing has been created yet.
func TestReadLoopGreenfieldSingleMiss(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		userMsg("create app.go"),
		assistantCall(readCall("r1", "app.go")),
		toolResult("r1", "file not found: app.go"),
	}
	fired, hint := fireReadLoop(t, msgs)
	if !fired {
		t.Fatal("a single failed read on an empty workspace should fire the greenfield hint")
	}
	if !strings.Contains(hint, "workspace is empty") || !strings.Contains(hint, "app.go") {
		t.Errorf("greenfield hint = %q, want it to name the empty workspace and app.go", hint)
	}
}

// In a populated workspace (a prior successful read) a read loop needs TWO misses across turns
// before the normal hint fires — "check then create" is legitimate once.
func TestReadLoopNormalTwoMissesAcrossTurns(t *testing.T) {
	t.Parallel()
	oneMiss := []domain.Message{
		userMsg("edit config.go"),
		assistantCall(readCall("s1", "other.go")),
		toolResult("s1", "package other"),
		assistantCall(readCall("r1", "config.go")),
		toolResult("r1", "error: file not found"),
	}
	if fired, _ := fireReadLoop(t, oneMiss); fired {
		t.Fatal("a single miss in a populated workspace must not fire (threshold 2)")
	}

	twoMisses := append(oneMiss,
		assistantCall(readCall("r2", "config.go")),
		toolResult("r2", "error: file not found"),
	)
	fired, hint := fireReadLoop(t, twoMisses)
	if !fired {
		t.Fatal("two failed reads of the same file across turns should fire the read-loop hint")
	}
	if strings.Contains(hint, "workspace is empty") {
		t.Error("a populated workspace must not get the greenfield phrasing")
	}
	if !strings.Contains(hint, "config.go") {
		t.Errorf("normal hint = %q, want it to name config.go", hint)
	}
}

// The same file read three times WITHOUT a write fires the successful-read-loop branch.
func TestReadLoopSuccessfulReReads(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		userMsg("analyze a.go"),
		assistantCall(readCall("r1", "a.go")), toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")), toolResult("r2", "package a"),
		assistantCall(readCall("r3", "a.go")), toolResult("r3", "package a"),
	}
	fired, hint := fireReadLoop(t, msgs)
	if !fired {
		t.Fatal("three successful re-reads without a write should fire the successful-read-loop hint")
	}
	if !strings.Contains(hint, "without making changes") || !strings.Contains(hint, "a.go") {
		t.Errorf("successful-read-loop hint = %q, want the re-read wording naming a.go", hint)
	}
}

// The hint is idempotent: a second pre-request pass over a request already carrying it does not
// inject again (the hint is its own marker).
func TestReadLoopHintIsIdempotent(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		userMsg("create app.go"),
		assistantCall(readCall("r1", "app.go")),
		toolResult("r1", "file not found: app.go"),
	}
	hook := mustBuild(t, readLoopID).Hook.(domain.PreRequestHook)
	req := shaperRequest(msgs, nil)
	if err := hook.PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest #1: %v", err)
	}
	afterFirst := req.Revision()
	if afterFirst == 0 {
		t.Fatal("first pass should have injected")
	}
	if err := hook.PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest #2: %v", err)
	}
	if req.Revision() != afterFirst {
		t.Error("second pass re-injected the hint; it must be idempotent")
	}
}

// With no read loop present, read_loop is inert (a lone successful read is not a loop).
func TestReadLoopInertWithoutLoop(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		userMsg("read a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
	}
	if fired, _ := fireReadLoop(t, msgs); fired {
		t.Error("a single successful read is not a loop; read_loop must be inert")
	}
}

// listCall is a directory-listing call in one of the list-family spellings — the only tool class
// greenfield detection reads through listResultEmpty.
func listCall(id, tool, path string) domain.ToolCall {
	return domaintest.Call(id, tool, map[string]string{"path": path})
}

// greenfieldAfterListing reports how the greenfield scan reads a conversation whose single listing
// came back as content (or, when omitted, has no result yet).
func greenfieldAfterListing(tool, content string, omit bool) bool {
	msgs := []domain.Message{
		userMsg("create app.go"),
		assistantCall(listCall("l1", tool, ".")),
	}
	if !omit {
		msgs = append(msgs, toolResult("l1", content))
	}
	return isGreenfieldContext(scanView(msgs))
}

// listResultEmpty is the only gate on greenfield detection's list arm. A listing that came back
// empty — in any of the four shapes tools spell "nothing here", case- and padding-insensitively —
// leaves the workspace looking greenfield; a listing with entries flips it. A listing with no
// result yet counts as non-empty: missing the greenfield signal is the cheaper mistake than
// mis-firing the blunt hint on a call still in flight.
func TestGreenfieldListingGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		omit    bool // build the call with no paired result at all
		want    bool
	}{
		{name: "no content at all", want: true},
		{name: "empty json array", content: "[]", want: true},
		{name: "padded empty json array", content: "  []\n", want: true},
		{name: "empty-directory wording", content: "`.` is an empty directory", want: true},
		{name: "upper-cased no-files wording", content: "NO FILES MATCH THE PATTERN", want: true},
		{name: "a listing with entries", content: "app.go\nmain.go", want: false},
		{name: "a listing still in flight", omit: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := greenfieldAfterListing("list_dir", tc.content, tc.omit); got != tc.want {
				t.Errorf("greenfield = %v, want %v for a listing result %q (omitted: %v)", got, tc.want, tc.content, tc.omit)
			}
		})
	}

	// The gate reads the whole list-spelling family, not just the snake_case spelling above: a
	// camelCase menu must reach the same verdict, or greenfield detection silently ignores the
	// listing and calls a populated workspace empty.
	t.Run("every list spelling gates the same", func(t *testing.T) {
		t.Parallel()
		for _, spelling := range listSpellings {
			if !greenfieldAfterListing(spelling, "[]", false) {
				t.Errorf("%s: an empty listing should keep the workspace greenfield", spelling)
			}
			if greenfieldAfterListing(spelling, "app.go\nmain.go", false) {
				t.Errorf("%s: a listing with entries should flip greenfield off", spelling)
			}
		}
	})
}

// What the gate decides, the model sees: an empty listing keeps the workspace greenfield, so ONE
// failed read is enough for the blunt greenfield hint (threshold 1); a listing with entries proves
// the workspace exists, so the same single miss is legitimate "check then create" and stays silent
// until a second miss (threshold 2).
func TestReadLoopGreenfieldHintFollowsTheListingGate(t *testing.T) {
	t.Parallel()
	history := func(listing string) []domain.Message {
		return []domain.Message{
			userMsg("create app.go"),
			assistantCall(listCall("l1", "list_dir", ".")),
			toolResult("l1", listing),
			assistantCall(readCall("r1", "app.go")),
			toolResult("r1", "file not found: app.go"),
		}
	}

	fired, hint := fireReadLoop(t, history("[]"))
	if !fired {
		t.Fatal("an empty listing plus one failed read should fire the greenfield hint")
	}
	if !strings.Contains(hint, "workspace is empty") {
		t.Errorf("greenfield hint = %q, want the empty-workspace phrasing", hint)
	}

	if fired, hint := fireReadLoop(t, history("app.go\nmain.go")); fired {
		t.Errorf("a listing with entries must flip greenfield off, leaving one miss below threshold: %q", hint)
	}
}
