package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// GitActionsHost is the seam the footer's git actions run through: the user's own fetch, pull,
// push, sync, branch switch and rebase on the workspace (ADR 0108). Like [GitStatusHost], it keeps HOW git runs — the
// hardened runner, the no-prompt transport, the refused-repository rule — in the binary, which is
// why this package imports neither internal/gitexec nor internal/subprocess. cmd/apogee backs it;
// nil in [Options.GitActions] means no git actions (ADR 0054).
//
// Each call blocks until git lands, bounded only by a generous backstop, so it runs off the
// Update loop on a Cmd goroutine under the program context — Branches included. Each action
// returns a one-line result for the transcript note on success, or an error carrying git's own
// message. A refused repository runs no fetch, merge, push, checkout or rebase and comes back as
// an error; so does a networked call whose
// transport cannot run safely (an ssh command of the operator's own on Windows), which is never
// skipped in silence.
type GitActionsHost interface {
	// Fetch updates root's remote-tracking refs from its default remote.
	Fetch(ctx context.Context, root string) (string, error)
	// Pull fetches, then fast-forwards the branch to its upstream; a diverged branch is an error,
	// never a merge commit.
	Pull(ctx context.Context, root string) (string, error)
	// Push pushes the branch, never forced. A branch with no upstream is pushed to origin and
	// set to track it; without a remote named origin it is an error naming the remotes.
	Push(ctx context.Context, root string) (string, error)
	// Sync runs Pull, then Push, and stops at the first failure.
	Sync(ctx context.Context, root string) (string, error)
	// Branches lists root's local branch names by their bare names, the checked-out branch first
	// and the rest in name order; a detached HEAD puts none first.
	Branches(ctx context.Context, root string) ([]string, error)
	// Switch checks out the local branch name. A dirty tree, a name beginning with "-" and a
	// name with no local branch are each refused with an error, and nothing is checked out.
	Switch(ctx context.Context, root, name string) (string, error)
	// Rebase rebases the branch onto its upstream as last fetched; a dirty tree is refused. A
	// rebase that stops on a conflict is aborted, leaving the branch and tree as they were, and
	// comes back as an error naming the conflicting files.
	Rebase(ctx context.Context, root string) (string, error)
}

// gitAction names one footer git action — a row of the git actions picker (pickerGitActions) and,
// while one runs, which ([gitState.action]). It is an enum so the Model holds plain values only
// (ADR 0011); what each action calls on the host is [gitActionCall]'s to say.
type gitAction int

const (
	gitActionNone gitAction = iota
	gitActionFetch
	gitActionPull
	gitActionPush
	gitActionSync
	gitActionSwitch
	gitActionRebase
)

// word is the action's git verb — the word its notes open with ("git pull…").
func (a gitAction) word() string {
	switch a {
	case gitActionFetch:
		return "fetch"
	case gitActionPull:
		return "pull"
	case gitActionPush:
		return "push"
	case gitActionSync:
		return "sync"
	case gitActionSwitch:
		return "switch"
	case gitActionRebase:
		return "rebase"
	}
	return ""
}

// The git actions notes: the refusals /git and the footer click answer with, and the title the
// picker carries.
const (
	// noGitActionsNote answers /git with no git to act on: no actions host wired, no workspace, or
	// a workspace the status read found outside any work tree — or in a repository apogee refuses.
	noGitActionsNote = "no git here — git actions need a workspace in a git repository apogee can act on"
	// gitActionsBusyNote answers /git and the footer click while a Turn runs or a decision is
	// pending: the picker opens only at idle.
	gitActionsBusyNote = "git actions wait for the Turn to end"
	// gitActionRunningFormat answers them while a git action is still running; %s is its verb.
	gitActionRunningFormat = "git %s is still running — git actions wait for it to finish"
	// gitActionsTitle is the picker's title; a branch, where HEAD is on one, follows it.
	gitActionsTitle = "git actions"
	// gitBranchTitle is the branch picker's title; the branch HEAD is on, where it is on one,
	// follows it.
	gitBranchTitle = "switch branch"
	// gitNoOtherBranchNote answers "switch branch" when the listing found no local branch but the
	// one checked out.
	gitNoOtherBranchNote = "git switch: no other local branch to switch to"
	// gitBranchesGaveWayNote answers a listing that landed under another pane — one a command
	// queued behind it opened — so the branch picker does not open over it.
	gitBranchesGaveWayNote = "git switch: the branch list gave way to another pane — /git lists the branches again"
	// gitRebaseCancelledNote answers the rebase confirm's cancel row: nothing ran.
	gitRebaseCancelledNote = "git rebase cancelled — nothing ran"
)

