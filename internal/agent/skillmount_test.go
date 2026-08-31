package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
)

// mountAddress matches every virtual-mount address a skill block can carry — the `files:` line's
// folder and every {{SKILL_DIR}} the body expanded — so the test reads the addresses out of the
// text the model is actually handed rather than off a fixture the code never emitted.
var mountAddress = regexp.MustCompile(`shipped:[A-Za-z0-9_./-]+`)

// The announced-surface invariant for a shipped skill (ADR 0065 §3): every `shipped:` address the
// injected block names — the files: line's folder and every {{SKILL_DIR}} the body expanded — is a
// path the read tools of the SAME Agent resolve. apogee may not hand the model an address its own
// tools refuse, so the addresses are scraped out of resolveSkillRefs' own output and driven through
// the registry that Agent built, with nothing spelled twice.
func TestShippedSkillAnnouncesOnlyReadableAddresses(t *testing.T) {
	provider := skills.NewProvider(skills.Sources{UseShippedSkills: true})
	a, _ := refAgentWithWindow(t, t.TempDir(), 200_000, func(cfg *domain.Config) {
		cfg.Skills = provider
		cfg.VirtualReadRoots = provider.VirtualReadRoots
	})

	block := a.resolveSkillRefs(1, []string{"debugging"}, 1<<20)
	if !strings.Contains(block, "files: shipped:debugging ") {
		t.Fatalf("the injected block names no shipped folder; got:\n%s", block)
	}

	addresses := uniqueSorted(mountAddress.FindAllString(block, -1))
	if len(addresses) < 2 {
		t.Fatalf("want the files: folder AND at least one bundled file address, got %v", addresses)
	}

	files := 0
	for _, address := range addresses {
		if address == "shipped:debugging" {
			assertToolReads(t, a, "list_dir", address, "SKILL.md")
			continue
		}
		assertToolReads(t, a, "read_file", address, "")
		files++
	}
	if files == 0 {
		t.Fatal("the body expanded no {{SKILL_DIR}} file address, so nothing proved a bundled file is readable")
	}
}

// assertToolReads drives one read tool at the announced address and fails when the result is an
// error result — the refusal an unreadable announced path produces. want, when non-empty, must
// appear in the text.
func assertToolReads(t *testing.T, a *Agent, tool, address, want string) {
	t.Helper()
	text := toolResultText(t, a, tool, address)
	if text == "" {
		t.Fatalf("%s refused the announced address %q", tool, address)
	}
	if want != "" && !strings.Contains(text, want) {
		t.Errorf("%s(%q) = %q, want it to mention %q", tool, address, text, want)
	}
}

// toolResultText runs tool against path and returns the result's text, or "" when the tool
// refused — the shape both assertions above are written against.
func toolResultText(t *testing.T, a *Agent, tool, path string) string {
	t.Helper()
	impl, ok := a.tools.Lookup(tool)
	if !ok {
		t.Fatalf("the agent's roster carries no %s", tool)
	}
	args, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := impl.Execute(context.Background(), domain.ToolCall{ID: "c1", Tool: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s returned a Go error: %v", tool, err)
	}
	if res.IsError {
		return ""
	}
	return res.Content
}

// uniqueSorted collapses the scraped addresses to a stable set, so a body naming one file twice
// does not make the assertion count it twice.
func uniqueSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
