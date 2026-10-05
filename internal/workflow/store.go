package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// The fixed names of a workflow folder's parts (ADR 0087 D4).
const (
	storeDirName   = "workflows"
	planFileName   = "plan.json"
	statusFileName = "status.json"
	itemsDirName   = "items"
	receiptName    = "receipt.json"
	transcriptName = "transcript.jsonl"
	listingName    = "items.md"
	resultsDirName = "results"
)

// Permissions: the folder holds the model's working notes and a child's whole conversation, so it
// is the owner's alone, like the session store.
const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// idTimeLayout is the timestamp an id leads with: `YYYYMMDD-HHMMSS`.
const idTimeLayout = "20060102-150405"

// slugMaxLen bounds the slug an id ends with, so a long workflow name cannot make a long path.
const slugMaxLen = 40

// fallbackSlug is the slug of a workflow whose name leaves nothing of [a-z0-9-].
const fallbackSlug = "workflow"

// maxIDAttempts bounds the `-2`, `-3`… retries when an id's folder already exists: creates within
// one second collide, but a thousand of them is a runaway caller, not a busy one.
const maxIDAttempts = 1000

// idMaxLen is the longest id Dir accepts: the timestamp, the slug and a collision suffix.
const idMaxLen = len(idTimeLayout) + 1 + slugMaxLen + 5

// tempPattern names an atomic write's temp file; the leading dot keeps it out of a casual listing.
const tempPattern = ".apogee-workflow-*.tmp"

// ErrInvalidID is returned for a workflow id that is not a plain folder name of the store's own
// shape, so no id can turn into a path outside the store.
var ErrInvalidID = errors.New("workflow: invalid workflow id")

// ErrInvalidKey is returned for an item key that is not a SHA-256 in lower-case hex.
var ErrInvalidKey = errors.New("workflow: invalid item key")

// ErrInvalidName is returned for a stage-output name that is not a local path inside the folder.
var ErrInvalidName = errors.New("workflow: invalid output name")

// ErrItemFinished is returned by AdoptItem when the item's folder under its current key already
// holds an ok or partial receipt, which an adoption never overwrites.
var ErrItemFinished = errors.New("workflow: item already finished under its current key")

// Phase is where a workflow, a stage or an item stands.
type Phase string

// The phases. A workflow, stage or item starts pending; done means it ran to its end (an item's
// receipt may still say partial or blocked); stopped means a cancel or shutdown ended it unfinished,
// and an unfinished item restarts fresh on resume, never mid-conversation (ADR 0087 D4).
const (
	PhasePending Phase = "pending"
	PhaseRunning Phase = "running"
	PhaseDone    Phase = "done"
	PhaseSkipped Phase = "skipped"
	PhaseFailed  Phase = "failed"
	PhaseStopped Phase = "stopped"
)

// RunStatus is a workflow's status.json: its identity, the recipe skill its plan comes from ("" for
// a fan_out's plan, and for a folder no Run has opened yet), the PlanHash a resume matches on, and
// every stage's and item's phase with the receipts inline, so a reader (the /workflows view, a
// resume, a re-run) never opens an item's own files.
type RunStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Recipe string `json:"recipe,omitempty"`
	// Origin is what launched the workflow when that is not a recipe or a fan_out: OriginSubAgent
	// for a background sub_agent's one-item workflow (ADR 0094), "" otherwise and in every
	// status.json written before it existed. Create stamps it from the plan (OriginOf).
	Origin   string        `json:"origin,omitempty"`
	PlanHash string        `json:"plan_hash"`
	Phase    Phase         `json:"phase"`
	Created  time.Time     `json:"created"`
	Updated  time.Time     `json:"updated"`
	Stages   []StageStatus `json:"stages"`
}

// StageStatus is one stage's line in status.json, in the plan's stage order. Note is the stage's
// one-line note (why it was skipped, what a pick picked, that an ask took its default, how many
// finished items it redid) and Round
// the repeat round its items come from (0 for the stage's own run).
type StageStatus struct {
	Name  string       `json:"name"`
	Kind  StageKind    `json:"kind"`
	Phase Phase        `json:"phase"`
	Note  string       `json:"note,omitempty"`
	Round int          `json:"round,omitempty"`
	Items []ItemStatus `json:"items,omitempty"`
}

