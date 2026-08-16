package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// scriptedPresenter answers with a fixed outcome (or a fixed error), recording the request it
// saw — the hermetic stand-in for a host that owns the real presentation ladder.
type scriptedPresenter struct {
	outcome domain.PresentOutcome
	err     error
	seen    domain.PresentRequest
	calls   int
}

func (p *scriptedPresenter) Present(_ context.Context, req domain.PresentRequest) (domain.PresentOutcome, error) {
	p.seen = req
	p.calls++
	if p.err != nil {
		return domain.PresentOutcome{}, p.err
	}
	return p.outcome, nil
}

func presentCall(t *testing.T, args map[string]string) domain.ToolCall {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCall{ID: "c1", Tool: "present_document", Arguments: raw}
}

// presentWorkspace writes report.html — and report.bat, the document rung 1 refuses to hand the
// OS handler — into a fresh workspace and returns the root.
func presentWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"report.html": "<h1>hi</h1>",
		"report.bat":  "@echo off\r\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return root
}

func TestPresentDocument_OutcomeWordingPerRung(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		file    string // the document presented; empty means report.html
		outcome domain.PresentOutcome
		want    string
	}{
		{
			name:    "opened",
			outcome: domain.PresentOutcome{Method: domain.PresentOpened, Location: "report.html"},
			want:    "Presented report.html: opened on the user's machine.",
		},
		{
			name:    "served",
			outcome: domain.PresentOutcome{Method: domain.PresentServed, Location: "http://192.168.64.2:8080/d/abc/report.html"},
			want:    "Presented report.html: shown in the transcript with a link (http://192.168.64.2:8080/d/abc/report.html).",
		},
		{
			name:    "shown",
			outcome: domain.PresentOutcome{Method: domain.PresentShown, Location: "report.html"},
			want:    "Presented report.html: the path is shown in the transcript for the user to open.",
		},
		{
			// The Method enum is open (ADR 0019): an outcome this build does not know may only
			// claim the baseline, which is the rung that is always true.
			name:    "unknown method degrades to the baseline wording",
			outcome: domain.PresentOutcome{Method: domain.PresentMethod("beamed"), Location: "x"},
			want:    "Presented report.html: the path is shown in the transcript for the user to open.",
		},
		{
			// A served rung with no URL cannot be relayed as a link.
			name:    "served without a location degrades to the baseline wording",
			outcome: domain.PresentOutcome{Method: domain.PresentServed},
			want:    "Presented report.html: the path is shown in the transcript for the user to open.",
		},
		{
			// A document rung 1 refuses to hand the OS handler (present.OpenerRenderable, the
			// launch bound added 2026-07-26) is a DEGRADE, not a refusal: the host answers
			// "shown", the tool reports the baseline rung, and the call is not an error — the
			// path really is in the transcript. Nothing was launched to report otherwise.
			name:    "a non-renderable extension reads as the baseline rung, not an error",
			file:    "report.bat",
			outcome: domain.PresentOutcome{Method: domain.PresentShown, Location: "report.bat"},
			want:    "Presented report.bat: the path is shown in the transcript for the user to open.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file := tc.file
			if file == "" {
				file = "report.html"
			}
			presenter := &scriptedPresenter{outcome: tc.outcome}
			tool := NewPresentDocument(presentWorkspace(t), presenter)

			res, err := tool.Execute(context.Background(), presentCall(t, map[string]string{"path": file}))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("Execute result is an error: %q", res.Content)
			}
			if res.Content != tc.want {
				t.Errorf("result = %q, want %q", res.Content, tc.want)
			}
			if presenter.calls != 1 {
				t.Errorf("presenter consulted %d times, want exactly 1 — rung 0 always runs", presenter.calls)
			}
		})
	}
}

