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
	ctx := s.topCtx()
	var children []int
	if parentIdx < 0 {
		for i, item := range ctx.allItems {
			if item.Depth == 0 {
				children = append(children, i)
			}
		}
	} else {
		children = ctx.allItems[parentIdx].Children
	}

	for _, idx := range children {
		if idx >= len(ctx.allItems) {
			continue
		}
		*rows = append(*rows, treeRow{item: ctx.allItems[idx], itemIdx: idx})
		expanded := ctx.treeExpanded[idx] || ctx.queryExpanded[idx]
		if ctx.allItems[idx].HasChildren && expanded {
			buildVisibleTree(s, idx, rows)
		}
	}
}

// updateQueryExpansion sets auto-expansion to reveal the top match in the tree.
func updateQueryExpansion(s *state) {
	ctx := s.topCtx()
	ctx.queryExpanded = make(map[int]bool)
	if len(ctx.filtered) == 0 {
		return
	}
	// Walk ancestor chain of top match, expanding each
	topMatch := ctx.filtered[0]
	idx := findInAll(ctx.allItems, topMatch)
	if idx < 0 {
		return
	}
	for {
		parentIdx := ctx.allItems[idx].ParentIdx
		if parentIdx < 0 {
			break
		}
		ctx.queryExpanded[parentIdx] = true
		idx = parentIdx
	}
}

// syncTreeCursorToTopMatch moves the tree cursor to the top match position
// in the visible tree. Called after filtering to keep the cursor on the best match.
func syncTreeCursorToTopMatch(s *state) {
	ctx := s.topCtx()
	if len(ctx.filtered) == 0 {
		return
	}
	topIdx := findInAll(ctx.allItems, ctx.filtered[0])
	if topIdx < 0 {
		return
	}
	visible := treeVisibleItems(s)
	for vi, row := range visible {
		if row.itemIdx == topIdx {
			ctx.treeCursor = vi
			return
		}
	}
}

// pushScope enters a folder, expanding it in the tree.
func pushScope(s *state, itemIdx int, cfg Config, searchCols []int) {
	ctx := s.topCtx()
	// Save current state
	ctx.scope[len(ctx.scope)-1].query = ctx.query
	ctx.scope[len(ctx.scope)-1].cursor = ctx.cursor
	ctx.scope[len(ctx.scope)-1].index = ctx.treeCursor
	ctx.scope[len(ctx.scope)-1].offset = ctx.treeOffset

	// Push new scope level, recording whether folder was already expanded
	ctx.scope = append(ctx.scope, scopeLevel{
		parentIdx:   itemIdx,
		wasExpanded: ctx.treeExpanded[itemIdx],
	})
	ctx.items = childrenOf(ctx.allItems, itemIdx)

	// Expand the folder in the tree so its children are visible
	ctx.treeExpanded[itemIdx] = true

	// Activate search within scope
	ctx.searchActive = true
	ctx.query = nil
	ctx.cursor = 0
	ctx.queryExpanded = make(map[int]bool)
	filterItems(s, cfg, searchCols)
}

// popScope exits the current folder scope, returning to the parent.
func popScope(s *state, cfg Config, searchCols []int) {
	ctx := s.topCtx()
	if len(ctx.scope) <= 1 {
		return
	}
	popped := ctx.scope[len(ctx.scope)-1]
	ctx.scope = ctx.scope[:len(ctx.scope)-1]
	prev := ctx.scope[len(ctx.scope)-1]

	// Collapse the folder if pushScope was the one that expanded it
	if !popped.wasExpanded && popped.parentIdx >= 0 {
		delete(ctx.treeExpanded, popped.parentIdx)
	}

	if prev.parentIdx < 0 {
		ctx.items = rootItemsOf(ctx.allItems)
	} else {
		ctx.items = childrenOf(ctx.allItems, prev.parentIdx)
	}

	ctx.query = prev.query
	ctx.cursor = prev.cursor
	ctx.treeCursor = prev.index
	ctx.treeOffset = prev.offset

	// If we're back at root with no query, deactivate search
	if len(ctx.scope) <= 1 && len(ctx.query) == 0 {
		ctx.searchActive = false
		ctx.filtered = nil
		ctx.treeCursor = -1
		ctx.queryExpanded = make(map[int]bool)
	} else {
		filterItems(s, cfg, searchCols)
		updateQueryExpansion(s)
	}
}