// gitActionsWired reports whether the footer's git actions exist at all: a host is wired and there
// is a workspace to act on.
func (m Model) gitActionsWired() bool {
	return m.opts.GitActions != nil && m.opts.Workspace != ""
}

// gitActionsRefusal is the one gate /git and the footer click share: the note the opener answers
// with instead of the picker, and false where the picker may open. With no git to act on it says
// so; while a Turn runs or a decision is pending, and while a git action is still running, the
// actions wait.
func (m Model) gitActionsRefusal() (string, bool) {
	switch {
	case !m.gitActionsWired() || !m.git.hasSnap:
		return noGitActionsNote, true
	case m.busy():
		return gitActionsBusyNote, true
	case m.holds.has(holdGitAction):
		return fmt.Sprintf(gitActionRunningFormat, m.git.action.word()), true
	}
	return "", false
}

// openGitActions opens the git actions picker, or notes why it does not open
// ([Model.gitActionsRefusal]). It is the one opener of /git and of a click on the footer's git
// segment ([Model.handleFooterGitClick]).
func (m Model) openGitActions() (Model, tea.Cmd) {
	if note, refused := m.gitActionsRefusal(); refused {
		m.transcript.addNote(note)
		return m, nil
	}
	m.picker = picker{open: true, kind: pickerGitActions}
	return m, nil
}

// runGitCommand is /git's run: the git actions picker, the footer click's keyboard reach. The row is
// whileRunning so a /git typed mid-Turn answers at once with the wait note rather than being queued
// silently for the next idle.
func (m Model) runGitCommand() (tea.Model, tea.Cmd) {
	return m.openGitActions()
}

// gitActionRow is one row the git actions picker offers: the action and the words it is shown with.
type gitActionRow struct {
	action gitAction
	label  string
	gloss  string
}

// gitActionsOffered is the picker's offering, derived from the last good snapshot: fetch and push
// always, pull, sync and rebase only while the branch has an upstream to act against, and switch
// branch. It is derived per frame and re-derived at accept, so a snapshot that lands under the open
// pane moves the rows and the accept reads the same list it drew.
func (m Model) gitActionsOffered() []gitActionRow {
	upstream := stripEscapes(m.git.snap.Upstream)
	hasUpstream := m.git.hasSnap && upstream != ""
	push := gitActionRow{gitActionPush, "push", "push to origin and track it — never forced"}
	if hasUpstream {
		push.gloss = "push to " + upstream + " — never forced"
	}
	rows := []gitActionRow{{gitActionFetch, "fetch", "update the remote-tracking refs"}}
	if hasUpstream {
		rows = append(rows, gitActionRow{gitActionPull, "pull", "fast-forward to " + upstream + " — never a merge commit"})
	}
	rows = append(rows, push)
	if hasUpstream {
		rows = append(rows, gitActionRow{gitActionSync, "sync", "pull, then push — stops at the first failure"})
	}
	rows = append(rows, gitActionRow{gitActionSwitch, "switch branch", "check out another local branch"})
	if hasUpstream {
		rows = append(rows, gitActionRow{gitActionRebase, "rebase onto upstream", "rebase onto " + upstream + " as last fetched"})
	}
	return rows
}

// gitActionRows paints [Model.gitActionsOffered] as the picker's rows: the label, then its gloss.
func (m Model) gitActionRows() []popupRow {
	offered := m.gitActionsOffered()
	rows := make([]popupRow, 0, len(offered))
	for _, row := range offered {
		rows = append(rows, popupRow{row.label, "— " + row.gloss})
	}
	return rows
}

