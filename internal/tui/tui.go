package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzh/internal/column"
	"github.com/nelsong6/fzh/internal/model"
	"github.com/nelsong6/fzh/internal/scorer"
)

// Config holds all TUI options derived from CLI flags.
type Config struct {
	Layout       string // "reverse" or "default"
	Border       bool
	HeaderLines  int
	Nth          []int // 1-based field indices for search scope
	AcceptNth    []int // 1-based field indices for output
	Prompt       string
	Delimiter    string
	Tiered       bool
	DepthPenalty int
	SearchCols   []int // 1-based, overrides Nth for scoring
	Height       int   // percentage of terminal height (0 = full)
	ShowScores   bool  // annotate filter output with scores
	ANSI         bool  // preserve ANSI colors from input
}

type state struct {
	query     []rune
	cursor    int // cursor position within query
	index     int // selected item index in filtered list
	offset    int // scroll offset
	items     []model.Item
	filtered  []model.Item
	headers   []model.Item
	widths    []int
	colGap    int
	cancelled bool
}

func initState(items []model.Item, cfg Config) (*state, []int) {
	var headers []model.Item
	data := items
	if cfg.HeaderLines > 0 && cfg.HeaderLines <= len(items) {
		headers = items[:cfg.HeaderLines]
		data = items[cfg.HeaderLines:]
	}

	allWidths := column.ComputeWidths(items)

	s := &state{
		query:   nil,
		cursor:  0,
		index:   0,
		offset:  0,
		items:   data,
		headers: headers,
		widths:  allWidths,
		colGap:  2,
	}

	searchCols := cfg.SearchCols
	if len(searchCols) == 0 {
		searchCols = cfg.Nth
	}
	filterItems(s, cfg, searchCols)

	return s, searchCols
}

func renderFrame(c Canvas, s *state, cfg Config) {
	w, h := c.Size()

	usableH := h
	if cfg.Height > 0 && cfg.Height < 100 {
		usableH = h * cfg.Height / 100
		if usableH < 3 {
			usableH = 3
		}
	}

	startY := 0
	if cfg.Height > 0 && cfg.Height < 100 {
		startY = h - usableH
	}

	if cfg.Layout == "reverse" {
		drawReverse(c, s, cfg, w, startY, usableH)
	} else {
		drawDefault(c, s, cfg, w, startY, usableH)
	}
}

// Run launches the interactive TUI. Returns the selected item's output string, or "" if cancelled.
func Run(items []model.Item, cfg Config) (string, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return "", fmt.Errorf("creating screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return "", fmt.Errorf("initializing screen: %w", err)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.Background(tcell.ColorDefault).Foreground(tcell.ColorDefault))
	screen.EnablePaste()

	s, searchCols := initState(items, cfg)
	canvas := &tcellCanvas{screen: screen}

	for {
		screen.Clear()
		renderFrame(canvas, s, cfg)
		screen.Show()

		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyEscape, tcell.KeyCtrlC:
				s.cancelled = true
				return "", nil
			case tcell.KeyEnter:
				if len(s.filtered) > 0 {
					selected := s.filtered[s.index]
					return formatOutput(selected, cfg), nil
				}
				return "", nil
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if s.cursor > 0 {
					s.query = append(s.query[:s.cursor-1], s.query[s.cursor:]...)
					s.cursor--
					s.index = 0
					s.offset = 0
					filterItems(s, cfg, searchCols)
				}
			case tcell.KeyDelete:
				if s.cursor < len(s.query) {
					s.query = append(s.query[:s.cursor], s.query[s.cursor+1:]...)
					filterItems(s, cfg, searchCols)
				}
			case tcell.KeyLeft:
				if s.cursor > 0 {
					s.cursor--
				}
			case tcell.KeyRight:
				if s.cursor < len(s.query) {
					s.cursor++
				}
			case tcell.KeyUp, tcell.KeyCtrlP:
				if cfg.Layout == "reverse" {
					if s.index < len(s.filtered)-1 {
						s.index++
					}
				} else {
					if s.index > 0 {
						s.index--
					}
				}
			case tcell.KeyDown, tcell.KeyCtrlN:
				if cfg.Layout == "reverse" {
					if s.index > 0 {
						s.index--
					}
				} else {
					if s.index < len(s.filtered)-1 {
						s.index++
					}
				}
			case tcell.KeyCtrlA:
				s.cursor = 0
			case tcell.KeyCtrlE:
				s.cursor = len(s.query)
			case tcell.KeyCtrlU:
				s.query = s.query[s.cursor:]
				s.cursor = 0
				s.index = 0
				s.offset = 0
				filterItems(s, cfg, searchCols)
			case tcell.KeyCtrlW:
				if s.cursor > 0 {
					end := s.cursor
					for s.cursor > 0 && s.query[s.cursor-1] == ' ' {
						s.cursor--
					}
					for s.cursor > 0 && s.query[s.cursor-1] != ' ' {
						s.cursor--
					}
					s.query = append(s.query[:s.cursor], s.query[end:]...)
					s.index = 0
					s.offset = 0
					filterItems(s, cfg, searchCols)
				}
			case tcell.KeyRune:
				ch := ev.Rune()
				s.query = append(s.query[:s.cursor], append([]rune{ch}, s.query[s.cursor:]...)...)
				s.cursor++
				s.index = 0
				s.offset = 0
				filterItems(s, cfg, searchCols)
			}
		case *tcell.EventResize:
			screen.Sync()
		}
	}
}