// ItemStatus is one item's line in status.json: its key (the folder name under items/), its label,
// its short name (ItemName, the one a Driver shows; empty on a script or ask line and in a
// status.json written before it existed, where the label serves), its phase, and the receipt its
// child handed back once it has one. A script or ask stage's one line has no key: it runs no child
// and keeps no item folder, so its receipt lives here and in the stage's record under results/
// (StageRecord), the copy a resume replays.
type ItemStatus struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Name    string   `json:"name,omitempty"`
	Phase   Phase    `json:"phase"`
	Receipt *Receipt `json:"receipt,omitempty"`
}

// Info is one Workflow of a session as a listing reads it: its status.json (every stage and item
// with the receipts so far), its folder, and whether the session's background manager holds it —
// running now, or Queued behind another on its server. A workflow neither flag marks is not live: a
// blocking fan_out's, or a background one that has ended. It lives here rather than beside the
// manager so a Driver can name it without importing the engine (ADR 0010's lowest-layer rule).
type Info struct {
	Status     RunStatus
	Dir        string
	Background bool
	Queued     bool
}

// Store is one session's workflow folders, rooted at `<scratch>/workflows/`. The scratch dir moves
// at every session boundary, so a Store is built per workflow from the live value, never cached.
// Every write is atomic (a temp file renamed into place), directories are 0700 and files 0600.
type Store struct {
	root string
}

// NewStore returns the store under scratchDir. It refuses an empty or relative scratchDir — the
// host answers "" when it could not make the dir, and a relative one would land in the workspace.
// Nothing is created until the first Create.
func NewStore(scratchDir string) (*Store, error) {
	if scratchDir == "" || !filepath.IsAbs(scratchDir) {
		return nil, fmt.Errorf("workflow: store root %q must be an absolute scratch directory", scratchDir)
	}
	return &Store{root: filepath.Join(filepath.Clean(scratchDir), storeDirName)}, nil
}

// Root is the `<scratch>/workflows` directory the store's folders live in.
func (s *Store) Root() string { return s.root }

// Create makes a new workflow folder for plan and writes its plan.json and a status.json with every
// stage pending. The id is `YYYYMMDD-HHMMSS-<slug>` from now and the plan's name; when that folder
// already exists (two creates in one second) the id takes `-2`, `-3`… until one is free.
func (s *Store) Create(plan Plan, planHash string, now time.Time) (RunStatus, error) {
	if planHash == "" {
		return RunStatus{}, errors.New("workflow: create needs the plan hash a resume matches on")
	}
	if err := os.MkdirAll(s.root, dirPerm); err != nil {
		return RunStatus{}, fmt.Errorf("workflow: create store root %q: %w", s.root, err)
	}
	id, err := s.mkdirUnique(now.Format(idTimeLayout) + "-" + slugOf(plan.Name))
	if err != nil {
		return RunStatus{}, err
	}

	status := RunStatus{
		ID: id, Name: plan.Name, Origin: OriginOf(plan), PlanHash: planHash, Phase: PhasePending,
		Created: now, Updated: now, Stages: make([]StageStatus, 0, len(plan.Stages)),
	}
	for _, stage := range plan.Stages {
		status.Stages = append(status.Stages, StageStatus{Name: stage.Name, Kind: stage.Kind, Phase: PhasePending})
	}
	if err := writeJSON(filepath.Join(s.root, id, planFileName), plan); err != nil {
		return RunStatus{}, err
	}
	if err := s.WriteStatus(status); err != nil {
		return RunStatus{}, err
	}
	return status, nil
}

// OriginSubAgent is the RunStatus.Origin of a workflow whose plan runs the sub_agent path: a
// background sub_agent's one-item workflow (ADR 0094).
const OriginSubAgent = "sub_agent"

// OriginOf is the Origin a workflow folder of plan is created with: OriginSubAgent when one of its
// stages runs the sub_agent path, else none. The engine reads it too, to word a background
// workflow's finish note by what launched it.
func OriginOf(plan Plan) string {
	for _, stage := range plan.Stages {
		if stage.RunsSubAgent() {
			return OriginSubAgent
		}
	}
	return ""
}

