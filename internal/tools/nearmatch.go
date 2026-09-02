package tools

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxNearMatchExcerptLines caps how many numbered file lines a not-found report quotes back. A
// near-miss report is a POINTER, not a listing: twelve lines place the region a model has to look
// at, and an old text of a hundred lines cannot turn one failed edit into a re-read of the file.
const maxNearMatchExcerptLines = 12

// nearMatchCutMarker closes an excerpt the cap cut short, indented to sit under the numbered rows.
const nearMatchCutMarker = "  …"

// notFoundInFile is the bare not-found sentence the find-replace tools have always answered with.
// closestRegion returns it UNCHANGED to the byte when it has nothing to add, mirroring
// notFoundMessage's contract in path_suggest.go: a report only ever grows on the wording, so a
// caller that concatenated the literal keeps the message it had.
const notFoundInFile = "old text not found in file"

// closestRegion renders a find-replace not-found refusal for the model: the former sentence, plus
// the region of the file that came closest to the text it asked to replace.
//
// It is REPORT ONLY. Nothing here is applied — the near match is never substituted for the exact
// text the model asked for, and the caller has already refused the edit — so a model reading this
// learns where to look and re-issues the call itself, spelled the way the file actually reads.
//
// Three answers, in order:
//
//   - whitespace-normalised match, exactly once: the old text is in the file but spelled with
//     different leading/trailing spaces, tabs or blank lines. The report names the line range and
//     quotes it, so the model can copy the file's own spelling.
//   - closest window: no whitespace-insensitive match, but some window of the file's lines shares
//     at least one trimmed line with the old text. The best-scoring window (earliest wins ties) is
//     named with its score and quoted.
//   - nothing: not one line of the old text appears anywhere, so there is no region to point at
//     and the message is the bare sentence.
//
// Quoted lines pass through escapeRowBreaks: file text is DATA inside a report the model reads as
// the tool's words, and a lone \r inside a quoted line must not forge a row of its own.
func closestRegion(content, old string) string {
	fileLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

	if start, end, ok := uniqueNormalizedMatch(fileLines, oldLines); ok {
		return notFoundInFile +
			fmt.Sprintf(" — found at lines %d–%d with different whitespace "+
				"(leading/trailing spaces, tabs or blank lines); the file is unchanged:", start, end) +
			"\n" + numberedExcerpt(fileLines, start, end)
	}

	if start, end, score, ok := closestWindow(fileLines, oldLines); ok {
		return notFoundInFile +
			fmt.Sprintf(" — closest match at lines %d–%d (%d of %d lines match); the file is unchanged:",
				start, end, score, len(oldLines)) +
			"\n" + numberedExcerpt(fileLines, start, end)
	}

	return notFoundInFile
}

// occurrenceLines answers the 1-based line number of the first character of every non-overlapping
// occurrence of old in content, in file order. It mirrors countOccurrences' counting exactly — the
// same non-overlapping scan strings.Count performs — so a "found N times" refusal and the lines it
// names can never disagree about how many there are. An empty old text yields no lines, as
// countOccurrences yields no occurrences.
func occurrenceLines(content, old string) []int {
	if old == "" {
		return nil
	}

	var lines []int
	for offset := 0; offset <= len(content); {
		i := strings.Index(content[offset:], old)
		if i < 0 {
			break
		}
		at := offset + i
		lines = append(lines, strings.Count(content[:at], "\n")+1)
		offset = at + len(old)
	}
	return lines
}

// occurrenceNote renders the clause a "found N times" refusal gains: " — at lines 4, 17, 92". It is
// empty when there is nothing to name, so the former wording survives to the byte.
func occurrenceNote(content, old string) string {
	lines := occurrenceLines(content, old)
	if len(lines) == 0 {
		return ""
	}

	spelled := make([]string, len(lines))
	for i, n := range lines {
		spelled[i] = strconv.Itoa(n)
	}
	return " — at lines " + strings.Join(spelled, ", ")
}

