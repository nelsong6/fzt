package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/model"
)

// treeRow represents a single visible row in the tree view.
type treeRow struct {
	item    model.Item
	itemIdx int // index in allItems
}

// treeVisibleItems builds the list of currently visible tree rows
// based on manual expand/collapse state merged with query auto-expansion.
// Always shows from root — scoped folders are just expanded in place.
func treeVisibleItems(s *state) []treeRow {
	var rows []treeRow
	buildVisibleTree(s, -1, &rows)
	return rows
}

func buildVisibleTree(s *state, parentIdx int, rows *[]treeRow) {
	var children []int
	if parentIdx < 0 {
		for i, item := range s.allItems {
			if item.Depth == 0 {
				children = append(children, i)
			}
		}
	} else {
		children = s.allItems[parentIdx].Children
	}

	for _, idx := range children {
		if idx >= len(s.allItems) {
			continue
		}
		*rows = append(*rows, treeRow{item: s.allItems[idx], itemIdx: idx})
		expanded := s.treeExpanded[idx] || s.queryExpanded[idx]
		if s.allItems[idx].HasChildren && expanded {
			buildVisibleTree(s, idx, rows)
		}
	}
}

// updateQueryExpansion sets auto-expansion to reveal the top match in the tree.
func updateQueryExpansion(s *state) {
	s.queryExpanded = make(map[int]bool)
	if len(s.filtered) == 0 {
		return
	}
	// Walk ancestor chain of top match, expanding each
	topMatch := s.filtered[0]
	idx := findInAll(s.allItems, topMatch)
	if idx < 0 {
		return
	}
	for {
		parentIdx := s.allItems[idx].ParentIdx
		if parentIdx < 0 {
			break
		}
		s.queryExpanded[parentIdx] = true
		idx = parentIdx
	}
}

// syncTreeCursorToTopMatch moves the tree cursor to the top match position
// in the visible tree. Called after filtering to keep the cursor on the best match.
func syncTreeCursorToTopMatch(s *state) {
	if len(s.filtered) == 0 {
		return
	}
	topIdx := findInAll(s.allItems, s.filtered[0])
	if topIdx < 0 {
		return
	}
	visible := treeVisibleItems(s)
	for vi, row := range visible {
		if row.itemIdx == topIdx {
			s.treeCursor = vi
			return
		}
	}
}

// pushScope enters a folder, expanding it in the tree.
func pushScope(s *state, itemIdx int, cfg Config, searchCols []int) {
	// Save current state
	s.scope[len(s.scope)-1].query = s.query
	s.scope[len(s.scope)-1].cursor = s.cursor
	s.scope[len(s.scope)-1].index = s.treeCursor
	s.scope[len(s.scope)-1].offset = s.treeOffset

	// Push new scope level, recording whether folder was already expanded
	s.scope = append(s.scope, scopeLevel{
		parentIdx:   itemIdx,
		wasExpanded: s.treeExpanded[itemIdx],
	})
	s.items = childrenOf(s.allItems, itemIdx)

	// Expand the folder in the tree so its children are visible
	s.treeExpanded[itemIdx] = true

	// Activate search within scope
	s.searchActive = true
	s.query = nil
	s.cursor = 0
	s.queryExpanded = make(map[int]bool)
	filterItems(s, cfg, searchCols)
}

// popScope exits the current folder scope, returning to the parent.
func popScope(s *state, cfg Config, searchCols []int) {
	if len(s.scope) <= 1 {
		return
	}
	popped := s.scope[len(s.scope)-1]
	s.scope = s.scope[:len(s.scope)-1]
	prev := s.scope[len(s.scope)-1]

	// Collapse the folder if pushScope was the one that expanded it
	if !popped.wasExpanded && popped.parentIdx >= 0 {
		delete(s.treeExpanded, popped.parentIdx)
	}

	if prev.parentIdx < 0 {
		s.items = rootItems(s.allItems)
	} else {
		s.items = childrenOf(s.allItems, prev.parentIdx)
	}

	s.query = prev.query
	s.cursor = prev.cursor
	s.treeCursor = prev.index
	s.treeOffset = prev.offset

	// If we're back at root with no query, deactivate search
	if len(s.scope) <= 1 && len(s.query) == 0 {
		s.searchActive = false
		s.filtered = nil
		s.treeCursor = -1
		s.queryExpanded = make(map[int]bool)
	} else {
		filterItems(s, cfg, searchCols)
		updateQueryExpansion(s)
	}
}

