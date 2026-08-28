package skills

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
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

// minDraftWords is how many whitespace-separated words the draft must hold before anything is
// suggested at all. It counts the words a person sees, stopwords included — the band must not go
// dark because "me", "on" and "this" were dropped — and below three the draft is still a fragment
// ("fix the") where every match is an accident, so the band stays dark rather than flickering a
// guess at the user on every keystroke. At least one content term must survive tokenising as well:
// a draft of pure stopwords has nothing to score.
const minDraftWords = 3

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

// minPrefixRunes is the shortest term that may match another by prefix: four runes is the shortest
// stem that names a topic — "plan", "test", "code" — and below it "run" would claim "running" and
// "rune" alike. Either side may be the prefix ("relea" finds "release", "plan" finds "plann"), but
// the shorter side must reach this floor.
const minPrefixRunes = 4

// nameBonusIDFs sizes the id / display-name bonus in units of the matched term's own IDF: a word
// the author put in the skill's NAME is the skill's topic, while a summary mention is merely a use
// of it. One IDF's worth keeps an id hit ahead of any single summary repeat without drowning a
// two-term summary match. It is added beside BM25, never inside its term frequency, so it neither
// saturates with k1 nor lengthens the document.
const nameBonusIDFs = 1.0

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

// stemSuffixes is the whole stemmer: one ordered pass, the first rule that applies wins. It is
// deliberately not a Porter implementation — the corpus is a few hundred words of hand-written
// prose, and the only job is to let "audits", "auditing" and "audit" meet. Order matters: "ies"
// before "es" before "s" so "utilities" becomes "utility" rather than "utilitie". A rule's guard,
// when set, may decline the word — and unlike the length floor, a guard's refusal lets the NEXT
// rule try, which is how "holes" passes "es" and lands on "hole" under "s".
var stemSuffixes = []struct {
	from, to string
	// applies reports whether the rule may take word, given the stem it would leave; nil means
	// always.
	applies func(word, stemmed string) bool
}{
	{"ies", "y", nil},
	// "es" only after a sibilant — boxes, wishes, classes — where the e belongs to the suffix;
	// elsewhere it belongs to the word (holes, releases, changes) and the "s" rule takes it.
	{"es", "", func(_, stemmed string) bool { return hasAnySuffix(stemmed, "x", "z", "ch", "sh", "ss") }},
	// "s" never strips from a word ending in ss or us — stress, process, status, focus — where the
	// s is no plural at all.
	{"s", "", func(word, _ string) bool { return !hasAnySuffix(word, "ss", "us") }},
	{"ing", "", nil},
	// "ed" never strips when the stem would end in e — speed, need, feed, agreed — where the ed is
	// part of the word, not a tense.
	{"ed", "", func(_, stemmed string) bool { return !strings.HasSuffix(stemmed, "e") }},
}

// Suggestion is one ranked match between a draft and a skill: enough to paint a row (ID,
// DisplayName, Summary) plus why it ranked where it did (Score, TriggerHit). It is a host-side
// value — nothing here ever reaches the model, which learns a skill exists only when the user
// attaches it as a "/id" (ADR 0061).
type Suggestion struct {
	ID          string
	DisplayName string
	Summary     string
	// Score is the BM25 score over the draft's distinct terms, plus the name bonus for every term
	// that hit the skill's id or display name, plus the trigger boost when TriggerHit. It is
	// comparable only within one Suggest call — it is not a probability and carries no threshold
	// a caller should test against.
	Score float64
	// TriggerHit reports that one of the skill's authored trigger phrases appeared verbatim in
	// the draft. A hit admits the skill on its own; lexical matches need minMatchedTerms.
	TriggerHit bool
}

