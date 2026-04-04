package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/model"
)

// commandItem represents a single executable command.
type commandItem struct {
	Name        string
	Description string
	Global      bool // available in global command mode (no selection)
	Contextual  bool // available in contextual command mode (item selected)
}

// commands is the registry of all available commands.
var commands = []commandItem{
	{Name: "version", Description: "Show fzt version", Global: true},
	{Name: "name", Description: "Print item name", Contextual: true},
	{Name: "desc", Description: "Print item description", Contextual: true},
}

// cmdNameColWidth returns the max command name width for column alignment.
func cmdNameColWidth(items []commandItem) int {
	w := 0
	for _, cmd := range items {
		if n := len([]rune(cmd.Name)); n > w {
			w = n
		}
	}
	return w + 2 // gap
}

// enterCommandMode activates command mode.
func enterCommandMode(s *state, global bool) {
	s.commandMode = true
	s.commandGlobal = global
	s.commandQuery = nil
	s.commandCursor = 0
	s.commandRanName = ""
	s.commandOutput = nil
	filterCommands(s)
}

// exitCommandMode returns to normal mode, preserving all tree/search/nav state.
func exitCommandMode(s *state) {
	s.commandMode = false
	s.commandGlobal = false
	s.commandQuery = nil
	s.commandCursor = -1
	s.commandFiltered = nil
	s.commandRanName = ""
	s.commandOutput = nil
}

// filterCommands filters the command registry against the current command query.
func filterCommands(s *state) {
	query := strings.ToLower(string(s.commandQuery))
	s.commandFiltered = nil
	for _, cmd := range commands {
		if s.commandGlobal && !cmd.Global {
			continue
		}
		if !s.commandGlobal && !cmd.Contextual {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(cmd.Name), query) {
			s.commandFiltered = append(s.commandFiltered, cmd)
		}
	}
	if len(s.commandFiltered) > 0 {
		s.commandCursor = 0
	} else {
		s.commandCursor = -1
	}
}

// getContextItem returns the currently selected tree item for contextual commands.
func getContextItem(s *state) *model.Item {
	visible := treeVisibleItems(s)
	if s.treeCursor >= 0 && s.treeCursor < len(visible) {
		return &visible[s.treeCursor].item
	}
	return nil
}

// executeCommand runs the selected command and stores its output.
func executeCommand(s *state, cmd commandItem) {
	s.commandRanName = cmd.Name

	switch cmd.Name {
	case "version":
		v := Version
		if v == "" {
			v = "dev"
		}
		s.commandOutput = []string{"fzt " + v}

	case "name":
		item := getContextItem(s)
		if item != nil && len(item.Fields) > 0 {
			s.commandOutput = []string{item.Fields[0]}
		}

	case "desc":
		item := getContextItem(s)
		if item != nil && len(item.Fields) > 1 {
			s.commandOutput = []string{item.Fields[1]}
		} else {
			s.commandOutput = []string{"(no description)"}
		}
	}
}

// handleCommandKey processes key events in command mode.
func handleCommandKey(s *state, key tcell.Key, ch rune) string {
	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		exitCommandMode(s)
		return ""

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(s.commandQuery) == 0 {
			exitCommandMode(s)
			return ""
		}
		s.commandQuery = s.commandQuery[:len(s.commandQuery)-1]
		filterCommands(s)
		return ""

	case tcell.KeyEnter:
		if s.commandCursor >= 0 && s.commandCursor < len(s.commandFiltered) {
			executeCommand(s, s.commandFiltered[s.commandCursor])
		}
		return ""

	case tcell.KeyUp, tcell.KeyCtrlP:
		if len(s.commandFiltered) > 0 {
			if s.commandCursor <= 0 {
				s.commandCursor = len(s.commandFiltered) - 1
			} else {
				s.commandCursor--
			}
		}
		return ""

	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
		if len(s.commandFiltered) > 0 {
			s.commandCursor++
			if s.commandCursor >= len(s.commandFiltered) {
				s.commandCursor = 0
			}
		}
		return ""

	case tcell.KeyCtrlU:
		s.commandQuery = nil
		filterCommands(s)
		return ""

	case tcell.KeyCtrlW:
		if len(s.commandQuery) > 0 {
			i := len(s.commandQuery) - 1
			for i > 0 && s.commandQuery[i-1] == ' ' {
				i--
			}
			for i > 0 && s.commandQuery[i-1] != ' ' {
				i--
			}
			s.commandQuery = s.commandQuery[:i]
			filterCommands(s)
		}
		return ""

	case tcell.KeyRune:
		s.commandQuery = append(s.commandQuery, ch)
		filterCommands(s)
		return ""
	}

	return ""
}

