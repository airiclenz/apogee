package gitstatus

import (
	"bytes"
	"strconv"
	"strings"
)

// recordTerminator ends every record of `git status -z` output.
const recordTerminator = "\x00"

// The porcelain v2 record prefixes the fold recognises.
const (
	headPrefix      = "# branch.head "
	upstreamPrefix  = "# branch.upstream "
	aheadPrefix     = "# branch.ab "
	ordinaryPrefix  = "1 "
	renamePrefix    = "2 "
	unmergedPrefix  = "u "
	untrackedPrefix = "? "
	detachedHead    = "(detached)"
)

// The field counts of the change records. The last field is the path, which may itself hold
// spaces, so a record is split into exactly this many fields and no more.
const (
	ordinaryFields = 9  // "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>"
	renameFields   = 10 // "2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <Xscore> <path>"
	unmergedFields = 11 // "u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>"
)

// Snapshot is the parsed shape of `git status --porcelain=v2 --branch -z`: the branch headers,
// the change counts, and — from Parse only — the entry lists behind those counts. The zero
// Snapshot is a clean tree on an unnamed branch with no upstream.
type Snapshot struct {
	// Branch is the current branch; empty when the header is absent or HEAD is detached.
	Branch string
	// Detached reports that HEAD is not on a branch.
	Detached bool
	// Upstream is the upstream ref, empty when the branch tracks nothing.
	Upstream string
	// HasAheadBehind reports that an ahead/behind header was present (only ever with an
	// upstream); Ahead and Behind are meaningful only when it is true.
	HasAheadBehind bool
	// Ahead is how many commits the branch has that its upstream lacks.
	Ahead int
	// Behind is how many commits the upstream has that the branch lacks.
	Behind int

	// Dirty counts each changed path once: an ordinary change, a rename (counted once, not
	// as two paths), an unmerged path and an untracked path. A path both staged and edited
	// again is one path, so it counts once here while it counts in both Staged and Unstaged.
	Dirty int
	// Staged counts the entries whose index differs from HEAD.
	Staged int
	// Unstaged counts the entries whose working tree differs from the index, unmerged paths
	// included.
	Unstaged int
	// Untracked counts the untracked paths.
	Untracked int

	// StagedEntries lists each staged entry as "<code>  <path>" (a rename adds
	// " (from <origin>)"). Parse fills it; Counter leaves it nil.
	StagedEntries []string
	// UnstagedEntries lists each unstaged entry the same way, an unmerged path as
	// "U  <path> (unmerged)". Parse fills it; Counter leaves it nil.
	UnstagedEntries []string
	// UntrackedEntries lists each untracked path verbatim. Parse fills it; Counter leaves it nil.
	UntrackedEntries []string
}

// Parse reads a whole `git status --porcelain=v2 --branch -z` capture into a Snapshot, entry
// lists included. A record it does not recognise is skipped rather than failing the read: git
// may prepend a warning when stderr is captured alongside, and a status that is mostly
// readable beats none.
func Parse(out string) Snapshot {
	var s Snapshot
	records := strings.Split(out, recordTerminator)
	for i := 0; i < len(records); i++ {
		next := ""
		if i+1 < len(records) {
			next = records[i+1]
		}
		if s.fold(records[i], next, true) {
			i++
		}
	}
	return s
}

// Counter folds a `git status --porcelain=v2 --branch -z` stream into a Snapshot as it is
// written, with Parse's counts and branch fields but no entry lists. A record may be split
// across writes anywhere; Counter holds at most the one partial record in flight, so a stream
// of any size is counted exactly in bounded memory. The zero Counter is ready to use; it is not
// safe for concurrent writes.
type Counter struct {
	snapshot Snapshot
	partial  []byte
	// skipOrigin is set after a rename record: the record after it is the rename's original
	// path, which Counter does not keep and must not read as a record of its own.
	skipOrigin bool
}

// Write folds every record p completes and keeps the unterminated tail for the next write.
// It never fails: an unrecognised record is skipped, as Parse skips it.
func (c *Counter) Write(p []byte) (int, error) {
	rest := p
	for {
		end := bytes.IndexByte(rest, recordTerminator[0])
		if end < 0 {
			break
		}
		record := rest[:end]
		if len(c.partial) > 0 {
			c.partial = append(c.partial, record...)
			record = c.partial
		}
		c.foldRecord(string(record))
		c.partial = c.partial[:0]
		rest = rest[end+1:]
	}
	c.partial = append(c.partial, rest...)
	return len(p), nil
}

