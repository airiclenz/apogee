package library

import (
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The stored observation (ported from apogee-sim internal/library/entry.go @pin)
// ----------------------------------------------------------------------------

// Category classifies what kind of observation an Entry represents. The values are the
// pinned sim's, so a bench comparing the two stores reads the same taxonomy.
type Category string

const (
	// CategoryCorrection is a recorded correction for a model's malformed output (an unknown
	// tool name, a missing parameter, invalid JSON) — evidence the model needs a nudge.
	CategoryCorrection Category = "correction"

	// CategoryBehavioral is a recorded behavioural tendency (e.g. narrating instead of
	// calling a tool) that a pre-request hint can counteract.
	CategoryBehavioral Category = "behavioral"

	// CategoryExample is a recorded example of a valid, complex tool call worth showing the
	// model again.
	CategoryExample Category = "example"
)

// scoreCap bounds the Bayesian score so a long run of pure observations never certifies a
// pattern as absolute (ported from the sim — headroom for the success signal to move it).
const scoreCap = 0.95

// Entry is a single observation the Library holds, keyed on a ModelFingerprint. Observations
// counts how many times the pattern was seen; Successes counts the opposite signal (the model
// later did the right thing). The Bayesian Score falls as Successes accumulate, so a pattern
// the model grows out of naturally stops qualifying for injection without being deleted.
type Entry struct {
	ID           string                       `json:"id"`
	Category     Category                     `json:"category"`
	ModelLabel   string                       `json:"model_label"`
	Confidence   domain.FingerprintConfidence `json:"confidence"`
	Tags         []string                     `json:"tags"`
	Content      string                       `json:"content"`
	Observations int                          `json:"observations"`
	Successes    int                          `json:"successes"`
	CreatedAt    time.Time                    `json:"created_at"`
	LastUsed     time.Time                    `json:"last_used"`
	TTLHours     int                          `json:"ttl_hours"`
}

// Score returns the Bayesian confidence that the Entry is a genuine failure pattern:
// (observations - successes + 1) / (observations + 2), capped at scoreCap. Higher means more
// likely a real pattern; the score drops toward the prior as Successes accumulate.
func (e *Entry) Score() float64 {
	obs := e.Observations
	if obs <= 0 {
		obs = 1
	}
	score := float64(obs-e.Successes+1) / float64(obs+2)
	if score > scoreCap {
		return scoreCap
	}
	return score
}

// Expired reports whether the Entry has outlived its TTL. A non-positive TTLHours means the
// Entry never expires.
//
// Expiry keys on CreatedAt and is deliberately NOT refreshed by re-observation: an entry
// reinforced within its TTL window still expires at CreatedAt + TTL, not from its last sighting.
// This is a sim-faithful port choice, not an oversight — the pinned sim's Entry.Expired keys on
// CreatedAt too, and its Store.Record match path bumps Observations/LastUsed/Content but leaves
// CreatedAt untouched (apogee-sim internal/library/{entry.go,store.go} @pin). Changing it here
// would diverge the two stores' eviction behaviour, which the bench compares.
func (e *Entry) Expired(now time.Time) bool {
	if e.TTLHours <= 0 {
		return false
	}
	return now.Sub(e.CreatedAt) > time.Duration(e.TTLHours)*time.Hour
}

// HasTag reports whether the Entry carries the given tag.
func (e *Entry) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