// handleTreeKey processes a key event when no query is active (tree navigation).
func handleTreeKey(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) (action string, switchToSearch bool) {
	ctx := s.topCtx()
	visible := treeVisibleItems(s)
	visLen := len(visible)

	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel", false

	case tcell.KeyUp, tcell.KeyCtrlP:
		ctx.navMode = true
		if visLen > 0 {
			if ctx.treeCursor <= 0 {
				ctx.treeCursor = visLen - 1
			} else {
				ctx.treeCursor--
			}
		}
		return "", false

	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
		ctx.navMode = true
		if visLen > 0 {
			if ctx.treeCursor < 0 {
				ctx.treeCursor = 0
			} else {
				ctx.treeCursor++
				if ctx.treeCursor >= visLen {
					ctx.treeCursor = 0
				}
			}
		}
		return "", false

	case tcell.KeyBacktab:
		ctx.navMode = true
		if visLen > 0 {
			ctx.treeCursor--
			if ctx.treeCursor < 0 {
				ctx.treeCursor = visLen - 1
			}
		}
		return "", false

	case tcell.KeyEnter:
		if ctx.treeCursor >= 0 && ctx.treeCursor < visLen {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren {
				pushScope(s, row.itemIdx, cfg, searchCols)
				return "", false
			}
			if ctx.onLeafSelect != nil {
				return ctx.onLeafSelect(row.item), false
			}
			return "select:" + formatOutput(row.item, cfg), false
		}
		return "", false

	case tcell.KeyRight:
		ctx.navMode = true
		if ctx.treeCursor >= 0 && ctx.treeCursor < visLen {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren {
				if !ctx.treeExpanded[row.itemIdx] {
					// Expand collapsed folder
					ctx.treeExpanded[row.itemIdx] = true
				} else {
					// Already expanded — move to first child
					if ctx.treeCursor+1 < visLen {
						ctx.treeCursor++
					}
				}
			}
		}
		return "", false

	case tcell.KeyLeft:
		ctx.navMode = true
		if ctx.treeCursor >= 0 && ctx.treeCursor < visLen {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren && ctx.treeExpanded[row.itemIdx] {
				// Collapse expanded folder
				ctx.treeExpanded[row.itemIdx] = false
			} else if row.item.ParentIdx >= 0 {
				// Move cursor to parent
				for vi, vr := range visible {
					if vr.itemIdx == row.item.ParentIdx {
						ctx.treeCursor = vi
						break
					}
				}
			}
		}
		return "", false

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// Pop scope first, then context
		if len(ctx.scope) > 1 {
			popScope(s, cfg, searchCols)
			return "", false
		}
		if len(s.contexts) > 1 {
			popContext(s)
			return "", false
		}
		return "", false

	case tcell.KeyEscape:
		// Pop scope first, then context
		if len(ctx.scope) > 1 {
			popScope(s, cfg, searchCols)
			return "", false
		}
		if len(s.contexts) > 1 {
			popContext(s)
			return "", false
		}
		// Root context: exit nav mode, clear query, deselect, collapse all
		ctx.navMode = false
		ctx.searchActive = false
		ctx.query = nil
		ctx.cursor = 0
		ctx.treeCursor = -1
		ctx.treeExpanded = make(map[int]bool)
		ctx.queryExpanded = make(map[int]bool)
		return "", false

	case tcell.KeyRune:
		return "", true
	}

	return "", false
}

