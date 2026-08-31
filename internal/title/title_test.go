package title

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// promptDate is the fixed date every Prompt test builds against, so the rendered context line is
// deterministic.
var promptDate = time.Date(2026, 7, 31, 14, 30, 0, 0, time.UTC)

// userContent returns the naming request's user message, failing the test if the request is not
// the expected system+user pair.
func userContent(t *testing.T, prompts []string, workspace string) string {
	t.Helper()
	req := Prompt(prompts, workspace, promptDate, provider.EffortDialectNone)
	if len(req.Messages) != 2 {
		t.Fatalf("Prompt built %d messages, want 2 (system + user)", len(req.Messages))
	}
	if req.Messages[1].Role != "user" {
		t.Fatalf("second message role = %q, want user", req.Messages[1].Role)
	}
	return req.Messages[1].Content
}

// listedEntry is one numbered request parsed back out of a rendered window.
type listedEntry struct {
	position int
	body     string
}

// listedEntries parses the numbered lines out of a rendered user message, in render order, so the
// window tests can assert on the selection without re-implementing the renderer. Non-numbered
// lines — the context labels, the header, the elision marker, the closing instruction — carry no
// "<digits>. " prefix and are skipped.
func listedEntries(t *testing.T, user string) []listedEntry {
	t.Helper()
	var entries []listedEntry
	for _, line := range strings.Split(user, "\n") {
		number, body, found := strings.Cut(line, ". ")
		if !found {
			continue
		}
		position, err := strconv.Atoi(number)
		if err != nil {
			continue
		}
		entries = append(entries, listedEntry{position: position, body: body})
	}
	return entries
}

// listedPositions returns just the 1-based positions a rendered window numbers.
func listedPositions(t *testing.T, user string) []int {
	t.Helper()
	positions := make([]int, 0, len(user))
	for _, entry := range listedEntries(t, user) {
		positions = append(positions, entry.position)
	}
	return positions
}

// markerCount counts the elision marker lines in a rendered window.
func markerCount(user string) int {
	count := 0
	for _, line := range strings.Split(user, "\n") {
		if strings.HasPrefix(line, elisionOpen) {
			count++
		}
	}
	return count
}

// window builds n requests, each opening with its own 1-based sentinel so a test can tell which
// of them survived the selection, and each padded with body so the size is under the test's
// control.
func window(n int, body string) []string {
	requests := make([]string, n)
	for i := range requests {
		requests[i] = fmt.Sprintf("request-%03d %s", i+1, body)
	}
	return requests
}

func TestPromptCarriesWorkspaceDateAndInstruction(t *testing.T) {
	t.Parallel()

	req := Prompt([]string{"add a retry to the uploader"}, "apogee", promptDate, provider.EffortDialectNone)

	if req.Messages[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", req.Messages[0].Role)
	}
	if !strings.Contains(req.Messages[0].Content, "3 to 8 words") {
		t.Errorf("system prompt does not state the length instruction: %q", req.Messages[0].Content)
	}

	user := req.Messages[1].Content
	for _, want := range []string{"apogee", "2026-07-31", "add a retry to the uploader", userInstruction} {
		if !strings.Contains(user, want) {
			t.Errorf("user message missing %q:\n%s", want, user)
		}
	}
}

func TestPromptLeavesModelToTheClient(t *testing.T) {
	t.Parallel()

	req := Prompt([]string{"hello"}, "apogee", promptDate, provider.EffortDialectNone)
	if req.Model != "" {
		t.Errorf("Model = %q, want empty so the Client's current binding wins", req.Model)
	}
	if req.Stream {
		t.Error("Stream = true, want a single non-streaming round-trip")
	}
	if len(req.Tools) != 0 {
		t.Errorf("Tools = %d, want none on a naming call", len(req.Tools))
	}
}

