package domain

import (
	"fmt"
	"time"
)

// ----------------------------------------------------------------------------
// Construction surface (ADR 0001)
// ----------------------------------------------------------------------------

// Config is the full construction surface. It carries the Upstream target, the
// autonomy posture, the host-supplied delegates, the extension registries, and the
// injected state roots. A zero Config is not valid; Endpoint and Events are the
// minimum. A struct (not functional options) because every field is a
// deliberate, reviewable seam and ADR 0001 speaks of state "injected via Config".
type Config struct {
	// Upstream — the local OpenAI-compatible LLM server (CONTEXT: Upstream).
	Endpoint string

	// Model is the model id sent on the wire. It MAY be empty at construction when the host
	// late-binds it — a host that starts before its Upstream is reachable constructs model-less
	// and binds the model it observes through Agent.Rebind (ADR 0024). Submit refuses until
	// something is bound, so an empty Model delays requests rather than allowing anonymous ones.
	Model string

	// APIKey is the bearer token sent as `Authorization: Bearer <key>` on every upstream
	// request. Empty — the local-server default — sends no auth header, so a keyless server
	// behaves exactly as it did before this field existed. The provider client redacts it from
	// server-echoed errors; it must never be logged, persisted, or shown by any consumer. One
	// key per session, because a session has exactly one Upstream (ADR 0024) — a different
	// server is a different invocation.
	APIKey string

	// Autonomy.
	Mode   Mode // Plan / Ask-Before / Allow-Edits / Auto (the privilege ladder)
	Bypass bool // ADR 0006: Mechanisms off, structure on (the hard-constraint floor)

	// ConfineToWorkspace tunes Auto's blast radius (ADR 0012); meaningful only in Auto.
	// true (the default) fences subprocess writes to the workspace under OS confinement
	// (network open, MCP gated); false ("I am the sandbox") runs Auto unconfined, safe
	// only inside a VM. It is loaded from the GLOBAL config only (a project config cannot
	// loosen it — the hostile-repo footgun is closed). The host sets it; the loop reads it
	// in the dispatch disposition.
	ConfineToWorkspace bool

	// ConfineWritablePaths and ConfineNetworkAllow extend the confinement box beyond the
	// workspace root (confinement-execution-contract §7): the toolchain cache/temp dirs a
	// confined `go build`/`pip` needs to write, and the per-project network tightening
	// list. The host probes/configures these and folds them into Config; the loop confines
	// a subprocess to WorkspaceDir ∪ ConfineWritablePaths with ConfineNetworkAllow as the
	// box's NetworkAllow. Empty NetworkAllow leaves the network open (the ADR 0012 default).
	ConfineWritablePaths []string
	ConfineNetworkAllow  []string

	// ScratchDir is this session's own scratch directory OUTSIDE the workspace
	// (`~/.apogee/scratch/<session-id>/` under the shipped composition root) — the one place a
	// confined subprocess may write besides the workspace itself, so scratch work has somewhere
	// safe to land instead of improvising into the project tree (the 2026-08-22 clobber incident).
	// ConfinementBox() folds it into WritablePaths, and the host owns its lifecycle: minting the
	// per-session path, creating it, and garbage-collecting stale siblings. Empty — the default,
	// and what a Driver that manages no sessions passes — adds nothing, leaving the box exactly
	// what it was before this field existed. It is the construction seed; the live, session-
	// following value is Agent.SetScratchDir's (the mode/confine pattern).
	ScratchDir string

	// Host-supplied delegates. The host (TUI / bench / embedder) owns these.
	Approver  Approver  // the human-in-the-loop gate; required unless Mode==Plan
	Asker     Asker     // free-text Q&A delegate for the ask_user tool; nil ⇒ ask_user is not registered (P3.11)
	Presenter Presenter // document-presentation delegate for the present_document tool; nil ⇒ present_document is not registered (ADR 0019)
	Confiner  Confiner  // nil ⇒ no confinement ⇒ Auto is refused (ADR 0004)
	Events    EventSink // where typed Events are pushed; required

	// Inspector arms the raw-protocol capture (`ui.inspector` in config.yaml): with it set, the
	// engine observes the Upstream client's own bytes and reports each model call's request body
	// and response payload to Events as a WireEvent, stamped with the emitting Agent's identity
	// like every other Event. It is a DEBUGGING view of traffic the engine already builds and
	// parses — nothing is retained here, and the credentials never travel (headers are not part of
	// the capture, by construction in internal/provider).
	//
	// false (the default) installs no observer at all, so the capture paths never run and a session
	// that leaves it alone is byte-identical to one built before this field existed. It is read at
	// CONSTRUCTION: New, Resume and a routed sub-agent spawn arm from it, and SwitchUpstream
	// re-arms the client it rebuilds, but nothing re-reads it mid-session — a host that flips it
	// applies the change by starting again.
	Inspector bool

	// Extension points. nil ⇒ the built-in defaults.
	Tools *ToolRegistry // open extension point (ADR 0002)

	// Mechanisms is the experimental-hook carrier: the bench registers candidate hooks on it via
	// AddExperimental, and a host may pre-build catalogued Mechanisms into it directly. Catalogued
	// Mechanisms are normally armed by ID through EnableMechanisms (ADR 0015), which builds each
	// named Mechanism and merges it INTO this registry (a fresh one when nil), so the two coexist in
	// one arm. The field keeps its name under v1 semver (no rename); EnableMechanisms is the
	// enable-by-ID surface (ADR 0002/0003, ADR 0015).
	Mechanisms *MechanismRegistry

	// EnableMechanisms names catalogued Mechanisms to arm by ID (ADR 0015 §1). New and Resume build
	// each named Mechanism at construction and merge it INTO Mechanisms (creating a fresh registry
	// when that is nil), so a catalogued Mechanism and a bench experimental hook coexist in one arm.
	// An unknown ID (ErrUnknownMechanism), an ID listed twice or already pre-built into Mechanisms
	// (the registry's already-registered rejection), a hook-less Mechanism, or a half-armed Requires
	// stack fails construction — a typo or a half-built stack never silently disables a Mechanism.
	// Empty/nil arms nothing (the default-off posture). The catalogue's CONTENTS are data, not v1
	// contract — an ID may change in a minor with a CHANGELOG notice; the field and its build
	// semantics are the stable surface (locked decisions 1–2, 6).
	EnableMechanisms []MechanismID

	// Skills resolves the user's attached skill IDs (UserInput.SkillIDs) to their injectable
	// bodies; nil ⇒ no skills are wired and any attached ID is reported and dropped. It is an
	// interface defined here (not the concrete internal/skills catalog) so the loop fulfils the
	// SkillIDs seam without domain importing skills — the dependency flows toward domain (ADR
	// 0010). The host (cmd/apogee) loads the catalog and injects it.
	Skills SkillResolver

	// Injected state roots — no implicit ~/.apogee (ADR 0001). The bench points
	// these at ephemeral dirs so sim runs never touch the production Library.
	LibraryDir string
	ConfigDir  string

	// WorkspaceDir is the sandbox root the built-in file tools are scoped to when
	// Config.Tools is nil. Empty ⇒ no default tools are wired (the host must inject
	// Config.Tools to give the Agent any tools). The bench points it at an ephemeral
	// workspace so a file-edit task never escapes its sandbox (ADR 0001 isolation).
	WorkspaceDir string

	// ExtraReadRoots names directories OUTSIDE WorkspaceDir that the built-in READ-ONLY file
	// tools (read_file, list_dir, grep, find_files) may reach — read-only mounts beside the
	// workspace fence — plus copy_file's SOURCE, which is itself a read (2026-08-12); that
	// tool's destination stays workspace-fenced like every other write.
	// nil ⇒ workspace-only, byte-identical to the fence before this field
	// existed. It applies to the DEFAULT tool set only, like DisabledTools: an injected
	// Config.Tools is the host's own assembly and is taken exactly as given.
	//
	// The func is evaluated LIVE — once per tool call — so a host whose set of mounts moves
	// mid-session (a setting a human can flip) is honoured by the next read with no re-wiring
	// and no reconstruction. Only ABSOLUTE paths resolve against these roots; a relative
	// argument keeps resolving against WorkspaceDir alone, so no one name can mean two files.
	// Each root keeps its own fence, so a symlink inside one that escapes it is still refused,
	// and a root that does not exist yet is skipped rather than failing the call.
	//
	// It is a GENERIC seam and the engine never defaults it: the TUI mounts its skill source
	// dirs through it, but nothing here knows what a skill is — the engine stays skill-agnostic
	// and any Driver can mount whatever its user has opened up (ADR 0031). Read-only is
	// structural, not a promise: nothing receives it for a WRITE (the workspaceScopedWriter
	// discipline, ADR 0012 D1), so mounting a directory never makes it writable — copy_file may
	// READ its source from a mount, and still writes only inside WorkspaceDir.
	ExtraReadRoots func() []string

	// ExternalEffects is the single injectable boundary for non-forkable effects
	// (network, MCP). nil ⇒ live. The bench injects a deterministic stub for v1;
	// record/replay slots in behind the same interface later (ADR 0008).
	ExternalEffects ExternalEffects

	// WebSearchEndpoint is the search backend the web_search tool sends a query to
	// (P3.11). DEFAULT-ON: empty ⇒ the tool falls back to its built-in DuckDuckGo
	// provider (no API key needed); the sentinel "off" disables it (a graceful "web
	// search is disabled", never a crash). The host folds a configured endpoint in from
	// config.yaml.
	WebSearchEndpoint string

	// DisabledTools names built-in tools the Agent must not be given — the roster switch the
	// host folds in from `tools.disabled:` in config.yaml. A named tool is left out of the set
	// built when Config.Tools is nil, so it is neither offered to the model nor dispatchable (a
	// call naming it is refused as an unknown tool). Empty/nil ⇒ the whole default menu, the
	// byte-identical default. A name matching no tool is ignored — pruning a roster must not be
	// able to stop an Agent from being constructed — so a host that can warn should check the
	// list against tools.KnownToolNames before it gets here.
	//
	// It is the GLOBAL rung of the roster precedence ladder — profile > global > build default
	// (ADR 0057) — so it is the default word on a tool rather than the last one: a matching
	// Config.Profile's Tools axis re-enables what it names. See EnabledTools for the ladder.
	//
	// It applies to the DEFAULT set only: an injected Config.Tools is the host's own assembly and
	// is taken exactly as given (ADR 0001 — the registry is injectable, and an embedder that
	// builds one has already said what it wants).
	DisabledTools []string

	// EnabledTools names built-in tools the Agent must be given even though this build leaves
	// them OUT of the default menu — the host's `tools.enabled:` list, the lift a tool
	// registered default-off needs (domain.DefaultOffTool, ADR 0057). It is the enable direction
	// of the switch DisabledTools spells: a name here puts such a tool back on the set built
	// when Config.Tools is nil, so it is both offered to the model and dispatchable. Empty/nil ⇒
	// nothing is lifted — the byte-identical default, and today the whole state, since no
	// built-in tool ships default-off.
	//
	// These two lists are the GLOBAL rung of the roster precedence ladder — profile > global >
	// build default (ADR 0057): a matching Config.Profile's Tools axis overrides them per tool,
	// and they in turn override a tool's own default-off registration. A name in BOTH global
	// lists is a conflict DISABLED wins — fail closed — and, like a name matching no tool, one
	// the HOST reports: construction has nowhere to put a warning, and a roster the user is
	// editing must never be able to stop an Agent from being built (see DisabledTools).
	//
	// Like DisabledTools it applies to the DEFAULT set only: an injected Config.Tools is the
	// host's own assembly and is taken exactly as given (ADR 0001).
	EnabledTools []string

	// URLAllowHosts and URLDenyHosts are the host layer of the network tools' url-safety guard —
	// the hosts web_fetch / http_request / web_search may reach, and the hosts they may not — which
	// the host folds in from `url-safety:` in config.yaml. Deny wins over allow; a non-empty allow
	// list restricts to exactly those hosts and their subdomains; empty/nil ⇒ every host, the
	// byte-identical default before the key existed.
	//
	// They can only ever TIGHTEN: the guard's default-on, resolved-IP SSRF floor is not reachable
	// from configuration at all (security.URLGuard.DisableIPFloor is a code-level opt-out), so a
	// Config carrying these fields adds denials and never removes the floor. Entries are normalised
	// to the dialled host form when the guard is built (security.NewURLGuard), so an entry written
	// with mixed case, a trailing root dot, non-ASCII, or an IPv6 literal in brackets still matches.
	//
	// Like DisabledTools they apply to the DEFAULT tool set only: an injected Config.Tools is the
	// host's own assembly and is taken exactly as given (ADR 0001).
	URLAllowHosts []string
	URLDenyHosts  []string

	// SecretEnvVars names environment variables the execution tools (terminal, python_exec,
	// run_tests) must drop from the environment they hand a subprocess — the caller-named half of
	// a scrub whose fixed half is apogee's own APOGEE_API_KEY. The host folds in the variables its
	// configured key sources read (`api-key-env:`, ADR 0047): a key the operator exported into the
	// shell apogee was started from is otherwise inherited by every subprocess whose contents the
	// MODEL chose, where reading it and sending it somewhere is one command away. Empty/nil ⇒ only
	// apogee's own names are dropped, byte-identical to the scrub before this field existed.
	//
	// Names are compared case-insensitively (Windows environment names are one variable in either
	// spelling), and a name nothing in the environment matches is simply not there to drop. Like
	// DisabledTools it applies to the DEFAULT tool set only: an injected Config.Tools is the host's
	// own assembly and is taken exactly as given (ADR 0001).
	SecretEnvVars []string

	// Profile describes how apogee equips and speaks to the configured model (CONTEXT: Model
	// profile) — its tool-call format, its inline thinking-channel style, and the tool roster it
	// is equipped with (ADR 0057 decision 1) — so the loop selects the matching tool-call parser
	// and content-stripper at the parse seam. A ZERO Profile == native tool calls with no inline
	// thinking and no roster deltas == today's exact behaviour (the byte-identical anchor): a
	// native profile selects no-op parsers, so the content path is unchanged. The host folds a
	// configured profile in from config.yaml; an embedder sets it directly. It is declarative
	// DATA translated to internal/processing's parsers at the boundary (ADR 0010) — not the
	// parsers' own config types, which cannot move up the DAG since processing imports domain.
	Profile ModelProfile

	// SystemPrompt is the system-prompt TEMPLATE (ADR 0023) — internal/prompt's four
	// placeholders ({{workspace}}, {{datetime}}, {{mode}}, {{scratch}}) — rendered FRESH per request by
	// the loop and seeded as the first system message of the wire request. It is a template,
	// not a rendered string, because two inputs are live (the date, the autonomy mode). It is
	// request-scoped only: never committed to history, never in the snapshot. "" (the
	// default) seeds nothing — the byte-identical no-prompt anchor. The host folds a
	// configured template in from config.yaml (file-only); an embedder sets it directly. An
	// unknown placeholder fails construction (New/Resume), like a bad Profile.
	SystemPrompt string

	// ContextFiles are the workspace context files folded into the standing system content —
	// the RESOLVED names to look for, in inclusion order; nil or empty ⇒ the feature is off.
	// Each name is workspace-relative (joined to WorkspaceDir): it must be relative and must
	// not escape the workspace, or construction fails naming the offender. The list is an
	// INCLUSION set, not a priority chain — every listed name that exists is included, and a
	// name that does not exist is simply skipped, because discovery is the feature.
	//
	// Content is DATA, never a template: it is carried beside SystemPrompt rather than
	// concatenated into it and is never run through internal/prompt, so a repo's own
	// {{braces}} reach the model verbatim and can never fail startup. The files are read at
	// SESSION BOUNDARIES only — construction, /clear|/new, a live restore — so a mid-session
	// edit never swaps the content under a running session (KV-cache stability), and a
	// sub-agent inherits the parent session's bytes rather than re-reading. Like SystemPrompt
	// the content is request-scoped: never committed to history, never in the snapshot. The
	// host folds a configured list in from config.yaml (file-only); an embedder sets it
	// directly.
	ContextFiles []string

	// ParallelAgents is the width of a depth-0 sub-agent fan-out (CONTEXT: Parallel agents, ADR
	// 0039): how many of ONE reply's `sub_agent` calls the loop may run at the same time. < 2 means
	// serial — the byte-identical floor this engine has always run at — and the engine treats it as
	// a WIDTH, never a promise: it is an upper bound the dispatch may fall short of (a reply with
	// one delegation runs one), and nothing reserves capacity against it.
	//
	// It is a property of the SERVER rather than of the session — a llama.cpp server serves
	// `--parallel N` requests at once, each in its own slot — so the host resolves it per bound
	// server (the entry's `parallel-agents:` pin, else that server's /props total_slots, else 1) and
	// re-states it when the session moves. An embedder sets it directly; 0 is the safe default that
	// asks for nothing.
	ParallelAgents int

	// Budget / Compaction knobs (context/) are structural and load-bearing — they
	// run even under Bypass. Defaults are sane; overrides are advanced.
	Context ContextConfig

	// Delegation bounds what a sub-agent run may spend. Like Context it is structural — it
	// stays on under Bypass — and it applies to CHILD agents only: the main loop is the
	// human's to stop, a delegate's is nobody's.
	Delegation DelegationConfig
}

