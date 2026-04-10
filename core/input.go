package core

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// syncQueryToCursor updates the search query to match the name of the item
// under the tree cursor. Called when navigating away from a search result
// so the search bar follows the highlighted row.
func syncQueryToCursor(ctx *TreeContext, visible []TreeRow) {
	if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
		row := visible[ctx.TreeCursor]
		if len(row.Item.Fields) > 0 {
			ctx.Query = []rune(row.Item.Fields[0])
			ctx.Cursor = len(ctx.Query)
			ctx.Filtered = nil // clear stale top match highlight
		}
	}
}

// HandleUnifiedKey handles all key events in unified tree+search mode.
// The tree is the single navigation surface. Typing filters and auto-expands
// the tree to reveal matches. Up/Down always move the tree cursor.
func HandleUnifiedKey(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	ctx := s.TopCtx()

	// Shift+HJKL -> vim-style navigation (capitals bypass search input)
	if key == tcell.KeyRune {
		var navKey tcell.Key
		switch ch {
		case 'H':
			navKey = tcell.KeyLeft
		case 'J':
			navKey = tcell.KeyDown
		case 'K':
			navKey = tcell.KeyUp
		case 'L':
			navKey = tcell.KeyRight
		}
		if navKey != 0 {
			action, _ := HandleTreeKey(s, navKey, 0, cfg, searchCols)
			return action
		}
	}

	// Nav mode + Ctrl+U: clean slate -- exit nav, clear query, deselect
	if ctx.NavMode && key == tcell.KeyCtrlU {
		ctx.NavMode = false
		ctx.Query = nil
		ctx.Cursor = 0
		ctx.TreeCursor = -1
		ctx.QueryExpanded = make(map[int]bool)
		if len(ctx.Scope) <= 1 {
			ctx.SearchActive = false
			ctx.Filtered = nil
		} else {
			FilterItems(s, cfg, searchCols)
		}
		return ""
	}

	// Nav mode + Backspace: edit the displayed item name (remove last char)
	if ctx.NavMode && (key == tcell.KeyBackspace || key == tcell.KeyBackspace2) {
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) && len(visible[ctx.TreeCursor].Item.Fields) > 0 {
			name := []rune(visible[ctx.TreeCursor].Item.Fields[0])
			if len(name) > 1 {
				ctx.Query = name[:len(name)-1]
				ctx.Cursor = len(ctx.Query)
			} else {
				ctx.Query = nil
				ctx.Cursor = 0
			}
		}
		ctx.NavMode = false
		if len(ctx.Query) > 0 {
			ctx.SearchActive = true
			FilterItems(s, cfg, searchCols)
			UpdateQueryExpansion(s)
			SyncTreeCursorToTopMatch(s)
		} else {
			ctx.SearchActive = false
			ctx.Filtered = nil
			ctx.TreeCursor = -1
			ctx.QueryExpanded = make(map[int]bool)
		}
		return ""
	}

	// When no search active, delegate to tree navigation (except printable chars)
	if !ctx.SearchActive {
		if key == tcell.KeyRune {
			if ch == '/' {
				// Activate search without inserting the /
				ctx.SearchActive = true
				ctx.NavMode = false
				return ""
			}
			// Space on a folder -> push scope (same as Enter)
			if ch == ' ' {
				visible := TreeVisibleItems(s)
				if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
					row := visible[ctx.TreeCursor]
					if row.Item.HasChildren {
						PushScope(s, row.ItemIdx, cfg, searchCols)
						return ""
					}
				}
			}
			// Printable character -> activate search
			ctx.SearchActive = true
			ctx.NavMode = false
			ctx.Query = []rune{ch}
			ctx.Cursor = 1
			FilterItems(s, cfg, searchCols)
			UpdateQueryExpansion(s)
			SyncTreeCursorToTopMatch(s)
			return ""
		}
		action, _ := HandleTreeKey(s, key, ch, cfg, searchCols)
		return action
	}

	// Search active -- unified handling
	return HandleSearchKey(s, key, ch, cfg, searchCols)
}

