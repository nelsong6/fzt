package core

import (
	"github.com/gdamore/tcell/v2"
)

// StyledRune is a single character with its ANSI-derived style.
type StyledRune struct {
	Char  rune
	Style tcell.Style
}

// ItemAction describes what happens when an item is selected.
// Type determines how target is interpreted by the shell wrapper.
type ItemAction struct {
	Type   string `json:"type" yaml:"type"`     // "url", "function", "command"
	Target string `json:"target" yaml:"target"` // URL, function name, or command action ID
}

// Item represents a single input line parsed into fields.
type Item struct {
	Fields        []string       // ANSI-stripped field values for matching. Index 0 is always the name.
	DisplayFields []string       // field values with ANSI codes preserved (for rendering)
	StyledFields  [][]StyledRune // parsed ANSI per field (nil if --ansi not used)
	Depth         int            // tree depth (0 = top level). Used for tiered scoring depth penalty.
	Score         TieredScore    // match score populated by FilterItems, consumed by sort
	MatchIndices  [][]int        // per-field character positions that matched — used for highlight rendering
	Original      string         // original unsplit input line (set by ParseLines; empty for YAML/provider)
	Children      []int          // indices of direct children in the flat AllItems slice
	ParentIdx     int            // index of parent in flat AllItems slice (-1 for root). Drives ancestor matching in ScoreItem.
	HasChildren   bool           // true for folders. Distinct from len(Children)>0: lazy-loaded folders start with HasChildren=true but empty Children.
	Path          string         // breadcrumb path from YAML loading (e.g. "git › gitprune"). Set by flattenYAML.
	Hidden        bool           // excluded from tree view unless in scope chain. Used for the `:` command palette folder.
	Action           *ItemAction // what happens on selection. nil = informational or folder.
	DisplayCondition string      // env tag required to show this item (e.g. "terminal"). Empty = always show.
	IsProperty    bool           // true for temporary property items created by inspect mode. Not serialized.
	PropertyOf    int            // index of the item this property belongs to (-1 = not a property).
	PropertyKey   string         // which property this item represents: "name", "description", "url", "action".
}