// DelegationConfig bounds a sub-agent run. It is NOT a Mechanism (ADR 0006): a delegate that
// cannot be stopped is a structural hole, so the bound stays on under Bypass.
type DelegationConfig struct {
	// MaxSteps is the number of Turns a child agent may take in its one Exchange before the
	// engine ends it; 0 = unbounded. The host folds in the `delegate-max-steps:` key
	// (default 80); an embedder sets it directly.
	MaxSteps int
}

// ContextConfig governs the structural context reducers — Budget and Compaction —
// which are NOT Mechanisms and stay on under Bypass (CONTEXT: Budget, Compaction).
type ContextConfig struct {
	MaxContextTokens int // 0 ⇒ window unknown; the CLI discovers it or the context-window key supplies it (the Budget then allocates nothing and the engine's growth bounds fall back to one conservative assumed ceiling — internal/agent, ADR 0018)
	ResponseReserve  int

	// ResponseReserveFraction is the share of the discovered window held back for the model's
	// reply when no explicit token reserve is pinned — the `response-reserve:` config key
	// (top-level, or the bound server entry's override), expressed as a fraction. The reserve
	// follows one precedence, in this order: an explicit ResponseReserve in TOKENS wins whatever
	// the fraction says; else a fraction in (0, 1) of the window; else the built-in default share
	// (internal/context.Allocate). 0 means unset, and so does any value outside (0, 1) — the
	// config layer rejects an out-of-range fraction, and the allocator treats one that reaches it
	// as unset rather than allocating an absurd reserve.
	ResponseReserveFraction float64

	// MaxOutputTokens CAPS one reply, in tokens — the ceiling the engine states on the wire so a
	// reply stops where the engine already budgeted for it rather than at the server's context wall
	// (ADR 0046). 0 ⇒ nothing pinned, and the engine derives the cap from the Budget's own
	// ResponseReserve, clamped; > 0 ⇒ that ceiling whatever the window says. It is fed the bound
	// server entry's `max-output-tokens:` pin, the way MaxContextTokens is fed `context-window:`,
	// and it is the escape hatch for an unknown window — which the derivation must read as the
	// clamp floor rather than as "unbounded" (internal/context.Allocation).
	MaxOutputTokens int

	CompactionEnabled bool // generative summarisation; default true
}