// TestPromptSetsSamplingConstants pins the naming call's sampling on BOTH prompt forms: the
// single request the automatic call sends and the multi-request window a bare /rename sends. The
// cap is asserted against the literal 4096 rather than titleMaxTokens so lowering the constant
// back to a value a thinking model exhausts fails here instead of passing silently.
func TestPromptSetsSamplingConstants(t *testing.T) {
	t.Parallel()

	forms := map[string][]string{
		"single request": {"hello"},
		"window":         {"hello", "now add a retry", "and write the test"},
	}
	for name, prompts := range forms {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := Prompt(prompts, "apogee", promptDate, provider.EffortDialectNone)
			if req.Sampling.Temperature == nil || *req.Sampling.Temperature != titleTemperature {
				t.Errorf("Temperature = %v, want %v", req.Sampling.Temperature, titleTemperature)
			}
			if req.Sampling.MaxTokens == nil {
				t.Fatal("MaxTokens is unset, want the backstop cap for a server that ignores the thinking switch")
			}
			if *req.Sampling.MaxTokens != 4096 {
				t.Errorf("MaxTokens = %d, want 4096", *req.Sampling.MaxTokens)
			}
			if req.ThinkingEffort != provider.EffortOff {
				t.Errorf("ThinkingEffort = %q, want %q — the naming call asks for no reasoning pass",
					req.ThinkingEffort, provider.EffortOff)
			}
		})
	}
}

// The naming call's "answer without thinking" ask only reaches the server if it is spelled in the
// dialect that server reads (ADR 0060), so Prompt states the dialect it is handed and states it
// beside the unchanged intent. Which bytes each dialect becomes is provider.Client's business —
// pinned there — so all this asks is that the caller's dialect survives the trip verbatim, the zero
// included: the un-dialected request is the historical chat_template_kwargs shape.
func TestPromptCarriesTheDialectItIsHanded(t *testing.T) {
	t.Parallel()

	for name, dialect := range map[string]provider.EffortDialect{
		"the zero dialect": provider.EffortDialectNone,
		"kwargs":           provider.EffortDialectKwargs,
		"reasoning":        provider.EffortDialectReasoning,
		"openai":           provider.EffortDialectOpenAI,
		"off":              provider.EffortDialectOff,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := Prompt([]string{"the parser test fails every other run"}, "apogee", promptDate, dialect)

			if req.EffortDialect != dialect {
				t.Errorf("EffortDialect = %q, want %q — the caller's dialect did not reach the request",
					req.EffortDialect, dialect)
			}
			if req.ThinkingEffort != provider.EffortOff {
				t.Errorf("ThinkingEffort = %q, want %q — the dialect names the shape, never the ask",
					req.ThinkingEffort, provider.EffortOff)
			}
		})
	}
}

func TestPromptCapsTheExcerpt(t *testing.T) {
	t.Parallel()

	// "z" appears nowhere in the message template, so counting it counts excerpt body only.
	long := strings.Repeat("z", promptExcerptRunes*3) + " SENTINEL_TAIL"
	user := userContent(t, []string{long}, "apogee")

	if strings.Contains(user, "SENTINEL_TAIL") {
		t.Error("user message carries the tail of an over-long prompt; the excerpt cap did not apply")
	}
	if got := strings.Count(user, "z"); got > promptExcerptRunes {
		t.Errorf("excerpt carried %d body runes, want at most %d", got, promptExcerptRunes)
	}
	if !strings.Contains(user, "…") {
		t.Error("a truncated excerpt should be marked with an ellipsis")
	}
}

func TestPromptKeepsAShortPromptWhole(t *testing.T) {
	t.Parallel()

	user := userContent(t, []string{"  fix the parser  "}, "apogee")
	if !strings.Contains(user, "fix the parser") {
		t.Errorf("short prompt not carried verbatim:\n%s", user)
	}
	if strings.Contains(user, "…") {
		t.Error("a prompt under the cap must not be marked as truncated")
	}
}

func TestPromptOneRequestRendersTheFirstRequestFormVerbatim(t *testing.T) {
	t.Parallel()

	// Pinned literally: the automatic call sends this message byte for byte, and widening the
	// seam to a window must not have moved a character of it.
	want := "Workspace: apogee\n" +
		"Date: 2026-07-31\n" +
		"\n" +
		"The user's first request:\n" +
		"fix the parser\n" +
		"\n" +
		"Reply with the title only."

	got := userContent(t, []string{"  fix the parser  "}, "apogee")

	if got != want {
		t.Errorf("one-request message =\n%q\nwant\n%q", got, want)
	}
}