// handleTreeKey processes a key event when no query is active (tree navigation).
func handleTreeKey(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) (action string, switchToSearch bool) {
	visible := treeVisibleItems(s)
	visLen := len(visible)

	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel", false

	case tcell.KeyUp, tcell.KeyCtrlP:
		s.navMode = true
		if visLen > 0 {
			if s.treeCursor <= 0 {
				s.treeCursor = visLen - 1
			} else {
				s.treeCursor--
			}
		}
		return "", false

	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
		s.navMode = true
		if visLen > 0 {
			if s.treeCursor < 0 {
				s.treeCursor = 0
			} else {
				s.treeCursor++
				if s.treeCursor >= visLen {
					s.treeCursor = 0
				}
			}
		}
		return "", false

	case tcell.KeyBacktab:
		s.navMode = true
		if visLen > 0 {
			s.treeCursor--
			if s.treeCursor < 0 {
				s.treeCursor = visLen - 1
			}
		}
		return "", false

	case tcell.KeyEnter:
		if s.treeCursor >= 0 && s.treeCursor < visLen {
			row := visible[s.treeCursor]
			if row.item.HasChildren {
				// Toggle expand/collapse in place
				s.treeExpanded[row.itemIdx] = !s.treeExpanded[row.itemIdx]
				return "", false
			}
			return "select:" + formatOutput(row.item, cfg), false
		}
		return "", false

	case tcell.KeyRight:
		s.navMode = true
		if s.treeCursor >= 0 && s.treeCursor < visLen {
			row := visible[s.treeCursor]
			if row.item.HasChildren {
				if !s.treeExpanded[row.itemIdx] {
					// Expand collapsed folder
					s.treeExpanded[row.itemIdx] = true
				} else {
					// Already expanded — move to first child
					if s.treeCursor+1 < visLen {
						s.treeCursor++
					}
				}
			}
		}
		return "", false

	case tcell.KeyLeft:
		s.navMode = true
		if s.treeCursor >= 0 && s.treeCursor < visLen {
			row := visible[s.treeCursor]
			if row.item.HasChildren && s.treeExpanded[row.itemIdx] {
				// Collapse expanded folder
				s.treeExpanded[row.itemIdx] = false
			} else if row.item.ParentIdx >= 0 {
				// Move cursor to parent
				for vi, vr := range visible {
					if vr.itemIdx == row.item.ParentIdx {
						s.treeCursor = vi
						break
					}
				}
			}
		}
		return "", false

	case tcell.KeyEscape:
		s.cancelled = true
		return "cancel", false

	case tcell.KeyRune:
		return "", true
	}

	return "", false
}

