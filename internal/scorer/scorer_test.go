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
	if r.Indices[0] != 0 || r.Indices[1] != 1 || r.Indices[2] != 2 {
		t.Errorf("expected indices [0,1,2], got %v", r.Indices)
	}
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
	r := FuzzyMatch("Remove local branches", "lo")
	if r == nil {
		t.Fatal("expected match")
	}
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

func TestScoreItemSingleTerm(t *testing.T) {
	fields := []string{"remove-old", "Remove local branches"}
	ts, indices := ScoreItem(fields, "rem", nil, nil)
	if ts.Name == 0 {
		t.Error("expected non-zero name score")
	}
	if indices == nil {
		t.Error("expected non-nil indices")
	}
}

func TestScoreItemSearchCols(t *testing.T) {
	fields := []string{"gitprune", "Remove local branches"}

	// searchCols=[1] → only field 0. "Remove" won't match "gitprune".
	ts1, _ := ScoreItem(fields, "Remove", []int{1}, nil)
	if ts1.Name != 0 && ts1.Desc != 0 {
		t.Errorf("expected no match on col 1 for 'Remove', got %+v", ts1)
	}

	// searchCols=[2] → only field 1. "Remove" matches description.
	ts2, indices2 := ScoreItem(fields, "Remove", []int{2}, nil)
	if ts2.Desc == 0 {
		t.Fatal("expected desc match on col 2")
	}
	if indices2[1] == nil {
		t.Error("expected indices on field 1")
	}
	if indices2[0] != nil {
		t.Error("field 0 should not have indices (not searched)")
	}
}

func TestScoreItemMultiTerm(t *testing.T) {
	fields := []string{"gitprune", "Remove local branches"}

	// "git rem" → "git" matches name, "rem" matches description
	ts, indices := ScoreItem(fields, "git rem", nil, nil)
	if ts.Name == 0 {
		t.Fatal("expected name score for 'git'")
	}
	if ts.Desc == 0 {
		t.Fatal("expected desc score for 'rem'")
	}
	if indices == nil {
		t.Fatal("expected non-nil indices")
	}
	if indices[0] == nil {
		t.Error("expected indices on field 0 for 'git'")
	}
	if indices[1] == nil {
		t.Error("expected indices on field 1 for 'rem'")
	}
}

func TestScoreItemMultiTermNoMatch(t *testing.T) {
	fields := []string{"gitprune", "Remove local branches"}

	_, indices := ScoreItem(fields, "git xyz", nil, nil)
	if indices != nil {
		t.Error("expected nil indices when a term doesn't match")
	}
}

func TestScoreItemAncestorMatch(t *testing.T) {
	fields := []string{"prune-merged", "Remove merged branches"}
	ancestors := []string{"git"}

	// "git prune" → "git" matches ancestor, "prune" matches name
	ts, indices := ScoreItem(fields, "git prune", nil, ancestors)
	if ts.Name == 0 {
		t.Fatal("expected name score for 'prune'")
	}
	if ts.Ancestor == 0 {
		t.Fatal("expected ancestor score for 'git'")
	}
	if indices == nil {
		t.Fatal("expected non-nil indices")
	}
	if indices[0] == nil {
		t.Error("expected indices on field 0 for 'prune'")
	}
}

func TestScoreItemNameBeatsDescription(t *testing.T) {
	// Item A: "rem" matches the name
	nameFields := []string{"rem-tool", "does other things"}
	tsName, _ := ScoreItem(nameFields, "rem", nil, nil)

	// Item B: "rem" only matches the description
	descFields := []string{"xyz", "Remove local branches and remote refs"}
	tsDesc, _ := ScoreItem(descFields, "rem", nil, nil)

	// Name match must rank above description match
	if !tsDesc.Less(tsName) {
		t.Errorf("name match %+v should outrank description match %+v", tsName, tsDesc)
	}
}

func TestScoreItemDescriptionBeatsAncestor(t *testing.T) {
	// Item with "git" matching description
	descFields := []string{"something", "git integration helper"}
	tsDesc, _ := ScoreItem(descFields, "git", nil, nil)

	// Item with "git" matching only ancestor
	ancestorFields := []string{"no-match-here"}
	ancestors := []string{"git"}
	tsAnc, _ := ScoreItem(ancestorFields, "git", nil, ancestors)

	if !tsAnc.Less(tsDesc) {
		t.Errorf("description match %+v should outrank ancestor match %+v", tsDesc, tsAnc)
	}
}

func TestTieredScoreLess(t *testing.T) {
	// Name always wins regardless of desc/ancestor
	a := TieredScore{Name: 1, Desc: 0, Ancestor: 0}
	b := TieredScore{Name: 0, Desc: 999, Ancestor: 999}
	if !b.Less(a) {
		t.Error("any name score should beat any desc+ancestor score")
	}

	// Equal name → desc decides
	c := TieredScore{Name: 5, Desc: 1, Ancestor: 0}
	d := TieredScore{Name: 5, Desc: 0, Ancestor: 999}
	if !d.Less(c) {
		t.Error("with equal name, desc should decide")
	}

	// Equal name+desc → ancestor decides
	e := TieredScore{Name: 5, Desc: 3, Ancestor: 1}
	f := TieredScore{Name: 5, Desc: 3, Ancestor: 0}
	if !f.Less(e) {
		t.Error("with equal name+desc, ancestor should decide")
	}
}