// HandleKeyEvent processes a single key event against the TUI state (flat mode).
// Returns "" for normal continuation, "cancel" to quit, or "select:<output>" for leaf selection.
func HandleKeyEvent(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	ctx := s.TopCtx()
	switch key {
	case tcell.KeyCtrlC:
		s.Cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		if len(ctx.Query) > 0 {
			ctx.Query = nil
			ctx.Cursor = 0
			ctx.Offset = 0
			FilterItems(s, cfg, searchCols)
			if len(ctx.Filtered) > 0 {
				ctx.Index = 0
			} else {
				ctx.Index = -1
			}
			return ""
		}
		if cfg.Tiered && len(ctx.Scope) > 1 {
			ctx.Scope = ctx.Scope[:len(ctx.Scope)-1]
			prev := ctx.Scope[len(ctx.Scope)-1]
			if prev.ParentIdx < 0 {
				ctx.Items = RootItemsOf(ctx.AllItems)
			} else {
				ctx.Items = ChildrenOf(ctx.AllItems, prev.ParentIdx)
			}
			ctx.Query = prev.Query
			ctx.Cursor = prev.Cursor
			ctx.Index = prev.Index
			ctx.Offset = prev.Offset
			FilterItems(s, cfg, searchCols)
			return ""
		}
		s.Cancelled = true
		return "cancel"

	case tcell.KeyEnter:
		if ctx.Index >= 0 && ctx.Index < len(ctx.Filtered) {
			selected := ctx.Filtered[ctx.Index]
			if selected.HasChildren && cfg.Tiered {
				parentIdx := FindInAll(ctx.AllItems, selected)
				if parentIdx >= 0 {
					ctx.Scope[len(ctx.Scope)-1].Query = ctx.Query
					ctx.Scope[len(ctx.Scope)-1].Cursor = ctx.Cursor
					ctx.Scope[len(ctx.Scope)-1].Index = ctx.Index
					ctx.Scope[len(ctx.Scope)-1].Offset = ctx.Offset
					ctx.Scope = append(ctx.Scope, ScopeLevel{ParentIdx: parentIdx})
					ctx.Items = ChildrenOf(ctx.AllItems, parentIdx)
					ctx.Query = nil
					ctx.Cursor = 0
					ctx.Index = -1
					ctx.Offset = 0
					FilterItems(s, cfg, searchCols)
				}
			} else {
				return "select:" + FormatOutput(selected, cfg)
			}
		}

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if ctx.Cursor > 0 {
			ctx.Query = append(ctx.Query[:ctx.Cursor-1], ctx.Query[ctx.Cursor:]...)
			ctx.Cursor--
			ctx.Offset = 0
			FilterItems(s, cfg, searchCols)
			if len(ctx.Filtered) > 0 {
				ctx.Index = 0
			} else {
				ctx.Index = -1
			}
		}

	case tcell.KeyDelete:
		if ctx.Cursor < len(ctx.Query) {
			ctx.Query = append(ctx.Query[:ctx.Cursor], ctx.Query[ctx.Cursor+1:]...)
			FilterItems(s, cfg, searchCols)
		}

	case tcell.KeyLeft:
		if cfg.Tiered && len(ctx.Query) == 0 && len(ctx.Scope) > 1 {
			ctx.Scope = ctx.Scope[:len(ctx.Scope)-1]
			prev := ctx.Scope[len(ctx.Scope)-1]
			if prev.ParentIdx < 0 {
				ctx.Items = RootItemsOf(ctx.AllItems)
			} else {
				ctx.Items = ChildrenOf(ctx.AllItems, prev.ParentIdx)
			}
			ctx.Query = prev.Query
			ctx.Cursor = prev.Cursor
			ctx.Index = prev.Index
			ctx.Offset = prev.Offset
			FilterItems(s, cfg, searchCols)
		} else if ctx.Index >= 0 {
			ctx.Index = -1
		} else if ctx.Cursor > 0 {
			ctx.Cursor--
		}

	case tcell.KeyRight:
		if ctx.Index >= 0 && cfg.Tiered && len(ctx.Query) == 0 && len(ctx.Filtered) > 0 && ctx.Filtered[ctx.Index].HasChildren {
			selected := ctx.Filtered[ctx.Index]
			parentIdx := FindInAll(ctx.AllItems, selected)
			if parentIdx >= 0 {
				ctx.Scope[len(ctx.Scope)-1].Query = ctx.Query
				ctx.Scope[len(ctx.Scope)-1].Cursor = ctx.Cursor
				ctx.Scope[len(ctx.Scope)-1].Index = ctx.Index
				ctx.Scope[len(ctx.Scope)-1].Offset = ctx.Offset
				ctx.Scope = append(ctx.Scope, ScopeLevel{ParentIdx: parentIdx})
				ctx.Items = ChildrenOf(ctx.AllItems, parentIdx)
				ctx.Query = nil
				ctx.Cursor = 0
				ctx.Index = -1
				ctx.Offset = 0
				FilterItems(s, cfg, searchCols)
			}
		} else if ctx.Index == -1 && ctx.Cursor < len(ctx.Query) {
			ctx.Cursor++
		}

	case tcell.KeyTab:
		if len(ctx.Filtered) > 0 {
			if ctx.Index < len(ctx.Filtered)-1 {
				ctx.Index++
			} else {
				ctx.Index = -1
			}
		}

	case tcell.KeyBacktab:
		if len(ctx.Filtered) > 0 {
			if ctx.Index == -1 {
				ctx.Index = len(ctx.Filtered) - 1
			} else if ctx.Index > 0 {
				ctx.Index--
			} else {
				ctx.Index = -1
			}
		}

	case tcell.KeyUp, tcell.KeyCtrlP:
		if ctx.Index > 0 {
			ctx.Index--
		} else if ctx.Index == 0 {
			ctx.Index = -1
		}

	case tcell.KeyDown, tcell.KeyCtrlN:
		if ctx.Index < len(ctx.Filtered)-1 {
			ctx.Index++
		}

	case tcell.KeyCtrlA:
		ctx.Cursor = 0

	case tcell.KeyCtrlE:
		ctx.Cursor = len(ctx.Query)

	case tcell.KeyCtrlU:
		ctx.Query = ctx.Query[ctx.Cursor:]
		ctx.Cursor = 0
		ctx.Offset = 0
		FilterItems(s, cfg, searchCols)
		if len(ctx.Filtered) > 0 {
			ctx.Index = 0
		} else {
			ctx.Index = -1
		}

	case tcell.KeyCtrlW:
		if ctx.Cursor > 0 {
			end := ctx.Cursor
			for ctx.Cursor > 0 && ctx.Query[ctx.Cursor-1] == ' ' {
				ctx.Cursor--
			}
			for ctx.Cursor > 0 && ctx.Query[ctx.Cursor-1] != ' ' {
				ctx.Cursor--
			}
			ctx.Query = append(ctx.Query[:ctx.Cursor], ctx.Query[end:]...)
			ctx.Offset = 0
			FilterItems(s, cfg, searchCols)
			if len(ctx.Filtered) > 0 {
				ctx.Index = 0
			} else {
				ctx.Index = -1
			}
		}

	case tcell.KeyRune:
		ctx.Query = append(ctx.Query[:ctx.Cursor], append([]rune{ch}, ctx.Query[ctx.Cursor:]...)...)
		ctx.Cursor++
		ctx.Offset = 0
		FilterItems(s, cfg, searchCols)
		if len(ctx.Filtered) > 0 {
			ctx.Index = 0
		} else {
			ctx.Index = -1
		}
	}

	return ""
}

