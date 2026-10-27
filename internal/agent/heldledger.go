package agent

import "slices"

// heldLedger is the one record of what the engine holds for the next user-role message and what it
// has taken into the open Exchange — the advise lanes' runs and the background workflows' finish
// notes alike. An entry is pending until a take moves it on: into delivered, when it reached the
// Exchange, or out of the ledger, when it did not. An aborted Exchange puts its delivered entries
// back ahead of pending (restore); an Exchange that closed with them kept forgets them (forget).
//
// The ledger is unlocked: its owner's mutex guards every call. The zero value is an empty ledger.
type heldLedger[T comparable] struct {
	// held is the entries no take has moved on yet, oldest first.
	held []T
	// delivered is the entries taken into the open Exchange, in the order they were taken.
	delivered []T
}

// hold adds entries behind the ones already pending.
func (l *heldLedger[T]) hold(entries ...T) {
	l.held = append(l.held, entries...)
}

// putBack returns entries a taker could not deliver to the front of pending, ahead of any held
// since, so their order is kept.
func (l *heldLedger[T]) putBack(entries ...T) {
	l.held = append(slices.Clone(entries), l.held...)
}

// pending returns a copy of the entries no take has moved on yet; nil when none is.
func (l *heldLedger[T]) pending() []T {
	return slices.Clone(l.held)
}

// take removes every entry of read from pending — the snapshot a taker read, so an entry held
// since stays pending — and records as delivered, in read order, the entries of read that delivers
// accepts. The rest of read leaves the ledger for good. delivers is called with the ledger's owner
// holding its lock, so it should only test membership.
func (l *heldLedger[T]) take(read []T, delivers func(T) bool) {
	l.held = slices.DeleteFunc(l.held, func(entry T) bool { return slices.Contains(read, entry) })
	for _, entry := range read {
		if delivers(entry) {
			l.delivered = append(l.delivered, entry)
		}
	}
}

// markDelivered records entries a taker handed to the Exchange about to open without their having
// been pending, so an abort holds them again.
func (l *heldLedger[T]) markDelivered(entries ...T) {
	l.delivered = append(l.delivered, entries...)
}

// restore puts the delivered entries back ahead of pending and empties delivered: the abort dropped
// every message that carried them, so the next Exchange takes them first.
func (l *heldLedger[T]) restore() {
	l.held = append(l.delivered, l.held...)
	l.delivered = nil
}

// forget empties delivered: the entries reached the conversation and are never delivered again.
func (l *heldLedger[T]) forget() {
	l.delivered = nil
}

// reset empties the ledger, pending and delivered both.
func (l *heldLedger[T]) reset() {
	l.held, l.delivered = nil, nil
}
