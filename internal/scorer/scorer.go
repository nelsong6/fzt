package scorer

import (
	"strings"
	"unicode"
)

const wordBoundaries = " /-_>"

// Result holds the outcome of a single fuzzy match.
type Result struct {
	Score   int
	Indices []int
}

// FuzzyMatch scores how well query matches text using a left-to-right scan.
// Returns nil if the query cannot be fully matched.
//
// Scoring:
//   - +1 per matched character
//   - +2 instead if the match is consecutive (immediately follows previous match)
//   - +3 bonus if the match is at position 0 or immediately after a word boundary
func FuzzyMatch(text, query string) *Result {
	if query == "" {
		return &Result{Score: 0, Indices: nil}
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)
	textRunes := []rune(lowerText)
	queryRunes := []rune(lowerQuery)
	origRunes := []rune(text)

	qi := 0
	score := 0
	lastMatch := -1
	var indices []int

	for i := 0; i < len(textRunes) && qi < len(queryRunes); i++ {
		if textRunes[i] == queryRunes[qi] {
			indices = append(indices, i)
			if lastMatch == i-1 {
				score += 2 // consecutive
			} else {
				score += 1
			}
			if i == 0 || isWordBoundary(origRunes[i-1]) {
				score += 3 // word boundary bonus
			}
			lastMatch = i
			qi++
		}
	}

	if qi < len(queryRunes) {
		return nil // incomplete match
	}

	return &Result{Score: score, Indices: indices}
}

func isWordBoundary(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	return strings.ContainsRune(wordBoundaries, r)
}

// ScoreItem scores an item across multiple fields, returning the best combined score.
// searchCols specifies which field indices to search (1-based). If empty, all fields are searched.
// In tiered mode, applies: finalScore = rawScore - (depth * depthPenalty).
func ScoreItem(fields []string, query string, searchCols []int, tiered bool, depth int, depthPenalty int) (int, [][]int) {
	if query == "" {
		indices := make([][]int, len(fields))
		return 0, indices
	}

	indices := make([][]int, len(fields))
	bestScore := 0
	matchCount := 0

	for i, field := range fields {
		if !shouldSearch(i, searchCols) {
			continue
		}
		result := FuzzyMatch(field, query)
		if result != nil {
			indices[i] = result.Indices
			matchCount++
			if result.Score > bestScore {
				bestScore = result.Score
			}
		}
	}

	if matchCount == 0 {
		return 0, nil // no match in any field
	}

	// Multi-field bonus: +1 when more than one field matches
	if matchCount > 1 {
		bestScore++
	}

	if tiered {
		bestScore -= depth * depthPenalty
	}

	return bestScore, indices
}

// shouldSearch returns true if field index i (0-based) is in the search set.
// searchCols uses 1-based indexing to match fzf's --nth convention.
func shouldSearch(i int, searchCols []int) bool {
	if len(searchCols) == 0 {
		return true
	}
	for _, col := range searchCols {
		if col-1 == i {
			return true
		}
	}
	return false
}