// gitActionsPickerTitle names the picker: "git actions", and the branch HEAD is on where it is on
// one. The branch is the repository's text, so it is stripped like every other footer fact.
func (m Model) gitActionsPickerTitle() string {
	if branch := stripEscapes(m.git.snap.Branch); m.git.hasSnap && branch != "" {
		return gitActionsTitle + " — " + branch
	}
	return gitActionsTitle
}

// acceptGitAction takes the picker's highlighted row, named by its index into the OFFERING
// (acceptPicker resolves the filter first), re-derived rather than trusted from the frame that drew
// it. Fetch, pull, push and sync start at once ([Model.startGitAction]); switch branch first lists
// the local branches off the loop ([Model.startGitBranches]), and rebase first asks a latched
// confirm ([Model.raiseGitRebaseConfirm]). An index naming no row closes the overlay and moves
// nothing. The gate is asked again: the overlay may have stood open across a fold that changed the
// answer.
func (m Model) acceptGitAction(offered int) (tea.Model, tea.Cmd) {
	rows := m.gitActionsOffered()
	m.picker = picker{}
	if offered < 0 || offered >= len(rows) {
		return m, nil
	}
	if note, refused := m.gitActionsRefusal(); refused {
		return m.pickerNote(note)
	}
	switch action := rows[offered].action; action {
	case gitActionSwitch:
		return m.startGitBranches()
	case gitActionRebase:
		return m, m.raiseGitRebaseConfirm()
	default:
		return m.startGitAction(action)
	}
}

// startGitBranches is switch branch's first step: it lists the local branches off the loop under
// the git action hold, waiting out an in-flight background fetch as every action does, and the
// branch picker opens on the listing's landing ([Model.foldGitActionDone]) — never at the click,
// because the listing is a git process and Update never waits on one. It notes nothing: the
// listing changes nothing, and the switch the picker leads to notes its own start.
func (m Model) startGitBranches() (tea.Model, tea.Cmd) {
	m.git.branch = ""
	return m.issueGitAction(gitActionSwitch)
}

// offerGitBranches opens the branch picker over the names the listing landed, the branch HEAD is on
// left out — a row that switches nothing is not a choice (offeredModels' posture). With no other
// branch, or with another pane up or the gate shut by the time the listing lands, it notes why
// instead. The names are the repository's text, kept raw for the switch and stripped where painted
// ([gitBranchRows]).
func (m Model) offerGitBranches(names []string) Model {
	current := ""
	if m.git.hasSnap {
		current = m.git.snap.Branch
	}
	offered := make([]string, 0, len(names))
	for _, name := range names {
		if name != current {
			offered = append(offered, name)
		}
	}
	if len(offered) == 0 {
		m.transcript.addNote(gitNoOtherBranchNote)
		return m
	}
	if m.picker.open || m.settings.open {
		m.transcript.addNote(gitBranchesGaveWayNote)
		return m
	}
	if note, refused := m.gitActionsRefusal(); refused {
		m.transcript.addNote(note)
		return m
	}
	m.picker = picker{open: true, kind: pickerGitBranch, branches: offered}
	return m
}

// gitBranchRows paints the branch picker's rows: one cell per branch, its name stripped of escapes.
func (m Model) gitBranchRows() []popupRow {
	rows := make([]popupRow, 0, len(m.picker.branches))
	for _, name := range m.picker.branches {
		rows = append(rows, popupRow{stripEscapes(name)})
	}
	return rows
}

// gitBranchPickerTitle names the branch picker: "switch branch", and the branch it switches from
// where HEAD is on one.
func (m Model) gitBranchPickerTitle() string {
	if branch := stripEscapes(m.git.snap.Branch); m.git.hasSnap && branch != "" {
		return gitBranchTitle + " — from " + branch
	}
	return gitBranchTitle
}

