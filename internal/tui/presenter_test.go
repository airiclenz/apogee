package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/present"
)

// ----------------------------------------------------------------------------
// Test doubles for the ladder's rungs
// ----------------------------------------------------------------------------

// openerRunning returns an Opener for goos whose launches are captured instead of executed
// (present.Runner is the seam the package exposes for exactly this), so a ladder test asserts
// which rung ran without a desktop anywhere near it.
func openerRunning(goos string, argv *[]string) *present.Opener {
	return &present.Opener{
		GOOS: goos,
		Env:  func(string) string { return "" }, // headless: only darwin/windows/override reach a runner
		Run: func(name string, args ...string) error {
			*argv = append([]string{name}, args...)
			return nil
		},
	}
}

// failingOpener returns an Opener whose launch fails — the fail-visible case: an opener that was
// tried and did not deliver, as opposed to one that was never there (ErrNoOpener).
func failingOpener() *present.Opener {
	return &present.Opener{
		GOOS: "darwin",
		Env:  func(string) string { return "" },
		Run:  func(string, ...string) error { return errors.New("boom") },
	}
}

// headlessOpener returns an Opener with nothing to open into: a Linux session with no display
// server and no present.command, which answers present.ErrNoOpener.
func headlessOpener(t *testing.T) *present.Opener {
	t.Helper()
	return &present.Opener{
		GOOS: "linux",
		Env:  func(string) string { return "" },
		Run: func(name string, args ...string) error {
			t.Errorf("the ladder ran %q %v on a machine with no opener", name, args)
			return nil
		},
	}
}