// handleSearchKey handles all keys when search is active.
// The tree is always the navigation surface — Up/Down move the tree cursor,
// typing edits the query and auto-positions the cursor on the top match.
func handleSearchKey(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		if len(s.query) > 0 {
			// Clear query, collapse auto-expansions
			s.query = nil
			s.cursor = 0
			s.queryExpanded = make(map[int]bool)
			if len(s.scope) <= 1 {
				s.searchActive = false
				s.filtered = nil
				s.treeCursor = -1
			} else {
				filterItems(s, cfg, searchCols)
			}
			return ""
		}
		if len(s.scope) > 1 {
			popScope(s, cfg, searchCols)
			return ""
		}
		s.searchActive = false
		s.filtered = nil
		s.treeCursor = -1
		s.queryExpanded = make(map[int]bool)
		return ""

	case tcell.KeyUp, tcell.KeyCtrlP:
		s.navMode = true
		visible := treeVisibleItems(s)
		if len(visible) > 0 {
			if s.treeCursor <= 0 {
				s.treeCursor = len(visible) - 1
			} else {
				s.treeCursor--
			}
		}
		return ""

	case tcell.KeyDown, tcell.KeyCtrlN:
		s.navMode = true
		visible := treeVisibleItems(s)
		if len(visible) > 0 {
			if s.treeCursor < 0 {
				s.treeCursor = 0
			} else {
				s.treeCursor++
				if s.treeCursor >= len(visible) {
					s.treeCursor = 0
				}
			}
		}
		return ""

	case tcell.KeyTab:
		// Autocomplete: set query to the top match's name.
		// If the query already matches the highlighted item exactly, do nothing.
		// TODO: we don't know how to handle repeated Tab after a perfect
		// autocomplete match yet. Cycling? Confirming? Intentionally left as
		// a no-op until the right behavior is clear.
		if len(s.filtered) > 0 && len(s.filtered[0].Fields) > 0 {
			name := s.filtered[0].Fields[0]
			if strings.EqualFold(string(s.query), name) {
				// Already autocompleted to this match — no-op
				return ""
			}
			s.query = []rune(name)
			s.cursor = len(s.query)
			filterItems(s, cfg, searchCols)
			updateQueryExpansion(s)
			syncTreeCursorToTopMatch(s)
		}
		return ""

	case tcell.KeyEnter:
		// Act on tree cursor item
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) {
			row := visible[s.treeCursor]
			if row.item.HasChildren {
				s.treeExpanded[row.itemIdx] = !s.treeExpanded[row.itemIdx]
				return ""
			}
			return "select:" + formatOutput(row.item, cfg)
		}
		// No cursor — act on top match
		if len(s.filtered) > 0 {
			selected := s.filtered[0]
			if selected.HasChildren {
				idx := findInAll(s.allItems, selected)
				if idx >= 0 {
					s.treeExpanded[idx] = !s.treeExpanded[idx]
				}
				return ""
			}
			return "select:" + formatOutput(selected, cfg)
		}
		return ""

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		s.navMode = false
		if len(s.query) == 0 && len(s.scope) > 1 {
			popScope(s, cfg, searchCols)
			return ""
		}
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
			s.cursor = len(s.query)
			if len(s.query) == 0 {
				s.queryExpanded = make(map[int]bool)
				s.treeCursor = -1
				if len(s.scope) <= 1 {
					s.searchActive = false
					s.filtered = nil
				} else {
					filterItems(s, cfg, searchCols)
				}
			} else {
				filterItems(s, cfg, searchCols)
				updateQueryExpansion(s)
				syncTreeCursorToTopMatch(s)
			}
		}
		return ""

	case tcell.KeyLeft:
		// Tree navigation: collapse or move to parent
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) {
			row := visible[s.treeCursor]
			if row.item.HasChildren && s.treeExpanded[row.itemIdx] {
				s.navMode = true
				s.treeExpanded[row.itemIdx] = false
			} else if row.item.ParentIdx >= 0 {
				s.navMode = true
				for vi, vr := range visible {
					if vr.itemIdx == row.item.ParentIdx {
						s.treeCursor = vi
						break
					}
				}
			} else if s.navMode {
				// Already leftmost — exit nav mode, return to search
				s.navMode = false
			}
		}
		return ""

	case tcell.KeyRight:
		s.navMode = true
		// Tree navigation: expand or move to first child
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) {
			row := visible[s.treeCursor]
			if row.item.HasChildren {
				if !s.treeExpanded[row.itemIdx] {
					s.treeExpanded[row.itemIdx] = true
				} else if s.treeCursor+1 < len(visible) {
					s.treeCursor++
				}
			}
		}
		return ""

	case tcell.KeyCtrlU:
		s.navMode = false
		s.query = nil
		s.cursor = 0
		s.queryExpanded = make(map[int]bool)
		if len(s.scope) <= 1 {
			s.searchActive = false
			s.filtered = nil
		} else {
			filterItems(s, cfg, searchCols)
		}
		return ""

	case tcell.KeyCtrlW:
		s.navMode = false
		if len(s.query) > 0 {
			// Delete last word from end
			i := len(s.query) - 1
			for i > 0 && s.query[i-1] == ' ' {
				i--
			}
			for i > 0 && s.query[i-1] != ' ' {
				i--
			}
			s.query = s.query[:i]
			s.cursor = len(s.query)
			if len(s.query) == 0 {
				s.queryExpanded = make(map[int]bool)
				s.treeCursor = -1
				if len(s.scope) <= 1 {
					s.searchActive = false
					s.filtered = nil
				} else {
					filterItems(s, cfg, searchCols)
				}
			} else {
				filterItems(s, cfg, searchCols)
				updateQueryExpansion(s)
				syncTreeCursorToTopMatch(s)
			}
		}
		return ""

	case tcell.KeyRune:
		s.navMode = false

		// Space on a folder → enter it
		if ch == ' ' {
			visible := treeVisibleItems(s)
			if s.treeCursor >= 0 && s.treeCursor < len(visible) {
				row := visible[s.treeCursor]
				if row.item.HasChildren {
					pushScope(s, row.itemIdx, cfg, searchCols)
					return ""
				}
			}
			// Not on a folder — insert space in query
		}

		// Append character
		s.query = append(s.query, ch)
		s.cursor = len(s.query)
		filterItems(s, cfg, searchCols)
		updateQueryExpansion(s)
		syncTreeCursorToTopMatch(s)
		return ""
	}

	return ""
}