// ── Shared rendering helpers ─────────────────────────────────

var cmdBlue = tcell.NewRGBColor(86, 156, 214)

// drawCommandPromptBar renders the `:` prompt bar (top border + content + bottom border).
// When output is showing, the prompt displays the command that was run (dimmed).
// Returns the y position after the bottom border.
func drawCommandPromptBar(c Canvas, s *state, borderOffset, startY, w int) int {
	y := startY
	promptBg := tcell.ColorValid + 236
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	if !s.commandGlobal {
		borderStyle = tcell.StyleDefault.Foreground(cmdBlue)
	}

	// Top border
	c.SetContent(borderOffset, y, '\u250c', nil, borderStyle)
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, '\u2500', nil, borderStyle)
	}
	c.SetContent(w-borderOffset-1, y, '\u2510', nil, borderStyle)
	y++

	// Content line with background
	c.SetContent(borderOffset, y, '\u2502', nil, borderStyle)
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, ' ', nil, tcell.StyleDefault.Background(promptBg))
	}
	c.SetContent(w-borderOffset-1, y, '\u2502', nil, borderStyle)

	px := borderOffset + 1
	pw := w - borderOffset*2 - 2

	// ':' icon in blue
	iconStyle := tcell.StyleDefault.Foreground(cmdBlue).Bold(true).Background(promptBg)
	c.SetContent(px, y, ':', nil, iconStyle)
	c.SetContent(px+1, y, ' ', nil, tcell.StyleDefault.Background(promptBg))

	hasOutput := len(s.commandOutput) > 0

	if hasOutput {
		// Show the command that was run, dimmed
		ranStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
		drawText(c, px+2, y, s.commandRanName, ranStyle, pw-2)
		c.HideCursor()
	} else {
		queryStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(promptBg)
		drawText(c, px+2, y, string(s.commandQuery), queryStyle, pw-2)
		c.ShowCursor(px+2+len(s.commandQuery), y)

		// Ghost autocomplete
		qLen := len(s.commandQuery)
		if qLen > 0 && len(s.commandFiltered) > 0 {
			name := s.commandFiltered[0].Name
			nameRunes := []rune(name)
			if len(nameRunes) > qLen && strings.EqualFold(string(nameRunes[:qLen]), string(s.commandQuery)) {
				ghost := string(nameRunes[qLen:])
				ghostStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
				drawText(c, px+2+qLen, y, ghost, ghostStyle, pw-2-qLen)
			}
		}
	}
	y++

	// Bottom border
	c.SetContent(borderOffset, y, '\u2514', nil, borderStyle)
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, '\u2500', nil, borderStyle)
	}
	c.SetContent(w-borderOffset-1, y, '\u2518', nil, borderStyle)
	y++

	return y
}

// drawCommandOutput renders command output lines and "press any key" hint.
// Returns the y position after the last line rendered.
func drawCommandOutput(c Canvas, s *state, borderOffset, startY, w, maxLines int) int {
	y := startY
	outputStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)

	for i, line := range s.commandOutput {
		if i >= maxLines-1 { // leave room for hint
			break
		}
		drawText(c, borderOffset+2, y, line, outputStyle, w-borderOffset*2-2)
		y++
	}

	// "press any key" hint, right-aligned
	hint := "press any key"
	hintStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true)
	hintX := w - borderOffset - len([]rune(hint)) - 1
	if hintX < borderOffset+2 {
		hintX = borderOffset + 2
	}
	// Place hint at the bottom of available space
	hintY := startY + maxLines - 1
	if hintY <= y {
		hintY = y
	}
	drawText(c, hintX, hintY, hint, hintStyle, w-hintX-borderOffset)

	return y
}

