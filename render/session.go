package render

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/core"
)

// DrawFunc is a callback for rendering to a Canvas. The terminal frontend
// provides its own draw functions (drawUnified for tree, renderFrame for flat).
type DrawFunc func(c Canvas, s *core.State, cfg core.Config)

// DrawTreeFunc is a callback for tree-mode rendering that also receives
// width, startY, and height parameters.
type DrawTreeFunc func(c Canvas, s *core.State, cfg core.Config, w, startY, h int)

// Session holds a headless TUI instance for WASM or testing use.
// It wraps state, config, and a MemScreen so external code can
// feed key events and receive rendered ANSI frames.
type Session struct {
	state      *core.State
	cfg        core.Config
	searchCols []int
	screen     *MemScreen
	drawTree   DrawTreeFunc
	drawFlat   DrawFunc
}

// SessionFrame is the result of rendering: ANSI text plus cursor position.
type SessionFrame struct {
	ANSI    string
	CursorX int
	CursorY int
}

// SessionAction describes an action emitted by a session event.
type SessionAction = core.ActionResult

// State returns the underlying core.State for direct manipulation (e.g. command injection).
func (sess *Session) State() *core.State {
	return sess.state
}

// NewSession creates a headless TUI session with the given items, config, and dimensions.
// drawFlat is the rendering callback for flat (non-tree) mode.
func NewSession(items []core.Item, cfg core.Config, w, h int, drawFlat DrawFunc) *Session {
	s, searchCols := core.NewState(items, cfg)
	s.TopCtx().Index = -1
	return &Session{
		state:      s,
		cfg:        cfg,
		searchCols: searchCols,
		screen:     NewMemScreen(w, h),
		drawFlat:   drawFlat,
	}
}

// NewTreeSession creates a headless TUI session in unified tree+search mode.
// drawTree is the rendering callback for tree mode; drawFlat is used for flat mode fallback.
func NewTreeSession(items []core.Item, cfg core.Config, w, h int, drawTree DrawTreeFunc, drawFlat DrawFunc) *Session {
	s, searchCols := core.NewState(items, cfg)
	if cfg.Provider != nil {
		s.Provider = cfg.Provider
	}
	ctx := s.TopCtx()
	ctx.Index = -1
	ctx.TreeExpanded = make(map[int]bool)
	ctx.QueryExpanded = make(map[int]bool)
	ctx.TreeCursor = -1
	ctx.TreeOffset = 0
	return &Session{
		state:      s,
		cfg:        cfg,
		searchCols: searchCols,
		screen:     NewMemScreen(w, h),
		drawTree:   drawTree,
		drawFlat:   drawFlat,
	}
}

// Render draws the current state onto the MemScreen and returns an ANSI frame.
func (sess *Session) Render() SessionFrame {
	sess.screen.Clear()
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded != nil && sess.drawTree != nil {
		w, h := sess.screen.Size()
		sess.drawTree(sess.screen, sess.state, sess.cfg, w, 0, h)
	} else if sess.drawFlat != nil {
		sess.drawFlat(sess.screen, sess.state, sess.cfg)
	}
	return SessionFrame{
		ANSI:    sess.screen.ToANSI(),
		CursorX: sess.screen.CursorX,
		CursorY: sess.screen.CursorY,
	}
}

// Resize changes the terminal dimensions and re-renders.
func (sess *Session) Resize(w, h int) SessionFrame {
	sess.screen = NewMemScreen(w, h)
	return sess.Render()
}

// HandleKey processes a key event and returns the new frame.
//
// shift reports whether Shift was held. Used by core to recognize
// Shift+Enter as a universal confirm-select. Callers that can't
// observe modifier state should pass false.
func (sess *Session) HandleKey(key tcell.Key, ch rune, shift bool) (SessionFrame, string) {
	frame, result := sess.HandleKeyResult(key, ch, shift)
	return frame, result.Action
}

// HandleKeyResult processes a key event and returns the action result with the
// item that produced a select action when one is known.
func (sess *Session) HandleKeyResult(key tcell.Key, ch rune, shift bool) (SessionFrame, SessionAction) {
	var result SessionAction
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded != nil {
		result = core.HandleUnifiedKeyResult(sess.state, key, ch, shift, sess.cfg, sess.searchCols)
	} else {
		result = core.HandleKeyEventResult(sess.state, key, ch, shift, sess.cfg, sess.searchCols)
	}
	frame := sess.Render()
	return frame, result
}

// ClickRow handles a mouse click on a visual row in unified mode.
func (sess *Session) ClickRow(row int) (SessionFrame, string) {
	frame, result := sess.ClickRowResult(row)
	return frame, result.Action
}

// ClickRowResult handles a mouse click in unified mode and returns the action
// result with the selected item when the click selects one.
func (sess *Session) ClickRowResult(row int) (SessionFrame, SessionAction) {
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded == nil {
		return sess.Render(), SessionAction{}
	}
	_, h := sess.screen.Size()
	result := core.ClickUnifiedRowResult(sess.state, row, sess.cfg, h)
	frame := sess.Render()
	return frame, result
}

// SelectedItemPath returns the full filesystem path of the currently selected tree item.
func (sess *Session) SelectedItemPath() string {
	ctx := sess.state.TopCtx()
	visible := core.TreeVisibleItems(sess.state)
	if ctx.TreeCursor < 0 || ctx.TreeCursor >= len(visible) {
		return ""
	}
	row := visible[ctx.TreeCursor]
	return core.ItemFullPath(ctx, row.ItemIdx)
}