// mkdirUnique creates the folder base, or base-2, base-3… on a collision, and returns its id.
func (s *Store) mkdirUnique(base string) (string, error) {
	for attempt := 1; attempt <= maxIDAttempts; attempt++ {
		id := base
		if attempt > 1 {
			id = base + "-" + strconv.Itoa(attempt)
		}
		err := os.Mkdir(filepath.Join(s.root, id), dirPerm)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("workflow: create folder %q: %w", id, err)
		}
	}
	return "", fmt.Errorf("workflow: no free folder name for %q after %d attempts", base, maxIDAttempts)
}

// Dir is the absolute path of the workflow folder id, or ErrInvalidID for an id that is not of the
// store's own shape (so `../x`, `a/b` and the like never become a path).
func (s *Store) Dir(id string) (string, error) {
	if !isValidID(id) {
		return "", fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return filepath.Join(s.root, id), nil
}

// ReadPlan reads the workflow's plan.json.
func (s *Store) ReadPlan(id string) (Plan, error) {
	var plan Plan
	dir, err := s.Dir(id)
	if err != nil {
		return plan, err
	}
	return plan, readJSON(filepath.Join(dir, planFileName), &plan)
}

// ReadStatus reads the workflow's status.json.
func (s *Store) ReadStatus(id string) (RunStatus, error) {
	var status RunStatus
	dir, err := s.Dir(id)
	if err != nil {
		return status, err
	}
	return status, readJSON(filepath.Join(dir, statusFileName), &status)
}

// WriteStatus replaces the status.json of the workflow status.ID names. The folder must exist.
func (s *Store) WriteStatus(status RunStatus) error {
	dir, err := s.Dir(status.ID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, statusFileName), status)
}

// WriteReceipt writes an item's receipt.json, making items/<key>/ when it is the item's first file.
func (s *Store) WriteReceipt(id, key string, receipt Receipt) error {
	dir, err := s.itemDir(id, key)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, receiptName), receipt)
}

// ReadReceipt reads an item's receipt.json; found is false when the item has none yet.
func (s *Store) ReadReceipt(id, key string) (receipt Receipt, found bool, err error) {
	dir, err := s.Dir(id)
	if err != nil {
		return receipt, false, err
	}
	if !isValidKey(key) {
		return receipt, false, fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	err = readJSON(filepath.Join(dir, itemsDirName, key, receiptName), &receipt)
	if errors.Is(err, fs.ErrNotExist) {
		return receipt, false, nil
	}
	return receipt, err == nil, err
}

// finishedReceipt reads an item's receipt and reports found only for an ok or partial one: the
// receipt a resume skips the item for. A blocked receipt, or none, is not found.
func (s *Store) finishedReceipt(id, key string) (Receipt, bool, error) {
	receipt, found, err := s.ReadReceipt(id, key)
	if err != nil || !found {
		return Receipt{}, false, err
	}
	return receipt, receipt.Status == StatusOK || receipt.Status == StatusPartial, nil
}

// AdoptItem renames the folder an older key scheme gave an item, items/<fromKey>/, to the one its
// current key names, items/<toKey>/, so the item resumes from the receipt kept there. A toKey
// folder without an ok or partial receipt holds only unfinished work and is removed first; one
// with such a receipt is refused with ErrItemFinished and nothing moves. ErrInvalidKey refuses a
// key that is not a SHA-256 in lower-case hex, and fromKey equal to toKey.
func (s *Store) AdoptItem(id, fromKey, toKey string) error {
	dir, err := s.Dir(id)
	if err != nil {
		return err
	}
	for _, key := range []string{fromKey, toKey} {
		if !isValidKey(key) {
			return fmt.Errorf("%w: %q", ErrInvalidKey, key)
		}
	}
	if fromKey == toKey {
		return fmt.Errorf("%w: %q adopted onto itself", ErrInvalidKey, toKey)
	}
	_, finished, err := s.finishedReceipt(id, toKey)
	if err != nil {
		return err
	}
	if finished {
		return fmt.Errorf("%w: %q", ErrItemFinished, toKey)
	}
	from := filepath.Join(dir, itemsDirName, fromKey)
	to := filepath.Join(dir, itemsDirName, toKey)
	if err := os.RemoveAll(to); err != nil {
		return fmt.Errorf("workflow: clear item folder %q for adoption: %w", toKey, err)
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("workflow: adopt item folder %q as %q: %w", fromKey, toKey, err)
	}
	return nil
}

// StageRecord is a script or ask stage's settled outcome as the folder keeps it — the stage's phase,
// its note and its receipt (a script's fields, an ask's answer) — under results/<stage>/<round>.json,
// so a resume of the workflow replays it instead of running the script or asking the question again
// (Runner.Run). Only an outcome the stage actually reached is recorded: a script that ran (whatever
// its exit code) and a question someone answered, never a cancel, a script that could not run, or a
// default taken because no one could be asked.
type StageRecord struct {
	Phase   Phase   `json:"phase"`
	Note    string  `json:"note,omitempty"`
	Receipt Receipt `json:"receipt"`
}

// WriteStageRecord writes the record of stage's run in the given repeat round (0 for its own run),
// making results/<stage>/ when it is the stage's first. ErrInvalidName refuses a stage name that is
// not a plan's stage name, and a negative round.
func (s *Store) WriteStageRecord(id, stage string, round int, record StageRecord) error {
	path, err := s.stageRecordPath(id, stage, round)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("workflow: create results folder for stage %q: %w", stage, err)
	}
	return writeJSON(path, record)
}

