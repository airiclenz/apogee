package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The start-up key-migration offer (ADR 0047)
// ----------------------------------------------------------------------------
//
// A `servers:` entry may carry its API key as a literal `api-key:` line, which means the key is
// sitting in a file the human edits, a watcher re-reads and a backup tool copies. Where the machine
// has a secret store apogee can BOTH write into and read back out of, the start-up offers, once per
// entry, to move the key there and leave the entry pointing at it with an ordinary `api-key-cmd:`
// line — after which the file holds a command instead of a secret and every read path is the
// resolver's usual one.
//
// It is an OFFER, not a migration. Nothing is written until the human takes the "move it" row: that
// is ADR 0035's deliberate-edit grain, one entry at a time, and it is why "never for this entry"
// exists at all — an answer that is not "yes" has to be able to be final, or the question becomes a
// recurring nag the human learns to dismiss without reading.
//
// The pane is the shared picker (picker.go) over three fixed rows, for the reason /schedule's two
// question panes are: this is a single-select question, and a second modal surface with its own keys
// would be one more thing to learn for one more question. What is different is that the pane comes
// up UNASKED, so it opens under a notice saying why (the prebound.go posture) — and that it can be
// asked more than once, which is what the queue on the overlay's own state is for: one entry per
// pane, the next opening where the last one closed, esc ending the whole round (every remaining
// entry is a "not now", which persists nothing and is re-offered at the next start-up).
//
// The renderer never sees a key. It is handed the entry NAMES and the store's human name, and each
// answer is one call to a seam the binary owns ([Options.MigrateKey], [Options.KeepPlaintextKey]) —
// the SaveHostAcknowledgement contract, for the same reasons: the file format, the store and the
// read-back verification are the binary's business, and a secret that never crosses into the
// renderer cannot be painted, logged or recorded by it.

// The three answers, in the order the rows are offered — least surprising first. The indexes are the
// offering's, so they are what acceptKeyMigration switches on.
const (
	keyMigrationMove = iota
	keyMigrationNotNow
	keyMigrationNever
)

// keyMigrationHint is the legend under the offer. It says "choose" rather than "switch" for the
// reason /schedule's panes do: taking a row answers a question, it does not move the session.
const keyMigrationHint = "type to filter · ↑/↓ select · ⏎ choose · esc close"

// openKeyMigration raises the first offer at construction, before the first frame is painted — the
// openPrebound seam, and for the same reason: the overlay is STATE, so it has to be set on the
// stored Model rather than returned as a Cmd.
//
// It gives way to anything already up. A pre-bound session opens the `/server` picker or the
// `/settings` pane here, and that ask is the more urgent one — a session with no server can do
// nothing at all, while a plaintext key is a file that has been readable for as long as it has
// existed. Standing a second unasked-for pane on top would also be the one thing this offer must
// never be: a stack of questions the human clears without reading. The offer costs nothing by
// waiting, because "not now" is exactly what it is: it comes back at the next start-up.
func (m *Model) openKeyMigration() {
	offer := m.opts.KeyMigration
	if offer.StoreName == "" || len(offer.Entries) == 0 || m.opts.MigrateKey == nil {
		return
	}
	if m.picker.open || m.settings.open {
		return
	}
	m.transcript.addNote(keyMigrationNotice(offer))
	// The queue is COPIED out of the Options: the overlay owns it from here (an answered entry is
	// dropped from the front), and the Options keep describing what this start-up found.
	m.picker = picker{
		open:      true,
		kind:      pickerKeyMigration,
		migration: append([]string(nil), offer.Entries...),
	}
}

// keyMigrationNotice is the line the unasked-for pane comes up under: what was found, and what the
// pane is about to offer to do about it. It names the entries rather than counting them, because
// which server's key is in the file is the fact that decides how the human answers.
func keyMigrationNotice(offer KeyMigrationOffer) string {
	return fmt.Sprintf("api-key: is stored in plain text in config.yaml for %s — %s can hold it instead",
		entryNameList(offer.Entries), stripEscapes(offer.StoreName))
}

// entryNameList spells a list of entry names for a sentence — "a", "a and b", "a, b and c" — each
// name sanitized, because a `servers:` name is text out of a file this package never wrote.
func entryNameList(names []string) string {
	clean := make([]string, 0, len(names))
	for _, name := range names {
		clean = append(clean, stripEscapes(name))
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	}
	return strings.Join(clean[:len(clean)-1], ", ") + " and " + clean[len(clean)-1]
}