// Simulate runs a headless simulation: renders the initial frame, then one frame
// per character of the query. Returns all frames as text snapshots.
func Simulate(items []model.Item, cfg Config, query string, w, h int, styled bool) []Frame {
	s, searchCols := initState(items, cfg)

	var frames []Frame

	// Frame 0: initial state (empty query)
	mem := NewMemScreen(w, h)
	renderFrame(mem, s, cfg)
	label := "(initial)"
	if styled {
		frames = append(frames, Frame{Label: label, Content: mem.StyledSnapshot()})
	} else {
		frames = append(frames, Frame{Label: label, Content: mem.Snapshot()})
	}

	// One frame per keystroke
	for _, ch := range query {
		s.query = append(s.query, ch)
		s.cursor++
		s.index = 0
		s.offset = 0
		filterItems(s, cfg, searchCols)

		mem.Clear()
		renderFrame(mem, s, cfg)
		label := fmt.Sprintf("key: '%c'  query: \"%s\"", ch, string(s.query))
		if styled {
			frames = append(frames, Frame{Label: label, Content: mem.StyledSnapshot()})
		} else {
			frames = append(frames, Frame{Label: label, Content: mem.Snapshot()})
		}
	}

	return frames
}

// Frame represents one rendered screen state.
type Frame struct {
	Label   string // description of what triggered this frame
	Content string // text grid snapshot
}

// FormatFrames renders all frames as a single string for file output.
func FormatFrames(frames []Frame) string {
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "=== Frame %d [%s] ===\n", i, f.Label)
		b.WriteString(f.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

func filterItems(s *state, cfg Config, searchCols []int) {
	query := string(s.query)
	if query == "" {
		s.filtered = make([]model.Item, len(s.items))
		copy(s.filtered, s.items)
		return
	}

	var matched []model.Item
	for _, item := range s.items {
		score, indices := scorer.ScoreItem(item.Fields, query, searchCols, cfg.Tiered, item.Depth, cfg.DepthPenalty)
		if indices != nil {
			m := item
			m.Score = score
			m.MatchIndices = indices
			matched = append(matched, m)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Score > matched[j].Score
	})
	s.filtered = matched
}

func drawReverse(c Canvas, s *state, cfg Config, w, startY, h int) {
	y := startY

	borderOffset := 0
	if cfg.Border {
		drawBorderTop(c, w, y)
		y++
		borderOffset = 1
	}

	promptStr := cfg.Prompt
	if promptStr == "" {
		promptStr = "> "
	}
	drawText(c, 0+borderOffset, y, promptStr, tcell.StyleDefault.Bold(true), w-borderOffset*2)
	promptLen := len([]rune(promptStr))
	drawText(c, promptLen+borderOffset, y, string(s.query), tcell.StyleDefault, w-promptLen-borderOffset*2)
	c.ShowCursor(promptLen+s.cursor+borderOffset, y)
	y++

	counterStr := fmt.Sprintf("  %d/%d", len(s.filtered), len(s.items))
	drawText(c, borderOffset, y, counterStr, tcell.StyleDefault.Foreground(tcell.ColorDarkGray), w-borderOffset*2)
	y++

	for _, hdr := range s.headers {
		row := column.FormatRow(hdr.Fields, s.widths, s.colGap)
		drawText(c, borderOffset+2, y, row, tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true), w-borderOffset*2-2)
		y++
	}

	itemLines := startY + h - y
	if cfg.Border {
		itemLines--
	}
	if itemLines < 0 {
		itemLines = 0
	}

	if s.index < s.offset {
		s.offset = s.index
	}
	if s.index >= s.offset+itemLines {
		s.offset = s.index - itemLines + 1
	}

	for i := 0; i < itemLines && i+s.offset < len(s.filtered); i++ {
		idx := i + s.offset
		item := s.filtered[idx]
		isSelected := idx == s.index

		style := tcell.StyleDefault
		if isSelected {
			style = style.Background(tcell.ColorDarkBlue).Foreground(tcell.ColorWhite)
		}

		indicator := "  "
		if isSelected {
			indicator = "> "
		}
		drawText(c, borderOffset, y+i, indicator, style.Bold(true), 2)

		x := borderOffset + 2
		for fi, field := range item.Fields {
			var indices []int
			if item.MatchIndices != nil && fi < len(item.MatchIndices) {
				indices = item.MatchIndices[fi]
			}
			var sr []model.StyledRune
			if item.StyledFields != nil && fi < len(item.StyledFields) {
				sr = item.StyledFields[fi]
			}
			x = drawHighlightedField(c, x, y+i, field, sr, indices, style, isSelected, s.widths, fi, s.colGap, w-borderOffset*2)
		}

		if isSelected {
			for fx := x; fx < w-borderOffset; fx++ {
				c.SetContent(fx, y+i, ' ', nil, style)
			}
		}
	}

	if cfg.Border {
		drawBorderBottom(c, w, startY+h-1)
	}
}

