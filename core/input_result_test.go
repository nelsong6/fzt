package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func testTreeState(t *testing.T) (*State, []int, Config) {
	t.Helper()
	items, err := LoadYAMLFromString(`
- name: Example Links
  children:
    - name: Wikipedia
      url: https://en.wikipedia.org
    - name: Hacker News
      url: https://news.ycombinator.com
`)
	if err != nil {
		t.Fatalf("LoadYAMLFromString: %v", err)
	}
	cfg := Config{Tiered: true}
	s, searchCols := NewState(items, cfg)
	ctx := s.TopCtx()
	ctx.TreeExpanded = make(map[int]bool)
	ctx.QueryExpanded = make(map[int]bool)
	ctx.TreeCursor = -1
	return s, searchCols, cfg
}

func TestHandleUnifiedKeyResultReportsShiftEnterTopMatchItem(t *testing.T) {
	s, searchCols, cfg := testTreeState(t)

	for _, ch := range "hacker" {
		result := HandleUnifiedKeyResult(s, tcell.KeyRune, ch, false, cfg, searchCols)
		if result.Action != "" {
			t.Fatalf("typing emitted action %q", result.Action)
		}
	}

	result := HandleUnifiedKeyResult(s, tcell.KeyEnter, 0, true, cfg, searchCols)
	if result.Action != "select:Hacker News" {
		t.Fatalf("action = %q, want select:Hacker News", result.Action)
	}
	if !result.HasItem {
		t.Fatal("expected selected item")
	}
	if result.Item.Action == nil || result.Item.Action.Target != "https://news.ycombinator.com" {
		t.Fatalf("selected target = %#v, want Hacker News URL", result.Item.Action)
	}
}

func TestClickUnifiedRowResultReportsClickedItem(t *testing.T) {
	s, searchCols, cfg := testTreeState(t)
	_ = searchCols
	ctx := s.TopCtx()
	ctx.TreeExpanded[0] = true

	result := ClickUnifiedRowResult(s, 4, cfg, 24)
	if result.Action != "select:Wikipedia" {
		t.Fatalf("action = %q, want select:Wikipedia", result.Action)
	}
	if !result.HasItem {
		t.Fatal("expected selected item")
	}
	if result.Item.Action == nil || result.Item.Action.Target != "https://en.wikipedia.org" {
		t.Fatalf("selected target = %#v, want Wikipedia URL", result.Item.Action)
	}
}
