package core

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// normalModeNavBinding maps normal-mode letter keys (hjkl) to their arrow
// equivalents and a short glyph to flash in the title bar as feedback that
// the nav keystroke was received. Returns ok=false for unbound characters.
func normalModeNavBinding(ch rune) (key tcell.Key, arrow string, ok bool) {
	switch ch {
	case 'h':
		return tcell.KeyLeft, "\u2190", true
	case 'j':
		return tcell.KeyDown, "\u2193", true
	case 'k':
		return tcell.KeyUp, "\u2191", true
	case 'l':
		return tcell.KeyRight, "\u2192", true
	}
	return 0, "", false
}

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

// handleRenameKey processes key events during rename mode.
// Enter confirms, Escape cancels, printable chars edit the buffer.
func handleRenameKey(s *State, key tcell.Key, ch rune) string {
	switch key {
	case tcell.KeyEnter:
		// Confirm edit
		ctx := s.TopCtx()
		if s.EditTargetIdx >= 0 && s.EditTargetIdx < len(ctx.AllItems) {
			target := &ctx.AllItems[s.EditTargetIdx]
			newVal := string(s.EditBuffer)

			if target.IsProperty && target.PropertyOf >= 0 && target.PropertyOf < len(ctx.AllItems) {
				// Property edit — write value back to the parent item's field
				parent := &ctx.AllItems[target.PropertyOf]
				switch target.PropertyKey {
				case "name":
					if len(parent.Fields) > 0 {
						parent.Fields[0] = newVal
					}
				case "description":
					for len(parent.Fields) < 2 {
						parent.Fields = append(parent.Fields, "")
					}
					parent.Fields[1] = newVal
				case "url":
					parent.Action = &ItemAction{Type: "url", Target: newVal}
				case "action":
					if parent.Action != nil {
						parent.Action.Target = newVal
					} else {
						parent.Action = &ItemAction{Type: "command", Target: newVal}
					}
				}
				// Update the property item's display value
				for len(target.Fields) < 2 {
					target.Fields = append(target.Fields, "")
				}
				target.Fields[1] = newVal
				s.Dirty = true
			} else if newVal != "" {
				// Regular item rename
				target.Fields[0] = newVal
				s.Dirty = true
			}
		}
		s.EditMode = ""
		s.EditBuffer = nil
		s.ClearTitle()
		return ""
	case tcell.KeyEscape:
		// Cancel edit — restore original value
		ctx := s.TopCtx()
		if s.EditTargetIdx >= 0 && s.EditTargetIdx < len(ctx.AllItems) {
			target := &ctx.AllItems[s.EditTargetIdx]
			if target.IsProperty {
				// Restore property display value
				for len(target.Fields) < 2 {
					target.Fields = append(target.Fields, "")
				}
				target.Fields[1] = s.EditOrigName
			} else if s.EditOrigName != "" {
				target.Fields[0] = s.EditOrigName
			}
		}
		s.EditMode = ""
		s.EditBuffer = nil
		s.ClearTitle()
		return ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(s.EditBuffer) > 0 {
			s.EditBuffer = s.EditBuffer[:len(s.EditBuffer)-1]
		}
		return ""
	case tcell.KeyRune:
		s.EditBuffer = append(s.EditBuffer, ch)
		return ""
	}
	return ""
}

// ActionResult describes an action emitted by input handling.
// Item is populated when Action was produced by selecting a concrete item.
type ActionResult struct {
	Action  string
	Item    Item
	HasItem bool
}

func noActionResult() ActionResult {
	return ActionResult{}
}

func actionResult(action string) ActionResult {
	return ActionResult{Action: action}
}

func selectActionResult(item Item, cfg Config) ActionResult {
	return ActionResult{
		Action:  "select:" + FormatOutput(item, cfg),
		Item:    item,
		HasItem: true,
	}
}

// HandleUnifiedKey handles all key events in unified tree+search mode.
// The tree is the single navigation surface. Typing filters and auto-expands
// the tree to reveal matches. Up/Down always move the tree cursor.
//
// shift reports whether Shift was held with the key event. Currently only
// Shift+Enter is observed here (universal confirm-select). Event sources
// that can't report modifier state (inline raw-byte parser, anything
// reading from a pipe) should pass false.
func HandleUnifiedKey(s *State, key tcell.Key, ch rune, shift bool, cfg Config, searchCols []int) string {
	return HandleUnifiedKeyResult(s, key, ch, shift, cfg, searchCols).Action
}