// FormatOutput formats the selected item for output based on accept-nth configuration.
func FormatOutput(item Item, cfg Config) string {
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

// HandleTreeKey processes a key event when no query is active (tree navigation).
func HandleTreeKey(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) (action string, switchToSearch bool) {
	ctx := s.TopCtx()
	visible := TreeVisibleItems(s)
	visLen := len(visible)

	switch key {
	case tcell.KeyCtrlC:
		s.Cancelled = true
		return "cancel", false

	case tcell.KeyUp, tcell.KeyCtrlP:
		ctx.NavMode = true
		if visLen > 0 {
			if ctx.TreeCursor <= 0 {
				ctx.TreeCursor = visLen - 1
			} else {
				ctx.TreeCursor--
			}
		}
		return "", false

	case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
		ctx.NavMode = true
		if visLen > 0 {
			if ctx.TreeCursor < 0 {
				ctx.TreeCursor = 0
			} else {
				ctx.TreeCursor++
				if ctx.TreeCursor >= visLen {
					ctx.TreeCursor = 0
				}
			}
		}
		return "", false

	case tcell.KeyBacktab:
		ctx.NavMode = true
		if visLen > 0 {
			ctx.TreeCursor--
			if ctx.TreeCursor < 0 {
				ctx.TreeCursor = visLen - 1
			}
		}
		return "", false

	case tcell.KeyEnter:
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < visLen {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				curScope := ctx.Scope[len(ctx.Scope)-1]
				if curScope.ParentIdx == row.ItemIdx {
					// Already scoped into this folder — trigger folder-link or no-op
					if row.Item.URL != "" {
						return "select:" + FormatOutput(row.Item, cfg), false
					}
					return "", false
				}
				PushScope(s, row.ItemIdx, cfg, searchCols)
				return "", false
			}
			return "select:" + FormatOutput(row.Item, cfg), false
		}
		return "", false

	case tcell.KeyRight:
		ctx.NavMode = true
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < visLen {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				PushScope(s, row.ItemIdx, cfg, searchCols)
			}
		}
		return "", false

	case tcell.KeyLeft:
		ctx.NavMode = true
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < visLen {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren && ctx.TreeExpanded[row.ItemIdx] {
				// Collapse expanded folder
				ctx.TreeExpanded[row.ItemIdx] = false
			} else if row.Item.ParentIdx >= 0 {
				// Move cursor to parent
				for vi, vr := range visible {
					if vr.ItemIdx == row.Item.ParentIdx {
						ctx.TreeCursor = vi
						break
					}
				}
			}
		}
		return "", false

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// Pop scope first, then context
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return "", false
		}
		if len(s.Contexts) > 1 {
			s.PopContext()
			return "", false
		}
		return "", false

	case tcell.KeyEscape:
		// Pop scope first, then context
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return "", false
		}
		if len(s.Contexts) > 1 {
			s.PopContext()
			return "", false
		}
		// Root context with nothing to clear -- exit
		s.Cancelled = true
		return "cancel", false

	case tcell.KeyRune:
		return "", true
	}

	return "", false
}