func TestPresentDocument_RequestCarriesAbsolutePathDisplayPathAndTitle(t *testing.T) {
	t.Parallel()

	root := presentWorkspace(t)
	presenter := &scriptedPresenter{outcome: domain.PresentOutcome{Method: domain.PresentShown}}
	tool := NewPresentDocument(root, presenter)

	// The presenting agent's run identity rides the call's ctx exactly as the asking agent's does
	// (the engine installs it at the dispatch seam): the tool must copy both onto the request, or
	// a child's document reaches the host looking like the top-level agent's.
	ctx := domain.WithSpawnCallID(domain.WithSubAgentDepth(context.Background(), 2), "call-77")
	_, err := tool.Execute(ctx, presentCall(t, map[string]string{
		"path": "report.html", "title": "  Architecture review  ",
	}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !filepath.IsAbs(presenter.seen.Path) {
		t.Errorf("Path = %q, want an absolute path", presenter.seen.Path)
	}
	if _, err := os.Stat(presenter.seen.Path); err != nil {
		t.Errorf("Path %q does not exist: %v", presenter.seen.Path, err)
	}
	if presenter.seen.DisplayPath != "report.html" {
		t.Errorf("DisplayPath = %q, want %q", presenter.seen.DisplayPath, "report.html")
	}
	if presenter.seen.Title != "Architecture review" {
		t.Errorf("Title = %q, want the trimmed %q", presenter.seen.Title, "Architecture review")
	}
	if presenter.seen.Depth != 2 {
		t.Errorf("Depth = %d, want the ctx-installed 2 — the request carries the presenting "+
			"agent's nesting level", presenter.seen.Depth)
	}
	if presenter.seen.SpawnCallID != "call-77" {
		t.Errorf("SpawnCallID = %q, want the ctx-installed %q — depth alone cannot tell two "+
			"sibling runs apart", presenter.seen.SpawnCallID, "call-77")
	}
}

// TestPresentDocument_TopLevelRequestCarriesTheHonestZeroIdentity pins the floor: with nothing
// installed on the ctx the request reads depth 0 and no spawn id — the top-level agent's true
// identity, not a lost value — so a host need not tell "absent" from "outermost".
func TestPresentDocument_TopLevelRequestCarriesTheHonestZeroIdentity(t *testing.T) {
	t.Parallel()

	presenter := &scriptedPresenter{outcome: domain.PresentOutcome{Method: domain.PresentShown}}
	_, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "report.html"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if presenter.seen.Depth != 0 {
		t.Errorf("Depth = %d, want 0 for a call with no installed identity", presenter.seen.Depth)
	}
	if presenter.seen.SpawnCallID != "" {
		t.Errorf("SpawnCallID = %q, want empty — no sub_agent call spawned the top-level agent",
			presenter.seen.SpawnCallID)
	}
}

func TestPresentDocument_AbsolutePathInsideRootStillDisplaysRelative(t *testing.T) {
	t.Parallel()

	root := presentWorkspace(t)
	nested := filepath.Join(root, "docs", "reports")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "review.md"), []byte("# review"), 0o600); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	presenter := &scriptedPresenter{outcome: domain.PresentOutcome{Method: domain.PresentShown}}
	_, err := NewPresentDocument(root, presenter).Execute(context.Background(),
		presentCall(t, map[string]string{"path": filepath.Join(nested, "review.md")}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}

	want := filepath.Join("docs", "reports", "review.md")
	if presenter.seen.DisplayPath != want {
		t.Errorf("DisplayPath = %q, want %q", presenter.seen.DisplayPath, want)
	}
}

func TestPresentDocument_IsReadOnly(t *testing.T) {
	t.Parallel()
	if !domain.IsReadOnly(NewPresentDocument(t.TempDir(), &scriptedPresenter{})) {
		t.Error("present_document must be read-only (runs in Plan, never gates)")
	}
}

func TestPresentDocument_IsNotExternalEffect(t *testing.T) {
	t.Parallel()
	tool := domain.Tool(NewPresentDocument(t.TempDir(), &scriptedPresenter{}))
	if _, ok := tool.(domain.ExternalEffectTool); ok {
		t.Error("present_document must NOT be an ExternalEffectTool (the user's own display is not a stubbable service)")
	}
}

func TestPresentDocument_NilPresenterIsGracefulResultError(t *testing.T) {
	t.Parallel()

	res, err := NewPresentDocument(presentWorkspace(t), nil).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "report.html"}))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("a nil Presenter should yield a graceful result error, not a panic or Go error")
	}
}

func TestPresentDocument_EmptyPathIsResultError(t *testing.T) {
	t.Parallel()

	presenter := &scriptedPresenter{}
	res, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "  "}))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("an empty path should be a result-level error")
	}
	if presenter.calls != 0 {
		t.Error("the Presenter must not be consulted for a missing path")
	}
}

func TestPresentDocument_MissingFileIsResultError(t *testing.T) {
	t.Parallel()

	presenter := &scriptedPresenter{}
	res, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "nope.html"}))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("a missing file should be a result-level error; got %q", res.Content)
	}
	if presenter.calls != 0 {
		t.Error("the Presenter must not be consulted for a file that does not exist")
	}
}

func TestPresentDocument_DirectoryIsResultError(t *testing.T) {
	t.Parallel()

	root := presentWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	presenter := &scriptedPresenter{}
	res, err := NewPresentDocument(root, presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "docs"}))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("a directory should be a result-level error; got %q", res.Content)
	}
	if presenter.calls != 0 {
		t.Error("the Presenter must not be consulted for a directory")
	}
}

func TestPresentDocument_PathEscapeIsResultError(t *testing.T) {
	t.Parallel()

	presenter := &scriptedPresenter{}
	res, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "../outside.html"}))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("a path escaping the workspace should be a result-level error; got %q", res.Content)
	}
	if presenter.calls != 0 {
		t.Error("the Presenter must not be consulted for a path outside the workspace")
	}
}

