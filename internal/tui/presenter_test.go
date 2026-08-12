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

// docServer starts a real (ephemeral-port) doc server advertising a fixed host and fenced to the
// workspace root its documents live in, so a served URL can be asserted verbatim, and closes it
// with the test.
func docServer(t *testing.T, root string) *present.DocServer {
	t.Helper()
	srv := &present.DocServer{Host: "192.168.64.2", Root: root}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// writeDoc writes a document into the workspace root and returns its absolute path.
func writeDoc(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
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

	root := t.TempDir()
	html := writeDoc(t, root, "review.html")
	markdown := writeDoc(t, root, "review.md")
	script := writeDoc(t, root, "review.bat")
	// Markdown, not HTML: since 2026-08-12 rung 1 refuses .html on its extension alone, so an
	// .html fixture here would be refused before cmdSafe saw the name and the row would prove
	// nothing about the NAME bound it exists to pin.
	injected := writeDoc(t, root, "report&calc&.md")

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
			path:       markdown,
			wantMethod: domain.PresentOpened,
		},
		{
			// The user-visible half of ADR 0019's fourth amendment: an HTML report on a LOCAL
			// session no longer launches a browser at all. climb's two branches are exclusive, so
			// rung 1's refusal degrades to the baseline rather than falling through to rung 2 —
			// the URL rung is for a machine that is not this one, and the policy that bounds an
			// active page can only ride a served response.
			name: "a local desktop refuses an active-content page and degrades to the baseline",
			rungs: func(t *testing.T) Presentation {
				o := headlessOpener(t)
				o.GOOS = "darwin" // a real desktop: only the extension keeps this from launching
				return Presentation{Local: true, Opener: o}
			},
			path:       html,
			wantMethod: domain.PresentShown,
			wantReason: "no opener on this machine",
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
			// The document the OS handler would EXECUTE rather than show: rung 1 refuses it
			// (present.OpenerRenderable) and the ladder degrades exactly as it does on a machine
			// with nothing to open into, which is the same ErrNoOpener answer.
			name: "a local desktop still refuses a document the handler would run",
			rungs: func(t *testing.T) Presentation {
				o := headlessOpener(t)
				o.GOOS = "darwin" // a real desktop: only the extension keeps this from launching
				return Presentation{Local: true, Opener: o}
			},
			path:       script,
			wantMethod: domain.PresentShown,
			wantReason: "no opener on this machine",
		},
		{
			// The name cmd.exe would re-parse as a second command: rung 1's Windows opener
			// refuses it (the extension is fine — the injection rides the rest of the name) and
			// the ladder degrades exactly as it does for a document the handler would run.
			name: "a windows desktop still refuses a name cmd.exe would parse",
			rungs: func(t *testing.T) Presentation {
				o := headlessOpener(t)
				o.GOOS = "windows" // a real desktop: only the NAME keeps this from launching
				return Presentation{Local: true, Opener: o}
			},
			path:       injected,
			wantMethod: domain.PresentShown,
			wantReason: "no opener on this machine",
		},
		{
			name:       "an opener that fails is visible",
			rungs:      func(*testing.T) Presentation { return Presentation{Local: true, Opener: failingOpener()} },
			path:       markdown, // reaches the runner: an .html would be refused before it launched
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
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t, root)} },
			path:       html,
			wantMethod: domain.PresentServed,
			wantServed: true,
		},
		{
			name:       "remote markdown is not browser-renderable",
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t, root)} },
			path:       markdown,
			wantMethod: domain.PresentShown,
		},
		{
			name: "a remote session never opens, opener or not",
			rungs: func(t *testing.T) Presentation {
				return Presentation{Opener: headlessOpener(t), Docs: docServer(t, root)}
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
			rungs:      func(t *testing.T) Presentation { return Presentation{Docs: docServer(t, root)} },
			path:       filepath.Join(root, "gone.html"),
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
		Path:        "/workspace/docs/review.md",
		DisplayPath: "docs/review.md",
		Title:       "Architecture review",
	})

	if want := []string{"open", "/workspace/docs/review.md"}; strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v; want %v", argv, want)
	}
	if msg.Path != "docs/review.md" || msg.Title != "Architecture review" {
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

// TestBridgeSetPresentationSwapsTheLadderInPlace proves the live half of ADR 0037: a `present.` key
// committed mid-session re-installs the rungs on the presenter the ENGINE already holds — the same
// pointer, so the next presentation climbs the new ladder — rather than making a second presenter
// nothing would ever dispatch to.
func TestBridgeSetPresentationSwapsTheLadderInPlace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := writeDoc(t, root, "report.html")

	prog := newStubProgram()
	b := NewBridge()
	b.Bind(prog)
	b.SetPresentation(Presentation{Local: true}) // a local session with no opener wired
	before := b.Presenter()

	out, err := before.Present(context.Background(),
		domain.PresentRequest{Path: path, DisplayPath: "report.html"})
	if err != nil {
		t.Fatalf("Present: %v", err)
	}
	if out.Method != domain.PresentShown {
		t.Fatalf("method = %q; want the baseline rung before the swap", out.Method)
	}

	// The host's `present.` block moved: this session is remote now and serves rung 2.
	b.SetPresentation(Presentation{Docs: docServer(t, root)})
	if after := b.Presenter(); after != before {
		t.Error("the Presenter was replaced; the engine captured the first one, so the swap would be invisible")
	}

	out, err = before.Present(context.Background(),
		domain.PresentRequest{Path: path, DisplayPath: "report.html"})
	if err != nil {
		t.Fatalf("Present after the swap: %v", err)
	}
	if out.Method != domain.PresentServed || !strings.Contains(out.Location, "192.168.64.2:") {
		t.Errorf("outcome = %+v; want the swapped-in doc server to have carried this presentation", out)
	}
}