func drawDefault(c Canvas, s *state, cfg Config, w, startY, h int) {
	y := startY

	borderOffset := 0
	if cfg.Border {
		drawBorderTop(c, w, y)
		y++
		borderOffset = 1
	}

	for _, hdr := range s.headers {
		row := column.FormatRow(hdr.Fields, s.widths, s.colGap)
		drawText(c, borderOffset+2, y, row, tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true), w-borderOffset*2-2)
		y++
	}

	promptLines := 2
	itemLines := startY + h - y - promptLines
	if cfg.Border {
		itemLines--
	}
	if itemLines < 0 {
		itemLines = 0
	}

	if s.index < s.offset {
		s.offset = s.index
	}
	if s.index >= s.offset+itemLines {
		s.offset = s.index - itemLines + 1
	}

	for i := 0; i < itemLines && i+s.offset < len(s.filtered); i++ {
		idx := i + s.offset
		item := s.filtered[idx]
		isSelected := idx == s.index

		style := tcell.StyleDefault
		if isSelected {
			style = style.Background(tcell.ColorDarkBlue).Foreground(tcell.ColorWhite)
		}

		indicator := "  "
		if isSelected {
			indicator = "> "
		}
		drawText(c, borderOffset, y+i, indicator, style.Bold(true), 2)

		x := borderOffset + 2
		for fi, field := range item.Fields {
			var indices []int
			if item.MatchIndices != nil && fi < len(item.MatchIndices) {
				indices = item.MatchIndices[fi]
			}
			var sr []model.StyledRune
			if item.StyledFields != nil && fi < len(item.StyledFields) {
				sr = item.StyledFields[fi]
			}
			x = drawHighlightedField(c, x, y+i, field, sr, indices, style, isSelected, s.widths, fi, s.colGap, w-borderOffset*2)
		}

		if isSelected {
			for fx := x; fx < w-borderOffset; fx++ {
				c.SetContent(fx, y+i, ' ', nil, style)
			}
		}
	}

	bottomY := startY + h - promptLines
	if cfg.Border {
		bottomY--
	}

	counterStr := fmt.Sprintf("  %d/%d", len(s.filtered), len(s.items))
	drawText(c, borderOffset, bottomY, counterStr, tcell.StyleDefault.Foreground(tcell.ColorDarkGray), w-borderOffset*2)

	promptStr := cfg.Prompt
	if promptStr == "" {
		promptStr = "> "
	}
	drawText(c, borderOffset, bottomY+1, promptStr, tcell.StyleDefault.Bold(true), w-borderOffset*2)
	promptLen := len([]rune(promptStr))
	drawText(c, promptLen+borderOffset, bottomY+1, string(s.query), tcell.StyleDefault, w-promptLen-borderOffset*2)
	c.ShowCursor(promptLen+s.cursor+borderOffset, bottomY+1)

	if cfg.Border {
		drawBorderBottom(c, w, startY+h-1)
	}
}

