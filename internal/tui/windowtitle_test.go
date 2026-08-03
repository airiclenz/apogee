package tui

import (
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The terminal window's title (session-name-window-title plan, item 2)
// ----------------------------------------------------------------------------
//
// Two things are pinned here, and only one of them is cosmetic. The shape — `✭ <name>`, clipped to
// windowTitleRunes — is the owner's ratified spelling. The other is a security property: the name
// reaching this seam is untrusted (a model's reply, a pasted /rename, a record's Meta.Title read
// back off disk) and it is handed to the terminal inside an OSC 2 payload, where a BEL TERMINATES
// the sequence and everything after it is executed rather than displayed. So every case below is
// asserted twice: against the exact title it should produce, and against the invariant that no rune
// of the result is a control character at all.

// assertWindowTitleSafe fails when title is anything the OSC payload must not carry: a control rune
// of any kind, a title that lost its star, or bytes that are not valid UTF-8 (which is what a
// byte-counting clip would leave behind on a multi-byte name).
func assertWindowTitleSafe(t *testing.T, title string) {
	t.Helper()
	for _, r := range title {
		if unicode.IsControl(r) {
			t.Errorf("window title carries control rune %U: %q", r, title)
		}
	}
	if !strings.HasPrefix(title, windowTitleMark) {
		t.Errorf("window title = %q, want it led by the star %q", title, windowTitleMark)
	}
	if !utf8.ValidString(title) {
		t.Errorf("window title is not valid UTF-8: %q", title)
	}
}

// formatWindowTitle is the whole seam: what a name becomes on the way to the terminal.
func TestFormatWindowTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a plain name", "fix the broken parser", windowTitleMark + "fix the broken parser"},
		// The boundary, from both sides. A name exactly at the cap is left whole; one rune past it
		// is cut to the cap and closed with an ellipsis (clipRunes), so the ellipsis is chrome the
		// cap does not pay for.
		{
			"exactly at the cap",
			strings.Repeat("n", windowTitleRunes),
			windowTitleMark + strings.Repeat("n", windowTitleRunes),
		},
		{
			"one rune past the cap",
			strings.Repeat("n", windowTitleRunes+1),
			windowTitleMark + strings.Repeat("n", windowTitleRunes) + "…",
		},
		// The cap counts RUNES: 31 CJK characters are 93 bytes, so a byte cap would cut this one
		// mid-rune and hand the terminal a broken sequence (assertWindowTitleSafe catches that).
		{
			"a CJK name is capped in runes, not bytes",
			strings.Repeat("時", windowTitleRunes+1),
			windowTitleMark + strings.Repeat("時", windowTitleRunes) + "…",
		},
		// The attack this seam exists to stop: a name closing its own OSC sequence with a BEL and
		// opening another. The whole sequence goes, BEL included.
		{"an OSC 2 payload of its own", "my session\x1b]2;pwned\x07", windowTitleMark + "my session"},
		{"a bare BEL", "ding\x07dong", windowTitleMark + "dingdong"},
		// A TRAILING ESC is the unambiguous case: mid-string, an ESC takes the rune after it as a
		// two-character escape (title.escapeRunes), which is the safe direction but a fussier
		// expectation than this table is here to state.
		{"a bare ESC", "the parser rewrite\x1b", windowTitleMark + "the parser rewrite"},
		// Whitespace controls are not stripped but COLLAPSED — dropping a tab would weld two words
		// together — and the collapse is what keeps a pasted multi-line name to one title.
		{"a tab", "fix\tthe parser", windowTitleMark + "fix the parser"},
		{"a newline", "fix the parser\nplease", windowTitleMark + "fix the parser please"},
		// Nothing survivable falls back to the program's own name rather than leaving the star to
		// stand alone: a window that named the session and then named nothing would read as a bug.
		{"empty", "", windowTitleMark + windowTitleUnnamed},
		{"nothing but whitespace", " \t \n ", windowTitleMark + windowTitleUnnamed},
		{"nothing but control characters", "\x07\x1b[31m\x00", windowTitleMark + windowTitleUnnamed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatWindowTitle(tc.in)
			if got != tc.want {
				t.Errorf("formatWindowTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
			assertWindowTitleSafe(t, got)
		})
	}
}