// Suggest ranks the catalog against draft and returns the best matches, strongest first.
//
// A skill is admitted when one of its trigger phrases appears verbatim in the draft OR at least
// minMatchedTerms distinct draft terms appear in its document (id + display name + full
// description + triggers; bodies are never indexed). A draft term appears when the document holds it exactly or
// holds a term it shares a prefix with either way, the shorter side at least minPrefixRunes long.
// Admitted skills are ordered by score descending, then by ID ascending so ties are stable across
// calls.
//
// It returns nil — never a partial guess — when the draft holds fewer than minDraftWords words
// (stopwords included) or no content term at all, when nothing clears the gate, or when the
// catalog was never finalized.
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
	if len(strings.Fields(draft)) < minDraftWords || len(queryTerms) == 0 {
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

// document is one skill as the matcher sees it: a bag of terms with its length, the subset of those
// terms that came from the id or display name (the name bonus asks for it per hit), plus the
// tokenised trigger phrases kept as SEQUENCES because a trigger matches contiguously and the bag
// cannot say whether "cut" and "release" were adjacent.
type document struct {
	id        string
	terms     map[string]int
	nameTerms map[string]bool
	length    int
	triggers  [][]string
}

// buildIndex indexes every skill in byID. The document is id + display name + description + every
// trigger phrase: the description is the summary's full source text (Skill.Description) rather
// than the menu-clamped Summary, so a phrase an author placed past the 200-rune cap still finds
// the skill; a Skill built without one falls back to its Summary. Triggers are ordinary document
// text for BM25 (an author's phrasing is evidence like any other), and their contiguous-phrase
// boost is applied separately in Suggest. Bodies are deliberately absent — indexing a whole
// SKILL.md would let one long skill match every draft.
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
		doc := document{id: id, terms: map[string]int{}, nameTerms: map[string]bool{}}
		fields := strings.Join([]string{s.ID, s.DisplayName, firstNonEmpty(s.Description, s.Summary), strings.Join(s.Triggers, " ")}, " ")
		for _, term := range tokenize(fields) {
			doc.terms[term]++
			doc.length++
		}
		for _, term := range tokenize(s.ID + " " + s.DisplayName) {
			doc.nameTerms[term] = true
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

// score returns doc's score over queryTerms and how many of them it matched — the two halves
// Suggest needs, computed in one walk so the evidence gate never costs a second pass. Each query
// term lands on at most one document term (see match); its BM25 contribution uses that term's
// frequency and IDF — the draft's spelling has no document frequency of its own — and a term
// found in the id or display name adds the name bonus on top. queryTerms must already be
// distinct; a repeated draft word must not count twice.
func (x *index) score(doc document, queryTerms []string) (float64, int) {
	if x.avgLen == 0 {
		return 0, 0
	}
	total, matched := 0.0, 0
	for _, q := range queryTerms {
		term, tf := doc.match(q)
		if tf == 0 {
			continue
		}
		matched++
		idf := x.idf(term)
		freq := float64(tf)
		norm := freq + bm25K1*(1-bm25B+bm25B*float64(doc.length)/x.avgLen)
		total += idf * (freq * (bm25K1 + 1)) / norm
		if doc.nameTerms[term] {
			total += nameBonusIDFs * idf
		}
	}
	return total, matched
}

// match finds the document term a query term lands on and that term's frequency: the exact term
// when the document holds it, otherwise the best of the document's terms that share a prefix with
// q either way with the shorter side at least minPrefixRunes long — highest frequency first,
// then the lexicographically smallest, so a map walk cannot make two calls disagree. It returns
// ("", 0) when nothing matches. The scan is O(document terms), which over a corpus of dozens of
// ~30-term documents is a few thousand comparisons per keystroke.
func (d document) match(q string) (term string, tf int) {
	if exact := d.terms[q]; exact > 0 {
		return q, exact
	}
	// Whichever side is shorter must reach the floor, and the query is the shorter side whenever
	// a longer document term is to be found — so a short query can match nothing by prefix.
	if utf8.RuneCountInString(q) < minPrefixRunes {
		return "", 0
	}
	for candidate, count := range d.terms {
		if !isPrefixEitherWay(q, candidate) {
			continue
		}
		if count > tf || (count == tf && candidate < term) {
			term, tf = candidate, count
		}
	}
	return term, tf
}

// isPrefixEitherWay reports whether one of a and b is a proper prefix of the other and the shorter
// of them is at least minPrefixRunes long. Equal strings are an exact match, not a prefix one, and
// are the caller's business.
func isPrefixEitherWay(a, b string) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) == len(long) || utf8.RuneCountInString(short) < minPrefixRunes {
		return false
	}
	return strings.HasPrefix(long, short)
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
// rather than falling through to the next rule; a suffix whose guard declines lets the next rule
// try. One pass either way, so the result is a pure function of the first rule that applies.
func stem(word string) string {
	for _, suffix := range stemSuffixes {
		if !strings.HasSuffix(word, suffix.from) {
			continue
		}
		stemmed := word[:len(word)-len(suffix.from)] + suffix.to
		if len([]rune(stemmed)) < minStemRunes {
			return word
		}
		if suffix.applies != nil && !suffix.applies(word, stemmed) {
			continue
		}
		return stemmed
	}
	return word
}

// hasAnySuffix reports whether s ends in any of the given suffixes — the stemmer's guards ask
// "does this end in a sibilant" and "is this an ss/us word" in one breath.
func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
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
