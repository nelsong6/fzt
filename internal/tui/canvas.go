package tui

import "github.com/gdamore/tcell/v2"

// Canvas is the drawing target abstraction. Both tcell.Screen and the
// headless MemScreen implement it, so all draw* functions work with either.
type Canvas interface {
	SetContent(x, y int, primary rune, combining []rune, style tcell.Style)
	Size() (int, int)
	ShowCursor(x, y int)
}

// tcellCanvas wraps a real tcell.Screen to satisfy Canvas.
type tcellCanvas struct {
	screen tcell.Screen
}

func (c *tcellCanvas) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	c.screen.SetContent(x, y, primary, combining, style)
}

func (c *tcellCanvas) Size() (int, int) {
	return c.screen.Size()
}

func (c *tcellCanvas) ShowCursor(x, y int) {
	c.screen.ShowCursor(x, y)
}

// MemScreen is a headless in-memory screen for simulation/testing.
type MemScreen struct {
	W, H    int
	Grid    [][]rune
	Styles  [][]tcell.Style
	CursorX int
	CursorY int
}

// NewMemScreen creates a blank in-memory screen.
func NewMemScreen(w, h int) *MemScreen {
	grid := make([][]rune, h)
	styles := make([][]tcell.Style, h)
	for y := 0; y < h; y++ {
		grid[y] = make([]rune, w)
		styles[y] = make([]tcell.Style, w)
		for x := 0; x < w; x++ {
			grid[y][x] = ' '
		}
	}
	return &MemScreen{W: w, H: h, Grid: grid, Styles: styles}
}

func (m *MemScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	if x >= 0 && x < m.W && y >= 0 && y < m.H {
		m.Grid[y][x] = primary
		m.Styles[y][x] = style
	}
}

func (m *MemScreen) Size() (int, int) {
	return m.W, m.H
}

func (m *MemScreen) ShowCursor(x, y int) {
	m.CursorX = x
	m.CursorY = y
}

// Clear resets all cells to spaces.
func (m *MemScreen) Clear() {
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			m.Grid[y][x] = ' '
			m.Styles[y][x] = tcell.StyleDefault
		}
	}
}

// Snapshot returns the grid as a string, trimming trailing whitespace per line.
func (m *MemScreen) Snapshot() string {
	var lines []string
	for y := 0; y < m.H; y++ {
		line := string(m.Grid[y])
		// Trim trailing spaces but keep the line
		trimmed := trimRight(line)
		lines = append(lines, trimmed)
	}
	return joinLines(lines)
}

// StyledSnapshot returns the grid annotated with style markers.
// [H] = highlighted (green/bold), [S] = selected (blue bg), [*] = both.
// Plain characters have no marker.
func (m *MemScreen) StyledSnapshot() string {
	var lines []string
	for y := 0; y < m.H; y++ {
		var line []rune
		for x := 0; x < m.W; x++ {
			ch := m.Grid[y][x]
			style := m.Styles[y][x]
			fg, bg, attrs := style.Decompose()
			isHL := fg == tcell.ColorGreen && attrs&tcell.AttrBold != 0
			isSel := bg == tcell.ColorDarkBlue

			if isHL && isSel {
				line = append(line, '[', '*', ']')
			} else if isHL {
				line = append(line, '[', 'H', ']')
			} else if isSel {
				line = append(line, '[', 'S', ']')
			}
			line = append(line, ch)
		}
		trimmed := trimRight(string(line))
		lines = append(lines, trimmed)
	}
	return joinLines(lines)
}

func trimRight(s string) string {
	runes := []rune(s)
	end := len(runes)
	for end > 0 && runes[end-1] == ' ' {
		end--
	}
	return string(runes[:end])
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
