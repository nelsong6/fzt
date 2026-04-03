package scorer

import (
	"strings"
	"unicode"
)

const wordBoundaries = " /-_>"

// TieredScore holds match quality broken into strict tiers.
// Comparison is lexicographic: Name is consulted first, then Desc, then Ancestor.
// A higher Name score always beats any Desc/Ancestor combination.
type TieredScore struct {
	Name     int // total raw score from name (field 0) matches
	Desc     int // total raw score from description (field 1+) matches
	Ancestor int // total raw score from ancestor name matches
}

// Total flattens a TieredScore into a single int for ranking.
// Only called after tier comparison decides the ordering.
func (s TieredScore) Total() int {
	return s.Name + s.Desc + s.Ancestor
}

// Less returns true if a ranks below b. Lexicographic: name first, then desc, then ancestor.
func (a TieredScore) Less(b TieredScore) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Desc != b.Desc {
		return a.Desc < b.Desc
	}
	return a.Ancestor < b.Ancestor
}

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

// ScoreItem scores an item across multiple fields, returning a TieredScore.
// searchCols specifies which field indices to search (1-based). If empty, all fields are searched.
// ancestorNames provides parent/grandparent folder names as the lowest match tier.
// The query is split on whitespace; every term must match for the item to be included.
// Returns a zero TieredScore and nil indices if any term fails to match.
func ScoreItem(fields []string, query string, searchCols []int, ancestorNames []string) (TieredScore, [][]int) {
	if query == "" {
		return TieredScore{}, make([][]int, len(fields))
	}

	terms := strings.Fields(query)
	if len(terms) == 0 {
		return TieredScore{}, make([][]int, len(fields))
	}

	indices := make([][]int, len(fields))
	var ts TieredScore

	for _, term := range terms {
		type match struct {
			score   int
			field   int
			indices []int
		}
		var bestName, bestDesc, bestAncestor match

		for i, field := range fields {
			result := FuzzyMatch(field, term)
			if result == nil {
				continue
			}
			if i == 0 && shouldSearch(i, searchCols) {
				if result.Score > bestName.score {
					bestName = match{result.Score, i, result.Indices}
				}
			} else if i > 0 {
				// Descriptions are always searchable at the desc tier
				if result.Score > bestDesc.score {
					bestDesc = match{result.Score, i, result.Indices}
				}
			}
		}

		for _, name := range ancestorNames {
			result := FuzzyMatch(name, term)
			if result != nil && result.Score > bestAncestor.score {
				bestAncestor = match{result.Score, -1, nil}
			}
		}

		// Pick highest tier that matched
		if bestName.score > 0 {
			ts.Name += bestName.score
			indices[bestName.field] = mergeIndices(indices[bestName.field], bestName.indices)
		} else if bestDesc.score > 0 {
			ts.Desc += bestDesc.score
			indices[bestDesc.field] = mergeIndices(indices[bestDesc.field], bestDesc.indices)
		} else if bestAncestor.score > 0 {
			ts.Ancestor += bestAncestor.score
		} else {
			return TieredScore{}, nil // term unmatched → item rejected
		}
	}

	return ts, indices
}

func mergeIndices(a, b []int) []int {
	if a == nil {
		return b
	}
	return append(a, b...)
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
