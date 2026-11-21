package tui

import "testing"

// TestNoticeKindClasses pins every declared kind's class and source as the one table declares them,
// the reserved zero kind among them, read through the table lookup.
func TestNoticeKindClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   NoticeKind
		class  noticeClass
		source noticeSource
	}{
		{"the zero kind is loud", kindUnset, classLoud, sourceBoundary},
		{"auto unconfined", NoticeAutoUnconfined, classLoud, sourceBoundary},
		{"auto degraded", NoticeAutoDegraded, classLoud, sourceBoundary},
		{"fence residual", NoticeFenceResidual, classRoutine, sourceBoundary},
		{"share group config", NoticeShareGroupConfig, classLoud, sourceBoundary},
		{"mcp truncated", NoticeMCPTruncated, classLoud, sourceBoundary},
		{"mcp skipped", NoticeMCPSkipped, classLoud, sourceBoundary},
		{"model binding", NoticeModelBinding, classRoutine, sourceBeat},
		{"trusted hint", NoticeTrustedHint, classRoutine, sourceBeat},
		{"roster unapplied", NoticeRosterUnapplied, classLoud, sourceBeat},
		{"context loaded", kindContextLoaded, classRoutine, sourceAmbient},
		{"context anomaly", kindContextAnomaly, classLoud, sourceAmbient},
		{"colour scheme", kindColorScheme, classLoud, sourceAmbient},
		{"git refusal", kindGitRefusal, classRoutine, sourceAmbient},
		{"rebind change", kindRebindChange, classRoutine, sourceBeat},
		{"rebind failed", kindRebindFailed, classLoud, sourceBeat},
		{"window unknown", kindWindowUnknown, classLoud, sourceBeat},
		{"effort cleared", kindEffortCleared, classLoud, sourceBeat},
		{"mandatory effort", kindMandatoryEffort, classRoutine, sourceBeat},
		{"offline", kindOffline, classLoud, sourceBeat},
		{"online", kindOnline, classLoud, sourceBeat},
		{"routing", kindRouting, classLoud, sourceAmbient},
	}
	if len(tests) != len(noticeKinds) {
		t.Fatalf("the test pins %d kinds, the table declares %d — pin the new kind here", len(tests), len(noticeKinds))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := noticeKinds[tt.kind]; !ok {
				t.Fatalf("kind %d has no row in noticeKinds", tt.kind)
			}
			row := kindRow(tt.kind)
			if row.class != tt.class {
				t.Errorf("class = %d, want %d", row.class, tt.class)
			}
			if got := classOf(tt.kind); got != tt.class {
				t.Errorf("classOf = %d, want %d", got, tt.class)
			}
			if row.source != tt.source {
				t.Errorf("source = %d, want %d", row.source, tt.source)
			}
		})
	}
}

// TestNoticeKindClasses_UndeclaredKindReadsAsTheZeroKind pins the lookup's fallback: a kind with no
// row is loud, like a notice whose producer named none.
func TestNoticeKindClasses_UndeclaredKindReadsAsTheZeroKind(t *testing.T) {
	t.Parallel()

	if got, want := kindRow(NoticeKind(-1)), noticeKinds[kindUnset]; got != want {
		t.Errorf("kindRow(undeclared) = %+v, want the zero kind's %+v", got, want)
	}
}

// TestNoticeKindClasses_NoticeReadsItsKindsClass pins that a Notice takes its class from the
// table: a loud kind's notice is loud, a routine kind's is not, and one naming no kind is loud.
func TestNoticeKindClasses_NoticeReadsItsKindsClass(t *testing.T) {
	t.Parallel()

	if !(Notice{Kind: NoticeAutoUnconfined}).Loud() {
		t.Error("a loud kind's notice is not loud")
	}
	if (Notice{Kind: NoticeFenceResidual}).Loud() {
		t.Error("a routine kind's notice is loud")
	}
	if !(Notice{}).Loud() {
		t.Error("a notice naming no kind is routine, want it loud")
	}
}