// ── Unified renderer ──────────────────────────────────────────

// drawUnified renders the prompt bar and tree. The tree is the single
// navigation surface — no separate results section.
func drawUnified(c Canvas, s *state, cfg Config, w, startY, h int) {
	if s.commandMode && s.commandGlobal {
		drawGlobalCommandMode(c, s, cfg, w, startY, h)
		return
	}

	borderOffset := 0
	y := startY

	if cfg.Border {
		drawBorderTopWithTitle(c, w, y, cfg.Title, cfg.TitlePos)
		y++
		borderOffset = 1
	}

	hasQuery := len(s.query) > 0

	// Prompt bar — bordered input field, the primary UI element
	promptBg := tcell.ColorValid + 236 // 256-color: #303030, subtle surface
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)

	// Mode indicator: search (magnifying glass) vs nav (arrow)
	var promptIcon rune
	var promptIconStyle tcell.Style
	if s.navMode {
		promptIcon = '\uF0A9'  //
		promptIconStyle = tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Background(promptBg)
	} else {
		promptIcon = '\uF002'  //
		promptIconStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true).Background(promptBg)
	}
	promptLen := 2 // icon + space

	// Top border of prompt bar
	c.SetContent(borderOffset, y, '\u250c', nil, borderStyle)     // ┌
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, '\u2500', nil, borderStyle)             // ─
	}
	c.SetContent(w-borderOffset-1, y, '\u2510', nil, borderStyle) // ┐
	y++

	// Prompt content line with background
	c.SetContent(borderOffset, y, '\u2502', nil, borderStyle) // │
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, ' ', nil, tcell.StyleDefault.Background(promptBg))
	}
	c.SetContent(w-borderOffset-1, y, '\u2502', nil, borderStyle) // │

	px := borderOffset + 1 // content starts inside the border
	pw := w - borderOffset*2 - 2 // content width inside borders
	// Prompt: [icon] [locked breadcrumb ›] [query or nav preview]
	c.SetContent(px, y, promptIcon, nil, promptIconStyle)
	c.SetContent(px+1, y, ' ', nil, tcell.StyleDefault.Background(promptBg))
	tx := px + promptLen // text position after icon + space

	// Scope breadcrumb — just the word greyed out with a space after it.
	scopeLen := 0
	if len(s.scope) > 1 {
		lockedStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
		for si := 1; si < len(s.scope); si++ {
			level := s.scope[si]
			if level.parentIdx >= 0 && level.parentIdx < len(s.allItems) {
				name := s.allItems[level.parentIdx].Fields[0]
				drawText(c, tx+scopeLen, y, name, lockedStyle, pw-promptLen-scopeLen)
				scopeLen += len([]rune(name))
				c.SetContent(tx+scopeLen, y, ' ', nil, lockedStyle)
				scopeLen++
			}
		}
	}

	qx := tx + scopeLen // where editable query starts

	// Context breadcrumb — italic path showing where the focused item lives.
	// In search mode: ancestor path of the top match.
	// In nav mode: ancestor path of the tree cursor item.
	// Purely display-derived, not a real scope.
	matchCtxLen := 0
	ctxItemIdx := -1
	if s.navMode {
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) {
			ctxItemIdx = visible[s.treeCursor].itemIdx
		}
	} else if hasQuery && len(s.filtered) > 0 {
		ctxItemIdx = findInAll(s.allItems, s.filtered[0])
	}
	if ctxItemIdx >= 0 {
		scopeParent := -1
		if len(s.scope) > 1 {
			scopeParent = s.scope[len(s.scope)-1].parentIdx
		}
		var ancestors []string
		idx := s.allItems[ctxItemIdx].ParentIdx
		for idx >= 0 && idx < len(s.allItems) && idx != scopeParent {
			if len(s.allItems[idx].Fields) > 0 {
				ancestors = append([]string{s.allItems[idx].Fields[0]}, ancestors...)
			}
			idx = s.allItems[idx].ParentIdx
		}
		if len(ancestors) > 0 {
			ctxStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
			for _, a := range ancestors {
				aRunes := []rune(a)
				drawText(c, qx+matchCtxLen, y, a, ctxStyle, pw-promptLen-scopeLen-matchCtxLen)
				matchCtxLen += len(aRunes)
				drawText(c, qx+matchCtxLen, y, " \u203A ", ctxStyle, pw-promptLen-scopeLen-matchCtxLen)
				matchCtxLen += 3
			}
		}
	}

	contentX := qx + matchCtxLen // where query or nav preview starts
	contentW := pw - promptLen - scopeLen - matchCtxLen

	if s.navMode {
		// Nav mode: echo selected item's name in the prompt
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) && len(visible[s.treeCursor].item.Fields) > 0 {
			name := visible[s.treeCursor].item.Fields[0]
			nameStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
			drawText(c, contentX, y, name, nameStyle, contentW)
		}
		c.HideCursor()
	} else if hasQuery {
		queryStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(promptBg)
		drawText(c, contentX, y, string(s.query), queryStyle, contentW)
		c.ShowCursor(contentX+s.cursor, y)

		// Ghost autocomplete text: show remaining chars of top match if query is a prefix
		if s.cursor == len(s.query) && len(s.filtered) > 0 && len(s.filtered[0].Fields) > 0 {
			name := s.filtered[0].Fields[0]
			nameRunes := []rune(name)
			if len(nameRunes) > len(s.query) && strings.EqualFold(string(nameRunes[:len(s.query)]), string(s.query)) {
				ghost := string(nameRunes[len(s.query):])
				ghostStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
				drawText(c, contentX+len(s.query), y, ghost, ghostStyle, contentW-len(s.query))
			}
		}
	} else if s.searchActive || len(s.scope) > 1 {
		hintStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
		drawText(c, qx, y, "search\u2026", hintStyle, pw-promptLen-scopeLen)
		c.ShowCursor(qx, y)
	} else {
		hintStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
		drawText(c, qx, y, "type to search\u2026", hintStyle, pw-promptLen-scopeLen)
		c.ShowCursor(qx, y)
	}
	y++

	// Bottom border of prompt bar
	c.SetContent(borderOffset, y, '\u2514', nil, borderStyle)     // └
	for x := borderOffset + 1; x < w-borderOffset-1; x++ {
		c.SetContent(x, y, '\u2500', nil, borderStyle)             // ─
	}
	c.SetContent(w-borderOffset-1, y, '\u2518', nil, borderStyle) // ┘
	y++

	// Headers
	if len(s.headers) > 0 {
		hdrStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
		x := borderOffset + 2
		for fi, hdr := range s.headers[0].Fields {
			colW := s.nameColWidth + s.colGap
			if fi > 0 {
				if cfg.Tiered {
					x += 2
				}
				colW = 0
			}
			drawText(c, x, y, hdr, hdrStyle, w-x-borderOffset)
			x += colW
		}
		y++

		divStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		for x := borderOffset + 1; x < w-borderOffset; x++ {
			c.SetContent(x, y, '\u2500', nil, divStyle)
		}
		y++
	}

	// Tree section — the single navigation surface
	visible := treeVisibleItems(s)
	totalSpace := h - (y - startY) - borderOffset

	// Reserve space for contextual command panel at the bottom
	cmdPanelH := 0
	if s.commandMode && !s.commandGlobal {
		// 3 rows prompt bar + 2 rows headers + command rows
		cmdRows := len(s.commandFiltered)
		if cmdRows > 5 {
			cmdRows = 5
		}
		cmdPanelH = 5 + cmdRows // prompt(3) + headers(2) + rows
		if cmdPanelH > totalSpace/2 {
			cmdPanelH = totalSpace / 2
		}
	}
	treeSpace := totalSpace - cmdPanelH

	// When query active, find top match in tree for highlighting
	topMatchIdx := -1
	if hasQuery && len(s.filtered) > 0 {
		topMatchIdx = findInAll(s.allItems, s.filtered[0])
	}

	// Scroll tree to keep cursor visible
	if s.treeCursor >= 0 {
		if s.treeCursor < s.treeOffset {
			s.treeOffset = s.treeCursor
		}
		if s.treeCursor >= s.treeOffset+treeSpace {
			s.treeOffset = s.treeCursor - treeSpace + 1
		}
	}
	if s.treeOffset < 0 {
		s.treeOffset = 0
	}

	for i := 0; i < treeSpace; i++ {
		vi := s.treeOffset + i
		if vi >= len(visible) {
			break
		}
		row := visible[vi]
		isSelected := vi == s.treeCursor
		isTopMatch := hasQuery && !s.navMode && row.itemIdx == topMatchIdx && !isSelected
		drawTreeRow(c, row, isSelected, isTopMatch, s, cfg, borderOffset, y+i, w)
	}

	// Contextual command panel at the bottom
	if s.commandMode && !s.commandGlobal && cmdPanelH > 0 {
		panelY := y + treeSpace
		drawContextualCommandPanel(c, s, cfg, borderOffset, panelY, w, cmdPanelH)
	}

	if cfg.Border {
		drawBorderBottom(c, w, startY+h-1)
		drawBorderSides(c, w, startY, startY+h-1)
	}
}

