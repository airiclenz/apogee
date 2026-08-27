package skills

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// bm25K1 is BM25's term-frequency saturation parameter: how fast a term's contribution stops
// growing as it repeats. 1.2 is the standard value and the right end of the range for documents
// this short — an id plus a display name plus a summary — where a third occurrence of a word says
// almost nothing a second did not.
const bm25K1 = 1.2

// bm25B is BM25's length-normalisation weight: 0.75 is the standard value, and it matters here
// because skill documents differ in length by an order of magnitude (a bare id versus a summary
// with thirty triggers). Without it the wordiest SKILL.md would win every draft.
const bm25B = 0.75

// defaultSuggestLimit is how many suggestions Suggest returns when the caller names no limit —
// three, the width the one-row band can show without clipping the last entry to nothing.
const defaultSuggestLimit = 3

// minContentWords is how many DISTINCT non-stopword draft terms must exist before anything is
// suggested at all. Below three the draft is still a fragment ("fix the") and every match is an
// accident, so the band stays dark rather than flickering a guess at the user on every keystroke.
const minContentWords = 3

// minMatchedTerms is the evidence gate: absent a trigger hit, a skill is admitted only when at
// least this many distinct draft terms appear in its document. One shared word is the noise floor
// — "code", "file", "test" appear in half a library — so a single match never earns a row.
const minMatchedTerms = 2

// minTokenRunes drops one-rune fragments ("a", "I", the "s" left by a possessive) before they can
// become terms: they carry no topic and inflate every document's length.
const minTokenRunes = 2

// minStemRunes guards the stemmer: a suffix is only stripped when at least this much word is left,
// so "goes" and "ties" keep their shape instead of collapsing into two-letter noise that collides
// with unrelated words.
const minStemRunes = 3

// triggerBoostIDFs sizes the trigger boost in units of the corpus's maximum single-term IDF: an
// author who declared "cut a release" and saw it typed verbatim has said more than any one rare
// word could, so a hit is worth twice the corpus's rarest term. It is added ON TOP of the BM25
// score rather than replacing it, so two triggered skills — and two untriggered ones — still order
// among themselves lexically.
const triggerBoostIDFs = 2.0

// stopwords are the English function words dropped before scoring, plus the request verbs a chat
// draft opens with ("please", "want", "need", "make", "use"). They appear in nearly every draft and
// in many summaries, so leaving them in would let politeness alone admit a skill.
var stopwords = map[string]bool{
	"about": true, "after": true, "all": true, "also": true, "am": true, "an": true,
	"and": true, "any": true, "are": true, "as": true, "at": true, "be": true,
	"because": true, "been": true, "but": true, "by": true, "can": true, "could": true,
	"did": true, "do": true, "does": true, "for": true, "from": true, "get": true,
	"had": true, "has": true, "have": true, "how": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "just": true, "like": true,
	"make": true, "may": true, "me": true, "my": true, "need": true, "not": true,
	"of": true, "on": true, "or": true, "our": true, "out": true, "over": true,
	"please": true, "should": true, "so": true, "some": true, "than": true, "that": true,
	"the": true, "their": true, "them": true, "then": true, "there": true, "these": true,
	"they": true, "this": true, "to": true, "up": true, "us": true, "use": true,
	"want": true, "was": true, "we": true, "were": true, "what": true, "when": true,
	"which": true, "will": true, "with": true, "would": true, "you": true, "your": true,
}

// stemSuffixes is the whole stemmer: one ordered pass, first match wins. It is deliberately not a
// Porter implementation — the corpus is a few hundred words of hand-written prose, and the only
// job is to let "audits", "auditing" and "audit" meet. Order matters: "ies" before "es" before "s"
// so "utilities" becomes "utility" rather than "utilitie".
var stemSuffixes = []struct{ from, to string }{
	{"ies", "y"},
	{"es", ""},
	{"s", ""},
	{"ing", ""},
	{"ed", ""},
}

