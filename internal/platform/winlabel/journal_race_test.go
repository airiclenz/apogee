package winlabel

import (
	"fmt"
	"sync"
	"testing"
)

// The Journal's promise of concurrency safety is proved rather than asserted in a comment: two
// goroutines record into one journal while two more read it through the exported accessors,
// under -race. Nothing in production reaches this state today — Confine is driven from one
// goroutine at a time — but the type is what the backend now composes instead of a mutex it
// held itself, and the whole point of moving the lock in was that the backend can no longer
// get this wrong on its own.
//
// The writers take the lock explicitly and call the unexported body, which is what the label
// walk does in production: the walk is Windows-tagged, so this test stands in for it on the
// machine the package is developed on.
func TestJournalIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const writers, readers, perWriter = 4, 2, 16

	j := Open(t.TempDir())
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				path := fmt.Sprintf(`C:\work\%d\%d`, w, i)
				j.mu.Lock()
				_, err := j.record(Entry{Path: path, PriorSDDL: "S:AI(ML;;NW;;;ME)"})
				j.mu.Unlock()
				if err != nil {
					t.Errorf("record %q: %v", path, err)
					return
				}
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = j.Writable()
				_ = j.Entries()
				_ = j.PriorLabels()
				j.MarkLabelled(fmt.Sprintf(`C:\work\%d`, i))
			}
		}()
	}
	wg.Wait()

	if got := len(j.Entries()); got != writers*perWriter {
		t.Errorf("entries = %d, want %d — every concurrent record must survive", got, writers*perWriter)
	}
}