// normalizeLine is the whitespace-insensitive spelling of one line: trimmed at both ends with every
// internal run of blanks collapsed to a single space. A line that holds nothing else normalises to
// the empty string, which is how a blank line is recognised and dropped.
func normalizeLine(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

// normalizedLines answers the non-blank lines of a text in normalised form, each paired with the
// 1-based file line it came from. Blank lines are dropped, so a run that differs from the file only
// by an interleaved empty line still lines up.
func normalizedLines(lines []string) (texts []string, numbers []int) {
	texts = make([]string, 0, len(lines))
	numbers = make([]int, 0, len(lines))
	for i, line := range lines {
		normalized := normalizeLine(line)
		if normalized == "" {
			continue
		}
		texts = append(texts, normalized)
		numbers = append(numbers, i+1)
	}
	return texts, numbers
}

// uniqueNormalizedMatch reports the 1-based line range of the file's ONLY whitespace-insensitive
// occurrence of the old text. ok is false when there is no such occurrence, when there is more than
// one — an ambiguous pointer is worse than none, and the exactly-once rule matches the tools' own
// contract — or when the old text is entirely blank, which matches nothing worth naming.
func uniqueNormalizedMatch(fileLines, oldLines []string) (start, end int, ok bool) {
	oldTexts, _ := normalizedLines(oldLines)
	fileTexts, fileNumbers := normalizedLines(fileLines)
	if len(oldTexts) == 0 || len(fileTexts) < len(oldTexts) {
		return 0, 0, false
	}

	matches := 0
	for i := 0; i+len(oldTexts) <= len(fileTexts); i++ {
		if !slices.Equal(fileTexts[i:i+len(oldTexts)], oldTexts) {
			continue
		}
		matches++
		if matches > 1 {
			return 0, 0, false
		}
		start, end = fileNumbers[i], fileNumbers[i+len(oldTexts)-1]
	}
	if matches != 1 {
		return 0, 0, false
	}
	return start, end, true
}

// closestWindow scores every window of len(oldLines) consecutive file lines by how many of its
// trimmed lines equal the trimmed old line in the same position, and answers the best-scoring one
// as a 1-based inclusive range. Ties go to the EARLIEST window, so the same file and old text
// always name the same region. ok is false when nothing scores — not one line of the old text
// appears in the right relative place — which is the caller's "no region to point at" case.
//
// A blank old line never scores: a window of empty lines lines up with any other window of empty
// lines, and calling that a closest match would point the model at whitespace.
func closestWindow(fileLines, oldLines []string) (start, end, score int, ok bool) {
	if len(oldLines) == 0 || len(fileLines) == 0 {
		return 0, 0, 0, false
	}

	oldTrimmed := trimmedLines(oldLines)
	fileTrimmed := trimmedLines(fileLines)

	// A file shorter than the old text has no full window; its single partial window is still the
	// closest thing there is, so it is scored over the lines it does have.
	lastStart := len(fileTrimmed) - len(oldTrimmed)
	if lastStart < 0 {
		lastStart = 0
	}

	bestStart, bestScore := 0, 0
	for s := 0; s <= lastStart; s++ {
		matched := 0
		for i := 0; i < len(oldTrimmed) && s+i < len(fileTrimmed); i++ {
			if oldTrimmed[i] != "" && oldTrimmed[i] == fileTrimmed[s+i] {
				matched++
			}
		}
		if matched > bestScore {
			bestStart, bestScore = s, matched
		}
	}
	if bestScore == 0 {
		return 0, 0, 0, false
	}

	end = bestStart + len(oldTrimmed)
	if end > len(fileTrimmed) {
		end = len(fileTrimmed)
	}
	return bestStart + 1, end, bestScore, true
}

// trimmedLines answers each line with its leading and trailing whitespace removed.
func trimmedLines(lines []string) []string {
	trimmed := make([]string, len(lines))
	for i, line := range lines {
		trimmed[i] = strings.TrimSpace(line)
	}
	return trimmed
}

// numberedExcerpt renders the file's lines start..end (1-based, inclusive) as numbered rows, right
// aligned on the widest number shown and indented so the rows read as a quotation rather than as
// more of the tool's own sentence. At most maxNearMatchExcerptLines rows are rendered; a range cut
// short ends with nearMatchCutMarker.
func numberedExcerpt(fileLines []string, start, end int) string {
	if end > len(fileLines) {
		end = len(fileLines)
	}
	last := end
	if limit := start + maxNearMatchExcerptLines - 1; last > limit {
		last = limit
	}
	width := len(strconv.Itoa(last))

	rows := make([]string, 0, maxNearMatchExcerptLines+1)
	for n := start; n <= last; n++ {
		rows = append(rows, fmt.Sprintf("  %*d | %s", width, n, escapeRowBreaks(fileLines[n-1])))
	}
	if last < end {
		rows = append(rows, nearMatchCutMarker)
	}
	return strings.Join(rows, "\n")
}
