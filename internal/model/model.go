package model

import "github.com/gdamore/tcell/v2"

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
	Score         int           // computed fuzzy match score
	MatchIndices  [][]int       // per-field match indices for highlighting
	Original      string        // the original unsplit line
}
