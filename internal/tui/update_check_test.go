package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/airiclenz/apogee/internal/scheme"
)

// fakeUpdateHost answers the boot update check with a fixed result.
type fakeUpdateHost struct {
	latest, command string
	ok              bool
}

func (f fakeUpdateHost) CheckForUpdate(context.Context) (string, string, bool) {
	return f.latest, f.command, f.ok
}

// updateTestNotice is the version value a newer-release answer turns the box's version row into.
const updateTestNotice = "v0.24.11 → v0.25.0 · scoop update apogee"

// updateOpts is the display options with a release version and an update host wired.
func updateOpts(host UpdateHost) Options {
	opts := testOpts
	opts.BaseVersion = "v0.24.11"
	opts.Update = host
	return opts
}

// newerRelease is the answer the scoop-installed fake host gives.
var newerRelease = fakeUpdateHost{latest: "v0.25.0", command: "scoop update apogee", ok: true}

// firstUpdateCheck runs Init's Cmd and returns the updateCheckedMsg it yields, walking the batch the
// way firstRecall does: the check rides beside the other start-up Cmds and must still land.
func firstUpdateCheck(t *testing.T, cmd tea.Cmd) updateCheckedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init returned no Cmd — the update check never went out")
	}
	switch msg := cmd().(type) {
	case updateCheckedMsg:
		return msg
	case tea.BatchMsg:
		out := make(chan tea.Msg, len(msg))
		for _, c := range msg {
			go func() { out <- c() }()
		}
		deadline := time.After(5 * time.Second)
		for range msg {
			select {
			case landed := <-out:
				if checked, ok := landed.(updateCheckedMsg); ok {
					return checked
				}
			case <-deadline:
				t.Fatal("no updateCheckedMsg five seconds after Init — the check never landed")
			}
		}
		t.Fatal("Init's batch carried no updateCheckedMsg — the check never went out")
	default:
		t.Fatalf("Init's Cmd yielded %T, want the update check", msg)
	}
	return updateCheckedMsg{}
}

// A host reporting a newer release turns the start-up box's version value into
// "<current> → <latest> · <command>" in the rendered frame, through Init's own Cmd and the fold.
func TestUpdateCheckNamesTheUpgradeCommandOnTheVersionRow(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, updateOpts(newerRelease))
	if got := plain(m.View()); strings.Contains(got, "→") {
		t.Fatalf("the notice is on the box before the check landed:\n%s", got)
	}

	m = step(t, m, firstUpdateCheck(t, m.Init()))

	if got := plain(m.View()); !strings.Contains(got, updateTestNotice) {
		t.Errorf("frame does not carry the update notice %q:\n%s", updateTestNotice, got)
	}
	if got := m.transcript.entries[0].startup.Version; got != updateTestNotice {
		t.Errorf("start-up box version = %q, want %q", got, updateTestNotice)
	}
}

// No host, or a host with nothing to say, is the whole degrade: no Cmd goes out (nil host), and an
// ok=false answer leaves the box byte-identical to an unwired one — the frame the
// TestRenderStartupBox goldens pin.
func TestUpdateCheckWithNothingToSayLeavesTheBoxUnchanged(t *testing.T) {
	t.Parallel()
	if cmd := newTestModelEng(t, &fakeEngine{}, updateOpts(nil)).updateCheckCmd(); cmd != nil {
		t.Error("an unwired update host still issued a check Cmd")
	}

	tests := []struct {
		name string
		msg  updateCheckedMsg
	}{
		{"not ok", updateCheckedMsg{latest: "v0.25.0", command: "brew upgrade apogee", ok: false}},
		{"ok without a tag", updateCheckedMsg{command: "brew upgrade apogee", ok: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := newTestModelEng(t, &fakeEngine{}, updateOpts(nil))
			after := step(t, before, tt.msg)

			if got, want := plain(after.View()), plain(before.View()); got != want {
				t.Errorf("frame changed on %+v:\n got:\n%s\nwant:\n%s", tt.msg, got, want)
			}
			if got := after.transcript.entries[0].startup.Version; got != "v0.24.11" {
				t.Errorf("start-up box version = %q, want the bare BaseVersion", got)
			}
			th := newTheme(scheme.Default())
			bare := newStartupView(updateOpts(nil))
			if got, want := renderStartupBox(th, newStartupView(after.opts), 80), renderStartupBox(th, bare, 80); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("rendered box differs from the unwired one:\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// /clear re-seeds the start-up box from Options, and the notice is kept there, so the fresh box
// still names the upgrade.
func TestUpdateCheckNoticeSurvivesClear(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, updateOpts(newerRelease))
	m = step(t, m, updateCheckedMsg{latest: "v0.25.0", command: "scoop update apogee", ok: true})

	m, _ = typeCommand(t, m, "/clear")

	if got := plainTranscript(m); !strings.Contains(got, updateTestNotice) {
		t.Errorf("the re-seeded box dropped the update notice:\n%s", got)
	}
}

// Every restatement of the box reads the same Options, so the notice survives the cold start's first
// heartbeat (applyRebind's late seed), a /model pick through the same rebind, and a /server switch.
func TestUpdateCheckNoticeSurvivesRebindAndServerSwitch(t *testing.T) {
	t.Parallel()
	rb := &fakeRebind{}
	m := wireRebind(t, unbound(updateOpts(newerRelease)), &fakeHeartbeat{}, rb)
	m = step(t, m, updateCheckedMsg{latest: "v0.25.0", command: "scoop update apogee", ok: true})

	assertNotice := func(t *testing.T, m Model, also, when string) {
		t.Helper()
		got := plainTranscript(m)
		if !strings.Contains(got, updateTestNotice) {
			t.Errorf("%s dropped the update notice:\n%s", when, got)
		}
		if !strings.Contains(got, also) {
			t.Errorf("%s did not restate the box with %q:\n%s", when, also, got)
		}
	}

	m = foldBeatMsg(t, m, twoModelBeat())
	assertNotice(t, m, "test-model", "the first heartbeat's rebind")

	m, _ = typeCommand(t, m, "/model")
	m, _ = stepCmd(t, m, keyEnter())
	if len(rb.calls) == 0 || rb.calls[len(rb.calls)-1].model != "other-model" {
		t.Fatalf("rebind calls = %v, want the /model pick of other-model last", rb.calls)
	}
	assertNotice(t, m, "other-model", "a /model rebind")

	next, _ := m.foldServerSwitch("test-host", ServerSwitchResult{Endpoint: "http://elsewhere:1", HostAlias: "elsewhere"}, choiceRecord{})
	assertNotice(t, next.(Model), "elsewhere", "a server switch")
}