// keyMigrationTitle names the entry THIS pane is asking about and the store it would move to, so a
// round of several offers can never be answered for the wrong server. The current entry is the head
// of the overlay's queue; a queue that emptied under the pane (which no path reaches) titles
// nothing rather than inventing a name.
func (m Model) keyMigrationTitle() string {
	if len(m.picker.migration) == 0 {
		return ""
	}
	return fmt.Sprintf("key for %s — move it into %s?",
		stripEscapes(m.picker.migration[0]), stripEscapes(m.opts.KeyMigration.StoreName))
}

// keyMigrationRows is the offering: three answers, each with a gloss saying what it does to the
// file, because every one of them is a decision about a secret and none of the three verbs says on
// its own what it costs. Two cells, so the glosses line up in one column.
func keyMigrationRows(storeName string) []popupRow {
	return []popupRow{
		{"move it", "— store it in " + stripEscapes(storeName) + " and point the entry at it"},
		{"not now", "— leave the file alone; the offer comes back at the next start-up"},
		{"never for this entry", "— record plaintext-key-ok: true and stop asking"},
	}
}

// acceptKeyMigration answers the open offer and moves to the next entry in the round.
//
// Each answer is one seam call and one note, and the note is the whole feedback: these seams are
// synchronous file work on a keypress the human is waiting on (the [SettingsHost.Write] contract), and an
// error is REPORTED rather than swallowed, because a migration that silently did not happen leaves
// the human believing their key has moved.
//
// The queue advances the same way whatever the answer was — including a failed one. The entry keeps
// its plaintext key, which is exactly the state "not now" leaves it in, so the next start-up asks
// again; re-asking about the same entry now would only put the same failure up twice.
func (m Model) acceptKeyMigration(choice int) (tea.Model, tea.Cmd) {
	if len(m.picker.migration) == 0 {
		return m, nil
	}
	entry, rest := m.picker.migration[0], m.picker.migration[1:]

	switch choice {
	case keyMigrationMove:
		m.transcript.addNote(m.migrateKeyNote(entry))
	case keyMigrationNotNow:
		m.transcript.addNote(fmt.Sprintf("%s keeps its key in config.yaml — the offer comes back at "+
			"the next start-up", stripEscapes(entry)))
	case keyMigrationNever:
		m.transcript.addNote(m.keepPlaintextKeyNote(entry))
	}

	// The whole overlay is replaced rather than edited, so nothing of the answered pane — the
	// filter above all — can survive into the next entry's question.
	m.picker = picker{}
	if len(rest) > 0 {
		m.picker = picker{open: true, kind: pickerKeyMigration, migration: rest}
	}
	m.layout()
	return m, nil
}

// migrateKeyNote performs the move and words what came of it. The seam does the whole of it — the
// store write, the read-back through the very command it is about to persist, and the rewrite —
// and reports the file it wrote, so the confirmation can name where to look.
func (m Model) migrateKeyNote(entry string) string {
	if m.opts.MigrateKey == nil {
		return "moving a key is not available in this build"
	}
	path, err := m.opts.MigrateKey(entry)
	if err != nil {
		return fmt.Sprintf("could not move %s's key: %s", stripEscapes(entry), stripEscapes(err.Error()))
	}
	return fmt.Sprintf("%s's key is in %s now — %s reads it back with api-key-cmd",
		stripEscapes(entry), stripEscapes(m.opts.KeyMigration.StoreName), stripEscapes(path))
}

// keepPlaintextKeyNote records the "never" answer and words it. The marker is a per-entry
// acknowledgement that this key stays in the file (ADR 0035), so the note says how to take it back:
// the line it wrote is the line to delete.
func (m Model) keepPlaintextKeyNote(entry string) string {
	if m.opts.KeepPlaintextKey == nil {
		return "recording that answer is not available in this build"
	}
	path, err := m.opts.KeepPlaintextKey(entry)
	if err != nil {
		return fmt.Sprintf("could not record that answer for %s: %s",
			stripEscapes(entry), stripEscapes(err.Error()))
	}
	return fmt.Sprintf("%s keeps its key in the file — %s records plaintext-key-ok: true, and "+
		"deleting that line asks again", stripEscapes(entry), stripEscapes(path))
}

// ----------------------------------------------------------------------------
// The start-up sub-agents-flag offer (ADR 0045)
// ----------------------------------------------------------------------------
//
// A `servers:` entry may still spell `sub-agents: true`, which is how ADR 0045 first marked the
// Sub-agent server. The root `sub-agents-server:` key replaced it, and nothing decodes the flag any
// more: the file reads without complaint and the delegations run on the session's own server, which
// is not what its owner wrote. Where the start-up finds that, it offers the one edit that fixes it —
// drop the flag line, name the entry in the root key, and re-point THIS session's delegations at it.
//
// It is the key migration's posture in every respect that is not the question: the pane comes up
// unasked under a notice saying why, it gives way to anything already open, esc is "not now" and
// persists nothing, and the renderer is handed entry NAMES and one seam. What it is not is a round —
// there is one question, because the answer is a single root key naming a single entry, and the
// entries beyond the first are named in the notice rather than asked about one at a time.