// SetLabel sets the border label displayed on the top-left of the border.
func (sess *Session) SetLabel(label string) {
	sess.cfg.Label = label
}

// SelectedURL returns the URL of the currently selected item, if any.
func (sess *Session) SelectedURL() string {
	s := sess.state
	ctx := s.TopCtx()
	if ctx.TreeExpanded != nil {
		// Unified mode: tree cursor is the only selection
		visible := core.TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			item := visible[ctx.TreeCursor].Item
			if item.Action != nil && item.Action.Type == "url" {
				return item.Action.Target
			}
		}
		return ""
	}
	if ctx.Index >= 0 && ctx.Index < len(ctx.Filtered) {
		item := ctx.Filtered[ctx.Index]
		if item.Action != nil && item.Action.Type == "url" {
			return item.Action.Target
		}
	}
	return ""
}

// ActionURL returns the URL attached to the item that emitted an action.
func ActionURL(result SessionAction) string {
	if result.HasItem && result.Item.Action != nil && result.Item.Action.Type == "url" {
		return result.Item.Action.Target
	}
	return ""
}

// --- Structured data API for browser frontend ---

// VisibleRow describes a single row for DOM rendering.
type VisibleRow struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	Depth            int    `json:"depth"`
	IsFolder         bool   `json:"isFolder"`
	IsSelected       bool   `json:"isSelected"`
	IsTopMatch       bool   `json:"isTopMatch"`
	NameMatchIndices []int  `json:"nameMatchIndices"`
	DescMatchIndices []int  `json:"descMatchIndices"`
}

// PromptState describes the prompt bar state for DOM rendering.
type PromptState struct {
	Mode      string   `json:"mode"`      // "search" or "nav"
	ScopePath []string `json:"scopePath"` // breadcrumb segments
	Query     string   `json:"query"`
	Cursor    int      `json:"cursor"`
	Ghost     string   `json:"ghost"` // autocomplete ghost text
	Hint      string   `json:"hint"`  // placeholder when empty
}

// UIState describes chrome/metadata for DOM rendering.
type UIState struct {
	Title        string `json:"title"`
	TitlePos     string `json:"titlePos"`
	Label        string `json:"label"`
	Border       bool   `json:"border"`
	TreeOffset   int    `json:"treeOffset"`
	TotalVisible int    `json:"totalVisible"`
}

// GetVisibleRows returns structured data for all visible tree rows.
func (sess *Session) GetVisibleRows() []VisibleRow {
	ctx := sess.state.TopCtx()
	visible := core.TreeVisibleItems(sess.state)

	// Find top match index for highlighting
	topMatchIdx := -1
	if len(ctx.Filtered) > 0 {
		topMatchIdx = core.FindInAll(ctx.AllItems, ctx.Filtered[0])
	}

	rows := make([]VisibleRow, len(visible))
	for i, row := range visible {
		vr := VisibleRow{
			Depth:    row.Item.Depth,
			IsFolder: row.Item.HasChildren,
		}
		if len(row.Item.Fields) > 0 {
			vr.Name = row.Item.Fields[0]
		}
		if len(row.Item.Fields) > 1 {
			vr.Description = row.Item.Fields[1]
		}
		vr.IsSelected = (i == ctx.TreeCursor)
		vr.IsTopMatch = (row.ItemIdx == topMatchIdx && topMatchIdx >= 0)

		// Match indices from filtered results
		if len(row.Item.MatchIndices) > 0 {
			vr.NameMatchIndices = row.Item.MatchIndices[0]
		}
		if len(row.Item.MatchIndices) > 1 {
			vr.DescMatchIndices = row.Item.MatchIndices[1]
		}

		rows[i] = vr
	}
	return rows
}

// GetPromptState returns structured prompt bar state.
func (sess *Session) GetPromptState() PromptState {
	ctx := sess.state.TopCtx()

	mode := "search"
	if ctx.NavMode {
		mode = "nav"
	}

	// Build scope path
	var scopePath []string
	for _, level := range ctx.Scope[1:] {
		if level.ParentIdx >= 0 && level.ParentIdx < len(ctx.AllItems) {
			scopePath = append(scopePath, ctx.AllItems[level.ParentIdx].Fields[0])
		}
	}

	// Ghost autocomplete
	ghost := ""
	query := string(ctx.Query)
	if len(ctx.Query) > 0 && len(ctx.Filtered) > 0 && len(ctx.Filtered[0].Fields) > 0 {
		name := ctx.Filtered[0].Fields[0]
		if len([]rune(name)) > len(ctx.Query) && strings.EqualFold(string([]rune(name)[:len(ctx.Query)]), query) {
			ghost = string([]rune(name)[len(ctx.Query):])
		}
	}

	// Hint
	hint := ""
	if len(ctx.Query) == 0 {
		if ctx.SearchActive || len(ctx.Scope) > 1 {
			hint = "search\u2026"
		} else {
			hint = "type to search\u2026"
		}
	}

	return PromptState{
		Mode:      mode,
		ScopePath: scopePath,
		Query:     query,
		Cursor:    ctx.Cursor,
		Ghost:     ghost,
		Hint:      hint,
	}
}

// GetUIState returns structured chrome/metadata state.
func (sess *Session) GetUIState() UIState {
	ctx := sess.state.TopCtx()
	visible := core.TreeVisibleItems(sess.state)

	title := sess.cfg.Title

	return UIState{
		Title:        title,
		TitlePos:     sess.cfg.TitlePos,
		Label:        sess.cfg.Label,
		Border:       sess.cfg.Border,
		TreeOffset:   ctx.TreeOffset,
		TotalVisible: len(visible),
	}
}