// The window follows the session through every name it goes by: none, the heuristic the first
// request earns it, the one a naming call decides, and none again once /clear opens a fresh record.
func TestWindowTitleFollowsSession(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	seam := &titleSeam{reply: "rework the token cache"}
	m := newTitlingModel(t, host, seam, true)

	// Nothing said yet: the window says what program it belongs to, and no more.
	if got, want := m.windowTitle(), windowTitleMark+windowTitleUnnamed; got != want {
		t.Errorf("a silent session's window title = %q, want %q", got, want)
	}

	// The first request names the window on the spot, from the heuristic the first Save stamps —
	// hours before a naming call could answer.
	m, cmd := sendPrompt(t, m, "fix the parser")
	if got, want := m.windowTitle(), windowTitleMark+"fix the parser"; got != want {
		t.Errorf("after the first prompt the window title = %q, want the heuristic %q", got, want)
	}

	// The naming call lands. There is no record id yet, so the title is stashed rather than written
	// — and the window takes the name from the DECISION regardless (applyTitle → nameSession).
	msg, ok := namingCall(t, cmd)
	if !ok {
		t.Fatal("the first prompt batched no naming call")
	}
	m, cmd = stepCmd(t, m, msg)
	cmdMsg(cmd)
	if got, want := m.windowTitle(), windowTitleMark+"rework the token cache"; got != want {
		t.Errorf("after the naming call the window title = %q, want the generated name %q", got, want)
	}

	// /clear rotates to a fresh record, which has neither a name nor a request to derive one from.
	m = idle(t, m)
	m, _ = sendPrompt(t, m, "/clear")
	if got, want := m.windowTitle(), windowTitleMark+windowTitleUnnamed; got != want {
		t.Errorf("after /clear the window title = %q, want %q: a fresh session has no name", got, want)
	}
}

// The frame actually carries it. windowTitle can be right and the window still be nameless if the
// title never reaches tea.View — so this reads the field the renderer emits, in BOTH frames the
// Model can produce: the pre-geometry placeholder (a window is this session's window before it has
// a size) and the laid-out frame.
func TestViewCarriesWindowTitle(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "an older task"})
	m := newTitlingModel(t, host, &titleSeam{}, true)
	m, cmd := sendPrompt(t, m, "/rename the parser rewrite")
	cmdMsg(cmd)

	want := windowTitleMark + "the parser rewrite"
	if got := m.View().WindowTitle; got != want {
		t.Errorf("the laid-out frame's WindowTitle = %q, want %q", got, want)
	}

	// The same model before its first WindowSizeMsg: no geometry, no frame — still this session's
	// window.
	m.ready = false
	if got := m.View().WindowTitle; got != want {
		t.Errorf("the starting frame's WindowTitle = %q, want %q", got, want)
	}
}

// A generated title dropped by the never-clobber rule takes the window back to the heuristic — the
// title the record actually carries — rather than leaving it wearing a name nothing ever stored.
// The Model-side half of this is pinned in autotitle_test.go; what this adds is the window's own
// reading of it, which is the thing a human would see disagree with the /sessions browser.
func TestWindowTitleGivesUpADroppedAutoTitle(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{} // no active record: the naming call beat the first Save
	storeMeta(host, "old1", "an older task", "/ws", time.Now(), 0, nil)
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, _ = sendPrompt(t, m, "please fix the broken parser")
	m = step(t, m, exchangeDoneMsg{})
	m, cmd := stepCmd(t, m, autoTitleMsg{title: "a generated name"})
	m = runWrites(t, m, cmd)
	if got, want := m.windowTitle(), windowTitleMark+"a generated name"; got != want {
		t.Fatalf("window title = %q, want the stashed generated name %q", got, want)
	}

	// The human renames a DIFFERENT, stored session, which trips titleTouched without naming this
	// one; the first Save then mints the id the stash was waiting for, and the flush drops it.
	m = openBrowser(t, m)
	m = step(t, m, keyRune('r'))
	m.sessionBrowser.renameBuf = "some other session"
	m, cmd = stepCmd(t, m, keyEnter())
	m = runWrites(t, m, cmd)
	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = stepCmd(t, m, saveDoneMsg{})
	m = runWrites(t, m, cmd)

	if got, want := m.windowTitle(), windowTitleMark+"please fix the broken parser"; got != want {
		t.Errorf("window title = %q, want the heuristic the record carries (%q)", got, want)
	}
}