// ----------------------------------------------------------------------------
// Model profile (CONTEXT: Model profile) — the per-model wire description
// ----------------------------------------------------------------------------

// ModelProfile describes how apogee EQUIPS and speaks to a given model (CONTEXT: Model profile):
// three ORTHOGONAL axes — its tool-call format, its inline thinking-channel style (a model can
// emit native tool calls AND inline thinking; gpt-oss does both), and its tool roster, which is
// capability tuning rather than wire shape (ADR 0057). It is declarative domain
// DATA on Config (host- or embedder-settable) that the loop translates to the internal/processing
// parsers at the parse seam, not the parsers' own config types — those cannot move up the
// dependency DAG because internal/processing imports domain (ADR 0010), and profile-as-data
// snapshots cleanly and seeds the deferred switchable-profile / `apogee probe` work. A ZERO
// ModelProfile == native tool calls, no inline thinking, no roster deltas == today's exact
// behaviour (the byte-identical anchor), every axis independently.
//
// The axes resolve INDEPENDENTLY at the layer above: a host picks each one from the nearest
// configuration layer that spells it (ADR 0057 supersedes ADR 0044's whole-entry replacement).
// What reaches here is one already-resolved profile — the engine sees no layering.
//
// The roster axis carries slices, so a ModelProfile is NOT comparable: test two of them with
// reflect.DeepEqual or axis by axis, never with == / !=.
type ModelProfile struct {
	// ToolCallFormat selects how the model emits tool calls. "" and FormatNative both mean the
	// structured out-of-band tool_calls path (nothing to recover from visible content); a text
	// format (FormatMarkdownFenced / FormatCustomRegex) is parsed from the model's visible
	// content at the seam.
	ToolCallFormat ToolCallFormat

	// Pattern is the custom-regex tool-call pattern — required by FormatCustomRegex, and REFUSED
	// at config load under the other formats, which never read it. Named capture groups name the
	// tool and its arguments; the parser's own group/flag defaults apply at the boundary when its
	// finer knobs are unset.
	Pattern string

	// Thinking selects the model's inline reasoning-channel style. A zero Thinking (ThinkingNone)
	// leaves the Upstream-split reasoning_content path untouched (the default).
	Thinking ThinkingProfile

	// Tools is this model's roster axis — delta lists against the DEFAULT tool set, so a tool can
	// be off for the small class and on for a big model. A zero ToolRosterDelta means no deltas
	// at all: the roster the host would build anyway, which is what keeps a zero ModelProfile the
	// byte-identical anchor.
	Tools ToolRosterDelta
}

