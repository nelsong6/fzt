package model

import (
	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/scorer"
)

// StyledRune is a single character with its ANSI-derived style.
type StyledRune struct {
	Char  rune
	Style tcell.Style
}

// Item represents a single input line parsed into fields.
type Item struct {
	Fields        []string      // clean field values (ANSI stripped) for matching and width calculation
	DisplayFields []string      // original field values with ANSI codes preserved
	StyledFields  [][]StyledRune // parsed ANSI: each field as styled runes (nil if --ansi not used)
	Depth         int           // hierarchy depth (0 = top level), only used with --tiered
	Score         scorer.TieredScore // computed fuzzy match score
	MatchIndices  [][]int       // per-field match indices for highlighting
	Original      string        // the original unsplit line
	Children      []int         // indices of direct children in the flat items slice
	ParentIdx     int           // index of parent in the flat items slice (-1 for root)
	HasChildren   bool          // true if this item has children
	Path          string        // breadcrumb path (e.g. "git › gitprune") for nested items
	URL           string        // optional link URL (for web showcase leaf selection)
}