// Snapshot reports the counts and branch fields folded so far. An unterminated tail is read as
// a final record, as Parse reads the text after the last NUL, without consuming it: a later
// Write that completes the record still folds it once.
func (c *Counter) Snapshot() Snapshot {
	s := c.snapshot
	if len(c.partial) > 0 && !c.skipOrigin {
		s.fold(string(c.partial), "", false)
	}
	return s
}

// foldRecord folds one complete record, skipping it when it is the original path of the
// rename before it.
func (c *Counter) foldRecord(record string) {
	if c.skipOrigin {
		c.skipOrigin = false
		return
	}
	c.skipOrigin = c.snapshot.fold(record, "", false)
}

// fold reads one record into s. next is the record after it, which a rename takes as its
// original path; fold reports true when it did, so the caller skips that record. keepEntries
// says whether the entry lists are filled as well as the counts.
func (s *Snapshot) fold(record, next string, keepEntries bool) (consumedNext bool) {
	switch {
	case strings.HasPrefix(record, headPrefix):
		head := strings.TrimSpace(strings.TrimPrefix(record, headPrefix))
		if head == detachedHead {
			s.Detached = true
		} else {
			s.Branch = head
		}
	case strings.HasPrefix(record, upstreamPrefix):
		s.Upstream = strings.TrimSpace(strings.TrimPrefix(record, upstreamPrefix))
	case strings.HasPrefix(record, aheadPrefix):
		s.Ahead, s.Behind, s.HasAheadBehind = parseAheadBehind(strings.TrimPrefix(record, aheadPrefix))
	case strings.HasPrefix(record, ordinaryPrefix):
		if fields, ok := porcelainFields(record, ordinaryFields); ok {
			s.addChange(fields[1], fields[ordinaryFields-1], "", keepEntries)
		}
	case strings.HasPrefix(record, renamePrefix):
		// The one record that spans two: the ORIGINAL path follows as the next record.
		if fields, ok := porcelainFields(record, renameFields); ok {
			s.addChange(fields[1], fields[renameFields-1], next, keepEntries)
			return true
		}
	case strings.HasPrefix(record, unmergedPrefix):
		// A conflicted path is reported once, under unstaged — it is work the tree still owes,
		// not a change already staged.
		if fields, ok := porcelainFields(record, unmergedFields); ok {
			s.Dirty++
			s.Unstaged++
			if keepEntries {
				s.UnstagedEntries = append(s.UnstagedEntries, "U  "+fields[unmergedFields-1]+" (unmerged)")
			}
		}
	case strings.HasPrefix(record, untrackedPrefix):
		s.Dirty++
		s.Untracked++
		if keepEntries {
			s.UntrackedEntries = append(s.UntrackedEntries, strings.TrimPrefix(record, untrackedPrefix))
		}
	}
	return false
}

// addChange counts one ordinary or renamed entry under the kinds its XY code selects: X (index
// vs HEAD) makes it staged, Y (working tree vs index) unstaged, and a path that is both —
// staged then edited again — is counted in both, which is exactly what git reports, yet is one
// Dirty path. A code that is not two characters is a malformed record and counts nowhere.
func (s *Snapshot) addChange(xy, path, origin string, keepEntries bool) {
	if len(xy) != 2 {
		return
	}
	s.Dirty++
	suffix := ""
	if origin != "" {
		suffix = " (from " + origin + ")"
	}
	if xy[0] != '.' {
		s.Staged++
		if keepEntries {
			s.StagedEntries = append(s.StagedEntries, string(xy[0])+"  "+path+suffix)
		}
	}
	if xy[1] != '.' {
		s.Unstaged++
		if keepEntries {
			s.UnstagedEntries = append(s.UnstagedEntries, string(xy[1])+"  "+path+suffix)
		}
	}
}

// porcelainFields splits a porcelain v2 record into n space-separated fields, the last of which
// is the trailing path (which may itself contain spaces, so the split is bounded). It reports
// false for a record with too few fields — a truncated or unexpected line the caller skips.
func porcelainFields(record string, n int) ([]string, bool) {
	fields := strings.SplitN(record, " ", n)
	if len(fields) < n {
		return nil, false
	}
	return fields, true
}

// parseAheadBehind reads the "+<ahead> -<behind>" pair of the branch.ab header. It reports
// false when the header is malformed, in which case the divergence is simply not stated.
func parseAheadBehind(header string) (ahead, behind int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
		return 0, 0, false
	}
	ahead, err := strconv.Atoi(fields[0][1:])
	if err != nil {
		return 0, 0, false
	}
	behind, err = strconv.Atoi(fields[1][1:])
	if err != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}