// ToolRosterDelta is the Model profile's tool-roster axis (CONTEXT: Model profile): a pair of
// DELTA lists against the default tool set, never a replacement roster — a full list would
// silently starve a profiled model of every tool a later release adds (ADR 0057). The ZERO value
// is no deltas: the default roster stands untouched, which is why adding this axis leaves a zero
// ModelProfile the byte-identical anchor it has always been.
//
// It is the most specific rung of the roster precedence ladder — profile > global
// (Config.DisabledTools / Config.EnabledTools) > build default (a tool registered
// DefaultOffTool). So a profile Enabled entry lifts a globally disabled or default-off tool — the
// ratifying use case, a tool off for the models that would drown in it and on for the one that
// wants it — and a profile Disabled entry turns off what global allows. Within ONE scope a name
// in both lists is a conflict DISABLED wins (fail closed); that conflict and an unknown name are
// both reported by the HOST and are never a refusal, exactly as for the global lists.
//
// Like those lists it applies to the DEFAULT tool set only: an injected Config.Tools is the
// host's own assembly, taken exactly as given (ADR 0001), and MCP-served tools ride their own
// surface untouched. It is plain configuration, not a Mechanism — no gating, no Bypass
// interaction.
type ToolRosterDelta struct {
	// Disabled names built-in tools this model must NOT be offered, whatever the global lists and
	// the build default say. Empty/nil ⇒ this axis subtracts nothing.
	Disabled []string

	// Enabled names built-in tools this model must be offered even when the build registers them
	// default-off or the global list disables them. Empty/nil ⇒ this axis adds nothing.
	Enabled []string
}