// drawTreeRow renders a single tree item row.
func drawTreeRow(c Canvas, row treeRow, isSelected, isTopMatch bool, s *state, cfg Config, borderOffset, y, w int) {
	// Fill background
	if isSelected || isTopMatch {
		bg := tcell.StyleDefault.Background(tcell.ColorDarkBlue)
		for x := borderOffset; x < w-borderOffset; x++ {
			c.SetContent(x, y, ' ', nil, bg)
		}
	}

	x := borderOffset
	hasBg := isSelected || isTopMatch

	// Selection indicator
	if isSelected {
		indStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true).Background(tcell.ColorDarkBlue)
		drawText(c, x, y, "\u25b8 ", indStyle, 2)
	} else {
		style := tcell.StyleDefault
		if hasBg {
			style = style.Background(tcell.ColorDarkBlue)
		}
		drawText(c, x, y, "  ", style, 2)
	}
	x += 2

	// Indentation
	indent := row.item.Depth * 2
	for i := 0; i < indent; i++ {
		style := tcell.StyleDefault
		if hasBg {
			style = style.Background(tcell.ColorDarkBlue)
		}
		c.SetContent(x+i, y, ' ', nil, style)
	}
	x += indent

	// Icon
	var iconRune rune
	var iconStyle tcell.Style
	if row.item.HasChildren {
		iconRune = '\U000F024B'
		iconStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	} else {
		iconRune = '\uF15B'
		iconStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite)
	}
	if hasBg {
		iconStyle = iconStyle.Background(tcell.ColorDarkBlue)
	}
	c.SetContent(x, y, iconRune, nil, iconStyle)
	x++
	bufStyle := tcell.StyleDefault
	if hasBg {
		bufStyle = bufStyle.Background(tcell.ColorDarkBlue)
	}
	c.SetContent(x, y, ' ', nil, bufStyle)
	x++

	// Name
	name := ""
	if len(row.item.Fields) > 0 {
		name = row.item.Fields[0]
	}
	var nameStyle tcell.Style
	if row.item.HasChildren {
		nameStyle = tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
	} else if isSelected {
		nameStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite)
	} else {
		nameStyle = tcell.StyleDefault
	}
	if hasBg {
		nameStyle = nameStyle.Background(tcell.ColorDarkBlue)
	}

	nameWidth := s.nameColWidth + s.colGap - indent
	nameRunes := []rune(name)
	if nameWidth < len(nameRunes)+1 {
		nameWidth = len(nameRunes) + 1
	}

	// Highlight matched characters for top match
	if isTopMatch && len(row.item.MatchIndices) > 0 && len(row.item.MatchIndices[0]) > 0 {
		drawHighlightedText(c, x, y, name, nameStyle, nameWidth, row.item.MatchIndices[0], hasBg)
	} else {
		drawText(c, x, y, name, nameStyle, nameWidth)
	}
	x += nameWidth

	// Description
	if len(row.item.Fields) > 1 {
		desc := row.item.Fields[1]
		descStyle := tcell.StyleDefault
		if hasBg {
			descStyle = descStyle.Background(tcell.ColorDarkBlue)
		}
		remaining := w - x - borderOffset
		if remaining > 0 {
			drawText(c, x, y, desc, descStyle, remaining)
		}
	}
}