func TestPromptDropsEmptyRequestsBeforeSelecting(t *testing.T) {
	t.Parallel()

	got := userContent(t, []string{"", "   ", "fix the parser", "\n\t "}, "apogee")

	want := userContent(t, []string{"fix the parser"}, "apogee")
	if got != want {
		t.Errorf("window of one real request =\n%q\nwant the one-request form\n%q", got, want)
	}
}

func TestPromptEmptyWindowRendersAnEmptyRequest(t *testing.T) {
	t.Parallel()

	// A contract, not a path anyone reaches: both callers refuse to name a session with nothing
	// in it, so this only pins what the package does if one ever stops.
	want := "Workspace: apogee\n" +
		"Date: 2026-07-31\n" +
		"\n" +
		"The user's first request:\n" +
		"\n" +
		"\n" +
		"Reply with the title only."

	for name, prompts := range map[string][]string{
		"nil":              nil,
		"empty slice":      {},
		"only empty texts": {"", "  \n\t "},
	} {
		if got := userContent(t, prompts, "apogee"); got != want {
			t.Errorf("%s window =\n%q\nwant\n%q", name, got, want)
		}
	}
}

func TestPromptWindowRendersEveryRequestWhenTheBudgetAllows(t *testing.T) {
	t.Parallel()

	user := userContent(t, window(10, "do the thing"), "apogee")

	if !strings.Contains(user, windowHeader) {
		t.Errorf("window message does not label the request block:\n%s", user)
	}
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := listedPositions(t, user); !slices.Equal(got, want) {
		t.Errorf("listed positions = %v, want %v:\n%s", got, want, user)
	}
	if got := markerCount(user); got != 0 {
		t.Errorf("elision markers = %d, want none when nothing was omitted:\n%s", got, user)
	}
	for _, want := range []string{"apogee", "2026-07-31", userInstruction} {
		if !strings.Contains(user, want) {
			t.Errorf("window message missing %q:\n%s", want, user)
		}
	}
}

// overflowingWindow is the fixture the budget tests share: enough requests, each over the
// per-entry cap, that the total budget cannot carry them all.
func overflowingWindow(t *testing.T) (requests []string, user string) {
	t.Helper()
	requests = window(30, strings.Repeat("z", windowEntryRunes*2))
	return requests, userContent(t, requests, "apogee")
}

func TestPromptWindowKeepsTheMandatorySetAndFillsNewestFirst(t *testing.T) {
	t.Parallel()

	requests, user := overflowingWindow(t)

	positions := listedPositions(t, user)
	if len(positions) >= len(requests) {
		t.Fatalf("nothing was omitted from %d over-long requests: %v", len(requests), positions)
	}
	for _, want := range []int{1, len(requests) - 2, len(requests) - 1, len(requests)} {
		if !slices.Contains(positions, want) {
			t.Errorf("request %d is mandatory but was omitted: %v", want, positions)
		}
	}
	// The fill runs newest-first and stops rather than skipping, so what survives is the opening
	// request plus one contiguous run reaching the newest.
	tail := positions[1:]
	for i, position := range tail {
		if want := tail[0] + i; position != want {
			t.Fatalf("included tail %v is not contiguous at index %d", positions, i+1)
		}
	}
	if last := tail[len(tail)-1]; last != len(requests) {
		t.Errorf("included run ends at %d, want the newest request %d", last, len(requests))
	}
}

func TestPromptWindowStaysWithinTheRuneBudget(t *testing.T) {
	t.Parallel()

	_, user := overflowingWindow(t)

	total := 0
	for _, entry := range listedEntries(t, user) {
		total += len([]rune(entry.body))
	}
	if total > windowBudgetRunes {
		t.Errorf("included excerpts total %d runes, want at most %d", total, windowBudgetRunes)
	}
	// Every candidate is capped at windowEntryRunes, so a fill that stopped early with room for
	// one more would mean the budget was not actually spent.
	if total+windowEntryRunes <= windowBudgetRunes {
		t.Errorf("included excerpts total only %d runes of a %d budget; the fill stopped short",
			total, windowBudgetRunes)
	}
}

