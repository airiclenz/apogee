package agent

import (
	"reflect"
	"slices"
	"testing"
)

// hostCopy is how backgroundHost treats one Agent field when it builds a background workflow's host
// (background.go): the launch-time snapshot a background workflow runs off (ADR 0089).
type hostCopy int

const (
	// hostCopied: an idle-only field copied as it stands — backgroundHost runs at an idle boundary
	// or inside a Step, where nothing mutates it under the copy.
	hostCopied hostCopy = iota + 1
	// hostShared: a handle the whole tree holds by reference; the host holds the session's own, so
	// what the session or the workflow writes through it the other sees.
	hostShared
	// hostFrozen: a lock-guarded live setting read through its accessor and frozen at launch, so a
	// later move on the session never reaches a workflow already running (ADR 0089's launch-time
	// snapshot).
	hostFrozen
	// hostFresh: built new for the host, never the session's.
	hostFresh
	// hostZero: left at its zero value by design — state the host never needs, because it runs no
	// Turn of its own, is no delegate, and owns nothing it could close.
	hostZero
)

// hostField classes one Agent field and says why.
type hostField struct {
	class hostCopy
	why   string
}

// backgroundHostFields classes every Agent field by what backgroundHost does with it. A field added
// to Agent fails TestBackgroundHostCoversEveryAgentField until it is classed here, which is the
// moment to decide whether the host copies, shares, freezes, rebuilds or leaves it.
var backgroundHostFields = map[string]hostField{
	// Copied as they stand: the idle-only fields.
	"cfg":           {hostCopied, "idle-only: the Config the session runs, as it stands at launch"},
	"armed":         {hostCopied, "the construction-time armed route, fixed for the session"},
	"tools":         {hostCopied, "idle-only tool set as it stands; the host never closes or recomposes it"},
	"textParser":    {hostCopied, "idle-only parse seam of the bound profile"},
	"stripper":      {hostCopied, "idle-only parse seam of the bound profile"},
	"effortDialect": {hostCopied, "idle-only wire shape of the session server's effort intent"},
	"undoNote":      {hostCopied, "set with the journal it describes, which the host shares"},
	"depth":         {hostCopied, "the host stands where the session does, so its children nest from there"},

	// Shared by reference: the tree's handles.
	"sharedHandles": {hostShared, "the tree's shared handles copied whole, as a delegate takes its parent's: the latch, journal, consoles, run-id minter and workflow liveness by reference, the dial seam, clock, bounds and context files as they stand"},
	"seating":       {hostShared, "the session seat inherited whole: the holder by reference, so a rehome reaches the workflow's children and a /server switch leaves them behind (adr-0105-amend-1102b), with its resolver, owning nothing"},
	"guards":        {hostShared, "the session's guardrails; their breaker state is one"},
	"liveMode":      {hostShared, "a tighten-only view of the session's live mode, as a delegate holds its parent's"},
	"prompts":       {hostShared, "the one prompt slot every human gate in the tree queues on"},
	"tasks":         {hostShared, "the session's checklist, written through the task_list tool"},
	"tree":          {hostShared, "the tracked-file mutation floor of the one workspace"},

	// Frozen at launch: the lock-guarded live settings (ADR 0089's launch-time snapshot).
	"builtins":           {hostFrozen, "the builtin ladder of the Generation live at launch"},
	"mode":               {hostFrozen, "the live mode at launch; liveMode keeps it tighten-only after"},
	"confineToWorkspace": {hostFrozen, "the live blast radius at launch"},
	"scratchDir":         {hostFrozen, "the live session scratch dir at launch"},
	"gen":                {hostFrozen, "the live Generation at launch, Bypass and Floor read as one value"},
	"compaction":         {hostFrozen, "the live auto-Compaction gate at launch"},
	"prune":              {hostFrozen, "the live Pruning gate at launch"},
	"contextFileNames":   {hostFrozen, "the live context-file names at launch"},
	"parallelAgents":     {hostFrozen, "the live session width at launch"},
	"farWidth":           {hostFrozen, "the stated far width at launch, read under the same lock"},
	"seat":               {hostFrozen, "the Sub-agent server as installed at launch"},
	"effortOverride":     {hostFrozen, "the session's Thinking-effort intent at launch"},

	// Fresh: the host's own.
	"tokens": {hostFresh, "its own estimator, so a Step calibrating the session's never races it"},
	"turns":  {hostFresh, "its own idle Turn lifecycle over its own empty conversation"},

	// Zero by design.
	"ownsToolSet":           {hostZero, "the host never recomposes the tool set it copied"},
	"modeMu":                {hostZero, "the host's own lock, over its frozen copy"},
	"confineMu":             {hostZero, "the host's own lock, over its frozen copy"},
	"scratchMu":             {hostZero, "the host's own lock, over its frozen copy"},
	"genMu":                 {hostZero, "the host's own lock, over its frozen copy"},
	"swapMu":                {hostZero, "the host is never swapped (SetReactions)"},
	"forkNext":              {hostZero, "the host restores no session"},
	"briefing":              {hostZero, "the host opens no Session boundary"},
	"compactionMu":          {hostZero, "the host's own lock, over its frozen copy"},
	"pruneMu":               {hostZero, "the host's own lock, over its frozen copy"},
	"contextFilesMu":        {hostZero, "the host's own lock, over its frozen copy"},
	"parallelAgentsMu":      {hostZero, "the host's own lock, over its frozen copies"},
	"seatMu":                {hostZero, "the host's own lock, over its frozen copy"},
	"effortMu":              {hostZero, "the host's own lock, over its frozen copy"},
	"usage":                 {hostZero, "the host makes no call of its own to count"},
	"reading":               {hostZero, "the host reads no reply of its own"},
	"gitHost":               {hostZero, "the zero Host is the real OS, as newAgent sets it"},
	"conv":                  {hostZero, "the host runs no Turn: its conversation stays empty"},
	"compacting":            {hostZero, "the host folds nothing"},
	"callID":                {hostZero, "the host is no delegation, so no spawning call"},
	"runID":                 {hostZero, "the host is no delegation; its children mint their own"},
	"consoleOwner":          {hostZero, "the host is no delegation and opens no Console"},
	"task":                  {hostZero, "the host is no delegation"},
	"seatFallback":          {hostZero, "the host is no delegation"},
	"capRequested":          {hostZero, "the host is no delegation"},
	"nameMu":                {hostZero, "the host's own lock over an empty name"},
	"name":                  {hostZero, "the host is no delegation, so it is never named"},
	"children":              {hostZero, "the workflow's children are its own, addressed through the background run"},
	"mailbox":               {hostZero, "the host is nobody's child"},
	"mcpUnresolved":         {hostZero, "ready at its zero value, the host's own reports"},
	"retained":              {hostZero, "the session keeps the retained delegations, not its snapshot"},
	"background":            {hostZero, "the session runs the workflows; the host runs none of its own"},
	"delegations":           {hostZero, "the host opens no Exchange to ledger"},
	"steered":               {hostZero, "the host is nobody's child"},
	"stepCap":               {hostZero, "a delegate bound; the host is no delegate"},
	"tokenCap":              {hostZero, "a delegate bound; the host is no delegate"},
	"timeCap":               {hostZero, "a delegate bound; the host is no delegate"},
	"capStart":              {hostZero, "a delegate bound; the host is no delegate"},
	"capHit":                {hostZero, "a delegate bound; the host is no delegate"},
	"capFold":               {hostZero, "a delegate's fold; the host is no delegate"},
	"stoppedByUser":         {hostZero, "a delegate's stop; the host is no delegate"},
	"stopUndelivered":       {hostZero, "a delegate's stop; the host is no delegate"},
	"bindMu":                {hostZero, "the host's own lock; it runs no Turn a rehome could write under"},
	"outputPath":            {hostZero, "a delegation's output file; the host is no delegation"},
	"outputTarget":          {hostZero, "a delegation's output file; the host is no delegation"},
	"outputBefore":          {hostZero, "a delegation's output file; the host is no delegation"},
	"workflowItem":          {hostZero, "the host is no workflow item's child"},
	"midExchangeCompaction": {hostZero, "a delegate contract; the host is no delegate"},
}