// HandleSearchKey handles all keys when search is active.
// The tree is always the navigation surface -- Up/Down move the tree cursor,
// typing edits the query and auto-positions the cursor on the top match.
func HandleSearchKey(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	ctx := s.TopCtx()
	switch key {
	case tcell.KeyCtrlC:
		s.Cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		if len(ctx.Query) > 0 {
			// Clear query, collapse auto-expansions
			ctx.Query = nil
			ctx.Cursor = 0
			ctx.QueryExpanded = make(map[int]bool)
			if len(ctx.Scope) <= 1 {
				ctx.SearchActive = false
				ctx.Filtered = nil
				ctx.TreeCursor = -1
			} else {
				FilterItems(s, cfg, searchCols)
			}
			return ""
		}
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return ""
		}
		// At root with empty query -- pop context if stacked, else exit
		if len(s.Contexts) > 1 {
			s.PopContext()
			return ""
		}
		s.Cancelled = true
		return "cancel"

	case tcell.KeyUp, tcell.KeyCtrlP:
		ctx.NavMode = true
		visible := TreeVisibleItems(s)
		if len(visible) > 0 {
			if ctx.TreeCursor <= 0 {
				ctx.TreeCursor = len(visible) - 1
			} else {
				ctx.TreeCursor--
			}
			syncQueryToCursor(ctx, visible)
		}
		return ""

	case tcell.KeyDown, tcell.KeyCtrlN:
		ctx.NavMode = true
		visible := TreeVisibleItems(s)
		if len(visible) > 0 {
			if ctx.TreeCursor < 0 {
				ctx.TreeCursor = 0
			} else {
				ctx.TreeCursor++
				if ctx.TreeCursor >= len(visible) {
					ctx.TreeCursor = 0
				}
			}
			syncQueryToCursor(ctx, visible)
		}
		return ""

	case tcell.KeyTab:
		// Autocomplete: set query to the top match's name.
		// If the match is a folder, push scope (same as typing name + Space).
		if len(ctx.Filtered) > 0 && len(ctx.Filtered[0].Fields) > 0 {
			topMatch := ctx.Filtered[0]
			name := topMatch.Fields[0]
			if !strings.EqualFold(string(ctx.Query), name) {
				// First Tab: autocomplete the name
				ctx.Query = []rune(name)
				ctx.Cursor = len(ctx.Query)
				FilterItems(s, cfg, searchCols)
				UpdateQueryExpansion(s)
				SyncTreeCursorToTopMatch(s)
			}
			// If folder, push scope (same behavior as Space)
			if topMatch.HasChildren {
				idx := FindInAll(ctx.AllItems, topMatch)
				if idx >= 0 {
					PushScope(s, idx, cfg, searchCols)
				}
			}
		}
		return ""

	case tcell.KeyEnter:
		// Act on tree cursor item
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				curScope := ctx.Scope[len(ctx.Scope)-1]
				if curScope.ParentIdx == row.ItemIdx {
					// Already scoped into this folder — trigger folder-link or no-op
					if row.Item.URL != "" {
						return "select:" + FormatOutput(row.Item, cfg)
					}
					return ""
				}
				PushScope(s, row.ItemIdx, cfg, searchCols)
				return ""
			}
			return "select:" + FormatOutput(row.Item, cfg)
		}
		// No cursor -- act on top match
		if len(ctx.Filtered) > 0 {
			selected := ctx.Filtered[0]
			if selected.HasChildren {
				idx := FindInAll(ctx.AllItems, selected)
				if idx >= 0 {
					curScope := ctx.Scope[len(ctx.Scope)-1]
					if curScope.ParentIdx == idx {
						if selected.URL != "" {
							return "select:" + FormatOutput(selected, cfg)
						}
						return ""
					}
					PushScope(s, idx, cfg, searchCols)
				}
				return ""
			}
			return "select:" + FormatOutput(selected, cfg)
		}
		return ""

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		ctx.NavMode = false
		if len(ctx.Query) == 0 && len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return ""
		}
		if len(ctx.Query) == 0 && len(s.Contexts) > 1 {
			s.PopContext()
			return ""
		}
		if len(ctx.Query) > 0 {
			ctx.Query = ctx.Query[:len(ctx.Query)-1]
			ctx.Cursor = len(ctx.Query)
			if len(ctx.Query) == 0 {
				ctx.QueryExpanded = make(map[int]bool)
				ctx.TreeCursor = -1
				if len(ctx.Scope) <= 1 {
					ctx.SearchActive = false
					ctx.Filtered = nil
				} else {
					FilterItems(s, cfg, searchCols)
				}
			} else {
				FilterItems(s, cfg, searchCols)
				UpdateQueryExpansion(s)
				SyncTreeCursorToTopMatch(s)
			}
		}
		return ""

	case tcell.KeyLeft:
		// Tree navigation: collapse or move to parent
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren && ctx.TreeExpanded[row.ItemIdx] {
				ctx.NavMode = true
				ctx.TreeExpanded[row.ItemIdx] = false
			} else if row.Item.ParentIdx >= 0 {
				ctx.NavMode = true
				for vi, vr := range visible {
					if vr.ItemIdx == row.Item.ParentIdx {
						ctx.TreeCursor = vi
						break
					}
				}
			} else if ctx.NavMode {
				// Already leftmost -- exit nav mode, return to search
				ctx.NavMode = false
			}
		}
		return ""

	case tcell.KeyRight:
		ctx.NavMode = true
		// Tree navigation: expand or move to first child
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				if !ctx.TreeExpanded[row.ItemIdx] {
					ctx.TreeExpanded[row.ItemIdx] = true
				} else if ctx.TreeCursor+1 < len(visible) {
					ctx.TreeCursor++
				}
			}
		}
		return ""

	case tcell.KeyCtrlU:
		ctx.NavMode = false
		ctx.Query = nil
		ctx.Cursor = 0
		ctx.QueryExpanded = make(map[int]bool)
		if len(ctx.Scope) <= 1 {
			ctx.SearchActive = false
			ctx.Filtered = nil
		} else {
			FilterItems(s, cfg, searchCols)
		}
		return ""

	case tcell.KeyCtrlW:
		ctx.NavMode = false
		if len(ctx.Query) > 0 {
			// Delete last word from end
			i := len(ctx.Query) - 1
			for i > 0 && ctx.Query[i-1] == ' ' {
				i--
			}
			for i > 0 && ctx.Query[i-1] != ' ' {
				i--
			}
			ctx.Query = ctx.Query[:i]
			ctx.Cursor = len(ctx.Query)
			if len(ctx.Query) == 0 {
				ctx.QueryExpanded = make(map[int]bool)
				ctx.TreeCursor = -1
				if len(ctx.Scope) <= 1 {
					ctx.SearchActive = false
					ctx.Filtered = nil
				} else {
					FilterItems(s, cfg, searchCols)
				}
			} else {
				FilterItems(s, cfg, searchCols)
				UpdateQueryExpansion(s)
				SyncTreeCursorToTopMatch(s)
			}
		}
		return ""

	case tcell.KeyRune:
		ctx.NavMode = false

		// Space on a folder -> enter it
		if ch == ' ' {
			visible := TreeVisibleItems(s)
			if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
				row := visible[ctx.TreeCursor]
				if row.Item.HasChildren {
					PushScope(s, row.ItemIdx, cfg, searchCols)
					return ""
				}
			}
			// No cursor (hidden top match) -- push scope into it
			if ctx.TreeCursor < 0 && len(ctx.Filtered) > 0 {
				top := ctx.Filtered[0]
				if top.HasChildren {
					idx := FindInAll(ctx.AllItems, top)
					if idx >= 0 {
						PushScope(s, idx, cfg, searchCols)
						return ""
					}
				}
			}
			// Not on a folder -- insert space in query
		}

		// Append character
		ctx.Query = append(ctx.Query, ch)
		ctx.Cursor = len(ctx.Query)
		FilterItems(s, cfg, searchCols)
		UpdateQueryExpansion(s)
		SyncTreeCursorToTopMatch(s)
		return ""
	}

	return ""
}

// ClickUnifiedRow handles a click on a visual row in the unified view.
func ClickUnifiedRow(s *State, row int, cfg Config, h int) string {
	ctx := s.TopCtx()
	borderOffset := 0
	if cfg.Border {
		borderOffset = 1
	}

	firstItemRow := borderOffset + 3 // prompt bar (top border + content + bottom border)
	if len(ctx.Headers) > 0 {
		firstItemRow += 2 // header + divider
	}

	visible := TreeVisibleItems(s)
	itemRow := row - firstItemRow

	if itemRow < 0 {
		return ""
	}

	vi := ctx.TreeOffset + itemRow
	if vi >= len(visible) {
		return ""
	}
	ctx.TreeCursor = vi
	tr := visible[vi]
	if tr.Item.HasChildren {
		ctx.TreeExpanded[tr.ItemIdx] = !ctx.TreeExpanded[tr.ItemIdx]
		return ""
	}
	return "select:" + FormatOutput(tr.Item, cfg)
}

