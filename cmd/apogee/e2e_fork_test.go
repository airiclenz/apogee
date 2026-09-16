package main

// Session forking end to end: /fork cuts a child record off the live session at a chosen prompt,
// switches the TUI to it, and the child resumes on its own as a session whose history ends where
// the cut was made and that names the record it was cut from.
//
// Every seam below this is pinned one layer down — the cut in internal/agent (CutSession), the
// prefix and the fork points in internal/tui's transcript, the host's fork write and the carried
// parent id in cmd/apogee's session host, the browser's ⑂ tag in internal/tui's sessions. None of
// them proves the ROPE: a child whose engine State kept an Exchange its scrollback dropped would pass
// every one of them and still re-send the dropped prompt on its next request. So this run asks the
// question the way the model meets it — what did the upstream actually RECEIVE from the resumed
// child — and judges it from the END of the history, the way the cut was made.

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts fork.yaml answers and the wording each step is waited on.
const (
	forkPrompt1 = "Name the first stop."
	forkReply1  = "The first stop is the harbour."
	forkPrompt2 = "Name the second stop."
	forkReply2  = "The second stop is the lighthouse."
	forkPrompt3 = "Name the third stop."
	forkReply3  = "The third stop is the cliff path."
	// forkChildPrompt is sent AFTER the `--resume <child id>` relaunch. A resumed session issues no
	// request until it is asked something, so without it there is no wire to look at.
	forkChildPrompt = "Where did we stop?"
	forkChildReply  = "We stopped at the lighthouse."
	// forkTitle is the title the stub's naming turn answers: the parent's, the one the child starts
	// under, and the name the browser's ⑂ tag spells.
	forkTitle = "Fork journey"
	// forkPickerTitle is the fork picker's own title (internal/tui's forkPickerTitle), so a frame
	// carrying it is the picker OPEN over the session's prompts.
	forkPickerTitle = "fork this session — keep the history through which prompt"
	// forkTag is the browser's fork tag as sessionRowCells spells it: the glyph and the PARENT's
	// title. Only a record with Meta.ParentID set earns the glyph.
	forkTag = "⑂ " + forkTitle
)

// TestE2EForkKeepsThePrefixAndSurvivesAResume drives one session through three prompts, forks it at
// the second (keeping the history through that prompt and dropping the third), checks the switch on
// the frame and the parent's record on disk, quits, resumes the CHILD by id, and checks the history
// its first prompt puts on the wire — then opens /sessions to see the child tagged with its parent,
// after the child's own first Save has had its chance to lose the pointer.
func TestE2EForkKeepsThePrefixAndSurvivesAResume(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "fork"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	// Three Exchanges, each settled before the next is sent, so the fork picker has three points to
	// offer and the record the fork is cut from holds all three.
	for _, step := range []struct{ prompt, reply string }{
		{forkPrompt1, forkReply1}, {forkPrompt2, forkReply2}, {forkPrompt3, forkReply3},
	} {
		submit(drv, step.prompt)
		drv.WaitText(step.reply)
		drv.WaitQuiet(settled)
	}
	drv.WaitFor(func() bool { return len(sess.sessionRecords()) == 1 },
		tuitest.Awaiting("the parent's idle save to reach the session store"))
	store := session.NewStore(filepath.Join(sess.Home(), "sessions"))
	parentBefore := onlyRecord(t, store)

	// /fork, and the row that keeps the history through the SECOND prompt: the picker opens on its
	// first row, so one step down is the second point — the one whose cut drops the last prompt.
	submit(drv, "/fork")
	drv.WaitText(forkPickerTitle)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Down)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Enter)

	// The switch: the note names the parent, and the dropped Exchange is gone from the scrollback
	// the child replayed.
	drv.WaitText("forked from " + parentBefore.Meta.ID)
	drv.WaitQuiet(settled)
	frame := drv.Frame().String()
	if !strings.Contains(frame, "forked from "+parentBefore.Meta.ID+" — "+forkTitle) {
		t.Errorf("the frame carries no `forked from <parent id> — <title>` note:\n%s", frame)
	}
	if !strings.Contains(frame, forkReply2) {
		t.Errorf("the frame does not show the kept Exchange's reply %q:\n%s", forkReply2, frame)
	}
	for _, dropped := range []string{forkPrompt3, forkReply3} {
		if strings.Contains(frame, dropped) {
			t.Errorf("the frame still shows the dropped Exchange's %q:\n%s", dropped, frame)
		}
	}

	// The records: the parent's, unchanged but for the stamp its idle save re-applied, and the child
	// beside it pointing back at it.
	parentAfter, child := parentAndChild(t, store, parentBefore.Meta.ID)
	assertSameRecordButUpdatedAt(t, parentBefore, parentAfter)
	if child.Meta.ParentID != parentBefore.Meta.ID {
		t.Errorf("the child's Meta.ParentID = %q, want the parent's id %q", child.Meta.ParentID, parentBefore.Meta.ID)
	}
	if child.Meta.Title != forkTitle {
		t.Errorf("the child's Meta.Title = %q, want the parent's %q", child.Meta.Title, forkTitle)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the first run returned %v; want a clean quit", err)
	}

	// The resume, by the CHILD's id: the replay ends at the kept Exchange, and the child's first
	// prompt goes out over a history that ends there too.
	before := len(stub.Requests())
	next := sess.RelaunchWith("--resume", child.Meta.ID)
	waitIdle(next)
	next.WaitText(forkReply2)
	next.WaitQuiet(settled)
	if frame := next.Frame().String(); strings.Contains(frame, forkPrompt3) || strings.Contains(frame, forkReply3) {
		t.Errorf("the resumed child replays the dropped Exchange:\n%s", frame)
	}

	submit(next, forkChildPrompt)
	next.WaitText(forkChildReply)
	next.WaitQuiet(settled)

	req := taskListRequestCarrying(t, stub.Requests()[before:], forkChildPrompt)
	assertHistoryEndsAtTheKeptExchange(t, req)

	// The browser, after the child's own Save: the child's row carries the ⑂ tag with its parent's
	// title — the pointer survived the first Save a resumed child makes.
	submit(next, "/sessions")
	next.WaitText(forkTag)
	next.Press(tuitest.Esc)
	next.WaitGone(forkTag)

	stub.AssertConsumed(t)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the resumed run returned %v; want a clean quit", err)
	}
}