// acceptGitBranch takes the branch picker's highlighted row, named by its index into the names the
// picker carries (acceptPicker resolves the filter first), and switches to it. The gate is asked
// again, for acceptGitAction's reason; an index naming no row closes the overlay and moves nothing.
func (m Model) acceptGitBranch(offered int) (tea.Model, tea.Cmd) {
	names := m.picker.branches
	m.picker = picker{}
	if offered < 0 || offered >= len(names) {
		return m, nil
	}
	if note, refused := m.gitActionsRefusal(); refused {
		return m.pickerNote(note)
	}
	m.git.branch = names[offered]
	return m.startGitAction(gitActionSwitch)
}

// The rebase confirm's answers, in row order.
const (
	gitRebaseGo = iota
	gitRebaseCancel
)

// raiseGitRebaseConfirm puts up the rebase confirm: a pane that asks before a rebase rewrites the
// branch. It is latched as [Model.raiseAdoption]'s pane is — closed until its drain marker (or the
// backstop) arms it, so a ⏎ already in flight from the git actions picker answers nothing — and it
// opens on cancel, so a stray ⏎ once armed runs nothing either.
func (m *Model) raiseGitRebaseConfirm() tea.Cmd {
	m.picker = picker{open: true, kind: pickerGitRebase, latched: true}
	m.picker.seat(gitRebaseCancel, len(m.gitRebaseRows()))
	return m.openDecisionLatch()
}

// gitRebaseRows is the rebase confirm's offering, in answer order: rebase, then cancel.
func (m Model) gitRebaseRows() []popupRow {
	onto := "its upstream"
	if upstream := stripEscapes(m.git.snap.Upstream); m.git.hasSnap && upstream != "" {
		onto = upstream
	}
	return []popupRow{
		{"rebase", "— onto " + onto + " as last fetched; a conflict is aborted and changes nothing"},
		{"cancel", "— run nothing"},
	}
}

// gitRebasePickerTitle asks the confirm's question, naming the branch where HEAD is on one.
func (m Model) gitRebasePickerTitle() string {
	if branch := stripEscapes(m.git.snap.Branch); m.git.hasSnap && branch != "" {
		return "rebase " + branch + " onto its upstream?"
	}
	return "rebase onto the upstream?"
}

// acceptGitRebase answers the rebase confirm: rebase starts the rebase, the gate asked again for
// acceptGitAction's reason; cancel notes that nothing ran.
func (m Model) acceptGitRebase(choice int) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if choice != gitRebaseGo {
		return m.pickerNote(gitRebaseCancelledNote)
	}
	if note, refused := m.gitActionsRefusal(); refused {
		return m.pickerNote(note)
	}
	return m.startGitAction(gitActionRebase)
}

// gitActionDoneMsg carries one landed footer git action into the Update loop: the host's one-line
// result, or its error. listing marks switch branch's first step, whose landing carries the local
// branch names rather than a result.
type gitActionDoneMsg struct {
	action   gitAction
	result   string
	err      error
	listing  bool
	branches []string
}

// startGitAction takes the git action hold and notes `git <verb>…`, then issues the action off the
// loop — or, while the background fetch is in flight, leaves it waiting for that fetch to land
// ([Model.foldGitFetch]), so two git processes never run on the repository at once. From here until
// the action folds, a prompt, a wake, a workflow resume and an idle-only command wait
// (holdGitAction), no background fetch is issued ([Model.foldGitFetchTick]), and a quit defers its
// exit ([Model.quitSession]).
func (m Model) startGitAction(action gitAction) (tea.Model, tea.Cmd) {
	m.transcript.addNote("git " + action.word() + "…")
	return m.issueGitAction(action)
}

// issueGitAction takes the git action hold for action and issues it off the loop, or leaves it
// waiting for the in-flight background fetch — the half of [Model.startGitAction] the branch
// listing shares, which notes nothing.
func (m Model) issueGitAction(action gitAction) (tea.Model, tea.Cmd) {
	m.holds.hold(holdGitAction)
	m.git.action = action
	if m.git.fetchInFlight {
		m.git.actionWaits = true
		return m, nil
	}
	return m, m.gitActionCmd()
}