func TestPromptWindowMarksTheOmittedRunExactlyOnce(t *testing.T) {
	t.Parallel()

	requests, user := overflowingWindow(t)

	if got := markerCount(user); got != 1 {
		t.Fatalf("elision markers = %d, want exactly 1:\n%s", got, user)
	}
	omitted := len(requests) - len(listedPositions(t, user))
	if want := elisionMarker(omitted); !strings.Contains(user, want) {
		t.Errorf("window does not carry %q:\n%s", want, user)
	}
}

func TestPromptWindowNumbersByOriginalPositionAcrossTheGap(t *testing.T) {
	t.Parallel()

	_, user := overflowingWindow(t)

	entries := listedEntries(t, user)
	for i, entry := range entries {
		if i > 0 && entry.position <= entries[i-1].position {
			t.Fatalf("positions %v are not rendered oldest first", listedPositions(t, user))
		}
		// Each request opens with its own original position, so a number that survived the
		// selection must still sit beside the request it belongs to.
		if want := fmt.Sprintf("request-%03d ", entry.position); !strings.HasPrefix(entry.body, want) {
			t.Errorf("entry numbered %d does not carry %q", entry.position, want)
		}
	}
	if entries[1].position == entries[0].position+1 {
		t.Fatalf("nothing was omitted, so the numbering never crosses a gap: %v",
			listedPositions(t, user))
	}
}

func TestPromptWindowCapsEachEntry(t *testing.T) {
	t.Parallel()

	// "Q" appears nowhere else in the message, so counting it counts one entry's body.
	huge := strings.Repeat("Q", windowEntryRunes*10)
	user := userContent(t, []string{"open the file", huge, "then", "and then", "finally"}, "apogee")

	if got := strings.Count(user, "Q"); got != windowEntryRunes {
		t.Errorf("the huge entry carried %d runes, want the per-entry cap of %d",
			got, windowEntryRunes)
	}
	for _, entry := range listedEntries(t, user) {
		if got := len([]rune(entry.body)); got > windowEntryRunes+1 {
			t.Errorf("entry %d is %d runes, want at most %d plus the ellipsis",
				entry.position, got, windowEntryRunes)
		}
	}
}

func TestPromptWindowCapsEntriesInRunesNotBytes(t *testing.T) {
	t.Parallel()

	// 600 CJK runes are 1800 bytes: a byte cap would keep a third of what a rune cap keeps.
	cjk := strings.Repeat("日", windowEntryRunes+200)
	user := userContent(t, []string{"open the file", cjk, "then", "and then", "finally"}, "apogee")

	if got := strings.Count(user, "日"); got != windowEntryRunes {
		t.Errorf("CJK entry carried %d runes, want %d", got, windowEntryRunes)
	}
}

func TestPromptWindowCollapsesAnEntryOntoOneLine(t *testing.T) {
	t.Parallel()

	requests := []string{"open the file", "fix this:\n\tpanic: boom\n\tat main.go:1", "and go on"}
	user := userContent(t, requests, "apogee")

	if got := listedPositions(t, user); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("listed positions = %v, want one line per request:\n%s", got, user)
	}
	if !strings.Contains(user, "2. fix this: panic: boom at main.go:1") {
		t.Errorf("a multi-line request did not collapse onto its numbered line:\n%s", user)
	}
}

func TestSelectWindowKeepsTheMandatorySetOverTheBudget(t *testing.T) {
	t.Parallel()

	excerpts := []string{"a", "b", "c", "d", "e", "f"}

	// A budget the mandatory set alone blows: it is exempt, so it rides anyway.
	got := selectWindow(excerpts, 1)

	if want := []int{0, 3, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("selectWindow over budget = %v, want the mandatory set %v", got, want)
	}
}

func TestSelectWindowStopsAtTheFirstEntryThatDoesNotFit(t *testing.T) {
	t.Parallel()

	// Entry 2 does not fit; entry 1 would. Skipping it for entry 1 would leave the omitted
	// entries in two runs, which the single elision marker cannot render.
	excerpts := []string{"a", "b", strings.Repeat("x", 10), "c", "d", "e"}

	got := selectWindow(excerpts, 8)

	if want := []int{0, 3, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("selectWindow = %v, want %v — the fill must stop, not skip", got, want)
	}
}