// handleSearchKey handles all keys when search is active.
// The tree is always the navigation surface — Up/Down move the tree cursor,
// typing edits the query and auto-positions the cursor on the top match.
func handleSearchKey(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	ctx := s.topCtx()
	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		if len(ctx.query) > 0 {
			// Clear query, collapse auto-expansions
			ctx.query = nil
			ctx.cursor = 0
			ctx.queryExpanded = make(map[int]bool)
			if len(ctx.scope) <= 1 {
				ctx.searchActive = false
				ctx.filtered = nil
				ctx.treeCursor = -1
			} else {
				filterItems(s, cfg, searchCols)
			}
			return ""
		}
		if len(ctx.scope) > 1 {
			popScope(s, cfg, searchCols)
			return ""
		}
		// At root with empty query — pop context if stacked, else deactivate search
		if len(s.contexts) > 1 {
			popContext(s)
			return ""
		}
		ctx.searchActive = false
		ctx.filtered = nil
		ctx.treeCursor = -1
		ctx.queryExpanded = make(map[int]bool)
		return ""

	case tcell.KeyUp, tcell.KeyCtrlP:
		ctx.navMode = true
		visible := treeVisibleItems(s)
		if len(visible) > 0 {
			if ctx.treeCursor <= 0 {
				ctx.treeCursor = len(visible) - 1
			} else {
				ctx.treeCursor--
			}
		}
		return ""

	case tcell.KeyDown, tcell.KeyCtrlN:
		ctx.navMode = true
		visible := treeVisibleItems(s)
		if len(visible) > 0 {
			if ctx.treeCursor < 0 {
				ctx.treeCursor = 0
			} else {
				ctx.treeCursor++
				if ctx.treeCursor >= len(visible) {
					ctx.treeCursor = 0
				}
			}
		}
		return ""

	case tcell.KeyTab:
		// Autocomplete: set query to the top match's name.
		// If the match is a folder, push scope (same as typing name + Space).
		if len(ctx.filtered) > 0 && len(ctx.filtered[0].Fields) > 0 {
			topMatch := ctx.filtered[0]
			name := topMatch.Fields[0]
			if !strings.EqualFold(string(ctx.query), name) {
				// First Tab: autocomplete the name
				ctx.query = []rune(name)
				ctx.cursor = len(ctx.query)
				filterItems(s, cfg, searchCols)
				updateQueryExpansion(s)
				syncTreeCursorToTopMatch(s)
			}
			// If folder, push scope (same behavior as Space)
			if topMatch.HasChildren {
				idx := findInAll(ctx.allItems, topMatch)
				if idx >= 0 {
					pushScope(s, idx, cfg, searchCols)
				}
			}
		}
		return ""

	case tcell.KeyEnter:
		// Act on tree cursor item
		visible := treeVisibleItems(s)
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren {
				pushScope(s, row.itemIdx, cfg, searchCols)
				return ""
			}
			if ctx.onLeafSelect != nil {
				return ctx.onLeafSelect(row.item)
			}
			return "select:" + formatOutput(row.item, cfg)
		}
		// No cursor — act on top match
		if len(ctx.filtered) > 0 {
			selected := ctx.filtered[0]
			if selected.HasChildren {
				idx := findInAll(ctx.allItems, selected)
				if idx >= 0 {
					pushScope(s, idx, cfg, searchCols)
				}
				return ""
			}
			if ctx.onLeafSelect != nil {
				return ctx.onLeafSelect(selected)
			}
			return "select:" + formatOutput(selected, cfg)
		}
		return ""

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		ctx.navMode = false
		if len(ctx.query) == 0 && len(ctx.scope) > 1 {
			popScope(s, cfg, searchCols)
			return ""
		}
		if len(ctx.query) == 0 && len(s.contexts) > 1 {
			popContext(s)
			return ""
		}
		if len(ctx.query) > 0 {
			ctx.query = ctx.query[:len(ctx.query)-1]
			ctx.cursor = len(ctx.query)
			if len(ctx.query) == 0 {
				ctx.queryExpanded = make(map[int]bool)
				ctx.treeCursor = -1
				if len(ctx.scope) <= 1 {
					ctx.searchActive = false
					ctx.filtered = nil
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
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren && ctx.treeExpanded[row.itemIdx] {
				ctx.navMode = true
				ctx.treeExpanded[row.itemIdx] = false
			} else if row.item.ParentIdx >= 0 {
				ctx.navMode = true
				for vi, vr := range visible {
					if vr.itemIdx == row.item.ParentIdx {
						ctx.treeCursor = vi
						break
					}
				}
			} else if ctx.navMode {
				// Already leftmost — exit nav mode, return to search
				ctx.navMode = false
			}
		}
		return ""

	case tcell.KeyRight:
		ctx.navMode = true
		// Tree navigation: expand or move to first child
		visible := treeVisibleItems(s)
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
			row := visible[ctx.treeCursor]
			if row.item.HasChildren {
				if !ctx.treeExpanded[row.itemIdx] {
					ctx.treeExpanded[row.itemIdx] = true
				} else if ctx.treeCursor+1 < len(visible) {
					ctx.treeCursor++
				}
			}
		}
		return ""

	case tcell.KeyCtrlU:
		ctx.navMode = false
		ctx.query = nil
		ctx.cursor = 0
		ctx.queryExpanded = make(map[int]bool)
		if len(ctx.scope) <= 1 {
			ctx.searchActive = false
			ctx.filtered = nil
		} else {
			filterItems(s, cfg, searchCols)
		}
		return ""

	case tcell.KeyCtrlW:
		ctx.navMode = false
		if len(ctx.query) > 0 {
			// Delete last word from end
			i := len(ctx.query) - 1
			for i > 0 && ctx.query[i-1] == ' ' {
				i--
			}
			for i > 0 && ctx.query[i-1] != ' ' {
				i--
			}
			ctx.query = ctx.query[:i]
			ctx.cursor = len(ctx.query)
			if len(ctx.query) == 0 {
				ctx.queryExpanded = make(map[int]bool)
				ctx.treeCursor = -1
				if len(ctx.scope) <= 1 {
					ctx.searchActive = false
					ctx.filtered = nil
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
		ctx.navMode = false

		// Space on a folder → enter it
		if ch == ' ' {
			visible := treeVisibleItems(s)
			if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
				row := visible[ctx.treeCursor]
				if row.item.HasChildren {
					pushScope(s, row.itemIdx, cfg, searchCols)
					return ""
				}
			}
			// Not on a folder — insert space in query
		}

		// Append character
		ctx.query = append(ctx.query, ch)
		ctx.cursor = len(ctx.query)
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
	ctx := s.topCtx()

	borderOffset := 0
	y := startY

	if cfg.Border {
		versionStr := ""
		if s.showVersion {
			versionStr = Version
		}
		drawBorderTopWithTitle(c, w, y, cfg.Title, cfg.TitlePos, versionStr)
		y++
		borderOffset = 1
	}

	hasQuery := len(ctx.query) > 0

	// Prompt bar — bordered input field, the primary UI element
	promptBg := tcell.ColorValid + 236 // 256-color: #303030, subtle surface
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)

	// Mode indicator: search (magnifying glass) vs nav (arrow) — always shown
	var promptIcon rune
	var promptIconStyle tcell.Style
	if ctx.navMode {
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

	// Context breadcrumb — ':' when in a pushed context (command mode)
	scopeLen := 0
	if len(s.contexts) > 1 && ctx.promptIcon != 0 {
		lockedStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
		c.SetContent(tx+scopeLen, y, ctx.promptIcon, nil, lockedStyle)
		scopeLen++
		c.SetContent(tx+scopeLen, y, ' ', nil, tcell.StyleDefault.Background(promptBg))
		scopeLen++
	}

	// Scope breadcrumb — just the word greyed out with a space after it.
	if len(ctx.scope) > 1 {
		lockedStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
		for si := 1; si < len(ctx.scope); si++ {
			level := ctx.scope[si]
			if level.parentIdx >= 0 && level.parentIdx < len(ctx.allItems) {
				name := ctx.allItems[level.parentIdx].Fields[0]
				drawText(c, tx+scopeLen, y, name, lockedStyle, pw-promptLen-scopeLen)
				scopeLen += len([]rune(name))
				c.SetContent(tx+scopeLen, y, ' ', nil, lockedStyle)
				scopeLen++
			}
		}
	}

	qx := tx + scopeLen // where editable query starts

	// Context breadcrumb — italic path showing where the focused item lives.
	matchCtxLen := 0
	ctxItemIdx := -1
	if ctx.navMode {
		visible := treeVisibleItems(s)
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
			ctxItemIdx = visible[ctx.treeCursor].itemIdx
		}
	} else if hasQuery && len(ctx.filtered) > 0 {
		ctxItemIdx = findInAll(ctx.allItems, ctx.filtered[0])
	}
	if ctxItemIdx >= 0 {
		scopeParent := -1
		if len(ctx.scope) > 1 {
			scopeParent = ctx.scope[len(ctx.scope)-1].parentIdx
		}
		var ancestors []string
		idx := ctx.allItems[ctxItemIdx].ParentIdx
		for idx >= 0 && idx < len(ctx.allItems) && idx != scopeParent {
			if len(ctx.allItems[idx].Fields) > 0 {
				ancestors = append([]string{ctx.allItems[idx].Fields[0]}, ancestors...)
			}
			idx = ctx.allItems[idx].ParentIdx
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

	if ctx.navMode {
		// Nav mode: echo selected item's name in the prompt
		visible := treeVisibleItems(s)
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) && len(visible[ctx.treeCursor].item.Fields) > 0 {
			name := visible[ctx.treeCursor].item.Fields[0]
			nameStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true).Background(promptBg)
			drawText(c, contentX, y, name, nameStyle, contentW)
		}
		c.HideCursor()
	} else if hasQuery {
		queryStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(promptBg)
		drawText(c, contentX, y, string(ctx.query), queryStyle, contentW)
		c.ShowCursor(contentX+ctx.cursor, y)

		// Ghost autocomplete text: show remaining chars of top match if query is a prefix
		if ctx.cursor == len(ctx.query) && len(ctx.filtered) > 0 && len(ctx.filtered[0].Fields) > 0 {
			name := ctx.filtered[0].Fields[0]
			nameRunes := []rune(name)
			if len(nameRunes) > len(ctx.query) && strings.EqualFold(string(nameRunes[:len(ctx.query)]), string(ctx.query)) {
				ghost := string(nameRunes[len(ctx.query):])
				ghostStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Background(promptBg)
				drawText(c, contentX+len(ctx.query), y, ghost, ghostStyle, contentW-len(ctx.query))
			}
		}
	} else if ctx.searchActive || len(ctx.scope) > 1 {
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
	if len(ctx.headers) > 0 {
		hdrStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
		x := borderOffset + 2
		for fi, hdr := range ctx.headers[0].Fields {
			colW := ctx.nameColWidth + ctx.colGap
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
	treeSpace := totalSpace

	// When query active, find top match in tree for highlighting
	topMatchIdx := -1
	if hasQuery && len(ctx.filtered) > 0 {
		topMatchIdx = findInAll(ctx.allItems, ctx.filtered[0])
	}

	// Scroll tree to keep cursor visible
	if ctx.treeCursor >= 0 {
		if ctx.treeCursor < ctx.treeOffset {
			ctx.treeOffset = ctx.treeCursor
		}
		if ctx.treeCursor >= ctx.treeOffset+treeSpace {
			ctx.treeOffset = ctx.treeCursor - treeSpace + 1
		}
	}
	if ctx.treeOffset < 0 {
		ctx.treeOffset = 0
	}

	for i := 0; i < treeSpace; i++ {
		vi := ctx.treeOffset + i
		if vi >= len(visible) {
			break
		}
		row := visible[vi]
		isSelected := vi == ctx.treeCursor
		isTopMatch := hasQuery && !ctx.navMode && row.itemIdx == topMatchIdx && !isSelected
		drawTreeRow(c, row, isSelected, isTopMatch, ctx, cfg, borderOffset, y+i, w)
	}

	if cfg.Border {
		drawBorderBottom(c, w, startY+h-1)
		drawBorderSides(c, w, startY, startY+h-1)
	}
}

// drawTreeRow renders a single tree item row.
func drawTreeRow(c Canvas, row treeRow, isSelected, isTopMatch bool, ctx *treeContext, cfg Config, borderOffset, y, w int) {
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

	nameWidth := ctx.nameColWidth + ctx.colGap - indent
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
	ctx := s.topCtx()
	borderOffset := 0
	if cfg.Border {
		borderOffset = 1
	}

	firstItemRow := borderOffset + 3 // prompt bar (top border + content + bottom border)
	if len(ctx.headers) > 0 {
		firstItemRow += 2 // header + divider
	}

	visible := treeVisibleItems(s)
	itemRow := row - firstItemRow

	if itemRow < 0 {
		return ""
	}

	vi := ctx.treeOffset + itemRow
	if vi >= len(visible) {
		return ""
	}
	ctx.treeCursor = vi
	tr := visible[vi]
	if tr.item.HasChildren {
		ctx.treeExpanded[tr.itemIdx] = !ctx.treeExpanded[tr.itemIdx]
		return ""
	}
	if ctx.onLeafSelect != nil {
		return ctx.onLeafSelect(tr.item)
	}
	return "select:" + formatOutput(tr.item, cfg)
}