// ToolCallFormat identifies how a model emits tool calls, so the loop can select the matching
// parser at the seam. Its values mirror internal/processing's ToolCallFormat so the boundary
// translation is a straight map. "" is treated as FormatNative.
type ToolCallFormat string

const (
	// FormatNative is the structured tool_calls path: calls arrive out-of-band and the text
	// parser finds nothing in the visible content ("" is treated the same).
	FormatNative ToolCallFormat = "native"
	// FormatMarkdownFenced is the markdown-fenced code-block tool-call format.
	FormatMarkdownFenced ToolCallFormat = "markdown-fenced"
	// FormatCustomRegex is the user-supplied named-group regex tool-call format (needs Pattern).
	FormatCustomRegex ToolCallFormat = "custom-regex"
)

// ThinkingProfile selects a model's inline thinking-channel style (CONTEXT: Thinking channel):
// the private reasoning the loop strips from visible content and preserves as reasoning in
// history. A zero ThinkingProfile (ThinkingNone) means no inline channel — content passes
// through untouched, the right default when the Upstream already splits reasoning into a
// separate reasoning_content field.
type ThinkingProfile struct {
	// Style selects the stripping strategy: ThinkingNone (no inline channel, the default),
	// ThinkingDelimited (a literal Start/End token pair), or ThinkingHarmony (gpt-oss channels,
	// which need no tokens).
	Style ThinkingStyle

	// Start and End are the literal delimiter tokens for ThinkingDelimited (e.g. "<think>" /
	// "</think>"); both must be set for stripping to run. They are ignored for the other styles.
	Start string
	End   string

	// Effort is how hard this model is asked to think (CONTEXT: Thinking effort) — a dial the
	// request forwards to the server's chat template, ORTHOGONAL to Style above: Style says how
	// the reasoning arrives in the reply, Effort says how much of it to produce. The zero value
	// ("") is the wire anchor: absent means NOTHING is emitted for it, so an out-of-the-box
	// request stays byte-identical and the model's own template default stands (ADR 0050).
	Effort ThinkingEffort
}