func TestSystemInstructionAsksForTheDominantThreadBiasedRecent(t *testing.T) {
	t.Parallel()

	// Asserted on the constant so the instruction cannot be silently reworded away: recency by
	// accident is not the same as recency by instruction.
	for _, want := range []string{
		"main thread",
		"dominant task rather than listing every request",
		"moved on to a different task, name the task it moved to",
		"3 to 8 words",
		"never the project, the folder, or the date",
	} {
		if !strings.Contains(systemInstruction, want) {
			t.Errorf("system instruction does not say %q:\n%s", want, systemInstruction)
		}
	}
}

// TestUserInstructionAndWindowHeaderPinTheirExactWording holds the two one-line prompt assets to
// their wording. Every other assertion that reads them — the user-message checks above — compares
// against the vars themselves, so it passes whatever the assets happen to say; only a literal pin
// notices a re-wording. Both lines are load-bearing: the closing instruction is what keeps the
// reply to one line, and the header is what lets the model read the last entries as the most recent
// work. The literals are therefore deliberately duplicated rather than derived, and an intended
// re-wording is meant to change the asset and this test in the same commit (prompts/README.md).
func TestUserInstructionAndWindowHeaderPinTheirExactWording(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		asset string
		got   string
		want  string
	}{
		{
			asset: "user-instruction.txt",
			got:   userInstruction,
			want:  "Reply with the title only.",
		},
		{
			asset: "window-header.txt",
			got:   windowHeader,
			want:  "The user's requests in this session, oldest first:",
		},
	} {
		if tc.got != tc.want {
			t.Errorf("prompts/%s changed:\n got  %q\n want %q\nan intended re-wording must update this pin",
				tc.asset, tc.got, tc.want)
		}
	}
}

func TestSanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{
			name: "plain title passes through",
			raw:  "Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "leading think block stripped",
			raw:  "<think>The user wants a retry.\nSo the title is about retries.</think>\nAdd retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "think block on one line stripped",
			raw:  "<think>hmm</think> Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "unterminated think block leaves nothing",
			raw:  "<think>still reasoning about the answer",
			ok:   false,
		},
		{
			name: "multiline reply takes the first non-empty line",
			raw:  "\n\nAdd retry to the uploader\nHere is why I chose that.",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "bare fence markers skipped as noise",
			raw:  "```\nAdd retry to the uploader\n```",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "language-tagged fence marker skipped as noise",
			raw:  "```text\nAdd retry to the uploader\n```",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "tilde fence marker skipped as noise",
			raw:  "~~~\nAdd retry to the uploader\n~~~",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "fence pair with nothing inside fails",
			raw:  "```\n```",
			ok:   false,
		},
		{
			name: "fence glued to prose keeps the prose",
			raw:  "```Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "ansi colour sequences stripped",
			raw:  "\x1b[1;32mAdd retry to the uploader\x1b[0m",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "osc sequence stripped",
			raw:  "\x1b]0;pwned\x07Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "bare control characters stripped",
			raw:  "Add\x07 retry\x00 to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "surrounding double quotes stripped",
			raw:  `"Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "surrounding backticks stripped",
			raw:  "`Add retry to the uploader`",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "curly quotes stripped",
			raw:  "“Add retry to the uploader”",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "unbalanced apostrophe kept",
			raw:  "Fix the user's uploader",
			want: "Fix the user's uploader",
			ok:   true,
		},
		{
			name: "title label stripped",
			raw:  "Title: Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "title label stripped case-insensitively",
			raw:  "TITLE:Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "quoted label stripped in either order",
			raw:  `"Title: Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "label then quotes stripped",
			raw:  `Title: "Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "comment marker and label stripped",
			raw:  "// title: Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "heading marker stripped",
			raw:  "# Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "multi-hash heading marker stripped",
			raw:  "### Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "trailing period trimmed",
			raw:  "Add retry to the uploader.",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "inner whitespace collapsed",
			raw:  "Add   retry\tto  the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "empty input fails",
			raw:  "",
			ok:   false,
		},
		{
			name: "whitespace-only input fails",
			raw:  "   \n\t \n ",
			ok:   false,
		},
		{
			name: "all-noise input fails",
			raw:  "<think>reasoning</think>\n```\n\"Title:\"\n```",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := Sanitize(tc.raw)
			if ok != tc.ok {
				t.Fatalf("Sanitize(%q) ok = %v, want %v (got %q)", tc.raw, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if !ok && got != "" {
				t.Errorf("Sanitize(%q) returned %q alongside ok=false, want an empty title", tc.raw, got)
			}
		})
	}
}