func drawHighlightedField(c Canvas, x, y int, field string, styledRunes []model.StyledRune, indices []int, baseStyle tcell.Style, isSelected bool, widths []int, fieldIdx, gap, maxW int) int {
	runes := []rune(field)
	indexSet := make(map[int]bool)
	for _, idx := range indices {
		indexSet[idx] = true
	}

	for i, r := range runes {
		if x >= maxW {
			break
		}

		style := baseStyle

		// Layer 1: Apply ANSI color if available
		if styledRunes != nil && i < len(styledRunes) {
			style = styledRunes[i].Style
			// If this row is selected, override the background but keep the foreground color
			if isSelected {
				fg, _, attrs := style.Decompose()
				style = tcell.StyleDefault.Background(tcell.ColorDarkBlue).Foreground(fg).Attributes(attrs)
			}
		}

		// Layer 2: Override with match highlight
		if indexSet[i] {
			if isSelected {
				style = style.Foreground(tcell.ColorGreen).Bold(true).Background(tcell.ColorDarkBlue)
			} else {
				style = style.Foreground(tcell.ColorGreen).Bold(true)
			}
		}

		c.SetContent(x, y, r, nil, style)
		x++
	}

	if fieldIdx < len(widths)-1 {
		padTo := widths[fieldIdx]
		for len(runes) < padTo {
			if x >= maxW {
				break
			}
			c.SetContent(x, y, ' ', nil, baseStyle)
			x++
			runes = append(runes, ' ')
		}
		for g := 0; g < gap; g++ {
			if x >= maxW {
				break
			}
			c.SetContent(x, y, ' ', nil, baseStyle)
			x++
		}
	}

	return x
}

func drawText(c Canvas, x, y int, text string, style tcell.Style, maxW int) {
	for _, r := range text {
		if x >= maxW {
			break
		}
		c.SetContent(x, y, r, nil, style)
		x++
	}
}

func drawBorderTop(c Canvas, w, y int) {
	style := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	c.SetContent(0, y, '┌', nil, style)
	for x := 1; x < w-1; x++ {
		c.SetContent(x, y, '─', nil, style)
	}
	c.SetContent(w-1, y, '┐', nil, style)
}

func drawBorderBottom(c Canvas, w, y int) {
	style := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	c.SetContent(0, y, '└', nil, style)
	for x := 1; x < w-1; x++ {
		c.SetContent(x, y, '─', nil, style)
	}
	c.SetContent(w-1, y, '┘', nil, style)
}

func formatOutput(item model.Item, cfg Config) string {
	if len(cfg.AcceptNth) > 0 {
		// Use clean fields for output (ANSI stripped) so downstream consumers get plain text
		var parts []string
		for _, col := range cfg.AcceptNth {
			idx := col - 1
			if idx >= 0 && idx < len(item.Fields) {
				parts = append(parts, item.Fields[idx])
			}
		}
		return strings.Join(parts, "\t")
	}
	// No accept-nth: return the original line (preserves ANSI for piping)
	if item.Original != "" {
		return item.Original
	}
	return strings.Join(item.Fields, "\t")
}

// RunFilter runs in non-interactive mode (like fzf --filter).
func RunFilter(items []model.Item, query string, cfg Config) {
	searchCols := cfg.SearchCols
	if len(searchCols) == 0 {
		searchCols = cfg.Nth
	}

	var matched []model.Item
	for _, item := range items {
		score, indices := scorer.ScoreItem(item.Fields, query, searchCols, cfg.Tiered, item.Depth, cfg.DepthPenalty)
		if indices != nil {
			m := item
			m.Score = score
			m.MatchIndices = indices
			matched = append(matched, m)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Score > matched[j].Score
	})

	for _, item := range matched {
		if cfg.ShowScores {
			fmt.Fprintf(os.Stdout, "[score=%d] %s\n", item.Score, formatOutput(item, cfg))
		} else {
			fmt.Fprintln(os.Stdout, formatOutput(item, cfg))
		}
	}
}
