package notice_test

import (
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/notice"
)

// The composed notices in the order a reader meets them: what loaded first, then each loud skip,
// then the advisory warning — and only the last two carry the Anomaly flag a Driver routes on.
func TestContextFileNoticesOrderAndAnomalies(t *testing.T) {
	got := notice.ContextFileNotices(domain.ContextFilesReport{
		Files: []domain.ContextFileNote{
			{Name: "CONVENTIONS.md", Bytes: 512},
			{Name: "BROKEN.md", Err: "permission denied"},
			{Name: "AGENTS.md", Bytes: 3174},
		},
		StandingTokens: 9000,
		SystemShare:    4000,
	})

	want := []notice.ContextNotice{
		{Text: "context: CONVENTIONS.md (512 B), AGENTS.md (3.1 KiB)"},
		{Text: "context: BROKEN.md unreadable — permission denied", Anomaly: true},
		{Text: "standing system content ~8.8k tokens exceeds its Budget share (~3.9k) — trim context files, the task list or the system prompt", Anomaly: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("notices = %#v, want %#v", got, want)
	}
}

// A repo with none of the configured names says nothing at all — the oversize warning included.
// Silence is the common case, and a standing-content warning is not the place to break it.
func TestContextFileNoticesSilentWithoutFiles(t *testing.T) {
	got := notice.ContextFileNotices(domain.ContextFilesReport{StandingTokens: 9000, SystemShare: 4000})

	if len(got) != 0 {
		t.Errorf("notices = %#v, want none — an empty report composes nothing, warning included", got)
	}
}

// Every file loaded and the standing content fits its share: one plain notice, no anomaly.
func TestContextFileNoticesAllLoadedFitsBudget(t *testing.T) {
	got := notice.ContextFileNotices(domain.ContextFilesReport{
		Files:          []domain.ContextFileNote{{Name: "AGENTS.md", Bytes: 3174}},
		StandingTokens: 1000,
		SystemShare:    4000,
	})

	want := []notice.ContextNotice{{Text: "context: AGENTS.md (3.1 KiB)"}}
	if !slices.Equal(got, want) {
		t.Errorf("notices = %#v, want %#v", got, want)
	}
}