// drawCommandHeaders renders the Name/Description column headers and divider.
func drawCommandHeaders(c Canvas, borderOffset, y, w, nameColW int) int {
	hdrStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
	x := borderOffset + 2
	drawText(c, x, y, "Name", hdrStyle, nameColW)
	drawText(c, x+nameColW, y, "Description", hdrStyle, w-x-nameColW-borderOffset)
	y++

	divStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	for dx := borderOffset + 1; dx < w-borderOffset; dx++ {
		c.SetContent(dx, y, '\u2500', nil, divStyle)
	}
	y++

	return y
}

// drawCommandRow renders a single command row with consistent column width.
func drawCommandRow(c Canvas, cmd commandItem, isSelected bool, borderOffset, y, w, nameColW int) {
	if isSelected {
		bg := tcell.StyleDefault.Background(tcell.ColorDarkBlue)
		for x := borderOffset; x < w-borderOffset; x++ {
			c.SetContent(x, y, ' ', nil, bg)
		}
	}

	x := borderOffset
	hasBg := isSelected

	if isSelected {
		indStyle := tcell.StyleDefault.Foreground(cmdBlue).Bold(true).Background(tcell.ColorDarkBlue)
		drawText(c, x, y, "\u25b8 ", indStyle, 2)
	} else {
		drawText(c, x, y, "  ", tcell.StyleDefault, 2)
	}
	x += 2

	nameStyle := tcell.StyleDefault.Foreground(cmdBlue).Bold(true)
	if hasBg {
		nameStyle = nameStyle.Background(tcell.ColorDarkBlue)
	}
	drawText(c, x, y, cmd.Name, nameStyle, nameColW)
	x += nameColW

	if x < w-borderOffset {
		descStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		if hasBg {
			descStyle = descStyle.Background(tcell.ColorDarkBlue)
		}
		drawText(c, x, y, cmd.Description, descStyle, w-x-borderOffset)
	}
}

// ── Global command mode renderer ─────────────────────────────

func drawGlobalCommandMode(c Canvas, s *state, cfg Config, w, startY, h int) {
	borderOffset := 0
	y := startY

	if cfg.Border {
		drawBorderTopWithTitle(c, w, y, cfg.Title, cfg.TitlePos)
		y++
		borderOffset = 1
	}

	y = drawCommandPromptBar(c, s, borderOffset, y, w)

	hasOutput := len(s.commandOutput) > 0
	remaining := h - (y - startY) - borderOffset

	if hasOutput {
		drawCommandOutput(c, s, borderOffset, y, w, remaining)
	} else {
		nameColW := cmdNameColWidth(s.commandFiltered)
		y = drawCommandHeaders(c, borderOffset, y, w, nameColW)

		cmdSpace := h - (y - startY) - borderOffset
		for i := 0; i < cmdSpace && i < len(s.commandFiltered); i++ {
			drawCommandRow(c, s.commandFiltered[i], i == s.commandCursor, borderOffset, y+i, w, nameColW)
		}
	}

	if cfg.Border {
		drawBorderBottom(c, w, startY+h-1)
		drawBorderSides(c, w, startY, startY+h-1)
	}
}

// ── Contextual command panel renderer ────────────────────────

func drawContextualCommandPanel(c Canvas, s *state, _ Config, borderOffset, panelY, w, panelH int) {
	y := drawCommandPromptBar(c, s, borderOffset, panelY, w)

	hasOutput := len(s.commandOutput) > 0
	remaining := panelH - 3 // prompt bar takes 3

	if hasOutput {
		drawCommandOutput(c, s, borderOffset, y, w, remaining)
	} else {
		nameColW := cmdNameColWidth(s.commandFiltered)
		y = drawCommandHeaders(c, borderOffset, y, w, nameColW)

		rows := remaining - 2 // headers take 2
		for i := 0; i < rows && i < len(s.commandFiltered); i++ {
			drawCommandRow(c, s.commandFiltered[i], i == s.commandCursor, borderOffset, y+i, w, nameColW)
		}
	}
}