// TestBackgroundHostCoversEveryAgentField fails when an Agent field is missing from
// backgroundHostFields, or the table names a field Agent no longer has, so a new field cannot reach
// a background workflow's host unclassed. It holds the classes to what backgroundHost builds: a
// zero-by-design field is zero on the host, a shared pointer is the session's own, and a fresh one
// is the host's.
func TestBackgroundHostCoversEveryAgentField(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	r.child(t, "running") // a session with a child spawned, so the host builds off a used Agent

	host := r.parent.backgroundHost()

	agentType := reflect.TypeFor[Agent]()
	session, copied := reflect.ValueOf(r.parent).Elem(), reflect.ValueOf(host).Elem()
	var fields []string
	for i := range agentType.NumField() {
		field := agentType.Field(i)
		fields = append(fields, field.Name)
		entry, ok := backgroundHostFields[field.Name]
		if !ok {
			t.Errorf("Agent.%s is not classed in backgroundHostFields: decide how backgroundHost treats it", field.Name)
			continue
		}
		if entry.why == "" {
			t.Errorf("Agent.%s is classed without a reason", field.Name)
		}
		got, was := copied.Field(i), session.Field(i)
		switch entry.class {
		case hostZero:
			if !got.IsZero() {
				t.Errorf("Agent.%s is classed zero by design, but the host's is set", field.Name)
			}
		case hostShared:
			if !sameHandles(got, was) {
				t.Errorf("Agent.%s is classed a shared handle, but the host holds another one", field.Name)
			}
		case hostFresh:
			if got.Kind() == reflect.Pointer && (got.IsNil() || got.Pointer() == was.Pointer()) {
				t.Errorf("Agent.%s is classed fresh, but the host's is nil or the session's", field.Name)
			}
		}
	}
	for name := range backgroundHostFields {
		if !slices.Contains(fields, name) {
			t.Errorf("backgroundHostFields classes %q, which Agent no longer has", name)
		}
	}
}