// HandleUnifiedKeyResult is like HandleUnifiedKey, but also returns the item
// that produced a select action when one is known.
func HandleUnifiedKeyResult(s *State, key tcell.Key, ch rune, shift bool, cfg Config, searchCols []int) ActionResult {
	ctx := s.TopCtx()

	// Rename mode — all input goes to EditBuffer
	if s.EditMode == "rename" {
		return actionResult(handleRenameKey(s, key, ch))
	}

	// Shift+Enter — universal confirm-select: commit whatever the cursor is on,
	// skipping the scope-push gesture that plain Enter does on folders.
	if shift && key == tcell.KeyEnter {
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			return selectActionResult(visible[ctx.TreeCursor].Item, cfg)
		}
		if len(ctx.Filtered) > 0 {
			return selectActionResult(ctx.Filtered[0], cfg)
		}
		return noActionResult()
	}

	// Nav mode + Backspace: chop last char of the displayed item name (which
	// syncQueryToCursor has kept in sync with Query) and return to search mode.
	// Paired with `/` which also returns to search but preserves the query
	// untouched. Both are the designated exits from normal mode.
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
		s.SetTitle("\u232B", 5)
		return noActionResult()
	}

	// When no search active, delegate to tree navigation (except printable chars)
	if !ctx.SearchActive {
		if key == tcell.KeyRune {
			if ch == '/' {
				// Activate search without inserting the /
				ctx.SearchActive = true
				ctx.NavMode = false
				s.SetTitle("\uF002", 5)
				return noActionResult()
			}
			if ch == '`' {
				// Explicit entry into normal mode (cursor-on-tree). Mirrors the
				// console-summon gesture from Quake/Source games and VS Code's
				// Ctrl+`; complements implicit arrow-key entry.
				ctx.NavMode = true
				visible := TreeVisibleItems(s)
				if ctx.TreeCursor < 0 && len(visible) > 0 {
					ctx.TreeCursor = 0
				}
				s.SetTitle("\uF0A9", 4)
				return noActionResult()
			}
			// Space on a folder -> push scope (same as Enter)
			if ch == ' ' {
				visible := TreeVisibleItems(s)
				if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
					row := visible[ctx.TreeCursor]
					if row.Item.HasChildren {
						PushScope(s, row.ItemIdx, cfg, searchCols)
						return noActionResult()
					}
				}
			}
			// Normal mode (arrow-nav engaged): letter keys are nav bindings or no-op.
			// `/` and Backspace are the designated search-mode re-entries (handled
			// elsewhere). No auto-switchback on an unbound keypress.
			if ctx.NavMode {
				if navKey, arrow, ok := normalModeNavBinding(ch); ok {
					s.SetTitle(arrow, 4)
					result, _ := HandleTreeKeyResult(s, navKey, 0, cfg, searchCols)
					return result
				}
				return noActionResult()
			}
			// Not in nav mode (boot or cleared-to-empty-root): printable activates search
			ctx.SearchActive = true
			ctx.NavMode = false
			ctx.Query = []rune{ch}
			ctx.Cursor = 1
			FilterItems(s, cfg, searchCols)
			UpdateQueryExpansion(s)
			SyncTreeCursorToTopMatch(s)
			return noActionResult()
		}
		result, _ := HandleTreeKeyResult(s, key, ch, cfg, searchCols)
		return result
	}

	// Search active -- unified handling
	return HandleSearchKeyResult(s, key, ch, cfg, searchCols)
}

// HandleKeyEvent processes a single key event against the TUI state (flat mode).
// Returns "" for normal continuation, "cancel" to quit, or "select:<output>" for leaf selection.
//
// shift reports whether Shift was held. Only Shift+Enter is observed — it
// commits the highlighted filtered item without scope-pushing folders.
func HandleKeyEvent(s *State, key tcell.Key, ch rune, shift bool, cfg Config, searchCols []int) string {
	return HandleKeyEventResult(s, key, ch, shift, cfg, searchCols).Action
}

// HandleKeyEventResult is like HandleKeyEvent, but also returns the item that
// produced a select action when one is known.
func HandleKeyEventResult(s *State, key tcell.Key, ch rune, shift bool, cfg Config, searchCols []int) ActionResult {
	ctx := s.TopCtx()
	// Shift+Enter — universal confirm-select, parallel to the tree-mode handler.
	if shift && key == tcell.KeyEnter {
		if ctx.Index >= 0 && ctx.Index < len(ctx.Filtered) {
			return selectActionResult(ctx.Filtered[ctx.Index], cfg)
		}
		return noActionResult()
	}
	switch key {
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
			return noActionResult()
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
			return noActionResult()
		}
		s.Cancelled = true
		return actionResult("cancel")

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
				return selectActionResult(selected, cfg)
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

	case tcell.KeyUp:
		if ctx.Index > 0 {
			ctx.Index--
		} else if ctx.Index == 0 {
			ctx.Index = -1
		}

	case tcell.KeyDown:
		if ctx.Index < len(ctx.Filtered)-1 {
			ctx.Index++
		}

	case tcell.KeyHome:
		ctx.Cursor = 0

	case tcell.KeyEnd:
		ctx.Cursor = len(ctx.Query)

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

	return noActionResult()
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
	result, switchToSearch := HandleTreeKeyResult(s, key, ch, cfg, searchCols)
	return result.Action, switchToSearch
}

