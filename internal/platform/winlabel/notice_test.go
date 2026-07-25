package winlabel

import (
	"errors"
	"strings"
	"testing"
)

func TestConfinementTeardownNoticeWordsTheFailure(t *testing.T) {
	t.Parallel()

	if got := TeardownNotice(nil); got != "" {
		t.Errorf("TeardownNotice(nil) = %q, want \"\" so the caller can state it unconditionally", got)
	}

	got := TeardownNotice(errors.New(`the journal "C:\Users\dev\.apogee\confinement\labels-9.json" is kept`))
	if strings.Contains(got, "\n") {
		t.Errorf("notice = %q; want a single stderr line", got)
	}
	if !strings.Contains(got, `labels-9.json`) {
		t.Errorf("notice = %q; want it to name the journal that survived the failure", got)
	}
	if !strings.Contains(got, Remedy) {
		t.Errorf("notice = %q; want the same manual remedy the host report names", got)
	}
	if !strings.Contains(ResidueNotice([]string{`C:\work`}, nil), Remedy) {
		t.Error("the host report no longer quotes the shared remedy; the two surfaces have drifted")
	}
}

func TestWindowsLabelProgressNoticeNamesRootAndFence(t *testing.T) {
	t.Parallel()

	const root = `C:\work\proj`
	got := ProgressNotice(root)

	if got == "" {
		t.Fatal("ProgressNotice returned \"\"; a wait notice with no words explains nothing")
	}
	if strings.Contains(got, "\n") {
		t.Errorf("notice = %q; want a single pre-alt-screen stderr line", got)
	}
	if !strings.Contains(got, root) {
		t.Errorf("notice = %q; want it to name the workspace root being labelled", got)
	}
	// The fence wording is the shared remedy verbatim, so this surface never invents a third
	// spelling of the manual undo the teardown warning and host report already quote — the
	// ResidueNotice byte-identity assertion pattern.
	if !strings.Contains(got, Remedy) {
		t.Errorf("notice = %q; want the shared remedy %q so the surfaces cannot drift", got, Remedy)
	}
}

func TestWindowsResidueNoticeWordsBothFindings(t *testing.T) {
	t.Parallel()

	const journal = `C:\Users\dev\.apogee\confinement\labels-9.json`

	tests := []struct {
		name       string
		roots      []string
		unreadable []string
		want       []string // substrings the notice must carry
		wantEmpty  bool
	}{
		{
			name:      "nothing_outstanding",
			wantEmpty: true,
		},
		{
			name:  "outstanding_labels",
			roots: []string{`C:\work`, `D:\cache`},
			want:  []string{"2 path(s)", `C:\work, D:\cache`, "reverts them automatically", Remedy},
		},
		{
			name:       "unreadable_journal",
			unreadable: []string{journal},
			want:       []string{"journal present but unreadable: " + journal, "undecodable", Remedy},
		},
		{
			name:       "both_findings_are_stated",
			roots:      []string{`C:\work`},
			unreadable: []string{journal},
			want:       []string{`C:\work`, "journal present but unreadable: " + journal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResidueNotice(tt.roots, tt.unreadable)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("notice = %q, want \"\" so the caller can state it unconditionally", got)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("notice = %q; want it to carry %q", got, want)
				}
			}
			// Every continuation line stays aligned under the host report's "labels:" field.
			for _, line := range strings.Split(got, "\n")[1:] {
				if !strings.HasPrefix(line, residueIndent) {
					t.Errorf("continuation line %q is not indented under the labels field", line)
				}
			}
		})
	}

	// The labels half is worded exactly as it was before the unreadable finding joined it: the
	// host report renders this verbatim and its wording is pinned by internal/probe's tests.
	want := "1 path(s) may still carry apogee's Low integrity label: C:\\work\n" +
		residueIndent + "(a run was interrupted, or another apogee holds them now; a new session\n" +
		residueIndent + "reverts them automatically, or: " + Remedy + ")"
	if got := ResidueNotice([]string{`C:\work`}, nil); got != want {
		t.Errorf("the outstanding-labels wording drifted:\n got %q\nwant %q", got, want)
	}
}