// A title is the one piece of model-authored text that is SAVED and comes back out onto a browsable
// list, so a bidirectional formatting character in the reply would reorder a session-browser row —
// and the stored title, read later by something else, would not say what the row said. IsControl is
// Cc only, so these survived the strip until this seam named them. The set stays narrow on purpose:
// the ZWJ and soft-hyphen cases below are what a blanket unicode.Cf drop would break, and a title is
// prose a person may legitimately have written.
func TestStripEscapesDropsBidiControls(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"RLO", "Add\u202e retry to the uploader", "Add retry to the uploader"},
		{"LRO", "Add\u202d retry to the uploader", "Add retry to the uploader"},
		{"embeddings and their pop", "Add\u202a\u202b\u202c retry", "Add retry"},
		{"isolates", "Add\u2066\u2067\u2068\u2069 retry", "Add retry"},
		{"the marks", "Add\u200e\u200f retry", "Add retry"},
		{"ZWJ survives", "Ship \U0001f469\u200d\U0001f4bb", "Ship \U0001f469\u200d\U0001f4bb"},
		{"soft hyphen survives", "In\u00adcremental parse", "In\u00adcremental parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := StripEscapes(tc.raw); got != tc.want {
				t.Errorf("StripEscapes(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			// Through the whole cleanup too, since that is how a reply really reaches a saved record.
			got, ok := Sanitize(tc.raw)
			if !ok {
				t.Fatalf("Sanitize(%q) reported failure", tc.raw)
			}
			if strings.ContainsFunc(got, sanitize.BidiControl) {
				t.Errorf("Sanitize(%q) = %q, which still carries a bidi control", tc.raw, got)
			}
		})
	}
}

func TestSanitizeTruncatesOnAWordBoundary(t *testing.T) {
	t.Parallel()

	// 11 words of 5 runes plus separators runs well past the 50-rune cap.
	raw := strings.TrimSpace(strings.Repeat("alpha ", 11))
	got, ok := Sanitize(raw)
	if !ok {
		t.Fatal("Sanitize reported failure on an over-long but valid title")
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated title %q does not end in an ellipsis", got)
	}
	if n := len([]rune(got)); n > MaxRunes+1 {
		t.Errorf("title is %d runes (%q), want at most %d plus the ellipsis", n, got, MaxRunes)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Errorf("truncated title %q keeps the boundary space", got)
	}
	if !strings.HasPrefix(raw, strings.TrimSuffix(got, "…")) {
		t.Errorf("truncated title %q is not a prefix of the input", got)
	}
}

func TestSanitizeHardCutsWhenThereIsNoLateWordBoundary(t *testing.T) {
	t.Parallel()

	// One long word: no boundary past the 60% floor, so the cut is hard at the rune cap.
	got, ok := Sanitize(strings.Repeat("x", 120))
	if !ok {
		t.Fatal("Sanitize reported failure on a single long word")
	}
	if want := strings.Repeat("x", MaxRunes) + "…"; got != want {
		t.Errorf("Sanitize hard cut = %q, want %q", got, want)
	}
}

func TestSanitizeCountsMultibyteRunesNotBytes(t *testing.T) {
	t.Parallel()

	// 60 CJK runes: 180 bytes, but only 60 runes, so the cap must bite at 50 runes.
	raw := strings.Repeat("日", 60)
	got, ok := Sanitize(raw)
	if !ok {
		t.Fatal("Sanitize reported failure on a CJK title")
	}
	if want := strings.Repeat("日", MaxRunes) + "…"; got != want {
		t.Errorf("Sanitize(CJK) = %q (%d runes), want %d runes plus the ellipsis",
			got, len([]rune(got)), MaxRunes)
	}
}