// HandleTreeKeyResult is like HandleTreeKey, but also returns the item that
// produced a select action when one is known.
func HandleTreeKeyResult(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) (ActionResult, bool) {
	ctx := s.TopCtx()
	visible := TreeVisibleItems(s)
	visLen := len(visible)

	switch key {
	case tcell.KeyUp:
		ctx.NavMode = true
		if visLen > 0 {
			if ctx.TreeCursor <= 0 {
				ctx.TreeCursor = visLen - 1
			} else {
				ctx.TreeCursor--
			}
		}
		return noActionResult(), false

	case tcell.KeyDown, tcell.KeyTab:
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
		return noActionResult(), false

	case tcell.KeyBacktab:
		ctx.NavMode = true
		if visLen > 0 {
			ctx.TreeCursor--
			if ctx.TreeCursor < 0 {
				ctx.TreeCursor = visLen - 1
			}
		}
		return noActionResult(), false

	case tcell.KeyEnter:
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < visLen {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				curScope := ctx.Scope[len(ctx.Scope)-1]
				if curScope.ParentIdx == row.ItemIdx {
					// Already scoped into this folder — select it (folder-only) or trigger folder-link or no-op
					if cfg.FoldersOnly {
						return selectActionResult(row.Item, cfg), false
					}
					if row.Item.Action != nil && row.Item.Action.Type == "url" {
						return selectActionResult(row.Item, cfg), false
					}
					return noActionResult(), false
				}
				PushScope(s, row.ItemIdx, cfg, searchCols)
				return noActionResult(), false
			}
			return selectActionResult(row.Item, cfg), false
		}
		return noActionResult(), false

	case tcell.KeyRight:
		ctx.NavMode = true
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < visLen {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				PushScope(s, row.ItemIdx, cfg, searchCols)
			}
		}
		return noActionResult(), false

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
		return noActionResult(), false

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// Pop scope first, then context
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return noActionResult(), false
		}
		if len(s.Contexts) > 1 {
			s.PopContext()
			return noActionResult(), false
		}
		return noActionResult(), false

	case tcell.KeyEscape:
		// Pop scope first, then context
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return noActionResult(), false
		}
		if len(s.Contexts) > 1 {
			s.PopContext()
			return noActionResult(), false
		}
		// Root context with nothing to clear -- exit
		s.Cancelled = true
		return actionResult("cancel"), false

	case tcell.KeyRune:
		return noActionResult(), true
	}

	return noActionResult(), false
}

// HandleSearchKey handles all keys when search is active.
// The tree is always the navigation surface -- Up/Down move the tree cursor,
// typing edits the query and auto-positions the cursor on the top match.
func HandleSearchKey(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	return HandleSearchKeyResult(s, key, ch, cfg, searchCols).Action
}