// ThinkingStyle names a model's inline reasoning-channel format. "" is treated as ThinkingNone.
type ThinkingStyle string

const (
	// ThinkingNone is the default: no inline channel (the model emits none, or the Upstream
	// already split reasoning into reasoning_content). "" is treated the same.
	ThinkingNone ThinkingStyle = "none"
	// ThinkingDelimited is a literal Start/End token pair bracketing reasoning (e.g.
	// <think>…</think>). The exact tokens vary per model and even per build — the live smoke
	// test found gemma-4-e4b-it-qat emits <|channel>thought…<channel|> — so Start/End must be
	// set to what the model actually emits, not assumed from the model family.
	ThinkingDelimited ThinkingStyle = "delimited"
	// ThinkingHarmony is gpt-oss's harmony channel format (<|channel|>analysis<|message|>…).
	ThinkingHarmony ThinkingStyle = "harmony"
)

// ThinkingEffort names how hard a model is asked to think (CONTEXT: Thinking effort). It is a
// profile axis rather than a global knob because the vocabulary a template understands is a fact
// about the model, and the provider Client owns the mapping from these words onto the wire, one
// per dialect (ADR 0050, amended by ADR 0060). The vocabulary is the seven-name union of what real
// servers report — off, low, medium, high, minimal, xhigh, max — so a model's own reported set is
// always spellable; no single model offers all seven, and a level its template does not understand
// fails the turn rather than this type. "off" is the apogee-canonical spelling of "no reasoning at
// all", and "none" is the same rung under the spelling the OpenRouter dialect renders it as. "" is
// not a level at all — it is the ABSENCE of the setting, and absence emits nothing.
type ThinkingEffort string

