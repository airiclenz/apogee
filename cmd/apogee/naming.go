package main

// The delegation-naming seam of the composition root (ADR 0068).
//
// A `sub_agent` call that named nothing leaves the run wearing the delegated task's first line,
// which is what the model wrote for the CHILD to read rather than for a human to scan. The engine
// says so and nothing more: it hands out a [domain.DelegationNaming] — the task, and whether this
// child is routed — and this file answers it with ONE out-of-band completion, exactly as title.go
// answers "name this session". Everything the call is made of — which endpoint, which model, which
// key, which effort wire dialect, which prompt, which timeout — is wiring the binary resolves, and
// that is the whole reason the seam exists (ADR 0031: the engine stays wire-silent).
//
// The one thing that differs from the session's namer is WHICH Upstream answers. The naming call
// goes to the CHILD's own (ADR 0068 decision 2): a routed child's grunt box is already warm for
// this run, so the machine doing the work answers the question about the work, and a session that
// routes its delegations to a cheap server does not hand the expensive one an extra call per spawn.

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/title"
)

// delegationNamer is the host's [domain.DelegationNamer]: one completion, built per call from the
// Upstream the child it names is running on.
//
// It is a POINTER type because of the gate. `auto-title:` is live-editable — the `/settings` pane
// and the file watcher over the same key — so the switch has to be a cell the host can flip under a
// namer the engine captured at construction, not a bool frozen into the Config. Everything else is
// read through a closure for titleWiring's reason: a `/server` switch, a rebind or a retarget
// between two spawns must carry the naming call with it, and reading at CALL time is the whole of
// that.
//
// It is called concurrently — a depth-0 fan-out names all its members at once (ADR 0068 decision
// 3) — so it holds no per-call state and the atomic is the only thing written after construction.
type delegationNamer struct {
	// session answers the Upstream an UNROUTED child's naming call is built on: the binding this
	// session is on right now, and the effort wire dialect its last beat observed. It is also the
	// fallback for a routed child whose target this Driver has never had (routed below), because a
	// naming call on the session's own server still produces the name.
	session func() (upstreamBinding, provider.EffortDialect)
	// routed answers the Sub-agent server's last-landed binding and dialect, and false when no
	// target has ever landed. nil is the same answer for a Driver that has no Sub-agent server at
	// all — an unattended Firing, a bench — and keeps the fallback one branch rather than two.
	routed func() (upstreamBinding, provider.EffortDialect, bool)
	// enabled is the live `auto-title:` switch, seeded from the launch Options and flipped by the
	// host when the pane or the file moves the key (tui.Options.OnAutoTitle). It is read at call
	// time, so a session that switches naming off stops paying for it from the very next spawn.
	enabled atomic.Bool
	// requestTimeout bounds one call; titleRequestTimeout in production, short in tests.
	requestTimeout time.Duration
}

// newDelegationNamer builds the namer over its two Upstream readers and the `auto-title:` value the
// launch resolved. routed may be nil for a Driver with no Sub-agent server of its own.
func newDelegationNamer(
	session func() (upstreamBinding, provider.EffortDialect),
	routed func() (upstreamBinding, provider.EffortDialect, bool),
	autoTitle bool,
) *delegationNamer {
	namer := &delegationNamer{session: session, routed: routed, requestTimeout: titleRequestTimeout}
	namer.enabled.Store(autoTitle)
	return namer
}

// newFiringNamer builds the namer an unattended run carries: one server, bound before the run
// starts and never moved, so both readers collapse to the constant the Firing was composed on and
// the gate is whatever `auto-title:` said at startup. A Firing has no live settings door to flip it
// through — there is no pane and no session — which is exactly why the value is read once here and
// the same atomic still answers at call time.
func newFiringNamer(binding upstreamBinding, dialect provider.EffortDialect, autoTitle bool) *delegationNamer {
	return newDelegationNamer(
		func() (upstreamBinding, provider.EffortDialect) { return binding, dialect },
		nil, autoTitle)
}

