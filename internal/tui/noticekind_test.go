package tui

import (
	"os"
	"strings"
	"testing"
)

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

// The three docs that list the loud start-up notice kinds, as paths from this package's directory
// (where `go test` runs): the repo layout is fixed, so a missing file fails rather than skips.
const (
	loudDocLayout   = "../../layout.md"
	loudDocCommands = "../../docs/manual/commands.md"
	loudDocGlossary = "../../.capcom/knowledge/glossary/glossary-identity/session-log.md"
)

// loudKindPhrases is, per loud kind and per doc, the phrases that doc names the kind with. The docs
// word several kinds differently, so each doc keeps its own phrases; a kind whose notice has two
// texts (kindContextAnomaly: an unreadable context file, standing content over its share) carries a
// phrase for each.
var loudKindPhrases = map[NoticeKind]map[string][]string{
	NoticeAutoUnconfined: {
		loudDocLayout:   {"Auto running unconfined"},
		loudDocCommands: {"Auto running unconfined"},
		loudDocGlossary: {"Auto unconfined"},
	},
	NoticeAutoDegraded: {
		loudDocLayout:   {"degraded Auto"},
		loudDocCommands: {"Auto degraded on a host that cannot fence writes"},
		loudDocGlossary: {"degraded Auto"},
	},
	NoticeShareGroupConfig: {
		loudDocLayout:   {"group-readable config under `sessions.share-group`"},
		loudDocCommands: {"a config file other members of your `sessions.share-group` can read"},
		loudDocGlossary: {"group-readable config under `sessions.share-group`"},
	},
	NoticeMCPTruncated: {
		loudDocLayout:   {"an MCP server's tool list cut short"},
		loudDocCommands: {"an MCP server whose tool list was cut short"},
		loudDocGlossary: {"an MCP server's tool list cut short"},
	},
	NoticeMCPSkipped: {
		loudDocLayout:   {"a skipped MCP server"},
		loudDocCommands: {"a skipped MCP server"},
		loudDocGlossary: {"a skipped MCP server"},
	},
	NoticeRosterUnapplied: {
		loudDocLayout:   {"a profile roster that was not applied"},
		loudDocCommands: {"a profile roster that could not be applied"},
		loudDocGlossary: {"an unapplied profile roster"},
	},
	kindContextAnomaly: {
		loudDocLayout:   {"an unreadable context file", "standing content over its Budget share"},
		loudDocCommands: {"an unreadable context file", "standing content over its Budget share"},
		loudDocGlossary: {"an unreadable context file", "standing content over its Budget share"},
	},
	kindColorScheme: {
		loudDocLayout:   {"colour-scheme warning"},
		loudDocCommands: {"colour-scheme warning"},
		loudDocGlossary: {"colour-scheme warning"},
	},
	kindRebindFailed: {
		loudDocLayout:   {"a refused rebind"},
		loudDocCommands: {"a rebind the server refused"},
		loudDocGlossary: {"refused rebind"},
	},
	kindWindowUnknown: {
		loudDocLayout:   {"an unknown context window"},
		loudDocCommands: {"an unknown context window"},
		loudDocGlossary: {"an unknown context window"},
	},
	kindEffortCleared: {
		loudDocLayout:   {"an effort level the new model dropped"},
		loudDocCommands: {"an effort level the new model dropped"},
		loudDocGlossary: {"a dropped effort level"},
	},
	kindOffline: {
		loudDocLayout:   {"the server going offline"},
		loudDocCommands: {"the server going offline"},
		loudDocGlossary: {"the server going offline"},
	},
	kindOnline: {
		loudDocLayout:   {"or coming back"},
		loudDocCommands: {"or coming back"},
		loudDocGlossary: {"or coming back"},
	},
	kindRouting: {
		loudDocLayout:   {"a routing notice"},
		loudDocCommands: {"`sub-agents: …` routing notice"},
		loudDocGlossary: {"a routing notice"},
	},
}

// TestLoudKindsNamedInDocs pins the docs' lists of the notices that keep their own line to the one
// table: every loud kind in [noticeKinds] (the reserved zero kind aside) is named in each doc, every
// kind the phrases name is still loud, and each doc points at the file the table lives in. The docs
// are hard-wrapped, so each is matched with its whitespace folded to single spaces.
func TestLoudKindsNamedInDocs(t *testing.T) {
	t.Parallel()

	docs := map[string]string{}
	for _, path := range []string{loudDocLayout, loudDocCommands, loudDocGlossary} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		docs[path] = strings.Join(strings.Fields(string(raw)), " ")
		if !strings.Contains(docs[path], "`internal/tui/noticekind.go`") {
			t.Errorf("%s does not say the notice kinds are defined in `internal/tui/noticekind.go`", path)
		}
	}

	for kind, row := range noticeKinds {
		if kind == kindUnset || row.class != classLoud {
			continue
		}
		if _, ok := loudKindPhrases[kind]; !ok {
			t.Errorf("loud kind %d has no docs phrases — name it in each doc and add it to loudKindPhrases", kind)
		}
	}

	for kind, perDoc := range loudKindPhrases {
		if row, ok := noticeKinds[kind]; !ok || row.class != classLoud || kind == kindUnset {
			t.Errorf("kind %d has docs phrases but is not a declared loud kind — drop its phrases from the docs and the map", kind)
		}
		for path, doc := range docs {
			phrases := perDoc[path]
			if len(phrases) == 0 {
				t.Errorf("kind %d has no phrase for %s", kind, path)
			}
			for _, phrase := range phrases {
				if !strings.Contains(doc, phrase) {
					t.Errorf("%s no longer names loud kind %d as %q", path, kind, phrase)
				}
			}
		}
	}
}