func TestSanitizeKeepsAMultibyteTitleUnderTheCapWhole(t *testing.T) {
	t.Parallel()

	raw := "修复上传器的重试逻辑"
	got, ok := Sanitize(raw)
	if !ok || got != raw {
		t.Errorf("Sanitize(%q) = %q, %v; want the title unchanged", raw, got, ok)
	}
}

// TestEmbeddedPromptsLoadWithoutTrailingNewline pins the loader contract behind the prompt
// assets: every file under prompts/ carries text, and mustPrompt returns it with the single
// trailing newline the file ends in already stripped. That is what keeps a loaded prompt
// byte-identical to the literal it replaced, and keeps the joiners the message assembly writes
// in code from being doubled by one hiding at the end of a file.
func TestEmbeddedPromptsLoadWithoutTrailingNewline(t *testing.T) {
	t.Parallel()

	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatalf("read the embedded prompts directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no prompt assets are embedded — the prompts/ directory is empty")
	}
	for _, e := range entries {
		got := mustPrompt(e.Name())
		if strings.TrimSpace(got) == "" {
			t.Errorf("prompt asset %s loads as empty text", e.Name())
		}
		if strings.HasSuffix(got, "\n") {
			t.Errorf("prompt asset %s still ends in a newline after load: %q", e.Name(), got)
		}
	}
}

// TestClip pins the heuristic title rule the TUI, internal/run and internal/schedule used to keep
// three copies of. The 60% floor is compared as a BYTE index against a rune cap in every one of
// those copies, so it is pinned here as it behaves rather than as it reads: the non-ASCII case
// below breaks on a boundary a rune-indexed floor would have refused.
func TestClip(t *testing.T) {
	t.Parallel()

	long := "The quick brown fox jumps over the lazy dog and then keeps running"
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"a short line is its own title", "fix the login bug", MaxRunes, "fix the login bug"},
		{"the first line wins over the rest", "rename the store\nand add tests", MaxRunes, "rename the store"},
		{"an over-cap line breaks at the last space past the floor", long, MaxRunes, "The quick brown fox jumps over the lazy dog and…"},
		{"a hard cut when the only space is at or before the floor", "ab " + strings.Repeat("x", 60), MaxRunes, "ab " + strings.Repeat("x", 47) + "…"},
		{"trailing spaces on the first line are dropped", "fix the bug   \nand more", MaxRunes, "fix the bug"},
		{"the byte-indexed floor clears early on a multibyte line", strings.Repeat("日", 20) + " " + strings.Repeat("x", 40), MaxRunes, strings.Repeat("日", 20) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Clip(tc.in, tc.max); got != tc.want {
				t.Errorf("Clip(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// TestDerive pins the fallback half: text that names no task earns the dated label instead, and
// everything else is clipped.
func TestDerive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 14, 5, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a prompt is its own title", "check the build", "check the build"},
		{"empty text falls back to the dated label", "   ", "Session 2026-08-30"},
		{"a code fence has no useful title", "```go\nfunc main() {}", "Session 2026-08-30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Derive(tc.in, MaxRunes, now); got != tc.want {
				t.Errorf("Derive(%q, %d, %v) = %q, want %q", tc.in, MaxRunes, now, got, tc.want)
			}
		})
	}
}

// TestDeriveSpellsTheZoneItIsHanded proves the dated fallback never relocates the instant: which
// zone a derived title is spelled in is the CALLER's stated choice (internal/run's Spec.title makes
// it on its own line), so the same instant handed in twice, in two zones straddling midnight,
// spells two different dates.
func TestDeriveSpellsTheZoneItIsHanded(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 8, 30, 23, 30, 0, 0, time.UTC)
	ahead := instant.In(time.FixedZone("UTC+9", 9*60*60))

	if got, want := Derive("", MaxRunes, instant), "Session 2026-08-30"; got != want {
		t.Errorf("Derive in UTC = %q, want %q", got, want)
	}
	if got, want := Derive("", MaxRunes, ahead), "Session 2026-08-31"; got != want {
		t.Errorf("Derive in UTC+9 = %q, want %q", got, want)
	}
}