// The two answers, in the order the rows are offered. There is deliberately no "never" row: the flag
// is dead weight its owner removes once, so an answer that made the question permanent would
// preserve a line that does nothing (the ratified call).
const (
	subAgentsMigrationMove = iota
	subAgentsMigrationNotNow
)

// openSubAgentsMigration raises the offer at construction, after the key migration has had its turn —
// openKeyMigration's seam, and it gives way the same way and for the same reason. A plaintext key and
// an unbound session are both more urgent than a routing key that has been wrong since the flag was
// retired, and two unasked-for panes at once are a stack of questions the human clears without
// reading.
func (m *Model) openSubAgentsMigration() {
	if len(m.opts.SubAgentsMigration) == 0 || m.opts.MigrateSubAgentsServer == nil {
		return
	}
	if m.picker.open || m.settings.open {
		return
	}
	m.transcript.addNote(subAgentsMigrationNotice(m.opts.SubAgentsMigration))
	m.picker = picker{open: true, kind: pickerSubAgentsMigration}
}

// subAgentsMigrationNotice is the line the unasked-for pane comes up under: which entries carry the
// retired flag, and what has become of it. It names them rather than counting them, for the key
// migration's reason — which server the file meant is the fact that decides how this is answered.
func subAgentsMigrationNotice(entries []string) string {
	return fmt.Sprintf("sub-agents: true is retired on %s — the sub-agents-server: key names the "+
		"delegation target now", entryNameList(entries))
}

// subAgentsMigrationTitle names the entry the pane would write as the key. The offering's first name
// is that entry (Options.SubAgentsMigration), so a file that flagged two says which one it is about
// rather than leaving the human to guess which of the two the answer covers.
func (m Model) subAgentsMigrationTitle() string {
	if len(m.opts.SubAgentsMigration) == 0 {
		return ""
	}
	return fmt.Sprintf("delegate to %s — move the flag into sub-agents-server:?",
		stripEscapes(m.opts.SubAgentsMigration[0]))
}

// subAgentsMigrationRows is the offering: two answers, each with a gloss saying what it does to the
// file, because "move it" is one edit of a file the human owns and "not now" has to say that the
// question comes back. Two cells, so the glosses line up in one column.
func subAgentsMigrationRows(entries []string) []popupRow {
	name := ""
	if len(entries) > 0 {
		name = stripEscapes(entries[0])
	}
	return []popupRow{
		{"move it", "— write sub-agents-server: " + name + " and drop the retired flag"},
		{"not now", "— leave the file alone; the offer comes back at the next start-up"},
	}
}

// acceptSubAgentsMigration answers the offer. Each answer is one note and, for "move it", one seam
// call, exactly as the key migration's answers are: the seam is synchronous file work on a keypress
// the human is waiting on, and an error is REPORTED rather than swallowed.
//
// Either way the overlay closes for the rest of the session. There is nothing left to ask — the
// question was about one key, and "not now" is a whole answer that persists nothing and comes back
// at the next start-up.
func (m Model) acceptSubAgentsMigration(choice int) (tea.Model, tea.Cmd) {
	entries := m.opts.SubAgentsMigration
	if len(entries) == 0 {
		return m, nil
	}
	switch choice {
	case subAgentsMigrationMove:
		m.transcript.addNote(m.migrateSubAgentsNote(entries[0]))
	case subAgentsMigrationNotNow:
		m.transcript.addNote(fmt.Sprintf("%s keeps its retired sub-agents: flag — the offer comes "+
			"back at the next start-up", entryNameList(entries)))
	}
	m.picker = picker{}
	m.layout()
	return m, nil
}

// migrateSubAgentsNote performs the move and words what came of it. The seam does the whole of it —
// the rewrite, and the retarget that puts the answer in force in THIS session — and reports the file
// it wrote, so the confirmation can name where to look.
func (m Model) migrateSubAgentsNote(entry string) string {
	if m.opts.MigrateSubAgentsServer == nil {
		return "moving the sub-agents flag is not available in this build"
	}
	path, err := m.opts.MigrateSubAgentsServer(entry)
	if err != nil {
		return fmt.Sprintf("could not move %s's sub-agents flag: %s",
			stripEscapes(entry), stripEscapes(err.Error()))
	}
	return fmt.Sprintf("sub-agents-server: %s now — %s names it, and this session's delegations "+
		"run there", stripEscapes(entry), stripEscapes(path))
}