const (
	// EffortOff asks for no chain-of-thought at all.
	EffortOff ThinkingEffort = "off"
	// EffortNone is the same rung as EffortOff under the spelling the OpenRouter dialect uses; a
	// server that reports "none" is configurable with the word it reported.
	EffortNone ThinkingEffort = "none"
	// EffortMinimal is the barely-there rung the OpenAI-shaped servers report below "low".
	EffortMinimal ThinkingEffort = "minimal"
	// EffortLow is the shortest reasoning the template offers.
	EffortLow ThinkingEffort = "low"
	// EffortMedium is the middle rung.
	EffortMedium ThinkingEffort = "medium"
	// EffortHigh is the longest reasoning the template offers.
	EffortHigh ThinkingEffort = "high"
	// EffortXHigh is the rung above "high" on the templates that offer one (Qwen3.8 defaults to it).
	EffortXHigh ThinkingEffort = "xhigh"
	// EffortMax is the topmost rung reported in the wild, above "xhigh" where both exist.
	EffortMax ThinkingEffort = "max"
)

// Valid reports whether e is a value this build understands: any level in the union above, or the
// zero value meaning unset — which is a legitimate configuration, not a defect, because absence is
// how a profile leaves the model's own template default alone. The config loader asks this so a
// typo'd `effort:` is a startup error naming the key rather than a setting that silently does
// nothing. It gates the SPELLING only: whether the bound model actually offers the rung is the
// server's answer, and the enriched turn error naming `thinking.effort` stays that backstop.
func (e ThinkingEffort) Valid() bool {
	switch e {
	case "", EffortOff, EffortNone, EffortMinimal,
		EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	default:
		return false
	}
}

// Mode is the autonomy level governing whether tool calls need human approval
// (CONTEXT: Agent mode). It is orthogonal to Config.Bypass.
type Mode string

const (
	// ModePlan is read-only: no writes, no command execution.
	ModePlan Mode = "plan"
	// ModeAskBefore requires an Approval for every write, command, and external reach
	// (a harmless read runs free).
	ModeAskBefore Mode = "ask-before"
	// ModeAllowEdits auto-approves Apogee's own workspace-scoped writes (path-safety-
	// bounded); shell/exec, network, MCP, third-party in-process tools, and any
	// out-of-workspace write still gate. It needs NO Confinement — path-safety bounds
	// the auto-approved writes and the human backstops the unbounded surface — so it is
	// identical on every OS (ADR 0012).
	ModeAllowEdits Mode = "allow-edits"
	// ModeAuto runs unbounded tool calls without per-call approval, tuned by
	// Config.ConfineToWorkspace (ADR 0012). With confinement on (the default), the
	// subprocess surface runs OS-confined to the workspace; an unfenceable tool (MCP) or
	// an out-of-workspace Apogee write still gates through Approval; if fs-confinement is
	// unavailable, the subprocess surface gates ("confine if you can, gate if you can't").
	ModeAuto Mode = "auto"
)

// ParseMode validates a mode spelling against the known autonomy modes (the ladder
// Plan → Ask-Before → Allow-Edits → Auto) — the `--mode` flag, `APOGEE_MODE`, and the
// config file's `mode:` key all name a rung with the same four words, so they all read
// the ladder from here rather than each restating it. Auto parses successfully; whether
// it can RUN depends on the host's fs-confinement (ADR 0012 — Auto needs landlock ABI ≥1
// on Linux, or is refused only when no fs-confinement exists), which is a construction
// question and not a spelling one.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModePlan, ModeAskBefore, ModeAllowEdits, ModeAuto:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("apogee: invalid --mode %q (want plan, ask-before, allow-edits, or auto)", s)
	}
}

// modeLadder is the autonomy privilege ladder in cycle order (least to most autonomous);
// the cycle wraps Auto → Plan. It is the single source of truth for Shift+Tab mode cycling.
var modeLadder = []Mode{ModePlan, ModeAskBefore, ModeAllowEdits, ModeAuto}

// NextMode returns the mode one rung up the privilege ladder, wrapping Auto back to Plan.
// An unknown or empty mode starts the cycle at Plan (the safest rung), so a caller can never
// get stuck off-ladder.
func NextMode(cur Mode) Mode {
	for i, m := range modeLadder {
		if m == cur {
			return modeLadder[(i+1)%len(modeLadder)]
		}
	}
	return ModePlan
}

// TighterMode returns the more restrictive of two autonomy modes — the one lower on the
// privilege ladder (Plan < Ask-Before < Allow-Edits < Auto). It is the sub-agent tighten-only
// helper (ADR 0013): a sub-agent's disposition takes the tighter of the parent's LIVE mode and
// the child's spawn mode, so a parent tightening mid-delegation (Shift+Tab down) reaches the
// still-running child, while a parent loosening can never loosen it. An off-ladder mode
// (empty/unknown) ranks with Ask-Before — the same safe default the dispatch disposition
// applies to an unrecognised mode — so a stray value can neither loosen nor over-tighten the
// result.
func TighterMode(a, b Mode) Mode {
	if modeRank(a) <= modeRank(b) {
		return a
	}
	return b
}