// ReadStageRecord reads the record of stage's run in the given repeat round; found is false when the
// stage has none for that round.
func (s *Store) ReadStageRecord(id, stage string, round int) (record StageRecord, found bool, err error) {
	path, err := s.stageRecordPath(id, stage, round)
	if err != nil {
		return record, false, err
	}
	err = readJSON(path, &record)
	if errors.Is(err, fs.ErrNotExist) {
		return record, false, nil
	}
	return record, err == nil, err
}

// stageRecordPath is results/<stage>/<round>.json in the workflow folder id. A stage name is held to
// the plan's own spelling (namePattern), so it is always one plain path segment.
func (s *Store) stageRecordPath(id, stage string, round int) (string, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}
	if !namePattern.MatchString(stage) || round < 0 {
		return "", fmt.Errorf("%w: stage %q round %d", ErrInvalidName, stage, round)
	}
	return filepath.Join(dir, resultsDirName, stage, strconv.Itoa(round)+".json"), nil
}

// WriteTranscript writes an item child's conversation as transcript.jsonl, one message per line,
// for /workflows inspection (ADR 0087 D5: saved in the folder, never a Session record).
func (s *Store) WriteTranscript(id, key string, messages []domain.Message) error {
	dir, err := s.itemDir(id, key)
	if err != nil {
		return err
	}
	var lines bytes.Buffer
	encoder := json.NewEncoder(&lines)
	for i, message := range messages {
		if err := encoder.Encode(message); err != nil {
			return fmt.Errorf("workflow: encode transcript message %d: %w", i, err)
		}
	}
	return atomicWrite(filepath.Join(dir, transcriptName), lines.Bytes())
}