// TestTheTwoExtensionSetsCrossOnlyOnActiveContent pins the relationship between the ladder's two
// extension sets. It used to be a SUBSET relation — rung 2 served what a browser renders, rung 1
// handed the OS handler a strictly wider set — and this test used to assert exactly that.
//
// The relation is now a CROSSING, and that is a decision rather than a broken invariant (ADR 0019,
// fourth amendment 2026-08-12). The three active formats left rung 1 because the rung that shows
// active content must be the rung that can BOUND it: a served response carries a
// Content-Security-Policy, a file:// launch carries nothing. So the subset direction is inverted
// on exactly one axis and holds everywhere else, which is what this test states — asserting the
// inversion in both directions, so a later "repair" that quietly restores .html to the opener set
// fails here rather than passing.
func TestTheTwoExtensionSetsCrossOnlyOnActiveContent(t *testing.T) {
	t.Parallel()

	// The axis of the inversion: rung 2 serves these, rung 1 must refuse them. A regression here
	// is a local session launching a browser on a page it cannot police.
	for _, ext := range []string{".html", ".htm", ".svg"} {
		if !browserRenderableExts[ext] {
			t.Errorf("rung 2 no longer serves %q — it is then shown on no rung at all", ext)
		}
		if present.OpenerRenderable("report" + ext) {
			t.Errorf("rung 1 opens %q again — active content must stay on the rung that carries a policy", ext)
		}
	}

	// Everywhere else the old relation stands: what rung 2 serves, rung 1 still opens. Only .pdf
	// is left in that intersection today, and it is the inert one.
	for ext := range browserRenderableExts {
		switch ext {
		case ".html", ".htm", ".svg":
			continue
		}
		if !present.OpenerRenderable("report" + ext) {
			t.Errorf("rung 2 serves %q but rung 1 refuses it — an inert format must be openable on both rungs", ext)
		}
	}
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
// (label, verb, target) — the presentation entry is additional to it, not a replacement. The
// ratified table leads the row with the document's TITLE and leaves the outcome slot blank: what
// the call did is already said by the header and the title, so there is nothing for a slot to add.
func TestPresentDocumentToolCard(t *testing.T) {
	t.Parallel()
	tv := presentToolCall(domain.ToolCall{
		Tool:      "present_document",
		Arguments: []byte(`{"path":"docs/review.html","title":"Architecture review"}`),
	}, workspaceRoot{})
	if tv.Label != "Present" || tv.Verb != "presenting" || tv.Target != "Architecture review" {
		t.Errorf("view = %+v; want the Present/presenting/title registry entry", tv)
	}
	tv.enrichWithResult(domain.ToolResult{Content: "Presented docs/review.html: opened on the user's machine."}, workspaceRoot{})
	if tv.Summary.Text != "" {
		t.Errorf("summary = %q; want the table's blank slot", tv.Summary.Text)
	}

	// A call that named no title still says which document was presented.
	untitled := presentToolCall(domain.ToolCall{
		Tool:      "present_document",
		Arguments: []byte(`{"path":"docs/review.html"}`),
	}, workspaceRoot{})
	if untitled.Target != "docs/review.html" {
		t.Errorf("untitled target = %q; want the path fallback", untitled.Target)
	}
}