// docServer starts a real (ephemeral-port) doc server advertising a fixed host, so a served URL
// can be asserted verbatim, and closes it with the test.
func docServer(t *testing.T) *present.DocServer {
	t.Helper()
	srv := &present.DocServer{Host: "192.168.64.2"}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// writeDoc writes a document into a temp workspace and returns its absolute path.
func writeDoc(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("<h1>report</h1>"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// presentOnce drives one presentation through a bound uiPresenter and returns the outcome plus
// the single presentedMsg the ladder's baseline rung sent.
func presentOnce(t *testing.T, rungs Presentation, req domain.PresentRequest) (domain.PresentOutcome, presentedMsg) {
	t.Helper()
	prog := newStubProgram()
	ref := &programRef{}
	ref.bind(prog)

	out, err := (&uiPresenter{prog: ref, rungs: rungs}).Present(context.Background(), req)
	if err != nil {
		t.Fatalf("Present: unexpected error %v", err)
	}
	return out, onlyPresented(t, prog)
}

// onlyPresented asserts the stub program received exactly one presentedMsg and returns it — the
// rung-0 invariant: every presentation records exactly one transcript entry, whatever happened
// above the baseline.
func onlyPresented(t *testing.T, prog *stubProgram) presentedMsg {
	t.Helper()
	var found []presentedMsg
	for _, m := range prog.messages() {
		if msg, ok := m.(presentedMsg); ok {
			found = append(found, msg)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d presentedMsgs; want exactly 1 (rung 0 always runs, exactly once)", len(found))
	}
	return found[0]
}

// ----------------------------------------------------------------------------
// The ladder (ADR 0019 §2)
// ----------------------------------------------------------------------------

// TestPresenterLadderPicksRung walks the ladder's decision table: which rung a session reaches,
// and what the entry says when a rung was tried and did not deliver. The two gates it pins are
// the ones the ADR is explicit about — locality is the LADDER's (a remote session never opens,
// even with an opener wired), while "is there anything to open into" is the OPENER's alone, so a
// configured present.command opens on a machine with no detectable desktop.
func TestPresenterLadderPicksRung(t *testing.T) {
	t.Parallel()

	html := writeDoc(t, "review.html")
	markdown := writeDoc(t, "review.md")

	tests := []struct {
		name       string
		rungs      func(t *testing.T) Presentation
		path       string
		wantMethod domain.PresentMethod
		wantReason string
		wantServed bool // the outcome carries a URL rather than the display path
	}{
		{
			name: "local desktop opens",
			rungs: func(*testing.T) Presentation {
				return Presentation{Local: true, Opener: openerRunning("darwin", new([]string))}
			},
			path:       html,
			wantMethod: domain.PresentOpened,
		},
		{
			name: "local with a present.command opens on a machine with no desktop",
			rungs: func(*testing.T) Presentation {
				o := openerRunning("linux", new([]string)) // no DISPLAY: HasDesktop is false
				o.CommandOverride = "zed {path}"
				return Presentation{Local: true, Opener: o}
			},
			path:       markdown,
			wantMethod: domain.PresentOpened,
		},
		{
			name:       "local with nothing to open into degrades",
			rungs:      func(t *testing.T) Presentation { return Presentation{Local: true, Opener: headlessOpener(t)} },
			path:       markdown,
			wantMethod: domain.PresentShown,
			wantReason: "no opener on this machine",
		},
		{
			name:       "an opener that fails is visible",
			rungs:      func(*testing.T) Presentation { return Presentation{Local: true, Opener: failingOpener()} },
			path:       html,
			wantMethod: domain.PresentShown,
			wantReason: "could not open: ",
		},
		{
			name:       "local with no opener wired stays at the baseline",
			rungs:      func(*testing.T) Presentation { return Presentation{Local: true} },
			path:       html,
			wantMethod: domain.PresentShown,
		},
		{
			name:       "remote html is served",
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t)} },
			path:       html,
			wantMethod: domain.PresentServed,
			wantServed: true,
		},
		{
			name:       "remote markdown is not browser-renderable",
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t)} },
			path:       markdown,
			wantMethod: domain.PresentShown,
		},
		{
			name: "a remote session never opens, opener or not",
			rungs: func(t *testing.T) Presentation {
				return Presentation{Opener: headlessOpener(t), Docs: docServer(t)}
			},
			path:       markdown,
			wantMethod: domain.PresentShown,
		},
		{
			name:       "remote with no doc server stays at the baseline",
			rungs:      func(*testing.T) Presentation { return Presentation{} },
			path:       html,
			wantMethod: domain.PresentShown,
		},
		{
			name:       "a doc server that cannot read the file is visible",
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t)} },
			path:       filepath.Join(t.TempDir(), "gone.html"),
			wantMethod: domain.PresentShown,
			wantReason: "could not serve: ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := domain.PresentRequest{Path: tc.path, DisplayPath: "docs/" + filepath.Base(tc.path)}
			out, msg := presentOnce(t, tc.rungs(t), req)

			if out.Method != tc.wantMethod {
				t.Errorf("method = %q; want %q", out.Method, tc.wantMethod)
			}
			if msg.Method != out.Method {
				t.Errorf("entry method = %q; outcome method = %q — the transcript and the model must agree", msg.Method, out.Method)
			}
			if !strings.HasPrefix(msg.Reason, tc.wantReason) || (tc.wantReason == "" && msg.Reason != "") {
				t.Errorf("reason = %q; want prefix %q", msg.Reason, tc.wantReason)
			}
			if tc.wantServed {
				if !strings.HasPrefix(msg.Location, "http://192.168.64.2:") || !strings.Contains(msg.Location, "/d/") {
					t.Errorf("location = %q; want a doc-server URL on the advertised host", msg.Location)
				}
				if out.Location != msg.Location {
					t.Errorf("outcome location = %q; want the served URL %q", out.Location, msg.Location)
				}
				return
			}
			if msg.Location != "" {
				t.Errorf("location = %q; want empty on a rung that served nothing", msg.Location)
			}
			if out.Location != req.DisplayPath {
				t.Errorf("outcome location = %q; want the display path %q", out.Location, req.DisplayPath)
			}
		})
	}
}

// TestPresenterOpensTheResolvedPath proves the opener is handed the ABSOLUTE path the tool
// resolved, never the display path — the display half is for the transcript alone.
func TestPresenterOpensTheResolvedPath(t *testing.T) {
	t.Parallel()
	var argv []string
	rungs := Presentation{Local: true, Opener: openerRunning("darwin", &argv)}

	_, msg := presentOnce(t, rungs, domain.PresentRequest{
		Path:        "/workspace/docs/review.html",
		DisplayPath: "docs/review.html",
		Title:       "Architecture review",
	})

	if want := []string{"open", "/workspace/docs/review.html"}; strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v; want %v", argv, want)
	}
	if msg.Path != "docs/review.html" || msg.Title != "Architecture review" {
		t.Errorf("entry = %+v; want the display path and the title", msg)
	}
}

