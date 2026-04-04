package tui

import (
	"fmt"
	"strings"

	"github.com/nelsong6/fzt/internal/model"
)

// RunInline renders the TUI inline in the main terminal buffer (no alternate screen).
// Used when --height is specified. The picker occupies only the requested percentage
// of the terminal, preserving scrollback above.
func RunInline(items []model.Item, cfg Config) (string, error) {
	rt, err := openRawTerminal()
	if err != nil {
		return "", fmt.Errorf("opening terminal: %w", err)
	}
	defer rt.Close()

	termW, termH, err := rt.Size()
	if err != nil {
		return "", fmt.Errorf("getting terminal size: %w", err)
	}

	inlineH := termH * cfg.Height / 100
	if inlineH < 3 {
		inlineH = 3
	}
	if inlineH > termH {
		inlineH = termH
	}

	// Reserve space: print newlines to scroll the terminal, then move cursor back up
	rt.WriteString(strings.Repeat("\n", inlineH))
	rt.WriteString(fmt.Sprintf("\x1b[%dA", inlineH))

	// Hide the terminal cursor during rendering to avoid flicker
	rt.WriteString("\x1b[?25l")

	// Create a MemScreen sized to the inline region
	mem := NewMemScreen(termW, inlineH)

	// Zero out Height in the config copy so renderFrame uses the full MemScreen
	inlineCfg := cfg
	inlineCfg.Height = 0

	s, searchCols := initState(items, inlineCfg)

	// Initialize tree state if tree mode
	if inlineCfg.TreeMode {
		s.treeExpanded = make(map[int]bool)
		s.queryExpanded = make(map[int]bool)
		s.treeCursor = -1
		s.treeOffset = 0
	}

	// Track cursor row relative to top of reserved region so we can
	// return to the top at the start of the next render without moving
	// the visible cursor away from the prompt between frames.
	cursorRow := 0

	render := func() {
		mem.Clear()
		if inlineCfg.TreeMode {
			w, h := mem.Size()
			drawUnified(mem, s, inlineCfg, w, 0, h)
		} else {
			renderFrame(mem, s, inlineCfg)
		}

		rt.WriteString("\x1b[?25l") // hide cursor during redraw

		// Move from current cursor position to top of reserved region
		if cursorRow > 0 {
			rt.WriteString(fmt.Sprintf("\x1b[%dA", cursorRow))
		}

		// Build frame: each line gets \r (carriage return) + content + \x1b[K (clear to EOL)
		ansi := mem.ToANSI()
		lines := strings.Split(ansi, "\n")
		var frame strings.Builder
		for i, line := range lines {
			frame.WriteString("\r")
			frame.WriteString(line)
			frame.WriteString("\x1b[K") // clear rest of line
			if i < len(lines)-1 {
				frame.WriteString("\n")
			}
		}
		rt.WriteString(frame.String())

		// Position the real cursor
		// After writing, the cursor is on the last line of the region.
		// Move it to where MemScreen says it should be.
		if mem.CursorX >= 0 && mem.CursorY >= 0 {
			// Move from last line (inlineH-1) up to cursorY
			linesUp := (inlineH - 1) - mem.CursorY
			if linesUp > 0 {
				rt.WriteString(fmt.Sprintf("\x1b[%dA", linesUp))
			}
			// Move to column (1-based)
			rt.WriteString(fmt.Sprintf("\r\x1b[%dC", mem.CursorX))
			rt.WriteString("\x1b[?25h") // show cursor
			cursorRow = mem.CursorY
		} else {
			// Cursor hidden — we're still at the last line
			cursorRow = inlineH - 1
		}
	}

	// Initial render
	render()

	// Event loop
	var result string
	for {
		key, ch, err := rt.ReadKey()
		if err != nil {
			break
		}
		if key == 0 && ch == 0 {
			continue
		}

		var action string
		if inlineCfg.TreeMode {
			action = handleUnifiedKey(s, key, ch, inlineCfg, searchCols)
		} else {
			action = handleKeyEvent(s, key, ch, inlineCfg, searchCols)
		}
		render()

		switch {
		case action == "cancel":
			goto cleanup
		case len(action) > 7 && action[:7] == "select:":
			result = action[7:]
			goto cleanup
		}
	}

cleanup:
	// Clear the inline region: move to top, clear each line
	rt.WriteString("\x1b[?25l") // hide cursor during cleanup
	if cursorRow > 0 {
		rt.WriteString(fmt.Sprintf("\x1b[%dA", cursorRow))
	}
	for i := 0; i < inlineH; i++ {
		rt.WriteString("\r\x1b[K") // clear line
		if i < inlineH-1 {
			rt.WriteString("\n")
		}
	}
	// Move back to top of the cleared region so the shell prompt appears there
	if inlineH > 1 {
		rt.WriteString(fmt.Sprintf("\x1b[%dA", inlineH-1))
	}
	rt.WriteString("\r")
	rt.WriteString("\x1b[?25h") // restore cursor

	return result, nil
}