// TestPresentDocument_RefusesEscapingSymlink pins the workspace fence on the file
// present_document actually OPENS, not just on the path argument it was given. The tool is
// ReadOnly(), so it runs unapproved in every mode, Plan included, and what it confirms to
// exist is what the host is then asked to open on the user's machine.
//
// Both cases are boundary pins rather than new behaviour: the escape is already refused by
// resolveInRoot, and the fix closes the check-then-use gap BEHIND it — the existence check is
// now an fstat of a descriptor opened through the pinned root instead of an os.Stat that
// re-walks the resolved string. The test fails if either half is ever dropped.
func TestPresentDocument_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := presentWorkspace(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "config.yaml"), []byte("api-key: SUPERSECRET"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "config.yaml"), filepath.Join(root, "escape.html")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A RELATIVE in-workspace symlink must still present: the fence narrows what leaves the
	// workspace, never what stays inside it.
	if err := os.Symlink("report.html", filepath.Join(root, "link.html")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	t.Run("escaping symlink", func(t *testing.T) {
		t.Parallel()

		presenter := &scriptedPresenter{outcome: domain.PresentOutcome{Method: domain.PresentOpened}}
		res, err := NewPresentDocument(root, presenter).
			Execute(context.Background(), presentCall(t, map[string]string{"path": "escape.html"}))
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("IsError = false, want true: a symlink out of the workspace was presented (%q)", res.Content)
		}
		if !strings.Contains(res.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", res.Content)
		}
		if presenter.calls != 0 {
			t.Errorf("the Presenter was consulted %d times for a file outside the workspace, want 0", presenter.calls)
		}
	})

	t.Run("in-workspace symlink still presents", func(t *testing.T) {
		t.Parallel()

		presenter := &scriptedPresenter{outcome: domain.PresentOutcome{Method: domain.PresentOpened}}
		res, err := NewPresentDocument(root, presenter).
			Execute(context.Background(), presentCall(t, map[string]string{"path": "link.html"}))
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if res.IsError {
			t.Fatalf("an in-workspace symlink stopped presenting: %q", res.Content)
		}
		if presenter.calls != 1 {
			t.Fatalf("the Presenter was consulted %d times, want 1", presenter.calls)
		}
		if presenter.seen.DisplayPath != "report.html" {
			t.Errorf("DisplayPath = %q, want the resolved %q", presenter.seen.DisplayPath, "report.html")
		}
	})
}

func TestPresentDocument_PresenterErrorDegradesToShownNotAnError(t *testing.T) {
	t.Parallel()

	presenter := &scriptedPresenter{err: errors.New("opener exploded")}
	res, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(context.Background(), presentCall(t, map[string]string{"path": "report.html"}))
	if err != nil {
		t.Fatalf("a mechanism failure must not be a Go error; got %v", err)
	}
	if res.IsError {
		t.Errorf("a mechanism failure must degrade to the baseline, not an error result; got %q", res.Content)
	}
	want := "Presented report.html: the path is shown in the transcript for the user to open."
	if res.Content != want {
		t.Errorf("result = %q, want the degraded %q", res.Content, want)
	}
}

func TestPresentDocument_CancelledCtxIsGoError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewPresentDocument(presentWorkspace(t), &scriptedPresenter{}).
		Execute(ctx, presentCall(t, map[string]string{"path": "report.html"}))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx should be a Go error (context.Canceled); got %v", err)
	}
}

// A Presenter that fails BECAUSE the Turn was cancelled mid-presentation is the one error the
// tool must re-raise as a Go error, so the loop rolls the Turn back (ADR 0007) instead of
// recording a presentation that never reached the user.
func TestPresentDocument_PresenterErrorUnderCancellationIsGoError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	presenter := &cancellingPresenter{cancel: cancel}
	_, err := NewPresentDocument(presentWorkspace(t), presenter).
		Execute(ctx, presentCall(t, map[string]string{"path": "report.html"}))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a Presenter error under a cancelled ctx should be a Go error; got %v", err)
	}
}

type cancellingPresenter struct{ cancel context.CancelFunc }

func (p *cancellingPresenter) Present(ctx context.Context, _ domain.PresentRequest) (domain.PresentOutcome, error) {
	p.cancel()
	<-ctx.Done()
	return domain.PresentOutcome{}, ctx.Err()
}

func TestPresentDocument_SpecIsModelFacing(t *testing.T) {
	t.Parallel()

	tool := NewPresentDocument(t.TempDir(), &scriptedPresenter{})
	if tool.Name() != "present_document" {
		t.Errorf("name = %q, want %q", tool.Name(), "present_document")
	}
	if !strings.Contains(tool.Description(), "Show a finished document to the user.") {
		t.Errorf("description does not lead with the affordance: %q", tool.Description())
	}

	var schema struct {
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Errorf("required = %v, want [path]", schema.Required)
	}
	if _, ok := schema.Properties["title"]; !ok {
		t.Error("schema is missing the optional title property")
	}
}