// assertHistoryEndsAtTheKeptExchange judges req's conversation from the END, the way the cut was
// made: the last message is the child's prompt, the message before it is the kept Exchange's final
// assistant reply, and no message anywhere carries the dropped Exchange's prompt or reply.
func assertHistoryEndsAtTheKeptExchange(t *testing.T, req stubllm.Request) {
	t.Helper()

	msgs := req.Messages
	if len(msgs) < 2 {
		t.Fatalf("the child's request %d carries %d messages; want a history under its prompt", req.N, len(msgs))
	}
	last, kept := msgs[len(msgs)-1], msgs[len(msgs)-2]
	if last.Role != "user" || !strings.Contains(last.Content, forkChildPrompt) {
		t.Errorf("the child's request %d ends on %s %q, want the child's prompt %q", req.N, last.Role,
			last.Content, forkChildPrompt)
	}
	if kept.Role != "assistant" || kept.Content != forkReply2 {
		t.Errorf("the child's request %d has %s %q before its prompt; want the kept Exchange's final "+
			"assistant message %q", req.N, kept.Role, kept.Content, forkReply2)
	}
	for i, msg := range msgs {
		for _, dropped := range []string{forkPrompt3, forkReply3} {
			if strings.Contains(msg.Content, dropped) {
				t.Errorf("the child's request %d carries the dropped Exchange's %q on message %d (%s)",
					req.N, dropped, i, msg.Role)
			}
		}
	}
}

// assertSameRecordButUpdatedAt fails t unless after is before's record with only Meta.UpdatedAt
// changed — the stamp every Save re-applies, and the one field the fork's queued idle save of the
// parent is allowed to touch.
func assertSameRecordButUpdatedAt(t *testing.T, before, after session.Record) {
	t.Helper()

	if after.Session.Version != before.Session.Version || !bytes.Equal(after.Session.State, before.Session.State) {
		t.Errorf("the parent's Session changed across the fork:\nbefore: %s\nafter:  %s",
			before.Session.State, after.Session.State)
	}
	if !bytes.Equal(after.Transcript, before.Transcript) {
		t.Errorf("the parent's Transcript changed across the fork:\nbefore: %s\nafter:  %s",
			before.Transcript, after.Transcript)
	}
	wantMeta, gotMeta := before.Meta, after.Meta
	wantMeta.UpdatedAt, gotMeta.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(gotMeta, wantMeta) {
		t.Errorf("the parent's Meta changed across the fork beyond UpdatedAt:\nbefore: %+v\nafter:  %+v",
			wantMeta, gotMeta)
	}
}

// onlyRecord loads the one record store holds, failing t when there is not exactly one.
func onlyRecord(t *testing.T, store *session.Store) session.Record {
	t.Helper()

	metas, err := store.List()
	if err != nil {
		t.Fatalf("list the session store: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("the store holds %d records, want the parent alone", len(metas))
	}
	return loadRecord(t, store, metas[0].ID)
}

// parentAndChild loads the parent by id and the ONE other record the store holds — the child the
// fork wrote — failing t when the store holds any other number of records.
func parentAndChild(t *testing.T, store *session.Store, parentID string) (parent, child session.Record) {
	t.Helper()

	metas, err := store.List()
	if err != nil {
		t.Fatalf("list the session store: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("the store holds %d records after the fork, want the parent and the child", len(metas))
	}
	childID := metas[0].ID
	if childID == parentID {
		childID = metas[1].ID
	}
	return loadRecord(t, store, parentID), loadRecord(t, store, childID)
}

// loadRecord loads one record by id, failing t on any error.
func loadRecord(t *testing.T, store *session.Store, id string) session.Record {
	t.Helper()

	rec, err := store.Load(id)
	if err != nil {
		t.Fatalf("load session record %s: %v", id, err)
	}
	return rec
}