// TestPresenterCancelledCtxPresentsNothing proves a user stop is honoured before any mechanism
// runs: Present returns ctx.Err() (so the loop rolls the Turn back, ADR 0007) and the transcript
// records nothing, because nothing was presented.
func TestPresenterCancelledCtxPresentsNothing(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	ref := &programRef{}
	ref.bind(prog)
	p := &uiPresenter{prog: ref, rungs: Presentation{Local: true, Opener: headlessOpener(t)}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := p.Present(ctx, domain.PresentRequest{Path: "/ws/a.html", DisplayPath: "a.html"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
	if out.Method != "" {
		t.Errorf("method = %q; want the zero outcome on a cancelled presentation", out.Method)
	}
	if msgs := prog.messages(); len(msgs) != 0 {
		t.Errorf("captured %d msgs; want none — nothing was presented", len(msgs))
	}
}

// TestPresenterUnboundIsSafe proves the delegate is usable before Run binds the program (the
// no-op send), the same headless-safety the Approver and Asker have.
func TestPresenterUnboundIsSafe(t *testing.T) {
	t.Parallel()
	p := &uiPresenter{prog: &programRef{}, rungs: Presentation{}} // never bound

	out, err := p.Present(context.Background(), domain.PresentRequest{Path: "/ws/a.md", DisplayPath: "a.md"})
	if err != nil {
		t.Fatalf("Present: %v", err)
	}
	if out.Method != domain.PresentShown || out.Location != "a.md" {
		t.Errorf("outcome = %+v; want the baseline rung", out)
	}
}

// TestBridgePresenterNilUntilInstalled proves the nil-delegate contract the registry keys on: a
// Bridge with no presentation installed answers a truly nil Presenter (not a typed-nil pointer
// that would satisfy the interface), so present_document goes unregistered on a headless host.
func TestBridgePresenterNilUntilInstalled(t *testing.T) {
	t.Parallel()
	b := NewBridge()
	if b.Presenter() != nil {
		t.Error("Presenter() is non-nil before SetPresentation — present_document would be registered with no ladder")
	}

	prog := newStubProgram()
	b.SetPresentation(Presentation{})
	b.Bind(prog)

	p := b.Presenter()
	if p == nil {
		t.Fatal("Presenter() is nil after SetPresentation")
	}
	if _, err := p.Present(context.Background(), domain.PresentRequest{DisplayPath: "a.md"}); err != nil {
		t.Fatalf("Present: %v", err)
	}
	onlyPresented(t, prog) // the installed presenter shares the Bridge's programRef
}

// ----------------------------------------------------------------------------
// The transcript entry (rung 0)
// ----------------------------------------------------------------------------

// TestPresentedEntryRendering pins the shape of the presentation block: the ▤ marker leading the
// title (or the path when there is none), the path on its own line, the URL on its own line only
// when one was served, and the closing status line. It is deliberately not a tool card.
func TestPresentedEntryRendering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  presentedMsg
		want []string
	}{
		{
			name: "titled and served",
			msg: presentedMsg{
				Title:    "Architecture review",
				Path:     "docs/review.html",
				Location: "http://192.168.64.2:51234/d/deadbeef/review.html",
				Method:   domain.PresentServed,
			},
			want: []string{
				"▤ Architecture review",
				"  docs/review.html",
				"  http://192.168.64.2:51234/d/deadbeef/review.html",
				"  cmd+click to open",
			},
		},
		{
			name: "untitled and opened",
			msg:  presentedMsg{Path: "docs/review.html", Method: domain.PresentOpened},
			want: []string{
				"▤ docs/review.html",
				"  opened on your machine",
			},
		},
		{
			name: "a degraded rung says what happened and that the path stands",
			msg: presentedMsg{
				Path:   "docs/review.html",
				Method: domain.PresentShown,
				Reason: "no opener on this machine",
			},
			want: []string{
				"▤ docs/review.html",
				"  no opener on this machine — path shown",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := &transcript{}
			tr.addPresented(tc.msg)
			if got := plainRender(tr); got != strings.Join(tc.want, "\n") {
				t.Errorf("rendered:\n%s\nwant:\n%s", got, strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestPresentedEntryKeepsPathAndURLWhole is the linkification invariant: at a width far too narrow
// for them, the path and the URL are still each ONE physical line, unwrapped and unclipped, so the
// terminal that turns them into something clickable sees a whole token. Only the title and the
// status line — prose, not links — wrap.
func TestPresentedEntryKeepsPathAndURLWhole(t *testing.T) {
	t.Parallel()
	const (
		path = "docs/reports/architecture-review.html"
		url  = "http://192.168.64.2:51234/d/0123456789abcdef0123456789abcdef/architecture-review.html"
	)
	tr := &transcript{}
	tr.addPresented(presentedMsg{
		Title:    "A rather long architecture review title that cannot fit",
		Path:     path,
		Location: url,
		Method:   domain.PresentServed,
	})

	lines := strings.Split(renderPlain(tr, 24), "\n")
	var sawPath, sawURL bool
	for _, ln := range lines {
		switch strings.TrimSpace(ln) {
		case path:
			sawPath = true
		case url:
			sawURL = true
		}
	}
	if !sawPath {
		t.Errorf("the path was split or clipped at width 24:\n%s", strings.Join(lines, "\n"))
	}
	if !sawURL {
		t.Errorf("the URL was split or clipped at width 24:\n%s", strings.Join(lines, "\n"))
	}
	if got := lines[0]; !strings.HasPrefix(got, "▤ A rather long") {
		t.Errorf("first line = %q; want the wrapped title under the ▤ marker", got)
	}
}

// TestPresentedEntrySanitizesModelText proves the untrusted halves are treated like every other
// model string reaching the terminal: the title is escape-stripped (and clipped), and so is the
// path — a filename is filesystem data, not this program's — while the path is never truncated.
func TestPresentedEntrySanitizesModelText(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addPresented(presentedMsg{
		Title: "\x1b]52;c;cGF3bmVk\x07report",
		Path:  "docs/\x1b[2Jreview.html",
	})

	got := tr.entries[0].presented
	if strings.ContainsRune(got.Title, 0x1b) || strings.ContainsRune(got.Path, 0x1b) {
		t.Errorf("an ESC byte survived into the transcript: %+v", got)
	}
	if !strings.HasSuffix(got.Path, "review.html") {
		t.Errorf("path = %q; want the escape-stripped path intact", got.Path)
	}
}

// TestUpdateFoldsPresentedMsg proves the Update loop records the presentation without touching the
// state machine: a presentation arriving mid-run leaves the worker running and shows in the View.
func TestUpdateFoldsPresentedMsg(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.state = stateRunning

	m = step(t, m, presentedMsg{Path: "docs/review.html", Method: domain.PresentOpened})

	if m.state != stateRunning {
		t.Errorf("state = %v; want the running worker untouched", m.state)
	}
	if view := plain(m.View()); !strings.Contains(view, "docs/review.html") {
		t.Errorf("the View does not carry the presented path:\n%s", view)
	}
}

// TestPresentDocumentToolCard proves the tool call itself still renders as an ordinary card
// (label, verb, target) — the presentation entry is additional to it, not a replacement.
func TestPresentDocumentToolCard(t *testing.T) {
	t.Parallel()
	tv := presentToolCall(domain.ToolCall{
		Tool:      "present_document",
		Arguments: []byte(`{"path":"docs/review.html","title":"Architecture review"}`),
	})
	if tv.Label != "Present" || tv.Verb != "presenting" || tv.Target != "docs/review.html" {
		t.Errorf("view = %+v; want the Present/presenting/path registry entry", tv)
	}
	tv.enrichWithResult(domain.ToolResult{Content: "Presented docs/review.html: opened on the user's machine."})
	if tv.Summary.Text != "Presented docs/review.html: opened on the user's machine." {
		t.Errorf("summary = %q; want the result's first line", tv.Summary.Text)
	}
}