// setEnabled flips the `auto-title:` gate. It is the host's half of [tui.Options.OnAutoTitle], and
// it is safe from any goroutine: the pane applies from the renderer's Update loop while the engine
// may be naming a child from a dispatch goroutine.
func (n *delegationNamer) setEnabled(on bool) { n.enabled.Store(on) }

// NameDelegation answers the engine's ask with the model's RAW reply, exactly as
// [titleWiring.generate] does — sanitising is the caller's, so the generated name and everything
// else that reaches a status line pass through one pipeline (title.SanitizeTo, applied engine-side
// at internal/agent/subagent.go).
//
// With the gate off it answers ("", nil) and makes no request at all: an empty name is what the
// engine reads as "nothing usable came back", and the run keeps the task's first line. That is the
// same silence every other failure takes (ADR 0068 decision 8), which is why the gate needs no
// error of its own.
//
// The client is built per call and retries are OFF, both for titleWiring.generate's reasons: the
// binding moves under a running session, and a cosmetic call that failed must not re-POST twice
// onto a single-slot server ahead of the child's own next Turn. [title.ErrTruncated] is the one
// error synthesised here rather than passed on — a reply the server cut off at the token cap with
// nothing in it is a REPORTABLE cause rather than the generic "nothing came back".
//
// One honest looseness: the binding is read WHEN THE NAME IS ASKED FOR, not when the child was
// spawned. A `/server` switch or a `/sub-agents-server` retarget landing in the seconds between the
// two therefore names on the new box. The name describes the TASK rather than the server, so the
// answer is the same one either box would have given, and following the live binding is what keeps
// this call in step with every other out-of-band one the host makes.
func (n *delegationNamer) NameDelegation(ctx context.Context, req domain.DelegationNaming) (string, error) {
	if !n.enabled.Load() {
		return "", nil
	}

	binding, dialect := n.upstream(req.Routed)
	client := provider.NewClient(binding.Endpoint, binding.Model,
		provider.WithRequestTimeout(n.requestTimeout), provider.WithAPIKey(binding.APIKey),
		provider.WithMaxRetries(0))

	resp, err := respondDroppingThinkingOff(ctx, client, title.DelegationPrompt(req.Task, dialect))
	if err != nil {
		return "", err
	}
	if resp.FinishReason == finishReasonLength && strings.TrimSpace(resp.Content) == "" {
		return "", title.ErrTruncated
	}
	return resp.Content, nil
}

// upstream picks the Upstream this call is built on: the Sub-agent server for a routed child, this
// session's own for every other. A routed child whose target has never landed — the Driver has no
// Sub-agent server, or none has been resolved yet — falls back to the session's binding rather than
// dialling nothing, because a name from the wrong box still beats no name at all.
func (n *delegationNamer) upstream(routed bool) (upstreamBinding, provider.EffortDialect) {
	if routed && n.routed != nil {
		if binding, dialect, ok := n.routed(); ok {
			return binding, dialect
		}
	}
	return n.session()
}

// sessionNamingUpstream is the session half of the namer's readers, wired as a method so the two
// live holders it reads are looked up at CALL time: both are filled by the live-session assembly,
// which runs after the Config this namer is installed on is built (wire_boot.go).
func (w *rootWiring) sessionNamingUpstream() (upstreamBinding, provider.EffortDialect) {
	dialect := provider.EffortDialectNone
	if w.live != nil {
		dialect = w.live.observedDialect()
	}
	if w.holder == nil {
		return upstreamBinding{}, dialect
	}
	return w.holder.Binding(), dialect
}

// routedNamingUpstream is the Sub-agent server half: the dial facts of the target the routing beat
// last latched, and false while none has been (delegationWiring.routedBinding).
func (w *rootWiring) routedNamingUpstream() (upstreamBinding, provider.EffortDialect, bool) {
	if w.delegation == nil {
		return upstreamBinding{}, provider.EffortDialectNone, false
	}
	return w.delegation.routedBinding()
}
