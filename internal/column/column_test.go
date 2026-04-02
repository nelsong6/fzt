package column

import (
	"testing"
)

func TestParseLinesPlain(t *testing.T) {
	lines := []string{"gitprune\tRemove branches", "gitsub\tSubmodule clone"}
	items := ParseLines(lines, "\t", false, false)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Fields[0] != "gitprune" {
		t.Errorf("expected 'gitprune', got '%s'", items[0].Fields[0])
	}
	if items[0].Depth != 0 {
		t.Errorf("expected depth 0, got %d", items[0].Depth)
	}
}

func TestParseLinesTiered(t *testing.T) {
	lines := []string{"0\tGitHub\thttps://github.com", "2\tfzh\thttps://github.com/fzh"}
	items := ParseLines(lines, "\t", true, false)
	if items[0].Depth != 0 {
		t.Errorf("expected depth 0, got %d", items[0].Depth)
	}
	if items[1].Depth != 2 {
		t.Errorf("expected depth 2, got %d", items[1].Depth)
	}
	// Depth column should be stripped from fields
	if items[0].Fields[0] != "GitHub" {
		t.Errorf("expected 'GitHub', got '%s'", items[0].Fields[0])
	}
	if len(items[1].Fields) != 2 {
		t.Errorf("expected 2 fields after stripping depth, got %d", len(items[1].Fields))
	}
}

func TestComputeWidths(t *testing.T) {
	lines := []string{"git\tShort", "workspace\tLonger description"}
	items := ParseLines(lines, "\t", false, false)
	widths := ComputeWidths(items)
	if widths[0] != 9 { // "workspace" = 9
		t.Errorf("expected width 9, got %d", widths[0])
	}
	if widths[1] != 18 { // "Longer description" = 18
		t.Errorf("expected width 18, got %d", widths[1])
	}
}

func TestFormatRow(t *testing.T) {
	widths := []int{9, 20}
	row := FormatRow([]string{"git", "Short"}, widths, 2)
	// "git" + 6 spaces padding + 2 gap + "Short"
	expected := "git        Short"
	if row != expected {
		t.Errorf("expected %q, got %q", expected, row)
	}
}