// Suggestion is one ranked match between a draft and a skill: enough to paint a row (ID,
// DisplayName, Summary) plus why it ranked where it did (Score, TriggerHit). It is a host-side
// value — nothing here ever reaches the model, which learns a skill exists only when the user
// attaches it as a "/id" (ADR 0061).
type Suggestion struct {
	ID          string
	DisplayName string
	Summary     string
	// Score is the BM25 score over the draft's distinct terms, plus the trigger boost when
	// TriggerHit. It is comparable only within one Suggest call — it is not a probability and
	// carries no threshold a caller should test against.
	Score float64
	// TriggerHit reports that one of the skill's authored trigger phrases appeared verbatim in
	// the draft. A hit admits the skill on its own; lexical matches need minMatchedTerms.
	TriggerHit bool
}

// Suggest ranks the catalog against draft and returns the best matches, strongest first.
//
// A skill is admitted when one of its trigger phrases appears verbatim in the draft OR at least
// minMatchedTerms distinct draft terms appear in its document (id + display name + summary +
// triggers; bodies are never indexed). Admitted skills are ordered by score descending, then by ID
// ascending so ties are stable across calls.
//
// It returns nil — never a partial guess — when the draft holds fewer than minContentWords
// distinct content words, when nothing clears the gate, or when the catalog was never finalized.
// exclude, when non-nil, drops a skill by ID before ranking: the caller's set of skills already
// invoked in the draft and already spent this session. limit caps the result; zero or negative
// means defaultSuggestLimit.
func (c *Catalog) Suggest(draft string, exclude func(id string) bool, limit int) []Suggestion {
	if c == nil || c.idx == nil {
		return nil
	}
	if limit <= 0 {
		limit = defaultSuggestLimit
	}
	draftTokens := tokenize(draft)
	queryTerms := distinctTerms(draftTokens)
	if len(queryTerms) < minContentWords {
		return nil
	}

	out := make([]Suggestion, 0, len(c.idx.docs))
	for _, doc := range c.idx.docs {
		if exclude != nil && exclude(doc.id) {
			continue
		}
		triggerHit := doc.hasTriggerHit(draftTokens)
		score, matched := c.idx.score(doc, queryTerms)
		if !triggerHit && matched < minMatchedTerms {
			continue
		}
		if triggerHit {
			score += c.idx.triggerBoost
		}
		s := c.byID[doc.id]
		out = append(out, Suggestion{
			ID:          s.ID,
			DisplayName: s.DisplayName,
			Summary:     s.Summary,
			Score:       score,
			TriggerHit:  triggerHit,
		})
	}
	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// index is the immutable BM25 index over one catalog's skills, built once by buildIndex at the end
// of a scan and never mutated afterwards. That is what lets Suggest run lock-free on the UI
// goroutine while the loop goroutine resolves against the same snapshot (Provider swaps whole
// catalogs, it never edits one).
type index struct {
	// docs holds one document per skill in ascending ID order, so a Suggest that ends in a score
	// tie sees the same order every call before it even sorts.
	docs []document
	// docFreq is how many documents each term appears in — BM25's IDF input.
	docFreq map[string]int
	// avgLen is the mean document length in terms; zero for an empty corpus, which makes score
	// return zero rather than divide by it.
	avgLen float64
	// triggerBoost is the corpus-wide bonus a trigger hit adds, precomputed from the maximum IDF
	// so it costs nothing per keystroke.
	triggerBoost float64
}

// document is one skill as the matcher sees it: a bag of terms with its length, plus the tokenised
// trigger phrases kept as SEQUENCES because a trigger matches contiguously and the bag cannot say
// whether "cut" and "release" were adjacent.
type document struct {
	id       string
	terms    map[string]int
	length   int
	triggers [][]string
}

// buildIndex indexes every skill in byID. The document is id + display name + summary + every
// trigger phrase: triggers are ordinary document text for BM25 (an author's phrasing is evidence
// like any other), and their contiguous-phrase boost is applied separately in Suggest. Bodies are
// deliberately absent — indexing a whole SKILL.md would let one long skill match every draft.
func buildIndex(byID map[string]Skill) *index {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	idx := &index{
		docs:    make([]document, 0, len(ids)),
		docFreq: make(map[string]int, len(ids)*8),
	}
	totalLen := 0
	for _, id := range ids {
		s := byID[id]
		doc := document{id: id, terms: map[string]int{}}
		fields := strings.Join([]string{s.ID, s.DisplayName, s.Summary, strings.Join(s.Triggers, " ")}, " ")
		for _, term := range tokenize(fields) {
			doc.terms[term]++
			doc.length++
		}
		for _, phrase := range s.Triggers {
			if seq := tokenize(phrase); len(seq) > 0 {
				doc.triggers = append(doc.triggers, seq)
			}
		}
		for term := range doc.terms {
			idx.docFreq[term]++
		}
		totalLen += doc.length
		idx.docs = append(idx.docs, doc)
	}
	if len(idx.docs) > 0 {
		idx.avgLen = float64(totalLen) / float64(len(idx.docs))
	}
	idx.triggerBoost = triggerBoostIDFs * idx.maxIDF()
	return idx
}

// idf is BM25's probabilistic inverse document frequency in its non-negative form: a term in every
// document still scores slightly above zero rather than turning negative and penalising a match.
// An unknown term scores zero.
func (x *index) idf(term string) float64 {
	df := float64(x.docFreq[term])
	if df == 0 {
		return 0
	}
	n := float64(len(x.docs))
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// maxIDF is the rarest single term's IDF across the corpus — the unit the trigger boost is
// measured in. Iterating the docFreq map is order-independent because a maximum is.
func (x *index) maxIDF() float64 {
	best := 0.0
	for term := range x.docFreq {
		if v := x.idf(term); v > best {
			best = v
		}
	}
	return best
}

// score returns doc's BM25 score over queryTerms and how many of them it matched — the two halves
// Suggest needs, computed in one walk so the evidence gate never costs a second pass. queryTerms
// must already be distinct; a repeated draft word must not count twice.
func (x *index) score(doc document, queryTerms []string) (float64, int) {
	if x.avgLen == 0 {
		return 0, 0
	}
	total, matched := 0.0, 0
	for _, term := range queryTerms {
		tf := doc.terms[term]
		if tf == 0 {
			continue
		}
		matched++
		freq := float64(tf)
		norm := freq + bm25K1*(1-bm25B+bm25B*float64(doc.length)/x.avgLen)
		total += x.idf(term) * (freq * (bm25K1 + 1)) / norm
	}
	return total, matched
}

// hasTriggerHit reports whether any of the document's trigger phrases appears as a contiguous run
// of tokens in the draft. Both sides went through tokenize, so the comparison is immune to casing,
// punctuation and the stopwords sitting between the words the author wrote ("cut a release" and
// "cut the release" are the same phrase here).
func (d document) hasTriggerHit(draftTokens []string) bool {
	for _, phrase := range d.triggers {
		if containsSequence(draftTokens, phrase) {
			return true
		}
	}
	return false
}

// containsSequence reports whether needle appears as a contiguous subsequence of haystack. An
// empty needle never matches: a trigger phrase made entirely of stopwords must not hit every draft.
func containsSequence(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if slices.Equal(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

// tokenize turns free text into the matcher's term sequence: lowercased, cut on every rune that is
// neither a letter nor a digit (so "code-audit" contributes "code" and "audit", and an id matches
// the words a human types), with sub-minTokenRunes fragments and stopwords dropped and one
// stemming pass applied to what is left.
//
// It is the ONE normalisation both the corpus and the draft pass through: a term can only ever
// match a term that came out of this function, which is why the index and the query can never
// disagree about what a word is.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len([]rune(field)) < minTokenRunes || stopwords[field] {
			continue
		}
		out = append(out, stem(field))
	}
	return out
}

// stem strips at most ONE inflectional suffix from word, in stemSuffixes order, and only when at
// least minStemRunes remain. A suffix whose removal would leave too little keeps the whole word
// rather than falling through to the next rule — one pass, so the result is a pure function of the
// first rule that applies.
func stem(word string) string {
	for _, suffix := range stemSuffixes {
		if !strings.HasSuffix(word, suffix.from) {
			continue
		}
		stemmed := word[:len(word)-len(suffix.from)] + suffix.to
		if len([]rune(stemmed)) >= minStemRunes {
			return stemmed
		}
		return word
	}
	return word
}

// distinctTerms drops repeats while keeping first-seen order — the draft's query terms. BM25 sums
// over DISTINCT query terms, so typing a word twice must not double its weight.
func distinctTerms(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