// sameHandles reports whether got holds the very handles was does: the same pointer for a pointer,
// and for a struct of handles (sharedHandles) the same pointer in every pointer field. Any other
// kind holds no handle to compare and passes.
func sameHandles(got, was reflect.Value) bool {
	switch got.Kind() {
	case reflect.Pointer:
		return got.Pointer() == was.Pointer()
	case reflect.Struct:
		for i := range got.NumField() {
			if !sameHandles(got.Field(i), was.Field(i)) {
				return false
			}
		}
	}
	return true
}

// TestBackgroundHostChildrenInheritTheSessionSeat pins a background workflow's children on the
// session seat exactly as a foreground child is: for every seat a sub_agent call may ask for, a child
// spawned off the host carries the seat — holder, resolver, applied generation, owning nothing — the
// Config's binding, the Delegation latch (the tree's, or a session-seated child's own empty one)
// and the seat-fallback note a child the session spawns
// carries, before a fallover, after one the session has not yet followed, and after it has.
func TestBackgroundHostChildrenInheritTheSessionSeat(t *testing.T) {
	t.Parallel()
	moments := []struct {
		name    string
		prepare func(t *testing.T, r *seatRig)
		gen     uint64
	}{
		{name: "before a fallover", prepare: func(*testing.T, *seatRig) {}, gen: 0},
		{name: "after a fallover the session has not followed", prepare: func(t *testing.T, r *seatRig) {
			t.Helper()
			stepOnce(t, r.child(t, "trigger"), "move the seat")
		}, gen: 0},
		{name: "after the session followed a fallover", prepare: func(t *testing.T, r *seatRig) {
			t.Helper()
			stepOnce(t, r.child(t, "trigger"), "move the seat")
			stepOnce(t, r.parent, "the session follows")
		}, gen: 1},
	}
	seats := []struct {
		name string
		seat delegationSeat
	}{
		{"configured", seatConfigured},
		{"session", seatSession},
		{"sub-agents server, nothing latched", seatSubAgentsServer},
	}
	for _, moment := range moments {
		for _, ask := range seats {
			t.Run(moment.name+"/"+ask.name, func(t *testing.T) {
				t.Parallel()
				r := newSeatRig(t, nil)
				moment.prepare(t, r)

				foreground := spawnOn(t, r.parent, ask.seat)
				background := spawnOn(t, r.parent.backgroundHost(), ask.seat)

				if background.seating != foreground.seating {
					t.Errorf("background child seat = %+v, foreground child's = %+v", background.seating, foreground.seating)
				}
				if background.seating.upstream != r.parent.seating.upstream || background.seating.fallback != r.resolver {
					t.Error("background child is not on the session's holder with the session's resolver")
				}
				if background.seating.ownsUpstream || background.seating.appliedGen != moment.gen {
					t.Errorf("background child owns=%t appliedGen=%d, want owns=false appliedGen=%d",
						background.seating.ownsUpstream, background.seating.appliedGen, moment.gen)
				}
				if b, f := background.cfg, foreground.cfg; b.Endpoint != f.Endpoint || b.ServerName != f.ServerName || b.Model != f.Model {
					t.Errorf("background child bound %s/%s/%s, foreground child %s/%s/%s",
						b.Endpoint, b.ServerName, b.Model, f.Endpoint, f.ServerName, f.Model)
				}
				// A session-seated child is handed an empty latch of its own; every other one the tree's.
				onTreeLatch := func(child *Agent) bool { return child.delegation == r.parent.delegation }
				if onTreeLatch(background) != onTreeLatch(foreground) || background.delegationTarget() != foreground.delegationTarget() {
					t.Errorf("background child on the tree's latch=%t target=%+v, foreground child's %t %+v",
						onTreeLatch(background), background.delegationTarget(), onTreeLatch(foreground), foreground.delegationTarget())
				}
				if background.seatFallback != foreground.seatFallback {
					t.Errorf("background child seatFallback = %t, foreground child's = %t", background.seatFallback, foreground.seatFallback)
				}
			})
		}
	}
}