// issueWaitingGitAction issues the git action that was waiting for the background fetch, now that
// it has landed, and is nil when none waits. A quit asked for meanwhile wins: the action that never
// started is dropped with its hold, and the exit it held fires once the write queue has drained
// ([Model.pumpOrQuit]). It takes a pointer for [Model.refreshGit]'s reason.
func (m *Model) issueWaitingGitAction() tea.Cmd {
	if !m.git.actionWaits {
		return nil
	}
	m.git.actionWaits = false
	if m.quitting {
		m.holds.release(holdGitAction)
		m.git.action, m.git.branch = gitActionNone, ""
		if m.writeBusy {
			return nil
		}
		return m.pumpOrQuit()
	}
	return m.gitActionCmd()
}

// gitActionCall is the host call one action runs — a switch to branch — and nil for an action the
// picker does not run through this path.
func gitActionCall(host GitActionsHost, action gitAction, branch string) func(context.Context, string) (string, error) {
	switch action {
	case gitActionFetch:
		return host.Fetch
	case gitActionPull:
		return host.Pull
	case gitActionPush:
		return host.Push
	case gitActionSync:
		return host.Sync
	case gitActionSwitch:
		return func(ctx context.Context, root string) (string, error) { return host.Switch(ctx, root, branch) }
	case gitActionRebase:
		return host.Rebase
	}
	return nil
}

// gitActionCmd runs the current action off the Update loop under the program context, so a shutdown
// cancels a git child still running; the host bounds it only by its generous backstop. A switch
// with no branch chosen yet is its first step, the branch listing.
func (m Model) gitActionCmd() tea.Cmd {
	host, action, ctx, root := m.opts.GitActions, m.git.action, m.parent, m.opts.Workspace
	if action == gitActionSwitch && m.git.branch == "" {
		return func() tea.Msg {
			names, err := host.Branches(ctx, root)
			return gitActionDoneMsg{action: action, err: err, listing: true, branches: names}
		}
	}
	call := gitActionCall(host, action, m.git.branch)
	return func() tea.Msg {
		if call == nil {
			return gitActionDoneMsg{action: action, err: fmt.Errorf("git %s: not an action this picker runs", action.word())}
		}
		result, err := call(ctx, root)
		return gitActionDoneMsg{action: action, result: result, err: err}
	}
}

// foldGitActionDone folds one landed git action: it releases the hold, notes the result — or the
// error, git's own message — and asks for a status read so the footer's counts show what the action
// did. A quit that waited for the action exits from here once the write queue has drained too
// ([Model.pumpOrQuit]); otherwise the engine is the loop's again, so a stashed rebind is applied
// and the commands queued behind the action run. The branch listing changed nothing, so it notes
// only its failure and asks for no read; once the queued commands have run, its names open the
// branch picker ([Model.offerGitBranches]). A Msg with no action held is inert.
func (m Model) foldGitActionDone(msg gitActionDoneMsg) (tea.Model, tea.Cmd) {
	if !m.holds.has(holdGitAction) {
		return m, nil
	}
	m.holds.release(holdGitAction)
	m.git.action, m.git.branch, m.git.actionWaits = gitActionNone, "", false
	if !msg.listing || msg.err != nil {
		m.transcript.addNote(gitActionNote(msg))
	}
	if m.quitting {
		if m.writeBusy {
			return m, nil
		}
		return m, m.pumpOrQuit()
	}
	var refresh tea.Cmd
	if !msg.listing {
		refresh = m.refreshGit()
	}
	m.releaseEngine()
	next, cmd := m.runDeferredCommands()
	if msg.listing && msg.err == nil {
		next = next.offerGitBranches(msg.branches)
	}
	return next, tea.Batch(refresh, cmd)
}

// gitActionNote words a landed action for the transcript: `git <result>` on success — the host's
// result already opens with the verb ("pull: already up to date") — and `git <verb> failed — <why>`
// on failure, with a leading `git <verb>: ` the host's error carries dropped so the verb is said
// once.
func gitActionNote(msg gitActionDoneMsg) string {
	if msg.err == nil {
		return "git " + msg.result
	}
	verb := msg.action.word()
	return "git " + verb + " failed — " + strings.TrimPrefix(msg.err.Error(), "git "+verb+": ")
}