// drawHighlightedText draws text with certain character indices highlighted in green.
func drawHighlightedText(c Canvas, x, y int, text string, baseStyle tcell.Style, maxW int, matchIndices []int, hasBg bool) {
	runes := []rune(text)
	matchSet := make(map[int]bool, len(matchIndices))
	for _, idx := range matchIndices {
		matchSet[idx] = true
	}

	for i, r := range runes {
		if i >= maxW {
			break
		}
		style := baseStyle
		if matchSet[i] {
			style = tcell.StyleDefault.Foreground(tcell.ColorGreen).Bold(true)
			if hasBg {
				style = style.Background(tcell.ColorDarkBlue)
			}
		}
		c.SetContent(x+i, y, r, nil, style)
	}
}

// clickUnifiedRow handles a click on a visual row in the unified view.
func clickUnifiedRow(s *state, row int, cfg Config, h int) string {
	borderOffset := 0
	if cfg.Border {
		borderOffset = 1
	}

	firstItemRow := borderOffset + 3 // prompt bar (top border + content + bottom border)
	if len(s.headers) > 0 {
		firstItemRow += 2 // header + divider
	}

	visible := treeVisibleItems(s)
	itemRow := row - firstItemRow

	if itemRow < 0 {
		return ""
	}

	vi := s.treeOffset + itemRow
	if vi >= len(visible) {
		return ""
	}
	s.treeCursor = vi
	tr := visible[vi]
	if tr.item.HasChildren {
		s.treeExpanded[tr.itemIdx] = !s.treeExpanded[tr.itemIdx]
		return ""
	}
	return "select:" + formatOutput(tr.item, cfg)
}
