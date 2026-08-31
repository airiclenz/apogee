// Package domain is the ubiquitous language (CONTEXT.md) rendered as Go: every
// type, interface, enum, sentinel error, and hook working-value in Apogee's public
// surface, plus the pure logic intrinsic to those types (the Mechanism registry's
// ordering-cycle detection, ConfinementCaps.AutoEligible, the Session envelope and
// its versioning).
//
// It is the foundational layer of the package layout decided in ADR 0010: the engine
// (internal/agent), the provider (internal/provider), and the platform backends
// (internal/platform) all import domain for these types and never import the root
// apogee package; the root facade re-exports the public ones as aliases. Domain
// depends only on the standard library, so the language has a dependency-free home
// and the invariant "internal/* never imports root" holds at the bottom of the graph.
//
// Naming: "domain", not "core" — the retired "Apogee Core" library term
// (CONTEXT.md "Retired terms") is unrelated to this internal package.
//
// # The files, one line each
//
// Twenty-four files, grouped by which part of the language each one carries.
//
// The construction surface and the session envelope. config.go is Config, the whole
// construction surface (ADR 0001), plus the mode ladder it opens on — Mode, ParseMode,
// NextMode, TighterMode — the UserInput a Driver submits, the StepResult it gets back, the
// SkillResolver seam, and the ModelProfile with its three axes: tool-call format, thinking
// channel, and the ToolRosterDelta a profile equips a model with (ADR 0057). uivocab.go is the presentation vocabulary a Driver is CONFIGURED
// with — the spinner styles with their parse, the cursor-shape NAMES with their validator, and
// the PreboundStart a session begins unbound with — homed here for ParseMode's reason, so the
// config layer validates a spelling without importing a renderer (ADR 0043). session.go is the
// Session envelope, its SessionVersion and DecodeSession; the opaque State payload inside it
// belongs to the engine. errors.go is the sentinel errors the root facade re-exports as vars,
// each carrying the condition that raises it.
//
// The host delegates. approval.go is Approver with its request and decision pair — the
// human-in-the-loop gate on one tool call (ADR 0004, ADR 0012). ask.go is Asker, the
// free-text question delegate ask_user routes to, plus the sub-agent task and name context
// carriers a prompt is labelled with. present.go is Presenter and the presentation ladder's
// request, outcome and method types (ADR 0019). promptslot.go is PromptSlot — the ONE prompt
// a Driver draws, handed to one asking Agent at a time so a depth-0 fan-out cannot orphan a
// human's reply (ADR 0039). events.go is EventSink and every typed Event the loop emits:
// tokens and reasoning, messages, tool calls and results, approvals, Mechanism fires, usage,
// audit.
//
// The loop's working values. hooks.go is the substrate a hook actually touches — Message and
// its wire JSON, Role, ToolDef, Budget, the method-only Request / Response / Conversation,
// and the LoopView / ConversationView interfaces. hookview.go is the unexported read-only
// views backing those interfaces, so a hook reading loop state can never mutate it.
// exchange.go derives the current Exchange's boundary from the conversation instead of
// caching it, skipping the Interjection that is deliberately not an opening. budget.go is
// the Budget's pure token arithmetic — the ONE chars-to-token conversion every estimator and
// token-gated Mechanism delegates to.
//
// Mechanisms. mechanism.go is the Mechanism vocabulary: the HookPoint set, the five hook
// interfaces, the descriptor with its ordering constraints and suppression policy, the
// post-response decision, and the MechanismRegistry surface. registry.go is that registry's
// pure logic — the hook-interface assertions, the startup ordering-cycle check, the
// deterministic per-hook-point total order, and the incompatibility gate (ADR 0003).
// stack.go is the one implementation of "is this Mechanism stack valid?", the
// requires/conflicts rule and its StackDefectKind, read by both the pre-build catalogue check
// and the post-build registry gates.
//
// Tools and confinement. tools.go is the open Tool extension point (ADR 0002) with ToolCall,
// ToolResult, the ToolRegistry, and the marker interfaces the dispatch disposition reads —
// ReadOnlyTool, SubprocessTool, ExternalEffectTool, ReadSourceTool, PromptTool — plus
// ApprovalScoper, read on the approval path rather than by the dispatch disposition, and
// DefaultOffTool, read by the registry assembly when it composes the default menu. It also holds
// FoldArgumentKey, CollidingArgumentKeys and RepeatedArgumentKeys — the one fold every reader of
// an argument object agrees on (the executor's decode matches keys case-insensitively), the check
// that refuses an object naming one parameter under two spellings, and the check that refuses an
// object answering one parameter twice with differing values.
// tooledit.go is the tool stage's pair of hook working values — ToolCallEdit and
// ToolResultEdit, the revision-bearing wrappers the two tool-stage hooks reshape a pending
// call and a returned result through. toolsummary.go is ToolSummary and its eight variants,
// the structured half of an outcome, written for a host rather than for the model. confinement.go is the Confiner interface, its
// capability and box value types, the per-call Confinement / SubprocessPermit context
// carriers (ADR 0012), and the WriteEscapePermit that carries one approved out-of-workspace
// write target (ADR 0049).
//
// The remaining facts about a session. contextfile.go is the workspace context files' report
// — one note per file and the Oversize predicate over their standing cost against the
// window. workspacename.go is the machine-independent rule for whether a configured
// context-file NAME can only ever resolve inside the workspace root, spoken identically by
// the host's config gate and the engine's construction gate (ADR 0023). fingerprint.go is
// ModelFingerprint, its confidence tiers and the resolver seam — what the Library keys
// learned observations on.
//
// And doc.go this map.
package domain