// HandleSearchKeyResult is like HandleSearchKey, but also returns the item
// that produced a select action when one is known.
func HandleSearchKeyResult(s *State, key tcell.Key, ch rune, cfg Config, searchCols []int) ActionResult {
	ctx := s.TopCtx()
	switch key {
	case tcell.KeyEscape:
		// Vim-parallel: Escape from the free-typing mode (query active, NavMode
		// off) exits into normal mode with the query preserved, mirroring
		// Vim's insert→normal transition. Subsequent Escape presses then unwind
		// query → scope → context → cancel.
		if len(ctx.Query) > 0 && !ctx.NavMode {
			ctx.NavMode = true
			visible := TreeVisibleItems(s)
			if ctx.TreeCursor < 0 && len(visible) > 0 {
				ctx.TreeCursor = 0
				syncQueryToCursor(ctx, visible)
			}
			s.SetTitle("\uF0A9", 4)
			return noActionResult()
		}
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
			return noActionResult()
		}
		if len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return noActionResult()
		}
		// At root with empty query -- pop context if stacked, else exit
		if len(s.Contexts) > 1 {
			s.PopContext()
			return noActionResult()
		}
		s.Cancelled = true
		return actionResult("cancel")

	case tcell.KeyUp:
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
		return noActionResult()

	case tcell.KeyDown:
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
		return noActionResult()

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
		return noActionResult()

	case tcell.KeyEnter:
		// Act on tree cursor item
		visible := TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			row := visible[ctx.TreeCursor]
			if row.Item.HasChildren {
				curScope := ctx.Scope[len(ctx.Scope)-1]
				if curScope.ParentIdx == row.ItemIdx {
					// Already scoped into this folder — select it (folder-only) or trigger folder-link or no-op
					if cfg.FoldersOnly {
						return selectActionResult(row.Item, cfg)
					}
					if row.Item.Action != nil && row.Item.Action.Type == "url" {
						return selectActionResult(row.Item, cfg)
					}
					return noActionResult()
				}
				PushScope(s, row.ItemIdx, cfg, searchCols)
				return noActionResult()
			}
			return selectActionResult(row.Item, cfg)
		}
		// No cursor -- act on top match
		if len(ctx.Filtered) > 0 {
			selected := ctx.Filtered[0]
			if selected.HasChildren {
				idx := FindInAll(ctx.AllItems, selected)
				if idx >= 0 {
					curScope := ctx.Scope[len(ctx.Scope)-1]
					if curScope.ParentIdx == idx {
						if cfg.FoldersOnly {
							return selectActionResult(selected, cfg)
						}
						if selected.Action != nil && selected.Action.Type == "url" {
							return selectActionResult(selected, cfg)
						}
						return noActionResult()
					}
					PushScope(s, idx, cfg, searchCols)
				}
				return noActionResult()
			}
			return selectActionResult(selected, cfg)
		}
		return noActionResult()

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		ctx.NavMode = false
		if len(ctx.Query) == 0 && len(ctx.Scope) > 1 {
			PopScope(s, cfg, searchCols)
			return noActionResult()
		}
		if len(ctx.Query) == 0 && len(s.Contexts) > 1 {
			s.PopContext()
			return noActionResult()
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
		return noActionResult()

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
		return noActionResult()

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
		return noActionResult()

	case tcell.KeyRune:
		// Normal mode (arrow-nav engaged): letter keys are nav bindings or no-op.
		// `/` returns to search with query preserved; Backspace returns to search
		// with the last char chopped (the existing KeyBackspace case handles that
		// since syncQueryToCursor keeps Query synced to the cursor's item name).
		if ctx.NavMode {
			if ch == '/' {
				// Return to search mode, query preserved
				ctx.NavMode = false
				s.SetTitle("\uF002", 5)
				return noActionResult()
			}
			if navKey, arrow, ok := normalModeNavBinding(ch); ok {
				s.SetTitle(arrow, 4)
				return HandleSearchKeyResult(s, navKey, 0, cfg, searchCols)
			}
			// Unbound key in normal mode: silent (future: dead-key hint)
			return noActionResult()
		}

		if ch == '`' {
			// Explicit entry into normal mode. Mirrors the Quake/Source console
			// gesture and VS Code's Ctrl+`; complements implicit arrow-key entry.
			ctx.NavMode = true
			visible := TreeVisibleItems(s)
			if ctx.TreeCursor < 0 && len(visible) > 0 {
				ctx.TreeCursor = 0
				syncQueryToCursor(ctx, visible)
			}
			s.SetTitle("\uF0A9", 4)
			return noActionResult()
		}

		ctx.NavMode = false

		// Space on a folder -> enter it
		if ch == ' ' {
			visible := TreeVisibleItems(s)
			if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
				row := visible[ctx.TreeCursor]
				if row.Item.HasChildren {
					PushScope(s, row.ItemIdx, cfg, searchCols)
					return noActionResult()
				}
			}
			// No cursor (hidden top match) -- push scope into it
			if ctx.TreeCursor < 0 && len(ctx.Filtered) > 0 {
				top := ctx.Filtered[0]
				if top.HasChildren {
					idx := FindInAll(ctx.AllItems, top)
					if idx >= 0 {
						PushScope(s, idx, cfg, searchCols)
						return noActionResult()
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
		return noActionResult()
	}

	return noActionResult()
}

// ClickUnifiedRow handles a click on a visual row in the unified view.
func ClickUnifiedRow(s *State, row int, cfg Config, h int) string {
	return ClickUnifiedRowResult(s, row, cfg, h).Action
}

// ClickUnifiedRowResult is like ClickUnifiedRow, but also returns the item
// that produced a select action when one is known.
func ClickUnifiedRowResult(s *State, row int, cfg Config, h int) ActionResult {
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
		return noActionResult()
	}

	vi := ctx.TreeOffset + itemRow
	if vi >= len(visible) {
		return noActionResult()
	}
	ctx.TreeCursor = vi
	tr := visible[vi]
	if tr.Item.HasChildren {
		ctx.TreeExpanded[tr.ItemIdx] = !ctx.TreeExpanded[tr.ItemIdx]
		return noActionResult()
	}
	return selectActionResult(tr.Item, cfg)
}
