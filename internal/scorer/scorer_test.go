package scorer

import (
	"testing"
)

func TestFuzzyMatchBasic(t *testing.T) {
	r := FuzzyMatch("GitHub", "git")
	if r == nil {
		t.Fatal("expected match")
	}
	if len(r.Indices) != 3 {
		t.Errorf("expected 3 indices, got %d", len(r.Indices))
	}
	// Verify indices point to correct positions
	if r.Indices[0] != 0 || r.Indices[1] != 1 || r.Indices[2] != 2 {
		t.Errorf("expected indices [0,1,2], got %v", r.Indices)
	}
	// Score should be positive
	if r.Score <= 0 {
		t.Errorf("expected positive score, got %d", r.Score)
	}
}

func TestFuzzyMatchNoMatch(t *testing.T) {
	r := FuzzyMatch("hello", "xyz")
	if r != nil {
		t.Error("expected nil for no match")
	}
}

func TestFuzzyMatchEmptyQuery(t *testing.T) {
	r := FuzzyMatch("anything", "")
	if r == nil {
		t.Fatal("expected non-nil for empty query")
	}
	if r.Score != 0 {
		t.Errorf("expected score 0, got %d", r.Score)
	}
}

func TestFuzzyMatchWordBoundary(t *testing.T) {
	// "Remove local branches" matching "lo"
	// l(7): 1 (no boundary), o(8): 2(consec) = 3
	// Actually l at "local" position 7, preceded by space → boundary
	r := FuzzyMatch("Remove local branches", "lo")
	if r == nil {
		t.Fatal("expected match")
	}
	// l(7): 1 + 3(after space) = 4, o(8): 2(consec) = 6
	if r.Score != 6 {
		t.Errorf("expected score 6, got %d", r.Score)
	}
}

func TestFuzzyMatchCaseInsensitive(t *testing.T) {
	r := FuzzyMatch("GitHub", "GITHUB")
	if r == nil {
		t.Fatal("expected case-insensitive match")
	}
	if len(r.Indices) != 6 {
		t.Errorf("expected 6 indices, got %d", len(r.Indices))
	}
}

func TestScoreItemMultiField(t *testing.T) {
	// Use a query that matches both fields
	fields := []string{"remove-old", "Remove local branches"}
	score, indices := ScoreItem(fields, "rem", nil, false, 0, 5)
	if score == 0 {
		t.Error("expected non-zero score")
	}
	if indices == nil {
		t.Error("expected non-nil indices")
	}
	// Both fields match "rem" → multi-field bonus (+1)
	single := FuzzyMatch("Remove local branches", "rem")
	if score != single.Score+1 {
		t.Errorf("expected multi-field bonus: got %d, single was %d", score, single.Score)
	}
}

func TestScoreItemTiered(t *testing.T) {
	fields := []string{"nested-item"}
	scoreD0, _ := ScoreItem(fields, "nest", nil, true, 0, 5)
	scoreD2, _ := ScoreItem(fields, "nest", nil, true, 2, 5)
	if scoreD0-scoreD2 != 10 {
		t.Errorf("expected depth 2 to be penalized by 10, got diff %d", scoreD0-scoreD2)
	}
}

func TestScoreItemSearchCols(t *testing.T) {
	fields := []string{"gitprune", "Remove local branches"}

	// searchCols=[1] means 1-based col 1 = field index 0 ("gitprune")
	// "Remove" won't match "gitprune", so no match expected
	score1, _ := ScoreItem(fields, "Remove", []int{1}, false, 0, 5)
	if score1 != 0 {
		t.Errorf("expected no match on col 1 for 'Remove', got score %d", score1)
	}

	// searchCols=[2] means 1-based col 2 = field index 1 ("Remove local branches")
	score2, indices2 := ScoreItem(fields, "Remove", []int{2}, false, 0, 5)
	if score2 == 0 {
		t.Fatal("expected match on col 2")
	}
	if indices2[1] == nil {
		t.Error("expected indices on field 1")
	}
	if indices2[0] != nil {
		t.Error("field 0 should not have indices (not searched)")
	}
}