// modeRank is a mode's restriction rank: its index on the privilege ladder, where a lower rank
// is tighter. An off-ladder mode (empty/unknown) ranks with Ask-Before, matching the
// disposition's safe default for an unrecognised mode, so TighterMode's ordering agrees with
// the ladder the dispatch table keys on.
func modeRank(m Mode) int {
	rank, fallback := -1, 0
	for i, lm := range modeLadder {
		if lm == m {
			rank = i
		}
		if lm == ModeAskBefore {
			fallback = i
		}
	}
	if rank < 0 {
		return fallback
	}
	return rank
}

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// UserInput is one user message into an Exchange: free text plus optional file
// references the loop resolves into context, plus reserved skill references. Stays a value
// (no live handles) so it snapshots cleanly.
//
// FileRefs (@file tokens parsed from the chat input) are resolved at Step time — the loop
// reads each within the workspace fence and prepends its content to the user message.
// SkillIDs are the skills the user attached in chat (inline "/id" tokens); the loop resolves
// each through Config.Skills and prepends its body to the user message for that one turn. The
// refs round-trip through a snapshot, so a resumed session re-resolves them.
type UserInput struct {
	Text     string
	FileRefs []string
	SkillIDs []string `json:",omitempty"`
}

// SkillResolver maps attached skill IDs to their injectable form. It is implemented by the
// skills catalog (internal/skills) and injected via Config.Skills; the interface lives in
// domain so the loop can fulfil the UserInput.SkillIDs seam without importing the skills
// package (ADR 0010 — the dependency flows toward domain).
type SkillResolver interface {
	// ResolveSkills returns the resolved skills for ids, in the given order, skipping any
	// unknown ID. The caller compares the result against what it requested to report a miss,
	// so a typo in an attached ID is never silently swallowed.
	ResolveSkills(ids []string) []ResolvedSkill
}

// ResolvedSkill is one attached skill reduced to the fields the loop injects: the ID and
// DisplayName label the prepended block, and Body is the skill's instruction text scoped to
// the turn it was attached to.
//
// Dir is the absolute path of the skill's folder, which the loop names in the injected block so
// the model can read the files bundled beside the skill (refs, prompts, scripts) with the
// read-only tools. It is empty when the resolver has none, and an empty Dir simply omits that
// line — the block is then exactly what it was before the field existed.
type ResolvedSkill struct {
	ID          string
	DisplayName string
	Body        string
	Dir         string
}

// StepResult reports the outcome of one Step at the quiescent boundary.
type StepResult struct {
	Status    StepStatus
	TurnIndex int           // 0-based index of the Turn just completed
	Elapsed   time.Duration // wall time for this Turn
	// Faulted marks a Turn ABANDONED rather than completed: it produced no usable outcome (an
	// Upstream fault, a recovered extension panic, or an overflow the loop could not fold its way
	// out of), so the loop degraded it to a clean boundary. It is deliberately ORTHOGONAL to
	// Status — an abandoned Turn ends its Exchange, so it reports StatusExchangeComplete exactly
	// as a real final answer does, and this flag is the only thing that tells the two apart.
	// A host that merely resumes at the boundary can ignore it; anything that reports the
	// Exchange's RESULT onward — a sub-agent delegation answering its parent — must not present a
	// faulted Exchange as a success. The fault itself was already surfaced as an ErrorEvent.
	Faulted bool

	// StepCapped marks an Exchange the engine ENDED at the delegate step cap
	// (Config.Delegation.MaxSteps) rather than one the model finished: the child was still
	// asking for tools when the bound was reached, so what it has is a PARTIAL result. Like
	// Faulted it is orthogonal to Status — a capped Exchange closes on StatusExchangeComplete
	// exactly as a real final answer does — and unlike Faulted it is NOT a failure: the work up
	// to the cap stands. A host that merely resumes at the boundary can ignore it; anything
	// reporting the Exchange's RESULT onward — a sub-agent delegation answering its parent —
	// must say the result is partial rather than present it as the finished task. The cap
	// itself was already surfaced as an ErrorEvent.
	StepCapped bool
}

// StepStatus is the disposition of a completed Step. The set is open (additively
// extensible — treat unknown values defensively).
type StepStatus string

const (
	// StatusTurnComplete: the Turn finished and more Turns are pending (the model
	// requested tools; the loop will continue on the next Step).
	StatusTurnComplete StepStatus = "turn-complete"
	// StatusExchangeComplete: the model produced a final no-tool response; the
	// Agent now awaits the next Submit.
	StatusExchangeComplete StepStatus = "exchange-complete"
	// StatusCancelled: ctx was cancelled; state is serializable, resume is valid.
	StatusCancelled StepStatus = "cancelled"
)