// ReadItemTranscript reads back the conversation WriteTranscript saved for the item key of the
// workflow folder dir (an Info's Dir), oldest message first; found is false when the item saved none
// (it has not finished, or its stage runs no child). ErrInvalidKey refuses a key that is not an item
// key, so no key can turn into a path outside the folder.
func ReadItemTranscript(dir, key string) (messages []domain.Message, found bool, err error) {
	if !isValidKey(key) {
		return nil, false, fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	data, err := os.ReadFile(filepath.Join(dir, itemsDirName, key, transcriptName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("workflow: read transcript of item %q: %w", key, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var message domain.Message
		if err := decoder.Decode(&message); err != nil {
			return nil, false, fmt.Errorf("workflow: decode transcript of item %q: %w", key, err)
		}
		messages = append(messages, message)
	}
	return messages, true, nil
}

// ItemTranscriptPath is the path of the transcript.jsonl WriteTranscript saved for the item key of
// the workflow folder dir (an Info's or a Result's Dir); found is false when the item saved none —
// it has not finished, its child faulted before it started, or its stage runs no child — and the
// path is then empty, so a caller names only a transcript that is there. ErrInvalidKey refuses a key
// that is not an item key, so no key can turn into a path outside the folder.
func ItemTranscriptPath(dir, key string) (path string, found bool, err error) {
	if !isValidKey(key) {
		return "", false, fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	path = filepath.Join(dir, itemsDirName, key, transcriptName)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("workflow: stat transcript of item %q: %w", key, err)
	}
	return path, true, nil
}

// ItemOutputPath is where the item key, labelled label, of the stage named stage in the workflow
// folder dir was told to write its detail output — read from the folder's plan.json: the stage's
// `out:` with {item} rendered as the label, exactly as the child was handed it (a relative path is
// the workspace's), or output.md inside the item's own folder when the stage sets no `out:` or runs
// the sub_agent path (outputPath). A stage the plan does not name has no `out:` of its own.
// ErrInvalidKey refuses a key that is not an item key.
func ItemOutputPath(dir, stage, key, label string) (string, error) {
	if !isValidKey(key) {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	var plan Plan
	if err := readJSON(filepath.Join(dir, planFileName), &plan); err != nil {
		return "", err
	}
	for _, candidate := range plan.Stages {
		if candidate.Name == stage && candidate.Out != "" && !candidate.RunsSubAgent() {
			return strings.ReplaceAll(candidate.Out, placeholderItem, label), nil
		}
	}
	return filepath.Join(dir, itemsDirName, key, outputName), nil
}

// Path is the absolute path of a stage output named by name, a slash-separated path local to the
// workflow folder (`report.md`, `stages/find/out-1.md`). It makes the parent directories, so a
// child handed the path can write it at once. ErrInvalidName refuses a name that is absolute,
// climbs out with `..`, or is empty.
func (s *Store) Path(id, name string) (string, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}
	local := filepath.FromSlash(name)
	if name == "" || strings.Contains(name, `\`) || !filepath.IsLocal(local) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	target := filepath.Join(dir, local)
	if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
		return "", fmt.Errorf("workflow: create output directory for %q: %w", name, err)
	}
	return target, nil
}

// WriteFile atomically writes data to the stage output name (see Path).
func (s *Store) WriteFile(id, name string, data []byte) error {
	target, err := s.Path(id, name)
	if err != nil {
		return err
	}
	return atomicWrite(target, data)
}

// WriteItems atomically writes items.md in the workflow's folder — the full listing of result,
// every item line with its output path (see Format) — and returns its absolute path. The Runner
// writes it as a run ends, stopped or done, so Format's `items:` line past 40 items points at it.
func (s *Store) WriteItems(id string, result Result) (string, error) {
	if err := s.WriteFile(id, listingName, []byte(renderListing(result))); err != nil {
		return "", err
	}
	return s.Path(id, listingName)
}

// Find returns the newest workflow whose status.json carries planHash — the workflow a re-issue
// resumes, skipping its finished items (ADR 0087 D4). found is false when none does or the store
// has never been written. A folder whose status.json is missing or unreadable is not a match.
func (s *Store) Find(planHash string) (status RunStatus, found bool, err error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, fs.ErrNotExist) {
		return RunStatus{}, false, nil
	}
	if err != nil {
		return RunStatus{}, false, fmt.Errorf("workflow: list store %q: %w", s.root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isValidID(entry.Name()) {
			continue
		}
		candidate, readErr := s.ReadStatus(entry.Name())
		if readErr != nil || candidate.PlanHash != planHash || candidate.ID != entry.Name() {
			continue
		}
		if !found || isNewer(candidate, status) {
			status, found = candidate, true
		}
	}
	return status, found, nil
}

// isNewer orders two statuses by creation time, the id breaking a tie (a `-2` sorts after).
func isNewer(candidate, current RunStatus) bool {
	if !candidate.Created.Equal(current.Created) {
		return candidate.Created.After(current.Created)
	}
	return candidate.ID > current.ID
}

// itemDir validates id and key and makes the item's folder under items/.
func (s *Store) itemDir(id, key string) (string, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}
	if !isValidKey(key) {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	itemDir := filepath.Join(dir, itemsDirName, key)
	if err := os.MkdirAll(itemDir, dirPerm); err != nil {
		return "", fmt.Errorf("workflow: create item folder %q: %w", key, err)
	}
	return itemDir, nil
}

// ItemKey is the SHA-256 (lower-case hex) that names an item's folder: over the brief (the Runner
// passes the stage's child-facing fields, its prompt file's contents and its repeat round), the item, and the path and contents of every context file read from the workspace fsys, in the order
// given. The same work gets the same key, so a re-issued workflow skips it; a changed context file
// gives a new key, so stale work is redone. A context file that cannot be read is an error. Every
// key scheme in keyscheme.go ends here, so a change to this encoding moves every scheme's golden key.
func ItemKey(brief string, item Item, contextFiles []string, fsys fs.FS) (string, error) {
	digest := sha256.New()
	writeField(digest, "brief", brief)
	writeField(digest, "label", item.Label)
	for _, unit := range item.Units {
		writeField(digest, "unit", unit)
	}
	for _, name := range contextFiles {
		cleaned, err := workspacePath(name)
		if err != nil {
			return "", fmt.Errorf("context file %q: %w", name, err)
		}
		contents, err := fs.ReadFile(fsys, cleaned)
		if err != nil {
			return "", fmt.Errorf("context file %q: %w", name, err)
		}
		writeField(digest, "context", cleaned)
		writeField(digest, "contents", string(contents))
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// PlanHash is the SHA-256 (lower-case hex) a resume matches a workflow on: over the plan's
// canonical JSON (encoding/json sorts map keys), the bound inputs, and the item list.
func PlanHash(plan Plan, inputs map[string]string, items []Item) (string, error) {
	canonical, err := json.Marshal(struct {
		Plan   Plan              `json:"plan"`
		Inputs map[string]string `json:"inputs"`
		Items  []Item            `json:"items"`
	}{plan, inputs, items})
	if err != nil {
		return "", fmt.Errorf("workflow: encode plan for hashing: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// writeField feeds one labelled, length-prefixed field to digest, so no two different field
// sequences can concatenate to the same bytes.
func writeField(digest hash.Hash, label, value string) {
	// hash.Hash.Write never returns an error.
	_, _ = fmt.Fprintf(digest, "%s:%d:%s\n", label, len(value), value)
}

// slugOf reduces a workflow name to `[a-z0-9-]`: letters lower-cased, every other run of
// characters one dash, trimmed of dashes and bounded to slugMaxLen; fallbackSlug when nothing is left.
func slugOf(name string) string {
	var slug strings.Builder
	isDashPending := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if isDashPending && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			isDashPending = false
			slug.WriteRune(r)
			continue
		}
		isDashPending = true
	}
	bounded := slug.String()
	if len(bounded) > slugMaxLen {
		bounded = strings.TrimRight(bounded[:slugMaxLen], "-")
	}
	if bounded == "" {
		return fallbackSlug
	}
	return bounded
}

// isValidID reports whether id has the shape Create mints: a `YYYYMMDD-HHMMSS-` timestamp, then
// only [a-z0-9-], within idMaxLen.
func isValidID(id string) bool {
	prefix := len(idTimeLayout) + 1
	if len(id) <= prefix || len(id) > idMaxLen {
		return false
	}
	if _, err := time.Parse(idTimeLayout, id[:len(idTimeLayout)]); err != nil || id[len(idTimeLayout)] != '-' {
		return false
	}
	return !slices.ContainsFunc([]byte(id[prefix:]), func(c byte) bool {
		return !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-')
	})
}

// isValidKey reports whether key is a SHA-256 in lower-case hex, the only shape ItemKey returns.
func isValidKey(key string) bool {
	if len(key) != sha256.Size*2 {
		return false
	}
	return !slices.ContainsFunc([]byte(key), func(c byte) bool {
		return !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))
	})
}

// writeJSON encodes value indented and writes it atomically to path.
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("workflow: encode %s: %w", filepath.Base(path), err)
	}
	return atomicWrite(path, append(data, '\n'))
}

// readJSON decodes the JSON file at path into value; a missing file wraps fs.ErrNotExist.
func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("workflow: read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("workflow: decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

// atomicWrite writes data to path through a temp file in the same directory renamed into place,
// so a reader sees the old file or the new one, never a partial one; the temp file is removed on
// every failure.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("workflow: create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	// After a successful rename the temp name no longer exists, so this Remove is a no-op.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workflow: chmod temp file %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workflow: write temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workflow: close temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("workflow: rename into %q: %w", path, err)
	}
	return nil
}
